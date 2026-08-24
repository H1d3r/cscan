package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/zeromicro/go-zero/core/logx"
	"gopkg.in/yaml.v3"
)

// VulEvidence 漏洞证据结构体
type VulEvidence struct {
	MatcherName       string   `json:"matcherName"`
	ExtractedResults  []string `json:"extractedResults"`
	CurlCommand       string   `json:"curlCommand"`
	Request           string   `json:"request"`
	Response          string   `json:"response"`
	ResponseTruncated bool     `json:"responseTruncated"`
}

func CollectEvidence(request, response, matcherName string, extractedResults, curlCommand []string) *VulEvidence {
	truncated := false
	resp := response
	if len(resp) > MaxResponseSize {
		resp = resp[:MaxResponseSize]
		truncated = true
	}
	return &VulEvidence{
		MatcherName:       matcherName,
		ExtractedResults:  extractedResults,
		CurlCommand:       strings.Join(curlCommand, "\n"),
		Request:           request,
		Response:          resp,
		ResponseTruncated: truncated,
	}
}

// MaxResponseSize 响应内容最大存储大小 (10KB)
const MaxResponseSize = 10 * 1024

// NucleiResultEvent 对应 nuclei CLI -jsonl 输出的 JSON 行结构
type NucleiResultEvent struct {
	TemplateID       string             `json:"template-id"`
	TemplateURL      string             `json:"template-url"`
	TemplatePath     string             `json:"template-path"`
	Type             string             `json:"type"`
	Host             string             `json:"host"`
	Port             string             `json:"port"`
	Scheme           string             `json:"scheme"`
	URL              string             `json:"url"`
	MatchedAt        string             `json:"matched-at"`
	MatcherStatus    bool               `json:"matcher-status"`
	MatcherName      string             `json:"matcher-name"`
	Timestamp        string             `json:"timestamp"`
	IP               string             `json:"ip"`
	CurlCommand      string             `json:"curl-command"`
	Request          string             `json:"request"`
	Response         string             `json:"response"`
	ExtractedResults []string           `json:"extracted-results"`
	Info             NucleiTemplateInfo `json:"info"`
	Template         struct {
		ID   string             `json:"id"`
		Info NucleiTemplateInfo `json:"info"`
	} `json:"template,omitempty"`
}

type NucleiTemplateInfo struct {
	Name           string   `json:"name"`
	Author         []string `json:"author"`
	Tags           []string `json:"tags"`
	Severity       string   `json:"severity"`
	Description    string   `json:"description"`
	Reference      []string `json:"reference"`
	Remediation    string   `json:"remediation"`
	Classification *struct {
		CveID     []string `json:"cve-id"`
		CweID     []string `json:"cwe-id"`
		CvssScore float64  `json:"cvss-score"`
	} `json:"classification,omitempty"`
	SeverityHolder struct {
		Severity string `json:"severity"`
	} `json:"severity_holder,omitempty"`
}

// NucleiScanner Nuclei扫描器 (CLI 模式)
type NucleiScanner struct {
	BaseScanner
	executor *CmdExecutor
}

// NewNucleiScanner 创建 Nuclei 扫描器
func NewNucleiScanner() *NucleiScanner {
	return &NucleiScanner{
		BaseScanner: BaseScanner{name: "nuclei"},
		executor:    NewExecutorForTool("nuclei"),
	}
}

