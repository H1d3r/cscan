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
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type UserProfileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserProfileLogic {
	return &UserProfileLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// UserProfileGet 返回当前登录用户的个人信息
func (l *UserProfileLogic) UserProfileGet() (resp *types.UserProfileGetResp, err error) {
	uid := middleware.GetUserId(l.ctx)
	if uid == "" {
		return &types.UserProfileGetResp{Code: 401, Msg: "未认证"}, nil
	}
	user, err := l.svcCtx.UserModel.FindById(l.ctx, uid)
	if err != nil {
		logx.Errorf("[Profile] query user failed: %v", err)
		return &types.UserProfileGetResp{Code: 500, Msg: "系统错误"}, nil
	}
	if user == nil {
		return &types.UserProfileGetResp{Code: 404, Msg: "用户不存在"}, nil
	}
	role := user.Role
	if role == "" {
		role = "user"
	}
	resp = &types.UserProfileGetResp{
		Code:       0,
		Msg:        "success",
		Id:         user.Id.Hex(),
		Username:   user.Username,
		Email:      user.Email,
		Phone:      user.Phone,
		Avatar:     user.Avatar,
		Role:       role,
		Status:     user.Status,
		CreateTime: user.CreateTime.Unix(),
	}
	if user.LastLoginTime != nil {
		resp.LastLoginTime = user.LastLoginTime.Unix()
	}
	return resp, nil
}
func (l *UserProfileLogic) UserProfileUpdate(req *types.UserProfileUpdateReq) (resp *types.BaseResp, err error) {
	uid := middleware.GetUserId(l.ctx)
	if uid == "" {
		return &types.BaseResp{Code: 401, Msg: "未认证"}, nil
	}
	user, err := l.svcCtx.UserModel.FindById(l.ctx, uid)
	if err != nil {
		logx.Errorf("[Profile] query user failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if user == nil {
		return &types.BaseResp{Code: 404, Msg: "用户不存在"}, nil
	}

	// superadmin 用户名禁止修改
	if user.IsSuperadmin() && req.Username != "" && req.Username != user.Username {
		return &types.BaseResp{Code: 400, Msg: "管理员用户名不可修改"}, nil
	}

	// 用户名非空且变化时，检查冲突
	if req.Username != "" && req.Username != user.Username {
		exist, err := l.svcCtx.UserModel.FindByUsername(l.ctx, req.Username)
		if err != nil {
			logx.Errorf("[Profile] check username conflict failed: %v", err)
			return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
		}
		if exist != nil {
			return &types.BaseResp{Code: 400, Msg: "用户名已存在"}, nil
		}
	}

	if err := l.svcCtx.UserModel.UpdateProfile(l.ctx, uid, req.Username, req.Email, req.Phone, req.Avatar); err != nil {
		logx.Errorf("[Profile] update profile failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "更新失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "更新成功"}, nil
}

// UserPasswordChange 修改密码：成功后强制重新登录 + 吊销该用户所有 PAT
func (l *UserProfileLogic) UserPasswordChange(req *types.UserPasswordChangeReq) (resp *types.UserPasswordChangeResp, err error) {
	uid := middleware.GetUserId(l.ctx)
	if uid == "" {
		return &types.UserPasswordChangeResp{Code: 401, Msg: "未认证"}, nil
	}
	if req.OldPassword == "" {
		return &types.UserPasswordChangeResp{Code: 400, Msg: "请输入原密码"}, nil
	}
	if err := model.ValidatePasswordStrength(req.NewPassword); err != nil {
		return &types.UserPasswordChangeResp{Code: 400, Msg: err.Error()}, nil
	}
	user, err := l.svcCtx.UserModel.FindById(l.ctx, uid)
	if err != nil {
		logx.Errorf("[Profile] query user failed: %v", err)
		return &types.UserPasswordChangeResp{Code: 500, Msg: "系统错误"}, nil
	}
	if user == nil {
		return &types.UserPasswordChangeResp{Code: 404, Msg: "用户不存在"}, nil
	}
	if !model.CheckPassword(req.OldPassword, user.Password) {
		return &types.UserPasswordChangeResp{Code: 400, Msg: "原密码错误"}, nil
	}
	if req.OldPassword == req.NewPassword {
		return &types.UserPasswordChangeResp{Code: 400, Msg: "新密码不能与原密码相同"}, nil
	}

	if err := l.svcCtx.UserModel.UpdatePassword(l.ctx, uid, req.NewPassword); err != nil {
		logx.Errorf("[Profile] update password failed: %v", err)
		return &types.UserPasswordChangeResp{Code: 500, Msg: "修改密码失败"}, nil
	}

	// 吊销该用户所有 PAT
	if _, err := l.svcCtx.UserTokenModel.RevokeByUserId(l.ctx, user.Id); err != nil {
		logx.Errorf("[Profile] revoke user tokens failed: %v", err)
	}

	return &types.UserPasswordChangeResp{Code: 0, Msg: "密码修改成功，请重新登录", MustReLogin: true}, nil
}

// UserTokenCreate 创建 PAT，明文 token 仅本次返回
func (l *UserProfileLogic) UserTokenCreate(req *types.UserTokenCreateReq) (resp *types.UserTokenCreateResp, err error) {
	uid := middleware.GetUserId(l.ctx)
	if uid == "" {
		return &types.UserTokenCreateResp{Code: 401, Msg: "未认证"}, nil
	}
	if req.Name == "" {
		return &types.UserTokenCreateResp{Code: 400, Msg: "Token名称不能为空"}, nil
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return &types.UserTokenCreateResp{Code: 400, Msg: "用户ID无效"}, nil
	}

	// 过期时间校验
	var expiresPtr *time.Time
	if req.ExpiresAt > 0 {
		t := time.Unix(req.ExpiresAt, 0)
		if t.Before(time.Now()) {
			return &types.UserTokenCreateResp{Code: 400, Msg: "过期时间不能早于当前时间"}, nil
		}
		expiresPtr = &t
	}

	// 数量上限校验
	count, err := l.svcCtx.UserTokenModel.CountByUserId(l.ctx, oid)
	if err != nil {
		logx.Errorf("[PAT] count user tokens failed: %v", err)
		return &types.UserTokenCreateResp{Code: 500, Msg: "系统错误"}, nil
	}
	if count >= model.MaxTokensPerUser {
		return &types.UserTokenCreateResp{Code: 400, Msg: fmt.Sprintf("每个用户最多 %d 个有效 Token", model.MaxTokensPerUser)}, nil
	}

	plain, err := model.GeneratePAT()
	if err != nil {
		logx.Errorf("[PAT] generate token failed: %v", err)
		return &types.UserTokenCreateResp{Code: 500, Msg: "生成Token失败"}, nil
	}

	// 归一化 scopes：空或 ["*"] 视为全量；否则去重 + 校验每个 scope 是否合法
	scopes, code, msg := normalizeScopes(req.Scopes)
	if code != 0 {
		return &types.UserTokenCreateResp{Code: code, Msg: msg}, nil
	}

	doc := &model.UserToken{
		UserId:    oid,
		Name:      req.Name,
		TokenHash: model.HashPAT(plain),
		Prefix:    model.PATPrefixOf(plain),
		Scopes:    scopes,
		ExpiresAt: expiresPtr,
		Status:    model.StatusEnable,
	}
	if err := l.svcCtx.UserTokenModel.Insert(l.ctx, doc); err != nil {
		logx.Errorf("[PAT] insert token failed: %v", err)
		return &types.UserTokenCreateResp{Code: 500, Msg: "创建Token失败"}, nil
	}

	resp = &types.UserTokenCreateResp{
		Code:       0,
		Msg:        "创建成功",
		Token:      plain,
		Id:         doc.Id.Hex(),
		Name:       doc.Name,
		Prefix:     doc.Prefix,
		Scopes:     doc.Scopes,
		CreateTime: doc.CreateTime.Unix(),
	}
	if expiresPtr != nil {
		resp.ExpiresAt = expiresPtr.Unix()
	}
	return resp, nil
}

// normalizeScopes 归一化 PAT scopes
//   - 空 / ["*"] → ["*"]（全量）
//   - 否则去重 + 校验每个 scope 是否在白名单内
func normalizeScopes(raw []string) ([]string, int, string) {
	if len(raw) == 0 {
		return []string{"*"}, 0, ""
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s == "*" {
			return []string{"*"}, 0, ""
		}
		if !middleware.ValidScope(s) {
			return nil, 400, fmt.Sprintf("未知的API分组: %s", s)
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return []string{"*"}, 0, ""
	}
	return out, 0, ""
}

// UserTokenList 列出当前用户所有 PAT
func (l *UserProfileLogic) UserTokenList() (resp *types.UserTokenListResp, err error) {
	uid := middleware.GetUserId(l.ctx)
	if uid == "" {
		return &types.UserTokenListResp{Code: 401, Msg: "未认证"}, nil
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return &types.UserTokenListResp{Code: 400, Msg: "用户ID无效"}, nil
	}
	docs, err := l.svcCtx.UserTokenModel.FindByUserId(l.ctx, oid)
	if err != nil {
		logx.Errorf("[PAT] list user tokens failed: %v", err)
		return &types.UserTokenListResp{Code: 500, Msg: "系统错误"}, nil
	}
	list := make([]types.UserTokenListItem, 0, len(docs))
	for _, d := range docs {
		item := types.UserTokenListItem{
			Id:         d.Id.Hex(),
			Name:       d.Name,
			Prefix:     d.Prefix,
			Scopes:     d.Scopes,
			Status:     d.Status,
			CreateTime: d.CreateTime.Unix(),
		}
		if d.ExpiresAt != nil {
			item.ExpiresAt = d.ExpiresAt.Unix()
		}
		if d.LastUsedAt != nil {
			item.LastUsedAt = d.LastUsedAt.Unix()
		}
		item.LastUsedIP = d.LastUsedIP
		list = append(list, item)
	}
	return &types.UserTokenListResp{Code: 0, Msg: "success", List: list}, nil
}

// UserTokenSetStatus 切换当前用户指定 PAT 的启用状态（enable / disable）
func (l *UserProfileLogic) UserTokenSetStatus(req *types.UserTokenSetStatusReq) (resp *types.BaseResp, err error) {
	uid := middleware.GetUserId(l.ctx)
	if uid == "" {
		return &types.BaseResp{Code: 401, Msg: "未认证"}, nil
	}
	if req.Id == "" {
		return &types.BaseResp{Code: 400, Msg: "Token ID不能为空"}, nil
	}
	if req.Status != model.StatusEnable && req.Status != model.StatusDisable {
		return &types.BaseResp{Code: 400, Msg: "状态值无效"}, nil
	}
	tokId, err := primitive.ObjectIDFromHex(req.Id)
	if err != nil {
		return &types.BaseResp{Code: 400, Msg: "Token ID无效"}, nil
	}
	userOid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return &types.BaseResp{Code: 400, Msg: "用户ID无效"}, nil
	}
	tok, err := l.svcCtx.UserTokenModel.FindByIdAndUserId(l.ctx, tokId, userOid)
	if err != nil {
		logx.Errorf("[PAT] query token failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "系统错误"}, nil
	}
	if tok == nil {
		return &types.BaseResp{Code: 404, Msg: "Token不存在"}, nil
	}
	if tok.Status == req.Status {
		return &types.BaseResp{Code: 0, Msg: "success"}, nil
	}
	if err := l.svcCtx.UserTokenModel.SetStatusById(l.ctx, tokId, userOid, req.Status); err != nil {
		logx.Errorf("[PAT] set token status failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "操作失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "success"}, nil
}

// UserTokenScopeList 返回可供 PAT 选择的 API 分组清单
//   - Groups：分组矩阵，每组带可用 CRUD 动作集合（20 组 × 4 动作）
//   - List：扁平 <group>:<action> 列表（兼容旧前端）
//   - Actions：动作集合
func (l *UserProfileLogic) UserTokenScopeList() (resp *types.UserTokenScopeListResp, err error) {
	groups := middleware.ScopeGroups()
	actions := middleware.ScopeActions()

	groupList := make([]types.UserTokenScopeGroup, 0, len(groups))
	flat := make([]types.UserTokenScopeItem, 0, len(groups)*len(actions))
	for _, g := range groups {
		groupList = append(groupList, types.UserTokenScopeGroup{
			Value:       string(g.Value),
			Label:       g.Label,
			Description: g.Description,
			Actions:     actions,
		})
		for _, a := range actions {
			flat = append(flat, types.UserTokenScopeItem{
				Value:       string(g.Value) + ":" + a,
				Label:       g.Label + " · " + a,
				Description: g.Description,
			})
		}
	}
	return &types.UserTokenScopeListResp{
		Code:    0,
		Msg:     "success",
		List:    flat,
		Groups:  groupList,
		Actions: actions,
	}, nil
}
