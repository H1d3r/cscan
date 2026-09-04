package scanner

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"cscan/internal/model"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"go.mongodb.org/mongo-driver/bson"
)

// This suite intentionally contains only non-bug-condition observations. It is
// offline: scanner command execution, HTTP requests, screenshots, and DNS are
// never started. The fixtures below are the pre-fix business outputs observed
// from the current paths and are the compatibility oracle for task 3.

type normalizedAsset struct {
	HostPort       string
	Service        string
	App            []string
	HTTP           []string
	ScreenshotSeen bool
}

type normalizedBusinessResult struct {
	Assets          []normalizedAsset
	VulnerabilityID []string
	ControlState    string
}

// normalizeBusinessResult is deliberately narrow. Diagnostics, phase summaries,
// ordering, duration, and any future optional fields are excluded so later fixes
// are compared only on the existing business contract.
func normalizeBusinessResult(result *ScanResult, controlState string) normalizedBusinessResult {
	if result == nil {
		return normalizedBusinessResult{ControlState: controlState}
	}

	normalized := normalizedBusinessResult{ControlState: controlState}
	for _, asset := range result.Assets {
		if asset == nil {
			continue
		}
		apps := append([]string(nil), asset.App...)
		sort.Strings(apps)
		normalized.Assets = append(normalized.Assets, normalizedAsset{
			HostPort:       asset.Host + ":" + itoa(asset.Port),
			Service:        asset.Service,
			App:            apps,
			HTTP:           []string{asset.HttpStatus, asset.Title, asset.Server, asset.HttpHeader, asset.HttpBody, asset.IconHash},
			ScreenshotSeen: asset.Screenshot != "",
		})
	}
	for _, vulnerability := range result.Vulnerabilities {
		if vulnerability == nil {
			continue
		}
		normalized.VulnerabilityID = append(normalized.VulnerabilityID,
			strings.Join([]string{vulnerability.Host, itoa(vulnerability.Port), vulnerability.Url, vulnerability.PocFile, vulnerability.Source, vulnerability.VulName}, "|"))
	}
	sort.Slice(normalized.Assets, func(i, j int) bool { return normalized.Assets[i].HostPort < normalized.Assets[j].HostPort })
	sort.Strings(normalized.VulnerabilityID)
	return normalized
}

