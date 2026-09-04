package scanner

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

const fingerprintGovernancePropertyCases = 300

var (
	fingerprintPropertySources  = []string{"custom", "wappalyzer", "httpx", "active"}
	fingerprintPropertyChannels = []string{"favicon", "header", "cookie", "title", "body", "script", "url", "active_probe"}
	fingerprintPropertyStrength = []string{"strong", "medium", "weak"}
	fingerprintPropertyGroups   = []string{"", "web-server", "platform", "framework"}
)

// fingerprintGovernancePropertyInput maps a shrinkable integer sequence to
// governance inputs. Each bit range controls a separate dimension, so gopter's
// slice/integer shrinking yields small, readable counterexamples.
func fingerprintGovernancePropertyInput(codes []int) FingerprintFindings {
	findings := make(FingerprintFindings, 0, len(codes)+10)
	for index, code := range codes {
		value := uint(code)
		nameIndex := int(value & 7)
		targetIndex := (nameIndex + 1 + int((value>>15)&7)) % 8
		finding := FingerprintFinding{
			FingerprintID: fmt.Sprintf("generated-%d-%d", index, code),
			Name:          fmt.Sprintf("Product-%d", nameIndex),
			Source:        fingerprintPropertySources[(value>>6)&3],
			RawMatched:    ((value >> 18) & 1) == 0,
			ConflictGroup: fingerprintPropertyGroups[(value>>11)&3],
			Evidence: []FingerprintEvidence{{
				Channel:            fingerprintPropertyChannels[(value>>8)&7],
				Strength:           fingerprintPropertyStrength[(value>>3)%3],
				Complete:           ((value >> 5) & 1) == 0,
				MatchedValueDigest: fmt.Sprintf("sha256:%x", value),
			}},
		}
		targetName := fmt.Sprintf("Product-%d", targetIndex)
		switch (value >> 13) & 3 {
		case 1:
			finding.ExclusiveWith = []string{targetName}
		case 2:
			finding.Coexistence = []string{targetName}
		case 3:
			if finding.ConflictGroup != "" {
				finding.Coexistence = []string{finding.ConflictGroup}
			}
		}
		findings = append(findings, finding)
	}

	// Every generated case contains an isolated strong control. It proves that
	// unrelated fan-out/conflicts never delete or downgrade sufficient evidence.
	findings = append(findings, FingerprintFinding{
		FingerprintID: "isolated-strong",
		Name:          "IsolatedStrong",
		Source:        "custom",
		RawMatched:    true,
		Evidence: []FingerprintEvidence{{
			Channel: "favicon", Strength: "strong", Complete: true,
			MatchedValueDigest: "sha256:isolated",
		}},
	})

	// The first code selects a random weak fan-out size from zero through six,
	// crossing the production threshold in both directions across generated cases.
	if len(codes) > 0 {
		fanoutCount := codes[0] % 7
		for i := 0; i < fanoutCount; i++ {
			value := uint(codes[(i+1)%len(codes)])
			findings = append(findings, FingerprintFinding{
				FingerprintID: fmt.Sprintf("fanout-%d", i),
				Name:          fmt.Sprintf("Fanout-%d", i),
				Source:        fingerprintPropertySources[(value+uint(i))%uint(len(fingerprintPropertySources))],
				RawMatched:    true,
				Evidence: []FingerprintEvidence{{
					Channel: "body", Strength: "weak", Complete: true,
					MatchedValueDigest: fmt.Sprintf("sha256:fanout-%d", i),
				}},
			})
		}
	}

	// Two generated controls force conflict-group and coexistence combinations
	// to occur frequently rather than depending on accidental random alignment.
	if len(codes) > 1 {
		left, right := uint(codes[0]), uint(codes[1])
		coexist := ((left >> 4) & 1) == 1
		leftFinding := FingerprintFinding{
			Name: "GeneratedConflictA", Source: fingerprintPropertySources[left&3], RawMatched: true,
			ConflictGroup: "generated-exclusive",
			Evidence:      []FingerprintEvidence{{Channel: "header", Strength: fingerprintPropertyStrength[(left>>2)%3], Complete: ((left >> 6) & 1) == 0}},
		}
		rightFinding := FingerprintFinding{
			Name: "GeneratedConflictB", Source: fingerprintPropertySources[right&3], RawMatched: true,
			ConflictGroup: "generated-exclusive",
			Evidence:      []FingerprintEvidence{{Channel: "title", Strength: fingerprintPropertyStrength[(right>>2)%3], Complete: ((right >> 6) & 1) == 0}},
		}
		leftFinding.ExclusiveWith = []string{rightFinding.Name}
		if coexist {
			leftFinding.Coexistence = []string{rightFinding.Name}
		}
		findings = append(findings, leftFinding, rightFinding)
	}
	return findings
}

func fingerprintPropertyNormalizedName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func fingerprintPropertyRawGroups(findings FingerprintFindings) map[string]FingerprintFindings {
	groups := make(map[string]FingerprintFindings)
	for _, finding := range findings {
		if !finding.RawMatched {
			continue
		}
		key := fingerprintPropertyNormalizedName(finding.Name)
		if key != "" {
			groups[key] = append(groups[key], finding)
		}
	}
	return groups
}

