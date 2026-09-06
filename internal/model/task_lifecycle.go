package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	DispatchIntentInitial = "initial"
	DispatchIntentResume  = "resume"

	TaskControlActionPause = "PAUSE"
	TaskControlActionStop  = "STOP"
)

var (
	ErrTaskDispatchConflict    = errors.New("task dispatch generation is no longer active")
	ErrTaskParentFenced        = errors.New("task dispatch is fenced by a same-generation parent control")
	ErrTaskFinalizationPending = errors.New("task semantic finalization is pending")
)

func bsonMillisecondTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Millisecond)
}

func bsonTimePointer(value time.Time) *time.Time {
	stable := bsonMillisecondTime(value)
	return &stable
}

func nextReconcileAttemptTime(previous *time.Time) time.Time {
	next := bsonMillisecondTime(time.Now())
	if previous == nil {
		return next
	}
	prior := bsonMillisecondTime(*previous)
	if !next.After(prior) {
		return prior.Add(time.Millisecond)
	}
	return next
}

func addReconcileAttemptFilter(filter bson.M, path string, previous *time.Time) {
	if previous == nil {
		filter["$or"] = bson.A{
			bson.M{path: bson.M{"$exists": false}},
			bson.M{path: nil},
		}
		return
	}
	filter[path] = bsonMillisecondTime(*previous)
}

func dispatchReconciliationOwnerTime(metadata bson.M) time.Time {
	if value, ok := metadata["dispatch_create_time"]; ok {
		switch typed := value.(type) {
		case time.Time:
			if !typed.IsZero() {
				return bsonMillisecondTime(typed)
			}
		case *time.Time:
			if typed != nil && !typed.IsZero() {
				return bsonMillisecondTime(*typed)
			}
		}
	}
	return bsonMillisecondTime(time.Now())
}

// completionReconciliationRefreshExpression advances the owner update version
// while preserving the fairness cursor for the same dispatch generation.
func completionReconciliationRefreshExpression(generation string, now time.Time) bson.M {
	now = bsonMillisecondTime(now)
	epoch := time.Unix(0, 0).UTC()
	currentGeneration := bson.M{"$ifNull": bson.A{"$completion_reconciliation.dispatch_generation", ""}}
	currentUpdatedTime := bson.M{"$ifNull": bson.A{"$completion_reconciliation.updated_time", epoch}}
	nextUpdatedTime := bson.M{"$cond": bson.A{
		bson.M{"$gte": bson.A{currentUpdatedTime, now}},
		bson.M{"$add": bson.A{currentUpdatedTime, 1}},
		now,
	}}
	return bson.M{"$cond": bson.A{
		bson.M{"$eq": bson.A{currentGeneration, generation}},
		bson.M{"$mergeObjects": bson.A{
			"$completion_reconciliation",
			bson.M{"dispatch_generation": generation, "updated_time": nextUpdatedTime},
		}},
		bson.M{
			"dispatch_generation":    generation,
			"updated_time":           now,
			"reconcile_attempt_time": now,
		},
	}}
}

// HasCompletionReconciliation reports whether the durable retry owner still
// names the captured dispatch generation.
func HasCompletionReconciliation(task *MainTask, generation string) bool {
	return task != nil && task.CompletionReconciliation != nil &&
		strings.TrimSpace(generation) != "" &&
		task.CompletionReconciliation.DispatchGeneration == generation
}

// HasExactControlIntent reports whether the durable parent state and embedded
// intent agree on one exact generation and control action.
func HasExactControlIntent(task *MainTask, generation, action string) bool {
	if task == nil || strings.TrimSpace(generation) == "" || task.DispatchGeneration != generation ||
		task.ControlIntent == nil || !task.ControlIntent.IsValid() ||
		task.ControlIntent.DispatchGeneration != generation || task.ControlIntent.Action != action {
		return false
	}
	switch action {
	case TaskControlActionPause:
		return task.Status == TaskStatusPaused
	case TaskControlActionStop:
		return task.Status == TaskStatusStopped
	default:
		return false
	}
}

// DispatchStateError distinguishes a durable same-generation PAUSE/STOP fence
// from a missing, superseded, malformed, or otherwise inactive dispatch.
func DispatchStateError(task *MainTask, generation string) error {
	if HasExactControlIntent(task, generation, TaskControlActionPause) ||
		HasExactControlIntent(task, generation, TaskControlActionStop) {
		return ErrTaskParentFenced
	}
	return ErrTaskDispatchConflict
}

func (intent *TaskControlIntent) IsValid() bool {
	if intent == nil || strings.TrimSpace(intent.IntentID) == "" ||
		strings.TrimSpace(intent.DispatchGeneration) == "" || intent.CreatedTime.IsZero() {
		return false
	}
	return intent.Action == TaskControlActionPause || intent.Action == TaskControlActionStop
}

func exactControlIntentFilter(intent *TaskControlIntent) bson.M {
	return bson.M{
		"control_intent.intent_id":           intent.IntentID,
		"control_intent.action":              intent.Action,
		"control_intent.dispatch_generation": intent.DispatchGeneration,
		"control_intent.created_time":        intent.CreatedTime.UTC().Truncate(time.Millisecond),
	}
}

