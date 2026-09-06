package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

// TaskRecoveryManager 任务恢复管理器。
type TaskRecoveryManager struct {
	rdb                *redis.Client
	ctx                context.Context
	processingKey      string
	taskTimeoutKey     string
	workerHeartbeatKey string
	checkInterval      time.Duration
	taskTimeout        time.Duration
	logger             logx.Logger
	scheduler          *Scheduler
}

// TaskExecutionInfo 任务执行信息。LeaseToken 标识本次 pop 的唯一所有权。
type TaskExecutionInfo struct {
	TaskId             string    `json:"taskId"`
	WorkerName         string    `json:"workerName"`
	InstanceID         string    `json:"instanceId"`
	TaskProtocol       int       `json:"taskProtocol"`
	LeaseToken         string    `json:"leaseToken"`
	DispatchGeneration string    `json:"dispatchGeneration"`
	StartTime          time.Time `json:"startTime"`
	LastUpdate         time.Time `json:"lastUpdate"`
	LastUpdateUnix     int64     `json:"lastUpdateUnix"`
	Phase              string    `json:"phase"`
	Progress           int       `json:"progress"`
	RetryCount         int       `json:"retryCount"`
	MaxRetries         int       `json:"maxRetries"`
}

// NewTaskRecoveryManager 创建任务恢复管理器。
// scheduler 用于执行带 lease 校验的原子状态迁移；传 nil 时基于同一 Redis
// 客户端创建一个实例，绝不退化为无所有权校验的恢复路径。
// 超时自适应：CPU≤4 核 20min，≤8 核 15min，其余 10min。
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
	if scheduler == nil {
		scheduler = NewScheduler(rdb)
	}

	return &TaskRecoveryManager{
		rdb:                rdb,
		ctx:                ctx,
		processingKey:      "cscan:task:processing",
		taskTimeoutKey:     "cscan:task:execution",
		workerHeartbeatKey: "cscan:worker:instance:",
		checkInterval:      30 * time.Second,
		taskTimeout:        timeout,
		logger:             logx.WithContext(ctx),
		scheduler:          scheduler,
	}
}

func (m *TaskRecoveryManager) retryCountKey(taskID string) string {
	return fmt.Sprintf("cscan:task:retry:%s", taskID)
}

func (m *TaskRecoveryManager) getRetryCount(taskID string) int {
	val, err := m.rdb.Get(m.ctx, m.retryCountKey(taskID)).Result()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(val)
	return n
}

// Start 启动任务恢复监控。
func (m *TaskRecoveryManager) Start() {
	go m.monitorLoop()
	m.logger.Info("TaskRecoveryManager started")
}

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

func (m *TaskRecoveryManager) checkAndRecoverTasks() {
	taskIDs, err := m.rdb.SMembers(m.ctx, m.processingKey).Result()
	if err != nil {
		m.logger.Errorf("Failed to get processing tasks: %v", err)
		return
	}
	if len(taskIDs) == 0 {
		return
	}

	m.logger.Infof("Checking %d processing tasks for recovery", len(taskIDs))
	for _, taskID := range taskIDs {
		m.checkTask(taskID)
	}
}

func (m *TaskRecoveryManager) checkTask(taskID string) {
	execInfo, err := m.getTaskExecutionInfo(taskID)
	if err != nil {
		m.logger.Errorf("Failed to get execution info for task %s: %v", taskID, err)
		return
	}

	if execInfo == nil {
		// A modern pop writes processing, taskInfo, and execution in one Lua call.
		// Never reconstruct an execution without its acquisition token: doing so
		// would let a stale observer release a newer generation. Only a marker for
		// which both ownership records are still absent may be removed.
		_, infoErr := m.getTaskInfo(taskID)
		if infoErr == redis.Nil {
			cleaned, cleanupErr := m.scheduler.CleanupUnownedProcessingTask(m.ctx, taskID, "")
			if cleanupErr != nil {
				m.logger.Errorf("Failed to clean unowned processing task %s: %v", taskID, cleanupErr)
			} else if cleaned {
				m.logger.Errorf("Removed orphaned processing marker for task %s with no payload or execution", taskID)
			}
			return
		}
		if infoErr != nil {
			m.logger.Errorf("Failed to inspect task info for execution-less task %s: %v", taskID, infoErr)
			return
		}
		m.logger.Errorf("Task %s has payload but no execution lease; refusing generation-blind recovery", taskID)
		return
	}
	if execInfo.LeaseToken == "" || execInfo.DispatchGeneration == "" ||
		execInfo.TaskProtocol != TaskProtocolV1 || execInfo.InstanceID == "" {
		m.logger.Errorf("Task %s has legacy or incomplete execution ownership; refusing generation-blind recovery", taskID)
		return
	}
	if execInfo.Phase == "pausing" {
		// Completing or rolling back a pause requires durable Mongo evidence and
		// parent-generation state. The grouped API recovery path is the sole
		// authority for this transition.
		m.logger.Infof("Task %s is pausing; deferring to durable API recovery", taskID)
		return
	}

	workerOnline := m.isWorkerInstanceOnline(execInfo.InstanceID)
	if workerOnline {
		// A live v1 process generation is never recovered, even if ancillary
		// timestamps are stale. Lease renewal and the instance heartbeat are the
		// two independent liveness signals; ownership transfer requires heartbeat
		// absence and is rechecked atomically during requeue.
		return
	}

	reason := "heartbeat_lost"
	m.logger.Infof("Task %s needs recovery: %s", taskID, reason)
	m.recoverTask(taskID, execInfo, reason)
}

