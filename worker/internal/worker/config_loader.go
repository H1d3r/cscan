package worker

// 配置直连读取：Worker 通过既有 MongoDB 直连读取扫描配置（与结果直写同架构），
// 替代原 /api/v1/worker/config/* HTTP 拉取——该路径受 install key 认证约束，
// dev 模式密钥未配置或密钥轮换未同步时 401 会中断扫描阶段。
// 各加载器的过滤/关联语义与 API 侧对应处理器保持一致；API 端点保留用于兼容旧版 Worker。

import (
	"context"
	"fmt"
	"strings"

	"cscan/internal/model"

	"go.mongodb.org/mongo-driver/bson"
)

// ==================== 模板（Nuclei 模板 / 自定义 POC） ====================

// TemplatesReq 模板加载请求
type TemplatesReq struct {
	Tags              []string `json:"tags,omitempty"`
	Severities        []string `json:"severities,omitempty"`
	NucleiTemplateIds []string `json:"nucleiTemplateIds,omitempty"`
	CustomPocIds      []string `json:"customPocIds,omitempty"`
	CustomPocOnly     bool     `json:"customPocOnly,omitempty"`
}

// TemplateLoadOutcome is the stable classification for one template lookup.
type TemplateLoadOutcome string

const (
	TemplateLoadLoaded           TemplateLoadOutcome = "loaded"
	TemplateLoadNoMatch          TemplateLoadOutcome = "no_match"
	TemplateLoadFiltered         TemplateLoadOutcome = "filtered"
	TemplateLoadStoreUnavailable TemplateLoadOutcome = "store_unavailable"
	TemplateLoadDBError          TemplateLoadOutcome = "db_error"
	TemplateLoadInvalidContent   TemplateLoadOutcome = "invalid_content"
)

// TemplateLoadResult separates template data from lookup/validation diagnostics.
// Contents and FileRefs preserve the two existing Nuclei input forms. MissingIDs
// is bounded before the result leaves the loader.
type TemplateLoadResult struct {
	Contents   []string            `json:"contents,omitempty"`
	FileRefs   []string            `json:"fileRefs,omitempty"`
	Requested  int                 `json:"requested"`
	Loaded     int                 `json:"loaded"`
	Invalid    int                 `json:"invalid"`
	Source     string              `json:"source,omitempty"`
	Outcome    TemplateLoadOutcome `json:"outcome"`
	MissingIDs []string            `json:"missingIds,omitempty"`
	ReasonCode string              `json:"reasonCode,omitempty"`
}

// TemplatesResp 模板加载响应. LoadResult is optional so older callers and JSON
// consumers continue to see the original fields unchanged.
type TemplatesResp struct {
	Code       int                 `json:"code"`
	Msg        string              `json:"msg"`
	Success    bool                `json:"success"`
	Templates  []string            `json:"templates"`
	Count      int32               `json:"count"`
	LoadResult *TemplateLoadResult `json:"loadResult,omitempty"`
}

// loadTemplates preserves the historical response while exposing diagnostics to
// upgraded callers through LoadResult. Database errors are returned instead of
// being collapsed into an empty successful response.
func (w *Worker) loadTemplates(ctx context.Context, req *TemplatesReq) (*TemplatesResp, error) {
	result, err := w.loadTemplatesWithResult(ctx, req)
	if err != nil {
		return nil, err
	}
	return &TemplatesResp{
		Code:       0,
		Msg:        "success",
		Success:    true,
		Templates:  result.Contents,
		Count:      int32(result.Loaded),
		LoadResult: &result,
	}, nil
}

