package logic

import (
	"context"

	"cscan/api/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const firstDeployCompletedKey = "first_deploy_completed"

// SystemStatusResp 系统状态响应
type SystemStatusResp struct {
	Code            int    `json:"code"`
	Msg             string `json:"msg"`
	HasUsers        bool   `json:"hasUsers"`        // 是否已有用户（false=首次部署）
	IsFirstDeploy   bool   `json:"isFirstDeploy"`   // 是否为首次部署
	RegisterEnabled bool   `json:"registerEnabled"` // 注册功能是否开放
}

// SystemStatusLogic 系统状态检测逻辑
type SystemStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSystemStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SystemStatusLogic {
	return &SystemStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Check 检查系统是否已有用户（公开接口，无需认证）
func (l *SystemStatusLogic) Check() (*SystemStatusResp, error) {
	// 注册开关：登录页据此决定是否展示注册入口
	registerEnabled := loadRegistrationConfig(l.ctx, l.svcCtx).Enabled

	// 先检查是否有首装完成标记
	collection := l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName).Collection("system_config")
	var flagDoc struct {
		Value bool `bson:"value"`
	}
	err := collection.FindOne(l.ctx, bson.M{"key": firstDeployCompletedKey}).Decode(&flagDoc)
	if err == nil && flagDoc.Value {
		// 已完成首装，恒返回 hasUsers=true
		return &SystemStatusResp{
			Code:            0,
			Msg:             "success",
			HasUsers:        true,
			IsFirstDeploy:   false,
			RegisterEnabled: registerEnabled,
		}, nil
	}

	// 未标记，查询用户数
	count, err := l.svcCtx.UserModel.Count(l.ctx, bson.M{})
	if err != nil {
		logx.Errorf("[SystemStatus] count users failed: %v", err)
		return &SystemStatusResp{Code: 500, Msg: "系统错误"}, nil
	}

	hasUsers := count > 0
	if hasUsers {
		// 写入首装完成标记（upsert，文档不存在时创建），后续请求免全表 Count
		_, err := collection.UpdateOne(l.ctx,
			bson.M{"key": firstDeployCompletedKey},
			bson.M{"$set": bson.M{"key": firstDeployCompletedKey, "value": true}},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			logx.Errorf("[SystemStatus] mark first deploy completed failed: %v", err)
		}
	}

	return &SystemStatusResp{
		Code:          0,
		Msg:           "success",
		HasUsers:      hasUsers,
		IsFirstDeploy: !hasUsers,
		// 首装无用户时注册必然可用（首位 superadmin 创建不受开关限制）
		RegisterEnabled: hasUsers == false || registerEnabled,
	}, nil
}
