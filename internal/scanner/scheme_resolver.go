package scanner

import (
	"strings"
	"time"
)

const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

const (
	SchemeEvidenceSuccessfulResponse = "successful_response"
	SchemeEvidenceTLSHandshake       = "tls_handshake"
	SchemeEvidenceExplicitInput      = "explicit_input"
	SchemeEvidenceScannerService     = "scanner_service"
	SchemeEvidencePortHint           = "port_hint"
)

// SchemeEvidence is one observation that may contribute to selecting HTTP or
// HTTPS. Success is required for response and TLS evidence; it is intentionally
// ignored for declarative evidence such as explicit input, service metadata,
// and port hints.
type SchemeEvidence struct {
	Scheme     string
	Kind       string
	Success    bool
	StatusCode int
	ErrorClass string
	ObservedAt time.Time
}

// SchemeResolution is the deterministic result of evaluating scheme evidence.
// AlternateScheme identifies the other evidenced protocol when a conflict is
// present; it does not imply that the alternate protocol was verified.
type SchemeResolution struct {
	Scheme           string
	AlternateScheme  string
	SelectedEvidence SchemeEvidence
	HasEvidence      bool
	Conflict         bool
}

type rankedSchemeEvidence struct {
	evidence SchemeEvidence
	rank     int
	quality  int
	index    int
}

// ResolveScheme selects a protocol using the following descending priority:
// successful HTTP response, successful TLS handshake, explicit input, scanner
// service metadata, and port hint. Invalid schemes, unknown evidence kinds,
// and failed response/TLS observations do not participate in selection.
//
// When both protocols have equally ranked successful HTTP responses, an
// explicit input breaks the tie. Otherwise response quality and then original
// evidence order are used. Any usable evidence for the opposite protocol is
// retained as an alternate and reported as a conflict, while never overriding
// higher-ranked verified evidence.
func ResolveScheme(evidence []SchemeEvidence) SchemeResolution {
	candidates := make([]rankedSchemeEvidence, 0, len(evidence))
	for index, item := range evidence {
		item.Scheme = normalizeScheme(item.Scheme)
		if item.Scheme == "" {
			continue
		}

		rank, usable := schemeEvidenceRank(item)
		if !usable {
			continue
		}
		candidates = append(candidates, rankedSchemeEvidence{
			evidence: item,
			rank:     rank,
			quality:  schemeEvidenceQuality(item),
			index:    index,
		})
	}
	if len(candidates) == 0 {
		return SchemeResolution{}
	}

	bestRank := candidates[0].rank
	for _, candidate := range candidates[1:] {
		if candidate.rank < bestRank {
			bestRank = candidate.rank
		}
	}

	bestByScheme := make(map[string]rankedSchemeEvidence, 2)
	for _, candidate := range candidates {
		if candidate.rank != bestRank {
			continue
		}
		current, exists := bestByScheme[candidate.evidence.Scheme]
		if !exists || betterSchemeEvidence(candidate, current) {
			bestByScheme[candidate.evidence.Scheme] = candidate
		}
	}

	selected := selectBestScheme(bestByScheme, candidates, bestRank)
	resolution := SchemeResolution{
		Scheme:           selected.evidence.Scheme,
		SelectedEvidence: selected.evidence,
		HasEvidence:      true,
	}

	alternate := oppositeScheme(resolution.Scheme)
	for _, candidate := range candidates {
		if candidate.evidence.Scheme == alternate {
			resolution.AlternateScheme = alternate
			resolution.Conflict = true
			break
		}
	}
	return resolution
}

func schemeEvidenceRank(evidence SchemeEvidence) (int, bool) {
	switch evidence.Kind {
	case SchemeEvidenceSuccessfulResponse:
		return 0, evidence.Success
	case SchemeEvidenceTLSHandshake:
		return 1, evidence.Success
	case SchemeEvidenceExplicitInput:
		return 2, true
	case SchemeEvidenceScannerService:
		return 3, true
	case SchemeEvidencePortHint:
		return 4, true
	default:
		return 0, false
	}
}

func schemeEvidenceQuality(evidence SchemeEvidence) int {
	if evidence.Kind != SchemeEvidenceSuccessfulResponse {
		return 0
	}

	quality := 0
	if evidence.StatusCode >= 100 && evidence.StatusCode <= 599 {
		quality += 2
	}
	if evidence.ErrorClass == "" {
		quality++
	}
	return quality
}

