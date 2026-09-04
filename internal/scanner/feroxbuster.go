package scanner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// FeroxbusterScanner 基于 CLI 的目录扫描器（feroxbuster 实现）
type FeroxbusterScanner struct {
	BaseScanner
	executor *CmdExecutor
}

// NewFeroxbusterScanner 创建 feroxbuster 目录扫描器
func NewFeroxbusterScanner() *FeroxbusterScanner {
	return &FeroxbusterScanner{
		BaseScanner: BaseScanner{name: "feroxbuster"},
		executor:    NewExecutorForTool("feroxbuster"),
	}
}

// FeroxbusterResult feroxbuster NDJSON 输出中的 response 记录
type FeroxbusterResult struct {
	Type          string            `json:"type"`
	Url           string            `json:"url"`
	OriginalUrl   string            `json:"original_url"`
	Path          string            `json:"path"`
	Wildcard      bool              `json:"wildcard"`
	Status        int               `json:"status"`
	Method        string            `json:"method"`
	ContentLength int64             `json:"content_length"`
	LineCount     int64             `json:"line_count"`
	WordCount     int64             `json:"word_count"`
	Headers       map[string]string `json:"headers"`
	Extension     string            `json:"extension"`
	Truncated     bool              `json:"truncated"`
	Timestamp     float64           `json:"timestamp"`
}

