package worker

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"cscan/internal/scheduler"
)

func (w *Worker) keepAliveLoop() {
	// 启动时立即发送一次心跳
	w.sendHeartbeat()

	const (
		normalInterval   = 30 * time.Second // 正常心跳间隔
		circuitInterval  = 60 * time.Second // 熔断期间心跳间隔
		circuitThreshold = 5                // 连续失败多少次进入熔断
	)

	ticker := time.NewTicker(normalInterval)
	defer ticker.Stop()

	consecutiveFailures := 0
	inCircuit := false

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			if err := w.sendHeartbeatWithRetry(); err != nil {
				consecutiveFailures++

				// 进入熔断状态
				if !inCircuit && consecutiveFailures >= circuitThreshold {
					inCircuit = true
					ticker.Reset(circuitInterval)
					w.logger.Warn("Heartbeat circuit breaker OPEN after %d failures, interval increased to %v", consecutiveFailures, circuitInterval)
				}
			} else {
				// 退出熔断状态
				if inCircuit {
					inCircuit = false
					ticker.Reset(normalInterval)
					w.logger.Info("Heartbeat circuit breaker CLOSED, recovered after %d failures", consecutiveFailures)
				}
				consecutiveFailures = 0
			}
		}
	}
}

// sendHeartbeatWithRetry 带重试的心跳发送
func (w *Worker) sendHeartbeatWithRetry() error {
	var lastErr error
	for i := 0; i < 2; i++ { // 最多重试 1 次
		if i > 0 {
			time.Sleep(2 * time.Second)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := w.doSendHeartbeat(ctx)
		cancel()

		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// doSendHeartbeat 执行心跳发送
func (w *Worker) doSendHeartbeat(ctx context.Context) error {
	cpuLoad := GetCPULoad()
	memUsed := GetMemoryUsage()

	if cpuLoad < 0 || cpuLoad > 100 {
		cpuLoad = 0.0
	}
	if memUsed < 0 || memUsed > 100 {
		memUsed = 0.0
	}

	// 修复 #27：读取 concurrency 时加 RLock，避免与 applyConcurrency 写入竞争
	w.mu.RLock()
	concurrency := w.config.Concurrency
	w.mu.RUnlock()

	// 优先使用 Redis 直连
	if w.schedClient != nil {
		resp, err := w.schedClient.KeepAliveWithResponse(ctx, cpuLoad, memUsed,
			w.taskStarted, w.taskExecuted, concurrency)
		if err != nil {
			return err
		}

		if resp.ManualStopFlag {
			w.logger.Info("received stop signal, stopping worker...")
			w.safeGo("heartbeat-stop", func() {
				w.drainAndExit(60*time.Second, func() { os.Exit(0) })
			})
		} else if resp.ManualReloadFlag {
			w.logger.Info("received reload/restart signal, restarting worker...")
			w.safeGo("heartbeat-restart", func() {
				w.drainAndExit(60*time.Second, w.restartSelf)
			})
		}

		if resp.DesiredConcurrency > 0 {
			w.applyConcurrency(resp.DesiredConcurrency)
		}
		return nil
	}

	// 回退到 HTTP
	resp, err := w.httpClient.Heartbeat(ctx, &HeartbeatReq{
		WorkerName:         w.workerName,
		InstanceID:         w.instanceID,
		TaskProtocol:       w.taskProtocol,
		IP:                 w.config.IP,
		CpuLoad:            cpuLoad,
		MemUsed:            memUsed,
		TaskStartedNumber:  int32(w.taskStarted),
		TaskExecutedNumber: int32(w.taskExecuted),
		IsDaemon:           false,
		Concurrency:        concurrency,
	})
	if err != nil {
		return err
	}

	if resp.ManualStopFlag {
		w.logger.Info("received stop signal, stopping worker...")
		w.safeGo("heartbeat-stop-http", func() {
			w.drainAndExit(60*time.Second, func() { os.Exit(0) })
		})
	} else if resp.ManualReloadFlag {
		w.logger.Info("received reload/restart signal, restarting worker...")
		w.safeGo("heartbeat-restart-http", func() {
			w.drainAndExit(60*time.Second, w.restartSelf)
		})
	}

	if resp.DesiredConcurrency > 0 {
		w.applyConcurrency(resp.DesiredConcurrency)
	}

	return nil
}

// sendHeartbeat 发送心跳（简单包装，用于外部调用）
func (w *Worker) sendHeartbeat() {
	if err := w.sendHeartbeatWithRetry(); err != nil {
		w.logger.Warn("sendHeartbeat failed: %v", err)
	}
}

// maintainDrainHeartbeat keeps this exact instance alive after acquisition
// loops stop and until every task goroutine has returned.
func (w *Worker) maintainDrainHeartbeat(stop <-chan struct{}) {
	refresh := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cpuLoad := GetCPULoad()
		memUsed := GetMemoryUsage()
		if cpuLoad < 0 || cpuLoad > 100 {
			cpuLoad = 0
		}
		if memUsed < 0 || memUsed > 100 {
			memUsed = 0
		}
		w.mu.RLock()
		concurrency := w.config.Concurrency
		taskStarted := w.taskStarted
		taskExecuted := w.taskExecuted
		w.mu.RUnlock()

		var err error
		if w.schedClient != nil {
			err = w.schedClient.KeepAlive(ctx, cpuLoad, memUsed, taskStarted, taskExecuted, concurrency)
		} else if w.httpClient != nil {
			_, err = w.httpClient.Heartbeat(ctx, &HeartbeatReq{
				WorkerName:         w.workerName,
				InstanceID:         w.instanceID,
				TaskProtocol:       w.taskProtocol,
				IP:                 w.config.IP,
				CpuLoad:            cpuLoad,
				MemUsed:            memUsed,
				TaskStartedNumber:  int32(taskStarted),
				TaskExecutedNumber: int32(taskExecuted),
				Concurrency:        concurrency,
			})
		}
		if err != nil {
			w.logger.Warn("drain heartbeat failed: %v", err)
		}
	}

	refresh()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			refresh()
		}
	}
}

