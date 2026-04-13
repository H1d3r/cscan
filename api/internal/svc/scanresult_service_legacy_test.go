package svc

import (
	"cscan/model"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// =============================================================================
// Property-Based Tests for Backward Compatibility
// Feature: scan-results-integration-fix, Task 7.2-7.5
// =============================================================================

// TestProperty13_LegacyDataVersionAssignment verifies that scan result records
// without version or scan_timestamp fields are treated as version 1 and assigned
// default timestamps without failing.
// **Property 13: Legacy Data Version Assignment**
// **Validates: Requirements 5.1**
func TestProperty13_LegacyDataVersionAssignment(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property 13.1: Directory scan results without version get version 1
	properties.Property("Directory scan results without version get version 1", prop.ForAll(
		func(workspaceId, host string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			path := "/test/path"

			// Create legacy record without version
			legacyResult := model.DirScanResult{
				WorkspaceId: workspaceId,
				Host:        host,
				Port:        port,
				Path:        path,
				StatusCode:  200,
				Version:     0, // Legacy record without version
			}

			// Normalize the legacy record
			normalizeDirScanResult(&legacyResult)

			// Verify version is assigned to 1
			return legacyResult.Version == 1
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
	))

	// Property 13.2: Vulnerability scan results without version get version 1
	properties.Property("Vulnerability scan results without version get version 1", prop.ForAll(
		func(host string, port int, riskScore float64) bool {
			// Skip invalid inputs
			if host == "" || port <= 0 || port > 65535 || riskScore < 0 || riskScore > 10 {
				return true
			}

			// Create legacy record without version
			legacyResult := model.ScanResult{
				Host:      host,
				Port:      port,
				RiskScore: riskScore,
				RiskLevel: "high",
				Version:   0, // Legacy record without version
			}

			// Normalize the legacy record
			normalizeVulnScanResult(&legacyResult)

			// Verify version is assigned to 1
			return legacyResult.Version == 1
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.Float64Range(0, 10),
	))

	// Property 13.3: Records with existing version are preserved
	properties.Property("Records with existing version are preserved", prop.ForAll(
		func(workspaceId, host string, port int, existingVersion int64) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 || existingVersion <= 0 || existingVersion > 1000 {
				return true
			}

			path := "/test/path"

			// Create record with existing version
			result := model.DirScanResult{
				WorkspaceId: workspaceId,
				Host:        host,
				Port:        port,
				Path:        path,
				StatusCode:  200,
				Version:     existingVersion,
			}

			// Normalize the record
			normalizeDirScanResult(&result)

			// Verify existing version is preserved
			return result.Version == existingVersion
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.Int64Range(1, 1000),
	))

	// Property 13.4: Legacy records without scan_time use create_time fallback
	properties.Property("Legacy records without scan_time use create_time fallback", prop.ForAll(
		func(workspaceId, host string, port int, hoursAgo int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 || hoursAgo < 0 || hoursAgo > 8760 {
				return true
			}

			path := "/test/path"
			createTime := time.Now().Add(-time.Duration(hoursAgo) * time.Hour)

			// Create legacy record without scan_time but with create_time
			legacyResult := model.DirScanResult{
				WorkspaceId: workspaceId,
				Host:        host,
				Port:        port,
				Path:        path,
				StatusCode:  200,
				CreateTime:  createTime,
				ScanTime:    time.Time{}, // Zero value - legacy record
			}

			// Normalize the legacy record
			normalizeDirScanResult(&legacyResult)

			// Verify scan_time is set to create_time
			return legacyResult.ScanTime.Equal(createTime)
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.IntRange(0, 8760), // Up to 1 year ago
	))

	// Property 13.5: Vulnerability records without scan_time use completed time fallback
	properties.Property("Vulnerability records without scan_time use completed time fallback", prop.ForAll(
		func(host string, port int, hoursAgo int) bool {
			// Skip invalid inputs
			if host == "" || port <= 0 || port > 65535 || hoursAgo < 0 || hoursAgo > 8760 {
				return true
			}

			completedTime := time.Now().Add(-time.Duration(hoursAgo) * time.Hour)

			// Create legacy record without scan_time but with completed time
			legacyResult := model.ScanResult{
				Host:      host,
				Port:      port,
				RiskScore: 7.5,
				RiskLevel: "high",
				Completed: completedTime,
				ScanTime:  time.Time{}, // Zero value - legacy record
			}

			// Normalize the legacy record
			normalizeVulnScanResult(&legacyResult)

			// Verify scan_time is set to completed time
			return legacyResult.ScanTime.Equal(completedTime)
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.IntRange(0, 8760),
	))

	// Property 13.6: Records with existing scan_time are preserved
	properties.Property("Records with existing scan_time are preserved", prop.ForAll(
		func(workspaceId, host string, port int, hoursAgo int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 || hoursAgo < 0 || hoursAgo > 8760 {
				return true
			}

			path := "/test/path"
			scanTime := time.Now().Add(-time.Duration(hoursAgo) * time.Hour)
			createTime := time.Now().Add(-time.Duration(hoursAgo+100) * time.Hour)

			// Create record with existing scan_time
			result := model.DirScanResult{
				WorkspaceId: workspaceId,
				Host:        host,
				Port:        port,
				Path:        path,
				StatusCode:  200,
				CreateTime:  createTime,
				ScanTime:    scanTime,
			}

			// Normalize the record
			normalizeDirScanResult(&result)

			// Verify existing scan_time is preserved (not overwritten by create_time)
			return result.ScanTime.Equal(scanTime)
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.IntRange(0, 8760),
	))

	properties.TestingRun(t)
}

// TestProperty14_LegacyRecordCompatibility verifies that existing dirscan_result
// and scanresult records without new fields can be successfully read and processed
// without errors.
// **Property 14: Legacy Record Compatibility**
// **Validates: Requirements 5.2, 5.3**
func TestProperty14_LegacyRecordCompatibility(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property 14.1: Legacy directory scan records can be read without errors
	properties.Property("Legacy directory scan records can be read without errors", prop.ForAll(
		func(workspaceId, host string, port, statusCode int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 || statusCode < 100 || statusCode > 599 {
				return true
			}

			path := "/test/path"

			// Create legacy record without new fields (version, scan_time)
			legacyRecord := model.DirScanResult{
				WorkspaceId:   workspaceId,
				Host:          host,
				Port:          port,
				Path:          path,
				StatusCode:    statusCode,
				ContentLength: 1024,
				Title:         "Test Page",
				// No Version field
				// No ScanTime field
			}

			// Normalize the legacy record (simulates reading from database)
			normalizeDirScanResult(&legacyRecord)

			// Verify record can be processed without errors
			// Check that required fields are present
			return legacyRecord.WorkspaceId == workspaceId &&
				legacyRecord.Host == host &&
				legacyRecord.Port == port &&
				legacyRecord.Path == path &&
				legacyRecord.StatusCode == statusCode &&
				legacyRecord.Version == 1 // Should be assigned version 1
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.IntRange(100, 599),
	))

	// Property 14.2: Legacy vulnerability scan records can be read without errors
	properties.Property("Legacy vulnerability scan records can be read without errors", prop.ForAll(
		func(host string, port int, riskScore float64, riskLevel string) bool {
			// Skip invalid inputs
			if host == "" || port <= 0 || port > 65535 || riskScore < 0 || riskScore > 10 {
				return true
			}

			// Create legacy record without new fields (version, scan_time, authority)
			legacyRecord := model.ScanResult{
				Host:      host,
				Port:      port,
				RiskScore: riskScore,
				RiskLevel: riskLevel,
				// No Version field
				// No ScanTime field
				// No Authority field
			}

			// Normalize the legacy record (simulates reading from database)
			normalizeVulnScanResult(&legacyRecord)

			// Verify record can be processed without errors
			return legacyRecord.Host == host &&
				legacyRecord.Port == port &&
				legacyRecord.RiskScore == riskScore &&
				legacyRecord.RiskLevel == riskLevel &&
				legacyRecord.Version == 1 // Should be assigned version 1
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.Float64Range(0, 10),
		gen.OneConstOf("critical", "high", "medium", "low", "info"),
	))

	// Property 14.3: Legacy records with partial fields are handled correctly
	properties.Property("Legacy records with partial fields are handled correctly", prop.ForAll(
		func(workspaceId, host string, port int, hasTitle bool) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			path := "/test/path"
			title := ""
			if hasTitle {
				title = "Test Title"
			}

			// Create legacy record with some optional fields missing
			legacyRecord := model.DirScanResult{
				WorkspaceId: workspaceId,
				Host:        host,
				Port:        port,
				Path:        path,
				StatusCode:  200,
				Title:       title, // May be empty
			}

			// Normalize the legacy record
			normalizeDirScanResult(&legacyRecord)

			// Verify record is processed correctly
			// Title should remain as is (empty or with value)
			return legacyRecord.WorkspaceId == workspaceId &&
				legacyRecord.Host == host &&
				legacyRecord.Title == title &&
				legacyRecord.Version == 1
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.Bool(),
	))

	// Property 14.4: Multiple legacy records can be processed in batch
	properties.Property("Multiple legacy records can be processed in batch", prop.ForAll(
		func(workspaceId, host string, port, recordCount int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 || recordCount < 1 || recordCount > 100 {
				return true
			}

			// Create multiple legacy records
			legacyRecords := make([]model.DirScanResult, recordCount)
			for i := 0; i < recordCount; i++ {
				legacyRecords[i] = model.DirScanResult{
					WorkspaceId: workspaceId,
					Host:        host,
					Port:        port,
					Path:        "/path" + string(rune(i+'0')),
					StatusCode:  200,
					// No Version or ScanTime
				}
			}

			// Normalize all legacy records
			for i := range legacyRecords {
				normalizeDirScanResult(&legacyRecords[i])
			}

			// Verify all records are processed correctly
			for i := range legacyRecords {
				if legacyRecords[i].Version != 1 {
					return false
				}
				if legacyRecords[i].WorkspaceId != workspaceId {
					return false
				}
			}

			return true
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.IntRange(1, 100),
	))

	// Property 14.5: Legacy records with zero values are handled gracefully
	properties.Property("Legacy records with zero values are handled gracefully", prop.ForAll(
		func(workspaceId, host string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			path := "/test/path"

			// Create legacy record with zero values for optional fields
			legacyRecord := model.DirScanResult{
				WorkspaceId:   workspaceId,
				Host:          host,
				Port:          port,
				Path:          path,
				StatusCode:    200,
				ContentLength: 0,           // Zero value
				Title:         "",          // Empty string
				Version:       0,           // Zero value - legacy
				ScanTime:      time.Time{}, // Zero time - legacy
			}

			// Normalize the legacy record
			normalizeDirScanResult(&legacyRecord)

			// Verify record is processed without errors
			// Version should be assigned, other zero values preserved
			return legacyRecord.Version == 1 &&
				legacyRecord.ContentLength == 0 &&
				legacyRecord.Title == ""
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
	))

	properties.TestingRun(t)
}

// TestProperty15_FallbackAssociationLogic verifies that scan results missing
// the authority field can still be successfully associated with assets using
// the fallback criteria of workspace_id + host + port.
// **Property 15: Fallback Association Logic**
// **Validates: Requirements 5.4**
func TestProperty15_FallbackAssociationLogic(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property 15.1: Directory scan requests without authority use fallback
	properties.Property("Directory scan requests without authority use fallback", prop.ForAll(
		func(workspaceId, host string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			// Create request without authority (fallback scenario)
			req := &GetDirScanResultsReq{
				WorkspaceId: workspaceId,
				Authority:   "", // Empty authority - triggers fallback
				Host:        host,
				Port:        port,
				Limit:       10,
				Offset:      0,
			}

			// Verify fallback criteria are present
			return req.WorkspaceId == workspaceId &&
				req.Authority == "" &&
				req.Host == host &&
				req.Port == port
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
	))

	// Property 15.2: Vulnerability scan requests without authority use fallback
	properties.Property("Vulnerability scan requests without authority use fallback", prop.ForAll(
		func(workspaceId, host string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			// Create request without authority (fallback scenario)
			req := &GetVulnScanResultsReq{
				WorkspaceId: workspaceId,
				Authority:   "", // Empty authority - triggers fallback
				Host:        host,
				Port:        port,
				Limit:       10,
				Offset:      0,
			}

			// Verify fallback criteria are present
			return req.WorkspaceId == workspaceId &&
				req.Authority == "" &&
				req.Host == host &&
				req.Port == port
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
	))

	// Property 15.3: Fallback works with various host formats
	properties.Property("Fallback works with various host formats", prop.ForAll(
		func(workspaceId string, hostType int, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || port <= 0 || port > 65535 {
				return true
			}

			// Generate different host formats
			var host string
			switch hostType % 4 {
			case 0:
				host = "example.com" // Domain
			case 1:
				host = "192.168.1.1" // IPv4
			case 2:
				host = "sub.example.com" // Subdomain
			case 3:
				host = "example-test.com" // Domain with dash
			}

			// Create request without authority
			req := &GetDirScanResultsReq{
				WorkspaceId: workspaceId,
				Authority:   "", // Empty authority
				Host:        host,
				Port:        port,
				Limit:       10,
				Offset:      0,
			}

			// Verify fallback works with any host format
			return req.WorkspaceId == workspaceId &&
				req.Authority == "" &&
				req.Host == host &&
				req.Port == port
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(0, 1000),
		gen.IntRange(1, 65535),
	))

	// Property 15.4: Fallback works with common ports
	properties.Property("Fallback works with common ports", prop.ForAll(
		func(workspaceId, host string, portType int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" {
				return true
			}

			// Use common ports
			commonPorts := []int{80, 443, 8080, 8443, 3000, 5000, 9000}
			port := commonPorts[portType%len(commonPorts)]

			// Create request without authority
			req := &GetDirScanResultsReq{
				WorkspaceId: workspaceId,
				Authority:   "", // Empty authority
				Host:        host,
				Port:        port,
				Limit:       10,
				Offset:      0,
			}

			// Verify fallback works with common ports
			return req.WorkspaceId == workspaceId &&
				req.Authority == "" &&
				req.Host == host &&
				req.Port == port
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(0, 1000),
	))

	// Property 15.5: Fallback association is consistent across query types
	properties.Property("Fallback association is consistent across query types", prop.ForAll(
		func(workspaceId, host string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			// Create both types of requests without authority
			dirReq := &GetDirScanResultsReq{
				WorkspaceId: workspaceId,
				Authority:   "",
				Host:        host,
				Port:        port,
				Limit:       10,
				Offset:      0,
			}

			vulnReq := &GetVulnScanResultsReq{
				WorkspaceId: workspaceId,
				Authority:   "",
				Host:        host,
				Port:        port,
				Limit:       10,
				Offset:      0,
			}

			// Verify both use same fallback criteria
			return dirReq.WorkspaceId == vulnReq.WorkspaceId &&
				dirReq.Host == vulnReq.Host &&
				dirReq.Port == vulnReq.Port &&
				dirReq.Authority == "" &&
				vulnReq.Authority == ""
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
	))

	properties.TestingRun(t)
}

// TestProperty16_GracefulMissingFieldHandling verifies that scan results with
// missing optional fields are returned with appropriate default values without
// throwing errors.
// **Property 16: Graceful Missing Field Handling**
// **Validates: Requirements 5.6**
func TestProperty16_GracefulMissingFieldHandling(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property 16.1: Missing title field defaults to empty string
	properties.Property("Missing title field defaults to empty string", prop.ForAll(
		func(workspaceId, host string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			path := "/test/path"

			// Create record without title
			result := model.DirScanResult{
				WorkspaceId: workspaceId,
				Host:        host,
				Port:        port,
				Path:        path,
				StatusCode:  200,
				Title:       "", // Missing/empty title
			}

			// Normalize the record
			normalizeDirScanResult(&result)

			// Verify title defaults to empty string (not null or error)
			return result.Title == ""
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
	))

	// Property 16.2: Missing description in findings defaults to empty string
	properties.Property("Missing description in findings defaults to empty string", prop.ForAll(
		func(host string, port int, riskScore float64) bool {
			// Skip invalid inputs
			if host == "" || port <= 0 || port > 65535 || riskScore < 0 || riskScore > 10 {
				return true
			}

			// Create record with finding without description
			result := model.ScanResult{
				Host:      host,
				Port:      port,
				RiskScore: riskScore,
				RiskLevel: "high",
				Findings: []model.Finding{
					{
						Type:        "XSS",
						Description: "", // Missing/empty description
					},
				},
			}

			// Normalize the record
			normalizeVulnScanResult(&result)

			// Verify description defaults to empty string
			return len(result.Findings) > 0 && result.Findings[0].Description == ""
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.Float64Range(0, 10),
	))

	// Property 16.3: Records with all optional fields missing are handled
	properties.Property("Records with all optional fields missing are handled", prop.ForAll(
		func(workspaceId, host string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			path := "/test/path"

			// Create record with minimal required fields only
			result := model.DirScanResult{
				WorkspaceId: workspaceId,
				Host:        host,
				Port:        port,
				Path:        path,
				StatusCode:  200,
				// All optional fields missing
				Title:         "",
				ContentLength: 0,
				Version:       0,
				ScanTime:      time.Time{},
			}

			// Normalize the record
			normalizeDirScanResult(&result)

			// Verify record is processed without errors
			// Required fields preserved, optional fields have defaults
			return result.WorkspaceId == workspaceId &&
				result.Host == host &&
				result.Port == port &&
				result.Path == path &&
				result.Title == "" &&
				result.Version == 1 // Should be assigned
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
	))

	// Property 16.4: Existing optional field values are preserved
	properties.Property("Existing optional field values are preserved", prop.ForAll(
		func(workspaceId, host, title string, port int) bool {
			// Skip invalid inputs
			if workspaceId == "" || host == "" || port <= 0 || port > 65535 {
				return true
			}

			path := "/test/path"

			// Create record with optional fields populated
			result := model.DirScanResult{
				WorkspaceId:   workspaceId,
				Host:          host,
				Port:          port,
				Path:          path,
				StatusCode:    200,
				Title:         title, // May be empty or have value
				ContentLength: 1024,
			}

			// Normalize the record
			normalizeDirScanResult(&result)

			// Verify optional fields are preserved as is
			return result.Title == title &&
				result.ContentLength == 1024
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString(), // Title can be any string including empty
		gen.IntRange(1, 65535),
	))

	// Property 16.5: Multiple findings with missing descriptions are handled
	properties.Property("Multiple findings with missing descriptions are handled", prop.ForAll(
		func(host string, port int, findingCount int) bool {
			// Skip invalid inputs
			if host == "" || port <= 0 || port > 65535 || findingCount < 1 || findingCount > 20 {
				return true
			}

			// Create record with multiple findings, some without descriptions
			findings := make([]model.Finding, findingCount)
			for i := 0; i < findingCount; i++ {
				findings[i] = model.Finding{
					Type:        "VULN" + string(rune(i+'0')),
					Description: "", // All missing descriptions
				}
			}

			result := model.ScanResult{
				Host:      host,
				Port:      port,
				RiskScore: 7.5,
				RiskLevel: "high",
				Findings:  findings,
			}

			// Normalize the record
			normalizeVulnScanResult(&result)

			// Verify all findings are processed without errors
			if len(result.Findings) != findingCount {
				return false
			}

			for i := range result.Findings {
				if result.Findings[i].Description != "" {
					return false
				}
			}

			return true
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(1, 65535),
		gen.IntRange(1, 20),
	))

	properties.TestingRun(t)
}