// loadTemplatesWithResult performs the Mongo lookup and validates every content
// before it can be passed to Nuclei.
func (w *Worker) loadTemplatesWithResult(ctx context.Context, req *TemplatesReq) (TemplateLoadResult, error) {
	result := TemplateLoadResult{Source: "mongo", Outcome: TemplateLoadNoMatch}
	if req == nil {
		result.ReasonCode = "template_request_missing"
		return result, nil
	}
	if w.mongoDB == nil {
		result.Outcome = TemplateLoadStoreUnavailable
		result.ReasonCode = "mongo_unavailable"
		return result, fmt.Errorf("mongo direct connection unavailable")
	}

	var contents []string
	filtered := 0
	matchedIDs := make(map[string]bool)

	fail := func(reason string, err error) (TemplateLoadResult, error) {
		result.Outcome = TemplateLoadDBError
		result.ReasonCode = reason
		return result, err
	}

	if len(req.NucleiTemplateIds) > 0 || len(req.CustomPocIds) > 0 {
		result.Requested = len(req.NucleiTemplateIds) + len(req.CustomPocIds)
		if len(req.NucleiTemplateIds) > 0 {
			docs, err := model.NewNucleiTemplateModel(w.mongoDB).FindByIds(ctx, req.NucleiTemplateIds)
			if err != nil {
				return fail("mongo_nuclei_query_failed", err)
			}
			for _, t := range docs {
				matchedIDs["n:"+t.TemplateId] = true
				if !t.Enabled {
					filtered++
					continue
				}
				contents = append(contents, t.Content)
			}
		}
		if len(req.CustomPocIds) > 0 {
			docs, err := model.NewCustomPocModel(w.mongoDB).FindByIds(ctx, req.CustomPocIds)
			if err != nil {
				return fail("mongo_custom_query_failed", err)
			}
			for _, p := range docs {
				matchedIDs["c:"+p.Id.Hex()] = true
				if !p.Enabled {
					filtered++
					continue
				}
				contents = append(contents, p.Content)
			}
		}
		for _, id := range req.NucleiTemplateIds {
			if !matchedIDs["n:"+id] {
				result.MissingIDs = appendBoundedMissingID(result.MissingIDs, id)
			}
		}
		for _, id := range req.CustomPocIds {
			if !matchedIDs["c:"+id] {
				result.MissingIDs = appendBoundedMissingID(result.MissingIDs, id)
			}
		}
	} else if req.CustomPocOnly {
		filter := bson.M{"enabled": true}
		if len(req.Severities) > 0 {
			filter["severity"] = bson.M{"$in": req.Severities}
		}
		pocs, err := model.NewCustomPocModel(w.mongoDB).FindWithFilter(ctx, filter, 0, 0)
		if err != nil {
			return fail("mongo_custom_query_failed", err)
		}
		result.Requested = len(pocs)
		for _, p := range pocs {
			contents = append(contents, p.Content)
		}
	} else {
		filter := bson.M{"enabled": true}
		if len(req.Tags) > 0 {
			filter["tags"] = bson.M{"$in": req.Tags}
		}
		if len(req.Severities) > 0 {
			filter["severity"] = bson.M{"$in": req.Severities}
		}
		docs, err := model.NewNucleiTemplateModel(w.mongoDB).FindEnabledByFilter(ctx, filter)
		if err != nil {
			return fail("mongo_nuclei_query_failed", err)
		}
		result.Requested += len(docs)
		for _, t := range docs {
			contents = append(contents, t.Content)
		}
		if len(req.Tags) > 0 {
			customFilter := bson.M{"enabled": true, "tags": bson.M{"$in": req.Tags}}
			if len(req.Severities) > 0 {
				customFilter["severity"] = bson.M{"$in": req.Severities}
			}
			pocs, err := model.NewCustomPocModel(w.mongoDB).FindWithFilter(ctx, customFilter, 0, 0)
			if err != nil {
				return fail("mongo_custom_query_failed", err)
			}
			result.Requested += len(pocs)
			for _, p := range pocs {
				contents = append(contents, p.Content)
			}
		}
	}

	result.Contents, result.Invalid = validateTemplateContents(contents)
	result.Loaded = len(result.Contents)
	classifyTemplateLoadResult(&result, filtered, false)
	return result, nil
}

