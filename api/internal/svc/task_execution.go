package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cscan/internal/model"
	"cscan/internal/notification"
	"cscan/internal/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// CheckTaskResult 任务拉取结果
type CheckTaskResult struct {
	IsExist            bool
	IsFinished         bool
	TaskId             string
	MainTaskId         string
	Config             string
	LeaseToken         string
	DispatchGeneration string
}

// CheckTask returns only a child whose durable parent accepted the exact
// dispatch generation.
func (s *ServiceContext) CheckTask(ctx context.Context, workerName, instanceID string) (*CheckTaskResult, error) {
	if result, err := s.tryPopTask(ctx, workerName, instanceID); err != nil || result != nil {
		return result, err
	}
	return s.waitForTask(ctx, workerName, instanceID)
}

const longPollTimeout = 25 * time.Second

func (s *ServiceContext) tryPopTask(ctx context.Context, workerName, instanceID string) (*CheckTaskResult, error) {
	if s.Scheduler == nil {
		return nil, nil
	}
	for attempt := 0; attempt < 100; attempt++ {
		task, err := s.Scheduler.PopTaskForWorkerInstance(ctx, workerName, instanceID)
		if err != nil {
			return nil, err
		}
		if task == nil {
			return nil, nil
		}
		if isValidObjectID(task.MainTaskId) {
			mongoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, claimErr := model.NewMainTaskModel(s.MongoDB).ClaimTaskStarted(mongoCtx, task.MainTaskId, task.DispatchGeneration)
			cancel()
			if claimErr != nil {
				if errors.Is(claimErr, model.ErrTaskDispatchConflict) {
					if discardErr := s.Scheduler.DiscardExactTask(ctx, task); discardErr != nil {
						return nil, fmt.Errorf("discard rejected dispatch %s: %w", task.TaskId, discardErr)
					}
					continue
				}
				if _, requeueErr := s.Scheduler.RequeueExactTask(ctx, task); requeueErr != nil {
					return nil, fmt.Errorf("parent claim failed (%v) and exact requeue failed: %w", claimErr, requeueErr)
				}
				return nil, fmt.Errorf("claim durable parent generation: %w", claimErr)
			}
			if s.QueryCache != nil {
				s.QueryCache.Clear()
			}
		}
		return &CheckTaskResult{
			IsExist:            true,
			IsFinished:         false,
			TaskId:             task.TaskId,
			MainTaskId:         task.MainTaskId,
			Config:             task.Config,
			LeaseToken:         task.LeaseToken,
			DispatchGeneration: task.DispatchGeneration,
		}, nil
	}
	return nil, fmt.Errorf("too many rejected queued task generations")
}

func (s *ServiceContext) waitForTask(ctx context.Context, workerName, instanceID string) (*CheckTaskResult, error) {
	pubsub := s.RedisClient.Subscribe(ctx, "cscan:task:available")
	defer pubsub.Close()

	if _, err := pubsub.Receive(ctx); err != nil {
		return &CheckTaskResult{}, nil
	}

	ch := pubsub.Channel()
	pollCtx, cancel := context.WithTimeout(ctx, longPollTimeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return &CheckTaskResult{}, nil
		case <-ch:
			if result, err := s.tryPopTask(ctx, workerName, instanceID); err != nil || result != nil {
				return result, err
			}
		case <-ticker.C:
			if result, err := s.tryPopTask(ctx, workerName, instanceID); err != nil || result != nil {
				return result, err
			}
		}
	}
}

func isWorkerTerminalState(state string) bool {
	switch state {
	case model.TaskStatusSuccess, model.TaskStatusPartial, model.TaskStatusFailure,
		model.TaskStatusStopped, model.TaskStatusRevoked, "COMPLETED":
		return true
	default:
		return false
	}
}

func workerDispatchError(err error) error {
	switch {
	case errors.Is(err, model.ErrTaskParentFenced):
		return scheduler.ErrTaskParentFenced
	case errors.Is(err, model.ErrTaskDispatchConflict):
		return scheduler.ErrTaskLeaseConflict
	default:
		return err
	}
}

