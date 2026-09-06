package worker

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"cscan/api/internal/svc"
	"cscan/internal/scheduler"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/zeromicro/go-zero/core/logx"
)

// ==================== WebSocket Message Types ====================

const (
	WSTypeAuth     = "AUTH"      // 认证请求
	WSTypeAuthOK   = "AUTH_OK"   // 认证成功
	WSTypeAuthFail = "AUTH_FAIL" // 认证失败
	WSTypePing     = "PING"      // 心跳请求
	WSTypePong     = "PONG"      // 心跳响应
	WSTypeControl  = "CONTROL"   // 控制信号
)

// WSMessage WebSocket消息结构
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// AuthPayload 认证消息载荷
type AuthPayload struct {
	WorkerName string `json:"workerName"`
	InstallKey string `json:"installKey"`
}

// ControlPayload is the strict generation-bearing task control envelope.
type ControlPayload = scheduler.TaskControlEnvelope

// ==================== Worker Connection ====================

// WorkerConnection 单个Worker的WebSocket连接
type WorkerConnection struct {
	conn       net.Conn
	workerName string
	svcCtx     *svc.ServiceContext
	sendChan   chan []byte
	closeChan  chan struct{}
	closeOnce  sync.Once
	lastPing   time.Time
	mu         sync.RWMutex
}

// NewWorkerConnection 创建新的Worker连接
func NewWorkerConnection(conn net.Conn, workerName string, svcCtx *svc.ServiceContext) *WorkerConnection {
	return &WorkerConnection{
		conn:       conn,
		workerName: workerName,
		svcCtx:     svcCtx,
		sendChan:   make(chan []byte, 256),
		closeChan:  make(chan struct{}),
		lastPing:   time.Now(),
	}
}

// GetWorkerName 获取Worker名称（并发安全）
func (wc *WorkerConnection) GetWorkerName() string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.workerName
}

// SetWorkerName 设置Worker名称（并发安全），供 RenameConnection 等场景使用
func (wc *WorkerConnection) SetWorkerName(name string) {
	wc.mu.Lock()
	wc.workerName = name
	wc.mu.Unlock()
}

// Send 发送消息到Worker
func (wc *WorkerConnection) Send(msg *WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case wc.sendChan <- data:
		return nil
	case <-wc.closeChan:
		return ErrConnectionClosed
	default:
		return ErrSendBufferFull
	}
}

// Close 关闭连接
func (wc *WorkerConnection) Close() {
	wc.closeOnce.Do(func() {
		close(wc.closeChan)
	})
}

// isClosed 检查连接是否已关闭（非阻塞）
func (wc *WorkerConnection) isClosed() bool {
	select {
	case <-wc.closeChan:
		return true
	default:
		return false
	}
}

// UpdateLastPing 更新最后心跳时间
func (wc *WorkerConnection) UpdateLastPing() {
	wc.mu.Lock()
	wc.lastPing = time.Now()
	wc.mu.Unlock()
}

// GetLastPing 获取最后心跳时间
func (wc *WorkerConnection) GetLastPing() time.Time {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.lastPing
}

// ==================== File Operation Methods ====================

// ==================== WebSocket Handler ====================

// WorkerWSHandler WebSocket处理器
type WorkerWSHandler struct {
	svcCtx      *svc.ServiceContext
	connections sync.Map // workerName -> *WorkerConnection
}

// 错误定义
var (
	ErrConnectionClosed = &WSError{Code: 1000, Message: "connection closed"}
	ErrSendBufferFull   = &WSError{Code: 1001, Message: "send buffer full"}
	ErrAuthFailed       = &WSError{Code: 1002, Message: "authentication failed"}
	ErrInvalidMessage   = &WSError{Code: 1003, Message: "invalid message"}
)

type WSError struct {
	Code    int
	Message string
}

func (e *WSError) Error() string {
	return e.Message
}

// NewWorkerWSHandler 创建WebSocket处理器
func NewWorkerWSHandler(svcCtx *svc.ServiceContext) *WorkerWSHandler {
	h := &WorkerWSHandler{
		svcCtx: svcCtx,
	}

	// 启动 Worker 控制命令订阅
	go h.subscribeWorkerControl()

	return h
}

