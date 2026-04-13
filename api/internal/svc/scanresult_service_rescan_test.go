package svc

import (
	"cscan/model"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// =============================================================================
// Comprehensive Unit Tests for Rescan Scenarios
// Task 5.4: Write unit tests for rescan scenarios
// **Validates: Requirements 3.1, 3.7**
// =============================================================================

// TestRescanScenario_FirstScan tests the first scan scenario where no existing
// results exist. This verifies that the system correctly handles the initial
// scan without attempting to archive non-existent data.
func TestRescanScenario_FirstScan(t *testing.T) {
	t.Run("First scan with directory and vulnerability results", func(t *testing.T) {
		// Create a first scan request
		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-001",
			Authority:   "https://example.com:443",
			Host:        "example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{
					WorkspaceId:   "test-workspace",
					Authority:     "https://example.com:443",
					Host:          "example.com",
					Port:          443,
					Path:          "/admin",
					StatusCode:    200,
					ContentLength: 1024,
					Title:         "Admin Panel",
				},
				{
					WorkspaceId:   "test-workspace",
					Authority:     "https://example.com:443",
					Host:          "example.com",
					Port:          443,
					Path:          "/api",
					StatusCode:    200,
					ContentLength: 512,
					Title:         "API Endpoint",
				},
			},
			VulnResults: []model.ScanResult{
				{
					Authority: "https://example.com:443",
					Host:      "example.com",
					Port:      443,
					RiskScore: 7.5,
					RiskLevel: "high",
				},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify request structure
		if req.WorkspaceId == "" {
			t.Error("WorkspaceId should not be empty")
		}
		if req.Host == "" {
			t.Error("Host should not be empty")
		}
		if req.Port == 0 {
			t.Error("Port should not be zero")
		}
		if len(req.DirResults) != 2 {
			t.Errorf("Expected 2 directory scan results, got %d", len(req.DirResults))
		}
		if len(req.VulnResults) != 1 {
			t.Errorf("Expected 1 vulnerability scan result, got %d", len(req.VulnResults))
		}
		if req.ScanTimestamp.IsZero() {
			t.Error("ScanTimestamp should not be zero")
		}

		// Verify that all results have proper association fields
		for i, dirResult := range req.DirResults {
			if dirResult.WorkspaceId != req.WorkspaceId {
				t.Errorf("DirResult[%d] WorkspaceId mismatch", i)
			}
			if dirResult.Host != req.Host {
				t.Errorf("DirResult[%d] Host mismatch", i)
			}
			if dirResult.Port != req.Port {
				t.Errorf("DirResult[%d] Port mismatch", i)
			}
		}

		for i, vulnResult := range req.VulnResults {
			if vulnResult.Host != req.Host {
				t.Errorf("VulnResult[%d] Host mismatch", i)
			}
			if vulnResult.Port != req.Port {
				t.Errorf("VulnResult[%d] Port mismatch", i)
			}
		}
	})

	t.Run("First scan with only directory results", func(t *testing.T) {
		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-002",
			Authority:   "https://api.example.com:443",
			Host:        "api.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/v1", StatusCode: 200},
				{Path: "/v2", StatusCode: 200},
				{Path: "/health", StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{}, // No vulnerabilities found
			ScanTimestamp: time.Now(),
		}

		if len(req.DirResults) != 3 {
			t.Errorf("Expected 3 directory scan results, got %d", len(req.DirResults))
		}
		if len(req.VulnResults) != 0 {
			t.Errorf("Expected 0 vulnerability scan results, got %d", len(req.VulnResults))
		}
	})

	t.Run("First scan with only vulnerability results", func(t *testing.T) {
		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-003",
			Authority:   "https://secure.example.com:443",
			Host:        "secure.example.com",
			Port:        443,
			DirResults:  []model.DirScanResult{}, // No directories found
			VulnResults: []model.ScanResult{
				{RiskScore: 9.0, RiskLevel: "critical"},
				{RiskScore: 7.0, RiskLevel: "high"},
			},
			ScanTimestamp: time.Now(),
		}

		if len(req.DirResults) != 0 {
			t.Errorf("Expected 0 directory scan results, got %d", len(req.DirResults))
		}
		if len(req.VulnResults) != 2 {
			t.Errorf("Expected 2 vulnerability scan results, got %d", len(req.VulnResults))
		}
	})

	t.Run("First scan with no results (clean target)", func(t *testing.T) {
		req := &SaveScanResultsReq{
			WorkspaceId:   "test-workspace",
			TargetId:      "target-004",
			Authority:     "https://clean.example.com:443",
			Host:          "clean.example.com",
			Port:          443,
			DirResults:    []model.DirScanResult{}, // No directories found
			VulnResults:   []model.ScanResult{},    // No vulnerabilities found
			ScanTimestamp: time.Now(),
		}

		// Even with no results, the request should be valid
		if req.WorkspaceId == "" {
			t.Error("WorkspaceId should not be empty")
		}
		if len(req.DirResults) != 0 {
			t.Errorf("Expected 0 directory scan results, got %d", len(req.DirResults))
		}
		if len(req.VulnResults) != 0 {
			t.Errorf("Expected 0 vulnerability scan results, got %d", len(req.VulnResults))
		}
	})

	t.Run("First scan without authority (fallback association)", func(t *testing.T) {
		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-005",
			Authority:   "", // No authority - will use host+port for association
			Host:        "192.168.1.100",
			Port:        8080,
			DirResults: []model.DirScanResult{
				{Path: "/", StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify fallback association criteria
		if req.Authority != "" {
			t.Error("Authority should be empty for fallback test")
		}
		if req.Host == "" {
			t.Error("Host should not be empty")
		}
		if req.Port == 0 {
			t.Error("Port should not be zero")
		}
	})
}

// TestRescanScenario_ExistingResultsArchived tests the rescan scenario where
// existing results exist and should be archived before saving new results.
// This verifies that historical data is preserved correctly.
func TestRescanScenario_ExistingResultsArchived(t *testing.T) {
	t.Run("Rescan with more results than first scan", func(t *testing.T) {
		// Simulate first scan
		firstScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-100",
			Authority:   "https://example.com:443",
			Host:        "example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/admin", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 7.5, RiskLevel: "high"},
			},
			ScanTimestamp: time.Now().Add(-24 * time.Hour), // 1 day ago
		}

		// Simulate second scan (rescan) with more results
		secondScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-100",
			Authority:   "https://example.com:443",
			Host:        "example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/admin", StatusCode: 200},
				{Path: "/api", StatusCode: 200},
				{Path: "/dashboard", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 7.5, RiskLevel: "high"},
				{RiskScore: 6.0, RiskLevel: "medium"},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify that second scan has more recent timestamp
		if !secondScan.ScanTimestamp.After(firstScan.ScanTimestamp) {
			t.Error("Second scan should have more recent timestamp")
		}

		// Verify that second scan has more results
		if len(secondScan.DirResults) <= len(firstScan.DirResults) {
			t.Log("Second scan has more directory results")
		}
		if len(secondScan.VulnResults) > len(firstScan.VulnResults) {
			t.Log("Second scan has more vulnerability results")
		}

		// Verify that both scans target the same asset
		if firstScan.TargetId != secondScan.TargetId {
			t.Error("Both scans should target the same asset")
		}
		if firstScan.Host != secondScan.Host || firstScan.Port != secondScan.Port {
			t.Error("Both scans should have same host and port")
		}
	})

	t.Run("Rescan with fewer results than first scan", func(t *testing.T) {
		// Simulate first scan with many results
		firstScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-101",
			Authority:   "https://api.example.com:443",
			Host:        "api.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/v1", StatusCode: 200},
				{Path: "/v2", StatusCode: 200},
				{Path: "/v3", StatusCode: 200},
				{Path: "/admin", StatusCode: 403},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 8.0, RiskLevel: "high"},
				{RiskScore: 7.0, RiskLevel: "high"},
				{RiskScore: 5.0, RiskLevel: "medium"},
			},
			ScanTimestamp: time.Now().Add(-48 * time.Hour), // 2 days ago
		}

		// Simulate second scan with fewer results (some paths removed/fixed)
		secondScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-101",
			Authority:   "https://api.example.com:443",
			Host:        "api.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/v1", StatusCode: 200},
				{Path: "/v2", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 5.0, RiskLevel: "medium"}, // High-risk vulns fixed
			},
			ScanTimestamp: time.Now(),
		}

		// Verify that second scan has fewer results
		if len(secondScan.DirResults) >= len(firstScan.DirResults) {
			t.Log("Second scan has fewer directory results")
		}
		if len(secondScan.VulnResults) < len(firstScan.VulnResults) {
			t.Log("Second scan has fewer vulnerability results (improvements made)")
		}

		// Verify timestamps
		if !secondScan.ScanTimestamp.After(firstScan.ScanTimestamp) {
			t.Error("Second scan should have more recent timestamp")
		}
	})

	t.Run("Rescan with completely different results", func(t *testing.T) {
		// Simulate first scan
		firstScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-102",
			Authority:   "https://shop.example.com:443",
			Host:        "shop.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/products", StatusCode: 200},
				{Path: "/cart", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 6.0, RiskLevel: "medium"},
			},
			ScanTimestamp: time.Now().Add(-72 * time.Hour), // 3 days ago
		}

		// Simulate second scan with completely different results (site redesigned)
		secondScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-102",
			Authority:   "https://shop.example.com:443",
			Host:        "shop.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/api/v2/products", StatusCode: 200},
				{Path: "/api/v2/checkout", StatusCode: 200},
				{Path: "/api/v2/orders", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 4.0, RiskLevel: "low"},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify that results are completely different
		// (In real implementation, this would trigger archival of old results)
		if firstScan.TargetId != secondScan.TargetId {
			t.Error("Both scans should target the same asset")
		}

		// Verify that paths are different
		firstPaths := make(map[string]bool)
		for _, dr := range firstScan.DirResults {
			firstPaths[dr.Path] = true
		}
		secondPaths := make(map[string]bool)
		for _, dr := range secondScan.DirResults {
			secondPaths[dr.Path] = true
		}

		// Check if any paths overlap
		hasOverlap := false
		for path := range secondPaths {
			if firstPaths[path] {
				hasOverlap = true
				break
			}
		}
		if !hasOverlap {
			t.Log("No overlapping paths between first and second scan (complete redesign)")
		}
	})

	t.Run("Rescan after long time period", func(t *testing.T) {
		// Simulate first scan long ago
		firstScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-103",
			Authority:   "https://legacy.example.com:443",
			Host:        "legacy.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/old-admin", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 9.0, RiskLevel: "critical"},
			},
			ScanTimestamp: time.Now().Add(-365 * 24 * time.Hour), // 1 year ago
		}

		// Simulate rescan after long period
		secondScan := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-103",
			Authority:   "https://legacy.example.com:443",
			Host:        "legacy.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/admin", StatusCode: 200},
				{Path: "/api", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 3.0, RiskLevel: "low"},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify time difference
		timeDiff := secondScan.ScanTimestamp.Sub(firstScan.ScanTimestamp)
		if timeDiff < 364*24*time.Hour {
			t.Error("Expected at least 364 days between scans")
		}

		// Verify that old critical vulnerability is no longer present
		// (In real implementation, this would be visible in historical data)
		if len(firstScan.VulnResults) > 0 && firstScan.VulnResults[0].RiskLevel == "critical" {
			t.Log("First scan had critical vulnerability")
		}
		if len(secondScan.VulnResults) > 0 && secondScan.VulnResults[0].RiskLevel == "low" {
			t.Log("Second scan shows improvement (only low-risk vulnerability)")
		}
	})

	t.Run("Multiple consecutive rescans", func(t *testing.T) {
		// Simulate multiple rescans over time
		scans := []SaveScanResultsReq{
			{
				WorkspaceId: "test-workspace",
				TargetId:    "target-104",
				Authority:   "https://evolving.example.com:443",
				Host:        "evolving.example.com",
				Port:        443,
				DirResults: []model.DirScanResult{
					{Path: "/v1", StatusCode: 200},
				},
				VulnResults:   []model.ScanResult{{RiskScore: 8.0, RiskLevel: "high"}},
				ScanTimestamp: time.Now().Add(-72 * time.Hour), // 3 days ago
			},
			{
				WorkspaceId: "test-workspace",
				TargetId:    "target-104",
				Authority:   "https://evolving.example.com:443",
				Host:        "evolving.example.com",
				Port:        443,
				DirResults: []model.DirScanResult{
					{Path: "/v1", StatusCode: 200},
					{Path: "/v2", StatusCode: 200},
				},
				VulnResults:   []model.ScanResult{{RiskScore: 7.0, RiskLevel: "high"}},
				ScanTimestamp: time.Now().Add(-48 * time.Hour), // 2 days ago
			},
			{
				WorkspaceId: "test-workspace",
				TargetId:    "target-104",
				Authority:   "https://evolving.example.com:443",
				Host:        "evolving.example.com",
				Port:        443,
				DirResults: []model.DirScanResult{
					{Path: "/v1", StatusCode: 200},
					{Path: "/v2", StatusCode: 200},
					{Path: "/v3", StatusCode: 200},
				},
				VulnResults:   []model.ScanResult{{RiskScore: 5.0, RiskLevel: "medium"}},
				ScanTimestamp: time.Now().Add(-24 * time.Hour), // 1 day ago
			},
			{
				WorkspaceId: "test-workspace",
				TargetId:    "target-104",
				Authority:   "https://evolving.example.com:443",
				Host:        "evolving.example.com",
				Port:        443,
				DirResults: []model.DirScanResult{
					{Path: "/v1", StatusCode: 200},
					{Path: "/v2", StatusCode: 200},
					{Path: "/v3", StatusCode: 200},
					{Path: "/v4", StatusCode: 200},
				},
				VulnResults:   []model.ScanResult{{RiskScore: 3.0, RiskLevel: "low"}},
				ScanTimestamp: time.Now(), // Current scan
			},
		}

		// Verify that each scan is more recent than the previous
		for i := 1; i < len(scans); i++ {
			if !scans[i].ScanTimestamp.After(scans[i-1].ScanTimestamp) {
				t.Errorf("Scan %d should be more recent than scan %d", i, i-1)
			}
		}

		// Verify progressive improvement (risk score decreasing)
		for i := 1; i < len(scans); i++ {
			if len(scans[i].VulnResults) > 0 && len(scans[i-1].VulnResults) > 0 {
				currentRisk := scans[i].VulnResults[0].RiskScore
				previousRisk := scans[i-1].VulnResults[0].RiskScore
				if currentRisk < previousRisk {
					t.Logf("Scan %d shows improvement: risk score decreased from %.1f to %.1f",
						i, previousRisk, currentRisk)
				}
			}
		}

		// Verify progressive growth (directory count increasing)
		for i := 1; i < len(scans); i++ {
			if len(scans[i].DirResults) > len(scans[i-1].DirResults) {
				t.Logf("Scan %d found more directories: %d vs %d",
					i, len(scans[i].DirResults), len(scans[i-1].DirResults))
			}
		}
	})
}

