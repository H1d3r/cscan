package logic

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"
	"cscan/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type CheckTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCheckTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckTaskLogic {
	return &CheckTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 检查任务状态 - 从Redis队列中获取待执行的任务
// 优先从 Worker 专属队列获取任务，然后从公共队列获取
func (l *CheckTaskLogic) CheckTask(in *pb.CheckTaskReq) (*pb.CheckTaskResp, error) {
	workerName := in.TaskId // TaskId 实际上是 Worker 名称

	publicQueueKey := "cscan:task:queue"
	workerQueueKey := "cscan:task:queue:worker:" + strings.ToLower(workerName)
	processingKey := "cscan:task:processing"

	// 1. 优先从 Worker 专属队列获取任务（使用 ZPopMin 原子操作）
	task, err := l.popTaskFromQueue(workerQueueKey, processingKey, workerName)
	if err != nil {
		l.Logger.Errorf("CheckTask: failed to pop from worker queue: %v", err)
	}
	if task != nil {
		return task, nil
	}

	// 2. 从公共队列获取任务（使用 ZPopMin 原子操作）
	task, err = l.popTaskFromQueue(publicQueueKey, processingKey, workerName)
	if err != nil {
		l.Logger.Errorf("CheckTask: failed to pop from public queue: %v", err)
	}
	if task != nil {
		return task, nil
	}

	return &pb.CheckTaskResp{IsExist: false}, nil
}

// popTaskFromQueue 从指定队列原子获取一个任务
func (l *CheckTaskLogic) popTaskFromQueue(queueKey, processingKey, workerName string) (*pb.CheckTaskResp, error) {
	// 使用 ZPopMin 原子获取优先级最高的任务
	results, err := l.svcCtx.RedisClient.ZPopMin(l.ctx, queueKey, 1).Result()
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	taskData := results[0].Member.(string)
	var task scheduler.TaskInfo
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		l.Logger.Errorf("CheckTask: failed to parse task: %v", err)
		return nil, nil
	}

	// 添加到处理中集合
	l.svcCtx.RedisClient.SAdd(l.ctx, processingKey, task.TaskId)

	l.Logger.Infof("CheckTask: assigned task %s to worker %s from queue %s", task.TaskId, workerName, queueKey)

	// 立即更新主任务状态为 STARTED
	l.updateMainTaskToStarted(task.MainTaskId, task.WorkspaceId)

	return &pb.CheckTaskResp{
		IsExist:     true,
		IsFinished:  false,
		TaskId:      task.TaskId,
		MainTaskId:  task.MainTaskId,
		WorkspaceId: task.WorkspaceId,
		Config:      task.Config,
	}, nil
}


// updateMainTaskToStarted 更新主任务状态为 STARTED
func (l *CheckTaskLogic) updateMainTaskToStarted(mainTaskId, workspaceId string) {
	if mainTaskId == "" || workspaceId == "" {
		l.Logger.Errorf("CheckTask: updateMainTaskToStarted called with empty params: mainTaskId='%s', workspaceId='%s'", mainTaskId, workspaceId)
		return
	}

	l.Logger.Infof("CheckTask: updating main task status to STARTED, mainTaskId=%s, workspaceId=%s", mainTaskId, workspaceId)

	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)
	task, err := taskModel.FindById(l.ctx, mainTaskId)
	if err != nil {
		l.Logger.Errorf("CheckTask: failed to find main task %s in workspace %s: %v", mainTaskId, workspaceId, err)
		return
	}

	l.Logger.Infof("CheckTask: found main task, id=%s, taskId=%s, current status='%s'", task.Id.Hex(), task.TaskId, task.Status)

	// PENDING、CREATED 或空状态都更新为 STARTED
	if task.Status == "PENDING" || task.Status == "CREATED" || task.Status == "" {
		now := time.Now()
		update := bson.M{
			"status":     "STARTED",
			"start_time": now,
		}
		if err := taskModel.Update(l.ctx, mainTaskId, update); err != nil {
			l.Logger.Errorf("CheckTask: failed to update main task status: %v", err)
		} else {
			l.Logger.Infof("CheckTask: main task %s status updated from '%s' to STARTED successfully", mainTaskId, task.Status)
		}
	} else {
		l.Logger.Infof("CheckTask: main task %s status is '%s', not updating to STARTED", mainTaskId, task.Status)
	}
}
