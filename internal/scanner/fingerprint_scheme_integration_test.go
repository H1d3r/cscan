package scanner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFingerprintSchemePipelineUsesVerifiedHTTPS is an offline integration
// fixture. The asset starts with incorrect Service=http on logical port 443,
// while all network I/O is redirected to a local TLS server. It verifies that
// httpx's successful HTTPS evidence is persisted and reused by asset updates,
// screenshots, and certificate collection.
// **Validates: Requirements 2.4, 2.7, 3.7, 3.11**
func TestFingerprintSchemePipelineUsesVerifiedHTTPS(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "local-tls")
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><title>Local TLS</title><body>retained</body></html>`)
	}))
	defer tlsServer.Close()

	localAddr := tlsServer.Listener.Addr().String()
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, localAddr)
		},
	}}

	asset := &Asset{
		Authority: "scheme.example.test:443", Host: "scheme.example.test", Port: 443,
		Service: "http", Source: "naabu", App: []string{"Existing"},
	}
	scanner := NewFingerprintScanner()
	scanner.client = client

	var mu sync.Mutex
	var httpxTargets, successfulHTTPXTargets, screenshotTargets, certTargets []string
	var beforeScreenshot *Asset
	scanner.runHttpx = func(ctx context.Context, assets []*Asset, _ *FingerprintOptions, taskLog func(string, string, ...interface{})) error {
		httpx := NewHttpxScanner()
		httpx.execute = func(ctx context.Context, args []string, _ ExecuteOpts) (*ExecuteResult, error) {
			target := targetArg(args)
			mu.Lock()
			httpxTargets = append(httpxTargets, target)
			mu.Unlock()

			resp, err := client.Get(target)
			if err != nil {
				return &ExecuteResult{}, nil
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}
			parsed, err := url.Parse(target)
			if err != nil {
				return nil, err
			}
			response := map[string]interface{}{
				"input": target, "url": target, "scheme": parsed.Scheme,
				"status-code": resp.StatusCode, "title": "Local TLS",
				"webserver": "local-tls", "headers": "Content-Type: text/html",
				"body": string(body),
			}
			if parsed.Scheme == SchemeHTTPS {
				mu.Lock()
				successfulHTTPXTargets = append(successfulHTTPXTargets, target)
				mu.Unlock()
			} else {
				// The TLS listener's HTTP 400 rejection is protocol-negative
				// evidence, not a successful clear-text HTTP service.
				response["failed"] = true
			}
			line, err := json.Marshal(response)
			if err != nil {
				return nil, err
			}
			return &ExecuteResult{Stdout: string(line)}, nil
		}
		_, err := httpx.Scan(ctx, &ScanConfig{
			Assets:     assets,
			Options:    &HttpxOptions{Concurrency: 1, Timeout: 2, StatusCode: true, Title: true, Body: true},
			TaskLogger: taskLog,
		})
		return err
	}
	scanner.captureScreen = func(_ context.Context, target string, _ func(string, string, ...interface{})) string {
		mu.Lock()
		defer mu.Unlock()
		copyOfAsset := *asset
		copyOfAsset.App = append([]string(nil), asset.App...)
		beforeScreenshot = &copyOfAsset
		screenshotTargets = append(screenshotTargets, target)
		return "" // deliberate screenshot failure
	}
	scanner.fetchCert = func(_ context.Context, host string, port int, _ time.Duration) *CertResult {
		mu.Lock()
		certTargets = append(certTargets, fmt.Sprintf("%s:%d", host, port))
		mu.Unlock()
		return &CertResult{Host: host, Port: port, Authority: fmt.Sprintf("%s:%d", host, port)}
	}

	var persisted []*Asset
	result, err := scanner.Scan(context.Background(), &ScanConfig{
		Assets: []*Asset{asset},
		Options: &FingerprintOptions{
			Tool: "httpx", TargetTimeout: 3, Concurrency: 1,
			Screenshot: true, Cert: true,
		},
		OnAssetUpdated: func(updated *Asset) {
			copyOfAsset := *updated
			persisted = append(persisted, &copyOfAsset)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(httpxTargets) != 2 || !strings.HasPrefix(httpxTargets[0], "https://") {
		t.Fatalf("httpx targets/order = %#v, want HTTPS first then alternate probe", httpxTargets)
	}
	if !reflect.DeepEqual(successfulHTTPXTargets, []string{"https://scheme.example.test:443"}) {
		t.Fatalf("successful httpx targets = %#v", successfulHTTPXTargets)
	}
	if asset.Service != SchemeHTTPS || asset.ProtocolProbeStatus != ProtocolProbeConfirmed || !asset.IsHTTP {
		t.Fatalf("successful protocol evidence not persisted: %#v", asset)
	}
	if got := buildAssetTargetURL(asset); got != "https://scheme.example.test:443" {
		t.Fatalf("asset target URL = %q", got)
	}
	if !reflect.DeepEqual(screenshotTargets, []string{"https://scheme.example.test:443"}) {
		t.Fatalf("screenshot targets = %#v", screenshotTargets)
	}
	if !reflect.DeepEqual(certTargets, []string{"scheme.example.test:443"}) || len(result.CertResults) != 1 {
		t.Fatalf("certificate targets/results = %#v / %#v", certTargets, result.CertResults)
	}
	if len(persisted) == 0 || persisted[len(persisted)-1].Service != SchemeHTTPS || persisted[len(persisted)-1].ProtocolProbeStatus != ProtocolProbeConfirmed {
		t.Fatalf("persisted asset snapshots did not retain HTTPS evidence: %#v", persisted)
	}

	// Screenshot failure is diagnostic-only and retains the complete asset state
	// observed immediately before the screenshot call.
	if beforeScreenshot == nil || !reflect.DeepEqual(asset, beforeScreenshot) {
		t.Fatalf("screenshot failure changed successful results:\n before=%#v\n after=%#v", beforeScreenshot, asset)
	}
	if result.Diagnostic == nil || len(result.Diagnostic.Targets) != 1 {
		t.Fatalf("missing screenshot diagnostic: %#v", result.Diagnostic)
	}
	diagnostic := result.Diagnostic.Targets[0]
	if diagnostic.ReasonCode != ReasonScreenshotFailed || diagnostic.Target != "https://scheme.example.test:443" || diagnostic.Metadata["selected_scheme"] != SchemeHTTPS {
		t.Fatalf("screenshot diagnostic = %#v", diagnostic)
	}
}

// TestFingerprintSchemePreservesVerifiedNonstandardCombinations fixes the
// preservation boundary: a verified HTTP response on 443 remains HTTP, while
// verified HTTPS on a nonstandard port remains HTTPS for screenshots and certs.
// No network process is launched.
// **Validates: Requirements 3.7, 3.11**
func TestFingerprintSchemePreservesVerifiedNonstandardCombinations(t *testing.T) {
	tests := []struct {
		name     string
		asset    *Asset
		wantURL  string
		wantCert bool
	}{
		{
			name:    "verified HTTP on 443",
			asset:   &Asset{Host: "http443.example.test", Port: 443, Service: SchemeHTTP, IsHTTP: true, HttpStatus: "200", Title: "HTTP"},
			wantURL: "http://http443.example.test:443", wantCert: false,
		},
		{
			name:    "verified HTTPS on nonstandard port",
			asset:   &Asset{Host: "https8448.example.test", Port: 8448, Service: SchemeHTTPS, IsHTTP: true, HttpStatus: "200", Title: "HTTPS"},
			wantURL: "https://https8448.example.test:8448", wantCert: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanner := NewFingerprintScanner()
			var screenshotTarget string
			scanner.captureScreen = func(_ context.Context, target string, _ func(string, string, ...interface{})) string {
				screenshotTarget = target
				return "captured"
			}
			scanner.runAdditionalFingerprint(context.Background(), test.asset, &FingerprintOptions{Screenshot: true, TargetTimeout: 1}, quietHttpxLog, nil)

			if got := buildAssetTargetURL(test.asset); got != test.wantURL {
				t.Fatalf("asset URL = %q, want %q", got, test.wantURL)
			}
			if screenshotTarget != test.wantURL || test.asset.Screenshot != "captured" {
				t.Fatalf("screenshot target/result = %q / %q", screenshotTarget, test.asset.Screenshot)
			}
			if got := isCertFetchTarget(test.asset); got != test.wantCert {
				t.Fatalf("isCertFetchTarget = %v, want %v", got, test.wantCert)
			}
		})
	}
}
