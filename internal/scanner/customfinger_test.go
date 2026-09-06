package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"cscan/internal/model"
)

func TestParsedConditionType(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		want      string
	}{
		{name: "positive body", condition: `body="x"`, want: "body"},
		{name: "negative body", condition: `body!="x"`, want: "body"},
		{name: "negative title", condition: `title!="x"`, want: "title"},
		{name: "negative header", condition: `header!="x"`, want: "header"},
		{name: "negative server", condition: `server!="x"`, want: "server"},
		{name: "equals in value", condition: `body="a=b"`, want: "body"},
		{name: "outer parentheses", condition: `((header!="x"))`, want: "header"},
		{name: "unknown format", condition: `not a condition`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsedConditionType(tt.condition); got != tt.want {
				t.Fatalf("parsedConditionType(%q) = %q, want %q", tt.condition, got, tt.want)
			}
		})
	}
}

func TestUnknownConditionSummaryAcceptsNegativeConditions(t *testing.T) {
	engine := NewCustomFingerprintEngine([]*model.Fingerprint{{
		Name:    "negative conditions",
		Rule:    `body!="x" && title!="x" && header!="x" && server!="x"`,
		Enabled: true,
	}})

	total, types := engine.unknownConditionSummary(true, false)
	if total != 0 || len(types) != 0 {
		t.Fatalf("unknownConditionSummary() = (%d, %v), want (0, [])", total, types)
	}
}

