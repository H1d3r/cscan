package logic

import (
	"context"
	"encoding/json"
	"time"

	"cscan/pkg/notify"
	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"
	"cscan/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type UpdateTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateTaskLogic {
	return &UpdateTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 更新任务状态
func (l *UpdateTaskLogic) UpdateTask(in *pb.UpdateTaskReq) (*pb.UpdateTaskResp, error) {
	taskId := in.TaskId
	state := in.State

	l.Logger.Infof("UpdateTask: taskId=%s, state=%s, phase=%s", taskId, state, in.Phase)

	// 从处理中集合移除
	processingKey := "cscan:task:processing"
	l.svcCtx.RedisClient.SRem(l.ctx, processingKey, taskId)

	// 更新任务状态到Redis（包含当前阶段）
	statusKey := "cscan:task:status:" + taskId
	statusData := map[string]interface{}{
		"taskId": taskId,
		"state":  state,
		"worker": in.Worker,
		"result": in.Result,
		"phase":  in.Phase,
	}
	statusJson, _ := json.Marshal(statusData)
	l.svcCtx.RedisClient.Set(l.ctx, statusKey, statusJson, 0)

	// 更新进度信息到Redis（用于前端实时获取当前阶段）
	if in.Phase != "" {
		progressKey := "cscan:task:progress:" + taskId
		progressData := map[string]interface{}{
			"currentPhase": in.Phase,
		}
		progressJson, _ := json.Marshal(progressData)
		l.svcCtx.RedisClient.Set(l.ctx, progressKey, progressJson, 24*time.Hour)
	}

	// 如果任务完成或失败，添加到完成集合
	if state == "SUCCESS" || state == "FAILURE" || state == "COMPLETED" {
		completedKey := "cscan:task:completed"
		taskInfo := scheduler.TaskInfo{
			TaskId: taskId,
		}
		taskJson, _ := json.Marshal(taskInfo)
		l.svcCtx.RedisClient.SAdd(l.ctx, completedKey, string(taskJson))
	}

	// 更新数据库中的任务状态（包括开始时间、结束时间、进度、当前阶段）
	l.updateTaskInDBWithPhase(taskId, state, in.Result, in.Phase)

	return &pb.UpdateTaskResp{
		Success: true,
		Message: "Task status updated",
	}, nil
}

// updateTaskInDB 更新数据库中的任务状态
func (l *UpdateTaskLogic) updateTaskInDB(taskId, state, result string) {
	l.updateTaskInDBWithPhase(taskId, state, result, "")
}

// updateTaskInDBWithPhase 更新数据库中的任务状态（包含阶段）
func (l *UpdateTaskLogic) updateTaskInDBWithPhase(taskId, state, result, phase string) {
	// 如果状态为空且阶段为空，只是进度更新，不更新数据库状态
	if state == "" && phase == "" {
		l.Logger.Infof("UpdateTask: state and phase are empty for taskId=%s, skipping DB update (progress only)", taskId)
		return
	}

	// 从Redis获取任务信息（workspaceId）
	taskInfoKey := "cscan:task:info:" + taskId
	taskInfoData, err := l.svcCtx.RedisClient.Get(l.ctx, taskInfoKey).Result()
	if err != nil {
		l.Logger.Errorf("UpdateTask: failed to get task info from Redis, taskId=%s, error=%v", taskId, err)
		return
	}

	var taskInfo map[string]interface{}
	if err := json.Unmarshal([]byte(taskInfoData), &taskInfo); err != nil {
		l.Logger.Errorf("UpdateTask: failed to parse task info, taskId=%s, error=%v", taskId, err)
		return
	}

	workspaceId, _ := taskInfo["workspaceId"].(string)
	mainTaskId, _ := taskInfo["mainTaskId"].(string) // MongoDB ObjectID (Hex)
	subTaskCount := 1
	if count, ok := taskInfo["subTaskCount"].(float64); ok {
		subTaskCount = int(count)
	}
	if workspaceId == "" {
		l.Logger.Errorf("UpdateTask: workspaceId is empty, taskId=%s", taskId)
		return
	}

	// 获取任务模型
	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)
	now := time.Now()

	// 构建更新字段
	update := bson.M{}
	
	// 如果有状态，更新状态
	if state != "" {
		update["status"] = state
	}
	
	// 如果有阶段，更新当前阶段
	if phase != "" {
		update["current_phase"] = phase
	}

	l.Logger.Infof("UpdateTask: taskId=%s, mainTaskId=%s, subTaskCount=%d, state=%s, phase=%s", taskId, mainTaskId, subTaskCount, state, phase)

	// 根据状态设置不同字段
	switch state {
	case "STARTED":
		// 任务开始时设置开始时间和状态
		// 检查主任务当前状态，如果已经是STARTED则不重复设置
		task, err := taskModel.FindById(l.ctx, mainTaskId)
		if err != nil {
			l.Logger.Errorf("UpdateTask: failed to find task, mainTaskId=%s, error=%v", mainTaskId, err)
			// 查询失败时仍然尝试更新状态和开始时间
			update["start_time"] = now
		} else if task.Status == "STARTED" {
			// 主任务已经是STARTED状态，只更新阶段（如果有）
			if phase != "" {
				l.Logger.Infof("UpdateTask: main task %s already STARTED, updating phase only", mainTaskId)
				update = bson.M{"current_phase": phase}
			} else {
				l.Logger.Infof("UpdateTask: main task %s already STARTED, skipping update", mainTaskId)
				return
			}
		} else {
			// 主任务不是STARTED状态（如PENDING/CREATED），更新状态和开始时间
			l.Logger.Infof("UpdateTask: updating main task %s from %s to STARTED", mainTaskId, task.Status)
			update["start_time"] = now
		}
	case "SUCCESS", "COMPLETED":
		// 如果有多个子任务（subTaskCount > 1），不在这里更新主任务状态
		// 主任务的完成状态由 IncrSubTaskDone 在所有子任务完成后设置
		if subTaskCount > 1 {
			l.Logger.Infof("UpdateTask: task %s has %d sub-tasks, skipping status update (managed by IncrSubTaskDone)", taskId, subTaskCount)
			return
		}
		// 单任务（subTaskCount <= 1）完成时设置结束时间
		update["end_time"] = now
		update["result"] = result
		// 触发任务完成通知
		l.sendTaskNotification(workspaceId, mainTaskId, state)
	case "FAILURE":
		// 任务失败时设置结束时间
		update["end_time"] = now
		update["result"] = result
		// 触发任务失败通知
		l.sendTaskNotification(workspaceId, mainTaskId, state)
	case "STOPPED":
		// 任务停止时设置结束时间
		update["end_time"] = now
		update["result"] = "任务已停止"
	case "":
		// 只更新阶段，不更新状态
		if phase == "" {
			return
		}
		// phase 已经在上面设置了，直接更新
	}

	// 更新数据库，mainTaskId 是 MongoDB ObjectID
	if mainTaskId != "" {
		if err := taskModel.Update(l.ctx, mainTaskId, update); err != nil {
			l.Logger.Errorf("UpdateTask: failed to update task in DB, mainTaskId=%s, error=%v", mainTaskId, err)
		} else {
			l.Logger.Infof("UpdateTask: task updated in DB, mainTaskId=%s, state=%s", mainTaskId, state)
		}
	}
}

