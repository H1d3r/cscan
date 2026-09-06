package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

const (
	executorTaskUniqueIndexName     = "executor_task_main_task_id_task_id_uq"
	executorTaskMigrationCollection = "schema_migrations"
	// Retain the v3 marker identity so a v5 binary upgrades completed or
	// interrupted earlier work under the same cross-process lease. V5 can
	// protect duplicates still present, but cannot reconstruct rows already deleted.
	executorTaskMigrationID                           = "executor_task_natural_key_v3"
	executorTaskMigrationVersion                int32 = 5
	executorTaskMigrationStatePending                 = "pending"
	executorTaskMigrationStateRunning                 = "running"
	executorTaskMigrationStateComplete                = "complete"
	executorTaskMigrationIndexFingerprint             = "main_task_id:1,task_id:1;unique=true;sparse=false;partial=false;collation=default"
	executorTaskMigrationWriteFenceFingerprint        = "executor_task_string_natural_key_v1"
	executorTaskMigrationDedupPolicyFingerprint       = "executor_task_lossless_dedup_v5"
	executorTaskDuplicateQuarantineCollection         = "executor_task_duplicate_quarantine"
	executorTaskDuplicateQuarantineReason             = "confirmed duplicate natural-key group"

	executorTaskMigrationLeaseDuration           = 12 * time.Second
	executorTaskMigrationHeartbeatInterval       = 3 * time.Second
	executorTaskMigrationPollInterval            = 250 * time.Millisecond
	executorTaskMigrationLeaderTimeout           = 30 * time.Minute
	executorTaskMigrationBatchSize         int32 = 64
	executorTaskMigrationCheckpointGroups        = 128
	executorTaskMigrationInvalidBatchSize        = 128

	executorTaskPriorityMin = 0
	executorTaskPriorityMax = 4
)

var errExecutorTaskMigrationRetry = errors.New("executor task migration must retry")

func isValidExecutorTaskState(taskState string) bool {
	return taskState != "" && json.Valid([]byte(taskState))
}

// MigrateAndEnsureUniqueIndex deterministically merges historical duplicate
// executor rows before enforcing the natural (main_task_id, task_id) key.
func (m *ExecutorTaskModel) MigrateAndEnsureUniqueIndex(ctx context.Context) error {
	if m == nil || m.coll == nil {
		return fmt.Errorf("executor task model is not initialized")
	}

	markers := m.executorTaskMigrationMarkers()
	complete, err := m.executorTaskMigrationFastPath(ctx, markers)
	if err != nil {
		return fmt.Errorf("verify executor task migration: %w", err)
	}
	if complete {
		return nil
	}
	if err := ensureExecutorTaskMigrationMarker(ctx, markers); err != nil {
		return fmt.Errorf("initialize executor task migration marker: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		complete, err = m.executorTaskMigrationFastPath(ctx, markers)
		if err != nil {
			return fmt.Errorf("verify executor task migration: %w", err)
		}
		if complete {
			return nil
		}

		lease, acquired, err := acquireExecutorTaskMigrationLease(ctx, markers)
		if err != nil {
			return fmt.Errorf("acquire executor task migration lease: %w", err)
		}
		if !acquired {
			if err := waitForExecutorTaskMigration(ctx); err != nil {
				return err
			}
			continue
		}

		migrationErr := m.runExecutorTaskMigrationWithLease(ctx, markers, lease)
		if migrationErr == nil {
			return nil
		}

		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
		releaseDone := make(chan error, 1)
		go func() {
			defer cancelCleanup()
			releaseDone <- releaseExecutorTaskMigrationLease(cleanupCtx, markers, lease, migrationErr)
		}()
		select {
		case releaseErr := <-releaseDone:
			if releaseErr != nil {
				return fmt.Errorf("migrate executor tasks: %v (release lease: %w)", migrationErr, releaseErr)
			}
		case <-ctx.Done():
			// Cleanup remains bounded by its own context after the caller returns.
		}
		return fmt.Errorf("migrate executor tasks: %w", migrationErr)
	}
}

type executorTaskMigrationMarker struct {
	State                  string `bson:"state"`
	SchemaVersion          int32  `bson:"schema_version"`
	Owner                  string `bson:"owner"`
	Generation             int64  `bson:"generation"`
	IndexFingerprint       string `bson:"index_fingerprint"`
	WriteFenceFingerprint  string `bson:"write_fence_fingerprint"`
	DedupPolicyFingerprint string `bson:"dedup_policy_fingerprint"`
}

type executorTaskMigrationLease struct {
	owner      string
	generation int64
}

func (m *ExecutorTaskModel) executorTaskMigrationMarkers() *mongo.Collection {
	return m.coll.Database().Collection(
		executorTaskMigrationCollection,
		options.Collection().SetWriteConcern(writeconcern.Majority()),
	)
}

func (m *ExecutorTaskModel) executorTaskMigrationFastPath(ctx context.Context, markers *mongo.Collection) (bool, error) {
	var marker executorTaskMigrationMarker
	err := markers.FindOne(
		ctx,
		bson.M{"_id": executorTaskMigrationID},
		options.FindOne().SetProjection(bson.M{
			"state":                    1,
			"schema_version":           1,
			"index_fingerprint":        1,
			"write_fence_fingerprint":  1,
			"dedup_policy_fingerprint": 1,
		}),
	).Decode(&marker)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if marker.State != executorTaskMigrationStateComplete ||
		marker.SchemaVersion != executorTaskMigrationVersion ||
		marker.IndexFingerprint != executorTaskMigrationIndexFingerprint ||
		marker.WriteFenceFingerprint != executorTaskMigrationWriteFenceFingerprint ||
		marker.DedupPolicyFingerprint != executorTaskMigrationDedupPolicyFingerprint {
		return false, nil
	}

	exact, _, err := m.executorTaskHasExactUniqueIndex(ctx)
	if err != nil || !exact {
		return false, err
	}
	fenced, err := m.executorTaskHasCanonicalWriteFence(ctx, "strict")
	return fenced, err
}

func ensureExecutorTaskMigrationMarker(ctx context.Context, markers *mongo.Collection) error {
	now := time.Now()
	_, err := markers.UpdateOne(
		ctx,
		bson.M{"_id": executorTaskMigrationID},
		bson.M{"$setOnInsert": bson.M{
			"schema_version":           executorTaskMigrationVersion,
			"dedup_policy_fingerprint": executorTaskMigrationDedupPolicyFingerprint,
			"state":                    executorTaskMigrationStatePending,
			"generation":               int64(0),
			"created_at":               now,
			"updated_at":               now,
		}},
		options.Update().SetUpsert(true),
	)
	if mongo.IsDuplicateKeyError(err) {
		// Another process inserted the singleton marker between match and upsert.
		return nil
	}
	return err
}

func acquireExecutorTaskMigrationLease(
	ctx context.Context,
	markers *mongo.Collection,
) (executorTaskMigrationLease, bool, error) {
	owner := primitive.NewObjectID().Hex()
	filter := bson.D{
		{Key: "_id", Value: executorTaskMigrationID},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "state", Value: bson.D{{Key: "$ne", Value: executorTaskMigrationStateRunning}}}},
			bson.D{{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{
				bson.D{{Key: "$ifNull", Value: bson.A{"$lease_until", primitive.NewDateTimeFromTime(time.Unix(0, 0).UTC())}}},
				"$$NOW",
			}}}}},
		}},
	}
	restartDedupForPolicy := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{"$state", executorTaskMigrationStateComplete}}},
		bson.D{{Key: "$ne", Value: bson.A{
			bson.D{{Key: "$ifNull", Value: bson.A{"$schema_version", int32(0)}}},
			executorTaskMigrationVersion,
		}}},
		bson.D{{Key: "$ne", Value: bson.A{
			bson.D{{Key: "$ifNull", Value: bson.A{"$dedup_policy_fingerprint", ""}}},
			executorTaskMigrationDedupPolicyFingerprint,
		}}},
	}}}
	update := mongo.Pipeline{
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "schema_version", Value: executorTaskMigrationVersion},
			{Key: "dedup_policy_fingerprint", Value: executorTaskMigrationDedupPolicyFingerprint},
			{Key: "owner", Value: owner},
			{Key: "generation", Value: bson.D{{Key: "$add", Value: bson.A{
				bson.D{{Key: "$ifNull", Value: bson.A{"$generation", int64(0)}}},
				int64(1),
			}}}},
			{Key: "invalid_keys_complete", Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$eq", Value: bson.A{"$state", executorTaskMigrationStateComplete}}},
				false,
				bson.D{{Key: "$ifNull", Value: bson.A{"$invalid_keys_complete", false}}},
			}}}},
			{Key: "checkpoint_set", Value: bson.D{{Key: "$cond", Value: bson.A{
				restartDedupForPolicy,
				false,
				bson.D{{Key: "$ifNull", Value: bson.A{"$checkpoint_set", false}}},
			}}}},
			{Key: "checkpoint_main_id", Value: bson.D{{Key: "$cond", Value: bson.A{
				restartDedupForPolicy,
				"",
				bson.D{{Key: "$ifNull", Value: bson.A{"$checkpoint_main_id", ""}}},
			}}}},
			{Key: "checkpoint_task_id", Value: bson.D{{Key: "$cond", Value: bson.A{
				restartDedupForPolicy,
				"",
				bson.D{{Key: "$ifNull", Value: bson.A{"$checkpoint_task_id", ""}}},
			}}}},
			{Key: "state", Value: executorTaskMigrationStateRunning},
			{Key: "lease_until", Value: executorTaskLeaseUntilExpression()},
			{Key: "updated_at", Value: "$$NOW"},
		}}},
	}

	var marker executorTaskMigrationMarker
	err := markers.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&marker)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return executorTaskMigrationLease{}, false, nil
	}
	if err != nil {
		return executorTaskMigrationLease{}, false, err
	}
	if marker.Owner != owner || marker.State != executorTaskMigrationStateRunning {
		return executorTaskMigrationLease{}, false, fmt.Errorf("lease ownership was not persisted")
	}
	return executorTaskMigrationLease{owner: owner, generation: marker.Generation}, true, nil
}

