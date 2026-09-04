package types

import (
	"encoding/json"
	"testing"

	"cscan/internal/model"
)

// Validates: Requirements 2.10, 3.15.
func TestMainTaskAPIKeepsLegacyCoreFieldsAndOmitsNilScanSummary(t *testing.T) {
	task := MainTask{
		Id: "id", TaskId: "task-id", Name: "legacy", Target: "example.test",
		Status: model.TaskStatusSuccess, Progress: 100, Result: "Assets:1 Vuls:0 Duration:1s",
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "taskId", "name", "target", "status", "progress", "result"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("legacy API field %q missing: %s", field, encoded)
		}
	}
	if _, ok := fields["scanSummary"]; ok {
		t.Fatalf("nil scanSummary must be omitted: %s", encoded)
	}
}

func TestMainTaskAPIExposesPartialScanSummary(t *testing.T) {
	key := model.TaskPhaseReportKey("sub-1", "poc")
	task := MainTask{
		Status: model.TaskStatusPartial,
		ScanSummary: &model.TaskScanSummary{
			Outcome:                 model.TaskStatusPartial,
			VulnerabilityConclusion: model.VulnerabilityConclusionNotEvaluated,
			Phases: map[string]model.TaskPhaseSummary{
				key: {SubTaskId: "sub-1", Phase: "poc", Status: model.TaskPhaseUncovered},
			},
			WarningCodes: []string{"poc_uncovered"},
		},
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MainTask
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != model.TaskStatusPartial || decoded.Status == model.TaskStatusSuccess || decoded.ScanSummary == nil || decoded.ScanSummary.Phases[key].Status != model.TaskPhaseUncovered {
		t.Fatalf("PARTIAL API summary changed: %#v", decoded)
	}
}
