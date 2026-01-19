package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cscan/scanner"
	"cscan/scheduler"
)

// TaskRunnerIntegration 任务执行器集成
// 提供 Worker 与 TaskRunner 之间的桥接层，保持向后兼容
type TaskRunnerIntegration struct {
	worker     *Worker
	taskRunner *TaskRunner
}

// NewTaskRunnerIntegration 创建任务执行器集成
func NewTaskRunnerIntegration(worker *Worker) *TaskRunnerIntegration {
	return &TaskRunnerIntegration{
		worker:     worker,
		taskRunner: NewTaskRunner(worker.scanners, worker.logger),
	}
}

// GetTaskRunner 获取任务执行器
func (i *TaskRunnerIntegration) GetTaskRunner() *TaskRunner {
	return i.taskRunner
}

// ExecuteWithRunner 使用 TaskRunner 执行任务
// 这是一个可选的执行路径，用于简单任务
// 复杂任务仍然使用原有的 executeTask 方法
func (i *TaskRunnerIntegration) ExecuteWithRunner(ctx context.Context, task *scheduler.TaskInfo) (*TaskResult, error) {
	return i.taskRunner.Run(ctx, task, i.worker)
}

// CanUseRunner 检查任务是否可以使用 TaskRunner 执行
// 某些特殊任务类型（如 POC 验证）仍需使用原有逻辑
func (i *TaskRunnerIntegration) CanUseRunner(task *scheduler.TaskInfo) bool {
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		return false
	}

	// POC 验证任务使用原有逻辑
	taskType, _ := taskConfig["taskType"].(string)
	if taskType == "poc_validate" || taskType == "poc_batch_validate" {
		return false
	}

	return true
}

// DomainScanExecutor 子域名扫描阶段执行器
type DomainScanExecutor struct {
	worker *Worker
}

// NewDomainScanExecutor 创建子域名扫描执行器
func NewDomainScanExecutor(worker *Worker) *DomainScanExecutor {
	return &DomainScanExecutor{worker: worker}
}

// CanExecute 检查是否可以执行
func (e *DomainScanExecutor) CanExecute(ctx *TaskContext) bool {
	return ctx.Config.DomainScan != nil && ctx.Config.DomainScan.Enable
}

