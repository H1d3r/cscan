package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

// TaskRecoveryManager 任务恢复管理器
type TaskRecoveryManager struct {
	rdb                *redis.Client
	ctx                context.Context
	processingKey      string
	queueKey           string
	taskTimeoutKey     string
	taskWorkerKey      string
	workerHeartbeatKey string
	checkInterval      time.Duration
	taskTimeout        time.Duration
	logger             logx.Logger
	scheduler          *Scheduler // 用于恢复时复用 calculatePriorityScore，保留原任务优先级
}

// TaskExecutionInfo 任务执行信息
type TaskExecutionInfo struct {
	TaskId     string    `json:"taskId"`
	WorkerName string    `json:"workerName"`
	StartTime  time.Time `json:"startTime"`
	LastUpdate time.Time `json:"lastUpdate"`
	Phase      string    `json:"phase"`
	Progress   int       `json:"progress"`
	RetryCount int       `json:"retryCount"`
	MaxRetries int       `json:"maxRetries"`
}

// NewTaskRecoveryManager 创建任务恢复管理器
// scheduler 参数用于恢复任务时复用 calculatePriorityScore，保留原任务优先级
// 修复历史问题：原恢复时用 time.Now().Unix() 作 score，高优先级任务恢复后降级为 Normal
// 允许 scheduler 为 nil（向后兼容），此时退化为时间戳 score
// 超时自适应：CPU≤4 核 20min，≤8 核 15min，其余 10min
func NewTaskRecoveryManager(rdb *redis.Client, ctx context.Context, scheduler *Scheduler) *TaskRecoveryManager {
	cpuCores := runtime.NumCPU()
	var timeout time.Duration
	switch {
	case cpuCores <= 4:
		timeout = 20 * time.Minute
	case cpuCores <= 8:
		timeout = 15 * time.Minute
	default:
		timeout = 10 * time.Minute
	}

	return &TaskRecoveryManager{
		rdb:                rdb,
		ctx:                ctx,
		processingKey:      "cscan:task:processing",
		queueKey:           "cscan:task:queue",
		taskTimeoutKey:     "cscan:task:execution",
		taskWorkerKey:      "cscan:task:worker",
		workerHeartbeatKey: "cscan:worker:",
		checkInterval:      30 * time.Second, // 每30秒检查一次
		taskTimeout:        timeout,
		logger:             logx.WithContext(ctx),
		scheduler:          scheduler,
	}
}

// retryCountKey 返回任务重试计数的独立 Redis key
// 修复 M-04：原 RetryCount 存储在 execInfo JSON 中，recoverTask 的 GET→修改→SET
// 与 UpdateTaskProgress 的 GET→修改→SET 存在 lost-update 竞争：
// UpdateTaskProgress 最后 SET 时会用旧 RetryCount 覆盖 recoverTask 已递增的值。
// 现使用独立 Redis key + INCR 原子递增，避免 lost-update。
func (m *TaskRecoveryManager) retryCountKey(taskId string) string {
	return fmt.Sprintf("cscan:task:retry:%s", taskId)
}

// getRetryCount 从独立 key 读取重试计数（原子读取）
func (m *TaskRecoveryManager) getRetryCount(taskId string) int {
	val, err := m.rdb.Get(m.ctx, m.retryCountKey(taskId)).Result()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(val)
	return n
}

// resetRetryCount 重置重试计数（任务重新开始时调用）
func (m *TaskRecoveryManager) resetRetryCount(taskId string) {
	m.rdb.Set(m.ctx, m.retryCountKey(taskId), 0, 24*time.Hour)
}

// Start 启动任务恢复监控
func (m *TaskRecoveryManager) Start() {
	go m.monitorLoop()
	m.logger.Info("TaskRecoveryManager started")
}

// monitorLoop 监控循环
func (m *TaskRecoveryManager) monitorLoop() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			m.logger.Info("TaskRecoveryManager stopped")
			return
		case <-ticker.C:
			m.checkAndRecoverTasks()
		}
	}
}

