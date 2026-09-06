package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/notification"
	"cscan/internal/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
)

const completionReconcileLimit int64 = 500

type completionReconciliationOutcome uint8

const (
	completionReconciliationAbsent completionReconciliationOutcome = iota
	completionReconciliationRunnablePending
	completionReconciliationRetired
	completionReconciliationQuiescent
	completionReconciliationTerminalNoFinalEvidence
	completionReconciliationTerminalPending
)

type completionLeaseEvidenceState uint8

const (
	completionLeaseEvidenceAbsent completionLeaseEvidenceState = iota
	completionLeaseEvidenceExact
	completionLeaseEvidenceConflict
)

var errNoFinalCompletionEvidence = errors.New("canonical final completion evidence is absent")

func completionReconciliationChildIDs(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	task *model.MainTask,
	generation string,
) ([]string, error) {
	if task == nil || task.Id.IsZero() || strings.TrimSpace(generation) == "" {
		return nil, fmt.Errorf("completion reconciliation requires a valid parent and generation")
	}
	manifest, err := model.NewTaskDispatchManifestModel(svcCtx.MongoDB).Find(
		ctx, task.Id.Hex(), generation,
	)
	if err != nil {
		return nil, fmt.Errorf("load immutable dispatch manifest: %w", err)
	}
	var definitions []model.ExecutorTask
	if manifest != nil {
		definitions = manifest.Definitions
	} else {
		definitions, err = svcCtx.GetExecutorTaskModel().FindBatchDefinitionsForDispatch(
			ctx, task.Id.Hex(), generation,
		)
		if err != nil {
			return nil, fmt.Errorf("load exact generation executor definitions: %w", err)
		}
	}
	for _, definition := range definitions {
		if definition.DispatchGeneration != generation {
			return nil, fmt.Errorf("executor definition %s belongs to another dispatch generation", definition.TaskId)
		}
	}
	topology, _, err := exactDefinitionManifest(task, definitions)
	if err != nil {
		return nil, fmt.Errorf("validate exact completion topology: %w", err)
	}
	if task.BatchCount > 0 && topology.count != task.BatchCount {
		return nil, fmt.Errorf("completion topology has %d children, expected %d", topology.count, task.BatchCount)
	}
	return append([]string(nil), topology.childIDs...), nil
}

// classifyCompletionLeaseEvidence distinguishes a genuinely absent canonical
// final key from exact non-bearer lease evidence and from conflicting evidence.
func classifyCompletionLeaseEvidence(
	task *model.MainTask,
	childID, leaseGeneration string,
) (completionLeaseEvidenceState, error) {
	if task == nil || strings.TrimSpace(childID) == "" || strings.TrimSpace(leaseGeneration) == "" {
		return completionLeaseEvidenceConflict, model.ErrTaskDispatchConflict
	}
	if task.ScanSummary == nil {
		return completionLeaseEvidenceAbsent, errNoFinalCompletionEvidence
	}
	phase, ok := task.ScanSummary.Phases[model.TaskPhaseReportKey(childID, "complete")]
	if !ok {
		return completionLeaseEvidenceAbsent, errNoFinalCompletionEvidence
	}
	if phase.SubTaskId == childID && phase.LeaseGeneration == leaseGeneration {
		return completionLeaseEvidenceExact, nil
	}
	return completionLeaseEvidenceConflict, fmt.Errorf(
		"%w: canonical final evidence conflicts for child %s", scheduler.ErrTaskLeaseConflict, childID,
	)
}

