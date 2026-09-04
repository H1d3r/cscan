package scanner

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cscan/internal/model"
	"cscan/pkg/utils"

	"github.com/chromedp/chromedp"
	wappalyzer "github.com/projectdiscovery/wappalyzergo"
	"github.com/zeromicro/go-zero/core/logx"
)

// chromedp 全局持久化分配器 + 共享浏览器实例
// 使用 context.Background() 确保不因外部 context 取消而触发 chromedp 内部的 close-of-closed-channel panic
// 共享浏览器模式：全局只启动 1 个 Chrome 进程，每次截图通过 NewContext(browserCtx) 创建新 Tab
// 超时时取消 Tab 上下文（安全，仅关闭标签页），不取消浏览器上下文（避免触发 close-of-closed-channel panic）
var (
	globalAllocCtx    context.Context
	globalAllocCancel context.CancelFunc
	globalAllocOnce   sync.Once

	globalBrowserCtx    context.Context
	globalBrowserCancel context.CancelFunc
	globalBrowserMu     sync.Mutex
	globalBrowserInited bool

	// chromedpSemaphore 限制并发 Tab 数，避免内存耗尽
	chromedpSemaphore = make(chan struct{}, 3)
)

func getGlobalAllocator() (context.Context, context.CancelFunc) {
	globalAllocOnce.Do(func() {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", false),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true),
			chromedp.Flag("ignore-certificate-errors", true),
			chromedp.Flag("disable-web-security", true),
			chromedp.Flag("disable-features", "VizDisplayCompositor"),
			chromedp.Flag("disable-background-timer-throttling", true),
			chromedp.Flag("disable-backgrounding-occluded-windows", true),
			chromedp.Flag("disable-renderer-backgrounding", true),
			chromedp.Flag("force-color-profile", "srgb"),
			chromedp.WindowSize(1920, 1080),
			chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		)
		if chromePath := os.Getenv("CHROME_BIN"); chromePath != "" {
			opts = append(opts, chromedp.ExecPath(chromePath))
		}
		globalAllocCtx, globalAllocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	})
	return globalAllocCtx, globalAllocCancel
}

// getGlobalBrowser 获取全局共享浏览器上下文（首次调用启动 Chrome 进程，后续复用）
// 返回的 context 用于派生 Tab 上下文：chromedp.NewContext(browserCtx) 创建新标签页
func getGlobalBrowser(taskLog func(level, format string, args ...interface{})) (context.Context, error) {
	globalBrowserMu.Lock()
	defer globalBrowserMu.Unlock()

	if globalBrowserInited && globalBrowserCtx != nil {
		return globalBrowserCtx, nil
	}

	// 重置可能残留的旧状态
	if globalBrowserCancel != nil {
		globalBrowserCancel()
	}
	globalBrowserCtx = nil
	globalBrowserCancel = nil
	globalBrowserInited = false

	allocCtx, _ := getGlobalAllocator()
	ctx, cancel := chromedp.NewContext(allocCtx)
	// 执行空操作以触发浏览器实际启动（chromedp 是惰性初始化）
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("chromedp browser init failed: %w", err)
	}

	globalBrowserCtx = ctx
	globalBrowserCancel = cancel
	globalBrowserInited = true
	if taskLog == nil {
		taskLog = func(level, format string, args ...interface{}) {
			logx.Infof(format, args...)
		}
	}
	taskLog("INFO", "[Chromedp] Global browser initialized (single Chrome process, tab-per-screenshot)")
	return globalBrowserCtx, nil
}

// CleanupChromedp 清理全局 chromedp 资源（浏览器进程 + 分配器）
// 应在 Worker 停止时调用，确保 Chrome 进程不残留
func CleanupChromedp() {
	globalBrowserMu.Lock()
	defer globalBrowserMu.Unlock()

	if globalBrowserCancel != nil {
		globalBrowserCancel()
		globalBrowserCancel = nil
	}
	globalBrowserCtx = nil
	globalBrowserInited = false

	if globalAllocCancel != nil {
		globalAllocCancel()
		globalAllocCancel = nil
	}
	globalAllocCtx = nil

	logx.Info("[Chromedp] Global browser and allocator cleaned up")
}

// FingerprintScanner 指纹扫描器
// 使用 assetMutex 保护对共享 asset 数据的并发访问
type FingerprintScanner struct {
	BaseScanner
	client                  *http.Client
	wappalyzerClient        *wappalyzer.Wappalyze
	customFingerprintEngine *CustomFingerprintEngine
	// Test seams also keep target construction observable without launching
	// external httpx/Chrome or dialing public certificate endpoints.
	runHttpx               func(context.Context, []*Asset, *FingerprintOptions, func(string, string, ...interface{})) error
	runHttpxWithDiagnostic func(context.Context, []*Asset, *FingerprintOptions, func(string, string, ...interface{})) (*ScanDiagnostic, error)
	captureScreen          func(context.Context, string, func(string, string, ...interface{})) string
	fetchCert              func(context.Context, string, int, time.Duration) *CertResult
	// assetMutex 保护 httpx 回调和主循环对同一 asset 的并发访问
	assetMutex sync.Mutex
	// httpxDone 标记 httpx 扫描是否完成，用于主循环等待
	httpxDone   bool
	httpxDoneMu sync.Mutex
}

// AppDetectionResult 应用检测结果，用于合并多个来源的识别结果
type AppDetectionResult struct {
	Name         string   // 应用名称
	OriginalName string   // 原始名称（可能包含版本号）
	Sources      []string // 检测来源：httpx, wappalyzer, custom
	CustomIDs    []string // 自定义指纹的ID列表
	ActiveIDs    []string // 主动指纹的ID列表
}

const (
	fingerprintDecisionConfirmed = "CONFIRMED"
	fingerprintDecisionCandidate = "CANDIDATE"

	fingerprintWeakFanoutThreshold = 3
	fingerprintConflictCloseDelta  = 10
)

type fingerprintEvidenceObservation struct {
	source   string
	evidence FingerprintEvidence
}

// GovernFingerprintFindings applies confidence, conflict, and weak fan-out
// policy after rule evaluation. It never mutates the input findings or changes
// RawMatched; governance only decides whether a raw match is confirmed or kept
// as an auditable candidate.
func GovernFingerprintFindings(findings FingerprintFindings) FingerprintFindings {
	groups := make(map[string][]FingerprintFinding)
	order := make([]string, 0)
	for _, finding := range findings {
		if !finding.RawMatched {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(finding.Name))
		if key == "" {
			continue
		}
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], finding)
	}

	governed := make(FingerprintFindings, 0, len(order))
	for _, key := range order {
		merged, observations := mergeRawFingerprintFindings(groups[key])
		applyFingerprintConfidence(&merged, observations)
		governed = append(governed, merged)
	}

	applyWeakFingerprintFanout(governed)
	applyFingerprintConflicts(governed)
	sort.SliceStable(governed, func(i, j int) bool {
		return strings.ToLower(governed[i].Name) < strings.ToLower(governed[j].Name)
	})
	return governed
}

func mergeRawFingerprintFindings(findings []FingerprintFinding) (FingerprintFinding, []fingerprintEvidenceObservation) {
	merged := findings[0]
	merged.RawMatched = true
	merged.Decision = ""
	merged.Confidence = 0
	merged.ReasonCode = ""
	merged.Evidence = nil

	sources := make(map[string]struct{})
	ids := make(map[string]struct{})
	coexistence := make(map[string]struct{})
	exclusiveWith := make(map[string]struct{})
	evidenceSeen := make(map[string]struct{})
	observations := make([]fingerprintEvidenceObservation, 0)

	for _, finding := range findings {
		source := strings.TrimSpace(finding.Source)
		if source == "" {
			source = "custom"
		}
		sources[source] = struct{}{}
		if finding.FingerprintID != "" {
			ids[finding.FingerprintID] = struct{}{}
		}
		if merged.ConflictGroup == "" {
			merged.ConflictGroup = finding.ConflictGroup
		}
		for _, item := range finding.Coexistence {
			if item = strings.TrimSpace(item); item != "" {
				coexistence[item] = struct{}{}
			}
		}
		for _, item := range finding.ExclusiveWith {
			if item = strings.TrimSpace(item); item != "" {
				exclusiveWith[item] = struct{}{}
			}
		}
		for _, evidence := range finding.Evidence {
			observations = append(observations, fingerprintEvidenceObservation{source: source, evidence: evidence})
			key := source + "\x00" + evidence.Channel + "\x00" + evidence.Strength + "\x00" + evidence.MatchedValueDigest
			if _, exists := evidenceSeen[key]; exists {
				continue
			}
			evidenceSeen[key] = struct{}{}
			merged.Evidence = append(merged.Evidence, evidence)
		}
	}

	merged.Source = joinSortedSet(sources)
	merged.FingerprintID = joinSortedSet(ids)
	merged.Coexistence = sortedSetValues(coexistence)
	merged.ExclusiveWith = sortedSetValues(exclusiveWith)
	return merged, observations
}

func applyFingerprintConfidence(finding *FingerprintFinding, observations []fingerprintEvidenceObservation) {
	strong := make(map[string]struct{})
	medium := make(map[string]struct{})
	weak := make(map[string]struct{})
	incomplete := false
	for _, observation := range observations {
		evidence := observation.evidence
		if !evidence.Complete {
			incomplete = true
			continue
		}
		key := observation.source + "\x00" + evidence.Channel
		switch strings.ToLower(evidence.Strength) {
		case "strong":
			strong[key] = struct{}{}
		case "medium":
			medium[key] = struct{}{}
		default:
			weak[key] = struct{}{}
		}
	}

	switch {
	case len(strong) > 0:
		finding.Decision = fingerprintDecisionConfirmed
		finding.Confidence = 95
		finding.ReasonCode = "confirmed_strong_evidence"
	case len(medium) >= 2:
		finding.Decision = fingerprintDecisionConfirmed
		finding.Confidence = 85
		finding.ReasonCode = "confirmed_independent_medium_evidence"
	case hasIndependentMediumAndWeak(medium, weak):
		finding.Decision = fingerprintDecisionConfirmed
		finding.Confidence = 75
		finding.ReasonCode = "confirmed_medium_weak_evidence"
	case len(medium) > 0:
		finding.Decision = fingerprintDecisionCandidate
		finding.Confidence = 55
		finding.ReasonCode = "insufficient_evidence"
	case len(weak) > 0:
		finding.Decision = fingerprintDecisionCandidate
		finding.Confidence = 25
		finding.ReasonCode = "insufficient_evidence"
	default:
		finding.Decision = fingerprintDecisionCandidate
		finding.Confidence = 10
		if incomplete {
			finding.ReasonCode = "incomplete_evidence"
		} else {
			finding.ReasonCode = "no_complete_evidence"
		}
	}
}