// checkAndRecoverTasks 检查并恢复任务
func (m *TaskRecoveryManager) checkAndRecoverTasks() {
	// 获取所有处理中的任务
	taskIds, err := m.rdb.SMembers(m.ctx, m.processingKey).Result()
	if err != nil {
		m.logger.Errorf("Failed to get processing tasks: %v", err)
		return
	}

	if len(taskIds) == 0 {
		return
	}

	m.logger.Infof("Checking %d processing tasks for recovery", len(taskIds))

	for _, taskId := range taskIds {
		m.checkTask(taskId)
	}
}

// checkTask 检查单个任务
func (m *TaskRecoveryManager) checkTask(taskId string) {
	// 获取任务执行信息
	execInfo, err := m.getTaskExecutionInfo(taskId)
	if err != nil {
		m.logger.Errorf("Failed to get execution info for task %s: %v", taskId, err)
		return
	}

	// 如果没有执行信息：可能是任务刚弹出还没来得及写入（短暂窗口），
	// 也可能是 PopTask 默认路径脚本不持久化 execution 的历史 BUG 残留。
	// 修复 C-03：不能永久跳过，应该尝试从 taskInfo 补建 execution；若 taskInfo 也不存在则标记失败
	if execInfo == nil {
		taskInfo, infoErr := m.getTaskInfo(taskId)
		if infoErr == nil && taskInfo != nil {
			// taskInfo 存在但 execution 缺失：补建 execution info 并触发恢复
			m.logger.Infof("Task %s has taskInfo but no execution info, rebuilding execution info", taskId)
			// taskInfo.CreateTime 由 scheduler 写入，格式为 "2006-01-02 15:04:05"（本地时间）。
			// 此前按 RFC3339 解析恒失败，StartTime 永远落到 fallback，兼容两种格式
			var createTime time.Time
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", taskInfo.CreateTime, time.Local); err == nil {
				createTime = t
			} else if t, err := time.Parse(time.RFC3339, taskInfo.CreateTime); err == nil {
				createTime = t
			} else {
				createTime = time.Now().Add(-m.taskTimeout) // 解析失败按已超时处理
			}
			execInfo = &TaskExecutionInfo{
				TaskId:     taskId,
				WorkerName: "", // 未知 worker
				StartTime:  createTime,
				LastUpdate: time.Now(), // H-11 修复：原 LastUpdate=createTime 导致旧任务立即被判定超时，30s 后又触发，busy-loop 直到重试预算耗尽
				Phase:      "started",
				Progress:   0,
				RetryCount: 0,
				MaxRetries: 3,
			}
			// 持久化补建的 execution info
			if err := m.saveTaskExecutionInfo(taskId, execInfo); err != nil {
				m.logger.Errorf("Task %s failed to save rebuilt execution info: %v", taskId, err)
				return
			}
			// 落入下面的恢复逻辑
		} else {
			// taskInfo 也不存在（24h TTL 已过期或从未写入）：标记失败，避免永久卡死
			m.logger.Errorf("Task %s has no execution info and no taskInfo, marking as failed", taskId)
			m.markTaskFailed(taskId, "No execution info and no taskInfo (orphaned task)")
			return
		}
	}

	// 检查 Worker 是否在线
	// WorkerName 为空的 execInfo 来自补建路径（checkTask / UpdateTaskProgress），
	// isWorkerOnline("") 恒为 false，若据此判定离线会对实际仍在运行的任务误触发恢复（双跑）。
	// 未知 worker 时只能依赖 LastUpdate 超时判断存活
	workerOnline := true
	if execInfo.WorkerName != "" {
		workerOnline = m.isWorkerOnline(execInfo.WorkerName)
	}

	// 检查任务是否超时
	taskTimedOut := time.Since(execInfo.LastUpdate) > m.taskTimeout

	// 决定是否需要恢复
	needsRecovery := false
	reason := ""

	if !workerOnline {
		needsRecovery = true
		reason = fmt.Sprintf("Worker %s is offline", execInfo.WorkerName)
	} else if taskTimedOut {
		needsRecovery = true
		reason = fmt.Sprintf("Task timeout (no update for %v)", time.Since(execInfo.LastUpdate))
	}

	if needsRecovery {
		// Phase 4 可观测性埋点：分类 reason 为 label
		reasonLabel := "stale"
		if !workerOnline {
			reasonLabel = "heartbeat_lost"
		} else if taskTimedOut {
			reasonLabel = "orphaned"
		}
		m.logger.Infof("Task %s needs recovery: %s", taskId, reason)
		m.recoverTask(taskId, execInfo, reasonLabel)
	}
}

