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

	// 尝试从不同类型的Options中提取配置
	if config.Options != nil {
		switch v := config.Options.(type) {
		case *MasscanOptions:
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
		default:
			// 尝试通过JSON转换
			if data, err := json.Marshal(config.Options); err == nil {
				var portConfig struct {
					Ports             string `json:"ports"`
					Rate              int    `json:"rate"`
					Timeout           int    `json:"timeout"`
					PortThreshold     int    `json:"portThreshold"`
					SkipHostDiscovery bool   `json:"skipHostDiscovery"`
					ExcludeHosts      string `json:"excludeHosts"`
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
					opts.SkipHostDiscovery = portConfig.SkipHostDiscovery
					opts.ExcludeHosts = portConfig.ExcludeHosts
				}
			}
		}
	}

	logx.Infof("Masscan Scan config - Ports: %s, Rate: %d, Timeout: %d, PortThreshold: %d, ExcludeHosts: %s", opts.Ports, opts.Rate, opts.Timeout, opts.PortThreshold, opts.ExcludeHosts)

	// 检查masscan是否安装
	if !checkMasscanInstalled() {
		logx.Error("masscan not installed, falling back to tcp scan")
		// 回退到TCP扫描
		tcpScanner := NewPortScanner()
		return tcpScanner.Scan(ctx, config)
	}

	// 解析目标
	targets := parseTargets(config.Target)
	if len(config.Targets) > 0 {
		targets = append(targets, config.Targets...)
	}

	// 执行masscan扫描（传入阈值参数）
	assets := s.runMasscan(ctx, targets, opts)

	return &ScanResult{
		WorkspaceId:  config.WorkspaceId,
		MainTaskId:   config.MainTaskId,
		Assets:       assets,
		SkippedHosts: s.collectSkippedHosts(),
	}, nil
}

// runMasscan 运行masscan
func (s *MasscanScanner) runMasscan(ctx context.Context, targets []string, opts *MasscanOptions) []*Asset {
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

	// 输出执行命令到日志
	logx.Infof("Executing command: masscan %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "masscan", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logx.Errorf("masscan stdout pipe error: %v", err)
		return assets
	}

	if err := cmd.Start(); err != nil {
		logx.Errorf("masscan start error: %v", err)
		return assets
	}

	// 解析JSON输出
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" {
			continue
		}
		// 去除行尾逗号
		line = strings.TrimSuffix(line, ",")

		var result MasscanResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
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
						logx.Infof("Host %s exceeded port threshold (%d > %d), discarding all results for this host",
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

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			logx.Infof("Masscan cancelled for targets")
		} else {
			logx.Errorf("Masscan command failed: %v", err)
		}
	}

	return assets
}

// checkMasscanInstalled 检查masscan是否安装
func checkMasscanInstalled() bool {
	cmd := exec.Command("masscan", "--version")
	output, _ := cmd.CombinedOutput()
	// 通过检查输出内容来判断是否安装
	return strings.Contains(string(output), "Masscan version")
}

// CheckMasscanInstalled 导出的检查函数，供外部调用
func CheckMasscanInstalled() bool {
	return checkMasscanInstalled()
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
