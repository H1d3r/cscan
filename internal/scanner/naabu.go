package scanner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		case "ERROR", "WARN":
			logx.Errorf(format, args...)
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

	logFn("INFO", "Naabu(CLI): targetTimeout=%ds probeTimeout=%dms targets=%d",
		opts.TargetTimeout, opts.ProbeTimeoutMs, len(cleanTargets))

	assets, thresholdExceeded := s.runNaabuCLI(ctx, config, opts, logFn)
	if thresholdExceeded {
		return &ScanResult{
			MainTaskId: config.MainTaskId,
			Assets:     assets, SkippedHosts: s.collectSkippedHosts(),
		}, ErrPortThresholdExceeded
	}
	return &ScanResult{
		MainTaskId: config.MainTaskId,
		Assets:     assets, SkippedHosts: s.collectSkippedHosts(),
	}, nil
}

func (s *NaabuScanner) runNaabuCLI(ctx context.Context, config *ScanConfig, opts *NaabuOptions, logFn func(level, format string, args ...interface{})) ([]*Asset, bool) {
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

	type targetResult struct {
		target            string
		assets            []*Asset
		thresholdExceeded bool
		err               error
	}

	// 并发 Worker Pool：每个 Worker 从 targetChan 取目标执行，结果写入 resultChan
	targetChan := make(chan string, totalTargets)
	resultChan := make(chan targetResult, totalTargets)
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
				assets, thresholdExceeded := s.scanTargetCLI(ctx, target, portsStr, opts, logFn)
				resultChan <- targetResult{
					target:            target,
					assets:            assets,
					thresholdExceeded: thresholdExceeded,
				}
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

	completed := 0
	for res := range resultChan {
		if res.err != nil {
			logFn("WARN", "Naabu(CLI): worker error: %v", res.err)
			continue
		}
		if ctx.Err() == context.Canceled {
			// 主动停止任务时不再触发完成/进度回调，避免取消后的部分结果异步入库。
			continue
		}
		if res.thresholdExceeded {
			anyThresholdExceeded = true
		}
		allAssets = append(allAssets, res.assets...)
		if config.OnTargetDone != nil {
			config.OnTargetDone(res.target, res.assets)
		}
		completed++
		if config.OnProgress != nil && totalTargets > 0 {
			config.OnProgress(completed*100/totalTargets,
				fmt.Sprintf("端口扫描: %d/%d", completed, totalTargets))
		}
	}

	logFn("INFO", "Naabu(CLI): completed, found %d open ports across %d targets", len(allAssets), totalTargets)
	return allAssets, anyThresholdExceeded
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

func (s *NaabuScanner) scanTargetCLI(ctx context.Context, target, portsStr string, opts *NaabuOptions, logFn func(level, format string, args ...interface{})) ([]*Asset, bool) {
	// 单目标 Context 是 Web targetTimeout 的真正执行边界；等待全局进程槽也计入该预算。
	targetTimeout := opts.targetTimeoutDuration()
	targetCtx, targetCancel := context.WithTimeout(ctx, targetTimeout)
	defer targetCancel()

	tmpFile, err := os.CreateTemp("", "naabu-*.json")
	if err != nil {
		return nil, false
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := buildNaabuArgs(target, portsStr, tmpPath, opts)

	logFn("INFO", "[Naabu] CLI: target=%s targetTimeout=%s probeTimeout=%dms retries=%d args=%s",
		target, targetTimeout, opts.ProbeTimeoutMs, opts.Retries, strings.Join(args, " "))

	res, execErr := s.executor.Execute(targetCtx, args, ExecuteOpts{
		Timeout: targetTimeout,
		LogFn:   logFn,
	})
	// 主动取消表示任务已停止，不应把临时文件中残留的部分结果继续回调给
	// Worker；阶段 deadline 则仍按既有设计保留可解析的部分结果。
	if targetCtx.Err() == context.Canceled {
		return nil, false
	}
	if execErr != nil {
		if targetCtx.Err() == context.DeadlineExceeded {
			logFn("WARN", "Naabu(CLI): target %s reached targetTimeout=%s; attempting to parse partial output", target, targetTimeout)
		} else {
			logFn("WARN", "Naabu(CLI): %s execution ended early: %v, stderr=%q", target, execErr, strings.TrimSpace(res.Stderr))
		}
	}
	s.executor.LogResult("Naabu(CLI): "+target, res, execErr)

	// 进程超时或异常退出时仍读取临时文件，尽可能保留已发现的端口。
	content, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		logFn("WARN", "[WARN] Naabu(CLI): failed to read output file: %v", readErr)
		return nil, false
	}

	var parseSource io.Reader
	if len(content) > 0 {
		parseSource = strings.NewReader(string(content))
	} else if len(res.Stdout) > 0 {
		logFn("INFO", "Naabu(CLI): output file is empty, falling back to stdout (%d bytes)", len(res.Stdout))
		parseSource = bytes.NewReader([]byte(res.Stdout))
	} else {
		logFn("INFO", "Naabu(CLI): no output from file or stdout, returning empty result")
		return nil, false
	}

	if execErr != nil || res.ExitCode != 0 {
		logFn("WARN", "[WARN] Naabu(CLI): %s ended with exit code %d; parsing partial output (%d bytes)", target, res.ExitCode, len(content))
	}

	var assets []*Asset
	var foundPorts []string
	seenPort := make(map[string]bool)
	hostPortCount := 0
	parseFailCount := 0

	scanner := bufio.NewScanner(parseSource)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var hostResult NaabuHostResult
		if err := json.Unmarshal([]byte(line), &hostResult); err != nil {
			parseFailCount++
			logFn("DEBUG", "[Naabu] JSON parse failed line=%q err=%v", line, err)
			continue
		}
		if hostResult.Port <= 0 {
			logFn("DEBUG", "[Naabu] ignoring non-positive port: ip=%s port=%d", hostResult.IP, hostResult.Port)
			continue
		}

		// 优先使用原始目标主机名（子域名），仅在目标为 IP 时使用解析结果
		// 修复：naabu 内部 DNS 解析后 JSON 仅含 IP，导致后续 nmap/证书扫描使用 IP 而非域名
		assetHost := hostResult.IP
		if target != "" && net.ParseIP(target) == nil {
			assetHost = target
		}

		// 去重：同一 host:port 只保留一次（Naabu 可能对同一端口输出多条 JSON）
		dedupKey := fmt.Sprintf("%s:%d", assetHost, hostResult.Port)
		if seenPort[dedupKey] {
			continue
		}
		seenPort[dedupKey] = true
		hostPortCount++
		if opts.PortThreshold > 0 && hostPortCount > opts.PortThreshold {
			return nil, true
		}
		locStr, _ := ipLocator.Locate(hostResult.IP)
		location := geolocation.NormalizeLocation(locStr)

		asset := &Asset{
			Authority: utils.BuildTargetWithPort(assetHost, hostResult.Port),
			Host:      assetHost,
			Port:      hostResult.Port,
			Category:  getCategory(hostResult.IP),
		}
		if hostResult.IP != "" {
			if strings.Contains(hostResult.IP, ":") {
				asset.IPV6 = []IPInfo{{IP: hostResult.IP, Location: location}}
			} else {
				asset.IPV4 = []IPInfo{{IP: hostResult.IP, Location: location}}
			}
		}
		assets = append(assets, asset)
		foundPorts = append(foundPorts, strconv.Itoa(hostResult.Port))
	}

	if len(foundPorts) > 0 {
		logFn("INFO", "Naabu(CLI): %s -> %s", target, strings.Join(foundPorts, ","))
	} else {
		logFn("INFO", "Naabu(CLI): %s -> no open ports found", target)
	}

	if parseFailCount > 0 {
		logFn("DEBUG", "[Naabu] scanTargetCLI target=%s: assets=%d parseFail=%d", target, len(assets), parseFailCount)
	}

	return assets, false
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
