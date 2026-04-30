package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/model"
	"cscan/pkg/response"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

// ==================== Terminal HTTP Handlers ====================

// WorkerTerminalOpenHandler 打开终端会话
// POST /api/v1/worker/console/terminal/open
func WorkerTerminalOpenHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		var req struct {
			WorkerName string `json:"workerName"`
			SessionId  string `json:"sessionId"`
			Cols       int    `json:"cols"`
			Rows       int    `json:"rows"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.ParamError(w, "invalid request body")
			return
		}

		if req.WorkerName == "" {
			response.ParamError(w, "workerName is required")
			return
		}

		if req.SessionId == "" {
			req.SessionId = fmt.Sprintf("%d", time.Now().UnixNano())
		}

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(req.WorkerName)
		if !ok {
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, req.WorkerName, req.SessionId, "", false, "worker not connected", time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusNotFound, "worker not connected")
			return
		}

		// 检查会话数限制
		maxSessions := svcCtx.Config.Console.GetMaxSessionsPerWorker()
		currentSessions := wsHandler.GetWorkerSessionCount(req.WorkerName)
		if currentSessions >= maxSessions {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, req.WorkerName, req.SessionId, "", false, "session limit exceeded", time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusTooManyRequests, fmt.Sprintf("maximum sessions per worker reached (%d)", maxSessions))
			return
		}

		// 请求打开终端
		resp, err := conn.RequestTerminalOpen(req.SessionId, req.Cols, req.Rows, 30*time.Second)
		if err != nil {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, req.WorkerName, req.SessionId, "", false, err.Error(), time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusInternalServerError, "failed to open terminal: "+err.Error())
			return
		}

		if !resp.Success {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, req.WorkerName, req.SessionId, "", false, resp.Error, time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusInternalServerError, "failed to open terminal: "+resp.Error)
			return
		}

		// 记录会话
		wsHandler.AddWorkerSession(req.WorkerName, req.SessionId)

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, req.WorkerName, req.SessionId, "", true, "", time.Since(startTime))
		}

		response.Success(w, map[string]interface{}{
			"sessionId": resp.SessionId,
		})
	}
}

// WorkerTerminalCloseHandler 关闭终端会话
// POST /api/v1/worker/console/terminal/close
func WorkerTerminalCloseHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		var req struct {
			WorkerName string `json:"workerName"`
			SessionId  string `json:"sessionId"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.ParamError(w, "invalid request body")
			return
		}

		if req.WorkerName == "" || req.SessionId == "" {
			response.ParamError(w, "workerName and sessionId are required")
			return
		}

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(req.WorkerName)
		if !ok {
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalClose, req.WorkerName, req.SessionId, "", false, "worker not connected", time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusNotFound, "worker not connected")
			return
		}

		// 请求关闭终端
		resp, err := conn.RequestTerminalClose(req.SessionId, 10*time.Second)
		if err != nil {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalClose, req.WorkerName, req.SessionId, "", false, err.Error(), time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusInternalServerError, "failed to close terminal: "+err.Error())
			return
		}

		if !resp.Success {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalClose, req.WorkerName, req.SessionId, "", false, resp.Error, time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusInternalServerError, "failed to close terminal: "+resp.Error)
			return
		}

		// 移除会话记录
		wsHandler.RemoveWorkerSession(req.WorkerName, req.SessionId)

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalClose, req.WorkerName, req.SessionId, "", true, "", time.Since(startTime))
		}

		response.Success(w, nil)
	}
}

