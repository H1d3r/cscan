package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"cscan/internal/scanner"
	"cscan/pkg/geolocation"
	"cscan/pkg/utils"
)

func (w *Worker) initGeolocation() {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Init geolocation panic recovered: %v", r)
		}
	}()

	w.logger.Info("InitGeolocation: starting initialization...")

	// 查找 data 目录（支持相对路径和绝对路径）
	dataDir := "data"

	// 首先检查当前目录下的 data
	if _, err := os.Stat(dataDir); err == nil {
		w.logger.Info("InitGeolocation: found data dir at current directory: %s", dataDir)
	} else {
		// 尝试可执行文件所在目录
		execPath, _ := os.Executable()
		if execPath != "" {
			projectRoot := filepath.Dir(execPath)
			dataDir = filepath.Join(projectRoot, "data")
			w.logger.Info("InitGeolocation: trying exec path: %s", dataDir)
		}
	}

	// 检查数据库文件是否存在
	v4Path := filepath.Join(dataDir, "ip2region_v4.xdb")
	w.logger.Info("InitGeolocation: checking database at: %s", v4Path)

	if _, err := os.Stat(v4Path); os.IsNotExist(err) {
		w.logger.Info("InitGeolocation: ip2region database NOT found at %s, geolocation disabled", v4Path)
		return
	}

	w.logger.Info("InitGeolocation: database file found, initializing service...")

	// 初始化 geolocation 服务
	config := geolocation.Config{
		Enabled:      true,
		DataDir:      dataDir,
		AutoDownload: false,
	}

	if err := geolocation.GetManager().InitWithConfig(config); err != nil {
		w.logger.Error("InitGeolocation: failed to init geolocation service: %v", err)
		return
	}

	w.logger.Info("InitGeolocation: SUCCESS, service initialized with data dir: %s", dataDir)
}

// loadHttpServiceMappings 从 HTTP 接口加载HTTP服务设置（端口配置+服务映射）
func (w *Worker) loadHttpServiceMappings() (httpPorts, httpsPorts, nonHTTPPorts, mappings int) {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Load HTTP service mappings panic recovered: %v, stack: %s", r, string(getStackTrace()))
		}
	}()

	ctx := context.Background()

	// 直连 MongoDB 获取 HTTP 服务设置
	resp, err := w.loadHttpServiceSettings(ctx)
	if err != nil {
		w.logger.Error("GetHttpServiceSettings HTTP failed: %v, using default settings", err)
		return 0, 0, 0, 0
	}

	if !resp.Success {
		w.logger.Error("GetHttpServiceSettings failed: %s, using default settings", resp.Msg)
		return 0, 0, 0, 0
	}

	// 创建检查器
	checker := NewWorkerHttpServiceChecker()

	// 设置端口配置
	if len(resp.Config.HttpPorts) > 0 {
		checker.SetHttpPorts(resp.Config.HttpPorts)
	}
	if len(resp.Config.HttpsPorts) > 0 {
		checker.SetHttpsPorts(resp.Config.HttpsPorts)
	}
	if len(resp.Config.NonHttpPorts) > 0 {
		checker.SetNonHttpPorts(resp.Config.NonHttpPorts)
	}

	// 设置服务映射
	for _, mapping := range resp.Mappings {
		checker.SetMapping(mapping.ServiceName, mapping.IsHttp)
	}

	// 设置全局检查器
	scanner.SetHttpServiceChecker(checker)
	return len(resp.Config.HttpPorts), len(resp.Config.HttpsPorts), len(resp.Config.NonHttpPorts), len(resp.Mappings)
}

// getBlacklistMatcher 获取黑名单匹配器
func (w *Worker) getBlacklistMatcher(ctx context.Context, taskId string) *utils.BlacklistMatcher {
	// 直连 MongoDB 获取黑名单规则
	resp, err := w.loadBlacklistRules(ctx)
	if err != nil {
		w.taskLog(taskId, LevelWarn, "Failed to get blacklist rules: %v", err)
		return nil
	}

	if resp.Code != 0 {
		w.taskLog(taskId, LevelWarn, "Get blacklist rules failed: %s", resp.Msg)
		return nil
	}

	if len(resp.Rules) == 0 {
		return nil
	}

	matcher := utils.NewBlacklistMatcher(resp.Rules)
	w.taskLog(taskId, LevelDebug, "Loaded %d blacklist rules", matcher.RuleCount())
	return matcher
}

// filterAssetsByBlacklist 使用黑名单过滤资产
func (w *Worker) filterAssetsByBlacklist(assets []*scanner.Asset, matcher *utils.BlacklistMatcher, taskId string) []*scanner.Asset {
	if matcher == nil || matcher.IsEmpty() || len(assets) == 0 {
		return assets
	}

	var filtered []*scanner.Asset
	var skippedCount int

	for _, asset := range assets {
		// 检查主机名/域名
		if matcher.IsDomainBlacklisted(asset.Host) {
			skippedCount++
			continue
		}

		// 检查IP地址
		isBlacklisted := false
		for _, ipInfo := range asset.IPV4 {
			if matcher.IsIPBlacklisted(ipInfo.IP) {
				isBlacklisted = true
				break
			}
		}
		if isBlacklisted {
			skippedCount++
			continue
		}

		// 检查Authority（可能包含端口）
		if matcher.IsBlacklisted(asset.Authority) {
			skippedCount++
			continue
		}

		filtered = append(filtered, asset)
	}

	if skippedCount > 0 {
		w.taskLog(taskId, LevelInfo, "Blacklist: filtered %d assets", skippedCount)
	}

	return filtered
}

// filterAssetsByExcludeHosts 使用排除目标过滤资产（检查解析的IP）
// 与黑名单过滤类似，但专门用于端口扫描的排除目标配置
func (w *Worker) filterAssetsByExcludeHosts(assets []*scanner.Asset, matcher *utils.BlacklistMatcher, taskId string) []*scanner.Asset {
	if matcher == nil || matcher.IsEmpty() || len(assets) == 0 {
		return assets
	}

	var filtered []*scanner.Asset
	var skippedHosts []string

	for _, asset := range assets {
		// 检查主机名/域名本身是否在排除列表
		if matcher.IsBlacklisted(asset.Host) {
			skippedHosts = append(skippedHosts, asset.Host)
			continue
		}

		// 检查该资产解析出的所有IPv4地址
		isExcluded := false
		for _, ipInfo := range asset.IPV4 {
			if matcher.IsIPBlacklisted(ipInfo.IP) {
				isExcluded = true
				skippedHosts = append(skippedHosts, fmt.Sprintf("%s(%s)", asset.Host, ipInfo.IP))
				break
			}
		}
		if isExcluded {
			continue
		}

		filtered = append(filtered, asset)
	}

	if len(skippedHosts) > 0 {
		w.taskLog(taskId, LevelDebug, "ExcludeHosts: skipped targets: %v", skippedHosts)
	}

	return filtered
}
