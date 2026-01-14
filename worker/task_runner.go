package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cscan/scanner"
	"cscan/scheduler"
)

// TaskPhase 任务执行阶段
type TaskPhase string

const (
	PhaseDomainScan    TaskPhase = "domainscan"
	PhasePortScan      TaskPhase = "portscan"
	PhasePortIdentify  TaskPhase = "portidentify"
	PhaseFingerprint   TaskPhase = "fingerprint"
	PhaseDirScan       TaskPhase = "dirscan"
	PhasePocScan       TaskPhase = "pocscan"
)

// PhaseConfig 阶段配置
type PhaseConfig struct {
	Phase           TaskPhase
	Name            string // 显示名称
	Scanner         string // 扫描器名称
	ProgressStart   int    // 进度起始值
	ProgressEnd     int    // 进度结束值
	ContinueOnError bool   // 错误时是否继续
}

// DefaultPhaseOrder 默认阶段执行顺序
var DefaultPhaseOrder = []PhaseConfig{
	{Phase: PhaseDomainScan, Name: "子域名扫描", Scanner: "subfinder", ProgressStart: 10, ProgressEnd: 20, ContinueOnError: true},
	{Phase: PhasePortScan, Name: "端口扫描", Scanner: "naabu", ProgressStart: 20, ProgressEnd: 40, ContinueOnError: true},
	{Phase: PhasePortIdentify, Name: "端口识别", Scanner: "nmap/fingerprintx", ProgressStart: 40, ProgressEnd: 50, ContinueOnError: true},
	{Phase: PhaseFingerprint, Name: "指纹识别", Scanner: "fingerprint", ProgressStart: 50, ProgressEnd: 70, ContinueOnError: true},
	{Phase: PhaseDirScan, Name: "目录扫描", Scanner: "urlfinder", ProgressStart: 70, ProgressEnd: 80, ContinueOnError: true},
	{Phase: PhasePocScan, Name: "漏洞扫描", Scanner: "nuclei", ProgressStart: 80, ProgressEnd: 100, ContinueOnError: true},
}

// TaskRunner 任务执行器
// 提供统一的任务执行管道，消除特殊情况处理
type TaskRunner struct {
	scanners       map[string]scanner.Scanner
	logger         Logger
	phaseOrder     []PhaseConfig
	phaseExecutors map[TaskPhase]PhaseExecutor
}

// PhaseExecutor 阶段执行器接口
type PhaseExecutor interface {
	// CanExecute 检查是否可以执行该阶段
	CanExecute(ctx *TaskContext) bool
	// Execute 执行阶段
	Execute(ctx *TaskContext) (*PhaseResult, error)
}

// TaskContext 任务执行上下文
type TaskContext struct {
	Ctx             context.Context
	Task            *scheduler.TaskInfo
	Config          *scheduler.TaskConfig
	RawConfig       map[string]interface{}
	Target          string
	OrgId           string
	Assets          []*scanner.Asset
	Vulnerabilities []*scanner.Vulnerability
	CompletedPhases map[TaskPhase]bool
	Runner          *TaskRunner
	Worker          *Worker // 引用Worker以访问其方法
}

// PhaseResult 阶段执行结果
type PhaseResult struct {
	Assets          []*scanner.Asset
	Vulnerabilities []*scanner.Vulnerability
	Stopped         bool   // 是否被停止
	Paused          bool   // 是否被暂停
	Error           error  // 执行错误
	Message         string // 结果消息
}

// TaskRunnerConfig 任务执行器配置
type TaskRunnerConfig struct {
	PhaseOrder []PhaseConfig
}

// NewTaskRunner 创建任务执行器
func NewTaskRunner(scanners map[string]scanner.Scanner, logger Logger) *TaskRunner {
	runner := &TaskRunner{
		scanners:       scanners,
		logger:         logger,
		phaseOrder:     DefaultPhaseOrder,
		phaseExecutors: make(map[TaskPhase]PhaseExecutor),
	}
	return runner
}

// NewTaskRunnerWithConfig 使用自定义配置创建任务执行器
func NewTaskRunnerWithConfig(scanners map[string]scanner.Scanner, logger Logger, config TaskRunnerConfig) *TaskRunner {
	runner := NewTaskRunner(scanners, logger)
	if len(config.PhaseOrder) > 0 {
		runner.phaseOrder = config.PhaseOrder
	}
	return runner
}

