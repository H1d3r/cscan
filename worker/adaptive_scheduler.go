package worker

import (
	"context"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// ScheduleMode 调度模式
type ScheduleMode int

const (
	ModeAggressive   ScheduleMode = iota // 激进模式：最大化吞吐量
	ModeNormal                           // 正常模式：平衡性能和资源
	ModeConservative                     // 保守模式：优先保护系统稳定
	ModeCritical                         // 危急模式：最小化资源使用
)

func (m ScheduleMode) String() string {
	switch m {
	case ModeAggressive:
		return "aggressive"
	case ModeNormal:
		return "normal"
	case ModeConservative:
		return "conservative"
	case ModeCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// ResourceMetrics 资源指标
type ResourceMetrics struct {
	CPUPercent    float64   // CPU使用率
	MemPercent    float64   // 内存使用率
	CPUCores      int       // CPU核心数
	TotalMemoryMB uint64    // 总内存(MB)
	AvailMemoryMB uint64    // 可用内存(MB)
	LoadAvg1      float64   // 1分钟负载
	LoadAvg5      float64   // 5分钟负载
	LoadAvg15     float64   // 15分钟负载
	Timestamp     time.Time // 采集时间
}

// AdaptiveSchedulerConfig 自适应调度器配置
type AdaptiveSchedulerConfig struct {
	// 基础配置
	BaseConcurrency int // 基础并发数（用户配置的值）
	MinConcurrency  int // 最小并发数
	MaxConcurrency  int // 最大并发数（不超过基础并发数）

	// 资源阈值配置
	CPULowThreshold      float64 // CPU低负载阈值（可以增加并发）
	CPUHighThreshold     float64 // CPU高负载阈值（需要减少并发）
	CPUCriticalThreshold float64 // CPU危急阈值（立即限流）
	MemLowThreshold      float64 // 内存低负载阈值
	MemHighThreshold     float64 // 内存高负载阈值
	MemCriticalThreshold float64 // 内存危急阈值

	// 调度参数
	SampleInterval    time.Duration // 资源采样间隔
	AdjustInterval    time.Duration // 并发调整间隔
	HistorySize       int           // 历史数据保留数量
	SmoothingFactor   float64       // 平滑因子（0-1，越大越平滑）
	ScaleUpCooldown   time.Duration // 扩容冷却时间
	ScaleDownCooldown time.Duration // 缩容冷却时间

	// 任务拉取配置
	MinPullInterval time.Duration // 最小拉取间隔
	MaxPullInterval time.Duration // 最大拉取间隔
	IdleMultiplier  float64       // 空闲时拉取间隔倍数
}

// DefaultAdaptiveSchedulerConfig 默认配置
// 根据系统硬件自适应调整阈值，低配机器使用更宽松的阈值
func DefaultAdaptiveSchedulerConfig(baseConcurrency int) *AdaptiveSchedulerConfig {
	if baseConcurrency <= 0 {
		baseConcurrency = runtime.NumCPU()
	}

	cpuCores := runtime.NumCPU()
	totalMemMB := getAdaptiveSchedulerTotalMemMB()

	// 根据硬件配置分级
	var cpuLow, cpuHigh, cpuCritical float64
	var memLow, memHigh, memCritical float64

	if cpuCores <= 4 || totalMemMB <= 4096 {
		// 低配：放宽所有阈值，避免频繁限流
		cpuLow = 50.0
		cpuHigh = 80.0
		cpuCritical = 92.0
		memLow = 60.0
		memHigh = 82.0
		memCritical = 93.0
	} else if cpuCores <= 8 && totalMemMB <= 16384 {
		// 中配：适度放宽
		cpuLow = 45.0
		cpuHigh = 75.0
		cpuCritical = 88.0
		memLow = 55.0
		memHigh = 78.0
		memCritical = 92.0
	} else {
		// 高配：使用原始阈值
		cpuLow = 40.0
		cpuHigh = 70.0
		cpuCritical = 85.0
		memLow = 50.0
		memHigh = 75.0
		memCritical = 90.0
	}

	return &AdaptiveSchedulerConfig{
		BaseConcurrency:      baseConcurrency,
		MinConcurrency:       1,
		MaxConcurrency:       baseConcurrency,
		CPULowThreshold:      cpuLow,
		CPUHighThreshold:     cpuHigh,
		CPUCriticalThreshold: cpuCritical,
		MemLowThreshold:      memLow,
		MemHighThreshold:     memHigh,
		MemCriticalThreshold: memCritical,
		SampleInterval:       time.Second,
		AdjustInterval:       5 * time.Second,
		HistorySize:          60,
		SmoothingFactor:      0.3,
		ScaleUpCooldown:      30 * time.Second,
		ScaleDownCooldown:    10 * time.Second,
		MinPullInterval:      3 * time.Second,
		MaxPullInterval:      10 * time.Second,
		IdleMultiplier:       2.0,
	}
}

// getAdaptiveSchedulerTotalMemMB 获取系统总内存(MB)
func getAdaptiveSchedulerTotalMemMB() uint64 {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return 0
	}
	return memInfo.Total / 1024 / 1024
}

// AdaptiveScheduler 自适应调度器
type AdaptiveScheduler struct {
	config *AdaptiveSchedulerConfig

	mu sync.RWMutex

	// 当前状态
	currentMode        ScheduleMode
	currentConcurrency int
	currentTasks       int32 // 使用atomic操作

	// 资源历史
	metricsHistory []ResourceMetrics
	smoothedCPU    float64
	smoothedMem    float64

	// 冷却时间
	lastScaleUp   time.Time
	lastScaleDown time.Time

	// 统计信息
	stats SchedulerStats

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 日志回调
	logger func(level, format string, args ...interface{})
}

// SchedulerStats 调度器统计
type SchedulerStats struct {
	TotalTasksAccepted int64     // 接受的任务总数
	TotalTasksRejected int64     // 拒绝的任务总数
	TotalScaleUps      int64     // 扩容次数
	TotalScaleDowns    int64     // 缩容次数
	TotalThrottles     int64     // 限流次数
	CurrentMode        string    // 当前模式
	CurrentConcurrency int       // 当前并发数
	AvgCPU             float64   // 平均CPU
	AvgMem             float64   // 平均内存
	LastAdjustTime     time.Time // 上次调整时间
	LastThrottleTime   time.Time // 上次限流时间
	ThrottledUntil     time.Time // 限流结束时间
	PullInterval       int64     // 当前拉取间隔(ms)
}

// NewAdaptiveScheduler 创建自适应调度器
func NewAdaptiveScheduler(config *AdaptiveSchedulerConfig) *AdaptiveScheduler {
	if config == nil {
		config = DefaultAdaptiveSchedulerConfig(runtime.NumCPU())
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &AdaptiveScheduler{
		config:             config,
		currentMode:        ModeNormal,
		currentConcurrency: config.BaseConcurrency,
		metricsHistory:     make([]ResourceMetrics, 0, config.HistorySize),
		ctx:                ctx,
		cancel:             cancel,
	}

	return s
}

// SetLogger 设置日志回调
func (s *AdaptiveScheduler) SetLogger(logger func(level, format string, args ...interface{})) {
	s.logger = logger
}

func (s *AdaptiveScheduler) log(level, format string, args ...interface{}) {
	if s.logger != nil {
		s.logger(level, format, args...)
	}
}

// Start 启动调度器
func (s *AdaptiveScheduler) Start() {
	s.wg.Add(1)
	go s.monitorLoop()
	s.log("INFO", "Adaptive scheduler started, base concurrency: %d", s.config.BaseConcurrency)
}

// Stop 停止调度器
func (s *AdaptiveScheduler) Stop() {
	s.cancel()
	s.wg.Wait()
	s.log("INFO", "Adaptive scheduler stopped")
}

// monitorLoop 监控循环
func (s *AdaptiveScheduler) monitorLoop() {
	defer s.wg.Done()

	sampleTicker := time.NewTicker(s.config.SampleInterval)
	adjustTicker := time.NewTicker(s.config.AdjustInterval)
	defer sampleTicker.Stop()
	defer adjustTicker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-sampleTicker.C:
			s.sampleMetrics()
		case <-adjustTicker.C:
			s.adjustConcurrency()
		}
	}
}

// sampleMetrics 采样资源指标
func (s *AdaptiveScheduler) sampleMetrics() {
	metrics := s.collectMetrics()

	s.mu.Lock()
	defer s.mu.Unlock()

	// 添加到历史
	s.metricsHistory = append(s.metricsHistory, metrics)
	if len(s.metricsHistory) > s.config.HistorySize {
		s.metricsHistory = s.metricsHistory[1:]
	}

	// 指数移动平均平滑
	alpha := s.config.SmoothingFactor
	s.smoothedCPU = alpha*metrics.CPUPercent + (1-alpha)*s.smoothedCPU
	s.smoothedMem = alpha*metrics.MemPercent + (1-alpha)*s.smoothedMem

	// 更新统计
	s.stats.AvgCPU = s.smoothedCPU
	s.stats.AvgMem = s.smoothedMem
}

// collectMetrics 收集资源指标
func (s *AdaptiveScheduler) collectMetrics() ResourceMetrics {
	metrics := ResourceMetrics{
		Timestamp: time.Now(),
		CPUCores:  runtime.NumCPU(),
	}

	// CPU使用率（采样100ms获取更准确的值）
	if cpuPercent, err := cpu.Percent(100*time.Millisecond, false); err == nil && len(cpuPercent) > 0 {
		metrics.CPUPercent = cpuPercent[0]
	}

	// 内存使用率
	if memInfo, err := mem.VirtualMemory(); err == nil {
		metrics.MemPercent = memInfo.UsedPercent
		metrics.TotalMemoryMB = memInfo.Total / 1024 / 1024
		metrics.AvailMemoryMB = memInfo.Available / 1024 / 1024
	}

	// 负载（仅Linux/Unix，Windows上会返回错误）
	if loadAvg, err := getLoadAvg(); err == nil {
		metrics.LoadAvg1 = loadAvg.Load1
		metrics.LoadAvg5 = loadAvg.Load5
		metrics.LoadAvg15 = loadAvg.Load15
	}

	return metrics
}

// LoadAvgInfo 负载信息
type LoadAvgInfo struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

// adjustConcurrency 调整并发数
func (s *AdaptiveScheduler) adjustConcurrency() {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldMode := s.currentMode
	oldConcurrency := s.currentConcurrency

	// 确定新模式
	newMode := s.determineMode()
	s.currentMode = newMode

	// 根据模式调整并发
	targetConcurrency := s.calculateTargetConcurrency(newMode)

	// 应用冷却时间
	now := time.Now()
	if targetConcurrency > s.currentConcurrency {
		if now.Sub(s.lastScaleUp) < s.config.ScaleUpCooldown {
			return // 扩容冷却中
		}
		s.lastScaleUp = now
		atomic.AddInt64(&s.stats.TotalScaleUps, 1)
	} else if targetConcurrency < s.currentConcurrency {
		if now.Sub(s.lastScaleDown) < s.config.ScaleDownCooldown {
			return // 缩容冷却中
		}
		s.lastScaleDown = now
		atomic.AddInt64(&s.stats.TotalScaleDowns, 1)
	}

	// 渐进式调整（每次最多调整25%）
	maxChange := int(math.Ceil(float64(s.currentConcurrency) * 0.25))
	if maxChange < 1 {
		maxChange = 1
	}

	diff := targetConcurrency - s.currentConcurrency
	if diff > maxChange {
		diff = maxChange
	} else if diff < -maxChange {
		diff = -maxChange
	}

	s.currentConcurrency += diff

	// 边界检查
	if s.currentConcurrency < s.config.MinConcurrency {
		s.currentConcurrency = s.config.MinConcurrency
	}
	if s.currentConcurrency > s.config.MaxConcurrency {
		s.currentConcurrency = s.config.MaxConcurrency
	}

	// 更新统计
	s.stats.CurrentMode = newMode.String()
	s.stats.CurrentConcurrency = s.currentConcurrency
	s.stats.LastAdjustTime = now

	// 记录变化
	if oldMode != newMode || oldConcurrency != s.currentConcurrency {
		s.log("INFO", "Scheduler adjusted: mode %s->%s, concurrency %d->%d (CPU:%.1f%%, Mem:%.1f%%)",
			oldMode, newMode, oldConcurrency, s.currentConcurrency, s.smoothedCPU, s.smoothedMem)
	}
}

// determineMode 确定调度模式
func (s *AdaptiveScheduler) determineMode() ScheduleMode {
	cpu := s.smoothedCPU
	mem := s.smoothedMem

	// 危急模式：任一资源超过危急阈值
	if cpu >= s.config.CPUCriticalThreshold || mem >= s.config.MemCriticalThreshold {
		return ModeCritical
	}

	// 保守模式：任一资源超过高阈值
	if cpu >= s.config.CPUHighThreshold || mem >= s.config.MemHighThreshold {
		return ModeConservative
	}

	// 激进模式：两个资源都低于低阈值
	if cpu < s.config.CPULowThreshold && mem < s.config.MemLowThreshold {
		return ModeAggressive
	}

	// 正常模式
	return ModeNormal
}

// calculateTargetConcurrency 计算目标并发数
func (s *AdaptiveScheduler) calculateTargetConcurrency(mode ScheduleMode) int {
	base := s.config.BaseConcurrency

	switch mode {
	case ModeAggressive:
		return base // 100%
	case ModeNormal:
		return int(float64(base) * 0.75) // 75%
	case ModeConservative:
		return int(float64(base) * 0.5) // 50%
	case ModeCritical:
		return int(float64(base) * 0.25) // 25%
	default:
		return base
	}
}

// CanAcceptTask 检查是否可以接受新任务
func (s *AdaptiveScheduler) CanAcceptTask() bool {
	s.mu.RLock()
	mode := s.currentMode
	maxConcurrency := s.currentConcurrency
	s.mu.RUnlock()

	currentTasks := int(atomic.LoadInt32(&s.currentTasks))

	// 危急模式下，如果已有任务在执行，拒绝新任务
	if mode == ModeCritical && currentTasks > 0 {
		atomic.AddInt64(&s.stats.TotalTasksRejected, 1)
		return false
	}

	// 检查并发数
	if currentTasks >= maxConcurrency {
		atomic.AddInt64(&s.stats.TotalTasksRejected, 1)
		return false
	}

	// 实时检查资源（快速路径）
	if s.isResourceCritical() {
		atomic.AddInt64(&s.stats.TotalTasksRejected, 1)
		atomic.AddInt64(&s.stats.TotalThrottles, 1)
		s.mu.Lock()
		s.stats.LastThrottleTime = time.Now()
		s.mu.Unlock()
		return false
	}

	atomic.AddInt64(&s.stats.TotalTasksAccepted, 1)
	return true
}

// isResourceCritical 快速检查资源是否处于危急状态
func (s *AdaptiveScheduler) isResourceCritical() bool {
	// 使用缓存的平滑值进行快速判断
	s.mu.RLock()
	cpu := s.smoothedCPU
	mem := s.smoothedMem
	s.mu.RUnlock()

	return cpu >= s.config.CPUCriticalThreshold || mem >= s.config.MemCriticalThreshold
}

// AcquireSlot 获取任务槽位
func (s *AdaptiveScheduler) AcquireSlot() bool {
	if !s.CanAcceptTask() {
		return false
	}
	atomic.AddInt32(&s.currentTasks, 1)
	return true
}

// ReleaseSlot 释放任务槽位
func (s *AdaptiveScheduler) ReleaseSlot() {
	if atomic.LoadInt32(&s.currentTasks) > 0 {
		atomic.AddInt32(&s.currentTasks, -1)
	}
}

// GetPullInterval 获取当前建议的任务拉取间隔
func (s *AdaptiveScheduler) GetPullInterval() time.Duration {
	s.mu.RLock()
	mode := s.currentMode
	currentTasks := atomic.LoadInt32(&s.currentTasks)
	maxConcurrency := s.currentConcurrency
	s.mu.RUnlock()

	// 基础间隔
	baseInterval := s.config.MinPullInterval

	// 根据模式调整
	switch mode {
	case ModeAggressive:
		baseInterval = s.config.MinPullInterval
	case ModeNormal:
		baseInterval = s.config.MinPullInterval * 2
	case ModeConservative:
		baseInterval = s.config.MinPullInterval * 4
	case ModeCritical:
		baseInterval = s.config.MaxPullInterval
	}

	// 根据负载调整
	loadRatio := float64(currentTasks) / float64(maxConcurrency)
	if loadRatio > 0.8 {
		// 高负载时增加间隔
		baseInterval = time.Duration(float64(baseInterval) * (1 + loadRatio))
	} else if loadRatio < 0.2 && int(currentTasks) < maxConcurrency {
		// 低负载且有空闲槽位时减少间隔
		baseInterval = time.Duration(float64(baseInterval) * 0.5)
	}

	// 边界检查
	if baseInterval < s.config.MinPullInterval {
		baseInterval = s.config.MinPullInterval
	}
	if baseInterval > s.config.MaxPullInterval {
		baseInterval = s.config.MaxPullInterval
	}

	// 更新统计
	s.mu.Lock()
	s.stats.PullInterval = int64(baseInterval / time.Millisecond)
	s.mu.Unlock()

	return baseInterval
}

// GetScannerConfig 获取扫描器配置建议
func (s *AdaptiveScheduler) GetScannerConfig() ScannerConfigRecommendation {
	s.mu.RLock()
	mode := s.currentMode
	cpu := s.smoothedCPU
	mem := s.smoothedMem
	s.mu.RUnlock()

	config := ScannerConfigRecommendation{}

	// 根据模式和资源状况计算推荐配置
	switch mode {
	case ModeAggressive:
		config.NaabuRate = 3000
		config.NaabuWorkers = 50
		config.NucleiConcurrency = 25
		config.NucleiRateLimit = 150
		config.FingerprintConcurrency = 20
	case ModeNormal:
		config.NaabuRate = 2000
		config.NaabuWorkers = 30
		config.NucleiConcurrency = 15
		config.NucleiRateLimit = 100
		config.FingerprintConcurrency = 10
	case ModeConservative:
		config.NaabuRate = 1000
		config.NaabuWorkers = 20
		config.NucleiConcurrency = 10
		config.NucleiRateLimit = 50
		config.FingerprintConcurrency = 5
	case ModeCritical:
		config.NaabuRate = 500
		config.NaabuWorkers = 10
		config.NucleiConcurrency = 5
		config.NucleiRateLimit = 20
		config.FingerprintConcurrency = 3
	}

	// 根据内存进一步调整
	if mem > 80 {
		config.NaabuWorkers = int(float64(config.NaabuWorkers) * 0.5)
		config.NucleiConcurrency = int(float64(config.NucleiConcurrency) * 0.5)
	}

	// 根据CPU进一步调整
	if cpu > 80 {
		config.NaabuRate = int(float64(config.NaabuRate) * 0.5)
		config.NucleiRateLimit = int(float64(config.NucleiRateLimit) * 0.5)
	}

	return config
}

// ScannerConfigRecommendation 扫描器配置建议
type ScannerConfigRecommendation struct {
	NaabuRate              int // Naabu 每秒发包数
	NaabuWorkers           int // Naabu 工作线程数
	NucleiConcurrency      int // Nuclei 并发数
	NucleiRateLimit        int // Nuclei 速率限制
	FingerprintConcurrency int // 指纹识别并发数
}

// GetCurrentMode 获取当前调度模式
func (s *AdaptiveScheduler) GetCurrentMode() ScheduleMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentMode
}

