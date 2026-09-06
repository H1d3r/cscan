package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/scheduler"

	"github.com/redis/go-redis/v9"
)

type taskBatchTopology struct {
	count     int
	childIDs  []string
	indexByID map[string]int
}

func prepareResumeTaskPlan(ctx context.Context, svcCtx *svc.ServiceContext, task *model.MainTask) ([]*scheduler.TaskInfo, []string, error) {
	executorModel := svcCtx.GetExecutorTaskModel()
	states, err := executorModel.FindTaskStatesByMainTaskId(ctx, task.Id.Hex())
	if err != nil {
		return nil, nil, fmt.Errorf("load sub-task snapshots: %w", err)
	}
	definitions, err := executorModel.FindBatchDefinitions(ctx, task.Id.Hex())
	if err != nil {
		return nil, nil, fmt.Errorf("load batch plan: %w", err)
	}

	topology, err := resolveTaskBatchTopology(task, definitions)
	if err != nil {
		return nil, nil, err
	}
	subTaskIDs := topology.childIDs

	baseTasks, complete := planFromDefinitions(task, topology, definitions)
	if !complete {
		baseTasks, complete, err = planFromTaskInfoCache(ctx, svcCtx, task, topology)
		if err != nil {
			return nil, nil, err
		}
	}
	if !complete {
		return nil, nil, fmt.Errorf("original %d-batch target plan is unavailable", topology.count)
	}

	if topology.count == 1 && states[task.TaskId] == "" && task.TaskState != "" {
		states[task.TaskId] = task.TaskState
	}
	prepared := make([]*scheduler.TaskInfo, 0, len(baseTasks))
	for i, baseTask := range baseTasks {
		if baseTask == nil || i >= len(subTaskIDs) || baseTask.TaskId != subTaskIDs[i] || baseTask.MainTaskId != task.Id.Hex() {
			return nil, nil, fmt.Errorf("batch plan identity mismatch at index %d", i)
		}
		preparedTask, err := injectResumeState(baseTask, states[baseTask.TaskId])
		if err != nil {
			return nil, nil, fmt.Errorf("prepare sub-task %s: %w", baseTask.TaskId, err)
		}
		prepared = append(prepared, preparedTask)
	}
	if len(prepared) != topology.count {
		return nil, nil, fmt.Errorf("batch plan has %d entries, expected %d", len(prepared), topology.count)
	}
	return prepared, append([]string(nil), subTaskIDs...), nil
}

// resolveTaskBatchTopology treats a persisted positive BatchCount as
// authoritative. Historical tasks without it must prove their topology from a
// complete durable executor-task definition manifest; cache or snapshot
// absence is never evidence of a singleton.
func resolveTaskBatchTopology(task *model.MainTask, definitions []model.ExecutorTask) (*taskBatchTopology, error) {
	if task == nil {
		return nil, fmt.Errorf("task batch topology requires a main task")
	}
	if task.BatchCount > 0 {
		return newTaskBatchTopology(task.TaskId, task.BatchCount)
	}

	topology, _, err := exactDefinitionManifest(task, definitions)
	if err != nil {
		return nil, fmt.Errorf("historical batch topology is unavailable: %w", err)
	}
	return topology, nil
}

func newTaskBatchTopology(parentID string, count int) (*taskBatchTopology, error) {
	if strings.TrimSpace(parentID) == "" {
		return nil, fmt.Errorf("main task scheduler identity is empty")
	}
	if count <= 0 {
		return nil, fmt.Errorf("batch count must be positive")
	}
	ids := expectedSubTaskIDs(parentID, count)
	indexByID := make(map[string]int, len(ids))
	for index, id := range ids {
		indexByID[id] = index
	}
	return &taskBatchTopology{
		count:     count,
		childIDs:  ids,
		indexByID: indexByID,
	}, nil
}

