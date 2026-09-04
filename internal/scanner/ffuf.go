package scanner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// FFufScanner 基于 CLI 的目录扫描器
type FFufScanner struct {
	BaseScanner
	executor *CmdExecutor
}

// NewFFufScanner 创建 ffuf 目录扫描器
func NewFFufScanner() *FFufScanner {
	return &FFufScanner{
		BaseScanner: BaseScanner{name: "ffuf"},
		executor:    NewExecutorForTool("ffuf"),
	}
}

// FFufOptions ffuf 扫描选项
type FFufOptions struct {
	Paths           []string `json:"paths"`
	Threads         int      `json:"threads"`
	Timeout         int      `json:"timeout"`
	Extensions      []string `json:"extensions"`
	FollowRedirect  bool     `json:"followRedirect"`
	AutoCalibration bool     `json:"autoCalibration"`
	StatusCodes     []int    `json:"statusCodes"`
	FilterSize      string   `json:"filterSize"`
	FilterWords     string   `json:"filterWords"`
	FilterLines     string   `json:"filterLines"`
	FilterRegex     string   `json:"filterRegex"`
	MatcherMode     string   `json:"matcherMode"`
	FilterMode      string   `json:"filterMode"`
	Rate            int      `json:"rate"`
	Recursion       bool     `json:"recursion"`
	RecursionDepth  int      `json:"recursionDepth"`
}

// Validate 验证配置
func (o *FFufOptions) Validate() error {
	if o.Threads < 0 {
		return fmt.Errorf("threads must be non-negative, got %d", o.Threads)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	for _, statusCode := range o.StatusCodes {
		if statusCode < 100 || statusCode > 599 {
			return fmt.Errorf("status code must be between 100 and 599, got %d", statusCode)
		}
	}
	return nil
}

// FFufCLIResult ffuf 2.x JSON 输出中 results[] 的单条记录
type FFufCLIResult struct {
	Url              string `json:"url"`
	StatusCode       int    `json:"status"`
	ContentLength    int64  `json:"length"`
	ContentWords     int64  `json:"words"`
	ContentLines     int64  `json:"lines"`
	ContentType      string `json:"content-type"`
	RedirectLocation string `json:"redirectlocation"`
	DurationNs       int64  `json:"duration"`
}

// ffufCLIOutput ffuf 2.x JSON 输出顶层包裹结构（-of json）
type ffufCLIOutput struct {
	Results []FFufCLIResult `json:"results"`
}

// Scan 执行目录扫描
func (s *FFufScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
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
		logFn("ERROR", "[FFuf] 配置无效: %v", err)
		return nil, fmt.Errorf("invalid ffuf options: %w", err)
	}

	if len(opts.Paths) == 0 {
		logFn("WARN", "[FFuf] 未提供扫描路径")
		return result, nil
	}

	targets := s.collectTargets(config, logFn)
	if len(targets) == 0 {
		logFn("WARN", "[FFuf] 无有效目标")
		return result, nil
	}

	wordlistFile, err := s.writeWordlistFile(opts.Paths)
	if err != nil {
		return nil, fmt.Errorf("创建字典临时文件失败: %w", err)
	}
	defer os.Remove(wordlistFile)

	logFn("INFO", "[FFuf] 开始目录扫描，目标数: %d，路径数: %d", len(targets), len(opts.Paths))

	var allAssets []*Asset

	// 并发 Worker Pool：每个目标一个 ffuf 进程，完成一个补一个
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
	logFn("INFO", "[FFuf] 开始目录扫描，目标数: %d，并发: %d", len(targets), concurrency)

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
				logFn("INFO", "[FFuf] 扫描目标: %s", target)
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
			logFn("WARN", "[FFuf] 扫描目标 %s 失败: %v", res.target, res.err)
		} else {
			allAssets = append(allAssets, res.assets...)
			logFn("INFO", "[FFuf] 目标 %s 发现 %d 个有效路径", res.target, len(res.assets))
			// 流式入库：每完成一个目标立即回调
			if config.OnTargetDone != nil && len(res.assets) > 0 {
				config.OnTargetDone(res.target, res.assets)
			}
		}
		if config.OnProgress != nil {
			progress := completed * 100 / len(targets)
			config.OnProgress(progress, fmt.Sprintf("已完成 %d/%d 个目标", completed, len(targets)))
		}
	}

	logFn("INFO", "[FFuf] 目录扫描完成，共发现 %d 个有效路径", len(allAssets))
	result.Assets = allAssets
	return result, nil
}

