package logic

import (
	"context"
	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type DeleteAssetGroupLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteAssetGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteAssetGroupLogic {
	return &DeleteAssetGroupLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DeleteAssetGroup 删除资产分组及其下的所有资产和相关任务
func (l *DeleteAssetGroupLogic) DeleteAssetGroup(req *types.DeleteAssetGroupReq, workspaceId string) (resp *types.DeleteAssetGroupResp, err error) {
	l.Logger.Infof("DeleteAssetGroup: workspaceId=%s, domain=%s", workspaceId, req.Domain)

	if req.Domain == "" {
		return &types.DeleteAssetGroupResp{
			Code: 1,
			Msg:  "域名不能为空",
		}, nil
	}

	// 获取需要操作的工作空间列表
	wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)
	l.Logger.Infof("DeleteAssetGroup工作空间列表: %v", wsIds)

	totalDeleted := int64(0)
	totalTasksDeleted := int64(0)

	// 遍历所有工作空间，删除匹配的资产和任务
	for _, wsId := range wsIds {
		// 1. 删除资产
		assetModel := l.svcCtx.GetAssetModel(wsId)

		// 先查询所有资产，然后筛选出属于该分组的资产
		// 这样可以确保使用和分组查询相同的逻辑
		allAssets, err := assetModel.Find(l.ctx, bson.M{}, 0, 0)
		if err != nil {
			l.Logger.Errorf("查询工作空间 %s 的资产失败: %v", wsId, err)
			continue
		}

		// 收集需要删除的资产ID
		var idsToDelete []string
		for _, asset := range allAssets {
			// 使用和分组查询相同的逻辑提取主域名
			mainDomain := extractMainDomainForDelete(asset.Host)
			if mainDomain == req.Domain {
				idsToDelete = append(idsToDelete, asset.Id.Hex())
			}
		}

		l.Logger.Infof("工作空间 %s 找到 %d 个匹配的资产", wsId, len(idsToDelete))

		// 批量删除资产
		if len(idsToDelete) > 0 {
			deletedCount, err := assetModel.BatchDelete(l.ctx, idsToDelete)
			if err != nil {
				l.Logger.Errorf("删除工作空间 %s 的资产失败: %v", wsId, err)
			} else {
				l.Logger.Infof("工作空间 %s 删除了 %d 个资产", wsId, deletedCount)
				totalDeleted += deletedCount
			}
		}

		// 2. 删除相关任务
		taskModel := l.svcCtx.GetMainTaskModel(wsId)
		
		// 查询所有任务
		allTasks, err := taskModel.Find(l.ctx, bson.M{}, 0, 0)
		if err != nil {
			l.Logger.Errorf("查询工作空间 %s 的任务失败: %v", wsId, err)
			continue
		}

		// 收集需要删除的任务ID
		var taskIdsToDelete []string
		for _, task := range allTasks {
			// 从任务目标中提取域名
			targets := strings.Split(task.Target, "\n")
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}
				
				// 提取主域名
				domain := extractMainDomainFromTargetForDelete(target)
				if domain == req.Domain {
					taskIdsToDelete = append(taskIdsToDelete, task.Id.Hex())
					break // 找到匹配的目标就跳出，避免重复添加
				}
			}
		}

		l.Logger.Infof("工作空间 %s 找到 %d 个匹配的任务", wsId, len(taskIdsToDelete))

		// 批量删除任务
		if len(taskIdsToDelete) > 0 {
			deletedTaskCount, err := taskModel.BatchDelete(l.ctx, taskIdsToDelete)
			if err != nil {
				l.Logger.Errorf("删除工作空间 %s 的任务失败: %v", wsId, err)
			} else {
				l.Logger.Infof("工作空间 %s 删除了 %d 个任务", wsId, deletedTaskCount)
				totalTasksDeleted += deletedTaskCount
			}
		}
	}

	return &types.DeleteAssetGroupResp{
		Code:         0,
		Msg:          "删除成功",
		DeletedCount: totalDeleted,
	}, nil
}

// extractMainDomainForDelete 从主机名中提取主域名（与assetgroupslogic.go中的逻辑保持一致）
func extractMainDomainForDelete(host string) string {
	// 如果是IP地址，返回IP
	if isIPAddressForDelete(host) {
		return host
	}

	// 分割域名
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}

	// 返回主域名（最后两部分）
	return strings.Join(parts[len(parts)-2:], ".")
}

// extractMainDomainFromTargetForDelete 从任务目标中提取主域名
func extractMainDomainFromTargetForDelete(target string) string {
	// 移除协议前缀
	target = strings.TrimPrefix(target, "http://")
	target = strings.TrimPrefix(target, "https://")
	
	// 移除端口
	if idx := strings.Index(target, ":"); idx > 0 {
		target = target[:idx]
	}
	
	// 移除路径
	if idx := strings.Index(target, "/"); idx > 0 {
		target = target[:idx]
	}
	
	// 移除通配符
	target = strings.TrimPrefix(target, "*.")
	
	// 移除CIDR
	if strings.Contains(target, "/") {
		return "" // CIDR不作为域名分组
	}
	
	// 如果是IP地址，返回IP
	if isIPAddressForDelete(target) {
		return target
	}
	
	// 提取主域名
	parts := strings.Split(target, ".")
	if len(parts) < 2 {
		return target
	}
	
	// 返回主域名（最后两部分）
	return strings.Join(parts[len(parts)-2:], ".")
}

// isIPAddressForDelete 判断是否为IP地址
func isIPAddressForDelete(host string) bool {
	// 简单判断：包含数字和点
	for _, c := range host {
		if (c >= '0' && c <= '9') || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return strings.Contains(host, ".")
}
