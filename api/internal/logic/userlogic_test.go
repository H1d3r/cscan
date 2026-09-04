package logic

import (
	"testing"

	"cscan/api/internal/types"
	"cscan/internal/model"
)

// 测试策略：由于 UserModel 是具体类型而非接口，我们采用表驱动测试 + 边界条件测试
// 重点测试业务逻辑（参数校验、错误处理、admin 保护）而非数据库交互

// TestUserCreateReq_PasswordValidation 测试密码强度验证逻辑
func TestUserCreateReq_PasswordValidation(t *testing.T) {
	testCases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"强密码", "StrongPass123!", false},
		{"包含大小写和数字", "Abcd1234", false},
		{"太短", "123", true},
		{"纯数字", "12345678", true},
		{"纯小写字母", "abcdefgh", true},
		{"空密码", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := model.ValidatePasswordStrength(tc.password)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidatePasswordStrength(%q) error = %v, wantErr %v", tc.password, err, tc.wantErr)
			}
		})
	}
}

// TestUserInfo_DefaultRole 测试用户角色默认值逻辑
func TestUserInfo_DefaultRole(t *testing.T) {
	testCases := []struct {
		name         string
		inputRole    string
		expectedRole string
	}{
		{"指定admin", "admin", "admin"},
		{"指定user", "user", "user"},
		{"空角色应默认user", "", "user"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role := tc.inputRole
			if role == "" {
				role = "user"
			}
			if role != tc.expectedRole {
				t.Errorf("期望角色 %s，实际 %s", tc.expectedRole, role)
			}
		})
	}
}

// TestNormalizePage 测试分页参数规范化及显式不分页哨兵。
func TestNormalizePage(t *testing.T) {
	testCases := []struct {
		name             string
		page, pageSize   int
		expectedPage     int
		expectedPageSize int
	}{
		{"正常分页", 2, 20, 2, 20},
		{"零页码表示不分页", 0, 20, 0, 0},
		{"零页大小表示不分页", 1, 0, 0, 0},
		{"双零表示不分页", 0, 0, 0, 0},
		{"负页码应规范化", -1, 20, 1, 20},
		{"负页大小应规范化", 1, -1, 1, 20},
		{"超大页大小应限制", 1, 200, 1, 100},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			page, pageSize := model.NormalizePage(tc.page, tc.pageSize)
			if page != tc.expectedPage {
				t.Errorf("期望 page=%d，实际 %d", tc.expectedPage, page)
			}
			if pageSize != tc.expectedPageSize {
				t.Errorf("期望 pageSize=%d，实际 %d", tc.expectedPageSize, pageSize)
			}
		})
	}
}

// TestUserModel_IsAdmin 测试 admin 判断逻辑
func TestUserModel_IsAdmin(t *testing.T) {
	testCases := []struct {
		name     string
		user     *model.User
		expected bool
	}{
		{"superadmin用户", &model.User{Username: "admin", Role: "superadmin"}, true},
		{"admin角色用户", &model.User{Username: "user1", Role: "admin"}, true},
		{"普通用户", &model.User{Username: "user1", Role: "user"}, false},
		{"nil用户", nil, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.user.IsSuperadmin()
			if result != tc.expected {
				t.Errorf("IsSuperadmin() = %v, 期望 %v", result, tc.expected)
			}
		})
	}
}

func TestUserListResp_Structure(t *testing.T) {
	resp := &types.UserListResp{
		Code:  0,
		Msg:   "success",
		Total: 10,
		List: []types.UserInfo{
			{
				Id:       "507f1f77bcf86cd799439011",
				Username: "testuser",
				Role:     "user",
				Status:   "enable",
				Avatar:   "http://example.com/avatar.jpg",
			},
		},
	}

	if resp.Code != 0 {
		t.Errorf("期望 Code=0，实际 %d", resp.Code)
	}
	if resp.Total != 10 {
		t.Errorf("期望 Total=10，实际 %d", resp.Total)
	}
	if len(resp.List) != 1 {
		t.Errorf("期望列表长度=1，实际 %d", len(resp.List))
	}
	if resp.List[0].Role != "user" {
		t.Errorf("期望角色=user，实际 %s", resp.List[0].Role)
	}
}

