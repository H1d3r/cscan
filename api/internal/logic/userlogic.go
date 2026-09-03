package logic

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// roleAssignable 校验角色是否可分配给用户。
// 内置 superadmin/admin/user 恒可用；其余角色必须已在 role 集合中定义。
func roleAssignable(ctx context.Context, svcCtx *svc.ServiceContext, roleName string) (bool, error) {
	switch roleName {
	case model.RoleSuperadmin, model.RoleAdmin, model.RoleUser:
		return true, nil
	}
	role, err := svcCtx.RoleModel.FindByName(ctx, roleName)
	if err != nil {
		logx.Errorf("查询角色失败: role=%s err=%v", roleName, err)
		return false, err
	}
	return role != nil, nil
}

type UserListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserListLogic {
	return &UserListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserListLogic) UserList(req *types.UserListReq) (resp *types.UserListResp, err error) {
	// Admin protection: 仅管理员可访问用户列表
	currentRole := middleware.GetRole(l.ctx)
	if !l.svcCtx.IsAdminRole(l.ctx, currentRole) {
		return &types.UserListResp{Code: 403, Msg: "无权限访问"}, nil
	}

	filter := bson.M{}
	if req.Search != "" {
		search := bson.M{"$regex": regexp.QuoteMeta(req.Search), "$options": "i"}
		filter["$or"] = []bson.M{
			{"username": search},
			{"role": search},
		}
	}

	total, err := l.svcCtx.UserModel.Count(l.ctx, filter)
	if err != nil {
		logx.Errorf("查询用户数量失败: %v", err)
		return nil, fmt.Errorf("查询用户数量失败: %w", err)
	}

	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
	users, err := l.svcCtx.UserModel.Find(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		logx.Errorf("查询用户列表失败: %v", err)
		return nil, fmt.Errorf("查询用户列表失败: %w", err)
	}

	list := make([]types.UserInfo, 0, len(users))
	for _, u := range users {
		role := u.Role
		if role == "" {
			role = "user"
		}
		list = append(list, types.UserInfo{
			Id:       u.Id.Hex(),
			Username: u.Username,
			Role:     role,
			Status:   u.Status,
			Avatar:   u.Avatar,
		})
	}

	return &types.UserListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
}

