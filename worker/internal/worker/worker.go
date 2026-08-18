package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"cscan/internal/model"
	"cscan/internal/notification"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"
	"cscan/pkg/utils"

	"github.com/projectdiscovery/wappalyzergo"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// WorkerConfig Worker配置
type WorkerConfig struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	ServerAddr  string `json:"serverAddr"` // API 服务地址 (e.g., http://server:8888)
	InstallKey  string `json:"installKey"` // 安装密钥
	Concurrency int    `json:"concurrency"`
	Timeout     int    `json:"timeout"`

	// Phase 2 客户端优先级队列管理器（默认关闭，保持向后兼容）
	// 开启后 taskChan 退化为预留槽位计数器，任务实际进入 TaskQueueManager
	// 由 GetTaskPriority 推断优先级，按 Urgent>High>Normal>Low 顺序出队
	EnableTaskQueueManager bool          `json:"enableTaskQueueManager"`
	MaxQueueSize           int           `json:"maxQueueSize"` // 0 表示默认 100
	MaxWaitTime            time.Duration `json:"maxWaitTime"`  // 0 表示默认 5 分钟
}

// Worker 工作节点
type Worker struct {
	ctx         context.Context
	cancel      context.CancelFunc
	config      WorkerConfig
	httpClient  *WorkerHTTPClient // HTTP 客户端（配置/模板/字典等仍走 HTTP）
	schedClient *SchedulerClient  // Redis 直连调度客户端（任务拉取/心跳/进度上报）
	wsClient    *WorkerWSClient   // WebSocket 客户端（用于日志推送和控制信号）
	scanners    map[string]scanner.Scanner
	taskChan    chan *scheduler.TaskInfo
	stopChan    chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
	mu          sync.RWMutex

	// Phase 2 客户端优先级队列管理器
	// 当 config.EnableTaskQueueManager=true 时启用
	// taskChan 此时退化为预留槽位计数器（len(taskChan) 用于检查并发槽位）
	// 任务实体存放在 taskQueue 的 4 级优先级 slice 中
	taskQueue *TaskQueueManager

	taskStarted   int
	taskExecuted  int
	isRunning     bool
	executorCount int // 已启动的任务处理协程数

	// 任务控制信号
	taskControlSignals sync.Map // taskId -> action (STOP, PAUSE)

	// 正在执行的任务
	runningTasks sync.Map // taskId -> true

	// 日志组件
	logger Logger

	// Wappalyzer 指纹识别客户端（单例，避免重复初始化）
	wappalyzerClient *wappalyzer.Wappalyze
	wappalyzerOnce   sync.Once

	// 本地结果队列（API 不可用时持久化任务结果）
	resultQueue *ResultQueue

	// MongoDB 直连（用于扫描结果直接写入，绕过 HTTP API）
	mongoClient *mongo.Client
	mongoDB     *mongo.Database

	// 活跃任务的日志记录器（维持 buffer 生命周期）
	taskLoggers sync.Map // mainTaskId -> *TaskLoggerWS

	// 主任务实时进度缓存（incrSubTaskDone 时刷新，onProgress 时读取计算）
	progressMu           sync.Mutex
	cachedSubTaskDone    int
	cachedSubTaskCount   int
	cachedMainTaskId     string
	lastReportedProgress int // 上次上报的进度百分比，防止回退
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

// defaultTaskTimeoutSec 默认单个任务整体超时上限（秒）。
// 为 baseCtx 提供兜底超时，防止任务无限期占用并发槽位；可经环境变量 CSCAN_TASK_TIMEOUT 覆盖。
const defaultTaskTimeoutSec = 6 * 60 * 60 // 6 小时

// resolveTaskOverallTimeout 推导单个任务的整体超时上限（秒）。
// 优先级：环境变量 CSCAN_TASK_TIMEOUT > Worker 配置 Timeout > 默认值。
func resolveTaskOverallTimeout(configTimeout int) time.Duration {
	timeoutSec := defaultTaskTimeoutSec
	if configTimeout > 0 {
		timeoutSec = configTimeout
	}
	if env := os.Getenv("CSCAN_TASK_TIMEOUT"); env != "" {
		if v, err := strconv.Atoi(env); err == nil && v > 0 {
			timeoutSec = v
		}
	}
	return time.Duration(timeoutSec) * time.Second
}

// taskLog 发布任务级别日志
// 子任务的日志会同时写入主任务的日志流，方便统一查看
func (w *Worker) taskLog(taskId, level, format string, args ...interface{}) {
	mainTaskId := getMainTaskId(taskId)

	if mainTaskId != taskId {
		subIndex := taskId[len(mainTaskId)+1:]
		format = fmt.Sprintf("[Sub-%s] %s", subIndex, format)
	}

	// 从 map 获取持久化的 logger 实例，仅在首次时创建
	val, ok := w.taskLoggers.Load(mainTaskId)
	if !ok {
		newLogger := NewTaskLoggerWS(w.config.Name, mainTaskId)
		val, _ = w.taskLoggers.LoadOrStore(mainTaskId, newLogger)
	}
	logger := val.(*TaskLoggerWS)

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

// cleanupTaskLogger 清理任务日志记录器
func (w *Worker) cleanupTaskLogger(taskId string) {
	mainTaskId := getMainTaskId(taskId)
	// 日志直写 MongoDB，无需 flush 缓冲区
	w.taskLoggers.Delete(mainTaskId)
}

// VulnerabilityBuffer 批量缓冲保存漏洞
// AssetBuffer 批量缓冲保存资产
type AssetBuffer struct {
	assets    []*scanner.Asset
	mu        sync.Mutex
	maxSize   int
	flushChan chan struct{}
}

// NewAssetBuffer 创建资产缓冲区
func NewAssetBuffer(maxSize int) *AssetBuffer {
	return &AssetBuffer{
		assets:    make([]*scanner.Asset, 0, maxSize),
		maxSize:   maxSize,
		flushChan: make(chan struct{}, 1),
	}
}

// GetFlushChan 返回刷新信号通道，供外层 select 监听
func (b *AssetBuffer) GetFlushChan() <-chan struct{} {
	return b.flushChan
}

// Add 添加资产到缓冲区，如果达到 maxSize 则触发刷新
func (b *AssetBuffer) Add(asset *scanner.Asset) {
	b.mu.Lock()
	b.assets = append(b.assets, asset)
	shouldFlush := len(b.assets) >= b.maxSize
	b.mu.Unlock()

	if shouldFlush {
		select {
		case b.flushChan <- struct{}{}:
		default:
		}
	}
}

// Flush 刷新缓冲区，批量保存
func (b *AssetBuffer) Flush(ctx context.Context, saver func([]*scanner.Asset)) {
	b.mu.Lock()
	assets := b.assets
	b.assets = nil
	b.mu.Unlock()

	if len(assets) > 0 {
		saver(assets) // 批量保存
	}
}

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

// getWappalyzerClient 懒初始化 wappalyzer 客户端（单例）
func (w *Worker) getWappalyzerClient() *wappalyzer.Wappalyze {
	w.wappalyzerOnce.Do(func() {
		client, err := wappalyzer.New()
		if err != nil {
			logx.Errorf("[Worker] Failed to init wappalyzer client: %v", err)
			return
		}
		w.wappalyzerClient = client
		logx.Info("[Worker] Wappalyzer client initialized")
	})
	return w.wappalyzerClient
}

// NewWorker 创建Worker
func NewWorker(config WorkerConfig) (*Worker, error) {
	// 自动获取本机IP地址
	if config.IP == "" {
		config.IP = GetLocalIP()
	}

	// 创建 HTTP 客户端（替代 RPC 和 Redis）
	httpClient := NewWorkerHTTPClient(config.ServerAddr, config.InstallKey, config.Name)

	logx.Infof("[Worker] HTTP client created, API server: %s", config.ServerAddr)

	// 创建可取消的Context
	ctx, cancel := context.WithCancel(context.Background())

	w := &Worker{
		ctx:        ctx,
		cancel:     cancel,
		config:     config,
		httpClient: httpClient,
		scanners:   make(map[string]scanner.Scanner),
		taskChan:   make(chan *scheduler.TaskInfo, config.Concurrency),
		stopChan:   make(chan struct{}),
		logger:     NewWorkerLoggerLocal(config.Name), // 使用本地日志
	}

	// Phase 2: 按需启用客户端优先级队列管理器
	// 关闭时保持原 taskChan FIFO 行为；开启时 taskQueue 接管任务排队，taskChan 仅作为并发槽位计数
	if config.EnableTaskQueueManager {
		w.taskQueue = NewTaskQueueManager(config.MaxQueueSize, config.MaxWaitTime)
		w.taskQueue.SetLogger(func(level, format string, args ...interface{}) {
			if level == "WARN" {
				w.logger.Warn("[TaskQueue] "+format, args...)
			} else {
				w.logger.Info("[TaskQueue] "+format, args...)
			}
		})
		logx.Infof("[Worker] TaskQueueManager enabled: maxQueueSize=%d, maxWaitTime=%v",
			config.MaxQueueSize, config.MaxWaitTime)
	}

	// 创建 WebSocket 客户端
	wsConfig := DefaultWSClientConfig(config.ServerAddr, config.Name, config.InstallKey)
	w.wsClient = NewWorkerWSClient(wsConfig)

	// 更新 logger 为 MongoDB 版本（直写 MongoDB）
	// globalMongoLogger 在 SetMongoDB 中初始化，此处仅创建 logger 壳
	w.logger = NewWorkerLoggerWS(config.Name)

	// 设置控制信号处理函数
	w.wsClient.SetControlHandler(func(taskId, action string) {
		w.handleControlSignal(taskId, action)
	})

	// 设置 Worker 级别控制处理函数
	w.wsClient.SetWorkerControlHandler(func(action, param string) {
		w.handleWorkerControl(action, param)
	})

	// 设置Worker信息请求处理函数

	// 注册扫描器
	w.registerScanners()

	// 初始化 IP 地理位置服务
	w.initGeolocation()

	// 创建本地结果队列
	// 修复 P0：队列目录改到挂载卷 log/ 下（cscan_worker_logs 已挂载 /app/log），
	// 避免容器 OOM 重启后本地队列文件随容器丢失；maxSize 提升至 2000，
	// 降低扫描期 API 长时间不可用时队列溢出丢弃最旧结果的风险。
	resultQueueDir := filepath.Join("log", "result_queue")
	// 回放函数必须走直写（不带 fallback），避免二次入队导致自循环膨胀。
	w.resultQueue = NewResultQueue(resultQueueDir, 2000, func(ctx context.Context, req *TaskResultReq) error {
		return w.replayAssetResult(ctx, req)
	})
	// 漏洞/JS/证书结果对称接入本地队列重放（修复 P0：原保存无重试、无队列，API 抖动即永久丢失）
	w.resultQueue.SetVulReplayFn(func(ctx context.Context, req *VulResultReq) error {
		vuls := make([]*scanner.Vulnerability, 0, len(req.Vuls))
		for i := range req.Vuls {
			vul, err := vulDocumentToScanner(&req.Vuls[i])
			if err != nil {
				return err
			}
			vuls = append(vuls, vul)
		}
		return w.saveVulResultDirect(ctx, req.MainTaskId, vuls)
	})
	w.resultQueue.SetJSReplayFn(func(ctx context.Context, req *SaveJSFinderResultReq) error {
		return w.saveJSFinderResultDirect(ctx, req.MainTaskId, req.Results)
	})
	w.resultQueue.SetCertReplayFn(func(ctx context.Context, req *SaveCertResultReq) error {
		if w.mongoDB == nil {
			return fmt.Errorf("mongoDB unavailable; cert replay requires direct MongoDB connection")
		}
		certs := make([]*scanner.CertResult, len(req.Results))
		for i, r := range req.Results {
			certs[i] = &scanner.CertResult{
				Host:         r.Host,
				Port:         r.Port,
				Authority:    r.Authority,
				Subject:      r.Subject,
				SubjectDN:    r.SubjectDN,
				Issuer:       r.Issuer,
				IssuerDN:     r.IssuerDN,
				SerialNumber: r.SerialNumber,
				SigAlg:       r.SigAlg,
				NotBefore:    r.NotBefore,
				NotAfter:     r.NotAfter,
				Version:      r.Version,
				SANs:         r.SANs,
				Fingerprints: r.Fingerprints,
				IsSelfSigned: r.IsSelfSigned,
			}
		}
		return w.saveCertResultsDirect(ctx, req.MainTaskId, certs)
	})
	w.resultQueue.SetDirScanReplayFn(func(ctx context.Context, req *SaveDirScanResultReq) error {
		return w.replayDirScanResult(ctx, req)
	})
	w.resultQueue.SetLogger(func(level, format string, args ...interface{}) {
		w.logger.Info("[ResultQueue] "+format, args...)
	})

	return w, nil
}

// SetMongoDB 设置 MongoDB 直连实例，用于扫描结果直接写入
func (w *Worker) SetMongoDB(client *mongo.Client, db *mongo.Database) {
	w.mongoClient = client
	w.mongoDB = db
	// 初始化 MongoDB 直写日志器（在 db 就绪后执行，避免 nil pointer）
	InitMongoLogger(db, w.config.Name)
	// HTTP 服务映射直连 MongoDB 读取，必须在 db 就绪后加载（不可提前到 NewWorker）
	w.loadHttpServiceMappings()
}

// SetRedis 设置 Redis 客户端并创建 SchedulerClient，用于直连 Redis 调度
func (w *Worker) SetRedis(rdb *redis.Client) {
	w.schedClient = NewSchedulerClient(rdb, w.config.Name, w.mongoDB)
	logx.Infof("[Worker] SchedulerClient initialized for direct Redis scheduling")
}

// SetNotifyService 设置通知服务，任务完成/失败时触发通知
func (w *Worker) SetNotifyService(svc *notification.Service) {
	if w.schedClient != nil {
		w.schedClient.SetNotifyService(svc)
	}
}

// handleControlSignal 处理控制信号
func (w *Worker) handleControlSignal(taskId, action string) {
	// 检查信号是否已存在，避免重复处理（防止日志刷屏）
	if existingAction, loaded := w.taskControlSignals.Load(taskId); loaded {
		if existingAction == action {
			// 信号已存在且值相同，跳过
			return
		}
	}

	w.logger.Info("Received control signal: taskId=%s, action=%s", taskId, action)

	// 存储控制信号
	w.taskControlSignals.Store(taskId, action)
	w.logger.Info("Stored control signal for task %s: %s", taskId, action)

	// 如果是STOP或PAUSE信号，也存储到主任务ID
	mainTaskId := getMainTaskId(taskId)
	if mainTaskId != taskId {
		// 检查主任务ID的信号是否已存在
		if existingAction, loaded := w.taskControlSignals.Load(mainTaskId); loaded {
			if existingAction == action {
				// 主任务信号已存在且值相同，跳过
				return
			}
		}
		w.taskControlSignals.Store(mainTaskId, action)
		w.logger.Info("Also stored control signal for main task %s: %s", mainTaskId, action)
	}
}

// handleWorkerControl 处理 Worker 级别控制命令
func (w *Worker) handleWorkerControl(action, param string) {
	w.logger.Info("Received worker control: action=%s, param=%s", action, param)

	switch action {
	case "stop":
		w.logger.Info("Stopping worker via WebSocket command (draining in-flight tasks)...")
		// 在新 goroutine 中执行停止，避免死锁（因为当前在 WebSocket 读取 goroutine 中）
		// 缺陷 5 修复：改为“排空”式停止——停止拉取新任务并等待在途任务完成，
		// 不再跳过在途任务（原 StopImmediate 会直接退出导致结果丢失）。
		go func() {
			w.drainAndExit(60*time.Second, func() { os.Exit(0) })
		}()
	case "restart":
		w.logger.Info("Restarting worker via WebSocket command (draining in-flight tasks)...")
		// 在新 goroutine 中执行重启
		go func() {
			w.drainAndExit(60*time.Second, w.restartSelf)
		}()
	case "rename":
		w.logger.Info("Renaming worker to: %s", param)
		w.config.Name = param
		// 更新 MongoDB 日志器的 worker 名称
		UpdateGlobalMongoLoggerWorkerName(param)
		// 更新日志前缀（使用 WebSocket 版本）
		w.logger = NewWorkerLoggerWS(param)
		// 立即发送心跳，让服务端更新状态
		go w.sendHeartbeat()
	case "setConcurrency":
		newConcurrency, err := strconv.Atoi(param)
		if err != nil || newConcurrency < 1 {
			w.logger.Error("Invalid concurrency value: %s", param)
			return
		}
		w.applyConcurrency(newConcurrency)
		// 立即发送心跳，让服务端更新状态
		go w.sendHeartbeat()
	default:
		w.logger.Warn("Unknown worker control action: %s", action)
	}
}

// applyConcurrency 应用新的并发数：同步配置，按需补启执行协程
// 调小并发时由任务拉取门限自然收敛，多余协程保持空闲
func (w *Worker) applyConcurrency(newConcurrency int) {
	if newConcurrency < 1 {
		return
	}

	w.mu.Lock()
	if w.config.Concurrency == newConcurrency {
		w.mu.Unlock()
		return
	}
	w.config.Concurrency = newConcurrency
	spawn := 0
	startId := w.executorCount
	if w.isRunning && newConcurrency > w.executorCount {
		spawn = newConcurrency - w.executorCount
		w.executorCount = newConcurrency
	}
	w.mu.Unlock()

	for i := 0; i < spawn; i++ {
		w.wg.Add(1)
		go w.processTaskWithRecovery(startId + i)
	}

	w.logger.Info("Concurrency applied: %d (spawned %d new executors)", newConcurrency, spawn)
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

// registerScanners 注册扫描器
func (w *Worker) registerScanners() {
	w.scanners["portscan"] = scanner.NewPortScanner()
	w.scanners["masscan"] = scanner.NewMasscanScanner()
	w.scanners["nmap"] = scanner.NewNmapScanner()
	w.scanners["fingerprintx"] = scanner.NewFingerprintxScanner()
	w.scanners["naabu"] = scanner.NewNaabuScanner()
	w.scanners["subfinder"] = scanner.NewSubfinderScanner()
	w.scanners["subdomain_bruteforce"] = scanner.NewSubdomainBruteforceScanner()
	w.scanners["fingerprint"] = scanner.NewFingerprintScanner()
	w.scanners["nuclei"] = scanner.NewNucleiScanner()
	w.scanners["ffuf"] = scanner.NewFFufScanner()
	w.scanners["feroxbuster"] = scanner.NewFeroxbusterScanner()
	w.scanners["brutescan"] = scanner.NewBruteScanScanner()
	w.scanners["jsfinder"] = scanner.NewJSFinderScanner()
}

// Start 启动Worker
func (w *Worker) Start() {
	w.isRunning = true

	// 启动本地结果队列
	if w.resultQueue != nil {
		if err := w.resultQueue.Start(w.ctx); err != nil {
			w.logger.Warn("Result queue failed to start: %v", err)
		}
	}

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

	// Worker 启动时恢复之前未完成的任务
	w.recoverOrphanedTasks()

	// Phase 2: 启用客户端优先级队列管理器时启动过期清理协程
	if w.taskQueue != nil {
		w.taskQueue.Start(w.ctx)
	}

	// 启动任务处理协程
	for i := 0; i < w.config.Concurrency; i++ {
		w.wg.Add(1)
		go w.processTaskWithRecovery(i)
	}
	w.mu.Lock()
	w.executorCount = w.config.Concurrency
	w.mu.Unlock()

	// 启动任务拉取协程
	w.wg.Add(1)
	go w.fetchTasksWithRecovery()

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
	// 构造随 stopChan 取消的 ctx,供 DequeueWait 使用
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-w.stopChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Phase 2: 启用 taskQueue 时从优先级队列出队，否则从 taskChan 读取
	for {
		select {
		case <-w.stopChan:
			w.logger.Info("processTaskLoop: received stop signal, exiting")
			return
		default:
		}

		var task *scheduler.TaskInfo
		if w.taskQueue != nil {
			// 启用优先级队列：DequeueWait 用 sync.Cond 挂起等待,替代 50ms 空轮询
			ok := false
			task, _, ok = w.taskQueue.DequeueWait(ctx)
			if !ok {
				// Stop 或 ctx 取消,退出循环
				return
			}
		} else {
			// 原 taskChan 路径
			select {
			case <-w.stopChan:
				w.logger.Info("processTaskLoop: received stop signal, exiting")
				return
			case t := <-w.taskChan:
				task = t
			}
		}

		w.logger.Info("processTaskLoop: received task %s from %s", task.TaskId, w.taskSource())
		taskCtx := context.Background()
		if ctrl := w.checkTaskControl(taskCtx, task.TaskId); ctrl == "STOP" {
			w.taskLog(task.TaskId, LevelInfo, "Task %s skipped because it was stopped while waiting in queue", task.TaskId)
			w.cleanupTaskLogger(task.TaskId)
			// 队列内被丢弃时一次性发出本批次应有的全部增量，避免 sub_task_done 永远到不了 sub_task_count
			w.incrSubTaskDone(taskCtx, task, "完成", true, w.expectedTaskIncr(task.Config))
			continue
		}
		w.logger.Info("processTaskLoop: calling executeTask for task %s", task.TaskId)
		w.executeTask(task)
		w.logger.Info("processTaskLoop: executeTask completed for task %s", task.TaskId)
	}
}

// taskSource 返回当前任务来源标签，用于日志
func (w *Worker) taskSource() string {
	if w.taskQueue != nil {
		return "taskQueue"
	}
	return "taskChan"
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
// 配合服务端长轮询机制：
//   - 有任务时：短间隔快速拉取，尽量填满 Worker 槽位
//   - 无任务时：服务端通过 Pub/Sub 长轮询 hold 请求最多 25s，
//     返回空后客户端只需短暂 sleep（1s）防止极端情况下的空转，
//     相比旧方案（空闲时 3~10s 轮询一次）大幅减少空请求
func (w *Worker) fetchTasksLoop() {
	for {
		select {
		case <-w.stopChan:
			return
		default:
			hasTask := w.pullTask()

			if hasTask {
				// 有任务时短暂等待后立即拉取下一个（尽量填满并发槽位）
				// 服务端长轮询在此场景下立即返回，不会 hold
				time.Sleep(50 * time.Millisecond)
			} else {
				// 无任务时：服务端已经通过长轮询等待了最多 25s，
				// 这里只需短暂 sleep 防止极端情况下的空转（如网络错误导致立即返回）
				time.Sleep(1 * time.Second)
			}
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

// safeGo 启动一个带 panic 恢复的 goroutine，防止子协程 panic 导致整个 Worker 进程崩溃。
// label 用于日志定位来源，fn 为实际执行函数。
func (w *Worker) safeGo(label string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("[%s] goroutine panic recovered: %v, stack: %s", label, r, string(getStackTrace()))
			}
		}()
		fn()
	}()
}

// safeGoTask 启动带 panic 恢复的 goroutine，panic 时将堆栈附加到任务日志而非仅 Worker 日志。
// 用于与具体任务绑定的后台协程（流式缓冲刷新等），便于按任务排查。
func (w *Worker) safeGoTask(taskId, label string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.taskLog(taskId, LevelError, "[%s] goroutine panic recovered: %v, stack: %s", label, r, string(getStackTrace()))
			}
		}()
		fn()
	}()
}

// pullTask 拉取单个任务，返回是否获取到任务
// 优先使用 schedClient（Redis 直连），回退到 httpClient（HTTP）
func (w *Worker) pullTask() bool {
	ctx := context.Background()

	// 修复 #27：读取 concurrency 时加 RLock，避免与 applyConcurrency 写入竞争
	w.mu.RLock()
	concurrency := w.config.Concurrency
	w.mu.RUnlock()

	// 检查是否有空闲槽位
	pendingCount := len(w.taskChan)
	if w.taskQueue != nil {
		pendingCount += w.taskQueue.Size()
	}
	if pendingCount >= concurrency {
		return false
	}

	var task *scheduler.TaskInfo

	// 优先使用 Redis 直连
	if w.schedClient != nil {
		resp, err := w.schedClient.CheckTask(ctx)
		if err != nil {
			w.logger.Debug("pullTask: schedClient.CheckTask failed: %v", err)
			return false
		}
		if resp.IsExist && !resp.IsFinished {
			task = &scheduler.TaskInfo{
				TaskId:     resp.TaskId,
				MainTaskId: resp.MainTaskId,
				TaskName:   "scan",
				Config:     resp.Config,
			}
		}
	} else {
		// 回退到 HTTP
		resp, err := w.httpClient.CheckTask(ctx)
		if err != nil {
			w.logger.Debug("pullTask: CheckTask failed: %v", err)
			return false
		}
		if resp.IsExist && !resp.IsFinished {
			task = &scheduler.TaskInfo{
				TaskId:     resp.TaskId,
				MainTaskId: resp.MainTaskId,
				TaskName:   "scan",
				Config:     resp.Config,
			}
		}
	}

	if task == nil {
		return false
	}

	w.logger.Info("pullTask: got task %s (main: %s)", task.TaskId, task.MainTaskId)

	// 启用客户端优先级队列时走 TaskQueueManager
	if w.taskQueue != nil {
		priority := GetTaskPriority(task)
		if !w.taskQueue.Enqueue(task, priority) {
			w.logger.Warn("pullTask: task %s rejected by TaskQueue (full or low-priority dropped), requeuing", task.TaskId)
			// 修复 #15：本地入队失败时回滚到 Redis 队列，避免任务在 processing 中孤立
			if w.schedClient != nil {
				if err := w.schedClient.RequeueTask(ctx, task); err != nil {
					w.logger.Error("pullTask: requeue task %s failed: %v", task.TaskId, err)
				}
			}
			return false
		}
		w.logger.Info("pullTask: task %s enqueued with priority %d (queue size: %d/%d)",
			task.TaskId, priority, w.taskQueue.Size(), concurrency)
		return true
	}

	// 修复 #8：非阻塞发送，并发上调后 taskChan 容量不匹配时不会死锁
	w.logger.Info("pullTask: pushing task %s to taskChan (channel size: %d/%d)", task.TaskId, len(w.taskChan), cap(w.taskChan))
	select {
	case w.taskChan <- task:
		w.logger.Info("pullTask: task %s pushed to taskChan successfully", task.TaskId)
		return true
	case <-w.stopChan:
		w.logger.Warn("pullTask: worker stopping, task %s not pushed", task.TaskId)
		return false
	}
}

// recoverOrphanedTasks Worker 启动时恢复之前未完成的任务
func (w *Worker) recoverOrphanedTasks() {
	w.logger.Info("Checking for orphaned tasks to recover...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := w.httpClient.RecoverTasks(ctx)
	if err != nil {
		w.logger.Warn("Failed to recover orphaned tasks: %v", err)
		return
	}

	if resp.RecoveredCount > 0 {
		w.logger.Info("Recovered %d orphaned tasks:", resp.RecoveredCount)
		for _, task := range resp.RecoveredTasks {
			w.logger.Info("  - Task %s (status: %s)", task.TaskId, task.Status)
		}
	} else {
		w.logger.Info("No orphaned tasks found")
	}
}

// Stop 停止Worker
func (w *Worker) Stop() {
	w.isRunning = false

	// 停止本地结果队列
	if w.resultQueue != nil {
		w.resultQueue.Stop()
	}

	// Phase 2: 停止客户端优先级队列管理器
	if w.taskQueue != nil {
		w.taskQueue.Stop()
	}

	// 通知服务器Worker即将离线，删除Redis状态数据
	w.notifyOffline()

	w.cancel()
	w.stopOnce.Do(func() { close(w.stopChan) })

	// 关闭 WebSocket 客户端
	if w.wsClient != nil {
		w.wsClient.Close()
	}

	w.wg.Wait()

	// 修复 #7：flush MongoDB 日志缓冲，避免停机/重启时丢失未落库的关键日志
	if globalMongoLogger != nil {
		globalMongoLogger.Close()
	}

	// 关闭 MongoDB 直连连接池（在途写入已随 wg.Wait 结束）
	if w.mongoClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := w.mongoClient.Disconnect(ctx); err != nil {
			logx.Errorf("[Worker][MongoDirect] disconnect failed: %v", err)
		}
	}

	// 清理 chromedp 全局浏览器进程，防止 Chrome 进程残留
	scanner.CleanupChromedp()

	w.logger.Info("Worker %s stopped", w.config.Name)
}

// drainAndExit 优雅停止 Worker：先停止拉取新任务（关闭 stopChan），
// 再等待在途任务执行完成（受 timeout 约束），超时后强制退出，避免任务永久挂起
// 导致进程无法退出。修复缺陷 5：替代 StopImmediate 的“跳过在途任务”行为。
func (w *Worker) drainAndExit(timeout time.Duration, after func()) {
	done := make(chan struct{})
	go func() {
		// Stop() 内部会 wg.Wait()，等待当前 executeTask 及其子 goroutine 完成
		w.Stop()
		close(done)
	}()
	select {
	case <-done:
		w.logger.Info("Worker %s drained and stopped", w.config.Name)
	case <-time.After(timeout):
		w.logger.Warn("Worker %s graceful stop timed out after %s, forcing exit (in-flight tasks may be interrupted)",
			w.config.Name, timeout)
	}
	if after != nil {
		after()
	}
}

// notifyOffline 通知服务器Worker即将离线
func (w *Worker) notifyOffline() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if w.schedClient != nil {
		if err := w.schedClient.NotifyOffline(ctx); err != nil {
			w.logger.Warn("Failed to notify offline via Redis: %v", err)
		} else {
			w.logger.Info("Notified offline via Redis")
		}
		return
	}

	if w.httpClient == nil {
		return
	}

	_, err := w.httpClient.NotifyOffline(ctx)
	if err != nil {
		w.logger.Warn("Failed to notify server about offline: %v", err)
	} else {
		w.logger.Info("Notified server about offline")
	}
}