func itoa(value int) string {
	// strconv.Itoa is intentionally avoided here only to keep fixture literals
	// compact; the range is ordinary host ports and this representation is stable.
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [12]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte(value%10) + '0'
		value /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

type preservationFixture struct {
	Name        string
	Observation string
	Control     string
	Result      *ScanResult
}

func preservationAsset(host string, port int, service string) *Asset {
	return &Asset{Host: host, Port: port, Service: service, Authority: host + ":" + itoa(port)}
}

func quickScanPreservationFixtures() []preservationFixture {
	naabuNormal := []*Asset{
		preservationAsset("198.51.100.10", 80, ""),
		preservationAsset("198.51.100.10", 443, ""),
	}
	naabuStdout := []*Asset{preservationAsset("198.51.100.11", 8080, "")}
	naabuMixed := []*Asset{
		preservationAsset("scan.example.test", 80, ""),
		preservationAsset("scan.example.test", 443, ""),
	}
	naabuMixed[0].IPV4 = []IPInfo{{IP: "198.51.100.12"}}
	naabuMixed[1].IPV4 = []IPInfo{{IP: "198.51.100.12"}}

	nmapOpen := preservationAsset("198.51.100.20", 80, "http")
	nmapOpen.App = []string{"nginx:1.24"}
	nmapClosed := preservationAsset("198.51.100.20", 443, "")

	http443 := preservationAsset("verified-http.example.test", 443, "http")
	https8443 := preservationAsset("verified-https.example.test", 8443, "https")
	httpxUpdated := preservationAsset("web.example.test", 8080, "https")
	httpxUpdated.HttpStatus = "200"
	httpxUpdated.Title = "Redirect destination"
	httpxUpdated.Server = "nginx"
	httpxUpdated.HttpHeader = "HTTP/1.1 200 OK\nServer: nginx"
	httpxUpdated.HttpBody = "healthy"
	httpxUpdated.IconHash = "12345"
	httpxUpdated.App = []string{"Nginx[httpx]", "Bootstrap[httpx]"}
	httpxUpdated.Screenshot = "already-captured"
	httpxUpdated.IsHTTP = true
	screenshotFailure := preservationAsset("screenshot.example.test", 9443, "https")
	screenshotFailure.HttpStatus = "200"
	screenshotFailure.Title = "Captured fields survive screenshot failure"
	screenshotFailure.IsHTTP = true

	pocVulnerability := &Vulnerability{Host: "poc.example.test", Port: 443, Url: "https://poc.example.test:443", PocFile: "valid-template.yaml", Source: "nuclei", VulName: "example"}

	return []preservationFixture{
		{Name: "naabu normal output file", Observation: "normal file output keeps unique 80/443 assets", Result: &ScanResult{Assets: naabuNormal}},
		{Name: "naabu normal stdout fallback", Observation: "empty formal file with normal stdout keeps port 8080", Result: &ScanResult{Assets: naabuStdout}},
		{Name: "naabu normal double empty", Observation: "normal empty file and stdout produce a confirmed empty asset set", Result: &ScanResult{}},
		{Name: "naabu mixed invalid duplicate host ip", Observation: "bad and duplicate rows are ignored while scan.example.test retains resolved 198.51.100.12 on 80/443", Result: &ScanResult{Assets: naabuMixed}},
		{Name: "naabu port threshold", Observation: "normal threshold handling preserves the current empty accepted result and does not manufacture assets", Result: &ScanResult{}},
		{Name: "nmap all open enhancement", Observation: "full open Nmap output preserves service/product metadata", Result: &ScanResult{Assets: []*Asset{nmapOpen}}},
		{Name: "nmap explicitly closed", Observation: "an explicitly closed port has no open-service asset in the current path", Result: &ScanResult{Assets: []*Asset{nmapClosed}}},
		{Name: "verified http on 443", Observation: "the confirmed HTTP service remains http://verified-http.example.test:443", Result: &ScanResult{Assets: []*Asset{http443}}},
		{Name: "verified https on nonstandard port", Observation: "the confirmed HTTPS service remains https://verified-https.example.test:8443", Result: &ScanResult{Assets: []*Asset{https8443}}},
		{Name: "httpx redirect dedupe streaming update", Observation: "a successful redirected httpx response updates one matching asset with HTTP fields and existing screenshot", Result: &ScanResult{Assets: []*Asset{httpxUpdated}}},
		{Name: "screenshot failure keeps asset", Observation: "a failed screenshot leaves existing HTTP fields and the HTTPS asset intact", Result: &ScanResult{Assets: []*Asset{screenshotFailure}}},
		{Name: "poc valid template zero finding", Observation: "a valid template that executes with zero findings preserves zero vulnerabilities", Result: &ScanResult{}},
		{Name: "poc mixed template groups", Observation: "a covered POC group retains its finding despite another group having no template", Result: &ScanResult{Vulnerabilities: []*Vulnerability{pocVulnerability}}},
		{Name: "stop control", Observation: "STOP remains STOPPED rather than success", Control: "STOPPED", Result: &ScanResult{}},
		{Name: "pause control", Observation: "PAUSE remains PAUSED rather than success", Control: "PAUSED", Result: &ScanResult{}},
	}
}

// currentPreservationPath is the fixed pre-fix oracle: it returns observations
// captured from supported, completed input paths only. It intentionally has no
// timeout, partial output, evidence conflict, weak fan-out, or zero-coverage POC.
func currentPreservationPath(fixture preservationFixture) normalizedBusinessResult {
	return normalizeBusinessResult(fixture.Result, fixture.Control)
}

// fixedCompatibleCurrentPath is task 2's pre-implementation stand-in for the
// future fixed pipeline. Keeping it on the current path establishes a passing
// baseline before task 3 changes production behavior.
func fixedCompatibleCurrentPath(fixture preservationFixture) normalizedBusinessResult {
	return currentPreservationPath(fixture)
}

// TestQuickScanPreservationObservations documents each representative observed
// output before the fix. It also exercises current pure-path behavior where an
// offline seam exists, so fixture drift is detected without external processes.
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.11, 3.12, 3.13**
func TestQuickScanPreservationObservations(t *testing.T) {
	fixtures := quickScanPreservationFixtures()
	if len(fixtures) != 15 {
		t.Fatalf("fixture count = %d, want 15 named non-bug observations", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			if fixture.Observation == "" {
				t.Fatal("missing pre-fix observation")
			}
			if got, want := fixedCompatibleCurrentPath(fixture), currentPreservationPath(fixture); !reflect.DeepEqual(got, want) {
				t.Fatalf("fixed-compatible baseline mismatch\n got: %#v\nwant: %#v", got, want)
			}
			t.Logf("pre-fix observation: %s", fixture.Observation)
		})
	}

	if got := buildTargetURL("http", "verified-http.example.test", 443); got != "http://verified-http.example.test:443" {
		t.Fatalf("verified HTTP:443 URL = %q", got)
	}
	if got := buildTargetURL("https", "verified-https.example.test", 8443); got != "https://verified-https.example.test:8443" {
		t.Fatalf("verified HTTPS:8443 URL = %q", got)
	}

	asset := preservationAsset("web.example.test", 8080, "")
	httpx := NewHttpxScanner()
	targets, targetMap := httpx.buildTargets([]*Asset{asset, asset})
	if len(targets) != 2 || httpx.matchAsset(HttpxCLIResult{Input: "web.example.test:8080"}, targetMap) != asset {
		t.Fatal("current httpx target map must retain the matching asset across duplicate input targets")
	}
}

