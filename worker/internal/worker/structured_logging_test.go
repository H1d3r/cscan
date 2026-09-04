package worker

import (
	"strings"
	"testing"
	"time"

	"cscan/internal/model"
	"cscan/internal/scanner"

	"go.mongodb.org/mongo-driver/bson"
)

// Validates: Requirements 2.1, 2.2, 2.5, 2.6, 2.8, 2.9, 2.10, 3.15.
func TestStructuredLogSanitizesSensitiveFieldsAndKeepsNaabuCounts(t *testing.T) {
	event, phase, outcome, fields := sanitizeStructuredEvent(
		scanner.EventNaabuParseComplete, "naabu", "timeout",
		map[string]interface{}{
			"target": "https://user:password@fixture.example.test:443/path?token=secret",
			"source": "stdout", "file_bytes": 0, "stdout_bytes": 173, "parsed_bytes": 173,
			"accepted_ports": 4, "error_detail": "Authorization token=top-secret",
			"response_body": "private body", "template_content": "private template",
			"cookie": "session=secret", "credentials": "admin:secret",
			"custom_headers": []string{"X-Token: secret"}, "command_args": []string{"--header", "secret"},
		},
	)
	if event != scanner.EventNaabuParseComplete || phase != "naabu" || outcome != "timeout" {
		t.Fatalf("event tuple=(%q,%q,%q)", event, phase, outcome)
	}
	if fields["source"] != "stdout" || fields["stdout_bytes"] != 173 || fields["parsed_bytes"] != 173 || fields["accepted_ports"] != 4 {
		t.Fatalf("required Naabu fields lost: %#v", fields)
	}
	serialized, err := bson.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(serialized))
	for _, secret := range []string{"top-secret", "private body", "private template", "session=secret", "admin:secret", "x-token", "password@", "token=secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("sensitive value %q leaked in %#v", secret, fields)
		}
	}
	if fields["error_detail"] != "[redacted_sensitive_error]" || fields["target"] != "https://fixture.example.test:443/path" {
		t.Fatalf("redaction mismatch: %#v", fields)
	}
}

func TestPocTemplateLoadEventAndPhaseCoverageAreAuditable(t *testing.T) {
	var gotEvent, gotPhase, gotOutcome string
	var gotFields map[string]interface{}
	summary := PocCoverageResult{}
	appendPocGroupEvent(&summary, PocGroupResult{
		GroupKey: "nginx", Tags: []string{"nginx"}, AssetCount: 3,
		RequestedTemplates: 2, ValidTemplates: 0, InvalidTemplates: 1,
		TemplateLoadOutcome: TemplateLoadNoMatch, TemplateSource: "local_store",
		Status: scanner.PhaseUncovered, ReasonCode: scanner.ReasonTemplateNoMatch,
	}, func(event, phase, outcome string, fields map[string]interface{}) {
		gotEvent, gotPhase, gotOutcome, gotFields = event, phase, outcome, fields
	})
	if len(summary.Groups) != 1 || gotEvent != EventPocTemplateLoad || gotPhase != "poc" || gotOutcome != string(scanner.PhaseUncovered) {
		t.Fatalf("POC event tuple=(%q,%q,%q), summary=%+v", gotEvent, gotPhase, gotOutcome, summary)
	}
	if gotFields["asset_count"] != 3 || gotFields["requested"] != 2 || gotFields["loaded"] != 0 || gotFields["invalid"] != 1 || gotFields["reason_code"] != scanner.ReasonTemplateNoMatch {
		t.Fatalf("POC coverage fields=%#v", gotFields)
	}

	phaseResult := PhaseResult{
		Phase: "poc", Status: scanner.PhaseUncovered,
		Coverage:                scanner.Coverage{Input: 3, Uncovered: 3},
		ReasonCodes:             []string{scanner.ReasonZeroCoverage},
		VulnerabilityConclusion: model.VulnerabilityConclusionNotEvaluated,
	}
	phaseFields := phaseEventFields(phaseResult)
	if phaseFields["input"] != 3 || phaseFields["uncovered"] != 3 || phaseFields["status"] != "UNCOVERED" || phaseFields["vulnerability_conclusion"] != model.VulnerabilityConclusionNotEvaluated {
		t.Fatalf("phase coverage fields=%#v", phaseFields)
	}
}