// subscribeWorkerControl 订阅 Worker 控制命令频道
// 修复 C-09：原实现 for msg := range ch 在 Redis 连接断开时 channel 关闭，
// goroutine 直接退出，后续所有 worker 控制命令（stop/restart/rename）永久失效。
// 现增加断线重连+指数退避。
func (h *WorkerWSHandler) subscribeWorkerControl() {
	ctx := context.Background()
	const maxBackoff = 30 * time.Second
	backoff := time.Second

	for {
		pubsub := h.svcCtx.RedisClient.Subscribe(ctx, "cscan:worker:control")
		ch := pubsub.Channel()

		// 等待订阅确认
		if _, err := pubsub.Receive(ctx); err != nil {
			logx.Errorf("[WorkerWS] Subscribe cscan:worker:control failed: %v, retry in %v", err, backoff)
			pubsub.Close()
			h.sleepCtx(ctx, backoff)
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		backoff = time.Second
		logx.Info("[WorkerWS] Subscribed to worker control channel")

	consumeLoop:
		for msg := range ch {
			if msg == nil {
				break consumeLoop
			}
			h.handleWorkerControlMessage(msg.Payload)
		}

		logx.Errorf("[WorkerWS] Worker control subscription disconnected, reconnecting in %v...", backoff)
		pubsub.Close()
		h.sleepCtx(ctx, backoff)
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// handleWorkerControlMessage 处理单条 worker 控制命令
func (h *WorkerWSHandler) handleWorkerControlMessage(payloadStr string) {
	// 解析控制命令
	var cmd struct {
		Action      string `json:"action"`
		WorkerName  string `json:"workerName"`
		NewName     string `json:"newName,omitempty"`
		Concurrency int    `json:"concurrency,omitempty"`
	}
	if err := json.Unmarshal([]byte(payloadStr), &cmd); err != nil {
		logx.Errorf("[WorkerWS] Invalid control command: %v", err)
		return
	}

	logx.Infof("[WorkerWS] Received control command: action=%s, worker=%s", cmd.Action, cmd.WorkerName)

	// 获取 Worker 连接
	conn, ok := h.GetConnection(cmd.WorkerName)
	if !ok {
		logx.Infof("[WorkerWS] Worker %s not connected, skipping control command", cmd.WorkerName)
		return
	}

	// 构造并发送控制消息
	var payload []byte
	switch cmd.Action {
	case "stop":
		payload, _ = json.Marshal(map[string]interface{}{
			"action": "WORKER_STOP",
		})
	case "restart":
		payload, _ = json.Marshal(map[string]interface{}{
			"action": "WORKER_RESTART",
		})
	case "rename":
		payload, _ = json.Marshal(map[string]interface{}{
			"action":  "WORKER_RENAME",
			"newName": cmd.NewName,
		})
		// 同时更新服务端的连接映射
		if cmd.NewName != "" && cmd.NewName != cmd.WorkerName {
			h.RenameConnection(cmd.WorkerName, cmd.NewName)
		}
	case "setConcurrency":
		payload, _ = json.Marshal(map[string]interface{}{
			"action":      "WORKER_SET_CONCURRENCY",
			"concurrency": cmd.Concurrency,
		})
	default:
		logx.Infof("[WorkerWS] Unknown control action: %s", cmd.Action)
		return
	}

	// 发送控制消息给 Worker
	if err := conn.Send(&WSMessage{
		Type:    WSTypeControl,
		Payload: payload,
	}); err != nil {
		logx.Errorf("[WorkerWS] Failed to send control command to %s: %v", cmd.WorkerName, err)
	} else {
		logx.Infof("[WorkerWS] Sent control command to %s: %s", cmd.WorkerName, cmd.Action)
	}
}

// sleepCtx 可被 ctx 取消的 sleep
func (h *WorkerWSHandler) sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// nextBackoff 指数退避，上限为 max
func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}

// GetConnection 获取Worker连接
func (h *WorkerWSHandler) GetConnection(workerName string) (*WorkerConnection, bool) {
	if conn, ok := h.connections.Load(workerName); ok {
		return conn.(*WorkerConnection), true
	}
	return nil, false
}

// RenameConnection 重命名Worker连接映射
// 当Worker被重命名时，需要同步更新WebSocket连接映射的key
func (h *WorkerWSHandler) RenameConnection(oldName, newName string) {
	if oldName == newName || newName == "" {
		return
	}

	// 获取旧连接
	if conn, ok := h.connections.Load(oldName); ok {
		workerConn := conn.(*WorkerConnection)
		// 更新连接的workerName（并发安全，可能正被 readPump/心跳/任务回调读取）
		workerConn.SetWorkerName(newName)
		// 存储到新key
		h.connections.Store(newName, workerConn)
		// 删除旧key
		h.connections.Delete(oldName)

		logx.Infof("[WorkerWS] Connection renamed: %s -> %s", oldName, newName)
	}
}

// ==================== HTTP Handler ====================

// WorkerWSEndpointHandler WebSocket端点处理器
// GET /api/v1/worker/ws
func WorkerWSEndpointHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 修复 C-26：升级前校验 Origin，防止 CSWSH
		// Worker 是非浏览器客户端，Origin 通常为空，validateWSOrigin 允许空 Origin
		if !validateWSOrigin(r) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}

		// 升级HTTP连接为WebSocket
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			logx.Errorf("[WorkerWS] Failed to upgrade connection: %v", err)
			return
		}

		// 创建连接上下文
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// 处理连接
		handleWebSocketConnection(ctx, conn, svcCtx, wsHandler)
	}
}

