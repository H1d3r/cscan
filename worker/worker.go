package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"cscan/model"
	"cscan/pkg/mapping"
	"cscan/scanner"
	"cscan/scheduler"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WorkerConfig Worker配置
type WorkerConfig struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	ServerAddr  string `json:"serverAddr"`  // API 服务地址 (e.g., http://server:8888)
	InstallKey  string `json:"installKey"`  // 安装密钥
	Concurrency int    `json:"concurrency"`
	Timeout     int    `json:"timeout"`
}

// Worker 工作节点
type Worker struct {
	ctx         context.Context
	cancel      context.CancelFunc
	config      WorkerConfig
	httpClient  *WorkerHTTPClient // HTTP 客户端（替代 RPC 和 Redis）
	wsClient    *WorkerWSClient   // WebSocket 客户端（用于日志推送和控制信号）
	scanners    map[string]scanner.Scanner
	taskChan    chan *scheduler.TaskInfo
	resultChan  chan *scanner.ScanResult
	stopChan    chan struct{}
	wg          sync.WaitGroup
	mu          sync.Mutex

	taskStarted  int
	taskExecuted int
	isRunning    bool

	// 健康状态监控
	lastCPUCheck     time.Time // 上次CPU检查时间
	cpuOverloadCount int       // CPU过载计数
	isThrottled      bool      // 是否处于限流状态
	throttleUntil    time.Time // 限流结束时间

	// 任务控制信号
	taskControlSignals sync.Map // taskId -> action (STOP, PAUSE)

	// 正在执行的任务
	runningTasks sync.Map // taskId -> true

	// 日志组件
	logger Logger

	// 系统信息收集器
	sysInfoCollector *SysInfoCollector

	// 文件管理器
	fileManager *FileManager

	// 终端处理器
	terminalHandler *TerminalHandler

	// 资源管理器
	resourceManager *ResourceManager

	// 任务执行器集成
	taskRunnerIntegration *TaskRunnerIntegration
}

// getMainTaskId 从 taskId 中提取主任务ID
// 子任务格式: {mainTaskId}-{index}，主任务格式: {mainTaskId}
func getMainTaskId(taskId string) string {
	// 查找最后一个 "-" 后面是否是数字
	lastDash := strings.LastIndex(taskId, "-")
	if lastDash > 0 && lastDash < len(taskId)-1 {
		suffix := taskId[lastDash+1:]
		// 检查后缀是否全是数字
		isNumber := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				isNumber = false
				break
			}
		}
		if isNumber {
			return taskId[:lastDash]
		}
	}
	return taskId
}

// taskLog 发布任务级别日志
// 子任务的日志会同时写入主任务的日志流，方便统一查看
func (w *Worker) taskLog(taskId, level, format string, args ...interface{}) {
	// 获取主任务ID，确保子任务日志也能在主任务中查看
	mainTaskId := getMainTaskId(taskId)

	// 如果是子任务，在日志消息前加上子任务标识
	if mainTaskId != taskId {
		subIndex := taskId[len(mainTaskId)+1:]
		format = fmt.Sprintf("[Sub-%s] %s", subIndex, format)
	}

	// 使用 WebSocket 日志记录器，将日志发送到服务器
	logger := NewTaskLoggerWS(w.config.Name, mainTaskId, w.wsClient)

	switch level {
	case LevelError:
		logger.Error(format, args...)
	case LevelWarn:
		logger.Warn(format, args...)
	case LevelDebug:
		logger.Debug(format, args...)
	default:
		logger.Info(format, args...)
	}
}

// VulnerabilityBuffer 批量缓冲保存漏洞
type VulnerabilityBuffer struct {
	vuls      []*scanner.Vulnerability
	mu        sync.Mutex
	maxSize   int
	flushChan chan struct{}
}

// NewVulnerabilityBuffer 创建漏洞缓冲区
func NewVulnerabilityBuffer(maxSize int) *VulnerabilityBuffer {
	return &VulnerabilityBuffer{
		vuls:      make([]*scanner.Vulnerability, 0, maxSize),
		maxSize:   maxSize,
		flushChan: make(chan struct{}, 1),
	}
}

// Add 添加漏洞到缓冲区，返回是否需要刷新
func (b *VulnerabilityBuffer) Add(vul *scanner.Vulnerability) {
	b.mu.Lock()
	b.vuls = append(b.vuls, vul)
	shouldFlush := len(b.vuls) >= b.maxSize
	b.mu.Unlock()

	if shouldFlush {
		select {
		case b.flushChan <- struct{}{}:
		default:
		}
	}
}

// Flush 刷新缓冲区，批量保存
func (b *VulnerabilityBuffer) Flush(ctx context.Context, saver func([]*scanner.Vulnerability)) {
	b.mu.Lock()
	vuls := b.vuls
	b.vuls = nil
	b.mu.Unlock()

	if len(vuls) > 0 {
		saver(vuls) // 批量保存
	}
}

// NewWorker 创建Worker
func NewWorker(config WorkerConfig) (*Worker, error) {
	// 自动获取本机IP地址
	if config.IP == "" {
		config.IP = GetLocalIP()
	}

	// 创建 HTTP 客户端（替代 RPC 和 Redis）
	httpClient := NewWorkerHTTPClient(config.ServerAddr, config.InstallKey, config.Name)

	fmt.Printf("[Worker] HTTP client created, API server: %s\n", config.ServerAddr)

	// 创建可取消的Context
	ctx, cancel := context.WithCancel(context.Background())

	// Worker版本号
	workerVersion := "1.0.0"

	w := &Worker{
		ctx:              ctx,
		cancel:           cancel,
		config:           config,
		httpClient:       httpClient,
		scanners:         make(map[string]scanner.Scanner),
		taskChan:         make(chan *scheduler.TaskInfo, config.Concurrency),
		resultChan:       make(chan *scanner.ScanResult, 100),
		stopChan:         make(chan struct{}),
		logger:           NewWorkerLoggerLocal(config.Name), // 使用本地日志
		sysInfoCollector: NewSysInfoCollector(config.Name, config.IP, workerVersion),
	}

	// 创建 WebSocket 客户端
	wsConfig := DefaultWSClientConfig(config.ServerAddr, config.Name, config.InstallKey)
	w.wsClient = NewWorkerWSClient(wsConfig)

	// 更新 logger 为 WebSocket 版本，将日志发送到服务器
	w.logger = NewWorkerLoggerWS(config.Name, w.wsClient)

	// 设置控制信号处理函数
	w.wsClient.SetControlHandler(func(taskId, action string) {
		w.handleControlSignal(taskId, action)
	})

	// 设置 Worker 级别控制处理函数
	w.wsClient.SetWorkerControlHandler(func(action, param string) {
		w.handleWorkerControl(action, param)
	})

	// 设置Worker信息请求处理函数
	w.wsClient.SetWorkerInfoHandler(func() *WorkerInfoPayload {
		return w.GetWorkerInfo()
	})

	// 创建文件管理器并设置到WebSocket客户端
	w.fileManager = NewFileManager(nil) // 使用默认配置
	w.wsClient.SetFileHandler(w.fileManager)

	// 创建终端处理器并设置到WebSocket客户端
	w.terminalHandler = NewTerminalHandler(nil) // 使用默认配置
	w.wsClient.SetTerminalHandler(w.terminalHandler)

	// 设置终端输出回调，将输出发送到WebSocket
	w.terminalHandler.SetOutputHandler(func(sessionId string, data []byte) {
		w.wsClient.SendTerminalOutput(sessionId, data)
	})

	// 创建资源管理器
	w.resourceManager = NewResourceManager(config.Concurrency)

	// 注册扫描器
	w.registerScanners()

	// 创建任务执行器集成（在扫描器注册后）
	w.taskRunnerIntegration = NewTaskRunnerIntegration(w)
	w.taskRunnerIntegration.RegisterDefaultExecutors()

	// 加载HTTP服务映射配置
	w.loadHttpServiceMappings()

	return w, nil
}

// handleControlSignal 处理控制信号
func (w *Worker) handleControlSignal(taskId, action string) {
	w.logger.Info("Received control signal: taskId=%s, action=%s", taskId, action)

	// 存储控制信号
	w.taskControlSignals.Store(taskId, action)
	w.logger.Info("Stored control signal for task %s: %s", taskId, action)

	// 如果是STOP或PAUSE信号，也存储到主任务ID
	mainTaskId := getMainTaskId(taskId)
	if mainTaskId != taskId {
		w.taskControlSignals.Store(mainTaskId, action)
		w.logger.Info("Also stored control signal for main task %s: %s", mainTaskId, action)
	}
}

// handleWorkerControl 处理 Worker 级别控制命令
func (w *Worker) handleWorkerControl(action, param string) {
	w.logger.Info("Received worker control: action=%s, param=%s", action, param)

	switch action {
	case "stop":
		w.logger.Info("Stopping worker via WebSocket command...")
		// 在新 goroutine 中执行停止，避免死锁（因为当前在 WebSocket 读取 goroutine 中）
		go func() {
			w.StopImmediate()
			os.Exit(0)
		}()
	case "restart":
		w.logger.Info("Restarting worker via WebSocket command...")
		// 在新 goroutine 中执行重启
		go func() {
			w.StopImmediate()
			w.restartSelf()
		}()
	case "rename":
		w.logger.Info("Renaming worker to: %s", param)
		w.config.Name = param
		// 更新日志前缀（使用 WebSocket 版本）
		w.logger = NewWorkerLoggerWS(param, w.wsClient)
		// 立即发送心跳，让服务端更新状态
		go w.sendHeartbeat()
	case "setConcurrency":
		newConcurrency, err := strconv.Atoi(param)
		if err != nil || newConcurrency < 1 {
			w.logger.Error("Invalid concurrency value: %s", param)
			return
		}
		w.logger.Info("Setting concurrency to: %d", newConcurrency)
		w.config.Concurrency = newConcurrency
		// 更新资源管理器的最大并发数
		if w.resourceManager != nil {
			w.resourceManager.SetMaxConcurrency(newConcurrency)
		}
		// 注意：增加并发数需要重启才能生效，减少并发数会在任务完成后自然生效
		// 立即发送心跳，让服务端更新状态
		go w.sendHeartbeat()
	default:
		w.logger.Warn("Unknown worker control action: %s", action)
	}
}

// restartSelf 重新执行自身
func (w *Worker) restartSelf() {
	// 获取当前可执行文件路径
	executable, err := os.Executable()
	if err != nil {
		w.logger.Error("Failed to get executable path: %v", err)
		os.Exit(1)
	}

	// 获取命令行参数
	args := os.Args

	w.logger.Info("Restarting worker: %s %v", executable, args[1:])

	// 等待一小段时间确保资源释放
	time.Sleep(500 * time.Millisecond)

	// 使用平台特定的重启方式
	platformRestart(executable, args, w.logger)
}

// ClearTaskControlSignal 清除任务控制信号（任务完成后调用）
func (w *Worker) ClearTaskControlSignal(taskId string) {
	w.taskControlSignals.Delete(taskId)
	mainTaskId := getMainTaskId(taskId)
	if mainTaskId != taskId {
		w.taskControlSignals.Delete(mainTaskId)
	}
}

// GetWorkerInfo 获取Worker详细信息
func (w *Worker) GetWorkerInfo() *WorkerInfoPayload {
	w.mu.Lock()
	taskStarted := w.taskStarted
	taskExecuted := w.taskExecuted
	concurrency := w.config.Concurrency
	w.mu.Unlock()

	// 计算正在运行的任务数
	taskRunning := taskStarted - taskExecuted
	if taskRunning < 0 {
		taskRunning = 0
	}

	return w.sysInfoCollector.Collect(taskStarted, taskRunning, concurrency)
}

// registerScanners 注册扫描器
func (w *Worker) registerScanners() {
	w.scanners["portscan"] = scanner.NewPortScanner()
	w.scanners["masscan"] = scanner.NewMasscanScanner()
	w.scanners["nmap"] = scanner.NewNmapScanner()
	w.scanners["naabu"] = scanner.NewNaabuScanner()
	w.scanners["subfinder"] = scanner.NewSubfinderScanner()
	w.scanners["subdomain_bruteforce"] = scanner.NewSubdomainBruteforceScanner()
	w.scanners["fingerprint"] = scanner.NewFingerprintScanner()
	w.scanners["nuclei"] = scanner.NewNucleiScanner()
	w.scanners["urlfinder"] = scanner.NewURLFinderScanner()
}

// Start 启动Worker
func (w *Worker) Start() {
	w.isRunning = true

	// 启动 WebSocket 客户端（用于日志推送和控制信号）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("WebSocket client goroutine panic recovered: %v", r)
			}
		}()
		if err := w.wsClient.Start(w.ctx); err != nil {
			w.logger.Warn("WebSocket client failed to start: %v, falling back to HTTP polling", err)
		} else {
			w.logger.Info("WebSocket client started")
		}
	}()

	// 等待 WebSocket 连接成功（最多等待 5 秒）
	for i := 0; i < 50; i++ {
		if w.wsClient.IsConnected() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 启动任务处理协程
	for i := 0; i < w.config.Concurrency; i++ {
		w.wg.Add(1)
		go w.processTaskWithRecovery(i)
	}

	// 启动任务拉取协程
	w.wg.Add(1)
	go w.fetchTasksWithRecovery()

	// 启动结果上报协程
	w.wg.Add(1)
	go w.reportResultWithRecovery()

	// 启动心跳协程
	w.wg.Add(1)
	go w.keepAliveWithRecovery()

	// 启动 HTTP 轮询回退（当 WebSocket 不可用时）
	w.wg.Add(1)
	go w.controlPollingWithRecovery()

	w.logger.Info("Worker %s started with %d workers", w.config.Name, w.config.Concurrency)
}

// processTaskWithRecovery 带 panic 恢复的任务处理
func (w *Worker) processTaskWithRecovery(workerId int) {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopChan:
			return
		default:
		}
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("Task processor %d panic recovered: %v, stack: %s", workerId, r, string(getStackTrace()))
				}
			}()
			w.processTaskLoop()
		}()
		
		// 如果 processTaskLoop 正常返回（stopChan 关闭），退出
		select {
		case <-w.stopChan:
			return
		default:
			// panic 恢复后短暂等待再重启
			time.Sleep(time.Second)
			w.logger.Info("Task processor %d restarting after recovery", workerId)
		}
	}
}

// processTaskLoop 任务处理循环（内部方法）
func (w *Worker) processTaskLoop() {
	for {
		select {
		case <-w.stopChan:
			return
		case task := <-w.taskChan:
			// 在执行前检查任务是否已被停止
			ctx := context.Background()
			if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
				w.taskLog(task.TaskId, LevelInfo, "Task %s skipped because it was stopped while waiting in queue", task.TaskId)
				continue
			}
			w.executeTask(task)
		}
	}
}

