package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"cscan/pkg/mapping"
	"cscan/pkg/utils"

	nuclei "github.com/projectdiscovery/nuclei/v3/lib"
	"github.com/projectdiscovery/nuclei/v3/pkg/output"
	"github.com/projectdiscovery/nuclei/v3/pkg/templates"
	"github.com/zeromicro/go-zero/core/logx"
	yamlv2 "gopkg.in/yaml.v2"
	"gopkg.in/yaml.v3"
)

// MaxResponseSize 响应内容最大存储大小 (10KB)
// 响应内容超过10KB时只存储前10KB并标记为截断
const MaxResponseSize = 10 * 1024

// VulEvidence 漏洞证据结构体
type VulEvidence struct {
	MatcherName       string   `json:"matcherName"`       // 匹配器名称 (Req 3.1)
	ExtractedResults  []string `json:"extractedResults"`  // 提取器结果列表 (Req 3.2)
	CurlCommand       string   `json:"curlCommand"`       // 可复现的curl命令 (Req 3.3)
	Request           string   `json:"request"`           // HTTP请求内容 (Req 3.4)
	Response          string   `json:"response"`          // HTTP响应摘要 (Req 3.5)
	ResponseTruncated bool     `json:"responseTruncated"` // 响应是否被截断 (Req 3.7)
}

// CollectEvidence 从Nuclei ResultEvent收集证据
func CollectEvidence(event *output.ResultEvent) *VulEvidence {
	if event == nil {
		return nil
	}

	evidence := &VulEvidence{
		MatcherName:       event.MatcherName,
		ExtractedResults:  event.ExtractedResults,
		CurlCommand:       event.CURLCommand,
		Request:           event.Request,
		Response:          event.Response,
		ResponseTruncated: false,
	}

	// 处理响应截断逻辑：超过10KB只存储前10KB
	if len(evidence.Response) > MaxResponseSize {
		evidence.Response = evidence.Response[:MaxResponseSize]
		evidence.ResponseTruncated = true
	}

	return evidence
}

// templateMeta 记录每个临时模板文件对应的原始索引和templateId，用于坏模板定位
type templateMeta struct {
	index      int
	templateID string
	path       string
}

// NucleiScanner Nuclei扫描器 (使用SDK模式)
type NucleiScanner struct {
	BaseScanner
}

// NewNucleiScanner 创建Nuclei扫描器
func NewNucleiScanner() *NucleiScanner {
	return &NucleiScanner{
		BaseScanner: BaseScanner{name: "nuclei"},
	}
}

// NucleiOptions Nuclei扫描选项
type NucleiOptions struct {
	Templates            []string                 `json:"templates"`        // 模板路径
	Tags                 []string                 `json:"tags"`             // 标签过滤
	Severity             string                   `json:"severity"`         // 严重级别: critical,high,medium,low,info,unknown (CSV格式)
	ExcludeTags          []string                 `json:"excludeTags"`      // 排除标签
	ExcludeTemplates     []string                 `json:"excludeTemplates"` // 排除模板
	RateLimit            int                      `json:"rateLimit"`        // 速率限制
	Concurrency          int                      `json:"concurrency"`      // 并发数
	Timeout              int                      `json:"timeout"`          // 总超时时间(秒)，由调用方根据目标数量计算
	TargetTimeout        int                      `json:"targetTimeout"`    // 单个目标超时时间(秒)，默认600秒
	Retries              int                      `json:"retries"`          // 重试次数
	AutoScan             bool                     `json:"autoScan"`         // 基于自定义标签映射自动扫描
	AutomaticScan        bool                     `json:"automaticScan"`    // 基于Wappalyzer技术的自动扫描（nuclei -as）
	TagMappings          map[string][]string      `json:"tagMappings"`      // 应用名称到Nuclei标签的映射
	CustomTemplates      []string                 `json:"customTemplates"`  // 自定义模板内容(YAML)
	CustomPocOnly        bool                     `json:"customPocOnly"`    // 只使用自定义POC
	NucleiTemplates      []string                 `json:"nucleiTemplates"`  // 从数据库加载的Nuclei模板内容
	CustomHeaders        []string                 `json:"customHeaders"`    // 自定义HTTP头部，格式: "Header: Value"
	OnVulnerabilityFound func(vul *Vulnerability) `json:"-"`                // 发现漏洞时的回调函数
}

// Validate 验证 NucleiOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *NucleiOptions) Validate() error {
	if o.RateLimit < 0 {
		return fmt.Errorf("rateLimit must be non-negative, got %d", o.RateLimit)
	}
	if o.Concurrency < 0 {
		return fmt.Errorf("concurrency must be non-negative, got %d", o.Concurrency)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.TargetTimeout < 0 {
		return fmt.Errorf("targetTimeout must be non-negative, got %d", o.TargetTimeout)
	}
	if o.Retries < 0 {
		return fmt.Errorf("retries must be non-negative, got %d", o.Retries)
	}
	// 验证 severity 格式
	if o.Severity != "" {
		validSeverities := map[string]bool{
			"critical": true, "high": true, "medium": true,
			"low": true, "info": true, "unknown": true,
		}
		severities := strings.Split(o.Severity, ",")
		for _, s := range severities {
			s = strings.TrimSpace(strings.ToLower(s))
			if !validSeverities[s] {
				return fmt.Errorf("invalid severity '%s', must be one of: critical, high, medium, low, info, unknown", s)
			}
		}
	}
	return nil
}

