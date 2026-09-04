package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"cscan/pkg/geolocation"
	"cscan/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

// DnsxScanner DNS 查询扫描器 (CLI 模式)
type DnsxScanner struct {
	BaseScanner
	executor *CmdExecutor
}

// NewDnsxScanner 创建 Dnsx 扫描器
func NewDnsxScanner() *DnsxScanner {
	return &DnsxScanner{
		BaseScanner: BaseScanner{name: "dnsx"},
		executor:    NewExecutorForTool("dnsx"),
	}
}

// DnsxOptions DNS 扫描选项
type DnsxOptions struct {
	Timeout        int             `json:"timeout"`
	Retries        int             `json:"retries"`
	Resolvers      []string        `json:"resolvers"`
	WildcardIPs    map[string]bool `json:"wildcardIPs"`
	WildcardFilter bool            `json:"wildcardFilter"`
}

// Validate 验证配置
func (o *DnsxOptions) Validate() error {
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.Retries < 0 {
		return fmt.Errorf("retries must be non-negative, got %d", o.Retries)
	}
	return nil
}

// DnsxResult dnsx CLI JSON 输出
type DnsxResult struct {
	Host     string   `json:"host"`
	A        []string `json:"a,omitempty"`
	AAAA     []string `json:"aaaa,omitempty"`
	CNAME    []string `json:"cname,omitempty"`
	Resolver string   `json:"resolver,omitempty"`
}

// Scan 执行 DNS 查询
func (s *DnsxScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		MainTaskId: config.MainTaskId,
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

	opts := &DnsxOptions{
		Timeout: 5,
		Retries: 1,
	}
	if config.Options != nil {
		if o, ok := config.Options.(*DnsxOptions); ok {
			opts = o
		}
	}

	var domains []string
	if len(config.Targets) > 0 {
		domains = config.Targets
	} else if config.Target != "" {
		domains = strings.Split(config.Target, "\n")
	}
	filtered := domains[:0]
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	domains = filtered

	if len(domains) == 0 {
		return result, nil
	}

	// 并发 Worker Pool：每个域名一个 dnsx 进程，完成一个补一个
	concurrency := config.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 5 {
		concurrency = 5
	}
	if concurrency > len(domains) {
		concurrency = len(domains)
	}
	logFn("INFO", "Dnsx(CLI): scanning %d domains with %d workers", len(domains), concurrency)

	type queryResult struct {
		assets []*Asset
		domain string
		err    error
	}
	targetChan := make(chan string, len(domains))
	resultChan := make(chan queryResult, len(domains))
	var scanWg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for domain := range targetChan {
				select {
				case <-ctx.Done():
					resultChan <- queryResult{domain: domain, err: ctx.Err()}
					return
				default:
				}
				assets, err := s.querySingleDomain(ctx, domain, opts, logFn)
				resultChan <- queryResult{assets: assets, domain: domain, err: err}
			}
		}()
	}

dispatch:
	for _, domain := range domains {
		select {
		case <-ctx.Done():
			break dispatch
		case targetChan <- domain:
		}
	}
	close(targetChan)

	go func() {
		scanWg.Wait()
		close(resultChan)
	}()

	var allAssets []*Asset
	for res := range resultChan {
		if res.err != nil {
			logFn("ERROR", "Dnsx: query error for %s: %v", res.domain, res.err)
			continue
		}
		allAssets = append(allAssets, res.assets...)
	}

	result.Assets = allAssets
	return result, nil
}

