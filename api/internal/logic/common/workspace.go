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

	// 始终包含默认空间
	ids := []string{"default"}

	// 查询所有工作空间
	workspaces, err := svcCtx.WorkspaceModel.Find(ctx, bson.M{}, 1, 100)
	if err != nil {
		return ids
	}

	for _, ws := range workspaces {
		ids = append(ids, ws.Id.Hex())
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
