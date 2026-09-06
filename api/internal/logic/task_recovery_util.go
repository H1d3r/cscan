package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/scheduler"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	recoveryProcessingKey     = "cscan:task:processing"
	recoveryTaskInfoKeyPrefix = "cscan:task:info:"
	recoveryExecutionPrefix   = "cscan:task:execution:"
	recoveryInstanceKeyPrefix = "cscan:worker:instance:"
)

// RecoveredTaskInfo 恢复的任务信息
type RecoveredTaskInfo struct {
	TaskId     string `json:"taskId"`
	MainTaskId string `json:"mainTaskId"`
	Status     string `json:"status"`
	StartTime  string `json:"startTime"`
}

type recoveryExecutionInfo struct {
	TaskID             string `json:"taskId"`
	WorkerName         string `json:"workerName"`
	InstanceID         string `json:"instanceId"`
	TaskProtocol       int    `json:"taskProtocol"`
	LeaseToken         string `json:"leaseToken"`
	DispatchGeneration string `json:"dispatchGeneration"`
	Phase              string `json:"phase"`
}

type recoveryRecord struct {
	TaskID string

	TaskInfo       *scheduler.TaskInfo
	TaskInfoExists bool
	TaskInfoErr    error

	Execution       *recoveryExecutionInfo
	ExecutionExists bool
	ExecutionErr    error
}

type recoveryCandidate struct {
	record        *recoveryRecord
	parentID      string
	expectedOwner string
	validationErr error
}

type resolvedRecoveryParent struct {
	task     *model.MainTask
	topology *taskBatchTopology
}

type recoveryGroup struct {
	parentID   string
	resolved   *resolvedRecoveryParent
	candidates []*recoveryCandidate
	tasks      []*scheduler.TaskInfo
	infos      []RecoveredTaskInfo
	err        error
}

func isMongoRecoveryParentID(parentID string) bool {
	_, err := primitive.ObjectIDFromHex(strings.TrimSpace(parentID))
	return err == nil
}

// snapshotRecoveryRecords captures every currently processing task before any
// recovery commit. Redis transport errors abort collection so they cannot hide
// a sibling that should have been part of a parent batch.
func snapshotRecoveryRecords(ctx context.Context, svcCtx *svc.ServiceContext) ([]*recoveryRecord, error) {
	taskIDs, err := svcCtx.RedisClient.SMembers(ctx, recoveryProcessingKey).Result()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("list processing tasks: %w", err)
	}
	sort.Strings(taskIDs)

	records := make([]*recoveryRecord, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record := &recoveryRecord{TaskID: taskID}

		payload, err := svcCtx.RedisClient.Get(ctx, recoveryTaskInfoKeyPrefix+taskID).Result()
		switch {
		case err == nil:
			record.TaskInfoExists = true
			var taskInfo scheduler.TaskInfo
			if decodeErr := json.Unmarshal([]byte(payload), &taskInfo); decodeErr != nil {
				record.TaskInfoErr = fmt.Errorf("decode exact task payload %s: %w", taskID, decodeErr)
			} else {
				record.TaskInfo = &taskInfo
			}
		case errors.Is(err, redis.Nil):
			// Absence is retained in the snapshot and validated if this record
			// becomes a recovery candidate.
		default:
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("load exact task payload %s: %w", taskID, err)
		}

		executionData, err := svcCtx.RedisClient.Get(ctx, recoveryExecutionPrefix+taskID).Result()
		switch {
		case err == nil:
			record.ExecutionExists = true
			var execution recoveryExecutionInfo
			if decodeErr := json.Unmarshal([]byte(executionData), &execution); decodeErr != nil {
				record.ExecutionErr = fmt.Errorf("decode execution owner %s: %w", taskID, decodeErr)
			} else {
				record.Execution = &execution
			}
		case errors.Is(err, redis.Nil):
			// Missing execution ownership is not synthesised. If the record is
			// otherwise a candidate, validation fails before its parent commits.
		default:
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("load execution owner %s: %w", taskID, err)
		}

		records = append(records, record)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func newRecoveryCandidate(record *recoveryRecord, parentHint, expectedOwner string, validationErr error) (*recoveryCandidate, error) {
	if record == nil || record.TaskID == "" {
		return nil, fmt.Errorf("recovery candidate identity is required")
	}

	parentID := strings.TrimSpace(parentHint)
	if record.TaskInfo != nil {
		payloadParentID := strings.TrimSpace(record.TaskInfo.MainTaskId)
		if parentID == "" {
			parentID = payloadParentID
		} else if payloadParentID != "" && payloadParentID != parentID {
			validationErr = errors.Join(validationErr, fmt.Errorf(
				"exact payload parent %s does not match discovered parent %s",
				payloadParentID, parentID,
			))
		}
	}
	if parentID == "" {
		switch {
		case record.TaskInfoErr != nil:
			return nil, fmt.Errorf("cannot group recovery candidate %s: %w", record.TaskID, record.TaskInfoErr)
		case !record.TaskInfoExists:
			return nil, fmt.Errorf("cannot group recovery candidate %s: exact payload is missing", record.TaskID)
		default:
			return nil, fmt.Errorf("cannot group recovery candidate %s: exact payload has no parent identity", record.TaskID)
		}
	}

	return &recoveryCandidate{
		record:        record,
		parentID:      parentID,
		expectedOwner: expectedOwner,
		validationErr: validationErr,
	}, nil
}

func validateRecoveryExecution(candidate *recoveryCandidate) error {
	if candidate == nil || candidate.record == nil {
		return fmt.Errorf("recovery candidate is nil")
	}
	record := candidate.record
	validationErr := candidate.validationErr

	if !record.ExecutionExists {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution owner is missing"))
		return validationErr
	}
	if record.ExecutionErr != nil {
		validationErr = errors.Join(validationErr, record.ExecutionErr)
		return validationErr
	}
	if record.Execution == nil {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution owner is unavailable"))
		return validationErr
	}

	execution := record.Execution
	if execution.TaskID != record.TaskID {
		validationErr = errors.Join(validationErr, fmt.Errorf(
			"execution task identity %q does not match %q", execution.TaskID, record.TaskID,
		))
	}
	if strings.TrimSpace(execution.WorkerName) == "" {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution owner is empty"))
	}
	if execution.TaskProtocol != scheduler.TaskProtocolV1 {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution task protocol is not leased-task-v1"))
	}
	if strings.TrimSpace(execution.InstanceID) == "" {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution instance id is empty"))
	}
	if candidate.expectedOwner != "" && execution.WorkerName != candidate.expectedOwner {
		validationErr = errors.Join(validationErr, fmt.Errorf(
			"execution owner %q does not match expected owner %q",
			execution.WorkerName, candidate.expectedOwner,
		))
	}
	if strings.TrimSpace(execution.LeaseToken) == "" {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution lease token is empty"))
	}
	requiresDispatchGeneration := record.TaskInfo == nil || isMongoRecoveryParentID(record.TaskInfo.MainTaskId)
	if requiresDispatchGeneration && strings.TrimSpace(execution.DispatchGeneration) == "" {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution dispatch generation is empty"))
	}
	if strings.TrimSpace(execution.Phase) == "" {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution phase is empty"))
	}
	return validationErr
}

func prepareParentlessRecoveryTask(candidate *recoveryCandidate) (*scheduler.TaskInfo, error) {
	if candidate == nil || candidate.record == nil {
		return nil, fmt.Errorf("parentless recovery candidate is nil")
	}
	record := candidate.record
	validationErr := validateRecoveryExecution(candidate)
	if !record.TaskInfoExists {
		validationErr = errors.Join(validationErr, fmt.Errorf("exact task payload is missing"))
	} else if record.TaskInfoErr != nil {
		validationErr = errors.Join(validationErr, record.TaskInfoErr)
	} else if record.TaskInfo == nil {
		validationErr = errors.Join(validationErr, fmt.Errorf("exact task payload is unavailable"))
	} else {
		task := record.TaskInfo
		if task.TaskId != record.TaskID || strings.TrimSpace(task.MainTaskId) == "" ||
			strings.TrimSpace(task.Config) == "" || isMongoRecoveryParentID(task.MainTaskId) {
			validationErr = errors.Join(validationErr, fmt.Errorf("parentless task payload identity or config is invalid"))
		}
		if record.Execution != nil && task.DispatchGeneration != record.Execution.DispatchGeneration {
			validationErr = errors.Join(validationErr, fmt.Errorf("payload dispatch generation does not match execution ownership"))
		}
	}
	if validationErr != nil {
		return nil, validationErr
	}
	prepared := *record.TaskInfo
	prepared.LeaseToken = record.Execution.LeaseToken
	prepared.RecoveryInstanceID = record.Execution.InstanceID
	return &prepared, nil
}

func commitParentlessRecoveryCandidates(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	candidates []*recoveryCandidate,
	component string,
) ([]RecoveredTaskInfo, error) {
	recovered := make([]RecoveredTaskInfo, 0, len(candidates))
	var recoveryErrs []error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return recovered, err
		}
		task, err := prepareParentlessRecoveryTask(candidate)
		if err != nil {
			taskID := ""
			if candidate != nil && candidate.record != nil {
				taskID = candidate.record.TaskID
			}
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parentless task %s validation failed; retry required: %w", component, taskID, err,
			))
			continue
		}
		moved, err := svcCtx.Scheduler.RequeueExactTask(ctx, task)
		if err != nil {
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parentless task %s exact requeue failed; retry required: %w", component, task.TaskId, err,
			))
			continue
		}
		if !moved {
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parentless task %s exact requeue did not commit; retry required", component, task.TaskId,
			))
			continue
		}
		recovered = append(recovered, RecoveredTaskInfo{
			TaskId: task.TaskId, MainTaskId: task.MainTaskId, Status: "requeued",
		})
	}
	return recovered, errors.Join(recoveryErrs...)
}

