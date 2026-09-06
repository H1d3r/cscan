package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cscan/internal/model"
	"cscan/internal/notification"
	"cscan/internal/scheduler"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// SchedulerClient Worker 直连 Redis 调度层客户端
// 替代原 HTTP→API→RPC 链路，直接操作 Redis 完成任务拉取/心跳/进度上报
type SchedulerClient struct {
	rdb          *redis.Client
	workerName   string
	instanceID   string
	taskProtocol int
	scheduler    *scheduler.Scheduler
	recoveryMgr  *scheduler.TaskRecoveryManager
	mongoDB      *mongo.Database
	executorTask *model.ExecutorTaskModel
	notifySvc    *notification.Service // 通知服务（任务完成/失败触发通知）
}

// NewSchedulerClient creates a v1 direct scheduler client with a generated
// process instance. Worker construction should use NewSchedulerClientForInstance
// so every transport shares the same immutable identity.
func NewSchedulerClient(rdb *redis.Client, workerName string, mongoDB *mongo.Database) (*SchedulerClient, error) {
	return NewSchedulerClientForInstance(rdb, workerName, uuid.NewString(), scheduler.TaskProtocolV1, mongoDB)
}

// NewSchedulerClientForInstance creates a direct scheduler client for one
// immutable worker process identity.
func NewSchedulerClientForInstance(
	rdb *redis.Client,
	workerName, instanceID string,
	taskProtocol int,
	mongoDB *mongo.Database,
) (*SchedulerClient, error) {
	if workerName == "" {
		return nil, fmt.Errorf("worker name is required")
	}
	if _, err := uuid.Parse(instanceID); err != nil {
		return nil, fmt.Errorf("invalid worker instance id: %w", err)
	}
	if taskProtocol != scheduler.TaskProtocolV1 {
		return nil, fmt.Errorf("unsupported task protocol %d", taskProtocol)
	}

	ctx := context.Background()
	s := scheduler.NewScheduler(rdb)
	recoveryMgr := scheduler.NewTaskRecoveryManager(rdb, ctx, s)
	executorTask := model.NewExecutorTaskModel(mongoDB)
	migrationCtx, cancel := context.WithTimeout(ctx, 35*time.Minute)
	defer cancel()
	if err := executorTask.MigrateAndEnsureUniqueIndex(migrationCtx); err != nil {
		return nil, fmt.Errorf("migrate executor task snapshots: %w", err)
	}

	return &SchedulerClient{
		rdb:          rdb,
		workerName:   workerName,
		instanceID:   instanceID,
		taskProtocol: taskProtocol,
		scheduler:    s,
		recoveryMgr:  recoveryMgr,
		mongoDB:      mongoDB,
		executorTask: executorTask,
	}, nil
}

// CheckTaskResponse 拉取任务响应（对齐原 HTTP TaskCheckResp）
type CheckTaskResponse struct {
	IsExist            bool
	IsFinished         bool
	TaskId             string
	MainTaskId         string
	Config             string
	LeaseToken         string
	DispatchGeneration string
}

// CheckTask pulls only a generation accepted by the durable parent. A popped
// stale/terminal generation is exact-discarded; a transient Mongo failure is
// exact-requeued and is never handed to the worker.
func (c *SchedulerClient) CheckTask(ctx context.Context) (*CheckTaskResponse, error) {
	if c.taskProtocol != scheduler.TaskProtocolV1 || c.instanceID == "" {
		return nil, fmt.Errorf("direct task acquisition requires leased-task-v1 instance identity")
	}
	if task, err := c.popAcceptedTask(ctx); err != nil {
		return nil, err
	} else if task != nil {
		return checkTaskResponse(task), nil
	}
	if err := c.waitForTaskAvailable(ctx, 25*time.Second); err != nil {
		return nil, err
	}
	if task, err := c.popAcceptedTask(ctx); err != nil {
		return nil, err
	} else if task != nil {
		return checkTaskResponse(task), nil
	}
	return &CheckTaskResponse{IsExist: false}, nil
}

func checkTaskResponse(task *scheduler.TaskInfo) *CheckTaskResponse {
	return &CheckTaskResponse{
		IsExist:            true,
		IsFinished:         false,
		TaskId:             task.TaskId,
		MainTaskId:         task.MainTaskId,
		Config:             task.Config,
		LeaseToken:         task.LeaseToken,
		DispatchGeneration: task.DispatchGeneration,
	}
}

