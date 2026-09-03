package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cscan/internal/model"
	"cscan/pkg/response"

	"github.com/golang-jwt/jwt/v4"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ContextKey string

const (
	UserIdKey   ContextKey = "userId"
	UsernameKey ContextKey = "username"
	RoleKey     ContextKey = "role"
	TokenIdKey  ContextKey = "tokenId"
)

// PATLookup 由调用方注入的 PAT 认证回调。
// 入参为 token 明文，返回 (userId, role, status, tokenId, scopes, error)；
// token 不存在/失效时返回 (primitive.NilObjectID, "", "", primitive.NilObjectID, nil, nil)。
type PATLookup func(ctx context.Context, token string) (userId primitive.ObjectID, role, status string, tokenId primitive.ObjectID, scopes []string, err error)

// PATUsageRecorder 异步记录 PAT 使用信息的回调（可不提供）
type PATUsageRecorder func(ctx context.Context, tokenId primitive.ObjectID, ip string)

// RoleAdminLookup 由调用方注入的管理员角色判定回调。
// 入参为角色名，返回该角色是否具备管理员接口权限。
type RoleAdminLookup func(ctx context.Context, role string) bool

type AuthMiddleware struct {
	AccessSecret string
	UserModel    *model.UserModel
	PATLookup    PATLookup
	PATRecorder  PATUsageRecorder
	RateLimiter  *TokenRateLimiter // PAT 认证限流器（可选）
	RoleAdmin    RoleAdminLookup   // 管理员角色判定（可选，未注入时仅认内置 admin/superadmin）
}

func NewAuthMiddleware(accessSecret string) *AuthMiddleware {
	return &AuthMiddleware{
		AccessSecret: accessSecret,
	}
}

// WithPAT 注入 PAT 查询与审计回调，启用 PAT 认证路径
func (m *AuthMiddleware) WithPAT(lookup PATLookup, recorder PATUsageRecorder, userModel *model.UserModel) *AuthMiddleware {
	m.PATLookup = lookup
	m.PATRecorder = recorder
	m.UserModel = userModel
	return m
}

// WithRateLimiter 注入限流器，为 PAT 认证路径提供暴力破解防护
func (m *AuthMiddleware) WithRateLimiter(limiter *TokenRateLimiter) *AuthMiddleware {
	m.RateLimiter = limiter
	return m
}

// WithRoleAdmin 注入管理员角色判定回调，使自定义角色也能被授予管理员权限
func (m *AuthMiddleware) WithRoleAdmin(lookup RoleAdminLookup) *AuthMiddleware {
	m.RoleAdmin = lookup
	return m
}

func (m *AuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tokenStr string

		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenStr = parts[1]
			}
		}
		if tokenStr == "" {
			tokenStr = r.URL.Query().Get("token")
		}

		if tokenStr == "" {
			unauthorized(w, "未提供认证信息")
			return
		}

		// PAT 路径：以 cscan_pat_ 前缀开头，优先走 PAT 认证
		if strings.HasPrefix(tokenStr, model.PATPrefix) {
			if m.PATLookup == nil {
				unauthorized(w, "Token无效或已过期")
				return
			}
			m.handlePAT(w, r, tokenStr, next)
			return
		}

		// JWT 路径
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(m.AccessSecret), nil
		})
		if err != nil || !token.Valid {
			// 区分过期 / 签名错误 / 其他无效，便于前端精准引导用户重新登录
			if ve, ok := err.(*jwt.ValidationError); ok {
				if ve.Errors&jwt.ValidationErrorExpired != 0 {
					unauthorized(w, "Token已过期，请重新登录")
					return
				}
				if ve.Errors&jwt.ValidationErrorSignatureInvalid != 0 {
					unauthorized(w, "Token签名校验失败")
					return
				}
			}
			unauthorized(w, "Token无效或已过期")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			unauthorized(w, "Token解析失败")
			return
		}

		ctx := r.Context()
		if userId, ok := claims["userId"].(string); ok {
			ctx = context.WithValue(ctx, UserIdKey, userId)
		}
		if username, ok := claims["username"].(string); ok {
			ctx = context.WithValue(ctx, UsernameKey, username)
		}
		if role, ok := claims["role"].(string); ok {
			ctx = context.WithValue(ctx, RoleKey, role)
		}

		next(w, r.WithContext(ctx))
	}
}