// CommitControlIntent is the durable control linearization point. The parent
// status and the exact generation-scoped intent are changed by one Mongo CAS.
// A same-generation STOP may therefore replace a PAUSE, while a stale request
// cannot affect a newer dispatch generation.
func (m *MainTaskModel) CommitControlIntent(
	ctx context.Context,
	id, generation string,
	allowedStatuses []string,
	nextStatus string,
	intent TaskControlIntent,
	fields bson.M,
) (*MainTask, error) {
	intent.IntentID = strings.TrimSpace(intent.IntentID)
	intent.DispatchGeneration = strings.TrimSpace(intent.DispatchGeneration)
	intent.CreatedTime = bsonMillisecondTime(intent.CreatedTime)
	intent.ReconcileAttemptTime = bsonTimePointer(intent.CreatedTime)
	if len(allowedStatuses) == 0 || !intent.IsValid() || generation == "" ||
		intent.DispatchGeneration != generation {
		return nil, fmt.Errorf("control intent requires an exact dispatch generation")
	}
	if (intent.Action == TaskControlActionPause && nextStatus != TaskStatusPaused) ||
		(intent.Action == TaskControlActionStop && nextStatus != TaskStatusStopped) {
		return nil, fmt.Errorf("control intent action and status do not match")
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	set := bson.M{
		"status":         nextStatus,
		"control_intent": intent,
		"update_time":    time.Now(),
	}
	for key, value := range fields {
		set[key] = value
	}
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, bson.M{
		"_id":                 oid,
		"status":              bson.M{"$in": allowedStatuses},
		"dispatch_generation": generation,
	}, bson.M{"$set": set}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTaskDispatchConflict
	}
	if err != nil {
		return nil, err
	}
	m.syncScanStatusIfStatusChange(ctx, id, set)
	return &task, nil
}

// NextDispatchCreateTime returns a BSON-stable millisecond timestamp that is
// strictly newer than the previous dispatch. The strict order is the executor
// activation stale-writer fence across pause/resume generations.
func NextDispatchCreateTime(previous *time.Time) time.Time {
	next := time.Now().UTC().Truncate(time.Millisecond)
	if previous == nil || previous.IsZero() {
		return next
	}
	prior := previous.UTC().Truncate(time.Millisecond)
	if !next.After(prior) {
		return prior.Add(time.Millisecond)
	}
	return next
}

