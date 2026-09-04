package worker

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"cscan/internal/scanner"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

type nmapMergePropertyScenario struct {
	discovered []*scanner.Asset
	results    []scanner.PortIdentifyResult
}

var nmapMergePropertyOutcomes = []scanner.PortIdentifyOutcome{
	scanner.PortOpen,
	scanner.PortOpen,
	scanner.PortClosed,
	scanner.PortFiltered,
	scanner.PortTimeout,
	scanner.PortExecError,
	scanner.PortParseError,
	scanner.PortCanceled,
	scanner.PortNoRecord,
}

func nmapMergePropertyAsset(host string, port, marker int) *scanner.Asset {
	return &scanner.Asset{
		Authority:       fmt.Sprintf("%s:%d", host, port),
		Host:            host,
		Port:            port,
		Category:        "domain",
		Service:         fmt.Sprintf("discovered-service-%d", marker),
		Server:          fmt.Sprintf("server-%d", marker),
		Banner:          fmt.Sprintf("banner-%d", marker),
		Title:           fmt.Sprintf("title-%d", marker),
		App:             []string{fmt.Sprintf("discovered-app-%d", marker)},
		HttpStatus:      fmt.Sprintf("2%02d", marker%100),
		HttpHeader:      fmt.Sprintf("X-Marker: %d", marker),
		HttpBody:        fmt.Sprintf("body-%d", marker),
		Cert:            fmt.Sprintf("cert-%d", marker),
		IconHash:        fmt.Sprintf("icon-%d", marker),
		IconData:        []byte{byte(marker), byte(marker >> 8)},
		Screenshot:      fmt.Sprintf("shot-%d.png", marker),
		IsCDN:           true,
		CName:           fmt.Sprintf("cname-%d.example.test", marker),
		IsCloud:         true,
		IsHTTP:          true,
		IPV4:            []scanner.IPInfo{{IP: fmt.Sprintf("192.0.2.%d", marker%250+1), Location: "discovered"}},
		IPV6:            []scanner.IPInfo{{IP: fmt.Sprintf("2001:db8::%x", marker+1), Location: "discovered"}},
		Source:          fmt.Sprintf("source-%d", marker),
		Path:            fmt.Sprintf("/path-%d", marker),
		ContentLength:   int64(marker + 1),
		ContentType:     "text/html",
		ContentWords:    int64(marker + 2),
		ContentLines:    int64(marker + 3),
		Duration:        int64(marker + 4),
		RequestRaw:      fmt.Sprintf("request-%d", marker),
		ResponseRaw:     fmt.Sprintf("response-%d", marker),
		TakeoverRisk:    true,
		TakeoverService: fmt.Sprintf("takeover-service-%d", marker),
		TakeoverCName:   fmt.Sprintf("takeover-%d.example.test", marker),
	}
}

func nmapMergePropertyResult(asset *scanner.Asset, outcome scanner.PortIdentifyOutcome, enriched bool) scanner.PortIdentifyResult {
	result := scanner.PortIdentifyResult{Host: asset.Host, Port: asset.Port, Outcome: outcome}
	if outcome == scanner.PortOpen && enriched {
		result.Service = "nmap-service"
		result.Product = "nmap-product"
		result.Version = "1.0"
		result.ResolvedIP = "198.51.100.254"
	}
	return result
}

