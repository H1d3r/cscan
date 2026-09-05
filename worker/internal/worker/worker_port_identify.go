package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

func (w *Worker) executePortIdentify(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.PortIdentifyConfig, orgId string) []*scanner.Asset {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "Port identify panic recovered: %v, stack: %s", r, string(getStackTrace()))
			// panic 时返回原始资产，确保任务能继续执行
			for _, asset := range assets {
				asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
			}
		}
	}()

	// 确定使用的工具
	tool := config.Tool
	if tool == "" {
		w.taskLog(task.TaskId, LevelInfo, "Port identify: tool not specified, using default 'nmap'")
		tool = "nmap" // 默认使用 nmap
	}

	w.taskLog(task.TaskId, LevelInfo, "Port identify: using tool '%s' (%d assets)", tool, len(assets))

	// 根据工具选择不同的执行逻辑
	if tool == "fingerprintx" {
		w.taskLog(task.TaskId, LevelInfo, "Port identify: executing with Fingerprintx")
		return w.executePortIdentifyWithFingerprintx(ctx, task, assets, config, orgId)
	} else {
		w.taskLog(task.TaskId, LevelInfo, "Port identify: executing with Nmap")
		return w.executePortIdentifyWithNmap(ctx, task, assets, config, orgId)
	}
}

// executePortIdentifyWithNmap 使用 Nmap 执行端口识别。
func (w *Worker) executePortIdentifyWithNmap(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.PortIdentifyConfig, orgId string) []*scanner.Asset {
	return w.executePortIdentifyWithNmapResult(ctx, task, assets, config, orgId).Assets
}

// executePortIdentifyWithNmapResult exposes the merged phase conclusion for
// the pipeline and deterministic scanner-to-worker integration tests.
func (w *Worker) executePortIdentifyWithNmapResult(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.PortIdentifyConfig, orgId string) portIdentifyMergeResult {
	// 获取超时配置
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 // 默认30秒/主机
	}

	// 按主机分组（去重，防止同一端口被 Nmap 重复扫描）
	hostPorts := make(map[string][]int)
	hostAssets := make(map[string][]*scanner.Asset)
	seenPortPerHost := make(map[string]map[int]bool)
	for _, asset := range assets {
		if seenPortPerHost[asset.Host] == nil {
			seenPortPerHost[asset.Host] = make(map[int]bool)
		}
		if !seenPortPerHost[asset.Host][asset.Port] {
			seenPortPerHost[asset.Host][asset.Port] = true
			hostPorts[asset.Host] = append(hostPorts[asset.Host], asset.Port)
		}
		hostAssets[asset.Host] = append(hostAssets[asset.Host], asset)
	}

	portCount := 0
	maxPortsPerHost := 0
	for _, ports := range hostPorts {
		portCount += len(ports)
		if len(ports) > maxPortsPerHost {
			maxPortsPerHost = len(ports)
		}
	}
	configuredConcurrency := config.Concurrency
	if configuredConcurrency <= 0 {
		configuredConcurrency = 1
	}
	nmapConcurrency := configuredConcurrency
	if nmapConcurrency > 5 {
		nmapConcurrency = 5
	}
	if nmapConcurrency > maxPortsPerHost {
		nmapConcurrency = maxPortsPerHost
	}
	w.taskLog(task.TaskId, LevelInfo, "端口识别开始：Nmap，主机 %d 个，端口 %d 个，单命令超时 %d 秒，实际并发 %d",
		len(hostPorts), portCount, timeout, nmapConcurrency)

	// 编排层不设置阶段总超时，只继承任务取消；每条 Nmap 命令由 scanner
	// 按用户配置的 timeout 独立控制。
	identifyCtx, identifyCancel := context.WithCancel(ctx)
	defer identifyCancel()

	var identifyResults []scanner.PortIdentifyResult
	nmapScanner := w.scanners["nmap"]

	for host, ports := range hostPorts {
		if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "任务已停止")
			canceled := portIdentifyResultsForAssets(assets, scanner.PortCanceled, scanner.NmapReasonCanceled)
			return mergeNmapPortIdentifyResults(assets, append(identifyResults, canceled...), config.ExcludeClosed)
		}

		// naabu/masscan 未识别到端口时跳过 nmap，避免产生大量 "Nmap: no ports to scan" 噪音日志
		if len(ports) == 0 {
			w.taskLog(task.TaskId, LevelDebug, "Port identify(nmap): skipping %s, no ports to scan", host)
			mergedHost := mergeNmapPortIdentifyResults(hostAssets[host], nil, config.ExcludeClosed)
			w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, mergedHost.Assets)
			continue
		}

		// 构建端口字符串
		portStrs := make([]string, len(ports))
		for i, p := range ports {
			portStrs[i] = fmt.Sprintf("%d", p)
		}
		portsStr := strings.Join(portStrs, ",")

		// 构建 Nmap 选项
		nmapOpts := &scanner.NmapOptions{
			Ports:      portsStr,
			Timeout:    timeout,
			Concurrent: nmapConcurrency,
		}
		if config.Args != "" {
			nmapOpts.Args = config.Args
		}

		nmapResult, err := nmapScanner.Scan(identifyCtx, &scanner.ScanConfig{
			Target:      host,
			Options:     nmapOpts,
			EventLogger: w.scannerEventLogger(task.TaskId),
			TaskLogger: func(level, format string, args ...interface{}) {
				w.taskLog(task.TaskId, level, format, args...)
			},
		})

		// 检查是否被停止
		if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "任务已停止")
			canceled := portIdentifyResultsForAssets(assets, scanner.PortCanceled, scanner.NmapReasonCanceled)
			return mergeNmapPortIdentifyResults(assets, append(identifyResults, canceled...), config.ExcludeClosed)
		}

		if err != nil {
			w.taskLog(task.TaskId, LevelError, "Nmap error %s: %v", host, err)
			failed := portIdentifyResultsForAssets(hostAssets[host], scanner.PortExecError, scanner.NmapReasonNonzeroExit)
			identifyResults = append(identifyResults, failed...)
			mergedHost := mergeNmapPortIdentifyResults(hostAssets[host], failed, config.ExcludeClosed)
			w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, mergedHost.Assets)
			continue
		}

		var hostResults []scanner.PortIdentifyResult
		if nmapResult != nil {
			hostResults = nmapResult.PortIdentifyResults
		}
		for _, identify := range hostResults {
			if identify.Outcome == scanner.PortTimeout {
				w.taskLog(task.TaskId, LevelWarn, "端口识别超时：%s:%d，不再进入指纹、截图、证书与漏洞扫描", identify.Host, identify.Port)
			}
		}
		identifyResults = append(identifyResults, hostResults...)
		mergedHost := mergeNmapPortIdentifyResults(hostAssets[host], hostResults, config.ExcludeClosed)
		// 流式入库：保存以发现资产为基线的合并结果，而不是仅保存 OPEN 投影。
		w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, mergedHost.Assets)
	}

	merged := mergeNmapPortIdentifyResults(assets, identifyResults, config.ExcludeClosed)
	w.taskLog(task.TaskId, LevelInfo, "端口识别完成：资产 %d，可继续扫描 %d，成功 %d，超时 %d，失败 %d，未确认 %d，状态 %s",
		len(merged.Assets), len(merged.EligibleAssets), merged.Phase.Coverage.Succeeded, merged.Phase.Coverage.TimedOut,
		merged.Phase.Coverage.Failed, merged.Phase.Coverage.Unconfirmed, merged.Phase.Status)
	return merged
}

