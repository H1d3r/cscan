package scheduler

import (
	"fmt"
	"strings"
)

// SimpleValidator 简单配置验证器
// 提供链式验证API，用于快速验证
type SimpleValidator struct {
	errors []string
}

// NewSimpleValidator 创建简单验证器
func NewSimpleValidator() *SimpleValidator {
	return &SimpleValidator{
		errors: make([]string, 0),
	}
}

// Required 必填字段验证
func (v *SimpleValidator) Required(field string, value interface{}) *SimpleValidator {
	if value == nil {
		v.errors = append(v.errors, fmt.Sprintf("%s is required", field))
		return v
	}

	switch val := value.(type) {
	case string:
		if strings.TrimSpace(val) == "" {
			v.errors = append(v.errors, fmt.Sprintf("%s is required", field))
		}
	case []string:
		if len(val) == 0 {
			v.errors = append(v.errors, fmt.Sprintf("%s is required", field))
		}
	case int:
		if val == 0 {
			v.errors = append(v.errors, fmt.Sprintf("%s is required", field))
		}
	}
	return v
}

// Range 范围验证
func (v *SimpleValidator) Range(field string, value, min, max int) *SimpleValidator {
	if value < min || value > max {
		v.errors = append(v.errors, fmt.Sprintf("%s must be between %d and %d, got %d", field, min, max, value))
	}
	return v
}

// Min 最小值验证
func (v *SimpleValidator) Min(field string, value, min int) *SimpleValidator {
	if value < min {
		v.errors = append(v.errors, fmt.Sprintf("%s must be at least %d, got %d", field, min, value))
	}
	return v
}

// Max 最大值验证
func (v *SimpleValidator) Max(field string, value, max int) *SimpleValidator {
	if value > max {
		v.errors = append(v.errors, fmt.Sprintf("%s must be at most %d, got %d", field, max, value))
	}
	return v
}

// Positive 正数验证
func (v *SimpleValidator) Positive(field string, value int) *SimpleValidator {
	if value <= 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s must be positive, got %d", field, value))
	}
	return v
}

// NonNegative 非负数验证
func (v *SimpleValidator) NonNegative(field string, value int) *SimpleValidator {
	if value < 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s must be non-negative, got %d", field, value))
	}
	return v
}

// OneOf 枚举值验证
func (v *SimpleValidator) OneOf(field, value string, allowed ...string) *SimpleValidator {
	if value == "" {
		return v // 空值不验证
	}
	for _, a := range allowed {
		if value == a {
			return v
		}
	}
	v.errors = append(v.errors, fmt.Sprintf("%s must be one of [%s], got %s", field, strings.Join(allowed, ", "), value))
	return v
}

// Custom 自定义验证
func (v *SimpleValidator) Custom(condition bool, message string) *SimpleValidator {
	if !condition {
		v.errors = append(v.errors, message)
	}
	return v
}

// HasErrors 是否有错误
func (v *SimpleValidator) HasErrors() bool {
	return len(v.errors) > 0
}

// Error 返回错误（如果有）
func (v *SimpleValidator) Error() error {
	if len(v.errors) == 0 {
		return nil
	}
	return fmt.Errorf("validation failed: %s", strings.Join(v.errors, "; "))
}

// Errors 返回所有错误
func (v *SimpleValidator) Errors() []string {
	return v.errors
}

// ==================== 配置默认值 ====================

// ApplyDefaults 应用默认值到任务配置
func ApplyDefaults(config *TaskConfig) {
	if config.PortScan != nil {
		applyPortScanDefaults(config.PortScan)
	}
	if config.PortIdentify != nil {
		applyPortIdentifyDefaults(config.PortIdentify)
	}
	if config.DomainScan != nil {
		applyDomainScanDefaults(config.DomainScan)
	}
	if config.Fingerprint != nil {
		applyFingerprintDefaults(config.Fingerprint)
	}
	if config.PocScan != nil {
		applyPocScanDefaults(config.PocScan)
	}
	if config.DirScan != nil {
		applyDirScanDefaults(config.DirScan)
	}
}

