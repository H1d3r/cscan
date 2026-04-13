package logic

import (
	"context"
	"encoding/json"
	"fmt"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
)

// ChunkProgressLogic 分片进度查询逻辑
type ChunkProgressLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewChunkProgressLogic 创建分片进度查询逻辑
func NewChunkProgressLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChunkProgressLogic {
	return &ChunkProgressLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ChunkProgress 获取任务分片进度
func (l *ChunkProgressLogic) ChunkProgress(req *types.ChunkProgressReq) (resp *types.ChunkProgressResp, err error) {
	if req.TaskId == "" {
		return &types.ChunkProgressResp{
			Code: 400,
			Msg:  "任务ID不能为空",
		}, nil
	}

	// 创建分片管理器
	chunkManager := scheduler.NewChunkManager(l.svcCtx.RedisClient, nil)

	// 获取分片进度
	progress, err := chunkManager.GetChunkProgress(l.ctx, req.TaskId)
	if err != nil {
		l.Logger.Errorf("Failed to get chunk progress for task %s: %v", req.TaskId, err)
		return &types.ChunkProgressResp{
			Code: 500,
			Msg:  fmt.Sprintf("获取分片进度失败: %v", err),
		}, nil
	}

	// 转换为响应格式
	chunks := make([]types.ChunkStatus, 0, len(progress.Chunks))
	for _, chunk := range progress.Chunks {
		chunks = append(chunks, types.ChunkStatus{
			ChunkId:     chunk.ChunkId,
			Status:      chunk.Status,
			StartTime:   chunk.StartTime.Format("2006-01-02 15:04:05"),
			EndTime:     chunk.EndTime.Format("2006-01-02 15:04:05"),
			Duration:    chunk.Duration,
			TargetCount: chunk.TargetCount,
			AssetCount:  chunk.AssetCount,
			VulCount:    chunk.VulCount,
			ErrorMsg:    chunk.ErrorMsg,
			WorkerName:  chunk.WorkerName,
		})
	}

	return &types.ChunkProgressResp{
		Code:            0,
		Msg:             "获取成功",
		TaskId:          progress.TaskId,
		TotalChunks:     progress.TotalChunks,
		CompletedChunks: progress.CompletedChunks,
		FailedChunks:    progress.FailedChunks,
		RunningChunks:   progress.RunningChunks,
		TotalTargets:    progress.TotalTargets,
		CompletionRate:  progress.CompletionRate,
		Chunks:          chunks,
	}, nil
}

// ChunkPreviewLogic 分片预览逻辑
type ChunkPreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewChunkPreviewLogic 创建分片预览逻辑
func NewChunkPreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChunkPreviewLogic {
	return &ChunkPreviewLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ChunkPreview 获取任务分片预览
func (l *ChunkPreviewLogic) ChunkPreview(req *types.ChunkPreviewReq) (resp *types.ChunkPreviewResp, err error) {
	if req.Target == "" {
		return &types.ChunkPreviewResp{
			Code: 400,
			Msg:  "目标不能为空",
		}, nil
	}

	// 解析任务配置
	var taskConfig map[string]interface{}
	if req.Config != "" {
		if err := json.Unmarshal([]byte(req.Config), &taskConfig); err != nil {
			return &types.ChunkPreviewResp{
				Code: 400,
				Msg:  "任务配置格式错误",
			}, nil
		}
	} else {
		taskConfig = make(map[string]interface{})
	}

	// 创建分片配置
	chunkConfig := scheduler.DefaultChunkConfig()
	if req.MaxTargetsPerChunk > 0 {
		chunkConfig.MaxTargetsPerChunk = req.MaxTargetsPerChunk
	}
	if req.MinChunkSize > 0 {
		chunkConfig.MinChunkSize = req.MinChunkSize
	}
	if req.MaxChunkSize > 0 {
		chunkConfig.MaxChunkSize = req.MaxChunkSize
	}

	// 验证分片配置
	if err := scheduler.ValidateChunkConfig(chunkConfig); err != nil {
		return &types.ChunkPreviewResp{
			Code: 400,
			Msg:  fmt.Sprintf("分片配置无效: %v", err),
		}, nil
	}

	// 创建分片管理器
	chunkManager := scheduler.NewChunkManager(l.svcCtx.RedisClient, chunkConfig)

	// 获取分片预览
	preview, err := chunkManager.GetSplitPreview(req.Target, taskConfig)
	if err != nil {
		l.Logger.Errorf("Failed to get chunk preview: %v", err)
		return &types.ChunkPreviewResp{
			Code: 500,
			Msg:  fmt.Sprintf("获取分片预览失败: %v", err),
		}, nil
	}

	return &types.ChunkPreviewResp{
		Code:             0,
		Msg:              "获取成功",
		TotalTargets:     preview.TotalTargets,
		ChunkCount:       preview.ChunkCount,
		ChunkSize:        preview.ChunkSize,
		NeedSplit:        preview.NeedSplit,
		EstimatedTime:    preview.EstimatedTime,
		RecommendedSize:  preview.RecommendedSize,
		MaxMemoryUsage:   preview.MaxMemoryUsage,
		ParallelCapacity: preview.ParallelCapacity,
	}, nil
}
