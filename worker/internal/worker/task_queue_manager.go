package worker

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cscan/internal/scheduler"
)

// TaskPriority 任务优先级
type TaskPriority int

const (
	PriorityLow    TaskPriority = 1
	PriorityNormal TaskPriority = 2
	PriorityHigh   TaskPriority = 3
	PriorityUrgent TaskPriority = 4
)

// TaskQueueItem 任务队列项
type TaskQueueItem struct {
	Task     *scheduler.TaskInfo
	Priority TaskPriority
	AddTime  time.Time
}

// TaskQueueManager 任务队列管理器
// 实现优先级队列，防止任务堆积导致内存溢出
type TaskQueueManager struct {
	// mu 改为 sync.Mutex 以便 sync.Cond 绑定(GetStats/Size 等读路径使用同一锁,RLock 取消)
	// 内部读多写少场景下 Mutex 与 RWMutex 开销可忽略,而 cond 必须绑定同一锁
	mu sync.Mutex

	// 队列配置
	maxQueueSize int           // 最大队列长度
	maxWaitTime  time.Duration // 任务最大等待时间

	// 优先级队列
	queues map[TaskPriority][]*TaskQueueItem

	// 统计信息
	totalEnqueued int64 // 总入队数
	totalDequeued int64 // 总出队数
	totalDropped  int64 // 总丢弃数
	totalExpired  int64 // 总过期数
	currentSize   int32 // 当前队列大小

	// 控制
	stopChan chan struct{}
	stopOnce sync.Once

	// 修复 H5:用 sync.Cond 替代 worker 侧 50ms 空轮询
	// cond 守护同一把 mu;Enqueue 后 Signal 唤醒等待者,Stop 时 Broadcast 解除所有 DequeueWait
	cond *sync.Cond

	// 日志回调
	logger func(level, format string, args ...interface{})
}

// NewTaskQueueManager 创建任务队列管理器
func NewTaskQueueManager(maxQueueSize int, maxWaitTime time.Duration) *TaskQueueManager {
	if maxQueueSize <= 0 {
		maxQueueSize = 100 // 默认最大100个任务
	}
	if maxWaitTime <= 0 {
		maxWaitTime = 5 * time.Minute // 默认最大等待5分钟
	}

	m := &TaskQueueManager{
		maxQueueSize: maxQueueSize,
		maxWaitTime:  maxWaitTime,
		queues: map[TaskPriority][]*TaskQueueItem{
			PriorityUrgent: make([]*TaskQueueItem, 0),
			PriorityHigh:   make([]*TaskQueueItem, 0),
			PriorityNormal: make([]*TaskQueueItem, 0),
			PriorityLow:    make([]*TaskQueueItem, 0),
		},
		stopChan: make(chan struct{}),
	}
	// sync.Cond 绑定到 m.mu,保证 Enqueue 的 Signal 与 DequeueWait 的 Wait/Lock 互斥可见
	m.cond = sync.NewCond(&m.mu)
	return m
}

// SetLogger 设置日志回调
func (m *TaskQueueManager) SetLogger(logger func(level, format string, args ...interface{})) {
	m.logger = logger
}

func (m *TaskQueueManager) log(level, format string, args ...interface{}) {
	if m.logger != nil {
		m.logger(level, format, args...)
	}
}

// Start 启动队列管理器
func (m *TaskQueueManager) Start(ctx context.Context) {
	go m.cleanupLoop(ctx)
}

// Stop 停止队列管理器
// 修复 C-11：原直接 close(stopChan)，重复调用会 panic（close of closed channel）
// 现使用 sync.Once 保证只关闭一次
// 修复 H5：Stop 同时 Broadcast cond,解除所有 DequeueWait 的阻塞,使其返回 nil 让 worker 退出
func (m *TaskQueueManager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopChan)
		// 唤醒所有 DequeueWait 等待者,避免 worker 永久阻塞在 cond.Wait
		m.mu.Lock()
		m.cond.Broadcast()
		m.mu.Unlock()
	})
}

// cleanupLoop 清理过期任务循环
func (m *TaskQueueManager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // 每30秒清理一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.cleanupExpiredTasks()
		}
	}
}

