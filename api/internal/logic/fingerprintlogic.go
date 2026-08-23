package logic

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/internal/scheduler"

	"github.com/google/uuid"
	wappalyzer "github.com/projectdiscovery/wappalyzergo"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"gopkg.in/yaml.v3"
)

// ==================== 指纹批量验证任务状态管理（内存，对标AI研判） ====================

var fingerprintBatchTasks sync.Map // taskId -> *fingerprintBatchTaskState

type fingerprintBatchTaskState struct {
	mu         sync.Mutex
	TaskId     string
	Url        string
	Scope      string
	Total      int64
	Completed  int64
	Matched    int64
	Status     string // running / completed / failed / stopped / stopping
	ErrMsg     string // 失败原因（Status=failed 时透出给前端）
	Results    []types.MatchedFingerprintInfo
	StopCh     chan struct{}
	CreateTime time.Time
}

// isHexString 检查字符串是否为十六进制字符串
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// FingerprintListLogic 指纹列表
type FingerprintListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintListLogic {
	return &FingerprintListLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *FingerprintListLogic) FingerprintList(req *types.FingerprintListReq) (*types.FingerprintListResp, error) {
	filter := bson.M{}

	if req.Keyword != "" {
		// 支持同时搜索name和ID
		// 检查keyword是否可能是ObjectID（24位十六进制字符串）
		keyword := strings.TrimSpace(req.Keyword)
		if len(keyword) == 24 && isHexString(keyword) {
			// 可能是ObjectID，同时搜索_id和name
			oid, err := primitive.ObjectIDFromHex(keyword)
			if err == nil {
				filter["$or"] = []bson.M{
					{"_id": oid},
					{"name": bson.M{"$regex": regexp.QuoteMeta(keyword), "$options": "i"}},
				}
			} else {
				filter["name"] = bson.M{"$regex": regexp.QuoteMeta(keyword), "$options": "i"}
			}
		} else {
			// 普通关键字搜索name
			filter["name"] = bson.M{"$regex": regexp.QuoteMeta(keyword), "$options": "i"}
		}
	}
	if req.Source != "" {
		filter["source"] = req.Source
	}
	if req.IsBuiltin != nil {
		filter["is_builtin"] = *req.IsBuiltin
	}
	if req.Enabled != nil {
		filter["enabled"] = *req.Enabled
	}

	total, _ := l.svcCtx.FingerprintModel.Count(l.ctx, filter)
	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
	docs, err := l.svcCtx.FingerprintModel.Find(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		return &types.FingerprintListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.Fingerprint, 0, len(docs))
	for _, doc := range docs {
		list = append(list, types.Fingerprint{
			Id:          doc.Id.Hex(),
			Name:        doc.Name,
			Category:    doc.Category,
			Website:     doc.Website,
			Icon:        doc.Icon,
			Description: doc.Description,
			Headers:     doc.Headers,
			Cookies:     doc.Cookies,
			HTML:        doc.HTML,
			Scripts:     doc.Scripts,
			ScriptSrc:   doc.ScriptSrc,
			JS:          doc.JS,
			Meta:        doc.Meta,
			CSS:         doc.CSS,
			URL:         doc.URL,
			Dom:         doc.Dom,
			Rule:        doc.Rule,
			Source:      doc.Source,
			Implies:     doc.Implies,
			Excludes:    doc.Excludes,
			CPE:         doc.CPE,
			IsBuiltin:   doc.IsBuiltin,
			Enabled:     doc.Enabled,
			CreateTime:  doc.CreateTime.Local().Format("2006-01-02 15:04:05"),
			UpdateTime:  doc.UpdateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.FingerprintListResp{
		Code:  0,
		Total: int(total),
		List:  list,
	}, nil
}

// FingerprintSaveLogic 保存指纹
type FingerprintSaveLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintSaveLogic {
	return &FingerprintSaveLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *FingerprintSaveLogic) FingerprintSave(req *types.FingerprintSaveReq) (*types.BaseResp, error) {
	// 设置默认来源
	source := req.Source
	if source == "" {
		source = "custom"
	}

	// 如果是主动指纹类型，需要同时保存到两个集合
	if req.Type == "active" && len(req.ActivePaths) > 0 {
		// 1. 保存/更新主动指纹（探测路径）到 ActiveFingerprintModel
		activeDoc := &model.ActiveFingerprint{
			Name:        req.Name,
			Paths:       req.ActivePaths,
			Description: req.Description,
			Enabled:     req.Enabled,
		}

		// 检查是否已存在同名主动指纹
		existingActive, _ := l.svcCtx.ActiveFingerprintModel.FindByName(l.ctx, req.Name)
		if existingActive != nil {
			// 更新
			if err := l.svcCtx.ActiveFingerprintModel.Update(l.ctx, existingActive.Id.Hex(), map[string]interface{}{
				"paths":       req.ActivePaths,
				"description": req.Description,
				"enabled":     req.Enabled,
			}); err != nil {
				return &types.BaseResp{Code: 500, Msg: "更新主动指纹失败: " + err.Error()}, nil
			}
		} else {
			// 新增
			if err := l.svcCtx.ActiveFingerprintModel.Insert(l.ctx, activeDoc); err != nil {
				return &types.BaseResp{Code: 500, Msg: "保存主动指纹失败: " + err.Error()}, nil
			}
		}
	}

	// 2. 保存被动指纹（匹配规则）到 FingerprintModel
	doc := &model.Fingerprint{
		Name:        req.Name,
		Website:     req.Website,
		Icon:        req.Icon,
		Description: req.Description,
		Rule:        req.Rule,
		Source:      source,
		Headers:     req.Headers,
		Cookies:     req.Cookies,
		HTML:        req.HTML,
		Scripts:     req.Scripts,
		Meta:        req.Meta,
		CSS:         req.CSS,
		URL:         req.URL,
		Implies:     req.Implies,
		Excludes:    req.Excludes,
		IsBuiltin:   false, // 用户保存的都是自定义指纹
		Enabled:     req.Enabled,
	}

	if req.Id != "" {
		// 更新
		update := bson.M{
			"name":        req.Name,
			"website":     req.Website,
			"icon":        req.Icon,
			"description": req.Description,
			"rule":        req.Rule,
			"source":      source,
			"headers":     req.Headers,
			"cookies":     req.Cookies,
			"html":        req.HTML,
			"scripts":     req.Scripts,
			"meta":        req.Meta,
			"css":         req.CSS,
			"url":         req.URL,
			"implies":     req.Implies,
			"excludes":    req.Excludes,
			"enabled":     req.Enabled,
		}
		if err := l.svcCtx.FingerprintModel.Update(l.ctx, req.Id, update); err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新失败: " + err.Error()}, nil
		}
	} else {
		// 新增
		if err := l.svcCtx.FingerprintModel.Insert(l.ctx, doc); err != nil {
			return &types.BaseResp{Code: 500, Msg: "保存失败: " + err.Error()}, nil
		}
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

// FingerprintDeleteLogic 删除指纹
type FingerprintDeleteLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintDeleteLogic {
	return &FingerprintDeleteLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *FingerprintDeleteLogic) FingerprintDelete(req *types.FingerprintDeleteReq) (*types.BaseResp, error) {
	// 检查是否为内置指纹
	fp, err := l.svcCtx.FingerprintModel.FindById(l.ctx, req.Id)
	if err != nil {
		logx.Errorf("FingerprintDelete: find fingerprint failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "查询指纹失败"}, nil
	}
	if fp == nil {
		return &types.BaseResp{Code: 404, Msg: "指纹不存在"}, nil
	}
	if fp.IsBuiltin {
		return &types.BaseResp{Code: 400, Msg: "内置指纹不能删除，只能禁用"}, nil
	}

	if err := l.svcCtx.FingerprintModel.Delete(l.ctx, req.Id); err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// FingerprintCategoriesLogic 获取指纹分类
type FingerprintCategoriesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintCategoriesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintCategoriesLogic {
	return &FingerprintCategoriesLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *FingerprintCategoriesLogic) FingerprintCategories() (*types.FingerprintCategoriesResp, error) {
	categories, _ := l.svcCtx.FingerprintModel.GetCategories(l.ctx)
	stats, _ := l.svcCtx.FingerprintModel.GetStats(l.ctx)

	// 从 ActiveFingerprintModel 获取主动探测指纹数量（独立指标，不覆盖 Fingerprint 集合的 active 统计）
	activeStats, _ := l.svcCtx.ActiveFingerprintModel.GetStats(l.ctx)
	if activeStats != nil {
		stats["activeDetected"] = activeStats["total"]
	}

	return &types.FingerprintCategoriesResp{
		Code:       0,
		Categories: categories,
		Stats:      stats,
	}, nil
}

// FingerprintUpdateEnabledLogic 更新指纹启用状态
type FingerprintUpdateEnabledLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintUpdateEnabledLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintUpdateEnabledLogic {
	return &FingerprintUpdateEnabledLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *FingerprintUpdateEnabledLogic) UpdateEnabled(id string, enabled bool) (*types.BaseResp, error) {
	if err := l.svcCtx.FingerprintModel.Update(l.ctx, id, bson.M{"enabled": enabled}); err != nil {
		return &types.BaseResp{Code: 500, Msg: "更新失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "更新成功"}, nil
}

// FingerprintBatchUpdateEnabledLogic 批量更新指纹启用状态
type FingerprintBatchUpdateEnabledLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintBatchUpdateEnabledLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintBatchUpdateEnabledLogic {
	return &FingerprintBatchUpdateEnabledLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *FingerprintBatchUpdateEnabledLogic) BatchUpdateEnabled(ids []string, enabled bool, all bool) (*types.BaseResp, error) {
	var filter bson.M

	if all {
		// 操作全部自定义指纹
		filter = bson.M{"is_builtin": false}
	} else if len(ids) > 0 {
		// 操作指定ID列表
		oids := make([]primitive.ObjectID, 0, len(ids))
		for _, id := range ids {
			oid, err := primitive.ObjectIDFromHex(id)
			if err != nil {
				continue
			}
			oids = append(oids, oid)
		}
		if len(oids) == 0 {
			return &types.BaseResp{Code: 400, Msg: "无有效的指纹ID"}, nil
		}
		filter = bson.M{"_id": bson.M{"$in": oids}}
	} else {
		return &types.BaseResp{Code: 400, Msg: "请指定要操作的指纹"}, nil
	}

	count, err := l.svcCtx.FingerprintModel.BatchUpdateEnabled(l.ctx, filter, enabled)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "批量更新失败: " + err.Error()}, nil
	}

	action := "启用"
	if !enabled {
		action = "禁用"
	}
	return &types.BaseResp{Code: 0, Msg: fmt.Sprintf("已%s %d 条指纹", action, count)}, nil
}

// FingerprintImportLogic 导入指纹
type FingerprintImportLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintImportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintImportLogic {
	return &FingerprintImportLogic{ctx: ctx, svcCtx: svcCtx}
}

// ARLFingerprint ARL格式指纹 (YAML格式: name + rule)
type ARLFingerprint struct {
	Name string `yaml:"name" json:"name"`
	Rule string `yaml:"rule" json:"rule"`
}

// ARLFingerJSON ARL finger.json格式 (JSON格式: cms + method + location + keyword)
type ARLFingerJSON struct {
	CMS      string   `json:"cms"`
	Method   string   `json:"method"`
	Location string   `json:"location"`
	Keyword  []string `json:"keyword"`
}

// ARLFingerJSONWrapper finger.json的包装结构
type ARLFingerJSONWrapper struct {
	Fingerprint []ARLFingerJSON `json:"fingerprint"`
}

func (l *FingerprintImportLogic) FingerprintImport(req *types.FingerprintImportReq) (*types.FingerprintImportResp, error) {
	if req.Content == "" {
		return &types.FingerprintImportResp{Code: 400, Msg: "内容不能为空"}, nil
	}

	// 预处理内容：去除BOM头和多余空白
	content := strings.TrimSpace(req.Content)
	content = strings.TrimPrefix(content, "\xef\xbb\xbf") // UTF-8 BOM
	content = strings.TrimPrefix(content, "\xff\xfe")     // UTF-16 LE BOM
	content = strings.TrimPrefix(content, "\xfe\xff")     // UTF-16 BE BOM

	var docs []*model.Fingerprint
	var skipped int
	var parseErr error

	// 自动检测格式
	format := req.Format
	if format == "" || format == "auto" {
		format = detectFingerFormat(content)
	}

	switch format {
	case "wappalyzer":
		// 解析Wappalyzer technologies.json格式
		docs, skipped, parseErr = l.parseWappalyzerJSON(content, req.IsBuiltin)

	case "arl-json", "finger-json":
		// 解析ARL finger.json格式: {"fingerprint": [{cms, method, location, keyword}]}
		docs, skipped, parseErr = l.parseARLFingerJSON(content)

	case "arl-yaml", "arl", "finger-yaml":
		// 解析ARL finger.yml格式: [{name, rule}]
		docs, skipped, parseErr = l.parseARLFingerYAML(content)

	default:
		// 尝试自动检测并解析
		docs, skipped, parseErr = l.parseAutoDetect(content)
	}

	if parseErr != nil {
		preview := content
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		return &types.FingerprintImportResp{
			Code: 400,
			Msg:  fmt.Sprintf("解析失败: %v\n\n文件预览:\n%s", parseErr, preview),
		}, nil
	}

	if len(docs) == 0 {
		return &types.FingerprintImportResp{
			Code:    400,
			Msg:     fmt.Sprintf("未解析到有效指纹数据，跳过 %d 条", skipped),
			Skipped: skipped,
		}, nil
	}

	// 批量插入
	insertedCount, matchedCount, err := l.svcCtx.FingerprintModel.BulkUpsert(l.ctx, docs)
	if err != nil {
		return &types.FingerprintImportResp{Code: 500, Msg: "批量导入失败: " + err.Error()}, nil
	}

	// insertedCount 是新插入的数量，matchedCount 是已存在被更新的数量（视为重复跳过）
	totalSkipped := skipped + matchedCount

	return &types.FingerprintImportResp{
		Code:     0,
		Msg:      fmt.Sprintf("导入完成: 新增 %d 个, 跳过 %d 个（重复）", insertedCount, totalSkipped),
		Imported: insertedCount,
		Skipped:  totalSkipped,
	}, nil
}

// detectFingerFormat 自动检测指纹文件格式
func detectFingerFormat(content string) string {
	content = strings.TrimSpace(content)
	// JSON格式检测
	if strings.HasPrefix(content, "{") {
		// 检查是否是finger.json格式
		if strings.Contains(content, `"fingerprint"`) && strings.Contains(content, `"cms"`) {
			return "arl-json"
		}
		return "json"
	}
	// YAML数组格式检测
	if strings.HasPrefix(content, "- ") || strings.HasPrefix(content, "-\n") {
		if strings.Contains(content, "rule:") || strings.Contains(content, "rule=") {
			return "arl-yaml"
		}
	}
	return "arl-yaml" // 默认尝试YAML格式
}

// parseARLFingerJSON 解析ARL finger.json格式
func (l *FingerprintImportLogic) parseARLFingerJSON(content string) ([]*model.Fingerprint, int, error) {
	var wrapper ARLFingerJSONWrapper
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		return nil, 0, fmt.Errorf("JSON解析错误: %v", err)
	}

	var docs []*model.Fingerprint
	var skipped int
	// 使用 name+rule 作为去重key，只有完全相同才跳过
	seen := make(map[string]bool)

	for _, fp := range wrapper.Fingerprint {
		if fp.CMS == "" || len(fp.Keyword) == 0 {
			skipped++
			continue
		}

		// 构建ARL格式规则
		rule := buildARLRule(fp.Location, fp.Method, fp.Keyword)
		if rule == "" {
			skipped++
			continue
		}

		name := strings.TrimSpace(fp.CMS)
		// 去重key: name + rule
		key := name + "|" + rule
		if seen[key] {
			skipped++
			continue
		}
		seen[key] = true

		doc := &model.Fingerprint{
			Name:      name,
			Rule:      rule,
			Source:    "custom",
			IsBuiltin: false,
			Enabled:   true,
		}
		docs = append(docs, doc)
	}

	return docs, skipped, nil
}

// parseARLFingerYAML 解析ARL finger.yml格式
func (l *FingerprintImportLogic) parseARLFingerYAML(content string) ([]*model.Fingerprint, int, error) {
	var fingerprints []ARLFingerprint
	var parseErr error = yaml.Unmarshal([]byte(content), &fingerprints)

	// 方式2: 如果解析失败或为空，尝试解析为map格式 {key: [{name, rule}]}
	if parseErr != nil || len(fingerprints) == 0 {
		var wrapper map[string][]ARLFingerprint
		if err2 := yaml.Unmarshal([]byte(content), &wrapper); err2 == nil {
			for _, fps := range wrapper {
				fingerprints = append(fingerprints, fps...)
			}
		}
	}

	// 方式3: 尝试解析为通用map数组
	if len(fingerprints) == 0 {
		var rawList []map[string]interface{}
		if err3 := yaml.Unmarshal([]byte(content), &rawList); err3 == nil {
			for _, item := range rawList {
				name := getStringField(item, "name", "Name", "NAME")
				rule := getStringField(item, "rule", "Rule", "RULE")
				if name != "" {
					fingerprints = append(fingerprints, ARLFingerprint{Name: name, Rule: rule})
				}
			}
		}
	}

	// 方式4: 尝试解析为 AppName: [rules...] 格式（标准YAML解析）
	// 格式示例:
	// NetGain_Enterprise_Manager:
	// - 'title="NetGain EM" || title="NetGain Enterprise Manager"'
	if len(fingerprints) == 0 {
		var appRulesMap map[string][]string
		if err4 := yaml.Unmarshal([]byte(content), &appRulesMap); err4 == nil && len(appRulesMap) > 0 {
			for appName, rules := range appRulesMap {
				appName = strings.TrimSpace(appName)
				if appName == "" {
					continue
				}
				for _, rule := range rules {
					rule = strings.TrimSpace(rule)
					if rule == "" {
						continue
					}
					fingerprints = append(fingerprints, ARLFingerprint{Name: appName, Rule: rule})
				}
			}
		}
	}

	// 方式5: 手动逐行解析 AppName: [rules...] 格式，支持重复key
	// 当YAML解析因重复key失败时使用此方式
	if len(fingerprints) == 0 {
		fingerprints = parseAppRulesManually(content)
	}

	if len(fingerprints) == 0 {
		if parseErr != nil {
			return nil, 0, parseErr
		}
		return nil, 0, fmt.Errorf("未解析到任何指纹数据")
	}

	var docs []*model.Fingerprint
	var skipped int
	// 使用 name+rule 作为去重key，只有完全相同才跳过
	seen := make(map[string]bool)

	for _, fp := range fingerprints {
		if fp.Name == "" {
			skipped++
			continue
		}
		if fp.Rule == "" {
			skipped++
			continue
		}

		name := strings.TrimSpace(fp.Name)
		rule := strings.TrimSpace(fp.Rule)

		// 去重key: name + rule
		key := name + "|" + rule
		if seen[key] {
			skipped++
			continue
		}
		seen[key] = true

		doc := &model.Fingerprint{
			Name:      name,
			Rule:      rule,
			Source:    "custom",
			IsBuiltin: false,
			Enabled:   true,
		}
		docs = append(docs, doc)
	}

	return docs, skipped, nil
}

// parseAppRulesManually 手动逐行解析 AppName: [rules...] 格式
// 支持重复的应用名称（YAML标准解析会报错）
func parseAppRulesManually(content string) []ARLFingerprint {
	var fingerprints []ARLFingerprint
	lines := strings.Split(content, "\n")

	var currentAppName string

	for _, line := range lines {
		// 跳过空行和注释
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
			continue
		}

		// 检查是否是应用名称行（不以 - 开头，以 : 结尾或包含 :）
		if !strings.HasPrefix(trimmedLine, "-") {
			// 可能是应用名称
			if idx := strings.Index(trimmedLine, ":"); idx > 0 {
				appName := strings.TrimSpace(trimmedLine[:idx])
				if appName != "" {
					currentAppName = appName
				}
			}
			continue
		}

		// 规则行（以 - 开头）
		if currentAppName != "" && strings.HasPrefix(trimmedLine, "-") {
			rule := strings.TrimPrefix(trimmedLine, "-")
			rule = strings.TrimSpace(rule)
			// 去除引号包裹
			if (strings.HasPrefix(rule, "'") && strings.HasSuffix(rule, "'")) ||
				(strings.HasPrefix(rule, "\"") && strings.HasSuffix(rule, "\"")) {
				rule = rule[1 : len(rule)-1]
			}
			if rule != "" {
				fingerprints = append(fingerprints, ARLFingerprint{
					Name: currentAppName,
					Rule: rule,
				})
			}
		}
	}

	return fingerprints
}

// parseAutoDetect 自动检测格式并解析
func (l *FingerprintImportLogic) parseAutoDetect(content string) ([]*model.Fingerprint, int, error) {
	// 先尝试JSON格式
	if strings.HasPrefix(strings.TrimSpace(content), "{") {
		docs, skipped, err := l.parseARLFingerJSON(content)
		if err == nil && len(docs) > 0 {
			return docs, skipped, nil
		}
	}

	// 再尝试YAML格式
	return l.parseARLFingerYAML(content)
}

// buildARLRule 根据location、method和keyword构建ARL格式规则
func buildARLRule(location, method string, keywords []string) string {
	if len(keywords) == 0 {
		return ""
	}

	location = strings.ToLower(location)
	method = strings.ToLower(method)

	var rules []string
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}

		var rule string
		switch {
		case method == "faviconhash" || method == "icon_hash":
			// favicon hash匹配
			rule = fmt.Sprintf(`icon_hash="%s"`, kw)
		case location == "title":
			rule = fmt.Sprintf(`title="%s"`, kw)
		case location == "header":
			rule = fmt.Sprintf(`header="%s"`, kw)
		case location == "body" || location == "":
			rule = fmt.Sprintf(`body="%s"`, kw)
		default:
			rule = fmt.Sprintf(`body="%s"`, kw)
		}
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return ""
	}

	// 多个keyword之间是AND关系（同一条规则内的多个关键字都要匹配）
	return strings.Join(rules, " && ")
}

// FingerprintImportFromFileLogic 从文件导入指纹
type FingerprintImportFromFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintImportFromFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintImportFromFileLogic {
	return &FingerprintImportFromFileLogic{ctx: ctx, svcCtx: svcCtx}
}

// FingerprintImportFromFile 从指定目录导入指纹文件
func (l *FingerprintImportFromFileLogic) FingerprintImportFromFile(req *types.FingerprintImportFromFileReq) (*types.FingerprintImportResp, error) {
	if req.Path == "" {
		return &types.FingerprintImportResp{Code: 400, Msg: "路径不能为空"}, nil
	}

	// 检查路径是否存在
	info, err := os.Stat(req.Path)
	if err != nil {
		return &types.FingerprintImportResp{Code: 400, Msg: "路径不存在: " + err.Error()}, nil
	}

	var totalImported, totalSkipped int
	var files []string

	if info.IsDir() {
		// 扫描目录下的指纹文件
		entries, err := os.ReadDir(req.Path)
		if err != nil {
			return &types.FingerprintImportResp{Code: 500, Msg: "读取目录失败: " + err.Error()}, nil
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			// 支持 .json, .yml, .yaml 文件
			if ext == ".json" || ext == ".yml" || ext == ".yaml" {
				files = append(files, filepath.Join(req.Path, name))
			}
		}
	} else {
		files = []string{req.Path}
	}

	if len(files) == 0 {
		return &types.FingerprintImportResp{Code: 400, Msg: "未找到指纹文件（支持 .json, .yml, .yaml）"}, nil
	}

	importLogic := NewFingerprintImportLogic(l.ctx, l.svcCtx)
	var results []string

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			results = append(results, fmt.Sprintf("%s: 读取失败 - %v", filepath.Base(file), err))
			continue
		}

		resp, _ := importLogic.FingerprintImport(&types.FingerprintImportReq{
			Content: string(content),
			Format:  "auto",
		})

		if resp.Code == 0 {
			totalImported += resp.Imported
			totalSkipped += resp.Skipped
			results = append(results, fmt.Sprintf("%s: 新增 %d, 跳过 %d", filepath.Base(file), resp.Imported, resp.Skipped))
		} else {
			results = append(results, fmt.Sprintf("%s: %s", filepath.Base(file), resp.Msg))
		}
	}

	return &types.FingerprintImportResp{
		Code:     0,
		Msg:      fmt.Sprintf("导入完成: 共处理 %d 个文件, 新增 %d 条, 跳过 %d 条\n%s", len(files), totalImported, totalSkipped, strings.Join(results, "\n")),
		Imported: totalImported,
		Skipped:  totalSkipped,
	}, nil
}

