package worker

import (
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/mongo"
)

// 日志级别常量
const (
	LevelDebug = "DEBUG"
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)

// LogEntry 日志条目（统一结构）
type LogEntry struct {
	Timestamp  string `json:"timestamp"`
	Level      string `json:"level"`
	WorkerName string `json:"workerName"`
	TaskId     string `json:"taskId,omitempty"`
	Message    string `json:"message"`
}

// Logger 统一日志接口
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// logConsole 按业务等级输出到控制台，时间/等级前缀由 logx 统一附加，消息内不再重复。
// 任务流水日志（taskId 非空）降级为 DEBUG 输出，容器日志页默认隐藏（完整分级日志仍直写 MongoDB）；
// WARN/ERROR 保持原级别确保异常默认可见。go-zero 无 WARN 级别，借用 slow 输出（前端映射为 WARN）。
func logConsole(level, taskId, msg string) {
	if taskId != "" {
		msg = fmt.Sprintf("[Task:%s] %s", taskId, msg)
		if level == LevelInfo {
			level = LevelDebug
		}
	}
	switch level {
	case LevelError:
		logx.Error(msg)
	case LevelWarn:
		logx.Slow(msg)
	case LevelDebug:
		logx.Debug(msg)
	default:
		logx.Info(msg)
	}
}

// ==================== Local Logger (No Redis) ====================

// WorkerLogger Worker 日志记录器（本地输出）
type WorkerLogger struct {
	workerName string
}

// NewWorkerLoggerLocal 创建本地日志记录器
func NewWorkerLoggerLocal(workerName string) *WorkerLogger {
	return &WorkerLogger{
		workerName: workerName,
	}
}

// log 内部日志方法，输出到控制台
func (l *WorkerLogger) log(level, format string, args ...interface{}) {
	logConsole(level, "", fmt.Sprintf(format, args...))
}

func (l *WorkerLogger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

func (l *WorkerLogger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

func (l *WorkerLogger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

func (l *WorkerLogger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// ==================== MongoDB Logger (直写 MongoDB) ====================

// WorkerLoggerWS Worker 日志记录器
// 日志直写 MongoDB（worker_log 集合），不再经过 WebSocket 游标同步
type WorkerLoggerWS struct {
	workerName string
}

// NewWorkerLoggerWS 创建日志记录器
func NewWorkerLoggerWS(workerName string) *WorkerLoggerWS {
	return &WorkerLoggerWS{
		workerName: workerName,
	}
}

// log 内部日志方法
func (l *WorkerLoggerWS) log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logConsole(level, "", msg)

	// 直写 MongoDB（动态引用 globalMongoLogger，支持 SetMongoDB 后懒初始化）
	if globalMongoLogger != nil {
		globalMongoLogger.Write(level, "", msg)
	}
}

func (l *WorkerLoggerWS) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

func (l *WorkerLoggerWS) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

func (l *WorkerLoggerWS) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

func (l *WorkerLoggerWS) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// TaskLoggerWS 任务日志记录器
// 日志直写 MongoDB（worker_log 集合），不再经过 WebSocket 游标同步
type TaskLoggerWS struct {
	workerName string
	taskId     string
}

// NewTaskLoggerWS 创建任务日志记录器
func NewTaskLoggerWS(workerName, taskId string) *TaskLoggerWS {
	return &TaskLoggerWS{
		workerName: workerName,
		taskId:     taskId,
	}
}

// log 内部日志方法
func (l *TaskLoggerWS) log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logConsole(level, l.taskId, msg)

	// 直写 MongoDB（动态引用 globalMongoLogger，支持 SetMongoDB 后懒初始化）
	// 说明：不在写入端丢弃 DEBUG 级别日志。DEBUG 默认在 API 读取端（GetTaskLogs）
	// 按需过滤，这样任务日志视图默认不显示 DEBUG，但可通过 IncludeDebug 拉取完整日志。
	if globalMongoLogger != nil {
		globalMongoLogger.Write(level, l.taskId, msg)
	}
}

func (l *TaskLoggerWS) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

func (l *TaskLoggerWS) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

func (l *TaskLoggerWS) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

func (l *TaskLoggerWS) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Event intentionally discards structured scanner/phase data. User-facing
// task logs continue through Debug/Info/Warn/Error.
func (l *TaskLoggerWS) Event(level, msg, event, phase, outcome string, fields map[string]interface{}) {
}

// ==================== 全局 MongoLogger 实例 ====================

// globalMongoLogger 全局 MongoDB 日志写入器，由 Worker 初始化时设置
var globalMongoLogger *MongoLogger

// InitGlobalMongoLogger 初始化全局 MongoDB 日志写入器
func InitGlobalMongoLogger(db *mongo.Database, workerName string) {
	globalMongoLogger = NewMongoLogger(db, workerName)
}

// UpdateGlobalMongoLoggerWorkerName 更新全局日志器的 worker 名称（rename 后调用）
func UpdateGlobalMongoLoggerWorkerName(name string) {
	if globalMongoLogger != nil {
		globalMongoLogger.SetWorkerName(name)
	}
}