func executorTaskLeaseUntilExpression() bson.D {
	return bson.D{{Key: "$add", Value: bson.A{
		"$$NOW",
		executorTaskMigrationLeaseDuration.Milliseconds(),
	}}}
}

func executorTaskActiveLeaseFilter(lease executorTaskMigrationLease) bson.D {
	return bson.D{
		{Key: "_id", Value: executorTaskMigrationID},
		{Key: "state", Value: executorTaskMigrationStateRunning},
		{Key: "owner", Value: lease.owner},
		{Key: "generation", Value: lease.generation},
		{Key: "$expr", Value: bson.D{{Key: "$gt", Value: bson.A{"$lease_until", "$$NOW"}}}},
	}
}

func waitForExecutorTaskMigration(ctx context.Context) error {
	timer := time.NewTimer(executorTaskMigrationPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *ExecutorTaskModel) runExecutorTaskMigrationWithLease(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
) error {
	// Bound leader work without detaching it from the startup caller. The same
	// context continues to govern lease heartbeats and durable checkpoints.
	migrationCtx, cancelMigration := context.WithTimeout(
		ctx,
		executorTaskMigrationLeaderTimeout,
	)
	defer cancelMigration()

	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go maintainExecutorTaskMigrationLease(
		migrationCtx,
		cancelMigration,
		markers,
		lease,
		stopHeartbeat,
		heartbeatDone,
	)

	migrationErr := m.runExecutorTaskOneTimeMigration(migrationCtx, markers, lease)
	close(stopHeartbeat)
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		return heartbeatErr
	}
	if migrationErr != nil {
		return migrationErr
	}

	// The marker is only committed after fresh catalog reads while the lease is
	// still active. This protects the startup fast path from a stale marker.
	exact, indexName, err := m.executorTaskHasExactUniqueIndex(migrationCtx)
	if err != nil {
		return fmt.Errorf("reverify executor task unique index: %w", err)
	}
	if !exact {
		return fmt.Errorf("reverify executor task unique index: exact index is missing")
	}
	fenced, err := m.executorTaskHasCanonicalWriteFence(migrationCtx, "strict")
	if err != nil {
		return fmt.Errorf("reverify executor task write fence: %w", err)
	}
	if !fenced {
		return fmt.Errorf("reverify executor task write fence: strict canonical-key validator is missing")
	}
	if err := completeExecutorTaskMigration(migrationCtx, markers, lease, indexName); err != nil {
		return err
	}
	return nil
}

func maintainExecutorTaskMigrationLease(
	ctx context.Context,
	cancelMigration context.CancelFunc,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
	stop <-chan struct{},
	done chan<- error,
) {
	ticker := time.NewTicker(executorTaskMigrationHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			done <- nil
			return
		case <-ctx.Done():
			done <- ctx.Err()
			return
		case <-ticker.C:
			matched, err := renewExecutorTaskMigrationLease(ctx, markers, lease)
			if err != nil {
				cancelMigration()
				done <- fmt.Errorf("renew executor task migration lease: %w", err)
				return
			}
			if !matched {
				cancelMigration()
				done <- fmt.Errorf("executor task migration lease was lost")
				return
			}
		}
	}
}

func renewExecutorTaskMigrationLease(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
) (bool, error) {
	result, err := markers.UpdateOne(
		ctx,
		executorTaskActiveLeaseFilter(lease),
		mongo.Pipeline{bson.D{{Key: "$set", Value: bson.D{
			{Key: "lease_until", Value: executorTaskLeaseUntilExpression()},
			{Key: "updated_at", Value: "$$NOW"},
		}}}},
	)
	if err != nil {
		return false, err
	}
	return result.MatchedCount == 1, nil
}

func assertExecutorTaskMigrationLease(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
) error {
	err := markers.FindOne(
		ctx,
		executorTaskActiveLeaseFilter(lease),
		options.FindOne().SetProjection(bson.M{"_id": 1}),
	).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("executor task migration lease was lost")
	}
	return err
}

