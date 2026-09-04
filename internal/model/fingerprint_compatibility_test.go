package model

import (
	"encoding/json"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// TestFingerprintEvidenceModelBackwardCompatibility verifies historical rules
// and finding containers decode without migration while new optional metadata
// round-trips through both public encodings.
// **Validates: Requirements 3.15**
func TestFingerprintEvidenceModelBackwardCompatibility(t *testing.T) {
	legacyJSON := []byte(`{"name":"Legacy","enabled":true,"rule":"body=\"legacy\""}`)
	var legacyFromJSON Fingerprint
	if err := json.Unmarshal(legacyJSON, &legacyFromJSON); err != nil {
		t.Fatal(err)
	}
	if legacyFromJSON.ConflictGroup != "" || legacyFromJSON.Coexistence != nil || legacyFromJSON.ExclusiveWith != nil {
		t.Fatalf("legacy JSON optional metadata = %#v", legacyFromJSON)
	}

	legacyBSON, err := bson.Marshal(bson.M{"name": "Legacy", "enabled": true, "rule": `body="legacy"`})
	if err != nil {
		t.Fatal(err)
	}
	var legacyFromBSON Fingerprint
	if err := bson.Unmarshal(legacyBSON, &legacyFromBSON); err != nil {
		t.Fatal(err)
	}
	if legacyFromBSON.ConflictGroup != "" || legacyFromBSON.Coexistence != nil || legacyFromBSON.ExclusiveWith != nil {
		t.Fatalf("legacy BSON optional metadata = %#v", legacyFromBSON)
	}

	want := FingerprintFinding{
		FingerprintID: "rule-id",
		Name:          "Example",
		Source:        "custom",
		RawMatched:    true,
		Evidence: []FingerprintEvidence{{
			Channel:            "header",
			Pattern:            "header condition",
			MatchedValueDigest: "sha256:0123456789abcdef",
			Strength:           "medium",
			Complete:           true,
		}},
		ConflictGroup: "web-server",
		Coexistence:   []string{"frontend"},
		ExclusiveWith: []string{"OtherServer"},
	}
	findings := FingerprintFindings{want}

	jsonBytes, err := json.Marshal(findings)
	if err != nil {
		t.Fatal(err)
	}
	var jsonRoundTrip FingerprintFindings
	if err := json.Unmarshal(jsonBytes, &jsonRoundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jsonRoundTrip, findings) {
		t.Fatalf("JSON round trip = %#v, want %#v", jsonRoundTrip, findings)
	}

	bsonBytes, err := bson.Marshal(bson.M{"fingerprint_findings": findings})
	if err != nil {
		t.Fatal(err)
	}
	var bsonRoundTrip struct {
		Findings FingerprintFindings `bson:"fingerprint_findings,omitempty"`
	}
	if err := bson.Unmarshal(bsonBytes, &bsonRoundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bsonRoundTrip.Findings, findings) {
		t.Fatalf("BSON round trip = %#v, want %#v", bsonRoundTrip.Findings, findings)
	}

	var missing struct {
		Findings FingerprintFindings `json:"fingerprintFindings,omitempty" bson:"fingerprint_findings,omitempty"`
	}
	if err := json.Unmarshal([]byte(`{}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Findings != nil {
		t.Fatalf("missing historical findings = %#v, want nil", missing.Findings)
	}
}