// fetchTasksWithRecovery 带 panic 恢复的任务拉取
func (w *Worker) fetchTasksWithRecovery() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopChan:
			return
		default:
		}
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("Task fetcher panic recovered: %v", r)
				}
			}()
			w.fetchTasksLoop()
		}()
		
		select {
		case <-w.stopChan:
			return
		default:
			time.Sleep(time.Second)
			w.logger.Info("Task fetcher restarting after recovery")
		}
	}
}

// fetchTasksLoop 任务拉取循环（内部方法）
func (w *Worker) fetchTasksLoop() {
	emptyCount := 0
	baseInterval := 500 * time.Millisecond
	maxInterval := 2 * time.Second

	for {
		select {
		case <-w.stopChan:
			return
		default:
			hasTask := w.pullTask()
			if hasTask {
				emptyCount = 0
				time.Sleep(50 * time.Millisecond)
			} else {
				emptyCount++
				interval := baseInterval * time.Duration(emptyCount)
				if interval > maxInterval {
					interval = maxInterval
				}
				time.Sleep(interval)
			}
		}
	}
}

// reportResultWithRecovery 带 panic 恢复的结果上报
func (w *Worker) reportResultWithRecovery() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopChan:
			return
		default:
		}
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("Result reporter panic recovered: %v", r)
				}
			}()
			w.reportResultLoop()
		}()
		
		select {
		case <-w.stopChan:
			return
		default:
			time.Sleep(time.Second)
			w.logger.Info("Result reporter restarting after recovery")
		}
	}
}

// keepAliveWithRecovery 带 panic 恢复的心跳
func (w *Worker) keepAliveWithRecovery() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopChan:
			return
		default:
		}
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("Keepalive panic recovered: %v", r)
				}
			}()
			w.keepAliveLoop()
		}()
		
		select {
		case <-w.stopChan:
			return
		default:
			time.Sleep(time.Second)
			w.logger.Info("Keepalive restarting after recovery")
		}
	}
}

// controlPollingWithRecovery 带 panic 恢复的控制轮询
func (w *Worker) controlPollingWithRecovery() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopChan:
			return
		default:
		}
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("Control polling panic recovered: %v", r)
				}
			}()
			w.controlPollingLoop()
		}()
		
		select {
		case <-w.stopChan:
			return
		default:
			time.Sleep(time.Second)
			w.logger.Info("Control polling restarting after recovery")
		}
	}
}

// getStackTrace 获取堆栈跟踪
func getStackTrace() []byte {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return buf[:n]
}

// pullTask 拉取单个任务，返回是否获取到任务
func (w *Worker) pullTask() bool {
	ctx := context.Background()

	// 检查是否有空闲槽位
	if len(w.taskChan) >= w.config.Concurrency {
		return false
	}

	// 使用资源管理器检查是否可以接受新任务
	// 这会综合检查并发槽位和系统资源（CPU/内存）
	if w.resourceManager != nil && !w.resourceManager.CanAcceptTask() {
		return false
	}

	// 保留原有的CPU过载检查作为备用（当resourceManager为nil时）
	if w.resourceManager == nil && w.isCPUOverloaded() {
		return false
	}

	// 通过 HTTP 接口获取任务
	resp, err := w.httpClient.CheckTask(ctx)
	if err != nil {
		w.logger.Debug("pullTask: CheckTask failed: %v", err)
		return false
	}

	if resp.IsExist && !resp.IsFinished {
		// 有待执行的任务
		w.logger.Info("pullTask: got task %s (main: %s)", resp.TaskId, resp.MainTaskId)
		task := &scheduler.TaskInfo{
			TaskId:      resp.TaskId,
			MainTaskId:  resp.MainTaskId,
			WorkspaceId: resp.WorkspaceId,
			TaskName:    "scan",
			Config:      resp.Config,
		}
		w.taskChan <- task
		return true
	}
	return false
}

// Stop 停止Worker
func (w *Worker) Stop() {
	w.isRunning = false

	// 通知服务器Worker即将离线，删除Redis状态数据
	w.notifyOffline()

	w.cancel() // 通知所有 goroutine 停止
	close(w.stopChan)

	// 关闭 WebSocket 客户端
	if w.wsClient != nil {
		w.wsClient.Close()
	}

	w.wg.Wait()
	w.logger.Info("Worker %s stopped", w.config.Name)
}

// StopImmediate 立即停止Worker（跳过当前任务，不等待完成）
func (w *Worker) StopImmediate() {
	w.isRunning = false

	// 通知服务器Worker即将离线，删除Redis状态数据
	w.notifyOffline()

	w.cancel() // 通知所有 goroutine 停止
	close(w.stopChan)

	// 关闭 WebSocket 客户端
	if w.wsClient != nil {
		w.wsClient.Close()
	}

	// 不等待 wg.Wait()，立即返回，跳过当前正在执行的任务
	w.logger.Info("Worker %s stopped immediately (tasks skipped)", w.config.Name)
}

// notifyOffline 通知服务器Worker即将离线
func (w *Worker) notifyOffline() {
	if w.httpClient == nil {
		return
	}

	// 使用独立的context，不受w.ctx取消影响
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := w.httpClient.NotifyOffline(ctx)
	if err != nil {
		w.logger.Warn("Failed to notify server about offline: %v", err)
	} else {
		w.logger.Info("Notified server about offline")
	}
}

// SubmitTask 提交任务
func (w *Worker) SubmitTask(task *scheduler.TaskInfo) {
	w.taskChan <- task
}

// checkTaskControl 检查任务控制信号
// 返回: "PAUSE" - 暂停, "STOP" - 停止, "" - 继续执行
func (w *Worker) checkTaskControl(ctx context.Context, taskId string) string {
	// 从控制信号映射中检查
	if signal, ok := w.taskControlSignals.Load(taskId); ok {
		if action, ok := signal.(string); ok {
			return action
		}
	}

	// 也检查主任务ID的控制信号
	mainTaskId := getMainTaskId(taskId)
	if mainTaskId != taskId {
		if signal, ok := w.taskControlSignals.Load(mainTaskId); ok {
			if action, ok := signal.(string); ok {
				return action
			}
		}
	}

	return ""
}

// shouldStopTask 检查任务是否应该停止（包括 STOP 和 PAUSE）
func (w *Worker) shouldStopTask(ctx context.Context, taskId string) bool {
	ctrl := w.checkTaskControl(ctx, taskId)
	return ctrl == "STOP" || ctrl == "PAUSE" || ctx.Err() != nil
}

// saveTaskProgress 保存任务进度（用于暂停后继续扫描)
func (w *Worker) saveTaskProgress(ctx context.Context, task *scheduler.TaskInfo, completedPhases map[string]bool, assets []*scanner.Asset) {
	// 使用新的context，因为原context可能已被取消
	saveCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 构建状态
	phases := make([]string, 0)
	for phase, completed := range completedPhases {
		if completed {
			phases = append(phases, phase)
		}
	}

	assetsJson, _ := json.Marshal(assets)
	state := map[string]interface{}{
		"completedPhases": phases,
		"assets":          string(assetsJson),
	}
	stateJson, _ := json.Marshal(state)

	// 通过 HTTP 接口保存到数据库
	w.httpClient.UpdateTask(saveCtx, &TaskUpdateReq{
		TaskId: task.TaskId,
		State:  "PAUSED",
		Result: string(stateJson),
	})
	w.taskLog(task.TaskId, LevelInfo, "Task %s progress saved: completedPhases=%v, assets=%d", task.TaskId, phases, len(assets))
}

// createTaskContext 创建带有任务控制信号检查的上下文
// 当任务被停止或暂停时，上下文会被取消
func (w *Worker) createTaskContext(parentCtx context.Context, taskId string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parentCtx)

	// 启动一个goroutine定期检查任务控制信号
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond) // 检查间隔200ms
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ctrl := w.checkTaskControl(ctx, taskId)
				if ctrl == "STOP" || ctrl == "PAUSE" {
					w.taskLog(taskId, LevelInfo, "Task %s received %s signal, cancelling context", taskId, ctrl)
					cancel()
					return
				}
			}
		}
	}()

	return ctx, cancel
}

