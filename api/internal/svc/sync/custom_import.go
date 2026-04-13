package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cscan/model"

	"github.com/zeromicro/go-zero/core/logx"
	"gopkg.in/yaml.v3"
)

// CustomImportService 自定义POC和指纹导入服务
type CustomImportService struct {
	fingerprintModel       *model.FingerprintModel
	customPocModel         *model.CustomPocModel
	activeFingerprintModel *model.ActiveFingerprintModel
}

// NewCustomImportService 创建导入服务
func NewCustomImportService(fpModel *model.FingerprintModel, pocModel *model.CustomPocModel, afpModel *model.ActiveFingerprintModel) *CustomImportService {
	return &CustomImportService{
		fingerprintModel:       fpModel,
		customPocModel:         pocModel,
		activeFingerprintModel: afpModel,
	}
}

// ImportAll 导入所有自定义POC和指纹
func (s *CustomImportService) ImportAll(ctx context.Context) {
	time.Sleep(2 * time.Second)

	pocBaseDir := "/app/poc"
	if _, err := os.Stat(pocBaseDir); os.IsNotExist(err) {
		pocBaseDir = "poc"
	}
	if _, err := os.Stat(pocBaseDir); os.IsNotExist(err) {
		logx.Info("[CustomImport] POC directory not found, skipping import")
		return
	}

	s.importCustomFingerprints(ctx, pocBaseDir)
	s.importActiveFingerprints(ctx, pocBaseDir)
	s.importCustomPocs(ctx, pocBaseDir)
}

// importCustomFingerprints 导入自定义指纹
func (s *CustomImportService) importCustomFingerprints(ctx context.Context, baseDir string) {
	fingerDir := filepath.Join(baseDir, "custom-finger")
	if _, err := os.Stat(fingerDir); os.IsNotExist(err) {
		logx.Info("[CustomImport] custom-finger directory not found, skipping")
		return
	}

	startTime := time.Now()
	totalImported := 0
	totalSkipped := 0

	logx.Infof("[CustomImport] Starting import fingerprints from: %s", fingerDir)

	err := filepath.Walk(fingerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" && ext != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		fingerprints, skipped := parseARLFingerprints(string(data))
		totalSkipped += skipped

		for _, fp := range fingerprints {
			doc := &model.Fingerprint{
				Name:      fp.Name,
				Rule:      fp.Rule,
				Source:    "custom",
				IsBuiltin: false,
				Enabled:   true,
			}
			if err := s.fingerprintModel.Upsert(ctx, doc); err != nil {
				totalSkipped++
			} else {
				totalImported++
			}
		}

		return nil
	})

	if err != nil {
		logx.Errorf("[CustomImport] Walk fingerprints error: %v", err)
	}

	duration := time.Since(startTime)
	logx.Infof("[CustomImport] Fingerprints import completed: %d imported, %d skipped in %v", totalImported, totalSkipped, duration)
}

// importActiveFingerprints 导入主动指纹路径配置
func (s *CustomImportService) importActiveFingerprints(ctx context.Context, baseDir string) {
	if s.activeFingerprintModel == nil {
		logx.Info("[CustomImport] ActiveFingerprintModel is nil, skipping active fingerprints import")
		return
	}

	fingerDir := filepath.Join(baseDir, "custom-finger")
	if _, err := os.Stat(fingerDir); os.IsNotExist(err) {
		logx.Info("[CustomImport] custom-finger directory not found, skipping active fingerprints")
		return
	}

	// 查找 active-paths.yaml 或 dir.yaml
	activePathsFiles := []string{
		filepath.Join(fingerDir, "active-paths.yaml"),
		filepath.Join(fingerDir, "active-paths.yml"),
		filepath.Join(fingerDir, "dir.yaml"),
		filepath.Join(fingerDir, "dir.yml"),
	}

	var activePathsFile string
	for _, f := range activePathsFiles {
		if _, err := os.Stat(f); err == nil {
			activePathsFile = f
			break
		}
	}

	if activePathsFile == "" {
		logx.Debug("[CustomImport] No active-paths.yaml or dir.yaml found, skipping active fingerprints")
		return
	}

	startTime := time.Now()
	totalImported := 0
	totalSkipped := 0

	logx.Infof("[CustomImport] Starting import active fingerprints from: %s", activePathsFile)

	data, err := os.ReadFile(activePathsFile)
	if err != nil {
		logx.Errorf("[CustomImport] Read active paths file error: %v", err)
		return
	}

	// 解析 YAML 格式: map[应用名称][]路径
	var activePaths map[string][]string
	if err := yaml.Unmarshal(data, &activePaths); err != nil {
		logx.Errorf("[CustomImport] Parse active paths YAML error: %v", err)
		return
	}

	for name, paths := range activePaths {
		if name == "" || len(paths) == 0 {
			totalSkipped++
			continue
		}

		doc := &model.ActiveFingerprint{
			Name:    name,
			Paths:   paths,
			Enabled: true,
		}

		if err := s.activeFingerprintModel.Upsert(ctx, doc); err != nil {
			logx.Debugf("[CustomImport] Upsert active fingerprint '%s' error: %v", name, err)
			totalSkipped++
		} else {
			totalImported++
		}
	}

	duration := time.Since(startTime)
	logx.Infof("[CustomImport] Active fingerprints import completed: %d imported, %d skipped in %v", totalImported, totalSkipped, duration)
}

