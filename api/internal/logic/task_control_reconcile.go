package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/scheduler"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

const controlIntentReconcileLimit int64 = 500

func sameControlIntent(left, right *model.TaskControlIntent) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.IntentID == right.IntentID && left.Action == right.Action &&
		left.DispatchGeneration == right.DispatchGeneration &&
		left.CreatedTime.UTC().UnixMilli() == right.CreatedTime.UTC().UnixMilli()
}

func expectedControlStatus(action string) string {
	switch action {
	case model.TaskControlActionPause:
		return model.TaskStatusPaused
	case model.TaskControlActionStop:
		return model.TaskStatusStopped
	default:
		return ""
	}
}

func isCurrentControlIntent(task *model.MainTask, intent *model.TaskControlIntent) bool {
	return task != nil && intent != nil && sameControlIntent(task.ControlIntent, intent) &&
		task.DispatchGeneration == intent.DispatchGeneration && task.Status == expectedControlStatus(intent.Action)
}

func controlIntentChildIDs(ctx context.Context, svcCtx *svc.ServiceContext, task *model.MainTask, intent *model.TaskControlIntent) ([]string, error) {
	if task == nil || intent == nil || task.Id.IsZero() || !intent.IsValid() {
		return nil, fmt.Errorf("control reconciliation requires a valid parent and intent")
	}
	manifest, err := model.NewTaskDispatchManifestModel(svcCtx.MongoDB).Find(
		ctx, task.Id.Hex(), intent.DispatchGeneration,
	)
	if err != nil {
		return nil, fmt.Errorf("load immutable dispatch manifest: %w", err)
	}
	var definitions []model.ExecutorTask
	if manifest != nil {
		definitions = manifest.Definitions
	} else {
		definitions, err = svcCtx.GetExecutorTaskModel().FindBatchDefinitionsForDispatch(
			ctx, task.Id.Hex(), intent.DispatchGeneration,
		)
		if err != nil {
			return nil, fmt.Errorf("load exact active executor definitions: %w", err)
		}
	}
	for _, definition := range definitions {
		if definition.DispatchGeneration != intent.DispatchGeneration {
			return nil, fmt.Errorf("executor definition %s belongs to another dispatch generation", definition.TaskId)
		}
	}
	topology, _, err := exactDefinitionManifest(task, definitions)
	if err != nil {
		return nil, fmt.Errorf("validate exact control topology: %w", err)
	}
	if task.BatchCount > 0 && topology.count != task.BatchCount {
		return nil, fmt.Errorf("control topology has %d children, expected %d", topology.count, task.BatchCount)
	}
	return append([]string(nil), topology.childIDs...), nil
}

func controlEnvelope(task *model.MainTask, intent *model.TaskControlIntent, childID string) scheduler.TaskControlEnvelope {
	return scheduler.TaskControlEnvelope{
		IntentID:           intent.IntentID,
		MainTaskID:         task.Id.Hex(),
		TaskID:             childID,
		Action:             intent.Action,
		DispatchGeneration: intent.DispatchGeneration,
		Timestamp:          intent.CreatedTime.UTC().Truncate(time.Millisecond),
	}
}

func refreshControlIntent(ctx context.Context, svcCtx *svc.ServiceContext, taskID string, intent *model.TaskControlIntent) (*model.MainTask, bool, error) {
	current, err := svcCtx.GetMainTaskModel().FindById(ctx, taskID)
	if err != nil {
		return nil, false, err
	}
	if current == nil || !sameControlIntent(current.ControlIntent, intent) {
		return current, false, nil
	}
	return current, isCurrentControlIntent(current, intent), nil
}