func releaseCompletionOperation(sched *scheduler.Scheduler, operation *scheduler.LeaseOperation) error {
	if sched == nil || operation == nil {
		return nil
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return sched.ReleaseLeaseOperation(releaseCtx, operation)
}

// closeReconciledCompletedLease converts only an exact currently-processing
// lease whose canonical completion hash is durable on the same terminal parent.
func closeReconciledCompletedLease(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	parent *model.MainTask,
	generation, childID string,
) (resultErr error) {
	if parent == nil || !model.HasCompletionReconciliation(parent, generation) ||
		parent.DispatchGeneration != generation || !model.IsSemanticTerminalTaskStatus(parent.Status) {
		return model.ErrTaskDispatchConflict
	}
	snapshot, err := svcCtx.Scheduler.SnapshotProcessingExecution(ctx, childID)
	if err != nil {
		return err
	}
	if snapshot == nil || snapshot.AlreadyQuiescent {
		return nil
	}
	if snapshot.TaskInfo.TaskId != childID || snapshot.TaskInfo.MainTaskId != parent.Id.Hex() ||
		snapshot.TaskInfo.DispatchGeneration != generation || snapshot.DispatchGeneration != generation {
		return scheduler.ErrTaskLeaseConflict
	}
	leaseGeneration := scheduler.LeaseGenerationHash(snapshot.LeaseToken)
	evidence, evidenceErr := classifyCompletionLeaseEvidence(parent, childID, leaseGeneration)
	if evidenceErr != nil {
		return evidenceErr
	}
	if evidence != completionLeaseEvidenceExact {
		return scheduler.ErrTaskLeaseConflict
	}

	operation, err := svcCtx.Scheduler.BeginLeaseOperation(ctx, childID, snapshot.LeaseToken)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := releaseCompletionOperation(svcCtx.Scheduler, operation); releaseErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("release exact completion operation: %w", releaseErr))
		}
	}()
	if operation == nil || operation.TaskID != childID || operation.LeaseToken != snapshot.LeaseToken ||
		operation.WorkerName != snapshot.WorkerName || operation.InstanceID != snapshot.InstanceID ||
		operation.TaskProtocol != snapshot.TaskProtocol || operation.DispatchGeneration != generation ||
		operation.TaskInfoData != snapshot.TaskInfoData {
		return scheduler.ErrTaskLeaseConflict
	}
	var guardedPayload scheduler.TaskInfo
	if err := json.Unmarshal([]byte(operation.TaskInfoData), &guardedPayload); err != nil ||
		guardedPayload.TaskId != childID || guardedPayload.MainTaskId != parent.Id.Hex() ||
		guardedPayload.DispatchGeneration != generation {
		return fmt.Errorf("%w: guarded payload identity changed", scheduler.ErrTaskLeaseConflict)
	}

	// The operation guard now excludes recovery and worker updates. Re-read the
	// durable authority and the same canonical lease-hash evidence before close.
	current, err := svcCtx.GetMainTaskModel().FindById(ctx, parent.Id.Hex())
	if err != nil {
		return err
	}
	if current == nil || !model.HasCompletionReconciliation(current, generation) ||
		current.DispatchGeneration != generation || !model.IsSemanticTerminalTaskStatus(current.Status) {
		return model.ErrTaskDispatchConflict
	}
	evidence, evidenceErr = classifyCompletionLeaseEvidence(current, childID, leaseGeneration)
	if evidenceErr != nil {
		return evidenceErr
	}
	if evidence != completionLeaseEvidenceExact {
		return scheduler.ErrTaskLeaseConflict
	}
	_, err = svcCtx.Scheduler.UpdateLeasedTaskWithOperation(
		ctx, operation, operation.WorkerName, "COMPLETED", "", snapshot.Phase, 100,
	)
	return err
}

func completionSummaryReady(task *model.MainTask) bool {
	if task == nil || !model.IsRunnableTaskStatus(task.Status) || task.SubTaskDone < task.SubTaskCount {
		return false
	}
	var phases map[string]model.TaskPhaseSummary
	if task.ScanSummary != nil {
		phases = task.ScanSummary.Phases
	}
	summary := model.AggregateTaskScanSummary(task.Status, task.SubTaskCount, phases)
	return summary.PhaseCount >= task.SubTaskCount
}

func retireCompletionReconciliation(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	taskID string,
	marker *model.TaskCompletionReconciliation,
) (bool, error) {
	return svcCtx.GetMainTaskModel().ClearCompletionReconciliationExact(ctx, taskID, marker)
}

func notifyReconciledCompletion(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	taskID string,
	summary *model.TaskScanSummary,
) {
	if summary == nil {
		return
	}
	if svcCtx.QueryCache != nil {
		svcCtx.QueryCache.Clear()
	}
	if svcCtx.RedisClient == nil {
		return
	}
	notifySvc := notification.NewService(svcCtx.MongoDB, svcCtx.RedisClient)
	if err := notifySvc.NotifyTaskCompleted(ctx, taskID, summary.Outcome); err != nil {
		logx.Errorf("[CompletionReconcile] notification failed for %s: %v", taskID, err)
	}
}

