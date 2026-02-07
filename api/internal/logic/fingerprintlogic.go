package logic

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/yaml.v3"
)

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
					{"name": bson.M{"$regex": keyword, "$options": "i"}},
				}
			} else {
				filter["name"] = bson.M{"$regex": keyword, "$options": "i"}
			}
		} else {
			// 普通关键字搜索name
			filter["name"] = bson.M{"$regex": keyword, "$options": "i"}
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
	docs, err := l.svcCtx.FingerprintModel.Find(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		return &types.FingerprintListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.Fingerprint, 0, len(docs))
	for _, doc := range docs {
		list = append(list, types.Fingerprint{
			Id:          doc.Id.Hex(),
			Name:        doc.Name,
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

	// 从 ActiveFingerprintModel 获取主动指纹数量
	activeStats, _ := l.svcCtx.ActiveFingerprintModel.GetStats(l.ctx)
	if activeStats != nil {
		stats["active"] = activeStats["total"]
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
	var parseErr error

	// 方式1: 直接解析为数组 [{name, rule}]
	parseErr = yaml.Unmarshal([]byte(content), &fingerprints)

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

// parseIntArray 解析可能是数组或单个数字的字段
func parseIntArray(v interface{}) []int {
	var result []int
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			if n, ok := item.(float64); ok {
				result = append(result, int(n))
			}
		}
	case float64:
		result = append(result, int(val))
	}
	return result
}

// FingerprintValidateLogic 验证指纹
type FingerprintValidateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintValidateLogic {
	return &FingerprintValidateLogic{ctx: ctx, svcCtx: svcCtx}
}

// FingerprintValidate 验证单个指纹是否能匹配目标URL（直接在API服务中执行）
func (l *FingerprintValidateLogic) FingerprintValidate(req *types.FingerprintValidateReq) (*types.FingerprintValidateResp, error) {
	if req.Url == "" {
		return &types.FingerprintValidateResp{Code: 400, Msg: "URL不能为空"}, nil
	}
	if req.Id == "" {
		return &types.FingerprintValidateResp{Code: 400, Msg: "指纹ID不能为空"}, nil
	}

	// 从数据库获取指纹
	fp, err := l.svcCtx.FingerprintModel.FindById(l.ctx, req.Id)
	if err != nil {
		return &types.FingerprintValidateResp{Code: 404, Msg: "指纹不存在"}, nil
	}

	// 获取目标数据
	data, err := fetchFingerprintData(req.Url)
	if err != nil {
		return &types.FingerprintValidateResp{Code: 500, Msg: "获取目标数据失败: " + err.Error()}, nil
	}

	// 创建指纹引擎并验证
	engine := NewSingleFingerprintEngine(fp)
	matched, conditions := engine.MatchWithDetails(data)

	return &types.FingerprintValidateResp{
		Code:    0,
		Msg:     "验证完成",
		Matched: matched,
		Details: strings.Join(conditions, "\n"),
	}, nil
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

func (e *SingleFingerprintEngine) Match(data *FingerprintData) bool {
	matched, _ := e.MatchWithDetails(data)
	return matched
}

// MatchWithDetails 执行匹配并返回匹配的条件详情
func (e *SingleFingerprintEngine) MatchWithDetails(data *FingerprintData) (bool, []string) {
	fp := e.fp

	// 优先使用Rule字段（ARL格式规则语法）
	if fp.Rule != "" {
		return matchRuleWithDetails(fp.Rule, data)
	}

	// 使用Wappalyzer格式规则
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

// fetchFingerprintData 请求URL获取指纹匹配数据
func fetchFingerprintData(targetUrl string) (*FingerprintData, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
	hash := calculateMMH3Hash(faviconBytes)
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

// calculateMMH3Hash 计算Shodan风格的MMH3 favicon hash
func calculateMMH3Hash(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// Shodan的favicon hash计算方式：
	// 1. Base64编码（标准编码，每76字符换行）
	// 2. 计算MMH3 hash
	b64 := base64.StdEncoding.EncodeToString(data)

	// 添加换行符（每76字符）模拟标准base64输出
	var b64WithNewlines strings.Builder
	for i := 0; i < len(b64); i += 76 {
		end := i + 76
		if end > len(b64) {
			end = len(b64)
		}
		b64WithNewlines.WriteString(b64[i:end])
		b64WithNewlines.WriteString("\n")
	}

	hash := mmh3Hash32([]byte(b64WithNewlines.String()))
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

// matchRule 匹配ARL格式规则
func matchRule(rule string, data *FingerprintData) bool {
	matched, _ := matchRuleWithDetails(rule, data)
	return matched
}

// matchRuleWithDetails 匹配ARL格式规则并返回匹配的条件
func matchRuleWithDetails(rule string, data *FingerprintData) (bool, []string) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false, nil
	}

	// 处理OR逻辑 (||)
	parts := splitByOperator(rule, "||")
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

func matchRuleAnd(rule string, data *FingerprintData) bool {
	matched, _ := matchRuleAndWithDetails(rule, data)
	return matched
}

func matchRuleAndWithDetails(rule string, data *FingerprintData) (bool, []string) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false, nil
	}

	var matchedConditions []string

	parts := splitByOperator(rule, "&&")
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

func matchSingleCondition(condition string, data *FingerprintData) bool {
	matched, _ := matchSingleConditionWithDetails(condition, data)
	return matched
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

	var result bool
	var matchedValue string
	condTypeLower := strings.ToLower(condType)

	switch condTypeLower {
	case "body":
		result = containsIgnoreCase(data.Body, value)
		if result {
			matchedValue = findMatchContext(data.Body, value, 50)
		}
	case "title":
		result = containsIgnoreCase(data.Title, value)
		if result {
			matchedValue = data.Title
		}
	case "header":
		result = containsIgnoreCase(data.HeaderString, value)
		if result {
			matchedValue = findMatchContext(data.HeaderString, value, 100)
		}
	case "server":
		result = containsIgnoreCase(data.Server, value)
		if result {
			matchedValue = data.Server
		}
	case "url":
		result = containsIgnoreCase(data.URL, value)
		if result {
			matchedValue = data.URL
		}
	case "cookie":
		result = containsIgnoreCase(data.Cookies, value)
		if result {
			matchedValue = findMatchContext(data.Cookies, value, 100)
		}
	case "icon_hash", "favicon_hash":
		result = data.FaviconHash == value
		if result {
			matchedValue = data.FaviconHash
		}
	default:
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
			detail = fmt.Sprintf("%s = \"%s\" → 匹配到: %s", condType, value, truncateString(matchedValue, 80))
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

func extractQuotedValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return s
	}
	if s[0] == '"' || s[0] == '\'' {
		quoteChar := s[0]
		for i := 1; i < len(s); i++ {
			if s[i] == quoteChar && (i <= 1 || s[i-1] != '\\') {
				// 提取值并处理转义引号
				value := s[1:i]
				return unescapeQuotes(value, quoteChar)
			}
		}
		// 没有找到结束引号，返回去掉开头引号的内容，并处理转义
		value := s[1:]
		return unescapeQuotes(value, quoteChar)
	}
	if s[len(s)-1] == '"' || s[len(s)-1] == '\'' {
		return s[:len(s)-1]
	}
	return s
}

// unescapeQuotes 将转义的引号还原
// 例如：id=\"swagger-ui 还原为 id="swagger-ui
func unescapeQuotes(s string, quoteChar byte) string {
	if quoteChar == '"' {
		return strings.ReplaceAll(s, `\"`, `"`)
	} else if quoteChar == '\'' {
		return strings.ReplaceAll(s, `\'`, `'`)
	}
	return s
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func matchWappalyzerRules(fp *model.Fingerprint, data *FingerprintData) bool {
	matched, _ := matchWappalyzerRulesWithDetails(fp, data)
	return matched
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
			// 遍历响应头，大小写不敏感匹配key
			for hKey, hVal := range data.Headers {
				if strings.EqualFold(hKey, key) {
					headerValue := strings.Join(hVal, " ")
					if pattern == "" {
						// 只要header存在就匹配
						headerMatch = true
						matchedConditions = append(matchedConditions, fmt.Sprintf("header[%s] 存在 → 匹配到: %s", key, truncateString(headerValue, 80)))
						break
					}
					// pattern是正则表达式
					if matchRegexOrContains(headerValue, pattern) {
						headerMatch = true
						matchedConditions = append(matchedConditions, fmt.Sprintf("header[%s] =~ \"%s\" → 匹配到: %s", key, truncateString(pattern, 50), truncateString(headerValue, 80)))
						break
					}
				}
			}
			if headerMatch {
				break
			}
		}
		if !headerMatch {
			allMatch = false
		}
	}

	// HTML匹配 - pattern是正则表达式
	if len(fp.HTML) > 0 && allMatch {
		hasRule = true
		htmlMatch := false
		for _, pattern := range fp.HTML {
			if matchRegexOrContains(data.Body, pattern) {
				htmlMatch = true
				matchedConditions = append(matchedConditions, fmt.Sprintf("html =~ \"%s\" → 匹配到", truncateString(pattern, 50)))
				break
			}
		}
		if !htmlMatch {
			allMatch = false
		}
	}

	// Scripts匹配 - pattern是正则表达式，匹配script标签的src属性
	if len(fp.Scripts) > 0 && allMatch {
		hasRule = true
		scriptMatch := false
		// 提取所有script src
		scriptSrcRe := regexp.MustCompile(`(?i)<script[^>]*src=["']([^"']+)["']`)
		scriptSrcs := scriptSrcRe.FindAllStringSubmatch(data.Body, -1)
		for _, pattern := range fp.Scripts {
			for _, src := range scriptSrcs {
				if len(src) > 1 && matchRegexOrContains(src[1], pattern) {
					scriptMatch = true
					matchedConditions = append(matchedConditions, fmt.Sprintf("scripts =~ \"%s\" → 匹配到: %s", truncateString(pattern, 50), truncateString(src[1], 80)))
					break
				}
			}
			if scriptMatch {
				break
			}
		}
		if !scriptMatch {
			allMatch = false
		}
	}

	// ScriptSrc匹配
	if len(fp.ScriptSrc) > 0 && allMatch {
		hasRule = true
		scriptSrcMatch := false
		scriptSrcRe := regexp.MustCompile(`(?i)<script[^>]*src=["']([^"']+)["']`)
		scriptSrcs := scriptSrcRe.FindAllStringSubmatch(data.Body, -1)
		for _, pattern := range fp.ScriptSrc {
			for _, src := range scriptSrcs {
				if len(src) > 1 && matchRegexOrContains(src[1], pattern) {
					scriptSrcMatch = true
					matchedConditions = append(matchedConditions, fmt.Sprintf("scriptSrc =~ \"%s\" → 匹配到: %s", truncateString(pattern, 50), truncateString(src[1], 80)))
					break
				}
			}
			if scriptSrcMatch {
				break
			}
		}
		if !scriptSrcMatch {
			allMatch = false
		}
	}

	// Cookies匹配
	if len(fp.Cookies) > 0 && allMatch {
		hasRule = true
		cookieMatch := false
		for key, pattern := range fp.Cookies {
			if containsIgnoreCase(data.Cookies, key) {
				if pattern == "" || matchRegexOrContains(data.Cookies, pattern) {
					cookieMatch = true
					matchedConditions = append(matchedConditions, fmt.Sprintf("cookie[%s] =~ \"%s\" → 匹配到", key, pattern))
					break
				}
			}
		}
		if !cookieMatch {
			allMatch = false
		}
	}

	// Meta匹配
	if len(fp.Meta) > 0 && allMatch {
		hasRule = true
		metaMatch := false
		for key, pattern := range fp.Meta {
			// 在body中搜索meta标签，支持多种格式
			// 格式1: <meta name="xxx" content="yyy">
			// 格式2: <meta content="yyy" name="xxx">
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
		}
	}

	// CSS匹配
	if len(fp.CSS) > 0 && allMatch {
		hasRule = true
		cssMatch := false
		for _, pattern := range fp.CSS {
			if matchRegexOrContains(data.Body, pattern) {
				cssMatch = true
				matchedConditions = append(matchedConditions, fmt.Sprintf("css =~ \"%s\" → 匹配到", truncateString(pattern, 50)))
				break
			}
		}
		if !cssMatch {
			allMatch = false
		}
	}

	// URL匹配
	if len(fp.URL) > 0 && allMatch {
		hasRule = true
		urlMatch := false
		for _, pattern := range fp.URL {
			if matchRegexOrContains(data.URL, pattern) {
				urlMatch = true
				matchedConditions = append(matchedConditions, fmt.Sprintf("url =~ \"%s\" → 匹配到: %s", truncateString(pattern, 50), data.URL))
				break
			}
		}
		if !urlMatch {
			allMatch = false
		}
	}

	if hasRule && allMatch {
		return true, matchedConditions
	}
	return false, nil
}

// matchRegexOrContains 尝试正则匹配，如果正则无效则回退到字符串包含匹配
func matchRegexOrContains(text, pattern string) bool {
	if pattern == "" {
		return true
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
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFingerprintBatchValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FingerprintBatchValidateLogic {
	return &FingerprintBatchValidateLogic{ctx: ctx, svcCtx: svcCtx}
}

// FingerprintBatchValidate 批量验证所有指纹（直接在API服务中执行）
func (l *FingerprintBatchValidateLogic) FingerprintBatchValidate(req *types.FingerprintBatchValidateReq) (*types.FingerprintBatchValidateResp, error) {
	if req.Url == "" {
		return &types.FingerprintBatchValidateResp{Code: 400, Msg: "URL不能为空"}, nil
	}

	startTime := time.Now()

	// 获取目标数据
	data, err := fetchFingerprintData(req.Url)
	if err != nil {
		return &types.FingerprintBatchValidateResp{Code: 500, Msg: "获取目标数据失败: " + err.Error()}, nil
	}

	// 获取所有启用的指纹
	filter := map[string]interface{}{"enabled": true}
	if req.Scope == "builtin" {
		filter["is_builtin"] = true
	} else if req.Scope == "custom" {
		filter["is_builtin"] = false
	}

	fingerprints, err := l.svcCtx.FingerprintModel.Find(l.ctx, filter, 0, 0)
	if err != nil {
		return &types.FingerprintBatchValidateResp{Code: 500, Msg: "获取指纹列表失败: " + err.Error()}, nil
	}

	// 批量验证
	var matched []types.MatchedFingerprintInfo
	for _, fp := range fingerprints {
		engine := NewSingleFingerprintEngine(&fp)
		if isMatched, conditions := engine.MatchWithDetails(data); isMatched {
			matched = append(matched, types.MatchedFingerprintInfo{
				Id:                fp.Id.Hex(),
				Name:              fp.Name,
				IsBuiltin:         fp.IsBuiltin,
				MatchedConditions: strings.Join(conditions, "\n"),
			})
		}
	}

	duration := time.Since(startTime)
	return &types.FingerprintBatchValidateResp{
		Code:         0,
		Msg:          fmt.Sprintf("验证完成，共检测 %d 个指纹", len(fingerprints)),
		MatchedCount: len(matched),
		Duration:     fmt.Sprintf("%.2fs", duration.Seconds()),
		Matched:      matched,
	}, nil
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
func (l *FingerprintMatchAssetsLogic) FingerprintMatchAssets(req *types.FingerprintMatchAssetsReq, workspaceId string) (*types.FingerprintMatchAssetsResp, error) {
	if req.FingerprintId == "" {
		return &types.FingerprintMatchAssetsResp{Code: 400, Msg: "指纹ID不能为空"}, nil
	}

	startTime := time.Now()

	// 获取指纹
	fp, err := l.svcCtx.FingerprintModel.FindById(l.ctx, req.FingerprintId)
	if err != nil {
		return &types.FingerprintMatchAssetsResp{Code: 404, Msg: "指纹不存在"}, nil
	}

	// 获取资产列表（只获取有HTTP响应数据的资产）
	assetModel := l.svcCtx.GetAssetModel(workspaceId)
	// 查询有 body 或 header 或 title 的资产
	filter := map[string]interface{}{
		"$or": []map[string]interface{}{
			{"body": map[string]interface{}{"$ne": ""}},
			{"header": map[string]interface{}{"$ne": ""}},
			{"title": map[string]interface{}{"$ne": ""}},
			{"icon_hash": map[string]interface{}{"$ne": ""}},
		},
	}

	assets, err := assetModel.Find(l.ctx, filter, 0, 0)
	if err != nil {
		return &types.FingerprintMatchAssetsResp{Code: 500, Msg: "获取资产列表失败: " + err.Error()}, nil
	}

	l.Logger.Infof("FingerprintMatchAssets: fingerprintId=%s, name=%s, totalAssets=%d, updateAsset=%v", req.FingerprintId, fp.Name, len(assets), req.UpdateAsset)

	// 创建指纹引擎
	engine := NewSingleFingerprintEngine(fp)

	// 匹配资产（匹配所有资产，不再限制数量）
	var matchedList []types.FingerprintMatchedAsset
	var updatedCount int
	for _, asset := range assets {
		// 构建指纹数据
		data := &FingerprintData{
			Title:        asset.Title,
			Body:         asset.HttpBody,
			HeaderString: asset.HttpHeader,
			Server:       asset.Server,
			FaviconHash:  asset.IconHash,
			URL:          asset.Authority,
		}

		// 解析 Header 字符串为 map
		if asset.HttpHeader != "" {
			data.Headers = parseHeaderString(asset.HttpHeader)
		}

		// 执行匹配
		if matched, _ := engine.MatchWithDetails(data); matched {
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
				// 检查指纹是否已存在
				fpExists := false
				for _, app := range asset.App {
					if app == fp.Name {
						fpExists = true
						break
					}
				}
				// 如果不存在，添加指纹
				if !fpExists {
					newApps := append(asset.App, fp.Name)

					// 构建更新内容，同时标记资产为已更新
					update := bson.M{
						"app":                     newApps,
						"update":                  true, // 标记为有更新
						"last_status_change_time": time.Now(),
					}

					err := assetModel.Update(l.ctx, asset.Id.Hex(), update)
					if err == nil {
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

	// 保存服务映射（去重）
	for _, m := range serviceMappings {
		// 检查是否已存在
		existing, _ := l.svcCtx.HttpServiceModel.GetMappings(l.ctx)
		exists := false
		for _, e := range existing {
			if strings.ToLower(e.ServiceName) == strings.ToLower(m.ServiceName) {
				exists = true
				skipped++
				break
			}
		}
		if !exists {
			doc := &model.HttpServiceMapping{
				ServiceName: strings.ToLower(m.ServiceName),
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
