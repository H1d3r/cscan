package svc

import (
	"context"
	"time"

	"cscan/internal/model"
	"go.mongodb.org/mongo-driver/mongo"
)

// WorkerLogEntry Worker 日志条目（API 响应格式）
type WorkerLogEntry struct {
	Ts     string `json:"ts"`
	Level  string `json:"level"`
	Worker string `json:"worker"`
	TaskId string `json:"taskId,omitempty"`
	Msg    string `json:"msg"`
	Seq    int64  `json:"-"` // 用于 cursor 游标，不返回前端
}

// WorkerLogReader Worker 日志读取器（从 MongoDB 读取）
type WorkerLogReader struct {
	model *model.WorkerLogModel
}

// NewWorkerLogReader 创建日志读取器
func NewWorkerLogReader(db *mongo.Database) *WorkerLogReader {
	return &WorkerLogReader{model: model.NewWorkerLogModel(db)}
}

// toEntry 将 MongoDB 文档转为 API 响应格式
func toEntry(log model.WorkerLog) WorkerLogEntry {
	return WorkerLogEntry{
		Ts:     log.CreateTime.Local().Format("2006-01-02T15:04:05.000-07:00"),
		Level:  log.Level,
		Worker: log.Worker,
		TaskId: log.TaskId,
		Msg:    log.Msg,
		Seq:    log.Seq,
	}
}

// ReadTail 读取指定 Worker 在指定日期的最后 N 条日志
func (r *WorkerLogReader) ReadTail(workerName, date string, lines int) ([]WorkerLogEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logs, err := r.model.ReadTail(ctx, workerName, date, lines)
	if err != nil {
		return []WorkerLogEntry{}, err
	}
	entries := make([]WorkerLogEntry, len(logs))
	for i, log := range logs {
		entries[i] = toEntry(log)
	}
	return entries, nil
}

// ReadByTaskId 按 taskId 过滤日志
func (r *WorkerLogReader) ReadByTaskId(workerName, taskId, date string, lines int) ([]WorkerLogEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logs, err := r.model.ReadByTaskId(ctx, workerName, taskId, date, lines)
	if err != nil {
		return []WorkerLogEntry{}, err
	}
	entries := make([]WorkerLogEntry, len(logs))
	for i, log := range logs {
		entries[i] = toEntry(log)
	}
	return entries, nil
}

// ReadByTaskIdAll 跨所有日期和 Worker 按 taskId 查询日志
func (r *WorkerLogReader) ReadByTaskIdAll(taskId string, lines int) ([]WorkerLogEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logs, err := r.model.ReadByTaskIdAll(ctx, taskId, lines)
	if err != nil {
		return []WorkerLogEntry{}, err
	}
	entries := make([]WorkerLogEntry, len(logs))
	for i, log := range logs {
		entries[i] = toEntry(log)
	}
	return entries, nil
}

// ReadByTaskIdAfter 增量读取：返回 seq > afterSeq 且 createTime > afterTime 的新日志
func (r *WorkerLogReader) ReadByTaskIdAfter(taskId string, afterSeq int64, afterTime string, lines int) ([]WorkerLogEntry, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logs, err := r.model.ReadByTaskIdAfter(ctx, taskId, afterSeq, afterTime, lines)
	if err != nil {
		return []WorkerLogEntry{}, 0, err
	}
	entries := make([]WorkerLogEntry, 0, len(logs))
	var nextCursor int64
	for _, log := range logs {
		entry := toEntry(log)
		nextCursor = log.Seq
		entries = append(entries, entry)
	}
	return entries, nextCursor, nil
}

// ListDates 返回有日志的日期列表（降序）
func (r *WorkerLogReader) ListDates() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.model.ListDates(ctx)
}

// ListWorkers 返回指定日期下的 Worker 列表
func (r *WorkerLogReader) ListWorkers(date string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.model.ListWorkers(ctx, date)
}

// FindLatestDate 返回最新日志日期
func (r *WorkerLogReader) FindLatestDate() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.model.FindLatestDate(ctx)
}

// Clear 清空所有 Worker 日志
func (r *WorkerLogReader) Clear() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return r.model.Clear(ctx)
}