// prepareRecoveryTask is deliberately read-only. The exact payload and
// execution record were captured from Redis before preparation; this function
// validates their parent/child identity against a proven topology, loads only
// the child's scoped snapshot, and attaches the observed lease token for the
// later batch commit.
func prepareRecoveryTask(ctx context.Context, svcCtx *svc.ServiceContext, resolved *resolvedRecoveryParent, candidate *recoveryCandidate) (*scheduler.TaskInfo, RecoveredTaskInfo, error) {
	if resolved == nil || resolved.task == nil || resolved.topology == nil || candidate == nil || candidate.record == nil {
		return nil, RecoveredTaskInfo{}, fmt.Errorf("exact recovery parent, topology, and candidate are required")
	}
	parent := resolved.task
	topology := resolved.topology
	record := candidate.record
	validationErr := validateRecoveryExecution(candidate)
	childIndex, isChild := topology.indexByID[record.TaskID]

	if !record.TaskInfoExists {
		validationErr = errors.Join(validationErr, fmt.Errorf("exact task payload is missing"))
	} else if record.TaskInfoErr != nil {
		validationErr = errors.Join(validationErr, record.TaskInfoErr)
	} else if record.TaskInfo == nil {
		validationErr = errors.Join(validationErr, fmt.Errorf("exact task payload is unavailable"))
	} else {
		taskInfo := record.TaskInfo
		if taskInfo.TaskId != record.TaskID {
			validationErr = errors.Join(validationErr, fmt.Errorf(
				"payload task identity %q does not match %q", taskInfo.TaskId, record.TaskID,
			))
		}
		if strings.TrimSpace(taskInfo.DispatchGeneration) == "" ||
			taskInfo.DispatchGeneration != record.Execution.DispatchGeneration {
			validationErr = errors.Join(validationErr, fmt.Errorf(
				"payload dispatch generation does not match exact execution ownership",
			))
		}
		if taskInfo.MainTaskId != parent.Id.Hex() || candidate.parentID != parent.Id.Hex() {
			validationErr = errors.Join(validationErr, fmt.Errorf(
				"payload parent identity %q does not match %q", taskInfo.MainTaskId, parent.Id.Hex(),
			))
		}
		if !isChild {
			validationErr = errors.Join(validationErr, fmt.Errorf(
				"task %s is not a child of parent task %s with resolved batch count %d",
				record.TaskID, parent.TaskId, topology.count,
			))
		} else {
			configIndex, configTotal, err := decodeBatchConfigTopology(taskInfo.Config)
			if err != nil {
				validationErr = errors.Join(validationErr, fmt.Errorf("decode exact task config: %w", err))
			} else if configIndex != childIndex || configTotal != topology.count {
				validationErr = errors.Join(validationErr, fmt.Errorf(
					"exact task config topology is index %d of %d, expected index %d of %d",
					configIndex, configTotal, childIndex, topology.count,
				))
			}
		}
	}
	if validationErr != nil {
		return nil, RecoveredTaskInfo{}, validationErr
	}

	resumeState, err := svcCtx.GetExecutorTaskModel().FindTaskState(ctx, parent.Id.Hex(), record.TaskID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, RecoveredTaskInfo{}, ctxErr
		}
		return nil, RecoveredTaskInfo{}, fmt.Errorf("load scoped snapshot: %w", err)
	}
	if resumeState == "" && topology.count == 1 && record.TaskID == parent.TaskId {
		resumeState = parent.TaskState
	}
	prepared, err := injectResumeState(record.TaskInfo, resumeState)
	if err != nil {
		return nil, RecoveredTaskInfo{}, fmt.Errorf("inject scoped snapshot: %w", err)
	}
	prepared.LeaseToken = record.Execution.LeaseToken
	prepared.RecoveryInstanceID = record.Execution.InstanceID

	startTime := ""
	if parent.StartTime != nil {
		startTime = parent.StartTime.Format("2006-01-02 15:04:05")
	}
	return prepared, RecoveredTaskInfo{
		TaskId:     record.TaskID,
		MainTaskId: parent.Id.Hex(),
		Status:     parent.Status,
		StartTime:  startTime,
	}, nil
}

func isRecoveryChild(topology *taskBatchTopology, taskID string) bool {
	if topology == nil || taskID == "" {
		return false
	}
	_, ok := topology.indexByID[taskID]
	return ok
}

func recoveryExpectedChildIDs(topology *taskBatchTopology) []string {
	if topology == nil {
		return nil
	}
	return append([]string(nil), topology.childIDs...)
}

