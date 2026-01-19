package scanner

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"cscan/pkg/utils"

	"github.com/projectdiscovery/dnsx/libs/dnsx"
	"github.com/zeromicro/go-zero/core/logx"
)

// SubdomainBruteforceScanner 子域名暴力破解扫描器
type SubdomainBruteforceScanner struct {
	BaseScanner
}

// NewSubdomainBruteforceScanner 创建子域名暴力破解扫描器
func NewSubdomainBruteforceScanner() *SubdomainBruteforceScanner {
	return &SubdomainBruteforceScanner{
		BaseScanner: BaseScanner{name: "subdomain_bruteforce"},
	}
}

// SubdomainBruteforceOptions 子域名暴力破解选项
type SubdomainBruteforceOptions struct {
	Wordlist        string   `json:"wordlist"`        // 字典内容（每行一个前缀）
	Threads         int      `json:"threads"`         // 并发线程数
	RateLimit       int      `json:"rateLimit"`       // 速率限制
	Timeout         int      `json:"timeout"`         // 超时时间(秒)
	Resolvers       []string `json:"resolvers"`       // 自定义DNS解析器
	WildcardFilter  bool     `json:"wildcardFilter"`  // 泛解析过滤
	ResolveDNS      bool     `json:"resolveDNS"`      // 是否解析DNS
	Concurrent      int      `json:"concurrent"`      // DNS解析并发数
	// 扫描引擎选择
	Engine          string   `json:"engine"`          // 扫描引擎: dnsx, ksubdomain (默认ksubdomain)
	Bandwidth       string   `json:"bandwidth"`       // ksubdomain带宽限制，如"5M", "10M", "100M"
	Retry           int      `json:"retry"`           // ksubdomain重试次数
	WildcardMode    string   `json:"wildcardMode"`    // ksubdomain泛解析过滤模式: basic, advanced, none
	// 增强功能
	RecursiveBrute    bool   `json:"recursiveBrute"`    // 递归爆破
	RecursiveDepth    int    `json:"recursiveDepth"`    // 递归深度，默认2
	RecursiveWordlist string `json:"recursiveWordlist"` // 递归爆破字典内容
	WildcardDetect    bool   `json:"wildcardDetect"`    // 泛解析检测并处理
}

// Validate 验证 SubdomainBruteforceOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *SubdomainBruteforceOptions) Validate() error {
	if o.Threads < 0 {
		return fmt.Errorf("threads must be non-negative, got %d", o.Threads)
	}
	if o.RateLimit < 0 {
		return fmt.Errorf("rateLimit must be non-negative, got %d", o.RateLimit)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.Concurrent < 0 {
		return fmt.Errorf("concurrent must be non-negative, got %d", o.Concurrent)
	}
	if o.RecursiveDepth < 0 {
		return fmt.Errorf("recursiveDepth must be non-negative, got %d", o.RecursiveDepth)
	}
	if o.Retry < 0 {
		return fmt.Errorf("retry must be non-negative, got %d", o.Retry)
	}
	// 设置默认引擎
	if o.Engine == "" {
		o.Engine = "ksubdomain"
	}
	return nil
}

// TakeoverResult 子域接管检测结果
type TakeoverResult struct {
	Subdomain   string `json:"subdomain"`
	CName       string `json:"cname"`
	Vulnerable  bool   `json:"vulnerable"`
	Service     string `json:"service"`
	Fingerprint string `json:"fingerprint"`
}

// 子域接管指纹库
var takeoverFingerprints = map[string][]string{
	"github":        {"There isn't a GitHub Pages site here", "For root URLs (like http://example.com/) you must provide an index.html file"},
	"heroku":        {"No such app", "no-such-app.herokuapp.com"},
	"amazonaws":     {"NoSuchBucket", "The specified bucket does not exist"},
	"bitbucket":     {"Repository not found"},
	"ghost":         {"The thing you were looking for is no longer here"},
	"tumblr":        {"There's nothing here.", "Whatever you were looking for doesn't currently exist at this address"},
	"shopify":       {"Sorry, this shop is currently unavailable", "Only one step left!"},
	"wordpress":     {"Do you want to register"},
	"teamwork":      {"Oops - We didn't find your site"},
	"helpjuice":     {"We could not find what you're looking for"},
	"helpscout":     {"No settings were found for this company"},
	"cargo":         {"If you're moving your domain away from Cargo"},
	"statuspage":    {"You are being redirected", "statuspage.io"},
	"uservoice":     {"This UserVoice subdomain is currently available"},
	"surge":         {"project not found"},
	"intercom":      {"This page is reserved for artistic dogs", "Uh oh. That page doesn't exist"},
	"webflow":       {"The page you are looking for doesn't exist or has been moved"},
	"kajabi":        {"The page you were looking for doesn't exist"},
	"thinkific":     {"You may have mistyped the address or the page may have moved"},
	"tave":          {"Sorry, this page is no longer available"},
	"wishpond":      {"https://www.wishpond.com/404?campaign=true"},
	"aftership":     {"Oops.</h2><p class=\"text-muted text-tight\">The page you're looking for doesn't exist"},
	"aha":           {"There is no portal here ... sending you back to Aha!"},
	"tictail":       {"to target URL: <a href=\"https://tictail.com"},
	"brightcove":    {"<p class=\"bc-gallery-error-code\">Error Code: 404</p>"},
	"bigcartel":     {"<h1>Oops! We couldn&#8217;t find that page.</h1>"},
	"acquia":        {"The site you are looking for could not be found"},
	"fastly":        {"Fastly error: unknown domain"},
	"pantheon":      {"The gods are wise, but do not know of the site which you seek"},
	"zendesk":       {"Help Center Closed", "Oops, this help center no longer exists"},
	"desk":          {"Sorry, We Couldn't Find That Page", "Please check the URL and try your request again"},
	"unbounce":      {"The requested URL was not found on this server", "The page you're looking for doesn't exist"},
	"pingdom":       {"Sorry, couldn't find the status page"},
	"tilda":         {"Please renew your subscription"},
	"smartling":     {"Domain is not configured"},
	"campaignmonitor": {"Trying to access your account?", "Double check the URL"},
	"azure":         {"404 Web Site not found"},
}

