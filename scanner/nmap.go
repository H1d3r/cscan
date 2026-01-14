package scanner

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// NmapScanner Nmap扫描器
type NmapScanner struct {
	BaseScanner
}

// NewNmapScanner 创建Nmap扫描器
func NewNmapScanner() *NmapScanner {
	return &NmapScanner{
		BaseScanner: BaseScanner{name: "nmap"},
	}
}

// NmapOptions Nmap扫描选项
type NmapOptions struct {
	Ports      string `json:"ports"`
	Rate       int    `json:"rate"`
	Timeout    int    `json:"timeout"`
	Args       string `json:"args"`       // 额外参数
	Concurrent int    `json:"concurrent"` // 并发扫描的端口数，默认为1（每次扫描一个端口）
}

// Validate 验证 NmapOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *NmapOptions) Validate() error {
	if o.Rate < 0 {
		return fmt.Errorf("rate must be non-negative, got %d", o.Rate)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.Concurrent < 0 {
		return fmt.Errorf("concurrent must be non-negative, got %d", o.Concurrent)
	}
	return nil
}

// NmapRun Nmap XML输出结构
type NmapRun struct {
	XMLName xml.Name   `xml:"nmaprun"`
	Hosts   []NmapHost `xml:"host"`
}

type NmapHost struct {
	Addresses []NmapAddress `xml:"address"`
	Ports     NmapPorts     `xml:"ports"`
}

type NmapAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

// GetIPv4Address 获取IPv4地址（忽略MAC地址）
func (h *NmapHost) GetIPv4Address() string {
	for _, addr := range h.Addresses {
		if addr.AddrType == "ipv4" {
			return addr.Addr
		}
	}
	// 如果没有ipv4，尝试ipv6
	for _, addr := range h.Addresses {
		if addr.AddrType == "ipv6" {
			return addr.Addr
		}
	}
	// 最后返回第一个非mac地址
	for _, addr := range h.Addresses {
		if addr.AddrType != "mac" {
			return addr.Addr
		}
	}
	return ""
}

type NmapPorts struct {
	Ports []NmapPort `xml:"port"`
}

type NmapPort struct {
	Protocol string      `xml:"protocol,attr"`
	PortID   int         `xml:"portid,attr"`
	State    NmapState   `xml:"state"`
	Service  NmapService `xml:"service"`
}

type NmapState struct {
	State string `xml:"state,attr"`
}

type NmapService struct {
	Name    string `xml:"name,attr"`
	Product string `xml:"product,attr"`
	Version string `xml:"version,attr"`
}

// Scan 执行Nmap扫描
func (s *NmapScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	// 默认配置
	opts := &NmapOptions{
		Ports:      "21,22,23,25,80,443,3306,3389,6379,8080",
		Timeout:    3,
		Concurrent: 1, // 默认每次扫描一个端口，降低扫描影响
	}

	// 尝试从不同类型的Options中提取配置
	if config.Options != nil {
		switch v := config.Options.(type) {
		case *NmapOptions:
			opts = v
		case *PortScanOptions:
			if v.Ports != "" {
				opts.Ports = v.Ports
			}
			if v.Timeout > 0 {
				opts.Timeout = v.Timeout
			}
			if v.Concurrent > 0 {
				opts.Concurrent = v.Concurrent
			}
		default:
			// 尝试通过JSON转换
			if data, err := json.Marshal(config.Options); err == nil {
				json.Unmarshal(data, opts)
			}
		}
	}

	// 确保并发数至少为1
	if opts.Concurrent <= 0 {
		opts.Concurrent = 1
	}

	// 检查nmap是否安装
	if !checkNmapInstalled() {
		logx.Error("nmap not installed, falling back to tcp scan")
		// 回退到TCP扫描
		tcpScanner := NewPortScanner()
		return tcpScanner.Scan(ctx, config)
	}

	// 解析目标
	targets := parseTargets(config.Target)
	if len(config.Targets) > 0 {
		targets = append(targets, config.Targets...)
	}

	// 执行nmap扫描
	assets := s.runNmap(ctx, targets, opts, config.OnProgress)

	return &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      assets,
	}, nil
}