// getStringField 从map中获取字符串字段，支持多个可能的字段名
func getStringField(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// FingerprintClearCustomLogic 清空自定义指纹
type FingerprintClearCustomLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintClearCustomLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintClearCustomLogic {
	return &FingerprintClearCustomLogic{ctx: ctx, svcCtx: svcCtx}
}

// FingerprintClearCustom 清空自定义指纹
func (l *FingerprintClearCustomLogic) FingerprintClearCustom(req *types.FingerprintClearCustomReq) (*types.FingerprintClearCustomResp, error) {
	var deleted int64
	var err error

	if req.Source != "" {
		// 按来源清空
		deleted, err = l.svcCtx.FingerprintModel.DeleteBySource(l.ctx, req.Source)
	} else {
		// 清空所有自定义指纹（非内置）
		deleted, err = l.svcCtx.FingerprintModel.DeleteCustom(l.ctx)
	}

	if err != nil {
		return &types.FingerprintClearCustomResp{Code: 500, Msg: "清空失败: " + err.Error()}, nil
	}

	msg := fmt.Sprintf("已清空 %d 条自定义指纹", deleted)
	if req.Source != "" {
		msg = fmt.Sprintf("已清空来源为 %s 的 %d 条指纹", req.Source, deleted)
	}

	return &types.FingerprintClearCustomResp{
		Code:    0,
		Msg:     msg,
		Deleted: int(deleted),
	}, nil
}

// WappalyzerWrapper wappalyzergo fingerprints_data.json的包装结构
type WappalyzerWrapper struct {
	Apps map[string]interface{} `json:"apps"`
}

// parseWappalyzerJSON 解析Wappalyzer fingerprints_data.json格式
// 支持 {"apps": {...}} 包装格式和直接的 {...} 格式
func (l *FingerprintImportLogic) parseWappalyzerJSON(content string, isBuiltin bool) ([]*model.Fingerprint, int, error) {
	var technologies map[string]interface{}

	// 先尝试解析为 {"apps": {...}} 格式
	var wrapper WappalyzerWrapper
	if err := json.Unmarshal([]byte(content), &wrapper); err == nil && wrapper.Apps != nil {
		technologies = wrapper.Apps
	} else {
		// 尝试直接解析为 {...} 格式
		if err := json.Unmarshal([]byte(content), &technologies); err != nil {
			return nil, 0, fmt.Errorf("JSON解析错误: %v", err)
		}
	}

	var docs []*model.Fingerprint
	var skipped int

	for name, techRaw := range technologies {
		if name == "" {
			skipped++
			continue
		}

		// 将interface{}转换为map
		techMap, ok := techRaw.(map[string]interface{})
		if !ok {
			skipped++
			continue
		}

		doc := &model.Fingerprint{
			Name:      name,
			Source:    "wappalyzer",
			IsBuiltin: isBuiltin,
			Enabled:   true,
		}

		// 解析简单字符串字段
		if v, ok := techMap["website"].(string); ok {
			doc.Website = v
		}
		if v, ok := techMap["icon"].(string); ok {
			doc.Icon = v
		}
		if v, ok := techMap["cpe"].(string); ok {
			doc.CPE = v
		}

		// 解析Headers
		if v, ok := techMap["headers"]; ok && v != nil {
			doc.Headers = parseMapOrString(v)
		}

		// 解析Cookies
		if v, ok := techMap["cookies"]; ok && v != nil {
			doc.Cookies = parseMapOrString(v)
		}

		// 解析HTML
		if v, ok := techMap["html"]; ok && v != nil {
			doc.HTML = parseArrayOrString(v)
		}

		// 解析Scripts
		if v, ok := techMap["scripts"]; ok && v != nil {
			doc.Scripts = parseArrayOrString(v)
		}

		// 解析ScriptSrc
		if v, ok := techMap["scriptSrc"]; ok && v != nil {
			doc.ScriptSrc = parseArrayOrString(v)
		}

		// 解析JS
		if v, ok := techMap["js"]; ok && v != nil {
			doc.JS = parseMapOrString(v)
		}

		// 解析Meta
		if v, ok := techMap["meta"]; ok && v != nil {
			doc.Meta = parseMapOrString(v)
		}

		// 解析CSS
		if v, ok := techMap["css"]; ok && v != nil {
			doc.CSS = parseArrayOrString(v)
		}

		// 解析URL
		if v, ok := techMap["url"]; ok && v != nil {
			doc.URL = parseArrayOrString(v)
		}

		// 解析Dom
		if v, ok := techMap["dom"]; ok && v != nil {
			if domStr, err := json.Marshal(v); err == nil {
				doc.Dom = string(domStr)
			}
		}

		// 解析Implies
		if v, ok := techMap["implies"]; ok && v != nil {
			doc.Implies = parseArrayOrString(v)
		}

		// 解析Excludes
		if v, ok := techMap["excludes"]; ok && v != nil {
			doc.Excludes = parseArrayOrString(v)
		}

		// 解析cats
		// if v, ok := techMap["cats"]; ok && v != nil {
		// 	cats := parseIntArray(v)
		// }

		docs = append(docs, doc)
	}

	return docs, skipped, nil
}

// parseMapOrString 解析可能是map或string的字段
func parseMapOrString(v interface{}) map[string]string {
	result := make(map[string]string)
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
	case string:
		result[""] = val
	}
	return result
}

