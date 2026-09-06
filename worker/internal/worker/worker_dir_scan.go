package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

func buildDirScanOptions(paths []string, threads, timeout, rate int, config *scheduler.DirScanConfig) *scanner.FFufOptions {
	return &scanner.FFufOptions{
		Paths:           paths,
		Threads:         threads,
		Timeout:         timeout,
		Extensions:      config.Extensions,
		StatusCodes:     config.StatusCodes,
		FollowRedirect:  config.FollowRedirect,
		AutoCalibration: config.AutoCalibration,
		FilterSize:      config.FilterSize,
		FilterWords:     config.FilterWords,
		FilterLines:     config.FilterLines,
		FilterRegex:     config.FilterRegex,
		MatcherMode:     config.MatcherMode,
		FilterMode:      config.FilterMode,
		Rate:            rate,
		Recursion:       config.Recursion,
		RecursionDepth:  config.RecursionDepth,
	}
}

func (w *Worker) executeDirScan(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.DirScanConfig, orgId string) []*scanner.Asset {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "Directory scan panic recovered: %v, stack: %s", r, string(getStackTrace()))
			// panic 时返回 nil，让任务继续执行后续阶段
		}
	}()

	// 过滤出HTTP资产（必须同时满足 IsHTTP=true 且端口是常见HTTP端口或已确认为HTTP服务）
	var httpAssets []*scanner.Asset
	for _, asset := range assets {
		// 只有明确标记为HTTP服务的资产才进行目录扫描
		// 避免对非HTTP服务（如SSH、MySQL等）进行无效扫描
		if asset.IsHTTP && scanner.IsHTTPService(asset.Service, asset.Port) {
			httpAssets = append(httpAssets, asset)
		} else if asset.IsHTTP {
			// IsHTTP=true 但端口不是常见HTTP端口，记录跳过原因
			w.taskLog(task.TaskId, LevelDebug, "Dir scan: skipping %s:%d (port not recognized as HTTP service)", asset.Host, asset.Port)
		}
	}

	if len(httpAssets) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "Dir scan: skipped (no HTTP assets)")
		return nil
	}

	w.taskLog(task.TaskId, LevelInfo, "Dir scan: %d HTTP assets, %d dicts", len(httpAssets), len(config.DictIds))

	// 获取字典内容
	if len(config.DictIds) == 0 {
		w.taskLog(task.TaskId, LevelWarn, "Dir scan: no dicts configured")
		return nil
	}

	dictResp, err := w.loadDirScanDicts(ctx, config.DictIds)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Dir scan: get dicts failed: %v", err)
		return nil
	}

	if len(dictResp.Dicts) == 0 {
		w.taskLog(task.TaskId, LevelWarn, "Dir scan: no dicts found")
		return nil
	}

	// 合并所有字典的路径
	var allPaths []string
	pathSet := make(map[string]bool)
	for _, dict := range dictResp.Dicts {
		for _, path := range dict.Paths {
			if !pathSet[path] {
				pathSet[path] = true
				allPaths = append(allPaths, path)
			}
		}
		w.taskLog(task.TaskId, LevelInfo, "Dir scan: loaded dict '%s' with %d paths", dict.Name, len(dict.Paths))
	}

	if len(allPaths) == 0 {
		w.taskLog(task.TaskId, LevelWarn, "Dir scan: no paths in dicts")
		return nil
	}

	w.taskLog(task.TaskId, LevelInfo, "Dir scan: total %d unique paths", len(allPaths))

	// 根据配置选择扫描工具（默认 ffuf）
	dirscanTool := config.Tool
	if dirscanTool == "" {
		dirscanTool = "ffuf"
	}

	scannerKey := "ffuf"
	if dirscanTool == "feroxbuster" {
		scannerKey = "feroxbuster"
	}

	dirScanner, ok := w.scanners[scannerKey]
	if !ok {
		w.taskLog(task.TaskId, LevelError, "Dir scan: %s scanner not found", scannerKey)
		return nil
	}
	w.taskLog(task.TaskId, LevelInfo, "Dir scan: using %s scanner", scannerKey)

	// 构建扫描选项：前端已隐藏threads/timeout/rate配置，这里使用合理默认值，并用Worker并发数做上限保护
	threads := config.Threads
	if threads <= 0 {
		threads = 50 // 默认50并发
	}
	if w.config.Concurrency > 0 && threads > w.config.Concurrency {
		threads = w.config.Concurrency // 不超过Worker并发上限，避免压垮单Worker
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10 // 默认单请求超时10秒
	}
	rate := config.Rate
	// rate=0 表示不限制速率，由ffuf内部按threads全速跑

	opts := buildDirScanOptions(allPaths, threads, timeout, rate, config)

	// 按单目标超时计算总超时：单目标超时 × 资产数 × 路径数 / 线程数
	totalTimeout := timeout * len(httpAssets) * len(allPaths) / threads
	if totalTimeout < 60 {
		totalTimeout = 60
	}
	w.taskLog(task.TaskId, LevelInfo, "Dir scan: total timeout=%ds (single=%ds, assets=%d, paths=%d, threads=%d)",
		totalTimeout, timeout, len(httpAssets), len(allPaths), threads)
	dirCtx, dirCancel := context.WithTimeout(ctx, time.Duration(totalTimeout)*time.Second)
	defer dirCancel()

	// 创建任务日志回调
	taskLogger := func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, format, args...)
	}

	result, err := dirScanner.Scan(dirCtx, &scanner.ScanConfig{
		Assets:     httpAssets,
		Options:    opts,
		MainTaskId: task.MainTaskId,
		TaskLogger: taskLogger,
		OnProgress: w.makeOnProgress(task, "目录扫描"),
		OnTargetDone: func(target string, assets []*scanner.Asset) {
			// 流式入库：每完成一个目标立即保存
			w.saveDirScanResults(ctx, task, assets)
		},
	})

	// 检查是否超时
	if dirCtx.Err() == context.DeadlineExceeded {
		w.taskLog(task.TaskId, LevelWarn, "Dir scan timeout after %ds, using partial results", totalTimeout)
	}

	// 检查是否被停止
	if ctx.Err() != nil || w.checkTaskControl(ctx, task) == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task stopped")
		return nil
	}

	if err != nil {
		// 目录扫描错误不应导致任务退出，只记录警告并继续
		w.taskLog(task.TaskId, LevelWarn, "Dir scan error (continuing): %v", err)
		// 结果已通过 OnTargetDone 流式保存，仅返回已有结果
		if result != nil && len(result.Assets) > 0 {
			return result.Assets
		}
		return nil
	}

	if result != nil && len(result.Assets) > 0 {
		w.taskLog(task.TaskId, LevelInfo, "Dir scan completed: found %d paths", len(result.Assets))
		return result.Assets
	}

	return nil
}

