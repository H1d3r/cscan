package worker

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"cscan/internal/model"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

// ==================== 指纹验证任务处理 ====================

// FingerprintValidationResult 指纹验证结果
type FingerprintValidationResult struct {
	Matched      bool            `json:"matched"`
	Details      string          `json:"details"`
	MatchedList  []string        `json:"matchedList,omitempty"`
	MatchedInfos []MatchedFpInfo `json:"matchedInfos,omitempty"` // 批量验证匹配详情
	TotalScanned int             `json:"totalScanned,omitempty"` // 批量验证扫描总数
	StatusCode   int             `json:"statusCode,omitempty"`
	Error        string          `json:"error,omitempty"`
	Path         string          `json:"path,omitempty"`        // 主动指纹探测路径
	MatchedRule  string          `json:"matchedRule,omitempty"` // 主动指纹匹配的规则名
	PathResults  []PathResult    `json:"pathResults,omitempty"` // 主动指纹各路径结果
}

// MatchedFpInfo 批量验证中匹配的指纹信息
type MatchedFpInfo struct {
	Id                string `json:"id"`
	Name              string `json:"name"`
	IsBuiltin         bool   `json:"isBuiltin"`
	IsActive          bool   `json:"isActive"`
	MatchedConditions string `json:"matchedConditions"`
}

// PathResult 主动指纹单个路径的验证结果
type PathResult struct {
	Path           string `json:"path"`
	StatusCode     int    `json:"statusCode"`
	Matched        bool   `json:"matched"`
	MatchedRule    string `json:"matchedRule,omitempty"`
	MatchedDetails string `json:"matchedDetails,omitempty"`
	Error          string `json:"error,omitempty"`
}

// executeFingerprintValidateTask 执行被动指纹验证任务
func (w *Worker) executeFingerprintValidateTask(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, startTime time.Time) {
	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusStarted, "正在验证指纹...")

	url, _ := taskConfig["url"].(string)
	fpId, _ := taskConfig["fingerprintId"].(string)

	if url == "" || fpId == "" {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "URL或指纹ID为空"})
		return
	}

	// 1. 获取目标数据（HTTP请求 + 指纹数据）
	data, err := w.fetchFingerprintDataForValidate(url)
	if err != nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "请求目标失败: " + err.Error()})
		return
	}

	// 2. 按ID直查指纹（避免全量加载指纹库），文档自带全部结构化字段
	if w.mongoDB == nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "mongo direct connection unavailable"})
		return
	}
	fp, err := model.NewFingerprintModel(w.mongoDB).FindById(ctx, fpId)
	if err != nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "查询指纹失败: " + err.Error()})
		return
	}
	if fp == nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "指纹不存在"})
		return
	}

	// 3. 匹配
	var result FingerprintValidationResult

	// 使用 wappalyzer 库检测（内置/wappalyzer来源，按应用名比对）
	if fp.Source == "wappalyzer" || fp.IsBuiltin {
		wappalyzerClient := w.getWappalyzerClient()
		if wappalyzerClient != nil {
			apps := wappalyzerClient.Fingerprint(data.Headers, data.BodyBytes)
			fpNameLower := strings.ToLower(fp.Name)
			for app := range apps {
				if strings.ToLower(app) == fpNameLower {
					result = FingerprintValidationResult{
						Matched: true,
						Details: fmt.Sprintf("wappalyzergo 库检测匹配: %s", fp.Name),
					}
					break
				}
			}
		}
	}

	// 如果wappalyzer没匹配，使用自定义引擎（Rule + ARL/Wappalyzer结构化字段全量匹配，
	// 与API侧批量验证能力对齐）
	if !result.Matched {
		engine := scanner.NewCustomFingerprintEngine([]*model.Fingerprint{fp})
		matchedFps := engine.MatchWithId(data)
		matched := len(matchedFps) > 0
		details := "未匹配"
		if matched {
			var matchedNames []string
			for _, m := range matchedFps {
				matchedNames = append(matchedNames, m.Name)
			}
			details = fmt.Sprintf("自定义引擎匹配: %s", strings.Join(matchedNames, ", "))
		}
		result = FingerprintValidationResult{
			Matched: matched,
			Details: details,
		}
	}

	duration := time.Since(startTime).Seconds()
	w.saveFingerprintValidationResult(ctx, task.TaskId, fmt.Sprintf("验证完成, 耗时%.2fs", duration), result)
}

