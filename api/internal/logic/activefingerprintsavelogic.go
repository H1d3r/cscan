package logic

import (
	"context"
	"errors"
	"fmt"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ActiveFingerprintSaveLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewActiveFingerprintSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActiveFingerprintSaveLogic {
	return &ActiveFingerprintSaveLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActiveFingerprintSaveLogic) ActiveFingerprintSave(req *types.ActiveFingerprintSaveReq) (*types.BaseResp, error) {
	if req.Name == "" {
		return nil, errors.New("应用名称不能为空")
	}
	if len(req.Paths) == 0 {
		return nil, errors.New("探测路径不能为空")
	}

	doc := &model.ActiveFingerprint{
		Name:        req.Name,
		Paths:       req.Paths,
		Description: req.Description,
		Enabled:     req.Enabled,
	}

	if req.Id != "" {
		// 更新
		oid, err := primitive.ObjectIDFromHex(req.Id)
		if err != nil {
			return nil, errors.New("无效的ID")
		}
		doc.Id = oid
		err = l.svcCtx.ActiveFingerprintModel.Update(l.ctx, req.Id, bson.M{
			"name":        req.Name,
			"paths":       req.Paths,
			"description": req.Description,
			"enabled":     req.Enabled,
		})
		if err != nil {
			return nil, err
		}
	} else {
		// 新增（使用Upsert避免重复）
		err := l.svcCtx.ActiveFingerprintModel.Upsert(l.ctx, doc)
		if err != nil {
			return nil, err
		}
	}

	msg := "保存成功"
	// 主动指纹的验证与扫描匹配都依赖同名启用的被动指纹规则，
	// 缺失时验证会直接报"未找到同名被动指纹"，这里提前给出警告
	if related, err := l.svcCtx.FingerprintModel.FindByNames(l.ctx, []string{req.Name}); err == nil && len(related) == 0 {
		msg = fmt.Sprintf("保存成功。警告：不存在名为「%s」的启用被动指纹，主动指纹验证与扫描匹配将无法命中，请先在被动指纹中创建同名规则", req.Name)
	}

	return &types.BaseResp{
		Code: 0,
		Msg:  msg,
	}, nil
}