// Scan 执行Nuclei扫描
// OPTIMIZATION: Refactored to use ScanBatch for parallel target scanning.
func (s *NucleiScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		WorkspaceId:     config.WorkspaceId,
		MainTaskId:      config.MainTaskId,
		Vulnerabilities: make([]*Vulnerability, 0),
	}

	// 1. Prepare Options - 使用自适应参数
	adaptive := GetGlobalAdaptiveConfig()
	opts := &NucleiOptions{
		Severity:      "critical,high,medium",
		RateLimit:     adaptive.NucleiRateLimit,   // 自适应: 低配50, 中配100, 高配150
		Concurrency:   adaptive.NucleiConcurrency, // 自适应: 低配5, 中配15, 高配25
		Timeout:       600,                        // Global Timeout
		TargetTimeout: 600,                        // Per-target Timeout
		Retries:       adaptive.NucleiRetries,     // 自适应: 低配1, 中配1, 高配1
	}
	if config.Options != nil {
		if o, ok := config.Options.(*NucleiOptions); ok {
			opts = o
		}
	}

	// Safety cap for template concurrency
	if opts.Concurrency > 50 {
		opts.Concurrency = 50
	}

	// 2. Prepare Targets
	var targets []string
	if len(config.Targets) > 0 {
		targets = config.Targets
	} else {
		targets = s.prepareTargets(config.Assets)
	}

	if len(targets) == 0 {
		logx.Info("No targets for nuclei scan")
		return result, nil
	}

	// 3. Auto-Tag Generation (Logic preserved)
	if opts.AutoScan && opts.TagMappings != nil {
		if autoTags := s.generateAutoTags(config.Assets, opts.TagMappings); len(autoTags) > 0 {
			opts.Tags = append(opts.Tags, autoTags...)
		}
	}
	if opts.AutomaticScan {
		if wappTags := s.generateWappalyzerAutoTags(config.Assets); len(wappTags) > 0 {
			opts.Tags = append(opts.Tags, wappTags...)
		}
	}
	opts.Tags = utils.UniqueStrings(opts.Tags)

	// 4. EXECUTION OPTIMIZATION: Use Batch Scan instead of Serial Loop
	// This enables Nuclei's internal scheduler to handle HostConcurrency
	logx.Infof("Nuclei: Starting batch scan for %d targets (Parallel Mode)", len(targets))

	vuls, err := s.ScanBatch(ctx, targets, opts, config.TaskLogger)
	if err != nil {
		return result, err
	}

	result.Vulnerabilities = vuls
	return result, nil
}

// nucleiScanError 统一的Nuclei扫描错误处理
type nucleiScanError struct {
	target  string
	phase   string
	err     error
	timeout bool
}

func (e *nucleiScanError) Error() string {
	if e.timeout {
		return fmt.Sprintf("nuclei %s timeout for %s", e.phase, e.target)
	}
	return fmt.Sprintf("nuclei %s failed for %s: %v", e.phase, e.target, e.err)
}

// logScanError 统一的错误日志处理
func logScanError(err *nucleiScanError, taskLogger func(level, format string, args ...interface{})) {
	if err.timeout {
		logx.Infof("Nuclei: %s timeout for %s", err.phase, err.target)
		if taskLogger != nil {
			taskLogger("WARN", "POC %s timeout", err.phase)
		}
	} else {
		logx.Errorf("Nuclei %s error for %s: %v", err.phase, err.target, err.err)
		if taskLogger != nil {
			taskLogger("ERROR", "POC %s error: %v", err.phase, err.err)
		}
	}
}

// scanSingleTarget 扫描单个目标
func (s *NucleiScanner) scanSingleTarget(ctx context.Context, target string, opts *NucleiOptions, customTemplatePaths []string, templateNames []string, taskLogger func(level, format string, args ...interface{})) []*Vulnerability {
	var vuls []*Vulnerability
	startTime := time.Now()

	// 创建独立的context避免任务间相互影响
	engineCtx, engineCancel := context.WithTimeout(context.Background(), time.Duration(opts.TargetTimeout)*time.Second)
	defer engineCancel()

	// 初始化引擎
	ne, err := s.initNucleiEngine(engineCtx, opts, customTemplatePaths, target, taskLogger)
	if err != nil {
		return vuls
	}
	defer ne.Close()

	// 获取实际加载的模板数量
	loadedTemplates := ne.GetTemplates()
	if len(loadedTemplates) == 0 {
		logx.Errorf("No templates loaded for %s", target)
		if taskLogger != nil {
			taskLogger("ERROR", "POC不可用: 模板加载失败")
		}
		return vuls
	}
	if taskLogger != nil {
		taskLogger("INFO", "  Loaded %d templates (filtered by severity)", len(loadedTemplates))
	}

	// 加载目标并执行扫描
	ne.LoadTargets([]string{target}, false)
	vuls = s.executeNucleiScan(ctx, engineCtx, ne, target, opts, loadedTemplates, taskLogger, startTime)

	return vuls
}

// initNucleiEngine 初始化Nuclei引擎 - 提取公共逻辑
func (s *NucleiScanner) initNucleiEngine(ctx context.Context, opts *NucleiOptions, customTemplatePaths []string, target string, taskLogger func(level, format string, args ...interface{})) (*nuclei.NucleiEngine, error) {
	nucleiOpts := s.buildNucleiOptions(opts, customTemplatePaths, 1)

	logx.Debugf("Creating nuclei engine for %s with timeout %ds", target, opts.TargetTimeout)
	ne, err := nuclei.NewNucleiEngineCtx(ctx, nucleiOpts...)
	if err != nil {
		logScanError(&nucleiScanError{target: target, phase: "engine init", err: err}, taskLogger)
		return nil, err
	}

	// 启用请求/响应存储
	if engineOpts := ne.Options(); engineOpts != nil {
		engineOpts.StoreResponse = true
	}

	// 加载模板
	if err := ne.LoadAllTemplates(); err != nil {
		ne.Close()
		logScanError(&nucleiScanError{target: target, phase: "template load", err: err}, taskLogger)
		return nil, err
	}

	return ne, nil
}