// executeTask 执行任务
func (w *Worker) executeTask(task *scheduler.TaskInfo) {
	baseCtx := context.Background()
	startTime := time.Now()

	// 获取资源槽位
	if w.resourceManager != nil {
		w.resourceManager.AcquireSlot()
	}

	w.mu.Lock()
	w.taskStarted++
	w.mu.Unlock()

	// 注册正在执行的任务
	w.runningTasks.Store(task.TaskId, true)
	mainTaskId := getMainTaskId(task.TaskId)
	if mainTaskId != task.TaskId {
		w.runningTasks.Store(mainTaskId, true)
	}

	// 使用 defer 确保无论任务如何结束，taskExecuted 都会递增
	// 这样 runningCount (taskStarted - taskExecuted) 才能正确反映正在执行的任务数
	defer func() {
		w.mu.Lock()
		w.taskExecuted++
		w.mu.Unlock()

		// 注销正在执行的任务
		w.runningTasks.Delete(task.TaskId)
		if mainTaskId != task.TaskId {
			w.runningTasks.Delete(mainTaskId)
		}

		// 清除控制信号
		w.ClearTaskControlSignal(task.TaskId)

		// 释放资源槽位
		if w.resourceManager != nil {
			w.resourceManager.ReleaseSlot()
		}
	}()

	// 检查是否有停止信号（任务可能在队列中被停止)
	if ctrl := w.checkTaskControl(baseCtx, task.TaskId); ctrl == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task %s was stopped before execution", task.TaskId)
		return
	}

	// 创建带有任务控制信号检查的上下文
	ctx, cancelTask := w.createTaskContext(baseCtx, task.TaskId)
	defer cancelTask()

	// 更新任务状态
	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusStarted, "")

	// 解析任务配置
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "配置解析失败: "+err.Error())
		return
	}

	// 检查任务类型，处理POC验证任务
	taskType, _ := taskConfig["taskType"].(string)
	if taskType == "poc_validate" {
		w.executePocValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "poc_batch_validate" {
		w.executePocBatchValidateTask(ctx, task, taskConfig, startTime)
		return
	}

	// 获取目标
	target, _ := taskConfig["target"].(string)
	if target == "" {
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "Target is empty")
		return
	}

	// 获取组织ID
	orgId, _ := taskConfig["orgId"].(string)
	w.taskLog(task.TaskId, LevelInfo, "OrgId from config: %s", orgId)

	var allAssets []*scanner.Asset
	var allVuls []*scanner.Vulnerability

	// 解析扫描配置
	config, _ := scheduler.ParseTaskConfig(task.Config)
	if config == nil {
		config = &scheduler.TaskConfig{
			PortScan: &scheduler.PortScanConfig{Enable: true, Ports: "80,443,8080"},
		}
	}

	// 输出任务开始日志（包含关键配置信息）
	var enabledPhases []string
	if config.DomainScan != nil && config.DomainScan.Enable {
		enabledPhases = append(enabledPhases, "Domain Scan")
	}
	if config.PortScan != nil && config.PortScan.Enable {
		enabledPhases = append(enabledPhases, "Port Scan")
	}
	if config.PortIdentify != nil && config.PortIdentify.Enable {
		enabledPhases = append(enabledPhases, "Port Identify")
	}
	if config.Fingerprint != nil && config.Fingerprint.Enable {
		enabledPhases = append(enabledPhases, "Fingerprint")
	}
	if config.DirScan != nil && config.DirScan.Enable {
		enabledPhases = append(enabledPhases, "Dir Scan")
	}
	if config.PocScan != nil && config.PocScan.Enable {
		enabledPhases = append(enabledPhases, "POC Scan")
	}

	// 解析目标列表
	targetLines := strings.Split(strings.TrimSpace(target), "\n")
	var targets []string
	for _, line := range targetLines {
		line = strings.TrimSpace(line)
		if line != "" {
			targets = append(targets, line)
		}
	}

	// 输出任务开始日志
	w.taskLog(task.TaskId, LevelInfo, "Starting: %s", strings.Join(enabledPhases, " → "))
	w.taskLog(task.TaskId, LevelInfo, "Targets (%d): %s", len(targets), strings.Join(targets, ", "))

	// 解析恢复状态（如果是继续执行的任务）
	var resumeState map[string]interface{}
	if stateStr, ok := taskConfig["resumeState"].(string); ok && stateStr != "" {
		json.Unmarshal([]byte(stateStr), &resumeState)
		w.taskLog(task.TaskId, LevelInfo, "Resuming from saved state")
	}
	completedPhases := make(map[string]bool)
	if resumeState != nil {
		if phases, ok := resumeState["completedPhases"].([]interface{}); ok {
			for _, p := range phases {
				if ps, ok := p.(string); ok {
					completedPhases[ps] = true
				}
			}
		}
		// 恢复已扫描的资产
		if assetsJson, ok := resumeState["assets"].(string); ok && assetsJson != "" {
			json.Unmarshal([]byte(assetsJson), &allAssets)
			w.taskLog(task.TaskId, LevelInfo, "Restored %d assets", len(allAssets))
		}
	}

	// 当端口扫描禁用时，需要从目标生成初始资产列表
	// 支持 IP:Port 格式的目标，用于资产扫描场景
	if config.PortScan != nil && !config.PortScan.Enable && len(allAssets) == 0 {
		// 检查是否有其他阶段需要资产
		needAssets := (config.PortIdentify != nil && config.PortIdentify.Enable) ||
			(config.Fingerprint != nil && config.Fingerprint.Enable) ||
			(config.PocScan != nil && config.PocScan.Enable)

		if needAssets {
			generatedAssets := w.generateAssetsFromTarget(target, config.PortScan)
			if len(generatedAssets) > 0 {
				allAssets = generatedAssets
				w.taskLog(task.TaskId, LevelInfo, "Generated %d assets from target (port scan disabled)", len(allAssets))
			}
		}
	}

	// 执行子域名扫描（在端口扫描之前）
	if config.DomainScan != nil && config.DomainScan.Enable && !completedPhases["domainscan"] {
		// 检查控制信号
		if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "Task stopped")
			return
		}

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task.TaskId, 10, "子域名扫描中", "子域名扫描")
		w.taskLog(task.TaskId, LevelInfo, "Starting domain scan...")

		// 创建任务日志回调
		domainTaskLogger := func(level, format string, args ...interface{}) {
			w.taskLog(task.TaskId, level, format, args...)
		}

		// 通过 HTTP 接口获取 Subfinder 配置
		var providerConfig map[string][]string
		providerResp, err := w.httpClient.GetSubfinderProviders(ctx, task.WorkspaceId)
		if err != nil {
			w.taskLog(task.TaskId, LevelWarn, "Failed to get subfinder providers: %v", err)
		} else if providerResp != nil && len(providerResp.Providers) > 0 {
			providerConfig = make(map[string][]string)
			for _, p := range providerResp.Providers {
				if len(p.Keys) > 0 {
					providerConfig[p.Provider] = p.Keys
					w.taskLog(task.TaskId, LevelDebug, "Subfinder provider: %s, keys: %d", p.Provider, len(p.Keys))
				}
			}
			w.taskLog(task.TaskId, LevelInfo, "Loaded %d subfinder providers with keys", len(providerConfig))
		} else {
			w.taskLog(task.TaskId, LevelInfo, "No subfinder providers configured in database")
		}

		// 构建Subfinder选项，使用Worker并发数
		subfinderOpts := &scanner.SubfinderOptions{
			Timeout:            config.DomainScan.Timeout,
			MaxEnumerationTime: config.DomainScan.MaxEnumerationTime,
			Threads:            w.config.Concurrency, // 使用Worker并发数
			RateLimit:          config.DomainScan.RateLimit,
			Sources:            config.DomainScan.Sources,
			ExcludeSources:     config.DomainScan.ExcludeSources,
			All:                config.DomainScan.All,
			Recursive:          config.DomainScan.Recursive,
			RemoveWildcard:     config.DomainScan.RemoveWildcard,
			ResolveDNS:         config.DomainScan.ResolveDNS,
			Concurrent:         w.config.Concurrency * 10, // DNS解析并发数为Worker并发数的10倍
			ProviderConfig:     providerConfig,
		}

		// 设置默认值
		if subfinderOpts.Timeout <= 0 {
			subfinderOpts.Timeout = 30
		}
		if subfinderOpts.MaxEnumerationTime <= 0 {
			subfinderOpts.MaxEnumerationTime = 10
		}
		w.taskLog(task.TaskId, LevelInfo, "Subfinder using worker concurrency: threads=%d, dns_concurrent=%d", subfinderOpts.Threads, subfinderOpts.Concurrent)

		// 执行子域名扫描
		var subfinderAssets []*scanner.Asset
		// 只有启用Subfinder时才执行被动枚举
		if config.DomainScan.Subfinder {
			if s, ok := w.scanners["subfinder"]; ok {
				result, err := s.Scan(ctx, &scanner.ScanConfig{
					Target:      target,
					WorkspaceId: task.WorkspaceId,
					MainTaskId:  task.MainTaskId,
					Options:     subfinderOpts,
					TaskLogger:  domainTaskLogger,
				})

				if err != nil {
					w.taskLog(task.TaskId, LevelError, "Subfinder error: %v", err)
				} else if result != nil && len(result.Assets) > 0 {
					subfinderAssets = result.Assets
					w.taskLog(task.TaskId, LevelInfo, "Subfinder: found %d subdomains", len(subfinderAssets))
				}
			} else {
				w.taskLog(task.TaskId, LevelWarn, "Subfinder scanner not available")
			}
		} else {
			w.taskLog(task.TaskId, LevelInfo, "Subfinder disabled, skipping passive enumeration")
		}

		// 执行子域名暴力破解（如果配置了字典）
		var bruteforceAssets []*scanner.Asset
		if len(config.DomainScan.SubdomainDictIds) > 0 {
			w.taskLog(task.TaskId, LevelInfo, "Starting subdomain bruteforce with %d dicts", len(config.DomainScan.SubdomainDictIds))

			// 获取字典内容
			dictResp, err := w.httpClient.GetSubdomainDicts(ctx, config.DomainScan.SubdomainDictIds)
			if err != nil {
				w.taskLog(task.TaskId, LevelError, "Bruteforce: get dicts failed: %v", err)
			} else if dictResp != nil && len(dictResp.Dicts) > 0 {
				// 合并所有字典内容
				var allWords []string
				wordSet := make(map[string]bool)
				for _, dict := range dictResp.Dicts {
					lines := strings.Split(dict.Content, "\n")
					for _, line := range lines {
						word := strings.TrimSpace(line)
						if word != "" && !strings.HasPrefix(word, "#") && !wordSet[word] {
							wordSet[word] = true
							allWords = append(allWords, word)
						}
					}
					w.taskLog(task.TaskId, LevelInfo, "Bruteforce: loaded dict '%s'", dict.Name)
				}

				if len(allWords) > 0 {
					w.taskLog(task.TaskId, LevelInfo, "Bruteforce: total %d unique words", len(allWords))

					// 构建暴力破解选项（包含增强功能）
					bruteforceOpts := &scanner.SubdomainBruteforceOptions{
						Wordlist:       strings.Join(allWords, "\n"),
						Threads:        w.config.Concurrency * 2,
						Timeout:        5,
						WildcardFilter: config.DomainScan.RemoveWildcard,
						ResolveDNS:     config.DomainScan.ResolveDNS,
						Concurrent:     w.config.Concurrency * 10,
						// 增强功能配置
						RecursiveBrute: config.DomainScan.RecursiveBrute,
						RecursiveDepth: 2,
						WildcardDetect: config.DomainScan.WildcardDetect,
						SubdomainCrawl: config.DomainScan.SubdomainCrawl,
						TakeoverCheck:  config.DomainScan.TakeoverCheck,
					}

					// 获取递归爆破字典（如果启用了递归爆破）
					if config.DomainScan.RecursiveBrute && len(config.DomainScan.RecursiveDictIds) > 0 {
						recursiveDictResp, err := w.httpClient.GetSubdomainDicts(ctx, config.DomainScan.RecursiveDictIds)
						if err != nil {
							w.taskLog(task.TaskId, LevelWarn, "Bruteforce: get recursive dicts failed: %v", err)
						} else if recursiveDictResp != nil && len(recursiveDictResp.Dicts) > 0 {
							var recursiveWords []string
							recursiveWordSet := make(map[string]bool)
							for _, dict := range recursiveDictResp.Dicts {
								lines := strings.Split(dict.Content, "\n")
								for _, line := range lines {
									word := strings.TrimSpace(line)
									if word != "" && !strings.HasPrefix(word, "#") && !recursiveWordSet[word] {
										recursiveWordSet[word] = true
										recursiveWords = append(recursiveWords, word)
									}
								}
								w.taskLog(task.TaskId, LevelInfo, "Bruteforce: loaded recursive dict '%s'", dict.Name)
							}
							if len(recursiveWords) > 0 {
								bruteforceOpts.RecursiveWordlist = strings.Join(recursiveWords, "\n")
								w.taskLog(task.TaskId, LevelInfo, "Bruteforce: recursive wordlist total %d unique words", len(recursiveWords))
							}
						}
					}

					// 执行暴力破解
					if bruteScanner, ok := w.scanners["subdomain_bruteforce"]; ok {
						bruteResult, err := bruteScanner.Scan(ctx, &scanner.ScanConfig{
							Target:      target,
							WorkspaceId: task.WorkspaceId,
							MainTaskId:  task.MainTaskId,
							Options:     bruteforceOpts,
							TaskLogger:  domainTaskLogger,
						})

						if err != nil {
							w.taskLog(task.TaskId, LevelError, "Bruteforce error: %v", err)
						} else if bruteResult != nil && len(bruteResult.Assets) > 0 {
							bruteforceAssets = bruteResult.Assets
							w.taskLog(task.TaskId, LevelInfo, "Bruteforce: found %d subdomains", len(bruteforceAssets))
						}
					} else {
						w.taskLog(task.TaskId, LevelWarn, "Subdomain bruteforce scanner not available")
					}
				}
			}
		}

		// 合并subfinder和bruteforce结果
		allAssetMap := make(map[string]*scanner.Asset)
		for _, asset := range subfinderAssets {
			if asset.Host != "" {
				allAssetMap[asset.Host] = asset
			}
		}
		for _, asset := range bruteforceAssets {
			if asset.Host != "" {
				if _, exists := allAssetMap[asset.Host]; !exists {
					allAssetMap[asset.Host] = asset
				}
			}
		}

		// 转换为切片并保存
		var mergedAssets []*scanner.Asset
		for _, asset := range allAssetMap {
			mergedAssets = append(mergedAssets, asset)
		}

		// 检查是否被暂停或取消
		if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "Task stopped during domain scan")
			return
		} else if ctrl == "PAUSE" {
			w.taskLog(task.TaskId, LevelInfo, "Task paused during domain scan, saving progress...")
			// 保存已发现的子域名资产
			if len(mergedAssets) > 0 {
				allAssets = append(allAssets, mergedAssets...)
			}
			w.saveTaskProgress(ctx, task, completedPhases, allAssets)
			return
		}

		// 检查context是否被取消
		select {
		case <-ctx.Done():
			w.taskLog(task.TaskId, LevelInfo, "Domain scan cancelled by context")
			// 保存已发现的子域名资产
			if len(mergedAssets) > 0 {
				allAssets = append(allAssets, mergedAssets...)
			}
			w.saveTaskProgress(ctx, task, completedPhases, allAssets)
			return
		default:
		}

		if len(mergedAssets) > 0 {
			// 保存子域名扫描结果到数据库
			w.taskLog(task.TaskId, LevelInfo, "Saving %d subdomains to database", len(mergedAssets))
			w.saveAssetResult(ctx, task.WorkspaceId, task.MainTaskId, orgId, mergedAssets)

			// 将发现的子域名添加到目标列表
			var newTargets []string
			for _, asset := range mergedAssets {
				if asset.Host != "" {
					newTargets = append(newTargets, asset.Host)
				}
			}
			if len(newTargets) > 0 {
				// 更新目标（将子域名添加到原始目标）
				target = target + "\n" + strings.Join(newTargets, "\n")
				w.taskLog(task.TaskId, LevelInfo, "Domain scan completed: found %d subdomains (subfinder: %d, bruteforce: %d)",
					len(newTargets), len(subfinderAssets), len(bruteforceAssets))
			}
		}

		completedPhases["domainscan"] = true
		// 子域名扫描模块完成，递增子任务进度
		w.incrSubTaskDone(ctx, task, "子域名扫描")
	}

	// 执行端口扫描（只有明确启用时才执行）
	if config.PortScan != nil && config.PortScan.Enable && !completedPhases["portscan"] {
		// 检查控制信号
		if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "Task stopped")
			return
		} else if ctrl == "PAUSE" {
			w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
			w.saveTaskProgress(ctx, task, completedPhases, allAssets)
			return
		}

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task.TaskId, 20, "端口扫描中", "端口扫描")

		// 创建带超时的上下文，防止端口扫描卡死
		portScanTimeout := 600 // 默认10分钟总超时
		if config.PortScan != nil && config.PortScan.Timeout > 0 {
			// 根据单个端口超时计算总超时（至少10分钟）
			portScanTimeout = config.PortScan.Timeout * 100
			if portScanTimeout < 600 {
				portScanTimeout = 600
			}
		}
		portCtx, portCancel := context.WithTimeout(ctx, time.Duration(portScanTimeout)*time.Second)

		// 根据配置选择端口发现工具（默认使用Naabu)
		portDiscoveryTool := "naabu"
		if config.PortScan != nil && config.PortScan.Tool != "" {
			portDiscoveryTool = config.PortScan.Tool
		}

		var openPorts []*scanner.Asset

		// 创建任务日志回调
		taskLogger := func(level, format string, args ...interface{}) {
			w.taskLog(task.TaskId, level, format, args...)
		}

		// 创建进度回调
		onProgress := func(progress int, message string) {
			w.updateTaskProgress(ctx, task.TaskId, progress, message)
		}

		// 第一步：端口发现
		switch portDiscoveryTool {
		case "masscan":
			w.taskLog(task.TaskId, LevelInfo, "Port scan: Masscan")
			masscanScanner := w.scanners["masscan"]
			masscanResult, err := masscanScanner.Scan(portCtx, &scanner.ScanConfig{
				Target:     target,
				Options:    config.PortScan,
				TaskLogger: taskLogger,
				OnProgress: onProgress,
			})
			// 检查是否被停止或超时
			if portCtx.Err() == context.DeadlineExceeded {
				w.taskLog(task.TaskId, LevelWarn, "Port scan timeout, continuing with partial results")
			} else if ctx.Err() != nil {
				portCancel()
				w.taskLog(task.TaskId, LevelInfo, "Task stopped")
				return
			}
			if err != nil {
				w.taskLog(task.TaskId, LevelError, "Masscan error: %v", err)
			}
			if masscanResult != nil && len(masscanResult.Assets) > 0 {
				openPorts = masscanResult.Assets
				w.taskLog(task.TaskId, LevelInfo, "Found %d open ports", len(openPorts))
			}
		default: // naabu
			w.taskLog(task.TaskId, LevelInfo, "Port scan: Naabu")
			naabuScanner := w.scanners["naabu"]
			naabuResult, err := naabuScanner.Scan(portCtx, &scanner.ScanConfig{
				Target:     target,
				Options:    config.PortScan,
				TaskLogger: taskLogger,
				OnProgress: onProgress,
			})
			// 检查是否有目标超过端口阈值（不终止任务，只记录警告）
			if err == scanner.ErrPortThresholdExceeded {
				w.taskLog(task.TaskId, LevelWarn, "Some targets exceeded port threshold and were skipped")
			}
			// 检查是否被停止或超时
			if portCtx.Err() == context.DeadlineExceeded {
				w.taskLog(task.TaskId, LevelWarn, "Port scan timeout, continuing with partial results")
			} else if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
				portCancel()
				w.taskLog(task.TaskId, LevelInfo, "Task stopped")
				return
			}
			if err != nil && err != scanner.ErrPortThresholdExceeded {
				w.taskLog(task.TaskId, LevelError, "Naabu error: %v", err)
			}
			if naabuResult != nil && len(naabuResult.Assets) > 0 {
				openPorts = naabuResult.Assets
				w.taskLog(task.TaskId, LevelInfo, "Found %d open ports", len(openPorts))
			}
		}

		// 检查是否被停止
		if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
			portCancel()
			w.taskLog(task.TaskId, LevelInfo, "Task stopped")
			return
		}

		// 端口发现完成，将结果添加到 allAssets
		if len(openPorts) > 0 {
			for _, asset := range openPorts {
				asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
			}
			allAssets = append(allAssets, openPorts...)
			w.taskLog(task.TaskId, LevelInfo, "Port scan completed: %d assets", len(allAssets))

			// 端口扫描完成后立即保存结果
			w.saveAssetResult(ctx, task.WorkspaceId, task.MainTaskId, orgId, allAssets)
		} else {
			w.taskLog(task.TaskId, LevelInfo, "No open ports found")
		}

		portCancel() // 释放端口扫描上下文
		completedPhases["portscan"] = true
		// 端口扫描模块完成，递增子任务进度
		w.incrSubTaskDone(ctx, task, "端口扫描")
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task stopped")
		return
	} else if ctrl == "PAUSE" {
		w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
		w.saveTaskProgress(ctx, task, completedPhases, allAssets)
		return
	}

	// 执行端口识别（Nmap服务识别）- 独立阶段
	if config.PortIdentify != nil && config.PortIdentify.Enable && !completedPhases["portidentify"] {
		// 没有资产时跳过实际扫描，但仍需递增进度
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Port identify: skipped (no assets)")
			completedPhases["portidentify"] = true
			w.incrSubTaskDone(ctx, task, "端口识别")
		} else {
			// 检查控制信号
			if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
				w.taskLog(task.TaskId, LevelInfo, "Task stopped")
				return
			} else if ctrl == "PAUSE" {
				w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
				w.saveTaskProgress(ctx, task, completedPhases, allAssets)
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task.TaskId, 40, "端口识别中", "端口识别")

			identifiedAssets := w.executePortIdentify(ctx, task, allAssets, config.PortIdentify)
			if len(identifiedAssets) > 0 {
				allAssets = identifiedAssets
				// 端口识别完成后保存更新结果
				w.saveAssetResult(ctx, task.WorkspaceId, task.MainTaskId, orgId, allAssets)
			}
			completedPhases["portidentify"] = true
			// 端口识别模块完成，递增子任务进度
			w.incrSubTaskDone(ctx, task, "端口识别")
		}
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task stopped")
		return
	} else if ctrl == "PAUSE" {
		w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
		w.saveTaskProgress(ctx, task, completedPhases, allAssets)
		return
	}

	// 执行指纹识别
	if config.Fingerprint != nil && config.Fingerprint.Enable && !completedPhases["fingerprint"] {
		// 没有资产时跳过实际扫描，但仍需递增进度
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Fingerprint: skipped (no assets)")
			completedPhases["fingerprint"] = true
			w.incrSubTaskDone(ctx, task, "指纹识别")
		} else {
			// 在指纹识别开始前检查停止信号
			if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
				w.taskLog(task.TaskId, LevelInfo, "Task stopped")
				return
			} else if ctrl == "PAUSE" {
			w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
			w.saveTaskProgress(ctx, task, completedPhases, allAssets)
			return
		}

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task.TaskId, 60, "指纹识别中", "指纹识别")

		if s, ok := w.scanners["fingerprint"]; ok {
			// 获取单目标超时配置
			targetTimeout := config.Fingerprint.TargetTimeout
			if targetTimeout <= 0 {
				targetTimeout = 30 // 默认30秒
			}
			// 使用Worker并发数覆盖配置中的并发数
			config.Fingerprint.Concurrency = w.config.Concurrency
			w.taskLog(task.TaskId, LevelInfo, "Fingerprint: %d assets, timeout %ds/target, concurrency=%d, activeScan=%v", len(allAssets), targetTimeout, w.config.Concurrency, config.Fingerprint.ActiveScan)

			// 每次扫描前实时加载HTTP服务映射配置
			w.loadHttpServiceMappings()

			// 如果启用自定义指纹引擎，加载自定义指纹（包括主动指纹）
			if config.Fingerprint.CustomEngine {
				w.loadCustomFingerprints(ctx, s.(*scanner.FingerprintScanner), config.Fingerprint.ActiveScan)
			}

			// 创建带超时的上下文，防止指纹识别卡死
			fingerprintTimeout := config.Fingerprint.Timeout
			if fingerprintTimeout <= 0 {
				fingerprintTimeout = 300 // 默认5分钟总超时
			}
			fpCtx, fpCancel := context.WithTimeout(ctx, time.Duration(fingerprintTimeout)*time.Second)

			// 创建任务日志回调
			fpTaskLogger := func(level, format string, args ...interface{}) {
				w.taskLog(task.TaskId, level, format, args...)
			}

			result, err := s.Scan(fpCtx, &scanner.ScanConfig{
				Assets:     allAssets,
				Options:    config.Fingerprint,
				TaskLogger: fpTaskLogger,
			})
			fpCancel()

			// 检查是否超时
			if fpCtx.Err() == context.DeadlineExceeded {
				w.taskLog(task.TaskId, LevelWarn, "Fingerprint scan timeout after %ds, continuing with partial results", fingerprintTimeout)
			}

			// 检查是否被取消
			if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
				w.taskLog(task.TaskId, LevelInfo, "Task stopped")
				return
			}

			if err == nil && result != nil {
				// 构建 Host:Port -> Asset 的映射，用于匹配指纹结果
				assetMap := make(map[string]*scanner.Asset)
				for _, asset := range allAssets {
					key := fmt.Sprintf("%s:%d", asset.Host, asset.Port)
					assetMap[key] = asset
				}

				// 通过 Host:Port 匹配来更新资产信息，而不是按索引
				for _, fpAsset := range result.Assets {
					key := fmt.Sprintf("%s:%d", fpAsset.Host, fpAsset.Port)
					if originalAsset, ok := assetMap[key]; ok {
						originalAsset.Service = fpAsset.Service
						originalAsset.Title = fpAsset.Title
						originalAsset.App = fpAsset.App
						originalAsset.HttpStatus = fpAsset.HttpStatus
						originalAsset.HttpHeader = fpAsset.HttpHeader
						originalAsset.HttpBody = fpAsset.HttpBody
						originalAsset.Server = fpAsset.Server
						originalAsset.IconHash = fpAsset.IconHash
						originalAsset.Screenshot = fpAsset.Screenshot
					}
				}

				// 指纹识别完成后保存更新结果
				w.saveAssetResult(ctx, task.WorkspaceId, task.MainTaskId, orgId, allAssets)
			}
		}
		completedPhases["fingerprint"] = true
		// 指纹识别模块完成，递增子任务进度
		w.incrSubTaskDone(ctx, task, "指纹识别")
		} // 结束 len(allAssets) > 0 的 else 分支
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task stopped")
		return
	} else if ctrl == "PAUSE" {
		w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
		w.saveTaskProgress(ctx, task, completedPhases, allAssets)
		return
	}

	// 执行目录扫描（在指纹识别之后、POC扫描之前）
	if config.DirScan != nil && config.DirScan.Enable && !completedPhases["dirscan"] {
		// 如果没有资产，尝试从目标生成 HTTP 资产（用于只启用目录扫描的场景）
		if len(allAssets) == 0 {
			generatedAssets := w.generateHTTPAssetsFromTarget(target)
			if len(generatedAssets) > 0 {
				allAssets = generatedAssets
				w.taskLog(task.TaskId, LevelInfo, "Dir scan: generated %d HTTP assets from target", len(allAssets))
			}
		}

		// 仍然没有资产时跳过
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Dir scan: skipped (no assets)")
			completedPhases["dirscan"] = true
			w.incrSubTaskDone(ctx, task, "目录扫描")
		} else {
			// 检查控制信号
			if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
				w.taskLog(task.TaskId, LevelInfo, "Task stopped")
				return
			} else if ctrl == "PAUSE" {
				w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
				w.saveTaskProgress(ctx, task, completedPhases, allAssets)
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task.TaskId, 70, "目录扫描中", "目录扫描")

			// 执行目录扫描
			dirScanAssets := w.executeDirScan(ctx, task, allAssets, config.DirScan, orgId)
			if len(dirScanAssets) > 0 {
				// 将目录扫描发现的资产添加到总资产列表
				allAssets = append(allAssets, dirScanAssets...)
				w.taskLog(task.TaskId, LevelInfo, "Dir scan completed: found %d paths", len(dirScanAssets))
				// 保存目录扫描结果
				w.saveAssetResult(ctx, task.WorkspaceId, task.MainTaskId, orgId, dirScanAssets)
			}
			completedPhases["dirscan"] = true
			w.incrSubTaskDone(ctx, task, "目录扫描")
		}
	}

	// 检查控制信号
	if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task stopped")
		return
	} else if ctrl == "PAUSE" {
		w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
		w.saveTaskProgress(ctx, task, completedPhases, allAssets)
		return
	}

	// 执行POC扫描 (使用Nuclei引擎)
	if config.PocScan != nil && config.PocScan.Enable && !completedPhases["pocscan"] {
		// 没有资产时跳过实际扫描，但仍需递增进度
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "POC scan: skipped (no assets)")
			completedPhases["pocscan"] = true
			w.incrSubTaskDone(ctx, task, "漏洞扫描")
		} else {
			// 在POC扫描开始前检查停止信号
			if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
				w.taskLog(task.TaskId, LevelInfo, "Task stopped")
			return
		} else if ctrl == "PAUSE" {
			w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
			w.saveTaskProgress(ctx, task, completedPhases, allAssets)
			return
		}

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task.TaskId, 80, "漏洞扫描中", "漏洞扫描")

		if s, ok := w.scanners["nuclei"]; ok {
			// 获取单目标超时配置
			pocTargetTimeout := config.PocScan.TargetTimeout
			if pocTargetTimeout <= 0 {
				pocTargetTimeout = 600 // 默认600秒
			}
			w.taskLog(task.TaskId, LevelInfo, "POC scan: %d assets, timeout %ds/target", len(allAssets), pocTargetTimeout)

			// 从数据库获取模板（所有模板都存储在数据库中）
			var templates []string
			var autoTags []string

			// 检查是否有模板ID列表（任务创建时已筛选好的模板）
			if len(config.PocScan.NucleiTemplateIds) > 0 || len(config.PocScan.CustomPocIds) > 0 {
				// 通过RPC根据ID获取模板内容（包括默认模板和自定义POC)
				templates = w.getTemplatesByIds(ctx, config.PocScan.NucleiTemplateIds, config.PocScan.CustomPocIds)
				w.taskLog(task.TaskId, LevelInfo, "Loaded %d POC templates", len(templates))
			} else if config.PocScan.CustomPocOnly {
				// 只使用自定义POC模式，但没有指定具体ID，获取所有自定义POC
				severities := []string{}
				if config.PocScan.Severity != "" {
					severities = strings.Split(config.PocScan.Severity, ",")
				}
				templates = w.getAllCustomPocs(ctx, severities)
				w.taskLog(task.TaskId, LevelInfo, "CustomPocOnly mode: loaded %d custom POC templates", len(templates))
			} else {
				// 没有预设的模板ID，根据自动扫描配置生成标签并获取模板
				if config.PocScan.AutoScan || config.PocScan.AutomaticScan {
					autoTags = w.generateAutoTags(allAssets, config.PocScan)
				}

				if len(autoTags) > 0 {
					// 有自动生成的标签，通过RPC获取符合标签的模板
					severities := []string{}
					if config.PocScan.Severity != "" {
						severities = strings.Split(config.PocScan.Severity, ",")
					}
					templates = w.getTemplatesByTags(ctx, autoTags, severities)
					w.taskLog(task.TaskId, LevelInfo, "Loaded %d POC templates", len(templates))
				} else {
					// 没有模板ID也没有自动标签，记录警告
					w.taskLog(task.TaskId, LevelWarn, "No POC templates configured, skipping POC scan")
				}
			}

			// 只有在有模板时才执行扫描
			if len(templates) > 0 {
				// 用于统计漏洞数量
				var vulCount int

				// 创建漏洞缓冲区，发现漏洞立即保存
				vulBuffer := NewVulnerabilityBuffer(1)

				// 获取单目标超时配置
				targetTimeout := config.PocScan.TargetTimeout
				if targetTimeout <= 0 {
					targetTimeout = 600 // 默认600秒
				}

				// 总超时基于目标数量和单目标超时计算，至少10分钟
				pocTimeout := targetTimeout * len(allAssets)
				if pocTimeout < 600 {
					pocTimeout = 600
				}
				pocCtx, pocCancel := context.WithTimeout(ctx, time.Duration(pocTimeout)*time.Second)

				// 启动后台刷新协程
				flushDone := make(chan struct{})
				go func() {
					defer close(flushDone)
					ticker := time.NewTicker(5 * time.Second) // 5秒也刷新一次
					defer ticker.Stop()
					for {
						select {
						case <-pocCtx.Done():
							return
						case <-flushDone:
							return
						case <-vulBuffer.flushChan:
							vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
								w.saveVulResult(ctx, task.WorkspaceId, task.MainTaskId, vuls)
							})
						case <-ticker.C:
							vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
								w.saveVulResult(ctx, task.WorkspaceId, task.MainTaskId, vuls)
							})
						}
					}
				}()

				// 构建Nuclei扫描选项，设置回调函数批量保存漏洞
				taskIdForCallback := task.TaskId // 捕获taskId用于回调

				nucleiOpts := &scanner.NucleiOptions{
					Severity:        config.PocScan.Severity,
					Tags:            autoTags,
					ExcludeTags:     config.PocScan.ExcludeTags,
					RateLimit:       config.PocScan.RateLimit,
					Concurrency:     config.PocScan.Concurrency,
					Timeout:         pocTimeout,
					TargetTimeout:   targetTimeout,
					AutoScan:        false, // 标签已在Worker端生成
					AutomaticScan:   false,
					CustomPocOnly:   config.PocScan.CustomPocOnly,
					CustomTemplates: templates,
					TagMappings:     config.PocScan.TagMappings,
					// 设置回调函数，发现漏洞时添加到缓冲区
					OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
						vulCount++
						w.taskLog(taskIdForCallback, LevelInfo, "Vulnerability found: %s → %s", vul.PocFile, vul.Url)
						vulBuffer.Add(vul)
					},
				}
				// 设置默认
				if nucleiOpts.RateLimit == 0 {
					nucleiOpts.RateLimit = 150
				}
				if nucleiOpts.Concurrency == 0 {
					nucleiOpts.Concurrency = 25
				}

				// 创建任务日志回调
				pocTaskLogger := func(level, format string, args ...interface{}) {
					w.taskLog(task.TaskId, level, format, args...)
				}

				result, err := s.Scan(pocCtx, &scanner.ScanConfig{
					Assets:     allAssets,
					Options:    nucleiOpts,
					TaskLogger: pocTaskLogger,
				})
				pocCancel()

				// 扫描完成后，刷新剩余的漏洞
				vulBuffer.Flush(ctx, func(vuls []*scanner.Vulnerability) {
					w.saveVulResult(ctx, task.WorkspaceId, task.MainTaskId, vuls)
				})

				// 检查是否超时
				if pocCtx.Err() == context.DeadlineExceeded {
					w.taskLog(task.TaskId, LevelWarn, "POC scan timeout after %ds, continuing with partial results", pocTimeout)
				}

				// 检查是否被停止
				if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
					w.taskLog(task.TaskId, LevelInfo, "Task stopped")
					return
				}

				if err != nil {
					w.taskLog(task.TaskId, LevelError, "POC scan error: %v", err)
				}
				if result != nil {
					allVuls = append(allVuls, result.Vulnerabilities...)
					if vulCount > 0 {
						w.taskLog(task.TaskId, LevelInfo, "POC scan completed: found %d vulnerabilities", vulCount)
					}
				}
			}
		}
		// POC扫描模块完成，递增子任务进度
		w.incrSubTaskDone(ctx, task, "漏洞扫描")
		} // 结束 len(allAssets) > 0 的 else 分支
	}

	// 更新任务状态为完成
	duration := time.Since(startTime).Seconds()
	result := fmt.Sprintf("Assets:%d Vuls:%d Duration:%.0fs", len(allAssets), len(allVuls), duration)
	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusSuccess, result)
	w.taskLog(task.TaskId, LevelInfo, "Completed: %s", result)
	// 注意：taskExecuted 由 defer 递增，无需在此处理
}

