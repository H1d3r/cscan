package scanner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cscan/pkg/utils"

	"github.com/projectdiscovery/dnsx/libs/dnsx"
	"github.com/projectdiscovery/subfinder/v2/pkg/runner"
	"github.com/zeromicro/go-zero/core/logx"
)

// SubfinderScanner Subfinder子域名扫描器
type SubfinderScanner struct {
	BaseScanner
}

// NewSubfinderScanner 创建Subfinder扫描器
func NewSubfinderScanner() *SubfinderScanner {
	return &SubfinderScanner{
		BaseScanner: BaseScanner{name: "subfinder"},
	}
}

// SubfinderOptions Subfinder扫描选项
type SubfinderOptions struct {
	Timeout            int                 `json:"timeout"`            // 超时时间(秒)
	MaxEnumerationTime int                 `json:"maxEnumerationTime"` // 最大枚举时间(分钟)
	Threads            int                 `json:"threads"`            // Subfinder SDK 内部线程数，默认10
	RateLimit          int                 `json:"rateLimit"`          // 速率限制
	Sources            []string            `json:"sources"`            // 指定数据源
	ExcludeSources     []string            `json:"excludeSources"`     // 排除数据源
	All                bool                `json:"all"`                // 使用所有数据源(慢)
	Recursive          bool                `json:"recursive"`          // 只使用递归数据源
	RemoveWildcard     bool                `json:"removeWildcard"`     // 移除泛解析域名
	ProviderConfig     map[string][]string `json:"providerConfig"`     // API配置 (从数据库加载)
	ResolveDNS         bool                `json:"resolveDNS"`         // 是否解析DNS
	Concurrent         int                 `json:"concurrent"`         // DNS解析并发数，默认50（内部参数，不受 Worker 并发限制）
}

// Validate 验证 SubfinderOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *SubfinderOptions) Validate() error {
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.MaxEnumerationTime < 0 {
		return fmt.Errorf("maxEnumerationTime must be non-negative, got %d", o.MaxEnumerationTime)
	}
	if o.Threads < 0 {
		return fmt.Errorf("threads must be non-negative, got %d", o.Threads)
	}
	if o.RateLimit < 0 {
		return fmt.Errorf("rateLimit must be non-negative, got %d", o.RateLimit)
	}
	if o.Concurrent < 0 {
		return fmt.Errorf("concurrent must be non-negative, got %d", o.Concurrent)
	}
	return nil
}