// executeNucleiScan 执行Nuclei扫描 - 提取扫描逻辑
func (s *NucleiScanner) executeNucleiScan(ctx, engineCtx context.Context, ne *nuclei.NucleiEngine, target string, opts *NucleiOptions, loadedTemplates []*templates.Template, taskLogger func(level, format string, args ...interface{}), startTime time.Time) []*Vulnerability {
	var vuls []*Vulnerability
	templateCount := len(loadedTemplates)
	scannedCount := 0
	foundCount := 0
	seenTemplates := make(map[string]bool)

	// 监听父context取消
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			logx.Infof("Nuclei: parent context cancelled for %s", target)
			if taskLogger != nil {
				taskLogger("WARN", "POC scan interrupted: %v", ctx.Err())
			}
		case <-done:
		}
	}()
	defer close(done)

	// 执行扫描
	err := ne.ExecuteCallbackWithCtx(engineCtx, func(event *output.ResultEvent) {
		scannedCount++
		if event.Matched != "" {
			vulKey := event.TemplateID + ":" + event.MatcherName
			if !seenTemplates[vulKey] {
				seenTemplates[vulKey] = true
				foundCount++
				if taskLogger != nil {
					if event.MatcherName != "" {
						// 抽取并格式化有效原因详细信息
						reason := s.extractMatchedReason(event)
						taskLogger("INFO", "  [%d] ✓ %s:%s [%s] - 原因: %s", scannedCount, event.TemplateID, event.MatcherName, event.Info.SeverityHolder.Severity.String(), reason)
					} else {
						reason := s.extractMatchedReason(event)
						taskLogger("INFO", "  [%d] ✓ %s [%s] - 原因: %s", scannedCount, event.TemplateID, event.Info.SeverityHolder.Severity.String(), reason)
					}
				}
				if vul := s.convertResult(event); vul != nil {
					vuls = append(vuls, vul)
				}
			}
		} else if taskLogger != nil && scannedCount%50 == 0 {
			taskLogger("INFO", "  [%d] Scanning... %d vuls found", scannedCount, foundCount)
		}
	})

	// 处理扫描结果
	s.handleScanResult(err, engineCtx, target, opts, taskLogger, scannedCount, templateCount, foundCount, startTime)

	return vuls
}

// handleScanResult 处理扫描结果 - 统一错误处理
func (s *NucleiScanner) handleScanResult(err error, engineCtx context.Context, target string, opts *NucleiOptions, taskLogger func(level, format string, args ...interface{}), scannedCount, templateCount, foundCount int, startTime time.Time) {
	elapsed := int(time.Since(startTime).Seconds())

	if err != nil || engineCtx.Err() == context.DeadlineExceeded {
		logScanError(&nucleiScanError{
			target:  target,
			phase:   "scan",
			err:     err,
			timeout: engineCtx.Err() == context.DeadlineExceeded,
		}, taskLogger)
	} else if engineCtx.Err() == context.Canceled {
		if taskLogger != nil {
			taskLogger("WARN", "POC scan cancelled")
		}
	}

	// 输出统计 - 使用实际执行的模板数作为完成数
	// 注意：scannedCount 是回调触发次数，可能小于 templateCount
	// 因为某些模板可能因协议不匹配、条件不满足或超时而被跳过
	if taskLogger != nil {
		taskLogger("INFO", "  Completed: %d templates, %d vuls, %ds", scannedCount, foundCount, elapsed)
	}
}

