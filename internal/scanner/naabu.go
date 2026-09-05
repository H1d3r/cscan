package scanner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cscan/pkg/geolocation"
	"cscan/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

// ErrPortThresholdExceeded 端口阈值超过错误
var ErrPortThresholdExceeded = fmt.Errorf("port threshold exceeded")

var ipLocator = geolocation.NewIPLocator()

const (
	// DefaultPortScanTargetTimeoutSeconds 是单个目标完整端口扫描的默认硬上限。
	DefaultPortScanTargetTimeoutSeconds = 60
	// DefaultNaabuProbeTimeoutMilliseconds 是 naabu 单次端口探测的默认等待时间。
	DefaultNaabuProbeTimeoutMilliseconds = 1000
	maxNaabuProcessConcurrency           = 5
)

// naabuSem 已移除：全局并发限制统一在 CmdExecutor.Execute/StreamLines 中实现，
// 覆盖所有扫描模块（naabu/nmap/nuclei/httpx/ffuf/feroxbuster/subfinder/dnsx/fingerprintx）。

// NaabuScanner Naabu端口扫描器 (CLI 模式)
type NaabuScanner struct {
	BaseScanner
	skippedHosts []string
	mu           sync.Mutex
	executor     *CmdExecutor
}

// NewNaabuScanner 创建Naabu扫描器
func NewNaabuScanner() *NaabuScanner {
	return &NaabuScanner{
		BaseScanner:  BaseScanner{name: "naabu"},
		skippedHosts: make([]string, 0),
		executor:     NewExecutorForTool("naabu"),
	}
}

// EffectiveNaabuProcessConcurrency 返回单个子任务实际并行启动的 naabu 进程数。
func EffectiveNaabuProcessConcurrency(workers, targetCount int) int {
	if targetCount <= 0 {
		return 0
	}
	concurrency := workers
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > maxNaabuProcessConcurrency {
		concurrency = maxNaabuProcessConcurrency
	}
	if concurrency > targetCount {
		concurrency = targetCount
	}
	return concurrency
}

// CountNaabuProcessTargets 返回 Naabu 实际会启动的目标进程数。
// ParseTargetsForPortScan 会展开 CIDR/IP range；这里再按 host 去重，与 runNaabuCLI 保持一致。
func CountNaabuProcessTargets(target string) int {
	parsed := ParseTargetsForPortScan(target)
	seen := make(map[string]struct{}, len(parsed.WithoutPort)+len(parsed.WithPort))
	for _, host := range parsed.WithoutPort {
		if host != "" {
			seen[host] = struct{}{}
		}
	}
	for _, targetWithPort := range parsed.WithPort {
		if targetWithPort != nil && targetWithPort.Host != "" {
			seen[targetWithPort.Host] = struct{}{}
		}
	}
	return len(seen)
}

// NaabuOptions Naabu扫描选项
type NaabuOptions struct {
	Ports             string `json:"ports"`
	Rate              int    `json:"rate"`
	TargetTimeout     int    `json:"targetTimeout"`  // 单个目标完整扫描上限（秒）
	ProbeTimeoutMs    int    `json:"probeTimeoutMs"` // 单次端口探测等待时间（毫秒），传给 naabu -timeout
	ScanType          string `json:"scanType"`
	PortThreshold     int    `json:"portThreshold"`
	SkipHostDiscovery bool   `json:"skipHostDiscovery"`
	ExcludeCDN        bool   `json:"excludeCDN"`
	ExcludeHosts      string `json:"excludeHosts"`
	Retries           int    `json:"retries"`
	WarmUpTime        int    `json:"warmUpTime"`
	Workers           int    `json:"workers"`
	Verify            bool   `json:"verify"`
}