// validateWSOrigin 校验 WebSocket 升级请求的 Origin 头，防止 CSWSH 攻击
// 修复 C-26：原 UpgradeHTTP 调用未校验 Origin，浏览器发起的跨站请求可自动携带 cookie/token，
// 存在跨站 WebSocket 劫持风险。
// 策略：
//   - Origin 为空：允许（非浏览器客户端如 Worker、curl 不发送 Origin）
//   - Origin 存在：必须与请求 Host 同源（scheme+host+port 一致）
func validateWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// 非浏览器客户端，由后续 install key / JWT 认证负责
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := parsed.Hostname()
	originPort := parsed.Port()
	if originHost == "" {
		return false
	}

	// 解析请求 Host
	reqHost := r.Host
	if h, p, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
		reqPort := p
		// 同源比较：host 一致，且端口一致（或都为默认端口）
		return isSameHostPort(originHost, originPort, reqHost, reqPort)
	}
	// 请求 Host 无端口（默认 80/443）
	return isSameHostPort(originHost, originPort, reqHost, "")
}

// isSameHostPort 比较 host:port 是否同源（考虑默认端口）
func isSameHostPort(originHost, originPort, reqHost, reqPort string) bool {
	if originHost != reqHost {
		return false
	}
	// 实际部署中前端与 API 同源时端口必然一致；
	// 任一端口为空表示使用默认端口，视为同源
	return originPort == reqPort || originPort == "" || reqPort == ""
}

// handleWebSocketConnection 处理WebSocket连接
func handleWebSocketConnection(ctx context.Context, conn net.Conn, svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) {
	defer conn.Close()

	// 等待认证消息（超时30秒）
	authCtx, authCancel := context.WithTimeout(ctx, 30*time.Second)
	defer authCancel()

	workerName, err := waitForAuth(authCtx, conn, svcCtx)
	if err != nil {
		logx.Errorf("[WorkerWS] Authentication failed: %v", err)
		sendAuthFail(conn, err.Error())
		return
	}

	// 认证成功，发送AUTH_OK
	sendAuthOK(conn)
	logx.Infof("[WorkerWS] Worker authenticated: %s", workerName)

	// 创建Worker连接
	wc := NewWorkerConnection(conn, workerName, svcCtx)

	// 检查是否已有同名连接，如果有则关闭旧连接
	if oldConn, ok := wsHandler.connections.Load(workerName); ok {
		if old, ok := oldConn.(*WorkerConnection); ok {
			old.Close()
		}
	}

	// 注册连接
	wsHandler.connections.Store(workerName, wc)
	defer func() {
		wsHandler.connections.Delete(workerName)
		wc.Close()
		logx.Infof("[WorkerWS] Worker disconnected: %s", workerName)
	}()

	// 启动控制信号订阅
	go subscribeControlSignals(ctx, wc, svcCtx)

	// 启动发送协程
	go writePump(ctx, conn, wc)

	// 启动心跳检测
	go heartbeatChecker(ctx, wc)

	// 主循环：读取消息
	readPump(ctx, conn, wc, svcCtx)
}

