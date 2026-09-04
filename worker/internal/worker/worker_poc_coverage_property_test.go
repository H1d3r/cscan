package worker

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"cscan/internal/scanner"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

const pocCoveragePropertyCasesPerScenario = 75

type pocCoveragePropertyScenario string

const (
	pocPropertyAllZero      pocCoveragePropertyScenario = "all_zero"
	pocPropertyMixed        pocCoveragePropertyScenario = "mixed"
	pocPropertyFullyCovered pocCoveragePropertyScenario = "fully_covered_zero_finding"
	pocPropertyFindings     pocCoveragePropertyScenario = "findings"
	pocPropertyLoadFailure  pocCoveragePropertyScenario = "template_load_failure"
	pocPropertyExecFailure  pocCoveragePropertyScenario = "execution_failure"
	pocPropertyUntagged     pocCoveragePropertyScenario = "untagged"
	pocPropertyCancellation pocCoveragePropertyScenario = "cancellation"
)

var pocCoveragePropertyScenarios = []pocCoveragePropertyScenario{
	pocPropertyAllZero,
	pocPropertyMixed,
	pocPropertyFullyCovered,
	pocPropertyFindings,
	pocPropertyLoadFailure,
	pocPropertyExecFailure,
	pocPropertyUntagged,
	pocPropertyCancellation,
}

type pocCoveragePropertyGroup struct {
	group           *AssetGroup
	loadResult      TemplateLoadResult
	loadErr         error
	scanStatus      scanner.PhaseStatus
	scannedAssets   int
	vulnerabilities []*scanner.Vulnerability
	scanErr         error
	cancelAfterScan bool
}

func pocCoveragePropertyCode(codes []int, index int) int {
	if len(codes) == 0 {
		return 17 + index*31
	}
	return codes[index%len(codes)]
}

func pocCoveragePropertyAssets(groupIndex, count int) []*scanner.Asset {
	assets := make([]*scanner.Asset, 0, count)
	for assetIndex := 0; assetIndex < count; assetIndex++ {
		assets = append(assets, &scanner.Asset{
			Host:   fmt.Sprintf("property-%d-%d.example.test", groupIndex, assetIndex),
			Port:   8000 + groupIndex*10 + assetIndex,
			IsHTTP: true,
		})
	}
	return assets
}

func newPocCoveragePropertyGroup(groupIndex, assetCount int) pocCoveragePropertyGroup {
	key := fmt.Sprintf("property-group-%d", groupIndex)
	return pocCoveragePropertyGroup{
		group: &AssetGroup{
			GroupKey: key,
			Tags:     []string{key},
			Assets:   pocCoveragePropertyAssets(groupIndex, assetCount),
		},
	}
}

func setPocPropertyLoaded(group *pocCoveragePropertyGroup, templateCount int) {
	group.loadResult = TemplateLoadResult{
		Contents:  []string{fmt.Sprintf("id: %s", group.group.GroupKey)},
		Requested: templateCount,
		Loaded:    templateCount,
		Source:    "property_memory",
		Outcome:   TemplateLoadLoaded,
	}
}

func setPocPropertyZeroTemplates(group *pocCoveragePropertyGroup, code int) {
	group.loadResult.Source = "property_memory"
	group.loadResult.Requested = code % 3
	if code%2 == 0 {
		group.loadResult.Outcome = TemplateLoadNoMatch
		group.loadResult.ReasonCode = "templates_no_match"
	} else {
		group.loadResult.Outcome = TemplateLoadFiltered
		group.loadResult.ReasonCode = "templates_filtered"
	}
}

func setPocPropertyLoadFailure(group *pocCoveragePropertyGroup, code int) {
	group.loadResult.Source = "property_memory"
	if code%2 == 0 {
		group.loadResult.Outcome = TemplateLoadDBError
		group.loadResult.ReasonCode = "property_db_error"
	} else {
		group.loadResult.Outcome = TemplateLoadStoreUnavailable
		group.loadResult.ReasonCode = "property_store_unavailable"
	}
	group.loadErr = errors.New("property template load failure")
}

func setPocPropertyExecution(group *pocCoveragePropertyGroup, status scanner.PhaseStatus, scannedAssets, findingCount int) {
	setPocPropertyLoaded(group, 1+len(group.group.Assets)%3)
	group.scanStatus = status
	group.scannedAssets = scannedAssets
	for findingIndex := 0; findingIndex < findingCount; findingIndex++ {
		group.vulnerabilities = append(group.vulnerabilities, &scanner.Vulnerability{
			Host:    group.group.Assets[findingIndex%len(group.group.Assets)].Host,
			Port:    group.group.Assets[findingIndex%len(group.group.Assets)].Port,
			PocFile: fmt.Sprintf("property-finding-%d-%d", len(group.group.Assets), findingIndex),
			VulName: fmt.Sprintf("Property finding %d", findingIndex),
		})
	}
}