// Scan 执行Subfinder子域名扫描
func (s *SubfinderScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      make([]*Asset, 0),
	}

	// 解析选项
	opts := &SubfinderOptions{
		Timeout:            30,
		MaxEnumerationTime: 10,
		Threads:            10,
		RateLimit:          0,
		RemoveWildcard:     true,
		ResolveDNS:         true,
		Concurrent:         50,
	}
	if config.Options != nil {
		if o, ok := config.Options.(*SubfinderOptions); ok {
			opts = o
		}
	}

	// 日志辅助函数
	taskLog := func(level, format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger(level, format, args...)
		}
	}

	// 解析目标域名，区分根域名和子域名
	parseResult := s.parseDomainsWithType(config.Target)

	// 收集所有需要处理的子域名
	var allSubdomains []string

	// 1. 子域名目标直接添加到结果（不需要枚举）
	if len(parseResult.Subdomains) > 0 {
		taskLog("INFO", "Subfinder: %d subdomains detected, skipping enumeration", len(parseResult.Subdomains))
		logx.Infof("Subfinder: %d subdomain targets detected, will be used directly", len(parseResult.Subdomains))
		allSubdomains = append(allSubdomains, parseResult.Subdomains...)
	}

	// 2. 根域名才执行子域名枚举
	if len(parseResult.RootDomains) > 0 {
		taskLog("INFO", "Subfinder: enumerating %d root domains", len(parseResult.RootDomains))

		var mu sync.Mutex
		for _, domain := range parseResult.RootDomains {
			select {
			case <-ctx.Done():
				logx.Info("Subfinder scan cancelled by context")
				return result, ctx.Err()
			default:
			}

			taskLog("INFO", "Subfinder: enumerating %s", domain)
			subdomains, err := s.enumerateDomain(ctx, domain, opts)
			if err != nil {
				logx.Errorf("Subfinder error for %s: %v", domain, err)
				taskLog("WARN", "Subfinder: %s error: %v", domain, err)
				continue
			}

			mu.Lock()
			allSubdomains = append(allSubdomains, subdomains...)
			mu.Unlock()

			taskLog("INFO", "Subfinder: found %d subdomains for %s", len(subdomains), domain)
		}
	}

	// 如果没有任何域名需要处理
	if len(allSubdomains) == 0 {
		logx.Info("No domains for subfinder scan")
		return result, nil
	}

	// 去重
	allSubdomains = utils.UniqueStrings(allSubdomains)
	taskLog("INFO", "Subfinder: total %d unique subdomains", len(allSubdomains))

	// DNS解析（可选）
	if opts.ResolveDNS && len(allSubdomains) > 0 {
		taskLog("INFO", "Resolving DNS for %d subdomains", len(allSubdomains))
		assets := s.resolveDomains(ctx, allSubdomains, opts.Concurrent, taskLog)
		// 设置Source字段
		for _, asset := range assets {
			asset.Source = "subfinder"
		}
		result.Assets = assets
		taskLog("INFO", "Subfinder: resolved %d assets", len(assets))
	} else {
		// 不解析DNS，直接返回域名作为资产
		for _, subdomain := range allSubdomains {
			result.Assets = append(result.Assets, &Asset{
				Authority: subdomain,
				Host:      subdomain,
				Category:  "domain",
				Source:    "subfinder",
			})
		}
	}

	return result, nil
}

// DomainParseResult 域名解析结果
type DomainParseResult struct {
	RootDomains []string // 根域名列表（需要枚举子域名）
	Subdomains  []string // 子域名列表（直接作为资产返回）
}

// parseDomains 解析目标中的域名（跳过IP地址）
// 区分根域名和子域名：
// - 根域名（如 example.com）：使用 subfinder 枚举子域名
// - 子域名（如 api.example.com）：直接作为资产返回，不需要再枚举
func (s *SubfinderScanner) parseDomains(target string) []string {
	result := s.parseDomainsWithType(target)
	return result.RootDomains
}

// parseDomainsWithType 解析目标中的域名，区分根域名和子域名
func (s *SubfinderScanner) parseDomainsWithType(target string) *DomainParseResult {
	result := &DomainParseResult{
		RootDomains: make([]string, 0),
		Subdomains:  make([]string, 0),
	}
	seenRoot := make(map[string]bool)
	seenSub := make(map[string]bool)

	lines := strings.Split(target, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析目标信息
		info := utils.ParseTarget(line)

		// 跳过IP地址
		if info.IsIP {
			continue
		}

		// 跳过非域名
		if !info.IsDomain {
			continue
		}

		domain := info.Host

		// 区分子域名和根域名
		if info.IsSubdomain {
			// 子域名：直接作为资产，不枚举
			if !seenSub[domain] {
				seenSub[domain] = true
				result.Subdomains = append(result.Subdomains, domain)
			}
		} else {
			// 根域名：使用 subfinder 枚举
			if !seenRoot[domain] {
				seenRoot[domain] = true
				result.RootDomains = append(result.RootDomains, domain)
			}
		}
	}

	return result
}