// reconcileControlIntent revalidates one durable intent before every Redis
// decision. Obsolete intents may be exact-cleared but are never published.
func reconcileControlIntent(ctx context.Context, svcCtx *svc.ServiceContext, snapshot *model.MainTask) error {
	if snapshot == nil || snapshot.ControlIntent == nil {
		return nil
	}
	intent := *snapshot.ControlIntent
	intent.CreatedTime = intent.CreatedTime.UTC().Truncate(time.Millisecond)
	if !intent.IsValid() || expectedControlStatus(intent.Action) == "" {
		return fmt.Errorf("durable control intent is malformed")
	}

	current, active, err := refreshControlIntent(ctx, svcCtx, snapshot.Id.Hex(), &intent)
	if err != nil {
		return fmt.Errorf("revalidate durable control intent: %w", err)
	}
	if current == nil || !sameControlIntent(current.ControlIntent, &intent) {
		return nil
	}
	if !active {
		if _, err := svcCtx.GetMainTaskModel().ClearControlIntentExact(ctx, snapshot.Id.Hex(), &intent); err != nil {
			return fmt.Errorf("clear obsolete control intent: %w", err)
		}
		return nil
	}

	childIDs, err := controlIntentChildIDs(ctx, svcCtx, current, &intent)
	if err != nil {
		return err
	}
	quiescent, err := svcCtx.Scheduler.IsTaskBatchQuiescent(ctx, childIDs)
	if err != nil {
		return fmt.Errorf("check exact control quiescence: %w", err)
	}

	current, active, err = refreshControlIntent(ctx, svcCtx, snapshot.Id.Hex(), &intent)
	if err != nil {
		return fmt.Errorf("revalidate control intent after topology check: %w", err)
	}
	if current == nil || !sameControlIntent(current.ControlIntent, &intent) {
		return nil
	}
	if !active {
		if _, err := svcCtx.GetMainTaskModel().ClearControlIntentExact(ctx, snapshot.Id.Hex(), &intent); err != nil {
			return fmt.Errorf("clear newly obsolete control intent: %w", err)
		}
		return nil
	}

	if quiescent {
		cleared, err := svcCtx.GetMainTaskModel().ClearControlIntentExact(ctx, current.Id.Hex(), &intent)
		if err != nil {
			return fmt.Errorf("clear acknowledged control intent: %w", err)
		}
		if !cleared {
			return nil
		}
		var deleteErrs []error
		for _, childID := range childIDs {
			if _, err := svcCtx.Scheduler.DeleteTaskControlExact(ctx, controlEnvelope(current, &intent, childID)); err != nil {
				deleteErrs = append(deleteErrs, fmt.Errorf("delete exact control for %s: %w", childID, err))
			}
		}
		if len(deleteErrs) > 0 {
			// The Mongo clear necessarily precedes exact Redis deletion. Restore
			// the same intent on ambiguous cleanup failure, but only if no racing
			// status/generation/intent replaced it.
			restoreCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			restored, restoreErr := svcCtx.GetMainTaskModel().RestoreControlIntentExact(
				restoreCtx, current.Id.Hex(), &intent,
			)
			cancel()
			if restoreErr != nil {
				deleteErrs = append(deleteErrs, fmt.Errorf("restore control intent after Redis uncertainty: %w", restoreErr))
			} else if !restored {
				// A racing STOP/resume makes this old cleanup obsolete; exact Redis
				// keys remain generation-scoped and cannot affect the replacement.
				logx.Infof("[ControlIntent] Cleanup intent %s became obsolete before it could be restored", intent.IntentID)
			}
		}
		return errors.Join(deleteErrs...)
	}

	// A final exact parent check immediately precedes publication. Redis also
	// enforces STOP-over-PAUSE if the parent changes after this cross-store fence.
	current, active, err = refreshControlIntent(ctx, svcCtx, snapshot.Id.Hex(), &intent)
	if err != nil {
		return fmt.Errorf("revalidate control intent before replay: %w", err)
	}
	if !active {
		return nil
	}
	var publishErrs []error
	for _, childID := range childIDs {
		err := svcCtx.Scheduler.PublishTaskControl(ctx, controlEnvelope(current, &intent, childID))
		if errors.Is(err, scheduler.ErrTaskControlSuperseded) {
			return nil
		}
		if err != nil {
			publishErrs = append(publishErrs, fmt.Errorf("publish exact control for %s: %w", childID, err))
		}
	}
	return errors.Join(publishErrs...)
}

