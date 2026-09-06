package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cscan/internal/model"
	"cscan/internal/notification"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"
	"cscan/pkg/utils"

	"github.com/google/uuid"
	"github.com/projectdiscovery/wappalyzergo"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/mongo"
)

// WorkerConfig Worker配置
type WorkerConfig struct {
	Name        string `json:"name"`
	InstanceID  string `json:"instanceId"`
	IP          string `json:"ip"`
	ServerAddr  string `json:"serverAddr"` // API 服务地址 (e.g., http://server:8888)
	InstallKey  string `json:"installKey"` // 安装密钥
	Concurrency int    `json:"concurrency"`

	// Phase 2 客户端优先级队列管理器（默认关闭，保持向后兼容）
	// 开启后 taskChan 退化为预留槽位计数器，任务实际进入 TaskQueueManager
	// 由 GetTaskPriority 推断优先级，按 Urgent>High>Normal>Low 顺序出队
	EnableTaskQueueManager bool          `json:"enableTaskQueueManager"`
	MaxQueueSize           int           `json:"maxQueueSize"` // 0 表示默认 100
	MaxWaitTime            time.Duration `json:"maxWaitTime"`  // 0 表示默认 5 分钟
}

type mainTaskProgressState struct {
	mu           sync.Mutex
	done         int
	total        int
	lastReported int
}

type taskLeaseState struct {
	lost   atomic.Bool
	closed atomic.Bool
	mu     sync.Mutex
	cancel context.CancelFunc
}

// ownedTaskDispatch spans the complete local acquisition lifetime: successful
// backend pop, local queue wait, execution, and exact closure/replacement.
// Control state is embedded under the same lock so a late global callback
// cannot recreate a signal after this exact lease has been released.
type ownedTaskDispatch struct {
	mu                 sync.Mutex
	taskID             string
	mainTaskID         string
	dispatchGeneration string
	leaseToken         string
	closed             bool
	control            *scheduler.TaskControlEnvelope
}