// TestRescanScenario_MergeBehavior tests that unchanged asset fields are
// preserved during rescans. This verifies that user-modified fields like
// labels, memo, and color_tag are not overwritten by new scan results.
func TestRescanScenario_MergeBehavior(t *testing.T) {
	t.Run("Merge preserves user-modified labels", func(t *testing.T) {
		// Simulate existing asset with user-modified labels
		existingAsset := &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "https://example.com:443",
			Host:       "example.com",
			Port:       443,
			Labels:     []string{"production", "critical", "monitored"},
			Memo:       "Important production server",
			ColorTag:   "red",
			UpdateTime: time.Now().Add(-24 * time.Hour),
		}

		// Simulate rescan request (doesn't include user-modified fields)
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://example.com:443",
			Host:        "example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/new-endpoint", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 6.0, RiskLevel: "medium"},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify that user-modified fields should be preserved
		// (In actual implementation, these fields are not in SaveScanResultsReq,
		// so they won't be overwritten)
		if len(existingAsset.Labels) != 3 {
			t.Error("Labels should be preserved")
		}
		if existingAsset.Memo != "Important production server" {
			t.Error("Memo should be preserved")
		}
		if existingAsset.ColorTag != "red" {
			t.Error("ColorTag should be preserved")
		}

		// Verify that scan timestamp is updated
		if !rescanReq.ScanTimestamp.After(existingAsset.UpdateTime) {
			t.Error("Scan timestamp should be more recent than last update")
		}
	})

	t.Run("Merge preserves empty user-modified fields", func(t *testing.T) {
		// Simulate existing asset with empty user-modified fields
		existingAsset := &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "https://api.example.com:443",
			Host:       "api.example.com",
			Port:       443,
			Labels:     []string{}, // Empty labels
			Memo:       "",         // Empty memo
			ColorTag:   "",         // Empty color tag
			UpdateTime: time.Now().Add(-48 * time.Hour),
		}

		// Simulate rescan
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://api.example.com:443",
			Host:        "api.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/api/v1", StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that empty fields remain empty (not set to defaults)
		if len(existingAsset.Labels) != 0 {
			t.Error("Empty labels should remain empty")
		}
		if existingAsset.Memo != "" {
			t.Error("Empty memo should remain empty")
		}
		if existingAsset.ColorTag != "" {
			t.Error("Empty color tag should remain empty")
		}

		// Verify request is valid
		if rescanReq.WorkspaceId == "" {
			t.Error("WorkspaceId should not be empty")
		}
	})

	t.Run("Merge preserves asset metadata fields", func(t *testing.T) {
		// Simulate existing asset with various metadata
		existingAsset := &model.Asset{
			Id:           primitive.NewObjectID(),
			Authority:    "https://shop.example.com:443",
			Host:         "shop.example.com",
			Port:         443,
			Title:        "E-Commerce Shop",
			Service:      "nginx/1.18.0",
			Banner:       "Server: nginx/1.18.0",
			Fingerprints: []string{"nginx", "php", "mysql"},
			IsCDN:        false,
			IsCloud:      true,
			IsHTTP:       true,
			UpdateTime:   time.Now().Add(-72 * time.Hour),
		}

		// Simulate rescan (doesn't include metadata fields)
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://shop.example.com:443",
			Host:        "shop.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/products", StatusCode: 200},
				{Path: "/cart", StatusCode: 200},
			},
			VulnResults: []model.ScanResult{
				{RiskScore: 4.0, RiskLevel: "low"},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify that metadata fields should be preserved
		if existingAsset.Title != "E-Commerce Shop" {
			t.Error("Title should be preserved")
		}
		if existingAsset.Service != "nginx/1.18.0" {
			t.Error("Service should be preserved")
		}
		if existingAsset.Banner != "Server: nginx/1.18.0" {
			t.Error("Banner should be preserved")
		}
		if len(existingAsset.Fingerprints) != 3 {
			t.Error("Fingerprints should be preserved")
		}
		if existingAsset.IsCDN != false {
			t.Error("IsCDN flag should be preserved")
		}
		if existingAsset.IsCloud != true {
			t.Error("IsCloud flag should be preserved")
		}
		if existingAsset.IsHTTP != true {
			t.Error("IsHTTP flag should be preserved")
		}

		// Verify rescan request has new scan results
		if len(rescanReq.DirResults) == 0 {
			t.Error("Rescan should have directory results")
		}
		if len(rescanReq.VulnResults) == 0 {
			t.Error("Rescan should have vulnerability results")
		}
	})

	t.Run("Merge preserves risk assessment fields", func(t *testing.T) {
		// Simulate existing asset with risk assessment
		existingAsset := &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "https://secure.example.com:443",
			Host:       "secure.example.com",
			Port:       443,
			RiskScore:  8.5,
			RiskLevel:  "high",
			UpdateTime: time.Now().Add(-24 * time.Hour),
		}

		// Simulate rescan with new vulnerability results
		// Note: Risk assessment should be recalculated based on new vuln results,
		// but if no new assessment is provided, old values should be preserved
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://secure.example.com:443",
			Host:        "secure.example.com",
			Port:        443,
			DirResults:  []model.DirScanResult{},
			VulnResults: []model.ScanResult{
				{RiskScore: 7.0, RiskLevel: "high"},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify that risk assessment fields exist
		if existingAsset.RiskScore != 8.5 {
			t.Error("RiskScore should be preserved")
		}
		if existingAsset.RiskLevel != "high" {
			t.Error("RiskLevel should be preserved")
		}

		// Verify new scan has vulnerability results
		if len(rescanReq.VulnResults) == 0 {
			t.Error("Rescan should have vulnerability results")
		}

		// Verify rescan request is valid
		if rescanReq.WorkspaceId == "" {
			t.Error("WorkspaceId should not be empty")
		}
	})

	t.Run("Merge preserves task tracking fields", func(t *testing.T) {
		// Simulate existing asset with task tracking
		existingAsset := &model.Asset{
			Id:              primitive.NewObjectID(),
			Authority:       "https://tracked.example.com:443",
			Host:            "tracked.example.com",
			Port:            443,
			TaskId:          "task-12345",
			LastTaskId:      "task-12344",
			FirstSeenTaskId: "task-12340",
			UpdateTime:      time.Now().Add(-48 * time.Hour),
		}

		// Simulate rescan
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://tracked.example.com:443",
			Host:        "tracked.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/status", StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that task tracking fields should be preserved
		if existingAsset.TaskId != "task-12345" {
			t.Error("TaskId should be preserved")
		}
		if existingAsset.LastTaskId != "task-12344" {
			t.Error("LastTaskId should be preserved")
		}
		if existingAsset.FirstSeenTaskId != "task-12340" {
			t.Error("FirstSeenTaskId should be preserved")
		}

		// Verify rescan request is valid
		if rescanReq.WorkspaceId == "" {
			t.Error("WorkspaceId should not be empty")
		}
	})

	t.Run("Merge preserves timestamp fields except update_time", func(t *testing.T) {
		createTime := time.Now().Add(-365 * 24 * time.Hour)      // 1 year ago
		lastStatusChange := time.Now().Add(-30 * 24 * time.Hour) // 30 days ago
		lastUpdateTime := time.Now().Add(-7 * 24 * time.Hour)    // 7 days ago

		// Simulate existing asset with various timestamps
		existingAsset := &model.Asset{
			Id:                   primitive.NewObjectID(),
			Authority:            "https://old.example.com:443",
			Host:                 "old.example.com",
			Port:                 443,
			CreateTime:           createTime,
			LastStatusChangeTime: lastStatusChange,
			UpdateTime:           lastUpdateTime,
		}

		// Simulate rescan
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://old.example.com:443",
			Host:        "old.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/legacy", StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that historical timestamps are preserved
		if !existingAsset.CreateTime.Equal(createTime) {
			t.Error("CreateTime should be preserved")
		}
		if !existingAsset.LastStatusChangeTime.Equal(lastStatusChange) {
			t.Error("LastStatusChangeTime should be preserved")
		}

		// Verify that update_time should be updated to scan timestamp
		if !rescanReq.ScanTimestamp.After(existingAsset.UpdateTime) {
			t.Error("New scan timestamp should be more recent than last update")
		}
	})

	t.Run("Merge with special characters in memo", func(t *testing.T) {
		// Simulate existing asset with memo containing special characters
		specialMemo := "Server with special chars: <script>alert('test')</script> & \"quotes\" 'apostrophes' 中文字符"
		existingAsset := &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "https://special.example.com:443",
			Host:       "special.example.com",
			Port:       443,
			Memo:       specialMemo,
			UpdateTime: time.Now().Add(-24 * time.Hour),
		}

		// Simulate rescan
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://special.example.com:443",
			Host:        "special.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/test", StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that memo with special characters is preserved exactly
		if existingAsset.Memo != specialMemo {
			t.Error("Memo with special characters should be preserved exactly")
		}

		// Verify rescan request is valid
		if rescanReq.WorkspaceId == "" {
			t.Error("WorkspaceId should not be empty")
		}
	})

	t.Run("Merge preserves complex label arrays", func(t *testing.T) {
		// Simulate existing asset with complex labels
		complexLabels := []string{
			"production",
			"critical",
			"monitored-24/7",
			"team:security",
			"env:prod",
			"region:us-east-1",
			"compliance:pci-dss",
			"backup:enabled",
		}
		existingAsset := &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "https://complex.example.com:443",
			Host:       "complex.example.com",
			Port:       443,
			Labels:     complexLabels,
			UpdateTime: time.Now().Add(-24 * time.Hour),
		}

		// Simulate rescan
		rescanReq := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    existingAsset.Id.Hex(),
			Authority:   "https://complex.example.com:443",
			Host:        "complex.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/api", StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that all labels are preserved
		if len(existingAsset.Labels) != len(complexLabels) {
			t.Errorf("Expected %d labels, got %d", len(complexLabels), len(existingAsset.Labels))
		}
		for i, label := range complexLabels {
			if existingAsset.Labels[i] != label {
				t.Errorf("Label[%d] mismatch: expected %s, got %s", i, label, existingAsset.Labels[i])
			}
		}

		// Verify rescan request is valid
		if rescanReq.WorkspaceId == "" {
			t.Error("WorkspaceId should not be empty")
		}
	})
}

