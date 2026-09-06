package logic

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/scheduler"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

const reverifyWorkerOnlineThreshold = 45 * time.Second

// VulReverifyLogic 单条/批量漏洞复验逻辑（T-复验闭环）
type VulReverifyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewVulReverifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VulReverifyLogic {
	return &VulReverifyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// hasOnlineWorker 检查是否有在线 Worker（心跳 45 秒内更新视为在线）
func (l *VulReverifyLogic) hasOnlineWorker() bool {
	rdb := l.svcCtx.RedisClient
	var cursor uint64
	for {
		batch, nextCursor, err := rdb.Scan(l.ctx, cursor, "cscan:worker:*", 100).Result()
		if err != nil {
			return false
		}
		for _, key := range batch {
			if key == "cscan:worker:install_key" ||
				strings.Contains(key, ":instance:") ||
				strings.Contains(key, ":control:") ||
				strings.Contains(key, ":register:") ||
				strings.Contains(key, ":desired_concurrency:") {
				continue
			}
			data, err := rdb.Get(l.ctx, key).Result()
			if err != nil {
				continue
			}
			var status struct {
				WorkerName string `json:"workerName"`
				UpdateTime string `json:"updateTime"`
			}
			if json.Unmarshal([]byte(data), &status) != nil || status.WorkerName == "" || status.UpdateTime == "" {
				continue
			}
			updateTime, err := time.ParseInLocation("2006-01-02 15:04:05", status.UpdateTime, time.Local)
			if err == nil && time.Since(updateTime) < reverifyWorkerOnlineThreshold {
				return true
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return false
}

// VulReverify 将复验任务下发到 scheduler 队列（由 worker 执行复测），并立即标记漏洞为"复验中"。
// 前端据此实时展示"复验中"，worker 复测完成后经由 /api/v1/worker/task/vul/reverify 落库结果，
// 漏洞状态与复验结论被更新，形成完整闭环。
func (l *VulReverifyLogic) VulReverify(req *types.VulReverifyReq) (*types.VulReverifyResp, error) {
	if len(req.Ids) == 0 {
		return &types.VulReverifyResp{Code: 400, Msg: "漏洞 ID 不能为空"}, nil
	}

	// 检查是否有在线 Worker
	if !l.hasOnlineWorker() {
		return &types.VulReverifyResp{Code: 400, Msg: "当前无在线 Worker 节点，请先启动 Worker 后再执行复验"}, nil
	}

	reviewer := middleware.GetUsername(l.ctx)
	if reviewer == "" {
		reviewer = "system"
	}

	vulModel := l.svcCtx.GetVulModel()

	var taskIds []string
	var reverified []string
	for _, id := range req.Ids {
		// 读取漏洞基础信息，随任务下发，worker 无需再次查询即可复测
		v, err := vulModel.FindById(l.ctx, id)
		if err != nil || v == nil {
			l.Logger.Errorf("[VulReverify] vuln %s not found (err=%v), skip", id, err)
			continue
		}

		// 先下发任务到队列；成功后再标记"复验中"，避免下发失败时前端卡在"复验中"
		taskId := uuid.New().String()
		cfg := map[string]interface{}{
			"taskType":    "vuln_reverify",
			"vulnId":      id,
			"workspaceId": "",
			"reviewer":    reviewer,
			"authority":   v.Authority,
			"host":        v.Host,
			"port":        v.Port,
			"url":         v.Url,
			"pocFile":     v.PocFile,
			"source":      v.Source,
			"severity":    v.Severity,
			"riskSource":  v.RiskSource,
		}
		cfgBytes, mErr := json.Marshal(cfg)
		if mErr != nil {
			l.Logger.Errorf("[VulReverify] marshal config for %s failed: %v", id, mErr)
			continue
		}
		task := &scheduler.TaskInfo{
			TaskId:     taskId,
			MainTaskId: taskId,
			TaskName:   "vuln_reverify",
			Config:     string(cfgBytes),
			Priority:   scheduler.PriorityHigh,
			CreateTime: time.Now().Local().Format("2006-01-02 15:04:05"),
		}
		if pErr := l.svcCtx.Scheduler.PushTask(l.ctx, task); pErr != nil {
			l.Logger.Errorf("[VulReverify] PushTask for vuln %s failed: %v", id, pErr)
			continue
		}

		// 任务已入队，标记漏洞为"复验中"（前端立即展示）
		if sErr := vulModel.MarkReverifyStarted(l.ctx, id, reviewer); sErr != nil {
			l.Logger.Errorf("[VulReverify] MarkReverifyStarted %s failed (task dispatched): %v", id, sErr)
			// 不阻断：worker 复测完成回传时仍会置为 done
		}

		taskIds = append(taskIds, taskId)
		reverified = append(reverified, id)
	}

	if len(reverified) == 0 {
		return &types.VulReverifyResp{Code: 500, Msg: "没有可复验的漏洞（ID 可能不存在）"}, nil
	}
	return &types.VulReverifyResp{
		Code:       0,
		Msg:        "已下发复验任务",
		TaskIds:    taskIds,
		Reverified: reverified,
	}, nil
}
