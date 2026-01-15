package notify

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
)

// NotifyManager 通知管理器
type NotifyManager struct {
	notifier *Notifier
}

// NewNotifyManager 创建通知管理器
func NewNotifyManager() *NotifyManager {
	return &NotifyManager{
		notifier: NewNotifier(),
	}
}

// ConfigItem 配置项（从数据库读取）
type ConfigItem struct {
	Provider        string          `json:"provider"`
	Config          string          `json:"config"`
	Status          string          `json:"status"`
	MessageTemplate string          `json:"messageTemplate"`
	HighRiskFilter  *HighRiskFilter `json:"highRiskFilter,omitempty"`
	WebURL          string          `json:"webUrl"` // 前端URL，用于生成报告链接
}

// HighRiskFilter 高危过滤配置
type HighRiskFilter struct {
	Enabled              bool     `json:"enabled"`              // 是否启用高危过滤
	HighRiskFingerprints []string `json:"highRiskFingerprints"` // 高危指纹列表
	HighRiskPorts        []int    `json:"highRiskPorts"`        // 高危端口列表
	HighRiskPocSeverities []string `json:"highRiskPocSeverities"` // 高危POC严重级别
}

// LoadConfigs 从配置列表加载提供者
func (m *NotifyManager) LoadConfigs(configs []ConfigItem) error {
	m.notifier.ClearProviders()

	for _, cfg := range configs {
		if cfg.Status != "enable" {
			continue
		}

		provider, err := CreateProvider(cfg.Provider, cfg.Config, cfg.MessageTemplate)
		if err != nil {
			logx.Errorf("Failed to create notify provider %s: %v", cfg.Provider, err)
			continue
		}

		m.notifier.AddProvider(provider)
		logx.Infof("Loaded notify provider: %s", cfg.Provider)
	}

	return nil
}

// Send 发送通知
func (m *NotifyManager) Send(ctx context.Context, result *NotifyResult) error {
	return m.notifier.Send(ctx, result)
}

// CreateProvider 根据类型创建提供者
func CreateProvider(providerType, configJSON, messageTemplate string) (Provider, error) {
	switch providerType {
	case "smtp":
		var cfg SMTPConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse smtp config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewSMTPProvider(cfg), nil

	case "feishu":
		var cfg FeishuConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse feishu config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewFeishuProvider(cfg), nil

	case "dingtalk":
		var cfg DingTalkConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse dingtalk config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewDingTalkProvider(cfg), nil

	case "wecom":
		var cfg WeComConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse wecom config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewWeComProvider(cfg), nil

	case "slack":
		var cfg SlackConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse slack config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewSlackProvider(cfg), nil

	case "discord":
		var cfg DiscordConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse discord config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewDiscordProvider(cfg), nil

	case "telegram":
		var cfg TelegramConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse telegram config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewTelegramProvider(cfg), nil

	case "teams":
		var cfg TeamsConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse teams config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewTeamsProvider(cfg), nil

	case "gotify":
		var cfg GotifyConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse gotify config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewGotifyProvider(cfg), nil

	case "webhook":
		var cfg WebhookConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse webhook config: %w", err)
		}
		if messageTemplate != "" {
			cfg.MessageTemplate = messageTemplate
		}
		return NewWebhookProvider(cfg), nil

	default:
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}
}

// TestProvider 测试通知提供者
func TestProvider(providerType, configJSON, messageTemplate string) error {
	provider, err := CreateProvider(providerType, configJSON, messageTemplate)
	if err != nil {
		return err
	}

	// 创建测试结果
	testResult := &NotifyResult{
		TaskId:     "test-task-id",
		TaskName:   "测试任务",
		Status:     "SUCCESS",
		AssetCount: 100,
		VulCount:   5,
		Duration:   "10m30s",
		ReportURL:  "https://example.com/report?taskId=test-task-id",
	}

	ctx := context.Background()
	return provider.Send(ctx, testResult)
}
