package scanner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"cscan/pkg/geolocation"

	"github.com/zeromicro/go-zero/core/logx"
)

// HttpxScanner httpx 扫描器 (CLI 模式)
type HttpxScanner struct {
	BaseScanner
	executor *CmdExecutor
	execute  func(context.Context, []string, ExecuteOpts) (*ExecuteResult, error)
}

// NewHttpxScanner 创建 httpx 扫描器
func NewHttpxScanner() *HttpxScanner {
	return &HttpxScanner{
		BaseScanner: BaseScanner{name: "httpx"},
		executor:    NewExecutorForTool("httpx"),
	}
}

// HttpxAttemptOutcome is the explicit result of one scheme-specific httpx run.
type HttpxAttemptOutcome string

const (
	HttpxOutcomeSuccess    HttpxAttemptOutcome = "SUCCESS"
	HttpxOutcomeTimeout    HttpxAttemptOutcome = "TIMEOUT"
	HttpxOutcomeExecError  HttpxAttemptOutcome = "EXEC_ERROR"
	HttpxOutcomeNoOutput   HttpxAttemptOutcome = "NO_OUTPUT"
	HttpxOutcomeParseError HttpxAttemptOutcome = "PARSE_ERROR"
	HttpxOutcomeNoMatch    HttpxAttemptOutcome = "NO_MATCH"
	HttpxOutcomeNotHTTP    HttpxAttemptOutcome = "NOT_HTTP"
)

const (
	ProtocolProbeConfirmed        = "CONFIRMED"
	ProtocolProbeUnconfirmed      = "UNCONFIRMED"
	ProtocolProbeNotHTTPConfirmed = "NOT_HTTP_CONFIRMED"
)

// HttpxAttemptResult preserves the outcome of a single explicit scheme probe.
type HttpxAttemptResult struct {
	Target         string
	Scheme         string
	ObservedScheme string
	Outcome        HttpxAttemptOutcome
	Asset          *Asset
	Response       *HttpxCLIResult
	Duration       time.Duration
}

// HttpxOptions httpx 扫描选项
type HttpxOptions struct {
	Concurrency     int      `json:"concurrency"`
	Timeout         int      `json:"timeout"`
	FollowRedirects bool     `json:"followRedirects"`
	MaxRedirects    int      `json:"maxRedirects"`
	TechDetect      bool     `json:"techDetect"`
	Favicon         bool     `json:"favicon"`
	ServerHeader    bool     `json:"serverHeader"`
	ContentType     bool     `json:"contentType"`
	Body            bool     `json:"body"`
	StatusCode      bool     `json:"statusCode"`
	Title           bool     `json:"title"`
	Screenshot      bool     `json:"screenshot"`
	OutputIP        bool     `json:"outputIP"`
	CustomHeaders   []string `json:"customHeaders"`
}

