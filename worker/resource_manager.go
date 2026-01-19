package worker

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// ResourceManagerConfig 资源管理器配置
type ResourceManagerConfig struct {
	MaxConcurrency       int           // 最大并发数
	CPUThreshold         float64       // CPU使用率阈值 (0-100)
	MemThreshold         float64       // 内存使用率阈值 (0-100)
	CPURecoveryThreshold float64       // CPU恢复阈值
	CheckInterval        time.Duration // 资源检查间隔
	ThrottleDuration     time.Duration // 限流持续时间
	OverloadThreshold    int           // 连续过载次数阈值
}

// DefaultResourceManagerConfig 默认资源管理器配置
func DefaultResourceManagerConfig(maxConcurrency int) ResourceManagerConfig {
	return ResourceManagerConfig{
		MaxConcurrency:       maxConcurrency,
		CPUThreshold:         50.0,
		MemThreshold:         55.0,
		CPURecoveryThreshold: 50.0,
		CheckInterval:        2 * time.Second,
		ThrottleDuration:     60 * time.Second,
		OverloadThreshold:    2,
	}
}

// ResourceManager 资源管理器
// 负责管理Worker的并发槽位和系统资源监控
type ResourceManager struct {
	config ResourceManagerConfig

	mu           sync.Mutex
	currentTasks int       // 当前正在执行的任务数
	throttled    bool      // 是否处于限流状态
	throttleUntil time.Time // 限流结束时间

	// 资源监控状态
	lastCheck      time.Time // 上次资源检查时间
	overloadCount  int       // 连续过载计数
}

// NewResourceManager 创建资源管理器
func NewResourceManager(maxConcurrency int) *ResourceManager {
	return NewResourceManagerWithConfig(DefaultResourceManagerConfig(maxConcurrency))
}

// NewResourceManagerWithConfig 使用自定义配置创建资源管理器
func NewResourceManagerWithConfig(config ResourceManagerConfig) *ResourceManager {
	return &ResourceManager{
		config: config,
	}
}


// CanAcceptTask 检查是否可以接受新任务
// 综合考虑并发槽位和系统资源状态
func (m *ResourceManager) CanAcceptTask() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查并发数
	if m.currentTasks >= m.config.MaxConcurrency {
		return false
	}

	// 检查是否在限流期
	if m.throttled && time.Now().Before(m.throttleUntil) {
		return false
	}

	// 限流期结束，重置状态
	if m.throttled && time.Now().After(m.throttleUntil) {
		m.throttled = false
		m.overloadCount = 0
	}

	// 检查系统资源（带频率限制）
	if time.Since(m.lastCheck) >= m.config.CheckInterval {
		m.lastCheck = time.Now()
		if m.isOverloadedLocked() {
			m.overloadCount++
			if m.overloadCount >= m.config.OverloadThreshold {
				m.throttled = true
				m.throttleUntil = time.Now().Add(m.config.ThrottleDuration)
			}
			return false
		}
		// 资源恢复正常，重置过载计数
		if m.overloadCount > 0 && m.isRecoveredLocked() {
			m.overloadCount = 0
		}
	}

	return true
}

// AcquireSlot 获取任务槽位
// 返回 true 表示成功获取槽位，false 表示没有可用槽位
func (m *ResourceManager) AcquireSlot() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentTasks >= m.config.MaxConcurrency {
		return false
	}
	m.currentTasks++
	return true
}

// ReleaseSlot 释放任务槽位
func (m *ResourceManager) ReleaseSlot() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentTasks > 0 {
		m.currentTasks--
	}
}

// CurrentTasks 获取当前任务数
func (m *ResourceManager) CurrentTasks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentTasks
}

// AvailableSlots 获取可用槽位数
func (m *ResourceManager) AvailableSlots() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	available := m.config.MaxConcurrency - m.currentTasks
	if available < 0 {
		return 0
	}
	return available
}

// IsThrottled 检查是否处于限流状态
func (m *ResourceManager) IsThrottled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.throttled && time.Now().Before(m.throttleUntil)
}

// GetResourceStatus 获取资源状态信息
func (m *ResourceManager) GetResourceStatus() ResourceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	cpuPercent := getCPUPercent()
	memPercent := getMemPercent()

	return ResourceStatus{
		CurrentTasks:   m.currentTasks,
		MaxConcurrency: m.config.MaxConcurrency,
		AvailableSlots: m.config.MaxConcurrency - m.currentTasks,
		CPUPercent:     cpuPercent,
		MemPercent:     memPercent,
		IsThrottled:    m.throttled && time.Now().Before(m.throttleUntil),
		OverloadCount:  m.overloadCount,
	}
}

// ResourceStatus 资源状态
type ResourceStatus struct {
	CurrentTasks   int     `json:"currentTasks"`
	MaxConcurrency int     `json:"maxConcurrency"`
	AvailableSlots int     `json:"availableSlots"`
	CPUPercent     float64 `json:"cpuPercent"`
	MemPercent     float64 `json:"memPercent"`
	IsThrottled    bool    `json:"isThrottled"`
	OverloadCount  int     `json:"overloadCount"`
}


// isOverloadedLocked 检查系统是否过载（内部方法，需要持有锁）
func (m *ResourceManager) isOverloadedLocked() bool {
	cpuPercent := getCPUPercent()
	if cpuPercent >= m.config.CPUThreshold {
		return true
	}

	memPercent := getMemPercent()
	if memPercent >= m.config.MemThreshold {
		return true
	}

	return false
}

// isRecoveredLocked 检查系统是否已恢复（内部方法，需要持有锁）
func (m *ResourceManager) isRecoveredLocked() bool {
	cpuPercent := getCPUPercent()
	return cpuPercent < m.config.CPURecoveryThreshold
}

// getCPUPercent 获取CPU使用率
func getCPUPercent() float64 {
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil || len(cpuPercent) == 0 {
		return 0
	}
	return cpuPercent[0]
}

// getMemPercent 获取内存使用率
func getMemPercent() float64 {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return 0
	}
	return memInfo.UsedPercent
}

// SetMaxConcurrency 动态设置最大并发数
func (m *ResourceManager) SetMaxConcurrency(maxConcurrency int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxConcurrency > 0 {
		m.config.MaxConcurrency = maxConcurrency
	}
}

// SetCPUThreshold 动态设置CPU阈值
func (m *ResourceManager) SetCPUThreshold(threshold float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if threshold > 0 && threshold <= 100 {
		m.config.CPUThreshold = threshold
	}
}

// SetMemThreshold 动态设置内存阈值
func (m *ResourceManager) SetMemThreshold(threshold float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if threshold > 0 && threshold <= 100 {
		m.config.MemThreshold = threshold
	}
}

// ResetThrottle 重置限流状态（用于测试或手动恢复）
func (m *ResourceManager) ResetThrottle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.throttled = false
	m.overloadCount = 0
	m.throttleUntil = time.Time{}
}
