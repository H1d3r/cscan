package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/praetorian-inc/fingerprintx/pkg/plugins"
	"github.com/praetorian-inc/fingerprintx/pkg/scan"
	"github.com/zeromicro/go-zero/core/logx"
)

// FingerprintxScanner fingerprintx 扫描器
// 使用 fingerprintx SDK 进行端口服务识别
type FingerprintxScanner struct {
	BaseScanner
}

// NewFingerprintxScanner 创建 fingerprintx 扫描器
func NewFingerprintxScanner() *FingerprintxScanner {
	return &FingerprintxScanner{
		BaseScanner: BaseScanner{name: "fingerprintx"},
	}
}

// FingerprintxOptions fingerprintx 扫描选项
type FingerprintxOptions struct {
	Timeout     int `json:"timeout"`     // 单个目标超时时间(秒)，默认10秒
	Concurrency int `json:"concurrency"` // 并发数，默认10
	UDP         bool `json:"udp"`         // 是否扫描UDP端口
	FastMode    bool `json:"fastMode"`    // 快速模式，减少探测深度
}

// Validate 验证 FingerprintxOptions 配置是否有效
func (o *FingerprintxOptions) Validate() error {
	if o.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative, got %d", o.Timeout)
	}
	if o.Concurrency < 0 {
		return fmt.Errorf("concurrency must be non-negative, got %d", o.Concurrency)
	}
	return nil
}

// Scan 执行 fingerprintx 扫描
func (s *FingerprintxScanner) Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error) {
	// 解析配置
	opts := &FingerprintxOptions{
		Timeout:     10,
		Concurrency: 10,
		UDP:         false,
		FastMode:    false,
	}

	if config.Options != nil {
		switch v := config.Options.(type) {
		case *FingerprintxOptions:
			opts = v
		default:
			if data, err := json.Marshal(config.Options); err == nil {
				json.Unmarshal(data, opts)
			}
		}
	}

	// 验证配置
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	// 日志辅助函数
	taskLog := func(level, format string, args ...interface{}) {
		if config.TaskLogger != nil {
			config.TaskLogger(level, format, args...)
		}
	}

	// 如果没有资产，返回空结果
	if len(config.Assets) == 0 {
		logx.Info("No assets to scan with fingerprintx")
		return &ScanResult{
			WorkspaceId: config.WorkspaceId,
			MainTaskId:  config.MainTaskId,
			Assets:      []*Asset{},
		}, nil
	}

	logx.Infof("Fingerprintx: scanning %d assets, timeout=%ds, concurrency=%d", 
		len(config.Assets), opts.Timeout, opts.Concurrency)
	taskLog("INFO", "Fingerprintx: scanning %d assets, timeout=%ds, concurrency=%d", 
		len(config.Assets), opts.Timeout, opts.Concurrency)

	// 执行扫描
	identifiedAssets := s.runFingerprintx(ctx, config.Assets, opts, taskLog, config.OnProgress)

	return &ScanResult{
		WorkspaceId: config.WorkspaceId,
		MainTaskId:  config.MainTaskId,
		Assets:      identifiedAssets,
	}, nil
}

// runFingerprintx 运行 fingerprintx 扫描
func (s *FingerprintxScanner) runFingerprintx(
	ctx context.Context,
	assets []*Asset,
	opts *FingerprintxOptions,
	taskLog func(level, format string, args ...interface{}),
	onProgress func(int, string),
) []*Asset {
	var (
		identifiedAssets []*Asset
		mu               sync.Mutex
		wg               sync.WaitGroup
		completed        int32
		totalAssets      = len(assets)
	)

	// 创建任务通道
	taskChan := make(chan *Asset, opts.Concurrency)

	// 启动工作协程
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for asset := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					// 执行单个资产扫描
					result := s.scanSingleAsset(ctx, asset, opts, taskLog)
					
					mu.Lock()
					identifiedAssets = append(identifiedAssets, result)
					completed++
					currentCompleted := int(completed)
					mu.Unlock()

					// 更新进度
					if onProgress != nil {
						progress := currentCompleted * 100 / totalAssets
						onProgress(progress, fmt.Sprintf("Scanned %d/%d assets", currentCompleted, totalAssets))
					}
				}
			}
		}()
	}

	// 分发任务
	for _, asset := range assets {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return identifiedAssets
		case taskChan <- asset:
		}
	}

	close(taskChan)
	wg.Wait()

	logx.Infof("Fingerprintx: completed scanning %d assets", len(identifiedAssets))
	return identifiedAssets
}

