package logic

import (
	"context"
	"fmt"
	"strings"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type AssetTargetGroupsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetTargetGroupsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetTargetGroupsLogic {
	return &AssetTargetGroupsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetTargetGroups 目标或全局资产按维度聚合（host/port/ip/app/status），
// 支撑统一 Inventory 的 Hosts/Ports/IP Addresses/Technologies/Status Code 子 Tab。
func (l *AssetTargetGroupsLogic) AssetTargetGroups(req *types.AssetTargetGroupsReq) (*types.AssetTargetGroupsResp, error) {
	match := bson.M{}
	if strings.TrimSpace(req.TargetId) != "" {
		tType, tValue, err := model.DecodeTargetID(req.TargetId)
		if err != nil {
			return nil, err
		}
		match["host"] = hostFilterForTarget(tType, tValue)
	}

	assetModel := l.svcCtx.GetAssetModel()
	if assetModel == nil {
		return nil, xerr.NewServerError("asset model not available")
	}

	if err := applyAssetTargetFilters(match, req.Query, req.Ports, req.StatusCodes, req.Technologies, req.Labels); err != nil {
		return nil, err
	}
	req.Page, req.PageSize = normalizeListPage(req.Page, req.PageSize)

	pipeline, err := buildTargetGroupPipeline(req.GroupBy, match, req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}

	rows, total, err := assetModel.AggregateTargetGroups(l.ctx, pipeline)
	if err != nil {
		l.Logger.Errorf("[AssetTargetGroups] aggregate groupBy=%s fail: %v", req.GroupBy, err)
		return nil, xerr.NewServerError("")
	}

	list := make([]types.AssetTargetGroupItem, 0, len(rows))
	for _, row := range rows {
		key := fmt.Sprintf("%v", row.Id)
		if key == "" || key == "<nil>" {
			continue
		}
		// app 分组以归一化键为 _id，展示改用组内原始条目保留大小写
		if strings.TrimSpace(req.GroupBy) == "app" && row.Name != "" {
			key = row.Name
		}
		item := types.AssetTargetGroupItem{Key: key, Count: row.Count, Location: row.Location, Extras: row.Extras, Labels: row.Labels}
		list = append(list, item)
	}
	return &types.AssetTargetGroupsResp{
		Code: 0, Msg: "success", Page: req.Page, PageSize: req.PageSize, Total: total, List: list,
	}, nil
}

// buildTargetGroupPipeline 构造聚合 pipeline：
//   - host:   group by host，count = 服务数
//   - port:   group by port，extras = distinct service
//   - ip:     unwind ip.ipv4 后 group by ip，location = 归属地
//   - app:    unwind app 后 group by 技术名
//   - status: group by HTTP 状态码
//
// 所有分组统一收集组内资产标签并集（labels），供子 Tab 标签列展示；
// 用 $push + $setUnion 归并，避免 $unwind labels 拉高 count。
func buildTargetGroupPipeline(groupBy string, match bson.M, page, pageSize int) ([]bson.M, error) {
	var group bson.M
	pipeline := []bson.M{{"$match": match}}

	switch strings.TrimSpace(groupBy) {
	case "host":
		// 与服务列表同口径：端口 0 是无端口占位记录（纯域名），不计入主机服务数。
		if _, ok := match["port"]; !ok {
			match["port"] = bson.M{"$gt": 0}
		}
		pipeline = append(pipeline, bson.M{"$match": bson.M{"host": bson.M{"$nin": bson.A{nil, ""}}}})
		group = bson.M{
			"_id":   "$host",
			"count": bson.M{"$sum": 1},
		}
	case "port":
		// 端口 0 是无端口资产（纯域名/协议探测残留），不参与端口分组
		if _, ok := match["port"]; !ok {
			match["port"] = bson.M{"$gt": 0}
		}
		group = bson.M{
			"_id":   "$port",
			"count": bson.M{"$sum": 1},
			"extras": bson.M{
				"$addToSet": bson.M{"$cond": bson.A{
					bson.M{"$gt": bson.A{"$service", nil}},
					"$service", "$$REMOVE",
				}},
			},
		}
	case "ip":
		pipeline = append(pipeline,
			bson.M{"$unwind": "$ip.ipv4"},
			bson.M{"$match": bson.M{"ip.ipv4.ip": bson.M{"$nin": bson.A{nil, ""}}}},
		)
		group = bson.M{
			"_id":   "$ip.ipv4.ip",
			"count": bson.M{"$sum": 1},
			"location": bson.M{
				"$first": bson.M{"$cond": bson.A{
					bson.M{"$gt": bson.A{"$ip.ipv4.location", nil}},
					"$ip.ipv4.location", "",
				}},
			},
		}
	case "app":
		// 归一化技术名分组：剥掉 [来源] 后缀与 :版本号、忽略大小写后再分组，
		// 避免同一技术因来源后缀/版本不同（"Nginx[httpx]" vs "Nginx:1.18[custom(id)]"）拆成多组。
		// 先按 (资产, 归一化键) 折叠变体，保证 count = 含该技术的资产数而非变体计数；
		// 展示名取组内首个原始条目，前端 getTechName 会再做同样的归一化展示。
		pipeline = append(pipeline,
			bson.M{"$unwind": "$app"},
			bson.M{"$match": bson.M{"app": bson.M{"$nin": bson.A{nil, ""}}}},
			bson.M{"$addFields": bson.M{"appKey": bson.M{"$toLower": bson.M{"$trim": bson.M{"input": bson.M{
				"$arrayElemAt": bson.A{
					bson.M{"$split": bson.A{
						bson.M{"$arrayElemAt": bson.A{bson.M{"$split": bson.A{"$app", "["}}, 0}},
						":",
					}},
					0,
				},
			}}}}}},
			bson.M{"$group": bson.M{
				"_id":    bson.M{"asset": "$_id", "key": "$appKey"},
				"app":    bson.M{"$first": "$app"},
				"labels": bson.M{"$first": bson.M{"$ifNull": bson.A{"$labels", bson.A{}}}},
			}},
		)
		group = bson.M{
			"_id":   "$_id.key",
			"count": bson.M{"$sum": 1},
			"name":  bson.M{"$first": "$app"},
		}
	case "status":
		// 过滤无效状态码（缺失 / 空 / "0"），只保留真实 HTTP 状态码分组
		if _, ok := match["status"]; !ok {
			match["status"] = bson.M{"$nin": bson.A{nil, "", "0"}}
		}
		group = bson.M{
			"_id":   "$status",
			"count": bson.M{"$sum": 1},
		}
	default:
		return nil, xerr.NewParamError(fmt.Sprintf("invalid groupBy %q", groupBy))
	}

	group["allLabels"] = bson.M{"$push": bson.M{"$ifNull": bson.A{"$labels", bson.A{}}}}

	pipeline = append(pipeline,
		bson.M{"$group": group},
		// allLabels 是数组的数组，逐层 $setUnion 归并为去重标签并集
		bson.M{"$addFields": bson.M{"labels": bson.M{"$reduce": bson.M{
			"input":        "$allLabels",
			"initialValue": bson.A{},
			"in":           bson.M{"$setUnion": bson.A{"$$value", "$$this"}},
		}}}},
		bson.M{"$sort": bson.D{{Key: "count", Value: -1}, {Key: "_id", Value: 1}}},
		bson.M{"$facet": bson.M{
			"total": bson.A{bson.M{"$count": "count"}},
			"data": bson.A{
				bson.M{"$skip": int64((page - 1) * pageSize)},
				bson.M{"$limit": int64(pageSize)},
			},
		}},
	)
	return pipeline, nil
}
