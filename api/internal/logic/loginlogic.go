package logic

import (
	"context"
	"errors"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

// loginTimeout 登录请求的 MongoDB 查询超时时间。
// 远小于 HTTP 服务器 5 分钟总超时，避免 MongoDB 故障时登录请求长时间挂起。
const loginTimeout = 3 * time.Second

// ErrAuthServiceUnavailable 表示认证服务（依赖 MongoDB）暂时不可用。
// 调用方应将其映射为 HTTP 503，而不是 401。
var ErrAuthServiceUnavailable = errors.New("authentication service temporarily unavailable")

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
	// 为 MongoDB 查询单独设置超时，避免 MongoDB 故障时整个请求挂起 5 分钟。
	// 使用单独的 context 而非 l.ctx，确保超时只影响数据库查询，不影响后续 token 生成。
	verifyCtx, cancel := context.WithTimeout(l.ctx, loginTimeout)
	defer cancel()

	// 验证用户名密码
	user, ok, verifyErr := l.svcCtx.UserModel.VerifyPassword(verifyCtx, req.Username, req.Password)
	if verifyErr != nil {
		// 基础设施错误（MongoDB 故障、context 超时等）必须与认证失败区分开。
		// 不能返回 401 "用户名或密码错误"，否则会误导用户。
		l.Logger.Errorf("login verify failed: username=%s err=%v", req.Username, verifyErr)
		return nil, ErrAuthServiceUnavailable
	}
	if !ok {
		// 认证失败：进一步区分"待审核"/"已禁用"/"密码错误"
		user, findErr := l.svcCtx.UserModel.FindByUsername(verifyCtx, req.Username)
		if findErr != nil {
			l.Logger.Errorf("login find user failed: username=%s err=%v", req.Username, findErr)
		}
		if user != nil {
			if user.Status == "pending" {
				return &types.LoginResp{
					Code: 10004,
					Msg:  "账号待管理员审核",
				}, nil
			}
			if user.Status == model.StatusDisable {
				return &types.LoginResp{
					Code: 10003,
					Msg:  "账号已被禁用",
				}, nil
			}
		}
		return &types.LoginResp{
			Code: 401,
			Msg:  "用户名或密码错误",
		}, nil
	}

	// 更新登录时间（非关键路径，失败不影响登录）
	if err := l.svcCtx.UserModel.UpdateLoginTime(l.ctx, user.Id.Hex()); err != nil {
		l.Logger.Errorf("更新登录时间失败: %v", err)
	}

	// 确定用户角色，默认为 user
	role := user.Role
	if role == "" {
		role = model.RoleUser
	}

	// 生成JWT Token
	now := time.Now().Unix()
	accessExpire := l.svcCtx.Config.Auth.AccessExpire
	token, err := l.generateToken(user.Id.Hex(), user.Username, role, now, accessExpire)
	if err != nil {
		l.Logger.Errorf("generate token failed: username=%s err=%v", req.Username, err)
		return nil, err
	}

	return &types.LoginResp{
		Code:      0,
		Msg:       "登录成功",
		Token:     token,
		UserId:    user.Id.Hex(),
		Username:  user.Username,
		Role:      role,
		MenuPaths: l.svcCtx.MenuPathsForRole(l.ctx, role),
		IsAdmin:   l.svcCtx.IsAdminRole(l.ctx, role),
		AllPaths:  model.MenuPathList(),
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