func resolveRecoveryParentTopology(ctx context.Context, svcCtx *svc.ServiceContext, parent *model.MainTask) (*resolvedRecoveryParent, error) {
	if parent == nil {
		return nil, fmt.Errorf("recovery parent is nil")
	}

	var definitions []model.ExecutorTask
	if parent.BatchCount <= 0 {
		var err error
		definitions, err = svcCtx.GetExecutorTaskModel().FindBatchDefinitions(ctx, parent.Id.Hex())
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("load recovery batch topology definitions: %w", err)
		}
	}
	topology, err := resolveTaskBatchTopology(parent, definitions)
	if err != nil {
		return nil, err
	}
	return &resolvedRecoveryParent{task: parent, topology: topology}, nil
}

// resolveRecoveryParents proves common-path grouping against a resolved
// positive topology before any parent is prepared. A cached parent ID that
// does not own the child aborts the whole collection because its true sibling
// group is unknown.
func resolveRecoveryParents(ctx context.Context, svcCtx *svc.ServiceContext, candidates []*recoveryCandidate) (map[string]*resolvedRecoveryParent, error) {
	candidateParents := make(map[string]struct{})
	for _, candidate := range candidates {
		if candidate == nil || candidate.record == nil || candidate.parentID == "" {
			return nil, fmt.Errorf("recovery candidate parent identity is incomplete; retry required")
		}
		candidateParents[candidate.parentID] = struct{}{}
	}
	parentIDs := make([]string, 0, len(candidateParents))
	for parentID := range candidateParents {
		parentIDs = append(parentIDs, parentID)
	}
	sort.Strings(parentIDs)

	parents := make(map[string]*resolvedRecoveryParent, len(parentIDs))
	for _, parentID := range parentIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parent, err := svcCtx.GetMainTaskModel().FindById(ctx, parentID)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("resolve recovery parent %s; retry required: %w", parentID, err)
		}
		if parent == nil {
			return nil, fmt.Errorf("resolve recovery parent %s; retry required: parent does not exist", parentID)
		}
		resolved, err := resolveRecoveryParentTopology(ctx, svcCtx, parent)
		if err != nil {
			return nil, fmt.Errorf("resolve recovery parent %s topology; retry required: %w", parentID, err)
		}
		parents[parentID] = resolved
	}

	for _, candidate := range candidates {
		record := candidate.record
		resolved := parents[candidate.parentID]
		if !record.TaskInfoExists || record.TaskInfoErr != nil || record.TaskInfo == nil {
			if record.TaskInfoErr != nil {
				return nil, fmt.Errorf("prove parent for task %s; retry required: %w", record.TaskID, record.TaskInfoErr)
			}
			return nil, fmt.Errorf("prove parent for task %s; retry required: exact payload is missing", record.TaskID)
		}
		if resolved == nil || resolved.task == nil || resolved.topology == nil ||
			record.TaskInfo.TaskId != record.TaskID ||
			record.TaskInfo.MainTaskId != resolved.task.Id.Hex() ||
			!isRecoveryChild(resolved.topology, record.TaskID) {
			return nil, fmt.Errorf(
				"prove parent for task %s; retry required: cached parent/child identity is invalid",
				record.TaskID,
			)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return parents, nil
}

// prepareRecoveryGroups resolves every parent and validates every candidate
// before commitRecoveryGroups is allowed to move any task.
func prepareRecoveryGroups(ctx context.Context, svcCtx *svc.ServiceContext, candidates []*recoveryCandidate, knownParents map[string]*resolvedRecoveryParent) ([]*recoveryGroup, error) {
	if knownParents == nil {
		var err error
		knownParents, err = resolveRecoveryParents(ctx, svcCtx, candidates)
		if err != nil {
			return nil, err
		}
	}

	groupsByParent := make(map[string]*recoveryGroup)
	for _, candidate := range candidates {
		if candidate == nil || candidate.parentID == "" {
			return nil, fmt.Errorf("recovery candidate has no parent group")
		}
		group := groupsByParent[candidate.parentID]
		if group == nil {
			group = &recoveryGroup{parentID: candidate.parentID}
			groupsByParent[candidate.parentID] = group
		}
		group.candidates = append(group.candidates, candidate)
	}

	parentIDs := make([]string, 0, len(groupsByParent))
	for parentID := range groupsByParent {
		parentIDs = append(parentIDs, parentID)
	}
	sort.Strings(parentIDs)

	groups := make([]*recoveryGroup, 0, len(parentIDs))
	for _, parentID := range parentIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		group := groupsByParent[parentID]
		groups = append(groups, group)

		group.resolved = knownParents[parentID]
		if group.resolved == nil || group.resolved.task == nil || group.resolved.topology == nil {
			group.err = errors.Join(group.err, fmt.Errorf("parent topology was not in the validated recovery set"))
			continue
		}

		candidatesByID := make(map[string]*recoveryCandidate, len(group.candidates))
		for _, candidate := range group.candidates {
			if candidate == nil || candidate.record == nil {
				group.err = errors.Join(group.err, fmt.Errorf("recovery candidate identity is incomplete"))
				continue
			}
			taskID := candidate.record.TaskID
			if !isRecoveryChild(group.resolved.topology, taskID) {
				group.err = errors.Join(group.err, fmt.Errorf("task %s is outside the resolved parent topology", taskID))
				continue
			}
			if _, duplicate := candidatesByID[taskID]; duplicate {
				group.err = errors.Join(group.err, fmt.Errorf("task %s appears more than once in the recovery group", taskID))
				continue
			}
			candidatesByID[taskID] = candidate
		}
		if group.err != nil {
			continue
		}

		orderedCandidates := make([]*recoveryCandidate, 0, len(group.candidates))
		for _, childID := range recoveryExpectedChildIDs(group.resolved.topology) {
			if candidate := candidatesByID[childID]; candidate != nil {
				orderedCandidates = append(orderedCandidates, candidate)
			}
		}
		if len(orderedCandidates) != len(group.candidates) {
			group.err = errors.Join(group.err, fmt.Errorf("recovery candidates do not match the resolved parent topology"))
			continue
		}
		group.candidates = orderedCandidates

		for _, candidate := range group.candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prepared, info, err := prepareRecoveryTask(ctx, svcCtx, group.resolved, candidate)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				group.err = errors.Join(group.err, fmt.Errorf("task %s: %w", candidate.record.TaskID, err))
				continue
			}
			group.tasks = append(group.tasks, prepared)
			group.infos = append(group.infos, info)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func pauseCommitMatches(candidate *recoveryCandidate, task *scheduler.TaskInfo, evidence *model.PauseCommitEvidence) bool {
	if candidate == nil || candidate.record == nil || candidate.record.Execution == nil || task == nil || evidence == nil {
		return false
	}
	execution := candidate.record.Execution
	return strings.EqualFold(strings.TrimSpace(execution.Phase), "pausing") &&
		evidence.DispatchGeneration == task.DispatchGeneration &&
		evidence.LeaseGeneration == scheduler.LeaseGenerationHash(task.LeaseToken) &&
		evidence.Worker == execution.WorkerName &&
		evidence.InstanceID == execution.InstanceID &&
		evidence.TaskProtocol == execution.TaskProtocol &&
		!evidence.CommitTime.IsZero()
}

func recoveryGroupHasPausing(group *recoveryGroup) bool {
	if group == nil {
		return false
	}
	for _, candidate := range group.candidates {
		if candidate != nil && candidate.record != nil && candidate.record.Execution != nil &&
			strings.EqualFold(strings.TrimSpace(candidate.record.Execution.Phase), "pausing") {
			return true
		}
	}
	return false
}

func completionMarkerSnapshot(task *model.MainTask) (*model.TaskCompletionReconciliation, error) {
	if task == nil || task.CompletionReconciliation == nil {
		return nil, nil
	}
	marker := *task.CompletionReconciliation
	marker.DispatchGeneration = strings.TrimSpace(marker.DispatchGeneration)
	if marker.DispatchGeneration == "" || marker.UpdatedTime.IsZero() {
		return nil, fmt.Errorf("durable completion reconciliation marker is malformed")
	}
	marker.UpdatedTime = marker.UpdatedTime.UTC().Truncate(time.Millisecond)
	if marker.ReconcileAttemptTime != nil {
		attempt := marker.ReconcileAttemptTime.UTC().Truncate(time.Millisecond)
		marker.ReconcileAttemptTime = &attempt
	}
	return &marker, nil
}

func completionMarkerGeneration(task *model.MainTask) (string, error) {
	marker, err := completionMarkerSnapshot(task)
	if err != nil || marker == nil {
		return "", err
	}
	return marker.DispatchGeneration, nil
}

func sameCompletionMarker(left, right *model.TaskCompletionReconciliation) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.DispatchGeneration != right.DispatchGeneration ||
		left.UpdatedTime.UTC().UnixMilli() != right.UpdatedTime.UTC().UnixMilli() {
		return false
	}
	if left.ReconcileAttemptTime == nil || right.ReconcileAttemptTime == nil {
		return left.ReconcileAttemptTime == nil && right.ReconcileAttemptTime == nil
	}
	return left.ReconcileAttemptTime.UTC().UnixMilli() == right.ReconcileAttemptTime.UTC().UnixMilli()
}

