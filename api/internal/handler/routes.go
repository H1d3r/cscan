package handler

import (
	"net/http"

	"cscan/api/internal/handler/ai"
	"cscan/api/internal/handler/asset"
	"cscan/api/internal/handler/blacklist"
	"cscan/api/internal/handler/dirscan"
	"cscan/api/internal/handler/fingerprint"
	"cscan/api/internal/handler/notify"
	"cscan/api/internal/handler/onlineapi"
	"cscan/api/internal/handler/organization"
	"cscan/api/internal/handler/poc"
	"cscan/api/internal/handler/report"
	"cscan/api/internal/handler/subdomain"
	"cscan/api/internal/handler/subfinder"
	"cscan/api/internal/handler/task"
	"cscan/api/internal/handler/user"
	"cscan/api/internal/handler/vul"
	"cscan/api/internal/handler/worker"
	"cscan/api/internal/handler/workspace"
	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// WorkerWSHandlerInstance 全局WebSocket处理器实例
var WorkerWSHandlerInstance *worker.WorkerWSHandler

func RegisterHandlers(server *rest.Server, svcCtx *svc.ServiceContext) {
	// 初始化WebSocket处理器
	WorkerWSHandlerInstance = worker.NewWorkerWSHandler(svcCtx)

	// 初始化审计服务
	worker.InitAuditService(svcCtx)

	// 健康检查端点（无需认证）
	server.AddRoutes(
		[]rest.Route{
			{Method: http.MethodGet, Path: "/health", Handler: func(w http.ResponseWriter, r *http.Request) {
				// 检查MongoDB连接
				if err := svcCtx.MongoClient.Ping(r.Context(), nil); err != nil {
					http.Error(w, "MongoDB unhealthy", http.StatusServiceUnavailable)
					return
				}
				// 检查Redis连接
				if err := svcCtx.RedisClient.Ping(r.Context()).Err(); err != nil {
					http.Error(w, "Redis unhealthy", http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			}},
		},
	)

	// 公开路由（无需认证）- 登录接口和Worker安装相关
	server.AddRoutes(
		[]rest.Route{
			{Method: http.MethodPost, Path: "/api/v1/login", Handler: user.LoginHandler(svcCtx)},
			// 全局主题配置（无需认证，所有人可获取）
			{Method: http.MethodPost, Path: "/api/v1/theme/config/get", Handler: notify.ThemeConfigGetHandler(svcCtx)},
			// Worker安装相关（无需认证，Worker需要调用）
			{Method: http.MethodGet, Path: "/api/v1/worker/download", Handler: worker.WorkerDownloadHandler(svcCtx)},
			{Method: http.MethodPost, Path: "/api/v1/worker/validate", Handler: worker.WorkerValidateKeyHandler(svcCtx)},
			// Worker WebSocket端点（认证在WebSocket握手后进行）
			{Method: http.MethodGet, Path: "/api/v1/worker/ws", Handler: worker.WorkerWSEndpointHandler(svcCtx, WorkerWSHandlerInstance)},
			// 静态文件 - docker-compose-worker.yaml
			{Method: http.MethodGet, Path: "/static/docker-compose-worker.yaml", Handler: worker.DockerComposeWorkerHandler(svcCtx)},
		},
	)

	// Worker专用路由（需要Install Key认证）
	workerAuthMiddleware := middleware.NewWorkerAuthMiddleware(svcCtx.RedisClient)
	workerRoutes := []rest.Route{
		// 任务相关
		{Method: http.MethodPost, Path: "/api/v1/worker/task/check", Handler: worker.WorkerTaskCheckHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/update", Handler: worker.WorkerTaskUpdateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/result", Handler: worker.WorkerTaskResultHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/vul", Handler: worker.WorkerVulResultHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/dirscan", Handler: worker.WorkerDirScanResultHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/subtask/done", Handler: worker.WorkerSubTaskDoneHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/control", Handler: worker.WorkerTaskControlHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/recovery", Handler: worker.WorkerTaskRecoveryHandler(svcCtx)},
		// 心跳
		{Method: http.MethodPost, Path: "/api/v1/worker/heartbeat", Handler: worker.WorkerHeartbeatHandler(svcCtx)},
		// Worker离线通知
		{Method: http.MethodPost, Path: "/api/v1/worker/offline", Handler: worker.WorkerOfflineHandler(svcCtx)},
		// 配置获取
		{Method: http.MethodPost, Path: "/api/v1/worker/config/templates", Handler: worker.WorkerConfigTemplatesHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/fingerprints", Handler: worker.WorkerConfigFingerprintsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/subfinder", Handler: worker.WorkerConfigSubfinderHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/httpservice", Handler: worker.WorkerConfigHttpServiceHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/httpservice/settings", Handler: worker.WorkerConfigHttpServiceSettingsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/activefingerprints", Handler: worker.WorkerConfigActiveFingerprintsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/poc", Handler: worker.WorkerConfigPocHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/dirscandict", Handler: worker.WorkerConfigDirScanDictHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/config/subdomaindict", Handler: worker.WorkerConfigSubdomainDictHandler(svcCtx)},
		// 黑名单规则（供Worker使用）
		{Method: http.MethodPost, Path: "/api/v1/worker/config/blacklist", Handler: blacklist.BlacklistRulesHandler(svcCtx)},
	}

	// 为Worker路由包装认证中间件
	for i := range workerRoutes {
		originalHandler := workerRoutes[i].Handler
		workerRoutes[i].Handler = func(w http.ResponseWriter, r *http.Request) {
			workerAuthMiddleware.Handle(http.HandlerFunc(originalHandler)).ServeHTTP(w, r)
		}
	}

	server.AddRoutes(workerRoutes)

	// 需要认证的路由
	authMiddleware := middleware.NewAuthMiddleware(svcCtx.Config.Auth.AccessSecret)
	authRoutes := []rest.Route{
		// 用户管理
		{Method: http.MethodPost, Path: "/api/v1/user/list", Handler: user.UserListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/create", Handler: user.UserCreateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/update", Handler: user.UserUpdateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/delete", Handler: user.UserDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/resetPassword", Handler: user.UserResetPasswordHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/scanConfig/save", Handler: user.SaveScanConfigHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/scanConfig/get", Handler: user.GetScanConfigHandler(svcCtx)},

		// Worker日志（需要认证）
		{Method: http.MethodGet, Path: "/api/v1/worker/logs/stream", Handler: worker.WorkerLogsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/logs/history", Handler: worker.WorkerLogsHistoryHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/logs/export", Handler: worker.WorkerLogsExportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/logs/clear", Handler: worker.WorkerLogsClearHandler(svcCtx)},

		// 工作空间
		{Method: http.MethodPost, Path: "/api/v1/workspace/list", Handler: workspace.WorkspaceListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/workspace/save", Handler: workspace.WorkspaceSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/workspace/delete", Handler: workspace.WorkspaceDeleteHandler(svcCtx)},

		// 组织管理
		{Method: http.MethodPost, Path: "/api/v1/organization/list", Handler: organization.OrganizationListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/organization/save", Handler: organization.OrganizationSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/organization/delete", Handler: organization.OrganizationDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/organization/updateStatus", Handler: organization.OrganizationUpdateStatusHandler(svcCtx)},

		// 资产管理
		{Method: http.MethodPost, Path: "/api/v1/asset/list", Handler: asset.AssetListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/stat", Handler: asset.AssetStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/groups", Handler: asset.AssetGroupsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/groups/delete", Handler: asset.DeleteAssetGroupHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/inventory", Handler: asset.AssetInventoryHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/screenshots", Handler: asset.ScreenshotsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/filterOptions", Handler: asset.AssetFilterOptionsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/exposures", Handler: asset.AssetExposuresHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/updateLabels", Handler: asset.AssetUpdateLabelsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/addLabel", Handler: asset.AssetAddLabelHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/removeLabel", Handler: asset.AssetRemoveLabelHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/delete", Handler: asset.AssetDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/batchDelete", Handler: asset.AssetBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/clear", Handler: asset.AssetClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/history", Handler: asset.AssetHistoryHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/import", Handler: asset.AssetImportHandler(svcCtx)},

		// 站点管理
		{Method: http.MethodPost, Path: "/api/v1/asset/site/list", Handler: asset.SiteListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/site/stat", Handler: asset.SiteStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/site/delete", Handler: asset.SiteDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/site/batchDelete", Handler: asset.SiteBatchDeleteHandler(svcCtx)},

		// 域名管理
		{Method: http.MethodPost, Path: "/api/v1/asset/domain/list", Handler: asset.DomainListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/domain/stat", Handler: asset.DomainStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/domain/delete", Handler: asset.DomainDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/domain/batchDelete", Handler: asset.DomainBatchDeleteHandler(svcCtx)},

		// IP管理
		{Method: http.MethodPost, Path: "/api/v1/asset/ip/list", Handler: asset.IPListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/ip/stat", Handler: asset.IPStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/ip/delete", Handler: asset.IPDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/ip/batchDelete", Handler: asset.IPBatchDeleteHandler(svcCtx)},

		// 任务管理
		{Method: http.MethodPost, Path: "/api/v1/task/list", Handler: task.MainTaskListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/create", Handler: task.MainTaskCreateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/update", Handler: task.MainTaskUpdateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/delete", Handler: task.MainTaskDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/batchDelete", Handler: task.MainTaskBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/retry", Handler: task.MainTaskRetryHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/start", Handler: task.MainTaskStartHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/pause", Handler: task.MainTaskPauseHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/resume", Handler: task.MainTaskResumeHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/stop", Handler: task.MainTaskStopHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/stat", Handler: task.TaskStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/profile/list", Handler: task.TaskProfileListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/profile/save", Handler: task.TaskProfileSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/profile/delete", Handler: task.TaskProfileDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/logs", Handler: task.GetTaskLogsHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/task/logs/stream", Handler: task.TaskLogsStreamHandler(svcCtx)},

		// 扫描配置模板管理
		{Method: http.MethodPost, Path: "/api/v1/task/template/list", Handler: task.ScanTemplateListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/save", Handler: task.ScanTemplateSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/delete", Handler: task.ScanTemplateDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/detail", Handler: task.ScanTemplateDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/fromTask", Handler: task.ScanTemplateFromTaskHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/categories", Handler: task.ScanTemplateCategoriesHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/export", Handler: task.ScanTemplateExportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/import", Handler: task.ScanTemplateImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/template/use", Handler: task.ScanTemplateUseHandler(svcCtx)},

		// 定时任务管理
		{Method: http.MethodPost, Path: "/api/v1/task/cron/list", Handler: task.CronTaskListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/save", Handler: task.CronTaskSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/toggle", Handler: task.CronTaskToggleHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/delete", Handler: task.CronTaskDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/batchDelete", Handler: task.CronTaskBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/runNow", Handler: task.CronTaskRunNowHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/validate", Handler: task.ValidateCronSpecHandler(svcCtx)},

		// 漏洞管理
		{Method: http.MethodPost, Path: "/api/v1/vul/list", Handler: vul.VulListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/detail", Handler: vul.VulDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/stat", Handler: vul.VulStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/delete", Handler: vul.VulDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/batchDelete", Handler: vul.VulBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/clear", Handler: vul.VulClearHandler(svcCtx)},

		// Worker管理
		{Method: http.MethodPost, Path: "/api/v1/worker/list", Handler: worker.WorkerListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/delete", Handler: worker.WorkerDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/rename", Handler: worker.WorkerRenameHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/restart", Handler: worker.WorkerRestartHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/concurrency", Handler: worker.WorkerSetConcurrencyHandler(svcCtx)},
		// Worker安装管理（需要认证）
		{Method: http.MethodPost, Path: "/api/v1/worker/install/command", Handler: worker.WorkerInstallCommandHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/install/refresh", Handler: worker.WorkerRefreshKeyHandler(svcCtx)},

		// 在线API搜索
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/search", Handler: onlineapi.OnlineSearchHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/import", Handler: onlineapi.OnlineImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/importAll", Handler: onlineapi.OnlineImportAllHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/config/list", Handler: onlineapi.APIConfigListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/config/save", Handler: onlineapi.APIConfigSaveHandler(svcCtx)},

		// POC标签映射
		{Method: http.MethodPost, Path: "/api/v1/poc/tagmapping/list", Handler: poc.TagMappingListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/tagmapping/save", Handler: poc.TagMappingSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/tagmapping/delete", Handler: poc.TagMappingDeleteHandler(svcCtx)},

		// 自定义POC
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/list", Handler: poc.CustomPocListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/save", Handler: poc.CustomPocSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/delete", Handler: poc.CustomPocDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/batchImport", Handler: poc.CustomPocBatchImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/clearAll", Handler: poc.CustomPocClearAllHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/scanAssets", Handler: poc.CustomPocScanAssetsHandler(svcCtx)},

		// Nuclei默认模板
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/templates", Handler: poc.NucleiTemplateListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/categories", Handler: poc.NucleiTemplateCategoriesHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/sync", Handler: poc.NucleiTemplateSyncHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/download", Handler: poc.NucleiTemplateDownloadHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/poc/nuclei/download/status", Handler: poc.NucleiTemplateDownloadStatusHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/clear", Handler: poc.NucleiTemplateClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/updateEnabled", Handler: poc.NucleiTemplateUpdateEnabledHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/detail", Handler: poc.NucleiTemplateDetailHandler(svcCtx)},

		// 指纹管理
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/list", Handler: fingerprint.FingerprintListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/save", Handler: fingerprint.FingerprintSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/delete", Handler: fingerprint.FingerprintDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/categories", Handler: fingerprint.FingerprintCategoriesHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/sync", Handler: fingerprint.FingerprintSyncHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/updateEnabled", Handler: fingerprint.FingerprintUpdateEnabledHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/batchUpdateEnabled", Handler: fingerprint.FingerprintBatchUpdateEnabledHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/import", Handler: fingerprint.FingerprintImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/importFromFile", Handler: fingerprint.FingerprintImportFromFileHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/clearCustom", Handler: fingerprint.FingerprintClearCustomHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/validate", Handler: fingerprint.FingerprintValidateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/batchValidate", Handler: fingerprint.FingerprintBatchValidateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/matchAssets", Handler: fingerprint.FingerprintMatchAssetsHandler(svcCtx)},

		// POC验证
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/validate", Handler: poc.PocValidateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/validateSyntax", Handler: poc.ValidatePocSyntaxHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/batchValidate", Handler: poc.PocBatchValidateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/queryResult", Handler: poc.PocValidationResultQueryHandler(svcCtx)},

		// HTTP服务映射（旧接口，保持兼容）
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/httpservice/list", Handler: fingerprint.HttpServiceMappingListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/httpservice/save", Handler: fingerprint.HttpServiceMappingSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/httpservice/delete", Handler: fingerprint.HttpServiceMappingDeleteHandler(svcCtx)},

		// HTTP服务设置（新接口）
		{Method: http.MethodGet, Path: "/api/v1/httpservice/config", Handler: fingerprint.HttpServiceConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/httpservice/config", Handler: fingerprint.HttpServiceConfigSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/httpservice/mapping/list", Handler: fingerprint.HttpServiceMappingListV2Handler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/httpservice/mapping/save", Handler: fingerprint.HttpServiceMappingSaveV2Handler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/httpservice/mapping/delete", Handler: fingerprint.HttpServiceMappingDeleteV2Handler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/httpservice/export", Handler: fingerprint.HttpServiceExportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/httpservice/import", Handler: fingerprint.HttpServiceImportHandler(svcCtx)},

		// 主动扫描指纹
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/active/list", Handler: fingerprint.ActiveFingerprintListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/active/save", Handler: fingerprint.ActiveFingerprintSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/active/delete", Handler: fingerprint.ActiveFingerprintDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/active/import", Handler: fingerprint.ActiveFingerprintImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/active/export", Handler: fingerprint.ActiveFingerprintExportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/active/clear", Handler: fingerprint.ActiveFingerprintClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/active/validate", Handler: fingerprint.ActiveFingerprintValidateHandler(svcCtx)},

		// 报告管理
		{Method: http.MethodPost, Path: "/api/v1/report/detail", Handler: report.ReportDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/report/export", Handler: report.ReportExportHandler(svcCtx)},

		// Subfinder数据源配置
		{Method: http.MethodPost, Path: "/api/v1/subfinder/provider/list", Handler: subfinder.SubfinderProviderListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subfinder/provider/save", Handler: subfinder.SubfinderProviderSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subfinder/provider/info", Handler: subfinder.SubfinderProviderInfoHandler(svcCtx)},

		// AI辅助
		{Method: http.MethodPost, Path: "/api/v1/ai/generatePoc", Handler: ai.GeneratePocHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/ai/config/get", Handler: ai.AIConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/ai/config/save", Handler: ai.AIConfigSaveHandler(svcCtx)},

		// 目录扫描字典
		{Method: http.MethodPost, Path: "/api/v1/dirscan/dict/list", Handler: dirscan.DirScanDictListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/dict/save", Handler: dirscan.DirScanDictSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/dict/delete", Handler: dirscan.DirScanDictDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/dict/clear", Handler: dirscan.DirScanDictClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/dict/enabled", Handler: dirscan.DirScanDictEnabledListHandler(svcCtx)},

		// 子域名字典
		{Method: http.MethodPost, Path: "/api/v1/subdomain/dict/list", Handler: subdomain.SubdomainDictListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subdomain/dict/save", Handler: subdomain.SubdomainDictSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subdomain/dict/delete", Handler: subdomain.SubdomainDictDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subdomain/dict/clear", Handler: subdomain.SubdomainDictClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subdomain/dict/enabled", Handler: subdomain.SubdomainDictEnabledListHandler(svcCtx)},

		// 目录扫描结果
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/list", Handler: dirscan.DirScanResultListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/stat", Handler: dirscan.DirScanResultStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/delete", Handler: dirscan.DirScanResultDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/batchDelete", Handler: dirscan.DirScanResultBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/clear", Handler: dirscan.DirScanResultClearHandler(svcCtx)},

		// 通知配置
		{Method: http.MethodPost, Path: "/api/v1/notify/config/list", Handler: notify.NotifyConfigListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/config/save", Handler: notify.NotifyConfigSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/config/delete", Handler: notify.NotifyConfigDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/config/test", Handler: notify.NotifyConfigTestHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/providers", Handler: notify.NotifyProviderListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/highrisk/config/get", Handler: notify.HighRiskFilterConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/highrisk/config/save", Handler: notify.HighRiskFilterConfigSaveHandler(svcCtx)},

		// 全局主题配置（需要认证才能保存）
		{Method: http.MethodPost, Path: "/api/v1/theme/config/save", Handler: notify.ThemeConfigSaveHandler(svcCtx)},

		// 资产指纹和端口统计
		{Method: http.MethodPost, Path: "/api/v1/asset/fingerprints/list", Handler: asset.AssetFingerprintsListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/ports/stats", Handler: asset.AssetPortsStatsHandler(svcCtx)},

		// 全局黑名单
		{Method: http.MethodPost, Path: "/api/v1/blacklist/config/get", Handler: blacklist.BlacklistConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/blacklist/config/save", Handler: blacklist.BlacklistConfigSaveHandler(svcCtx)},
	}

	// 为每个路由包装认证中间件
	for i := range authRoutes {
		originalHandler := authRoutes[i].Handler
		authRoutes[i].Handler = func(w http.ResponseWriter, r *http.Request) {
			authMiddleware.Handle(http.HandlerFunc(originalHandler)).ServeHTTP(w, r)
		}
	}

	server.AddRoutes(authRoutes)

	// 需要管理员权限的路由（敏感操作）
	adminRoutes := []rest.Route{
		// 清除日志移到普通认证路由，如需管理员限制可移回此处
	}

	// 为管理员路由包装认证中间件
	for i := range adminRoutes {
		originalHandler := adminRoutes[i].Handler
		adminRoutes[i].Handler = func(w http.ResponseWriter, r *http.Request) {
			authMiddleware.Handle(http.HandlerFunc(originalHandler)).ServeHTTP(w, r)
		}
	}

	if len(adminRoutes) > 0 {
		server.AddRoutes(adminRoutes)
	}

	server.AddRoutes(adminRoutes)

	// Worker控制台路由（需要认证 + 管理员权限）
	consoleAuthMiddleware := middleware.NewConsoleAuthMiddleware(svcCtx.RedisClient)
	consoleRoutes := []rest.Route{
		// Worker控制台信息
		{Method: http.MethodGet, Path: "/api/v1/worker/console/info", Handler: worker.WorkerConsoleInfoHandler(svcCtx, WorkerWSHandlerInstance)},
		// Worker文件管理
		{Method: http.MethodGet, Path: "/api/v1/worker/console/files", Handler: worker.WorkerFileListHandler(svcCtx, WorkerWSHandlerInstance)},
		{Method: http.MethodPost, Path: "/api/v1/worker/console/files/upload", Handler: worker.WorkerFileUploadHandler(svcCtx, WorkerWSHandlerInstance)},
		{Method: http.MethodGet, Path: "/api/v1/worker/console/files/download", Handler: worker.WorkerFileDownloadHandler(svcCtx, WorkerWSHandlerInstance)},
		{Method: http.MethodDelete, Path: "/api/v1/worker/console/files", Handler: worker.WorkerFileDeleteHandler(svcCtx, WorkerWSHandlerInstance)},
		{Method: http.MethodPost, Path: "/api/v1/worker/console/files/mkdir", Handler: worker.WorkerFileMkdirHandler(svcCtx, WorkerWSHandlerInstance)},
		// Worker终端操作（非WebSocket）
		{Method: http.MethodPost, Path: "/api/v1/worker/console/terminal/open", Handler: worker.WorkerTerminalOpenHandler(svcCtx, WorkerWSHandlerInstance)},
		{Method: http.MethodPost, Path: "/api/v1/worker/console/terminal/close", Handler: worker.WorkerTerminalCloseHandler(svcCtx, WorkerWSHandlerInstance)},
		{Method: http.MethodPost, Path: "/api/v1/worker/console/terminal/exec", Handler: worker.WorkerTerminalExecHandler(svcCtx, WorkerWSHandlerInstance)},
		{Method: http.MethodGet, Path: "/api/v1/worker/console/terminal/history", Handler: worker.WorkerTerminalHistoryHandler(svcCtx)},
		// 审计日志
		{Method: http.MethodGet, Path: "/api/v1/worker/console/audit", Handler: worker.WorkerAuditLogHandler(svcCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/worker/console/audit", Handler: worker.WorkerAuditLogClearHandler(svcCtx)},
	}

	// 为控制台路由包装认证中间件和管理员权限中间件
	for i := range consoleRoutes {
		originalHandler := consoleRoutes[i].Handler
		consoleRoutes[i].Handler = func(w http.ResponseWriter, r *http.Request) {
			// 先进行JWT认证
			authMiddleware.Handle(func(w http.ResponseWriter, r *http.Request) {
				// 再进行管理员权限检查
				consoleAuthMiddleware.Handle(http.HandlerFunc(originalHandler)).ServeHTTP(w, r)
			}).ServeHTTP(w, r)
		}
	}

	server.AddRoutes(consoleRoutes)

	// 终端 WebSocket 路由（单独处理，支持从 URL 参数读取 token）
	terminalWSRoute := []rest.Route{
		{Method: http.MethodGet, Path: "/api/v1/worker/console/terminal", Handler: worker.WorkerTerminalWSHandlerWithAuth(svcCtx, WorkerWSHandlerInstance)},
	}
	server.AddRoutes(terminalWSRoute)
}
