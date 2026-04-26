package weakpass

import (
	"net/http"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/pkg/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// WeakpassDictListHandler 弱口令字典列表
func WeakpassDictListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WeakpassDictListReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewWeakpassDictListLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictList(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictSaveHandler 保存弱口令字典
func WeakpassDictSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WeakpassDictSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewWeakpassDictSaveLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictSave(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictDeleteHandler 删除弱口令字典
func WeakpassDictDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WeakpassDictDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewWeakpassDictDeleteLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictDelete(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictClearHandler 清空弱口令字典
func WeakpassDictClearHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewWeakpassDictClearLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictClear()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictEnabledListHandler 获取启用的弱口令字典列表（用于任务创建时选择）
func WeakpassDictEnabledListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewWeakpassDictEnabledListLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictEnabledList()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictImportHandler 导入弱口令字典
func WeakpassDictImportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WeakpassDictImportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewWeakpassDictImportLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictImport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictExportHandler 导出弱口令字典
func WeakpassDictExportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WeakpassDictExportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewWeakpassDictExportLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictExport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictParseHandler 解析弱口令字典内容（预览用）
func WeakpassDictParseHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WeakpassDictParseReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewWeakpassDictParseLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictParse(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WeakpassDictServiceStatsHandler 获取服务类型统计
func WeakpassDictServiceStatsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewWeakpassDictServiceStatsLogic(r.Context(), svcCtx)
		resp, err := l.WeakpassDictServiceStats()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}