// cleanupExpiredTasks 清理过期任务
func (m *TaskQueueManager) cleanupExpiredTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for priority, queue := range m.queues {
		newQueue := make([]*TaskQueueItem, 0, len(queue))
		for _, item := range queue {
			if now.Sub(item.AddTime) > m.maxWaitTime {
				expiredCount++
				atomic.AddInt64(&m.totalExpired, 1)
			} else {
				newQueue = append(newQueue, item)
			}
		}
		m.queues[priority] = newQueue
	}

	if expiredCount > 0 {
		atomic.AddInt32(&m.currentSize, int32(-expiredCount))
		m.log("INFO", "Cleaned up %d expired tasks from queue", expiredCount)
	}
}

// Enqueue 入队任务
func (m *TaskQueueManager) Enqueue(task *scheduler.TaskInfo, priority TaskPriority) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查队列是否已满
	currentSize := int(atomic.LoadInt32(&m.currentSize))
	if currentSize >= m.maxQueueSize {
		// 队列已满，尝试丢弃低优先级任务
		if !m.dropLowPriorityTaskLocked() {
			atomic.AddInt64(&m.totalDropped, 1)
			m.log("ERROR", "[CRITICAL] Task queue full (size=%d), DROPPING task %s - task will be LOST", currentSize, task.TaskId)
			return false
		}
	}

	// 创建队列项
	item := &TaskQueueItem{
		Task:     task,
		Priority: priority,
		AddTime:  time.Now(),
	}

	// 添加到对应优先级队列
	m.queues[priority] = append(m.queues[priority], item)

	atomic.AddInt32(&m.currentSize, 1)
	atomic.AddInt64(&m.totalEnqueued, 1)

	// 修复 H5：唤醒一个等待 DequeueWait 的 worker,替代 worker 侧 50ms 空轮询
	m.cond.Signal()

	return true
}

// Dequeue 出队任务（按优先级）
// 返回任务及入队时确定的优先级，避免出队时重新解析 Config 导致 metric label 不一致
func (m *TaskQueueManager) Dequeue() (*scheduler.TaskInfo, TaskPriority) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 按优先级顺序检查队列
	priorities := []TaskPriority{PriorityUrgent, PriorityHigh, PriorityNormal, PriorityLow}

	for _, priority := range priorities {
		queue := m.queues[priority]
		if len(queue) > 0 {
			// 取出第一个任务
			item := queue[0]
			m.queues[priority] = queue[1:]

			atomic.AddInt32(&m.currentSize, -1)
			atomic.AddInt64(&m.totalDequeued, 1)

			return item.Task, item.Priority
		}
	}

	return nil, PriorityNormal // 队列为空
}

// DequeueWait 阻塞出队,直到有任务、Stop 被调用或 ctx 取消
// 修复 H5：用 sync.Cond 替代 worker 侧 50ms 空轮询,空队列时挂起等待 Signal
// 返回 (task, priority, ok)，ok=false 表示因 Stop 或 ctx 取消而退出,调用方应结束循环
//
// sync.Cond 本身不感知 context,这里启动一个协程在 ctx.Done 时 Broadcast 解除等待
func (m *TaskQueueManager) DequeueWait(ctx context.Context) (*scheduler.TaskInfo, TaskPriority, bool) {
	// ctx 已取消则直接退出,避免启动无谓的唤醒协程
	if err := ctx.Err(); err != nil {
		return nil, PriorityNormal, false
	}

	// 启动 ctx-cancelled 唤醒协程。m.mu 锁住后再 Broadcast,确保 DequeueWait 在 Wait 中被唤醒
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.cond.Broadcast()
			m.mu.Unlock()
		case <-ctxDone:
		}
	}()
	defer close(ctxDone)

	m.mu.Lock()
	defer m.mu.Unlock()

	priorities := []TaskPriority{PriorityUrgent, PriorityHigh, PriorityNormal, PriorityLow}
	for {
		// 退出条件 1: Stop 被调用(stopChan 已关闭)
		select {
		case <-m.stopChan:
			return nil, PriorityNormal, false
		default:
		}
		// 退出条件 2: ctx 已取消
		if err := ctx.Err(); err != nil {
			return nil, PriorityNormal, false
		}

		// 尝试按优先级出队
		for _, priority := range priorities {
			queue := m.queues[priority]
			if len(queue) > 0 {
				item := queue[0]
				m.queues[priority] = queue[1:]

				atomic.AddInt32(&m.currentSize, -1)
				atomic.AddInt64(&m.totalDequeued, 1)

				return item.Task, item.Priority, true
			}
		}

		// 队列空,挂起等待 Enqueue 的 Signal 或 Stop/ctx 的 Broadcast
		m.cond.Wait()
	}
}

