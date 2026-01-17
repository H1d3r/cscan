package logic

import (
	"context"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AssetUpdateLabelsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetUpdateLabelsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetUpdateLabelsLogic {
	return &AssetUpdateLabelsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetUpdateLabels 更新资产标签
func (l *AssetUpdateLabelsLogic) AssetUpdateLabels(req *types.AssetUpdateLabelsReq, workspaceId string) (resp *types.BaseResp, err error) {
	assetModel := l.svcCtx.GetAssetModel(workspaceId)

	err = assetModel.UpdateLabels(l.ctx, req.Id, req.Labels)
	if err != nil {
		l.Logger.Errorf("更新资产标签失败: %v", err)
		return &types.BaseResp{
			Code: 1,
			Msg:  "更新失败",
		}, nil
	}

	return &types.BaseResp{
		Code: 0,
		Msg:  "success",
	}, nil
}

type AssetAddLabelLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetAddLabelLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetAddLabelLogic {
	return &AssetAddLabelLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetAddLabel 添加资产标签
func (l *AssetAddLabelLogic) AssetAddLabel(req *types.AssetAddLabelReq, workspaceId string) (resp *types.BaseResp, err error) {
	assetModel := l.svcCtx.GetAssetModel(workspaceId)

	err = assetModel.AddLabel(l.ctx, req.Id, req.Label)
	if err != nil {
		l.Logger.Errorf("添加资产标签失败: %v", err)
		return &types.BaseResp{
			Code: 1,
			Msg:  "添加失败",
		}, nil
	}

	return &types.BaseResp{
		Code: 0,
		Msg:  "success",
	}, nil
}

type AssetRemoveLabelLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetRemoveLabelLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetRemoveLabelLogic {
	return &AssetRemoveLabelLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetRemoveLabel 删除资产标签
func (l *AssetRemoveLabelLogic) AssetRemoveLabel(req *types.AssetRemoveLabelReq, workspaceId string) (resp *types.BaseResp, err error) {
	assetModel := l.svcCtx.GetAssetModel(workspaceId)

	err = assetModel.RemoveLabel(l.ctx, req.Id, req.Label)
	if err != nil {
		l.Logger.Errorf("删除资产标签失败: %v", err)
		return &types.BaseResp{
			Code: 1,
			Msg:  "删除失败",
		}, nil
	}

	return &types.BaseResp{
		Code: 0,
		Msg:  "success",
	}, nil
}