// reconcileTaskCompletionExact reconciles one captured parent/generation. Its
// outcome distinguishes a harmless incomplete runnable marker from a terminal
// generation whose exact final leases are still unresolved.
func reconcileTaskCompletionExact(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	taskID, generation string,
) (completionReconciliationOutcome, error) {
	taskID = strings.TrimSpace(taskID)
	generation = strings.TrimSpace(generation)
	if taskID == "" || generation == "" {
		return completionReconciliationAbsent, fmt.Errorf("exact completion reconciliation requires parent and generation")
	}
	current, err := svcCtx.GetMainTaskModel().FindById(ctx, taskID)
	if err != nil {
		return completionReconciliationAbsent, fmt.Errorf("revalidate completion parent: %w", err)
	}
	return reconcileTaskCompletionCurrent(ctx, svcCtx, current, taskID, generation)
}

// reconcileTaskCompletionCurrent starts from either an exact targeted read or
// the fresh post-claim task returned by the broad fairness scan.
func reconcileTaskCompletionCurrent(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	current *model.MainTask,
	taskID, generation string,
) (completionReconciliationOutcome, error) {
	var err error
	if current == nil || !model.HasCompletionReconciliation(current, generation) {
		return completionReconciliationAbsent, nil
	}
	if current.DispatchGeneration != generation {
		cleared, clearErr := retireCompletionReconciliation(
			ctx, svcCtx, taskID, current.CompletionReconciliation,
		)
		if clearErr != nil {
			return completionReconciliationRetired, clearErr
		}
		if !cleared {
			return completionReconciliationTerminalPending, nil
		}
		return completionReconciliationRetired, nil
	}

	if model.IsRunnableTaskStatus(current.Status) {
		if !completionSummaryReady(current) {
			return completionReconciliationRunnablePending, nil
		}
		summary, updated, finalizeErr := svcCtx.GetMainTaskModel().FinalizeFromScanSummaryForDispatch(
			ctx, taskID, generation,
		)
		switch {
		case finalizeErr == nil:
			if updated {
				notifyReconciledCompletion(ctx, svcCtx, taskID, summary)
			}
		case errors.Is(finalizeErr, model.ErrTaskFinalizationPending):
			return completionReconciliationRunnablePending, nil
		case errors.Is(finalizeErr, model.ErrTaskParentFenced), errors.Is(finalizeErr, model.ErrTaskDispatchConflict):
			// Re-read below and retire only the still-captured obsolete marker.
		default:
			return completionReconciliationRunnablePending, fmt.Errorf("finalize exact dispatch summary: %w", finalizeErr)
		}
		current, err = svcCtx.GetMainTaskModel().FindById(ctx, taskID)
		if err != nil {
			return completionReconciliationRunnablePending, fmt.Errorf("refresh completion parent after finalization: %w", err)
		}
		if current == nil || !model.HasCompletionReconciliation(current, generation) {
			return completionReconciliationAbsent, nil
		}
		if current.DispatchGeneration != generation {
			cleared, clearErr := retireCompletionReconciliation(
				ctx, svcCtx, taskID, current.CompletionReconciliation,
			)
			if clearErr != nil {
				return completionReconciliationRetired, clearErr
			}
			if !cleared {
				return completionReconciliationTerminalPending, nil
			}
			return completionReconciliationRetired, nil
		}
	}

	if !model.IsSemanticTerminalTaskStatus(current.Status) {
		// PAUSED/STOPPED/REVOKED and malformed inactive states must never be
		// semantic-finalized. Their dedicated control/recovery paths own cleanup.
		cleared, clearErr := retireCompletionReconciliation(
			ctx, svcCtx, taskID, current.CompletionReconciliation,
		)
		if clearErr != nil {
			return completionReconciliationRetired, clearErr
		}
		if !cleared {
			return completionReconciliationTerminalPending, nil
		}
		return completionReconciliationRetired, nil
	}

	childIDs, err := completionReconciliationChildIDs(ctx, svcCtx, current, generation)
	if err != nil {
		return completionReconciliationTerminalPending, err
	}
	var closeErrs []error
	noFinalEvidence := false
	for _, childID := range childIDs {
		if err := ctx.Err(); err != nil {
			return completionReconciliationTerminalPending, err
		}
		if err := closeReconciledCompletedLease(ctx, svcCtx, current, generation, childID); err != nil {
			if errors.Is(err, errNoFinalCompletionEvidence) {
				noFinalEvidence = true
				continue
			}
			closeErrs = append(closeErrs, fmt.Errorf("close exact completed lease %s: %w", childID, err))
		}
	}

	quiescent, quiescenceErr := svcCtx.Scheduler.IsTaskBatchQuiescent(ctx, childIDs)
	if quiescenceErr != nil {
		closeErrs = append(closeErrs, fmt.Errorf("check completion batch quiescence: %w", quiescenceErr))
		return completionReconciliationTerminalPending, errors.Join(closeErrs...)
	}
	if len(closeErrs) > 0 {
		// Busy/conflict/uncertain close attempts deliberately keep the marker even
		// if another actor happened to make the batch quiescent meanwhile.
		return completionReconciliationTerminalPending, errors.Join(closeErrs...)
	}
	if !quiescent {
		if noFinalEvidence {
			return completionReconciliationTerminalNoFinalEvidence, nil
		}
		return completionReconciliationTerminalPending, nil
	}
	current, err = svcCtx.GetMainTaskModel().FindById(ctx, taskID)
	if err != nil {
		return completionReconciliationTerminalPending, fmt.Errorf("revalidate quiescent completion parent: %w", err)
	}
	if current == nil || !model.HasCompletionReconciliation(current, generation) {
		return completionReconciliationQuiescent, nil
	}
	if current.DispatchGeneration != generation || !model.IsSemanticTerminalTaskStatus(current.Status) {
		cleared, clearErr := retireCompletionReconciliation(
			ctx, svcCtx, taskID, current.CompletionReconciliation,
		)
		if clearErr != nil {
			return completionReconciliationRetired, clearErr
		}
		if !cleared {
			return completionReconciliationTerminalPending, nil
		}
		return completionReconciliationRetired, nil
	}
	cleared, clearErr := svcCtx.GetMainTaskModel().ClearCompletionReconciliationExact(
		ctx, taskID, current.CompletionReconciliation,
	)
	if clearErr != nil {
		return completionReconciliationTerminalPending, fmt.Errorf("clear exact quiescent completion marker: %w", clearErr)
	}
	if !cleared {
		return completionReconciliationTerminalPending, nil
	}
	return completionReconciliationQuiescent, nil
}

