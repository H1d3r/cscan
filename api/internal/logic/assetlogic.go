package logic

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// isValidImageBytes 检查二进制数据是否为有效的图片格式（通过魔数判断）
func isValidImageBytes(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return true
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	// GIF: 47 49 46 38
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return true
	}
	// ICO: 00 00 01 00 or 00 00 02 00
	if data[0] == 0x00 && data[1] == 0x00 && (data[2] == 0x01 || data[2] == 0x02) && data[3] == 0x00 {
		return true
	}
	// BMP: 42 4D
	if data[0] == 0x42 && data[1] == 0x4D {
		return true
	}
	// WebP: RIFF....WEBP
	if len(data) >= 12 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
		return true
	}
	// SVG: 以 '<svg' 或 '<?xml' 开头（文本格式）
	if data[0] == '<' {
		header := strings.ToLower(string(data[:min(len(data), 100)]))
		if strings.HasPrefix(header, "<svg") || (strings.HasPrefix(header, "<?xml") && strings.Contains(header, "<svg")) {
			return true
		}
	}
	return false
}

// formatTimeIfNotZero 格式化时间，如果是零值则返回空字符串
func formatTimeIfNotZero(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// cleanAppName 清理指纹名称，去掉类似 [custom(xxx)] 的后缀
func cleanAppName(app string) string {
	// 匹配 [xxx] 或 [xxx(yyy)] 格式的后缀并去掉
	re := regexp.MustCompile(`\s*\[.*\]\s*$`)
	return strings.TrimSpace(re.ReplaceAllString(app, ""))
}

// sortAssetsByTime 按时间排序资产
func sortAssetsByTime(assets []model.Asset, byUpdateTime bool) {
	sort.Slice(assets, func(i, j int) bool {
		if byUpdateTime {
			return assets[i].UpdateTime.After(assets[j].UpdateTime)
		}
		return assets[i].CreateTime.After(assets[j].CreateTime)
	})
}

// sortMapToStatItems 将 map 转换为排序后的 StatItem 列表
func sortMapToStatItems(m map[string]int, limit int) []types.StatItem {
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	result := make([]types.StatItem, 0, limit)
	for i, item := range sorted {
		if i >= limit {
			break
		}
		result = append(result, types.StatItem{Name: item.Key, Count: item.Value})
	}
	return result
}

// sortMapToStatItemsInt 将 int key 的 map 转换为排序后的 StatItem 列表
func sortMapToStatItemsInt(m map[int]int, limit int) []types.StatItem {
	type kv struct {
		Key   int
		Value int
	}
	var sorted []kv
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	result := make([]types.StatItem, 0, limit)
	for i, item := range sorted {
		if i >= limit {
			break
		}
		result = append(result, types.StatItem{Name: strconv.Itoa(item.Key), Count: item.Value})
	}
	return result
}

// sortIconHashMap 将 IconHash map 转换为排序后的列表
func sortIconHashMap(m map[string]*types.IconHashStatItem, limit int) []types.IconHashStatItem {
	var sorted []*types.IconHashStatItem
	for _, v := range m {
		sorted = append(sorted, v)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})

	result := make([]types.IconHashStatItem, 0, limit)
	for i, item := range sorted {
		if i >= limit {
			break
		}
		result = append(result, *item)
	}
	return result
}

