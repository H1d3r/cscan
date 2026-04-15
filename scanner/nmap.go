package scanner

import (
	"bytes"
	"cscan/pkg/geolocation"
	"cscan/pkg/utils"
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

	// 日志函数，优先使用任务日志回调
	logInfo := func(format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger("INFO", format, args...)
		}
		logx.Infof(format, args...)
	}
	logWarn := func(format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger("WARN", format, args...)
		}
		logx.Infof(format, args...)
	}
	logError := func(format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger("ERROR", format, args...)
		}
		logx.Errorf(format, args...)
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
		case map[string]interface{}:
			// 处理从 scheduler.PortIdentifyConfig 传递的配置
			if ports, ok := v["ports"].(string); ok && ports != "" {
				opts.Ports = ports
			}
			if timeout, ok := v["timeout"].(int); ok && timeout > 0 {
				opts.Timeout = timeout
			}
			if concurrent, ok := v["concurrency"].(int); ok && concurrent > 0 {
				opts.Concurrent = concurrent
			}
		default:
			// 尝试通过JSON转换
			if data, err := json.Marshal(config.Options); err == nil {
				json.Unmarshal(data, opts)
			}
		}
	}

	// 确保并发数至少为1，最大不超过5（避免过度并发）
	if opts.Concurrent <= 0 {
		opts.Concurrent = 1
	}
	if opts.Concurrent > 5 {
		logWarn("Nmap concurrent %d exceeds maximum 5, limiting to 5", opts.Concurrent)
		opts.Concurrent = 5
	}

	// 检查nmap是否安装
	if !checkNmapInstalled() {
		logError("nmap not installed, falling back to tcp scan")
		// 回退到TCP扫描
		tcpScanner := NewPortScanner()
		return tcpScanner.Scan(ctx, config)
	}

	// 解析目标
	targetParseResult := ParseTargetsForPortScan(config.Target)
	for _, t := range config.Targets {
		res := ParseTargetsForPortScan(t)
		targetParseResult.WithPort = append(targetParseResult.WithPort, res.WithPort...)
		targetParseResult.WithoutPort = append(targetParseResult.WithoutPort, res.WithoutPort...)
	}

	var cleanTargets []string
	seenTarget := make(map[string]bool)

	for _, host := range targetParseResult.WithoutPort {
		if !seenTarget[host] {
			seenTarget[host] = true
			cleanTargets = append(cleanTargets, host)
		}
	}

	ports := parsePorts(opts.Ports)
	portSet := make(map[int]bool)
	for _, p := range ports {
		portSet[p] = true
	}

	for _, taskWithPort := range targetParseResult.WithPort {
		if !seenTarget[taskWithPort.Host] {
			seenTarget[taskWithPort.Host] = true
			cleanTargets = append(cleanTargets, taskWithPort.Host)
		}
		if !portSet[taskWithPort.Port] {
			portSet[taskWithPort.Port] = true
			ports = append(ports, taskWithPort.Port)
		}
	}

	targets := cleanTargets
	opts.Ports = portsToString(ports)

	// 执行nmap扫描
	assets := s.runNmapWithLogger(ctx, targets, opts, config.OnProgress, logInfo, logWarn, logError)

	return &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      assets,
	}, nil
}

// runNmapWithLogger 运行nmap（带日志回调）
// 优化为每个端口一个进程，通过并发控制降低扫描影响
func (s *NmapScanner) runNmapWithLogger(ctx context.Context, targets []string, opts *NmapOptions, onProgress func(int, string), logInfo, _ logFunc, logError logFunc) []*Asset {
	var assets []*Asset
	var mu sync.Mutex

	// 使用 parsePorts 解析端口
	ports := parsePorts(opts.Ports)
	totalPorts := len(ports)

	if totalPorts == 0 {
		logInfo("Nmap: no ports to scan")
		return assets
	}

	logInfo("Nmap: starting scan, %d ports, %d targets, concurrent=%d", totalPorts, len(targets), opts.Concurrent)

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
					result := s.scanSinglePortWithLogger(ctx, targets, task.port, opts, logInfo, logError)
					if len(result) > 0 {
						mu.Lock()
						assets = append(assets, result...)
						mu.Unlock()
					}

					// 更新进度
					if onProgress != nil && totalPorts > 0 {
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

	logInfo("Nmap: scan completed, found %d open ports", len(assets))
	return assets
}

// scanSinglePortWithLogger 扫描单个端口（带日志回调）
func (s *NmapScanner) scanSinglePortWithLogger(ctx context.Context, targets []string, port int, opts *NmapOptions, logInfo, logError logFunc) []*Asset {
	var assets []*Asset

	// 构建nmap命令
	args := []string{
		"-Pn",                         // 跳过主机发现
		"-p", fmt.Sprintf("%d", port), // 单个端口
		"-oX", "-", // XML输出到stdout
	}

	// 添加额外参数
	if opts.Args != "" {
		extraArgs := strings.Fields(opts.Args)
		args = append(args, extraArgs...)
	}

	args = append(args, targets...)

	// 输出执行命令到日志
	logInfo("Nmap: executing nmap -Pn -p %d %s", port, strings.Join(targets, " "))

	cmd := exec.CommandContext(ctx, "nmap", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	if err != nil {
		logError("Nmap: failed to start nmap for port %d: %v", port, err)
		return assets
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var output []byte
	select {
	case <-ctx.Done():
		// 超时，强制终止进程
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done // 等待Wait返回
		logError("Nmap: port %d timeout after %ds, killed", port, opts.Timeout)
		return assets
	case err := <-done:
		if err != nil {
			logError("Nmap: error for port %d: %v (stderr: %s)", port, err, stderr.String())
			return assets
		}
		output = stdout.Bytes()
	}

	// 解析XML输出
	var nmapRun NmapRun
	if err := xml.Unmarshal(output, &nmapRun); err != nil {
		logError("Nmap: xml parse error for port %d: %v", port, err)
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

				// 填充已解析的 IP 和地理位置
				if ip != "" && !utils.IsIPAddress(hostStr) {
					locStr, _ := ipLocator.Locate(ip)
					location := geolocation.NormalizeLocation(locStr)
					if strings.Contains(ip, ":") {
						asset.IPV6 = []IPInfo{{IP: ip, Location: location}}
					} else {
						asset.IPV4 = []IPInfo{{IP: ip, Location: location}}
					}
				} else if ip != "" && utils.IsIPAddress(hostStr) {
					// 原始目标本身就是 IP
					locStr, _ := ipLocator.Locate(ip)
					location := geolocation.NormalizeLocation(locStr)
					asset.IPV4 = []IPInfo{{IP: ip, Location: location}}
				}
				// 如果有产品信息，添加到App
				if nmapPort.Service.Product != "" {
					productInfo := nmapPort.Service.Product
					if nmapPort.Service.Version != "" {
						productInfo += ":" + nmapPort.Service.Version
					}
					asset.App = []string{productInfo}
					// 输出详细服务指纹日志
					logInfo("发现存活服务: %s:%d -> %s (产品: %s)", originalTarget, nmapPort.PortID, nmapPort.Service.Name, productInfo)
				} else {
					logInfo("发现存活服务: %s:%d -> %s", originalTarget, nmapPort.PortID, nmapPort.Service.Name)
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
