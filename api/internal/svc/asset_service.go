package svc

import (
	"context"
	"cscan/model"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// AssetService provides asset-related operations with scan result integration
type AssetService struct {
	db                *mongo.Database
	scanResultService *ScanResultService
}

// NewAssetService creates a new AssetService
func NewAssetService(db *mongo.Database) *AssetService {
	return &AssetService{
		db:                db,
		scanResultService: NewScanResultService(db),
	}
}

// ==================== Request/Response Types ====================

// GetAssetListReq represents a request to get asset list
type GetAssetListReq struct {
	WorkspaceId string
	Filter      bson.M
	Page        int
	PageSize    int
	SortField   string
}

// GetAssetListResp represents the response with assets and scan summaries
type GetAssetListResp struct {
	Assets []AssetWithScanSummary
	Total  int64
}

// AssetWithScanSummary represents an asset with aggregated scan result information
type AssetWithScanSummary struct {
	Asset             model.Asset
	DirScanCount      int64
	VulnScanCount     int64
	HighRiskVulnCount int64
	LastScanTime      time.Time
}

// ==================== Service Methods ====================

// GetAssetList retrieves assets with scan result summaries
// This method integrates with ScanResultService to provide enriched asset data
// including directory scan counts, vulnerability scan counts, and high-risk vulnerability counts
func (s *AssetService) GetAssetList(ctx context.Context, req *GetAssetListReq) (*GetAssetListResp, error) {
	assetModel := model.NewAssetModel(s.db, req.WorkspaceId)

	// Set default values
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.SortField == "" {
		req.SortField = "update_time"
	}
	if req.Filter == nil {
		req.Filter = bson.M{}
	}

	// Count total assets
	total, err := assetModel.Count(ctx, req.Filter)
	if err != nil {
		return nil, err
	}

	// Fetch assets with pagination
	assets, err := assetModel.FindWithSort(ctx, req.Filter, req.Page, req.PageSize, req.SortField)
	if err != nil {
		return nil, err
	}

	// If no assets found, return empty result
	if len(assets) == 0 {
		return &GetAssetListResp{
			Assets: []AssetWithScanSummary{},
			Total:  total,
		}, nil
	}

	// Collect asset IDs for batch query optimization
	assetIds := make([]string, len(assets))
	for i, asset := range assets {
		assetIds[i] = asset.Id.Hex()
	}

	// Fetch scan result summaries for all assets in one batch call
	summaryReq := &GetScanResultSummaryReq{
		WorkspaceId: req.WorkspaceId,
		AssetIds:    assetIds,
	}
	summaryResp, err := s.scanResultService.GetScanResultSummary(ctx, summaryReq)
	if err != nil {
		// If summary fetch fails, log error but continue with empty summaries
		// This ensures the asset list is still returned even if scan results are unavailable
		summaryResp = &GetScanResultSummaryResp{
			Summaries: make(map[string]ScanResultSummary),
		}
	}

	// Combine assets with their scan summaries
	assetsWithSummaries := make([]AssetWithScanSummary, len(assets))
	for i, asset := range assets {
		assetId := asset.Id.Hex()
		summary, exists := summaryResp.Summaries[assetId]

		assetsWithSummaries[i] = AssetWithScanSummary{
			Asset:             asset,
			DirScanCount:      0,
			VulnScanCount:     0,
			HighRiskVulnCount: 0,
			LastScanTime:      time.Time{},
		}

		// If summary exists, populate the scan counts
		if exists {
			assetsWithSummaries[i].DirScanCount = summary.DirScanCount
			assetsWithSummaries[i].VulnScanCount = summary.VulnScanCount
			assetsWithSummaries[i].HighRiskVulnCount = summary.HighRiskCount
			assetsWithSummaries[i].LastScanTime = summary.LastScanTime
		}
	}

	return &GetAssetListResp{
		Assets: assetsWithSummaries,
		Total:  total,
	}, nil
}
