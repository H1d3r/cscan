package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

// ChunkManager 分片管理器
type ChunkManager struct {
	rdb      *redis.Client
	splitter *TaskSplitter
}

// NewChunkManager 创建分片管理器
func NewChunkManager(rdb *redis.Client, config *ChunkConfig) *ChunkManager {
	return &ChunkManager{
		rdb:      rdb,
		splitter: NewTaskSplitter(config),
	}
}

// ChunkTaskRequest 分片任务请求
type ChunkTaskRequest struct {
	TaskId      string                 `json:"taskId"`      // 主任务ID
	TaskName    string                 `json:"taskName"`    // 任务名称
	Target      string                 `json:"target"`      // 目标列表
	Config      map[string]interface{} `json:"config"`      // 任务配置
	WorkspaceId string                 `json:"workspaceId"` // 工作空间ID
	MainTaskId  string                 `json:"mainTaskId"`  // 主任务文档ID
	Priority    int                    `json:"priority"`    // 优先级
	Workers     []string               `json:"workers"`     // 指定Worker列表
}

// ChunkTaskResponse 分片任务响应
type ChunkTaskResponse struct {
	Success      bool        `json:"success"`      // 是否成功
	Message      string      `json:"message"`      // 消息
	ChunkCount   int         `json:"chunkCount"`   // 分片数量
	TotalTargets int         `json:"totalTargets"` // 总目标数
	ChunkIds     []string    `json:"chunkIds"`     // 分片ID列表
	SplitResult  SplitResult `json:"splitResult"`  // 拆分结果详情
}

// CreateChunkedTask 创建分片任务
func (cm *ChunkManager) CreateChunkedTask(ctx context.Context, req *ChunkTaskRequest) (*ChunkTaskResponse, error) {
	logx.Infof("[ChunkManager] Creating chunked task: taskId=%s, targets=%d chars",
		req.TaskId, len(req.Target))

	// 执行任务拆分
	splitResult, err := cm.splitter.SplitTask(req.TaskId, req.Target, req.Config)
	if err != nil {
		return &ChunkTaskResponse{
			Success: false,
			Message: fmt.Sprintf("任务拆分失败: %v", err),
		}, err
	}

	logx.Infof("[ChunkManager] Task split result: taskId=%s, totalTargets=%d, chunkCount=%d, needSplit=%v",
		req.TaskId, splitResult.TotalTargets, splitResult.ChunkCount, splitResult.NeedSplit)

	// 保存分片信息到Redis
	if err := cm.saveChunkInfo(ctx, req.TaskId, splitResult); err != nil {
		logx.Errorf("[ChunkManager] Failed to save chunk info: %v", err)
		return &ChunkTaskResponse{
			Success: false,
			Message: fmt.Sprintf("保存分片信息失败: %v", err),
		}, err
	}

	// 创建调度任务
	var chunkIds []string
	var schedTasks []*TaskInfo

	for _, chunk := range splitResult.Chunks {
		// 创建分片配置
		chunkConfig := make(map[string]interface{})
		for k, v := range req.Config {
			chunkConfig[k] = v
		}

		// 设置分片特定配置
		chunkConfig["target"] = strings.Join(chunk.Targets, "\n")
		chunkConfig["chunkIndex"] = chunk.Index
		chunkConfig["chunkTotal"] = splitResult.ChunkCount
		chunkConfig["chunkId"] = chunk.ChunkId
		chunkConfig["parentTaskId"] = req.TaskId

		chunkConfigBytes, _ := json.Marshal(chunkConfig)

		// 创建调度任务
		schedTask := &TaskInfo{
			TaskId:      chunk.ChunkId,
			MainTaskId:  req.MainTaskId,
			WorkspaceId: req.WorkspaceId,
			TaskName:    req.TaskName,
			Config:      string(chunkConfigBytes),
			Priority:    chunk.Priority,
			Workers:     req.Workers,
		}

		schedTasks = append(schedTasks, schedTask)
		chunkIds = append(chunkIds, chunk.ChunkId)

		// 保存分片任务信息到Redis
		if err := cm.saveChunkTaskInfo(ctx, chunk.ChunkId, req, chunk); err != nil {
			logx.Errorf("[ChunkManager] Failed to save chunk task info for %s: %v", chunk.ChunkId, err)
		}
	}

	logx.Infof("[ChunkManager] Created %d chunk tasks for taskId=%s", len(schedTasks), req.TaskId)

	return &ChunkTaskResponse{
		Success:      true,
		Message:      "分片任务创建成功",
		ChunkCount:   splitResult.ChunkCount,
		TotalTargets: splitResult.TotalTargets,
		ChunkIds:     chunkIds,
		SplitResult:  *splitResult,
	}, nil
}

