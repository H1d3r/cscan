package worker

import (
	"encoding/json"
	"testing"
)

// Validates: Requirements 3.15.
func TestWorkerSubTaskDoneReqAcceptsLegacyPayloadWithoutSummary(t *testing.T) {
	var req WorkerSubTaskDoneReq
	if err := json.Unmarshal([]byte(`{"taskId":"sub-1","mainTaskId":"0123456789abcdef01234567","phase":"端口扫描","isCompleted":false,"incrAmount":1,"unknownFutureField":true}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.TaskId != "sub-1" || req.PhaseResult != nil || req.TaskSummary != nil {
		t.Fatalf("legacy request decoded unexpectedly: %#v", req)
	}
}

func TestWorkerSubTaskDoneReqRoundTripsOptionalSummaries(t *testing.T) {
	payload := []byte(`{"taskId":"sub-1","mainTaskId":"0123456789abcdef01234567","phase":"漏洞扫描","incrAmount":1,"phaseResult":{"phase":"poc","status":"UNCOVERED","coverage":{"input":2,"uncovered":2},"reasonCodes":["zero_coverage"]},"taskSummary":{"outcome":"PARTIAL","vulnerabilityConclusion":"NOT_EVALUATED","phases":{"report":{"subTaskId":"sub-1","phase":"poc","status":"UNCOVERED","coverage":{"input":2,"uncovered":2}}},"warningCodes":["zero_coverage"]}}`)
	var req WorkerSubTaskDoneReq
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatal(err)
	}
	if req.PhaseResult == nil || req.PhaseResult.Phase != "poc" || req.PhaseResult.Status != "UNCOVERED" || req.PhaseResult.Coverage.Uncovered != 2 {
		t.Fatalf("phase summary not preserved: %#v", req.PhaseResult)
	}
	if req.TaskSummary == nil || req.TaskSummary.Outcome != "PARTIAL" || req.TaskSummary.VulnerabilityConclusion != "NOT_EVALUATED" {
		t.Fatalf("task summary not preserved: %#v", req.TaskSummary)
	}
}
