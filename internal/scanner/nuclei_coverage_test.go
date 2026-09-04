package scanner

import "testing"

// Nuclei must expose applicable-but-unscanned assets as zero coverage instead
// of returning an indistinguishable empty vulnerability result. No command or
// network request is started by this fixture.
// **Validates: Requirements 2.8, 2.9**
func TestNucleiCoverageNoApplicableTargetsIsUncovered(t *testing.T) {
	s := NewNucleiScanner()
	result, err := s.Scan(t.Context(), &ScanConfig{
		Assets: []*Asset{{Host: "ssh-only.test", Port: 22, Service: "ssh", IsHTTP: false}},
		Options: &NucleiOptions{
			CustomTemplates: []string{"id: unused\ninfo:\n  name: unused fixture\n  author: test\n  severity: info\n"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected no-target error: %v", err)
	}
	if result == nil || result.Diagnostic == nil {
		t.Fatalf("missing zero-coverage diagnostic: %#v", result)
	}
	if result.Diagnostic.Status != PhaseUncovered {
		t.Fatalf("status = %s, want UNCOVERED", result.Diagnostic.Status)
	}
	coverage := result.Diagnostic.Coverage
	if coverage.Input != 1 || coverage.Attempted != 0 || coverage.Succeeded != 0 || coverage.Uncovered != 1 {
		t.Fatalf("coverage = %+v", coverage)
	}
	if len(result.Diagnostic.WarningCodes) != 1 || result.Diagnostic.WarningCodes[0] != ReasonZeroCoverage {
		t.Fatalf("warning codes = %v", result.Diagnostic.WarningCodes)
	}
}