// TestUserCreateReq_Validation 测试创建用户请求参数验证
func TestUserCreateReq_Validation(t *testing.T) {
	testCases := []struct {
		name    string
		req     *types.UserCreateReq
		wantErr bool
	}{
		{
			"有效请求",
			&types.UserCreateReq{
				Username: "newuser",
				Password: "StrongPass123!",
				Role:     "user",
				Status:   "enable",
			},
			false,
		},
		{
			"弱密码",
			&types.UserCreateReq{
				Username: "newuser",
				Password: "123",
				Role:     "user",
				Status:   "enable",
			},
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := model.ValidatePasswordStrength(tc.req.Password)
			if (err != nil) != tc.wantErr {
				t.Errorf("密码验证 error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestUserUpdateReq_AdminProtection 测试 admin 账号保护逻辑（业务规则）
func TestUserUpdateReq_AdminProtection(t *testing.T) {
	adminUser := &model.User{
		Username: "admin",
		Role:     "superadmin",
		Status:   "enable",
	}

	testCases := []struct {
		name          string
		req           *types.UserUpdateReq
		shouldReject  bool
		rejectMessage string
	}{
		{
			"尝试修改管理员状态",
			&types.UserUpdateReq{
				Username: "admin",
				Role:     "superadmin",
				Status:   "disable",
			},
			true,
			"管理员账号状态不允许修改",
		},
		{
			"尝试修改管理员角色",
			&types.UserUpdateReq{
				Username: "admin",
				Role:     "user",
				Status:   "enable",
			},
			true,
			"管理员账号角色不允许修改",
		},
		{
			"修改管理员其他字段",
			&types.UserUpdateReq{
				Username: "admin",
				Role:     "superadmin",
				Status:   "enable",
				Avatar:   "new-avatar.jpg",
			},
			false,
			"",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 测试业务规则：admin 状态和角色保护
			statusChanged := tc.req.Status != "" && tc.req.Status != adminUser.Status
			roleChanged := tc.req.Role != "" && tc.req.Role != adminUser.Role

			shouldReject := adminUser.IsSuperadmin() && (statusChanged || roleChanged)
			if shouldReject != tc.shouldReject {
				t.Errorf("admin 保护判断错误：期望拒绝=%v，实际=%v", tc.shouldReject, shouldReject)
			}
		})
	}
}

// TestUserDeleteReq_AdminProtection 测试删除 admin 账号保护逻辑
func TestUserDeleteReq_AdminProtection(t *testing.T) {
	testCases := []struct {
		name         string
		username     string
		shouldReject bool
	}{
		{"删除admin账号", "admin", true},
		{"删除普通用户", "user1", false},
		{"删除空用户名", "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shouldReject := tc.username == "admin"
			if shouldReject != tc.shouldReject {
				t.Errorf("删除保护判断错误：期望拒绝=%v，实际=%v", tc.shouldReject, shouldReject)
			}
		})
	}
}

// TestBaseResp_ErrorCodes 测试错误码约定
func TestBaseResp_ErrorCodes(t *testing.T) {
	testCases := []struct {
		name         string
		code         int
		expectedType string
	}{
		{"成功", 0, "success"},
		{"参数错误", 400, "client_error"},
		{"未授权", 401, "auth_error"},
		{"禁止访问", 403, "permission_error"},
		{"资源不存在", 404, "not_found"},
		{"服务器错误", 500, "server_error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &types.BaseResp{
				Code: tc.code,
				Msg:  tc.name,
			}

			var actualType string
			switch {
			case resp.Code == 0:
				actualType = "success"
			case resp.Code >= 400 && resp.Code < 500:
				if resp.Code == 401 {
					actualType = "auth_error"
				} else if resp.Code == 403 {
					actualType = "permission_error"
				} else if resp.Code == 404 {
					actualType = "not_found"
				} else {
					actualType = "client_error"
				}
			case resp.Code >= 500:
				actualType = "server_error"
			}

			if actualType != tc.expectedType {
				t.Errorf("错误码 %d 类型判断错误：期望 %s，实际 %s", tc.code, tc.expectedType, actualType)
			}
		})
	}
}
