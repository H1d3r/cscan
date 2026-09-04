package scanner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// MasscanScanner Masscan扫描器
type MasscanScanner struct {
	BaseScanner
	skippedHosts []string   // 因端口阈值超限被跳过的主机
	mu           sync.Mutex // 保护 skippedHosts
}

// NewMasscanScanner 创建Masscan扫描器
func NewMasscanScanner() *MasscanScanner {
	return &MasscanScanner{
		BaseScanner:  BaseScanner{name: "masscan"},
		skippedHosts: make([]string, 0),
	}
}

// collectSkippedHosts 收集因端口阈值超限被跳过的主机列表
func (s *MasscanScanner) collectSkippedHosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.skippedHosts))
	copy(result, s.skippedHosts)
	return result
}

// MasscanOptions Masscan扫描选项
type MasscanOptions struct {
	Ports             string `json:"ports"`
	Rate              int    `json:"rate"`
	Timeout           int    `json:"timeout"`
	PortThreshold     int    `json:"portThreshold"`     // 端口阈值，实时检测
	SkipHostDiscovery bool   `json:"skipHostDiscovery"` // 跳过主机发现 (-Pn)
	ExcludeHosts      string `json:"excludeHosts"`      // 排除的目标，逗号分隔 (--exclude)
}

// Validate 验证 MasscanOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *MasscanOptions) Validate() error {
	if o.Rate < 0 {
		return fmt.Errorf("rate must be non-negative, got %d", o.Rate)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.PortThreshold < 0 {
		return fmt.Errorf("portThreshold must be non-negative, got %d", o.PortThreshold)
	}
	return nil
}

// MasscanResult Masscan输出结果
type MasscanResult struct {
	IP    string `json:"ip"`
	Ports []struct {
		Port   int    `json:"port"`
		Proto  string `json:"proto"`
		Status string `json:"status"`
	} `json:"ports"`
}