func completeExecutorTaskMigration(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
	indexName string,
) error {
	result, err := markers.UpdateOne(
		ctx,
		executorTaskActiveLeaseFilter(lease),
		bson.M{
			"$set": bson.M{
				"schema_version":           executorTaskMigrationVersion,
				"state":                    executorTaskMigrationStateComplete,
				"index_name":               indexName,
				"index_fingerprint":        executorTaskMigrationIndexFingerprint,
				"write_fence_fingerprint":  executorTaskMigrationWriteFenceFingerprint,
				"dedup_policy_fingerprint": executorTaskMigrationDedupPolicyFingerprint,
			},
			"$unset": bson.M{
				"owner":              "",
				"lease_until":        "",
				"last_error":         "",
				"checkpoint_main_id": "",
				"checkpoint_task_id": "",
				"checkpoint_set":     "",
			},
			"$currentDate": bson.M{
				"completed_at": true,
				"updated_at":   true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("complete executor task migration marker: %w", err)
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("complete executor task migration marker: lease was lost")
	}
	return nil
}

func releaseExecutorTaskMigrationLease(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
	migrationErr error,
) error {
	result, err := markers.UpdateOne(
		ctx,
		bson.M{
			"_id":        executorTaskMigrationID,
			"state":      executorTaskMigrationStateRunning,
			"owner":      lease.owner,
			"generation": lease.generation,
		},
		bson.M{
			"$set": bson.M{
				"state":      executorTaskMigrationStatePending,
				"last_error": migrationErr.Error(),
			},
			"$unset": bson.M{
				"owner":       "",
				"lease_until": "",
			},
			"$currentDate": bson.M{"updated_at": true},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		// A newer generation owns the marker; never overwrite it.
		return nil
	}
	return nil
}

func executorTaskCanonicalKeyValidator() bson.D {
	return bson.D{{Key: "$jsonSchema", Value: bson.D{
		{Key: "bsonType", Value: "object"},
		{Key: "description", Value: executorTaskMigrationWriteFenceFingerprint},
		{Key: "required", Value: bson.A{"main_task_id", "task_id"}},
		{Key: "properties", Value: bson.D{
			{Key: "main_task_id", Value: bson.D{{Key: "bsonType", Value: "string"}, {Key: "minLength", Value: 1}}},
			{Key: "task_id", Value: bson.D{{Key: "bsonType", Value: "string"}, {Key: "minLength", Value: 1}}},
		}},
	}}}
}

type executorTaskCollectionValidation struct {
	exists    bool
	validator bson.Raw
	level     string
	action    string
}

func (m *ExecutorTaskModel) ensureExecutorTaskCanonicalWriteFence(ctx context.Context, requestedLevel string) error {
	for attempt := 0; attempt < 2; attempt++ {
		current, err := m.executorTaskCollectionValidation(ctx)
		if err != nil {
			return err
		}
		if !current.exists {
			err = m.coll.Database().CreateCollection(
				ctx,
				m.coll.Name(),
				options.CreateCollection().
					SetValidator(executorTaskCanonicalKeyValidator()).
					SetValidationLevel(requestedLevel).
					SetValidationAction("error"),
			)
			if err == nil {
				return nil
			}
			var commandErr mongo.CommandError
			if errors.As(err, &commandErr) && commandErr.HasErrorCode(48) {
				continue
			}
			return fmt.Errorf("create executor task collection with canonical-key validator: %w", err)
		}

		targetLevel := requestedLevel
		levelSatisfied := current.level == targetLevel
		if executorRawContainsValidatorMarker(current.validator) && levelSatisfied && current.action == "error" {
			return nil
		}

		var validator interface{} = executorTaskCanonicalKeyValidator()
		if len(current.validator) > 0 {
			if executorRawContainsValidatorMarker(current.validator) {
				validator = current.validator
			} else {
				validator = bson.D{{Key: "$and", Value: bson.A{
					current.validator,
					executorTaskCanonicalKeyValidator(),
				}}}
			}
		}
		if err := m.coll.Database().RunCommand(ctx, bson.D{
			{Key: "collMod", Value: m.coll.Name()},
			{Key: "validator", Value: validator},
			{Key: "validationLevel", Value: targetLevel},
			{Key: "validationAction", Value: "error"},
		}).Err(); err != nil {
			return fmt.Errorf("install executor task canonical-key validator: %w", err)
		}

		fenced, err := m.executorTaskHasCanonicalWriteFence(ctx, targetLevel)
		if err != nil {
			return err
		}
		if !fenced {
			return fmt.Errorf("executor task canonical-key validator was not installed")
		}
		return nil
	}
	return fmt.Errorf("executor task collection changed while installing canonical-key validator")
}

func (m *ExecutorTaskModel) executorTaskHasCanonicalWriteFence(ctx context.Context, requiredLevel string) (bool, error) {
	current, err := m.executorTaskCollectionValidation(ctx)
	if err != nil || !current.exists {
		return false, err
	}
	levelSatisfied := current.level == requiredLevel || (requiredLevel == "moderate" && current.level == "strict")
	return executorRawContainsValidatorMarker(current.validator) && levelSatisfied && current.action == "error", nil
}

func (m *ExecutorTaskModel) executorTaskCollectionValidation(ctx context.Context) (executorTaskCollectionValidation, error) {
	specs, err := m.coll.Database().ListCollectionSpecifications(ctx, bson.M{"name": m.coll.Name()})
	if err != nil {
		return executorTaskCollectionValidation{}, err
	}
	if len(specs) == 0 {
		return executorTaskCollectionValidation{}, nil
	}
	if len(specs) != 1 || specs[0].Type != "collection" {
		return executorTaskCollectionValidation{}, fmt.Errorf("executor task namespace is not a collection")
	}

	result := executorTaskCollectionValidation{
		exists: true,
		level:  "strict",
		action: "error",
	}
	if value, err := specs[0].Options.LookupErr("validator"); err == nil {
		if validator, ok := value.DocumentOK(); ok {
			result.validator = validator
		} else {
			return executorTaskCollectionValidation{}, fmt.Errorf("executor task collection has invalid validator metadata")
		}
	}
	if value, err := specs[0].Options.LookupErr("validationLevel"); err == nil {
		if level, ok := value.StringValueOK(); ok {
			result.level = level
		}
	}
	if value, err := specs[0].Options.LookupErr("validationAction"); err == nil {
		if action, ok := value.StringValueOK(); ok {
			result.action = action
		}
	}
	return result, nil
}

func executorRawContainsValidatorMarker(raw bson.Raw) bool {
	if len(raw) == 0 {
		return false
	}
	if value, err := raw.LookupErr("description"); err == nil {
		if description, ok := value.StringValueOK(); ok && description == executorTaskMigrationWriteFenceFingerprint {
			return true
		}
	}
	elements, err := raw.Elements()
	if err != nil {
		return false
	}
	for _, element := range elements {
		value := element.Value()
		switch value.Type {
		case bsontype.EmbeddedDocument:
			if executorRawContainsValidatorMarker(value.Document()) {
				return true
			}
		case bsontype.Array:
			values, err := value.Array().Values()
			if err != nil {
				continue
			}
			for _, arrayValue := range values {
				if arrayValue.Type == bsontype.EmbeddedDocument && executorRawContainsValidatorMarker(arrayValue.Document()) {
					return true
				}
			}
		}
	}
	return false
}

func (m *ExecutorTaskModel) runExecutorTaskOneTimeMigration(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
) error {
	for {
		if err := assertExecutorTaskMigrationLease(ctx, markers, lease); err != nil {
			return err
		}
		// A moderate validator rejects new incompatible inserts while allowing
		// bypassed migration updates to historical mixed-key documents.
		if err := m.ensureExecutorTaskCanonicalWriteFence(ctx, "moderate"); err != nil {
			return err
		}

		progress, err := loadExecutorTaskMigrationProgress(ctx, markers, lease)
		if err != nil {
			return err
		}
		if !progress.invalidKeysComplete {
			if err := m.removeInvalidExecutorTaskRows(ctx, markers, lease); err != nil {
				return fmt.Errorf("remove invalid executor tasks: %w", err)
			}
			if err := markExecutorTaskInvalidKeysComplete(ctx, markers, lease); err != nil {
				return err
			}
			progress.invalidKeysComplete = true
		}

		if err := m.deduplicateLegacyRows(ctx, markers, lease, progress); err != nil {
			if errors.Is(err, errExecutorTaskMigrationRetry) {
				continue
			}
			return fmt.Errorf("deduplicate executor tasks: %w", err)
		}

		canonical, err := m.executorTaskRowsAreCanonical(ctx)
		if err != nil {
			return fmt.Errorf("validate canonical executor task keys: %w", err)
		}
		if !canonical {
			if err := resetExecutorTaskMigrationProgress(ctx, markers, lease, true); err != nil {
				return err
			}
			continue
		}

		if err := m.ensureVerifiedUniqueIndex(ctx); err != nil {
			if errors.Is(err, errExecutorTaskMigrationRetry) {
				// A canonical duplicate was written after its checkpoint while no
				// exact index existed. Rewind only the key-range checkpoint.
				if resetErr := resetExecutorTaskMigrationProgress(ctx, markers, lease, false); resetErr != nil {
					return resetErr
				}
				continue
			}
			return fmt.Errorf("ensure executor task unique index: %w", err)
		}

		// Strict validation is installed before the final scan. From this point,
		// no new ObjectID/missing/empty natural key can enter the completion gap.
		if err := m.ensureExecutorTaskCanonicalWriteFence(ctx, "strict"); err != nil {
			return err
		}
		canonical, err = m.executorTaskRowsAreCanonical(ctx)
		if err != nil {
			return fmt.Errorf("revalidate canonical executor task keys: %w", err)
		}
		if !canonical {
			if err := resetExecutorTaskMigrationProgress(ctx, markers, lease, true); err != nil {
				return err
			}
			continue
		}

		exact, _, err := m.executorTaskHasExactUniqueIndex(ctx)
		if err != nil {
			return err
		}
		fenced, err := m.executorTaskHasCanonicalWriteFence(ctx, "strict")
		if err != nil {
			return err
		}
		if !exact || !fenced {
			continue
		}
		return nil
	}
}

type executorTaskMigrationProgress struct {
	invalidKeysComplete bool
	checkpointSet       bool
	checkpointMainID    string
	checkpointTaskID    string
}

func loadExecutorTaskMigrationProgress(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
) (executorTaskMigrationProgress, error) {
	var marker struct {
		InvalidKeysComplete bool   `bson:"invalid_keys_complete"`
		CheckpointSet       bool   `bson:"checkpoint_set"`
		CheckpointMainID    string `bson:"checkpoint_main_id"`
		CheckpointTaskID    string `bson:"checkpoint_task_id"`
	}
	err := markers.FindOne(
		ctx,
		executorTaskActiveLeaseFilter(lease),
		options.FindOne().SetProjection(bson.M{
			"invalid_keys_complete": 1,
			"checkpoint_set":        1,
			"checkpoint_main_id":    1,
			"checkpoint_task_id":    1,
		}),
	).Decode(&marker)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return executorTaskMigrationProgress{}, fmt.Errorf("executor task migration lease was lost")
	}
	if err != nil {
		return executorTaskMigrationProgress{}, err
	}
	return executorTaskMigrationProgress{
		invalidKeysComplete: marker.InvalidKeysComplete,
		checkpointSet:       marker.CheckpointSet,
		checkpointMainID:    marker.CheckpointMainID,
		checkpointTaskID:    marker.CheckpointTaskID,
	}, nil
}

func markExecutorTaskInvalidKeysComplete(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
) error {
	result, err := markers.UpdateOne(
		ctx,
		executorTaskActiveLeaseFilter(lease),
		bson.M{
			"$set":         bson.M{"invalid_keys_complete": true},
			"$currentDate": bson.M{"updated_at": true},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("executor task migration lease was lost while recording invalid-key phase")
	}
	return nil
}

func checkpointExecutorTaskMigration(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
	mainID string,
	taskID string,
) error {
	result, err := markers.UpdateOne(
		ctx,
		executorTaskActiveLeaseFilter(lease),
		bson.M{
			"$set": bson.M{
				"checkpoint_set":     true,
				"checkpoint_main_id": mainID,
				"checkpoint_task_id": taskID,
			},
			"$currentDate": bson.M{"updated_at": true},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("executor task migration lease was lost while checkpointing")
	}
	return nil
}

func resetExecutorTaskMigrationProgress(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
	resetInvalidKeys bool,
) error {
	set := bson.M{"checkpoint_set": false}
	if resetInvalidKeys {
		set["invalid_keys_complete"] = false
	}
	result, err := markers.UpdateOne(
		ctx,
		executorTaskActiveLeaseFilter(lease),
		bson.M{
			"$set": set,
			"$unset": bson.M{
				"checkpoint_main_id": "",
				"checkpoint_task_id": "",
			},
			"$currentDate": bson.M{"updated_at": true},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("executor task migration lease was lost while rewinding progress")
	}
	return nil
}

type invalidExecutorTaskRow struct {
	ID         interface{} `bson:"_id"`
	MainIDType string      `bson:"main_id_type"`
	TaskIDType string      `bson:"task_id_type"`
}

func (m *ExecutorTaskModel) removeInvalidExecutorTaskRows(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
) error {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "main_id_type", Value: bson.D{{Key: "$type", Value: "$main_task_id"}}},
			{Key: "task_id_type", Value: bson.D{{Key: "$type", Value: "$task_id"}}},
			{Key: "normalized_main_id", Value: executorNormalizedIdentifierExpression("main_task_id")},
			{Key: "normalized_task_id", Value: executorNormalizedIdentifierExpression("task_id")},
		}}},
		bson.D{{Key: "$match", Value: bson.D{{Key: "$expr", Value: bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{"$normalized_main_id", nil}}},
			bson.D{{Key: "$eq", Value: bson.A{"$normalized_task_id", nil}}},
			bson.D{{Key: "$eq", Value: bson.A{"$normalized_main_id", ""}}},
			bson.D{{Key: "$eq", Value: bson.A{"$normalized_task_id", ""}}},
		}}}}}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "main_id_type", Value: 1},
			{Key: "task_id_type", Value: 1},
		}}},
	}
	cursor, err := m.coll.Aggregate(
		ctx,
		pipeline,
		options.Aggregate().SetAllowDiskUse(true).SetBatchSize(executorTaskMigrationBatchSize),
	)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	rows := make([]invalidExecutorTaskRow, 0, executorTaskMigrationInvalidBatchSize)
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		if err := assertExecutorTaskMigrationLease(ctx, markers, lease); err != nil {
			return err
		}
		writes := make([]mongo.WriteModel, 0, len(rows))
		ids := make([]interface{}, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		documentCursor, err := m.coll.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
		if err != nil {
			return fmt.Errorf("load invalid executor rows for quarantine: %w", err)
		}
		var documents []bson.M
		if err := documentCursor.All(ctx, &documents); err != nil {
			documentCursor.Close(ctx)
			return fmt.Errorf("decode invalid executor rows for quarantine: %w", err)
		}
		documentCursor.Close(ctx)
		documentsByID := make(map[string]bson.M, len(documents))
		for _, document := range documents {
			documentsByID[fmt.Sprintf("%T:%v", document["_id"], document["_id"])] = document
		}
		now := time.Now()
		for _, row := range rows {
			document, exists := documentsByID[fmt.Sprintf("%T:%v", row.ID, row.ID)]
			if !exists {
				return fmt.Errorf("invalid executor row %v disappeared before quarantine", row.ID)
			}
			writes = append(writes, mongo.NewUpdateOneModel().
				SetFilter(bson.M{"source_id": row.ID}).
				SetUpdate(bson.M{"$set": bson.M{
					"source_id":      row.ID,
					"reason":         "missing or invalid natural key",
					"main_id_type":   row.MainIDType,
					"task_id_type":   row.TaskIDType,
					"document":       document,
					"quarantined_at": now,
				}}).
				SetUpsert(true))
		}
		quarantine := m.coll.Database().Collection("executor_task_quarantine")
		if _, err := quarantine.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(true)); err != nil {
			return fmt.Errorf("record invalid executor rows: %w", err)
		}
		if _, err := m.coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}}); err != nil {
			return fmt.Errorf("delete invalid executor rows: %w", err)
		}
		rows = rows[:0]
		return nil
	}

	for cursor.Next(ctx) {
		var row invalidExecutorTaskRow
		if err := cursor.Decode(&row); err != nil {
			return err
		}
		rows = append(rows, row)
		if len(rows) == cap(rows) {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	return flush()
}

type legacyExecutorRow struct {
	ID            interface{}        `bson:"_id"`
	MainID        string             `bson:"normalized_main_id"`
	TaskID        string             `bson:"normalized_task_id"`
	MainIDType    string             `bson:"main_id_type"`
	TaskIDType    string             `bson:"task_id_type"`
	EffectiveTime primitive.DateTime `bson:"effective_time"`
}

type executorMergeState struct {
	hasTaskState    bool
	hasConfig       bool
	hasTaskName     bool
	hasPriority     bool
	invalidPriority bool
}

type executorMergeGroup struct {
	mainID string
	taskID string
	rows   []legacyExecutorRow
}

type executorTaskSnapshot struct {
	row      legacyExecutorRow
	document bson.Raw
}

func (m *ExecutorTaskModel) deduplicateLegacyRows(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
	progress executorTaskMigrationProgress,
) error {
	cursor, err := m.coll.Aggregate(
		ctx,
		executorTaskMigrationMetadataPipeline(progress),
		options.Aggregate().
			SetAllowDiskUse(true).
			SetBatchSize(executorTaskMigrationBatchSize).
			SetCollation(&options.Collation{Locale: "simple"}),
	)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var group *executorMergeGroup
	completedSinceCheckpoint := 0
	lastMainID := ""
	lastTaskID := ""
	finishGroup := func() error {
		if group == nil {
			return nil
		}
		if err := m.finishExecutorMergeGroup(ctx, markers, lease, group); err != nil {
			return err
		}
		lastMainID = group.mainID
		lastTaskID = group.taskID
		completedSinceCheckpoint++
		group = nil
		if completedSinceCheckpoint < executorTaskMigrationCheckpointGroups {
			return nil
		}
		if err := checkpointExecutorTaskMigration(ctx, markers, lease, lastMainID, lastTaskID); err != nil {
			return err
		}
		completedSinceCheckpoint = 0
		return nil
	}

	for cursor.Next(ctx) {
		var row legacyExecutorRow
		if err := cursor.Decode(&row); err != nil {
			return err
		}
		if row.MainID == "" || row.TaskID == "" {
			return fmt.Errorf("projected executor task row %v has an invalid natural key", row.ID)
		}

		if group == nil || group.mainID != row.MainID || group.taskID != row.TaskID {
			if err := finishGroup(); err != nil {
				return err
			}
			group = startExecutorMergeGroup(row)
			continue
		}

		group.rows = append(group.rows, row)
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	if err := finishGroup(); err != nil {
		return err
	}
	if completedSinceCheckpoint > 0 {
		if err := checkpointExecutorTaskMigration(ctx, markers, lease, lastMainID, lastTaskID); err != nil {
			return err
		}
	}
	return nil
}

func executorTaskMigrationMetadataPipeline(progress executorTaskMigrationProgress) mongo.Pipeline {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "main_id_type", Value: bson.D{{Key: "$type", Value: "$main_task_id"}}},
			{Key: "task_id_type", Value: bson.D{{Key: "$type", Value: "$task_id"}}},
			{Key: "normalized_main_id", Value: executorNormalizedIdentifierExpression("main_task_id")},
			{Key: "normalized_task_id", Value: executorNormalizedIdentifierExpression("task_id")},
			{Key: "effective_time", Value: executorEffectiveTimeExpression()},
		}}},
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "normalized_main_id", Value: bson.D{{Key: "$nin", Value: bson.A{nil, ""}}}},
			{Key: "normalized_task_id", Value: bson.D{{Key: "$nin", Value: bson.A{nil, ""}}}},
		}}},
	}
	if progress.checkpointSet {
		checkpointMatch := bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "normalized_main_id", Value: bson.D{{Key: "$gt", Value: progress.checkpointMainID}}}},
			bson.D{
				{Key: "normalized_main_id", Value: progress.checkpointMainID},
				{Key: "normalized_task_id", Value: bson.D{{Key: "$gt", Value: progress.checkpointTaskID}}},
			},
		}}}
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: checkpointMatch}})
	}
	return append(
		pipeline,
		bson.D{{Key: "$sort", Value: bson.D{
			{Key: "normalized_main_id", Value: 1},
			{Key: "normalized_task_id", Value: 1},
			{Key: "effective_time", Value: -1},
			{Key: "_id", Value: -1},
		}}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "normalized_main_id", Value: 1},
			{Key: "normalized_task_id", Value: 1},
			{Key: "main_id_type", Value: 1},
			{Key: "task_id_type", Value: 1},
			{Key: "effective_time", Value: 1},
		}}},
	)
}

