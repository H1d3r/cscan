package scanner

import (
	"context"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

type naabuPropertyLine struct {
	text  string
	valid bool
	host  string
	port  int
}

var naabuPropertyLines = []naabuPropertyLine{
	{text: `{"ip":"198.51.100.10","port":80}`, valid: true, host: "198.51.100.10", port: 80},
	{text: `{"ip":"198.51.100.10","port":443}`, valid: true, host: "198.51.100.10", port: 443},
	{text: `{"ip":"198.51.100.10","port":80}`, valid: true, host: "198.51.100.10", port: 80}, // duplicate host-port
	{text: `{"ip":"198.51.100.10","port":65535}`, valid: true, host: "198.51.100.10", port: 65535},
	{text: `{"ip":"198.51.100.11","port":8080}`, valid: true, host: "198.51.100.11", port: 8080},
	{text: `{`, valid: false}, // malformed JSON
	{text: `{"ip":"not-an-ip","port":443}`, valid: false},
	{text: `{"ip":"198.51.100.10","port":0}`, valid: false},
	{text: `{"ip":"198.51.100.10","port":65536}`, valid: false},
	{text: `{"ip":"198.51.100.10","port":-1}`, valid: false},
	{text: `{"ip":"198.51.100.10"}`, valid: false},
	{text: `   `, valid: false}, // empty lines are not classified records
}

type naabuPropertyExpectation struct {
	source        NaabuParseSource
	keys          map[string]struct{}
	nonEmptyLines int
	stats         NaabuParseStats
}

func naabuPropertyContent(lineCodes []int) []byte {
	var content strings.Builder
	for _, code := range lineCodes {
		content.WriteString(naabuPropertyLines[code].text)
		content.WriteByte('\n')
	}
	return []byte(content.String())
}

func expectedNaabuPropertyParse(target string, fileCodes, stdoutCodes []int, outcome string) naabuPropertyExpectation {
	fileContent := naabuPropertyContent(fileCodes)
	stdoutContent := naabuPropertyContent(stdoutCodes)
	expectation := naabuPropertyExpectation{
		keys: map[string]struct{}{},
		stats: NaabuParseStats{
			FileBytes:   len(fileContent),
			StdoutBytes: len(stdoutContent),
		},
	}

	selected := [][]int(nil)
	switch {
	case (outcome == "timeout" || outcome == "exit_error") && len(fileContent) > 0 && len(stdoutContent) > 0:
		expectation.source = NaabuParseSourceFileStdout
		selected = [][]int{fileCodes, stdoutCodes}
	case len(fileContent) > 0:
		expectation.source = NaabuParseSourceFile
		selected = [][]int{fileCodes}
	case len(stdoutContent) > 0:
		expectation.source = NaabuParseSourceStdout
		selected = [][]int{stdoutCodes}
	default:
		expectation.source = NaabuParseSourceNone
	}

	for _, sourceCodes := range selected {
		expectation.stats.ParsedBytes += len(naabuPropertyContent(sourceCodes))
		for _, code := range sourceCodes {
			line := naabuPropertyLines[code]
			if strings.TrimSpace(line.text) == "" {
				continue
			}
			expectation.nonEmptyLines++
			expectation.stats.TotalLines++
			if !line.valid {
				expectation.stats.InvalidLines++
				continue
			}
			expectation.stats.ValidLines++
			host := line.host
			if net.ParseIP(target) == nil {
				host = target
			}
			key := host + ":" + strconv.Itoa(line.port)
			if _, duplicate := expectation.keys[key]; duplicate {
				expectation.stats.DuplicateLines++
				continue
			}
			expectation.keys[key] = struct{}{}
			expectation.stats.AcceptedPorts++
		}
	}
	return expectation
}

func naabuAssetKeys(assets []*Asset) map[string]struct{} {
	keys := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		keys[asset.Host+":"+strconv.Itoa(asset.Port)] = struct{}{}
	}
	return keys
}

func sameNaabuPropertyKeys(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, found := right[key]; !found {
			return false
		}
	}
	return true
}

