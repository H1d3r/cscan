package scanner

import (
	"runtime"

	"github.com/shirou/gopsutil/v3/mem"
	"github.com/zeromicro/go-zero/core/logx"
)

// SystemProfile 系统硬件配置档位
type SystemProfile int

const (
	ProfileLow    SystemProfile = iota // 低配: <=4核 或 <=4GB
	ProfileMedium                      // 中配: <=8核 且 <=16GB
	ProfileHigh                        // 高配: >8核 或 >16GB
)

func (p SystemProfile) String() string {
	switch p {
	case ProfileLow:
		return "low"
	case ProfileMedium:
		return "medium"
	case ProfileHigh:
		return "high"
	default:
		return "unknown"
	}
}

// AdaptiveScanConfig 自适应扫描配置
// 根据系统硬件配置自动计算各扫描器的推荐参数
type AdaptiveScanConfig struct {
	Profile       SystemProfile // 系统配置档位
	CPUCores      int           // CPU 核心数
	TotalMemoryMB uint64        // 总内存 (MB)

	// Naabu 推荐参数
	NaabuRate    int // 每秒发送包数
	NaabuWorkers int // 内部工作线程数
	NaabuRetries int // 重试次数

	// Nuclei 推荐参数
	NucleiConcurrency int // 模板/主机并发数
	NucleiRateLimit   int // 速率限制
	NucleiRetries     int // 重试次数

	// Fingerprint 推荐参数
	FingerprintConcurrency int // 并发数
	FingerprintTimeout     int // 总超时 (秒)
	FingerprintTargetTmout int // 单目标超时 (秒)
}

// DetectSystemProfile 检测当前系统硬件配置档位
func DetectSystemProfile() SystemProfile {
	cpuCores := runtime.NumCPU()
	totalMemMB := getSystemTotalMemMB()

	if cpuCores <= 4 || totalMemMB <= 4096 {
		return ProfileLow
	}
	if cpuCores <= 8 && totalMemMB <= 16384 {
		return ProfileMedium
	}
	return ProfileHigh
}

// GetAdaptiveScanConfig 获取基于系统硬件的自适应扫描配置
func GetAdaptiveScanConfig() *AdaptiveScanConfig {
	cpuCores := runtime.NumCPU()
	totalMemMB := getSystemTotalMemMB()
	profile := DetectSystemProfile()

	config := &AdaptiveScanConfig{
		Profile:       profile,
		CPUCores:      cpuCores,
		TotalMemoryMB: totalMemMB,
	}

	switch profile {
	case ProfileLow:
		// 低配 (<=4核 或 <=4GB): 大幅降低并发，避免资源耗尽
		config.NaabuRate = 500
		config.NaabuWorkers = 10
		config.NaabuRetries = 1
		config.NucleiConcurrency = 5
		config.NucleiRateLimit = 50
		config.NucleiRetries = 1
		config.FingerprintConcurrency = 3
		config.FingerprintTimeout = 600
		config.FingerprintTargetTmout = 20
	case ProfileMedium:
		// 中配 (<=8核 且 <=16GB): 适度调整
		config.NaabuRate = 1500
		config.NaabuWorkers = 25
		config.NaabuRetries = 2
		config.NucleiConcurrency = 15
		config.NucleiRateLimit = 100
		config.NucleiRetries = 1
		config.FingerprintConcurrency = 5
		config.FingerprintTimeout = 300
		config.FingerprintTargetTmout = 30
	case ProfileHigh:
		// 高配 (>8核 或 >16GB): 使用原始高性能默认值
		config.NaabuRate = 3000
		config.NaabuWorkers = 50
		config.NaabuRetries = 2
		config.NucleiConcurrency = 25
		config.NucleiRateLimit = 150
		config.NucleiRetries = 1
		config.FingerprintConcurrency = 5
		config.FingerprintTimeout = 300
		config.FingerprintTargetTmout = 30
	}

	logx.Infof("[AdaptiveConfig] System profile: %s (CPU: %d cores, Mem: %d MB)", profile, cpuCores, totalMemMB)
	logx.Infof("[AdaptiveConfig] Naabu: rate=%d, workers=%d, retries=%d", config.NaabuRate, config.NaabuWorkers, config.NaabuRetries)
	logx.Infof("[AdaptiveConfig] Nuclei: concurrency=%d, rateLimit=%d", config.NucleiConcurrency, config.NucleiRateLimit)
	logx.Infof("[AdaptiveConfig] Fingerprint: concurrency=%d, timeout=%ds, targetTimeout=%ds", config.FingerprintConcurrency, config.FingerprintTimeout, config.FingerprintTargetTmout)

	return config
}

// getSystemTotalMemMB 获取系统总内存 (MB)
func getSystemTotalMemMB() uint64 {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return 0
	}
	return memInfo.Total / 1024 / 1024
}

// 全局自适应配置实例（懒加载）
var globalAdaptiveConfig *AdaptiveScanConfig

// GetGlobalAdaptiveConfig 获取全局自适应配置（单例，首次调用时初始化）
func GetGlobalAdaptiveConfig() *AdaptiveScanConfig {
	if globalAdaptiveConfig == nil {
		globalAdaptiveConfig = GetAdaptiveScanConfig()
	}
	return globalAdaptiveConfig
}