func executorNormalizedIdentifierExpression(field string) bson.D {
	path := "$" + field
	stringType := bson.D{{Key: "$eq", Value: bson.A{
		bson.D{{Key: "$type", Value: path}},
		"string",
	}}}
	objectIDType := bson.D{{Key: "$and", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: path}}, "objectId"}}},
		bson.D{{Key: "$ne", Value: bson.A{path, primitive.NilObjectID}}},
	}}}
	return bson.D{{Key: "$switch", Value: bson.D{
		{Key: "branches", Value: bson.A{
			bson.D{{Key: "case", Value: stringType}, {Key: "then", Value: path}},
			bson.D{{Key: "case", Value: objectIDType}, {Key: "then", Value: bson.D{{Key: "$toString", Value: path}}}},
		}},
		{Key: "default", Value: nil},
	}}}
}

func executorEffectiveTimeExpression() bson.D {
	return bson.D{{Key: "$switch", Value: bson.D{
		{Key: "branches", Value: bson.A{
			bson.D{
				{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$update_time"}}, "date"}}}},
				{Key: "then", Value: "$update_time"},
			},
			bson.D{
				{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$create_time"}}, "date"}}}},
				{Key: "then", Value: "$create_time"},
			},
			bson.D{
				{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$_id"}}, "objectId"}}}},
				{Key: "then", Value: bson.D{{Key: "$toDate", Value: "$_id"}}},
			},
		}},
		{Key: "default", Value: primitive.NewDateTimeFromTime(time.Unix(0, 0).UTC())},
	}}}
}

func startExecutorMergeGroup(row legacyExecutorRow) *executorMergeGroup {
	return &executorMergeGroup{
		mainID: row.MainID,
		taskID: row.TaskID,
		rows:   []legacyExecutorRow{row},
	}
}

func (m *ExecutorTaskModel) loadExecutorMergeGroupSnapshots(
	ctx context.Context,
	group *executorMergeGroup,
) ([]executorTaskSnapshot, error) {
	snapshots := make([]executorTaskSnapshot, 0, len(group.rows))
	for _, row := range group.rows {
		var document bson.Raw
		err := m.coll.FindOne(ctx, bson.M{"_id": row.ID}).Decode(&document)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, fmt.Errorf("%w: executor merge member %v disappeared", errExecutorTaskMigrationRetry, row.ID)
		}
		if err != nil {
			return nil, fmt.Errorf("load executor merge member %v: %w", row.ID, err)
		}

		// Keep an owned byte-for-byte BSON snapshot. It is used unchanged for
		// quarantine, merge input, and the later compare-aware write/delete.
		document = append(bson.Raw(nil), document...)
		mainID, mainOK := executorRawIdentifier(document, "main_task_id")
		taskID, taskOK := executorRawIdentifier(document, "task_id")
		if !mainOK || !taskOK || mainID != group.mainID || taskID != group.taskID {
			return nil, fmt.Errorf("%w: executor merge member %v changed natural key", errExecutorTaskMigrationRetry, row.ID)
		}
		if executorRawEffectiveTime(document) != row.EffectiveTime {
			return nil, fmt.Errorf("%w: executor merge member %v changed rank", errExecutorTaskMigrationRetry, row.ID)
		}
		snapshots = append(snapshots, executorTaskSnapshot{row: row, document: document})
	}
	return snapshots, nil
}

func executorRawIdentifier(raw bson.Raw, field string) (string, bool) {
	value, err := raw.LookupErr(field)
	if err != nil {
		return "", false
	}
	switch value.Type {
	case bsontype.String:
		text := value.StringValue()
		return text, text != ""
	case bsontype.ObjectID:
		id := value.ObjectID()
		return id.Hex(), !id.IsZero()
	default:
		return "", false
	}
}

func executorRawEffectiveTime(raw bson.Raw) primitive.DateTime {
	for _, field := range []string{"update_time", "create_time"} {
		if value, err := raw.LookupErr(field); err == nil && value.Type == bsontype.DateTime {
			return primitive.DateTime(value.DateTime())
		}
	}
	if value, err := raw.LookupErr("_id"); err == nil && value.Type == bsontype.ObjectID {
		return primitive.NewDateTimeFromTime(value.ObjectID().Timestamp())
	}
	return primitive.NewDateTimeFromTime(time.Unix(0, 0).UTC())
}

func executorDuplicateQuarantineIdentity(document bson.Raw) (archiveID string, documentDigest string) {
	documentHash := sha256.Sum256(document)
	hash := sha256.New()
	_, _ = hash.Write([]byte(executorTaskMigrationDedupPolicyFingerprint))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(document)
	return fmt.Sprintf("%s:%x", executorTaskMigrationDedupPolicyFingerprint, hash.Sum(nil)),
		fmt.Sprintf("%x", documentHash[:])
}

type executorDuplicateArchiveExpectation struct {
	archiveID      string
	documentDigest string
	snapshot       executorTaskSnapshot
	stableFilter   bson.D
}

func (m *ExecutorTaskModel) quarantineExecutorDuplicateGroup(
	ctx context.Context,
	group *executorMergeGroup,
	snapshots []executorTaskSnapshot,
	physicalSurvivorIndex int,
) error {
	if len(snapshots) < 2 {
		return nil
	}

	if physicalSurvivorIndex < 0 || physicalSurvivorIndex >= len(snapshots) {
		return fmt.Errorf("physical survivor index %d is outside duplicate group", physicalSurvivorIndex)
	}

	logicalWinnerID := snapshots[0].row.ID
	physicalSurvivorID := snapshots[physicalSurvivorIndex].row.ID
	observedDecision := bson.D{
		{Key: "logical_winner_id", Value: logicalWinnerID},
		{Key: "physical_survivor_id", Value: physicalSurvivorID},
	}
	quarantinedAt := time.Now().UTC()
	naturalKey := bson.D{
		{Key: "main_task_id", Value: group.mainID},
		{Key: "task_id", Value: group.taskID},
	}
	writes := make([]mongo.WriteModel, 0, len(snapshots))
	expectations := make([]executorDuplicateArchiveExpectation, 0, len(snapshots))
	for index, snapshot := range snapshots {
		archiveID, documentDigest := executorDuplicateQuarantineIdentity(snapshot.document)
		logicalRole := "loser"
		if index == 0 {
			logicalRole = "winner"
		}
		physicalRole := "non_survivor"
		if index == physicalSurvivorIndex {
			physicalRole = "survivor"
		}

		// The exact source preimage, not the current dedup decision, determines
		// _id. An unchanged retry is therefore a no-op even if a partial merge
		// changes ranking; a changed source version receives its own record.
		stableFilter := bson.D{
			{Key: "_id", Value: archiveID},
			{Key: "migration_id", Value: executorTaskMigrationID},
			{Key: "schema_version", Value: executorTaskMigrationVersion},
			{Key: "dedup_policy_fingerprint", Value: executorTaskMigrationDedupPolicyFingerprint},
			{Key: "kind", Value: "confirmed_duplicate"},
			{Key: "source_id", Value: snapshot.row.ID},
			{Key: "natural_key", Value: naturalKey},
			{Key: "reason", Value: executorTaskDuplicateQuarantineReason},
			{Key: "document_sha256", Value: documentDigest},
		}
		record := bson.D{
			{Key: "_id", Value: archiveID},
			{Key: "migration_id", Value: executorTaskMigrationID},
			{Key: "schema_version", Value: executorTaskMigrationVersion},
			{Key: "dedup_policy_fingerprint", Value: executorTaskMigrationDedupPolicyFingerprint},
			{Key: "kind", Value: "confirmed_duplicate"},
			{Key: "source_id", Value: snapshot.row.ID},
			{Key: "natural_key", Value: naturalKey},
			{Key: "winner_id", Value: logicalWinnerID},
			{Key: "logical_winner_id", Value: logicalWinnerID},
			{Key: "physical_survivor_id", Value: physicalSurvivorID},
			{Key: "reason", Value: executorTaskDuplicateQuarantineReason},
			{Key: "member_role", Value: logicalRole},
			{Key: "physical_member_role", Value: physicalRole},
			{Key: "document", Value: snapshot.document},
			{Key: "document_sha256", Value: documentDigest},
			{Key: "quarantined_at", Value: quarantinedAt},
		}
		writes = append(writes, mongo.NewUpdateOneModel().
			SetFilter(stableFilter).
			SetUpdate(bson.D{
				{Key: "$setOnInsert", Value: record},
				{Key: "$addToSet", Value: bson.D{
					{Key: "observed_winner_ids", Value: logicalWinnerID},
					{Key: "observed_logical_winner_ids", Value: logicalWinnerID},
					{Key: "observed_physical_survivor_ids", Value: physicalSurvivorID},
					{Key: "observed_dedup_decisions", Value: observedDecision},
				}},
			}).
			SetUpsert(true))
		expectations = append(expectations, executorDuplicateArchiveExpectation{
			archiveID:      archiveID,
			documentDigest: documentDigest,
			snapshot:       snapshot,
			stableFilter:   stableFilter,
		})
	}

	quarantine := m.coll.Database().Collection(
		executorTaskDuplicateQuarantineCollection,
		options.Collection().SetWriteConcern(writeconcern.Majority()),
	)
	if _, err := quarantine.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(true)); err != nil {
		return fmt.Errorf("archive executor duplicate group (%q, %q): %w", group.mainID, group.taskID, err)
	}

	// A successful upsert is not enough: a pre-existing malformed record could
	// satisfy only the identity fields. Read every archive back and require all
	// provenance plus the exact raw BSON bytes before modifying source rows.
	for _, expected := range expectations {
		verificationFilter := append(bson.D(nil), expected.stableFilter...)
		verificationFilter = append(verificationFilter,
			bson.E{Key: "winner_id", Value: bson.D{{Key: "$exists", Value: true}}},
			bson.E{Key: "logical_winner_id", Value: bson.D{{Key: "$exists", Value: true}}},
			bson.E{Key: "physical_survivor_id", Value: bson.D{{Key: "$exists", Value: true}}},
			bson.E{Key: "member_role", Value: bson.D{{Key: "$type", Value: "string"}}},
			bson.E{Key: "physical_member_role", Value: bson.D{{Key: "$type", Value: "string"}}},
			bson.E{Key: "quarantined_at", Value: bson.D{{Key: "$type", Value: "date"}}},
			bson.E{Key: "observed_winner_ids", Value: logicalWinnerID},
			bson.E{Key: "observed_logical_winner_ids", Value: logicalWinnerID},
			bson.E{Key: "observed_physical_survivor_ids", Value: physicalSurvivorID},
			bson.E{Key: "observed_dedup_decisions", Value: observedDecision},
		)
		var archived struct {
			Document bson.Raw `bson:"document"`
		}
		err := quarantine.FindOne(
			ctx,
			verificationFilter,
			options.FindOne().SetProjection(bson.D{{Key: "document", Value: 1}}),
		).Decode(&archived)
		if err != nil {
			return fmt.Errorf("verify executor duplicate archive %s: %w", expected.archiveID, err)
		}
		if !bytes.Equal(archived.Document, expected.snapshot.document) {
			return fmt.Errorf("verify executor duplicate archive %s: full BSON preimage mismatch (expected sha256 %s)", expected.archiveID, expected.documentDigest)
		}
	}
	return nil
}

func executorMergeStateFromWinner(raw bson.Raw) (executorMergeState, bson.M) {
	state := executorMergeState{}
	if value, ok := executorNonEmptyString(raw, "task_state"); ok && isValidExecutorTaskState(value) {
		state.hasTaskState = true
	}
	if _, ok := executorNonEmptyString(raw, "config"); ok {
		state.hasConfig = true
	}
	if _, ok := executorNonEmptyString(raw, "task_name"); ok {
		state.hasTaskName = true
	}

	normalization := bson.M{}
	present, priority, valid, canonical := executorPriorityValue(raw)
	if valid {
		state.hasPriority = true
		if !canonical {
			normalization["priority"] = priority
		}
	} else if present {
		state.invalidPriority = true
	}
	return state, normalization
}

func (state *executorMergeState) mergeCandidate(raw bson.Raw) bson.M {
	merge := bson.M{}
	if !state.hasTaskState {
		if value, ok := executorNonEmptyString(raw, "task_state"); ok && isValidExecutorTaskState(value) {
			merge["task_state"] = value
			state.hasTaskState = true
		}
	}
	if !state.hasConfig {
		if value, ok := executorNonEmptyString(raw, "config"); ok {
			merge["config"] = value
			state.hasConfig = true
		}
	}
	if !state.hasTaskName {
		if value, ok := executorNonEmptyString(raw, "task_name"); ok {
			merge["task_name"] = value
			state.hasTaskName = true
		}
	}
	if !state.hasPriority {
		_, priority, valid, _ := executorPriorityValue(raw)
		if valid {
			merge["priority"] = priority
			state.hasPriority = true
			state.invalidPriority = false
		}
	}
	return merge
}

func executorMergedKnownFields(snapshots []executorTaskSnapshot) (bson.M, bson.M) {
	state, set := executorMergeStateFromWinner(snapshots[0].document)
	for _, snapshot := range snapshots[1:] {
		for field, value := range state.mergeCandidate(snapshot.document) {
			set[field] = value
		}
	}

	// The aggregation rank is newest first. Preserve the first non-empty value
	// for each execution snapshot field without fabricating a conflict policy
	// for empty, malformed, or unknown values (all remain in quarantine).
	for _, field := range []string{"status", "worker", "result"} {
		if value, ok := executorFirstNonEmptyString(snapshots, field); ok {
			set[field] = value
		}
	}
	for _, choice := range []struct {
		field    string
		earliest bool
	}{
		{field: "create_time", earliest: true},
		{field: "start_time", earliest: true},
		{field: "update_time", earliest: false},
		{field: "end_time", earliest: false},
	} {
		if value, ok := executorExtremeDateTime(snapshots, choice.field, choice.earliest); ok {
			set[choice.field] = value
		}
	}

	unset := bson.M{}
	if state.invalidPriority && !state.hasPriority {
		unset["priority"] = ""
	}
	return set, unset
}

func executorFirstNonEmptyString(snapshots []executorTaskSnapshot, field string) (string, bool) {
	for _, snapshot := range snapshots {
		if value, ok := executorNonEmptyString(snapshot.document, field); ok {
			return value, true
		}
	}
	return "", false
}

func executorExtremeDateTime(
	snapshots []executorTaskSnapshot,
	field string,
	earliest bool,
) (primitive.DateTime, bool) {
	var selected primitive.DateTime
	found := false
	for _, snapshot := range snapshots {
		value, err := snapshot.document.LookupErr(field)
		if err != nil || value.Type != bsontype.DateTime {
			continue
		}
		candidate := primitive.DateTime(value.DateTime())
		if !found || (earliest && candidate < selected) || (!earliest && candidate > selected) {
			selected = candidate
			found = true
		}
	}
	return selected, found
}

func executorExactSnapshotFilter(snapshot executorTaskSnapshot) bson.D {
	return bson.D{
		{Key: "_id", Value: snapshot.row.ID},
		{Key: "$expr", Value: bson.D{{Key: "$eq", Value: bson.A{
			"$$ROOT",
			bson.D{{Key: "$literal", Value: snapshot.document}},
		}}}},
	}
}

func executorPhysicalSurvivorIndex(
	group *executorMergeGroup,
	snapshots []executorTaskSnapshot,
) int {
	for index, snapshot := range snapshots {
		mainValue, mainErr := snapshot.document.LookupErr("main_task_id")
		taskValue, taskErr := snapshot.document.LookupErr("task_id")
		if mainErr != nil || taskErr != nil {
			continue
		}
		mainID, mainOK := mainValue.StringValueOK()
		taskID, taskOK := taskValue.StringValueOK()
		if mainOK && taskOK && mainID == group.mainID && taskID == group.taskID {
			return index
		}
	}
	return 0
}

func executorReplacementFromLogicalWinner(
	logicalWinner executorTaskSnapshot,
	physicalSurvivor executorTaskSnapshot,
	set bson.M,
	unset bson.M,
) (bson.D, error) {
	var logicalDocument bson.D
	if err := bson.Unmarshal(logicalWinner.document, &logicalDocument); err != nil {
		return nil, fmt.Errorf("decode logical executor merge winner %v: %w", logicalWinner.row.ID, err)
	}

	replacedFields := make(map[string]struct{}, len(set)+len(unset)+1)
	replacedFields["_id"] = struct{}{}
	for field := range set {
		replacedFields[field] = struct{}{}
	}
	for field := range unset {
		replacedFields[field] = struct{}{}
	}

	// Start from the logical winner so all unknown, generation, and pause fields
	// follow the deterministic content winner. Only the physical survivor's _id
	// is retained, and every merged/canonical field is written exactly once.
	replacement := make(bson.D, 0, len(logicalDocument)+len(set))
	replacement = append(replacement, bson.E{Key: "_id", Value: physicalSurvivor.row.ID})
	for _, element := range logicalDocument {
		if _, replaced := replacedFields[element.Key]; replaced {
			continue
		}
		replacement = append(replacement, element)
	}
	setFields := make([]string, 0, len(set))
	for field := range set {
		setFields = append(setFields, field)
	}
	sort.Strings(setFields)
	for _, field := range setFields {
		replacement = append(replacement, bson.E{Key: field, Value: set[field]})
	}
	return replacement, nil
}

func (m *ExecutorTaskModel) finishExecutorMergeGroup(
	ctx context.Context,
	markers *mongo.Collection,
	lease executorTaskMigrationLease,
	group *executorMergeGroup,
) error {
	if len(group.rows) == 0 {
		return nil
	}
	if len(group.rows) == 1 && group.rows[0].MainIDType == "string" && group.rows[0].TaskIDType == "string" {
		// The overwhelmingly common singleton canonical row needs no point read.
		return nil
	}

	snapshots, err := m.loadExecutorMergeGroupSnapshots(ctx, group)
	if err != nil {
		return err
	}
	physicalSurvivorIndex := executorPhysicalSurvivorIndex(group, snapshots)
	if len(snapshots) > 1 {
		// Archive every member, including the logical winner and physical
		// survivor preimages, before any source document can be modified.
		if err := assertExecutorTaskMigrationLease(ctx, markers, lease); err != nil {
			return err
		}
		if err := m.quarantineExecutorDuplicateGroup(ctx, group, snapshots, physicalSurvivorIndex); err != nil {
			return err
		}
	}

	logicalWinner := snapshots[0]
	physicalSurvivor := snapshots[physicalSurvivorIndex]
	set, unset := executorMergedKnownFields(snapshots)
	set["main_task_id"] = group.mainID
	set["task_id"] = group.taskID

	if err := assertExecutorTaskMigrationLease(ctx, markers, lease); err != nil {
		return err
	}
	if physicalSurvivorIndex == 0 {
		update := bson.M{"$set": set}
		if len(unset) > 0 {
			update["$unset"] = unset
		}
		result, err := m.coll.UpdateOne(
			ctx,
			executorExactSnapshotFilter(logicalWinner),
			update,
			options.Update().SetBypassDocumentValidation(true),
		)
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("%w: canonicalize executor row %v: %v", errExecutorTaskMigrationRetry, logicalWinner.row.ID, err)
		}
		if err != nil {
			return fmt.Errorf("merge executor duplicate group into %v: %w", logicalWinner.row.ID, err)
		}
		if result.MatchedCount != 1 {
			return fmt.Errorf("%w: executor merge winner %v changed after quarantine", errExecutorTaskMigrationRetry, logicalWinner.row.ID)
		}
	} else {
		replacement, err := executorReplacementFromLogicalWinner(logicalWinner, physicalSurvivor, set, unset)
		if err != nil {
			return err
		}
		result, err := m.coll.ReplaceOne(
			ctx,
			executorExactSnapshotFilter(physicalSurvivor),
			replacement,
			options.Replace().SetBypassDocumentValidation(true),
		)
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("%w: replace canonical executor survivor %v: %v", errExecutorTaskMigrationRetry, physicalSurvivor.row.ID, err)
		}
		if err != nil {
			return fmt.Errorf("replace executor survivor %v from logical winner %v: %w", physicalSurvivor.row.ID, logicalWinner.row.ID, err)
		}
		if result.MatchedCount != 1 {
			return fmt.Errorf("%w: executor physical survivor %v changed after quarantine", errExecutorTaskMigrationRetry, physicalSurvivor.row.ID)
		}
	}

	for index, snapshot := range snapshots {
		if index == physicalSurvivorIndex {
			continue
		}
		if err := assertExecutorTaskMigrationLease(ctx, markers, lease); err != nil {
			return err
		}
		deleteResult, err := m.coll.DeleteOne(ctx, executorExactSnapshotFilter(snapshot))
		if err != nil {
			return fmt.Errorf("delete archived executor duplicate %v: %w", snapshot.row.ID, err)
		}
		if deleteResult.DeletedCount != 1 {
			return fmt.Errorf("%w: archived executor duplicate %v changed before delete", errExecutorTaskMigrationRetry, snapshot.row.ID)
		}
	}
	return nil
}