// enumerateDomain 枚举单个域名的子域名
func (s *SubfinderScanner) enumerateDomain(ctx context.Context, domain string, opts *SubfinderOptions) ([]string, error) {
	// 如果有Provider配置，创建临时配置文件
	var tempConfigFile string
	if len(opts.ProviderConfig) > 0 {
		configContent := BuildProviderConfig(opts.ProviderConfig)
		if configContent != "" {
			tmpDir := os.TempDir()
			tempConfigFile = filepath.Join(tmpDir, "subfinder_provider_config.yaml")
			if err := os.WriteFile(tempConfigFile, []byte(configContent), 0600); err != nil {
				logx.Errorf("Failed to write provider config: %v", err)
				tempConfigFile = ""
			} else {
				logx.Infof("Created provider config: %s", tempConfigFile)
			}
		}
	}

	// 构建Subfinder选项
	runnerOpts := &runner.Options{
		Threads:            opts.Threads,
		Timeout:            opts.Timeout,
		MaxEnumerationTime: opts.MaxEnumerationTime,
		All:                opts.All,
		OnlyRecursive:      opts.Recursive,
		RemoveWildcard:     opts.RemoveWildcard,
	}

	// 设置Provider配置文件路径 - 必须在NewRunner之前设置
	// NewRunner会调用loadProvidersFrom来加载配置
	if tempConfigFile != "" {
		logx.Debug("tempConfigFile的路径是=", tempConfigFile)
		runnerOpts.ProviderConfig = tempConfigFile
	}

	// 设置速率限制
	if opts.RateLimit > 0 {
		runnerOpts.RateLimit = opts.RateLimit
	}

	// 只有用户显式指定了Sources才设置
	if len(opts.Sources) > 0 {
		runnerOpts.Sources = opts.Sources
		logx.Infof("Using specified sources: %v", opts.Sources)
	}
	if len(opts.ExcludeSources) > 0 {
		runnerOpts.ExcludeSources = opts.ExcludeSources
	}

	// 创建Runner - 这里会自动调用loadProvidersFrom加载provider配置
	subfinder, err := runner.NewRunner(runnerOpts)
	if err != nil {
		if tempConfigFile != "" {
			os.Remove(tempConfigFile)
		}
		logx.Errorf("Failed to create subfinder runner: %v", err)
		return nil, err
	}

	var output bytes.Buffer
	logx.Infof("Starting subfinder enumeration for domain: %s", domain)

	// 执行枚举
	sourceMap, err := subfinder.EnumerateSingleDomainWithCtx(ctx, domain, []io.Writer{&output})

	// 清理临时文件
	// if tempConfigFile != "" {
	// 	os.Remove(tempConfigFile)
	// }

	if err != nil {
		logx.Errorf("Subfinder enumeration error: %v", err)
		return nil, err
	}

	// 从sourceMap提取子域名
	var subdomains []string
	for subdomain, sources := range sourceMap {
		subdomains = append(subdomains, subdomain)
		sourcesList := make([]string, 0, len(sources))
		for source := range sources {
			sourcesList = append(sourcesList, source)
		}
		logx.Debugf("Found subdomain: %s from sources: %v", subdomain, sourcesList)
	}

	// 如果sourceMap为空，尝试从buffer解析
	if len(subdomains) == 0 && output.Len() > 0 {
		logx.Infof("Parsing from buffer (%d bytes)", output.Len())
		lines := strings.Split(output.String(), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				subdomains = append(subdomains, line)
			}
		}
	}

	logx.Infof("Subfinder found %d subdomains for %s", len(subdomains), domain)
	return subdomains, nil
}

// dnsResolver DNS解析器接口 - 消除重复代码
type dnsResolver interface {
	resolve(domain string) ([]net.IP, error)
}

// dnsxResolver 使用dnsx库的解析器
type dnsxResolver struct {
	client *dnsx.DNSX
}

func (r *dnsxResolver) resolve(domain string) ([]net.IP, error) {
	result, err := r.client.Lookup(domain)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(result))
	for _, ipStr := range result {
		if ip := net.ParseIP(ipStr); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips, nil
}

// stdlibResolver 使用标准库的解析器
type stdlibResolver struct{}

func (r *stdlibResolver) resolve(domain string) ([]net.IP, error) {
	return net.LookupIP(domain)
}

// resolveDomains 使用dnsx进行DNS解析子域名
func (s *SubfinderScanner) resolveDomains(ctx context.Context, domains []string, concurrent int, taskLog func(level, format string, args ...interface{})) []*Asset {
	// 创建dnsx客户端
	dnsxOpts := dnsx.DefaultOptions
	dnsxOpts.MaxRetries = 3
	dnsClient, err := dnsx.New(dnsxOpts)
	if err != nil {
		logx.Errorf("Failed to create dnsx client: %v, falling back to stdlib", err)
		return s.resolveDomainsWithResolver(ctx, domains, concurrent, taskLog, &stdlibResolver{})
	}
	return s.resolveDomainsWithResolver(ctx, domains, concurrent, taskLog, &dnsxResolver{client: dnsClient})
}

