package scanner

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/ffuf/ffuf/v2/pkg/ffuf"
	"github.com/ffuf/ffuf/v2/pkg/filter"
	"github.com/ffuf/ffuf/v2/pkg/input"
	"github.com/ffuf/ffuf/v2/pkg/runner"
	"github.com/zeromicro/go-zero/core/logx"
)

// debugRunner 是一个封装了原始 Runner 的组件，用来截获每一个请求结果并打印调试日志
type debugRunner struct {
	runner   ffuf.RunnerProvider
	conf     *ffuf.Config
	logDebug func(string, ...interface{})
}

func (r *debugRunner) Execute(req *ffuf.Request) (ffuf.Response, error) {
	resp, err := r.runner.Execute(req)
	if err == nil {
		inputData := ""
		if req.Input != nil {
			if val, ok := req.Input["FUZZ"]; ok {
				inputData = string(val)
			}
		}

		matched := false
		matchReason := ""
		mm := r.conf.MatcherManager

		// 检查 Matchers (必须匹配才能进入下一步)
		matchers := mm.GetMatchers()
		if len(matchers) == 0 {
			// 如果没有任何 Matcher，默认全部允许
			matched = true
			matchReason = "none(all allowed)"
		} else {
			for name, m := range matchers {
				matchedOk, _ := m.Filter(&resp)
				if matchedOk {
					matched = true
					matchReason = name
					if r.conf.MatcherMode == "or" {
						break
					}
				} else if r.conf.MatcherMode == "and" {
					matched = false
					break
				}
			}
		}

		if !matched {
			r.logDebug("[FFuf Debug] 路径: %s (FUZZ: %s) - 响应码: %d, 大小: %d, 字数: %d, 行数: %d -> [无效] 未匹配任何 Matcher (可能是状态码等不符)",
				req.Url, inputData, resp.StatusCode, resp.ContentLength, resp.ContentWords, resp.ContentLines)
			return resp, err
		}

		// 检查 Filters (如果命中 Filter，则丢弃)
		filtered := false
		filterReason := ""

		filters := mm.GetFilters()
		for name, f := range filters {
			filterOk, _ := f.Filter(&resp)
			if filterOk {
				filtered = true
				filterReason = name
				if r.conf.FilterMode == "or" {
					break
				}
			} else if r.conf.FilterMode == "and" {
				filtered = false
				break
			}
		}

		if filtered {
			r.logDebug("[FFuf Debug] 路径: %s (FUZZ: %s) - 响应码: %d, 大小: %d, 字数: %d, 行数: %d -> [无效] 触发过滤规则: %s",
				req.Url, inputData, resp.StatusCode, resp.ContentLength, resp.ContentWords, resp.ContentLines, filterReason)
		} else {
			r.logDebug("[FFuf Debug] 路径: %s (FUZZ: %s) - 响应码: %d, 大小: %d, 字数: %d, 行数: %d -> [有效] 匹配成功，已放行 (Matcher: %s)",
				req.Url, inputData, resp.StatusCode, resp.ContentLength, resp.ContentWords, resp.ContentLines, matchReason)
		}
	}
	return resp, err
}

func (r *debugRunner) Prepare(input map[string][]byte, req *ffuf.Request) (ffuf.Request, error) {
	return r.runner.Prepare(input, req)
}

func (r *debugRunner) Dump(req *ffuf.Request) ([]byte, error) {
	return r.runner.Dump(req)
}

// FFufScanner 基于 ffuf SDK 的目录扫描器
type FFufScanner struct {
	BaseScanner
}

// NewFFufScanner 创建 ffuf 目录扫描器
func NewFFufScanner() *FFufScanner {
	return &FFufScanner{
		BaseScanner: BaseScanner{name: "ffuf"},
	}
}

