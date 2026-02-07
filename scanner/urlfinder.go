package scanner

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// URLFinderScanner 目录扫描器（基于字典的URL发现）
type URLFinderScanner struct {
	BaseScanner
}

// NewURLFinderScanner 创建目录扫描器
func NewURLFinderScanner() *URLFinderScanner {
	return &URLFinderScanner{
		BaseScanner: BaseScanner{name: "urlfinder"},
	}
}

// URLFinderOptions 目录扫描选项
type URLFinderOptions struct {
	Paths          []string          `json:"paths"`          // 要扫描的路径列表
	Threads        int               `json:"threads"`        // 并发线程数
	Timeout        int               `json:"timeout"`        // 单个请求超时(秒)
	StatusCodes    []int             `json:"statusCodes"`    // 有效状态码列表
	Extensions     []string          `json:"extensions"`     // 文件扩展名
	FollowRedirect bool              `json:"followRedirect"` // 是否跟随重定向
	UserAgent      string            `json:"userAgent"`      // User-Agent
	Headers        map[string]string `json:"headers"`        // 自定义请求头
}

// Validate 验证 URLFinderOptions 配置是否有效
// 实现 ScannerOptions 接口
func (o *URLFinderOptions) Validate() error {
	if o.Threads < 0 {
		return fmt.Errorf("threads must be non-negative, got %d", o.Threads)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	return nil
}

// URLFinderResult 目录扫描结果
type URLFinderResult struct {
	URL           string `json:"url"`
	StatusCode    int    `json:"statusCode"`
	ContentLength int64  `json:"contentLength"`
	ContentType   string `json:"contentType"`
	Title         string `json:"title"`
	RedirectURL   string `json:"redirectUrl,omitempty"`
}

// Scan 执行目录扫描
func (s *URLFinderScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	// 默认配置
	opts := &URLFinderOptions{
		Threads:        50,
		Timeout:        10,
		StatusCodes:    []int{200, 201, 204, 301, 302, 307, 308, 401, 403, 405, 500},
		FollowRedirect: false,
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}

	// 日志函数
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
	logDebug := func(format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger("DEBUG", format, args...)
		}
		logx.Infof(format, args...)
	}

	// 进度回调
	onProgress := config.OnProgress

	// 从配置中提取选项
	if config.Options != nil {
		if v, ok := config.Options.(*URLFinderOptions); ok {
			opts = v
		}
	}

	// 验证路径列表
	if len(opts.Paths) == 0 {
		logWarn("[URLFinder] 未提供扫描路径")
		return &ScanResult{
			WorkspaceId: config.WorkspaceId,
			MainTaskId:  config.MainTaskId,
		}, nil
	}

	// 获取目标列表
	var targets []string
	if config.Assets != nil && len(config.Assets) > 0 {
		for _, asset := range config.Assets {
			// 同时检查 IsHTTP 字段和端口是否为常见HTTP端口
			if asset.IsHTTP && IsHTTPService(asset.Service, asset.Port) {
				scheme := "http"
				if asset.Port == 443 || strings.HasPrefix(asset.Service, "https") {
					scheme = "https"
				}
				// 构建基础URL
				var baseURL string
				if (scheme == "http" && asset.Port == 80) || (scheme == "https" && asset.Port == 443) {
					baseURL = fmt.Sprintf("%s://%s", scheme, asset.Host)
				} else {
					baseURL = fmt.Sprintf("%s://%s:%d", scheme, asset.Host, asset.Port)
				}
				// 如果资产有 Path 字段，将其作为基础路径前缀
				// 例如：用户输入 http://example.com/admin/，目录扫描应该扫描 /admin/login、/admin/config 等
				if asset.Path != "" && asset.Path != "/" {
					path := strings.TrimSuffix(asset.Path, "/")
					baseURL = baseURL + path
					logInfo("[URLFinder] 使用带路径的目标: %s (基础路径: %s)", baseURL, asset.Path)
				}
				targets = append(targets, baseURL)
			} else {
				logDebug("[URLFinder] 跳过非HTTP资产: %s:%d (service: %s, isHttp: %v)", asset.Host, asset.Port, asset.Service, asset.IsHTTP)
			}
		}
	} else if len(config.Targets) > 0 {
		targets = config.Targets
	} else if config.Target != "" {
		targets = strings.Split(config.Target, "\n")
	}

	if len(targets) == 0 {
		logWarn("[URLFinder] 无有效目标")
		return &ScanResult{
			WorkspaceId: config.WorkspaceId,
			MainTaskId:  config.MainTaskId,
		}, nil
	}

	logInfo("[URLFinder] 开始目录扫描，目标数: %d，路径数: %d", len(targets), len(opts.Paths))

	// 输出目标列表（调试用）
	for i, t := range targets {
		logInfo("[URLFinder] 目标 %d: %s", i+1, t)
	}

	// 创建HTTP客户端
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     30 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(opts.Timeout) * time.Second,
	}

	if !opts.FollowRedirect {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	// 构建有效状态码集合
	validStatusCodes := make(map[int]bool)
	for _, code := range opts.StatusCodes {
		validStatusCodes[code] = true
	}

	// 结果收集
	var results []URLFinderResult
	var resultsMu sync.Mutex

	// 任务队列
	type scanTask struct {
		baseURL string
		path    string
	}
	taskChan := make(chan scanTask, opts.Threads*2)

	// 启动工作协程
	var wg sync.WaitGroup
	for i := 0; i < opts.Threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				result := s.scanPath(client, task.baseURL, task.path, opts, validStatusCodes, logDebug)
				if result != nil {
					resultsMu.Lock()
					results = append(results, *result)
					resultsMu.Unlock()
					logInfo("[URLFinder] 发现: %s [%d]", result.URL, result.StatusCode)
				}
			}
		}()
	}

	// 分发任务
	totalTasks := len(targets) * len(opts.Paths)
	completedTasks := 0
	lastProgress := 0

	for _, target := range targets {
		baseURL := normalizeURL(target)
		for _, path := range opts.Paths {
			select {
			case <-ctx.Done():
				close(taskChan)
				wg.Wait()
				return &ScanResult{
					WorkspaceId: config.WorkspaceId,
					MainTaskId:  config.MainTaskId,
				}, ctx.Err()
			case taskChan <- scanTask{baseURL: baseURL, path: path}:
				completedTasks++
				progress := completedTasks * 100 / totalTasks
				if progress > lastProgress && onProgress != nil {
					onProgress(progress, fmt.Sprintf("扫描进度: %d/%d", completedTasks, totalTasks))
					lastProgress = progress
				}
			}
		}
	}

	close(taskChan)
	wg.Wait()

	logInfo("[URLFinder] 目录扫描完成，发现 %d 个有效路径", len(results))

	// 转换为资产结果
	var assets []*Asset
	for _, r := range results {
		parsedURL, err := url.Parse(r.URL)
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

		asset := &Asset{
			Authority:  parsedURL.Host,
			Host:       parsedURL.Hostname(),
			Port:       port,
			Category:   "url",
			Service:    parsedURL.Scheme,
			Title:      r.Title,
			HttpStatus: fmt.Sprintf("%d", r.StatusCode),
			IsHTTP:     true,
			Source:     "urlfinder",
			// 存储发现的路径
			Path:          parsedURL.Path,
			ContentLength: r.ContentLength,
			ContentType:   r.ContentType,
		}
		assets = append(assets, asset)
	}

	return &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      assets,
	}, nil
}