func nmapMergePropertyScenarios(codes []int) []nmapMergePropertyScenario {
	duplicateFirst := &scanner.Asset{
		Authority: "Alpha.Example.Test.:443",
		Host:      "Alpha.Example.Test.",
		Port:      443,
		Source:    "first-discovery",
	}
	duplicateAgain := nmapMergePropertyAsset(" alpha.example.test ", 443, 1)
	duplicate := nmapMergePropertyScenario{
		discovered: []*scanner.Asset{duplicateFirst, duplicateAgain},
		results: []scanner.PortIdentifyResult{{
			Host: "ALPHA.EXAMPLE.TEST", Port: 443, Outcome: scanner.PortOpen,
		}},
	}

	multiFirst := nmapMergePropertyAsset("first.example.test", 8443, 2)
	multiSecond := nmapMergePropertyAsset("second.example.test", 8443, 3)
	multiHostSamePort := nmapMergePropertyScenario{
		discovered: []*scanner.Asset{multiFirst, multiSecond},
		results: []scanner.PortIdentifyResult{
			nmapMergePropertyResult(multiFirst, scanner.PortOpen, true),
			nmapMergePropertyResult(multiSecond, scanner.PortTimeout, false),
		},
	}

	mixed := nmapMergePropertyScenario{}
	for i, outcome := range nmapMergePropertyOutcomes {
		asset := nmapMergePropertyAsset(fmt.Sprintf("mixed-%d.example.test", i), 8000+i, 10+i)
		mixed.discovered = append(mixed.discovered, asset)
		mixed.results = append(mixed.results, nmapMergePropertyResult(asset, outcome, i == 1))
	}

	hostVariants := [][]string{
		{"Generated-A.Example.Test.", " generated-a.example.test ", "GENERATED-A.EXAMPLE.TEST"},
		{"Generated-B.Example.Test.", " generated-b.example.test ", "GENERATED-B.EXAMPLE.TEST"},
		{"Generated-C.Example.Test.", " generated-c.example.test ", "GENERATED-C.EXAMPLE.TEST"},
	}
	ports := []int{80, 443, 8080}
	generated := nmapMergePropertyScenario{}
	firstCodeByKey := make(map[string]int)
	for i, code := range codes {
		hostIndex := code % len(hostVariants)
		port := ports[(code/len(hostVariants))%len(ports)]
		marker := hostIndex*10000 + port
		asset := nmapMergePropertyAsset(hostVariants[hostIndex][i%len(hostVariants[hostIndex])], port, marker)
		generated.discovered = append(generated.discovered, asset)
		key := normalizedHostPort(asset.Host, asset.Port)
		if _, exists := firstCodeByKey[key]; !exists {
			firstCodeByKey[key] = code
		}
	}
	seen := make(map[string]struct{})
	for _, asset := range generated.discovered {
		key := normalizedHostPort(asset.Host, asset.Port)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		code := firstCodeByKey[key]
		outcomeIndex := code % len(nmapMergePropertyOutcomes)
		generated.results = append(generated.results, nmapMergePropertyResult(asset, nmapMergePropertyOutcomes[outcomeIndex], outcomeIndex == 1))
	}

	return []nmapMergePropertyScenario{
		{},
		duplicate,
		multiHostSamePort,
		mixed,
		generated,
	}
}

func cloneNmapMergePropertyAssets(assets []*scanner.Asset) []*scanner.Asset {
	if assets == nil {
		return nil
	}
	cloned := make([]*scanner.Asset, len(assets))
	for i, asset := range assets {
		cloned[i] = cloneAsset(asset)
	}
	return cloned
}