// importCustomPocs 导入自定义POC
func (s *CustomImportService) importCustomPocs(ctx context.Context, baseDir string) {
	pocDir := filepath.Join(baseDir, "custom-pocs")
	if _, err := os.Stat(pocDir); os.IsNotExist(err) {
		logx.Info("[CustomImport] custom-pocs directory not found, skipping")
		return
	}

	startTime := time.Now()
	totalImported := 0
	totalSkipped := 0

	logx.Infof("[CustomImport] Starting import POCs from: %s", pocDir)

	err := filepath.Walk(pocDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		poc := parseNucleiPoc(string(data))
		if poc == nil {
			totalSkipped++
			return nil
		}

		existing, _ := s.customPocModel.FindByTemplateId(ctx, poc.TemplateId)
		if existing != nil {
			totalSkipped++
			return nil
		}

		if err := s.customPocModel.Insert(ctx, poc); err != nil {
			totalSkipped++
		} else {
			totalImported++
		}

		return nil
	})

	if err != nil {
		logx.Errorf("[CustomImport] Walk POCs error: %v", err)
	}

	duration := time.Since(startTime)
	logx.Infof("[CustomImport] POCs import completed: %d imported, %d skipped in %v", totalImported, totalSkipped, duration)
}

// ARLFingerprintItem ARL格式指纹项
type ARLFingerprintItem struct {
	Name string `yaml:"name" json:"name"`
	Rule string `yaml:"rule" json:"rule"`
}

// parseARLFingerprints 解析ARL格式指纹
func parseARLFingerprints(content string) ([]ARLFingerprintItem, int) {
	var fingerprints []ARLFingerprintItem
	var skipped int

	if err := yaml.Unmarshal([]byte(content), &fingerprints); err != nil {
		if err := json.Unmarshal([]byte(content), &fingerprints); err != nil {
			return nil, 1
		}
	}

	var valid []ARLFingerprintItem
	for _, fp := range fingerprints {
		if fp.Name == "" || fp.Rule == "" {
			skipped++
			continue
		}
		valid = append(valid, fp)
	}

	return valid, skipped
}

// NucleiPocYAML 用于解析Nuclei POC的结构
type NucleiPocYAML struct {
	Id   string `yaml:"id"`
	Info struct {
		Name        string `yaml:"name"`
		Author      any    `yaml:"author"`
		Severity    string `yaml:"severity"`
		Description string `yaml:"description"`
		Tags        string `yaml:"tags"`
	} `yaml:"info"`
}

