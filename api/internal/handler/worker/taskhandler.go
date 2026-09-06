package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/internal/scheduler"
	"cscan/pkg/response"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// ==================== Worker Task Types ====================

// WorkerTaskCheckReq 任务拉取请求
type WorkerTaskCheckReq struct {
	WorkerName   string `json:"workerName"`
	InstanceID   string `json:"instanceId"`
	TaskProtocol int    `json:"taskProtocol"`
}

// WorkerTaskCheckResp 任务拉取响应
type WorkerTaskCheckResp struct {
	Code               int    `json:"code"`
	Msg                string `json:"msg"`
	IsExist            bool   `json:"isExist"`
	IsFinished         bool   `json:"isFinished"`
	TaskId             string `json:"taskId"`
	MainTaskId         string `json:"mainTaskId"`
	Config             string `json:"config"`
	LeaseToken         string `json:"leaseToken,omitempty"`
	DispatchGeneration string `json:"dispatchGeneration,omitempty"`
}

// WorkerTaskUpdateReq 任务状态更新请求
type WorkerTaskUpdateReq struct {
	TaskId     string `json:"taskId"`
	MainTaskId string `json:"mainTaskId,omitempty"`
	LeaseToken string `json:"leaseToken,omitempty"`
	State      string `json:"state"` // started, success, failure, paused
	Worker     string `json:"worker"`
	Result     string `json:"result"`
	Progress   int    `json:"progress"`  // 0-100
	Phase      string `json:"phase"`     // 当前阶段描述
	TaskState  string `json:"taskState"` // 暂停恢复快照 JSON
}

// WorkerTaskUpdateResp 任务状态更新响应
type WorkerTaskUpdateResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// ==================== Task Check Handler ====================

func validWorkerTaskIdentity(instanceID string, taskProtocol int) bool {
	if taskProtocol != scheduler.TaskProtocolV1 {
		return false
	}
	_, err := uuid.Parse(instanceID)
	return err == nil
}

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
		if !validWorkerTaskIdentity(req.InstanceID, req.TaskProtocol) {
			httpx.OkJson(w, &WorkerTaskCheckResp{Code: http.StatusUpgradeRequired, Msg: "leased-task-v1 instance identity required"})
			return
		}

		checkCtx, checkCancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer checkCancel()

		result, err := svcCtx.CheckTask(checkCtx, req.WorkerName, req.InstanceID)
		if err != nil {
			response.Error(w, err)
			return
		}

		httpx.OkJson(w, &WorkerTaskCheckResp{
			Code:               0,
			Msg:                "success",
			IsExist:            result.IsExist,
			IsFinished:         result.IsFinished,
			TaskId:             result.TaskId,
			MainTaskId:         result.MainTaskId,
			Config:             result.Config,
			LeaseToken:         result.LeaseToken,
			DispatchGeneration: result.DispatchGeneration,
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

		err := svcCtx.UpdateTask(r.Context(), req.TaskId, req.MainTaskId, req.LeaseToken, req.State, req.Worker, req.Result, req.Phase, req.TaskState, req.Progress)
		if err != nil {
			logx.Errorf("[WorkerTaskUpdate] error: %v", err)
			if errors.Is(err, scheduler.ErrTaskParentFenced) {
				httpx.OkJson(w, &WorkerTaskUpdateResp{
					Code:    http.StatusLocked,
					Msg:     "task is fenced by a parent control; await exact control",
					Success: false,
				})
				return
			}
			if errors.Is(err, scheduler.ErrTaskLeaseConflict) {
				httpx.OkJson(w, &WorkerTaskUpdateResp{
					Code:    http.StatusConflict,
					Msg:     "task lease conflict",
					Success: false,
				})
				return
			}
			if errors.Is(err, scheduler.ErrTaskOperationBusy) {
				httpx.OkJson(w, &WorkerTaskUpdateResp{
					Code:    http.StatusTooEarly,
					Msg:     "task operation is busy; retry",
					Success: false,
				})
				return
			}
			response.Error(w, err)
			return
		}

		httpx.OkJson(w, &WorkerTaskUpdateResp{
			Code:    0,
			Msg:     "Task status updated",
			Success: true,
		})
	}
}

