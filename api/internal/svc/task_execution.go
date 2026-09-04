package svc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"cscan/internal/model"
	"cscan/internal/notification"
	"cscan/internal/scheduler"
	"cscan/pkg/utils"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// atomicPopTaskScript 原子化弹出任务 Lua 脚本
var atomicPopTaskScript = redis.NewScript(`
	local queueKey = KEYS[1]
	local processingKey = KEYS[2]
	local taskInfoPrefix = ARGV[1]
	local execPrefix = ARGV[2]
	local workerName = ARGV[3]
	local ttlSeconds = tonumber(ARGV[4])
	local nowStr = ARGV[5]

	local result = redis.call('ZPOPMIN', queueKey, 1)
	if #result == 0 then
		return nil
	end
	local member = result[1]
	local score = result[2]

	local data = nil
	pcall(function() data = cjson.decode(member) end)
	if not data or not data.taskId or data.taskId == '' then
		redis.call('ZADD', 'cscan:task:deadletter', score, member)
		return '__DL__' .. member
	end
	local taskId = data.taskId

	redis.call('SADD', processingKey, taskId)

	local taskInfoKey = taskInfoPrefix .. taskId
	redis.call('SET', taskInfoKey, member, 'EX', ttlSeconds)

	local execKey = execPrefix .. taskId
	local execInfo = cjson.encode({
		taskId = taskId,
		workerName = workerName,
		startTime = nowStr,
		lastUpdate = nowStr,
		phase = "started",
		progress = 0,
		retryCount = 0,
		maxRetries = 3
	})
	redis.call('SET', execKey, execInfo, 'EX', 3600)

	return member
`)

// atomicPopFromBucketsScript 分桶路径原子化弹出任务 Lua 脚本
var atomicPopFromBucketsScript = redis.NewScript(`
	local workerQueueKey = KEYS[1]
	local bucketPrefix = KEYS[2]
	local processingKey = KEYS[3]
	local taskInfoPrefix = ARGV[1]
	local execPrefix = ARGV[2]
	local workerName = ARGV[3]
	local ttlSeconds = tonumber(ARGV[4])
	local nowStr = ARGV[5]

	local sourceKey = workerQueueKey
	local result = redis.call('ZPOPMIN', sourceKey, 1)

	if #result == 0 then
		for i = 4, 0, -1 do
			sourceKey = bucketPrefix .. ":p" .. i
			result = redis.call('ZPOPMIN', sourceKey, 1)
			if #result > 0 then
				break
			end
		end
	end

	if #result == 0 then
		return nil
	end

	local member = result[1]
	local score = result[2]

	local data = nil
	pcall(function() data = cjson.decode(member) end)
	if not data or not data.taskId or data.taskId == '' then
		redis.call('ZADD', 'cscan:task:deadletter', score, member)
		return '__DL__' .. member
	end
	local taskId = data.taskId

	redis.call('SADD', processingKey, taskId)

	local taskInfoKey = taskInfoPrefix .. taskId
	redis.call('SET', taskInfoKey, member, 'EX', ttlSeconds)

	local execKey = execPrefix .. taskId
	local execInfo = cjson.encode({
		taskId = taskId,
		workerName = workerName,
		startTime = nowStr,
		lastUpdate = nowStr,
		phase = "started",
		progress = 0,
		retryCount = 0,
		maxRetries = 3
	})
	redis.call('SET', execKey, execInfo, 'EX', 3600)

	return member
`)

// CheckTaskResult 任务拉取结果
type CheckTaskResult struct {
	IsExist    bool
	IsFinished bool
	TaskId     string
	MainTaskId string
	Config     string
}

// CheckTask 从 Redis 队列中获取待执行的任务
func (s *ServiceContext) CheckTask(ctx context.Context, workerName string) (*CheckTaskResult, error) {
	publicQueueKey := "cscan:task:queue"
	workerQueueKey := "cscan:task:queue:worker:" + strings.ToLower(workerName)
	processingKey := "cscan:task:processing"

	// 第一次尝试：立即弹出
	if result := s.tryPopTask(ctx, workerQueueKey, publicQueueKey, processingKey, workerName); result != nil {
		return result, nil
	}

	// 队列为空，进入长轮询等待
	return s.waitForTask(ctx, workerQueueKey, publicQueueKey, processingKey, workerName)
}

