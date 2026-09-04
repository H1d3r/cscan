package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
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

func sanitizeIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 64 {
		value = value[:64]
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
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

// scanMetrics stores only fixed, low-cardinality keys. Host and task IDs are
// deliberately unavailable to this API.
type scanMetrics struct{ counters sync.Map }

var globalScanMetrics scanMetrics

func (m *scanMetrics) increment(key string, delta uint64) {
	counter, _ := m.counters.LoadOrStore(key, &atomic.Uint64{})
	counter.(*atomic.Uint64).Add(delta)
}

func (m *scanMetrics) record(event, phase, outcome string, fields map[string]interface{}) {
	switch event {
	case EventPhaseComplete:
		m.increment(fmt.Sprintf("scan_phase_total{phase=%s,status=%s}", metricPhase(phase), metricStatus(outcome)), 1)
		if valueAsInt(fields["timed_out"]) > 0 {
			m.increment(fmt.Sprintf("scan_target_timeout_total{scanner=%s}", metricScanner(phase)), uint64(valueAsInt(fields["timed_out"])))
		}
	case "naabu_parse_complete":
		if outcome != "success" && valueAsInt(fields["parsed_bytes"]) > 0 {
			m.increment(fmt.Sprintf("scanner_partial_output_total{scanner=naabu,source=%s}", metricSource(valueAsString(fields["source"]))), 1)
		}
		if count := valueAsInt(fields["invalid_lines"]); count > 0 {
			m.increment("scanner_parse_invalid_total{scanner=naabu}", uint64(count))
		}
	case "nmap_port_result":
		if outcome != "open" && outcome != "closed" {
			m.increment(fmt.Sprintf("service_identify_unconfirmed_total{reason=%s}", metricReason(valueAsString(fields["error_code"]))), 1)
		}
	case "scheme_probe_complete":
		if conflict, _ := fields["conflict"].(bool); conflict {
			m.increment(fmt.Sprintf("scheme_conflict_total{port_class=%s}", metricPortClass(valueAsInt(fields["port"]))), 1)
		}
	case "httpx_phase_complete":
		if count := valueAsInt(fields["zero_update"]); count > 0 {
			m.increment("http_probe_zero_update_total{reason=unconfirmed}", uint64(count))
		}
	case "fingerprint_decision":
		if strings.EqualFold(valueAsString(fields["decision"]), "candidate") {
			m.increment(fmt.Sprintf("fingerprint_candidate_total{reason=%s}", metricReason(valueAsString(fields["reason_code"]))), 1)
		}
	case EventPocTemplateLoad:
		if valueAsInt(fields["loaded"]) == 0 {
			m.increment(fmt.Sprintf("poc_uncovered_assets_total{reason=%s}", metricReason(valueAsString(fields["reason_code"]))), uint64(maxInt(1, valueAsInt(fields["asset_count"]))))
		}
	case EventTaskFinalized:
		m.increment(fmt.Sprintf("task_outcome_total{outcome=%s}", metricStatus(outcome)), 1)
	}
}

func (m *scanMetrics) snapshot() map[string]uint64 {
	result := make(map[string]uint64)
	m.counters.Range(func(key, value interface{}) bool {
		result[key.(string)] = value.(*atomic.Uint64).Load()
		return true
	})
	return result
}

func structuredMetricsJSON() string {
	snapshot := globalScanMetrics.snapshot()
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make([]struct {
		Metric string `json:"metric"`
		Value  uint64 `json:"value"`
	}, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, struct {
			Metric string `json:"metric"`
			Value  uint64 `json:"value"`
		}{key, snapshot[key]})
	}
	encoded, _ := json.Marshal(ordered)
	return string(encoded)
}

func valueAsString(value interface{}) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func valueAsInt(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	default:
		return 0
	}
}

func metricPhase(value string) string {
	switch sanitizeIdentifier(value) {
	case "naabu", "nmap", "scheme", "httpx", "fingerprint", "poc", "portscan", "portidentify", "domainscan", "brutescan", "dirscan", "jsfinder", "execution", "complete":
		return sanitizeIdentifier(value)
	default:
		return "other"
	}
}

func metricScanner(value string) string {
	switch metricPhase(value) {
	case "naabu", "nmap", "httpx", "fingerprint", "poc", "portscan", "portidentify":
		return metricPhase(value)
	default:
		return "other"
	}
}

func metricStatus(value string) string {
	switch sanitizeIdentifier(value) {
	case "complete", "partial", "uncovered", "failed", "skipped_not_applicable", "canceled", "success", "failure", "stopped", "paused":
		return sanitizeIdentifier(value)
	default:
		return "other"
	}
}

func metricSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "file", "stdout", "none":
		return strings.ToLower(strings.TrimSpace(value))
	case "file_stdout", "file+stdout":
		return "file_stdout"
	default:
		return "other"
	}
}

func metricReason(value string) string {
	value = sanitizeIdentifier(value)
	switch value {
	case "timeout", "execution_error", "parse_error", "no_output", "no_match", "unconfirmed", "zero_coverage", "template_unavailable", "template_no_match", "template_invalid", "untagged_assets", "nmap_timeout", "nmap_launch_error", "nmap_nonzero_exit", "nmap_xml_parse_error", "nmap_no_host_record", "insufficient_evidence", "weak_fanout", "conflict":
		return value
	default:
		return "other"
	}
}

func metricPortClass(port int) string {
	switch port {
	case 80, 8080, 8000:
		return "http_common"
	case 443, 8443, 9443:
		return "tls_common"
	default:
		return "other"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