func (c *SchedulerClient) popAcceptedTask(ctx context.Context) (*scheduler.TaskInfo, error) {
	for attempt := 0; attempt < 100; attempt++ {
		task, err := c.scheduler.PopTaskForWorkerInstance(ctx, c.workerName, c.instanceID)
		if err != nil {
			return nil, fmt.Errorf("PopTaskForWorkerInstance: %w", err)
		}
		if task == nil {
			return nil, nil
		}
		if !isValidObjectID(task.MainTaskId) {
			return task, nil
		}

		mongoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, claimErr := model.NewMainTaskModel(c.mongoDB).ClaimTaskStarted(mongoCtx, task.MainTaskId, task.DispatchGeneration)
		cancel()
		if claimErr == nil {
			return task, nil
		}
		if errors.Is(claimErr, model.ErrTaskDispatchConflict) {
			if discardErr := c.scheduler.DiscardExactTask(ctx, task); discardErr != nil {
				return nil, fmt.Errorf("discard rejected dispatch %s: %w", task.TaskId, discardErr)
			}
			continue
		}
		if _, requeueErr := c.scheduler.RequeueExactTask(ctx, task); requeueErr != nil {
			return nil, fmt.Errorf("parent claim failed (%v) and exact requeue failed: %w", claimErr, requeueErr)
		}
		return nil, fmt.Errorf("claim durable parent generation: %w", claimErr)
	}
	return nil, fmt.Errorf("too many rejected queued task generations")
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

func directDispatchError(err error) error {
	switch {
	case errors.Is(err, model.ErrTaskParentFenced):
		return scheduler.ErrTaskParentFenced
	case errors.Is(err, model.ErrTaskDispatchConflict):
		return scheduler.ErrTaskLeaseConflict
	default:
		return err
	}
}

func directDispatchStateError(task *model.MainTask, generation string) error {
	return directDispatchError(model.DispatchStateError(task, generation))
}

func directCompletionDispatchState(task *model.MainTask, generation string) (semanticTerminal, runnable bool, err error) {
	if task == nil || task.DispatchGeneration != generation {
		return false, false, scheduler.ErrTaskLeaseConflict
	}
	if model.IsSemanticTerminalTaskStatus(task.Status) {
		return true, false, nil
	}
	if model.HasExactControlIntent(task, generation, model.TaskControlActionPause) ||
		model.HasExactControlIntent(task, generation, model.TaskControlActionStop) {
		return false, false, scheduler.ErrTaskParentFenced
	}
	if model.IsRunnableTaskStatus(task.Status) {
		return false, true, nil
	}
	return false, false, scheduler.ErrTaskLeaseConflict
}

// UpdateTask 更新任务状态。
// taskState 是仅供 PAUSED 状态使用的可恢复快照，不能与阶段或结果字段混用。
func (c *SchedulerClient) UpdateTask(ctx context.Context, taskID, leaseToken, state string, progress int, phase, taskState string) error {
	if state == model.TaskStatusPaused && taskState == "" {
		return fmt.Errorf("paused task update requires a resumable snapshot")
	}
	if taskState != "" {
		if state != model.TaskStatusPaused {
			return fmt.Errorf("task state is only valid when task is paused")
		}
		if !json.Valid([]byte(taskState)) {
			return fmt.Errorf("task state must be valid JSON")
		}
		return c.PauseTask(ctx, taskID, "", leaseToken, phase, taskState)
	}
	if c.scheduler == nil || taskID == "" || leaseToken == "" {
		return scheduler.ErrTaskLeaseConflict
	}

	updateCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	terminal := state == model.TaskStatusSuccess || state == model.TaskStatusPartial ||
		state == model.TaskStatusFailure || state == model.TaskStatusStopped ||
		state == model.TaskStatusRevoked || state == "COMPLETED"
	var operation *scheduler.LeaseOperation
	var err error
	if state == model.TaskStatusStopped {
		operation, err = c.scheduler.BeginStoppedTask(updateCtx, taskID, leaseToken)
	} else {
		operation, err = c.scheduler.BeginLeaseOperation(updateCtx, taskID, leaseToken)
	}
	if err != nil {
		if errors.Is(err, scheduler.ErrTaskLeaseConflict) && terminal {
			if _, confirmErr := c.scheduler.ConfirmClosedTaskLease(updateCtx, taskID, leaseToken, state); confirmErr == nil {
				return nil
			} else if !errors.Is(confirmErr, scheduler.ErrTaskLeaseConflict) {
				return fmt.Errorf("confirm guarded terminal task update: %w", confirmErr)
			}
		}
		return err
	}
	if operation == nil {
		return nil
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer releaseCancel()
		if releaseErr := c.scheduler.ReleaseLeaseOperation(releaseCtx, operation); releaseErr != nil {
			logx.Errorf("[SchedulerClient] release task update guard failed: %v", releaseErr)
		}
	}()

	var leasedTask scheduler.TaskInfo
	if err := json.Unmarshal([]byte(operation.TaskInfoData), &leasedTask); err != nil {
		return fmt.Errorf("%w: decode guarded task payload: %v", scheduler.ErrTaskLeaseConflict, err)
	}
	if leasedTask.TaskId != taskID || leasedTask.DispatchGeneration != operation.DispatchGeneration ||
		operation.WorkerName != c.workerName || operation.InstanceID != c.instanceID ||
		operation.TaskProtocol != c.taskProtocol || operation.TaskProtocol != scheduler.TaskProtocolV1 {
		return scheduler.ErrTaskLeaseConflict
	}

	if isValidObjectID(leasedTask.MainTaskId) {
		if operation.DispatchGeneration == "" {
			return scheduler.ErrTaskLeaseConflict
		}
		taskModel := model.NewMainTaskModel(c.mongoDB)
		if terminal {
			nextState := state
			if nextState == "COMPLETED" {
				nextState = model.TaskStatusSuccess
			}
			if nextState == model.TaskStatusStopped || nextState == model.TaskStatusRevoked {
				current, findErr := taskModel.FindById(updateCtx, leasedTask.MainTaskId)
				if findErr != nil {
					return findErr
				}
				if nextState == model.TaskStatusStopped {
					if !model.HasExactControlIntent(current, operation.DispatchGeneration, model.TaskControlActionStop) {
						return directDispatchStateError(current, operation.DispatchGeneration)
					}
				} else if current == nil || current.DispatchGeneration != operation.DispatchGeneration ||
					current.Status != nextState {
					return directDispatchStateError(current, operation.DispatchGeneration)
				}
			} else {
				fields := bson.M{"progress": 100, "end_time": time.Now()}
				if phase != "" {
					fields["current_phase"] = phase
				}
				matched, transitionErr := taskModel.TransitionDispatchStatus(updateCtx, leasedTask.MainTaskId,
					operation.DispatchGeneration, []string{model.TaskStatusPending, model.TaskStatusStarted}, nextState, fields)
				if transitionErr != nil {
					return transitionErr
				}
				if !matched {
					current, findErr := taskModel.FindById(updateCtx, leasedTask.MainTaskId)
					if findErr != nil {
						return findErr
					}
					if current == nil || current.DispatchGeneration != operation.DispatchGeneration ||
						current.Status != nextState {
						return directDispatchStateError(current, operation.DispatchGeneration)
					}
				}
			}
		} else if phase != "" || progress > 0 {
			matched, updateErr := taskModel.UpdateDispatchProgress(updateCtx, leasedTask.MainTaskId,
				operation.DispatchGeneration, phase, progress)
			if updateErr != nil {
				return directDispatchError(updateErr)
			}
			if !matched {
				current, findErr := taskModel.FindById(updateCtx, leasedTask.MainTaskId)
				if findErr != nil {
					return findErr
				}
				return directDispatchStateError(current, operation.DispatchGeneration)
			}
		}
	}

	if state == model.TaskStatusStopped {
		return c.scheduler.FinalizeStoppedTask(updateCtx, operation, operation.WorkerName, "", phase)
	}
	_, err = c.scheduler.UpdateLeasedTaskWithOperation(
		updateCtx, operation, operation.WorkerName, state, "", phase, progress,
	)
	return err
}

// RenewTaskLease refreshes the exact child execution without changing task
// status, phase, or progress.
func (c *SchedulerClient) RenewTaskLease(ctx context.Context, taskID, leaseToken string) error {
	return c.scheduler.RenewTaskLease(ctx, taskID, leaseToken)
}

// KeepAlive writes both the logical-name heartbeat retained for UI
// compatibility and the immutable per-instance liveness key used by recovery.
func (c *SchedulerClient) KeepAlive(ctx context.Context, cpuLoad, memUsed float64, taskStarted, taskExecuted int, concurrency int) error {
	workerData := map[string]interface{}{
		"workerName":         c.workerName,
		"instanceId":         c.instanceID,
		"taskProtocol":       c.taskProtocol,
		"cpuLoad":            cpuLoad,
		"memUsed":            memUsed,
		"taskStartedNumber":  taskStarted,
		"taskExecutedNumber": taskExecuted,
		"isDaemon":           false,
		"concurrency":        concurrency,
		"updateTime":         time.Now().Format("2006-01-02 15:04:05"),
		"status":             "online",
	}
	workerJSON, err := json.Marshal(workerData)
	if err != nil {
		return fmt.Errorf("marshal worker heartbeat: %w", err)
	}

	pipe := c.rdb.TxPipeline()
	pipe.Set(ctx, "cscan:worker:"+c.workerName, workerJSON, 60*time.Second)
	pipe.Set(ctx, "cscan:worker:instance:"+c.instanceID, workerJSON, 60*time.Second)
	pipe.SAdd(ctx, "cscan:workers", c.workerName)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("write worker heartbeat: %w", err)
	}
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
	LeaseClosed         bool
	Finalized           bool
	FinalizationPending bool
	ScanSummary         *model.TaskScanSummary
}

