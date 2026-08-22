package common

import (
	"context"
	"regexp"
	"strings"

	"cscan/api/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// InjectPocConfig 注入POC模板ID到任务配置（不存储完整内容，避免文档过大）
func InjectPocConfig(ctx context.Context, svcCtx *svc.ServiceContext, taskConfig map[string]interface{}, logger logx.Logger) map[string]interface{} {
	pocscan, ok := taskConfig["pocscan"].(map[string]interface{})
	if !ok || pocscan == nil {
		return taskConfig
	}

	// 检查是否启用POC扫描和Nuclei
	enable, _ := pocscan["enable"].(bool)
	useNuclei, _ := pocscan["useNuclei"].(bool)
	if !enable || !useNuclei {
		return taskConfig
	}

	// 检查前端是否已经传递了手动选择的POC ID列表
	existingNucleiIds := getStringSlice(pocscan, "nucleiTemplateIds")
	existingCustomIds := getStringSlice(pocscan, "customPocIds")

	// 手动全选模式：前端只传选择意图（selectAll 标记 + 筛选条件），由后端按条件查询展开为 ID 列表
	if len(existingNucleiIds) == 0 && getBool(pocscan, "nucleiSelectAll") {
		if ids := resolveNucleiSelectAll(ctx, svcCtx, pocscan); len(ids) > 0 {
			pocscan["nucleiTemplateIds"] = ids
			existingNucleiIds = ids
			logger.Infof("Manual select-all: expanded %d nuclei templates by filter", len(ids))
		}
	}
	if len(existingCustomIds) == 0 && getBool(pocscan, "customPocSelectAll") {
		if ids := resolveCustomPocSelectAll(ctx, svcCtx, pocscan); len(ids) > 0 {
			pocscan["customPocIds"] = ids
			existingCustomIds = ids
			logger.Infof("Manual select-all: expanded %d custom POCs by filter", len(ids))
		}
	}

	// 如果前端已经传递了ID列表（手动选择模式），直接使用，不再自动注入
	if len(existingNucleiIds) > 0 || len(existingCustomIds) > 0 {
		logger.Infof("Manual POC selection mode: using %d nuclei templates and %d custom POCs from frontend",
			len(existingNucleiIds), len(existingCustomIds))
		return taskConfig
	}

	// 手动选择模式但未选择任何POC：不再自动注入模板，避免与用户手动选择的意图不符
	if mode, _ := pocscan["mode"].(string); mode == "manual" {
		logger.Infof("Manual POC selection mode: no POC selected, skipping template injection")
		taskConfig["pocscan"] = pocscan
		return taskConfig
	}

	// 检查是否启用自动扫描模式
	autoScan, _ := pocscan["autoScan"].(bool)
	automaticScan, _ := pocscan["automaticScan"].(bool)

	// 如果启用了自动扫描，不预先注入模板ID，让Worker根据资产指纹动态获取
	if autoScan || automaticScan {
		logger.Infof("Auto-scan enabled (autoScan=%v, automaticScan=%v), skipping template ID injection", autoScan, automaticScan)

		// 只注入标签映射（用于自定义标签映射模式）
		if autoScan {
			tagMappings, err := svcCtx.TagMappingModel.FindEnabled(ctx)
			if err == nil && len(tagMappings) > 0 {
				mappings := make(map[string][]string)
				for _, tm := range tagMappings {
					mappings[tm.AppName] = tm.NucleiTags
				}
				pocscan["tagMappings"] = mappings
				logger.Infof("Injected %d tag mappings for auto-scan", len(mappings))
			}
		}

		taskConfig["pocscan"] = pocscan
		return taskConfig
	}

	customPocOnly, _ := pocscan["customPocOnly"].(bool)
	var nucleiTemplateIds []string
	var customPocIds []string

	if customPocOnly {
		// 只使用自定义POC - 存储ID列表
		customPocs, err := svcCtx.CustomPocModel.FindEnabled(ctx)
		if err == nil && len(customPocs) > 0 {
			for _, poc := range customPocs {
				customPocIds = append(customPocIds, poc.Id.Hex())
			}
			logger.Infof("Injected %d custom POC IDs (CustomPocOnly mode)", len(customPocIds))
		}
	} else {
		// 从数据库获取默认模板ID（根据严重级别筛选）
		severityStr, _ := pocscan["severity"].(string)
		if severityStr != "" {
			severities := strings.Split(severityStr, ",")
			nucleiTemplates, err := svcCtx.NucleiTemplateModel.FindBySeverity(ctx, severities)
			if err == nil && len(nucleiTemplates) > 0 {
				for _, t := range nucleiTemplates {
					// 注意：Worker 按 template_id 匹配（FindByIds），不能使用 Mongo _id
					nucleiTemplateIds = append(nucleiTemplateIds, t.TemplateId)
				}
				logger.Infof("Injected %d nuclei template IDs (severity: %s)", len(nucleiTemplateIds), severityStr)
			}
		}

		// 添加自定义POC ID
		customPocs, err := svcCtx.CustomPocModel.FindEnabled(ctx)
		if err == nil && len(customPocs) > 0 {
			for _, poc := range customPocs {
				customPocIds = append(customPocIds, poc.Id.Hex())
			}
			logger.Infof("Added %d custom POC IDs", len(customPocIds))
		}
	}

	// 存储ID列表而不是完整内容
	if len(nucleiTemplateIds) > 0 {
		pocscan["nucleiTemplateIds"] = nucleiTemplateIds
	}
	if len(customPocIds) > 0 {
		pocscan["customPocIds"] = customPocIds
	}

	taskConfig["pocscan"] = pocscan
	return taskConfig
}

// getStringSlice 从map中获取字符串切片
func getStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case []string:
			return val
		case []interface{}:
			result := make([]string, 0, len(val))
			for _, item := range val {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}

// getBool 从map中获取布尔值
func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// resolveNucleiSelectAll 手动全选模式：按筛选条件查询并展开为 template_id 列表
// 筛选语义与 NucleiTemplateList 列表接口保持一致，保证全选数量与展开数量一致
func resolveNucleiSelectAll(ctx context.Context, svcCtx *svc.ServiceContext, pocscan map[string]interface{}) []string {
	filter := bson.M{}
	if f, ok := pocscan["nucleiSelectAllFilter"].(map[string]interface{}); ok {
		if s, _ := f["keyword"].(string); s != "" {
			kw := regexp.QuoteMeta(s)
			cond := bson.M{"$regex": kw, "$options": "i"}
			filter["$or"] = []bson.M{
				{"template_id": cond},
				{"name": cond},
				{"description": cond},
				{"tags": cond},
				{"author": cond},
				{"cve_ids": cond},
				{"cwe_ids": cond},
				{"product": cond},
				{"vendor": cond},
				{"category": cond},
				{"file_path": cond},
			}
		}
		if s, _ := f["tag"].(string); s != "" {
			filter["tags"] = bson.M{"$regex": regexp.QuoteMeta(s), "$options": "i"}
		}
		if s, _ := f["category"].(string); s != "" {
			filter["category"] = s
		}
		if severities := mergedSeverities(f); len(severities) == 1 {
			filter["severity"] = severities[0]
		} else if len(severities) > 1 {
			filter["severity"] = bson.M{"$in": severities}
		}
		if protocols := getStringSlice(f, "protocols"); len(protocols) > 0 {
			filter["protocol"] = bson.M{"$in": protocols}
		}
		if products := getStringSlice(f, "products"); len(products) > 0 {
			filter["product"] = bson.M{"$in": products}
		}
		if v, ok := f["hasCve"].(bool); ok {
			filter["cve_ids.0"] = bson.M{"$exists": v}
		}
	}
	docs, err := svcCtx.NucleiTemplateModel.SelectAll(ctx, filter)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.TemplateId)
	}
	return ids
}