// executeActiveFingerprintValidateTask 执行主动指纹验证任务
func (w *Worker) executeActiveFingerprintValidateTask(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, startTime time.Time) {
	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusStarted, "正在验证主动指纹...")

	url, _ := taskConfig["url"].(string)
	activeFpId, _ := taskConfig["activeFpId"].(string)

	if url == "" || activeFpId == "" {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "URL或主动指纹ID为空"})
		return
	}

	// 1. 按ID直查主动指纹配置
	if w.mongoDB == nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "mongo direct connection unavailable"})
		return
	}
	activeFp, err := model.NewActiveFingerprintModel(w.mongoDB).FindById(ctx, activeFpId)
	if err != nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "查询主动指纹失败: " + err.Error()})
		return
	}
	if activeFp == nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "主动指纹不存在"})
		return
	}

	// 2. 获取同名启用的被动指纹（用于匹配规则，与扫描时config_loader的关联语义一致）
	passiveFps, err := model.NewFingerprintModel(w.mongoDB).FindByNames(ctx, []string{activeFp.Name})
	if err != nil {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "获取被动指纹列表失败: " + err.Error()})
		return
	}
	if len(passiveFps) == 0 {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{
			Error: fmt.Sprintf("未找到同名被动指纹 '%s'", activeFp.Name),
		})
		return
	}

	// 3. 解析基础URL
	baseUrl, scheme := extractBaseUrlWithSchemeForWorker(url)
	if baseUrl == "" {
		w.saveFingerprintValidationResult(ctx, task.TaskId, "", FingerprintValidationResult{Error: "无效的URL格式"})
		return
	}

	// 4. 遍历每个探测路径
	anyMatched := false
	var pathResults []PathResult
	client := w.createValidateHttpClientForWorker()

	for _, path := range activeFp.Paths {
		pr := PathResult{Path: path}

		resp, body, finalUrl, err := w.smartHttpRequestForWorker(client, baseUrl, path, scheme)
		if err != nil {
			pr.Error = err.Error()
			pathResults = append(pathResults, pr)
			continue
		}

		pr.StatusCode = resp.StatusCode

		// 提取标题
		title := ""
		titleRe := regexp.MustCompile(`(?i)<title[^>]*>([^<]*)</title>`)
		if matches := titleRe.FindStringSubmatch(body); len(matches) > 1 {
			title = strings.TrimSpace(matches[1])
		}

		// 构建header字符串
		var headerStr strings.Builder
		for key, values := range resp.Header {
			for _, v := range values {
				headerStr.WriteString(key)
				headerStr.WriteString(": ")
				headerStr.WriteString(v)
				headerStr.WriteString("\n")
			}
		}

		data := &scanner.FingerprintData{
			Title:        title,
			Body:         body,
			BodyBytes:    []byte(body),
			Headers:      resp.Header,
			HeaderString: headerStr.String(),
			Server:       resp.Header.Get("Server"),
			URL:          finalUrl,
			Cookies:      resp.Header.Get("Set-Cookie"),
		}

		// 使用被动指纹规则匹配
		for _, fp := range passiveFps {
			engine := scanner.NewCustomFingerprintEngine([]*model.Fingerprint{fp})
			matchedFps := engine.MatchWithId(data)
			if len(matchedFps) > 0 {
				pr.Matched = true
				pr.MatchedRule = fp.Name
				pr.MatchedDetails = fmt.Sprintf("自定义引擎匹配: %s", fp.Name)
				anyMatched = true
				break
			}
		}

		if !pr.Matched {
			pr.MatchedDetails = "未匹配任何规则"
		}
		pathResults = append(pathResults, pr)
	}

	duration := time.Since(startTime).Seconds()

	// 构建匹配详情（用于API层填充MatchedConditions）
	details := ""
	if anyMatched {
		var matchedPaths []string
		for _, pr := range pathResults {
			if pr.Matched {
				matchedPaths = append(matchedPaths, fmt.Sprintf("路径[%s]匹配规则: %s", pr.Path, pr.MatchedRule))
			}
		}
		details = strings.Join(matchedPaths, "\n")
	}

	result := FingerprintValidationResult{
		Matched:     anyMatched,
		Details:     details,
		PathResults: pathResults,
	}

	w.saveFingerprintValidationResult(ctx, task.TaskId, fmt.Sprintf("主动指纹验证完成, 耗时%.2fs", duration), result)
}

