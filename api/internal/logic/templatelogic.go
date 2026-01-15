package logic

import (
	"context"
	"encoding/json"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// ==================== 模板列表 ====================

type ScanTemplateListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateListLogic {
	return &ScanTemplateListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateListLogic) ScanTemplateList(req *types.ScanTemplateListReq, userId string) (*types.ScanTemplateListResp, error) {
	templates, total, err := l.svcCtx.ScanTemplateModel.SearchTemplates(
		l.ctx,
		req.Keyword,
		req.Category,
		req.Tags,
		req.Page,
		req.PageSize,
	)
	if err != nil {
		return &types.ScanTemplateListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.ScanTemplate, 0, len(templates))
	for _, t := range templates {
		list = append(list, types.ScanTemplate{
			Id:          t.Id.Hex(),
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
			Tags:        t.Tags,
			Config:      t.Config,
			IsBuiltin:   t.IsBuiltin,
			UseCount:    t.UseCount,
			CreateTime:  t.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.ScanTemplateListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
}

// ==================== 保存模板 ====================

type ScanTemplateSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateSaveLogic {
	return &ScanTemplateSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateSaveLogic) ScanTemplateSave(req *types.ScanTemplateSaveReq, userId string) (*types.BaseRespWithId, error) {
	// 验证名称
	if req.Name == "" {
		return &types.BaseRespWithId{Code: 400, Msg: "模板名称不能为空"}, nil
	}

	// 验证配置JSON格式
	if req.Config != "" {
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(req.Config), &configMap); err != nil {
			return &types.BaseRespWithId{Code: 400, Msg: "配置格式无效"}, nil
		}
	}

	// 检查名称是否重复
	exists, err := l.svcCtx.ScanTemplateModel.ExistsName(l.ctx, req.Name, req.Id)
	if err != nil {
		return &types.BaseRespWithId{Code: 500, Msg: "检查名称失败"}, nil
	}
	if exists {
		return &types.BaseRespWithId{Code: 400, Msg: "模板名称已存在"}, nil
	}

	template := &model.ScanTemplate{
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Tags:        req.Tags,
		Config:      req.Config,
		SortNumber:  req.SortNumber,
	}

	if req.Id != "" {
		// 更新
		existing, err := l.svcCtx.ScanTemplateModel.FindById(l.ctx, req.Id)
		if err != nil {
			return &types.BaseRespWithId{Code: 400, Msg: "模板不存在"}, nil
		}

		// 内置模板不允许修改
		if existing.IsBuiltin {
			return &types.BaseRespWithId{Code: 403, Msg: "内置模板不允许修改"}, nil
		}

		err = l.svcCtx.ScanTemplateModel.Update(l.ctx, req.Id, template)
		if err != nil {
			return &types.BaseRespWithId{Code: 500, Msg: "更新失败"}, nil
		}
		return &types.BaseRespWithId{Code: 0, Msg: "更新成功", Id: req.Id}, nil
	}

	// 新增
	template.IsBuiltin = false
	err = l.svcCtx.ScanTemplateModel.Insert(l.ctx, template)
	if err != nil {
		return &types.BaseRespWithId{Code: 500, Msg: "创建失败"}, nil
	}

	return &types.BaseRespWithId{Code: 0, Msg: "创建成功", Id: template.Id.Hex()}, nil
}

// ==================== 删除模板 ====================

type ScanTemplateDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateDeleteLogic {
	return &ScanTemplateDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateDeleteLogic) ScanTemplateDelete(req *types.ScanTemplateDeleteReq, userId string) (*types.BaseResp, error) {
	template, err := l.svcCtx.ScanTemplateModel.FindById(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 400, Msg: "模板不存在"}, nil
	}

	// 内置模板不允许删除
	if template.IsBuiltin {
		return &types.BaseResp{Code: 403, Msg: "内置模板不允许删除"}, nil
	}

	err = l.svcCtx.ScanTemplateModel.DeleteById(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// ==================== 模板详情 ====================

type ScanTemplateDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateDetailLogic {
	return &ScanTemplateDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateDetailLogic) ScanTemplateDetail(req *types.ScanTemplateDetailReq) (*types.ScanTemplateDetailResp, error) {
	template, err := l.svcCtx.ScanTemplateModel.FindById(l.ctx, req.Id)
	if err != nil {
		return &types.ScanTemplateDetailResp{Code: 400, Msg: "模板不存在"}, nil
	}

	return &types.ScanTemplateDetailResp{
		Code: 0,
		Msg:  "success",
		Data: &types.ScanTemplate{
			Id:          template.Id.Hex(),
			Name:        template.Name,
			Description: template.Description,
			Category:    template.Category,
			Tags:        template.Tags,
			Config:      template.Config,
			IsBuiltin:   template.IsBuiltin,
			UseCount:    template.UseCount,
			CreateTime:  template.CreateTime.Local().Format("2006-01-02 15:04:05"),
		},
	}, nil
}

// ==================== 从任务创建模板 ====================

type ScanTemplateFromTaskLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateFromTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateFromTaskLogic {
	return &ScanTemplateFromTaskLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateFromTaskLogic) ScanTemplateFromTask(req *types.ScanTemplateFromTaskReq, userId, workspaceId string) (*types.BaseRespWithId, error) {
	// 获取任务
	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)
	task, err := taskModel.FindById(l.ctx, req.TaskId)
	if err != nil {
		return &types.BaseRespWithId{Code: 400, Msg: "任务不存在"}, nil
	}

	// 从任务配置中提取扫描配置（移除target等任务特定字段）
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		return &types.BaseRespWithId{Code: 500, Msg: "解析任务配置失败"}, nil
	}

	// 移除任务特定字段，只保留扫描配置
	delete(taskConfig, "target")
	delete(taskConfig, "orgId")
	delete(taskConfig, "workers")
	delete(taskConfig, "subTaskIndex")
	delete(taskConfig, "subTaskTotal")

	configBytes, _ := json.Marshal(taskConfig)

	// 检查名称是否重复
	exists, err := l.svcCtx.ScanTemplateModel.ExistsName(l.ctx, req.Name, "")
	if err != nil {
		return &types.BaseRespWithId{Code: 500, Msg: "检查名称失败"}, nil
	}
	if exists {
		return &types.BaseRespWithId{Code: 400, Msg: "模板名称已存在"}, nil
	}

	template := &model.ScanTemplate{
		Name:        req.Name,
		Description: req.Description,
		Category:    "custom",
		Tags:        []string{"from-task"},
		Config:      string(configBytes),
		IsBuiltin:   false,
	}

	err = l.svcCtx.ScanTemplateModel.Insert(l.ctx, template)
	if err != nil {
		return &types.BaseRespWithId{Code: 500, Msg: "创建模板失败"}, nil
	}

	return &types.BaseRespWithId{Code: 0, Msg: "模板创建成功", Id: template.Id.Hex()}, nil
}

// ==================== 获取分类 ====================

type ScanTemplateCategoriesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateCategoriesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateCategoriesLogic {
	return &ScanTemplateCategoriesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateCategoriesLogic) ScanTemplateCategories() (*types.ScanTemplateCategoriesResp, error) {
	categories, err := l.svcCtx.ScanTemplateModel.GetCategories(l.ctx)
	if err != nil {
		return &types.ScanTemplateCategoriesResp{Code: 500, Msg: "获取分类失败"}, nil
	}

	tags, err := l.svcCtx.ScanTemplateModel.GetAllTags(l.ctx)
	if err != nil {
		tags = []string{}
	}

	// 添加预定义分类
	predefinedCategories := []string{"quick", "standard", "full", "custom"}
	categoryMap := make(map[string]bool)
	for _, c := range categories {
		categoryMap[c] = true
	}
	for _, c := range predefinedCategories {
		if !categoryMap[c] {
			categories = append(categories, c)
		}
	}

	return &types.ScanTemplateCategoriesResp{
		Code:       0,
		Msg:        "success",
		Categories: categories,
		Tags:       tags,
	}, nil
}
