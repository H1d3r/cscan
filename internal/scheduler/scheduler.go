package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/core/logx"
)

// 任务状态常量
const (
	TaskStatusCreated = "CREATED"
	TaskStatusPending = "PENDING"
	TaskStatusStarted = "STARTED"
	TaskStatusSuccess = "SUCCESS"
	TaskStatusPartial = "PARTIAL"
	TaskStatusFailure = "FAILURE"
	TaskStatusRevoked = "REVOKED"
	TaskStatusStopped = "STOPPED"
)

// PriorityQueueMetrics 优先级队列性能指标
type PriorityQueueMetrics struct {
	PushCount      int64     // 推送任务总数
	PopCount       int64     // 弹出任务总数
	PushLatencySum int64     // 推送延迟总和（纳秒）
	PopLatencySum  int64     // 弹出延迟总和（纳秒）
	LastPushTime   time.Time // 最后推送时间
	LastPopTime    time.Time // 最后弹出时间
	mu             sync.RWMutex
}

// RecordPush 记录推送操作
func (m *PriorityQueueMetrics) RecordPush(latency time.Duration) {
	atomic.AddInt64(&m.PushCount, 1)
	atomic.AddInt64(&m.PushLatencySum, int64(latency))
	m.mu.Lock()
	m.LastPushTime = time.Now()
	m.mu.Unlock()
}

// RecordPop 记录弹出操作
func (m *PriorityQueueMetrics) RecordPop(latency time.Duration) {
	atomic.AddInt64(&m.PopCount, 1)
	atomic.AddInt64(&m.PopLatencySum, int64(latency))
	m.mu.Lock()
	m.LastPopTime = time.Now()
	m.mu.Unlock()
}

// GetStats 获取统计信息
func (m *PriorityQueueMetrics) GetStats() map[string]interface{} {
	pushCount := atomic.LoadInt64(&m.PushCount)
	popCount := atomic.LoadInt64(&m.PopCount)
	pushLatencySum := atomic.LoadInt64(&m.PushLatencySum)
	popLatencySum := atomic.LoadInt64(&m.PopLatencySum)

	m.mu.RLock()
	lastPush := m.LastPushTime
	lastPop := m.LastPopTime
	m.mu.RUnlock()

	var avgPushLatency, avgPopLatency float64
	if pushCount > 0 {
		avgPushLatency = float64(pushLatencySum) / float64(pushCount) / float64(time.Millisecond)
	}
	if popCount > 0 {
		avgPopLatency = float64(popLatencySum) / float64(popCount) / float64(time.Millisecond)
	}

	return map[string]interface{}{
		"pushCount":        pushCount,
		"popCount":         popCount,
		"avgPushLatencyMs": avgPushLatency,
		"avgPopLatencyMs":  avgPopLatency,
		"lastPushTime":     lastPush,
		"lastPopTime":      lastPop,
	}
}

// TaskInfo 任务信息
type TaskInfo struct {
	TaskId             string   `json:"taskId"`
	MainTaskId         string   `json:"mainTaskId"`
	TaskName           string   `json:"taskName"`
	Config             string   `json:"config"`
	Priority           int      `json:"priority"`
	CreateTime         string   `json:"createTime"`
	Workers            []string `json:"workers,omitempty"` // 指定执行任务的 Worker 列表，为空表示任意 Worker
	DispatchGeneration string   `json:"dispatchGeneration,omitempty"`
	LeaseToken         string   `json:"-"` // 单次 pop 所有权令牌，不得写回队列 payload
	RecoveryInstanceID string   `json:"-"` // recovery-only owner guard; never serialized into queue payloads
}

// WorkerLoad Worker负载信息
type WorkerLoad struct {
	WorkerName     string    `json:"workerName"`
	CurrentTasks   int       `json:"currentTasks"`
	MaxConcurrency int       `json:"maxConcurrency"`
	CPUPercent     float64   `json:"cpuPercent"`
	MemPercent     float64   `json:"memPercent"`
	LastHeartbeat  time.Time `json:"lastHeartbeat"`
}

// LoadScore 计算负载分数（越低越好）
func (w *WorkerLoad) LoadScore() float64 {
	if w.MaxConcurrency == 0 {
		return 100.0
	}
	// 综合考虑任务负载、CPU和内存
	taskLoad := float64(w.CurrentTasks) / float64(w.MaxConcurrency) * 100
	return taskLoad*0.5 + w.CPUPercent*0.3 + w.MemPercent*0.2
}

// IsAvailable 检查Worker是否可用
func (w *WorkerLoad) IsAvailable() bool {
	// 心跳超过30秒认为不可用
	if time.Since(w.LastHeartbeat) > 30*time.Second {
		return false
	}
	// 任务已满
	if w.CurrentTasks >= w.MaxConcurrency {
		return false
	}
	// CPU或内存过高
	if w.CPUPercent > 90 || w.MemPercent > 90 {
		return false
	}
	return true
}