const longPollTimeout = 25 * time.Second

func (s *ServiceContext) tryPopTask(ctx context.Context, workerQueueKey, publicQueueKey, processingKey, workerName string) *CheckTaskResult {
	if s.Scheduler != nil && s.Scheduler.IsPriorityBucketEnabled() {
		result, err := s.popTaskFromBuckets(ctx, workerQueueKey, publicQueueKey, processingKey, workerName)
		if err != nil {
			logx.Errorf("[CheckTask] pop from buckets error: %v", err)
			return nil
		}
		if result != nil && result.IsExist {
			return result
		}
		return nil
	}

	result, err := s.popTaskFromQueue(ctx, workerQueueKey, processingKey, workerName)
	if err != nil {
		logx.Errorf("[CheckTask] pop from worker queue error: %v", err)
	}
	if result != nil {
		return result
	}

	result, err = s.popTaskFromQueue(ctx, publicQueueKey, processingKey, workerName)
	if err != nil {
		logx.Errorf("[CheckTask] pop from public queue error: %v", err)
	}
	return result
}

func (s *ServiceContext) waitForTask(ctx context.Context, workerQueueKey, publicQueueKey, processingKey, workerName string) (*CheckTaskResult, error) {
	pubsub := s.RedisClient.Subscribe(ctx, "cscan:task:available")
	defer pubsub.Close()

	if _, err := pubsub.Receive(ctx); err != nil {
		return &CheckTaskResult{}, nil
	}

	ch := pubsub.Channel()
	pollCtx, cancel := context.WithTimeout(ctx, longPollTimeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return &CheckTaskResult{}, nil
		case <-ch:
			if result := s.tryPopTask(ctx, workerQueueKey, publicQueueKey, processingKey, workerName); result != nil {
				return result, nil
			}
		case <-ticker.C:
			if result := s.tryPopTask(ctx, workerQueueKey, publicQueueKey, processingKey, workerName); result != nil {
				return result, nil
			}
		}
	}
}

func (s *ServiceContext) popTaskFromQueue(ctx context.Context, queueKey, processingKey, workerName string) (*CheckTaskResult, error) {
	result, err := atomicPopTaskScript.Run(ctx, s.RedisClient,
		[]string{queueKey, processingKey},
		"cscan:task:info:", "cscan:task:execution:",
		workerName, 86400, time.Now().Format(time.RFC3339),
	).Result()

	if err == redis.Nil || result == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, nil
	}

	if strings.HasPrefix(taskData, "__DL__") {
		s.RedisClient.Publish(ctx, "cscan:task:deadletter:alert", strings.TrimPrefix(taskData, "__DL__"))
		return nil, nil
	}

	var task scheduler.TaskInfo
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		return nil, err
	}

	s.updateMainTaskToStarted(ctx, task.MainTaskId)

	return &CheckTaskResult{
		IsExist:    true,
		IsFinished: false,
		TaskId:     task.TaskId,
		MainTaskId: task.MainTaskId,
		Config:     task.Config,
	}, nil
}

func (s *ServiceContext) popTaskFromBuckets(ctx context.Context, workerQueueKey, bucketPrefix, processingKey, workerName string) (*CheckTaskResult, error) {
	result, err := atomicPopFromBucketsScript.Run(ctx, s.RedisClient,
		[]string{workerQueueKey, bucketPrefix, processingKey},
		"cscan:task:info:", "cscan:task:execution:",
		workerName, 86400, time.Now().Format(time.RFC3339),
	).Result()

	if err == redis.Nil || result == nil {
		return &CheckTaskResult{}, nil
	}
	if err != nil {
		return nil, err
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, nil
	}

	if strings.HasPrefix(taskData, "__DL__") {
		s.RedisClient.Publish(ctx, "cscan:task:deadletter:alert", strings.TrimPrefix(taskData, "__DL__"))
		return nil, nil
	}

	var task scheduler.TaskInfo
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		return nil, err
	}

	s.updateMainTaskToStarted(ctx, task.MainTaskId)

	return &CheckTaskResult{
		IsExist:    true,
		IsFinished: false,
		TaskId:     task.TaskId,
		MainTaskId: task.MainTaskId,
		Config:     task.Config,
	}, nil
}

