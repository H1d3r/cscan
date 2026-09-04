package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cscan/internal/model"
	"cscan/internal/notification"
	"cscan/internal/scheduler"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// SchedulerClient Worker 直连 Redis 调度层客户端
// 替代原 HTTP→API→RPC 链路，直接操作 Redis 完成任务拉取/心跳/进度上报
type SchedulerClient struct {
	rdb         *redis.Client
	workerName  string
	scheduler   *scheduler.Scheduler
	recoveryMgr *scheduler.TaskRecoveryManager
	mongoDB     *mongo.Database
	notifySvc   *notification.Service // 通知服务（任务完成/失败触发通知）
}

// NewSchedulerClient 创建调度客户端
// mongoDB 仅用于 MainTask 状态写（IncrSubTaskDone / current_phase / progress /
// MarkTaskCompleted），应传入独立状态连接池（独立持久化），与结果数据写隔离，
// 避免数据写入过载时状态回写被饿死。
func NewSchedulerClient(rdb *redis.Client, workerName string, mongoDB *mongo.Database) *SchedulerClient {
	ctx := context.Background()
	s := scheduler.NewScheduler(rdb)
	recoveryMgr := scheduler.NewTaskRecoveryManager(rdb, ctx, s)

	return &SchedulerClient{
		rdb:         rdb,
		workerName:  workerName,
		scheduler:   s,
		recoveryMgr: recoveryMgr,
		mongoDB:     mongoDB,
	}
}

// CheckTaskResponse 拉取任务响应（对齐原 HTTP TaskCheckResp）
type CheckTaskResponse struct {
	IsExist    bool
	IsFinished bool
	TaskId     string
	MainTaskId string
	Config     string
}

// CheckTask 拉取任务（Lua 原子出队 + Pub/Sub 长轮询）
// 复用 scheduler.PopTaskForWorker(workerName)，队空则 subscribe cscan:task:available 最长 25 秒
func (c *SchedulerClient) CheckTask(ctx context.Context) (*CheckTaskResponse, error) {
	// 1. 尝试原子弹出
	task, err := c.scheduler.PopTaskForWorker(ctx, c.workerName)
	if err != nil {
		return nil, fmt.Errorf("PopTaskForWorker: %w", err)
	}

	if task != nil {
		// 记录任务开始执行（写 cscan:task:execution:{taskId}）
		if err := c.recoveryMgr.RecordTaskStart(task.TaskId, c.workerName); err != nil {
			logx.Errorf("[SchedulerClient] RecordTaskStart failed: %v", err)
		}

		// 将主任务状态置 STARTED（写 MongoDB）
		c.updateMainTaskStatus(ctx, task.MainTaskId, model.TaskStatusStarted, "")

		return &CheckTaskResponse{
			IsExist:    true,
			IsFinished: false,
			TaskId:     task.TaskId,
			MainTaskId: task.MainTaskId,
			Config:     task.Config,
		}, nil
	}

	// 2. 队列空，Pub/Sub 长轮询等待（最长 25 秒）
	if err := c.waitForTaskAvailable(ctx, 25*time.Second); err != nil {
		return nil, err
	}

	// 3. 被唤醒后再次尝试弹出
	task, err = c.scheduler.PopTaskForWorker(ctx, c.workerName)
	if err != nil {
		return nil, fmt.Errorf("PopTaskForWorker (retry): %w", err)
	}

	if task != nil {
		if err := c.recoveryMgr.RecordTaskStart(task.TaskId, c.workerName); err != nil {
			logx.Errorf("[SchedulerClient] RecordTaskStart failed: %v", err)
		}
		c.updateMainTaskStatus(ctx, task.MainTaskId, model.TaskStatusStarted, "")

		return &CheckTaskResponse{
			IsExist:    true,
			IsFinished: false,
			TaskId:     task.TaskId,
			MainTaskId: task.MainTaskId,
			Config:     task.Config,
		}, nil
	}

	return &CheckTaskResponse{IsExist: false}, nil
}

