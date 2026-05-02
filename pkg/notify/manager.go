package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	Enabled               bool     `json:"enabled"`               // 是否启用高危过滤
	HighRiskFingerprints  []string `json:"highRiskFingerprints"`  // 高危指纹列表
	HighRiskPorts         []int    `json:"highRiskPorts"`         // 高危端口列表
	HighRiskPocSeverities []string `json:"highRiskPocSeverities"` // 高危POC严重级别
	NewAssetNotify        bool     `json:"newAssetNotify"`        // 新资产发现时通知
}

func (f *HighRiskFilter) HasConditions() bool {
	return f.NewAssetNotify ||
		len(f.HighRiskFingerprints) > 0 ||
		len(f.HighRiskPorts) > 0 ||
		len(f.HighRiskPocSeverities) > 0
}

// LoadConfigs 从配置列表加载提供者
func (m *NotifyManager) LoadConfigs(configs []ConfigItem) error {
	m.notifier.ClearProviders()

	var errs []string
	for _, cfg := range configs {
		if cfg.Status != "enable" {
			continue
		}

		// 确保消息模板包含高危详情占位符
		messageTemplate := fixMessageTemplate(cfg.MessageTemplate)

		provider, err := CreateProvider(cfg.Provider, cfg.Config, messageTemplate)
		if err != nil {
			logx.Errorf("Failed to create notify provider %s: %v", cfg.Provider, err)
			errs = append(errs, fmt.Sprintf("%s: %v", cfg.Provider, err))
			continue
		}

		m.notifier.AddProvider(provider)
		logx.Infof("Loaded notify provider: %s", cfg.Provider)
	}

	if len(errs) > 0 && m.notifier.ProviderCount() == 0 {
		return fmt.Errorf("all providers failed to load: %s", strings.Join(errs, "; "))
	}

	return nil
}

// fixMessageTemplate 确保消息模板包含高危详情占位符
// 防止数据库中存储的旧模板没有 {{highRiskDetails}} 导致高危信息丢失
func fixMessageTemplate(template string) string {
	if template == "" {
		return "" // 空模板使用默认模板
	}
	if !strings.Contains(template, "{{highRiskDetails}}") {
		return template + "{{highRiskDetails}}"
	}
	return template
}

// Send 发送通知
func (m *NotifyManager) Send(ctx context.Context, result *NotifyResult) error {
	return m.notifier.Send(ctx, result)
}

// providerFactory 通知提供者工厂函数类型
type providerFactory func(configJSON, messageTemplate string) (Provider, error)

// CreateProvider 根据类型创建提供者
func CreateProvider(providerType, configJSON, messageTemplate string) (Provider, error) {
	factory, ok := providerFactories[providerType]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}
	return factory(configJSON, messageTemplate)
}

// providerFactories 通知提供者工厂映射
var providerFactories = map[string]providerFactory{
	"smtp":     createSMTPProvider,
	"feishu":   createFeishuProvider,
	"dingtalk": createDingTalkProvider,
	"wecom":    createWeComProvider,
	"slack":    createSlackProvider,
	"discord":  createDiscordProvider,
	"telegram": createTelegramProvider,
	"teams":    createTeamsProvider,
	"gotify":   createGotifyProvider,
	"webhook":  createWebhookProvider,
}

// createSMTPProvider 创建SMTP提供者
func createSMTPProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg SMTPConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse smtp config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewSMTPProvider(cfg), nil
}

// createFeishuProvider 创建飞书提供者
func createFeishuProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg FeishuConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse feishu config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewFeishuProvider(cfg), nil
}

// createDingTalkProvider 创建钉钉提供者
func createDingTalkProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg DingTalkConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse dingtalk config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewDingTalkProvider(cfg), nil
}

// createWeComProvider 创建企业微信提供者
func createWeComProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg WeComConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse wecom config: %w", err)
	}
	// 优先级：ConfigItem.MessageTemplate > configJSON中的messageTemplate
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewWeComProvider(cfg), nil
}

// createSlackProvider 创建Slack提供者
func createSlackProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg SlackConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse slack config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewSlackProvider(cfg), nil
}

// createDiscordProvider 创建Discord提供者
func createDiscordProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg DiscordConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse discord config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewDiscordProvider(cfg), nil
}

// createTelegramProvider 创建Telegram提供者
func createTelegramProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg TelegramConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse telegram config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewTelegramProvider(cfg), nil
}

// createTeamsProvider 创建Teams提供者
func createTeamsProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg TeamsConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse teams config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewTeamsProvider(cfg), nil
}

// createGotifyProvider 创建Gotify提供者
func createGotifyProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg GotifyConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse gotify config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewGotifyProvider(cfg), nil
}

// createWebhookProvider 创建Webhook提供者
func createWebhookProvider(configJSON, messageTemplate string) (Provider, error) {
	var cfg WebhookConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse webhook config: %w", err)
	}
	if messageTemplate != "" {
		cfg.MessageTemplate = messageTemplate
	}
	return NewWebhookProvider(cfg), nil
}

// TestProvider 测试通知提供者
func TestProvider(providerType, configJSON, messageTemplate string) error {
	// 确保测试模板也包含高危详情占位符
	messageTemplate = fixMessageTemplate(messageTemplate)

	provider, err := CreateProvider(providerType, configJSON, messageTemplate)
	if err != nil {
		return err
	}

	// 创建测试结果（包含测试高危信息）
	testResult := &NotifyResult{
		TaskId:     "test-task-id",
		TaskName:   "测试任务",
		Status:     "SUCCESS",
		AssetCount: 100,
		VulCount:   5,
		Duration:   "10m30s",
		ReportURL:  "https://example.com/report?taskId=test-task-id",
		// 测试用的高危信息
		HighRiskInfo: &HighRiskInfo{
			HighRiskFingerprints: []string{"测试指纹1", "测试指纹2"},
			HighRiskPorts:        []int{5601, 9200},
			HighRiskVulCount:     3,
			HighRiskVulSeverities: map[string]int{
				"critical": 1,
				"high":     2,
			},
			NewAssetCount: 5,
		},
	}

	ctx := context.Background()
	return provider.Send(ctx, testResult)
}