// FFufOptions ffuf 扫描选项
type FFufOptions struct {
	Paths           []string `json:"paths"`           // 要扫描的路径列表（字典内容）
	Threads         int      `json:"threads"`         // 并发线程数
	Timeout         int      `json:"timeout"`         // 单个请求超时(秒)
	Extensions      []string `json:"extensions"`      // 文件扩展名
	FollowRedirect  bool     `json:"followRedirect"`  // 是否跟随重定向
	AutoCalibration bool     `json:"autoCalibration"` // 自动校准（anti soft-404）
	StatusCodes     []int    `json:"statusCodes"`     // 有效状态码列表
	FilterSize      string   `json:"filterSize"`      // 按响应大小过滤
	FilterWords     string   `json:"filterWords"`     // 按单词数过滤
	FilterLines     string   `json:"filterLines"`     // 按行数过滤
	FilterRegex     string   `json:"filterRegex"`     // 按正则过滤
	MatcherMode     string   `json:"matcherMode"`     // 匹配模式 and/or
	FilterMode      string   `json:"filterMode"`      // 过滤模式 and/or
	Rate            int      `json:"rate"`            // 每秒请求速率限制
	Recursion       bool     `json:"recursion"`       // 递归扫描
	RecursionDepth  int      `json:"recursionDepth"`  // 递归深度
}

// Validate 验证 FFufOptions 配置
func (o *FFufOptions) Validate() error {
	if o.Threads < 0 {
		return fmt.Errorf("threads must be non-negative, got %d", o.Threads)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	return nil
}

// cscanOutput 自定义 ffuf OutputProvider，收集结果到内存
type cscanOutput struct {
	results []ffuf.Result
	mu      sync.Mutex
	config  *ffuf.Config
}

func newCscanOutput(conf *ffuf.Config) *cscanOutput {
	return &cscanOutput{
		results: make([]ffuf.Result, 0),
		config:  conf,
	}
}

func (o *cscanOutput) Banner()                                 {}
func (o *cscanOutput) Finalize() error                         { return nil }
func (o *cscanOutput) Progress(status ffuf.Progress)           {}
func (o *cscanOutput) Info(infostring string)                  {}
func (o *cscanOutput) Error(errstring string)                  {}
func (o *cscanOutput) Raw(output string)                       {}
func (o *cscanOutput) Warning(warnstring string)               {}
func (o *cscanOutput) PrintResult(res ffuf.Result)             {}
func (o *cscanOutput) SaveFile(filename, format string) error  { return nil }
func (o *cscanOutput) Reset()                                  {}
func (o *cscanOutput) Cycle()                                  {}
func (o *cscanOutput) SetCurrentResults(results []ffuf.Result) { o.results = results }

func (o *cscanOutput) Result(resp ffuf.Response) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// 从 Input 中获取 FUZZ 关键字值
	inputData := ""
	if resp.Request != nil && resp.Request.Input != nil {
		if fuzzVal, ok := resp.Request.Input["FUZZ"]; ok {
			inputData = string(fuzzVal)
		}
	}

	// 获取重定向地址
	redirectLocation := ""
	if resp.Request != nil {
		if locHeaders, ok := resp.Headers["Location"]; ok && len(locHeaders) > 0 {
			redirectLocation = locHeaders[0]
		}
	}

	result := ffuf.Result{
		Input:            resp.Request.Input,
		StatusCode:       resp.StatusCode,
		ContentLength:    resp.ContentLength,
		ContentWords:     resp.ContentWords,
		ContentLines:     resp.ContentLines,
		ContentType:      resp.ContentType,
		RedirectLocation: redirectLocation,
		Duration:         resp.Time,
		Host:             resp.Request.Host,
	}

	// 构建完整 URL
	if resp.Request != nil {
		result.Url = resp.Request.Url
	} else if inputData != "" {
		result.Url = strings.Replace(o.config.Url, "FUZZ", inputData, 1)
	}

	o.results = append(o.results, result)
}

func (o *cscanOutput) GetCurrentResults() []ffuf.Result {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.results
}

