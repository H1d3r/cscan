package worker

import (
	"context"
	"errors"
	"testing"

	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

type fakePocNucleiScanner struct {
	calls int
	scan  func(context.Context, *scanner.ScanConfig) (*scanner.ScanResult, error)
}

func (f *fakePocNucleiScanner) Name() string { return "nuclei" }

func (f *fakePocNucleiScanner) Scan(ctx context.Context, config *scanner.ScanConfig) (*scanner.ScanResult, error) {
	f.calls++
	return f.scan(ctx, config)
}

func pocTestGroups(assets []*scanner.Asset, mappings map[string][]string) []*AssetGroup {
	w := &Worker{}
	return w.groupAssetsByTags(assets, &scheduler.PocScanConfig{AutoScan: true, TagMappings: mappings})
}

func loadedTemplateResult() TemplateLoadResult {
	return TemplateLoadResult{
		Contents:  []string{validTemplate("covered-template")},
		Requested: 1,
		Loaded:    1,
		Source:    "local_store",
		Outcome:   TemplateLoadLoaded,
	}
}

func completedPocResult(assetCount int, vuls ...*scanner.Vulnerability) *scanner.ScanResult {
	return &scanner.ScanResult{
		Vulnerabilities: vuls,
		Diagnostic: &scanner.ScanDiagnostic{
			Phase:    "poc",
			Status:   scanner.PhaseComplete,
			Coverage: scanner.Coverage{Input: assetCount, Attempted: assetCount, Succeeded: assetCount},
		},
	}
}

// Templates that execute successfully with zero findings are a legitimate
// COMPLETE/NO_FINDINGS result with explicit template and asset coverage.
// **Validates: Requirements 3.12**
func TestPocCoverageTemplatesExecutedWithZeroVulnerabilities(t *testing.T) {
	assets := []*scanner.Asset{{Host: "covered.test", Port: 443, App: []string{"Covered[custom]"}, IsHTTP: true}}
	groups := pocTestGroups(assets, map[string][]string{"Covered": {"covered"}})
	fake := &fakePocNucleiScanner{scan: func(_ context.Context, config *scanner.ScanConfig) (*scanner.ScanResult, error) {
		return completedPocResult(len(config.Assets)), nil
	}}

	summary := executePocGroups(t.Context(), groups, nil, &scanner.NucleiOptions{},
		func(context.Context, []string, []string) (TemplateLoadResult, error) {
			return loadedTemplateResult(), nil
		},
		fake, nil)

	if summary.Status != scanner.PhaseComplete || summary.VulnerabilityConclusion != PocConclusionNoFindings {
		t.Fatalf("covered zero-finding conclusion = %s/%s, want COMPLETE/NO_FINDINGS", summary.Status, summary.VulnerabilityConclusion)
	}
	if summary.ScannedGroups != 1 || summary.ScannedAssets != 1 || summary.ValidTemplates != 1 || summary.ExecutedTemplates != 1 || summary.Vulnerabilities != 0 {
		t.Fatalf("covered zero-finding counts = %+v", summary)
	}
}

// A covered group and a true no-match group produce PARTIAL coverage without
// erasing the completed group's execution evidence.
// **Validates: Requirements 2.8, 3.13**
func TestPocCoverageMixedCoveredAndUncoveredGroups(t *testing.T) {
	assets := []*scanner.Asset{
		{Host: "covered.test", Port: 443, App: []string{"Covered[custom]"}, IsHTTP: true},
		{Host: "missing.test", Port: 8443, App: []string{"Missing[custom]"}, IsHTTP: true},
	}
	groups := pocTestGroups(assets, map[string][]string{"Covered": {"covered"}, "Missing": {"missing"}})
	fake := &fakePocNucleiScanner{scan: func(_ context.Context, config *scanner.ScanConfig) (*scanner.ScanResult, error) {
		return completedPocResult(len(config.Assets)), nil
	}}
	loader := func(_ context.Context, tags, _ []string) (TemplateLoadResult, error) {
		if len(tags) == 1 && tags[0] == "covered" {
			return loadedTemplateResult(), nil
		}
		return TemplateLoadResult{Requested: 0, Source: "local_store", Outcome: TemplateLoadNoMatch, ReasonCode: "templates_no_match"}, nil
	}

	summary := executePocGroups(t.Context(), groups, nil, &scanner.NucleiOptions{}, loader, fake, nil)
	if summary.Status != scanner.PhasePartial || summary.VulnerabilityConclusion != PocConclusionPartiallyEvaluated {
		t.Fatalf("mixed conclusion = %s/%s, want PARTIAL/PARTIALLY_EVALUATED", summary.Status, summary.VulnerabilityConclusion)
	}
	if summary.TotalGroups != 2 || summary.ScannedGroups != 1 || summary.UncoveredGroups != 1 || summary.ScannedAssets != 1 || summary.UncoveredAssets != 1 || fake.calls != 1 {
		t.Fatalf("mixed coverage counts = %+v, scanner calls=%d", summary, fake.calls)
	}
}

// All applicable groups with zero templates remain UNCOVERED and must not
// claim that zero vulnerabilities means NO_FINDINGS.
// **Validates: Requirements 2.8, 2.9**
func TestPocCoverageAllZeroTemplatesIsNotEvaluated(t *testing.T) {
	assets := []*scanner.Asset{
		{Host: "one.test", Port: 80, App: []string{"One[custom]"}, IsHTTP: true},
		{Host: "two.test", Port: 443, App: []string{"Two[custom]"}, IsHTTP: true},
	}
	groups := pocTestGroups(assets, map[string][]string{"One": {"one"}, "Two": {"two"}})
	fake := &fakePocNucleiScanner{scan: func(context.Context, *scanner.ScanConfig) (*scanner.ScanResult, error) {
		t.Fatal("zero-template groups must not invoke Nuclei")
		return nil, nil
	}}
	loader := func(context.Context, []string, []string) (TemplateLoadResult, error) {
		return TemplateLoadResult{Source: "local_store", Outcome: TemplateLoadFiltered, ReasonCode: "templates_filtered"}, nil
	}

	summary := executePocGroups(t.Context(), groups, []string{"critical"}, &scanner.NucleiOptions{}, loader, fake, nil)
	if summary.Status != scanner.PhaseUncovered || summary.VulnerabilityConclusion != PocConclusionNotEvaluated {
		t.Fatalf("zero-template conclusion = %s/%s, want UNCOVERED/NOT_EVALUATED", summary.Status, summary.VulnerabilityConclusion)
	}
	if summary.UncoveredGroups != 2 || summary.UncoveredAssets != 2 || summary.ScannedAssets != 0 || summary.ExecutedTemplates != 0 || fake.calls != 0 {
		t.Fatalf("zero-template counts = %+v, scanner calls=%d", summary, fake.calls)
	}
	for _, group := range summary.Groups {
		if group.TemplateLoadOutcome != TemplateLoadFiltered || group.ReasonCode != "templates_filtered" {
			t.Fatalf("filtered load outcome was collapsed: %+v", group)
		}
	}
}

// Assets without generated tags are retained in one explicit untagged group
// and counted as uncovered without querying the template store.
// **Validates: Requirements 2.8, 2.9**
func TestPocCoverageUntaggedAssetsAreExplicitlyUncovered(t *testing.T) {
	assets := []*scanner.Asset{{Host: "untagged.test", Port: 8080, IsHTTP: true}}
	groups := pocTestGroups(assets, nil)
	if len(groups) != 1 || groups[0].GroupKey != "untagged" || !groups[0].Untagged || len(groups[0].Assets) != 1 {
		t.Fatalf("untagged grouping = %+v", groups)
	}
	loaderCalls := 0
	summary := executePocGroups(t.Context(), groups, nil, &scanner.NucleiOptions{},
		func(context.Context, []string, []string) (TemplateLoadResult, error) {
			loaderCalls++
			return TemplateLoadResult{}, nil
		}, nil, nil)
	if summary.Status != scanner.PhaseUncovered || summary.UncoveredGroups != 1 || summary.UncoveredAssets != 1 || loaderCalls != 0 {
		t.Fatalf("untagged coverage = %+v, loader calls=%d", summary, loaderCalls)
	}
	if summary.Groups[0].ReasonCode != "untagged_assets" {
		t.Fatalf("untagged reason = %q", summary.Groups[0].ReasonCode)
	}
}

// Template database failures remain FAILED/DB_ERROR rather than becoming an
// ordinary no-match or zero-finding result.
// **Validates: Requirements 2.8, 2.9**
func TestPocCoverageTemplateLoadFailureIsDistinct(t *testing.T) {
	assets := []*scanner.Asset{{Host: "load-failure.test", Port: 443, App: []string{"App[custom]"}, IsHTTP: true}}
	groups := pocTestGroups(assets, map[string][]string{"App": {"app"}})
	expected := errors.New("deterministic template database failure")
	summary := executePocGroups(t.Context(), groups, nil, &scanner.NucleiOptions{},
		func(context.Context, []string, []string) (TemplateLoadResult, error) {
			return TemplateLoadResult{Source: "mongo", Outcome: TemplateLoadDBError, ReasonCode: "mongo_nuclei_query_failed"}, expected
		}, nil, nil)

	if summary.Status != scanner.PhaseFailed || summary.FailedGroups != 1 || summary.VulnerabilityConclusion != PocConclusionNotEvaluated {
		t.Fatalf("load failure coverage = %+v", summary)
	}
	if summary.Groups[0].TemplateLoadOutcome != TemplateLoadDBError || summary.Groups[0].ReasonCode != "mongo_nuclei_query_failed" {
		t.Fatalf("load failure outcome was collapsed: %+v", summary.Groups[0])
	}
}

// A Nuclei execution error after successful loading is FAILED and does not
// count templates or assets as successfully executed/scanned.
// **Validates: Requirements 2.8, 2.9**
func TestPocCoverageExecutionFailureIsDistinct(t *testing.T) {
	assets := []*scanner.Asset{{Host: "execution-failure.test", Port: 443, App: []string{"App[custom]"}, IsHTTP: true}}
	groups := pocTestGroups(assets, map[string][]string{"App": {"app"}})
	fake := &fakePocNucleiScanner{scan: func(context.Context, *scanner.ScanConfig) (*scanner.ScanResult, error) {
		return nil, errors.New("deterministic nuclei execution failure")
	}}
	summary := executePocGroups(t.Context(), groups, nil, &scanner.NucleiOptions{},
		func(context.Context, []string, []string) (TemplateLoadResult, error) {
			return loadedTemplateResult(), nil
		},
		fake, nil)

	if summary.Status != scanner.PhaseFailed || summary.FailedGroups != 1 || summary.ValidTemplates != 1 || summary.ExecutedTemplates != 0 || summary.ScannedAssets != 0 {
		t.Fatalf("execution failure coverage = %+v", summary)
	}
	if summary.Groups[0].ReasonCode != scanner.ReasonExecutionError || summary.Groups[0].TemplateLoadOutcome != TemplateLoadLoaded {
		t.Fatalf("execution failure was collapsed: %+v", summary.Groups[0])
	}
}

// Findings emitted by a covered group are preserved in both the stream
// callback and aggregate result even when another group is uncovered.
// **Validates: Requirements 3.13**
func TestPocCoverageVulnerabilityResultPersistsAcrossUncoveredGroup(t *testing.T) {
	assets := []*scanner.Asset{
		{Host: "vulnerable.test", Port: 443, App: []string{"Covered[custom]"}, IsHTTP: true},
		{Host: "missing.test", Port: 443, App: []string{"Missing[custom]"}, IsHTTP: true},
	}
	groups := pocTestGroups(assets, map[string][]string{"Covered": {"covered"}, "Missing": {"missing"}})
	finding := &scanner.Vulnerability{Host: "vulnerable.test", Port: 443, Url: "https://vulnerable.test", PocFile: "covered-template"}
	persisted := make([]*scanner.Vulnerability, 0, 1)
	fake := &fakePocNucleiScanner{scan: func(_ context.Context, config *scanner.ScanConfig) (*scanner.ScanResult, error) {
		opts := config.Options.(*scanner.NucleiOptions)
		if len(opts.Tags) == 1 && opts.Tags[0] == "covered" {
			if opts.OnVulnerabilityFound != nil {
				opts.OnVulnerabilityFound(finding)
			}
			return completedPocResult(len(config.Assets), finding), nil
		}
		return completedPocResult(len(config.Assets)), nil
	}}
	loader := func(_ context.Context, tags, _ []string) (TemplateLoadResult, error) {
		if len(tags) == 1 && tags[0] == "covered" {
			return loadedTemplateResult(), nil
		}
		return TemplateLoadResult{Source: "local_store", Outcome: TemplateLoadNoMatch, ReasonCode: "templates_no_match"}, nil
	}
	baseOptions := &scanner.NucleiOptions{OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
		persisted = append(persisted, vul)
	}}

	summary := executePocGroups(t.Context(), groups, nil, baseOptions, loader, fake, nil)
	if summary.Status != scanner.PhasePartial || summary.VulnerabilityConclusion != PocConclusionFindings || summary.Vulnerabilities != 1 {
		t.Fatalf("finding summary = %+v", summary)
	}
	if len(summary.VulnerabilityResults) != 1 || summary.VulnerabilityResults[0] != finding || len(persisted) != 1 || persisted[0] != finding {
		t.Fatalf("finding was not preserved: aggregate=%v persisted=%v", summary.VulnerabilityResults, persisted)
	}
}

