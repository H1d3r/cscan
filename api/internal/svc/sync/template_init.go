package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"gopkg.in/yaml.v3"
)

// TemplateYAML YAML模板文件结构
type TemplateYAML struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Category    string                 `yaml:"category"`
	Tags        []string               `yaml:"tags"`
	SortNumber  int                    `yaml:"sort_number"`
	Config      map[string]interface{} `yaml:"config"`
}

// LoadBuiltinTemplateConfig 从 rules/scan-template 文件加载指定分类的内置模板配置（map 形式）。
// 供引导扫描（quickCreate）等场景在运行时直接读取文件，避免硬编码副本与模板漂移。
// 找不到文件或对应分类时返回 nil。
func LoadBuiltinTemplateConfig(category string) map[string]interface{} {
	templates := loadTemplatesFromFiles()
	for _, t := range templates {
		if t.Category == category && t.Config != "" {
			var cfg map[string]interface{}
			if err := json.Unmarshal([]byte(t.Config), &cfg); err == nil {
				return cfg
			}
		}
	}
	return nil
}

// InitBuiltinTemplates 初始化内置扫描模板
func InitBuiltinTemplates(templateModel *model.ScanTemplateModel) {
	ctx := context.Background()

	// 检查是否已有内置模板
	builtins, err := templateModel.FindBuiltinTemplates(ctx)
	if err == nil && len(builtins) > 0 {
		logx.Infof("[TemplateInit] Found %d builtin templates, skip init", len(builtins))
		return
	}

	logx.Info("[TemplateInit] Initializing builtin scan templates...")

	// 从文件加载模板
	templates := loadTemplatesFromFiles()
	if len(templates) == 0 {
		logx.Info("[TemplateInit] No template files found, using default templates")
		templates = getDefaultTemplates()
	}

	for _, t := range templates {
		if err := templateModel.Insert(ctx, &t); err != nil {
			logx.Errorf("[TemplateInit] Failed to insert template %s: %v", t.Name, err)
		} else {
			logx.Infof("[TemplateInit] Created builtin template: %s", t.Name)
		}
	}

	logx.Infof("[TemplateInit] Builtin templates initialized, total: %d", len(templates))
}

// loadTemplatesFromFiles 从 rules/scan-template 目录加载模板
func loadTemplatesFromFiles() []model.ScanTemplate {
	var templates []model.ScanTemplate

	// 尝试多个可能的路径
	possiblePaths := []string{
		"rules/scan-template",
		"../rules/scan-template",
		"../../rules/scan-template",
	}

	var templateDir string
	for _, p := range possiblePaths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			templateDir = p
			break
		}
	}

	if templateDir == "" {
		logx.Info("[TemplateInit] Template directory not found")
		return templates
	}

	logx.Infof("[TemplateInit] Loading templates from: %s", templateDir)

	entries, err := os.ReadDir(templateDir)
	if err != nil {
		logx.Errorf("[TemplateInit] Failed to read template directory: %v", err)
		return templates
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		isYAML := strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
		isJSON := strings.HasSuffix(name, ".json")
		if !isYAML && !isJSON {
			continue
		}

		filePath := filepath.Join(templateDir, name)
		var t *model.ScanTemplate
		var err error

		if isJSON {
			t, err = loadTemplateFromJSONFile(filePath)
		} else {
			t, err = loadTemplateFromYAMLFile(filePath)
		}

		if err != nil {
			logx.Errorf("[TemplateInit] Failed to load template %s: %v", name, err)
			continue
		}

		templates = append(templates, *t)
		logx.Infof("[TemplateInit] Loaded template from file: %s", name)
	}

	return templates
}

// loadTemplateFromYAMLFile 从YAML文件加载模板
func loadTemplateFromYAMLFile(filePath string) (*model.ScanTemplate, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var yamlTemplate TemplateYAML
	if err := yaml.Unmarshal(data, &yamlTemplate); err != nil {
		return nil, err
	}

	// 将 config map 转为 JSON 字符串
	configJSON, err := json.Marshal(yamlTemplate.Config)
	if err != nil {
		return nil, err
	}

	return &model.ScanTemplate{
		Name:        yamlTemplate.Name,
		Description: yamlTemplate.Description,
		Category:    yamlTemplate.Category,
		Tags:        yamlTemplate.Tags,
		Config:      string(configJSON),
		IsBuiltin:   true,
		SortNumber:  yamlTemplate.SortNumber,
	}, nil
}