// ScanBatch 批量扫描多个目标（使用单个Nuclei引擎实例）
// 适用于使用同一个POC扫描大量目标的场景
func (s *NucleiScanner) ScanBatch(ctx context.Context, targets []string, opts *NucleiOptions, taskLogger func(level, format string, args ...interface{})) (vuls []*Vulnerability, err error) {
	seen := make(map[string]bool)
	startTime := time.Now()

	if len(targets) == 0 {
		return vuls, nil
	}

	// 日志辅助函数
	taskLog := func(level, format string, args ...interface{}) {
		if taskLogger != nil {
			taskLogger(level, format, args...)
		}
	}

	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			taskLog("ERROR", "Nuclei batch scan panic recovered: %v, stack: %s", r, stack)
			logx.Errorf("[Nuclei] batch scan panic recovered: %v, stack: %s", r, stack)
			err = fmt.Errorf("nuclei batch scan panic recovered: %v", r)
		}
	}()

	taskLog("INFO", "Batch scan: %d targets", len(targets))

	// 处理自定义POC - 获取缓存或写入缓存
	var customTemplatePaths []string
	var templateMetas []templateMeta

	if len(opts.CustomTemplates) > 0 {
		cache := getTemplateCache()
		cache.EvictStale()

		taskLog("INFO", "Preparing %d POC templates", len(opts.CustomTemplates))
		for i, content := range opts.CustomTemplates {
			// 1. 廉价的YAML预校验
			templateID, err := preValidateTemplate(content)
			if err != nil {
				taskLog("WARN", "Skip bad template index=%d: %v", i, err)
				continue
			}

			// 2. 深度反序列化排雷（隔离 Nuclei 协程崩溃漏洞）
			if err := safeDeepParseTemplate(content); err != nil {
				logx.Errorf("Deep template panic intercepted for %s: %v", templateID, err)
				taskLog("WARN", "Skip panic-inducing template index=%d templateId=%s: %v", i, templateID, err)
				continue
			}

			// 3. 从缓存获取或写入
			path, err := cache.GetOrWrite(content, templateID)
			if err != nil {
				logx.Errorf("Failed to write custom template %d (%s): %v", i, templateID, err)
				taskLog("ERROR", "Failed to cache POC template index=%d templateId=%s: %v", i, templateID, err)
				continue
			}

			customTemplatePaths = append(customTemplatePaths, path)
			templateMetas = append(templateMetas, templateMeta{index: i, templateID: templateID, path: path})
		}
		taskLog("INFO", "Processed %d usable POC templates", len(customTemplatePaths))
	}

	if len(customTemplatePaths) == 0 {
		taskLog("ERROR", "No usable POC templates loaded")
		return nil, fmt.Errorf("no usable POC templates")
	}

	// 设置超时时间：基于目标数量动态计算
	timeout := opts.Timeout
	if timeout <= 0 {
		// 每个目标30秒，最少60秒，最多3600秒
		timeout = len(targets) * 30
		if timeout < 60 {
			timeout = 60
		}
		if timeout > 3600 {
			timeout = 3600
		}
	}

	// 创建带超时的context
	engineCtx, engineCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer engineCancel()

	// 消除双重引擎初始化，调用一次即可构建并加载好引擎
	taskLog("INFO", "Initializing Nuclei engine (1-pass)...")
	ne, customTemplatePaths, templateMetas, err := s.buildAndLoadEngine(
		engineCtx, customTemplatePaths, templateMetas, opts, len(targets), taskLog,
	)
	if err != nil {
		taskLog("ERROR", "Failed to construct running nuclei engine: %v", err)
		return nil, err
	}
	defer ne.Close()

	loadedTemplates := ne.GetTemplates()
	if len(loadedTemplates) == 0 {
		taskLog("WARN", "No templates loaded after filtering")
		return vuls, nil
	}
	taskLog("INFO", "Loaded %d templates", len(loadedTemplates))

	// 批量加载所有目标
	taskLog("INFO", "Loading %d targets...", len(targets))
	ne.LoadTargets(targets, false)

	// 统计变量
	scannedCount := 0
	foundCount := 0

	// 监听父context取消
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			logx.Infof("Nuclei batch scan: parent context cancelled")
			taskLog("WARN", "Scan interrupted")
			engineCancel()
		case <-done:
			return
		}
	}()
	defer close(done)

	// 执行扫描
	taskLog("INFO", "Starting batch scan (timeout: %ds)...", timeout)

	totalWork := len(customTemplatePaths) * len(targets)
	matcherStatusEnabled := totalWork <= 50_000

	// 智能进度回调机制
	if !matcherStatusEnabled {
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-engineCtx.Done():
					return
				case <-ticker.C:
					taskLog("INFO", "  Scanning... %d vuls found (%.0fs elapsed)", foundCount, time.Since(startTime).Seconds())
				case <-done:
					return
				}
			}
		}()
	}

	err = ne.ExecuteCallbackWithCtx(engineCtx, func(event *output.ResultEvent) {
		// 只有 MatcherStatus 开启的情况下，回调才是按个触发的；否则只会在成功匹配时触发
		if matcherStatusEnabled {
			scannedCount++
		}

		// 判断是否匹配成功
		if event.Matched != "" {
			if !matcherStatusEnabled {
				// MatcherStatus关闭时，只有漏洞触发回调，进度只能近似或忽略
				scannedCount++
			}
			vulKey := fmt.Sprintf("%s:%s:%s", event.Host, event.TemplateID, event.MatcherName)
			if !seen[vulKey] {
				seen[vulKey] = true
				foundCount++

				taskLog("INFO", "[%d] ✓ %s - %s [%s]", foundCount, event.Host, event.TemplateID, event.Info.SeverityHolder.Severity.String())

				vul := s.convertResult(event)
				if vul != nil {
					vuls = append(vuls, vul)
					// 实时回调
					if opts.OnVulnerabilityFound != nil {
						opts.OnVulnerabilityFound(vul)
					}
				}
			}
		} else if matcherStatusEnabled {
			// 每1000个任务显示进度 (MatcherStatus 开启时)
			if scannedCount%1000 == 0 {
				taskLog("INFO", "[%d] Scanning... %d vuls found", scannedCount, foundCount)
			}
		}
	})

	elapsed := time.Since(startTime).Seconds()

	if err != nil {
		if engineCtx.Err() == context.DeadlineExceeded {
			taskLog("WARN", "Scan timeout after %.0fs", elapsed)
		} else if engineCtx.Err() == context.Canceled {
			taskLog("WARN", "Scan cancelled after %.0fs", elapsed)
		} else {
			taskLog("ERROR", "Scan error: %v", err)
		}
	}

	taskLog("INFO", "Batch scan completed: %d targets, %d vuls found, %.0fs", len(targets), foundCount, elapsed)

	return vuls, nil
}