// Scan 执行目录扫描
func (s *FFufScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	// 默认配置
	opts := &FFufOptions{
		Threads:         50,
		Timeout:         10,
		FollowRedirect:  false,
		AutoCalibration: true,
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

	onProgress := config.OnProgress

	// 从配置中提取选项
	if config.Options != nil {
		if v, ok := config.Options.(*FFufOptions); ok {
			opts = v
		}
	}

	// 验证路径列表
	if len(opts.Paths) == 0 {
		logWarn("[FFuf] 未提供扫描路径")
		return &ScanResult{
			WorkspaceId: config.WorkspaceId,
			MainTaskId:  config.MainTaskId,
		}, nil
	}

	// 获取目标列表
	targets := s.collectTargets(config, logInfo, logDebug)
	if len(targets) == 0 {
		logWarn("[FFuf] 无有效目标")
		return &ScanResult{
			WorkspaceId: config.WorkspaceId,
			MainTaskId:  config.MainTaskId,
		}, nil
	}

	logInfo("[FFuf] 开始目录扫描，目标数: %d，路径数: %d，自动校准: %v", len(targets), len(opts.Paths), opts.AutoCalibration)

	// 写入字典到临时文件
	wordlistFile, err := s.writeWordlistFile(opts.Paths)
	if err != nil {
		return nil, fmt.Errorf("创建字典临时文件失败: %w", err)
	}
	defer os.Remove(wordlistFile)

	// 逐目标扫描
	var allAssets []*Asset
	for i, target := range targets {
		select {
		case <-ctx.Done():
			return &ScanResult{
				WorkspaceId: config.WorkspaceId,
				MainTaskId:  config.MainTaskId,
				Assets:      allAssets,
			}, ctx.Err()
		default:
		}

		logInfo("[FFuf] 扫描目标 %d/%d: %s", i+1, len(targets), target)

		assets, err := s.scanTarget(ctx, target, wordlistFile, opts, logInfo, logDebug)
		if err != nil {
			logWarn("[FFuf] 扫描目标 %s 失败: %v", target, err)
			continue
		}

		allAssets = append(allAssets, assets...)
		logInfo("[FFuf] 目标 %s 发现 %d 个有效路径", target, len(assets))

		if onProgress != nil {
			progress := (i + 1) * 100 / len(targets)
			onProgress(progress, fmt.Sprintf("已完成 %d/%d 个目标", i+1, len(targets)))
		}
	}

	logInfo("[FFuf] 目录扫描完成，共发现 %d 个有效路径", len(allAssets))

	return &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      allAssets,
	}, nil
}