// loadTemplateFromJSONFile 从JSON文件加载模板
// JSON文件格式与前端展示格式一致：顶层包含 name/target/template + 各扫描模块配置
// 提取 name/description/category/tags/sort_number 作为模板元数据，
// 其余字段（target/template/domainscan/portscan/...）作为 config 存储
func loadTemplateFromJSONFile(filePath string) (*model.ScanTemplate, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// 先解析为通用 map 提取元数据和配置
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// 提取模板元数据
	name, _ := raw["name"].(string)
	description, _ := raw["description"].(string)
	category, _ := raw["category"].(string)
	sortNumber := 0
	if sn, ok := raw["sort_number"].(float64); ok {
		sortNumber = int(sn)
	}
	var tags []string
	if t, ok := raw["tags"].([]interface{}); ok {
		for _, item := range t {
			if s, ok := item.(string); ok {
				tags = append(tags, s)
			}
		}
	}

	// 从原始 map 中移除元数据字段，剩余的即为 config
	metaKeys := []string{"name", "description", "category", "tags", "sort_number"}
	configMap := make(map[string]interface{})
	for k, v := range raw {
		isMeta := false
		for _, mk := range metaKeys {
			if k == mk {
				isMeta = true
				break
			}
		}
		if !isMeta {
			configMap[k] = v
		}
	}

	configJSON, err := json.Marshal(configMap)
	if err != nil {
		return nil, err
	}

	return &model.ScanTemplate{
		Name:        name,
		Description: description,
		Category:    category,
		Tags:        tags,
		Config:      string(configJSON),
		IsBuiltin:   true,
		SortNumber:  sortNumber,
	}, nil
}