// prepareTargets 准备目标URL列表（跳过非HTTP资产）
func (s *NucleiScanner) prepareTargets(assets []*Asset) []string {
	targets := make([]string, 0, len(assets))
	seen := make(map[string]bool)
	skipped := 0

	for _, asset := range assets {
		// 使用 IsHTTP 字段判断（端口扫描阶段已设置）
		// 同时检查端口是否为常见HTTP端口，避免对非HTTP服务进行扫描
		if !asset.IsHTTP && !IsHTTPService(asset.Service, asset.Port) {
			skipped++
			logx.Debugf("Skipping non-HTTP asset: %s:%d (service: %s, isHttp: %v)", asset.Host, asset.Port, asset.Service, asset.IsHTTP)
			continue
		}

		scheme := "http"
		if asset.Service == "https" || asset.Port == 443 || asset.Port == 8443 {
			scheme = "https"
		}

		// 构建目标URL，如果资产有 Path 字段，包含在目标URL中
		// 例如：用户输入 http://example.com/api/v1/，POC扫描应该针对该路径
		var target string
		if asset.Path != "" && asset.Path != "/" {
			// 有路径的情况：scheme://host:port/path
			path := asset.Path
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			target = fmt.Sprintf("%s://%s:%d%s", scheme, asset.Host, asset.Port, path)
		} else {
			// 无路径的情况：scheme://host:port
			target = fmt.Sprintf("%s://%s:%d", scheme, asset.Host, asset.Port)
		}

		if !seen[target] {
			seen[target] = true
			targets = append(targets, target)
		}
	}

	if skipped > 0 {
		logx.Infof("Nuclei: skipped %d non-HTTP assets, scanning %d HTTP targets", skipped, len(targets))
	}

	return targets
}

// buildNucleiOptions 构建Nuclei SDK选项
// 所有模板都应该从数据库获取，不使用本地模板目录
func (s *NucleiScanner) buildNucleiOptions(opts *NucleiOptions, customTemplatePaths []string, targetCount int) []nuclei.NucleiSDKOptions {
	var nucleiOpts []nuclei.NucleiSDKOptions

	// 智能进度回调：如果 模板数×目标数 > 阈值，则关闭 EnableMatcherStatus，
	// 避免回调风暴。由外部用定时器汇报进度。
	const matcherStatusThreshold = 5000
	totalWork := len(customTemplatePaths) * targetCount
	if totalWork <= matcherStatusThreshold {
		nucleiOpts = append(nucleiOpts, nuclei.EnableMatcherStatus())
	} else {
		logx.Infof("Large scan detected (%d templates x %d targets), disabling MatcherStatus to save CPU", len(customTemplatePaths), targetCount)
	}

	// 判断是否有模板（从数据库获取的模板）
	hasTemplates := len(customTemplatePaths) > 0

	if hasTemplates {
		// 使用从数据库获取的模板
		nucleiOpts = append(nucleiOpts, nuclei.WithTemplatesOrWorkflows(nuclei.TemplateSources{
			Templates: customTemplatePaths,
		}))
		logx.Infof("Using %d templates from database", len(customTemplatePaths))
	} else {
		// 没有POC，记录警告
		logx.Errorf("No templates provided! POC scan requires templates from database.")
	}

	// 注意：不再设置 severity 过滤器
	// 模板已经在数据库查询时按 severity 过滤过了，Nuclei 引擎不应该再次过滤
	// 这样可以确保 "Loaded X POC templates" 和 "Loaded X templates" 数量一致

	// 并发配置
	if opts.Concurrency > 0 {
		nucleiOpts = append(nucleiOpts, nuclei.WithConcurrency(nuclei.Concurrency{
			TemplateConcurrency:           opts.Concurrency,
			HostConcurrency:               opts.Concurrency,
			HeadlessHostConcurrency:       5,
			HeadlessTemplateConcurrency:   5,
			JavascriptTemplateConcurrency: 5,
			TemplatePayloadConcurrency:    10,
			ProbeConcurrency:              50,
		}))
	}

	// 速率限制
	if opts.RateLimit > 0 {
		nucleiOpts = append(nucleiOpts, nuclei.WithGlobalRateLimit(opts.RateLimit, 1))
	}

	// 自定义HTTP头部
	if len(opts.CustomHeaders) > 0 {
		nucleiOpts = append(nucleiOpts, nuclei.WithHeaders(opts.CustomHeaders))
		logx.Infof("Using %d custom headers", len(opts.CustomHeaders))
	}

	return nucleiOpts
}

func (s *NucleiScanner) extractMatchedReason(event *output.ResultEvent) string {
	if event == nil {
		return "无匹配信息"
	}

	var reason string
	if len(event.ExtractedResults) > 0 {
		reason = "提取到特征: " + strings.Join(event.ExtractedResults, ", ")
	} else if len(event.MatcherName) > 0 {
		reason = "规则命中: " + event.MatcherName
	} else {
		reason = "基于请求响应特征匹配模板"
	}

	if event.Matched != "" {
		reason += fmt.Sprintf(" (触点: %s)", event.Matched)
	}

	return reason
}