// parseNucleiPoc 解析Nuclei POC文件
func parseNucleiPoc(content string) *model.CustomPoc {
	var info NucleiPocYAML
	if err := yaml.Unmarshal([]byte(content), &info); err != nil {
		return nil
	}

	if info.Id == "" {
		return nil
	}

	author := ""
	switch v := info.Info.Author.(type) {
	case string:
		author = v
	case []interface{}:
		var authors []string
		for _, a := range v {
			if s, ok := a.(string); ok {
				authors = append(authors, s)
			}
		}
		author = strings.Join(authors, ", ")
	}

	var tags []string
	if info.Info.Tags != "" {
		for _, tag := range strings.Split(info.Info.Tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}

	severity := strings.ToLower(info.Info.Severity)
	if severity == "" {
		severity = "info"
	}

	return &model.CustomPoc{
		TemplateId:  info.Id,
		Name:        info.Info.Name,
		Author:      author,
		Severity:    severity,
		Description: info.Info.Description,
		Tags:        tags,
		Content:     content,
		Enabled:     true,
	}
}

// truncateError 截断错误信息
func truncateError(err error, maxLen int) string {
	if err == nil {
		return ""
	}
	errStr := err.Error()
	if len(errStr) > maxLen {
		return errStr[:maxLen] + "..."
	}
	return errStr
}

// 为了兼容性，在ServiceContext中添加这些方法的包装
type SyncMethods struct {
	nucleiSync         *NucleiSyncService
	fingerprintSync    *FingerprintSyncService
	customImport       *CustomImportService
	dirscanDictModel   *model.DirScanDictModel
	subdomainDictModel *model.SubdomainDictModel
	httpServiceModel   *model.HttpServiceModel
	blacklistModel     *model.BlacklistConfigModel
}

func NewSyncMethods(nucleiModel *model.NucleiTemplateModel, fpModel *model.FingerprintModel, pocModel *model.CustomPocModel, afpModel *model.ActiveFingerprintModel, dirscanDictModel *model.DirScanDictModel, subdomainDictModel *model.SubdomainDictModel) *SyncMethods {
	return &SyncMethods{
		nucleiSync:         NewNucleiSyncService(nucleiModel),
		fingerprintSync:    NewFingerprintSyncService(fpModel),
		customImport:       NewCustomImportService(fpModel, pocModel, afpModel),
		dirscanDictModel:   dirscanDictModel,
		subdomainDictModel: subdomainDictModel,
	}
}

// SetHttpServiceModel 设置HTTP服务模型（用于启动时导入）
func (s *SyncMethods) SetHttpServiceModel(model *model.HttpServiceModel) {
	s.httpServiceModel = model
}

// SetBlacklistModel 设置黑名单模型（用于启动时导入）
func (s *SyncMethods) SetBlacklistModel(model *model.BlacklistConfigModel) {
	s.blacklistModel = model
}

func (s *SyncMethods) SyncNucleiTemplates() {
	s.nucleiSync.SyncTemplates(context.Background())
}

func (s *SyncMethods) SyncWappalyzerFingerprints() {
	s.fingerprintSync.SyncWappalyzerFingerprints(context.Background())
}

func (s *SyncMethods) ImportCustomPocAndFingerprints() {
	s.customImport.ImportAll(context.Background())
	// 初始化内置目录扫描字典
	s.initBuiltinDirScanDicts(context.Background())
	// 初始化内置子域名字典
	s.initBuiltinSubdomainDicts(context.Background())
	// 导入HTTP服务映射配置
	s.importHttpServiceMappings(context.Background())
	// 导入默认黑名单规则
	s.initBuiltinBlacklist(context.Background())
}

func (s *SyncMethods) RefreshTemplateCache() {
	s.nucleiSync.RefreshCache(context.Background())
}

func (s *SyncMethods) GetCategories() []string {
	return s.nucleiSync.GetCategories()
}

func (s *SyncMethods) GetStats() map[string]int {
	return s.nucleiSync.GetStats()
}

// initBuiltinDirScanDicts 初始化内置目录扫描字典
func (s *SyncMethods) initBuiltinDirScanDicts(ctx context.Context) {
	if s.dirscanDictModel == nil {
		return
	}

	// 确定字典目录路径
	dictDir := "/app/poc/custom-url"
	if _, err := os.Stat(dictDir); os.IsNotExist(err) {
		dictDir = "poc/custom-url"
	}
	if _, err := os.Stat(dictDir); os.IsNotExist(err) {
		logx.Info("[SyncMethods] custom-url directory not found, skipping builtin dicts")
		return
	}

	logx.Infof("[SyncMethods] Initializing builtin dirscan dicts from: %s", dictDir)

	totalImported := 0
	totalSkipped := 0

	err := filepath.Walk(dictDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// 读取文件内容
		data, err := os.ReadFile(path)
		if err != nil {
			logx.Errorf("[SyncMethods] Failed to read dict file %s: %v", path, err)
			totalSkipped++
			return nil
		}

		content := string(data)
		if strings.TrimSpace(content) == "" {
			totalSkipped++
			return nil
		}

		// 使用文件名（不含扩展名）作为字典名称
		fileName := filepath.Base(path)
		dictName := strings.TrimSuffix(fileName, filepath.Ext(fileName))

		dict := &model.DirScanDict{
			Name:        dictName,
			Description: "系统内置字典",
			Content:     content,
			PathCount:   countLines(content),
			Enabled:     true,
			IsBuiltin:   true,
		}

		if err := s.dirscanDictModel.UpsertByName(ctx, dict); err != nil {
			logx.Errorf("[SyncMethods] Failed to upsert builtin dict %s: %v", dictName, err)
			totalSkipped++
		} else {
			totalImported++
		}

		return nil
	})

	if err != nil {
		logx.Errorf("[SyncMethods] Walk dict directory error: %v", err)
	}

	logx.Infof("[SyncMethods] Builtin dirscan dicts initialized: %d imported, %d skipped", totalImported, totalSkipped)
}

