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

// NaabuOptions Naabu扫描选项
type NaabuOptions struct {
	Ports             string `json:"ports"`
	Rate              int    `json:"rate"`
	Timeout           int    `json:"timeout"`
	ScanType          string `json:"scanType"`
	PortThreshold     int    `json:"portThreshold"`
	SkipHostDiscovery bool   `json:"skipHostDiscovery"`
	ExcludeCDN        bool   `json:"excludeCDN"`
	ExcludeHosts      string `json:"excludeHosts"`
	Retries           int    `json:"retries"`
	WarmUpTime        int    `json:"warmUpTime"`
	Workers           int    `json:"workers"`
	Verify            bool   `json:"verify"`
	AggregatedTimeout int    `json:"aggregatedTimeout"` // 聚合超时（秒），当>0时按目标数分摊为单目标超时
}

// Validate 验证配置
func (o *NaabuOptions) Validate() error {
	if o.Rate < 0 {
		return fmt.Errorf("rate must be non-negative, got %d", o.Rate)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.PortThreshold < 0 {
		return fmt.Errorf("portThreshold must be non-negative, got %d", o.PortThreshold)
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
		case "ERROR":
			logx.Errorf(format, args...)
		case "WARN":
			logx.Errorf(format, args...)
		case "DEBUG":
			logx.Debugf(format, args...)
		default:
			logx.Infof(format, args...)
		}
	}

	opts := &NaabuOptions{
		Ports:         "80,443,8080",
		Rate:          3000,
		Timeout:       120,
		ScanType:      "c",
		PortThreshold: 0,
		Retries:       2,
		WarmUpTime:    1,
		// 单目标 naabu 内部并发探测线程数。提升至 50 可显著加快单 host 的全端口探测吞吐；
		// 可经 PortScanOptions.Workers（或前端扫描配置）覆盖，带宽受限/低资源主机应适当下调以避免丢包与 IDS 限流。
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
			if v.Rate > 0 {
				opts.Rate = v.Rate
			}
			if v.Timeout > 0 {
				opts.Timeout = v.Timeout
			}
			if v.PortThreshold > 0 {
				opts.PortThreshold = v.PortThreshold
			}
			if v.ScanType != "" {
				opts.ScanType = v.ScanType
			}
			opts.SkipHostDiscovery = v.SkipHostDiscovery
			opts.ExcludeCDN = v.ExcludeCDN
			opts.ExcludeHosts = v.ExcludeHosts
			if v.Retries > 0 {
				opts.Retries = v.Retries
			}
			if v.WarmUpTime >= 0 {
				opts.WarmUpTime = v.WarmUpTime
			}
			if v.Workers > 0 {
				opts.Workers = v.Workers
			}
			opts.Verify = v.Verify
		default:
			if data, err := json.Marshal(config.Options); err == nil {
				var portConfig struct {
					Ports             string `json:"ports"`
					Rate              int    `json:"rate"`
					Timeout           int    `json:"timeout"`
					PortThreshold     int    `json:"portThreshold"`
					ScanType          string `json:"scanType"`
					SkipHostDiscovery bool   `json:"skipHostDiscovery"`
					ExcludeCDN        bool   `json:"excludeCDN"`
					ExcludeHosts      string `json:"excludeHosts"`
					Retries           int    `json:"retries"`
					WarmUpTime        int    `json:"warmUpTime"`
					Workers           int    `json:"workers"`
					Verify            bool   `json:"verify"`
				}
				if err := json.Unmarshal(data, &portConfig); err == nil {
					if portConfig.Ports != "" {
						opts.Ports = portConfig.Ports
					}
					if portConfig.Rate > 0 {
						opts.Rate = portConfig.Rate
					}
					if portConfig.Timeout > 0 {
						opts.Timeout = portConfig.Timeout
					}
					if portConfig.PortThreshold > 0 {
						opts.PortThreshold = portConfig.PortThreshold
					}
					if portConfig.ScanType != "" {
						opts.ScanType = portConfig.ScanType
					}
					if portConfig.Retries > 0 {
						opts.Retries = portConfig.Retries
					}
					if portConfig.WarmUpTime >= 0 {
						opts.WarmUpTime = portConfig.WarmUpTime
					}
					if portConfig.Workers > 0 {
						opts.Workers = portConfig.Workers
					}
					opts.SkipHostDiscovery = portConfig.SkipHostDiscovery
					opts.ExcludeCDN = portConfig.ExcludeCDN
					opts.ExcludeHosts = portConfig.ExcludeHosts
					opts.Verify = portConfig.Verify
				}
			}
		}
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

	// 确保 Worker Pool 大小继承 worker 自适应值
	if opts.Workers <= 0 {
		opts.Workers = config.WorkerConcurrency
	}

	// Phase 2：聚合超时透传
	// 当上层传入 AggregatedTimeout 时，按目标数均摊为单目标超时，
	// 使 naabu 实际执行语义与 worker 日志中的 timeout=2940s 对齐。
	aggregatedTimeout := opts.AggregatedTimeout
	if aggregatedTimeout > 0 && len(cleanTargets) > 0 {
		perTarget := aggregatedTimeout / len(cleanTargets)
		if perTarget < 1 {
			perTarget = 1
		}
		if opts.Timeout < 1 {
			opts.Timeout = 1
		}
		if perTarget > opts.Timeout {
			opts.Timeout = perTarget
		}
		logFn("INFO", "Naabu(CLI): aggregatedTimeout=%ds targets=%d => perTargetTimeout=%ds",
			aggregatedTimeout, len(cleanTargets), opts.Timeout)
	}

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
	concurrency := opts.Workers
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 5 {
		concurrency = 5
	}
	if concurrency > totalTargets {
		concurrency = totalTargets
	}

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

	// 启动并发 Worker
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

	// 分发目标
dispatch:
	for _, target := range cleanTargets {
		select {
		case <-ctx.Done():
			break dispatch
		case targetChan <- target:
		}
	}
	close(targetChan)

	// 等待所有 Worker 完成
	go func() {
		scanWg.Wait()
		close(resultChan)
	}()

	// 收集结果
	completed := 0
	for res := range resultChan {
		if res.err != nil {
			logFn("WARN", "Naabu(CLI): worker error: %v", res.err)
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

func (s *NaabuScanner) scanTargetCLI(ctx context.Context, target, portsStr string, opts *NaabuOptions, logFn func(level, format string, args ...interface{})) ([]*Asset, bool) {
	// 全局并发限制由 CmdExecutor.Execute 统一控制

	args := []string{
		"-host", target,
		"-json",
		"-silent",
		"-rate", strconv.Itoa(opts.Rate),
		"-timeout", strconv.Itoa(opts.Timeout),
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
	if opts.ScanType == "s" {
		args = append(args, "-s")
	}
	// connect scan is the default in naabu; do not add another -c because
	// -c is already used for worker concurrency above.
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

	// 输出到临时文件
	tmpFile, err := os.CreateTemp("", "naabu-*.json")
	if err != nil {
		return nil, false
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args = append(args, "-o", tmpPath)

	// 单目标进程超时 = 单目标超时 × (1 + 重试次数)，确保重试不会被提前杀进程
	perTargetProcessTimeout := time.Duration(opts.Timeout+opts.Retries*opts.Timeout) * time.Second
	logFn("INFO", "Naabu CLI: perTargetTimeout=%v (single=%ds, retries=%d) for target %s",
		perTargetProcessTimeout, opts.Timeout, opts.Retries, target)

	// 额外 30s 缓冲用于进程启动/退出/IO，避免正常完成时被误杀
	processTimeout := perTargetProcessTimeout + 30*time.Second
	logFn("INFO", "Naabu CLI: ProcessTimeout=%v (perTarget + 30s buffer)", processTimeout)

	logFn("INFO", "[Naabu] CLI: target=%s args=%s", target, strings.Join(args, " "))

	res, err := s.executor.Execute(ctx, args, ExecuteOpts{
		Timeout: processTimeout,
		LogFn:   logFn,
	})
	if err != nil {
		logFn("ERROR", "Naabu(CLI): %s execution failed: %v, stderr=%q", target, err, strings.TrimSpace(res.Stderr))
		return nil, false
	}
	s.executor.LogResult("Naabu(CLI): "+target, res, err)

	// 读取 JSON 输出文件（进程异常退出时仍尝试读取部分结果）
	content, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		logFn("WARN", "[WARN] Naabu(CLI): failed to read output file: %v", readErr)
		if res.ExitCode == 0 {
			return nil, false
		}
		logFn("WARN", "[WARN] Naabu(CLI): %s exit code %d and failed to read output, returning empty result", target, res.ExitCode)
		return nil, false
	}

	// 兼容处理：部分 Naabu 版本在 -json 模式下仍将结果写 stdout，即使指定了 -o
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

	// 进程异常退出但输出了部分结果，继续解析
	if res.ExitCode != 0 {
		logFn("WARN", "[WARN] Naabu(CLI): %s exit code %d, parsing partial output (%d bytes)", target, res.ExitCode, len(content))
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