// Validate 验证配置
func (o *NaabuOptions) Validate() error {
	if o.Rate < 0 {
		return fmt.Errorf("rate must be non-negative, got %d", o.Rate)
	}
	if o.TargetTimeout < 0 {
		return fmt.Errorf("targetTimeout must be non-negative, got %d", o.TargetTimeout)
	}
	if o.ProbeTimeoutMs < 0 {
		return fmt.Errorf("probeTimeoutMs must be non-negative, got %d", o.ProbeTimeoutMs)
	}
	if o.PortThreshold < 0 {
		return fmt.Errorf("portThreshold must be non-negative, got %d", o.PortThreshold)
	}
	if o.Retries < 0 {
		return fmt.Errorf("retries must be non-negative, got %d", o.Retries)
	}
	if o.WarmUpTime < 0 {
		return fmt.Errorf("warmUpTime must be non-negative, got %d", o.WarmUpTime)
	}
	if o.Workers < 0 {
		return fmt.Errorf("workers must be non-negative, got %d", o.Workers)
	}
	if o.ScanType != "" && o.ScanType != "s" && o.ScanType != "c" {
		return fmt.Errorf("scanType must be 's' or 'c', got %s", o.ScanType)
	}
	return nil
}

// NaabuHostResult Naabu JSON 输出结构（扁平格式，每行一个端口）
type NaabuHostResult struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	TLS      bool   `json:"tls"`
}

// NaabuParseSource identifies the output material that was actually parsed.
type NaabuParseSource string

const (
	NaabuParseSourceFile       NaabuParseSource = "file"
	NaabuParseSourceStdout     NaabuParseSource = "stdout"
	NaabuParseSourceFileStdout NaabuParseSource = "file+stdout"
	NaabuParseSourceNone       NaabuParseSource = "none"
)

// NaabuParseStats accounts for all non-empty records from the selected source.
type NaabuParseStats struct {
	FileBytes, StdoutBytes, ParsedBytes                  int
	TotalLines, ValidLines, InvalidLines, DuplicateLines int
	AcceptedPorts                                        int
}

// NaabuTargetDiagnostic is the Naabu-specific evidence attached to the
// generic scanner diagnostic for a single target.
type NaabuTargetDiagnostic struct {
	Source          NaabuParseSource
	ProcessOutcome  string // success | timeout | exit_error | canceled
	ExitCode        int
	OutputFileEmpty bool
	DurationMs      int64
	Stats           NaabuParseStats
}

type naabuTargetResult struct {
	target            string
	assets            []*Asset
	thresholdExceeded bool
	diagnostic        NaabuTargetDiagnostic
}