// ==================== Authentication ====================

// waitForAuth 等待认证消息
func waitForAuth(ctx context.Context, conn net.Conn, svcCtx *svc.ServiceContext) (string, error) {
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	// 读取认证消息
	data, _, err := wsutil.ReadClientData(conn)
	if err != nil {
		return "", err
	}

	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return "", ErrInvalidMessage
	}

	if msg.Type != WSTypeAuth {
		return "", ErrAuthFailed
	}

	var authPayload AuthPayload
	if err := json.Unmarshal(msg.Payload, &authPayload); err != nil {
		return "", ErrInvalidMessage
	}

	// 验证Install Key
	if err := validateInstallKey(ctx, svcCtx, authPayload.InstallKey); err != nil {
		return "", err
	}

	if authPayload.WorkerName == "" {
		return "", ErrAuthFailed
	}

	return authPayload.WorkerName, nil
}

// validateInstallKey 验证Install Key
// 双密钥接受：环境变量 CSCAN_WORKER_KEY（默认 Worker）或 Redis install_key（手动探针）。
// 基础设施故障返回 503 错误，避免 Worker 误判密钥无效。
func validateInstallKey(ctx context.Context, svcCtx *svc.ServiceContext, installKey string) error {
	if installKey == "" {
		return ErrAuthFailed
	}

	// 双密钥校验（环境变量默认密钥 或 Redis install_key）
	valid, infraError := svcCtx.ValidateWorkerKey(ctx, installKey)
	if infraError {
		logx.Errorf("[WorkerWS] Auth service unavailable during key validation")
		return &WSError{Code: 1004, Message: "认证服务暂时不可用"}
	}
	if !valid {
		logx.Errorf("[WorkerWS] Invalid install key attempt")
		return ErrAuthFailed
	}

	return nil
}

// sendAuthOK 发送认证成功消息
func sendAuthOK(conn io.Writer) {
	msg := &WSMessage{Type: WSTypeAuthOK}
	data, _ := json.Marshal(msg)
	wsutil.WriteServerMessage(conn, ws.OpText, data)
}

// sendAuthFail 发送认证失败消息
func sendAuthFail(conn io.Writer, reason string) {
	payload, _ := json.Marshal(map[string]string{"reason": reason})
	msg := &WSMessage{Type: WSTypeAuthFail, Payload: payload}
	data, _ := json.Marshal(msg)
	wsutil.WriteServerMessage(conn, ws.OpText, data)
}

// ==================== Message Pumps ====================

// readPump 读取消息循环
func readPump(ctx context.Context, conn net.Conn, wc *WorkerConnection, svcCtx *svc.ServiceContext) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-wc.closeChan:
			return
		default:
		}

		// 设置读取超时
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		data, _, err := wsutil.ReadClientData(conn)
		if err != nil {
			logx.Errorf("[WorkerWS] Read error for %s: %v", wc.GetWorkerName(), err)
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			logx.Errorf("[WorkerWS] Invalid message from %s: %v, data: %s", wc.GetWorkerName(), err, string(data))
			continue
		}

		// 仅对非心跳消息打印日志，避免 PING/PONG 刷屏
		if msg.Type != WSTypePing && msg.Type != WSTypePong {
			logx.Infof("[WorkerWS] Received message from %s: type=%s", wc.GetWorkerName(), msg.Type)
		}

		// 路由消息
		handleMessage(ctx, wc, svcCtx, &msg)
	}
}

// writePump 发送消息循环
func writePump(ctx context.Context, conn net.Conn, wc *WorkerConnection) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-wc.closeChan:
			return
		case data := <-wc.sendChan:
			if err := wsutil.WriteServerMessage(conn, ws.OpText, data); err != nil {
				logx.Errorf("[WorkerWS] Write error for %s: %v", wc.GetWorkerName(), err)
				return
			}
		case <-ticker.C:
			// 发送PING保活
			msg := &WSMessage{Type: WSTypePing}
			data, _ := json.Marshal(msg)
			if err := wsutil.WriteServerMessage(conn, ws.OpText, data); err != nil {
				logx.Errorf("[WorkerWS] Ping error for %s: %v", wc.GetWorkerName(), err)
				return
			}
		}
	}
}