// Scan 执行子域名暴力破解扫描
func (s *SubdomainBruteforceScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      make([]*Asset, 0),
	}

	// 解析选项
	opts := &SubdomainBruteforceOptions{
		Threads:        100,
		Timeout:        5,
		WildcardFilter: true,
		ResolveDNS:     true,
		Concurrent:     50,
		RecursiveDepth: 2,
	}
	if config.Options != nil {
		if o, ok := config.Options.(*SubdomainBruteforceOptions); ok {
			opts = o
		}
	}

	// 日志辅助函数
	taskLog := func(level, format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger(level, format, args...)
		}
	}

	// 检查字典内容
	if opts.Wordlist == "" {
		taskLog("WARN", "Bruteforce: no wordlist provided, skipping")
		return result, nil
	}

	// 解析目标域名
	domains := s.parseDomains(config.Target)
	if len(domains) == 0 {
		taskLog("INFO", "Bruteforce: no domains to scan")
		return result, nil
	}

	taskLog("INFO", "Bruteforce: scanning %d domains with wordlist", len(domains))

	// 解析字典
	wordlist := s.parseWordlist(opts.Wordlist)
	if len(wordlist) == 0 {
		taskLog("WARN", "Bruteforce: wordlist is empty")
		return result, nil
	}
	taskLog("INFO", "Bruteforce: loaded %d words from wordlist", len(wordlist))

	// 收集所有子域名和资产
	var allSubdomains []string
	var allAssets []*Asset
	var mu sync.Mutex
	useKsubdomain := (opts.Engine == "" || opts.Engine == "ksubdomain")

	for _, domain := range domains {
		select {
		case <-ctx.Done():
			taskLog("INFO", "Bruteforce: cancelled by context")
			return result, ctx.Err()
		default:
		}

		taskLog("INFO", "Bruteforce: scanning %s with engine %s", domain, opts.Engine)

		// 根据引擎选择不同的暴力破解方法
		if useKsubdomain {
			// ksubdomain 直接返回带 IP 信息的资产
			assets, err := s.bruteforceWithKSubdomainAndParseIP(ctx, domain, wordlist, opts, taskLog)
			if err != nil {
				taskLog("WARN", "Bruteforce ksubdomain error for %s: %v", domain, err)
				continue
			}

			// 递归爆破（如果启用）
			if opts.RecursiveBrute && len(assets) > 0 {
				taskLog("INFO", "Bruteforce: starting recursive bruteforce for %s", domain)
				// 提取子域名用于递归
				var subdomains []string
				for _, asset := range assets {
					subdomains = append(subdomains, asset.Host)
				}
				recursiveSubdomains := s.recursiveBruteforce(ctx, domain, subdomains, opts, taskLog)
				// 递归结果需要 DNS 解析
				if len(recursiveSubdomains) > 0 {
					recursiveAssets := s.resolveDomains(ctx, recursiveSubdomains, opts.Concurrent, taskLog)
					assets = append(assets, recursiveAssets...)
				}
			}

			mu.Lock()
			allAssets = append(allAssets, assets...)
			mu.Unlock()

			taskLog("INFO", "Bruteforce: domain %s returned %d assets with IP information", domain, len(assets))
		} else {
			// dnsx 返回子域名列表，需要后续 DNS 解析
			var subdomains []string
			var err error
			subdomains, err = s.bruteforceWithDnsxSDK(ctx, domain, wordlist, opts, taskLog)

			if err != nil {
				taskLog("WARN", "Bruteforce dnsx error for %s: %v", domain, err)
				continue
			}

			// 递归爆破
			if opts.RecursiveBrute && len(subdomains) > 0 {
				taskLog("INFO", "Bruteforce: starting recursive bruteforce for %s", domain)
				recursiveSubdomains := s.recursiveBruteforce(ctx, domain, subdomains, opts, taskLog)
				subdomains = append(subdomains, recursiveSubdomains...)
			}

			mu.Lock()
			allSubdomains = append(allSubdomains, subdomains...)
			mu.Unlock()

			taskLog("INFO", "Bruteforce: domain %s returned %d subdomains (before global deduplication)", domain, len(subdomains))
		}
	}

	// 处理 ksubdomain 的结果（已经包含 IP 信息）
	if useKsubdomain && len(allAssets) > 0 {
		// 去重资产（基于域名）
		assetMap := make(map[string]*Asset)
		for _, asset := range allAssets {
			if existing, ok := assetMap[asset.Host]; ok {
				// 合并 IP 信息
				existing.IPV4 = append(existing.IPV4, asset.IPV4...)
				existing.IPV6 = append(existing.IPV6, asset.IPV6...)
			} else {
				assetMap[asset.Host] = asset
			}
		}
		
		// 转换为列表
		result.Assets = make([]*Asset, 0, len(assetMap))
		for _, asset := range assetMap {
			asset.Source = "bruteforce"
			result.Assets = append(result.Assets, asset)
		}
		
		taskLog("INFO", "Bruteforce: total %d unique assets with IP information from ksubdomain", len(result.Assets))
		return result, nil
	}

	// 处理 dnsx 的结果（需要 DNS 解析）
	if len(allSubdomains) > 0 {
		// 去重
		beforeDedup := len(allSubdomains)
		allSubdomains = utils.UniqueStrings(allSubdomains)
		afterDedup := len(allSubdomains)
		taskLog("INFO", "Bruteforce: total subdomains before deduplication: %d, after deduplication: %d (removed %d duplicates)", beforeDedup, afterDedup, beforeDedup-afterDedup)

		// DNS解析（可选）- 使用dnsx进行DNS解析
		if opts.ResolveDNS {
			taskLog("INFO", "Bruteforce: resolving DNS for %d subdomains using dnsx", len(allSubdomains))
			assets := s.resolveDomains(ctx, allSubdomains, opts.Concurrent, taskLog)
			for _, asset := range assets {
				asset.Source = "bruteforce"
			}
			result.Assets = assets
		} else {
			for _, subdomain := range allSubdomains {
				result.Assets = append(result.Assets, &Asset{
					Authority: subdomain,
					Host:      subdomain,
					Category:  "domain",
					Source:    "bruteforce",
				})
			}
		}
	}

	return result, nil
}

