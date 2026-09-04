package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"cscan/internal/model"
)

func governanceEvidence(channel, strength string, complete bool) FingerprintEvidence {
	return FingerprintEvidence{Channel: channel, Strength: strength, Complete: complete, MatchedValueDigest: "sha256:test"}
}

// **Validates: Requirements 2.6, 3.9**
func TestFingerprintGovernanceConfirmsSingleStrongEvidence(t *testing.T) {
	input := FingerprintFindings{{Name: "StrongProduct", Source: "custom", RawMatched: true, Evidence: []FingerprintEvidence{governanceEvidence("favicon", "strong", true)}}}
	got := GovernFingerprintFindings(input)
	if len(got) != 1 || got[0].Decision != fingerprintDecisionConfirmed || got[0].ReasonCode != "confirmed_strong_evidence" {
		t.Fatalf("governed findings = %#v", got)
	}
	if input[0].Decision != "" || !input[0].RawMatched {
		t.Fatalf("governance mutated raw input: %#v", input[0])
	}
}

// **Validates: Requirements 2.6**
func TestFingerprintGovernanceKeepsLoneWeakEvidenceCandidate(t *testing.T) {
	got := GovernFingerprintFindings(FingerprintFindings{{Name: "WeakProduct", Source: "custom", RawMatched: true, Evidence: []FingerprintEvidence{governanceEvidence("body", "weak", true)}}})
	if len(got) != 1 || got[0].Decision != fingerprintDecisionCandidate || got[0].ReasonCode != "insufficient_evidence" {
		t.Fatalf("governed findings = %#v", got)
	}
}

// **Validates: Requirements 2.6**
func TestFingerprintGovernanceDowngradesCloseExclusiveConflict(t *testing.T) {
	got := GovernFingerprintFindings(FingerprintFindings{
		{Name: "ServerA", Source: "custom", RawMatched: true, ConflictGroup: "web-server", Evidence: []FingerprintEvidence{governanceEvidence("favicon", "strong", true)}},
		{Name: "ServerB", Source: "custom", RawMatched: true, ConflictGroup: "web-server", Evidence: []FingerprintEvidence{governanceEvidence("favicon", "strong", true)}},
	})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, finding := range got {
		if finding.Decision != fingerprintDecisionCandidate || finding.ReasonCode != "exclusive_conflict_close_score" {
			t.Fatalf("conflicting finding = %#v", finding)
		}
	}
}

// **Validates: Requirements 2.6, 3.9**
func TestFingerprintGovernanceDoesNotConfirmMissingResponse(t *testing.T) {
	engine := NewCustomFingerprintEngine([]*model.Fingerprint{{Name: "MissingResponse", Rule: `body!="forbidden"`, Enabled: true}})
	raw := engine.MatchWithEvidence(&FingerprintData{ResponseMissing: true})
	if len(raw) != 1 || !raw[0].RawMatched {
		t.Fatalf("raw findings = %#v, want preserved negation match", raw)
	}
	got := GovernFingerprintFindings(raw)
	if got[0].Decision != fingerprintDecisionCandidate || got[0].ReasonCode != "incomplete_evidence" {
		t.Fatalf("governed finding = %#v", got[0])
	}
}

// **Validates: Requirements 2.6, 3.10**
func TestFingerprintGovernanceAllowsNginxBootstrapCoexistence(t *testing.T) {
	got := GovernFingerprintFindings(FingerprintFindings{
		{Name: "Nginx", Source: "custom", RawMatched: true, Evidence: []FingerprintEvidence{governanceEvidence("header", "medium", true), governanceEvidence("body", "weak", true)}},
		{Name: "Bootstrap", Source: "wappalyzer", RawMatched: true, Evidence: []FingerprintEvidence{governanceEvidence("script", "medium", true), governanceEvidence("body", "weak", true)}},
	})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, finding := range got {
		if finding.Decision != fingerprintDecisionConfirmed {
			t.Fatalf("coexisting finding = %#v", finding)
		}
	}
}

// **Validates: Requirements 2.6, 3.9**
func TestFingerprintGovernanceMergesSameNameMultiSourceEvidence(t *testing.T) {
	got := GovernFingerprintFindings(FingerprintFindings{
		{Name: "Nginx", FingerprintID: "custom-id", Source: "custom", RawMatched: true, Evidence: []FingerprintEvidence{governanceEvidence("header", "medium", true)}},
		{Name: "nginx", FingerprintID: "wapp-id", Source: "wappalyzer", RawMatched: true, Evidence: []FingerprintEvidence{governanceEvidence("header", "medium", true)}},
	})
	if len(got) != 1 || got[0].Decision != fingerprintDecisionConfirmed {
		t.Fatalf("merged findings = %#v", got)
	}
	if got[0].Source != "custom+wappalyzer" || len(got[0].Evidence) != 2 {
		t.Fatalf("merged source/evidence = %#v", got[0])
	}
}