// convertResult 转换Nuclei结果为漏洞对象
func (s *NucleiScanner) convertResult(event *output.ResultEvent) *Vulnerability {
	if event == nil {
		return nil
	}

	// 优先从 Matched URL 解析 host 和 port（实际漏洞URL）
	// 如果 Matched 为空，则回退到 Host
	var host string
	var port int
	if event.Matched != "" {
		host, port = s.parseHostPort(event.Matched)
	} else {
		host, port = s.parseHostPort(event.Host)
	}

	resultDesc := event.Info.Name
	if event.Info.Description != "" {
		resultDesc += "\n" + event.Info.Description
	}
	if len(event.ExtractedResults) > 0 {
		resultDesc += "\nExtracted: " + strings.Join(event.ExtractedResults, ", ")
	}

	// 收集证据
	evidence := CollectEvidence(event)

	// 构建漏洞对象
	tags := event.Info.Tags.ToSlice()
	logx.Infof("[convertResult] poc=%s matched=%s vulName=%q tags=%v", event.TemplateID, event.Matched, event.Info.Name, tags)
	vul := &Vulnerability{
		Authority: utils.BuildTargetWithPort(host, port),
		Host:      host,
		Port:      port,
		Url:       event.Matched,
		PocFile:   event.TemplateID,
		Source:    "nuclei",
		Severity:  event.Info.SeverityHolder.Severity.String(),
		Result:    resultDesc,
		VulName:   event.Info.Name,
		Tags:      tags,
	}

	// 关联模板知识库信息 (Requirement 1.4)
	// 从模板info.classification提取CVE/CWE/CVSS信息
	if event.Info.Classification != nil {
		vul.CvssScore = event.Info.Classification.CVSSScore
		// CVE ID - 可能是单个或多个
		cveIds := event.Info.Classification.CVEID.ToSlice()
		if len(cveIds) > 0 {
			vul.CveId = cveIds[0] // 取第一个CVE ID
		}
		// CWE ID - 可能是单个或多个
		cweIds := event.Info.Classification.CWEID.ToSlice()
		if len(cweIds) > 0 {
			vul.CweId = cweIds[0] // 取第一个CWE ID
		}
	}

	// 关联参考链接
	if event.Info.Reference != nil {
		vul.References = event.Info.Reference.ToSlice()
	}

	// 关联修复建议
	if event.Info.Remediation != "" {
		vul.Remediation = event.Info.Remediation
	}

	// 添加证据信息
	if evidence != nil {
		vul.MatcherName = evidence.MatcherName
		vul.ExtractedResults = evidence.ExtractedResults
		vul.CurlCommand = evidence.CurlCommand
		vul.Request = evidence.Request
		vul.Response = evidence.Response
		vul.ResponseTruncated = evidence.ResponseTruncated
	}

	return vul
}

// parseHostPort 从URL解析主机和端口
func (s *NucleiScanner) parseHostPort(rawURL string) (string, int) {
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return s.parseHostPortSimple(rawURL)
	}

	host := u.Hostname()
	port := 80

	if u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	} else if u.Scheme == "https" {
		port = 443
	}

	return host, port
}

// parseHostPortSimple 简单解析主机和端口
func (s *NucleiScanner) parseHostPortSimple(hostPort string) (string, int) {
	hostPort = strings.TrimPrefix(hostPort, "http://")
	hostPort = strings.TrimPrefix(hostPort, "https://")

	if idx := strings.Index(hostPort, "/"); idx != -1 {
		hostPort = hostPort[:idx]
	}

	if idx := strings.LastIndex(hostPort, ":"); idx != -1 {
		host := hostPort[:idx]
		port := 80
		if p, err := strconv.Atoi(hostPort[idx+1:]); err == nil {
			port = p
		}
		return host, port
	}

	return hostPort, 80
}