// Scheduler 任务调度器
type Scheduler struct {
	rdb                  *redis.Client
	cron                 *cron.Cron
	queueKey             string
	processingKey        string
	workerLoadKey        string // Worker负载信息Key
	mu                   sync.Mutex
	handlers             map[string]TaskHandler
	metrics              *PriorityQueueMetrics // 性能指标
	enablePriorityBucket atomic.Bool           // 优先级分桶开关（默认 false，兼容原单 ZSet 行为）
}

// TaskHandler 任务处理函数
type TaskHandler func(ctx context.Context, task *TaskInfo) error

// NewScheduler 创建调度器
func NewScheduler(rdb *redis.Client) *Scheduler {
	return &Scheduler{
		rdb:           rdb,
		cron:          cron.New(cron.WithSeconds()),
		queueKey:      "cscan:task:queue",
		processingKey: "cscan:task:processing",
		workerLoadKey: "cscan:worker:load",
		handlers:      make(map[string]TaskHandler),
		metrics:       &PriorityQueueMetrics{},
		// enablePriorityBucket 默认 false，保持向后兼容；开启后走 5 级分桶路径
	}
}

// SetEnablePriorityBucket 启用/禁用优先级分桶
// 开启后 PushTask 推送到 5 个分桶 ZSet，PopTaskForWorker 跨分桶原子弹出
// 关闭时保持原 cscan:task:queue 单 ZSet 行为
// 用于灰度发布：先在测试环境启用，验证无问题后推到生产
func (s *Scheduler) SetEnablePriorityBucket(enable bool) {
	s.enablePriorityBucket.Store(enable)
}

// IsPriorityBucketEnabled 返回分桶开关状态
func (s *Scheduler) IsPriorityBucketEnabled() bool {
	return s.enablePriorityBucket.Load()
}

// BucketKey 返回指定优先级对应的分桶 Redis Key
// 用于跨模块（如 task_recovery）在分桶模式下路由任务到正确的桶
// 注意：调用方应先通过 IsPriorityBucketEnabled 确认分桶已启用
func (s *Scheduler) BucketKey(priority int) string {
	return priorityBucketKey(priority)
}

// RegisterHandler 注册任务处理器
func (s *Scheduler) RegisterHandler(taskName string, handler TaskHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[taskName] = handler
}

// Start 启动调度器
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// AddCronTask 添加定时任务
func (s *Scheduler) AddCronTask(spec string, taskFunc func()) (cron.EntryID, error) {
	return s.cron.AddFunc(spec, taskFunc)
}

// RemoveCronTask 移除定时任务
func (s *Scheduler) RemoveCronTask(id cron.EntryID) {
	s.cron.Remove(id)
}

// GetWorkerQueueKey 获取 Worker 专属队列的 Key
func (s *Scheduler) GetWorkerQueueKey(workerName string) string {
	return fmt.Sprintf("cscan:task:queue:worker:%s", strings.ToLower(workerName))
}

// GetMetrics 获取性能指标
func (s *Scheduler) GetMetrics() *PriorityQueueMetrics {
	return s.metrics
}

// calculatePriorityScore 计算优先级分数
// 分数越小优先级越高，确保高优先级任务先被处理
// Priority值越大，优先级越高（减去更多）
//
// 历史问题：使用 time.Now().Unix()（秒级），同秒入队的任务顺序依赖 i*0.001 微调
// 改进：使用 UnixMicro（微秒级），从根本上解决同秒顺序问题
// 同时将 priority 调整单位从 1000 秒提升到 1_000_000 微秒，保持相对量级一致
func (s *Scheduler) calculatePriorityScore(priority int, createTime time.Time) float64 {
	// 基础分数为创建时间戳（微秒级，解决同秒顺序问题）
	baseScore := float64(createTime.UnixMicro())
	// 优先级调整：每个优先级单位减少 1_000_000 微秒（= 1 秒）
	// 历史值是 1000 秒，改为 1 秒是因为微秒级时间戳本身已足够区分同秒任务
	// 优先级的作用是"让高优先级任务插队"，1 秒的调整已足够覆盖正常入队间隔
	// 若保持 1000 秒调整，会导致低优先级任务长期饥饿
	priorityAdjustment := float64(priority) * 1_000_000
	return baseScore - priorityAdjustment
}

// ==================== 优先级分桶队列 ====================
//
// 5 级优先级分桶设计（Phase 2 优化）：
//   Urgent(4)     -> cscan:task:queue:p4  （最高优先级，最先弹出）
//   High(3)       -> cscan:task:queue:p3
//   Normal(2)     -> cscan:task:queue:p2
//   Low(1)        -> cscan:task:queue:p1
//   Background(0) -> cscan:task:queue:p0  （最低优先级，最后弹出）
//
// 语义一致性：
//   - priority 数值越大，优先级越高（与 worker.TaskPriority 4=Urgent 一致）
//   - 桶索引 = priority 数值（p4 是 Urgent 桶）
//   - 弹出顺序：从 p4（Urgent）到 p0（Background）倒序检查
//   - calculatePriorityScore 公式 base - priority*1M 保持不变，priority 越大减越多，分数越小越先弹出
//   - 单 ZSet 模式下同一公式也正确（urgent 分数最小，先被 ZPopMin 弹出）
//
// 与现有 worker/task_queue_manager.go 的映射（完全兼容，数值一致）：
//   worker.Urgent(4)  -> scheduler.PriorityUrgent(4)    -> 桶 p4
//   worker.High(3)    -> scheduler.PriorityHigh(3)      -> 桶 p3
//   worker.Normal(2)  -> scheduler.PriorityNormal(2)    -> 桶 p2
//   worker.Low(1)     -> scheduler.PriorityLow(1)       -> 桶 p1
//   新增 Background(0) -> scheduler.PriorityBackground(0) -> 桶 p0
//
// 兼容开关：enablePriorityBucket（默认 false，保持原 cscan:task:queue 单 ZSet 行为）
// 开启后 PushTask/PopTaskForWorker 自动走分桶路径