// SubmitTask 提交任务
// Phase 2: 启用 taskQueue 时走优先级入队，否则保持原 taskChan 路径
func (w *Worker) SubmitTask(task *scheduler.TaskInfo) {
	if w.taskQueue != nil {
		priority := GetTaskPriority(task)
		w.taskQueue.Enqueue(task, priority)
		return
	}
	w.taskChan <- task
}

// checkTaskControl 检查任务控制信号
// 返回: "PAUSE" - 暂停, "STOP" - 停止, "" - 继续执行

// handleTaskControl 统一处理任务控制信号(STOP/PAUSE)
// 返回 true 表示任务被中止或暂停，调用方应直接 return
func (w *Worker) handleTaskControl(ctx context.Context, task *scheduler.TaskInfo, completedPhases map[string]bool, assets []*scanner.Asset, phaseName string) bool {
	if ctrl := w.checkTaskControl(ctx, task.TaskId); ctrl == "STOP" {
		if phaseName != "" {
			w.taskLog(task.TaskId, LevelInfo, "Task stopped during %s", phaseName)
		} else {
			w.taskLog(task.TaskId, LevelInfo, "Task stopped")
		}
		return true
	} else if ctrl == "PAUSE" {
		if phaseName != "" {
			w.taskLog(task.TaskId, LevelInfo, "Task paused during %s, saving progress...", phaseName)
		} else {
			w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
		}
		w.saveTaskProgress(ctx, task, completedPhases, assets)
		return true
	}
	return false
}

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

