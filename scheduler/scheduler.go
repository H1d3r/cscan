package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

// 任务状态常量
const (
	TaskStatusCreated = "CREATED"
	TaskStatusPending = "PENDING"
	TaskStatusStarted = "STARTED"
	TaskStatusSuccess = "SUCCESS"
	TaskStatusFailure = "FAILURE"
	TaskStatusRevoked = "REVOKED"
)

// PriorityQueueMetrics 优先级队列性能指标
type PriorityQueueMetrics struct {
	PushCount      int64         // 推送任务总数
	PopCount       int64         // 弹出任务总数
	PushLatencySum int64         // 推送延迟总和（纳秒）
	PopLatencySum  int64         // 弹出延迟总和（纳秒）
	LastPushTime   time.Time     // 最后推送时间
	LastPopTime    time.Time     // 最后弹出时间
	mu             sync.RWMutex
}

// RecordPush 记录推送操作
func (m *PriorityQueueMetrics) RecordPush(latency time.Duration) {
	atomic.AddInt64(&m.PushCount, 1)
	atomic.AddInt64(&m.PushLatencySum, int64(latency))
	m.mu.Lock()
	m.LastPushTime = time.Now()
	m.mu.Unlock()
}

// RecordPop 记录弹出操作
func (m *PriorityQueueMetrics) RecordPop(latency time.Duration) {
	atomic.AddInt64(&m.PopCount, 1)
	atomic.AddInt64(&m.PopLatencySum, int64(latency))
	m.mu.Lock()
	m.LastPopTime = time.Now()
	m.mu.Unlock()
}

// GetStats 获取统计信息
func (m *PriorityQueueMetrics) GetStats() map[string]interface{} {
	pushCount := atomic.LoadInt64(&m.PushCount)
	popCount := atomic.LoadInt64(&m.PopCount)
	pushLatencySum := atomic.LoadInt64(&m.PushLatencySum)
	popLatencySum := atomic.LoadInt64(&m.PopLatencySum)

	m.mu.RLock()
	lastPush := m.LastPushTime
	lastPop := m.LastPopTime
	m.mu.RUnlock()

	var avgPushLatency, avgPopLatency float64
	if pushCount > 0 {
		avgPushLatency = float64(pushLatencySum) / float64(pushCount) / float64(time.Millisecond)
	}
	if popCount > 0 {
		avgPopLatency = float64(popLatencySum) / float64(popCount) / float64(time.Millisecond)
	}

	return map[string]interface{}{
		"pushCount":       pushCount,
		"popCount":        popCount,
		"avgPushLatencyMs": avgPushLatency,
		"avgPopLatencyMs":  avgPopLatency,
		"lastPushTime":    lastPush,
		"lastPopTime":     lastPop,
	}
}

// TaskInfo 任务信息
type TaskInfo struct {
	TaskId      string   `json:"taskId"`
	MainTaskId  string   `json:"mainTaskId"`
	WorkspaceId string   `json:"workspaceId"`
	TaskName    string   `json:"taskName"`
	Config      string   `json:"config"`
	Priority    int      `json:"priority"`
	CreateTime  string   `json:"createTime"`
	Workers     []string `json:"workers,omitempty"` // 指定执行任务的 Worker 列表，为空表示任意 Worker
}

// WorkerLoad Worker负载信息
type WorkerLoad struct {
	WorkerName     string    `json:"workerName"`
	CurrentTasks   int       `json:"currentTasks"`
	MaxConcurrency int       `json:"maxConcurrency"`
	CPUPercent     float64   `json:"cpuPercent"`
	MemPercent     float64   `json:"memPercent"`
	LastHeartbeat  time.Time `json:"lastHeartbeat"`
}

// LoadScore 计算负载分数（越低越好）
func (w *WorkerLoad) LoadScore() float64 {
	if w.MaxConcurrency == 0 {
		return 100.0
	}
	// 综合考虑任务负载、CPU和内存
	taskLoad := float64(w.CurrentTasks) / float64(w.MaxConcurrency) * 100
	return taskLoad*0.5 + w.CPUPercent*0.3 + w.MemPercent*0.2
}

