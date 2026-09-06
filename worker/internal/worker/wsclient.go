package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

// WSAuthPayload 认证消息载荷
type WSAuthPayload struct {
	WorkerName string `json:"workerName"`
	InstallKey string `json:"installKey"`
}

// WSControlPayload is the strict generation-bearing task control envelope.
type WSControlPayload = scheduler.TaskControlEnvelope

// ==================== WebSocket Client ====================

// WSClientConfig WebSocket客户端配�?
type WSClientConfig struct {
	ServerURL      string        // WebSocket服务器URL (e.g., ws://server:8888/api/v1/worker/ws)
	WorkerName     string        // Worker名称
	InstallKey     string        // 安装密钥
	ReconnectDelay time.Duration // 初始重连延迟
	MaxReconnect   time.Duration // 最大重连延�?
	PingInterval   time.Duration // 心跳间隔
	WriteTimeout   time.Duration // 写超时
	ReadTimeout    time.Duration // 读超时
}

// DefaultWSClientConfig 默认配置
func DefaultWSClientConfig(serverURL, workerName, installKey string) *WSClientConfig {
	return &WSClientConfig{
		ServerURL:      serverURL,
		WorkerName:     workerName,
		InstallKey:     installKey,
		ReconnectDelay: 1 * time.Second,
		MaxReconnect:   30 * time.Second,
		PingInterval:   30 * time.Second,
		WriteTimeout:   10 * time.Second,
		ReadTimeout:    90 * time.Second,
	}
}

// ControlHandler receives only a validated exact-generation envelope.
type ControlHandler func(envelope *scheduler.TaskControlEnvelope)

// WorkerControlHandler Worker级别控制处理函数类型
type WorkerControlHandler func(action string, param string)

// WorkerWSClient Worker WebSocket客户端
type WorkerWSClient struct {
	config               *WSClientConfig
	conn                 net.Conn
	connMu               sync.RWMutex
	writeMu              sync.Mutex // serializes WebSocket write operations (writePump + sendPingDirect)
	connected            atomic.Bool
	authenticated        atomic.Bool
	closeChan            chan struct{}
	closeOnce            sync.Once
	sendChan             chan []byte
	controlHandler       ControlHandler
	workerControlHandler WorkerControlHandler
	lastPong             time.Time
	pongMu               sync.RWMutex
	reconnecting         atomic.Bool
	readPaused           chan struct{} // 非 nil 时 readPump 暂停；关闭则恢复
	readPauseMu          sync.Mutex
	wg                   sync.WaitGroup
}

// NewWorkerWSClient 创建WebSocket客户�?
func NewWorkerWSClient(config *WSClientConfig) *WorkerWSClient {
	return &WorkerWSClient{
		config:    config,
		closeChan: make(chan struct{}),
		sendChan:  make(chan []byte, 4096),
		lastPong:  time.Now(),
	}
}

// SetControlHandler 设置控制信号处理函数
func (c *WorkerWSClient) SetControlHandler(handler ControlHandler) {
	c.controlHandler = handler
}

// SetWorkerControlHandler 设置Worker级别控制处理函数
func (c *WorkerWSClient) SetWorkerControlHandler(handler WorkerControlHandler) {
	c.workerControlHandler = handler
}

// IsConnected 检查是否已连接
func (c *WorkerWSClient) IsConnected() bool {
	return c.connected.Load() && c.authenticated.Load()
}

// Connect 连接到WebSocket服务�?
func (c *WorkerWSClient) Connect(ctx context.Context) error {
	return c.connectWithRetry(ctx, false)
}

// connectWithRetry 带重试的连接
func (c *WorkerWSClient) connectWithRetry(ctx context.Context, isReconnect bool) error {
	backoff := c.config.ReconnectDelay
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closeChan:
			return fmt.Errorf("client closed")
		default:
		}

		err := c.doConnect(ctx)
		if err == nil {
			// 连接成功
			if isReconnect {
				logx.Info("[WSClient] Reconnected to server")
			} else {
				logx.Info("[WSClient] Connected to server")
			}
			return nil
		}

		attempt++
		logx.Infof("[WSClient] Connection attempt %d failed: %v, retrying in %v...", attempt, err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closeChan:
			return fmt.Errorf("client closed")
		case <-time.After(backoff):
		}

		// 指数退�?
		backoff = time.Duration(float64(backoff) * 2)
		if backoff > c.config.MaxReconnect {
			backoff = c.config.MaxReconnect
		}
	}
}

