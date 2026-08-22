package logic

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/utils"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	assetTargetListCacheTTL = 30 * time.Second
	// assetTargetDenormMaxAge 决定 list 页触发懒回填的阈值：
	// 超过此值或字段缺失即重算 exposure+risk 快照。
	assetTargetDenormMaxAge = 30 * time.Minute
)

type AssetTargetListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetTargetListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetTargetListLogic {
	return &AssetTargetListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetTargetList 顶层资产（IP/主域名）分页列表。
func (l *AssetTargetListLogic) AssetTargetList(req *types.AssetTargetListReq) (*types.AssetTargetListResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
	if req.PageSize <= 0 || req.PageSize > 200 {
		req.PageSize = 20
	}

	cacheKey := buildAssetTargetListCacheKey(req)
	// 手动刷新：清掉该 key 的缓存再走 GetOrSet 重建，
	// 确保刷新按钮拿到的是扫描完成后的最新数量而不是 30s 内的旧快照
	if req.Refresh {
		l.svcCtx.QueryCache.Delete(cacheKey)
	}
	cached, cerr := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, assetTargetListCacheTTL, func() (interface{}, error) {
		return l.buildList(req)
	})
	if cerr != nil {
		l.Logger.Errorf("[AssetTargetList] cache read fail: %v", cerr)
		return l.buildList(req)
	}
	if r, ok := cached.(*types.AssetTargetListResp); ok && r != nil {
		return r, nil
	}
	return l.buildList(req)
}

func (l *AssetTargetListLogic) buildList(req *types.AssetTargetListReq) (*types.AssetTargetListResp, error) {
	query := strings.TrimSpace(req.Query)
	targetType := strings.TrimSpace(req.TargetType)
	if targetType != "" && targetType != string(model.AssetTargetTypeIP) && targetType != string(model.AssetTargetTypeDomain) {
		return nil, xerr.NewParamError(fmt.Sprintf("invalid targetType %q", targetType))
	}

	// scope → internalNetworkId 转换
	var internalNetworkId string
	if req.Scope == "internal" {
		internalNetworkId = "__notnull__"
	} else if req.Scope == "external" {
		internalNetworkId = "__null__"
	}

	detailLogic := NewAssetTargetDetailLogic(l.ctx, l.svcCtx)

	metaModel := l.svcCtx.GetAssetTargetMetaModel()
	docs, total, err := metaModel.FindPage(l.ctx, targetType, query, req.Labels, req.Page, req.PageSize, "last_scan_time", req.ScanStatus, req.Source, internalNetworkId)
	if err != nil {
		return nil, err
	}

	// 扫描流程只写 asset 集合，不落 asset_target_meta；
	// 周期性（60s 节流）从 asset 反推目标并回填，保证扫描完成后列表可见。
	if l.trySyncTargetsFromAssets(metaModel) > 0 {
		docs, total, err = metaModel.FindPage(l.ctx, targetType, query, req.Labels, req.Page, req.PageSize, "last_scan_time", req.ScanStatus, req.Source, internalNetworkId)
		if err != nil {
			return nil, err
		}
	}

	list := make([]types.AssetTargetListItem, 0, len(docs))
	for i := range docs {
		d := &docs[i]
		if model.NeedsRefresh(d, assetTargetDenormMaxAge) {
			l.refreshDenormalized(detailLogic, d)
		}
		list = append(list, metaToItem(*d))
	}
	return &types.AssetTargetListResp{Code: 0, Msg: "success", Total: total, List: list}, nil
}

