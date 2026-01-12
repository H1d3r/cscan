package scanner

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cscan/pkg/mapping"
	"cscan/pkg/utils"

	nuclei "github.com/projectdiscovery/nuclei/v3/lib"
	"github.com/projectdiscovery/nuclei/v3/pkg/output"
	"github.com/zeromicro/go-zero/core/logx"
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
	Templates            []string                      `json:"templates"`            // 模板路径
	Tags                 []string                      `json:"tags"`                 // 标签过滤
	Severity             string                        `json:"severity"`             // 严重级别: critical,high,medium,low,info,unknown (CSV格式)
	ExcludeTags          []string                      `json:"excludeTags"`          // 排除标签
	ExcludeTemplates     []string                      `json:"excludeTemplates"`     // 排除模板
	RateLimit            int                           `json:"rateLimit"`            // 速率限制
	Concurrency          int                           `json:"concurrency"`          // 并发数
	Timeout              int                           `json:"timeout"`              // 总超时时间(秒)，由调用方根据目标数量计算
	TargetTimeout        int                           `json:"targetTimeout"`        // 单个目标超时时间(秒)，默认600秒
	Retries              int                           `json:"retries"`              // 重试次数
	AutoScan             bool                          `json:"autoScan"`             // 基于自定义标签映射自动扫描
	AutomaticScan        bool                          `json:"automaticScan"`        // 基于Wappalyzer技术的自动扫描（nuclei -as）
	TagMappings          map[string][]string           `json:"tagMappings"`          // 应用名称到Nuclei标签的映射
	CustomTemplates      []string                      `json:"customTemplates"`      // 自定义模板内容(YAML)
	CustomPocOnly        bool                          `json:"customPocOnly"`        // 只使用自定义POC
	NucleiTemplates      []string                      `json:"nucleiTemplates"`      // 从数据库加载的Nuclei模板内容
	OnVulnerabilityFound func(vul *Vulnerability)      `json:"-"`                    // 发现漏洞时的回调函数
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
func (s *NucleiScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		WorkspaceId:     config.WorkspaceId,
		MainTaskId:      config.MainTaskId,
		Vulnerabilities: make([]*Vulnerability, 0),
	}

	// 解析选项
	opts := &NucleiOptions{
		Severity:      "critical,high,medium",
		RateLimit:     150,
		Concurrency:   25,
		Timeout:       600,  // 总超时默认10分钟
		TargetTimeout: 600,  // 单目标超时默认600秒
		Retries:       1,
	}
	if config.Options != nil {
		if o, ok := config.Options.(*NucleiOptions); ok {
			opts = o
		}
	}

	// 设置默认值
	if opts.TargetTimeout <= 0 {
		opts.TargetTimeout = 600
	}

	// 自动扫描模式1: 基于自定义标签映射
	if opts.AutoScan && opts.TagMappings != nil {
		autoTags := s.generateAutoTags(config.Assets, opts.TagMappings)
		if len(autoTags) > 0 {
			logx.Debugf("Auto-scan (custom mapping) generated tags: %v", autoTags)
			opts.Tags = append(opts.Tags, autoTags...)
		}
	}

	// 自动扫描模式2: 基于Wappalyzer内置映射（类似nuclei -as）
	if opts.AutomaticScan {
		wappalyzerTags := s.generateWappalyzerAutoTags(config.Assets)
		if len(wappalyzerTags) > 0 {
			logx.Debugf("Auto-scan (Wappalyzer) generated tags: %v", wappalyzerTags)
			opts.Tags = append(opts.Tags, wappalyzerTags...)
		}
	}

	// 去重标签
	if len(opts.Tags) > 0 {
		opts.Tags = utils.UniqueStrings(opts.Tags)
	}

	// 准备目标列表
	var targets []string
	if len(config.Targets) > 0 {
		// 直接使用配置中的目标URL（用于POC验证等场景）
		targets = config.Targets
	} else {
		// 从资产列表构建目标URL
		targets = s.prepareTargets(config.Assets)
	}
	if len(targets) == 0 {
		logx.Info("No targets for nuclei scan")
		return result, nil
	}

	logx.Infof("Nuclei: scanning %d targets, timeout %ds/target", len(targets), opts.TargetTimeout)

	// 处理自定义POC - 写入临时文件
	var customTemplatePaths []string
	var tempDir string
	var templateNames []string // 记录模板名称用于日志
	if len(opts.CustomTemplates) > 0 {
		var err error
		tempDir, err = os.MkdirTemp("", "nuclei-custom-*")
		if err != nil {
			logx.Errorf("Failed to create temp dir for custom templates: %v", err)
		} else {
			for i, content := range opts.CustomTemplates {
				templatePath := filepath.Join(tempDir, fmt.Sprintf("custom-%d.yaml", i))
				// 调试：输出模板内容的前200个字符
				contentPreview := content
				if len(contentPreview) > 200 {
					contentPreview = contentPreview[:200] + "..."
				}
				logx.Debugf("Custom template %d content preview: %s", i, contentPreview)
				
				if err := os.WriteFile(templatePath, []byte(content), 0644); err != nil {
					logx.Errorf("Failed to write custom template %d: %v", i, err)
					continue
				}
				logx.Debugf("Custom template %d written to: %s", i, templatePath)
				customTemplatePaths = append(customTemplatePaths, templatePath)
				// 尝试从内容中提取模板ID/名称
				templateName := extractTemplateId(content)
				if templateName == "" {
					templateName = fmt.Sprintf("custom-%d", i)
				}
				templateNames = append(templateNames, templateName)
			}
		}
		// 清理临时目录
		defer func() {
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
		}()
	}

	// 收集结果（使用map去重）
	var vuls []*Vulnerability
	seen := make(map[string]bool)

	// 日志辅助函数
	taskLog := func(level, format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger(level, format, args...)
		}
	}

	// 输出加载的POC的数量（这是传入的模板文件数，实际加载数可能因过滤而不同）
	taskLog("INFO", "POC templates: %d files", len(customTemplatePaths))

	// 串行扫描每个目标，每个目标独立超时
	for i, target := range targets {
		select {
		case <-ctx.Done():
			logx.Info("Nuclei scan cancelled by context")
			result.Vulnerabilities = vuls
			return result, ctx.Err()
		default:
		}

		logx.Debugf("Nuclei [%d/%d]: %s", i+1, len(targets), target)
		// 显示目标
		taskLog("INFO", "POC [%d/%d]: %s", i+1, len(targets), target)

		// 扫描单个目标（内部已处理超时，使用独立context避免任务间相互影响）
		targetVuls := s.scanSingleTarget(ctx, target, opts, customTemplatePaths, templateNames, config.TaskLogger)

		// 合并结果并去重
		for _, vul := range targetVuls {
			key := fmt.Sprintf("%s:%d:%s:%s", vul.Host, vul.Port, vul.PocFile, vul.Url)
			if !seen[key] {
				seen[key] = true
				vuls = append(vuls, vul)
				// 如果有回调函数，实时通知
				if opts.OnVulnerabilityFound != nil {
					opts.OnVulnerabilityFound(vul)
				}
			}
		}
	}

	result.Vulnerabilities = vuls
	logx.Infof("Nuclei: completed, found %d vulnerabilities", len(vuls))
	return result, nil
}