// ==================== Task Control Handler ====================

// WorkerTaskControlReq requests controls for exact running dispatches.
type WorkerTaskControlReq struct {
	WorkerName string                        `json:"workerName"`
	Targets    []scheduler.TaskControlTarget `json:"targets"`
}

// WorkerTaskControlResp returns the same strict envelope stored in Redis.
type WorkerTaskControlResp struct {
	Code    int                             `json:"code"`
	Msg     string                          `json:"msg"`
	Success bool                            `json:"success"`
	Signals []scheduler.TaskControlEnvelope `json:"signals"`
}

// WorkerTaskControlHandler is the durable HTTP polling path for exact v1
// controls. Generation-blind task IDs and malformed Redis values fail closed.
func WorkerTaskControlHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerTaskControlReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerTaskControlResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		signals := make([]scheduler.TaskControlEnvelope, 0, len(req.Targets))
		seen := make(map[string]struct{}, len(req.Targets))
		for _, target := range req.Targets {
			if err := target.Validate(); err != nil {
				httpx.OkJson(w, &WorkerTaskControlResp{Code: 400, Msg: err.Error(), Signals: []scheduler.TaskControlEnvelope{}})
				return
			}
			key, _ := scheduler.TaskControlKey(target.TaskID, target.DispatchGeneration)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			envelope, err := svcCtx.Scheduler.GetTaskControl(r.Context(), target)
			if err != nil {
				logx.Errorf("[WorkerTaskControl] exact control lookup failed for %s: %v", key, err)
				httpx.OkJson(w, &WorkerTaskControlResp{Code: 500, Msg: "读取任务控制信号失败", Signals: []scheduler.TaskControlEnvelope{}})
				return
			}
			if envelope != nil {
				signals = append(signals, *envelope)
			}
		}

		httpx.OkJson(w, &WorkerTaskControlResp{
			Code: 0, Msg: "success", Success: true, Signals: signals,
		})
	}
}

// ==================== Task Recovery Handler ====================

// WorkerTaskRecoveryReq 任务恢复请求
type WorkerTaskRecoveryReq struct {
	WorkerName   string `json:"workerName"`
	InstanceID   string `json:"instanceId"`
	TaskProtocol int    `json:"taskProtocol"`
}

// RecoveredTaskInfo 恢复的任务信息
type RecoveredTaskInfo struct {
	TaskId     string `json:"taskId"`
	MainTaskId string `json:"mainTaskId"`
	Status     string `json:"status"`
	StartTime  string `json:"startTime"`
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
		if !validWorkerTaskIdentity(req.InstanceID, req.TaskProtocol) {
			httpx.OkJson(w, &WorkerTaskRecoveryResp{Code: http.StatusUpgradeRequired, Msg: "leased-task-v1 instance identity required"})
			return
		}

		logx.Infof("[WorkerTaskRecovery] Worker %s instance %s requesting task recovery", req.WorkerName, req.InstanceID)

		ctx := r.Context()

		// Recover this worker's exact children before any stale-record cleanup.
		// RequeueExactTask transfers queue ownership atomically, so sibling batches
		// remain independently recoverable.
		recoveredTasksInfo, err := logic.RecoverWorkerTasks(ctx, svcCtx, req.WorkerName, req.InstanceID)
		if err != nil {
			httpx.OkJson(w, &WorkerTaskRecoveryResp{
				Code:    500,
				Msg:     "恢复任务失败",
				Success: false,
			})
			return
		}
		logic.CleanupStaleProcessingTasks(ctx, svcCtx, req.WorkerName)

		// 转换为响应数据结构
		var recoveredTasks []RecoveredTaskInfo
		for _, v := range recoveredTasksInfo {
			recoveredTasks = append(recoveredTasks, RecoveredTaskInfo{
				TaskId:     v.TaskId,
				MainTaskId: v.MainTaskId,
				Status:     v.Status,
				StartTime:  v.StartTime,
			})
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