func releaseRecoveryGroupOperations(sched *scheduler.Scheduler, operations []*scheduler.LeaseOperation) error {
	if sched == nil || len(operations) == 0 {
		return nil
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var releaseErrs []error
	for _, operation := range operations {
		if err := sched.ReleaseLeaseOperation(releaseCtx, operation); err != nil {
			releaseErrs = append(releaseErrs, err)
		}
	}
	return errors.Join(releaseErrs...)
}

// beginRecoveryGroupOperations freezes every exact lease after targeted Mongo
// reconciliation. The final parent/evidence read is therefore ordered before
// the all-or-none Redis transition, including mixed complete/requeue plans.
func beginRecoveryGroupOperations(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	group *recoveryGroup,
) ([]*scheduler.LeaseOperation, error) {
	operations := make([]*scheduler.LeaseOperation, 0, len(group.tasks))
	fail := func(cause error) ([]*scheduler.LeaseOperation, error) {
		return nil, errors.Join(cause, releaseRecoveryGroupOperations(svcCtx.Scheduler, operations))
	}
	for index, task := range group.tasks {
		candidate := group.candidates[index]
		if candidate == nil || candidate.record == nil || candidate.record.Execution == nil ||
			candidate.record.TaskInfo == nil {
			return fail(scheduler.ErrTaskLeaseConflict)
		}
		execution := candidate.record.Execution
		var operation *scheduler.LeaseOperation
		var err error
		if strings.EqualFold(strings.TrimSpace(execution.Phase), "pausing") {
			operation, err = svcCtx.Scheduler.BeginPausedTask(ctx, task.TaskId, task.LeaseToken)
		} else {
			operation, err = svcCtx.Scheduler.BeginLeaseOperation(ctx, task.TaskId, task.LeaseToken)
		}
		if err != nil {
			return fail(fmt.Errorf("guard exact recovery lease %s: %w", task.TaskId, err))
		}
		operations = append(operations, operation)
		if operation == nil || operation.AlreadyClosed || operation.TaskID != task.TaskId ||
			operation.LeaseToken != task.LeaseToken || operation.WorkerName != execution.WorkerName ||
			operation.InstanceID != execution.InstanceID || operation.TaskProtocol != execution.TaskProtocol ||
			operation.DispatchGeneration != task.DispatchGeneration || operation.TaskInfoData == "" {
			return fail(fmt.Errorf("%w: guarded recovery owner changed for %s", scheduler.ErrTaskLeaseConflict, task.TaskId))
		}
		var payload scheduler.TaskInfo
		if err := json.Unmarshal([]byte(operation.TaskInfoData), &payload); err != nil ||
			payload.TaskId != candidate.record.TaskInfo.TaskId ||
			payload.MainTaskId != candidate.record.TaskInfo.MainTaskId ||
			payload.DispatchGeneration != candidate.record.TaskInfo.DispatchGeneration ||
			payload.Config != candidate.record.TaskInfo.Config {
			return fail(fmt.Errorf("%w: guarded recovery payload changed for %s", scheduler.ErrTaskLeaseConflict, task.TaskId))
		}
		if strings.EqualFold(strings.TrimSpace(execution.Phase), "pausing") {
			// BeginPausedTask canonicalizes the guarded execution phase in Redis.
			execution.Phase = "pausing"
		}
	}
	return operations, nil
}

func recoveryTaskCompletionEvidence(
	parent *model.MainTask,
	task *scheduler.TaskInfo,
) (completionLeaseEvidenceState, error) {
	if parent == nil || task == nil || parent.Id.Hex() != task.MainTaskId ||
		parent.DispatchGeneration == "" || task.DispatchGeneration != parent.DispatchGeneration ||
		!model.HasCompletionReconciliation(parent, task.DispatchGeneration) ||
		(!model.IsRunnableTaskStatus(parent.Status) && !model.IsSemanticTerminalTaskStatus(parent.Status)) {
		return completionLeaseEvidenceConflict, model.ErrTaskDispatchConflict
	}
	state, err := classifyCompletionLeaseEvidence(
		parent, task.TaskId, scheduler.LeaseGenerationHash(task.LeaseToken),
	)
	if errors.Is(err, errNoFinalCompletionEvidence) {
		return completionLeaseEvidenceAbsent, nil
	}
	return state, err
}

func commitRecoveryGroupTransition(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	group *recoveryGroup,
	currentParent *model.MainTask,
	markerAffected bool,
	reconciledGeneration string,
	reconciledMarker *model.TaskCompletionReconciliation,
	requireExactMarker bool,
	terminalNoFinalEvidence bool,
) (moved bool, resultErr error) {
	var operations []*scheduler.LeaseOperation
	if markerAffected {
		var err error
		operations, err = beginRecoveryGroupOperations(ctx, svcCtx, group)
		if err != nil {
			return false, err
		}
		defer func() {
			if releaseErr := releaseRecoveryGroupOperations(svcCtx.Scheduler, operations); releaseErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("release recovery operation guards: %w", releaseErr))
			}
		}()
		currentParent, err = svcCtx.GetMainTaskModel().FindById(ctx, group.parentID)
		if err != nil {
			return false, fmt.Errorf("refresh guarded recovery parent: %w", err)
		}
		currentMarker, markerErr := completionMarkerSnapshot(currentParent)
		if markerErr != nil {
			return false, markerErr
		}
		if requireExactMarker && !sameCompletionMarker(reconciledMarker, currentMarker) {
			return false, fmt.Errorf("completion reconciliation owner changed while recovery was guarded")
		}
		if currentMarker != nil && currentMarker.DispatchGeneration != reconciledGeneration {
			return false, fmt.Errorf("completion reconciliation generation changed while recovery was guarded")
		}
		if terminalNoFinalEvidence {
			if currentParent == nil || !model.IsSemanticTerminalTaskStatus(currentParent.Status) ||
				currentParent.DispatchGeneration != reconciledGeneration ||
				!sameCompletionMarker(reconciledMarker, currentMarker) {
				return false, fmt.Errorf("terminal no-evidence recovery authority changed while guarded")
			}
			for _, task := range group.tasks {
				if task == nil || task.DispatchGeneration != reconciledGeneration {
					return false, fmt.Errorf("terminal no-evidence candidate belongs to another generation")
				}
			}
		}
	}

	operationAt := func(index int) *scheduler.LeaseOperation {
		if len(operations) == 0 {
			return nil
		}
		return operations[index]
	}

	if !recoveryGroupHasPausing(group) {
		allRunnableCurrent := !markerAffected && currentParent != nil &&
			(currentParent.Status == model.TaskStatusPending || currentParent.Status == model.TaskStatusStarted ||
				currentParent.Status == model.TaskStatusPaused)
		if allRunnableCurrent {
			for _, task := range group.tasks {
				if task.DispatchGeneration != currentParent.DispatchGeneration {
					allRunnableCurrent = false
					break
				}
			}
		}
		if allRunnableCurrent {
			return svcCtx.Scheduler.RequeueExactTaskBatch(ctx, group.tasks)
		}

		resolutions := make([]scheduler.DeadTaskResolution, 0, len(group.tasks))
		for index, task := range group.tasks {
			execution := group.candidates[index].record.Execution
			action := scheduler.DeadTaskResolutionDiscard
			activeGeneration := currentParent != nil && task.DispatchGeneration == currentParent.DispatchGeneration
			if activeGeneration && model.HasCompletionReconciliation(currentParent, task.DispatchGeneration) &&
				(model.IsRunnableTaskStatus(currentParent.Status) || model.IsSemanticTerminalTaskStatus(currentParent.Status)) {
				evidence, evidenceErr := recoveryTaskCompletionEvidence(currentParent, task)
				if evidenceErr != nil {
					return false, fmt.Errorf("classify guarded completion evidence for %s: %w", task.TaskId, evidenceErr)
				}
				switch evidence {
				case completionLeaseEvidenceExact:
					action = scheduler.DeadTaskResolutionComplete
				case completionLeaseEvidenceAbsent:
				case completionLeaseEvidenceConflict:
					return false, fmt.Errorf("%w: conflicting completion evidence for %s", scheduler.ErrTaskLeaseConflict, task.TaskId)
				}
			}
			if action != scheduler.DeadTaskResolutionComplete && activeGeneration &&
				(currentParent.Status == model.TaskStatusPending || currentParent.Status == model.TaskStatusStarted ||
					currentParent.Status == model.TaskStatusPaused) {
				action = scheduler.DeadTaskResolutionRequeue
			}
			resolutionPhase := ""
			if action == scheduler.DeadTaskResolutionComplete {
				resolutionPhase = execution.Phase
			}
			resolutions = append(resolutions, scheduler.DeadTaskResolution{
				Task: task, ExpectedPhase: execution.Phase, Action: action,
				Worker: execution.WorkerName, Phase: resolutionPhase, Operation: operationAt(index),
			})
		}
		return svcCtx.Scheduler.ResolveDeadTaskBatch(ctx, resolutions)
	}

	pauseEvidence := make(map[string]*model.PauseCommitEvidence, len(group.tasks))
	pauseRequired := currentParent != nil && currentParent.Status == model.TaskStatusPaused
	var parentPhase, parentTaskState string
	for index, task := range group.tasks {
		candidate := group.candidates[index]
		if currentParent == nil || task.DispatchGeneration != currentParent.DispatchGeneration ||
			candidate.record.Execution == nil ||
			!strings.EqualFold(strings.TrimSpace(candidate.record.Execution.Phase), "pausing") {
			continue
		}
		evidence, err := svcCtx.GetExecutorTaskModel().FindPauseCommit(
			ctx, group.parentID, task.TaskId, task.DispatchGeneration,
		)
		if err != nil {
			return false, fmt.Errorf("pause evidence lookup failed: %w", err)
		}
		if pauseCommitMatches(candidate, task, evidence) {
			pauseEvidence[task.TaskId] = evidence
			pauseRequired = true
			if parentPhase == "" {
				parentPhase = evidence.Phase
			}
			if group.resolved != nil && group.resolved.topology != nil &&
				group.resolved.topology.count == 1 && task.TaskId == currentParent.TaskId {
				parentTaskState = evidence.TaskState
			}
		}
	}

	if currentParent != nil && pauseRequired && currentParent.DispatchGeneration != "" &&
		(currentParent.Status == model.TaskStatusPending || currentParent.Status == model.TaskStatusStarted ||
			currentParent.Status == model.TaskStatusPaused) {
		paused, err := svcCtx.GetMainTaskModel().EnsurePausedDispatch(
			ctx, group.parentID, currentParent.DispatchGeneration, parentPhase, parentTaskState,
		)
		if err != nil {
			return false, fmt.Errorf("durable pause repair failed: %w", err)
		}
		if !paused {
			return false, fmt.Errorf("durable pause repair lost its generation race")
		}
		currentParent, err = svcCtx.GetMainTaskModel().FindById(ctx, group.parentID)
		if err != nil {
			return false, fmt.Errorf("refresh durably paused parent: %w", err)
		}
		if currentParent == nil {
			return false, model.ErrTaskDispatchConflict
		}
	}

	resolutions := make([]scheduler.DeadTaskResolution, 0, len(group.tasks))
	for index, task := range group.tasks {
		candidate := group.candidates[index]
		execution := candidate.record.Execution
		action := scheduler.DeadTaskResolutionDiscard
		activeGeneration := currentParent != nil && task.DispatchGeneration == currentParent.DispatchGeneration
		if activeGeneration && model.HasCompletionReconciliation(currentParent, task.DispatchGeneration) &&
			(model.IsRunnableTaskStatus(currentParent.Status) || model.IsSemanticTerminalTaskStatus(currentParent.Status)) {
			evidence, evidenceErr := recoveryTaskCompletionEvidence(currentParent, task)
			if evidenceErr != nil {
				return false, fmt.Errorf("classify guarded pausing completion evidence for %s: %w", task.TaskId, evidenceErr)
			}
			switch evidence {
			case completionLeaseEvidenceExact:
				action = scheduler.DeadTaskResolutionComplete
			case completionLeaseEvidenceAbsent:
			case completionLeaseEvidenceConflict:
				return false, fmt.Errorf("%w: conflicting completion evidence for %s", scheduler.ErrTaskLeaseConflict, task.TaskId)
			}
		}
		if action != scheduler.DeadTaskResolutionComplete && activeGeneration {
			switch currentParent.Status {
			case model.TaskStatusPaused:
				action = scheduler.DeadTaskResolutionPause
			case model.TaskStatusPending, model.TaskStatusStarted:
				action = scheduler.DeadTaskResolutionRequeue
			}
		}
		resolutionPhase := ""
		if action == scheduler.DeadTaskResolutionComplete {
			resolutionPhase = execution.Phase
		} else if evidence := pauseEvidence[task.TaskId]; evidence != nil {
			resolutionPhase = evidence.Phase
		}
		resolutions = append(resolutions, scheduler.DeadTaskResolution{
			Task: task, ExpectedPhase: execution.Phase, Action: action,
			Worker: execution.WorkerName, Phase: resolutionPhase, Operation: operationAt(index),
		})
	}
	return svcCtx.Scheduler.ResolveDeadPausingTaskBatch(ctx, resolutions)
}

