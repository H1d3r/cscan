package scanner

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/projectdiscovery/httpx/runner"
	"github.com/zeromicro/go-zero/core/logx"
)

// HttpxLibScanner 使用httpx库进行HTTP探测
type HttpxLibScanner struct {
	mu sync.Mutex
}

// NewHttpxLibScanner 创建httpx库扫描器
func NewHttpxLibScanner() *HttpxLibScanner {
	return &HttpxLibScanner{}
}

// HttpxLibResult httpx库扫描结果
type HttpxLibResult struct {
	URL          string
	Host         string
	Port         int
	Title        string
	StatusCode   int
	Technologies []string
	FaviconHash  string
	Server       string
	ContentType  string
	Body         string
	Headers      string
	Screenshot   string
	Scheme       string
}

// RunHttpxLib 使用httpx库进行批量HTTP探测
// 注意：此函数使用同步阻塞方式执行，确保所有 httpx 回调完成后才返回
// 内部使用 mutex 保护对共享 asset 对象的并发访问
func (s *FingerprintScanner) RunHttpxLib(ctx context.Context, assets []*Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{})) error {
	if len(assets) == 0 {
		return nil
	}

	// 构建目标列表
	var targets []string
	targetMap := make(map[string]*Asset)
	// 用于快速查找 asset 的指针地址
	assetPtrSet := make(map[*Asset]bool)

	for _, asset := range assets {
		target := fmt.Sprintf("%s:%d", asset.Host, asset.Port)
		targets = append(targets, target)
		targetMap[target] = asset
		// 同时添加带协议的目标，提高匹配率
		targetMap[fmt.Sprintf("http://%s:%d", asset.Host, asset.Port)] = asset
		targetMap[fmt.Sprintf("https://%s:%d", asset.Host, asset.Port)] = asset
		// 记录所有 asset 指针
		assetPtrSet[asset] = true
	}

	// 结果处理锁 - 保护对共享 asset 的所有访问
	var mu sync.Mutex
	// 已处理的资产指针集合 - 用于防止同一资产被多次处理
	// 使用资产指针作为 key，因为多个 key 可能指向同一个 asset
	processedAssets := make(map[*Asset]bool)

	// 配置httpx选项
	httpxOpts := runner.Options{
		Methods:            "GET",
		InputTargetHost:    targets,
		StatusCode:         true,
		ExtractTitle:       true,
		TechDetect:         true,
		Favicon:            true,
		FollowRedirects:    true,
		MaxRedirects:       5,
		Threads:            opts.Concurrency,
		Timeout:            opts.TargetTimeout,
		NoFallback:         false,
		NoFallbackScheme:   false,
		OutputServerHeader: true,
		OutputContentType:  true,
		ResponseInStdout:   true,
		Silent:             true,
		DisableUpdateCheck: true,
		RandomAgent:        true,
		TLSGrab:            false,
		OutputIP:           true,
		LeaveDefaultPorts:  false,
		HostMaxErrors:      30,
		// 设置结果回调
		// 注意：httpx SDK 的 OnResult 回调可能由多个内部协程并发调用
		// 因此所有对共享资源（asset）的访问都必须加锁保护
		OnResult: func(result runner.Result) {
			// 关键修复：先检查错误，再获取锁，保持锁内逻辑最小化
			if result.Err != nil {
				if taskLog != nil {
					taskLog("DEBUG", "httpx error for %s: %v", result.Input, result.Err)
				} else {
					logx.Debugf("httpx error for %s: %v", result.Input, result.Err)
				}
				return
			}

			// 匹配资产
			var asset *Asset
			var key string

			// 尝试从Input匹配
			if result.Input != "" {
				key = result.Input
				asset = targetMap[key]
			}

			// 如果Input匹配失败，尝试从URL解析
			if asset == nil && result.URL != "" {
				if u, err := url.Parse(result.URL); err == nil {
					host := u.Hostname()
					port := u.Port()
					if port == "" {
						if u.Scheme == "https" {
							port = "443"
						} else {
							port = "80"
						}
					}
					key = fmt.Sprintf("%s:%s", host, port)
					asset = targetMap[key]

					// 还是没找到，尝试完整URL匹配
					if asset == nil {
						asset = targetMap[result.URL]
					}
				}
			}

			if asset == nil {
				if taskLog != nil {
					taskLog("DEBUG", "httpx result not matched: input=%s, url=%s", result.Input, result.URL)
				} else {
					logx.Debugf("httpx result not matched: input=%s, url=%s", result.Input, result.URL)
				}
				return
			}

			// ========== 关键修复：线程安全的资产更新逻辑 ==========
			// 修复说明：
			// 1. 将 processedAssets 检查移到锁内，防止竞态条件
			// 2. 使用 asset 指针作为 key，即使不同 URL 指向同一 asset 也能正确去重
			// 3. 在锁内完成所有对 asset 的写入操作，确保原子性

			mu.Lock()

			// 检查该资产是否已被处理过
			// 注意：由于 targetMap 可能将多个 key 指向同一个 asset，
			// 使用指针比较可以正确处理这种情况
			if processedAssets[asset] {
				mu.Unlock()
				return
			}

			// 标记为已处理
			processedAssets[asset] = true

			// 更新资产信息 - 所有写操作都在锁内完成
			asset.Title = result.Title
			asset.HttpStatus = fmt.Sprintf("%d", result.StatusCode)

			// 从URL或Scheme字段获取协议
			if result.Scheme != "" {
				asset.Service = result.Scheme
			} else if u, err := url.Parse(result.URL); err == nil {
				asset.Service = u.Scheme
			}

			// 技术检测结果
			if len(result.Technologies) > 0 {
				for _, tech := range result.Technologies {
					asset.App = append(asset.App, tech+"[httpx]")
					if taskLog != nil {
						taskLog("INFO", "发现应用指纹: %s -> %s (来源: httpx)", key, tech)
					} else {
						logx.Infof("发现应用指纹: %s -> %s (来源: httpx)", key, tech)
					}
				}
			}

			// Favicon hash + data
			if result.FavIconMMH3 != "" {
				asset.IconHash = result.FavIconMMH3
			}
			if len(result.FaviconData) > 0 && isImageData(result.FaviconData) {
				asset.IconData = result.FaviconData
			}

			// Server header
			if result.WebServer != "" {
				asset.Server = result.WebServer
			}

			// Response headers
			if result.RawHeaders != "" {
				asset.HttpHeader = result.RawHeaders
			} else if result.ResponseHeaders != nil {
				var headerBuilder strings.Builder
				// 添加状态行
				headerBuilder.WriteString(fmt.Sprintf("HTTP/1.1 %d\n", result.StatusCode))
				for k, v := range result.ResponseHeaders {
					headerBuilder.WriteString(fmt.Sprintf("%s: %v\n", k, v))
				}
				asset.HttpHeader = headerBuilder.String()
			}

			// Response body
			if result.ResponseBody != "" {
				body := result.ResponseBody
				// 限制body大小
				if len(body) > 50*1024 {
					body = body[:50*1024] + "\n...[truncated]"
				}
				asset.HttpBody = body
			}

			// Screenshot (base64)
			if len(result.ScreenshotBytes) > 0 {
				asset.Screenshot = base64.StdEncoding.EncodeToString(result.ScreenshotBytes)
			}

			mu.Unlock()

			if taskLog != nil {
				taskLog("DEBUG", "httpx lib matched: %s -> title=%s, status=%d, techs=%v", key, result.Title, result.StatusCode, result.Technologies)
			} else {
				logx.Debugf("httpx lib matched: %s -> title=%s, status=%d, techs=%v", key, result.Title, result.StatusCode, result.Technologies)
			}
		},
	}

	// 如果需要截图
	if opts.Screenshot {
		httpxOpts.Screenshot = true
		httpxOpts.UseInstalledChrome = true
	}

	// 创建httpx runner
	httpxRunner, err := runner.New(&httpxOpts)
	if err != nil {
		if taskLog != nil {
			taskLog("ERROR", "Failed to create httpx runner: %v", err)
		} else {
			logx.Errorf("Failed to create httpx runner: %v", err)
		}
		return err
	}

	scanDone := make(chan struct{})
	go func() {
		select {
		case <-scanDone:
			httpxRunner.Close()
		case <-ctx.Done():
			if taskLog != nil {
				taskLog("ERROR", "httpx scan canceled or timed out, forcing close")
			} else {
				logx.Errorf("httpx scan canceled or timed out, forcing close")
			}
			httpxRunner.Close()
		}
	}()

	// 运行扫描
	logx.Infof("Running httpx library scan for %d targets", len(targets))
	httpxRunner.RunEnumeration()

	close(scanDone)

	return nil
}