// updateTaskStatus 更新任务状态
func (w *Worker) updateTaskStatus(ctx context.Context, taskId, status, result string) {
	// 如果任务完成（SUCCESS/FAILURE），同时更新进度
	if status == scheduler.TaskStatusSuccess || status == scheduler.TaskStatusFailure {
		progress := 100
		if status == scheduler.TaskStatusFailure {
			progress = 0 // 失败时进度设为0
		}
		w.updateTaskProgressWithPhase(ctx, taskId, progress, result, "完成")
	}

	// 通过 HTTP 接口更新任务状态
	_, err := w.httpClient.UpdateTask(ctx, &TaskUpdateReq{
		TaskId: taskId,
		State:  status,
		Worker: w.config.Name,
		Result: result,
	})
	if err != nil {
		w.taskLog(taskId, LevelError, "update task status failed: %v", err)
	}
}

// updateTaskProgress 更新任务进度
// 注意：进度更新现在通过 HTTP 接口完成
func (w *Worker) updateTaskProgress(ctx context.Context, taskId string, progress int, message string) {
	w.updateTaskProgressWithPhase(ctx, taskId, progress, message, "")
}

// updateTaskProgressWithPhase 更新任务进度和当前阶段
// 注意：进度更新现在通过 HTTP 接口完成，不再直接写 Redis
func (w *Worker) updateTaskProgressWithPhase(ctx context.Context, taskId string, progress int, message string, currentPhase string) {
	// 通过 HTTP 接口更新任务状态
	// 进度信息包含在任务状态更新中
	if w.httpClient != nil && currentPhase != "" {
		_, err := w.httpClient.UpdateTask(ctx, &TaskUpdateReq{
			TaskId:   taskId,
			Progress: progress,
			Phase:    currentPhase,
			Result:   message,
		})
		if err != nil {
			w.taskLog(taskId, LevelError, "update task progress failed: %v", err)
		}
	}
}

