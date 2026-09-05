package scanner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func quietHttpxLog(string, string, ...interface{}) {}

func targetArg(args []string) string {
	for i := range args {
		if args[i] == "-u" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// TestHttpxAttemptOutcomeClassification proves every explicit scheme attempt
// has a distinct result instead of collapsing to nil/zero update.
// **Validates: Requirements 2.5**
func TestHttpxAttemptOutcomeClassification(t *testing.T) {
	const target = "https://outcome.example.test:443"
	tests := []struct {
		name   string
		stdout string
		err    error
		want   HttpxAttemptOutcome
	}{
		{name: "success", stdout: `{"input":"https://outcome.example.test:443","scheme":"https","status-code":200}`, want: HttpxOutcomeSuccess},
		{name: "timeout", err: context.DeadlineExceeded, want: HttpxOutcomeTimeout},
		{name: "execution error", err: errors.New("binary unavailable"), want: HttpxOutcomeExecError},
		{name: "no output", stdout: "  \n", want: HttpxOutcomeNoOutput},
		{name: "parse error", stdout: "not-json", want: HttpxOutcomeParseError},
		{name: "no matching asset", stdout: `{"input":"https://other.example.test:443","scheme":"https","status-code":200}`, want: HttpxOutcomeNoMatch},
		{name: "explicit non http", stdout: `{"input":"https://outcome.example.test:443","failed":true,"error":"not an http service"}`, want: HttpxOutcomeNotHTTP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset := &Asset{Host: "outcome.example.test", Port: 443, Service: "https"}
			s := NewHttpxScanner()
			s.execute = func(context.Context, []string, ExecuteOpts) (*ExecuteResult, error) {
				return &ExecuteResult{Stdout: tt.stdout, Duration: 5 * time.Millisecond}, tt.err
			}
			got := s.scanSingleTargetCLI(context.Background(), target, &HttpxOptions{Timeout: 1, Concurrency: 1}, map[string]*Asset{target: asset}, quietHttpxLog)
			if got.Outcome != tt.want {
				t.Fatalf("outcome = %s, want %s", got.Outcome, tt.want)
			}
		})
	}
}

// TestHttpxScanSuccessfulUpdatePreserved fixes the successful-response baseline
// while the scanner now returns input references and diagnostics.
// **Validates: Requirements 3.8**
func TestHttpxScanSuccessfulUpdatePreserved(t *testing.T) {
	asset := &Asset{Host: "success.example.test", Port: 8080, Service: "unknown", Source: "naabu", Screenshot: "existing"}
	s := NewHttpxScanner()
	s.execute = func(_ context.Context, args []string, _ ExecuteOpts) (*ExecuteResult, error) {
		target := targetArg(args)
		if strings.HasPrefix(target, "http://") {
			return &ExecuteResult{Stdout: `{"input":"http://success.example.test:8080","url":"https://success.example.test/home","scheme":"https","status-code":200,"title":"Home","tech":["Nginx"],"webserver":"nginx","content-type":"text/html","content-length":123,"headers":"X-Test: yes","body":"hello","favicon-mmh3":"42","favicon":"aWNvbg=="}`}, nil
		}
		return &ExecuteResult{}, nil
	}
	result, err := s.Scan(context.Background(), &ScanConfig{Assets: []*Asset{asset}, Options: &HttpxOptions{Concurrency: 1, Timeout: 1, FollowRedirects: true, StatusCode: true, Title: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 1 || result.Assets[0] != asset {
		t.Fatal("Scan must return the original input asset reference")
	}
	if asset.Service != "https" || asset.HttpStatus != "200" || asset.Title != "Home" || asset.Server != "nginx" || !asset.IsHTTP {
		t.Fatalf("successful HTTP fields not preserved: %#v", asset)
	}
	if !reflect.DeepEqual(asset.App, []string{"Nginx[httpx]"}) || asset.HttpHeader != "X-Test: yes" || asset.HttpBody != "hello" || asset.IconHash != "42" || string(asset.IconData) != "icon" {
		t.Fatalf("successful tech/header/body/favicon fields not preserved: %#v", asset)
	}
	if asset.ContentType != "text/html" || asset.ContentLength != 123 || asset.Screenshot != "existing" || asset.Source != "naabu" {
		t.Fatalf("successful update cleared existing data: %#v", asset)
	}
	want := Coverage{Input: 1, Attempted: 1, Succeeded: 1}
	if !reflect.DeepEqual(result.Diagnostic.Coverage, want) || result.Diagnostic.Status != PhaseComplete {
		t.Fatalf("diagnostic = %#v, want coverage %#v COMPLETE", result.Diagnostic, want)
	}
}

// TestHttpxMixedSuccessTimeoutIntegration is the required no-network mixed
// fixture: one asset updates and one remains intact with 1/2 PARTIAL coverage.
// **Validates: Requirements 2.4, 2.5, 3.7, 3.8**
func TestHttpxMixedSuccessTimeoutIntegration(t *testing.T) {
	success := &Asset{Authority: "ok.example.test:8080", Host: "ok.example.test", Port: 8080, Service: "unknown", Source: "naabu"}
	timedOut := &Asset{Authority: "slow.example.test:9443", Host: "slow.example.test", Port: 9443, Service: "https", Source: "naabu", Title: "retained", App: []string{"Existing"}}
	s := NewHttpxScanner()
	s.execute = func(_ context.Context, args []string, _ ExecuteOpts) (*ExecuteResult, error) {
		target := targetArg(args)
		if target == "http://ok.example.test:8080" {
			return &ExecuteResult{Stdout: `{"input":"http://ok.example.test:8080","scheme":"http","status-code":204,"title":"ok"}`}, nil
		}
		if strings.Contains(target, "slow.example.test") {
			return &ExecuteResult{}, context.DeadlineExceeded
		}
		return &ExecuteResult{}, nil
	}
	result, err := s.Scan(context.Background(), &ScanConfig{Assets: []*Asset{success, timedOut}, Options: &HttpxOptions{Concurrency: 2, Timeout: 1, StatusCode: true, Title: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 2 || result.Assets[0] != success || result.Assets[1] != timedOut {
		t.Fatal("mixed scan did not retain both input assets")
	}
	if success.HttpStatus != "204" || success.Service != "http" || success.ProtocolProbeStatus != ProtocolProbeConfirmed {
		t.Fatalf("success asset not updated: %#v", success)
	}
	if timedOut.Service != "https" || timedOut.Title != "retained" || !reflect.DeepEqual(timedOut.App, []string{"Existing"}) || timedOut.ProtocolProbeStatus != ProtocolProbeUnconfirmed {
		t.Fatalf("timeout asset was not retained unchanged: %#v", timedOut)
	}
	want := Coverage{Input: 2, Attempted: 2, Succeeded: 1, TimedOut: 1, Unconfirmed: 1, ZeroUpdate: 1}
	if !reflect.DeepEqual(result.Diagnostic.Coverage, want) {
		t.Fatalf("coverage = %#v, want %#v", result.Diagnostic.Coverage, want)
	}
	if result.Diagnostic.Status != PhasePartial {
		t.Fatalf("status = %s, want PARTIAL", result.Diagnostic.Status)
	}
	outcomes := map[string]int{}
	for _, diagnostic := range result.Diagnostic.Targets {
		outcomes[diagnostic.Outcome]++
	}
	if outcomes[string(HttpxOutcomeSuccess)] != 1 || outcomes[string(HttpxOutcomeTimeout)] != 2 || outcomes[string(HttpxOutcomeNoOutput)] != 0 {
		t.Fatalf("per-attempt outcomes = %#v", outcomes)
	}
}

// TestHttpxNotHTTPRequiresBothCompletedSchemes ensures a partial negative probe
// stays UNCONFIRMED and cannot clear the service or asset.
// **Validates: Requirements 2.5, 3.8**
func TestHttpxNotHTTPRequiresBothCompletedSchemes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		second     HttpxAttemptOutcome
		wantStatus string
		wantPhase  PhaseStatus
	}{
		{name: "both explicit non http", second: HttpxOutcomeNotHTTP, wantStatus: ProtocolProbeNotHTTPConfirmed, wantPhase: PhaseComplete},
		{name: "one non http and one no output", second: HttpxOutcomeNoOutput, wantStatus: ProtocolProbeUnconfirmed, wantPhase: PhaseFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asset := &Asset{Host: "negative.example.test", Port: 9000, Service: "custom", Source: "naabu"}
			var calls atomic.Int32
			s := NewHttpxScanner()
			s.execute = func(_ context.Context, args []string, _ ExecuteOpts) (*ExecuteResult, error) {
				target := targetArg(args)
				if calls.Add(1) == 1 || tc.second == HttpxOutcomeNotHTTP {
					return &ExecuteResult{Stdout: `{"input":"` + target + `","failed":true}`}, nil
				}
				return &ExecuteResult{}, nil
			}
			result, err := s.Scan(context.Background(), &ScanConfig{Assets: []*Asset{asset}, Options: &HttpxOptions{Concurrency: 1, Timeout: 1}})
			if err != nil {
				t.Fatal(err)
			}
			if asset.ProtocolProbeStatus != tc.wantStatus || asset.Service != "custom" || result.Diagnostic.Status != tc.wantPhase {
				t.Fatalf("asset/status = %#v / %s, want probe=%s phase=%s", asset, result.Diagnostic.Status, tc.wantStatus, tc.wantPhase)
			}
		})
	}
}

// TestHttpxSkipsAlreadySuccessfulEvidence prevents duplicate external probes.
// **Validates: Requirements 2.4, 3.7, 3.8**
func TestHttpxSkipsAlreadySuccessfulEvidence(t *testing.T) {
	asset := &Asset{Host: "known.example.test", Port: 443, Service: "http", HttpStatus: "200", IsHTTP: true}
	var calls atomic.Int32
	s := NewHttpxScanner()
	s.execute = func(context.Context, []string, ExecuteOpts) (*ExecuteResult, error) {
		calls.Add(1)
		return &ExecuteResult{}, nil
	}
	result, err := s.Scan(context.Background(), &ScanConfig{Assets: []*Asset{asset}, Options: &HttpxOptions{Concurrency: 1, Timeout: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("executor called %d times despite successful evidence", calls.Load())
	}
	want := Coverage{Input: 1, Succeeded: 1}
	if !reflect.DeepEqual(result.Diagnostic.Coverage, want) || result.Diagnostic.Status != PhaseComplete || asset.Service != "http" {
		t.Fatalf("successful evidence changed: asset=%#v diagnostic=%#v", asset, result.Diagnostic)
	}
}
