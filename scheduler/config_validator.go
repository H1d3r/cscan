package scheduler

import (
	"errors"

	"cscan/pkg/xerr"
)

// ConfigValidator 配置验证器
// 用于验证任务配置的必填字段和应用默认值
type ConfigValidator struct{}

// NewConfigValidator 创建配置验证器
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{}
}

// Validate 验证任务配置
// 返回所有验证错误的组合
func (v *ConfigValidator) Validate(config *TaskConfig) error {
	if config == nil {
		return xerr.NewConfigError("config", nil, "config cannot be nil")
	}

	var errs []error

	if config.PortScan != nil && config.PortScan.Enable {
		if err := v.validatePortScan(config.PortScan); err != nil {
			errs = append(errs, err)
		}
	}

	if config.PortIdentify != nil && config.PortIdentify.Enable {
		if err := v.validatePortIdentify(config.PortIdentify); err != nil {
			errs = append(errs, err)
		}
	}

	if config.DomainScan != nil && config.DomainScan.Enable {
		if err := v.validateDomainScan(config.DomainScan); err != nil {
			errs = append(errs, err)
		}
	}

	if config.Fingerprint != nil && config.Fingerprint.Enable {
		if err := v.validateFingerprint(config.Fingerprint); err != nil {
			errs = append(errs, err)
		}
	}

	if config.PocScan != nil && config.PocScan.Enable {
		if err := v.validatePocScan(config.PocScan); err != nil {
			errs = append(errs, err)
		}
	}

	if config.DirScan != nil && config.DirScan.Enable {
		if err := v.validateDirScan(config.DirScan); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validatePortScan 验证端口扫描配置
func (v *ConfigValidator) validatePortScan(config *PortScanConfig) error {
	var errs []error

	if config.Ports == "" {
		errs = append(errs, xerr.NewConfigError("portscan.ports", "", "ports cannot be empty when port scan is enabled"))
	}

	if config.Rate < 0 {
		errs = append(errs, xerr.NewConfigError("portscan.rate", config.Rate, "rate must be non-negative"))
	}

	if config.Timeout < 0 {
		errs = append(errs, xerr.NewConfigError("portscan.timeout", config.Timeout, "timeout must be non-negative"))
	}

	if config.PortThreshold < 0 {
		errs = append(errs, xerr.NewConfigError("portscan.portThreshold", config.PortThreshold, "portThreshold must be non-negative"))
	}

	if config.Tool != "" && config.Tool != "naabu" && config.Tool != "masscan" && config.Tool != "tcp" {
		errs = append(errs, xerr.NewConfigError("portscan.tool", config.Tool, "invalid tool, must be one of: naabu, masscan, tcp"))
	}

	if config.ScanType != "" && config.ScanType != "s" && config.ScanType != "c" {
		errs = append(errs, xerr.NewConfigError("portscan.scanType", config.ScanType, "invalid scanType, must be 's' (SYN) or 'c' (CONNECT)"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validatePortIdentify 验证端口识别配置
func (v *ConfigValidator) validatePortIdentify(config *PortIdentifyConfig) error {
	var errs []error

	if config.Timeout < 0 {
		errs = append(errs, xerr.NewConfigError("portidentify.timeout", config.Timeout, "timeout must be non-negative"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validateDomainScan 验证域名扫描配置
func (v *ConfigValidator) validateDomainScan(config *DomainScanConfig) error {
	var errs []error

	if config.Timeout < 0 {
		errs = append(errs, xerr.NewConfigError("domainscan.timeout", config.Timeout, "timeout must be non-negative"))
	}

	if config.MaxEnumerationTime < 0 {
		errs = append(errs, xerr.NewConfigError("domainscan.maxEnumerationTime", config.MaxEnumerationTime, "maxEnumerationTime must be non-negative"))
	}

	if config.Threads < 0 {
		errs = append(errs, xerr.NewConfigError("domainscan.threads", config.Threads, "threads must be non-negative"))
	}

	if config.RateLimit < 0 {
		errs = append(errs, xerr.NewConfigError("domainscan.rateLimit", config.RateLimit, "rateLimit must be non-negative"))
	}

	if config.Concurrent < 0 {
		errs = append(errs, xerr.NewConfigError("domainscan.concurrent", config.Concurrent, "concurrent must be non-negative"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validateFingerprint 验证指纹识别配置
func (v *ConfigValidator) validateFingerprint(config *FingerprintConfig) error {
	var errs []error

	if config.Tool != "" && config.Tool != "httpx" && config.Tool != "builtin" {
		errs = append(errs, xerr.NewConfigError("fingerprint.tool", config.Tool, "invalid tool, must be 'httpx' or 'builtin'"))
	}

	if config.ActiveTimeout < 0 {
		errs = append(errs, xerr.NewConfigError("fingerprint.activeTimeout", config.ActiveTimeout, "activeTimeout must be non-negative"))
	}

	if config.Timeout < 0 {
		errs = append(errs, xerr.NewConfigError("fingerprint.timeout", config.Timeout, "timeout must be non-negative"))
	}

	if config.TargetTimeout < 0 {
		errs = append(errs, xerr.NewConfigError("fingerprint.targetTimeout", config.TargetTimeout, "targetTimeout must be non-negative"))
	}

	if config.Concurrency < 0 {
		errs = append(errs, xerr.NewConfigError("fingerprint.concurrency", config.Concurrency, "concurrency must be non-negative"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validatePocScan 验证POC扫描配置
func (v *ConfigValidator) validatePocScan(config *PocScanConfig) error {
	var errs []error

	if config.RateLimit < 0 {
		errs = append(errs, xerr.NewConfigError("pocscan.rateLimit", config.RateLimit, "rateLimit must be non-negative"))
	}

	if config.Concurrency < 0 {
		errs = append(errs, xerr.NewConfigError("pocscan.concurrency", config.Concurrency, "concurrency must be non-negative"))
	}

	if config.TargetTimeout < 0 {
		errs = append(errs, xerr.NewConfigError("pocscan.targetTimeout", config.TargetTimeout, "targetTimeout must be non-negative"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validateDirScan 验证目录扫描配置
func (v *ConfigValidator) validateDirScan(config *DirScanConfig) error {
	var errs []error

	if config.Threads < 0 {
		errs = append(errs, xerr.NewConfigError("dirscan.threads", config.Threads, "threads must be non-negative"))
	}

	if config.Timeout < 0 {
		errs = append(errs, xerr.NewConfigError("dirscan.timeout", config.Timeout, "timeout must be non-negative"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// ApplyDefaults 应用默认值
// 在配置解析时应用默认值，而不是在执行时
func (v *ConfigValidator) ApplyDefaults(config *TaskConfig) {
	if config == nil {
		return
	}

	if config.PortScan != nil && config.PortScan.Enable {
		v.applyPortScanDefaults(config.PortScan)
	}

	if config.PortIdentify != nil && config.PortIdentify.Enable {
		v.applyPortIdentifyDefaults(config.PortIdentify)
	}

	if config.DomainScan != nil && config.DomainScan.Enable {
		v.applyDomainScanDefaults(config.DomainScan)
	}

	if config.Fingerprint != nil && config.Fingerprint.Enable {
		v.applyFingerprintDefaults(config.Fingerprint)
	}

	if config.PocScan != nil && config.PocScan.Enable {
		v.applyPocScanDefaults(config.PocScan)
	}

	if config.DirScan != nil && config.DirScan.Enable {
		v.applyDirScanDefaults(config.DirScan)
	}
}

// applyPortScanDefaults 应用端口扫描默认值
func (v *ConfigValidator) applyPortScanDefaults(config *PortScanConfig) {
	if config.Ports == "" {
		config.Ports = "80,443,8080"
	}
	if config.Rate == 0 {
		config.Rate = 1000
	}
	if config.Timeout == 0 {
		config.Timeout = 5
	}
	if config.Tool == "" {
		config.Tool = "naabu"
	}
	if config.ScanType == "" {
		config.ScanType = "c"
	}
}

// applyPortIdentifyDefaults 应用端口识别默认值
func (v *ConfigValidator) applyPortIdentifyDefaults(config *PortIdentifyConfig) {
	if config.Timeout == 0 {
		config.Timeout = 30
	}
}

// applyDomainScanDefaults 应用域名扫描默认值
func (v *ConfigValidator) applyDomainScanDefaults(config *DomainScanConfig) {
	if config.Timeout == 0 {
		config.Timeout = 300
	}
	if config.MaxEnumerationTime == 0 {
		config.MaxEnumerationTime = 10
	}
	if config.Threads == 0 {
		config.Threads = 10
	}
	if config.RateLimit == 0 {
		config.RateLimit = 100
	}
	if config.Concurrent == 0 {
		config.Concurrent = 100
	}
}

// applyFingerprintDefaults 应用指纹识别默认值
func (v *ConfigValidator) applyFingerprintDefaults(config *FingerprintConfig) {
	if config.Tool == "" {
		config.Tool = "httpx"
	}
	if config.ActiveTimeout == 0 {
		config.ActiveTimeout = 10
	}
	if config.Timeout == 0 {
		config.Timeout = 300
	}
	if config.TargetTimeout == 0 {
		config.TargetTimeout = 30
	}
	if config.Concurrency == 0 {
		config.Concurrency = 10
	}
}

// applyPocScanDefaults 应用POC扫描默认值
func (v *ConfigValidator) applyPocScanDefaults(config *PocScanConfig) {
	if config.Concurrency == 0 {
		config.Concurrency = 25
	}
	if config.TargetTimeout == 0 {
		config.TargetTimeout = 600
	}
	if config.RateLimit == 0 {
		config.RateLimit = 150
	}
}

// applyDirScanDefaults 应用目录扫描默认值
func (v *ConfigValidator) applyDirScanDefaults(config *DirScanConfig) {
	if config.Threads == 0 {
		config.Threads = 10
	}
	if config.Timeout == 0 {
		config.Timeout = 10
	}
}

// ValidateAndApplyDefaults 验证配置并应用默认值
// 这是一个便捷方法，先应用默认值再验证
func (v *ConfigValidator) ValidateAndApplyDefaults(config *TaskConfig) error {
	v.ApplyDefaults(config)
	return v.Validate(config)
}

// ParseAndValidate 解析配置字符串并验证
// 返回验证后的配置和可能的错误
func (v *ConfigValidator) ParseAndValidate(configStr string) (*TaskConfig, error) {
	config, err := ParseTaskConfig(configStr)
	if err != nil {
		return nil, xerr.NewConfigError("config", configStr, "failed to parse config: "+err.Error())
	}

	v.ApplyDefaults(config)

	if err := v.Validate(config); err != nil {
		return nil, err
	}

	return config, nil
}
