package worker

import (
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
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
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Local().Format("2006-01-02 15:04:05")

	// 输出到控制台
	logx.Infof("%s [%s] [%s] %s", timestamp, level, l.workerName, msg)
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

// TaskLogger 任务日志记录器（本地输出）
type TaskLogger struct {
	workerName string
	taskId     string
}

// NewTaskLoggerLocal 创建本地任务日志记录器
func NewTaskLoggerLocal(workerName, taskId string) *TaskLogger {
	return &TaskLogger{
		workerName: workerName,
		taskId:     taskId,
	}
}

// log 内部日志方法，输出到控制台
func (l *TaskLogger) log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Local().Format("2006-01-02 15:04:05")

	// 输出到控制台
	logx.Infof("%s [%s] [%s] [Task:%s] %s", timestamp, level, l.workerName, l.taskId, msg)
}

func (l *TaskLogger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

func (l *TaskLogger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

func (l *TaskLogger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

func (l *TaskLogger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// ==================== WebSocket Logger ====================

// WorkerLoggerWS WebSocket日志记录器
type WorkerLoggerWS struct {
	workerName string
	wsClient   *WorkerWSClient
}

// NewWorkerLoggerWS 创建WebSocket日志记录器
func NewWorkerLoggerWS(workerName string, wsClient *WorkerWSClient) *WorkerLoggerWS {
	return &WorkerLoggerWS{
		workerName: workerName,
		wsClient:   wsClient,
	}
}

// log 内部日志方法
func (l *WorkerLoggerWS) log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Local().Format("2006-01-02 15:04:05")

	// 输出到控制台
	logx.Infof("%s [%s] [%s] %s", timestamp, level, l.workerName, msg)

	// 通过WebSocket立即发送（不缓冲）
	if l.wsClient != nil && l.wsClient.IsConnected() {
		if err := l.wsClient.SendLogImmediate("", level, msg); err != nil {
			logx.Infof("[WorkerLoggerWS] Failed to send log via WebSocket: %v", err)
		}
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

// TaskLoggerWS WebSocket任务日志记录器
type TaskLoggerWS struct {
	workerName string
	taskId     string
	wsClient   *WorkerWSClient
}

// NewTaskLoggerWS 创建WebSocket任务日志记录器
func NewTaskLoggerWS(workerName, taskId string, wsClient *WorkerWSClient) *TaskLoggerWS {
	return &TaskLoggerWS{
		workerName: workerName,
		taskId:     taskId,
		wsClient:   wsClient,
	}
}

// log 内部日志方法
func (l *TaskLoggerWS) log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Local().Format("2006-01-02 15:04:05")

	// 输出到控制台
	logx.Infof("%s [%s] [%s] [Task:%s] %s", timestamp, level, l.workerName, l.taskId, msg)

	// 调试：检查 wsClient 状态
	if l.wsClient == nil {
		logx.Info("[TaskLoggerWS] wsClient is nil!")
		return
	}

	connected := l.wsClient.IsConnected()

	// 通过WebSocket立即发送（不缓冲，确保日志及时到达）
	if connected {
		if err := l.wsClient.SendLogImmediate(l.taskId, level, msg); err != nil {
			logx.Infof("[TaskLoggerWS] Failed to send log via WebSocket: %v", err)
		}
	} else {
		logx.Info("[TaskLoggerWS] WebSocket not connected, log not sent to server")
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
