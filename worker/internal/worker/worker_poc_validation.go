package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"cscan/internal/model"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
)

func (w *Worker) executePocValidateTask(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, startTime time.Time) {
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "POC validation task panic recovered: %v, stack: %s", r, string(getStackTrace()))
			batchId, _ := taskConfig["batchId"].(string)
			w.savePocValidationResult(ctx, task, batchId, nil, fmt.Sprintf("POC validation panic: %v", r))
		}
	}()

	url, _ := taskConfig["url"].(string)
	pocId, _ := taskConfig["pocId"].(string)
	pocType, _ := taskConfig["pocType"].(string)
	timeout, _ := taskConfig["timeout"].(float64)
	batchId, _ := taskConfig["batchId"].(string)

	w.taskLog(task.TaskId, LevelInfo, "[%s] 收到POC验证任务, 目标: %s", task.TaskId, url)

	if url == "" {
		w.taskLog(task.TaskId, LevelError, "[%s] POC验证失败: URL为空", task.TaskId)
		w.savePocValidationResult(ctx, task, batchId, nil, "URL为空")
		return
	}

	if timeout == 0 {
		timeout = 30
	}

	nucleiScanner, ok := w.scanners["nuclei"]
	if !ok {
		w.taskLog(task.TaskId, LevelError, "[%s] POC验证失败: Nuclei扫描器未初始化", task.TaskId)
		w.savePocValidationResult(ctx, task, batchId, nil, "Nuclei扫描器未初始化")
		return
	}

	var templates []string
	var templateRefs []string
	var pocName string
	var pocSeverity string

	if pocId != "" {
		w.taskLog(task.TaskId, LevelInfo, "[%s] Loading POC template...", task.TaskId)
		resp, err := w.loadPocById(ctx, pocId, pocType)
		if err != nil {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: failed to get POC - %v", task.TaskId, err)
			w.savePocValidationResult(ctx, task, batchId, nil, "Failed to get POC: "+err.Error())
			return
		}
		if !resp.Success {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: POC not found - %s", task.TaskId, resp.Msg)
			w.savePocValidationResult(ctx, task, batchId, nil, "POC not found: "+resp.Msg)
			return
		}
		if resp.Content == "" {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: POC content is empty", task.TaskId)
			w.savePocValidationResult(ctx, task, batchId, nil, "POC content is empty")
			return
		}
		templates = []string{resp.Content}
		pocName = resp.Name
		pocSeverity = resp.Severity
		pocType = resp.PocType
		w.taskLog(task.TaskId, LevelInfo, "[%s] POC template loaded: %s", task.TaskId, pocName)
	} else {
		var severities []string
		var tags []string

		if sevList, ok := taskConfig["severities"].([]interface{}); ok {
			for _, s := range sevList {
				if str, ok := s.(string); ok {
					severities = append(severities, str)
				}
			}
		}

		if tagList, ok := taskConfig["tags"].([]interface{}); ok {
			for _, t := range tagList {
				if str, ok := t.(string); ok {
					tags = append(tags, str)
				}
			}
		}

		if len(tags) > 0 {
			templates, templateRefs = w.resolveTemplatesByTags(ctx, tags, severities)
		}

		if len(templates) == 0 && len(templateRefs) == 0 {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: no POC templates found", task.TaskId)
			w.savePocValidationResult(ctx, task, batchId, nil, "No POC templates found")
			return
		}
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Initializing Nuclei scan engine...", task.TaskId)

	nucleiOpts := &scanner.NucleiOptions{
		RateLimit:        50,
		Concurrency:      10,
		CustomTemplates:  templates,
		TemplateFileRefs: templateRefs,
		CustomPocOnly:    true,
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Scanning target: %s", task.TaskId, url)

	result, err := nucleiScanner.Scan(ctx, &scanner.ScanConfig{
		Targets: []string{url},
		Options: nucleiOpts,
	})

	duration := time.Since(startTime).Seconds()

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: %v", task.TaskId, err)
		w.savePocValidationResult(ctx, task, batchId, nil, fmt.Sprintf("Scan failed: %v", err))
		return
	}

	var validationResults []*PocValidationResult

	w.taskLog(task.TaskId, LevelInfo, "[%s] Scan completed, duration: %.2fs", task.TaskId, duration)

	if result != nil && len(result.Vulnerabilities) > 0 {
		for _, vul := range result.Vulnerabilities {
			resultPocName := pocName
			resultSeverity := pocSeverity
			if resultPocName == "" {
				resultPocName = vul.PocFile
			}
			if resultSeverity == "" {
				resultSeverity = vul.Severity
			}
			validationResults = append(validationResults, &PocValidationResult{
				PocId:       pocId,
				PocName:     resultPocName,
				TemplateId:  pocId,
				Severity:    resultSeverity,
				Matched:     true,
				MatchedUrl:  vul.Url,
				Details:     vul.Result,
				Output:      vul.Extra,
				PocType:     pocType,
				MatcherName: vul.MatcherName,
				Request:     vul.Request,
				Response:    vul.Response,
			})
			logx.Infof("[%s] Vulnerability found! Matched URL: %s", task.TaskId, vul.Url)
			w.taskLog(task.TaskId, LevelInfo, "[%s] Vulnerability found! Matched URL: %s", task.TaskId, vul.Url)
		}
		w.saveVulResultWithFallback(ctx, task.MainTaskId, result.Vulnerabilities)
	} else {
		resultPocName := pocName
		if resultPocName == "" {
			resultPocName = pocId
		}
		validationResults = append(validationResults, &PocValidationResult{
			PocId:      pocId,
			PocName:    resultPocName,
			Severity:   pocSeverity,
			Matched:    false,
			MatchedUrl: url,
			Details:    "No vulnerability found",
			PocType:    pocType,
		})
		w.taskLog(task.TaskId, LevelInfo, "[%s] No vulnerability found", task.TaskId)
	}

	w.savePocValidationResult(ctx, task, batchId, validationResults, "")
}