func workerDispatchStateError(task *model.MainTask, generation string) error {
	return workerDispatchError(model.DispatchStateError(task, generation))
}

func workerCompletionDispatchState(task *model.MainTask, generation string) (semanticTerminal, runnable bool, err error) {
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

// UpdateTask 更新任务状态
func (s *ServiceContext) UpdateTask(ctx context.Context, taskId, mainTaskId, leaseToken, state, worker, result, phase, taskState string, progress int) error {
	if leaseToken != "" && mainTaskId == "" && state == "" && worker == "" && result == "" && phase == "" && taskState == "" && progress == 0 {
		if s.Scheduler == nil {
			return fmt.Errorf("scheduler is not initialized")
		}
		if err := s.Scheduler.RenewTaskLease(ctx, taskId, leaseToken); err != nil {
			return fmt.Errorf("renew task lease: %w", err)
		}
		return nil
	}
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
	}
	if state == model.TaskStatusPaused {
		return s.persistPausedTask(ctx, taskId, mainTaskId, leaseToken, worker, phase, taskState)
	}
	if s.Scheduler == nil || taskId == "" || leaseToken == "" {
		return scheduler.ErrTaskLeaseConflict
	}

	var operation *scheduler.LeaseOperation
	var err error
	if state == model.TaskStatusStopped {
		operation, err = s.Scheduler.BeginStoppedTask(ctx, taskId, leaseToken)
	} else {
		operation, err = s.Scheduler.BeginLeaseOperation(ctx, taskId, leaseToken)
	}
	if err != nil {
		if errors.Is(err, scheduler.ErrTaskLeaseConflict) && isWorkerTerminalState(state) {
			if _, confirmErr := s.Scheduler.ConfirmClosedTaskLease(ctx, taskId, leaseToken, state); confirmErr == nil {
				return nil
			} else if !errors.Is(confirmErr, scheduler.ErrTaskLeaseConflict) {
				return fmt.Errorf("confirm guarded terminal task update: %w", confirmErr)
			}
		}
		return fmt.Errorf("begin guarded task update: %w", err)
	}
	if operation == nil {
		return nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if releaseErr := s.Scheduler.ReleaseLeaseOperation(releaseCtx, operation); releaseErr != nil {
			logx.Errorf("[UpdateTask] release operation guard failed: %v", releaseErr)
		}
	}()

	var leasedTask scheduler.TaskInfo
	if err := json.Unmarshal([]byte(operation.TaskInfoData), &leasedTask); err != nil {
		return fmt.Errorf("%w: decode guarded task payload: %v", scheduler.ErrTaskLeaseConflict, err)
	}
	if leasedTask.TaskId != taskId || (mainTaskId != "" && leasedTask.MainTaskId != mainTaskId) ||
		leasedTask.DispatchGeneration != operation.DispatchGeneration || operation.WorkerName == "" ||
		operation.InstanceID == "" || operation.TaskProtocol != scheduler.TaskProtocolV1 ||
		(worker != "" && worker != operation.WorkerName) {
		return scheduler.ErrTaskLeaseConflict
	}

	if isValidObjectID(leasedTask.MainTaskId) {
		if operation.DispatchGeneration == "" {
			return scheduler.ErrTaskLeaseConflict
		}
		mongoCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		taskModel := model.NewMainTaskModel(s.MongoDB)
		if isWorkerTerminalState(state) {
			nextState := state
			if nextState == "COMPLETED" {
				nextState = model.TaskStatusSuccess
			}
			if nextState == model.TaskStatusStopped || nextState == model.TaskStatusRevoked {
				// STOP/REVOKE is acknowledgement-only. STOP additionally requires the
				// exact durable same-generation intent that caused this response.
				current, findErr := taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
				if findErr != nil {
					return findErr
				}
				if nextState == model.TaskStatusStopped {
					if !model.HasExactControlIntent(current, operation.DispatchGeneration, model.TaskControlActionStop) {
						return workerDispatchStateError(current, operation.DispatchGeneration)
					}
				} else if current == nil || current.DispatchGeneration != operation.DispatchGeneration ||
					current.Status != nextState {
					return workerDispatchStateError(current, operation.DispatchGeneration)
				}
			} else {
				fields := bson.M{"progress": 100, "end_time": time.Now()}
				if result != "" {
					fields["result"] = result
				}
				if phase != "" {
					fields["current_phase"] = phase
				}
				matched, transitionErr := taskModel.TransitionDispatchStatus(mongoCtx, leasedTask.MainTaskId,
					operation.DispatchGeneration, []string{model.TaskStatusPending, model.TaskStatusStarted}, nextState, fields)
				if transitionErr != nil {
					return transitionErr
				}
				if !matched {
					current, findErr := taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
					if findErr != nil {
						return findErr
					}
					if current == nil || current.DispatchGeneration != operation.DispatchGeneration ||
						current.Status != nextState {
						return workerDispatchStateError(current, operation.DispatchGeneration)
					}
				}
			}
		} else if phase != "" || progress > 0 {
			matched, updateErr := taskModel.UpdateDispatchProgress(mongoCtx, leasedTask.MainTaskId,
				operation.DispatchGeneration, phase, progress)
			if updateErr != nil {
				return workerDispatchError(updateErr)
			}
			if !matched {
				current, findErr := taskModel.FindById(mongoCtx, leasedTask.MainTaskId)
				if findErr != nil {
					return findErr
				}
				return workerDispatchStateError(current, operation.DispatchGeneration)
			}
		}
	}

	if state == model.TaskStatusStopped {
		if err := s.Scheduler.FinalizeStoppedTask(ctx, operation, operation.WorkerName, result, phase); err != nil {
			return fmt.Errorf("finalize guarded stopped task: %w", err)
		}
		return nil
	}
	if _, err := s.Scheduler.UpdateLeasedTaskWithOperation(
		ctx, operation, operation.WorkerName, state, result, phase, progress,
	); err != nil {
		return fmt.Errorf("finalize guarded task update: %w", err)
	}
	return nil
}

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