func hasIndependentMediumAndWeak(medium, weak map[string]struct{}) bool {
	for mediumKey := range medium {
		for weakKey := range weak {
			if mediumKey != weakKey {
				return true
			}
		}
	}
	return false
}

func applyWeakFingerprintFanout(findings FingerprintFindings) {
	weakOnly := make([]int, 0)
	for i := range findings {
		if findings[i].Decision == fingerprintDecisionCandidate && findings[i].Confidence == 25 {
			weakOnly = append(weakOnly, i)
		}
	}
	if len(weakOnly) <= fingerprintWeakFanoutThreshold {
		return
	}
	for _, index := range weakOnly {
		findings[index].ReasonCode = "weak_fanout"
	}
}

func applyFingerprintConflicts(findings FingerprintFindings) {
	conflictGroups := make(map[string][]int)
	for i := range findings {
		group := strings.ToLower(strings.TrimSpace(findings[i].ConflictGroup))
		if group != "" {
			conflictGroups[group] = append(conflictGroups[group], i)
		}
	}
	for _, indexes := range conflictGroups {
		filtered := make([]int, 0, len(indexes))
		for _, index := range indexes {
			coexistsWithAll := true
			for _, other := range indexes {
				if index != other && !fingerprintsMayCoexist(findings[index], findings[other]) {
					coexistsWithAll = false
					break
				}
			}
			if !coexistsWithAll {
				filtered = append(filtered, index)
			}
		}
		downgradeFingerprintConflict(findings, filtered)
	}

	seenPairs := make(map[string]struct{})
	for i := range findings {
		for j := i + 1; j < len(findings); j++ {
			if fingerprintsMayCoexist(findings[i], findings[j]) || !fingerprintsExplicitlyConflict(findings[i], findings[j]) {
				continue
			}
			pairKey := fmt.Sprintf("%d:%d", i, j)
			if _, seen := seenPairs[pairKey]; seen {
				continue
			}
			seenPairs[pairKey] = struct{}{}
			downgradeFingerprintConflict(findings, []int{i, j})
		}
	}
}

func downgradeFingerprintConflict(findings FingerprintFindings, indexes []int) {
	if len(indexes) < 2 {
		return
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		return findings[indexes[i]].Confidence > findings[indexes[j]].Confidence
	})
	if findings[indexes[0]].Confidence-findings[indexes[1]].Confidence <= fingerprintConflictCloseDelta {
		for _, index := range indexes {
			findings[index].Decision = fingerprintDecisionCandidate
			findings[index].ReasonCode = "exclusive_conflict_close_score"
		}
		return
	}
	for _, index := range indexes[1:] {
		findings[index].Decision = fingerprintDecisionCandidate
		findings[index].ReasonCode = "exclusive_conflict_lower_confidence"
	}
}

func fingerprintsExplicitlyConflict(left, right FingerprintFinding) bool {
	return containsFold(left.ExclusiveWith, right.Name) || containsFold(right.ExclusiveWith, left.Name)
}

func fingerprintsMayCoexist(left, right FingerprintFinding) bool {
	return containsFold(left.Coexistence, right.Name) || containsFold(right.Coexistence, left.Name) ||
		(left.ConflictGroup != "" && (containsFold(left.Coexistence, right.ConflictGroup) || containsFold(right.Coexistence, left.ConflictGroup)))
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func joinSortedSet(values map[string]struct{}) string {
	return strings.Join(sortedSetValues(values), "+")
}

func sortedSetValues(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// resetFingerprintFindingsForScan starts a fresh collection window while
// retaining historical findings until a real response proves the current
// fingerprint phase completed for that asset.
func resetFingerprintFindingsForScan(assets []*Asset) {
	for _, asset := range assets {
		if asset != nil {
			asset.FingerprintFindingsCollected = false
		}
	}
}

// markFingerprintFindingsCollected clears historical findings exactly once per
// scan window. An empty current result is intentional and must reach storage.
func markFingerprintFindingsCollected(asset *Asset) {
	if asset == nil {
		return
	}
	if !asset.FingerprintFindingsCollected {
		asset.FingerprintFindings = nil
	}
	asset.FingerprintFindingsCollected = true
}

func hasFingerprintResponseEvidence(asset *Asset) bool {
	if asset == nil {
		return false
	}
	return asset.HttpStatus != "" || (asset.IsHTTP &&
		(asset.Title != "" || asset.HttpBody != "" || asset.HttpHeader != "" || asset.Server != ""))
}

func applyGovernedFingerprintFindings(asset *Asset, appResults map[string]*AppDetectionResult, findings FingerprintFindings, taskLog func(level, format string, args ...interface{})) {
	if asset == nil {
		return
	}
	if !asset.FingerprintFindingsCollected {
		if taskLog != nil {
			taskLog("DEBUG", "[Fingerprint] findings skipped because no valid response was collected for %s:%d", asset.Host, asset.Port)
		}
		return
	}
	combined := make(FingerprintFindings, 0, len(asset.FingerprintFindings)+len(findings))
	combined = append(combined, asset.FingerprintFindings...)
	combined = append(combined, findings...)
	asset.FingerprintFindings = GovernFingerprintFindings(combined)
	for _, finding := range asset.FingerprintFindings {
		if finding.Decision != fingerprintDecisionConfirmed {
			if taskLog != nil {
				taskLog("DEBUG", "[Fingerprint] candidate %s confidence=%d reason=%s", finding.Name, finding.Confidence, finding.ReasonCode)
			}
			continue
		}
		mergeGovernedFingerprintDetection(appResults, finding)
		if taskLog != nil {
			taskLog("INFO", "发现应用指纹: %s:%d -> %s (来源: %s)", asset.Host, asset.Port, finding.Name, finding.Source)
		}
	}
}

func mergeGovernedFingerprintDetection(appResults map[string]*AppDetectionResult, finding FingerprintFinding) {
	sources := strings.Split(finding.Source, "+")
	ids := strings.Split(finding.FingerprintID, "+")
	incoming := &AppDetectionResult{Name: finding.Name, OriginalName: finding.Name}
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		incoming.Sources = append(incoming.Sources, source)
		if source == "custom" {
			incoming.CustomIDs = append(incoming.CustomIDs, ids...)
		}
		if source == "active" {
			incoming.ActiveIDs = append(incoming.ActiveIDs, ids...)
		}
	}
	mergeAppDetectionResult(appResults, incoming)
}

// NewFingerprintScanner 创建指纹扫描器
func NewFingerprintScanner() *FingerprintScanner {
	wappalyzerClient, _ := wappalyzer.New()
	return &FingerprintScanner{
		BaseScanner: BaseScanner{name: "fingerprint"},
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		wappalyzerClient: wappalyzerClient,
	}
}

func (s *FingerprintScanner) runHttpxScan(ctx context.Context, assets []*Asset, opts *FingerprintOptions, taskLog func(string, string, ...interface{})) error {
	if s.runHttpx != nil {
		return s.runHttpx(ctx, assets, opts, taskLog)
	}
	return RunHttpxLib(ctx, assets, opts, taskLog)
}

func mergeFingerprintDiagnostic(dst, src *ScanDiagnostic) {
	if dst == nil || src == nil {
		return
	}
	for _, code := range src.WarningCodes {
		appendWarningCode(dst, code)
	}
	for _, target := range src.Targets {
		if len(dst.Targets) >= MaxTargetDiagnostics {
			break
		}
		target.Metadata = cloneDiagnosticMetadata(target.Metadata)
		dst.Targets = append(dst.Targets, target)
	}
}

func cloneDiagnosticMetadata(metadata map[string]interface{}) map[string]interface{} {
	if len(metadata) == 0 {
		return nil
	}
	clone := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		clone[key] = value
	}
	return clone
}

func (s *FingerprintScanner) captureScreenshot(ctx context.Context, targetURL string, taskLog func(string, string, ...interface{})) string {
	if s.captureScreen != nil {
		return s.captureScreen(ctx, targetURL, taskLog)
	}
	return s.takeScreenshot(ctx, targetURL, taskLog)
}

func (s *FingerprintScanner) fetchCertificate(ctx context.Context, host string, port int, timeout time.Duration) *CertResult {
	if s.fetchCert != nil {
		return s.fetchCert(ctx, host, port, timeout)
	}
	return FetchCert(ctx, host, port, timeout)
}

// SetCustomFingerprintEngine 设置自定义指纹引擎
func (s *FingerprintScanner) SetCustomFingerprintEngine(engine *CustomFingerprintEngine) {
	s.customFingerprintEngine = engine
}

// FingerprintOptions 指纹识别选项
type FingerprintOptions struct {
	Enable        bool   `json:"enable"`
	Tool          string `json:"tool"`  // 探测工具: httpx, builtin (默认httpx)
	Httpx         bool   `json:"httpx"` // 已废弃，兼容旧配置
	Screenshot    bool   `json:"screenshot"`
	IconHash      bool   `json:"iconHash"`
	Wappalyzer    bool   `json:"wappalyzer"`
	CustomEngine  bool   `json:"customEngine"`  // 使用自定义指纹引擎
	ActiveScan    bool   `json:"activeScan"`    // 启用主动指纹扫描
	Cert          bool   `json:"cert"`          // 启用证书抓取（ARL 风格附加功能），默认关闭
	ActiveTimeout int    `json:"activeTimeout"` // 主动指纹单个请求超时时间(秒)，默认10秒
	Timeout       int    `json:"timeout"`       // 总超时时间(秒)，默认300秒
	TargetTimeout int    `json:"targetTimeout"` // 单个目标超时时间(秒)，默认30秒
	Concurrency   int    `json:"concurrency"`   // 并发数，默认10
}

// Validate 验证 FingerprintOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *FingerprintOptions) Validate() error {
	if o.Tool != "" && o.Tool != "httpx" && o.Tool != "builtin" {
		return fmt.Errorf("tool must be 'httpx' or 'builtin', got %s", o.Tool)
	}
	if o.ActiveTimeout < 0 {
		return fmt.Errorf("activeTimeout must be non-negative, got %d", o.ActiveTimeout)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.TargetTimeout < 0 {
		return fmt.Errorf("targetTimeout must be non-negative, got %d", o.TargetTimeout)
	}
	if o.Concurrency < 0 {
		return fmt.Errorf("concurrency must be non-negative, got %d", o.Concurrency)
	}
	return nil
}