// parseArrayOrString 解析可能是数组或字符串的字段
func parseArrayOrString(v interface{}) []string {
	var result []string
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	case string:
		result = append(result, val)
	}
	return result
}

// FingerprintValidateLogic 验证指纹
type FingerprintValidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintValidateLogic {
	return &FingerprintValidateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// FingerprintValidate 验证单个指纹是否能匹配目标URL（通过RPC下发给Worker执行）
func (l *FingerprintValidateLogic) FingerprintValidate(req *types.FingerprintValidateReq) (*types.FingerprintValidateResp, error) {
	if req.Url == "" {
		return &types.FingerprintValidateResp{Code: 400, Msg: "URL不能为空"}, nil
	}
	if req.Id == "" {
		return &types.FingerprintValidateResp{Code: 400, Msg: "指纹ID不能为空"}, nil
	}

	// 从数据库获取指纹（验证存在性）
	fp, err := l.svcCtx.FingerprintModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("FingerprintValidate: find fingerprint failed, id=%s, error=%v", req.Id, err)
		return &types.FingerprintValidateResp{Code: 500, Msg: "查询指纹失败"}, nil
	}
	if fp == nil {
		return &types.FingerprintValidateResp{Code: 404, Msg: "指纹不存在"}, nil
	}

	l.Logger.Infof("FingerprintValidate: fingerprintId=%s, name=%s, url=%s", req.Id, fp.Name, req.Url)

	// 检查在线 Worker
	if err := checkOnlineWorkers(l.ctx, l.svcCtx); err != nil {
		return &types.FingerprintValidateResp{Code: 500, Msg: err.Error()}, nil
	}

	// 直接入队指纹验证任务
	taskId := uuid.New().String()
	taskConfig := map[string]interface{}{
		"taskType":      "fingerprint_validate",
		"url":           req.Url,
		"fingerprintId": req.Id,
		"timeout":       30,
	}
	configBytes, _ := json.Marshal(taskConfig)

	task := &scheduler.TaskInfo{
		TaskId:     taskId,
		MainTaskId: taskId,
		TaskName:   "指纹验证",
		Config:     string(configBytes),
		Priority:   2,
	}

	if err := l.svcCtx.Scheduler.PushTask(l.ctx, task); err != nil {
		l.Logger.Errorf("FingerprintValidate: push task failed, taskId=%s, error=%v", taskId, err)
		return &types.FingerprintValidateResp{Code: 500, Msg: "任务下发失败"}, nil
	}

	persistTaskInfo(l.ctx, l.svcCtx, taskId, taskConfig)

	// 同步等待结果：轮询任务状态（最多30秒）
	result, err := pollFingerprintValidateResult(l.ctx, l.svcCtx, taskId, 30*time.Second)
	if err != nil {
		l.Logger.Errorf("FingerprintValidate: wait result failed, taskId=%s, error=%v", taskId, err)
		return &types.FingerprintValidateResp{Code: 500, Msg: "等待验证结果超时: " + err.Error()}, nil
	}
	if result.Error != "" {
		return &types.FingerprintValidateResp{Code: 500, Msg: result.Error}, nil
	}

	return &types.FingerprintValidateResp{
		Code:    0,
		Msg:     "验证完成",
		Matched: result.Matched,
		Details: result.Details,
	}, nil
}

// pollFingerprintValidateResult 轮询等待指纹验证结果（通用函数）
func pollFingerprintValidateResult(ctx context.Context, svcCtx *svc.ServiceContext, taskId string, timeout time.Duration) (*WorkerFingerprintResult, error) {
	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		// 从Redis读取任务状态（Worker通过UpdateTask接口写入 cscan:task:status:{taskId}）
		statusKey := "cscan:task:status:" + taskId
		val, err := svcCtx.RedisClient.Get(ctx, statusKey).Result()
		if err != nil {
			// key不存在，继续等待
			continue
		}

		var statusData struct {
			State  string `json:"state"`
			Result string `json:"result"`
		}
		if err := json.Unmarshal([]byte(val), &statusData); err != nil {
			continue
		}

		if statusData.State == "SUCCESS" || statusData.State == "FAILURE" {
			// 解析result字段（Worker写入的JSON）
			var resultWrapper struct {
				Status string                  `json:"status"`
				Result WorkerFingerprintResult `json:"result"`
				Error  string                  `json:"error"`
			}
			if err := json.Unmarshal([]byte(statusData.Result), &resultWrapper); err != nil {
				return &WorkerFingerprintResult{Error: "解析结果失败: " + err.Error()}, nil
			}
			if resultWrapper.Error != "" {
				resultWrapper.Result.Error = resultWrapper.Error
			}
			return &resultWrapper.Result, nil
		}
	}

	return nil, fmt.Errorf("等待验证结果超时(%v)", timeout)
}

// WorkerFingerprintResult Worker返回的指纹验证结果
type WorkerFingerprintResult struct {
	Matched      bool              `json:"matched"`
	Details      string            `json:"details"`
	Error        string            `json:"error"`
	MatchedInfos []WorkerMatchedFp `json:"matchedInfos,omitempty"` // 批量验证匹配详情
	TotalScanned int               `json:"totalScanned,omitempty"` // 批量验证扫描总数
}

