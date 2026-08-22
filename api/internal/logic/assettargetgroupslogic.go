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

const assetTargetGroupsLimit = 500

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

// AssetTargetGroups 目标资产按维度聚合（host/port/ip/app/status），
// 支撑详情页 Inventory 的 Hosts/Ports/IP Addresses/Technologies/Status Code 子 Tab。
func (l *AssetTargetGroupsLogic) AssetTargetGroups(req *types.AssetTargetGroupsReq) (*types.AssetTargetGroupsResp, error) {
	if req.TargetId == "" {
		return nil, xerr.NewParamError("targetId is empty")
	}
	tType, tValue, err := model.DecodeTargetID(req.TargetId)
	if err != nil {
		return nil, err
	}

	assetModel := l.svcCtx.GetAssetModel()
	if assetModel == nil {
		return nil, xerr.NewServerError("asset model not available")
	}

	match := bson.M{"host": hostFilterForTarget(tType, tValue)}
	if err := applyAssetTargetFilters(match, req.Query, req.Ports, req.StatusCodes, req.Technologies, req.Labels); err != nil {
		return nil, err
	}

	pipeline, err := buildTargetGroupPipeline(req.GroupBy, match)
	if err != nil {
		return nil, err
	}

	rows, err := assetModel.AggregateTargetGroups(l.ctx, pipeline)
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
		item := types.AssetTargetGroupItem{Key: key, Count: row.Count, Location: row.Location, Extras: row.Extras, Labels: row.Labels}
		list = append(list, item)
	}
	return &types.AssetTargetGroupsResp{Code: 0, Msg: "success", List: list}, nil
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
func buildTargetGroupPipeline(groupBy string, match bson.M) ([]bson.M, error) {
	var group bson.M
	pipeline := []bson.M{{"$match": match}}

	switch strings.TrimSpace(groupBy) {
	case "host":
		// 与服务列表同口径：端口 0 是无端口占位记录（纯域名），不计入主机服务数，
		// 否则主机 Tab 显示的服务数比服务 Tab 实际行数多
		if _, ok := match["port"]; !ok {
			match["port"] = bson.M{"$gt": 0}
		}
		group = bson.M{
			"_id": "$host",
			"count": bson.M{"$sum": 1},
		}
	case "port":
		// 端口 0 是无端口资产（纯域名/协议探测残留），不参与端口分组
		if _, ok := match["port"]; !ok {
			match["port"] = bson.M{"$gt": 0}
		}
		group = bson.M{
			"_id": "$port",
			"count": bson.M{"$sum": 1},
			"extras": bson.M{
				"$addToSet": bson.M{"$cond": bson.A{
					bson.M{"$gt": bson.A{"$service", nil}},
					"$service", "$$REMOVE",
				}},
			},
		}
	case "ip":
		pipeline = append(pipeline, bson.M{"$unwind": "$ip.ipv4"})
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
		pipeline = append(pipeline, bson.M{"$unwind": "$app"})
		group = bson.M{
			"_id":   "$app",
			"count": bson.M{"$sum": 1},
		}
	case "status":
		// 过滤无效状态码（缺失 / 空 / "0"），只保留真实 HTTP 状态码分组
		if _, ok := match["status"]; !ok {
			match["status"] = bson.M{"$nin": bson.A{nil, "", "0"}}
		}
		group = bson.M{
			"_id": "$status",
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
		bson.M{"$sort": bson.M{"count": -1, "_id": 1}},
		bson.M{"$limit": assetTargetGroupsLimit},
	)
	return pipeline, nil
}
