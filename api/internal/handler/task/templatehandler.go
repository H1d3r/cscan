package task

import (
	"net/http"

	"cscan/api/internal/logic"
	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/pkg/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// ScanTemplateListHandler 模板列表
func ScanTemplateListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateListReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		userId := middleware.GetUserId(r.Context())
		l := logic.NewScanTemplateListLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateList(&req, userId)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateSaveHandler 保存模板
func ScanTemplateSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		userId := middleware.GetUserId(r.Context())
		l := logic.NewScanTemplateSaveLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateSave(&req, userId)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateDeleteHandler 删除模板
func ScanTemplateDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		userId := middleware.GetUserId(r.Context())
		l := logic.NewScanTemplateDeleteLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateDelete(&req, userId)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateDetailHandler 模板详情
func ScanTemplateDetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateDetailReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewScanTemplateDetailLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateDetail(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateFromTaskHandler 从任务创建模板
func ScanTemplateFromTaskHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateFromTaskReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		userId := middleware.GetUserId(r.Context())
		workspaceId := middleware.GetWorkspaceId(r.Context())
		l := logic.NewScanTemplateFromTaskLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateFromTask(&req, userId, workspaceId)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateCategoriesHandler 获取模板分类
func ScanTemplateCategoriesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewScanTemplateCategoriesLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateCategories()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateExportHandler 导出模板
func ScanTemplateExportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateExportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewScanTemplateExportLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateExport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateImportHandler 导入模板
func ScanTemplateImportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateImportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		userId := middleware.GetUserId(r.Context())
		l := logic.NewScanTemplateImportLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateImport(&req, userId)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScanTemplateUseHandler 使用模板（增加使用计数）
func ScanTemplateUseHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScanTemplateUseReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewScanTemplateUseLogic(r.Context(), svcCtx)
		resp, err := l.ScanTemplateUse(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}