// Execute 执行子域名扫描
func (e *DomainScanExecutor) Execute(ctx *TaskContext) (*PhaseResult, error) {
	w := e.worker
	task := ctx.Task
	config := ctx.Config.DomainScan

	w.taskLog(task.TaskId, LevelInfo, "Starting domain scan...")

	// 创建任务日志回调
	domainTaskLogger := func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, format, args...)
	}

	// 获取 Subfinder 配置
	var providerConfig map[string][]string
	providerResp, err := w.httpClient.GetSubfinderProviders(ctx.Ctx, task.WorkspaceId)
	if err != nil {
		w.taskLog(task.TaskId, LevelWarn, "Failed to get subfinder providers: %v", err)
	} else if providerResp != nil && len(providerResp.Providers) > 0 {
		providerConfig = make(map[string][]string)
		for _, p := range providerResp.Providers {
			if len(p.Keys) > 0 {
				providerConfig[p.Provider] = p.Keys
			}
		}
		w.taskLog(task.TaskId, LevelInfo, "Loaded %d subfinder providers with keys", len(providerConfig))
	}

	// 构建 Subfinder 选项
	subfinderOpts := &scanner.SubfinderOptions{
		Timeout:            config.Timeout,
		MaxEnumerationTime: config.MaxEnumerationTime,
		Threads:            w.config.Concurrency,
		RateLimit:          config.RateLimit,
		Sources:            config.Sources,
		ExcludeSources:     config.ExcludeSources,
		All:                config.All,
		Recursive:          config.Recursive,
		RemoveWildcard:     config.RemoveWildcard,
		ResolveDNS:         config.ResolveDNS,
		Concurrent:         w.config.Concurrency * 10,
		ProviderConfig:     providerConfig,
	}

	// 设置默认值
	if subfinderOpts.Timeout <= 0 {
		subfinderOpts.Timeout = 30
	}
	if subfinderOpts.MaxEnumerationTime <= 0 {
		subfinderOpts.MaxEnumerationTime = 10
	}

	var subfinderAssets []*scanner.Asset
	var bruteforceAssets []*scanner.Asset

	// 执行 Subfinder（如果启用）
	if config.Subfinder {
		if s, ok := w.scanners["subfinder"]; ok {
			result, err := s.Scan(ctx.Ctx, &scanner.ScanConfig{
				Target:      ctx.Target,
				WorkspaceId: task.WorkspaceId,
				MainTaskId:  task.MainTaskId,
				Options:     subfinderOpts,
				TaskLogger:  domainTaskLogger,
			})

			if err != nil {
				w.taskLog(task.TaskId, LevelError, "Subfinder error: %v", err)
			} else if result != nil && len(result.Assets) > 0 {
				subfinderAssets = result.Assets
				w.taskLog(task.TaskId, LevelInfo, "Subfinder: found %d subdomains", len(result.Assets))
			}
		}
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx.Ctx, task.TaskId); ctrl == "STOP" {
		return &PhaseResult{Stopped: true}, nil
	} else if ctrl == "PAUSE" {
		return &PhaseResult{Paused: true, Assets: subfinderAssets}, nil
	}

	// 执行子域名暴力破解（如果配置了字典）
	if len(config.SubdomainDictIds) > 0 {
		bruteforceAssets = e.executeBruteforce(ctx, config, domainTaskLogger)
	}

	// 合并结果
	allAssets := e.mergeAssets(subfinderAssets, bruteforceAssets)

	// 保存结果
	if len(allAssets) > 0 {
		w.taskLog(task.TaskId, LevelInfo, "Saving %d subdomains to database", len(allAssets))
		w.saveAssetResult(ctx.Ctx, task.WorkspaceId, task.MainTaskId, ctx.OrgId, allAssets)
	}

	return &PhaseResult{Assets: allAssets}, nil
}