// resolveDomainsWithResolver 通用DNS解析实现 - 消除重复代码
func (s *SubfinderScanner) resolveDomainsWithResolver(ctx context.Context, domains []string, concurrent int, taskLog func(level, format string, args ...interface{}), resolver dnsResolver) []*Asset {
	if concurrent <= 0 {
		concurrent = 50
	}

	var (
		assets   []*Asset
		mu       sync.Mutex
		wg       sync.WaitGroup
		taskChan = make(chan string, concurrent)
	)

	// 启动工作协程
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for domain := range taskChan {
				if ctx.Err() != nil {
					return
				}
				if asset := s.resolveSingleDomain(domain, resolver); asset != nil {
					mu.Lock()
					assets = append(assets, asset)
					mu.Unlock()
				}
			}
		}()
	}

	// 分发任务
	resolved := 0
	for _, domain := range domains {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return assets
		case taskChan <- domain:
			resolved++
			if resolved%100 == 0 && taskLog != nil {
				taskLog("INFO", "DNS resolved: %d/%d", resolved, len(domains))
			}
		}
	}

	close(taskChan)
	wg.Wait()
	return assets
}

// resolveSingleDomain 解析单个域名 - 提取公共逻辑
func (s *SubfinderScanner) resolveSingleDomain(domain string, resolver dnsResolver) *Asset {
	ips, err := resolver.resolve(domain)
	if err != nil || len(ips) == 0 {
		return nil
	}

	// 过滤回环地址：如果所有IP都是127.0.0.1等回环地址，跳过该域名
	// 防止扫描本地服务造成安全风险
	allLoopback := true
	for _, ip := range ips {
		if !ip.IsLoopback() {
			allLoopback = false
			break
		}
	}
	if allLoopback {
		return nil
	}

	asset := &Asset{
		Authority: domain,
		Host:      domain,
		Category:  "domain",
	}

	for _, ip := range ips {
		// 跳过回环地址，不添加到资产IP列表中
		if ip.IsLoopback() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			asset.IPV4 = append(asset.IPV4, IPInfo{IP: ip4.String()})
		} else {
			asset.IPV6 = append(asset.IPV6, IPInfo{IP: ip.String()})
		}
	}

	if cname, err := net.LookupCNAME(domain); err == nil && cname != domain+"." {
		asset.CName = strings.TrimSuffix(cname, ".")
	}

	return asset
}

// BuildProviderConfig 构建Subfinder provider配置文件内容
// 格式必须是标准YAML列表格式:
// provider:
//   - key1
//   - key2
func BuildProviderConfig(configs map[string][]string) string {
	if len(configs) == 0 {
		return ""
	}

	// Subfinder支持的所有provider (需要API key的)
	allProviders := []string{
		"alienvault", "bevigil", "bufferover", "builtwith", "c99",
		"censys", "certspotter", "chaos", "chinaz", "digitalyama",
		"dnsdb", "dnsdumpster", "dnsrepo", "domainsproject", "driftnet",
		"facebook", "fofa", "fullhunt", "github", "intelx",
		"leakix", "merklemap", "netlas", "onyphe", "profundis",
		"pugrecon", "quake", "redhuntlabs", "robtex", "rsecloud",
		"securitytrails", "shodan", "threatbook", "virustotal",
		"whoisxmlapi", "windvane", "zoomeyeapi",
	}

	var sb strings.Builder
	for _, provider := range allProviders {
		keys, exists := configs[provider]
		if exists && len(keys) > 0 {
			// 有配置的provider - 使用YAML列表格式
			sb.WriteString(provider + ":\n")
			for _, key := range keys {
				sb.WriteString("  - " + key + "\n")
			}
		} else {
			// 没有配置的provider - 空列表
			sb.WriteString(provider + ": []\n")
		}
	}
	return sb.String()
}