// scanSingleAsset 扫描单个资产
func (s *FingerprintxScanner) scanSingleAsset(
	ctx context.Context,
	asset *Asset,
	opts *FingerprintxOptions,
	taskLog func(level, format string, args ...interface{}),
) *Asset {
	// 创建超时上下文
	scanCtx, cancel := context.WithTimeout(ctx, time.Duration(opts.Timeout)*time.Second)
	defer cancel()

	// 解析 IP 地址和端口
	addrPort, err := netip.ParseAddrPort(fmt.Sprintf("%s:%d", asset.Host, asset.Port))
	if err != nil {
		// 如果是域名，尝试解析
		logx.Debugf("Failed to parse address %s:%d, trying as hostname: %v", asset.Host, asset.Port, err)
		// 对于域名，使用 0.0.0.0 作为占位符
		addrPort, _ = netip.ParseAddrPort(fmt.Sprintf("0.0.0.0:%d", asset.Port))
	}

	// 构建目标
	target := plugins.Target{
		Address: addrPort,
		Host:    asset.Host,
	}

	// 创建 fingerprintx 扫描配置
	fxConfig := scan.Config{
		DefaultTimeout: time.Duration(opts.Timeout) * time.Second,
		FastMode:       opts.FastMode,
	}

	// 执行扫描
	var results []plugins.Service
	if opts.UDP {
		results, err = scan.UDPScan([]plugins.Target{target}, fxConfig)
	} else {
		results, err = scan.ScanTargets([]plugins.Target{target}, fxConfig)
	}

	if err != nil {
		// 扫描失败，保留原始资产信息
		if scanCtx.Err() == context.DeadlineExceeded {
			taskLog("WARN", "Fingerprintx: %s:%d timeout", asset.Host, asset.Port)
		} else {
			logx.Debugf("Fingerprintx scan error for %s:%d: %v", asset.Host, asset.Port, err)
		}
		// 设置 IsHTTP 字段
		asset.IsHTTP = IsHTTPService(asset.Service, asset.Port)
		return asset
	}

	// 更新资产信息
	if len(results) > 0 {
		result := results[0]
		
		// 更新服务名称
		if result.Protocol != "" {
			asset.Service = result.Protocol
		}

		// 更新服务版本信息
		if result.Version != "" {
			// 构建产品信息字符串
			productInfo := result.Protocol
			if result.Version != "" {
				productInfo += ":" + result.Version
			}
			
			// 添加到 App 列表（如果不存在）
			found := false
			for _, app := range asset.App {
				if app == productInfo {
					found = true
					break
				}
			}
			if !found {
				asset.App = append(asset.App, productInfo)
			}
		}

		// 更新 Banner 信息
		if len(result.Raw) > 0 {
			// 限制 Banner 长度
			maxBannerLen := 1024
			if len(result.Raw) > maxBannerLen {
				asset.Banner = string(result.Raw[:maxBannerLen]) + "...[truncated]"
			} else {
				asset.Banner = string(result.Raw)
			}
		}

		// 如果有元数据，添加到 Banner
		metadata := result.Metadata()
		if metadata != nil {
			metadataStr := formatMetadata(metadata)
			if metadataStr != "" {
				if asset.Banner != "" {
					asset.Banner += "\n" + metadataStr
				} else {
					asset.Banner = metadataStr
				}
			}
		}

		logx.Debugf("Fingerprintx identified %s:%d: service=%s, version=%s", 
			asset.Host, asset.Port, result.Protocol, result.Version)
	}

	// 设置 IsHTTP 字段
	asset.IsHTTP = IsHTTPService(asset.Service, asset.Port)

	return asset
}

// formatMetadata 格式化元数据
func formatMetadata(metadata plugins.Metadata) string {
	if metadata == nil {
		return ""
	}

	// 尝试 JSON 序列化
	data, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	
	// 如果是空对象，返回空字符串
	if string(data) == "{}" || string(data) == "null" {
		return ""
	}
	
	return string(data)
}

// CheckFingerprintxAvailable 检查 fingerprintx 是否可用
// 由于使用 SDK，总是返回 true
func CheckFingerprintxAvailable() bool {
	return true
}