// WorkerMatchedFp Worker返回的批量验证中匹配的指纹信息
type WorkerMatchedFp struct {
	Id                string `json:"id"`
	Name              string `json:"name"`
	IsBuiltin         bool   `json:"isBuiltin"`
	IsActive          bool   `json:"isActive"`
	MatchedConditions string `json:"matchedConditions"`
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// SingleFingerprintEngine 单指纹匹配引擎（用于验证）
type SingleFingerprintEngine struct {
	fp *model.Fingerprint
}

func NewSingleFingerprintEngine(fp *model.Fingerprint) *SingleFingerprintEngine {
	return &SingleFingerprintEngine{fp: fp}
}

// MatchWithDetails 执行匹配并返回匹配的条件详情
func (e *SingleFingerprintEngine) MatchWithDetails(data *FingerprintData, fps ...*model.Fingerprint) (bool, []string) {
	var fp *model.Fingerprint
	if len(fps) > 0 && fps[0] != nil {
		fp = fps[0]
	} else {
		fp = e.fp
	}
	if fp == nil {
		return false, nil
	}

	// 优先使用Rule字段（ARL格式规则语法）
	if fp.Rule != "" {
		return matchRuleWithDetails(fp.Rule, data)
	}

	// 然后尝试ARL webapp.json格式规则（HTML/Headers直接包含匹配，OR关系）
	if matched, conditions := matchARLWebappRulesWithDetails(fp, data); matched {
		return matched, conditions
	}

	// 最后使用Wappalyzer格式规则（正则表达式，AND关系）
	matched, conditions := matchWappalyzerRulesWithDetails(fp, data)
	return matched, conditions
}

// FingerprintData 用于指纹匹配的数据
type FingerprintData struct {
	Title        string
	Body         string
	BodyBytes    []byte
	Headers      map[string][]string
	HeaderString string
	Server       string
	URL          string
	FaviconHash  string
	Cookies      string
}

// extractBaseUrl 从URL中提取基础部分（scheme://host:port）
func extractBaseUrl(rawUrl string) string {
	rawUrl = strings.TrimSpace(rawUrl)
	if rawUrl == "" {
		return ""
	}
	if !strings.Contains(rawUrl, "://") {
		rawUrl = "http://" + rawUrl
	}
	schemeEnd := strings.Index(rawUrl, "://")
	rest := rawUrl[schemeEnd+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		return rawUrl
	}
	return rawUrl[:schemeEnd+3+slashIdx]
}

// fetchFingerprintData 请求URL获取指纹匹配数据
func fetchFingerprintData(targetUrl string) (*FingerprintData, error) {
	// 与扫描器保持一致：始终使用基础URL（scheme://host:port）进行请求
	// 扫描器在根URL上检测指纹，如果传入带路径的URL（如登录页），会导致指纹头丢失
	targetUrl = extractBaseUrl(targetUrl)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		// 与扫描器保持一致：最多跟随3次重定向后停止，使用重定向响应进行匹配
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	resp, err := client.Get(targetUrl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	body := string(bodyBytes)

	// 提取标题
	title := ""
	titleRe := regexp.MustCompile(`(?i)<title[^>]*>([^<]*)</title>`)
	if matches := titleRe.FindStringSubmatch(body); len(matches) > 1 {
		title = strings.TrimSpace(matches[1])
	}

	// 构建header字符串
	var headerStr strings.Builder
	for key, values := range resp.Header {
		for _, v := range values {
			headerStr.WriteString(key)
			headerStr.WriteString(": ")
			headerStr.WriteString(v)
			headerStr.WriteString("\n")
		}
	}

	// 获取favicon并计算MMH3 hash
	faviconHash := fetchFaviconHash(targetUrl, body, client)

	return &FingerprintData{
		Title:        title,
		Body:         body,
		BodyBytes:    bodyBytes,
		Headers:      resp.Header,
		HeaderString: headerStr.String(),
		Server:       resp.Header.Get("Server"),
		URL:          targetUrl,
		FaviconHash:  faviconHash,
		Cookies:      resp.Header.Get("Set-Cookie"),
	}, nil
}

// fetchFaviconHash 获取favicon并计算MMH3 hash
func fetchFaviconHash(baseUrl, body string, client *http.Client) string {
	// 尝试从HTML中提取favicon路径
	faviconUrl := ""

	// 1. 尝试从link标签获取
	linkRe := regexp.MustCompile(`(?i)<link[^>]*rel=["'](?:shortcut )?icon["'][^>]*href=["']([^"']+)["']`)
	if matches := linkRe.FindStringSubmatch(body); len(matches) > 1 {
		faviconUrl = matches[1]
	}
	// 也尝试href在rel前面的情况
	if faviconUrl == "" {
		linkRe2 := regexp.MustCompile(`(?i)<link[^>]*href=["']([^"']+)["'][^>]*rel=["'](?:shortcut )?icon["']`)
		if matches := linkRe2.FindStringSubmatch(body); len(matches) > 1 {
			faviconUrl = matches[1]
		}
	}

	// 2. 如果没找到，使用默认路径
	if faviconUrl == "" {
		faviconUrl = "/favicon.ico"
	}

	// 3. 处理相对路径
	if !strings.HasPrefix(faviconUrl, "http") {
		// 解析baseUrl
		if strings.HasPrefix(faviconUrl, "//") {
			faviconUrl = "https:" + faviconUrl
		} else if strings.HasPrefix(faviconUrl, "/") {
			// 绝对路径
			u, err := parseBaseUrl(baseUrl)
			if err == nil {
				faviconUrl = u + faviconUrl
			}
		} else {
			// 相对路径
			u, err := parseBaseUrl(baseUrl)
			if err == nil {
				faviconUrl = u + "/" + faviconUrl
			}
		}
	}

	// 4. 请求favicon
	resp, err := client.Get(faviconUrl)
	if err != nil {
		return "(获取失败: " + err.Error() + ")"
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Sprintf("(HTTP %d)", resp.StatusCode)
	}

	faviconBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "(读取失败)"
	}

	if len(faviconBytes) == 0 {
		return "(空文件)"
	}

	// 5. 计算MMH3 hash (Shodan风格)
	// 使用与扫描器一致的算法（无换行符）
	hash := calculateMMH3HashSimple(faviconBytes)
	return hash
}

// parseBaseUrl 解析URL获取基础部分 (scheme://host:port)
func parseBaseUrl(rawUrl string) (string, error) {
	// 简单解析
	if idx := strings.Index(rawUrl, "://"); idx > 0 {
		scheme := rawUrl[:idx]
		rest := rawUrl[idx+3:]
		// 找到第一个/
		if slashIdx := strings.Index(rest, "/"); slashIdx > 0 {
			return scheme + "://" + rest[:slashIdx], nil
		}
		return scheme + "://" + rest, nil
	}
	return "", fmt.Errorf("invalid url")
}

// calculateMMH3HashSimple 计算Shodan风格的MMH3 favicon hash（简化版，无换行）
// 与扫描器 scanner/fingerprint.go 中的 CalculateMMH3Hash 算法一致
func calculateMMH3HashSimple(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// 直接使用 base64 编码，不添加换行符（与扫描器算法一致）
	b64 := base64.StdEncoding.EncodeToString(data)
	hash := mmh3Hash32([]byte(b64))
	return fmt.Sprintf("%d", int32(hash))
}

// mmh3Hash32 MurmurHash3 32位实现
func mmh3Hash32(data []byte) uint32 {
	const (
		c1 = 0xcc9e2d51
		c2 = 0x1b873593
		r1 = 15
		r2 = 13
		m  = 5
		n  = 0xe6546b64
	)

	length := len(data)
	h := uint32(0) // seed = 0

	// 处理4字节块
	nblocks := length / 4
	for i := 0; i < nblocks; i++ {
		k := uint32(data[i*4]) | uint32(data[i*4+1])<<8 | uint32(data[i*4+2])<<16 | uint32(data[i*4+3])<<24
		k *= c1
		k = (k << r1) | (k >> (32 - r1))
		k *= c2

		h ^= k
		h = (h << r2) | (h >> (32 - r2))
		h = h*m + n
	}

	// 处理剩余字节
	tail := data[nblocks*4:]
	var k uint32
	switch len(tail) {
	case 3:
		k ^= uint32(tail[2]) << 16
		fallthrough
	case 2:
		k ^= uint32(tail[1]) << 8
		fallthrough
	case 1:
		k ^= uint32(tail[0])
		k *= c1
		k = (k << r1) | (k >> (32 - r1))
		k *= c2
		h ^= k
	}

	// 最终混合
	h ^= uint32(length)
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16

	return h
}

// matchRuleWithDetails 匹配ARL格式规则并返回匹配的条件
func matchRuleWithDetails(rule string, data *FingerprintData) (bool, []string) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false, nil
	}

	logx.Infof("matchRuleWithDetails: rule=%s", rule)

	// 处理OR逻辑 (||)
	parts := splitByOperator(rule, "||")
	logx.Infof("matchRuleWithDetails: OR split result count=%d, parts=%v", len(parts), parts)
	if len(parts) > 1 {
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			matched, conditions := matchRuleAndWithDetails(part, data)
			if matched {
				return true, conditions
			}
		}
		return false, nil
	}

	return matchRuleAndWithDetails(rule, data)
}

func matchRuleAndWithDetails(rule string, data *FingerprintData) (bool, []string) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false, nil
	}

	var matchedConditions []string

	parts := splitByOperator(rule, "&&")
	logx.Infof("matchRuleAndWithDetails: rule=%s, AND split count=%d, parts=%v", rule, len(parts), parts)
	if len(parts) > 1 {
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			matched, detail := matchSingleConditionWithDetails(part, data)
			if !matched {
				return false, nil
			}
			matchedConditions = append(matchedConditions, detail)
		}
		return true, matchedConditions
	}

	matched, detail := matchSingleConditionWithDetails(rule, data)
	if matched {
		return true, []string{detail}
	}
	return false, nil
}

func splitByOperator(rule, op string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(rule); i++ {
		c := rule[i]
		if (c == '"' || c == '\'') && (i == 0 || rule[i-1] != '\\') {
			if !inQuote {
				inQuote = true
				quoteChar = c
			} else if c == quoteChar {
				inQuote = false
			}
		}
		if !inQuote && i+len(op) <= len(rule) && rule[i:i+len(op)] == op {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			i += len(op) - 1
			continue
		}
		current.WriteByte(c)
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

// matchSingleConditionWithDetails 匹配单个条件并返回详情
func matchSingleConditionWithDetails(condition string, data *FingerprintData) (bool, string) {
	condition = strings.TrimSpace(condition)

	var condType, value string
	var negate bool

	if idx := strings.Index(condition, "!=\""); idx > 0 {
		condType = strings.TrimSpace(condition[:idx])
		// idx+2 跳过 !=" 中的 !=，保留开头的引号给 extractQuotedValue 处理
		value = extractQuotedValue(condition[idx+2:])
		negate = true
	} else if idx := strings.Index(condition, "=\""); idx > 0 {
		condType = strings.TrimSpace(condition[:idx])
		// idx+1 跳过 ="中的 =，保留开头的引号给 extractQuotedValue 处理
		value = extractQuotedValue(condition[idx+1:])
		negate = false
	} else if idx := strings.Index(condition, "="); idx > 0 {
		condType = strings.TrimSpace(condition[:idx])
		value = strings.Trim(strings.TrimSpace(condition[idx+1:]), "\"'")
		negate = false
	} else {
		return false, ""
	}

	// 空值条件（如 body="" 或 body=）无意义，且 contains(x, "") 恒为 true，
	// 会让规则无条件匹配任何响应，直接判不匹配
	if value == "" {
		return false, ""
	}

	var result bool
	var matchedValue string
	condTypeLower := strings.ToLower(condType)

	// 调试日志：记录匹配条件详情
	logx.Infof("matchSingleCondition: condition=%s, condType=%s, value=%s, negate=%v", condition, condType, value, negate)
	logx.Infof("matchSingleCondition: data.Body len=%d, data.Title=%s, data.HeaderString len=%d, data.Server=%s, data.FaviconHash=%s",
		len(data.Body), data.Title, len(data.HeaderString), data.Server, data.FaviconHash)

	switch condTypeLower {
	case "body":
		result = matchBodyWithEncoding(data, value)
		logx.Infof("matchSingleCondition body: result=%v, value=%s", result, value)
		if result {
			matchedValue = findMatchContext(data.Body, value, 20)
		}
	case "title":
		result = containsIgnoreCase(data.Title, value)
		logx.Infof("matchSingleCondition title: result=%v, value=%s, data.Title=%s", result, value, data.Title)
		if result {
			matchedValue = data.Title
		}
	case "header":
		result = containsIgnoreCase(data.HeaderString, value)
		logx.Infof("matchSingleCondition header: result=%v, value=%s", result, value)
		if result {
			matchedValue = findMatchContext(data.HeaderString, value, 100)
		}
	case "server":
		result = containsIgnoreCase(data.Server, value)
		logx.Infof("matchSingleCondition server: result=%v, value=%s, data.Server=%s", result, value, data.Server)
		if result {
			matchedValue = data.Server
		}
	case "url":
		result = containsIgnoreCase(data.URL, value)
		if result {
			matchedValue = data.URL
		}
	case "cookie":
		// 同时检查Cookies字段和header字符串中的cookie（与扫描器一致）
		cookieStr := data.Cookies
		if cookieStr == "" && data.Headers != nil {
			cookieStr = strings.Join(data.Headers["Set-Cookie"], " ")
		}
		result = containsIgnoreCase(cookieStr, value)
		if result {
			matchedValue = findMatchContext(cookieStr, value, 100)
		}
	case "icon_hash", "favicon_hash":
		result = data.FaviconHash == value
		logx.Infof("matchSingleCondition icon_hash: result=%v, expected=%s, actual=%s", result, value, data.FaviconHash)
		if result {
			matchedValue = data.FaviconHash
		}
	default:
		logx.Infof("matchSingleCondition: unknown condition type %s", condTypeLower)
		return false, ""
	}

	if negate {
		result = !result
	}

	// 构建详情字符串
	var detail string
	if result {
		if negate {
			detail = fmt.Sprintf("%s != \"%s\"", condType, value)
		} else {
			// 截断匹配上下文到40字符，避免过长的metrics等数据污染展示
			detail = fmt.Sprintf("%s = \"%s\" → 匹配到: %s", condType, value, truncateString(matchedValue, 40))
		}
	}

	return result, detail
}

// findMatchContext 在文本中找到匹配的关键字并返回上下文
func findMatchContext(text, keyword string, contextLen int) string {
	textLower := strings.ToLower(text)
	keywordLower := strings.ToLower(keyword)

	idx := strings.Index(textLower, keywordLower)
	if idx < 0 {
		return ""
	}

	start := idx - contextLen
	if start < 0 {
		start = 0
	}
	end := idx + len(keyword) + contextLen
	if end > len(text) {
		end = len(text)
	}

	result := text[start:end]
	// 清理换行符
	result = strings.ReplaceAll(result, "\n", " ")
	result = strings.ReplaceAll(result, "\r", "")

	prefix := ""
	suffix := ""
	if start > 0 {
		prefix = "..."
	}
	if end < len(text) {
		suffix = "..."
	}

	return prefix + result + suffix
}

// isEscapedQuote 检查指定位置的引号是否被转义
func isEscapedQuote(s string, pos int) bool {
	backslashCount := 0
	for i := pos - 1; i >= 0 && s[i] == '\\'; i-- {
		backslashCount++
	}
	return backslashCount%2 == 1
}

func extractQuotedValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return s
	}
	if s[0] == '"' || s[0] == '\'' {
		quoteChar := s[0]
		// 从末尾向前查找闭合引号（与扫描器一致）
		end := -1
		for i := len(s) - 1; i > 0; i-- {
			if s[i] == quoteChar && !isEscapedQuote(s, i) {
				end = i
				break
			}
		}
		if end == -1 {
			end = len(s)
		}
		return unescapeQuotes(s[1:end], quoteChar)
	}
	if s[len(s)-1] == '"' || s[len(s)-1] == '\'' {
		return s[:len(s)-1]
	}
	return s
}