func (s *DnsxScanner) querySingleDomain(ctx context.Context, domain string, opts *DnsxOptions, logFn func(level, format string, args ...interface{})) ([]*Asset, error) {
	args := []string{
		"-json",
		"-silent",
		"-a", "-aaaa",
		"-cname",
		"-timeout", fmt.Sprintf("%d", opts.Timeout),
		"-retry", fmt.Sprintf("%d", opts.Retries),
		"-disable-update-check",
		domain,
	}
	if len(opts.Resolvers) > 0 {
		args = append(args, "-r", strings.Join(opts.Resolvers, ","))
	}

	logFn("INFO", "[Dnsx] CLI: domain=%s resolvers=%d", domain, len(opts.Resolvers))

	res, err := s.executor.Execute(ctx, args, ExecuteOpts{
		Timeout: time.Duration(opts.Timeout+10) * time.Second,
		LogFn:   logFn,
	})
	if err != nil {
		logFn("DEBUG", "[Dnsx] execution error domain=%s err=%v", domain, err)
		s.executor.LogResult("Dnsx: "+domain, res, err)
		return nil, fmt.Errorf("dnsx execution for %s: %w", domain, err)
	}

	var assets []*Asset
	ipLocator := geolocation.NewIPLocator()

	scanner := newLineScanner(res.Stdout)
	lineCount := 0
	parseFailCount := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineCount++
		var dr DnsxResult
		if err := json.Unmarshal([]byte(line), &dr); err != nil {
			parseFailCount++
			logFn("DEBUG", "[Dnsx] JSON parse failed line=%d: %v, line=%s", lineCount, err, line)
			continue
		}
		if dr.Host == "" {
			continue
		}

		asset := &Asset{
			Authority: dr.Host,
			Host:      dr.Host,
			Category:  "domain",
		}

		for _, ipStr := range dr.A {
			appendIPInfo(asset, ipStr, ipLocator)
		}
		for _, ipStr := range dr.AAAA {
			appendIPInfo(asset, ipStr, ipLocator)
		}

		if len(dr.CNAME) > 0 {
			asset.CName = strings.TrimSuffix(dr.CNAME[0], ".")
		}

		assets = append(assets, asset)
	}

	logFn("DEBUG", "[Dnsx] domain=%s: lines=%d parseFail=%d assets=%d", domain, lineCount, parseFailCount, len(assets))
	return assets, nil
}

// runDnsxLookup 将域名写入临时文件后调用 dnsx CLI，解析每行 JSON 为 DnsxResult
func (s *DnsxScanner) runDnsxLookup(ctx context.Context, domains []string, timeoutSec int) ([]DnsxResult, error) {
	tmpFile, err := os.CreateTemp("", "dnsx-*.txt")
	if err != nil {
		return nil, err
	}
	for _, d := range domains {
		if _, err := tmpFile.WriteString(d + "\n"); err != nil {
			logx.Errorf("[Dnsx] write tmpfile failed: %v", err)
			tmpFile.Close()
			return nil, err
		}
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := []string{
		"-l", tmpPath,
		"-json",
		"-silent",
		"-a",
		"-timeout", "5",
		"-retry", "1",
		"-disable-update-check",
	}

	res, err := s.executor.Execute(ctx, args, ExecuteOpts{Timeout: time.Duration(timeoutSec) * time.Second})
	if err != nil {
		return nil, err
	}

	var results []DnsxResult
	scanner := newLineScanner(res.Stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var dr DnsxResult
		if err := json.Unmarshal([]byte(line), &dr); err != nil {
			continue
		}
		results = append(results, dr)
	}

	return results, nil
}

// DetectWildcard 检测泛解析（使用 dnsx CLI）
func (s *DnsxScanner) DetectWildcard(ctx context.Context, domain string) map[string]bool {
	wildcardIPs := make(map[string]bool)

	testSubdomains := []string{
		fmt.Sprintf("wildcard-test-%d.%s", utils.RandomInt(100000, 999999), domain),
		fmt.Sprintf("random-%d.%s", utils.RandomInt(100000, 999999), domain),
		fmt.Sprintf("nonexistent-%d.%s", utils.RandomInt(100000, 999999), domain),
	}
	logx.Debugf("[Dnsx] DetectWildcard domain=%s tests=%v", domain, testSubdomains)

	results, err := s.runDnsxLookup(ctx, testSubdomains, 30)
	if err != nil {
		logx.Debugf("[Dnsx] DetectWildcard error domain=%s err=%v", domain, err)
		return wildcardIPs
	}
	for _, dr := range results {
		for _, ip := range dr.A {
			wildcardIPs[ip] = true
		}
	}
	logx.Debugf("[Dnsx] DetectWildcard domain=%s: wildcardIPs=%v", domain, wildcardIPs)

	return wildcardIPs
}

// Lookup DNS 查询单个域名
func (s *DnsxScanner) Lookup(ctx context.Context, domain string) ([]string, error) {
	results, err := s.runDnsxLookup(ctx, []string{domain}, 15)
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, dr := range results {
		ips = append(ips, dr.A...)
	}

	return ips, nil
}