// Scan 执行Naabu扫描
func (s *NaabuScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	s.mu.Lock()
	s.skippedHosts = s.skippedHosts[:0]
	s.mu.Unlock()

	logFn := func(level, format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger(level, format, args...)
			return
		}
		switch level {
		case "ERROR":
			logx.Errorf(format, args...)
		case "WARN":
			logx.Slowf(format, args...)
		case "DEBUG":
			logx.Debugf(format, args...)
		default:
			logx.Infof(format, args...)
		}
	}

	opts := &NaabuOptions{
		Ports:             "80,443,8080",
		Rate:              3000,
		TargetTimeout:     DefaultPortScanTargetTimeoutSeconds,
		ProbeTimeoutMs:    DefaultNaabuProbeTimeoutMilliseconds,
		ScanType:          "c",
		PortThreshold:     0,
		Retries:           2,
		WarmUpTime:        1,
		Workers:           50,
		Verify:            false,
		SkipHostDiscovery: true, // 默认跳过 ICMP 主机发现（域名/CDN 目标 ICMP 几乎总被丢弃）
	}
	if config.Options != nil {
		switch v := config.Options.(type) {
		case *NaabuOptions:
			opts = v
		case *PortScanOptions:
			if v.Ports != "" {
				opts.Ports = v.Ports
			}
			if v.Rate != 0 {
				opts.Rate = v.Rate
			}
			if v.TargetTimeout != 0 {
				opts.TargetTimeout = v.TargetTimeout
			} else if v.Timeout != 0 {
				// 兼容直接构造的旧 PortScanOptions；旧 timeout 只作为目标扫描上限。
				opts.TargetTimeout = v.Timeout
			}
			if v.ProbeTimeoutMs != 0 {
				opts.ProbeTimeoutMs = v.ProbeTimeoutMs
			}
			if v.PortThreshold != 0 {
				opts.PortThreshold = v.PortThreshold
			}
			if v.ScanType != "" {
				opts.ScanType = v.ScanType
			}
			opts.SkipHostDiscovery = v.SkipHostDiscovery
			opts.ExcludeCDN = v.ExcludeCDN
			opts.ExcludeHosts = v.ExcludeHosts
			opts.Retries = v.Retries
			opts.WarmUpTime = v.WarmUpTime
			if v.Workers != 0 {
				opts.Workers = v.Workers
			}
			opts.Verify = v.Verify
		default:
			if data, err := json.Marshal(config.Options); err == nil {
				var portConfig struct {
					Ports             string `json:"ports"`
					Rate              int    `json:"rate"`
					TargetTimeout     int    `json:"targetTimeout"`
					ProbeTimeoutMs    int    `json:"probeTimeoutMs"`
					LegacyTimeout     int    `json:"timeout"`
					PortThreshold     int    `json:"portThreshold"`
					ScanType          string `json:"scanType"`
					SkipHostDiscovery *bool  `json:"skipHostDiscovery"`
					ExcludeCDN        *bool  `json:"excludeCDN"`
					ExcludeHosts      string `json:"excludeHosts"`
					Retries           *int   `json:"retries"`
					WarmUpTime        *int   `json:"warmUpTime"`
					Workers           int    `json:"workers"`
					Verify            *bool  `json:"verify"`
				}
				if err := json.Unmarshal(data, &portConfig); err == nil {
					if portConfig.Ports != "" {
						opts.Ports = portConfig.Ports
					}
					if portConfig.Rate != 0 {
						opts.Rate = portConfig.Rate
					}
					if portConfig.TargetTimeout != 0 {
						opts.TargetTimeout = portConfig.TargetTimeout
					} else if portConfig.LegacyTimeout != 0 {
						opts.TargetTimeout = portConfig.LegacyTimeout
					}
					if portConfig.ProbeTimeoutMs != 0 {
						opts.ProbeTimeoutMs = portConfig.ProbeTimeoutMs
					}
					if portConfig.PortThreshold != 0 {
						opts.PortThreshold = portConfig.PortThreshold
					}
					if portConfig.ScanType != "" {
						opts.ScanType = portConfig.ScanType
					}
					if portConfig.SkipHostDiscovery != nil {
						opts.SkipHostDiscovery = *portConfig.SkipHostDiscovery
					}
					if portConfig.ExcludeCDN != nil {
						opts.ExcludeCDN = *portConfig.ExcludeCDN
					}
					opts.ExcludeHosts = portConfig.ExcludeHosts
					if portConfig.Retries != nil {
						opts.Retries = *portConfig.Retries
					}
					if portConfig.WarmUpTime != nil {
						opts.WarmUpTime = *portConfig.WarmUpTime
					}
					if portConfig.Workers != 0 {
						opts.Workers = portConfig.Workers
					}
					if portConfig.Verify != nil {
						opts.Verify = *portConfig.Verify
					}
				}
			}
		}
	}

	if opts.Ports == "" {
		opts.Ports = "80,443,8080"
	}
	if opts.Rate == 0 {
		opts.Rate = 3000
	}
	if opts.TargetTimeout == 0 {
		opts.TargetTimeout = DefaultPortScanTargetTimeoutSeconds
	}
	if opts.ProbeTimeoutMs == 0 {
		opts.ProbeTimeoutMs = DefaultNaabuProbeTimeoutMilliseconds
	}
	if opts.ScanType == "" {
		opts.ScanType = "c"
	}
	if opts.Workers == 0 {
		opts.Workers = config.WorkerConcurrency
		if opts.Workers <= 0 {
			opts.Workers = 50
		}
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	targetParseResult := ParseTargetsForPortScan(config.Target)
	for _, t := range config.Targets {
		res := ParseTargetsForPortScan(t)
		targetParseResult.WithPort = append(targetParseResult.WithPort, res.WithPort...)
		targetParseResult.WithoutPort = append(targetParseResult.WithoutPort, res.WithoutPort...)
	}

	var cleanTargets []string
	seenHost := make(map[string]bool)
	for _, host := range targetParseResult.WithoutPort {
		if !seenHost[host] {
			seenHost[host] = true
			cleanTargets = append(cleanTargets, host)
		}
	}
	originalPorts := opts.Ports
	ports := parsePorts(opts.Ports)
	portSet := make(map[int]bool)
	for _, p := range ports {
		portSet[p] = true
	}
	for _, taskWithPort := range targetParseResult.WithPort {
		if !seenHost[taskWithPort.Host] {
			seenHost[taskWithPort.Host] = true
			cleanTargets = append(cleanTargets, taskWithPort.Host)
		}
		if !portSet[taskWithPort.Port] {
			portSet[taskWithPort.Port] = true
			ports = append(ports, taskWithPort.Port)
		}
	}
	if len(targetParseResult.WithPort) > 0 {
		opts.Ports = portsToString(ports)
	} else {
		opts.Ports = originalPorts
	}

	if len(cleanTargets) == 0 {
		return &ScanResult{MainTaskId: config.MainTaskId, Assets: []*Asset{}}, nil
	}

	assets, thresholdExceeded, diagnostic := s.runNaabuCLI(ctx, config, opts, logFn)
	result := &ScanResult{
		MainTaskId:   config.MainTaskId,
		Assets:       assets,
		SkippedHosts: s.collectSkippedHosts(),
		Diagnostic:   diagnostic,
	}
	if thresholdExceeded {
		return result, ErrPortThresholdExceeded
	}
	return result, nil
}