func executorNonEmptyString(raw bson.Raw, field string) (string, bool) {
	value, err := raw.LookupErr(field)
	if err != nil {
		return "", false
	}
	text, ok := value.StringValueOK()
	return text, ok && text != ""
}

func executorPriorityValue(raw bson.Raw) (present bool, priority int, valid bool, canonical bool) {
	value, err := raw.LookupErr("priority")
	if err != nil {
		return false, 0, false, false
	}
	present = true

	var numeric int64
	switch value.Type {
	case bsontype.Int32:
		numeric = int64(value.Int32())
		canonical = numeric >= executorTaskPriorityMin && numeric <= executorTaskPriorityMax
	case bsontype.Int64:
		numeric = value.Int64()
		canonical = numeric >= executorTaskPriorityMin && numeric <= executorTaskPriorityMax
	case bsontype.Double:
		doubleValue := value.Double()
		if math.IsNaN(doubleValue) || math.IsInf(doubleValue, 0) || math.Trunc(doubleValue) != doubleValue {
			return true, 0, false, false
		}
		if doubleValue < math.MinInt64 || doubleValue > math.MaxInt64 {
			return true, 0, false, false
		}
		numeric = int64(doubleValue)
		canonical = false
	default:
		return true, 0, false, false
	}

	if numeric < executorTaskPriorityMin || numeric > executorTaskPriorityMax {
		return true, 0, false, false
	}
	return true, int(numeric), true, canonical
}