// recoverTask 恢复任务
func (m *TaskRecoveryManager) recoverTask(taskId string, execInfo *TaskExecutionInfo, reason string) {
	// 修复 M-04：从独立 Redis key 读取重试计数，避免与 UpdateTaskProgress 的 lost-update
	redisCount := m.getRetryCount(taskId)

	// 修复 H4：retry key 24h TTL 到期后 getRetryCount 返回 0,而 execInfo.RetryCount 可能 > 0
	//   场景:worker 拉起后首次失败 → retry INCR 到 1 → 24h 内 worker 永不再起 → TTL 过期
	//         下一轮 recovery 看到计数 0,会绕过 MaxRetries 防护无限重试
	//   取 max(redisCount, execInfo.RetryCount) 保留内存侧已知的计数下限
	//   execInfo 自身也有 1h TTL,但即便两次都过期,取 max 仍能阻止 TTL 清零造成的绕过
	if execInfo.RetryCount > redisCount {
		redisCount = execInfo.RetryCount
	}
	execInfo.RetryCount = redisCount

	// 检查重试次数
	if execInfo.RetryCount >= execInfo.MaxRetries {
		m.logger.Errorf("Task %s exceeded max retries (%d), marking as failed", taskId, execInfo.MaxRetries)
		m.markTaskFailed(taskId, fmt.Sprintf("Exceeded max retries: %s", reason))
		return
	}

	// 修复 M-04：使用 Redis INCR 原子递增重试计数，避免并发覆盖
	// 修复 H4：INCR 前若 redisCount 与 execInfo 不一致(说明 redis 侧已被 TTL 清零),用 SET 显式对齐再 INCR,
	//   防止 INCR 在 0 基础上 +1 造成"首次失败"假象绕过计数。仅在 redis 侧 < 内存侧时对齐。
	if redisCountFromExec := execInfo.RetryCount; redisCountFromExec > 0 {
		if current, _ := m.rdb.Get(m.ctx, m.retryCountKey(taskId)).Result(); current == "" || current == "0" {
			m.rdb.Set(m.ctx, m.retryCountKey(taskId), redisCountFromExec, 24*time.Hour)
		}
	}
	newCount, err := m.rdb.Incr(m.ctx, m.retryCountKey(taskId)).Result()
	if err != nil {
		m.logger.Errorf("Failed to INCR retry count for task %s: %v, using local increment", taskId, err)
		execInfo.RetryCount++ // fallback
	} else {
		execInfo.RetryCount = int(newCount)
	}
	// 刷新 TTL，避免计数永久残留
	m.rdb.Expire(m.ctx, m.retryCountKey(taskId), 24*time.Hour)

	// 获取原始任务信息
	taskInfo, err := m.getTaskInfo(taskId)
	if err != nil {
		m.logger.Errorf("Failed to get task info for %s: %v", taskId, err)
		m.markTaskFailed(taskId, fmt.Sprintf("Failed to get task info: %v", err))
		return
	}

	taskData, err := json.Marshal(taskInfo)
	if err != nil {
		m.logger.Errorf("Failed to marshal task %s: %v", taskId, err)
		m.markTaskFailed(taskId, fmt.Sprintf("Failed to marshal task: %v", err))
		return
	}

	// 根据任务类型选择队列：原指定 Worker 离线时回退到公共队列，避免任务沉淀在已死亡 Worker 的专属队列
	var queueKey string
	if len(taskInfo.Workers) > 0 && m.isWorkerOnline(taskInfo.Workers[0]) {
		// 修复大小写不一致：scheduler.GetWorkerQueueKey 使用 strings.ToLower，
		// recovery 必须保持一致，否则 Worker 名含大写时任务写入与弹出 Key 不匹配
		if m.scheduler != nil {
			queueKey = m.scheduler.GetWorkerQueueKey(taskInfo.Workers[0])
		} else {
			queueKey = fmt.Sprintf("cscan:task:queue:worker:%s", strings.ToLower(taskInfo.Workers[0]))
		}
	} else if m.scheduler != nil && m.scheduler.IsPriorityBucketEnabled() {
		// 修复 C1：分桶模式下恢复任务必须路由到对应优先级桶，否则 CheckTask 无法消费
		// 原逻辑回退到 m.queueKey（"cscan:task:queue"），但 popFromBuckets 不读此 key
		// 导致任务在 24h taskInfo TTL 过期前永远不被消费，等同丢失
		queueKey = m.scheduler.BucketKey(taskInfo.Priority)
	} else {
		queueKey = m.queueKey
	}

	// 计算恢复后的优先级分数
	// 修复历史问题：原用 time.Now().Unix()，高优先级任务恢复后降级为 Normal
	// 现复用 scheduler.calculatePriorityScore 保留原优先级
	// 若 scheduler 为 nil（向后兼容场景），退化为原行为
	var score float64
	if m.scheduler != nil {
		score = m.scheduler.calculatePriorityScore(taskInfo.Priority, time.Now())
	} else {
		score = float64(time.Now().Unix())
	}

	// Lua 脚本：原子化重排队，顺序为「先 ZADD 回队列 → 刷新 taskInfo TTL → 再 SRem processing」
	// 修复历史问题：原顺序为「先 SRem 再 ZAdd」，若中间崩溃任务会从 processing 移除但不在队列中，等同丢失
	// 现调整为「先 ZAdd 再 SRem」，即使脚本中途失败任务也在队列里可被重新消费
	// 同时补偿 taskInfo TTL：恢复时若 taskInfo 已接近过期，重新 SET 24h TTL，避免下次恢复时无法读取
	//
	// 关于双消费窗口（review C5）:
	//   - 整个脚本由单条 Lua 原子执行,worker 无法在 ZADD 与 SREM 之间插入执行,不存在"队列 + processing 共存"窗口
	//   - pop 脚本自身 SADD processing 幂等,即使 processing 仍残留旧条目也无副作用
	//   - 保留 SREM 是刻意为之:若仅由 pop SADD,过期但 worker 仍缓慢执行中的任务将永久残留 processing,下一次恢复循环
	//     会再次看到 LastUpdate 旧戳并再次 ZADD,反而构成真实双重执行。原子 SREM 让本轮回退出 processing,
	//     后续若被 worker pop 则重新 SADD,这是唯一安全的 ownership 转移点
	taskInfoKey := fmt.Sprintf("cscan:task:info:%s", taskId)
	script := redis.NewScript(`
		-- 1. 先 ZADD 回队列：即使后续步骤崩溃，任务也在队列里可被重新消费
		redis.call('ZADD', KEYS[2], ARGV[2], ARGV[3])
		-- 2. 补偿：刷新 taskInfo TTL 为 24h（用 ARGV[3] 同样的数据，避免恢复后过期导致下次恢复失败）
		redis.call('SET', KEYS[3], ARGV[3], 'EX', ARGV[4])
		-- 3. 最后 SRem processing：原子收尾,转移 ownership 给下一轮 pop
		redis.call('SREM', KEYS[1], ARGV[1])
		return 1
	`)
	err = script.Run(m.ctx, m.rdb, []string{m.processingKey, queueKey, taskInfoKey},
		taskId, score, string(taskData), 86400).Err()

	if err != nil {
		m.logger.Errorf("Failed to atomically requeue task %s: %v", taskId, err)
		return
	}

	// 更新执行信息
	execInfo.LastUpdate = time.Now()
	m.saveTaskExecutionInfo(taskId, execInfo)

	m.logger.Infof("Task %s recovered and requeued (retry %d/%d), reason: %s",
		taskId, execInfo.RetryCount, execInfo.MaxRetries, reason)
}