// doConnect 执行单次连接
func (c *WorkerWSClient) doConnect(ctx context.Context) error {
	// 暂停 readPump，防止它与 authenticate() 竞争读同一连接
	unpause := c.pauseRead()
	defer unpause()

	// 解析WebSocket URL
	wsURL := c.buildWSURL()

	// 建立WebSocket连接
	conn, _, _, err := ws.Dial(ctx, wsURL)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
	c.connected.Store(true)

	// 发送认证消息
	if err := c.authenticate(); err != nil {
		conn.Close()
		c.connMu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.connMu.Unlock()
		c.connected.Store(false)
		return fmt.Errorf("authentication failed: %w", err)
	}

	c.authenticated.Store(true)
	c.pongMu.Lock()
	c.lastPong = time.Now()
	c.pongMu.Unlock()

	return nil
}

// buildWSURL 构建WebSocket URL
func (c *WorkerWSClient) buildWSURL() string {
	serverURL := c.config.ServerURL

	// 如果已经是ws://或wss://开头，直接使用
	if strings.HasPrefix(serverURL, "ws://") || strings.HasPrefix(serverURL, "wss://") {
		return serverURL
	}

	// 将http://转换为ws://，https://转换为wss://
	if strings.HasPrefix(serverURL, "https://") {
		serverURL = "wss://" + strings.TrimPrefix(serverURL, "https://")
	} else if strings.HasPrefix(serverURL, "http://") {
		serverURL = "ws://" + strings.TrimPrefix(serverURL, "http://")
	} else {
		serverURL = "ws://" + serverURL
	}

	// 解析URL并添加路�?
	u, err := url.Parse(serverURL)
	if err != nil {
		return serverURL + "/api/v1/worker/ws"
	}

	// 如果没有路径或路径为/，添加WebSocket路径
	if u.Path == "" || u.Path == "/" {
		u.Path = "/api/v1/worker/ws"
	}

	return u.String()
}

// authenticate 发送认证消息并等待响应
func (c *WorkerWSClient) authenticate() error {
	// 构建认证消息
	authPayload := WSAuthPayload{
		WorkerName: c.config.WorkerName,
		InstallKey: c.config.InstallKey,
	}
	payloadData, _ := json.Marshal(authPayload)

	msg := WSMessage{
		Type:    WSTypeAuth,
		Payload: payloadData,
	}
	msgData, _ := json.Marshal(msg)

	// 发送认证消�?
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection is nil, may have been closed")
	}

	if err := wsutil.WriteClientMessage(conn, ws.OpText, msgData); err != nil {
		return fmt.Errorf("send auth message failed: %w", err)
	}

	// 等待认证响应（超�?0秒）
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	data, _, err := wsutil.ReadServerData(conn)
	if err != nil {
		return fmt.Errorf("read auth response failed: %w", err)
	}

	var respMsg WSMessage
	if err := json.Unmarshal(data, &respMsg); err != nil {
		return fmt.Errorf("parse auth response failed: %w", err)
	}

	switch respMsg.Type {
	case WSTypeAuthOK:
		return nil
	case WSTypeAuthFail:
		var reason struct {
			Reason string `json:"reason"`
		}
		json.Unmarshal(respMsg.Payload, &reason)
		return fmt.Errorf("auth rejected: %s", reason.Reason)
	default:
		return fmt.Errorf("unexpected response type: %s", respMsg.Type)
	}
}

// Start 启动客户端（连接并启动读写协程）
func (c *WorkerWSClient) Start(ctx context.Context) error {
	// 连接服务�?
	if err := c.Connect(ctx); err != nil {
		return err
	}

	// 启动读取协程
	c.wg.Add(1)
	go c.readPump(ctx)

	// 启动写入协程
	c.wg.Add(1)
	go c.writePump(ctx)

	// 启动心跳协程
	c.wg.Add(1)
	go c.pingPump(ctx)

	return nil
}

