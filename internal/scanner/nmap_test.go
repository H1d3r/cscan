package scanner

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

var discardNmapLog logFunc = func(string, ...interface{}) {}

func nmapScannerWithResult(result nmapCommandResult) *NmapScanner {
	scanner := NewNmapScanner()
	scanner.commandRunner = func(context.Context, []string) nmapCommandResult { return result }
	return scanner
}

func scanNmapTestPort(t *testing.T, scanner *NmapScanner, ctx context.Context, targets []string, port int) []PortIdentifyResult {
	t.Helper()
	return scanner.scanSinglePortWithLogger(
		ctx,
		targets,
		port,
		&NmapOptions{Timeout: 1, Concurrent: 1},
		discardNmapLog,
		discardNmapLog,
		discardNmapLog,
	)
}

// TestNmapPortIdentifyOutcomeMappings verifies that every supported Nmap
// conclusion has a distinct typed outcome and stable failure reason.
// **Validates: Requirements 2.3, 3.5, 3.6, 3.15**
func TestNmapPortIdentifyOutcomeMappings(t *testing.T) {
	closedXML := `<nmaprun><host><address addr="192.0.2.10" addrtype="ipv4"/><ports><port protocol="tcp" portid="443"><state state="closed"/><service name="should-not-leak" product="closed-product" version="9"/></port></ports></host></nmaprun>`
	filteredXML := `<nmaprun><host><address addr="192.0.2.10" addrtype="ipv4"/><ports><port protocol="tcp" portid="443"><state state="filtered"/><service name="should-not-leak" product="filtered-product" version="9"/></port></ports></host></nmaprun>`

	tests := []struct {
		name       string
		result     nmapCommandResult
		want       PortIdentifyOutcome
		wantReason string
	}{
		{name: "closed", result: nmapCommandResult{Stdout: []byte(closedXML)}, want: PortClosed},
		{name: "filtered", result: nmapCommandResult{Stdout: []byte(filteredXML)}, want: PortFiltered},
		{name: "timeout", result: nmapCommandResult{WaitErr: context.DeadlineExceeded}, want: PortTimeout, wantReason: NmapReasonTimeout},
		{name: "launch error", result: nmapCommandResult{StartErr: errors.New("binary unavailable")}, want: PortExecError, wantReason: NmapReasonLaunchError},
		{name: "nonzero exit", result: nmapCommandResult{WaitErr: errors.New("exit status 2")}, want: PortExecError, wantReason: NmapReasonNonzeroExit},
		{name: "xml parse error", result: nmapCommandResult{Stdout: []byte(`<nmaprun><host>`)}, want: PortParseError, wantReason: NmapReasonXMLParse},
		{name: "no host record", result: nmapCommandResult{Stdout: []byte(`<nmaprun></nmaprun>`)}, want: PortNoRecord, wantReason: NmapReasonNoHostRecord},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := scanNmapTestPort(t, nmapScannerWithResult(test.result), context.Background(), []string{"192.0.2.10"}, 443)
			if len(got) != 1 {
				t.Fatalf("result count = %d, want 1: %#v", len(got), got)
			}
			if got[0].Outcome != test.want || got[0].ErrorCode != test.wantReason {
				t.Fatalf("outcome = %q reason = %q, want %q/%q", got[0].Outcome, got[0].ErrorCode, test.want, test.wantReason)
			}
			if test.want != PortOpen && (got[0].ResolvedIP != "" || got[0].Service != "" || got[0].Product != "" || got[0].Version != "") {
				t.Fatalf("non-open result leaked metadata: %#v", got[0])
			}
		})
	}
}

// TestNmapOpenMetadataPreservesBaseline verifies the normal successful path and
// the compatibility Asset projection used before task 3.4.
// **Validates: Requirements 3.5, 3.15**
func TestNmapOpenMetadataPreservesBaseline(t *testing.T) {
	xmlOutput := `<nmaprun><host><address addr="192.0.2.20" addrtype="ipv4"/><hostnames><hostname name="web.example.test"/></hostnames><ports><port protocol="tcp" portid="80"><state state="open"/><service name="http" product="nginx" version="1.24"/></port></ports></host></nmaprun>`
	got := scanNmapTestPort(t, nmapScannerWithResult(nmapCommandResult{Stdout: []byte(xmlOutput)}), context.Background(), []string{"WEB.EXAMPLE.TEST."}, 80)
	want := PortIdentifyResult{
		Host:       "web.example.test",
		ResolvedIP: "192.0.2.20",
		Port:       80,
		Outcome:    PortOpen,
		Service:    "http",
		Product:    "nginx",
		Version:    "1.24",
	}
	if !reflect.DeepEqual(got, []PortIdentifyResult{want}) {
		t.Fatalf("open result = %#v, want %#v", got, want)
	}

	assets := portIdentifyResultsToAssets(got, discardNmapLog)
	if len(assets) != 1 || assets[0].Host != "web.example.test" || assets[0].Port != 80 || assets[0].Service != "http" {
		t.Fatalf("open asset projection changed: %#v", assets)
	}
	if !reflect.DeepEqual(assets[0].App, []string{"nginx:1.24"}) {
		t.Fatalf("open product projection = %#v", assets[0].App)
	}
	if len(assets[0].IPV4) != 1 || assets[0].IPV4[0].IP != "192.0.2.20" {
		t.Fatalf("open resolved IP projection = %#v", assets[0].IPV4)
	}
}

