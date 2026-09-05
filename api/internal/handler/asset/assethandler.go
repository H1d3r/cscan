package asset

import (
	"net/http"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/pkg/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// AssetListHandler 资产列表
func AssetListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetListReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetListLogic(r.Context(), svcCtx)
		resp, err := l.AssetList(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetStatHandler 资产统计
func AssetStatHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetStatReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetStatLogic(r.Context(), svcCtx)
		resp, err := l.AssetStat(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetDeleteHandler 删除资产
func AssetDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetDeleteLogic(r.Context(), svcCtx)
		resp, err := l.AssetDelete(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetBatchDeleteHandler 批量删除资产
func AssetBatchDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetBatchDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetBatchDeleteLogic(r.Context(), svcCtx)
		resp, err := l.AssetBatchDelete(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetClearHandler 清空资产
func AssetClearHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewAssetClearLogic(r.Context(), svcCtx)
		resp, err := l.AssetClear()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetImportHandler 导入资产
func AssetImportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetImportReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetImportLogic(r.Context(), svcCtx)
		resp, err := l.AssetImport(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetSaveHandler 手动添加资产
func AssetSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetSaveLogic(r.Context(), svcCtx)
		resp, err := l.AssetSave(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetFingerprintsListHandler 获取资产中已识别的指纹列表
func AssetFingerprintsListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetFingerprintsListReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetFingerprintsListLogic(r.Context(), svcCtx)
		resp, err := l.AssetFingerprintsList(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetPortsStatsHandler 获取资产端口统计
func AssetPortsStatsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewAssetPortsStatsLogic(r.Context(), svcCtx)
		resp, err := l.AssetPortsStats()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetGroupsHandler 资产分组
func AssetGroupsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetGroupsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetGroupsLogic(r.Context(), svcCtx)
		resp, err := l.AssetGroups(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetInventoryHandler 资产清单
func AssetInventoryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetInventoryReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetInventoryLogic(r.Context(), svcCtx)
		resp, err := l.AssetInventory(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScreenshotsHandler 截图清单
func ScreenshotsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ScreenshotsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewScreenshotsLogic(r.Context(), svcCtx)
		resp, err := l.Screenshots(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetUpdateLabelsHandler 更新资产标签
func AssetUpdateLabelsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetUpdateLabelsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetUpdateLabelsLogic(r.Context(), svcCtx)
		resp, err := l.AssetUpdateLabels(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetAddLabelHandler 添加资产标签
func AssetAddLabelHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetAddLabelReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetAddLabelLogic(r.Context(), svcCtx)
		resp, err := l.AssetAddLabel(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetRemoveLabelHandler 删除资产标签
func AssetRemoveLabelHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetRemoveLabelReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetRemoveLabelLogic(r.Context(), svcCtx)
		resp, err := l.AssetRemoveLabel(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetFilterOptionsHandler 获取资产过滤器选项
func AssetFilterOptionsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetFilterOptionsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetFilterOptionsLogic(r.Context(), svcCtx)
		resp, err := l.AssetFilterOptions(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetDetailHandler 资产详情（按需加载完整资产，含 body/header/banner）
func AssetDetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetDetailReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetDetailLogic(r.Context(), svcCtx)
		resp, err := l.AssetDetail(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// DeleteAssetGroupHandler 删除资产分组
func DeleteAssetGroupHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DeleteAssetGroupReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewDeleteAssetGroupLogic(r.Context(), svcCtx)
		resp, err := l.DeleteAssetGroup(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetExposuresHandler 获取资产暴露面（目录扫描和漏洞扫描结果）
func AssetExposuresHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetExposuresReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		l := logic.NewAssetExposuresLogic(r.Context(), svcCtx)
		resp, err := l.AssetExposures(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetTargetListHandler 顶层资产列表（IP/主域名）
func AssetTargetListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetListReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetListLogic(r.Context(), svcCtx)
		resp, err := l.AssetTargetList(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetTargetDetailHandler 顶层资产详情（meta + exposure + risk）
func AssetTargetDetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetDetailReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetDetailLogic(r.Context(), svcCtx)
		resp, err := l.AssetTargetDetail(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetTargetAssetsHandler 获取目标下的资产列表
func AssetTargetAssetsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetAssetsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetAssetsLogic(r.Context(), svcCtx)
		resp, err := l.AssetTargetAssets(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetMediaHandler 按资产 ID 批量获取截图/favicon（列表懒加载）
func AssetMediaHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetMediaReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetMediaLogic(r.Context(), svcCtx)
		resp, err := l.AssetMedia(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetTargetUpdateHandler 更新顶层资产用户字段
func AssetTargetUpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetUpdateReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetUpdateLogic(r.Context(), svcCtx)
		if err := l.AssetTargetUpdate(&req); err != nil {
			response.Error(w, err)
			return
		}
		response.SuccessWithMsg(w, "success")
	}
}

// AssetTargetDeleteHandler 删除顶层资产
func AssetTargetDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetDeleteLogic(r.Context(), svcCtx)
		resp, err := l.AssetTargetDelete(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetTargetRediscoverHandler 重新发现目标（重放该目标最近一次扫描任务）
func AssetTargetRediscoverHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetRediscoverReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetRediscoverLogic(r.Context(), svcCtx)
		resp, err := l.AssetTargetRediscover(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetTargetGroupsHandler 目标资产按维度聚合（host/port/ip/app/status）
func AssetTargetGroupsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetGroupsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetGroupsLogic(r.Context(), svcCtx)
		resp, err := l.AssetTargetGroups(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// AssetTargetCertsHandler 目标关联的 TLS 证书列表
func AssetTargetCertsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AssetTargetCertsReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}
		l := logic.NewAssetTargetCertsLogic(r.Context(), svcCtx)
		resp, err := l.AssetTargetCerts(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// ScreenshotsClearHandler 清空截图
func ScreenshotsClearHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewScreenshotsClearLogic(r.Context(), svcCtx)
		resp, err := l.ScreenshotsClear()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}