// PushChunkedTasks 推送分片任务到调度队列
func (cm *ChunkManager) PushChunkedTasks(ctx context.Context, scheduler *Scheduler, req *ChunkTaskRequest) (*ChunkTaskResponse, error) {
	// 创建分片任务
	response, err := cm.CreateChunkedTask(ctx, req)
	if err != nil {
		return response, err
	}

	if !response.Success {
		return response, fmt.Errorf("%s", response.Message)
	}

	// 获取分片任务列表
	schedTasks, err := cm.getSchedulerTasks(ctx, req.TaskId)
	if err != nil {
		return &ChunkTaskResponse{
			Success: false,
			Message: fmt.Sprintf("获取分片任务失败: %v", err),
		}, err
	}

	// 批量推送到调度队列
	if err := scheduler.PushTaskBatch(ctx, schedTasks); err != nil {
		logx.Errorf("[ChunkManager] Failed to push chunk tasks to queue: %v", err)
		return &ChunkTaskResponse{
			Success: false,
			Message: fmt.Sprintf("推送任务到队列失败: %v", err),
		}, err
	}

	logx.Infof("[ChunkManager] Successfully pushed %d chunk tasks to queue for taskId=%s",
		len(schedTasks), req.TaskId)

	response.Message = fmt.Sprintf("成功推送 %d 个分片任务到队列", len(schedTasks))
	return response, nil
}

// GetChunkInfo 获取分片信息
func (cm *ChunkManager) GetChunkInfo(ctx context.Context, taskId string) (*SplitResult, error) {
	key := cm.getChunkInfoKey(taskId)
	data, err := cm.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("分片信息不存在")
		}
		return nil, err
	}

	var splitResult SplitResult
	if err := json.Unmarshal([]byte(data), &splitResult); err != nil {
		return nil, fmt.Errorf("解析分片信息失败: %v", err)
	}

	return &splitResult, nil
}

// GetChunkProgress 获取分片进度
func (cm *ChunkManager) GetChunkProgress(ctx context.Context, taskId string) (*ChunkProgress, error) {
	splitResult, err := cm.GetChunkInfo(ctx, taskId)
	if err != nil {
		return nil, err
	}

	progress := &ChunkProgress{
		TaskId:       taskId,
		TotalChunks:  splitResult.ChunkCount,
		TotalTargets: splitResult.TotalTargets,
		Chunks:       make([]ChunkStatus, 0, splitResult.ChunkCount),
	}

	// 获取每个分片的状态
	for _, chunk := range splitResult.Chunks {
		status, err := cm.getChunkStatus(ctx, chunk.ChunkId)
		if err != nil {
			logx.Errorf("[ChunkManager] Failed to get chunk status for %s: %v", chunk.ChunkId, err)
			status = &ChunkStatus{
				ChunkId: chunk.ChunkId,
				Status:  "UNKNOWN",
			}
		}
		progress.Chunks = append(progress.Chunks, *status)

		// 统计进度
		switch status.Status {
		case "SUCCESS":
			progress.CompletedChunks++
		case "FAILURE":
			progress.FailedChunks++
		case "STARTED":
			progress.RunningChunks++
		}
	}

	// 计算完成百分比
	if progress.TotalChunks > 0 {
		progress.CompletionRate = float64(progress.CompletedChunks) / float64(progress.TotalChunks) * 100
	}

	return progress, nil
}

// ChunkProgress 分片进度
type ChunkProgress struct {
	TaskId          string        `json:"taskId"`          // 任务ID
	TotalChunks     int           `json:"totalChunks"`     // 总分片数
	CompletedChunks int           `json:"completedChunks"` // 已完成分片数
	FailedChunks    int           `json:"failedChunks"`    // 失败分片数
	RunningChunks   int           `json:"runningChunks"`   // 运行中分片数
	TotalTargets    int           `json:"totalTargets"`    // 总目标数
	CompletionRate  float64       `json:"completionRate"`  // 完成百分比
	Chunks          []ChunkStatus `json:"chunks"`          // 分片状态列表
}

// ChunkStatus 分片状态
type ChunkStatus struct {
	ChunkId     string    `json:"chunkId"`     // 分片ID
	Status      string    `json:"status"`      // 状态
	StartTime   time.Time `json:"startTime"`   // 开始时间
	EndTime     time.Time `json:"endTime"`     // 结束时间
	Duration    int64     `json:"duration"`    // 执行时长（秒）
	TargetCount int       `json:"targetCount"` // 目标数量
	AssetCount  int       `json:"assetCount"`  // 发现资产数
	VulCount    int       `json:"vulCount"`    // 发现漏洞数
	ErrorMsg    string    `json:"errorMsg"`    // 错误信息
	WorkerName  string    `json:"workerName"`  // 执行Worker
}

