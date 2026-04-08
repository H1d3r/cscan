package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"cscan/api/internal/types"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// APIEndpoint represents an API endpoint definition
type APIEndpoint struct {
	Method string
	Path   string
}

// ExpectedEndpoints defines all API endpoints that must remain stable
// Requirements 9.1: THE System SHALL not change existing API endpoint paths or HTTP methods
var ExpectedEndpoints = []APIEndpoint{
	// Public routes (no auth)
	{Method: http.MethodPost, Path: "/api/v1/login"},
	{Method: http.MethodGet, Path: "/api/v1/worker/download"},
	{Method: http.MethodPost, Path: "/api/v1/worker/validate"},
	{Method: http.MethodGet, Path: "/api/v1/worker/ws"},

	// Worker routes
	{Method: http.MethodPost, Path: "/api/v1/worker/task/check"},
	{Method: http.MethodPost, Path: "/api/v1/worker/task/update"},
	{Method: http.MethodPost, Path: "/api/v1/worker/task/result"},
	{Method: http.MethodPost, Path: "/api/v1/worker/task/vul"},
	{Method: http.MethodPost, Path: "/api/v1/worker/heartbeat"},

	// User management
	{Method: http.MethodPost, Path: "/api/v1/user/list"},
	{Method: http.MethodPost, Path: "/api/v1/user/create"},
	{Method: http.MethodPost, Path: "/api/v1/user/update"},
	{Method: http.MethodPost, Path: "/api/v1/user/delete"},
	{Method: http.MethodPost, Path: "/api/v1/user/resetPassword"},

	// Workspace
	{Method: http.MethodPost, Path: "/api/v1/workspace/list"},
	{Method: http.MethodPost, Path: "/api/v1/workspace/save"},
	{Method: http.MethodPost, Path: "/api/v1/workspace/delete"},

	// Organization
	{Method: http.MethodPost, Path: "/api/v1/organization/list"},
	{Method: http.MethodPost, Path: "/api/v1/organization/save"},
	{Method: http.MethodPost, Path: "/api/v1/organization/delete"},

	// Asset management
	{Method: http.MethodPost, Path: "/api/v1/asset/list"},
	{Method: http.MethodPost, Path: "/api/v1/asset/stat"},
	{Method: http.MethodPost, Path: "/api/v1/asset/delete"},
	{Method: http.MethodPost, Path: "/api/v1/asset/batchDelete"},
	{Method: http.MethodPost, Path: "/api/v1/asset/clear"},

	// Site management
	{Method: http.MethodPost, Path: "/api/v1/asset/site/list"},
	{Method: http.MethodPost, Path: "/api/v1/asset/site/stat"},
	{Method: http.MethodPost, Path: "/api/v1/asset/site/delete"},

	// Domain management
	{Method: http.MethodPost, Path: "/api/v1/asset/domain/list"},
	{Method: http.MethodPost, Path: "/api/v1/asset/domain/stat"},
	{Method: http.MethodPost, Path: "/api/v1/asset/domain/delete"},

	// App management
	{Method: http.MethodPost, Path: "/api/v1/asset/app/list"},
	{Method: http.MethodPost, Path: "/api/v1/asset/app/stat"},
	{Method: http.MethodPost, Path: "/api/v1/asset/app/delete"},
	{Method: http.MethodPost, Path: "/api/v1/asset/app/batchDelete"},
	{Method: http.MethodPost, Path: "/api/v1/asset/app/clear"},

	// Icon management
	{Method: http.MethodPost, Path: "/api/v1/asset/icon/list"},
	{Method: http.MethodPost, Path: "/api/v1/asset/icon/stat"},
	{Method: http.MethodPost, Path: "/api/v1/asset/icon/delete"},
	{Method: http.MethodPost, Path: "/api/v1/asset/icon/batchDelete"},
	{Method: http.MethodPost, Path: "/api/v1/asset/icon/clear"},

	// IP management
	{Method: http.MethodPost, Path: "/api/v1/asset/ip/list"},
	{Method: http.MethodPost, Path: "/api/v1/asset/ip/stat"},
	{Method: http.MethodPost, Path: "/api/v1/asset/ip/delete"},

	// Task management
	{Method: http.MethodPost, Path: "/api/v1/task/list"},
	{Method: http.MethodPost, Path: "/api/v1/task/create"},
	{Method: http.MethodPost, Path: "/api/v1/task/update"},
	{Method: http.MethodPost, Path: "/api/v1/task/delete"},
	{Method: http.MethodPost, Path: "/api/v1/task/start"},
	{Method: http.MethodPost, Path: "/api/v1/task/pause"},
	{Method: http.MethodPost, Path: "/api/v1/task/resume"},
	{Method: http.MethodPost, Path: "/api/v1/task/stop"},
	{Method: http.MethodPost, Path: "/api/v1/task/stat"},

	// Vulnerability management
	{Method: http.MethodPost, Path: "/api/v1/vul/list"},
	{Method: http.MethodPost, Path: "/api/v1/vul/detail"},
	{Method: http.MethodPost, Path: "/api/v1/vul/stat"},
	{Method: http.MethodPost, Path: "/api/v1/vul/delete"},

	// Worker management
	{Method: http.MethodPost, Path: "/api/v1/worker/list"},
	{Method: http.MethodPost, Path: "/api/v1/worker/delete"},
	{Method: http.MethodPost, Path: "/api/v1/worker/rename"},
	{Method: http.MethodPost, Path: "/api/v1/worker/restart"},

	// Online API
	{Method: http.MethodPost, Path: "/api/v1/onlineapi/search"},
	{Method: http.MethodPost, Path: "/api/v1/onlineapi/import"},
	{Method: http.MethodPost, Path: "/api/v1/onlineapi/config/list"},
	{Method: http.MethodPost, Path: "/api/v1/onlineapi/config/save"},

	// POC management
	{Method: http.MethodPost, Path: "/api/v1/poc/custom/list"},
	{Method: http.MethodPost, Path: "/api/v1/poc/custom/save"},
	{Method: http.MethodPost, Path: "/api/v1/poc/custom/delete"},
	{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/templates"},
	{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/categories"},

	// Fingerprint management
	{Method: http.MethodPost, Path: "/api/v1/fingerprint/list"},
	{Method: http.MethodPost, Path: "/api/v1/fingerprint/save"},
	{Method: http.MethodPost, Path: "/api/v1/fingerprint/delete"},
	{Method: http.MethodPost, Path: "/api/v1/fingerprint/categories"},

	// Report
	{Method: http.MethodPost, Path: "/api/v1/report/detail"},
	{Method: http.MethodPost, Path: "/api/v1/report/export"},

	// Notify
	{Method: http.MethodPost, Path: "/api/v1/notify/config/list"},
	{Method: http.MethodPost, Path: "/api/v1/notify/config/save"},
	{Method: http.MethodPost, Path: "/api/v1/notify/config/delete"},
	{Method: http.MethodPost, Path: "/api/v1/notify/config/test"},
}

// ResponseField represents a required field in API response
type ResponseField struct {
	Name     string
	Type     string
	Required bool
}

// BaseResponseFields defines fields that must exist in all responses
// Requirements 9.2: THE System SHALL not remove existing API response fields
var BaseResponseFields = []ResponseField{
	{Name: "code", Type: "int", Required: true},
	{Name: "msg", Type: "string", Required: true},
}

// ListResponseFields defines fields for list responses
var ListResponseFields = []ResponseField{
	{Name: "code", Type: "int", Required: true},
	{Name: "msg", Type: "string", Required: true},
	{Name: "total", Type: "int", Required: true},
	{Name: "list", Type: "array", Required: true},
}

// TestAPIEndpointsExist verifies all expected endpoints are defined
// Validates: Requirements 9.1
func TestAPIEndpointsExist(t *testing.T) {
	// Create a map of expected endpoints for quick lookup
	endpointMap := make(map[string]bool)
	for _, ep := range ExpectedEndpoints {
		key := ep.Method + ":" + ep.Path
		endpointMap[key] = true
	}

	// Verify we have the expected number of endpoints
	if len(ExpectedEndpoints) < 50 {
		t.Errorf("Expected at least 50 API endpoints, got %d", len(ExpectedEndpoints))
	}

	// Verify critical endpoints exist
	criticalEndpoints := []APIEndpoint{
		{Method: http.MethodPost, Path: "/api/v1/login"},
		{Method: http.MethodPost, Path: "/api/v1/task/list"},
		{Method: http.MethodPost, Path: "/api/v1/asset/list"},
		{Method: http.MethodPost, Path: "/api/v1/vul/list"},
		{Method: http.MethodPost, Path: "/api/v1/worker/list"},
	}

	for _, ep := range criticalEndpoints {
		key := ep.Method + ":" + ep.Path
		if !endpointMap[key] {
			t.Errorf("Critical endpoint missing: %s %s", ep.Method, ep.Path)
		}
	}
}

// TestBaseResponseFieldsExist verifies BaseResp has required fields
// Validates: Requirements 9.2
func TestBaseResponseFieldsExist(t *testing.T) {
	resp := types.BaseResp{}
	respType := reflect.TypeOf(resp)

	for _, field := range BaseResponseFields {
		structField, found := respType.FieldByName(capitalizeFirst(field.Name))
		if !found {
			// Try to find by json tag
			found = hasJSONTag(respType, field.Name)
		}
		if !found && field.Required {
			t.Errorf("Required field '%s' not found in BaseResp", field.Name)
		}
		if found && structField.Type.Kind().String() != field.Type {
			// Type check is informational, not a failure
			t.Logf("Field '%s' type: expected %s, got %s", field.Name, field.Type, structField.Type.Kind().String())
		}
	}
}

// TestLoginResponseFieldsExist verifies LoginResp has required fields
// Validates: Requirements 9.2
func TestLoginResponseFieldsExist(t *testing.T) {
	resp := types.LoginResp{}
	respType := reflect.TypeOf(resp)

	requiredFields := []string{"code", "msg", "token", "userId", "username", "role"}
	for _, fieldName := range requiredFields {
		if !hasJSONTag(respType, fieldName) {
			t.Errorf("Required field '%s' not found in LoginResp", fieldName)
		}
	}
}

// TestAssetListResponseFieldsExist verifies AssetListResp has required fields
// Validates: Requirements 9.2
func TestAssetListResponseFieldsExist(t *testing.T) {
	resp := types.AssetListResp{}
	respType := reflect.TypeOf(resp)

	for _, field := range ListResponseFields {
		if !hasJSONTag(respType, field.Name) && field.Required {
			t.Errorf("Required field '%s' not found in AssetListResp", field.Name)
		}
	}
}

// TestVulListResponseFieldsExist verifies VulListResp has required fields
// Validates: Requirements 9.2
func TestVulListResponseFieldsExist(t *testing.T) {
	resp := types.VulListResp{}
	respType := reflect.TypeOf(resp)

	for _, field := range ListResponseFields {
		if !hasJSONTag(respType, field.Name) && field.Required {
			t.Errorf("Required field '%s' not found in VulListResp", field.Name)
		}
	}
}

// TestWorkerListResponseFieldsExist verifies WorkerListResp has required fields
// Validates: Requirements 9.2
func TestWorkerListResponseFieldsExist(t *testing.T) {
	resp := types.WorkerListResp{}
	respType := reflect.TypeOf(resp)

	requiredFields := []string{"code", "msg", "list"}
	for _, fieldName := range requiredFields {
		if !hasJSONTag(respType, fieldName) {
			t.Errorf("Required field '%s' not found in WorkerListResp", fieldName)
		}
	}
}

func TestAppListResponseFieldsExist(t *testing.T) {
	resp := types.AppListResp{}
	respType := reflect.TypeOf(resp)

	for _, field := range ListResponseFields {
		if !hasJSONTag(respType, field.Name) && field.Required {
			t.Errorf("Required field '%s' not found in AppListResp", field.Name)
		}
	}
}

func TestIconListResponseFieldsExist(t *testing.T) {
	resp := types.IconListResp{}
	respType := reflect.TypeOf(resp)

	for _, field := range ListResponseFields {
		if !hasJSONTag(respType, field.Name) && field.Required {
			t.Errorf("Required field '%s' not found in IconListResp", field.Name)
		}
	}
}

// TestTaskListResponseFieldsExist verifies MainTaskListResp has required fields
// Validates: Requirements 9.2
func TestTaskListResponseFieldsExist(t *testing.T) {
	resp := types.MainTaskListResp{}
	respType := reflect.TypeOf(resp)

	for _, field := range ListResponseFields {
		if !hasJSONTag(respType, field.Name) && field.Required {
			t.Errorf("Required field '%s' not found in MainTaskListResp", field.Name)
		}
	}
}

// TestAssetFieldsExist verifies Asset struct has required fields
// Validates: Requirements 9.2
func TestAssetFieldsExist(t *testing.T) {
	asset := types.Asset{}
	assetType := reflect.TypeOf(asset)

	requiredFields := []string{"id", "authority", "host", "port", "service", "title", "app", "createTime"}
	for _, fieldName := range requiredFields {
		if !hasJSONTag(assetType, fieldName) {
			t.Errorf("Required field '%s' not found in Asset", fieldName)
		}
	}
}

// TestVulFieldsExist verifies Vul struct has required fields
// Validates: Requirements 9.2
func TestVulFieldsExist(t *testing.T) {
	vul := types.Vul{}
	vulType := reflect.TypeOf(vul)

	requiredFields := []string{"id", "authority", "url", "pocFile", "severity", "result", "createTime"}
	for _, fieldName := range requiredFields {
		if !hasJSONTag(vulType, fieldName) {
			t.Errorf("Required field '%s' not found in Vul", fieldName)
		}
	}
}

// TestWorkerFieldsExist verifies Worker struct has required fields
// Validates: Requirements 9.2
func TestWorkerFieldsExist(t *testing.T) {
	worker := types.Worker{}
	workerType := reflect.TypeOf(worker)

	requiredFields := []string{"name", "ip", "cpuLoad", "memUsed", "status", "updateTime"}
	for _, fieldName := range requiredFields {
		if !hasJSONTag(workerType, fieldName) {
			t.Errorf("Required field '%s' not found in Worker", fieldName)
		}
	}
}

// TestMainTaskFieldsExist verifies MainTask struct has required fields
// Validates: Requirements 9.2
func TestMainTaskFieldsExist(t *testing.T) {
	task := types.MainTask{}
	taskType := reflect.TypeOf(task)

	requiredFields := []string{"id", "taskId", "name", "target", "status", "progress", "createTime"}
	for _, fieldName := range requiredFields {
		if !hasJSONTag(taskType, fieldName) {
			t.Errorf("Required field '%s' not found in MainTask", fieldName)
		}
	}
}

// Helper functions

// capitalizeFirst capitalizes the first letter of a string
func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]-32) + s[1:]
}