func reconcileTaskCompletion(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	snapshot *model.MainTask,
) (completionReconciliationOutcome, error) {
	if snapshot == nil || snapshot.CompletionReconciliation == nil {
		return completionReconciliationAbsent, nil
	}
	generation := strings.TrimSpace(snapshot.CompletionReconciliation.DispatchGeneration)
	if generation == "" {
		return completionReconciliationAbsent, fmt.Errorf("durable completion reconciliation marker is malformed")
	}
	if snapshot.Id.IsZero() {
		return completionReconciliationAbsent, fmt.Errorf("durable completion reconciliation parent is malformed")
	}
	claimed, ok, err := svcCtx.GetMainTaskModel().ClaimCompletionReconciliation(
		ctx, snapshot.Id.Hex(), snapshot.CompletionReconciliation,
	)
	if err != nil {
		return completionReconciliationAbsent, fmt.Errorf("claim durable completion reconciliation: %w", err)
	}
	if !ok {
		return completionReconciliationAbsent, nil
	}
	return reconcileTaskCompletionCurrent(ctx, svcCtx, claimed, claimed.Id.Hex(), generation)
}

// ReconcileTaskCompletions repairs semantic finalization and exact completed
// lease cleanup from a bounded marker list. Exact callers use the same
// per-parent helper and inspect its pending outcome instead of treating nil as
// proof that a still-marked generation was resolved.
func ReconcileTaskCompletions(ctx context.Context, svcCtx *svc.ServiceContext) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if svcCtx == nil || svcCtx.Scheduler == nil || svcCtx.MongoDB == nil {
		return 0, fmt.Errorf("completion reconciliation is not initialized")
	}
	tasks, err := svcCtx.GetMainTaskModel().FindCompletionReconciliations(ctx, completionReconcileLimit)
	if err != nil {
		return 0, fmt.Errorf("find durable completion reconciliations: %w", err)
	}
	reconciled := 0
	var reconcileErrs []error
	for index := range tasks {
		if err := ctx.Err(); err != nil {
			return reconciled, err
		}
		outcome, err := reconcileTaskCompletion(ctx, svcCtx, &tasks[index])
		if err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("task %s: %w", tasks[index].Id.Hex(), err))
			continue
		}
		if outcome != completionReconciliationRunnablePending &&
			outcome != completionReconciliationTerminalNoFinalEvidence &&
			outcome != completionReconciliationTerminalPending {
			reconciled++
		}
	}
	return reconciled, errors.Join(reconcileErrs...)
}