// GetCurrentConcurrency 获取当前并发数
func (s *AdaptiveScheduler) GetCurrentConcurrency() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentConcurrency
}

// GetCurrentTasks 获取当前任务数
func (s *AdaptiveScheduler) GetCurrentTasks() int {
	return int(atomic.LoadInt32(&s.currentTasks))
}

// GetStats 获取统计信息
func (s *AdaptiveScheduler) GetStats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := s.stats
	stats.CurrentConcurrency = s.currentConcurrency
	stats.CurrentMode = s.currentMode.String()
	return stats
}

// GetResourceStatus 获取资源状态（兼容旧接口）
func (s *AdaptiveScheduler) GetResourceStatus() ResourceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return ResourceStatus{
		CurrentTasks:   int(atomic.LoadInt32(&s.currentTasks)),
		MaxConcurrency: s.currentConcurrency,
		AvailableSlots: s.currentConcurrency - int(atomic.LoadInt32(&s.currentTasks)),
		CPUPercent:     s.smoothedCPU,
		MemPercent:     s.smoothedMem,
		IsThrottled:    s.currentMode == ModeCritical,
		OverloadCount:  0,
	}
}

// SetMaxConcurrency 动态设置最大并发数
func (s *AdaptiveScheduler) SetMaxConcurrency(maxConcurrency int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxConcurrency > 0 {
		s.config.BaseConcurrency = maxConcurrency
		s.config.MaxConcurrency = maxConcurrency
		// 如果当前并发数超过新的最大值，立即调整
		if s.currentConcurrency > maxConcurrency {
			s.currentConcurrency = maxConcurrency
		}
	}
}

// AvailableSlots 获取可用槽位数
func (s *AdaptiveScheduler) AvailableSlots() int {
	s.mu.RLock()
	maxConcurrency := s.currentConcurrency
	s.mu.RUnlock()

	currentTasks := int(atomic.LoadInt32(&s.currentTasks))
	available := maxConcurrency - currentTasks
	if available < 0 {
		return 0
	}
	return available
}

// CurrentTasks 获取当前任务数（兼容旧接口）
func (s *AdaptiveScheduler) CurrentTasks() int {
	return int(atomic.LoadInt32(&s.currentTasks))
}

// IsThrottled 检查是否处于限流状态
func (s *AdaptiveScheduler) IsThrottled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentMode == ModeCritical
}