// runNmap 运行nmap
// 优化为每个端口一个进程，通过并发控制降低扫描影响
func (s *NmapScanner) runNmap(ctx context.Context, targets []string, opts *NmapOptions, onProgress func(int, string)) []*Asset {
	var assets []*Asset
	var mu sync.Mutex

	// 使用 parsePorts 解析端口
	ports := parsePorts(opts.Ports)
	totalPorts := len(ports)

	if totalPorts == 0 {
		logx.Info("No ports to scan")
		return assets
	}

	logx.Infof("Starting nmap scan: %d ports, %d targets, concurrent=%d", totalPorts, len(targets), opts.Concurrent)

	// 创建任务通道
	type scanTask struct {
		port  int
		index int
	}
	taskChan := make(chan scanTask, opts.Concurrent)

	// 使用 WaitGroup 等待所有扫描完成
	var wg sync.WaitGroup

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
					// 执行单端口扫描
					result := s.scanSinglePort(ctx, targets, task.port, opts)
					if len(result) > 0 {
						mu.Lock()
						assets = append(assets, result...)
						mu.Unlock()
					}

					// 更新进度
					if onProgress != nil {
						progress := (task.index + 1) * 100 / totalPorts
						onProgress(progress, fmt.Sprintf("Scanning port %d (%d/%d)", task.port, task.index+1, totalPorts))
					}
				}
			}
		}()
	}

	// 分发任务
	for i, port := range ports {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return assets
		case taskChan <- scanTask{port: port, index: i}:
		}
	}

	close(taskChan)
	wg.Wait()

	logx.Infof("Nmap scan completed: found %d open ports", len(assets))
	return assets
}

// scanSinglePort 扫描单个端口
func (s *NmapScanner) scanSinglePort(ctx context.Context, targets []string, port int, opts *NmapOptions) []*Asset {
	var assets []*Asset

	// 构建nmap命令
	args := []string{
		"-Pn",                          // 跳过主机发现
		"-p", fmt.Sprintf("%d", port),  // 单个端口
		"-oX", "-",                     // XML输出到stdout
	}

	// 添加额外参数
	if opts.Args != "" {
		extraArgs := strings.Fields(opts.Args)
		args = append(args, extraArgs...)
	}

	args = append(args, targets...)

	// 输出执行命令到日志
	logx.Infof("Executing command: nmap %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "nmap", args...)
	output, err := cmd.Output()
	if err != nil {
		// 检查是否是上下文取消
		if ctx.Err() != nil {
			return assets
		}
		logx.Errorf("nmap error for port %d: %v", port, err)
		return assets
	}

	// 解析XML输出
	var nmapRun NmapRun
	if err := xml.Unmarshal(output, &nmapRun); err != nil {
		logx.Errorf("nmap xml parse error for port %d: %v", port, err)
		return assets
	}

	for _, host := range nmapRun.Hosts {
		// 获取IPv4地址（忽略MAC地址）
		ip := host.GetIPv4Address()
		if ip == "" {
			continue
		}

		// 查找原始目标（可能是域名）
		originalTarget := ip
		for _, target := range targets {
			if getCategory(target) == "domain" {
				originalTarget = target
				break
			}
		}

		for _, nmapPort := range host.Ports.Ports {
			if nmapPort.State.State == "open" {
				authority := fmt.Sprintf("%s:%d", originalTarget, nmapPort.PortID)
				hostStr := originalTarget
				category := getCategory(originalTarget)

				asset := &Asset{
					Authority: authority,
					Host:      hostStr,
					Port:      nmapPort.PortID,
					Category:  category,
					Service:   nmapPort.Service.Name,
				}
				// 如果有产品信息，添加到App
				if nmapPort.Service.Product != "" {
					productInfo := nmapPort.Service.Product
					if nmapPort.Service.Version != "" {
						productInfo += ":" + nmapPort.Service.Version
					}
					asset.App = []string{productInfo}
				}
				assets = append(assets, asset)
			}
		}
	}

	return assets
}

// checkNmapInstalled 检查nmap是否安装
func checkNmapInstalled() bool {
	cmd := exec.Command("nmap", "--version")
	err := cmd.Run()
	return err == nil
}

// CheckNmapInstalled 导出的检查函数，供外部调用
func CheckNmapInstalled() bool {
	return checkNmapInstalled()
}