// executeVulnReverifyTask 执行单条漏洞复验（复测）任务
func (w *Worker) executeVulnReverifyTask(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, startTime time.Time) {
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "Vuln reverify task panic recovered: %v, stack: %s", r, string(getStackTrace()))
			w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionReachableUntested, fmt.Sprintf("reverify panic: %v", r))
		}
	}()

	vulnId, _ := taskConfig["vulnId"].(string)
	authority, _ := taskConfig["authority"].(string)
	host, _ := taskConfig["host"].(string)
	url, _ := taskConfig["url"].(string)
	pocFile, _ := taskConfig["pocFile"].(string)

	port := ""
	switch p := taskConfig["port"].(type) {
	case string:
		port = p
	case int:
		if p > 0 {
			port = strconv.Itoa(p)
		}
	case int32:
		if p > 0 {
			port = strconv.Itoa(int(p))
		}
	case float64:
		if p > 0 {
			port = strconv.Itoa(int(p))
		}
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] 收到漏洞复验任务, vulnId: %s, target: %s, pocFile: %s", task.TaskId, vulnId, url, pocFile)

	if vulnId == "" {
		w.taskLog(task.TaskId, LevelError, "[%s] 漏洞复验失败: vulnId为空", task.TaskId)
		return
	}

	target := url
	if target == "" {
		if authority != "" {
			target = authority
		} else if host != "" {
			if port != "" {
				target = host + ":" + port
			} else {
				target = host
			}
		}
	}
	if target == "" {
		w.taskLog(task.TaskId, LevelError, "[%s] 漏洞复验失败: 无法构造复测目标", task.TaskId)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionUnreachable, "无法构造复测目标(url/authority/host均为空)")
		return
	}

	nucleiScanner, ok := w.scanners["nuclei"]
	if !ok {
		w.taskLog(task.TaskId, LevelError, "[%s] 漏洞复验失败: Nuclei扫描器未初始化", task.TaskId)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionReachableUntested, "Nuclei扫描器未初始化")
		return
	}

	if pocFile == "" {
		w.taskLog(task.TaskId, LevelInfo, "[%s] 漏洞无关联POC模板，无法精准复测，按可达未测处理", task.TaskId)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionReachableUntested, "漏洞无关联POC模板，无法精准复测")
		return
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Loading reverify template: %s", task.TaskId, pocFile)
	resp, err := w.loadTemplates(ctx, &TemplatesReq{
		NucleiTemplateIds: []string{pocFile},
	})
	if err != nil {
		msg := "获取复验模板失败: " + err.Error()
		w.taskLog(task.TaskId, LevelError, "[%s] %s", task.TaskId, msg)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionUnreachable, msg)
		return
	}
	if !resp.Success || len(resp.Templates) == 0 {
		msg := "复验模板为空"
		if resp.Msg != "" {
			msg = "获取复验模板失败: " + resp.Msg
		}
		w.taskLog(task.TaskId, LevelError, "[%s] %s", task.TaskId, msg)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionUnreachable, msg)
		return
	}

	nucleiOpts := &scanner.NucleiOptions{
		RateLimit:       50,
		Concurrency:     10,
		CustomTemplates: resp.Templates,
		CustomPocOnly:   true,
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Initializing Nuclei scan engine for reverify...", task.TaskId)
	w.taskLog(task.TaskId, LevelInfo, "[%s] Reverify scanning target: %s", task.TaskId, target)

	result, err := nucleiScanner.Scan(ctx, &scanner.ScanConfig{
		Targets: []string{target},
		Options: nucleiOpts,
	})
	duration := time.Since(startTime).Seconds()

	if err != nil {
		msg := fmt.Sprintf("复测扫描失败: %v", err)
		w.taskLog(task.TaskId, LevelError, "[%s] %s", task.TaskId, msg)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionUnreachable, msg)
		return
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Reverify scan completed, duration: %.2fs", task.TaskId, duration)

	if result != nil && len(result.Vulnerabilities) > 0 {
		w.taskLog(task.TaskId, LevelInfo, "[%s] 复验结论: 漏洞仍然存在(still_vuln)", task.TaskId)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionStillVuln, "复测仍命中漏洞，漏洞未修复")
	} else {
		w.taskLog(task.TaskId, LevelInfo, "[%s] 复验结论: 漏洞已修复(fixed)", task.TaskId)
		w.reportReverifyResult(ctx, task, taskConfig, model.ReverifyConclusionFixed, "复测未命中漏洞，漏洞已修复")
	}
}

