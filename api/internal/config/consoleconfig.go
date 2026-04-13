package config

import "time"

// ConsoleConfig 控制台安全配置
type ConsoleConfig struct {
	// 命令黑名单（正则表达式列表）
	CommandBlacklist []string `json:",optional"`

	// 文件访问白名单目录
	FileAllowedPaths []string `json:",optional"`

	// 命令执行超时（秒）
	CommandTimeout int64 `json:",default=60"`

	// 文件上传大小限制（字节）
	MaxUploadSize int64 `json:",default=104857600"` // 100MB

	// WebSocket空闲超时（秒）
	WSIdleTimeout int64 `json:",default=300"` // 5分钟

	// 每个Worker最大控制台会话数
	MaxSessionsPerWorker int `json:",default=3"`

	// 审计日志保留天数
	AuditLogRetentionDays int `json:",default=90"`
}

// DefaultConsoleConfig 默认控制台配置
func DefaultConsoleConfig() *ConsoleConfig {
	return &ConsoleConfig{
		CommandBlacklist: []string{
			`^rm\s+(-rf?|--recursive)\s+/\s*$`,   // rm -rf /
			`^rm\s+(-rf?|--recursive)\s+/\*`,     // rm -rf /*
			`^rm\s+(-rf?|--recursive)\s+~`,       // rm -rf ~
			`^shutdown`,                          // shutdown
			`^reboot`,                            // reboot
			`^halt`,                              // halt
			`^poweroff`,                          // poweroff
			`^init\s+0`,                          // init 0
			`^init\s+6`,                          // init 6
			`^mkfs`,                              // mkfs (format disk)
			`^dd\s+.*of=/dev/`,                   // dd to device
			`^:\(\)\{.*\}`,                       // fork bomb
			`>\s*/dev/sd[a-z]`,                   // write to disk device
			`>\s*/dev/nvme`,                      // write to nvme device
			`^chmod\s+(-R\s+)?777\s+/\s*$`,       // chmod 777 /
			`^chown\s+(-R\s+)?.*\s+/\s*$`,        // chown /
			`^format\s+[a-zA-Z]:`,                // Windows format drive
			`^del\s+/[sS]\s+/[qQ]\s+[a-zA-Z]:\\`, // Windows del /s /q C:\
			`^rd\s+/[sS]\s+/[qQ]\s+[a-zA-Z]:\\`,  // Windows rd /s /q C:\
		},
		FileAllowedPaths:      []string{}, // 空表示使用Worker默认配置
		CommandTimeout:        60,
		MaxUploadSize:         100 * 1024 * 1024, // 100MB
		WSIdleTimeout:         300,               // 5分钟
		MaxSessionsPerWorker:  3,
		AuditLogRetentionDays: 90,
	}
}

// GetCommandTimeout 获取命令超时时间
func (c *ConsoleConfig) GetCommandTimeout() time.Duration {
	if c.CommandTimeout <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.CommandTimeout) * time.Second
}

// GetWSIdleTimeout 获取WebSocket空闲超时时间
func (c *ConsoleConfig) GetWSIdleTimeout() time.Duration {
	if c.WSIdleTimeout <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.WSIdleTimeout) * time.Second
}

// GetMaxUploadSize 获取最大上传大小
func (c *ConsoleConfig) GetMaxUploadSize() int64 {
	if c.MaxUploadSize <= 0 {
		return 100 * 1024 * 1024 // 100MB
	}
	return c.MaxUploadSize
}

// GetMaxSessionsPerWorker 获取每个Worker最大会话数
func (c *ConsoleConfig) GetMaxSessionsPerWorker() int {
	if c.MaxSessionsPerWorker <= 0 {
		return 3
	}
	return c.MaxSessionsPerWorker
}
