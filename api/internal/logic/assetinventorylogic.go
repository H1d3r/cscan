package logic

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AssetInventoryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetInventoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetInventoryLogic {
	return &AssetInventoryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// buildInventoryFilter 构建资产清单的查询条件
func (l *AssetInventoryLogic) buildInventoryFilter(req *types.AssetInventoryReq) bson.M {
	filter := bson.M{}

	appendAndFilter := func(condition bson.M) {
		if existingAnd, ok := filter["$and"]; ok {
			filter["$and"] = append(existingAnd.([]bson.M), condition)
			return
		}
		filter["$and"] = []bson.M{condition}
	}

	// 搜索关键词
	if req.Query != "" {
		q := regexp.QuoteMeta(req.Query)
		filter["$or"] = []bson.M{
			{"host": bson.M{"$regex": q, "$options": "i"}},
			{"title": bson.M{"$regex": q, "$options": "i"}},
			{"domain": bson.M{"$regex": q, "$options": "i"}},
			{"ip.ipv4.ip": bson.M{"$regex": q, "$options": "i"}},
			{"ip.ipv6.ip": bson.M{"$regex": q, "$options": "i"}},
		}
	}

	// 域名过滤
	if req.Domain != "" {
		filter["host"] = bson.M{"$regex": regexp.QuoteMeta(req.Domain), "$options": "i"}
	}

	// 端口过滤
	if len(req.Ports) > 0 {
		filter["port"] = bson.M{"$in": req.Ports}
	}

	// 状态码过滤
	if len(req.StatusCodes) > 0 {
		filter["status"] = bson.M{"$in": req.StatusCodes}
	}

	// 标签过滤
	if len(req.Labels) > 0 {
		filter["labels"] = bson.M{"$in": req.Labels}
	}

	// 服务类型过滤
	if req.Service != "" {
		filter["service"] = bson.M{"$regex": regexp.QuoteMeta(req.Service), "$options": "i"}
	}

	// IconHash 过滤
	if req.IconHash != "" {
		filter["icon_hash"] = req.IconHash
	}

	// 技术栈过滤
	if len(req.Technologies) > 0 {
		techFilters := make([]bson.M, 0, len(req.Technologies))
		for _, tech := range req.Technologies {
			escapedTech := regexp.QuoteMeta(tech)
			techFilters = append(techFilters, bson.M{
				"app": bson.M{"$regex": escapedTech, "$options": "i"},
			})
		}
		if len(techFilters) > 0 {
			if existingOr, ok := filter["$or"]; ok {
				appendAndFilter(bson.M{"$or": existingOr})
				appendAndFilter(bson.M{"$or": techFilters})
				delete(filter, "$or")
			} else {
				filter["$or"] = techFilters
			}
		}
	}

	// 时间范围过滤
	if req.TimeRange != "" && req.TimeRange != "all" {
		now := time.Now()
		var startTime time.Time
		switch req.TimeRange {
		case "24h":
			startTime = now.Add(-24 * time.Hour)
		case "7d":
			startTime = now.Add(-7 * 24 * time.Hour)
		case "30d":
			startTime = now.Add(-30 * 24 * time.Hour)
		}
		if !startTime.IsZero() {
			filter["update_time"] = bson.M{"$gte": startTime}
		}
	}

	// 仅显示已识别（有指纹/技术栈）或已截图（有截图/图标）的资产
	if req.RequireRecognitionOrShot {
		appendAndFilter(bson.M{"$or": []bson.M{
			{"screenshot": bson.M{"$ne": ""}},
			{"icon_hash": bson.M{"$ne": ""}},
			{"fingerprints": bson.M{"$exists": true, "$ne": bson.M{}}},
			{"app": bson.M{"$exists": true, "$ne": bson.M{}}},
		}})
	}

	if req.WebOnly {
		appendAndFilter(bson.M{"$or": bson.A{
			bson.M{"is_http": true},
			bson.M{"service": bson.M{"$in": bson.A{"http", "https"}}},
			bson.M{"title": bson.M{"$exists": true, "$ne": ""}},
			bson.M{"screenshot": bson.M{"$exists": true, "$ne": ""}},
		}})
		appendAndFilter(bson.M{"port": bson.M{"$gt": 0}})
	}
	if req.HasIcon {
		appendAndFilter(bson.M{"icon_hash": bson.M{"$exists": true, "$nin": bson.A{"", nil}}})
	}
	if req.HasScreenshot {
		appendAndFilter(bson.M{"screenshot": bson.M{"$exists": true, "$nin": bson.A{"", nil}}})
	}

	return filter
}

// convertAssetToInventoryItem 将 Asset 模型转换为清单展示项
func convertAssetToInventoryItem(asset model.Asset) types.AssetInventoryItem {
	ip := ""
	var ips []string
	if len(asset.Ip.IpV4) > 0 {
		ip = asset.Ip.IpV4[0].IPName
	} else if len(asset.Ip.IpV6) > 0 {
		ip = asset.Ip.IpV6[0].IPName
	}
	for _, v4 := range asset.Ip.IpV4 {
		ips = append(ips, v4.IPName)
	}
	for _, v6 := range asset.Ip.IpV6 {
		ips = append(ips, v6.IPName)
	}

	iconHashBytes := ""
	if len(asset.IconHashBytes) > 0 && isValidImageBytes(asset.IconHashBytes) {
		iconHashBytes = base64.StdEncoding.EncodeToString(asset.IconHashBytes)
	}

	labels := asset.Labels
	if labels == nil {
		labels = []string{}
	}

	return types.AssetInventoryItem{
		Id:              asset.Id.Hex(),
		Host:            asset.Host,
		IP:              ip,
		Ips:             ips,
		Port:            asset.Port,
		Service:         asset.Service,
		Title:           asset.Title,
		Technologies:    asset.App,
		Labels:          labels,
		Status:          asset.HttpStatus,
		Domain:          asset.Domain,
		LastUpdated:     formatTimeAgo(asset.UpdateTime),
		FirstSeen:       asset.CreateTime.Local().Format("2006-01-02 15:04:05"),
		LastUpdatedFull: asset.UpdateTime.Local().Format("2006-01-02 15:04:05"),
		Screenshot:      asset.Screenshot,
		IconHash:        asset.IconHash,
		IconHashBytes:   iconHashBytes,
		HttpHeader:      asset.HttpHeader,
		HttpBody:        asset.HttpBody,
		Banner:          asset.Banner,
		CName:           asset.CName,
	}
}

// AssetInventory 获取资产清单
func (l *AssetInventoryLogic) AssetInventory(req *types.AssetInventoryReq) (resp *types.AssetInventoryResp, err error) {
	req.Page, req.PageSize = normalizeListPage(req.Page, req.PageSize)

	filter := l.buildInventoryFilter(req)
	if req.TargetId != "" {
		targetType, targetValue, decodeErr := model.DecodeTargetID(req.TargetId)
		if decodeErr != nil {
			return nil, decodeErr
		}
		appendBSONAndCondition(filter, bson.M{"host": hostFilterForTarget(targetType, targetValue)})
	}
	sortField := inventorySortField(req.SortBy)
	skip := int64((req.Page - 1) * req.PageSize)
	limit := int64(req.PageSize)

	assetModel := l.svcCtx.GetAssetModel()
	total, assets, err := assetModel.AggregateInventoryPaged(l.ctx, filter, skip, limit, sortField)
	if err != nil {
		l.Logger.Errorf("[AssetInventory] AggregateInventoryPaged 失败: %v", err)
		return &types.AssetInventoryResp{Code: 500, Msg: "查询失败"}, nil
	}

	resultItems := make([]types.AssetInventoryItem, 0, len(assets))
	for _, asset := range assets {
		resultItems = append(resultItems, convertAssetToInventoryItem(asset))
	}
	l.attachInventoryTLSSummaries(resultItems)

	return &types.AssetInventoryResp{
		Code:     0,
		Msg:      "success",
		Page:     req.Page,
		PageSize: req.PageSize,
		Total:    int(total),
		List:     resultItems,
	}, nil
}

// attachInventoryTLSSummaries 批量为当前页服务关联最新证书，避免逐行查询证书集合。
func (l *AssetInventoryLogic) attachInventoryTLSSummaries(items []types.AssetInventoryItem) {
	if len(items) == 0 {
		return
	}

	endpointFilters := make(bson.A, 0, len(items))
	seenEndpoints := make(map[string]struct{}, len(items))
	for _, item := range items {
		host := strings.TrimSpace(item.Host)
		if host == "" || item.Port <= 0 {
			continue
		}
		key := inventoryTLSKey(host, item.Port)
		if _, exists := seenEndpoints[key]; exists {
			continue
		}
		seenEndpoints[key] = struct{}{}
		endpointFilters = append(endpointFilters, bson.M{"host": host, "port": item.Port})
	}
	if len(endpointFilters) == 0 {
		return
	}

	certModel := l.svcCtx.GetCertModel()
	if certModel == nil {
		return
	}
	certs, err := certModel.Find(l.ctx, bson.M{"$or": endpointFilters}, options.Find().SetSort(bson.D{
		{Key: "update_time", Value: -1},
		{Key: "_id", Value: -1},
	}))
	if err != nil {
		l.Logger.Errorf("[AssetInventory] 查询当前页 TLS 证书失败: %v", err)
		return
	}

	now := time.Now()
	summaries := make(map[string]*types.AssetInventoryTLSSummary, len(certs))
	for _, cert := range certs {
		key := inventoryTLSKey(cert.Host, cert.Port)
		if _, exists := summaries[key]; exists {
			continue
		}
		notAfter := int64(0)
		if !cert.NotAfter.IsZero() {
			notAfter = cert.NotAfter.UnixMilli()
		}
		summaries[key] = &types.AssetInventoryTLSSummary{
			SubjectCN:  cert.Subject.CommonName,
			IssuerOrg:  cert.Issuer.Organization,
			NotAfter:   notAfter,
			Status:     inventoryTLSStatus(cert.NotAfter, now),
			SelfSigned: cert.IsSelfSigned,
		}
	}

	for index := range items {
		items[index].TLS = summaries[inventoryTLSKey(items[index].Host, items[index].Port)]
	}
}

func inventoryTLSKey(host string, port int) string {
	return strings.ToLower(strings.TrimSpace(host)) + "\x00" + fmt.Sprintf("%d", port)
}

func inventoryTLSStatus(notAfter, now time.Time) string {
	switch {
	case !notAfter.IsZero() && notAfter.Before(now):
		return "expired"
	case !notAfter.IsZero() && notAfter.Before(now.Add(30*24*time.Hour)):
		return "expiring"
	default:
		return "valid"
	}
}

// inventorySortField 将前端排序参数转为 MongoDB 排序字段
// 返回值带 "-" 前缀表示降序，无前缀表示升序
func inventorySortField(sortBy string) string {
	switch sortBy {
	case "name", "name-asc":
		return "host"
	case "name-desc":
		return "-host"
	case "time-asc":
		return "update_time"
	case "port":
		return "port"
	default: // "time", "time-desc", ""
		return "-update_time"
	}
}

// formatTimeAgo 格式化相对时间
func formatTimeAgo(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "刚刚"
	} else if diff < time.Hour {
		return fmt.Sprintf("%d分钟前", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		return fmt.Sprintf("%d小时前", int(diff.Hours()))
	} else {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1天前"
		}
		return fmt.Sprintf("%d天前", days)
	}
}