// parseDomains 解析目标中的域名
func (s *SubdomainBruteforceScanner) parseDomains(target string) []string {
	var domains []string
	seen := make(map[string]bool)

	lines := strings.Split(target, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 跳过IP地址
		if ip := net.ParseIP(line); ip != nil {
			continue
		}

		// 跳过带端口的格式
		if strings.Contains(line, ":") {
			continue
		}

		// 跳过URL格式
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			continue
		}

		// 去重
		if !seen[line] {
			seen[line] = true
			domains = append(domains, line)
		}
	}

	return domains
}

// parseWordlist 解析字典内容
func (s *SubdomainBruteforceScanner) parseWordlist(content string) []string {
	var words []string
	seen := make(map[string]bool)

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		word := strings.TrimSpace(line)
		if word == "" || strings.HasPrefix(word, "#") {
			continue
		}

		// 去重
		if !seen[word] {
			seen[word] = true
			words = append(words, word)
		}
	}

	return words
}

// bruteforceWithDnsxSDK 使用dnsx SDK进行暴力破解
// wildcardIPs 参数可选，如果传入nil则会自动检测泛解析
func (s *SubdomainBruteforceScanner) bruteforceWithDnsxSDK(ctx context.Context, domain string, wordlist []string, opts *SubdomainBruteforceOptions, taskLog func(level, format string, args ...interface{})) ([]string, error) {
	return s.bruteforceWithDnsxSDKAndWildcard(ctx, domain, wordlist, opts, nil, taskLog)
}

// bruteforceWithDnsxSDKAndWildcard 使用dnsx SDK进行暴力破解，支持传入预检测的泛解析IP
func (s *SubdomainBruteforceScanner) bruteforceWithDnsxSDKAndWildcard(ctx context.Context, domain string, wordlist []string, opts *SubdomainBruteforceOptions, predetectedWildcardIPs map[string]bool, taskLog func(level, format string, args ...interface{})) ([]string, error) {
	var subdomains []string
	var mu sync.Mutex

	// 泛解析检测（如果没有预检测结果）
	var wildcardIPs map[string]bool
	if opts.WildcardFilter || opts.WildcardDetect {
		if predetectedWildcardIPs != nil {
			wildcardIPs = predetectedWildcardIPs
		} else {
			wildcardIPs = s.detectWildcard(domain)
			if len(wildcardIPs) > 0 {
				taskLog("INFO", "Bruteforce: detected wildcard for %s (%d IPs), will filter wildcard results", domain, len(wildcardIPs))
				// 注意：不再直接跳过域名，而是在结果中过滤泛解析IP
			}
		}
	}

	// 创建dnsx客户端
	dnsxOpts := dnsx.DefaultOptions
	dnsxOpts.MaxRetries = 3

	// 设置自定义解析器
	if len(opts.Resolvers) > 0 {
		dnsxOpts.BaseResolvers = opts.Resolvers
	}

	dnsClient, err := dnsx.New(dnsxOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create dnsx client: %v", err)
	}

	// 并发控制
	concurrent := opts.Concurrent
	if concurrent <= 0 {
		concurrent = 50
	}

	taskChan := make(chan string, concurrent)
	var wg sync.WaitGroup

	processed := 0
	total := len(wordlist)

	// 启动工作协程
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for word := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					subdomain := fmt.Sprintf("%s.%s", word, domain)

					// 使用dnsx SDK进行DNS查询
					result, err := dnsClient.Lookup(subdomain)
					if err != nil || len(result) == 0 {
						continue
					}

					// 泛解析过滤
					if (opts.WildcardFilter || opts.WildcardDetect) && len(wildcardIPs) > 0 {
						isWildcard := true
						for _, ip := range result {
							if !wildcardIPs[ip] {
								isWildcard = false
								break
							}
						}
						if isWildcard {
							continue
						}
					}

					mu.Lock()
					subdomains = append(subdomains, subdomain)
					mu.Unlock()
				}
			}
		}()
	}

	// 发送任务
	for _, word := range wordlist {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return subdomains, ctx.Err()
		case taskChan <- word:
			processed++
			if processed%1000 == 0 {
				taskLog("INFO", "Bruteforce: progress %d/%d", processed, total)
			}
		}
	}

	close(taskChan)
	wg.Wait()

	return subdomains, nil
}