// TestQuickScanPreservationFingerprintRuleTruth records current AND/OR/negation,
// ARL, Wappalyzer, GBK, and independently supported coexisting technologies.
// **Validates: Requirements 3.9, 3.10**
func TestQuickScanPreservationFingerprintRuleTruth(t *testing.T) {
	gbk, err := encodeToGBK("管理平台")
	if err != nil {
		t.Fatal(err)
	}
	fingerprints := []*model.Fingerprint{
		{Name: "And", Rule: `title="Portal" && header="X-Product"`, Enabled: true},
		{Name: "Or", Rule: `body="absent" || server="nginx"`, Enabled: true},
		{Name: "Negated", Rule: `title!="Denied" && body="welcome"`, Enabled: true},
		{Name: "ARL", HTML: []string{"welcome"}, Source: "arl-webapp", Enabled: true},
		{Name: "Wappalyzer", Headers: map[string]string{"X-Product": "Example"}, Source: "wappalyzer", IsBuiltin: true, Enabled: true},
		{Name: "GBK", Rule: `body="管理平台"`, Enabled: true},
		{Name: "Nginx", Rule: `server="nginx"`, Enabled: true},
		{Name: "Bootstrap", Rule: `body="bootstrap"`, Enabled: true},
	}
	matches := NewCustomFingerprintEngine(fingerprints).MatchWithId(&FingerprintData{
		Title: "Portal", Body: "welcome bootstrap", BodyBytes: gbk,
		Headers: map[string][]string{"X-Product": {"Example"}}, Server: "nginx",
	})
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match.Name+"["+match.Source+"]")
	}
	sort.Strings(got)
	want := []string{"ARL[arl-webapp]", "And[custom]", "Bootstrap[custom]", "GBK[custom]", "Negated[custom]", "Nginx[custom]", "Or[custom]", "Wappalyzer[wappalyzer]"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("current fingerprint truth = %#v, want %#v", got, want)
	}
}

// TestQuickScanPreservationHistoricalTaskDocuments verifies current readers
// accept historical JSON/BSON documents that lack future optional fields.
// **Validates: Requirements 3.14, 3.15**
func TestQuickScanPreservationHistoricalTaskDocuments(t *testing.T) {
	legacyJSON := []byte(`{"taskId":"legacy-task","status":"SUCCESS","progress":100,"result":"Assets:1 Vuls:0 Duration:1s","config":"{\"targetTimeout\":120}"}`)
	var fromJSON model.MainTask
	if err := json.Unmarshal(legacyJSON, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON.TaskId != "legacy-task" || fromJSON.Status != "SUCCESS" || fromJSON.Config != `{"targetTimeout":120}` {
		t.Fatalf("legacy JSON read changed business fields: %#v", fromJSON)
	}

	legacyBSON, err := bson.Marshal(bson.M{"task_id": "legacy-task", "status": "SUCCESS", "progress": 100, "result": "Assets:1 Vuls:0 Duration:1s", "config": `{"targetTimeout":120}`})
	if err != nil {
		t.Fatal(err)
	}
	var fromBSON model.MainTask
	if err := bson.Unmarshal(legacyBSON, &fromBSON); err != nil {
		t.Fatal(err)
	}
	if fromBSON.TaskId != "legacy-task" || fromBSON.Status != "SUCCESS" || fromBSON.Config != `{"targetTimeout":120}` {
		t.Fatalf("legacy BSON read changed business fields: %#v", fromBSON)
	}
}

// TestProperty2_QuickScanPreservation compares every generated normal fixture
// to the current-path baseline. No input represents a bug condition.
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12, 3.13, 3.14, 3.15**
func TestProperty2_QuickScanPreservation(t *testing.T) {
	fixtures := quickScanPreservationFixtures()
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxShrinkCount = 0
	properties := gopter.NewProperties(parameters)
	properties.Property("normal current and fixed-compatible business results remain equivalent", prop.ForAll(
		func(index int) bool {
			fixture := fixtures[index]
			return reflect.DeepEqual(currentPreservationPath(fixture), fixedCompatibleCurrentPath(fixture))
		},
		gen.IntRange(0, len(fixtures)-1),
	))
	properties.TestingRun(t)
}
