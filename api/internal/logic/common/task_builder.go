package common

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cscan/api/internal/svc"
	"cscan/model"
	"cscan/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// TaskBuilder handles common task creation logic
type TaskBuilder struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	log    logx.Logger
}

func NewTaskBuilder(ctx context.Context, svcCtx *svc.ServiceContext) *TaskBuilder {
	return &TaskBuilder{
		ctx:    ctx,
		svcCtx: svcCtx,
		log:    logx.WithContext(ctx),
	}
}

// BuildAndPushSubTasks splits targets and pushes sub-tasks to Redis queue
func (b *TaskBuilder) BuildAndPushSubTasks(workspaceId string, task *model.MainTask, taskConfig map[string]interface{}) (int, error) {
	// 1. Determine Batch Size
	batchSize := 50
	if bs, ok := taskConfig["batchSize"].(float64); ok && bs > 0 {
		batchSize = int(bs)
	}

	// 2. Split Targets
	splitter := scheduler.NewTargetSplitter(batchSize)
	batches := splitter.SplitTargets(task.Target)

	// 3. Calculate SubTask Count
	enabledModules := b.countEnabledModules(taskConfig)
	subTaskCount := len(batches) * enabledModules

	// 4. Update Main Task Status
	now := time.Now()
	b.svcCtx.GetMainTaskModel(workspaceId).Update(b.ctx, task.Id.Hex(), bson.M{
		"status":         model.TaskStatusStarted,
		"sub_task_count": subTaskCount,
		"sub_task_done":  0,
		"start_time":     now,
	})

	// 5. Cache Info to Redis
	b.cacheTaskInfo(workspaceId, task, subTaskCount, len(batches), enabledModules)

	// 6. Push Sub-Tasks
	workers := b.extractWorkers(taskConfig)

	b.log.Infof("TaskBuilder: pushing %d batches for task %s", len(batches), task.TaskId)

	for i, batch := range batches {
		if err := b.pushSingleBatch(workspaceId, task, taskConfig, batch, i, len(batches), workers); err != nil {
			b.log.Errorf("Failed to push batch %d: %v", i, err)
			// Continue pushing other batches
		}
	}

	return len(batches), nil
}

func (b *TaskBuilder) pushSingleBatch(workspaceId string, task *model.MainTask, baseConfig map[string]interface{}, batchTarget string, index, total int, workers []string) error {
	// Deep copy config
	subConfig := make(map[string]interface{})
	for k, v := range baseConfig {
		subConfig[k] = v
	}
	subConfig["target"] = batchTarget
	subConfig["subTaskIndex"] = index
	subConfig["subTaskTotal"] = total

	configBytes, _ := json.Marshal(subConfig)
	subTaskId := task.TaskId
	if total > 1 {
		subTaskId = fmt.Sprintf("%s-%d", task.TaskId, index)
	}

	schedTask := &scheduler.TaskInfo{
		TaskId:      subTaskId,
		MainTaskId:  task.Id.Hex(),
		WorkspaceId: workspaceId,
		TaskName:    task.Name,
		Config:      string(configBytes),
		Priority:    1,
		Workers:     workers,
	}

	return b.svcCtx.Scheduler.PushTask(b.ctx, schedTask)
}

func (b *TaskBuilder) countEnabledModules(configMap map[string]interface{}) int {
	// Simplified parsing for counting
	// Since we are working with map[string]interface{}, we need to check keys safely
	count := 0

	// DomainScan
	if ds, ok := configMap["domainScan"].(map[string]interface{}); ok {
		if enable, ok := ds["enable"].(bool); ok && enable {
			count++
		}
	}

	// PortScan (default enabled if missing or nil)
	if ps, ok := configMap["portScan"].(map[string]interface{}); !ok || ps == nil {
		count++
	} else if enable, ok := ps["enable"].(bool); ok && enable {
		count++
	}

	// Other modules...
	modules := []string{"portIdentify", "fingerprint", "dirScan", "pocScan"}
	for _, mod := range modules {
		if m, ok := configMap[mod].(map[string]interface{}); ok {
			if enable, ok := m["enable"].(bool); ok && enable {
				count++
			}
		}
	}

	if count == 0 {
		return 1
	}
	return count
}

func (b *TaskBuilder) cacheTaskInfo(workspaceId string, task *model.MainTask, subTaskCount, batchCount, modules int) {
	key := fmt.Sprintf("cscan:task:info:%s", task.TaskId)
	data := map[string]interface{}{
		"workspaceId":    workspaceId,
		"mainTaskId":     task.Id.Hex(),
		"subTaskCount":   subTaskCount,
		"batchCount":     batchCount,
		"enabledModules": modules,
	}
	bytes, _ := json.Marshal(data)
	b.svcCtx.RedisClient.Set(b.ctx, key, bytes, 24*time.Hour)
}

func (b *TaskBuilder) extractWorkers(config map[string]interface{}) []string {
	var workers []string
	if w, ok := config["workers"].([]interface{}); ok {
		for _, v := range w {
			if s, ok := v.(string); ok {
				workers = append(workers, s)
			}
		}
	}
	return workers
}
