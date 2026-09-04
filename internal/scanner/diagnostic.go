package scanner

import (
	"net/url"
	"strings"
)

// PhaseStatus is the execution conclusion for an enabled scan phase. It is
// derived from coverage rather than from whether the phase produced assets.
type PhaseStatus string

const (
	PhaseComplete             PhaseStatus = "COMPLETE"
	PhasePartial              PhaseStatus = "PARTIAL"
	PhaseUncovered            PhaseStatus = "UNCOVERED"
	PhaseFailed               PhaseStatus = "FAILED"
	PhaseSkippedNotApplicable PhaseStatus = "SKIPPED_NOT_APPLICABLE"
	PhaseCanceled             PhaseStatus = "CANCELED"
)

// Stable diagnostic reason codes. Aggregation must use these codes instead of
// matching scanner-specific error strings.
const (
	ReasonCanceled              = "canceled"
	ReasonTimeout               = "timeout"
	ReasonExecutionError        = "execution_error"
	ReasonParseError            = "parse_error"
	ReasonPartialOutput         = "partial_output"
	ReasonNoOutput              = "no_output"
	ReasonNoMatch               = "no_match"
	ReasonNotHTTP               = "not_http"
	ReasonUnconfirmed           = "unconfirmed"
	ReasonZeroCoverage          = "zero_coverage"
	ReasonNotApplicable         = "not_applicable"
	ReasonTemplateUnavailable   = "template_unavailable"
	ReasonTemplateNoMatch       = "template_no_match"
	ReasonTemplateInvalid       = "template_invalid"
	ReasonPortThresholdExceeded = "port_threshold_exceeded"
	ReasonScreenshotFailed      = "screenshot_failed"
	ReasonPersistenceDegraded   = "result_persistence_degraded"
)

const (
	// MaxTargetDiagnostics keeps scanner diagnostics bounded before they are
	// attached to a task summary or persisted.
	MaxTargetDiagnostics = 100
	// MaxDiagnosticMetadataFields limits persisted, per-target metadata.
	MaxDiagnosticMetadataFields = 16
	maxDiagnosticTextLength     = 256
)

var knownReasonCodes = map[string]struct{}{
	ReasonCanceled: {}, ReasonTimeout: {}, ReasonExecutionError: {},
	ReasonParseError: {}, ReasonPartialOutput: {}, ReasonNoOutput: {},
	ReasonNoMatch: {}, ReasonNotHTTP: {}, ReasonUnconfirmed: {}, ReasonZeroCoverage: {}, ReasonNotApplicable: {},
	ReasonTemplateUnavailable: {}, ReasonTemplateNoMatch: {},
	ReasonTemplateInvalid: {}, ReasonPortThresholdExceeded: {},
	ReasonScreenshotFailed: {}, ReasonPersistenceDegraded: {},
}

// Coverage records explicit execution outcomes. Succeeded means a defined,
// completed phase conclusion; it does not imply an asset or vulnerability was
// found.
type Coverage struct {
	Input, Attempted, Succeeded int
	TimedOut, Failed, Skipped   int
	Uncovered, Unconfirmed      int
	ZeroUpdate                  int
}

// TargetDiagnostic is a bounded description of an exceptional target outcome.
// Metadata is internal scanner transport data and must be filtered through
// ForPersistence before it is written outside the worker process.
type TargetDiagnostic struct {
	Target     string                 `json:"target,omitempty" bson:"target,omitempty"`
	Host       string                 `json:"host,omitempty" bson:"host,omitempty"`
	Port       int                    `json:"port,omitempty" bson:"port,omitempty"`
	Outcome    string                 `json:"outcome,omitempty" bson:"outcome,omitempty"`
	ReasonCode string                 `json:"reasonCode,omitempty" bson:"reason_code,omitempty"`
	Message    string                 `json:"message,omitempty" bson:"message,omitempty"`
	DurationMs int64                  `json:"durationMs,omitempty" bson:"duration_ms,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
}

// ScanDiagnostic carries scanner-level execution evidence separately from
// business results. It remains optional to preserve existing ScanResult users.
type ScanDiagnostic struct {
	Phase        string             `json:"phase,omitempty" bson:"phase,omitempty"`
	Status       PhaseStatus        `json:"status,omitempty" bson:"status,omitempty"`
	Coverage     Coverage           `json:"coverage" bson:"coverage"`
	Targets      []TargetDiagnostic `json:"targets,omitempty" bson:"targets,omitempty"`
	WarningCodes []string           `json:"warningCodes,omitempty" bson:"warning_codes,omitempty"`
}

// IsKnownReasonCode reports whether code is safe for aggregation and durable
// diagnostic storage.
func IsKnownReasonCode(code string) bool {
	_, ok := knownReasonCodes[code]
	return ok
}

// ForPersistence creates a bounded, safe diagnostic representation. It drops
// free-form messages and all metadata except the fixed, scalar allowlist, so
// response bodies, templates, cookies, custom headers, commands, and
// credentials cannot be persisted through diagnostics.
func (d *ScanDiagnostic) ForPersistence() *ScanDiagnostic {
	if d == nil {
		return nil
	}

	persisted := &ScanDiagnostic{
		Phase:    boundedText(d.Phase),
		Status:   d.Status,
		Coverage: d.Coverage,
	}
	persisted.WarningCodes = knownCodes(d.WarningCodes)
	for _, target := range d.Targets {
		if len(persisted.Targets) == MaxTargetDiagnostics {
			break
		}
		if !IsKnownReasonCode(target.ReasonCode) {
			continue
		}
		persisted.Targets = append(persisted.Targets, TargetDiagnostic{
			Target:     sanitizedTarget(target.Target),
			Host:       boundedText(target.Host),
			Port:       target.Port,
			Outcome:    boundedText(target.Outcome),
			ReasonCode: target.ReasonCode,
			DurationMs: target.DurationMs,
			Metadata:   safeMetadata(target.Metadata),
		})
	}
	return persisted
}

func knownCodes(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		if !IsKnownReasonCode(code) {
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

var persistedMetadataKeys = map[string]struct{}{
	"source": {}, "process_outcome": {}, "exit_code": {}, "file_bytes": {},
	"stdout_bytes": {}, "parsed_bytes": {}, "total_lines": {}, "valid_lines": {},
	"invalid_lines": {}, "duplicate_lines": {}, "accepted_ports": {}, "output_file_empty": {},
	"attempted_schemes": {}, "selected_scheme": {}, "evidence_kind": {}, "conflict": {},
	"requested": {}, "loaded": {}, "invalid": {}, "asset_count": {}, "template_count": {},
	"scanned_assets": {}, "vulnerabilities": {},
}

func safeMetadata(metadata map[string]interface{}) map[string]interface{} {
	if len(metadata) == 0 {
		return nil
	}
	result := make(map[string]interface{}, MaxDiagnosticMetadataFields)
	for key, value := range metadata {
		if len(result) == MaxDiagnosticMetadataFields {
			break
		}
		if _, allowed := persistedMetadataKeys[key]; !allowed {
			continue
		}
		switch typed := value.(type) {
		case string:
			result[key] = boundedText(typed)
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			result[key] = typed
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sanitizedTarget(target string) string {
	parsed, err := url.Parse(target)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		parsed.Fragment = ""
		return boundedText(parsed.String())
	}
	return boundedText(target)
}

func boundedText(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	if len(value) > maxDiagnosticTextLength {
		return value[:maxDiagnosticTextLength]
	}
	return value
}
