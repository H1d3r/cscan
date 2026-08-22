package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/utils"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// assetTargetDetailDenormMaxAge 与 list 保持一致：>此阈值的 meta 视为需要回填。
const assetTargetDetailDenormMaxAge = 30 * time.Minute

type AssetTargetDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetTargetDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetTargetDetailLogic {
	return &AssetTargetDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetTargetDetail 获取顶层资产详情（meta + 暴露面统计 + 风险统计）。
func (l *AssetTargetDetailLogic) AssetTargetDetail(req *types.AssetTargetDetailReq) (*types.AssetTargetDetailResp, error) {
	targetId := strings.TrimSpace(req.TargetId)
	if targetId == "" {
		return nil, xerr.NewParamError("targetId is empty")
	}
	tType, tValue, err := model.DecodeTargetID(targetId)
	if err != nil {
		return nil, err
	}

	metaModel := l.svcCtx.GetAssetTargetMetaModel()
	meta, err := metaModel.FindByID(l.ctx, targetId)
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] FindByID fail: %v", err)
	}

	metaPersisted := meta != nil
	if meta == nil {
		meta = rebuildMetaFromAssets(l, targetId, tType, tValue)
		if meta == nil {
			return nil, xerr.NewNotFoundError(fmt.Sprintf("target %s not found", targetId))
		}
	}

	exposure := l.computeExposure(tType, tValue)
	risk := l.computeRisk(tType, tValue)

	if metaPersisted && model.NeedsRefresh(meta, assetTargetDetailDenormMaxAge) {
		l.writebackDenormalized(meta, exposure, risk)
	}

	return &types.AssetTargetDetailResp{
		Code: 0,
		Msg:  "success",
		Data: types.AssetTargetDetailData{
			Meta:     metaToItem(*meta),
			Exposure: exposure,
			Risk:     risk,
		},
	}, nil
}

// writebackDenormalized 把 detail 已算好的 exposure/risk 快照写回 meta 集合，
// 同时覆盖入参 meta 的内联字段，让本次响应 Meta 与 Exposure/Risk 数据一致。
func (l *AssetTargetDetailLogic) writebackDenormalized(meta *model.AssetTargetMeta, exp types.AssetTargetExposureStats, risk types.AssetTargetRiskStats) {
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
	totalSvc := l.computeTotalAssetServices(model.AssetTargetType(meta.TargetType), meta.TargetValue)
	if err := l.svcCtx.GetAssetTargetMetaModel().UpdateDenormalizedWithServices(l.ctx, meta.Id, expSnap, riskSnap, totalSvc); err != nil {
		l.Logger.Errorf("[AssetTargetDetail] UpdateDenormalizedWithServices id=%s fail: %v", meta.Id, err)
	}
	meta.ExposureSubdomains = expSnap.Subdomains
	meta.ExposureIps = expSnap.Ips
	meta.ExposurePorts = expSnap.Ports
	meta.ExposureSites = expSnap.Sites
	meta.ExposureIcons = expSnap.Icons
	meta.ExposureApps = expSnap.Apps
	meta.ExposureDirs = expSnap.Dirs
	meta.ExposureJs = expSnap.Js
	meta.ExposureScreenshots = expSnap.Screenshots
	meta.RiskSensitiveInfo = riskSnap.SensitiveInfo
	meta.RiskSensitiveDir = riskSnap.SensitiveDir
	meta.RiskVulnHigh = riskSnap.VulnHigh
	meta.RiskVulnTotal = riskSnap.VulnTotal
	meta.TotalAssetServices = totalSvc
	meta.RiskUpdatedAt = time.Now()
}