func (s *NaabuScanner) runNaabuCLI(ctx context.Context, config *ScanConfig, opts *NaabuOptions, logFn func(level, format string, args ...interface{})) ([]*Asset, bool, *ScanDiagnostic) {
	var allAssets []*Asset
	anyThresholdExceeded := false

	var portsStr string
	switch opts.Ports {
	case "top100", "top1000":
		// 交给 Naabu 原生 -tp，不本地展开，避免端口数被展开成 96 等固定值
	default:
		portsStr = optimizePortsForNaabu(opts.Ports)
	}

	targetParseResult := ParseTargetsForPortScan(config.Target)
	for _, t := range config.Targets {
		res := ParseTargetsForPortScan(t)
		targetParseResult.WithPort = append(targetParseResult.WithPort, res.WithPort...)
		targetParseResult.WithoutPort = append(targetParseResult.WithoutPort, res.WithoutPort...)
	}

	var cleanTargets []string
	seenHost := make(map[string]bool)
	for _, host := range targetParseResult.WithoutPort {
		if !seenHost[host] {
			seenHost[host] = true
			cleanTargets = append(cleanTargets, host)
		}
	}
	for _, taskWithPort := range targetParseResult.WithPort {
		if !seenHost[taskWithPort.Host] {
			seenHost[taskWithPort.Host] = true
			cleanTargets = append(cleanTargets, taskWithPort.Host)
		}
	}

	totalTargets := len(cleanTargets)
	concurrency := EffectiveNaabuProcessConcurrency(opts.Workers, totalTargets)
	targetChan := make(chan string, totalTargets)
	resultChan := make(chan naabuTargetResult, totalTargets)
	var scanWg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for target := range targetChan {
				select {
				case <-ctx.Done():
					resultChan <- naabuTargetResult{target: target, diagnostic: NaabuTargetDiagnostic{Source: NaabuParseSourceNone, ProcessOutcome: "canceled"}}
					return
				default:
				}
				resultChan <- s.scanTargetCLI(ctx, target, portsStr, opts, logFn, config.EventLogger)
			}
		}()
	}

