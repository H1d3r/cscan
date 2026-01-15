package worker

import (
	"encoding/json"
	"net/http"
	"time"

	"cscan/api/internal/svc"
	"cscan/pkg/response"
	"cscan/rpc/task/pb"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"go.mongodb.org/mongo-driver/bson"
)

// ==================== Worker Task Types ====================

// WorkerTaskCheckReq 任务拉取请求
type WorkerTaskCheckReq struct {
	WorkerName string `json:"workerName"`
}

// WorkerTaskCheckResp 任务拉取响应
type WorkerTaskCheckResp struct {
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	IsExist     bool   `json:"isExist"`
	IsFinished  bool   `json:"isFinished"`
	TaskId      string `json:"taskId"`
	MainTaskId  string `json:"mainTaskId"`
	WorkspaceId string `json:"workspaceId"`
	Config      string `json:"config"`
}

// WorkerTaskUpdateReq 任务状态更新请求
type WorkerTaskUpdateReq struct {
	TaskId   string `json:"taskId"`
	State    string `json:"state"`    // started, success, failure, paused
	Worker   string `json:"worker"`
	Result   string `json:"result"`
	Progress int    `json:"progress"` // 0-100
	Phase    string `json:"phase"`    // 当前阶段描述
}

// WorkerTaskUpdateResp 任务状态更新响应
type WorkerTaskUpdateResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// ==================== Task Check Handler ====================

// WorkerTaskCheckHandler 任务拉取接口
// POST /api/v1/worker/task/check
func WorkerTaskCheckHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerTaskCheckReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerTaskCheckResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if req.WorkerName == "" {
			httpx.OkJson(w, &WorkerTaskCheckResp{Code: 400, Msg: "workerName不能为空"})
			return
		}

		// 调用RPC CheckTask
		// 注意：RPC 的 TaskId 字段实际用于传递 WorkerName
		rpcReq := &pb.CheckTaskReq{
			TaskId:     req.WorkerName,
			MainTaskId: "",
		}

		rpcResp, err := svcCtx.TaskRpcClient.CheckTask(r.Context(), rpcReq)
		if err != nil {
			// RPC 连接错误不输出日志，避免 Worker 轮询时产生大量日志
			response.Error(w, err)
			return
		}

		httpx.OkJson(w, &WorkerTaskCheckResp{
			Code:        0,
			Msg:         "success",
			IsExist:     rpcResp.IsExist,
			IsFinished:  rpcResp.IsFinished,
			TaskId:      rpcResp.TaskId,
			MainTaskId:  rpcResp.MainTaskId,
			WorkspaceId: rpcResp.WorkspaceId,
			Config:      rpcResp.Config,
		})
	}
}

// ==================== Task Update Handler ====================

// WorkerTaskUpdateHandler 任务状态更新接口
// POST /api/v1/worker/task/update
func WorkerTaskUpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerTaskUpdateReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerTaskUpdateResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if req.TaskId == "" {
			httpx.OkJson(w, &WorkerTaskUpdateResp{Code: 400, Msg: "taskId不能为空"})
			return
		}

		// 调用RPC UpdateTask
		rpcReq := &pb.UpdateTaskReq{
			TaskId: req.TaskId,
			State:  req.State,
			Worker: req.Worker,
			Result: req.Result,
			Phase:  req.Phase,
		}

		rpcResp, err := svcCtx.TaskRpcClient.UpdateTask(r.Context(), rpcReq)
		if err != nil {
			logx.Errorf("[WorkerTaskUpdate] RPC error: %v", err)
			response.Error(w, err)
			return
		}

		httpx.OkJson(w, &WorkerTaskUpdateResp{
			Code:    0,
			Msg:     rpcResp.Message,
			Success: rpcResp.Success,
		})
	}
}

// ==================== Task Control Handler ====================

// WorkerTaskControlReq 任务控制信号请求
type WorkerTaskControlReq struct {
	WorkerName string   `json:"workerName"`
	TaskIds    []string `json:"taskIds"` // 当前正在执行的任务ID列表
}

// TaskControlSignal 单个任务的控制信号
type TaskControlSignal struct {
	TaskId string `json:"taskId"`
	Action string `json:"action"` // STOP, PAUSE, RESUME
}

// WorkerTaskControlResp 任务控制信号响应
type WorkerTaskControlResp struct {
	Code    int                 `json:"code"`
	Msg     string              `json:"msg"`
	Success bool                `json:"success"`
	Signals []TaskControlSignal `json:"signals"`
}

// WorkerTaskControlHandler 任务控制信号轮询接口
// POST /api/v1/worker/task/control
// 用于WebSocket不可用时的HTTP轮询回退
func WorkerTaskControlHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerTaskControlReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerTaskControlResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if len(req.TaskIds) == 0 {
			httpx.OkJson(w, &WorkerTaskControlResp{
				Code:    0,
				Msg:     "success",
				Success: true,
				Signals: []TaskControlSignal{},
			})
			return
		}

		// 从Redis检查每个任务的控制信号
		var signals []TaskControlSignal
		ctx := r.Context()

		for _, taskId := range req.TaskIds {
			// 检查Redis中是否有该任务的控制信号
			// 控制信号存储在 cscan:task:ctrl:{taskId} 键中
			ctrlKey := "cscan:task:ctrl:" + taskId
			action, err := svcCtx.RedisClient.Get(ctx, ctrlKey).Result()
			if err == nil && action != "" {
				signals = append(signals, TaskControlSignal{
					TaskId: taskId,
					Action: action,
				})
			}
		}

		httpx.OkJson(w, &WorkerTaskControlResp{
			Code:    0,
			Msg:     "success",
			Success: true,
			Signals: signals,
		})
	}
}


