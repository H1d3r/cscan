package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// NotifyResult 通知结果
type NotifyResult struct {
	TaskId      string    `json:"taskId"`
	TaskName    string    `json:"taskName"`
	Status      string    `json:"status"`      // SUCCESS, FAILURE
	AssetCount  int       `json:"assetCount"`
	VulCount    int       `json:"vulCount"`
	Duration    string    `json:"duration"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
	WorkspaceId string    `json:"workspaceId"`
	ReportURL   string    `json:"reportUrl"` // 报告URL地址
	// 高危检测结果
	HighRiskInfo *HighRiskInfo `json:"highRiskInfo,omitempty"`
}

// HighRiskInfo 高危检测信息
type HighRiskInfo struct {
	HighRiskFingerprints []string `json:"highRiskFingerprints"` // 发现的高危指纹
	HighRiskPorts        []int    `json:"highRiskPorts"`        // 发现的高危端口
	HighRiskVulCount     int      `json:"highRiskVulCount"`     // 高危漏洞数量
	HighRiskVulSeverities map[string]int `json:"highRiskVulSeverities"` // 按严重级别统计: critical->5, high->10
}

// Provider 通知提供者接口
type Provider interface {
	// Name 返回提供者名称
	Name() string
	// Send 发送通知
	Send(ctx context.Context, result *NotifyResult) error
}

// Notifier 通知服务
type Notifier struct {
	providers []Provider
	mu        sync.RWMutex
}

// NewNotifier 创建通知服务
func NewNotifier() *Notifier {
	return &Notifier{
		providers: make([]Provider, 0),
	}
}

// AddProvider 添加通知提供者
func (n *Notifier) AddProvider(p Provider) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.providers = append(n.providers, p)
}

// ClearProviders 清空所有提供者
func (n *Notifier) ClearProviders() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.providers = make([]Provider, 0)
}

// Send 发送通知到所有提供者
func (n *Notifier) Send(ctx context.Context, result *NotifyResult) error {
	n.mu.RLock()
	providers := make([]Provider, len(n.providers))
	copy(providers, n.providers)
	n.mu.RUnlock()

	if len(providers) == 0 {
		return nil
	}

	var errs []string
	for _, p := range providers {
		if err := p.Send(ctx, result); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p.Name(), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notify errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// FormatMessage 格式化通知消息
func FormatMessage(result *NotifyResult, template string) string {
	if template == "" {
		template = DefaultTemplate
	}

	statusEmoji := "✅"
	if result.Status == "FAILURE" {
		statusEmoji = "❌"
	}

	replacer := strings.NewReplacer(
		"{{taskName}}", result.TaskName,
		"{{taskId}}", result.TaskId,
		"{{status}}", result.Status,
		"{{statusEmoji}}", statusEmoji,
		"{{assetCount}}", fmt.Sprintf("%d", result.AssetCount),
		"{{vulCount}}", fmt.Sprintf("%d", result.VulCount),
		"{{duration}}", result.Duration,
		"{{startTime}}", result.StartTime.Format("2006-01-02 15:04:05"),
		"{{endTime}}", result.EndTime.Format("2006-01-02 15:04:05"),
		"{{workspaceId}}", result.WorkspaceId,
		"{{reportUrl}}", result.ReportURL,
	)

	return replacer.Replace(template)
}

// DefaultTemplate 默认消息模板
const DefaultTemplate = `{{statusEmoji}} 扫描任务完成

任务名称: {{taskName}}
任务状态: {{status}}
发现资产: {{assetCount}}
发现漏洞: {{vulCount}}
执行时长: {{duration}}
开始时间: {{startTime}}
结束时间: {{endTime}}
报告地址: {{reportUrl}}`

// MarkdownTemplate Markdown格式模板
const MarkdownTemplate = `## {{statusEmoji}} 扫描任务完成

| 项目 | 内容 |
|------|------|
| 任务名称 | {{taskName}} |
| 任务状态 | {{status}} |
| 发现资产 | {{assetCount}} |
| 发现漏洞 | {{vulCount}} |
| 执行时长 | {{duration}} |
| 开始时间 | {{startTime}} |
| 结束时间 | {{endTime}} |
| 报告地址 | {{reportUrl}} |`