// dropLowPriorityTaskLocked 丢弃低优先级任务（需要持有锁）
// 修复 #26：Urgent 任务永不丢弃，避免低优先级任务挤占 Urgent 槽位
func (m *TaskQueueManager) dropLowPriorityTaskLocked() bool {
	// 按优先级从低到高尝试丢弃，Urgent 除外
	priorities := []TaskPriority{PriorityLow, PriorityNormal, PriorityHigh}

	for _, priority := range priorities {
		queue := m.queues[priority]
		if len(queue) > 0 {
			// 丢弃最后一个任务（最新的）
			m.queues[priority] = queue[:len(queue)-1]
			atomic.AddInt32(&m.currentSize, -1)
			atomic.AddInt64(&m.totalDropped, 1)
			m.log("WARN", "Dropped low priority task to make room")
			return true
		}
	}

	return false
}

// Size 获取当前队列大小
func (m *TaskQueueManager) Size() int {
	return int(atomic.LoadInt32(&m.currentSize))
}

// IsFull 检查队列是否已满
func (m *TaskQueueManager) IsFull() bool {
	return m.Size() >= m.maxQueueSize
}

// IsEmpty 检查队列是否为空
func (m *TaskQueueManager) IsEmpty() bool {
	return m.Size() == 0
}

// GetStats 获取队列统计信息
func (m *TaskQueueManager) GetStats() TaskQueueStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	queueSizes := make(map[string]int)
	for priority, queue := range m.queues {
		var priorityName string
		switch priority {
		case PriorityUrgent:
			priorityName = "urgent"
		case PriorityHigh:
			priorityName = "high"
		case PriorityNormal:
			priorityName = "normal"
		case PriorityLow:
			priorityName = "low"
		}
		queueSizes[priorityName] = len(queue)
	}

	return TaskQueueStats{
		MaxQueueSize:  m.maxQueueSize,
		CurrentSize:   int(atomic.LoadInt32(&m.currentSize)),
		TotalEnqueued: atomic.LoadInt64(&m.totalEnqueued),
		TotalDequeued: atomic.LoadInt64(&m.totalDequeued),
		TotalDropped:  atomic.LoadInt64(&m.totalDropped),
		TotalExpired:  atomic.LoadInt64(&m.totalExpired),
		QueueSizes:    queueSizes,
		MaxWaitTime:   m.maxWaitTime,
	}
}

// TaskQueueStats 任务队列统计
type TaskQueueStats struct {
	MaxQueueSize  int            `json:"maxQueueSize"`
	CurrentSize   int            `json:"currentSize"`
	TotalEnqueued int64          `json:"totalEnqueued"`
	TotalDequeued int64          `json:"totalDequeued"`
	TotalDropped  int64          `json:"totalDropped"`
	TotalExpired  int64          `json:"totalExpired"`
	QueueSizes    map[string]int `json:"queueSizes"`
	MaxWaitTime   time.Duration  `json:"maxWaitTime"`
}

// Clear 清空队列
func (m *TaskQueueManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for priority := range m.queues {
		m.queues[priority] = make([]*TaskQueueItem, 0)
	}

	atomic.StoreInt32(&m.currentSize, 0)
	m.log("INFO", "Task queue cleared")
}

// GetTaskPriority 根据任务配置确定优先级
func GetTaskPriority(task *scheduler.TaskInfo) TaskPriority {
	// 解析任务配置
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		return PriorityNormal
	}

	// 检查任务类型
	taskType, _ := taskConfig["taskType"].(string)
	switch taskType {
	case "poc_validate", "poc_batch_validate", "fingerprint_validate", "active_fingerprint_validate":
		return PriorityHigh // POC验证、指纹验证任务优先级较高
	}

	// 检查是否是紧急任务
	if urgent, ok := taskConfig["urgent"].(bool); ok && urgent {
		return PriorityUrgent
	}

	// 检查优先级配置
	if priority, ok := taskConfig["priority"].(string); ok {
		switch priority {
		case "urgent":
			return PriorityUrgent
		case "high":
			return PriorityHigh
		case "low":
			return PriorityLow
		}
	}

	// 根据目标数量确定优先级
	if target, ok := taskConfig["target"].(string); ok {
		targetCount := len(strings.Split(strings.TrimSpace(target), "\n"))
		if targetCount <= 10 {
			return PriorityHigh // 小批量任务优先级较高
		} else if targetCount >= 1000 {
			return PriorityLow // 大批量任务优先级较低
		}
	}

	return PriorityNormal
}