// Scan 执行指纹识别
func (s *FingerprintScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
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

	opts := &FingerprintOptions{
		Enable:        true,
		Tool:          "httpx", // 默认使用httpx
		IconHash:      true,
		Wappalyzer:    true,
		CustomEngine:  true, // 默认启用自定义指纹引擎
		Screenshot:    false,
		Timeout:       300,
		TargetTimeout: 90,
		Concurrency:   5,
	}
	if config.Options != nil {
		switch v := config.Options.(type) {
		case *FingerprintOptions:
			opts = v
		default:
			if data, err := json.Marshal(config.Options); err == nil {
				if err := json.Unmarshal(data, opts); err != nil {
					taskLog("ERROR", "Fingerprint: failed to unmarshal options: %v", err)
				}
			} else {
				taskLog("ERROR", "Fingerprint: failed to marshal options: %v", err)
			}
		}
		// 反射回退：确保 Cert 字段从 scheduler.FingerprintConfig 正确传播
		if !opts.Cert {
			if rv := reflect.ValueOf(config.Options); rv.Kind() == reflect.Ptr {
				if certField := rv.Elem().FieldByName("Cert"); certField.IsValid() && certField.Kind() == reflect.Bool {
					opts.Cert = certField.Bool()
				}
			}
		}
	}

	// 兼容旧配置：如果Tool为空但Httpx为true，使用httpx
	if opts.Tool == "" {
		if opts.Httpx {
			opts.Tool = "httpx"
		} else {
			opts.Tool = "builtin"
		}
	}

	// 根据工具选择自动设置 Wappalyzer
	// httpx 自带技术检测，builtin 使用 wappalyzergo
	if opts.Tool == "builtin" {
		opts.Wappalyzer = true
	}

	// 设置默认值
	if opts.TargetTimeout <= 0 {
		opts.TargetTimeout = 30
	}

	// 限制最大并发数，避免过度并发
	if opts.Concurrency > 5 {
		opts.Concurrency = 5
	}

	result := &ScanResult{
		MainTaskId: config.MainTaskId,
		Assets:     make([]*Asset, 0),
		Diagnostic: &ScanDiagnostic{Phase: "fingerprint"},
	}

	// The scanner may receive assets carrying findings from a previous scan.
	// Do not clear them until a real response is observed for each asset.
	httpAssets := config.Assets
	resetFingerprintFindingsForScan(httpAssets)
	result.Diagnostic.Coverage.Input = len(httpAssets)
	if len(httpAssets) == 0 {
		result.Diagnostic.Status = PhaseSkippedNotApplicable
		taskLog("INFO", "Fingerprint: no assets to scan, skipping")
		return result, nil
	}

	taskLog("INFO", "Fingerprint: scanning %d assets, tool=%s, timeout %ds/target", len(httpAssets), opts.Tool, opts.TargetTimeout)

	// 根据选择的工具执行扫描
	useHttpx := opts.Tool == "httpx"
	var httpxErr error
	var httpxDiagnostic *ScanDiagnostic
	if useHttpx {
		// 使用 httpx 库进行基础探测，并保留其逐目标诊断；指纹 worker 的回调完成
		// 不能替代真实响应成功，否则全量 timeout/no-output 会被误报为 COMPLETE。
		taskLog("DEBUG", "Using httpx library for fingerprint detection")
		if s.runHttpxWithDiagnostic != nil {
			httpxDiagnostic, httpxErr = s.runHttpxWithDiagnostic(ctx, httpAssets, opts, taskLog)
		} else if s.runHttpx != nil {
			httpxErr = s.runHttpx(ctx, httpAssets, opts, taskLog)
		} else {
			httpxDiagnostic, httpxErr = runHttpxLibWithEventsResult(ctx, httpAssets, opts, taskLog, config.EventLogger)
		}
		mergeFingerprintDiagnostic(result.Diagnostic, httpxDiagnostic)
		for _, asset := range httpAssets {
			if hasSuccessfulHTTPResponseEvidence(asset) {
				// Compatibility seams may populate response fields without going
				// through HttpxScanner.applyHttpxResult.
				markFingerprintFindingsCollected(asset)
			}
		}
		if httpxErr != nil {
			taskLog("ERROR", "httpx probe failed (error_type=%T)", httpxErr)
			if httpxDiagnostic == nil {
				appendWarningCode(result.Diagnostic, ReasonExecutionError)
			}
		}
		// httpx 已完成基础信息采集（title/status/server/faviconHash 等），立即流式入库
		// 避免等待后续 worker pool 的截图/指纹识别完成才入库
		if config.OnAssetUpdated != nil {
			for _, asset := range httpAssets {
				if asset.HttpStatus != "" || asset.Title != "" || asset.Server != "" || asset.IconHash != "" {
					config.OnAssetUpdated(asset)
				}
			}
		}
	} else {
		taskLog("DEBUG", "Using builtin method for fingerprint detection")
	}

	// Screenshot errors are diagnostic-only: they never mutate protocol evidence
	// or any previously collected asset fields.
	recordScreenshotFailure := func(asset *Asset, targetURL string) {
		s.assetMutex.Lock()
		defer s.assetMutex.Unlock()
		if len(result.Diagnostic.Targets) < MaxTargetDiagnostics {
			resolution := resolveAssetScheme(asset)
			result.Diagnostic.Targets = append(result.Diagnostic.Targets, TargetDiagnostic{
				Target: targetURL, Host: asset.Host, Port: asset.Port,
				Outcome: "screenshot_failed", ReasonCode: ReasonScreenshotFailed,
				Metadata: map[string]interface{}{
					"selected_scheme": resolution.Scheme,
					"evidence_kind":   resolution.SelectedEvidence.Kind,
				},
			})
		}
		appendWarningCode(result.Diagnostic, ReasonScreenshotFailed)
	}

	// 记录已处理的资产索引，用于超时时补充未处理的资产
	processedSet := make(map[int]bool)

	// 并发 Worker Pool：每个目标一个指纹识别任务，完成一个补一个
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
	if concurrency > len(httpAssets) {
		concurrency = len(httpAssets)
	}
	taskLog("INFO", "Fingerprint: scanning %d assets with %d workers", len(httpAssets), concurrency)

	type fpResult struct {
		asset *Asset
		index int
	}
	assetChan := make(chan *Asset, len(httpAssets))
	resultChan := make(chan fpResult, len(httpAssets))
	var scanWg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for asset := range assetChan {
				select {
				case <-ctx.Done():
					resultChan <- fpResult{asset: asset}
					return
				default:
				}
				targetCtx, targetCancel := context.WithTimeout(ctx, time.Duration(opts.TargetTimeout)*time.Second)
				if useHttpx && asset.Title != "" && asset.HttpStatus != "" {
					s.runAdditionalFingerprint(targetCtx, asset, opts, taskLog, recordScreenshotFailure)
				} else {
					s.fingerprint(targetCtx, asset, opts, taskLog, recordScreenshotFailure)
				}
				if targetCtx.Err() == context.DeadlineExceeded {
					taskLog("WARN", "Fingerprint: %s timeout", formatAssetTarget(asset))
				}
				targetCancel()
				resultChan <- fpResult{asset: asset}
			}
		}()
	}

dispatch:
	for _, asset := range httpAssets {
		select {
		case <-ctx.Done():
			break dispatch
		case assetChan <- asset:
		}
	}
	close(assetChan)

	go func() {
		scanWg.Wait()
		close(resultChan)
	}()

	completed := 0
	for res := range resultChan {
		completed++
		processedSet[completed] = true
		if res.asset == nil {
			continue
		}
		result.Assets = append(result.Assets, res.asset)
		if config.OnAssetUpdated != nil {
			config.OnAssetUpdated(res.asset)
		}
		if config.OnTargetDone != nil {
			target := fmt.Sprintf("%s:%d", res.asset.Host, res.asset.Port)
			config.OnTargetDone(target, []*Asset{res.asset})
		}
	}

	// Worker completion only means the callback returned. A target is successful
	// for coverage purposes only when a real HTTP response was observed; this
	// prevents httpx timeout/no-output from becoming a false COMPLETE phase.
	succeeded := 0
	for _, asset := range httpAssets {
		if asset != nil && asset.HttpStatus != "" {
			succeeded++
		}
	}
	coverage := &result.Diagnostic.Coverage
	coverage.Attempted = completed
	coverage.Succeeded = succeeded
	coverage.Unconfirmed = len(httpAssets) - succeeded
	if coverage.Unconfirmed < 0 {
		coverage.Unconfirmed = 0
	}
	if completed < len(httpAssets) {
		coverage.Uncovered = len(httpAssets) - completed
	}
	if httpxDiagnostic != nil {
		timeoutCount := httpxDiagnostic.Coverage.TimedOut
		if timeoutCount > coverage.Unconfirmed {
			timeoutCount = coverage.Unconfirmed
		}
		coverage.TimedOut = timeoutCount
		failedCount := httpxDiagnostic.Coverage.Failed
		remaining := coverage.Unconfirmed - coverage.TimedOut
		if failedCount > remaining {
			failedCount = remaining
		}
		coverage.Failed = failedCount
	}
	if httpxErr != nil && coverage.Unconfirmed > 0 && coverage.Failed == 0 && coverage.TimedOut == 0 {
		coverage.Failed = coverage.Unconfirmed
	}
	if coverage.Unconfirmed > 0 {
		appendWarningCode(result.Diagnostic, ReasonUnconfirmed)
	}
	if ctx.Err() != nil {
		result.Diagnostic.Status = PhaseCanceled
	} else if succeeded == len(httpAssets) && completed == len(httpAssets) {
		result.Diagnostic.Status = PhaseComplete
	} else if succeeded > 0 {
		result.Diagnostic.Status = PhasePartial
	} else if coverage.TimedOut > 0 || coverage.Failed > 0 || coverage.Unconfirmed > 0 {
		result.Diagnostic.Status = PhaseFailed
	} else {
		result.Diagnostic.Status = PhaseUncovered
	}

	// 确保所有已扫描的 HTTP 资产 IsHTTP 标记正确
	// 修复：httpx 等工具可能未正确设置 IsHTTP，导致下游模块（JSFinder/DirScan）跳过该资产
	for _, asset := range httpAssets {
		if asset.HttpStatus != "" || asset.Title != "" || asset.Service == "http" || asset.Service == "https" {
			asset.IsHTTP = true
		}
	}

	taskLog("INFO", "Fingerprint: completed passive scan, scanned %d assets", len(httpAssets))

	// 证书抓取（ARL 风格附加功能）：对 HTTPS 资产及 TLS 端口白名单资产抓取 TLS 证书，
	// 采集结果经 worker.handleResult 落入 cert 集合。受 opts.Cert 开关控制，默认关闭。
	if opts.Cert {
		taskLog("DEBUG", "Fingerprint: cert fetch enabled, checking %d assets for cert targets", len(httpAssets))
		// 使用独立上下文避免指纹扫描超时影响证书采集
		certCtx, certCancel := context.WithTimeout(ctx, 120*time.Second)
		certFetched := s.fetchCertsForAssets(certCtx, httpAssets, result, taskLog, config.OnCertFound)
		certCancel()
		if certFetched > 0 {
			taskLog("INFO", "Fingerprint: cert fetch completed, collected %d certificates", certFetched)
		} else {
			taskLog("DEBUG", "Fingerprint: cert fetch completed, no certificates collected (0 cert targets found)")
		}
	} else {
		taskLog("DEBUG", "Fingerprint: cert fetch disabled (cert=false)")
	}

	// 执行主动指纹扫描（如果启用）
	if opts.ActiveScan {
		if s.customFingerprintEngine != nil {
			activeCount := s.customFingerprintEngine.GetActiveFingerprintCount()
			taskLog("INFO", "Active fingerprint scan enabled, engine has %d active fingerprints", activeCount)
			if activeCount > 0 {
				s.RunActiveFingerprint(ctx, httpAssets, opts, taskLog, config.OnAssetUpdated)
			} else {
				taskLog("WARN", "Active fingerprint scan enabled but no active fingerprints loaded")
			}
		} else {
			taskLog("WARN", "Active fingerprint scan enabled but customFingerprintEngine is nil")
		}
	} else {
		taskLog("DEBUG", "Active fingerprint scan not enabled (activeScan=%v)", opts.ActiveScan)
	}

	if config.EventLogger != nil {
		for _, asset := range httpAssets {
			for _, finding := range asset.FingerprintFindings {
				channels := make([]string, 0, len(finding.Evidence))
				for _, evidence := range finding.Evidence {
					channels = append(channels, evidence.Channel)
				}
				config.EventLogger(EventFingerprintDecision, "fingerprint", finding.Decision, map[string]interface{}{
					"host": asset.Host, "port": asset.Port, "fingerprint_id": finding.FingerprintID,
					"source": finding.Source, "confidence": finding.Confidence,
					"evidence_channels": uniqueStrings(channels), "decision": finding.Decision,
					"reason_code": finding.ReasonCode,
				})
			}
		}
	}

	return result, nil
}

