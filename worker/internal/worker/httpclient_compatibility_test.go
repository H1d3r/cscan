package worker

import (
	"encoding/json"
	"testing"

	"cscan/internal/model"
)

// Validates: Requirements 2.10, 3.15.
func TestSubTaskDoneReqOmitsOptionalSummariesForLegacyAPI(t *testing.T) {
	encoded, err := json.Marshal(SubTaskDoneReq{
		TaskId: "sub-1", MainTaskId: "0123456789abcdef01234567", Phase: "端口扫描", IncrAmount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["phaseResult"]; ok {
		t.Fatalf("nil phaseResult must be omitted: %s", encoded)
	}
	if _, ok := fields["taskSummary"]; ok {
		t.Fatalf("nil taskSummary must be omitted: %s", encoded)
	}
	for _, field := range []string{"taskId", "mainTaskId", "phase", "isCompleted", "incrAmount"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("legacy core field %q missing: %s", field, encoded)
		}
	}
}

func TestSubTaskDoneReqRoundTripsNewSummaries(t *testing.T) {
	phase := model.TaskPhaseSummary{
		SubTaskId: "sub-1", Phase: "poc", Status: model.TaskPhaseUncovered,
		Coverage:    model.TaskPhaseCoverage{Input: 2, Uncovered: 2},
		ReasonCodes: []string{"zero_coverage"}, Weight: 1,
	}
	key := model.TaskPhaseReportKey("sub-1", "poc")
	original := SubTaskDoneReq{
		TaskId: "sub-1", MainTaskId: "0123456789abcdef01234567", Phase: "漏洞扫描", IncrAmount: 1,
		PhaseResult: &phase,
		TaskSummary: &model.TaskScanSummary{
			Outcome: model.TaskStatusPartial, VulnerabilityConclusion: model.VulnerabilityConclusionNotEvaluated,
			Phases: map[string]model.TaskPhaseSummary{key: phase}, PhaseCount: 1,
			WarningCodes: []string{"zero_coverage"},
		},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SubTaskDoneReq
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PhaseResult == nil || decoded.PhaseResult.Status != model.TaskPhaseUncovered || decoded.PhaseResult.Coverage.Uncovered != 2 {
		t.Fatalf("phaseResult round trip failed: %#v", decoded.PhaseResult)
	}
	if decoded.TaskSummary == nil || decoded.TaskSummary.Outcome != model.TaskStatusPartial || decoded.TaskSummary.Phases[key].Phase != "poc" {
		t.Fatalf("taskSummary round trip failed: %#v", decoded.TaskSummary)
	}
}