// incrSubTaskDone 递增子任务完成数（模块级别）
// 每完成一个扫描模块就调用此方法，通知主任务进度更新
func (w *Worker) incrSubTaskDone(ctx context.Context, task *scheduler.TaskInfo, phase string) {
	if w.httpClient == nil {
		return
	}

	// 通过 HTTP 接口递增子任务完成数
	resp, err := w.httpClient.IncrSubTaskDone(ctx, &SubTaskDoneReq{
		TaskId:      task.TaskId,
		MainTaskId:  task.MainTaskId,
		WorkspaceId: task.WorkspaceId,
		Phase:       phase,
	})
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Failed to incr sub task done: %v", err)
		return
	}

	if resp.AllDone {
		w.taskLog(task.TaskId, LevelInfo, "All sub-tasks completed: %d/%d", resp.SubTaskDone, resp.SubTaskCount)
	} else {
		w.taskLog(task.TaskId, LevelDebug, "Sub-task progress: %d/%d (phase: %s)", resp.SubTaskDone, resp.SubTaskCount, phase)
	}
}

// saveAssetResult 保存资产结果
func (w *Worker) saveAssetResult(ctx context.Context, workspaceId, mainTaskId, orgId string, assets []*scanner.Asset) {
	if len(assets) == 0 {
		return
	}

	// 分批保存，每批最多500个
	batchSize := 500
	totalAssets := len(assets)
	totalBatches := (totalAssets + batchSize - 1) / batchSize

	w.taskLog(mainTaskId, LevelInfo, "Saving %d assets in %d batches", totalAssets, totalBatches)

	var totalNew, totalUpdate int32

	for batchIdx := 0; batchIdx < totalBatches; batchIdx++ {
		start := batchIdx * batchSize
		end := start + batchSize
		if end > totalAssets {
			end = totalAssets
		}

		batchAssets := assets[start:end]
		httpAssets := make([]AssetDocument, 0, len(batchAssets))

		for _, asset := range batchAssets {
			httpAsset := AssetDocument{
				Authority:  asset.Authority,
				Host:       asset.Host,
				Port:       int32(asset.Port),
				Category:   asset.Category,
				Service:    asset.Service,
				Title:      asset.Title,
				App:        asset.App,
				HttpStatus: asset.HttpStatus,
				HttpHeader: asset.HttpHeader,
				HttpBody:   asset.HttpBody,
				IconHash:   asset.IconHash,
				IconData:   asset.IconData,
				Screenshot: asset.Screenshot,
				Server:     asset.Server,
				Banner:     asset.Banner,
				IsHttp:     asset.IsHTTP,
				Cname:      asset.CName,
				IsCdn:      asset.IsCDN,
				IsCloud:    asset.IsCloud,
				Source:     asset.Source,
			}

			// 添加IPv4信息
			for _, ip := range asset.IPV4 {
				httpAsset.Ipv4 = append(httpAsset.Ipv4, IPV4Info{
					IP:       ip.IP,
					Location: ip.Location,
				})
			}

			// 添加IPv6信息
			for _, ip := range asset.IPV6 {
				httpAsset.Ipv6 = append(httpAsset.Ipv6, IPV6Info{
					IP:       ip.IP,
					Location: ip.Location,
				})
			}

			httpAssets = append(httpAssets, httpAsset)
		}

		// 使用独立的超时上下文，每批30秒超时
		batchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err := w.httpClient.SaveTaskResult(batchCtx, &TaskResultReq{
			WorkspaceId: workspaceId,
			MainTaskId:  mainTaskId,
			Assets:      httpAssets,
			OrgId:       orgId,
		})
		cancel()

		if err != nil {
			w.taskLog(mainTaskId, LevelError, "Batch %d/%d save failed: %v", batchIdx+1, totalBatches, err)
		} else {
			totalNew += resp.NewAsset
			totalUpdate += resp.UpdateAsset
			w.taskLog(mainTaskId, LevelDebug, "Batch %d/%d saved: new=%d, update=%d", batchIdx+1, totalBatches, resp.NewAsset, resp.UpdateAsset)
		}
	}

	w.taskLog(mainTaskId, LevelInfo, "Save completed: total=%d, new=%d, update=%d", totalAssets, totalNew, totalUpdate)
}

// saveVulResult 保存漏洞结果（支持去重与聚合）
func (w *Worker) saveVulResult(ctx context.Context, workspaceId, mainTaskId string, vuls []*scanner.Vulnerability) {
	if len(vuls) == 0 {
		return
	}

	httpVuls := make([]VulDocument, 0, len(vuls))
	for _, vul := range vuls {
		// Debug: 打印证据链数据
		w.taskLog(mainTaskId, LevelDebug, "[SaveVul] PocFile=%s, CurlCommand len=%d, Request len=%d, Response len=%d",
			vul.PocFile, len(vul.CurlCommand), len(vul.Request), len(vul.Response))

		httpVul := VulDocument{
			Authority: vul.Authority,
			Host:      vul.Host,
			Port:      int32(vul.Port),
			Url:       vul.Url,
			PocFile:   vul.PocFile,
			Source:    vul.Source,
			Severity:  vul.Severity,
			Result:    vul.Result,
			TaskId:    mainTaskId, // 设置任务ID用于报告查询
		}

		// 漏洞知识库关联字段
		if vul.CvssScore > 0 {
			httpVul.CvssScore = &vul.CvssScore
		}
		if vul.CveId != "" {
			httpVul.CveId = &vul.CveId
		}
		if vul.CweId != "" {
			httpVul.CweId = &vul.CweId
		}
		if vul.Remediation != "" {
			httpVul.Remediation = &vul.Remediation
		}
		if len(vul.References) > 0 {
			httpVul.References = vul.References
		}

		// 证据链字段
		if vul.MatcherName != "" {
			matcherName := vul.MatcherName
			httpVul.MatcherName = &matcherName
		}
		if len(vul.ExtractedResults) > 0 {
			httpVul.ExtractedResults = vul.ExtractedResults
		}
		if vul.CurlCommand != "" {
			curlCommand := vul.CurlCommand
			httpVul.CurlCommand = &curlCommand
		}
		if vul.Request != "" {
			request := vul.Request
			httpVul.Request = &request
		}
		if vul.Response != "" {
			response := vul.Response
			httpVul.Response = &response
		}
		if vul.ResponseTruncated {
			responseTruncated := vul.ResponseTruncated
			httpVul.ResponseTruncated = &responseTruncated
		}

		// 输出httpVul中的证据字段
		w.taskLog(mainTaskId, LevelDebug, "[SaveVul] httpVul.CurlCommand=%v, httpVul.Request=%v, httpVul.Response=%v",
			httpVul.CurlCommand != nil, httpVul.Request != nil, httpVul.Response != nil)

		httpVuls = append(httpVuls, httpVul)
	}

	// 通过 HTTP 接口保存漏洞结果
	_, err := w.httpClient.SaveVulResult(ctx, &VulResultReq{
		WorkspaceId: workspaceId,
		MainTaskId:  mainTaskId,
		Vuls:        httpVuls,
	})
	if err != nil {
		w.taskLog(mainTaskId, LevelError, "save vul result failed: %v", err)
	}
}

// reportResultLoop 上报结果循环（内部方法）
func (w *Worker) reportResultLoop() {
	for {
		select {
		case <-w.stopChan:
			return
		case result := <-w.resultChan:
			w.handleResult(result)
		}
	}
}

// handleResult 处理结果
func (w *Worker) handleResult(result *scanner.ScanResult) {
	ctx := context.Background()
	w.saveAssetResult(ctx, result.WorkspaceId, result.MainTaskId, "", result.Assets)
	w.saveVulResult(ctx, result.WorkspaceId, result.MainTaskId, result.Vulnerabilities)
}

// keepAliveLoop 心跳循环（内部方法）
// 心跳使用独立协程，不受扫描任务阻塞影响
func (w *Worker) keepAliveLoop() {
	// 启动时立即发送一次心跳
	w.sendHeartbeat()

	// 心跳间隔 30 秒，但允许 3 次失败（服务端应设置 90-120 秒超时判定离线）
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0
	maxFailures := 3

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			// 心跳发送使用独立 context，不受主 context 影响
			if err := w.sendHeartbeatWithRetry(); err != nil {
				consecutiveFailures++
				w.logger.Warn("Heartbeat failed (%d/%d): %v", consecutiveFailures, maxFailures, err)
				
				if consecutiveFailures >= maxFailures {
					w.logger.Error("Heartbeat failed %d times consecutively, worker may be marked offline", maxFailures)
					// 不主动退出，继续尝试，让服务端决定是否标记离线
				}
			} else {
				if consecutiveFailures > 0 {
					w.logger.Info("Heartbeat recovered after %d failures", consecutiveFailures)
				}
				consecutiveFailures = 0
			}
		}
	}
}

// sendHeartbeatWithRetry 带重试的心跳发送
func (w *Worker) sendHeartbeatWithRetry() error {
	var lastErr error
	for i := 0; i < 2; i++ { // 最多重试 1 次
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := w.doSendHeartbeat(ctx)
		cancel()
		
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// doSendHeartbeat 执行心跳发送
func (w *Worker) doSendHeartbeat(ctx context.Context) error {
	// 获取系统资源使用情况（快速采样，避免阻塞）
	cpuPercent, _ := cpu.Percent(0, false) // 使用 0 表示不等待，返回上次采样值
	memInfo, _ := mem.VirtualMemory()

	cpuLoad := 0.0
	if len(cpuPercent) > 0 {
		cpuLoad = cpuPercent[0]
	}
	memUsed := 0.0
	if memInfo != nil {
		memUsed = memInfo.UsedPercent
	}

	// 确保数值有效
	if cpuLoad < 0 || cpuLoad > 100 {
		cpuLoad = 0.0
	}
	if memUsed < 0 || memUsed > 100 {
		memUsed = 0.0
	}

	// 计算正在执行的任务数
	w.mu.Lock()
	runningTasks := w.taskStarted - w.taskExecuted
	if runningTasks < 0 {
		runningTasks = 0
	}
	w.mu.Unlock()

	// 通过 HTTP 接口发送心跳
	resp, err := w.httpClient.Heartbeat(ctx, &HeartbeatReq{
		WorkerName:         w.config.Name,
		IP:                 w.config.IP,
		CpuLoad:            cpuLoad,
		MemUsed:            memUsed,
		TaskStartedNumber:  int32(w.taskStarted),
		TaskExecutedNumber: int32(w.taskExecuted),
		IsDaemon:           false,
		Concurrency:        w.config.Concurrency,
	})

	if err != nil {
		return err
	}

	// 处理控制指令
	if resp.ManualStopFlag {
		w.logger.Info("received stop signal, stopping worker...")
		go func() {
			w.Stop()
			os.Exit(0)
		}()
	}
	if resp.ManualReloadFlag {
		w.logger.Info("received reload/restart signal, restarting worker...")
		go func() {
			w.Stop()
			os.Exit(0)
		}()
	}
	
	return nil
}

// sendHeartbeat 发送心跳（简单包装，用于外部调用）
func (w *Worker) sendHeartbeat() {
	_ = w.sendHeartbeatWithRetry()
}

// controlPollingLoop HTTP轮询控制信号循环（内部方法，作为WebSocket的备份方案）
func (w *Worker) controlPollingLoop() {
	ticker := time.NewTicker(2 * time.Second) // 每2秒轮询一次
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			// 获取当前正在执行的任务ID列表
			taskIds := w.getRunningTaskIds()
			if len(taskIds) == 0 {
				continue
			}

			// 通过HTTP轮询获取控制信号（始终执行，作为WebSocket的备份）
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			resp, err := w.httpClient.GetTaskControlSignals(ctx, taskIds)
			cancel()

			if err != nil {
				// 轮询失败，静默处理（避免日志刷屏）
				continue
			}

			// 处理控制信号
			for _, signal := range resp.Signals {
				w.handleControlSignal(signal.TaskId, signal.Action)
			}
		}
	}
}

// getRunningTaskIds 获取当前正在执行的任务ID列表
func (w *Worker) getRunningTaskIds() []string {
	var taskIds []string
	w.runningTasks.Range(func(key, value interface{}) bool {
		if taskId, ok := key.(string); ok {
			taskIds = append(taskIds, taskId)
		}
		return true
	})
	return taskIds
}

// CPU负载阈值常量
const (
	CPULoadThreshold     = 80.0 // CPU负载阈值，超过此值暂停任务拉取
	CPULoadRecovery      = 60.0 // CPU负载恢复阈值，低于此值恢复任务拉取
	CPUCheckInterval     = 5    // CPU检查间隔(秒)
	CPUOverloadThreshold = 3    // 连续过载次数阈值，超过则进入限流
	ThrottleDuration     = 30   // 限流持续时间(秒)
)