// RegisterPhaseExecutor 注册阶段执行器
func (r *TaskRunner) RegisterPhaseExecutor(phase TaskPhase, executor PhaseExecutor) {
	r.phaseExecutors[phase] = executor
}

// GetScanner 获取扫描器
func (r *TaskRunner) GetScanner(name string) (scanner.Scanner, bool) {
	s, ok := r.scanners[name]
	return s, ok
}

// Log 记录日志
func (r *TaskRunner) Log(level, format string, args ...interface{}) {
	if r.logger != nil {
		switch level {
		case LevelError:
			r.logger.Error(format, args...)
		case LevelWarn:
			r.logger.Warn(format, args...)
		case LevelDebug:
			r.logger.Debug(format, args...)
		default:
			r.logger.Info(format, args...)
		}
	}
}

// TaskControlAction 任务控制动作
type TaskControlAction string

const (
	TaskControlContinue TaskControlAction = "CONTINUE"
	TaskControlStop     TaskControlAction = "STOP"
	TaskControlPause    TaskControlAction = "PAUSE"
	TaskControlCancel   TaskControlAction = "CANCEL"
)

// checkTaskControlSignal 统一的任务控制检查 - 消除重复的控制检查代码
func (r *TaskRunner) checkTaskControlSignal(ctx context.Context, worker *Worker, taskId string) TaskControlAction {
	// 检查context取消
	select {
	case <-ctx.Done():
		return TaskControlCancel
	default:
	}

	// 检查worker控制信号
	if worker == nil {
		return TaskControlContinue
	}

	ctrl := worker.checkTaskControl(ctx, taskId)
	switch ctrl {
	case "STOP":
		return TaskControlStop
	case "PAUSE":
		return TaskControlPause
	default:
		return TaskControlContinue
	}
}

// Run 执行任务（统一入口）
func (r *TaskRunner) Run(ctx context.Context, task *scheduler.TaskInfo, worker *Worker) (*TaskResult, error) {
	startTime := time.Now()

	// 1. 解析配置
	taskCtx, err := r.parseTaskContext(ctx, task, worker)
	if err != nil {
		return nil, fmt.Errorf("parse task config failed: %w", err)
	}

	// 2. 构建执行计划
	phases := r.buildExecutionPlan(taskCtx)

	// 3. 按顺序执行各阶段
	for _, phaseConfig := range phases {
		// 统一的控制检查
		switch r.checkTaskControlSignal(ctx, worker, task.TaskId) {
		case TaskControlCancel:
			return r.buildResult(taskCtx, startTime, ctx.Err())
		case TaskControlStop:
			r.Log(LevelInfo, "Task %s stopped before phase %s", task.TaskId, phaseConfig.Name)
			return r.buildResult(taskCtx, startTime, nil)
		case TaskControlPause:
			r.Log(LevelInfo, "Task %s paused before phase %s", task.TaskId, phaseConfig.Name)
			if worker != nil {
				worker.saveTaskProgress(ctx, task, r.convertCompletedPhases(taskCtx.CompletedPhases), taskCtx.Assets)
			}
			return r.buildResult(taskCtx, startTime, nil)
		}

		// 更新进度
		if worker != nil {
			worker.updateTaskProgressWithPhase(ctx, task.TaskId, phaseConfig.ProgressStart, phaseConfig.Name+"中", phaseConfig.Name)
		}

		// 执行阶段
		result, err := r.executePhase(taskCtx, phaseConfig)
		if err != nil {
			r.Log(LevelError, "Phase %s failed: %v", phaseConfig.Name, err)
			if !phaseConfig.ContinueOnError {
				return r.buildResult(taskCtx, startTime, err)
			}
		}

		// 处理阶段结果
		if result != nil {
			if result.Stopped {
				r.Log(LevelInfo, "Task %s stopped during phase %s", task.TaskId, phaseConfig.Name)
				return r.buildResult(taskCtx, startTime, nil)
			}
			if result.Paused {
				r.Log(LevelInfo, "Task %s paused during phase %s", task.TaskId, phaseConfig.Name)
				if worker != nil {
					worker.saveTaskProgress(ctx, task, r.convertCompletedPhases(taskCtx.CompletedPhases), taskCtx.Assets)
				}
				return r.buildResult(taskCtx, startTime, nil)
			}

			// 合并结果
			if len(result.Assets) > 0 {
				taskCtx.Assets = append(taskCtx.Assets, result.Assets...)
			}
			if len(result.Vulnerabilities) > 0 {
				taskCtx.Vulnerabilities = append(taskCtx.Vulnerabilities, result.Vulnerabilities...)
			}
		}

		// 标记阶段完成
		taskCtx.CompletedPhases[phaseConfig.Phase] = true

		// 递增子任务完成计数
		if worker != nil {
			worker.incrSubTaskDone(ctx, task, phaseConfig.Name)
		}
	}

	return r.buildResult(taskCtx, startTime, nil)
}