// executeBruteforce 执行子域名暴力破解
func (e *DomainScanExecutor) executeBruteforce(ctx *TaskContext, config *scheduler.DomainScanConfig, taskLogger func(string, string, ...interface{})) []*scanner.Asset {
	w := e.worker
	task := ctx.Task

	w.taskLog(task.TaskId, LevelInfo, "Starting subdomain bruteforce with %d dicts", len(config.SubdomainDictIds))

	// 获取字典内容
	dictResp, err := w.httpClient.GetSubdomainDicts(ctx.Ctx, config.SubdomainDictIds)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Bruteforce: get dicts failed: %v", err)
		return nil
	}
	if dictResp == nil || len(dictResp.Dicts) == 0 {
		return nil
	}

	// 合并所有字典内容
	allWords := e.mergeDictWords(dictResp.Dicts, task.TaskId)
	if len(allWords) == 0 {
		return nil
	}

	w.taskLog(task.TaskId, LevelInfo, "Bruteforce: total %d unique words", len(allWords))

	// 构建暴力破解选项
	bruteforceOpts := &scanner.SubdomainBruteforceOptions{
		Wordlist:       strings.Join(allWords, "\n"),
		Threads:        w.config.Concurrency * 2,
		Timeout:        config.BruteforceTimeout * 60, // 转换为秒
		WildcardFilter: config.RemoveWildcard,
		ResolveDNS:     config.ResolveDNS,
		Concurrent:     w.config.Concurrency * 10,
		// 引擎配置
		Engine:         config.BruteforceEngine,
		Bandwidth:      config.Bandwidth,
		Retry:          config.Retry,
		WildcardMode:   config.WildcardMode,
		// 增强功能配置
		RecursiveBrute: config.RecursiveBrute,
		RecursiveDepth: 2,
		WildcardDetect: config.WildcardDetect,
	}

	// 获取递归爆破字典
	if config.RecursiveBrute && len(config.RecursiveDictIds) > 0 {
		recursiveDictResp, err := w.httpClient.GetSubdomainDicts(ctx.Ctx, config.RecursiveDictIds)
		if err == nil && recursiveDictResp != nil && len(recursiveDictResp.Dicts) > 0 {
			recursiveWords := e.mergeDictWords(recursiveDictResp.Dicts, task.TaskId)
			if len(recursiveWords) > 0 {
				bruteforceOpts.RecursiveWordlist = strings.Join(recursiveWords, "\n")
				w.taskLog(task.TaskId, LevelInfo, "Bruteforce: recursive wordlist total %d unique words", len(recursiveWords))
			}
		}
	}

	// 执行暴力破解
	bruteScanner, ok := w.scanners["subdomain_bruteforce"]
	if !ok {
		w.taskLog(task.TaskId, LevelWarn, "Subdomain bruteforce scanner not available")
		return nil
	}

	bruteResult, err := bruteScanner.Scan(ctx.Ctx, &scanner.ScanConfig{
		Target:      ctx.Target,
		WorkspaceId: task.WorkspaceId,
		MainTaskId:  task.MainTaskId,
		Options:     bruteforceOpts,
		TaskLogger:  taskLogger,
	})

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Bruteforce error: %v", err)
		return nil
	}

	if bruteResult != nil && len(bruteResult.Assets) > 0 {
		w.taskLog(task.TaskId, LevelInfo, "Bruteforce: found %d subdomains", len(bruteResult.Assets))
		return bruteResult.Assets
	}

	return nil
}

// mergeDictWords 合并字典内容
func (e *DomainScanExecutor) mergeDictWords(dicts []SubdomainDictItem, taskId string) []string {
	wordSet := make(map[string]bool)
	var allWords []string

	for _, dict := range dicts {
		lines := strings.Split(dict.Content, "\n")
		for _, line := range lines {
			word := strings.TrimSpace(line)
			if word != "" && !strings.HasPrefix(word, "#") && !wordSet[word] {
				wordSet[word] = true
				allWords = append(allWords, word)
			}
		}
		e.worker.taskLog(taskId, LevelInfo, "Bruteforce: loaded dict '%s'", dict.Name)
	}

	return allWords
}

// mergeAssets 合并资产列表（去重）
func (e *DomainScanExecutor) mergeAssets(subfinderAssets, bruteforceAssets []*scanner.Asset) []*scanner.Asset {
	assetMap := make(map[string]*scanner.Asset)

	for _, asset := range subfinderAssets {
		if asset.Host != "" {
			assetMap[asset.Host] = asset
		}
	}
	for _, asset := range bruteforceAssets {
		if asset.Host != "" {
			if _, exists := assetMap[asset.Host]; !exists {
				assetMap[asset.Host] = asset
			}
		}
	}

	var result []*scanner.Asset
	for _, asset := range assetMap {
		result = append(result, asset)
	}
	return result
}

// PortScanExecutor 端口扫描阶段执行器
type PortScanExecutor struct {
	worker *Worker
}

// NewPortScanExecutor 创建端口扫描执行器
func NewPortScanExecutor(worker *Worker) *PortScanExecutor {
	return &PortScanExecutor{worker: worker}
}

// CanExecute 检查是否可以执行
func (e *PortScanExecutor) CanExecute(ctx *TaskContext) bool {
	return ctx.Config.PortScan != nil && ctx.Config.PortScan.Enable
}