// ==================== 被动指纹 ====================

// FingerprintDocument 指纹文档
type FingerprintDocument struct {
	Id            string            `json:"id"`
	Name          string            `json:"name"`
	Category      string            `json:"category"`
	Rule          string            `json:"rule"`
	Source        string            `json:"source"`
	Headers       map[string]string `json:"headers"`
	Cookies       map[string]string `json:"cookies"`
	Html          []string          `json:"html"`
	Scripts       []string          `json:"scripts"`
	ScriptSrc     []string          `json:"scriptSrc"`
	Meta          map[string]string `json:"meta"`
	Css           []string          `json:"css"`
	Url           []string          `json:"url"`
	ConflictGroup string            `json:"conflictGroup,omitempty"`
	Coexistence   []string          `json:"coexistence,omitempty"`
	ExclusiveWith []string          `json:"exclusiveWith,omitempty"`
	IsBuiltin     bool              `json:"isBuiltin"`
	Enabled       bool              `json:"enabled"`
}

// FingerprintsResp 指纹加载响应
type FingerprintsResp struct {
	Code         int                   `json:"code"`
	Msg          string                `json:"msg"`
	Success      bool                  `json:"success"`
	Fingerprints []FingerprintDocument `json:"fingerprints"`
	Count        int32                 `json:"count"`
}

// loadFingerprints 加载被动指纹（语义同 WorkerConfigFingerprintsHandler）
func (w *Worker) loadFingerprints(ctx context.Context, enabledOnly bool) (*FingerprintsResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}

	filter := bson.M{}
	if enabledOnly {
		filter["enabled"] = true
	}

	fps, err := model.NewFingerprintModel(w.mongoDB).Find(ctx, filter, 0, 0)
	if err != nil {
		return nil, err
	}

	fingerprints := make([]FingerprintDocument, 0, len(fps))
	for _, fp := range fps {
		fingerprints = append(fingerprints, FingerprintDocument{
			Id:            fp.Id.Hex(),
			Name:          fp.Name,
			Category:      fp.Category,
			Rule:          fp.Rule,
			Source:        fp.Source,
			Headers:       fp.Headers,
			Cookies:       fp.Cookies,
			Html:          fp.HTML,
			Scripts:       fp.Scripts,
			ScriptSrc:     fp.ScriptSrc,
			Meta:          fp.Meta,
			Css:           fp.CSS,
			Url:           fp.URL,
			ConflictGroup: fp.ConflictGroup,
			Coexistence:   fp.Coexistence,
			ExclusiveWith: fp.ExclusiveWith,
			IsBuiltin:     fp.IsBuiltin,
			Enabled:       fp.Enabled,
		})
	}

	return &FingerprintsResp{
		Code:         0,
		Msg:          "success",
		Success:      true,
		Fingerprints: fingerprints,
		Count:        int32(len(fingerprints)),
	}, nil
}

// ==================== 主动指纹 ====================

// ActiveFingerprintDocument 主动指纹文档
type ActiveFingerprintDocument struct {
	Id          string   `json:"id"`
	Name        string   `json:"name"`  // 应用名称（用于关联被动指纹）
	Paths       []string `json:"paths"` // 主动探测路径列表
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	// 关联的被动指纹规则（用于匹配响应）
	Rule      string            `json:"rule,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Cookies   map[string]string `json:"cookies,omitempty"`
	Html      []string          `json:"html,omitempty"`
	Scripts   []string          `json:"scripts,omitempty"`
	ScriptSrc []string          `json:"scriptSrc,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Css       []string          `json:"css,omitempty"`
	Url       []string          `json:"url,omitempty"`
}

// ActiveFingerprintsResp 主动指纹加载响应
type ActiveFingerprintsResp struct {
	Code         int                         `json:"code"`
	Msg          string                      `json:"msg"`
	Success      bool                        `json:"success"`
	Fingerprints []ActiveFingerprintDocument `json:"fingerprints"`
	Count        int32                       `json:"count"`
}