// ==================== Task Recovery Handler ====================

// WorkerTaskRecoveryReq 任务恢复请求
type WorkerTaskRecoveryReq struct {
	WorkerName string `json:"workerName"`
}

// RecoveredTaskInfo 恢复的任务信息
type RecoveredTaskInfo struct {
	TaskId      string `json:"taskId"`
	MainTaskId  string `json:"mainTaskId"`
	WorkspaceId string `json:"workspaceId"`
	Status      string `json:"status"`
	StartTime   string `json:"startTime"`
}

// WorkerTaskRecoveryResp 任务恢复响应
type WorkerTaskRecoveryResp struct {
	Code           int                 `json:"code"`
	Msg            string              `json:"msg"`
	Success        bool                `json:"success"`
	RecoveredTasks []RecoveredTaskInfo `json:"recoveredTasks"`
	RecoveredCount int                 `json:"recoveredCount"`
}

// WorkerTaskRecoveryHandler Worker 启动时的任务恢复接口
// POST /api/v1/worker/task/recovery
// 当 Worker 重新启动时调用，恢复该 Worker 之前未完成的任务
func WorkerTaskRecoveryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerTaskRecoveryReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerTaskRecoveryResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if req.WorkerName == "" {
			httpx.OkJson(w, &WorkerTaskRecoveryResp{Code: 400, Msg: "workerName不能为空"})
			return
		}

		logx.Infof("[WorkerTaskRecovery] Worker %s requesting task recovery", req.WorkerName)

		ctx := r.Context()
		var recoveredTasks []RecoveredTaskInfo

		// 清理该 Worker 在 processing 集合中的任务记录
		processingKey := "cscan:task:processing"
		taskIds, err := svcCtx.RedisClient.SMembers(ctx, processingKey).Result()
		if err == nil {
			for _, taskId := range taskIds {
				statusKey := "cscan:task:status:" + taskId
				statusData, err := svcCtx.RedisClient.Get(ctx, statusKey).Result()
				if err != nil {
					// 状态不存在，从处理中集合移除
					svcCtx.RedisClient.SRem(ctx, processingKey, taskId)
					continue
				}

				var status map[string]interface{}
				if err := json.Unmarshal([]byte(statusData), &status); err != nil {
					continue
				}

				// 如果任务关联到指定 Worker，清理它
				if worker, ok := status["worker"].(string); ok {
					if worker == req.WorkerName {
						svcCtx.RedisClient.SRem(ctx, processingKey, taskId)
						svcCtx.RedisClient.Del(ctx, statusKey)
					}
				}
			}
		}

		// 获取所有 workspace 并恢复卡住的任务
		workspaces, err := svcCtx.WorkspaceModel.FindAll(ctx)
		if err != nil {
			logx.Errorf("[WorkerTaskRecovery] Failed to get workspaces: %v", err)
			httpx.OkJson(w, &WorkerTaskRecoveryResp{
				Code:    500,
				Msg:     "获取工作空间失败",
				Success: false,
			})
			return
		}

		// 任务超时时间：5 分钟没有更新的任务认为需要恢复
		cutoffTime := time.Now().Add(-5 * time.Minute)

		for _, ws := range workspaces {
			taskModel := svcCtx.GetMainTaskModel(ws.Name)

			// 查找状态为 STARTED 且超时的任务
			filter := bson.M{
				"status": "STARTED",
				"update_time": bson.M{
					"$lt": cutoffTime,
				},
			}

			tasks, err := taskModel.Find(ctx, filter, 0, 50)
			if err != nil {
				logx.Errorf("[WorkerTaskRecovery] Failed to find tasks for workspace %s: %v", ws.Name, err)
				continue
			}

			for _, task := range tasks {
				// 将任务状态重置为 PENDING
				update := bson.M{
					"status":      "PENDING",
					"update_time": time.Now(),
				}

				if err := taskModel.UpdateByTaskId(ctx, task.TaskId, update); err != nil {
					logx.Errorf("[WorkerTaskRecovery] Failed to update task %s: %v", task.TaskId, err)
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
					logx.Errorf("[WorkerTaskRecovery] Failed to requeue task %s: %v", task.TaskId, err)
					continue
				}

				startTimeStr := ""
				if task.StartTime != nil {
					startTimeStr = task.StartTime.Format("2006-01-02 15:04:05")
				}

				recoveredTasks = append(recoveredTasks, RecoveredTaskInfo{
					TaskId:      task.TaskId,
					MainTaskId:  task.TaskId,
					WorkspaceId: ws.Name,
					Status:      task.Status,
					StartTime:   startTimeStr,
				})
			}
		}

		if len(recoveredTasks) > 0 {
			logx.Infof("[WorkerTaskRecovery] Worker %s recovered %d tasks", req.WorkerName, len(recoveredTasks))
		}

		httpx.OkJson(w, &WorkerTaskRecoveryResp{
			Code:           0,
			Msg:            "success",
			Success:        true,
			RecoveredTasks: recoveredTasks,
			RecoveredCount: len(recoveredTasks),
		})
	}
}