// Execute 执行端口扫描
func (e *PortScanExecutor) Execute(ctx *TaskContext) (*PhaseResult, error) {
	w := e.worker
	task := ctx.Task
	config := ctx.Config.PortScan

	// 创建带超时的上下文
	portScanTimeout := 600
	if config.Timeout > 0 {
		portScanTimeout = config.Timeout * 100
		if portScanTimeout < 600 {
			portScanTimeout = 600
		}
	}
	portCtx, portCancel := context.WithTimeout(ctx.Ctx, time.Duration(portScanTimeout)*time.Second)
	defer portCancel()

	// 选择端口发现工具
	portDiscoveryTool := "naabu"
	if config.Tool != "" {
		portDiscoveryTool = config.Tool
	}

	// 创建任务日志回调
	taskLogger := func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, format, args...)
	}

	// 创建进度回调
	onProgress := func(progress int, message string) {
		w.updateTaskProgress(ctx.Ctx, task.TaskId, progress, message)
	}

	var openPorts []*scanner.Asset

	switch portDiscoveryTool {
	case "masscan":
		w.taskLog(task.TaskId, LevelInfo, "Port scan: Masscan")
		if s, ok := w.scanners["masscan"]; ok {
			result, err := s.Scan(portCtx, &scanner.ScanConfig{
				Target:     ctx.Target,
				Options:    config,
				TaskLogger: taskLogger,
				OnProgress: onProgress,
			})
			if err != nil {
				w.taskLog(task.TaskId, LevelError, "Masscan error: %v", err)
			}
			if result != nil && len(result.Assets) > 0 {
				openPorts = result.Assets
			}
		}
	default:
		w.taskLog(task.TaskId, LevelInfo, "Port scan: Naabu")
		if s, ok := w.scanners["naabu"]; ok {
			result, err := s.Scan(portCtx, &scanner.ScanConfig{
				Target:     ctx.Target,
				Options:    config,
				TaskLogger: taskLogger,
				OnProgress: onProgress,
			})
			if err != nil && err != scanner.ErrPortThresholdExceeded {
				w.taskLog(task.TaskId, LevelError, "Naabu error: %v", err)
			}
			if result != nil && len(result.Assets) > 0 {
				openPorts = result.Assets
			}
		}
	}

	// 检查控制信号
	if w.checkTaskControl(ctx.Ctx, task.TaskId) == "STOP" || ctx.Ctx.Err() != nil {
		return &PhaseResult{Stopped: true, Assets: openPorts}, nil
	}

	// 设置 IsHTTP 字段
	for _, asset := range openPorts {
		asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
	}

	// 保存结果
	if len(openPorts) > 0 {
		w.taskLog(task.TaskId, LevelInfo, "Port scan completed: %d assets", len(openPorts))
		w.saveAssetResult(ctx.Ctx, task.WorkspaceId, task.MainTaskId, ctx.OrgId, openPorts)
	} else {
		w.taskLog(task.TaskId, LevelInfo, "No open ports found")
	}

	return &PhaseResult{Assets: openPorts}, nil
}

// FingerprintExecutor 指纹识别阶段执行器
type FingerprintExecutor struct {
	worker *Worker
}

// NewFingerprintExecutor 创建指纹识别执行器
func NewFingerprintExecutor(worker *Worker) *FingerprintExecutor {
	return &FingerprintExecutor{worker: worker}
}

// CanExecute 检查是否可以执行
func (e *FingerprintExecutor) CanExecute(ctx *TaskContext) bool {
	return ctx.Config.Fingerprint != nil && ctx.Config.Fingerprint.Enable && len(ctx.Assets) > 0
}