// sendTaskNotification 发送任务完成通知
func (l *UpdateTaskLogic) sendTaskNotification(workspaceId, mainTaskId, status string) {
	// 获取任务详情
	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)
	task, err := taskModel.FindById(l.ctx, mainTaskId)
	if err != nil {
		l.Logger.Errorf("sendTaskNotification: failed to get task, mainTaskId=%s, error=%v", mainTaskId, err)
		return
	}

	// 获取资产和漏洞统计
	assetModel := l.svcCtx.GetAssetModel(workspaceId)
	vulModel := l.svcCtx.GetVulModel(workspaceId)

	assetCount, _ := assetModel.CountByTaskId(l.ctx, mainTaskId)
	vulCount, _ := vulModel.CountByTaskId(l.ctx, mainTaskId)

	// 获取启用的通知配置
	configs, err := l.svcCtx.NotifyConfigModel.FindEnabled(l.ctx)
	if err != nil {
		l.Logger.Errorf("sendTaskNotification: failed to get notify configs, error=%v", err)
		return
	}

	if len(configs) == 0 {
		l.Logger.Infof("sendTaskNotification: no enabled notify configs")
		return
	}

	// 构建通知配置列表
	var configItems []notify.ConfigItem
	for _, c := range configs {
		item := notify.ConfigItem{
			Provider:        c.Provider,
			Config:          c.Config,
			Status:          c.Status,
			MessageTemplate: c.MessageTemplate,
		}
		// 转换高危过滤配置
		if c.HighRiskFilter != nil {
			item.HighRiskFilter = &notify.HighRiskFilter{
				Enabled:              c.HighRiskFilter.Enabled,
				HighRiskFingerprints: c.HighRiskFilter.HighRiskFingerprints,
				HighRiskPorts:        c.HighRiskFilter.HighRiskPorts,
				HighRiskPocSeverities: c.HighRiskFilter.HighRiskPocSeverities,
			}
		}
		configItems = append(configItems, item)
	}

	// 构建通知结果
	result := &notify.NotifyResult{
		TaskId:      mainTaskId,
		TaskName:    task.Name,
		Status:      status,
		AssetCount:  int(assetCount),
		VulCount:    int(vulCount),
		WorkspaceId: workspaceId,
	}

	// 设置时间（处理指针类型）
	if task.StartTime != nil {
		result.StartTime = *task.StartTime
	}
	if task.EndTime != nil {
		result.EndTime = *task.EndTime
	}

	// 计算耗时
	if task.StartTime != nil && task.EndTime != nil {
		d := task.EndTime.Sub(*task.StartTime)
		if d.Hours() >= 1 {
			result.Duration = d.Round(time.Minute).String()
		} else if d.Minutes() >= 1 {
			result.Duration = d.Round(time.Second).String()
		} else {
			result.Duration = d.Round(time.Millisecond).String()
		}
	}

	// 收集高危信息（用于高危过滤判断）
	result.HighRiskInfo = l.collectHighRiskInfo(workspaceId, mainTaskId, configItems)

	// 异步发送通知
	notify.SendNotificationAsync(l.ctx, configItems, result)
	l.Logger.Infof("sendTaskNotification: notification queued for task %s, status=%s", mainTaskId, status)
}

