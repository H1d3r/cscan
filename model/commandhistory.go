package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CommandHistory 命令执行历史
type CommandHistory struct {
	Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	WorkerName string             `bson:"worker_name" json:"workerName"` // Worker名称
	SessionId  string             `bson:"session_id" json:"sessionId"`   // 终端会话ID
	Command    string             `bson:"command" json:"command"`        // 执行的命令
	ExitCode   int                `bson:"exit_code" json:"exitCode"`     // 退出码
	Success    bool               `bson:"success" json:"success"`        // 是否成功
	Error      string             `bson:"error,omitempty" json:"error"`  // 错误信息
	Duration   int64              `bson:"duration" json:"duration"`      // 执行时长(毫秒)
	Operator   string             `bson:"operator" json:"operator"`      // 操作人
	ClientIP   string             `bson:"client_ip" json:"clientIp"`     // 客户端IP
	CreateTime time.Time          `bson:"create_time" json:"createTime"` // 创建时间
}

// CommandHistoryModel 命令历史模型
type CommandHistoryModel struct {
	*BaseModel[CommandHistory]
}

// NewCommandHistoryModel 创建命令历史模型
func NewCommandHistoryModel(db *mongo.Database) *CommandHistoryModel {
	coll := db.Collection("command_history")
	m := &CommandHistoryModel{
		BaseModel: NewBaseModel[CommandHistory](coll),
	}

	// 创建索引
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "worker_name", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "session_id", Value: 1}},
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
	}
	m.EnsureIndexes(ctx, indexes)

	return m
}

// RecordCommand 记录命令执行
func (m *CommandHistoryModel) RecordCommand(ctx context.Context, history *CommandHistory) error {
	if history.Id.IsZero() {
		history.Id = primitive.NewObjectID()
	}
	if history.CreateTime.IsZero() {
		history.CreateTime = time.Now()
	}
	return m.Insert(ctx, history)
}

// GetByWorker 获取Worker的命令历史
func (m *CommandHistoryModel) GetByWorker(ctx context.Context, workerName string, page, pageSize int) ([]CommandHistory, int64, error) {
	filter := bson.M{"worker_name": workerName}

	total, err := m.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	histories, err := m.FindWithSort(ctx, filter, page, pageSize, "create_time", -1)
	if err != nil {
		return nil, 0, err
	}

	return histories, total, nil
}

// GetBySession 获取会话的命令历史
func (m *CommandHistoryModel) GetBySession(ctx context.Context, sessionId string) ([]CommandHistory, error) {
	filter := bson.M{"session_id": sessionId}
	return m.FindWithSort(ctx, filter, 0, 0, "create_time", 1)
}

// GetRecent 获取最近的命令历史
func (m *CommandHistoryModel) GetRecent(ctx context.Context, limit int) ([]CommandHistory, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "create_time", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := m.Coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var histories []CommandHistory
	if err = cursor.All(ctx, &histories); err != nil {
		return nil, err
	}
	return histories, nil
}

// DeleteOldRecords 删除旧记录（保留最近N天）
func (m *CommandHistoryModel) DeleteOldRecords(ctx context.Context, days int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	filter := bson.M{"create_time": bson.M{"$lt": cutoff}}
	return m.DeleteMany(ctx, filter)
}
