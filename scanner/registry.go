package scanner

import (
	"fmt"
	"sync"
)

// ScannerFactory 扫描器工厂函数
type ScannerFactory func(config *ScannerRegistryConfig) (Scanner, error)

// ScannerRegistryConfig 扫描器配置
type ScannerRegistryConfig struct {
	Timeout     int                    `json:"timeout"`
	Concurrency int                    `json:"concurrency"`
	RateLimit   int                    `json:"rateLimit"`
	Extra       map[string]interface{} `json:"extra"`
}

// ScannerRegistry 扫描器注册表
type ScannerRegistry struct {
	factories map[string]ScannerFactory
	instances sync.Map
	mu        sync.RWMutex
}

var (
	defaultRegistry *ScannerRegistry
	registryOnce    sync.Once
)

// DefaultRegistry 获取默认注册表
func DefaultRegistry() *ScannerRegistry {
	registryOnce.Do(func() {
		defaultRegistry = NewScannerRegistry()
		// 注册内置扫描器
		defaultRegistry.RegisterBuiltins()
	})
	return defaultRegistry
}

// NewScannerRegistry 创建扫描器注册表
func NewScannerRegistry() *ScannerRegistry {
	return &ScannerRegistry{
		factories: make(map[string]ScannerFactory),
	}
}

// Register 注册扫描器工厂
func (r *ScannerRegistry) Register(name string, factory ScannerFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// Get 获取扫描器实例
func (r *ScannerRegistry) Get(name string) (Scanner, error) {
	return r.GetWithConfig(name, nil)
}

// GetWithConfig 获取或创建带配置的扫描器实例
func (r *ScannerRegistry) GetWithConfig(name string, config *ScannerRegistryConfig) (Scanner, error) {
	// 生成缓存键
	cacheKey := name
	if config != nil {
		cacheKey = fmt.Sprintf("%s_%d_%d", name, config.Timeout, config.Concurrency)
	}

	// 先检查缓存
	if cached, ok := r.instances.Load(cacheKey); ok {
		return cached.(Scanner), nil
	}

	// 创建新实例
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("scanner %s not registered", name)
	}

	scanner, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf("create scanner %s: %w", name, err)
	}

	r.instances.Store(cacheKey, scanner)
	return scanner, nil
}

// List 列出所有已注册的扫描器
func (r *ScannerRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// Has 检查扫描器是否已注册
func (r *ScannerRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[name]
	return ok
}

// Unregister 取消注册扫描器
func (r *ScannerRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.factories, name)
	r.instances.Delete(name)
}

// Clear 清空注册表
func (r *ScannerRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories = make(map[string]ScannerFactory)
	r.instances = sync.Map{}
}

// ClearInstances 清空实例缓存
func (r *ScannerRegistry) ClearInstances() {
	r.instances = sync.Map{}
}

// RegisterBuiltins 注册内置扫描器
func (r *ScannerRegistry) RegisterBuiltins() {
	// Nuclei 扫描器
	r.Register("nuclei", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewNucleiScanner(), nil
	})

	// Naabu 扫描器
	r.Register("naabu", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewNaabuScanner(), nil
	})

	// Subfinder 扫描器
	r.Register("subfinder", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewSubfinderScanner(), nil
	})

	// 指纹扫描器
	r.Register("fingerprint", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewFingerprintScanner(), nil
	})

	// 端口扫描器
	r.Register("portscan", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewPortScanner(), nil
	})

	// 域名扫描器
	r.Register("domainscan", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewDomainScanner(), nil
	})

	// Nmap 扫描器
	r.Register("nmap", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewNmapScanner(), nil
	})

	// Masscan 扫描器
	r.Register("masscan", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewMasscanScanner(), nil
	})

	// URL Finder 扫描器
	r.Register("urlfinder", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewURLFinderScanner(), nil
	})

	// 子域名爆破扫描器
	r.Register("subdomain_bruteforce", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewSubdomainBruteforceScanner(), nil
	})

	// Fingerprintx 扫描器
	r.Register("fingerprintx", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewFingerprintxScanner(), nil
	})

	// 弱口令扫描器
	r.Register("brutescan", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewBruteScanScanner(), nil
	})

	// JSFinder 扫描器
	r.Register("jsfinder", func(cfg *ScannerRegistryConfig) (Scanner, error) {
		return NewJSFinderScanner(), nil
	})
}

// ScannerInfo 扫描器信息
type ScannerInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Registered  bool   `json:"registered"`
}

// GetScannerInfo 获取扫描器信息
func (r *ScannerRegistry) GetScannerInfo(name string) *ScannerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, registered := r.factories[name]

	return &ScannerInfo{
		Name:       name,
		Registered: registered,
	}
}

// GetAllScannerInfo 获取所有扫描器信息
func (r *ScannerRegistry) GetAllScannerInfo() []*ScannerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]*ScannerInfo, 0, len(r.factories))
	for name := range r.factories {
		infos = append(infos, &ScannerInfo{
			Name:       name,
			Registered: true,
		})
	}
	return infos
}
