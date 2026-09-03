package model

import (
	"context"
	"regexp"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// WorkerLog Worker 日志文档（直写 MongoDB，TTL 7 天自动过期）
type WorkerLog struct {
	Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Worker     string             `bson:"worker" json:"worker"`
	TaskId     string             `bson:"task_id,omitempty" json:"taskId,omitempty"`
	Level      string             `bson:"level" json:"level"`
	Msg        string             `bson:"msg" json:"msg"`
	CreateTime time.Time          `bson:"create_time" json:"createTime"`
	Seq        int64              `bson:"seq" json:"seq"`
}

// WorkerLogModel Worker 日志集合操作
type WorkerLogModel struct {
	coll *mongo.Collection
}

// NewWorkerLogModel 创建 Worker 日志模型，同时创建索引
func NewWorkerLogModel(db *mongo.Database) *WorkerLogModel {
	coll := db.Collection("worker_log")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "create_time", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(7 * 24 * 3600)},
		{Keys: bson.D{{Key: "worker", Value: 1}, {Key: "create_time", Value: -1}, {Key: "seq", Value: -1}}},
		{Keys: bson.D{{Key: "task_id", Value: 1}, {Key: "create_time", Value: -1}, {Key: "seq", Value: -1}}},
	})
	return &WorkerLogModel{coll: coll}
}

// InsertMany 批量插入日志
func (m *WorkerLogModel) InsertMany(ctx context.Context, logs []interface{}) error {
	if len(logs) == 0 {
		return nil
	}
	_, err := m.coll.InsertMany(ctx, logs, options.InsertMany().SetOrdered(false))
	return err
}

// ReadTail 读取指定 Worker 在指定日期的最后 N 条日志（按时间正序返回）
func (m *WorkerLogModel) ReadTail(ctx context.Context, worker, date string, lines int) ([]WorkerLog, error) {
	if lines <= 0 {
		lines = 500
	}
	if lines > 10000 {
		lines = 10000
	}
	filter := bson.M{"worker": worker}
	if date != "" {
		start, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err == nil {
			end := start.Add(24 * time.Hour)
			filter["create_time"] = bson.M{"$gte": start, "$lt": end}
		}
	}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}, {Key: "seq", Value: -1}}).SetLimit(int64(lines))
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result []WorkerLog
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	// 反转为正序
	slices.Reverse(result)
	return result, nil
}

// ReadByTaskId 按 taskId 过滤日志（按时间正序返回）
func (m *WorkerLogModel) ReadByTaskId(ctx context.Context, worker, taskId, date string, lines int) ([]WorkerLog, error) {
	if lines <= 0 {
		lines = 500
	}
	if lines > 10000 {
		lines = 10000
	}
	filter := bson.M{"worker": worker, "task_id": taskId}
	if date != "" {
		start, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err == nil {
			end := start.Add(24 * time.Hour)
			filter["create_time"] = bson.M{"$gte": start, "$lt": end}
		}
	}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}, {Key: "seq", Value: -1}}).SetLimit(int64(lines))
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result []WorkerLog
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	slices.Reverse(result)
	return result, nil
}

// ReadByTaskIdAll 跨所有日期和 Worker 按 taskId 查询日志（匹配主任务ID及子任务ID），按时间正序返回
func (m *WorkerLogModel) ReadByTaskIdAll(ctx context.Context, taskId string, lines int) ([]WorkerLog, error) {
	if lines <= 0 {
		lines = 500
	}
	if lines > 10000 {
		lines = 10000
	}
	// 前缀匹配主任务ID及其子任务ID（taskId-0, taskId-1, ...）
	filter := bson.M{"task_id": bson.M{"$regex": "^" + regexp.QuoteMeta(taskId)}}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}, {Key: "seq", Value: -1}}).SetLimit(int64(lines))
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result []WorkerLog
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	slices.Reverse(result)
	return result, nil
}

// ReadByTaskIdAfter 增量读取：返回 seq > afterSeq 且 createTime > afterTime 的新日志
func (m *WorkerLogModel) ReadByTaskIdAfter(ctx context.Context, taskId string, afterSeq int64, afterTime string, limit int) ([]WorkerLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 10000 {
		limit = 10000
	}

	filter := bson.M{
		"task_id": bson.M{"$regex": "^" + regexp.QuoteMeta(taskId)},
		"$or": []bson.M{
			{"seq": bson.M{"$gt": afterSeq}},
		},
	}
	// 解析 afterTime 为 time.Time 以匹配 MongoDB 中的 BSON Date 类型
	if parsed, err := time.ParseInLocation("2006-01-02T15:04:05.000-07:00", afterTime, time.Local); err == nil {
		filter["$or"] = append(filter["$or"].([]bson.M), bson.M{"seq": afterSeq, "create_time": bson.M{"$gt": parsed}})
	}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: 1}, {Key: "seq", Value: 1}}).SetLimit(int64(limit))
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result []WorkerLog
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListDates 返回有日志的日期列表（降序）
func (m *WorkerLogModel) ListDates(ctx context.Context) ([]string, error) {
	pipeline := []bson.M{
		{"$group": bson.M{"_id": bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$create_time"}}}},
		{"$sort": bson.M{"_id": -1}},
	}
	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return []string{}, nil
	}
	defer cursor.Close(ctx)
	var results []struct {
		Id string `bson:"_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return []string{}, nil
	}
	dates := make([]string, len(results))
	for i, r := range results {
		dates[i] = r.Id
	}
	return dates, nil
}

// ListWorkers 返回指定日期下的 Worker 列表
func (m *WorkerLogModel) ListWorkers(ctx context.Context, date string) ([]string, error) {
	matchFilter := bson.M{}
	if date != "" {
		start, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err == nil {
			end := start.Add(24 * time.Hour)
			matchFilter["create_time"] = bson.M{"$gte": start, "$lt": end}
		}
	}
	pipeline := []bson.M{
		{"$match": matchFilter},
		{"$group": bson.M{"_id": "$worker"}},
		{"$sort": bson.M{"_id": 1}},
	}
	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return []string{}, nil
	}
	defer cursor.Close(ctx)
	var results []struct {
		Id string `bson:"_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return []string{}, nil
	}
	workers := make([]string, len(results))
	for i, r := range results {
		workers[i] = r.Id
	}
	return workers, nil
}

// FindLatestDate 返回最新日志日期
func (m *WorkerLogModel) FindLatestDate(ctx context.Context) (string, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "create_time", Value: -1}}).SetProjection(bson.M{"create_time": 1})
	var doc WorkerLog
	if err := m.coll.FindOne(ctx, bson.M{}, opts).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return "", nil
		}
		return "", err
	}
	return doc.CreateTime.Local().Format("2006-01-02"), nil
}

// Clear 清空所有 Worker 日志
func (m *WorkerLogModel) Clear(ctx context.Context) error {
	_, err := m.coll.DeleteMany(ctx, bson.M{})
	return err
}
