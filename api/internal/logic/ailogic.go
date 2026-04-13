package logic

import (
	"context"
	"fmt"
	"strings"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type GeneratePocLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGeneratePocLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GeneratePocLogic {
	return &GeneratePocLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GeneratePocLogic) GeneratePoc(req *types.GeneratePocReq) (resp *types.GeneratePocResp, err error) {
	// 验证输入
	if req.Description == "" && req.CveId == "" {
		return &types.GeneratePocResp{Code: 400, Msg: "请提供漏洞描述或CVE编号"}, nil
	}

	// 生成POC模板
	// 注意：这里提供一个基础模板，实际生产环境应该接入AI服务（如OpenAI、Claude等）
	content := l.generateBasicTemplate(req)

	return &types.GeneratePocResp{
		Code: 0,
		Msg:  "success",
		Data: &types.GeneratePocData{
			Content: content,
		},
	}, nil
}

// generateBasicTemplate 生成基础POC模板
// 实际生产环境应该调用AI服务生成更智能的POC
func (l *GeneratePocLogic) generateBasicTemplate(req *types.GeneratePocReq) string {
	// 生成模板ID
	templateId := "custom-poc"
	if req.CveId != "" {
		templateId = strings.ToLower(req.CveId)
	} else if req.VulnType != "" {
		templateId = fmt.Sprintf("custom-%s-poc", req.VulnType)
	}

	// 生成名称
	name := "Custom POC"
	if req.CveId != "" {
		name = req.CveId + " Detection"
	} else if req.Description != "" {
		// 取描述的前50个字符作为名称
		name = req.Description
		if len(name) > 50 {
			name = name[:50] + "..."
		}
	}

	// 根据漏洞类型生成不同的模板
	var requestPart string
	switch req.VulnType {
	case "sqli":
		requestPart = `http:
  - method: GET
    path:
      - "{{BaseURL}}/?id=1' AND '1'='1"
      - "{{BaseURL}}/?id=1' AND '1'='2"
    matchers-condition: and
    matchers:
      - type: dsl
        dsl:
          - "contains(body_1, 'expected_content') && !contains(body_2, 'expected_content')"
      - type: status
        status:
          - 200`
	case "xss":
		requestPart = `http:
  - method: GET
    path:
      - "{{BaseURL}}/?q=<script>alert(1)</script>"
    matchers:
      - type: word
        words:
          - "<script>alert(1)</script>"
        part: body`
	case "rce":
		requestPart = `http:
  - method: POST
    path:
      - "{{BaseURL}}/api/exec"
    headers:
      Content-Type: application/json
    body: '{"cmd":"id"}'
    matchers:
      - type: regex
        regex:
          - "uid=[0-9]+.*gid=[0-9]+"
        part: body`
	case "lfi":
		requestPart = `http:
  - method: GET
    path:
      - "{{BaseURL}}/?file=../../../etc/passwd"
      - "{{BaseURL}}/?file=....//....//....//etc/passwd"
    matchers:
      - type: regex
        regex:
          - "root:.*:0:0:"
        part: body`
	case "ssrf":
		requestPart = `http:
  - method: GET
    path:
      - "{{BaseURL}}/?url=http://{{interactsh-url}}"
    matchers:
      - type: word
        part: interactsh_protocol
        words:
          - "http"`
	case "unauth":
		requestPart = `http:
  - method: GET
    path:
      - "{{BaseURL}}/admin"
      - "{{BaseURL}}/admin/"
      - "{{BaseURL}}/manager"
    matchers-condition: and
    matchers:
      - type: status
        status:
          - 200
      - type: word
        words:
          - "admin"
          - "dashboard"
          - "管理"
        condition: or
        part: body`
	case "info-disclosure":
		requestPart = `http:
  - method: GET
    path:
      - "{{BaseURL}}/.git/config"
      - "{{BaseURL}}/.env"
      - "{{BaseURL}}/config.php.bak"
    matchers-condition: or
    matchers:
      - type: word
        words:
          - "[core]"
          - "DB_PASSWORD"
          - "<?php"
        part: body`
	default:
		requestPart = `http:
  - method: GET
    path:
      - "{{BaseURL}}/"
    matchers:
      - type: status
        status:
          - 200
      - type: word
        words:
          - "TODO: Add detection logic"
        part: body`
	}

	// 组装完整模板
	severity := "medium"
	if req.VulnType == "rce" || req.VulnType == "sqli" {
		severity = "high"
	} else if req.VulnType == "info-disclosure" {
		severity = "low"
	}

	description := req.Description
	if description == "" {
		description = "Auto-generated POC template"
	}

	template := fmt.Sprintf(`id: %s

info:
  name: %s
  author: cscan
  severity: %s
  description: |
    %s
  tags: custom,%s

%s
`, templateId, name, severity, description, req.VulnType, requestPart)

	return template
}

// AIConfigLogic AI配置逻辑
type AIConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAIConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AIConfigLogic {
	return &AIConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetConfig 获取AI配置
func (l *AIConfigLogic) GetConfig(workspaceId string) (*types.AIConfigGetResp, error) {
	configModel := model.NewAPIConfigModel(l.svcCtx.MongoDB, workspaceId)
	doc, err := configModel.FindByPlatform(l.ctx, "ai")
	if err != nil {
		// 如果没有配置，返回默认值
		return &types.AIConfigGetResp{
			Code: 0,
			Msg:  "success",
			Data: &types.AIConfig{
				Protocol: "anthropic",
				BaseUrl:  "http://127.0.0.1:8045",
				ApiKey:   "",
				Model:    "gemini-2.5-flash",
				Status:   "enable",
			},
		}, nil
	}

	// 解析存储的配置
	// Key 字段存储格式: protocol|baseUrl|model
	parts := strings.Split(doc.Key, "|")
	protocol := "anthropic"
	baseUrl := "http://127.0.0.1:8045"
	modelName := "gemini-2.5-flash"
	if len(parts) >= 3 {
		protocol = parts[0]
		baseUrl = parts[1]
		modelName = parts[2]
	}

	return &types.AIConfigGetResp{
		Code: 0,
		Msg:  "success",
		Data: &types.AIConfig{
			Id:         doc.Id.Hex(),
			Protocol:   protocol,
			BaseUrl:    baseUrl,
			ApiKey:     doc.Secret,
			Model:      modelName,
			Status:     doc.Status,
			CreateTime: doc.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime: doc.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	}, nil
}

// SaveConfig 保存AI配置
func (l *AIConfigLogic) SaveConfig(req *types.AIConfigSaveReq, workspaceId string) (*types.BaseResp, error) {
	configModel := model.NewAPIConfigModel(l.svcCtx.MongoDB, workspaceId)

	// 查找现有配置
	existing, err := configModel.FindByPlatform(l.ctx, "ai")

	// Key 字段存储格式: protocol|baseUrl|model
	keyValue := fmt.Sprintf("%s|%s|%s", req.Protocol, req.BaseUrl, req.Model)

	if err == nil && existing.Id.Hex() != "" {
		// 更新现有配置
		err = configModel.Update(l.ctx, existing.Id.Hex(), bson.M{
			"key":    keyValue,
			"secret": req.ApiKey,
			"status": "enable",
		})
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新配置失败: " + err.Error()}, nil
		}
	} else {
		// 创建新配置
		doc := &model.APIConfig{
			Platform: "ai",
			Key:      keyValue,
			Secret:   req.ApiKey,
			Status:   "enable",
		}
		err = configModel.Insert(l.ctx, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "保存配置失败: " + err.Error()}, nil
		}
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}
