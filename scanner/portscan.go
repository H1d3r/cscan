package scanner

import (
	"context"
	"cscan/pkg/utils"
	"fmt"
	"net"
	"sync"
	"time"
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
	Tool          string `json:"tool"` // tcp, masscan, nmap
	Ports         string `json:"ports"`
	Rate          int    `json:"rate"`
	Timeout       int    `json:"timeout"`
	Concurrent    int    `json:"concurrent"`
	PortThreshold int    `json:"portThreshold"` // 开放端口数量阈值，超过则过滤该主机
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
	return nil
}

// Scan 执行端口扫描
func (s *PortScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	opts, ok := config.Options.(*PortScanOptions)
	if !ok {
		opts = &PortScanOptions{
			Ports:      "21,22,23,25,80,443,3306,3389,6379,8080",
			Timeout:    3,
			Concurrent: 100,
		}
	}

	// 解析目标
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

	// 执行扫描
	assets := s.scanPorts(ctx, targets, ports, opts)

	return &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      assets,
	}, nil
}

// scanPorts 扫描端口
func (s *PortScanner) scanPorts(ctx context.Context, targets []string, ports []int, opts *PortScanOptions) []*Asset {
	var assets []*Asset
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 创建任务通道
	taskChan := make(chan struct {
		target string
		port   int
	}, opts.Concurrent)

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
					if isPortOpen(task.target, task.port, opts.Timeout) {
						asset := &Asset{
							Authority: utils.BuildTargetWithPort(task.target, task.port),
							Host:      task.target,
							Port:      task.port,
							Category:  getCategory(task.target),
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
	address := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", address, time.Duration(timeout)*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
