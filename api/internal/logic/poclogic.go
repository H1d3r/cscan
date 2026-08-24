package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"
	"cscan/pkg/template"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"gopkg.in/yaml.v3"
)

// ==================== 标签映射 ====================

type TagMappingListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTagMappingListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TagMappingListLogic {
	return &TagMappingListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TagMappingListLogic) TagMappingList() (resp *types.TagMappingListResp, err error) {
	docs, err := l.svcCtx.TagMappingModel.FindAll(l.ctx)
	if err != nil {
		return &types.TagMappingListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.TagMapping, 0, len(docs))
	for _, doc := range docs {
		list = append(list, types.TagMapping{
			Id:          doc.Id.Hex(),
			AppName:     doc.AppName,
			NucleiTags:  doc.NucleiTags,
			Description: doc.Description,
			Enabled:     doc.Enabled,
			CreateTime:  doc.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.TagMappingListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

type TagMappingSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTagMappingSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TagMappingSaveLogic {
	return &TagMappingSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TagMappingSaveLogic) TagMappingSave(req *types.TagMappingSaveReq) (resp *types.BaseResp, err error) {
	doc := &model.TagMapping{
		AppName:     req.AppName,
		NucleiTags:  req.NucleiTags,
		Description: req.Description,
		Enabled:     req.Enabled,
	}

	if req.Id != "" {
		err = l.svcCtx.TagMappingModel.Update(l.ctx, req.Id, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新失败"}, nil
		}
	} else {
		err = l.svcCtx.TagMappingModel.Insert(l.ctx, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "创建失败"}, nil
		}
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

type TagMappingDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTagMappingDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TagMappingDeleteLogic {
	return &TagMappingDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TagMappingDeleteLogic) TagMappingDelete(req *types.TagMappingDeleteReq) (resp *types.BaseResp, err error) {
	err = l.svcCtx.TagMappingModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// ==================== 自定义POC ====================

type CustomPocListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomPocListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomPocListLogic {
	return &CustomPocListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// customPocFilterQuery 自定义POC筛选条件（列表查询与分面统计共用）
type customPocFilterQuery struct {
	Name       string
	TemplateId string
	Severity   string
	Tag        string
	Enabled    *bool
	Keyword    string
	Severities []string
	Protocols  []string
	Products   []string
	HasCve     *bool
}

// buildFilter 构建Mongo查询条件
// excludeFacet 非空时剔除该筛选维度的条件，用于分面统计（其余条件生效，保证计数与实际可选结果一致）
func (q customPocFilterQuery) buildFilter(excludeFacet string) bson.M {
	filter := bson.M{}
	if q.Name != "" {
		filter["name"] = bson.M{"$regex": regexp.QuoteMeta(q.Name), "$options": "i"}
	}
	if q.TemplateId != "" {
		filter["template_id"] = bson.M{"$regex": regexp.QuoteMeta(q.TemplateId), "$options": "i"}
	}
	// 兼容单选severity + 多选severities
	severities := make([]string, 0, len(q.Severities)+1)
	for _, s := range q.Severities {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			severities = append(severities, s)
		}
	}
	if q.Severity != "" {
		if s := strings.ToLower(strings.TrimSpace(q.Severity)); s != "" {
			severities = append(severities, s)
		}
	}
	if len(severities) > 0 && excludeFacet != "severity" {
		if len(severities) == 1 {
			filter["severity"] = severities[0]
		} else {
			filter["severity"] = bson.M{"$in": severities}
		}
	}
	if len(q.Protocols) > 0 && excludeFacet != "protocol" {
		filter["protocol"] = bson.M{"$in": q.Protocols}
	}
	if len(q.Products) > 0 && excludeFacet != "product" {
		filter["product"] = bson.M{"$in": q.Products}
	}
	if q.Tag != "" && excludeFacet != "tag" {
		filter["tags"] = bson.M{"$in": []string{q.Tag}}
	}
	if q.Enabled != nil && excludeFacet != "enabled" {
		filter["enabled"] = *q.Enabled
	}
	if q.Keyword != "" {
		// 全字段模糊搜索：一个输入框覆盖所有POC字段
		kw := regexp.QuoteMeta(q.Keyword)
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
	if q.HasCve != nil && excludeFacet != "cve" {
		filter["cve_ids.0"] = bson.M{"$exists": *q.HasCve}
	}
	return filter
}

// newCustomPocFilterQuery 从请求构造筛选条件
func newCustomPocFilterQuery(name, templateId, severity, tag, keyword string, enabled, hasCve *bool, severities, protocols, products []string) customPocFilterQuery {
	return customPocFilterQuery{
		Name:       name,
		TemplateId: templateId,
		Severity:   severity,
		Tag:        tag,
		Enabled:    enabled,
		Keyword:    keyword,
		Severities: severities,
		Protocols:  protocols,
		Products:   products,
		HasCve:     hasCve,
	}
}

// enrichCustomPoc 从POC内容解析 协议/厂商/产品/漏洞知识库 字段
func enrichCustomPoc(doc *model.CustomPoc) {
	if doc == nil || doc.Content == "" {
		return
	}
	doc.Severity = template.NormalizeSeverity(doc.Severity)
	doc.Protocol = template.ParseProtocol(doc.Content)
	if info, err := template.ParseTemplateInfo(doc.Content); err == nil && info != nil {
		doc.Vendor = info.GetVendor()
		doc.Product = info.GetProduct()
		doc.CvssScore = info.GetCvssScore()
		doc.CvssMetrics = info.GetCvssMetrics()
		doc.CveIds = info.GetCveIds()
		doc.CweIds = info.GetCweIds()
		doc.References = info.GetReferences()
		doc.Remediation = info.GetRemediation()
	}
}

func (l *CustomPocListLogic) CustomPocList(req *types.CustomPocListReq) (resp *types.CustomPocListResp, err error) {
	// 构建筛选条件
	filter := newCustomPocFilterQuery(req.Name, req.TemplateId, req.Severity, req.Tag, req.Keyword, req.Enabled, req.HasCve, req.Severities, req.Protocols, req.Products).buildFilter("")

	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
	docs, err := l.svcCtx.CustomPocModel.FindWithFilter(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		return &types.CustomPocListResp{Code: 500, Msg: "查询失败"}, nil
	}

	total, _ := l.svcCtx.CustomPocModel.CountWithFilter(l.ctx, filter)

	list := make([]types.CustomPoc, 0, len(docs))
	for _, doc := range docs {
		list = append(list, types.CustomPoc{
			Id:          doc.Id.Hex(),
			Name:        doc.Name,
			TemplateId:  doc.TemplateId,
			Severity:    doc.Severity,
			Tags:        doc.Tags,
			Author:      doc.Author,
			Description: doc.Description,
			Protocol:    doc.Protocol,
			Vendor:      doc.Vendor,
			Product:     doc.Product,
			CvssScore:   doc.CvssScore,
			CveIds:      doc.CveIds,
			Content:     doc.Content,
			Enabled:     doc.Enabled,
			CreateTime:  doc.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.CustomPocListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
}

// CustomPocCategoriesLogic 自定义POC筛选维度统计
type CustomPocCategoriesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomPocCategoriesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomPocCategoriesLogic {
	return &CustomPocCategoriesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CustomPocCategoriesLogic) CustomPocCategories(req *types.CustomPocCategoriesReq) (resp *types.CustomPocCategoriesResp, err error) {
	q := newCustomPocFilterQuery(req.Name, req.TemplateId, req.Severity, req.Tag, req.Keyword, req.Enabled, req.HasCve, req.Severities, req.Protocols, req.Products)

	m := l.svcCtx.CustomPocModel

	// 各分面统计时剔除自身维度条件，展示"点击后可得"的数量
	severityFacets, err := m.GetFacetCounts(l.ctx, q.buildFilter("severity"), "severity", 0)
	if err != nil {
		l.Logger.Errorf("get custom poc severity facets failed: %v", err)
		severityFacets = []model.FacetItem{}
	}
	// 严重级别按固定顺序展示
	severities := orderFacetsByValue(severityFacets, severityDisplayOrder)

	protocols, err := m.GetFacetCounts(l.ctx, q.buildFilter("protocol"), "protocol", 0)
	if err != nil {
		l.Logger.Errorf("get custom poc protocol facets failed: %v", err)
		protocols = []model.FacetItem{}
	}

	products, err := m.GetFacetCounts(l.ctx, q.buildFilter("product"), "product", 50)
	if err != nil {
		l.Logger.Errorf("get custom poc product facets failed: %v", err)
		products = []model.FacetItem{}
	}

	tags, err := m.GetTagFacetCounts(l.ctx, q.buildFilter("tag"), 100)
	if err != nil {
		l.Logger.Errorf("get custom poc tag facets failed: %v", err)
		tags = []model.FacetItem{}
	}

	withCve, withoutCve, err := m.GetCveFacetCounts(l.ctx, q.buildFilter("cve"))
	if err != nil {
		l.Logger.Errorf("get custom poc cve facets failed: %v", err)
	}

	// 总数按当前全部筛选条件统计
	total, err := m.CountWithFilter(l.ctx, q.buildFilter(""))
	if err != nil {
		total = 0
	}

	return &types.CustomPocCategoriesResp{
		Code:       0,
		Msg:        "success",
		Severities: facetItems(severities),
		Protocols:  facetItems(protocols),
		Products:   facetItems(products),
		Tags:       facetItems(tags),
		CveStats:   map[string]int{"true": int(withCve), "false": int(withoutCve)},
		Stats:      map[string]int{"total": int(total)},
	}, nil
}

type CustomPocSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomPocSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomPocSaveLogic {
	return &CustomPocSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CustomPocSaveLogic) CustomPocSave(req *types.CustomPocSaveReq) (resp *types.BaseResp, err error) {
	// 验证POC模板是否有效
	if req.Content != "" {
		if err := scanner.ValidatePocTemplate(req.Content); err != nil {
			return &types.BaseResp{Code: 400, Msg: "POC验证失败: " + err.Error()}, nil
		}
	}

	// 检查是否与默认模板重复（仅新建时检查，通过templateId匹配）
	isDuplicate := false
	if req.Id == "" && req.TemplateId != "" {
		existingTemplate, err := l.svcCtx.NucleiTemplateModel.FindByTemplateId(l.ctx, req.TemplateId)
		if err == nil && existingTemplate != nil {
			// 存在重复的默认模板
			isDuplicate = true
		}
	}

	// 如果重复，修改名称并禁用
	pocName := req.Name
	pocEnabled := req.Enabled
	if isDuplicate {
		if !strings.HasPrefix(pocName, "【重复】") {
			pocName = "【重复】" + pocName
		}
		pocEnabled = false
	}

	doc := &model.CustomPoc{
		Name:        pocName,
		TemplateId:  req.TemplateId,
		Severity:    req.Severity,
		Tags:        req.Tags,
		Author:      req.Author,
		Description: req.Description,
		Content:     req.Content,
		Enabled:     pocEnabled,
	}
	// 从内容解析 协议/厂商/产品/知识库 字段
	enrichCustomPoc(doc)

	if req.Id != "" {
		err = l.svcCtx.CustomPocModel.Update(l.ctx, req.Id, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新失败"}, nil
		}
	} else {
		err = l.svcCtx.CustomPocModel.Insert(l.ctx, doc)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "创建失败"}, nil
		}
	}

	msg := "保存成功"
	if isDuplicate {
		msg = "保存成功，该POC与默认模板重复，已标记【重复】并禁用"
	}

	return &types.BaseResp{Code: 0, Msg: msg}, nil
}

type CustomPocDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomPocDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomPocDeleteLogic {
	return &CustomPocDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CustomPocDeleteLogic) CustomPocDelete(req *types.CustomPocDeleteReq) (resp *types.BaseResp, err error) {
	err = l.svcCtx.CustomPocModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// ==================== 批量导入自定义POC ====================

type CustomPocBatchImportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomPocBatchImportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomPocBatchImportLogic {
	return &CustomPocBatchImportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CustomPocBatchImportLogic) CustomPocBatchImport(req *types.CustomPocBatchImportReq) (resp *types.CustomPocBatchImportResp, err error) {
	if len(req.Pocs) == 0 {
		return &types.CustomPocBatchImportResp{Code: 400, Msg: "POC列表不能为空"}, nil
	}

	imported := 0
	failed := 0
	duplicateCount := 0
	errors := make([]string, 0)

	for i, poc := range req.Pocs {
		// 验证必填字段
		if poc.Name == "" {
			failed++
			errors = append(errors, fmt.Sprintf("第%d个POC: 名称不能为空", i+1))
			continue
		}
		if poc.Content == "" {
			failed++
			errors = append(errors, poc.Name+": 内容不能为空")
			continue
		}

		// 检查是否与默认模板重复（通过templateId匹配）
		isDuplicate := false
		if poc.TemplateId != "" {
			existingTemplate, err := l.svcCtx.NucleiTemplateModel.FindByTemplateId(l.ctx, poc.TemplateId)
			if err == nil && existingTemplate != nil {
				// 存在重复的默认模板
				isDuplicate = true
				duplicateCount++
			}
		}

		// 如果重复，修改名称并禁用
		pocName := poc.Name
		pocEnabled := poc.Enabled
		if isDuplicate {
			if !strings.HasPrefix(pocName, "【重复】") {
				pocName = "【重复】" + pocName
			}
			pocEnabled = false
		}

		doc := &model.CustomPoc{
			Name:        pocName,
			TemplateId:  poc.TemplateId,
			Severity:    poc.Severity,
			Tags:        poc.Tags,
			Author:      poc.Author,
			Description: poc.Description,
			Content:     poc.Content,
			Enabled:     pocEnabled,
		}
		// 从内容解析 协议/厂商/产品/知识库 字段
		enrichCustomPoc(doc)

		err := l.svcCtx.CustomPocModel.Insert(l.ctx, doc)
		if err != nil {
			failed++
			errors = append(errors, poc.Name+": "+err.Error())
			continue
		}
		imported++
	}

	msg := "导入完成"
	if failed > 0 {
		msg = "部分导入成功"
	}
	if duplicateCount > 0 {
		msg = fmt.Sprintf("%s，%d个POC与默认模板重复已标记并禁用", msg, duplicateCount)
	}

	return &types.CustomPocBatchImportResp{
		Code:     0,
		Msg:      msg,
		Imported: imported,
		Failed:   failed,
		Errors:   errors,
	}, nil
}

// ==================== Nuclei默认模板 ====================

type NucleiTemplateListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNucleiTemplateListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NucleiTemplateListLogic {
	return &NucleiTemplateListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// nucleiTemplateFilterQuery 模板库筛选条件（列表查询与分面统计共用）
type nucleiTemplateFilterQuery struct {
	Category     string
	Severity     string
	Tag          string
	Keyword      string
	MinCvssScore float64
	CveId        string
	Severities   []string
	Protocols    []string
	Products     []string
	HasCve       *bool
}

// buildFilter 构建Mongo查询条件
// excludeFacet 非空时剔除该筛选维度的条件，用于分面统计（其余条件生效，保证计数与实际可选结果一致）
func (q nucleiTemplateFilterQuery) buildFilter(excludeFacet string) bson.M {
	filter := bson.M{}
	if q.Category != "" && excludeFacet != "category" {
		filter["category"] = q.Category
	}
	// 兼容单选severity + 多选severities
	severities := make([]string, 0, len(q.Severities)+1)
	for _, s := range q.Severities {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			severities = append(severities, s)
		}
	}
	if q.Severity != "" {
		if s := strings.ToLower(strings.TrimSpace(q.Severity)); s != "" {
			severities = append(severities, s)
		}
	}
	if len(severities) > 0 && excludeFacet != "severity" {
		if len(severities) == 1 {
			filter["severity"] = severities[0]
		} else {
			filter["severity"] = bson.M{"$in": severities}
		}
	}
	if len(q.Protocols) > 0 && excludeFacet != "protocol" {
		filter["protocol"] = bson.M{"$in": q.Protocols}
	}
	if len(q.Products) > 0 && excludeFacet != "product" {
		filter["product"] = bson.M{"$in": q.Products}
	}
	if q.Tag != "" && excludeFacet != "tag" {
		// 标签模糊匹配
		filter["tags"] = bson.M{"$regex": regexp.QuoteMeta(q.Tag), "$options": "i"}
	}
	if q.Keyword != "" {
		// 全字段模糊搜索：一个输入框覆盖所有模板字段
		kw := regexp.QuoteMeta(q.Keyword)
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
	if q.MinCvssScore > 0 {
		filter["cvss_score"] = bson.M{"$gte": q.MinCvssScore}
	}
	if q.CveId != "" && excludeFacet != "cve" {
		filter["cve_ids"] = bson.M{"$regex": regexp.QuoteMeta(q.CveId), "$options": "i"}
	}
	if q.HasCve != nil && excludeFacet != "cve" {
		filter["cve_ids.0"] = bson.M{"$exists": *q.HasCve}
	}
	return filter
}

func (l *NucleiTemplateListLogic) NucleiTemplateList(req *types.NucleiTemplateListReq) (resp *types.NucleiTemplateListResp, err error) {
	// 构建查询条件
	filter := nucleiTemplateFilterQuery{
		Category:     req.Category,
		Severity:     req.Severity,
		Tag:          req.Tag,
		Keyword:      req.Keyword,
		MinCvssScore: req.MinCvssScore,
		CveId:        req.CveId,
		Severities:   req.Severities,
		Protocols:    req.Protocols,
		Products:     req.Products,
		HasCve:       req.HasCve,
	}.buildFilter("")

	// 查询总数
	total, err := l.svcCtx.NucleiTemplateModel.Count(l.ctx, filter)
	if err != nil {
		return &types.NucleiTemplateListResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
	}

	// 查询列表
	docs, err := l.svcCtx.NucleiTemplateModel.Find(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		return &types.NucleiTemplateListResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
	}

	// 转换为响应类型
	list := make([]types.NucleiTemplate, 0, len(docs))
	for _, doc := range docs {
		list = append(list, types.NucleiTemplate{
			Id:          doc.TemplateId,
			Name:        doc.Name,
			Author:      doc.Author,
			Severity:    doc.Severity,
			Description: doc.Description,
			Tags:        doc.Tags,
			Category:    doc.Category,
			Protocol:    doc.Protocol,
			Vendor:      doc.Vendor,
			Product:     doc.Product,
			FilePath:    doc.FilePath,
			// 新增字段 - 漏洞知识库
			CvssScore:   doc.CvssScore,
			CvssMetrics: doc.CvssMetrics,
			CveIds:      doc.CveIds,
			CweIds:      doc.CweIds,
			References:  doc.References,
			Remediation: doc.Remediation,
		})
	}

	return &types.NucleiTemplateListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
}

type NucleiTemplateCategoriesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNucleiTemplateCategoriesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NucleiTemplateCategoriesLogic {
	return &NucleiTemplateCategoriesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// severityDisplayOrder 严重级别固定展示顺序（官方模板库已无 unknown）
var severityDisplayOrder = []string{"critical", "high", "medium", "low", "info"}

func (l *NucleiTemplateCategoriesLogic) NucleiTemplateCategories(req *types.NucleiTemplateCategoriesReq) (resp *types.NucleiTemplateCategoriesResp, err error) {
	q := nucleiTemplateFilterQuery{
		Category:     req.Category,
		Severity:     req.Severity,
		Tag:          req.Tag,
		Keyword:      req.Keyword,
		MinCvssScore: req.MinCvssScore,
		CveId:        req.CveId,
		Severities:   req.Severities,
		Protocols:    req.Protocols,
		Products:     req.Products,
		HasCve:       req.HasCve,
	}

	m := l.svcCtx.NucleiTemplateModel

	// 各分面统计时剔除自身维度条件，展示"点击后可得"的数量
	categories, err := m.GetFacetCounts(l.ctx, q.buildFilter("category"), "category", 0)
	if err != nil {
		l.Logger.Errorf("get category facets failed: %v", err)
		categories = []model.FacetItem{}
	}

	severityFacets, err := m.GetFacetCounts(l.ctx, q.buildFilter("severity"), "severity", 0)
	if err != nil {
		l.Logger.Errorf("get severity facets failed: %v", err)
		severityFacets = []model.FacetItem{}
	}
	// 严重级别按固定顺序展示
	severities := orderFacetsByValue(severityFacets, severityDisplayOrder)

	protocols, err := m.GetFacetCounts(l.ctx, q.buildFilter("protocol"), "protocol", 0)
	if err != nil {
		l.Logger.Errorf("get protocol facets failed: %v", err)
		protocols = []model.FacetItem{}
	}

	products, err := m.GetFacetCounts(l.ctx, q.buildFilter("product"), "product", 50)
	if err != nil {
		l.Logger.Errorf("get product facets failed: %v", err)
		products = []model.FacetItem{}
	}

	tags, err := m.GetTagFacetCounts(l.ctx, q.buildFilter("tag"), 100)
	if err != nil {
		l.Logger.Errorf("get tag facets failed: %v", err)
		tags = []model.FacetItem{}
	}

	withCve, withoutCve, err := m.GetCveFacetCounts(l.ctx, q.buildFilter("cve"))
	if err != nil {
		l.Logger.Errorf("get cve facets failed: %v", err)
	}

	// 总数按当前全部筛选条件统计
	total, err := m.Count(l.ctx, q.buildFilter(""))
	if err != nil {
		total = 0
	}

	return &types.NucleiTemplateCategoriesResp{
		Code:       0,
		Msg:        "success",
		Categories: facetItems(categories),
		Severities: facetItems(severities),
		Protocols:  facetItems(protocols),
		Products:   facetItems(products),
		Tags:       facetItems(tags),
		CveStats:   map[string]int{"true": int(withCve), "false": int(withoutCve)},
		Stats:      map[string]int{"total": int(total)},
	}, nil
}

// facetItems 模型分面项转API类型（保证非nil便于前端遍历）
func facetItems(items []model.FacetItem) []types.FacetItem {
	result := make([]types.FacetItem, 0, len(items))
	for _, item := range items {
		result = append(result, types.FacetItem{Value: item.Value, Count: item.Count})
	}
	return result
}

// orderFacetsByValue 按给定顺序排列分面项（未在顺序表中的排到最后，保持原有数量降序）
func orderFacetsByValue(items []model.FacetItem, order []string) []model.FacetItem {
	rank := make(map[string]int, len(order))
	for i, v := range order {
		rank[v] = i
	}
	ordered := make([]model.FacetItem, 0, len(items))
	rest := make([]model.FacetItem, 0, len(items))
	for _, v := range order {
		for _, item := range items {
			if item.Value == v {
				ordered = append(ordered, item)
				break
			}
		}
	}
	for _, item := range items {
		if _, ok := rank[item.Value]; !ok {
			rest = append(rest, item)
		}
	}
	return append(ordered, rest...)
}

// ==================== Nuclei模板启用/禁用 ====================

type NucleiTemplateUpdateEnabledLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNucleiTemplateUpdateEnabledLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NucleiTemplateUpdateEnabledLogic {
	return &NucleiTemplateUpdateEnabledLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NucleiTemplateUpdateEnabledLogic) UpdateEnabled(req *types.NucleiTemplateUpdateEnabledReq) (resp *types.BaseResp, err error) {
	if len(req.TemplateIds) == 0 {
		return &types.BaseResp{Code: 400, Msg: "请选择模板"}, nil
	}

	err = l.svcCtx.NucleiTemplateModel.BatchUpdateEnabled(l.ctx, req.TemplateIds, req.Enabled)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "更新失败: " + err.Error()}, nil
	}

	action := "启用"
	if !req.Enabled {
		action = "禁用"
	}
	return &types.BaseResp{Code: 0, Msg: action + "成功"}, nil
}

// ==================== Nuclei模板详情 ====================

type NucleiTemplateDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNucleiTemplateDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NucleiTemplateDetailLogic {
	return &NucleiTemplateDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NucleiTemplateDetailLogic) GetDetail(req *types.NucleiTemplateDetailReq) (resp *types.NucleiTemplateDetailResp, err error) {
	if req.TemplateId == "" {
		return &types.NucleiTemplateDetailResp{Code: 400, Msg: "模板ID不能为空"}, nil
	}

	// 从数据库查询完整模板（包含content）
	doc, err := l.svcCtx.NucleiTemplateModel.FindByTemplateId(l.ctx, req.TemplateId)
	if err != nil || doc == nil {
		return &types.NucleiTemplateDetailResp{Code: 404, Msg: "模板不存在"}, nil
	}
	return &types.NucleiTemplateDetailResp{
		Code: 0,
		Msg:  "success",
		Data: &types.NucleiTemplateWithContent{
			Id:          doc.TemplateId,
			Name:        doc.Name,
			Author:      doc.Author,
			Severity:    doc.Severity,
			Description: doc.Description,
			Tags:        doc.Tags,
			Category:    doc.Category,
			Protocol:    doc.Protocol,
			Vendor:      doc.Vendor,
			Product:     doc.Product,
			FilePath:    doc.FilePath,
			Content:     doc.Content,
			// 新增字段 - 漏洞知识库
			CvssScore:   doc.CvssScore,
			CvssMetrics: doc.CvssMetrics,
			CveIds:      doc.CveIds,
			CweIds:      doc.CweIds,
			References:  doc.References,
			Remediation: doc.Remediation,
		},
	}, nil
}

// ==================== POC验证 ====================

type PocValidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPocValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PocValidateLogic {
	return &PocValidateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PocValidateLogic) PocValidate(req *types.PocValidateReq) (resp *types.PocValidateResp, err error) {
	if req.Url == "" {
		return &types.PocValidateResp{Code: 400, Msg: "URL不能为空"}, nil
	}
	if req.Id == "" {
		return &types.PocValidateResp{Code: 400, Msg: "POC ID不能为空"}, nil
	}

	// 根据pocType确定POC类型
	pocType := req.PocType
	if pocType == "" {
		pocType = "custom" // 默认为自定义POC
	}

	var pocSeverity string

	if pocType == "nuclei" {
		template, err := l.svcCtx.NucleiTemplateModel.FindByTemplateId(l.ctx, req.Id)
		if err != nil {
			l.Logger.Errorf("PocValidate: find nuclei template failed, id=%s, error=%v", req.Id, err)
			return &types.PocValidateResp{Code: 500, Msg: "查询Nuclei模板失败"}, nil
		}
		if template == nil {
			return &types.PocValidateResp{Code: 404, Msg: "Nuclei模板不存在"}, nil
		}
		pocSeverity = template.Severity
	} else {
		poc, err := l.svcCtx.CustomPocModel.FindById(l.ctx, req.Id)
		if err != nil {
			l.Logger.Errorf("PocValidate: find custom poc failed, id=%s, error=%v", req.Id, err)
			return &types.PocValidateResp{Code: 500, Msg: "查询POC失败"}, nil
		}
		if poc == nil {
			return &types.PocValidateResp{Code: 404, Msg: "POC不存在"}, nil
		}
		pocSeverity = poc.Severity
	}

	// 检查在线 Worker
	if err := checkOnlineWorkers(l.ctx, l.svcCtx); err != nil {
		return &types.PocValidateResp{Code: 500, Msg: err.Error()}, nil
	}

	// 直接入队
	taskId := uuid.New().String()
	taskConfig := map[string]interface{}{
		"taskType": "poc_validate",
		"url":      req.Url,
		"pocId":    req.Id,
		"pocType":  pocType,
		"timeout":  30,
	}
	configBytes, _ := json.Marshal(taskConfig)

	task := &scheduler.TaskInfo{
		TaskId:     taskId,
		MainTaskId: taskId,
		TaskName:   "POC验证",
		Config:     string(configBytes),
		Priority:   2,
	}

	if err := l.svcCtx.Scheduler.PushTask(l.ctx, task); err != nil {
		l.Logger.Errorf("PocValidate: push task failed, taskId=%s, error=%v", taskId, err)
		return &types.PocValidateResp{Code: 500, Msg: "任务下发失败"}, nil
	}

	// 持久化 taskInfo
	persistTaskInfo(l.ctx, l.svcCtx, taskId, taskConfig)

	l.Logger.Infof("PocValidate: task created, taskId=%s, pocId=%s, url=%s", taskId, req.Id, req.Url)

	return &types.PocValidateResp{
		Code:     0,
		Msg:      "POC验证任务已下发，请稍后查询结果",
		Matched:  false,
		Severity: pocSeverity,
		TaskId:   taskId,
	}, nil
}

// ==================== 批量POC验证 ====================

type PocBatchValidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPocBatchValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PocBatchValidateLogic {
	return &PocBatchValidateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PocBatchValidateLogic) PocBatchValidate(req *types.PocBatchValidateReq) (resp *types.PocBatchValidateResp, err error) {
	if len(req.Urls) == 0 {
		return &types.PocBatchValidateResp{Code: 400, Msg: "URL列表不能为空"}, nil
	}

	// 检查在线 Worker
	if err := checkOnlineWorkers(l.ctx, l.svcCtx); err != nil {
		return &types.PocBatchValidateResp{Code: 500, Msg: err.Error()}, nil
	}

	if req.PocType == "" {
		req.PocType = "all"
	}
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 10
	}
	if !req.UseTemplate && !req.UseCustom {
		req.UseTemplate = true
		req.UseCustom = true
	}

	taskId := uuid.New().String()
	taskConfig := map[string]interface{}{
		"taskType":    "poc_batch_validate",
		"urls":        req.Urls,
		"pocType":     req.PocType,
		"severities":  req.Severities,
		"tags":        req.Tags,
		"timeout":     req.Timeout,
		"useTemplate": req.UseTemplate,
		"useCustom":   req.UseCustom,
		"concurrency": req.Concurrency,
		"batchMode":   true,
	}
	configBytes, _ := json.Marshal(taskConfig)

	task := &scheduler.TaskInfo{
		TaskId:     taskId,
		MainTaskId: taskId,
		TaskName:   "POC批量扫描",
		Config:     string(configBytes),
		Priority:   2,
	}

	if err := l.svcCtx.Scheduler.PushTask(l.ctx, task); err != nil {
		l.Logger.Errorf("PocBatchValidate: push task failed, taskId=%s, error=%v", taskId, err)
		return &types.PocBatchValidateResp{Code: 500, Msg: "任务下发失败"}, nil
	}

	persistTaskInfo(l.ctx, l.svcCtx, taskId, taskConfig)

	return &types.PocBatchValidateResp{
		Code:      0,
		Msg:       "批量验证任务已下发，请使用返回的批次ID查询结果",
		TotalUrls: len(req.Urls),
		BatchId:   taskId,
	}, nil
}

// ==================== POC验证结果查询 ====================

type PocValidationResultQueryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPocValidationResultQueryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PocValidationResultQueryLogic {
	return &PocValidationResultQueryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PocValidationResultQueryLogic) PocValidationResultQuery(req *types.PocValidationResultQueryReq) (resp *types.PocValidationResultQueryResp, err error) {
	if req.TaskId == "" && req.BatchId == "" {
		return &types.PocValidationResultQueryResp{Code: 400, Msg: "任务ID或批次ID不能为空"}, nil
	}

	taskId := req.TaskId
	if taskId == "" {
		taskId = req.BatchId
	}

	// 从 Redis 读取任务状态
	statusKey := "cscan:task:status:" + taskId
	val, err := l.svcCtx.RedisClient.Get(l.ctx, statusKey).Result()
	if err != nil {
		// 检查任务是否还在执行中
		taskInfoKey := "cscan:task:info:" + taskId
		if _, infoErr := l.svcCtx.RedisClient.Get(l.ctx, taskInfoKey).Result(); infoErr == nil {
			return &types.PocValidationResultQueryResp{
				Code:   0,
				Msg:    "任务执行中",
				Status: "RUNNING",
			}, nil
		}
		return &types.PocValidationResultQueryResp{Code: 500, Msg: "未找到验证结果", Status: "NOT_FOUND"}, nil
	}

	var statusInfo map[string]interface{}
	if err := json.Unmarshal([]byte(val), &statusInfo); err != nil {
		return &types.PocValidationResultQueryResp{Code: 500, Msg: "解析状态失败", Status: "ERROR"}, nil
	}

	state, _ := statusInfo["state"].(string)
	if state == "" {
		state = "RUNNING"
	}
	status := state
	if state == "COMPLETED" {
		status = "SUCCESS"
	}

	var results []types.PocValidationResult
	resultStr, _ := statusInfo["result"].(string)
	if resultStr != "" {
		var resultData map[string]interface{}
		if json.Unmarshal([]byte(resultStr), &resultData) == nil {
			if resultStatus, ok := resultData["status"].(string); ok && resultStatus != "" {
				status = resultStatus
			}
			if resultsArr, ok := resultData["results"].([]interface{}); ok {
				for _, r := range resultsArr {
					if rMap, ok := r.(map[string]interface{}); ok {
						pr := types.PocValidationResult{
							PocId:      getString(rMap, "pocId"),
							PocName:    getString(rMap, "pocName"),
							TemplateId: getString(rMap, "templateId"),
							Severity:   getString(rMap, "severity"),
							Matched:    getBool(rMap, "matched"),
							MatchedUrl: getString(rMap, "matchedUrl"),
							Details:    getString(rMap, "details"),
							Output:     getString(rMap, "output"),
							PocType:    getString(rMap, "pocType"),
						}
						if tags, ok := rMap["tags"].([]interface{}); ok {
							for _, t := range tags {
								if s, ok := t.(string); ok {
									pr.Tags = append(pr.Tags, s)
								}
							}
						}
						results = append(results, pr)
					}
				}
			}
		}
	}

	return &types.PocValidationResultQueryResp{
		Code:           0,
		Msg:            "查询成功",
		Status:         status,
		CompletedCount: len(results),
		TotalCount:     len(results),
		Results:        results,
	}, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// ==================== 清空所有自定义POC ====================

type CustomPocClearAllLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomPocClearAllLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomPocClearAllLogic {
	return &CustomPocClearAllLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CustomPocClearAllLogic) CustomPocClearAll(req *types.CustomPocClearAllReq) (resp *types.CustomPocClearAllResp, err error) {
	// 构建筛选条件（与列表筛选保持一致）
	filter := newCustomPocFilterQuery(req.Name, req.TemplateId, req.Severity, req.Tag, req.Keyword, req.Enabled, req.HasCve, req.Severities, req.Protocols, req.Products).buildFilter("")

	// 先获取符合条件的总数
	total, _ := l.svcCtx.CustomPocModel.CountWithFilter(l.ctx, filter)

	if total == 0 {
		return &types.CustomPocClearAllResp{Code: 0, Msg: "没有符合条件的POC", Deleted: 0}, nil
	}

	// 按条件删除自定义POC
	deleted, err := l.svcCtx.CustomPocModel.DeleteWithFilter(l.ctx, filter)
	if err != nil {
		return &types.CustomPocClearAllResp{Code: 500, Msg: "清空失败: " + err.Error()}, nil
	}

	if deleted == 0 {
		deleted = total
	}

	return &types.CustomPocClearAllResp{
		Code:    0,
		Msg:     "清空成功",
		Deleted: int(deleted),
	}, nil
}

// ==================== 自定义POC扫描现有资产 ====================

type CustomPocScanAssetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomPocScanAssetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomPocScanAssetsLogic {
	return &CustomPocScanAssetsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CustomPocScanAssetsLogic) CustomPocScanAssets(req *types.CustomPocScanAssetsReq) (*types.CustomPocScanAssetsResp, error) {
	if req.PocId == "" {
		return &types.CustomPocScanAssetsResp{Code: 400, Msg: "POC ID不能为空"}, nil
	}

	// 获取POC
	poc, err := l.svcCtx.CustomPocModel.FindById(l.ctx, req.PocId)
	if err != nil {
		l.Logger.Errorf("CustomPocScanAssets: find poc failed, pocId=%s, error=%v", req.PocId, err)
		return &types.CustomPocScanAssetsResp{Code: 500, Msg: "查询POC失败"}, nil
	}
	if poc == nil {
		return &types.CustomPocScanAssetsResp{Code: 404, Msg: "POC不存在"}, nil
	}

	// 常见HTTP端口
	httpPorts := []int{80, 8080, 8000, 8888, 8081, 8082, 8008, 9000, 9080, 3000, 5000}
	httpsPorts := []int{443, 8443, 9443, 4443}
	allHttpPorts := append(httpPorts, httpsPorts...)

	// 获取所有HTTP资产（扩展过滤条件）
	filter := bson.M{
		"$or": []bson.M{
			{"is_http": true}, // is_http 标记为 true
			{"service": bson.M{"$in": []string{"http", "https", "http-proxy"}}}, // service 为 http/https
			{"port": bson.M{"$in": allHttpPorts}},                               // 常见 HTTP 端口
			{"title": bson.M{"$exists": true, "$ne": ""}},                       // 有 title（说明是 HTTP 服务）
			{"authority": bson.M{"$regex": "^https?://", "$options": "i"}},      // authority 以 http:// 或 https:// 开头
		},
	}

	assetModel := l.svcCtx.GetAssetModel()
	// 全量查询：Find(0,0) 会被 NormalizePage 钳成第 1 页 20 条，导致只扫到 20 个目标。
	// 改用 FindAllForAgg（不分页 + AssetAggProjection 瘦投影，含 authority/service/port/host，够 buildAssetUrl 用）。
	assets, err := assetModel.FindAllForAgg(l.ctx, filter)
	if err != nil {
		l.Logger.Errorf("查询资产失败: %v", err)
	}

	if len(assets) == 0 {
		return &types.CustomPocScanAssetsResp{
			Code:         0,
			Msg:          "没有可扫描的HTTP资产",
			TotalScanned: 0,
			VulnCount:    0,
			Duration:     "0s",
			VulnList:     []types.CustomPocScanVulnItem{},
			TaskIds:      []string{},
		}, nil
	}

	l.Logger.Infof("CustomPocScanAssets: pocId=%s, name=%s, totalAssets=%d", req.PocId, poc.Name, len(assets))

	// 准备目标URL列表（去重）
	urlSet := make(map[string]bool)
	var urls []string
	for i := range assets {
		asset := &assets[i]
		url := buildAssetUrl(asset, httpsPorts)
		if url == "" {
			continue
		}
		// 去重
		if urlSet[url] {
			continue
		}
		urlSet[url] = true
		urls = append(urls, url)
	}

	if len(urls) == 0 {
		return &types.CustomPocScanAssetsResp{
			Code:         0,
			Msg:          "没有有效的目标URL",
			TotalScanned: 0,
			VulnCount:    0,
			Duration:     "0s",
			VulnList:     []types.CustomPocScanVulnItem{},
			TaskIds:      []string{},
		}, nil
	}

	// 检查在线 Worker
	if err := checkOnlineWorkers(l.ctx, l.svcCtx); err != nil {
		return &types.CustomPocScanAssetsResp{Code: 500, Msg: err.Error()}, nil
	}

	// 创建批量扫描任务
	taskId := uuid.New().String()
	taskConfig := map[string]interface{}{
		"taskType":    "poc_batch_validate",
		"urls":        urls,
		"pocId":       req.PocId,
		"pocType":     "custom",
		"timeout":     len(urls) * 30,
		"useTemplate": false,
		"useCustom":   true,
		"batchMode":   true,
	}
	configBytes, _ := json.Marshal(taskConfig)

	task := &scheduler.TaskInfo{
		TaskId:     taskId,
		MainTaskId: taskId,
		TaskName:   "POC批量扫描",
		Config:     string(configBytes),
		Priority:   2,
	}

	if err := l.svcCtx.Scheduler.PushTask(l.ctx, task); err != nil {
		l.Logger.Errorf("CustomPocScanAssets: push task failed, taskId=%s, error=%v", taskId, err)
		return &types.CustomPocScanAssetsResp{Code: 500, Msg: "创建扫描任务失败: " + err.Error()}, nil
	}

	persistTaskInfo(l.ctx, l.svcCtx, taskId, taskConfig)

	msg := fmt.Sprintf("已创建批量扫描任务（POC: %s，目标: %d个），发现的漏洞将显示在漏洞页面", poc.Name, len(urls))

	return &types.CustomPocScanAssetsResp{
		Code:         0,
		Msg:          msg,
		TotalScanned: len(urls),
		VulnCount:    0,
		Duration:     "异步执行中",
		VulnList:     []types.CustomPocScanVulnItem{},
		TaskIds:      []string{taskId},
	}, nil
}

// buildAssetUrl 根据资产信息构建正确的URL
func buildAssetUrl(asset *model.Asset, httpsPorts []int) string {
	// 如果 authority 已经有协议前缀，直接返回
	if strings.HasPrefix(asset.Authority, "http://") || strings.HasPrefix(asset.Authority, "https://") {
		return asset.Authority
	}

	// 判断是否使用 HTTPS
	useHttps := false

	// 1. 根据 service 判断
	if asset.Service == "https" || asset.Service == "ssl" || asset.Service == "tls" {
		useHttps = true
	}

	// 2. 根据端口判断
	if !useHttps {
		for _, p := range httpsPorts {
			if asset.Port == p {
				useHttps = true
				break
			}
		}
	}

	// 构建 URL
	var url string
	if asset.Authority != "" {
		// 使用 authority（通常是 host:port 格式）
		if useHttps {
			url = "https://" + asset.Authority
		} else {
			url = "http://" + asset.Authority
		}
	} else if asset.Host != "" {
		// 使用 host:port 构建
		if useHttps {
			if asset.Port == 443 {
				url = fmt.Sprintf("https://%s", asset.Host)
			} else {
				url = fmt.Sprintf("https://%s:%d", asset.Host, asset.Port)
			}
		} else {
			if asset.Port == 80 {
				url = fmt.Sprintf("http://%s", asset.Host)
			} else {
				url = fmt.Sprintf("http://%s:%d", asset.Host, asset.Port)
			}
		}
	}

	return url
}

// ==================== Nuclei模板同步（从前端上传） ====================

type NucleiTemplateSyncLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNucleiTemplateSyncLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NucleiTemplateSyncLogic {
	return &NucleiTemplateSyncLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NucleiTemplateSyncLogic) SyncFromUpload(req *types.NucleiTemplateSyncReq) (resp *types.NucleiTemplateSyncResp, err error) {
	if len(req.Templates) == 0 {
		return &types.NucleiTemplateSyncResp{Code: 400, Msg: "没有模板数据"}, nil
	}

	successCount := 0
	errorCount := 0

	for _, item := range req.Templates {
		if item.Content == "" {
			errorCount++
			continue
		}

		// 解析模板ID
		templateId := parseTemplateId(item.Content)
		if templateId == "" {
			errorCount++
			continue
		}

		// 解析模板信息
		templateInfo, err := parseTemplateContent(item.Content)
		if err != nil {
			errorCount++
			continue
		}

		// 从路径提取分类
		category := extractCategoryFromPath(item.Path)

		// 处理Author字段（可能是string或[]interface{}）
		author := parseAuthor(templateInfo.Author)

	// 构建模板文档
		doc := &model.NucleiTemplate{
			TemplateId:  templateId,
			Name:        templateInfo.Name,
			Author:      author,
			Severity:    template.NormalizeSeverity(templateInfo.Severity),
			Description: templateInfo.Description,
			Tags:        parseTemplateTags(templateInfo.Tags),
			Category:    category,
			Protocol:    template.ParseProtocol(item.Content),
			FilePath:    item.Path,
			Content:     item.Content,
			Enabled:     true,
		}

		// 提取厂商/产品（metadata.vendor / metadata.product）
		if meta, _ := template.ParseTemplateInfo(item.Content); meta != nil {
			doc.Vendor = meta.GetVendor()
			doc.Product = meta.GetProduct()
		}

		// 协议解析失败时回退用分类目录
		if doc.Protocol == "" {
			doc.Protocol = category
		}

		// 提取漏洞知识库信息
		if templateInfo.Classification != nil {
			doc.CvssScore = templateInfo.Classification.CvssScore
			doc.CvssMetrics = templateInfo.Classification.CvssMetrics
			doc.CveIds = parseCommaSeparated(templateInfo.Classification.CveId)
			doc.CweIds = parseCommaSeparated(templateInfo.Classification.CweId)
		}
		if len(templateInfo.Reference) > 0 {
			doc.References = templateInfo.Reference
		}
		if templateInfo.Remediation != "" {
			doc.Remediation = templateInfo.Remediation
		}

		// 保存到数据库（使用upsert）
		err = l.svcCtx.NucleiTemplateModel.Upsert(l.ctx, doc)
		if err != nil {
			errorCount++
			continue
		}
		successCount++
	}

	return &types.NucleiTemplateSyncResp{
		Code:         0,
		Msg:          fmt.Sprintf("导入完成，成功: %d, 失败: %d", successCount, errorCount),
		SuccessCount: successCount,
		ErrorCount:   errorCount,
	}, nil
}

// extractCategoryFromPath 从路径提取分类
func extractCategoryFromPath(path string) string {
	// 路径格式: nuclei-templates/http/cves/2021/CVE-2021-xxxx.yaml
	// 或: http/cves/2021/CVE-2021-xxxx.yaml
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "other"
	}

	// 跳过 nuclei-templates 前缀
	startIdx := 0
	for i, part := range parts {
		if part == "nuclei-templates" {
			startIdx = i + 1
			break
		}
	}

	if startIdx < len(parts) {
		return parts[startIdx]
	}
	return parts[0]
}

// parseTemplateId 从模板内容解析ID
func parseTemplateId(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
	}
	return ""
}

// parseTemplateContent 解析模板内容
func parseTemplateContent(content string) (*templateInfoWrapper, error) {
	var wrapper struct {
		Id   string               `yaml:"id"`
		Info *templateInfoWrapper `yaml:"info"`
	}
	if err := yaml.Unmarshal([]byte(content), &wrapper); err != nil {
		return nil, err
	}
	if wrapper.Info == nil {
		return &templateInfoWrapper{}, nil
	}
	return wrapper.Info, nil
}

// templateInfoWrapper 模板信息包装
type templateInfoWrapper struct {
	Name           string                  `yaml:"name"`
	Author         interface{}             `yaml:"author"` // 可能是string或[]string
	Severity       string                  `yaml:"severity"`
	Description    string                  `yaml:"description"`
	Reference      []string                `yaml:"reference"`
	Remediation    string                  `yaml:"remediation"`
	Classification *templateClassification `yaml:"classification"`
	Tags           string                  `yaml:"tags"`
}

type templateClassification struct {
	CvssMetrics string  `yaml:"cvss-metrics"`
	CvssScore   float64 `yaml:"cvss-score"`
	CveId       string  `yaml:"cve-id"`
	CweId       string  `yaml:"cwe-id"`
}

// parseTemplateTags 解析标签
func parseTemplateTags(tags string) []string {
	if tags == "" {
		return nil
	}
	var result []string
	for _, tag := range strings.Split(tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			result = append(result, tag)
		}
	}
	return result
}

// parseCommaSeparated 解析逗号分隔的字符串
func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// parseAuthor 解析作者字段（可能是string或[]interface{}）
func parseAuthor(author interface{}) string {
	if author == nil {
		return ""
	}
	switch v := author.(type) {
	case string:
		return v
	case []interface{}:
		var authors []string
		for _, a := range v {
			if s, ok := a.(string); ok {
				authors = append(authors, s)
			}
		}
		return strings.Join(authors, ", ")
	default:
		return fmt.Sprintf("%v", author)
	}
}

// checkOnlineWorkers 检查是否有在线 Worker
func checkOnlineWorkers(ctx context.Context, svcCtx *svc.ServiceContext) error {
	workers, err := svcCtx.RedisClient.SMembers(ctx, "cscan:workers").Result()
	if err != nil {
		return fmt.Errorf("获取Worker列表失败: %v", err)
	}
	for _, worker := range workers {
		exists, _ := svcCtx.RedisClient.Exists(ctx, "cscan:worker:"+worker).Result()
		if exists > 0 {
			return nil
		}
	}
	return fmt.Errorf("当前没有在线的扫描节点(Worker)，无法执行任务。请检查Worker服务状态。")
}

// persistTaskInfo 持久化任务信息到 Redis（24h TTL）
func persistTaskInfo(ctx context.Context, svcCtx *svc.ServiceContext, taskId string, taskConfig map[string]interface{}) {
	taskInfoKey := "cscan:task:info:" + taskId
	taskInfoData, _ := json.Marshal(taskConfig)
	if err := svcCtx.RedisClient.Set(ctx, taskInfoKey, taskInfoData, 24*time.Hour).Err(); err != nil {
		logx.Errorf("[TaskInfo] persist failed, taskId=%s, error=%v", taskId, err)
	}
}