// httpServiceConfig HTTP服务检测配置 - 消除特殊情况处理
var (
	defaultHttpServices = map[string]bool{
		"http": true, "https": true, "http-proxy": true,
		"https-alt": true, "http-alt": true, "ajp12": true, "esmagent": true,
	}
	nonHttpServices = map[string]bool{
		"ssh": true, "ftp": true, "smtp": true, "pop3": true, "imap": true,
		"mysql": true, "mssql": true, "oracle": true, "postgresql": true, "redis": true,
		"mongodb": true, "memcached": true, "elasticsearch": true,
		"dns": true, "snmp": true, "ldap": true, "smb": true, "netbios": true,
		"rdp": true, "vnc": true, "telnet": true, "rpc": true,
		"ntp": true, "tftp": true, "sip": true, "rtsp": true,
	}
	commonHttpPorts = map[int]bool{
		80: true, 443: true, 8080: true, 8443: true, 8000: true, 8888: true,
		8081: true, 8082: true, 8083: true, 8084: true, 8085: true,
		9000: true, 9001: true, 9090: true, 9443: true,
		3000: true, 3001: true, 4000: true, 5000: true, 5001: true,
		7001: true, 7002: true, 8180: true,
		8280: true, 8380: true, 8480: true, 8580: true,
		10000: true, 10001: true, 10080: true, 10443: true,
		8800: true, 8880: true, 8881: true, 18080: true, 28080: true,
	}
)

// formatAssetTarget 生成资产探测目标的展示字符串
// 端口为 0 表示仅已知主机名、端口未知（子域名枚举结果），回退到默认 Web 端口 80/443，
// 避免在日志中打印非法的 "host:0"。
func formatAssetTarget(asset *Asset) string {
	if asset.Port == 0 {
		return fmt.Sprintf("%s (port unknown, probe 80/443)", asset.Host)
	}
	return fmt.Sprintf("%s:%d", asset.Host, asset.Port)
}

// IsHttpAsset 判断资产是否为HTTP/HTTPS服务
// 重构：使用策略链模式消除多层if/else
func IsHttpAsset(asset *Asset) bool {
	// 策略链：按优先级依次检查
	checks := []func(*Asset) (isHttp bool, decided bool){
		checkByIsHTTPFlag,
		checkByGlobalChecker,
		checkByNonHttpPorts, // 新增：检查非HTTP端口（优先排除）
		checkByZeroPort,     // 新增：端口未知且无服务标识时不判定为HTTP
		checkByDefaultServices,
		checkByNonHttpServices,
		checkByCommonPorts,
		checkByEmptyService,
	}

	for _, check := range checks {
		if isHttp, decided := check(asset); decided {
			return isHttp
		}
	}
	return false
}

func checkByIsHTTPFlag(asset *Asset) (bool, bool) {
	if asset.IsHTTP {
		return true, true
	}
	return false, false
}

func checkByGlobalChecker(asset *Asset) (bool, bool) {
	if globalHttpServiceChecker == nil {
		return false, false
	}
	service := strings.ToLower(asset.Service)
	if isHttp, found := globalHttpServiceChecker.IsHttpService(service); found {
		return isHttp, true
	}
	return false, false
}

// checkByNonHttpPorts 检查是否在配置的非HTTP端口列表中
func checkByNonHttpPorts(asset *Asset) (bool, bool) {
	if globalHttpServiceChecker == nil {
		return false, false
	}
	if globalHttpServiceChecker.IsNonHttpPort(asset.Port) {
		return false, true // 明确排除
	}
	return false, false
}

func checkByDefaultServices(asset *Asset) (bool, bool) {
	service := strings.ToLower(asset.Service)
	if defaultHttpServices[service] {
		return true, true
	}
	return false, false
}

func checkByNonHttpServices(asset *Asset) (bool, bool) {
	service := strings.ToLower(asset.Service)
	if nonHttpServices[service] {
		return false, true
	}
	return false, false
}

func checkByCommonPorts(asset *Asset) (bool, bool) {
	if commonHttpPorts[asset.Port] {
		return true, true
	}
	return false, false
}

func checkByZeroPort(asset *Asset) (bool, bool) {
	if asset.Port == 0 && strings.ToLower(asset.Service) == "" {
		return false, true // 端口未知且无服务标识，不判定为HTTP
	}
	return false, false
}

func checkByEmptyService(asset *Asset) (bool, bool) {
	if strings.ToLower(asset.Service) == "" {
		return true, true // 空服务名，让fingerprint函数去实际探测
	}

	// 如果全局检查器存在，检查服务名是否在配置中
	// 对于不在配置中的未知服务（如cbt），也尝试HTTP探测
	if globalHttpServiceChecker != nil {
		if _, found := globalHttpServiceChecker.IsHttpService(strings.ToLower(asset.Service)); !found {
			// 服务名不在配置中，尝试HTTP探测
			return true, true
		}
	}

	return false, false
}

// HttpServiceChecker HTTP服务检查器接口
type HttpServiceChecker interface {
	IsHttpService(serviceName string) (isHttp bool, found bool)
	IsHttpPort(port int) bool
	IsNonHttpPort(port int) bool
	CheckIsHttp(serviceName string, port int) bool
}

// 全局HTTP服务检查器
var globalHttpServiceChecker HttpServiceChecker

// SetHttpServiceChecker 设置全局HTTP服务检查器
func SetHttpServiceChecker(checker HttpServiceChecker) {
	globalHttpServiceChecker = checker
}

// GetHttpServiceChecker 获取全局HTTP服务检查器
func GetHttpServiceChecker() HttpServiceChecker {
	return globalHttpServiceChecker
}

// buildTargetURL keeps the legacy call shape while routing the decision through
// ResolveScheme. Asset-aware callers use buildAssetTargetURL so successful
// response evidence outranks service metadata and port hints.
func buildTargetURL(service, host string, port int) string {
	return buildAssetTargetURL(&Asset{Service: service, Host: host, Port: port})
}

func buildAssetTargetURL(asset *Asset) string {
	if asset == nil {
		return ""
	}
	resolution := resolveAssetScheme(asset)
	scheme := SchemeHTTP
	if resolution.HasEvidence {
		scheme = resolution.Scheme
	}
	if asset.Port <= 0 {
		return scheme + "://" + asset.Host
	}
	return scheme + "://" + net.JoinHostPort(asset.Host, fmt.Sprintf("%d", asset.Port))
}