func TestExplicitPocTemplateCoverageClassifiesLoadOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		phase      PhaseResult
		load       TemplateLoadResult
		loadErr    error
		assets     int
		wantStatus scanner.PhaseStatus
		wantReason string
	}{
		{
			name:   "missing",
			phase:  NewPhaseResult("poc", scanner.Coverage{}, false),
			load:   TemplateLoadResult{Requested: 1, Outcome: TemplateLoadNoMatch},
			assets: 2, wantStatus: scanner.PhaseUncovered, wantReason: scanner.ReasonTemplateNoMatch,
		},
		{
			name:   "invalid",
			phase:  NewPhaseResult("poc", scanner.Coverage{}, false),
			load:   TemplateLoadResult{Requested: 1, Invalid: 1, Outcome: TemplateLoadInvalidContent},
			assets: 2, wantStatus: scanner.PhaseFailed, wantReason: scanner.ReasonTemplateInvalid,
		},
		{
			name:   "mixed valid and missing",
			phase:  NewPhaseResult("poc", scanner.Coverage{Input: 2, Attempted: 2, Succeeded: 2}, false),
			load:   TemplateLoadResult{Requested: 2, Loaded: 1, Contents: []string{"id: valid"}, Outcome: TemplateLoadNoMatch},
			assets: 2, wantStatus: scanner.PhasePartial, wantReason: scanner.ReasonTemplateNoMatch,
		},
		{
			name:    "store error",
			phase:   NewPhaseResult("poc", scanner.Coverage{}, false),
			load:    TemplateLoadResult{Requested: 1, Outcome: TemplateLoadStoreUnavailable},
			loadErr: errors.New("template store unavailable"),
			assets:  2, wantStatus: scanner.PhaseFailed, wantReason: scanner.ReasonTemplateUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyExplicitPocTemplateCoverage(tt.phase, tt.load, tt.loadErr, tt.assets)
			if got.Status != tt.wantStatus {
				t.Fatalf("status=%s, want %s; result=%+v", got.Status, tt.wantStatus, got)
			}
			if !containsReasonCode(got.ReasonCodes, tt.wantReason) {
				t.Fatalf("reason codes=%v, want %q", got.ReasonCodes, tt.wantReason)
			}
			if got.VulnerabilityConclusion != "" && got.VulnerabilityConclusion != "NOT_EVALUATED" && tt.wantStatus != scanner.PhasePartial {
				t.Fatalf("vulnerability conclusion=%q", got.VulnerabilityConclusion)
			}
		})
	}
}

func containsReasonCode(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