// controlPollingLoop consumes strict controls from Pub/Sub when available and
// always repairs missed delivery by reading durable exact-generation keys for
// every locally owned acquisition, including tasks still waiting in the queue.
func (w *Worker) controlPollingLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var signalCh <-chan *scheduler.TaskControlEnvelope
	if w.schedClient != nil {
		signalCh = w.schedClient.SubscribeTaskControls(ctx)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	pollDurable := func() {
		targets := w.getOwnedTaskTargets()
		if len(targets) == 0 {
			return
		}
		lookupCtx, lookupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer lookupCancel()
		if w.schedClient != nil {
			signals, err := w.schedClient.GetTaskControlSignals(lookupCtx, targets)
			if err != nil {
				return
			}
			for index := range signals {
				w.handleControlSignal(&signals[index])
			}
			return
		}
		if w.httpClient == nil {
			return
		}
		resp, err := w.httpClient.GetTaskControlSignals(lookupCtx, targets)
		if err != nil {
			return
		}
		for index := range resp.Signals {
			w.handleControlSignal(&resp.Signals[index])
		}
	}

	for {
		select {
		case <-w.stopChan:
			return
		case <-ctx.Done():
			return
		case envelope, ok := <-signalCh:
			if !ok {
				signalCh = nil
				continue
			}
			w.handleControlSignal(envelope)
		case <-ticker.C:
			pollDurable()
		}
	}
}

// GetWorkerName 获取Worker名称
func GetWorkerName() string {
	hostname, _ := os.Hostname()
	// 使用 hostname + pid + 随机后缀，确保唯一性
	return fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), randomSuffix(4))
}

// randomSuffix 生成随机后缀
func randomSuffix(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}

// GetLocalIP 获取本机IP地址
func GetLocalIP() string {
	// 1. 优先使用环境变量 WORKER_IP（适用于 Docker 等容器环境）
	if ip := os.Getenv("WORKER_IP"); ip != "" {
		return ip
	}

	// 2. 尝试通过 UDP 连接获取出口 IP（更可靠的方式）
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if localAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return localAddr.IP.String()
		}
	}

	// 3. 回退到遍历网络接口
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