// scanSingleTarget 扫描单个目标
func (s *NucleiScanner) scanSingleTarget(ctx context.Context, target string, opts *NucleiOptions, customTemplatePaths []string, templateNames []string, taskLogger func(level, format string, args ...interface{})) []*Vulnerability {
	var vuls []*Vulnerability
	startTime := time.Now()

	// 构建Nuclei SDK选项（包含EnableMatcherStatus以获取所有模板执行结果）
	// 注意：为每个目标创建独立的context，避免一个任务超时影响其他任务
	// Nuclei SDK内部有全局状态，使用父context可能导致任务间相互影响
	engineCtx, engineCancel := context.WithTimeout(context.Background(), time.Duration(opts.TargetTimeout)*time.Second)
	defer engineCancel()

	nucleiOpts := s.buildNucleiOptions(opts, customTemplatePaths)

	// 创建Nuclei引擎 - 使用独立的context
	logx.Debugf("Creating nuclei engine for %s with timeout %ds", target, opts.TargetTimeout)
	ne, err := nuclei.NewNucleiEngineCtx(engineCtx, nucleiOpts...)
	if err != nil {
		logx.Errorf("Failed to create nuclei engine for %s: %v", target, err)
		if taskLogger != nil {
			taskLogger("ERROR", "POC不可用: 引擎初始化失败 - %v", err)
		}
		return vuls
	}
	defer ne.Close()

	// 启用请求/响应存储（用于证据链）
	if engineOpts := ne.Options(); engineOpts != nil {
		engineOpts.StoreResponse = true
	}

	// 加载模板
	logx.Debugf("Loading templates for %s", target)
	if err := ne.LoadAllTemplates(); err != nil {
		logx.Errorf("Failed to load templates for %s: %v", target, err)
		if taskLogger != nil {
			taskLogger("ERROR", "POC不可用: 模板解析失败 - %v", err)
		}
		// 如果加载失败，返回空结果
		return vuls
	}

	// 获取实际加载的模板数量
	loadedTemplates := ne.GetTemplates()
	actualTemplateCount := len(loadedTemplates)
	logx.Debugf("Loaded %d templates for %s (input: %d)", actualTemplateCount, target, len(customTemplatePaths))
	
	// 如果没有加载到任何模板，记录警告并返回
	if actualTemplateCount == 0 {
		logx.Errorf("No templates loaded for %s, skipping scan", target)
		if taskLogger != nil {
			taskLogger("ERROR", "POC不可用: 模板加载失败，请检查POC格式是否正确")
		}
		return vuls
	}
	
	// 输出实际加载的模板数量（可能因severity过滤而减少）
	if taskLogger != nil {
		taskLogger("INFO", "  Loaded %d templates (filtered by severity)", actualTemplateCount)
	}

	// 加载单个目标
	ne.LoadTargets([]string{target}, false)

	// 记录扫描进度 - 使用实际加载的模板数量
	templateCount := actualTemplateCount
	if templateCount == 0 {
		templateCount = len(customTemplatePaths) // 回退到传入的数量
	}
	scannedCount := 0
	foundCount := 0
	
	// 用于去重同一模板的多次匹配（如 http-missing-security-headers 会匹配多个 header）
	seenTemplates := make(map[string]bool)

	logx.Debugf("Starting scan for %s with %d templates", target, templateCount)

	// 监听父context取消（任务被停止）
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// 父context取消，取消引擎context
			logx.Infof("Nuclei: parent context cancelled for %s, reason: %v", target, ctx.Err())
			if taskLogger != nil {
				taskLogger("WARN", "POC scan interrupted: %v", ctx.Err())
			}
			engineCancel()
		case <-done:
			return
		}
	}()
	defer close(done)

	// 执行扫描并通过回调收集结果
	// 使用EnableMatcherStatus后，回调会在每个模板执行完成后触发（无论是否匹配）
	err = ne.ExecuteCallbackWithCtx(engineCtx, func(event *output.ResultEvent) {
		scannedCount++
		
		// 判断是否匹配成功（发现漏洞）
		if event.Matched != "" {
			// 使用 TemplateID + MatcherName 作为去重key
			// 同一模板可能有多个matcher（如检测多个header），每个matcher匹配都会触发回调
			vulKey := event.TemplateID + ":" + event.MatcherName
			if !seenTemplates[vulKey] {
				seenTemplates[vulKey] = true
				foundCount++
				logx.Debugf("Nuclei result: TemplateID=%s, Host=%s, Matched=%s, Matcher=%s",
					event.TemplateID, event.Host, event.Matched, event.MatcherName)

				// 发现漏洞时输出日志
				if taskLogger != nil {
					if event.MatcherName != "" {
						taskLogger("INFO", "  [%d/%d] ✓ %s:%s [%s]", scannedCount, templateCount, event.TemplateID, event.MatcherName, event.Info.SeverityHolder.Severity.String())
					} else {
						taskLogger("INFO", "  [%d/%d] ✓ %s [%s]", scannedCount, templateCount, event.TemplateID, event.Info.SeverityHolder.Severity.String())
					}
				}

				vul := s.convertResult(event)
				if vul != nil {
					vuls = append(vuls, vul)
				}
			}
		} else {
			// 未匹配，显示扫描进度（每50个模板或最后一个显示一次）
			if taskLogger != nil && (scannedCount%50 == 0 || scannedCount == templateCount) {
				taskLogger("INFO", "  [%d/%d] Scanning... %d vuls found", scannedCount, templateCount, foundCount)
			}
		}
	})

	// 详细的错误日志
	if err != nil {
		if engineCtx.Err() == context.DeadlineExceeded {
			logx.Infof("Nuclei: %s timeout after %ds", target, opts.TargetTimeout)
			if taskLogger != nil {
				taskLogger("WARN", "POC scan timeout after %ds", opts.TargetTimeout)
			}
		} else if engineCtx.Err() == context.Canceled {
			logx.Infof("Nuclei: %s cancelled", target)
			if taskLogger != nil {
				taskLogger("WARN", "POC scan cancelled")
			}
		} else {
			logx.Errorf("Nuclei scan error for %s: %v", target, err)
			if taskLogger != nil {
				taskLogger("ERROR", "POC scan error: %v", err)
			}
		}
	} else if engineCtx.Err() == context.DeadlineExceeded {
		// ExecuteCallbackWithCtx 可能在超时时返回 nil error
		logx.Infof("Nuclei: %s timeout after %ds (no error returned)", target, opts.TargetTimeout)
		if taskLogger != nil {
			taskLogger("WARN", "POC scan timeout after %ds", opts.TargetTimeout)
		}
	}

	// 扫描完成后输出统计
	elapsed := int(time.Since(startTime).Seconds())
	if taskLogger != nil {
		if scannedCount < templateCount {
			taskLogger("INFO", "  Completed: %d/%d templates (incomplete), %d vuls, %ds", scannedCount, templateCount, foundCount, elapsed)
		} else {
			taskLogger("INFO", "  Completed: %d templates, %d vuls, %ds", scannedCount, foundCount, elapsed)
		}
	}

	return vuls
}