func (s *ServiceContext) updateMainTaskToStarted(ctx context.Context, mainTaskId string) {
	if mainTaskId == "" || !isValidObjectID(mainTaskId) {
		return
	}

	mongoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	taskModel := model.NewMainTaskModel(s.MongoDB)
	task, err := taskModel.FindById(mongoCtx, mainTaskId)
	if err != nil || task == nil {
		return
	}

	if task.Status == "PENDING" || task.Status == "CREATED" || task.Status == "" {
		taskModel.Update(mongoCtx, mainTaskId, bson.M{
			"status":     "STARTED",
			"start_time": time.Now(),
		})
		// 任务启动即把目标状态置为 in_progress（资产空间搜索实时反映扫描中）
		model.NewAssetTargetMetaModel(s.MongoDB).RegisterScanTargets(mongoCtx, utils.SplitTargetTokens(task.Target), "in_progress")
		// 状态流转立即失效列表缓存，前端 3s 轮询无需等 30s TTL 过期
		s.QueryCache.Clear()
	}
}

func isWorkerTerminalState(state string) bool {
	switch state {
	case model.TaskStatusSuccess, model.TaskStatusPartial, model.TaskStatusFailure, "COMPLETED":
		return true
	default:
		return false
	}
}

// UpdateTask 更新任务状态
func (s *ServiceContext) UpdateTask(ctx context.Context, taskId, state, worker, result, phase string) error {
	// 更新任务进度
	if phase != "" {
		// TaskRecoveryManager is optional; skip if not available
	}

	// 从处理中集合移除
	processingKey := "cscan:task:processing"
	if err := s.RedisClient.SRem(ctx, processingKey, taskId).Err(); err != nil {
		logx.Errorf("[UpdateTask] SRem processing failed, taskId=%s, error=%v", taskId, err)
	}

	// 读取 taskInfo（终态时删除前先读取）
	var taskInfoData string
	if isWorkerTerminalState(state) {
		taskInfoKey := "cscan:task:info:" + taskId
		if data, err := s.RedisClient.Get(ctx, taskInfoKey).Result(); err == nil {
			taskInfoData = data
		}
	}

	// 更新任务状态到 Redis
	statusKey := "cscan:task:status:" + taskId
	statusData := map[string]interface{}{
		"taskId": taskId,
		"state":  state,
		"worker": worker,
		"result": result,
		"phase":  phase,
	}
	statusJson, _ := json.Marshal(statusData)
	s.RedisClient.Set(ctx, statusKey, statusJson, 24*time.Hour)

	// 更新进度信息
	if phase != "" {
		progressKey := "cscan:task:progress:" + taskId
		progressData := map[string]interface{}{"currentPhase": phase}
		progressJson, _ := json.Marshal(progressData)
		s.RedisClient.Set(ctx, progressKey, progressJson, 24*time.Hour)
	}

	// 终态清理
	if isWorkerTerminalState(state) {
		taskInfoKey := "cscan:task:info:" + taskId
		s.RedisClient.Del(ctx, taskInfoKey)

		completedKey := "cscan:task:completed"
		taskJson, _ := json.Marshal(scheduler.TaskInfo{TaskId: taskId})
		s.RedisClient.SAdd(ctx, completedKey, string(taskJson))
	}

	// 更新数据库
	s.updateTaskInDB(ctx, taskId, state, result, phase, taskInfoData)

	return nil
}