// Close 关闭客户�?
func (c *WorkerWSClient) Close() {
	c.closeOnce.Do(func() {
		close(c.closeChan)

		c.connMu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil // 确保设为 nil，防止其�?goroutine 使用已关闭的连接
		}
		c.connMu.Unlock()

		c.connected.Store(false)
		c.authenticated.Store(false)
	})

	// 等待所有协程退�?
	c.wg.Wait()
}

// ==================== Read Pause Control ====================

// pauseRead 暂停 readPump，返回恢复函数
// 重连时调用，确保 readPump 不会与 authenticate() 竞争读同一连接
func (c *WorkerWSClient) pauseRead() (unpause func()) {
	c.readPauseMu.Lock()
	ch := make(chan struct{})
	c.readPaused = ch
	c.readPauseMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.readPauseMu.Lock()
			c.readPaused = nil
			c.readPauseMu.Unlock()
			close(ch)
		})
	}
}

// waitIfPaused 如果 readPump 被暂停则阻塞，直到恢复或 context/closeChan 关闭
func (c *WorkerWSClient) waitIfPaused(ctx context.Context) bool {
	c.readPauseMu.Lock()
	ch := c.readPaused
	c.readPauseMu.Unlock()

	if ch == nil {
		return true // 未暂停，继续
	}

	select {
	case <-ch:
		return true // 已恢复
	case <-ctx.Done():
		return false
	case <-c.closeChan:
		return false
	}
}

// ==================== Message Pumps ====================

// readPump 读取消息循环
func (c *WorkerWSClient) readPump(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closeChan:
			return
		default:
		}

		// 重连期间暂停读取，避免与 authenticate() 竞争读同一连接
		if !c.waitIfPaused(ctx) {
			return
		}

		// 使用读锁保护整个读取操作，防止竞态条�?
		c.connMu.RLock()
		conn := c.conn
		if conn == nil {
			c.connMu.RUnlock()
			// 连接断开，等待重�?
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 设置读取超时
		if err := conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout)); err != nil {
			c.connMu.RUnlock()
			// 连接可能已关闭，触发重连
			if !c.handleReadError(ctx, err) {
				return
			}
			continue
		}

		data, _, err := wsutil.ReadServerData(conn)
		c.connMu.RUnlock()

		if err != nil {
			if !c.handleReadError(ctx, err) {
				return
			}
			continue
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			logx.Infof("[WSClient] Invalid message: %v", err)
			continue
		}

		// 路由消息
		c.handleMessage(&msg)
	}
}

// initiateReconnect 统一的重连触发方法
// 参数:
//   - waitIfBusy: 当已有重连在进行时，是否等待其完成（handleReadError 场景需要）
//   - source: 触发来源，用于日志区分（如 "readError", "writePump", "pingPump"）
//   - waitCtx: 等待重连完成时监听的 context（仅 waitIfBusy=true 时有效）
//
// 返回值:
//   - true: 成功触发重连或已有重连在进行中
//   - false: 客户端已关闭或 waitCtx 已取消，不应继续操作
func (c *WorkerWSClient) initiateReconnect(waitIfBusy bool, source string, waitCtx ...context.Context) bool {
	// 检查客户端是否已关闭
	select {
	case <-c.closeChan:
		return false
	default:
	}

	// 修复 M6：将 "检查重连状态 + CAS + 关闭旧连接" 放入同一 connMu 临界区
	// 原实现先 Close 再 CAS，并发调用时多方都会 Close 同一 conn，虽然 net.Conn.Close 通常幂等
	// 但不保证；且 c.conn=nil 后仍调用 Close 可能引入空指针风险（虽此处已判断）
	var waitCtxVal context.Context
	if len(waitCtx) > 0 {
		waitCtxVal = waitCtx[0]
	}

	c.connMu.Lock()
	if c.reconnecting.Load() {
		// 已有重连在进行中
		c.connMu.Unlock()

		if !waitIfBusy {
			return true
		}
		// 等待重连完成
		for c.reconnecting.Load() {
			select {
			case <-c.closeChan:
				return false
			case <-waitCtxVal.Done():
				return false
			case <-time.After(100 * time.Millisecond):
				// 继续等待
			}
		}
		return true
	}

	// 占用重连槽位
	c.reconnecting.Store(true)
	// 关闭旧连接（仅由当前 goroutine 执行，避免双重 Close）
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()

	// 更新状态标志
	c.connected.Store(false)
	c.authenticated.Store(false)

	// 启动异步重连（使用独立 context，避免继承已取消的父 context）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logx.Errorf("[WSClient] reconnect goroutine panic recovered: %v", r)
			}
		}()
		defer c.reconnecting.Store(false)

		reconnectCtx, reconnectCancel := context.WithCancel(context.Background())
		defer reconnectCancel()

		// 监听关闭信号
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logx.Errorf("[WSClient] close watcher goroutine panic recovered: %v", r)
				}
			}()
			select {
			case <-c.closeChan:
				reconnectCancel()
			case <-reconnectCtx.Done():
			}
		}()

		// 等待一小段时间再重连，避免立即重连
		select {
		case <-reconnectCtx.Done():
			return
		case <-time.After(time.Second):
		}

		if err := c.connectWithRetry(reconnectCtx, true); err != nil {
			logx.Infof("[WSClient] Reconnect from %s failed: %v", source, err)
		}
	}()

	return true
}