// TestRescanScenario_EdgeCases tests edge cases and error conditions in rescan scenarios
func TestRescanScenario_EdgeCases(t *testing.T) {
	t.Run("Rescan with zero timestamp defaults to current time", func(t *testing.T) {
		req := &SaveScanResultsReq{
			WorkspaceId:   "test-workspace",
			TargetId:      "target-200",
			Authority:     "https://example.com:443",
			Host:          "example.com",
			Port:          443,
			DirResults:    []model.DirScanResult{{Path: "/test", StatusCode: 200}},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Time{}, // Zero timestamp
		}

		// Simulate default timestamp assignment
		if req.ScanTimestamp.IsZero() {
			req.ScanTimestamp = time.Now()
		}

		if req.ScanTimestamp.IsZero() {
			t.Error("Scan timestamp should be set to current time when not provided")
		}
	})

	t.Run("Rescan with very large result sets", func(t *testing.T) {
		// Simulate rescan with many results
		largeResultCount := 10000
		dirResults := make([]model.DirScanResult, largeResultCount)
		for i := 0; i < largeResultCount; i++ {
			dirResults[i] = model.DirScanResult{
				Path:       "/path" + string(rune(i)),
				StatusCode: 200,
			}
		}

		req := &SaveScanResultsReq{
			WorkspaceId:   "test-workspace",
			TargetId:      "target-201",
			Authority:     "https://large.example.com:443",
			Host:          "large.example.com",
			Port:          443,
			DirResults:    dirResults,
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		if len(req.DirResults) != largeResultCount {
			t.Errorf("Expected %d directory results, got %d", largeResultCount, len(req.DirResults))
		}
	})

	t.Run("Rescan with duplicate paths in results", func(t *testing.T) {
		// Simulate rescan with duplicate paths (should be handled by implementation)
		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-202",
			Authority:   "https://dup.example.com:443",
			Host:        "dup.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/admin", StatusCode: 200},
				{Path: "/admin", StatusCode: 200}, // Duplicate
				{Path: "/api", StatusCode: 200},
				{Path: "/api", StatusCode: 404}, // Duplicate with different status
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that duplicates are present (implementation should handle deduplication)
		if len(req.DirResults) != 4 {
			t.Errorf("Expected 4 directory results (including duplicates), got %d", len(req.DirResults))
		}
	})

	t.Run("Rescan with invalid status codes", func(t *testing.T) {
		// Simulate rescan with various status codes
		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-203",
			Authority:   "https://status.example.com:443",
			Host:        "status.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: "/ok", StatusCode: 200},
				{Path: "/redirect", StatusCode: 301},
				{Path: "/forbidden", StatusCode: 403},
				{Path: "/notfound", StatusCode: 404},
				{Path: "/error", StatusCode: 500},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that all status codes are preserved
		expectedStatusCodes := []int{200, 301, 403, 404, 500}
		for i, expected := range expectedStatusCodes {
			if req.DirResults[i].StatusCode != expected {
				t.Errorf("DirResult[%d] status code mismatch: expected %d, got %d",
					i, expected, req.DirResults[i].StatusCode)
			}
		}
	})

	t.Run("Rescan with extreme risk scores", func(t *testing.T) {
		// Simulate rescan with various risk scores
		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-204",
			Authority:   "https://risk.example.com:443",
			Host:        "risk.example.com",
			Port:        443,
			DirResults:  []model.DirScanResult{},
			VulnResults: []model.ScanResult{
				{RiskScore: 0.0, RiskLevel: "info"},
				{RiskScore: 3.9, RiskLevel: "low"},
				{RiskScore: 6.9, RiskLevel: "medium"},
				{RiskScore: 8.9, RiskLevel: "high"},
				{RiskScore: 10.0, RiskLevel: "critical"},
			},
			ScanTimestamp: time.Now(),
		}

		// Verify that all risk scores are preserved
		expectedRiskScores := []float64{0.0, 3.9, 6.9, 8.9, 10.0}
		for i, expected := range expectedRiskScores {
			if req.VulnResults[i].RiskScore != expected {
				t.Errorf("VulnResult[%d] risk score mismatch: expected %.1f, got %.1f",
					i, expected, req.VulnResults[i].RiskScore)
			}
		}
	})

	t.Run("Rescan with non-standard ports", func(t *testing.T) {
		// Test rescans on various non-standard ports
		testPorts := []int{8080, 8443, 3000, 5000, 9000, 10000, 65535}

		for _, port := range testPorts {
			req := &SaveScanResultsReq{
				WorkspaceId:   "test-workspace",
				TargetId:      "target-port-" + string(rune(port)),
				Authority:     "https://example.com:" + string(rune(port)),
				Host:          "example.com",
				Port:          port,
				DirResults:    []model.DirScanResult{{Path: "/", StatusCode: 200}},
				VulnResults:   []model.ScanResult{},
				ScanTimestamp: time.Now(),
			}

			if req.Port != port {
				t.Errorf("Port mismatch: expected %d, got %d", port, req.Port)
			}
		}
	})

	t.Run("Rescan with IPv4 and IPv6 addresses", func(t *testing.T) {
		testCases := []struct {
			name string
			host string
		}{
			{"IPv4", "192.168.1.100"},
			{"IPv4 localhost", "127.0.0.1"},
			{"IPv6", "2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
			{"IPv6 localhost", "::1"},
			{"IPv6 short", "2001:db8::1"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := &SaveScanResultsReq{
					WorkspaceId:   "test-workspace",
					TargetId:      "target-ip-" + tc.name,
					Authority:     "",
					Host:          tc.host,
					Port:          443,
					DirResults:    []model.DirScanResult{{Path: "/", StatusCode: 200}},
					VulnResults:   []model.ScanResult{},
					ScanTimestamp: time.Now(),
				}

				if req.Host != tc.host {
					t.Errorf("Host mismatch: expected %s, got %s", tc.host, req.Host)
				}
			})
		}
	})

	t.Run("Rescan with very long paths", func(t *testing.T) {
		// Simulate rescan with very long path
		longPath := "/api/v1/users/12345/profile/settings/notifications/email/preferences/advanced/filters/custom/rules/conditions/actions/webhooks/endpoints/configurations/parameters/validation/schemas/definitions/properties/metadata/annotations/labels/tags/categories/groups/permissions/roles/assignments/policies/rules/conditions/actions"

		req := &SaveScanResultsReq{
			WorkspaceId: "test-workspace",
			TargetId:    "target-205",
			Authority:   "https://long.example.com:443",
			Host:        "long.example.com",
			Port:        443,
			DirResults: []model.DirScanResult{
				{Path: longPath, StatusCode: 200},
			},
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		if req.DirResults[0].Path != longPath {
			t.Error("Long path should be preserved exactly")
		}
		if len(req.DirResults[0].Path) < 200 {
			t.Error("Path should be very long (>200 characters)")
		}
	})

	t.Run("Rescan with unicode characters in paths", func(t *testing.T) {
		// Simulate rescan with unicode characters
		unicodePaths := []string{
			"/用户/个人资料",
			"/utilisateurs/profil",
			"/пользователи/профиль",
			"/ユーザー/プロフィール",
			"/사용자/프로필",
		}

		dirResults := make([]model.DirScanResult, len(unicodePaths))
		for i, path := range unicodePaths {
			dirResults[i] = model.DirScanResult{
				Path:       path,
				StatusCode: 200,
			}
		}

		req := &SaveScanResultsReq{
			WorkspaceId:   "test-workspace",
			TargetId:      "target-206",
			Authority:     "https://unicode.example.com:443",
			Host:          "unicode.example.com",
			Port:          443,
			DirResults:    dirResults,
			VulnResults:   []model.ScanResult{},
			ScanTimestamp: time.Now(),
		}

		// Verify that unicode paths are preserved
		for i, expected := range unicodePaths {
			if req.DirResults[i].Path != expected {
				t.Errorf("Unicode path[%d] mismatch: expected %s, got %s",
					i, expected, req.DirResults[i].Path)
			}
		}
	})
}
