package worker

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	mongoLogChannelSize   = 2000 // 日志通道缓冲
	mongoLogBatchSize     = 100  // 批量写入大小
	mongoLogFlushInterval = 2 * time.Second
	maxStructuredFields   = 32
	maxStructuredText     = 256
	maxStructuredList     = 16
)

const (
	EventPocTemplateLoad = "poc_template_load"
	EventPhaseComplete   = "phase_complete"
	EventTaskFinalized   = "task_finalized"
)

// mongoLogDoc Worker 日志 MongoDB 文档。新增结构化字段均为可选，旧消费者
// 继续只读取 level/msg/create_time 等字段即可。
type mongoLogDoc struct {
	Worker     string                 `bson:"worker" json:"worker"`
	TaskId     string                 `bson:"task_id,omitempty" json:"taskId,omitempty"`
	Level      string                 `bson:"level" json:"level"`
	Msg        string                 `bson:"msg" json:"msg"`
	CreateTime time.Time              `bson:"create_time" json:"createTime"`
	Seq        int64                  `bson:"seq" json:"seq"`
	Event      string                 `bson:"event,omitempty" json:"event,omitempty"`
	Phase      string                 `bson:"phase,omitempty" json:"phase,omitempty"`
	Outcome    string                 `bson:"outcome,omitempty" json:"outcome,omitempty"`
	Fields     map[string]interface{} `bson:"fields,omitempty" json:"fields,omitempty"`
}

// MongoLogger 将 Worker 日志批量直写 MongoDB。
type MongoLogger struct {
	coll       *mongo.Collection
	workerName string
	logCh      chan mongoLogDoc
	closeChan  chan struct{}
	closeOnce  sync.Once
	wg         sync.WaitGroup
	seq        atomic.Int64
}

// NewMongoLogger 创建 MongoDB 日志写入器。
func NewMongoLogger(db *mongo.Database, workerName string) *MongoLogger {
	m := &MongoLogger{
		coll:       db.Collection("worker_log"),
		workerName: workerName,
		logCh:      make(chan mongoLogDoc, mongoLogChannelSize),
		closeChan:  make(chan struct{}),
	}
	m.wg.Add(1)
	go m.flushLoop()
	return m
}

// Write 写入一条兼容文本日志。
func (m *MongoLogger) Write(level, taskId, msg string) {
	m.write(level, taskId, msg, "", "", "", nil)
}

// WriteEvent writes the same human-readable message with optional structured
// facts. The persisted fields pass through a strict per-event allowlist.
func (m *MongoLogger) WriteEvent(level, taskId, msg, event, phase, outcome string, fields map[string]interface{}) {
	m.write(level, taskId, msg, event, phase, outcome, fields)
}

func (m *MongoLogger) write(level, taskId, msg, event, phase, outcome string, fields map[string]interface{}) {
	if m == nil {
		return
	}
	event, phase, outcome, fields = sanitizeStructuredEvent(event, phase, outcome, fields)
	doc := mongoLogDoc{
		Worker: m.workerName, TaskId: taskId, Level: level, Msg: msg,
		CreateTime: time.Now().Local(), Seq: m.seq.Add(1),
		Event: event, Phase: phase, Outcome: outcome, Fields: fields,
	}
	select {
	case m.logCh <- doc:
	default:
		logx.Errorf("[MongoLogger] log channel full, dropping log: level=%s worker=%s", level, m.workerName)
	}
}

// SetWorkerName 更新 worker 名称（rename 后调用）。
func (m *MongoLogger) SetWorkerName(name string) {
	if m != nil {
		m.workerName = name
	}
}

// Close 关闭写入器，flush 剩余日志。
func (m *MongoLogger) Close() {
	m.closeOnce.Do(func() { close(m.closeChan) })
	m.wg.Wait()
}