// handleReadError 处理读取错误，返回是否应继续运行
func (c *WorkerWSClient) handleReadError(ctx context.Context, err error) bool {
	logx.Infof("[WSClient] Read error: %v", err)
	return c.initiateReconnect(true, "readError", ctx)
}

// triggerReconnect 主动触发重连（供 writePump 等调用）
func (c *WorkerWSClient) triggerReconnect() {
	c.initiateReconnect(false, "writePump")
}

// writePump 写入消息循环
func (c *WorkerWSClient) writePump(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closeChan:
			return
		case data := <-c.sendChan:
			if !c.IsConnected() {
				// 未连接，丢弃消息
				continue
			}

			// 使用写锁序列化 WebSocket 写操作，防止与 sendPingDirect 并发写
			c.writeMu.Lock()
			c.connMu.RLock()
			conn := c.conn
			if conn == nil {
				c.connMu.RUnlock()
				c.writeMu.Unlock()
				continue
			}

			if err := conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout)); err != nil {
				c.connMu.RUnlock()
				c.writeMu.Unlock()
				logx.Infof("[WSClient] SetWriteDeadline error: %v", err)
				c.triggerReconnect()
				continue
			}

			err := wsutil.WriteClientMessage(conn, ws.OpText, data)
			c.connMu.RUnlock()
			c.writeMu.Unlock()

			if err != nil {
				logx.Infof("[WSClient] Write error: %v", err)
				c.triggerReconnect()
			}
		}
	}
}

// pingPump 心跳循环
func (c *WorkerWSClient) pingPump(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	// 心跳超时阈值：3倍心跳间隔（低配机器 CPU 过载时需要更长容忍时间）
	heartbeatTimeout := c.config.PingInterval * 3

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closeChan:
			return
		case <-ticker.C:
			if !c.IsConnected() {
				continue
			}

			// 检查是否超时未收到PONG
			c.pongMu.RLock()
			lastPong := c.lastPong
			c.pongMu.RUnlock()

			if time.Since(lastPong) > heartbeatTimeout {
				logx.Infof("[WSClient] Heartbeat timeout (no PONG for %v), triggering reconnect...", time.Since(lastPong))
				c.initiateReconnect(false, "pingPump")
				continue
			}

			// 发送PING（绕过 sendChan，避免日志背压导致 PING 丢失引发误重连）
			if err := c.sendPingDirect(); err != nil {
				logx.Infof("[WSClient] PING send failed: %v, triggering reconnect...", err)
				c.initiateReconnect(false, "pingPump")
			}
		}
	}
}

// ==================== Message Handling ====================

// handleMessage 处理接收到的消息
func (c *WorkerWSClient) handleMessage(msg *WSMessage) {
	switch msg.Type {
	case WSTypeAuthOK:
		// 认证成功（重连后可能收到�?
		c.authenticated.Store(true)
		logx.Info("[WSClient] Authentication successful (reconnected)")

	case WSTypeAuthFail:
		// 认证失败
		c.authenticated.Store(false)
		var reason struct {
			Reason string `json:"reason"`
		}
		json.Unmarshal(msg.Payload, &reason)
		logx.Infof("[WSClient] Authentication failed: %s", reason.Reason)

	case WSTypePing:
		// 收到服务器PING，回复PONG
		c.sendMessage(&WSMessage{Type: WSTypePong})
		c.pongMu.Lock()
		c.lastPong = time.Now()
		c.pongMu.Unlock()

	case WSTypePong:
		// 收到服务器PONG
		c.pongMu.Lock()
		c.lastPong = time.Now()
		c.pongMu.Unlock()

	case WSTypeControl:
		// 收到控制信号
		c.handleControl(msg.Payload)

	default:
		logx.Infof("[WSClient] Unknown message type: %s", msg.Type)
	}
}