// computeExposure 通过 AggregateGroupByDomain 一次扫描 owning ws 的 asset 集合，
// 按根域名/IP 归并到该目标，再累加 asset 的字段维度。
// 各字段统计口径与目标详情 Inventory 各子 Tab 保持一致，保证气泡数与点进去的列表总数对得上：
//   - 子域名(Subdomains): 归属该目标的非IP主机（按distinct host去重，同一host多端口只计一次）
//   - IP(Ips): 资产解析到的 distinct IPv4（与 Inventory IP Tab 的 ip.ipv4.ip 分组一致）
//   - 端口(Ports): distinct端口数（与端口管理页及 Ports Tab 一致，均限 port>0）
//   - 站点(Sites): Web服务资产数（is_http/service=http,https/有title/有screenshot，限 port>0，
//     与 Services Tab 同基准——端口 0 的子域名占位记录不计入）
//   - 图标(Icons): distinct icon_hash数
//   - 应用(Apps): distinct app数
func (l *AssetTargetDetailLogic) computeExposure(tType model.AssetTargetType, tValue string) types.AssetTargetExposureStats {
	var stats types.AssetTargetExposureStats
	assetModel := l.svcCtx.GetAssetModel()
	rows, err := assetModel.AggregateGroupByDomain(l.ctx)
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] AggregateGroupByDomain fail: %v", err)
		return stats
	}

	hostFilter := hostFilterForTarget(tType, tValue)

	seenHosts := make(map[string]struct{})

	for _, row := range rows {
		if !rowMatchesTarget(row.Host, row.Domain, tType, tValue) {
			continue
		}

		if _, dup := seenHosts[row.Host]; !dup {
			seenHosts[row.Host] = struct{}{}
			if !utils.IsIPAddress(row.Host) && row.Host != "" {
				// 子域名：非IP主机（每个host算一个子域名）
				stats.Subdomains++
			}
		}
	}

	// IP(Ips)：资产解析到的 distinct IPv4（与 Inventory IP Tab 的分组键一致；
	// 旧口径只统计 host 本身是 IP 的资产，域名目标解析出的 IP 永远为 0，与 IP Tab 对不上）
	ipVals, _ := assetModel.Distinct(l.ctx, "ip.ipv4.ip", bson.M{
		"host": hostFilter,
	})
	stats.Ips = countNonEmpty(ipVals)

	// 端口(Ports)：distinct端口数（与端口管理页一致：$group by port）
	portVals, _ := assetModel.Distinct(l.ctx, "port", bson.M{
		"port": bson.M{"$gt": 0},
		"host": hostFilter,
	})
	stats.Ports = countNonEmpty(portVals)

	// 站点(Sites)：Web服务资产数（限 port>0，与 Services Tab 同基准，
	// 端口 0 的子域名占位记录不会出现在任何服务列表里，不应计入站点数）
	webFilter := bson.M{
		"$or": bson.A{
			bson.M{"is_http": true},
			bson.M{"service": bson.M{"$in": bson.A{"http", "https"}}},
			bson.M{"title": bson.M{"$exists": true, "$ne": ""}},
			bson.M{"screenshot": bson.M{"$exists": true, "$ne": ""}},
		},
		"port": bson.M{"$gt": 0},
		"host":  hostFilter,
	}
	siteCount, _ := assetModel.Count(l.ctx, webFilter)
	stats.Sites = int(siteCount)

	// Icon/App 用 Distinct 按值去重，与 IconList/AppList 页面聚合逻辑一致
	iconVals, _ := assetModel.Distinct(l.ctx, "icon_hash", bson.M{
		"host":      hostFilter,
		"icon_hash": bson.M{"$ne": ""},
	})
	stats.Icons = countNonEmpty(iconVals)

	appVals, _ := assetModel.Distinct(l.ctx, "app", bson.M{
		"host": hostFilter,
		"app":  bson.M{"$ne": nil},
	})
	stats.Apps = countNonEmpty(appVals)

	screenshotCount, _ := assetModel.Count(l.ctx, bson.M{
		"$and": bson.A{
			bson.M{"screenshot": bson.M{"$exists": true}},
			bson.M{"screenshot": bson.M{"$ne": ""}},
			bson.M{"screenshot": bson.M{"$ne": nil}},
		},
		"host": hostFilter,
	})
	stats.Screenshots = int(screenshotCount)

	// 目录扫描(Dirs)：dirscan_result 集合按 host 过滤
	dirModel := l.svcCtx.GetDirScanResultModel()
	if dirModel != nil {
		dirCount, _ := dirModel.CountByFilter(l.ctx, bson.M{
			"host": hostFilter,
		})
		stats.Dirs = int(dirCount)
	}

	// JS信息(Js)：jsfinder 集合按 host 过滤
	jsModel := l.svcCtx.GetJSFinderResultModel()
	if jsModel != nil {
		jsCount, _ := jsModel.Count(l.ctx, bson.M{
			"host": hostFilter,
		})
		stats.Js = int(jsCount)
	}

	return stats
}