// scanPath 扫描单个路径
func (s *URLFinderScanner) scanPath(client *http.Client, baseURL, path string, opts *URLFinderOptions, validStatusCodes map[int]bool, logDebug func(string, ...interface{})) *URLFinderResult {
	// 构建完整URL
	fullURL := baseURL

	// 确保 baseURL 和 path 之间有 /
	if path == "" || path == "/" {
		// 根路径
		if !strings.HasSuffix(fullURL, "/") {
			fullURL += "/"
		}
	} else {
		// 非根路径，确保有分隔符
		if !strings.HasSuffix(baseURL, "/") {
			fullURL += "/"
		}
		// 去掉 path 开头的 /，避免重复
		fullURL += strings.TrimPrefix(path, "/")
	}

	// 创建请求
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		logDebug("[URLFinder] 创建请求失败: %s, err: %v", fullURL, err)
		return nil
	}

	// 设置请求头
	req.Header.Set("User-Agent", opts.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		logDebug("[URLFinder] 请求失败: %s, err: %v", fullURL, err)
		return nil
	}
	defer resp.Body.Close()

	// 输出调试信息
	logDebug("[URLFinder] %s -> %d", fullURL, resp.StatusCode)

	// 检查状态码
	if !validStatusCodes[resp.StatusCode] {
		return nil
	}

	// 读取部分响应体获取标题
	title := ""
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		title = extractTitleFromHTML(string(body))
	}

	result := &URLFinderResult{
		URL:           fullURL,
		StatusCode:    resp.StatusCode,
		ContentLength: resp.ContentLength,
		ContentType:   contentType,
		Title:         title,
	}

	// 获取重定向URL
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		result.RedirectURL = resp.Header.Get("Location")
	}

	return result
}

// normalizeURL 规范化URL
func normalizeURL(target string) string {
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "http://" + target
	}
	return strings.TrimSuffix(target, "/")
}

// extractTitleFromHTML 从HTML中提取标题（urlfinder专用）
func extractTitleFromHTML(html string) string {
	html = strings.ToLower(html)
	start := strings.Index(html, "<title>")
	if start == -1 {
		return ""
	}
	start += 7
	end := strings.Index(html[start:], "</title>")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(html[start : start+end])
}

// ParseDictContent 解析字典内容，返回路径列表
func ParseDictContent(content string) []string {
	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}
