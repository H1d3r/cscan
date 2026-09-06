package common

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/scheduler"
	"cscan/pkg/utils"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// TaskBuilder handles common task creation logic
type TaskBuilder struct {
	ctx      context.Context
	svcCtx   *svc.ServiceContext
	log      logx.Logger
	Priority int // 入队优先级（默认 PriorityLow=1）；自动触发任务注入 Background=0 以降低优先级
}

func NewTaskBuilder(ctx context.Context, svcCtx *svc.ServiceContext) *TaskBuilder {
	return &TaskBuilder{
		ctx:      ctx,
		svcCtx:   svcCtx,
		log:      logx.WithContext(ctx),
		Priority: scheduler.PriorityLow,
	}
}

// BuildAndPushSubTasks splits targets and pushes sub-tasks to Redis queue
func (b *TaskBuilder) BuildAndPushSubTasks(task *model.MainTask, taskConfig map[string]interface{}) (int, error) {
	// 1. Determine Batch Size — 自动计算最佳值
	batchSize := b.CalculateOptimalBatchSize(task.Target, taskConfig)
	b.log.Infof("TaskBuilder: auto-calculated batchSize=%d for task %s", batchSize, task.TaskId)

	// 2. Split Targets
	splitter := scheduler.NewTargetSplitter(batchSize)
	batches := splitter.SplitTargets(task.Target)

	// 注意：此处原先会异步 prewrite 初始资产——用 GenerateAssetsFromTargetsWithoutDNS 把用户输入的
	// 文本目标直接 upsert 进资产表（Source="user_input", IsNewAsset=true）。这在 worker 尚未进行任何
	// 存活/端口/指纹识别前就把目标录入暴露面，属逻辑错误，已移除。
	// 资产统一由 worker 扫描完成后通过直连 MongoDB 回写，
	// prewriteInitialAssets 等函数保留但不再调用，待后续清理。

	// 3. Calculate SubTask Count
	// subTaskCount = 批次数 × (启用模块数 + 1)
	// 与 worker 端单任务应发增量口径一致：worker 每完成一个扫描模块递增 1 次，
	// 加上最终"完成"阶段递增 1 次（见 worker.go expectedTaskIncr = CountEnabledModules + 1）。
	// 进度 = done / subTaskCount × 100。两侧口径必须保持一致，否则会出现 done > count 倒挂。
	// 无任何模块启用时，subTaskCount = 批次数（仅"完成"阶段递增）。
	enabledModules := utils.CountEnabledModules(taskConfig)
	subTaskCount := len(batches) * (enabledModules + 1)
	if enabledModules == 0 {
		subTaskCount = len(batches)
	}

	// 4. Prepare and persist the exact batch plan before queue publication.
	workers := b.extractWorkers(taskConfig)
	dispatchGeneration := uuid.NewString()
	schedTasks := make([]*scheduler.TaskInfo, 0, len(batches))
	definitions := make([]model.ExecutorTask, 0, len(batches))
	publicationTime := task.CreateTime
	if publicationTime.IsZero() && !task.Id.IsZero() {
		publicationTime = task.Id.Timestamp()
	}
	if publicationTime.IsZero() {
		publicationTime = time.Now()
	}
	stableCreateTime := publicationTime.Local().Format("2006-01-02 15:04:05")
	for i, batch := range batches {
		subConfig := make(map[string]interface{}, len(taskConfig)+3)
		for key, value := range taskConfig {
			subConfig[key] = value
		}
		subConfig["target"] = batch
		subConfig["subTaskIndex"] = i
		subConfig["subTaskTotal"] = len(batches)
		configBytes, err := json.Marshal(subConfig)
		if err != nil {
			return 0, fmt.Errorf("marshal sub-task %d config: %w", i, err)
		}
		subTaskID := task.TaskId
		if len(batches) > 1 {
			subTaskID = fmt.Sprintf("%s-%d", task.TaskId, i)
		}
		schedTask := &scheduler.TaskInfo{
			TaskId:             subTaskID,
			MainTaskId:         task.Id.Hex(),
			TaskName:           task.Name,
			Config:             string(configBytes),
			Priority:           b.Priority,
			CreateTime:         stableCreateTime,
			Workers:            workers,
			DispatchGeneration: dispatchGeneration,
		}
		schedTasks = append(schedTasks, schedTask)
		definitions = append(definitions, model.ExecutorTask{
			TaskId:             subTaskID,
			MainTaskId:         task.Id.Hex(),
			TaskName:           task.Name,
			Config:             string(configBytes),
			Priority:           b.Priority,
			DispatchGeneration: dispatchGeneration,
		})
	}
	if err := model.NewTaskDispatchManifestModel(b.svcCtx.MongoDB).Persist(
		b.ctx, task.Id.Hex(), dispatchGeneration, model.DispatchIntentInitial, publicationTime, definitions,
	); err != nil {
		return 0, fmt.Errorf("persist immutable batch plan: %w", err)
	}

	// The durable parent claim is the publication linearization point. Redis may
	// fail ambiguously afterwards; periodic reconciliation republishes only this
	// still-active PENDING generation from the executor manifest.
	now := time.Now()
	claimed, err := b.svcCtx.GetMainTaskModel().ClaimDispatch(
		b.ctx,
		task.Id.Hex(),
		task.Status,
		dispatchGeneration,
		model.DispatchIntentInitial,
		bson.M{
			"sub_task_count":       subTaskCount,
			"sub_task_done":        0,
			"batch_count":          len(batches),
			"start_time":           now,
			"dispatch_create_time": publicationTime,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("claim durable task dispatch: %w", err)
	}
	task.DispatchGeneration = claimed.DispatchGeneration
	task.DispatchIntent = claimed.DispatchIntent
	task.DispatchCreateTime = claimed.DispatchCreateTime
	task.Status = claimed.Status

	// Only the CAS winner may activate the shared executor snapshot rows. If
	// activation is interrupted, the PENDING reconciler repairs it from the
	// immutable generation manifest before republishing.
	if err := b.svcCtx.GetExecutorTaskModel().ActivateDispatchDefinitions(
		b.ctx, task.Id.Hex(), dispatchGeneration, publicationTime, definitions,
	); err != nil {
		return 0, fmt.Errorf("activate claimed batch dispatch: %w", err)
	}

	b.cacheTaskInfo(task, subTaskCount, len(batches), enabledModules)
	b.log.Infof("TaskBuilder: publishing %d batches for task %s generation %s", len(batches), task.TaskId, dispatchGeneration)
	markerKey := fmt.Sprintf("cscan:task:publish:%s:%s", task.Id.Hex(), dispatchGeneration)
	if err := b.svcCtx.Scheduler.PushTaskBatchOnce(b.ctx, schedTasks, markerKey); err != nil {
		return 0, fmt.Errorf("publish task batch: %w", err)
	}
	return len(batches), nil
}

func (b *TaskBuilder) pushSingleBatch(task *model.MainTask, baseConfig map[string]interface{}, batchTarget string, index, total int, workers []string) error {
	// Deep copy config
	subConfig := make(map[string]interface{})
	for k, v := range baseConfig {
		subConfig[k] = v
	}
	subConfig["target"] = batchTarget
	subConfig["subTaskIndex"] = index
	subConfig["subTaskTotal"] = total

	configBytes, err := json.Marshal(subConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal sub-task config: %w", err)
	}
	subTaskId := task.TaskId
	if total > 1 {
		subTaskId = fmt.Sprintf("%s-%d", task.TaskId, index)
	}

	schedTask := &scheduler.TaskInfo{
		TaskId:             subTaskId,
		MainTaskId:         task.Id.Hex(),
		TaskName:           task.Name,
		Config:             string(configBytes),
		Priority:           b.Priority,
		Workers:            workers,
		DispatchGeneration: task.DispatchGeneration,
	}

	return b.svcCtx.Scheduler.PushTask(b.ctx, schedTask)
}

func (b *TaskBuilder) cacheTaskInfo(task *model.MainTask, subTaskCount, batchCount, modules int) {
	key := fmt.Sprintf("cscan:task:info:%s", task.TaskId)
	data := map[string]interface{}{
		"mainTaskId":         task.Id.Hex(),
		"subTaskCount":       subTaskCount,
		"batchCount":         batchCount,
		"enabledModules":     modules,
		"dispatchGeneration": task.DispatchGeneration,
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

// CalculateOptimalBatchSize 根据目标数量和启用的模块自动计算最佳批次大小
// 默认不拆分：所有目标放入单个批次，避免多批次并发导致扫描进程数叠加
// （Worker 并发 × 扫描模块并发）。用户可通过 taskConfig["batchSize"] 显式启用拆分。
func (b *TaskBuilder) CalculateOptimalBatchSize(target string, taskConfig map[string]interface{}) int {
	// 用户显式设置 batchSize > 0 时优先使用（多 Worker 分布式场景）
	if bs, ok := taskConfig["batchSize"].(float64); ok && bs > 0 {
		return int(bs)
	}

	// 默认不拆分：所有目标放入单个批次
	// 子任务数 = 1 × (启用模块数 + 1)，Worker 同时仅执行 1 个子任务
	// 总并发进程 = 扫描模块并发 = 配置的 concurrency 值
	splitter := scheduler.NewTargetSplitter(1000000)
	targetCount := splitter.GetTargetCount(target)
	if targetCount < 1 {
		targetCount = 1
	}
	return targetCount
}
