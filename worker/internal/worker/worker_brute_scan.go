package worker

import (
	"context"
	"fmt"
	"strings"

	"cscan/internal/scanner"
	"cscan/internal/scanner/brute"
	"cscan/internal/scheduler"
)

func (w *Worker) executeBruteScan(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.BruteScanConfig, orgId string) []*scanner.Vulnerability {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "Brute scan panic recovered: %v, stack: %s", r, string(getStackTrace()))
		}
	}()

	// 过滤出可爆破的资产（有明确服务识别的资产）
	var bruteAssets []*scanner.Asset

	// 如果配置了服务列表，建立服务集合
	serviceSet := make(map[string]bool)
	hasServiceFilter := len(config.Services) > 0
	if hasServiceFilter {
		for _, svc := range config.Services {
			serviceSet[svc] = true
		}
	}

	for _, asset := range assets {
		if asset.Service == "" {
			continue
		}
		// 检查服务是否在目标列表中（只有配置了服务列表时才过滤）
		if hasServiceFilter && !serviceSet[asset.Service] {
			continue
		}
		bruteAssets = append(bruteAssets, asset)
	}

	if len(bruteAssets) == 0 {
		w.taskLog(task.TaskId, LevelInfo, "Brute scan: skipped (no service assets)")
		return nil
	}

	// 预检查：是否有资产匹配到暴力破解插件
	hasPlugin := false
	for _, asset := range bruteAssets {
		normalizedService := brute.NormalizeServiceName(asset.Service)
		if brute.GetPlugin(normalizedService) != nil {
			hasPlugin = true
			break
		}
	}
	if !hasPlugin {
		w.taskLog(task.TaskId, LevelInfo, "Brute scan: skipped (no matching brute-force plugin for detected services)")
		return nil
	}

	w.taskLog(task.TaskId, LevelInfo, "Brute scan: %d target assets, services=%v", len(bruteAssets), config.Services)

	// 获取字典内容
	// 字典格式是 user:password，需要拆分
	var usernameDict, passwordDict string
	usernameSet := make(map[string]struct{})
	passwordSet := make(map[string]struct{})

	// 服务特定字典：service -> entries
	serviceDicts := make(map[string][]scanner.ServiceDictEntry)

	// 获取要使用的字典ID列表和目标服务列表
	dictIds := config.WeakpassDictIds
	targetServices := config.Services

	// 获取字典内容（直连 MongoDB，按服务类型过滤字典）
	dictResp, err := w.loadWeakpassDicts(ctx, dictIds, targetServices)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Brute scan: get dicts failed: %v", err)
		return nil
	}

	if len(dictResp.Dicts) > 0 {
		for _, dict := range dictResp.Dicts {
			// 获取字典的服务类型
			serviceType := strings.ToLower(strings.TrimSpace(dict.Service))
			if serviceType == "" {
				serviceType = "common"
			}

			// 解析 user:password 格式的字典内容
			lines := strings.Split(dict.Content, "\n")
			currentService := serviceType

			for _, line := range lines {
				// 清理行尾的 \r（处理 Windows CRLF 格式）
				line = strings.TrimRight(line, "\r")
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}

				// 检查是否是服务分组标记 [service]
				if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
					currentService = strings.ToLower(strings.Trim(line, "[]"))
					continue
				}

				// 按冒号分割
				parts := strings.SplitN(line, ":", 2)
				username := ""
				password := ""
				if len(parts) >= 1 {
					username = strings.TrimSpace(parts[0])
				}
				if len(parts) >= 2 {
					password = strings.TrimSpace(parts[1])
				}

				// 添加到通用集合
				if username != "" {
					usernameSet[username] = struct{}{}
				}
				if password != "" {
					passwordSet[password] = struct{}{}
				}

				// 添加到服务特定字典
				if username != "" || password != "" {
					entry := scanner.ServiceDictEntry{
						Username: username,
						Password: password,
					}
					serviceDicts[currentService] = append(serviceDicts[currentService], entry)
				}
			}
		}
		// 去重后合并到字符串
		for u := range usernameSet {
			usernameDict += u + "\n"
		}
		for p := range passwordSet {
			passwordDict += p + "\n"
		}

		// 统计服务字典信息
		serviceDictInfo := make([]string, 0, len(serviceDicts))
		for svc, entries := range serviceDicts {
			serviceDictInfo = append(serviceDictInfo, fmt.Sprintf("%s(%d)", svc, len(entries)))
		}

		w.taskLog(task.TaskId, LevelInfo, "Brute scan: using %d dicts, %d usernames, %d passwords, services: %v",
			len(dictResp.Dicts), len(usernameSet), len(passwordSet), serviceDictInfo)
	}

	// 如果没有获取到字典内容且没有启用默认字典，跳过
	if usernameDict == "" && passwordDict == "" && len(serviceDicts) == 0 && !config.UseDefaultDict {
		w.taskLog(task.TaskId, LevelWarn, "Brute scan: no dicts configured")
		return nil
	}

	// 构建扫描配置 - 使用 Worker 的并发数
	bruteScanConfig := &scanner.BruteScanConfig{
		Services:       config.Services,
		Threads:        w.config.Concurrency, // 使用 Worker 并发数
		Timeout:        config.Timeout,
		DelayMs:        config.DelayMs,
		UseDefaultDict: config.UseDefaultDict,
		StopOnFirst:    config.StopOnFirst,
		ForceScan:      config.ForceScan,
		UsernameDict:   usernameDict,
		PasswordDict:   passwordDict,
		ServiceDicts:   serviceDicts,
		OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
			// 流式入库：发现弱口令立即保存
			w.saveVulResultWithFallback(ctx, task.MainTaskId, []*scanner.Vulnerability{vul})
		},
	}

	// 构建扫描器配置
	scanConfig := &scanner.ScanConfig{
		Assets:     bruteAssets,
		Options:    bruteScanConfig,
		MainTaskId: task.MainTaskId,
		OnProgress: w.makeOnProgress(task, "弱口令扫描"),
		TaskLogger: func(level, format string, args ...interface{}) {
			w.taskLog(task.TaskId, level, format, args...)
		},
	}

	// 获取 brutescan 扫描器
	bruteScanner, ok := w.scanners["brutescan"]
	if !ok {
		w.taskLog(task.TaskId, LevelError, "Brute scan: brutescan scanner not found")
		return nil
	}

	// 执行扫描
	result, err := bruteScanner.Scan(ctx, scanConfig)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Brute scan error: %v", err)
		return nil
	}

	return result.Vulnerabilities
}

// executeDirScan 执行目录扫描阶段