// computeRisk 统计 vul 中命中该目标的漏洞计数。
// 高危=severity in {critical,high} 或 cvss>=7；is_risk=true 计入风险层。
// SensitiveInfo/SensitiveDir 来自 risk_source="auto:info-leak" 分桶 + DirScanResult 旁路补 SensitiveDir。
// SensitiveInfoItems/SensitiveDirItems/SensitivePathItems 各返回 top-N 命中条目供前端展开。
func (l *AssetTargetDetailLogic) computeRisk(tType model.AssetTargetType, tValue string) types.AssetTargetRiskStats {
	var stats types.AssetTargetRiskStats
	vulModel := l.svcCtx.GetVulModel()

	hostFilter := hostFilterForTarget(tType, tValue)
	total, err := vulModel.Count(l.ctx, bson.M{"host": hostFilter})
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] vul Count fail: %v", err)
		return stats
	}
	stats.VulnTotal = int(total)

	// SensitiveInfo / SensitiveDir：基于 risk_source=auto:info-leak + 关键字分桶
	stats.SensitiveInfo = l.countRiskByKeyword(vulModel, hostFilter, sensitiveInfoKeywords)
	stats.SensitiveDir = l.countRiskByKeyword(vulModel, hostFilter, sensitiveDirKeywords)
	// 旁路补充：dirscan_result 集合中按 host 后缀命中且 path 含敏感特征的条目
	stats.SensitiveDir += l.countSensitiveDirFromScanResult(hostFilter)

	// top-N 命中条目（默认 10），前端可点击展开
	stats.SensitiveInfoItems = l.listRiskByKeyword(vulModel, hostFilter, sensitiveInfoKeywords, sensitiveTopN)
	stats.SensitiveDirItems = l.listRiskByKeyword(vulModel, hostFilter, sensitiveDirKeywords, sensitiveTopN)
	stats.SensitivePathItems = l.listSensitivePathFromScanResult(hostFilter, sensitiveTopN)

	// 漏洞 Tab 列表数据：该目标下最新 20 条漏洞（任意类型，非仅敏感信息）
	stats.VulnItems = l.listLatestVulns(vulModel, hostFilter, 20)

	highCount, err := vulModel.Count(l.ctx, bson.M{
		"host": hostFilter,
		"$or": bson.A{
			bson.M{"severity": bson.M{"$in": bson.A{"critical", "high"}}},
			bson.M{"cvss_score": bson.M{"$gte": 7.0}},
		},
	})
	if err == nil {
		stats.VulnHigh = int(highCount)
	}
	return stats
}

// computeTotalAssetServices 统计归属该目标的服务资产数（host 匹配 + port>0 的资产条数），
// 与 /asset/target/assets 服务列表同口径，保证列表卡片「N 个服务」点进详情后总数一致。
// 旧实现按 distinct service 名称计数（http/https... 仅个位数），与服务列表条数对不上。
func (l *AssetTargetDetailLogic) computeTotalAssetServices(tType model.AssetTargetType, tValue string) int {
	assetModel := l.svcCtx.GetAssetModel()
	hostFilter := hostFilterForTarget(tType, tValue)
	n, err := assetModel.Count(l.ctx, bson.M{
		"host": hostFilter,
		"port": bson.M{"$gt": 0},
	})
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] computeTotalAssetServices Count fail: %v", err)
		return 0
	}
	return int(n)
}