// TestNmapSamePortAcrossHostsAlignsByHost ensures XML records for a shared port
// are never assigned by port alone.
// **Validates: Requirements 2.3, 3.5**
func TestNmapSamePortAcrossHostsAlignsByHost(t *testing.T) {
	xmlOutput := `<nmaprun>
<host><address addr="192.0.2.31" addrtype="ipv4"/><hostnames><hostname name="alpha.example.test"/></hostnames><ports><port protocol="tcp" portid="443"><state state="open"/><service name="https" product="alpha-server" version="1"/></port></ports></host>
<host><address addr="192.0.2.32" addrtype="ipv4"/><hostnames><hostname name="beta.example.test"/></hostnames><ports><port protocol="tcp" portid="443"><state state="closed"/><service name="must-not-leak" product="beta-server" version="2"/></port></ports></host>
</nmaprun>`
	got := scanNmapTestPort(t, nmapScannerWithResult(nmapCommandResult{Stdout: []byte(xmlOutput)}), context.Background(), []string{"beta.example.test", "alpha.example.test"}, 443)
	if len(got) != 2 {
		t.Fatalf("result count = %d, want 2", len(got))
	}
	if got[0].Host != "beta.example.test" || got[0].Outcome != PortClosed {
		t.Fatalf("beta result misaligned: %#v", got[0])
	}
	if got[0].ResolvedIP != "" || got[0].Product != "" {
		t.Fatalf("closed beta leaked open-only metadata: %#v", got[0])
	}
	if got[1].Host != "alpha.example.test" || got[1].Outcome != PortOpen || got[1].ResolvedIP != "192.0.2.31" || got[1].Product != "alpha-server" {
		t.Fatalf("alpha result misaligned: %#v", got[1])
	}
}

// TestNmapAbsentHostRecordCompletesEveryTarget verifies a partial XML document
// still produces an auditable result for the omitted target.
// **Validates: Requirements 2.3**
func TestNmapAbsentHostRecordCompletesEveryTarget(t *testing.T) {
	xmlOutput := `<nmaprun><host><address addr="192.0.2.41" addrtype="ipv4"/><ports><port protocol="tcp" portid="8080"><state state="open"/><service name="http-proxy"/></port></ports></host></nmaprun>`
	got := scanNmapTestPort(t, nmapScannerWithResult(nmapCommandResult{Stdout: []byte(xmlOutput)}), context.Background(), []string{"192.0.2.41", "192.0.2.42"}, 8080)
	if len(got) != 2 {
		t.Fatalf("result count = %d, want 2", len(got))
	}
	if got[0].Outcome != PortOpen || got[1].Outcome != PortNoRecord || got[1].ErrorCode != NmapReasonNoHostRecord {
		t.Fatalf("partial host records = %#v", got)
	}
}

// TestNmapCancellationIsDistinctFromTimeout verifies explicit user/parent
// cancellation cannot be reported as a timeout or execution failure.
// **Validates: Requirements 2.3, 3.15**
func TestNmapCancellationIsDistinctFromTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanner := NewNmapScanner()
	scanner.commandRunner = func(ctx context.Context, _ []string) nmapCommandResult {
		return nmapCommandResult{WaitErr: ctx.Err()}
	}
	got := scanNmapTestPort(t, scanner, ctx, []string{"192.0.2.50", "192.0.2.51"}, 22)
	if len(got) != 2 {
		t.Fatalf("result count = %d, want 2", len(got))
	}
	for _, result := range got {
		if result.Outcome != PortCanceled || result.ErrorCode != NmapReasonCanceled {
			t.Fatalf("cancellation result = %#v", result)
		}
	}
}

// TestNmapResultCardinality verifies normalization/deduplication and one result
// per dispatched host-port across concurrent per-port commands.
// **Validates: Requirements 2.3, 3.5, 3.15**
func TestNmapResultCardinality(t *testing.T) {
	scanner := NewNmapScanner()
	scanner.commandRunner = func(_ context.Context, args []string) nmapCommandResult {
		port := 0
		for i := range args {
			if args[i] == "-p" && i+1 < len(args) {
				_, _ = fmt.Sscanf(args[i+1], "%d", &port)
			}
		}
		xmlOutput := fmt.Sprintf(`<nmaprun>
<host><address addr="192.0.2.61" addrtype="ipv4"/><hostnames><hostname name="alpha.example.test"/></hostnames><ports><port protocol="tcp" portid="%d"><state state="open"/><service name="service-%d"/></port></ports></host>
<host><address addr="192.0.2.62" addrtype="ipv4"/><hostnames><hostname name="beta.example.test"/></hostnames><ports><port protocol="tcp" portid="%d"><state state="open"/><service name="service-%d"/></port></ports></host>
</nmaprun>`, port, port, port, port)
		return nmapCommandResult{Stdout: []byte(xmlOutput)}
	}

	assets, got := scanner.runNmapWithLogger(
		context.Background(),
		[]string{"Alpha.Example.Test.", "alpha.example.test", "beta.example.test"},
		&NmapOptions{Ports: "80,80,443", Timeout: 1, Concurrent: 2},
		nil,
		nil,
		discardNmapLog,
		discardNmapLog,
		discardNmapLog,
		discardNmapLog,
	)
	if len(got) != 4 {
		t.Fatalf("result cardinality = %d, want 2 normalized hosts x 2 unique ports = 4: %#v", len(got), got)
	}
	if len(assets) != 4 {
		t.Fatalf("open asset cardinality = %d, want 4", len(assets))
	}

	keys := make([]string, 0, len(got))
	for _, result := range got {
		if result.Outcome != PortOpen {
			t.Fatalf("unexpected non-open result: %#v", result)
		}
		keys = append(keys, fmt.Sprintf("%s:%d", result.Host, result.Port))
	}
	sort.Strings(keys)
	want := []string{"alpha.example.test:443", "alpha.example.test:80", "beta.example.test:443", "beta.example.test:80"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("host-port keys = %#v, want %#v", keys, want)
	}
}