dispatch:
	for _, target := range cleanTargets {
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

	diagnostic := &ScanDiagnostic{Phase: "naabu", Coverage: Coverage{Input: totalTargets}}
	seenAsset := make(map[string]struct{})
	completed := 0
	for res := range resultChan {
		diagnostic.Coverage.Attempted++
		appendNaabuTargetDiagnostic(diagnostic, res.target, res.diagnostic)
		switch res.diagnostic.ProcessOutcome {
		case "success":
			diagnostic.Coverage.Succeeded++
		case "timeout":
			diagnostic.Coverage.TimedOut++
			diagnostic.Coverage.Unconfirmed++
		case "exit_error":
			diagnostic.Coverage.Failed++
			diagnostic.Coverage.Unconfirmed++
		case "canceled":
			diagnostic.Coverage.Skipped++
		}
		if res.thresholdExceeded {
			anyThresholdExceeded = true
			diagnostic.Coverage.Unconfirmed++
			diagnostic.WarningCodes = appendUniqueReason(diagnostic.WarningCodes, ReasonPortThresholdExceeded)
		}
		if ctx.Err() == context.Canceled {
			continue
		}
		for _, asset := range res.assets {
			key := naabuHostPortKey(asset.Host, asset.Port)
			if _, exists := seenAsset[key]; exists {
				continue
			}
			seenAsset[key] = struct{}{}
			allAssets = append(allAssets, asset)
		}
		if config.OnTargetDone != nil {
			config.OnTargetDone(res.target, res.assets)
		}
		completed++
		if config.OnProgress != nil && totalTargets > 0 {
			config.OnProgress(completed*100/totalTargets, fmt.Sprintf("端口扫描: %d/%d", completed, totalTargets))
		}
	}

	diagnostic.Status = deriveNaabuPhaseStatus(ctx, diagnostic.Coverage, anyThresholdExceeded, len(allAssets))
	if diagnostic.Status == PhasePartial {
		diagnostic.WarningCodes = appendUniqueReason(diagnostic.WarningCodes, ReasonPartialOutput)
	}
	if diagnostic.Status == PhaseFailed && diagnostic.Coverage.Attempted > 0 {
		diagnostic.WarningCodes = appendUniqueReason(diagnostic.WarningCodes, ReasonNoOutput)
	}
	return allAssets, anyThresholdExceeded, diagnostic
}

func (o *NaabuOptions) targetTimeoutDuration() time.Duration {
	return time.Duration(o.TargetTimeout) * time.Second
}

func buildNaabuArgs(target, portsStr, outputPath string, opts *NaabuOptions) []string {
	args := []string{
		"-host", target,
		"-json",
		"-silent",
		"-rate", strconv.Itoa(opts.Rate),
		"-timeout", strconv.Itoa(opts.ProbeTimeoutMs),
		"-retries", strconv.Itoa(opts.Retries),
		"-warm-up-time", strconv.Itoa(opts.WarmUpTime),
		"-c", strconv.Itoa(opts.Workers),
	}
	if portsStr != "" {
		args = append(args, "-p", portsStr)
	}
	if opts.Ports == "top100" {
		args = append(args, "-tp", "100")
	}
	if opts.Ports == "top1000" {
		args = append(args, "-tp", "1000")
	}
	if opts.ScanType != "" {
		// Naabu uses -s for scan type and -c for internal worker concurrency.
		args = append(args, "-s", opts.ScanType)
	}
	if opts.SkipHostDiscovery {
		args = append(args, "-Pn")
	}
	if opts.ExcludeCDN {
		args = append(args, "-ec")
	}
	if opts.ExcludeHosts != "" {
		args = append(args, "-eh", opts.ExcludeHosts)
	}
	if opts.Verify {
		args = append(args, "-verify")
	}
	if opts.PortThreshold > 0 {
		args = append(args, "-port-threshold", strconv.Itoa(opts.PortThreshold))
	}
	return append(args, "-o", outputPath)
}