// mergeNmapMergePropertyDuplicate models the specified discovery baseline:
// the first normalized host-port owns identity and order, while later
// discoveries may fill empty fields and contribute unique collection values.
func mergeNmapMergePropertyDuplicate(base, duplicate *scanner.Asset) {
	if base.Authority == "" {
		base.Authority = duplicate.Authority
	}
	if base.Category == "" {
		base.Category = duplicate.Category
	}
	if base.Service == "" {
		base.Service = duplicate.Service
	}
	if base.Server == "" {
		base.Server = duplicate.Server
	}
	if base.Banner == "" {
		base.Banner = duplicate.Banner
	}
	if base.Title == "" {
		base.Title = duplicate.Title
	}
	if base.HttpStatus == "" {
		base.HttpStatus = duplicate.HttpStatus
	}
	if base.HttpHeader == "" {
		base.HttpHeader = duplicate.HttpHeader
	}
	if base.HttpBody == "" {
		base.HttpBody = duplicate.HttpBody
	}
	if base.Cert == "" {
		base.Cert = duplicate.Cert
	}
	if base.IconHash == "" {
		base.IconHash = duplicate.IconHash
	}
	if len(base.IconData) == 0 {
		base.IconData = append([]byte(nil), duplicate.IconData...)
	}
	if base.Screenshot == "" {
		base.Screenshot = duplicate.Screenshot
	}
	if base.CName == "" {
		base.CName = duplicate.CName
	}
	if base.Source == "" {
		base.Source = duplicate.Source
	}
	if base.Path == "" {
		base.Path = duplicate.Path
	}
	if base.ContentLength == 0 {
		base.ContentLength = duplicate.ContentLength
	}
	if base.ContentType == "" {
		base.ContentType = duplicate.ContentType
	}
	if base.ContentWords == 0 {
		base.ContentWords = duplicate.ContentWords
	}
	if base.ContentLines == 0 {
		base.ContentLines = duplicate.ContentLines
	}
	if base.Duration == 0 {
		base.Duration = duplicate.Duration
	}
	if base.RequestRaw == "" {
		base.RequestRaw = duplicate.RequestRaw
	}
	if base.ResponseRaw == "" {
		base.ResponseRaw = duplicate.ResponseRaw
	}
	if base.TakeoverService == "" {
		base.TakeoverService = duplicate.TakeoverService
	}
	if base.TakeoverCName == "" {
		base.TakeoverCName = duplicate.TakeoverCName
	}
	base.IsCDN = base.IsCDN || duplicate.IsCDN
	base.IsCloud = base.IsCloud || duplicate.IsCloud
	base.IsHTTP = base.IsHTTP || duplicate.IsHTTP
	base.TakeoverRisk = base.TakeoverRisk || duplicate.TakeoverRisk
	for _, app := range duplicate.App {
		if !nmapMergePropertyContainsString(base.App, app) {
			base.App = append(base.App, app)
		}
	}
	for _, ip := range duplicate.IPV4 {
		if !nmapMergePropertyContainsIPAddress(base.IPV4, ip.IP) {
			base.IPV4 = append(base.IPV4, ip)
		}
	}
	for _, ip := range duplicate.IPV6 {
		if !nmapMergePropertyContainsIPAddress(base.IPV6, ip.IP) {
			base.IPV6 = append(base.IPV6, ip)
		}
	}
}

func nmapMergePropertyContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func nmapMergePropertyContainsIP(values []scanner.IPInfo, wanted scanner.IPInfo) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, wanted) {
			return true
		}
	}
	return false
}

func nmapMergePropertyContainsIPAddress(values []scanner.IPInfo, wanted string) bool {
	for _, value := range values {
		if value.IP == wanted {
			return true
		}
	}
	return false
}

func nmapMergePropertyPreservesFields(got, base *scanner.Asset, result scanner.PortIdentifyResult, hasResult bool) bool {
	gotProjection := cloneAsset(got)
	baseProjection := cloneAsset(base)
	gotProjection.Service, baseProjection.Service = "", ""
	gotProjection.App, baseProjection.App = nil, nil
	gotProjection.IPV4, baseProjection.IPV4 = nil, nil
	gotProjection.IPV6, baseProjection.IPV6 = nil, nil
	gotProjection.IsHTTP, baseProjection.IsHTTP = false, false
	if !reflect.DeepEqual(gotProjection, baseProjection) {
		return false
	}

	for _, app := range base.App {
		if !nmapMergePropertyContainsString(got.App, app) {
			return false
		}
	}
	for _, ip := range base.IPV4 {
		if !nmapMergePropertyContainsIP(got.IPV4, ip) {
			return false
		}
	}
	for _, ip := range base.IPV6 {
		if !nmapMergePropertyContainsIP(got.IPV6, ip) {
			return false
		}
	}
	if base.IsHTTP && !got.IsHTTP {
		return false
	}

	if !hasResult || result.Outcome != scanner.PortOpen {
		return reflect.DeepEqual(got, base)
	}
	if result.Service == "" && result.Product == "" && result.Version == "" && result.ResolvedIP == "" {
		return reflect.DeepEqual(got, base)
	}
	if result.Service != "" && got.Service != result.Service {
		return false
	}
	if result.Service == "" && got.Service != base.Service {
		return false
	}
	if result.Product != "" {
		product := result.Product
		if result.Version != "" {
			product += ":" + result.Version
		}
		if !nmapMergePropertyContainsString(got.App, product) {
			return false
		}
	}
	return result.ResolvedIP == "" || nmapMergePropertyContainsIP(got.IPV4, scanner.IPInfo{IP: result.ResolvedIP})
}

