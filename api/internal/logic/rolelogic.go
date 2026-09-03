package logic

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// roleNamePattern 角色名格式：小写字母开头，仅含小写字母、数字、下划线，长度 2-32
var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// RoleListLogic 角色列表
type RoleListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRoleListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RoleListLogic {
	return &RoleListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RoleListLogic) List() (*types.RoleListResp, error) {
	roles, err := l.svcCtx.RoleModel.FindAll(l.ctx)
	if err != nil {
		logx.Errorf("查询角色列表失败: %v", err)
		return nil, fmt.Errorf("查询角色列表失败: %w", err)
	}

	list := make([]types.RoleInfo, 0, len(roles))
	for _, r := range roles {
		list = append(list, toRoleInfo(&r))
	}

	return &types.RoleListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

// toRoleInfo 将角色文档转换为响应结构
func toRoleInfo(r *model.Role) types.RoleInfo {
	menuPaths := r.MenuPaths
	if menuPaths == nil {
		menuPaths = []string{}
	}
	return types.RoleInfo{
		Id:           r.Id,
		Name:         r.Name,
		DisplayName:  r.DisplayName,
		Description:  r.Description,
		MenuPaths:    menuPaths,
		IsBuiltIn:    r.IsBuiltIn,
		IsSuperadmin: r.IsSuperadmin,
		CreateTime:   r.CreateTime.Format("2006-01-02 15:04:05"),
		UpdateTime:   r.UpdateTime.Format("2006-01-02 15:04:05"),
	}
}

// sanitizeMenuPaths 过滤非法菜单路径并去重，防止写入前端不存在的路由
func sanitizeMenuPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if !model.IsValidMenuPath(p) {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		result = append(result, p)
	}
	return result
}

// RoleDetailLogic 角色详情
type RoleDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRoleDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RoleDetailLogic {
	return &RoleDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RoleDetailLogic) Detail(req *types.RoleReq) (*types.RoleInfo, error) {
	role, err := l.svcCtx.RoleModel.FindByName(l.ctx, req.Name)
	if err != nil {
		logx.Errorf("查询角色详情失败: %v", err)
		return nil, fmt.Errorf("查询角色详情失败: %w", err)
	}
	if role == nil {
		return nil, fmt.Errorf("角色不存在")
	}

	info := toRoleInfo(role)
	return &info, nil
}