// 优先级常量（数值越大优先级越高，与 worker.TaskPriority 一致）
const (
	PriorityBackground = 0
	PriorityLow        = 1
	PriorityNormal     = 2
	PriorityHigh       = 3
	PriorityUrgent     = 4
)

// priorityBucketKey 获取优先级分桶的 Redis Key
// 仅在 enablePriorityBucket=true 时使用
// 桶索引 = priority 数值，p4=Urgent 先弹出，p0=Background 最后弹出
func priorityBucketKey(priority int) string {
	p := priority
	if p < PriorityBackground {
		p = PriorityBackground
	}
	if p > PriorityUrgent {
		p = PriorityUrgent
	}
	return fmt.Sprintf("cscan:task:queue:p%d", p)
}

// bucketPriorityFromWorker 将 worker.TaskPriority 映射为 scheduler 分桶优先级
// worker 包使用 1-4（Low-Urgent），scheduler 使用相同的 1-4 数值（语义一致）
// 此函数作为兼容层，worker 包可独立调整
func BucketPriorityFromWorker(workerPriority int) int {
	switch workerPriority {
	case 4: // worker.Urgent
		return PriorityUrgent
	case 3: // worker.High
		return PriorityHigh
	case 2: // worker.Normal
		return PriorityNormal
	case 1: // worker.Low
		return PriorityLow
	default:
		if workerPriority <= 0 {
			return PriorityBackground
		}
		return PriorityNormal
	}
}

// pushToPriorityBucket 推送到指定优先级分桶
func (s *Scheduler) pushToPriorityBucket(ctx context.Context, task *TaskInfo, score float64) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	bucketKey := priorityBucketKey(task.Priority)
	return s.rdb.ZAdd(ctx, bucketKey, redis.Z{
		Score:  score,
		Member: string(data),
	}).Err()
}

// popFromPriorityBuckets is retained for package compatibility and delegates
// to the single leased acquisition primitive.
func (s *Scheduler) popFromPriorityBuckets(ctx context.Context) (*TaskInfo, error) {
	return s.popLeasedTask(ctx, "", "", 0, false)
}

// publishDeadLetterAlert 在 pop 脚本外执行死信告警 PUBLISH。
// 放在 Lua 外可避免订阅消费慢或缓冲满时阻塞整个 pop 事务。
// PUBLISH 失败仅记录日志,不影响后续 pop。
func (s *Scheduler) publishDeadLetterAlert(ctx context.Context, member string) {
	if err := s.rdb.Publish(ctx, "cscan:task:deadletter:alert", member).Err(); err != nil {
		logx.Errorf("[Scheduler] publish deadletter alert failed: %v", err)
	}
}

// PushTask 推送任务到队列
// 如果任务指定了 Workers，则推送到每个 Worker 的专属队列
// 否则推送到公共队列
//
// 当 enablePriorityBucket=true 时，公共队列走 5 级分桶路径
// 当 enablePriorityBucket=false 时，保持原 cscan:task:queue 单 ZSet 行为
//
// 推送成功后通过 Pub/Sub 通知空闲 Worker，实现长轮询唤醒（减少空轮询）
func (s *Scheduler) PushTask(ctx context.Context, task *TaskInfo) error {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPush(time.Since(startTime))
	}()
	return s.publishTaskBatch(ctx, []*TaskInfo{task}, "", true)
}

// notifyTaskAvailable 通过 Pub/Sub 通知空闲 Worker 有新任务可用
// 非阻塞：PUBLISH 失败仅记录 Debug 日志，不影响任务入队
func (s *Scheduler) notifyTaskAvailable(ctx context.Context) {
	// PUBLISH 到公共通道 + 一个通用唤醒信号（N个Subscriber中只需要一个被唤醒）
	// 使用 "1" 作为消息体，内容无意义仅作为唤醒信号
	if err := s.rdb.Publish(ctx, "cscan:task:available", "1").Err(); err != nil {
		// Pub/Sub 失败不影响主流程，仅 Debug 级别记录
		logx.Debugf("[Scheduler] publish task available notification failed: %v", err)
	}
}