// unescapeQuotes 将转义的引号还原
// 例如：id=\"swagger-ui 还原为 id="swagger-ui
func unescapeQuotes(s string, quoteChar byte) string {
	switch quoteChar {
	case '"':
		return strings.ReplaceAll(s, `\"`, `"`)
	case '\'':
		return strings.ReplaceAll(s, `\'`, `'`)
	}
	return s
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// matchBodyWithEncoding 同时支持UTF-8和GBK编码匹配（与扫描器一致）
func matchBodyWithEncoding(data *FingerprintData, keyword string) bool {
	// 空关键字 contains 恒为 true，直接判不匹配（防止ARL html字段中的空串规则恒命中）
	if keyword == "" {
		return false
	}
	// 1. 先尝试UTF-8匹配
	if containsIgnoreCase(data.Body, keyword) {
		return true
	}
	// 2. 尝试将keyword转换为GBK编码后在原始字节中匹配
	if len(data.BodyBytes) > 0 {
		gbkKeyword, err := encodeToGBK(keyword)
		if err == nil && len(gbkKeyword) > 0 {
			if bytes.Contains(data.BodyBytes, gbkKeyword) {
				return true
			}
		}
	}
	return false
}

// encodeToGBK 将UTF-8字符串转换为GBK编码
func encodeToGBK(s string) ([]byte, error) {
	reader := transform.NewReader(strings.NewReader(s), simplifiedchinese.GBK.NewEncoder())
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(reader)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// matchARLWebappRulesWithDetails 匹配ARL webapp.json格式规则并返回匹配详情
// ARL格式：HTML/Headers等字段是直接包含匹配（OR关系，匹配一个即可）
// 与Wappalyzer格式的区别：Wappalyzer是正则+AND关系，ARL是直接包含+OR关系
func matchARLWebappRulesWithDetails(fp *model.Fingerprint, data *FingerprintData) (bool, []string) {
	var matchedConditions []string

	// HTML/Body匹配 - 支持UTF-8和GBK双编码匹配（OR关系，匹配一个即可）
	if len(fp.HTML) > 0 {
		for _, keyword := range fp.HTML {
			if matchBodyWithEncoding(data, keyword) {
				matchedConditions = append(matchedConditions, fmt.Sprintf("html 包含 \"%s\" → 匹配到", truncateString(keyword, 50)))
				return true, matchedConditions
			}
		}
	}

	// Headers匹配 - 直接在header字符串中搜索pattern（OR关系）
	if len(fp.Headers) > 0 {
		for key, pattern := range fp.Headers {
			// 大小写不敏感遍历响应头
			for hKey, hVal := range data.Headers {
				if strings.EqualFold(hKey, key) {
					headerValue := strings.Join(hVal, " ")
					if pattern == "" {
						// 只检查key是否存在
						matchedConditions = append(matchedConditions, fmt.Sprintf("header[%s] 存在 → 匹配到", key))
						return true, matchedConditions
					}
					// 检查header值是否包含pattern
					if containsIgnoreCase(headerValue, pattern) {
						matchedConditions = append(matchedConditions, fmt.Sprintf("header[%s] 包含 \"%s\" → 匹配到: %s", key, truncateString(pattern, 50), truncateString(headerValue, 80)))
						return true, matchedConditions
					}
				}
			}
		}
	}

	// Cookies匹配
	if len(fp.Cookies) > 0 {
		cookieStr := data.Cookies
		if cookieStr == "" && data.Headers != nil {
			cookieStr = strings.Join(data.Headers["Set-Cookie"], " ")
		}
		for key, pattern := range fp.Cookies {
			if containsIgnoreCase(cookieStr, key) {
				if pattern == "" || containsIgnoreCase(cookieStr, pattern) {
					matchedConditions = append(matchedConditions, fmt.Sprintf("cookie[%s] 包含 \"%s\" → 匹配到", key, pattern))
					return true, matchedConditions
				}
			}
		}
	}

	// Meta匹配
	if len(fp.Meta) > 0 {
		for key, pattern := range fp.Meta {
			metaPatterns := []string{
				fmt.Sprintf(`(?i)<meta[^>]*name=["']?%s["']?[^>]*content=["']([^"']*)["']`, regexp.QuoteMeta(key)),
				fmt.Sprintf(`(?i)<meta[^>]*content=["']([^"']*)["'][^>]*name=["']?%s["']?`, regexp.QuoteMeta(key)),
			}
			for _, mp := range metaPatterns {
				re := regexp.MustCompile(mp)
				if matches := re.FindStringSubmatch(data.Body); len(matches) > 1 {
					if pattern == "" || containsIgnoreCase(matches[1], pattern) {
						matchedConditions = append(matchedConditions, fmt.Sprintf("meta[%s] 包含 \"%s\" → 匹配到: %s", key, pattern, truncateString(matches[1], 80)))
						return true, matchedConditions
					}
				}
			}
		}
	}

	return false, nil
}

// matchWappalyzerRulesWithDetails 匹配Wappalyzer格式规则并返回匹配详情
// Wappalyzer的html、scripts、css等字段是正则表达式
func matchWappalyzerRulesWithDetails(fp *model.Fingerprint, data *FingerprintData) (bool, []string) {
	hasRule := false
	allMatch := true
	var matchedConditions []string

	// Headers匹配 - key需要大小写不敏感匹配
	if len(fp.Headers) > 0 {
		hasRule = true
		headerMatch := false
		for key, pattern := range fp.Headers {
			logx.Infof("matchWappalyzer: checking header[%s] pattern=%q against resp headers", key, pattern)
			// 遍历响应头，大小写不敏感匹配key
			for hKey, hVal := range data.Headers {
				if strings.EqualFold(hKey, key) {
					headerValue := strings.Join(hVal, " ")
					if pattern == "" {
						// 只要header存在就匹配
						headerMatch = true
						matchedConditions = append(matchedConditions, fmt.Sprintf("header[%s] 存在 → 匹配到: %s", key, truncateString(headerValue, 80)))
						logx.Infof("matchWappalyzer: header[%s] MATCHED (existence)", key)
						break
					}
					// pattern是正则表达式
					if matchRegexOrContains(headerValue, pattern) {
						headerMatch = true
						matchedConditions = append(matchedConditions, fmt.Sprintf("header[%s] =~ \"%s\" → 匹配到: %s", key, truncateString(pattern, 50), truncateString(headerValue, 80)))
						logx.Infof("matchWappalyzer: header[%s] MATCHED value=%q", key, truncateString(headerValue, 80))
						break
					} else {
						logx.Infof("matchWappalyzer: header[%s] value=%q did NOT match pattern=%q", key, truncateString(headerValue, 80), pattern)
					}
				}
			}
			if headerMatch {
				break
			}
		}
		if !headerMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: header check FAILED, allMatch=false")
		}
	}

	// HTML匹配 - pattern是正则表达式
	if len(fp.HTML) > 0 && allMatch {
		hasRule = true
		htmlMatch := false
		for _, pattern := range fp.HTML {
			logx.Infof("matchWappalyzer: checking html pattern=%q against body (len=%d)", truncateString(pattern, 60), len(data.Body))
			if matchRegexOrContains(data.Body, pattern) {
				htmlMatch = true
				matchedConditions = append(matchedConditions, fmt.Sprintf("html =~ \"%s\" → 匹配到", truncateString(pattern, 50)))
				logx.Infof("matchWappalyzer: html pattern MATCHED")
				break
			}
		}
		if !htmlMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: html check FAILED, allMatch=false")
		}
	}

	// Scripts匹配 - pattern是正则表达式，匹配script标签的src属性
	if len(fp.Scripts) > 0 && allMatch {
		hasRule = true
		scriptMatch := false
		// 提取所有script src
		scriptSrcRe := regexp.MustCompile(`(?i)<script[^>]*src=["']([^"']+)["']`)
		scriptSrcs := scriptSrcRe.FindAllStringSubmatch(data.Body, -1)
		logx.Infof("matchWappalyzer: checking scripts, patterns=%d, found %d script srcs", len(fp.Scripts), len(scriptSrcs))
		for _, pattern := range fp.Scripts {
			for _, src := range scriptSrcs {
				if len(src) > 1 && matchRegexOrContains(src[1], pattern) {
					scriptMatch = true
					matchedConditions = append(matchedConditions, fmt.Sprintf("scripts =~ \"%s\" → 匹配到: %s", truncateString(pattern, 50), truncateString(src[1], 80)))
					logx.Infof("matchWappalyzer: script pattern=%q MATCHED src=%q", pattern, truncateString(src[1], 80))
					break
				}
			}
			if scriptMatch {
				break
			}
		}
		if !scriptMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: scripts check FAILED, allMatch=false")
		}
	}

	// ScriptSrc匹配
	if len(fp.ScriptSrc) > 0 && allMatch {
		hasRule = true
		scriptSrcMatch := false
		scriptSrcRe := regexp.MustCompile(`(?i)<script[^>]*src=["']([^"']+)["']`)
		scriptSrcs := scriptSrcRe.FindAllStringSubmatch(data.Body, -1)
		logx.Infof("matchWappalyzer: checking scriptSrc, patterns=%d, found %d script srcs", len(fp.ScriptSrc), len(scriptSrcs))
		for _, pattern := range fp.ScriptSrc {
			for _, src := range scriptSrcs {
				if len(src) > 1 && matchRegexOrContains(src[1], pattern) {
					scriptSrcMatch = true
					matchedConditions = append(matchedConditions, fmt.Sprintf("scriptSrc =~ \"%s\" → 匹配到: %s", truncateString(pattern, 50), truncateString(src[1], 80)))
					logx.Infof("matchWappalyzer: scriptSrc pattern=%q MATCHED src=%q", pattern, truncateString(src[1], 80))
					break
				}
			}
			if scriptSrcMatch {
				break
			}
		}
		if !scriptSrcMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: scriptSrc check FAILED, allMatch=false")
		}
	}

	// Cookies匹配
	if len(fp.Cookies) > 0 && allMatch {
		hasRule = true
		cookieMatch := false
		// 同时检查Cookies字段和header中的Set-Cookie（与扫描器一致）
		cookieStr := data.Cookies
		if cookieStr == "" && data.Headers != nil {
			cookieStr = strings.Join(data.Headers["Set-Cookie"], " ")
		}
		logx.Infof("matchWappalyzer: checking cookies, keys=%v, cookieStr=%q", fp.Cookies, truncateString(cookieStr, 100))
		for key, pattern := range fp.Cookies {
			if containsIgnoreCase(cookieStr, key) {
				if pattern == "" || matchRegexOrContains(cookieStr, pattern) {
					cookieMatch = true
					matchedConditions = append(matchedConditions, fmt.Sprintf("cookie[%s] =~ \"%s\" → 匹配到", key, pattern))
					logx.Infof("matchWappalyzer: cookie[%s] MATCHED", key)
					break
				}
			}
		}
		if !cookieMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: cookies check FAILED, allMatch=false")
		}
	}

	// Meta匹配
	if len(fp.Meta) > 0 && allMatch {
		hasRule = true
		metaMatch := false
		logx.Infof("matchWappalyzer: checking meta, keys=%v", fp.Meta)
		for key, pattern := range fp.Meta {
			metaPatterns := []string{
				fmt.Sprintf(`(?i)<meta[^>]*name=["']?%s["']?[^>]*content=["']([^"']*)["']`, regexp.QuoteMeta(key)),
				fmt.Sprintf(`(?i)<meta[^>]*content=["']([^"']*)["'][^>]*name=["']?%s["']?`, regexp.QuoteMeta(key)),
			}
			for _, mp := range metaPatterns {
				re := regexp.MustCompile(mp)
				if matches := re.FindStringSubmatch(data.Body); len(matches) > 1 {
					if pattern == "" || matchRegexOrContains(matches[1], pattern) {
						metaMatch = true
						matchedConditions = append(matchedConditions, fmt.Sprintf("meta[%s] =~ \"%s\" → 匹配到: %s", key, pattern, truncateString(matches[1], 80)))
						logx.Infof("matchWappalyzer: meta[%s] MATCHED content=%q", key, truncateString(matches[1], 80))
						break
					}
				}
			}
			if metaMatch {
				break
			}
		}
		if !metaMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: meta check FAILED, allMatch=false")
		}
	}

	// CSS匹配
	if len(fp.CSS) > 0 && allMatch {
		hasRule = true
		cssMatch := false
		logx.Infof("matchWappalyzer: checking css, patterns=%v", fp.CSS)
		for _, pattern := range fp.CSS {
			if matchRegexOrContains(data.Body, pattern) {
				cssMatch = true
				matchedConditions = append(matchedConditions, fmt.Sprintf("css =~ \"%s\" → 匹配到", truncateString(pattern, 50)))
				logx.Infof("matchWappalyzer: css pattern=%q MATCHED", pattern)
				break
			}
		}
		if !cssMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: css check FAILED, allMatch=false")
		}
	}

	// URL匹配
	if len(fp.URL) > 0 && allMatch {
		hasRule = true
		urlMatch := false
		logx.Infof("matchWappalyzer: checking url, patterns=%v, data.URL=%s", fp.URL, data.URL)
		for _, pattern := range fp.URL {
			if matchRegexOrContains(data.URL, pattern) {
				urlMatch = true
				matchedConditions = append(matchedConditions, fmt.Sprintf("url =~ \"%s\" → 匹配到: %s", truncateString(pattern, 50), data.URL))
				logx.Infof("matchWappalyzer: url pattern=%q MATCHED", pattern)
				break
			}
		}
		if !urlMatch {
			allMatch = false
			logx.Infof("matchWappalyzer: url check FAILED, allMatch=false")
		}
	}

	logx.Infof("matchWappalyzer: result hasRule=%v, allMatch=%v", hasRule, allMatch)

	if hasRule && allMatch {
		return true, matchedConditions
	}
	return false, nil
}

// matchRegexOrContains 尝试正则匹配，如果正则无效则回退到字符串包含匹配
func matchRegexOrContains(text, pattern string) bool {
	// 空pattern恒匹配任何文本（Wappalyzer字段中的空串规则会恒命中），判不匹配；
	// header的"仅存在性检查"语义在调用方单独处理，不经过此处
	if pattern == "" {
		return false
	}
	// 尝试编译为正则表达式
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		// 正则无效，回退到字符串包含匹配
		return containsIgnoreCase(text, pattern)
	}
	return re.MatchString(text)
}