// generateAutoTags 根据资产的应用信息生成Nuclei标签（基于自定义标签映射）
func (s *NucleiScanner) generateAutoTags(assets []*Asset, tagMappings map[string][]string) []string {
	tagSet := make(map[string]bool)

	for _, asset := range assets {
		logx.Debugf("Asset %s:%d apps: %v", asset.Host, asset.Port, asset.App)
		for _, app := range asset.App {
			appName := parseAppName(app)
			appNameLower := strings.ToLower(appName)

			logx.Debugf("Parsed app name: '%s' -> '%s'", app, appName)

			for mappedApp, tags := range tagMappings {
				if strings.ToLower(mappedApp) == appNameLower {
					logx.Infof("Matched app '%s' -> tags: %v", appName, tags)
					for _, tag := range tags {
						tagSet[tag] = true
					}
					break
				}
			}
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	return tags
}

// generateWappalyzerAutoTags 根据资产的应用信息生成Nuclei标签（基于Wappalyzer内置映射，类似nuclei -as）
func (s *NucleiScanner) generateWappalyzerAutoTags(assets []*Asset) []string {
	tagSet := make(map[string]bool)

	for _, asset := range assets {
		logx.Debugf("Asset %s:%d apps: %v", asset.Host, asset.Port, asset.App)
		for _, app := range asset.App {
			appName := parseAppName(app)
			appNameLower := strings.ToLower(appName)

			// 使用内置的Wappalyzer到Nuclei标签映射
			if tags, ok := mapping.WappalyzerNucleiMapping[appNameLower]; ok {
				logx.Infof("Wappalyzer auto-scan matched '%s' -> tags: %v", appName, tags)
				for _, tag := range tags {
					tagSet[tag] = true
				}
			}
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	return tags
}

// parseAppName 解析应用名称，去除版本号和来源标识
func parseAppName(app string) string {
	appName := app
	// 先去掉 [source] 后缀
	if idx := strings.Index(appName, "["); idx > 0 {
		appName = appName[:idx]
	}
	// 再去掉 :version 后缀
	if idx := strings.Index(appName, ":"); idx > 0 {
		appName = appName[:idx]
	}
	return strings.TrimSpace(appName)
}

// preValidateTemplate 廉价结构检查，在写盘前过滤坏模板（~10-50μs/模板）
func preValidateTemplate(content string) (templateID string, err error) {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < 30 {
		return "", fmt.Errorf("content too short (%d bytes)", len(trimmed))
	}
	if !strings.Contains(content, "id:") {
		return "", fmt.Errorf("missing 'id:' field")
	}
	var wrapper struct {
		Id   string      `yaml:"id"`
		Info interface{} `yaml:"info"`
	}
	if err := yaml.Unmarshal([]byte(content), &wrapper); err != nil {
		return "", fmt.Errorf("YAML syntax: %w", err)
	}
	if wrapper.Id == "" {
		return "", fmt.Errorf("'id' field is empty")
	}
	if wrapper.Info == nil {
		return "", fmt.Errorf("'info' section missing")
	}
	return wrapper.Id, nil
}

// safeDeepParseTemplate 深度模拟 Nuclei 底层解析过程并包裹 recover
// 用于在引擎开启独立 goroutine 加载前，提前在主协程引爆因 govaluate 表达式缺陷导致的 Panic
func safeDeepParseTemplate(content string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("go-evaluate panic recovered: %v", r)
		}
	}()

	var t templates.Template
	err = yamlv2.UnmarshalStrict([]byte(content), &t)
	return err
}

// templateFileCache 基于内容哈希的模板文件缓存
type templateFileCache struct {
	mu      sync.RWMutex
	baseDir string
	entries map[string]*cachedTemplate
	ttl     time.Duration
}

type cachedTemplate struct {
	path       string
	hash       string
	templateID string
	lastUsed   time.Time
}

var (
	globalTemplateCache     *templateFileCache
	globalTemplateCacheOnce sync.Once
)

func getTemplateCache() *templateFileCache {
	globalTemplateCacheOnce.Do(func() {
		baseDir := filepath.Join(os.TempDir(), "nuclei-template-cache")
		os.MkdirAll(baseDir, 0755)
		globalTemplateCache = &templateFileCache{
			baseDir: baseDir,
			entries: make(map[string]*cachedTemplate),
			ttl:     30 * time.Minute,
		}
	})
	return globalTemplateCache
}

func (c *templateFileCache) GetOrWrite(content string, templateID string) (string, error) {
	h := sha256.Sum256([]byte(content))
	hashStr := hex.EncodeToString(h[:])

	c.mu.RLock()
	entry, exists := c.entries[hashStr]
	c.mu.RUnlock()

	if exists {
		if _, err := os.Stat(entry.path); err == nil {
			c.mu.Lock()
			entry.lastUsed = time.Now()
			c.mu.Unlock()
			return entry.path, nil
		}
	}

	path := filepath.Join(c.baseDir, hashStr[:16]+".yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}

	c.mu.Lock()
	c.entries[hashStr] = &cachedTemplate{
		path:       path,
		hash:       hashStr,
		templateID: templateID,
		lastUsed:   time.Now(),
	}
	c.mu.Unlock()

	return path, nil
}

func (c *templateFileCache) EvictStale() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for hash, entry := range c.entries {
		if now.Sub(entry.lastUsed) > c.ttl {
			os.Remove(entry.path)
			delete(c.entries, hash)
		}
	}
}

// extractTemplateId 从模板YAML内容中提取模板ID
func extractTemplateId(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id:") {
			id := strings.TrimPrefix(line, "id:")
			return strings.TrimSpace(id)
		}
	}
	return ""
}

// tryBuildRealEngine 构建用于实际扫描的引擎，内置 panic 保护
// 成功时返回可直接用于 ExecuteCallbackWithCtx 的引擎
func (s *NucleiScanner) tryBuildRealEngine(
	ctx context.Context, paths []string, opts *NucleiOptions, targetCount int,
) (engine *nuclei.NucleiEngine, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("engine panic: %v", r)
			if engine != nil {
				engine.Close()
				engine = nil
			}
		}
	}()
	nucleiOpts := s.buildNucleiOptions(opts, paths, targetCount) // 带完整配置
	engine, err = nuclei.NewNucleiEngineCtx(ctx, nucleiOpts...)
	if err != nil {
		return nil, err
	}
	if eo := engine.Options(); eo != nil {
		eo.StoreResponse = true
	}
	if err = engine.LoadAllTemplates(); err != nil {
		engine.Close()
		return nil, err
	}
	return engine, nil
}

// buildAndLoadEngine 编排引擎创建和模板加载，包含异常回退
func (s *NucleiScanner) buildAndLoadEngine(
	ctx context.Context, paths []string, metas []templateMeta,
	opts *NucleiOptions, targetCount int, taskLog func(string, string, ...interface{}),
) (engine *nuclei.NucleiEngine, cleanPaths []string, cleanMetas []templateMeta, err error) {
	if len(paths) == 0 {
		return nil, nil, nil, fmt.Errorf("no templates provided")
	}

	// 尝试直接构建真正引擎（正常路径：1 次引擎创建）
	engine, err = s.tryBuildRealEngine(ctx, paths, opts, targetCount)
	if err == nil {
		return engine, paths, metas, nil
	}

	// 失败：二分查找坏模板（沿用 binarySearchBadTemplates 的隔离逻辑）
	taskLog("WARN", "Engine init failed (%v), isolating bad templates...", err)
	cleanPaths, cleanMetas = s.isolateBadTemplates(ctx, paths, metas, opts, taskLog)
	if len(cleanPaths) == 0 {
		return nil, nil, nil, fmt.Errorf("no loadable templates after filtering")
	}

	// 用清理后的模板重建
	engine, err = s.tryBuildRealEngine(ctx, cleanPaths, opts, targetCount)
	return engine, cleanPaths, cleanMetas, err
}