// saveFingerprintValidationResult 保存指纹验证结果（终态更新，包含worker字段，不应再调用updateTaskStatus覆盖）
func (w *Worker) saveFingerprintValidationResult(ctx context.Context, taskId, msg string, result FingerprintValidationResult) {
	resultData := map[string]interface{}{
		"taskId":     taskId,
		"status":     "SUCCESS",
		"result":     result,
		"updateTime": time.Now().Local().Format("2006-01-02 15:04:05"),
	}
	if result.Error != "" {
		resultData["status"] = "FAILURE"
		resultData["error"] = result.Error
	}

	resultJson, err := json.Marshal(resultData)
	if err != nil {
		w.taskLog(taskId, LevelError, "Failed to marshal fingerprint validation result: %v", err)
		return
	}

	status := scheduler.TaskStatusSuccess
	if result.Error != "" {
		status = scheduler.TaskStatusFailure
	}
	// 终态更新：包含 state、worker、result（JSON），不再由后续 updateTaskStatus 覆盖
	_, err = w.httpClient.UpdateTask(ctx, &TaskUpdateReq{
		TaskId: taskId,
		State:  status,
		Worker: w.config.Name,
		Result: string(resultJson),
	})
	if err != nil {
		w.taskLog(taskId, LevelError, "Failed to save fingerprint validation result: %v", err)
	}
}

// fetchFingerprintDataForValidate 从目标URL获取指纹数据
func (w *Worker) fetchFingerprintDataForValidate(targetUrl string) (*scanner.FingerprintData, error) {
	targetUrl = extractBaseUrlForWorker(targetUrl)

	w.logger.Info("[Fingerprint] HTTP GET %s", targetUrl)

	client := &http.Client{
		Timeout: 15 * time.Second,
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

	start := time.Now()
	resp, err := client.Get(targetUrl)
	if err != nil {
		w.logger.Warn("[Fingerprint] HTTP GET %s failed: %v", targetUrl, err)
		return nil, err
	}
	defer resp.Body.Close()
	w.logger.Info("[Fingerprint] HTTP %s -> %d %s (%dms)", targetUrl, resp.StatusCode, resp.Status, time.Since(start).Milliseconds())

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	body := string(bodyBytes)

	title := ""
	titleRe := regexp.MustCompile(`(?i)<title[^>]*>([^<]*)</title>`)
	if matches := titleRe.FindStringSubmatch(body); len(matches) > 1 {
		title = strings.TrimSpace(matches[1])
	}

	var headerStr strings.Builder
	for key, values := range resp.Header {
		for _, v := range values {
			headerStr.WriteString(key)
			headerStr.WriteString(": ")
			headerStr.WriteString(v)
			headerStr.WriteString("\n")
		}
	}

	faviconHash := w.fetchFaviconHashForWorker(targetUrl, body, client)

	return &scanner.FingerprintData{
		Title:        title,
		Body:         body,
		BodyBytes:    bodyBytes,
		Headers:      resp.Header,
		HeaderString: headerStr.String(),
		Server:       resp.Header.Get("Server"),
		URL:          targetUrl,
		FaviconHash:  faviconHash,
		Cookies:      resp.Header.Get("Set-Cookie"),
	}, nil
}

// fetchFaviconHashForWorker 获取favicon并计算MMH3 hash
func (w *Worker) fetchFaviconHashForWorker(baseUrl, body string, client *http.Client) string {
	faviconUrl := ""
	linkRe := regexp.MustCompile(`(?i)<link[^>]*rel=["'](?:shortcut )?icon["'][^>]*href=["']([^"']+)["']`)
	if matches := linkRe.FindStringSubmatch(body); len(matches) > 1 {
		faviconUrl = matches[1]
	}
	if faviconUrl == "" {
		linkRe2 := regexp.MustCompile(`(?i)<link[^>]*href=["']([^"']+)["'][^>]*rel=["'](?:shortcut )?icon["']`)
		if matches := linkRe2.FindStringSubmatch(body); len(matches) > 1 {
			faviconUrl = matches[1]
		}
	}
	if faviconUrl == "" {
		faviconUrl = "/favicon.ico"
	}
	if !strings.HasPrefix(faviconUrl, "http") {
		if strings.HasPrefix(faviconUrl, "//") {
			faviconUrl = "https:" + faviconUrl
		} else if strings.HasPrefix(faviconUrl, "/") {
			u := extractBaseUrlForWorker(baseUrl)
			if u != "" {
				faviconUrl = strings.TrimRight(u, "/") + faviconUrl
			}
		} else {
			u := extractBaseUrlForWorker(baseUrl)
			if u != "" {
				faviconUrl = strings.TrimRight(u, "/") + "/" + faviconUrl
			}
		}
	}

	resp, err := client.Get(faviconUrl)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ""
	}

	iconBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil || len(iconBytes) == 0 {
		return ""
	}

	encoded := base64.StdEncoding.EncodeToString(iconBytes)
	hash := mmh3Hash32ForWorker([]byte(encoded))
	return fmt.Sprintf("%d", int32(hash))
}