// runAdditionalFingerprint 执行额外的指纹识别功能（httpx已执行后）
func (s *FingerprintScanner) runAdditionalFingerprint(ctx context.Context, asset *Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{}), onScreenshotFailure func(*Asset, string)) {
	if hasFingerprintResponseEvidence(asset) {
		markFingerprintFindingsCollected(asset)
	}
	targetUrl := buildAssetTargetURL(asset)

	// 解析HTTP headers用于指纹识别
	var headers http.Header
	if asset.HttpHeader != "" {
		headers = parseHttpHeaders(asset.HttpHeader)
	}

	// httpx 正常情况下已获取 body/title；若为空（重定向丢失、超时截断等异常），使用内置客户端兜底
	var bodyBytes []byte
	if asset.HttpBody == "" || asset.Title == "" {
		taskLog("DEBUG", "httpx body/title empty for %s:%d, falling back to builtin client", asset.Host, asset.Port)
		resp, err := s.client.Get(targetUrl)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
			bodyBytes = body
			ct := resp.Header.Get("Content-Type")
			if asset.HttpBody == "" {
				if len(body) > 50*1024 {
					asset.HttpBody = ToUTF8(body[:50*1024], ct) + "\n...[truncated]"
				} else {
					asset.HttpBody = ToUTF8(body, ct)
				}
			}
			if asset.Title == "" {
				asset.Title = extractTitle(ToUTF8(body, ct))
			}
			if asset.Server == "" {
				asset.Server = resp.Header.Get("Server")
			}
			if headers == nil {
				headers = resp.Header
			}
			// 更新状态码并持久化实际成功响应的 scheme；重定向后的
			// Request.URL 是后续 favicon/截图应使用的协议证据。
			asset.HttpStatus = fmt.Sprintf("%d", resp.StatusCode)
			observedScheme := ""
			if resp.Request != nil && resp.Request.URL != nil {
				observedScheme = resp.Request.URL.Scheme
			}
			if observedScheme == "" {
				observedScheme = resolveAssetScheme(asset).Scheme
			}
			persistSuccessfulScheme(asset, observedScheme, resp.StatusCode)
			targetUrl = buildAssetTargetURL(asset)
			taskLog("DEBUG", "[Fingerprint] builtin HTTP %s: status=%d server=%q title=%q bodyLen=%d",
				targetUrl, resp.StatusCode, asset.Server, asset.Title, len(body))
			// 更新HttpHeader为实际响应的header
			if asset.HttpHeader == "" {
				asset.HttpHeader = formatHeadersWithStatus(resp.Header, resp.StatusCode, resp.Proto)
			}
		}
	} else {
		bodyBytes = []byte(asset.HttpBody)
	}

	// 收集所有指纹识别结果，用于智能合并
	appResults := make(map[string]*AppDetectionResult)

	// 记录 httpx 残留的应用检测结果
	mergeExistingAppDetections(appResults, asset.App)
	if len(asset.App) > 0 {
		taskLog("DEBUG", "[Fingerprint] httpx residual apps for %s:%d: %v", asset.Host, asset.Port, asset.App)
	}

	// 如果启用Wappalyzer，进行检测（httpx模式下通常不需要，但保留兼容性）
	if opts.Wappalyzer && s.wappalyzerClient != nil {
		apps := s.wappalyzerClient.Fingerprint(headers, []byte(asset.HttpBody))
		taskLog("DEBUG", "Wappalyzer detected apps for %s:%d: %v", asset.Host, asset.Port, apps)

		for app := range apps {
			appNameLower := strings.ToLower(app)
			if result, exists := appResults[appNameLower]; exists {
				result.Sources = append(result.Sources, "wappalyzer")
			} else {
				appResults[appNameLower] = &AppDetectionResult{
					Name:         app,
					OriginalName: app,
					Sources:      []string{"wappalyzer"},
				}
			}
			taskLog("INFO", "发现应用指纹: %s:%d -> %s (来源: wappalyzer)", asset.Host, asset.Port, app)
		}
	}

	// 获取 IconHash 和 MMH3 hash（用于自定义指纹匹配）
	var faviconMMH3Hash string
	if opts.IconHash || opts.CustomEngine {
		// 保存 httpx 可能已获取的 IconData 作为回退
		existingIconData := asset.IconData

		// 传入 HTML body 用于解析 <link rel="icon"> 标签发现自定义favicon路径
		iconHash, iconData := s.getIconHashWithData(targetUrl, asset.HttpBody)
		if len(iconData) > 0 {
			// 内置获取成功，使用内置数据
			asset.IconData = iconData
			faviconMMH3Hash = CalculateMMH3Hash(iconData)
			if asset.IconHash == "" && iconHash != "" {
				asset.IconHash = iconHash
			}
		} else if len(existingIconData) > 0 {
			// 内置获取失败，回退到 httpx 已获取的数据
			faviconMMH3Hash = CalculateMMH3Hash(existingIconData)
		}
	}

	// 如果启用自定义指纹引擎，使用自定义格式的规则进行识别
	if opts.CustomEngine && s.customFingerprintEngine != nil {
		fpCount := s.customFingerprintEngine.GetFingerprintCount()
		// 使用原始字节数据进行GBK编码匹配
		if len(bodyBytes) == 0 {
			bodyBytes = []byte(asset.HttpBody)
		}
		// 从header字符串中提取所有Set-Cookie值
		var cookies string
		if headers != nil {
			cookies = headers.Get("Set-Cookie")
			if cookies == "" {
				cookies = headers.Get("set-cookie")
			}
		}
		if cookies == "" && asset.HttpHeader != "" {
			cookies = extractCookiesFromHeader(asset.HttpHeader)
		}
		fpData := &FingerprintData{
			Title:           asset.Title,
			Body:            asset.HttpBody,
			BodyBytes:       bodyBytes,
			Headers:         headers,
			HeaderString:    asset.HttpHeader,
			Server:          asset.Server,
			URL:             targetUrl,
			FaviconHash:     faviconMMH3Hash,
			Cookies:         cookies,
			ResponseMissing: asset.HttpStatus == "" && asset.Title == "" && asset.HttpBody == "" && asset.HttpHeader == "" && asset.Server == "" && asset.IconHash == "",
			BodyTruncated:   strings.Contains(strings.ToLower(asset.HttpBody), "[truncated]"),
			HeaderTruncated: strings.Contains(strings.ToLower(asset.HttpHeader), "[truncated]"),
		}
		taskLog("DEBUG", "[Fingerprint] custom engine input: host=%s:%d url=%s title=%q server=%q bodyLen=%d faviconHash=%s cookies=%q",
			asset.Host, asset.Port, targetUrl, asset.Title, asset.Server, len(asset.HttpBody), faviconMMH3Hash, cookies)
		findings := s.customFingerprintEngine.MatchWithEvidence(fpData)
		taskLog("DEBUG", "Custom fingerprint engine (loaded %d fingerprints) evaluated findings for %s:%d: %v", fpCount, asset.Host, asset.Port, findings)
		applyGovernedFingerprintFindings(asset, appResults, findings, taskLog)
	}

	// 重新构建asset.App列表，使用智能合并的结果
	asset.App = make([]string, 0, len(appResults))
	for _, result := range appResults {
		formattedApp := formatAppWithSources(result)
		asset.App = append(asset.App, formattedApp)
	}
	taskLog("DEBUG", "[Fingerprint] final apps for %s:%d: %v (wappalyzer+custom+httpx merged)", asset.Host, asset.Port, asset.App)

	// 截图功能：如果 httpx 没有获取到截图，使用内置方法补充
	// 修复 D6：port 未知(==0)时跳过截图，避免对默认端口做无意义 Chrome 启动
	if opts.Screenshot && asset.Screenshot == "" && asset.Port > 0 {
		// 截图独立预算：不依赖 httpx/指纹共用的 ctx，避免被上游模块耗尽预算而饿死
		shotCtx, shotCancel := screenshotContext(ctx, opts)
		screenshot := s.captureScreenshot(shotCtx, targetUrl, taskLog)
		shotCancel()
		if screenshot != "" {
			asset.Screenshot = screenshot
			taskLog("DEBUG", "Screenshot captured for %s:%d using builtin method", asset.Host, asset.Port)
		} else if onScreenshotFailure != nil {
			onScreenshotFailure(asset, targetUrl)
		}
	}
}

// FingerprintScanner 的证书抓取能力在 fetchCertsForAssets 中实现，
// 对 HTTPS 资产及 TLS 端口白名单资产抓取 TLS 证书，结果汇总到 ScanResult.CertResults。
// 证书抓取作为指纹识别的附加功能（ARL 风格），由 FingerprintOptions.Cert 控制，默认关闭。

// fetchCertsForAssets 对资产列表中符合证书抓取条件的资产执行 TLS 握手并解析证书。
// 返回成功采集的证书数量；失败的目标静默跳过（不影响整体指纹识别）。
func (s *FingerprintScanner) fetchCertsForAssets(ctx context.Context, assets []*Asset, result *ScanResult, taskLog func(level, format string, args ...interface{}), onCertFound func(*CertResult)) int {
	certTargetCount := 0
	for _, a := range assets {
		if a == nil || !isCertFetchTarget(a) {
			continue
		}
		certTargetCount++
	}
	if taskLog != nil {
		taskLog("DEBUG", "Fingerprint: cert fetch checking %d/%d assets for TLS certs", certTargetCount, len(assets))
	}

	count := 0
	for _, a := range assets {
		if a == nil || !isCertFetchTarget(a) {
			continue
		}
		select {
		case <-ctx.Done():
			if taskLog != nil {
				taskLog("WARN", "Fingerprint cert fetch interrupted at %s:%d", a.Host, a.Port)
			}
			return count
		default:
		}
		if cr := s.fetchCertificate(ctx, a.Host, a.Port, 10*time.Second); cr != nil {
			result.CertResults = append(result.CertResults, cr)
			count++
			// 流式入库：单证书采集完成立即回调
			if onCertFound != nil {
				onCertFound(cr)
			}
		}
	}
	if taskLog != nil && certTargetCount > 0 {
		taskLog("DEBUG", "Fingerprint: cert fetch done, %d certs collected from %d targets", count, certTargetCount)
	}
	return count
}

// getIconHashWithData 获取favicon的hash值和原始数据
// htmlBody: 可选的HTML body内容，用于解析 <link rel="icon"> 标签发现自定义favicon路径
func (s *FingerprintScanner) getIconHashWithData(baseUrl string, htmlBody string) (string, []byte) {
	// 构建favicon候选路径列表
	faviconPaths := []string{}

	// 1. 先从HTML中解析 <link rel="icon"> 标签获取自定义favicon路径
	if htmlBody != "" {
		parsedPaths := parseFaviconFromHTML(htmlBody, baseUrl)
		faviconPaths = append(faviconPaths, parsedPaths...)
	}

	// 2. 追加常见的favicon路径作为回退
	faviconPaths = append(faviconPaths,
		"/favicon.ico",
		"/favicon.png",
		"/static/favicon.ico",
		"/assets/favicon.ico",
	)

	for _, path := range faviconPaths {
		match, hash, data := func(p string) (bool, string, []byte) {
			var iconUrl string
			if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
				// 已经是完整URL
				iconUrl = p
			} else {
				iconUrl = baseUrl + p
			}

			resp, err := s.client.Get(iconUrl)
			if err != nil {
				return false, "", nil
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return false, "", nil
			}

			iconData, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

			if readErr != nil || len(iconData) == 0 {
				return false, "", nil
			}

			// 验证是否为有效的图片数据（过滤HTML错误页等非图片响应）
			if !isImageData(iconData) {
				return false, "", nil
			}

			// 计算MMH3 hash（Shodan风格）- 与 asset.IconHash 和指纹匹配使用的格式一致
			return true, CalculateMMH3Hash(iconData), iconData
		}(path)

		if match {
			return hash, data
		}
	}

	return "", nil
}

