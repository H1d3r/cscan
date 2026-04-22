package logic

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"go.mongodb.org/mongo-driver/bson"
)

// ActiveFingerprintValidateLogic 验证主动指纹
type ActiveFingerprintValidateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewActiveFingerprintValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActiveFingerprintValidateLogic {
	return &ActiveFingerprintValidateLogic{ctx: ctx, svcCtx: svcCtx}
}

// ActiveFingerprintValidate 验证主动指纹
func (l *ActiveFingerprintValidateLogic) ActiveFingerprintValidate(req *types.ActiveFingerprintValidateReq) (*types.ActiveFingerprintValidateResp, error) {
	if req.Id == "" {
		return &types.ActiveFingerprintValidateResp{Code: 400, Msg: "主动指纹ID不能为空"}, nil
	}
	if req.Url == "" {
		return &types.ActiveFingerprintValidateResp{Code: 400, Msg: "目标URL不能为空"}, nil
	}

	// 获取主动指纹
	activeFp, err := l.svcCtx.ActiveFingerprintModel.FindById(l.ctx, req.Id)
	if err != nil {
		return &types.ActiveFingerprintValidateResp{Code: 404, Msg: "主动指纹不存在"}, nil
	}

	// 查找同名的被动指纹（用于匹配规则）
	filter := bson.M{"name": activeFp.Name}
	passiveFingerprints, err := l.svcCtx.FingerprintModel.Find(l.ctx, filter, 1, 100)
	if err != nil || len(passiveFingerprints) == 0 {
		return &types.ActiveFingerprintValidateResp{
			Code: 400,
			Msg:  fmt.Sprintf("未找到同名的被动指纹 '%s'，无法进行验证", activeFp.Name),
		}, nil
	}

	// 解析URL获取基础部分（scheme://host:port）
	baseUrl, scheme := extractBaseUrlWithScheme(req.Url)
	if baseUrl == "" {
		return &types.ActiveFingerprintValidateResp{Code: 400, Msg: "无效的URL格式"}, nil
	}

	var results []types.ActiveFingerprintValidateItem
	anyMatched := false

	// 遍历每个探测路径
	for _, path := range activeFp.Paths {
		result := types.ActiveFingerprintValidateItem{
			Path:    path,
			Matched: false,
		}

		// 尝试请求，如果HTTPS失败则尝试HTTP
		resp, body, finalUrl, err := smartHttpRequest(baseUrl, path, scheme)
		if err != nil {
			result.MatchedDetails = err.Error()
			results = append(results, result)
			continue
		}

		result.StatusCode = resp.StatusCode

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

		// 构建指纹数据
		data := &FingerprintData{
			Title:        title,
			Body:         body,
			BodyBytes:    []byte(body),
			Headers:      resp.Header,
			HeaderString: headerStr.String(),
			Server:       resp.Header.Get("Server"),
			URL:          finalUrl,
			Cookies:      resp.Header.Get("Set-Cookie"),
		}

		// 使用被动指纹规则进行匹配
		for _, fp := range passiveFingerprints {
			engine := NewSingleFingerprintEngine(&fp)
			matched, conditions := engine.MatchWithDetails(data)
			if matched {
				result.Matched = true
				result.MatchedRule = fp.Name
				result.MatchedDetails = strings.Join(conditions, "\n")
				anyMatched = true
				break
			}
		}

		if !result.Matched {
			result.MatchedDetails = "未匹配任何规则"
		}

		results = append(results, result)
	}

	return &types.ActiveFingerprintValidateResp{
		Code:    0,
		Msg:     "验证完成",
		Matched: anyMatched,
		Results: results,
	}, nil
}