// refreshDenormalized 复用 detail logic 的 computeExposure/computeRisk 重新算,
// 把 snapshot 写回 meta 集合并覆盖入参 doc 的内联字段，供本次响应返回最新值。
// UpdateDenormalizedWithServices 失败仅记日志，不影响本次响应（保持内存副本已刷新）。
func (l *AssetTargetListLogic) refreshDenormalized(dl *AssetTargetDetailLogic, d *model.AssetTargetMeta) {
	tType := model.AssetTargetType(d.TargetType)
	exp := dl.computeExposure(tType, d.TargetValue)
	risk := dl.computeRisk(tType, d.TargetValue)
	totalSvc := dl.computeTotalAssetServices(tType, d.TargetValue)

	expSnap := model.ExposureSnapshot{
		Subdomains:  exp.Subdomains,
		Ips:         exp.Ips,
		Ports:       exp.Ports,
		Sites:       exp.Sites,
		Icons:       exp.Icons,
		Apps:        exp.Apps,
		Dirs:        exp.Dirs,
		Js:          exp.Js,
		Screenshots: exp.Screenshots,
	}
	riskSnap := model.RiskSnapshot{
		SensitiveInfo: risk.SensitiveInfo,
		SensitiveDir:  risk.SensitiveDir,
		VulnHigh:      risk.VulnHigh,
		VulnTotal:     risk.VulnTotal,
	}
	if err := l.svcCtx.GetAssetTargetMetaModel().UpdateDenormalizedWithServices(l.ctx, d.Id, expSnap, riskSnap, totalSvc); err != nil {
		l.Logger.Errorf("[AssetTargetList] UpdateDenormalizedWithServices id=%s fail: %v", d.Id, err)
	}
	d.ExposureSubdomains = expSnap.Subdomains
	d.ExposureIps = expSnap.Ips
	d.ExposurePorts = expSnap.Ports
	d.ExposureSites = expSnap.Sites
	d.ExposureIcons = expSnap.Icons
	d.ExposureApps = expSnap.Apps
	d.ExposureDirs = expSnap.Dirs
	d.ExposureJs = expSnap.Js
	d.ExposureScreenshots = expSnap.Screenshots
	d.RiskSensitiveInfo = riskSnap.SensitiveInfo
	d.RiskSensitiveDir = riskSnap.SensitiveDir
	d.RiskVulnHigh = riskSnap.VulnHigh
	d.RiskVulnTotal = riskSnap.VulnTotal
	d.RiskUpdatedAt = time.Now()
	d.TotalAssetServices = totalSvc
}

func metaToItem(m model.AssetTargetMeta) types.AssetTargetListItem {
	labels := m.Labels
	if labels == nil {
		labels = []string{}
	}
	return types.AssetTargetListItem{
		Id:                  m.Id,
		TargetType:          m.TargetType,
		TargetValue:         m.TargetValue,
		Labels:              labels,
		Memo:                m.Memo,
		ColorTag:            m.ColorTag,
		ScanStatus:          m.ScanStatus,
		Source:              m.Source,
		InternalNetworkId:   m.InternalNetworkId,
		TotalAssetServices:  m.TotalAssetServices,
		LastScanTime:        tsMilli(m.LastScanTime),
		FirstSeen:           tsMilli(m.FirstSeenTime),
		TaskCount:           m.TaskCount,

		ExposureSubdomains:  m.ExposureSubdomains,
		ExposureIps:         m.ExposureIps,
		ExposurePorts:       m.ExposurePorts,
		ExposureSites:       m.ExposureSites,
		ExposureIcons:       m.ExposureIcons,
		ExposureApps:        m.ExposureApps,
		ExposureDirs:        m.ExposureDirs,
		ExposureJs:          m.ExposureJs,
		ExposureScreenshots: m.ExposureScreenshots,
		RiskSensitiveInfo:   m.RiskSensitiveInfo,
		RiskSensitiveDir:    m.RiskSensitiveDir,
		RiskVulnHigh:        m.RiskVulnHigh,
		RiskVulnTotal:       m.RiskVulnTotal,
	}
}

func tsMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// assetTargetSyncInterval 控制从 asset 集合反推目标回填 meta 的最小间隔，
// 避免列表 3s 轮询每次都触发全量聚合。
const assetTargetSyncInterval = 60 * time.Second

var lastAssetTargetSync atomic.Int64

// trySyncTargetsFromAssets 节流触发 syncTargetsFromAssets；未到间隔或无新增返回 0。
func (l *AssetTargetListLogic) trySyncTargetsFromAssets(metaModel *model.AssetTargetMetaModel) int {
	now := time.Now().UnixMilli()
	last := lastAssetTargetSync.Load()
	if now-last < assetTargetSyncInterval.Milliseconds() {
		return 0
	}
	if !lastAssetTargetSync.CompareAndSwap(last, now) {
		return 0
	}
	return l.syncTargetsFromAssets(metaModel)
}