// saveTaskProgress 保存任务进度（用于暂停后继续扫描)
func (w *Worker) saveTaskProgress(ctx context.Context, task *scheduler.TaskInfo, completedPhases map[string]bool, assets []*scanner.Asset) {
	saveCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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

	if w.schedClient != nil {
		w.schedClient.UpdateTask(saveCtx, task.TaskId, "PAUSED", 0, string(stateJson))
	} else {
		w.httpClient.UpdateTask(saveCtx, &TaskUpdateReq{
			TaskId: task.TaskId,
			State:  "PAUSED",
			Result: string(stateJson),
		})
	}
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
					w.taskLog(taskId, LevelInfo, "Task %s received %s signal, canceling context", taskId, ctrl)
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
	// 添加 panic 恢复机制，防止单个任务的 panic 导致整个 Worker 挂掉
	defer func() {
		if r := recover(); r != nil {
			// 使用 Worker 级别 logger，避免在 cleanupTaskLogger 之后创建孤儿 task logger
			w.logger.Error("[Task:%s] Task execution panic recovered: %v, stack: %s", task.TaskId, r, string(getStackTrace()))
			ctx := context.Background()
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, fmt.Sprintf("Task panic: %v", r))
		}
	}()

	// baseCtx 注入整体超时上限：修复 H2，防止单个任务因 baseCtx 无超时而无限期占用并发槽位。
	// 各子阶段仍可用自身更短的超时覆盖；此处仅为兜底，避免卡死。
	overallTimeout := resolveTaskOverallTimeout(w.config.Timeout)
	baseCtx, baseCancel := context.WithTimeout(context.Background(), overallTimeout)
	defer baseCancel()
	startTime := time.Now()
	w.taskLog(task.TaskId, LevelInfo, "=== executeTask START === TaskId: %s, MainTaskId: %s, overallTimeout=%s", task.TaskId, task.MainTaskId, overallTimeout)

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

		// 清理任务日志记录器，flush 残余缓冲
		w.cleanupTaskLogger(task.TaskId)
	}()

	// 兜底递增子任务计数器：正常流程中每个模块完成时已递增，此 defer 仅在异常退出（STOP/致命错误/panic）时补递增。
	// PAUSE 信号下跳过，恢复后由新一轮执行递增。
	// 此 defer 在 cleanup state defer 之后注册，按 LIFO 先执行，可在控制信号被清除前读取。
	finalIncrSent := false
	targetCount := 0  // 仅用于日志展示
	expectedIncr := 1 // 单 sub-task 应发增量 = enabledModules + 1，配置解析后赋真值
	incrSent := 0     // 已发出的增量数
	defer func() {
		if finalIncrSent {
			return
		}
		bgCtx := context.Background()
		if ctrl := w.checkTaskControl(bgCtx, task.TaskId); ctrl == "PAUSE" {
			return
		}
		remaining := expectedIncr - incrSent
		if remaining < 1 {
			remaining = 1
		}
		w.incrSubTaskDone(bgCtx, task, "完成", true, remaining)
	}()

	// 检查是否有停止信号（任务可能在队列中被停止)
	if ctrl := w.checkTaskControl(baseCtx, task.TaskId); ctrl == "STOP" {
		w.taskLog(task.TaskId, LevelInfo, "Task %s was stopped before execution", task.TaskId)
		return
	}
	w.taskLog(task.TaskId, LevelInfo, "Step 1: Control check passed")

	// 创建带有任务控制信号检查的上下文
	ctx, cancelTask := w.createTaskContext(baseCtx, task.TaskId)
	defer cancelTask()
	w.taskLog(task.TaskId, LevelInfo, "Step 2: Context created")

	// 更新任务状态
	w.taskLog(task.TaskId, LevelInfo, "Step 3: Updating task status to STARTED")
	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusStarted, "")
	w.taskLog(task.TaskId, LevelInfo, "Step 4: Task status updated")

	// 解析任务配置
	w.taskLog(task.TaskId, LevelInfo, "Step 5: Parsing task config, length=%d", len(task.Config))
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		w.taskLog(task.TaskId, LevelError, "Step 5 FAILED: Config parse error: %v", err)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "配置解析失败: "+err.Error())
		return
	}
	w.taskLog(task.TaskId, LevelInfo, "Step 6: Config parsed successfully, keys=%d", len(taskConfig))

	// 计算本 sub-task 应发出的总增量数 = 启用模块数 + 1，供早退路径与最终路径补齐使用
	expectedIncr = utils.CountEnabledModules(taskConfig) + 1

	// 检查任务类型，处理POC验证任务
	taskType, _ := taskConfig["taskType"].(string)
	if taskType == "" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7: Task type: normal scan")
	} else {
		w.taskLog(task.TaskId, LevelInfo, "Step 7: Task type: '%s'", taskType)
	}
	if taskType == "poc_validate" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7a: Executing POC validate task")
		w.executePocValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "poc_batch_validate" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7b: Executing POC batch validate task")
		w.executePocBatchValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "fingerprint_validate" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7c: Executing fingerprint validate task")
		w.executeFingerprintValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "fingerprint_batch_validate" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7c2: Executing fingerprint batch validate task")
		w.executeFingerprintBatchValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "active_fingerprint_validate" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7d: Executing active fingerprint validate task")
		w.executeActiveFingerprintValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "active_fingerprint_batch_validate" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7d2: Executing active fingerprint batch validate task")
		w.executeActiveFingerprintBatchValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "vuln_reverify" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7e: Executing vuln reverify task")
		w.executeVulnReverifyTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "reverify_weakpass" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7f: Executing weakpass reverify task")
		w.executeReverifyWeakpassTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "reverify_exposure" {
		w.taskLog(task.TaskId, LevelInfo, "Step 7g: Executing exposure reverify task")
		w.executeReverifyExposureTask(ctx, task, taskConfig, startTime)
		return
	}

	// 获取目标
	target, _ := taskConfig["target"].(string)
	if target == "" {
		w.taskLog(task.TaskId, LevelError, "Step 8 FAILED: Target is empty")
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "Target is empty")
		return
	}

	// 获取组织ID
	orgId, _ := taskConfig["orgId"].(string)
	w.taskLog(task.TaskId, LevelInfo, "Task started")
	w.taskLog(task.TaskId, LevelDebug, "Full task config: %s", task.Config)

	var allAssets []*scanner.Asset
	var allVuls []*scanner.Vulnerability
	var skippedHosts []string // 因端口阈值超限被跳过的主机

	// 解析扫描配置
	config, err := scheduler.ParseTaskConfig(task.Config)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Failed to parse task config: %v", err)
		w.taskLog(task.TaskId, LevelDebug, "Raw config string: %s", task.Config)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "配置解析失败: "+err.Error())
		return
	}
	if config == nil {
		w.taskLog(task.TaskId, LevelError, "Task config is nil after parsing")
		w.taskLog(task.TaskId, LevelDebug, "Raw config string: %s", task.Config)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "任务配置为空")
		return
	}

	// 输出任务开始日志（包含关键配置信息）
	var enabledPhases []string
	var configDetails []string

	if config.DomainScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("DomainScan.Enable=%v", config.DomainScan.Enable))
		if config.DomainScan.Enable {
			enabledPhases = append(enabledPhases, "Domain Scan")
		}
	} else {
		configDetails = append(configDetails, "DomainScan=nil")
	}

	if config.PortScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("PortScan.Enable=%v", config.PortScan.Enable))
		if config.PortScan.Enable {
			enabledPhases = append(enabledPhases, "Port Scan")
		}
	} else {
		configDetails = append(configDetails, "PortScan=nil")
	}

	if config.PortIdentify != nil {
		configDetails = append(configDetails, fmt.Sprintf("PortIdentify.Enable=%v", config.PortIdentify.Enable))
		if config.PortIdentify.Enable {
			enabledPhases = append(enabledPhases, "Port Identify")
		}
	} else {
		configDetails = append(configDetails, "PortIdentify=nil")
	}

	if config.Fingerprint != nil {
		configDetails = append(configDetails, fmt.Sprintf("Fingerprint.Enable=%v", config.Fingerprint.Enable))
		if config.Fingerprint.Enable {
			enabledPhases = append(enabledPhases, "Fingerprint")
		}
	} else {
		configDetails = append(configDetails, "Fingerprint=nil")
	}

	if config.BruteScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("BruteScan.Enable=%v", config.BruteScan.Enable))
		if config.BruteScan.Enable {
			enabledPhases = append(enabledPhases, "Brute Scan")
			w.taskLog(task.TaskId, LevelInfo, "BruteScan config: services=%v, threads=%d, timeout=%d, dictIds=%v",
				config.BruteScan.Services, config.BruteScan.Threads, config.BruteScan.Timeout, config.BruteScan.WeakpassDictIds)
		}
	} else {
		configDetails = append(configDetails, "BruteScan=nil")
	}

	if config.DirScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("DirScan.Enable=%v", config.DirScan.Enable))
		if config.DirScan.Enable {
			enabledPhases = append(enabledPhases, "Dir Scan")
		}
	} else {
		configDetails = append(configDetails, "DirScan=nil")
	}

	if config.JSFinder != nil {
		configDetails = append(configDetails, fmt.Sprintf("JSFinder.Enable=%v", config.JSFinder.Enable))
		if config.JSFinder.Enable {
			enabledPhases = append(enabledPhases, "JSFinder")
		}
	} else {
		configDetails = append(configDetails, "JSFinder=nil")
	}

	if config.PocScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("PocScan.Enable=%v", config.PocScan.Enable))
		if config.PocScan.Enable {
			enabledPhases = append(enabledPhases, "POC Scan")
		}
	} else {
		configDetails = append(configDetails, "PocScan=nil")
	}

	w.taskLog(task.TaskId, LevelInfo, "Config parsed: %s", strings.Join(configDetails, ", "))

	// 检查是否有启用的扫描阶段
	if len(enabledPhases) == 0 {
		w.taskLog(task.TaskId, LevelError, "No scan phases enabled in config")
		w.taskLog(task.TaskId, LevelError, "Config details: %s", strings.Join(configDetails, ", "))
		w.taskLog(task.TaskId, LevelDebug, "Full config JSON: %s", task.Config)
		w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusFailure, "未启用任何扫描阶段")
		return
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

	// 应用全局黑名单过滤
	blacklistMatcher := w.getBlacklistMatcher(ctx, task.TaskId)
	if blacklistMatcher != nil && !blacklistMatcher.IsEmpty() {
		originalCount := len(targets)
		blacklistedTargets := blacklistMatcher.GetBlacklistedTargets(targets)
		targets = blacklistMatcher.FilterTargets(targets)
		if len(blacklistedTargets) > 0 {
			w.taskLog(task.TaskId, LevelInfo, "Blacklist: filtered %d/%d targets", len(blacklistedTargets), originalCount)
			for _, t := range blacklistedTargets {
				w.taskLog(task.TaskId, LevelDebug, "Blacklist: skipped target: %s", t)
			}
		}
		if len(targets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "All targets filtered by blacklist, marking task as complete")
			blacklistRemaining := expectedIncr - incrSent
			if blacklistRemaining < 1 {
				blacklistRemaining = 1
			}
			w.incrSubTaskDone(ctx, task, "完成", true, blacklistRemaining)
			finalIncrSent = true
			w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusSuccess, "All targets filtered by blacklist")
			return
		}
	}

	// 输出任务开始日志
	targetCount = len(targets)
	w.taskLog(task.TaskId, LevelInfo, "Starting: %s", strings.Join(enabledPhases, " → "))
	w.taskLog(task.TaskId, LevelInfo, "Targets (%d): %s", targetCount, strings.Join(targets, ", "))

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

		// 子域名扫描仅针对“纯一级域名”：IP/CIDR/端口范围、以及子域名(www.example.com)
		// 直接跳过，不做子域名枚举；多段公共后缀(com.cn 等)下的一级域名为 eTLD+1(如 example.com.cn)。
		// 模块自行判断输入适用性，编排层不再按目标类型预关模块。
		eligibleTargets := filterEligibleSubdomainTargets(targets)
		if len(eligibleTargets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Domain scan: skipped (no eligible registrable domains; IP/subdomain targets are not scanned for subdomains)")
			completedPhases["domainscan"] = true
			w.incrSubTaskDone(ctx, task, "子域名扫描", false, 1)
			incrSent++
			goto domainScanDone
		}
		domainScanTarget := strings.Join(eligibleTargets, "\n")

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task.TaskId, 10, "子域名扫描中", "子域名扫描")
		w.taskLog(task.TaskId, LevelInfo, "Starting domain scan...")

		// 创建任务日志回调
		domainTaskLogger := func(level, format string, args ...interface{}) {
			w.taskLog(task.TaskId, level, format, args...)
		}

		// 直连 MongoDB 获取 Subfinder 配置
		var providerConfig map[string][]string
		providerResp, err := w.loadSubfinderProviders(ctx)
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
			Concurrent:         w.config.Concurrency, // 域名解析并发与 Worker 并发对齐
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
					Target:     domainScanTarget,
					MainTaskId: task.MainTaskId,
					Options:    subfinderOpts,
					TaskLogger: domainTaskLogger,
					OnProgress: w.makeOnProgress(task.MainTaskId, "子域名扫描"),
					OnTargetDone: func(domain string, assets []*scanner.Asset) {
						// 流式入库：单域名子域名枚举完成立即保存
						if len(assets) == 0 {
							return
						}
						for _, asset := range assets {
							asset.Source = "subfinder"
						}
						w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, assets)
					},
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

		// Subfinder 结果已通过 OnTargetDone 流式入库

		// 执行子域名暴力破解（如果配置了字典）
		var bruteforceAssets []*scanner.Asset
		if len(config.DomainScan.SubdomainDictIds) > 0 {
			w.taskLog(task.TaskId, LevelInfo, "Starting subdomain bruteforce with %d dicts", len(config.DomainScan.SubdomainDictIds))

			// 获取字典内容
			dictResp, err := w.loadSubdomainDicts(ctx, config.DomainScan.SubdomainDictIds)
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
						Timeout:        config.DomainScan.BruteforceTimeout * 60, // 转换为秒
						WildcardFilter: config.DomainScan.RemoveWildcard,
						ResolveDNS:     config.DomainScan.ResolveDNS,
						Concurrent:     w.config.Concurrency * 10,
						// 引擎配置
						Engine:       config.DomainScan.BruteforceEngine,
						Bandwidth:    config.DomainScan.Bandwidth,
						Retry:        config.DomainScan.Retry,
						WildcardMode: config.DomainScan.WildcardMode,
						// 增强功能配置
						RecursiveBrute: config.DomainScan.RecursiveBrute,
						RecursiveDepth: 2,
						WildcardDetect: config.DomainScan.WildcardDetect,
					}

					// 获取递归爆破字典（如果启用了递归爆破）
					if config.DomainScan.RecursiveBrute && len(config.DomainScan.RecursiveDictIds) > 0 {
						recursiveDictResp, err := w.loadSubdomainDicts(ctx, config.DomainScan.RecursiveDictIds)
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
						// 预计算 subfinder 已发现的域名，用于回调中过滤增量资产
						subfinderHosts := make(map[string]bool)
						for _, asset := range subfinderAssets {
							if asset.Host != "" {
								subfinderHosts[asset.Host] = true
							}
						}
						bruteResult, err := bruteScanner.Scan(ctx, &scanner.ScanConfig{
							Target:     domainScanTarget,
							MainTaskId: task.MainTaskId,
							Options:    bruteforceOpts,
							TaskLogger: domainTaskLogger,
							OnProgress: w.makeOnProgress(task.MainTaskId, "子域名爆破"),
							OnTargetDone: func(domain string, assets []*scanner.Asset) {
								// 流式入库：单域名暴力破解完成立即保存增量资产
								var newAssets []*scanner.Asset
								for _, asset := range assets {
									if asset.Host != "" && !subfinderHosts[asset.Host] {
										asset.Source = "bruteforce"
										newAssets = append(newAssets, asset)
									}
								}
								if len(newAssets) > 0 {
									w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, newAssets)
								}
							},
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

		// 转换为切片
		var mergedAssets []*scanner.Asset
		for _, asset := range allAssetMap {
			mergedAssets = append(mergedAssets, asset)
		}

		// 应用黑名单过滤子域名结果
		if blacklistMatcher != nil && !blacklistMatcher.IsEmpty() {
			mergedAssets = w.filterAssetsByBlacklist(mergedAssets, blacklistMatcher, task.TaskId)
		}

		// 应用端口扫描排除目标过滤子域名解析的IP
		if config.PortScan != nil && config.PortScan.ExcludeHosts != "" {
			excludeMatcher := utils.NewExcludeHostsMatcher(config.PortScan.ExcludeHosts)
			if excludeMatcher != nil && !excludeMatcher.IsEmpty() {
				originalCount := len(mergedAssets)
				mergedAssets = w.filterAssetsByExcludeHosts(mergedAssets, excludeMatcher, task.TaskId)
				if filteredCount := originalCount - len(mergedAssets); filteredCount > 0 {
					w.taskLog(task.TaskId, LevelInfo, "ExcludeHosts: filtered %d subdomains by resolved IP", filteredCount)
				}
			}
		}

		if len(mergedAssets) > 0 {
			allAssets = append(allAssets, mergedAssets...)
		}
		if w.handleTaskControl(ctx, task, completedPhases, allAssets, "domain scan") {
			return
		}

		// 检查context是否被取消
		select {
		case <-ctx.Done():
			w.taskLog(task.TaskId, LevelInfo, "Domain scan canceled by context")
			// 资产已在各阶段完成后立即保存，此处只需保存任务进度
			w.saveTaskProgress(ctx, task, completedPhases, allAssets)
			return
		default:
		}

		// 暴力破解结果已通过 OnTargetDone 流式入库，此处无需重复保存

		if len(mergedAssets) > 0 {
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
		w.incrSubTaskDone(ctx, task, "子域名扫描", false, 1)
		incrSent++
	}

domainScanDone:

	// 执行端口扫描（只有明确启用时才执行）
	if config.PortScan != nil && config.PortScan.Enable && !completedPhases["portscan"] {
		// 检查控制信号
		if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
			return
		}

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task.TaskId, 20, "端口扫描中", "端口扫描")

		// Naabu 内部 worker pool 用于并行启动多个 naabu 进程（每目标一个进程）。
		// 此值不应等于 Worker 子任务并发数，否则 N 个子任务各开 N 个进程 = N² 进程爆炸。
		// 全局信号量（naabuSem）已兜底限制总进程数 ≤ 5，此处仅控制单子任务内的并行度。
		if config.PortScan.Workers <= 0 {
			config.PortScan.Workers = 2
		}

		// 端口扫描在 naabu/masscan 内部按目标串行执行（见 scanner/naabu.go runNaabuWithLogger）
		// 故总超时 = 用户单目标超时 × 目标数；并基于端口数/速率/重试估算下限，避免大端口范围低速率被截断
		singleTimeout := config.PortScan.Timeout
		if singleTimeout <= 0 {
			singleTimeout = 5
		}
		// 粗略估算目标数（按换行分割）
		portTargetCount := len(strings.Split(strings.TrimSpace(target), "\n"))
		if portTargetCount <= 0 {
			portTargetCount = 1
		}

		portScanTimeout := singleTimeout * portTargetCount

		// 估算最小耗时：每目标 ports*(1+retries)*1.5/rate 秒（1.5 为安全系数）
		portCount := scanner.EstimatePortCount(config.PortScan.Ports)
		rate := config.PortScan.Rate
		if rate <= 0 {
			rate = 1000
		}
		retries := config.PortScan.Retries
		if retries < 0 {
			retries = 0
		}
		estimatedPerTarget := portCount * (1 + retries) * 3 / (rate * 2)
		if estimatedPerTarget < 60 {
			estimatedPerTarget = 60
		}
		minTotal := estimatedPerTarget * portTargetCount
		if portScanTimeout < minTotal {
			portScanTimeout = minTotal
		}

		w.taskLog(task.TaskId, LevelInfo, "Port scan: timeout=%ds (single=%ds, targets=%d, ports=%d, rate=%d, estimatedPerTarget=%ds)",
			portScanTimeout, singleTimeout, portTargetCount, portCount, rate, estimatedPerTarget)
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

		// 创建进度回调（统一使用 makeOnProgress，基于分子/分母实时计算主任务进度）
		onProgress := w.makeOnProgress(task.MainTaskId, "端口扫描")

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
			if masscanResult != nil {
				if len(masscanResult.Assets) > 0 {
					openPorts = masscanResult.Assets
					w.taskLog(task.TaskId, LevelInfo, "Found %d open ports", len(openPorts))
				}
				if len(masscanResult.SkippedHosts) > 0 {
					skippedHosts = append(skippedHosts, masscanResult.SkippedHosts...)
				}
				if len(masscanResult.DNSFailedHosts) > 0 {
					skippedHosts = append(skippedHosts, masscanResult.DNSFailedHosts...)
					w.taskLog(task.TaskId, LevelInfo, "DNS resolution failed for %d hosts, will skip in subsequent phases", len(masscanResult.DNSFailedHosts))
				}
			}
		default: // naabu
			w.taskLog(task.TaskId, LevelInfo, "Port scan: Naabu")
			naabuScanner := w.scanners["naabu"]
			naabuResult, err := naabuScanner.Scan(portCtx, &scanner.ScanConfig{
				Target:     target,
				Options:    config.PortScan,
				TaskLogger: taskLogger,
				OnProgress: onProgress,
				OnTargetDone: func(target string, assets []*scanner.Asset) {
					// 流式入库：单目标端口扫描完成立即保存
					if len(assets) == 0 {
						return
					}
					for _, asset := range assets {
						asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
					}
					w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, assets)
				},
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
			if naabuResult != nil {
				if len(naabuResult.Assets) > 0 {
					openPorts = naabuResult.Assets
					w.taskLog(task.TaskId, LevelInfo, "Found %d open ports", len(openPorts))
				}
				if len(naabuResult.SkippedHosts) > 0 {
					skippedHosts = append(skippedHosts, naabuResult.SkippedHosts...)
				}
				if len(naabuResult.DNSFailedHosts) > 0 {
					skippedHosts = append(skippedHosts, naabuResult.DNSFailedHosts...)
					w.taskLog(task.TaskId, LevelInfo, "DNS resolution failed for %d hosts, will skip in subsequent phases", len(naabuResult.DNSFailedHosts))
				}
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
			// 结果已通过 OnTargetDone 流式入库
		} else {
			w.taskLog(task.TaskId, LevelInfo, "No open ports found")
		}

		portCancel() // 释放端口扫描上下文
		if len(skippedHosts) > 0 {
			w.taskLog(task.TaskId, LevelWarn, "Port scan: %d hosts skipped due to port threshold: %v", len(skippedHosts), skippedHosts)
		}
		completedPhases["portscan"] = true
		// 端口扫描模块完成，递增子任务进度
		w.incrSubTaskDone(ctx, task, "端口扫描", false, 1)
		incrSent++
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
		return
	}

	// 执行端口识别（Nmap服务识别）- 独立阶段
	if config.PortIdentify != nil && config.PortIdentify.Enable && !completedPhases["portidentify"] {
		// 强制扫描模式：没有资产时从用户输入目标生成资产
		// GenerateAssetsFromTargets 已过滤DNS解析失败的域名
		if len(allAssets) == 0 && target != "" && config.PortIdentify.ForceScan {
			generatedAssets := scanner.GenerateAssetsFromTargets(target)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
				w.taskLog(task.TaskId, LevelInfo, "Port identify: generated %d assets from target (force scan)", len(generatedAssets))
			}
		}

		// 没有资产时跳过实际扫描，但仍需递增进度
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Port identify: skipped (no assets)")
			completedPhases["portidentify"] = true
			w.incrSubTaskDone(ctx, task, "端口识别", false, 1)
			incrSent++
		} else {
			// 检查控制信号
			if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task.TaskId, 40, "端口识别中", "端口识别")

			identifiedAssets := w.executePortIdentify(ctx, task, allAssets, config.PortIdentify, orgId)
			if len(identifiedAssets) > 0 {
				// 合并而非替换：保留 nmap 未处理的域名资产（port=0），
				// 避免 subfinder 发现的子域名在内存中丢失导致后续阶段无法扫描。
				identifiedHostPorts := make(map[string]bool)
				for _, a := range identifiedAssets {
					identifiedHostPorts[fmt.Sprintf("%s:%d", a.Host, a.Port)] = true
				}
				for _, a := range allAssets {
					if a.Port == 0 && !identifiedHostPorts[fmt.Sprintf("%s:%d", a.Host, a.Port)] {
						identifiedAssets = append(identifiedAssets, a)
					}
				}
				allAssets = identifiedAssets
				// 结果已通过 executePortIdentify 内部流式入库
			}
			completedPhases["portidentify"] = true
			// 端口识别模块完成，递增子任务进度
			w.incrSubTaskDone(ctx, task, "端口识别", false, 1)
			incrSent++
		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
		return
	}

	// 执行指纹识别
	if config.Fingerprint != nil && config.Fingerprint.Enable && !completedPhases["fingerprint"] {
		// 强制扫描模式：没有资产时从用户输入目标生成资产
		// GenerateAssetsFromTargets 已过滤DNS解析失败的域名
		if len(allAssets) == 0 && target != "" && config.Fingerprint.ForceScan {
			generatedAssets := scanner.GenerateAssetsFromTargets(target)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
				w.taskLog(task.TaskId, LevelInfo, "Fingerprint: generated %d assets from target (force scan)", len(generatedAssets))
			}
		}

		// 没有资产时跳过实际扫描，但仍需递增进度
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Fingerprint: skipped (no assets)")
			completedPhases["fingerprint"] = true
			w.incrSubTaskDone(ctx, task, "指纹识别", false, 1)
			incrSent++
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
				// 根据过滤模式处理资产
				assetsToScan := allAssets
				filterMode := config.Fingerprint.FilterMode
				if filterMode == "" {
					filterMode = "http_mapping" // 默认使用HTTP映射模式
				}

				if filterMode == "service_mapping" {
					// 模式B：使用服务映射过滤非HTTP服务
					var httpAssets []*scanner.Asset
					nonHttpCount := 0

					for _, asset := range allAssets {
						// 通过全局HTTP服务检查器判断服务是否为HTTP
						// 如果服务映射中明确标识为非HTTP，则排除
						if globalHttpServiceChecker := scanner.GetHttpServiceChecker(); globalHttpServiceChecker != nil {
							serviceLower := strings.ToLower(asset.Service)
							if isHttp, found := globalHttpServiceChecker.IsHttpService(serviceLower); found {
								if !isHttp {
									// 服务映射中明确标识为非HTTP，排除
									nonHttpCount++
									continue
								}
							}
							// 未在服务映射中找到或标识为HTTP，保留
						}
						httpAssets = append(httpAssets, asset)
					}

					assetsToScan = httpAssets
					w.taskLog(task.TaskId, LevelInfo, "Fingerprint: FilterMode=service_mapping, filtered %d assets (excluded %d non-HTTP services), remaining %d assets",
						len(allAssets), nonHttpCount, len(httpAssets))
				} else {
					// 模式A：使用HTTP映射，过滤非HTTP资产
					var httpAssets []*scanner.Asset
					nonHttpCount := 0

					for _, asset := range allAssets {
						if scanner.IsHttpAsset(asset) {
							httpAssets = append(httpAssets, asset)
						} else {
							nonHttpCount++
						}
					}

					assetsToScan = httpAssets
					w.taskLog(task.TaskId, LevelInfo, "Fingerprint: FilterMode=http_mapping, filtered %d assets (excluded %d non-HTTP assets), remaining %d assets",
						len(allAssets), nonHttpCount, len(httpAssets))
				}

				// 获取单目标超时配置
				targetTimeout := config.Fingerprint.TargetTimeout
				if targetTimeout <= 0 {
					targetTimeout = 30 // 默认30秒
				}
				// 使用Worker并发数覆盖配置中的并发数
				config.Fingerprint.Concurrency = w.config.Concurrency
				w.taskLog(task.TaskId, LevelInfo, "Fingerprint: %d assets, timeout %ds/target, concurrency=%d, activeScan=%v, filterMode=%s",
					len(assetsToScan), targetTimeout, w.config.Concurrency, config.Fingerprint.ActiveScan, filterMode)

				// 每次扫描前实时加载HTTP服务映射配置
				w.loadHttpServiceMappings()

				// 如果启用自定义指纹引擎，加载自定义指纹（包括主动指纹）
				if config.Fingerprint.CustomEngine {
					w.loadCustomFingerprints(ctx, s.(*scanner.FingerprintScanner), config.Fingerprint.ActiveScan)
				}

				// 并发数上限与扫描器内部一致（≤5）
				fpConcurrency := config.Fingerprint.Concurrency
				if fpConcurrency <= 0 {
					fpConcurrency = 1
				}
				if fpConcurrency > 5 {
					fpConcurrency = 5
				}
				// 超时按单目标单模块独立控制，不再设置会饿死下游模块的阶段级紧总超时：
				//   - httpx：执行器按单目标超时控制（targetTimeout+10s/目标）
				//   - 指纹：worker pool 派生的 targetCtx 按单目标超时控制
				//   - 截图：takeScreenshot 独立预算（与 httpx 解耦，60s/张兜底）
				// fpCtx 仅承载任务级取消信号（STOP/PAUSE），不再自带 deadline；
				// 最终由父任务 baseCtx（6h）统一兜底，避免任一模块吃满总额导致其它模块被异常跳过。
				w.taskLog(task.TaskId, LevelInfo, "Fingerprint: per-target timeout=%ds, assets=%d, concurrency=%d (no phase total; modules bounded independently)",
					targetTimeout, len(assetsToScan), fpConcurrency)
				fpCtx, fpCancel := context.WithCancel(ctx)

				// 创建任务日志回调
				fpTaskLogger := func(level, format string, args ...interface{}) {
					w.taskLog(task.TaskId, level, format, args...)
				}

				// 创建流式资产缓冲区，满10个或每3秒触发批量保存
				assetBuffer := NewAssetBuffer(10)
				w.safeGoTask(task.TaskId, "fingerprint-asset-flush", func() {
					ticker := time.NewTicker(3 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-assetBuffer.GetFlushChan():
							assetBuffer.Flush(ctx, func(assets []*scanner.Asset) {
								w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, assets)
							})
						case <-ticker.C:
							assetBuffer.Flush(ctx, func(assets []*scanner.Asset) {
								w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, assets)
							})
						case <-fpCtx.Done():
							return
						}
					}
				})

				result, err := s.Scan(fpCtx, &scanner.ScanConfig{
					Assets:     assetsToScan,
					Options:    config.Fingerprint,
					TaskLogger: fpTaskLogger,
					OnAssetUpdated: func(asset *scanner.Asset) {
						copiedAsset := *asset
						assetBuffer.Add(&copiedAsset)
					},
					OnCertFound: func(cert *scanner.CertResult) {
						// 流式入库：单证书采集完成立即保存
						w.saveCertResultsWithFallback(ctx, task.MainTaskId, []*scanner.CertResult{cert})
					},
				})
				fpCancel()

				// fpCtx 现为取消专用（无自带 deadline），阶段级超时由各模块单目标超时独立控制；
				// 任务级取消/停止由下方 ctx.Err() 与 STOP 检查统一判定。
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
							originalAsset.IsHTTP = fpAsset.IsHTTP
							if len(fpAsset.IconData) > 0 {
								originalAsset.IconData = fpAsset.IconData
							}
							originalAsset.Screenshot = fpAsset.Screenshot
						}
					}

					// 刷新流式缓冲区剩余资产
					assetBuffer.Flush(ctx, func(assets []*scanner.Asset) {
						w.saveAssetResultWithFallback(ctx, task.MainTaskId, orgId, assets)
					})

					// 证书采集结果已通过 OnCertFound 流式入库
				}
			}
			completedPhases["fingerprint"] = true
			// 指纹识别模块完成，递增子任务进度
			w.incrSubTaskDone(ctx, task, "指纹识别", false, 1)
			incrSent++
		} // 结束 len(allAssets) > 0 的 else 分支
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
		return
	}

	// 执行弱口令扫描（在指纹识别之后、目录扫描之前）
	if config.BruteScan != nil && config.BruteScan.Enable && !completedPhases["brutescan"] {
		w.taskLog(task.TaskId, LevelInfo, "Brute scan: starting, total assets=%d, forceScan=%v, services=%v", len(allAssets), config.BruteScan.ForceScan, config.BruteScan.Services)

		// 强制扫描模式：没有资产时从用户输入目标生成资产
		// 注意：如果目标携带端口（如 192.168.1.215:63791），会保留该端口进行扫描
		if len(allAssets) == 0 && target != "" && config.BruteScan.ForceScan {
			generatedAssets := w.generateBruteAssetsFromTargets(target, config.BruteScan.Services)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
				w.taskLog(task.TaskId, LevelInfo, "Brute scan: generated %d assets from target (force scan), services=%v", len(generatedAssets), config.BruteScan.Services)
				// 打印生成的资产详情
				for _, asset := range generatedAssets {
					w.taskLog(task.TaskId, LevelInfo, "Brute scan: generated asset: %s:%d (%s)", asset.Host, asset.Port, asset.Service)
				}
			}
		}

		// 仍然没有资产时跳过
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Brute scan: skipped (no assets)")
			completedPhases["brutescan"] = true
			w.incrSubTaskDone(ctx, task, "弱口令扫描", false, 1)
			incrSent++
		} else {
			// 检查控制信号
			if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task.TaskId, 65, "弱口令扫描中", "弱口令扫描")

			// 执行弱口令扫描
			bruteVulns := w.executeBruteScan(ctx, task, allAssets, config.BruteScan, orgId)
			if len(bruteVulns) > 0 {
				w.taskLog(task.TaskId, LevelInfo, "Brute scan completed: found %d weak passwords", len(bruteVulns))
			}
			completedPhases["brutescan"] = true
			w.incrSubTaskDone(ctx, task, "弱口令扫描", false, 1)
			incrSent++
		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
		return
	}

	// 执行目录扫描（在弱口令扫描之后、POC扫描之前）
	if config.DirScan != nil && config.DirScan.Enable && !completedPhases["dirscan"] {
		// 强制扫描模式：没有资产时从用户输入目标生成资产
		if len(allAssets) == 0 && target != "" && config.DirScan.ForceScan {
			generatedAssets := scanner.GenerateAssetsFromTargets(target)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
				w.taskLog(task.TaskId, LevelInfo, "Dir scan: generated %d assets from target (force scan)", len(generatedAssets))
			}
		}

		// 仍然没有资产时跳过
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "Dir scan: skipped (no assets)")
			completedPhases["dirscan"] = true
			w.incrSubTaskDone(ctx, task, "目录扫描", false, 1)
			incrSent++
		} else {
			// 检查控制信号
			if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task.TaskId, 70, "目录扫描中", "目录扫描")

			// 执行目录扫描
			dirScanAssets := w.executeDirScan(ctx, task, allAssets, config.DirScan, orgId)
			if len(dirScanAssets) > 0 {
				// 注意：目录扫描结果不添加到 allAssets，避免影响后续 POC 扫描
				// 目录扫描结果是 URL 路径，不是独立的扫描目标
				w.taskLog(task.TaskId, LevelInfo, "Dir scan completed: found %d paths", len(dirScanAssets))
				// 目录扫描结果已在 executeDirScan 中通过 saveDirScanResults 保存到数据库
			}
			completedPhases["dirscan"] = true
			w.incrSubTaskDone(ctx, task, "目录扫描", false, 1)
			incrSent++
		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
		return
	}

	// 执行 JSFinder 扫描（JS 敏感信息 + 未授权检测）
	if config.JSFinder != nil && config.JSFinder.Enable && !completedPhases["jsfinder"] {
		// 强制扫描模式：没有资产时从用户输入目标生成资产
		if len(allAssets) == 0 && target != "" && config.JSFinder.ForceScan {
			generatedAssets := scanner.GenerateAssetsFromTargets(target)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
				w.taskLog(task.TaskId, LevelInfo, "JSFinder: generated %d assets from target (force scan)", len(generatedAssets))
			}
		}

		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "JSFinder: skipped (no assets)")
			completedPhases["jsfinder"] = true
			w.incrSubTaskDone(ctx, task, "JS扫描", false, 1)
			incrSent++
		} else {
			if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
				return
			}

			w.updateTaskProgressWithPhase(ctx, task.TaskId, 80, "JS扫描中", "JS扫描")

			jsfinderResults := w.executeJSFinder(ctx, task, allAssets, config.JSFinder, orgId)
			// 结果已通过 OnResultFound 流式入库，此处仅记录日志
			if len(jsfinderResults) > 0 {
				w.taskLog(task.TaskId, LevelInfo, "JSFinder: %d findings (streamed)", len(jsfinderResults))
			}
			w.updateTaskProgressWithPhase(ctx, task.TaskId, 85, "JS扫描完成", "JS扫描")
			completedPhases["jsfinder"] = true
			w.incrSubTaskDone(ctx, task, "JS扫描", false, 1)
			incrSent++
		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, completedPhases, allAssets, "") {
		return
	}

	// 执行POC扫描 (使用Nuclei引擎)
	if config.PocScan != nil && config.PocScan.Enable && !completedPhases["pocscan"] {
		// 强制扫描模式：没有资产时从用户输入目标生成资产
		if len(allAssets) == 0 && target != "" && config.PocScan.ForceScan {
			generatedAssets := scanner.GenerateAssetsFromTargets(target)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
				w.taskLog(task.TaskId, LevelInfo, "POC scan: generated %d assets from target (force scan)", len(generatedAssets))
			}
		}

		// 没有资产时跳过实际扫描，但仍需递增进度
		if len(allAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "POC scan: skipped (no assets)")
			completedPhases["pocscan"] = true
			w.incrSubTaskDone(ctx, task, "漏洞扫描", false, 1)
			incrSent++
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

				// 检查是否有模板ID列表（任务创建时已筛选好的模板）
				if len(config.PocScan.NucleiTemplateIds) > 0 || len(config.PocScan.CustomPocIds) > 0 {
					// 通过RPC根据ID获取模板内容（包括默认模板和自定义POC)
					w.taskLog(task.TaskId, LevelInfo, "POC template request: nucleiTemplateIds=%d, customPocIds=%d", len(config.PocScan.NucleiTemplateIds), len(config.PocScan.CustomPocIds))
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
					// 优化：按资产分组，每组只加载相关的POC模板
					// 当AutoScan或AutomaticScan启用时，按资产的指纹标签进行分组
					var groups []*AssetGroup
					if config.PocScan.AutoScan || config.PocScan.AutomaticScan {
						groups = w.groupAssetsByTags(allAssets, config.PocScan)

						// 输出分组信息日志
						for _, group := range groups {
							w.taskLog(task.TaskId, LevelInfo, "Auto-scan group: tags=%v, assets=%d", group.Tags, len(group.Assets))
						}
					}

					if len(groups) > 0 {
						w.taskLog(task.TaskId, LevelInfo, "POC template auto selection: %d asset groups", len(groups))

						// 用于统计漏洞数量
						var vulCount int

						// 创建漏洞缓冲区，发现漏洞立即保存
						vulBuffer := NewVulnerabilityBuffer(1)

						// 获取单目标超时配置
						targetTimeout := config.PocScan.TargetTimeout
						if targetTimeout <= 0 {
							targetTimeout = 600 // 默认600秒
						}

						// 超时直接使用单目标超时，由自适应调度器管理并发和速率
						w.taskLog(task.TaskId, LevelInfo, "POC scan: target timeout=%ds, assets=%d, groups=%d (concurrency/rate managed by adaptive scheduler)",
							targetTimeout, len(allAssets), len(groups))
						pocCtx, pocCancel := context.WithTimeout(ctx, time.Duration(targetTimeout)*time.Second)

						// 启动后台刷新协程
						flushDone := make(chan struct{})
						w.safeGoTask(task.TaskId, "poc-group-vul-flush", func() {
							defer close(flushDone)
							ticker := time.NewTicker(5 * time.Second)
							defer ticker.Stop()
							for {
								select {
								case <-pocCtx.Done():
									return
								case <-flushDone:
									return
								case <-vulBuffer.flushChan:
									vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
										w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
									})
								case <-ticker.C:
									vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
										w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
									})
								}
							}
						})

						// 遍历每个分组进行扫描
						taskIdForCallback := task.TaskId
						severities := []string{}
						if config.PocScan.Severity != "" {
							severities = strings.Split(config.PocScan.Severity, ",")
						}

						for _, group := range groups {
							groupTemplates := w.getTemplatesByTags(ctx, group.Tags, severities)
							if len(groupTemplates) == 0 {
								w.taskLog(task.TaskId, LevelInfo, "Group (tags=%v): no templates loaded, skipping", group.Tags)
								continue
							}
							w.taskLog(task.TaskId, LevelInfo, "Group (tags=%v, assets=%d): loaded %d templates", group.Tags, len(group.Assets), len(groupTemplates))

							// 构建该分组的扫描选项（并发和速率由自适应调度器决定）
							groupOpts := &scanner.NucleiOptions{
								Severity:        config.PocScan.Severity,
								Tags:            group.Tags,
								ExcludeTags:     config.PocScan.ExcludeTags,
								TargetTimeout:   targetTimeout,
								AutoScan:        false,
								AutomaticScan:   false,
								CustomPocOnly:   false,
								CustomTemplates: groupTemplates,
								TagMappings:     nil,
								CustomHeaders:   config.PocScan.CustomHeaders,
								ForceScan:       config.PocScan.ForceScan,
								OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
									vulCount++
									w.taskLog(taskIdForCallback, LevelInfo, "Vulnerability found: %s → %s", vul.PocFile, vul.Url)
									vulBuffer.Add(vul)
								},
								OnProgress: w.makeOnProgress(task.MainTaskId, "POC扫描"),
							}

							// 创建任务日志回调
							pocTaskLogger := func(level, format string, args ...interface{}) {
								w.taskLog(task.TaskId, level, format, args...)
							}

							// 扫描该分组的资产
							result, err := s.Scan(pocCtx, &scanner.ScanConfig{
								Assets:     group.Assets,
								Options:    groupOpts,
								TaskLogger: pocTaskLogger,
							})

							if err != nil {
								w.taskLog(task.TaskId, LevelError, "POC scan error (group tags=%v): %v", group.Tags, err)
							}
							if result != nil {
								allVuls = append(allVuls, result.Vulnerabilities...)
							}
						}

						pocCancel()

						// 扫描完成后，刷新剩余的漏洞
						vulBuffer.Flush(ctx, func(vuls []*scanner.Vulnerability) {
							w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
						})

						if vulCount > 0 {
							w.taskLog(task.TaskId, LevelInfo, "POC scan completed: found %d vulnerabilities", vulCount)
						}
					} else {
						w.taskLog(task.TaskId, LevelWarn, "No POC templates configured (no tags matched), skipping POC scan")
					}
				}

				// 统一扫描执行：当模板通过 ID 或 CustomPocOnly 方式加载后，执行扫描
				if len(templates) > 0 {
					// 用于统计漏洞数量
					var vulCount int

					// 创建漏洞缓冲区，发现漏洞立即保存
					vulBuffer := NewVulnerabilityBuffer(1)

					// 获取单目标超时配置
					pocTargetTimeout := config.PocScan.TargetTimeout
					if pocTargetTimeout <= 0 {
						pocTargetTimeout = 600
					}

					// 超时直接使用单目标超时，由自适应调度器管理并发和速率
					w.taskLog(task.TaskId, LevelInfo, "POC scan: target timeout=%ds, assets=%d (concurrency/rate managed by adaptive scheduler)",
						pocTargetTimeout, len(allAssets))
					pocCtx, pocCancel := context.WithTimeout(ctx, time.Duration(pocTargetTimeout)*time.Second)

					// 启动后台刷新协程
					flushDone := make(chan struct{})
					w.safeGoTask(task.TaskId, "poc-vul-flush", func() {
						defer close(flushDone)
						ticker := time.NewTicker(5 * time.Second)
						defer ticker.Stop()
						for {
							select {
							case <-pocCtx.Done():
								return
							case <-flushDone:
								return
							case <-vulBuffer.flushChan:
								vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
									w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
								})
							case <-ticker.C:
								vulBuffer.Flush(pocCtx, func(vuls []*scanner.Vulnerability) {
									w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
								})
							}
						}
					})

					taskIdForCallback := task.TaskId
					// 并发和速率由自适应调度器决定
					nucleiOpts := &scanner.NucleiOptions{
						Severity:        config.PocScan.Severity,
						ExcludeTags:     config.PocScan.ExcludeTags,
						TargetTimeout:   pocTargetTimeout,
						AutoScan:        false,
						AutomaticScan:   false,
						CustomPocOnly:   config.PocScan.CustomPocOnly,
						CustomTemplates: templates,
						TagMappings:     config.PocScan.TagMappings,
						CustomHeaders:   config.PocScan.CustomHeaders,
						ForceScan:       config.PocScan.ForceScan,
						OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
							vulCount++
							w.taskLog(taskIdForCallback, LevelInfo, "Vulnerability found: %s → %s", vul.PocFile, vul.Url)
							vulBuffer.Add(vul)
						},
						OnProgress: w.makeOnProgress(task.MainTaskId, "POC扫描"),
					}

					pocTaskLogger := func(level, format string, args ...interface{}) {
						w.taskLog(task.TaskId, level, format, args...)
					}

					result, err := s.Scan(pocCtx, &scanner.ScanConfig{
						Assets:     allAssets,
						Options:    nucleiOpts,
						TaskLogger: pocTaskLogger,
					})

					if err != nil {
						w.taskLog(task.TaskId, LevelError, "POC scan error: %v", err)
					}
					if result != nil {
						allVuls = append(allVuls, result.Vulnerabilities...)
					}

					pocCancel()

					// 扫描完成后，刷新剩余的漏洞
					vulBuffer.Flush(ctx, func(vuls []*scanner.Vulnerability) {
						w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
					})

					if vulCount > 0 {
						w.taskLog(task.TaskId, LevelInfo, "POC scan completed: found %d vulnerabilities", vulCount)
					}
				}
			}
			// POC扫描模块完成，递增子任务进度
			w.incrSubTaskDone(ctx, task, "漏洞扫描", false, 1)
			incrSent++
		} // 结束 len(allAssets) > 0 的 else 分支
	}

	// 更新任务状态为完成
	duration := time.Since(startTime).Seconds()
	result := fmt.Sprintf("Assets:%d Vuls:%d Duration:%.0fs", len(allAssets), len(allVuls), duration)

	// 显式递增计数器，确保 progress=100 与 status=SUCCESS 原子落盘。
	// 正常路径下各模块已递增 enabledModules 次，此处补齐最终 +1 即可；
	// 若中途有路径未发出预期增量，clamp 兜底确保 sub_task_done 不超 sub_task_count。
	finalRemaining := expectedIncr - incrSent
	if finalRemaining < 1 {
		finalRemaining = 1
	}
	w.incrSubTaskDone(ctx, task, "完成", true, finalRemaining)
	finalIncrSent = true

	w.updateTaskStatus(ctx, task.TaskId, scheduler.TaskStatusSuccess, result)
	w.taskLog(task.TaskId, LevelInfo, "Completed: %s", result)
	// 注意：taskExecuted 由 defer 递增，无需在此处理
}