func (s *ServiceContext) updateTaskInDB(ctx context.Context, taskId, state, result, phase, taskInfoData string) {
	if state == "" && phase == "" {
		return
	}

	var taskInfo map[string]interface{}
	if taskInfoData != "" {
		if err := json.Unmarshal([]byte(taskInfoData), &taskInfo); err != nil {
			return
		}
	} else {
		taskInfoKey := "cscan:task:info:" + taskId
		data, err := s.RedisClient.Get(ctx, taskInfoKey).Result()
		if err != nil {
			return
		}
		if err := json.Unmarshal([]byte(data), &taskInfo); err != nil {
			return
		}
	}

	mainTaskId, _ := taskInfo["mainTaskId"].(string)
	subTaskCount := 1
	if count, ok := taskInfo["subTaskCount"].(float64); ok {
		subTaskCount = int(count)
	}
	if mainTaskId == "" || !isValidObjectID(mainTaskId) {
		return
	}

	taskModel := model.NewMainTaskModel(s.MongoDB)
	now := time.Now()
	update := bson.M{}

	if state != "" {
		update["status"] = state
	}
	if phase != "" {
		update["current_phase"] = phase
	}

	switch state {
	case "STARTED":
		task, err := taskModel.FindById(ctx, mainTaskId)
		if err == nil && task != nil && task.Status == "STARTED" {
			if phase != "" {
				taskModel.Update(ctx, mainTaskId, bson.M{"current_phase": phase})
			}
			return
		}
		update["start_time"] = now
	case "SUCCESS", "COMPLETED":
		// Executor success is not evidence of complete scan coverage. Main-task
		// terminal state is owned exclusively by FinalizeFromScanSummary.
		return
	case model.TaskStatusPartial:
		// A PARTIAL terminal state must remain distinct from SUCCESS. Distributed
		// scan tasks are finalized from persisted phase summaries instead.
		if subTaskCount > 1 {
			return
		}
		update["end_time"] = now
		update["result"] = result
	case "FAILURE":
		if subTaskCount > 1 {
			return
		}
		update["end_time"] = now
		update["result"] = result
	case "STOPPED":
		update["end_time"] = now
		update["result"] = "任务已停止"
	}

	if len(update) > 0 {
		taskModel.Update(ctx, mainTaskId, update)
	}
}

func isValidObjectID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// IncrSubTaskDoneResult 子任务完成结果
type IncrSubTaskDoneResult struct {
	Success             bool
	Message             string
	SubTaskDone         int32
	SubTaskCount        int32
	AllDone             bool
	Recorded            bool
	Finalized           bool
	FinalizationPending bool
	ScanSummary         *model.TaskScanSummary
}

// normalizeWorkerPhaseSummary accepts both rollout payload shapes. The server
// persists a single phase report and always recomputes the final outcome; a
// worker-provided aggregate outcome is never trusted as an authoritative state.
func normalizeWorkerPhaseSummary(taskId, phase string, incrAmount int, phaseResult *model.TaskPhaseSummary, taskSummary *model.TaskScanSummary) model.TaskPhaseSummary {
	canonicalPhase := canonicalTaskPhaseName(phase)
	var normalized model.TaskPhaseSummary

	if phaseResult != nil && phaseResult.Status != "" {
		normalized = *phaseResult
		normalized.ReasonCodes = append([]string(nil), phaseResult.ReasonCodes...)
	} else if taskSummary != nil {
		reportKey := model.TaskPhaseReportKey(taskId, canonicalPhase)
		if candidate, ok := taskSummary.Phases[reportKey]; ok && candidate.Status != "" {
			normalized = candidate
		}
		if normalized.Status == "" {
			for _, candidate := range taskSummary.Phases {
				candidatePhase := canonicalTaskPhaseName(candidate.Phase)
				if candidate.Status != "" && candidatePhase == canonicalPhase && (candidate.SubTaskId == "" || candidate.SubTaskId == taskId) {
					normalized = candidate
					break
				}
			}
		}
		if normalized.Status != "" {
			if normalized.Assets == 0 {
				normalized.Assets = taskSummary.Assets
			}
			if normalized.Vulnerabilities == 0 {
				normalized.Vulnerabilities = taskSummary.Vulnerabilities
			}
			if normalized.VulnerabilityConclusion == "" {
				normalized.VulnerabilityConclusion = taskSummary.VulnerabilityConclusion
			}
			normalized.ReasonCodes = appendUniqueReasonCodes(normalized.ReasonCodes, taskSummary.WarningCodes...)
		}
	}

	if normalized.Status == "" {
		normalized = model.TaskPhaseSummary{
			Status:      "UNKNOWN",
			ReasonCodes: []string{"legacy_summary_missing"},
		}
	}
	normalized.SubTaskId = taskId
	if normalized.Phase == "" {
		normalized.Phase = canonicalPhase
	} else {
		normalized.Phase = canonicalTaskPhaseName(normalized.Phase)
	}
	normalized.Weight = incrAmount
	return normalized
}