// commitRecoveryGroups is the safety boundary for every Mongo-backed recovery
// path. A durable completion marker is reconciled for this exact parent and
// generation before any recovery action is chosen; unrelated groups continue
// when one group must fail closed for retry.
func commitRecoveryGroups(ctx context.Context, svcCtx *svc.ServiceContext, groups []*recoveryGroup, component string) ([]RecoveredTaskInfo, error) {
	var recovered []RecoveredTaskInfo
	var recoveryErrs []error

	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return recovered, err
		}
		if group == nil {
			recoveryErrs = append(recoveryErrs, fmt.Errorf("%s recovery group is nil; retry required", component))
			continue
		}
		if group.err != nil {
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parent %s validation failed; retry required: %w",
				component, group.parentID, group.err,
			))
			continue
		}
		if len(group.tasks) == 0 || len(group.tasks) != len(group.candidates) || len(group.infos) != len(group.tasks) {
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parent %s recovery plan is incomplete; retry required",
				component, group.parentID,
			))
			continue
		}

		currentParent, err := svcCtx.GetMainTaskModel().FindById(ctx, group.parentID)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return recovered, ctxErr
			}
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parent %s refresh failed; retry required: %w", component, group.parentID, err,
			))
			continue
		}

		markerAffected := false
		requireExactMarker := false
		terminalNoFinalEvidence := false
		reconciledMarker, markerErr := completionMarkerSnapshot(currentParent)
		if markerErr != nil {
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parent %s completion marker is invalid; retry required: %w",
				component, group.parentID, markerErr,
			))
			continue
		}
		reconciledGeneration := ""
		if reconciledMarker != nil {
			reconciledGeneration = reconciledMarker.DispatchGeneration
			markerAffected = true
			outcome, reconcileErr := reconcileTaskCompletionExact(
				ctx, svcCtx, group.parentID, reconciledGeneration,
			)
			if reconcileErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return recovered, ctxErr
				}
				recoveryErrs = append(recoveryErrs, fmt.Errorf(
					"%s parent %s exact completion reconciliation failed; retry required: %w",
					component, group.parentID, reconcileErr,
				))
				continue
			}
			currentParent, err = svcCtx.GetMainTaskModel().FindById(ctx, group.parentID)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return recovered, ctxErr
				}
				recoveryErrs = append(recoveryErrs, fmt.Errorf(
					"%s parent %s refresh after exact completion reconciliation failed; retry required: %w",
					component, group.parentID, err,
				))
				continue
			}
			currentMarker, currentMarkerErr := completionMarkerSnapshot(currentParent)
			if currentMarkerErr != nil {
				recoveryErrs = append(recoveryErrs, fmt.Errorf(
					"%s parent %s completion marker became invalid; retry required: %w",
					component, group.parentID, currentMarkerErr,
				))
				continue
			}
			switch outcome {
			case completionReconciliationRunnablePending:
				if currentParent == nil || !model.IsRunnableTaskStatus(currentParent.Status) ||
					currentParent.DispatchGeneration != reconciledGeneration ||
					!sameCompletionMarker(reconciledMarker, currentMarker) {
					recoveryErrs = append(recoveryErrs, fmt.Errorf(
						"%s parent %s pending completion owner changed; retry required",
						component, group.parentID,
					))
					continue
				}
				requireExactMarker = true
			case completionReconciliationTerminalNoFinalEvidence:
				if currentParent == nil || !model.IsSemanticTerminalTaskStatus(currentParent.Status) ||
					currentParent.DispatchGeneration != reconciledGeneration ||
					!sameCompletionMarker(reconciledMarker, currentMarker) {
					recoveryErrs = append(recoveryErrs, fmt.Errorf(
						"%s parent %s terminal no-evidence owner changed; retry required",
						component, group.parentID,
					))
					continue
				}
				requireExactMarker = true
				terminalNoFinalEvidence = true
			case completionReconciliationTerminalPending:
				recoveryErrs = append(recoveryErrs, fmt.Errorf(
					"%s parent %s still has unresolved exact final leases; retry required",
					component, group.parentID,
				))
				continue
			case completionReconciliationQuiescent:
				if currentMarker != nil {
					recoveryErrs = append(recoveryErrs, fmt.Errorf(
						"%s parent %s completion owner changed after quiescence; retry required",
						component, group.parentID,
					))
					continue
				}
				recovered = append(recovered, group.infos...)
				logx.Infof("[%s] Reconciled %d exact completed task(s) for parent %s", component, len(group.infos), group.parentID)
				continue
			case completionReconciliationAbsent, completionReconciliationRetired:
				if currentMarker != nil {
					recoveryErrs = append(recoveryErrs, fmt.Errorf(
						"%s parent %s completion marker changed during reconciliation; retry required",
						component, group.parentID,
					))
					continue
				}
			}
		}

		moved, moveErr := commitRecoveryGroupTransition(
			ctx, svcCtx, group, currentParent, markerAffected, reconciledGeneration,
			reconciledMarker, requireExactMarker, terminalNoFinalEvidence,
		)
		if moveErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return recovered, ctxErr
			}
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parent %s atomic dead-owner transition failed; retry required: %w",
				component, group.parentID, moveErr,
			))
			continue
		}
		if !moved {
			recoveryErrs = append(recoveryErrs, fmt.Errorf(
				"%s parent %s atomic dead-owner transition did not commit; retry required",
				component, group.parentID,
			))
			continue
		}

		recovered = append(recovered, group.infos...)
		logx.Infof("[%s] Resolved %d exact task(s) for parent %s", component, len(group.infos), group.parentID)
	}
	if err := ctx.Err(); err != nil {
		return recovered, err
	}
	return recovered, errors.Join(recoveryErrs...)
}

