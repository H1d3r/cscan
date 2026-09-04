package svc

import (
	"reflect"
	"testing"

	"cscan/internal/model"
)

// Validates: Requirements 2.10, 3.15.
func TestNormalizeWorkerPhaseSummaryRecordsLegacySummaryMissing(t *testing.T) {
	got := normalizeWorkerPhaseSummary("sub-1", "端口扫描", 1, nil, nil)
	if got.SubTaskId != "sub-1" || got.Phase != "portscan" || got.Status != "UNKNOWN" || got.Weight != 1 {
		t.Fatalf("legacy summary normalization = %#v", got)
	}
	if !reflect.DeepEqual(got.ReasonCodes, []string{"legacy_summary_missing"}) {
		t.Fatalf("legacy reason codes = %#v", got.ReasonCodes)
	}
}

func TestNormalizeWorkerPhaseSummaryAcceptsTaskSummaryFallback(t *testing.T) {
	key := model.TaskPhaseReportKey("sub-1", "poc")
	taskSummary := &model.TaskScanSummary{
		Outcome:                 model.TaskStatusSuccess, // advisory only; finalizer recomputes it
		VulnerabilityConclusion: model.VulnerabilityConclusionNotEvaluated,
		Assets:                  3,
		Phases: map[string]model.TaskPhaseSummary{
			key: {
				SubTaskId: "sub-1", Phase: "poc", Status: model.TaskPhaseUncovered,
				Coverage:    model.TaskPhaseCoverage{Input: 3, Uncovered: 3},
				ReasonCodes: []string{"zero_coverage"},
			},
		},
		WarningCodes: []string{"zero_coverage", "poc_uncovered"},
	}

	got := normalizeWorkerPhaseSummary("sub-1", "漏洞扫描", 2, nil, taskSummary)
	if got.Status != model.TaskPhaseUncovered || got.Assets != 3 || got.Weight != 2 || got.VulnerabilityConclusion != model.VulnerabilityConclusionNotEvaluated {
		t.Fatalf("task summary fallback = %#v", got)
	}
	if !reflect.DeepEqual(got.ReasonCodes, []string{"zero_coverage", "poc_uncovered"}) {
		t.Fatalf("merged reason codes = %#v", got.ReasonCodes)
	}

	aggregated := model.AggregateTaskScanSummary(model.TaskStatusStarted, 2, map[string]model.TaskPhaseSummary{key: got})
	if aggregated.Outcome != model.TaskStatusPartial {
		t.Fatalf("worker SUCCESS advisory must not override server aggregation: %#v", aggregated)
	}
}

func TestNormalizeWorkerPhaseSummaryPrefersPhaseResult(t *testing.T) {
	phase := &model.TaskPhaseSummary{Phase: "fingerprint", Status: model.TaskPhasePartial, ReasonCodes: []string{"timeout"}}
	taskSummary := &model.TaskScanSummary{Outcome: model.TaskStatusSuccess, Phases: map[string]model.TaskPhaseSummary{
		"other": {Phase: "fingerprint", Status: model.TaskPhaseComplete},
	}}
	got := normalizeWorkerPhaseSummary("sub-1", "指纹识别", 1, phase, taskSummary)
	if got.Status != model.TaskPhasePartial || !reflect.DeepEqual(got.ReasonCodes, []string{"timeout"}) {
		t.Fatalf("phaseResult precedence = %#v", got)
	}
}

func TestWorkerPartialIsTerminalButNotSuccess(t *testing.T) {
	if !isWorkerTerminalState(model.TaskStatusPartial) {
		t.Fatal("PARTIAL must receive terminal cleanup")
	}
	if model.TaskStatusPartial == model.TaskStatusSuccess {
		t.Fatal("PARTIAL must remain distinct from SUCCESS")
	}
}
