package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

// CronTask 定时任务
type CronTask struct {
	Id           string       `json:"id"`
	Name         string       `json:"name"`
	ScheduleType string       `json:"scheduleType"` // cron: Cron表达式, once: 指定时间执行一次
	CronSpec     string       `json:"cronSpec"`     // Cron表达式 (scheduleType=cron时使用)
	ScheduleTime string       `json:"scheduleTime"` // 指定执行时间 (scheduleType=once时使用)
	WorkspaceId  string       `json:"workspaceId"`
	MainTaskId   string       `json:"mainTaskId"` // 关联的任务ID
	TaskName     string       `json:"taskName"`   // 关联的任务名称
	Target       string       `json:"target"`     // 扫描目标（从任务复制）
	Config       string       `json:"config"`     // 任务配置（从任务复制）
	Status       string       `json:"status"`     // enable/disable
	LastRunTime  string       `json:"lastRunTime"`
	NextRunTime  string       `json:"nextRunTime"`
	EntryId      cron.EntryID `json:"-"`
}

// CronManager 定时任务管理器
type CronManager struct {
	scheduler *Scheduler
	rdb       *redis.Client
	tasks     map[string]*CronTask
	cronKey   string
}

// NewCronManager 创建定时任务管理器
func NewCronManager(scheduler *Scheduler, rdb *redis.Client) *CronManager {
	return &CronManager{
		scheduler: scheduler,
		rdb:       rdb,
		tasks:     make(map[string]*CronTask),
		cronKey:   "cscan:cron:tasks",
	}
}

// LoadTasks 从Redis加载定时任务
func (m *CronManager) LoadTasks(ctx context.Context) error {
	data, err := m.rdb.HGetAll(ctx, m.cronKey).Result()
	if err != nil {
		return err
	}

	for id, taskData := range data {
		var task CronTask
		if err := json.Unmarshal([]byte(taskData), &task); err != nil {
			continue
		}
		task.Id = id
		if task.Status == "enable" {
			m.startTask(&task)
		}
		m.tasks[id] = &task
	}

	return nil
}

// AddTask 添加定时任务
func (m *CronManager) AddTask(ctx context.Context, task *CronTask) error {
	// 验证cron表达式
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(task.CronSpec)
	if err != nil {
		return fmt.Errorf("invalid cron spec: %v", err)
	}

	task.NextRunTime = schedule.Next(time.Now()).Local().Format("2006-01-02 15:04:05")
	task.Status = "enable"

	// 保存到Redis
	data, _ := json.Marshal(task)
	if err := m.rdb.HSet(ctx, m.cronKey, task.Id, data).Err(); err != nil {
		return err
	}

	// 启动任务
	m.startTask(task)
	m.tasks[task.Id] = task

	return nil
}

// RemoveTask 移除定时任务
func (m *CronManager) RemoveTask(ctx context.Context, taskId string) error {
	task, ok := m.tasks[taskId]
	if !ok {
		return fmt.Errorf("task not found: %s", taskId)
	}

	// 停止任务
	if task.EntryId > 0 {
		m.scheduler.RemoveCronTask(task.EntryId)
	}

	// 从Redis删除
	if err := m.rdb.HDel(ctx, m.cronKey, taskId).Err(); err != nil {
		return err
	}

	delete(m.tasks, taskId)
	return nil
}

// EnableTask 启用定时任务
func (m *CronManager) EnableTask(ctx context.Context, taskId string) error {
	task, ok := m.tasks[taskId]
	if !ok {
		return fmt.Errorf("task not found: %s", taskId)
	}

	if task.Status == "enable" {
		return nil
	}

	task.Status = "enable"
	m.startTask(task)

	// 更新Redis
	data, _ := json.Marshal(task)
	return m.rdb.HSet(ctx, m.cronKey, taskId, data).Err()
}

// DisableTask 禁用定时任务
func (m *CronManager) DisableTask(ctx context.Context, taskId string) error {
	task, ok := m.tasks[taskId]
	if !ok {
		return fmt.Errorf("task not found: %s", taskId)
	}

	if task.Status == "disable" {
		return nil
	}

	// 停止任务
	if task.EntryId > 0 {
		m.scheduler.RemoveCronTask(task.EntryId)
		task.EntryId = 0
	}

	task.Status = "disable"

	// 更新Redis
	data, _ := json.Marshal(task)
	return m.rdb.HSet(ctx, m.cronKey, taskId, data).Err()
}