// countRiskByKeyword 在 vul 上按 host 后缀 + is_risk=true + risk_source=auto:info-leak + 关键字分桶计数。
func (l *AssetTargetDetailLogic) countRiskByKeyword(vulModel *model.VulModel, hostFilter interface{}, keywords []string) int {
	if len(keywords) == 0 {
		return 0
	}
	filter := bson.M{
		"host":        hostFilter,
		"is_risk":     true,
		"risk_source": "auto:info-leak",
		"$or":         keywordOrClause(keywords),
	}
	n, err := vulModel.Count(l.ctx, filter)
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] countRiskByKeyword fail: %v", err)
		return 0
	}
	return int(n)
}

// countSensitiveDirFromScanResult 在全局 dirscan_result 集合按 host 后缀 + AI研判为风险计数。
func (l *AssetTargetDetailLogic) countSensitiveDirFromScanResult(hostFilter interface{}) int {
	dirModel := l.svcCtx.GetDirScanResultModel()
	if dirModel == nil {
		return 0
	}
	filter := bson.M{
		"host":      hostFilter,
		"ai_result": "risk",
	}
	n, err := dirModel.CountByFilter(l.ctx, filter)
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] countSensitiveDirFromScanResult fail: %v", err)
		return 0
	}
	return int(n)
}

// listLatestVulns 取目标下最新的 N 条漏洞（按 create_time desc），供漏洞 Tab 列表展示。
func (l *AssetTargetDetailLogic) listLatestVulns(vulModel *model.VulModel, hostFilter interface{}, limit int) []types.AssetTargetSensitiveVulItem {
	if limit <= 0 {
		return nil
	}
	docs, err := vulModel.Find(l.ctx, bson.M{"host": hostFilter}, 1, limit)
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] listLatestVulns fail: %v", err)
		return nil
	}
	items := make([]types.AssetTargetSensitiveVulItem, 0, len(docs))
	for _, v := range docs {
		tags := v.Tags
		if tags == nil {
			tags = []string{}
		}
		items = append(items, types.AssetTargetSensitiveVulItem{
			Id:         v.Id.Hex(),
			VulName:    v.VulName,
			Severity:   v.Severity,
			Host:       v.Host,
			Port:       v.Port,
			Url:        v.Url,
			Source:     v.Source,
			Tags:       tags,
			CreateTime: tsMilli(v.CreateTime),
		})
	}
	return items
}

// listRiskByKeyword 在 vul 上按 host 后缀 + is_risk=true + risk_source=auto:info-leak + 关键字分桶取 top-N 条目。
// 复用 VulModel.Find（已投影排除 request/response/curl_command，自动按 create_time desc 排序）。
func (l *AssetTargetDetailLogic) listRiskByKeyword(vulModel *model.VulModel, hostFilter interface{}, keywords []string, limit int) []types.AssetTargetSensitiveVulItem {
	if len(keywords) == 0 || limit <= 0 {
		return nil
	}
	filter := bson.M{
		"host":        hostFilter,
		"is_risk":     true,
		"risk_source": "auto:info-leak",
		"$or":         keywordOrClause(keywords),
	}
	docs, err := vulModel.Find(l.ctx, filter, 1, limit)
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] listRiskByKeyword fail: %v", err)
		return nil
	}
	items := make([]types.AssetTargetSensitiveVulItem, 0, len(docs))
	for _, v := range docs {
		tags := v.Tags
		if tags == nil {
			tags = []string{}
		}
		items = append(items, types.AssetTargetSensitiveVulItem{
			Id:         v.Id.Hex(),
			VulName:    v.VulName,
			Severity:   v.Severity,
			Host:       v.Host,
			Port:       v.Port,
			Url:        v.Url,
			Source:     v.Source,
			Tags:       tags,
			CreateTime: tsMilli(v.CreateTime),
		})
	}
	return items
}

