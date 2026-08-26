package logic

import (
	"context"
	"fmt"
	"time"

	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

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
		list = append(list, types.RoleInfo{
			Id:            r.Id,
			Name:          r.Name,
			DisplayName:   r.DisplayName,
			Description:   r.Description,
			MenuPaths:     r.MenuPaths,
			IsBuiltIn:     r.IsBuiltIn,
			IsSuperadmin:  r.IsSuperadmin,
			CreateTime:    r.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:    r.UpdateTime.Format("2006-01-02 15:04:05"),
		})
	}

	return &types.RoleListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
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

	return &types.RoleInfo{
		Id:            role.Id,
		Name:          role.Name,
		DisplayName:   role.DisplayName,
		Description:   role.Description,
		MenuPaths:     role.MenuPaths,
		IsBuiltIn:     role.IsBuiltIn,
		IsSuperadmin:  role.IsSuperadmin,
		CreateTime:    role.CreateTime.Format("2006-01-02 15:04:05"),
		UpdateTime:    role.UpdateTime.Format("2006-01-02 15:04:05"),
	}, nil
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
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		MenuPaths:   req.MenuPaths,
		IsBuiltIn:   false,
		IsSuperadmin: false,
		CreateTime:  now,
		UpdateTime:  now,
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

	// 内置角色不允许修改名称
	if role.IsBuiltIn && req.Name != role.Name {
		return &types.BaseResp{Code: 400, Msg: "内置角色名不允许修改"}, nil
	}

	updateData := bson.M{}
	if req.DisplayName != "" {
		updateData["display_name"] = req.DisplayName
	}
	if req.Description != "" {
		updateData["description"] = req.Description
	}
	if req.MenuPaths != nil {
		updateData["menu_paths"] = req.MenuPaths
	}
	if req.IsSuperadmin != nil {
		updateData["is_super_admin"] = *req.IsSuperadmin
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
		roleName = "user"
	}

	role, err := l.svcCtx.RoleModel.FindByName(l.ctx, roleName)
	if err != nil {
		logx.Errorf("查询角色菜单失败: %v", err)
		return &types.RoleMenusResp{Code: 500, Msg: "查询失败"}, nil
	}

	var menuPaths []string
	if role != nil && len(role.MenuPaths) > 0 {
		menuPaths = role.MenuPaths
	} else {
		// 内置角色（superadmin/admin/user）使用全部菜单
		menuPaths = model.MenuPathList()
	}

	return &types.RoleMenusResp{
		Code:      0,
		Msg:       "success",
		MenuPaths: menuPaths,
	}, nil
}
