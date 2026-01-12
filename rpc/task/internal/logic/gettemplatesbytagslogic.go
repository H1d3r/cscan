package logic

import (
	"context"

	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type GetTemplatesByTagsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTemplatesByTagsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTemplatesByTagsLogic {
	return &GetTemplatesByTagsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 根据标签获取模板
func (l *GetTemplatesByTagsLogic) GetTemplatesByTags(in *pb.GetTemplatesByTagsReq) (*pb.GetTemplatesByTagsResp, error) {
	var templates []string

	// 如果是只获取自定义POC模式
	if in.CustomPocOnly {
		// 构建自定义POC查询条件
		filter := bson.M{"enabled": true}
		if len(in.Severities) > 0 {
			filter["severity"] = bson.M{"$in": in.Severities}
		}

		// 使用FindWithFilter，传入0表示不分页
		customPocs, err := l.svcCtx.CustomPocModel.FindWithFilter(l.ctx, filter, 0, 0)
		if err != nil {
			l.Logger.Errorf("FindWithFilter for custom pocs failed: %v", err)
			return &pb.GetTemplatesByTagsResp{
				Success: false,
				Message: "获取自定义POC失败: " + err.Error(),
			}, nil
		}
		for _, p := range customPocs {
			if p.Content != "" {
				templates = append(templates, p.Content)
			}
		}

		l.Logger.Infof("CustomPocOnly mode: found %d custom POCs", len(templates))
		return &pb.GetTemplatesByTagsResp{
			Success:   true,
			Message:   "success",
			Templates: templates,
			Count:     int32(len(templates)),
		}, nil
	}

	// 构建查询条件
	filter := bson.M{"enabled": true}

	// 标签条件
	if len(in.Tags) > 0 {
		filter["tags"] = bson.M{"$in": in.Tags}
	}

	// 严重级别条件
	if len(in.Severities) > 0 {
		filter["severity"] = bson.M{"$in": in.Severities}
	}

	// 获取Nuclei默认模板
	nucleiTemplates, err := l.svcCtx.NucleiTemplateModel.FindEnabledByFilter(l.ctx, filter)
	if err != nil {
		l.Logger.Errorf("FindEnabledByFilter for nuclei templates failed: %v", err)
		return &pb.GetTemplatesByTagsResp{
			Success: false,
			Message: "获取Nuclei模板失败: " + err.Error(),
		}, nil
	}
	for _, t := range nucleiTemplates {
		if t.Content != "" {
			templates = append(templates, t.Content)
		}
	}

	// 获取自定义POC（按标签）
	if len(in.Tags) > 0 {
		customPocs, err := l.svcCtx.CustomPocModel.FindByTags(l.ctx, in.Tags)
		if err != nil {
			l.Logger.Errorf("FindByTags for custom pocs failed: %v", err)
			// 不返回错误，继续使用已获取的模板
		} else {
			for _, p := range customPocs {
				if p.Content != "" {
					templates = append(templates, p.Content)
				}
			}
		}
	}

	return &pb.GetTemplatesByTagsResp{
		Success:   true,
		Message:   "success",
		Templates: templates,
		Count:     int32(len(templates)),
	}, nil
}