// classifyRecoveryOwner reports whether a processing record is an orphan
// candidate. A healthy owner is always excluded. Missing or malformed owner
// metadata is returned as a candidate validation error, never synthesised.
func classifyRecoveryOwner(ctx context.Context, svcCtx *svc.ServiceContext, record *recoveryRecord, heartbeatCache map[string]bool) (bool, error, error) {
	if record == nil {
		return false, nil, nil
	}
	if !record.ExecutionExists || record.ExecutionErr != nil || record.Execution == nil {
		if !record.TaskInfoExists && !record.ExecutionExists {
			return false, nil, nil
		}
		reason := record.ExecutionErr
		if reason == nil {
			reason = fmt.Errorf("execution ownership is missing")
		}
		// Keep the unprovable sibling in its parent group so a valid v1 sibling
		// cannot move independently during a mixed-version rollout.
		return true, reason, nil
	}

	execution := record.Execution
	instanceID := strings.TrimSpace(execution.InstanceID)
	if execution.TaskProtocol == scheduler.TaskProtocolV1 && instanceID != "" {
		online, cached := heartbeatCache[instanceID]
		if !cached {
			exists, err := svcCtx.RedisClient.Exists(ctx, recoveryInstanceKeyPrefix+instanceID).Result()
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return false, nil, ctxErr
				}
				return false, nil, fmt.Errorf("check worker instance %s heartbeat: %w", instanceID, err)
			}
			online = exists > 0
			heartbeatCache[instanceID] = online
		}
		if online {
			return false, nil, nil
		}
	}
	requiresDispatchGeneration := record.TaskInfo == nil || isMongoRecoveryParentID(record.TaskInfo.MainTaskId)
	if execution.TaskProtocol != scheduler.TaskProtocolV1 || instanceID == "" ||
		strings.TrimSpace(execution.LeaseToken) == "" ||
		(requiresDispatchGeneration && strings.TrimSpace(execution.DispatchGeneration) == "") {
		return true, fmt.Errorf("execution ownership is legacy or incomplete"), nil
	}
	return true, nil, nil
}

