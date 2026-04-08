package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// IndexManager 统一索引管理器
type IndexManager struct {
	db *mongo.Database
}

// NewIndexManager 创建索引管理器
func NewIndexManager(db *mongo.Database) *IndexManager {
	return &IndexManager{db: db}
}

// IndexDefinition 索引定义
type IndexDefinition struct {
	Keys       bson.D
	Unique     bool
	Background bool
	Sparse     bool
	TTLSeconds int32 // 0 表示非TTL索引
	Name       string
}

// EnsureAllIndexes 为指定工作空间创建所有必要的索引
func (m *IndexManager) EnsureAllIndexes(ctx context.Context, workspaceId string) error {
	if workspaceId == "" {
		workspaceId = "default"
	}

	logx.Infof("[IndexManager] Ensuring indexes for workspace: %s", workspaceId)

	// 资产索引
	if err := m.ensureAssetIndexes(ctx, workspaceId); err != nil {
		logx.Errorf("[IndexManager] Failed to create asset indexes: %v", err)
	}

	// 漏洞索引
	if err := m.ensureVulIndexes(ctx, workspaceId); err != nil {
		logx.Errorf("[IndexManager] Failed to create vul indexes: %v", err)
	}

	// 目录扫描结果索引
	if err := m.ensureDirScanResultIndexes(ctx, workspaceId); err != nil {
		logx.Errorf("[IndexManager] Failed to create dirscan indexes: %v", err)
	}

	// 资产历史索引
	if err := m.ensureAssetHistoryIndexes(ctx, workspaceId); err != nil {
		logx.Errorf("[IndexManager] Failed to create asset history indexes: %v", err)
	}

	// 任务索引
	if err := m.ensureTaskIndexes(ctx, workspaceId); err != nil {
		logx.Errorf("[IndexManager] Failed to create task indexes: %v", err)
	}

	logx.Infof("[IndexManager] All indexes ensured for workspace: %s", workspaceId)
	return nil
}