func TestTaskFinalizedEventContainsOutcomeAndIncompletePhases(t *testing.T) {
	summary := &model.TaskScanSummary{
		Outcome: model.TaskStatusPartial, Complete: false, Assets: 4, Vulnerabilities: 0,
		VulnerabilityConclusion: model.VulnerabilityConclusionNotEvaluated,
		WarningCodes:            []string{scanner.ReasonTimeout, scanner.ReasonZeroCoverage},
		Phases: map[string]model.TaskPhaseSummary{
			"port": {Phase: "portscan", Status: model.TaskPhasePartial},
			"poc":  {Phase: "poc", Status: model.TaskPhaseUncovered},
			"fp":   {Phase: "fingerprint", Status: model.TaskPhaseComplete},
		},
	}
	fields := taskFinalizedEventFields(summary)
	if fields["outcome"] != model.TaskStatusPartial || fields["assets"] != 4 || fields["vulnerabilities"] != 0 || fields["complete"] != false {
		t.Fatalf("task outcome fields=%#v", fields)
	}
	incomplete, ok := fields["incomplete_phases"].([]string)
	if !ok || strings.Join(incomplete, ",") != "poc,portscan" {
		t.Fatalf("incomplete phases=%#v", fields["incomplete_phases"])
	}
}

func TestLegacyLogConsumerIgnoresOptionalStructuredFields(t *testing.T) {
	type legacyLogDoc struct {
		Worker     string    `bson:"worker"`
		TaskId     string    `bson:"task_id,omitempty"`
		Level      string    `bson:"level"`
		Msg        string    `bson:"msg"`
		CreateTime time.Time `bson:"create_time"`
		Seq        int64     `bson:"seq"`
	}
	created := time.Unix(1700000000, 0)
	encoded, err := bson.Marshal(mongoLogDoc{
		Worker: "worker-a", TaskId: "task-a", Level: LevelInfo, Msg: "human readable",
		CreateTime: created, Seq: 7, Event: EventPhaseComplete, Phase: "poc", Outcome: "UNCOVERED",
		Fields: map[string]interface{}{"input": 2, "uncovered": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	var legacy legacyLogDoc
	if err := bson.Unmarshal(encoded, &legacy); err != nil {
		t.Fatalf("legacy consumer rejected optional fields: %v", err)
	}
	if legacy.Worker != "worker-a" || legacy.TaskId != "task-a" || legacy.Level != LevelInfo || legacy.Msg != "human readable" || legacy.Seq != 7 {
		t.Fatalf("legacy fields changed: %#v", legacy)
	}
}

func TestMetricsNeverUseHostOrTaskAsLabels(t *testing.T) {
	var metrics scanMetrics
	metrics.record(scanner.EventSchemeProbeComplete, "scheme", "CONFIRMED", map[string]interface{}{
		"host": "high-cardinality.example.test", "port": 443, "conflict": true,
	})
	metrics.record(EventTaskFinalized, "task", model.TaskStatusPartial, map[string]interface{}{
		"task_id": "task-high-cardinality", "outcome": model.TaskStatusPartial,
	})
	for key := range metrics.snapshot() {
		if strings.Contains(key, "high-cardinality") || strings.Contains(key, "task_id") || strings.Contains(key, "host=") {
			t.Fatalf("high-cardinality metric label leaked: %s", key)
		}
	}
}

func TestExceptionalTargetEventsAreBoundedAndSampled(t *testing.T) {
	worker := &Worker{}
	logged := 0
	for i := 0; i < 2000; i++ {
		if worker.shouldPersistStructuredEvent("task-1", scanner.EventNmapPortResult) {
			logged++
		}
	}
	if logged <= 20 || logged > 100 {
		t.Fatalf("sampled logs=%d, want >20 and <=100", logged)
	}
}