// countLines 计算非空非注释行数
func countLines(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}
	return count
}

// initBuiltinSubdomainDicts 初始化内置子域名字典
func (s *SyncMethods) initBuiltinSubdomainDicts(ctx context.Context) {
	if s.subdomainDictModel == nil {
		return
	}

	// 确定字典目录路径
	dictDir := "/app/poc/custom-subname"
	if _, err := os.Stat(dictDir); os.IsNotExist(err) {
		dictDir = "poc/custom-subname"
	}
	if _, err := os.Stat(dictDir); os.IsNotExist(err) {
		logx.Info("[SyncMethods] custom-subname directory not found, skipping builtin subdomain dicts")
		return
	}

	logx.Infof("[SyncMethods] Initializing builtin subdomain dicts from: %s", dictDir)

	totalImported := 0
	totalSkipped := 0

	err := filepath.Walk(dictDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// 只处理 .txt 文件
		if !strings.HasSuffix(strings.ToLower(path), ".txt") {
			return nil
		}

		// 使用文件名（不含扩展名）作为字典名称
		fileName := filepath.Base(path)
		dictName := strings.TrimSuffix(fileName, filepath.Ext(fileName))

		// 检查是否已存在同名字典，如果存在则跳过
		existing, _ := s.subdomainDictModel.FindByName(ctx, dictName)
		if existing != nil && existing.Name != "" {
			logx.Debugf("[SyncMethods] Subdomain dict '%s' already exists, skipping", dictName)
			totalSkipped++
			return nil
		}

		// 读取文件内容
		data, err := os.ReadFile(path)
		if err != nil {
			logx.Errorf("[SyncMethods] Failed to read subdomain dict file %s: %v", path, err)
			totalSkipped++
			return nil
		}

		content := string(data)
		if strings.TrimSpace(content) == "" {
			totalSkipped++
			return nil
		}

		dict := &model.SubdomainDict{
			Name:        dictName,
			Description: "系统内置子域名字典",
			Content:     content,
			WordCount:   countLines(content),
			Enabled:     true,
			IsBuiltin:   true,
		}

		if err := s.subdomainDictModel.Insert(ctx, dict); err != nil {
			logx.Errorf("[SyncMethods] Failed to insert builtin subdomain dict %s: %v", dictName, err)
			totalSkipped++
		} else {
			totalImported++
		}

		return nil
	})

	if err != nil {
		logx.Errorf("[SyncMethods] Walk subdomain dict directory error: %v", err)
	}

	logx.Infof("[SyncMethods] Builtin subdomain dicts initialized: %d imported, %d skipped", totalImported, totalSkipped)
}

