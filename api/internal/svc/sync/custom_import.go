package sync

import (
	"context"
	"encoding/json"
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
	nucleiSync           *NucleiSyncService
	fingerprintSync      *FingerprintSyncService
	customImport         *CustomImportService
	dirscanDictModel     *model.DirScanDictModel
	subdomainDictModel   *model.SubdomainDictModel
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