// Scan 执行Masscan扫描
func (s *MasscanScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	// 重置跳过主机列表（扫描器可能被复用）
	s.mu.Lock()
	s.skippedHosts = s.skippedHosts[:0]
	s.mu.Unlock()

	// 默认配置
	opts := &MasscanOptions{
		Ports:         "21,22,23,25,80,443,3306,3389,6379,8080",
		Rate:          1000,
		Timeout:       3,
		PortThreshold: 0, // 默认不限制
	}
	fallbackConcurrent := 1
	var fallbackOptions *PortScanOptions

	// 尝试从不同类型的Options中提取配置
	if config.Options != nil {
		switch v := config.Options.(type) {
		case *MasscanOptions:
			opts = v
		case *PortScanOptions:
			fallback := *v
			fallback.Tool = "tcp"
			fallbackOptions = &fallback
			if v.Ports != "" {
				opts.Ports = v.Ports
			}
			if v.Rate > 0 {
				opts.Rate = v.Rate
			}
			if v.Timeout > 0 {
				opts.Timeout = v.Timeout
			}
			if v.TargetTimeout > 0 && v.Timeout <= 0 {
				opts.Timeout = v.TargetTimeout
			}
			if v.Concurrent > 0 {
				fallbackConcurrent = v.Concurrent
			}
			if v.PortThreshold > 0 {
				opts.PortThreshold = v.PortThreshold
			}
			opts.SkipHostDiscovery = v.SkipHostDiscovery
			opts.ExcludeHosts = v.ExcludeHosts
		default:
			// 尝试通过JSON转换。scheduler.PortScanConfig 已将旧 timeout
			// 归一化为 targetTimeout，因此这里必须同时兼容两个字段。
			if data, err := json.Marshal(config.Options); err == nil {
				var portConfig struct {
					Ports             string `json:"ports"`
					Rate              int    `json:"rate"`
					Timeout           *int   `json:"timeout"`
					TargetTimeout     int    `json:"targetTimeout"`
					PortThreshold     int    `json:"portThreshold"`
					SkipHostDiscovery bool   `json:"skipHostDiscovery"`
					ExcludeHosts      string `json:"excludeHosts"`
					Workers           int    `json:"workers"`
					Concurrent        int    `json:"concurrent"`
				}
				if err := json.Unmarshal(data, &portConfig); err == nil {
					if portConfig.Ports != "" {
						opts.Ports = portConfig.Ports
					}
					if portConfig.Rate > 0 {
						opts.Rate = portConfig.Rate
					}
					if portConfig.Timeout != nil && *portConfig.Timeout > 0 {
						opts.Timeout = *portConfig.Timeout
					} else if portConfig.TargetTimeout > 0 {
						opts.Timeout = portConfig.TargetTimeout
					}
					if portConfig.PortThreshold > 0 {
						opts.PortThreshold = portConfig.PortThreshold
					}
					opts.SkipHostDiscovery = portConfig.SkipHostDiscovery
					opts.ExcludeHosts = portConfig.ExcludeHosts
					if portConfig.Concurrent > 0 {
						fallbackConcurrent = portConfig.Concurrent
					} else if portConfig.Workers > 0 {
						fallbackConcurrent = portConfig.Workers
					}
				}
			}
		}
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

	logFn("INFO", "Masscan Scan config - Ports: %s, Rate: %d, Timeout: %d, PortThreshold: %d, ExcludeHosts: %s", opts.Ports, opts.Rate, opts.Timeout, opts.PortThreshold, opts.ExcludeHosts)

	// P0-5: 校验 ExcludeHosts，防止参数注入（拒绝 -- 、空格、分号等特殊字符）
	if opts.ExcludeHosts != "" {
		if err := ValidateIPList(opts.ExcludeHosts); err != nil {
			logFn("ERROR", "Invalid ExcludeHosts %q: %v", opts.ExcludeHosts, err)
			return nil, fmt.Errorf("invalid excludeHosts: %w", err)
		}
	}

	// 检查masscan是否安装
	if !checkMasscanInstalled() {
		logFn("ERROR", "masscan not installed, falling back to tcp scan")
		// 回退到 TCP 扫描时必须把已解析的 Masscan 配置显式转换为
		// PortScanOptions；PortScanner 不识别 MasscanOptions 或 scheduler 配置。
		if fallbackOptions == nil {
			fallbackOptions = &PortScanOptions{Tool: "tcp"}
		}
		// 使用 Masscan 解析后的有效值覆盖 fallback，确保 scheduler 配置、
		// 旧 PortScanOptions 和默认值在 TCP 路径上保持一致。
		fallbackOptions.Tool = "tcp"
		fallbackOptions.Ports = opts.Ports
		fallbackOptions.Rate = opts.Rate
		fallbackOptions.Timeout = opts.Timeout
		fallbackOptions.PortThreshold = opts.PortThreshold
		fallbackOptions.SkipHostDiscovery = opts.SkipHostDiscovery
		fallbackOptions.ExcludeHosts = opts.ExcludeHosts
		if fallbackOptions.Concurrent <= 0 {
			fallbackOptions.Concurrent = fallbackConcurrent
		}
		fallbackConfig := *config
		fallbackConfig.Options = fallbackOptions
		tcpScanner := NewPortScanner()
		return tcpScanner.Scan(ctx, &fallbackConfig)
	}

	// 解析目标
	targets := parseTargets(config.Target)
	if len(config.Targets) > 0 {
		targets = append(targets, config.Targets...)
	}

	// 执行masscan扫描（传入阈值参数）
	assets := s.runMasscan(ctx, targets, opts, logFn)

	return &ScanResult{
		MainTaskId:   config.MainTaskId,
		Assets:       assets,
		SkippedHosts: s.collectSkippedHosts(),
	}, nil
}

// runMasscan 运行masscan
func (s *MasscanScanner) runMasscan(ctx context.Context, targets []string, opts *MasscanOptions, logFn func(level, format string, args ...interface{})) []*Asset {
	var assets []*Asset

	// 实时端口阈值检测：记录每个主机的开放端口数量和是否已超过阈值
	hostPortCount := make(map[string]int)
	skippedHosts := make(map[string]bool)

	// 查找域名目标（masscan会将域名解析为IP）
	var domainTarget string
	for _, target := range targets {
		if getCategory(target) == "domain" {
			domainTarget = target
			break
		}
	}

	// 处理端口参数：masscan 原生支持范围格式（如 1-65535），直接使用更高效
	portsStr := optimizePortsForMasscan(opts.Ports)

	// 构建masscan命令
	// masscan -p ports targets --rate=rate -oJ -
	args := []string{
		"-p", portsStr,
		"--rate", strconv.Itoa(opts.Rate),
		"-oJ", "-", // JSON输出到stdout
	}
	// 跳过主机发现
	if opts.SkipHostDiscovery {
		args = append(args, "-Pn")
	}
	// 排除目标
	if opts.ExcludeHosts != "" {
		args = append(args, "--exclude", opts.ExcludeHosts)
	}
	args = append(args, targets...)

	// 仅记录非敏感执行摘要，不记录完整命令参数。
	logFn("INFO", "[Masscan] CLI: targets=%d ports_configured=%t", len(targets), opts.Ports != "")

	// 全局信号量：与 CmdExecutor 共用，限制所有扫描模块并发外部进程数
	if !acquireProcessSlot(ctx) {
		logFn("INFO", "Masscan canceled while waiting for process slot")
		return assets
	}
	defer releaseProcessSlot()

	cmd := exec.CommandContext(ctx, "masscan", args...)
	setSysProcAttr(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logFn("ERROR", "masscan stdout pipe error: %v", err)
		return assets
	}

	if err := cmd.Start(); err != nil {
		logFn("ERROR", "masscan start error: %v", err)
		return assets
	}

	// goroutine for cmd.Wait() — ensures process is reaped even if scanner blocks
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// 解析JSON输出
	// 当 ctx 取消时，exec.CommandContext 杀死进程，stdout 管道关闭，scanner.Scan() 返回 false
	scanner := bufio.NewScanner(stdout)
	lineCount := 0
	parseFailCount := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" {
			continue
		}
		// 去除行尾逗号
		line = strings.TrimSuffix(line, ",")

		var result MasscanResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			parseFailCount++
			logFn("DEBUG", "[Masscan] JSON parse failed line=%d: %v, line=%s", lineCount+1, err, line)
			continue
		}

		// 确定主机标识（用于阈值检测）
		hostKey := result.IP
		if domainTarget != "" {
			hostKey = domainTarget
		}

		// 如果该主机已被标记为跳过，直接忽略后续结果
		if skippedHosts[hostKey] {
			continue
		}

		for _, port := range result.Ports {
			if port.Status == "open" {
				// 实时检测端口阈值
				hostPortCount[hostKey]++
				if opts.PortThreshold > 0 && hostPortCount[hostKey] > opts.PortThreshold {
					// 第一次超过阈值时记录日志并标记跳过，继续处理其他主机
					if !skippedHosts[hostKey] {
						skippedHosts[hostKey] = true
						s.mu.Lock()
						s.skippedHosts = append(s.skippedHosts, hostKey)
						s.mu.Unlock()
						logFn("INFO", "Host %s exceeded port threshold (%d > %d), discarding all results for this host",
							hostKey, hostPortCount[hostKey], opts.PortThreshold)
						// 移除该主机已有的资产
						var filtered []*Asset
						for _, a := range assets {
							if a.Host != hostKey {
								filtered = append(filtered, a)
							}
						}
						assets = filtered
					}
					continue
				}

				// 如果原始目标是域名，使用域名作为Authority和Host
				host := result.IP
				authority := fmt.Sprintf("%s:%d", result.IP, port.Port)
				category := getCategory(result.IP)

				if domainTarget != "" {
					host = domainTarget
					authority = fmt.Sprintf("%s:%d", domainTarget, port.Port)
					category = "domain"
				}

				asset := &Asset{
					Authority: authority,
					Host:      host,
					Port:      port.Port,
					Category:  category,
				}
				assets = append(assets, asset)
			}
		}
	}

	if err := <-done; err != nil {
		if ctx.Err() != nil {
			logFn("INFO", "Masscan canceled for targets")
		} else {
			logFn("ERROR", "Masscan command failed: %v", err)
		}
	}

	logFn("DEBUG", "[Masscan] completed: assets=%d parseFail=%d", len(assets), parseFailCount)
	return assets
}