func (s *NaabuScanner) scanTargetCLI(ctx context.Context, target, portsStr string, opts *NaabuOptions, logFn func(level, format string, args ...interface{}), eventLog ScanEventLogger) (result naabuTargetResult) {
	startedAt := time.Now()
	result = naabuTargetResult{target: target}
	defer func() {
		result.diagnostic.DurationMs = time.Since(startedAt).Milliseconds()
		emitNaabuParseEvent(eventLog, target, result.diagnostic)
	}()
	// 单目标 Context 是 Web targetTimeout 的真正执行边界；等待全局进程槽也计入该预算。
	targetTimeout := opts.targetTimeoutDuration()
	targetCtx, targetCancel := context.WithTimeout(ctx, targetTimeout)
	defer targetCancel()

	tmpFile, err := os.CreateTemp("", "naabu-*.json")
	if err != nil {
		logFn("ERROR", "Naabu(CLI)：目标=%s 创建输出文件失败：%v", target, err)
		result.diagnostic = NaabuTargetDiagnostic{Source: NaabuParseSourceNone, ProcessOutcome: "exit_error", OutputFileEmpty: true}
		return result
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := buildNaabuArgs(target, portsStr, tmpPath, opts)
	logFn("INFO", "系统执行命令：%s", s.executor.CommandLine(args))

	execResult, execErr := s.executor.Execute(targetCtx, args, ExecuteOpts{Timeout: targetTimeout, LogFn: logFn})
	if execResult == nil {
		execResult = &ExecuteResult{}
	}
	processOutcome := classifyNaabuProcessOutcome(ctx, targetCtx, execErr, execResult.ExitCode)
	if processOutcome == "canceled" {
		// 用户取消时不将残留文件作为已完成结果回调给 Worker。
		result.diagnostic = NaabuTargetDiagnostic{Source: NaabuParseSourceNone, ProcessOutcome: processOutcome, ExitCode: execResult.ExitCode, OutputFileEmpty: true}
		return result
	}
	if processOutcome == "timeout" {
		logFn("WARN", "Naabu(CLI)：目标=%s 扫描超时（%s），尝试解析部分结果", target, targetTimeout)
	} else if processOutcome == "exit_error" {
		if execErr != nil {
			logFn("ERROR", "Naabu(CLI)：目标=%s 启动或执行失败，exitCode=%d：%v", target, execResult.ExitCode, execErr)
		} else {
			logFn("ERROR", "Naabu(CLI)：目标=%s 非零退出，exitCode=%d", target, execResult.ExitCode)
		}
	}

	fileContent, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		logFn("ERROR", "Naabu(CLI)：目标=%s 输出文件读取失败：%v", target, readErr)
		fileContent = nil
	}
	assets, stats, source := parseNaabuOutput(target, fileContent, []byte(execResult.Stdout), processOutcome)
	if stats.InvalidLines > 0 {
		logFn("ERROR", "Naabu(CLI)：目标=%s 输出解析失败记录=%d，总记录=%d", target, stats.InvalidLines, stats.TotalLines)
	}
	result.diagnostic = NaabuTargetDiagnostic{
		Source:          source,
		ProcessOutcome:  processOutcome,
		ExitCode:        execResult.ExitCode,
		OutputFileEmpty: len(fileContent) == 0,
		Stats:           stats,
	}
	if readErr != nil && source == NaabuParseSourceNone {
		result.diagnostic.ProcessOutcome = "exit_error"
	}
	result.thresholdExceeded = exceedsNaabuPortThreshold(stats, opts.PortThreshold)
	if result.thresholdExceeded {
		logFn("WARN", "Naabu(CLI)：目标=%s 开放端口数 %d 超过阈值 %d", target, stats.AcceptedPorts, opts.PortThreshold)
		// 保持既有阈值语义：被跳过的目标不产出部分资产，但诊断保留完整计数。
		return result
	}
	result.assets = assets

	if len(assets) > 0 {
		ports := make([]string, 0, len(assets))
		for _, asset := range assets {
			ports = append(ports, strconv.Itoa(asset.Port))
		}
		logFn("INFO", "开放端口：%s -> %s", target, strings.Join(ports, ","))
	}
	return result
}

