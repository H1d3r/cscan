package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/config"
	"cscan/api/internal/handler"
	"cscan/api/internal/svc"
	"cscan/model"
	"cscan/scheduler"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/cscan.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 创建服务上下文
	svcCtx := svc.NewServiceContext(c)

	// 创建HTTP服务器
	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	handler.RegisterHandlers(server, svcCtx)

	// 创建任务调度器服务
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Host,
		Password: c.Redis.Pass,
	})
	schedulerSvc := scheduler.NewSchedulerService(rdb, svcCtx.SyncMethods)
	go schedulerSvc.Start()

	// 启动定时任务执行消息订阅
	go startCronExecuteSubscriber(svcCtx, schedulerSvc.GetScheduler())

	// 启动孤儿任务恢复后台任务（每 5 分钟检查一次）
	go startOrphanedTaskRecovery(svcCtx)

	fmt.Printf("Starting API server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}

// CronExecuteMessage 定时任务执行消息
type CronExecuteMessage struct {
	CronTaskId  string `json:"cronTaskId"`
	WorkspaceId string `json:"workspaceId"`
	MainTaskId  string `json:"mainTaskId"`
	TaskName    string `json:"taskName"`
	Target      string `json:"target"`
	Config      string `json:"config"`
}

// startCronExecuteSubscriber 启动定时任务执行消息订阅
func startCronExecuteSubscriber(svcCtx *svc.ServiceContext, sched *scheduler.Scheduler) {
	ctx := context.Background()
	pubsub := svcCtx.RedisClient.Subscribe(ctx, "cscan:cron:execute")
	defer pubsub.Close()

	logx.Info("Cron execute subscriber started")

	ch := pubsub.Channel()
	for msg := range ch {
		var execMsg CronExecuteMessage
		if err := json.Unmarshal([]byte(msg.Payload), &execMsg); err != nil {
			logx.Errorf("Failed to parse cron execute message: %v", err)
			continue
		}

		logx.Infof("Received cron execute message: cronTaskId=%s, taskName=%s", execMsg.CronTaskId, execMsg.TaskName)

		// 创建新的 MainTask 并推送到队列
		if err := createAndPushCronTask(ctx, svcCtx, sched, &execMsg); err != nil {
			logx.Errorf("Failed to create cron task: %v", err)
		}
	}
}

// createAndPushCronTask 创建定时任务的 MainTask 并推送到队列
func createAndPushCronTask(ctx context.Context, svcCtx *svc.ServiceContext, sched *scheduler.Scheduler, msg *CronExecuteMessage) error {
	workspaceId := msg.WorkspaceId
	if workspaceId == "" {
		workspaceId = "default"
	}

	// 解析任务配置
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Config), &taskConfig); err != nil {
		return fmt.Errorf("failed to parse task config: %v", err)
	}

	// 生成新的任务ID
	newTaskId := uuid.New().String()

	// 创建新的 MainTask
	taskModel := svcCtx.GetMainTaskModel(workspaceId)
	newTask := &model.MainTask{
		TaskId:      newTaskId,
		Name:        fmt.Sprintf("%s (定时)", msg.TaskName),
		Target:      msg.Target,
		Config:      msg.Config,
		Status:      model.TaskStatusCreated,
		IsCron:      true,
		CronRule:    msg.CronTaskId,
	}

	if err := taskModel.Insert(ctx, newTask); err != nil {
		return fmt.Errorf("failed to insert main task: %v", err)
	}

	logx.Infof("Created cron main task: taskId=%s, name=%s", newTaskId, newTask.Name)

	// 计算子任务数量（基于目标数量和启用的模块数）
	targets := strings.Split(msg.Target, "\n")
	var validTargets []string
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t != "" {
			validTargets = append(validTargets, t)
		}
	}

	// 计算启用的模块数
	enabledModules := 0
	if ps, ok := taskConfig["portscan"].(map[string]interface{}); ok {
		if enable, _ := ps["enable"].(bool); enable {
			enabledModules++
		}
	}
	if ds, ok := taskConfig["domainscan"].(map[string]interface{}); ok {
		if enable, _ := ds["enable"].(bool); enable {
			enabledModules++
		}
	}
	if fp, ok := taskConfig["fingerprint"].(map[string]interface{}); ok {
		if enable, _ := fp["enable"].(bool); enable {
			enabledModules++
		}
	}
	if poc, ok := taskConfig["pocscan"].(map[string]interface{}); ok {
		if enable, _ := poc["enable"].(bool); enable {
			enabledModules++
		}
	}
	if dir, ok := taskConfig["dirscan"].(map[string]interface{}); ok {
		if enable, _ := dir["enable"].(bool); enable {
			enabledModules++
		}
	}
	if enabledModules == 0 {
		enabledModules = 1
	}

	// 分批处理目标
	batchSize := 50
	if bs, ok := taskConfig["batchSize"].(float64); ok && bs > 0 {
		batchSize = int(bs)
	}

	var batches []string
	for i := 0; i < len(validTargets); i += batchSize {
		end := i + batchSize
		if end > len(validTargets) {
			end = len(validTargets)
		}
		batches = append(batches, strings.Join(validTargets[i:end], "\n"))
	}
	if len(batches) == 0 {
		batches = []string{msg.Target}
	}

	subTaskCount := len(batches) * enabledModules

	// 更新任务状态为 STARTED
	now := time.Now()
	taskModel.Update(ctx, newTask.Id.Hex(), map[string]interface{}{
		"status":         model.TaskStatusStarted,
		"sub_task_count": subTaskCount,
		"sub_task_done":  0,
		"start_time":     now,
	})

	// 保存主任务信息到 Redis
	taskInfoKey := "cscan:task:info:" + newTaskId
	taskInfoData, _ := json.Marshal(map[string]interface{}{
		"workspaceId":    workspaceId,
		"mainTaskId":     newTask.Id.Hex(),
		"subTaskCount":   subTaskCount,
		"batchCount":     len(batches),
		"enabledModules": enabledModules,
	})
	svcCtx.RedisClient.Set(ctx, taskInfoKey, taskInfoData, 24*time.Hour)

	// 从配置中获取指定的 Worker 列表
	var workers []string
	if w, ok := taskConfig["workers"].([]interface{}); ok {
		for _, v := range w {
			if s, ok := v.(string); ok {
				workers = append(workers, s)
			}
		}
	}

	// 为每个批次创建子任务并推送到队列
	for i, batch := range batches {
		// 复制配置并替换目标
		subConfig := make(map[string]interface{})
		for k, v := range taskConfig {
			subConfig[k] = v
		}
		subConfig["target"] = batch
		subConfig["subTaskIndex"] = i
		subConfig["subTaskTotal"] = len(batches)
		subConfigBytes, _ := json.Marshal(subConfig)

		// 生成子任务ID
		subTaskId := newTaskId
		if len(batches) > 1 {
			subTaskId = newTaskId + "-" + strconv.Itoa(i)
		}

		schedTask := &scheduler.TaskInfo{
			TaskId:      subTaskId,
			MainTaskId:  newTask.Id.Hex(),
			WorkspaceId: workspaceId,
			TaskName:    newTask.Name,
			Config:      string(subConfigBytes),
			Priority:    0,
			Workers:     workers,
		}

		logx.Infof("Pushing cron sub-task %d/%d: taskId=%s, targets=%d", i+1, len(batches), subTaskId, len(strings.Split(batch, "\n")))

		if err := sched.PushTask(ctx, schedTask); err != nil {
			logx.Errorf("Failed to push cron task to queue: %v", err)
			continue
		}

		// 保存子任务信息到 Redis（多批次时）
		if len(batches) > 1 {
			subTaskInfoKey := "cscan:task:info:" + subTaskId
			subTaskInfoData, _ := json.Marshal(map[string]interface{}{
				"workspaceId":  workspaceId,
				"mainTaskId":   newTask.Id.Hex(),
				"subTaskCount": subTaskCount,
			})
			svcCtx.RedisClient.Set(ctx, subTaskInfoKey, subTaskInfoData, 24*time.Hour)
		}
	}

	logx.Infof("Cron task created and pushed: taskId=%s, batches=%d, subTaskCount=%d", newTaskId, len(batches), subTaskCount)
	return nil
}