// recoverTask requeues only the exact payload and lease generation observed by
// the recovery pass. RequeueExactTask revalidates ownership and releases it in
// the same Lua commit.
func (m *TaskRecoveryManager) recoverTask(taskID string, execInfo *TaskExecutionInfo, reason string) {
	if execInfo == nil || execInfo.LeaseToken == "" {
		m.logger.Errorf("Task %s recovery is missing its lease token", taskID)
		return
	}

	retryCount := m.getRetryCount(taskID)
	if execInfo.RetryCount > retryCount {
		retryCount = execInfo.RetryCount
	}
	maxRetries := execInfo.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if retryCount >= maxRetries {
		m.logger.Errorf("Task %s exceeded max retries (%d), marking its current lease as failed", taskID, maxRetries)
		m.markTaskFailed(taskID, execInfo.LeaseToken, fmt.Sprintf("Exceeded max retries: %s", reason))
		return
	}

	taskInfo, err := m.getTaskInfo(taskID)
	if err != nil {
		m.logger.Errorf("Failed to get task info for %s: %v", taskID, err)
		m.markTaskFailed(taskID, execInfo.LeaseToken, fmt.Sprintf("Failed to get task info: %v", err))
		return
	}
	if taskInfo.TaskId != taskID || taskInfo.DispatchGeneration == "" ||
		taskInfo.DispatchGeneration != execInfo.DispatchGeneration {
		m.logger.Errorf("Task %s payload does not match its execution dispatch generation; refusing recovery", taskID)
		return
	}
	taskInfo.LeaseToken = execInfo.LeaseToken
	taskInfo.RecoveryInstanceID = execInfo.InstanceID

	moved, err := m.scheduler.RequeueExactTask(m.ctx, taskInfo)
	if err != nil {
		if errors.Is(err, ErrTaskLeaseConflict) {
			m.logger.Infof("Skipped stale recovery for task %s because lease ownership changed", taskID)
			return
		}
		m.logger.Errorf("Failed to atomically requeue task %s: %v", taskID, err)
		return
	}
	if !moved {
		m.logger.Errorf("Task %s recovery did not commit", taskID)
		return
	}

	newCount, err := m.rdb.Incr(m.ctx, m.retryCountKey(taskID)).Result()
	if err != nil {
		m.logger.Errorf("Task %s was requeued but its retry counter could not be incremented: %v", taskID, err)
		newCount = int64(retryCount + 1)
	} else if err := m.rdb.Expire(m.ctx, m.retryCountKey(taskID), 24*time.Hour).Err(); err != nil {
		m.logger.Errorf("Failed to refresh retry counter TTL for task %s: %v", taskID, err)
	}

	m.logger.Infof("Task %s recovered and requeued (retry %d/%d), reason: %s", taskID, newCount, maxRetries, reason)
}

func (m *TaskRecoveryManager) getTaskExecutionInfo(taskID string) (*TaskExecutionInfo, error) {
	key := fmt.Sprintf("%s:%s", m.taskTimeoutKey, taskID)
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

func (m *TaskRecoveryManager) getTaskInfo(taskID string) (*TaskInfo, error) {
	data, err := m.rdb.Get(m.ctx, taskInfoKeyPrefix+taskID).Result()
	if err != nil {
		return nil, err
	}

	var taskInfo TaskInfo
	if err := json.Unmarshal([]byte(data), &taskInfo); err != nil {
		return nil, err
	}
	return &taskInfo, nil
}

func (m *TaskRecoveryManager) isWorkerInstanceOnline(instanceID string) bool {
	exists, err := m.rdb.Exists(m.ctx, m.workerHeartbeatKey+instanceID).Result()
	return err == nil && exists > 0
}

// markTaskFailed deliberately leaves Redis ownership untouched. This legacy
// Redis-only recovery manager has no durable parent model, so releasing a
// child here could strand a STARTED Mongo parent. The API recovery cadence will
// resolve the exact lease and durable parent together.
func (m *TaskRecoveryManager) markTaskFailed(taskID, leaseToken, reason string) {
	if leaseToken == "" {
		m.logger.Errorf("Refusing to mark task %s failed without a lease token", taskID)
		return
	}
	m.logger.Errorf("Task %s requires durable failure recovery; leaving exact lease unchanged: %s", taskID, reason)
}

// GetRecoveryStats 获取恢复统计信息。
func (m *TaskRecoveryManager) GetRecoveryStats() map[string]interface{} {
	processingCount, _ := m.rdb.SCard(m.ctx, m.processingKey).Result()
	return map[string]interface{}{
		"processingTasks": processingCount,
		"checkInterval":   m.checkInterval.String(),
		"taskTimeout":     m.taskTimeout.String(),
	}
}