// loadActiveFingerprints 加载主动指纹并关联同名被动指纹规则
// （语义同 WorkerConfigActiveFingerprintsHandler：按小写名称关联，关联查询失败容忍）
func (w *Worker) loadActiveFingerprints(ctx context.Context, enabledOnly bool) (*ActiveFingerprintsResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}

	m := model.NewActiveFingerprintModel(w.mongoDB)
	var activeFps []model.ActiveFingerprint
	var err error
	if enabledOnly {
		activeFps, err = m.FindEnabled(ctx)
	} else {
		activeFps, err = m.FindAll(ctx)
	}
	if err != nil {
		return nil, err
	}

	// 批量获取关联的被动指纹规则（小写名称作为 key，支持不区分大小写匹配）
	names := make([]string, 0, len(activeFps))
	for _, fp := range activeFps {
		names = append(names, fp.Name)
	}
	passiveFpMap := make(map[string]*model.Fingerprint)
	if len(names) > 0 {
		if passiveFps, err := model.NewFingerprintModel(w.mongoDB).FindByNames(ctx, names); err != nil {
			// 不中断：主动指纹仍然返回，只是没有匹配规则（与服务端行为一致）
			w.logger.Warn("loadActiveFingerprints: FindByNames error: %v", err)
		} else {
			for i := range passiveFps {
				passiveFpMap[strings.ToLower(passiveFps[i].Name)] = passiveFps[i]
			}
		}
	}

	docs := make([]ActiveFingerprintDocument, 0, len(activeFps))
	for _, fp := range activeFps {
		doc := ActiveFingerprintDocument{
			Id:          fp.Id.Hex(),
			Name:        fp.Name,
			Paths:       fp.Paths,
			Description: fp.Description,
			Enabled:     fp.Enabled,
		}
		if passiveFp, ok := passiveFpMap[strings.ToLower(fp.Name)]; ok {
			doc.Rule = passiveFp.Rule
			doc.Headers = passiveFp.Headers
			doc.Cookies = passiveFp.Cookies
			doc.Html = passiveFp.HTML
			doc.Scripts = passiveFp.Scripts
			doc.ScriptSrc = passiveFp.ScriptSrc
			doc.Meta = passiveFp.Meta
			doc.Css = passiveFp.CSS
			doc.Url = passiveFp.URL
		}
		docs = append(docs, doc)
	}

	return &ActiveFingerprintsResp{
		Code:         0,
		Msg:          "success",
		Success:      true,
		Fingerprints: docs,
		Count:        int32(len(docs)),
	}, nil
}

// ==================== Subfinder 数据源 ====================