// IsAvailable 检查Worker是否可用
func (w *WorkerLoad) IsAvailable() bool {
	// 心跳超过30秒认为不可用
	if time.Since(w.LastHeartbeat) > 30*time.Second {
		return false
	}
	// 任务已满
	if w.CurrentTasks >= w.MaxConcurrency {
		return false
	}
	// CPU或内存过高
	if w.CPUPercent > 90 || w.MemPercent > 90 {
		return false
	}
	return true
}

// Scheduler 任务调度器
type Scheduler struct {
	rdb           *redis.Client
	cron          *cron.Cron
	queueKey      string
	processingKey string
	workerLoadKey string // Worker负载信息Key
	mu            sync.Mutex
	handlers      map[string]TaskHandler
	metrics       *PriorityQueueMetrics // 性能指标
}

// TaskHandler 任务处理函数
type TaskHandler func(ctx context.Context, task *TaskInfo) error

// NewScheduler 创建调度器
func NewScheduler(rdb *redis.Client) *Scheduler {
	return &Scheduler{
		rdb:           rdb,
		cron:          cron.New(cron.WithSeconds()),
		queueKey:      "cscan:task:queue",
		processingKey: "cscan:task:processing",
		workerLoadKey: "cscan:worker:load",
		handlers:      make(map[string]TaskHandler),
		metrics:       &PriorityQueueMetrics{},
	}
}

// RegisterHandler 注册任务处理器
func (s *Scheduler) RegisterHandler(taskName string, handler TaskHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[taskName] = handler
}

// Start 启动调度器
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// AddCronTask 添加定时任务
func (s *Scheduler) AddCronTask(spec string, taskFunc func()) (cron.EntryID, error) {
	return s.cron.AddFunc(spec, taskFunc)
}

// RemoveCronTask 移除定时任务
func (s *Scheduler) RemoveCronTask(id cron.EntryID) {
	s.cron.Remove(id)
}

// GetWorkerQueueKey 获取 Worker 专属队列的 Key
func (s *Scheduler) GetWorkerQueueKey(workerName string) string {
	return fmt.Sprintf("cscan:task:queue:worker:%s", strings.ToLower(workerName))
}

// GetMetrics 获取性能指标
func (s *Scheduler) GetMetrics() *PriorityQueueMetrics {
	return s.metrics
}

// calculatePriorityScore 计算优先级分数
// 分数越小优先级越高，确保高优先级任务先被处理
// Priority值越大，优先级越高（减去更多）
func (s *Scheduler) calculatePriorityScore(priority int, createTime time.Time) float64 {
	// 基础分数为创建时间戳
	baseScore := float64(createTime.Unix())
	// 优先级调整：每个优先级单位减少1000秒的分数
	// 这确保高优先级任务即使创建时间较晚也会先被处理
	priorityAdjustment := float64(priority * 1000)
	return baseScore - priorityAdjustment
}

// PushTask 推送任务到队列
// 如果任务指定了 Workers，则推送到每个 Worker 的专属队列
// 否则推送到公共队列
func (s *Scheduler) PushTask(ctx context.Context, task *TaskInfo) error {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPush(time.Since(startTime))
	}()

	if task.TaskId == "" {
		task.TaskId = uuid.New().String()
	}
	now := time.Now()
	task.CreateTime = now.Local().Format("2006-01-02 15:04:05")

	data, err := json.Marshal(task)
	if err != nil {
		return err
	}

	// 使用统一的优先级分数计算
	score := s.calculatePriorityScore(task.Priority, now)

	// 如果指定了 Workers，推送到每个 Worker 的专属队列
	if len(task.Workers) > 0 {
		pipe := s.rdb.Pipeline()
		for _, workerName := range task.Workers {
			workerQueueKey := s.GetWorkerQueueKey(workerName)
			pipe.ZAdd(ctx, workerQueueKey, redis.Z{
				Score:  score,
				Member: data,
			})
		}
		_, err = pipe.Exec(ctx)
		return err
	}

	// 没有指定 Worker，推送到公共队列
	return s.rdb.ZAdd(ctx, s.queueKey, redis.Z{
		Score:  score,
		Member: data,
	}).Err()
}

