package scanner

import (
	"math/rand"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

type schemeResolutionDecision struct {
	scheme          string
	alternateScheme string
	selectedKind    string
	hasEvidence     bool
	conflict        bool
}

func schemeDecision(resolution SchemeResolution) schemeResolutionDecision {
	return schemeResolutionDecision{
		scheme:          resolution.Scheme,
		alternateScheme: resolution.AlternateScheme,
		selectedKind:    resolution.SelectedEvidence.Kind,
		hasEvidence:     resolution.HasEvidence,
		conflict:        resolution.Conflict,
	}
}

func propertyOtherScheme(scheme string) string {
	if scheme == SchemeHTTP {
		return SchemeHTTPS
	}
	return SchemeHTTP
}

// propertySchemeNoise turns generated codes into lower-ranked, failed, and
// invalid observations. No item is a usable response or TLS success, so a
// verified response/TLS winner can be tested independently of list size.
func propertySchemeNoise(codes []int, selected string, includeExplicit bool) []SchemeEvidence {
	other := propertyOtherScheme(selected)
	noise := make([]SchemeEvidence, 0, len(codes))
	for _, code := range codes {
		switch code % 12 {
		case 0:
			noise = append(noise, SchemeEvidence{Scheme: other, Kind: SchemeEvidencePortHint})
		case 1:
			noise = append(noise, SchemeEvidence{Scheme: selected, Kind: SchemeEvidencePortHint})
		case 2:
			noise = append(noise, SchemeEvidence{Scheme: other, Kind: SchemeEvidenceScannerService})
		case 3:
			noise = append(noise, SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceScannerService})
		case 4:
			if includeExplicit {
				noise = append(noise, SchemeEvidence{Scheme: other, Kind: SchemeEvidenceExplicitInput})
			} else {
				noise = append(noise, SchemeEvidence{Scheme: "ftp", Kind: SchemeEvidenceExplicitInput})
			}
		case 5:
			noise = append(noise, SchemeEvidence{Scheme: other, Kind: SchemeEvidenceSuccessfulResponse, Success: false, ErrorClass: ReasonTimeout})
		case 6:
			noise = append(noise, SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceTLSHandshake, Success: false, ErrorClass: ReasonTimeout})
		case 7:
			noise = append(noise, SchemeEvidence{Scheme: "ftp", Kind: SchemeEvidenceExplicitInput})
		case 8:
			noise = append(noise, SchemeEvidence{Scheme: other, Kind: "unknown", Success: true})
		case 9:
			noise = append(noise, SchemeEvidence{Scheme: "  " + selected + "  ", Kind: SchemeEvidencePortHint})
		case 10:
			noise = append(noise, SchemeEvidence{Scheme: other, Kind: SchemeEvidenceTLSHandshake, Success: false, ErrorClass: ReasonTimeout})
		case 11:
			noise = append(noise, SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceSuccessfulResponse, Success: false, StatusCode: 200, ErrorClass: "failed"})
		}
	}
	return noise
}

func cloneSchemeEvidence(items []SchemeEvidence) []SchemeEvidence {
	return append([]SchemeEvidence(nil), items...)
}

// propertySchemePermutations always includes identity and reverse order, then
// derives additional rotations/swaps from generated permutation codes.
func propertySchemePermutations(items []SchemeEvidence, codes []int) [][]SchemeEvidence {
	identity := cloneSchemeEvidence(items)
	reversed := cloneSchemeEvidence(items)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}

	rotated := cloneSchemeEvidence(items)
	if len(rotated) > 1 {
		offset := len(rotated) / 2
		if len(codes) > 0 {
			offset = codes[0] % len(rotated)
		}
		rotated = append(cloneSchemeEvidence(rotated[offset:]), rotated[:offset]...)
	}

	permuted := cloneSchemeEvidence(items)
	for index, code := range codes {
		if len(permuted) < 2 {
			break
		}
		left := index % len(permuted)
		right := code % len(permuted)
		permuted[left], permuted[right] = permuted[right], permuted[left]
	}
	return [][]SchemeEvidence{identity, reversed, rotated, permuted}
}

func propertyAllPermutationsDecide(items []SchemeEvidence, permutationCodes []int, expected schemeResolutionDecision) bool {
	for _, permutation := range propertySchemePermutations(items, permutationCodes) {
		if schemeDecision(ResolveScheme(permutation)) != expected {
			return false
		}
	}
	return true
}

func propertyFirstSuccessfulResponse(items []SchemeEvidence) string {
	for _, item := range items {
		if normalizeScheme(item.Scheme) != "" && item.Kind == SchemeEvidenceSuccessfulResponse && item.Success {
			return normalizeScheme(item.Scheme)
		}
	}
	return ""
}