// Execute 执行指纹识别
func (e *FingerprintExecutor) Execute(ctx *TaskContext) (*PhaseResult, error) {
	w := e.worker
	task := ctx.Task
	config := ctx.Config.Fingerprint

	if len(ctx.Assets) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "Fingerprint: skipped (no assets)")
		return &PhaseResult{}, nil
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx.Ctx, task.TaskId); ctrl == "STOP" {
		return &PhaseResult{Stopped: true}, nil
	} else if ctrl == "PAUSE" {
		return &PhaseResult{Paused: true}, nil
	}

	s, ok := w.scanners["fingerprint"]
	if !ok {
		return &PhaseResult{}, fmt.Errorf("fingerprint scanner not found")
	}

	// 获取超时配置
	targetTimeout := config.TargetTimeout
	if targetTimeout <= 0 {
		targetTimeout = 30
	}

	// 使用 Worker 并发数
	config.Concurrency = w.config.Concurrency
	w.taskLog(task.TaskId, LevelInfo, "Fingerprint: %d assets, timeout %ds/target, concurrency=%d",
		len(ctx.Assets), targetTimeout, w.config.Concurrency)

	// 加载 HTTP 服务映射配置
	w.loadHttpServiceMappings()

	// 如果启用自定义指纹引擎，加载自定义指纹
	if config.CustomEngine {
		w.loadCustomFingerprints(ctx.Ctx, s.(*scanner.FingerprintScanner), config.ActiveScan)
	}

	// 创建带超时的上下文
	fingerprintTimeout := config.Timeout
	if fingerprintTimeout <= 0 {
		fingerprintTimeout = 300
	}
	fpCtx, fpCancel := context.WithTimeout(ctx.Ctx, time.Duration(fingerprintTimeout)*time.Second)
	defer fpCancel()

	// 创建任务日志回调
	fpTaskLogger := func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, format, args...)
	}

	result, err := s.Scan(fpCtx, &scanner.ScanConfig{
		Assets:     ctx.Assets,
		Options:    config,
		TaskLogger: fpTaskLogger,
	})

	// 检查是否超时
	if fpCtx.Err() == context.DeadlineExceeded {
		w.taskLog(task.TaskId, LevelWarn, "Fingerprint scan timeout after %ds", fingerprintTimeout)
	}

	// 检查控制信号
	if ctx.Ctx.Err() != nil || w.checkTaskControl(ctx.Ctx, task.TaskId) == "STOP" {
		return &PhaseResult{Stopped: true}, nil
	}

	if err != nil {
		return &PhaseResult{Error: err}, err
	}

	// 更新资产信息
	if result != nil && len(result.Assets) > 0 {
		// 构建 Host:Port -> Asset 的映射
		assetMap := make(map[string]*scanner.Asset)
		for _, asset := range ctx.Assets {
			key := fmt.Sprintf("%s:%d", asset.Host, asset.Port)
			assetMap[key] = asset
		}

		// 通过 Host:Port 匹配来更新资产信息
		for _, fpAsset := range result.Assets {
			key := fmt.Sprintf("%s:%d", fpAsset.Host, fpAsset.Port)
			if originalAsset, ok := assetMap[key]; ok {
				originalAsset.Service = fpAsset.Service
				originalAsset.Title = fpAsset.Title
				originalAsset.App = fpAsset.App
				originalAsset.HttpStatus = fpAsset.HttpStatus
				originalAsset.HttpHeader = fpAsset.HttpHeader
				originalAsset.HttpBody = fpAsset.HttpBody
				originalAsset.Server = fpAsset.Server
				originalAsset.IconHash = fpAsset.IconHash
				originalAsset.Screenshot = fpAsset.Screenshot
			}
		}

		// 保存更新结果
		w.saveAssetResult(ctx.Ctx, task.WorkspaceId, task.MainTaskId, ctx.OrgId, ctx.Assets)
	}

	return &PhaseResult{}, nil
}

// PocScanExecutor POC扫描阶段执行器
type PocScanExecutor struct {
	worker *Worker
}

// NewPocScanExecutor 创建POC扫描执行器
func NewPocScanExecutor(worker *Worker) *PocScanExecutor {
	return &PocScanExecutor{worker: worker}
}

// CanExecute 检查是否可以执行
func (e *PocScanExecutor) CanExecute(ctx *TaskContext) bool {
	return ctx.Config.PocScan != nil && ctx.Config.PocScan.Enable && len(ctx.Assets) > 0
}