// getDefaultTemplates 获取默认模板（当文件不存在时的后备方案）
func getDefaultTemplates() []model.ScanTemplate {
	quickScanConfig := map[string]interface{}{
		"name":     "",
		"target":   "",
		"template": nil,
		"domainscan": map[string]interface{}{
			"enable": false, "subfinder": true, "timeout": 300, "maxEnumerationTime": 10,
			"threads": 10, "rateLimit": 0, "removeWildcard": true, "resolveDNS": true,
			"concurrent": 50, "subdomainDictIds": []interface{}{}, "bruteforceTimeout": 30,
			"recursiveBrute": false, "recursiveDictIds": []interface{}{}, "wildcardDetect": false,
		},
		"portscan": map[string]interface{}{
			"enable": true, "tool": "naabu", "rate": 3000,
			"ports": "top100",
			"portThreshold": 100, "scanType": "s", "targetTimeout": 60, "probeTimeoutMs": 1000, "skipHostDiscovery": false,
			"excludeCDN": false, "excludeHosts": "", "workers": 50, "retries": 2,
			"warmUpTime": 1, "verify": false,
		},
		"portidentify": map[string]interface{}{
			"enable": false, "tool": "nmap", "timeout": 60, "concurrency": 10,
			"args": "-sV -version-intensity 5", "udp": false, "fastMode": false, "forceScan": false,
		},
		"fingerprint": map[string]interface{}{
			"enable": true, "tool": "httpx", "iconHash": true, "customEngine": true,
			"screenshot": true, "activeScan": false, "cert": true, "activeTimeout": 10,
			"targetTimeout": 30, "filterMode": "http_mapping", "forceScan": false,
		},
		"brutescan": map[string]interface{}{
			"enable": false, "services": []interface{}{}, "threads": 20, "timeout": 5,
			"delayMs": 100, "stopOnFirst": true, "forceScan": false,
		},
		"pocscan": map[string]interface{}{
			"enable": false, "mode": "auto", "useNuclei": true, "forceScan": false,
			"autoScan": true, "automaticScan": true, "customOnly": false,
			"severity": "critical,high,medium,low,info,unknown", "targetTimeout": 600,
			"nucleiTemplateIds": []interface{}{}, "customPocIds": []interface{}{},
			"customHeaders": []interface{}{}, "customPocOnly": false,
		},
		"dirscan": map[string]interface{}{
			"enable": false, "dictIds": []interface{}{}, "threads": 50, "timeout": 10,
			"followRedirect": true, "forceScan": false, "autoCalibration": true,
			"filterSize": "", "filterWords": "", "filterLines": "", "filterRegex": "",
			"matcherMode": "or", "filterMode": "or", "rate": 0,
			"recursion": false, "recursionDepth": 2,
		},
		"jsfinder": map[string]interface{}{
			"enable": false, "threads": 10, "timeout": 10, "forceScan": false,
		},
	}

	standardScanConfig := map[string]interface{}{
		"name":     "",
		"target":   "",
		"template": nil,
		"domainscan": map[string]interface{}{
			"enable": false, "subfinder": true, "timeout": 300, "maxEnumerationTime": 10,
			"threads": 10, "rateLimit": 0, "removeWildcard": true, "resolveDNS": true,
			"concurrent": 50, "subdomainDictIds": []interface{}{}, "bruteforceTimeout": 30,
			"recursiveBrute": false, "recursiveDictIds": []interface{}{}, "wildcardDetect": false,
		},
		"portscan": map[string]interface{}{
			"enable": true, "tool": "naabu", "rate": 3000,
			"ports": "top100",
			"portThreshold": 100, "scanType": "s", "targetTimeout": 60, "probeTimeoutMs": 1000, "skipHostDiscovery": false,
			"excludeCDN": false, "excludeHosts": "", "workers": 50, "retries": 2,
			"warmUpTime": 1, "verify": false,
		},
		"portidentify": map[string]interface{}{
			"enable": true, "tool": "nmap", "timeout": 60, "concurrency": 10,
			"args": "-sV -version-intensity 5", "udp": false, "fastMode": false, "forceScan": false,
		},
		"fingerprint": map[string]interface{}{
			"enable": true, "tool": "httpx", "iconHash": true, "customEngine": true,
			"screenshot": true, "activeScan": true, "cert": true, "activeTimeout": 10,
			"targetTimeout": 90, "filterMode": "http_mapping", "forceScan": false,
		},
		"brutescan": map[string]interface{}{
			"enable": true, "services": []interface{}{}, "threads": 20, "timeout": 5,
			"delayMs": 100, "stopOnFirst": true, "forceScan": false,
		},
		"pocscan": map[string]interface{}{
			"enable": true, "mode": "auto", "useNuclei": true, "forceScan": false,
			"autoScan": true, "automaticScan": true, "customOnly": false,
			"severity": "critical,high,medium,low,info,unknown", "targetTimeout": 600,
			"nucleiTemplateIds": []interface{}{}, "customPocIds": []interface{}{},
			"customHeaders": []interface{}{}, "customPocOnly": false,
		},
		"dirscan": map[string]interface{}{
			"enable": true, "dictIds": []interface{}{"69fd35207eaed2f49d40abec"},
			"threads": 50, "timeout": 10, "followRedirect": true, "forceScan": false,
			"autoCalibration": true, "filterSize": "", "filterWords": "",
			"filterLines": "", "filterRegex": "", "matcherMode": "or", "filterMode": "or",
			"rate": 0, "recursion": false, "recursionDepth": 2,
		},
		"jsfinder": map[string]interface{}{
			"enable": true, "threads": 10, "timeout": 10, "forceScan": false,
		},
	}

	return []model.ScanTemplate{
		{
			Name:        "快速扫描",
			Description: "仅进行端口扫描和基础指纹识别，适合快速资产发现",
			Category:    "quick",
			Tags:        []string{"快速", "端口扫描"},
			Config:      buildConfig(quickScanConfig),
			IsBuiltin:   true,
			SortNumber:  1,
		},
		{
			Name:        "标准扫描",
			Description: "端口扫描 + 指纹识别 + 弱口令检测 + 漏洞扫描 + 目录扫描 + JS敏感信息与未授权检测，适合日常安全检测",
			Category:    "standard",
			Tags:        []string{"标准", "漏洞扫描", "JS审计"},
			Config:      buildConfig(standardScanConfig),
			IsBuiltin:   true,
			SortNumber:  2,
		},
	}
}

func buildConfig(config map[string]interface{}) string {
	data, _ := json.Marshal(config)
	return string(data)
}
