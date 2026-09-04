package worker

import (
	"sync"
	"testing"

	"cscan/internal/model"
	"cscan/internal/scanner"
)

type inMemorySummaryFinalizer struct {
	mu            sync.Mutex
	expected      int
	done          int
	phases        map[string]model.TaskPhaseSummary
	terminal      string
	notifications int
}

func newInMemorySummaryFinalizer(expected int) *inMemorySummaryFinalizer {
	return &inMemorySummaryFinalizer{expected: expected, phases: make(map[string]model.TaskPhaseSummary)}
}

func (f *inMemorySummaryFinalizer) report(subtask string, result PhaseResult, weight int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := model.TaskPhaseReportKey(subtask, result.Phase)
	if _, exists := f.phases[key]; !exists {
		if weight <= 0 {
			weight = 1
		}
		f.done += weight
		if f.done > f.expected {
			f.done = f.expected
		}
	}
	summary := result.TaskSummary(subtask)
	summary.Weight = weight
	f.phases[key] = summary
}

func (f *inMemorySummaryFinalizer) tryFinalize(current string) model.TaskScanSummary {
	f.mu.Lock()
	defer f.mu.Unlock()
	summary := model.AggregateTaskScanSummary(current, f.expected, f.phases)
	if f.done < f.expected || summary.PhaseCount < f.expected || f.terminal != "" {
		return summary
	}
	f.terminal = summary.Outcome
	f.notifications++
	return summary
}

// Validates: Requirements 2.10, 3.14.
func TestTaskOutcomeDuplicateReportsAndConcurrentFinalizationAreIdempotent(t *testing.T) {
	f := newInMemorySummaryFinalizer(2)
	port := NewPhaseResult("portscan", scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1}, false)
	poc := NewPhaseResult("poc", scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1}, false)
	poc.VulnerabilityConclusion = model.VulnerabilityConclusionNoFindings

	var reportWG sync.WaitGroup
	for i := 0; i < 20; i++ {
		reportWG.Add(1)
		go func() { defer reportWG.Done(); f.report("sub-1", port, 1) }()
	}
	reportWG.Wait()
	if f.done != 1 || len(f.phases) != 1 {
		t.Fatalf("duplicate report changed progress: done=%d phases=%d", f.done, len(f.phases))
	}
	f.report("sub-1", poc, 1)

	var finalizeWG sync.WaitGroup
	for i := 0; i < 20; i++ {
		finalizeWG.Add(1)
		go func() { defer finalizeWG.Done(); f.tryFinalize(model.TaskStatusStarted) }()
	}
	finalizeWG.Wait()
	if f.terminal != model.TaskStatusSuccess {
		t.Fatalf("terminal=%s, want SUCCESS", f.terminal)
	}
	if f.notifications != 1 {
		t.Fatalf("notifications=%d, want exactly 1", f.notifications)
	}
}

// Validates: Requirements 2.9, 2.10.
func TestTaskOutcomeProgressAtTotalBeforeSummariesDoesNotFinalize(t *testing.T) {
	f := newInMemorySummaryFinalizer(2)
	f.done = 2 // progress counter arrived first
	port := NewPhaseResult("portscan", scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1}, false)
	f.report("sub-1", port, 1)
	got := f.tryFinalize(model.TaskStatusStarted)
	if f.terminal != "" || f.notifications != 0 {
		t.Fatalf("finalized with missing summary: terminal=%s notifications=%d", f.terminal, f.notifications)
	}
	if got.Outcome == model.TaskStatusSuccess {
		t.Fatalf("missing summary produced SUCCESS")
	}
}

// Validates: Requirements 2.9, 2.10.
func TestTaskOutcomePocZeroCoverageFinalizesPartial(t *testing.T) {
	f := newInMemorySummaryFinalizer(1)
	poc := NewPhaseResult("poc", scanner.Coverage{Input: 3, Uncovered: 3}, false, scanner.ReasonZeroCoverage)
	poc.VulnerabilityConclusion = model.VulnerabilityConclusionNotEvaluated
	f.report("sub-1", poc, 1)
	got := f.tryFinalize(model.TaskStatusStarted)
	if got.Outcome != model.TaskStatusPartial || f.terminal != model.TaskStatusPartial {
		t.Fatalf("outcome=%s terminal=%s, want PARTIAL", got.Outcome, f.terminal)
	}
	if got.VulnerabilityConclusion != model.VulnerabilityConclusionNotEvaluated {
		t.Fatalf("conclusion=%s, want NOT_EVALUATED", got.VulnerabilityConclusion)
	}
}

func TestTaskOutcomeStopPausePrecedence(t *testing.T) {
	f := newInMemorySummaryFinalizer(1)
	failed := NewPhaseResult("portscan", scanner.Coverage{Input: 1, Attempted: 1, Failed: 1}, false)
	f.report("sub-1", failed, 1)
	if got := f.tryFinalize(model.TaskStatusStopped); got.Outcome != model.TaskStatusStopped {
		t.Fatalf("STOP outcome=%s", got.Outcome)
	}

	g := newInMemorySummaryFinalizer(1)
	g.report("sub-1", failed, 1)
	if got := g.tryFinalize(model.TaskStatusPaused); got.Outcome != model.TaskStatusPaused {
		t.Fatalf("PAUSE outcome=%s", got.Outcome)
	}
}