// SubfinderProvider Subfinder数据源
type SubfinderProvider struct {
	Id          string   `json:"id"`
	Provider    string   `json:"provider"`
	Keys        []string `json:"keys"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
}

// SubfinderResp Subfinder数据源加载响应
type SubfinderResp struct {
	Code      int                 `json:"code"`
	Msg       string              `json:"msg"`
	Success   bool                `json:"success"`
	Providers []SubfinderProvider `json:"providers"`
	Count     int32               `json:"count"`
}

// loadSubfinderProviders 加载启用的 Subfinder 数据源（语义同 WorkerConfigSubfinderHandler）
func (w *Worker) loadSubfinderProviders(ctx context.Context) (*SubfinderResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}

	providers, err := model.NewSubfinderProviderModel(w.mongoDB).FindEnabled(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]SubfinderProvider, 0, len(providers))
	for _, p := range providers {
		result = append(result, SubfinderProvider{
			Id:          p.Id.Hex(),
			Provider:    p.Provider,
			Keys:        p.Keys,
			Status:      p.Status,
			Description: p.Description,
		})
	}

	return &SubfinderResp{
		Code:      0,
		Msg:       "success",
		Success:   true,
		Providers: result,
		Count:     int32(len(result)),
	}, nil
}

// ==================== HTTP 服务设置 ====================

// HttpServiceMapping HTTP服务映射
type HttpServiceMapping struct {
	Id          string `json:"id"`
	ServiceName string `json:"serviceName"`
	IsHttp      bool   `json:"isHttp"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// HttpServiceConfig HTTP服务配置
type HttpServiceConfig struct {
	HttpPorts    []int  `json:"httpPorts"`
	HttpsPorts   []int  `json:"httpsPorts"`
	NonHttpPorts []int  `json:"nonHttpPorts"`
	Description  string `json:"description"`
}

// HttpServiceSettingsResp HTTP服务设置加载响应（端口配置 + 服务映射）
type HttpServiceSettingsResp struct {
	Code     int                  `json:"code"`
	Msg      string               `json:"msg"`
	Success  bool                 `json:"success"`
	Config   HttpServiceConfig    `json:"config"`
	Mappings []HttpServiceMapping `json:"mappings"`
}

// loadHttpServiceSettings 加载 HTTP 服务设置（语义同 WorkerConfigHttpServiceSettingsHandler：
// 端口配置读取失败回退内置默认端口，映射读取失败回退空列表）
func (w *Worker) loadHttpServiceSettings(ctx context.Context) (*HttpServiceSettingsResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}

	m := model.NewHttpServiceModel(w.mongoDB)

	config, err := m.GetConfig(ctx)
	if err != nil {
		w.logger.Error("loadHttpServiceSettings: GetConfig error: %v, using default ports", err)
		config = &model.HttpServiceConfig{
			HttpPorts:  []int{80, 8080, 8000, 8888, 8081, 8082, 8083, 8084, 8085, 8086, 8087, 8088, 8089, 8090, 9000, 9001, 9080, 3000, 3001, 5000, 5001, 8008, 8009, 8181, 8200, 8300, 8400, 8500, 8600, 8800, 8880, 8983, 9090, 9091, 9200, 9300, 10000},
			HttpsPorts: []int{443, 8443, 9443, 4443, 10443},
		}
	}

	mappingDocs, err := m.GetEnabledMappings(ctx)
	if err != nil {
		w.logger.Error("loadHttpServiceSettings: GetEnabledMappings error: %v", err)
		mappingDocs = []model.HttpServiceMapping{}
	}

	mappings := make([]HttpServiceMapping, 0, len(mappingDocs))
	for _, mm := range mappingDocs {
		mappings = append(mappings, HttpServiceMapping{
			Id:          mm.Id.Hex(),
			ServiceName: mm.ServiceName,
			IsHttp:      mm.IsHttp,
			Description: mm.Description,
			Enabled:     mm.Enabled,
		})
	}

	return &HttpServiceSettingsResp{
		Code:    0,
		Msg:     "success",
		Success: true,
		Config: HttpServiceConfig{
			HttpPorts:    config.HttpPorts,
			HttpsPorts:   config.HttpsPorts,
			NonHttpPorts: config.NonHttpPorts,
			Description:  config.Description,
		},
		Mappings: mappings,
	}, nil
}

// ==================== 单个 POC ====================

// PocByIdResp POC加载响应
type PocByIdResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Success  bool   `json:"success"`
	Content  string `json:"content"`
	Name     string `json:"name"`
	Severity string `json:"severity"`
	PocType  string `json:"pocType"`
}