// recursiveBruteforce 递归爆破子域名
func (s *SubdomainBruteforceScanner) recursiveBruteforce(ctx context.Context, rootDomain string, foundSubdomains []string, opts *SubdomainBruteforceOptions, taskLog func(level, format string, args ...interface{})) []string {
	var allNewSubdomains []string
	depth := opts.RecursiveDepth
	if depth <= 0 {
		depth = 2
	}

	// 解析递归爆破字典
	var recursiveWordlist []string
	if opts.RecursiveWordlist != "" {
		recursiveWordlist = s.parseWordlist(opts.RecursiveWordlist)
	}
	
	// 如果没有指定递归字典，跳过递归爆破
	if len(recursiveWordlist) == 0 {
		taskLog("WARN", "Bruteforce: no recursive wordlist provided, skipping recursive bruteforce")
		return allNewSubdomains
	}

	taskLog("INFO", "Bruteforce: recursive wordlist loaded %d words", len(recursiveWordlist))

	// 预检测根域名的泛解析（缓存结果，避免重复检测）
	var rootWildcardIPs map[string]bool
	if opts.WildcardFilter || opts.WildcardDetect {
		rootWildcardIPs = s.detectWildcard(rootDomain)
		if len(rootWildcardIPs) > 0 {
			taskLog("INFO", "Bruteforce: recursive - root domain %s has wildcard (%d IPs), will filter wildcard results", rootDomain, len(rootWildcardIPs))
			// 注意：不再直接跳过递归爆破，而是在结果中过滤泛解析IP
		}
	}

	currentLevel := foundSubdomains
	for level := 1; level <= depth; level++ {
		select {
		case <-ctx.Done():
			return allNewSubdomains
		default:
		}

		if len(currentLevel) == 0 {
			break
		}

		taskLog("INFO", "Bruteforce: recursive level %d, scanning %d subdomains", level, len(currentLevel))

		var levelSubdomains []string
		var mu sync.Mutex

		// 对每个已发现的子域名进行递归爆破（使用缓存的泛解析结果）
		for _, subdomain := range currentLevel {
			// 跳过根域名
			if subdomain == rootDomain {
				continue
			}

			// 使用预检测的泛解析结果，避免重复检测
			newSubs, err := s.bruteforceWithDnsxSDKAndWildcard(ctx, subdomain, recursiveWordlist, opts, rootWildcardIPs, taskLog)
			if err != nil {
				continue
			}

			mu.Lock()
			levelSubdomains = append(levelSubdomains, newSubs...)
			mu.Unlock()
		}

		// 去重
		levelSubdomains = utils.UniqueStrings(levelSubdomains)
		
		// 过滤已存在的子域名
		existingSet := make(map[string]bool)
		for _, s := range foundSubdomains {
			existingSet[s] = true
		}
		for _, s := range allNewSubdomains {
			existingSet[s] = true
		}

		var newSubdomains []string
		for _, s := range levelSubdomains {
			if !existingSet[s] {
				newSubdomains = append(newSubdomains, s)
			}
		}

		if len(newSubdomains) == 0 {
			taskLog("INFO", "Bruteforce: recursive level %d found no new subdomains, stopping", level)
			break
		}

		taskLog("INFO", "Bruteforce: recursive level %d found %d new subdomains", level, len(newSubdomains))
		allNewSubdomains = append(allNewSubdomains, newSubdomains...)
		currentLevel = newSubdomains
	}

	return allNewSubdomains
}