func executorIdentifier(value interface{}) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, typed != ""
	case primitive.ObjectID:
		return typed.Hex(), !typed.IsZero()
	default:
		return "", false
	}
}

func (m *ExecutorTaskModel) executorTaskRowsAreCanonical(ctx context.Context) (bool, error) {
	noncanonical := bson.A{
		bson.D{{Key: "$ne", Value: bson.A{bson.D{{Key: "$type", Value: "$main_task_id"}}, "string"}}},
		bson.D{{Key: "$ne", Value: bson.A{bson.D{{Key: "$type", Value: "$task_id"}}, "string"}}},
		bson.D{{Key: "$eq", Value: bson.A{"$main_task_id", ""}}},
		bson.D{{Key: "$eq", Value: bson.A{"$task_id", ""}}},
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "$expr", Value: bson.D{{Key: "$or", Value: noncanonical}}},
		}}},
		bson.D{{Key: "$project", Value: bson.D{{Key: "_id", Value: 1}}}},
		bson.D{{Key: "$limit", Value: 1}},
	}
	cursor, err := m.coll.Aggregate(ctx, pipeline, options.Aggregate().SetBatchSize(1))
	if err != nil {
		return false, err
	}
	defer cursor.Close(ctx)
	if cursor.Next(ctx) {
		return false, nil
	}
	if err := cursor.Err(); err != nil {
		return false, err
	}
	return true, nil
}

