package notify

import (
	"net/http"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"cscan/pkg/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// NotifyConfigListHandler 通知配置列表
func NotifyConfigListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewNotifyConfigListLogic(r.Context(), svcCtx)
		resp, err := l.NotifyConfigList()
		if err != nil {
			response.Error(w, err)
		} else {
			response.Success(w, resp)
		}
	}
}

// NotifyConfigSaveHandler 保存通知配置
func NotifyConfigSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.NotifyConfigSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewNotifyConfigSaveLogic(r.Context(), svcCtx)
		resp, err := l.NotifyConfigSave(&req)
		if err != nil {
			response.Error(w, err)
		} else {
			response.Success(w, resp)
		}
	}
}

// NotifyConfigDeleteHandler 删除通知配置
func NotifyConfigDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.NotifyConfigDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewNotifyConfigDeleteLogic(r.Context(), svcCtx)
		resp, err := l.NotifyConfigDelete(&req)
		if err != nil {
			response.Error(w, err)
		} else {
			response.Success(w, resp)
		}
	}
}

// NotifyConfigTestHandler 测试通知配置
func NotifyConfigTestHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.NotifyConfigTestReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewNotifyConfigTestLogic(r.Context(), svcCtx)
		resp, err := l.NotifyConfigTest(&req)
		if err != nil {
			response.Error(w, err)
		} else {
			response.Success(w, resp)
		}
	}
}

// NotifyProviderListHandler 获取支持的通知提供者列表
func NotifyProviderListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewNotifyProviderListLogic(r.Context(), svcCtx)
		resp, err := l.NotifyProviderList()
		if err != nil {
			response.Error(w, err)
		} else {
			response.Success(w, resp)
		}
	}
}

// HighRiskFilterConfigGetHandler 获取高危过滤配置
func HighRiskFilterConfigGetHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewHighRiskFilterConfigGetLogic(r.Context(), svcCtx)
		resp, err := l.HighRiskFilterConfigGet()
		if err != nil {
			response.Error(w, err)
		} else {
			response.Success(w, resp)
		}
	}
}

// HighRiskFilterConfigSaveHandler 保存高危过滤配置
func HighRiskFilterConfigSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HighRiskFilterConfigSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewHighRiskFilterConfigSaveLogic(r.Context(), svcCtx)
		resp, err := l.HighRiskFilterConfigSave(&req)
		if err != nil {
			response.Error(w, err)
		} else {
			response.Success(w, resp)
		}
	}
}
