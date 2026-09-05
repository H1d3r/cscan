package logic

import (
	"context"
	"strings"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type AssetTargetAssetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetTargetAssetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetTargetAssetsLogic {
	return &AssetTargetAssetsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetTargetAssets 获取目标下的资产列表（底层 asset 集合，按 host 过滤）
func (l *AssetTargetAssetsLogic) AssetTargetAssets(req *types.AssetTargetAssetsReq) (*types.AssetTargetAssetsResp, error) {
	targetId := req.TargetId
	if targetId == "" {
		return nil, xerr.NewParamError("targetId is empty")
	}

	tType, tValue, err := model.DecodeTargetID(targetId)
	if err != nil {
		return nil, err
	}

	assetModel := l.svcCtx.GetAssetModel()
	if assetModel == nil {
		return nil, xerr.NewServerError("asset model not available")
	}

	filter := bson.M{
		"host": hostFilterForTarget(tType, tValue),
		// 端口 0 是无端口资产（子域名发现阶段的占位记录），服务列表只展示有真实端口的服务
		"port": bson.M{"$gt": 0},
	}
	if err := applyAssetTargetFilters(filter, req.Query, req.Ports, req.StatusCodes, req.Technologies, req.Labels); err != nil {
		return nil, err
	}

	page, pageSize := normalizeListPage(req.Page, req.PageSize)
	req.Page, req.PageSize = page, pageSize

	total, err := assetModel.Count(l.ctx, filter)
	if err != nil {
		l.Logger.Errorf("[AssetTargetAssets] Count fail: %v", err)
		return nil, xerr.NewServerError("")
	}

	cursor, err := assetModel.FindForTargetInventory(l.ctx, filter, page, pageSize)
	if err != nil {
		l.Logger.Errorf("[AssetTargetAssets] Find fail: %v", err)
		return nil, xerr.NewServerError("")
	}

	items := make([]types.AssetTargetAssetItem, 0, len(cursor))
	for _, a := range cursor {
		item := types.AssetTargetAssetItem{
			Id:         a.Id.Hex(),
			Host:       a.Host,
			Port:       a.Port,
			Service:    a.Service,
			StatusCode: a.HttpStatus,
			Title:      a.Title,
			// Screenshot/IconBase64 不在列表响应中携带（单页 20 条可达数百 KB），
			// 前端拿到列表后走 /asset/media 按 id 懒加载
			// 同一技术的来源后缀变体（"Nginx[httpx]" vs "Nginx[custom(id)]"）折叠为一条展示
			Tech:       model.MergeAppsDedup(nil, a.App),
			Labels:     a.Labels,
			IsHTTP:     a.IsHTTP,
			Ips:        ipv4List(a),
			Server:     a.Server,
			Cname:      a.CName,
			Domain:     a.Domain,
			IconHash:   a.IconHash,
			CreateTime: a.CreateTime.UnixMilli(),
			UpdateTime: a.UpdateTime.UnixMilli(),
		}
		items = append(items, item)
	}

	return &types.AssetTargetAssetsResp{
		Code: 0,
		Msg:  "success",
		Data: types.AssetTargetAssetsData{
			List:     items,
			Page:     page,
			PageSize: pageSize,
			Total:    total,
		},
	}, nil
}

// ipv4List 从 asset 内嵌 ip 结构提取 IPv4 列表。
func ipv4List(a model.Asset) []string {
	if len(a.Ip.IpV4) == 0 {
		return nil
	}
	ips := make([]string, 0, len(a.Ip.IpV4))
	for _, e := range a.Ip.IpV4 {
		if e.IPName != "" {
			ips = append(ips, e.IPName)
		}
	}
	if len(ips) == 0 {
		return nil
	}
	return ips
}

// applyAssetTargetFilters 把目标资产子页的过滤条件（query/ports/statusCodes/technologies/labels）
// 合并进 filter（原地修改），供 services 列表与 groups 聚合共用。
func applyAssetTargetFilters(filter bson.M, query string, ports []int, statusCodes []string, technologies []string, labels []string) error {
	query = strings.TrimSpace(query)
	if query != "" {
		pattern := ".*" + regexpEscape(query) + ".*"
		filter["$or"] = bson.A{
			bson.M{"host": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"title": bson.M{"$regex": pattern, "$options": "i"}},
		}
	}
	if len(ports) > 0 {
		positive := make([]int, 0, len(ports))
		for _, p := range ports {
			if p > 0 {
				positive = append(positive, p)
			}
		}
		// 全部是非正端口时不设置 $in，保留调用方已有的 port 过滤（如服务列表的 port>0）
		if len(positive) > 0 {
			filter["port"] = bson.M{"$in": positive}
		}
	}
	if len(statusCodes) > 0 {
		filter["status"] = bson.M{"$in": statusCodes}
	}
	if len(technologies) > 0 {
		and := make(bson.A, 0, len(technologies))
		for _, tech := range technologies {
			tech = strings.TrimSpace(tech)
			if tech == "" {
				continue
			}
			and = append(and, bson.M{"app": bson.M{"$regex": regexpEscape(tech), "$options": "i"}})
		}
		if len(and) > 0 {
			filter["$and"] = and
		}
	}
	if len(labels) > 0 {
		// 与 /asset/inventory 的标签过滤口径一致：命中任一标签即匹配
		filter["labels"] = bson.M{"$in": labels}
	}
	return nil
}