// checkMasscanInstalled 检查masscan是否安装
func checkMasscanInstalled() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, _ := runCommandContext(ctx, "masscan", "--version")
	return strings.Contains(string(output), "Masscan version")
}

// optimizePortsForMasscan 优化端口参数格式，避免命令行参数过长
// masscan 原生支持范围格式（如 1-65535），直接使用比展开更高效
func optimizePortsForMasscan(portStr string) string {
	portStr = strings.TrimSpace(portStr)

	// 预定义端口集需要展开
	if portStr == "top100" {
		ports := GetTop100Ports()
		return portsToString(ports)
	}
	if portStr == "top1000" {
		ports := GetTop1000Ports()
		return portsToString(ports)
	}

	// 检查是否包含大范围端口（如 1-65535）
	// 如果是简单的范围格式，直接返回，让 masscan 自己处理
	parts := strings.Split(portStr, ",")

	// 如果只有一个部分且是范围格式，直接返回
	if len(parts) == 1 && strings.Contains(parts[0], "-") {
		return portStr
	}

	// 检查是否有大范围（超过1000个端口的范围）
	hasLargeRange := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				start, _ := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
				end, _ := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
				if end-start > 1000 {
					hasLargeRange = true
					break
				}
			}
		}
	}

	// 如果有大范围，直接返回原始字符串，让 masscan 处理
	if hasLargeRange {
		return portStr
	}

	// 否则展开为具体端口列表（小范围时更精确）
	ports := parsePorts(portStr)
	return portsToString(ports)
}