// loadPocById 按 ID 加载单个 POC 内容（语义同 WorkerConfigPocHandler：
// custom 走自定义 POC；nuclei 先按 _id 再按 template_id 查询）
func (w *Worker) loadPocById(ctx context.Context, pocId, pocType string) (*PocByIdResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}
	if pocId == "" {
		return &PocByIdResp{Code: 400, Msg: "pocId不能为空"}, nil
	}

	if pocType == "custom" {
		poc, err := model.NewCustomPocModel(w.mongoDB).FindById(ctx, pocId)
		if err != nil {
			return nil, err
		}
		if poc == nil {
			return &PocByIdResp{Code: 404, Msg: "自定义POC不存在", Success: false}, nil
		}
		return &PocByIdResp{
			Code:     0,
			Msg:      "success",
			Success:  true,
			Content:  poc.Content,
			Name:     poc.Name,
			Severity: poc.Severity,
			PocType:  "custom",
		}, nil
	}

	// Nuclei 模板：先按 _id（FindByIds），失败或未命中再按 template_id 查询
	nucleiModel := model.NewNucleiTemplateModel(w.mongoDB)
	if templates, err := nucleiModel.FindByIds(ctx, []string{pocId}); err == nil && len(templates) > 0 {
		t := templates[0]
		return &PocByIdResp{
			Code:     0,
			Msg:      "success",
			Success:  true,
			Content:  t.Content,
			Name:     t.Name,
			Severity: t.Severity,
			PocType:  "nuclei",
		}, nil
	}

	template, err := nucleiModel.FindByTemplateId(ctx, pocId)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return &PocByIdResp{Code: 404, Msg: "Nuclei模板不存在", Success: false}, nil
	}

	return &PocByIdResp{
		Code:     0,
		Msg:      "success",
		Success:  true,
		Content:  template.Content,
		Name:     template.Name,
		Severity: template.Severity,
		PocType:  "nuclei",
	}, nil
}

// ==================== 目录扫描字典 ====================

// DirScanDictItem 目录扫描字典项
type DirScanDictItem struct {
	Id    string   `json:"id"`
	Name  string   `json:"name"`
	Paths []string `json:"paths"` // 解析后的路径列表
}

// DirScanDictResp 目录扫描字典加载响应
type DirScanDictResp struct {
	Code  int               `json:"code"`
	Msg   string            `json:"msg"`
	Dicts []DirScanDictItem `json:"dicts"`
	Count int               `json:"count"`
}

// loadDirScanDicts 按 ID 加载目录扫描字典并解析为路径列表（语义同 WorkerConfigDirScanDictHandler）
func (w *Worker) loadDirScanDicts(ctx context.Context, dictIds []string) (*DirScanDictResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}
	if len(dictIds) == 0 {
		return &DirScanDictResp{Code: 400, Msg: "dictIds不能为空"}, nil
	}

	dicts, err := model.NewDirScanDictModel(w.mongoDB).FindByIds(ctx, dictIds)
	if err != nil {
		return nil, err
	}

	items := make([]DirScanDictItem, 0, len(dicts))
	for _, d := range dicts {
		items = append(items, DirScanDictItem{
			Id:    d.Id.Hex(),
			Name:  d.Name,
			Paths: parseDictPaths(d.Content),
		})
	}

	return &DirScanDictResp{
		Code:  0,
		Msg:   "success",
		Dicts: items,
		Count: len(items),
	}, nil
}

// parseDictPaths 解析字典内容为路径列表（跳过空行和注释）
func parseDictPaths(content string) []string {
	var paths []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}

// ==================== 子域名字典 ====================

// SubdomainDictItem 子域名字典项
type SubdomainDictItem struct {
	Id      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"` // 字典内容（每行一个前缀）
}

// SubdomainDictResp 子域名字典加载响应
type SubdomainDictResp struct {
	Code  int                 `json:"code"`
	Msg   string              `json:"msg"`
	Dicts []SubdomainDictItem `json:"dicts"`
	Count int                 `json:"count"`
}