// parseQuerySyntax 解析查询语法
// 支持格式: port=80 && service=http || title="test"
// 如果查询不包含 = 语法，则作为模糊搜索匹配 host/title/domain/service/authority
func parseQuerySyntax(query string, filter bson.M) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}

	// 如果不包含 = 号，则视为普通文本模糊搜索
	if !strings.Contains(query, "=") {
		// 修复 M-18：转义正则元字符，防止 ReDoS
		escQ := regexp.QuoteMeta(query)
		filter["$or"] = []bson.M{
			{"host": bson.M{"$regex": escQ, "$options": "i"}},
			{"authority": bson.M{"$regex": escQ, "$options": "i"}},
			{"title": bson.M{"$regex": escQ, "$options": "i"}},
			{"domain": bson.M{"$regex": escQ, "$options": "i"}},
			{"service": bson.M{"$regex": escQ, "$options": "i"}},
		}
		return
	}

	// 简单解析：支持 field=value 格式，多个条件用 && 连接
	// 例如: port=80 && service=http && title=test
	conditions := strings.Split(query, "&&")
	for _, cond := range conditions {
		cond = strings.TrimSpace(cond)
		if cond == "" {
			continue
		}

		// 解析 field=value 或 field="value"
		parts := strings.SplitN(cond, "=", 2)
		if len(parts) != 2 {
			continue
		}

		field := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// 去除引号
		value = strings.Trim(value, "\"'")

		// 映射字段名
		switch strings.ToLower(field) {
		case "port":
			if port, err := strconv.Atoi(value); err == nil {
				filter["port"] = port
			}
		case "host", "ip":
			filter["host"] = bson.M{"$regex": regexp.QuoteMeta(value), "$options": "i"}
		case "service", "protocol":
			filter["service"] = bson.M{"$regex": regexp.QuoteMeta(value), "$options": "i"}
		case "title":
			filter["title"] = bson.M{"$regex": regexp.QuoteMeta(value), "$options": "i"}
		case "app", "finger", "fingerprint":
			filter["app"] = bson.M{"$regex": regexp.QuoteMeta(cleanAppName(value)), "$options": "i"}
		case "status", "httpstatus":
			filter["status"] = value
		case "domain":
			filter["domain"] = bson.M{"$regex": regexp.QuoteMeta(value), "$options": "i"}
		case "banner":
			filter["banner"] = bson.M{"$regex": regexp.QuoteMeta(value), "$options": "i"}
		}
	}
}

type AssetListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetListLogic {
	return &AssetListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetListLogic) AssetList(req *types.AssetListReq) (resp *types.AssetListResp, err error) {
	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
	l.Logger.Infof("AssetList查询: page=%d, pageSize=%d", req.Page, req.PageSize)

	// 构建查询条件
	filter := bson.M{}

	// 如果有语法查询，解析语法
	if req.Query != "" {
		parseQuerySyntax(req.Query, filter)
	}

	// 独立筛选条件：无论是否有 query 都生效，且不覆盖 parseQuerySyntax 已设置的字段
	if req.Host != "" {
		if _, exists := filter["host"]; !exists {
			filter["host"] = bson.M{"$regex": regexp.QuoteMeta(req.Host), "$options": "i"}
		}
	}
	if req.Port > 0 {
		if _, exists := filter["port"]; !exists {
			filter["port"] = req.Port
		}
	} else {
		// 端口为空(port=0)的资产不应该出现在端口列表中
		if _, exists := filter["port"]; !exists {
			filter["port"] = bson.M{"$gt": 0}
		}
	}
	if req.Service != "" {
		if _, exists := filter["service"]; !exists {
			filter["service"] = bson.M{"$regex": regexp.QuoteMeta(req.Service), "$options": "i"}
		}
	}
	if req.Title != "" {
		if _, exists := filter["title"]; !exists {
			filter["title"] = bson.M{"$regex": regexp.QuoteMeta(req.Title), "$options": "i"}
		}
	}
	if req.App != "" {
		if _, exists := filter["app"]; !exists {
			cleanedApp := cleanAppName(req.App)
			filter["app"] = bson.M{"$regex": regexp.QuoteMeta(cleanedApp), "$options": "i"}
		}
	}
	if req.HttpStatus != "" {
		if _, exists := filter["status"]; !exists {
			filter["status"] = req.HttpStatus
		}
	}
	if req.IconHash != "" {
		if _, exists := filter["icon_hash"]; !exists {
			filter["icon_hash"] = req.IconHash
		}
	}

	// 只看新资产（旧路径：new 标记，保留兼容）
	if req.OnlyNew {
		filter["new"] = true
	}
	// T1.2: 新增窗口筛选 —— 基于 first_seen_time 的"近 N 天新增"口径，解决 G1（new 永久 true 造成虚高）。
	// new 旧路径保留兼容；NewWithinDays=0 表示不过滤。
	if req.NewWithinDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -req.NewWithinDays)
		filter["first_seen_time"] = bson.M{"$gte": cutoff}
	}
	// 只看有更新
	if req.OnlyUpdated {
		filter["update"] = true
	}
	// 时间范围筛选：最近N天内更新的资产
	if req.UpdatedWithinDays > 0 {
		cutoffTime := time.Now().AddDate(0, 0, -req.UpdatedWithinDays)
		filter["last_status_change_time"] = bson.M{"$gte": cutoffTime}
		// 同时要求是已更新状态
		filter["update"] = true
	}
	// 排除CDN/Cloud资产
	if req.ExcludeCdn {
		filter["cdn"] = bson.M{"$ne": true}
		filter["cloud"] = bson.M{"$ne": true}
	}
	// 按组织筛选
	if req.OrgId != "" {
		filter["org_id"] = req.OrgId
	}

	var total int64
	var assets []model.Asset

	assetModel := l.svcCtx.GetAssetModel()

	total, err = assetModel.Count(l.ctx, filter)
	if err != nil {
		return &types.AssetListResp{Code: 500, Msg: "查询失败"}, nil
	}

	// 查询列表 - 支持按风险评分排序
	if req.SortByRisk {
		assets, err = assetModel.FindByRiskScore(l.ctx, filter, req.Page, req.PageSize, false)
	} else {
		sortField := "update_time"
		if !req.SortByUpdate {
			sortField = "create_time"
		}
		assets, err = assetModel.FindWithSort(l.ctx, filter, req.Page, req.PageSize, sortField)
	}
	if err != nil {
		return &types.AssetListResp{Code: 500, Msg: "查询失败"}, nil
	}

	// 构建组织ID到名称的映射（走带缓存版的 LoadOrgMap，避免每次 list 都全表加载 organization）
	orgNameMap := common.LoadOrgMap(l.ctx, l.svcCtx)

	// 转换响应
	list := make([]types.Asset, 0, len(assets))
	for _, a := range assets {
		// 获取归属地信息
		location := ""
		if len(a.Ip.IpV4) > 0 && a.Ip.IpV4[0].Location != "" {
			location = a.Ip.IpV4[0].Location
		}

		// 构建IP信息
		var ipInfo *types.IPInfo
		if len(a.Ip.IpV4) > 0 || len(a.Ip.IpV6) > 0 {
			ipInfo = &types.IPInfo{}
			for _, ipv4 := range a.Ip.IpV4 {
				ipInfo.IPV4 = append(ipInfo.IPV4, types.IPV4Info{
					IP:       ipv4.IPName,
					Location: ipv4.Location,
				})
			}
			for _, ipv6 := range a.Ip.IpV6 {
				ipInfo.IPV6 = append(ipInfo.IPV6, types.IPV6Info{
					IP:       ipv6.IPName,
					Location: ipv6.Location,
				})
			}
		}

		// 获取组织名称
		orgName := ""
		if a.OrgId != "" {
			if name, ok := orgNameMap[a.OrgId]; ok {
				orgName = name
			}
			l.Logger.Infof("Asset %s:%d has orgId=%s, orgName=%s", a.Host, a.Port, a.OrgId, orgName)
		} else {
			l.Logger.Infof("Asset %s:%d has NO orgId", a.Host, a.Port)
		}

		// 将 IconHashBytes 转换为 base64（仅当是有效图片数据时）
		iconData := ""
		if len(a.IconHashBytes) > 0 && isValidImageBytes(a.IconHashBytes) {
			iconData = base64.StdEncoding.EncodeToString(a.IconHashBytes)
		}

		list = append(list, types.Asset{
			Id:        a.Id.Hex(),
			Authority: a.Authority,
			Host:      a.Host,
			Port:      a.Port,
			Category:  a.Category,
			Service:   a.Service,
			Title:     a.Title,
			// 同一技术的来源后缀变体折叠为一条展示（前端 TechTag 再做归一化展示）
			App:                  model.MergeAppsDedup(nil, a.App),
			HttpStatus:           a.HttpStatus,
			HttpHeader:           a.HttpHeader,
			HttpBody:             a.HttpBody,
			Banner:               a.Banner,
			IconHash:             a.IconHash,
			IconData:             iconData,
			Screenshot:           a.Screenshot,
			Location:             location,
			IP:                   ipInfo,
			IsCDN:                a.IsCDN,
			IsCloud:              a.IsCloud,
			IsNew:                a.IsNewAsset,
			IsUpdated:            a.IsUpdated,
			CreateTime:           a.CreateTime.Local().Format("2006-01-02 15:04:05"),
			UpdateTime:           a.UpdateTime.Local().Format("2006-01-02 15:04:05"),
			LastStatusChangeTime: formatTimeIfNotZero(a.LastStatusChangeTime),
			FirstSeenTaskId:      a.FirstSeenTaskId,
			// 组织信息
			OrgId:   a.OrgId,
			OrgName: orgName,
			// 新增字段 - 风险评分
			RiskScore: a.RiskScore,
			RiskLevel: a.RiskLevel,
		})
	}

	return &types.AssetListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
}

type AssetStatLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetStatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetStatLogic {
	return &AssetStatLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetStatLogic) AssetStat(req *types.AssetStatReq) (resp *types.AssetStatResp, err error) {
	if req == nil {
		req = &types.AssetStatReq{}
	}
	targetID := strings.TrimSpace(req.TargetId)
	cacheKey := "asset_stat"
	if targetID != "" {
		cacheKey += ":" + targetID
	}
	cached, cacheErr := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, 60*time.Second, func() (interface{}, error) {
		return l.loadAssetStat(targetID)
	})
	if cacheErr != nil {
		return l.loadAssetStat(targetID)
	}
	if r, ok := cached.(*types.AssetStatResp); ok {
		return r, nil
	}
	return l.loadAssetStat(targetID)
}

func (l *AssetStatLogic) loadAssetStat(targetID string) (*types.AssetStatResp, error) {
	attackSurfaceData, err := buildAttackSurfaceStatData(l.ctx, l.svcCtx, targetID)
	if err != nil {
		return nil, err
	}

	return &types.AssetStatResp{
		Code: 0,
		Msg:  "success",
		Data: attackSurfaceData,
	}, nil
}

// AssetDeleteLogic 单个删除
type AssetDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetDeleteLogic {
	return &AssetDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetDeleteLogic) AssetDelete(req *types.AssetDeleteReq) (resp *types.BaseResp, err error) {
	assetModel := l.svcCtx.GetAssetModel()
	err = assetModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// AssetBatchDeleteLogic 批量删除
type AssetBatchDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetBatchDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetBatchDeleteLogic {
	return &AssetBatchDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetBatchDeleteLogic) AssetBatchDelete(req *types.AssetBatchDeleteReq) (resp *types.BaseResp, err error) {
	if len(req.Ids) == 0 {
		return &types.BaseResp{Code: 400, Msg: "请选择要删除的资产"}, nil
	}

	assetModel := l.svcCtx.GetAssetModel()
	deleted, err := assetModel.BatchDelete(l.ctx, req.Ids)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(deleted, 10) + " 条资产"}, nil
}

// AssetClearLogic 清空资产
type AssetClearLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetClearLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetClearLogic {
	return &AssetClearLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetClearLogic) AssetClear() (resp *types.BaseResp, err error) {
	assetModel := l.svcCtx.GetAssetModel()
	deleted, err := assetModel.Clear(l.ctx)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "清空资产失败"}, nil
	}

	// 清空对应的资产历史表
	historyModel := l.svcCtx.GetAssetHistoryModel()
	historyModel.Clear(l.ctx)

	// 失效 stat 缓存
	l.svcCtx.QueryCache.Delete("asset_stat")

	return &types.BaseResp{Code: 0, Msg: "成功清空 " + strconv.FormatInt(deleted, 10) + " 条资产"}, nil
}

// DomainClearLogic 清空域名
type DomainClearLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDomainClearLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DomainClearLogic {
	return &DomainClearLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DomainClearLogic) DomainClear() (resp *types.BaseResp, err error) {
	assetModel := model.NewAssetModel(l.svcCtx.MongoDB)
	filter := bson.M{
		"$or": []bson.M{
			{"category": "domain"},
			{"domain": bson.M{"$exists": true, "$ne": ""}},
			{"source": "subfinder"},
		},
	}
	deleted, _ := assetModel.DeleteByFilter(l.ctx, filter)

	return &types.BaseResp{Code: 0, Msg: "成功清空 " + strconv.FormatInt(deleted, 10) + " 个域名"}, nil
}

