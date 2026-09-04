package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"cscan/internal/model"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

// TestFingerprintCandidatesDoNotExpandAutomaticPocTags covers the Worker
// integration boundary. Even if a legacy/mixed App value still contains a
// custom candidate, only the confirmed fingerprint may produce automatic POC
// tags. **Validates: Requirements 2.6, 3.9, 3.10**
func TestFingerprintCandidatesDoNotExpandAutomaticPocTags(t *testing.T) {
	asset := &scanner.Asset{
		App: []string{"StrongProduct[custom(strong-id)]", "WeakProduct[custom(weak-id)]"},
		FingerprintFindings: scanner.FingerprintFindings{
			{Name: "StrongProduct", Source: "custom", Decision: "CONFIRMED", RawMatched: true},
			{Name: "WeakProduct", Source: "custom", Decision: "CANDIDATE", RawMatched: true, ReasonCode: "weak_fanout"},
		},
	}
	config := &scheduler.PocScanConfig{
		AutoScan: true,
		TagMappings: map[string][]string{
			"StrongProduct": {"strong-poc"},
			"WeakProduct":   {"weak-poc"},
		},
	}

	got := (&Worker{}).generateAssetTags(asset, config)
	if !reflect.DeepEqual(got, []string{"strong-poc"}) {
		t.Fatalf("automatic tags = %#v, want confirmed-only tag", got)
	}
}

// TestFingerprintCandidatePersistenceRoundTrip verifies optional candidate
// detail/source survives both queued and direct Mongo writer DTO mappings, and
// remains omitted for legacy assets. **Validates: Requirements 2.6, 3.15**
func TestFingerprintCandidatePersistenceRoundTrip(t *testing.T) {
	candidate := model.FingerprintFinding{
		FingerprintID: "weak-id",
		Name:          "WeakProduct",
		Source:        "custom",
		RawMatched:    true,
		Decision:      "CANDIDATE",
		Confidence:    25,
		ReasonCode:    "weak_fanout",
		Evidence: []model.FingerprintEvidence{{
			Channel: "body", Strength: "weak", Complete: true, MatchedValueDigest: "sha256:test",
		}},
	}
	asset := &scanner.Asset{Host: "fixture.example.test", Port: 80, App: []string{"StrongProduct[custom]"}, FingerprintFindings: scanner.FingerprintFindings{candidate}}

	documents := scannerAssetsToDocuments([]*scanner.Asset{asset})
	if len(documents) != 1 || !reflect.DeepEqual(model.FingerprintFindings(documents[0].FingerprintFindings), model.FingerprintFindings{candidate}) {
		t.Fatalf("queued document findings = %#v", documents)
	}
	replayed := assetDocumentToScannerAsset(&documents[0])
	if !reflect.DeepEqual(replayed.FingerprintFindings, model.FingerprintFindings{candidate}) {
		t.Fatalf("replayed findings = %#v", replayed.FingerprintFindings)
	}
	direct := scannerAssetToDTO(asset)
	if !reflect.DeepEqual(direct.FingerprintFindings, model.FingerprintFindings{candidate}) {
		t.Fatalf("direct DTO findings = %#v", direct.FingerprintFindings)
	}

	legacyJSON, err := json.Marshal(AssetDocument{Host: "legacy.example.test", Port: 80})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacyJSON), "fingerprintFindings") {
		t.Fatalf("legacy JSON did not omit optional findings: %s", legacyJSON)
	}
}