// parseTaskContext 解析任务上下文
func (r *TaskRunner) parseTaskContext(ctx context.Context, task *scheduler.TaskInfo, worker *Worker) (*TaskContext, error) {
	// 解析原始配置
	var rawConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &rawConfig); err != nil {
		return nil, fmt.Errorf("unmarshal raw config: %w", err)
	}

	// 解析结构化配置
	config, err := scheduler.ParseTaskConfig(task.Config)
	if err != nil {
		// 使用默认配置
		config = &scheduler.TaskConfig{
			PortScan: &scheduler.PortScanConfig{Enable: true, Ports: "80,443,8080"},
		}
	}

	// 获取目标
	target, _ := rawConfig["target"].(string)
	if target == "" {
		return nil, fmt.Errorf("target is empty")
	}

	// 获取组织ID
	orgId, _ := rawConfig["orgId"].(string)

	// 解析恢复状态
	completedPhases := make(map[TaskPhase]bool)
	var resumedAssets []*scanner.Asset

	if stateStr, ok := rawConfig["resumeState"].(string); ok && stateStr != "" {
		var resumeState map[string]interface{}
		if err := json.Unmarshal([]byte(stateStr), &resumeState); err == nil {
			// 恢复已完成的阶段
			if phases, ok := resumeState["completedPhases"].([]interface{}); ok {
				for _, p := range phases {
					if ps, ok := p.(string); ok {
						completedPhases[TaskPhase(ps)] = true
					}
				}
			}
			// 恢复已扫描的资产
			if assetsJson, ok := resumeState["assets"].(string); ok && assetsJson != "" {
				json.Unmarshal([]byte(assetsJson), &resumedAssets)
			}
		}
	}

	return &TaskContext{
		Ctx:             ctx,
		Task:            task,
		Config:          config,
		RawConfig:       rawConfig,
		Target:          target,
		OrgId:           orgId,
		Assets:          resumedAssets,
		Vulnerabilities: nil,
		CompletedPhases: completedPhases,
		Runner:          r,
		Worker:          worker,
	}, nil
}

// buildExecutionPlan 构建执行计划
func (r *TaskRunner) buildExecutionPlan(taskCtx *TaskContext) []PhaseConfig {
	var phases []PhaseConfig

	for _, phaseConfig := range r.phaseOrder {
		// 跳过已完成的阶段
		if taskCtx.CompletedPhases[phaseConfig.Phase] {
			continue
		}

		// 检查阶段是否启用
		if !r.isPhaseEnabled(taskCtx, phaseConfig.Phase) {
			continue
		}

		phases = append(phases, phaseConfig)
	}

	return phases
}

// isPhaseEnabled 检查阶段是否启用
func (r *TaskRunner) isPhaseEnabled(taskCtx *TaskContext, phase TaskPhase) bool {
	config := taskCtx.Config
	if config == nil {
		return false
	}

	switch phase {
	case PhaseDomainScan:
		return config.DomainScan != nil && config.DomainScan.Enable
	case PhasePortScan:
		return config.PortScan != nil && config.PortScan.Enable
	case PhasePortIdentify:
		return config.PortIdentify != nil && config.PortIdentify.Enable
	case PhaseFingerprint:
		return config.Fingerprint != nil && config.Fingerprint.Enable
	case PhaseDirScan:
		return config.DirScan != nil && config.DirScan.Enable
	case PhasePocScan:
		return config.PocScan != nil && config.PocScan.Enable
	default:
		return false
	}
}

// executePhase 执行单个阶段
func (r *TaskRunner) executePhase(taskCtx *TaskContext, phaseConfig PhaseConfig) (*PhaseResult, error) {
	// 检查是否有自定义执行器
	if executor, ok := r.phaseExecutors[phaseConfig.Phase]; ok {
		if executor.CanExecute(taskCtx) {
			return executor.Execute(taskCtx)
		}
		return &PhaseResult{}, nil
	}

	// 使用默认执行逻辑
	return r.executePhaseDefault(taskCtx, phaseConfig)
}

