package logic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"
	"cscan/rpc/task/pb"
	"cscan/scanner"

	"github.com/projectdiscovery/nuclei/v3/pkg/installer"
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

func (l *CustomPocListLogic) CustomPocList(req *types.CustomPocListReq) (resp *types.CustomPocListResp, err error) {
	// 构建筛选条件
	filter := bson.M{}
	if req.Name != "" {
		filter["name"] = bson.M{"$regex": req.Name, "$options": "i"}
	}
	if req.TemplateId != "" {
		filter["template_id"] = bson.M{"$regex": req.TemplateId, "$options": "i"}
	}
	if req.Severity != "" {
		filter["severity"] = req.Severity
	}
	if req.Tag != "" {
		filter["tags"] = bson.M{"$in": []string{req.Tag}}
	}
	if req.Enabled != nil {
		filter["enabled"] = *req.Enabled
	}

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

func (l *NucleiTemplateListLogic) NucleiTemplateList(req *types.NucleiTemplateListReq) (resp *types.NucleiTemplateListResp, err error) {
	// 构建查询条件
	filter := bson.M{}
	if req.Category != "" {
		filter["category"] = req.Category
	}
	if req.Severity != "" {
		filter["severity"] = strings.ToLower(req.Severity)
	}
	if req.Tag != "" {
		// 标签模糊匹配
		filter["tags"] = bson.M{"$regex": req.Tag, "$options": "i"}
	}
	if req.Keyword != "" {
		// 使用正则表达式进行模糊搜索
		filter["$or"] = []bson.M{
			{"template_id": bson.M{"$regex": req.Keyword, "$options": "i"}},
			{"name": bson.M{"$regex": req.Keyword, "$options": "i"}},
			{"description": bson.M{"$regex": req.Keyword, "$options": "i"}},
		}
	}
	// 新增 - CVSS评分筛选
	if req.MinCvssScore > 0 {
		filter["cvss_score"] = bson.M{"$gte": req.MinCvssScore}
	}
	// 新增 - CVE编号搜索
	if req.CveId != "" {
		filter["cve_ids"] = bson.M{"$regex": req.CveId, "$options": "i"}
	}

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

func (l *NucleiTemplateCategoriesLogic) NucleiTemplateCategories() (resp *types.NucleiTemplateCategoriesResp, err error) {
	// 直接从数据库查询，不使用缓存
	categories, err := l.svcCtx.NucleiTemplateModel.GetCategories(l.ctx)
	if err != nil {
		categories = []string{}
	}

	tags, err := l.svcCtx.NucleiTemplateModel.GetTags(l.ctx, 100)
	if err != nil {
		tags = []string{}
	}

	stats, err := l.svcCtx.NucleiTemplateModel.GetStats(l.ctx)
	if err != nil {
		stats = map[string]int{"total": 0}
	}

	severities := []string{"critical", "high", "medium", "low", "info", "unknown"}

	return &types.NucleiTemplateCategoriesResp{
		Code:       0,
		Msg:        "success",
		Categories: categories,
		Severities: severities,
		Tags:       tags,
		Stats:      stats,
	}, nil
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

func (l *PocValidateLogic) PocValidate(req *types.PocValidateReq, workspaceId string) (resp *types.PocValidateResp, err error) {
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
		// Nuclei默认模板
		template, err := l.svcCtx.NucleiTemplateModel.FindByTemplateId(l.ctx, req.Id)
		if err != nil {
			return &types.PocValidateResp{Code: 404, Msg: "Nuclei模板不存在"}, nil
		}
		pocSeverity = template.Severity
	} else {
		// 自定义POC
		poc, err := l.svcCtx.CustomPocModel.FindById(l.ctx, req.Id)
		if err != nil {
			return &types.PocValidateResp{Code: 404, Msg: "POC不存在"}, nil
		}
		pocSeverity = poc.Severity
	}

	// 通过RPC调用worker执行POC验证
	rpcReq := &pb.ValidatePocReq{
		Url:         req.Url,
		PocId:       req.Id,
		PocType:     pocType,
		Timeout:     30,
		UseTemplate: pocType == "nuclei",
		UseCustom:   pocType == "custom",
		WorkspaceId: workspaceId,
	}

	rpcResp, err := l.svcCtx.TaskRpcClient.ValidatePoc(l.ctx, rpcReq)
	if err != nil {
		l.Logger.Errorf("RPC call failed: %v", err)
		return &types.PocValidateResp{Code: 500, Msg: "验证服务调用失败"}, nil
	}

	if !rpcResp.Success {
		return &types.PocValidateResp{Code: 500, Msg: rpcResp.Message}, nil
	}

	// 异步模式：返回任务已下发的信息和任务ID
	return &types.PocValidateResp{
		Code:     0,
		Msg:      "POC验证任务已下发，请稍后查询结果",
		Matched:  false, // 异步模式下无法立即返回匹配结果
		Severity: pocSeverity,
		Details:  rpcResp.Details,
		TaskId:   rpcResp.TaskId,
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

func (l *PocBatchValidateLogic) PocBatchValidate(req *types.PocBatchValidateReq, workspaceId string) (resp *types.PocBatchValidateResp, err error) {
	if len(req.Urls) == 0 {
		return &types.PocBatchValidateResp{Code: 400, Msg: "URL列表不能为空"}, nil
	}

	// 设置默认值
	if req.PocType == "" {
		req.PocType = "all"
	}
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 10
	}
	if req.UseTemplate == false && req.UseCustom == false {
		req.UseTemplate = true
		req.UseCustom = true
	}

	// 通过RPC调用worker执行批量POC验证
	rpcReq := &pb.BatchValidatePocReq{
		Urls:        req.Urls,
		PocType:     req.PocType,
		Severities:  req.Severities,
		Tags:        req.Tags,
		Timeout:     int32(req.Timeout),
		UseTemplate: req.UseTemplate,
		UseCustom:   req.UseCustom,
		Concurrency: int32(req.Concurrency),
		WorkspaceId: workspaceId,
	}

	rpcResp, err := l.svcCtx.TaskRpcClient.BatchValidatePoc(l.ctx, rpcReq)
	if err != nil {
		l.Logger.Errorf("RPC call failed: %v", err)
		return &types.PocBatchValidateResp{Code: 500, Msg: "验证服务调用失败"}, nil
	}

	if !rpcResp.Success {
		return &types.PocBatchValidateResp{Code: 500, Msg: rpcResp.Message}, nil
	}

	// 从RPC响应中获取批次ID
	batchId := rpcResp.BatchId

	return &types.PocBatchValidateResp{
		Code:      0,
		Msg:       "批量验证任务已下发，请使用返回的批次ID查询结果",
		TotalUrls: int(rpcResp.TotalUrls),
		Duration:  rpcResp.Duration,
		BatchId:   batchId,
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

	// 通过RPC查询验证结果
	rpcReq := &pb.GetPocValidationResultReq{
		TaskId:  req.TaskId,
		BatchId: req.BatchId,
	}

	rpcResp, err := l.svcCtx.TaskRpcClient.GetPocValidationResult(l.ctx, rpcReq)
	if err != nil {
		l.Logger.Errorf("RPC call failed: %v", err)
		return &types.PocValidationResultQueryResp{Code: 500, Msg: "查询服务调用失败"}, nil
	}

	if !rpcResp.Success {
		return &types.PocValidationResultQueryResp{Code: 500, Msg: rpcResp.Message}, nil
	}

	// 转换结果
	results := make([]types.PocValidationResult, 0, len(rpcResp.Results))
	for _, r := range rpcResp.Results {
		results = append(results, types.PocValidationResult{
			PocId:      r.PocId,
			PocName:    r.PocName,
			TemplateId: r.TemplateId,
			Severity:   r.Severity,
			Matched:    r.Matched,
			MatchedUrl: r.MatchedUrl,
			Details:    r.Details,
			Output:     r.Output,
			PocType:    r.PocType,
			Tags:       r.Tags,
		})
	}

	return &types.PocValidationResultQueryResp{
		Code:           0,
		Msg:            "查询成功",
		Status:         rpcResp.Status,
		CompletedCount: int(rpcResp.CompletedCount),
		TotalCount:     int(rpcResp.TotalCount),
		Results:        results,
		CreateTime:     rpcResp.CreateTime,
		UpdateTime:     rpcResp.UpdateTime,
	}, nil
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
	// 构建筛选条件
	filter := bson.M{}
	if req.Name != "" {
		filter["name"] = bson.M{"$regex": req.Name, "$options": "i"}
	}
	if req.TemplateId != "" {
		filter["template_id"] = bson.M{"$regex": req.TemplateId, "$options": "i"}
	}
	if req.Severity != "" {
		filter["severity"] = req.Severity
	}
	if req.Tag != "" {
		filter["tags"] = bson.M{"$in": []string{req.Tag}}
	}
	if req.Enabled != nil {
		filter["enabled"] = *req.Enabled
	}

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

func (l *CustomPocScanAssetsLogic) CustomPocScanAssets(req *types.CustomPocScanAssetsReq, workspaceId string) (*types.CustomPocScanAssetsResp, error) {
	if req.PocId == "" {
		return &types.CustomPocScanAssetsResp{Code: 400, Msg: "POC ID不能为空"}, nil
	}

	// 获取POC
	poc, err := l.svcCtx.CustomPocModel.FindById(l.ctx, req.PocId)
	if err != nil {
		return &types.CustomPocScanAssetsResp{Code: 404, Msg: "POC不存在"}, nil
	}

	// 常见HTTP端口
	httpPorts := []int{80, 8080, 8000, 8888, 8081, 8082, 8008, 9000, 9080, 3000, 5000}
	httpsPorts := []int{443, 8443, 9443, 4443}
	allHttpPorts := append(httpPorts, httpsPorts...)

	// 获取所有HTTP资产（扩展过滤条件），支持多工作空间
	wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)
	filter := bson.M{
		"$or": []bson.M{
			{"is_http": true}, // is_http 标记为 true
			{"service": bson.M{"$in": []string{"http", "https", "http-proxy"}}}, // service 为 http/https
			{"port": bson.M{"$in": allHttpPorts}},                               // 常见 HTTP 端口
			{"title": bson.M{"$exists": true, "$ne": ""}},                       // 有 title（说明是 HTTP 服务）
			{"authority": bson.M{"$regex": "^https?://", "$options": "i"}},      // authority 以 http:// 或 https:// 开头
		},
	}

	var assets []model.Asset
	for _, wsId := range wsIds {
		assetModel := l.svcCtx.GetAssetModel(wsId)
		wsAssets, err := assetModel.Find(l.ctx, filter, 0, 0)
		if err != nil {
			l.Logger.Errorf("查询工作空间 %s 资产失败: %v", wsId, err)
			continue
		}
		assets = append(assets, wsAssets...)
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

	// 创建一个批量扫描任务（使用批量模式）
	rpcReq := &pb.ValidatePocReq{
		PocId:       req.PocId,
		PocType:     "custom",
		Timeout:     int32(len(urls) * 30), // 每个目标30秒
		UseTemplate: false,
		UseCustom:   true,
		WorkspaceId: workspaceId,
		Urls:        urls,
		BatchMode:   true,
	}

	resp, err := l.svcCtx.TaskRpcClient.ValidatePoc(l.ctx, rpcReq)
	if err != nil {
		l.Logger.Errorf("Failed to create batch scan task: %v", err)
		return &types.CustomPocScanAssetsResp{Code: 500, Msg: "创建扫描任务失败: " + err.Error()}, nil
	}

	if !resp.Success {
		return &types.CustomPocScanAssetsResp{Code: 500, Msg: resp.Message}, nil
	}

	msg := fmt.Sprintf("已创建批量扫描任务（POC: %s，目标: %d个），发现的漏洞将显示在漏洞页面", poc.Name, len(urls))

	return &types.CustomPocScanAssetsResp{
		Code:         0,
		Msg:          msg,
		TotalScanned: len(urls),
		VulnCount:    0,
		Duration:     "异步执行中",
		VulnList:     []types.CustomPocScanVulnItem{},
		TaskIds:      []string{resp.TaskId},
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
			Severity:    strings.ToLower(templateInfo.Severity),
			Description: templateInfo.Description,
			Tags:        parseTemplateTags(templateInfo.Tags),
			Category:    category,
			FilePath:    item.Path,
			Content:     item.Content,
			Enabled:     true,
		}

		// 设置默认severity
		if doc.Severity == "" {
			doc.Severity = "unknown"
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

// ==================== Nuclei模板库下载 ====================

// 下载任务状态
type DownloadTaskStatus struct {
	Status        string // pending/downloading/completed/failed
	Progress      int    // 进度百分比 0-100
	TemplateCount int    // 已下载模板数量
	Error         string // 错误信息
	StartTime     time.Time
}

// 全局下载任务状态存储
var (
	downloadTasks   = make(map[string]*DownloadTaskStatus)
	downloadTasksMu sync.RWMutex
)

type NucleiTemplateDownloadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewNucleiTemplateDownloadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NucleiTemplateDownloadLogic {
	return &NucleiTemplateDownloadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *NucleiTemplateDownloadLogic) DownloadTemplates(req *types.NucleiTemplateDownloadReq) (resp *types.NucleiTemplateDownloadResp, err error) {
	// 生成任务ID
	taskId := fmt.Sprintf("dl_%d", time.Now().UnixNano())

	// 初始化任务状态
	downloadTasksMu.Lock()
	downloadTasks[taskId] = &DownloadTaskStatus{
		Status:    "pending",
		Progress:  0,
		StartTime: time.Now(),
	}
	downloadTasksMu.Unlock()

	// 异步执行下载任务
	go l.downloadTemplatesWithProgress(taskId, req.Force)

	return &types.NucleiTemplateDownloadResp{
		Code:   0,
		Msg:    "模板库下载任务已启动",
		TaskId: taskId,
	}, nil
}

// GetDownloadStatus 获取下载状态
func GetDownloadStatus(taskId string) *DownloadTaskStatus {
	downloadTasksMu.RLock()
	defer downloadTasksMu.RUnlock()
	if status, ok := downloadTasks[taskId]; ok {
		return status
	}
	return nil
}

// updateDownloadStatus 更新下载状态
func updateDownloadStatus(taskId string, status string, progress int, templateCount int, errMsg string) {
	downloadTasksMu.Lock()
	defer downloadTasksMu.Unlock()
	if task, ok := downloadTasks[taskId]; ok {
		task.Status = status
		task.Progress = progress
		task.TemplateCount = templateCount
		task.Error = errMsg
	}
}

func (l *NucleiTemplateDownloadLogic) downloadTemplatesWithProgress(taskId string, force bool) {
	logx.Infof("[Nuclei Templates] Starting download task %s, force=%v", taskId, force)

	// 更新状态为下载中
	updateDownloadStatus(taskId, "downloading", 5, 0, "")

	// 获取数据目录
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}
	lockFile := dataDir + "/.nuclei-templates-initialized"

	// Nuclei 默认模板目录
	homeDir, _ := os.UserHomeDir()
	templatesDir := filepath.Join(homeDir, "nuclei-templates")

	// 检查是否已初始化（非强制模式下）
	if !force {
		if _, err := os.Stat(lockFile); err == nil {
			if entries, err := os.ReadDir(templatesDir); err == nil && len(entries) > 0 {
				templateCount := l.countTemplates(templatesDir)
				logx.Info("[Nuclei Templates] Template library already initialized")
				updateDownloadStatus(taskId, "completed", 100, templateCount, "")
				return
			}
		}
	}

	// 强制更新时，删除锁文件
	if force {
		updateDownloadStatus(taskId, "downloading", 10, 0, "")
		logx.Info("[Nuclei Templates] Force update requested, removing lock file...")
		_ = os.Remove(lockFile)
		// 强制更新时也删除模板目录
		_ = os.RemoveAll(templatesDir)
	}

	// 确保数据目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logx.Errorf("[Nuclei Templates] Failed to create data directory: %v", err)
		updateDownloadStatus(taskId, "failed", 0, 0, "创建数据目录失败: "+err.Error())
		return
	}

	updateDownloadStatus(taskId, "downloading", 10, 0, "")

	// 检查模板目录是否已存在且有内容
	if entries, err := os.ReadDir(templatesDir); err == nil && len(entries) > 0 {
		templateCount := l.countTemplates(templatesDir)
		if templateCount > 0 {
			logx.Infof("[Nuclei Templates] Templates already exist: %d files", templateCount)
			updateDownloadStatus(taskId, "completed", 100, templateCount, "")
			return
		}
	}

	// 启动进度模拟
	done := make(chan bool)
	go func() {
		progress := 10
		ticker := time.NewTicker(800 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if progress < 85 {
					progress += 3
					count := l.countTemplates(templatesDir)
					updateDownloadStatus(taskId, "downloading", progress, count, "")
				}
			}
		}
	}()

	// 尝试多种下载方式
	var downloadErr error

	// 方式1: 尝试使用 Nuclei SDK
	logx.Info("[Nuclei Templates] Trying Nuclei SDK...")
	updateDownloadStatus(taskId, "downloading", 15, 0, "")

	installer.HideProgressBar = true
	installer.HideUpdateChangesTable = true
	installer.HideReleaseNotes = true

	tm := &installer.TemplateManager{
		DisablePublicTemplates: false,
	}

	downloadErr = tm.FreshInstallIfNotExists()

	// 如果 SDK 失败，尝试使用 git clone
	if downloadErr != nil {
		logx.Errorf("[Nuclei Templates] SDK failed: %v, trying git clone...", downloadErr)
		updateDownloadStatus(taskId, "downloading", 25, 0, "")

		// 清理可能的部分下载
		_ = os.RemoveAll(templatesDir)

		// 尝试多个镜像源
		mirrors := []string{
			"https://github.com/projectdiscovery/nuclei-templates.git",
			"https://ghproxy.com/https://github.com/projectdiscovery/nuclei-templates.git",
			"https://mirror.ghproxy.com/https://github.com/projectdiscovery/nuclei-templates.git",
		}

		for i, mirror := range mirrors {
			logx.Infof("[Nuclei Templates] Trying mirror %d: %s", i+1, mirror)
			updateDownloadStatus(taskId, "downloading", 30+i*15, 0, "")

			// 每次尝试前清理目录
			_ = os.RemoveAll(templatesDir)

			cmd := exec.Command("git", "clone", "--depth", "1", mirror, templatesDir)
			cmd.Env = append(os.Environ(),
				"GIT_SSL_NO_VERIFY=true",
				"GIT_TERMINAL_PROMPT=0",
			)

			output, err := cmd.CombinedOutput()
			if err == nil {
				logx.Infof("[Nuclei Templates] Successfully cloned from %s", mirror)
				downloadErr = nil
				break
			}
			logx.Infof("[Nuclei Templates] Mirror %s failed: %v, output: %s", mirror, err, string(output))
			downloadErr = fmt.Errorf("git clone failed: %v", err)
		}
	}

	// 方式3: 如果网络下载都失败，尝试使用 Docker 内置的兜底模板
	if downloadErr != nil {
		builtinDir := "/app/nuclei-templates-builtin"
		if info, err := os.Stat(builtinDir); err == nil && info.IsDir() {
			logx.Info("[Nuclei Templates] Using builtin templates as fallback...")
			updateDownloadStatus(taskId, "downloading", 80, 0, "")

			// 复制内置模板到目标目录
			cmd := exec.Command("cp", "-r", builtinDir, templatesDir)
			if output, err := cmd.CombinedOutput(); err == nil {
				logx.Info("[Nuclei Templates] Successfully copied builtin templates")
				downloadErr = nil
			} else {
				logx.Errorf("[Nuclei Templates] Failed to copy builtin templates: %v, output: %s", err, string(output))
			}
		}
	}

	close(done)

	if downloadErr != nil {
		logx.Errorf("[Nuclei Templates] All download methods failed: %v", downloadErr)
		updateDownloadStatus(taskId, "failed", 0, 0, "下载失败，请使用上传ZIP包方式导入模板")
		return
	}

	// 统计模板数量
	templateCount := l.countTemplates(templatesDir)
	logx.Infof("[Nuclei Templates] Found %d template files", templateCount)

	if templateCount == 0 {
		updateDownloadStatus(taskId, "failed", 0, 0, "下载完成但未找到模板文件")
		return
	}

	// 创建锁文件
	lockContent := fmt.Sprintf("initialized_at=%s\nsource=nuclei-templates\ntemplate_count=%d\n",
		time.Now().UTC().Format(time.RFC3339), templateCount)
	_ = os.WriteFile(lockFile, []byte(lockContent), 0644)

	updateDownloadStatus(taskId, "completed", 100, templateCount, "")
	logx.Info("[Nuclei Templates] Download completed successfully")

	// 5分钟后清理任务状态
	go func() {
		time.Sleep(5 * time.Minute)
		downloadTasksMu.Lock()
		delete(downloadTasks, taskId)
		downloadTasksMu.Unlock()
	}()
}

// countTemplates 统计模板文件数量
func (l *NucleiTemplateDownloadLogic) countTemplates(dir string) int {
	count := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".yaml" || ext == ".yml" {
				count++
			}
		}
		return nil
	})
	return count
}