const defaultFFufMatchCodes = "200,204,301,302,307,401,403,405,500"

func buildFFufArgs(baseURL, wordlistFile, outputPath string, opts *FFufOptions) []string {
	matchCodes := defaultFFufMatchCodes
	if len(opts.StatusCodes) > 0 {
		statusCodes := make([]string, len(opts.StatusCodes))
		for i, statusCode := range opts.StatusCodes {
			statusCodes[i] = fmt.Sprintf("%d", statusCode)
		}
		matchCodes = strings.Join(statusCodes, ",")
	}

	args := []string{
		"-u", baseURL,
		"-w", wordlistFile,
		"-of", "json",
		"-t", fmt.Sprintf("%d", opts.Threads),
		"-timeout", fmt.Sprintf("%d", opts.Timeout),
		"-mc", matchCodes,
	}

	if opts.AutoCalibration {
		args = append(args, "-ac")
	}
	if opts.FollowRedirect {
		args = append(args, "-r")
	}
	if opts.Recursion {
		args = append(args, "-recursion", "-recursion-depth", fmt.Sprintf("%d", opts.RecursionDepth))
	}
	if opts.Rate > 0 {
		args = append(args, "-rate", fmt.Sprintf("%d", opts.Rate))
	}
	for _, ext := range opts.Extensions {
		args = append(args, "-e", strings.TrimPrefix(ext, "."))
	}

	return append(args, "-o", outputPath)
}

func (s *FFufScanner) scanTarget(ctx context.Context, target, wordlistFile string, opts *FFufOptions, logFn func(level, format string, args ...interface{})) ([]*Asset, error) {
	scanCtx, scanCancel := context.WithCancel(ctx)
	defer scanCancel()

	baseURL := strings.TrimSuffix(target, "/") + "/FUZZ"

	// 输出到临时文件
	tmpFile, err := os.CreateTemp("", "ffuf-*.json")
	if err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := buildFFufArgs(baseURL, wordlistFile, tmpPath, opts)

	logFn("INFO", "[FFuf] CLI: target=%s wordlist_configured=%t", target, wordlistFile != "")

	logFn("INFO", "[FFuf] executing ffuf for %s", target)

	// opts.Timeout 是单请求时限；进程总时限由父级阶段 context 和工具默认值共同约束。
	res, err := s.executor.Execute(scanCtx, args, ExecuteOpts{
		LogFn: logFn,
	})
	if err != nil {
		logFn("DEBUG", "[FFuf] execution error target=%s err=%v", target, err)
		s.executor.LogResult("FFuf: "+target, res, err)
		return nil, fmt.Errorf("ffuf execution: %w", err)
	}

	// 读取 JSON 输出，若为空则回退到 stdout
	content, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		return nil, fmt.Errorf("read ffuf output: %w", readErr)
	}

	var parseData []byte
	if len(content) > 0 {
		parseData = content
	} else if len(res.Stdout) > 0 {
		logFn("DEBUG", "[FFuf] output file empty, falling back to stdout (%d bytes)", len(res.Stdout))
		parseData = []byte(res.Stdout)
	} else {
		logFn("DEBUG", "[FFuf] no output from file or stdout")
		return nil, nil
	}

	var wrapper ffufCLIOutput
	if err := json.Unmarshal(parseData, &wrapper); err != nil {
		logFn("DEBUG", "[FFuf] JSON parse error target=%s err=%v raw=%s", target, err, string(parseData))
		return nil, fmt.Errorf("parse ffuf results: %w", err)
	}
	ffufResults := wrapper.Results

	logFn("DEBUG", "[FFuf] %s: parsed %d results", target, len(ffufResults))

	results := s.convertResults(target, ffufResults)
	s.enrichWithHTTPRequests(ctx, results, opts, logFn)
	return results, nil
}

