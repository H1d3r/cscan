package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const taskDispatchManifestCollection = "task_dispatch_manifest"

// TaskDispatchManifest is an immutable, generation-keyed publication plan.
// Contending starts may persist separate generations, but only the generation
// selected by MainTask.ClaimDispatch is ever activated or published.
type TaskDispatchManifest struct {
	ID                 string         `bson:"_id" json:"id"`
	MainTaskID         string         `bson:"main_task_id" json:"mainTaskId"`
	DispatchGeneration string         `bson:"dispatch_generation" json:"dispatchGeneration"`
	DispatchIntent     string         `bson:"dispatch_intent" json:"dispatchIntent"`
	DispatchCreateTime time.Time      `bson:"dispatch_create_time" json:"dispatchCreateTime"`
	Definitions        []ExecutorTask `bson:"definitions" json:"definitions"`
	ManifestHash       string         `bson:"manifest_hash" json:"manifestHash"`
	CreateTime         time.Time      `bson:"create_time" json:"createTime"`
}

type TaskDispatchManifestModel struct {
	coll *mongo.Collection
}

func NewTaskDispatchManifestModel(db *mongo.Database) *TaskDispatchManifestModel {
	return &TaskDispatchManifestModel{coll: db.Collection(taskDispatchManifestCollection)}
}

type dispatchManifestHashDefinition struct {
	TaskID   string `json:"taskId"`
	TaskName string `json:"taskName"`
	Config   string `json:"config"`
	Priority int    `json:"priority"`
}

type dispatchManifestHashPayload struct {
	MainTaskID         string                           `json:"mainTaskId"`
	DispatchGeneration string                           `json:"dispatchGeneration"`
	DispatchIntent     string                           `json:"dispatchIntent"`
	DispatchCreateMS   int64                            `json:"dispatchCreateMs"`
	Definitions        []dispatchManifestHashDefinition `json:"definitions"`
}

func normalizeDispatchManifest(
	mainTaskID, generation, intent string,
	createTime time.Time,
	definitions []ExecutorTask,
) ([]ExecutorTask, string, error) {
	mainTaskID = strings.TrimSpace(mainTaskID)
	generation = strings.TrimSpace(generation)
	if mainTaskID == "" || generation == "" || createTime.IsZero() ||
		(intent != DispatchIntentInitial && intent != DispatchIntentResume) || len(definitions) == 0 {
		return nil, "", fmt.Errorf("dispatch manifest identity, intent, create time, and definitions are required")
	}

	normalized := make([]ExecutorTask, len(definitions))
	copy(normalized, definitions)
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].TaskId < normalized[j].TaskId })
	hashDefinitions := make([]dispatchManifestHashDefinition, 0, len(normalized))
	seen := make(map[string]struct{}, len(normalized))
	for index := range normalized {
		definition := &normalized[index]
		if strings.TrimSpace(definition.TaskId) == "" || definition.MainTaskId != mainTaskID ||
			strings.TrimSpace(definition.Config) == "" {
			return nil, "", fmt.Errorf("dispatch definition %d has incomplete identity or config", index)
		}
		if _, duplicate := seen[definition.TaskId]; duplicate {
			return nil, "", fmt.Errorf("dispatch definition %s is duplicated", definition.TaskId)
		}
		seen[definition.TaskId] = struct{}{}
		definition.DispatchGeneration = generation
		definition.Status = ""
		definition.Worker = ""
		definition.Result = ""
		definition.TaskState = ""
		definition.PauseLeaseGeneration = ""
		definition.PauseWorker = ""
		definition.PauseInstanceID = ""
		definition.PauseTaskProtocol = 0
		definition.PausePhase = ""
		definition.PauseCommitTime = nil
		definition.PauseDispatchGeneration = ""
		definition.CreateTime = time.Time{}
		definition.UpdateTime = time.Time{}
		definition.StartTime = nil
		definition.EndTime = nil
		hashDefinitions = append(hashDefinitions, dispatchManifestHashDefinition{
			TaskID: definition.TaskId, TaskName: definition.TaskName,
			Config: definition.Config, Priority: definition.Priority,
		})
	}
	payload, err := json.Marshal(dispatchManifestHashPayload{
		MainTaskID: mainTaskID, DispatchGeneration: generation, DispatchIntent: intent,
		DispatchCreateMS: createTime.UTC().UnixMilli(), Definitions: hashDefinitions,
	})
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(payload)
	return normalized, hex.EncodeToString(digest[:]), nil
}

// Persist stores one immutable generation manifest. A retry of the same
// generation is idempotent only when the complete manifest hash matches.
func (m *TaskDispatchManifestModel) Persist(
	ctx context.Context,
	mainTaskID, generation, intent string,
	createTime time.Time,
	definitions []ExecutorTask,
) error {
	mainTaskID = strings.TrimSpace(mainTaskID)
	generation = strings.TrimSpace(generation)
	normalized, manifestHash, err := normalizeDispatchManifest(
		mainTaskID, generation, intent, createTime, definitions,
	)
	if err != nil {
		return err
	}
	document := TaskDispatchManifest{
		ID:                 mainTaskID + ":" + generation,
		MainTaskID:         mainTaskID,
		DispatchGeneration: generation,
		DispatchIntent:     intent,
		DispatchCreateTime: createTime.UTC(),
		Definitions:        normalized,
		ManifestHash:       manifestHash,
		CreateTime:         time.Now().UTC(),
	}
	_, err = m.coll.InsertOne(ctx, document)
	if err == nil {
		return nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	existing, findErr := m.Find(ctx, mainTaskID, generation)
	if findErr != nil {
		return findErr
	}
	if existing == nil || existing.ManifestHash != manifestHash {
		return fmt.Errorf("dispatch manifest generation already exists with different content")
	}
	return nil
}

func (m *TaskDispatchManifestModel) Find(ctx context.Context, mainTaskID, generation string) (*TaskDispatchManifest, error) {
	mainTaskID = strings.TrimSpace(mainTaskID)
	generation = strings.TrimSpace(generation)
	if mainTaskID == "" || generation == "" {
		return nil, fmt.Errorf("dispatch manifest lookup requires parent and generation")
	}
	manifestID := mainTaskID + ":" + generation
	var manifest TaskDispatchManifest
	err := m.coll.FindOne(ctx, bson.M{"_id": manifestID}).Decode(&manifest)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if manifest.ID != manifestID || manifest.MainTaskID != mainTaskID || manifest.DispatchGeneration != generation {
		return nil, fmt.Errorf("stored dispatch manifest identity mismatch")
	}
	normalized, manifestHash, err := normalizeDispatchManifest(
		manifest.MainTaskID, manifest.DispatchGeneration, manifest.DispatchIntent,
		manifest.DispatchCreateTime, manifest.Definitions,
	)
	if err != nil {
		return nil, fmt.Errorf("stored dispatch manifest is invalid: %w", err)
	}
	if manifest.ManifestHash == "" || manifest.ManifestHash != manifestHash {
		return nil, fmt.Errorf("stored dispatch manifest hash mismatch")
	}
	manifest.Definitions = normalized
	return &manifest, nil
}