func (m *MongoLogger) flushLoop() {
	defer m.wg.Done()
	batch := make([]mongoLogDoc, 0, mongoLogBatchSize)
	ticker := time.NewTicker(mongoLogFlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		docs := make([]interface{}, len(batch))
		for i, d := range batch {
			docs[i] = d
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := m.coll.InsertMany(ctx, docs)
		cancel()
		if err != nil {
			logx.Errorf("[MongoLogger] InsertMany failed: %v, retaining %d logs for retry", err, len(batch))
			if len(batch) >= mongoLogBatchSize*3 {
				logx.Errorf("[MongoLogger] batch backlog too large (%d), dropping to prevent OOM", len(batch))
				batch = batch[:0]
			}
			return
		}
		batch = batch[:0]
	}

	for {
		select {
		case doc := <-m.logCh:
			batch = append(batch, doc)
			if len(batch) >= mongoLogBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-m.closeChan:
			for {
				select {
				case doc := <-m.logCh:
					batch = append(batch, doc)
				default:
					flush()
					return
				}
			}
		}
	}
}

var structuredEventFields = map[string]map[string]struct{}{
	"naabu_parse_complete":  fieldSet("target", "process_outcome", "exit_code", "source", "file_bytes", "stdout_bytes", "parsed_bytes", "total_lines", "valid_lines", "invalid_lines", "duplicate_lines", "accepted_ports", "output_file_empty", "duration_ms", "reason_code", "error_detail"),
	"nmap_port_result":      fieldSet("host", "port", "outcome", "service", "error_code", "duration_ms", "error_detail"),
	"scheme_probe_complete": fieldSet("host", "port", "attempted_schemes", "selected_scheme", "evidence_kind", "http_outcome", "https_outcome", "conflict", "duration_ms", "reason_code"),
	"httpx_phase_complete":  fieldSet("input", "attempted", "succeeded", "timed_out", "failed", "no_output", "parse_failed", "zero_update", "status", "unconfirmed"),
	"fingerprint_decision":  fieldSet("host", "port", "fingerprint_id", "source", "confidence", "evidence_channels", "decision", "reason_code"),
	EventPocTemplateLoad:    fieldSet("group_key", "tags", "asset_count", "requested", "loaded", "invalid", "source", "outcome", "reason_code", "scanned_assets", "vulnerabilities"),
	EventPhaseComplete:      fieldSet("input", "attempted", "succeeded", "timed_out", "failed", "skipped", "uncovered", "unconfirmed", "zero_update", "status", "reason_codes", "assets", "vulnerabilities", "vulnerability_conclusion"),
	EventTaskFinalized:      fieldSet("outcome", "assets", "vulnerabilities", "incomplete_phases", "warning_codes", "vulnerability_conclusion", "complete"),
}

func fieldSet(keys ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		result[key] = struct{}{}
	}
	return result
}

func sanitizeStructuredEvent(event, phase, outcome string, fields map[string]interface{}) (string, string, string, map[string]interface{}) {
	allowed, ok := structuredEventFields[event]
	if !ok {
		return "", "", "", nil
	}
	phase = sanitizeStructuredIdentifier(phase)
	outcome = sanitizeStructuredIdentifier(outcome)
	clean := make(map[string]interface{}, minInt(len(fields), maxStructuredFields))
	for key, value := range fields {
		if len(clean) >= maxStructuredFields {
			break
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		if safe, ok := sanitizeStructuredValue(key, value); ok {
			clean[key] = safe
		}
	}
	if len(clean) == 0 {
		clean = nil
	}
	return event, phase, outcome, clean
}

func sanitizeStructuredValue(key string, value interface{}) (interface{}, bool) {
	switch typed := value.(type) {
	case string:
		if key == "error_detail" {
			return sanitizeErrorDetail(typed), true
		}
		if key == "target" {
			return sanitizeLogTarget(typed), true
		}
		return truncateStructuredText(typed), true
	case []string:
		if len(typed) > maxStructuredList {
			typed = typed[:maxStructuredList]
		}
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, truncateStructuredText(item))
		}
		return result, true
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return typed, true
	default:
		return nil, false
	}
}

func sanitizeStructuredIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		value = value[:64]
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return "other"
		}
	}
	return value
}

func truncateStructuredText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxStructuredText {
		return value[:maxStructuredText] + "...[truncated]"
	}
	return value
}

func sanitizeLogTarget(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		value = parsed.String()
	}
	return truncateStructuredText(value)
}

func sanitizeErrorDetail(value string) string {
	lower := strings.ToLower(value)
	for _, marker := range []string{"authorization", "cookie", "password", "passwd", "credential", "secret", "token", "api_key", "apikey"} {
		if strings.Contains(lower, marker) {
			return "[redacted_sensitive_error]"
		}
	}
	return truncateStructuredText(value)
}