func (s *Scheduler) PushTaskBatch(ctx context.Context, tasks []*TaskInfo) error {
	if len(tasks) == 0 {
		return nil
	}

	startTime := time.Now()
	defer func() {
		avgLatency := time.Since(startTime) / time.Duration(len(tasks))
		for range tasks {
			s.metrics.RecordPush(avgLatency)
		}
	}()
	return s.publishTaskBatch(ctx, tasks, "", true)
}

func (s *Scheduler) PopTask(ctx context.Context) (*TaskInfo, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPop(time.Since(startTime))
	}()
	return s.popLeasedTask(ctx, "", "", 0, false)
}

// PopTaskForWorker is the legacy worker-name-only acquisition wrapper. New
// workers must use PopTaskForWorkerInstance so recovery can fence process
// generations with per-instance heartbeats.
func (s *Scheduler) PopTaskForWorker(ctx context.Context, workerName string) (*TaskInfo, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPop(time.Since(startTime))
	}()
	return s.popLeasedTask(ctx, workerName, "", 0, true)
}

// PopTaskForWorkerInstance acquires a leased-task-v1 execution for one
// immutable worker process instance.
func (s *Scheduler) PopTaskForWorkerInstance(ctx context.Context, workerName, instanceID string) (*TaskInfo, error) {
	if strings.TrimSpace(workerName) == "" {
		return nil, fmt.Errorf("worker name is required")
	}
	if strings.TrimSpace(instanceID) == "" {
		return nil, fmt.Errorf("worker instance id is required")
	}
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPop(time.Since(startTime))
	}()
	return s.popLeasedTask(ctx, workerName, instanceID, TaskProtocolV1, true)
}

// popForWorkerFromBuckets is retained for package compatibility and delegates
// to the legacy acquisition primitive.
func (s *Scheduler) popForWorkerFromBuckets(ctx context.Context, _ string, workerName string) (*TaskInfo, error) {
	return s.popLeasedTask(ctx, workerName, "", 0, true)
}