func runNaabuParseInvariantProperty(t *testing.T, seed int64) {
	t.Helper()
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 250
	parameters.MaxSize = 24
	parameters.MaxShrinkCount = 100
	parameters.Rng = rand.New(rand.NewSource(seed))
	properties := gopter.NewProperties(parameters)
	outcomes := []string{"success", "timeout", "exit_error"}
	targets := []string{"198.51.100.250", "scan.example.test"}

	properties.Property("parsed assets equal the unique valid host-port set and classifications conserve non-empty lines", prop.ForAll(
		func(fileCodes, stdoutCodes []int, outcomeIndex, targetIndex int) bool {
			outcome := outcomes[outcomeIndex]
			target := targets[targetIndex]
			fileContent := naabuPropertyContent(fileCodes)
			stdoutContent := naabuPropertyContent(stdoutCodes)
			assets, stats, source := parseNaabuOutput(target, fileContent, stdoutContent, outcome)
			expected := expectedNaabuPropertyParse(target, fileCodes, stdoutCodes, outcome)

			return source == expected.source &&
				sameNaabuPropertyKeys(naabuAssetKeys(assets), expected.keys) &&
				stats.FileBytes == expected.stats.FileBytes &&
				stats.StdoutBytes == expected.stats.StdoutBytes &&
				stats.ParsedBytes == expected.stats.ParsedBytes &&
				stats.TotalLines == expected.stats.TotalLines &&
				stats.ValidLines == expected.stats.ValidLines &&
				stats.InvalidLines == expected.stats.InvalidLines &&
				stats.DuplicateLines == expected.stats.DuplicateLines &&
				stats.AcceptedPorts == expected.stats.AcceptedPorts &&
				stats.AcceptedPorts+stats.DuplicateLines+stats.InvalidLines == expected.nonEmptyLines
		},
		gen.SliceOf(gen.IntRange(0, len(naabuPropertyLines)-1)),
		gen.SliceOf(gen.IntRange(0, len(naabuPropertyLines)-1)),
		gen.IntRange(0, len(outcomes)-1),
		gen.IntRange(0, len(targets)-1),
	))
	t.Logf("gopter seed=%d", seed)
	properties.TestingRun(t)
}

// TestProperty3_NaabuParseInvariantFixedSeed gives a reproducible counterexample
// when the parser violates source selection, host-port uniqueness, or accounting.
// **Validates: Requirements 2.1, 2.2, 3.1, 3.2, 3.3, 3.4**
func TestProperty3_NaabuParseInvariantFixedSeed(t *testing.T) {
	runNaabuParseInvariantProperty(t, 2026041001)
}

// TestProperty3_NaabuParseInvariantRandomSeed broadens the offline input space.
// The emitted seed makes every failure reproducible with the fixed-seed helper.
func TestProperty3_NaabuParseInvariantRandomSeed(t *testing.T) {
	runNaabuParseInvariantProperty(t, time.Now().UnixNano())
}

func flagValue(args []string, flag string) (string, int) {
	count := 0
	value := ""
	for i, arg := range args {
		if arg != flag {
			continue
		}
		count++
		if i+1 < len(args) {
			value = args[i+1]
		}
	}
	return value, count
}

func requireFlagValue(t *testing.T, args []string, flag, want string) {
	t.Helper()
	got, count := flagValue(args, flag)
	if count != 1 {
		t.Fatalf("flag %s appears %d times, want 1; args=%q", flag, count, args)
	}
	if got != want {
		t.Fatalf("value after %s=%q, want %q; args=%q", flag, got, want, args)
	}
}

func TestEffectiveNaabuProcessConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		workers     int
		targetCount int
		want        int
	}{
		{name: "no targets", workers: 50, targetCount: 0, want: 0},
		{name: "negative target count", workers: 50, targetCount: -1, want: 0},
		{name: "default workers", workers: 0, targetCount: 3, want: 1},
		{name: "negative workers", workers: -1, targetCount: 3, want: 1},
		{name: "limited by target count", workers: 4, targetCount: 2, want: 2},
		{name: "limited by process cap", workers: 50, targetCount: 20, want: 5},
		{name: "configured workers", workers: 3, targetCount: 20, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveNaabuProcessConcurrency(tt.workers, tt.targetCount); got != tt.want {
				t.Fatalf("EffectiveNaabuProcessConcurrency(%d, %d)=%d, want %d", tt.workers, tt.targetCount, got, tt.want)
			}
		})
	}
}

