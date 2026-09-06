package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cscan/internal/model"
	"cscan/internal/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
)

const directPauseMongoTimeout = 15 * time.Second

// PauseTask durably commits a direct-scheduler pause before releasing Redis
// execution ownership. The guarded payload, not a prior cache read, is the
// authority for parent, worker, instance, protocol, and dispatch generation.
func (c *SchedulerClient) PauseTask(ctx context.Context, taskID, suppliedMainTaskID, leaseToken, phase, taskState string) error {
	if taskID == "" || leaseToken == "" || !json.Valid([]byte(taskState)) {
		return scheduler.ErrTaskLeaseConflict
	}
	if c.scheduler == nil || c.executorTask == nil {
		return fmt.Errorf("pause persistence is not initialized")
	}

	operation, err := c.scheduler.BeginPausedTask(ctx, taskID, leaseToken)
	if err != nil {
		return fmt.Errorf("begin paused task ownership: %w", err)
	}
	if operation == nil {
		return nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if releaseErr := c.scheduler.ReleaseLeaseOperation(releaseCtx, operation); releaseErr != nil {
			logx.Errorf("[SchedulerClient] release pause operation guard failed: %v", releaseErr)
		}
	}()

	var leasedTask scheduler.TaskInfo
	if err := json.Unmarshal([]byte(operation.TaskInfoData), &leasedTask); err != nil {
		return fmt.Errorf("%w: decode paused task payload: %v", scheduler.ErrTaskLeaseConflict, err)
	}
	if leasedTask.TaskId != taskID || !isValidObjectID(leasedTask.MainTaskId) ||
		(suppliedMainTaskID != "" && suppliedMainTaskID != leasedTask.MainTaskId) ||
		leasedTask.DispatchGeneration == "" || leasedTask.DispatchGeneration != operation.DispatchGeneration ||
		operation.WorkerName != c.workerName || operation.InstanceID != c.instanceID ||
		operation.TaskProtocol != c.taskProtocol || operation.TaskProtocol != scheduler.TaskProtocolV1 {
		return scheduler.ErrTaskLeaseConflict
	}

	mongoCtx, cancel := context.WithTimeout(ctx, directPauseMongoTimeout)
	defer cancel()
	taskModel := model.NewMainTaskModel(c.mongoDB)
	task, err := taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
	if err != nil {
		return fmt.Errorf("load paused main task: %w", err)
	}
	if task == nil || task.DispatchGeneration != operation.DispatchGeneration {
		return scheduler.ErrTaskLeaseConflict
	}
	if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusStarted && task.Status != model.TaskStatusPaused {
		return directDispatchStateError(task, operation.DispatchGeneration)
	}
	if operation.AlreadyClosed {
		if task.Status == model.TaskStatusPaused {
			return nil
		}
		return directDispatchStateError(task, operation.DispatchGeneration)
	}

	definitions, err := c.executorTask.FindBatchDefinitionsForDispatch(
		mongoCtx, leasedTask.MainTaskId, operation.DispatchGeneration,
	)
	if err != nil {
		return fmt.Errorf("load paused dispatch manifest: %w", err)
	}
	if task.BatchCount > 0 && len(definitions) != task.BatchCount {
		return scheduler.ErrTaskLeaseConflict
	}
	definitionFound := false
	for _, definition := range definitions {
		if definition.TaskId == taskID && definition.MainTaskId == leasedTask.MainTaskId {
			definitionFound = true
			break
		}
	}
	if !definitionFound {
		return scheduler.ErrTaskLeaseConflict
	}

	committed, err := c.executorTask.CommitPauseSnapshot(mongoCtx, leasedTask.MainTaskId, taskID,
		model.PauseCommitEvidence{
			TaskState:          taskState,
			LeaseGeneration:    scheduler.LeaseGenerationHash(operation.LeaseToken),
			Worker:             operation.WorkerName,
			InstanceID:         operation.InstanceID,
			TaskProtocol:       operation.TaskProtocol,
			Phase:              phase,
			CommitTime:         time.Now().UTC(),
			DispatchGeneration: operation.DispatchGeneration,
		})
	if err != nil {
		return fmt.Errorf("persist paused sub-task snapshot: %w", err)
	}
	if !committed {
		return scheduler.ErrTaskLeaseConflict
	}

	mainTaskState := ""
	if len(definitions) == 1 && taskID == task.TaskId {
		mainTaskState = taskState
	}
	paused, err := taskModel.EnsurePausedDispatch(
		mongoCtx, leasedTask.MainTaskId, operation.DispatchGeneration, phase, mainTaskState,
	)
	if err != nil {
		return fmt.Errorf("update paused main task: %w", err)
	}
	if !paused {
		current, findErr := taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
		if findErr != nil {
			return fmt.Errorf("reload paused main task: %w", findErr)
		}
		return directDispatchStateError(current, operation.DispatchGeneration)
	}
	// Revalidate immediately before the Redis close. A racing STOP is a parent
	// fence, not lease loss; its STOP-only transition can take over `pausing`.
	current, err := taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
	if err != nil {
		return fmt.Errorf("revalidate paused main task: %w", err)
	}
	if current == nil || current.DispatchGeneration != operation.DispatchGeneration {
		return scheduler.ErrTaskLeaseConflict
	}
	if current.Status != model.TaskStatusPaused {
		return directDispatchStateError(current, operation.DispatchGeneration)
	}
	if err := c.scheduler.FinalizePausedTask(ctx, operation, operation.WorkerName, phase); err != nil {
		return fmt.Errorf("finalize paused task ownership: %w", err)
	}
	current, err = taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
	if err != nil {
		return fmt.Errorf("revalidate finalized paused main task: %w", err)
	}
	if current == nil || current.DispatchGeneration != operation.DispatchGeneration {
		return scheduler.ErrTaskLeaseConflict
	}
	if current.Status != model.TaskStatusPaused {
		return directDispatchStateError(current, operation.DispatchGeneration)
	}
	return nil
}