// ensureAssetIndexes 创建资产相关索引
func (m *IndexManager) ensureAssetIndexes(ctx context.Context, workspaceId string) error {
	collName := fmt.Sprintf("%s_asset", workspaceId)
	coll := m.db.Collection(collName)

	indexes := []mongo.IndexModel{
		// 复合唯一索引 - 防止重复资产
		{
			Keys:    bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1}},
			Options: options.Index().SetUnique(true).SetBackground(true).SetName("idx_host_port_unique"),
		},
		// authority 索引
		{
			Keys:    bson.D{{Key: "authority", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_authority"),
		},
		// 任务ID索引 - 新增重要索引
		{
			Keys:    bson.D{{Key: "taskId", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_taskId"),
		},
		// 更新时间索引
		{
			Keys:    bson.D{{Key: "update_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_update_time"),
		},
		// 创建时间索引
		{
			Keys:    bson.D{{Key: "create_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_create_time"),
		},
		// 服务索引
		{
			Keys:    bson.D{{Key: "service", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_service"),
		},
		// 应用索引（多值）
		{
			Keys:    bson.D{{Key: "app", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_app"),
		// icon 索引（多值）
		{
			Keys:    bson.D{{Key: "icon", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_icon"),
		},
		// port 索引
		{
			Keys:    bson.D{{Key: "port", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_port"),
		},
		},
		// 风险评分索引
		{
			Keys:    bson.D{{Key: "risk_score", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_risk_score"),
		},
		// 风险等级索引
		{
			Keys:    bson.D{{Key: "risk_level", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_risk_level"),
		},
		// 标签索引
		{
			Keys:    bson.D{{Key: "labels", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_labels"),
		},
		// 组织ID索引
		{
			Keys:    bson.D{{Key: "org_id", Value: 1}},
			Options: options.Index().SetBackground(true).SetSparse(true).SetName("idx_org_id"),
		},
		// 新资产标记索引
		{
			Keys:    bson.D{{Key: "new", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_new"),
		},
		// 复合索引：new + update_time（新资产列表查询优化）
		{
			Keys:    bson.D{{Key: "new", Value: 1}, {Key: "update_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_new_updateTime"),
		},
		// 复合索引：任务ID + 更新时间
		{
			Keys:    bson.D{{Key: "taskId", Value: 1}, {Key: "update_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_taskId_updateTime"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

// ensureVulIndexes 创建漏洞相关索引
func (m *IndexManager) ensureVulIndexes(ctx context.Context, workspaceId string) error {
	collName := fmt.Sprintf("%s_vul", workspaceId)
	coll := m.db.Collection(collName)

	indexes := []mongo.IndexModel{
		// 去重复合索引
		{
			Keys: bson.D{
				{Key: "host", Value: 1},
				{Key: "port", Value: 1},
				{Key: "pocfile", Value: 1},
				{Key: "url", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetBackground(true).SetName("idx_vul_unique"),
		},
		// 任务ID索引
		{
			Keys:    bson.D{{Key: "task_id", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_task_id"),
		},
		// 严重程度索引
		{
			Keys:    bson.D{{Key: "severity", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_severity"),
		},
		// 主机索引
		{
			Keys:    bson.D{{Key: "host", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_host"),
		},
		// CVE ID 索引
		{
			Keys:    bson.D{{Key: "cve_id", Value: 1}},
			Options: options.Index().SetBackground(true).SetSparse(true).SetName("idx_cve_id"),
		},
		// 创建时间索引
		{
			Keys:    bson.D{{Key: "create_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_create_time"),
		},
		// CVSS评分索引
		{
			Keys:    bson.D{{Key: "cvss_score", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_cvss_score"),
		},
		// 复合索引：host + port
		{
			Keys:    bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_host_port"),
		},
		// 复合索引：severity + create_time（按严重程度排序查询优化）
		{
			Keys:    bson.D{{Key: "severity", Value: 1}, {Key: "create_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_severity_createTime"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

// ensureDirScanResultIndexes 创建目录扫描结果索引
func (m *IndexManager) ensureDirScanResultIndexes(ctx context.Context, workspaceId string) error {
	collName := fmt.Sprintf("%s_dirscan_result", workspaceId)
	coll := m.db.Collection(collName)

	indexes := []mongo.IndexModel{
		// 资产关联复合索引
		{
			Keys: bson.D{
				{Key: "host", Value: 1},
				{Key: "port", Value: 1},
				{Key: "scan_time", Value: -1},
			},
			Options: options.Index().SetBackground(true).SetName("idx_host_port_scantime"),
		},
		// authority 索引
		{
			Keys:    bson.D{{Key: "authority", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_authority"),
		},
		// 任务ID索引
		{
			Keys:    bson.D{{Key: "task_id", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_task_id"),
		},
		// 复合索引：task_id + host + port（按任务查询目录扫描结果）
		{
			Keys:    bson.D{{Key: "task_id", Value: 1}, {Key: "host", Value: 1}, {Key: "port", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_taskId_host_port"),
		},
		// 状态码索引
		{
			Keys:    bson.D{{Key: "status_code", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_status_code"),
		},
		// TTL 索引 - 自动清理 30 天前的数据
		{
			Keys:    bson.D{{Key: "create_time", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(30 * 24 * 3600).SetBackground(true).SetName("idx_ttl_create_time"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

// ensureAssetHistoryIndexes 创建资产历史索引
func (m *IndexManager) ensureAssetHistoryIndexes(ctx context.Context, workspaceId string) error {
	collName := fmt.Sprintf("%s_asset_history", workspaceId)
	coll := m.db.Collection(collName)

	indexes := []mongo.IndexModel{
		// 资产ID索引
		{
			Keys:    bson.D{{Key: "assetId", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_assetId"),
		},
		// authority索引
		{
			Keys:    bson.D{{Key: "authority", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_authority"),
		},
		// 任务ID索引
		{
			Keys:    bson.D{{Key: "taskId", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_taskId"),
		},
		// TTL 索引 - 保留 90 天
		{
			Keys:    bson.D{{Key: "create_time", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(90 * 24 * 3600).SetBackground(true).SetName("idx_ttl_create_time"),
		},
		// 复合索引：assetId + create_time
		{
			Keys:    bson.D{{Key: "assetId", Value: 1}, {Key: "create_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_assetId_createTime"),
		},
		// 复合索引：assetId + taskId（按资产和任务关联查询）
		{
			Keys:    bson.D{{Key: "assetId", Value: 1}, {Key: "taskId", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_assetId_taskId"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

// ensureTaskIndexes 创建任务相关索引
func (m *IndexManager) ensureTaskIndexes(ctx context.Context, workspaceId string) error {
	collName := fmt.Sprintf("%s_maintask", workspaceId)
	coll := m.db.Collection(collName)

	indexes := []mongo.IndexModel{
		// 状态索引
		{
			Keys:    bson.D{{Key: "status", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_status"),
		},
		// 创建时间索引
		{
			Keys:    bson.D{{Key: "create_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_create_time"),
		},
		// 任务类型索引
		{
			Keys:    bson.D{{Key: "type", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_type"),
		},
		// 复合索引：状态 + 创建时间
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "create_time", Value: -1}},
			Options: options.Index().SetBackground(true).SetName("idx_status_createTime"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

// EnsureGlobalIndexes 创建全局（非工作空间隔离）索引
func (m *IndexManager) EnsureGlobalIndexes(ctx context.Context) error {
	logx.Info("[IndexManager] Ensuring global indexes")

	// 用户索引
	if err := m.ensureUserIndexes(ctx); err != nil {
		logx.Errorf("[IndexManager] Failed to create user indexes: %v", err)
	}

	// 指纹索引
	if err := m.ensureFingerprintIndexes(ctx); err != nil {
		logx.Errorf("[IndexManager] Failed to create fingerprint indexes: %v", err)
	}

	// 模板索引
	if err := m.ensureTemplateIndexes(ctx); err != nil {
		logx.Errorf("[IndexManager] Failed to create template indexes: %v", err)
	}

	logx.Info("[IndexManager] Global indexes ensured")
	return nil
}

func (m *IndexManager) ensureUserIndexes(ctx context.Context) error {
	coll := m.db.Collection("user")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetUnique(true).SetBackground(true).SetName("idx_username_unique"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

func (m *IndexManager) ensureFingerprintIndexes(ctx context.Context) error {
	coll := m.db.Collection("fingerprint")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_name"),
		},
		{
			Keys:    bson.D{{Key: "category", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_category"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

func (m *IndexManager) ensureTemplateIndexes(ctx context.Context) error {
	coll := m.db.Collection("nuclei_template")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "template_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetBackground(true).SetName("idx_template_id_unique"),
		},
		{
			Keys:    bson.D{{Key: "severity", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_severity"),
		},
		{
			Keys:    bson.D{{Key: "tags", Value: 1}},
			Options: options.Index().SetBackground(true).SetName("idx_tags"),
		},
		// 全文搜索索引
		{
			Keys: bson.D{
				{Key: "name", Value: "text"},
				{Key: "template_id", Value: "text"},
				{Key: "description", Value: "text"},
			},
			Options: options.Index().SetBackground(true).SetName("idx_fulltext"),
		},
	}

	return m.createIndexes(ctx, coll, indexes)
}

// createIndexes 创建索引（忽略已存在的索引）
func (m *IndexManager) createIndexes(ctx context.Context, coll *mongo.Collection, indexes []mongo.IndexModel) error {
	if len(indexes) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, err := coll.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		// 忽略索引已存在的错误
		if strings.Contains(err.Error(), "already exists") ||
			strings.Contains(err.Error(), "duplicate key") ||
			strings.Contains(err.Error(), "IndexOptionsConflict") {
			logx.Debugf("[IndexManager] Some indexes already exist for %s, skipping", coll.Name())
			return nil
		}
		return fmt.Errorf("create indexes for %s: %w", coll.Name(), err)
	}

	logx.Infof("[IndexManager] Created indexes for collection: %s", coll.Name())
	return nil
}

// DropAllIndexes 删除指定集合的所有非_id索引（用于重建）
func (m *IndexManager) DropAllIndexes(ctx context.Context, collName string) error {
	coll := m.db.Collection(collName)
	_, err := coll.Indexes().DropAll(ctx)
	return err
}

// GetIndexStats 获取索引统计信息
func (m *IndexManager) GetIndexStats(ctx context.Context, collName string) ([]bson.M, error) {
	coll := m.db.Collection(collName)

	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var indexes []bson.M
	if err = cursor.All(ctx, &indexes); err != nil {
		return nil, err
	}

	return indexes, nil
}