func TestCountNaabuProcessTargetsExpandsAndDeduplicates(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   int
	}{
		{name: "empty", target: "", want: 0},
		{name: "duplicate host", target: "example.com\nexample.com", want: 1},
		{name: "cidr", target: "192.0.2.0/30", want: 2},
		{name: "ip range", target: "198.51.100.10-198.51.100.12", want: 3},
		{
			name:   "mixed targets",
			target: "192.0.2.0/30\n192.0.2.1\n198.51.100.10-198.51.100.12\nhttps://example.com",
			want:   6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountNaabuProcessTargets(tt.target); got != tt.want {
				t.Fatalf("CountNaabuProcessTargets(%q)=%d, want %d", tt.target, got, tt.want)
			}
		})
	}
}

func TestBuildNaabuArgsSeparatesTargetAndProbeTimeouts(t *testing.T) {
	for _, scanType := range []string{"s", "c"} {
		t.Run(scanType, func(t *testing.T) {
			opts := &NaabuOptions{
				Ports:             "80,443",
				Rate:              3000,
				TargetTimeout:     75,
				ProbeTimeoutMs:    1250,
				ScanType:          scanType,
				PortThreshold:     100,
				SkipHostDiscovery: true,
				ExcludeCDN:        true,
				ExcludeHosts:      "192.0.2.1",
				Retries:           2,
				WarmUpTime:        1,
				Workers:           37,
				Verify:            true,
			}

			args := buildNaabuArgs("example.com", "80,443", "/tmp/naabu-output.json", opts)
			requireFlagValue(t, args, "-timeout", "1250")
			requireFlagValue(t, args, "-s", scanType)
			requireFlagValue(t, args, "-c", "37")
			requireFlagValue(t, args, "-o", "/tmp/naabu-output.json")

			if got := opts.targetTimeoutDuration(); got != 75*time.Second {
				t.Fatalf("target timeout duration=%s, want %s", got, 75*time.Second)
			}
		})
	}
}

func TestBuildNaabuArgsPassesTopPortsThrough(t *testing.T) {
	for _, tt := range []struct {
		ports string
		want  string
	}{
		{ports: "top100", want: "100"},
		{ports: "top1000", want: "1000"},
	} {
		t.Run(tt.ports, func(t *testing.T) {
			opts := &NaabuOptions{
				Ports:          tt.ports,
				Rate:           1000,
				TargetTimeout:  10,
				ProbeTimeoutMs: 1000,
				ScanType:       "c",
				Workers:        1,
			}
			args := buildNaabuArgs("host1", "", "/tmp/naabu-output.json", opts)
			requireFlagValue(t, args, "-tp", tt.want)
			if _, count := flagValue(args, "-p"); count != 0 {
				t.Fatalf("top ports unexpectedly emitted -p; args=%q", args)
			}
		})
	}
}

