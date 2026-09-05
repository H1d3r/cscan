package worker

import (
	"fmt"
	"strings"

	"cscan/internal/scanner"
)

// portIdentifyMergeResult separates the inventory result from assets that may
// continue into fingerprint and POC scans. TIMEOUT ports remain in Assets for
// persistence and task accounting, but are omitted from EligibleAssets.
type portIdentifyMergeResult struct {
	Assets            []*scanner.Asset
	EligibleAssets    []*scanner.Asset
	TimedOutHostPorts map[string]struct{}
	Phase             PhaseResult
}

// mergeNmapPortIdentifyResults overlays Nmap evidence onto a cloned, stable
// deduplicated discovery baseline. Only an explicit CLOSED result may remove a
// discovered asset, and only when excludeClosed is configured.
func mergeNmapPortIdentifyResults(discovered []*scanner.Asset, results []scanner.PortIdentifyResult, excludeClosed bool) portIdentifyMergeResult {
	assetByKey := make(map[string]*scanner.Asset, len(discovered))
	orderedKeys := make([]string, 0, len(discovered))
	inputKeys := make([]string, 0, len(discovered))

	for _, asset := range discovered {
		if asset == nil {
			continue
		}
		key := normalizedHostPort(asset.Host, asset.Port)
		if existing, ok := assetByKey[key]; ok {
			mergeDuplicateDiscoveredAsset(existing, asset)
			continue
		}
		assetByKey[key] = cloneAsset(asset)
		orderedKeys = append(orderedKeys, key)
		if asset.Port > 0 {
			inputKeys = append(inputKeys, key)
		}
	}

	coverage := scanner.Coverage{Input: len(inputKeys)}
	diagnostics := make([]scanner.TargetDiagnostic, 0)
	warningCodes := make([]string, 0)
	seenWarnings := make(map[string]struct{})
	seenResults := make(map[string]struct{}, len(results))
	removed := make(map[string]struct{})
	timedOutHostPorts := make(map[string]struct{})
	canceled := false

	addWarning := func(code string) {
		if code == "" {
			return
		}
		if _, ok := seenWarnings[code]; ok {
			return
		}
		seenWarnings[code] = struct{}{}
		warningCodes = append(warningCodes, code)
	}
	addUnconfirmed := func(result scanner.PortIdentifyResult, reason string) {
		addWarning(reason)
		if len(diagnostics) >= scanner.MaxTargetDiagnostics {
			return
		}
		diagnostics = append(diagnostics, scanner.TargetDiagnostic{
			Target:     fmt.Sprintf("%s:%d", result.Host, result.Port),
			Host:       result.Host,
			Port:       result.Port,
			Outcome:    string(result.Outcome),
			ReasonCode: reason,
		})
	}

	for _, result := range results {
		key := normalizedHostPort(result.Host, result.Port)
		asset, exists := assetByKey[key]
		if !exists || result.Port <= 0 {
			continue
		}
		if _, duplicate := seenResults[key]; duplicate {
			continue
		}
		seenResults[key] = struct{}{}
		coverage.Attempted++

		switch result.Outcome {
		case scanner.PortOpen:
			coverage.Succeeded++
			overlayOpenNmapResult(asset, result)
		case scanner.PortClosed:
			coverage.Succeeded++
			if excludeClosed {
				removed[key] = struct{}{}
			}
		case scanner.PortTimeout:
			coverage.TimedOut++
			timedOutHostPorts[key] = struct{}{}
			addUnconfirmed(result, scanner.ReasonTimeout)
		case scanner.PortExecError:
			coverage.Failed++
			addUnconfirmed(result, scanner.ReasonExecutionError)
		case scanner.PortParseError:
			coverage.Failed++
			addUnconfirmed(result, scanner.ReasonParseError)
		case scanner.PortCanceled:
			coverage.Unconfirmed++
			canceled = true
			addUnconfirmed(result, scanner.ReasonCanceled)
		case scanner.PortFiltered, scanner.PortNoRecord:
			coverage.Unconfirmed++
			addUnconfirmed(result, scanner.ReasonUnconfirmed)
		default:
			coverage.Unconfirmed++
			addUnconfirmed(result, scanner.ReasonUnconfirmed)
		}
	}

	for _, key := range inputKeys {
		if _, ok := seenResults[key]; ok {
			continue
		}
		asset := assetByKey[key]
		coverage.Unconfirmed++
		addUnconfirmed(scanner.PortIdentifyResult{
			Host:      asset.Host,
			Port:      asset.Port,
			Outcome:   scanner.PortNoRecord,
			ErrorCode: scanner.NmapReasonNoHostRecord,
		}, scanner.ReasonUnconfirmed)
	}

	phase := NewPhaseResult("portidentify", coverage, canceled, warningCodes...)
	phase.Diagnostic = &scanner.ScanDiagnostic{
		Phase:        phase.Phase,
		Status:       phase.Status,
		Coverage:     coverage,
		Targets:      diagnostics,
		WarningCodes: append([]string(nil), phase.ReasonCodes...),
	}

	merged := make([]*scanner.Asset, 0, len(orderedKeys))
	eligible := make([]*scanner.Asset, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		if _, excluded := removed[key]; excluded {
			continue
		}
		asset := assetByKey[key]
		merged = append(merged, asset)
		if _, timedOut := timedOutHostPorts[key]; !timedOut {
			eligible = append(eligible, asset)
		}
	}
	return portIdentifyMergeResult{
		Assets:            merged,
		EligibleAssets:    eligible,
		TimedOutHostPorts: timedOutHostPorts,
		Phase:             phase,
	}
}