// Scan 执行目录扫描
func (s *FeroxbusterScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	result := &ScanResult{
		MainTaskId: config.MainTaskId,
	}

	opts := &FFufOptions{
		Threads:         50,
		Timeout:         10,
		FollowRedirect:  false,
		AutoCalibration: true,
	}
	if config.Options != nil {
		if v, ok := config.Options.(*FFufOptions); ok {
			opts = v
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

	if err := opts.Validate(); err != nil {
		logFn("ERROR", "[Feroxbuster] 配置无效: %v", err)
		return nil, fmt.Errorf("invalid feroxbuster options: %w", err)
	}

	if len(opts.Paths) == 0 {
		logFn("WARN", "[Feroxbuster] 未提供扫描路径")
		return result, nil
	}

	targets := s.collectTargets(config, logFn)
	if len(targets) == 0 {
		logFn("WARN", "[Feroxbuster] 无有效目标")
		return result, nil
	}

	wordlistFile, err := s.writeWordlistFile(opts.Paths)
	if err != nil {
		return nil, fmt.Errorf("创建字典临时文件失败: %w", err)
	}
	defer os.Remove(wordlistFile)

	logFn("INFO", "[Feroxbuster] 开始目录扫描，目标数: %d，路径数: %d", len(targets), len(opts.Paths))

	var allAssets []*Asset

	concurrency := opts.Threads
	if concurrency <= 0 {
		concurrency = config.WorkerConcurrency
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 5 {
		concurrency = 5
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}
	logFn("INFO", "[Feroxbuster] 开始目录扫描，目标数: %d，并发: %d", len(targets), concurrency)

	type scanResult struct {
		assets []*Asset
		target string
		err    error
	}
	targetChan := make(chan string, len(targets))
	resultChan := make(chan scanResult, len(targets))
	var scanWg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for target := range targetChan {
				select {
				case <-ctx.Done():
					resultChan <- scanResult{target: target, err: ctx.Err()}
					return
				default:
				}
				logFn("INFO", "[Feroxbuster] 扫描目标: %s", target)
				assets, err := s.scanTarget(ctx, target, wordlistFile, opts, logFn)
				resultChan <- scanResult{assets: assets, target: target, err: err}
			}
		}()
	}

dispatch:
	for _, target := range targets {
		select {
		case <-ctx.Done():
			break dispatch
		case targetChan <- target:
		}
	}
	close(targetChan)

	go func() {
		scanWg.Wait()
		close(resultChan)
	}()

	completed := 0
	for res := range resultChan {
		completed++
		if res.err != nil {
			logFn("WARN", "[Feroxbuster] 扫描目标 %s 失败: %v", res.target, res.err)
		} else {
			allAssets = append(allAssets, res.assets...)
			logFn("INFO", "[Feroxbuster] 目标 %s 发现 %d 个有效路径", res.target, len(res.assets))
			if config.OnTargetDone != nil && len(res.assets) > 0 {
				config.OnTargetDone(res.target, res.assets)
			}
		}
		if config.OnProgress != nil {
			progress := completed * 100 / len(targets)
			config.OnProgress(progress, fmt.Sprintf("已完成 %d/%d 个目标", completed, len(targets)))
		}
	}

	logFn("INFO", "[Feroxbuster] 目录扫描完成，共发现 %d 个有效路径", len(allAssets))
	result.Assets = allAssets
	return result, nil
}

func (s *FeroxbusterScanner) scanTarget(ctx context.Context, target, wordlistFile string, opts *FFufOptions, logFn func(level, format string, args ...interface{})) ([]*Asset, error) {
	scanCtx, scanCancel := context.WithCancel(ctx)
	defer scanCancel()

	baseURL := strings.TrimSuffix(target, "/") + "/FUZZ"

	args := []string{
		"-u", baseURL,
		"-w", wordlistFile,
		"--json",
		"-t", fmt.Sprintf("%d", opts.Threads),
		"--timeout", fmt.Sprintf("%d", opts.Timeout),
	}

	// 状态码匹配
	if len(opts.StatusCodes) > 0 {
		codes := make([]string, len(opts.StatusCodes))
		for i, c := range opts.StatusCodes {
			codes[i] = fmt.Sprintf("%d", c)
		}
		args = append(args, "-s", strings.Join(codes, ","))
	} else {
		args = append(args, "-s", "200,204,301,302,307,401,403,405,500")
	}

	if opts.FollowRedirect {
		args = append(args, "--redirects")
	}
	if opts.Recursion {
		depth := opts.RecursionDepth
		if depth <= 0 {
			depth = 2
		}
		args = append(args, "-d", fmt.Sprintf("%d", depth))
	}
	if opts.Rate > 0 {
		args = append(args, "--rate-limit", fmt.Sprintf("%d", opts.Rate))
	}
	if len(opts.Extensions) > 0 {
		exts := make([]string, len(opts.Extensions))
		for i, e := range opts.Extensions {
			exts[i] = strings.TrimPrefix(e, ".")
		}
		args = append(args, "-x", strings.Join(exts, ","))
	}
	if opts.AutoCalibration {
		args = append(args, "--auto-tune")
	}

	// 过滤参数
	if opts.FilterSize != "" {
		args = append(args, "--filter-size", opts.FilterSize)
	}
	if opts.FilterWords != "" {
		args = append(args, "--filter-words", opts.FilterWords)
	}
	if opts.FilterLines != "" {
		args = append(args, "--filter-lines", opts.FilterLines)
	}
	if opts.FilterRegex != "" {
		args = append(args, "--filter-regex", opts.FilterRegex)
	}

	// 输出到临时文件
	tmpFile, err := os.CreateTemp("", "feroxbuster-*.json")
	if err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args = append(args, "-o", tmpPath, "--silent")

	logFn("INFO", "[Feroxbuster] CLI: target=%s wordlist_configured=%t", target, wordlistFile != "")

	// opts.Timeout 是单请求时限；进程总时限由父级阶段 context 和工具默认值共同约束。
	res, err := s.executor.Execute(scanCtx, args, ExecuteOpts{
		LogFn: logFn,
	})
	if err != nil {
		logFn("DEBUG", "[Feroxbuster] execution error target=%s err=%v", target, err)
		s.executor.LogResult("Feroxbuster: "+target, res, err)
		return nil, fmt.Errorf("feroxbuster execution: %w", err)
	}

	// 读取 NDJSON 输出
	content, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		return nil, fmt.Errorf("read feroxbuster output: %w", readErr)
	}

	var parseData []byte
	if len(content) > 0 {
		parseData = content
	} else if len(res.Stdout) > 0 {
		parseData = []byte(res.Stdout)
	} else {
		return nil, nil
	}

	feroxResults := parseFeroxbusterNDJSON(parseData)
	logFn("DEBUG", "[Feroxbuster] %s: parsed %d results", target, len(feroxResults))

	results := s.convertResults(target, feroxResults)
	s.enrichWithHTTPRequests(ctx, results, opts, logFn)
	return results, nil
}

// parseFeroxbusterNDJSON 解析 feroxbuster 的 NDJSON 输出（每行一个 JSON 对象）
func parseFeroxbusterNDJSON(data []byte) []FeroxbusterResult {
	var results []FeroxbusterResult
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var fr FeroxbusterResult
		if err := json.Unmarshal([]byte(line), &fr); err != nil {
			continue
		}
		if fr.Type == "response" && fr.Url != "" {
			results = append(results, fr)
		}
	}
	return results
}

