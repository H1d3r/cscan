package worker

import (
	"cscan/internal/model"
	"cscan/internal/scanner"
)

// PhaseResult is the worker-side, structured conclusion for one scan phase.
// The status is computed from explicit execution coverage, never from the
// number of emitted assets or vulnerabilities.
type PhaseResult struct {
	Phase                   string                  `json:"phase" bson:"phase"`
	Status                  scanner.PhaseStatus     `json:"status" bson:"status"`
	Coverage                scanner.Coverage        `json:"coverage" bson:"coverage"`
	ReasonCodes             []string                `json:"reasonCodes,omitempty" bson:"reason_codes,omitempty"`
	Diagnostic              *scanner.ScanDiagnostic `json:"diagnostic,omitempty" bson:"diagnostic,omitempty"`
	UsableResults           bool                    `json:"usableResults,omitempty" bson:"usable_results,omitempty"`
	Assets                  int                     `json:"assets,omitempty" bson:"assets,omitempty"`
	Vulnerabilities         int                     `json:"vulnerabilities,omitempty" bson:"vulnerabilities,omitempty"`
	VulnerabilityConclusion string                  `json:"vulnerabilityConclusion,omitempty" bson:"vulnerability_conclusion,omitempty"`
	ResultPrefix            string                  `json:"resultPrefix,omitempty" bson:"result_prefix,omitempty"`
}

// phaseReportAck separates durable phase acknowledgement from terminal task
// finalization. A report may be persisted while the aggregate transition is
// temporarily unavailable and therefore still require a retry.
type phaseReportAck struct {
	Recorded            bool
	LeaseClosed         bool
	Finalized           bool
	FinalizationPending bool
}

// NewPhaseResult constructs a phase result with a status derived solely from
// coverage and the explicit cancellation signal.
func NewPhaseResult(phase string, coverage scanner.Coverage, canceled bool, reasonCodes ...string) PhaseResult {
	return PhaseResult{
		Phase:       phase,
		Status:      StatusFromCoverage(coverage, canceled),
		Coverage:    coverage,
		ReasonCodes: knownReasonCodes(reasonCodes),
	}
}

// StatusFromCoverage is deliberately pure. A completed scan that finds no
// business results still supplies Succeeded coverage and is COMPLETE; an empty
// asset slice is never used as evidence of a successful or failed scan.
func StatusFromCoverage(coverage scanner.Coverage, canceled bool) scanner.PhaseStatus {
	if canceled {
		return scanner.PhaseCanceled
	}
	if coverage.Input == 0 {
		return scanner.PhaseSkippedNotApplicable
	}

	hasExceptionalOutcome := coverage.TimedOut > 0 || coverage.Failed > 0 ||
		coverage.Unconfirmed > 0 || coverage.Uncovered > 0 || coverage.Skipped > 0

	if coverage.Succeeded >= coverage.Input && !hasExceptionalOutcome {
		return scanner.PhaseComplete
	}
	if coverage.Succeeded > 0 {
		return scanner.PhasePartial
	}
	if coverage.Uncovered > 0 && coverage.Failed == 0 && coverage.TimedOut == 0 && coverage.Unconfirmed == 0 {
		return scanner.PhaseUncovered
	}
	if coverage.Attempted == 0 && coverage.Failed == 0 && coverage.TimedOut == 0 && coverage.Unconfirmed == 0 {
		return scanner.PhaseUncovered
	}
	return scanner.PhaseFailed
}

func knownReasonCodes(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		if !scanner.IsKnownReasonCode(code) {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	return result
}

func (r PhaseResult) TaskSummary(subTaskID string) model.TaskPhaseSummary {
	return model.TaskPhaseSummary{
		SubTaskId: subTaskID,
		Phase:     r.Phase,
		Status:    string(r.Status),
		Coverage: model.TaskPhaseCoverage{
			Input: r.Coverage.Input, Attempted: r.Coverage.Attempted, Succeeded: r.Coverage.Succeeded,
			TimedOut: r.Coverage.TimedOut, Failed: r.Coverage.Failed, Skipped: r.Coverage.Skipped,
			Uncovered: r.Coverage.Uncovered, Unconfirmed: r.Coverage.Unconfirmed,
		},
		ReasonCodes:             append([]string(nil), r.ReasonCodes...),
		UsableResults:           r.UsableResults,
		Assets:                  r.Assets,
		Vulnerabilities:         r.Vulnerabilities,
		VulnerabilityConclusion: r.VulnerabilityConclusion,
		ResultPrefix:            r.ResultPrefix,
		Weight:                  1,
	}
}

func canonicalTaskPhase(phase string) string {
	switch phase {
	case "子域名扫描":
		return "domainscan"
	case "端口扫描":
		return "portscan"
	case "端口识别":
		return "portidentify"
	case "指纹识别":
		return "fingerprint"
	case "弱口令扫描":
		return "brutescan"
	case "目录扫描":
		return "dirscan"
	case "JS扫描":
		return "jsfinder"
	case "漏洞扫描":
		return "poc"
	case "完成":
		return "complete"
	default:
		if phase == "" {
			return "unknown"
		}
		return phase
	}
}

func missingPhaseResult(phase string) PhaseResult {
	canonical := canonicalTaskPhase(phase)
	if canonical == "complete" || canonical == "subtask_complete" {
		canonical = "execution"
	}
	return PhaseResult{
		Phase:  canonical,
		Status: scanner.PhaseFailed,
		Coverage: scanner.Coverage{
			Input: 1, Attempted: 1, Failed: 1,
		},
	}
}

func PhaseResultFromDiagnostic(phase string, diagnostic *scanner.ScanDiagnostic, assets int) PhaseResult {
	if diagnostic == nil {
		result := NewPhaseResult(phase, scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1}, false)
		result.Assets = assets
		result.UsableResults = assets > 0 || result.Coverage.Succeeded > 0
		return result
	}
	result := PhaseResult{
		Phase: phase, Status: diagnostic.Status, Coverage: diagnostic.Coverage,
		ReasonCodes: append([]string(nil), diagnostic.WarningCodes...), Diagnostic: diagnostic,
		Assets: assets,
	}
	result.UsableResults = assets > 0 || diagnostic.Coverage.Succeeded > 0
	return result
}