// isCPUOverloaded 检查CPU是否过载
// 当CPU负载超过80%时返回true，暂停任务下发以防止扫描引擎崩溃
// 实现智能限流：连续多次过载后进入限流状态，等待一段时间后自动恢复
func (w *Worker) isCPUOverloaded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 检查是否处于限流状态
	if w.isThrottled {
		if time.Now().Before(w.throttleUntil) {
			return true // 仍在限流期间
		}
		// 限流期结束，重置状态
		w.isThrottled = false
		w.cpuOverloadCount = 0
		w.logger.Info("CPU throttle period ended, resuming task fetch")
	}

	// 避免频繁检查CPU
	if time.Since(w.lastCPUCheck) < time.Duration(CPUCheckInterval)*time.Second {
		return false
	}
	w.lastCPUCheck = time.Now()

	// 快速获取CPU使用率（1秒采样）
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil || len(cpuPercent) == 0 {
		return false // 获取失败时不阻止任务
	}

	cpuLoad := cpuPercent[0]

	if cpuLoad >= CPULoadThreshold {
		w.cpuOverloadCount++
		w.logger.Warn("CPU load %.1f%% exceeds threshold %.1f%% (count: %d/%d)",
			cpuLoad, CPULoadThreshold, w.cpuOverloadCount, CPUOverloadThreshold)

		// 连续多次过载，进入限流状态
		if w.cpuOverloadCount >= CPUOverloadThreshold {
			w.isThrottled = true
			w.throttleUntil = time.Now().Add(time.Duration(ThrottleDuration) * time.Second)
			w.logger.Warn("Entering throttle mode for %d seconds to prevent engine crash", ThrottleDuration)
		}
		return true
	} else if cpuLoad < CPULoadRecovery {
		// CPU负载恢复正常，重置计数
		if w.cpuOverloadCount > 0 {
			w.cpuOverloadCount = 0
			w.logger.Info("CPU load %.1f%% recovered below %.1f%%, resetting overload count",
				cpuLoad, CPULoadRecovery)
		}
	}

	return false
}

// NOTE: subscribeStatusQuery 已移除，将在 Task 6 中通过 WebSocket 实现
// NOTE: reportStatusToRedis 已移除，将在 Task 6 中通过 WebSocket 实现

// GetWorkerName 获取Worker名称
func GetWorkerName() string {
	hostname, _ := os.Hostname()
	// 使用 hostname + pid + 随机后缀，确保唯一性
	return fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), randomSuffix(4))
}

// randomSuffix 生成随机后缀
func randomSuffix(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}

// NOTE: ensureUniqueWorkerName 已移除，将在 Task 6 中通过 WebSocket 实现

// GetLocalIP 获取本机IP地址
func GetLocalIP() string {
	// 1. 优先使用环境变量 WORKER_IP（适用于 Docker 等容器环境）
	if ip := os.Getenv("WORKER_IP"); ip != "" {
		return ip
	}

	// 2. 尝试通过 UDP 连接获取出口 IP（更可靠的方式）
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if localAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return localAddr.IP.String()
		}
	}

	// 3. 回退到遍历网络接口
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// GetSystemInfo 获取系统信息
func GetSystemInfo() map[string]interface{} {
	return map[string]interface{}{
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"cpus":     runtime.NumCPU(),
		"hostname": func() string { h, _ := os.Hostname(); return h }(),
		"ip":       GetLocalIP(),
	}
}

// generateAutoTags 根据资产的应用信息生成Nuclei标签
func (w *Worker) generateAutoTags(assets []*scanner.Asset, pocConfig *scheduler.PocScanConfig) []string {
	tagSet := make(map[string]bool)

	for _, asset := range assets {
		for _, app := range asset.App {
			appName := parseAppName(app)
			appNameLower := strings.ToLower(appName)

			// 模式1: 基于自定义标签映射
			if pocConfig.AutoScan && pocConfig.TagMappings != nil {
				for mappedApp, tags := range pocConfig.TagMappings {
					if strings.ToLower(mappedApp) == appNameLower {
						for _, tag := range tags {
							tagSet[tag] = true
						}
						break
					}
				}
			}

			// 模式2: 基于Wappalyzer内置映射
			if pocConfig.AutomaticScan {
				if tags, ok := mapping.WappalyzerNucleiMapping[appNameLower]; ok {
					for _, tag := range tags {
						tagSet[tag] = true
					}
				}
			}
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	return tags
}

// getTemplatesByTags 通过 HTTP 接口从数据库获取符合标签的模板
func (w *Worker) getTemplatesByTags(ctx context.Context, tags []string, severities []string) []string {
	if len(tags) == 0 {
		return nil
	}

	// 通过 HTTP 接口获取模板
	resp, err := w.httpClient.GetTemplates(ctx, &TemplatesReq{
		Tags:       tags,
		Severities: severities,
	})
	if err != nil {
		w.logger.Error("GetTemplates HTTP failed: %v", err)
		return nil
	}

	if !resp.Success {
		w.logger.Error("GetTemplates failed: %s", resp.Msg)
		return nil
	}

	w.logger.Info("GetTemplatesByTags: fetched %d templates for tags %v", resp.Count, tags)
	return resp.Templates
}

// getTemplatesByIds 通过 HTTP 接口根据ID列表获取模板内容
func (w *Worker) getTemplatesByIds(ctx context.Context, nucleiTemplateIds, customPocIds []string) []string {
	if len(nucleiTemplateIds) == 0 && len(customPocIds) == 0 {
		return nil
	}

	// 通过 HTTP 接口获取模板
	resp, err := w.httpClient.GetTemplates(ctx, &TemplatesReq{
		NucleiTemplateIds: nucleiTemplateIds,
		CustomPocIds:      customPocIds,
	})
	if err != nil {
		w.logger.Error("GetTemplates HTTP failed: %v", err)
		return nil
	}

	if !resp.Success {
		w.logger.Error("GetTemplates failed: %s", resp.Msg)
		return nil
	}

	w.logger.Info("GetTemplatesByIds: fetched %d templates", resp.Count)
	return resp.Templates
}

// getAllCustomPocs 获取所有自定义POC
func (w *Worker) getAllCustomPocs(ctx context.Context, severities []string) []string {
	// 通过 HTTP 接口获取所有自定义POC
	resp, err := w.httpClient.GetTemplates(ctx, &TemplatesReq{
		Severities:    severities,
		CustomPocOnly: true,
	})
	if err != nil {
		w.logger.Error("GetAllCustomPocs HTTP failed: %v", err)
		return nil
	}

	if !resp.Success {
		w.logger.Error("GetAllCustomPocs failed: %s", resp.Msg)
		return nil
	}

	w.logger.Info("GetAllCustomPocs: fetched %d custom POC templates", resp.Count)
	return resp.Templates
}

// parseAppName 解析应用名称，去除版本号和来源标�?
func parseAppName(app string) string {
	appName := app
	// 先去�?[source] 后缀
	if idx := strings.Index(appName, "["); idx > 0 {
		appName = appName[:idx]
	}
	// 再去除 :version 后缀
	if idx := strings.Index(appName, ":"); idx > 0 {
		appName = appName[:idx]
	}
	return strings.TrimSpace(appName)
}

// loadCustomFingerprints 加载自定义指纹到指纹扫描器
// activeScan: 是否启用主动扫描，如果启用则同时加载主动指纹
func (w *Worker) loadCustomFingerprints(ctx context.Context, fpScanner *scanner.FingerprintScanner, activeScan bool) {
	// 通过 HTTP 接口获取被动指纹配置
	var passiveFingerprints []*model.Fingerprint
	passiveFpMap := make(map[string]*model.Fingerprint)
	
	resp, err := w.httpClient.GetFingerprints(ctx, &FingerprintsReq{
		EnabledOnly: true,
	})
	if err != nil {
		w.logger.Error("GetFingerprints HTTP failed: %v", err)
		// 不直接返回，继续尝试加载主动指纹
	} else if !resp.Success {
		w.logger.Error("GetFingerprints failed: %s", resp.Msg)
		// 不直接返回，继续尝试加载主动指纹
	} else {
		// 转换为model.Fingerprint（被动指纹）
		for _, fp := range resp.Fingerprints {
			mfp := &model.Fingerprint{
				Name:      fp.Name,
				Category:  fp.Category,
				Rule:      fp.Rule,
				Source:    fp.Source,
				Headers:   fp.Headers,
				Cookies:   fp.Cookies,
				HTML:      fp.Html,
				Scripts:   fp.Scripts,
				ScriptSrc: fp.ScriptSrc,
				Meta:      fp.Meta,
				CSS:       fp.Css,
				URL:       fp.Url,
				IsBuiltin: fp.IsBuiltin,
				Enabled:   fp.Enabled,
			}
			// 解析ID
			if fp.Id != "" {
				if oid, err := primitive.ObjectIDFromHex(fp.Id); err == nil {
					mfp.Id = oid
				}
			}
			passiveFingerprints = append(passiveFingerprints, mfp)
			// 存入映射（小写名称作为key，支持不区分大小写匹配）
			passiveFpMap[strings.ToLower(fp.Name)] = mfp
		}
	}

	// 如果启用主动扫描，加载主动指纹
	var activeFingerprints []*model.Fingerprint
	if activeScan {
		activeResp, err := w.httpClient.GetActiveFingerprints(ctx, true)
		if err != nil {
			w.logger.Warn("GetActiveFingerprints HTTP failed: %v", err)
		} else if activeResp.Success && len(activeResp.Fingerprints) > 0 {
			for _, afp := range activeResp.Fingerprints {
				// 创建主动指纹对象，直接使用API返回的规则（已包含关联的被动指纹规则）
				mfp := &model.Fingerprint{
					Name:        afp.Name,
					ActivePaths: afp.Paths,
					Enabled:     afp.Enabled,
					Type:        model.FingerprintTypeActive,
					// 使用API返回的匹配规则（服务端已关联被动指纹）
					Rule:      afp.Rule,
					Headers:   afp.Headers,
					Cookies:   afp.Cookies,
					HTML:      afp.Html,
					Scripts:   afp.Scripts,
					ScriptSrc: afp.ScriptSrc,
					Meta:      afp.Meta,
					CSS:       afp.Css,
					URL:       afp.Url,
				}
				
				// 如果API没有返回规则，尝试从本地被动指纹映射获取
				if mfp.Rule == "" && len(mfp.HTML) == 0 && len(mfp.Headers) == 0 {
					if passiveFp := passiveFpMap[strings.ToLower(afp.Name)]; passiveFp != nil {
						mfp.Rule = passiveFp.Rule
						mfp.Headers = passiveFp.Headers
						mfp.Cookies = passiveFp.Cookies
						mfp.HTML = passiveFp.HTML
						mfp.Scripts = passiveFp.Scripts
						mfp.ScriptSrc = passiveFp.ScriptSrc
						mfp.Meta = passiveFp.Meta
						mfp.CSS = passiveFp.CSS
						mfp.URL = passiveFp.URL
						mfp.Category = passiveFp.Category
						w.logger.Debug("Active fingerprint '%s' linked to local passive fingerprint with rule: %s", afp.Name, passiveFp.Rule)
					} else {
						w.logger.Warn("Active fingerprint '%s' has no matching rule", afp.Name)
					}
				} else if mfp.Rule != "" {
					w.logger.Debug("Active fingerprint '%s' loaded with rule from API: %s", afp.Name, mfp.Rule)
				}
				
				// 解析ID
				if afp.Id != "" {
					if oid, err := primitive.ObjectIDFromHex(afp.Id); err == nil {
						mfp.Id = oid
					}
				}
				activeFingerprints = append(activeFingerprints, mfp)
			}
			w.logger.Info("Loaded %d active fingerprints", len(activeFingerprints))
		}
	}

	// 创建自定义指纹引擎并设置到扫描器
	// 即使被动指纹为空，只要有主动指纹也要创建引擎
	if len(passiveFingerprints) > 0 || len(activeFingerprints) > 0 {
		var customEngine *scanner.CustomFingerprintEngine
		if len(activeFingerprints) > 0 {
			customEngine = scanner.NewCustomFingerprintEngineWithActive(passiveFingerprints, activeFingerprints)
		} else {
			customEngine = scanner.NewCustomFingerprintEngine(passiveFingerprints)
		}
		fpScanner.SetCustomFingerprintEngine(customEngine)
		w.logger.Info("Loaded %d passive fingerprints, %d active fingerprints into scanner", len(passiveFingerprints), len(activeFingerprints))
	} else {
		w.logger.Info("No fingerprints found")
	}
}

// filterByPortThreshold 根据端口阈值过滤资产
// 如果某个主机开放的端口数量超过阈值，则过滤掉该主机的所有资产（可能是防火墙或蜜罐）
// 返回值: 过滤后的资产列表, 是否有主机超过阈值
func filterByPortThreshold(assets []*scanner.Asset, threshold int) ([]*scanner.Asset, bool) {
	if threshold <= 0 {
		return assets, false // 阈值为0或负数表示不过滤
	}

	// 统计每个主机的开放端口数量
	hostPortCount := make(map[string]int)
	for _, asset := range assets {
		hostPortCount[asset.Host]++
	}

	// 找出需要过滤的主机
	filteredHosts := make(map[string]bool)
	thresholdExceeded := false
	for host, count := range hostPortCount {
		if count > threshold {
			filteredHosts[host] = true
			thresholdExceeded = true
			logx.Infof("Host %s has %d open ports (threshold: %d), filtered as potential honeypot/firewall", host, count, threshold)
		}
	}

	// 过滤资产
	if len(filteredHosts) == 0 {
		return assets, false
	}

	result := make([]*scanner.Asset, 0, len(assets))
	for _, asset := range assets {
		if !filteredHosts[asset.Host] {
			result = append(result, asset)
		}
	}
	return result, thresholdExceeded
}

// executePocValidateTask 执行POC验证任务
func (w *Worker) executePocValidateTask(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, startTime time.Time) {
	// 解析配置
	url, _ := taskConfig["url"].(string)
	pocId, _ := taskConfig["pocId"].(string)
	pocType, _ := taskConfig["pocType"].(string)
	timeout, _ := taskConfig["timeout"].(float64)
	batchId, _ := taskConfig["batchId"].(string)
	workspaceId, _ := taskConfig["workspaceId"].(string)
	if workspaceId == "" {
		workspaceId = task.WorkspaceId
	}
	if workspaceId == "" {
		workspaceId = "default"
	}

	// 立即输出任务接收日志
	w.taskLog(task.TaskId, LevelInfo, "[%s] 收到POC验证任务, 目标: %s", task.TaskId, url)

	if url == "" {
		w.taskLog(task.TaskId, LevelError, "[%s] POC验证失败: URL为空", task.TaskId)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "URL为空")
		w.savePocValidationResult(ctx, task.TaskId, batchId, nil, "URL为空")
		return
	}

	if timeout == 0 {
		timeout = 30
	}

	// 获取Nuclei扫描器
	nucleiScanner, ok := w.scanners["nuclei"]
	if !ok {
		w.taskLog(task.TaskId, LevelError, "[%s] POC验证失败: Nuclei扫描器未初始化", task.TaskId)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "Nuclei扫描器未初始化")
		w.savePocValidationResult(ctx, task.TaskId, batchId, nil, "Nuclei扫描器未初始化")
		return
	}

	// 获取POC模板
	var templates []string
	var pocName string
	var pocSeverity string

	// 如果指定了pocId，通过 HTTP 接口获取POC内容
	if pocId != "" {
		w.taskLog(task.TaskId, LevelInfo, "[%s] Loading POC template...", task.TaskId)
		resp, err := w.httpClient.GetPocById(ctx, pocId, pocType)
		if err != nil {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: failed to get POC - %v", task.TaskId, err)
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "Failed to get POC: "+err.Error())
			w.savePocValidationResult(ctx, task.TaskId, batchId, nil, "Failed to get POC: "+err.Error())
			return
		}
		if !resp.Success {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: POC not found - %s", task.TaskId, resp.Msg)
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "POC not found: "+resp.Msg)
			w.savePocValidationResult(ctx, task.TaskId, batchId, nil, "POC not found: "+resp.Msg)
			return
		}
		if resp.Content == "" {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: POC content is empty", task.TaskId)
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "POC content is empty")
			w.savePocValidationResult(ctx, task.TaskId, batchId, nil, "POC content is empty")
			return
		}
		templates = []string{resp.Content}
		pocName = resp.Name
		pocSeverity = resp.Severity
		pocType = resp.PocType
		w.taskLog(task.TaskId, LevelInfo, "[%s] POC template loaded: %s", task.TaskId, pocName)
	} else {
		// 没有指定pocId，尝试通过标签获取模板
		var severities []string
		var tags []string

		// 解析严重级别
		if sevList, ok := taskConfig["severities"].([]interface{}); ok {
			for _, s := range sevList {
				if str, ok := s.(string); ok {
					severities = append(severities, str)
				}
			}
		}

		// 解析标签
		if tagList, ok := taskConfig["tags"].([]interface{}); ok {
			for _, t := range tagList {
				if str, ok := t.(string); ok {
					tags = append(tags, str)
				}
			}
		}

		// 根据标签获取模板
		if len(tags) > 0 {
			templates = w.getTemplatesByTags(ctx, tags, severities)
		}

		if len(templates) == 0 {
			w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: no POC templates found", task.TaskId)
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "No POC templates found")
			w.savePocValidationResult(ctx, task.TaskId, batchId, nil, "No POC templates found")
			return
		}
	}

	// 输出开始扫描日志
	w.taskLog(task.TaskId, LevelInfo, "[%s] Initializing Nuclei scan engine...", task.TaskId)

	// 构建Nuclei扫描选项
	nucleiOpts := &scanner.NucleiOptions{
		RateLimit:       50,
		Concurrency:     10,
		CustomTemplates: templates,
		CustomPocOnly:   true, // 只使用自定义POC
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Scanning target: %s", task.TaskId, url)

	// 执行扫描 - 直接传递URL作为目标，不通过Asset构建
	result, err := nucleiScanner.Scan(ctx, &scanner.ScanConfig{
		Targets: []string{url}, // 直接使用URL作为目标
		Options: nucleiOpts,
	})

	duration := time.Since(startTime).Seconds()

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "[%s] POC validation failed: %v", task.TaskId, err)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, fmt.Sprintf("Scan failed: %v", err))
		w.savePocValidationResult(ctx, task.TaskId, batchId, nil, fmt.Sprintf("Scan failed: %v", err))
		return
	}

	// 构建验证结果
	var validationResults []*PocValidationResult
	matched := false
	vulCount := 0
	if result != nil {
		vulCount = len(result.Vulnerabilities)
	}

	w.taskLog(task.TaskId, LevelInfo, "[%s] Scan completed, duration: %.2fs", task.TaskId, duration)

	if result != nil && len(result.Vulnerabilities) > 0 {
		matched = true
		for _, vul := range result.Vulnerabilities {
			// 优先使用配置中的POC信息
			resultPocName := pocName
			resultSeverity := pocSeverity
			if resultPocName == "" {
				resultPocName = vul.PocFile
			}
			if resultSeverity == "" {
				resultSeverity = vul.Severity
			}
			validationResults = append(validationResults, &PocValidationResult{
				PocId:      pocId,
				PocName:    resultPocName,
				TemplateId: pocId,
				Severity:   resultSeverity,
				Matched:    true,
				MatchedUrl: vul.Url,
				Details:    vul.Result,
				Output:     vul.Extra,
				PocType:    pocType,
			})
			logx.Infof("[%s] Vulnerability found! Matched URL: %s", task.TaskId, vul.Url)
			w.taskLog(task.TaskId, LevelInfo, "[%s] Vulnerability found! Matched URL: %s", task.TaskId, vul.Url)
		}
		// 保存漏洞到数据库
		w.saveVulResult(ctx, workspaceId, task.TaskId, result.Vulnerabilities)
	} else {
		// 没有发现漏洞，添加一个未匹配的结果
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

	// 保存结果到Redis
	w.savePocValidationResult(ctx, task.TaskId, batchId, validationResults, "")

	// 更新任务状态
	resultMsg := fmt.Sprintf("Validation completed: matched=%v, vuls=%d, duration=%.2fs", matched, vulCount, duration)
	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusSuccess, resultMsg)
	// 注意：taskExecuted 由 executeTask 的 defer 递增，无需在此处理
}