// importHttpServiceMappings 从 poc/custom-http 目录导入HTTP服务映射配置
func (s *SyncMethods) importHttpServiceMappings(ctx context.Context) {
	if s.httpServiceModel == nil {
		logx.Info("[SyncMethods] HttpServiceModel is nil, skipping HTTP service mappings import")
		return
	}

	// 确定配置目录路径
	configDir := "/app/poc/custom-http"
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		configDir = "poc/custom-http"
	}
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		logx.Info("[SyncMethods] custom-http directory not found, skipping HTTP service mappings import")
		return
	}

	logx.Infof("[SyncMethods] Importing HTTP service mappings from: %s", configDir)

	totalImported := 0
	totalSkipped := 0

	err := filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// 只处理 .txt 文件
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".txt" {
			return nil
		}

		// 读取文件内容
		data, err := os.ReadFile(path)
		if err != nil {
			logx.Errorf("[SyncMethods] Failed to read HTTP service config file %s: %v", path, err)
			return nil
		}

		content := string(data)
		if strings.TrimSpace(content) == "" {
			return nil
		}

		// 解析并导入
		imported, skipped := s.parseAndImportHttpServiceConfig(ctx, content)
		totalImported += imported
		totalSkipped += skipped

		return nil
	})

	if err != nil {
		logx.Errorf("[SyncMethods] Walk HTTP service config directory error: %v", err)
	}

	logx.Infof("[SyncMethods] HTTP service mappings import completed: %d imported, %d skipped", totalImported, totalSkipped)
}

// parseAndImportHttpServiceConfig 解析并导入HTTP服务映射配置
func (s *SyncMethods) parseAndImportHttpServiceConfig(ctx context.Context, content string) (imported, skipped int) {
	lines := strings.Split(content, "\n")

	var httpPorts, httpsPorts, nonHttpPorts []int
	currentSection := ""

	// 获取现有的服务映射，用于去重
	existingMappings, _ := s.httpServiceModel.GetMappings(ctx)
	existingServiceNames := make(map[string]bool)
	for _, m := range existingMappings {
		existingServiceNames[strings.ToLower(m.ServiceName)] = true
	}

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
			if port := parsePortLine(line); port > 0 {
				httpPorts = append(httpPorts, port)
			}
		case "https_ports":
			if port := parsePortLine(line); port > 0 {
				httpsPorts = append(httpsPorts, port)
			}
		case "non_http_ports":
			if port := parsePortLine(line); port > 0 {
				nonHttpPorts = append(nonHttpPorts, port)
			}
		case "service_mapping":
			// 解析服务映射: serviceName=http/non_http [disabled] # description
			mapping := parseServiceMappingLine(line)
			if mapping.ServiceName == "" {
				continue
			}

			// 检查是否已存在（去重）
			if existingServiceNames[strings.ToLower(mapping.ServiceName)] {
				skipped++
				continue
			}

			doc := &model.HttpServiceMapping{
				ServiceName: strings.ToLower(mapping.ServiceName),
				IsHttp:      mapping.IsHttp,
				Description: mapping.Description,
				Enabled:     mapping.Enabled,
			}
			if err := s.httpServiceModel.SaveMapping(ctx, doc); err != nil {
				logx.Debugf("[SyncMethods] Save HTTP service mapping '%s' error: %v", mapping.ServiceName, err)
				skipped++
			} else {
				imported++
				existingServiceNames[strings.ToLower(mapping.ServiceName)] = true
			}
		}
	}

	// 保存端口配置（如果有新的端口，合并到现有配置）
	if len(httpPorts) > 0 || len(httpsPorts) > 0 || len(nonHttpPorts) > 0 {
		existingConfig, _ := s.httpServiceModel.GetConfig(ctx)
		if existingConfig != nil {
			// 合并端口（去重）
			httpPorts = mergeUniquePorts(existingConfig.HttpPorts, httpPorts)
			httpsPorts = mergeUniquePorts(existingConfig.HttpsPorts, httpsPorts)
			nonHttpPorts = mergeUniquePorts(existingConfig.NonHttpPorts, nonHttpPorts)
		}

		config := &model.HttpServiceConfig{
			HttpPorts:    httpPorts,
			HttpsPorts:   httpsPorts,
			NonHttpPorts: nonHttpPorts,
		}
		if err := s.httpServiceModel.SaveConfig(ctx, config); err != nil {
			logx.Errorf("[SyncMethods] Save HTTP service config error: %v", err)
		}
	}

	return imported, skipped
}