// IPClearLogic 清空 IP
type IPClearLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewIPClearLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IPClearLogic {
	return &IPClearLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *IPClearLogic) IPClear() (resp *types.BaseResp, err error) {
	assetModel := model.NewAssetModel(l.svcCtx.MongoDB)
	filter := bson.M{
		"$and": []bson.M{
			{"host": bson.M{"$exists": true, "$ne": ""}},
			{"host": bson.M{"$regex": `^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`}},
		},
	}
	deleted, _ := assetModel.DeleteByFilter(l.ctx, filter)

	return &types.BaseResp{Code: 0, Msg: "成功清空 " + strconv.FormatInt(deleted, 10) + " 个 IP"}, nil
}

// SiteClearLogic 清空站点
type SiteClearLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSiteClearLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SiteClearLogic {
	return &SiteClearLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SiteClearLogic) SiteClear() (resp *types.BaseResp, err error) {
	assetModel := model.NewAssetModel(l.svcCtx.MongoDB)
	filter := bson.M{
		"httpStatus": bson.M{"$exists": true, "$ne": ""},
	}
	deleted, _ := assetModel.DeleteByFilter(l.ctx, filter)

	return &types.BaseResp{Code: 0, Msg: "成功清空 " + strconv.FormatInt(deleted, 10) + " 个站点"}, nil
}

// PortClearLogic 清空端口
type PortClearLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPortClearLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PortClearLogic {
	return &PortClearLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PortClearLogic) PortClear() (resp *types.BaseResp, err error) {
	// 端口数据来源于资产，清空所有资产即可
	// 但这里不删除资产本身，仅返回提示（端口是资产的聚合视图）
	return &types.BaseResp{Code: 0, Msg: "端口数据为资产聚合视图，请通过清空资产来清理端口数据"}, nil
}

// ScreenshotsClearLogic 清空截图
type ScreenshotsClearLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScreenshotsClearLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScreenshotsClearLogic {
	return &ScreenshotsClearLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScreenshotsClearLogic) ScreenshotsClear() (resp *types.BaseResp, err error) {
	assetModel := model.NewAssetModel(l.svcCtx.MongoDB)
	result, err := assetModel.UpdateManyByFilter(l.ctx, bson.M{"screenshot": bson.M{"$exists": true, "$ne": ""}}, bson.M{"$set": bson.M{"screenshot": ""}})
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "清空截图失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "成功清空 " + strconv.FormatInt(result, 10) + " 条截图"}, nil
}

// AssetImportLogic 导入资产
type AssetImportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetImportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetImportLogic {
	return &AssetImportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetImportLogic) AssetImport(req *types.AssetImportReq) (resp *types.AssetImportResp, err error) {
	if len(req.Targets) == 0 {
		return &types.AssetImportResp{Code: 400, Msg: "请输入要导入的目标"}, nil
	}

	assetModel := l.svcCtx.GetAssetModel()
	metaModel := l.svcCtx.GetAssetTargetMetaModel()
	historyModel := l.svcCtx.GetAssetHistoryModel()

	var newCount, skipCount, errorCount int
	var errorDetails []string
	total := 0

	for _, target := range req.Targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		total++

		host, port, scheme, err := parseTarget(target)
		if err != nil {
			errorCount++
			errorDetails = append(errorDetails, fmt.Sprintf("%s: %s", target, err.Error()))
			continue
		}

		// 检查是否已存在
		existing, _ := assetModel.FindByHostPort(l.ctx, host, port)
		if existing != nil {
			// AssetImportReq 不携带 labels/memo，命中已存在时无需更新；保留 skip 计数语义
			skipCount++
			continue
		}

		// 创建新资产
		authority := host + ":" + strconv.Itoa(port)
		asset := &model.Asset{
			Authority: authority,
			Host:      host,
			Port:      port,
			Service:   scheme,
			IsHTTP:    scheme == "http" || scheme == "https",
			Source:    "import",
		}

		if err := assetModel.Insert(l.ctx, asset); err != nil {
			errorCount++
			errorDetails = append(errorDetails, fmt.Sprintf("%s: 保存失败", target))
			continue
		}
		newCount++

		// 记录首次发现历史，确保时间线不为空
		firstFound := model.SnapshotFromAsset(asset, "", time.Now(), nil)
		if histErr := historyModel.Insert(l.ctx, firstFound); histErr != nil {
			l.Logger.Errorf("[AssetImport] insert first-found history failed: %v", histErr)
		}

		// 同步创建/刷新顶层资产 meta，否则顶层资产列表（只读 {ws}_asset_target_meta）不会展示手动导入的资产
		if err := upsertAssetTargetMeta(l.ctx, metaModel, host, "", nil); err != nil {
			l.Logger.Errorf("[AssetImport] upsert target meta host=%s fail: %v", host, err)
		}
		invalidateAssetTargetCaches(l.svcCtx, "")
	}

	if total == 0 {
		return &types.AssetImportResp{Code: 400, Msg: "没有有效的目标"}, nil
	}

	msg := "导入完成"
	if newCount > 0 {
		msg += fmt.Sprintf("，新增 %d 条", newCount)
	}
	if skipCount > 0 {
		msg += fmt.Sprintf("，跳过 %d 条（已存在）", skipCount)
	}
	if errorCount > 0 {
		msg += fmt.Sprintf("，失败 %d 条（格式错误）", errorCount)
		// 最多显示前3个错误详情
		if len(errorDetails) > 0 {
			maxShow := 3
			if len(errorDetails) < maxShow {
				maxShow = len(errorDetails)
			}
			msg += "：" + strings.Join(errorDetails[:maxShow], "；")
			if len(errorDetails) > maxShow {
				msg += fmt.Sprintf("...等%d条", len(errorDetails))
			}
		}
	}

	return &types.AssetImportResp{
		Code:       0,
		Msg:        msg,
		Total:      total,
		NewCount:   newCount,
		SkipCount:  skipCount,
		ErrorCount: errorCount,
	}, nil
}

