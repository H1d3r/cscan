package model

import (
	"encoding/json"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// Validates: Requirements 2.10, 3.15.
func TestMainTaskHistoricalDocumentsNeedNoMigration(t *testing.T) {
	legacyJSON := []byte(`{"taskId":"legacy-task","status":"SUCCESS","progress":100,"result":"Assets:1 Vuls:0 Duration:1s","config":"{\"targetTimeout\":120}","unknownFutureField":{"enabled":true}}`)
	var fromJSON MainTask
	if err := json.Unmarshal(legacyJSON, &fromJSON); err != nil {
		t.Fatal(err)
	}
	assertLegacyTaskCore(t, fromJSON)
	if fromJSON.ScanSummary != nil {
		t.Fatalf("historical JSON scan summary = %#v, want nil", fromJSON.ScanSummary)
	}

	legacyBSON, err := bson.Marshal(bson.M{
		"task_id": "legacy-task", "status": "SUCCESS", "progress": 100,
		"result": "Assets:1 Vuls:0 Duration:1s", "config": `{"targetTimeout":120}`,
		"unknown_future_field": bson.M{"enabled": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var fromBSON MainTask
	if err := bson.Unmarshal(legacyBSON, &fromBSON); err != nil {
		t.Fatal(err)
	}
	assertLegacyTaskCore(t, fromBSON)
	if fromBSON.ScanSummary != nil {
		t.Fatalf("historical BSON scan summary = %#v, want nil", fromBSON.ScanSummary)
	}

	encoded, err := json.Marshal(MainTask{TaskId: "legacy-task", Status: TaskStatusSuccess, Progress: 100, Result: "Assets:1 Vuls:0 Duration:1s"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["scanSummary"]; ok {
		t.Fatalf("nil scanSummary must be omitted: %s", encoded)
	}
	for _, field := range []string{"taskId", "status", "progress", "result"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("legacy task field %q missing: %s", field, encoded)
		}
	}
}

func assertLegacyTaskCore(t *testing.T, task MainTask) {
	t.Helper()
	if task.TaskId != "legacy-task" || task.Status != TaskStatusSuccess || task.Progress != 100 || task.Result != "Assets:1 Vuls:0 Duration:1s" || task.Config != `{"targetTimeout":120}` {
		t.Fatalf("legacy core fields changed: %#v", task)
	}
}

func TestMainTaskScanSummaryJSONAndBSONRoundTrip(t *testing.T) {
	key := TaskPhaseReportKey("sub-1", "poc")
	phase := TaskPhaseSummary{
		SubTaskId: "sub-1", Phase: "poc", Status: TaskPhaseUncovered,
		Coverage:    TaskPhaseCoverage{Input: 2, Uncovered: 2},
		ReasonCodes: []string{"zero_coverage"}, Weight: 1,
	}
	original := MainTask{
		TaskId: "new-task", Status: TaskStatusPartial, Progress: 100,
		Result: "Assets:2 Vuls:0 Duration:1s Coverage:partial:poc",
		ScanSummary: &TaskScanSummary{
			Outcome: TaskStatusPartial, VulnerabilityConclusion: VulnerabilityConclusionNotEvaluated,
			Phases: map[string]TaskPhaseSummary{key: phase}, PhaseCount: 1, Assets: 2,
			WarningCodes: []string{"poc_uncovered", "zero_coverage"},
		},
	}

	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var jsonRoundTrip MainTask
	if err := json.Unmarshal(jsonBytes, &jsonRoundTrip); err != nil {
		t.Fatal(err)
	}
	assertTaskSummaryRoundTrip(t, jsonRoundTrip, key)

	bsonBytes, err := bson.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var bsonRoundTrip MainTask
	if err := bson.Unmarshal(bsonBytes, &bsonRoundTrip); err != nil {
		t.Fatal(err)
	}
	assertTaskSummaryRoundTrip(t, bsonRoundTrip, key)
}

func assertTaskSummaryRoundTrip(t *testing.T, task MainTask, key string) {
	t.Helper()
	if task.Status != TaskStatusPartial || task.Status == TaskStatusSuccess || task.ScanSummary == nil {
		t.Fatalf("PARTIAL task round trip changed: %#v", task)
	}
	if task.ScanSummary.Outcome != TaskStatusPartial || task.ScanSummary.VulnerabilityConclusion != VulnerabilityConclusionNotEvaluated || task.ScanSummary.Phases[key].Status != TaskPhaseUncovered {
		t.Fatalf("scan summary round trip changed: %#v", task.ScanSummary)
	}
}

func TestAssetVulnerabilityAndFingerprintCompatibilityTags(t *testing.T) {
	legacyAssetBSON, err := bson.Marshal(bson.M{
		"authority": "legacy.example:443", "host": "legacy.example", "port": 443,
		"status": "200", "taskId": "legacy-task", "unknown_future_field": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var asset Asset
	if err := bson.Unmarshal(legacyAssetBSON, &asset); err != nil {
		t.Fatal(err)
	}
	if asset.Authority != "legacy.example:443" || asset.Port != 443 || asset.HttpStatus != "200" || asset.TaskId != "legacy-task" || asset.FingerprintFindings != nil {
		t.Fatalf("legacy asset fields changed: %#v", asset)
	}

	candidate := FingerprintFinding{
		FingerprintID: "candidate-1", Name: "Candidate", Source: "custom", RawMatched: true,
		Decision: "CANDIDATE", Confidence: 25, ReasonCode: "weak_evidence",
	}
	asset.FingerprintFindings = FingerprintFindings{candidate}
	assetJSON, err := json.Marshal(asset)
	if err != nil {
		t.Fatal(err)
	}
	var assetRoundTrip Asset
	if err := json.Unmarshal(assetJSON, &assetRoundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(assetRoundTrip.FingerprintFindings, FingerprintFindings{candidate}) {
		t.Fatalf("candidate fingerprint round trip = %#v", assetRoundTrip.FingerprintFindings)
	}

	legacyVulJSON := []byte(`{"authority":"legacy.example:443","host":"legacy.example","port":443,"url":"https://legacy.example:443","pocFile":"legacy.yaml","source":"nuclei","severity":"high","extra":"","result":"matched","taskId":"legacy-task","unknownFutureField":true}`)
	var vulnerability Vul
	if err := json.Unmarshal(legacyVulJSON, &vulnerability); err != nil {
		t.Fatal(err)
	}
	if vulnerability.Authority != "legacy.example:443" || vulnerability.Port != 443 || vulnerability.PocFile != "legacy.yaml" || vulnerability.TaskId != "legacy-task" {
		t.Fatalf("legacy vulnerability fields changed: %#v", vulnerability)
	}

	assetBSON, err := bson.Marshal(asset)
	if err != nil {
		t.Fatal(err)
	}
	var assetFields bson.M
	if err := bson.Unmarshal(assetBSON, &assetFields); err != nil {
		t.Fatal(err)
	}
	if _, ok := assetFields["fingerprint_findings"]; !ok {
		t.Fatalf("candidate findings missing compatible BSON name: %#v", assetFields)
	}
	if assetFields["status"] != "200" || assetFields["taskId"] != "legacy-task" {
		t.Fatalf("legacy asset BSON names changed: %#v", assetFields)
	}

	vulBSON, err := bson.Marshal(vulnerability)
	if err != nil {
		t.Fatal(err)
	}
	var vulFields bson.M
	if err := bson.Unmarshal(vulBSON, &vulFields); err != nil {
		t.Fatal(err)
	}
	if vulFields["pocfile"] != "legacy.yaml" || vulFields["task_id"] != "legacy-task" {
		t.Fatalf("legacy vulnerability BSON names changed: %#v", vulFields)
	}
}
