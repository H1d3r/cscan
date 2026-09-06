package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// ==================== Heartbeat Types ====================

// WorkerHeartbeatReq 心跳请求
type WorkerHeartbeatReq struct {
	WorkerName         string  `json:"workerName"`
	InstanceID         string  `json:"instanceId"`
	TaskProtocol       int     `json:"taskProtocol"`
	IP                 string  `json:"ip"`
	CpuLoad            float64 `json:"cpuLoad"`
	MemUsed            float64 `json:"memUsed"`
	TaskStartedNumber  int32   `json:"taskStartedNumber"`
	TaskExecutedNumber int32   `json:"taskExecutedNumber"`
	Concurrency        int     `json:"concurrency"`
	IsDaemon           bool    `json:"isDaemon"`
}

// WorkerHeartbeatResp 心跳响应
type WorkerHeartbeatResp struct {
	Code               int    `json:"code"`
	Msg                string `json:"msg"`
	Status             string `json:"status"`
	ManualStopFlag     bool   `json:"manualStopFlag"`
	ManualReloadFlag   bool   `json:"manualReloadFlag"`
	ManualInitEnvFlag  bool   `json:"manualInitEnvFlag"`
	ManualSyncFlag     bool   `json:"manualSyncFlag"`
	DesiredConcurrency int    `json:"desiredConcurrency,omitempty"`
}

// ==================== Heartbeat Handler ====================

// WorkerHeartbeatHandler 心跳接口
// POST /api/v1/worker/heartbeat
func WorkerHeartbeatHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerHeartbeatReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerHeartbeatResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if req.WorkerName == "" {
			httpx.OkJson(w, &WorkerHeartbeatResp{Code: 400, Msg: "workerName不能为空"})
			return
		}
		v1Identity := validWorkerTaskIdentity(req.InstanceID, req.TaskProtocol)
		if !v1Identity && (req.InstanceID != "" || req.TaskProtocol != 0) {
			httpx.OkJson(w, &WorkerHeartbeatResp{Code: http.StatusUpgradeRequired, Msg: "leased-task-v1 instance identity required"})
			return
		}

		ctx := r.Context()

		// Retain the logical-name heartbeat for UI compatibility and publish the
		// immutable instance key used for execution ownership/recovery.
		workerKey := "cscan:worker:" + req.WorkerName
		instanceKey := "cscan:worker:instance:" + req.InstanceID
		workerData := map[string]interface{}{
			"workerName":         req.WorkerName,
			"instanceId":         req.InstanceID,
			"taskProtocol":       req.TaskProtocol,
			"ip":                 req.IP,
			"cpuLoad":            req.CpuLoad,
			"memUsed":            req.MemUsed,
			"taskStartedNumber":  req.TaskStartedNumber,
			"taskExecutedNumber": req.TaskExecutedNumber,
			"concurrency":        req.Concurrency,
			"isDaemon":           req.IsDaemon,
			"updateTime":         time.Now().Format("2006-01-02 15:04:05"),
			"status":             "online",
		}
		workerJSON, err := json.Marshal(workerData)
		if err != nil {
			httpx.OkJson(w, &WorkerHeartbeatResp{Code: 500, Msg: "心跳序列化失败"})
			return
		}
		pipe := svcCtx.RedisClient.TxPipeline()
		pipe.Set(ctx, workerKey, workerJSON, 60*time.Second)
		if v1Identity {
			pipe.Set(ctx, instanceKey, workerJSON, 60*time.Second)
		}
		pipe.SAdd(ctx, "cscan:workers", req.WorkerName)
		if _, err := pipe.Exec(ctx); err != nil {
			httpx.OkJson(w, &WorkerHeartbeatResp{Code: 500, Msg: "心跳写入失败"})
			return
		}

		// 检查控制命令
		controlKey := "cscan:worker:control:" + req.WorkerName
		controlData, err := svcCtx.RedisClient.Get(ctx, controlKey).Result()

		var manualStop, manualReload, manualInitEnv, manualSync bool
		if err == nil && controlData != "" {
			var control map[string]bool
			if json.Unmarshal([]byte(controlData), &control) == nil {
				manualStop = control["stop"]
				manualReload = control["reload"]
				manualInitEnv = control["initEnv"]
				manualSync = control["sync"]
			}
			svcCtx.RedisClient.Del(ctx, controlKey)
		}

		// 读取期望并发数
		desiredConcurrency := 0
		desiredKey := fmt.Sprintf("cscan:worker:desired_concurrency:%s", req.WorkerName)
		if val, err := svcCtx.RedisClient.Get(ctx, desiredKey).Int(); err == nil && val > 0 {
			desiredConcurrency = val
		}

		httpx.OkJson(w, &WorkerHeartbeatResp{
			Code:               0,
			Msg:                "success",
			Status:             "ok",
			ManualStopFlag:     manualStop,
			ManualReloadFlag:   manualReload,
			ManualInitEnvFlag:  manualInitEnv,
			ManualSyncFlag:     manualSync,
			DesiredConcurrency: desiredConcurrency,
		})
	}
}