// updateTaskStatus 更新任务状态
func (w *Worker) updateTaskStatus(ctx context.Context, taskId string, status string, result string) {
	if w.schedClient != nil {
		phase := ""
		progress := 0
		if status == scheduler.TaskStatusSuccess || status == scheduler.TaskStatusFailure {
			phase = "完成"
			progress = 100
		}
		if err := w.schedClient.UpdateTask(ctx, taskId, status, progress, phase); err != nil {
			w.taskLog(taskId, LevelError, "update task status failed: %v", err)
		}
		return
	}

	// 回退到 HTTP
	if status == scheduler.TaskStatusSuccess || status == scheduler.TaskStatusFailure {
		_, err := w.httpClient.UpdateTask(ctx, &TaskUpdateReq{
			TaskId:   taskId,
			State:    status,
			Worker:   w.config.Name,
			Result:   result,
			Progress: 100,
			Phase:    "完成",
		})
		if err != nil {
			w.taskLog(taskId, LevelError, "update task status failed: %v", err)
		}
		return
	}

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
func (w *Worker) updateTaskProgressWithPhase(ctx context.Context, taskId string, progress int, message string, currentPhase string) {
	if currentPhase == "" {
		return
	}

	if w.schedClient != nil {
		if err := w.schedClient.UpdateTask(ctx, taskId, "", progress, currentPhase); err != nil {
			w.taskLog(taskId, LevelError, "update task progress failed: %v", err)
		}
		return
	}

	// 回退到 HTTP
	if w.httpClient != nil {
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

// expectedTaskIncr 根据任务配置计算单个 sub-task 应发出的总增量数：
// = 启用模块数 + 1（每模块 1 次 + 最终完成 1 次）。配置解析失败时回退为 1。
// 早退路径据此补齐，避免主任务计数永远到不了 sub_task_count。
func (w *Worker) expectedTaskIncr(configStr string) int {
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return 1
	}
	return utils.CountEnabledModules(cfg) + 1
}

// incrSubTaskDone 递增子任务完成数（模块级别）
// 每完成一个扫描模块就调用此方法，通知主任务进度更新
// isCompleted: true 表示子任务全部阶段完成，此时才递增计数器
// incrAmount: 递增数量（早退路径下需补齐 expectedTaskIncr - alreadySent，正常路径恒为 1）
func (w *Worker) incrSubTaskDone(ctx context.Context, task *scheduler.TaskInfo, phase string, isCompleted bool, incrAmount int) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Increment subtask done panic recovered: %v, stack: %s", r, string(getStackTrace()))
		}
	}()

	if incrAmount <= 0 {
		incrAmount = 1
	}

	if w.schedClient != nil {
		resp, err := w.schedClient.IncrSubTaskDone(ctx, task.MainTaskId, phase, incrAmount)
		if err != nil {
			w.taskLog(task.TaskId, LevelError, "Failed to incr sub task done: %v", err)
			return
		}
		// 刷新实时进度缓存
		w.progressMu.Lock()
		w.cachedSubTaskDone = resp.SubTaskDone
		w.cachedSubTaskCount = resp.SubTaskCount
		w.cachedMainTaskId = task.MainTaskId
		w.lastReportedProgress = 0
		w.progressMu.Unlock()
		if resp.AllDone {
			w.taskLog(task.TaskId, LevelInfo, "All sub-tasks completed: %d/%d", resp.SubTaskDone, resp.SubTaskCount)
		} else {
			w.taskLog(task.TaskId, LevelDebug, "Sub-task progress: %d/%d (phase: %s)", resp.SubTaskDone, resp.SubTaskCount, phase)
		}
		return
	}

	// 回退到 HTTP
	if w.httpClient == nil {
		return
	}

	resp, err := w.httpClient.IncrSubTaskDone(ctx, &SubTaskDoneReq{
		TaskId:      task.TaskId,
		MainTaskId:  task.MainTaskId,
		Phase:       phase,
		IsCompleted: isCompleted,
		IncrAmount:  incrAmount,
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

// updateMainTaskProgress 基于模块内部分进度实时更新主任务 progress 字段。
// 计算公式: progress = (cachedSubTaskDone + moduleFraction) / cachedSubTaskCount × 100
// moduleFraction: 当前模块的完成比例 (0.0 ~ 1.0)，由各 scanner 的 OnProgress 回调提供。
// 进度只升不降，防止多子任务交叉上报导致回退。
func (w *Worker) updateMainTaskProgress(mainTaskId string, moduleFraction float64, phase, message string) {
	if mainTaskId == "" {
		return
	}
	w.progressMu.Lock()
	done := w.cachedSubTaskDone
	total := w.cachedSubTaskCount
	cachedId := w.cachedMainTaskId
	lastProgress := w.lastReportedProgress
	w.progressMu.Unlock()

	// 缓存的主任务 ID 不匹配时跳过（子任务切换）
	if cachedId != mainTaskId || total <= 0 {
		return
	}

	progress := int((float64(done) + moduleFraction) * 100.0 / float64(total))
	if progress < 0 {
		progress = 0
	}
	if progress > 99 {
		progress = 99 // 100 由 IncrSubTaskDone 的 allDone 路径设置
	}

	// 只升不降
	if progress <= lastProgress {
		return
	}

	w.progressMu.Lock()
	w.lastReportedProgress = progress
	w.progressMu.Unlock()

	// 直接更新 MongoDB progress 字段
	if w.mongoDB != nil {
		taskModel := model.NewMainTaskModel(w.mongoDB)
		if err := taskModel.Update(context.Background(), mainTaskId, bson.M{
			"progress":      progress,
			"current_phase": phase,
		}); err != nil {
			w.logger.Error("[Progress] update main task progress failed: %v", err)
		}
	}

	// 同时更新 Redis 执行信息（前端轮询）
	if w.schedClient != nil {
		_ = w.schedClient.UpdateTask(context.Background(), mainTaskId, "", progress, phase)
	}
}

// makeOnProgress 创建模块级 OnProgress 回调。
// modulePercent 是 scanner 回调传入的当前模块完成百分比 (0-100)，
// 映射到主任务整体进度的增量: moduleFraction = modulePercent / 100。
func (w *Worker) makeOnProgress(mainTaskId, phase string) func(int, string) {
	return func(modulePercent int, message string) {
		if modulePercent < 0 {
			modulePercent = 0
		}
		if modulePercent > 100 {
			modulePercent = 100
		}
		fraction := float64(modulePercent) / 100.0
		msg := message
		if msg == "" {
			msg = phase
		}
		w.updateMainTaskProgress(mainTaskId, fraction, phase, msg)
	}
}

// 资产分批的 OOM 硬约束：
//
//   - maxBatchSize=50 为条数硬上限（来自 project_memory 强约束），不可调高。
//   - maxBatchBytes=1MB 为单批字节上限；累计 JSON 字节超过此值则切分新批。
//   - maxItemSize=20KB 为单条资产字节上限；超过则该条独占一批（避免一条压垮整批）。
//
// 在低内存 Worker 上，按条数切分（如 500/批）会同时驻留多批资产副本与
// HTTP 序列化缓冲，极易触发 OOM；按字节切分可限制单批驻留内存。
const (
	maxBatchSize  = 50
	maxBatchBytes = 1 << 20 // 1MB
	maxItemSize   = 20 * 1024
)

// assetBatchRange 描述一个批次在原切片中的 [start, end) 区间
type assetBatchRange struct {
	start, end int
}

// estimateAssetBytes 估算单个 Asset 序列化后的字节数（仅累加大字段，避免全量 marshal 开销）
func estimateAssetBytes(a *scanner.Asset) int {
	if a == nil {
		return 0
	}
	// 基础开销：覆盖 host/title/server/category 等短字段
	const baseOverhead = 1024
	return baseOverhead +
		len(a.HttpBody) + len(a.Screenshot) + len(a.IconData) +
		len(a.Banner) + len(a.HttpHeader) + len(a.Cert)
}

// calculateAssetBatchBoundaries 按字节+条数双重约束计算批次边界
// 规则：单条 > maxItemSize 时该条独占一批；否则累计到 maxBatchBytes 或 maxBatchSize 即切分
func calculateAssetBatchBoundaries(assets []*scanner.Asset) []assetBatchRange {
	if len(assets) == 0 {
		return nil
	}
	boundaries := make([]assetBatchRange, 0, (len(assets)/maxBatchSize)+1)
	start := 0
	totalBytes := 0
	for i := 0; i < len(assets); i++ {
		sz := estimateAssetBytes(assets[i])
		// 单条过大：独占一批，避免一条压垮整批
		if sz > maxItemSize {
			if i > start {
				boundaries = append(boundaries, assetBatchRange{start: start, end: i})
			}
			boundaries = append(boundaries, assetBatchRange{start: i, end: i + 1})
			start = i + 1
			totalBytes = 0
			continue
		}
		// 累计字节超限或条数达上限：切分
		if i > start && (totalBytes+sz > maxBatchBytes || (i-start) >= maxBatchSize) {
			boundaries = append(boundaries, assetBatchRange{start: start, end: i})
			start = i
			totalBytes = 0
		}
		totalBytes += sz
	}
	if start < len(assets) {
		boundaries = append(boundaries, assetBatchRange{start: start, end: len(assets)})
	}
	return boundaries
}