func expectedSubTaskIDs(parentID string, count int) []string {
	if count <= 0 || strings.TrimSpace(parentID) == "" {
		return nil
	}
	if count == 1 {
		return []string{parentID}
	}
	ids := make([]string, count)
	for i := 0; i < count; i++ {
		ids[i] = fmt.Sprintf("%s-%d", parentID, i)
	}
	return ids
}

func exactDefinitionManifest(task *model.MainTask, definitions []model.ExecutorTask) (*taskBatchTopology, []model.ExecutorTask, error) {
	if task == nil {
		return nil, nil, fmt.Errorf("executor-task definition manifest requires a main task")
	}
	if len(definitions) == 0 {
		return nil, nil, fmt.Errorf("executor-task definition manifest is empty")
	}

	mainTaskID := task.Id.Hex()
	commonTotal := 0
	byIndex := make(map[int]model.ExecutorTask, len(definitions))
	seenTaskIDs := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.MainTaskId != mainTaskID {
			return nil, nil, fmt.Errorf("definition %q has main task identity %q, expected %q", definition.TaskId, definition.MainTaskId, mainTaskID)
		}
		index, total, err := decodeBatchConfigTopology(definition.Config)
		if err != nil {
			return nil, nil, fmt.Errorf("definition %q config: %w", definition.TaskId, err)
		}
		if commonTotal == 0 {
			commonTotal = total
		} else if total != commonTotal {
			return nil, nil, fmt.Errorf("definition %q has subTaskTotal %d, expected %d", definition.TaskId, total, commonTotal)
		}

		expectedID := task.TaskId
		if total > 1 {
			expectedID = fmt.Sprintf("%s-%d", task.TaskId, index)
		}
		if definition.TaskId != expectedID {
			return nil, nil, fmt.Errorf("definition task identity %q is not canonical %q", definition.TaskId, expectedID)
		}
		if _, duplicate := seenTaskIDs[definition.TaskId]; duplicate {
			return nil, nil, fmt.Errorf("definition task identity %q is duplicated", definition.TaskId)
		}
		if _, duplicate := byIndex[index]; duplicate {
			return nil, nil, fmt.Errorf("subTaskIndex %d is duplicated", index)
		}
		seenTaskIDs[definition.TaskId] = struct{}{}
		byIndex[index] = definition
	}

	if commonTotal <= 0 {
		return nil, nil, fmt.Errorf("executor-task definition manifest has no positive subTaskTotal")
	}
	if len(definitions) != commonTotal {
		return nil, nil, fmt.Errorf("executor-task definition manifest has %d entries, expected %d", len(definitions), commonTotal)
	}

	topology, err := newTaskBatchTopology(task.TaskId, commonTotal)
	if err != nil {
		return nil, nil, err
	}
	ordered := make([]model.ExecutorTask, commonTotal)
	for index, id := range topology.childIDs {
		definition, ok := byIndex[index]
		if !ok {
			return nil, nil, fmt.Errorf("executor-task definition manifest is missing subTaskIndex %d", index)
		}
		if definition.TaskId != id {
			return nil, nil, fmt.Errorf("definition at subTaskIndex %d has task identity %q, expected %q", index, definition.TaskId, id)
		}
		ordered[index] = definition
	}
	return topology, ordered, nil
}

func decodeBatchConfigTopology(config string) (int, int, error) {
	if strings.TrimSpace(config) == "" {
		return 0, 0, fmt.Errorf("config is empty")
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(config), &values); err != nil {
		return 0, 0, fmt.Errorf("config is invalid JSON: %w", err)
	}
	if values == nil {
		return 0, 0, fmt.Errorf("config must be a JSON object")
	}

	indexValue, ok := values["subTaskIndex"]
	if !ok {
		return 0, 0, fmt.Errorf("subTaskIndex is missing")
	}
	totalValue, ok := values["subTaskTotal"]
	if !ok {
		return 0, 0, fmt.Errorf("subTaskTotal is missing")
	}
	var index int
	if err := json.Unmarshal(indexValue, &index); err != nil {
		return 0, 0, fmt.Errorf("subTaskIndex must be an integer: %w", err)
	}
	var total int
	if err := json.Unmarshal(totalValue, &total); err != nil {
		return 0, 0, fmt.Errorf("subTaskTotal must be an integer: %w", err)
	}
	if total <= 0 {
		return 0, 0, fmt.Errorf("subTaskTotal must be positive")
	}
	if index < 0 || index >= total {
		return 0, 0, fmt.Errorf("subTaskIndex %d is outside [0,%d)", index, total)
	}
	return index, total, nil
}