// waitForTaskAvailable 订阅 cscan:task:available 频道，最长等待指定时长
func (c *SchedulerClient) waitForTaskAvailable(ctx context.Context, timeout time.Duration) error {
	pubsub := c.rdb.Subscribe(ctx, "cscan:task:available")
	defer pubsub.Close()

	// 等待订阅确认
	if _, err := pubsub.Receive(ctx); err != nil {
		return fmt.Errorf("subscribe task:available: %w", err)
	}

	ch := pubsub.Channel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return nil // 超时返回，由调用方决定后续行为
	case <-ch:
		return nil // 被唤醒
	}
}

// UpdateTask 更新任务状态
// 写 Redis status/progress + 终态清理 + MongoDB 主任务更新
func (c *SchedulerClient) UpdateTask(ctx context.Context, taskID, state string, progress int, phase string) error {
	var firstErr error

	// 更新恢复管理器进度
	if phase != "" {
		if err := c.recoveryMgr.UpdateTaskProgress(taskID, phase, progress); err != nil {
			logx.Errorf("[SchedulerClient] UpdateTaskProgress failed: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// 从 processing 集合移除（终态时）
	if state == scheduler.TaskStatusSuccess || state == scheduler.TaskStatusPartial || state == scheduler.TaskStatusFailure {
		if err := c.rdb.SRem(ctx, "cscan:task:processing", taskID).Err(); err != nil {
			logx.Errorf("[SchedulerClient] SRem processing failed: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// 写 Redis status（24h TTL）
	statusKey := "cscan:task:status:" + taskID
	statusData := map[string]interface{}{
		"taskId": taskID,
		"state":  state,
		"worker": c.workerName,
	}
	statusJson, _ := json.Marshal(statusData)
	if err := c.rdb.Set(ctx, statusKey, statusJson, 24*time.Hour).Err(); err != nil {
		logx.Errorf("[SchedulerClient] Set status failed: %v", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	// 写 Redis progress（24h TTL）
	if phase != "" {
		progressKey := "cscan:task:progress:" + taskID
		progressData := map[string]interface{}{
			"currentPhase": phase,
		}
		progressJson, _ := json.Marshal(progressData)
		if err := c.rdb.Set(ctx, progressKey, progressJson, 24*time.Hour).Err(); err != nil {
			logx.Errorf("[SchedulerClient] Set progress failed: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// 终态：清理 execution info + taskInfo + 加入 completed 集合
	if state == scheduler.TaskStatusSuccess || state == scheduler.TaskStatusPartial || state == scheduler.TaskStatusFailure {
		c.recoveryMgr.RemoveTaskExecution(taskID)

		taskInfoKey := "cscan:task:info:" + taskID
		// 读取 taskInfo 用于 MongoDB 更新
		taskInfoData, _ := c.rdb.Get(ctx, taskInfoKey).Result()
		c.rdb.Del(ctx, taskInfoKey)

		completedKey := "cscan:task:completed"
		taskInfo := scheduler.TaskInfo{TaskId: taskID}
		taskJson, _ := json.Marshal(taskInfo)
		c.rdb.SAdd(ctx, completedKey, string(taskJson))

		// MongoDB 主任务更新
		c.updateMainTaskFromTaskInfo(ctx, taskInfoData, state, phase)
	} else if state != "" {
		// 非终态（如 STARTED、PAUSED）：从 taskInfo 读取 mainTaskId 更新 MongoDB
		taskInfoKey := "cscan:task:info:" + taskID
		taskInfoData, _ := c.rdb.Get(ctx, taskInfoKey).Result()
		c.updateMainTaskFromTaskInfo(ctx, taskInfoData, state, phase)
	}

	return firstErr
}

// KeepAlive 心跳（对齐原 RPC KeepAlive）
// 直接写 cscan:worker:{name}（60s TTL）+ SADD cscan:workers
func (c *SchedulerClient) KeepAlive(ctx context.Context, cpuLoad, memUsed float64, taskStarted, taskExecuted int, concurrency int) error {
	workerKey := "cscan:worker:" + c.workerName
	workerData := map[string]interface{}{
		"workerName":         c.workerName,
		"cpuLoad":            cpuLoad,
		"memUsed":            memUsed,
		"taskStartedNumber":  taskStarted,
		"taskExecutedNumber": taskExecuted,
		"isDaemon":           false,
		"concurrency":        concurrency,
		"updateTime":         time.Now().Format("2006-01-02 15:04:05"),
		"status":             "online",
	}
	workerJson, _ := json.Marshal(workerData)
	if err := c.rdb.Set(ctx, workerKey, workerJson, 60*time.Second).Err(); err != nil {
		return fmt.Errorf("set worker heartbeat: %w", err)
	}

	// 添加到 Worker 集合
	c.rdb.SAdd(ctx, "cscan:workers", c.workerName)

	return nil
}

// KeepAliveResponse 心跳响应（对齐原 HTTP HeartbeatResp）
type KeepAliveResponse struct {
	ManualStopFlag     bool
	ManualReloadFlag   bool
	DesiredConcurrency int
}

// KeepAliveWithResponse 心跳并返回控制命令
func (c *SchedulerClient) KeepAliveWithResponse(ctx context.Context, cpuLoad, memUsed float64, taskStarted, taskExecuted int, concurrency int) (*KeepAliveResponse, error) {
	if err := c.KeepAlive(ctx, cpuLoad, memUsed, taskStarted, taskExecuted, concurrency); err != nil {
		return nil, err
	}

	resp := &KeepAliveResponse{}

	// 检查控制命令
	controlKey := "cscan:worker:control:" + c.workerName
	controlData, err := c.rdb.Get(ctx, controlKey).Result()
	if err == nil && controlData != "" {
		var control map[string]bool
		if json.Unmarshal([]byte(controlData), &control) == nil {
			resp.ManualStopFlag = control["stop"]
			resp.ManualReloadFlag = control["reload"]
		}
		c.rdb.Del(ctx, controlKey)
	}

	// 读取期望并发数
	desiredKey := "cscan:worker:desired_concurrency:" + c.workerName
	if val, err := c.rdb.Get(ctx, desiredKey).Int(); err == nil && val > 0 {
		resp.DesiredConcurrency = val
	}

	return resp, nil
}

// IncrSubTaskDoneResponse 子任务完成响应
type IncrSubTaskDoneResponse struct {
	SubTaskDone         int
	SubTaskCount        int
	AllDone             bool
	Recorded            bool
	Finalized           bool
	FinalizationPending bool
	ScanSummary         *model.TaskScanSummary
}

// IncrSubTaskDone 递增子任务完成数
// 原子递增 MongoDB MainTaskModel.IncrSubTaskDoneAtomic + 更新 progress / current_phase
// 全部完成时 MarkTaskCompleted
func (c *SchedulerClient) IncrSubTaskDone(ctx context.Context, mainTaskID, subTaskID, moduleName string, incrAmount int, phaseResult *PhaseResult) (*IncrSubTaskDoneResponse, error) {
	if mainTaskID == "" {
		return &IncrSubTaskDoneResponse{SubTaskDone: 1, SubTaskCount: 1, AllDone: true, Recorded: true, Finalized: true}, nil
	}

	// 快速验证类任务（UUID 格式）跳过 MongoDB 操作
	if !isValidObjectID(mainTaskID) {
		return &IncrSubTaskDoneResponse{SubTaskDone: 1, SubTaskCount: 1, AllDone: true, Recorded: true, Finalized: true}, nil
	}

	taskModel := model.NewMainTaskModel(c.mongoDB)

	if incrAmount <= 0 {
		incrAmount = 1
	}
	canonicalPhase := canonicalTaskPhase(moduleName)
	if phaseResult == nil {
		defaultResult := missingPhaseResult(moduleName)
		phaseResult = &defaultResult
	} else if phaseResult.Phase == "" {
		phaseResult.Phase = canonicalPhase
	}
	phaseSummary := phaseResult.TaskSummary(subTaskID)
	phaseSummary.Weight = incrAmount
	reportKey := model.TaskPhaseReportKey(subTaskID, canonicalPhase)
	task, recorded, err := taskModel.RecordPhaseSummaryAtomic(ctx, mainTaskID, reportKey, phaseSummary, incrAmount)
	if err != nil {
		return nil, fmt.Errorf("RecordPhaseSummaryAtomic: %w", err)
	}

	if phaseSummary.Weight != incrAmount {
		phaseSummary.Weight = incrAmount
	}
	logx.Debugf("[SchedulerClient] phase report acknowledged, mainTaskId=%s subTaskId=%s phase=%s",
		mainTaskID, subTaskID, canonicalPhase)

	allDone := task.SubTaskDone >= task.SubTaskCount

	// 计算进度
	progress := calculateProgress(task.SubTaskDone, task.SubTaskCount)

	// 更新进度和阶段
	if err := taskModel.Update(ctx, mainTaskID, bson.M{
		"progress":      progress,
		"current_phase": moduleName,
	}); err != nil {
		logx.Errorf("[SchedulerClient] update progress failed: %v", err)
	}

	// 更新恢复管理器进度
	if err := c.recoveryMgr.UpdateTaskProgress(mainTaskID, moduleName, progress); err != nil {
		logx.Errorf("[SchedulerClient] UpdateTaskProgress failed: %v", err)
	}

	// Completion count only opens the finalization gate; phase summaries decide outcome.
	var finalSummary *model.TaskScanSummary
	finalized := false
	finalizationPending := false
	if allDone {
		summary, updated, finalizeErr := taskModel.FinalizeFromScanSummary(ctx, mainTaskID)
		if finalizeErr != nil {
			finalizationPending = true
			logx.Errorf("[SchedulerClient] semantic finalization failed; phase report remains acknowledged and finalization is retryable: %v", finalizeErr)
		} else {
			finalSummary = summary
			terminal := model.IsTerminalTaskStatus(task.Status)
			finalized = updated || terminal
			if !updated && !terminal {
				finalizationPending = true
				logx.Infof("[SchedulerClient] semantic finalization is still pending, mainTaskId=%s", mainTaskID)
			}
			if updated {
				logx.Infof("[SchedulerClient] task finalized, mainTaskId=%s outcome=%s", mainTaskID, summary.Outcome)
				// The atomic terminal transition is the exactly-once notification gate.
				if c.notifySvc != nil {
					if nerr := c.notifySvc.NotifyTaskCompleted(ctx, mainTaskID, summary.Outcome); nerr != nil {
						logx.Errorf("[SchedulerClient] NotifyTaskCompleted failed: %v", nerr)
					}
				}
			}
		}
	}

	return &IncrSubTaskDoneResponse{
		SubTaskDone: task.SubTaskDone, SubTaskCount: task.SubTaskCount, AllDone: allDone,
		Recorded: recorded, Finalized: finalized, FinalizationPending: finalizationPending, ScanSummary: finalSummary,
	}, nil
}

// NotifyOffline 通知离线（对齐原 HTTP NotifyOffline）
// 直接删除 Redis 中的 Worker 状态
func (c *SchedulerClient) NotifyOffline(ctx context.Context) error {
	workerKey := "cscan:worker:" + c.workerName
	c.rdb.Del(ctx, workerKey)
	c.rdb.SRem(ctx, "cscan:workers", c.workerName)
	controlKey := "cscan:worker:control:" + c.workerName
	c.rdb.Del(ctx, controlKey)
	logx.Infof("[SchedulerClient] Worker %s offline, deleted from Redis", c.workerName)
	return nil
}

// RequeueTask 将已弹出到 processing 的任务重新入队
// 用于本地入队失败时回滚：PushTask 回到队列 + SRem 移除 processing 标记
func (c *SchedulerClient) RequeueTask(ctx context.Context, task *scheduler.TaskInfo) error {
	if c.scheduler == nil {
		return fmt.Errorf("scheduler not available")
	}
	if err := c.scheduler.PushTask(ctx, task); err != nil {
		return fmt.Errorf("push task back failed: %w", err)
	}
	if err := c.rdb.SRem(ctx, "cscan:task:processing", task.TaskId).Err(); err != nil {
		logx.Errorf("[SchedulerClient] RequeueTask: SRem processing failed for %s: %v", task.TaskId, err)
	}
	return nil
}

// GetCancelSignalKey 获取控制信号 Key（复用 scheduler）
func (c *SchedulerClient) GetCancelSignalKey(taskId string) string {
	return c.scheduler.GetCancelSignalKey(taskId)
}

// SubscribeCancel 订阅取消信号（复用 scheduler）
func (c *SchedulerClient) SubscribeCancel(ctx context.Context) <-chan *scheduler.CancelSignal {
	return c.scheduler.SubscribeCancelSignals(ctx)
}

// ==================== 内部辅助方法 ====================

// updateMainTaskStatus 更新 MongoDB 主任务状态
func (c *SchedulerClient) updateMainTaskStatus(ctx context.Context, mainTaskID, state, phase string) {
	if mainTaskID == "" || !isValidObjectID(mainTaskID) {
		return
	}
	taskModel := model.NewMainTaskModel(c.mongoDB)
	now := time.Now()

	switch state {
	case model.TaskStatusStarted:
		task, err := taskModel.FindById(ctx, mainTaskID)
		if err != nil {
			logx.Errorf("[SchedulerClient] find task failed: %v", err)
			return
		}
		if task == nil {
			logx.Errorf("[SchedulerClient] main task not found: %s", mainTaskID)
			return
		}
		if task.Status == model.TaskStatusStarted {
			return // 已经是 STARTED，不重复设置
		}
		update := bson.M{
			"status":     state,
			"start_time": now,
		}
		if phase != "" {
			update["current_phase"] = phase
		}
		if err := taskModel.Update(ctx, mainTaskID, update); err != nil {
			logx.Errorf("[SchedulerClient] update task status failed: %v", err)
		}
	}
}

// updateMainTaskFromTaskInfo 从 Redis taskInfo 更新 MongoDB 主任务
func (c *SchedulerClient) updateMainTaskFromTaskInfo(ctx context.Context, taskInfoData, state, phase string) {
	if taskInfoData == "" {
		return
	}

	var taskInfo map[string]interface{}
	if err := json.Unmarshal([]byte(taskInfoData), &taskInfo); err != nil {
		logx.Errorf("[SchedulerClient] parse taskInfo failed: %v", err)
		return
	}

	mainTaskId, _ := taskInfo["mainTaskId"].(string)
	if mainTaskId == "" || !isValidObjectID(mainTaskId) {
		return
	}

	taskModel := model.NewMainTaskModel(c.mongoDB)
	now := time.Now()
	update := bson.M{}

	switch state {
	case model.TaskStatusSuccess:
		// Sub-task SUCCESS only closes executor/Redis bookkeeping. The main task
		// is finalized exclusively from persisted phase summaries.
		return
	case model.TaskStatusFailure:
		subTaskCount := 1
		if count, ok := taskInfo["subTaskCount"].(float64); ok {
			subTaskCount = int(count)
		}
		if subTaskCount <= 1 {
			update["status"] = state
			update["end_time"] = now
		}

	case model.TaskStatusStarted:
		// 已在 updateMainTaskStatus 处理
		return
	case model.TaskStatusPaused:
		update["status"] = state
	case "":
		// 仅更新阶段
	default:
		update["status"] = state
	}

	if phase != "" {
		update["current_phase"] = phase
	}

	if len(update) > 0 {
		if err := taskModel.Update(ctx, mainTaskId, update); err != nil {
			logx.Errorf("[SchedulerClient] update main task failed: %v", err)
		}
	}
}

// calculateProgress 计算进度百分比
func calculateProgress(done, total int) int {
	if total <= 0 {
		return 0
	}
	progress := done * 100 / total
	if progress > 100 {
		progress = 100
	}
	return progress
}

// isValidObjectID 判断是否为有效的 MongoDB ObjectID（24 位十六进制）
func isValidObjectID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// GetMainTaskModel 获取主任务模型（供外部使用）
func (c *SchedulerClient) GetMainTaskModel() *model.MainTaskModel {
	return model.NewMainTaskModel(c.mongoDB)
}

// GetScheduler 获取底层调度器实例（供外部使用）
func (c *SchedulerClient) GetScheduler() *scheduler.Scheduler {
	return c.scheduler
}

// SetNotifyService 设置通知服务
func (c *SchedulerClient) SetNotifyService(svc *notification.Service) {
	c.notifySvc = svc
}

// GetRecoveryMgr 获取恢复管理器（供外部使用）
func (c *SchedulerClient) GetRecoveryMgr() *scheduler.TaskRecoveryManager {
	return c.recoveryMgr
}
