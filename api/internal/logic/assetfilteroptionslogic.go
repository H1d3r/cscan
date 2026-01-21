package logic

import (
	"context"
	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"sort"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type AssetFilterOptionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetFilterOptionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetFilterOptionsLogic {
	return &AssetFilterOptionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetFilterOptions 获取资产过滤器选项
func (l *AssetFilterOptionsLogic) AssetFilterOptions(req *types.AssetFilterOptionsReq, workspaceId string) (resp *types.AssetFilterOptionsResp, err error) {
	l.Logger.Infof("AssetFilterOptions查询: workspaceId=%s, domain=%s, hasScreenshot=%v", workspaceId, req.Domain, req.HasScreenshot)

	// 获取需要查询的工作空间列表
	wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)
	l.Logger.Infof("AssetFilterOptions查询工作空间列表: %v", wsIds)

	// 用于存储所有唯一值
	techSet := make(map[string]bool)
	portSet := make(map[int]bool)
	statusSet := make(map[string]bool)
	labelSet := make(map[string]bool)

	// 遍历所有工作空间
	for _, wsId := range wsIds {
		assetModel := l.svcCtx.GetAssetModel(wsId)

		// 构建查询条件
		filter := bson.M{}

		// 域名过滤
		if req.Domain != "" {
			filter["host"] = bson.M{"$regex": req.Domain, "$options": "i"}
		}

		// 是否只查询有截图的资产
		if req.HasScreenshot {
			filter["screenshot"] = bson.M{"$ne": ""}
		}

		// 查询所有资产
		assets, err := assetModel.Find(l.ctx, filter, 0, 0)
		if err != nil {
			l.Logger.Errorf("查询工作空间 %s 资产失败: %v", wsId, err)
			continue
		}

		// 收集所有唯一的技术栈、端口和状态码
		for _, asset := range assets {
			// 技术栈
			for _, tech := range asset.App {
				if tech != "" {
					techSet[tech] = true
				}
			}

			// 端口
			if asset.Port > 0 {
				portSet[asset.Port] = true
			}

			// 状态码
			if asset.HttpStatus != "" {
				statusSet[asset.HttpStatus] = true
			}

			// 标签
			for _, label := range asset.Labels {
				if label != "" {
					labelSet[label] = true
				}
			}
		}
	}

	// 转换为切片并排序
	technologies := make([]string, 0, len(techSet))
	for tech := range techSet {
		technologies = append(technologies, tech)
	}
	sort.Strings(technologies)

	ports := make([]int, 0, len(portSet))
	for port := range portSet {
		ports = append(ports, port)
	}
	sort.Ints(ports)

	statusCodes := make([]string, 0, len(statusSet))
	for status := range statusSet {
		statusCodes = append(statusCodes, status)
	}
	sort.Strings(statusCodes)

	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	// 构建响应
	return &types.AssetFilterOptionsResp{
		Code:         0,
		Msg:          "success",
		Technologies: technologies,
		Ports:        ports,
		StatusCodes:  statusCodes,
		Labels:       labels,
	}, nil
}
