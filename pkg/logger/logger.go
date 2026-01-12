package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level 日志级别
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

// Logger 日志接口
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
	WithPrefix(prefix string) Logger
	WithField(key string, value interface{}) Logger
}

// SimpleLogger 简单日志实现
type SimpleLogger struct {
	prefix string
	level  Level
	output io.Writer
	fields map[string]interface{}
	mu     sync.Mutex
}

// NewLogger 创建日志器
func NewLogger(prefix string) *SimpleLogger {
	return &SimpleLogger{
		prefix: prefix,
		level:  LevelInfo,
		output: os.Stdout,
		fields: make(map[string]interface{}),
	}
}

// SetLevel 设置日志级别
func (l *SimpleLogger) SetLevel(level Level) *SimpleLogger {
	l.level = level
	return l
}

// SetOutput 设置输出
func (l *SimpleLogger) SetOutput(w io.Writer) *SimpleLogger {
	l.output = w
	return l
}

// WithPrefix 创建带前缀的子日志器
func (l *SimpleLogger) WithPrefix(prefix string) Logger {
	newLogger := &SimpleLogger{
		prefix: l.prefix + "/" + prefix,
		level:  l.level,
		output: l.output,
		fields: make(map[string]interface{}),
	}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

// WithField 创建带字段的子日志器
func (l *SimpleLogger) WithField(key string, value interface{}) Logger {
	newLogger := &SimpleLogger{
		prefix: l.prefix,
		level:  l.level,
		output: l.output,
		fields: make(map[string]interface{}),
	}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (l *SimpleLogger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)

	// 构建字段字符串
	fieldStr := ""
	if len(l.fields) > 0 {
		for k, v := range l.fields {
			fieldStr += fmt.Sprintf(" %s=%v", k, v)
		}
	}

	fmt.Fprintf(l.output, "[%s] [%s] [%s]%s %s\n",
		timestamp, levelNames[level], l.prefix, fieldStr, message)
}

func (l *SimpleLogger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

func (l *SimpleLogger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

func (l *SimpleLogger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

func (l *SimpleLogger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// ==================== 全局日志器 ====================

var defaultLogger = NewLogger("cscan")

// SetDefaultLevel 设置默认日志级别
func SetDefaultLevel(level Level) {
	defaultLogger.SetLevel(level)
}

// Debug 全局Debug日志
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

// Info 全局Info日志
func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

// Warn 全局Warn日志
func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

// Error 全局Error日志
func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

// ==================== Noop Logger ====================

// NoopLogger 空日志器（用于禁用日志）
type NoopLogger struct{}

func (l *NoopLogger) Debug(format string, args ...interface{}) {}
func (l *NoopLogger) Info(format string, args ...interface{})  {}
func (l *NoopLogger) Warn(format string, args ...interface{})  {}
func (l *NoopLogger) Error(format string, args ...interface{}) {}
func (l *NoopLogger) WithPrefix(prefix string) Logger          { return l }
func (l *NoopLogger) WithField(key string, value interface{}) Logger { return l }

// NewNoopLogger 创建空日志器
func NewNoopLogger() Logger {
	return &NoopLogger{}
}

// ==================== Callback Logger ====================

// CallbackLogger 回调日志器（用于将日志发送到其他地方）
type CallbackLogger struct {
	callback func(level, message string)
	prefix   string
}

// NewCallbackLogger 创建回调日志器
func NewCallbackLogger(callback func(level, message string)) *CallbackLogger {
	return &CallbackLogger{
		callback: callback,
	}
}

func (l *CallbackLogger) log(level, format string, args ...interface{}) {
	if l.callback == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	if l.prefix != "" {
		message = fmt.Sprintf("[%s] %s", l.prefix, message)
	}
	l.callback(level, message)
}

func (l *CallbackLogger) Debug(format string, args ...interface{}) {
	l.log("DEBUG", format, args...)
}

func (l *CallbackLogger) Info(format string, args ...interface{}) {
	l.log("INFO", format, args...)
}

func (l *CallbackLogger) Warn(format string, args ...interface{}) {
	l.log("WARN", format, args...)
}

func (l *CallbackLogger) Error(format string, args ...interface{}) {
	l.log("ERROR", format, args...)
}

func (l *CallbackLogger) WithPrefix(prefix string) Logger {
	newPrefix := prefix
	if l.prefix != "" {
		newPrefix = l.prefix + "/" + prefix
	}
	return &CallbackLogger{
		callback: l.callback,
		prefix:   newPrefix,
	}
}

func (l *CallbackLogger) WithField(key string, value interface{}) Logger {
	// CallbackLogger 不支持字段，返回自身
	return l
}
