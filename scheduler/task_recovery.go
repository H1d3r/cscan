package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
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
func NewTaskRecoveryManager(rdb *redis.Client, ctx context.Context) *TaskRecoveryManager {
	// 根据系统配置自适应调整任务超时时间
	// 低配机器扫描速度慢，需要更长的超时时间避免误判超时
	taskTimeout := adaptiveTaskTimeout()

	return &TaskRecoveryManager{
		rdb:                rdb,
		ctx:                ctx,
		processingKey:      "cscan:task:processing",
		queueKey:           "cscan:task:queue",
		taskTimeoutKey:     "cscan:task:execution",
		taskWorkerKey:      "cscan:task:worker",
		workerHeartbeatKey: "cscan:worker:",
		checkInterval:      30 * time.Second, // 每30秒检查一次
		taskTimeout:        taskTimeout,      // 自适应超时
		logger:             logx.WithContext(ctx),
	}
}

// adaptiveTaskTimeout 根据系统硬件配置计算任务超时时间
func adaptiveTaskTimeout() time.Duration {
	cpuCores := runtime.NumCPU()
	if cpuCores <= 4 {
		return 20 * time.Minute // 低配：20分钟
	}
	if cpuCores <= 8 {
		return 15 * time.Minute // 中配：15分钟
	}
	return 10 * time.Minute // 高配：10分钟（原始值）
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

	// 如果没有执行信息，说明任务刚开始，给它一些时间
	if execInfo == nil {
		m.logger.Infof("Task %s has no execution info yet, skipping", taskId)
		return
	}

	// 检查 Worker 是否在线
	workerOnline := m.isWorkerOnline(execInfo.WorkerName)

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
		m.logger.Infof("Task %s needs recovery: %s", taskId, reason)
		m.recoverTask(taskId, execInfo, reason)
	}
}

// recoverTask 恢复任务
func (m *TaskRecoveryManager) recoverTask(taskId string, execInfo *TaskExecutionInfo, reason string) {
	// 检查重试次数
	if execInfo.RetryCount >= execInfo.MaxRetries {
		m.logger.Errorf("Task %s exceeded max retries (%d), marking as failed", taskId, execInfo.MaxRetries)
		m.markTaskFailed(taskId, fmt.Sprintf("Exceeded max retries: %s", reason))
		return
	}

	// 增加重试次数
	execInfo.RetryCount++

	// 获取原始任务信息
	taskInfo, err := m.getTaskInfo(taskId)
	if err != nil {
		m.logger.Errorf("Failed to get task info for %s: %v", taskId, err)
		m.markTaskFailed(taskId, fmt.Sprintf("Failed to get task info: %v", err))
		return
	}

	// 从处理中集合移除
	m.rdb.SRem(m.ctx, m.processingKey, taskId)

	// 重新放回队列
	score := float64(time.Now().Unix())
	taskData, _ := json.Marshal(taskInfo)

	// 根据任务类型选择队列
	var queueKey string
	if len(taskInfo.Workers) > 0 {
		// 如果指定了 Worker，放回专属队列
		queueKey = fmt.Sprintf("cscan:task:queue:worker:%s", taskInfo.Workers[0])
	} else {
		// 否则放回公共队列
		queueKey = m.queueKey
	}

	err = m.rdb.ZAdd(m.ctx, queueKey, redis.Z{
		Score:  score,
		Member: taskData,
	}).Err()

	if err != nil {
		m.logger.Errorf("Failed to requeue task %s: %v", taskId, err)
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
	m.rdb.SRem(m.ctx, m.processingKey, taskId)

	// 更新任务状态
	statusKey := fmt.Sprintf("cscan:task:status:%s", taskId)
	statusData := map[string]interface{}{
		"taskId": taskId,
		"state":  "FAILURE",
		"result": reason,
	}
	statusJson, _ := json.Marshal(statusData)
	m.rdb.Set(m.ctx, statusKey, statusJson, 24*time.Hour)

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
