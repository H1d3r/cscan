package logic

import (
	"context"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"
	"fmt"
)

type AssetVulnScansLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetVulnScansLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetVulnScansLogic {
	return &AssetVulnScansLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetVulnScans retrieves vulnerability scan results for a specific asset
func (l *AssetVulnScansLogic) AssetVulnScans(req *types.AssetVulnScansReq, workspaceId string) (*types.AssetVulnScansResp, error) {
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

	// Create scan result service and fetch vulnerability scan results
	scanResultService := svc.NewScanResultService(l.svcCtx.MongoClient.Database(l.svcCtx.Config.Mongo.DbName))

	scanReq := &svc.GetVulnScanResultsReq{
		WorkspaceId: workspaceId,
		Authority:   asset.Authority,
		Host:        asset.Host,
		Port:        asset.Port,
		Limit:       req.Limit,
		Offset:      req.Offset,
	}

	scanResp, err := scanResultService.GetVulnScanResults(l.ctx, scanReq)
	if err != nil {
		return nil, err
	}

	// Convert to response format
	results := make([]types.VulnResultItem, 0)
	for _, result := range scanResp.Results {
		// Each ScanResult can have multiple findings
		for _, finding := range result.Findings {
			vulnItem := types.VulnResultItem{
				ID:          finding.ID,
				Name:        finding.Title,
				Severity:    finding.Severity,
				URL:         "", // Not available in Finding struct
				Description: finding.Description,
				CVE:         "", // Not available in Finding struct, check metadata
				CVSS:        finding.RiskScore,
				MatchedURL:  "", // Not available in Finding struct
			}

			// Try to get CVE from metadata
			if cve, ok := finding.Metadata["cve"]; ok {
				vulnItem.CVE = cve
			}

			// Format discovered time
			if !finding.Discovered.IsZero() {
				vulnItem.DiscoveredAt = finding.Discovered.Format("2006-01-02 15:04:05")
			}

			results = append(results, vulnItem)
		}
	}

	// Format scan time
	scanTime := ""
	if !scanResp.ScanTime.IsZero() {
		scanTime = scanResp.ScanTime.Format("2006-01-02 15:04:05")
	}

	return &types.AssetVulnScansResp{
		Code:     0,
		Msg:      "success",
		Total:    scanResp.Total,
		Results:  results,
		ScanTime: scanTime,
	}, nil
}