// smartHttpRequest 智能HTTP请求，自动处理协议切换
func smartHttpRequest(baseUrl, path, originalScheme string) (*http.Response, string, string, error) {
	client := createValidateHttpClient()

	// 构建URL列表，按优先级尝试
	var urls []string
	fullUrl := baseUrl + path

	switch originalScheme {
	case "https":
		// 用户指定HTTPS，先尝试HTTPS，失败后尝试HTTP
		urls = append(urls, fullUrl)
		httpUrl := strings.Replace(fullUrl, "https://", "http://", 1)
		urls = append(urls, httpUrl)
	case "http":
		// 用户指定HTTP，只尝试HTTP
		urls = append(urls, fullUrl)
	default:
		// 未指定协议，先HTTP后HTTPS
		urls = append(urls, fullUrl)
		if strings.HasPrefix(fullUrl, "http://") {
			httpsUrl := strings.Replace(fullUrl, "http://", "https://", 1)
			urls = append(urls, httpsUrl)
		}
	}

	var lastErr error
	for _, url := range urls {
		resp, body, err := doValidateRequest(client, url)
		if err == nil {
			return resp, body, url, nil
		}
		lastErr = err
		// 如果是连接被拒绝或超时，继续尝试下一个URL
		if isRetryableError(err) {
			continue
		}
		// 其他错误也继续尝试
	}

	if lastErr != nil {
		return nil, "", "", fmt.Errorf("请求失败: %v", simplifyError(lastErr))
	}
	return nil, "", "", fmt.Errorf("请求失败: 无法连接到目标")
}

// isRetryableError 判断是否是可重试的错误
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// EOF、连接重置、连接拒绝、超时等都可以重试
	return strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "tls:") ||
		strings.Contains(errStr, "certificate")
}

// simplifyError 简化错误信息
func simplifyError(err error) string {
	if err == nil {
		return ""
	}
	errStr := err.Error()
	if strings.Contains(errStr, "EOF") {
		return "连接被服务器关闭(EOF)"
	}
	if strings.Contains(errStr, "connection refused") {
		return "连接被拒绝"
	}
	if strings.Contains(errStr, "connection reset") {
		return "连接被重置"
	}
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "Timeout") {
		return "连接超时"
	}
	if strings.Contains(errStr, "no such host") {
		return "域名解析失败"
	}
	if strings.Contains(errStr, "certificate") {
		return "证书验证失败"
	}
	if strings.Contains(errStr, "network is unreachable") {
		return "网络不可达"
	}
	// 截取关键信息
	if len(errStr) > 100 {
		return errStr[:100] + "..."
	}
	return errStr
}

// createValidateHttpClient 创建HTTP客户端
func createValidateHttpClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   8 * time.Second,
		KeepAlive: 0,
	}

	transport := &http.Transport{
		DialContext: dialer.DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS10,
		},
		DisableKeepAlives:     true,
		DisableCompression:    false,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       10 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     false,
	}

	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// doValidateRequest 发起HTTP请求
func doValidateRequest(client *http.Client, url string) (*http.Response, string, error) {
	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}

	// 设置请求头
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	httpReq.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	httpReq.Header.Set("Connection", "close")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	// 读取响应体（限制大小）
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return resp, "", err
	}

	return resp, string(bodyBytes), nil
}

// extractBaseUrlWithScheme 从URL中提取基础部分和协议
func extractBaseUrlWithScheme(rawUrl string) (string, string) {
	rawUrl = strings.TrimSpace(rawUrl)
	if rawUrl == "" {
		return "", ""
	}

	var scheme string

	// 查找 ://
	schemeEnd := strings.Index(rawUrl, "://")
	if schemeEnd == -1 {
		// 没有scheme，添加默认的http://
		rawUrl = "http://" + rawUrl
		scheme = "http"
		schemeEnd = 4
	} else {
		scheme = rawUrl[:schemeEnd]
	}

	// 从scheme之后查找第一个/
	rest := rawUrl[schemeEnd+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		// 没有路径，直接返回
		return rawUrl, scheme
	}

	// 返回 scheme://host:port 部分
	return rawUrl[:schemeEnd+3+slashIdx], scheme
}

// extractBaseUrl 从URL中提取基础部分（保留兼容）
func extractBaseUrl(rawUrl string) string {
	base, _ := extractBaseUrlWithScheme(rawUrl)
	return base
}
