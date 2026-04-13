package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditLogType 审计日志类型
type AuditLogType string

const (
	AuditLogTypeFileList      AuditLogType = "file_list"      // 文件列表
	AuditLogTypeFileUpload    AuditLogType = "file_upload"    // 文件上传
	AuditLogTypeFileDownload  AuditLogType = "file_download"  // 文件下载
	AuditLogTypeFileDelete    AuditLogType = "file_delete"    // 文件删除
	AuditLogTypeFileMkdir     AuditLogType = "file_mkdir"     // 创建目录
	AuditLogTypeTerminalOpen  AuditLogType = "terminal_open"  // 打开终端
	AuditLogTypeTerminalClose AuditLogType = "terminal_close" // 关闭终端
	AuditLogTypeTerminalExec  AuditLogType = "terminal_exec"  // 执行命令
	AuditLogTypeConsoleInfo   AuditLogType = "console_info"   // 查看Worker信息
)

// AuditLog 审计日志
type AuditLog struct {
	Id         primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	Type       AuditLogType           `bson:"type" json:"type"`                      // 操作类型
	WorkerName string                 `bson:"worker_name" json:"workerName"`         // Worker名称
	UserId     string                 `bson:"user_id" json:"userId"`                 // 操作用户ID
	Username   string                 `bson:"username" json:"username"`              // 操作用户名
	ClientIP   string                 `bson:"client_ip" json:"clientIp"`             // 客户端IP
	Path       string                 `bson:"path,omitempty" json:"path"`            // 文件路径（文件操作）
	Command    string                 `bson:"command,omitempty" json:"command"`      // 命令（终端操作）
	SessionId  string                 `bson:"session_id,omitempty" json:"sessionId"` // 会话ID
	Success    bool                   `bson:"success" json:"success"`                // 是否成功
	Error      string                 `bson:"error,omitempty" json:"error"`          // 错误信息
	Details    map[string]interface{} `bson:"details,omitempty" json:"details"`      // 额外详情
	Duration   int64                  `bson:"duration" json:"duration"`              // 操作耗时(毫秒)，始终记录
	CreateTime time.Time              `bson:"create_time" json:"createTime"`         // 创建时间
}

// AuditLogModel 审计日志模型
type AuditLogModel struct {
	*BaseModel[AuditLog]
}

// NewAuditLogModel 创建审计日志模型
func NewAuditLogModel(db *mongo.Database) *AuditLogModel {
	coll := db.Collection("audit_log")
	m := &AuditLogModel{
		BaseModel: NewBaseModel[AuditLog](coll),
	}

	// 创建索引
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "type", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "worker_name", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "create_time", Value: -1}},
		},
		{
			Keys: bson.D{
				{Key: "worker_name", Value: 1},
				{Key: "create_time", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "type", Value: 1},
				{Key: "create_time", Value: -1},
			},
		},
	}
	m.EnsureIndexes(ctx, indexes)

	return m
}

// RecordAudit 记录审计日志
func (m *AuditLogModel) RecordAudit(ctx context.Context, log *AuditLog) error {
	if log.Id.IsZero() {
		log.Id = primitive.NewObjectID()
	}
	if log.CreateTime.IsZero() {
		log.CreateTime = time.Now()
	}
	return m.Insert(ctx, log)
}

// GetByWorker 获取Worker的审计日志
func (m *AuditLogModel) GetByWorker(ctx context.Context, workerName string, page, pageSize int) ([]AuditLog, int64, error) {
	filter := bson.M{"worker_name": workerName}

	total, err := m.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	logs, err := m.FindWithSort(ctx, filter, page, pageSize, "create_time", -1)
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetByUser 获取用户的审计日志
func (m *AuditLogModel) GetByUser(ctx context.Context, userId string, page, pageSize int) ([]AuditLog, int64, error) {
	filter := bson.M{"user_id": userId}

	total, err := m.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	logs, err := m.FindWithSort(ctx, filter, page, pageSize, "create_time", -1)
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetByType 获取指定类型的审计日志
func (m *AuditLogModel) GetByType(ctx context.Context, logType AuditLogType, page, pageSize int) ([]AuditLog, int64, error) {
	filter := bson.M{"type": logType}

	total, err := m.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	logs, err := m.FindWithSort(ctx, filter, page, pageSize, "create_time", -1)
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetRecent 获取最近的审计日志
func (m *AuditLogModel) GetRecent(ctx context.Context, limit int) ([]AuditLog, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "create_time", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := m.Coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []AuditLog
	if err = cursor.All(ctx, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// Search 搜索审计日志
func (m *AuditLogModel) Search(ctx context.Context, filter AuditLogFilter, page, pageSize int) ([]AuditLog, int64, error) {
	query := bson.M{}

	if filter.Type != "" {
		query["type"] = filter.Type
	}
	if filter.WorkerName != "" {
		query["worker_name"] = filter.WorkerName
	}
	if filter.UserId != "" {
		query["user_id"] = filter.UserId
	}
	if filter.Username != "" {
		query["username"] = bson.M{"$regex": filter.Username, "$options": "i"}
	}
	if !filter.StartTime.IsZero() || !filter.EndTime.IsZero() {
		timeFilter := bson.M{}
		if !filter.StartTime.IsZero() {
			timeFilter["$gte"] = filter.StartTime
		}
		if !filter.EndTime.IsZero() {
			timeFilter["$lte"] = filter.EndTime
		}
		query["create_time"] = timeFilter
	}
	if filter.Success != nil {
		query["success"] = *filter.Success
	}

	total, err := m.Count(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	logs, err := m.FindWithSort(ctx, query, page, pageSize, "create_time", -1)
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// DeleteOldRecords 删除旧记录（保留最近N天）
func (m *AuditLogModel) DeleteOldRecords(ctx context.Context, days int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	filter := bson.M{"create_time": bson.M{"$lt": cutoff}}
	return m.DeleteMany(ctx, filter)
}

// ClearByWorker 清空指定Worker的审计日志
func (m *AuditLogModel) ClearByWorker(ctx context.Context, workerName string) (int64, error) {
	filter := bson.M{"worker_name": workerName}
	return m.DeleteMany(ctx, filter)
}

// ClearAll 清空所有审计日志
func (m *AuditLogModel) ClearAll(ctx context.Context) (int64, error) {
	return m.DeleteMany(ctx, bson.M{})
}

// AuditLogFilter 审计日志过滤条件
type AuditLogFilter struct {
	Type       AuditLogType
	WorkerName string
	UserId     string
	Username   string
	StartTime  time.Time
	EndTime    time.Time
	Success    *bool
}