// Worker 工作节点
type Worker struct {
	ctx          context.Context
	cancel       context.CancelFunc
	config       WorkerConfig
	workerName   string // immutable logical identity for this process lifetime
	instanceID   string // immutable UUID process generation
	taskProtocol int
	httpClient   *WorkerHTTPClient // HTTP 客户端（配置/模板/字典等仍走 HTTP）
	schedClient  *SchedulerClient  // Redis 直连调度客户端（任务拉取/心跳/进度上报）
	wsClient     *WorkerWSClient   // WebSocket 客户端（用于日志推送和控制信号）
	scanners     map[string]scanner.Scanner
	taskChan     chan *scheduler.TaskInfo
	stopChan     chan struct{}
	stopOnce     sync.Once
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	wg           sync.WaitGroup
	taskAsyncWG  sync.WaitGroup
	mu           sync.RWMutex

	// Exact execution-generation state lets every callback stop immediately
	// after a lease conflict without confusing a later lease for the same ID.
	taskLeaseStates sync.Map // taskId + leaseToken -> *taskLeaseState

	// Phase 2 客户端优先级队列管理器
	// 当 config.EnableTaskQueueManager=true 时启用
	// taskChan 此时退化为预留槽位计数器（len(taskChan) 用于检查并发槽位）
	// 任务实体存放在 taskQueue 的 4 级优先级 slice 中
	taskQueue *TaskQueueManager

	taskStarted   int
	taskExecuted  int
	isRunning     bool
	executorCount int // 已启动的任务处理协程数

	// Exact acquired dispatches, registered immediately after backend acquisition
	// and removed only after confirmed closure, exact requeue, or replacement.
	// Values are *ownedTaskDispatch keyed by taskId + dispatchGeneration.
	ownedTasks sync.Map

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

	// 本地模板库（内容寻址文件 + 内存索引）：POC 扫描优先按 id/标签解析库文件
	// 硬链接进扫描目录，避免每次扫描从 Mongo 拉内容重写临时文件
	templateStore *TemplateStore

	// 任务状态写独立连接池（独立持久化）：
	// current_phase/progress/sub_task_done/MarkTaskCompleted 等状态回写走独立小池，
	// 与结果数据写（mongoDB，池=20）隔离——数据写入过载时状态回写不会被饿死，
	// 避免平台侧任务"卡在 Fingerprint"。
	statusClient *mongo.Client
	statusDB     *mongo.Database

	// 扫描结果异步批量落库（后台写协程 + 有界缓冲）
	asyncWriter *AsyncResultWriter

	// 活跃任务的日志记录器（维持 buffer 生命周期）
	taskLoggers sync.Map // mainTaskId -> *TaskLoggerWS

	// 逐任务结构化目标事件采用固定上限与确定性采样，任务结束时清理。
	eventSampleMu     sync.Mutex
	eventSampleSeen   map[string]int
	eventSampleLogged map[string]int

	// Main-task aggregate progress is isolated per parent. Each state serializes
	// compare-and-write so concurrent child callbacks cannot regress Mongo.
	progressMu     sync.Mutex
	progressByMain map[string]*mainTaskProgressState
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

func (w *Worker) taskLogEvent(taskId, level, message, event, phase, outcome string, fields map[string]interface{}) {
}

func (w *Worker) scannerEventLogger(taskId string) scanner.ScanEventLogger {
	return nil
}

func (w *Worker) shouldPersistStructuredEvent(taskId, event string) bool {
	switch event {
	case scanner.EventNaabuParseComplete, scanner.EventNmapPortResult, scanner.EventSchemeProbeComplete, scanner.EventFingerprintDecision:
	default:
		return true
	}
	const (
		initialSample = 20
		sampleEvery   = 10
		maxLogged     = 100
	)
	key := getMainTaskId(taskId) + "\x00" + event
	w.eventSampleMu.Lock()
	defer w.eventSampleMu.Unlock()
	if w.eventSampleSeen == nil {
		w.eventSampleSeen = make(map[string]int)
		w.eventSampleLogged = make(map[string]int)
	}
	w.eventSampleSeen[key]++
	seen := w.eventSampleSeen[key]
	if w.eventSampleLogged[key] >= maxLogged || (seen > initialSample && seen%sampleEvery != 0) {
		return false
	}
	w.eventSampleLogged[key]++
	return true
}

func phaseEventFields(result PhaseResult) map[string]interface{} {
	return map[string]interface{}{
		"input": result.Coverage.Input, "attempted": result.Coverage.Attempted,
		"succeeded": result.Coverage.Succeeded, "timed_out": result.Coverage.TimedOut,
		"failed": result.Coverage.Failed, "skipped": result.Coverage.Skipped,
		"uncovered": result.Coverage.Uncovered, "unconfirmed": result.Coverage.Unconfirmed,
		"zero_update": result.Coverage.ZeroUpdate, "status": string(result.Status),
		"reason_codes": append([]string(nil), result.ReasonCodes...), "assets": result.Assets,
		"vulnerabilities": result.Vulnerabilities, "vulnerability_conclusion": result.VulnerabilityConclusion,
	}
}

func taskFinalizedEventFields(summary *model.TaskScanSummary) map[string]interface{} {
	if summary == nil {
		return nil
	}
	incomplete := make([]string, 0)
	for _, phase := range summary.Phases {
		if phase.Status != model.TaskPhaseComplete && phase.Status != model.TaskPhaseSkippedNotApplicable {
			incomplete = append(incomplete, phase.Phase)
		}
	}
	sort.Strings(incomplete)
	return map[string]interface{}{
		"outcome": summary.Outcome, "assets": summary.Assets, "vulnerabilities": summary.Vulnerabilities,
		"incomplete_phases": incomplete, "warning_codes": append([]string(nil), summary.WarningCodes...),
		"vulnerability_conclusion": summary.VulnerabilityConclusion, "complete": summary.Complete,
	}
}

// cleanupTaskLogger 清理任务日志记录器
func (w *Worker) cleanupTaskLogger(taskId string) {
	mainTaskId := getMainTaskId(taskId)
	// 日志直写 MongoDB，无需 flush 缓冲区
	w.taskLoggers.Delete(mainTaskId)
	w.eventSampleMu.Lock()
	prefix := mainTaskId + "\x00"
	for key := range w.eventSampleSeen {
		if strings.HasPrefix(key, prefix) {
			delete(w.eventSampleSeen, key)
			delete(w.eventSampleLogged, key)
		}
	}
	w.eventSampleMu.Unlock()
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
	if strings.TrimSpace(config.Name) == "" {
		config.Name = GetWorkerName()
	}
	// A lease owner identifies one process incarnation, not a deployment slot.
	// Always generate it here so configuration cannot reuse a prior process ID.
	config.InstanceID = uuid.NewString()

	// 自动获取本机IP地址
	if config.IP == "" {
		config.IP = GetLocalIP()
	}

	// HTTP and direct clients share one immutable process identity.
	httpClient := NewWorkerHTTPClientForInstance(
		config.ServerAddr,
		config.InstallKey,
		config.Name,
		config.InstanceID,
		scheduler.TaskProtocolV1,
	)

	logx.Infof("[Worker] HTTP client created, API server: %s", config.ServerAddr)

	// 创建可取消的Context
	ctx, cancel := context.WithCancel(context.Background())

	w := &Worker{
		ctx:               ctx,
		cancel:            cancel,
		config:            config,
		workerName:        config.Name,
		instanceID:        config.InstanceID,
		taskProtocol:      scheduler.TaskProtocolV1,
		httpClient:        httpClient,
		scanners:          make(map[string]scanner.Scanner),
		taskChan:          make(chan *scheduler.TaskInfo, config.Concurrency),
		stopChan:          make(chan struct{}),
		shutdownDone:      make(chan struct{}),
		logger:            NewWorkerLoggerLocal(config.Name), // 使用本地日志
		eventSampleSeen:   make(map[string]int),
		eventSampleLogged: make(map[string]int),
		progressByMain:    make(map[string]*mainTaskProgressState),
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

	// Store only validated generation-bearing task controls.
	w.wsClient.SetControlHandler(func(envelope *scheduler.TaskControlEnvelope) {
		w.handleControlSignal(envelope)
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

	// 异步批量落库：扫描主链路只投递，后台协程按任务分组定量/定时批量直写。
	// flush 回调为"同步直写 + 失败入 ResultQueue"（SyncOrQueue），不投递回异步通道避免自循环；
	// 通道满/写协程停止时 saveXxxWithFallback 自动回退同步路径，保证零丢失。
	w.asyncWriter = NewAsyncResultWriter(defaultAsyncWriterConfig(), AsyncWriteCallbacks{
		SaveAssets:  w.saveAssetResultSyncOrQueue,
		SaveCerts:   w.saveCertResultsSyncOrQueue,
		SaveVuls:    w.saveVulResultSyncOrQueue,
		SaveJS:      w.saveJSFinderResultSyncOrQueue,
		SaveDirScan: w.saveDirScanResultsSyncOrQueue,
	})
	w.asyncWriter.SetLogger(func(level, format string, args ...interface{}) {
		switch level {
		case LevelError:
			w.logger.Error("[AsyncWriter] "+format, args...)
		case LevelWarn:
			w.logger.Warn("[AsyncWriter] "+format, args...)
		default:
			w.logger.Info("[AsyncWriter] "+format, args...)
		}
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
	_, _, _, _ = w.loadHttpServiceMappings()
	// 初始化本地模板库并后台首同步（不阻塞启动；同步完成前的扫描回退 Mongo 内容加载）
	storeDir := os.Getenv("CSCAN_TEMPLATE_STORE_DIR")
	if storeDir == "" {
		storeDir = filepath.Join("data", "template-store")
	}
	w.templateStore = NewTemplateStore(storeDir, w.logger)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := w.templateStore.EnsureSynced(ctx, db); err != nil {
			w.logger.Error("TemplateStore initial sync failed: %v (POC scan falls back to Mongo content loading)", err)
		}
	}()
}

// SetStatusMongoDB 设置任务状态写专用 MongoDB 实例（独立连接池，独立持久化）。
// db 为 nil 时回退使用数据写库（与历史行为一致，不阻断启动）。
// 必须在 SetRedis 之前调用（SchedulerClient 构造时取状态库）。
func (w *Worker) SetStatusMongoDB(client *mongo.Client, db *mongo.Database) {
	if db == nil {
		w.statusDB = w.mongoDB
		return
	}
	w.statusClient = client
	w.statusDB = db
}

// statusDBHandle 返回任务状态写库（独立池）；未配置时回退数据写库
func (w *Worker) statusDBHandle() *mongo.Database {
	if w.statusDB != nil {
		return w.statusDB
	}
	return w.mongoDB
}

// SetRedis 设置 Redis 客户端并创建 SchedulerClient，用于直连 Redis 调度
func (w *Worker) SetRedis(rdb *redis.Client) error {
	// SchedulerClient 的 mongoDB 仅用于 MainTask 状态写
	// （IncrSubTaskDone / current_phase / progress / MarkTaskCompleted），
	// 传入独立状态池实现"独立持久化"，与结果数据写（w.mongoDB）隔离。
	client, err := NewSchedulerClientForInstance(
		rdb,
		w.workerName,
		w.instanceID,
		w.taskProtocol,
		w.statusDBHandle(),
	)
	if err != nil {
		return err
	}
	w.schedClient = client
	logx.Infof("[Worker] SchedulerClient initialized for direct Redis scheduling")
	return nil
}

// SetNotifyService 设置通知服务，任务完成/失败时触发通知
func (w *Worker) SetNotifyService(svc *notification.Service) {
	if w.schedClient != nil {
		w.schedClient.SetNotifyService(svc)
	}
}

func taskControlStorageKey(taskID, dispatchGeneration string) string {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(dispatchGeneration) == "" {
		return ""
	}
	return taskID + "\x00" + dispatchGeneration
}

func taskControlStorageKeyForTask(task *scheduler.TaskInfo) string {
	if task == nil {
		return ""
	}
	return taskControlStorageKey(task.TaskId, task.DispatchGeneration)
}

func sameTaskControlEnvelope(left, right scheduler.TaskControlEnvelope) bool {
	return left.IntentID == right.IntentID && left.MainTaskID == right.MainTaskID &&
		left.TaskID == right.TaskID && left.Action == right.Action &&
		left.DispatchGeneration == right.DispatchGeneration &&
		left.Timestamp.UTC().UnixMilli() == right.Timestamp.UTC().UnixMilli()
}

func (owned *ownedTaskDispatch) matchesTask(task *scheduler.TaskInfo) bool {
	return owned != nil && task != nil && owned.taskID == task.TaskId &&
		owned.mainTaskID == task.MainTaskId && owned.dispatchGeneration == task.DispatchGeneration &&
		owned.leaseToken == task.LeaseToken
}

// registerOwnedTask establishes local control ownership before a successfully
// acquired child can wait in either local queue. Pointer CAS prevents a stale
// lease's deferred cleanup from deleting a later acquisition of the same target.
func (w *Worker) registerOwnedTask(task *scheduler.TaskInfo) bool {
	key := taskControlStorageKeyForTask(task)
	if key == "" || task == nil || strings.TrimSpace(task.MainTaskId) == "" || strings.TrimSpace(task.LeaseToken) == "" {
		return false
	}
	incoming := &ownedTaskDispatch{
		taskID: task.TaskId, mainTaskID: task.MainTaskId,
		dispatchGeneration: task.DispatchGeneration, leaseToken: task.LeaseToken,
	}
	for {
		existingValue, loaded := w.ownedTasks.LoadOrStore(key, incoming)
		if !loaded {
			return true
		}
		existing, ok := existingValue.(*ownedTaskDispatch)
		if !ok || existing == nil {
			return false
		}
		existing.mu.Lock()
		if !existing.closed && existing.matchesTask(task) {
			existing.mu.Unlock()
			return true
		}
		existing.closed = true
		existing.control = nil
		staleTask := &scheduler.TaskInfo{
			TaskId: existing.taskID, MainTaskId: existing.mainTaskID,
			DispatchGeneration: existing.dispatchGeneration, LeaseToken: existing.leaseToken,
		}
		existing.mu.Unlock()
		if w.ownedTasks.CompareAndSwap(key, existingValue, incoming) {
			if staleTask.LeaseToken != incoming.leaseToken {
				w.markTaskLeaseLost(staleTask)
			}
			w.logger.Warn("Replaced stale local ownership: taskId=%s generation=%s", task.TaskId, task.DispatchGeneration)
			return true
		}
	}
}

// releaseOwnedTask removes only the exact acquisition identified by its lease
// token. Confirmed closure/requeue and true ownership replacement are the only
// callers permitted to use this transition.
func (w *Worker) releaseOwnedTask(task *scheduler.TaskInfo) bool {
	key := taskControlStorageKeyForTask(task)
	if key == "" {
		return false
	}
	value, ok := w.ownedTasks.Load(key)
	if !ok {
		return false
	}
	owned, ok := value.(*ownedTaskDispatch)
	if !ok || owned == nil {
		return false
	}
	owned.mu.Lock()
	if owned.closed || !owned.matchesTask(task) {
		owned.mu.Unlock()
		return false
	}
	owned.closed = true
	owned.control = nil
	owned.mu.Unlock()
	return w.ownedTasks.CompareAndDelete(key, value)
}

func (w *Worker) ownsExactTask(task *scheduler.TaskInfo) bool {
	key := taskControlStorageKeyForTask(task)
	if key == "" {
		return false
	}
	value, ok := w.ownedTasks.Load(key)
	if !ok {
		return false
	}
	owned, ok := value.(*ownedTaskDispatch)
	if !ok || owned == nil {
		return false
	}
	owned.mu.Lock()
	defer owned.mu.Unlock()
	currentValue, current := w.ownedTasks.Load(key)
	return current && currentValue == value && !owned.closed && owned.matchesTask(task)
}

func (w *Worker) getOwnedTaskTargets() []scheduler.TaskControlTarget {
	targets := make([]scheduler.TaskControlTarget, 0)
	w.ownedTasks.Range(func(_, value interface{}) bool {
		owned, ok := value.(*ownedTaskDispatch)
		if !ok || owned == nil {
			return true
		}
		owned.mu.Lock()
		if !owned.closed {
			targets = append(targets, scheduler.TaskControlTarget{
				TaskID: owned.taskID, DispatchGeneration: owned.dispatchGeneration,
			})
		}
		owned.mu.Unlock()
		return true
	})
	return targets
}

// handleControlSignal accepts global Redis/WebSocket fan-out only for an exact
// acquisition currently owned by this process. STOP-over-PAUSE ordering is
// serialized under the same record lock as exact ownership release.
func (w *Worker) handleControlSignal(envelope *scheduler.TaskControlEnvelope) {
	if envelope == nil || envelope.Validate() != nil {
		w.logger.Warn("Rejected malformed or generation-blind task control")
		return
	}
	key := taskControlStorageKey(envelope.TaskID, envelope.DispatchGeneration)
	value, ok := w.ownedTasks.Load(key)
	if !ok {
		return
	}
	owned, ok := value.(*ownedTaskDispatch)
	if !ok || owned == nil {
		return
	}
	incoming := *envelope
	owned.mu.Lock()
	defer owned.mu.Unlock()
	currentValue, current := w.ownedTasks.Load(key)
	if !current || currentValue != value || owned.closed || owned.taskID != incoming.TaskID ||
		owned.mainTaskID != incoming.MainTaskID || owned.dispatchGeneration != incoming.DispatchGeneration {
		return
	}
	if owned.control == nil {
		owned.control = &incoming
		w.logger.Info("Stored owned task control: taskId=%s generation=%s intentId=%s action=%s",
			incoming.TaskID, incoming.DispatchGeneration, incoming.IntentID, incoming.Action)
		return
	}
	existing := *owned.control
	if sameTaskControlEnvelope(existing, incoming) || existing.Action == scheduler.TaskControlActionStop ||
		existing.Action == incoming.Action || incoming.Action != scheduler.TaskControlActionStop {
		return
	}
	owned.control = &incoming
	w.logger.Info("Replaced owned PAUSE with STOP: taskId=%s generation=%s intentId=%s",
		incoming.TaskID, incoming.DispatchGeneration, incoming.IntentID)
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
		w.logger.Warn("Worker rename to %q rejected: worker identity is immutable; restart with the new name", param)
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
func (w *Worker) Start() error {
	w.mu.Lock()
	w.isRunning = true
	w.mu.Unlock()

	// Recovery and acquisition are unsafe until this exact process generation
	// is visible. Fail startup rather than running without its instance lease.
	if err := w.sendHeartbeatWithRetry(); err != nil {
		w.mu.Lock()
		w.isRunning = false
		w.mu.Unlock()
		return fmt.Errorf("establish worker instance heartbeat: %w", err)
	}

	// 启动本地结果队列
	if w.resultQueue != nil {
		if err := w.resultQueue.Start(w.ctx); err != nil {
			w.logger.Warn("Result queue failed to start: %v", err)
		}
	}

	// 启动扫描结果异步批量落库协程
	if w.asyncWriter != nil {
		w.asyncWriter.Start()
		w.logger.Info("Async result writer started: chanSize=%d, flushInterval=%v, maxAssetsBatch=%d",
			1024, 3*time.Second, 50)
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

	w.logger.Info("Worker %s instance %s started with %d workers", w.workerName, w.instanceID, w.config.Concurrency)
	return nil
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

		taskCtx := context.Background()
		if ctrl := w.checkTaskControl(taskCtx, task); ctrl == scheduler.TaskControlActionStop {
			w.taskLog(task.TaskId, LevelInfo, "Task %s generation %s stopped while waiting in the local queue",
				task.TaskId, task.DispatchGeneration)
			// Keep retrying while this live worker owns the lease. Exact success or
			// definitive replacement releases the acquisition registry entry.
			_ = w.acknowledgeStoppedTask(task)
			w.cleanupTaskLogger(task.TaskId)
			continue
		}
		w.executeTask(task)
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

func taskLeaseStateKey(task *scheduler.TaskInfo) string {
	if task == nil {
		return ""
	}
	return task.TaskId + "\x00" + task.DispatchGeneration + "\x00" + task.LeaseToken
}

func (state *taskLeaseState) setCancel(cancel context.CancelFunc) {
	if state == nil || cancel == nil {
		return
	}
	state.mu.Lock()
	state.cancel = cancel
	inactive := state.lost.Load() || state.closed.Load()
	state.mu.Unlock()
	if inactive {
		cancel()
	}
}

func (state *taskLeaseState) cancelTask() {
	if state == nil {
		return
	}
	state.mu.Lock()
	cancel := state.cancel
	state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (w *Worker) registerTaskLeaseState(task *scheduler.TaskInfo, cancel context.CancelFunc) *taskLeaseState {
	key := taskLeaseStateKey(task)
	if key == "" {
		return nil
	}
	candidate := &taskLeaseState{}
	value, _ := w.taskLeaseStates.LoadOrStore(key, candidate)
	state, _ := value.(*taskLeaseState)
	if state == nil {
		return nil
	}
	state.setCancel(cancel)
	return state
}

func (w *Worker) unregisterTaskLeaseState(task *scheduler.TaskInfo) {
	if key := taskLeaseStateKey(task); key != "" {
		w.taskLeaseStates.Delete(key)
	}
}

func (w *Worker) taskLeaseState(task *scheduler.TaskInfo) *taskLeaseState {
	key := taskLeaseStateKey(task)
	if key == "" {
		return nil
	}
	value, ok := w.taskLeaseStates.Load(key)
	if !ok {
		return nil
	}
	state, _ := value.(*taskLeaseState)
	return state
}

func (w *Worker) taskLeaseLost(task *scheduler.TaskInfo) bool {
	state := w.taskLeaseState(task)
	return state != nil && state.lost.Load()
}

func (w *Worker) taskLeaseClosed(task *scheduler.TaskInfo) bool {
	state := w.taskLeaseState(task)
	return state != nil && state.closed.Load()
}

func (w *Worker) markTaskLeaseClosed(task *scheduler.TaskInfo) {
	state := w.taskLeaseState(task)
	if state != nil {
		state.closed.Store(true)
		state.cancelTask()
	}
	w.releaseOwnedTask(task)
}

func (w *Worker) markTaskLeaseLost(task *scheduler.TaskInfo) {
	state := w.taskLeaseState(task)
	if state != nil {
		if !state.lost.Swap(true) {
			w.taskLog(task.TaskId, LevelError, "Task lease ownership was lost; canceling stale execution")
		}
		state.cancelTask()
	}
	w.releaseOwnedTask(task)
}

func (w *Worker) pollExactTaskControl(task *scheduler.TaskInfo) {
	if task == nil {
		return
	}
	target := scheduler.TaskControlTarget{TaskID: task.TaskId, DispatchGeneration: task.DispatchGeneration}
	if target.Validate() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if w.schedClient != nil {
		signals, err := w.schedClient.GetTaskControlSignals(ctx, []scheduler.TaskControlTarget{target})
		if err != nil {
			return
		}
		for index := range signals {
			w.handleControlSignal(&signals[index])
		}
		return
	}
	if w.httpClient == nil {
		return
	}
	resp, err := w.httpClient.GetTaskControlSignals(ctx, []scheduler.TaskControlTarget{target})
	if err != nil {
		return
	}
	for index := range resp.Signals {
		w.handleControlSignal(&resp.Signals[index])
	}
}

// awaitParentControl keeps exact ownership registered until the strict durable
// envelope that explains a same-generation Mongo fence is available locally.
// The sentinel itself is never interpreted as PAUSE or STOP.
func (w *Worker) awaitParentControl(task *scheduler.TaskInfo) string {
	for {
		if !w.ownsExactTask(task) {
			w.markTaskLeaseLost(task)
			return ""
		}
		if control := w.checkTaskControl(context.Background(), task); control != "" {
			if state := w.taskLeaseState(task); state != nil {
				state.cancelTask()
			}
			return control
		}
		w.pollExactTaskControl(task)
		if control := w.checkTaskControl(context.Background(), task); control != "" {
			if state := w.taskLeaseState(task); state != nil {
				state.cancelTask()
			}
			return control
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-w.stopChan:
			timer.Stop()
			return ""
		case <-timer.C:
		}
	}
}

func (w *Worker) handleTaskLeaseError(task *scheduler.TaskInfo, err error) error {
	if errors.Is(err, scheduler.ErrTaskLeaseConflict) {
		w.markTaskLeaseLost(task)
	} else if errors.Is(err, scheduler.ErrTaskParentFenced) {
		w.awaitParentControl(task)
	}
	return err
}

// safeGoTask 启动带 panic 恢复的 goroutine，panic 时将堆栈附加到任务日志而非仅 Worker 日志。
// 用于与具体任务绑定的后台协程（流式缓冲刷新等），便于按任务排查。
func (w *Worker) safeGoTask(taskId, label string, fn func()) {
	w.taskAsyncWG.Add(1)
	go func() {
		defer w.taskAsyncWG.Done()
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
	ctx := w.ctx

	// 修复 #27：读取 concurrency 时加 RLock，避免与 applyConcurrency 写入竞争
	// taskStarted/taskExecuted 同样在 mu 下写（executeTask），一并持锁读取
	w.mu.RLock()
	concurrency := w.config.Concurrency
	// 在途任务数 = 正在执行 + 排队等待。此前只统计排队数，
	// 执行协程取走任务后槽位立即释放，实际在途可达 2×Concurrency
	pendingCount := w.taskStarted - w.taskExecuted
	w.mu.RUnlock()
	if w.taskQueue != nil {
		pendingCount += w.taskQueue.Size()
	} else {
		pendingCount += len(w.taskChan)
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
				TaskId:             resp.TaskId,
				MainTaskId:         resp.MainTaskId,
				TaskName:           "scan",
				Config:             resp.Config,
				LeaseToken:         resp.LeaseToken,
				DispatchGeneration: resp.DispatchGeneration,
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
				TaskId:             resp.TaskId,
				MainTaskId:         resp.MainTaskId,
				TaskName:           "scan",
				Config:             resp.Config,
				LeaseToken:         resp.LeaseToken,
				DispatchGeneration: resp.DispatchGeneration,
			}
		}
	}

	if task == nil {
		return false
	}
	if !w.registerOwnedTask(task) {
		w.logger.Error("pullTask: rejected incomplete or conflicting acquired ownership for task %s", task.TaskId)
		if w.schedClient != nil {
			if err := w.schedClient.RequeueTask(ctx, task); err != nil {
				w.logger.Error("pullTask: exact requeue after local ownership rejection failed for %s: %v", task.TaskId, err)
			}
		}
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
				} else {
					w.releaseOwnedTask(task)
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

// Stop stops acquisition first, drains active tasks while retaining the
// instance heartbeat, and publishes offline only after task goroutines and
// buffered result writers are quiescent.
func (w *Worker) Stop() {
	w.shutdownOnce.Do(func() {
		w.stopAndDrain()
		close(w.shutdownDone)
	})
	<-w.shutdownDone
}

func (w *Worker) stopAndDrain() {
	w.mu.Lock()
	w.isRunning = false
	w.mu.Unlock()

	// Stop acquisition, control polling, and idle executors first. Task
	// contexts intentionally continue until their current work completes.
	w.cancel()
	w.stopOnce.Do(func() { close(w.stopChan) })
	if w.taskQueue != nil {
		w.taskQueue.Stop()
	}
	if w.wsClient != nil {
		w.wsClient.Close()
	}

	// keepAliveLoop exits with stopChan, so maintain liveness separately while
	// the wait group drains. Recovery must never classify this instance offline
	// while it can still execute leased work.
	drainHeartbeatStop := make(chan struct{})
	drainHeartbeatDone := make(chan struct{})
	go func() {
		defer close(drainHeartbeatDone)
		w.maintainDrainHeartbeat(drainHeartbeatStop)
	}()

	w.wg.Wait()
	// Task-owned flushers/watchers are launched from counted executors, so no
	// new Add can occur after the executor group has drained.
	w.taskAsyncWG.Wait()

	if w.asyncWriter != nil {
		w.asyncWriter.Stop()
	}
	if w.resultQueue != nil {
		w.resultQueue.Stop()
	}

	// Keep the instance heartbeat alive through all accepted task/result
	// persistence. Only then may recovery observe this process as offline.
	close(drainHeartbeatStop)
	<-drainHeartbeatDone

	// Offline is last among ownership-sensitive operations. Both transports
	// compare-and-delete this exact instance and leave newer same-name workers.
	w.notifyOffline()

	if globalMongoLogger != nil {
		globalMongoLogger.Close()
	}

	if w.mongoClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := w.mongoClient.Disconnect(ctx); err != nil {
			logx.Errorf("[Worker][MongoDirect] disconnect failed: %v", err)
		}
		cancel()
	}

	if w.statusClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := w.statusClient.Disconnect(ctx); err != nil {
			logx.Errorf("[Worker][MongoStatus] disconnect failed: %v", err)
		}
		cancel()
	}

	scanner.CleanupChromedp()
	w.logger.Info("Worker %s instance %s stopped", w.workerName, w.instanceID)
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
func (w *Worker) handleTaskControl(ctx context.Context, task *scheduler.TaskInfo, config *scheduler.TaskConfig, completedPhases map[string]bool, assets []*scanner.Asset, timedOutHostPorts map[string]struct{}, phaseName string) bool {
	if ctrl := w.checkTaskControl(ctx, task); ctrl == "STOP" {
		reason := "Task stopped by user"
		if phaseName != "" {
			reason = fmt.Sprintf("Task stopped by user during %s", phaseName)
		}
		w.logTaskAbort(task, config, completedPhases, reason)
		return true
	} else if ctrl == "PAUSE" {
		if phaseName != "" {
			w.taskLog(task.TaskId, LevelInfo, "Task paused during %s, saving progress...", phaseName)
		} else {
			w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
		}
		w.saveTaskProgress(ctx, task, completedPhases, assets, timedOutHostPorts)
		return true
	}
	return false
}

func enabledTaskPhaseKeys(config *scheduler.TaskConfig) []string {
	if config == nil {
		return nil
	}
	checks := []struct {
		key     string
		enabled bool
	}{
		{"domainscan", config.DomainScan != nil && config.DomainScan.Enable},
		{"portscan", config.PortScan != nil && config.PortScan.Enable},
		{"portidentify", config.PortIdentify != nil && config.PortIdentify.Enable},
		{"fingerprint", config.Fingerprint != nil && config.Fingerprint.Enable},
		{"brutescan", config.BruteScan != nil && config.BruteScan.Enable},
		{"dirscan", config.DirScan != nil && config.DirScan.Enable},
		{"jsfinder", config.JSFinder != nil && config.JSFinder.Enable},
		{"poc", config.PocScan != nil && config.PocScan.Enable},
	}
	keys := make([]string, 0, len(checks))
	for _, check := range checks {
		if check.enabled {
			keys = append(keys, check.key)
		}
	}
	return keys
}

// skippedEnabledPhases 按扫描链顺序返回「已启用但尚未完成」的阶段中文名，
// 供任务中途退出时明确提示哪些阶段被跳过。
func skippedEnabledPhases(config *scheduler.TaskConfig, completedPhases map[string]bool) []string {
	if config == nil {
		return nil
	}
	checks := []struct {
		key     string
		name    string
		enabled bool
	}{
		{"domainscan", "域名扫描", config.DomainScan != nil && config.DomainScan.Enable},
		{"portscan", "端口扫描", config.PortScan != nil && config.PortScan.Enable},
		{"portidentify", "端口识别", config.PortIdentify != nil && config.PortIdentify.Enable},
		{"fingerprint", "指纹识别", config.Fingerprint != nil && config.Fingerprint.Enable},
		{"brutescan", "弱口令扫描", config.BruteScan != nil && config.BruteScan.Enable},
		{"dirscan", "目录扫描", config.DirScan != nil && config.DirScan.Enable},
		{"jsfinder", "JS扫描", config.JSFinder != nil && config.JSFinder.Enable},
		{"pocscan", "漏洞扫描", config.PocScan != nil && config.PocScan.Enable},
	}
	skipped := make([]string, 0, len(checks))
	for _, c := range checks {
		if c.enabled && !completedPhases[c.key] {
			skipped = append(skipped, c.name)
		}
	}
	return skipped
}

// logTaskAbort 以 WARN 级别记录任务中途退出，并列出已启用但未执行的阶段，
// 避免剩余阶段被静默跳过而无任何可感知的日志。
func (w *Worker) logTaskAbort(task *scheduler.TaskInfo, config *scheduler.TaskConfig, completedPhases map[string]bool, reason string) {
	if skipped := skippedEnabledPhases(config, completedPhases); len(skipped) > 0 {
		w.taskLog(task.TaskId, LevelWarn, "%s; 已启用但未执行的阶段: %s", reason, strings.Join(skipped, " → "))
	} else {
		w.taskLog(task.TaskId, LevelWarn, "%s", reason)
	}
}

func (w *Worker) checkTaskControl(_ context.Context, task *scheduler.TaskInfo) string {
	key := taskControlStorageKeyForTask(task)
	if key == "" || task == nil {
		return ""
	}
	value, ok := w.ownedTasks.Load(key)
	if !ok {
		return ""
	}
	owned, ok := value.(*ownedTaskDispatch)
	if !ok || owned == nil {
		return ""
	}
	owned.mu.Lock()
	defer owned.mu.Unlock()
	currentValue, current := w.ownedTasks.Load(key)
	if !current || currentValue != value || owned.closed || !owned.matchesTask(task) || owned.control == nil {
		return ""
	}
	envelope := *owned.control
	if envelope.Validate() != nil || envelope.TaskID != task.TaskId ||
		envelope.MainTaskID != task.MainTaskId || envelope.DispatchGeneration != task.DispatchGeneration {
		return ""
	}
	return envelope.Action
}

// saveTaskProgress 保存暂停恢复快照。保存使用独立短超时，避免任务控制已取消时丢失快照。
func (w *Worker) saveTaskProgress(_ context.Context, task *scheduler.TaskInfo, completedPhases map[string]bool, assets []*scanner.Asset, timedOutHostPorts map[string]struct{}) {
	phases := make([]string, 0, len(completedPhases))
	for phase, completed := range completedPhases {
		if completed {
			phases = append(phases, phase)
		}
	}
	sort.Strings(phases)

	assetsJSON, err := json.Marshal(assets)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "marshal paused task assets failed; retaining execution ownership until shutdown: %v", err)
		<-w.stopChan
		return
	}
	timedOutKeys := make([]string, 0, len(timedOutHostPorts))
	for key := range timedOutHostPorts {
		timedOutKeys = append(timedOutKeys, key)
	}
	sort.Strings(timedOutKeys)
	state := map[string]interface{}{
		"completedPhases":   phases,
		"assets":            string(assetsJSON),
		"timedOutHostPorts": timedOutKeys,
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "marshal paused task state failed; retaining execution ownership until shutdown: %v", err)
		<-w.stopChan
		return
	}

	backoff := time.Second
	for {
		select {
		case <-w.stopChan:
			w.taskLog(task.TaskId, LevelWarn, "worker is stopping before paused task state was acknowledged")
			return
		default:
		}
		if w.checkTaskControl(context.Background(), task) == scheduler.TaskControlActionStop {
			w.taskLog(task.TaskId, LevelInfo, "STOP superseded PAUSE while saving generation %s", task.DispatchGeneration)
			_ = w.acknowledgeStoppedTask(task)
			return
		}

		saveCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var saveErr error
		if w.schedClient != nil {
			saveErr = w.schedClient.PauseTask(saveCtx, task.TaskId, task.MainTaskId, task.LeaseToken, "已暂停", string(stateJSON))
		} else if w.httpClient != nil {
			_, saveErr = w.httpClient.UpdateTask(saveCtx, &TaskUpdateReq{
				TaskId:     task.TaskId,
				MainTaskId: task.MainTaskId,
				LeaseToken: task.LeaseToken,
				State:      model.TaskStatusPaused,
				Worker:     w.config.Name,
				Phase:      "已暂停",
				TaskState:  string(stateJSON),
			})
		} else {
			saveErr = fmt.Errorf("no task update client configured")
		}
		cancel()
		if saveErr == nil {
			w.markTaskLeaseClosed(task)
			w.taskLog(task.TaskId, LevelInfo, "Task %s progress saved: completedPhases=%v, assets=%d", task.TaskId, phases, len(assets))
			return
		}
		if errors.Is(saveErr, scheduler.ErrTaskLeaseConflict) {
			w.markTaskLeaseLost(task)
			w.taskLog(task.TaskId, LevelWarn, "paused task ownership changed before snapshot acknowledgement; stopping retries")
			return
		}
		if errors.Is(saveErr, scheduler.ErrTaskParentFenced) {
			control := w.awaitParentControl(task)
			if control == scheduler.TaskControlActionStop {
				w.taskLog(task.TaskId, LevelInfo, "Durable STOP fenced PAUSE for generation %s; acknowledging STOP", task.DispatchGeneration)
				_ = w.acknowledgeStoppedTask(task)
				return
			}
			if control == "" {
				return
			}
		}

		w.taskLog(task.TaskId, LevelError, "save paused task state failed, retaining execution ownership and retrying: %v", saveErr)
		timer := time.NewTimer(backoff)
		select {
		case <-w.stopChan:
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 10*time.Second {
			backoff *= 2
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
		}
	}
}

// acknowledgeStoppedTask retains the exact live lease and retries STOP closure
// until the server confirms it or proves that this generation no longer owns
// the lease. A worker shutdown may hand the still-owned lease to dead-owner
// recovery only after its instance heartbeat is withdrawn.
func (w *Worker) acknowledgeStoppedTask(task *scheduler.TaskInfo) error {
	backoff := time.Second
	for {
		ackCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := w.updateTaskStatus(ackCtx, task, scheduler.TaskStatusStopped, "Task stopped by user")
		cancel()
		if err == nil || errors.Is(err, scheduler.ErrTaskLeaseConflict) {
			return err
		}
		w.taskLog(task.TaskId, LevelError,
			"STOP acknowledgement failed for generation %s; retaining ownership and retrying: %v",
			task.DispatchGeneration, err)
		timer := time.NewTimer(backoff)
		select {
		case <-w.stopChan:
			timer.Stop()
			return err
		case <-timer.C:
		}
		if backoff < 10*time.Second {
			backoff *= 2
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
		}
	}
}

// createTaskContext 创建带有任务控制信号检查的上下文
// 当任务被停止或暂停时，上下文会被取消
func (w *Worker) createTaskContext(parentCtx context.Context, task *scheduler.TaskInfo) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parentCtx)

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ctrl := w.checkTaskControl(ctx, task)
				if ctrl == scheduler.TaskControlActionStop || ctrl == scheduler.TaskControlActionPause {
					w.taskLog(task.TaskId, LevelInfo, "Task %s generation %s received %s, canceling context",
						task.TaskId, task.DispatchGeneration, ctrl)
					cancel()
					return
				}
			}
		}
	}()

	return ctx, cancel
}

const taskLeaseHeartbeatInterval = 2 * time.Minute

func (w *Worker) renewTaskLease(ctx context.Context, task *scheduler.TaskInfo) error {
	if task == nil || task.TaskId == "" || task.LeaseToken == "" {
		return scheduler.ErrTaskLeaseConflict
	}
	if w.schedClient != nil {
		return w.schedClient.RenewTaskLease(ctx, task.TaskId, task.LeaseToken)
	}
	if w.httpClient != nil {
		return w.httpClient.RenewTaskLease(ctx, task.TaskId, task.LeaseToken)
	}
	return fmt.Errorf("no task lease client configured")
}

func (w *Worker) runTaskLeaseHeartbeat(ctx context.Context, cancelTask context.CancelFunc, task *scheduler.TaskInfo, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(taskLeaseHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := w.renewTaskLease(renewCtx, task)
			cancel()
			if errors.Is(err, scheduler.ErrTaskLeaseConflict) {
				w.markTaskLeaseLost(task)
				cancelTask()
				return
			}
			if err != nil {
				w.taskLog(task.TaskId, LevelWarn, "Task lease renewal failed: %v", err)
			}
		}
	}
}

// executeTask 执行任务
func (w *Worker) executeTask(task *scheduler.TaskInfo) {
	// Keep the exact generation state available to every deferred reporter and
	// the panic handler; remove it only after those defers have completed.
	defer w.unregisterTaskLeaseState(task)
	w.registerTaskLeaseState(task, nil)

	// 添加 panic 恢复机制，防止单个任务的 panic 导致整个 Worker 挂掉
	defer func() {
		if r := recover(); r != nil {
			// 使用 Worker 级别 logger，避免在 cleanupTaskLogger 之后创建孤儿 task logger
			w.logger.Error("[Task:%s] Task execution panic recovered: %v, stack: %s", task.TaskId, r, string(getStackTrace()))
			ctx := context.Background()
			w.updateTaskStatus(ctx, task, scheduler.TaskStatusFailure, fmt.Sprintf("Task panic: %v", r))
		}
	}()

	// baseCtx 不设整体超时：超时控制下放到各扫描阶段（端口扫描/端口识别/指纹/POC 等
	// 均有阶段级或单目标级超时）。此前的整体超时会在长链路任务中途掐断剩余阶段，
	// 且退出日志与用户手动停止无法区分，导致已启用阶段被静默跳过。
	baseCtx := context.Background()
	startTime := time.Now()

	w.mu.Lock()
	w.taskStarted++
	w.mu.Unlock()

	// These variables are declared before fallback defers so a parent-fenced
	// PAUSE at any execution point still has a live snapshot producer.
	var allAssets []*scanner.Asset
	var timedOutHostPorts map[string]struct{}
	var completedPhases map[string]bool

	// 使用 defer 确保无论任务如何结束，taskExecuted 都会递增
	// 这样 runningCount (taskStarted - taskExecuted) 才能正确反映正在执行的任务数
	defer func() {
		w.mu.Lock()
		w.taskExecuted++
		w.mu.Unlock()

		// Exact control ownership is not execution-scoped. It was registered at
		// acquisition and is released only by confirmed close/requeue/replacement.
		w.cleanupTaskLogger(task.TaskId)
	}()

	// A synthetic execution failure is emitted only when no canonical final
	// payload was durably accepted. PAUSE remains resumable and skips fallback.
	finalPayloadAccepted := false
	targetCount := 0 // 仅用于日志展示
	incrSent := 0    // 已确认由服务端记录的增量数
	expectedPhaseKeys := []string(nil)
	reportedPhaseKeys := make(map[string]bool)
	reportPhase := func(reportCtx context.Context, phase string, isCompleted bool, incrAmount int, phaseResult PhaseResult) phaseReportAck {
		if w.taskLeaseLost(task) || w.taskLeaseClosed(task) {
			return phaseReportAck{}
		}
		if incrAmount <= 0 {
			incrAmount = 1
		}
		ack := w.incrSubTaskDone(reportCtx, task, phase, isCompleted, incrAmount, phaseResult)
		if !ack.Recorded {
			return ack
		}
		incrSent += incrAmount
		canonical := canonicalTaskPhase(phaseResult.Phase)
		if canonical != "complete" && canonical != "subtask_complete" && canonical != "execution" {
			reportedPhaseKeys[canonical] = true
		}
		return ack
	}
	reportMissingPhases := func(reportCtx context.Context, canceled, skipped bool) {
		for _, expectedPhase := range expectedPhaseKeys {
			if w.taskLeaseLost(task) || w.taskLeaseClosed(task) {
				return
			}
			if reportedPhaseKeys[expectedPhase] {
				continue
			}
			var missing PhaseResult
			if skipped {
				missing = NewPhaseResult(expectedPhase, scanner.Coverage{}, false)
			} else {
				missing = missingPhaseResult(expectedPhase)
				if canceled {
					missing.Status = scanner.PhaseCanceled
					missing.Coverage = scanner.Coverage{Input: 1, Attempted: 1}
					missing.ReasonCodes = []string{scanner.ReasonCanceled}
				}
			}
			reportPhase(reportCtx, expectedPhase+":missing", false, 1, missing)
		}
	}
	defer func() {
		if finalPayloadAccepted || w.taskLeaseLost(task) || w.taskLeaseClosed(task) {
			return
		}
		bgCtx := context.Background()
		ctrl := w.checkTaskControl(bgCtx, task)
		if ctrl == scheduler.TaskControlActionPause {
			w.saveTaskProgress(bgCtx, task, completedPhases, allAssets, timedOutHostPorts)
			return
		}
		if ctrl == scheduler.TaskControlActionStop {
			_ = w.acknowledgeStoppedTask(task)
			return
		}
		reportMissingPhases(bgCtx, false, false)
		if w.taskLeaseLost(task) || w.taskLeaseClosed(task) {
			return
		}
		fallbackPhase := missingPhaseResult("execution")
		ack := reportPhase(bgCtx, "完成", true, 1, fallbackPhase)
		if ack.Recorded {
			finalPayloadAccepted = true
			if !ack.LeaseClosed {
				w.taskLog(task.TaskId, LevelInfo, "Fallback final payload accepted; lease cleanup remains pending")
			}
		}
	}()

	// A locally queued acquired task may already have a durable control before
	// execution starts. STOP closes it; PAUSE writes an empty resumable snapshot.
	switch ctrl := w.checkTaskControl(baseCtx, task); ctrl {
	case scheduler.TaskControlActionStop:
		w.taskLog(task.TaskId, LevelInfo, "任务在执行前已停止")
		return
	case scheduler.TaskControlActionPause:
		w.taskLog(task.TaskId, LevelInfo, "任务在执行前已暂停")
		w.saveTaskProgress(baseCtx, task, completedPhases, allAssets, timedOutHostPorts)
		return
	}

	// 创建带有任务控制信号检查的上下文
	ctx, cancelTask := w.createTaskContext(baseCtx, task)
	w.registerTaskLeaseState(task, cancelTask)
	leaseHeartbeatDone := make(chan struct{})
	go w.runTaskLeaseHeartbeat(ctx, cancelTask, task, leaseHeartbeatDone)
	defer func() {
		cancelTask()
		<-leaseHeartbeatDone
	}()

	if err := w.updateTaskStatus(ctx, task, scheduler.TaskStatusStarted, ""); errors.Is(err, scheduler.ErrTaskLeaseConflict) || errors.Is(err, scheduler.ErrTaskParentFenced) {
		return
	}

	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		w.taskLog(task.TaskId, LevelError, "配置解析失败：%v", err)
		w.updateTaskStatus(ctx, task, scheduler.TaskStatusFailure, "配置解析失败: "+err.Error())
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
	if taskType == "fingerprint_validate" {
		w.executeFingerprintValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "active_fingerprint_validate" {
		w.executeActiveFingerprintValidateTask(ctx, task, taskConfig, startTime)
		return
	}
	if taskType == "vuln_reverify" {
		w.executeVulnReverifyTask(ctx, task, taskConfig, startTime)
		return
	}

	// 获取目标
	target, _ := taskConfig["target"].(string)
	if target == "" {
		w.taskLog(task.TaskId, LevelError, "目标为空")
		w.updateTaskStatus(ctx, task, scheduler.TaskStatusFailure, "Target is empty")
		return
	}

	// 获取组织ID
	orgId, _ := taskConfig["orgId"].(string)

	var allVuls []*scanner.Vulnerability
	var skippedHosts []string // 因端口阈值超限被跳过的主机

	// 解析扫描配置
	config, err := scheduler.ParseTaskConfig(task.Config)
	if err != nil {
		w.taskLog(task.TaskId, LevelError, "Failed to parse task config: %v", err)
		w.updateTaskStatus(ctx, task, scheduler.TaskStatusFailure, "配置解析失败: "+err.Error())
		return
	}
	if config == nil {
		w.taskLog(task.TaskId, LevelError, "Task config is nil after parsing")
		w.updateTaskStatus(ctx, task, scheduler.TaskStatusFailure, "任务配置为空")
		return
	}

	// 记录启用阶段的稳定 canonical key；结束时若某阶段报告未被服务端确认，
	// 逐个写入 FAILED/CANCELED 结论，不能用 complete 的额外 weight 补齐。
	expectedPhaseKeys = enabledTaskPhaseKeys(config)

	// 输出任务开始日志（包含关键配置信息）
	var enabledPhases []string
	var configDetails []string

	if config.DomainScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("DomainScan.Enable=%v", config.DomainScan.Enable))
		if config.DomainScan.Enable {
			enabledPhases = append(enabledPhases, "子域名扫描")
		}
	} else {
		configDetails = append(configDetails, "DomainScan=nil")
	}

	if config.PortScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("PortScan.Enable=%v", config.PortScan.Enable))
		if config.PortScan.Enable {
			enabledPhases = append(enabledPhases, "端口扫描")
		}
	} else {
		configDetails = append(configDetails, "PortScan=nil")
	}

	if config.PortIdentify != nil {
		configDetails = append(configDetails, fmt.Sprintf("PortIdentify.Enable=%v", config.PortIdentify.Enable))
		if config.PortIdentify.Enable {
			enabledPhases = append(enabledPhases, "端口识别")
		}
	} else {
		configDetails = append(configDetails, "PortIdentify=nil")
	}

	if config.Fingerprint != nil {
		configDetails = append(configDetails, fmt.Sprintf("Fingerprint.Enable=%v", config.Fingerprint.Enable))
		if config.Fingerprint.Enable {
			enabledPhases = append(enabledPhases, "指纹识别")
		}
	} else {
		configDetails = append(configDetails, "Fingerprint=nil")
	}

	if config.BruteScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("BruteScan.Enable=%v", config.BruteScan.Enable))
		if config.BruteScan.Enable {
			enabledPhases = append(enabledPhases, "弱口令扫描")
			w.taskLog(task.TaskId, LevelInfo, "BruteScan config: services=%v, threads=%d, timeout=%d, dictIds=%v",
				config.BruteScan.Services, config.BruteScan.Threads, config.BruteScan.Timeout, config.BruteScan.WeakpassDictIds)
		}
	} else {
		configDetails = append(configDetails, "BruteScan=nil")
	}

	if config.DirScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("DirScan.Enable=%v", config.DirScan.Enable))
		if config.DirScan.Enable {
			enabledPhases = append(enabledPhases, "目录扫描")
		}
	} else {
		configDetails = append(configDetails, "DirScan=nil")
	}

	if config.JSFinder != nil {
		configDetails = append(configDetails, fmt.Sprintf("JSFinder.Enable=%v", config.JSFinder.Enable))
		if config.JSFinder.Enable {
			enabledPhases = append(enabledPhases, "JS扫描")
		}
	} else {
		configDetails = append(configDetails, "JSFinder=nil")
	}

	if config.PocScan != nil {
		configDetails = append(configDetails, fmt.Sprintf("PocScan.Enable=%v", config.PocScan.Enable))
		if config.PocScan.Enable {
			enabledPhases = append(enabledPhases, "漏洞扫描")
		}
	} else {
		configDetails = append(configDetails, "PocScan=nil")
	}

	// 检查是否有启用的扫描阶段
	if len(enabledPhases) == 0 {
		w.taskLog(task.TaskId, LevelError, "No scan phases enabled in config")
		w.taskLog(task.TaskId, LevelError, "Config details: %s", strings.Join(configDetails, ", "))
		w.taskLog(task.TaskId, LevelDebug, "Full config JSON: %s", task.Config)
		w.updateTaskStatus(ctx, task, scheduler.TaskStatusFailure, "未启用任何扫描阶段")
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
			// 黑名单过滤是合法的“不适用”：每个启用阶段都显式记录
			// SKIPPED_NOT_APPLICABLE，完成报告只承担最后一个计数单位。
			reportMissingPhases(ctx, false, true)
			blacklistPhase := NewPhaseResult("complete", scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1}, false)
			ack := reportPhase(ctx, "完成", true, 1, blacklistPhase)
			if ack.Recorded {
				finalPayloadAccepted = true
				switch {
				case ack.LeaseClosed:
					w.taskLog(task.TaskId, LevelInfo, "所有目标均被黑名单过滤，任务完成")
				case ack.FinalizationPending:
					w.taskLog(task.TaskId, LevelInfo, "黑名单完成结果已接受，任务终态仍在协调")
				default:
					w.taskLog(task.TaskId, LevelInfo, "黑名单完成结果已接受，租约清理仍在协调")
				}
			} else {
				w.taskLog(task.TaskId, LevelError, "Failed to record blacklist completion payload")
			}
			return
		}
	}

	// 输出任务开始日志
	targetCount = len(targets)
	w.taskLog(task.TaskId, LevelInfo, "任务开始")
	w.taskLog(task.TaskId, LevelInfo, "扫描阶段：%s", strings.Join(enabledPhases, " → "))
	w.taskLog(task.TaskId, LevelInfo, "扫描目标（%d）：%s", targetCount, strings.Join(targets, "，"))

	// 解析恢复状态（如果是继续执行的任务）
	var resumeState map[string]interface{}
	if stateStr, ok := taskConfig["resumeState"].(string); ok && stateStr != "" {
		json.Unmarshal([]byte(stateStr), &resumeState)
		w.taskLog(task.TaskId, LevelInfo, "Resuming from saved state")
	}
	completedPhases = make(map[string]bool)
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
		if timeoutKeys, ok := resumeState["timedOutHostPorts"].([]interface{}); ok {
			timedOutHostPorts = make(map[string]struct{}, len(timeoutKeys))
			for _, value := range timeoutKeys {
				if key, ok := value.(string); ok && key != "" {
					timedOutHostPorts[key] = struct{}{}
				}
			}
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
		if ctrl := w.checkTaskControl(ctx, task); ctrl == "STOP" {
			w.logTaskAbort(task, config, completedPhases, "Task stopped by user before domain scan")
			return
		}

		// 子域名扫描仅针对“纯一级域名”：IP/CIDR/端口范围、以及子域名(www.example.com)
		// 直接跳过，不做子域名枚举；多段公共后缀(com.cn 等)下的一级域名为 eTLD+1(如 example.com.cn)。
		// 模块自行判断输入适用性，编排层不再按目标类型预关模块。
		eligibleTargets := filterEligibleSubdomainTargets(targets)
		if len(eligibleTargets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "子域名扫描不适用于当前目标，已跳过")
			completedPhases["domainscan"] = true
			reportPhase(ctx, "子域名扫描", false, 1, NewPhaseResult("domainscan", scanner.Coverage{}, false))

			goto domainScanDone
		}
		domainScanTarget := strings.Join(eligibleTargets, "\n")

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task, 10, "子域名扫描中", "子域名扫描")
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
					OnProgress: w.makeOnProgress(task, "子域名扫描"),
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
							OnProgress: w.makeOnProgress(task, "子域名爆破"),
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

		// 子域接管检测：覆盖 subfinder + bruteforce 发现的全部子域，命中即写入漏洞
		if len(mergedAssets) > 0 {
			if ts, ok := w.scanners["subdomain_bruteforce"].(*scanner.SubdomainBruteforceScanner); ok {
				w.taskLog(task.TaskId, LevelInfo, "Takeover check: checking %d subdomains for takeover risk", len(mergedAssets))
				takeoverVuls := ts.CheckTakeover(ctx, mergedAssets, &scanner.SubdomainBruteforceOptions{
					Timeout:    10,
					Concurrent: 20,
					OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
						w.saveVulResultWithFallback(ctx, task.MainTaskId, []*scanner.Vulnerability{vul})
					},
				}, func(level, format string, args ...interface{}) {
					w.taskLog(task.TaskId, level, format, args...)
				})
				if len(takeoverVuls) > 0 {
					w.taskLog(task.TaskId, LevelWarn, "Takeover check: %d subdomains vulnerable to takeover", len(takeoverVuls))
				}
			}
		}

		if len(mergedAssets) > 0 {
			allAssets = append(allAssets, mergedAssets...)
		}
		if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "domain scan") {
			return
		}

		// 检查context是否被取消
		select {
		case <-ctx.Done():
			w.taskLog(task.TaskId, LevelInfo, "Domain scan canceled by context")
			// 资产已在各阶段完成后立即保存，此处只需保存任务进度
			w.saveTaskProgress(ctx, task, completedPhases, allAssets, timedOutHostPorts)
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
		domainCoverage := scanner.Coverage{Input: len(eligibleTargets), Attempted: len(eligibleTargets), Succeeded: len(eligibleTargets)}
		domainPhase := NewPhaseResult("domainscan", domainCoverage, false)
		domainPhase.Assets = len(mergedAssets)
		domainPhase.UsableResults = domainCoverage.Succeeded > 0 || len(mergedAssets) > 0
		reportPhase(ctx, "子域名扫描", false, 1, domainPhase)

	}