func TestExtractQuotedValueKeepsInnerDoubleQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "daffodil footer",
			input: `"<div id="right_footer">Design & Development by Daffodil Software Ltd</div>"`,
			want:  `<div id="right_footer">Design & Development by Daffodil Software Ltd</div>`,
		},
		{
			name:  "energine footer",
			input: `"<div id="footer"><span class="copyright">Powered by <a href="http://energine.org">Energine</a><br/>"`,
			want:  `<div id="footer"><span class="copyright">Powered by <a href="http://energine.org">Energine</a><br/>`,
		},
		{
			name:  "escaped quote",
			input: `"id=\"swagger-ui"`,
			want:  `id="swagger-ui`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractQuotedValue(tt.input)
			if got != tt.want {
				t.Fatalf("extractQuotedValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchRuleBodyWithInnerDoubleQuotes(t *testing.T) {
	engine := NewCustomFingerprintEngine(nil)
	data := &FingerprintData{
		Body: `<html><div id="right_footer">Design & Development by Daffodil Software Ltd</div></html>`,
	}

	if !engine.matchRule(`body="<div id="right_footer">Design & Development by Daffodil Software Ltd</div>"`, data) {
		t.Fatal("expected full body rule with inner quotes to match")
	}

	if engine.matchRule(`body="<div id="footer"><span class="copyright">Powered by <a href="http://energine.org">Energine</a><br/>"`, data) {
		t.Fatal("expected unrelated full body rule not to match")
	}
}

func TestMatchWithIdPrefersBuiltinWappalyzerOverCustomDuplicate(t *testing.T) {
	engine := NewCustomFingerprintEngine([]*model.Fingerprint{
		{
			Name:    "Fireblade",
			Rule:    `title="Fireblade"`,
			Source:  "custom",
			Enabled: true,
		},
		{
			Name:      "Fireblade",
			HTML:      []string{`Fireblade`},
			Source:    "wappalyzer",
			IsBuiltin: true,
			Enabled:   true,
		},
	})

	matches := engine.MatchWithId(&FingerprintData{Title: "Fireblade", Body: "Fireblade"})
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].Source != "wappalyzer" {
		t.Fatalf("Source = %q, want wappalyzer", matches[0].Source)
	}
	if !matches[0].IsBuiltin {
		t.Fatal("expected builtin wappalyzer match to be preferred")
	}
}

func TestFormatAppWithSourcesUsesRealFingerprintSource(t *testing.T) {
	wappalyzerResult := &AppDetectionResult{
		Name:         "VentryShield",
		OriginalName: "VentryShield",
		Sources:      []string{"wappalyzer"},
	}
	if got := formatAppWithSources(wappalyzerResult); got != "VentryShield[wappalyzer]" {
		t.Fatalf("formatAppWithSources() = %q, want VentryShield[wappalyzer]", got)
	}

	customResult := &AppDetectionResult{
		Name:         "Daffodil-CRM",
		OriginalName: "Daffodil-CRM",
		Sources:      []string{"custom"},
		CustomIDs:    []string{"69f36180002636c8d5a5ebc0"},
	}
	if got := formatAppWithSources(customResult); got != "Daffodil-CRM[custom(69f36180002636c8d5a5ebc0)]" {
		t.Fatalf("formatAppWithSources() = %q, want custom id suffix", got)
	}
}

func TestFormatAppWithSourcesIncludesAllFourSources(t *testing.T) {
	result := &AppDetectionResult{
		Name:         "Kibana",
		OriginalName: "Kibana",
		Sources:      []string{"active", "custom", "wappalyzer", "httpx"},
		CustomIDs:    []string{"custom-id"},
		ActiveIDs:    []string{"active-id"},
	}
	want := "Kibana[httpx+wappalyzer+custom(custom-id)+active(active-id)]"
	if got := formatAppWithSources(result); got != want {
		t.Fatalf("formatAppWithSources() = %q, want %q", got, want)
	}
}

func TestMergeExistingAppDetectionsAddsMissingSourceSuffix(t *testing.T) {
	appResults := make(map[string]*AppDetectionResult)
	mergeExistingAppDetections(appResults, []string{"Elasticsearch Kibana"})
	result := appResults["elasticsearch kibana"]
	if result == nil {
		t.Fatal("expected app result")
	}
	if got := formatAppWithSources(result); got != "Elasticsearch Kibana[httpx]" {
		t.Fatalf("formatAppWithSources() = %q, want Elasticsearch Kibana[httpx]", got)
	}
}

func TestMergeActiveFingerprintAppCombinesExistingSources(t *testing.T) {
	asset := &Asset{App: []string{"Kibana[httpx+custom(custom-id)]"}}
	fp := &model.Fingerprint{Name: "Kibana", Enabled: true}
	fp.Id = primitive.NewObjectID()

	if !mergeActiveFingerprintApp(asset, fp) {
		t.Fatal("expected first active fingerprint merge to change asset.App")
	}
	want := "Kibana[httpx+custom(custom-id)+active(" + fp.Id.Hex() + ")]"
	if len(asset.App) != 1 || asset.App[0] != want {
		t.Fatalf("asset.App = %#v, want %#v", asset.App, []string{want})
	}
	if mergeActiveFingerprintApp(asset, fp) {
		t.Fatal("expected duplicate active fingerprint merge to leave asset.App unchanged")
	}
	if len(asset.App) != 1 || asset.App[0] != want {
		t.Fatalf("asset.App after duplicate merge = %#v, want %#v", asset.App, []string{want})
	}
}

func TestRunActiveFingerprintNotifiesChangedAssetOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("GeoServer marker"))
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(serverURL.Port())
	if err != nil {
		t.Fatal(err)
	}

	first := &model.Fingerprint{Name: "GeoServer", Rule: `body="GeoServer marker"`, ActivePaths: []string{"/first"}, Enabled: true}
	first.Id = primitive.NewObjectID()
	second := &model.Fingerprint{Name: "Nacos", Rule: `body="GeoServer marker"`, ActivePaths: []string{"/second"}, Enabled: true}
	second.Id = primitive.NewObjectID()

	scanner := NewFingerprintScanner()
	scanner.SetCustomFingerprintEngine(NewCustomFingerprintEngineWithActive(nil, []*model.Fingerprint{first, second}))
	asset := &Asset{Host: serverURL.Hostname(), Port: port, Service: "http", HttpStatus: "200"}
	var snapshots []*Asset

	scanner.RunActiveFingerprint(context.Background(), []*Asset{asset}, &FingerprintOptions{Concurrency: 2}, nil, func(updated *Asset) {
		snapshots = append(snapshots, updated)
	})

	if len(snapshots) != 1 {
		t.Fatalf("callback count = %d, want 1", len(snapshots))
	}
	joined := strings.Join(snapshots[0].App, "\n")
	if !strings.Contains(joined, "GeoServer[active("+first.Id.Hex()+")]") {
		t.Fatalf("callback App missing GeoServer result: %#v", snapshots[0].App)
	}
	if !strings.Contains(joined, "Nacos[active("+second.Id.Hex()+")]") {
		t.Fatalf("callback App missing Nacos result: %#v", snapshots[0].App)
	}

	asset.App[0] = "mutated"
	if snapshots[0].App[0] == "mutated" {
		t.Fatal("callback App shares backing array with scanned asset")
	}
}

