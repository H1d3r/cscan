package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// ============== SMTP Email Provider ==============

// SMTPConfig SMTP配置
type SMTPConfig struct {
	Server          string   `json:"server"`
	Port            int      `json:"port"`
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	FromAddress     string   `json:"fromAddress"`
	ToAddresses     []string `json:"toAddresses"`
	Subject         string   `json:"subject"`
	UseTLS          bool     `json:"useTLS"`
	SkipVerify      bool     `json:"skipVerify"`
	MessageTemplate string   `json:"messageTemplate"`
}

// SMTPProvider SMTP邮件通知
type SMTPProvider struct {
	config SMTPConfig
}

// NewSMTPProvider 创建SMTP提供者
func NewSMTPProvider(config SMTPConfig) *SMTPProvider {
	return &SMTPProvider{config: config}
}

func (p *SMTPProvider) Name() string { return "smtp" }

func (p *SMTPProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.Server == "" || len(p.config.ToAddresses) == 0 {
		return fmt.Errorf("smtp config incomplete")
	}

	subject := p.config.Subject
	if subject == "" {
		subject = fmt.Sprintf("扫描任务完成: %s", result.TaskName)
	}

	body := FormatMessage(result, p.config.MessageTemplate)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		p.config.FromAddress,
		strings.Join(p.config.ToAddresses, ","),
		subject,
		body,
	)

	addr := fmt.Sprintf("%s:%d", p.config.Server, p.config.Port)
	auth := smtp.PlainAuth("", p.config.Username, p.config.Password, p.config.Server)

	if p.config.UseTLS {
		return p.sendWithTLS(addr, auth, msg)
	}
	return smtp.SendMail(addr, auth, p.config.FromAddress, p.config.ToAddresses, []byte(msg))
}

func (p *SMTPProvider) sendWithTLS(addr string, auth smtp.Auth, msg string) error {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: p.config.SkipVerify,
		ServerName:         p.config.Server,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, p.config.Server)
	if err != nil {
		return err
	}
	defer client.Close()

	if err = client.Auth(auth); err != nil {
		return err
	}
	if err = client.Mail(p.config.FromAddress); err != nil {
		return err
	}
	for _, to := range p.config.ToAddresses {
		if err = client.Rcpt(to); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return err
	}
	return w.Close()
}

// ============== 飞书 (Feishu/Lark) Provider ==============

// FeishuConfig 飞书配置
type FeishuConfig struct {
	WebhookURL      string `json:"webhookUrl"`
	Secret          string `json:"secret"` // 签名密钥（可选）
	MessageTemplate string `json:"messageTemplate"`
}

// FeishuProvider 飞书通知
type FeishuProvider struct {
	config FeishuConfig
}

// NewFeishuProvider 创建飞书提供者
func NewFeishuProvider(config FeishuConfig) *FeishuProvider {
	return &FeishuProvider{config: config}
}

func (p *FeishuProvider) Name() string { return "feishu" }

func (p *FeishuProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.WebhookURL == "" {
		return fmt.Errorf("feishu webhook url is empty")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	payload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": content,
		},
	}

	// 如果配置了签名密钥
	if p.config.Secret != "" {
		timestamp := time.Now().Unix()
		sign := p.genFeishuSign(timestamp)
		payload["timestamp"] = fmt.Sprintf("%d", timestamp)
		payload["sign"] = sign
	}

	return postJSON(ctx, p.config.WebhookURL, payload)
}

