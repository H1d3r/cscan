package logic

import (
	"context"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"go.mongodb.org/mongo-driver/bson"
)

type ActiveFingerprintListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewActiveFingerprintListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActiveFingerprintListLogic {
	return &ActiveFingerprintListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActiveFingerprintListLogic) ActiveFingerprintList(req *types.ActiveFingerprintListReq) (*types.ActiveFingerprintListResp, error) {
	// 构建查询条件
	filter := bson.M{}
	if req.Keyword != "" {
		filter["name"] = bson.M{"$regex": req.Keyword, "$options": "i"}
	}
	if req.Enabled != nil {
		filter["enabled"] = *req.Enabled
	}

	// 查询总数
	total, err := l.svcCtx.ActiveFingerprintModel.Count(l.ctx, filter)
	if err != nil {
		return nil, err
	}

	// 查询列表
	docs, err := l.svcCtx.ActiveFingerprintModel.Find(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}

	// 转换为响应类型，并查询关联的被动指纹
	list := make([]types.ActiveFingerprint, 0, len(docs))
	for _, doc := range docs {
		item := types.ActiveFingerprint{
			Id:          doc.Id.Hex(),
			Name:        doc.Name,
			Paths:       doc.Paths,
			Description: doc.Description,
			Enabled:     doc.Enabled,
			CreateTime:  doc.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:  doc.UpdateTime.Format("2006-01-02 15:04:05"),
		}

		// 查询关联的被动指纹（按名称匹配）
		relatedFilter := bson.M{"name": doc.Name}
		relatedDocs, _ := l.svcCtx.FingerprintModel.Find(l.ctx, relatedFilter, 0, 0)
		item.RelatedCount = len(relatedDocs)

		// 转换关联指纹
		relatedFingerprints := make([]types.Fingerprint, 0, len(relatedDocs))
		for _, rf := range relatedDocs {
			relatedFingerprints = append(relatedFingerprints, types.Fingerprint{
				Id:          rf.Id.Hex(),
				Name:        rf.Name,
				Website:     rf.Website,
				Description: rf.Description,
				Rule:        rf.Rule,
				Source:      rf.Source,
				IsBuiltin:   rf.IsBuiltin,
				Enabled:     rf.Enabled,
				CreateTime:  rf.CreateTime.Format("2006-01-02 15:04:05"),
			})
		}
		item.RelatedFingerprints = relatedFingerprints

		list = append(list, item)
	}

	// 获取统计信息
	stats, _ := l.svcCtx.ActiveFingerprintModel.GetStats(l.ctx)

	return &types.ActiveFingerprintListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
		Stats: stats,
	}, nil
}