// reportReverifyResult 将复验结论上报给 API 服务
func (w *Worker) reportReverifyResult(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, conclusion, message string) {
	vulnId, _ := taskConfig["vulnId"].(string)
	reviewer, _ := taskConfig["reviewer"].(string)

	resp, err := w.httpClient.SaveVulReverify(ctx, &VulReverifyReq{
		VulnId:     vulnId,
		Conclusion: conclusion,
		Reviewer:   reviewer,
		Message:    message,
		ReverifyAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "[%s] 复验结果上报失败: %v", task.TaskId, err)
		return
	}
	if !resp.Success {
		w.taskLog(task.TaskId, LevelError, "[%s] 复验结果上报失败: %s", task.TaskId, resp.Msg)
		return
	}
	w.taskLog(task.TaskId, LevelInfo, "[%s] 复验结果上报成功: %s", task.TaskId, conclusion)
}

// executePocBatchValidateTask 执行POC批量验证任务（使用单个Nuclei引擎扫描所有目标）
func (w *Worker) executePocBatchValidateTask(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, startTime time.Time) {
	defer func() {
		if r := recover(); r != nil {
			w.taskLog(task.TaskId, LevelError, "POC batch validation task panic recovered: %v, stack: %s", r, string(getStackTrace()))
			w.savePocValidationResult(ctx, task, "", nil, fmt.Sprintf("POC batch validation panic: %v", r))
		}
	}()

	pocId, _ := taskConfig["pocId"].(string)
	pocType, _ := taskConfig["pocType"].(string)
	timeout, _ := taskConfig["timeout"].(float64)

	var urls []string
	if urlsInterface, ok := taskConfig["urls"].([]interface{}); ok {
		for _, u := range urlsInterface {
			if urlStr, ok := u.(string); ok && urlStr != "" {
				urls = append(urls, urlStr)
			}
		}
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] 收到POC批量扫描任务, 目标数: %d", task.TaskId, len(urls))

	if len(urls) == 0 {
		w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: 目标列表为空", task.TaskId)
		w.savePocValidationResult(ctx, task, "", nil, "目标列表为空")
		return
	}

	if timeout == 0 {
		timeout = float64(len(urls) * 30)
		if timeout < 60 {
			timeout = 60
		}
	}

	nucleiScanner, ok := w.scanners["nuclei"].(*scanner.NucleiScanner)
	if !ok {
		w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: Nuclei扫描器未初始化", task.TaskId)
		w.savePocValidationResult(ctx, task, "", nil, "Nuclei扫描器未初始化")
		return
	}

	var templates []string
	var pocName string
	var pocSeverity string

	if pocId != "" {
		w.taskLog(task.TaskId, LevelInfo, "[%s] Loading POC template...", task.TaskId)
		resp, err := w.loadPocById(ctx, pocId, pocType)
		if err != nil {
			w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: 获取POC失败 - %v", task.TaskId, err)
			w.savePocValidationResult(ctx, task, "", nil, "获取POC失败: "+err.Error())
			return
		}
		if !resp.Success || resp.Content == "" {
			w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: POC不存在或内容为空", task.TaskId)
			w.savePocValidationResult(ctx, task, "", nil, "POC不存在或内容为空")
			return
		}
		templates = []string{resp.Content}
		pocName = resp.Name
		pocSeverity = resp.Severity
		pocType = resp.PocType
		w.taskLog(task.TaskId, LevelInfo, "[%s] POC template loaded: %s", task.TaskId, pocName)
	} else {
		w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: 未指定POC ID", task.TaskId)
		w.savePocValidationResult(ctx, task, "", nil, "未指定POC ID")
		return
	}

	nucleiOpts := &scanner.NucleiOptions{
		RateLimit:       150,
		Concurrency:     25,
		CustomTemplates: templates,
		CustomPocOnly:   true,
		OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
			w.taskLog(task.TaskId, LevelInfo, "[%s] Vulnerability found! %s → %s", task.TaskId, vul.PocFile, vul.Url)
			w.saveVulResultWithFallback(ctx, task.MainTaskId, []*scanner.Vulnerability{vul})
		},
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Starting batch scan: %d targets, timeout %ds", task.TaskId, len(urls), int(timeout))

	// 任务 timeout 控制整个批量扫描进程，不得复用为 nuclei 的单请求 -timeout。
	batchCtx, batchCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	vuls, err := nucleiScanner.ScanBatch(batchCtx, urls, nucleiOpts, func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, "[%s] "+format, append([]interface{}{task.TaskId}, args...)...)
	})
	batchCancel()

	duration := time.Since(startTime).Seconds()

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描出错: %v", task.TaskId, err)
	}

	vulCount := len(vuls)
	w.taskLog(task.TaskId, LevelInfo, "[%s] Batch scan completed, duration: %.2fs, vuls: %d", task.TaskId, duration, vulCount)

	if vulCount > 0 {
		w.taskLog(task.TaskId, LevelInfo, "[%s] Total %d vulnerabilities saved to database", task.TaskId, vulCount)
	}

	var validationResults []*PocValidationResult
	if vulCount > 0 {
		for _, vul := range vuls {
			validationResults = append(validationResults, &PocValidationResult{
				PocId:       pocId,
				PocName:     pocName,
				TemplateId:  pocId,
				Severity:    pocSeverity,
				Matched:     true,
				MatchedUrl:  vul.Url,
				Details:     vul.Result,
				Output:      vul.Extra,
				PocType:     pocType,
				MatcherName: vul.MatcherName,
				Request:     vul.Request,
				Response:    vul.Response,
			})
		}
	}

	w.savePocValidationResult(ctx, task, "", validationResults, "")
}

