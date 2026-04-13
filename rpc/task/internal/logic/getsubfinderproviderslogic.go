package logic

import (
	"context"

	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetSubfinderProvidersLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSubfinderProvidersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubfinderProvidersLogic {
	return &GetSubfinderProvidersLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取Subfinder数据源配置
func (l *GetSubfinderProvidersLogic) GetSubfinderProviders(in *pb.GetSubfinderProvidersReq) (*pb.GetSubfinderProvidersResp, error) {
	l.Logger.Infof("GetSubfinderProviders: workspaceId=%s", in.WorkspaceId)

	// 获取所有启用的Subfinder数据源配置
	providers, err := l.svcCtx.SubfinderProviderModel.FindEnabled(l.ctx)
	if err != nil {
		l.Logger.Errorf("GetSubfinderProviders: failed to get providers: %v", err)
		return &pb.GetSubfinderProvidersResp{
			Success: false,
			Message: "获取配置失败: " + err.Error(),
		}, nil
	}

	l.Logger.Infof("GetSubfinderProviders: found %d enabled providers", len(providers))

	// 转换为protobuf格式
	var pbProviders []*pb.SubfinderProviderDocument
	for _, p := range providers {
		l.Logger.Infof("GetSubfinderProviders: provider=%s, keys=%d, status=%s", p.Provider, len(p.Keys), p.Status)
		pbProviders = append(pbProviders, &pb.SubfinderProviderDocument{
			Id:          p.Id.Hex(),
			Provider:    p.Provider,
			Keys:        p.Keys,
			Status:      p.Status,
			Description: p.Description,
		})
	}

	return &pb.GetSubfinderProvidersResp{
		Success:   true,
		Message:   "success",
		Providers: pbProviders,
		Count:     int32(len(pbProviders)),
	}, nil
}
