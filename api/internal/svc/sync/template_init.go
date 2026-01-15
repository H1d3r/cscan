package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"cscan/model"

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

// loadTemplatesFromFiles 从 poc/custom-scanTemplate 目录加载模板
func loadTemplatesFromFiles() []model.ScanTemplate {
	var templates []model.ScanTemplate

	// 尝试多个可能的路径
	possiblePaths := []string{
		"poc/custom-scanTemplate",
		"../poc/custom-scanTemplate",
		"../../poc/custom-scanTemplate",
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
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(templateDir, name)
		t, err := loadTemplateFromFile(filePath)
		if err != nil {
			logx.Errorf("[TemplateInit] Failed to load template %s: %v", name, err)
			continue
		}

		templates = append(templates, *t)
		logx.Infof("[TemplateInit] Loaded template from file: %s", name)
	}

	return templates
}

// loadTemplateFromFile 从单个文件加载模板
func loadTemplateFromFile(filePath string) (*model.ScanTemplate, error) {
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

// getDefaultTemplates 获取默认模板（当文件不存在时的后备方案）
func getDefaultTemplates() []model.ScanTemplate {
	return []model.ScanTemplate{
		{
			Name:        "快速扫描",
			Description: "仅进行端口扫描和基础指纹识别，适合快速资产发现",
			Category:    "quick",
			Tags:        []string{"快速", "端口扫描"},
			Config:      buildConfig(map[string]interface{}{"portscan": map[string]interface{}{"enable": true, "ports": "21,22,23,25,53,80,443,3306,3389,8080", "rate": 1000, "timeout": 3}, "fingerprint": map[string]interface{}{"enable": true}, "pocscan": map[string]interface{}{"enable": false}, "dirscan": map[string]interface{}{"enable": false}, "domainscan": map[string]interface{}{"enable": false}}),
			IsBuiltin:   true,
			SortNumber:  1,
		},
		{
			Name:        "标准扫描",
			Description: "端口扫描 + 指纹识别 + 漏洞扫描，适合日常安全检测",
			Category:    "standard",
			Tags:        []string{"标准", "漏洞扫描"},
			Config:      buildConfig(map[string]interface{}{"portscan": map[string]interface{}{"enable": true, "ports": "21,22,23,25,53,80,443,1433,3306,3389,5432,6379,8080,27017", "rate": 500, "timeout": 5}, "fingerprint": map[string]interface{}{"enable": true}, "pocscan": map[string]interface{}{"enable": true, "severity": "critical,high,medium"}, "dirscan": map[string]interface{}{"enable": false}, "domainscan": map[string]interface{}{"enable": false}}),
			IsBuiltin:   true,
			SortNumber:  2,
		},
	}
}

func buildConfig(config map[string]interface{}) string {
	data, _ := json.Marshal(config)
	return string(data)
}