// PocValidationResult POC验证结果
type PocValidationResult struct {
	PocId       string   `json:"pocId"`
	PocName     string   `json:"pocName"`
	TemplateId  string   `json:"templateId"`
	Severity    string   `json:"severity"`
	Matched     bool     `json:"matched"`
	MatchedUrl  string   `json:"matchedUrl"`
	Details     string   `json:"details"`
	Output      string   `json:"output"`
	PocType     string   `json:"pocType"`
	Tags        []string `json:"tags"`
	MatcherName string   `json:"matcherName,omitempty"`
	Request     string   `json:"request,omitempty"`
	Response    string   `json:"response,omitempty"`
}

// savePocValidationResult 保存POC验证结果
func (w *Worker) savePocValidationResult(ctx context.Context, task *scheduler.TaskInfo, batchId string, results []*PocValidationResult, errorMsg string) {
	if task == nil {
		return
	}
	taskId := task.TaskId
	resultData := map[string]interface{}{
		"taskId":     taskId,
		"batchId":    batchId,
		"status":     "SUCCESS",
		"results":    results,
		"updateTime": time.Now().Local().Format("2006-01-02 15:04:05"),
	}

	if errorMsg != "" {
		resultData["status"] = "FAILURE"
		resultData["error"] = errorMsg
	}

	resultJson, err := json.Marshal(resultData)
	if err != nil {
		w.taskLog(taskId, LevelError, "Failed to marshal POC validation result: %v", err)
		return
	}

	status := scheduler.TaskStatusSuccess
	if errorMsg != "" {
		status = scheduler.TaskStatusFailure
	}
	_, err = w.httpClient.UpdateTask(ctx, &TaskUpdateReq{
		TaskId:     taskId,
		LeaseToken: task.LeaseToken,
		State:      status,
		Worker:     w.workerName,
		Result:     string(resultJson),
		Progress:   100,
		Phase:      "完成",
	})
	if err != nil {
		w.taskLog(taskId, LevelError, "Failed to save POC validation result: %v", err)
	}
}