// **Validates: Requirements 2.1, 2.2, 3.1, 3.2, 3.3, 3.4, 3.15**
func TestParseNaabuOutputSelectsActualSource(t *testing.T) {
	fileRecord := []byte(`{"ip":"198.51.100.10","port":80}` + "\n")
	stdoutRecord := []byte(`{"ip":"198.51.100.10","port":443}` + "\n")

	tests := []struct {
		name       string
		outcome    string
		file       []byte
		stdout     []byte
		wantSource NaabuParseSource
		wantPorts  []int
		wantBytes  int
	}{
		{name: "normal file is authoritative", outcome: "success", file: fileRecord, stdout: stdoutRecord, wantSource: NaabuParseSourceFile, wantPorts: []int{80}, wantBytes: len(fileRecord)},
		{name: "normal stdout fallback", outcome: "success", stdout: stdoutRecord, wantSource: NaabuParseSourceStdout, wantPorts: []int{443}, wantBytes: len(stdoutRecord)},
		{name: "timeout merges file and stdout", outcome: "timeout", file: fileRecord, stdout: stdoutRecord, wantSource: NaabuParseSourceFileStdout, wantPorts: []int{80, 443}, wantBytes: len(fileRecord) + len(stdoutRecord)},
		{name: "normal double empty is confirmed empty", outcome: "success", wantSource: NaabuParseSourceNone, wantPorts: nil, wantBytes: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assets, stats, source := parseNaabuOutput("198.51.100.10", tt.file, tt.stdout, tt.outcome)
			if source != tt.wantSource {
				t.Fatalf("source=%q, want %q", source, tt.wantSource)
			}
			if stats.ParsedBytes != tt.wantBytes {
				t.Fatalf("parsed bytes=%d, want %d", stats.ParsedBytes, tt.wantBytes)
			}
			if len(assets) != len(tt.wantPorts) {
				t.Fatalf("assets=%d, want %d", len(assets), len(tt.wantPorts))
			}
			for i, port := range tt.wantPorts {
				if assets[i].Port != port {
					t.Fatalf("asset[%d].Port=%d, want %d", i, assets[i].Port, port)
				}
			}
		})
	}
}

func TestNaabuTimeoutStdoutFixtureRetainsFourPortsAndIsPartial(t *testing.T) {
	stdout := []byte("{\"ip\":\"139.159.180.18\",\"port\":8080}\n" +
		"{\"ip\":\"139.159.180.18\",\"port\":5800}\n" +
		"{\"ip\":\"139.159.180.18\",\"port\":443}\n" +
		"{\"ip\":\"139.159.180.18\",\"port\":80}\n")
	assets, stats, source := parseNaabuOutput("139.159.180.18", nil, stdout, "timeout")
	if source != NaabuParseSourceStdout {
		t.Fatalf("source=%q, want stdout", source)
	}
	if stats.ParsedBytes != len(stdout) || stats.AcceptedPorts != 4 {
		t.Fatalf("stats=%+v, want parsed_bytes=%d accepted_ports=4", stats, len(stdout))
	}
	if len(assets) != 4 {
		t.Fatalf("assets=%d, want 4", len(assets))
	}
	if status := deriveNaabuPhaseStatus(context.Background(), Coverage{Input: 1, Attempted: 1, TimedOut: 1, Unconfirmed: 1}, false, len(assets)); status != PhasePartial {
		t.Fatalf("timeout stdout phase status=%s, want %s", status, PhasePartial)
	}
}

func TestParseNaabuOutputToleratesBadRecordsAndDeduplicates(t *testing.T) {
	content := []byte("{\"ip\":\"198.51.100.10\",\"port\":80}\n" +
		"{\"ip\":\"198.51.100.10\",\"port\":80}\n" +
		"not-json\n" +
		"{\"ip\":\"not-an-ip\",\"port\":443}\n" +
		"{\"ip\":\"198.51.100.10\",\"port\":0}\n" +
		"{\"ip\":\"198.51.100.10\",\"port\":65536}\n")
	assets, stats, source := parseNaabuOutput("198.51.100.10", content, nil, "success")
	if source != NaabuParseSourceFile || len(assets) != 1 || assets[0].Port != 80 {
		t.Fatalf("unexpected parsed output: source=%s assets=%+v", source, assets)
	}
	if stats.TotalLines != 6 || stats.ValidLines != 2 || stats.InvalidLines != 4 || stats.DuplicateLines != 1 || stats.AcceptedPorts != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.AcceptedPorts+stats.DuplicateLines+stats.InvalidLines != stats.TotalLines {
		t.Fatalf("record classifications do not account for every line: %+v", stats)
	}
}