// ReconcileControlIntents repairs missed PAUSE/STOP delivery from Mongo. The
// durable intent remains present across every publication error or uncertainty
// and is cleared only after every canonical child is quiescent.
func ReconcileControlIntents(ctx context.Context, svcCtx *svc.ServiceContext) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	intents, err := svcCtx.GetMainTaskModel().FindControlIntents(ctx, controlIntentReconcileLimit)
	if err != nil {
		return 0, fmt.Errorf("find durable control intents: %w", err)
	}
	reconciled := 0
	var reconcileErrs []error
	for index := range intents {
		if err := ctx.Err(); err != nil {
			return reconciled, err
		}
		snapshot := &intents[index]
		claimed, ok, claimErr := svcCtx.GetMainTaskModel().ClaimControlIntentReconciliation(
			ctx, snapshot.Id.Hex(), snapshot.ControlIntent,
		)
		if claimErr != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("task %s claim: %w", snapshot.Id.Hex(), claimErr))
			continue
		}
		if !ok {
			continue
		}
		if err := reconcileControlIntent(ctx, svcCtx, claimed); err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("task %s: %w", claimed.Id.Hex(), err))
			continue
		}
		reconciled++
	}
	return reconciled, errors.Join(reconcileErrs...)
}

// ReconcileControlIntentSoon is a bounded request-independent latency
// optimization. Periodic reconciliation remains authoritative if it fails.
func ReconcileControlIntentSoon(svcCtx *svc.ServiceContext, taskID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		task, err := svcCtx.GetMainTaskModel().FindById(ctx, taskID)
		if err == nil && task != nil && task.ControlIntent != nil {
			var claimed bool
			task, claimed, err = svcCtx.GetMainTaskModel().ClaimControlIntentReconciliation(
				ctx, taskID, task.ControlIntent,
			)
			if err == nil && claimed {
				err = reconcileControlIntent(ctx, svcCtx, task)
			}
		}
		if err != nil {
			logx.Errorf("[ControlIntent] Immediate reconciliation failed for %s: %v", taskID, err)
		}
	}()
}

// PublishDeleteStopSoon replaces the legacy plaintext delete notification with
// an exact-generation envelope. Deletion itself removes the durable parent, so
// this remains a bounded best-effort cancellation aid rather than a control
// intent; the missing parent is still the fail-closed execution fence.
func PublishDeleteStopSoon(ctx context.Context, svcCtx *svc.ServiceContext, task *model.MainTask) {
	if task == nil || task.DispatchGeneration == "" {
		return
	}
	intent := &model.TaskControlIntent{
		IntentID:           uuid.NewString(),
		Action:             model.TaskControlActionStop,
		DispatchGeneration: task.DispatchGeneration,
		CreatedTime:        time.Now().UTC().Truncate(time.Millisecond),
	}
	childIDs, err := controlIntentChildIDs(ctx, svcCtx, task, intent)
	if err != nil {
		logx.Errorf("[ControlIntent] Cannot construct exact delete controls for %s: %v", task.Id.Hex(), err)
		return
	}
	envelopes := make([]scheduler.TaskControlEnvelope, 0, len(childIDs))
	for _, childID := range childIDs {
		envelopes = append(envelopes, controlEnvelope(task, intent, childID))
	}
	go func() {
		publishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, envelope := range envelopes {
			if err := svcCtx.Scheduler.PublishTaskControl(publishCtx, envelope); err != nil &&
				!errors.Is(err, scheduler.ErrTaskControlSuperseded) {
				logx.Errorf("[ControlIntent] Exact delete control failed for %s generation %s: %v",
					envelope.TaskID, envelope.DispatchGeneration, err)
			}
		}
	}()
}
