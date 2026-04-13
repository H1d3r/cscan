package logic

import (
	"context"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// AssetFingerprintsListLogic 资产指纹列表
type AssetFingerprintsListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetFingerprintsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetFingerprintsListLogic {
	return &AssetFingerprintsListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetFingerprintsListLogic) AssetFingerprintsList(req *types.AssetFingerprintsListReq) (*types.AssetFingerprintsListResp, error) {
	// 从资产集合中聚合获取所有不重复的指纹
	collection := l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName).Collection("assets")

	// 使用distinct获取所有不重复的指纹
	fingerprints, err := collection.Distinct(l.ctx, "fingerprints", bson.M{})
	if err != nil {
		l.Logger.Errorf("获取指纹列表失败: %v", err)
		return &types.AssetFingerprintsListResp{
			Code: 500,
			Msg:  "获取指纹列表失败",
			List: []string{},
		}, nil
	}

	// 转换为字符串列表
	result := make([]string, 0, len(fingerprints))
	for _, fp := range fingerprints {
		if s, ok := fp.(string); ok && s != "" {
			result = append(result, s)
		}
	}

	// 限制返回数量
	if req.Limit > 0 && len(result) > req.Limit {
		result = result[:req.Limit]
	}

	return &types.AssetFingerprintsListResp{
		Code: 0,
		Msg:  "success",
		List: result,
	}, nil
}

// AssetPortsStatsLogic 资产端口统计
type AssetPortsStatsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetPortsStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetPortsStatsLogic {
	return &AssetPortsStatsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetPortsStatsLogic) AssetPortsStats() (*types.AssetPortsStatsResp, error) {
	// 从资产集合中聚合端口统计
	collection := l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName).Collection("assets")

	// 聚合管道：按端口分组统计
	pipeline := []bson.M{
		{"$match": bson.M{"port": bson.M{"$gt": 0}}},
		{"$group": bson.M{
			"_id":     "$port",
			"service": bson.M{"$first": "$service"},
			"count":   bson.M{"$sum": 1},
		}},
		{"$sort": bson.M{"count": -1}},
		{"$limit": 200},
	}

	cursor, err := collection.Aggregate(l.ctx, pipeline)
	if err != nil {
		l.Logger.Errorf("获取端口统计失败: %v", err)
		return &types.AssetPortsStatsResp{
			Code: 500,
			Msg:  "获取端口统计失败",
			List: []types.PortStatItem{},
		}, nil
	}
	defer cursor.Close(l.ctx)

	var results []struct {
		Port    int    `bson:"_id"`
		Service string `bson:"service"`
		Count   int64  `bson:"count"`
	}

	if err := cursor.All(l.ctx, &results); err != nil {
		l.Logger.Errorf("解析端口统计失败: %v", err)
		return &types.AssetPortsStatsResp{
			Code: 500,
			Msg:  "解析端口统计失败",
			List: []types.PortStatItem{},
		}, nil
	}

	list := make([]types.PortStatItem, 0, len(results))
	for _, r := range results {
		list = append(list, types.PortStatItem{
			Port:    r.Port,
			Service: r.Service,
			Count:   r.Count,
		})
	}

	return &types.AssetPortsStatsResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}
