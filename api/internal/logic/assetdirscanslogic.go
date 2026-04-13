package logic

import (
	"context"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"
	"fmt"
)

type AssetDirScansLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetDirScansLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetDirScansLogic {
	return &AssetDirScansLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetDirScans retrieves directory scan results for a specific asset
func (l *AssetDirScansLogic) AssetDirScans(req *types.AssetDirScansReq, workspaceId string) (*types.AssetDirScansResp, error) {
	// Validate asset ID
	if req.AssetId == "" {
		return nil, fmt.Errorf("asset_id is required")
	}

	// Fetch asset to get authority, host, port
	assetModel := model.NewAssetModel(l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName), workspaceId)
	asset, err := assetModel.FindById(l.ctx, req.AssetId)
	if err != nil {
		return nil, fmt.Errorf("asset not found: %w", err)
	}

	// Create scan result service and fetch directory scan results
	scanResultService := svc.NewScanResultService(l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName))

	scanReq := &svc.GetDirScanResultsReq{
		WorkspaceId: workspaceId,
		Authority:   asset.Authority,
		Host:        asset.Host,
		Port:        asset.Port,
		Limit:       req.Limit,
		Offset:      req.Offset,
	}

	scanResp, err := scanResultService.GetDirScanResults(l.ctx, scanReq)
	if err != nil {
		return nil, err
	}

	// Convert to response format
	results := make([]types.DirScanResultItem, len(scanResp.Results))
	for i, result := range scanResp.Results {
		results[i] = types.DirScanResultItem{
			URL:           result.URL,
			Path:          result.Path,
			Status:        int(result.StatusCode),
			ContentLength: result.ContentLength,
			ContentType:   result.ContentType,
			Title:         result.Title,
			RedirectURL:   result.RedirectURL,
		}
	}

	// Format scan time
	scanTime := ""
	if !scanResp.ScanTime.IsZero() {
		scanTime = scanResp.ScanTime.Format("2006-01-02 15:04:05")
	}

	return &types.AssetDirScansResp{
		Code:     0,
		Msg:      "success",
		Total:    scanResp.Total,
		Results:  results,
		ScanTime: scanTime,
	}, nil
}