func (m *AuthMiddleware) handlePAT(w http.ResponseWriter, r *http.Request, tokenStr string, next http.HandlerFunc) {
	ctx := r.Context()

	// PAT 认证限流：防止暴力破解
	if m.RateLimiter != nil {
		clientKey := tokenStr // 使用 token 本身作为限流 key
		if len(clientKey) > 32 {
			clientKey = clientKey[:32] // 避免 key 过长
		}
		b := m.RateLimiter.bucket(clientKey)
		if !b.allow() {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			response.ErrorWithCode(w, http.StatusTooManyRequests, "PAT认证请求过于频繁")
			return
		}
	}

	uid, role, status, tokenId, scopes, err := m.PATLookup(ctx, tokenStr)
	if err != nil {
		unauthorized(w, "Token无效或已过期")
		return
	}
	if uid.IsZero() {
		unauthorized(w, "Token无效或已过期")
		return
	}
	if status != model.StatusEnable {
		unauthorized(w, "Token已失效")
		return
	}
	if role == "" {
		role = "user"
	}

	if !ScopeAllowed(scopes, r.URL.Path) {
		forbidden(w, "Token不允许调用此API分组")
		return
	}

	newCtx := context.WithValue(ctx, UserIdKey, uid.Hex())
	newCtx = context.WithValue(newCtx, UsernameKey, "")
	newCtx = context.WithValue(newCtx, RoleKey, role)
	newCtx = context.WithValue(newCtx, TokenIdKey, tokenId.Hex())

	// 异步记录使用信息，避免阻塞请求
	if m.PATRecorder != nil {
		ip := clientIP(r)
		go func(id primitive.ObjectID, ipStr string) {
			recCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			m.PATRecorder(recCtx, id, ipStr)
		}(tokenId, ip)
	}

	next(w, r.WithContext(newCtx))
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if idx := strings.Index(v, ","); idx > 0 {
			return strings.TrimSpace(v[:idx])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return strings.TrimSpace(v)
	}
	// 截掉端口
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}
	return strings.TrimSpace(host)
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 401,
		"msg":  msg,
	})
}

func forbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 403,
		"msg":  msg,
	})
}

// GetUserId 从Context获取用户ID
func GetUserId(ctx context.Context) string {
	if v := ctx.Value(UserIdKey); v != nil {
		return v.(string)
	}
	return ""
}

// GetUsername 从Context获取用户名
func GetUsername(ctx context.Context) string {
	if v := ctx.Value(UsernameKey); v != nil {
		return v.(string)
	}
	return ""
}

// GetRole 从Context获取角色
func GetRole(ctx context.Context) string {
	if v := ctx.Value(RoleKey); v != nil {
		return v.(string)
	}
	return ""
}

// GetTokenId 从Context获取当前 PAT 的 tokenId（未走 PAT 认证时为空）
func GetTokenId(ctx context.Context) string {
	if v := ctx.Value(TokenIdKey); v != nil {
		return v.(string)
	}
	return ""
}

// RequireAdmin 管理员权限中间件，需要先经过认证中间件。
// 内置 admin/superadmin 直接放行；自定义角色由注入的 RoleAdmin 回调按角色标志判定。
func (m *AuthMiddleware) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.IsAdmin(r.Context()) {
			forbidden(w, "需要管理员权限")
			return
		}
		next(w, r)
	}
}

// IsAdmin 判定当前请求上下文的角色是否具备管理员权限
func (m *AuthMiddleware) IsAdmin(ctx context.Context) bool {
	role := GetRole(ctx)
	if role == model.RoleAdmin || role == model.RoleSuperadmin {
		return true
	}
	if m.RoleAdmin == nil {
		return false
	}
	return m.RoleAdmin(ctx, role)
}