// AssetSaveLogic 手动添加资产
type AssetSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetSaveLogic {
	return &AssetSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetSaveLogic) AssetSave(req *types.AssetSaveReq) (resp *types.AssetSaveResp, err error) {
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return nil, xerr.NewCodeErrorMsg(xerr.ParamError, "主机不能为空")
	}
	if req.Port <= 0 || req.Port > 65535 {
		return nil, xerr.NewCodeErrorMsg(xerr.ParamError, "端口范围 1-65535")
	}
	if !isValidHost(host) {
		return nil, xerr.NewCodeErrorMsg(xerr.ParamError, "无效的主机名或IP")
	}

	protocol := strings.TrimSpace(strings.ToLower(req.Protocol))
	service := protocol
	isHTTP := protocol == "http" || protocol == "https"

	authority := host + ":" + strconv.Itoa(req.Port)

	asset := &model.Asset{
		Authority: authority,
		Host:      host,
		Port:      req.Port,
		Service:   service,
		IsHTTP:    isHTTP,
		Title:     strings.TrimSpace(req.Title),
		Labels:    req.Labels,
		Memo:      strings.TrimSpace(req.Memo),
		Source:    "manual",
	}

	assetModel := l.svcCtx.GetAssetModel()
	historyModel := l.svcCtx.GetAssetHistoryModel()

	// 预读 existing 以驱动 helper 的 diff / 状态字段门控 / 历史写入
	existing, _ := assetModel.FindByAuthorityOnly(l.ctx, authority)

	opts := model.AssetWriteOptions{
		IsManual:             true,
		TaskId:               "",
		IsDifferentTask:      false,
		AllowClearUserFields: true,
	}
	update, changes := model.BuildAssetUpdateDoc(asset, existing, opts)

	if err := assetModel.UpsertByAuthority(l.ctx, authority, update); err != nil {
		l.Errorf("[AssetSave] upsert failed: %v", err)
		return nil, xerr.NewServerError("保存资产失败")
	}

	// 写入历史：仅当 existing 非空且存在实际变更时
	if existing != nil && len(changes) > 0 {
		history := model.SnapshotFromAsset(existing, existing.TaskId, existing.UpdateTime, changes)
		if err := historyModel.Insert(l.ctx, history); err != nil {
			l.Logger.Errorf("[AssetSave] insert history failed: %v", err)
		}
	} else if existing == nil {
		// 新资产：记录首次发现历史，确保时间线不为空
		newAsset, _ := assetModel.FindByAuthorityOnly(l.ctx, authority)
		if newAsset != nil {
			firstFound := model.SnapshotFromAsset(newAsset, "", time.Now(), nil)
			if err := historyModel.Insert(l.ctx, firstFound); err != nil {
				l.Logger.Errorf("[AssetSave] insert first-found history failed: %v", err)
			}
		}
	}

	// 同步创建/刷新顶层资产 meta，否则顶层资产列表不会展示手动添加的资产
	if err := upsertAssetTargetMeta(l.ctx, l.svcCtx.GetAssetTargetMetaModel(), host, "", req.Labels); err != nil {
		l.Logger.Errorf("[AssetSave] upsert target meta host=%s fail: %v", host, err)
	}
	invalidateAssetTargetCaches(l.svcCtx, "")

	return &types.AssetSaveResp{Code: 0, Msg: "success"}, nil
}