// PushTaskBatch 批量推送任务到队列（使用 Pipeline 提高性能）
// 如果任务指定了 Workers，则推送到每个 Worker 的专属队列
// 否则推送到公共队列
func (s *Scheduler) PushTaskBatch(ctx context.Context, tasks []*TaskInfo) error {
	if len(tasks) == 0 {
		return nil
	}

	startTime := time.Now()
	defer func() {
		// 记录每个任务的平均推送时间
		avgLatency := time.Since(startTime) / time.Duration(len(tasks))
		for range tasks {
			s.metrics.RecordPush(avgLatency)
		}
	}()

	pipe := s.rdb.Pipeline()
	baseTime := time.Now()

	for i, task := range tasks {
		if task.TaskId == "" {
			task.TaskId = uuid.New().String()
		}
		task.CreateTime = baseTime.Local().Format("2006-01-02 15:04:05")

		data, err := json.Marshal(task)
		if err != nil {
			continue
		}

		// 使用统一的优先级分数计算
		// 同一批次的任务按顺序递增分数，保持顺序
		score := s.calculatePriorityScore(task.Priority, baseTime) + float64(i)*0.001

		// 如果指定了 Workers，推送到每个 Worker 的专属队列
		if len(task.Workers) > 0 {
			for _, workerName := range task.Workers {
				workerQueueKey := s.GetWorkerQueueKey(workerName)
				pipe.ZAdd(ctx, workerQueueKey, redis.Z{
					Score:  score,
					Member: data,
				})
			}
		} else {
			// 没有指定 Worker，推送到公共队列
			pipe.ZAdd(ctx, s.queueKey, redis.Z{
				Score:  score,
				Member: data,
			})
		}
	}

	_, err := pipe.Exec(ctx)
	return err
}

// PopTask 从队列获取任务
func (s *Scheduler) PopTask(ctx context.Context) (*TaskInfo, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPop(time.Since(startTime))
	}()

	// 获取优先级最高的任务（分数最小）
	results, err := s.rdb.ZPopMin(ctx, s.queueKey, 1).Result()
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(results[0].Member.(string)), &task); err != nil {
		return nil, err
	}

	// 添加到处理中集合
	s.rdb.SAdd(ctx, s.processingKey, task.TaskId)

	return &task, nil
}

// PopTaskForWorker 从队列获取任务（考虑Worker负载）
// 优先从Worker专属队列获取，然后从公共队列获取
func (s *Scheduler) PopTaskForWorker(ctx context.Context, workerName string) (*TaskInfo, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordPop(time.Since(startTime))
	}()

	// 1. 优先从Worker专属队列获取
	workerQueueKey := s.GetWorkerQueueKey(workerName)
	results, err := s.rdb.ZPopMin(ctx, workerQueueKey, 1).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}

	// 2. 如果专属队列为空，从公共队列获取
	if len(results) == 0 {
		results, err = s.rdb.ZPopMin(ctx, s.queueKey, 1).Result()
		if err != nil {
			return nil, err
		}
	}

	if len(results) == 0 {
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(results[0].Member.(string)), &task); err != nil {
		return nil, err
	}

	// 添加到处理中集合
	s.rdb.SAdd(ctx, s.processingKey, task.TaskId)

	return &task, nil
}