// GetTasks 获取所有定时任务
func (m *CronManager) GetTasks() []*CronTask {
	tasks := make([]*CronTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// startTask 启动定时任务
func (m *CronManager) startTask(task *CronTask) {
	if task.ScheduleType == "once" {
		// 指定时间执行一次
		scheduleTime, err := time.ParseInLocation("2006-01-02 15:04:05", task.ScheduleTime, time.Local)
		if err != nil {
			return
		}

		// 如果时间已过，不启动
		if scheduleTime.Before(time.Now()) {
			return
		}

		// 使用定时器在指定时间执行
		duration := time.Until(scheduleTime)
		go func(t *CronTask) {
			timer := time.NewTimer(duration)
			<-timer.C
			// 检查任务是否仍然启用
			if currentTask, ok := m.tasks[t.Id]; ok && currentTask.Status == "enable" {
				m.executeTask(t)
			}
		}(task)
	} else {
		// Cron表达式
		if task.CronSpec == "" {
			return
		}
		entryId, err := m.scheduler.AddCronTask(task.CronSpec, func() {
			m.executeTask(task)
		})
		if err != nil {
			return
		}
		task.EntryId = entryId
	}
}

// executeTask 执行定时任务
func (m *CronManager) executeTask(task *CronTask) {
	ctx := context.Background()

	// 更新最后执行时间
	task.LastRunTime = time.Now().Local().Format("2006-01-02 15:04:05")

	// 计算下次执行时间
	switch task.ScheduleType {
	case "cron":
		parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		schedule, _ := parser.Parse(task.CronSpec)
		task.NextRunTime = schedule.Next(time.Now()).Local().Format("2006-01-02 15:04:05")
	case "once":
		// 一次性任务执行后禁用
		task.Status = "disable"
		task.NextRunTime = ""
	}

	// 增加运行次数
	runCountKey := fmt.Sprintf("cscan:cron:runcount:%s", task.Id)
	m.rdb.Incr(ctx, runCountKey)

	// 更新Redis
	data, _ := json.Marshal(task)
	m.rdb.HSet(ctx, m.cronKey, task.Id, data)

	// 发布消息通知 API 服务创建新任务
	// API 服务会创建新的 MainTask 记录并推送到队列
	cronExecData, _ := json.Marshal(map[string]interface{}{
		"cronTaskId":  task.Id,
		"workspaceId": task.WorkspaceId,
		"mainTaskId":  task.MainTaskId,
		"taskName":    task.Name,
		"target":      task.Target,
		"config":      task.Config,
	})
	m.rdb.Publish(ctx, "cscan:cron:execute", string(cronExecData))
}

// ReloadTask 重新加载单个任务
func (m *CronManager) ReloadTask(ctx context.Context, taskId string) error {
	// 先停止现有任务
	if existingTask, ok := m.tasks[taskId]; ok {
		if existingTask.EntryId > 0 {
			m.scheduler.RemoveCronTask(existingTask.EntryId)
		}
	}

	// 从Redis获取最新任务数据
	taskData, err := m.rdb.HGet(ctx, m.cronKey, taskId).Result()
	if err != nil {
		delete(m.tasks, taskId)
		return err
	}

	var task CronTask
	if err := json.Unmarshal([]byte(taskData), &task); err != nil {
		return err
	}
	task.Id = taskId

	// 如果启用则启动
	if task.Status == "enable" {
		m.startTask(&task)
	}
	m.tasks[taskId] = &task

	return nil
}

// RunTaskNow 立即执行任务
func (m *CronManager) RunTaskNow(ctx context.Context, taskId string) error {
	task, ok := m.tasks[taskId]
	if !ok {
		// 尝试从Redis加载
		taskData, err := m.rdb.HGet(ctx, m.cronKey, taskId).Result()
		if err != nil {
			return fmt.Errorf("task not found: %s", taskId)
		}
		var t CronTask
		if err := json.Unmarshal([]byte(taskData), &t); err != nil {
			return err
		}
		task = &t
	}

	// 执行任务
	go m.executeTask(task)
	return nil
}

// StartMessageSubscriber 启动消息订阅
func (m *CronManager) StartMessageSubscriber(ctx context.Context) {
	go func() {
		pubsub := m.rdb.Subscribe(ctx, "cscan:cron:reload", "cscan:cron:remove", "cscan:cron:runnow")
		defer pubsub.Close()

		ch := pubsub.Channel()
		for msg := range ch {
			switch msg.Channel {
			case "cscan:cron:reload":
				m.ReloadTask(ctx, msg.Payload)
			case "cscan:cron:remove":
				if task, ok := m.tasks[msg.Payload]; ok {
					if task.EntryId > 0 {
						m.scheduler.RemoveCronTask(task.EntryId)
					}
					delete(m.tasks, msg.Payload)
				}
			case "cscan:cron:runnow":
				m.RunTaskNow(ctx, msg.Payload)
			}
		}
	}()
}