// RecordTaskStart 记录任务开始执行
func (m *TaskRecoveryManager) RecordTaskStart(taskId, workerName string) error {
	execInfo := &TaskExecutionInfo{
		TaskId:     taskId,
		WorkerName: workerName,
		StartTime:  time.Now(),
		LastUpdate: time.Now(),
		Phase:      "started",
		Progress:   0,
		RetryCount: 0,
		MaxRetries: 3, // 默认最多重试3次
	}

	// 修复 M-04：重置独立重试计数 key
	m.resetRetryCount(taskId)

	return m.saveTaskExecutionInfo(taskId, execInfo)
}

// UpdateTaskProgress 更新任务进度
func (m *TaskRecoveryManager) UpdateTaskProgress(taskId, phase string, progress int) error {
	execInfo, err := m.getTaskExecutionInfo(taskId)
	if err != nil || execInfo == nil {
		// 如果没有执行信息，创建一个
		execInfo = &TaskExecutionInfo{
			TaskId:     taskId,
			StartTime:  time.Now(),
			MaxRetries: 3,
		}
	}

	execInfo.LastUpdate = time.Now()
	execInfo.Phase = phase
	execInfo.Progress = progress

	return m.saveTaskExecutionInfo(taskId, execInfo)
}

// RemoveTaskExecution 移除任务执行记录
func (m *TaskRecoveryManager) RemoveTaskExecution(taskId string) error {
	key := fmt.Sprintf("%s:%s", m.taskTimeoutKey, taskId)
	return m.rdb.Del(m.ctx, key).Err()
}

