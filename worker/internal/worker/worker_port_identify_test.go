package worker

import (
	"context"
	"reflect"
	"testing"

	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

func TestMergeNmapPortIdentifyResultsPreservesDiscoveryFields(t *testing.T) {
	discovered := &scanner.Asset{
		Authority: "Example.TEST:443", Host: "Example.TEST", Port: 443, Category: "domain",
		Service: "unknown", Server: "existing-server", Banner: "existing-banner", Title: "existing-title",
		App: []string{"existing-app"}, HttpStatus: "200", HttpHeader: "X-Test: preserved",
		HttpBody: "preserved body", Cert: "preserved cert", IconHash: "icon-hash", IconData: []byte{1, 2, 3},
		Screenshot: "preserved.png", IsCDN: true, CName: "cdn.example.test", IsCloud: true, IsHTTP: true,
		IPV4:   []scanner.IPInfo{{IP: "192.0.2.1", Location: "existing-location"}},
		Source: "naabu", Path: "/existing", ContentLength: 42, ContentType: "text/html",
		ContentWords: 3, ContentLines: 2, Duration: 9, RequestRaw: "request", ResponseRaw: "response",
		TakeoverRisk: true, TakeoverService: "service", TakeoverCName: "takeover.example.test",
	}
	original := cloneAsset(discovered)

	merged := mergeNmapPortIdentifyResults([]*scanner.Asset{discovered}, []scanner.PortIdentifyResult{{
		Host: "example.test.", Port: 443, Outcome: scanner.PortOpen, ResolvedIP: "192.0.2.2",
		Service: "https", Product: "nginx", Version: "1.24",
	}}, false)

	if merged.Phase.Status != scanner.PhaseComplete {
		t.Fatalf("phase status = %s, want COMPLETE", merged.Phase.Status)
	}
	if len(merged.Assets) != 1 {
		t.Fatalf("merged assets = %d, want 1", len(merged.Assets))
	}

	want := cloneAsset(original)
	want.Service = "https"
	want.App = []string{"existing-app", "nginx:1.24"}
	want.IPV4 = []scanner.IPInfo{{IP: "192.0.2.1", Location: "existing-location"}, {IP: "192.0.2.2"}}
	if !reflect.DeepEqual(merged.Assets[0], want) {
		t.Fatalf("merged asset lost or changed fields:\n got: %#v\nwant: %#v", merged.Assets[0], want)
	}
	if !reflect.DeepEqual(discovered, original) {
		t.Fatalf("discovered input was mutated:\n got: %#v\nwant: %#v", discovered, original)
	}
}

func TestMergeNmapPortIdentifyResultsOnlyConfiguredClosedIsRemoved(t *testing.T) {
	outcomes := []scanner.PortIdentifyOutcome{
		scanner.PortClosed,
		scanner.PortTimeout,
		scanner.PortExecError,
		scanner.PortParseError,
		scanner.PortFiltered,
		scanner.PortNoRecord,
	}
	discovered := make([]*scanner.Asset, 0, len(outcomes))
	results := make([]scanner.PortIdentifyResult, 0, len(outcomes))
	for i, outcome := range outcomes {
		port := 8000 + i
		discovered = append(discovered, &scanner.Asset{Host: "retain.example.test", Port: port, Source: "naabu"})
		results = append(results, scanner.PortIdentifyResult{Host: "retain.example.test", Port: port, Outcome: outcome})
	}

	kept := mergeNmapPortIdentifyResults(discovered, results, false)
	if len(kept.Assets) != len(discovered) {
		t.Fatalf("default merge retained %d assets, want %d", len(kept.Assets), len(discovered))
	}
	if kept.Phase.Status != scanner.PhasePartial {
		t.Fatalf("default merge status = %s, want PARTIAL", kept.Phase.Status)
	}
	if len(kept.Phase.Diagnostic.Targets) != 5 {
		t.Fatalf("unconfirmed diagnostics = %d, want 5", len(kept.Phase.Diagnostic.Targets))
	}

	excluded := mergeNmapPortIdentifyResults(discovered, results, true)
	if len(excluded.Assets) != len(discovered)-1 {
		t.Fatalf("configured close policy retained %d assets, want %d", len(excluded.Assets), len(discovered)-1)
	}
	for _, asset := range excluded.Assets {
		if asset.Port == 8000 {
			t.Fatal("explicit CLOSED asset was not removed by configured policy")
		}
	}
	for _, port := range []int{8001, 8002, 8003, 8004, 8005} {
		if !containsHostPort(excluded.Assets, "retain.example.test", port) {
			t.Fatalf("non-CLOSED outcome removed discovered port %d", port)
		}
	}
}

func TestMergeNmapPortIdentifyResultsNormalizesAndStableDeduplicates(t *testing.T) {
	discovered := []*scanner.Asset{
		{Host: "Example.COM.", Port: 80, Source: "first"},
		{Host: " example.com ", Port: 80, Title: "filled-from-duplicate", App: []string{"existing"}},
	}
	merged := mergeNmapPortIdentifyResults(discovered, []scanner.PortIdentifyResult{{
		Host: "EXAMPLE.COM", Port: 80, Outcome: scanner.PortOpen, Service: "http", Product: "server",
	}}, false)

	if len(merged.Assets) != 1 {
		t.Fatalf("normalized duplicate count = %d, want 1", len(merged.Assets))
	}
	asset := merged.Assets[0]
	if asset.Host != "Example.COM." || asset.Source != "first" || asset.Title != "filled-from-duplicate" {
		t.Fatalf("stable duplicate merge changed first identity or lost fields: %#v", asset)
	}
	if !reflect.DeepEqual(asset.App, []string{"existing", "server"}) {
		t.Fatalf("merged apps = %#v, want existing + server", asset.App)
	}
}

// TestPortIdentifyScannerWorkerPartialFixture exercises the scanner-to-worker
// contract without a network process: four discovered ports enter the Worker,
// the scanner reports two OPEN and two TIMEOUT outcomes, and the Worker must
// retain all four while reporting PARTIAL coverage.
// **Validates: Requirements 2.3, 3.5, 3.6**
func TestPortIdentifyScannerWorkerPartialFixture(t *testing.T) {
	const host = "139.159.180.18"
	discovered := []*scanner.Asset{
		{Authority: host + ":8080", Host: host, Port: 8080, Source: "naabu"},
		{Authority: host + ":5800", Host: host, Port: 5800, Source: "naabu"},
		{Authority: host + ":443", Host: host, Port: 443, Source: "naabu", Cert: "existing-cert", Screenshot: "existing-shot"},
		{Authority: host + ":80", Host: host, Port: 80, Source: "naabu", HttpStatus: "200", Title: "existing-title"},
	}
	identifyResults := []scanner.PortIdentifyResult{
		{Host: host, Port: 80, Outcome: scanner.PortOpen, Service: "http", Product: "nginx", Version: "1.24"},
		{Host: host, Port: 443, Outcome: scanner.PortOpen, Service: "https", Product: "tls-server"},
		{Host: host, Port: 8080, Outcome: scanner.PortTimeout, ErrorCode: scanner.NmapReasonTimeout},
		{Host: host, Port: 5800, Outcome: scanner.PortTimeout, ErrorCode: scanner.NmapReasonTimeout},
	}

	worker := &Worker{scanners: map[string]scanner.Scanner{
		"nmap": &portIdentifyFixtureScanner{result: &scanner.ScanResult{
			Assets:              []*scanner.Asset{{Host: host, Port: 80}, {Host: host, Port: 443}},
			PortIdentifyResults: identifyResults,
		}},
	}}
	result := worker.executePortIdentifyWithNmapResult(
		context.Background(),
		&scheduler.TaskInfo{TaskId: "fixture-task-1", MainTaskId: "fixture-task"},
		discovered,
		&scheduler.PortIdentifyConfig{Tool: "nmap", Timeout: 1, Concurrency: 1},
		"fixture-org",
	)

	if result.Phase.Status != scanner.PhasePartial {
		t.Fatalf("phase status = %s, want PARTIAL", result.Phase.Status)
	}
	wantCoverage := scanner.Coverage{Input: 4, Attempted: 4, Succeeded: 2, TimedOut: 2}
	if !reflect.DeepEqual(result.Phase.Coverage, wantCoverage) {
		t.Fatalf("coverage = %#v, want %#v", result.Phase.Coverage, wantCoverage)
	}
	if len(result.Assets) != 4 {
		t.Fatalf("final assets = %d, want 4: %#v", len(result.Assets), result.Assets)
	}
	wantPorts := []int{8080, 5800, 443, 80}
	for i, port := range wantPorts {
		if result.Assets[i].Port != port {
			t.Fatalf("stable final order[%d] = %d, want %d", i, result.Assets[i].Port, port)
		}
		if result.Assets[i].Source != "naabu" {
			t.Fatalf("port %d source = %q, want naabu", port, result.Assets[i].Source)
		}
	}
	if !containsHostPort(result.Assets, host, 8080) || !containsHostPort(result.Assets, host, 5800) {
		t.Fatal("timed-out discovered ports were not retained")
	}
	if result.Assets[2].Cert != "existing-cert" || result.Assets[2].Screenshot != "existing-shot" {
		t.Fatalf("443 enrichment cleared certificate/screenshot: %#v", result.Assets[2])
	}
	if result.Assets[3].HttpStatus != "200" || result.Assets[3].Title != "existing-title" {
		t.Fatalf("80 enrichment cleared HTTP fields: %#v", result.Assets[3])
	}
	if len(result.Phase.Diagnostic.Targets) != 2 {
		t.Fatalf("timeout diagnostics = %d, want 2", len(result.Phase.Diagnostic.Targets))
	}
}

type portIdentifyFixtureScanner struct {
	result *scanner.ScanResult
	err    error
}

func (s *portIdentifyFixtureScanner) Name() string { return "nmap-fixture" }

func (s *portIdentifyFixtureScanner) Scan(context.Context, *scanner.ScanConfig) (*scanner.ScanResult, error) {
	return s.result, s.err
}

func containsHostPort(assets []*scanner.Asset, host string, port int) bool {
	key := normalizedHostPort(host, port)
	for _, asset := range assets {
		if asset != nil && normalizedHostPort(asset.Host, asset.Port) == key {
			return true
		}
	}
	return false
}