// PeekTask 查看队列中优先级最高的任务（不移除）
//
// 分桶模式下按 p4(Urgent) -> p0(Background) 顺序遍历，返回第一个非空分桶的首个任务；
// 单 ZSet 模式保持原行为（ZRangeWithScores 取 score 最小者，即优先级最高者）。
func (s *Scheduler) PeekTask(ctx context.Context) (*TaskInfo, error) {
	if s.enablePriorityBucket.Load() {
		for i := PriorityUrgent; i >= PriorityBackground; i-- {
			bucketKey := priorityBucketKey(i)
			results, err := s.rdb.ZRangeWithScores(ctx, bucketKey, 0, 0).Result()
			if err != nil {
				return nil, err
			}
			if len(results) == 0 {
				continue
			}
			member, ok := results[0].Member.(string)
			if !ok {
				continue
			}
			var task TaskInfo
			if err := json.Unmarshal([]byte(member), &task); err != nil {
				return nil, err
			}
			return &task, nil
		}
		return nil, nil
	}

	results, err := s.rdb.ZRangeWithScores(ctx, s.queueKey, 0, 0).Result()
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(results[0].Member.(string)), &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// GetTasksByPriority 获取指定优先级范围的任务
//
// 分桶模式下，每个桶只含单一优先级任务（桶索引=priority 数值），
// 因此直接遍历 [minPriority, maxPriority] 范围内的桶并合并结果，
// 顺序为高优先级桶（maxPriority）-> 低优先级桶（minPriority），与 PeekTask 一致。
// 单 ZSet 模式保持原行为（按 score 范围查询）。
func (s *Scheduler) GetTasksByPriority(ctx context.Context, minPriority, maxPriority int, limit int64) ([]*TaskInfo, error) {
	if s.enablePriorityBucket.Load() {
		return s.getTasksByPriorityFromBuckets(ctx, minPriority, maxPriority, limit)
	}

	// 计算分数范围（注意：分数越小优先级越高）
	now := time.Now()
	maxScore := s.calculatePriorityScore(minPriority, now)
	minScore := s.calculatePriorityScore(maxPriority, now)

	results, err := s.rdb.ZRangeByScoreWithScores(ctx, s.queueKey, &redis.ZRangeBy{
		Min:   fmt.Sprintf("%f", minScore),
		Max:   fmt.Sprintf("%f", maxScore),
		Count: limit,
	}).Result()
	if err != nil {
		return nil, err
	}

	tasks := make([]*TaskInfo, 0, len(results))
	for _, r := range results {
		var task TaskInfo
		if err := json.Unmarshal([]byte(r.Member.(string)), &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// getTasksByPriorityFromBuckets 分桶模式下按优先级范围跨桶查询
// 每个桶只含单一优先级任务，桶索引=priority 数值，因此直接遍历 [minPriority, maxPriority] 范围的桶
// limit<=0 表示不限制数量（与单 ZSet 模式下 ZRangeByScore Count=0 语义一致）
func (s *Scheduler) getTasksByPriorityFromBuckets(ctx context.Context, minPriority, maxPriority int, limit int64) ([]*TaskInfo, error) {
	// 钳制到有效优先级范围
	if minPriority < PriorityBackground {
		minPriority = PriorityBackground
	}
	if maxPriority > PriorityUrgent {
		maxPriority = PriorityUrgent
	}
	if minPriority > maxPriority {
		return nil, nil
	}

	tasks := make([]*TaskInfo, 0)
	remaining := limit
	// 从高优先级桶（maxPriority）向低优先级桶（minPriority）遍历，与 PeekTask 顺序一致
	for p := maxPriority; p >= minPriority; p-- {
		if limit > 0 && remaining <= 0 {
			break
		}
		bucketKey := priorityBucketKey(p)
		rangeOpt := &redis.ZRangeBy{Min: "-inf", Max: "+inf"}
		if limit > 0 {
			rangeOpt.Count = remaining
		}
		results, err := s.rdb.ZRangeByScoreWithScores(ctx, bucketKey, rangeOpt).Result()
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			member, ok := r.Member.(string)
			if !ok {
				continue
			}
			var task TaskInfo
			if err := json.Unmarshal([]byte(member), &task); err != nil {
				continue
			}
			tasks = append(tasks, &task)
			if limit > 0 {
				remaining--
			}
		}
	}

	return tasks, nil
}

// GetQueueLength 获取队列长度
func (s *Scheduler) GetQueueLength(ctx context.Context) (int64, error) {
	if !s.enablePriorityBucket.Load() {
		return s.rdb.ZCard(ctx, s.queueKey).Result()
	}

	pipe := s.rdb.Pipeline()
	counts := make([]*redis.IntCmd, PriorityUrgent-PriorityBackground+1)
	for priority := PriorityBackground; priority <= PriorityUrgent; priority++ {
		counts[priority-PriorityBackground] = pipe.ZCard(ctx, priorityBucketKey(priority))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}

	var total int64
	for i := range counts {
		value, err := counts[i].Result()
		if err != nil {
			return 0, err
		}
		total += value
	}
	return total, nil
}

// GetProcessingCount 获取处理中任务数
func (s *Scheduler) GetProcessingCount(ctx context.Context) (int64, error) {
	return s.rdb.SCard(ctx, s.processingKey).Result()
}

// TaskConfig 任务配置
type TaskConfig struct {
	PortScan     *PortScanConfig     `json:"portscan,omitempty"`
	PortIdentify *PortIdentifyConfig `json:"portidentify,omitempty"` // 端口识别（Nmap服务识别）
	DomainScan   *DomainScanConfig   `json:"domainscan,omitempty"`
	Fingerprint  *FingerprintConfig  `json:"fingerprint,omitempty"`
	BruteScan    *BruteScanConfig    `json:"brutescan,omitempty"` // 弱口令扫描
	PocScan      *PocScanConfig      `json:"pocscan,omitempty"`
	DirScan      *DirScanConfig      `json:"dirscan,omitempty"`  // 目录扫描
	JSFinder     *JSFinderConfig     `json:"jsfinder,omitempty"` // JS 敏感信息与未授权检测
}

// JSFinderConfig JSFinder 扫描配置
// 4 个清单（高危路由 / 鉴权关键词 / 敏感数据关键词 / 域名黑名单）由 Worker 从 API 动态拉取，不在此结构中下发
// EnableSourcemap / EnableUnauthCheck 使用指针：nil 表示"未显式设置"，Worker 侧默认视为 true；
// &false 表示用户明确关闭。
type JSFinderConfig struct {
	Enable            bool  `json:"enable"`
	Threads           int   `json:"threads,omitempty"`           // 并发线程数，默认 10
	Timeout           int   `json:"timeout,omitempty"`           // 单个 HTTP 请求超时(秒)，默认 10
	EnableSourcemap   *bool `json:"enableSourcemap,omitempty"`   // nil=默认启用；&false=明确关闭
	EnableUnauthCheck *bool `json:"enableUnauthCheck,omitempty"` // nil=默认启用；&false=明确关闭
	ForceScan         bool  `json:"forceScan"`                   // 强制扫描：无资产时直接使用目标
}

// BruteScanConfig 弱口令扫描配置
type BruteScanConfig struct {
	Enable          bool     `json:"enable"`
	Services        []string `json:"services"`        // 服务列表: ssh,mysql,redis,mongodb,postgresql,mssql,ftp,snmp,oracle,smb,mqtt
	Threads         int      `json:"threads"`         // 并发线程数
	Timeout         int      `json:"timeout"`         // 单次连接超时(秒)
	DelayMs         int      `json:"delayMs"`         // 每次尝试间隔(毫秒)
	WeakpassDictIds []string `json:"weakpassDictIds"` // 字典ID列表
	UseDefaultDict  bool     `json:"useDefaultDict"`  // 是否使用默认字典
	StopOnFirst     bool     `json:"stopOnFirst"`     // 发现一个弱口令即停止
	ForceScan       bool     `json:"forceScan"`       // 强制扫描（不检测端口开放状态）
}

// DirScanConfig 目录扫描配置
type DirScanConfig struct {
	Enable         bool     `json:"enable"`
	Tool           string   `json:"tool"`           // 扫描工具: ffuf(默认), feroxbuster
	DictIds        []string `json:"dictIds"`        // 字典ID列表
	Threads        int      `json:"threads"`        // 并发线程数
	Timeout        int      `json:"timeout"`        // 单个请求超时(秒)
	Extensions     []string `json:"extensions"`     // 文件扩展名
	StatusCodes    []int    `json:"statusCodes"`    // 有效HTTP状态码列表（空则使用默认：200,204,301,302,307,401,403,405,500）
	FollowRedirect bool     `json:"followRedirect"` // 是否跟随重定向
	ForceScan      bool     `json:"forceScan"`      // 强制扫描：无资产时直接使用目标
	// ffuf 高级配置
	AutoCalibration bool   `json:"autoCalibration"` // 自动校准（anti soft-404）
	FilterSize      string `json:"filterSize"`      // 按响应大小过滤，逗号分隔
	FilterWords     string `json:"filterWords"`     // 按单词数过滤
	FilterLines     string `json:"filterLines"`     // 按行数过滤
	FilterRegex     string `json:"filterRegex"`     // 按正则过滤
	MatcherMode     string `json:"matcherMode"`     // 匹配模式 and/or
	FilterMode      string `json:"filterMode"`      // 过滤模式 and/or
	Rate            int    `json:"rate"`            // 每秒请求速率限制
	Recursion       bool   `json:"recursion"`       // 递归扫描
	RecursionDepth  int    `json:"recursionDepth"`  // 递归深度
}

const (
	defaultPortScanTargetTimeoutSeconds  = 120
	defaultNaabuProbeTimeoutMilliseconds = 1000
	defaultNaabuRetries                  = 2
)

type PortScanConfig struct {
	Enable            bool   `json:"enable"`
	Tool              string `json:"tool"` // tcp, masscan, naabu
	Ports             string `json:"ports"`
	Rate              int    `json:"rate"`              // 每秒发送包数，默认3000，建议3000-7000
	TargetTimeout     int    `json:"targetTimeout"`     // 单个目标完整端口扫描上限（秒），默认60
	ProbeTimeoutMs    int    `json:"probeTimeoutMs"`    // Naabu 单次端口探测等待时间（毫秒），默认1000
	LegacyTimeout     int    `json:"-"`                 // 已废弃：仅由 UnmarshalJSON 读取旧版 timeout
	PortThreshold     int    `json:"portThreshold"`     // 开放端口数量阈值，超过则过滤该主机
	ScanType          string `json:"scanType"`          // s=SYN, c=CONNECT，默认 c
	SkipHostDiscovery bool   `json:"skipHostDiscovery"` // 跳过主机发现 (-Pn)
	ExcludeCDN        bool   `json:"excludeCDN"`        // 排除 CDN/WAF，仅扫描 80,443 端口 (-ec)
	ExcludeHosts      string `json:"excludeHosts"`      // 排除的目标，逗号分隔的 IP/CIDR
	Retries           int    `json:"retries"`           // 重试次数，默认2，建议1-2
	WarmUpTime        int    `json:"warmUpTime"`        // 扫描阶段间等待时间(秒)，默认1，建议0-1
	Workers           int    `json:"workers"`           // Naabu内部工作线程，默认50，建议50-100
	Verify            bool   `json:"verify"`            // TCP验证，默认false（禁用以提速）
}

// UnmarshalJSON 兼容旧版 portscan.timeout，并将两个不同维度的超时归一化。
func (c *PortScanConfig) UnmarshalJSON(data []byte) error {
	type plain PortScanConfig
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	// 兼容字段不参与正常序列化；指针用于区分 retries 缺失与显式 0。
	var compatibility struct {
		LegacyTimeout int  `json:"timeout"`
		Retries       *int `json:"retries"`
	}
	if err := json.Unmarshal(data, &compatibility); err != nil {
		return err
	}

	if decoded.TargetTimeout == 0 {
		decoded.TargetTimeout = compatibility.LegacyTimeout
	}
	if decoded.TargetTimeout == 0 {
		decoded.TargetTimeout = defaultPortScanTargetTimeoutSeconds
	}
	if decoded.TargetTimeout < 0 {
		return fmt.Errorf("portscan.targetTimeout must be non-negative, got %d", decoded.TargetTimeout)
	}
	if decoded.ProbeTimeoutMs == 0 {
		decoded.ProbeTimeoutMs = defaultNaabuProbeTimeoutMilliseconds
	}
	if decoded.ProbeTimeoutMs < 0 {
		return fmt.Errorf("portscan.probeTimeoutMs must be non-negative, got %d", decoded.ProbeTimeoutMs)
	}
	if compatibility.Retries == nil {
		decoded.Retries = defaultNaabuRetries
	}

	// 后续序列化只输出新字段，避免继续传播语义含糊的 timeout。
	decoded.LegacyTimeout = 0
	*c = PortScanConfig(decoded)
	return nil
}

// PortIdentifyConfig 端口识别配置（Nmap/Fingerprintx 服务识别）
type PortIdentifyConfig struct {
	Enable        bool   `json:"enable"`
	Tool          string `json:"tool"`          // 识别工具: nmap, fingerprintx (默认 nmap)
	Timeout       int    `json:"timeout"`       // 单个主机超时时间(秒)，默认30秒
	Concurrency   int    `json:"concurrency"`   // 并发数，默认10 (仅 fingerprintx)
	Args          string `json:"args"`          // Nmap额外参数，如 "-sV --version-intensity 5"
	UDP           bool   `json:"udp"`           // 是否扫描UDP端口 (仅 fingerprintx)
	FastMode      bool   `json:"fastMode"`      // 快速模式 (仅 fingerprintx)
	ForceScan     bool   `json:"forceScan"`     // 强制扫描：无资产时直接使用目标
	ExcludeClosed bool   `json:"excludeClosed"` // 仅在 Nmap 明确确认 CLOSED 时从后续活跃资产中排除
}

type DomainScanConfig struct {
	Enable             bool     `json:"enable"`
	Subfinder          bool     `json:"subfinder"`          // 使用Subfinder
	Timeout            int      `json:"timeout"`            // 超时时间(秒)
	MaxEnumerationTime int      `json:"maxEnumerationTime"` // 最大枚举时间(分钟)
	Threads            int      `json:"threads"`            // 并发线程数
	RateLimit          int      `json:"rateLimit"`          // 速率限制
	Sources            []string `json:"sources"`            // 指定数据源
	ExcludeSources     []string `json:"excludeSources"`     // 排除数据源
	All                bool     `json:"all"`                // 使用所有数据源(慢)
	Recursive          bool     `json:"recursive"`          // 只使用递归数据源
	RemoveWildcard     bool     `json:"removeWildcard"`     // 移除泛解析域名
	ResolveDNS         bool     `json:"resolveDNS"`         // 是否解析DNS（使用dnsx）
	Concurrent         int      `json:"concurrent"`         // DNS解析并发数
	SubdomainDictIds   []string `json:"subdomainDictIds"`   // 子域名暴力破解字典ID列表
	// 子域名暴力破解引擎配置
	BruteforceEngine  string `json:"bruteforceEngine"`  // 暴力破解引擎: dnsx, ksubdomain (默认ksubdomain)
	BruteforceTimeout int    `json:"bruteforceTimeout"` // 暴力破解超时时间(分钟)
	Bandwidth         string `json:"bandwidth"`         // ksubdomain带宽限制，如"5M", "10M", "100M"
	Retry             int    `json:"retry"`             // ksubdomain重试次数
	WildcardMode      string `json:"wildcardMode"`      // ksubdomain泛解析过滤模式: basic, advanced, none
	// Dnsx增强功能
	RecursiveBrute   bool     `json:"recursiveBrute"`   // 递归爆破
	RecursiveDictIds []string `json:"recursiveDictIds"` // 递归爆破字典ID列表
	WildcardDetect   bool     `json:"wildcardDetect"`   // 泛解析检测并处理
}

type FingerprintConfig struct {
	Enable        bool   `json:"enable"`
	Tool          string `json:"tool"`  // 探测工具: httpx, builtin (wappalyzer)
	Httpx         bool   `json:"httpx"` // 已废弃，使用Tool字段
	IconHash      bool   `json:"iconHash"`
	Wappalyzer    bool   `json:"wappalyzer"`   // 已废弃，builtin模式自动启用
	CustomEngine  bool   `json:"customEngine"` // 使用自定义指纹引擎（ARL格式）
	Screenshot    bool   `json:"screenshot"`
	ActiveScan    bool   `json:"activeScan"`    // 启用主动指纹扫描
	Cert          bool   `json:"cert"`          // 启用证书抓取（ARL 风格附加功能），默认关闭
	ActiveTimeout int    `json:"activeTimeout"` // 主动指纹单个请求超时时间(秒)，默认10秒
	Timeout       int    `json:"timeout"`       // 总超时时间(秒)，默认300秒
	TargetTimeout int    `json:"targetTimeout"` // 单个目标超时时间(秒)，默认30秒
	Concurrency   int    `json:"concurrency"`   // 指纹识别并发数，默认10
	FilterMode    string `json:"filterMode"`    // 过滤模式: "http_mapping"(使用HTTP映射), "service_mapping"(使用服务映射过滤非HTTP)
	ForceScan     bool   `json:"forceScan"`     // 强制扫描：无资产时直接使用目标
}

type PocScanConfig struct {
	Enable            bool                `json:"enable"`
	PocTypes          []string            `json:"pocTypes"`          // nuclei, builtin
	PocFiles          []string            `json:"pocFiles"`          // 自定义POC文件
	UseNuclei         bool                `json:"useNuclei"`         // 使用Nuclei扫描
	AutoScan          bool                `json:"autoScan"`          // 基于自定义标签映射自动扫描
	AutomaticScan     bool                `json:"automaticScan"`     // 基于Wappalyzer内置映射自动扫描（类似nuclei -as）
	Severity          string              `json:"severity"`          // 严重级别过滤
	Tags              []string            `json:"tags"`              // 手动指定标签
	ExcludeTags       []string            `json:"excludeTags"`       // 排除标签
	RateLimit         int                 `json:"rateLimit"`         // 速率限制
	Concurrency       int                 `json:"concurrency"`       // 并发数
	TargetTimeout     int                 `json:"targetTimeout"`     // 单个目标超时时间(秒)，默认600秒
	CustomPocOnly     bool                `json:"customPocOnly"`     // 只使用自定义POC
	CustomTemplates   []string            `json:"customTemplates"`   // 自定义POC模板内容(YAML) - 已废弃
	NucleiTemplates   []string            `json:"nucleiTemplates"`   // 从数据库获取的模板内容(YAML) - 已废弃
	NucleiTemplateIds []string            `json:"nucleiTemplateIds"` // Nuclei模板ID列表（新）
	CustomPocIds      []string            `json:"customPocIds"`      // 自定义POC ID列表（新）
	TagMappings       map[string][]string `json:"tagMappings"`       // 应用名称到Nuclei标签的映射
	ForceScan         bool                `json:"forceScan"`         // 强制扫描：无资产时直接使用目标进行POC扫描
	CustomHeaders     []string            `json:"customHeaders"`     // 自定义HTTP头部，格式: "Header: Value"
}

// ParseTaskConfig 解析任务配置
func ParseTaskConfig(configStr string) (*TaskConfig, error) {
	var config TaskConfig
	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// BuildTaskConfig 构建任务配置
func BuildTaskConfig(config *TaskConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TaskResult 任务结果
type TaskResult struct {
	TaskId     string `json:"taskId"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	AssetCount int    `json:"assetCount"`
	VulCount   int    `json:"vulCount"`
	Duration   int64  `json:"duration"`
}

// FormatResult 格式化结果
func (r *TaskResult) FormatResult() string {
	return fmt.Sprintf("状态:%s 资产:%d 漏洞:%d 耗时:%ds",
		r.Status, r.AssetCount, r.VulCount, r.Duration)
}

// ==================== Worker Load Management ====================

// UpdateWorkerLoad 更新Worker负载信息
func (s *Scheduler) UpdateWorkerLoad(ctx context.Context, load *WorkerLoad) error {
	data, err := json.Marshal(load)
	if err != nil {
		return err
	}
	// 使用Hash存储，key为worker名称
	return s.rdb.HSet(ctx, s.workerLoadKey, load.WorkerName, data).Err()
}

// GetWorkerLoad 获取单个Worker负载信息
func (s *Scheduler) GetWorkerLoad(ctx context.Context, workerName string) (*WorkerLoad, error) {
	data, err := s.rdb.HGet(ctx, s.workerLoadKey, workerName).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var load WorkerLoad
	if err := json.Unmarshal([]byte(data), &load); err != nil {
		return nil, err
	}
	return &load, nil
}

// GetAllWorkerLoads 获取所有Worker负载信息
func (s *Scheduler) GetAllWorkerLoads(ctx context.Context) ([]*WorkerLoad, error) {
	data, err := s.rdb.HGetAll(ctx, s.workerLoadKey).Result()
	if err != nil {
		return nil, err
	}

	loads := make([]*WorkerLoad, 0, len(data))
	for _, v := range data {
		var load WorkerLoad
		if err := json.Unmarshal([]byte(v), &load); err != nil {
			continue
		}
		loads = append(loads, &load)
	}
	return loads, nil
}

// GetAvailableWorkers 获取可用的Worker列表（按负载排序）
func (s *Scheduler) GetAvailableWorkers(ctx context.Context) ([]*WorkerLoad, error) {
	loads, err := s.GetAllWorkerLoads(ctx)
	if err != nil {
		return nil, err
	}

	// 过滤可用的Worker
	available := make([]*WorkerLoad, 0)
	for _, load := range loads {
		if load.IsAvailable() {
			available = append(available, load)
		}
	}

	// 预计算负载分数，避免排序比较时重复计算 O(n log n) 次 LoadScore()
	type workerWithScore struct {
		load  *WorkerLoad
		score float64
	}
	availableWithScores := make([]workerWithScore, 0, len(available))
	for _, w := range available {
		availableWithScores = append(availableWithScores, workerWithScore{
			load:  w,
			score: w.LoadScore(),
		})
	}
	// 按负载分数排序（升序，负载低的在前）
	sort.Slice(availableWithScores, func(i, j int) bool {
		return availableWithScores[i].score < availableWithScores[j].score
	})
	for i, item := range availableWithScores {
		available[i] = item.load
	}

	return available, nil
}

// SelectWorkerForTask 为任务选择最佳Worker
// 返回负载最低的可用Worker
func (s *Scheduler) SelectWorkerForTask(ctx context.Context) (*WorkerLoad, error) {
	workers, err := s.GetAvailableWorkers(ctx)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 {
		return nil, nil
	}
	return workers[0], nil
}

// RemoveWorkerLoad 移除Worker负载信息（Worker下线时调用）
func (s *Scheduler) RemoveWorkerLoad(ctx context.Context, workerName string) error {
	return s.rdb.HDel(ctx, s.workerLoadKey, workerName).Err()
}
