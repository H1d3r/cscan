package svc

import (
	"context"
	"cscan/model"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ScanResultService provides unified access to scan results
type ScanResultService struct {
	db *mongo.Database
}

// NewScanResultService creates a new ScanResultService
func NewScanResultService(db *mongo.Database) *ScanResultService {
	return &ScanResultService{
		db: db,
	}
}

// ==================== Request/Response Types ====================

// GetDirScanResultsReq represents a request to get directory scan results
type GetDirScanResultsReq struct {
	WorkspaceId string
	Authority   string
	Host        string
	Port        int
	Limit       int
	Offset      int
}

// GetDirScanResultsResp represents the response with directory scan results
type GetDirScanResultsResp struct {
	Results  []model.DirScanResult
	Total    int64
	ScanTime time.Time
}

// GetVulnScanResultsReq represents a request to get vulnerability scan results
type GetVulnScanResultsReq struct {
	WorkspaceId string
	Authority   string
	Host        string
	Port        int
	Limit       int
	Offset      int
}

// GetVulnScanResultsResp represents the response with vulnerability scan results
type GetVulnScanResultsResp struct {
	Results  []model.ScanResult
	Total    int64
	ScanTime time.Time
}

// GetScanResultSummaryReq represents a request to get scan result summaries
type GetScanResultSummaryReq struct {
	WorkspaceId string
	AssetIds    []string
}

// GetScanResultSummaryResp represents the response with scan result summaries
type GetScanResultSummaryResp struct {
	Summaries map[string]ScanResultSummary
}

// ScanResultSummary represents aggregated scan result information for an asset
type ScanResultSummary struct {
	AssetId       string
	DirScanCount  int64
	VulnScanCount int64
	HighRiskCount int64
	LastScanTime  time.Time
}

// ==================== Service Methods ====================

// GetDirScanResults retrieves directory scan results for an asset
// Uses workspace_id + authority + host + port for association
// Falls back to workspace_id + host + port if authority is missing
func (s *ScanResultService) GetDirScanResults(ctx context.Context, req *GetDirScanResultsReq) (*GetDirScanResultsResp, error) {
	dirScanModel := model.NewDirScanResultModel(s.db)

	// Build filter with primary association criteria
	filter := bson.M{
		"workspace_id": req.WorkspaceId,
		"host":         req.Host,
		"port":         req.Port,
	}

	// Add authority to filter if provided (primary match)
	if req.Authority != "" {
		filter["authority"] = req.Authority
	}

	// Count total matching results
	total, err := dirScanModel.CountByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	// If no results found with authority, try fallback without authority
	// This handles legacy data that may not have the authority field
	if total == 0 && req.Authority != "" {
		delete(filter, "authority")
		total, err = dirScanModel.CountByFilter(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	// Calculate page number from offset and limit
	page := 1
	pageSize := req.Limit
	if req.Limit > 0 && req.Offset >= 0 {
		page = (req.Offset / req.Limit) + 1
	}
	if pageSize == 0 {
		pageSize = 100 // Default page size
	}

	// Fetch results with pagination, sorted by scan_time descending (most recent first)
	results, err := dirScanModel.FindByFilterWithSort(ctx, filter, page, pageSize, "scan_time", "desc")
	if err != nil {
		return nil, err
	}

	// Normalize legacy data: apply defaults for missing fields
	for i := range results {
		normalizeDirScanResult(&results[i])
	}

	// Determine the most recent scan time
	var scanTime time.Time
	if len(results) > 0 && !results[0].ScanTime.IsZero() {
		scanTime = results[0].ScanTime
	}

	return &GetDirScanResultsResp{
		Results:  results,
		Total:    total,
		ScanTime: scanTime,
	}, nil
}

// GetVulnScanResults retrieves vulnerability scan results for an asset
// Uses workspace_id + authority + host + port for association
// Falls back to workspace_id + host + port if authority is missing
func (s *ScanResultService) GetVulnScanResults(ctx context.Context, req *GetVulnScanResultsReq) (*GetVulnScanResultsResp, error) {
	scanResultModel := model.NewScanResultModel(s.db, req.WorkspaceId)

	// Build filter with primary association criteria
	filter := bson.M{
		"host": req.Host,
		"port": req.Port,
	}

	// Add authority to filter if provided (primary match)
	if req.Authority != "" {
		filter["authority"] = req.Authority
	}

	// Count total matching results
	total, err := scanResultModel.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// If no results found with authority, try fallback without authority
	// This handles legacy data that may not have the authority field
	if total == 0 && req.Authority != "" {
		delete(filter, "authority")
		total, err = scanResultModel.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	// Calculate page number from offset and limit
	page := 1
	pageSize := req.Limit
	if req.Limit > 0 && req.Offset >= 0 {
		page = (req.Offset / req.Limit) + 1
	}
	if pageSize == 0 {
		pageSize = 50 // Default page size
	}

	// Fetch results with pagination, sorted by scan_time descending (most recent first)
	results, err := scanResultModel.FindWithSort(ctx, filter, page, pageSize, "scan_time", -1)
	if err != nil {
		return nil, err
	}

	// Normalize legacy data: apply defaults for missing fields
	for i := range results {
		normalizeVulnScanResult(&results[i])
	}

	// Determine the most recent scan time
	var scanTime time.Time
	if len(results) > 0 && !results[0].ScanTime.IsZero() {
		scanTime = results[0].ScanTime
	}

	return &GetVulnScanResultsResp{
		Results:  results,
		Total:    total,
		ScanTime: scanTime,
	}, nil
}

// GetScanResultSummary retrieves aggregated scan result summaries for multiple assets
// This is optimized for batch queries to support asset inventory views
func (s *ScanResultService) GetScanResultSummary(ctx context.Context, req *GetScanResultSummaryReq) (*GetScanResultSummaryResp, error) {
	summaries := make(map[string]ScanResultSummary)

	// Get asset model to fetch asset details
	assetModel := model.NewAssetModel(s.db, req.WorkspaceId)
	dirScanModel := model.NewDirScanResultModel(s.db)
	scanResultModel := model.NewScanResultModel(s.db, req.WorkspaceId)

	// For each asset ID, fetch and aggregate scan results
	for _, assetId := range req.AssetIds {
		// Fetch asset to get authority, host, port
		asset, err := assetModel.FindById(ctx, assetId)
		if err != nil {
			// Skip assets that can't be found
			continue
		}

		summary := ScanResultSummary{
			AssetId: assetId,
		}

		// Count directory scan results
		dirFilter := bson.M{
			"workspace_id": req.WorkspaceId,
			"host":         asset.Host,
			"port":         asset.Port,
		}
		if asset.Authority != "" {
			dirFilter["authority"] = asset.Authority
		}
		dirCount, err := dirScanModel.CountByFilter(ctx, dirFilter)
		if err == nil {
			summary.DirScanCount = dirCount
		}

		// Count vulnerability scan results
		vulnFilter := bson.M{
			"host": asset.Host,
			"port": asset.Port,
		}
		if asset.Authority != "" {
			vulnFilter["authority"] = asset.Authority
		}
		vulnCount, err := scanResultModel.Count(ctx, vulnFilter)
		if err == nil {
			summary.VulnScanCount = vulnCount
		}

		// Count high-risk vulnerabilities (risk_score >= 7.0)
		highRiskFilter := bson.M{
			"host":       asset.Host,
			"port":       asset.Port,
			"risk_score": bson.M{"$gte": 7.0},
		}
		if asset.Authority != "" {
			highRiskFilter["authority"] = asset.Authority
		}
		highRiskCount, err := scanResultModel.Count(ctx, highRiskFilter)
		if err == nil {
			summary.HighRiskCount = highRiskCount
		}

		// Get most recent scan time from directory scans
		dirResults, err := dirScanModel.FindByFilterWithSort(ctx, dirFilter, 1, 1, "scan_time", "desc")
		if err == nil && len(dirResults) > 0 && !dirResults[0].ScanTime.IsZero() {
			summary.LastScanTime = dirResults[0].ScanTime
		}

		// Check vulnerability scan time if it's more recent
		vulnResults, err := scanResultModel.FindWithSort(ctx, vulnFilter, 1, 1, "scan_time", -1)
		if err == nil && len(vulnResults) > 0 && !vulnResults[0].ScanTime.IsZero() {
			if vulnResults[0].ScanTime.After(summary.LastScanTime) {
				summary.LastScanTime = vulnResults[0].ScanTime
			}
		}

		summaries[assetId] = summary
	}

	return &GetScanResultSummaryResp{
		Summaries: summaries,
	}, nil
}

// SaveScanResultsReq represents a request to save scan results with history preservation
type SaveScanResultsReq struct {
	WorkspaceId   string
	TargetId      string
	TaskId        string
	Authority     string
	Host          string
	Port          int
	DirResults    []model.DirScanResult
	VulnResults   []model.ScanResult
	ScanTimestamp time.Time
}

// SaveScanResultsWithHistory saves new scan results and preserves historical data
// This method implements the complete rescan flow:
// 1. Check if target has existing results
// 2. If exists, archive current results to history
// 3. Save new scan results with current timestamp
// 4. Implement merge logic to preserve unchanged asset fields
func (s *ScanResultService) SaveScanResultsWithHistory(ctx context.Context, req *SaveScanResultsReq) error {
	// Validate required parameters
	if req.WorkspaceId == "" {
		return model.ErrValidationFailed.WithDetails("workspace_id is required")
	}
	if req.Host == "" {
		return model.ErrValidationFailed.WithDetails("host is required")
	}
	if req.Port == 0 {
		return model.ErrValidationFailed.WithDetails("port is required")
	}

	// Set default scan timestamp if not provided
	if req.ScanTimestamp.IsZero() {
		req.ScanTimestamp = time.Now()
	}

	// Step 1: Check if target has existing results
	dirScanModel := model.NewDirScanResultModel(s.db)
	scanResultModel := model.NewScanResultModel(s.db, req.WorkspaceId)

	// Build filter for existing results
	dirFilter := bson.M{
		"workspace_id": req.WorkspaceId,
		"host":         req.Host,
		"port":         req.Port,
	}
	if req.Authority != "" {
		dirFilter["authority"] = req.Authority
	}

	vulnFilter := bson.M{
		"host": req.Host,
		"port": req.Port,
	}
	if req.Authority != "" {
		vulnFilter["authority"] = req.Authority
	}

	// Check if existing results exist
	existingDirCount, err := dirScanModel.CountByFilter(ctx, dirFilter)
	if err != nil {
		return err
	}

	existingVulnCount, err := scanResultModel.Count(ctx, vulnFilter)
	if err != nil {
		return err
	}

	hasExistingResults := existingDirCount > 0 || existingVulnCount > 0

	// Step 2: If existing results exist, archive them (for historical comparison)
	if hasExistingResults {
		historyService := NewHistoryService(s.db)
		archiveReq := &ArchiveResultsReq{
			WorkspaceId: req.WorkspaceId,
			TargetId:    req.TargetId,
			Authority:   req.Authority,
			Host:        req.Host,
			Port:        req.Port,
			ArchiveTime: req.ScanTimestamp,
		}

		// Archive for history comparison, but don't fail if archival fails
		if err := historyService.ArchiveCurrentResults(ctx, archiveReq); err != nil {
			// Log the error but continue - we don't want to lose new scan results
			// just because archival failed
			// logx.Errorf("Failed to archive results: %v", err)
		}

		// NOTE: We no longer delete old results here!
		// Instead, we use Upsert below to merge new results with existing ones.
		// This preserves historical data while updating with new scan information.
	}

	// Step 3: Save new scan results using Insert
	// Old results are preserved in the history archive
	// New results are added alongside existing ones (not replacing them)
	for i := range req.DirResults {
		req.DirResults[i].ScanTime = req.ScanTimestamp
		req.DirResults[i].Version = 1
		// Use Upsert to avoid duplicates (based on URL)
		if err := dirScanModel.Upsert(ctx, &req.DirResults[i]); err != nil {
			return err
		}
	}

	// Save vulnerability scan results
	// Use VulModel instead of ScanResultModel for proper Upsert
	vulModel := model.NewVulModel(s.db, req.WorkspaceId)
	for i := range req.VulnResults {
		req.VulnResults[i].ScanTime = req.ScanTimestamp
		req.VulnResults[i].Version = 1
		// Insert new scan results (ScanResultModel stores raw scan results)
		if err := scanResultModel.Insert(ctx, &req.VulnResults[i]); err != nil {
			// If insert fails due to duplicate, try to continue
			// This can happen if the same result already exists
			continue
		}
	}
	_ = vulModel // Suppress unused warning - can be used for Vul-specific operations

	// Step 4: Update asset with merge logic and record changes
	assetModel := model.NewAssetModel(s.db, req.WorkspaceId)
	assetHistoryModel := model.NewAssetHistoryModel(s.db, req.WorkspaceId)

	// Try to find existing asset
	var existingAsset *model.Asset
	if req.Authority != "" {
		existingAsset, _ = assetModel.FindByAuthorityOnly(ctx, req.Authority)
	}
	if existingAsset == nil {
		existingAsset, _ = assetModel.FindByHostPort(ctx, req.Host, req.Port)
	}

	// If asset exists, compare and record changes, then update
	if existingAsset != nil {
		// Calculate changes between old and new scan results
		changes := calculateAssetChanges(existingAsset, req)

		// If there are changes, record them in asset history
		if len(changes) > 0 {
			historyRecord := &model.AssetHistory{
				AssetId:    existingAsset.Id.Hex(),
				Authority:  existingAsset.Authority,
				Host:       existingAsset.Host,
				Port:       existingAsset.Port,
				Service:    existingAsset.Service,
				Title:      existingAsset.Title,
				App:        existingAsset.App,
				HttpStatus: existingAsset.HttpStatus,
				HttpHeader: existingAsset.HttpHeader,
				HttpBody:   existingAsset.HttpBody,
				Banner:     existingAsset.Banner,
				IconHash:   existingAsset.IconHash,
				Screenshot: existingAsset.Screenshot,
				TaskId:     req.TaskId,
				Changes:    changes,
			}

			// Insert history record (ignore errors to not block the main flow)
			if err := assetHistoryModel.Insert(ctx, historyRecord); err != nil {
				// Log error but continue
			}
		}

		update := bson.M{
			"last_scan_time": req.ScanTimestamp,
			"update_time":    req.ScanTimestamp,
		}

		// 如果检测到变更，标记资产为已更新
		if len(changes) > 0 {
			update["update"] = true
			update["last_status_change_time"] = req.ScanTimestamp
		}

		// Only update fields that are provided in the new scan
		// Preserve user-modified fields like labels, memo, color_tag, etc.
		if err := assetModel.Update(ctx, existingAsset.Id.Hex(), update); err != nil {
			return err
		}
	}

	return nil
}

// calculateAssetChanges compares old asset with new scan results and returns changes
func calculateAssetChanges(oldAsset *model.Asset, newReq *SaveScanResultsReq) []model.FieldChange {
	var changes []model.FieldChange

	// Compare directory scan results count
	oldDirCount := 0 // We don't have old count here, but we can track new additions
	newDirCount := len(newReq.DirResults)
	if newDirCount > 0 {
		changes = append(changes, model.FieldChange{
			Field:    "dirScanResults",
			OldValue: fmt.Sprintf("%d results", oldDirCount),
			NewValue: fmt.Sprintf("%d new results", newDirCount),
		})
	}

	// Compare vulnerability scan results count
	oldVulnCount := 0
	newVulnCount := len(newReq.VulnResults)
	if newVulnCount > 0 {
		changes = append(changes, model.FieldChange{
			Field:    "vulnScanResults",
			OldValue: fmt.Sprintf("%d vulnerabilities", oldVulnCount),
			NewValue: fmt.Sprintf("%d new vulnerabilities", newVulnCount),
		})
	}

	return changes
}

// ==================== Legacy Data Normalization ====================

// normalizeDirScanResult applies default values for legacy directory scan results
// This ensures backward compatibility with existing data that may not have new fields
func normalizeDirScanResult(result *model.DirScanResult) {
	// Requirement 5.1: Assign version 1 to records without version field
	if result.Version == 0 {
		result.Version = 1
	}

	// Requirement 5.6: Handle missing optional fields with appropriate defaults
	// Title defaults to empty string if missing (already handled by Go zero value)
	// But we explicitly ensure it's not nil-like
	if result.Title == "" {
		result.Title = ""
	}

	// Requirement 5.1: If scan_time is missing, use create_time as fallback
	// This treats legacy records as version 1 with their creation time
	if result.ScanTime.IsZero() && !result.CreateTime.IsZero() {
		result.ScanTime = result.CreateTime
	}
}

// normalizeVulnScanResult applies default values for legacy vulnerability scan results
// This ensures backward compatibility with existing data that may not have new fields
func normalizeVulnScanResult(result *model.ScanResult) {
	// Requirement 5.1: Assign version 1 to records without version field
	if result.Version == 0 {
		result.Version = 1
	}

	// Requirement 5.6: Handle missing optional fields with appropriate defaults
	// Process findings to ensure descriptions have appropriate defaults
	for i := range result.Findings {
		// Description can be empty string or remain as is
		// Go's zero value for string is already empty string
		if result.Findings[i].Description == "" {
			result.Findings[i].Description = ""
		}
	}

	// Requirement 5.1: If scan_time is missing, use completed or created time as fallback
	// This treats legacy records as version 1 with their completion/creation time
	if result.ScanTime.IsZero() {
		if !result.Completed.IsZero() {
			result.ScanTime = result.Completed
		} else if !result.Created.IsZero() {
			result.ScanTime = result.Created
		}
	}
}
