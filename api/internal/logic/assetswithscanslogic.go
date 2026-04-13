package logic

import (
	"context"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"go.mongodb.org/mongo-driver/bson"
)

type AssetsWithScansLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetsWithScansLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetsWithScansLogic {
	return &AssetsWithScansLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetsWithScans retrieves assets with scan result summaries
func (l *AssetsWithScansLogic) AssetsWithScans(req *types.AssetsWithScansReq, workspaceId string) (*types.AssetsWithScansResp, error) {
	// Build filter based on request parameters
	filter := bson.M{}
	if req.Query != "" {
		filter["$or"] = []bson.M{
			{"host": bson.M{"$regex": req.Query, "$options": "i"}},
			{"title": bson.M{"$regex": req.Query, "$options": "i"}},
		}
	}
	if req.Host != "" {
		filter["host"] = bson.M{"$regex": req.Host, "$options": "i"}
	}
	if req.Port > 0 {
		filter["port"] = req.Port
	}
	if req.Service != "" {
		filter["service"] = bson.M{"$regex": req.Service, "$options": "i"}
	}

	// Create asset service and fetch assets with scan summaries
	assetService := svc.NewAssetService(l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName))

	assetReq := &svc.GetAssetListReq{
		WorkspaceId: workspaceId,
		Filter:      filter,
		Page:        req.Page,
		PageSize:    req.PageSize,
		SortField:   "update_time",
	}

	assetResp, err := assetService.GetAssetList(l.ctx, assetReq)
	if err != nil {
		return nil, err
	}

	// Convert to response format
	list := make([]types.AssetWithScans, len(assetResp.Assets))
	for i, assetWithSummary := range assetResp.Assets {
		// Convert model.Asset to types.Asset
		asset := types.Asset{
			Id:         assetWithSummary.Asset.Id.Hex(),
			Authority:  assetWithSummary.Asset.Authority,
			Host:       assetWithSummary.Asset.Host,
			Port:       assetWithSummary.Asset.Port,
			Service:    assetWithSummary.Asset.Service,
			Title:      assetWithSummary.Asset.Title,
			App:        assetWithSummary.Asset.App,
			Screenshot: assetWithSummary.Asset.Screenshot,
			CreateTime: assetWithSummary.Asset.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime: assetWithSummary.Asset.UpdateTime.Format("2006-01-02 15:04:05"),
		}

		// Format last scan time
		lastScanTime := ""
		if !assetWithSummary.LastScanTime.IsZero() {
			lastScanTime = assetWithSummary.LastScanTime.Format("2006-01-02 15:04:05")
		}

		list[i] = types.AssetWithScans{
			Asset:             asset,
			DirScanCount:      assetWithSummary.DirScanCount,
			VulnScanCount:     assetWithSummary.VulnScanCount,
			HighRiskVulnCount: assetWithSummary.HighRiskVulnCount,
			LastScanTime:      lastScanTime,
		}
	}

	return &types.AssetsWithScansResp{
		Code:  0,
		Msg:   "success",
		Total: assetResp.Total,
		List:  list,
	}, nil
}
