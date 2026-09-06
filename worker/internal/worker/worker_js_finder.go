package worker

import (
	"context"
	"fmt"
	"time"

	"cscan/internal/model"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

func (w *Worker) executeJSFinder(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.JSFinderConfig, orgId string) []*scanner.JSFinderResult {
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "JSFinder panic recovered: %v, stack: %s", r, string(getStackTrace()))
		}
	}()

	var httpAssets []*scanner.Asset
	for _, asset := range assets {
		if asset.IsHTTP || scanner.IsHTTPService(asset.Service, asset.Port) {
			httpAssets = append(httpAssets, asset)
		}
	}

	if len(httpAssets) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "JSFinder: skipped (no HTTP assets)")
		return nil
	}

	// 读取 4 份清单（高危路由 / 鉴权关键词 / 敏感数据关键词 / 域名黑名单）。
	// 直连 MongoDB 读取（与结果直写同架构），不走 HTTP：HTTP 路径受 install key
	// 认证约束，dev 模式密钥未配置或密钥轮换后未同步都会导致 401 中断任务。
	// 模型层在配置文档不存在时返回内置默认值；读取失败时同样兜底默认值，不阻断任务。
	cfg := model.NewDefaultJSFinderConfig()
	if w.mongoDB == nil {
		w.taskLog(task.TaskId, LevelWarn, "JSFinder: mongo direct connection unavailable, using built-in config defaults")
	} else if loaded, err := model.NewJSFinderConfigModel(w.mongoDB).Get(ctx); err != nil {
		w.taskLog(task.TaskId, LevelWarn, "JSFinder: load config from MongoDB failed: %v, using built-in defaults", err)
	} else {
		cfg = loaded
	}

	threads := config.Threads
	if threads <= 0 {
		threads = 10
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10
	}

	// nil 表示用户未显式设置，默认视为 true；&false 表示用户明确关闭
	enableSourcemap := config.EnableSourcemap == nil || *config.EnableSourcemap
	enableUnauthCheck := config.EnableUnauthCheck == nil || *config.EnableUnauthCheck

	w.taskLog(task.TaskId, LevelInfo, "JSFinder: %d HTTP assets, threads=%d, timeout=%ds, sourcemap=%v, unauth=%v",
		len(httpAssets), threads, timeout, enableSourcemap, enableUnauthCheck)

	jsScanner, ok := w.scanners["jsfinder"]
	if !ok {
		w.taskLog(task.TaskId, LevelError, "JSFinder: scanner not found")
		return nil
	}

	opts := &scanner.JSFinderOptions{
		HighRiskRoutes:       cfg.HighRiskRoutes,
		AuthRequiredKeywords: cfg.AuthRequiredKeywords,
		SensitiveKeywords:    cfg.SensitiveKeywords,
		DomainBlacklist:      cfg.DomainBlacklist,
		Threads:              threads,
		Timeout:              timeout,
		EnableSourcemap:      enableSourcemap,
		EnableUnauthCheck:    enableUnauthCheck,
		OnResultFound: func(results []*scanner.JSFinderResult) {
			// 流式入库：单目标完成后立即保存
			var schedResults []*JSFinderResultItem
			for _, r := range results {
				schedResults = append(schedResults, &JSFinderResultItem{
					Authority:        r.Authority,
					Host:             r.Host,
					Port:             r.Port,
					URL:              r.URL,
					Severity:         r.Severity,
					VulName:          r.VulName,
					Result:           r.Result,
					Tags:             r.Tags,
					MatcherName:      r.MatcherName,
					ExtractedResults: r.ExtractedResults,
					CurlCommand:      r.CurlCommand,
					Request:          r.Request,
					Response:         r.Response,
				})
			}
			w.saveJSFinderResultWithFallback(ctx, task.MainTaskId, schedResults)
		},
	}

	jsTaskLogger := func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, format, args...)
	}

	// 阶段总超时 = 单目标超时 × 资产数 ÷ 线程数（与端口识别/目录扫描一致），
	// 确保 JS 扫描阶段有硬上限，不会因大量 JS 文件/接口而无限运行。
	jsTotalTimeout := timeout * len(httpAssets) / threads
	if jsTotalTimeout < 60 {
		jsTotalTimeout = 60
	}
	w.taskLog(task.TaskId, LevelInfo, "JSFinder: total timeout=%ds (single=%ds, assets=%d, threads=%d)",
		jsTotalTimeout, timeout, len(httpAssets), threads)
	jsCtx, jsCancel := context.WithTimeout(ctx, time.Duration(jsTotalTimeout)*time.Second)
	defer jsCancel()

	result, err := jsScanner.Scan(jsCtx, &scanner.ScanConfig{
		Assets:     httpAssets,
		Options:    opts,
		MainTaskId: task.MainTaskId,
		TaskLogger: jsTaskLogger,
		OnProgress: w.makeOnProgress(task, "JSFinder扫描"),
	})

	if ctx.Err() != nil || jsCtx.Err() != nil || w.checkTaskControl(ctx, task) == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "JSFinder: task stopped or timed out")
		return nil
	}

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "JSFinder error: %v", err)
		return nil
	}

	if result == nil || len(result.JSFinderResults) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "JSFinder: no findings")
		return nil
	}

	w.taskLog(task.TaskId, LevelInfo, "JSFinder: completed, found %d findings", len(result.JSFinderResults))
	return result.JSFinderResults
}

// parseStatusCode 解析状态码字符串为整数
func parseStatusCode(status string) int {
	if status == "" {
		return 0
	}
	var code int
	fmt.Sscanf(status, "%d", &code)
	return code
}