func buildPocCoveragePropertyGroups(scenario pocCoveragePropertyScenario, codes []int) []pocCoveragePropertyGroup {
	groupCount := 1 + pocCoveragePropertyCode(codes, 0)%4
	groups := make([]pocCoveragePropertyGroup, 0, groupCount+1)

	switch scenario {
	case pocPropertyAllZero:
		for index := 0; index < groupCount; index++ {
			group := newPocCoveragePropertyGroup(index, 1+pocCoveragePropertyCode(codes, index+1)%3)
			setPocPropertyZeroTemplates(&group, pocCoveragePropertyCode(codes, index+2))
			groups = append(groups, group)
		}
	case pocPropertyMixed:
		coveredAssets := 2 + pocCoveragePropertyCode(codes, 1)%3
		covered := newPocCoveragePropertyGroup(0, coveredAssets)
		if pocCoveragePropertyCode(codes, 2)%2 == 0 {
			setPocPropertyExecution(&covered, scanner.PhaseComplete, coveredAssets, 1+pocCoveragePropertyCode(codes, 3)%3)
		} else {
			setPocPropertyExecution(&covered, scanner.PhasePartial, 1+pocCoveragePropertyCode(codes, 3)%(coveredAssets-1), 1+pocCoveragePropertyCode(codes, 4)%3)
		}
		groups = append(groups, covered)
		for index := 0; index < groupCount; index++ {
			group := newPocCoveragePropertyGroup(index+1, 1+pocCoveragePropertyCode(codes, index+5)%3)
			switch pocCoveragePropertyCode(codes, index+6) % 4 {
			case 0:
				setPocPropertyZeroTemplates(&group, pocCoveragePropertyCode(codes, index+7))
			case 1:
				setPocPropertyLoadFailure(&group, pocCoveragePropertyCode(codes, index+7))
			case 2:
				setPocPropertyLoaded(&group, 1)
				group.scanErr = errors.New("property nuclei execution failure")
			default:
				group.group.Tags = nil
				group.group.Untagged = true
			}
			groups = append(groups, group)
		}
	case pocPropertyFullyCovered:
		for index := 0; index < groupCount; index++ {
			assetCount := 1 + pocCoveragePropertyCode(codes, index+1)%3
			group := newPocCoveragePropertyGroup(index, assetCount)
			setPocPropertyExecution(&group, scanner.PhaseComplete, assetCount, 0)
			groups = append(groups, group)
		}
	case pocPropertyFindings:
		for index := 0; index < groupCount; index++ {
			assetCount := 1 + pocCoveragePropertyCode(codes, index+1)%3
			group := newPocCoveragePropertyGroup(index, assetCount)
			setPocPropertyExecution(&group, scanner.PhaseComplete, assetCount, 1+pocCoveragePropertyCode(codes, index+2)%3)
			groups = append(groups, group)
		}
	case pocPropertyLoadFailure:
		for index := 0; index < groupCount; index++ {
			group := newPocCoveragePropertyGroup(index, 1+pocCoveragePropertyCode(codes, index+1)%3)
			setPocPropertyLoadFailure(&group, pocCoveragePropertyCode(codes, index+2))
			groups = append(groups, group)
		}
	case pocPropertyExecFailure:
		for index := 0; index < groupCount; index++ {
			group := newPocCoveragePropertyGroup(index, 1+pocCoveragePropertyCode(codes, index+1)%3)
			setPocPropertyLoaded(&group, 1+pocCoveragePropertyCode(codes, index+2)%3)
			group.scanErr = errors.New("property nuclei execution failure")
			groups = append(groups, group)
		}
	case pocPropertyUntagged:
		for index := 0; index < groupCount; index++ {
			group := newPocCoveragePropertyGroup(index, 1+pocCoveragePropertyCode(codes, index+1)%3)
			group.group.Tags = nil
			group.group.Untagged = true
			groups = append(groups, group)
		}
	case pocPropertyCancellation:
		// At least two groups are required so cancellation after a covered group
		// has a remaining applicable group over which to take precedence.
		for index := 0; index < 2+groupCount; index++ {
			assetCount := 1 + pocCoveragePropertyCode(codes, index+1)%3
			group := newPocCoveragePropertyGroup(index, assetCount)
			findingCount := 0
			if index == 0 && pocCoveragePropertyCode(codes, 2)%3 != 0 {
				findingCount = 1 + pocCoveragePropertyCode(codes, 3)%2
			}
			setPocPropertyExecution(&group, scanner.PhaseComplete, assetCount, findingCount)
			group.cancelAfterScan = index == 0
			groups = append(groups, group)
		}
	}
	return groups
}