// NucleiOptions Nuclei扫描选项
type NucleiOptions struct {
	Templates            []string                          `json:"templates"`
	Tags                 []string                          `json:"tags"`
	Severity             string                            `json:"severity"`
	ExcludeTags          []string                          `json:"excludeTags"`
	ExcludeTemplates     []string                          `json:"excludeTemplates"`
	RateLimit            int                               `json:"rateLimit"`
	Concurrency          int                               `json:"concurrency"`
	Timeout              int                               `json:"timeout"`
	TargetTimeout        int                               `json:"targetTimeout"`
	Retries              int                               `json:"retries"`
	AutoScan             bool                              `json:"autoScan"`
	AutomaticScan        bool                              `json:"automaticScan"`
	TagMappings          map[string][]string               `json:"tagMappings"`
	CustomTemplates      []string                          `json:"customTemplates"`
	// TemplateFileRefs 已落盘的模板文件路径（本地模板库内容寻址文件）。
	// 扫描时硬链接/复制进本次扫描目录，避免模板内容进内存后重复写盘
	TemplateFileRefs []string                          `json:"templateFileRefs"`
	CustomPocOnly        bool                              `json:"customPocOnly"`
	NucleiTemplates      []string                          `json:"nucleiTemplates"`
	CustomHeaders        []string                          `json:"customHeaders"`
	ForceScan            bool                              `json:"forceScan"`
	OnVulnerabilityFound func(vul *Vulnerability)          `json:"-"`
	OnProgress           func(percent int, message string) `json:"-"`
}

// Validate 验证配置
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
	if o.Severity != "" {
		validSeverities := map[string]bool{
			"critical": true, "high": true, "medium": true,
			"low": true, "info": true, "unknown": true,
		}
		for _, s := range strings.Split(o.Severity, ",") {
			s = strings.TrimSpace(strings.ToLower(s))
			if !validSeverities[s] {
				return fmt.Errorf("invalid severity '%s'", s)
			}
		}
	}
	return nil
}