// handleControl 处理控制信号
func (c *WorkerWSClient) handleControl(payload json.RawMessage) {
	// 先尝试解析为 Worker 级别控制命令
	var workerControl struct {
		Action      string `json:"action"`
		NewName     string `json:"newName,omitempty"`
		Concurrency int    `json:"concurrency,omitempty"`
	}
	if err := json.Unmarshal(payload, &workerControl); err == nil {
		logx.Infof("[WSClient] Parsed control action: '%s'", workerControl.Action)

		// 检查是否是 Worker 级别控制命令
		isWorkerControl := false
		switch workerControl.Action {
		case "WORKER_STOP":
			logx.Info("[WSClient] Executing WORKER_STOP command...")
			if c.workerControlHandler != nil {
				c.workerControlHandler("stop", "")
			} else {
				logx.Info("[WSClient] ERROR: workerControlHandler is nil!")
			}
			isWorkerControl = true
		case "WORKER_RESTART":
			logx.Info("[WSClient] Executing WORKER_RESTART command...")
			if c.workerControlHandler != nil {
				c.workerControlHandler("restart", "")
			} else {
				logx.Info("[WSClient] ERROR: workerControlHandler is nil!")
			}
			isWorkerControl = true
		case "WORKER_RENAME":
			logx.Infof("[WSClient] Executing WORKER_RENAME command, new name: %s", workerControl.NewName)
			if c.workerControlHandler != nil {
				c.workerControlHandler("rename", workerControl.NewName)
			}
			isWorkerControl = true
		case "WORKER_SET_CONCURRENCY":
			logx.Infof("[WSClient] Executing WORKER_SET_CONCURRENCY command, concurrency: %d", workerControl.Concurrency)
			if c.workerControlHandler != nil {
				c.workerControlHandler("setConcurrency", fmt.Sprintf("%d", workerControl.Concurrency))
			}
			isWorkerControl = true
		}

		if isWorkerControl {
			return
		}
	}

	// Task-level controls are strict JSON and must carry the exact generation.
	envelope, err := scheduler.ParseTaskControlEnvelope(payload)
	if err != nil {
		logx.Infof("[WSClient] Rejected invalid task control payload: %v", err)
		return
	}

	logx.Infof("[WSClient] Received task control signal: taskId=%s generation=%s action=%s",
		envelope.TaskID, envelope.DispatchGeneration, envelope.Action)
	if c.controlHandler != nil {
		c.controlHandler(envelope)
	}
}

// ==================== Send Methods ====================

// sendMessage 发送消息（内部方法�?
func (c *WorkerWSClient) sendMessage(msg *WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case c.sendChan <- data:
		return nil
	case <-c.closeChan:
		return fmt.Errorf("client closed")
	default:
		return fmt.Errorf("send buffer full")
	}
}

// sendPingDirect 直接写入 PING JSON 消息，绕过 sendChan
// 修复 #18：日志堵住 sendChan 时 PING 会被丢弃，导致心跳超时误触发重连
func (c *WorkerWSClient) sendPingDirect() error {
	data, err := json.Marshal(&WSMessage{Type: WSTypePing})
	if err != nil {
		return err
	}
	// 使用写锁序列化 WebSocket 写操作，防止与 writePump 并发写
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	c.connMu.RLock()
	conn := c.conn
	if conn == nil {
		c.connMu.RUnlock()
		return fmt.Errorf("not connected")
	}
	// 必须设置写超时，否则会复用 writePump 留下的已过期 deadline，
	// 导致写入立即返回 i/o timeout（连接实际正常）
	if err := conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout)); err != nil {
		c.connMu.RUnlock()
		return fmt.Errorf("set write deadline: %w", err)
	}
	err = wsutil.WriteClientMessage(conn, ws.OpText, data)
	c.connMu.RUnlock()
	return err
}