// heartbeatChecker 心跳检测
func heartbeatChecker(ctx context.Context, wc *WorkerConnection) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-wc.closeChan:
			return
		case <-ticker.C:
			// 检查最后心跳时间，超过90秒未收到心跳则断开
			if time.Since(wc.GetLastPing()) > 90*time.Second {
				logx.Infof("[WorkerWS] Heartbeat timeout for %s", wc.GetWorkerName())
				wc.Close()
				return
			}
		}
	}
}

// ==================== Message Routing ====================

// handleMessage 处理消息路由
func handleMessage(ctx context.Context, wc *WorkerConnection, svcCtx *svc.ServiceContext, msg *WSMessage) {
	switch msg.Type {
	case WSTypePing:
		handlePing(wc)
	case WSTypePong:
		handlePong(wc)
	default:
		logx.Infof("[WorkerWS] Unknown message type from %s: %s", wc.GetWorkerName(), msg.Type)
	}
}

// handlePing 处理PING消息
func handlePing(wc *WorkerConnection) {
	wc.UpdateLastPing()
	// 发送PONG响应
	wc.Send(&WSMessage{Type: WSTypePong})
}

// handlePong 处理PONG消息
func handlePong(wc *WorkerConnection) {
	wc.UpdateLastPing()
}

// ==================== Control Signal Subscription ====================

// subscribeControlSignals relays only strict, generation-bearing Redis
// envelopes. Plaintext and channel/payload identity mismatches are rejected.
func subscribeControlSignals(ctx context.Context, wc *WorkerConnection, svcCtx *svc.ServiceContext) {
	const maxBackoff = 30 * time.Second
	backoff := time.Second

	for {
		if ctx.Err() != nil || wc.isClosed() {
			return
		}

		pattern := scheduler.TaskControlChannelPattern()
		pubsub := svcCtx.RedisClient.PSubscribe(ctx, pattern)
		ch := pubsub.Channel()
		if _, err := pubsub.Receive(ctx); err != nil {
			if ctx.Err() != nil || wc.isClosed() {
				pubsub.Close()
				return
			}
			logx.Errorf("[WorkerWS] PSubscribe %s failed for %s: %v, retry in %v",
				pattern, wc.GetWorkerName(), err, backoff)
			pubsub.Close()
			controlSleepCtx(ctx, wc, backoff)
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		backoff = time.Second

	consumeLoop:
		for {
			select {
			case <-ctx.Done():
				pubsub.Close()
				return
			case <-wc.closeChan:
				pubsub.Close()
				return
			case msg, ok := <-ch:
				if !ok || msg == nil {
					logx.Errorf("[WorkerWS] Task control subscription closed for %s, reconnecting", wc.GetWorkerName())
					pubsub.Close()
					break consumeLoop
				}
				envelope, err := scheduler.ParseTaskControlEnvelope([]byte(msg.Payload))
				if err != nil {
					logx.Errorf("[WorkerWS] Rejected malformed task control on %s: %v", msg.Channel, err)
					continue
				}
				expectedChannel, _ := envelope.Key()
				if msg.Channel != expectedChannel {
					logx.Errorf("[WorkerWS] Rejected task control channel mismatch: channel=%s expected=%s", msg.Channel, expectedChannel)
					continue
				}
				payload, err := json.Marshal(envelope)
				if err != nil {
					continue
				}
				if err := wc.Send(&WSMessage{Type: WSTypeControl, Payload: payload}); err != nil {
					logx.Errorf("[WorkerWS] Failed to relay task control to %s: %v", wc.GetWorkerName(), err)
					continue
				}
				logx.Infof("[WorkerWS] Forwarded task control to %s: taskId=%s generation=%s action=%s",
					wc.GetWorkerName(), envelope.TaskID, envelope.DispatchGeneration, envelope.Action)
			}
		}

		controlSleepCtx(ctx, wc, backoff)
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// controlSleepCtx 可被 ctx / wc.closeChan 取消的 sleep
func controlSleepCtx(ctx context.Context, wc *WorkerConnection, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-wc.closeChan:
	case <-t.C:
	}
}

// ==================== Terminal Output Handling ====================