// mmh3Hash32ForWorker MurmurHash3 32位实现（与API端一致）
func mmh3Hash32ForWorker(data []byte) uint32 {
	const (
		c1 = 0xcc9e2d51
		c2 = 0x1b873593
		r1 = 15
		r2 = 13
		m  = 5
		n  = 0xe6546b64
	)
	length := len(data)
	h1 := uint32(0)
	pos := 0
	for pos+4 <= length {
		k1 := uint32(data[pos]) | uint32(data[pos+1])<<8 | uint32(data[pos+2])<<16 | uint32(data[pos+3])<<24
		pos += 4
		k1 *= c1
		k1 = (k1 << r1) | (k1 >> (32 - r1))
		k1 *= c2
		h1 ^= k1
		h1 = (h1 << r2) | (h1 >> (32 - r2))
		h1 = h1*m + n
	}
	var tail uint32
	switch length - pos {
	case 3:
		tail ^= uint32(data[pos+2]) << 16
		fallthrough
	case 2:
		tail ^= uint32(data[pos+1]) << 8
		fallthrough
	case 1:
		tail ^= uint32(data[pos])
		tail *= c1
		tail = (tail << r1) | (tail >> (32 - r1))
		tail *= c2
		h1 ^= tail
	}
	h1 ^= uint32(length)
	h1 ^= h1 >> 16
	h1 *= 0x85ebca6b
	h1 ^= h1 >> 13
	h1 *= 0xc2b2ae35
	h1 ^= h1 >> 16
	return h1
}

// createValidateHttpClientForWorker 创建HTTP客户端
func (w *Worker) createValidateHttpClientForWorker() *http.Client {
	dialer := &net.Dialer{
		Timeout:   8 * time.Second,
		KeepAlive: 0,
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10},
		DisableKeepAlives:   true,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     10 * time.Second,
		TLSHandshakeTimeout: 8 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// smartHttpRequestForWorker 智能HTTP请求，自动处理协议切换
func (w *Worker) smartHttpRequestForWorker(client *http.Client, baseUrl, path, originalScheme string) (*http.Response, string, string, error) {
	fullUrl := baseUrl + path
	var urls []string

	switch originalScheme {
	case "https":
		urls = append(urls, fullUrl)
		urls = append(urls, strings.Replace(fullUrl, "https://", "http://", 1))
	case "http":
		urls = append(urls, fullUrl)
	default:
		urls = append(urls, fullUrl)
		if strings.HasPrefix(fullUrl, "http://") {
			urls = append(urls, strings.Replace(fullUrl, "http://", "https://", 1))
		}
	}

	var lastErr error
	for _, url := range urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Connection", "close")

		resp, err := client.Do(req)
		if err == nil {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
			resp.Body.Close()
			return resp, string(bodyBytes), url, nil
		}
		lastErr = err
	}
	return nil, "", "", fmt.Errorf("请求失败: %v", lastErr)
}

