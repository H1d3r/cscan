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

	"github.com/google/uuid"
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
	TaskStatusFailure = "FAILURE"
	TaskStatusRevoked = "REVOKED"
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
	TaskId     string   `json:"taskId"`
	MainTaskId string   `json:"mainTaskId"`
	TaskName   string   `json:"taskName"`
	Config     string   `json:"config"`
	Priority   int      `json:"priority"`
	CreateTime string   `json:"createTime"`
	Workers    []string `json:"workers,omitempty"` // 指定执行任务的 Worker 列表，为空表示任意 Worker
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

// atomicPopTaskScript 默认路径（非分桶）原子弹出 Lua 脚本
// 与 checktasklogic.go 的 atomicPopTaskScript 保持一致：
//  1. ZPOPMIN 从队列弹出
//  2. pcall 解析 JSON，失败时移入 cscan:task:deadletter 死信队列
//  3. SADD 加入 processing 集合
//  4. SET taskInfo（24h TTL，供 TaskRecoveryManager 恢复读取）
//  5. SET execution info（1h TTL）
//
// 修复 C-01：原默认路径脚本不持久化 taskInfo/execution，导致恢复链路断裂、任务永久丢失
// 修复 C6：死信 PUBLISH 移出原子 Lua,避免订阅消费慢或缓冲满时阻塞整个 pop 事务。
//
//	Lua 仅做 ZADD 死信并返回 "__DL__" 前缀的哨兵字符串,由调用方在 Lua 返回后再 PUBLISH。
var atomicPopTaskScript = redis.NewScript(`
	local queueKey = KEYS[1]
	local processingKey = KEYS[2]
	local taskInfoPrefix = ARGV[1]
	local execPrefix = ARGV[2]
	local workerName = ARGV[3]
	local ttlSeconds = tonumber(ARGV[4])
	local nowStr = ARGV[5]

	local result = redis.call('ZPOPMIN', queueKey, 1)
	if #result == 0 then
		return nil
	end
	local member = result[1]
	local score = result[2]

	-- 解析任务 JSON 提取 taskId
	local data = nil
	pcall(function() data = cjson.decode(member) end)
	if not data or not data.taskId then
		-- decode 失败移入死信队列，避免放回后形成无限循环阻塞整个队列
		redis.call('ZADD', 'cscan:task:deadletter', score, member)
		-- 返回 "__DL__" 前缀哨兵,由调用方在 Lua 外执行 PUBLISH,避免阻塞 pop 事务
		return '__DL__' .. member
	end
	local taskId = data.taskId

	-- 原子加入 processing 集合
	redis.call('SADD', processingKey, taskId)

	-- 持久化 taskInfo（24h TTL，供 TaskRecoveryManager 恢复读取）
	local taskInfoKey = taskInfoPrefix .. taskId
	redis.call('SET', taskInfoKey, member, 'EX', ttlSeconds)

	-- 记录 execution info（与 TaskRecoveryManager.saveTaskExecutionInfo 格式一致）
	local execKey = execPrefix .. taskId
	local execInfo = cjson.encode({
		taskId = taskId,
		workerName = workerName,
		startTime = nowStr,
		lastUpdate = nowStr,
		phase = "started",
		progress = 0,
		retryCount = 0,
		maxRetries = 3
	})
	redis.call('SET', execKey, execInfo, 'EX', 3600)

	return member
`)

