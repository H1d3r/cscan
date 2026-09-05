package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cscan/pkg/geolocation"

	"github.com/zeromicro/go-zero/core/logx"
)

// PortIdentifyOutcome is the explicit Nmap conclusion for one normalized
// host-port pair. A missing Asset must never be used to infer one of these
// outcomes.
type PortIdentifyOutcome string

const (
	PortOpen       PortIdentifyOutcome = "OPEN"
	PortClosed     PortIdentifyOutcome = "CLOSED"
	PortFiltered   PortIdentifyOutcome = "FILTERED"
	PortTimeout    PortIdentifyOutcome = "TIMEOUT"
	PortExecError  PortIdentifyOutcome = "EXEC_ERROR"
	PortParseError PortIdentifyOutcome = "PARSE_ERROR"
	PortCanceled   PortIdentifyOutcome = "CANCELED"
	PortNoRecord   PortIdentifyOutcome = "NO_HOST_RECORD"
)

// Stable Nmap reason codes. Callers aggregate these codes rather than matching
// process or XML error text.
const (
	NmapReasonLaunchError  = "nmap_launch_error"
	NmapReasonNonzeroExit  = "nmap_nonzero_exit"
	NmapReasonXMLParse     = "nmap_xml_parse_error"
	NmapReasonTimeout      = "nmap_timeout"
	NmapReasonCanceled     = "nmap_canceled"
	NmapReasonNoHostRecord = "nmap_no_host_record"
)

// PortIdentifyResult is the one-for-one result of dispatching a normalized
// host-port pair to Nmap. Service metadata and ResolvedIP are populated only
// for OPEN results.
type PortIdentifyResult struct {
	Host       string              `json:"host"`
	ResolvedIP string              `json:"resolvedIp,omitempty"`
	Port       int                 `json:"port"`
	Outcome    PortIdentifyOutcome `json:"outcome"`
	Service    string              `json:"service,omitempty"`
	Product    string              `json:"product,omitempty"`
	Version    string              `json:"version,omitempty"`
	ErrorCode  string              `json:"errorCode,omitempty"`
}

type nmapCommandResult struct {
	Stdout   []byte
	Stderr   []byte
	StartErr error
	WaitErr  error
}

type nmapCommandRunner func(context.Context, []string) nmapCommandResult

// NmapScanner Nmap扫描器
type NmapScanner struct {
	BaseScanner
	commandRunner nmapCommandRunner
}

// logFunc 日志函数类型
type logFunc func(format string, args ...interface{})

// progressFunc 进度回调函数类型
type progressFunc func(progress int, message string)

// NewNmapScanner 创建Nmap扫描器
func NewNmapScanner() *NmapScanner {
	return &NmapScanner{
		BaseScanner:   BaseScanner{name: "nmap"},
		commandRunner: runNmapCommand,
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
	Hostnames NmapHostnames `xml:"hostnames"`
	Ports     NmapPorts     `xml:"ports"`
}

type NmapHostnames struct {
	Names []NmapHostname `xml:"hostname"`
}

type NmapHostname struct {
	Name string `xml:"name,attr"`
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
			return // 已由 TaskLogger 统一输出，避免双写
		}
		logx.Infof(format, args...)
	}
	logWarn := func(format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger("WARN", format, args...)
			return // 已由 TaskLogger 统一输出，避免双写
		}
		logx.Slowf(format, args...)
	}
	logError := func(format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger("ERROR", format, args...)
			return
		}
		logx.Errorf(format, args...)
	}
	logDebug := func(format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger("DEBUG", format, args...)
			return
		}
		logx.Debugf(format, args...)
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
				_ = json.Unmarshal(data, opts)
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

	// 端口识别超时以用户配置为准（默认 30s，来源于 scheduler.PortIdentifyConfig.Timeout）。
	const maxPortIdentifyTimeout = 600
	if opts.Timeout <= 0 {
		opts.Timeout = 30
	}
	if opts.Timeout > maxPortIdentifyTimeout {
		logWarn("Nmap timeout %ds exceeds maximum %ds for port identification, limiting to %ds", opts.Timeout, maxPortIdentifyTimeout, maxPortIdentifyTimeout)
		opts.Timeout = maxPortIdentifyTimeout
	}

	// P0-5: 校验额外参数 Args，防止参数注入
	if opts.Args != "" {
		if err := ValidateNmapArgs(opts.Args); err != nil {
			logError("Invalid nmap args %q: %v", opts.Args, err)
			return nil, fmt.Errorf("invalid nmap args: %w", err)
		}
	}

	// 检查nmap是否安装
	if !checkNmapInstalled() {
		logError("nmap not installed, falling back to tcp scan")
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
	assets, identifyResults := s.runNmapWithLogger(ctx, targets, opts, config.OnTargetDone, config.OnProgress, logInfo, logWarn, logError, logDebug)
	if config.EventLogger != nil {
		for _, identify := range identifyResults {
			config.EventLogger(EventNmapPortResult, "nmap", string(identify.Outcome), map[string]interface{}{
				"host": identify.Host, "port": identify.Port, "outcome": string(identify.Outcome),
				"service": identify.Service, "error_code": identify.ErrorCode, "duration_ms": int64(0),
			})
		}
	}

	return &ScanResult{
		MainTaskId:          config.MainTaskId,
		Assets:              assets,
		PortIdentifyResults: identifyResults,
	}, nil
}

