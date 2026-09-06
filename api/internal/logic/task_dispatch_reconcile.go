package logic

import (
	"context"
	"errors"
	"fmt"

	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/scheduler"
)

const pendingDispatchReconcileLimit int64 = 500

func pendingDispatchPlan(task *model.MainTask, definitions []model.ExecutorTask) ([]*scheduler.TaskInfo, error) {
	if task == nil || task.Id.IsZero() || task.DispatchGeneration == "" || task.DispatchCreateTime == nil ||
		task.DispatchCreateTime.IsZero() {
		return nil, fmt.Errorf("pending dispatch identity, generation, and create time are required")
	}
	topology, ordered, err := exactDefinitionManifest(task, definitions)
	if err != nil {
		return nil, err
	}
	if task.BatchCount > 0 && topology.count != task.BatchCount {
		return nil, fmt.Errorf("pending dispatch manifest has %d batches, expected %d", topology.count, task.BatchCount)
	}

	stableCreateTime := task.DispatchCreateTime.Local().Format("2006-01-02 15:04:05")
	tasks := make([]*scheduler.TaskInfo, 0, len(ordered))
	for index, definition := range ordered {
		if definition.DispatchGeneration != task.DispatchGeneration {
			return nil, fmt.Errorf("definition %s belongs to a superseded dispatch", definition.TaskId)
		}
		childID := topology.childIDs[index]
		tasks = append(tasks, &scheduler.TaskInfo{
			TaskId:             childID,
			MainTaskId:         task.Id.Hex(),
			TaskName:           firstNonEmpty(definition.TaskName, task.Name),
			Config:             definition.Config,
			Priority:           definition.Priority,
			CreateTime:         stableCreateTime,
			Workers:            workersFromConfig(definition.Config),
			DispatchGeneration: task.DispatchGeneration,
		})
	}
	return tasks, nil
}

func reconcilePendingDispatch(ctx context.Context, svcCtx *svc.ServiceContext, task *model.MainTask) error {
	if task == nil || svcCtx == nil || svcCtx.Scheduler == nil {
		return fmt.Errorf("pending dispatch reconciliation is not initialized")
	}
	manifest, err := model.NewTaskDispatchManifestModel(svcCtx.MongoDB).Find(
		ctx, task.Id.Hex(), task.DispatchGeneration,
	)
	if err != nil {
		return fmt.Errorf("load immutable dispatch manifest: %w", err)
	}
	var definitions []model.ExecutorTask
	if manifest != nil {
		if manifest.DispatchIntent != task.DispatchIntent || task.DispatchCreateTime == nil ||
			manifest.DispatchCreateTime.UTC().UnixMilli() != task.DispatchCreateTime.UTC().UnixMilli() {
			return fmt.Errorf("immutable manifest does not match the active parent dispatch")
		}
		definitions = manifest.Definitions
		if err := svcCtx.GetExecutorTaskModel().ActivateDispatchDefinitions(
			ctx, task.Id.Hex(), task.DispatchGeneration, manifest.DispatchCreateTime, definitions,
		); err != nil {
			return fmt.Errorf("repair active executor dispatch: %w", err)
		}
	} else {
		// Rollout compatibility: an already-claimed generation from the prior
		// process version is usable only when its exact active definitions exist.
		definitions, err = svcCtx.GetExecutorTaskModel().FindBatchDefinitionsForDispatch(
			ctx, task.Id.Hex(), task.DispatchGeneration,
		)
		if err != nil {
			return fmt.Errorf("load active executor manifest: %w", err)
		}
	}
	tasks, err := pendingDispatchPlan(task, definitions)
	if err != nil {
		return fmt.Errorf("validate active executor manifest: %w", err)
	}

	stillPending, err := svcCtx.GetMainTaskModel().IsPendingDispatch(ctx, task.Id.Hex(), task.DispatchGeneration)
	if err != nil {
		return fmt.Errorf("recheck active pending dispatch: %w", err)
	}
	if !stillPending {
		return nil
	}

	switch task.DispatchIntent {
	case model.DispatchIntentInitial:
		markerKey := fmt.Sprintf("cscan:task:publish:%s:%s", task.Id.Hex(), task.DispatchGeneration)
		if err := svcCtx.Scheduler.PushTaskBatchOnce(ctx, tasks, markerKey); err != nil {
			return fmt.Errorf("republish initial dispatch: %w", err)
		}
	case model.DispatchIntentResume:
		markerKey := fmt.Sprintf("cscan:task:resume:%s:%s", task.Id.Hex(), task.DispatchGeneration)
		if err := svcCtx.Scheduler.ResumeTaskBatch(ctx, tasks, nil, markerKey); err != nil {
			return fmt.Errorf("republish resumed dispatch: %w", err)
		}
	default:
		return fmt.Errorf("unknown durable dispatch intent %q", task.DispatchIntent)
	}
	return nil
}

// ReconcilePendingDispatches republishes only durable PENDING generations from
// their exact executor manifests. Redis markers make ambiguous retries
// idempotent; the acquisition-side Mongo gate remains the final stop/supersede
// fence across the two stores.
func ReconcilePendingDispatches(ctx context.Context, svcCtx *svc.ServiceContext) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	pending, err := svcCtx.GetMainTaskModel().FindPendingDispatches(ctx, pendingDispatchReconcileLimit)
	if err != nil {
		return 0, fmt.Errorf("find pending dispatches: %w", err)
	}

	reconciled := 0
	var reconcileErrs []error
	for index := range pending {
		if err := ctx.Err(); err != nil {
			return reconciled, err
		}
		snapshot := &pending[index]
		claimed, ok, claimErr := svcCtx.GetMainTaskModel().ClaimPendingDispatchReconciliation(ctx, snapshot)
		if claimErr != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf(
				"task %s generation %s claim: %w", snapshot.Id.Hex(), snapshot.DispatchGeneration, claimErr,
			))
			continue
		}
		if !ok {
			continue
		}
		if err := reconcilePendingDispatch(ctx, svcCtx, claimed); err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf(
				"task %s generation %s: %w", claimed.Id.Hex(), claimed.DispatchGeneration, err,
			))
			continue
		}
		reconciled++
	}
	return reconciled, errors.Join(reconcileErrs...)
}