domainScanDone:

	// 执行端口扫描（只有明确启用时才执行）
	if config.PortScan != nil && config.PortScan.Enable && !completedPhases["portscan"] {
		// 检查控制信号
		if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
			return
		}

		// 更新当前阶段
		w.updateTaskProgressWithPhase(ctx, task, 20, "端口扫描中", "端口扫描")

		// DNS 预解析过滤：仅将可解析的目标投入端口扫描，不可解析的直接跳过。
		// 大量无法解析的 dev/test 子域名会让 naabu 对每个目标空跑满超时，
		// 显著拖慢整体链路（总超时 = 单目标超时 × 目标数）。此处提前剔除。
		// resolvedIPs 缓存预解析结果，端口扫描后回填 asset，避免下游重复 DNS。
		var resolvedIPs map[string][]net.IP
		{
			resolvable, unresolvable, resolved := w.filterResolvableTargets(ctx, targets)
			resolvedIPs = resolved
			if len(unresolvable) > 0 {
				w.taskLog(task.TaskId, LevelInfo, "Port scan: DNS pre-filter skipped %d/%d unresolvable targets", len(unresolvable), len(targets))
				for _, t := range unresolvable {
					w.taskLog(task.TaskId, LevelDebug, "Port scan: skip unresolvable target: %s", t)
				}
			}
			if len(resolvable) == 0 {
				w.taskLog(task.TaskId, LevelWarn, "Port scan: no resolvable targets, skipping port scan phase")
				completedPhases["portscan"] = true
				reportPhase(ctx, "端口扫描", false, 1, NewPhaseResult("portscan", scanner.Coverage{}, false))

				goto portScanDone
			}
			targets = resolvable
			target = strings.Join(resolvable, "\n")
		}

		// Naabu 每个目标启动一个独立进程，单子任务内最多并行 5 个进程；
		// Workers 同时作为 naabu -c 的内部线程数，但不会突破进程级全局信号量。
		if config.PortScan.Workers <= 0 {
			config.PortScan.Workers = 2
		}

		// 根据配置选择端口发现工具（默认使用 Naabu）。
		portDiscoveryTool := "naabu"
		if config.PortScan.Tool != "" {
			portDiscoveryTool = config.PortScan.Tool
		}

		// Web 的 targetTimeout 是单个目标完整扫描的硬上限；probeTimeoutMs 仅控制
		// Naabu 单次探测等待时间。二者不得互相推导或覆盖。
		targetTimeout := config.PortScan.TargetTimeout
		if targetTimeout <= 0 {
			targetTimeout = scanner.DefaultPortScanTargetTimeoutSeconds
		}
		probeTimeoutMs := config.PortScan.ProbeTimeoutMs
		if probeTimeoutMs <= 0 {
			probeTimeoutMs = scanner.DefaultNaabuProbeTimeoutMilliseconds
		}
		config.PortScan.TargetTimeout = targetTimeout
		config.PortScan.ProbeTimeoutMs = probeTimeoutMs

		portTargetCount := len(targets)
		if portDiscoveryTool == "naabu" {
			// 与 Naabu 的真实执行模型保持一致：CIDR/IP range 会先展开，再按 host 去重。
			portTargetCount = scanner.CountNaabuProcessTargets(target)
		}
		if portTargetCount <= 0 {
			portTargetCount = 1
		}
		processConcurrency := 1
		if portDiscoveryTool == "naabu" {
			processConcurrency = scanner.EffectiveNaabuProcessConcurrency(config.PortScan.Workers, portTargetCount)
		}
		waves := (portTargetCount + processConcurrency - 1) / processConcurrency
		const phaseBufferSeconds = 30
		portScanTimeout := targetTimeout*waves + phaseBufferSeconds

		// 估算值仅用于提示配置可能过紧，绝不再偷偷提高用户设置的单目标上限。
		portCount := scanner.EstimatePortCount(config.PortScan.Ports)
		rate := config.PortScan.Rate
		if rate <= 0 {
			rate = 1000
		}
		retries := config.PortScan.Retries
		if retries < 0 {
			retries = 0
		}
		estimateDenominator := rate * 2
		estimatedPerTarget := (portCount*(1+retries)*3 + estimateDenominator - 1) / estimateDenominator
		if estimatedPerTarget < 1 {
			estimatedPerTarget = 1
		}
		if estimatedPerTarget > targetTimeout {
			w.taskLog(task.TaskId, LevelWarn,
				"Port scan: targetTimeout=%ds may be insufficient for estimated runtime=%ds (ports=%d, rate=%d, retries=%d)",
				targetTimeout, estimatedPerTarget, portCount, rate, retries)
		}

		toolDisplay := strings.TrimSpace(portDiscoveryTool)
		switch strings.ToLower(toolDisplay) {
		case "naabu":
			toolDisplay = "Naabu"
		case "masscan":
			toolDisplay = "Masscan"
		case "tcp":
			toolDisplay = "TCP"
		}
		portRangeDisplay := strings.TrimSpace(config.PortScan.Ports)
		if portRangeDisplay == "" {
			portRangeDisplay = "默认端口"
		}
		w.taskLog(task.TaskId, LevelInfo,
			"端口扫描开始：%s，目标 %d 个，端口范围 %s，单目标超时 %d 秒",
			toolDisplay, portTargetCount, portRangeDisplay, targetTimeout)
		portCtx, portCancel := context.WithTimeout(ctx, time.Duration(portScanTimeout)*time.Second)

		var openPorts []*scanner.Asset
		portScanTimedOut := false
		portPhaseResult := NewPhaseResult("portscan", scanner.Coverage{Input: portTargetCount, Attempted: portTargetCount, Succeeded: portTargetCount}, false)

		// 创建任务日志回调
		taskLogger := func(level, format string, args ...interface{}) {
			w.taskLog(task.TaskId, level, format, args...)
		}

		// 创建进度回调（统一使用 makeOnProgress，基于分子/分母实时计算主任务进度）
		onProgress := w.makeOnProgress(task, "端口扫描")

		// 第一步：端口发现
		switch portDiscoveryTool {
		case "masscan":
			masscanScanner := w.scanners["masscan"]
			masscanResult, err := masscanScanner.Scan(portCtx, &scanner.ScanConfig{
				Target:     target,
				Options:    config.PortScan,
				TaskLogger: taskLogger,
				OnProgress: onProgress,
			})
			// 检查是否被停止或超时
			if portCtx.Err() == context.DeadlineExceeded {
				portScanTimedOut = true
			} else if ctx.Err() != nil {
				portCancel()
				w.logTaskAbort(task, config, completedPhases, fmt.Sprintf("Port scan aborted, task canceled (context error: %v)", ctx.Err()))
				return
			}
			if err != nil {
				w.taskLog(task.TaskId, LevelError, "Masscan error: %v", err)
			}
			if masscanResult != nil {
				portPhaseResult = PhaseResultFromDiagnostic("portscan", masscanResult.Diagnostic, len(masscanResult.Assets))
				if len(masscanResult.Assets) > 0 {
					openPorts = masscanResult.Assets
				}
				if len(masscanResult.SkippedHosts) > 0 {
					skippedHosts = append(skippedHosts, masscanResult.SkippedHosts...)
				}
				if len(masscanResult.DNSFailedHosts) > 0 {
					skippedHosts = append(skippedHosts, masscanResult.DNSFailedHosts...)
					w.taskLog(task.TaskId, LevelInfo, "DNS resolution failed for %d hosts, will skip in subsequent phases", len(masscanResult.DNSFailedHosts))
				}
			}
		case "tcp":
			tcpOptions := &scanner.PortScanOptions{
				Tool:              "tcp",
				Ports:             config.PortScan.Ports,
				Rate:              config.PortScan.Rate,
				Timeout:           targetTimeout,
				TargetTimeout:     targetTimeout,
				ProbeTimeoutMs:    probeTimeoutMs,
				Concurrent:        config.PortScan.Workers,
				PortThreshold:     config.PortScan.PortThreshold,
				ScanType:          config.PortScan.ScanType,
				SkipHostDiscovery: config.PortScan.SkipHostDiscovery,
				ExcludeCDN:        config.PortScan.ExcludeCDN,
				ExcludeHosts:      config.PortScan.ExcludeHosts,
				Retries:           config.PortScan.Retries,
				WarmUpTime:        config.PortScan.WarmUpTime,
				Workers:           config.PortScan.Workers,
				Verify:            config.PortScan.Verify,
			}
			if tcpOptions.Ports == "" {
				tcpOptions.Ports = "21,22,23,25,80,443,3306,3389,6379,8080"
			}
			tcpScanner := w.scanners["portscan"]
			tcpResult, err := tcpScanner.Scan(portCtx, &scanner.ScanConfig{
				Target:     target,
				Options:    tcpOptions,
				TaskLogger: taskLogger,
				OnProgress: onProgress,
			})
			if portCtx.Err() == context.DeadlineExceeded {
				portScanTimedOut = true
			} else if ctx.Err() != nil {
				portCancel()
				w.logTaskAbort(task, config, completedPhases, fmt.Sprintf("Port scan aborted, task canceled (context error: %v)", ctx.Err()))
				return
			}
			if err != nil {
				w.taskLog(task.TaskId, LevelError, "TCP scan error: %v", err)
			}
			if tcpResult != nil {
				portPhaseResult = PhaseResultFromDiagnostic("portscan", tcpResult.Diagnostic, len(tcpResult.Assets))
				if len(tcpResult.Assets) > 0 {
					openPorts = tcpResult.Assets
				}
				if len(tcpResult.SkippedHosts) > 0 {
					skippedHosts = append(skippedHosts, tcpResult.SkippedHosts...)
				}
				if len(tcpResult.DNSFailedHosts) > 0 {
					skippedHosts = append(skippedHosts, tcpResult.DNSFailedHosts...)
					w.taskLog(task.TaskId, LevelInfo, "DNS resolution failed for %d hosts, will skip in subsequent phases", len(tcpResult.DNSFailedHosts))
				}
			}
		case "naabu":
			naabuScanner := w.scanners["naabu"]
			naabuResult, err := naabuScanner.Scan(portCtx, &scanner.ScanConfig{
				Target:      target,
				Options:     config.PortScan,
				TaskLogger:  taskLogger,
				EventLogger: w.scannerEventLogger(task.TaskId),
				OnProgress:  onProgress,
				OnTargetDone: func(target string, assets []*scanner.Asset) {
					// 流式入库：单目标端口扫描完成立即保存
					if len(assets) == 0 {
						return
					}
					// 回填预解析 IP，避免下游重复 DNS
					backfillAssetIPs(assets, resolvedIPs)
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
				portScanTimedOut = true
			} else if ctx.Err() != nil || w.checkTaskControl(ctx, task) == "STOP" {
				portCancel()
				reason := "Port scan aborted, task canceled or stopped"
				if ctx.Err() != nil {
					reason = fmt.Sprintf("%s (context error: %v)", reason, ctx.Err())
				}
				w.logTaskAbort(task, config, completedPhases, reason)
				return
			}
			if err != nil && err != scanner.ErrPortThresholdExceeded {
				w.taskLog(task.TaskId, LevelError, "Naabu error: %v", err)
			}
			if naabuResult != nil {
				portPhaseResult = PhaseResultFromDiagnostic("portscan", naabuResult.Diagnostic, len(naabuResult.Assets))
				if portPhaseResult.Coverage.TimedOut > 0 {
					portScanTimedOut = true
				}
				if len(naabuResult.Assets) > 0 {
					openPorts = naabuResult.Assets
				}
				if len(naabuResult.SkippedHosts) > 0 {
					skippedHosts = append(skippedHosts, naabuResult.SkippedHosts...)
				}
				if len(naabuResult.DNSFailedHosts) > 0 {
					skippedHosts = append(skippedHosts, naabuResult.DNSFailedHosts...)
					w.taskLog(task.TaskId, LevelInfo, "DNS resolution failed for %d hosts, will skip in subsequent phases", len(naabuResult.DNSFailedHosts))
				}
			}
		default:
			w.taskLog(task.TaskId, LevelError, "Unsupported port scan tool: %s", portDiscoveryTool)
			portCancel()
			w.logTaskAbort(task, config, completedPhases, fmt.Sprintf("Unsupported port scan tool: %s", portDiscoveryTool))
			return
		}

		// 检查是否被停止
		if ctx.Err() != nil || w.checkTaskControl(ctx, task) == "STOP" {
			portCancel()
			reason := "Port scan aborted, task canceled or stopped"
			if ctx.Err() != nil {
				reason = fmt.Sprintf("%s (context error: %v)", reason, ctx.Err())
			}
			w.logTaskAbort(task, config, completedPhases, reason)
			return
		}

		// 端口发现完成，将结果添加到全量资产；后续阶段汇总单独输出。
		if len(openPorts) > 0 {
			backfillAssetIPs(openPorts, resolvedIPs)
			for _, asset := range openPorts {
				asset.IsHTTP = scanner.IsHTTPService(asset.Service, asset.Port)
			}
			allAssets = append(allAssets, openPorts...)
		}

		portCancel()
		if len(skippedHosts) > 0 {
			w.taskLog(task.TaskId, LevelWarn, "端口扫描跳过 %d 个超过端口阈值的主机", len(skippedHosts))
		}
		completedPhases["portscan"] = true
		portPhaseResult.Assets = len(openPorts)
		portPhaseResult.UsableResults = len(openPorts) > 0 || portPhaseResult.Coverage.Succeeded > 0

		openPortList := make([]string, 0, len(openPorts))
		for _, asset := range openPorts {
			if asset != nil {
				openPortList = append(openPortList, fmt.Sprintf("%s:%d", asset.Host, asset.Port))
			}
		}
		portList := "无"
		if len(openPortList) > 0 {
			portList = strings.Join(openPortList, "，")
		}
		// Naabu 已逐目标输出开放端口和超时；编排层只保留唯一完成摘要。
		// 其他端口发现器没有逐目标结果日志，继续在这里输出结果列表。
		if !strings.EqualFold(portDiscoveryTool, "naabu") {
			if portScanTimedOut {
				w.taskLog(task.TaskId, LevelWarn, "端口扫描部分结果：%s 超时，已保留 %d 个开放端口：%s", portDiscoveryTool, len(openPorts), portList)
			} else {
				w.taskLog(task.TaskId, LevelInfo, "端口扫描结果：发现 %d 个开放端口：%s", len(openPorts), portList)
			}
		}
		w.taskLog(task.TaskId, LevelInfo, "端口扫描完成：状态 %s，成功 %d，超时 %d，资产 %d",
			portPhaseResult.Status, portPhaseResult.Coverage.Succeeded, portPhaseResult.Coverage.TimedOut, len(openPorts))
		reportPhase(ctx, "端口扫描", false, 1, portPhaseResult)

	}

portScanDone:

	// 检查控制信号
	if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
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
			reportPhase(ctx, "端口识别", false, 1, NewPhaseResult("portidentify", scanner.Coverage{}, false))

		} else {
			// 检查控制信号
			if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task, 40, "端口识别中", "端口识别")

			var identifiedAssets []*scanner.Asset
			var identifyPhase PhaseResult
			tool := strings.ToLower(strings.TrimSpace(config.PortIdentify.Tool))
			if tool == "" || tool == "nmap" {
				merged := w.executePortIdentifyWithNmapResult(ctx, task, allAssets, config.PortIdentify, orgId)
				identifiedAssets = merged.Assets
				timedOutHostPorts = merged.TimedOutHostPorts
				identifyPhase = merged.Phase
			} else {
				identifiedAssets = w.executePortIdentify(ctx, task, allAssets, config.PortIdentify, orgId)
				identifyPhase = NewPhaseResult("portidentify", scanner.Coverage{Input: len(allAssets), Attempted: len(allAssets), Succeeded: len(identifiedAssets)}, false)
			}
			// Nmap execution already returns the cloned, stable-deduplicated discovery
			// baseline with per-port outcomes merged. Do not reconstruct or supplement
			// it here: doing so previously reintroduced replacement semantics.
			if tool == "" || tool == "nmap" || len(identifiedAssets) > 0 {
				allAssets = identifiedAssets
			}
			identifyPhase.Assets = len(allAssets)
			identifyPhase.UsableResults = len(allAssets) > 0
			completedPhases["portidentify"] = true
			// 端口识别模块完成，递增子任务进度
			reportPhase(ctx, "端口识别", false, 1, identifyPhase)

		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
		return
	}

	// 执行指纹识别
	if config.Fingerprint != nil && config.Fingerprint.Enable && !completedPhases["fingerprint"] {
		fingerprintPhaseResult := NewPhaseResult("fingerprint", scanner.Coverage{}, false)
		// 强制扫描模式：没有资产时从用户输入目标生成资产
		// GenerateAssetsFromTargets 已过滤DNS解析失败的域名
		if len(allAssets) == 0 && target != "" && config.Fingerprint.ForceScan {
			generatedAssets := scanner.GenerateAssetsFromTargets(target)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
			}
		}

		fingerprintCandidates := excludeAssetsByHostPort(allAssets, timedOutHostPorts)
		// 没有可扫描资产时跳过；TIMEOUT 资产仍保留在 allAssets 中用于入库和计数。
		if len(fingerprintCandidates) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "指纹识别无可扫描资产，已跳过")
			completedPhases["fingerprint"] = true
			reportPhase(ctx, "指纹识别", false, 1, fingerprintPhaseResult)

		} else {
			// 在指纹识别开始前检查停止信号
			if ctrl := w.checkTaskControl(ctx, task); ctrl == "STOP" {
				w.logTaskAbort(task, config, completedPhases, "Task stopped by user before fingerprint")
				return
			} else if ctrl == "PAUSE" {
				w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
				w.saveTaskProgress(ctx, task, completedPhases, allAssets, timedOutHostPorts)
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task, 60, "指纹识别中", "指纹识别")

			if s, ok := w.scanners["fingerprint"]; ok {
				assetsToScan := fingerprintCandidates
				filterMode := config.Fingerprint.FilterMode
				if filterMode == "" {
					filterMode = "http_mapping"
				}
				targetTimeout := config.Fingerprint.TargetTimeout
				if targetTimeout <= 0 {
					targetTimeout = 30
				}
				config.Fingerprint.Concurrency = w.config.Concurrency
				w.taskLog(task.TaskId, LevelInfo, "指纹识别开始：候选资产 %d，单目标超时 %d 秒，并发 %d，主动识别 %t，截图 %t",
					len(fingerprintCandidates), targetTimeout, w.config.Concurrency, config.Fingerprint.ActiveScan, config.Fingerprint.Screenshot)

				nonHTTPCount := 0
				if filterMode == "service_mapping" {
					var httpAssets []*scanner.Asset
					for _, asset := range fingerprintCandidates {
						if globalHttpServiceChecker := scanner.GetHttpServiceChecker(); globalHttpServiceChecker != nil {
							serviceLower := strings.ToLower(asset.Service)
							if isHTTP, found := globalHttpServiceChecker.IsHttpService(serviceLower); found && !isHTTP {
								nonHTTPCount++
								continue
							}
						}
						httpAssets = append(httpAssets, asset)
					}
					assetsToScan = httpAssets
				} else {
					var httpAssets []*scanner.Asset
					for _, asset := range fingerprintCandidates {
						if scanner.IsHttpAsset(asset) {
							httpAssets = append(httpAssets, asset)
						} else {
							nonHTTPCount++
						}
					}
					assetsToScan = httpAssets
				}
				w.taskLog(task.TaskId, LevelInfo, "指纹识别过滤：输入 %d，排除超时端口 %d，排除非 HTTP 资产 %d，待扫描 %d",
					len(allAssets), len(allAssets)-len(fingerprintCandidates), nonHTTPCount, len(assetsToScan))

				// 每次扫描前实时加载HTTP服务映射配置
				httpPortCount, httpsPortCount, nonHTTPPortCount, httpMappingCount := w.loadHttpServiceMappings()

				// 如果启用自定义指纹引擎，加载自定义指纹（包括主动指纹）
				passiveFingerprintCount, activeFingerprintCount := 0, 0
				if config.Fingerprint.CustomEngine {
					passiveFingerprintCount, activeFingerprintCount = w.loadCustomFingerprints(ctx, s.(*scanner.FingerprintScanner), config.Fingerprint.ActiveScan)
				}
				w.taskLog(task.TaskId, LevelInfo,
					"本次扫描加载：HTTP 端口 %d 个、HTTPS 端口 %d 个、非 HTTP 端口 %d 个、HTTP 服务映射 %d 条、被动指纹 %d 条、主动指纹 %d 条",
					httpPortCount, httpsPortCount, nonHTTPPortCount, httpMappingCount, passiveFingerprintCount, activeFingerprintCount)

				// 仅继承任务取消；各模块使用自己的单目标超时。
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
					Assets:      assetsToScan,
					Options:     config.Fingerprint,
					TaskLogger:  fpTaskLogger,
					EventLogger: w.scannerEventLogger(task.TaskId),
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
				if result != nil {
					fingerprintPhaseResult = PhaseResultFromDiagnostic("fingerprint", result.Diagnostic, len(result.Assets))
				} else if err != nil {
					fingerprintPhaseResult = NewPhaseResult("fingerprint", scanner.Coverage{Input: len(assetsToScan), Attempted: len(assetsToScan), Failed: len(assetsToScan)}, false)
				}

				// fpCtx 现为取消专用（无自带 deadline），阶段级超时由各模块单目标超时独立控制；
				// 任务级取消/停止由下方 ctx.Err() 与 STOP 检查统一判定。
				// 检查是否被取消
				if ctx.Err() != nil || w.checkTaskControl(ctx, task) == "STOP" {
					reason := "Task aborted after fingerprint, task canceled or stopped"
					if ctx.Err() != nil {
						reason = fmt.Sprintf("%s (context error: %v)", reason, ctx.Err())
					}
					w.logTaskAbort(task, config, completedPhases, reason)
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
							originalAsset.FingerprintFindings = fpAsset.FingerprintFindings
							originalAsset.FingerprintFindingsCollected = fpAsset.FingerprintFindingsCollected
							originalAsset.HttpStatus = fpAsset.HttpStatus
							originalAsset.HttpHeader = fpAsset.HttpHeader
							originalAsset.HttpBody = fpAsset.HttpBody
							originalAsset.Server = fpAsset.Server
							originalAsset.IconHash = fpAsset.IconHash
							originalAsset.IsHTTP = fpAsset.IsHTTP
							originalAsset.ProtocolProbeStatus = fpAsset.ProtocolProbeStatus
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
			reportPhase(ctx, "指纹识别", false, 1, fingerprintPhaseResult)

		} // 结束 len(allAssets) > 0 的 else 分支
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
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
			reportPhase(ctx, "弱口令扫描", false, 1, NewPhaseResult("brutescan", scanner.Coverage{}, false))

		} else {
			// 检查控制信号
			if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task, 65, "弱口令扫描中", "弱口令扫描")

			// 执行弱口令扫描
			bruteVulns := w.executeBruteScan(ctx, task, allAssets, config.BruteScan, orgId)
			if len(bruteVulns) > 0 {
				w.taskLog(task.TaskId, LevelInfo, "Brute scan completed: found %d weak passwords", len(bruteVulns))
			}
			completedPhases["brutescan"] = true
			brutePhase := NewPhaseResult("brutescan", scanner.Coverage{Input: len(allAssets), Attempted: len(allAssets), Succeeded: len(allAssets)}, false)
			brutePhase.UsableResults = len(allAssets) > 0 || len(bruteVulns) > 0
			brutePhase.Vulnerabilities = len(bruteVulns)
			reportPhase(ctx, "弱口令扫描", false, 1, brutePhase)

		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
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
			reportPhase(ctx, "目录扫描", false, 1, NewPhaseResult("dirscan", scanner.Coverage{}, false))

		} else {
			// 检查控制信号
			if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task, 70, "目录扫描中", "目录扫描")

			// 执行目录扫描
			dirScanAssets := w.executeDirScan(ctx, task, allAssets, config.DirScan, orgId)
			if len(dirScanAssets) > 0 {
				// 注意：目录扫描结果不添加到 allAssets，避免影响后续 POC 扫描
				// 目录扫描结果是 URL 路径，不是独立的扫描目标
				w.taskLog(task.TaskId, LevelInfo, "Dir scan completed: found %d paths", len(dirScanAssets))
				// 目录扫描结果已在 executeDirScan 中通过 saveDirScanResults 保存到数据库
			}
			completedPhases["dirscan"] = true
			dirPhase := NewPhaseResult("dirscan", scanner.Coverage{Input: len(allAssets), Attempted: len(allAssets), Succeeded: len(allAssets)}, false)
			dirPhase.UsableResults = len(allAssets) > 0 || len(dirScanAssets) > 0
			reportPhase(ctx, "目录扫描", false, 1, dirPhase)

		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
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
			reportPhase(ctx, "JS扫描", false, 1, NewPhaseResult("jsfinder", scanner.Coverage{}, false))

		} else {
			if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
				return
			}

			w.updateTaskProgressWithPhase(ctx, task, 80, "JS扫描中", "JS扫描")

			jsfinderResults := w.executeJSFinder(ctx, task, allAssets, config.JSFinder, orgId)
			// 结果已通过 OnResultFound 流式入库，此处仅记录日志
			if len(jsfinderResults) > 0 {
				w.taskLog(task.TaskId, LevelInfo, "JSFinder: %d findings (streamed)", len(jsfinderResults))
			}
			w.updateTaskProgressWithPhase(ctx, task, 85, "JS扫描完成", "JS扫描")
			completedPhases["jsfinder"] = true
			jsPhase := NewPhaseResult("jsfinder", scanner.Coverage{Input: len(allAssets), Attempted: len(allAssets), Succeeded: len(allAssets)}, false)
			jsPhase.UsableResults = len(allAssets) > 0 || len(jsfinderResults) > 0
			reportPhase(ctx, "JS扫描", false, 1, jsPhase)

		}
	}

	// 检查控制信号
	if w.handleTaskControl(ctx, task, config, completedPhases, allAssets, timedOutHostPorts, "") {
		return
	}

	// 执行POC扫描 (使用Nuclei引擎)
	if config.PocScan != nil && config.PocScan.Enable && !completedPhases["pocscan"] {
		pocPhaseResult := NewPhaseResult("poc", scanner.Coverage{}, false)
		// 强制扫描模式：没有资产时从用户输入目标生成资产
		if len(allAssets) == 0 && target != "" && config.PocScan.ForceScan {
			generatedAssets := scanner.GenerateAssetsFromTargets(target)
			generatedAssets = filterSkippedHostsAssets(generatedAssets, skippedHosts)
			if len(generatedAssets) > 0 {
				allAssets = append(allAssets, generatedAssets...)
			}
		}

		pocAssets := excludeAssetsByHostPort(allAssets, timedOutHostPorts)
		// 没有可扫描资产时跳过；TIMEOUT 资产仍保留在全量资产结果中。
		if len(pocAssets) == 0 {
			w.taskLog(task.TaskId, LevelInfo, "漏洞扫描无可扫描资产，已跳过")
			completedPhases["pocscan"] = true
			pocPhaseResult = NewPhaseResult("poc", scanner.Coverage{}, false)
			reportPhase(ctx, "漏洞扫描", false, 1, pocPhaseResult)

		} else {
			// 在POC扫描开始前检查停止信号
			if ctrl := w.checkTaskControl(ctx, task); ctrl == "STOP" {
				w.logTaskAbort(task, config, completedPhases, "Task stopped by user before POC scan")
				return
			} else if ctrl == "PAUSE" {
				w.taskLog(task.TaskId, LevelInfo, "Task paused, saving progress...")
				w.saveTaskProgress(ctx, task, completedPhases, allAssets, timedOutHostPorts)
				return
			}

			// 更新当前阶段
			w.updateTaskProgressWithPhase(ctx, task, 80, "漏洞扫描中", "漏洞扫描")

			var templates []string
			var templateRefs []string
			var templateLoadResult TemplateLoadResult
			var templateLoadErr error
			explicitTemplateSelection := false
			if s, ok := w.scanners["nuclei"]; ok {
				// 获取单目标超时配置
				pocTargetTimeout := config.PocScan.TargetTimeout
				if pocTargetTimeout <= 0 {
					pocTargetTimeout = 600 // 默认600秒
				}
				w.taskLog(task.TaskId, LevelInfo, "漏洞扫描开始：资产 %d，单目标超时 %d 秒", len(pocAssets), pocTargetTimeout)
				loggedVulnerabilities := make(map[string]struct{})
				logVulnerability := func(vul *scanner.Vulnerability) {
					if vul == nil {
						return
					}
					key := vul.PocFile + "\x00" + vul.Url
					if _, exists := loggedVulnerabilities[key]; exists {
						return
					}
					loggedVulnerabilities[key] = struct{}{}
					w.taskLog(task.TaskId, LevelInfo, "发现漏洞：%s → %s", vul.PocFile, vul.Url)
				}

				// 模板解析：优先本地模板库（库文件硬链接进扫描目录），未命中回退 Mongo 内容加载
				w.ensureTemplateStore(ctx)
				// 检查是否有模板ID列表（任务创建时已筛选好的模板）
				if len(config.PocScan.NucleiTemplateIds) > 0 || len(config.PocScan.CustomPocIds) > 0 {
					explicitTemplateSelection = true
					templateLoadResult, templateLoadErr = w.resolveTemplatesByIdsResult(ctx, config.PocScan.NucleiTemplateIds, config.PocScan.CustomPocIds)
					templates, templateRefs = templateLoadResult.Contents, templateLoadResult.FileRefs
					loaded := len(templates) + len(templateRefs)
					if templateLoadResult.Loaded == 0 {
						templateLoadResult.Loaded = loaded
					}
					w.taskLogEvent(task.TaskId, LevelInfo, "模板加载结果已记录", EventPocTemplateLoad, "poc", string(templateLoadResult.Outcome), map[string]interface{}{
						"group_key": "explicit_ids", "asset_count": len(pocAssets),
						"requested": templateLoadResult.Requested, "loaded": loaded, "invalid": templateLoadResult.Invalid,
						"missing": len(templateLoadResult.MissingIDs), "source": templateLoadResult.Source,
						"outcome": string(templateLoadResult.Outcome), "reason_code": templateLoadResult.ReasonCode,
					})
				} else if config.PocScan.CustomPocOnly {
					explicitTemplateSelection = true
					// 只使用自定义POC模式，但没有指定具体ID，获取所有自定义POC
					severities := []string{}
					if config.PocScan.Severity != "" {
						severities = strings.Split(config.PocScan.Severity, ",")
					}
					templateLoadResult, templateLoadErr = w.resolveAllCustomPocsResult(ctx, severities)
					templates, templateRefs = templateLoadResult.Contents, templateLoadResult.FileRefs
					loaded := len(templates) + len(templateRefs)
					if templateLoadResult.Loaded == 0 {
						templateLoadResult.Loaded = loaded
					}
					w.taskLogEvent(task.TaskId, LevelInfo, "模板加载结果已记录", EventPocTemplateLoad, "poc", string(templateLoadResult.Outcome), map[string]interface{}{
						"group_key": "custom_poc_only", "asset_count": len(pocAssets), "requested": templateLoadResult.Requested,
						"loaded": loaded, "invalid": templateLoadResult.Invalid, "source": templateLoadResult.Source,
						"outcome": string(templateLoadResult.Outcome), "reason_code": templateLoadResult.ReasonCode,
					})
				} else {
					// 优化：按资产分组，每组只加载相关的POC模板
					// 当AutoScan或AutomaticScan启用时，按资产的指纹标签进行分组
					var groups []*AssetGroup
					if config.PocScan.AutoScan || config.PocScan.AutomaticScan {
						groups = w.groupAssetsByTags(pocAssets, config.PocScan)
					}

					if len(groups) > 0 {

						// 用于统计漏洞数量
						var vulCount int

						// 创建漏洞缓冲区，发现漏洞立即保存
						vulBuffer := NewVulnerabilityBuffer(1)

						// 获取单目标超时配置
						targetTimeout := config.PocScan.TargetTimeout
						if targetTimeout <= 0 {
							targetTimeout = 600 // 默认600秒
						}

						// 无阶段总预算：单目标超时由每个 nuclei 进程的 TargetTimeout 硬边界保证，
						// 阶段 context 仅作取消信号，避免多组串行扫描被整体 deadline 截断后剩余分组全部瞬间失败
						pocCtx, pocCancel := context.WithCancel(ctx)

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
						severities := []string{}
						if config.PocScan.Severity != "" {
							severities = strings.Split(config.PocScan.Severity, ",")
						}

						baseOptions := &scanner.NucleiOptions{
							Severity:      config.PocScan.Severity,
							ExcludeTags:   config.PocScan.ExcludeTags,
							TargetTimeout: targetTimeout,
							CustomHeaders: config.PocScan.CustomHeaders,
							ForceScan:     config.PocScan.ForceScan,
							OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
								vulCount++
								logVulnerability(vul)
								vulBuffer.Add(vul)
							},
							OnProgress: w.makeOnProgress(task, "POC扫描"),
						}
						pocTaskLogger := func(level, format string, args ...interface{}) {
							w.taskLog(task.TaskId, level, format, args...)
						}
						coverage := executePocGroupsWithEvents(
							pocCtx,
							groups,
							severities,
							baseOptions,
							w.resolveTemplatesByTagsResult,
							s,
							pocTaskLogger,
							w.scannerEventLogger(task.TaskId),
						)
						pocPhaseResult = PhaseResult{
							Phase: "poc", Status: coverage.Status,
							Coverage:                scanner.Coverage{Input: coverage.TotalAssets, Attempted: coverage.ScannedAssets, Succeeded: coverage.ScannedAssets, Failed: coverage.FailedGroups, Uncovered: coverage.UncoveredAssets},
							UsableResults:           coverage.ScannedAssets > 0 || len(coverage.VulnerabilityResults) > 0,
							Vulnerabilities:         coverage.Vulnerabilities,
							VulnerabilityConclusion: string(coverage.VulnerabilityConclusion),
						}
						allVuls = append(allVuls, coverage.VulnerabilityResults...)

						pocCancel()

						// 扫描完成后，刷新剩余的漏洞
						vulBuffer.Flush(ctx, func(vuls []*scanner.Vulnerability) {
							w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
						})

					} else {
						w.taskLog(task.TaskId, LevelWarn, "No POC templates configured (no tags matched), skipping POC scan")
					}
				}

				// 统一扫描执行：当模板通过 ID 或 CustomPocOnly 方式加载后，执行扫描
				if len(templates) > 0 || len(templateRefs) > 0 {
					// 用于统计漏洞数量
					var vulCount int

					// 创建漏洞缓冲区，发现漏洞立即保存
					vulBuffer := NewVulnerabilityBuffer(1)

					// 获取单目标超时配置
					pocTargetTimeout := config.PocScan.TargetTimeout
					if pocTargetTimeout <= 0 {
						pocTargetTimeout = 600
					}

					// 无阶段总预算：单目标超时由每个 nuclei 进程的 TargetTimeout 硬边界保证，
					// 阶段 context 仅作取消信号，避免批量目标被整体 deadline 截断后剩余目标全部瞬间失败
					pocCtx, pocCancel := context.WithCancel(ctx)

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

					// 并发和速率由自适应调度器决定
					nucleiOpts := &scanner.NucleiOptions{
						Severity:         config.PocScan.Severity,
						ExcludeTags:      config.PocScan.ExcludeTags,
						TargetTimeout:    pocTargetTimeout,
						AutoScan:         false,
						AutomaticScan:    false,
						CustomPocOnly:    config.PocScan.CustomPocOnly,
						CustomTemplates:  templates,
						TemplateFileRefs: templateRefs,
						TagMappings:      config.PocScan.TagMappings,
						CustomHeaders:    config.PocScan.CustomHeaders,
						ForceScan:        config.PocScan.ForceScan,
						OnVulnerabilityFound: func(vul *scanner.Vulnerability) {
							vulCount++
							logVulnerability(vul)
							vulBuffer.Add(vul)
						},
						OnProgress: w.makeOnProgress(task, "POC扫描"),
					}

					pocTaskLogger := func(level, format string, args ...interface{}) {
						w.taskLog(task.TaskId, level, format, args...)
					}

					result, err := s.Scan(pocCtx, &scanner.ScanConfig{
						Assets:      pocAssets,
						Options:     nucleiOpts,
						TaskLogger:  pocTaskLogger,
						EventLogger: w.scannerEventLogger(task.TaskId),
					})

					if err != nil {
						w.taskLog(task.TaskId, LevelError, "POC scan error: %v", err)
					}
					if result != nil {
						allVuls = append(allVuls, result.Vulnerabilities...)
						if result.Diagnostic != nil {
							pocPhaseResult = PhaseResult{
								Phase: "poc", Status: result.Diagnostic.Status, Coverage: result.Diagnostic.Coverage,
								ReasonCodes:     append([]string(nil), result.Diagnostic.WarningCodes...),
								UsableResults:   result.Diagnostic.Coverage.Succeeded > 0 || len(result.Vulnerabilities) > 0,
								Vulnerabilities: len(result.Vulnerabilities),
							}
						}
					}
					if result == nil || result.Diagnostic == nil {
						coverage := scanner.Coverage{Input: len(pocAssets), Attempted: len(pocAssets)}
						if err == nil {
							coverage.Succeeded = len(pocAssets)
						} else {
							coverage.Failed = len(pocAssets)
						}
						pocPhaseResult = NewPhaseResult("poc", coverage, false)
						pocPhaseResult.UsableResults = coverage.Succeeded > 0 || vulCount > 0
						pocPhaseResult.Vulnerabilities = vulCount
					}

					pocCancel()

					// 扫描完成后，刷新剩余的漏洞
					vulBuffer.Flush(ctx, func(vuls []*scanner.Vulnerability) {
						w.saveVulResultWithFallback(ctx, task.MainTaskId, vuls)
					})
				}
			}
			if explicitTemplateSelection {
				pocPhaseResult = applyExplicitPocTemplateCoverage(pocPhaseResult, templateLoadResult, templateLoadErr, len(pocAssets))
			} else if pocPhaseResult.Status == scanner.PhaseSkippedNotApplicable && len(pocAssets) > 0 {
				pocPhaseResult = NewPhaseResult("poc", scanner.Coverage{Input: len(pocAssets), Uncovered: len(pocAssets)}, false, scanner.ReasonZeroCoverage)
				pocPhaseResult.VulnerabilityConclusion = model.VulnerabilityConclusionNotEvaluated
			}
			w.taskLog(task.TaskId, LevelInfo, "漏洞扫描完成：状态 %s，扫描资产 %d，漏洞 %d",
				pocPhaseResult.Status, len(pocAssets), pocPhaseResult.Vulnerabilities)
			reportPhase(ctx, "漏洞扫描", false, 1, pocPhaseResult)

		} // 结束 len(allAssets) > 0 的 else 分支
	}

	// 更新任务状态为完成
	duration := time.Since(startTime).Seconds()
	result := fmt.Sprintf("Assets:%d Vuls:%d Duration:%.0fs", len(allAssets), len(allVuls), duration)

	// 每个启用阶段必须先有自己的语义报告；报告失败时保留其身份和失败状态。
	reportMissingPhases(ctx, false, false)
	finalPhase := NewPhaseResult("complete", scanner.Coverage{Input: 1, Attempted: 1, Succeeded: 1}, false)
	finalPhase.UsableResults = len(allAssets) > 0 || len(allVuls) > 0
	finalPhase.Assets = len(allAssets)
	finalPhase.Vulnerabilities = len(allVuls)
	finalPhase.ResultPrefix = result
	ack := reportPhase(ctx, "完成", true, 1, finalPhase)
	if ack.Recorded {
		finalPayloadAccepted = true
		switch {
		case ack.LeaseClosed:
			w.taskLog(task.TaskId, LevelInfo, "任务完成：资产 %d，漏洞 %d，用时 %.0f 秒", len(allAssets), len(allVuls), duration)
		case ack.FinalizationPending:
			w.taskLog(task.TaskId, LevelInfo, "任务最终结果已接受，终态仍在协调：%s", result)
		default:
			w.taskLog(task.TaskId, LevelInfo, "任务最终结果已接受，租约清理仍在协调：%s", result)
		}
	} else {
		w.taskLog(task.TaskId, LevelError, "Failed to record canonical task completion payload: %s", result)
	}
	// 终态由 IncrSubTaskDone 的语义聚合统一落库，避免无条件 SUCCESS 覆盖 PARTIAL/FAILURE。
	// 注意：taskExecuted 由 defer 递增，无需在此处理
}