// parsePortLine 解析端口行
func parsePortLine(line string) int {
	// 去除注释
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	var port int
	_, err := fmt.Sscanf(line, "%d", &port)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

// parseServiceMappingLine 解析服务映射行
func parseServiceMappingLine(line string) struct {
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

// mergeUniquePorts 合并端口列表并去重
func mergeUniquePorts(existing, newPorts []int) []int {
	portSet := make(map[int]bool)
	for _, p := range existing {
		portSet[p] = true
	}
	for _, p := range newPorts {
		portSet[p] = true
	}
	result := make([]int, 0, len(portSet))
	for p := range portSet {
		result = append(result, p)
	}
	return result
}

// initBuiltinBlacklist 初始化内置黑名单规则
// 从 poc/custom-blacklist 目录读取默认黑名单，合并到现有黑名单（不重复导入）
func (s *SyncMethods) initBuiltinBlacklist(ctx context.Context) {
	if s.blacklistModel == nil {
		logx.Info("[SyncMethods] BlacklistModel is nil, skipping builtin blacklist import")
		return
	}

	// 确定黑名单目录路径
	blacklistDir := "/app/poc/custom-blacklist"
	if _, err := os.Stat(blacklistDir); os.IsNotExist(err) {
		blacklistDir = "poc/custom-blacklist"
	}
	if _, err := os.Stat(blacklistDir); os.IsNotExist(err) {
		logx.Info("[SyncMethods] custom-blacklist directory not found, skipping builtin blacklist import")
		return
	}

	logx.Infof("[SyncMethods] Importing builtin blacklist from: %s", blacklistDir)

	// 获取现有黑名单规则
	existingConfig, err := s.blacklistModel.Get(ctx)
	if err != nil {
		logx.Errorf("[SyncMethods] Failed to get existing blacklist: %v", err)
		return
	}

	// 解析现有规则到集合（用于去重）
	existingRules := make(map[string]bool)
	if existingConfig != nil && existingConfig.Rules != "" {
		for _, rule := range model.ParseBlacklistRules(existingConfig.Rules) {
			existingRules[strings.ToLower(rule)] = true
		}
	}

	// 收集新规则
	var newRules []string
	totalImported := 0
	totalSkipped := 0

	err = filepath.Walk(blacklistDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// 只处理 .txt 文件
		if !strings.HasSuffix(strings.ToLower(path), ".txt") {
			return nil
		}

		// 读取文件内容
		data, err := os.ReadFile(path)
		if err != nil {
			logx.Errorf("[SyncMethods] Failed to read blacklist file %s: %v", path, err)
			return nil
		}

		content := string(data)
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)
			// 跳过空行和注释
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			// 检查是否已存在（去重，忽略大小写）
			ruleLower := strings.ToLower(line)
			if existingRules[ruleLower] {
				totalSkipped++
				continue
			}

			// 添加新规则
			newRules = append(newRules, line)
			existingRules[ruleLower] = true
			totalImported++
		}

		return nil
	})

	if err != nil {
		logx.Errorf("[SyncMethods] Walk blacklist directory error: %v", err)
	}

	// 如果有新规则，合并并保存
	if len(newRules) > 0 {
		// 合并规则
		var allRules string
		if existingConfig != nil && existingConfig.Rules != "" {
			allRules = existingConfig.Rules + "\n" + strings.Join(newRules, "\n")
		} else {
			allRules = strings.Join(newRules, "\n")
		}

		// 保存更新后的黑名单
		config := &model.BlacklistConfig{
			Rules:  allRules,
			Status: "enable",
		}
		if existingConfig != nil && !existingConfig.Id.IsZero() {
			config.Id = existingConfig.Id
		}

		if err := s.blacklistModel.Save(ctx, config); err != nil {
			logx.Errorf("[SyncMethods] Failed to save blacklist: %v", err)
		} else {
			logx.Infof("[SyncMethods] Builtin blacklist imported: %d new rules, %d skipped (already exist)", totalImported, totalSkipped)
		}
	} else {
		logx.Infof("[SyncMethods] Builtin blacklist: no new rules to import (%d already exist)", totalSkipped)
	}
}