// IncrSubTaskDone holds an exact-lease Redis operation guard across every
// bounded Mongo mutation. Once the phase write installs durable reconciliation
// ownership, a completed child may release its exact lease while semantic
// finalization continues asynchronously.
func (c *SchedulerClient) IncrSubTaskDone(ctx context.Context, mainTaskID, subTaskID, leaseToken, moduleName string, isCompleted bool, incrAmount int, phaseResult *PhaseResult) (*IncrSubTaskDoneResponse, error) {
	if mainTaskID == "" || subTaskID == "" || leaseToken == "" || c.scheduler == nil {
		return nil, scheduler.ErrTaskLeaseConflict
	}
	reportCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	operation, err := c.scheduler.BeginLeaseOperation(reportCtx, subTaskID, leaseToken)
	if err != nil {
		if errors.Is(err, scheduler.ErrTaskLeaseConflict) && isCompleted {
			closedTask, confirmErr := c.scheduler.ConfirmClosedTaskLease(reportCtx, subTaskID, leaseToken, "COMPLETED")
			if confirmErr == nil {
				if closedTask.MainTaskId != mainTaskID {
					return nil, scheduler.ErrTaskLeaseConflict
				}
				if !isValidObjectID(mainTaskID) {
					return &IncrSubTaskDoneResponse{
						SubTaskDone: 1, SubTaskCount: 1, AllDone: true,
						Recorded: true, LeaseClosed: true, Finalized: true,
					}, nil
				}
				task, findErr := model.NewMainTaskModel(c.mongoDB).FindById(reportCtx, mainTaskID)
				if findErr != nil {
					return nil, findErr
				}
				if task == nil || closedTask.DispatchGeneration == "" || task.DispatchGeneration != closedTask.DispatchGeneration {
					return nil, scheduler.ErrTaskLeaseConflict
				}
				semanticTerminal, runnable, stateErr := directCompletionDispatchState(task, closedTask.DispatchGeneration)
				if stateErr != nil {
					return nil, stateErr
				}
				allDone := task.SubTaskDone >= task.SubTaskCount
				return &IncrSubTaskDoneResponse{
					SubTaskDone: task.SubTaskDone, SubTaskCount: task.SubTaskCount,
					AllDone: allDone, Recorded: true, LeaseClosed: true,
					Finalized: semanticTerminal, FinalizationPending: runnable && allDone, ScanSummary: task.ScanSummary,
				}, nil
			}
			if !errors.Is(confirmErr, scheduler.ErrTaskLeaseConflict) {
				return nil, fmt.Errorf("confirm completed task lease: %w", confirmErr)
			}
		}
		return nil, err
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer releaseCancel()
		if releaseErr := c.scheduler.ReleaseLeaseOperation(releaseCtx, operation); releaseErr != nil {
			logx.Errorf("[SchedulerClient] release phase operation guard failed: %v", releaseErr)
		}
	}()

	var leasedTask scheduler.TaskInfo
	if err := json.Unmarshal([]byte(operation.TaskInfoData), &leasedTask); err != nil {
		return nil, fmt.Errorf("%w: decode leased task payload: %v", scheduler.ErrTaskLeaseConflict, err)
	}
	if leasedTask.TaskId != subTaskID || leasedTask.MainTaskId != mainTaskID {
		return nil, fmt.Errorf("%w: leased task identity does not match phase report", scheduler.ErrTaskLeaseConflict)
	}
	closeCompletedLease := func() (bool, error) {
		if !isCompleted {
			return false, nil
		}
		if _, closeErr := c.scheduler.UpdateLeasedTaskWithOperation(
			reportCtx, operation, operation.WorkerName, "COMPLETED", "", moduleName, 100,
		); closeErr != nil {
			if _, confirmErr := c.scheduler.ConfirmClosedTaskLease(reportCtx, subTaskID, leaseToken, "COMPLETED"); confirmErr == nil {
				return true, nil
			}
			return false, closeErr
		}
		return true, nil
	}
	if !isValidObjectID(mainTaskID) {
		leaseClosed, closeErr := closeCompletedLease()
		if closeErr != nil {
			return nil, fmt.Errorf("close quick validation task lease: %w", closeErr)
		}
		return &IncrSubTaskDoneResponse{
			SubTaskDone: 1, SubTaskCount: 1, AllDone: true,
			Recorded: true, LeaseClosed: leaseClosed, Finalized: true,
		}, nil
	}
	if leasedTask.DispatchGeneration == "" || leasedTask.DispatchGeneration != operation.DispatchGeneration {
		return nil, scheduler.ErrTaskLeaseConflict
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
	phaseSummary.LeaseGeneration = scheduler.LeaseGenerationHash(leaseToken)
	reportIdentity := canonicalPhase
	if isCompleted {
		reportIdentity = "complete"
	} else if reportIdentity == "complete" {
		// Only an explicit completed report may own the canonical final key.
		reportIdentity = "complete-intermediate"
	}
	reportKey := model.TaskPhaseReportKey(subTaskID, reportIdentity)
	task, recorded, err := taskModel.RecordPhaseSummaryForDispatch(reportCtx, mainTaskID,
		leasedTask.DispatchGeneration, reportKey, phaseSummary, incrAmount)
	if err != nil {
		if errors.Is(err, model.ErrTaskDispatchConflict) || errors.Is(err, model.ErrTaskParentFenced) {
			current, findErr := taskModel.FindById(reportCtx, mainTaskID)
			if findErr != nil {
				return nil, findErr
			}
			return nil, directDispatchStateError(current, leasedTask.DispatchGeneration)
		}
		return nil, fmt.Errorf("RecordPhaseSummaryForDispatch: %w", err)
	}
	if !recorded {
		logx.Infof("[SchedulerClient] duplicate phase report accepted without payload overwrite, mainTaskId=%s taskId=%s phase=%s",
			mainTaskID, subTaskID, canonicalPhase)
	}

	allDone := task.SubTaskDone >= task.SubTaskCount
	if model.IsSemanticTerminalTaskStatus(task.Status) {
		leaseClosed, closeErr := closeCompletedLease()
		if closeErr != nil {
			return nil, fmt.Errorf("close completed task lease: %w", closeErr)
		}
		return &IncrSubTaskDoneResponse{
			SubTaskDone: task.SubTaskDone, SubTaskCount: task.SubTaskCount, AllDone: allDone,
			Recorded: true, LeaseClosed: leaseClosed, Finalized: true, ScanSummary: task.ScanSummary,
		}, nil
	}
	progress := calculateProgress(task.SubTaskDone, task.SubTaskCount)
	matched, err := taskModel.UpdateDispatchProgress(reportCtx, mainTaskID,
		leasedTask.DispatchGeneration, moduleName, progress)
	if err != nil {
		return nil, fmt.Errorf("update conditional task progress: %w", err)
	}
	if !matched {
		current, findErr := taskModel.FindById(reportCtx, mainTaskID)
		if findErr != nil {
			return nil, findErr
		}
		semanticTerminal, _, stateErr := directCompletionDispatchState(current, leasedTask.DispatchGeneration)
		if stateErr != nil {
			return nil, stateErr
		}
		if !semanticTerminal {
			return nil, scheduler.ErrTaskLeaseConflict
		}
		leaseClosed, closeErr := closeCompletedLease()
		if closeErr != nil {
			return nil, fmt.Errorf("close exact lease after sibling finalization: %w", closeErr)
		}
		return &IncrSubTaskDoneResponse{
			SubTaskDone: current.SubTaskDone, SubTaskCount: current.SubTaskCount,
			AllDone: current.SubTaskDone >= current.SubTaskCount, Recorded: true, LeaseClosed: leaseClosed,
			Finalized: true, ScanSummary: current.ScanSummary,
		}, nil
	}

	var finalSummary *model.TaskScanSummary
	finalized := false
	finalizationPending := false
	closeLease := !allDone
	if allDone {
		summary, updated, finalizeErr := taskModel.FinalizeFromScanSummaryForDispatch(
			reportCtx, mainTaskID, leasedTask.DispatchGeneration)
		switch {
		case finalizeErr == nil:
			finalSummary = summary
			finalized = true
			closeLease = true
			if updated && c.notifySvc != nil {
				if nerr := c.notifySvc.NotifyTaskCompleted(reportCtx, mainTaskID, summary.Outcome); nerr != nil {
					logx.Errorf("[SchedulerClient] NotifyTaskCompleted failed: %v", nerr)
				}
			}
		case errors.Is(finalizeErr, model.ErrTaskFinalizationPending):
			finalSummary = summary
			finalizationPending = true
			current, findErr := taskModel.FindById(reportCtx, mainTaskID)
			if findErr != nil {
				return nil, findErr
			}
			semanticTerminal, runnable, stateErr := directCompletionDispatchState(current, leasedTask.DispatchGeneration)
			if stateErr != nil {
				return nil, stateErr
			}
			if semanticTerminal {
				finalized = true
				finalizationPending = false
				finalSummary = current.ScanSummary
			}
			closeLease = semanticTerminal || runnable
		default:
			if errors.Is(finalizeErr, model.ErrTaskDispatchConflict) || errors.Is(finalizeErr, model.ErrTaskParentFenced) {
				current, findErr := taskModel.FindById(reportCtx, mainTaskID)
				if findErr != nil {
					return nil, findErr
				}
				return nil, directDispatchStateError(current, leasedTask.DispatchGeneration)
			}
			finalizationPending = true
			logx.Errorf("[SchedulerClient] semantic finalization failed; durable reconciliation retains ownership: %v", finalizeErr)
			current, findErr := taskModel.FindById(reportCtx, mainTaskID)
			if findErr != nil {
				return nil, findErr
			}
			semanticTerminal, _, stateErr := directCompletionDispatchState(current, leasedTask.DispatchGeneration)
			if stateErr != nil {
				return nil, stateErr
			}
			if semanticTerminal {
				finalized = true
				finalizationPending = false
				finalSummary = current.ScanSummary
				closeLease = true
			}
		}
	}

	leaseClosed := false
	if closeLease {
		leaseClosed, err = closeCompletedLease()
		if err != nil {
			return nil, fmt.Errorf("close completed task lease: %w", err)
		}
	}
	return &IncrSubTaskDoneResponse{
		SubTaskDone: task.SubTaskDone, SubTaskCount: task.SubTaskCount, AllDone: allDone,
		Recorded: true, LeaseClosed: leaseClosed, Finalized: finalized,
		FinalizationPending: finalizationPending, ScanSummary: finalSummary,
	}, nil
}

var notifyWorkerOfflineScript = redis.NewScript(`
	local function ownedByInstance(value)
		if not value then
			return false
		end
		local decoded = nil
		pcall(function() decoded = cjson.decode(value) end)
		return decoded and (decoded.instanceId or '') == ARGV[1] and (decoded.workerName or '') == ARGV[2]
	end

	local removedInstance = 0
	if ownedByInstance(redis.call('GET', KEYS[1])) then
		redis.call('DEL', KEYS[1])
		removedInstance = 1
	end
	if ownedByInstance(redis.call('GET', KEYS[2])) then
		redis.call('DEL', KEYS[2])
		redis.call('SREM', KEYS[3], ARGV[2])
		redis.call('DEL', KEYS[4])
	end
	return removedInstance
`)

// NotifyOffline compare-and-deletes only this process instance. It never
// removes a same-name heartbeat written by another process generation.
func (c *SchedulerClient) NotifyOffline(ctx context.Context) error {
	_, err := notifyWorkerOfflineScript.Run(ctx, c.rdb, []string{
		"cscan:worker:instance:" + c.instanceID,
		"cscan:worker:" + c.workerName,
		"cscan:workers",
		"cscan:worker:control:" + c.workerName,
	}, c.instanceID, c.workerName).Int()
	if err != nil {
		return fmt.Errorf("compare-delete worker heartbeat: %w", err)
	}
	logx.Infof("[SchedulerClient] Worker %s instance %s offline", c.workerName, c.instanceID)
	return nil
}

// RequeueTask 将已弹出到 processing 的任务按当前 lease 原子退回队列。
func (c *SchedulerClient) RequeueTask(ctx context.Context, task *scheduler.TaskInfo) error {
	if c.scheduler == nil {
		return fmt.Errorf("scheduler not available")
	}
	moved, err := c.scheduler.RequeueExactTask(ctx, task)
	if err != nil {
		return fmt.Errorf("requeue leased task: %w", err)
	}
	if !moved {
		return fmt.Errorf("requeue leased task was not committed")
	}
	return nil
}

// GetTaskControlSignals reads durable exact-generation control keys. Pub/Sub
// remains a latency optimization; this lookup repairs registration/reconnect races.
func (c *SchedulerClient) GetTaskControlSignals(
	ctx context.Context,
	targets []scheduler.TaskControlTarget,
) ([]scheduler.TaskControlEnvelope, error) {
	signals := make([]scheduler.TaskControlEnvelope, 0, len(targets))
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return nil, err
		}
		envelope, err := c.scheduler.GetTaskControl(ctx, target)
		if err != nil {
			return nil, err
		}
		if envelope != nil {
			signals = append(signals, *envelope)
		}
	}
	return signals, nil
}