// crawlSubdomains 从已发现的子域名响应体和JS中爬取新子域名
func (s *SubdomainBruteforceScanner) crawlSubdomains(ctx context.Context, subdomains []string, rootDomains []string, opts *SubdomainBruteforceOptions, taskLog func(level, format string, args ...interface{})) []string {
	var newSubdomains []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 构建根域名正则
	var domainPatterns []*regexp.Regexp
	for _, domain := range rootDomains {
		// 转义域名中的点
		escapedDomain := strings.ReplaceAll(domain, ".", "\\.")
		// 匹配子域名模式: xxx.domain.com
		pattern := regexp.MustCompile(`(?i)([a-z0-9][-a-z0-9]*\.)*` + escapedDomain)
		domainPatterns = append(domainPatterns, pattern)
	}

	// HTTP客户端
	client := &http.Client{
		Timeout: time.Duration(opts.Timeout) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	concurrent := opts.Concurrent
	if concurrent <= 0 {
		concurrent = 20
	}

	taskChan := make(chan string, concurrent)

	// 启动工作协程
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for subdomain := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					// 尝试HTTP和HTTPS
					for _, scheme := range []string{"https", "http"} {
						url := fmt.Sprintf("%s://%s", scheme, subdomain)
						found := s.extractSubdomainsFromURL(ctx, client, url, domainPatterns, taskLog)
						if len(found) > 0 {
							mu.Lock()
							newSubdomains = append(newSubdomains, found...)
							mu.Unlock()
						}
					}
				}
			}
		}()
	}

	// 发送任务
	for _, subdomain := range subdomains {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return utils.UniqueStrings(newSubdomains)
		case taskChan <- subdomain:
		}
	}

	close(taskChan)
	wg.Wait()

	// 去重并过滤已存在的子域名
	existingSet := make(map[string]bool)
	for _, s := range subdomains {
		existingSet[s] = true
	}

	var filteredSubdomains []string
	for _, s := range utils.UniqueStrings(newSubdomains) {
		if !existingSet[s] {
			filteredSubdomains = append(filteredSubdomains, s)
		}
	}

	return filteredSubdomains
}

// extractSubdomainsFromURL 从URL响应中提取子域名
func (s *SubdomainBruteforceScanner) extractSubdomainsFromURL(ctx context.Context, client *http.Client, url string, patterns []*regexp.Regexp, taskLog func(level, format string, args ...interface{})) []string {
	var subdomains []string

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return subdomains
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return subdomains
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 限制1MB
	if err != nil {
		return subdomains
	}

	bodyStr := string(body)

	// 从响应体中提取子域名
	for _, pattern := range patterns {
		matches := pattern.FindAllString(bodyStr, -1)
		subdomains = append(subdomains, matches...)
	}

	// 提取JS文件URL并爬取
	jsPattern := regexp.MustCompile(`(?i)(?:src|href)=["']([^"']*\.js[^"']*)["']`)
	jsMatches := jsPattern.FindAllStringSubmatch(bodyStr, -1)
	for _, match := range jsMatches {
		if len(match) > 1 {
			jsURL := match[1]
			// 处理相对路径
			if strings.HasPrefix(jsURL, "//") {
				jsURL = "https:" + jsURL
			} else if strings.HasPrefix(jsURL, "/") {
				jsURL = url + jsURL
			} else if !strings.HasPrefix(jsURL, "http") {
				jsURL = url + "/" + jsURL
			}

			// 爬取JS文件
			jsSubdomains := s.extractSubdomainsFromJS(ctx, client, jsURL, patterns)
			subdomains = append(subdomains, jsSubdomains...)
		}
	}

	return subdomains
}

// extractSubdomainsFromJS 从JS文件中提取子域名
func (s *SubdomainBruteforceScanner) extractSubdomainsFromJS(ctx context.Context, client *http.Client, jsURL string, patterns []*regexp.Regexp) []string {
	var subdomains []string

	req, err := http.NewRequestWithContext(ctx, "GET", jsURL, nil)
	if err != nil {
		return subdomains
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return subdomains
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 限制2MB
	if err != nil {
		return subdomains
	}

	bodyStr := string(body)

	for _, pattern := range patterns {
		matches := pattern.FindAllString(bodyStr, -1)
		subdomains = append(subdomains, matches...)
	}

	return subdomains
}

// checkSubdomainTakeover 检查子域接管漏洞
func (s *SubdomainBruteforceScanner) checkSubdomainTakeover(ctx context.Context, assets []*Asset, opts *SubdomainBruteforceOptions, taskLog func(level, format string, args ...interface{})) {
	var wg sync.WaitGroup

	concurrent := opts.Concurrent
	if concurrent <= 0 {
		concurrent = 20
	}

	taskChan := make(chan *Asset, concurrent)

	// HTTP客户端
	client := &http.Client{
		Timeout: time.Duration(opts.Timeout) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// 启动工作协程
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for asset := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					result := s.checkTakeover(ctx, client, asset)
					if result != nil && result.Vulnerable {
						taskLog("WARN", "Takeover: %s is vulnerable! CNAME: %s, Service: %s", 
							result.Subdomain, result.CName, result.Service)
						// 在资产中标记接管风险
						asset.TakeoverRisk = true
						asset.TakeoverService = result.Service
						asset.TakeoverCName = result.CName
					}
				}
			}
		}()
	}

	// 发送任务
	for _, asset := range assets {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return
		case taskChan <- asset:
		}
	}

	close(taskChan)
	wg.Wait()
}