// runNmapWithLogger runs one Nmap command per unique port. It returns exactly
// one PortIdentifyResult for every normalized target in every command that was
// dispatched to a worker.
func (s *NmapScanner) runNmapWithLogger(
	ctx context.Context, targets []string, opts *NmapOptions,
	onTargetDone func(string, []*Asset), onProgress func(int, string),
	logInfo, logWarn logFunc, logError, logDebug logFunc,
) ([]*Asset, []PortIdentifyResult) {
	normalizedTargets := normalizeNmapTargets(targets)
	ports := uniqueNmapPorts(parsePorts(opts.Ports))
	if len(ports) == 0 || len(normalizedTargets) == 0 {
		return nil, nil
	}

	concurrent := opts.Concurrent
	if concurrent <= 0 {
		concurrent = 1
	}
	if concurrent > len(ports) {
		concurrent = len(ports)
	}

	type scanTask struct {
		port  int
		index int
	}
	type completedTask struct {
		index   int
		results []PortIdentifyResult
	}

	taskChan := make(chan scanTask, concurrent)
	completedChan := make(chan completedTask, len(ports))
	var finishedCount int32
	var wg sync.WaitGroup

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				results := s.scanSinglePortWithLevelLogger(ctx, normalizedTargets, task.port, opts, logInfo, logWarn, logError, logDebug)
				completedChan <- completedTask{index: task.index, results: results}

				if onTargetDone != nil {
					onTargetDone(fmt.Sprintf("port:%d", task.port), portIdentifyResultsToAssets(results, logInfo))
				}

				finished := atomic.AddInt32(&finishedCount, 1)
				if onProgress != nil {
					onProgress(int(finished)*100/len(ports), fmt.Sprintf("Scanning port %d (%d/%d)", task.port, finished, len(ports)))
				}
			}
		}()
	}

	dispatched := 0
dispatch:
	for i, port := range ports {
		select {
		case taskChan <- scanTask{port: port, index: i}:
			dispatched++
		case <-ctx.Done():
			break dispatch
		}
	}
	close(taskChan)
	wg.Wait()
	close(completedChan)

	completed := make([]completedTask, 0, dispatched)
	for item := range completedChan {
		completed = append(completed, item)
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].index < completed[j].index })

	identifyResults := make([]PortIdentifyResult, 0, dispatched*len(normalizedTargets))
	for _, item := range completed {
		identifyResults = append(identifyResults, item.results...)
	}
	assets := portIdentifyResultsToAssets(identifyResults, logInfo)
	return assets, identifyResults
}