type observedRoundTripFunc func(*http.Request) (*http.Response, error)

func (f observedRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunActiveFingerprintWaitsForStartedRequestAfterCancel(t *testing.T) {
	started := make(chan struct{})
	requestCanceled := make(chan struct{})
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		close(started)
		<-r.Context().Done()
		close(requestCanceled)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		server.CloseClientConnections()
		server.Close()
	})

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(serverURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	fp := &model.Fingerprint{Name: "Test", Rule: `body="marker"`, ActivePaths: []string{"/"}, Enabled: true}
	fp.Id = primitive.NewObjectID()
	scanner := NewFingerprintScanner()
	scanner.SetCustomFingerprintEngine(NewCustomFingerprintEngineWithActive(nil, []*model.Fingerprint{fp}))

	lifecycle := make(chan string, 2)
	client := server.Client()
	baseTransport := client.Transport
	client.Transport = observedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := baseTransport.RoundTrip(req)
		lifecycle <- "roundtrip_done"
		return resp, err
	})
	scanner.client = client

	done := make(chan struct{})
	go func() {
		scanner.RunActiveFingerprint(ctx, []*Asset{{Host: serverURL.Hostname(), Port: port, Service: "http", HttpStatus: "200"}}, &FingerprintOptions{Concurrency: 1}, nil, nil)
		lifecycle <- "scan_done"
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active fingerprint request did not start")
	}
	cancel()

	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("started active fingerprint request did not observe cancellation")
	}

	select {
	case event := <-lifecycle:
		if event != "roundtrip_done" {
			t.Fatalf("first lifecycle event = %q, want roundtrip_done", event)
		}
	case <-time.After(time.Second):
		t.Fatal("active fingerprint HTTP round trip did not exit after cancellation")
	}

	select {
	case event := <-lifecycle:
		if event != "scan_done" {
			t.Fatalf("second lifecycle event = %q, want scan_done", event)
		}
	case <-time.After(time.Second):
		t.Fatal("active scan did not wait for the canceled request to exit")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active scan completion was not signaled")
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("canceled active fingerprint handler did not exit")
	}
	server.Close()
}