// checkTakeover 检查单个子域名的接管风险
func (s *SubdomainBruteforceScanner) checkTakeover(ctx context.Context, client *http.Client, asset *Asset) *TakeoverResult {
	subdomain := asset.Host
	if subdomain == "" {
		return nil
	}

	result := &TakeoverResult{
		Subdomain: subdomain,
	}

	// 获取CNAME记录
	cname, err := net.LookupCNAME(subdomain)
	if err != nil {
		return nil
	}
	cname = strings.TrimSuffix(cname, ".")
	result.CName = cname

	// 检查CNAME是否指向已知的可接管服务
	for service, fingerprints := range takeoverFingerprints {
		if strings.Contains(strings.ToLower(cname), service) {
			// 尝试访问并检查响应
			for _, scheme := range []string{"https", "http"} {
				url := fmt.Sprintf("%s://%s", scheme, subdomain)
				req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
				if err != nil {
					continue
				}
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

				resp, err := client.Do(req)
				if err != nil {
					// 连接失败可能意味着可接管
					result.Vulnerable = true
					result.Service = service
					result.Fingerprint = "Connection failed - potential takeover"
					return result
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
				bodyStr := string(body)

				// 检查指纹
				for _, fp := range fingerprints {
					if strings.Contains(bodyStr, fp) {
						result.Vulnerable = true
						result.Service = service
						result.Fingerprint = fp
						return result
					}
				}
			}
		}
	}

	// 检查NXDOMAIN（DNS记录存在但无法解析）
	_, err = net.LookupIP(subdomain)
	if err != nil {
		// DNS解析失败，检查是否有CNAME指向外部服务
		if cname != subdomain && cname != "" {
			result.Vulnerable = true
			result.Service = "unknown"
			result.Fingerprint = "CNAME exists but target is unresolvable"
			return result
		}
	}

	return result
}

// detectWildcard 检测泛解析
func (s *SubdomainBruteforceScanner) detectWildcard(domain string) map[string]bool {
	wildcardIPs := make(map[string]bool)

	// 创建dnsx客户端用于泛解析检测
	dnsClient, err := dnsx.New(dnsx.DefaultOptions)
	if err != nil {
		logx.Errorf("Failed to create dnsx client for wildcard detection: %v", err)
		return wildcardIPs
	}

	// 使用随机字符串测试泛解析
	testSubdomains := []string{
		fmt.Sprintf("wildcard-test-%d.%s", utils.RandomInt(100000, 999999), domain),
		fmt.Sprintf("random-%d.%s", utils.RandomInt(100000, 999999), domain),
		fmt.Sprintf("nonexistent-%d.%s", utils.RandomInt(100000, 999999), domain),
	}

	for _, subdomain := range testSubdomains {
		result, err := dnsClient.Lookup(subdomain)
		if err == nil && len(result) > 0 {
			for _, ip := range result {
				wildcardIPs[ip] = true
			}
		}
	}

	return wildcardIPs
}

// resolveDomains 使用dnsx进行DNS解析子域名
func (s *SubdomainBruteforceScanner) resolveDomains(ctx context.Context, domains []string, concurrent int, taskLog func(level, format string, args ...interface{})) []*Asset {
	var assets []*Asset
	var mu sync.Mutex
	var wg sync.WaitGroup

	if concurrent <= 0 {
		concurrent = 50
	}

	// 创建dnsx客户端
	dnsxOpts := dnsx.DefaultOptions
	dnsxOpts.MaxRetries = 3
	dnsClient, err := dnsx.New(dnsxOpts)
	if err != nil {
		logx.Errorf("Failed to create dnsx client for resolution: %v", err)
		return assets
	}

	taskChan := make(chan string, concurrent)
	skippedLoopback := 0
	var skippedMu sync.Mutex

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for domain := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					// 使用dnsx进行DNS解析
					result, err := dnsClient.Lookup(domain)
					if err != nil || len(result) == 0 {
						continue
					}

					// 过滤回环地址：如果所有IP都是127.0.0.1等回环地址，跳过该域名
					allLoopback := true
					for _, ip := range result {
						parsedIP := net.ParseIP(ip)
						if parsedIP != nil && !parsedIP.IsLoopback() {
							allLoopback = false
							break
						}
					}
					if allLoopback {
						skippedMu.Lock()
						skippedLoopback++
						skippedMu.Unlock()
						continue
					}

					asset := &Asset{
						Authority: domain,
						Host:      domain,
						Category:  "domain",
					}

					for _, ip := range result {
						parsedIP := net.ParseIP(ip)
						if parsedIP == nil {
							continue
						}
						// 跳过回环地址
						if parsedIP.IsLoopback() {
							continue
						}
						if ip4 := parsedIP.To4(); ip4 != nil {
							asset.IPV4 = append(asset.IPV4, IPInfo{IP: ip4.String()})
						} else {
							asset.IPV6 = append(asset.IPV6, IPInfo{IP: parsedIP.String()})
						}
					}

					// 使用dnsx查询CNAME
					cname, err := net.LookupCNAME(domain)
					if err == nil && cname != domain+"." {
						asset.CName = strings.TrimSuffix(cname, ".")
					}

					mu.Lock()
					assets = append(assets, asset)
					mu.Unlock()
				}
			}
		}()
	}

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
				taskLog("INFO", "Bruteforce DNS resolved: %d/%d", resolved, len(domains))
			}
		}
	}

	close(taskChan)
	wg.Wait()

	// 输出跳过的回环地址域名数量
	if skippedLoopback > 0 && taskLog != nil {
		taskLog("INFO", "Bruteforce: skipped %d domains resolving to loopback address (127.0.0.1)", skippedLoopback)
	}

	return assets
}
// bruteforceWithKSubdomain 使用ksubdomain进行暴力破解
func (s *SubdomainBruteforceScanner) bruteforceWithKSubdomain(ctx context.Context, domain string, wordlist []string, opts *SubdomainBruteforceOptions, taskLog func(level, format string, args ...interface{})) ([]string, error) {
	var subdomains []string
	
	// 创建 output 目录
	outputDir := "output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		taskLog("WARN", "Bruteforce: failed to create output directory: %v", err)
		outputDir = os.TempDir() // 回退到临时目录
	}
	
	// 创建字典文件（保存到 output 目录）
	timestamp := time.Now().Format("20060102_150405")
	dictFile := filepath.Join(outputDir, fmt.Sprintf("ksubdomain_dict_%s_%s.txt", domain, timestamp))
	
	// 写入字典内容（从数据库获取的字典）
	if err := os.WriteFile(dictFile, []byte(strings.Join(wordlist, "\n")), 0644); err != nil {
		return nil, fmt.Errorf("failed to write wordlist file: %v", err)
	}
	
	taskLog("INFO", "Bruteforce: created dictionary file with %d words: %s", len(wordlist), dictFile)
	
	// 创建输出文件（保存到 output 目录）
	outputFile := filepath.Join(outputDir, fmt.Sprintf("ksubdomain_output_%s_%s.txt", domain, timestamp))
	
	// 构建ksubdomain命令
	// ksubdomain e -d example.com -f dict.txt --wild-filter-mode advanced -o output.txt
	args := []string{
		"e",  // 枚举模式
		"-d", domain,
		"-f", dictFile,  // 使用数据库中的字典文件
		"--wild-filter-mode", "advanced",  // 泛解析过滤模式
		"-o", outputFile,
	}
	
	// 添加带宽限制
	if opts.Bandwidth != "" {
		args = append(args, "-b", opts.Bandwidth)
	}
	
	// 添加重试次数
	if opts.Retry > 0 {
		args = append(args, "--retry", fmt.Sprintf("%d", opts.Retry))
	}
	
	// 自定义泛解析过滤模式（如果指定）
	if opts.WildcardMode != "" && opts.WildcardMode != "none" && opts.WildcardMode != "advanced" {
		// 替换默认的advanced模式
		for i, arg := range args {
			if arg == "--wild-filter-mode" && i+1 < len(args) {
				args[i+1] = opts.WildcardMode
				break
			}
		}
	}
	
	// 添加DNS服务器
	if len(opts.Resolvers) > 0 {
		args = append(args, "-r", strings.Join(opts.Resolvers, ","))
	}
	
	taskLog("INFO", "Bruteforce: executing ksubdomain with args: %v", args)
	
	// 设置超时（防止卡住）
	// 子域名爆破需要较长时间，使用独立的超时设置
	timeout := 3 * time.Minute // 默认30分钟超时
	if opts.Timeout > 60 {
		// 如果配置的超时时间大于60秒，则使用配置的值
		timeout = time.Duration(opts.Timeout) * time.Second
	}
	
	// 创建带超时的context
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	// 执行ksubdomain命令
	cmd := exec.CommandContext(cmdCtx, "ksubdomain", args...)
	
	// 捕获输出
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	// 执行命令
	err := cmd.Run()
	
	// 检查是否超时
	if cmdCtx.Err() == context.DeadlineExceeded {
		taskLog("WARN", "Bruteforce: ksubdomain execution timeout after %v", timeout)
		return subdomains, fmt.Errorf("ksubdomain execution timeout after %v", timeout)
	}
	
	if err != nil {
		// 检查是否是因为ksubdomain未安装
		if strings.Contains(err.Error(), "executable file not found") {
			taskLog("WARN", "Bruteforce: ksubdomain not found, falling back to dnsx")
			return s.bruteforceWithDnsxSDK(ctx, domain, wordlist, opts, taskLog)
		}
		taskLog("WARN", "Bruteforce: ksubdomain execution error: %v, stderr: %s, falling back to dnsx", err, stderr.String())
		// 如果ksubdomain执行失败，回退到dnsx
		return s.bruteforceWithDnsxSDK(ctx, domain, wordlist, opts, taskLog)
	}
	
	taskLog("INFO", "Bruteforce: ksubdomain execution completed, reading results from: %s", outputFile)
	
	// 读取输出文件
	content, err := os.ReadFile(outputFile)
	if err != nil {
		taskLog("WARN", "Bruteforce: failed to read ksubdomain output file: %v, falling back to dnsx", err)
		return s.bruteforceWithDnsxSDK(ctx, domain, wordlist, opts, taskLog)
	}
	
	// 解析结果（ksubdomain格式：域名=>IP）
	// 例如：www.qq.com=>198.18.119.65
	lines := strings.Split(string(content), "\n")
	totalLines := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		totalLines++
		
		// 解析 ksubdomain 格式：域名=>IP
		parts := strings.Split(line, "=>")
		if len(parts) >= 1 {
			subdomain := strings.TrimSpace(parts[0])
			if subdomain != "" {
				subdomains = append(subdomains, subdomain)
			}
		}
	}
	
	taskLog("INFO", "Bruteforce: ksubdomain output file contains %d lines, parsed %d subdomains (before deduplication)", totalLines, len(subdomains))
	
	return subdomains, nil
}