// RecoverOrphanedByHeartbeat 通过 Redis 心跳快速检测离线 Worker 的任务并恢复
// 遍历 cscan:task:processing 集合，检查每个任务对应 Worker 的心跳 key 是否存在
// 如果心跳 key 不存在（Worker 已离线），立即恢复该任务
func RecoverOrphanedByHeartbeat(ctx context.Context, svcCtx *svc.ServiceContext) ([]RecoveredTaskInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := snapshotRecoveryRecords(ctx, svcCtx)
	if err != nil {
		logx.Errorf("[OrphanedTaskRecovery] Failed to snapshot processing tasks: %v", err)
		return nil, err
	}

	heartbeatCache := make(map[string]bool)
	candidates := make([]*recoveryCandidate, 0)
	parentlessCandidates := make([]*recoveryCandidate, 0)
	var collectionErrs []error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		orphaned, ownerErr, readErr := classifyRecoveryOwner(ctx, svcCtx, record, heartbeatCache)
		if readErr != nil {
			return nil, readErr
		}
		if !orphaned {
			continue
		}
		if !record.TaskInfoExists && record.TaskInfoErr == nil && !record.ExecutionExists {
			// A marker with neither payload nor execution belongs to the atomic
			// cleanup path; it cannot be safely reconstructed here.
			continue
		}

		expectedOwner := ""
		if record.Execution != nil {
			expectedOwner = record.Execution.WorkerName
		}
		candidate, err := newRecoveryCandidate(record, "", expectedOwner, ownerErr)
		if err != nil {
			collectionErrs = append(collectionErrs, err)
			continue
		}
		if record.TaskInfo != nil && !isMongoRecoveryParentID(record.TaskInfo.MainTaskId) {
			parentlessCandidates = append(parentlessCandidates, candidate)
			continue
		}
		candidates = append(candidates, candidate)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	recovered, parentlessErr := commitParentlessRecoveryCandidates(
		ctx, svcCtx, parentlessCandidates, "OrphanedTaskRecovery",
	)
	if len(collectionErrs) > 0 {
		return recovered, errors.Join(parentlessErr, fmt.Errorf(
			"orphan recovery candidate collection failed; retry required: %w", errors.Join(collectionErrs...),
		))
	}
	if len(candidates) == 0 {
		if err := ctx.Err(); err != nil {
			return recovered, err
		}
		return recovered, parentlessErr
	}

	groups, err := prepareRecoveryGroups(ctx, svcCtx, candidates, nil)
	if err != nil {
		return recovered, errors.Join(parentlessErr, err)
	}
	groupRecovered, groupErr := commitRecoveryGroups(ctx, svcCtx, groups, "OrphanedTaskRecovery")
	recovered = append(recovered, groupRecovered...)
	return recovered, errors.Join(parentlessErr, groupErr)
}

// RecoverOrphanedTasks 查找并恢复卡住的任务
func RecoverOrphanedTasks(ctx context.Context, svcCtx *svc.ServiceContext, timeout time.Duration) ([]RecoveredTaskInfo, error) {
	return recoverStaleMainTasks(ctx, svcCtx, timeout, "OrphanedTaskRecovery")
}

// RecoverWorkerTasks performs startup recovery for one logical worker name.
// It can recover only dead v1 executions from earlier process instances; the
// current instance and every instance with a live heartbeat are excluded.
func RecoverWorkerTasks(ctx context.Context, svcCtx *svc.ServiceContext, workerName, currentInstanceID string) ([]RecoveredTaskInfo, error) {
	return recoverWorkerExecutions(ctx, svcCtx, workerName, currentInstanceID, false, "WorkerStartupRecovery")
}

// RecoverWorkerInstanceTasks recovers one exact process generation after its
// instance heartbeat has been proven absent (for example, after graceful
// offline compare-and-delete).
func RecoverWorkerInstanceTasks(ctx context.Context, svcCtx *svc.ServiceContext, workerName, offlineInstanceID string) ([]RecoveredTaskInfo, error) {
	return recoverWorkerExecutions(ctx, svcCtx, workerName, offlineInstanceID, true, "WorkerOffline")
}

func workerRecoveryScopeError(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	record *recoveryRecord,
	workerName, instanceID string,
	exactInstance bool,
	heartbeatCache map[string]bool,
) (error, error) {
	if record == nil {
		return fmt.Errorf("recovery record is nil"), nil
	}
	if !record.ExecutionExists || record.ExecutionErr != nil || record.Execution == nil {
		if record.ExecutionErr != nil {
			return record.ExecutionErr, nil
		}
		return fmt.Errorf("execution ownership is missing"), nil
	}
	execution := record.Execution
	var validationErr error
	ownerInstance := strings.TrimSpace(execution.InstanceID)
	if execution.TaskID != record.TaskID {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution task identity does not match processing identity"))
	}
	if execution.WorkerName != workerName {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution belongs to worker %q, expected %q", execution.WorkerName, workerName))
	}
	requiresDispatchGeneration := record.TaskInfo == nil || isMongoRecoveryParentID(record.TaskInfo.MainTaskId)
	if execution.TaskProtocol != scheduler.TaskProtocolV1 || ownerInstance == "" ||
		strings.TrimSpace(execution.LeaseToken) == "" ||
		(requiresDispatchGeneration && strings.TrimSpace(execution.DispatchGeneration) == "") ||
		strings.TrimSpace(execution.Phase) == "" {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution ownership is legacy or incomplete"))
	}
	if exactInstance {
		if ownerInstance != instanceID {
			validationErr = errors.Join(validationErr, fmt.Errorf(
				"execution instance %q is outside exact recovery instance %q", ownerInstance, instanceID,
			))
		}
	} else if ownerInstance == instanceID {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution is owned by the current worker instance"))
	}
	if ownerInstance == "" {
		return validationErr, nil
	}

	online, cached := heartbeatCache[ownerInstance]
	if !cached {
		exists, err := svcCtx.RedisClient.Exists(ctx, recoveryInstanceKeyPrefix+ownerInstance).Result()
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("check worker instance %s heartbeat: %w", ownerInstance, err)
		}
		online = exists > 0
		heartbeatCache[ownerInstance] = online
	}
	if online {
		validationErr = errors.Join(validationErr, fmt.Errorf("execution owner instance %s is still online", ownerInstance))
	}
	return validationErr, nil
}