func TestNaabuDomainHostKeepsHostAndResolvedIP(t *testing.T) {
	assets, _, _ := parseNaabuOutput("scan.example.test", []byte(`{"ip":"198.51.100.12","port":8443}`+"\n"), nil, "success")
	if len(assets) != 1 {
		t.Fatalf("assets=%d, want 1", len(assets))
	}
	asset := assets[0]
	if asset.Host != "scan.example.test" || len(asset.IPV4) != 1 || asset.IPV4[0].IP != "198.51.100.12" {
		t.Fatalf("domain/IP preservation failed: %+v", asset)
	}
}

func TestNaabuThresholdAndPhaseOutcomes(t *testing.T) {
	if !exceedsNaabuPortThreshold(NaabuParseStats{AcceptedPorts: 3}, 2) {
		t.Fatal("expected threshold to be exceeded")
	}
	if exceedsNaabuPortThreshold(NaabuParseStats{AcceptedPorts: 2}, 2) {
		t.Fatal("threshold must allow exactly the configured number of ports")
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name      string
		ctx       context.Context
		coverage  Coverage
		threshold bool
		assets    int
		want      PhaseStatus
	}{
		{name: "normal empty is complete", ctx: context.Background(), coverage: Coverage{Input: 1, Attempted: 1, Succeeded: 1}, want: PhaseComplete},
		{name: "timeout with valid output is partial", ctx: context.Background(), coverage: Coverage{Input: 1, Attempted: 1, TimedOut: 1, Unconfirmed: 1}, assets: 1, want: PhasePartial},
		{name: "exit error with no output failed", ctx: context.Background(), coverage: Coverage{Input: 1, Attempted: 1, Failed: 1, Unconfirmed: 1}, want: PhaseFailed},
		{name: "cancellation wins", ctx: canceledCtx, coverage: Coverage{Input: 1}, want: PhaseCanceled},
		{name: "threshold is partial", ctx: context.Background(), coverage: Coverage{Input: 1, Attempted: 1, Succeeded: 1, Unconfirmed: 1}, threshold: true, want: PhasePartial},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveNaabuPhaseStatus(tt.ctx, tt.coverage, tt.threshold, tt.assets); got != tt.want {
				t.Fatalf("status=%s, want %s", got, tt.want)
			}
		})
	}
}

// Validates: Requirements 2.1, 2.2.
func TestNaabuStructuredParseEventUsesActualStdoutBytes(t *testing.T) {
	stdout := []byte("{\"ip\":\"139.159.180.18\",\"port\":8080}\n" +
		"{\"ip\":\"139.159.180.18\",\"port\":5800}\n" +
		"{\"ip\":\"139.159.180.18\",\"port\":443}\n" +
		"{\"ip\":\"139.159.180.18\",\"port\":80}\n")
	assets, stats, source := parseNaabuOutput("139.159.180.18", nil, stdout, "timeout")
	if len(assets) != 4 {
		t.Fatalf("assets=%d, want 4", len(assets))
	}

	var gotEvent, gotPhase, gotOutcome string
	var gotFields map[string]interface{}
	emitNaabuParseEvent(func(event, phase, outcome string, fields map[string]interface{}) {
		gotEvent, gotPhase, gotOutcome, gotFields = event, phase, outcome, fields
	}, "139.159.180.18", NaabuTargetDiagnostic{
		Source: source, ProcessOutcome: "timeout", ExitCode: -1, OutputFileEmpty: true, Stats: stats,
	})

	if gotEvent != EventNaabuParseComplete || gotPhase != "naabu" || gotOutcome != "timeout" {
		t.Fatalf("event tuple=(%q,%q,%q)", gotEvent, gotPhase, gotOutcome)
	}
	if gotFields["source"] != string(NaabuParseSourceStdout) || gotFields["stdout_bytes"] != len(stdout) || gotFields["parsed_bytes"] != len(stdout) {
		t.Fatalf("stdout event fields=%#v, want source=stdout bytes=%d", gotFields, len(stdout))
	}
	if gotFields["file_bytes"] != 0 || gotFields["output_file_empty"] != true || gotFields["accepted_ports"] != 4 {
		t.Fatalf("unexpected parse event fields=%#v", gotFields)
	}
}