// bruteforceWithKSubdomainAndParseIP 使用ksubdomain进行暴力破解并解析IP信息
// 返回带IP信息的资产列表
func (s *SubdomainBruteforceScanner) bruteforceWithKSubdomainAndParseIP(ctx context.Context, domain string, wordlist []string, opts *SubdomainBruteforceOptions, taskLog func(level, format string, args ...interface{})) ([]*Asset, error) {
	var assets []*Asset
	
	// 创建 output 目录
	outputDir := "output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		taskLog("WARN", "Bruteforce: failed to create output directory: %v", err)
		outputDir = os.TempDir() // 回退到临时目录
	}
	
	// 创建字典文件（保存到 output 目录）
	timestamp := time.Now().Format("20060102_150405")
	dictFile := filepath.Join(outputDir, fmt.Sprintf("ksubdomain_dict_%s_%s.txt", domain, timestamp))
	
	// 写入字典内容（从数据库获取的字典）
	if err := os.WriteFile(dictFile, []byte(strings.Join(wordlist, "\n")), 0644); err != nil {
		return nil, fmt.Errorf("failed to write wordlist file: %v", err)
	}
	
	taskLog("INFO", "Bruteforce: created dictionary file with %d words: %s", len(wordlist), dictFile)
	
	// 创建输出文件（保存到 output 目录）
	outputFile := filepath.Join(outputDir, fmt.Sprintf("ksubdomain_output_%s_%s.txt", domain, timestamp))
	
	// 构建ksubdomain命令
	args := []string{
		"e",  // 枚举模式
		"-d", domain,
		"-f", dictFile,
		"--wild-filter-mode", "advanced",
		"-o", outputFile,
	}
	
	// 添加带宽限制
	if opts.Bandwidth != "" {
		args = append(args, "-b", opts.Bandwidth)
	}
	
	// 添加重试次数
	if opts.Retry > 0 {
		args = append(args, "--retry", fmt.Sprintf("%d", opts.Retry))
	}
	
	// 自定义泛解析过滤模式
	if opts.WildcardMode != "" && opts.WildcardMode != "none" && opts.WildcardMode != "advanced" {
		for i, arg := range args {
			if arg == "--wild-filter-mode" && i+1 < len(args) {
				args[i+1] = opts.WildcardMode
				break
			}
		}
	}
	
	// 添加DNS服务器
	if len(opts.Resolvers) > 0 {
		args = append(args, "-r", strings.Join(opts.Resolvers, ","))
	}
	
	taskLog("INFO", "Bruteforce: executing ksubdomain with args: %v", args)
	
	// 设置超时
	timeout := 30 * time.Minute
	if opts.Timeout > 60 {
		timeout = time.Duration(opts.Timeout) * time.Second
	}
	
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	cmd := exec.CommandContext(cmdCtx, "ksubdomain", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	err := cmd.Run()
	
	if cmdCtx.Err() == context.DeadlineExceeded {
		taskLog("WARN", "Bruteforce: ksubdomain execution timeout after %v", timeout)
		return assets, fmt.Errorf("ksubdomain execution timeout after %v", timeout)
	}
	
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			taskLog("WARN", "Bruteforce: ksubdomain not found")
			return assets, fmt.Errorf("ksubdomain not found")
		}
		taskLog("WARN", "Bruteforce: ksubdomain execution error: %v, stderr: %s", err, stderr.String())
		return assets, fmt.Errorf("ksubdomain execution error: %v", err)
	}
	
	taskLog("INFO", "Bruteforce: ksubdomain execution completed, parsing results with IP from: %s", outputFile)
	
	// 读取输出文件
	content, err := os.ReadFile(outputFile)
	if err != nil {
		taskLog("WARN", "Bruteforce: failed to read ksubdomain output file: %v", err)
		return assets, err
	}
	
	// 解析结果（ksubdomain格式：域名=>IP）
	// 例如：www.qq.com=>198.18.119.65
	lines := strings.Split(string(content), "\n")
	totalLines := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		totalLines++
		
		// 解析 ksubdomain 格式：域名=>IP
		// 注意：分隔符是固定的 "=>"
		if !strings.Contains(line, "=>") {
			continue
		}
		
		parts := strings.Split(line, "=>")
		if len(parts) != 2 {
			continue
		}
		
		subdomain := strings.TrimSpace(parts[0])
		ipStr := strings.TrimSpace(parts[1])
		
		if subdomain == "" {
			continue
		}
		
		asset := &Asset{
			Authority: subdomain,
			Host:      subdomain,
			Category:  "domain",
			Source:    "bruteforce",
			Port:      0, // 子域名爆破阶段没有端口信息
		}
		
		// 解析IP地址（如果有）
		if ipStr != "" {
			ip := net.ParseIP(ipStr)
			if ip != nil {
				if ip4 := ip.To4(); ip4 != nil {
					asset.IPV4 = []IPInfo{{IP: ip4.String()}}
				} else {
					asset.IPV6 = []IPInfo{{IP: ip.String()}}
				}
			}
		}
		
		assets = append(assets, asset)
	}
	
	taskLog("INFO", "Bruteforce: ksubdomain output file contains %d lines, parsed %d assets with IP information", totalLines, len(assets))
	
	return assets, nil
}
