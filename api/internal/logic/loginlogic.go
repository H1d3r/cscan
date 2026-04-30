package logic

import (
	"context"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogic) Login(req *types.LoginReq) (resp *types.LoginResp, err error) {
	// 验证用户名密码
	user, ok := l.svcCtx.UserModel.VerifyPassword(l.ctx, req.Username, req.Password)
	if !ok {
		return &types.LoginResp{
			Code: 401,
			Msg:  "用户名或密码错误",
		}, nil
	}

	// 更新登录时间
	if err := l.svcCtx.UserModel.UpdateLoginTime(l.ctx, user.Id.Hex()); err != nil {
		l.Logger.Errorf("更新登录时间失败: %v", err)
	}

	// 确定用户角色，默认为 user
	role := user.Role
	if role == "" {
		role = "user"
	}

	// 生成JWT Token
	now := time.Now().Unix()
	accessExpire := l.svcCtx.Config.Auth.AccessExpire
	token, err := l.generateToken(user.Id.Hex(), user.Username, role, now, accessExpire)
	if err != nil {
		return &types.LoginResp{
			Code: 500,
			Msg:  "生成Token失败",
		}, nil
	}

	// 获取默认工作空间 - 如果用户没有分配工作空间，使用空字符串（对应 default 工作空间）
	workspaceId := ""
	if len(user.WorkspaceIds) > 0 {
		workspaceId = user.WorkspaceIds[0]
	}
	// 注意：workspaceId 为空时，后端会使用 "default" 工作空间

	return &types.LoginResp{
		Code:          0,
		Msg:           "登录成功",
		Token:         token,
		UserId:        user.Id.Hex(),
		Username:      user.Username,
		Role:          role,
		WorkspaceId:   workspaceId,
		NeedChangePwd: user.MustChangePassword,
	}, nil
}

func (l *LoginLogic) generateToken(userId, username, role string, iat, expire int64) (string, error) {
	claims := jwt.MapClaims{
		"userId":   userId,
		"username": username,
		"role":     role,
		"iat":      iat,
		"exp":      iat + expire,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(l.svcCtx.Config.Auth.AccessSecret))
}