func checkPocCoverageConclusionProperty(scenario pocCoveragePropertyScenario, codes []int) error {
	propertyGroups := buildPocCoveragePropertyGroups(scenario, codes)
	groups := make([]*AssetGroup, 0, len(propertyGroups))
	byKey := make(map[string]*pocCoveragePropertyGroup, len(propertyGroups))
	totalAssets := 0
	for index := range propertyGroups {
		propertyGroup := &propertyGroups[index]
		groups = append(groups, propertyGroup.group)
		byKey[propertyGroup.group.GroupKey] = propertyGroup
		totalAssets += len(propertyGroup.group.Assets)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	preCanceled := scenario == pocPropertyCancellation && pocCoveragePropertyCode(codes, 0)%2 == 0
	if preCanceled {
		cancel()
	}

	loader := func(_ context.Context, tags, _ []string) (TemplateLoadResult, error) {
		if len(tags) != 1 {
			return TemplateLoadResult{}, errors.New("property loader received invalid tags")
		}
		group := byKey[tags[0]]
		if group == nil {
			return TemplateLoadResult{}, errors.New("property loader received unknown group")
		}
		return group.loadResult, group.loadErr
	}

	emitted := make([]*scanner.Vulnerability, 0)
	fake := &fakePocNucleiScanner{scan: func(_ context.Context, config *scanner.ScanConfig) (*scanner.ScanResult, error) {
		opts, ok := config.Options.(*scanner.NucleiOptions)
		if !ok || len(opts.Tags) != 1 {
			return nil, errors.New("property scanner received invalid options")
		}
		group := byKey[opts.Tags[0]]
		if group == nil {
			return nil, errors.New("property scanner received unknown group")
		}
		if group.scanErr != nil {
			return nil, group.scanErr
		}
		result := &scanner.ScanResult{
			Vulnerabilities: append([]*scanner.Vulnerability(nil), group.vulnerabilities...),
			Diagnostic: &scanner.ScanDiagnostic{
				Phase:  "poc",
				Status: group.scanStatus,
				Coverage: scanner.Coverage{
					Input:     len(group.group.Assets),
					Attempted: len(group.group.Assets),
					Succeeded: group.scannedAssets,
				},
			},
		}
		emitted = append(emitted, result.Vulnerabilities...)
		if group.cancelAfterScan {
			cancel()
		}
		return result, nil
	}}

	summary := executePocGroups(ctx, groups, nil, &scanner.NucleiOptions{}, loader, fake, nil)
	if totalAssets <= 0 || summary.TotalAssets != totalAssets {
		return fmt.Errorf("scenario=%s total assets=%d, want %d", scenario, summary.TotalAssets, totalAssets)
	}
	if summary.ScannedAssets == 0 {
		if summary.Status == scanner.PhaseComplete {
			return fmt.Errorf("scenario=%s zero scanned assets yielded COMPLETE", scenario)
		}
		if summary.VulnerabilityConclusion == PocConclusionNoFindings {
			return fmt.Errorf("scenario=%s zero scanned assets yielded NO_FINDINGS", scenario)
		}
	}
	if summary.Vulnerabilities != len(emitted) || len(summary.VulnerabilityResults) != len(emitted) {
		return fmt.Errorf("scenario=%s vulnerability counts=%d/%d, emitted=%d", scenario, summary.Vulnerabilities, len(summary.VulnerabilityResults), len(emitted))
	}
	for index := range emitted {
		if summary.VulnerabilityResults[index] != emitted[index] {
			return fmt.Errorf("scenario=%s vulnerability %d was not preserved", scenario, index)
		}
	}

	switch scenario {
	case pocPropertyAllZero, pocPropertyUntagged:
		if summary.Status != scanner.PhaseUncovered || summary.VulnerabilityConclusion != PocConclusionNotEvaluated {
			return fmt.Errorf("scenario=%s conclusion=%s/%s, want UNCOVERED/NOT_EVALUATED", scenario, summary.Status, summary.VulnerabilityConclusion)
		}
	case pocPropertyMixed:
		if summary.ScannedAssets <= 0 || summary.ScannedAssets >= summary.TotalAssets {
			return fmt.Errorf("mixed coverage scanned=%d total=%d", summary.ScannedAssets, summary.TotalAssets)
		}
		if summary.Status != scanner.PhasePartial {
			return fmt.Errorf("mixed coverage status=%s, want PARTIAL", summary.Status)
		}
		if len(emitted) == 0 || summary.VulnerabilityConclusion != PocConclusionFindings {
			return fmt.Errorf("mixed findings=%d conclusion=%s, want preserved FINDINGS", len(emitted), summary.VulnerabilityConclusion)
		}
	case pocPropertyFullyCovered:
		if summary.ScannedAssets != summary.TotalAssets || summary.Status != scanner.PhaseComplete || summary.VulnerabilityConclusion != PocConclusionNoFindings {
			return fmt.Errorf("full zero-finding conclusion=%s/%s coverage=%d/%d", summary.Status, summary.VulnerabilityConclusion, summary.ScannedAssets, summary.TotalAssets)
		}
	case pocPropertyFindings:
		if summary.ScannedAssets != summary.TotalAssets || summary.Status != scanner.PhaseComplete || summary.VulnerabilityConclusion != PocConclusionFindings || len(emitted) == 0 {
			return fmt.Errorf("findings conclusion=%s/%s coverage=%d/%d findings=%d", summary.Status, summary.VulnerabilityConclusion, summary.ScannedAssets, summary.TotalAssets, len(emitted))
		}
	case pocPropertyLoadFailure, pocPropertyExecFailure:
		if summary.Status != scanner.PhaseFailed || summary.VulnerabilityConclusion != PocConclusionNotEvaluated {
			return fmt.Errorf("scenario=%s conclusion=%s/%s, want FAILED/NOT_EVALUATED", scenario, summary.Status, summary.VulnerabilityConclusion)
		}
	case pocPropertyCancellation:
		if summary.Status != scanner.PhaseCanceled {
			return fmt.Errorf("cancellation status=%s, want CANCELED", summary.Status)
		}
		if preCanceled && summary.ScannedAssets != 0 {
			return fmt.Errorf("pre-canceled coverage scanned=%d, want 0", summary.ScannedAssets)
		}
		if !preCanceled && (summary.ScannedAssets == 0 || summary.ScannedAssets >= summary.TotalAssets) {
			return fmt.Errorf("mid-run cancellation coverage=%d/%d, want mixed coverage", summary.ScannedAssets, summary.TotalAssets)
		}
	}
	return nil
}

func runPocCoverageConclusionProperty(t *testing.T, seed int64) {
	t.Helper()
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = pocCoveragePropertyCasesPerScenario
	parameters.MaxSize = 24
	parameters.MaxShrinkCount = 150
	parameters.Rng = rand.New(rand.NewSource(seed))
	properties := gopter.NewProperties(parameters)
	for _, propertyScenario := range pocCoveragePropertyScenarios {
		scenario := propertyScenario
		properties.Property(fmt.Sprintf("Property 7: POC coverage conclusion (%s)", scenario), prop.ForAll(
			func(codes []int) bool {
				if err := checkPocCoverageConclusionProperty(scenario, codes); err != nil {
					t.Logf("Property 7 counterexample scenario=%s codes=%v: %v", scenario, codes, err)
					return false
				}
				return true
			},
			gen.SliceOf(gen.IntRange(0, 4095)),
		))
	}
	t.Logf("Property 7 gopter seed=%d scenarios=%d cases_per_scenario=%d total_cases=%d", seed, len(pocCoveragePropertyScenarios), pocCoveragePropertyCasesPerScenario, len(pocCoveragePropertyScenarios)*pocCoveragePropertyCasesPerScenario)
	properties.TestingRun(t)
}

// TestProperty7_PocCoverageConclusionFixedSeed exercises all-zero, mixed,
// fully covered zero-finding, findings, load failure, execution failure,
// untagged, and cancellation cases using only local in-memory collaborators.
// **Validates: Requirements 2.8, 2.9, 3.12, 3.13**
func TestProperty7_PocCoverageConclusionFixedSeed(t *testing.T) {
	runPocCoverageConclusionProperty(t, 2026041607)
}

// TestProperty7_PocCoverageConclusionRandomSeed logs the seed and exact case
// count so any generated counterexample can be reproduced without networking
// or a real Nuclei subprocess.
// **Validates: Requirements 2.8, 2.9, 3.12, 3.13**
func TestProperty7_PocCoverageConclusionRandomSeed(t *testing.T) {
	runPocCoverageConclusionProperty(t, time.Now().UnixNano())
}