func fingerprintPropertyMeetsThreshold(findings FingerprintFindings) bool {
	strong := make(map[string]struct{})
	medium := make(map[string]struct{})
	weak := make(map[string]struct{})
	for _, finding := range findings {
		source := strings.TrimSpace(finding.Source)
		if source == "" {
			source = "custom"
		}
		for _, evidence := range finding.Evidence {
			if !evidence.Complete {
				continue
			}
			key := source + "\x00" + evidence.Channel
			switch strings.ToLower(evidence.Strength) {
			case "strong":
				strong[key] = struct{}{}
			case "medium":
				medium[key] = struct{}{}
			default:
				weak[key] = struct{}{}
			}
		}
	}
	if len(strong) > 0 || len(medium) >= 2 {
		return true
	}
	for mediumKey := range medium {
		for weakKey := range weak {
			if mediumKey != weakKey {
				return true
			}
		}
	}
	return false
}

func fingerprintPropertyHasCompleteStrong(findings FingerprintFindings) bool {
	for _, finding := range findings {
		for _, evidence := range finding.Evidence {
			if evidence.Complete && strings.EqualFold(evidence.Strength, "strong") {
				return true
			}
		}
	}
	return false
}

func fingerprintPropertyConflict(left, right FingerprintFinding) bool {
	if fingerprintsMayCoexist(left, right) {
		return false
	}
	leftGroup := strings.TrimSpace(left.ConflictGroup)
	rightGroup := strings.TrimSpace(right.ConflictGroup)
	return leftGroup != "" && strings.EqualFold(leftGroup, rightGroup) || fingerprintsExplicitlyConflict(left, right)
}

func checkFingerprintGovernanceProperty(codes []int) error {
	input := fingerprintGovernancePropertyInput(codes)
	before := append(FingerprintFindings(nil), input...)
	governed := GovernFingerprintFindings(input)
	if !reflect.DeepEqual(input, before) {
		return fmt.Errorf("governance mutated raw findings")
	}

	rawGroups := fingerprintPropertyRawGroups(input)
	outputByName := make(map[string]FingerprintFinding, len(governed))
	for _, finding := range governed {
		key := fingerprintPropertyNormalizedName(finding.Name)
		if !finding.RawMatched {
			return fmt.Errorf("output %q changed RawMatched to false", finding.Name)
		}
		if _, exists := rawGroups[key]; !exists {
			return fmt.Errorf("output %q has no raw-matched input", finding.Name)
		}
		if _, duplicate := outputByName[key]; duplicate {
			return fmt.Errorf("raw-matched name %q was not uniquely merged", finding.Name)
		}
		outputByName[key] = finding
		if finding.Decision == fingerprintDecisionConfirmed {
			if finding.Confidence < 75 || !fingerprintPropertyMeetsThreshold(rawGroups[key]) {
				return fmt.Errorf("confirmed %q lacks configured evidence threshold: confidence=%d raw=%#v", finding.Name, finding.Confidence, rawGroups[key])
			}
		}
	}
	for key := range rawGroups {
		if _, exists := outputByName[key]; !exists {
			return fmt.Errorf("raw-matched name %q was deleted", key)
		}
	}

	for i := range governed {
		if governed[i].Decision != fingerprintDecisionConfirmed {
			continue
		}
		for j := i + 1; j < len(governed); j++ {
			if governed[j].Decision == fingerprintDecisionConfirmed && fingerprintPropertyConflict(governed[i], governed[j]) {
				return fmt.Errorf("unresolved exclusive findings both confirmed: %q and %q", governed[i].Name, governed[j].Name)
			}
		}
	}

	for key, raw := range rawGroups {
		if !fingerprintPropertyHasCompleteStrong(raw) {
			continue
		}
		finding := outputByName[key]
		conflicting := false
		for otherKey, other := range outputByName {
			if otherKey != key && fingerprintPropertyConflict(finding, other) {
				conflicting = true
				break
			}
		}
		if !conflicting && finding.Decision != fingerprintDecisionConfirmed {
			return fmt.Errorf("non-conflicting strong finding %q was downgraded: %#v", finding.Name, finding)
		}
	}
	return nil
}

func runFingerprintGovernanceSafetyProperty(t *testing.T, seed int64) {
	t.Helper()
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = fingerprintGovernancePropertyCases
	parameters.MaxSize = 40
	parameters.MaxShrinkCount = 200
	parameters.Rng = rand.New(rand.NewSource(seed))
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 6: confirmed fingerprints meet evidence thresholds, unresolved exclusivity is unique, strong evidence survives, and RawMatched is immutable", prop.ForAll(
		func(codes []int) bool {
			if err := checkFingerprintGovernanceProperty(codes); err != nil {
				t.Logf("Property 6 counterexample codes=%v: %v", codes, err)
				return false
			}
			return true
		},
		gen.SliceOf(gen.IntRange(0, 524287)),
	))
	t.Logf("Property 6 gopter seed=%d cases=%d", seed, fingerprintGovernancePropertyCases)
	properties.TestingRun(t)
}

// TestProperty6_FingerprintGovernanceSafetyFixedSeed is reproducible and uses
// only generated in-memory findings; it never performs network access.
// **Validates: Requirements 2.6, 3.9, 3.10**
func TestProperty6_FingerprintGovernanceSafetyFixedSeed(t *testing.T) {
	runFingerprintGovernanceSafetyProperty(t, 2026041306)
}

// TestProperty6_FingerprintGovernanceSafetyRandomSeed broadens evidence,
// conflict, coexistence, completeness, and fan-out combinations. The logged
// seed makes every run reproducible after gopter reports a shrunk sequence.
// **Validates: Requirements 2.6, 3.9, 3.10**
func TestProperty6_FingerprintGovernanceSafetyRandomSeed(t *testing.T) {
	runFingerprintGovernanceSafetyProperty(t, time.Now().UnixNano())
}
