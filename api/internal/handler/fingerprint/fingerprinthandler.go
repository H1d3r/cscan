package fingerprint

import (
	"net/http"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/pkg/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// FingerprintListHandler 指纹列表
func FingerprintListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintListReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintListLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintList(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintSaveHandler 保存指纹
func FingerprintSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintSaveLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintSave(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintDeleteHandler 删除指纹
func FingerprintDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintDeleteLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintDelete(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintCategoriesHandler 指纹分类
func FingerprintCategoriesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewFingerprintCategoriesLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintCategories()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintSyncHandler 同步指纹
func FingerprintSyncHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintSyncReq
		httpx.Parse(r, &req)

		if req.Force {
			svcCtx.FingerprintModel.DeleteBuiltin(r.Context())
		}

		go svcCtx.SyncWappalyzerFingerprints()
		httpx.OkJson(w, &types.BaseResp{Code: 0, Msg: "指纹同步已开始，请稍后刷新查看"})
	}
}

// FingerprintUpdateEnabledHandler 更新指纹启用状态
func FingerprintUpdateEnabledHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Id      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintUpdateEnabledLogic(r.Context(), svcCtx)
		resp, err := l.UpdateEnabled(req.Id, req.Enabled)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintBatchUpdateEnabledHandler 批量更新指纹启用状态
func FingerprintBatchUpdateEnabledHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Ids     []string `json:"ids"`     // 指定ID列表（可选）
			Enabled bool     `json:"enabled"` // 启用/禁用
			All     bool     `json:"all"`     // 是否操作全部自定义指纹
		}
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintBatchUpdateEnabledLogic(r.Context(), svcCtx)
		resp, err := l.BatchUpdateEnabled(req.Ids, req.Enabled, req.All)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintImportHandler 导入指纹
func FingerprintImportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintImportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintImportLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintImport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintImportFromFileHandler 从文件导入指纹
func FingerprintImportFromFileHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintImportFromFileReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintImportFromFileLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintImportFromFile(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintClearCustomHandler 清空自定义指纹
func FingerprintClearCustomHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintClearCustomReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintClearCustomLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintClearCustom(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintValidateHandler 验证指纹
func FingerprintValidateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintValidateReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintValidateLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintValidate(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintBatchValidateHandler 批量验证指纹
func FingerprintBatchValidateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintBatchValidateReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewFingerprintBatchValidateLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintBatchValidate(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceMappingListHandler HTTP服务映射列表
func HttpServiceMappingListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceMappingListReq
		httpx.Parse(r, &req)

		l := logic.NewHttpServiceMappingListLogic(r.Context(), svcCtx)
		resp, err := l.HttpServiceMappingList(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceMappingSaveHandler 保存HTTP服务映射
func HttpServiceMappingSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceMappingSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewHttpServiceMappingSaveLogic(r.Context(), svcCtx)
		resp, err := l.HttpServiceMappingSave(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceMappingDeleteHandler 删除HTTP服务映射
func HttpServiceMappingDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceMappingDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewHttpServiceMappingDeleteLogic(r.Context(), svcCtx)
		resp, err := l.HttpServiceMappingDelete(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ==================== HTTP服务设置 ====================

// HttpServiceConfigGetHandler 获取HTTP服务配置
func HttpServiceConfigGetHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewHttpServiceConfigGetLogic(r.Context(), svcCtx)
		resp, err := l.HttpServiceConfigGet()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceConfigSaveHandler 保存HTTP服务配置
func HttpServiceConfigSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceConfigSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewHttpServiceConfigSaveLogic(r.Context(), svcCtx)
		resp, err := l.HttpServiceConfigSave(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceMappingListV2Handler HTTP服务映射列表（使用新模型）
func HttpServiceMappingListV2Handler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceMappingListReq
		httpx.Parse(r, &req)

		l := logic.NewHttpServiceMappingListV2Logic(r.Context(), svcCtx)
		resp, err := l.HttpServiceMappingListV2(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceMappingSaveV2Handler 保存HTTP服务映射（使用新模型）
func HttpServiceMappingSaveV2Handler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceMappingSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewHttpServiceMappingSaveV2Logic(r.Context(), svcCtx)
		resp, err := l.HttpServiceMappingSaveV2(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceMappingDeleteV2Handler 删除HTTP服务映射（使用新模型）
func HttpServiceMappingDeleteV2Handler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceMappingDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewHttpServiceMappingDeleteV2Logic(r.Context(), svcCtx)
		resp, err := l.HttpServiceMappingDeleteV2(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceExportHandler 导出HTTP服务映射
func HttpServiceExportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceExportReq
		httpx.Parse(r, &req)

		l := logic.NewHttpServiceExportLogic(r.Context(), svcCtx)
		resp, err := l.HttpServiceExport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// HttpServiceImportHandler 导入HTTP服务映射
func HttpServiceImportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HttpServiceImportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewHttpServiceImportLogic(r.Context(), svcCtx)
		resp, err := l.HttpServiceImport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// FingerprintMatchAssetsHandler 验证指纹匹配现有资产
func FingerprintMatchAssetsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FingerprintMatchAssetsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		workspaceId := r.Header.Get("X-Workspace-Id")
		l := logic.NewFingerprintMatchAssetsLogic(r.Context(), svcCtx)
		resp, err := l.FingerprintMatchAssets(&req, workspaceId)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ==================== 主动扫描指纹 Handler ====================

// ActiveFingerprintListHandler 主动指纹列表
func ActiveFingerprintListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ActiveFingerprintListReq
		httpx.Parse(r, &req)

		l := logic.NewActiveFingerprintListLogic(r.Context(), svcCtx)
		resp, err := l.ActiveFingerprintList(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ActiveFingerprintSaveHandler 保存主动指纹
func ActiveFingerprintSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ActiveFingerprintSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewActiveFingerprintSaveLogic(r.Context(), svcCtx)
		resp, err := l.ActiveFingerprintSave(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ActiveFingerprintDeleteHandler 删除主动指纹
func ActiveFingerprintDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ActiveFingerprintDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewActiveFingerprintDeleteLogic(r.Context(), svcCtx)
		resp, err := l.ActiveFingerprintDelete(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ActiveFingerprintImportHandler 导入主动指纹（YAML格式）
func ActiveFingerprintImportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ActiveFingerprintImportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewActiveFingerprintImportLogic(r.Context(), svcCtx)
		resp, err := l.ActiveFingerprintImport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ActiveFingerprintExportHandler 导出主动指纹（YAML格式）
func ActiveFingerprintExportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewActiveFingerprintExportLogic(r.Context(), svcCtx)
		resp, err := l.ActiveFingerprintExport()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ActiveFingerprintClearHandler 清空主动指纹
func ActiveFingerprintClearHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewActiveFingerprintClearLogic(r.Context(), svcCtx)
		resp, err := l.ActiveFingerprintClear()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ActiveFingerprintValidateHandler 验证主动指纹
func ActiveFingerprintValidateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ActiveFingerprintValidateReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewActiveFingerprintValidateLogic(r.Context(), svcCtx)
		resp, err := l.ActiveFingerprintValidate(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}