// scanSinglePortWithLogger keeps the existing test seam while production uses
// a distinct WARN logger for expected timeouts.
func (s *NmapScanner) scanSinglePortWithLogger(ctx context.Context, targets []string, port int, opts *NmapOptions, logInfo, logError, logDebug logFunc) []PortIdentifyResult {
	return s.scanSinglePortWithLevelLogger(ctx, targets, port, opts, logInfo, logError, logError, logDebug)
}

// scanSinglePortWithLevelLogger scans one port across all normalized targets
// and always returns one result per target, including process and parse failures.
func (s *NmapScanner) scanSinglePortWithLevelLogger(ctx context.Context, targets []string, port int, opts *NmapOptions, logInfo, logWarn, logError, logDebug logFunc) []PortIdentifyResult {
	targets = normalizeNmapTargets(targets)
	if len(targets) == 0 {
		return nil
	}

	args := []string{
		"-Pn",
		"-p", fmt.Sprintf("%d", port),
		"-oX", "-",
	}
	if opts.Args != "" {
		args = append(args, strings.Fields(opts.Args)...)
	}
	args = append(args, targets...)

	timeout := time.Duration(opts.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	portCtx, portCancel := context.WithTimeout(ctx, timeout)
	defer portCancel()

	if !acquireProcessSlot(portCtx) {
		if errors.Is(portCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return uniformPortIdentifyResults(targets, port, PortTimeout, NmapReasonTimeout)
		}
		logError("Nmap: canceled while waiting for process slot")
		return uniformPortIdentifyResults(targets, port, PortCanceled, NmapReasonCanceled)
	}
	defer releaseProcessSlot()

	logInfo("系统执行命令：%s", FormatCommandLine("nmap", args))
	runner := s.commandRunner
	if runner == nil {
		runner = runNmapCommand
	}
	commandResult := runner(portCtx, args)

	if commandResult.StartErr != nil {
		logError("Nmap: failed to start nmap for port %d: %v", port, commandResult.StartErr)
		return uniformPortIdentifyResults(targets, port, PortExecError, NmapReasonLaunchError)
	}

	if portCtx.Err() != nil || errors.Is(commandResult.WaitErr, context.Canceled) || errors.Is(commandResult.WaitErr, context.DeadlineExceeded) {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(portCtx.Err(), context.Canceled) {
			logError("Nmap: port %d canceled", port)
			return uniformPortIdentifyResults(targets, port, PortCanceled, NmapReasonCanceled)
		}
		return uniformPortIdentifyResults(targets, port, PortTimeout, NmapReasonTimeout)
	}

	if commandResult.WaitErr != nil {
		logError("Nmap: error for port %d: %v", port, commandResult.WaitErr)
		return uniformPortIdentifyResults(targets, port, PortExecError, NmapReasonNonzeroExit)
	}

	var nmapRun NmapRun
	if err := xml.Unmarshal(commandResult.Stdout, &nmapRun); err != nil {
		logError("Nmap: xml parse error for port %d: %v", port, err)
		return uniformPortIdentifyResults(targets, port, PortParseError, NmapReasonXMLParse)
	}

	return alignNmapHostPortResults(targets, port, nmapRun)
}

func runNmapCommand(ctx context.Context, args []string) nmapCommandResult {
	cmd := exec.CommandContext(ctx, "nmap", args...)
	setSysProcAttr(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nmapCommandResult{StartErr: err}
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		killProcessTree(cmd)
		<-done
		return nmapCommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), WaitErr: ctx.Err()}
	case err := <-done:
		return nmapCommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), WaitErr: err}
	}
}

func normalizeNmapTargets(targets []string) []string {
	result := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		normalized := normalizeNmapHost(target)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func normalizeNmapHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func uniqueNmapPorts(ports []int) []int {
	result := make([]int, 0, len(ports))
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if port <= 0 || port > 65535 {
			continue
		}
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		result = append(result, port)
	}
	return result
}

func uniformPortIdentifyResults(targets []string, port int, outcome PortIdentifyOutcome, reasonCode string) []PortIdentifyResult {
	results := make([]PortIdentifyResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, PortIdentifyResult{Host: target, Port: port, Outcome: outcome, ErrorCode: reasonCode})
	}
	return results
}