// RoleCreateLogic 创建角色
type RoleCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRoleCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RoleCreateLogic {
	return &RoleCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RoleCreateLogic) Create(req *types.RoleCreateReq) (*types.BaseResp, error) {
	if !roleNamePattern.MatchString(req.Name) {
		return &types.BaseResp{Code: 400, Msg: "角色名仅支持小写字母、数字、下划线，长度 2-32"}, nil
	}
	if req.DisplayName == "" {
		return &types.BaseResp{Code: 400, Msg: "显示名不能为空"}, nil
	}

	// 检查角色名是否已存在
	exists, err := l.svcCtx.RoleModel.FindByName(l.ctx, req.Name)
	if err != nil {
		logx.Errorf("查询角色失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if exists != nil {
		return &types.BaseResp{Code: 400, Msg: "角色名已存在"}, nil
	}

	now := time.Now()
	role := &model.Role{
		Name:         req.Name,
		DisplayName:  req.DisplayName,
		Description:  req.Description,
		MenuPaths:    sanitizeMenuPaths(req.MenuPaths),
		IsBuiltIn:    false,
		IsSuperadmin: false, // 自定义角色不允许直接设置 IsSuperadmin，由内置角色控制
		CreateTime:   now,
		UpdateTime:   now,
	}

	err = l.svcCtx.RoleModel.Insert(l.ctx, role)
	if err != nil {
		logx.Errorf("创建角色失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "创建角色失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "创建成功"}, nil
}

// RoleUpdateLogic 更新角色
type RoleUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRoleUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RoleUpdateLogic {
	return &RoleUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RoleUpdateLogic) Update(req *types.RoleUpdateReq) (*types.BaseResp, error) {
	role, err := l.svcCtx.RoleModel.FindByName(l.ctx, req.Name)
	if err != nil {
		logx.Errorf("查询角色失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if role == nil {
		return &types.BaseResp{Code: 404, Msg: "角色不存在"}, nil
	}
	// superadmin 是系统兜底角色，权限被裁剪后将无人可恢复配置
	if role.Name == model.RoleSuperadmin {
		return &types.BaseResp{Code: 400, Msg: "超级管理员角色不允许修改"}, nil
	}

	updateData := bson.M{}
	if req.DisplayName != "" {
		updateData["display_name"] = req.DisplayName
	}
	if req.Description != "" {
		updateData["description"] = req.Description
	}
	if req.MenuPaths != nil {
		updateData["menu_paths"] = sanitizeMenuPaths(req.MenuPaths)
	}
	// 内置角色的管理员标记固定，不随请求变更
	if req.IsSuperadmin != nil && !role.IsBuiltIn {
		updateData["is_superadmin"] = *req.IsSuperadmin
	}

	if len(updateData) == 0 {
		return &types.BaseResp{Code: 0, Msg: "无更新内容"}, nil
	}

	err = l.svcCtx.RoleModel.Update(l.ctx, role.Name, updateData)
	if err != nil {
		logx.Errorf("更新角色失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "更新角色失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "更新成功"}, nil
}

// RoleDeleteLogic 删除角色
type RoleDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRoleDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RoleDeleteLogic {
	return &RoleDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RoleDeleteLogic) Delete(req *types.RoleReq) (*types.BaseResp, error) {
	role, err := l.svcCtx.RoleModel.FindByName(l.ctx, req.Name)
	if err != nil {
		logx.Errorf("查询角色失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if role == nil {
		return &types.BaseResp{Code: 404, Msg: "角色不存在"}, nil
	}

	if role.IsBuiltIn {
		return &types.BaseResp{Code: 400, Msg: "内置角色不允许删除"}, nil
	}

	// 仍有用户绑定该角色时拒绝删除，否则这些用户将失去菜单权限来源
	inUse, err := l.svcCtx.RoleModel.CountUsersByRole(l.ctx, req.Name)
	if err != nil {
		logx.Errorf("统计角色占用失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if inUse > 0 {
		return &types.BaseResp{Code: 400, Msg: fmt.Sprintf("仍有 %d 个用户使用该角色，请先调整用户角色", inUse)}, nil
	}

	err = l.svcCtx.RoleModel.Delete(l.ctx, req.Name)
	if err != nil {
		logx.Errorf("删除角色失败: %v", err)
		return &types.BaseResp{Code: 500, Msg: "删除角色失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// RoleSyncMenusLogic 同步当前登录角色的菜单权限（登录后或手动触发）
type RoleSyncMenusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRoleSyncMenusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RoleSyncMenusLogic {
	return &RoleSyncMenusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RoleSyncMenusLogic) SyncMenus() (*types.RoleMenusResp, error) {
	roleName := middleware.GetRole(l.ctx)
	if roleName == "" {
		roleName = model.RoleUser
	}

	return &types.RoleMenusResp{
		Code:      0,
		Msg:       "success",
		MenuPaths: l.svcCtx.MenuPathsForRole(l.ctx, roleName),
		IsAdmin:   l.svcCtx.IsAdminRole(l.ctx, roleName),
		AllPaths:  model.MenuPathList(),
	}, nil
}

// RoleMenuOptionsLogic 返回系统全部可配置菜单路径，供角色配置页选择
type RoleMenuOptionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRoleMenuOptionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RoleMenuOptionsLogic {
	return &RoleMenuOptionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RoleMenuOptionsLogic) Options() (*types.RoleMenusResp, error) {
	return &types.RoleMenusResp{
		Code:      0,
		Msg:       "success",
		MenuPaths: model.MenuPathList(),
	}, nil
}