// loadSubdomainDicts 按 ID 加载子域名字典（语义同 WorkerConfigSubdomainDictHandler）
func (w *Worker) loadSubdomainDicts(ctx context.Context, dictIds []string) (*SubdomainDictResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}
	if len(dictIds) == 0 {
		return &SubdomainDictResp{Code: 400, Msg: "dictIds不能为空"}, nil
	}

	dicts, err := model.NewSubdomainDictModel(w.mongoDB).FindByIds(ctx, dictIds)
	if err != nil {
		return nil, err
	}

	items := make([]SubdomainDictItem, 0, len(dicts))
	for _, d := range dicts {
		items = append(items, SubdomainDictItem{
			Id:      d.Id.Hex(),
			Name:    d.Name,
			Content: d.Content,
		})
	}

	return &SubdomainDictResp{
		Code:  0,
		Msg:   "success",
		Dicts: items,
		Count: len(items),
	}, nil
}

// ==================== 弱口令字典 ====================

// WeakpassDictItem 弱口令字典项
type WeakpassDictItem struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	Service   string `json:"service"`   // 服务类型
	DictType  string `json:"dictType"`  // 字典类型：username 或 password
	Content   string `json:"content"`   // 字典内容（每行一个）
	WordCount int    `json:"wordCount"` // 词条数量
}

// WeakpassDictResp 弱口令字典加载响应
type WeakpassDictResp struct {
	Code  int                `json:"code"`
	Msg   string             `json:"msg"`
	Dicts []WeakpassDictItem `json:"dicts"`
	Count int                `json:"count"`
}

// loadWeakpassDicts 加载弱口令字典（语义同 WorkerConfigWeakpassDictHandler）：
// 优先按字典 ID；否则按服务过滤（匹配服务 + common 通用字典）；都未指定时返回全部启用字典。
func (w *Worker) loadWeakpassDicts(ctx context.Context, dictIds []string, services []string) (*WeakpassDictResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}

	dictModel := model.NewWeakpassDictModel(w.mongoDB)
	var dicts []model.WeakpassDict
	var err error

	if len(dictIds) > 0 {
		dicts, err = dictModel.FindByIds(ctx, dictIds)
	} else if len(services) > 0 {
		var allDicts []model.WeakpassDict
		allDicts, err = dictModel.FindEnabled(ctx, "")
		if err == nil {
			serviceSet := make(map[string]bool, len(services))
			for _, svc := range services {
				serviceSet[svc] = true
			}
			for _, d := range allDicts {
				if d.Service == "common" || serviceSet[d.Service] {
					dicts = append(dicts, d)
				}
			}
		}
	} else {
		dicts, err = dictModel.FindEnabled(ctx, "")
	}
	if err != nil {
		return nil, err
	}

	items := make([]WeakpassDictItem, 0, len(dicts))
	for _, d := range dicts {
		items = append(items, WeakpassDictItem{
			Id:        d.Id.Hex(),
			Name:      d.Name,
			Service:   d.Service,
			Content:   d.Content,
			WordCount: d.WordCount,
		})
	}

	return &WeakpassDictResp{
		Code:  0,
		Msg:   "success",
		Dicts: items,
		Count: len(items),
	}, nil
}

// ==================== 黑名单 ====================

// BlacklistRulesResp 黑名单规则加载响应
type BlacklistRulesResp struct {
	Code  int      `json:"code"`
	Msg   string   `json:"msg"`
	Rules []string `json:"rules"`
}

// loadBlacklistRules 加载启用的黑名单规则（语义同 GetBlacklistRules：禁用或无规则返回空列表）
func (w *Worker) loadBlacklistRules(ctx context.Context) (*BlacklistRulesResp, error) {
	if w.mongoDB == nil {
		return nil, fmt.Errorf("mongo direct connection unavailable")
	}

	rules, err := model.NewBlacklistConfigModel(w.mongoDB).GetRules(ctx)
	if err != nil {
		return &BlacklistRulesResp{Code: -1, Msg: "获取黑名单规则失败"}, nil
	}

	return &BlacklistRulesResp{Code: 0, Msg: "success", Rules: rules}, nil
}