// Execute 执行POC扫描
func (e *PocScanExecutor) Execute(ctx *TaskContext) (*PhaseResult, error) {
	w := e.worker
	task := ctx.Task
	config := ctx.Config.PocScan

	if len(ctx.Assets) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "POC scan: skipped (no assets)")
		return &PhaseResult{}, nil
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx.Ctx, task.TaskId); ctrl == "STOP" {
		return &PhaseResult{Stopped: true}, nil
	} else if ctrl == "PAUSE" {
		return &PhaseResult{Paused: true}, nil
	}

	s, ok := w.scanners["nuclei"]
	if !ok {
		return &PhaseResult{}, fmt.Errorf("nuclei scanner not found")
	}

	// 获取超时配置
	pocTargetTimeout := config.TargetTimeout
	if pocTargetTimeout <= 0 {
		pocTargetTimeout = 600
	}
	w.taskLog(task.TaskId, LevelInfo, "POC scan: %d assets, timeout %ds/target", len(ctx.Assets), pocTargetTimeout)

	// 获取模板
	var templates []string
	var autoTags []string

	if len(config.NucleiTemplateIds) > 0 || len(config.CustomPocIds) > 0 {
		templates = w.getTemplatesByIds(ctx.Ctx, config.NucleiTemplateIds, config.CustomPocIds)
		w.taskLog(task.TaskId, LevelInfo, "Loaded %d POC templates", len(templates))
	} else if config.CustomPocOnly {
		// 只使用自定义POC模式，获取所有自定义POC
		severities := []string{}
		if config.Severity != "" {
			severities = strings.Split(config.Severity, ",")
		}
		templates = w.getAllCustomPocs(ctx.Ctx, severities)
		w.taskLog(task.TaskId, LevelInfo, "CustomPocOnly mode: loaded %d custom POC templates", len(templates))
	} else {
		var matchInfos []TagMatchInfo
		if config.AutoScan || config.AutomaticScan {
			autoTags, matchInfos = w.generateAutoTags(ctx.Assets, config)
			// 输出匹配信息日志
			for _, info := range matchInfos {
				sourceDesc := "自定义标签映射"
				if info.Source == "builtin" {
					sourceDesc = "内置映射"
				}
				w.taskLog(task.TaskId, LevelInfo, "Auto-scan matched: fingerprint [%s] -> tags %v (source: %s)", info.Fingerprint, info.Tags, sourceDesc)
			}
		}

		if len(autoTags) > 0 {
			severities := []string{}
			if config.Severity != "" {
				severities = strings.Split(config.Severity, ",")
			}
			templates = w.getTemplatesByTags(ctx.Ctx, autoTags, severities)
			w.taskLog(task.TaskId, LevelInfo, "Loaded %d POC templates", len(templates))
		} else {
			w.taskLog(task.TaskId, LevelWarn, "No POC templates configured, skipping POC scan")
			return &PhaseResult{}, nil
		}
	}

	if len(templates) == 0 {
		return &PhaseResult{}, nil
	}

	var allVuls []*scanner.Vulnerability
	var vulCount int

	// 创建漏洞缓冲区
	vulBuffer := NewVulnerabilityBuffer(1)

	// 计算总超时
	pocTimeout := pocTargetTimeout * len(ctx.Assets)
	if pocTimeout < 600 {
		pocTimeout = 600
	}
	pocCtx, pocCancel := context.WithTimeout(ctx.Ctx, time.Duration(pocTimeout)*time.Second)
	defer pocCancel()

	// 启动后台刷新协程
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pocCtx.Done():
				return
			case <-vulBuffer.flushChan:
				vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
					w.saveVulResult(ctx.Ctx, task.WorkspaceId, task.MainTaskId, vuls)
				})
			case <-ticker.C:
				vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
					w.saveVulResult(ctx.Ctx, task.WorkspaceId, task.MainTaskId, vuls)
				})
			}
		}
	}()

	// 构建 Nuclei 扫描选项
	taskIdForCallback := task.TaskId
	nucleiOpts := &scanner.NucleiOptions{
		Severity:        config.Severity,
		Tags:            autoTags,
		ExcludeTags:     config.ExcludeTags,
		RateLimit:       config.RateLimit,
		Concurrency:     config.Concurrency,
		Timeout:         pocTimeout,
		TargetTimeout:   pocTargetTimeout,
		AutoScan:        false,
		AutomaticScan:   false,
		CustomPocOnly:   config.CustomPocOnly,
		CustomTemplates: templates,
		TagMappings:     config.TagMappings,
		OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
			vulCount++
			w.taskLog(taskIdForCallback, LevelInfo, "Vulnerability found: %s → %s", vul.PocFile, vul.Url)
			vulBuffer.Add(vul)
		},
	}

	// 设置默认值
	if nucleiOpts.RateLimit == 0 {
		nucleiOpts.RateLimit = 150
	}
	if nucleiOpts.Concurrency == 0 {
		nucleiOpts.Concurrency = 25
	}

	// 创建任务日志回调
	pocTaskLogger := func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, format, args...)
	}

	result, err := s.Scan(pocCtx, &scanner.ScanConfig{
		Assets:     ctx.Assets,
		Options:    nucleiOpts,
		TaskLogger: pocTaskLogger,
	})

	// 刷新剩余漏洞
	vulBuffer.Flush(ctx.Ctx, func(vuls []*scanner.Vulnerability) {
		w.saveVulResult(ctx.Ctx, task.WorkspaceId, task.MainTaskId, vuls)
	})

	// 检查是否超时
	if pocCtx.Err() == context.DeadlineExceeded {
		w.taskLog(task.TaskId, LevelWarn, "POC scan timeout after %ds", pocTimeout)
	}

	// 检查控制信号
	if ctx.Ctx.Err() != nil || w.checkTaskControl(ctx.Ctx, task.TaskId) == "STOP" {
		return &PhaseResult{Stopped: true, Vulnerabilities: allVuls}, nil
	}

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "POC scan error: %v", err)
	}

	if result != nil {
		allVuls = append(allVuls, result.Vulnerabilities...)
		if vulCount > 0 {
			w.taskLog(task.TaskId, LevelInfo, "POC scan completed: found %d vulnerabilities", vulCount)
		}
	}

	return &PhaseResult{Vulnerabilities: allVuls}, nil
}

