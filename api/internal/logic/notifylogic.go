package logic

import (
	"context"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"
	"cscan/pkg/notify"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// NotifyConfigListLogic 通知配置列表
type NotifyConfigListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNotifyConfigListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NotifyConfigListLogic {
	return &NotifyConfigListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NotifyConfigListLogic) NotifyConfigList() (resp *types.NotifyConfigListResp, err error) {
	configs, err := l.svcCtx.NotifyConfigModel.FindAll(l.ctx)
	if err != nil {
		return &types.NotifyConfigListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.NotifyConfig, 0, len(configs))
	for _, c := range configs {
		item := types.NotifyConfig{
			Id:              c.Id.Hex(),
			Name:            c.Name,
			Provider:        c.Provider,
			Config:          c.Config,
			Status:          c.Status,
			MessageTemplate: c.MessageTemplate,
			CreateTime:      c.CreateTime.Local().Format("2006-01-02 15:04:05"),
			UpdateTime:      c.UpdateTime.Local().Format("2006-01-02 15:04:05"),
		}
		// 转换高危过滤配置
		if c.HighRiskFilter != nil {
			item.HighRiskFilter = &types.HighRiskFilter{
				Enabled:              c.HighRiskFilter.Enabled,
				HighRiskFingerprints: c.HighRiskFilter.HighRiskFingerprints,
				HighRiskPorts:        c.HighRiskFilter.HighRiskPorts,
				HighRiskPocSeverities: c.HighRiskFilter.HighRiskPocSeverities,
			}
		}
		list = append(list, item)
	}

	return &types.NotifyConfigListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

// NotifyConfigSaveLogic 保存通知配置
type NotifyConfigSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNotifyConfigSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NotifyConfigSaveLogic {
	return &NotifyConfigSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NotifyConfigSaveLogic) NotifyConfigSave(req *types.NotifyConfigSaveReq) (resp *types.BaseResp, err error) {
	if req.Provider == "" {
		return &types.BaseResp{Code: 400, Msg: "提供者类型不能为空"}, nil
	}

	// 转换高危过滤配置
	var highRiskFilter *model.HighRiskFilter
	if req.HighRiskFilter != nil {
		highRiskFilter = &model.HighRiskFilter{
			Enabled:              req.HighRiskFilter.Enabled,
			HighRiskFingerprints: req.HighRiskFilter.HighRiskFingerprints,
			HighRiskPorts:        req.HighRiskFilter.HighRiskPorts,
			HighRiskPocSeverities: req.HighRiskFilter.HighRiskPocSeverities,
		}
	}

	doc := &model.NotifyConfig{
		Name:            req.Name,
		Provider:        req.Provider,
		Config:          req.Config,
		Status:          req.Status,
		MessageTemplate: req.MessageTemplate,
		HighRiskFilter:  highRiskFilter,
	}

	if req.Id != "" {
		// 更新
		update := bson.M{
			"name":             req.Name,
			"config":           req.Config,
			"status":           req.Status,
			"message_template": req.MessageTemplate,
		}
		if highRiskFilter != nil {
			update["high_risk_filter"] = highRiskFilter
		}
		err = l.svcCtx.NotifyConfigModel.Update(l.ctx, req.Id, update)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新失败: " + err.Error()}, nil
		}
	} else {
		// 新增（使用Upsert，同一provider只保留一个配置）
		err = l.svcCtx.NotifyConfigModel.Upsert(l.ctx, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "保存失败: " + err.Error()}, nil
		}
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

// NotifyConfigDeleteLogic 删除通知配置
type NotifyConfigDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNotifyConfigDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NotifyConfigDeleteLogic {
	return &NotifyConfigDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NotifyConfigDeleteLogic) NotifyConfigDelete(req *types.NotifyConfigDeleteReq) (resp *types.BaseResp, err error) {
	if req.Id == "" {
		return &types.BaseResp{Code: 400, Msg: "ID不能为空"}, nil
	}

	err = l.svcCtx.NotifyConfigModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// NotifyConfigTestLogic 测试通知配置
type NotifyConfigTestLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNotifyConfigTestLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NotifyConfigTestLogic {
	return &NotifyConfigTestLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NotifyConfigTestLogic) NotifyConfigTest(req *types.NotifyConfigTestReq) (resp *types.BaseResp, err error) {
	if req.Provider == "" || req.Config == "" {
		return &types.BaseResp{Code: 400, Msg: "参数不完整"}, nil
	}

	err = notify.TestProvider(req.Provider, req.Config, req.MessageTemplate)
	if err != nil {
		l.Logger.Errorf("Test notify provider %s failed: %v", req.Provider, err)
		return &types.BaseResp{Code: 500, Msg: "测试失败: " + err.Error()}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "测试成功，请检查是否收到通知"}, nil
}

// GetNotifyProviders 获取支持的通知提供者列表
type NotifyProviderListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNotifyProviderListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NotifyProviderListLogic {
	return &NotifyProviderListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NotifyProviderListLogic) NotifyProviderList() (resp *types.NotifyProviderListResp, err error) {
	providers := []types.NotifyProvider{
		{
			Id:          "smtp",
			Name:        "邮件 (SMTP)",
			Description: "通过SMTP服务器发送邮件通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "server", Label: "SMTP服务器", Type: "text", Required: true, Placeholder: "smtp.example.com"},
				{Name: "port", Label: "端口", Type: "number", Required: true, Placeholder: "465"},
				{Name: "username", Label: "用户名", Type: "text", Required: true, Placeholder: "user@example.com"},
				{Name: "password", Label: "密码", Type: "password", Required: true},
				{Name: "fromAddress", Label: "发件人地址", Type: "text", Required: true, Placeholder: "notify@example.com"},
				{Name: "toAddresses", Label: "收件人地址", Type: "textarea", Required: true, Placeholder: "每行一个邮箱地址"},
				{Name: "subject", Label: "邮件主题", Type: "text", Required: false, Placeholder: "扫描任务完成通知"},
				{Name: "useTLS", Label: "使用TLS", Type: "switch", Required: false},
				{Name: "skipVerify", Label: "跳过证书验证", Type: "switch", Required: false},
			},
		},
		{
			Id:          "feishu",
			Name:        "飞书",
			Description: "通过飞书机器人Webhook发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "webhookUrl", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx"},
				{Name: "secret", Label: "签名密钥", Type: "password", Required: false, Placeholder: "可选，用于签名验证"},
			},
		},
		{
			Id:          "dingtalk",
			Name:        "钉钉",
			Description: "通过钉钉机器人Webhook发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "webhookUrl", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://oapi.dingtalk.com/robot/send?access_token=xxx"},
				{Name: "secret", Label: "签名密钥", Type: "password", Required: false, Placeholder: "可选，用于签名验证"},
			},
		},
		{
			Id:          "wecom",
			Name:        "企业微信",
			Description: "通过企业微信机器人Webhook发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "webhookUrl", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"},
			},
		},
		{
			Id:          "slack",
			Name:        "Slack",
			Description: "通过Slack Webhook发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "webhookUrl", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://hooks.slack.com/services/xxx"},
				{Name: "channel", Label: "频道", Type: "text", Required: false, Placeholder: "#general"},
				{Name: "username", Label: "机器人名称", Type: "text", Required: false, Placeholder: "CSCAN Bot"},
			},
		},
		{
			Id:          "discord",
			Name:        "Discord",
			Description: "通过Discord Webhook发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "webhookUrl", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://discord.com/api/webhooks/xxx"},
				{Name: "username", Label: "机器人名称", Type: "text", Required: false, Placeholder: "CSCAN Bot"},
			},
		},
		{
			Id:          "telegram",
			Name:        "Telegram",
			Description: "通过Telegram Bot发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "botToken", Label: "Bot Token", Type: "password", Required: true, Placeholder: "从 @BotFather 获取"},
				{Name: "chatId", Label: "Chat ID", Type: "text", Required: true, Placeholder: "用户或群组ID"},
				{Name: "parseMode", Label: "解析模式", Type: "select", Required: false, Options: []string{"", "Markdown", "MarkdownV2", "HTML"}},
			},
		},
		{
			Id:          "teams",
			Name:        "Microsoft Teams",
			Description: "通过Teams Webhook发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "webhookUrl", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://xxx.webhook.office.com/webhookb2/xxx"},
			},
		},
		{
			Id:          "gotify",
			Name:        "Gotify",
			Description: "通过Gotify服务器发送通知",
			ConfigFields: []types.NotifyConfigField{
				{Name: "serverUrl", Label: "服务器地址", Type: "text", Required: true, Placeholder: "https://gotify.example.com"},
				{Name: "token", Label: "应用Token", Type: "password", Required: true},
				{Name: "priority", Label: "优先级", Type: "number", Required: false, Placeholder: "5"},
			},
		},
		{
			Id:          "webhook",
			Name:        "自定义Webhook",
			Description: "发送到自定义HTTP接口",
			ConfigFields: []types.NotifyConfigField{
				{Name: "url", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://example.com/api/notify"},
				{Name: "method", Label: "请求方法", Type: "select", Required: false, Options: []string{"POST", "GET"}},
				{Name: "headers", Label: "自定义Headers", Type: "textarea", Required: false, Placeholder: "JSON格式，如: {\"X-Api-Key\": \"xxx\"}"},
				{Name: "bodyTemplate", Label: "请求体模板", Type: "textarea", Required: false, Placeholder: "自定义JSON模板"},
			},
		},
	}

	return &types.NotifyProviderListResp{
		Code: 0,
		Msg:  "success",
		List: providers,
	}, nil
}