// FindBatchDefinitionsForDispatch returns only definitions belonging to the
// active durable generation. It is used by publication reconciliation.
func (m *ExecutorTaskModel) FindBatchDefinitionsForDispatch(ctx context.Context, mainTaskID, generation string) ([]ExecutorTask, error) {
	cursor, err := m.coll.Find(ctx, bson.M{
		"main_task_id":        mainTaskID,
		"dispatch_generation": generation,
		"config":              bson.M{"$exists": true, "$ne": ""},
	}, options.Find().SetSort(bson.D{{Key: "task_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var definitions []ExecutorTask
	if err := cursor.All(ctx, &definitions); err != nil {
		return nil, err
	}
	return definitions, nil
}

// ClaimDispatch is the durable publication linearization point. The parent is
// PENDING with its active generation before any Redis queue member is exposed.
// Repeating the same generation and intent while still PENDING is idempotent.
func (m *MainTaskModel) ClaimDispatch(
	ctx context.Context,
	id, sourceStatus, generation, intent string,
	metadata bson.M,
) (*MainTask, error) {
	if strings.TrimSpace(generation) == "" || (intent != DispatchIntentInitial && intent != DispatchIntentResume) {
		return nil, fmt.Errorf("dispatch claim requires generation and known intent")
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	ownerTime := dispatchReconciliationOwnerTime(metadata)
	set := bson.M{
		"status":              TaskStatusPending,
		"dispatch_generation": generation,
		"dispatch_intent":     intent,
		"update_time":         time.Now(),
	}
	for key, value := range metadata {
		set[key] = value
	}
	sameGeneration := bson.M{"$eq": bson.A{
		bson.M{"$ifNull": bson.A{"$dispatch_generation", ""}}, generation,
	}}
	// An exact idempotent claim preserves its scheduling cursor. A replacement
	// generation starts at the durable dispatch owner creation time.
	set["dispatch_reconcile_attempt_time"] = bson.M{"$cond": bson.A{
		sameGeneration, "$dispatch_reconcile_attempt_time", ownerTime,
	}}
	// A retry of the selected generation must preserve completion ownership,
	// while a genuinely new generation atomically retires the old marker.
	set["completion_reconciliation"] = bson.M{"$cond": bson.A{
		sameGeneration,
		"$completion_reconciliation",
		"$$REMOVE",
	}}
	filter := bson.M{
		"_id": oid,
		"$or": bson.A{
			bson.M{"status": sourceStatus},
			bson.M{
				"status":              TaskStatusPending,
				"dispatch_generation": generation,
				"dispatch_intent":     intent,
			},
		},
	}
	pipeline := mongo.Pipeline{{{Key: "$set", Value: set}}}
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, filter, pipeline,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTaskDispatchConflict
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ClaimResumeDispatch atomically replaces one PAUSED generation with a new
// PENDING generation and retires only the exact PAUSE intent observed by the
// caller. An already-reconciled (absent) intent is accepted, but any different
// intent, including a same-generation STOP, fails closed.
func (m *MainTaskModel) ClaimResumeDispatch(
	ctx context.Context,
	id, previousGeneration, generation string,
	pauseIntent *TaskControlIntent,
	metadata bson.M,
) (*MainTask, error) {
	previousGeneration = strings.TrimSpace(previousGeneration)
	generation = strings.TrimSpace(generation)
	if previousGeneration == "" || generation == "" || previousGeneration == generation {
		return nil, fmt.Errorf("resume claim requires distinct old and new generations")
	}
	if pauseIntent != nil {
		pauseIntent.IntentID = strings.TrimSpace(pauseIntent.IntentID)
		pauseIntent.DispatchGeneration = strings.TrimSpace(pauseIntent.DispatchGeneration)
		pauseIntent.CreatedTime = pauseIntent.CreatedTime.UTC().Truncate(time.Millisecond)
		if !pauseIntent.IsValid() || pauseIntent.Action != TaskControlActionPause ||
			pauseIntent.DispatchGeneration != previousGeneration {
			return nil, fmt.Errorf("resume claim requires the exact old-generation PAUSE intent")
		}
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	ownerTime := dispatchReconciliationOwnerTime(metadata)
	set := bson.M{
		"status":                          TaskStatusPending,
		"dispatch_generation":             generation,
		"dispatch_intent":                 DispatchIntentResume,
		"dispatch_reconcile_attempt_time": ownerTime,
		"update_time":                     time.Now(),
	}
	for key, value := range metadata {
		set[key] = value
	}
	absentIntent := bson.A{
		bson.M{"control_intent": bson.M{"$exists": false}},
		bson.M{"control_intent": nil},
	}
	intentFilter := absentIntent
	if pauseIntent != nil {
		intentFilter = append(intentFilter, exactControlIntentFilter(pauseIntent))
	}
	filter := bson.M{
		"_id":                 oid,
		"status":              TaskStatusPaused,
		"dispatch_generation": previousGeneration,
		"$or":                 intentFilter,
	}
	update := bson.M{
		"$set": set,
		"$unset": bson.M{
			"control_intent":            "",
			"completion_reconciliation": "",
		},
	}
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTaskDispatchConflict
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ClaimTaskStarted atomically accepts an acquired child only when its payload
// names the current PENDING/STARTED dispatch. CREATED, PAUSED, terminal, and
// superseded generations all fail closed.
func (m *MainTaskModel) ClaimTaskStarted(ctx context.Context, id, generation string) (*MainTask, error) {
	if strings.TrimSpace(generation) == "" {
		return nil, ErrTaskDispatchConflict
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	pipeline := mongo.Pipeline{{{Key: "$set", Value: bson.M{
		"status": TaskStatusStarted,
		"start_time": bson.M{"$cond": bson.A{
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$start_time", nil}}, nil}},
			now,
			"$start_time",
		}},
		"update_time": now,
	}}}}
	filter := bson.M{
		"_id":                 oid,
		"dispatch_generation": generation,
		"status":              bson.M{"$in": bson.A{TaskStatusPending, TaskStatusStarted}},
	}
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, filter, pipeline,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTaskDispatchConflict
	}
	if err != nil {
		return nil, err
	}
	m.syncScanStatusToTargets(ctx, task.Target, "in_progress")
	return &task, nil
}

// UpdateDispatchProgress is monotonic and can mutate only the active runnable
// generation. A terminal or superseded parent cannot be resurrected.
func (m *MainTaskModel) UpdateDispatchProgress(ctx context.Context, id, generation, phase string, progress int) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(generation) == "" {
		return false, ErrTaskDispatchConflict
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	set := bson.M{"update_time": time.Now()}
	if phase != "" {
		set["current_phase"] = phase
	}
	result, err := m.coll.UpdateOne(ctx, bson.M{
		"_id":                 oid,
		"dispatch_generation": generation,
		"status":              bson.M{"$in": bson.A{TaskStatusPending, TaskStatusStarted}},
	}, bson.M{
		"$max": bson.M{"progress": progress},
		"$set": set,
	})
	if err != nil {
		return false, err
	}
	return result.MatchedCount == 1, nil
}

// UpdateActiveDispatchFields applies nonterminal worker metadata only to the
// current runnable generation.
func (m *MainTaskModel) UpdateActiveDispatchFields(ctx context.Context, id, generation string, fields bson.M) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(generation) == "" {
		return false, ErrTaskDispatchConflict
	}
	set := bson.M{"update_time": time.Now()}
	for key, value := range fields {
		set[key] = value
	}
	result, err := m.coll.UpdateOne(ctx, bson.M{
		"_id":                 oid,
		"dispatch_generation": generation,
		"status":              bson.M{"$in": bson.A{TaskStatusPending, TaskStatusStarted}},
	}, bson.M{"$set": set})
	if err != nil {
		return false, err
	}
	return result.MatchedCount == 1, nil
}

// TransitionDispatchStatus compare-and-sets a control transition against both
// the observed status and dispatch generation.
func (m *MainTaskModel) TransitionDispatchStatus(
	ctx context.Context,
	id, generation string,
	allowedStatuses []string,
	next string,
	fields bson.M,
) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	if len(allowedStatuses) == 0 {
		return false, fmt.Errorf("dispatch transition requires allowed source statuses")
	}
	set := bson.M{"status": next, "update_time": time.Now()}
	for key, value := range fields {
		set[key] = value
	}
	filter := bson.M{
		"_id":    oid,
		"status": bson.M{"$in": allowedStatuses},
	}
	if generation == "" {
		filter["dispatch_generation"] = bson.M{"$in": bson.A{"", nil}}
	} else {
		filter["dispatch_generation"] = generation
	}
	result, err := m.coll.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return false, err
	}
	if result.ModifiedCount > 0 {
		m.syncScanStatusIfStatusChange(ctx, id, set)
	}
	return result.MatchedCount == 1, nil
}

// RecordPhaseSummaryForDispatch stores a report only for the active runnable
// dispatch. Report keys are immutable first-write-wins identities: an exact
// duplicate may refresh reconciliation ownership, but never its payload.
func (m *MainTaskModel) RecordPhaseSummaryForDispatch(
	ctx context.Context,
	id, generation, reportKey string,
	phase TaskPhaseSummary,
	incrAmount int,
) (*MainTask, bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, false, err
	}
	generation = strings.TrimSpace(generation)
	if generation == "" {
		return nil, false, ErrTaskDispatchConflict
	}
	if incrAmount <= 0 {
		incrAmount = 1
	}
	phasePath := "scan_summary.phases." + reportKey
	now := bsonMillisecondTime(time.Now())
	marker := completionReconciliationRefreshExpression(generation, now)
	activeFilter := bson.M{
		"_id":                 oid,
		"dispatch_generation": generation,
		"status":              bson.M{"$in": bson.A{TaskStatusPending, TaskStatusStarted}},
	}
	firstFilter := bson.M{}
	for key, value := range activeFilter {
		firstFilter[key] = value
	}
	firstFilter[phasePath] = bson.M{"$exists": false}
	pipeline := mongo.Pipeline{{{Key: "$set", Value: bson.M{
		phasePath:                  phase,
		"scan_summary.phase_count": bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$scan_summary.phase_count", 0}}, incrAmount}},
		"sub_task_done": bson.M{"$min": bson.A{
			bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$sub_task_done", 0}}, incrAmount}},
			"$sub_task_count",
		}},
		"completion_reconciliation": marker,
		"update_time":               now,
	}}}}
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, firstFilter, pipeline,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if err == nil {
		return &task, true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}

	current, err := m.FindById(ctx, id)
	if err != nil || current == nil {
		return current, false, err
	}
	if current.DispatchGeneration != generation {
		return nil, false, ErrTaskDispatchConflict
	}
	persisted, duplicate := taskPhaseMap(current.ScanSummary)[reportKey]
	if !duplicate {
		return nil, false, DispatchStateError(current, generation)
	}
	if phase.SubTaskId == "" || phase.LeaseGeneration == "" ||
		persisted.SubTaskId != phase.SubTaskId || persisted.LeaseGeneration != phase.LeaseGeneration {
		return nil, false, ErrTaskDispatchConflict
	}
	if !IsRunnableTaskStatus(current.Status) && !IsSemanticTerminalTaskStatus(current.Status) {
		return nil, false, DispatchStateError(current, generation)
	}

	// Qualify the refresh with the stored exact child and non-bearer lease
	// identity. A conflicting duplicate is never allowed to replace evidence.
	duplicateFilter := bson.M{
		"_id":                 oid,
		"dispatch_generation": generation,
		"status": bson.M{"$in": bson.A{
			TaskStatusPending, TaskStatusStarted, TaskStatusSuccess, TaskStatusPartial,
			TaskStatusFailure, TaskStatusLegacyCompleted,
		}},
		phasePath + ".sub_task_id":      phase.SubTaskId,
		phasePath + ".lease_generation": phase.LeaseGeneration,
	}
	duplicatePipeline := mongo.Pipeline{{{Key: "$set", Value: bson.M{
		"completion_reconciliation": marker,
		"update_time":               now,
	}}}}
	err = m.coll.FindOneAndUpdate(ctx, duplicateFilter, duplicatePipeline,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if err == nil {
		return &task, false, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}
	fresh, findErr := m.FindById(ctx, id)
	if findErr != nil {
		return nil, false, findErr
	}
	return nil, false, DispatchStateError(fresh, generation)
}

// FinalizeFromScanSummaryForDispatch atomically claims the terminal state for
// the active generation only. ErrTaskFinalizationPending distinguishes a
// runnable generation that still needs reconciliation from an already-terminal
// same-generation result (updated=false, err=nil).
func (m *MainTaskModel) FinalizeFromScanSummaryForDispatch(ctx context.Context, id, generation string) (*TaskScanSummary, bool, error) {
	generation = strings.TrimSpace(generation)
	if generation == "" {
		return nil, false, ErrTaskDispatchConflict
	}
	task, err := m.FindById(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if task == nil || task.DispatchGeneration != generation {
		return nil, false, ErrTaskDispatchConflict
	}
	if IsSemanticTerminalTaskStatus(task.Status) {
		if task.ScanSummary == nil {
			return nil, false, nil
		}
		summary := *task.ScanSummary
		return &summary, false, nil
	}
	if !IsRunnableTaskStatus(task.Status) {
		return nil, false, DispatchStateError(task, generation)
	}
	phases := taskPhaseMap(task.ScanSummary)
	summary := AggregateTaskScanSummary(task.Status, task.SubTaskCount, phases)
	if !HasCompletionReconciliation(task, generation) ||
		task.SubTaskDone < task.SubTaskCount || summary.PhaseCount < task.SubTaskCount {
		return &summary, false, ErrTaskFinalizationPending
	}
	resultPrefix := task.Result
	for _, reportedPhase := range phases {
		if strings.HasPrefix(reportedPhase.ResultPrefix, "Assets:") {
			resultPrefix = reportedPhase.ResultPrefix
			break
		}
	}
	resultText := AppendCoverageHint(resultPrefix, summary)
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	result, err := m.coll.UpdateOne(ctx, bson.M{
		"_id":                 oid,
		"dispatch_generation": generation,
		"completion_reconciliation.dispatch_generation": generation,
		"status": bson.M{"$in": bson.A{TaskStatusPending, TaskStatusStarted}},
		"$expr": bson.M{"$and": bson.A{
			bson.M{"$gte": bson.A{"$sub_task_done", "$sub_task_count"}},
			bson.M{"$gte": bson.A{"$scan_summary.phase_count", "$sub_task_count"}},
		}},
	}, bson.M{"$set": bson.M{
		"status": summary.Outcome, "progress": 100, "result": resultText,
		"scan_summary": summary, "end_time": now, "update_time": now,
	}})
	if err != nil {
		return nil, false, err
	}
	if result.ModifiedCount > 0 {
		scanStatus := "completed"
		if summary.Outcome == TaskStatusFailure {
			scanStatus = "failed"
		}
		m.syncScanStatusToTargets(ctx, task.Target, scanStatus)
		return &summary, true, nil
	}

	// A sibling may have won the semantic CAS, or a same-generation parent
	// control may have fenced it. Re-read to make updated=false unambiguous.
	current, findErr := m.FindById(ctx, id)
	if findErr != nil {
		return nil, false, findErr
	}
	if current == nil || current.DispatchGeneration != generation {
		return nil, false, ErrTaskDispatchConflict
	}
	if IsSemanticTerminalTaskStatus(current.Status) {
		if current.ScanSummary == nil {
			return nil, false, nil
		}
		persisted := *current.ScanSummary
		return &persisted, false, nil
	}
	if !IsRunnableTaskStatus(current.Status) {
		return nil, false, DispatchStateError(current, generation)
	}
	return &summary, false, ErrTaskFinalizationPending
}

// PauseCommitEvidence is durable, non-bearer proof that a snapshot belongs to
// one exact lease and dispatch generation.
type PauseCommitEvidence struct {
	TaskState          string
	LeaseGeneration    string
	Worker             string
	InstanceID         string
	TaskProtocol       int
	Phase              string
	CommitTime         time.Time
	DispatchGeneration string
}

// CommitPauseSnapshot atomically stores the snapshot and its exact opaque lease
// evidence on the existing executor definition.
func (m *ExecutorTaskModel) CommitPauseSnapshot(
	ctx context.Context,
	mainTaskID, taskID string,
	evidence PauseCommitEvidence,
) (bool, error) {
	if mainTaskID == "" || taskID == "" || evidence.DispatchGeneration == "" || evidence.LeaseGeneration == "" ||
		strings.TrimSpace(evidence.Worker) == "" || evidence.InstanceID == "" || evidence.TaskProtocol <= 0 ||
		!isValidExecutorTaskState(evidence.TaskState) {
		return false, fmt.Errorf("pause snapshot evidence is incomplete")
	}
	commitTime := evidence.CommitTime
	if commitTime.IsZero() {
		commitTime = time.Now()
	}
	result, err := m.coll.UpdateOne(ctx, bson.M{
		"main_task_id":        mainTaskID,
		"task_id":             taskID,
		"dispatch_generation": evidence.DispatchGeneration,
	}, bson.M{"$set": bson.M{
		"task_state":                evidence.TaskState,
		"status":                    TaskStatusPaused,
		"pause_lease_generation":    evidence.LeaseGeneration,
		"pause_worker":              evidence.Worker,
		"pause_instance_id":         evidence.InstanceID,
		"pause_task_protocol":       evidence.TaskProtocol,
		"pause_phase":               evidence.Phase,
		"pause_commit_time":         commitTime,
		"pause_dispatch_generation": evidence.DispatchGeneration,
		"update_time":               commitTime,
	}})
	if err != nil {
		return false, err
	}
	return result.MatchedCount == 1, nil
}

func (m *ExecutorTaskModel) FindPauseCommit(ctx context.Context, mainTaskID, taskID, generation string) (*PauseCommitEvidence, error) {
	if strings.TrimSpace(mainTaskID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(generation) == "" {
		return nil, fmt.Errorf("pause commit lookup requires exact task and dispatch generation")
	}
	var task ExecutorTask
	err := m.coll.FindOne(ctx, bson.M{
		"main_task_id":              mainTaskID,
		"task_id":                   taskID,
		"dispatch_generation":       generation,
		"pause_dispatch_generation": generation,
	}).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if task.PauseLeaseGeneration == "" || task.PauseCommitTime == nil || task.PauseCommitTime.IsZero() {
		return nil, nil
	}
	return &PauseCommitEvidence{
		TaskState:          task.TaskState,
		LeaseGeneration:    task.PauseLeaseGeneration,
		Worker:             task.PauseWorker,
		InstanceID:         task.PauseInstanceID,
		TaskProtocol:       task.PauseTaskProtocol,
		Phase:              task.PausePhase,
		CommitTime:         *task.PauseCommitTime,
		DispatchGeneration: task.PauseDispatchGeneration,
	}, nil
}

// EnsurePausedDispatch repairs or preserves PAUSED only while the same dispatch
// remains runnable/paused; STOP/REVOKE/terminal states always win.
func (m *MainTaskModel) EnsurePausedDispatch(ctx context.Context, id, generation, phase, taskState string) (bool, error) {
	fields := bson.M{}
	if phase != "" {
		fields["current_phase"] = phase
	}
	if taskState != "" {
		fields["task_state"] = taskState
	}
	return m.TransitionDispatchStatus(ctx, id, generation,
		[]string{TaskStatusPending, TaskStatusStarted, TaskStatusPaused}, TaskStatusPaused, fields)
}

// FindCompletionReconciliations returns a bounded least-recently-attempted
// snapshot. Owner update time and _id make equal/missing legacy cursors stable.
func (m *MainTaskModel) FindCompletionReconciliations(ctx context.Context, limit int64) ([]MainTask, error) {
	if limit <= 0 {
		limit = 50
	}
	cursor, err := m.coll.Find(ctx, bson.M{
		"completion_reconciliation.dispatch_generation": bson.M{"$exists": true, "$ne": ""},
	}, options.Find().SetSort(bson.D{
		{Key: "completion_reconciliation.reconcile_attempt_time", Value: 1},
		{Key: "completion_reconciliation.updated_time", Value: 1},
		{Key: "_id", Value: 1},
	}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var tasks []MainTask
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// ClaimCompletionReconciliation advances only the selected generation owner's
// fairness cursor. A replacement owner or competing visitor returns claimed=false.
func (m *MainTaskModel) ClaimCompletionReconciliation(
	ctx context.Context,
	id string,
	marker *TaskCompletionReconciliation,
) (*MainTask, bool, error) {
	if marker == nil || strings.TrimSpace(marker.DispatchGeneration) == "" {
		return nil, false, fmt.Errorf("exact completion reconciliation owner is required")
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, false, err
	}
	filter := bson.M{
		"_id": oid,
		"completion_reconciliation.dispatch_generation": marker.DispatchGeneration,
	}
	addReconcileAttemptFilter(filter,
		"completion_reconciliation.reconcile_attempt_time", marker.ReconcileAttemptTime)
	next := nextReconcileAttemptTime(marker.ReconcileAttemptTime)
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, filter, bson.M{"$set": bson.M{
		"completion_reconciliation.reconcile_attempt_time": next,
	}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

func exactCompletionReconciliationFilter(marker *TaskCompletionReconciliation) (bson.M, error) {
	if marker == nil || strings.TrimSpace(marker.DispatchGeneration) == "" || marker.UpdatedTime.IsZero() {
		return nil, fmt.Errorf("exact completion reconciliation marker is required")
	}
	filter := bson.M{
		"completion_reconciliation.dispatch_generation": marker.DispatchGeneration,
		"completion_reconciliation.updated_time":        bsonMillisecondTime(marker.UpdatedTime),
	}
	addReconcileAttemptFilter(filter,
		"completion_reconciliation.reconcile_attempt_time", marker.ReconcileAttemptTime)
	return filter, nil
}

// ClearCompletionReconciliationExact retires only the captured marker version.
// A same-generation fresh report or competing visitor survives this CAS.
func (m *MainTaskModel) ClearCompletionReconciliationExact(
	ctx context.Context,
	id string,
	marker *TaskCompletionReconciliation,
) (bool, error) {
	filter, err := exactCompletionReconciliationFilter(marker)
	if err != nil {
		return false, err
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	filter["_id"] = oid
	result, err := m.coll.UpdateOne(ctx, filter,
		bson.M{"$unset": bson.M{"completion_reconciliation": ""}})
	if err != nil {
		return false, err
	}
	return result.ModifiedCount == 1, nil
}

func (m *MainTaskModel) FindControlIntents(ctx context.Context, limit int64) ([]MainTask, error) {
	if limit <= 0 {
		limit = 50
	}
	cursor, err := m.coll.Find(ctx, bson.M{
		"control_intent":                     bson.M{"$exists": true, "$ne": nil},
		"control_intent.intent_id":           bson.M{"$exists": true, "$ne": ""},
		"control_intent.dispatch_generation": bson.M{"$exists": true, "$ne": ""},
	}, options.Find().SetSort(bson.D{
		{Key: "control_intent.reconcile_attempt_time", Value: 1},
		{Key: "control_intent.created_time", Value: 1},
		{Key: "_id", Value: 1},
	}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var tasks []MainTask
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// ClaimControlIntentReconciliation advances only the exact semantic intent's
// fairness cursor. Scheduling metadata is deliberately outside intent identity.
func (m *MainTaskModel) ClaimControlIntentReconciliation(
	ctx context.Context,
	id string,
	intent *TaskControlIntent,
) (*MainTask, bool, error) {
	if intent == nil || !intent.IsValid() {
		return nil, false, fmt.Errorf("exact control intent is required")
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, false, err
	}
	filter := exactControlIntentFilter(intent)
	filter["_id"] = oid
	addReconcileAttemptFilter(filter, "control_intent.reconcile_attempt_time", intent.ReconcileAttemptTime)
	next := nextReconcileAttemptTime(intent.ReconcileAttemptTime)
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, filter, bson.M{"$set": bson.M{
		"control_intent.reconcile_attempt_time": next,
	}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

// ClearControlIntentExact removes only the intent visit whose semantic identity
// and attempt cursor still match. A racing STOP or newer visitor is untouched.
func (m *MainTaskModel) ClearControlIntentExact(ctx context.Context, id string, intent *TaskControlIntent) (bool, error) {
	if intent == nil || !intent.IsValid() {
		return false, fmt.Errorf("exact control intent is required")
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	filter := exactControlIntentFilter(intent)
	filter["_id"] = oid
	addReconcileAttemptFilter(filter, "control_intent.reconcile_attempt_time", intent.ReconcileAttemptTime)
	result, err := m.coll.UpdateOne(ctx, filter, bson.M{
		"$unset": bson.M{"control_intent": ""},
		"$set":   bson.M{"update_time": time.Now()},
	})
	if err != nil {
		return false, err
	}
	return result.ModifiedCount == 1, nil
}

func controlIntentStatus(action string) string {
	switch action {
	case TaskControlActionPause:
		return TaskStatusPaused
	case TaskControlActionStop:
		return TaskStatusStopped
	default:
		return ""
	}
}

// RestoreControlIntentExact restores cleanup work only while the same parent
// status/generation still has no replacement intent. The claimed fairness
// cursor is preserved so an uncertain cleanup does not jump ahead of peers.
func (m *MainTaskModel) RestoreControlIntentExact(ctx context.Context, id string, intent *TaskControlIntent) (bool, error) {
	if intent == nil || !intent.IsValid() || controlIntentStatus(intent.Action) == "" {
		return false, fmt.Errorf("exact control intent is required")
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	restored := *intent
	restored.CreatedTime = bsonMillisecondTime(restored.CreatedTime)
	if restored.ReconcileAttemptTime != nil {
		restored.ReconcileAttemptTime = bsonTimePointer(*restored.ReconcileAttemptTime)
	}
	result, err := m.coll.UpdateOne(ctx, bson.M{
		"_id":                 oid,
		"status":              controlIntentStatus(restored.Action),
		"dispatch_generation": restored.DispatchGeneration,
		"$or": bson.A{
			bson.M{"control_intent": bson.M{"$exists": false}},
			bson.M{"control_intent": nil},
		},
	}, bson.M{"$set": bson.M{
		"control_intent": restored,
		"update_time":    time.Now(),
	}})
	if err != nil {
		return false, err
	}
	return result.ModifiedCount == 1, nil
}

func (m *MainTaskModel) FindPendingDispatches(ctx context.Context, limit int64) ([]MainTask, error) {
	if limit <= 0 {
		limit = 50
	}
	cursor, err := m.coll.Find(ctx, bson.M{
		"status":              TaskStatusPending,
		"dispatch_generation": bson.M{"$exists": true, "$ne": ""},
	}, options.Find().SetSort(bson.D{
		{Key: "status", Value: 1},
		{Key: "dispatch_reconcile_attempt_time", Value: 1},
		{Key: "dispatch_create_time", Value: 1},
		{Key: "_id", Value: 1},
	}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var tasks []MainTask
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// ClaimPendingDispatchReconciliation advances only the exact pending dispatch
// owner's fairness cursor and writes no decoded task fields back to Mongo.
func (m *MainTaskModel) ClaimPendingDispatchReconciliation(
	ctx context.Context,
	snapshot *MainTask,
) (*MainTask, bool, error) {
	if snapshot == nil || snapshot.Id.IsZero() || snapshot.Status != TaskStatusPending ||
		strings.TrimSpace(snapshot.DispatchGeneration) == "" ||
		(snapshot.DispatchIntent != DispatchIntentInitial && snapshot.DispatchIntent != DispatchIntentResume) ||
		snapshot.DispatchCreateTime == nil || snapshot.DispatchCreateTime.IsZero() {
		return nil, false, fmt.Errorf("exact pending dispatch owner is required")
	}
	filter := bson.M{
		"_id":                  snapshot.Id,
		"status":               TaskStatusPending,
		"dispatch_generation":  snapshot.DispatchGeneration,
		"dispatch_intent":      snapshot.DispatchIntent,
		"dispatch_create_time": bsonMillisecondTime(*snapshot.DispatchCreateTime),
	}
	addReconcileAttemptFilter(filter,
		"dispatch_reconcile_attempt_time", snapshot.DispatchReconcileAttemptTime)
	next := nextReconcileAttemptTime(snapshot.DispatchReconcileAttemptTime)
	var task MainTask
	err := m.coll.FindOneAndUpdate(ctx, filter, bson.M{"$set": bson.M{
		"dispatch_reconcile_attempt_time": next,
	}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&task)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

func (m *MainTaskModel) IsPendingDispatch(ctx context.Context, id, generation string) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	count, err := m.coll.CountDocuments(ctx, bson.M{
		"_id":                 oid,
		"status":              TaskStatusPending,
		"dispatch_generation": generation,
	})
	return count == 1, err
}

// ActivateDispatchDefinitions materializes the immutable generation manifest
// into executor snapshot rows only while the parent still selects this PENDING
// generation. Exact retries preserve runtime and pause evidence; a strictly
// newer generation resets all runtime state, while stale writers are no-ops.
func (m *ExecutorTaskModel) ActivateDispatchDefinitions(
	ctx context.Context,
	mainTaskID, generation string,
	dispatchCreateTime time.Time,
	definitions []ExecutorTask,
) error {
	mainTaskID = strings.TrimSpace(mainTaskID)
	generation = strings.TrimSpace(generation)
	if mainTaskID == "" || generation == "" || dispatchCreateTime.IsZero() || len(definitions) == 0 {
		return fmt.Errorf("dispatch activation requires parent, generation, create time, and definitions")
	}
	parentObjectID, err := primitive.ObjectIDFromHex(mainTaskID)
	if err != nil {
		return err
	}
	createTime := dispatchCreateTime.UTC().Truncate(time.Millisecond)
	activeParentFilter := bson.M{
		"_id":                  parentObjectID,
		"status":               TaskStatusPending,
		"dispatch_generation":  generation,
		"dispatch_create_time": createTime,
	}
	activeParents, err := m.coll.Database().Collection("maintask").CountDocuments(ctx, activeParentFilter)
	if err != nil {
		return err
	}
	if activeParents != 1 {
		return fmt.Errorf("%w: parent no longer selects dispatch %s", ErrTaskDispatchConflict, generation)
	}

	now := time.Now().UTC()
	epoch := primitive.NewDateTimeFromTime(time.Unix(0, 0).UTC())
	writes := make([]mongo.WriteModel, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.MainTaskId != mainTaskID || strings.TrimSpace(definition.TaskId) == "" ||
			strings.TrimSpace(definition.Config) == "" || definition.DispatchGeneration != generation {
			return fmt.Errorf("dispatch activation definition %q is incomplete or belongs to another generation", definition.TaskId)
		}
		if _, duplicate := seen[definition.TaskId]; duplicate {
			return fmt.Errorf("dispatch activation definition %s is duplicated", definition.TaskId)
		}
		seen[definition.TaskId] = struct{}{}

		sameActivation := bson.M{"$and": bson.A{
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$dispatch_generation", ""}}, generation}},
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$dispatch_create_time", nil}}, createTime}},
		}}
		missingActivation := bson.M{"$or": bson.A{
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$dispatch_generation", ""}}, ""}},
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$dispatch_create_time", nil}}, nil}},
		}}
		strictlyNewer := bson.M{"$lt": bson.A{
			bson.M{"$ifNull": bson.A{"$dispatch_create_time", epoch}}, createTime,
		}}
		replaceActivation := bson.M{"$and": bson.A{
			bson.M{"$not": bson.A{sameActivation}},
			bson.M{"$ne": bson.A{bson.M{"$ifNull": bson.A{"$dispatch_generation", ""}}, generation}},
			bson.M{"$or": bson.A{missingActivation, strictlyNewer}},
		}}
		canActivate := bson.M{"$or": bson.A{sameActivation, replaceActivation}}
		activateValue := func(value interface{}, currentField string) bson.M {
			return bson.M{"$cond": bson.A{canActivate, value, currentField}}
		}
		resetRuntime := func(currentField string) bson.M {
			return bson.M{"$cond": bson.A{replaceActivation, "$$REMOVE", currentField}}
		}

		pipeline := mongo.Pipeline{{{Key: "$set", Value: bson.M{
			"_id":                       bson.M{"$ifNull": bson.A{"$_id", primitive.NewObjectID()}},
			"main_task_id":              bson.M{"$ifNull": bson.A{"$main_task_id", mainTaskID}},
			"task_id":                   bson.M{"$ifNull": bson.A{"$task_id", definition.TaskId}},
			"task_name":                 activateValue(definition.TaskName, "$task_name"),
			"config":                    activateValue(definition.Config, "$config"),
			"priority":                  activateValue(definition.Priority, "$priority"),
			"status":                    bson.M{"$cond": bson.A{replaceActivation, TaskStatusPending, "$status"}},
			"worker":                    resetRuntime("$worker"),
			"result":                    resetRuntime("$result"),
			"task_state":                resetRuntime("$task_state"),
			"start_time":                resetRuntime("$start_time"),
			"end_time":                  resetRuntime("$end_time"),
			"create_time":               bson.M{"$cond": bson.A{canActivate, bson.M{"$ifNull": bson.A{"$create_time", now}}, "$create_time"}},
			"update_time":               activateValue(now, "$update_time"),
			"dispatch_generation":       activateValue(generation, "$dispatch_generation"),
			"dispatch_create_time":      activateValue(createTime, "$dispatch_create_time"),
			"pause_lease_generation":    resetRuntime("$pause_lease_generation"),
			"pause_worker":              resetRuntime("$pause_worker"),
			"pause_instance_id":         resetRuntime("$pause_instance_id"),
			"pause_task_protocol":       resetRuntime("$pause_task_protocol"),
			"pause_phase":               resetRuntime("$pause_phase"),
			"pause_commit_time":         resetRuntime("$pause_commit_time"),
			"pause_dispatch_generation": resetRuntime("$pause_dispatch_generation"),
		}}}}
		writes = append(writes, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"main_task_id": mainTaskID, "task_id": definition.TaskId}).
			SetUpdate(pipeline).
			SetUpsert(true))
	}
	if _, err := m.coll.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(true)); err != nil {
		return err
	}
	activeParents, err = m.coll.Database().Collection("maintask").CountDocuments(ctx, activeParentFilter)
	if err != nil {
		return err
	}
	if activeParents != 1 {
		return fmt.Errorf("%w: parent changed during dispatch activation", ErrTaskDispatchConflict)
	}

	cursor, err := m.coll.Find(ctx, bson.M{
		"main_task_id":         mainTaskID,
		"dispatch_generation":  generation,
		"dispatch_create_time": createTime,
		"config":               bson.M{"$exists": true, "$ne": ""},
	}, options.Find().SetProjection(bson.M{"task_id": 1}))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	activated := make(map[string]struct{}, len(definitions))
	for cursor.Next(ctx) {
		var row struct {
			TaskID string `bson:"task_id"`
		}
		if err := cursor.Decode(&row); err != nil {
			return err
		}
		activated[row.TaskID] = struct{}{}
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	if len(activated) != len(seen) {
		return fmt.Errorf("%w: activated dispatch manifest has %d definitions, expected %d",
			ErrTaskDispatchConflict, len(activated), len(seen))
	}
	for taskID := range seen {
		if _, ok := activated[taskID]; !ok {
			return fmt.Errorf("%w: dispatch definition %s was not activated", ErrTaskDispatchConflict, taskID)
		}
	}
	return nil
}