// executePocBatchValidateTask 执行POC批量验证任务（使用单个Nuclei引擎扫描所有目标）
func (w *Worker) executePocBatchValidateTask(ctx context.Context, task *scheduler.TaskInfo, taskConfig map[string]interface{}, startTime time.Time) {
	// 解析配置
	pocId, _ := taskConfig["pocId"].(string)
	pocType, _ := taskConfig["pocType"].(string)
	timeout, _ := taskConfig["timeout"].(float64)
	workspaceId, _ := taskConfig["workspaceId"].(string)
	if workspaceId == "" {
		workspaceId = task.WorkspaceId
	}
	if workspaceId == "" {
		workspaceId = "default"
	}

	// 解析目标URL列表
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
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "目标列表为空")
		return
	}

	if timeout == 0 {
		// 每个目标30秒，最少60秒
		timeout = float64(len(urls) * 30)
		if timeout < 60 {
			timeout = 60
		}
	}

	// 获取Nuclei扫描器
	nucleiScanner, ok := w.scanners["nuclei"].(*scanner.NucleiScanner)
	if !ok {
		w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: Nuclei扫描器未初始化", task.TaskId)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "Nuclei扫描器未初始化")
		return
	}

	// 获取POC模板
	var templates []string
	var pocName string
	var pocSeverity string

	if pocId != "" {
		w.taskLog(task.TaskId, LevelInfo, "[%s] Loading POC template...", task.TaskId)
		resp, err := w.httpClient.GetPocById(ctx, pocId, pocType)
		if err != nil {
			w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: 获取POC失败 - %v", task.TaskId, err)
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "获取POC失败: "+err.Error())
			return
		}
		if !resp.Success || resp.Content == "" {
			w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: POC不存在或内容为空", task.TaskId)
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "POC不存在或内容为空")
			return
		}
		templates = []string{resp.Content}
		pocName = resp.Name
		pocSeverity = resp.Severity
		pocType = resp.PocType
		w.taskLog(task.TaskId, LevelInfo, "[%s] POC template loaded: %s", task.TaskId, pocName)
	} else {
		w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描失败: 未指定POC ID", task.TaskId)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "未指定POC ID")
		return
	}

	// 构建Nuclei扫描选项
	nucleiOpts := &scanner.NucleiOptions{
		RateLimit:       150,
		Concurrency:     25,
		Timeout:         int(timeout),
		CustomTemplates: templates,
		CustomPocOnly:   true,
		// 设置回调函数，发现漏洞时立即保存到数据库
		OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
			w.taskLog(task.TaskId, LevelInfo, "[%s] Vulnerability found! %s → %s", task.TaskId, vul.PocFile, vul.Url)
			// 立即保存到数据库
			w.saveVulResult(ctx, workspaceId, task.TaskId, []*scanner.Vulnerability{vul})
		},
	}

	// 使用批量扫描方法
	w.taskLog(task.TaskId, LevelInfo, "[%s] Starting batch scan: %d targets, timeout %ds", task.TaskId, len(urls), int(timeout))

	vuls, err := nucleiScanner.ScanBatch(ctx, urls, nucleiOpts, func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, "[%s] "+format, append([]interface{}{task.TaskId}, args...)...)
	})

	duration := time.Since(startTime).Seconds()

	if err != nil {
		w.taskLog(task.TaskId, LevelError, "[%s] POC批量扫描出错: %v", task.TaskId, err)
	}

	vulCount := len(vuls)
	w.taskLog(task.TaskId, LevelInfo, "[%s] Batch scan completed, duration: %.2fs, vuls: %d", task.TaskId, duration, vulCount)

	// 漏洞已在回调中实时保存，这里不需要再保存
	if vulCount > 0 {
		w.taskLog(task.TaskId, LevelInfo, "[%s] Total %d vulnerabilities saved to database", task.TaskId, vulCount)
	}

	// 构建验证结果
	var validationResults []*PocValidationResult
	if vulCount > 0 {
		for _, vul := range vuls {
			validationResults = append(validationResults, &PocValidationResult{
				PocId:      pocId,
				PocName:    pocName,
				TemplateId: pocId,
				Severity:   pocSeverity,
				Matched:    true,
				MatchedUrl: vul.Url,
				Details:    vul.Result,
				Output:     vul.Extra,
				PocType:    pocType,
			})
		}
	}

	// 保存结果到Redis
	w.savePocValidationResult(ctx, task.TaskId, "", validationResults, "")

	// 更新任务状态
	resultMsg := fmt.Sprintf("Batch scan completed: targets=%d, vuls=%d, duration=%.2fs", len(urls), vulCount, duration)
	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusSuccess, resultMsg)
	// 注意：taskExecuted 由 executeTask 的 defer 递增，无需在此处理
}

// PocValidationResult POC验证结果
type PocValidationResult struct {
	PocId      string   `json:"pocId"`
	PocName    string   `json:"pocName"`
	TemplateId string   `json:"templateId"`
	Severity   string   `json:"severity"`
	Matched    bool     `json:"matched"`
	MatchedUrl string   `json:"matchedUrl"`
	Details    string   `json:"details"`
	Output     string   `json:"output"`
	PocType    string   `json:"pocType"`
	Tags       []string `json:"tags"`
}

// savePocValidationResult 保存POC验证结果
// NOTE: POC验证结果现在通过任务状态更新接口保存，不再直接写 Redis
func (w *Worker) savePocValidationResult(ctx context.Context, taskId, batchId string, results []*PocValidationResult, errorMsg string) {
	// 构建结果数据
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

	// 通过 HTTP 接口更新任务结果
	status := scheduler.TaskStatusSuccess
	if errorMsg != "" {
		status = scheduler.TaskStatusFailure
	}
	_, err = w.httpClient.UpdateTask(ctx, &TaskUpdateReq{
		TaskId: taskId,
		State:  status,
		Result: string(resultJson),
	})
	if err != nil {
		w.taskLog(taskId, LevelError, "Failed to save POC validation result: %v", err)
	}
}

// WorkerHttpServiceChecker Worker端的HTTP服务检查器实现
type WorkerHttpServiceChecker struct {
	serviceCache map[string]bool // serviceName -> isHttp
	httpPorts    map[int]bool    // HTTP端口
	httpsPorts   map[int]bool    // HTTPS端口
	mu           sync.RWMutex
}

// NewWorkerHttpServiceChecker 创建HTTP服务检查器
func NewWorkerHttpServiceChecker() *WorkerHttpServiceChecker {
	return &WorkerHttpServiceChecker{
		serviceCache: make(map[string]bool),
		httpPorts:    make(map[int]bool),
		httpsPorts:   make(map[int]bool),
	}
}

// IsHttpService 判断服务名称是否为HTTP服务
func (c *WorkerHttpServiceChecker) IsHttpService(serviceName string) (isHttp bool, found bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	isHttp, found = c.serviceCache[serviceName]
	return
}

// IsHttpPort 判断端口是否为HTTP端口
func (c *WorkerHttpServiceChecker) IsHttpPort(port int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpPorts[port] || c.httpsPorts[port]
}

// CheckIsHttp 综合判断是否为HTTP服务（服务名称+端口）
func (c *WorkerHttpServiceChecker) CheckIsHttp(serviceName string, port int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 1. 先检查服务名称映射
	if isHttp, found := c.serviceCache[serviceName]; found {
		return isHttp
	}

	// 2. 再检查端口
	return c.httpPorts[port] || c.httpsPorts[port]
}

// SetMapping 设置服务映射
func (c *WorkerHttpServiceChecker) SetMapping(serviceName string, isHttp bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serviceCache[serviceName] = isHttp
}

// SetHttpPorts 设置HTTP端口列表
func (c *WorkerHttpServiceChecker) SetHttpPorts(ports []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpPorts = make(map[int]bool)
	for _, port := range ports {
		c.httpPorts[port] = true
	}
}

// SetHttpsPorts 设置HTTPS端口列表
func (c *WorkerHttpServiceChecker) SetHttpsPorts(ports []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpsPorts = make(map[int]bool)
	for _, port := range ports {
		c.httpsPorts[port] = true
	}
}

// executePortIdentify 执行端口识别阶段（Nmap服务识别）
func (w *Worker) executePortIdentify(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.PortIdentifyConfig) []*scanner.Asset {
	w.taskLog(task.TaskId, LevelInfo, "Port identify: Nmap (%d assets)", len(assets))

	// 获取超时配置
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 // 默认30秒/主机
	}

	// 按主机分组
	hostPorts := make(map[string][]int)
	hostAssets := make(map[string][]*scanner.Asset)
	for _, asset := range assets {
		hostPorts[asset.Host] = append(hostPorts[asset.Host], asset.Port)
		hostAssets[asset.Host] = append(hostAssets[asset.Host], asset)
	}

	// 计算总超时时间
	totalTimeout := timeout * len(hostPorts)
	if totalTimeout < 60 {
		totalTimeout = 60
	}
	identifyCtx, identifyCancel := context.WithTimeout(ctx, time.Duration(totalTimeout)*time.Second)
	defer identifyCancel()

	var identifiedAssets []*scanner.Asset
	nmapScanner := w.scanners["nmap"]

	for host, ports := range hostPorts {
		// 检查是否被停止或超时
		if identifyCtx.Err() == context.DeadlineExceeded {
			w.taskLog(task.TaskId, LevelWarn, "Port identify timeout, using partial results")
			// 超时时使用原始资产
			for _, asset := range hostAssets[host] {
				asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
				identifiedAssets = append(identifiedAssets, asset)
			}
			continue
		}
		if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "Task stopped")
			return identifiedAssets
		}

		// 构建端口字符串
		portStrs := make([]string, len(ports))
		for i, p := range ports {
			portStrs[i] = fmt.Sprintf("%d", p)
		}
		portsStr := strings.Join(portStrs, ",")

		// 构建 Nmap 选项
		nmapOpts := &scanner.NmapOptions{
			Ports:   portsStr,
			Timeout: timeout,
		}
		if config.Args != "" {
			nmapOpts.Args = config.Args
		}

		nmapResult, err := nmapScanner.Scan(identifyCtx, &scanner.ScanConfig{
			Target:  host,
			Options: nmapOpts,
		})

		// 检查是否被停止
		if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "Task stopped")
			return identifiedAssets
		}

		if err != nil {
			w.taskLog(task.TaskId, LevelError, "Nmap error %s: %v", host, err)
			// Nmap失败时，使用原始资产
			for _, asset := range hostAssets[host] {
				asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
				identifiedAssets = append(identifiedAssets, asset)
			}
			continue
		}

		if nmapResult != nil && len(nmapResult.Assets) > 0 {
			// 设置 IsHTTP 字段
			for _, asset := range nmapResult.Assets {
				asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
			}
			identifiedAssets = append(identifiedAssets, nmapResult.Assets...)
		} else {
			// Nmap没有结果时，使用原始资产
			for _, asset := range hostAssets[host] {
				asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
				identifiedAssets = append(identifiedAssets, asset)
			}
		}
	}

	w.taskLog(task.TaskId, LevelInfo, "Port identify completed: %d assets", len(identifiedAssets))
	return identifiedAssets
}