// parseTarget 解析目标字符串，支持 IP:端口、URL、域名 格式
func parseTarget(target string) (host string, port int, scheme string, err error) {
	target = strings.TrimSpace(target)

	if target == "" {
		return "", 0, "", xerr.NewParamError("目标不能为空")
	}

	// 处理 URL 格式
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		// 解析 URL
		if strings.HasPrefix(target, "https://") {
			scheme = "https"
			target = strings.TrimPrefix(target, "https://")
		} else {
			scheme = "http"
			target = strings.TrimPrefix(target, "http://")
		}

		// 去掉路径部分
		if idx := strings.Index(target, "/"); idx > 0 {
			target = target[:idx]
		}

		// 去掉查询参数
		if idx := strings.Index(target, "?"); idx > 0 {
			target = target[:idx]
		}

		if target == "" {
			return "", 0, "", fmt.Errorf("URL格式错误：缺少主机名")
		}

		// 解析 host:port
		if strings.Contains(target, ":") {
			parts := strings.SplitN(target, ":", 2)
			host = parts[0]
			if host == "" {
				return "", 0, "", fmt.Errorf("URL格式错误：主机名为空")
			}
			port, err = strconv.Atoi(parts[1])
			if err != nil {
				return "", 0, "", fmt.Errorf("端口格式错误：%s", parts[1])
			}
		} else {
			host = target
			if scheme == "https" {
				port = 443
			} else {
				port = 80
			}
		}
	} else if strings.Contains(target, ":") {
		// IP:端口 或 域名:端口 格式
		parts := strings.SplitN(target, ":", 2)
		host = parts[0]
		if host == "" {
			return "", 0, "", fmt.Errorf("格式错误：主机名为空")
		}
		port, err = strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, "", fmt.Errorf("端口格式错误：%s", parts[1])
		}
		// 根据端口推断协议
		if port == 443 || port == 8443 {
			scheme = "https"
		} else {
			scheme = "http"
		}
	} else {
		// 只有 host（IP或域名），默认 80 端口
		host = target
		port = 80
		scheme = "http"
	}

	// 校验端口范围
	if port <= 0 || port > 65535 {
		return "", 0, "", fmt.Errorf("端口超出范围(1-65535)：%d", port)
	}

	// 校验主机名格式（IP或域名）
	if !isValidHost(host) {
		return "", 0, "", fmt.Errorf("无效的主机名或IP：%s", host)
	}

	return host, port, scheme, nil
}

// upsertAssetTargetMeta 将手动导入/添加的资产归并到顶层资产 meta 集合。
// 解析与 upsert 共用 model.AssetTargetMetaModel.EnsureForAsset，
// 与扫描结果保存（Worker 直连 MongoDB）保持一致，避免顶层资产列表只见手动新增。
//
// labels 在 Upsert 时按 $set 覆盖；nil 则保留原值。
func upsertAssetTargetMeta(ctx context.Context, metaModel *model.AssetTargetMetaModel, host, domain string, labels []string) error {
	return metaModel.EnsureForAsset(ctx, host, domain, labels)
}

// isValidHost 校验主机名是否为有效的IP或域名
func isValidHost(host string) bool {
	if host == "" {
		return false
	}

	// 检查是否为有效IP
	if net.ParseIP(host) != nil {
		return true
	}

	// 检查是否为有效域名
	// 域名规则：由字母、数字、连字符组成，点分隔，每段不超过63字符
	if len(host) > 253 {
		return false
	}

	// 简单的域名格式校验
	domainRegex := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)
	return domainRegex.MatchString(host)
}