func recoverWorkerExecutions(
	ctx context.Context,
	svcCtx *svc.ServiceContext,
	workerName, instanceID string,
	exactInstance bool,
	component string,
) ([]RecoveredTaskInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workerName = strings.TrimSpace(workerName)
	instanceID = strings.TrimSpace(instanceID)
	if workerName == "" || instanceID == "" {
		return nil, fmt.Errorf("worker name and instance id are required for recovery")
	}

	currentHeartbeat, err := svcCtx.RedisClient.Exists(ctx, recoveryInstanceKeyPrefix+instanceID).Result()
	if err != nil {
		return nil, fmt.Errorf("check worker instance heartbeat before recovery: %w", err)
	}
	if exactInstance {
		if currentHeartbeat > 0 {
			return nil, nil
		}
	} else if currentHeartbeat == 0 {
		return nil, fmt.Errorf("current worker instance heartbeat is not established")
	}

	// Bounded completion reconciliation is a latency optimization. The shared
	// per-parent commit below repeats exact marker reconciliation as the safety
	// boundary before it can requeue, discard, pause, or complete a dead owner.
	if _, err := ReconcileTaskCompletions(ctx, svcCtx); err != nil {
		logx.Errorf("[%s] completion reconciliation preflight had retryable errors: %v", component, err)
	}

	records, err := snapshotRecoveryRecords(ctx, svcCtx)
	if err != nil {
		logx.Errorf("[%s] Failed to snapshot processing tasks: %v", component, err)
		return nil, err
	}

	// Fully valid dead records seed the affected parents. Parent topology then
	// expands each seed to every processing sibling, including malformed or
	// mixed-version records, so one child can never move independently.
	heartbeatCache := map[string]bool{instanceID: currentHeartbeat > 0}
	seeds := make([]*recoveryCandidate, 0)
	parentlessSeeds := make([]*recoveryCandidate, 0)
	var collectionErrs []error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		scopeErr, readErr := workerRecoveryScopeError(
			ctx, svcCtx, record, workerName, instanceID, exactInstance, heartbeatCache,
		)
		if readErr != nil {
			return nil, readErr
		}
		if scopeErr != nil {
			continue
		}
		candidate, candidateErr := newRecoveryCandidate(record, "", workerName, nil)
		if candidateErr != nil {
			collectionErrs = append(collectionErrs, candidateErr)
			continue
		}
		if record.TaskInfo != nil && !isMongoRecoveryParentID(record.TaskInfo.MainTaskId) {
			parentlessSeeds = append(parentlessSeeds, candidate)
			continue
		}
		seeds = append(seeds, candidate)
	}
	parentlessRecovered, parentlessErr := commitParentlessRecoveryCandidates(ctx, svcCtx, parentlessSeeds, component)
	if len(collectionErrs) > 0 {
		return parentlessRecovered, errors.Join(parentlessErr, fmt.Errorf(
			"worker %s recovery candidate collection failed; retry required: %w", workerName, errors.Join(collectionErrs...),
		))
	}
	if len(seeds) == 0 {
		if err := ctx.Err(); err != nil {
			return parentlessRecovered, err
		}
		return parentlessRecovered, parentlessErr
	}

	knownParents, err := resolveRecoveryParents(ctx, svcCtx, seeds)
	if err != nil {
		return parentlessRecovered, errors.Join(parentlessErr, err)
	}
	expectedParentsByChild := make(map[string]string)
	for parentID, resolved := range knownParents {
		for _, childID := range recoveryExpectedChildIDs(resolved.topology) {
			if existing := expectedParentsByChild[childID]; existing != "" && existing != parentID {
				return parentlessRecovered, errors.Join(parentlessErr, fmt.Errorf(
					"worker %s recovery topology is ambiguous for child %s", workerName, childID,
				))
			}
			expectedParentsByChild[childID] = parentID
		}
	}

	candidates := make([]*recoveryCandidate, 0, len(seeds))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return parentlessRecovered, err
		}
		parentID := expectedParentsByChild[record.TaskID]
		if parentID == "" && record.TaskInfo != nil {
			if _, seeded := knownParents[record.TaskInfo.MainTaskId]; seeded {
				parentID = record.TaskInfo.MainTaskId
			}
		}
		if parentID == "" {
			continue
		}

		scopeErr, readErr := workerRecoveryScopeError(
			ctx, svcCtx, record, workerName, instanceID, exactInstance, heartbeatCache,
		)
		if readErr != nil {
			return parentlessRecovered, errors.Join(parentlessErr, readErr)
		}
		candidate, candidateErr := newRecoveryCandidate(record, parentID, workerName, scopeErr)
		if candidateErr != nil {
			collectionErrs = append(collectionErrs, candidateErr)
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(collectionErrs) > 0 {
		return parentlessRecovered, errors.Join(parentlessErr, fmt.Errorf(
			"worker %s sibling expansion failed; retry required: %w", workerName, errors.Join(collectionErrs...),
		))
	}
	if len(candidates) == 0 {
		return parentlessRecovered, errors.Join(parentlessErr, fmt.Errorf(
			"worker %s recovery lost every seeded parent candidate; retry required", workerName,
		))
	}

	groups, err := prepareRecoveryGroups(ctx, svcCtx, candidates, knownParents)
	if err != nil {
		return parentlessRecovered, errors.Join(parentlessErr, err)
	}
	groupRecovered, groupErr := commitRecoveryGroups(ctx, svcCtx, groups, component)
	parentlessRecovered = append(parentlessRecovered, groupRecovered...)
	return parentlessRecovered, errors.Join(parentlessErr, groupErr)
}

// CleanupStaleProcessingTasks removes only records that have neither an exact
// task payload nor execution ownership. Each item is proven and removed by one
// scheduler-side atomic operation, avoiding an EXISTS/SREM race.
func CleanupStaleProcessingTasks(ctx context.Context, svcCtx *svc.ServiceContext, workerName string) {
	taskIDs, err := svcCtx.RedisClient.SMembers(ctx, recoveryProcessingKey).Result()
	if err != nil {
		return
	}

	cleaned := 0
	for _, taskID := range taskIDs {
		if ctx.Err() != nil {
			return
		}
		removed, err := svcCtx.Scheduler.CleanupUnownedProcessingTask(ctx, taskID, workerName)
		if err != nil {
			logx.Errorf("[OrphanedTaskRecovery] Failed to clean processing task %s: %v", taskID, err)
			continue
		}
		if removed {
			cleaned++
		}
	}
	if cleaned > 0 {
		logx.Infof("[OrphanedTaskRecovery] Cleaned up %d unrecoverable processing records", cleaned)
	}
}

// RecoverStaleMongoTasks 从 MongoDB 直接查找卡住的 STARTED 任务并恢复
// 作为 Redis 检测的兜底机制，处理 Worker 异常退出（OOM/SIGKILL）导致 Redis 状态不一致的情况
func RecoverStaleMongoTasks(ctx context.Context, svcCtx *svc.ServiceContext, timeout time.Duration) ([]RecoveredTaskInfo, error) {
	return recoverStaleMainTasks(ctx, svcCtx, timeout, "StaleTaskRecovery")
}