// resolveCustomPocSelectAll 手动全选模式：按筛选条件查询并展开为自定义POC ID列表
// 筛选语义与 CustomPocList 列表接口保持一致，保证全选数量与展开数量一致
func resolveCustomPocSelectAll(ctx context.Context, svcCtx *svc.ServiceContext, pocscan map[string]interface{}) []string {
	filter := bson.M{"enabled": true}
	if f, ok := pocscan["customPocSelectAllFilter"].(map[string]interface{}); ok {
		if s, _ := f["name"].(string); s != "" {
			filter["name"] = bson.M{"$regex": regexp.QuoteMeta(s), "$options": "i"}
		}
		if s, _ := f["keyword"].(string); s != "" {
			kw := regexp.QuoteMeta(s)
			cond := bson.M{"$regex": kw, "$options": "i"}
			filter["$or"] = []bson.M{
				{"template_id": cond},
				{"name": cond},
				{"description": cond},
				{"tags": cond},
				{"author": cond},
				{"cve_ids": cond},
				{"cwe_ids": cond},
				{"product": cond},
				{"vendor": cond},
				{"protocol": cond},
			}
		}
		if s, _ := f["tag"].(string); s != "" {
			filter["tags"] = bson.M{"$in": []string{s}}
		}
		if severities := mergedSeverities(f); len(severities) == 1 {
			filter["severity"] = severities[0]
		} else if len(severities) > 1 {
			filter["severity"] = bson.M{"$in": severities}
		}
		if protocols := getStringSlice(f, "protocols"); len(protocols) > 0 {
			filter["protocol"] = bson.M{"$in": protocols}
		}
		if products := getStringSlice(f, "products"); len(products) > 0 {
			filter["product"] = bson.M{"$in": products}
		}
		if v, ok := f["hasCve"].(bool); ok {
			filter["cve_ids.0"] = bson.M{"$exists": v}
		}
	}
	docs, err := svcCtx.CustomPocModel.SelectAll(ctx, filter)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.Id.Hex())
	}
	return ids
}

// mergedSeverities 合并筛选条件中的单选 severity 与多选 severities（小写化、去空），
// 与列表接口 buildFilter 的兼容逻辑一致
func mergedSeverities(f map[string]interface{}) []string {
	seen := make(map[string]bool)
	severities := make([]string, 0, 4)
	appendSeverity := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !seen[s] {
			seen[s] = true
			severities = append(severities, s)
		}
	}
	for _, s := range getStringSlice(f, "severities") {
		appendSeverity(s)
	}
	if s, _ := f["severity"].(string); s != "" {
		appendSeverity(s)
	}
	return severities
}
