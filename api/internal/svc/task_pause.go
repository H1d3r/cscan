package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cscan/internal/model"
	"cscan/internal/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
)

const pausedTaskMongoTimeout = 10 * time.Second

// persistPausedTask is the strict HTTP pause acknowledgement contract:
// snapshot and main-task state are durable before Redis exposes PAUSED and
// releases execution ownership.
func (s *ServiceContext) persistPausedTask(ctx context.Context, taskID, suppliedMainTaskID, leaseToken, worker, phase, taskState string) error {
	if s.Scheduler == nil {
		return fmt.Errorf("scheduler is not initialized")
	}
	if taskID == "" || leaseToken == "" || !json.Valid([]byte(taskState)) {
		return scheduler.ErrTaskLeaseConflict
	}

	operation, err := s.Scheduler.BeginPausedTask(ctx, taskID, leaseToken)
	if err != nil {
		return fmt.Errorf("begin paused task ownership: %w", err)
	}
	if operation == nil {
		return nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if releaseErr := s.Scheduler.ReleaseLeaseOperation(releaseCtx, operation); releaseErr != nil {
			logx.Errorf("[persistPausedTask] release operation guard failed: %v", releaseErr)
		}
	}()

	var leasedTask scheduler.TaskInfo
	if err := json.Unmarshal([]byte(operation.TaskInfoData), &leasedTask); err != nil {
		return fmt.Errorf("%w: decode paused task payload: %v", scheduler.ErrTaskLeaseConflict, err)
	}
	if leasedTask.TaskId != taskID || !isValidObjectID(leasedTask.MainTaskId) ||
		(suppliedMainTaskID != "" && suppliedMainTaskID != leasedTask.MainTaskId) ||
		leasedTask.DispatchGeneration == "" || leasedTask.DispatchGeneration != operation.DispatchGeneration ||
		operation.WorkerName == "" || operation.InstanceID == "" || operation.TaskProtocol != scheduler.TaskProtocolV1 ||
		(worker != "" && worker != operation.WorkerName) {
		return scheduler.ErrTaskLeaseConflict
	}

	mongoCtx, cancel := context.WithTimeout(ctx, pausedTaskMongoTimeout)
	defer cancel()
	taskModel := model.NewMainTaskModel(s.MongoDB)
	task, err := taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
	if err != nil {
		return fmt.Errorf("load paused main task: %w", err)
	}
	if task == nil || task.DispatchGeneration != operation.DispatchGeneration {
		return scheduler.ErrTaskLeaseConflict
	}
	if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusStarted && task.Status != model.TaskStatusPaused {
		return workerDispatchStateError(task, operation.DispatchGeneration)
	}
	if operation.AlreadyClosed {
		if task.Status == model.TaskStatusPaused {
			return nil
		}
		return workerDispatchStateError(task, operation.DispatchGeneration)
	}

	definitions, err := s.GetExecutorTaskModel().FindBatchDefinitionsForDispatch(
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

	committed, err := s.GetExecutorTaskModel().CommitPauseSnapshot(mongoCtx, leasedTask.MainTaskId, taskID,
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
		return workerDispatchStateError(current, operation.DispatchGeneration)
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
		return workerDispatchStateError(current, operation.DispatchGeneration)
	}
	if err := s.Scheduler.FinalizePausedTask(ctx, operation, operation.WorkerName, phase); err != nil {
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
		return workerDispatchStateError(current, operation.DispatchGeneration)
	}
	return nil
}