func applyPortScanDefaults(c *PortScanConfig) {
	if c.Tool == "" {
		c.Tool = "naabu"
	}
	if c.Ports == "" {
		c.Ports = "21,22,23,25,80,443,3306,3389,6379,8080,8443"
	}
	if c.Rate <= 0 {
		c.Rate = 1000
	}
	if c.Timeout <= 0 {
		c.Timeout = 5
	}
	if c.PortThreshold <= 0 {
		c.PortThreshold = 100
	}
}

func applyPortIdentifyDefaults(c *PortIdentifyConfig) {
	if c.Timeout <= 0 {
		c.Timeout = 30
	}
}

func applyDomainScanDefaults(c *DomainScanConfig) {
	if c.Timeout <= 0 {
		c.Timeout = 30
	}
	if c.MaxEnumerationTime <= 0 {
		c.MaxEnumerationTime = 10
	}
	if c.Threads <= 0 {
		c.Threads = 10
	}
	if c.Concurrent <= 0 {
		c.Concurrent = 100
	}
}

func applyFingerprintDefaults(c *FingerprintConfig) {
	if c.Tool == "" {
		c.Tool = "builtin"
	}
	if c.Timeout <= 0 {
		c.Timeout = 300
	}
	if c.TargetTimeout <= 0 {
		c.TargetTimeout = 30
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 10
	}
	if c.ActiveTimeout <= 0 {
		c.ActiveTimeout = 10
	}
}

func applyPocScanDefaults(c *PocScanConfig) {
	if c.Severity == "" {
		c.Severity = "critical,high,medium"
	}
	if c.RateLimit <= 0 {
		c.RateLimit = 150
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 25
	}
	if c.TargetTimeout <= 0 {
		c.TargetTimeout = 600
	}
}

func applyDirScanDefaults(c *DirScanConfig) {
	if c.Threads <= 0 {
		c.Threads = 10
	}
	if c.Timeout <= 0 {
		c.Timeout = 10
	}
	if len(c.StatusCodes) == 0 {
		c.StatusCodes = []int{200, 201, 301, 302, 307, 401, 403}
	}
}

// ==================== 配置验证 ====================

// ValidateTaskConfig 验证任务配置
func ValidateTaskConfig(config *TaskConfig) error {
	v := NewSimpleValidator()

	if config.PortScan != nil && config.PortScan.Enable {
		v.OneOf("portscan.tool", config.PortScan.Tool, "tcp", "masscan", "nmap", "naabu")
		v.NonNegative("portscan.rate", config.PortScan.Rate)
		v.NonNegative("portscan.timeout", config.PortScan.Timeout)
	}

	if config.PortIdentify != nil && config.PortIdentify.Enable {
		v.NonNegative("portidentify.timeout", config.PortIdentify.Timeout)
	}

	if config.DomainScan != nil && config.DomainScan.Enable {
		v.NonNegative("domainscan.timeout", config.DomainScan.Timeout)
		v.NonNegative("domainscan.threads", config.DomainScan.Threads)
	}

	if config.Fingerprint != nil && config.Fingerprint.Enable {
		v.OneOf("fingerprint.tool", config.Fingerprint.Tool, "httpx", "builtin", "")
		v.NonNegative("fingerprint.timeout", config.Fingerprint.Timeout)
		v.NonNegative("fingerprint.concurrency", config.Fingerprint.Concurrency)
	}

	if config.PocScan != nil && config.PocScan.Enable {
		v.NonNegative("pocscan.rateLimit", config.PocScan.RateLimit)
		v.NonNegative("pocscan.concurrency", config.PocScan.Concurrency)
	}

	if config.DirScan != nil && config.DirScan.Enable {
		v.NonNegative("dirscan.threads", config.DirScan.Threads)
		v.NonNegative("dirscan.timeout", config.DirScan.Timeout)
	}

	return v.Error()
}