// collectHighRiskInfo 收集任务的高危信息
func (l *UpdateTaskLogic) collectHighRiskInfo(workspaceId, mainTaskId string, configs []notify.ConfigItem) *notify.HighRiskInfo {
	// 检查是否有配置启用了高危过滤
	hasHighRiskFilter := false
	var allFingerprints []string
	var allPorts []int
	var allSeverities []string

	for _, cfg := range configs {
		if cfg.HighRiskFilter != nil && cfg.HighRiskFilter.Enabled {
			hasHighRiskFilter = true
			allFingerprints = append(allFingerprints, cfg.HighRiskFilter.HighRiskFingerprints...)
			allPorts = append(allPorts, cfg.HighRiskFilter.HighRiskPorts...)
			allSeverities = append(allSeverities, cfg.HighRiskFilter.HighRiskPocSeverities...)
		}
	}

	// 如果没有配置启用高危过滤，不需要收集
	if !hasHighRiskFilter {
		return nil
	}

	info := &notify.HighRiskInfo{
		HighRiskFingerprints:  []string{},
		HighRiskPorts:         []int{},
		HighRiskVulSeverities: make(map[string]int),
	}

	// 收集高危指纹（从资产的指纹中匹配）
	if len(allFingerprints) > 0 {
		assetModel := l.svcCtx.GetAssetModel(workspaceId)
		assets, err := assetModel.FindByTaskId(l.ctx, mainTaskId)
		if err == nil {
			fingerprintSet := make(map[string]bool)
			for _, fp := range allFingerprints {
				fingerprintSet[fp] = true
			}
			foundFpSet := make(map[string]bool)
			for _, asset := range assets {
				for _, fp := range asset.Fingerprints {
					if fingerprintSet[fp] && !foundFpSet[fp] {
						info.HighRiskFingerprints = append(info.HighRiskFingerprints, fp)
						foundFpSet[fp] = true
					}
				}
			}
		}
	}

	// 收集高危端口（从资产的端口中匹配）
	if len(allPorts) > 0 {
		assetModel := l.svcCtx.GetAssetModel(workspaceId)
		assets, err := assetModel.FindByTaskId(l.ctx, mainTaskId)
		if err == nil {
			portSet := make(map[int]bool)
			for _, port := range allPorts {
				portSet[port] = true
			}
			foundPortSet := make(map[int]bool)
			for _, asset := range assets {
				if portSet[asset.Port] && !foundPortSet[asset.Port] {
					info.HighRiskPorts = append(info.HighRiskPorts, asset.Port)
					foundPortSet[asset.Port] = true
				}
			}
		}
	}

	// 收集高危漏洞统计
	if len(allSeverities) > 0 {
		vulModel := l.svcCtx.GetVulModel(workspaceId)
		vuls, err := vulModel.Find(l.ctx, bson.M{"task_id": mainTaskId}, 0, 0)
		if err == nil {
			severitySet := make(map[string]bool)
			for _, s := range allSeverities {
				severitySet[s] = true
			}
			for _, vul := range vuls {
				if severitySet[vul.Severity] {
					info.HighRiskVulSeverities[vul.Severity]++
					info.HighRiskVulCount++
				}
			}
		}
	}

	return info
}