func planFromDefinitions(task *model.MainTask, topology *taskBatchTopology, definitions []model.ExecutorTask) ([]*scheduler.TaskInfo, bool) {
	manifestTopology, ordered, err := exactDefinitionManifest(task, definitions)
	if err != nil || !sameTaskBatchTopology(topology, manifestTopology) {
		return nil, false
	}

	plan := make([]*scheduler.TaskInfo, 0, topology.count)
	for index, definition := range ordered {
		plan = append(plan, &scheduler.TaskInfo{
			TaskId:             topology.childIDs[index],
			MainTaskId:         task.Id.Hex(),
			TaskName:           firstNonEmpty(definition.TaskName, task.Name),
			Config:             definition.Config,
			Priority:           definition.Priority,
			Workers:            workersFromConfig(definition.Config),
			DispatchGeneration: definition.DispatchGeneration,
		})
	}
	return plan, true
}

func planFromTaskInfoCache(ctx context.Context, svcCtx *svc.ServiceContext, task *model.MainTask, topology *taskBatchTopology) ([]*scheduler.TaskInfo, bool, error) {
	plan := make([]*scheduler.TaskInfo, 0, topology.count)
	for index, id := range topology.childIDs {
		data, err := svcCtx.RedisClient.Get(ctx, "cscan:task:info:"+id).Result()
		if err == redis.Nil {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("load cached sub-task %s: %w", id, err)
		}
		var cached scheduler.TaskInfo
		if err := json.Unmarshal([]byte(data), &cached); err != nil {
			return nil, false, nil
		}
		if cached.TaskId != id || cached.MainTaskId != task.Id.Hex() {
			return nil, false, nil
		}
		configIndex, configTotal, err := decodeBatchConfigTopology(cached.Config)
		if err != nil || configIndex != index || configTotal != topology.count {
			return nil, false, nil
		}
		plan = append(plan, &cached)
	}
	return plan, true, nil
}

func sameTaskBatchTopology(left, right *taskBatchTopology) bool {
	if left == nil || right == nil || left.count != right.count || len(left.childIDs) != len(right.childIDs) {
		return false
	}
	for index := range left.childIDs {
		if left.childIDs[index] != right.childIDs[index] {
			return false
		}
	}
	return true
}

func injectResumeState(base *scheduler.TaskInfo, state string) (*scheduler.TaskInfo, error) {
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(base.Config), &config); err != nil {
		return nil, fmt.Errorf("parse batch config: %w", err)
	}
	if config == nil {
		return nil, fmt.Errorf("batch config must be a JSON object")
	}
	delete(config, "resumeState")
	if state != "" {
		if !json.Valid([]byte(state)) {
			return nil, fmt.Errorf("stored resume state is invalid JSON")
		}
		config["resumeState"] = state
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	prepared := *base
	prepared.Config = string(data)
	if len(prepared.Workers) == 0 {
		prepared.Workers = workersFromMap(config)
	}
	return &prepared, nil
}

func workersFromConfig(config string) []string {
	var values map[string]interface{}
	if json.Unmarshal([]byte(config), &values) != nil {
		return nil
	}
	return workersFromMap(values)
}

func workersFromMap(config map[string]interface{}) []string {
	values, ok := config["workers"].([]interface{})
	if !ok {
		return nil
	}
	workers := make([]string, 0, len(values))
	for _, value := range values {
		if worker, ok := value.(string); ok && worker != "" {
			workers = append(workers, worker)
		}
	}
	return workers
}

func cloneConfigMap(source map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