// PortIdentifyExecutor 端口识别阶段执行器
type PortIdentifyExecutor struct {
	worker *Worker
}

// NewPortIdentifyExecutor 创建端口识别执行器
func NewPortIdentifyExecutor(worker *Worker) *PortIdentifyExecutor {
	return &PortIdentifyExecutor{worker: worker}
}

// CanExecute 检查是否可以执行
func (e *PortIdentifyExecutor) CanExecute(ctx *TaskContext) bool {
	return ctx.Config.PortIdentify != nil && ctx.Config.PortIdentify.Enable && len(ctx.Assets) > 0
}

// Execute 执行端口识别
func (e *PortIdentifyExecutor) Execute(ctx *TaskContext) (*PhaseResult, error) {
	w := e.worker
	task := ctx.Task
	config := ctx.Config.PortIdentify

	if len(ctx.Assets) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "Port identify: skipped (no assets)")
		return &PhaseResult{}, nil
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx.Ctx, task.TaskId); ctrl == "STOP" {
		return &PhaseResult{Stopped: true}, nil
	} else if ctrl == "PAUSE" {
		return &PhaseResult{Paused: true}, nil
	}

	// 调用 Worker 的 executePortIdentify 方法
	identifiedAssets := w.executePortIdentify(ctx.Ctx, task, ctx.Assets, config)

	// 检查控制信号
	if ctx.Ctx.Err() != nil || w.checkTaskControl(ctx.Ctx, task.TaskId) == "STOP" {
		return &PhaseResult{Stopped: true, Assets: identifiedAssets}, nil
	}

	// 更新 ctx.Assets 为识别后的资产
	if len(identifiedAssets) > 0 {
		// 保存结果
		w.saveAssetResult(ctx.Ctx, task.WorkspaceId, task.MainTaskId, ctx.OrgId, identifiedAssets)
		// 返回更新后的资产列表
		return &PhaseResult{Assets: identifiedAssets}, nil
	}

	return &PhaseResult{}, nil
}

