package worker

import (
	"encoding/json"
	"errors"
	"net/http"

	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/scheduler"
	"cscan/pkg/response"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// ==================== SubTask Types ====================

// WorkerSubTaskDoneReq 子任务完成请求
type WorkerSubTaskDoneReq struct {
	TaskId      string                  `json:"taskId"`
	MainTaskId  string                  `json:"mainTaskId"`
	LeaseToken  string                  `json:"leaseToken"`
	Phase       string                  `json:"phase"`
	IsCompleted bool                    `json:"isCompleted"`
	IncrAmount  int                     `json:"incrAmount"`
	PhaseResult *model.TaskPhaseSummary `json:"phaseResult,omitempty"`
	TaskSummary *model.TaskScanSummary  `json:"taskSummary,omitempty"`
}

// WorkerSubTaskDoneResp 子任务完成响应
type WorkerSubTaskDoneResp struct {
	Code                int                    `json:"code"`
	Msg                 string                 `json:"msg"`
	Success             bool                   `json:"success"`
	SubTaskDone         int32                  `json:"subTaskDone"`
	SubTaskCount        int32                  `json:"subTaskCount"`
	AllDone             bool                   `json:"allDone"`
	Recorded            bool                   `json:"recorded,omitempty"`
	LeaseClosed         bool                   `json:"leaseClosed,omitempty"`
	Finalized           bool                   `json:"finalized,omitempty"`
	FinalizationPending bool                   `json:"finalizationPending,omitempty"`
	ScanSummary         *model.TaskScanSummary `json:"scanSummary,omitempty"`
}

// ==================== SubTask Handler ====================

// WorkerSubTaskDoneHandler 子任务进度接口
// POST /api/v1/worker/task/subtask/done
func WorkerSubTaskDoneHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerSubTaskDoneReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerSubTaskDoneResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if req.TaskId == "" || req.MainTaskId == "" || req.LeaseToken == "" {
			httpx.OkJson(w, &WorkerSubTaskDoneResp{Code: 400, Msg: "taskId、mainTaskId和leaseToken不能为空"})
			return
		}

		result, err := svcCtx.IncrSubTaskDone(r.Context(), req.TaskId, req.MainTaskId, req.LeaseToken, req.Phase, req.IsCompleted, req.IncrAmount, req.PhaseResult, req.TaskSummary)
		if err != nil {
			logx.Errorf("[WorkerSubTaskDone] IncrSubTaskDone error: %v", err)
			if errors.Is(err, scheduler.ErrTaskOperationBusy) {
				httpx.OkJson(w, &WorkerSubTaskDoneResp{
					Code:    http.StatusTooEarly,
					Msg:     "task operation busy; retry",
					Success: false,
				})
				return
			}
			if errors.Is(err, scheduler.ErrTaskParentFenced) {
				httpx.OkJson(w, &WorkerSubTaskDoneResp{
					Code:    http.StatusLocked,
					Msg:     "task is fenced by a parent control; await exact control",
					Success: false,
				})
				return
			}
			if errors.Is(err, scheduler.ErrTaskLeaseConflict) {
				httpx.OkJson(w, &WorkerSubTaskDoneResp{
					Code:    http.StatusConflict,
					Msg:     "task lease conflict",
					Success: false,
				})
				return
			}
			response.Error(w, err)
			return
		}

		httpx.OkJson(w, &WorkerSubTaskDoneResp{
			Code:                0,
			Msg:                 result.Message,
			Success:             result.Success,
			SubTaskDone:         result.SubTaskDone,
			SubTaskCount:        result.SubTaskCount,
			AllDone:             result.AllDone,
			Recorded:            result.Recorded,
			LeaseClosed:         result.LeaseClosed,
			Finalized:           result.Finalized,
			FinalizationPending: result.FinalizationPending,
			ScanSummary:         result.ScanSummary,
		})
	}
}