// Scan 执行 Nuclei 扫描
func (s *NucleiScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		MainTaskId:      config.MainTaskId,
		Vulnerabilities: make([]*Vulnerability, 0),
	}

	opts := &NucleiOptions{
		Severity:      "critical,high,medium",
		RateLimit:     150,
		Concurrency:   25,
		Timeout:       600,
		TargetTimeout: 600,
		Retries:       1,
	}
	if config.Options != nil {
		if o, ok := config.Options.(*NucleiOptions); ok {
			opts = o
		}
	}

	if opts.Concurrency <= 0 {
		opts.Concurrency = 25
	}
	if opts.RateLimit <= 0 {
		opts.RateLimit = 150
	}
	if opts.TargetTimeout <= 0 {
		opts.TargetTimeout = 600
	}
	if opts.Timeout <= 0 {
		opts.Timeout = opts.TargetTimeout
	}
	if opts.Retries <= 0 {
		opts.Retries = 1
	}
	if opts.Concurrency > 50 {
		opts.Concurrency = 50
	}

	targets := prepareTargets(config.Assets, opts.ForceScan, config.TaskLogger)
	if len(targets) == 0 && len(config.Targets) > 0 {
		targets = config.Targets
	}

	logFn := func(level, format string, args ...interface{}) {
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

	if len(targets) == 0 {
		logFn("INFO", "No targets for nuclei scan")
		if config.TaskLogger != nil {
			config.TaskLogger("WARN", "No targets for nuclei scan (all assets were skipped as non-HTTP)")
		}
		return result, nil
	}

	if opts.AutoScan && opts.TagMappings != nil {
		if autoTags := generateAutoTags(config.Assets, opts.TagMappings); len(autoTags) > 0 {
			opts.Tags = append(opts.Tags, autoTags...)
		}
	}
	if opts.AutomaticScan {
		if wappTags := generateWappalyzerAutoTags(config.Assets); len(wappTags) > 0 {
			opts.Tags = append(opts.Tags, wappTags...)
		}
	}
	opts.Tags = utils.UniqueStrings(opts.Tags)

	logFn("INFO", "Nuclei: Starting scan for %d targets (CLI mode)", len(targets))

	customTemplatePaths, err := s.prepareTemplates(opts, logFn)
	if err != nil {
		return nil, fmt.Errorf("prepare templates: %w", err)
	}
	defer cleanupTemplatePaths(customTemplatePaths)

	// 并发 Worker Pool：每个目标一个 nuclei 进程，完成一个补一个
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
	logFn("INFO", "Nuclei: scanning %d targets with %d workers (CLI mode)", len(targets), concurrency)

	type targetResult struct {
		vuls []*Vulnerability
		err  error
	}
	targetChan := make(chan string, len(targets))
	resultChan := make(chan targetResult, len(targets))
	var scanWg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for target := range targetChan {
				select {
				case <-ctx.Done():
					resultChan <- targetResult{err: ctx.Err()}
					return
				default:
				}
				vuls, err := s.scanSingleTargetCLI(ctx, target, opts, customTemplatePaths, config.TaskLogger)
				resultChan <- targetResult{vuls: vuls, err: err}
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

	seen := make(map[string]bool)
	for res := range resultChan {
		if res.err != nil {
			logFn("ERROR", "Nuclei: scan error: %v", res.err)
			continue
		}
		for _, vul := range res.vuls {
			vulKey := fmt.Sprintf("%s:%s:%s", vul.Url, vul.PocFile, vul.MatcherName)
			if !seen[vulKey] {
				seen[vulKey] = true
				result.Vulnerabilities = append(result.Vulnerabilities, vul)
				if opts.OnVulnerabilityFound != nil {
					opts.OnVulnerabilityFound(vul)
				}
			}
		}
	}

	logFn("INFO", "Nuclei: Completed, found %d vulnerabilities", len(result.Vulnerabilities))
	return result, nil
}

// ScanBatch 对一批目标 URL 执行批量 POC 扫描（单进程 CLI 模式）
// 注入自定义 POC 模板，将全部目标写入临时列表文件后启动【单个】nuclei 进程（-list）批量扫描，
// 由 nuclei 进程内部按 -concurrency 并发消化目标；流式读取 -jsonl 输出，
// 通过 OnVulnerabilityFound 回调实时上报并入库。
func (s *NucleiScanner) ScanBatch(ctx context.Context, targets []string, opts *NucleiOptions, taskLogger func(level, format string, args ...interface{})) ([]*Vulnerability, error) {
	taskLog := func(level, format string, args ...interface{}) {
		if taskLogger != nil {
			taskLogger(level, format, args...)
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

	if len(targets) == 0 {
		return nil, nil
	}

	if opts.Concurrency <= 0 {
		opts.Concurrency = 25
	}
	if opts.RateLimit <= 0 {
		opts.RateLimit = 150
	}
	if opts.TargetTimeout <= 0 {
		opts.TargetTimeout = 600
	}
	if opts.Timeout <= 0 {
		opts.Timeout = opts.TargetTimeout
	}
	if opts.Retries <= 0 {
		opts.Retries = 1
	}
	if opts.Concurrency > 50 {
		opts.Concurrency = 50
	}

	taskLog("INFO", "Batch scan: %d targets (single-process CLI mode)", len(targets))

	customTemplatePaths, err := s.prepareTemplates(opts, taskLog)
	if err != nil {
		return nil, fmt.Errorf("prepare templates: %w", err)
	}
	defer cleanupTemplatePaths(customTemplatePaths)
	if len(customTemplatePaths) == 0 {
		taskLog("ERROR", "No usable POC templates loaded")
		return nil, fmt.Errorf("no usable POC templates")
	}
	templateDir := filepath.Dir(customTemplatePaths[0])

	// 将全部目标写入临时列表文件，交给单个 nuclei 进程 -list 批量消化，
	// 由 nuclei 进程内部按 -concurrency 并发，而非每目标一个进程。
	targetFile, err := os.CreateTemp("", "nuclei-targets-*.txt")
	if err != nil {
		return nil, fmt.Errorf("create target list file: %w", err)
	}
	targetFilePath := targetFile.Name()
	defer os.Remove(targetFilePath)
	if _, err := targetFile.WriteString(strings.Join(targets, "\n") + "\n"); err != nil {
		targetFile.Close()
		return nil, fmt.Errorf("write target list file: %w", err)
	}
	targetFile.Close()

	// 进程超时 = 目标数 × POC模板数 × 30s（每 POC×目标 30s 累加），下限 60s，上限 12h 封顶
	const perUnitSeconds = 30
	totalSeconds := len(targets) * len(customTemplatePaths) * perUnitSeconds
	if totalSeconds < 60 {
		totalSeconds = 60
	}
	if totalSeconds > 43200 { // 12h
		totalSeconds = 43200
	}
	processTimeout := time.Duration(totalSeconds) * time.Second

	args := []string{
		"-list", targetFilePath,
		"-jsonl",
		"-silent",
		"-timeout", fmt.Sprintf("%d", opts.TargetTimeout),
		"-retries", fmt.Sprintf("%d", opts.Retries),
		"-rate-limit", fmt.Sprintf("%d", opts.RateLimit),
		"-concurrency", fmt.Sprintf("%d", opts.Concurrency),
		"-bulk-size", "25",
		"-disable-update-check",
		"-t", templateDir,
	}
	for _, header := range opts.CustomHeaders {
		args = append(args, "-header", header)
	}

	taskLog("INFO", "Nuclei: scanning %d targets in single process (templates=%d, concurrency=%d, rate=%d, timeout=%v)",
		len(targets), len(customTemplatePaths), opts.Concurrency, opts.RateLimit, processTimeout)
	taskLog("INFO", "Nuclei CLI: command: %s", s.executor.CommandLine(args))

	var allVuls []*Vulnerability
	seen := make(map[string]bool)

	// 流式读取 nuclei -jsonl 输出，边扫边通过 OnVulnerabilityFound 实时入库
	streamErr := s.executor.StreamLines(ctx, args, func(line string) (bool, error) {
		line = strings.TrimSpace(line)
		if line == "" {
			return true, nil
		}
		var event NucleiResultEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			taskLog("WARN", "Nuclei CLI: json unmarshal skipped: %v | line=%s", err, line)
			return true, nil
		}
		if !event.MatcherStatus {
			return true, nil
		}
		vul := s.convertCLIResult(&event, event.URL)
		if vul == nil {
			return true, nil
		}
		vulKey := fmt.Sprintf("%s:%s:%s", vul.Url, vul.PocFile, vul.MatcherName)
		if seen[vulKey] {
			return true, nil
		}
		seen[vulKey] = true
		allVuls = append(allVuls, vul)
		taskLog("INFO", "  Found: %s - %s [%s] -> %s",
			event.TemplateID, event.Info.Name, event.Info.Severity, vul.Url)
		if opts.OnVulnerabilityFound != nil {
			opts.OnVulnerabilityFound(vul)
		}
		return true, nil
	}, ExecuteOpts{Timeout: processTimeout, LogFn: taskLog})

	if streamErr != nil {
		taskLog("WARN", "Nuclei batch scan stream error: %v", streamErr)
	}

	taskLog("INFO", "Batch scan completed, %d targets, %d vuls", len(targets), len(allVuls))
	return allVuls, streamErr
}

func (s *NucleiScanner) scanSingleTargetCLI(ctx context.Context, target string, opts *NucleiOptions, templatePaths []string, taskLogger func(level, format string, args ...interface{})) ([]*Vulnerability, error) {
	if len(templatePaths) == 0 {
		return nil, fmt.Errorf("no templates available")
	}

	if taskLogger == nil {
		taskLogger = func(level, format string, args ...interface{}) {
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
	logFn := taskLogger

	templateDir := ""
	if len(templatePaths) > 0 {
		templateDir = filepath.Dir(templatePaths[0])
	}

	args := []string{
		"-target", target,
		"-jsonl",
		"-silent",
		"-timeout", fmt.Sprintf("%d", opts.TargetTimeout),
		"-retries", fmt.Sprintf("%d", opts.Retries),
		"-rate-limit", fmt.Sprintf("%d", opts.RateLimit),
		"-concurrency", fmt.Sprintf("%d", opts.Concurrency),
		"-bulk-size", "25",
		"-disable-update-check",
	}
	if templateDir != "" {
		args = append(args, "-t", templateDir)
	}
	// 不传 -tags/-severity：-t 目录内已是本次扫描的精确模板集合（载入时已按条件过滤），
	// 运行时再按模板 YAML 元数据过滤会误删所选模板（如手动选择的低危模板、
	// 未在 YAML 中声明 tags 的自定义 POC）。
	for _, header := range opts.CustomHeaders {
		args = append(args, "-header", header)
	}

	taskLogger("INFO", "Nuclei CLI: scanning %s (templates=%d, concurrency=%d, rate=%d)",
		target, len(templatePaths), opts.Concurrency, opts.RateLimit)
	taskLogger("INFO", "Nuclei CLI: command: %s", s.executor.CommandLine(args))

	// 进程超时 = 目标超时 + 30s 缓冲
	processTimeout := time.Duration(opts.TargetTimeout+30) * time.Second
	taskLogger("INFO", "Nuclei CLI: ProcessTimeout=%v (target timeout + 30s buffer)", processTimeout)

	res, err := s.executor.Execute(ctx, args, ExecuteOpts{
		Timeout: processTimeout,
		LogFn:   logFn,
	})
	taskLogger("INFO", "Nuclei CLI: target=%s exitCode=%d stdout_len=%d stderr=%q err=%v",
		target, res.ExitCode, len(res.Stdout), strings.TrimSpace(res.Stderr), err)
	if err != nil {
		taskLogger("WARN", "Nuclei CLI: %s execution error: %v", target, err)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if len(res.Stderr) > 0 {
		taskLogger("WARN", "Nuclei CLI: %s stderr detail: %s", target, strings.TrimSpace(res.Stderr))
	}
	if res.ExitCode != 0 && len(res.Stdout) > 0 {
		taskLogger("WARN", "Nuclei CLI: %s stdout detail: %s", target, strings.TrimSpace(res.Stdout))
	}

	var vuls []*Vulnerability
	scanner := newLineScanner(res.Stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event NucleiResultEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			taskLogger("WARN", "Nuclei CLI: json unmarshal skipped: %v | line=%s", err, line)
			continue
		}
		if !event.MatcherStatus {
			continue
		}
		vul := s.convertCLIResult(&event, target)
		if vul != nil {
			vuls = append(vuls, vul)
			taskLogger("INFO", "  Found: %s - %s [%s]",
				event.TemplateID, event.Info.Name, event.Info.Severity)
		}
	}

	taskLogger("INFO", "Nuclei: %s -> %d vulnerabilities", target, len(vuls))
	return vuls, nil
}

func (s *NucleiScanner) convertCLIResult(event *NucleiResultEvent, target string) *Vulnerability {
	if event == nil {
		return nil
	}

	matchedAt := event.MatchedAt
	if matchedAt == "" {
		matchedAt = event.URL
	}
	host, port := parseHostPort(matchedAt)
	if host == "" {
		host, port = parseHostPort(target)
	}

	info := event.Info
	if info.Name == "" {
		info = NucleiTemplateInfo{
			Name:           event.Template.Info.Name,
			Tags:           event.Template.Info.Tags,
			Severity:       event.Template.Info.Severity,
			Description:    event.Template.Info.Description,
			Reference:      event.Template.Info.Reference,
			Remediation:    event.Template.Info.Remediation,
			Classification: event.Template.Info.Classification,
			SeverityHolder: event.Template.Info.SeverityHolder,
		}
	}

	resultDesc := info.Name
	if info.Description != "" {
		resultDesc += "\n" + info.Description
	}
	if len(event.ExtractedResults) > 0 {
		resultDesc += "\nExtracted: " + strings.Join(event.ExtractedResults, ", ")
	}

	response := event.Response
	if len(response) > MaxResponseSize {
		response = response[:MaxResponseSize]
	}

	vul := &Vulnerability{
		Authority:        utils.BuildTargetWithPort(host, port),
		Host:             host,
		Port:             port,
		Url:              matchedAt,
		PocFile:          event.TemplateID,
		Source:           "nuclei",
		Severity:         info.Severity,
		Result:           resultDesc,
		VulName:          info.Name,
		Tags:             info.Tags,
		MatcherName:      event.MatcherName,
		ExtractedResults: event.ExtractedResults,
		CurlCommand:      event.CurlCommand,
		Request:          event.Request,
		Response:         response,
	}

	if info.Classification != nil {
		vul.CvssScore = info.Classification.CvssScore
		if len(info.Classification.CveID) > 0 {
			vul.CveId = info.Classification.CveID[0]
		}
		if len(info.Classification.CweID) > 0 {
			vul.CweId = info.Classification.CweID[0]
		}
	}
	if len(info.Reference) > 0 {
		vul.References = info.Reference
	}
	vul.Remediation = info.Remediation

	return vul
}

func (s *NucleiScanner) prepareTemplates(opts *NucleiOptions, logFn func(level, format string, args ...interface{})) ([]string, error) {
	if logFn == nil {
		logFn = func(level, format string, args ...interface{}) {
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

	allTemplateContents := make([]string, 0, len(opts.CustomTemplates)+len(opts.NucleiTemplates))
	allTemplateContents = append(allTemplateContents, opts.CustomTemplates...)
	allTemplateContents = append(allTemplateContents, opts.NucleiTemplates...)
	refs := opts.TemplateFileRefs

	if len(allTemplateContents) == 0 && len(refs) == 0 {
		return nil, nil
	}

	// 每次扫描写入独立临时目录，-t 目录即本次扫描的精确模板集合。
	// 不能复用共享缓存目录：其 LRU 容量上限会在模板数超限时删除已写入的文件，
	// 导致大量模板被静默丢弃（如手动全选数千模板），且并发任务的模板会互相混入。
	scanDir, err := os.MkdirTemp("", "nuclei-scan-templates-*")
	if err != nil {
		return nil, fmt.Errorf("create scan template dir: %w", err)
	}

	paths := make([]string, 0, len(allTemplateContents)+len(refs))
	for i, content := range allTemplateContents {
		templateID, err := preValidateTemplate(content)
		if err != nil {
			logFn("WARN", "Skip bad template index=%d: %v", i, err)
			continue
		}
		h := sha256.Sum256([]byte(content))
		path := filepath.Join(scanDir, fmt.Sprintf("%05d-%s.yaml", len(paths), hex.EncodeToString(h[:8])))
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			logFn("ERROR", "Failed to write template %d (%s): %v", i, templateID, err)
			continue
		}
		paths = append(paths, path)
	}
	// 本地模板库文件：硬链接进扫描目录（零数据拷贝），文件系统不支持时回退复制
	for _, ref := range refs {
		if _, err := os.Stat(ref); err != nil {
			logFn("WARN", "Template store file missing: %s", ref)
			continue
		}
		dst := filepath.Join(scanDir, fmt.Sprintf("%05d-%s.yaml", len(paths), filepath.Base(ref)))
		if err := os.Link(ref, dst); err != nil {
			if err := copyFile(ref, dst); err != nil {
				logFn("ERROR", "Failed to link/copy template %s: %v", ref, err)
				continue
			}
		}
		paths = append(paths, dst)
	}
	// 全部模板校验/写入失败时回收空目录，避免遗留孤儿目录
	if len(paths) == 0 {
		os.RemoveAll(scanDir)
	}
	return paths, nil
}

// copyFile 流式复制文件（硬链接不可用时的回退路径）
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// cleanupTemplatePaths 删除本次扫描的临时模板目录（整轮扫描结束后由调用方 defer 触发）
func cleanupTemplatePaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	os.RemoveAll(filepath.Dir(paths[0]))
}

// ValidatePocTemplate 验证POC模板（CLI 模式）
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

	tmpDir, err := os.MkdirTemp("", "nuclei-validate-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templatePath := filepath.Join(tmpDir, "template.yaml")
	if err := os.WriteFile(templatePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %v", err)
	}

	executor := NewCmdExecutor("nuclei", 256, 30*time.Second)
	args := []string{
		"-target", "http://example.com",
		"-t", tmpDir,
		"-silent",
		"-timeout", "10",
		"-disable-update-check",
	}
	_, execErr := executor.Execute(context.Background(), args, ExecuteOpts{Timeout: 30 * time.Second})
	if execErr != nil {
		return fmt.Errorf("POC验证失败: %w", execErr)
	}
	return nil
}

// --- Helper types and functions ---

type templateMeta struct {
	index      int
	templateID string
	path       string
}

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

// parseAppName 解析应用名称，去除来源标识与版本号。
// 注意：同名实现存在于 worker_auto_tag.go（worker 包），跨包重复，后续可抽到公共 pkg。
func parseAppName(app string) string {
	appName := app
	if idx := strings.Index(appName, "["); idx > 0 {
		appName = appName[:idx]
	}
	if idx := strings.Index(appName, ":"); idx > 0 {
		appName = appName[:idx]
	}
	return strings.TrimSpace(appName)
}

func prepareTargets(assets []*Asset, forceScan bool, taskLogger func(level, format string, args ...interface{})) []string {
	logFn := func(level, format string, args ...interface{}) {
		if taskLogger != nil {
			taskLogger(level, format, args...)
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

	targets := make([]string, 0, len(assets))
	seen := make(map[string]bool)
	skipped := 0
	var skippedDetails []string

	for _, asset := range assets {
		isHTTP := asset.IsHTTP || IsHTTPService(asset.Service, asset.Port)
		if !forceScan && !isHTTP {
			skipped++
			detail := fmt.Sprintf("%s:%d (service=%s)", asset.Host, asset.Port, asset.Service)
			skippedDetails = append(skippedDetails, detail)
			logFn("DEBUG", "Skipping non-HTTP asset: %s", detail)
			continue
		}

		scheme := "http"
		if asset.Service == "https" || asset.Port == 443 || asset.Port == 8443 {
			scheme = "https"
		}

		var target string
		if asset.Path != "" && asset.Path != "/" {
			path := asset.Path
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			target = fmt.Sprintf("%s://%s:%d%s", scheme, asset.Host, asset.Port, path)
		} else {
			target = fmt.Sprintf("%s://%s:%d", scheme, asset.Host, asset.Port)
		}

		if !seen[target] {
			seen[target] = true
			targets = append(targets, target)
		}
	}

	if skipped > 0 {
		if forceScan {
			logFn("INFO", "Force scan enabled: processing %d assets", len(targets))
		} else {
			logFn("INFO", "Nuclei: skipped %d non-HTTP assets, scanning %d HTTP targets", skipped, len(targets))
			if len(skippedDetails) <= 10 {
				logFn("INFO", "Skipped non-HTTP assets: %s", strings.Join(skippedDetails, ", "))
			} else {
				logFn("INFO", "Skipped non-HTTP assets (first 10): %s", strings.Join(skippedDetails[:10], ", "))
			}
		}
	} else if forceScan && len(assets) > 0 {
		logFn("INFO", "Force scan enabled: all %d assets will be scanned", len(assets))
	}

	return targets
}

func generateAutoTags(assets []*Asset, tagMappings map[string][]string) []string {
	tagSet := make(map[string]bool)
	for _, asset := range assets {
		for _, app := range asset.App {
			appName := parseAppName(app)
			for mappedApp, tags := range tagMappings {
				if strings.ToLower(mappedApp) == strings.ToLower(appName) {
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

func generateWappalyzerAutoTags(assets []*Asset) []string {
	tagSet := make(map[string]bool)
	for _, asset := range assets {
		for _, app := range asset.App {
			appName := parseAppName(app)
			if tags, ok := mapping.WappalyzerNucleiMapping[strings.ToLower(appName)]; ok {
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

func parseHostPort(rawURL string) (string, int) {
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return parseHostPortSimple(rawURL)
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

func parseHostPortSimple(hostPort string) (string, int) {
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

func extractMatchedReason(event *NucleiResultEvent) string {
	if event == nil {
		return "无匹配信息"
	}
	var reason string
	if len(event.ExtractedResults) > 0 {
		reason = "提取到特征: " + strings.Join(event.ExtractedResults, ", ")
	} else if event.MatcherName != "" {
		reason = "规则命中: " + event.MatcherName
	} else {
		reason = "基于请求响应特征匹配模板"
	}
	if event.MatcherStatus {
		reason += fmt.Sprintf(" (触点: %s)", event.MatchedAt)
	}
	return reason
}