type executorIndexSpec struct {
	Name       string
	Key        bson.D
	Exact      bool
	NaturalKey bool
}

func (m *ExecutorTaskModel) ensureVerifiedUniqueIndex(ctx context.Context) error {
	specs, err := m.executorIndexSpecs(ctx)
	if err != nil {
		return err
	}

	// Reconcile conflicting natural-key indexes even when another exact index
	// already protects the collection. Exact indexes are always preserved: a
	// healthy desired index must not be dropped just to canonicalize rows.
	for _, spec := range specs {
		if spec.Exact || (spec.Name != executorTaskUniqueIndexName && !spec.NaturalKey) {
			continue
		}
		if _, err := m.coll.Indexes().DropOne(ctx, spec.Name); err != nil {
			return fmt.Errorf("drop conflicting executor task index %s: %w", spec.Name, err)
		}
	}

	exact, _, err := m.executorTaskHasExactUniqueIndex(ctx)
	if err != nil {
		return err
	}
	if exact {
		return nil
	}

	_, err = m.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "main_task_id", Value: 1}, {Key: "task_id", Value: 1}},
		Options: options.Index().
			SetName(executorTaskUniqueIndexName).
			SetUnique(true),
	})
	if err != nil {
		exact, _, verifyErr := m.executorTaskHasExactUniqueIndex(ctx)
		if verifyErr == nil && exact {
			return nil
		}
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("%w: create executor task unique index: %v", errExecutorTaskMigrationRetry, err)
		}
		return err
	}

	exact, _, err = m.executorTaskHasExactUniqueIndex(ctx)
	if err != nil {
		return err
	}
	if !exact {
		return fmt.Errorf("exact executor task unique index was not created")
	}
	return nil
}

