package geolocation

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
)

// Config 配置
type Config struct {
	Enabled      bool   `json:"enabled"`      // 是否启用
	DataDir      string `json:"dataDir"`      // 数据库目录（编译时准备好）
	AutoDownload bool   `json:"autoDownload"` // 是否自动下载（已废弃，数据库应在编译时准备好）
}

// DefaultConfig 默认配置
var DefaultConfig = Config{
	Enabled:      true,
	DataDir:      "data",
	AutoDownload: false, // 数据库在编译时准备好，不再运行时下载
}

// ProviderManager IP 地理位置服务管理器
type ProviderManager struct {
	provider Provider
	config   Config
	mu       sync.RWMutex
	once     sync.Once
}

var (
	defaultManager *ProviderManager
	managerOnce    sync.Once
)

// GetManager 获取全局管理器
func GetManager() *ProviderManager {
	managerOnce.Do(func() {
		defaultManager = &ProviderManager{
			config: DefaultConfig,
		}
	})
	return defaultManager
}

// Init 初始化服务
func (m *ProviderManager) Init(configPath string) error {
	return m.initWithConfig(configPath, DefaultConfig)
}

// InitWithConfig 使用配置初始化
func (m *ProviderManager) InitWithConfig(config Config) error {
	return m.initWithConfig("", config)
}

func (m *ProviderManager) initWithConfig(configPath string, config Config) error {
	var err error
	m.once.Do(func() {
		// 加载配置文件
		if configPath != "" {
			var loadedCfg Config
			if err := conf.LoadConfig(configPath, &loadedCfg); err != nil {
				logx.Slowf("load geolocation config from %s failed: %v", configPath, err)
			} else {
				config = loadedCfg
			}
		}

		m.mu.Lock()
		m.config = config
		m.mu.Unlock()

		// 如果禁用，直接返回
		if !config.Enabled {
			logx.Info("geolocation service is disabled")
			return
		}

		// 获取数据库路径
		dataDir := config.DataDir
		if dataDir == "" {
			dataDir = DefaultConfig.DataDir
		}

		v4Path := filepath.Join(dataDir, IPv4FileName)
		v6Path := filepath.Join(dataDir, IPv6FileName)

		// 检查数据库是否存在（数据库应在编译时准备好）
		if !fileExists(v4Path) {
			logx.Errorf("ip2region v4 database not found: %s, please download from https://github.com/lionsoul2014/ip2region/tree/master/data", v4Path)
			err = fmt.Errorf("%w: %s (请从 https://github.com/lionsoul2014/ip2region/tree/master/data 下载并放置到 data 目录)", ErrDatabaseNotFound, v4Path)
			return
		}

		// 创建服务
		provider, err := NewIp2RegionProvider(v4Path, v6Path)
		if err != nil {
			logx.Errorf("create ip2region provider failed: %v", err)
			err = fmt.Errorf("create ip2region provider failed: %w", err)
			return
		}

		m.mu.Lock()
		m.provider = provider
		m.mu.Unlock()

		logx.Info("geolocation service initialized successfully")
	})

	return err
}

// GetProvider 获取服务提供者
func (m *ProviderManager) GetProvider() Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}

// IsEnabled 检查服务是否启用
func (m *ProviderManager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider != nil
}

// Search 查询 IP 地址的地理位置
func (m *ProviderManager) Search(ip string) (string, error) {
	provider := m.GetProvider()
	if provider == nil {
		return "", ErrServiceNotInitialized
	}
	return provider.Search(ip)
}

// SearchWithDetail 查询并返回详细地理位置信息
func (m *ProviderManager) SearchWithDetail(ip string) (*Location, error) {
	provider := m.GetProvider()
	if provider == nil {
		return nil, ErrServiceNotInitialized
	}
	return provider.SearchWithDetail(ip)
}

// Close 关闭服务
func (m *ProviderManager) Close() error {
	provider := m.GetProvider()
	if provider == nil {
		return nil
	}
	return provider.Close()
}

// GetDataFilePath 获取数据文件路径
func GetDataFilePath(dataDir string) (v4Path string, v6Path string) {
	if dataDir == "" {
		dataDir = DefaultConfig.DataDir
	}
	return filepath.Join(dataDir, IPv4FileName), filepath.Join(dataDir, IPv6FileName)
}

// NormalizeLocation 标准化地理位置字符串
// 将 "中国|0|广东省|深圳市|电信" 转换为 "广东省深圳市-电信"
func NormalizeLocation(location string) string {
	if location == "" {
		return ""
	}

	// 解析 region
	country, region, city, isp := ParseRegion(location)

	// 如果是国家未知
	if country == "0" || country == "" {
		return ""
	}

	// 构建简化的地理位置字符串
	var result string

	// 添加省份/城市
	if region != "0" && region != "" {
		result += region
	}
	if city != "0" && city != "" {
		result += city
	}

	// 添加运营商
	if isp != "0" && isp != "" {
		if result != "" {
			result += "-"
		}
		result += isp
	}

	return result
}

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	// 尝试多个可能的配置路径
	paths := []string{
		"etc/geolocation.json",
		"./geolocation.json",
		"geolocation.json",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
