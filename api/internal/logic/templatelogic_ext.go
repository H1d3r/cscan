package logic

import (
	"context"
	"encoding/json"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// ==================== 导出模板 ====================

type ScanTemplateExportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateExportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateExportLogic {
	return &ScanTemplateExportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateExportLogic) ScanTemplateExport(req *types.ScanTemplateExportReq) (*types.ScanTemplateExportResp, error) {
	var templates []model.ScanTemplate
	var err error

	if len(req.Ids) > 0 {
		// 导出指定模板
		for _, id := range req.Ids {
			t, err := l.svcCtx.ScanTemplateModel.FindById(l.ctx, id)
			if err == nil {
				templates = append(templates, *t)
			}
		}
	} else {
		// 导出所有模板
		templates, err = l.svcCtx.ScanTemplateModel.FindAll(l.ctx)
		if err != nil {
			return &types.ScanTemplateExportResp{Code: 500, Msg: "查询模板失败"}, nil
		}
	}

	// 转换为导出格式
	exportData := make([]types.ScanTemplateExportItem, 0, len(templates))
	for _, t := range templates {
		exportData = append(exportData, types.ScanTemplateExportItem{
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
			Tags:        t.Tags,
			Config:      t.Config,
		})
	}

	dataBytes, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return &types.ScanTemplateExportResp{Code: 500, Msg: "序列化失败"}, nil
	}

	return &types.ScanTemplateExportResp{
		Code: 0,
		Msg:  "success",
		Data: string(dataBytes),
	}, nil
}

// ==================== 导入模板 ====================

type ScanTemplateImportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateImportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateImportLogic {
	return &ScanTemplateImportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateImportLogic) ScanTemplateImport(req *types.ScanTemplateImportReq, userId string) (*types.ScanTemplateImportResp, error) {
	var importData []types.ScanTemplateExportItem
	if err := json.Unmarshal([]byte(req.Data), &importData); err != nil {
		return &types.ScanTemplateImportResp{Code: 400, Msg: "数据格式无效"}, nil
	}

	imported := 0
	skipped := 0
	errors := []string{}

	for _, item := range importData {
		// 检查名称是否存在
		if req.SkipExisting {
			exists, _ := l.svcCtx.ScanTemplateModel.ExistsName(l.ctx, item.Name, "")
			if exists {
				skipped++
				continue
			}
		}

		// 验证配置格式
		if item.Config != "" {
			var configMap map[string]interface{}
			if err := json.Unmarshal([]byte(item.Config), &configMap); err != nil {
				errors = append(errors, "模板 "+item.Name+" 配置格式无效")
				continue
			}
		}

		template := &model.ScanTemplate{
			Name:        item.Name,
			Description: item.Description,
			Category:    item.Category,
			Tags:        item.Tags,
			Config:      item.Config,
			IsBuiltin:   false,
		}

		// 如果名称已存在且不跳过，则更新
		if !req.SkipExisting {
			exists, _ := l.svcCtx.ScanTemplateModel.ExistsName(l.ctx, item.Name, "")
			if exists {
				// 查找并更新
				templates, _, _ := l.svcCtx.ScanTemplateModel.SearchTemplates(l.ctx, item.Name, "", nil, 1, 1)
				if len(templates) > 0 {
					l.svcCtx.ScanTemplateModel.Update(l.ctx, templates[0].Id.Hex(), template)
					imported++
					continue
				}
			}
		}

		err := l.svcCtx.ScanTemplateModel.Insert(l.ctx, template)
		if err != nil {
			errors = append(errors, "模板 "+item.Name+" 导入失败: "+err.Error())
			continue
		}
		imported++
	}

	return &types.ScanTemplateImportResp{
		Code:     0,
		Msg:      "导入完成",
		Imported: imported,
		Skipped:  skipped,
		Errors:   errors,
	}, nil
}

// ==================== 使用模板 ====================

type ScanTemplateUseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScanTemplateUseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanTemplateUseLogic {
	return &ScanTemplateUseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ScanTemplateUseLogic) ScanTemplateUse(req *types.ScanTemplateUseReq) (*types.ScanTemplateDetailResp, error) {
	template, err := l.svcCtx.ScanTemplateModel.FindById(l.ctx, req.Id)
	if err != nil {
		return &types.ScanTemplateDetailResp{Code: 400, Msg: "模板不存在"}, nil
	}

	// 增加使用计数
	l.svcCtx.ScanTemplateModel.IncrUseCount(l.ctx, req.Id)

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
			UseCount:    template.UseCount + 1,
			CreateTime:  template.CreateTime.Local().Format("2006-01-02 15:04:05"),
		},
	}, nil
}