func checkNmapMergePropertyScenario(scenario nmapMergePropertyScenario, excludeClosed bool) bool {
	before := cloneNmapMergePropertyAssets(scenario.discovered)
	expectedByKey := make(map[string]*scanner.Asset)
	orderedKeys := make([]string, 0, len(scenario.discovered))
	for _, asset := range scenario.discovered {
		if asset == nil {
			continue
		}
		key := normalizedHostPort(asset.Host, asset.Port)
		if existing, exists := expectedByKey[key]; exists {
			mergeNmapMergePropertyDuplicate(existing, asset)
			continue
		}
		expectedByKey[key] = cloneAsset(asset)
		orderedKeys = append(orderedKeys, key)
	}

	resultByKey := make(map[string]scanner.PortIdentifyResult)
	for _, result := range scenario.results {
		key := normalizedHostPort(result.Host, result.Port)
		if _, discovered := expectedByKey[key]; !discovered || result.Port <= 0 {
			continue
		}
		if _, exists := resultByKey[key]; !exists {
			resultByKey[key] = result
		}
	}

	expectedKeys := make([]string, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		result, hasResult := resultByKey[key]
		if excludeClosed && hasResult && result.Outcome == scanner.PortClosed {
			continue
		}
		expectedKeys = append(expectedKeys, key)
	}

	merged := mergeNmapPortIdentifyResults(scenario.discovered, scenario.results, excludeClosed)
	if !reflect.DeepEqual(scenario.discovered, before) || len(merged.Assets) != len(expectedKeys) {
		return false
	}
	seen := make(map[string]struct{}, len(merged.Assets))
	for i, asset := range merged.Assets {
		if asset == nil {
			return false
		}
		key := normalizedHostPort(asset.Host, asset.Port)
		if key != expectedKeys[i] {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
		result, hasResult := resultByKey[key]
		if !nmapMergePropertyPreservesFields(asset, expectedByKey[key], result, hasResult) {
			return false
		}
	}
	return true
}

func runNmapMergePreservationProperty(t *testing.T, seed int64) {
	t.Helper()
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 250
	parameters.MaxSize = 32
	parameters.MaxShrinkCount = 100
	parameters.Rng = rand.New(rand.NewSource(seed))
	properties := gopter.NewProperties(parameters)

	properties.Property("Property 4: Nmap merge is stable, unique, lossless except configured CLOSED, and empty OPEN enrichment is non-destructive", prop.ForAll(
		func(codes []int) bool {
			for _, scenario := range nmapMergePropertyScenarios(codes) {
				if !checkNmapMergePropertyScenario(scenario, false) || !checkNmapMergePropertyScenario(scenario, true) {
					return false
				}
			}
			return true
		},
		gen.SliceOf(gen.IntRange(0, 511)),
	))
	t.Logf("gopter seed=%d", seed)
	properties.TestingRun(t)
}

// TestProperty4_NmapMergePreservationFixedSeed exercises empty inputs,
// normalized duplicate keys, hosts sharing one port, and mixed outcomes on
// every generated case, with a reproducible seed.
// **Validates: Requirements 2.3, 3.5, 3.6**
func TestProperty4_NmapMergePreservationFixedSeed(t *testing.T) {
	runNmapMergePreservationProperty(t, 2026041004)
}

// TestProperty4_NmapMergePreservationRandomSeed broadens the offline generated
// asset/outcome space and logs the seed so failures can be reproduced.
// **Validates: Requirements 2.3, 3.5, 3.6**
func TestProperty4_NmapMergePreservationRandomSeed(t *testing.T) {
	runNmapMergePreservationProperty(t, time.Now().UnixNano())
}
