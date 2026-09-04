package model

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func phase(subtask, name, status string, succeeded int) TaskPhaseSummary {
	return TaskPhaseSummary{
		SubTaskId:     subtask,
		Phase:         name,
		Status:        status,
		Coverage:      TaskPhaseCoverage{Input: 1, Attempted: 1, Succeeded: succeeded},
		UsableResults: succeeded > 0,
		Weight:        1,
	}
}

// Validates: Requirements 2.10, 3.14, 3.15.
func TestAggregateTaskScanSummaryStatusCombinations(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		expected int
		phases   map[string]TaskPhaseSummary
		want     string
		complete bool
	}{
		{"all complete", TaskStatusStarted, 2, map[string]TaskPhaseSummary{"a": phase("s", "portscan", TaskPhaseComplete, 1), "b": phase("s", "poc", TaskPhaseComplete, 1)}, TaskStatusSuccess, true},
		{"not applicable allowed", TaskStatusStarted, 1, map[string]TaskPhaseSummary{"a": phase("s", "poc", TaskPhaseSkippedNotApplicable, 0)}, TaskStatusSuccess, true},
		{"partial with usable result", TaskStatusStarted, 1, map[string]TaskPhaseSummary{"a": phase("s", "portscan", TaskPhasePartial, 1)}, TaskStatusPartial, false},
		{"uncovered", TaskStatusStarted, 1, map[string]TaskPhaseSummary{"a": phase("s", "poc", TaskPhaseUncovered, 0)}, TaskStatusPartial, false},
		{"failed without result", TaskStatusStarted, 1, map[string]TaskPhaseSummary{"a": phase("s", "portscan", TaskPhaseFailed, 0)}, TaskStatusFailure, false},
		{"failed with retained result", TaskStatusStarted, 1, map[string]TaskPhaseSummary{"a": phase("s", "portscan", TaskPhaseFailed, 1)}, TaskStatusPartial, false},
		{"missing summary", TaskStatusStarted, 2, map[string]TaskPhaseSummary{"a": phase("s", "portscan", TaskPhaseComplete, 1)}, TaskStatusPartial, false},
		{"stopped wins", TaskStatusStopped, 1, map[string]TaskPhaseSummary{"a": phase("s", "portscan", TaskPhaseComplete, 1)}, TaskStatusStopped, false},
		{"paused wins", TaskStatusPaused, 1, map[string]TaskPhaseSummary{"a": phase("s", "portscan", TaskPhaseComplete, 1)}, TaskStatusPaused, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AggregateTaskScanSummary(tt.current, tt.expected, tt.phases)
			if got.Outcome != tt.want || got.Complete != tt.complete {
				t.Fatalf("outcome=%s complete=%t, want %s/%t", got.Outcome, got.Complete, tt.want, tt.complete)
			}
		})
	}
}

// Validates: Requirements 2.9, 2.10.
func TestAggregateTaskScanSummaryPocUncoveredNeverSucceeds(t *testing.T) {
	p := phase("sub-1", "poc", TaskPhaseUncovered, 0)
	p.VulnerabilityConclusion = VulnerabilityConclusionNotEvaluated
	got := AggregateTaskScanSummary(TaskStatusStarted, 1, map[string]TaskPhaseSummary{"poc": p})
	if got.Outcome != TaskStatusPartial {
		t.Fatalf("outcome=%s, want PARTIAL", got.Outcome)
	}
	if got.VulnerabilityConclusion != VulnerabilityConclusionNotEvaluated {
		t.Fatalf("conclusion=%s, want NOT_EVALUATED", got.VulnerabilityConclusion)
	}
}

// Validates: Requirements 3.15.
func TestTaskScanSummaryOptionalCompatibilityAndResultPrefix(t *testing.T) {
	legacy := []byte(`{"taskId":"legacy","status":"SUCCESS","result":"Assets:2 Vuls:0 Duration:3s"}`)
	var task MainTask
	if err := json.Unmarshal(legacy, &task); err != nil {
		t.Fatal(err)
	}
	if task.ScanSummary != nil {
		t.Fatalf("legacy summary=%#v, want nil", task.ScanSummary)
	}
	if _, err := bson.Marshal(task); err != nil {
		t.Fatalf("BSON marshal legacy task: %v", err)
	}

	summary := AggregateTaskScanSummary(TaskStatusStarted, 1, map[string]TaskPhaseSummary{
		"p": phase("s", "poc", TaskPhaseUncovered, 0),
	})
	result := AppendCoverageHint("Assets:2 Vuls:0 Duration:3s", summary)
	if !strings.HasPrefix(result, "Assets:2 Vuls:0 Duration:3s") {
		t.Fatalf("legacy prefix changed: %q", result)
	}
	if !strings.Contains(result, "Coverage:partial:poc") {
		t.Fatalf("coverage hint missing: %q", result)
	}
}

func TestTaskPhaseReportKeyIsStableAndMongoSafe(t *testing.T) {
	a := TaskPhaseReportKey("main.sub-$1", "poc.scan")
	b := TaskPhaseReportKey("main.sub-$1", "poc.scan")
	if a != b || strings.ContainsAny(a, ".$") {
		t.Fatalf("unsafe or unstable report key: %q / %q", a, b)
	}
}

func TestAggregateTaskScanSummaryWeightedCompletionCannotHideMissingPhase(t *testing.T) {
	completion := phase("sub-1", "complete", TaskPhaseComplete, 1)
	completion.Weight = 2
	got := AggregateTaskScanSummary(TaskStatusStarted, 2, map[string]TaskPhaseSummary{
		"complete": completion,
	})
	if got.Outcome != TaskStatusPartial || got.Complete {
		t.Fatalf("weighted completion outcome=%s complete=%t, want PARTIAL/false", got.Outcome, got.Complete)
	}
	if !containsString(got.WarningCodes, "weighted_completion_compensation") || !containsString(got.WarningCodes, "summary_missing") {
		t.Fatalf("weighted completion warnings=%v", got.WarningCodes)
	}
}

func TestIsTerminalTaskStatusIncludesLegacyCompleted(t *testing.T) {
	for _, status := range []string{TaskStatusSuccess, TaskStatusPartial, TaskStatusFailure, TaskStatusStopped, TaskStatusRevoked, TaskStatusPaused, TaskStatusLegacyCompleted} {
		if !IsTerminalTaskStatus(status) {
			t.Fatalf("status %q was not treated as terminal", status)
		}
	}
	if IsTerminalTaskStatus(TaskStatusStarted) {
		t.Fatal("STARTED must remain reportable")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
