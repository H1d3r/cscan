package logic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"cscan/api/internal/svc"
	"cscan/internal/model"

	"go.mongodb.org/mongo-driver/bson"
)

// recoverStaleMainTasks uses Mongo only to select stale parents. Runnable work
// is taken exclusively from their current processing children and exact cached
// payloads; aggregate MainTask.Config is never used to synthesize a child.
func recoverStaleMainTasks(ctx context.Context, svcCtx *svc.ServiceContext, timeout time.Duration, component string) ([]RecoveredTaskInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tasks, err := svcCtx.GetMainTaskModel().Find(ctx, bson.M{
		"status":      model.TaskStatusStarted,
		"update_time": bson.M{"$lt": time.Now().Add(-timeout)},
	}, 0, 50)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("find stale parent tasks: %w", err)
	}
	if len(tasks) == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	staleParents := make(map[string]*resolvedRecoveryParent, len(tasks))
	expectedParentsByChild := make(map[string]string)
	var topologyErrs []error
	for index := range tasks {
		parent := &tasks[index]
		parentID := parent.Id.Hex()
		resolved, err := resolveRecoveryParentTopology(ctx, svcCtx, parent)
		if err != nil {
			topologyErrs = append(topologyErrs, fmt.Errorf(
				"resolve stale parent %s topology: %w", parentID, err,
			))
			continue
		}
		staleParents[parentID] = resolved
		for _, childID := range recoveryExpectedChildIDs(resolved.topology) {
			if existingParent, exists := expectedParentsByChild[childID]; exists && existingParent != parentID {
				topologyErrs = append(topologyErrs, fmt.Errorf(
					"processing child identity %s is shared by stale parents %s and %s",
					childID, existingParent, parentID,
				))
				continue
			}
			expectedParentsByChild[childID] = parentID
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(topologyErrs) > 0 {
		return nil, fmt.Errorf("stale recovery topology is ambiguous; retry required: %w", errors.Join(topologyErrs...))
	}

	records, err := snapshotRecoveryRecords(ctx, svcCtx)
	if err != nil {
		return nil, err
	}

	heartbeatCache := make(map[string]bool)
	candidates := make([]*recoveryCandidate, 0)
	var collectionErrs []error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// A child is relevant only when its exact payload names a queried stale
		// parent, or its canonical child ID belongs to one of those parents.
		// The latter lets a missing/corrupt cache invalidate the correct parent
		// instead of allowing its remaining siblings to move partially.
		relevantParents := make(map[string]struct{}, 2)
		if expectedParent := expectedParentsByChild[record.TaskID]; expectedParent != "" {
			relevantParents[expectedParent] = struct{}{}
		}
		if record.TaskInfo != nil {
			if _, stale := staleParents[record.TaskInfo.MainTaskId]; stale {
				relevantParents[record.TaskInfo.MainTaskId] = struct{}{}
			}
		}
		if len(relevantParents) == 0 {
			continue
		}

		orphaned, ownerErr, readErr := classifyRecoveryOwner(ctx, svcCtx, record, heartbeatCache)
		if readErr != nil {
			return nil, readErr
		}
		if !orphaned {
			// Stale Mongo timestamps never override a healthy execution owner.
			continue
		}

		expectedOwner := ""
		if record.Execution != nil {
			expectedOwner = record.Execution.WorkerName
		}
		parentIDs := make([]string, 0, len(relevantParents))
		for parentID := range relevantParents {
			parentIDs = append(parentIDs, parentID)
		}
		sort.Strings(parentIDs)
		for _, parentID := range parentIDs {
			candidate, err := newRecoveryCandidate(record, parentID, expectedOwner, ownerErr)
			if err != nil {
				collectionErrs = append(collectionErrs, err)
				continue
			}
			candidates = append(candidates, candidate)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(collectionErrs) > 0 {
		return nil, fmt.Errorf("stale recovery candidate collection failed; retry required: %w", errors.Join(collectionErrs...))
	}
	if len(candidates) == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	// Every stale-parent group is fully validated before any parent commits.
	// Valid parents can recover independently, but each parent is all-or-none.
	groups, err := prepareRecoveryGroups(ctx, svcCtx, candidates, staleParents)
	if err != nil {
		return nil, err
	}
	return commitRecoveryGroups(ctx, svcCtx, groups, component)
}
