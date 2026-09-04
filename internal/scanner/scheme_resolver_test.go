package scanner

import "testing"

// TestResolveSchemeEvidencePriority covers task 3.7's required examples and
// conflict behavior without performing any network access.
// **Validates: Requirements 2.4, 2.7, 3.7**
func TestResolveSchemeEvidencePriority(t *testing.T) {
	tests := []struct {
		name          string
		evidence      []SchemeEvidence
		wantScheme    string
		wantAlternate string
		wantKind      string
		wantConflict  bool
		wantEvidence  bool
	}{
		{
			name: "verified HTTP on 443 beats HTTPS port hint",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTPS, Kind: SchemeEvidencePortHint},
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
			},
			wantScheme: SchemeHTTP, wantAlternate: SchemeHTTPS,
			wantKind: SchemeEvidenceSuccessfulResponse, wantConflict: true, wantEvidence: true,
		},
		{
			name: "verified HTTPS on nonstandard port beats HTTP port hint",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTP, Kind: SchemeEvidencePortHint},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 204},
			},
			wantScheme: SchemeHTTPS, wantAlternate: SchemeHTTP,
			wantKind: SchemeEvidenceSuccessfulResponse, wantConflict: true, wantEvidence: true,
		},
		{
			name: "dual success prefers explicit input and records alternate",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceExplicitInput},
			},
			wantScheme: SchemeHTTPS, wantAlternate: SchemeHTTP,
			wantKind: SchemeEvidenceSuccessfulResponse, wantConflict: true, wantEvidence: true,
		},
		{
			name: "dual success without explicit input keeps first high quality response",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 301},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
			},
			wantScheme: SchemeHTTP, wantAlternate: SchemeHTTPS,
			wantKind: SchemeEvidenceSuccessfulResponse, wantConflict: true, wantEvidence: true,
		},
		{
			name: "one protocol success ignores timeout and wrong service",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceSuccessfulResponse, Success: false, ErrorClass: ReasonTimeout},
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceScannerService},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
			},
			wantScheme: SchemeHTTPS, wantAlternate: SchemeHTTP,
			wantKind: SchemeEvidenceSuccessfulResponse, wantConflict: true, wantEvidence: true,
		},
		{
			name: "dual timeout has no usable evidence",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceSuccessfulResponse, Success: false, ErrorClass: ReasonTimeout},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceSuccessfulResponse, Success: false, ErrorClass: ReasonTimeout},
			},
		},
		{
			name: "same rank conflict uses response quality",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceSuccessfulResponse, Success: true, ErrorClass: "incomplete"},
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 404},
			},
			wantScheme: SchemeHTTP, wantAlternate: SchemeHTTPS,
			wantKind: SchemeEvidenceSuccessfulResponse, wantConflict: true, wantEvidence: true,
		},
		{
			name: "TLS evidence beats wrong scanner service",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceScannerService},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceTLSHandshake, Success: true},
			},
			wantScheme: SchemeHTTPS, wantAlternate: SchemeHTTP,
			wantKind: SchemeEvidenceTLSHandshake, wantConflict: true, wantEvidence: true,
		},
		{
			name: "same rank explicit conflict is stable",
			evidence: []SchemeEvidence{
				{Scheme: SchemeHTTP, Kind: SchemeEvidenceExplicitInput},
				{Scheme: SchemeHTTPS, Kind: SchemeEvidenceExplicitInput},
			},
			wantScheme: SchemeHTTP, wantAlternate: SchemeHTTPS,
			wantKind: SchemeEvidenceExplicitInput, wantConflict: true, wantEvidence: true,
		},
		{
			name:     "no evidence returns unresolved result",
			evidence: nil,
		},
		{
			name: "invalid and unknown evidence is ignored",
			evidence: []SchemeEvidence{
				{Scheme: "ftp", Kind: SchemeEvidenceExplicitInput},
				{Scheme: SchemeHTTPS, Kind: "unknown", Success: true},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ResolveScheme(test.evidence)
			if got.Scheme != test.wantScheme {
				t.Errorf("Scheme = %q, want %q", got.Scheme, test.wantScheme)
			}
			if got.AlternateScheme != test.wantAlternate {
				t.Errorf("AlternateScheme = %q, want %q", got.AlternateScheme, test.wantAlternate)
			}
			if got.SelectedEvidence.Kind != test.wantKind {
				t.Errorf("SelectedEvidence.Kind = %q, want %q", got.SelectedEvidence.Kind, test.wantKind)
			}
			if got.Conflict != test.wantConflict {
				t.Errorf("Conflict = %v, want %v", got.Conflict, test.wantConflict)
			}
			if got.HasEvidence != test.wantEvidence {
				t.Errorf("HasEvidence = %v, want %v", got.HasEvidence, test.wantEvidence)
			}
		})
	}
}

// TestResolveSchemePriorityLadder checks each adjacent priority boundary so a
// lower-ranked hint can never replace verified protocol evidence.
// **Validates: Requirements 2.4, 3.7**
func TestResolveSchemePriorityLadder(t *testing.T) {
	tests := []struct {
		name       string
		higherKind string
		lowerKind  string
	}{
		{name: "response over TLS", higherKind: SchemeEvidenceSuccessfulResponse, lowerKind: SchemeEvidenceTLSHandshake},
		{name: "TLS over explicit input", higherKind: SchemeEvidenceTLSHandshake, lowerKind: SchemeEvidenceExplicitInput},
		{name: "explicit input over service", higherKind: SchemeEvidenceExplicitInput, lowerKind: SchemeEvidenceScannerService},
		{name: "service over port hint", higherKind: SchemeEvidenceScannerService, lowerKind: SchemeEvidencePortHint},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			higher := SchemeEvidence{Scheme: SchemeHTTP, Kind: test.higherKind, Success: true, StatusCode: 200}
			lower := SchemeEvidence{Scheme: SchemeHTTPS, Kind: test.lowerKind, Success: true, StatusCode: 200}
			got := ResolveScheme([]SchemeEvidence{lower, higher})
			if got.Scheme != SchemeHTTP || got.SelectedEvidence.Kind != test.higherKind {
				t.Fatalf("ResolveScheme() = scheme %q kind %q, want http from %q", got.Scheme, got.SelectedEvidence.Kind, test.higherKind)
			}
			if !got.Conflict || got.AlternateScheme != SchemeHTTPS {
				t.Fatalf("conflict = %v alternate = %q, want true/https", got.Conflict, got.AlternateScheme)
			}
		})
	}
}