func emitNaabuParseEvent(eventLog ScanEventLogger, target string, diagnostic NaabuTargetDiagnostic) {
	if eventLog == nil {
		return
	}
	stats := diagnostic.Stats
	reasonCode := ""
	switch diagnostic.ProcessOutcome {
	case "timeout":
		reasonCode = ReasonTimeout
	case "exit_error":
		reasonCode = ReasonExecutionError
	case "canceled":
		reasonCode = ReasonCanceled
	}
	eventLog(EventNaabuParseComplete, "naabu", diagnostic.ProcessOutcome, map[string]interface{}{
		"target": target, "process_outcome": diagnostic.ProcessOutcome,
		"exit_code": diagnostic.ExitCode, "source": string(diagnostic.Source),
		"file_bytes": stats.FileBytes, "stdout_bytes": stats.StdoutBytes, "parsed_bytes": stats.ParsedBytes,
		"total_lines": stats.TotalLines, "valid_lines": stats.ValidLines, "invalid_lines": stats.InvalidLines,
		"duplicate_lines": stats.DuplicateLines, "accepted_ports": stats.AcceptedPorts,
		"output_file_empty": diagnostic.OutputFileEmpty, "duration_ms": diagnostic.DurationMs,
		"reason_code": reasonCode,
	})
}

func classifyNaabuProcessOutcome(parentCtx, targetCtx context.Context, execErr error, exitCode int) string {
	if parentCtx.Err() == context.Canceled || targetCtx.Err() == context.Canceled {
		return "canceled"
	}
	if targetCtx.Err() == context.DeadlineExceeded {
		return "timeout"
	}
	if execErr != nil || exitCode != 0 {
		return "exit_error"
	}
	return "success"
}

func selectNaabuParseSource(fileContent, stdout []byte, processOutcome string) (NaabuParseSource, [][]byte) {
	fileAvailable, stdoutAvailable := len(fileContent) > 0, len(stdout) > 0
	if (processOutcome == "timeout" || processOutcome == "exit_error") && fileAvailable && stdoutAvailable {
		return NaabuParseSourceFileStdout, [][]byte{fileContent, stdout}
	}
	if fileAvailable {
		return NaabuParseSourceFile, [][]byte{fileContent}
	}
	if stdoutAvailable {
		return NaabuParseSourceStdout, [][]byte{stdout}
	}
	return NaabuParseSourceNone, nil
}

// parseNaabuOutput parses the selected output sources in a stable order (file,
// then stdout). Bad records are local failures and never discard valid ports.
func parseNaabuOutput(target string, fileContent, stdout []byte, processOutcome string) ([]*Asset, NaabuParseStats, NaabuParseSource) {
	source, contents := selectNaabuParseSource(fileContent, stdout, processOutcome)
	stats := NaabuParseStats{FileBytes: len(fileContent), StdoutBytes: len(stdout)}
	seen := make(map[string]struct{})
	assets := make([]*Asset, 0)
	for _, content := range contents {
		stats.ParsedBytes += len(content)
		lineScanner := bufio.NewScanner(bytes.NewReader(content))
		lineScanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for lineScanner.Scan() {
			line := strings.TrimSpace(lineScanner.Text())
			if line == "" {
				continue
			}
			stats.TotalLines++
			var hostResult NaabuHostResult
			if err := json.Unmarshal([]byte(line), &hostResult); err != nil || net.ParseIP(hostResult.IP) == nil || hostResult.Port < 1 || hostResult.Port > 65535 {
				stats.InvalidLines++
				continue
			}
			stats.ValidLines++
			assetHost := hostResult.IP
			if target != "" && net.ParseIP(target) == nil {
				assetHost = target
			}
			key := naabuHostPortKey(assetHost, hostResult.Port)
			if _, exists := seen[key]; exists {
				stats.DuplicateLines++
				continue
			}
			seen[key] = struct{}{}
			locStr, _ := ipLocator.Locate(hostResult.IP)
			location := geolocation.NormalizeLocation(locStr)
			asset := &Asset{
				Authority: utils.BuildTargetWithPort(assetHost, hostResult.Port),
				Host:      assetHost,
				Port:      hostResult.Port,
				Category:  getCategory(hostResult.IP),
			}
			if strings.Contains(hostResult.IP, ":") {
				asset.IPV6 = []IPInfo{{IP: hostResult.IP, Location: location}}
			} else {
				asset.IPV4 = []IPInfo{{IP: hostResult.IP, Location: location}}
			}
			assets = append(assets, asset)
			stats.AcceptedPorts++
		}
		if lineScanner.Err() != nil {
			stats.InvalidLines++
		}
	}
	return assets, stats, source
}