// executeDirScan 执行目录扫描阶段
func (w *Worker) executeDirScan(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset, config *scheduler.DirScanConfig, orgId string) []*scanner.Asset {
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

	dictResp, err := w.httpClient.GetDirScanDicts(ctx, config.DictIds)
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

	// 获取扫描器
	urlfinderScanner, ok := w.scanners["urlfinder"]
	if !ok {
		w.taskLog(task.TaskId, LevelError, "Dir scan: urlfinder scanner not found")
		return nil
	}

	// 构建扫描选项
	threads := config.Threads
	if threads <= 0 {
		threads = 50
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	statusCodes := config.StatusCodes
	if len(statusCodes) == 0 {
		statusCodes = []int{200, 201, 204, 301, 302, 307, 308, 401, 403, 405, 500}
	}

	opts := &scanner.URLFinderOptions{
		Paths:          allPaths,
		Threads:        threads,
		Timeout:        timeout,
		StatusCodes:    statusCodes,
		Extensions:     config.Extensions,
		FollowRedirect: config.FollowRedirect,
	}

	// 计算总超时时间
	totalTimeout := timeout * len(httpAssets) * len(allPaths) / threads
	if totalTimeout < 60 {
		totalTimeout = 60
	}
	if totalTimeout > 3600 {
		totalTimeout = 3600 // 最大1小时
	}
	dirCtx, dirCancel := context.WithTimeout(ctx, time.Duration(totalTimeout)*time.Second)
	defer dirCancel()

	// 创建任务日志回调
	taskLogger := func(level, format string, args ...interface{}) {
		w.taskLog(task.TaskId, level, format, args...)
	}

	// 创建进度回调
	onProgress := func(progress int, message string) {
		w.updateTaskProgress(ctx, task.TaskId, 70+progress/5, message) // 70-90%
	}

	// 执行扫描
	result, err := urlfinderScanner.Scan(dirCtx, &scanner.ScanConfig{
		Assets:      httpAssets,
		Options:     opts,
		WorkspaceId: task.WorkspaceId,
		MainTaskId:  task.MainTaskId,
		TaskLogger:  taskLogger,
		OnProgress:  onProgress,
	})

	// 检查是否超时
	if dirCtx.Err() == context.DeadlineExceeded {
		w.taskLog(task.TaskId, LevelWarn, "Dir scan timeout after %ds, using partial results", totalTimeout)
	}

	// 检查是否被停止
	if ctx.Err() != nil || w.checkTaskControl(ctx, task.TaskId) == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task stopped")
		return nil
	}

	if err != nil {
		// 目录扫描错误不应导致任务退出，只记录警告并继续
		w.taskLog(task.TaskId, LevelWarn, "Dir scan error (continuing): %v", err)
		// 如果有部分结果，仍然返回
		if result != nil && len(result.Assets) > 0 {
			w.taskLog(task.TaskId, LevelInfo, "Dir scan: returning %d partial results despite error", len(result.Assets))
			w.saveDirScanResults(ctx, task, result.Assets)
			return result.Assets
		}
		return nil
	}

	if result != nil && len(result.Assets) > 0 {
		w.taskLog(task.TaskId, LevelInfo, "Dir scan completed: found %d paths", len(result.Assets))

		// 保存目录扫描结果到数据库
		w.saveDirScanResults(ctx, task, result.Assets)

		return result.Assets
	}

	return nil
}

// saveDirScanResults 保存目录扫描结果到数据库
func (w *Worker) saveDirScanResults(ctx context.Context, task *scheduler.TaskInfo, assets []*scanner.Asset) {
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
		})
	}

	// 调用 HTTP 接口保存结果
	req := &DirScanResultReq{
		WorkspaceId: task.WorkspaceId,
		MainTaskId:  task.MainTaskId,
		Results:     results,
	}

	w.taskLog(task.TaskId, LevelDebug, "Dir scan: calling SaveDirScanResult API with %d results", len(results))

	resp, err := w.httpClient.SaveDirScanResult(ctx, req)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Dir scan: save results failed: %v", err)
		return
	}

	if resp.Success {
		w.taskLog(task.TaskId, LevelInfo, "Dir scan: saved %d results to database", resp.Total)
	} else {
		w.taskLog(task.TaskId, LevelWarn, "Dir scan: save results response: %s", resp.Msg)
	}
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

// generateHTTPAssetsFromTarget 从目标生成 HTTP 资产（用于只启用目录扫描的场景）
// 支持的目标格式：
// - 域名: example.com (默认生成 80 和 443 端口)
// - 带端口: example.com:8080
// - URL: http://example.com:8080 或 https://example.com
func (w *Worker) generateHTTPAssetsFromTarget(target string) []*scanner.Asset {
	var assets []*scanner.Asset

	targets := strings.Split(target, "\n")
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		// 处理 URL 格式
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			asset := w.parseURLToAsset(t)
			if asset != nil {
				asset.IsHTTP = true
				assets = append(assets, asset)
			}
			continue
		}

		// 处理带端口的格式 host:port
		if strings.Contains(t, ":") && !strings.Contains(t, "/") {
			parts := strings.Split(t, ":")
			if len(parts) == 2 {
				host := parts[0]
				port := 80
				if p, err := strconv.Atoi(parts[1]); err == nil {
					port = p
				}
				// 只有当端口是常见HTTP端口时才标记为HTTP服务
				isHTTP := scanner.IsHTTPService("", port)
				asset := &scanner.Asset{
					Host:      host,
					Port:      port,
					Authority: fmt.Sprintf("%s:%d", host, port),
					IsHTTP:    isHTTP,
				}
				if isHTTP {
					assets = append(assets, asset)
				} else {
					w.logger.Debug("Skipping non-HTTP target: %s:%d", host, port)
				}
				continue
			}
		}

		// 跳过 CIDR 和 IP 范围格式（目录扫描不适用）
		if strings.Contains(t, "/") || (strings.Contains(t, "-") && !strings.Contains(t, ".")) {
			continue
		}

		// 单个主机（域名或IP），生成 80 和 443 端口
		for _, port := range []int{80, 443} {
			asset := &scanner.Asset{
				Host:      t,
				Port:      port,
				Authority: fmt.Sprintf("%s:%d", t, port),
				Service:   map[int]string{80: "http", 443: "https"}[port],
				IsHTTP:    true,
			}
			assets = append(assets, asset)
		}
	}

	return assets
}

// generateAssetsFromTarget 从目标生成初始资产列表（用于端口扫描禁用时）
// 支持的目标格式：
// - 单个IP: 192.168.1.1
// - IP范围: 192.168.1.1-192.168.1.10
// - CIDR: 192.168.1.0/24
// - 域名: example.com
// - 带端口: 192.168.1.1:8080 或 example.com:443
// - URL: http://example.com:8080
func (w *Worker) generateAssetsFromTarget(target string, portConfig *scheduler.PortScanConfig) []*scanner.Asset {
	var assets []*scanner.Asset

	// 默认端口列表
	defaultPorts := []int{80, 443, 8080, 8443}

	// 如果配置了端口，解析端口列表
	if portConfig != nil && portConfig.Ports != "" {
		defaultPorts = parsePortList(portConfig.Ports)
	}

	// 解析目标
	targets := strings.Split(target, "\n")
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		// 处理URL格式
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			asset := w.parseURLToAsset(t)
			if asset != nil {
				assets = append(assets, asset)
			}
			continue
		}

		// 处理带端口的格式 host:port
		if strings.Contains(t, ":") && !strings.Contains(t, "/") {
			parts := strings.Split(t, ":")
			if len(parts) == 2 {
				host := parts[0]
				port := 80
				if p, err := strconv.Atoi(parts[1]); err == nil {
					port = p
				}
				asset := &scanner.Asset{
					Host:      host,
					Port:      port,
					Authority: fmt.Sprintf("%s:%d", host, port),
					IsHTTP:    scanner.IsHTTPService("", port),
				}
				assets = append(assets, asset)
				continue
			}
		}

		// 处理CIDR格式 - 跳过，因为没有端口扫描无法确定开放端口
		if strings.Contains(t, "/") {
			w.logger.Warn("CIDR target %s skipped: port scan disabled, cannot determine open ports", t)
			continue
		}

		// 处理IP范围格式 - 跳过
		if strings.Contains(t, "-") && !strings.Contains(t, ".") {
			w.logger.Warn("IP range target %s skipped: port scan disabled, cannot determine open ports", t)
			continue
		}

		// 单个主机（IP或域名），使用默认端口
		for _, port := range defaultPorts {
			asset := &scanner.Asset{
				Host:      t,
				Port:      port,
				Authority: fmt.Sprintf("%s:%d", t, port),
				IsHTTP:    scanner.IsHTTPService("", port),
			}
			assets = append(assets, asset)
		}
	}

	return assets
}

// parseURLToAsset 解析URL为资产
func (w *Worker) parseURLToAsset(urlStr string) *scanner.Asset {
	// 简单解析URL
	scheme := "http"
	host := ""
	port := 80

	if strings.HasPrefix(urlStr, "https://") {
		scheme = "https"
		port = 443
		urlStr = strings.TrimPrefix(urlStr, "https://")
	} else if strings.HasPrefix(urlStr, "http://") {
		urlStr = strings.TrimPrefix(urlStr, "http://")
	}

	// 移除路径部分
	if idx := strings.Index(urlStr, "/"); idx > 0 {
		urlStr = urlStr[:idx]
	}

	// 解析host:port
	if strings.Contains(urlStr, ":") {
		parts := strings.Split(urlStr, ":")
		host = parts[0]
		if p, err := strconv.Atoi(parts[1]); err == nil {
			port = p
		}
	} else {
		host = urlStr
	}

	if host == "" {
		return nil
	}

	return &scanner.Asset{
		Host:      host,
		Port:      port,
		Authority: fmt.Sprintf("%s:%d", host, port),
		Service:   scheme,
		IsHTTP:    true,
	}
}

// parsePortList 解析端口列表字符串
func parsePortList(portsStr string) []int {
	var ports []int
	seen := make(map[int]bool)

	parts := strings.Split(portsStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// 处理端口范围 (如 80-90)
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
				if err1 == nil && err2 == nil && start <= end {
					for p := start; p <= end && p <= 65535; p++ {
						if !seen[p] {
							seen[p] = true
							ports = append(ports, p)
						}
					}
				}
			}
		} else {
			// 单个端口
			if p, err := strconv.Atoi(part); err == nil && p > 0 && p <= 65535 {
				if !seen[p] {
					seen[p] = true
					ports = append(ports, p)
				}
			}
		}
	}

	return ports
}

// loadHttpServiceMappings 从 HTTP 接口加载HTTP服务设置（端口配置+服务映射）
func (w *Worker) loadHttpServiceMappings() {
	ctx := context.Background()

	// 通过 HTTP 接口获取 HTTP 服务设置
	resp, err := w.httpClient.GetHttpServiceSettings(ctx)
	if err != nil {
		w.logger.Error("GetHttpServiceSettings HTTP failed: %v, using default settings", err)
		// 回退到旧接口
		w.loadHttpServiceMappingsLegacy()
		return
	}

	if !resp.Success {
		w.logger.Error("GetHttpServiceSettings failed: %s, using default settings", resp.Msg)
		// 回退到旧接口
		w.loadHttpServiceMappingsLegacy()
		return
	}

	// 创建检查器
	checker := NewWorkerHttpServiceChecker()

	// 设置端口配置
	if len(resp.Config.HttpPorts) > 0 {
		checker.SetHttpPorts(resp.Config.HttpPorts)
		w.logger.Info("Loaded %d HTTP ports from database", len(resp.Config.HttpPorts))
	}
	if len(resp.Config.HttpsPorts) > 0 {
		checker.SetHttpsPorts(resp.Config.HttpsPorts)
		w.logger.Info("Loaded %d HTTPS ports from database", len(resp.Config.HttpsPorts))
	}

	// 设置服务映射
	for _, mapping := range resp.Mappings {
		checker.SetMapping(mapping.ServiceName, mapping.IsHttp)
	}
	if len(resp.Mappings) > 0 {
		w.logger.Info("Loaded %d HTTP service mappings from database", len(resp.Mappings))
	}

	// 设置全局检查器
	scanner.SetHttpServiceChecker(checker)
}

// loadHttpServiceMappingsLegacy 旧版本的加载方法（兼容）
func (w *Worker) loadHttpServiceMappingsLegacy() {
	ctx := context.Background()

	// 通过 HTTP 接口获取 HTTP 服务映射
	resp, err := w.httpClient.GetHttpServiceMappings(ctx, true)
	if err != nil {
		w.logger.Error("GetHttpServiceMappings HTTP failed: %v, using default mappings", err)
		return
	}

	if !resp.Success {
		w.logger.Error("GetHttpServiceMappings failed: %s, using default mappings", resp.Msg)
		return
	}

	if len(resp.Mappings) == 0 {
		w.logger.Info("No HTTP service mappings found, using default mappings")
		return
	}

	// 创建检查器并设置映射
	checker := NewWorkerHttpServiceChecker()
	for _, mapping := range resp.Mappings {
		checker.SetMapping(mapping.ServiceName, mapping.IsHttp)
	}

	// 设置全局检查器
	scanner.SetHttpServiceChecker(checker)
	w.logger.Info("Loaded %d HTTP service mappings from database (legacy)", len(resp.Mappings))
}

// NOTE: subscribeControlCommand 和 handleControlCommand 已移除，将在 Task 6 中通过 WebSocket 实现
