package scanner

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"cscan/pkg/geolocation"
	"cscan/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

// PortScanner 端口扫描器
type PortScanner struct {
	BaseScanner
}

// NewPortScanner 创建端口扫描器
func NewPortScanner() *PortScanner {
	return &PortScanner{
		BaseScanner: BaseScanner{name: "portscan"},
	}
}

// PortScanOptions 端口扫描选项
type PortScanOptions struct {
	Tool              string `json:"tool"` // tcp, masscan, nmap, naabu
	Ports             string `json:"ports"`
	Rate              int    `json:"rate"`              // 每秒发送包数
	Timeout           int    `json:"timeout"`           // 端口扫描超时时间(秒)
	Concurrent        int    `json:"concurrent"`        // 并发数
	PortThreshold     int    `json:"portThreshold"`     // 开放端口数量阈值
	ScanType          string `json:"scanType"`          // s=SYN, c=CONNECT
	SkipHostDiscovery bool   `json:"skipHostDiscovery"` // 跳过主机发现 (-Pn)
	ExcludeCDN        bool   `json:"excludeCDN"`        // 排除 CDN/WAF (-ec)
	ExcludeHosts      string `json:"excludeHosts"`      // 排除的目标
	Retries           int    `json:"retries"`           // 重试次数
	WarmUpTime        int    `json:"warmUpTime"`        // 扫描阶段间等待时间(秒)
	Workers           int    `json:"workers"`           // Naabu 内部工作线程
	Verify            bool   `json:"verify"`            // TCP 验证
	AggregatedTimeout int    `json:"aggregatedTimeout"` // 聚合超时（秒），当>0时按目标数分摊为单目标超时
}

// Validate 验证 PortScanOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *PortScanOptions) Validate() error {
	if o.Tool != "" && o.Tool != "tcp" && o.Tool != "masscan" && o.Tool != "nmap" && o.Tool != "naabu" {
		return fmt.Errorf("tool must be one of: tcp, masscan, nmap, naabu, got %s", o.Tool)
	}
	if o.Rate < 0 {
		return fmt.Errorf("rate must be non-negative, got %d", o.Rate)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.Concurrent < 0 {
		return fmt.Errorf("concurrent must be non-negative, got %d", o.Concurrent)
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

// Scan 执行端口扫描
func (s *PortScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	opts, ok := config.Options.(*PortScanOptions)
	if !ok {
		opts = &PortScanOptions{
			Ports:      "21,22,23,25,80,443,3306,3389,6379,8080",
			Timeout:    5,
			Concurrent: 1,
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

	logFn("INFO", "PortScanner(CLI): Scan start target=%s opts={tool:%s ports:%s timeout:%d concurrent:%d}",
		config.Target, opts.Tool, opts.Ports, opts.Timeout, opts.Concurrent)

	// 解析目标
	targetParseResult := ParseTargetsForPortScan(config.Target)
	for _, t := range config.Targets {
		res := ParseTargetsForPortScan(t)
		targetParseResult.WithPort = append(targetParseResult.WithPort, res.WithPort...)
		targetParseResult.WithoutPort = append(targetParseResult.WithoutPort, res.WithoutPort...)
	}

	logFn("INFO", "PortScanner(CLI): parsed targets: withoutPort=%d withPort=%d", len(targetParseResult.WithoutPort), len(targetParseResult.WithPort))

	var cleanTargets []string
	seenHost := make(map[string]bool)

	for _, host := range targetParseResult.WithoutPort {
		if !seenHost[host] {
			seenHost[host] = true
			cleanTargets = append(cleanTargets, host)
		}
	}

	// 解析配置自带的端口
	ports := parsePorts(opts.Ports)
	portSet := make(map[int]bool)
	for _, p := range ports {
		portSet[p] = true
	}

	// 合并附带口
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

	targets := cleanTargets
	opts.Ports = portsToString(ports)

	logFn("INFO", "PortScanner(CLI): final targets=%d ports=%d", len(targets), len(ports))

	// 执行扫描
	assets := s.scanPorts(ctx, targets, ports, opts)

	logFn("INFO", "PortScanner(CLI): scan completed, found %d open ports", len(assets))

	return &ScanResult{
		MainTaskId: config.MainTaskId,
		Assets:      assets,
	}, nil
}

// scanPorts 扫描端口
func (s *PortScanner) scanPorts(ctx context.Context, targets []string, ports []int, opts *PortScanOptions) []*Asset {
	var assets []*Asset
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 预先解析域名的 IP 和地理位置（避免每个端口重复解析）
	type targetInfo struct {
		target string
		ipv4   []IPInfo
		ipv6   []IPInfo
	}
	targetIPMap := make(map[string]*targetInfo)
	for _, target := range targets {
		if utils.IsIPAddress(target) {
			// 目标本身就是 IP
			locStr, _ := ipLocator.Locate(target)
			location := geolocation.NormalizeLocation(locStr)
			if strings.Contains(target, ":") {
				targetIPMap[target] = &targetInfo{target: target, ipv6: []IPInfo{{IP: target, Location: location}}}
			} else {
				targetIPMap[target] = &targetInfo{target: target, ipv4: []IPInfo{{IP: target, Location: location}}}
			}
			continue
		}
		// 域名：DNS 解析
		ips, err := net.LookupIP(target)
		if err != nil || len(ips) == 0 {
			continue
		}
		info := &targetInfo{target: target}
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				locStr, _ := ipLocator.Locate(ip4.String())
				location := geolocation.NormalizeLocation(locStr)
				info.ipv4 = append(info.ipv4, IPInfo{IP: ip4.String(), Location: location})
			} else {
				locStr, _ := ipLocator.Locate(ip.String())
				location := geolocation.NormalizeLocation(locStr)
				info.ipv6 = append(info.ipv6, IPInfo{IP: ip.String(), Location: location})
			}
		}
		targetIPMap[target] = info
	}

	// 创建任务通道
	taskChan := make(chan struct {
		target string
		port   int
	}, opts.Concurrent)

	if opts.Concurrent <= 0 {
		opts.Concurrent = 1
	}
	if opts.Concurrent > 5 {
		opts.Concurrent = 5
	}

	// 启动工作协程
	for i := 0; i < opts.Concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					if isPortOpenWithContext(ctx, task.target, task.port, opts.Timeout) {
						asset := &Asset{
							Authority: utils.BuildTargetWithPort(task.target, task.port),
							Host:      task.target,
							Port:      task.port,
							Category:  getCategory(task.target),
						}
						// 填充预解析的 IP 和地理位置
						if info, ok := targetIPMap[task.target]; ok {
							asset.IPV4 = info.ipv4
							asset.IPV6 = info.ipv6
						}
						mu.Lock()
						assets = append(assets, asset)
						mu.Unlock()
					}
				}
			}
		}()
	}

	// 分发任务
	for _, target := range targets {
		for _, port := range ports {
			select {
			case <-ctx.Done():
				close(taskChan)
				wg.Wait()
				return assets
			case taskChan <- struct {
				target string
				port   int
			}{target, port}:
			}
		}
	}

	close(taskChan)
	wg.Wait()

	return assets
}

// isPortOpen 检查端口是否开放
func isPortOpen(host string, port int, timeout int) bool {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", address, time.Duration(timeout)*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// isPortOpenWithContext 检查端口是否开放（支持 context 取消）
func isPortOpenWithContext(ctx context.Context, host string, port int, timeout int) bool {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: time.Duration(timeout) * time.Second}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