// scanTarget 对单个目标执行 ffuf 扫描
func (s *FFufScanner) scanTarget(ctx context.Context, target, wordlistFile string, opts *FFufOptions, logInfo, logDebug func(string, ...interface{})) ([]*Asset, error) {
	// 创建子 context 用于控制单个目标的扫描
	scanCtx, scanCancel := context.WithCancel(ctx)
	defer scanCancel()

	// 构建 ffuf Config
	confVal := ffuf.NewConfig(scanCtx, scanCancel)
	conf := &confVal

	// 基本配置
	conf.Url = strings.TrimSuffix(target, "/") + "/FUZZ"
	conf.Method = "GET"
	conf.Threads = opts.Threads
	conf.Timeout = opts.Timeout
	conf.Noninteractive = true
	conf.Quiet = true
	conf.IgnoreWordlistComments = true

	// 跟随重定向
	conf.FollowRedirects = opts.FollowRedirect

	// 速率限制
	if opts.Rate > 0 {
		conf.Rate = int64(opts.Rate)
	}

	// 递归扫描
	conf.Recursion = opts.Recursion
	if opts.RecursionDepth > 0 {
		conf.RecursionDepth = opts.RecursionDepth
	}

	// 自动校准
	conf.AutoCalibration = opts.AutoCalibration
	if opts.AutoCalibration {
		conf.AutoCalibrationPerHost = true
		conf.AutoCalibrationStrategies = []string{"basic", "advanced"}
	}

	// 扩展名
	if len(opts.Extensions) > 0 {
		conf.Extensions = opts.Extensions
	}

	// 输入模式
	conf.InputMode = "clusterbomb"
	conf.InputProviders = []ffuf.InputProviderConfig{
		{
			Name:    "wordlist",
			Keyword: "FUZZ",
			Value:   wordlistFile,
		},
	}

	// Headers
	conf.Headers = map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Connection":      "keep-alive",
	}

	// 创建 MatcherManager 并配置过滤器/匹配器
	mm := filter.NewMatcherManager()

	// 设置匹配模式
	if opts.MatcherMode != "" {
		conf.MatcherMode = opts.MatcherMode
	}
	if opts.FilterMode != "" {
		conf.FilterMode = opts.FilterMode
	}

	// 添加状态码验证规则
	if len(opts.StatusCodes) > 0 {
		var sc []string
		for _, c := range opts.StatusCodes {
			sc = append(sc, strconv.Itoa(c))
		}
		scStr := strings.Join(sc, ",")
		if err := mm.AddMatcher("status", scStr); err != nil {
			logDebug("[FFuf] 添加状态码匹配失败: %v", err)
		}
	} else {
		// 默认 ffuf 状态码
		_ = mm.AddMatcher("status", "200,204,301,302,307,401,403,405,500")
	}

	// 添加用户自定义过滤器
	if opts.FilterSize != "" {
		if err := mm.AddFilter("size", opts.FilterSize, true); err != nil {
			logDebug("[FFuf] 添加大小过滤器失败: %v", err)
		}
	}
	if opts.FilterWords != "" {
		if err := mm.AddFilter("word", opts.FilterWords, true); err != nil {
			logDebug("[FFuf] 添加单词数过滤器失败: %v", err)
		}
	}
	if opts.FilterLines != "" {
		if err := mm.AddFilter("line", opts.FilterLines, true); err != nil {
			logDebug("[FFuf] 添加行数过滤器失败: %v", err)
		}
	}
	if opts.FilterRegex != "" {
		if err := mm.AddFilter("regexp", opts.FilterRegex, true); err != nil {
			logDebug("[FFuf] 添加正则过滤器失败: %v", err)
		}
	}

	conf.MatcherManager = mm

	// 创建 custom OutputProvider
	output := newCscanOutput(conf)

	// 创建 InputProvider
	inputProvider, inputErr := input.NewInputProvider(conf)
	if inputErr.ErrorOrNil() != nil {
		return nil, fmt.Errorf("创建输入提供器失败: %w", inputErr.ErrorOrNil())
	}

	// 创建 Runner
	simpleRunner := runner.NewSimpleRunner(conf, false)
	debuggableRunner := &debugRunner{
		runner:   simpleRunner,
		conf:     conf,
		logDebug: logDebug,
	}

	// 创建 Job
	job := ffuf.NewJob(conf)
	job.Input = inputProvider
	job.Runner = debuggableRunner
	job.Output = output

	// 执行扫描（Start 是阻塞调用，无返回值）
	job.Start()

	// 收集结果
	results := output.GetCurrentResults()
	logInfo("[FFuf] 目标 %s 扫描完成，原始结果: %d 条", target, len(results))

	// 转换为 Asset
	return s.convertResults(target, results), nil
}

// convertResults 将 ffuf 结果转换为 Asset 列表
func (s *FFufScanner) convertResults(target string, results []ffuf.Result) []*Asset {
	assets := make([]*Asset, 0, len(results))

	parsedTarget, err := url.Parse(target)
	if err != nil {
		return assets
	}

	for _, r := range results {
		// 解析结果 URL
		resultURL := r.Url
		if resultURL == "" {
			continue
		}

		parsedURL, err := url.Parse(resultURL)
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
			Duration:      r.Duration.Milliseconds(),
		}

		// 处理重定向
		if r.RedirectLocation != "" {
			asset.Title = r.RedirectLocation
		}

		assets = append(assets, asset)
	}

	return assets
}

// collectTargets 从 ScanConfig 中提取目标列表
func (s *FFufScanner) collectTargets(config *ScanConfig, logInfo, logDebug func(string, ...interface{})) []string {
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
					logInfo("[FFuf] 使用带路径的目标: %s (基础路径: %s)", baseURL, asset.Path)
				}
				targets = append(targets, baseURL)
			} else {
				logDebug("[FFuf] 跳过非HTTP资产: %s:%d (service: %s, isHttp: %v)", asset.Host, asset.Port, asset.Service, asset.IsHTTP)
			}
		}
	} else if len(config.Targets) > 0 {
		targets = config.Targets
	} else if config.Target != "" {
		targets = strings.Split(config.Target, "\n")
	}

	// 规范化
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
		// 去掉开头的 /，ffuf 会自动拼接
		line = strings.TrimPrefix(line, "/")
		if _, err := fmt.Fprintln(tmpFile, line); err != nil {
			os.Remove(tmpFile.Name())
			return "", err
		}
	}

	return tmpFile.Name(), nil
}