func betterSchemeEvidence(candidate, current rankedSchemeEvidence) bool {
	if candidate.quality != current.quality {
		return candidate.quality > current.quality
	}
	return candidate.index < current.index
}

func selectBestScheme(bestByScheme map[string]rankedSchemeEvidence, all []rankedSchemeEvidence, rank int) rankedSchemeEvidence {
	httpEvidence, hasHTTP := bestByScheme[SchemeHTTP]
	httpsEvidence, hasHTTPS := bestByScheme[SchemeHTTPS]
	if !hasHTTP {
		return httpsEvidence
	}
	if !hasHTTPS {
		return httpEvidence
	}

	if rank == 0 {
		if explicit := unambiguousExplicitScheme(all); explicit != "" {
			if explicit == SchemeHTTP {
				return httpEvidence
			}
			return httpsEvidence
		}
	}
	if betterSchemeEvidence(httpsEvidence, httpEvidence) {
		return httpsEvidence
	}
	return httpEvidence
}

func unambiguousExplicitScheme(evidence []rankedSchemeEvidence) string {
	hasHTTP := false
	hasHTTPS := false
	for _, candidate := range evidence {
		if candidate.evidence.Kind != SchemeEvidenceExplicitInput {
			continue
		}
		switch candidate.evidence.Scheme {
		case SchemeHTTP:
			hasHTTP = true
		case SchemeHTTPS:
			hasHTTPS = true
		}
	}
	if hasHTTP == hasHTTPS {
		return ""
	}
	if hasHTTP {
		return SchemeHTTP
	}
	return SchemeHTTPS
}

func normalizeScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case SchemeHTTP:
		return SchemeHTTP
	case SchemeHTTPS:
		return SchemeHTTPS
	default:
		return ""
	}
}

func oppositeScheme(scheme string) string {
	if scheme == SchemeHTTP {
		return SchemeHTTPS
	}
	return SchemeHTTP
}

// assetSchemeEvidence translates the protocol facts already persisted on an
// asset into the common resolver model. A completed HTTP response is stronger
// than scanner service metadata, and both are stronger than a port hint.
func assetSchemeEvidence(asset *Asset) []SchemeEvidence {
	if asset == nil {
		return nil
	}

	evidence := make([]SchemeEvidence, 0, 3)
	serviceScheme := normalizeScheme(asset.Service)
	if asset.IsHTTP && asset.HttpStatus != "" && serviceScheme != "" {
		evidence = append(evidence, SchemeEvidence{
			Scheme: serviceScheme, Kind: SchemeEvidenceSuccessfulResponse, Success: true,
		})
	} else if serviceScheme != "" {
		evidence = append(evidence, SchemeEvidence{Scheme: serviceScheme, Kind: SchemeEvidenceScannerService})
	}

	evidence = append(evidence, SchemeEvidence{Scheme: schemePortHint(asset.Port), Kind: SchemeEvidencePortHint})
	return evidence
}

func schemePortHint(port int) string {
	switch port {
	case 443, 8443, 9443:
		return SchemeHTTPS
	default:
		return SchemeHTTP
	}
}

// resolveAssetScheme is the single asset-level protocol decision consumed by
// HTTP probing, screenshots, certificate selection, and persistence.
func resolveAssetScheme(asset *Asset) SchemeResolution {
	return ResolveScheme(assetSchemeEvidence(asset))
}

// persistSuccessfulScheme records verified response evidence without allowing
// lower-ranked service or port guesses to overwrite it.
func persistSuccessfulScheme(asset *Asset, scheme string, statusCode int) SchemeResolution {
	if asset == nil {
		return SchemeResolution{}
	}
	evidence := assetSchemeEvidence(asset)
	evidence = append(evidence, SchemeEvidence{
		Scheme: normalizeScheme(scheme), Kind: SchemeEvidenceSuccessfulResponse,
		Success: true, StatusCode: statusCode, ObservedAt: time.Now(),
	})
	resolution := ResolveScheme(evidence)
	if resolution.HasEvidence && resolution.SelectedEvidence.Kind == SchemeEvidenceSuccessfulResponse {
		markFingerprintFindingsCollected(asset)
		asset.Service = resolution.Scheme
		asset.IsHTTP = true
		asset.ProtocolProbeStatus = ProtocolProbeConfirmed
	}
	return resolution
}