func naabuHostPortKey(host string, port int) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]")) + ":" + strconv.Itoa(port)
}

func exceedsNaabuPortThreshold(stats NaabuParseStats, threshold int) bool {
	return threshold > 0 && stats.AcceptedPorts > threshold
}

func appendNaabuTargetDiagnostic(diagnostic *ScanDiagnostic, target string, naabuDiagnostic NaabuTargetDiagnostic) {
	if len(diagnostic.Targets) >= MaxTargetDiagnostics {
		return
	}
	reason := ""
	switch naabuDiagnostic.ProcessOutcome {
	case "timeout":
		reason = ReasonTimeout
	case "exit_error":
		reason = ReasonExecutionError
	case "canceled":
		reason = ReasonCanceled
	}
	if reason != "" {
		diagnostic.WarningCodes = appendUniqueReason(diagnostic.WarningCodes, reason)
	}
	stats := naabuDiagnostic.Stats
	diagnostic.Targets = append(diagnostic.Targets, TargetDiagnostic{
		Target: target, Host: target, Outcome: naabuDiagnostic.ProcessOutcome, ReasonCode: reason,
		Metadata: map[string]interface{}{
			"source": string(naabuDiagnostic.Source), "process_outcome": naabuDiagnostic.ProcessOutcome,
			"exit_code": naabuDiagnostic.ExitCode, "file_bytes": stats.FileBytes, "stdout_bytes": stats.StdoutBytes,
			"parsed_bytes": stats.ParsedBytes, "total_lines": stats.TotalLines, "valid_lines": stats.ValidLines,
			"invalid_lines": stats.InvalidLines, "duplicate_lines": stats.DuplicateLines, "accepted_ports": stats.AcceptedPorts,
			"output_file_empty": naabuDiagnostic.OutputFileEmpty, "duration_ms": naabuDiagnostic.DurationMs,
		},
	})
}

func appendUniqueReason(codes []string, code string) []string {
	for _, existing := range codes {
		if existing == code {
			return codes
		}
	}
	return append(codes, code)
}

func deriveNaabuPhaseStatus(ctx context.Context, coverage Coverage, thresholdExceeded bool, assets int) PhaseStatus {
	if ctx.Err() == context.Canceled {
		return PhaseCanceled
	}
	if coverage.Attempted == 0 && coverage.Input > 0 {
		return PhaseCanceled
	}
	if coverage.TimedOut == 0 && coverage.Failed == 0 && coverage.Unconfirmed == 0 && !thresholdExceeded && coverage.Succeeded == coverage.Input {
		return PhaseComplete
	}
	if assets > 0 || coverage.Succeeded > 0 {
		return PhasePartial
	}
	return PhaseFailed
}

func (s *NaabuScanner) collectSkippedHosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.skippedHosts))
	copy(result, s.skippedHosts)
	return result
}

// optimizePortsForNaabu 优化端口参数
func optimizePortsForNaabu(portStr string) string {
	portStr = strings.TrimSpace(portStr)
	if portStr == "top100" {
		return portsToString(GetTop100Ports())
	}
	if portStr == "top1000" {
		return portsToString(GetTop1000Ports())
	}
	parts := strings.Split(portStr, ",")
	if len(parts) == 1 && strings.Contains(parts[0], "-") {
		return portStr
	}
	hasLargeRange := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rParts := strings.Split(part, "-")
			if len(rParts) == 2 {
				start, _ := strconv.Atoi(strings.TrimSpace(rParts[0]))
				end, _ := strconv.Atoi(strings.TrimSpace(rParts[1]))
				if end-start > 1000 {
					hasLargeRange = true
					break
				}
			}
		}
	}
	if hasLargeRange {
		return portStr
	}
	return portsToString(parsePorts(portStr))
}
