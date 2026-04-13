package logic

import (
	"context"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type TaskRecoveryStatsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskRecoveryStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TaskRecoveryStatsLogic {
	return &TaskRecoveryStatsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetTaskRecoveryStats 获取任务恢复统计信息
func (l *TaskRecoveryStatsLogic) GetTaskRecoveryStats() (resp *types.TaskRecoveryStatsResp, err error) {
	// 通过 RPC 调用获取恢复统计信息
	// 这里简化处理，直接从 Redis 获取

	processingCount, _ := l.svcCtx.RedisClient.SCard(l.ctx, "cscan:task:processing").Result()

	// 获取所有 Worker 状态
	workersKey := "cscan:workers"
	workers, _ := l.svcCtx.RedisClient.SMembers(l.ctx, workersKey).Result()

	onlineWorkers := 0
	for _, worker := range workers {
		workerKey := "cscan:worker:" + worker
		exists, _ := l.svcCtx.RedisClient.Exists(l.ctx, workerKey).Result()
		if exists > 0 {
			onlineWorkers++
		}
	}

	return &types.TaskRecoveryStatsResp{
		ProcessingTasks: int(processingCount),
		OnlineWorkers:   onlineWorkers,
		TotalWorkers:    len(workers),
		CheckInterval:   "30s",
		TaskTimeout:     "10m",
	}, nil
}