// TestFingerprintGovernancePipelineWeakFanout uses only a local HTTP fixture.
// It verifies the scanner integration boundary: raw weak matches remain
// auditable candidates, only the strong match enters App, and fan-out has a
// stable reason code. **Validates: Requirements 2.6, 3.9, 3.10**
func TestFingerprintGovernancePipelineWeakFanout(t *testing.T) {
	favicon := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")
	faviconHash := CalculateMMH3Hash(favicon)
	body := "login dashboard bootstrap generic portal"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "favicon") {
			_, _ = w.Write(favicon)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	fingerprints := []*model.Fingerprint{
		{Name: "StrongProduct", Rule: `icon_hash="` + faviconHash + `"`, Source: "custom", Enabled: true},
		{Name: "Kibana", Rule: `body="dashboard"`, Source: "custom", Enabled: true},
		{Name: "Bootstrap", Rule: `body="bootstrap"`, Source: "custom", Enabled: true},
		{Name: "LoginPage", Rule: `body="login"`, Source: "custom", Enabled: true},
		{Name: "GenericPortal", Rule: `body="portal"`, Source: "custom", Enabled: true},
	}
	scanner := NewFingerprintScanner()
	scanner.SetCustomFingerprintEngine(NewCustomFingerprintEngine(fingerprints))
	asset := &Asset{Host: parsed.Hostname(), Port: port, Service: "http", IsHTTP: true, Title: "fixture", HttpBody: body}
	scanner.runAdditionalFingerprint(context.Background(), asset, &FingerprintOptions{CustomEngine: true}, func(string, string, ...interface{}) {}, nil)

	if len(asset.App) != 1 || !strings.HasPrefix(asset.App[0], "StrongProduct[") {
		t.Fatalf("confirmed apps = %#v, want only strong product", asset.App)
	}
	candidates := 0
	for _, finding := range asset.FingerprintFindings {
		if finding.Name == "StrongProduct" {
			if finding.Decision != fingerprintDecisionConfirmed {
				t.Fatalf("strong finding = %#v", finding)
			}
			continue
		}
		candidates++
		if finding.Decision != fingerprintDecisionCandidate || finding.ReasonCode != "weak_fanout" || !finding.RawMatched {
			t.Fatalf("weak finding = %#v", finding)
		}
	}
	if candidates != 4 {
		t.Fatalf("candidate count = %d, want 4; findings=%#v", candidates, asset.FingerprintFindings)
	}
}

func TestFingerprintNoMatchReplacesHistoricalFindingsAfterResponse(t *testing.T) {
	asset := &Asset{
		Host: "rescan.example.test", Port: 443,
		FingerprintFindings: FingerprintFindings{{Name: "Historical", Source: "custom", RawMatched: true}},
	}
	markFingerprintFindingsCollected(asset)
	appResults := make(map[string]*AppDetectionResult)
	applyGovernedFingerprintFindings(asset, appResults, nil, nil)
	if !asset.FingerprintFindingsCollected {
		t.Fatal("response collection marker was not retained")
	}
	if len(asset.FingerprintFindings) != 0 {
		t.Fatalf("historical findings survived an empty current response: %#v", asset.FingerprintFindings)
	}
}

func TestFingerprintMissingResponsePreservesHistoricalFindings(t *testing.T) {
	historical := FingerprintFinding{Name: "Historical", Source: "custom", RawMatched: true}
	asset := &Asset{Host: "unconfirmed.example.test", Port: 443, FingerprintFindings: FingerprintFindings{historical}}
	applyGovernedFingerprintFindings(asset, make(map[string]*AppDetectionResult), FingerprintFindings{{Name: "Current"}}, nil)
	if asset.FingerprintFindingsCollected {
		t.Fatal("missing response was incorrectly marked as collected")
	}
	if len(asset.FingerprintFindings) != 1 || asset.FingerprintFindings[0].Name != historical.Name {
		t.Fatalf("historical findings changed without a response: %#v", asset.FingerprintFindings)
	}
}