// IncrSubTaskDoneResult 子任务完成结果
type IncrSubTaskDoneResult struct {
	Success             bool
	Message             string
	SubTaskDone         int32
	SubTaskCount        int32
	AllDone             bool
	Recorded            bool
	LeaseClosed         bool
	Finalized           bool
	FinalizationPending bool
	ScanSummary         *model.TaskScanSummary
}

// normalizeWorkerPhaseSummary accepts both rollout payload shapes. The server
// persists a single phase report and always recomputes the final outcome; a
// worker-provided aggregate outcome is never trusted as an authoritative state.
func normalizeWorkerPhaseSummary(taskId, phase string, incrAmount int, phaseResult *model.TaskPhaseSummary, taskSummary *model.TaskScanSummary) model.TaskPhaseSummary {
	canonicalPhase := canonicalTaskPhaseName(phase)
	var normalized model.TaskPhaseSummary

	if phaseResult != nil && phaseResult.Status != "" {
		normalized = *phaseResult
		normalized.ReasonCodes = append([]string(nil), phaseResult.ReasonCodes...)
	} else if taskSummary != nil {
		reportKey := model.TaskPhaseReportKey(taskId, canonicalPhase)
		if candidate, ok := taskSummary.Phases[reportKey]; ok && candidate.Status != "" {
			normalized = candidate
		}
		if normalized.Status == "" {
			for _, candidate := range taskSummary.Phases {
				candidatePhase := canonicalTaskPhaseName(candidate.Phase)
				if candidate.Status != "" && candidatePhase == canonicalPhase && (candidate.SubTaskId == "" || candidate.SubTaskId == taskId) {
					normalized = candidate
					break
				}
			}
		}
		if normalized.Status != "" {
			if normalized.Assets == 0 {
				normalized.Assets = taskSummary.Assets
			}
			if normalized.Vulnerabilities == 0 {
				normalized.Vulnerabilities = taskSummary.Vulnerabilities
			}
			if normalized.VulnerabilityConclusion == "" {
				normalized.VulnerabilityConclusion = taskSummary.VulnerabilityConclusion
			}
			normalized.ReasonCodes = appendUniqueReasonCodes(normalized.ReasonCodes, taskSummary.WarningCodes...)
		}
	}

	if normalized.Status == "" {
		normalized = model.TaskPhaseSummary{
			Status:      "UNKNOWN",
			ReasonCodes: []string{"legacy_summary_missing"},
		}
	}
	normalized.SubTaskId = taskId
	if normalized.Phase == "" {
		normalized.Phase = canonicalPhase
	} else {
		normalized.Phase = canonicalTaskPhaseName(normalized.Phase)
	}
	normalized.Weight = incrAmount
	return normalized
}