// Validate 验证配置
func (o *HttpxOptions) Validate() error {
	if o.Concurrency < 0 {
		return fmt.Errorf("concurrency must be non-negative, got %d", o.Concurrency)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	return nil
}

// HttpxCLIResult httpx CLI JSON 输出结构
type HttpxCLIResult struct {
	Input         string   `json:"input"`
	URL           string   `json:"url"`
	Scheme        string   `json:"scheme"`
	Host          string   `json:"host"`
	Port          string   `json:"port"`
	StatusCode    int      `json:"status-code"`
	Title         string   `json:"title"`
	Technologies  []string `json:"tech,omitempty"`
	WebServer     string   `json:"webserver,omitempty"`
	ContentType   string   `json:"content-type,omitempty"`
	ContentLength int64    `json:"content-length,omitempty"`
	ResponseBody  string   `json:"body,omitempty"`
	Headers       string   `json:"headers,omitempty"`
	FaviconMMH3   string   `json:"favicon-mmh3,omitempty"`
	FaviconData   string   `json:"favicon,omitempty"`
	IP            []string `json:"ip,omitempty"`
	Chain         []string `json:"chain,omitempty"`
	Screenshot    string   `json:"screenshot,omitempty"`
	Failed        bool     `json:"failed,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type httpxTargetResult struct {
	target          string
	asset           *Asset
	attempts        []HttpxAttemptResult
	selected        *HttpxAttemptResult
	priorSuccessful bool
	conflict        bool
}

// Scan 执行 httpx 扫描. Assets always contains the input references so a zero
// update cannot delete a discovered service.
func (s *HttpxScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		MainTaskId: config.MainTaskId,
		Assets:     append([]*Asset(nil), config.Assets...),
		Diagnostic: &ScanDiagnostic{Phase: "httpx"},
	}
	noOutputCount, parseFailedCount := 0, 0
	defer func() {
		emitHTTPXPhaseEvent(config.EventLogger, result.Diagnostic, noOutputCount, parseFailedCount)
	}()

	opts := &HttpxOptions{
		Concurrency: 1, Timeout: 10, FollowRedirects: true, MaxRedirects: 5,
		TechDetect: true, Favicon: true, ServerHeader: true, ContentType: true,
		StatusCode: true, Title: true, OutputIP: true,
	}
	if config.Options != nil {
		switch v := config.Options.(type) {
		case *HttpxOptions:
			opts = v
		default:
			if data, err := json.Marshal(config.Options); err == nil {
				_ = json.Unmarshal(data, opts)
			}
		}
	}

	taskLog := func(level, format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger(level, format, args...)
			return
		}
		switch level {
		case "ERROR", "WARN":
			logx.Errorf(format, args...)
		case "DEBUG":
			logx.Debugf(format, args...)
		default:
			logx.Infof(format, args...)
		}
	}

	if len(config.Assets) == 0 {
		result.Diagnostic.Status = PhaseSkippedNotApplicable
		return result, nil
	}

	targets, targetMap := s.buildTargets(config.Assets)
	targets = uniqueStrings(targets)
	result.Diagnostic.Coverage.Input = len(targets)
	if len(targets) == 0 {
		result.Diagnostic.Status = PhaseSkippedNotApplicable
		return result, nil
	}

	taskLog("INFO", "Httpx(CLI): scanning %d targets", len(targets))
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = config.WorkerConcurrency
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 5 {
		concurrency = 5
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}
	taskLog("INFO", "Httpx(CLI): using %d workers for %d targets", concurrency, len(targets))

	targetChan := make(chan string, len(targets))
	resultChan := make(chan httpxTargetResult, len(targets))
	var scanWg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for target := range targetChan {
				asset := targetMap[target]
				probe := s.probeTarget(ctx, target, asset, opts, targetMap, taskLog)
				resultChan <- probe
				if config.OnTargetDone != nil {
					if probe.asset != nil {
						config.OnTargetDone(target, []*Asset{probe.asset})
					} else {
						config.OnTargetDone(target, nil)
					}
				}
			}
		}()
	}

dispatch:
	for _, target := range targets {
		select {
		case <-ctx.Done():
			break dispatch
		case targetChan <- target:
		}
	}
	close(targetChan)
	go func() {
		scanWg.Wait()
		close(resultChan)
	}()

	processed := make(map[*Asset]bool)
	for probe := range resultChan {
		if probe.selected != nil && probe.asset != nil && !processed[probe.asset] {
			applyHttpxResult(probe.asset, *probe.selected)
			processed[probe.asset] = true
		}
		for _, attempt := range probe.attempts {
			switch attempt.Outcome {
			case HttpxOutcomeNoOutput:
				noOutputCount++
			case HttpxOutcomeParseError:
				parseFailedCount++
			}
		}
		summarizeHttpxTarget(result.Diagnostic, probe)
		emitHTTPXProbeEvent(config.EventLogger, probe)
	}
	if dispatched := result.Diagnostic.Coverage.Attempted + countPriorSuccesses(result.Diagnostic.Coverage); dispatched < result.Diagnostic.Coverage.Input && ctx.Err() != nil {
		result.Diagnostic.Coverage.Unconfirmed += result.Diagnostic.Coverage.Input - dispatched
	}
	result.Diagnostic.Status = deriveHttpxPhaseStatus(ctx, result.Diagnostic.Coverage)
	taskLog("INFO", "Httpx(CLI): completed, input=%d attempted=%d succeeded=%d timed_out=%d failed=%d zero_update=%d status=%s",
		result.Diagnostic.Coverage.Input, result.Diagnostic.Coverage.Attempted, result.Diagnostic.Coverage.Succeeded,
		result.Diagnostic.Coverage.TimedOut, result.Diagnostic.Coverage.Failed,
		result.Diagnostic.Coverage.ZeroUpdate, result.Diagnostic.Status)
	return result, nil
}

func emitHTTPXPhaseEvent(eventLog ScanEventLogger, diagnostic *ScanDiagnostic, noOutput, parseFailed int) {
	if eventLog == nil || diagnostic == nil {
		return
	}
	coverage := diagnostic.Coverage
	eventLog(EventHTTPXPhaseComplete, "httpx", string(diagnostic.Status), map[string]interface{}{
		"input": coverage.Input, "attempted": coverage.Attempted, "succeeded": coverage.Succeeded,
		"timed_out": coverage.TimedOut, "failed": coverage.Failed, "no_output": noOutput,
		"parse_failed": parseFailed, "zero_update": coverage.ZeroUpdate,
		"unconfirmed": coverage.Unconfirmed, "status": string(diagnostic.Status),
	})
}

func emitHTTPXProbeEvent(eventLog ScanEventLogger, probe httpxTargetResult) {
	if eventLog == nil || probe.asset == nil {
		return
	}
	attempted := make([]string, 0, len(probe.attempts))
	httpOutcome, httpsOutcome := "", ""
	var duration time.Duration
	for _, attempt := range probe.attempts {
		attempted = append(attempted, attempt.Scheme)
		duration += attempt.Duration
		switch attempt.Scheme {
		case SchemeHTTP:
			httpOutcome = string(attempt.Outcome)
		case SchemeHTTPS:
			httpsOutcome = string(attempt.Outcome)
		}
	}
	selectedScheme, evidenceKind := "", ""
	if probe.selected != nil {
		selectedScheme = probe.selected.ObservedScheme
		if selectedScheme == "" {
			selectedScheme = probe.selected.Scheme
		}
		evidenceKind = SchemeEvidenceSuccessfulResponse
	}
	outcome := "UNCONFIRMED"
	if probe.priorSuccessful || probe.selected != nil {
		outcome = "CONFIRMED"
	}
	eventLog(EventSchemeProbeComplete, "scheme", outcome, map[string]interface{}{
		"host": probe.asset.Host, "port": probe.asset.Port, "attempted_schemes": attempted,
		"selected_scheme": selectedScheme, "evidence_kind": evidenceKind,
		"http_outcome": httpOutcome, "https_outcome": httpsOutcome,
		"conflict": probe.conflict, "duration_ms": duration.Milliseconds(),
	})
}

func countPriorSuccesses(coverage Coverage) int {
	if coverage.Succeeded > coverage.Attempted {
		return coverage.Succeeded - coverage.Attempted
	}
	return 0
}

func (s *HttpxScanner) probeTarget(ctx context.Context, target string, asset *Asset, opts *HttpxOptions, targetMap map[string]*Asset, taskLog func(string, string, ...interface{})) httpxTargetResult {
	probe := httpxTargetResult{target: target, asset: asset}
	if asset == nil {
		return probe
	}

	evidence := httpxSchemeEvidence(asset)
	resolution := ResolveScheme(evidence)
	probe.conflict = resolution.Conflict
	if hasSuccessfulHTTPResponseEvidence(asset) && resolution.HasEvidence {
		asset.ProtocolProbeStatus = ProtocolProbeConfirmed
		probe.priorSuccessful = true
		return probe
	}

	for _, scheme := range httpxAttemptSchemes(asset) {
		explicitTarget := scheme + "://" + target
		attempt := s.scanSingleTargetCLI(ctx, explicitTarget, opts, targetMap, taskLog)
		probe.attempts = append(probe.attempts, attempt)
		if attempt.Outcome == HttpxOutcomeSuccess {
			observed := attempt.ObservedScheme
			if observed == "" {
				observed = scheme
			}
			evidence = append(evidence, SchemeEvidence{Scheme: observed, Kind: SchemeEvidenceSuccessfulResponse, Success: true, StatusCode: attempt.Response.StatusCode})
		}
	}

	resolution = ResolveScheme(evidence)
	probe.conflict = resolution.Conflict
	if resolution.SelectedEvidence.Kind == SchemeEvidenceSuccessfulResponse {
		for i := range probe.attempts {
			attempt := &probe.attempts[i]
			if attempt.Outcome == HttpxOutcomeSuccess && attempt.ObservedScheme == resolution.Scheme {
				probe.selected = attempt
				break
			}
		}
		if probe.selected == nil {
			for i := range probe.attempts {
				if probe.attempts[i].Outcome == HttpxOutcomeSuccess {
					probe.selected = &probe.attempts[i]
					break
				}
			}
		}
	}
	return probe
}

// scanSingleTargetCLI executes one explicit URL and returns a classified result.
func (s *HttpxScanner) scanSingleTargetCLI(ctx context.Context, target string, opts *HttpxOptions, targetMap map[string]*Asset, taskLog func(level, format string, args ...interface{})) HttpxAttemptResult {
	attempt := HttpxAttemptResult{Target: target}
	safeTarget := sanitizedTarget(target)
	if parsed, err := url.Parse(target); err == nil {
		attempt.Scheme = normalizeScheme(parsed.Scheme)
	}
	args := s.httpxArgs(target, opts)
	taskLog("INFO", "[Httpx] CLI: target=%s", safeTarget)

	res, err := s.executeCommand(ctx, args, ExecuteOpts{Timeout: time.Duration(opts.Timeout+10) * time.Second, LogFn: taskLog})
	if res != nil {
		attempt.Duration = res.Duration
	}
	if err != nil {
		if isHttpxTimeout(ctx, err) {
			attempt.Outcome = HttpxOutcomeTimeout
		} else {
			attempt.Outcome = HttpxOutcomeExecError
		}
		taskLog("ERROR", "Httpx(CLI): target=%s outcome=%s exitCode=%d stdout_bytes=%d stderr_bytes=%d error_type=%T",
			safeTarget, attempt.Outcome, exitCodeOf(res), outputBytesOf(res, true), outputBytesOf(res, false), err)
		return attempt
	}
	if res == nil {
		attempt.Outcome = HttpxOutcomeExecError
		taskLog("ERROR", "Httpx(CLI): target=%s outcome=nil_result", safeTarget)
		return attempt
	}
	if res.ExitCode != 0 {
		attempt.Outcome = HttpxOutcomeExecError
		taskLog("ERROR", "Httpx(CLI): target=%s outcome=nonzero_exit exitCode=%d stdout_bytes=%d stderr_bytes=%d",
			safeTarget, res.ExitCode, len(res.Stdout), len(res.Stderr))
		return attempt
	}
	if s.executor != nil {
		s.executor.LogResult("Httpx(CLI): "+safeTarget, res, nil)
	}
	if strings.TrimSpace(res.Stdout) == "" {
		attempt.Outcome = HttpxOutcomeNoOutput
		return attempt
	}

	scanner := newLineScanner(res.Stdout)
	lineCount, parseFailCount, validCount := 0, 0, 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineCount++
		var hr HttpxCLIResult
		if err := json.Unmarshal([]byte(line), &hr); err != nil {
			parseFailCount++
			continue
		}
		validCount++
		asset := s.matchAsset(hr, targetMap)
		if asset == nil {
			continue
		}
		attempt.Asset = asset
		attempt.Response = &hr
		attempt.ObservedScheme = normalizeScheme(hr.Scheme)
		if attempt.ObservedScheme == "" {
			attempt.ObservedScheme = attempt.Scheme
		}
		if hr.Failed {
			attempt.Outcome = HttpxOutcomeNotHTTP
			return attempt
		}
		attempt.Outcome = HttpxOutcomeSuccess
		return attempt
	}
	if parseFailCount > 0 && validCount == 0 {
		attempt.Outcome = HttpxOutcomeParseError
	} else {
		attempt.Outcome = HttpxOutcomeNoMatch
	}
	taskLog("DEBUG", "[Httpx] target=%s lines=%d parseFail=%d outcome=%s", target, lineCount, parseFailCount, attempt.Outcome)
	return attempt
}

func (s *HttpxScanner) httpxArgs(target string, opts *HttpxOptions) []string {
	threads := opts.Concurrency
	if threads <= 0 {
		threads = 1
	}
	args := []string{"-u", target, "-json", "-silent", "-timeout", fmt.Sprintf("%d", opts.Timeout), "-threads", fmt.Sprintf("%d", threads), "-disable-update-check"}
	if opts.FollowRedirects {
		args = append(args, "-follow-redirects")
		if opts.MaxRedirects > 0 {
			args = append(args, "-max-redirects", fmt.Sprintf("%d", opts.MaxRedirects))
		}
	}
	if opts.TechDetect {
		args = append(args, "-tech-detect")
	}
	if opts.Favicon {
		args = append(args, "-favicon")
	}
	if opts.ServerHeader {
		args = append(args, "-web-server")
	}
	if opts.ContentType {
		args = append(args, "-content-type")
	}
	if opts.Body {
		args = append(args, "-body")
	}
	if opts.StatusCode {
		args = append(args, "-status-code")
	}
	if opts.Title {
		args = append(args, "-title")
	}
	if opts.OutputIP {
		args = append(args, "-ip")
	}
	for _, h := range opts.CustomHeaders {
		args = append(args, "-header", h)
	}
	return args
}

func (s *HttpxScanner) executeCommand(ctx context.Context, args []string, opts ExecuteOpts) (*ExecuteResult, error) {
	if s.execute != nil {
		return s.execute(ctx, args, opts)
	}
	return s.executor.Execute(ctx, args, opts)
}

func exitCodeOf(result *ExecuteResult) int {
	if result == nil {
		return -1
	}
	return result.ExitCode
}

func outputBytesOf(result *ExecuteResult, stdout bool) int {
	if result == nil {
		return 0
	}
	if stdout {
		return len(result.Stdout)
	}
	return len(result.Stderr)
}

func isHttpxTimeout(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func hasSuccessfulHTTPResponseEvidence(asset *Asset) bool {
	return asset != nil && asset.IsHTTP && asset.HttpStatus != "" && normalizeScheme(asset.Service) != ""
}

func httpxSchemeEvidence(asset *Asset) []SchemeEvidence {
	return assetSchemeEvidence(asset)
}

func httpxAttemptSchemes(asset *Asset) []string {
	resolution := resolveAssetScheme(asset)
	// A TLS port hint controls attempt order (not final selection) so incorrect
	// service metadata such as http:443 cannot prevent collecting stronger HTTPS
	// response evidence.
	if (resolution.HasEvidence && resolution.Scheme == SchemeHTTPS) || (asset != nil && isLikelyTLSPort(asset.Port)) {
		return []string{SchemeHTTPS, SchemeHTTP}
	}
	return []string{SchemeHTTP, SchemeHTTPS}
}

func isLikelyTLSPort(port int) bool {
	return schemePortHint(port) == SchemeHTTPS
}

func summarizeHttpxTarget(diagnostic *ScanDiagnostic, probe httpxTargetResult) {
	coverage := &diagnostic.Coverage
	if probe.asset == nil {
		coverage.Attempted++
		coverage.Unconfirmed++
		coverage.Failed++
		appendWarningCode(diagnostic, ReasonUnconfirmed)
		return
	}
	if probe.priorSuccessful {
		coverage.Succeeded++
		return
	}
	coverage.Attempted++

	success := probe.selected != nil
	notHTTP := 0
	hasTimeout := false
	for _, attempt := range probe.attempts {
		if attempt.Outcome == HttpxOutcomeNotHTTP {
			notHTTP++
		}
		if attempt.Outcome == HttpxOutcomeTimeout {
			hasTimeout = true
		}
		appendHttpxAttemptDiagnostic(diagnostic, attempt, probe)
	}
	if success {
		coverage.Succeeded++
		probe.asset.ProtocolProbeStatus = ProtocolProbeConfirmed
		return
	}

	coverage.ZeroUpdate++
	if len(probe.attempts) == 2 && notHTTP == 2 {
		coverage.Succeeded++
		probe.asset.ProtocolProbeStatus = ProtocolProbeNotHTTPConfirmed
		probe.asset.IsHTTP = false
		return
	}
	coverage.Unconfirmed++
	probe.asset.ProtocolProbeStatus = ProtocolProbeUnconfirmed
	if hasTimeout {
		coverage.TimedOut++
		appendWarningCode(diagnostic, ReasonTimeout)
	} else {
		coverage.Failed++
		appendWarningCode(diagnostic, httpxTargetFailureReason(probe.attempts))
	}
}

func appendHttpxAttemptDiagnostic(diagnostic *ScanDiagnostic, attempt HttpxAttemptResult, probe httpxTargetResult) {
	if len(diagnostic.Targets) >= MaxTargetDiagnostics {
		return
	}
	selectedScheme := ""
	if probe.selected != nil {
		selectedScheme = probe.selected.ObservedScheme
	}
	diagnostic.Targets = append(diagnostic.Targets, TargetDiagnostic{
		Target: attempt.Target, Host: hostForDiagnostic(probe.asset), Port: portForDiagnostic(probe.asset),
		Outcome: string(attempt.Outcome), ReasonCode: httpxOutcomeReason(attempt.Outcome), DurationMs: attempt.Duration.Milliseconds(),
		Metadata: map[string]interface{}{
			"attempted_schemes": attempt.Scheme, "selected_scheme": selectedScheme,
			"evidence_kind": SchemeEvidenceSuccessfulResponse, "conflict": probe.conflict,
		},
	})
}

func httpxOutcomeReason(outcome HttpxAttemptOutcome) string {
	switch outcome {
	case HttpxOutcomeTimeout:
		return ReasonTimeout
	case HttpxOutcomeExecError:
		return ReasonExecutionError
	case HttpxOutcomeNoOutput:
		return ReasonNoOutput
	case HttpxOutcomeParseError:
		return ReasonParseError
	case HttpxOutcomeNoMatch:
		return ReasonNoMatch
	case HttpxOutcomeNotHTTP:
		return ReasonNotHTTP
	default:
		return ""
	}
}

func httpxTargetFailureReason(attempts []HttpxAttemptResult) string {
	for _, attempt := range attempts {
		if reason := httpxOutcomeReason(attempt.Outcome); reason != "" {
			return reason
		}
	}
	return ReasonUnconfirmed
}

func appendWarningCode(diagnostic *ScanDiagnostic, code string) {
	if code == "" {
		return
	}
	for _, existing := range diagnostic.WarningCodes {
		if existing == code {
			return
		}
	}
	diagnostic.WarningCodes = append(diagnostic.WarningCodes, code)
}

func deriveHttpxPhaseStatus(ctx context.Context, coverage Coverage) PhaseStatus {
	if ctx.Err() != nil {
		return PhaseCanceled
	}
	if coverage.Input == 0 {
		return PhaseSkippedNotApplicable
	}
	if coverage.Succeeded >= coverage.Input && coverage.Unconfirmed == 0 {
		return PhaseComplete
	}
	if coverage.Succeeded > 0 {
		return PhasePartial
	}
	if coverage.TimedOut > 0 || coverage.Failed > 0 || coverage.Unconfirmed > 0 {
		return PhaseFailed
	}
	return PhaseUncovered
}

func applyHttpxResult(asset *Asset, attempt HttpxAttemptResult) {
	if asset == nil || attempt.Response == nil {
		return
	}
	hr := attempt.Response
	scheme := normalizeScheme(hr.Scheme)
	if scheme == "" {
		scheme = attempt.Scheme
	}
	persistSuccessfulScheme(asset, scheme, hr.StatusCode)
	asset.HttpStatus = fmt.Sprintf("%d", hr.StatusCode)
	asset.Title = hr.Title
	for _, tech := range hr.Technologies {
		asset.App = append(asset.App, tech+"[httpx]")
	}
	if hr.FaviconMMH3 != "" {
		asset.IconHash = hr.FaviconMMH3
	}
	if hr.FaviconData != "" {
		if data, err := base64.StdEncoding.DecodeString(hr.FaviconData); err == nil && len(data) > 0 {
			asset.IconData = data
		}
	}
	if hr.WebServer != "" {
		asset.Server = hr.WebServer
	}
	if hr.Headers != "" {
		asset.HttpHeader = hr.Headers
	}
	if hr.ResponseBody != "" {
		body := hr.ResponseBody
		if len(body) > 50*1024 {
			body = body[:50*1024] + "\n...[truncated]"
		}
		asset.HttpBody = body
	}
	if hr.ContentType != "" {
		asset.ContentType = hr.ContentType
	}
	if hr.ContentLength > 0 {
		asset.ContentLength = hr.ContentLength
	}
	asset.IsHTTP = true
	asset.ProtocolProbeStatus = ProtocolProbeConfirmed
	if hr.Screenshot != "" && asset.Screenshot == "" {
		asset.Screenshot = hr.Screenshot
	}
	if len(hr.IP) > 0 {
		ipLocator := geolocation.NewIPLocator()
		for _, ipStr := range hr.IP {
			appendIPInfo(asset, ipStr, ipLocator)
		}
	}
}

func hostForDiagnostic(asset *Asset) string {
	if asset == nil {
		return ""
	}
	return asset.Host
}
func portForDiagnostic(asset *Asset) int {
	if asset == nil {
		return 0
	}
	return asset.Port
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// buildTargets 构建目标列表和映射
func (s *HttpxScanner) buildTargets(assets []*Asset) ([]string, map[string]*Asset) {
	var targets []string
	targetMap := make(map[string]*Asset)
	for _, asset := range assets {
		ports := []int{asset.Port}
		if asset.Port == 0 {
			ports = []int{80, 443}
		}
		for _, p := range ports {
			hostPort := net.JoinHostPort(asset.Host, fmt.Sprintf("%d", p))
			target := hostPort
			targets = append(targets, target)
			targetMap[target] = asset
			targetMap["http://"+hostPort] = asset
			targetMap["https://"+hostPort] = asset
		}
	}
	return targets, targetMap
}

// matchAsset 将 httpx 结果匹配到 Asset
func (s *HttpxScanner) matchAsset(hr HttpxCLIResult, targetMap map[string]*Asset) *Asset {
	if hr.Input != "" {
		if asset, ok := targetMap[hr.Input]; ok {
			return asset
		}
	}
	if hr.URL != "" {
		if asset, ok := targetMap[hr.URL]; ok {
			return asset
		}
		if u, err := url.Parse(hr.URL); err == nil {
			host, port := u.Hostname(), u.Port()
			if port == "" {
				if u.Scheme == "https" {
					port = "443"
				} else {
					port = "80"
				}
			}
			if asset, ok := targetMap[net.JoinHostPort(host, port)]; ok {
				return asset
			}
		}
	}
	return nil
}

// RunHttpxLib 使用 CLI 方式执行 httpx 扫描（兼容旧接口）
func RunHttpxLib(ctx context.Context, assets []*Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{})) error {
	_, err := runHttpxLibWithEventsResult(ctx, assets, opts, taskLog, nil)
	return err
}

func runHttpxLibWithEvents(ctx context.Context, assets []*Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{}), eventLog ScanEventLogger) error {
	_, err := runHttpxLibWithEventsResult(ctx, assets, opts, taskLog, eventLog)
	return err
}

func runHttpxLibWithEventsResult(ctx context.Context, assets []*Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{}), eventLog ScanEventLogger) (*ScanDiagnostic, error) {
	if len(assets) == 0 {
		return &ScanDiagnostic{Phase: "httpx", Status: PhaseSkippedNotApplicable}, nil
	}
	scanner := NewHttpxScanner()
	scanResult, err := scanner.Scan(ctx, &ScanConfig{Assets: assets, Options: &HttpxOptions{
		Concurrency: 150, Timeout: opts.TargetTimeout, FollowRedirects: true, MaxRedirects: 5,
		TechDetect: true, Favicon: true, Screenshot: opts.Screenshot, ServerHeader: true,
		ContentType: true, StatusCode: true, Title: true, Body: false, OutputIP: true,
	}, TaskLogger: taskLog, EventLogger: eventLog})
	if scanResult == nil {
		return nil, err
	}
	return scanResult.Diagnostic, err
}