func normalizedHostPort(host string, port int) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	host = strings.TrimSuffix(host, ".")
	return fmt.Sprintf("%s:%d", host, port)
}

func cloneAsset(asset *scanner.Asset) *scanner.Asset {
	if asset == nil {
		return nil
	}
	cloned := *asset
	cloned.App = append([]string(nil), asset.App...)
	cloned.FingerprintFindings = append(scanner.FingerprintFindings(nil), asset.FingerprintFindings...)
	cloned.IconData = append([]byte(nil), asset.IconData...)
	cloned.IPV4 = append([]scanner.IPInfo(nil), asset.IPV4...)
	cloned.IPV6 = append([]scanner.IPInfo(nil), asset.IPV6...)
	return &cloned
}

func mergeDuplicateDiscoveredAsset(base, duplicate *scanner.Asset) {
	if base == nil || duplicate == nil {
		return
	}
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
	base.FingerprintFindingsCollected = base.FingerprintFindingsCollected || duplicate.FingerprintFindingsCollected
	base.TakeoverRisk = base.TakeoverRisk || duplicate.TakeoverRisk
	base.App = appendUniqueStrings(base.App, duplicate.App...)
	if len(duplicate.FingerprintFindings) > 0 {
		combined := append(scanner.FingerprintFindings(nil), base.FingerprintFindings...)
		combined = append(combined, duplicate.FingerprintFindings...)
		base.FingerprintFindings = scanner.GovernFingerprintFindings(combined)
	}
	base.IPV4 = appendUniqueIPInfo(base.IPV4, duplicate.IPV4...)
	base.IPV6 = appendUniqueIPInfo(base.IPV6, duplicate.IPV6...)
}

func overlayOpenNmapResult(asset *scanner.Asset, result scanner.PortIdentifyResult) {
	if result.Service != "" {
		asset.Service = result.Service
	}
	if result.Product != "" {
		product := result.Product
		if result.Version != "" {
			product += ":" + result.Version
		}
		asset.App = appendUniqueStrings(asset.App, product)
	}
	if result.ResolvedIP != "" {
		ipInfo := scanner.IPInfo{IP: result.ResolvedIP}
		if strings.Contains(result.ResolvedIP, ":") {
			asset.IPV6 = appendUniqueIPInfo(asset.IPV6, ipInfo)
		} else {
			asset.IPV4 = appendUniqueIPInfo(asset.IPV4, ipInfo)
		}
	}
	asset.IsHTTP = asset.IsHTTP || scanner.IsHTTPService(asset.Service, asset.Port)
}

func appendUniqueStrings(existing []string, values ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(values))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		existing = append(existing, value)
	}
	return existing
}

func appendUniqueIPInfo(existing []scanner.IPInfo, values ...scanner.IPInfo) []scanner.IPInfo {
	seen := make(map[string]struct{}, len(existing)+len(values))
	for _, value := range existing {
		seen[value.IP] = struct{}{}
	}
	for _, value := range values {
		if value.IP == "" {
			continue
		}
		if _, ok := seen[value.IP]; ok {
			continue
		}
		seen[value.IP] = struct{}{}
		existing = append(existing, value)
	}
	return existing
}

func portIdentifyResultsForAssets(assets []*scanner.Asset, outcome scanner.PortIdentifyOutcome, errorCode string) []scanner.PortIdentifyResult {
	results := make([]scanner.PortIdentifyResult, 0, len(assets))
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		if asset == nil || asset.Port <= 0 {
			continue
		}
		key := normalizedHostPort(asset.Host, asset.Port)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, scanner.PortIdentifyResult{
			Host:      asset.Host,
			Port:      asset.Port,
			Outcome:   outcome,
			ErrorCode: errorCode,
		})
	}
	return results
}

// excludeAssetsByHostPort returns the downstream scan candidates while keeping
// the caller's inventory slice untouched.
func excludeAssetsByHostPort(assets []*scanner.Asset, excluded map[string]struct{}) []*scanner.Asset {
	if len(excluded) == 0 {
		return assets
	}
	eligible := make([]*scanner.Asset, 0, len(assets))
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		if _, skip := excluded[normalizedHostPort(asset.Host, asset.Port)]; skip {
			continue
		}
		eligible = append(eligible, asset)
	}
	return eligible
}