// extractBaseUrlForWorker 从URL提取基础部分
func extractBaseUrlForWorker(rawUrl string) string {
	rawUrl = strings.TrimSpace(rawUrl)
	if rawUrl == "" {
		return ""
	}
	if !strings.Contains(rawUrl, "://") {
		rawUrl = "http://" + rawUrl
	}
	schemeEnd := strings.Index(rawUrl, "://")
	rest := rawUrl[schemeEnd+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		return rawUrl
	}
	return rawUrl[:schemeEnd+3+slashIdx]
}

// extractBaseUrlWithSchemeForWorker 从URL提取基础部分和协议
func extractBaseUrlWithSchemeForWorker(rawUrl string) (string, string) {
	rawUrl = strings.TrimSpace(rawUrl)
	if rawUrl == "" {
		return "", ""
	}
	var scheme string
	schemeEnd := strings.Index(rawUrl, "://")
	if schemeEnd == -1 {
		rawUrl = "http://" + rawUrl
		scheme = "http"
		schemeEnd = 4
	} else {
		scheme = rawUrl[:schemeEnd]
	}
	rest := rawUrl[schemeEnd+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		return rawUrl, scheme
	}
	return rawUrl[:schemeEnd+3+slashIdx], scheme
}

// WorkerHttpServiceChecker Worker端的HTTP服务检查器实现
type WorkerHttpServiceChecker struct {
	serviceCache map[string]bool // serviceName -> isHttp
	httpPorts    map[int]bool    // HTTP端口
	httpsPorts   map[int]bool    // HTTPS端口
	nonHttpPorts map[int]bool    // 非HTTP端口（明确排除）
	mu           sync.RWMutex
}

// NewWorkerHttpServiceChecker 创建HTTP服务检查器
func NewWorkerHttpServiceChecker() *WorkerHttpServiceChecker {
	return &WorkerHttpServiceChecker{
		serviceCache: make(map[string]bool),
		httpPorts:    make(map[int]bool),
		httpsPorts:   make(map[int]bool),
		nonHttpPorts: make(map[int]bool),
	}
}

// IsHttpService 判断服务名称是否为HTTP服务
func (c *WorkerHttpServiceChecker) IsHttpService(serviceName string) (isHttp bool, found bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	isHttp, found = c.serviceCache[serviceName]
	return
}

// IsHttpPort 判断端口是否为HTTP端口
func (c *WorkerHttpServiceChecker) IsHttpPort(port int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpPorts[port] || c.httpsPorts[port]
}

// IsNonHttpPort 判断端口是否为非HTTP端口（明确排除）
func (c *WorkerHttpServiceChecker) IsNonHttpPort(port int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nonHttpPorts[port]
}

// CheckIsHttp 综合判断是否为HTTP服务（服务名称+端口）
func (c *WorkerHttpServiceChecker) CheckIsHttp(serviceName string, port int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 1. 先检查是否在非HTTP端口列表中（明确排除）
	if c.nonHttpPorts[port] {
		return false
	}

	// 2. 检查服务名称映射
	if isHttp, found := c.serviceCache[serviceName]; found {
		return isHttp
	}

	// 3. 检查HTTP/HTTPS端口
	return c.httpPorts[port] || c.httpsPorts[port]
}

// SetMapping 设置服务映射
func (c *WorkerHttpServiceChecker) SetMapping(serviceName string, isHttp bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serviceCache[serviceName] = isHttp
}

// SetHttpPorts 设置HTTP端口列表
func (c *WorkerHttpServiceChecker) SetHttpPorts(ports []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpPorts = make(map[int]bool)
	for _, port := range ports {
		c.httpPorts[port] = true
	}
}

// SetHttpsPorts 设置HTTPS端口列表
func (c *WorkerHttpServiceChecker) SetHttpsPorts(ports []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpsPorts = make(map[int]bool)
	for _, port := range ports {
		c.httpsPorts[port] = true
	}
}

// SetNonHttpPorts 设置非HTTP端口列表
func (c *WorkerHttpServiceChecker) SetNonHttpPorts(ports []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nonHttpPorts = make(map[int]bool)
	for _, port := range ports {
		c.nonHttpPorts[port] = true
	}
}

// executePortIdentify 执行端口识别阶段（Nmap/Fingerprintx 服务识别）