// UserCreateLogic 创建用户逻辑
type UserCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserCreateLogic {
	return &UserCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserCreateLogic) UserCreate(req *types.UserCreateReq) (resp *types.BaseResp, err error) {
	// 验证密码强度
	if err := model.ValidatePasswordStrength(req.Password); err != nil {
		return &types.BaseResp{Code: 400, Msg: err.Error()}, nil
	}

	// 检查用户名是否已存在
	exists, err := l.svcCtx.UserModel.FindByUsername(l.ctx, req.Username)
	if err != nil {
		logx.Errorf("查询用户失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if exists != nil {
		return &types.BaseResp{Code: 400, Msg: "用户名已存在"}, nil
	}

	// 首位注册用户自动成为 superadmin（替代内建 admin 账号）
	total, err := l.svcCtx.UserModel.Count(l.ctx, bson.M{})
	if err != nil {
		logx.Errorf("查询用户数量失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}

	role := req.Role
	if total == 0 {
		role = model.RoleSuperadmin
		logx.Infof("[SECURITY] First user auto-promoted to superadmin: username=%s", req.Username)
	} else if role == "" {
		role = model.RoleUser
	}
	if total > 0 {
		if ok, err := roleAssignable(l.ctx, l.svcCtx, role); err != nil {
			return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
		} else if !ok {
			return &types.BaseResp{Code: 400, Msg: "角色不存在"}, nil
		}
	}
	user := &model.User{
		Username: req.Username,
		Password: req.Password, // 在model层会自动bcrypt加密
		Role:     role,
		Status:   req.Status,
		Avatar:   req.Avatar,
	}

	err = l.svcCtx.UserModel.Insert(l.ctx, user)
	if err != nil {
		logx.Errorf("创建用户失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "创建用户失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "创建成功"}, nil
}

// UserUpdateLogic 更新用户逻辑
type UserUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserUpdateLogic {
	return &UserUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserUpdateLogic) UserUpdate(req *types.UserUpdateReq) (resp *types.BaseResp, err error) {
	// 检查用户是否存在
	user, err := l.svcCtx.UserModel.FindById(l.ctx, req.Id)
	if err != nil {
		logx.Errorf("查询用户失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if user == nil {
		return &types.BaseResp{Code: 404, Msg: "用户不存在"}, nil
	}

	// superadmin 账号状态受保护：禁止修改状态
	if user.IsSuperadmin() && req.Status != "" && req.Status != user.Status {
		return &types.BaseResp{Code: 400, Msg: "管理员账号状态不允许修改"}, nil
	}
	// superadmin 账号角色受保护：禁止降级
	if user.IsSuperadmin() && req.Role != "" && req.Role != user.Role {
		return &types.BaseResp{Code: 400, Msg: "管理员账号角色不允许修改"}, nil
	}

	// 如果修改用户名，检查是否重复
	if req.Username != user.Username {
		exists, err := l.svcCtx.UserModel.FindByUsername(l.ctx, req.Username)
		if err != nil {
			logx.Errorf("查询用户失败: %v", err)
			return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
		}
		if exists != nil {
			return &types.BaseResp{Code: 400, Msg: "用户名已存在"}, nil
		}
	}

	// 更新用户信息
	updateData := bson.M{
		"username":    req.Username,
		"status":      req.Status,
		"avatar":      req.Avatar,
		"update_time": time.Now(),
	}
	if req.Role != "" {
		if ok, err := roleAssignable(l.ctx, l.svcCtx, req.Role); err != nil {
			return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
		} else if !ok {
			return &types.BaseResp{Code: 400, Msg: "角色不存在"}, nil
		}
		updateData["role"] = req.Role
	}

	err = l.svcCtx.UserModel.Update(l.ctx, req.Id, updateData)
	if err != nil {
		logx.Errorf("更新用户失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "更新用户失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "更新成功"}, nil
}

// UserDeleteLogic 删除用户逻辑
type UserDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserDeleteLogic {
	return &UserDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserDeleteLogic) UserDelete(req *types.UserDeleteReq) (resp *types.BaseResp, err error) {
	// 检查用户是否存在
	user, err := l.svcCtx.UserModel.FindById(l.ctx, req.Id)
	if err != nil {
		logx.Errorf("查询用户失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if user == nil {
		return &types.BaseResp{Code: 404, Msg: "用户不存在"}, nil
	}

	// 禁止删除 admin 账号
	if user.Username == "admin" {
		return &types.BaseResp{Code: 400, Msg: "admin 账号不允许删除"}, nil
	}

	// 删除用户
	err = l.svcCtx.UserModel.DeleteById(l.ctx, req.Id)
	if err != nil {
		logx.Errorf("删除用户失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "删除用户失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// UserResetPasswordLogic 重置密码逻辑
type UserResetPasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserResetPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserResetPasswordLogic {
	return &UserResetPasswordLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserResetPasswordLogic) UserResetPassword(req *types.UserResetPasswordReq) (resp *types.BaseResp, err error) {
	currentUserId := middleware.GetUserId(l.ctx)
	if currentUserId == "" {
		return &types.BaseResp{Code: 401, Msg: "未认证"}, nil
	}
	currentRole := middleware.GetRole(l.ctx)
	isAdmin := l.svcCtx.IsAdminRole(l.ctx, currentRole)

	// 非管理员只能修改自己的密码
	if !isAdmin {
		req.Id = currentUserId
	} else if req.Id == "" {
		// 管理员未指定目标用户时，默认改自己
		req.Id = currentUserId
	}

	// 验证新密码强度
	if err := model.ValidatePasswordStrength(req.NewPassword); err != nil {
		return &types.BaseResp{Code: 400, Msg: err.Error()}, nil
	}

	// 检查目标用户是否存在
	user, err := l.svcCtx.UserModel.FindById(l.ctx, req.Id)
	if err != nil {
		logx.Errorf("查询用户失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if user == nil {
		return &types.BaseResp{Code: 404, Msg: "用户不存在"}, nil
	}

	// 管理员重置其他用户密码时，跳过原密码校验（管理员不知道目标用户的旧密码）
	// 管理员重置自己密码、或普通用户修改自己密码时，仍需验证原密码
	isResetSelf := req.Id == currentUserId
	if isResetSelf {
		if req.OldPassword == "" {
			return &types.BaseResp{Code: 400, Msg: "请输入原密码"}, nil
		}
		if !model.CheckPassword(req.OldPassword, user.Password) {
			return &types.BaseResp{Code: 400, Msg: "原密码错误"}, nil
		}
		if req.OldPassword == req.NewPassword {
			return &types.BaseResp{Code: 400, Msg: "新密码不能与原密码相同"}, nil
		}
	}

	// 重置密码
	err = l.svcCtx.UserModel.UpdatePassword(l.ctx, req.Id, req.NewPassword)
	if err != nil {
		logx.Errorf("重置密码失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "重置密码失败"}, nil
	}

	// 同步吊销该用户所有 PAT，保证密码修改后 Token 全部失效
	if _, err := l.svcCtx.UserTokenModel.RevokeByUserId(l.ctx, user.Id); err != nil {
		logx.Errorf("[UserResetPassword] revoke user tokens failed: %v", err)
	}

	return &types.BaseResp{Code: 0, Msg: "密码重置成功"}, nil
}

// ScanConfigLogic 扫描配置逻辑
type ScanConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanConfigLogic {
	return &ScanConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanConfigLogic) SaveScanConfig(r *http.Request, req *types.SaveScanConfigReq) (resp *types.BaseResp, err error) {
	// 从请求上下文获取用户ID
	userId := middleware.GetUserId(r.Context())
	if userId == "" {
		return &types.BaseResp{Code: 401, Msg: "未登录"}, nil
	}

	err = l.svcCtx.UserModel.UpdateScanConfig(l.ctx, userId, req.Config)
	if err != nil {
		logx.Errorf("保存扫描配置失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "保存失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

func (l *ScanConfigLogic) GetScanConfig(r *http.Request) (resp *types.GetScanConfigResp, err error) {
	// 从请求上下文获取用户ID
	userId := middleware.GetUserId(r.Context())
	if userId == "" {
		return &types.GetScanConfigResp{Code: 401, Msg: "未登录"}, nil
	}

	config, err := l.svcCtx.UserModel.GetScanConfig(l.ctx, userId)
	if err != nil {
		logx.Errorf("获取扫描配置失败: %v", err)
		return &types.GetScanConfigResp{Code: 500, Msg: "获取失败"}, nil
	}

	return &types.GetScanConfigResp{Code: 0, Msg: "success", Config: config}, nil
}