// TestMatchWithEvidencePreservesInterpreterTruth locks the pre-governance rule
// truth item-by-item while requiring the new explanatory interface to report
// both matching and non-matching rules. **Validates: Requirements 2.6, 3.9, 3.10**
func TestMatchWithEvidencePreservesInterpreterTruth(t *testing.T) {
	gbk, err := encodeToGBK("管理平台")
	if err != nil {
		t.Fatal(err)
	}
	data := &FingerprintData{
		Title:        "Portal",
		Body:         `<html><meta name="generator" content="ExampleCMS"><script src="/assets/example.js"></script>welcome bootstrap</html>`,
		BodyBytes:    gbk,
		Headers:      http.Header{"X-Product": {"Example"}, "Set-Cookie": {"example_session=secret"}},
		HeaderString: "HTTP/1.1 200 OK\nX-Product: Example\nSet-Cookie: example_session=secret",
		Server:       "nginx",
		URL:          "https://example.test/login",
		FaviconHash:  "12345",
		Cookies:      "example_session=secret",
	}

	fingerprints := []*model.Fingerprint{
		{Name: "And", Rule: `title="Portal" && header="X-Product"`, Enabled: true},
		{Name: "Or", Rule: `body="absent" || server="nginx"`, Enabled: true},
		{Name: "Negated", Rule: `title!="Denied" && body="welcome"`, Enabled: true},
		{Name: "NoMatch", Rule: `title="Denied"`, Enabled: true},
		{Name: "ARL", HTML: []string{"welcome"}, Source: "arl-webapp", Enabled: true},
		{Name: "Wappalyzer", Scripts: []string{"example\\.js"}, Source: "wappalyzer", IsBuiltin: true, Enabled: true},
		{Name: "GBK", Rule: `body="管理平台"`, Enabled: true},
	}
	engine := NewCustomFingerprintEngine(fingerprints)
	findings := engine.MatchWithEvidence(data)
	if len(findings) != len(fingerprints) {
		t.Fatalf("len(findings) = %d, want %d", len(findings), len(fingerprints))
	}

	for i, fp := range fingerprints {
		var legacyTruth bool
		switch {
		case fp.Rule != "":
			legacyTruth = engine.matchRule(fp.Rule, data)
		case engine.matchARLWebappRules(fp, data):
			legacyTruth = true
		default:
			legacyTruth = engine.matchWappalyzerRules(fp, data)
		}
		if findings[i].RawMatched != legacyTruth {
			t.Fatalf("finding %q RawMatched = %v, legacy interpreter = %v", fp.Name, findings[i].RawMatched, legacyTruth)
		}
		if findings[i].RawMatched && len(findings[i].Evidence) == 0 {
			t.Fatalf("finding %q has no explanatory evidence", fp.Name)
		}
	}

	legacyProjection := engine.MatchWithId(data)
	if len(legacyProjection) != len(fingerprints)-1 {
		t.Fatalf("legacy projection matched %d fingerprints, want %d", len(legacyProjection), len(fingerprints)-1)
	}
}

// TestMatchWithEvidenceMarksMissingAndTruncatedResponsesIncomplete verifies
// incomplete evidence without changing negation or positive-match truth.
// **Validates: Requirements 2.6, 3.9**
func TestMatchWithEvidenceMarksMissingAndTruncatedResponsesIncomplete(t *testing.T) {
	tests := []struct {
		name string
		fp   *model.Fingerprint
		data *FingerprintData
	}{
		{
			name: "missing response preserves negation truth",
			fp:   &model.Fingerprint{Name: "Missing", Rule: `body!="forbidden"`, Enabled: true},
			data: &FingerprintData{ResponseMissing: true},
		},
		{
			name: "truncated body",
			fp:   &model.Fingerprint{Name: "Body", Rule: `body="sensitive-marker"`, Enabled: true},
			data: &FingerprintData{Body: "sensitive-marker", BodyTruncated: true},
		},
		{
			name: "truncated header",
			fp:   &model.Fingerprint{Name: "Header", Rule: `header="X-Secret"`, Enabled: true},
			data: &FingerprintData{HeaderString: "X-Secret: raw-secret", HeaderTruncated: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding := NewCustomFingerprintEngine([]*model.Fingerprint{tt.fp}).MatchWithEvidence(tt.data)
			if len(finding) != 1 || !finding[0].RawMatched {
				t.Fatalf("finding = %#v, want one raw match", finding)
			}
			if len(finding[0].Evidence) == 0 {
				t.Fatal("expected explanatory evidence")
			}
			for _, evidence := range finding[0].Evidence {
				if evidence.Complete {
					t.Fatalf("evidence unexpectedly complete: %#v", evidence)
				}
				if !strings.HasPrefix(evidence.MatchedValueDigest, "sha256:") {
					t.Fatalf("digest = %q, want sha256 prefix", evidence.MatchedValueDigest)
				}
				serialized := evidence.Pattern + evidence.MatchedValueDigest
				if strings.Contains(serialized, "sensitive-marker") || strings.Contains(serialized, "raw-secret") {
					t.Fatalf("evidence retained sensitive raw value: %#v", evidence)
				}
			}
		})
	}
}