// parseFaviconFromHTML 从HTML内容中解析favicon路径
// 支持 <link rel="icon" href="..."> 和 <link rel="shortcut icon" href="...">
func parseFaviconFromHTML(htmlBody string, _ string) []string {
	var paths []string
	seen := make(map[string]bool)

	// 匹配 <link> 标签中包含 rel="icon" 或 rel="shortcut icon" 的 href
	// 支持单引号和双引号，支持属性顺序不同
	linkRe := regexp.MustCompile(`(?i)<link[^>]*\brel\s*=\s*["'](?:shortcut\s+)?icon["'][^>]*\bhref\s*=\s*["']([^"']+)["'][^>]*/?>`)
	matches := linkRe.FindAllStringSubmatch(htmlBody, -1)

	// 也匹配 href 在 rel 之前的情况
	linkRe2 := regexp.MustCompile(`(?i)<link[^>]*\bhref\s*=\s*["']([^"']+)["'][^>]*\brel\s*=\s*["'](?:shortcut\s+)?icon["'][^>]*/?>`)
	matches = append(matches, linkRe2.FindAllStringSubmatch(htmlBody, -1)...)

	// 也匹配 apple-touch-icon
	linkRe3 := regexp.MustCompile(`(?i)<link[^>]*\brel\s*=\s*["']apple-touch-icon(?:-precomposed)?["'][^>]*\bhref\s*=\s*["']([^"']+)["'][^>]*/?>`)
	matches = append(matches, linkRe3.FindAllStringSubmatch(htmlBody, -1)...)

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		href := strings.TrimSpace(m[1])
		if href == "" || seen[href] {
			continue
		}
		seen[href] = true

		// 处理相对路径
		if strings.HasPrefix(href, "//") {
			// 协议相对URL
			href = "https:" + href
		} else if strings.HasPrefix(href, "/") {
			// 绝对路径 - 将在调用方拼接baseUrl
			// 保持原样，调用方会处理
		} else if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			// 相对路径
			href = "/" + href
		}

		paths = append(paths, href)
	}

	return paths
}

// fingerprint 识别单个资产指纹
func (s *FingerprintScanner) fingerprint(ctx context.Context, asset *Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{}), onScreenshotFailure func(*Asset, string)) {
	// 检查上下文是否已取消
	if ctx.Err() != nil {
		return
	}

	// Probe order is derived from the same evidence resolver used by httpx and
	// downstream consumers. The alternate protocol is still attempted when the
	// selected hint fails.
	schemes := []string{SchemeHTTP, SchemeHTTPS}
	if resolution := resolveAssetScheme(asset); resolution.HasEvidence && resolution.Scheme == SchemeHTTPS {
		schemes = []string{SchemeHTTPS, SchemeHTTP}
	}

	var httpDetected bool
	for _, scheme := range schemes {
		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return
		}

		targetUrl := buildTargetURL(scheme, asset.Host, asset.Port)
		resp, err := s.client.Get(targetUrl)
		if err != nil {
			continue
		}

		// 验证是否为有效的HTTP响应
		if !isValidHttpResponse(resp) {
			resp.Body.Close()
			continue
		}

		httpDetected = true

		// 读取响应体（保留原始字节用于GBK编码匹配）
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 限制1MB
		resp.Body.Close()

		// 提取信息
		asset.HttpStatus = fmt.Sprintf("%d", resp.StatusCode)
		asset.HttpHeader = formatHeadersWithStatus(resp.Header, resp.StatusCode, resp.Proto)
		// 限制HttpBody大小为50KB
		if len(body) > 50*1024 {
			asset.HttpBody = string(body[:50*1024]) + "\n...[truncated]"
		} else {
			asset.HttpBody = string(body)
		}
		asset.Title = extractTitle(string(body))
		asset.Server = resp.Header.Get("Server")
		observedScheme := scheme
		if resp.Request != nil && resp.Request.URL != nil && normalizeScheme(resp.Request.URL.Scheme) != "" {
			observedScheme = resp.Request.URL.Scheme
		}
		persistSuccessfulScheme(asset, observedScheme, resp.StatusCode)
		targetUrl = buildAssetTargetURL(asset)

		// 获取Icon Hash和原始数据
		var faviconMMH3Hash string
		if opts.IconHash {
			iconHash, iconData := s.getIconHashWithData(targetUrl, asset.HttpBody)
			if iconHash != "" {
				asset.IconHash = iconHash // getIconHashWithData 返回 MMH3 hash
			}
			// 计算MMH3 hash用于ARL格式指纹匹配
			if len(iconData) > 0 {
				faviconMMH3Hash = CalculateMMH3Hash(iconData)
				// 保存 icon 图片数据
				asset.IconData = iconData
			}
		}

		// 使用wappalyzergo识别应用指纹
		var appResults = make(map[string]*AppDetectionResult)
		if opts.Wappalyzer && s.wappalyzerClient != nil {
			apps := s.identifyWithWappalyzer(resp.Header, body)
			for _, app := range apps {
				appNameLower := strings.ToLower(app)
				appResults[appNameLower] = &AppDetectionResult{
					Name:         app,
					OriginalName: app,
					Sources:      []string{"wappalyzer"},
				}
			}
		}

		// 使用自定义指纹引擎
		if opts.CustomEngine && s.customFingerprintEngine != nil {
			fpCount := s.customFingerprintEngine.GetFingerprintCount()
			fpData := &FingerprintData{
				Title:         asset.Title,
				Body:          asset.HttpBody,
				BodyBytes:     body, // 原始字节用于GBK编码匹配
				Headers:       resp.Header,
				HeaderString:  asset.HttpHeader, // 原始header字符串
				Server:        asset.Server,
				URL:           targetUrl,
				FaviconHash:   faviconMMH3Hash,
				Cookies:       resp.Header.Get("Set-Cookie"),
				BodyTruncated: len(body) > 50*1024 || strings.Contains(strings.ToLower(asset.HttpBody), "[truncated]"),
			}
			findings := s.customFingerprintEngine.MatchWithEvidence(fpData)
			taskLog("DEBUG", "Custom fingerprint engine (loaded %d fingerprints) evaluated findings for %s:%d: %v", fpCount, asset.Host, asset.Port, findings)
			applyGovernedFingerprintFindings(asset, appResults, findings, taskLog)
		}

		// 构建最终的应用列表
		for _, result := range appResults {
			formattedApp := formatAppWithSources(result)
			asset.App = append(asset.App, formattedApp)
		}

		// 截图
		// 修复 D6：port 未知(==0)时跳过截图
		if opts.Screenshot && asset.Port > 0 {
			// 截图独立预算：不依赖指纹探测共用的 ctx，避免被上游模块耗尽预算而饿死
			shotCtx, shotCancel := screenshotContext(ctx, opts)
			screenshot := s.captureScreenshot(shotCtx, targetUrl, taskLog)
			shotCancel()
			taskLog("INFO", "takeScreenshot截图: targetUrl:%s ->screenshot)", targetUrl)
			if screenshot != "" {
				asset.Screenshot = screenshot
			} else if onScreenshotFailure != nil {
				onScreenshotFailure(asset, targetUrl)
			}
		}

		break
	}

	// 如果HTTP探测失败，标记为非HTTP服务
	if !httpDetected && asset.Service == "" {
		taskLog("DEBUG", "HTTP probe failed for %s:%d, marking as non-http", asset.Host, asset.Port)
		asset.Service = "unknown"
	}
}

// isValidHttpResponse 验证响应是否为有效的HTTP响应
func isValidHttpResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}

	// 检查状态码是否在有效范围内
	if resp.StatusCode < 100 || resp.StatusCode >= 600 {
		return false
	}

	// 检查是否有HTTP特征的响应头
	// 有效的HTTP服务通常会返回以下头之一
	httpHeaders := []string{
		"Content-Type",
		"Server",
		"Date",
		"Content-Length",
		"Transfer-Encoding",
		"Connection",
		"Set-Cookie",
		"X-Powered-By",
	}

	for _, header := range httpHeaders {
		if resp.Header.Get(header) != "" {
			return true
		}
	}

	// 如果状态码是常见的HTTP状态码，也认为是有效的
	validStatusCodes := map[int]bool{
		200: true, 201: true, 204: true, 206: true,
		301: true, 302: true, 303: true, 304: true, 307: true, 308: true,
		400: true, 401: true, 403: true, 404: true, 405: true, 500: true, 502: true, 503: true,
	}

	return validStatusCodes[resp.StatusCode]
}

// checkHttpxInstalled 检查httpx是否安装
func checkHttpxInstalled() bool {
	cmd := exec.Command("httpx", "-version")
	output, _ := cmd.CombinedOutput()
	return strings.Contains(string(output), "Version")
}

// identifyWithWappalyzer 使用wappalyzergo识别应用
func (s *FingerprintScanner) identifyWithWappalyzer(headers http.Header, body []byte) []string {
	if s.wappalyzerClient == nil {
		return nil
	}

	fingerprints := s.wappalyzerClient.Fingerprint(headers, body)
	apps := make([]string, 0, len(fingerprints))
	for app := range fingerprints {
		apps = append(apps, app)
	}
	return apps
}

// getIconHash 获取favicon的hash值
func (s *FingerprintScanner) getIconHash(baseUrl string) string {
	// 尝试常见的favicon路径
	faviconPaths := []string{
		"/favicon.ico",
		"/favicon.png",
		"/static/favicon.ico",
		"/assets/favicon.ico",
	}

	for _, path := range faviconPaths {
		match, hash := func(p string) (bool, string) {
			iconUrl := baseUrl + p
			resp, err := s.client.Get(iconUrl)
			if err != nil {
				return false, ""
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return false, ""
			}

			// 读取icon内容
			iconData, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

			if err != nil || len(iconData) == 0 {
				return false, ""
			}

			// 计算MMH3 hash (Shodan风格)
			return true, CalculateMMH3Hash(iconData)
		}(path)

		if match {
			return hash
		}
	}

	return ""
}

// screenshotContext 为单次截图派生独立上下文预算，与 httpx/指纹共用的 ctx 解耦，
// 避免 httpx 耗尽共享 ctx 预算后截图被静默跳过（饿死）。
// 注意：外层 caller 必须同时传入扫描主 ctx，以便 Worker 停止时能快速中断在途截图，
// 防止截图 goroutine 阻塞 Worker 退出或产生长期 renderer 进程。
func screenshotContext(parentCtx context.Context, opts *FingerprintOptions) (context.Context, context.CancelFunc) {
	budget := 60
	if opts != nil && opts.TargetTimeout > budget {
		budget = opts.TargetTimeout
	}
	// 以扫描主 ctx 为祖先，保证 Worker 取消时截图也能终止
	return context.WithTimeout(parentCtx, time.Duration(budget)*time.Second)
}