// atomicPopForWorkerScript 默认路径（非分桶）Worker 优先原子弹出 Lua 脚本
// 与 atomicPopTaskScript 一致，但先从 Worker 专属队列弹出，再回退到公共队列
// 修复 C-01：持久化 taskInfo/execution，保证恢复链路完整
// 修复 C6：死信 PUBLISH 移出原子 Lua,返回 "__DL__" 前缀哨兵,由调用方在 Lua 外 PUBLISH
var atomicPopForWorkerScript = redis.NewScript(`
	local workerQueueKey = KEYS[1]
	local queueKey = KEYS[2]
	local processingKey = KEYS[3]
	local taskInfoPrefix = ARGV[1]
	local execPrefix = ARGV[2]
	local workerName = ARGV[3]
	local ttlSeconds = tonumber(ARGV[4])
	local nowStr = ARGV[5]

	local member = nil
	local score = 0
	local result = redis.call('ZPOPMIN', workerQueueKey, 1)
	if #result > 0 then
		member = result[1]
		score = result[2]
	else
		result = redis.call('ZPOPMIN', queueKey, 1)
		if #result > 0 then
			member = result[1]
			score = result[2]
		end
	end
	if member == nil then
		return nil
	end

	-- 解析任务 JSON 提取 taskId
	local data = nil
	pcall(function() data = cjson.decode(member) end)
	if not data or not data.taskId then
		redis.call('ZADD', 'cscan:task:deadletter', score, member)
		return '__DL__' .. member
	end
	local taskId = data.taskId

	redis.call('SADD', processingKey, taskId)

	local taskInfoKey = taskInfoPrefix .. taskId
	redis.call('SET', taskInfoKey, member, 'EX', ttlSeconds)

	local execKey = execPrefix .. taskId
	local execInfo = cjson.encode({
		taskId = taskId,
		workerName = workerName,
		startTime = nowStr,
		lastUpdate = nowStr,
		phase = "started",
		progress = 0,
		retryCount = 0,
		maxRetries = 3
	})
	redis.call('SET', execKey, execInfo, 'EX', 3600)

	return member
`)

// popFromPriorityBuckets 跨分桶原子弹出（p4 Urgent -> p0 Background 顺序）
// 使用 Lua 脚本按优先级从高到低检查 5 个分桶，从第一个非空分桶弹出
// 同时原子加入 processing 集合，持久化 taskInfo/execution（对齐 checktasklogic.go 的 atomicPopTaskScript）
var popFromBucketsScript = redis.NewScript(`
	local bucketPrefix = KEYS[1]
	local processingKey = KEYS[2]
	local taskInfoPrefix = ARGV[1]
	local execPrefix = ARGV[2]
	local workerName = ARGV[3]
	local ttlSeconds = tonumber(ARGV[4])
	local nowStr = ARGV[5]

	-- 按 p4 -> p0 顺序检查 5 个分桶（urgent 先，background 后）
	for i = 4, 0, -1 do
		local bucketKey = bucketPrefix .. ":p" .. i
		local result = redis.call('ZPOPMIN', bucketKey, 1)
		if #result > 0 then
			local member = result[1]
			local score = result[2]
			-- 使用 pcall 保护 cjson.decode，避免无效 member 抛错中断脚本
			local data = nil
			pcall(function() data = cjson.decode(member) end)
			if not data or not data.taskId then
				-- 修复 C2：decode 失败时移入死信队列，避免放回后形成无限循环阻塞整个分桶
				-- 修复 C6：PUBLISH 移出 Lua,由调用方在 Lua 返回后执行,避免阻塞 pop 事务
				redis.call('ZADD', 'cscan:task:deadletter', score, member)
				return '__DL__' .. member
			end
			local taskId = data.taskId
			redis.call('SADD', processingKey, taskId)
			-- 持久化 taskInfo（24h TTL）
			redis.call('SET', taskInfoPrefix .. taskId, member, 'EX', ttlSeconds)
			-- 记录 execution info
			local execInfo = cjson.encode({
				taskId = taskId,
				workerName = workerName,
				startTime = nowStr,
				lastUpdate = nowStr,
				phase = "started",
				progress = 0,
				retryCount = 0,
				maxRetries = 3
			})
			redis.call('SET', execPrefix .. taskId, execInfo, 'EX', 3600)
			return member
		end
	end
	return nil
`)

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

