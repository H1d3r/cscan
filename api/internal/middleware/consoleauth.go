package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

// ConsoleAuthMiddleware 控制台权限中间件（仅管理员可访问）
type ConsoleAuthMiddleware struct {
	RedisClient *redis.Client
}

// NewConsoleAuthMiddleware 创建控制台权限中间件
func NewConsoleAuthMiddleware(redisClient *redis.Client) *ConsoleAuthMiddleware {
	return &ConsoleAuthMiddleware{
		RedisClient: redisClient,
	}
}

// Handle 控制台权限检查处理
func (m *ConsoleAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 从Context获取用户角色（需要先经过AuthMiddleware）
		role := GetRole(r.Context())
		if role != "admin" && role != "superadmin" {
			logx.Errorf("[ConsoleAuth] Access denied for non-admin user, role: %s, path: %s", role, r.URL.Path)
			consoleForbidden(w, "需要管理员权限访问控制台")
			return
		}

		// 记录控制台访问日志
		go m.recordConsoleAccess(r)

		next(w, r)
	}
}

// recordConsoleAccess 记录控制台访问日志
func (m *ConsoleAuthMiddleware) recordConsoleAccess(r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 获取用户信息
	userId := GetUserId(r.Context())
	username := GetUsername(r.Context())

	// 构建访问日志
	accessLog := map[string]interface{}{
		"type":      "console_access",
		"userId":    userId,
		"username":  username,
		"path":      r.URL.Path,
		"method":    r.Method,
		"clientIP":  getClientIPFromRequest(r),
		"userAgent": r.UserAgent(),
		"timestamp": time.Now().UnixMilli(),
	}

	logJSON, err := json.Marshal(accessLog)
	if err != nil {
		return
	}

	// 写入Redis审计日志流
	m.RedisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: "cscan:audit:console",
		MaxLen: 10000,
		Approx: true,
		Values: map[string]interface{}{"data": string(logJSON)},
	})
}

// consoleForbidden 返回403禁止访问响应
func consoleForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 403,
		"msg":  msg,
	})
}

// getClientIPFromRequest 从请求获取客户端IP
func getClientIPFromRequest(r *http.Request) string {
	// 尝试从X-Forwarded-For获取
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// 取第一个IP
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// 尝试从X-Real-IP获取
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// 从RemoteAddr获取
	ip := r.RemoteAddr
	// 移除端口号
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == ':' {
			return ip[:i]
		}
	}
	return ip
}