// UpdateChunkStatus 更新分片状态
func (cm *ChunkManager) UpdateChunkStatus(ctx context.Context, chunkId, status string, details map[string]interface{}) error {
	key := cm.getChunkStatusKey(chunkId)

	// 获取现有状态
	existingStatus, _ := cm.getChunkStatus(ctx, chunkId)
	if existingStatus == nil {
		existingStatus = &ChunkStatus{
			ChunkId: chunkId,
		}
	}

	// 更新状态
	existingStatus.Status = status
	if status == "STARTED" && existingStatus.StartTime.IsZero() {
		existingStatus.StartTime = time.Now()
	}
	if (status == "SUCCESS" || status == "FAILURE") && existingStatus.EndTime.IsZero() {
		existingStatus.EndTime = time.Now()
		if !existingStatus.StartTime.IsZero() {
			existingStatus.Duration = int64(existingStatus.EndTime.Sub(existingStatus.StartTime).Seconds())
		}
	}

	// 更新详细信息
	if details != nil {
		if assetCount, ok := details["assetCount"].(int); ok {
			existingStatus.AssetCount = assetCount
		}
		if vulCount, ok := details["vulCount"].(int); ok {
			existingStatus.VulCount = vulCount
		}
		if errorMsg, ok := details["errorMsg"].(string); ok {
			existingStatus.ErrorMsg = errorMsg
		}
		if workerName, ok := details["workerName"].(string); ok {
			existingStatus.WorkerName = workerName
		}
		if targetCount, ok := details["targetCount"].(int); ok {
			existingStatus.TargetCount = targetCount
		}
	}

	// 保存到Redis
	data, err := json.Marshal(existingStatus)
	if err != nil {
		return err
	}

	return cm.rdb.Set(ctx, key, data, 24*time.Hour).Err()
}

// CleanupChunkData 清理分片数据
func (cm *ChunkManager) CleanupChunkData(ctx context.Context, taskId string) error {
	// 获取分片信息
	splitResult, err := cm.GetChunkInfo(ctx, taskId)
	if err != nil {
		return err
	}

	// 删除分片相关的Redis键
	var keys []string
	keys = append(keys, cm.getChunkInfoKey(taskId))

	for _, chunk := range splitResult.Chunks {
		keys = append(keys, cm.getChunkStatusKey(chunk.ChunkId))
		keys = append(keys, cm.getChunkTaskInfoKey(chunk.ChunkId))
	}

	if len(keys) > 0 {
		return cm.rdb.Del(ctx, keys...).Err()
	}

	return nil
}

// GetSplitPreview 获取拆分预览
func (cm *ChunkManager) GetSplitPreview(target string, config map[string]interface{}) (*SplitPreview, error) {
	return cm.splitter.GetSplitPreview(target, config)
}

// 内部方法

// saveChunkInfo 保存分片信息
func (cm *ChunkManager) saveChunkInfo(ctx context.Context, taskId string, splitResult *SplitResult) error {
	key := cm.getChunkInfoKey(taskId)
	data, err := json.Marshal(splitResult)
	if err != nil {
		return err
	}
	return cm.rdb.Set(ctx, key, data, 24*time.Hour).Err()
}

// saveChunkTaskInfo 保存分片任务信息
func (cm *ChunkManager) saveChunkTaskInfo(ctx context.Context, chunkId string, req *ChunkTaskRequest, chunk TaskChunk) error {
	key := cm.getChunkTaskInfoKey(chunkId)

	info := map[string]interface{}{
		"chunkId":      chunkId,
		"parentTaskId": req.TaskId,
		"workspaceId":  req.WorkspaceId,
		"mainTaskId":   req.MainTaskId,
		"chunkIndex":   chunk.Index,
		"targetCount":  chunk.TargetCount,
		"priority":     chunk.Priority,
		"createTime":   time.Now(),
	}

	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	return cm.rdb.Set(ctx, key, data, 24*time.Hour).Err()
}

// getChunkStatus 获取分片状态
func (cm *ChunkManager) getChunkStatus(ctx context.Context, chunkId string) (*ChunkStatus, error) {
	key := cm.getChunkStatusKey(chunkId)
	data, err := cm.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return &ChunkStatus{
				ChunkId: chunkId,
				Status:  "PENDING",
			}, nil
		}
		return nil, err
	}

	var status ChunkStatus
	if err := json.Unmarshal([]byte(data), &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// getSchedulerTasks 获取调度任务列表
func (cm *ChunkManager) getSchedulerTasks(ctx context.Context, taskId string) ([]*TaskInfo, error) {
	splitResult, err := cm.GetChunkInfo(ctx, taskId)
	if err != nil {
		return nil, err
	}

	var tasks []*TaskInfo
	for _, chunk := range splitResult.Chunks {
		// 从Redis获取分片任务信息
		key := cm.getChunkTaskInfoKey(chunk.ChunkId)
		data, err := cm.rdb.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var info map[string]interface{}
		if err := json.Unmarshal([]byte(data), &info); err != nil {
			continue
		}

		// 重建调度任务
		task := &TaskInfo{
			TaskId:      chunk.ChunkId,
			MainTaskId:  info["mainTaskId"].(string),
			WorkspaceId: info["workspaceId"].(string),
			Priority:    chunk.Priority,
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// Redis键生成方法
func (cm *ChunkManager) getChunkInfoKey(taskId string) string {
	return fmt.Sprintf("cscan:chunk:info:%s", taskId)
}

func (cm *ChunkManager) getChunkStatusKey(chunkId string) string {
	return fmt.Sprintf("cscan:chunk:status:%s", chunkId)
}

func (cm *ChunkManager) getChunkTaskInfoKey(chunkId string) string {
	return fmt.Sprintf("cscan:chunk:task:%s", chunkId)
}
