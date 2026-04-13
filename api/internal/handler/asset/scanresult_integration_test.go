package asset

import (
	"context"
	"cscan/api/internal/svc"
	"cscan/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestIntegration_CompleteFlow tests the complete flow:
// create asset → run scan → view in inventory → open screenshot dialog → verify all results displayed
// Validates: Requirements 1.1, 1.2, 1.3, 2.1, 2.2, 2.3, 2.4, 3.1, 3.5, 4.1
func TestIntegration_CompleteFlow(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test database connection
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := "test_workspace"

	// Clean up test data before and after
	defer cleanupTestData(t, db, workspaceId)
	cleanupTestData(t, db, workspaceId)

	// Step 1: Create test asset
	assetModel := model.NewAssetModel(db, workspaceId)
	testAsset := &model.Asset{
		Id:         primitive.NewObjectID(),
		Authority:  "example.com:443",
		Host:       "example.com",
		Port:       443,
		Service:    "https",
		Title:      "Example Site",
		App:        []string{"nginx"},
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}
	err = assetModel.Insert(ctx, testAsset)
	require.NoError(t, err, "Failed to create test asset")

	// Step 2: Create directory scan results
	dirScanModel := model.NewDirScanResultModel(db)
	dirResults := []model.DirScanResult{
		{
			Id:            primitive.NewObjectID(),
			WorkspaceId:   workspaceId,
			Authority:     testAsset.Authority,
			Host:          testAsset.Host,
			Port:          testAsset.Port,
			URL:           "https://example.com/admin",
			Path:          "/admin",
			StatusCode:    200,
			ContentLength: 1024,
			Title:         "Admin Panel",
			ScanTime:      time.Now(),
			Version:       1,
			CreateTime:    time.Now(),
		},
		{
			Id:            primitive.NewObjectID(),
			WorkspaceId:   workspaceId,
			Authority:     testAsset.Authority,
			Host:          testAsset.Host,
			Port:          testAsset.Port,
			URL:           "https://example.com/api",
			Path:          "/api",
			StatusCode:    200,
			ContentLength: 512,
			Title:         "API Endpoint",
			ScanTime:      time.Now(),
			Version:       1,
			CreateTime:    time.Now(),
		},
	}
	for i := range dirResults {
		err = dirScanModel.Insert(ctx, &dirResults[i])
		require.NoError(t, err, "Failed to create directory scan result")
	}

	// Step 3: Create vulnerability scan results
	scanResultModel := model.NewScanResultModel(db, workspaceId)
	vulnResult := &model.ScanResult{
		ID:        primitive.NewObjectID(),
		JobID:     "test-job-1",
		TargetID:  testAsset.Id.Hex(),
		Authority: testAsset.Authority,
		Host:      testAsset.Host,
		Port:      testAsset.Port,
		RiskScore: 8.5,
		RiskLevel: "high",
		ScanTime:  time.Now(),
		Version:   1,
		Created:   time.Now(),
		Completed: time.Now(),
		Findings: []model.Finding{
			{
				ID:          "CVE-2023-1234",
				Type:        "vulnerability",
				Severity:    "high",
				Title:       "SQL Injection",
				Description: "SQL injection vulnerability found",
				RiskScore:   8.5,
				Discovered:  time.Now(),
			},
		},
	}
	err = scanResultModel.Insert(ctx, vulnResult)
	require.NoError(t, err, "Failed to create vulnerability scan result")

	// Step 4: Test AssetService.GetAssetList with scan summaries
	assetService := svc.NewAssetService(db)
	assetListReq := &svc.GetAssetListReq{
		WorkspaceId: workspaceId,
		Page:        1,
		PageSize:    20,
		SortField:   "update_time",
	}
	assetListResp, err := assetService.GetAssetList(ctx, assetListReq)
	require.NoError(t, err, "Failed to get asset list")
	require.NotEmpty(t, assetListResp.Assets, "Asset list should not be empty")

	// Verify scan summaries are included
	assetWithSummary := assetListResp.Assets[0]
	assert.Equal(t, int64(2), assetWithSummary.DirScanCount, "Directory scan count should be 2")
	assert.Equal(t, int64(1), assetWithSummary.VulnScanCount, "Vulnerability scan count should be 1")
	assert.Equal(t, int64(1), assetWithSummary.HighRiskVulnCount, "High risk vulnerability count should be 1")
	assert.False(t, assetWithSummary.LastScanTime.IsZero(), "Last scan time should be set")

	// Step 5: Test ScanResultService.GetDirScanResults
	scanResultService := svc.NewScanResultService(db)
	dirScanReq := &svc.GetDirScanResultsReq{
		WorkspaceId: workspaceId,
		Authority:   testAsset.Authority,
		Host:        testAsset.Host,
		Port:        testAsset.Port,
		Limit:       100,
		Offset:      0,
	}
	dirScanResp, err := scanResultService.GetDirScanResults(ctx, dirScanReq)
	require.NoError(t, err, "Failed to get directory scan results")
	assert.Equal(t, int64(2), dirScanResp.Total, "Total directory scan results should be 2")
	assert.Len(t, dirScanResp.Results, 2, "Should return 2 directory scan results")

	// Step 6: Test ScanResultService.GetVulnScanResults
	vulnScanReq := &svc.GetVulnScanResultsReq{
		WorkspaceId: workspaceId,
		Authority:   testAsset.Authority,
		Host:        testAsset.Host,
		Port:        testAsset.Port,
		Limit:       50,
		Offset:      0,
	}
	vulnScanResp, err := scanResultService.GetVulnScanResults(ctx, vulnScanReq)
	require.NoError(t, err, "Failed to get vulnerability scan results")
	assert.Equal(t, int64(1), vulnScanResp.Total, "Total vulnerability scan results should be 1")
	assert.Len(t, vulnScanResp.Results, 1, "Should return 1 vulnerability scan result")

	// Step 7: Verify cross-view consistency
	// Directory scan count in inventory should match count in screenshot dialog
	assert.Equal(t, assetWithSummary.DirScanCount, dirScanResp.Total, "Directory scan counts should match across views")
	assert.Equal(t, assetWithSummary.VulnScanCount, vulnScanResp.Total, "Vulnerability scan counts should match across views")
}

// TestIntegration_RescanFlow tests the rescan flow:
// run scan → run second scan → verify history preserved → verify new results displayed
// Validates: Requirements 3.1, 3.4, 3.5
func TestIntegration_RescanFlow(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test database connection
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := "test_workspace"

	// Clean up test data before and after
	defer cleanupTestData(t, db, workspaceId)
	cleanupTestData(t, db, workspaceId)

	// Step 1: Create test asset
	assetModel := model.NewAssetModel(db, workspaceId)
	testAsset := &model.Asset{
		Id:         primitive.NewObjectID(),
		Authority:  "rescan.example.com:443",
		Host:       "rescan.example.com",
		Port:       443,
		Service:    "https",
		Title:      "Rescan Test Site",
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}
	err = assetModel.Insert(ctx, testAsset)
	require.NoError(t, err, "Failed to create test asset")

	// Step 2: Run first scan - create initial scan results
	scanResultService := svc.NewScanResultService(db)
	firstScanTime := time.Now()
	firstScanReq := &svc.SaveScanResultsReq{
		WorkspaceId:   workspaceId,
		TargetId:      testAsset.Id.Hex(),
		Authority:     testAsset.Authority,
		Host:          testAsset.Host,
		Port:          testAsset.Port,
		ScanTimestamp: firstScanTime,
		DirResults: []model.DirScanResult{
			{
				Id:            primitive.NewObjectID(),
				WorkspaceId:   workspaceId,
				Authority:     testAsset.Authority,
				Host:          testAsset.Host,
				Port:          testAsset.Port,
				URL:           "https://rescan.example.com/old",
				Path:          "/old",
				StatusCode:    200,
				ContentLength: 100,
				Title:         "Old Path",
				CreateTime:    firstScanTime,
			},
		},
		VulnResults: []model.ScanResult{},
	}
	err = scanResultService.SaveScanResultsWithHistory(ctx, firstScanReq)
	require.NoError(t, err, "Failed to save first scan results")

	// Step 3: Verify first scan results exist
	dirScanReq := &svc.GetDirScanResultsReq{
		WorkspaceId: workspaceId,
		Authority:   testAsset.Authority,
		Host:        testAsset.Host,
		Port:        testAsset.Port,
		Limit:       100,
		Offset:      0,
	}
	dirScanResp, err := scanResultService.GetDirScanResults(ctx, dirScanReq)
	require.NoError(t, err, "Failed to get directory scan results after first scan")
	assert.Equal(t, int64(1), dirScanResp.Total, "Should have 1 directory scan result after first scan")

	// Step 4: Run second scan - this should archive old results and save new ones
	time.Sleep(1 * time.Second) // Ensure different timestamp
	secondScanTime := time.Now()
	secondScanReq := &svc.SaveScanResultsReq{
		WorkspaceId:   workspaceId,
		TargetId:      testAsset.Id.Hex(),
		Authority:     testAsset.Authority,
		Host:          testAsset.Host,
		Port:          testAsset.Port,
		ScanTimestamp: secondScanTime,
		DirResults: []model.DirScanResult{
			{
				Id:            primitive.NewObjectID(),
				WorkspaceId:   workspaceId,
				Authority:     testAsset.Authority,
				Host:          testAsset.Host,
				Port:          testAsset.Port,
				URL:           "https://rescan.example.com/new",
				Path:          "/new",
				StatusCode:    200,
				ContentLength: 200,
				Title:         "New Path",
				CreateTime:    secondScanTime,
			},
		},
		VulnResults: []model.ScanResult{},
	}
	err = scanResultService.SaveScanResultsWithHistory(ctx, secondScanReq)
	require.NoError(t, err, "Failed to save second scan results")

	// Step 5: Verify new scan results are displayed (most recent)
	dirScanResp, err = scanResultService.GetDirScanResults(ctx, dirScanReq)
	require.NoError(t, err, "Failed to get directory scan results after second scan")
	assert.Equal(t, int64(1), dirScanResp.Total, "Should have 1 directory scan result after second scan")
	assert.Equal(t, "/new", dirScanResp.Results[0].Path, "Should display new scan results")

	// Step 6: Verify historical data is preserved
	historyService := svc.NewHistoryService(db)
	historyReq := &svc.GetResultHistoryReq{
		WorkspaceId: workspaceId,
		Authority:   testAsset.Authority,
		Host:        testAsset.Host,
		Port:        testAsset.Port,
	}
	historyResp, err := historyService.GetResultHistory(ctx, historyReq)
	require.NoError(t, err, "Failed to get result history")
	assert.NotEmpty(t, historyResp.Versions, "Should have historical versions")
	assert.Equal(t, int64(1), historyResp.Versions[0].DirScanCount, "Historical version should have 1 directory scan")
}

// TestIntegration_CrossViewConsistency tests cross-view consistency:
// query same asset from different endpoints → verify counts match
// Validates: Requirements 4.1, 4.3
func TestIntegration_CrossViewConsistency(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test database connection
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := "test_workspace"

	// Clean up test data before and after
	defer cleanupTestData(t, db, workspaceId)
	cleanupTestData(t, db, workspaceId)

	// Create test asset with scan results
	assetModel := model.NewAssetModel(db, workspaceId)
	testAsset := &model.Asset{
		Id:         primitive.NewObjectID(),
		Authority:  "consistency.example.com:443",
		Host:       "consistency.example.com",
		Port:       443,
		Service:    "https",
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}
	err = assetModel.Insert(ctx, testAsset)
	require.NoError(t, err)

	// Create 5 directory scan results
	dirScanModel := model.NewDirScanResultModel(db)
	for i := 0; i < 5; i++ {
		dirResult := &model.DirScanResult{
			Id:            primitive.NewObjectID(),
			WorkspaceId:   workspaceId,
			Authority:     testAsset.Authority,
			Host:          testAsset.Host,
			Port:          testAsset.Port,
			URL:           "https://consistency.example.com/path" + string(rune(i)),
			Path:          "/path" + string(rune(i)),
			StatusCode:    200,
			ContentLength: 100,
			ScanTime:      time.Now(),
			Version:       1,
			CreateTime:    time.Now(),
		}
		err = dirScanModel.Insert(ctx, dirResult)
		require.NoError(t, err)
	}

	// Query from AssetService (inventory view)
	assetService := svc.NewAssetService(db)
	assetListReq := &svc.GetAssetListReq{
		WorkspaceId: workspaceId,
		Page:        1,
		PageSize:    20,
		SortField:   "update_time",
	}
	assetListResp, err := assetService.GetAssetList(ctx, assetListReq)
	require.NoError(t, err)
	require.NotEmpty(t, assetListResp.Assets)
	inventoryDirCount := assetListResp.Assets[0].DirScanCount

	// Query from ScanResultService (screenshot dialog view)
	scanResultService := svc.NewScanResultService(db)
	dirScanReq := &svc.GetDirScanResultsReq{
		WorkspaceId: workspaceId,
		Authority:   testAsset.Authority,
		Host:        testAsset.Host,
		Port:        testAsset.Port,
		Limit:       100,
		Offset:      0,
	}
	dirScanResp, err := scanResultService.GetDirScanResults(ctx, dirScanReq)
	require.NoError(t, err)
	dialogDirCount := dirScanResp.Total

	// Verify consistency
	assert.Equal(t, inventoryDirCount, dialogDirCount, "Directory scan counts must match across views")
	assert.Equal(t, int64(5), inventoryDirCount, "Should have 5 directory scans")
	assert.Equal(t, int64(5), dialogDirCount, "Should have 5 directory scans")
}

// TestIntegration_BackwardCompatibility tests backward compatibility with existing data:
// import legacy scan results without version fields → verify they display correctly
// Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.6
func TestIntegration_BackwardCompatibility(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test database connection
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := "test_workspace"

	// Clean up test data before and after
	defer cleanupTestData(t, db, workspaceId)
	cleanupTestData(t, db, workspaceId)

	// Create test asset
	assetModel := model.NewAssetModel(db, workspaceId)
	testAsset := &model.Asset{
		Id:         primitive.NewObjectID(),
		Authority:  "legacy.example.com:443",
		Host:       "legacy.example.com",
		Port:       443,
		Service:    "https",
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}
	err = assetModel.Insert(ctx, testAsset)
	require.NoError(t, err)

	// Create legacy directory scan result (without version and scan_time fields)
	dirScanModel := model.NewDirScanResultModel(db)
	legacyDirResult := &model.DirScanResult{
		Id:            primitive.NewObjectID(),
		WorkspaceId:   workspaceId,
		Authority:     testAsset.Authority,
		Host:          testAsset.Host,
		Port:          testAsset.Port,
		URL:           "https://legacy.example.com/old",
		Path:          "/old",
		StatusCode:    200,
		ContentLength: 100,
		Title:         "", // Missing optional field
		CreateTime:    time.Now(),
		// Note: Version and ScanTime are not set (legacy data)
	}
	err = dirScanModel.Insert(ctx, legacyDirResult)
	require.NoError(t, err)

	// Query legacy data through ScanResultService
	scanResultService := svc.NewScanResultService(db)
	dirScanReq := &svc.GetDirScanResultsReq{
		WorkspaceId: workspaceId,
		Authority:   testAsset.Authority,
		Host:        testAsset.Host,
		Port:        testAsset.Port,
		Limit:       100,
		Offset:      0,
	}
	dirScanResp, err := scanResultService.GetDirScanResults(ctx, dirScanReq)
	require.NoError(t, err, "Should successfully query legacy data")
	assert.Equal(t, int64(1), dirScanResp.Total, "Should find 1 legacy directory scan result")
	assert.Len(t, dirScanResp.Results, 1, "Should return 1 legacy directory scan result")

	// Verify legacy data is normalized
	result := dirScanResp.Results[0]
	assert.Equal(t, int64(1), result.Version, "Legacy data should be assigned version 1")
	assert.Equal(t, "", result.Title, "Missing optional field should default to empty string")
}

// cleanupTestData removes all test data from the database
func cleanupTestData(t *testing.T, db *mongo.Database, workspaceId string) {
	ctx := context.Background()

	// Clean up assets
	assetModel := model.NewAssetModel(db, workspaceId)
	_, err := assetModel.DeleteByFilter(ctx, primitive.M{})
	if err != nil {
		t.Logf("Warning: Failed to clean up assets: %v", err)
	}

	// Clean up directory scan results
	dirScanModel := model.NewDirScanResultModel(db)
	_, err = dirScanModel.DeleteByFilter(ctx, primitive.M{"workspace_id": workspaceId})
	if err != nil {
		t.Logf("Warning: Failed to clean up directory scan results: %v", err)
	}

	// Clean up vulnerability scan results
	scanResultModel := model.NewScanResultModel(db, workspaceId)
	_, err = scanResultModel.DeleteMany(ctx, primitive.M{})
	if err != nil {
		t.Logf("Warning: Failed to clean up vulnerability scan results: %v", err)
	}

	// Clean up history
	historyModel := model.NewScanResultHistoryModel(db, workspaceId)
	_, err = historyModel.Clear(ctx, workspaceId)
	if err != nil {
		t.Logf("Warning: Failed to clean up history: %v", err)
	}
}