// executePortIdentifyWithFingerprintx 使用 Fingerprintx 执行端口识别
func (w *Worker) executePortIdentifyWithFingerprintx(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.PortIdentifyConfig, orgId string) []*scanner.Asset {
	// 获取配置
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10 // fingerprintx 默认10秒/目标
	}

	concurrency := config.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	// 按单目标超时计算总超时：单目标超时 × 目标数 / 并发数
	totalTimeout := timeout * len(assets) / concurrency
	if totalTimeout < 60 {
		totalTimeout = 60
	}
	w.taskLog(task.TaskId, LevelInfo, "Port identify(fingerprintx): timeout=%ds (single=%ds, assets=%d, concurrency=%d)",
		totalTimeout, timeout, len(assets), concurrency)

	// 同样做防超时继承处理，保证独立阶段时间充足
	fingerCtx, fingerCancel := context.WithTimeout(context.Background(), time.Duration(totalTimeout)*time.Second)
	defer fingerCancel()

	w.safeGoTask(task.TaskId, "fingerprintx-cancel-watch", func() {
		select {
		case <-fingerCtx.Done():
		case <-ctx.Done():
			fingerCancel()
		}
	})

	// 构建 fingerprintx 选项
	fpxOpts := &scanner.FingerprintxOptions{
		Timeout:     timeout,
		Concurrency: concurrency,
		UDP:         config.UDP,
		FastMode:    config.FastMode,
	}

	// 创建扫描配置
	scanConfig := &scanner.ScanConfig{
		Assets:     assets,
		Options:    fpxOpts,
		MainTaskId: task.TaskId,
		TaskLogger: func(level, format string, args ...interface{}) {
			w.taskLog(task.TaskId, level, format, args...)
		},
		OnProgress: func(progress int, message string) {
			// 可以在这里更新任务进度
			w.taskLog(task.TaskId, LevelDebug, "Progress: %d%% - %s", progress, message)
		},
	}

	// 执行扫描
	fpxScanner := w.scanners["fingerprintx"]
	result, err := fpxScanner.Scan(ctx, scanConfig)

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Fingerprintx error: %v", err)
		// 失败时返回原始资产
		for _, asset := range assets {
			asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
		}
		return assets
	}

	w.taskLog(task.TaskId, LevelInfo, "Port identify completed: %d assets", len(result.Assets))
	return result.Assets
}

// executeBruteScan 执行弱口令扫描阶段