// takeScreenshot 使用chromedp截图（共享浏览器 + Tab 模式）
// 全局只维护 1 个 Chrome 进程，每次截图创建新 Tab，完成后或超时时关闭 Tab。
// 取消 Tab 上下文是安全的（仅关闭标签页），不会触发 chromedp 的 close-of-closed-channel panic。
// 该 panic 仅在取消 Allocator 分配中的浏览器上下文时发生（Allocation 竞态）。
func (s *FingerprintScanner) takeScreenshot(ctx context.Context, targetUrl string, taskLog func(level, format string, args ...interface{})) (result string) {
	// taskLog 默认化前置：下方 ctx 过期日志依赖它，必须先就绪
	if taskLog == nil {
		taskLog = func(level, format string, args ...interface{}) {
			logx.Infof(format, args...)
		}
	}

	// 截图 ctx 现已独立派生（screenshotContext，不再共享 httpx/指纹预算），
	// 此处 ctx.Err() 仅发生于独立预算耗尽或显式取消，仍显式记录跳过原因便于排障。
	if ctx.Err() != nil {
		taskLog("DEBUG", "[Chromedp] skip screenshot for %s: context expired (%v)", targetUrl, ctx.Err())
		return ""
	}

	defer func() {
		if r := recover(); r != nil {
			taskLog("ERROR", "takeScreenshot panic recovered for %s: %v", targetUrl, r)
			result = ""
		}
	}()

	select {
	case chromedpSemaphore <- struct{}{}:
		defer func() { <-chromedpSemaphore }()
	case <-ctx.Done():
		taskLog("DEBUG", "[Chromedp] skip screenshot for %s: context expired while waiting for semaphore (%v)", targetUrl, ctx.Err())
		return ""
	}

	browserCtx, err := getGlobalBrowser(taskLog)
	if err != nil {
		taskLog("ERROR", "[Chromedp] Failed to get global browser: %v, skipping screenshot for %s", err, targetUrl)
		return ""
	}

	// 从共享浏览器派生 Tab 上下文
	// 注意：chromedp.WithErrorf 是 BrowserOption，不能在 Tab 派生时使用，
	// 否则触发 "WithBrowserOption can only be used when allocating a new browser" panic
	taskCtx, taskCancel := chromedp.NewContext(browserCtx)
	defer taskCancel()

	type screenshotResult struct {
		data string
		err  error
	}
	ch := make(chan screenshotResult, 1)

	go func() {
		defer taskCancel()

		var buf []byte
		var pageHeight int64

		err := chromedp.Run(taskCtx,
			chromedp.Navigate(targetUrl),
			chromedp.WaitReady("body", chromedp.ByQuery),
			chromedp.ActionFunc(func(ctx context.Context) error {
				time.Sleep(3 * time.Second)
				return nil
			}),
			chromedp.Evaluate(`document.body.scrollHeight`, &pageHeight),
			chromedp.ActionFunc(func(ctx context.Context) error {
				if pageHeight < 1080 {
					pageHeight = 1080
				}
				return chromedp.EmulateViewport(1920, pageHeight).Do(ctx)
			}),
			chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
			chromedp.Sleep(2*time.Second),
			chromedp.FullScreenshot(&buf, 90),
		)

		if err != nil {
			ch <- screenshotResult{"", err}
			return
		}

		if len(buf) > 0 {
			ch <- screenshotResult{base64.StdEncoding.EncodeToString(buf), nil}
		} else {
			ch <- screenshotResult{"", nil}
		}
	}()

	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()

	select {
	case r := <-ch:
		if r.err != nil {
			taskLog("ERROR", "Screenshot failed for %s: %v", targetUrl, r.err)
			return ""
		}
		if r.data != "" {
			taskLog("INFO", "完成使用chromedp截图: %s", targetUrl)
		}
		return r.data
	case <-timer.C:
		taskLog("ERROR", "Screenshot timeout for %s (60s), closing tab", targetUrl)
		// 取消 Tab 上下文，安全关闭标签页，Chrome 进程不受影响
		taskCancel()
		return ""
	case <-ctx.Done():
		// 父 context 取消：DeadlineExceeded=预算耗尽（httpx 占满 fpCtx），Canceled=任务停止
		taskLog("DEBUG", "[Chromedp] abort screenshot for %s mid-run: parent context done (%v)", targetUrl, ctx.Err())
		taskCancel()
		return ""
	}
}

// extractTitle 提取网页标题
func extractTitle(body string) string {
	re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	matches := re.FindStringSubmatch(body)
	if len(matches) > 1 {
		title := strings.TrimSpace(matches[1])
		// 限制长度
		if len(title) > 100 {
			title = title[:100]
		}
		return title
	}
	return ""
}

// formatHeaders 格式化响应头
func formatHeaders(headers http.Header) string {
	var sb strings.Builder
	for key, values := range headers {
		for _, value := range values {
			sb.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}
	return sb.String()
}

// formatHeadersWithStatus 格式化响应头，包含HTTP状态行
func formatHeadersWithStatus(headers http.Header, statusCode int, proto string) string {
	var sb strings.Builder
	// 添加HTTP状态行
	if proto == "" {
		proto = "HTTP/1.1"
	}
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = "Unknown"
	}
	sb.WriteString(fmt.Sprintf("%s %d %s\n", proto, statusCode, statusText))
	// 添加headers
	for key, values := range headers {
		for _, value := range values {
			sb.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}
	return sb.String()
}

// parseHttpHeaders 解析HTTP headers字符串为http.Header
func parseHttpHeaders(headerStr string) http.Header {
	headers := make(http.Header)
	lines := strings.Split(headerStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			headers.Add(key, value)
		}
	}
	return headers
}

// extractCookiesFromHeader 从header字符串中提取所有Set-Cookie值
func extractCookiesFromHeader(headerStr string) string {
	var cookies []string
	lines := strings.Split(headerStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 不区分大小写匹配 Set-Cookie, set-cookie, set_cookie
		lowerLine := strings.ToLower(line)
		if strings.HasPrefix(lowerLine, "set-cookie:") || strings.HasPrefix(lowerLine, "set_cookie:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				cookies = append(cookies, strings.TrimSpace(parts[1]))
			}
		}
	}
	return strings.Join(cookies, "; ")
}

// containsAppName 检查应用列表中是否包含指定应用名（忽略来源标识）
func containsAppName(apps []string, appName string) bool {
	appNameLower := strings.ToLower(appName)
	for _, app := range apps {
		// 移除来源标识后比较
		name := app
		if idx := strings.Index(app, "["); idx > 0 {
			name = app[:idx]
		}
		if strings.ToLower(name) == appNameLower {
			return true
		}
	}
	return false
}

// extractAppName 从应用字符串中提取应用名称（移除来源标识）
func extractAppName(app string) string {
	if idx := strings.Index(app, "["); idx > 0 {
		return strings.TrimSpace(app[:idx])
	}
	return app
}

func mergeExistingAppDetections(appResults map[string]*AppDetectionResult, apps []string) {
	for _, app := range apps {
		result := parseAppDetection(app)
		if result == nil {
			continue
		}
		mergeAppDetectionResult(appResults, result)
	}
}

func parseAppDetection(app string) *AppDetectionResult {
	app = strings.TrimSpace(app)
	if app == "" {
		return nil
	}
	name := extractAppName(app)
	if colonIdx := strings.Index(name, ":"); colonIdx > 0 {
		name = name[:colonIdx]
	}
	result := &AppDetectionResult{Name: name, OriginalName: name}
	left := strings.LastIndex(app, "[")
	right := strings.LastIndex(app, "]")
	if left < 0 || right <= left {
		result.Sources = []string{"httpx"}
		return result
	}
	for _, sourcePart := range strings.Split(app[left+1:right], "+") {
		source, id := parseSourcePart(sourcePart)
		if source == "" {
			continue
		}
		result.Sources = append(result.Sources, source)
		switch source {
		case "custom":
			if id != "" {
				result.CustomIDs = append(result.CustomIDs, id)
			}
		case "active":
			if id != "" {
				result.ActiveIDs = append(result.ActiveIDs, id)
			}
		}
	}
	return result
}

func parseSourcePart(part string) (string, string) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", ""
	}
	if open := strings.Index(part, "("); open > 0 && strings.HasSuffix(part, ")") {
		return part[:open], part[open+1 : len(part)-1]
	}
	return part, ""
}

func mergeAppDetectionResult(appResults map[string]*AppDetectionResult, incoming *AppDetectionResult) {
	key := strings.ToLower(incoming.Name)
	if existing, ok := appResults[key]; ok {
		existing.Sources = append(existing.Sources, incoming.Sources...)
		existing.CustomIDs = append(existing.CustomIDs, incoming.CustomIDs...)
		existing.ActiveIDs = append(existing.ActiveIDs, incoming.ActiveIDs...)
		if existing.OriginalName == "" {
			existing.OriginalName = incoming.OriginalName
		}
		return
	}
	appResults[key] = incoming
}

func mergeFingerprintDetection(appResults map[string]*AppDetectionResult, matched MatchedFingerprint) {
	source := matched.Source
	if source == "" {
		source = "custom"
	}
	incoming := &AppDetectionResult{
		Name:         matched.Name,
		OriginalName: matched.Name,
		Sources:      []string{source},
		CustomIDs:    sourceIDs(matched, "custom"),
		ActiveIDs:    sourceIDs(matched, "active"),
	}
	mergeAppDetectionResult(appResults, incoming)
}

func sourceIDs(matched MatchedFingerprint, source string) []string {
	if matched.Source != source || matched.Id == "" {
		return nil
	}
	return []string{matched.Id}
}

// formatAppWithSources 根据检测来源格式化应用名称
func formatAppWithSources(result *AppDetectionResult) string {
	if len(result.Sources) == 0 {
		return result.OriginalName
	}

	// 使用原始名称（可能包含版本号）
	appName := result.OriginalName
	if appName == "" {
		appName = result.Name
	}

	// 如果应用名称仍为空，跳过
	if appName == "" {
		return ""
	}

	// 移除现有的来源标识
	if idx := strings.Index(appName, "["); idx > 0 {
		appName = strings.TrimSpace(appName[:idx])
	}

	// 去重并排序来源
	sources := utils.UniqueStrings(result.Sources)

	orderedSources := orderFingerprintSources(sources)
	sourceStr := fmt.Sprintf("[%s]", strings.Join(formatFingerprintSources(orderedSources, result), "+"))

	return appName + sourceStr
}