// popFromPriorityBuckets 从优先级分桶弹出任务
func (s *Scheduler) popFromPriorityBuckets(ctx context.Context) (*TaskInfo, error) {
	result, err := popFromBucketsScript.Run(ctx, s.rdb,
		[]string{"cscan:task:queue", s.processingKey},
		"cscan:task:info:",
		"cscan:task:execution:",
		"", // PopTask 无 worker 上下文
		86400,
		time.Now().Format(time.RFC3339),
	).Result()
	if err == redis.Nil || result == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type: %T", result)
	}

	// 修复 C6：死信哨兵在 Lua 外执行 PUBLISH,避免阻塞 pop 事务
	if strings.HasPrefix(taskData, "__DL__") {
		s.publishDeadLetterAlert(ctx, strings.TrimPrefix(taskData, "__DL__"))
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		return nil, err
	}
	return &task, nil
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

	if task.TaskId == "" {
		task.TaskId = uuid.New().String()
	}
	now := time.Now()
	task.CreateTime = now.Local().Format("2006-01-02 15:04:05")

	// 使用统一的优先级分数计算
	score := s.calculatePriorityScore(task.Priority, now)

	var err error
	notifyWorkers := false

	// 如果指定了 Workers，推送到每个 Worker 的专属队列（不受分桶影响）
	// 专属队列任务量小，分桶收益有限，保留原行为
	if len(task.Workers) > 0 {
		pipe := s.rdb.Pipeline()
		for _, workerName := range task.Workers {
			taskCopy := *task
			taskCopy.Workers = []string{workerName}
			data, _ := json.Marshal(taskCopy)
			workerQueueKey := s.GetWorkerQueueKey(workerName)
			pipe.ZAdd(ctx, workerQueueKey, redis.Z{
				Score:  score,
				Member: string(data),
			})
		}
		_, err = pipe.Exec(ctx)
		notifyWorkers = err == nil
	} else if s.enablePriorityBucket.Load() {
		// 公共队列：分桶路径
		err = s.pushToPriorityBucket(ctx, task, score)
		notifyWorkers = err == nil
	} else {
		// 默认路径：单 ZSet（保持向后兼容）
		var data []byte
		data, err = json.Marshal(task)
		if err != nil {
			return fmt.Errorf("marshal task: %w", err)
		}
		err = s.rdb.ZAdd(ctx, s.queueKey, redis.Z{
			Score:  score,
			Member: string(data),
		}).Err()
		notifyWorkers = err == nil
	}

	// 推送成功后通知等待中的 Worker（Pub/Sub 长轮询唤醒）
	if notifyWorkers {
		s.notifyTaskAvailable(ctx)
	}

	return err
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
		// 记录每个任务的平均推送时间
		avgLatency := time.Since(startTime) / time.Duration(len(tasks))
		for range tasks {
			s.metrics.RecordPush(avgLatency)
		}
	}()

	pipe := s.rdb.Pipeline()
	baseTime := time.Now()

	for i, task := range tasks {
		if task.TaskId == "" {
			task.TaskId = uuid.New().String()
		}
		task.CreateTime = baseTime.Local().Format("2006-01-02 15:04:05")

		// 使用统一的优先级分数计算
		// 同一批次的任务按 i 微秒递增创建时间，保持顺序
		// 修复：原 float64(i)*0.001 在 UnixMicro（~1.75e15）量级被 float64 ULP（~0.25）吞掉
		score := s.calculatePriorityScore(task.Priority, baseTime.Add(time.Duration(i)*time.Microsecond))

		// 如果指定了 Workers，推送到每个 Worker 的专属队列
		if len(task.Workers) > 0 {
			for _, workerName := range task.Workers {
				taskCopy := *task
				taskCopy.Workers = []string{workerName}
				data, err := json.Marshal(taskCopy)
				if err != nil {
					return fmt.Errorf("marshal task for worker %s: %w", workerName, err)
				}
				workerQueueKey := s.GetWorkerQueueKey(workerName)
				pipe.ZAdd(ctx, workerQueueKey, redis.Z{
					Score:  score,
					Member: string(data),
				})
			}
		} else if s.enablePriorityBucket.Load() {
			// 分桶路径：按 task.Priority 路由到对应分桶 ZSet
			data, err := json.Marshal(task)
			if err != nil {
				return fmt.Errorf("marshal task %s: %w", task.TaskId, err)
			}
			bucketKey := priorityBucketKey(task.Priority)
			pipe.ZAdd(ctx, bucketKey, redis.Z{
				Score:  score,
				Member: string(data),
			})
		} else {
			data, err := json.Marshal(task)
			if err != nil {
				return fmt.Errorf("marshal task %s: %w", task.TaskId, err)
			}
			pipe.ZAdd(ctx, s.queueKey, redis.Z{
				Score:  score,
				Member: string(data),
			})
		}
	}

	_, err := pipe.Exec(ctx)
	if err == nil {
		s.notifyTaskAvailable(ctx)
	}
	return err
}