func alignNmapHostPortResults(targets []string, port int, run NmapRun) []PortIdentifyResult {
	results := make([]PortIdentifyResult, 0, len(targets))
	for _, target := range targets {
		host := findNmapHostForTarget(target, run.Hosts, len(targets) == 1)
		if host == nil {
			results = append(results, PortIdentifyResult{Host: target, Port: port, Outcome: PortNoRecord, ErrorCode: NmapReasonNoHostRecord})
			continue
		}

		var matchedPort *NmapPort
		for i := range host.Ports.Ports {
			if host.Ports.Ports[i].PortID == port {
				matchedPort = &host.Ports.Ports[i]
				break
			}
		}
		if matchedPort == nil {
			results = append(results, PortIdentifyResult{Host: target, Port: port, Outcome: PortNoRecord, ErrorCode: NmapReasonNoHostRecord})
			continue
		}

		switch strings.ToLower(matchedPort.State.State) {
		case "open":
			results = append(results, PortIdentifyResult{
				Host:       target,
				ResolvedIP: host.GetIPv4Address(),
				Port:       port,
				Outcome:    PortOpen,
				Service:    matchedPort.Service.Name,
				Product:    matchedPort.Service.Product,
				Version:    matchedPort.Service.Version,
			})
		case "closed":
			results = append(results, PortIdentifyResult{Host: target, Port: port, Outcome: PortClosed})
		case "filtered", "open|filtered", "closed|filtered", "unfiltered":
			results = append(results, PortIdentifyResult{Host: target, Port: port, Outcome: PortFiltered})
		default:
			results = append(results, PortIdentifyResult{Host: target, Port: port, Outcome: PortNoRecord, ErrorCode: NmapReasonNoHostRecord})
		}
	}
	return results
}

func findNmapHostForTarget(target string, hosts []NmapHost, allowSingleHostFallback bool) *NmapHost {
	target = normalizeNmapHost(target)
	for i := range hosts {
		for _, address := range hosts[i].Addresses {
			if address.AddrType != "mac" && normalizeNmapHost(address.Addr) == target {
				return &hosts[i]
			}
		}
		for _, hostname := range hosts[i].Hostnames.Names {
			if normalizeNmapHost(hostname.Name) == target {
				return &hosts[i]
			}
		}
	}
	// A single domain target is unambiguous even when Nmap omits <hostnames>.
	// Never use this fallback for multi-host commands: doing so would attach one
	// host's state and metadata to a different target sharing the same port.
	if allowSingleHostFallback && len(hosts) == 1 {
		return &hosts[0]
	}
	return nil
}

func portIdentifyResultsToAssets(results []PortIdentifyResult, logInfo logFunc) []*Asset {
	assets := make([]*Asset, 0, len(results))
	for _, result := range results {
		if result.Outcome != PortOpen {
			continue
		}
		asset := &Asset{
			Authority: fmt.Sprintf("%s:%d", result.Host, result.Port),
			Host:      result.Host,
			Port:      result.Port,
			Category:  getCategory(result.Host),
			Service:   result.Service,
		}

		if result.ResolvedIP != "" {
			locStr, _ := ipLocator.Locate(result.ResolvedIP)
			location := geolocation.NormalizeLocation(locStr)
			ipInfo := IPInfo{IP: result.ResolvedIP, Location: location}
			if strings.Contains(result.ResolvedIP, ":") {
				asset.IPV6 = []IPInfo{ipInfo}
			} else {
				asset.IPV4 = []IPInfo{ipInfo}
			}
		}

		if result.Product != "" {
			productInfo := result.Product
			if result.Version != "" {
				productInfo += ":" + result.Version
			}
			asset.App = []string{productInfo}
		}
		assets = append(assets, asset)
	}
	return assets
}

// checkNmapInstalled 检查nmap是否安装
// 修复 C-32：原 exec.Command 无 context，nmap 异常时会永久阻塞
func checkNmapInstalled() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nmap", "--version")
	setSysProcAttr(cmd)
	err := cmd.Run()
	return err == nil
}