func appendUniqueReasonCodes(existing []string, values ...string) []string {
	result := append([]string(nil), existing...)
	seen := make(map[string]struct{}, len(result))
	for _, value := range result {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// IncrSubTaskDone 递增子任务完成数
func (s *ServiceContext) IncrSubTaskDone(ctx context.Context, taskId, mainTaskId, phase string, incrAmount int, phaseResult *model.TaskPhaseSummary, taskSummary *model.TaskScanSummary) (*IncrSubTaskDoneResult, error) {
	if mainTaskId == "" {
		return &IncrSubTaskDoneResult{Success: false, Message: "mainTaskId is empty"}, nil
	}

	// 快速验证类任务跳过
	if !isValidObjectID(mainTaskId) {
		return &IncrSubTaskDoneResult{
			Success: true, Message: "ok (quick validation task)",
			SubTaskDone: 1, SubTaskCount: 1, AllDone: true, Recorded: true, Finalized: true,
		}, nil
	}

	mongoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	taskModel := model.NewMainTaskModel(s.MongoDB)

	if incrAmount <= 0 {
		incrAmount = 1
	}
	canonicalPhase := canonicalTaskPhaseName(phase)
	normalizedPhase := normalizeWorkerPhaseSummary(taskId, phase, incrAmount, phaseResult, taskSummary)
	reportKey := model.TaskPhaseReportKey(taskId, canonicalPhase)
	task, recorded, err := taskModel.RecordPhaseSummaryAtomic(mongoCtx, mainTaskId, reportKey, normalizedPhase, incrAmount)
	if err != nil {
		return &IncrSubTaskDoneResult{Success: false, Message: err.Error()}, nil
	}

	if !recorded {
		logx.Infof("[IncrSubTaskDone] duplicate phase report merged, mainTaskId=%s taskId=%s phase=%s",
			mainTaskId, taskId, canonicalPhase)
	}

	allDone := task.SubTaskDone >= task.SubTaskCount

	// 更新进度
	progress := 0
	if task.SubTaskCount > 0 {
		progress = task.SubTaskDone * 100 / task.SubTaskCount
		if progress > 100 {
			progress = 100
		}
	}
	taskModel.Update(mongoCtx, mainTaskId, bson.M{
		"progress":      progress,
		"current_phase": phase,
	})

	// Count completion only opens the finalization gate; summaries decide outcome.
	var finalSummary *model.TaskScanSummary
	finalized := false
	finalizationPending := false
	if allDone {
		summary, updated, finalizeErr := taskModel.FinalizeFromScanSummary(mongoCtx, mainTaskId)
		if finalizeErr != nil {
			finalizationPending = true
			logx.Errorf("[IncrSubTaskDone] semantic finalization failed; phase report remains acknowledged and finalization is retryable, mainTaskId=%s, error=%v", mainTaskId, finalizeErr)
		} else {
			finalSummary = summary
			terminal := model.IsTerminalTaskStatus(task.Status)
			finalized = updated || terminal
			if !updated && !terminal {
				finalizationPending = true
				logx.Infof("[IncrSubTaskDone] semantic finalization is still pending, mainTaskId=%s", mainTaskId)
			}
			if updated {
				logx.Infof("[IncrSubTaskDone] task finalized, mainTaskId=%s, outcome=%s", mainTaskId, summary.Outcome)
				if s.QueryCache != nil {
					s.QueryCache.Clear()
				}
				if s.RedisClient != nil {
					notifySvc := notification.NewService(s.MongoDB, s.RedisClient)
					if nerr := notifySvc.NotifyTaskCompleted(mongoCtx, mainTaskId, summary.Outcome); nerr != nil {
						logx.Errorf("[IncrSubTaskDone] notification failed, mainTaskId=%s, error=%v", mainTaskId, nerr)
					}
				}
			}
		}
	}

	return &IncrSubTaskDoneResult{
		Success: true, Message: "ok", SubTaskDone: int32(task.SubTaskDone), SubTaskCount: int32(task.SubTaskCount),
		AllDone: allDone, Recorded: recorded, Finalized: finalized, FinalizationPending: finalizationPending, ScanSummary: finalSummary,
	}, nil
}

func canonicalTaskPhaseName(phase string) string {
	switch phase {
	case "子域名扫描":
		return "domainscan"
	case "端口扫描":
		return "portscan"
	case "端口识别":
		return "portidentify"
	case "指纹识别":
		return "fingerprint"
	case "弱口令扫描":
		return "brutescan"
	case "目录扫描":
		return "dirscan"
	case "JS扫描":
		return "jsfinder"
	case "漏洞扫描":
		return "poc"
	case "完成":
		return "complete"
	default:
		if phase == "" {
			return "unknown"
		}
		return phase
	}
}