func (p *FeishuProvider) genFeishuSign(timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, p.config.Secret)
	h := hmac.New(sha256.New, []byte(p.config.Secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ============== 钉钉 (DingTalk) Provider ==============

// DingTalkConfig 钉钉配置
type DingTalkConfig struct {
	WebhookURL      string `json:"webhookUrl"`
	Secret          string `json:"secret"` // 签名密钥
	MessageTemplate string `json:"messageTemplate"`
}

// DingTalkProvider 钉钉通知
type DingTalkProvider struct {
	config DingTalkConfig
}

// NewDingTalkProvider 创建钉钉提供者
func NewDingTalkProvider(config DingTalkConfig) *DingTalkProvider {
	return &DingTalkProvider{config: config}
}

func (p *DingTalkProvider) Name() string { return "dingtalk" }

func (p *DingTalkProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.WebhookURL == "" {
		return fmt.Errorf("dingtalk webhook url is empty")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	payload := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]string{
			"content": content,
		},
	}

	webhookURL := p.config.WebhookURL
	// 如果配置了签名密钥
	if p.config.Secret != "" {
		timestamp := time.Now().UnixMilli()
		sign := p.genDingTalkSign(timestamp)
		webhookURL = fmt.Sprintf("%s&timestamp=%d&sign=%s", webhookURL, timestamp, url.QueryEscape(sign))
	}

	return postJSON(ctx, webhookURL, payload)
}

func (p *DingTalkProvider) genDingTalkSign(timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, p.config.Secret)
	h := hmac.New(sha256.New, []byte(p.config.Secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ============== 企业微信 (WeCom) Provider ==============

// WeComConfig 企业微信配置
type WeComConfig struct {
	WebhookURL      string `json:"webhookUrl"`
	MessageTemplate string `json:"messageTemplate"`
}

// WeComProvider 企业微信通知
type WeComProvider struct {
	config WeComConfig
}

// NewWeComProvider 创建企业微信提供者
func NewWeComProvider(config WeComConfig) *WeComProvider {
	return &WeComProvider{config: config}
}

func (p *WeComProvider) Name() string { return "wecom" }

func (p *WeComProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.WebhookURL == "" {
		return fmt.Errorf("wecom webhook url is empty")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	payload := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]string{
			"content": content,
		},
	}

	return postJSON(ctx, p.config.WebhookURL, payload)
}

// ============== Slack Provider ==============

// SlackConfig Slack配置
type SlackConfig struct {
	WebhookURL      string `json:"webhookUrl"`
	Channel         string `json:"channel"`
	Username        string `json:"username"`
	MessageTemplate string `json:"messageTemplate"`
}

// SlackProvider Slack通知
type SlackProvider struct {
	config SlackConfig
}

// NewSlackProvider 创建Slack提供者
func NewSlackProvider(config SlackConfig) *SlackProvider {
	return &SlackProvider{config: config}
}

func (p *SlackProvider) Name() string { return "slack" }

func (p *SlackProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.WebhookURL == "" {
		return fmt.Errorf("slack webhook url is empty")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	payload := map[string]interface{}{
		"text": content,
	}
	if p.config.Channel != "" {
		payload["channel"] = p.config.Channel
	}
	if p.config.Username != "" {
		payload["username"] = p.config.Username
	}

	return postJSON(ctx, p.config.WebhookURL, payload)
}

// ============== Discord Provider ==============

// DiscordConfig Discord配置
type DiscordConfig struct {
	WebhookURL      string `json:"webhookUrl"`
	Username        string `json:"username"`
	MessageTemplate string `json:"messageTemplate"`
}

// DiscordProvider Discord通知
type DiscordProvider struct {
	config DiscordConfig
}

// NewDiscordProvider 创建Discord提供者
func NewDiscordProvider(config DiscordConfig) *DiscordProvider {
	return &DiscordProvider{config: config}
}

func (p *DiscordProvider) Name() string { return "discord" }

func (p *DiscordProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.WebhookURL == "" {
		return fmt.Errorf("discord webhook url is empty")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	payload := map[string]interface{}{
		"content": content,
	}
	if p.config.Username != "" {
		payload["username"] = p.config.Username
	}

	return postJSON(ctx, p.config.WebhookURL, payload)
}

// ============== Telegram Provider ==============

// TelegramConfig Telegram配置
type TelegramConfig struct {
	BotToken        string `json:"botToken"`
	ChatID          string `json:"chatId"`
	ParseMode       string `json:"parseMode"` // Markdown, HTML, MarkdownV2
	MessageTemplate string `json:"messageTemplate"`
}

// TelegramProvider Telegram通知
type TelegramProvider struct {
	config TelegramConfig
}

// NewTelegramProvider 创建Telegram提供者
func NewTelegramProvider(config TelegramConfig) *TelegramProvider {
	return &TelegramProvider{config: config}
}

func (p *TelegramProvider) Name() string { return "telegram" }

func (p *TelegramProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.BotToken == "" || p.config.ChatID == "" {
		return fmt.Errorf("telegram config incomplete")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", p.config.BotToken)

	payload := map[string]interface{}{
		"chat_id": p.config.ChatID,
		"text":    content,
	}
	if p.config.ParseMode != "" {
		payload["parse_mode"] = p.config.ParseMode
	}

	return postJSON(ctx, apiURL, payload)
}

// ============== Microsoft Teams Provider ==============

// TeamsConfig Teams配置
type TeamsConfig struct {
	WebhookURL      string `json:"webhookUrl"`
	MessageTemplate string `json:"messageTemplate"`
}

// TeamsProvider Teams通知
type TeamsProvider struct {
	config TeamsConfig
}

// NewTeamsProvider 创建Teams提供者
func NewTeamsProvider(config TeamsConfig) *TeamsProvider {
	return &TeamsProvider{config: config}
}

func (p *TeamsProvider) Name() string { return "teams" }

func (p *TeamsProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.WebhookURL == "" {
		return fmt.Errorf("teams webhook url is empty")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	// Teams Adaptive Card 格式
	payload := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": "0076D7",
		"summary":    "扫描任务完成",
		"sections": []map[string]interface{}{
			{
				"activityTitle": "扫描任务完成通知",
				"text":          content,
			},
		},
	}

	return postJSON(ctx, p.config.WebhookURL, payload)
}

// ============== Gotify Provider ==============

// GotifyConfig Gotify配置
type GotifyConfig struct {
	ServerURL       string `json:"serverUrl"`
	Token           string `json:"token"`
	Priority        int    `json:"priority"`
	MessageTemplate string `json:"messageTemplate"`
}

// GotifyProvider Gotify通知
type GotifyProvider struct {
	config GotifyConfig
}

// NewGotifyProvider 创建Gotify提供者
func NewGotifyProvider(config GotifyConfig) *GotifyProvider {
	return &GotifyProvider{config: config}
}

func (p *GotifyProvider) Name() string { return "gotify" }

func (p *GotifyProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.ServerURL == "" || p.config.Token == "" {
		return fmt.Errorf("gotify config incomplete")
	}

	content := FormatMessage(result, p.config.MessageTemplate)

	apiURL := fmt.Sprintf("%s/message?token=%s", strings.TrimSuffix(p.config.ServerURL, "/"), p.config.Token)

	priority := p.config.Priority
	if priority == 0 {
		priority = 5
	}

	payload := map[string]interface{}{
		"title":    fmt.Sprintf("扫描任务完成: %s", result.TaskName),
		"message":  content,
		"priority": priority,
	}

	return postJSON(ctx, apiURL, payload)
}

// ============== Custom Webhook Provider ==============

// WebhookConfig 自定义Webhook配置
type WebhookConfig struct {
	URL             string            `json:"url"`
	Method          string            `json:"method"` // GET, POST
	Headers         map[string]string `json:"headers"`
	MessageTemplate string            `json:"messageTemplate"`
	BodyTemplate    string            `json:"bodyTemplate"` // 自定义请求体模板
}

// WebhookProvider 自定义Webhook通知
type WebhookProvider struct {
	config WebhookConfig
}

// NewWebhookProvider 创建Webhook提供者
func NewWebhookProvider(config WebhookConfig) *WebhookProvider {
	return &WebhookProvider{config: config}
}

func (p *WebhookProvider) Name() string { return "webhook" }

func (p *WebhookProvider) Send(ctx context.Context, result *NotifyResult) error {
	if p.config.URL == "" {
		return fmt.Errorf("webhook url is empty")
	}

	method := p.config.Method
	if method == "" {
		method = "POST"
	}

	var body io.Reader
	if method == "POST" {
		if p.config.BodyTemplate != "" {
			// 使用自定义模板
			content := FormatMessage(result, p.config.BodyTemplate)
			body = strings.NewReader(content)
		} else {
			// 默认JSON格式
			payload := map[string]interface{}{
				"taskId":     result.TaskId,
				"taskName":   result.TaskName,
				"status":     result.Status,
				"assetCount": result.AssetCount,
				"vulCount":   result.VulCount,
				"duration":   result.Duration,
				"startTime":  result.StartTime.Format(time.RFC3339),
				"endTime":    result.EndTime.Format(time.RFC3339),
				"reportUrl":  result.ReportURL,
				"message":    FormatMessage(result, p.config.MessageTemplate),
			}
			data, _ := json.Marshal(payload)
			body = bytes.NewReader(data)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, p.config.URL, body)
	if err != nil {
		return err
	}

	// 设置默认Content-Type
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 设置自定义Headers
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook request failed: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ============== Helper Functions ==============

func postJSON(ctx context.Context, url string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}
