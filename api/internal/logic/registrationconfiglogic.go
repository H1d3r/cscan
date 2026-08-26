package logic

import (
	"context"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const registrationConfigKey = "registration_config"

// loadRegistrationConfig 读取注册配置；未配置或读取失败时返回默认值（关闭注册 + 需审核）
func loadRegistrationConfig(ctx context.Context, svcCtx *svc.ServiceContext) types.RegistrationConfig {
	collection := svcCtx.MongoClient.Database(svcCtx.Config.Mongo.DbName).Collection("system_config")
	var result struct {
		Config types.RegistrationConfig `bson:"config"`
	}
	if err := collection.FindOne(ctx, bson.M{"key": registrationConfigKey}).Decode(&result); err != nil {
		return types.RegistrationConfig{Enabled: false, RequireApproval: true}
	}
	return result.Config
}

// RegistrationConfigGetLogic 获取注册配置
type RegistrationConfigGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRegistrationConfigGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegistrationConfigGetLogic {
	return &RegistrationConfigGetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RegistrationConfigGetLogic) Get() (*types.RegistrationConfigResp, error) {
	config := loadRegistrationConfig(l.ctx, l.svcCtx)
	if config.UpdateTime == "" {
		config.UpdateTime = time.Now().Format("2006-01-02 15:04:05")
	}

	return &types.RegistrationConfigResp{
		Code:   0,
		Msg:    "success",
		Config: &config,
	}, nil
}

// RegistrationConfigSaveLogic 保存注册配置
type RegistrationConfigSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRegistrationConfigSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegistrationConfigSaveLogic {
	return &RegistrationConfigSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RegistrationConfigSaveLogic) Save(req *types.RegistrationConfigSaveReq) (*types.RegistrationConfigResp, error) {
	config := types.RegistrationConfig{
		Enabled:         req.Enabled,
		RequireApproval: req.RequireApproval,
		UpdateTime:      time.Now().Format("2006-01-02 15:04:05"),
	}

	collection := l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName).Collection("system_config")
	filter := bson.M{"key": registrationConfigKey}
	update := bson.M{
		"$set": bson.M{
			"key":    registrationConfigKey,
			"config": config,
		},
	}
	if _, err := collection.UpdateOne(l.ctx, filter, update, options.Update().SetUpsert(true)); err != nil {
		return nil, xerr.NewServerError("保存注册配置失败: " + err.Error())
	}

	return &types.RegistrationConfigResp{
		Code:   0,
		Msg:    "保存成功",
		Config: &config,
	}, nil
}