// isImageData 检查字节数据是否为有效的图片格式（通过魔数判断）
func isImageData(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return true
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	// GIF: 47 49 46 38
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return true
	}
	// ICO: 00 00 01 00 or 00 00 02 00
	if data[0] == 0x00 && data[1] == 0x00 && (data[2] == 0x01 || data[2] == 0x02) && data[3] == 0x00 {
		return true
	}
	// BMP: 42 4D
	if data[0] == 0x42 && data[1] == 0x4D {
		return true
	}
	// WebP: RIFF....WEBP
	if len(data) >= 12 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
		return true
	}
	// SVG: 以 '<svg' 或 '<?xml' 开头（文本格式）
	if data[0] == '<' {
		header := strings.ToLower(string(data[:min(len(data), 100)]))
		if strings.HasPrefix(header, "<svg") || (strings.HasPrefix(header, "<?xml") && strings.Contains(header, "<svg")) {
			return true
		}
	}
	return false
}

// runHttpxLib 使用httpx库进行批量探测（替代原有的runHttpx命令行方式）
func (s *FingerprintScanner) runHttpxLib(ctx context.Context, assets []*Asset, opts *FingerprintOptions, taskLog func(level, format string, args ...interface{})) {
	if err := s.RunHttpxLib(ctx, assets, opts, taskLog); err != nil {
		if taskLog != nil {
			taskLog("ERROR", "httpx library scan failed: %v", err)
		} else {
			logx.Errorf("httpx library scan failed: %v", err)
		}
	}
}