// getTaskExecutionInfo 获取任务执行信息
func (m *TaskRecoveryManager) getTaskExecutionInfo(taskId string) (*TaskExecutionInfo, error) {
	key := fmt.Sprintf("%s:%s", m.taskTimeoutKey, taskId)
	data, err := m.rdb.Get(m.ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var execInfo TaskExecutionInfo
	if err := json.Unmarshal([]byte(data), &execInfo); err != nil {
		return nil, err
	}

	return &execInfo, nil
}

// saveTaskExecutionInfo 保存任务执行信息
func (m *TaskRecoveryManager) saveTaskExecutionInfo(taskId string, execInfo *TaskExecutionInfo) error {
	key := fmt.Sprintf("%s:%s", m.taskTimeoutKey, taskId)
	data, err := json.Marshal(execInfo)
	if err != nil {
		return err
	}

	// 设置过期时间为1小时
	return m.rdb.Set(m.ctx, key, data, time.Hour).Err()
}

// getTaskInfo 获取任务信息
func (m *TaskRecoveryManager) getTaskInfo(taskId string) (*TaskInfo, error) {
	key := fmt.Sprintf("cscan:task:info:%s", taskId)
	data, err := m.rdb.Get(m.ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var taskInfo TaskInfo
	if err := json.Unmarshal([]byte(data), &taskInfo); err != nil {
		return nil, err
	}

	return &taskInfo, nil
}

// isWorkerOnline 检查 Worker 是否在线
func (m *TaskRecoveryManager) isWorkerOnline(workerName string) bool {
	key := fmt.Sprintf("%s%s", m.workerHeartbeatKey, workerName)
	exists, err := m.rdb.Exists(m.ctx, key).Result()
	if err != nil {
		return false
	}
	return exists > 0
}

// markTaskFailed 标记任务失败
func (m *TaskRecoveryManager) markTaskFailed(taskId, reason string) {
	// 从处理中集合移除
	if err := m.rdb.SRem(m.ctx, m.processingKey, taskId).Err(); err != nil {
		m.logger.Errorf("Failed to remove task %s from processing set: %v", taskId, err)
	}

	// 更新任务状态
	statusKey := fmt.Sprintf("cscan:task:status:%s", taskId)
	statusData := map[string]interface{}{
		"taskId": taskId,
		"state":  "FAILURE",
		"result": reason,
	}
	statusJson, err := json.Marshal(statusData)
	if err != nil {
		m.logger.Errorf("Failed to marshal status for task %s: %v", taskId, err)
		return
	}
	if err := m.rdb.Set(m.ctx, statusKey, statusJson, 24*time.Hour).Err(); err != nil {
		m.logger.Errorf("Failed to set status for task %s: %v", taskId, err)
	}

	// 移除执行信息
	m.RemoveTaskExecution(taskId)

	m.logger.Infof("Task %s marked as failed: %s", taskId, reason)
}

// GetRecoveryStats 获取恢复统计信息
func (m *TaskRecoveryManager) GetRecoveryStats() map[string]interface{} {
	processingCount, _ := m.rdb.SCard(m.ctx, m.processingKey).Result()

	return map[string]interface{}{
		"processingTasks": processingCount,
		"checkInterval":   m.checkInterval.String(),
		"taskTimeout":     m.taskTimeout.String(),
	}
}