// saveDirScanResults 保存目录扫描结果到数据库
func (w *Worker) saveDirScanResults(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset) {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "Save directory scan results panic recovered: %v, stack: %s", r, string(getStackTrace()))
		}
	}()

	if len(assets) == 0 {
		w.taskLog(task.TaskId, LevelDebug, "Dir scan: no assets to save")
		return
	}

	w.taskLog(task.TaskId, LevelInfo, "Dir scan: saving %d results to database", len(assets))

	// 转换为目录扫描结果文档
	var results []DirScanResultDocument
	for _, asset := range assets {
		// 构建完整URL
		scheme := "http"
		if asset.Port == 443 || strings.HasPrefix(asset.Service, "https") {
			scheme = "https"
		}
		var fullURL string
		if (scheme == "http" && asset.Port == 80) || (scheme == "https" && asset.Port == 443) {
			fullURL = fmt.Sprintf("%s://%s%s", scheme, asset.Host, asset.Path)
		} else {
			fullURL = fmt.Sprintf("%s://%s:%d%s", scheme, asset.Host, asset.Port, asset.Path)
		}

		results = append(results, DirScanResultDocument{
			Authority:     asset.Authority,
			Host:          asset.Host,
			Port:          asset.Port,
			URL:           fullURL,
			Path:          asset.Path,
			StatusCode:    parseStatusCode(asset.HttpStatus),
			ContentLength: asset.ContentLength,
			ContentType:   asset.ContentType,
			Title:         asset.Title,
			ContentWords:  asset.ContentWords,
			ContentLines:  asset.ContentLines,
			Duration:      asset.Duration,
			Request:       asset.RequestRaw,
			Response:      asset.ResponseRaw,
		})
	}

	// 直连 MongoDB 保存结果（与 JSFinder 保持一致，避免 HTTP 接口不存在导致 404）
	if err := w.saveDirScanResultsWithFallback(ctx, task.MainTaskId, results); err != nil {
		w.taskLog(task.TaskId, LevelError, "Dir scan result persistence failed: %v", err)
	}
}

// executeJSFinder 执行 JSFinder 扫描阶段（JS 敏感信息 + 未授权检测）