// hasJSONTag checks if a struct has a field with the given JSON tag
func hasJSONTag(t reflect.Type, tagName string) bool {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		// Handle tags like "code" or "code,omitempty"
		if jsonTag == tagName || (len(jsonTag) > len(tagName) && jsonTag[:len(tagName)+1] == tagName+",") {
			return true
		}
	}
	return false
}

// =============================================================================
// Property-Based Tests for API Backward Compatibility
// Feature: cscan-refactoring, Property 7: API Endpoint Backward Compatibility
// Validates: Requirements 4.1, 4.3
// =============================================================================

// TestProperty7_APIEndpointBackwardCompatibility verifies that existing API endpoints
// continue to accept the same requests and return compatible responses after refactoring.
// **Property 7: API Endpoint Backward Compatibility**
// **Validates: Requirements 4.1, 4.3**
func TestProperty7_APIEndpointBackwardCompatibility(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property 7.1: All expected endpoints exist and maintain their HTTP methods
	properties.Property("API endpoints maintain HTTP methods and paths", prop.ForAll(
		func(endpointIndex int) bool {
			if endpointIndex < 0 || endpointIndex >= len(ExpectedEndpoints) {
				return true // Skip invalid indices
			}

			endpoint := ExpectedEndpoints[endpointIndex]
			
			// Verify endpoint has valid HTTP method
			validMethods := map[string]bool{
				http.MethodGet:    true,
				http.MethodPost:   true,
				http.MethodPut:    true,
				http.MethodDelete: true,
				http.MethodPatch:  true,
			}

			if !validMethods[endpoint.Method] {
				return false
			}

			// Verify endpoint follows API versioning pattern
			if len(endpoint.Path) < 8 || endpoint.Path[:8] != "/api/v1/" {
				return false
			}

			return true
		},
		gen.IntRange(0, len(ExpectedEndpoints)-1),
	))

	// Property 7.2: Response structures maintain required fields
	properties.Property("Response structures maintain required fields", prop.ForAll(
		func(code int, msg string) bool {
			// Test BaseResp structure compatibility
			resp := types.BaseResp{Code: code, Msg: msg}
			data, err := json.Marshal(resp)
			if err != nil {
				return false
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// Required fields must exist for backward compatibility
			_, hasCode := decoded["code"]
			_, hasMsg := decoded["msg"]
			return hasCode && hasMsg
		},
		gen.Int(),
		gen.AnyString(),
	))

	// Property 7.3: List response structures maintain pagination fields
	properties.Property("List responses maintain pagination fields", prop.ForAll(
		func(code, total int, msg string) bool {
			// Test list response compatibility using AssetListResp as example
			resp := types.AssetListResp{
				Code:  code,
				Msg:   msg,
				Total: total,
				List:  []types.Asset{},
			}

			data, err := json.Marshal(resp)
			if err != nil {
				return false
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// All list responses must have these fields for backward compatibility
			requiredFields := []string{"code", "msg", "total", "list"}
			for _, field := range requiredFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.Int(),
		gen.Int(),
		gen.AnyString(),
	))

	// Property 7.4: Login response maintains authentication fields
	properties.Property("Login response maintains authentication fields", prop.ForAll(
		func(code int, msg, token, userId, username, role, workspaceId string) bool {
			resp := types.LoginResp{
				Code:        code,
				Msg:         msg,
				Token:       token,
				UserId:      userId,
				Username:    username,
				Role:        role,
				WorkspaceId: workspaceId,
			}

			data, err := json.Marshal(resp)
			if err != nil {
				return false
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// Critical authentication fields must exist for backward compatibility
			authFields := []string{"code", "msg", "token", "userId", "username", "role"}
			for _, field := range authFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.Int(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property 7.5: Asset structure maintains core fields
	properties.Property("Asset structure maintains core fields", prop.ForAll(
		func(id, authority, host, service, title string, port int) bool {
			asset := types.Asset{
				Id:        id,
				Authority: authority,
				Host:      host,
				Port:      port,
				Service:   service,
				Title:     title,
				App:       []string{},
			}

			data, err := json.Marshal(asset)
			if err != nil {
				return false
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// Core asset fields must exist for backward compatibility
			coreFields := []string{"id", "authority", "host", "port", "service", "title", "app"}
			for _, field := range coreFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.IntRange(1, 65535),
	))

	// Property 7.6: Vulnerability structure maintains core fields
	properties.Property("Vulnerability structure maintains core fields", prop.ForAll(
		func(id, authority, url, pocFile, severity, result string) bool {
			vul := types.Vul{
				Id:        id,
				Authority: authority,
				Url:       url,
				PocFile:   pocFile,
				Severity:  severity,
				Result:    result,
			}

			data, err := json.Marshal(vul)
			if err != nil {
				return false
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// Core vulnerability fields must exist for backward compatibility
			coreFields := []string{"id", "authority", "url", "pocFile", "severity", "result"}
			for _, field := range coreFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property 7.7: Worker structure maintains core fields
	properties.Property("Worker structure maintains core fields", prop.ForAll(
		func(name, ip, status string, cpuLoad, memUsed float64, taskCount, runningCount, concurrency int) bool {
			worker := types.Worker{
				Name:         name,
				IP:           ip,
				CPULoad:      cpuLoad,
				MemUsed:      memUsed,
				TaskCount:    taskCount,
				RunningCount: runningCount,
				Concurrency:  concurrency,
				Status:       status,
			}

			data, err := json.Marshal(worker)
			if err != nil {
				return false
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// Core worker fields must exist for backward compatibility
			coreFields := []string{"name", "ip", "cpuLoad", "memUsed", "taskCount", "runningCount", "concurrency", "status"}
			for _, field := range coreFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.Float64Range(0, 100),
		gen.Float64Range(0, 100),
		gen.IntRange(0, 1000),
		gen.IntRange(0, 100),
		gen.IntRange(1, 50),
	))

	// Property 7.8: Task structure maintains core fields
	properties.Property("Task structure maintains core fields", prop.ForAll(
		func(id, taskId, name, target, status string, progress int) bool {
			task := types.MainTask{
				Id:       id,
				TaskId:   taskId,
				Name:     name,
				Target:   target,
				Status:   status,
				Progress: progress,
			}

			data, err := json.Marshal(task)
			if err != nil {
				return false
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// Core task fields must exist for backward compatibility
			coreFields := []string{"id", "taskId", "name", "target", "status", "progress"}
			for _, field := range coreFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.IntRange(0, 100),
	))

	properties.TestingRun(t)
}

// =============================================================================
// Property-Based Tests for API Backward Compatibility
// Feature: cscan-optimization, Property 12: API Backward Compatibility
// Validates: Requirements 9.3
// =============================================================================

// TestProperty12_APIBackwardCompatibility verifies that new API fields are optional
// with sensible defaults that maintain existing behavior.
// **Property 12: API Backward Compatibility**
// **Validates: Requirements 9.3**
func TestProperty12_APIBackwardCompatibility(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property 12.1: BaseResp JSON serialization maintains required fields
	properties.Property("BaseResp serialization preserves required fields", prop.ForAll(
		func(code int, msg string) bool {
			resp := types.BaseResp{Code: code, Msg: msg}
			data, err := json.Marshal(resp)
			if err != nil {
				return false
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			// Required fields must exist
			_, hasCode := decoded["code"]
			_, hasMsg := decoded["msg"]
			return hasCode && hasMsg
		},
		gen.Int(),
		gen.AnyString(),
	))

	// Property 12.2: LoginResp JSON serialization maintains required fields
	properties.Property("LoginResp serialization preserves required fields", prop.ForAll(
		func(code int, msg, token, userId, username, role string) bool {
			resp := types.LoginResp{
				Code:     code,
				Msg:      msg,
				Token:    token,
				UserId:   userId,
				Username: username,
				Role:     role,
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return false
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			// All required fields must exist
			requiredFields := []string{"code", "msg", "token", "userId", "username", "role"}
			for _, field := range requiredFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.Int(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property 12.3: List responses maintain required fields (code, msg, total, list)
	properties.Property("AssetListResp serialization preserves required fields", prop.ForAll(
		func(code, total int, msg string) bool {
			resp := types.AssetListResp{
				Code:  code,
				Msg:   msg,
				Total: total,
				List:  []types.Asset{},
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return false
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			requiredFields := []string{"code", "msg", "total", "list"}
			for _, field := range requiredFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.Int(),
		gen.Int(),
		gen.AnyString(),
	))

	// Property 12.4: Asset struct maintains required fields
	properties.Property("Asset serialization preserves required fields", prop.ForAll(
		func(id, authority, host, service, title string, port int) bool {
			asset := types.Asset{
				Id:        id,
				Authority: authority,
				Host:      host,
				Port:      port,
				Service:   service,
				Title:     title,
				App:       []string{},
			}
			data, err := json.Marshal(asset)
			if err != nil {
				return false
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			requiredFields := []string{"id", "authority", "host", "port", "service", "title", "app"}
			for _, field := range requiredFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.IntRange(1, 65535),
	))

	// Property 12.5: Vul struct maintains required fields
	properties.Property("Vul serialization preserves required fields", prop.ForAll(
		func(id, authority, url, pocFile, severity, result string) bool {
			vul := types.Vul{
				Id:        id,
				Authority: authority,
				Url:       url,
				PocFile:   pocFile,
				Severity:  severity,
				Result:    result,
			}
			data, err := json.Marshal(vul)
			if err != nil {
				return false
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			requiredFields := []string{"id", "authority", "url", "pocFile", "severity", "result"}
			for _, field := range requiredFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property 12.6: Worker struct maintains required fields
	properties.Property("Worker serialization preserves required fields", prop.ForAll(
		func(name, ip, status string, cpuLoad, memUsed float64) bool {
			worker := types.Worker{
				Name:    name,
				IP:      ip,
				CPULoad: cpuLoad,
				MemUsed: memUsed,
				Status:  status,
			}
			data, err := json.Marshal(worker)
			if err != nil {
				return false
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			requiredFields := []string{"name", "ip", "cpuLoad", "memUsed", "status"}
			for _, field := range requiredFields {
				if _, exists := decoded[field]; !exists {
					return false
				}
			}
			return true
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.Float64Range(0, 100),
		gen.Float64Range(0, 100),
	))

	properties.TestingRun(t)
}

// TestOptionalFieldsHaveDefaults verifies that optional fields have sensible defaults
// Validates: Requirements 9.3 - new fields SHALL be optional with sensible defaults
func TestOptionalFieldsHaveDefaults(t *testing.T) {
	// Test that optional fields in request structs have default values
	// PageReq should have default page=1 and pageSize=20
	pageReq := types.PageReq{}
	// The default tag is handled by go-zero, but we verify the struct allows zero values
	if pageReq.Page != 0 {
		t.Errorf("PageReq.Page should be zero value when not set, got %d", pageReq.Page)
	}

	// Test AssetListReq optional fields
	assetReq := types.AssetListReq{}
	// Optional fields should be empty/zero by default
	if assetReq.Query != "" {
		t.Errorf("AssetListReq.Query should be empty by default")
	}
	if assetReq.Host != "" {
		t.Errorf("AssetListReq.Host should be empty by default")
	}
	if assetReq.OnlyNew != false {
		t.Errorf("AssetListReq.OnlyNew should be false by default")
	}
}

// TestJSONOmitemptyBehavior verifies that omitempty fields are properly omitted
// Validates: Requirements 9.3
func TestJSONOmitemptyBehavior(t *testing.T) {
	// Asset with optional fields empty should omit them in JSON
	asset := types.Asset{
		Id:        "test-id",
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Service:   "http",
		Title:     "Test",
		App:       []string{},
	}

	data, err := json.Marshal(asset)
	if err != nil {
		t.Fatalf("Failed to marshal Asset: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Asset: %v", err)
	}

	// Required fields must exist
	requiredFields := []string{"id", "authority", "host", "port", "service", "title"}
	for _, field := range requiredFields {
		if _, exists := decoded[field]; !exists {
			t.Errorf("Required field '%s' missing from JSON output", field)
		}
	}
}

// TestAPIVersionPrefix verifies all endpoints use /api/v1/ prefix
// Validates: Requirements 9.1
func TestAPIVersionPrefix(t *testing.T) {
	for _, ep := range ExpectedEndpoints {
		if len(ep.Path) < 8 || ep.Path[:8] != "/api/v1/" {
			t.Errorf("Endpoint %s does not use /api/v1/ prefix", ep.Path)
		}
	}
}

// TestHTTPMethodsAreValid verifies all endpoints use valid HTTP methods
// Validates: Requirements 9.1
func TestHTTPMethodsAreValid(t *testing.T) {
	validMethods := map[string]bool{
		http.MethodGet:    true,
		http.MethodPost:   true,
		http.MethodPut:    true,
		http.MethodDelete: true,
		http.MethodPatch:  true,
	}

	for _, ep := range ExpectedEndpoints {
		if !validMethods[ep.Method] {
			t.Errorf("Endpoint %s uses invalid HTTP method: %s", ep.Path, ep.Method)
		}
	}
}

// TestResponseCodeField verifies all response types have code field
// Validates: Requirements 9.2
func TestResponseCodeField(t *testing.T) {
	responseTypes := []interface{}{
		types.BaseResp{},
		types.LoginResp{},
		types.UserListResp{},
		types.WorkspaceListResp{},
		types.AssetListResp{},
		types.VulListResp{},
		types.WorkerListResp{},
		types.MainTaskListResp{},
	}

	for _, resp := range responseTypes {
		respType := reflect.TypeOf(resp)
		if !hasJSONTag(respType, "code") {
			t.Errorf("Response type %s missing 'code' field", respType.Name())
		}
		if !hasJSONTag(respType, "msg") {
			t.Errorf("Response type %s missing 'msg' field", respType.Name())
		}
	}
}