// WorkerTerminalExecHandler 执行终端命令
// POST /api/v1/worker/console/terminal/exec
func WorkerTerminalExecHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		var req struct {
			WorkerName string `json:"workerName"`
			SessionId  string `json:"sessionId"`
			Command    string `json:"command"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.ParamError(w, "invalid request body")
			return
		}

		if req.WorkerName == "" || req.SessionId == "" || req.Command == "" {
			response.ParamError(w, "workerName, sessionId and command are required")
			return
		}

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(req.WorkerName)
		if !ok {
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalExec, req.WorkerName, req.SessionId, req.Command, false, "worker not connected", time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusNotFound, "worker not connected")
			return
		}

		// 请求执行命令
		resp, err := conn.RequestTerminalInput(req.SessionId, "", req.Command, 30*time.Second)

		// 记录命令历史
		history := &model.CommandHistory{
			WorkerName: req.WorkerName,
			SessionId:  req.SessionId,
			Command:    req.Command,
			Duration:   time.Since(startTime).Milliseconds(),
			ClientIP:   getClientIP(r),
			CreateTime: startTime,
		}

		if err != nil {
			history.Success = false
			history.Error = err.Error()
			// 异步记录命令历史
			go recordCommandHistory(svcCtx, history)
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalExec, req.WorkerName, req.SessionId, req.Command, false, err.Error(), time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusInternalServerError, "failed to execute command: "+err.Error())
			return
		}

		if !resp.Success {
			history.Success = false
			history.Error = resp.Error
			// 异步记录命令历史
			go recordCommandHistory(svcCtx, history)
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalExec, req.WorkerName, req.SessionId, req.Command, false, resp.Error, time.Since(startTime))
			}
			response.ErrorWithCode(w, http.StatusInternalServerError, "failed to execute command: "+resp.Error)
			return
		}

		history.Success = true
		// 异步记录命令历史
		go recordCommandHistory(svcCtx, history)
		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalExec, req.WorkerName, req.SessionId, req.Command, true, "", time.Since(startTime))
		}

		response.Success(w, nil)
	}
}

// WorkerTerminalHistoryHandler 获取命令历史
// GET /api/v1/worker/console/terminal/history
func WorkerTerminalHistoryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workerName := r.URL.Query().Get("workerName")
		sessionId := r.URL.Query().Get("sessionId")

		// 解析分页参数
		page := 1
		pageSize := 20
		if p := r.URL.Query().Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		if ps := r.URL.Query().Get("pageSize"); ps != "" {
			fmt.Sscanf(ps, "%d", &pageSize)
		}

		// 限制pageSize
		if pageSize > 100 {
			pageSize = 100
		}
		if pageSize < 1 {
			pageSize = 20
		}
		if page < 1 {
			page = 1
		}

		ctx := r.Context()

		// 如果指定了sessionId，获取会话的命令历史
		if sessionId != "" {
			histories, err := svcCtx.CommandHistoryModel.GetBySession(ctx, sessionId)
			if err != nil {
				response.ErrorWithCode(w, http.StatusInternalServerError, "failed to get command history: "+err.Error())
				return
			}
			response.Success(w, map[string]interface{}{
				"list":  histories,
				"total": len(histories),
			})
			return
		}

		// 如果指定了workerName，获取Worker的命令历史
		if workerName != "" {
			histories, total, err := svcCtx.CommandHistoryModel.GetByWorker(ctx, workerName, page, pageSize)
			if err != nil {
				response.ErrorWithCode(w, http.StatusInternalServerError, "failed to get command history: "+err.Error())
				return
			}
			response.Success(w, map[string]interface{}{
				"list":     histories,
				"total":    total,
				"page":     page,
				"pageSize": pageSize,
			})
			return
		}

		// 获取最近的命令历史
		histories, err := svcCtx.CommandHistoryModel.GetRecent(ctx, pageSize)
		if err != nil {
			response.ErrorWithCode(w, http.StatusInternalServerError, "failed to get command history: "+err.Error())
			return
		}
		response.Success(w, map[string]interface{}{
			"list":  histories,
			"total": len(histories),
		})
	}
}

// ==================== Terminal WebSocket Handler ====================

// WorkerTerminalWSHandler 终端WebSocket端点
// GET /api/v1/worker/console/:name/terminal
func WorkerTerminalWSHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// 从URL获取Worker名称
		workerName := r.URL.Query().Get("name")
		if workerName == "" {
			http.Error(w, "worker name is required", http.StatusBadRequest)
			return
		}

		// 获取会话ID（可选，如果没有则创建新会话）
		sessionId := r.URL.Query().Get("sessionId")
		if sessionId == "" {
			sessionId = fmt.Sprintf("%d", time.Now().UnixNano())
		}

		// 获取终端大小
		cols := 80
		rows := 24
		if c := r.URL.Query().Get("cols"); c != "" {
			fmt.Sscanf(c, "%d", &cols)
		}
		if ro := r.URL.Query().Get("rows"); ro != "" {
			fmt.Sscanf(ro, "%d", &rows)
		}

		// 获取客户端IP
		clientIP := getClientIP(r)

		// 检查会话数限制
		maxSessions := svcCtx.Config.Console.GetMaxSessionsPerWorker()
		currentSessions := wsHandler.GetWorkerSessionCount(workerName)
		if currentSessions >= maxSessions {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, workerName, sessionId, "", false, "session limit exceeded", time.Since(startTime))
			}
			http.Error(w, fmt.Sprintf("maximum sessions per worker reached (%d)", maxSessions), http.StatusTooManyRequests)
			return
		}

		// 获取Worker连接
		workerConn, ok := wsHandler.GetConnection(workerName)
		if !ok {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, workerName, sessionId, "", false, "worker not connected", time.Since(startTime))
			}
			http.Error(w, "worker not connected", http.StatusNotFound)
			return
		}

		// 升级HTTP连接为WebSocket
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			logx.Errorf("[TerminalWS] Failed to upgrade connection: %v", err)
			return
		}
		defer conn.Close()

		logx.Infof("[TerminalWS] Client connected for worker %s, session %s, clientIP %s", workerName, sessionId, clientIP)

		// 在Worker上打开终端会话
		logx.Infof("[TerminalWS] Requesting terminal open on worker %s...", workerName)
		openResp, err := workerConn.RequestTerminalOpen(sessionId, cols, rows, 30*time.Second)
		if err != nil {
			logx.Errorf("[TerminalWS] Failed to open terminal on worker %s: %v", workerName, err)
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, workerName, sessionId, "", false, err.Error(), time.Since(startTime))
			}
			sendTerminalError(conn, "failed to open terminal: "+err.Error())
			return
		}
		if !openResp.Success {
			logx.Errorf("[TerminalWS] Worker %s failed to open terminal: %s", workerName, openResp.Error)
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, workerName, sessionId, "", false, openResp.Error, time.Since(startTime))
			}
			sendTerminalError(conn, "failed to open terminal: "+openResp.Error)
			return
		}

		logx.Infof("[TerminalWS] Terminal opened successfully on worker %s, session %s", workerName, sessionId)

		// 记录会话
		wsHandler.AddWorkerSession(workerName, sessionId)

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalOpen, workerName, sessionId, "", true, "", time.Since(startTime))
		}

		// 确保会话关闭时清理
		defer func() {
			closeStartTime := time.Now()
			_, closeErr := workerConn.RequestTerminalClose(sessionId, 5*time.Second)
			wsHandler.RemoveWorkerSession(workerName, sessionId)

			// 记录关闭终端的审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				errMsg := ""
				success := true
				if closeErr != nil {
					errMsg = closeErr.Error()
					success = false
				}
				auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalClose, workerName, sessionId, "", success, errMsg, time.Since(closeStartTime))
			}

			logx.Infof("[TerminalWS] Client disconnected for worker %s, session %s", workerName, sessionId)
		}()

		// 创建上下文
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// 获取空闲超时配置
		idleTimeout := svcCtx.Config.Console.GetWSIdleTimeout()

		// 订阅终端输出
		outputChan := make(chan []byte, 256)
		go subscribeTerminalOutput(ctx, svcCtx, workerName, sessionId, outputChan)

		// 启动输出转发协程
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case data := <-outputChan:
					if err := wsutil.WriteServerMessage(conn, ws.OpText, data); err != nil {
						logx.Errorf("[TerminalWS] Failed to send output: %v", err)
						cancel()
						return
					}
				}
			}
		}()

		// 主循环：读取客户端输入
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 设置读取超时（使用配置的空闲超时）
			conn.SetReadDeadline(time.Now().Add(idleTimeout))

			data, _, err := wsutil.ReadClientData(conn)
			if err != nil {
				logx.Infof("[TerminalWS] Client read error: %v", err)
				return
			}

			// 解析消息
			var msg struct {
				Type    string `json:"type"`
				Data    string `json:"data,omitempty"`    // Base64编码的输入数据
				Command string `json:"command,omitempty"` // 直接命令
				Cols    int    `json:"cols,omitempty"`
				Rows    int    `json:"rows,omitempty"`
			}

			if err := json.Unmarshal(data, &msg); err != nil {
				logx.Errorf("[TerminalWS] Invalid message: %v", err)
				continue
			}

			switch msg.Type {
			case "input":
				// 记录命令开始时间
				cmdStartTime := time.Now()

				// 发送输入到Worker
				resp, err := workerConn.RequestTerminalInput(sessionId, msg.Data, msg.Command, 30*time.Second)

				// 如果有直接命令，记录命令历史和审计日志
				if msg.Command != "" {
					history := &model.CommandHistory{
						WorkerName: workerName,
						SessionId:  sessionId,
						Command:    msg.Command,
						Duration:   time.Since(cmdStartTime).Milliseconds(),
						ClientIP:   clientIP,
						CreateTime: cmdStartTime,
					}

					if err != nil {
						history.Success = false
						history.Error = err.Error()
					} else if resp != nil && !resp.Success {
						history.Success = false
						history.Error = resp.Error
					} else {
						history.Success = true
					}

					// 异步记录命令历史
					go recordCommandHistory(svcCtx, history)

					// 记录审计日志
					if auditSvc := GetAuditService(); auditSvc != nil {
						auditSvc.RecordTerminalOperation(r.Context(), r, model.AuditLogTypeTerminalExec, workerName, sessionId, msg.Command, history.Success, history.Error, time.Since(cmdStartTime))
					}
				}

				if err != nil {
					logx.Errorf("[TerminalWS] Failed to send input: %v", err)
				}

			case "resize":
				// 调整终端大小
				_, err := workerConn.RequestTerminalResize(sessionId, msg.Cols, msg.Rows, 10*time.Second)
				if err != nil {
					logx.Errorf("[TerminalWS] Failed to resize terminal: %v", err)
				}

			case "ping":
				// 心跳响应
				pong, _ := json.Marshal(map[string]string{"type": "pong"})
				wsutil.WriteServerMessage(conn, ws.OpText, pong)

			default:
				logx.Infof("[TerminalWS] Unknown message type: %s", msg.Type)
			}
		}
	}
}

// subscribeTerminalOutput 订阅终端输出
func subscribeTerminalOutput(ctx context.Context, svcCtx *svc.ServiceContext, workerName, sessionId string, outputChan chan<- []byte) {
	channel := fmt.Sprintf("cscan:worker:terminal:%s:%s", workerName, sessionId)
	pubsub := svcCtx.RedisClient.Subscribe(ctx, channel)
	defer pubsub.Close()

	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// 解析 Redis 消息
			var redisMsg struct {
				WorkerName string `json:"workerName"`
				SessionId  string `json:"sessionId"`
				Data       string `json:"data"` // Base64 编码的输出
				Timestamp  int64  `json:"timestamp"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &redisMsg); err != nil {
				logx.Errorf("[TerminalWS] Invalid Redis message: %v", err)
				continue
			}

			// 解码 Base64 数据
			decodedData, err := base64.StdEncoding.DecodeString(redisMsg.Data)
			if err != nil {
				logx.Errorf("[TerminalWS] Failed to decode terminal output: %v", err)
				continue
			}

			// 构造前端期望的消息格式
			frontendMsg, _ := json.Marshal(map[string]interface{}{
				"type": "output",
				"data": string(decodedData),
			})

			// 转发输出到客户端
			select {
			case outputChan <- frontendMsg:
			default:
				// 通道满了，丢弃
			}
		}
	}
}