// startOrphanedTaskRecovery 启动孤儿任务恢复后台任务
// 定期检查并恢复卡住的任务（状态为 STARTED 但长时间没有更新的任务）
func startOrphanedTaskRecovery(svcCtx *svc.ServiceContext) {
	logx.Info("Orphaned task recovery background job started")

	// 每 5 分钟检查一次
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		recoverOrphanedTasks(svcCtx)
	}
}

// recoverOrphanedTasks 恢复孤儿任务
func recoverOrphanedTasks(svcCtx *svc.ServiceContext) {
	ctx := context.Background()

	// 获取所有 workspace
	workspaces, err := svcCtx.WorkspaceModel.FindAll(ctx)
	if err != nil {
		logx.Errorf("[OrphanedTaskRecovery] Failed to get workspaces: %v", err)
		return
	}

	// 任务超时时间：30 分钟没有更新的任务认为需要恢复
	cutoffTime := time.Now().Add(-30 * time.Minute)
	totalRecovered := 0

	for _, ws := range workspaces {
		taskModel := svcCtx.GetMainTaskModel(ws.Name)

		// 查找状态为 STARTED 且超时的任务
		filter := map[string]interface{}{
			"status": "STARTED",
			"update_time": map[string]interface{}{
				"$lt": cutoffTime,
			},
		}

		tasks, err := taskModel.Find(ctx, filter, 0, 50)
		if err != nil {
			logx.Errorf("[OrphanedTaskRecovery] Failed to find tasks for workspace %s: %v", ws.Name, err)
			continue
		}

		for _, task := range tasks {
			// 将任务状态重置为 PENDING
			update := map[string]interface{}{
				"status":      "PENDING",
				"update_time": time.Now(),
			}

			if err := taskModel.UpdateByTaskId(ctx, task.TaskId, update); err != nil {
				logx.Errorf("[OrphanedTaskRecovery] Failed to update task %s: %v", task.TaskId, err)
				continue
			}

			// 重新将任务推入队列
			taskInfo := map[string]interface{}{
				"taskId":      task.TaskId,
				"mainTaskId":  task.TaskId,
				"workspaceId": ws.Name,
				"taskName":    task.Name,
				"config":      task.Config,
				"priority":    5, // 恢复任务使用较高优先级
				"createTime":  time.Now().Format("2006-01-02 15:04:05"),
			}

			taskData, _ := json.Marshal(taskInfo)
			score := float64(time.Now().Unix()) - 5000 // 提高优先级

			publicQueueKey := "cscan:task:queue"
			if err := svcCtx.RedisClient.ZAdd(ctx, publicQueueKey, redis.Z{
				Score:  score,
				Member: taskData,
			}).Err(); err != nil {
				logx.Errorf("[OrphanedTaskRecovery] Failed to requeue task %s: %v", task.TaskId, err)
				continue
			}

			totalRecovered++
			logx.Infof("[OrphanedTaskRecovery] Recovered task %s for workspace %s", task.TaskId, ws.Name)
		}
	}

	// 清理过期的 processing 集合记录
	cleanupStaleProcessingTasks(svcCtx)

	if totalRecovered > 0 {
		logx.Infof("[OrphanedTaskRecovery] Total recovered tasks: %d", totalRecovered)
	}
}

// cleanupStaleProcessingTasks 清理过期的处理中任务记录
func cleanupStaleProcessingTasks(svcCtx *svc.ServiceContext) {
	ctx := context.Background()
	processingKey := "cscan:task:processing"

	taskIds, err := svcCtx.RedisClient.SMembers(ctx, processingKey).Result()
	if err != nil {
		return
	}

	cleaned := 0
	for _, taskId := range taskIds {
		statusKey := "cscan:task:status:" + taskId
		_, err := svcCtx.RedisClient.Get(ctx, statusKey).Result()
		if err != nil {
			// 状态不存在，直接清理
			svcCtx.RedisClient.SRem(ctx, processingKey, taskId)
			cleaned++
		}
	}

	if cleaned > 0 {
		logx.Infof("[OrphanedTaskRecovery] Cleaned up %d stale processing records", cleaned)
	}
}