// PeekTask 查看队列中优先级最高的任务（不移除）
func (s *Scheduler) PeekTask(ctx context.Context) (*TaskInfo, error) {
	results, err := s.rdb.ZRangeWithScores(ctx, s.queueKey, 0, 0).Result()
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(results[0].Member.(string)), &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// GetTasksByPriority 获取指定优先级范围的任务
func (s *Scheduler) GetTasksByPriority(ctx context.Context, minPriority, maxPriority int, limit int64) ([]*TaskInfo, error) {
	// 计算分数范围（注意：分数越小优先级越高）
	now := time.Now()
	maxScore := s.calculatePriorityScore(minPriority, now)
	minScore := s.calculatePriorityScore(maxPriority, now)

	results, err := s.rdb.ZRangeByScoreWithScores(ctx, s.queueKey, &redis.ZRangeBy{
		Min:   fmt.Sprintf("%f", minScore),
		Max:   fmt.Sprintf("%f", maxScore),
		Count: limit,
	}).Result()
	if err != nil {
		return nil, err
	}

	tasks := make([]*TaskInfo, 0, len(results))
	for _, r := range results {
		var task TaskInfo
		if err := json.Unmarshal([]byte(r.Member.(string)), &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// CompleteTask 完成任务
func (s *Scheduler) CompleteTask(ctx context.Context, taskId string) error {
	return s.rdb.SRem(ctx, s.processingKey, taskId).Err()
}

// GetQueueLength 获取队列长度
func (s *Scheduler) GetQueueLength(ctx context.Context) (int64, error) {
	return s.rdb.ZCard(ctx, s.queueKey).Result()
}

// GetProcessingCount 获取处理中任务数
func (s *Scheduler) GetProcessingCount(ctx context.Context) (int64, error) {
	return s.rdb.SCard(ctx, s.processingKey).Result()
}

// TaskConfig 任务配置
type TaskConfig struct {
	PortScan     *PortScanConfig     `json:"portscan,omitempty"`
	PortIdentify *PortIdentifyConfig `json:"portidentify,omitempty"` // 端口识别（Nmap服务识别）
	DomainScan   *DomainScanConfig   `json:"domainscan,omitempty"`
	Fingerprint  *FingerprintConfig  `json:"fingerprint,omitempty"`
	PocScan      *PocScanConfig      `json:"pocscan,omitempty"`
	DirScan      *DirScanConfig      `json:"dirscan,omitempty"` // 目录扫描
}

// DirScanConfig 目录扫描配置
type DirScanConfig struct {
	Enable         bool     `json:"enable"`
	DictIds        []string `json:"dictIds"`        // 字典ID列表
	Threads        int      `json:"threads"`        // 并发线程数
	Timeout        int      `json:"timeout"`        // 单个请求超时(秒)
	StatusCodes    []int    `json:"statusCodes"`    // 有效状态码列表
	Extensions     []string `json:"extensions"`     // 文件扩展名
	FollowRedirect bool     `json:"followRedirect"` // 是否跟随重定向
}

type PortScanConfig struct {
	Enable            bool   `json:"enable"`
	Tool              string `json:"tool"`              // tcp, masscan, naabu
	Ports             string `json:"ports"`
	Rate              int    `json:"rate"`              // 每秒发送包数，默认3000，建议3000-7000
	Timeout           int    `json:"timeout"`           // 端口扫描超时时间(秒)，默认5秒
	PortThreshold     int    `json:"portThreshold"`     // 开放端口数量阈值，超过则过滤该主机
	ScanType          string `json:"scanType"`          // s=SYN, c=CONNECT，默认 c
	SkipHostDiscovery bool   `json:"skipHostDiscovery"` // 跳过主机发现 (-Pn)
	ExcludeCDN        bool   `json:"excludeCDN"`        // 排除 CDN/WAF，仅扫描 80,443 端口 (-ec)
	ExcludeHosts      string `json:"excludeHosts"`      // 排除的目标，逗号分隔的 IP/CIDR
	Retries           int    `json:"retries"`           // 重试次数，默认2，建议1-2
	WarmUpTime        int    `json:"warmUpTime"`        // 扫描阶段间等待时间(秒)，默认1，建议0-1
	Workers           int    `json:"workers"`           // Naabu内部工作线程，默认50，建议50-100
	Verify            bool   `json:"verify"`            // TCP验证，默认false（禁用以提速）
}

// PortIdentifyConfig 端口识别配置（Nmap/Fingerprintx 服务识别）
type PortIdentifyConfig struct {
	Enable      bool   `json:"enable"`
	Tool        string `json:"tool"`        // 识别工具: nmap, fingerprintx (默认 nmap)
	Timeout     int    `json:"timeout"`     // 单个主机超时时间(秒)，默认30秒
	Concurrency int    `json:"concurrency"` // 并发数，默认10 (仅 fingerprintx)
	Args        string `json:"args"`        // Nmap额外参数，如 "-sV --version-intensity 5"
	UDP         bool   `json:"udp"`         // 是否扫描UDP端口 (仅 fingerprintx)
	FastMode    bool   `json:"fastMode"`    // 快速模式 (仅 fingerprintx)
}

type DomainScanConfig struct {
	Enable             bool     `json:"enable"`
	Subfinder          bool     `json:"subfinder"`          // 使用Subfinder
	Timeout            int      `json:"timeout"`            // 超时时间(秒)
	MaxEnumerationTime int      `json:"maxEnumerationTime"` // 最大枚举时间(分钟)
	Threads            int      `json:"threads"`            // 并发线程数
	RateLimit          int      `json:"rateLimit"`          // 速率限制
	Sources            []string `json:"sources"`            // 指定数据源
	ExcludeSources     []string `json:"excludeSources"`     // 排除数据源
	All                bool     `json:"all"`                // 使用所有数据源(慢)
	Recursive          bool     `json:"recursive"`          // 只使用递归数据源
	RemoveWildcard     bool     `json:"removeWildcard"`     // 移除泛解析域名
	ResolveDNS         bool     `json:"resolveDNS"`         // 是否解析DNS（使用dnsx）
	Concurrent         int      `json:"concurrent"`         // DNS解析并发数
	SubdomainDictIds   []string `json:"subdomainDictIds"`   // 子域名暴力破解字典ID列表
	// 子域名暴力破解引擎配置
	BruteforceEngine   string   `json:"bruteforceEngine"`   // 暴力破解引擎: dnsx, ksubdomain (默认ksubdomain)
	Bandwidth          string   `json:"bandwidth"`          // ksubdomain带宽限制，如"5M", "10M", "100M"
	Retry              int      `json:"retry"`              // ksubdomain重试次数
	WildcardMode       string   `json:"wildcardMode"`       // ksubdomain泛解析过滤模式: basic, advanced, none
	// Dnsx增强功能
	RecursiveBrute       bool     `json:"recursiveBrute"`       // 递归爆破
	RecursiveDictIds     []string `json:"recursiveDictIds"`     // 递归爆破字典ID列表
	WildcardDetect       bool     `json:"wildcardDetect"`       // 泛解析检测并处理
	SubdomainCrawl       bool     `json:"subdomainCrawl"`       // 子域爬取（从响应体和JS中发现子域）
	TakeoverCheck        bool     `json:"takeoverCheck"`        // 子域接管检查
}

type FingerprintConfig struct {
	Enable        bool   `json:"enable"`
	Tool          string `json:"tool"`          // 探测工具: httpx, builtin (wappalyzer)
	Httpx         bool   `json:"httpx"`         // 已废弃，使用Tool字段
	IconHash      bool   `json:"iconHash"`
	Wappalyzer    bool   `json:"wappalyzer"`    // 已废弃，builtin模式自动启用
	CustomEngine  bool   `json:"customEngine"`  // 使用自定义指纹引擎（ARL格式）
	Screenshot    bool   `json:"screenshot"`
	ActiveScan    bool   `json:"activeScan"`    // 启用主动指纹扫描
	ActiveTimeout int    `json:"activeTimeout"` // 主动指纹单个请求超时时间(秒)，默认10秒
	Timeout       int    `json:"timeout"`       // 总超时时间(秒)，默认300秒
	TargetTimeout int    `json:"targetTimeout"` // 单个目标超时时间(秒)，默认30秒
	Concurrency   int    `json:"concurrency"`   // 指纹识别并发数，默认10
	FilterMode    string `json:"filterMode"`    // 过滤模式: "http_mapping"(使用HTTP映射), "service_mapping"(使用服务映射过滤非HTTP)
}

type PocScanConfig struct {
	Enable            bool                `json:"enable"`
	PocTypes          []string            `json:"pocTypes"`          // nuclei, builtin
	PocFiles          []string            `json:"pocFiles"`          // 自定义POC文件
	UseNuclei         bool                `json:"useNuclei"`         // 使用Nuclei扫描
	AutoScan          bool                `json:"autoScan"`          // 基于自定义标签映射自动扫描
	AutomaticScan     bool                `json:"automaticScan"`     // 基于Wappalyzer内置映射自动扫描（类似nuclei -as）
	Severity          string              `json:"severity"`          // 严重级别过滤
	Tags              []string            `json:"tags"`              // 手动指定标签
	ExcludeTags       []string            `json:"excludeTags"`       // 排除标签
	RateLimit         int                 `json:"rateLimit"`         // 速率限制
	Concurrency       int                 `json:"concurrency"`       // 并发数
	TargetTimeout     int                 `json:"targetTimeout"`     // 单个目标超时时间(秒)，默认600秒
	CustomPocOnly     bool                `json:"customPocOnly"`     // 只使用自定义POC
	CustomTemplates   []string            `json:"customTemplates"`   // 自定义POC模板内容(YAML) - 已废弃
	NucleiTemplates   []string            `json:"nucleiTemplates"`   // 从数据库获取的模板内容(YAML) - 已废弃
	NucleiTemplateIds []string            `json:"nucleiTemplateIds"` // Nuclei模板ID列表（新）
	CustomPocIds      []string            `json:"customPocIds"`      // 自定义POC ID列表（新）
	TagMappings       map[string][]string `json:"tagMappings"`       // 应用名称到Nuclei标签的映射
}

// ParseTaskConfig 解析任务配置
func ParseTaskConfig(configStr string) (*TaskConfig, error) {
	var config TaskConfig
	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// BuildTaskConfig 构建任务配置
func BuildTaskConfig(config *TaskConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TaskResult 任务结果
type TaskResult struct {
	TaskId     string `json:"taskId"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	AssetCount int    `json:"assetCount"`
	VulCount   int    `json:"vulCount"`
	Duration   int64  `json:"duration"`
}

// FormatResult 格式化结果
func (r *TaskResult) FormatResult() string {
	return fmt.Sprintf("状态:%s 资产:%d 漏洞:%d 耗时:%ds",
		r.Status, r.AssetCount, r.VulCount, r.Duration)
}

// ==================== Worker Load Management ====================

// UpdateWorkerLoad 更新Worker负载信息
func (s *Scheduler) UpdateWorkerLoad(ctx context.Context, load *WorkerLoad) error {
	data, err := json.Marshal(load)
	if err != nil {
		return err
	}
	// 使用Hash存储，key为worker名称
	return s.rdb.HSet(ctx, s.workerLoadKey, load.WorkerName, data).Err()
}

// GetWorkerLoad 获取单个Worker负载信息
func (s *Scheduler) GetWorkerLoad(ctx context.Context, workerName string) (*WorkerLoad, error) {
	data, err := s.rdb.HGet(ctx, s.workerLoadKey, workerName).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var load WorkerLoad
	if err := json.Unmarshal([]byte(data), &load); err != nil {
		return nil, err
	}
	return &load, nil
}

// GetAllWorkerLoads 获取所有Worker负载信息
func (s *Scheduler) GetAllWorkerLoads(ctx context.Context) ([]*WorkerLoad, error) {
	data, err := s.rdb.HGetAll(ctx, s.workerLoadKey).Result()
	if err != nil {
		return nil, err
	}

	loads := make([]*WorkerLoad, 0, len(data))
	for _, v := range data {
		var load WorkerLoad
		if err := json.Unmarshal([]byte(v), &load); err != nil {
			continue
		}
		loads = append(loads, &load)
	}
	return loads, nil
}

// GetAvailableWorkers 获取可用的Worker列表（按负载排序）
func (s *Scheduler) GetAvailableWorkers(ctx context.Context) ([]*WorkerLoad, error) {
	loads, err := s.GetAllWorkerLoads(ctx)
	if err != nil {
		return nil, err
	}

	// 过滤可用的Worker
	available := make([]*WorkerLoad, 0)
	for _, load := range loads {
		if load.IsAvailable() {
			available = append(available, load)
		}
	}

	// 按负载分数排序（升序，负载低的在前）
	for i := 0; i < len(available)-1; i++ {
		for j := i + 1; j < len(available); j++ {
			if available[i].LoadScore() > available[j].LoadScore() {
				available[i], available[j] = available[j], available[i]
			}
		}
	}

	return available, nil
}

// SelectWorkerForTask 为任务选择最佳Worker
// 返回负载最低的可用Worker
func (s *Scheduler) SelectWorkerForTask(ctx context.Context) (*WorkerLoad, error) {
	workers, err := s.GetAvailableWorkers(ctx)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 {
		return nil, nil
	}
	return workers[0], nil
}

// RemoveWorkerLoad 移除Worker负载信息（Worker下线时调用）
func (s *Scheduler) RemoveWorkerLoad(ctx context.Context, workerName string) error {
	return s.rdb.HDel(ctx, s.workerLoadKey, workerName).Err()
}

// ==================== Task Cancellation ====================

// CancelSignal 取消信号
type CancelSignal struct {
	TaskId    string    `json:"taskId"`
	Action    string    `json:"action"` // STOP, PAUSE
	Timestamp time.Time `json:"timestamp"`
}

// GetCancelSignalKey 获取取消信号的Redis Key
func (s *Scheduler) GetCancelSignalKey(taskId string) string {
	return fmt.Sprintf("cscan:task:cancel:%s", taskId)
}

// SendCancelSignal 发送取消信号
func (s *Scheduler) SendCancelSignal(ctx context.Context, taskId, action string) error {
	signal := &CancelSignal{
		TaskId:    taskId,
		Action:    action,
		Timestamp: time.Now(),
	}
	data, err := json.Marshal(signal)
	if err != nil {
		return err
	}

	key := s.GetCancelSignalKey(taskId)
	// 设置信号，5分钟后自动过期
	return s.rdb.Set(ctx, key, data, 5*time.Minute).Err()
}

// CheckCancelSignal 检查取消信号
func (s *Scheduler) CheckCancelSignal(ctx context.Context, taskId string) (*CancelSignal, error) {
	key := s.GetCancelSignalKey(taskId)
	data, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var signal CancelSignal
	if err := json.Unmarshal([]byte(data), &signal); err != nil {
		return nil, err
	}
	return &signal, nil
}

// ClearCancelSignal 清除取消信号
func (s *Scheduler) ClearCancelSignal(ctx context.Context, taskId string) error {
	key := s.GetCancelSignalKey(taskId)
	return s.rdb.Del(ctx, key).Err()
}

// PublishCancelSignal 通过Pub/Sub发布取消信号（实时通知）
func (s *Scheduler) PublishCancelSignal(ctx context.Context, taskId, action string) error {
	signal := &CancelSignal{
		TaskId:    taskId,
		Action:    action,
		Timestamp: time.Now(),
	}
	data, err := json.Marshal(signal)
	if err != nil {
		return err
	}

	// 同时设置Key（用于轮询检查）和发布消息（用于实时通知）
	key := s.GetCancelSignalKey(taskId)
	if err := s.rdb.Set(ctx, key, data, 5*time.Minute).Err(); err != nil {
		return err
	}

	// 发布到取消信号频道
	return s.rdb.Publish(ctx, "cscan:task:cancel", data).Err()
}

// SubscribeCancelSignals 订阅取消信号
func (s *Scheduler) SubscribeCancelSignals(ctx context.Context) <-chan *CancelSignal {
	ch := make(chan *CancelSignal, 100)

	go func() {
		defer close(ch)

		pubsub := s.rdb.Subscribe(ctx, "cscan:task:cancel")
		defer pubsub.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-pubsub.Channel():
				var signal CancelSignal
				if err := json.Unmarshal([]byte(msg.Payload), &signal); err != nil {
					continue
				}
				select {
				case ch <- &signal:
				default:
					// 通道满了，丢弃旧信号
				}
			}
		}
	}()

	return ch
}