// isolateBadTemplates 定位并隔离坏模板
func (s *NucleiScanner) isolateBadTemplates(ctx context.Context, paths []string, metas []templateMeta, opts *NucleiOptions, taskLog func(string, string, ...interface{})) ([]string, []templateMeta) {
	badSet := make(map[string]bool)
	s.binarySearchBadTemplates(ctx, paths, metas, opts, taskLog, badSet)

	if len(badSet) == 0 {
		taskLog("WARN", "Binary search found no single bad template, falling back to sequential validation")
		return s.sequentialFilter(ctx, paths, metas, opts, taskLog)
	}

	var goodPaths []string
	var goodMetas []templateMeta
	for i, p := range paths {
		if !badSet[p] {
			goodPaths = append(goodPaths, p)
			if i < len(metas) {
				goodMetas = append(goodMetas, metas[i])
			}
		}
	}

	taskLog("INFO", "Template filtering done: %d good, %d bad removed", len(goodPaths), len(badSet))

	if len(goodPaths) > 0 && !s.tryLoadTemplates(ctx, goodPaths, opts) {
		taskLog("WARN", "Filtered templates still fail to load, falling back to sequential validation")
		return s.sequentialFilter(ctx, goodPaths, goodMetas, opts, taskLog)
	}

	return goodPaths, goodMetas
}

// tryLoadTemplates 尝试用给定路径创建 Nuclei 引擎并加载模板，成功返回 true
// 内部 recover panic，不会向上传播
func (s *NucleiScanner) tryLoadTemplates(ctx context.Context, paths []string, opts *NucleiOptions) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logx.Errorf("[Nuclei] tryLoadTemplates panic recovered: %v", r)
			ok = false
		}
	}()

	testCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	nucleiOpts := []nuclei.NucleiSDKOptions{
		nuclei.WithTemplatesOrWorkflows(nuclei.TemplateSources{
			Templates: paths,
		}),
		nuclei.DisableUpdateCheck(),
	}

	ne, err := nuclei.NewNucleiEngineCtx(testCtx, nucleiOpts...)
	if err != nil {
		return false
	}
	defer ne.Close()

	if err := ne.LoadAllTemplates(); err != nil {
		return false
	}

	return true
}

// binarySearchBadTemplates 二分法递归查找坏模板，结果写入 badSet
func (s *NucleiScanner) binarySearchBadTemplates(ctx context.Context, paths []string, metas []templateMeta, opts *NucleiOptions, taskLog func(string, string, ...interface{}), badSet map[string]bool) {
	// 递归终止：单个模板
	if len(paths) == 1 {
		if !s.tryLoadTemplates(ctx, paths, opts) {
			badSet[paths[0]] = true
			tid := "unknown"
			if len(metas) > 0 {
				tid = metas[0].templateID
			}
			taskLog("ERROR", "Bad template found: index=%d, templateId=%s, path=%s", metas[0].index, tid, paths[0])
			logx.Errorf("[Nuclei] Bad template isolated: index=%d, templateId=%s, path=%s", metas[0].index, tid, paths[0])
		}
		return
	}

	// 如果整批能加载，说明这批没问题
	if s.tryLoadTemplates(ctx, paths, opts) {
		return
	}

	// 这批有问题，二分继续
	mid := len(paths) / 2
	s.binarySearchBadTemplates(ctx, paths[:mid], metas[:mid], opts, taskLog, badSet)
	s.binarySearchBadTemplates(ctx, paths[mid:], metas[mid:], opts, taskLog, badSet)
}

// sequentialFilter 逐个验证模板，返回可加载的模板列表（最后手段，较慢）
func (s *NucleiScanner) sequentialFilter(ctx context.Context, paths []string, metas []templateMeta, opts *NucleiOptions, taskLog func(string, string, ...interface{})) ([]string, []templateMeta) {
	var goodPaths []string
	var goodMetas []templateMeta

	for i, p := range paths {
		if s.tryLoadTemplates(ctx, []string{p}, opts) {
			goodPaths = append(goodPaths, p)
			if i < len(metas) {
				goodMetas = append(goodMetas, metas[i])
			}
		} else {
			tid := "unknown"
			if i < len(metas) {
				tid = metas[i].templateID
			}
			taskLog("ERROR", "Bad template (sequential): index=%d, templateId=%s", i, tid)
		}
	}

	taskLog("INFO", "Sequential filter done: %d good, %d bad", len(goodPaths), len(paths)-len(goodPaths))
	return goodPaths, goodMetas
}

// ValidatePocTemplate 验证POC模板是否有效
// 使用Nuclei SDK加载模板，检查是否能正确解析
func ValidatePocTemplate(content string) (err error) {
	if content == "" {
		return fmt.Errorf("POC内容不能为空")
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("POC加载panic: %v", r)
			logx.Errorf("[Nuclei] ValidatePocTemplate panic recovered: %v, stack: %s", r, string(debug.Stack()))
		}
	}()

	// 创建临时文件
	tempDir, err := os.MkdirTemp("", "nuclei-validate-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tempDir)

	templatePath := filepath.Join(tempDir, "template.yaml")
	if err := os.WriteFile(templatePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %v", err)
	}

	// 创建Nuclei引擎验证模板
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ne, err := nuclei.NewNucleiEngineCtx(ctx,
		nuclei.WithTemplatesOrWorkflows(nuclei.TemplateSources{
			Templates: []string{templatePath},
		}),
		nuclei.DisableUpdateCheck(),
	)
	if err != nil {
		return fmt.Errorf("POC格式错误: %v", err)
	}
	defer ne.Close()

	// 尝试加载模板
	if err := ne.LoadAllTemplates(); err != nil {
		return fmt.Errorf("POC加载失败: %v", err)
	}

	// 检查是否成功加载了模板
	templates := ne.GetTemplates()
	if len(templates) == 0 {
		return fmt.Errorf("POC无效: 未能加载任何模板，请检查YAML格式和必填字段(id, info, requests等)")
	}

	return nil
}