// convertResults 将 ffuf 结果转换为 Asset 列表
func (s *FFufScanner) convertResults(target string, results []FFufCLIResult) []*Asset {
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

		asset := &Asset{
			Authority:     authority,
			Host:          hostname,
			Port:          port,
			Category:      "url",
			Service:       parsedURL.Scheme,
			HttpStatus:    fmt.Sprintf("%d", r.StatusCode),
			IsHTTP:        true,
			Source:        "ffuf",
			Path:          parsedURL.Path,
			ContentLength: r.ContentLength,
			ContentType:   r.ContentType,
			ContentWords:  r.ContentWords,
			ContentLines:  r.ContentLines,
			Duration:      r.DurationNs / int64(time.Millisecond),
		}

		if r.RedirectLocation != "" {
			asset.Title = r.RedirectLocation
		}

		assets = append(assets, asset)
	}

	return assets
}

// enrichWithHTTPRequests 对扫描结果补充HTTP请求和响应原文（供AI研判使用）
func (s *FFufScanner) enrichWithHTTPRequests(ctx context.Context, assets []*Asset, opts *FFufOptions, logFn func(level, format string, args ...interface{})) {
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
					logFn("ERROR", "[FFuf] enrichWithHTTPRequests panic: %v", r)
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
	logFn("INFO", "[FFuf] HTTP请求响应补充完成，共 %d 条", len(assets))
}

// fetchHTTPRequestResponse 获取URL的HTTP请求和响应原文（跟随重定向，最多10次）
func fetchHTTPRequestResponse(ctx context.Context, targetURL string, timeout time.Duration) (reqRaw, respRaw string) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return "", ""
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", ""
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("User-Agent", "cscan-dirscan/1.0")

	var reqBuf bytes.Buffer
	if err := req.Write(&reqBuf); err == nil {
		reqRaw = reqBuf.String()
	}

	resp, err := client.Do(req)
	if err != nil {
		return reqRaw, ""
	}
	defer resp.Body.Close()

	var respBuilder strings.Builder
	respBuilder.WriteString(fmt.Sprintf("HTTP/%d.%d %s\r\n", resp.ProtoMajor, resp.ProtoMinor, resp.Status))
	resp.Header.Write(&respBuilder)
	respBuilder.WriteString("\r\n")

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err == nil && len(body) > 0 {
		if isMostlyPrintable(body) {
			respBuilder.Write(body)
		} else {
			respBuilder.WriteString("[binary content omitted]")
		}
	}

	respRaw = respBuilder.String()
	if len(respRaw) > 100*1024 {
		respRaw = respRaw[:100*1024] + "...(truncated)"
	}

	return reqRaw, respRaw
}

// isMostlyPrintable 判断数据是否主要为可打印字符（用于过滤二进制内容）
func isMostlyPrintable(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	nonPrintable := 0
	for _, b := range data {
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			nonPrintable++
		}
	}
	return nonPrintable*100/len(data) < 10
}

// collectTargets 从 ScanConfig 中提取目标列表
func (s *FFufScanner) collectTargets(config *ScanConfig, logFn func(level, format string, args ...interface{})) []string {
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
					logFn("INFO", "[FFuf] 使用带路径的目标: %s (基础路径: %s)", baseURL, asset.Path)
				}
				targets = append(targets, baseURL)
			} else {
				logFn("DEBUG", "[FFuf] 跳过非HTTP资产: %s:%d", asset.Host, asset.Port)
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

// writeWordlistFile 将路径列表写入临时文件
func (s *FFufScanner) writeWordlistFile(paths []string) (string, error) {
	tmpFile, err := os.CreateTemp("", "ffuf-wordlist-*.txt")
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