const taskOperationBusyRetryAttempts = 7

func retryTaskOperationBusy(ctx context.Context, update func() error) error {
	delay := 250 * time.Millisecond
	for attempt := 0; attempt < taskOperationBusyRetryAttempts; attempt++ {
		err := update()
		if !errors.Is(err, scheduler.ErrTaskOperationBusy) || attempt == taskOperationBusyRetryAttempts-1 {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
	return nil
}

func closesWorkerTaskLease(status string) bool {
	switch status {
	case scheduler.TaskStatusSuccess, scheduler.TaskStatusFailure, scheduler.TaskStatusStopped,
		model.TaskStatusPartial, model.TaskStatusRevoked, model.TaskStatusLegacyCompleted:
		return true
	default:
		return false
	}
}

// updateTaskStatus 更新任务状态
func (w *Worker) updateTaskStatus(ctx context.Context, task *scheduler.TaskInfo, status string, result string) error {
	if task == nil {
		return nil
	}
	if w.taskLeaseClosed(task) {
		return nil
	}
	if w.taskLeaseLost(task) {
		return scheduler.ErrTaskLeaseConflict
	}
	taskId := task.TaskId
	err := retryTaskOperationBusy(ctx, func() error {
		if w.schedClient != nil {
			phase := ""
			progress := 0
			if status == scheduler.TaskStatusSuccess || status == scheduler.TaskStatusFailure {
				phase = "完成"
				progress = 100
			}
			return w.schedClient.UpdateTask(ctx, taskId, task.LeaseToken, status, progress, phase, "")
		}
		if w.httpClient != nil {
			req := &TaskUpdateReq{
				TaskId:     taskId,
				LeaseToken: task.LeaseToken,
				State:      status,
				Worker:     w.workerName,
				Result:     result,
			}
			if status == scheduler.TaskStatusSuccess || status == scheduler.TaskStatusFailure {
				req.Progress = 100
				req.Phase = "完成"
			}
			_, updateErr := w.httpClient.UpdateTask(ctx, req)
			return updateErr
		}
		return fmt.Errorf("no task update client configured")
	})
	if err != nil {
		w.taskLog(taskId, LevelError, "update task status failed: %v", err)
		return w.handleTaskLeaseError(task, err)
	}
	if closesWorkerTaskLease(status) {
		w.markTaskLeaseClosed(task)
	}
	return nil
}

// updateTaskProgress 更新任务进度
func (w *Worker) updateTaskProgress(ctx context.Context, task *scheduler.TaskInfo, progress int, message string) error {
	return w.updateTaskProgressWithPhase(ctx, task, progress, message, "")
}

// updateTaskProgressWithPhase 更新任务进度和当前阶段
func (w *Worker) updateTaskProgressWithPhase(ctx context.Context, task *scheduler.TaskInfo, progress int, message string, currentPhase string) error {
	if task == nil || currentPhase == "" {
		return nil
	}
	if w.taskLeaseClosed(task) {
		return nil
	}
	if w.taskLeaseLost(task) {
		return scheduler.ErrTaskLeaseConflict
	}
	taskId := task.TaskId
	err := retryTaskOperationBusy(ctx, func() error {
		if w.schedClient != nil {
			return w.schedClient.UpdateTask(ctx, taskId, task.LeaseToken, "", progress, currentPhase, "")
		}
		if w.httpClient != nil {
			_, updateErr := w.httpClient.UpdateTask(ctx, &TaskUpdateReq{
				TaskId:     taskId,
				LeaseToken: task.LeaseToken,
				Progress:   progress,
				Phase:      currentPhase,
				Result:     message,
			})
			return updateErr
		}
		return fmt.Errorf("no task update client configured")
	})
	if err != nil {
		w.taskLog(taskId, LevelError, "update task progress failed: %v", err)
	}
	return w.handleTaskLeaseError(task, err)
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
// 每完成一个扫描模块就调用此方法，通知主任务进度更新。
// 只有服务端确认已记录（包括幂等重试成功）时才返回 Recorded=true；
// Finalized=false/FinalizationPending=true 表示阶段已落库但终态转换仍需重试。
func (w *Worker) incrSubTaskDone(ctx context.Context, task *scheduler.TaskInfo, phase string, isCompleted bool, incrAmount int, phaseResults ...PhaseResult) (ack phaseReportAck) {
	defer func() {
		if r := recover(); r != nil {
			if w.logger != nil {
				w.logger.Error("Increment subtask done panic recovered: %v, stack: %s", r, string(getStackTrace()))
			}
			ack = phaseReportAck{}
		}
	}()

	if task == nil || w.taskLeaseLost(task) || w.taskLeaseClosed(task) {
		return phaseReportAck{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if incrAmount <= 0 {
		incrAmount = 1
	}

	var phaseResult *PhaseResult
	if len(phaseResults) > 0 {
		phaseResult = &phaseResults[0]
	}
	if phaseResult == nil {
		defaultResult := missingPhaseResult(phase)
		phaseResult = &defaultResult
	}
	w.taskLogEvent(task.TaskId, LevelInfo,
		"阶段结果已记录",
		EventPhaseComplete, phaseResult.Phase, string(phaseResult.Status), phaseEventFields(*phaseResult))

	const maxAttempts = 3
	waitForRetry := func(attempt int) bool {
		if attempt+1 >= maxAttempts {
			return false
		}
		delay := time.Duration(1<<attempt) * 250 * time.Millisecond
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		}
	}

	finalizationPending := func(recorded, leaseClosed, finalized, pending bool, summary *model.TaskScanSummary, allDone bool) bool {
		if !recorded || !isCompleted || leaseClosed || finalized {
			return false
		}
		// New servers set the explicit flag. The summary-nil fallback keeps the
		// retry behavior safe until the final child lease is durably closed.
		return pending || (allDone && summary == nil)
	}
	mergeAck := func(current, next phaseReportAck) phaseReportAck {
		current.Recorded = current.Recorded || next.Recorded
		current.LeaseClosed = current.LeaseClosed || next.LeaseClosed
		current.Finalized = current.Finalized || next.Finalized
		current.FinalizationPending = (current.FinalizationPending || next.FinalizationPending) &&
			!current.LeaseClosed && !current.Finalized
		return current
	}

	if w.schedClient != nil {
		var latest *IncrSubTaskDoneResponse
		var best phaseReportAck
		var lastErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			resp, callErr := w.schedClient.IncrSubTaskDone(
				ctx, task.MainTaskId, task.TaskId, task.LeaseToken, phase, isCompleted, incrAmount, phaseResult,
			)
			if errors.Is(callErr, scheduler.ErrTaskLeaseConflict) || errors.Is(callErr, scheduler.ErrTaskParentFenced) {
				w.handleTaskLeaseError(task, callErr)
				lastErr = callErr
				break
			}
			if callErr == nil && resp != nil {
				latest = resp
				attemptAck := phaseReportAck{
					Recorded:    resp.Recorded,
					LeaseClosed: resp.LeaseClosed,
					Finalized:   resp.Finalized,
					FinalizationPending: finalizationPending(
						resp.Recorded, resp.LeaseClosed, resp.Finalized, resp.FinalizationPending,
						resp.ScanSummary, resp.AllDone,
					),
				}
				best = mergeAck(best, attemptAck)
				lastErr = nil
				if attemptAck.FinalizationPending && waitForRetry(attempt) {
					continue
				}
				break
			}
			if callErr == nil {
				callErr = fmt.Errorf("scheduler returned nil subtask response")
			}
			lastErr = callErr
			if !waitForRetry(attempt) {
				break
			}
		}
		if latest == nil {
			w.taskLog(task.TaskId, LevelError, "Failed to record subtask result after retries: %v", lastErr)
			return best
		}
		if lastErr != nil {
			w.taskLog(task.TaskId, LevelWarn,
				"Subtask result was accepted before a follow-up retry failed: %v", lastErr)
		}
		w.setMainTaskProgressBaseline(task.MainTaskId, latest.SubTaskDone, latest.SubTaskCount)
		if best.Finalized && latest.ScanSummary != nil {
			w.taskLogEvent(task.TaskId, LevelInfo,
				"任务结果已记录",
				EventTaskFinalized, "task", latest.ScanSummary.Outcome, taskFinalizedEventFields(latest.ScanSummary))
		}
		if best.LeaseClosed {
			w.markTaskLeaseClosed(task)
		}
		return best
	}

	// 回退到 HTTP
	if w.httpClient == nil {
		w.taskLog(task.TaskId, LevelError, "Failed to incr sub task done: no scheduler or HTTP client")
		return phaseReportAck{}
	}

	phaseSummary := phaseResult.TaskSummary(task.TaskId)
	taskSummary := &model.TaskScanSummary{
		VulnerabilityConclusion: phaseSummary.VulnerabilityConclusion,
		Phases: map[string]model.TaskPhaseSummary{
			model.TaskPhaseReportKey(task.TaskId, phaseSummary.Phase): phaseSummary,
		},
		PhaseCount:      incrAmount,
		Assets:          phaseSummary.Assets,
		Vulnerabilities: phaseSummary.Vulnerabilities,
		WarningCodes:    append([]string(nil), phaseSummary.ReasonCodes...),
	}
	req := &SubTaskDoneReq{
		TaskId:      task.TaskId,
		MainTaskId:  task.MainTaskId,
		LeaseToken:  task.LeaseToken,
		Phase:       phase,
		IsCompleted: isCompleted,
		IncrAmount:  incrAmount,
		PhaseResult: &phaseSummary,
		TaskSummary: taskSummary,
	}
	var latest *SubTaskDoneResp
	var best phaseReportAck
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, callErr := w.httpClient.IncrSubTaskDone(ctx, req)
		if errors.Is(callErr, scheduler.ErrTaskLeaseConflict) || errors.Is(callErr, scheduler.ErrTaskParentFenced) {
			w.handleTaskLeaseError(task, callErr)
			lastErr = callErr
			break
		}
		if callErr == nil && resp != nil && resp.Success {
			latest = resp
			attemptAck := phaseReportAck{
				Recorded:    resp.Recorded,
				LeaseClosed: resp.LeaseClosed,
				Finalized:   resp.Finalized,
				FinalizationPending: finalizationPending(
					resp.Recorded, resp.LeaseClosed, resp.Finalized, resp.FinalizationPending,
					resp.ScanSummary, resp.AllDone,
				),
			}
			best = mergeAck(best, attemptAck)
			lastErr = nil
			if attemptAck.FinalizationPending && waitForRetry(attempt) {
				continue
			}
			break
		}
		if callErr == nil {
			if resp == nil {
				callErr = fmt.Errorf("HTTP scheduler returned nil subtask response")
			} else {
				callErr = fmt.Errorf("HTTP scheduler rejected subtask report: code=%d msg=%s", resp.Code, resp.Msg)
			}
		}
		lastErr = callErr
		if !waitForRetry(attempt) {
			break
		}
	}
	if latest == nil {
		w.taskLog(task.TaskId, LevelError, "Failed to record HTTP subtask result after retries: %v", lastErr)
		return best
	}
	if lastErr != nil {
		w.taskLog(task.TaskId, LevelWarn,
			"HTTP subtask result was accepted before a follow-up retry failed: %v", lastErr)
	}

	w.setMainTaskProgressBaseline(task.MainTaskId, int(latest.SubTaskDone), int(latest.SubTaskCount))
	if best.Finalized && latest.ScanSummary != nil {
		w.taskLogEvent(task.TaskId, LevelInfo,
			"任务结果已记录",
			EventTaskFinalized, "task", latest.ScanSummary.Outcome, taskFinalizedEventFields(latest.ScanSummary))
	}
	if best.LeaseClosed {
		w.markTaskLeaseClosed(task)
	}
	return best
}

func (w *Worker) mainTaskProgressState(mainTaskID string) *mainTaskProgressState {
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	state := w.progressByMain[mainTaskID]
	if state == nil {
		state = &mainTaskProgressState{}
		w.progressByMain[mainTaskID] = state
	}
	return state
}

func (w *Worker) setMainTaskProgressBaseline(mainTaskID string, done, total int) {
	if mainTaskID == "" || total <= 0 {
		return
	}
	state := w.mainTaskProgressState(mainTaskID)
	state.mu.Lock()
	state.done = done
	state.total = total
	baseline := calculateProgress(done, total)
	if baseline > state.lastReported {
		state.lastReported = baseline
	}
	state.mu.Unlock()
}

// updateMainTaskProgress reports one aggregate parent percentage through the
// guarded task-update transport. The API/direct adapter holds the exact lease
// barrier across the Mongo mutation, so no second unfenced database write is
// performed here.
func (w *Worker) updateMainTaskProgress(task *scheduler.TaskInfo, moduleFraction float64, phase, message string) {
	if task == nil || task.TaskId == "" || w.taskLeaseLost(task) || w.taskLeaseClosed(task) {
		return
	}
	if moduleFraction < 0 {
		moduleFraction = 0
	}
	if moduleFraction > 1 {
		moduleFraction = 1
	}

	mainTaskID := task.MainTaskId
	state := w.mainTaskProgressState(mainTaskID)
	state.mu.Lock()
	defer state.mu.Unlock()

	progress := int(moduleFraction * 100)
	if state.total > 0 {
		progress = int((float64(state.done) + moduleFraction) * 100.0 / float64(state.total))
		if progress > 99 {
			progress = 99
		}
	}
	if progress < 0 {
		progress = 0
	}
	if progress <= state.lastReported || w.taskLeaseLost(task) || w.taskLeaseClosed(task) {
		return
	}
	if err := w.updateTaskProgressWithPhase(context.Background(), task, progress, message, phase); err != nil {
		return
	}
	state.lastReported = progress
}

// makeOnProgress captures the exact child TaskInfo so every module callback
// refreshes that child's lease rather than writing the parent with an empty token.
func (w *Worker) makeOnProgress(task *scheduler.TaskInfo, phase string) func(int, string) {
	return func(modulePercent int, message string) {
		if modulePercent < 0 {
			modulePercent = 0
		}
		if modulePercent > 100 {
			modulePercent = 100
		}
		msg := message
		if msg == "" {
			msg = phase
		}
		w.updateMainTaskProgress(task, float64(modulePercent)/100.0, phase, msg)
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
