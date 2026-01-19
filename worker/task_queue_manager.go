package worker

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cscan/scheduler"
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
	mu sync.RWMutex
	
	// 队列配置
	maxQueueSize    int           // 最大队列长度
	maxWaitTime     time.Duration // 任务最大等待时间
	
	// 优先级队列
	queues map[TaskPriority][]*TaskQueueItem
	
	// 统计信息
	totalEnqueued   int64 // 总入队数
	totalDequeued   int64 // 总出队数
	totalDropped    int64 // 总丢弃数
	totalExpired    int64 // 总过期数
	currentSize     int32 // 当前队列大小
	
	// 控制
	stopChan chan struct{}
	
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
	
	return &TaskQueueManager{
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
func (m *TaskQueueManager) Stop() {
	close(m.stopChan)
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
			m.log("WARN", "Task queue full, dropping task %s", task.TaskId)
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
	
	return true
}

// Dequeue 出队任务（按优先级）
func (m *TaskQueueManager) Dequeue() *scheduler.TaskInfo {
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
			
			return item.Task
		}
	}
	
	return nil // 队列为空
}

// dropLowPriorityTaskLocked 丢弃低优先级任务（需要持有锁）
func (m *TaskQueueManager) dropLowPriorityTaskLocked() bool {
	// 按优先级从低到高尝试丢弃任务
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	
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
		MaxQueueSize:    m.maxQueueSize,
		CurrentSize:     int(atomic.LoadInt32(&m.currentSize)),
		TotalEnqueued:   atomic.LoadInt64(&m.totalEnqueued),
		TotalDequeued:   atomic.LoadInt64(&m.totalDequeued),
		TotalDropped:    atomic.LoadInt64(&m.totalDropped),
		TotalExpired:    atomic.LoadInt64(&m.totalExpired),
		QueueSizes:      queueSizes,
		MaxWaitTime:     m.maxWaitTime,
	}
}

// TaskQueueStats 任务队列统计
type TaskQueueStats struct {
	MaxQueueSize    int                `json:"maxQueueSize"`
	CurrentSize     int                `json:"currentSize"`
	TotalEnqueued   int64              `json:"totalEnqueued"`
	TotalDequeued   int64              `json:"totalDequeued"`
	TotalDropped    int64              `json:"totalDropped"`
	TotalExpired    int64              `json:"totalExpired"`
	QueueSizes      map[string]int     `json:"queueSizes"`
	MaxWaitTime     time.Duration      `json:"maxWaitTime"`
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
	case "poc_validate", "poc_batch_validate":
		return PriorityHigh // POC验证任务优先级较高
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