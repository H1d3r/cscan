package worker

import (
	"testing"

	"cscan/internal/model"
	"cscan/internal/scanner"
)

// **Validates: Requirements 2.2, 2.5, 2.8, 2.9, 2.10, 3.15**
func TestStatusFromCoverage(t *testing.T) {
	tests := []struct {
		name     string
		coverage scanner.Coverage
		canceled bool
		want     scanner.PhaseStatus
	}{
		{
			name:     "complete when every applicable input has a defined result",
			coverage: scanner.Coverage{Input: 2, Attempted: 2, Succeeded: 2},
			want:     scanner.PhaseComplete,
		},
		{
			name:     "partial when valid results coexist with timeout",
			coverage: scanner.Coverage{Input: 2, Attempted: 2, Succeeded: 1, TimedOut: 1},
			want:     scanner.PhasePartial,
		},
		{
			name:     "uncovered when applicable inputs receive no effective attempts",
			coverage: scanner.Coverage{Input: 2, Uncovered: 2},
			want:     scanner.PhaseUncovered,
		},
		{
			name:     "failed when exceptional empty execution has no usable result",
			coverage: scanner.Coverage{Input: 2, Attempted: 2, Failed: 2},
			want:     scanner.PhaseFailed,
		},
		{
			name:     "skipped only when upstream confirms no applicable inputs",
			coverage: scanner.Coverage{},
			want:     scanner.PhaseSkippedNotApplicable,
		},
		{
			name:     "cancellation takes priority over otherwise complete coverage",
			coverage: scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1},
			canceled: true,
			want:     scanner.PhaseCanceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusFromCoverage(tt.coverage, tt.canceled); got != tt.want {
				t.Fatalf("StatusFromCoverage(%+v, %t) = %s, want %s", tt.coverage, tt.canceled, got, tt.want)
			}
		})
	}
}

// **Validates: Requirements 2.2, 3.3**
func TestStatusFromCoverageDistinguishesLegalAndExceptionalEmptyResults(t *testing.T) {
	// A zero-business-result scan is legal when its one applicable input has a
	// completed, defined conclusion. Asset count is deliberately absent here.
	legalEmpty := scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1}
	if got := StatusFromCoverage(legalEmpty, false); got != scanner.PhaseComplete {
		t.Fatalf("legal empty scan = %s, want %s", got, scanner.PhaseComplete)
	}

	// The same lack of assets after an exception has no completed conclusion.
	exceptionalEmpty := scanner.Coverage{Input: 1, Attempted: 1, TimedOut: 1}
	if got := StatusFromCoverage(exceptionalEmpty, false); got != scanner.PhaseFailed {
		t.Fatalf("exceptional empty scan = %s, want %s", got, scanner.PhaseFailed)
	}
}

// **Validates: Requirements 2.10, 3.15**
func TestNewPhaseResultUsesCoverageAndFiltersUnknownReasons(t *testing.T) {
	result := NewPhaseResult("poc", scanner.Coverage{Input: 1, Uncovered: 1}, false,
		scanner.ReasonZeroCoverage, "database error text: password=secret", scanner.ReasonZeroCoverage)

	if result.Status != scanner.PhaseUncovered {
		t.Fatalf("status = %s, want %s", result.Status, scanner.PhaseUncovered)
	}
	if len(result.ReasonCodes) != 1 || result.ReasonCodes[0] != scanner.ReasonZeroCoverage {
		t.Fatalf("reason codes = %#v, want only stable zero coverage", result.ReasonCodes)
	}
}

// **Validates: Requirements 2.10**
func TestMissingWorkerPhaseResultCannotAggregateToSuccess(t *testing.T) {
	result := missingPhaseResult("完成")
	if result.Phase != "execution" || result.Status != scanner.PhaseFailed {
		t.Fatalf("missing result = phase %q status %s, want execution/FAILED", result.Phase, result.Status)
	}

	summary := model.AggregateTaskScanSummary(model.TaskStatusStarted, 1, map[string]model.TaskPhaseSummary{
		model.TaskPhaseReportKey("sub-1", result.Phase): result.TaskSummary("sub-1"),
	})
	if summary.Outcome != model.TaskStatusFailure {
		t.Fatalf("missing phase summary outcome = %s, want FAILURE", summary.Outcome)
	}
}