func (s *Scheduler) PopTask(ctx context.Context) (*TaskInfo, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPop(time.Since(startTime))
	}()

	// 分桶路径：跨 5 个分桶按 P4 -> P0 顺序原子弹出（urgent 先，background 后）
	if s.enablePriorityBucket.Load() {
		return s.popFromPriorityBuckets(ctx)
	}

	// 默认路径：单 ZSet（保持向后兼容）
	// 修复 C-01：使用与 checktasklogic.go 一致的 atomicPopTaskScript，持久化 taskInfo/execution
	// 避免恢复链路断裂导致任务永久丢失
	result, err := atomicPopTaskScript.Run(ctx, s.rdb,
		[]string{s.queueKey, s.processingKey},
		"cscan:task:info:",
		"cscan:task:execution:",
		"", // 默认路径无 worker 上下文
		86400,
		time.Now().Format(time.RFC3339),
	).Result()
	if err == redis.Nil || result == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type: %T", result)
	}
	// 修复 C6：死信哨兵在 Lua 外 PUBLISH
	if strings.HasPrefix(taskData, "__DL__") {
		s.publishDeadLetterAlert(ctx, strings.TrimPrefix(taskData, "__DL__"))
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// PopTaskForWorker 从队列获取任务（考虑Worker负载）
// 优先从Worker专属队列获取，然后从公共队列获取
//
// 分桶路径下，公共队列弹出改为跨 5 个分桶按 P4 -> P0 顺序原子弹出（urgent 先）
// 专属队列保持单 ZSet（任务量小，分桶收益有限）
func (s *Scheduler) PopTaskForWorker(ctx context.Context, workerName string) (*TaskInfo, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPop(time.Since(startTime))
	}()

	workerQueueKey := s.GetWorkerQueueKey(workerName)

	// 分桶路径：专属队列 + 跨分桶公共队列
	if s.enablePriorityBucket.Load() {
		return s.popForWorkerFromBuckets(ctx, workerQueueKey, workerName)
	}

	// 默认路径：专属队列 + 单 ZSet 公共队列
	// 修复 C-01：使用 atomicPopForWorkerScript，持久化 taskInfo/execution，保证恢复链路完整
	result, err := atomicPopForWorkerScript.Run(ctx, s.rdb,
		[]string{workerQueueKey, s.queueKey, s.processingKey},
		"cscan:task:info:",
		"cscan:task:execution:",
		workerName,
		86400,
		time.Now().Format(time.RFC3339),
	).Result()
	if err == redis.Nil || result == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type: %T", result)
	}
	// 修复 C6：死信哨兵在 Lua 外 PUBLISH
	if strings.HasPrefix(taskData, "__DL__") {
		s.publishDeadLetterAlert(ctx, strings.TrimPrefix(taskData, "__DL__"))
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// popForWorkerFromBuckets 分桶路径下的 Worker 弹出
// 先查专属队列，再跨 5 个公共分桶按 p4 Urgent -> p0 Background 顺序原子弹出
// 同时原子加入 processing 集合，持久化 taskInfo/execution（对齐 checktasklogic.go 的 atomicPopTaskScript）
var popForWorkerFromBucketsScript = redis.NewScript(`
	local workerQueueKey = KEYS[1]
	local bucketPrefix = KEYS[2]
	local processingKey = KEYS[3]
	local taskInfoPrefix = ARGV[1]
	local execPrefix = ARGV[2]
	local workerName = ARGV[3]
	local ttlSeconds = tonumber(ARGV[4])
	local nowStr = ARGV[5]

	-- 1. 先从 Worker 专属队列弹出
	local sourceKey = workerQueueKey
	local result = redis.call('ZPOPMIN', sourceKey, 1)

	-- 2. 若为空，跨 5 个公共分桶按 p4 -> p0 顺序弹出（urgent 先，background 后）
	if #result == 0 then
		for i = 4, 0, -1 do
			sourceKey = bucketPrefix .. ":p" .. i
			result = redis.call('ZPOPMIN', sourceKey, 1)
			if #result > 0 then
				break
			end
		end
	end

	if #result == 0 then
		return nil
	end

	local member = result[1]
	local score = result[2]
	-- 使用 pcall 保护 cjson.decode，避免无效 member 抛错中断脚本
	local data = nil
	pcall(function() data = cjson.decode(member) end)
	if not data or not data.taskId then
		-- 修复 C2：decode 失败时移入死信队列，避免放回后形成无限循环阻塞整个队列
		-- 修复 C6：PUBLISH 移出 Lua,由调用方在 Lua 返回后执行,避免阻塞 pop 事务
		redis.call('ZADD', 'cscan:task:deadletter', score, member)
		return '__DL__' .. member
	end
	local taskId = data.taskId

	redis.call('SADD', processingKey, taskId)
	-- 持久化 taskInfo（24h TTL）
	redis.call('SET', taskInfoPrefix .. taskId, member, 'EX', ttlSeconds)
	-- 记录 execution info
	local execInfo = cjson.encode({
		taskId = taskId,
		workerName = workerName,
		startTime = nowStr,
		lastUpdate = nowStr,
		phase = "started",
		progress = 0,
		retryCount = 0,
		maxRetries = 3
	})
	redis.call('SET', execPrefix .. taskId, execInfo, 'EX', 3600)

	return member
`)

// popForWorkerFromBuckets 执行分桶路径的 Worker 弹出
func (s *Scheduler) popForWorkerFromBuckets(ctx context.Context, workerQueueKey, workerName string) (*TaskInfo, error) {
	result, err := popForWorkerFromBucketsScript.Run(ctx, s.rdb,
		[]string{workerQueueKey, s.queueKey, s.processingKey},
		"cscan:task:info:",
		"cscan:task:execution:",
		workerName,
		86400,
		time.Now().Format(time.RFC3339),
	).Result()
	if err == redis.Nil || result == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type: %T", result)
	}

	// 修复 C6：死信哨兵在 Lua 外 PUBLISH
	if strings.HasPrefix(taskData, "__DL__") {
		s.publishDeadLetterAlert(ctx, strings.TrimPrefix(taskData, "__DL__"))
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		return nil, err
	}
	return &task, nil
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

// CompleteTask 完成任务
func (s *Scheduler) CompleteTask(ctx context.Context, taskId string) error {
	return s.rdb.SRem(ctx, s.processingKey, taskId).Err()
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
	defaultPortScanTargetTimeoutSeconds  = 60
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
	Enable      bool   `json:"enable"`
	Tool        string `json:"tool"`        // 识别工具: nmap, fingerprintx (默认 nmap)
	Timeout     int    `json:"timeout"`     // 单个主机超时时间(秒)，默认30秒
	Concurrency int    `json:"concurrency"` // 并发数，默认10 (仅 fingerprintx)
	Args        string `json:"args"`        // Nmap额外参数，如 "-sV --version-intensity 5"
	UDP         bool   `json:"udp"`         // 是否扫描UDP端口 (仅 fingerprintx)
	FastMode    bool   `json:"fastMode"`    // 快速模式 (仅 fingerprintx)
	ForceScan   bool   `json:"forceScan"`   // 强制扫描：无资产时直接使用目标
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

// ==================== Task Cancellation ====================

// CancelSignal 取消信号
type CancelSignal struct {
	TaskId    string    `json:"taskId"`
	Action    string    `json:"action"` // STOP, PAUSE
	Timestamp time.Time `json:"timestamp"`
}

// GetCancelSignalKey 获取控制信号的Redis Key
// 统一使用 cscan:task:ctrl:{taskId} 命名，与 API 端 tasklogic.go 和 taskhandler.go 保持一致
// 修复历史问题：原为 cscan:task:cancel:{taskId}，导致 HTTP 轮询回退读不到信号
func (s *Scheduler) GetCancelSignalKey(taskId string) string {
	return fmt.Sprintf("cscan:task:ctrl:%s", taskId)
}

// SendCancelSignal 发送取消信号
func (s *Scheduler) SendCancelSignal(ctx context.Context, taskId, action string) error {
	signal := &CancelSignal{
		TaskId:    taskId,
		Action:    action,
		Timestamp: time.Now(),
	}
	data, err := json.Marshal(signal)
	if err != nil {
		return err
	}

	key := s.GetCancelSignalKey(taskId)
	// 设置信号，5分钟后自动过期
	return s.rdb.Set(ctx, key, data, 5*time.Minute).Err()
}

// CheckCancelSignal 检查取消信号
func (s *Scheduler) CheckCancelSignal(ctx context.Context, taskId string) (*CancelSignal, error) {
	key := s.GetCancelSignalKey(taskId)
	data, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var signal CancelSignal
	if err := json.Unmarshal([]byte(data), &signal); err != nil {
		return nil, err
	}
	return &signal, nil
}

// ClearCancelSignal 清除取消信号
func (s *Scheduler) ClearCancelSignal(ctx context.Context, taskId string) error {
	key := s.GetCancelSignalKey(taskId)
	return s.rdb.Del(ctx, key).Err()
}

// PublishCancelSignal 通过Pub/Sub发布取消信号（实时通知）
func (s *Scheduler) PublishCancelSignal(ctx context.Context, taskId, action string) error {
	signal := &CancelSignal{
		TaskId:    taskId,
		Action:    action,
		Timestamp: time.Now(),
	}
	data, err := json.Marshal(signal)
	if err != nil {
		return err
	}

	// 同时设置Key（用于轮询检查）和发布消息（用于实时通知）
	// Key 用 cscan:task:ctrl:{taskId}；Pub/Sub 频道发布到 cscan:task:ctrl:{taskId}，
	// worker 与 wshandler.go 均通过 PSubscribe "cscan:task:ctrl:*" 接收，按频道名解析 taskId
	key := s.GetCancelSignalKey(taskId)
	if err := s.rdb.Set(ctx, key, data, 5*time.Minute).Err(); err != nil {
		return err
	}

	// 发布到带 taskId 的控制信号频道，确保 PSubscribe("cscan:task:ctrl:*") 能匹配
	return s.rdb.Publish(ctx, "cscan:task:ctrl:"+taskId, data).Err()
}

// SubscribeCancelSignals 订阅取消信号
// 修复 C-07：原实现存在两个问题
//  1. msg.Payload 在 pubsub.Channel() 关闭时 msg 为 nil，导致 nil pointer panic
//  2. 连接断开后无重连逻辑，订阅永久失效，取消信号无法送达
//
// 现增加 nil 检查、订阅状态校验和指数退避重连
func (s *Scheduler) SubscribeCancelSignals(ctx context.Context) <-chan *CancelSignal {
	ch := make(chan *CancelSignal, 100)

	go func() {
		defer close(ch)

		const maxBackoff = 30 * time.Second
		backoff := time.Second

		for {
			if ctx.Err() != nil {
				return
			}

			pubsub := s.rdb.PSubscribe(ctx, "cscan:task:ctrl:*")
			// sync.Once 保证 pubsub.Close() 只被调用一次,避免:
			//   - msg==nil 分支与 ctx.Done() 分支并发 Close
			//   - 我们主动 Close 与 go-redis 内部读循环退出时二次 Close
			var closeOnce sync.Once
			closePubsub := func() { closeOnce.Do(func() { pubsub.Close() }) }
			msgCh := pubsub.Channel()

			// 等待订阅确认，避免在订阅未就绪时消费
			_, err := pubsub.Receive(ctx)
			if err != nil {
				logx.Errorf("[Scheduler] Subscribe cscan:task:ctrl failed: %v, retry in %v", err, backoff)
				closePubsub()
				s.sleepCtx(ctx, backoff)
				backoff = s.nextBackoff(backoff, maxBackoff)
				continue
			}
			// 订阅成功，重置退避
			backoff = time.Second
			logx.Infof("[Scheduler] Subscribed to cscan:task:ctrl:* (pattern)")

		consumeLoop:
			for {
				select {
				case <-ctx.Done():
					closePubsub()
					return
				case msg, ok := <-msgCh:
					// 通道关闭（连接断开/错误）：退出内层循环，外层重连
					if !ok || msg == nil {
						logx.Errorf("[Scheduler] Pub/Sub channel closed, reconnecting")
						closePubsub()
						break consumeLoop
					}
					var signal CancelSignal
					if err := json.Unmarshal([]byte(msg.Payload), &signal); err != nil {
						// 回退：API 发布的是明文字符串（"STOP"/"PAUSE"），非 JSON
						taskId := strings.TrimPrefix(msg.Channel, "cscan:task:ctrl:")
						signal = CancelSignal{
							TaskId: taskId,
							Action: msg.Payload,
						}
						upper := strings.ToUpper(msg.Payload)
						if upper != "STOP" && upper != "PAUSE" {
							logx.Errorf("[Scheduler] Unrecognized task control payload: channel=%s, payload=%s", msg.Channel, msg.Payload)
						}
					}
					select {
					case ch <- &signal:
					default:
						// 通道满了，丢弃旧信号
					}
				}
			}

			// 断线后短暂退避再重连
			s.sleepCtx(ctx, backoff)
			backoff = s.nextBackoff(backoff, maxBackoff)
		}
	}()

	return ch
}

// sleepCtx 可被 ctx 取消的 sleep
func (s *Scheduler) sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// nextBackoff 指数退避，上限为 max
func (s *Scheduler) nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}