// syncTargetsFromAssets 从 asset 集合反推顶层目标（IP 或根域名），
// upsert 进 asset_target_meta。返回成功写入的目标数。
// firstSeen 取该目标下最早资产时间，lastScanTime 取最新资产更新时间。
func (l *AssetTargetListLogic) syncTargetsFromAssets(metaModel *model.AssetTargetMetaModel) int {
	assetModel := l.svcCtx.GetAssetModel()
	if assetModel == nil {
		return 0
	}
	rows, err := assetModel.AggregateGroupByDomain(l.ctx)
	if err != nil || len(rows) == 0 {
		if err != nil {
			l.Logger.Errorf("[AssetTargetList] syncTargetsFromAssets aggregate fail: %v", err)
		}
		return 0
	}

	type targetAcc struct {
		tType     model.AssetTargetType
		value     string
		firstSeen time.Time
		lastScan  time.Time
	}
	acc := make(map[string]*targetAcc)
	for i := range rows {
		row := &rows[i]
		if row.Host == "" {
			continue
		}
		var tType model.AssetTargetType
		var value string
		if utils.IsIPAddress(row.Host) {
			tType, value = model.AssetTargetTypeIP, row.Host
		} else {
			root := resolveRootDomain(row.Host, row.Domain)
			if root == "" {
				continue
			}
			tType, value = model.AssetTargetTypeDomain, root
		}
		id := model.EncodeTargetID(tType, value)
		t, ok := acc[id]
		if !ok {
			t = &targetAcc{tType: tType, value: value, firstSeen: row.CreateTime, lastScan: row.UpdateTime}
			acc[id] = t
			continue
		}
		if row.CreateTime.Before(t.firstSeen) {
			t.firstSeen = row.CreateTime
		}
		if row.UpdateTime.After(t.lastScan) {
			t.lastScan = row.UpdateTime
		}
	}

	// 从最近的 maintask 推导每个目标的扫描状态（最新任务优先）
	scanStatusByTarget := l.buildScanStatusByTarget()

	synced := 0
	for _, t := range acc {
		if t.firstSeen.IsZero() {
			t.firstSeen = time.Now()
		}
		if t.lastScan.IsZero() {
			t.lastScan = t.firstSeen
		}
		doc := &model.AssetTargetMeta{
			Id:            model.EncodeTargetID(t.tType, t.value),
			TargetType:    string(t.tType),
			TargetValue:   t.value,
			Source:        "auto",
			ScanStatus:    scanStatusByTarget[t.value],
			FirstSeenTime: t.firstSeen,
			LastScanTime:  t.lastScan,
		}
		if err := metaModel.Upsert(l.ctx, doc); err != nil {
			l.Logger.Errorf("[AssetTargetList] sync upsert %s fail: %v", doc.Id, err)
			continue
		}
		synced++
	}
	if synced > 0 {
		l.Logger.Infof("[AssetTargetList] synced %d targets from assets", synced)
	}
	return synced
}

// buildScanStatusByTarget 遍历最近的 maintask（update_time 倒序），
// 按任务 Target 字段中的目标 token 建立目标 → 扫描状态映射，首个命中即最新任务。
func (l *AssetTargetListLogic) buildScanStatusByTarget() map[string]string {
	statusByTarget := make(map[string]string)
	taskModel := l.svcCtx.GetMainTaskModel()
	if taskModel == nil {
		return statusByTarget
	}
	tasks, err := taskModel.FindRecent(l.ctx, 200)
	if err != nil {
		l.Logger.Errorf("[AssetTargetList] FindRecent fail: %v", err)
		return statusByTarget
	}
	for _, tg := range tasks {
		status := mapTaskStatusToScan(tg.Status)
		if status == "" {
			continue
		}
		for _, tok := range strings.FieldsFunc(tg.Target, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ',' || r == ';' || r == ' ' || r == '\t'
		}) {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if _, exists := statusByTarget[tok]; !exists {
				statusByTarget[tok] = status
			}
		}
	}
	return statusByTarget
}

// mapTaskStatusToScan 把 maintask 状态映射为 meta 的 scanStatus
//（pending/in_progress/completed/failed/cancelled）。
func mapTaskStatusToScan(status string) string {
	switch status {
	case model.TaskStatusCreated, model.TaskStatusPending, model.TaskStatusPaused:
		return "pending"
	case model.TaskStatusStarted:
		return "in_progress"
	case model.TaskStatusSuccess:
		return "completed"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusRevoked, model.TaskStatusStopped:
		return "cancelled"
	default:
		return ""
	}
}

func buildAssetTargetListCacheKey(req *types.AssetTargetListReq) string {
	labelsHash := sha1.Sum([]byte(strings.Join(req.Labels, ",")))
	queryHash := sha1.Sum([]byte(strings.TrimSpace(req.Query)))
	// FindPage 实际参与的过滤字段必须全部进 key，
	// 否则切换 scanStatus/scope/source 筛选会命中同 key 的串数据缓存
	return fmt.Sprintf("asset_target_list:%s:%s:%s:%s:%s:%s:%d:%d",
		strings.TrimSpace(req.TargetType),
		hex.EncodeToString(queryHash[:6]),
		hex.EncodeToString(labelsHash[:6]),
		strings.TrimSpace(req.ScanStatus),
		strings.TrimSpace(req.Scope),
		strings.TrimSpace(req.Source),
		req.Page, req.PageSize,
	)
}