// DirScanExecutor 目录扫描阶段执行器
type DirScanExecutor struct {
	worker *Worker
}

// NewDirScanExecutor 创建目录扫描执行器
func NewDirScanExecutor(worker *Worker) *DirScanExecutor {
	return &DirScanExecutor{worker: worker}
}

// CanExecute 检查是否可以执行
func (e *DirScanExecutor) CanExecute(ctx *TaskContext) bool {
	return ctx.Config.DirScan != nil && ctx.Config.DirScan.Enable
}

// Execute 执行目录扫描
func (e *DirScanExecutor) Execute(ctx *TaskContext) (*PhaseResult, error) {
	w := e.worker
	task := ctx.Task
	config := ctx.Config.DirScan

	// 如果没有资产，尝试从目标生成 HTTP 资产
	assets := ctx.Assets
	if len(assets) == 0 {
		generatedAssets := w.generateHTTPAssetsFromTarget(ctx.Target)
		if len(generatedAssets) > 0 {
			assets = generatedAssets
			w.taskLog(task.TaskId, LevelInfo, "Dir scan: generated %d HTTP assets from target", len(assets))
		}
	}

	if len(assets) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "Dir scan: skipped (no assets)")
		return &PhaseResult{}, nil
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx.Ctx, task.TaskId); ctrl == "STOP" {
		return &PhaseResult{Stopped: true}, nil
	} else if ctrl == "PAUSE" {
		return &PhaseResult{Paused: true}, nil
	}

	// 调用 Worker 的 executeDirScan 方法
	dirScanAssets := w.executeDirScan(ctx.Ctx, task, assets, config, ctx.OrgId)

	// 检查控制信号
	if ctx.Ctx.Err() != nil || w.checkTaskControl(ctx.Ctx, task.TaskId) == "STOP" {
		// 注意：不返回 dirScanAssets，因为目录扫描结果不应该被添加到主资产列表
		return &PhaseResult{Stopped: true}, nil
	}

	if len(dirScanAssets) > 0 {
		w.taskLog(task.TaskId, LevelInfo, "Dir scan completed: found %d paths", len(dirScanAssets))
	}

	// 注意：返回空的 Assets，目录扫描结果（URL路径）不应该被当作 POC 扫描的目标
	// 目录扫描结果已在 executeDirScan 中通过 saveDirScanResults 保存到数据库
	return &PhaseResult{}, nil
}

// RegisterDefaultExecutors 注册默认阶段执行器
func (i *TaskRunnerIntegration) RegisterDefaultExecutors() {
	i.taskRunner.RegisterPhaseExecutor(PhaseDomainScan, NewDomainScanExecutor(i.worker))
	i.taskRunner.RegisterPhaseExecutor(PhasePortScan, NewPortScanExecutor(i.worker))
	i.taskRunner.RegisterPhaseExecutor(PhasePortIdentify, NewPortIdentifyExecutor(i.worker))
	i.taskRunner.RegisterPhaseExecutor(PhaseFingerprint, NewFingerprintExecutor(i.worker))
	i.taskRunner.RegisterPhaseExecutor(PhaseDirScan, NewDirScanExecutor(i.worker))
	i.taskRunner.RegisterPhaseExecutor(PhasePocScan, NewPocScanExecutor(i.worker))
}
