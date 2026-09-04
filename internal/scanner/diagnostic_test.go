package scanner

import (
	"encoding/json"
	"strings"
	"testing"
)

// **Validates: Requirements 2.1, 2.2, 2.5, 2.8, 2.9, 3.15**
func TestScanDiagnosticForPersistenceBoundsAndFiltersSensitiveData(t *testing.T) {
	diagnostic := &ScanDiagnostic{
		Phase:        "httpx",
		Status:       PhasePartial,
		Coverage:     Coverage{Input: 101, Attempted: 101, Succeeded: 1, TimedOut: 100},
		WarningCodes: []string{ReasonTimeout, "untrusted_error_text", ReasonTimeout},
	}
	for i := 0; i < MaxTargetDiagnostics+1; i++ {
		diagnostic.Targets = append(diagnostic.Targets, TargetDiagnostic{
			Target:     "https://user:secret@example.test/path?token=secret#fragment",
			Host:       "example.test",
			Port:       443,
			Outcome:    "TIMEOUT",
			ReasonCode: ReasonTimeout,
			Message:    "response body, cookie, and command data must not persist",
			DurationMs: 30,
			Metadata: map[string]interface{}{
				"source":           "stdout",
				"file_bytes":       123,
				"response_body":    "private response",
				"template_content": "private template",
				"cookie":           "session=secret",
				"custom_header":    "X-Token: secret",
				"credentials":      "user:secret",
				"command":          "scanner --token secret",
			},
		})
	}

	persisted := diagnostic.ForPersistence()
	if len(persisted.Targets) != MaxTargetDiagnostics {
		t.Fatalf("persisted target count = %d, want %d", len(persisted.Targets), MaxTargetDiagnostics)
	}
	if got := persisted.WarningCodes; len(got) != 1 || got[0] != ReasonTimeout {
		t.Fatalf("warning codes = %#v, want only known deduplicated timeout", got)
	}
	first := persisted.Targets[0]
	if first.Message != "" {
		t.Fatalf("free-form message persisted: %q", first.Message)
	}
	if strings.Contains(first.Target, "secret") || strings.Contains(first.Target, "?") || strings.Contains(first.Target, "#") {
		t.Fatalf("target credentials/query/fragment persisted: %q", first.Target)
	}
	if len(first.Metadata) > MaxDiagnosticMetadataFields {
		t.Fatalf("metadata count = %d, limit = %d", len(first.Metadata), MaxDiagnosticMetadataFields)
	}
	for _, forbidden := range []string{"response_body", "template_content", "cookie", "custom_header", "credentials", "command"} {
		if _, found := first.Metadata[forbidden]; found {
			t.Fatalf("forbidden metadata %q persisted: %#v", forbidden, first.Metadata)
		}
	}
	if first.Metadata["source"] != "stdout" || first.Metadata["file_bytes"] != 123 {
		t.Fatalf("safe metadata not retained: %#v", first.Metadata)
	}
}

// **Validates: Requirements 3.15**
func TestScanResultLegacyInitializationKeepsNilDiagnostic(t *testing.T) {
	asset := &Asset{Host: "legacy.example.test", Port: 443}
	vulnerability := &Vulnerability{Host: "legacy.example.test", Port: 443, VulName: "legacy"}
	legacy := ScanResult{MainTaskId: "task-1", Assets: []*Asset{asset}, Vulnerabilities: []*Vulnerability{vulnerability}}

	if legacy.Diagnostic != nil {
		t.Fatalf("legacy literal diagnostic = %#v, want nil", legacy.Diagnostic)
	}
	if legacy.Assets[0] != asset || legacy.Vulnerabilities[0] != vulnerability {
		t.Fatal("legacy assets or vulnerabilities changed when diagnostic was added")
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "diagnostic") {
		t.Fatalf("nil optional diagnostic serialized: %s", encoded)
	}

	var decoded ScanResult
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Diagnostic != nil || len(decoded.Assets) != 1 || len(decoded.Vulnerabilities) != 1 {
		t.Fatalf("legacy round trip incompatible: %#v", decoded)
	}
}
