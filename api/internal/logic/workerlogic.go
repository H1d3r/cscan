package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	workerOnlineThreshold = 45 * time.Second
)

type WorkerListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewWorkerListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WorkerListLogic {
	return &WorkerListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

type WorkerStatus struct {
	WorkerName         string          `json:"workerName"`
	IP                 string          `json:"ip"`
	CPULoad            float64         `json:"cpuLoad"`
	MemUsed            float64         `json:"memUsed"`
	TaskStartedNumber  int             `json:"taskStartedNumber"`
	TaskExecutedNumber int             `json:"taskExecutedNumber"`
	Concurrency        int             `json:"concurrency"`
	RunningTasks       int             `json:"runningTasks"`
	UpdateTime         string          `json:"updateTime"`
	Tools              map[string]bool `json:"tools"`
}

func (l *WorkerListLogic) WorkerList() (resp *types.WorkerListResp, err error) {
	rdb := l.svcCtx.RedisClient

	// 异步通知所有Worker上报最新状态（fire-and-forget，不阻塞响应）
	go rdb.Publish(context.Background(), "cscan:worker:query", "refresh")

	// 直接从Redis获取Worker状态（使用 SCAN 避免 KEYS 阻塞 Redis 单线程）
	var keys []string
	var cursor uint64
	for {
		batch, nextCursor, err := rdb.Scan(l.ctx, cursor, "cscan:worker:*", 100).Result()
		if err != nil {
			return &types.WorkerListResp{Code: 500, Msg: "查询失败"}, nil
		}
		keys = append(keys, batch...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	list := make([]types.Worker, 0, len(keys))
	for _, key := range keys {
		// 跳过非Worker状态的键（如 cscan:worker:control:*, cscan:worker:install_key 等）
		if key == "cscan:worker:install_key" ||
			strings.Contains(key, ":control:") ||
			strings.Contains(key, ":register:") {
			continue
		}

		data, err := rdb.Get(l.ctx, key).Result()
		if err != nil {
			continue
		}

		var status WorkerStatus
		if err := json.Unmarshal([]byte(data), &status); err != nil {
			continue
		}

		// 如果 WorkerName 为空，跳过
		if status.WorkerName == "" {
			continue
		}

		// 根据最后更新时间判断在线状态
		// 心跳间隔30秒，如果45秒内有更新则认为在线
		workerStatus := "offline"
		if status.UpdateTime != "" {
			loc := time.Local
			updateTime, err := time.ParseInLocation("2006-01-02 15:04:05", status.UpdateTime, loc)
			if err == nil {
				elapsed := time.Since(updateTime)
				l.Logger.Infof("Worker %s: updateTime=%s, elapsed=%v", status.WorkerName, status.UpdateTime, elapsed)
				if elapsed < workerOnlineThreshold {
					workerStatus = "running"
				}
			} else {
				l.Logger.Errorf("Parse time error for worker %s: %v, updateTime=%s", status.WorkerName, err, status.UpdateTime)
			}
		} else {
			l.Logger.Infof("Worker %s has empty updateTime", status.WorkerName)
		}

		// 计算正在执行的任务数
		runningCount := status.TaskStartedNumber - status.TaskExecutedNumber
		if runningCount < 0 {
			runningCount = 0
		}

		// 计算健康状态
		healthStatus := "healthy"
		if status.CPULoad > 85 || status.MemUsed > 90 {
			healthStatus = "overloaded"
		} else if status.CPULoad > 70 || status.MemUsed > 75 {
			healthStatus = "warning"
		}

		list = append(list, types.Worker{
			Name:         status.WorkerName,
			IP:           status.IP,
			CPULoad:      status.CPULoad,
			MemUsed:      status.MemUsed,
			TaskCount:    status.TaskExecutedNumber,
			RunningCount: runningCount,
			Concurrency:  status.Concurrency,
			Status:       workerStatus,
			UpdateTime:   status.UpdateTime,
			Tools:        status.Tools,
			HealthStatus: healthStatus,
		})
	}

	return &types.WorkerListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

// WorkerRenameLogic Worker重命名逻辑
type WorkerRenameLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewWorkerRenameLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WorkerRenameLogic {
	return &WorkerRenameLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *WorkerRenameLogic) WorkerRename(req *types.WorkerRenameReq) (resp *types.WorkerRenameResp, err error) {
	if req.OldName == "" || req.NewName == "" {
		return &types.WorkerRenameResp{Code: 400, Msg: "Worker名称不能为空"}, nil
	}

	if req.OldName == req.NewName {
		return &types.WorkerRenameResp{Code: 400, Msg: "新旧名称相同"}, nil
	}

	rdb := l.svcCtx.RedisClient

	// 1. 获取原Worker状态数据
	oldKey := fmt.Sprintf("cscan:worker:%s", req.OldName)
	data, err := rdb.Get(l.ctx, oldKey).Result()
	if err != nil {
		return &types.WorkerRenameResp{Code: 404, Msg: "Worker不存在"}, nil
	}

	// 2. 检查新名称是否已存在
	newKey := fmt.Sprintf("cscan:worker:%s", req.NewName)
	exists, _ := rdb.Exists(l.ctx, newKey).Result()
	if exists > 0 {
		return &types.WorkerRenameResp{Code: 400, Msg: "新名称已被使用"}, nil
	}

	// 3. 更新状态数据中的workerName
	var status map[string]interface{}
	if err := json.Unmarshal([]byte(data), &status); err != nil {
		return &types.WorkerRenameResp{Code: 500, Msg: "数据解析失败"}, nil
	}
	status["workerName"] = req.NewName

	// 4. 保存到新key
	newData, _ := json.Marshal(status)
	rdb.Set(l.ctx, newKey, newData, 10*time.Minute)

	// 5. 删除旧key
	rdb.Del(l.ctx, oldKey)

	// 6. 更新Worker集合
	rdb.SRem(l.ctx, "cscan:workers", req.OldName)
	rdb.SAdd(l.ctx, "cscan:workers", req.NewName)

	// 7. 发送重命名命令给Worker（让Worker更新自己的名称）
	renameMsg, _ := json.Marshal(map[string]interface{}{"action": "rename", "workerName": req.OldName, "newName": req.NewName})
	rdb.Publish(l.ctx, "cscan:worker:control", renameMsg)

	l.Logger.Infof("[WorkerRename] Renamed worker from %s to %s", req.OldName, req.NewName)

	return &types.WorkerRenameResp{Code: 0, Msg: "重命名成功"}, nil
}

// WorkerRestartLogic Worker重启逻辑
type WorkerRestartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewWorkerRestartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WorkerRestartLogic {
	return &WorkerRestartLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *WorkerRestartLogic) WorkerRestart(req *types.WorkerRestartReq) (resp *types.WorkerRestartResp, err error) {
	if req.Name == "" {
		return &types.WorkerRestartResp{Code: 400, Msg: "Worker名称不能为空"}, nil
	}

	rdb := l.svcCtx.RedisClient

	// 检查Worker是否存在
	workerKey := fmt.Sprintf("cscan:worker:%s", req.Name)
	_, err = rdb.Get(l.ctx, workerKey).Result()
	if err != nil {
		return &types.WorkerRestartResp{Code: 404, Msg: "Worker不存在或已离线"}, nil
	}

	// 1. 设置重启控制命令到 Redis（Worker 心跳时会读取）
	ctrlKey := fmt.Sprintf("cscan:worker:control:%s", req.Name)
	controlData := map[string]bool{"reload": true}
	controlJson, _ := json.Marshal(controlData)
	rdb.Set(l.ctx, ctrlKey, controlJson, 5*time.Minute) // 5分钟过期
	l.Logger.Infof("[WorkerRestart] Set reload command for worker: %s", req.Name)

	// 2. 同时通过 WebSocket 发送重启命令（如果 Worker 已连接）
	restartMsg := fmt.Sprintf(`{"action":"restart","workerName":"%s"}`, req.Name)
	rdb.Publish(l.ctx, "cscan:worker:control", restartMsg)

	// 3. 删除Redis中的Worker状态数据，让Worker重启后重新注册
	rdb.Del(l.ctx, workerKey)
	l.Logger.Infof("[WorkerRestart] Deleted worker data: %s", req.Name)

	return &types.WorkerRestartResp{Code: 0, Msg: "重启命令已发送"}, nil
}

// WorkerSetConcurrencyLogic Worker设置并发数逻辑
type WorkerSetConcurrencyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewWorkerSetConcurrencyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WorkerSetConcurrencyLogic {
	return &WorkerSetConcurrencyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *WorkerSetConcurrencyLogic) WorkerSetConcurrency(req *types.WorkerSetConcurrencyReq) (resp *types.WorkerSetConcurrencyResp, err error) {
	if req.Name == "" {
		return &types.WorkerSetConcurrencyResp{Code: 400, Msg: "Worker名称不能为空"}, nil
	}

	if req.Concurrency < 1 || req.Concurrency > 100 {
		return &types.WorkerSetConcurrencyResp{Code: 400, Msg: "并发数必须在1-100之间"}, nil
	}

	rdb := l.svcCtx.RedisClient

	// 检查Worker是否存在
	workerKey := fmt.Sprintf("cscan:worker:%s", req.Name)
	_, err = rdb.Get(l.ctx, workerKey).Result()
	if err != nil {
		return &types.WorkerSetConcurrencyResp{Code: 404, Msg: "Worker不存在或已离线"}, nil
	}

	// 持久化期望并发数（无TTL），Worker重启后通过心跳响应恢复
	desiredKey := fmt.Sprintf("cscan:worker:desired_concurrency:%s", req.Name)
	if err := rdb.Set(l.ctx, desiredKey, req.Concurrency, 0).Err(); err != nil {
		l.Logger.Errorf("[WorkerSetConcurrency] Failed to persist desired concurrency: %v", err)
		return &types.WorkerSetConcurrencyResp{Code: 500, Msg: "保存并发数配置失败"}, nil
	}

	// 通过Pub/Sub发送设置并发数命令
	setConcurrencyMsg := fmt.Sprintf(`{"action":"setConcurrency","workerName":"%s","concurrency":%d}`, req.Name, req.Concurrency)
	rdb.Publish(l.ctx, "cscan:worker:control", setConcurrencyMsg)
	l.Logger.Infof("[WorkerSetConcurrency] Sent setConcurrency command to worker: %s, concurrency: %d", req.Name, req.Concurrency)

	return &types.WorkerSetConcurrencyResp{Code: 0, Msg: "设置命令已发送"}, nil
}