// sendTerminalError 发送终端错误消息
func sendTerminalError(conn interface{ Write([]byte) (int, error) }, errMsg string) {
	msg, _ := json.Marshal(map[string]interface{}{
		"type":  "error",
		"error": errMsg,
	})
	wsutil.WriteServerMessage(conn, ws.OpText, msg)
}

// recordCommandHistory 异步记录命令历史
func recordCommandHistory(svcCtx *svc.ServiceContext, history *model.CommandHistory) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := svcCtx.CommandHistoryModel.RecordCommand(ctx, history); err != nil {
		logx.Errorf("[TerminalHistory] Failed to record command history: %v", err)
	}
}

// getClientIP 获取客户端IP
func getClientIP(r *http.Request) string {
	// 尝试从X-Forwarded-For获取
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// 取第一个IP
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// 尝试从X-Real-IP获取
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// 从RemoteAddr获取
	ip := r.RemoteAddr
	// 移除端口号
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// WorkerTerminalWSHandlerWithAuth 带认证的终端WebSocket端点
// 支持从 URL 参数读取 token 进行认证
// GET /api/v1/worker/console/terminal?name=xxx&token=xxx
func WorkerTerminalWSHandlerWithAuth(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 从 URL 参数获取 token
		token := r.URL.Query().Get("token")
		if token == "" {
			// 也尝试从 Authorization header 获取
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if token == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		// 验证 JWT token
		claims, err := validateJWTToken(token, svcCtx.Config.Auth.AccessSecret)
		if err != nil {
			logx.Errorf("[TerminalWS] Invalid token: %v", err)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		// 检查是否是管理员
		role, _ := claims["role"].(string)
		if role != "admin" && role != "superadmin" {
			logx.Errorf("[TerminalWS] Access denied for non-admin user, role: %s", role)
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		// 调用原始的 WebSocket handler
		WorkerTerminalWSHandler(svcCtx, wsHandler)(w, r)
	}
}

// validateJWTToken 验证 JWT token
func validateJWTToken(tokenString string, secret string) (map[string]interface{}, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}