func checkSchemeEvidenceMonotonicity(codes, permutationCodes []int, selectedIndex int) bool {
	selected := []string{SchemeHTTP, SchemeHTTPS}[selectedIndex]
	other := propertyOtherScheme(selected)
	noise := propertySchemeNoise(codes, selected, true)

	// A successful response is invariant under permutations when it is the
	// unique best response. A successful opposite TLS observation plus arbitrary
	// lower-ranked evidence must not replace it.
	responseEvidence := append(cloneSchemeEvidence(noise),
		SchemeEvidence{Scheme: other, Kind: SchemeEvidenceTLSHandshake, Success: true},
		SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
	)
	responseExpected := schemeResolutionDecision{
		scheme: selected, alternateScheme: other,
		selectedKind: SchemeEvidenceSuccessfulResponse, hasEvidence: true, conflict: true,
	}
	if !propertyAllPermutationsDecide(responseEvidence, permutationCodes, responseExpected) {
		return false
	}

	// TLS is likewise stable against explicit/service/port hints and failed
	// response/TLS observations.
	tlsEvidence := append(cloneSchemeEvidence(noise),
		SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceTLSHandshake, Success: true},
		SchemeEvidence{Scheme: other, Kind: SchemeEvidencePortHint},
	)
	tlsExpected := schemeResolutionDecision{
		scheme: selected, alternateScheme: other,
		selectedKind: SchemeEvidenceTLSHandshake, hasEvidence: true, conflict: true,
	}
	if !propertyAllPermutationsDecide(tlsEvidence, permutationCodes, tlsExpected) {
		return false
	}

	// Adding any number of lower-ranked observations is monotonic for the
	// selected verified response, while opposite usable hints are still exposed
	// as conflict/alternate evidence.
	verifiedOnly := []SchemeEvidence{{Scheme: selected, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 204}}
	withLowRank := append(cloneSchemeEvidence(verifiedOnly), noise...)
	withLowRank = append(withLowRank, SchemeEvidence{Scheme: other, Kind: SchemeEvidencePortHint})
	if schemeDecision(ResolveScheme(verifiedOnly)).scheme != selected ||
		schemeDecision(ResolveScheme(withLowRank)) != responseExpected {
		return false
	}

	// Invalid schemes/kinds and failed active evidence are non-evidence: alone
	// they remain unresolved, and when added they cannot alter a clean decision.
	invalidAndFailed := []SchemeEvidence{
		{Scheme: "ftp", Kind: SchemeEvidenceExplicitInput},
		{Scheme: selected, Kind: "unknown", Success: true},
		{Scheme: other, Kind: SchemeEvidenceSuccessfulResponse, Success: false, StatusCode: 200, ErrorClass: ReasonTimeout},
		{Scheme: other, Kind: SchemeEvidenceTLSHandshake, Success: false, ErrorClass: ReasonTimeout},
		{Scheme: "", Kind: SchemeEvidencePortHint},
	}
	if ResolveScheme(invalidAndFailed).HasEvidence {
		return false
	}
	cleanDecision := schemeDecision(ResolveScheme(verifiedOnly))
	if schemeDecision(ResolveScheme(append(cloneSchemeEvidence(verifiedOnly), invalidAndFailed...))) != cleanDecision {
		return false
	}

	// Equal-quality dual successes have two explicit tie modes. A unique
	// explicit scheme makes the decision order-independent.
	dualWithExplicit := append(propertySchemeNoise(codes, selected, false),
		SchemeEvidence{Scheme: other, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
		SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
		SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceExplicitInput},
	)
	if !propertyAllPermutationsDecide(dualWithExplicit, permutationCodes, responseExpected) {
		return false
	}

	// Without explicit input, unequal response quality is order-independent.
	dualWithQualityWinner := append(propertySchemeNoise(codes, selected, false),
		SchemeEvidence{Scheme: other, Kind: SchemeEvidenceSuccessfulResponse, Success: true, ErrorClass: "incomplete"},
		SchemeEvidence{Scheme: selected, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 404},
	)
	if !propertyAllPermutationsDecide(dualWithQualityWinner, permutationCodes, responseExpected) {
		return false
	}

	// An exact dual-success tie intentionally preserves first-observed semantics;
	// every permutation must select its first equally high-quality success and
	// record the other successful protocol as the alternate.
	exactTie := append(propertySchemeNoise(codes, selected, false),
		SchemeEvidence{Scheme: SchemeHTTP, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
		SchemeEvidence{Scheme: SchemeHTTPS, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: 200},
	)
	for _, permutation := range propertySchemePermutations(exactTie, permutationCodes) {
		first := propertyFirstSuccessfulResponse(permutation)
		got := schemeDecision(ResolveScheme(permutation))
		want := schemeResolutionDecision{
			scheme: first, alternateScheme: propertyOtherScheme(first),
			selectedKind: SchemeEvidenceSuccessfulResponse, hasEvidence: true, conflict: true,
		}
		if first == "" || got != want {
			return false
		}
	}

	return true
}

func runSchemeEvidenceMonotonicityProperty(t *testing.T, seed int64) {
	t.Helper()
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 300
	parameters.MaxSize = 32
	parameters.MaxShrinkCount = 100
	parameters.Rng = rand.New(rand.NewSource(seed))
	properties := gopter.NewProperties(parameters)

	properties.Property("Property 5: Scheme evidence decisions are stable under equivalent permutations and monotonic under lower-ranked evidence", prop.ForAll(
		checkSchemeEvidenceMonotonicity,
		gen.SliceOf(gen.IntRange(0, 511)),
		gen.SliceOf(gen.IntRange(0, 511)),
		gen.IntRange(0, 1),
	))
	t.Logf("gopter seed=%d", seed)
	properties.TestingRun(t)
}

// TestProperty5_SchemeEvidenceMonotonicityFixedSeed provides a reproducible
// evidence set/permutation counterexample for priority, conflict, alternate,
// invalid/failed evidence, and dual-success tie semantics.
// **Validates: Requirements 2.4, 2.7, 3.7**
func TestProperty5_SchemeEvidenceMonotonicityFixedSeed(t *testing.T) {
	runSchemeEvidenceMonotonicityProperty(t, 2026041010)
}

// TestProperty5_SchemeEvidenceMonotonicityRandomSeed broadens the generated
// evidence/permutation space offline and logs its replayable seed.
// **Validates: Requirements 2.4, 2.7, 3.7**
func TestProperty5_SchemeEvidenceMonotonicityRandomSeed(t *testing.T) {
	runSchemeEvidenceMonotonicityProperty(t, time.Now().UnixNano())
}