// ==================== Offline Types ====================

// WorkerOfflineReq Worker离线通知请求
type WorkerOfflineReq struct {
	WorkerName   string `json:"workerName"`
	InstanceID   string `json:"instanceId"`
	TaskProtocol int    `json:"taskProtocol"`
}

// WorkerOfflineResp Worker离线通知响应
type WorkerOfflineResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// ==================== Offline Handler ====================

var compareDeleteWorkerHeartbeatScript = redis.NewScript(`
	local function ownedByInstance(value)
		if not value then
			return false
		end
		local decoded = nil
		pcall(function() decoded = cjson.decode(value) end)
		return decoded and (decoded.instanceId or '') == ARGV[1] and (decoded.workerName or '') == ARGV[2]
	end

	local instanceValue = redis.call('GET', KEYS[1])
	local offlineProven = 0
	if not instanceValue then
		offlineProven = 1
	elseif ownedByInstance(instanceValue) then
		redis.call('DEL', KEYS[1])
		offlineProven = 1
	end

	if ownedByInstance(redis.call('GET', KEYS[2])) then
		redis.call('DEL', KEYS[2])
		redis.call('SREM', KEYS[3], ARGV[2])
		redis.call('DEL', KEYS[4])
	end
	return offlineProven
`)

// WorkerOfflineHandler Worker离线通知接口
// POST /api/v1/worker/offline
// Worker停止时调用此接口，立即删除Redis中的状态数据
func WorkerOfflineHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WorkerOfflineReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &WorkerOfflineResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if req.WorkerName == "" {
			httpx.OkJson(w, &WorkerOfflineResp{Code: 400, Msg: "workerName不能为空"})
			return
		}
		if !validWorkerTaskIdentity(req.InstanceID, req.TaskProtocol) {
			httpx.OkJson(w, &WorkerOfflineResp{Code: http.StatusUpgradeRequired, Msg: "leased-task-v1 instance identity required"})
			return
		}

		offlineProven, err := compareDeleteWorkerHeartbeatScript.Run(r.Context(), svcCtx.RedisClient, []string{
			"cscan:worker:instance:" + req.InstanceID,
			"cscan:worker:" + req.WorkerName,
			"cscan:workers",
			"cscan:worker:control:" + req.WorkerName,
		}, req.InstanceID, req.WorkerName).Int()
		if err != nil {
			httpx.OkJson(w, &WorkerOfflineResp{Code: 500, Msg: "offline heartbeat update failed"})
			return
		}

		logx.Infof("[WorkerOffline] Worker %s instance %s offline (proven=%t)", req.WorkerName, req.InstanceID, offlineProven == 1)

		// Immediate recovery, when possible, is scoped to the exact process
		// generation whose instance heartbeat is now absent.
		if offlineProven == 1 {
			go func(workerName, instanceID string) {
				defer func() {
					if recovered := recover(); recovered != nil {
						logx.Errorf("[WorkerOffline] panic worker=%s instance=%s err=%v stack=%s", workerName, instanceID, recovered, debug.Stack())
					}
				}()
				recoverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				recoveredTasks, recoverErr := logic.RecoverWorkerInstanceTasks(recoverCtx, svcCtx, workerName, instanceID)
				if recoverErr != nil {
					logx.Errorf("[WorkerOffline] Failed to recover tasks for worker %s instance %s: %v", workerName, instanceID, recoverErr)
				} else if len(recoveredTasks) > 0 {
					logx.Infof("[WorkerOffline] Worker %s instance %s: recovered %d orphaned tasks", workerName, instanceID, len(recoveredTasks))
				}
			}(req.WorkerName, req.InstanceID)
		}

		httpx.OkJson(w, &WorkerOfflineResp{
			Code:    0,
			Msg:     "success",
			Success: true,
		})
	}
}