// FingerprintBatchValidateLogic 批量验证指纹
type FingerprintBatchValidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintBatchValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintBatchValidateLogic {
	return &FingerprintBatchValidateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// FingerprintBatchValidate 批量验证指纹（异步提交，立即返回taskId，前端轮询进度）
func (l *FingerprintBatchValidateLogic) FingerprintBatchValidate(req *types.FingerprintBatchValidateReq) (*types.FingerprintBatchValidateResp, error) {
	if req.Url == "" {
		return &types.FingerprintBatchValidateResp{Code: 400, Msg: "URL不能为空"}, nil
	}

	scope := req.Scope
	if scope == "" {
		scope = "all"
	}

	l.Logger.Infof("FingerprintBatchValidate(async): url=%s, scope=%s", req.Url, scope)

	// 涉及主动指纹时需要检查Worker在线
	needWorker := scope == "active" || scope == "all"
	if needWorker {
		workers, err := l.svcCtx.RedisClient.SMembers(l.ctx, "cscan:workers").Result()
		if err != nil || len(workers) == 0 {
			return &types.FingerprintBatchValidateResp{Code: 500, Msg: "当前没有在线的扫描节点(Worker)"}, nil
		}
		hasActive := false
		for _, w := range workers {
			exists, _ := l.svcCtx.RedisClient.Exists(l.ctx, "cscan:worker:"+w).Result()
			if exists > 0 {
				hasActive = true
				break
			}
		}
		if !hasActive {
			return &types.FingerprintBatchValidateResp{Code: 500, Msg: "当前没有在线的扫描节点(Worker)"}, nil
		}
	}

	// 生成API端任务ID（用于前端轮询）
	apiTaskId := uuid.New().String()

	// 统一异步执行
	go l.startFingerprintBatchAsync(apiTaskId, req.Url, scope)

	return &types.FingerprintBatchValidateResp{
		Code:   0,
		Msg:    "批量验证任务已提交",
		TaskId: apiTaskId,
	}, nil
}

// startFingerprintBatchAsync 统一批量验证：被动指纹API本地执行（1次HTTP），主动指纹并发下发Worker
func (l *FingerprintBatchValidateLogic) startFingerprintBatchAsync(apiTaskId, url, scope string) {
	bgCtx := context.Background()

	state := &fingerprintBatchTaskState{
		TaskId:     apiTaskId,
		Url:        url,
		Scope:      scope,
		Status:     "running",
		StopCh:     make(chan struct{}),
		CreateTime: time.Now(),
	}
	fingerprintBatchTasks.Store(apiTaskId, state)

	// 计算总数并开始执行
	var totalCount int64
	var passiveFps []model.Fingerprint
	var activeFps []model.ActiveFingerprint

	// 收集被动指纹（内置+自定义）
	if scope == "all" || scope == "builtin" || scope == "custom" {
		allFps, err := l.svcCtx.FingerprintModel.FindPassiveEnabled(bgCtx)
		if err != nil {
			l.Logger.Errorf("startFingerprintBatchAsync: get passive fps failed: %v", err)
		} else {
			for _, fp := range allFps {
				if scope == "builtin" && !fp.IsBuiltin {
					continue
				}
				if scope == "custom" && fp.IsBuiltin {
					continue
				}
				passiveFps = append(passiveFps, fp)
			}
			totalCount += int64(len(passiveFps))
		}
	}

	// 收集主动指纹
	if scope == "all" || scope == "active" {
		afps, err := l.svcCtx.ActiveFingerprintModel.FindEnabled(bgCtx)
		if err != nil {
			l.Logger.Errorf("startFingerprintBatchAsync: get active fps failed: %v", err)
		} else {
			activeFps = afps
			totalCount += int64(len(activeFps))
		}
	}

	state.mu.Lock()
	state.Total = totalCount
	state.mu.Unlock()

	if totalCount == 0 {
		state.mu.Lock()
		state.Status = "completed"
		state.mu.Unlock()
		return
	}

	// Phase 1: 被动指纹验证（API本地执行，只发1次HTTP请求）
	if len(passiveFps) > 0 {
		l.runPassiveFingerprintBatch(bgCtx, state, url, passiveFps)
	}

	// Phase 2: 主动指纹验证（并发下发Worker）
	if len(activeFps) > 0 {
		l.runActiveFingerprintBatch(bgCtx, state, url, activeFps)
	}

	// 最终状态
	state.mu.Lock()
	var matchedResults []types.MatchedFingerprintInfo
	if state.Status != "failed" {
		select {
		case <-state.StopCh:
			state.Status = "stopped"
		default:
			state.Status = "completed"
		}
		matchedResults = make([]types.MatchedFingerprintInfo, len(state.Results))
		copy(matchedResults, state.Results)
	}
	finalUrl := state.Url
	state.mu.Unlock()

	// 同步验证结果到资产库（暴露面管理）
	if len(matchedResults) > 0 {
		go l.syncValidateResultsToAssets(bgCtx, finalUrl, matchedResults)
	}
}

// runPassiveFingerprintBatch 被动指纹批量验证：1次HTTP请求 + 本地匹配
func (l *FingerprintBatchValidateLogic) runPassiveFingerprintBatch(ctx context.Context, state *fingerprintBatchTaskState, url string, fps []model.Fingerprint) {
	l.Logger.Infof("runPassiveFingerprintBatch: url=%s, fps=%d", url, len(fps))

	// 只发1次HTTP请求获取目标数据
	data, err := fetchFingerprintData(url)
	if err != nil {
		// 目标获取失败必须显式失败，不能伪装成"完成、0匹配"
		l.Logger.Errorf("runPassiveFingerprintBatch: fetch data failed: %v", err)
		errMsg := fmt.Sprintf("请求目标失败: %v", err)
		state.mu.Lock()
		state.Completed += int64(len(fps))
		state.Status = "failed"
		state.ErrMsg = errMsg
		state.mu.Unlock()
		return
	}

	// 初始化wappalyzergo库（用于wappalyzer来源/内置指纹检测）
	wappalyzerApps := make(map[string]struct{})
	wappalyzerClient, wErr := wappalyzer.New()
	if wErr == nil {
		apps := wappalyzerClient.Fingerprint(data.Headers, data.BodyBytes)
		for app := range apps {
			wappalyzerApps[strings.ToLower(app)] = struct{}{}
		}
	}

	// 批量匹配（所有被动指纹共享同一份data，不重复发HTTP请求）
	engine := NewSingleFingerprintEngine(nil)
	for _, fp := range fps {
		select {
		case <-state.StopCh:
			state.mu.Lock()
			state.Completed += int64(len(fps))
			state.mu.Unlock()
			return
		default:
		}

		matched := false
		var conditions []string

		// wappalyzergo库检测（用于wappalyzer来源和内置指纹）
		if (fp.Source == "wappalyzer" || fp.IsBuiltin) && wErr == nil {
			if _, ok := wappalyzerApps[strings.ToLower(fp.Name)]; ok {
				matched = true
				conditions = append(conditions, fmt.Sprintf("wappalyzergo库检测匹配: %s", fp.Name))
			}
		}

		// 自定义规则引擎匹配（Rule字段或Wappalyzer字段规则）
		if !matched {
			var ruleConds []string
			matched, ruleConds = engine.MatchWithDetails(data, &fp)
			if matched {
				conditions = append(conditions, ruleConds...)
			}
		}

		if matched {
			state.mu.Lock()
			state.Matched++
			state.Results = append(state.Results, types.MatchedFingerprintInfo{
				Id:                fp.Id.Hex(),
				Name:              fp.Name,
				IsBuiltin:         fp.IsBuiltin,
				IsActive:          false,
				MatchedConditions: strings.Join(conditions, "\n"),
			})
			state.mu.Unlock()
		}

		state.mu.Lock()
		state.Completed++
		state.mu.Unlock()
	}
}

// runActiveFingerprintBatch 主动指纹批量验证：并发下发Worker，每个主动指纹单独探测路径
func (l *FingerprintBatchValidateLogic) runActiveFingerprintBatch(ctx context.Context, state *fingerprintBatchTaskState, url string, afps []model.ActiveFingerprint) {
	l.Logger.Infof("runActiveFingerprintBatch: url=%s, afps=%d", url, len(afps))

	sem := make(chan struct{}, 3) // 并发3
	var wg sync.WaitGroup
	stopped := int32(0)

	for _, afp := range afps {
		select {
		case <-state.StopCh:
			atomic.StoreInt32(&stopped, 1)
		default:
		}
		if atomic.LoadInt32(&stopped) == 1 {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(afpId, afpName string) {
			defer wg.Done()
			defer func() { <-sem }()
			// 确保Completed总是递增，即使panic也不遗漏
			defer func() {
				if r := recover(); r != nil {
					l.Logger.Errorf("runActiveFingerprintBatch: panic for afp %s: %v", afpName, r)
				}
				state.mu.Lock()
				state.Completed++
				state.mu.Unlock()
			}()

			// 直接入队主动指纹验证任务
			fpTaskId := uuid.New().String()
			fpTaskConfig := map[string]interface{}{
				"taskType":   "active_fingerprint_validate",
				"url":        url,
				"activeFpId": afpId,
				"timeout":    60,
			}
			fpConfigBytes, _ := json.Marshal(fpTaskConfig)

			fpTask := &scheduler.TaskInfo{
				TaskId:     fpTaskId,
				MainTaskId: fpTaskId,
				TaskName:   "主动指纹验证",
				Config:     string(fpConfigBytes),
				Priority:   2,
			}

			if err := l.svcCtx.Scheduler.PushTask(ctx, fpTask); err != nil {
				l.Logger.Errorf("runActiveFingerprintBatch: push task failed for %s: %v", afpName, err)
				return
			}
			persistTaskInfo(ctx, l.svcCtx, fpTaskId, fpTaskConfig)

			result, perr := pollFingerprintValidateResult(ctx, l.svcCtx, fpTaskId, 90*time.Second)
			if perr != nil {
				l.Logger.Errorf("runActiveFingerprintBatch: poll timeout for %s: %v", afpName, perr)
				return
			}
			if result.Matched && result.Error == "" {
				state.mu.Lock()
				state.Matched++
				state.Results = append(state.Results, types.MatchedFingerprintInfo{
					Id:                afpId,
					Name:              afpName,
					IsActive:          true,
					MatchedConditions: result.Details,
				})
				state.mu.Unlock()
			}
		}(afp.Id.Hex(), afp.Name)
	}

	wg.Wait()
}

// ==================== 指纹批量验证进度查询与停止 ====================

// FingerprintBatchProgressLogic 查询指纹批量验证进度
type FingerprintBatchProgressLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintBatchProgressLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintBatchProgressLogic {
	return &FingerprintBatchProgressLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FingerprintBatchProgressLogic) FingerprintBatchProgress(req *types.FingerprintBatchProgressReq) (*types.FingerprintBatchProgressResp, error) {
	if req.TaskId == "" {
		return &types.FingerprintBatchProgressResp{Code: 400, Msg: "任务ID不能为空"}, nil
	}

	if v, ok := fingerprintBatchTasks.Load(req.TaskId); ok {
		state := v.(*fingerprintBatchTaskState)
		state.mu.Lock()
		defer state.mu.Unlock()
		msg := ""
		if state.Status == "failed" {
			msg = state.ErrMsg
		}
		return &types.FingerprintBatchProgressResp{
			Code:      0,
			Msg:       msg,
			TaskId:    state.TaskId,
			Status:    state.Status,
			Total:     state.Total,
			Completed: state.Completed,
			Matched:   state.Matched,
			Url:       state.Url,
		}, nil
	}

	return &types.FingerprintBatchProgressResp{Code: 404, Msg: "任务不存在或已过期"}, nil
}

// FingerprintBatchResultLogic 获取指纹批量验证结果详情
type FingerprintBatchResultLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintBatchResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintBatchResultLogic {
	return &FingerprintBatchResultLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FingerprintBatchResultLogic) FingerprintBatchResult(req *types.FingerprintBatchResultReq) (*types.FingerprintBatchResultResp, error) {
	if req.TaskId == "" {
		return &types.FingerprintBatchResultResp{Code: 400, Msg: "任务ID不能为空"}, nil
	}

	if v, ok := fingerprintBatchTasks.Load(req.TaskId); ok {
		state := v.(*fingerprintBatchTaskState)
		state.mu.Lock()
		defer state.mu.Unlock()
		msg := ""
		if state.Status == "failed" {
			msg = state.ErrMsg
		}
		return &types.FingerprintBatchResultResp{
			Code:    0,
			Msg:     msg,
			TaskId:  state.TaskId,
			Status:  state.Status,
			Total:   state.Total,
			Matched: state.Matched,
			Url:     state.Url,
			Results: state.Results,
		}, nil
	}

	return &types.FingerprintBatchResultResp{Code: 404, Msg: "任务不存在或已过期"}, nil
}

// FingerprintBatchStopLogic 停止指纹批量验证
type FingerprintBatchStopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintBatchStopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintBatchStopLogic {
	return &FingerprintBatchStopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FingerprintBatchStopLogic) FingerprintBatchStop(req *types.FingerprintBatchStopReq) error {
	if v, ok := fingerprintBatchTasks.Load(req.TaskId); ok {
		state := v.(*fingerprintBatchTaskState)
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.Status == "running" {
			state.Status = "stopping"
			close(state.StopCh)
		}
	}
	return nil
}

// ==================== 指纹匹配现有资产 ====================

// FingerprintMatchAssetsLogic 验证指纹匹配现有资产
type FingerprintMatchAssetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintMatchAssetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintMatchAssetsLogic {
	return &FingerprintMatchAssetsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// FingerprintMatchAssets 验证指纹匹配现有资产
func (l *FingerprintMatchAssetsLogic) FingerprintMatchAssets(req *types.FingerprintMatchAssetsReq) (*types.FingerprintMatchAssetsResp, error) {
	if req.FingerprintId == "" {
		return &types.FingerprintMatchAssetsResp{Code: 400, Msg: "指纹ID不能为空"}, nil
	}

	startTime := time.Now()

	l.Logger.Infof("FingerprintMatchAssets: START, fingerprintId=%s", req.FingerprintId)

	// 获取指纹
	fp, err := l.svcCtx.FingerprintModel.FindById(l.ctx, req.FingerprintId)
	if err != nil {
		l.Logger.Errorf("FingerprintMatchAssets: find fingerprint failed, id=%s, err=%v", req.FingerprintId, err)
		return &types.FingerprintMatchAssetsResp{Code: 500, Msg: "查询指纹失败"}, nil
	}
	if fp == nil {
		l.Logger.Errorf("FingerprintMatchAssets: fingerprint not found, id=%s", req.FingerprintId)
		return &types.FingerprintMatchAssetsResp{Code: 404, Msg: "指纹不存在"}, nil
	}
	l.Logger.Infof("FingerprintMatchAssets: fingerprint found, name=%s, rule=%s", fp.Name, fp.Rule)

	// 查询有 body 或 header 或 title 的资产（字段带 omitempty，需同时检查 $exists）
	filter := bson.M{
		"$or": bson.A{
			bson.M{"body": bson.M{"$exists": true, "$ne": ""}},
			bson.M{"header": bson.M{"$exists": true, "$ne": ""}},
			bson.M{"title": bson.M{"$exists": true, "$ne": ""}},
			bson.M{"icon_hash": bson.M{"$exists": true, "$ne": ""}},
		},
	}

	assetModel := l.svcCtx.GetAssetModel()
	assets, err := assetModel.FindForFingerprint(l.ctx, filter, 0, 0)
	if err != nil {
		l.Logger.Errorf("FingerprintMatchAssets: query assets failed, err=%v", err)
		return &types.FingerprintMatchAssetsResp{Code: 500, Msg: "查询资产失败"}, nil
	}
	l.Logger.Infof("FingerprintMatchAssets: found %d assets, rule=%s", len(assets), fp.Rule)

	// 安全告警：当待匹配资产数量过大时提示 OOM 风险
	if len(assets) > 100000 {
		l.Logger.Errorf("FingerprintMatchAssets: WARNING 资产数量 %d 超过 10 万，存在 OOM 风险，建议分批匹配", len(assets))
	}

	l.Logger.Infof("FingerprintMatchAssets: fingerprintId=%s, name=%s, totalAssets=%d, updateAsset=%v, rule=%s", req.FingerprintId, fp.Name, len(assets), req.UpdateAsset, fp.Rule)

	// 创建指纹引擎
	engine := NewSingleFingerprintEngine(fp)

	// 匹配资产（匹配所有资产，不再限制数量）
	var matchedList []types.FingerprintMatchedAsset
	var updatedCount int
	for _, asset := range assets {
		// 计算 MMH3 hash（如果存储了原始图标数据）
		mmh3Hash := asset.IconHash
		if len(asset.IconHashBytes) > 0 {
			// 使用与扫描器相同的算法计算 MMH3 hash
			mmh3Hash = calculateMMH3HashSimple(asset.IconHashBytes)
		}

		// 构建指纹数据
		data := &FingerprintData{
			Title:        asset.Title,
			Body:         asset.HttpBody,
			HeaderString: asset.HttpHeader,
			Server:       asset.Server,
			FaviconHash:  mmh3Hash,
			URL:          asset.Authority,
		}

		// 调试日志：显示前几个资产的匹配数据
		if len(matchedList) < 3 {
			bodyPreview := asset.HttpBody
			if len(bodyPreview) > 100 {
				bodyPreview = bodyPreview[:100] + "..."
			}
			l.Logger.Infof("FingerprintMatchAssets: checking asset authority=%s, title=%s, body_len=%d, iconHash=%s, body_preview=%s",
				asset.Authority, asset.Title, len(asset.HttpBody), asset.IconHash, bodyPreview)
		}

		// 解析 Header 字符串为 map
		if asset.HttpHeader != "" {
			data.Headers = parseHeaderString(asset.HttpHeader)
		}

		// 执行匹配
		matched, _ := engine.MatchWithDetails(data)
		if matched {
			l.Logger.Infof("FingerprintMatchAssets: MATCHED asset=%s, title=%s", asset.Authority, asset.Title)
			matchedList = append(matchedList, types.FingerprintMatchedAsset{
				Id:        asset.Id.Hex(),
				Authority: asset.Authority,
				Host:      asset.Host,
				Port:      asset.Port,
				Title:     asset.Title,
				Service:   asset.Service,
			})

			// 如果需要更新资产，将指纹添加到资产的app字段
			if req.UpdateAsset {
				// 检查指纹是否已存在（按归一化键比较，"Nginx" 能命中 "Nginx[httpx]" 这类带来源后缀的变体）
				fpKey := model.NormalizeAppKey(fp.Name)
				fpExists := false
				for _, app := range asset.App {
					if model.NormalizeAppKey(app) == fpKey {
						fpExists = true
						break
					}
				}
				// 如果不存在，添加指纹
				if !fpExists {
					newApps := append([]string{}, asset.App...)
					newApps = append(newApps, fp.Name)

					assetModel := l.svcCtx.GetAssetModel()

					// 通过 helper 构造更新文档：app 走归一化合并去重后 $set，diff 触发 update_time 推进。
					// 指纹匹配是管理员触发的"主动发现"，标记资产为已更新并推进
					// last_status_change_time，但不变更 new / last_task_id（无跨任务语义）。
					updatedAsset := &model.Asset{
						Authority: asset.Authority,
						Host:      asset.Host,
						Port:      asset.Port,
						IsHTTP:    asset.IsHTTP,
						App:       newApps,
						TaskId:    asset.TaskId,
					}
					existingForHelper := &model.Asset{
						Id:         asset.Id,
						TaskId:     asset.TaskId,
						LastTaskId: asset.LastTaskId,
						App:        asset.App,
						Authority:  asset.Authority,
						Host:       asset.Host,
						Port:       asset.Port,
						IsHTTP:     asset.IsHTTP,
					}
					opts := model.AssetWriteOptions{
						TaskId:          asset.TaskId,
						IsDifferentTask: false,
					}
					update, _ := model.BuildAssetUpdateDoc(updatedAsset, existingForHelper, opts)
					// 显式补齐指纹匹配特有语义：update=true / last_status_change_time
					if setFields, ok := update["$set"].(bson.M); ok {
						setFields["update"] = true
						setFields["last_status_change_time"] = time.Now()
					}

					if err := assetModel.UpdateWithRaw(l.ctx, asset.Id.Hex(), update); err == nil {
						updatedCount++
					}
				}
			}
		}
	}

	duration := time.Since(startTime)
	l.Logger.Infof("FingerprintMatchAssets: matched=%d, updated=%d, scanned=%d, duration=%s", len(matchedList), updatedCount, len(assets), duration)

	msg := "匹配完成"
	if req.UpdateAsset && updatedCount > 0 {
		msg = fmt.Sprintf("匹配完成，已更新 %d 个资产的指纹信息", updatedCount)
	}

	return &types.FingerprintMatchAssetsResp{
		Code:         0,
		Msg:          msg,
		MatchedCount: len(matchedList),
		TotalScanned: len(assets),
		UpdatedCount: updatedCount,
		Duration:     fmt.Sprintf("%.2fs", duration.Seconds()),
		MatchedList:  matchedList,
	}, nil
}

// parseHeaderString 解析 Header 字符串为 map
func parseHeaderString(headerStr string) map[string][]string {
	headers := make(map[string][]string)
	lines := strings.Split(headerStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			headers[key] = append(headers[key], value)
		}
	}
	return headers
}

// ==================== HTTP服务映射管理 ====================

// HttpServiceMappingListLogic HTTP服务映射列表
type HttpServiceMappingListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceMappingListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceMappingListLogic {
	return &HttpServiceMappingListLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceMappingListLogic) HttpServiceMappingList(req *types.HttpServiceMappingListReq) (*types.HttpServiceMappingListResp, error) {
	docs, err := l.svcCtx.HttpServiceMappingModel.FindWithFilter(l.ctx, req.IsHttp, req.Keyword)
	if err != nil {
		return &types.HttpServiceMappingListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.HttpServiceMapping, 0, len(docs))
	for _, doc := range docs {
		list = append(list, types.HttpServiceMapping{
			Id:          doc.Id.Hex(),
			ServiceName: doc.ServiceName,
			IsHttp:      doc.IsHttp,
			Description: doc.Description,
			Enabled:     doc.Enabled,
			CreateTime:  doc.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.HttpServiceMappingListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

// HttpServiceMappingSaveLogic 保存HTTP服务映射
type HttpServiceMappingSaveLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceMappingSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceMappingSaveLogic {
	return &HttpServiceMappingSaveLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceMappingSaveLogic) HttpServiceMappingSave(req *types.HttpServiceMappingSaveReq) (*types.BaseResp, error) {
	doc := &model.HttpServiceMapping{
		ServiceName: strings.ToLower(strings.TrimSpace(req.ServiceName)),
		IsHttp:      req.IsHttp,
		Description: req.Description,
		Enabled:     req.Enabled,
	}

	if req.Id != "" {
		err := l.svcCtx.HttpServiceMappingModel.Update(l.ctx, req.Id, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新失败: " + err.Error()}, nil
		}
	} else {
		err := l.svcCtx.HttpServiceMappingModel.Insert(l.ctx, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "创建失败: " + err.Error()}, nil
		}
	}

	// 刷新缓存，确保新的映射立即生效
	l.svcCtx.HttpServiceMappingModel.RefreshCache(l.ctx)

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

// HttpServiceMappingDeleteLogic 删除HTTP服务映射
type HttpServiceMappingDeleteLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceMappingDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceMappingDeleteLogic {
	return &HttpServiceMappingDeleteLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceMappingDeleteLogic) HttpServiceMappingDelete(req *types.HttpServiceMappingDeleteReq) (*types.BaseResp, error) {
	err := l.svcCtx.HttpServiceMappingModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	// 刷新缓存，确保删除的映射立即失效
	l.svcCtx.HttpServiceMappingModel.RefreshCache(l.ctx)

	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// ==================== HTTP服务设置 ====================

// HttpServiceConfigGetLogic 获取HTTP服务配置
type HttpServiceConfigGetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceConfigGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceConfigGetLogic {
	return &HttpServiceConfigGetLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceConfigGetLogic) HttpServiceConfigGet() (*types.HttpServiceConfigGetResp, error) {
	config, err := l.svcCtx.HttpServiceModel.GetConfig(l.ctx)
	if err != nil {
		return &types.HttpServiceConfigGetResp{Code: 500, Msg: "获取配置失败: " + err.Error()}, nil
	}

	return &types.HttpServiceConfigGetResp{
		Code: 0,
		Msg:  "success",
		Data: types.HttpServiceConfig{
			HttpPorts:    config.HttpPorts,
			HttpsPorts:   config.HttpsPorts,
			NonHttpPorts: config.NonHttpPorts,
			Description:  config.Description,
		},
	}, nil
}

// HttpServiceConfigSaveLogic 保存HTTP服务配置
type HttpServiceConfigSaveLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceConfigSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceConfigSaveLogic {
	return &HttpServiceConfigSaveLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceConfigSaveLogic) HttpServiceConfigSave(req *types.HttpServiceConfigSaveReq) (*types.BaseResp, error) {
	config := &model.HttpServiceConfig{
		HttpPorts:    req.HttpPorts,
		HttpsPorts:   req.HttpsPorts,
		NonHttpPorts: req.NonHttpPorts,
		Description:  req.Description,
	}

	err := l.svcCtx.HttpServiceModel.SaveConfig(l.ctx, config)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "保存配置失败: " + err.Error()}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

// HttpServiceMappingListV2Logic 获取HTTP服务映射列表（使用新模型）
type HttpServiceMappingListV2Logic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceMappingListV2Logic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceMappingListV2Logic {
	return &HttpServiceMappingListV2Logic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceMappingListV2Logic) HttpServiceMappingListV2(req *types.HttpServiceMappingListReq) (*types.HttpServiceMappingListResp, error) {
	docs, err := l.svcCtx.HttpServiceModel.GetMappings(l.ctx)
	if err != nil {
		return &types.HttpServiceMappingListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.HttpServiceMapping, 0, len(docs))
	for _, doc := range docs {
		// 筛选
		if req.IsHttp != nil && doc.IsHttp != *req.IsHttp {
			continue
		}
		if req.Keyword != "" && !strings.Contains(strings.ToLower(doc.ServiceName), strings.ToLower(req.Keyword)) {
			continue
		}

		list = append(list, types.HttpServiceMapping{
			Id:          doc.Id.Hex(),
			ServiceName: doc.ServiceName,
			IsHttp:      doc.IsHttp,
			Description: doc.Description,
			Enabled:     doc.Enabled,
			CreateTime:  doc.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.HttpServiceMappingListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

// HttpServiceMappingSaveV2Logic 保存HTTP服务映射（使用新模型）
type HttpServiceMappingSaveV2Logic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceMappingSaveV2Logic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceMappingSaveV2Logic {
	return &HttpServiceMappingSaveV2Logic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceMappingSaveV2Logic) HttpServiceMappingSaveV2(req *types.HttpServiceMappingSaveReq) (*types.BaseResp, error) {
	doc := &model.HttpServiceMapping{
		ServiceName: strings.ToLower(strings.TrimSpace(req.ServiceName)),
		IsHttp:      req.IsHttp,
		Description: req.Description,
		Enabled:     req.Enabled,
	}

	if req.Id != "" {
		oid, err := primitive.ObjectIDFromHex(req.Id)
		if err != nil {
			return &types.BaseResp{Code: 400, Msg: "无效的ID"}, nil
		}
		doc.Id = oid
	}

	err := l.svcCtx.HttpServiceModel.SaveMapping(l.ctx, doc)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "保存失败: " + err.Error()}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

// HttpServiceMappingDeleteV2Logic 删除HTTP服务映射（使用新模型）
type HttpServiceMappingDeleteV2Logic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceMappingDeleteV2Logic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceMappingDeleteV2Logic {
	return &HttpServiceMappingDeleteV2Logic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceMappingDeleteV2Logic) HttpServiceMappingDeleteV2(req *types.HttpServiceMappingDeleteReq) (*types.BaseResp, error) {
	err := l.svcCtx.HttpServiceModel.DeleteMapping(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// ==================== HTTP服务映射导出/导入 ====================

// HttpServiceExportLogic 导出HTTP服务映射
type HttpServiceExportLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHttpServiceExportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceExportLogic {
	return &HttpServiceExportLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HttpServiceExportLogic) HttpServiceExport(req *types.HttpServiceExportReq) (*types.HttpServiceExportResp, error) {
	var sb strings.Builder

	// 1. 导出端口配置
	config, err := l.svcCtx.HttpServiceModel.GetConfig(l.ctx)
	if err != nil {
		return &types.HttpServiceExportResp{Code: 500, Msg: "获取端口配置失败: " + err.Error()}, nil
	}

	sb.WriteString("# HTTP服务映射配置\n")
	sb.WriteString("# 格式说明:\n")
	sb.WriteString("# [http_ports] HTTP端口列表\n")
	sb.WriteString("# [https_ports] HTTPS端口列表\n")
	sb.WriteString("# [non_http_ports] 非HTTP端口列表（明确排除）\n")
	sb.WriteString("# [service_mapping] 服务名称映射 (格式: 服务名=http/非http 描述)\n")
	sb.WriteString("\n")

	// HTTP端口
	sb.WriteString("[http_ports]\n")
	for _, port := range config.HttpPorts {
		sb.WriteString(fmt.Sprintf("%d\n", port))
	}
	sb.WriteString("\n")

	// HTTPS端口
	sb.WriteString("[https_ports]\n")
	for _, port := range config.HttpsPorts {
		sb.WriteString(fmt.Sprintf("%d\n", port))
	}
	sb.WriteString("\n")

	// 非HTTP端口
	sb.WriteString("[non_http_ports]\n")
	for _, port := range config.NonHttpPorts {
		sb.WriteString(fmt.Sprintf("%d\n", port))
	}
	sb.WriteString("\n")

	// 2. 导出服务映射
	mappings, err := l.svcCtx.HttpServiceModel.GetMappings(l.ctx)
	if err != nil {
		return &types.HttpServiceExportResp{Code: 500, Msg: "获取服务映射失败: " + err.Error()}, nil
	}

	sb.WriteString("[service_mapping]\n")
	for _, m := range mappings {
		httpType := "http"
		if !m.IsHttp {
			httpType = "non_http"
		}
		enabledStr := ""
		if !m.Enabled {
			enabledStr = " [disabled]"
		}
		desc := ""
		if m.Description != "" {
			desc = " # " + m.Description
		}
		sb.WriteString(fmt.Sprintf("%s=%s%s%s\n", m.ServiceName, httpType, enabledStr, desc))
	}

	return &types.HttpServiceExportResp{
		Code:    0,
		Msg:     "导出成功",
		Content: sb.String(),
	}, nil
}

// HttpServiceImportLogic 导入HTTP服务映射
type HttpServiceImportLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewHttpServiceImportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HttpServiceImportLogic {
	return &HttpServiceImportLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *HttpServiceImportLogic) HttpServiceImport(req *types.HttpServiceImportReq) (*types.HttpServiceImportResp, error) {
	if req.Content == "" {
		return &types.HttpServiceImportResp{Code: 400, Msg: "内容不能为空"}, nil
	}

	return l.ParseAndImport(req.Content)
}

// ParseAndImport 解析并导入HTTP服务映射配置
func (l *HttpServiceImportLogic) ParseAndImport(content string) (*types.HttpServiceImportResp, error) {
	lines := strings.Split(content, "\n")

	var httpPorts, httpsPorts, nonHttpPorts []int
	var serviceMappings []struct {
		ServiceName string
		IsHttp      bool
		Description string
		Enabled     bool
	}

	currentSection := ""
	var imported, skipped int

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 检测section
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(line[1 : len(line)-1])
			continue
		}

		switch currentSection {
		case "http_ports":
			if port := parsePort(line); port > 0 {
				httpPorts = append(httpPorts, port)
			}
		case "https_ports":
			if port := parsePort(line); port > 0 {
				httpsPorts = append(httpsPorts, port)
			}
		case "non_http_ports":
			if port := parsePort(line); port > 0 {
				nonHttpPorts = append(nonHttpPorts, port)
			}
		case "service_mapping":
			// 解析服务映射: serviceName=http/non_http [disabled] # description
			mapping := parseServiceMapping(line)
			if mapping.ServiceName != "" {
				serviceMappings = append(serviceMappings, mapping)
			}
		}
	}

	// 保存端口配置（如果有）
	if len(httpPorts) > 0 || len(httpsPorts) > 0 || len(nonHttpPorts) > 0 {
		config := &model.HttpServiceConfig{
			HttpPorts:    httpPorts,
			HttpsPorts:   httpsPorts,
			NonHttpPorts: nonHttpPorts,
		}
		if err := l.svcCtx.HttpServiceModel.SaveConfig(l.ctx, config); err != nil {
			l.Logger.Errorf("保存端口配置失败: %v", err)
		}
	}

	// 保存服务映射（去重）：循环外拉一次 mappings 全表，避免 N+1 查询
	existingMappings, _ := l.svcCtx.HttpServiceModel.GetMappings(l.ctx)
	existingSet := make(map[string]struct{}, len(existingMappings))
	for _, e := range existingMappings {
		existingSet[strings.ToLower(e.ServiceName)] = struct{}{}
	}
	for _, m := range serviceMappings {
		key := strings.ToLower(m.ServiceName)
		if _, exists := existingSet[key]; exists {
			skipped++
			continue
		}
		// 标记为已存在，防止导入数据内部重复
		existingSet[key] = struct{}{}
		doc := &model.HttpServiceMapping{
			ServiceName: key,
			IsHttp:      m.IsHttp,
			Description: m.Description,
			Enabled:     m.Enabled,
		}
		if err := l.svcCtx.HttpServiceModel.SaveMapping(l.ctx, doc); err != nil {
			l.Logger.Errorf("保存服务映射失败: %v", err)
			skipped++
		} else {
			imported++
		}
	}

	return &types.HttpServiceImportResp{
		Code:     0,
		Msg:      fmt.Sprintf("导入完成: 新增 %d 条, 跳过 %d 条（重复）", imported, skipped),
		Imported: imported,
		Skipped:  skipped,
	}, nil
}

// parsePort 解析端口号
func parsePort(line string) int {
	// 去除注释
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	var port int
	fmt.Sscanf(line, "%d", &port)
	if port > 0 && port <= 65535 {
		return port
	}
	return 0
}

// parseServiceMapping 解析服务映射行
func parseServiceMapping(line string) struct {
	ServiceName string
	IsHttp      bool
	Description string
	Enabled     bool
} {
	result := struct {
		ServiceName string
		IsHttp      bool
		Description string
		Enabled     bool
	}{Enabled: true}

	// 提取描述（# 后面的内容）
	if idx := strings.Index(line, "#"); idx >= 0 {
		result.Description = strings.TrimSpace(line[idx+1:])
		line = strings.TrimSpace(line[:idx])
	}

	// 检查是否禁用
	if strings.Contains(line, "[disabled]") {
		result.Enabled = false
		line = strings.Replace(line, "[disabled]", "", 1)
		line = strings.TrimSpace(line)
	}

	// 解析 serviceName=http/non_http
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return result
	}

	result.ServiceName = strings.TrimSpace(parts[0])
	httpType := strings.ToLower(strings.TrimSpace(parts[1]))
	result.IsHttp = (httpType == "http" || httpType == "true" || httpType == "1")

	return result
}

// ==================== 验证结果同步到资产库（暴露面管理） ====================

// syncValidateResultsToAssets 将批量验证匹配的指纹同步到资产库的app字段，并更新资产概览
func (l *FingerprintBatchValidateLogic) syncValidateResultsToAssets(ctx context.Context, rawUrl string, results []types.MatchedFingerprintInfo) {
	if len(results) == 0 {
		return
	}

	// 解析URL获取host/port/scheme
	u, err := url.Parse(rawUrl)
	if err != nil {
		l.Logger.Errorf("syncValidateResultsToAssets: parse url failed: %v", err)
		return
	}
	host := u.Hostname()
	portStr := u.Port()
	scheme := strings.ToLower(u.Scheme)
	if host == "" {
		l.Logger.Errorf("syncValidateResultsToAssets: empty host from url: %s", rawUrl)
		return
	}
	port := 0
	switch {
	case portStr != "":
		fmt.Sscanf(portStr, "%d", &port)
	case scheme == "https":
		port = 443
	case scheme == "http":
		port = 80
	}
	authority := u.Host
	if authority == "" {
		authority = net.JoinHostPort(host, portStr)
	}
	isHttp := scheme == "http" || scheme == "https"

	assetModel := l.svcCtx.GetAssetModel()
	targetMetaModel := l.svcCtx.GetAssetTargetMetaModel()
	now := time.Now()

	// 收集匹配的app名称（去重）
	appNames := make([]string, 0)
	seen := make(map[string]struct{})
	for _, r := range results {
		if r.Name != "" {
			if _, ok := seen[r.Name]; !ok {
				seen[r.Name] = struct{}{}
				appNames = append(appNames, r.Name)
			}
		}
	}
	if len(appNames) == 0 {
		return
	}

	// 查找现有资产
	existingAsset, _ := assetModel.FindByHostPort(ctx, host, port)
	if existingAsset == nil {
		existingAsset, _ = assetModel.FindByAuthorityOnly(ctx, authority)
	}

	if existingAsset != nil {
		// 资产已存在：$addToSet app字段
		update := bson.M{
			"$addToSet": bson.M{"app": bson.M{"$each": appNames}},
			"$set": bson.M{
				"update":                  true,
				"last_status_change_time": now,
				"update_time":             now,
			},
		}
		if isHttp {
			update["$set"].(bson.M)["is_http"] = true
		}
		if err := assetModel.UpdateWithRaw(ctx, existingAsset.Id.Hex(), update); err != nil {
			l.Logger.Errorf("syncValidateResultsToAssets: update asset failed: %v", err)
		} else {
			l.Logger.Infof("syncValidateResultsToAssets: updated asset %s with apps %v", authority, appNames)
		}
	} else {
		// 资产不存在：创建新资产
		newAsset := &model.Asset{
			Authority:  authority,
			Host:       host,
			Port:       port,
			Category:   scheme,
			IsHTTP:     isHttp,
			App:        appNames,
			Source:     "manual_validate",
			IsNewAsset: true,
			IsUpdated:  true,
			TaskId:     "manual_validate",
			CreateTime: now,
			UpdateTime: now,
		}
		if err := assetModel.Insert(ctx, newAsset); err != nil {
			l.Logger.Errorf("syncValidateResultsToAssets: insert asset failed: %v", err)
		} else {
			l.Logger.Infof("syncValidateResultsToAssets: created new asset %s with apps %v", authority, appNames)
		}
	}

	// 确保AssetTargetMeta存在并更新暴露面计数
	domain := ""
	if err := targetMetaModel.EnsureForAsset(ctx, host, domain, nil); err != nil {
		l.Logger.Errorf("syncValidateResultsToAssets: EnsureForAsset failed: %v", err)
	}
}