// SubscribeTaskControls subscribes to strict generation-bearing controls.
func (c *SchedulerClient) SubscribeTaskControls(ctx context.Context) <-chan *scheduler.TaskControlEnvelope {
	return c.scheduler.SubscribeTaskControls(ctx)
}

// ==================== 内部辅助方法 ====================

// updateMainTaskFromTaskInfo applies only nonterminal metadata to the active
// durable dispatch. Scan terminal state remains owned by semantic summaries.
func (c *SchedulerClient) updateMainTaskFromTaskInfo(ctx context.Context, taskInfoData, phase string) error {
	if taskInfoData == "" {
		return nil
	}
	var taskInfo scheduler.TaskInfo
	if err := json.Unmarshal([]byte(taskInfoData), &taskInfo); err != nil {
		return fmt.Errorf("parse taskInfo: %w", err)
	}
	if taskInfo.MainTaskId == "" || !isValidObjectID(taskInfo.MainTaskId) {
		return nil
	}
	if taskInfo.DispatchGeneration == "" {
		return scheduler.ErrTaskLeaseConflict
	}
	if phase == "" {
		return nil
	}
	matched, err := model.NewMainTaskModel(c.mongoDB).UpdateActiveDispatchFields(ctx,
		taskInfo.MainTaskId, taskInfo.DispatchGeneration, bson.M{"current_phase": phase})
	if err != nil {
		return err
	}
	if !matched {
		current, findErr := model.NewMainTaskModel(c.mongoDB).FindById(ctx, taskInfo.MainTaskId)
		if findErr != nil {
			return findErr
		}
		return directDispatchStateError(current, taskInfo.DispatchGeneration)
	}
	return nil
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
