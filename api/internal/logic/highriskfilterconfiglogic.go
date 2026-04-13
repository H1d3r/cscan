package logic

import (
	"context"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const highRiskFilterConfigKey = "high_risk_filter_config"

// HighRiskFilterConfigGetLogic 获取高危过滤配置
type HighRiskFilterConfigGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHighRiskFilterConfigGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HighRiskFilterConfigGetLogic {
	return &HighRiskFilterConfigGetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *HighRiskFilterConfigGetLogic) HighRiskFilterConfigGet() (*types.HighRiskFilterConfigResp, error) {
	// 从系统配置集合中获取高危过滤配置
	collection := l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName).Collection("system_config")

	var result struct {
		Key    string                     `bson:"key"`
		Config types.HighRiskFilterConfig `bson:"config"`
	}

	err := collection.FindOne(l.ctx, bson.M{"key": highRiskFilterConfigKey}).Decode(&result)
	if err != nil {
		// 如果没有配置，返回默认配置
		return &types.HighRiskFilterConfigResp{
			Code: 0,
			Msg:  "success",
			Config: &types.HighRiskFilterConfig{
				Enabled:               false,
				HighRiskFingerprints:  []string{},
				HighRiskPorts:         []int{},
				HighRiskPocSeverities: []string{},
				NewAssetNotify:        false,
			},
		}, nil
	}

	return &types.HighRiskFilterConfigResp{
		Code:   0,
		Msg:    "success",
		Config: &result.Config,
	}, nil
}

// HighRiskFilterConfigSaveLogic 保存高危过滤配置
type HighRiskFilterConfigSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHighRiskFilterConfigSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HighRiskFilterConfigSaveLogic {
	return &HighRiskFilterConfigSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *HighRiskFilterConfigSaveLogic) HighRiskFilterConfigSave(req *types.HighRiskFilterConfigSaveReq) (*types.HighRiskFilterConfigResp, error) {
	collection := l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName).Collection("system_config")

	config := types.HighRiskFilterConfig{
		Enabled:               req.Enabled,
		HighRiskFingerprints:  req.HighRiskFingerprints,
		HighRiskPorts:         req.HighRiskPorts,
		HighRiskPocSeverities: req.HighRiskPocSeverities,
		NewAssetNotify:        req.NewAssetNotify,
		UpdateTime:            time.Now().Format("2006-01-02 15:04:05"),
	}

	// 确保数组不为nil
	if config.HighRiskFingerprints == nil {
		config.HighRiskFingerprints = []string{}
	}
	if config.HighRiskPorts == nil {
		config.HighRiskPorts = []int{}
	}
	if config.HighRiskPocSeverities == nil {
		config.HighRiskPocSeverities = []string{}
	}

	// 使用upsert更新或插入配置
	filter := bson.M{"key": highRiskFilterConfigKey}
	update := bson.M{
		"$set": bson.M{
			"key":    highRiskFilterConfigKey,
			"config": config,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := collection.UpdateOne(l.ctx, filter, update, opts)
	if err != nil {
		return &types.HighRiskFilterConfigResp{
			Code: 500,
			Msg:  "保存失败: " + err.Error(),
		}, nil
	}

	return &types.HighRiskFilterConfigResp{
		Code:   0,
		Msg:    "保存成功",
		Config: &config,
	}, nil
}