func orderFingerprintSources(sources []string) []string {
	orderedSources := make([]string, 0, len(sources))
	for _, source := range []string{"httpx", "wappalyzer", "custom", "active"} {
		for _, s := range sources {
			if s == source {
				orderedSources = append(orderedSources, s)
				break
			}
		}
	}
	return orderedSources
}

func formatFingerprintSources(sources []string, result *AppDetectionResult) []string {
	formatted := make([]string, 0, len(sources))
	for _, source := range sources {
		switch source {
		case "custom":
			formatted = append(formatted, formatSourceWithIDs("custom", result.CustomIDs))
		case "active":
			formatted = append(formatted, formatSourceWithIDs("active", result.ActiveIDs))
		default:
			formatted = append(formatted, source)
		}
	}
	return formatted
}

func formatSourceWithIDs(source string, ids []string) string {
	ids = utils.UniqueStrings(ids)
	if len(ids) == 0 {
		return source
	}
	return fmt.Sprintf("%s(%s)", source, strings.Join(ids, ","))
}

// containsString 检查字符串切片是否包含指定字符串
func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func mergeActiveFingerprintApp(asset *Asset, fp *model.Fingerprint) bool {
	previousApps := make(map[string]struct{}, len(asset.App))
	for _, app := range asset.App {
		previousApps[app] = struct{}{}
	}

	appResults := make(map[string]*AppDetectionResult)
	mergeExistingAppDetections(appResults, asset.App)
	mergeAppDetectionResult(appResults, &AppDetectionResult{
		Name:         fp.Name,
		OriginalName: fp.Name,
		Sources:      []string{"active"},
		ActiveIDs:    []string{fp.Id.Hex()},
	})
	asset.App = make([]string, 0, len(appResults))
	for _, result := range appResults {
		asset.App = append(asset.App, formatAppWithSources(result))
	}

	currentApps := make(map[string]struct{}, len(asset.App))
	for _, app := range asset.App {
		currentApps[app] = struct{}{}
	}
	return !reflect.DeepEqual(previousApps, currentApps)
}

// ==================== Active Fingerprint Scanning ====================

// ActiveFingerprintResult 主动指纹扫描结果
type ActiveFingerprintResult struct {
	URL           string // 完整URL（包含路径）
	Path          string // 探测路径
	Fingerprint   string // 匹配到的指纹名称
	FingerprintID string // 指纹ID
	StatusCode    int    // HTTP状态码
	Title         string // 页面标题
}

// RunActiveFingerprint 执行主动指纹扫描
// 参考 Slack 项目的 ActiveFingerScan 实现
func (s *FingerprintScanner) RunActiveFingerprint(ctx context.Context, assets []*Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{}), onAssetUpdated func(*Asset)) {
	if taskLog == nil {
		taskLog = func(level, format string, args ...interface{}) {
			switch level {
			case "ERROR", "WARN":
				logx.Errorf(format, args...)
			case "DEBUG":
				logx.Debugf(format, args...)
			default:
				logx.Infof(format, args...)
			}
		}
	}

	if s.customFingerprintEngine == nil {
		taskLog("INFO", "Active fingerprint: no custom engine configured, skipping")
		return
	}

	activeCount := s.customFingerprintEngine.GetActiveFingerprintCount()
	if activeCount == 0 {
		taskLog("INFO", "Active fingerprint: no active fingerprints loaded, skipping")
		return
	}

	// 过滤出存活的HTTP资产
	aliveAssets := make([]*Asset, 0)
	for _, asset := range assets {
		if asset.HttpStatus != "" && asset.HttpStatus != "0" {
			aliveAssets = append(aliveAssets, asset)
		}
	}

	if len(aliveAssets) == 0 {
		taskLog("INFO", "Active fingerprint: no alive HTTP assets found, skipping")
		return
	}

	taskLog("INFO", "Active fingerprint: scanning %d assets with %d active fingerprints", len(aliveAssets), activeCount)

	// 设置主动指纹超时
	activeTimeout := 10 * time.Second
	if opts.ActiveTimeout > 0 {
		activeTimeout = time.Duration(opts.ActiveTimeout) * time.Second
	}

	// 记录已访问的URL，避免重复扫描
	visited := make(map[string]bool)
	var visitedMu sync.Mutex

	// 记录每个目标的超时次数，超过阈值则跳过
	timeoutCounter := make(map[string]int)
	var timeoutMu sync.Mutex
	const maxTimeoutCount = 8 // 降低超时阈值，减少对不可达目标的无效探测

	// 并发控制
	concurrency := 10
	if opts.Concurrency > 0 {
		concurrency = opts.Concurrency
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	changedAssets := make(map[*Asset]struct{})

	// 获取主动指纹列表
	activeFingerprints := s.customFingerprintEngine.GetActiveFingerprints()

	// 调试：检查主动指纹是否有匹配规则
	fingerprintsWithRule := 0
	fingerprintsWithoutRule := 0
	for _, fp := range activeFingerprints {
		hasRule := fp.Rule != "" || len(fp.HTML) > 0 || len(fp.Headers) > 0 || len(fp.Scripts) > 0
		if hasRule {
			fingerprintsWithRule++
			taskLog("DEBUG", "Active fingerprint '%s' has rule: %s, paths: %v", fp.Name, fp.Rule, fp.ActivePaths)
		} else {
			fingerprintsWithoutRule++
			taskLog("DEBUG", "Active fingerprint '%s' has NO rule, paths: %v", fp.Name, fp.ActivePaths)
		}
	}
	taskLog("DEBUG", "Active fingerprints: %d with rules, %d without rules", fingerprintsWithRule, fingerprintsWithoutRule)

	// 请求计数器
	var requestCount int32
	var successCount int32
	var failCount int32

dispatch:
	for _, asset := range aliveAssets {
		// 构建基础URL
		scheme := asset.Service
		if scheme == "" {
			if asset.Port == 443 || asset.Port == 8443 {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}
		baseURL := fmt.Sprintf("%s://%s:%d", scheme, asset.Host, asset.Port)

		for _, fp := range activeFingerprints {
			if !fp.Enabled || len(fp.ActivePaths) == 0 {
				continue
			}

			for _, path := range fp.ActivePaths {
				select {
				case <-ctx.Done():
					break dispatch
				default:
				}

				// 检查是否超过超时阈值
				timeoutMu.Lock()
				if timeoutCounter[baseURL] >= maxTimeoutCount {
					timeoutMu.Unlock()
					continue
				}
				timeoutMu.Unlock()

				// 构建完整URL
				fullURL := baseURL + path

				// 去重检查
				visitedMu.Lock()
				if visited[fullURL] {
					visitedMu.Unlock()
					continue
				}
				visited[fullURL] = true
				visitedMu.Unlock()

				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					break dispatch
				}
				wg.Add(1)

				go func(asset *Asset, fp *model.Fingerprint, fullURL, path, baseURL string) {
					defer wg.Done()
					defer func() { <-sem }()

					// 增加请求计数
					atomic.AddInt32(&requestCount, 1)

					// 创建带超时的请求
					reqCtx, cancel := context.WithTimeout(ctx, activeTimeout)
					defer cancel()

					// 发起请求
					req, err := http.NewRequestWithContext(reqCtx, "GET", fullURL, nil)
					if err != nil {
						atomic.AddInt32(&failCount, 1)
						taskLog("DEBUG", "Active fingerprint request create failed: %s, error: %v", fullURL, err)
						return
					}

					resp, err := s.client.Do(req)
					if err != nil {
						// 记录超时
						timeoutMu.Lock()
						timeoutCounter[baseURL]++
						timeoutMu.Unlock()
						atomic.AddInt32(&failCount, 1)

						// 连接被拒绝时快速失败：该目标不可达，跳过后续探测
						if isConnectionRefused(err) {
							timeoutMu.Lock()
							timeoutCounter[baseURL] = maxTimeoutCount // 直接标记为达到上限
							timeoutMu.Unlock()
						}
						return
					}
					defer resp.Body.Close()

					atomic.AddInt32(&successCount, 1)

					// 读取响应体
					body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

					// 构建指纹匹配数据
					fpData := &FingerprintData{
						Title:         extractTitle(ToUTF8(body, "")),
						Body:          ToUTF8(body, ""),
						BodyBytes:     body,
						Headers:       resp.Header,
						HeaderString:  formatHeaders(resp.Header),
						Server:        resp.Header.Get("Server"),
						URL:           fullURL,
						Cookies:       resp.Header.Get("Set-Cookie"),
						BodyTruncated: len(body) >= 1024*1024,
					}

					// 调试：记录请求结果
					taskLog("DEBUG", "Active fingerprint request: %s, status=%d, bodyLen=%d, title=%s", fullURL, resp.StatusCode, len(body), fpData.Title)

					// 匹配指纹
					if s.customFingerprintEngine.MatchActiveFingerprint(fp, fpData) {
						// 404页面通常不算匹配成功（除非是特定指纹如ThinkPHP）
						if resp.StatusCode == 404 && !strings.Contains(strings.ToLower(fp.Name), "thinkphp") {
							taskLog("DEBUG", "Active fingerprint '%s' matched but status is 404, skipping", fp.Name)
							return
						}

						taskLog("DEBUG", "Active fingerprint matched: %s -> %s (path: %s)", baseURL, fp.Name, path)
						taskLog("INFO", "Active fingerprint: %s -> %s", fullURL, fp.Name)
						s.assetMutex.Lock()
						if mergeActiveFingerprintApp(asset, fp) {
							changedAssets[asset] = struct{}{}
						}
						s.assetMutex.Unlock()
					}
				}(asset, fp, fullURL, path, baseURL)
			}
		}
	}

	wg.Wait()
	if onAssetUpdated != nil {
		for _, asset := range aliveAssets {
			if _, changed := changedAssets[asset]; !changed {
				continue
			}
			assetSnapshot := *asset
			assetSnapshot.App = append([]string(nil), asset.App...)
			onAssetUpdated(&assetSnapshot)
		}
	}
	taskLog("INFO", "Active fingerprint: scan completed, requests=%d, success=%d, fail=%d", requestCount, successCount, failCount)
}

// isConnectionRefused 检查错误是否为连接被拒绝（目标不可达）
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no route to host") ||
		strings.Contains(errStr, "network is unreachable")
}