// ScanBatch 批量扫描多个目标（使用单个Nuclei引擎实例）
// 适用于使用同一个POC扫描大量目标的场景
func (s *NucleiScanner) ScanBatch(ctx context.Context, targets []string, opts *NucleiOptions, taskLogger func(level, format string, args ...interface{})) ([]*Vulnerability, error) {
	var vuls []*Vulnerability
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

	taskLog("INFO", "Batch scan: %d targets", len(targets))

	// 处理自定义POC - 写入临时文件
	var customTemplatePaths []string
	var tempDir string
	if len(opts.CustomTemplates) > 0 {
		var err error
		tempDir, err = os.MkdirTemp("", "nuclei-batch-*")
		if err != nil {
			logx.Errorf("Failed to create temp dir for custom templates: %v", err)
			return nil, err
		}
		defer os.RemoveAll(tempDir)

		for i, content := range opts.CustomTemplates {
			templatePath := filepath.Join(tempDir, fmt.Sprintf("custom-%d.yaml", i))
			if err := os.WriteFile(templatePath, []byte(content), 0644); err != nil {
				logx.Errorf("Failed to write custom template %d: %v", i, err)
				continue
			}
			customTemplatePaths = append(customTemplatePaths, templatePath)
		}
		taskLog("INFO", "Loaded %d POC templates", len(customTemplatePaths))
	}

	if len(customTemplatePaths) == 0 {
		taskLog("ERROR", "No POC templates loaded")
		return nil, fmt.Errorf("no POC templates loaded")
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

	// 构建Nuclei SDK选项
	nucleiOpts := s.buildNucleiOptions(opts, customTemplatePaths)

	// 创建Nuclei引擎
	taskLog("INFO", "Initializing Nuclei engine...")
	ne, err := nuclei.NewNucleiEngineCtx(engineCtx, nucleiOpts...)
	if err != nil {
		taskLog("ERROR", "Failed to create nuclei engine: %v", err)
		return nil, err
	}
	defer ne.Close()

	// 启用请求/响应存储（用于证据链）
	if engineOpts := ne.Options(); engineOpts != nil {
		engineOpts.StoreResponse = true
	}

	// 加载模板
	taskLog("INFO", "Loading templates...")
	if err := ne.LoadAllTemplates(); err != nil {
		taskLog("ERROR", "Failed to load templates: %v", err)
		return nil, err
	}

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
	totalTasks := len(targets) * len(loadedTemplates)

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
	err = ne.ExecuteCallbackWithCtx(engineCtx, func(event *output.ResultEvent) {
		scannedCount++

		// 判断是否匹配成功
		if event.Matched != "" {
			vulKey := fmt.Sprintf("%s:%s:%s", event.Host, event.TemplateID, event.MatcherName)
			if !seen[vulKey] {
				seen[vulKey] = true
				foundCount++

				taskLog("INFO", "[%d/%d] ✓ %s - %s [%s]", scannedCount, totalTasks, event.Host, event.TemplateID, event.Info.SeverityHolder.Severity.String())

				vul := s.convertResult(event)
				if vul != nil {
					vuls = append(vuls, vul)
					// 实时回调
					if opts.OnVulnerabilityFound != nil {
						opts.OnVulnerabilityFound(vul)
					}
				}
			}
		} else {
			// 每100个任务或完成时显示进度
			if scannedCount%100 == 0 || scannedCount == totalTasks {
				taskLog("INFO", "[%d/%d] Scanning... %d vuls found", scannedCount, totalTasks, foundCount)
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

		target := fmt.Sprintf("%s://%s:%d", scheme, asset.Host, asset.Port)

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
func (s *NucleiScanner) buildNucleiOptions(opts *NucleiOptions, customTemplatePaths []string) []nuclei.NucleiSDKOptions {
	var nucleiOpts []nuclei.NucleiSDKOptions

	// 启用MatcherStatus，使回调在每个模板执行后都触发（无论是否匹配）
	// 这样可以实现实时进度显示
	nucleiOpts = append(nucleiOpts, nuclei.EnableMatcherStatus())

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

	// 模板过滤器 - 当使用数据库模板时，模板已经是筛选过的，跳过tag过滤
	filters := nuclei.TemplateFilters{}
	hasFilters := false

	// severity过滤仍然生效
	if opts.Severity != "" {
		filters.Severity = opts.Severity
		hasFilters = true
	}

	if hasFilters {
		nucleiOpts = append(nucleiOpts, nuclei.WithTemplateFilters(filters))
	}

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

	// 禁用更新检查
	nucleiOpts = append(nucleiOpts, nuclei.DisableUpdateCheck())

	return nucleiOpts
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
	vul := &Vulnerability{
		Authority: fmt.Sprintf("%s:%d", host, port),
		Host:      host,
		Port:      port,
		Url:       event.Matched,
		PocFile:   event.TemplateID,
		Source:    "nuclei",
		Severity:  event.Info.SeverityHolder.Severity.String(),
		Result:    resultDesc,
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


// ValidatePocTemplate 验证POC模板是否有效
// 使用Nuclei SDK加载模板，检查是否能正确解析
func ValidatePocTemplate(content string) error {
	if content == "" {
		return fmt.Errorf("POC内容不能为空")
	}

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