// convertResults 将 feroxbuster 结果转换为 Asset 列表
func (s *FeroxbusterScanner) convertResults(target string, results []FeroxbusterResult) []*Asset {
	assets := make([]*Asset, 0, len(results))

	parsedTarget, err := url.Parse(target)
	if err != nil {
		return assets
	}

	for _, r := range results {
		if r.Url == "" {
			continue
		}

		parsedURL, err := url.Parse(r.Url)
		if err != nil {
			continue
		}

		port := 80
		if parsedURL.Scheme == "https" {
			port = 443
		}
		if parsedURL.Port() != "" {
			fmt.Sscanf(parsedURL.Port(), "%d", &port)
		}

		authority := parsedURL.Host
		if authority == "" {
			authority = parsedTarget.Host
		}
		hostname := parsedURL.Hostname()
		if hostname == "" {
			hostname = parsedTarget.Hostname()
		}

		contentType := r.Headers["content-type"]
		if contentType == "" {
			contentType = r.Headers["Content-Type"]
		}

		asset := &Asset{
			Authority:     authority,
			Host:          hostname,
			Port:          port,
			Category:      "url",
			Service:       parsedURL.Scheme,
			HttpStatus:    fmt.Sprintf("%d", r.Status),
			IsHTTP:        true,
			Source:        "feroxbuster",
			Path:          parsedURL.Path,
			ContentLength: r.ContentLength,
			ContentType:   contentType,
			ContentWords:  r.WordCount,
			ContentLines:  r.LineCount,
		}

		if redirectLocation := r.Headers["location"]; redirectLocation != "" {
			asset.Title = redirectLocation
		} else if redirectLocation := r.Headers["Location"]; redirectLocation != "" {
			asset.Title = redirectLocation
		}

		assets = append(assets, asset)
	}

	return assets
}

// collectTargets 从 ScanConfig 中提取目标列表（与 FFufScanner 相同逻辑）
func (s *FeroxbusterScanner) collectTargets(config *ScanConfig, logFn func(level, format string, args ...interface{})) []string {
	var targets []string

	if len(config.Assets) > 0 {
		for _, asset := range config.Assets {
			if asset.IsHTTP && IsHTTPService(asset.Service, asset.Port) {
				scheme := "http"
				if asset.Port == 443 || strings.HasPrefix(asset.Service, "https") {
					scheme = "https"
				}
				var baseURL string
				if (scheme == "http" && asset.Port == 80) || (scheme == "https" && asset.Port == 443) {
					baseURL = fmt.Sprintf("%s://%s", scheme, asset.Host)
				} else {
					baseURL = fmt.Sprintf("%s://%s:%d", scheme, asset.Host, asset.Port)
				}
				if asset.Path != "" && asset.Path != "/" {
					path := strings.TrimSuffix(asset.Path, "/")
					baseURL = baseURL + path
					logFn("INFO", "[Feroxbuster] 使用带路径的目标: %s (基础路径: %s)", baseURL, asset.Path)
				}
				targets = append(targets, baseURL)
			} else {
				logFn("DEBUG", "[Feroxbuster] 跳过非HTTP资产: %s:%d", asset.Host, asset.Port)
			}
		}
	} else if len(config.Targets) > 0 {
		targets = config.Targets
	} else if config.Target != "" {
		targets = strings.Split(config.Target, "\n")
	}

	for i, t := range targets {
		targets[i] = normalizeURL(t)
	}

	return targets
}

// writeWordlistFile 将路径列表写入临时文件（与 FFufScanner 相同逻辑）
func (s *FeroxbusterScanner) writeWordlistFile(paths []string) (string, error) {
	tmpFile, err := os.CreateTemp("", "feroxbuster-wordlist-*.txt")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	for _, p := range paths {
		line := strings.TrimSpace(p)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "/")
		if _, err := fmt.Fprintln(tmpFile, line); err != nil {
			os.Remove(tmpFile.Name())
			return "", err
		}
	}

	return tmpFile.Name(), nil
}

// enrichWithHTTPRequests 对扫描结果补充HTTP请求和响应原文（复用 ffuf.go 中的实现）
func (s *FeroxbusterScanner) enrichWithHTTPRequests(ctx context.Context, assets []*Asset, opts *FFufOptions, logFn func(level, format string, args ...interface{})) {
	if len(assets) == 0 {
		return
	}

	concurrency := 10
	if concurrency > len(assets) {
		concurrency = len(assets)
	}

	timeout := time.Duration(opts.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, asset := range assets {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(a *Asset) {
			defer wg.Done()
			defer func() {
				<-sem
				if r := recover(); r != nil {
					logFn("ERROR", "[Feroxbuster] enrichWithHTTPRequests panic: %v", r)
				}
			}()

			select {
			case <-ctx.Done():
				return
			default:
			}

			scheme := a.Service
			if scheme == "" {
				scheme = "http"
			}
			var targetURL string
			if (scheme == "http" && a.Port == 80) || (scheme == "https" && a.Port == 443) {
				targetURL = fmt.Sprintf("%s://%s%s", scheme, a.Host, a.Path)
			} else {
				targetURL = fmt.Sprintf("%s://%s:%d%s", scheme, a.Host, a.Port, a.Path)
			}

			reqRaw, respRaw := fetchHTTPRequestResponse(ctx, targetURL, timeout)
			a.RequestRaw = reqRaw
			a.ResponseRaw = respRaw
		}(asset)
	}

	wg.Wait()
	logFn("INFO", "[Feroxbuster] HTTP请求响应补充完成，共 %d 条", len(assets))
}