// listSensitivePathFromScanResult 在全局 dirscan_result 集合按 host 后缀 + AI研判为风险取 top-N 条目。
func (l *AssetTargetDetailLogic) listSensitivePathFromScanResult(hostFilter interface{}, limit int) []types.AssetTargetSensitiveDirItem {
	dirModel := l.svcCtx.GetDirScanResultModel()
	if dirModel == nil || limit <= 0 {
		return nil
	}
	filter := bson.M{
		"host":      hostFilter,
		"ai_result": "risk",
	}
	docs, err := dirModel.FindByFilterWithSort(l.ctx, filter, 1, limit, "", "")
	if err != nil {
		l.Logger.Errorf("[AssetTargetDetail] listSensitivePathFromScanResult fail: %v", err)
		return nil
	}
	items := make([]types.AssetTargetSensitiveDirItem, 0, len(docs))
	for _, d := range docs {
		items = append(items, types.AssetTargetSensitiveDirItem{
			Id:         d.Id.Hex(),
			Host:       d.Host,
			Port:       d.Port,
			Path:       d.Path,
			Url:        d.URL,
			StatusCode: d.StatusCode,
			Title:      d.Title,
			CreateTime: tsMilli(d.CreateTime),
		})
	}
	return items
}

// countNonEmpty 统计 Distinct 返回值中非空元素的数量。
// Distinct 返回 []interface{}，可能包含 "" 或 nil（MongoDB 对缺失字段返回 null）。
func countNonEmpty(vals []interface{}) int {
	n := 0
	for _, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		n++
	}
	return n
}

// rowMatchesTarget 判断 AggregateGroupByDomain 的一行是否归属该目标。
// IP 目标：row.Host 是该 IP。
// 域名目标：resolveRootDomain(row.Host, row.Domain) == tValue。
func rowMatchesTarget(host, domain string, tType model.AssetTargetType, tValue string) bool {
	if tType == model.AssetTargetTypeIP {
		return host == tValue
	}
	return resolveRootDomain(host, domain) == tValue
}

// hostFilterForTarget 返回按 host 过滤的 bson 值。
// IP 目标直接精确匹配；域名目标用后缀正则匹配根域及其所有子域。
func hostFilterForTarget(tType model.AssetTargetType, tValue string) interface{} {
	if tType == model.AssetTargetTypeIP {
		return tValue
	}
	// 匹配根域或任意子域：example.com / *.example.com
	pattern := "^(" + regexpEscape(tValue) + `|.*\.` + regexpEscape(tValue) + ")$"
	return bson.M{"$regex": pattern, "$options": "i"}
}

// regexpEscape 简单转义正则元字符，仅用于 host 这种受控输入。
func regexpEscape(s string) string {
	const meta = `\.+*?()[]{}|^$`
	var b strings.Builder
	for _, c := range s {
		if strings.ContainsRune(meta, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// rebuildMetaFromAssets 当 meta 未命中时，从 asset 重建临时 meta（不写库）。
func rebuildMetaFromAssets(l *AssetTargetDetailLogic, targetId string, tType model.AssetTargetType, tValue string) *model.AssetTargetMeta {
	rows, err := l.svcCtx.GetAssetModel().AggregateGroupByDomain(l.ctx)
	if err != nil {
		return nil
	}
	for _, row := range rows {
		if rowMatchesTarget(row.Host, row.Domain, tType, tValue) {
			return &model.AssetTargetMeta{
				Id:            targetId,
				TargetType:    string(tType),
				TargetValue:   tValue,
				CreateTime:    row.CreateTime,
				UpdateTime:    row.UpdateTime,
				FirstSeenTime: row.CreateTime,
				LastScanTime:  row.UpdateTime,
			}
		}
	}
	return nil
}