func (m *ExecutorTaskModel) executorTaskHasExactUniqueIndex(ctx context.Context) (bool, string, error) {
	specs, err := m.executorIndexSpecs(ctx)
	if err != nil {
		return false, "", err
	}
	for _, spec := range specs {
		if spec.Exact {
			return true, spec.Name, nil
		}
	}
	return false, "", nil
}

func (m *ExecutorTaskModel) executorIndexSpecs(ctx context.Context) ([]executorIndexSpec, error) {
	cursor, err := m.coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	specs := make([]executorIndexSpec, 0)
	for cursor.Next(ctx) {
		var raw bson.Raw
		if err := cursor.Decode(&raw); err != nil {
			return nil, err
		}
		nameValue, err := raw.LookupErr("name")
		if err != nil {
			return nil, fmt.Errorf("executor task index has no name")
		}
		name, ok := nameValue.StringValueOK()
		if !ok || name == "" {
			return nil, fmt.Errorf("executor task index has invalid name")
		}
		keyValue, err := raw.LookupErr("key")
		if err != nil {
			return nil, fmt.Errorf("executor task index %s has no key: %w", name, err)
		}
		keyDocument, ok := keyValue.DocumentOK()
		if !ok {
			return nil, fmt.Errorf("executor task index %s has invalid key", name)
		}
		var key bson.D
		if err := bson.Unmarshal(keyDocument, &key); err != nil {
			return nil, fmt.Errorf("decode executor task index %s key: %w", name, err)
		}

		naturalKey := executorNaturalKeyIndex(key)
		specs = append(specs, executorIndexSpec{
			Name:       name,
			Key:        key,
			NaturalKey: naturalKey,
			Exact:      naturalKey && executorIndexOptionsAreExact(raw),
		})
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return specs, nil
}

func executorIndexOptionsAreExact(raw bson.Raw) bool {
	unique, present, valid := executorIndexBooleanOption(raw, "unique")
	if !present || !valid || !unique {
		return false
	}
	if sparse, present, valid := executorIndexBooleanOption(raw, "sparse"); present && (!valid || sparse) {
		return false
	}
	if hidden, present, valid := executorIndexBooleanOption(raw, "hidden"); present && (!valid || hidden) {
		return false
	}
	for _, field := range []string{
		"partialFilterExpression",
		"collation",
		"expireAfterSeconds",
		"clustered",
	} {
		if _, err := raw.LookupErr(field); err == nil {
			return false
		}
	}
	return true
}

func executorIndexBooleanOption(raw bson.Raw, field string) (value bool, present bool, valid bool) {
	rawValue, err := raw.LookupErr(field)
	if err != nil {
		return false, false, true
	}
	value, valid = rawValue.BooleanOK()
	return value, true, valid
}

func executorNaturalKeyIndex(keys bson.D) bool {
	if len(keys) != 2 || keys[0].Key != "main_task_id" || keys[1].Key != "task_id" {
		return false
	}
	return executorAscending(keys[0].Value) && executorAscending(keys[1].Value)
}

func executorAscending(value interface{}) bool {
	switch typed := value.(type) {
	case int:
		return typed == 1
	case int32:
		return typed == 1
	case int64:
		return typed == 1
	case float64:
		return typed == 1
	default:
		return false
	}
}

// UpsertBatchDefinitions persists the exact scheduler identity/config for every
// batch without overwriting any pause snapshot already stored on the row.
func (m *ExecutorTaskModel) UpsertBatchDefinitions(ctx context.Context, definitions []ExecutorTask) error {
	if len(definitions) == 0 {
		return fmt.Errorf("executor batch definitions cannot be empty")
	}
	now := time.Now()
	writes := make([]mongo.WriteModel, 0, len(definitions))
	for _, definition := range definitions {
		if definition.MainTaskId == "" || definition.TaskId == "" || definition.Config == "" {
			return fmt.Errorf("executor batch definition requires main task id, task id, and config")
		}
		writes = append(writes, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"main_task_id": definition.MainTaskId, "task_id": definition.TaskId}).
			SetUpdate(bson.M{
				"$set": bson.M{
					"task_name":   definition.TaskName,
					"config":      definition.Config,
					"priority":    definition.Priority,
					"update_time": now,
				},
				"$setOnInsert": bson.M{
					"_id":          primitive.NewObjectID(),
					"main_task_id": definition.MainTaskId,
					"task_id":      definition.TaskId,
					"status":       TaskStatusPending,
					"create_time":  now,
				},
			}).
			SetUpsert(true))
	}
	_, err := m.coll.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(true))
	return err
}

// FindBatchDefinitions returns exact persisted batch payloads for a main task.
func (m *ExecutorTaskModel) FindBatchDefinitions(ctx context.Context, mainTaskID string) ([]ExecutorTask, error) {
	cursor, err := m.coll.Find(ctx, bson.M{
		"main_task_id": mainTaskID,
		"config":       bson.M{"$exists": true, "$ne": ""},
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

// UpdateStatusIfCurrent performs a compare-and-set task status transition.
func (m *MainTaskModel) UpdateStatusIfCurrent(ctx context.Context, id, current, next string) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}
	result, err := m.coll.UpdateOne(ctx,
		bson.M{"_id": oid, "status": current},
		bson.M{"$set": bson.M{"status": next, "update_time": time.Now()}},
	)
	if err != nil {
		return false, err
	}
	return result.MatchedCount == 1, nil
}