// executePhaseDefault 默认阶段执行逻辑
func (r *TaskRunner) executePhaseDefault(taskCtx *TaskContext, phaseConfig PhaseConfig) (*PhaseResult, error) {
	// 获取扫描器
	s, ok := r.scanners[phaseConfig.Scanner]
	if !ok {
		r.Log(LevelWarn, "Scanner %s not found for phase %s", phaseConfig.Scanner, phaseConfig.Name)
		return &PhaseResult{}, nil
	}

	// 构建扫描配置
	scanConfig := &scanner.ScanConfig{
		Target:      taskCtx.Target,
		Assets:      taskCtx.Assets,
		WorkspaceId: taskCtx.Task.WorkspaceId,
		MainTaskId:  taskCtx.Task.MainTaskId,
	}

	// 设置阶段特定选项
	scanConfig.Options = r.getPhaseOptions(taskCtx, phaseConfig.Phase)

	// 执行扫描
	result, err := s.Scan(taskCtx.Ctx, scanConfig)
	if err != nil {
		return &PhaseResult{Error: err}, err
	}

	if result == nil {
		return &PhaseResult{}, nil
	}

	return &PhaseResult{
		Assets:          result.Assets,
		Vulnerabilities: result.Vulnerabilities,
	}, nil
}

// getPhaseOptions 获取阶段特定选项
func (r *TaskRunner) getPhaseOptions(taskCtx *TaskContext, phase TaskPhase) interface{} {
	config := taskCtx.Config
	if config == nil {
		return nil
	}

	switch phase {
	case PhaseDomainScan:
		return config.DomainScan
	case PhasePortScan:
		return config.PortScan
	case PhasePortIdentify:
		return config.PortIdentify
	case PhaseFingerprint:
		return config.Fingerprint
	case PhaseDirScan:
		return config.DirScan
	case PhasePocScan:
		return config.PocScan
	default:
		return nil
	}
}

// buildResult 构建任务结果
func (r *TaskRunner) buildResult(taskCtx *TaskContext, startTime time.Time, err error) (*TaskResult, error) {
	duration := time.Since(startTime)

	status := scheduler.TaskStatusSuccess
	message := ""
	if err != nil {
		status = scheduler.TaskStatusFailure
		message = err.Error()
	}

	return &TaskResult{
		TaskId:     taskCtx.Task.TaskId,
		Status:     status,
		Message:    message,
		AssetCount: len(taskCtx.Assets),
		VulCount:   len(taskCtx.Vulnerabilities),
		Duration:   int64(duration.Seconds()),
	}, err
}

// convertCompletedPhases 转换已完成阶段为字符串映射
func (r *TaskRunner) convertCompletedPhases(phases map[TaskPhase]bool) map[string]bool {
	result := make(map[string]bool)
	for phase, completed := range phases {
		result[string(phase)] = completed
	}
	return result
}

// TaskResult 任务执行结果
type TaskResult struct {
	TaskId     string
	Status     string
	Message    string
	AssetCount int
	VulCount   int
	Duration   int64
}

// FormatResult 格式化结果
func (r *TaskResult) FormatResult() string {
	return fmt.Sprintf("Assets:%d Vuls:%d Duration:%ds", r.AssetCount, r.VulCount, r.Duration)
}

// GetEnabledPhases 获取启用的阶段列表（用于日志输出）
func GetEnabledPhases(config *scheduler.TaskConfig) []string {
	var phases []string
	if config == nil {
		return phases
	}

	if config.DomainScan != nil && config.DomainScan.Enable {
		phases = append(phases, "Domain Scan")
	}
	if config.PortScan != nil && config.PortScan.Enable {
		phases = append(phases, "Port Scan")
	}
	if config.PortIdentify != nil && config.PortIdentify.Enable {
		phases = append(phases, "Port Identify")
	}
	if config.Fingerprint != nil && config.Fingerprint.Enable {
		phases = append(phases, "Fingerprint")
	}
	if config.DirScan != nil && config.DirScan.Enable {
		phases = append(phases, "Dir Scan")
	}
	if config.PocScan != nil && config.PocScan.Enable {
		phases = append(phases, "POC Scan")
	}

	return phases
}

// ParseTargets 解析目标列表
func ParseTargets(target string) []string {
	targetLines := strings.Split(strings.TrimSpace(target), "\n")
	var targets []string
	for _, line := range targetLines {
		line = strings.TrimSpace(line)
		if line != "" {
			targets = append(targets, line)
		}
	}
	return targets
}