func appendUniqueReasonCodes(existing []string, values ...string) []string {
	result := append([]string(nil), existing...)
	seen := make(map[string]struct{}, len(result))
	for _, value := range result {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// IncrSubTaskDone holds a unique exact-lease guard across all bounded Mongo
// mutations, aligning HTTP reporting with the direct scheduler path. Once the
// phase write installs durable reconciliation ownership, a completed child may
// release its exact lease while semantic finalization continues asynchronously.
func (s *ServiceContext) IncrSubTaskDone(ctx context.Context, taskId, mainTaskId, leaseToken, phase string, isCompleted bool, incrAmount int, phaseResult *model.TaskPhaseSummary, taskSummary *model.TaskScanSummary) (*IncrSubTaskDoneResult, error) {
	if taskId == "" || mainTaskId == "" || leaseToken == "" || s.Scheduler == nil {
		return nil, scheduler.ErrTaskLeaseConflict
	}
	operation, err := s.Scheduler.BeginLeaseOperation(ctx, taskId, leaseToken)
	if err != nil {
		if errors.Is(err, scheduler.ErrTaskLeaseConflict) && isCompleted {
			closedTask, confirmErr := s.Scheduler.ConfirmClosedTaskLease(ctx, taskId, leaseToken, "COMPLETED")
			if confirmErr == nil {
				if closedTask.MainTaskId != mainTaskId {
					return nil, scheduler.ErrTaskLeaseConflict
				}
				if !isValidObjectID(mainTaskId) {
					return &IncrSubTaskDoneResult{
						Success: true, Message: "ok (quick validation task)", SubTaskDone: 1, SubTaskCount: 1,
						AllDone: true, Recorded: true, LeaseClosed: true, Finalized: true,
					}, nil
				}
				mongoCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				task, findErr := model.NewMainTaskModel(s.MongoDB).FindById(mongoCtx, mainTaskId)
				if findErr != nil {
					return nil, findErr
				}
				if task == nil || closedTask.DispatchGeneration == "" || task.DispatchGeneration != closedTask.DispatchGeneration {
					return nil, scheduler.ErrTaskLeaseConflict
				}
				semanticTerminal, runnable, stateErr := workerCompletionDispatchState(task, closedTask.DispatchGeneration)
				if stateErr != nil {
					return nil, stateErr
				}
				allDone := task.SubTaskDone >= task.SubTaskCount
				return &IncrSubTaskDoneResult{
					Success: true, Message: "ok", SubTaskDone: int32(task.SubTaskDone), SubTaskCount: int32(task.SubTaskCount),
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
		if releaseErr := s.Scheduler.ReleaseLeaseOperation(releaseCtx, operation); releaseErr != nil {
			logx.Errorf("[IncrSubTaskDone] release operation guard failed: %v", releaseErr)
		}
	}()

	var leasedTask scheduler.TaskInfo
	if err := json.Unmarshal([]byte(operation.TaskInfoData), &leasedTask); err != nil {
		return nil, fmt.Errorf("%w: decode leased task payload: %v", scheduler.ErrTaskLeaseConflict, err)
	}
	if leasedTask.TaskId != taskId || leasedTask.MainTaskId != mainTaskId {
		return nil, fmt.Errorf("%w: leased task identity does not match phase report", scheduler.ErrTaskLeaseConflict)
	}
	closeCompletedLease := func() (bool, error) {
		if !isCompleted {
			return false, nil
		}
		if _, closeErr := s.Scheduler.UpdateLeasedTaskWithOperation(
			ctx, operation, operation.WorkerName, "COMPLETED", "", phase, 100,
		); closeErr != nil {
			if _, confirmErr := s.Scheduler.ConfirmClosedTaskLease(ctx, taskId, leaseToken, "COMPLETED"); confirmErr == nil {
				return true, nil
			}
			return false, closeErr
		}
		return true, nil
	}
	if !isValidObjectID(mainTaskId) {
		leaseClosed, closeErr := closeCompletedLease()
		if closeErr != nil {
			return nil, fmt.Errorf("close quick validation task lease: %w", closeErr)
		}
		return &IncrSubTaskDoneResult{
			Success: true, Message: "ok (quick validation task)", SubTaskDone: 1, SubTaskCount: 1,
			AllDone: true, Recorded: true, LeaseClosed: leaseClosed, Finalized: true,
		}, nil
	}
	if leasedTask.DispatchGeneration == "" || leasedTask.DispatchGeneration != operation.DispatchGeneration {
		return nil, scheduler.ErrTaskLeaseConflict
	}

	mongoCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	taskModel := model.NewMainTaskModel(s.MongoDB)
	if incrAmount <= 0 {
		incrAmount = 1
	}
	canonicalPhase := canonicalTaskPhaseName(phase)
	normalizedPhase := normalizeWorkerPhaseSummary(taskId, phase, incrAmount, phaseResult, taskSummary)
	normalizedPhase.LeaseGeneration = scheduler.LeaseGenerationHash(leaseToken)
	reportIdentity := canonicalPhase
	if isCompleted {
		reportIdentity = "complete"
	} else if reportIdentity == "complete" {
		// Only an explicit completed report may own the canonical final key.
		reportIdentity = "complete-intermediate"
	}
	reportKey := model.TaskPhaseReportKey(taskId, reportIdentity)
	task, recorded, err := taskModel.RecordPhaseSummaryForDispatch(mongoCtx, mainTaskId,
		leasedTask.DispatchGeneration, reportKey, normalizedPhase, incrAmount)
	if errors.Is(err, model.ErrTaskDispatchConflict) || errors.Is(err, model.ErrTaskParentFenced) {
		current, findErr := taskModel.FindById(mongoCtx, mainTaskId)
		if findErr != nil {
			return nil, findErr
		}
		return nil, workerDispatchStateError(current, leasedTask.DispatchGeneration)
	}
	if err != nil {
		return nil, err
	}
	if !recorded {
		logx.Infof("[IncrSubTaskDone] duplicate phase report accepted without payload overwrite, mainTaskId=%s taskId=%s phase=%s",
			mainTaskId, taskId, canonicalPhase)
	}

	allDone := task.SubTaskDone >= task.SubTaskCount
	if model.IsSemanticTerminalTaskStatus(task.Status) {
		leaseClosed, closeErr := closeCompletedLease()
		if closeErr != nil {
			return nil, fmt.Errorf("close completed task lease: %w", closeErr)
		}
		return &IncrSubTaskDoneResult{
			Success: true, Message: "ok", SubTaskDone: int32(task.SubTaskDone), SubTaskCount: int32(task.SubTaskCount),
			AllDone: allDone, Recorded: true, LeaseClosed: leaseClosed, Finalized: true, ScanSummary: task.ScanSummary,
		}, nil
	}
	progress := 0
	if task.SubTaskCount > 0 {
		progress = task.SubTaskDone * 100 / task.SubTaskCount
		if progress > 100 {
			progress = 100
		}
	}
	matched, err := taskModel.UpdateDispatchProgress(mongoCtx, mainTaskId,
		leasedTask.DispatchGeneration, phase, progress)
	if err != nil {
		return nil, err
	}
	if !matched {
		current, findErr := taskModel.FindById(mongoCtx, mainTaskId)
		if findErr != nil {
			return nil, findErr
		}
		semanticTerminal, _, stateErr := workerCompletionDispatchState(current, leasedTask.DispatchGeneration)
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
		return &IncrSubTaskDoneResult{
			Success: true, Message: "ok", SubTaskDone: int32(current.SubTaskDone), SubTaskCount: int32(current.SubTaskCount),
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
			mongoCtx, mainTaskId, leasedTask.DispatchGeneration)
		switch {
		case finalizeErr == nil:
			// updated=false with nil error now means another actor already made
			// this exact generation semantic-terminal, not that work is pending.
			finalSummary = summary
			finalized = true
			closeLease = true
			if updated {
				logx.Infof("[IncrSubTaskDone] task finalized, mainTaskId=%s, outcome=%s", mainTaskId, summary.Outcome)
				if s.QueryCache != nil {
					s.QueryCache.Clear()
				}
				if s.RedisClient != nil {
					notifySvc := notification.NewService(s.MongoDB, s.RedisClient)
					if nerr := notifySvc.NotifyTaskCompleted(mongoCtx, mainTaskId, summary.Outcome); nerr != nil {
						logx.Errorf("[IncrSubTaskDone] notification failed, mainTaskId=%s, error=%v", mainTaskId, nerr)
					}
				}
			}
		case errors.Is(finalizeErr, model.ErrTaskFinalizationPending):
			finalSummary = summary
			finalizationPending = true
			current, findErr := taskModel.FindById(mongoCtx, mainTaskId)
			if findErr != nil {
				return nil, findErr
			}
			semanticTerminal, runnable, stateErr := workerCompletionDispatchState(current, leasedTask.DispatchGeneration)
			if stateErr != nil {
				return nil, stateErr
			}
			if semanticTerminal {
				finalized = true
				finalizationPending = false
				finalSummary = current.ScanSummary
			}
			// The durable marker owns retry while a same-generation runnable
			// parent permits this exact completed child to release its lease.
			closeLease = semanticTerminal || runnable
		default:
			if errors.Is(finalizeErr, model.ErrTaskDispatchConflict) || errors.Is(finalizeErr, model.ErrTaskParentFenced) {
				current, findErr := taskModel.FindById(mongoCtx, mainTaskId)
				if findErr != nil {
					return nil, findErr
				}
				return nil, workerDispatchStateError(current, leasedTask.DispatchGeneration)
			}
			finalizationPending = true
			logx.Errorf("[IncrSubTaskDone] semantic finalization failed; durable reconciliation retains ownership, mainTaskId=%s, error=%v", mainTaskId, finalizeErr)
			current, findErr := taskModel.FindById(mongoCtx, mainTaskId)
			if findErr != nil {
				return nil, findErr
			}
			semanticTerminal, _, stateErr := workerCompletionDispatchState(current, leasedTask.DispatchGeneration)
			if stateErr != nil {
				return nil, stateErr
			}
			if semanticTerminal {
				finalized = true
				finalizationPending = false
				finalSummary = current.ScanSummary
				closeLease = true
			}
			// On unresolved Mongo uncertainty the exact lease may remain for the
			// reconciler; the marker, not worker retries, owns eventual progress.
		}
	}

	leaseClosed := false
	if closeLease {
		leaseClosed, err = closeCompletedLease()
		if err != nil {
			return nil, fmt.Errorf("close completed task lease: %w", err)
		}
	}
	return &IncrSubTaskDoneResult{
		Success: true, Message: "ok", SubTaskDone: int32(task.SubTaskDone), SubTaskCount: int32(task.SubTaskCount),
		AllDone: allDone, Recorded: true, LeaseClosed: leaseClosed, Finalized: finalized,
		FinalizationPending: finalizationPending, ScanSummary: finalSummary,
	}, nil
}

func canonicalTaskPhaseName(phase string) string {
	switch phase {
	case "子域名扫描":
		return "domainscan"
	case "端口扫描":
		return "portscan"
	case "端口识别":
		return "portidentify"
	case "指纹识别":
		return "fingerprint"
	case "弱口令扫描":
		return "brutescan"
	case "目录扫描":
		return "dirscan"
	case "JS扫描":
		return "jsfinder"
	case "漏洞扫描":
		return "poc"
	case "完成":
		return "complete"
	default:
		if phase == "" {
			return "unknown"
		}
		return phase
	}
}
