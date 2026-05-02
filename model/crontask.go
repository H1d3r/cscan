package model

import (
	"context"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CronTask 定时任务（MongoDB持久化模型）
type CronTask struct {
	Id           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	CronTaskId   string             `bson:"cron_task_id" json:"cronTaskId"` // 业务ID（UUID，用于跨服务引用）
	Name         string             `bson:"name" json:"name"`
	ScheduleType string             `bson:"schedule_type" json:"scheduleType"` // cron / once
	CronSpec     string             `bson:"cron_spec" json:"cronSpec"`
	ScheduleTime string             `bson:"schedule_time" json:"scheduleTime"`
	WorkspaceId  string             `bson:"workspace_id" json:"workspaceId"`
	MainTaskId   string             `bson:"main_task_id" json:"mainTaskId"`
	TaskName     string             `bson:"task_name" json:"taskName"`
	Target       string             `bson:"target" json:"target"`
	Config       string             `bson:"config" json:"config"`
	Status       string             `bson:"status" json:"status"` // enable / disable
	LastRunTime  string             `bson:"last_run_time" json:"lastRunTime"`
	NextRunTime  string             `bson:"next_run_time" json:"nextRunTime"`
	CreateTime   time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime   time.Time          `bson:"update_time" json:"updateTime"`
}

// CronTaskModel 定时任务数据模型
type CronTaskModel struct {
	coll *mongo.Collection
}

// NewCronTaskModel 创建定时任务模型
func NewCronTaskModel(db *mongo.Database) *CronTaskModel {
	coll := db.Collection("cron_task")

	// 创建索引
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "cron_task_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "workspace_id", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
	}
	ensureIndexes(coll, indexes)

	return &CronTaskModel{coll: coll}
}

// Insert 插入定时任务
func (m *CronTaskModel) Insert(ctx context.Context, doc *CronTask) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

// FindByCronTaskId 根据业务ID查找
func (m *CronTaskModel) FindByCronTaskId(ctx context.Context, cronTaskId string) (*CronTask, error) {
	var doc CronTask
	err := m.coll.FindOne(ctx, bson.M{"cron_task_id": cronTaskId}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// FindByWorkspaceId 根据工作空间查找（支持关键字过滤）
func (m *CronTaskModel) FindByWorkspaceId(ctx context.Context, workspaceId string, keyword string, page, pageSize int) ([]CronTask, int64, error) {
	filter := bson.M{}
	if workspaceId != "" && workspaceId != "all" {
		filter["workspace_id"] = workspaceId
	}
	if keyword != "" {
		escapedKeyword := regexp.QuoteMeta(keyword)
		filter["$or"] = bson.A{
			bson.M{"name": primitive.Regex{Pattern: escapedKeyword, Options: "i"}},
			bson.M{"task_name": primitive.Regex{Pattern: escapedKeyword, Options: "i"}},
		}
	}

	total, err := m.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var docs []CronTask
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

// FindAll 查找所有定时任务（调度器启动时加载，内部按 status 过滤是否启动）
func (m *CronTaskModel) FindAll(ctx context.Context) ([]CronTask, error) {
	cursor, err := m.coll.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []CronTask
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// UpdateByCronTaskId 根据业务ID更新
func (m *CronTaskModel) UpdateByCronTaskId(ctx context.Context, cronTaskId string, update bson.M) error {
	// 检查update中是否已包含update_time，避免容量浪费
	setDoc := make(bson.M, len(update))
	for k, v := range update {
		setDoc[k] = v
	}
	setDoc["update_time"] = time.Now()
	_, err := m.coll.UpdateOne(ctx, bson.M{"cron_task_id": cronTaskId}, bson.M{"$set": setDoc})
	return err
}

// DeleteByCronTaskId 根据业务ID删除
func (m *CronTaskModel) DeleteByCronTaskId(ctx context.Context, cronTaskId string) error {
	_, err := m.coll.DeleteOne(ctx, bson.M{"cron_task_id": cronTaskId})
	return err
}

// BatchDeleteByCronTaskIds 批量删除
func (m *CronTaskModel) BatchDeleteByCronTaskIds(ctx context.Context, cronTaskIds []string) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{"cron_task_id": bson.M{"$in": cronTaskIds}})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}
