package common

import (
	"context"

	"cscan/api/internal/svc"

	"go.mongodb.org/mongo-driver/bson"
)

// GetWorkspaceIds 获取工作空间ID列表
// 当 workspaceId 为空或 "all" 时，返回所有工作空间ID（包括默认空间）
func GetWorkspaceIds(ctx context.Context, svcCtx *svc.ServiceContext, workspaceId string) []string {
	// 处理 "all" 值 - 前端传递 "all" 表示查询所有工作空间
	if workspaceId != "" && workspaceId != "all" {
		return []string{workspaceId}
	}

	var ids []string

	// 查询所有工作空间
	workspaces, err := svcCtx.WorkspaceModel.Find(ctx, bson.M{}, 1, 100)
	if err != nil {
		// 如果查询失败，至少返回默认空间
		return []string{"default"}
	}

	// 添加所有存在的工作空间
	for _, ws := range workspaces {
		ids = append(ids, ws.Id.Hex())
	}

	// 如果没有找到任何工作空间，添加默认空间
	if len(ids) == 0 {
		ids = append(ids, "default")
	} else {
		// 确保默认空间在列表中（如果存在的话）
		hasDefault := false
		for _, id := range ids {
			if id == "default" {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			// 检查默认空间是否真的存在数据
			defaultAssetModel := svcCtx.GetAssetModel("default")
			if count, err := defaultAssetModel.Count(ctx, bson.M{}); err == nil && count > 0 {
				ids = append(ids, "default")
			}
		}
	}

	return ids
}

// LoadOrgMap 加载组织ID到名称的映射
func LoadOrgMap(ctx context.Context, svcCtx *svc.ServiceContext) map[string]string {
	orgMap := make(map[string]string)
	orgs, err := svcCtx.OrganizationModel.Find(ctx, bson.M{}, 0, 0)
	if err != nil {
		return orgMap
	}
	for _, org := range orgs {
		orgMap[org.Id.Hex()] = org.Name
	}
	return orgMap
}
