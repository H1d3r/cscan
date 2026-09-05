package handler

import (
	"context"
	"net/http"
	"time"

	"cscan/api/internal/handler/ai"
	"cscan/api/internal/handler/asset"
	"cscan/api/internal/handler/blacklist"
	"cscan/api/internal/handler/cert"
	"cscan/api/internal/handler/container"
	"cscan/api/internal/handler/dashboard"
	"cscan/api/internal/handler/dirscan"
	"cscan/api/internal/handler/fingerprint"
	"cscan/api/internal/handler/jsfinder"
	"cscan/api/internal/handler/notify"
	"cscan/api/internal/handler/onlineapi"
	"cscan/api/internal/handler/openapi"
	"cscan/api/internal/handler/organization"
	"cscan/api/internal/handler/poc"
	"cscan/api/internal/handler/report"
	"cscan/api/internal/handler/role"
	"cscan/api/internal/handler/subdomain"
	"cscan/api/internal/handler/subfinder"
	"cscan/api/internal/handler/task"
	"cscan/api/internal/handler/techicon"
	"cscan/api/internal/handler/user"
	"cscan/api/internal/handler/vul"
	"cscan/api/internal/handler/weakpass"
	"cscan/api/internal/handler/worker"
	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/rest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// WorkerWSHandlerInstance 全局WebSocket处理器实例
var WorkerWSHandlerInstance *worker.WorkerWSHandler

func RegisterHandlers(server *rest.Server, svcCtx *svc.ServiceContext) {
	// 初始化WebSocket处理器
	WorkerWSHandlerInstance = worker.NewWorkerWSHandler(svcCtx)

	// 健康检查处理函数（统一的健康检查逻辑）
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 检查MongoDB连接
		if err := svcCtx.MongoClient.Ping(r.Context(), nil); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"code":503,"msg":"MongoDB unhealthy","data":null}`))
			return
		}
		// 检查Redis连接
		if err := svcCtx.RedisClient.Ping(r.Context()).Err(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"code":503,"msg":"Redis unhealthy","data":null}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"healthy","data":{"status":"ok"}}`))
	}

	// 健康检查端点（无需认证）
	server.AddRoutes(
		[]rest.Route{
			{Method: http.MethodGet, Path: "/health", Handler: healthHandler},
			{Method: http.MethodGet, Path: "/api/v1/health", Handler: healthHandler},
		},
	)

	// 公开路由（无需认证）- 登录接口和Worker安装相关
	server.AddRoutes(
		[]rest.Route{
			{Method: http.MethodPost, Path: "/api/v1/login", Handler: user.LoginHandler(svcCtx)},
			{Method: http.MethodPost, Path: "/api/v1/register", Handler: user.RegisterHandler(svcCtx)},
			// 系统状态检测（首次部署时前端调用，无需认证）
			{Method: http.MethodPost, Path: "/api/v1/system/status", Handler: user.SystemStatusHandler(svcCtx)},
			// 全局主题配置（无需认证，所有人可获取）
			{Method: http.MethodPost, Path: "/api/v1/theme/config/get", Handler: notify.ThemeConfigGetHandler(svcCtx)},
			// 全局品牌配置（Logo / 标题；登录页也需展示，因此公开可读）
			{Method: http.MethodPost, Path: "/api/v1/branding/config/get", Handler: notify.BrandingConfigGetHandler(svcCtx)},
			// Worker安装相关（无需认证，Worker需要调用）
			{Method: http.MethodGet, Path: "/api/v1/worker/download", Handler: worker.WorkerDownloadHandler(svcCtx)},
			{Method: http.MethodPost, Path: "/api/v1/worker/validate", Handler: worker.WorkerValidateKeyHandler(svcCtx)},
			// Worker WebSocket端点（认证在WebSocket握手后进行）
			{Method: http.MethodGet, Path: "/api/v1/worker/ws", Handler: worker.WorkerWSEndpointHandler(svcCtx, WorkerWSHandlerInstance)},
			// 静态文件 - docker-compose-worker.yaml
			{Method: http.MethodGet, Path: "/static/docker-compose-worker.yaml", Handler: worker.DockerComposeWorkerHandler(svcCtx)},
			// 静态文件 - worker-tune.sh（Worker 探针本地资源自适应，按目标机规格生成 override）
			{Method: http.MethodGet, Path: "/static/worker-tune.sh", Handler: worker.WorkerTuneHandler(svcCtx)},
			// 静态文件 - worker-tune.ps1（Windows / PowerShell 版，同上）
			{Method: http.MethodGet, Path: "/static/worker-tune.ps1", Handler: worker.WorkerTunePsHandler(svcCtx)},
			// 静态文件 - 用户头像 /static/avatars/<filename>
			{Method: http.MethodGet, Path: "/static/avatars/:filename", Handler: user.AvatarStaticHandler(svcCtx)},
			// 技术栈图标（公开只读，供 <img> 直接引用；通用公开 Logo，MongoDB 本地缓存）
			{Method: http.MethodGet, Path: "/api/v1/tech/icon", Handler: techicon.TechIconHandler(svcCtx)},
		},
	)

	// Worker专用路由（需要Install Key认证）
	workerAuthMiddleware := middleware.NewWorkerAuthMiddleware(svcCtx)
	workerRoutes := []rest.Route{
		// 任务相关
		{Method: http.MethodPost, Path: "/api/v1/worker/task/check", Handler: worker.WorkerTaskCheckHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/task/update", Handler: worker.WorkerTaskUpdateHandler(svcCtx)},
		// 单条漏洞复验结果回传（worker 复测完成后写入复验结论/状态，T-复验闭环）
		{Method: http.MethodPost, Path: "/api/v1/worker/task/vul/reverify", Handler: worker.WorkerVulReverifyHandler(svcCtx)},
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
		{Method: http.MethodPost, Path: "/api/v1/worker/config/weakpassdict", Handler: worker.WorkerConfigWeakpassDictHandler(svcCtx)},
		// 黑名单规则（供Worker使用）
		{Method: http.MethodPost, Path: "/api/v1/worker/config/blacklist", Handler: blacklist.BlacklistRulesHandler(svcCtx)},
		// JSFinder 配置（供Worker使用）
		{Method: http.MethodPost, Path: "/api/v1/worker/config/jsfinder", Handler: worker.WorkerConfigJSFinderHandler(svcCtx)},
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
	authMiddleware = authMiddleware.WithPAT(
		func(ctx context.Context, token string) (primitive.ObjectID, string, string, primitive.ObjectID, []string, error) {
			pat, err := svcCtx.UserTokenModel.FindByHash(ctx, model.HashPAT(token))
			if err != nil || pat == nil {
				return primitive.NilObjectID, "", "", primitive.NilObjectID, nil, err
			}
			if pat.Status != model.StatusEnable {
				return primitive.NilObjectID, "", "", primitive.NilObjectID, nil, nil
			}
			if pat.ExpiresAt != nil && pat.ExpiresAt.Before(time.Now()) {
				return primitive.NilObjectID, "", "", primitive.NilObjectID, nil, nil
			}
			user, err := svcCtx.UserModel.FindByObjectId(ctx, pat.UserId)
			if err != nil || user == nil || user.Status != model.StatusEnable {
				return primitive.NilObjectID, "", "", primitive.NilObjectID, nil, err
			}
			return user.Id, firstNonEmpty(user.Role, "user"), user.Status, pat.Id, pat.Scopes, nil
		},
		func(ctx context.Context, tokenId primitive.ObjectID, ip string) {
			_ = svcCtx.UserTokenModel.UpdateLastUsed(ctx, tokenId, ip, time.Now())
		},
		svcCtx.UserModel,
	)
	authMiddleware = authMiddleware.WithRoleAdmin(svcCtx.IsAdminRole)
	authRoutes := []rest.Route{
		// 用户管理（查看权限）
		{Method: http.MethodPost, Path: "/api/v1/user/list", Handler: user.UserListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/resetPassword", Handler: user.UserResetPasswordHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/scanConfig/save", Handler: user.SaveScanConfigHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/scanConfig/get", Handler: user.GetScanConfigHandler(svcCtx)},
		// 用户自助更新头像（任意登录用户可更新自己的头像）
		{Method: http.MethodPost, Path: "/api/v1/user/avatar/upload", Handler: user.UserAvatarUploadHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/avatar/update", Handler: user.UserUpdateAvatarHandler(svcCtx)},

		// 个人中心：个人信息 / 修改密码 / 个人 API Token
		{Method: http.MethodPost, Path: "/api/v1/user/profile/get", Handler: user.UserProfileGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/profile/update", Handler: user.UserProfileUpdateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/password/change", Handler: user.UserPasswordChangeHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/token/create", Handler: user.UserTokenCreateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/token/list", Handler: user.UserTokenListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/token/setStatus", Handler: user.UserTokenSetStatusHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/token/scopes", Handler: user.UserTokenScopeListHandler(svcCtx)},

		// 引导式首次体验（T4.2）：查询/完成首次引导
		{Method: http.MethodPost, Path: "/api/v1/user/onboarding/status", Handler: user.UserOnboardingStatusHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/onboarding/complete", Handler: user.UserOnboardingCompleteHandler(svcCtx)},

		// 组织管理（查看权限）
		{Method: http.MethodPost, Path: "/api/v1/organization/list", Handler: organization.OrganizationListHandler(svcCtx)},

		// 资产管理
		{Method: http.MethodPost, Path: "/api/v1/asset/list", Handler: asset.AssetListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/port/list", Handler: asset.PortListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/stat", Handler: asset.AssetStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dashboard/changes", Handler: dashboard.DashboardChangesHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dashboard/summary", Handler: dashboard.DashboardSummaryHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/groups", Handler: asset.AssetGroupsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/groups/delete", Handler: asset.DeleteAssetGroupHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/inventory", Handler: asset.AssetInventoryHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/screenshots", Handler: asset.ScreenshotsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/filterOptions", Handler: asset.AssetFilterOptionsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/detail", Handler: asset.AssetDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/exposures", Handler: asset.AssetExposuresHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/list", Handler: asset.AssetTargetListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/detail", Handler: asset.AssetTargetDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/assets", Handler: asset.AssetTargetAssetsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/media", Handler: asset.AssetMediaHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/groups", Handler: asset.AssetTargetGroupsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/certs", Handler: asset.AssetTargetCertsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/update", Handler: asset.AssetTargetUpdateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/delete", Handler: asset.AssetTargetDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/target/rediscover", Handler: asset.AssetTargetRediscoverHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/updateLabels", Handler: asset.AssetUpdateLabelsHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/addLabel", Handler: asset.AssetAddLabelHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/removeLabel", Handler: asset.AssetRemoveLabelHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/delete", Handler: asset.AssetDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/batchDelete", Handler: asset.AssetBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/clear", Handler: asset.AssetClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/history", Handler: asset.AssetHistoryV2Handler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/import", Handler: asset.AssetImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/save", Handler: asset.AssetSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/diff/list", Handler: asset.AssetDiffListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/diff/stat", Handler: asset.AssetDiffStatHandler(svcCtx)},

		// 应用管理
		{Method: http.MethodPost, Path: "/api/v1/asset/app/list", Handler: asset.AppListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/app/stat", Handler: asset.AppStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/app/batchDelete", Handler: asset.AppBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/app/clear", Handler: asset.AppClearHandler(svcCtx)},

		// Icon管理
		{Method: http.MethodPost, Path: "/api/v1/asset/icon/list", Handler: asset.IconListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/icon/stat", Handler: asset.IconStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/icon/batchDelete", Handler: asset.IconBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/icon/clear", Handler: asset.IconClearHandler(svcCtx)},

		// 扫描结果集成 API
		{Method: http.MethodPost, Path: "/api/v1/assets/withScans", Handler: asset.AssetsWithScansHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/assets/dirscans", Handler: asset.AssetDirScansHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/assets/vulnscans", Handler: asset.AssetVulnScansHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/assets/history", Handler: asset.AssetHistoryV2Handler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/assets/compareVersions", Handler: asset.CompareVersionsHandler(svcCtx)},

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
		{Method: http.MethodPost, Path: "/api/v1/task/detail", Handler: task.MainTaskDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/profile/list", Handler: task.TaskProfileListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/profile/save", Handler: task.TaskProfileSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/profile/delete", Handler: task.TaskProfileDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/logs", Handler: task.GetTaskLogsHandler(svcCtx)},
		// 任务分片管理
		{Method: http.MethodPost, Path: "/api/v1/task/chunk/progress", Handler: task.ChunkProgressHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/chunk/preview", Handler: task.ChunkPreviewHandler(svcCtx)},

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
		{Method: http.MethodPost, Path: "/api/v1/task/cron/detail", Handler: task.CronTaskDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/save", Handler: task.CronTaskSaveHandler(svcCtx)},
		// M-4 接口契约兼容别名：文档中的 /task/cron/create 映射到同一个保存处理器
		{Method: http.MethodPost, Path: "/api/v1/task/cron/create", Handler: task.CronTaskSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/toggle", Handler: task.CronTaskToggleHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/delete", Handler: task.CronTaskDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/batchDelete", Handler: task.CronTaskBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/runNow", Handler: task.CronTaskRunNowHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/task/cron/validate", Handler: task.ValidateCronSpecHandler(svcCtx)},

		// 一键扫描 + 智能模板推荐（T4.1）
		{Method: http.MethodPost, Path: "/api/v1/task/quickCreate", Handler: task.TaskQuickCreateHandler(svcCtx)},

		// 漏洞管理
		{Method: http.MethodPost, Path: "/api/v1/vul/list", Handler: vul.VulListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/detail", Handler: vul.VulDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/stat", Handler: vul.VulStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/delete", Handler: vul.VulDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/batchDelete", Handler: vul.VulBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/clear", Handler: vul.VulClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/vul/updateStatus", Handler: vul.VulUpdateStatusHandler(svcCtx)},
		// 单条/批量漏洞复验（人工触发，worker 执行复测，T-复验闭环）
		{Method: http.MethodPost, Path: "/api/v1/vul/reverify", Handler: vul.ReverifyHandler(svcCtx)},

		// Worker 日志相关路由保留在 authRoutes(普通用户可查看)
		// Worker 管理类敏感操作(删除/重启/重命名/并发度/install key)已移至 adminRoutes

		// 在线API搜索
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/search", Handler: onlineapi.OnlineSearchHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/import", Handler: onlineapi.OnlineImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/importAll", Handler: onlineapi.OnlineImportAllHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/import/progress", Handler: onlineapi.OnlineImportProgressHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/import/result", Handler: onlineapi.OnlineImportResultHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/config/list", Handler: onlineapi.APIConfigListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/onlineapi/config/save", Handler: onlineapi.APIConfigSaveHandler(svcCtx)},

		// POC标签映射
		{Method: http.MethodPost, Path: "/api/v1/poc/tagmapping/list", Handler: poc.TagMappingListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/tagmapping/save", Handler: poc.TagMappingSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/tagmapping/delete", Handler: poc.TagMappingDeleteHandler(svcCtx)},

		// 自定义POC
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/list", Handler: poc.CustomPocListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/categories", Handler: poc.CustomPocCategoriesHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/save", Handler: poc.CustomPocSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/delete", Handler: poc.CustomPocDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/batchImport", Handler: poc.CustomPocBatchImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/clearAll", Handler: poc.CustomPocClearAllHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/custom/scanAssets", Handler: poc.CustomPocScanAssetsHandler(svcCtx)},

		// Nuclei默认模板
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/templates", Handler: poc.NucleiTemplateListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/categories", Handler: poc.NucleiTemplateCategoriesHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/sync", Handler: poc.NucleiTemplateSyncHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/clear", Handler: poc.NucleiTemplateClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/updateEnabled", Handler: poc.NucleiTemplateUpdateEnabledHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/poc/nuclei/detail", Handler: poc.NucleiTemplateDetailHandler(svcCtx)},
		// M-4 接口契约兼容别名：文档中的 /poc/detail 映射到同一个详情处理器
		{Method: http.MethodPost, Path: "/api/v1/poc/detail", Handler: poc.NucleiTemplateDetailHandler(svcCtx)},

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
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/batchProgress", Handler: fingerprint.FingerprintBatchProgressHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/batchResult", Handler: fingerprint.FingerprintBatchResultHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/fingerprint/batchStop", Handler: fingerprint.FingerprintBatchStopHandler(svcCtx)},
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
		{Method: http.MethodPost, Path: "/api/v1/report/periodic/generate", Handler: report.ReportPeriodicGenerateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/report/periodic/export", Handler: report.ReportPeriodicExportHandler(svcCtx)},

		// Subfinder数据源配置
		{Method: http.MethodPost, Path: "/api/v1/subfinder/provider/list", Handler: subfinder.SubfinderProviderListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subfinder/provider/save", Handler: subfinder.SubfinderProviderSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/subfinder/provider/info", Handler: subfinder.SubfinderProviderInfoHandler(svcCtx)},

		// AI辅助
		{Method: http.MethodPost, Path: "/api/v1/ai/generatePoc", Handler: ai.GeneratePocHandler(svcCtx)},

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

		// 弱口令字典
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/list", Handler: weakpass.WeakpassDictListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/save", Handler: weakpass.WeakpassDictSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/delete", Handler: weakpass.WeakpassDictDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/clear", Handler: weakpass.WeakpassDictClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/enabled", Handler: weakpass.WeakpassDictEnabledListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/import", Handler: weakpass.WeakpassDictImportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/export", Handler: weakpass.WeakpassDictExportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/parse", Handler: weakpass.WeakpassDictParseHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/weakpass/dict/stats", Handler: weakpass.WeakpassDictServiceStatsHandler(svcCtx)},

		// 目录扫描结果
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/list", Handler: dirscan.DirScanListHandlerV2(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/stat", Handler: dirscan.DirScanResultStatHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/delete", Handler: dirscan.DirScanResultDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/batchDelete", Handler: dirscan.DirScanResultBatchDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/clear", Handler: dirscan.DirScanResultClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/result/detail", Handler: dirscan.DirScanDetailHandler(svcCtx)},
		// DirScan AI研判
		{Method: http.MethodPost, Path: "/api/v1/dirscan/ai/analyze", Handler: dirscan.DirScanAIAnalyzeHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/ai/batch-analyze", Handler: dirscan.DirScanAIBatchAnalyzeHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/ai/batch-progress", Handler: dirscan.DirScanAIBatchProgressHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/dirscan/ai/stop-batch", Handler: dirscan.DirScanAIStopBatchHandler(svcCtx)},

		// 角色菜单同步（任意登录用户拉取自己角色的菜单权限）
		{Method: http.MethodPost, Path: "/api/v1/role/menus/sync", Handler: role.RoleSyncMenusHandler(svcCtx)},

		// 通知配置（查看权限）
		{Method: http.MethodPost, Path: "/api/v1/notify/config/list", Handler: notify.NotifyConfigListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/config/test", Handler: notify.NotifyConfigTestHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/providers", Handler: notify.NotifyProviderListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/highrisk/config/get", Handler: notify.HighRiskFilterConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/highrisk/config/save", Handler: notify.HighRiskFilterConfigSaveHandler(svcCtx)},

		// 全局主题配置（需要认证才能保存）
		{Method: http.MethodPost, Path: "/api/v1/theme/config/save", Handler: notify.ThemeConfigSaveHandler(svcCtx)},

		// 资产指纹和端口统计
		{Method: http.MethodPost, Path: "/api/v1/asset/fingerprints/list", Handler: asset.AssetFingerprintsListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/ports/stats", Handler: asset.AssetPortsStatsHandler(svcCtx)},

		// 全局黑名单（查看权限）
		{Method: http.MethodPost, Path: "/api/v1/blacklist/config/get", Handler: blacklist.BlacklistConfigGetHandler(svcCtx)},

		// JSFinder 全局配置
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/config/get", Handler: jsfinder.JSFinderConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/config/save", Handler: jsfinder.JSFinderConfigSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/config/reset", Handler: jsfinder.JSFinderConfigResetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/list", Handler: jsfinder.JSFinderListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/detail", Handler: jsfinder.JSFinderDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/clear", Handler: jsfinder.JSFinderClearHandler(svcCtx)},

		// JSFinder AI研判
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/ai/analyze", Handler: jsfinder.JSFinderAIAnalyzeHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/ai/batch-analyze", Handler: jsfinder.JSFinderAIBatchAnalyzeHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/ai/batch-progress", Handler: jsfinder.JSFinderAIBatchProgressHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/jsfinder/ai/batch-stop", Handler: jsfinder.JSFinderAIStopBatchHandler(svcCtx)},

		// 证书结果（指纹识别附加产出，ARL 风格）
		{Method: http.MethodPost, Path: "/api/v1/cert/list", Handler: cert.CertListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/cert/detail", Handler: cert.CertDetailHandler(svcCtx)},
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
		// 角色管理（可授予菜单与管理员权限，必须限管理员）
		{Method: http.MethodPost, Path: "/api/v1/role/list", Handler: role.RoleListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/role/detail", Handler: role.RoleDetailHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/role/create", Handler: role.RoleCreateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/role/update", Handler: role.RoleUpdateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/role/delete", Handler: role.RoleDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/role/menus/options", Handler: role.RoleMenuOptionsHandler(svcCtx)},

		// 用户管理（写操作需要管理员权限）
		{Method: http.MethodPost, Path: "/api/v1/user/create", Handler: user.UserCreateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/update", Handler: user.UserUpdateHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/user/delete", Handler: user.UserDeleteHandler(svcCtx)},
		// 用户审核（pending → enable/disable）
		{Method: http.MethodPost, Path: "/api/v1/user/approve", Handler: user.UserApproveHandler(svcCtx)},
		// 注册配置管理
		{Method: http.MethodPost, Path: "/api/v1/registration/config/get", Handler: user.RegistrationConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/registration/config/save", Handler: user.RegistrationConfigSaveHandler(svcCtx)},
		// Worker管理（敏感操作,需要管理员权限）
		// 安全修复:原放在 authRoutes 中,任意登录用户可获取 install key 或重启/删除 Worker
		{Method: http.MethodPost, Path: "/api/v1/worker/list", Handler: worker.WorkerListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/rename", Handler: worker.WorkerRenameHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/restart", Handler: worker.WorkerRestartHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/concurrency", Handler: worker.WorkerSetConcurrencyHandler(svcCtx)},
		// Worker安装管理（install key 是 Worker 主密钥,必须管理员权限）
		{Method: http.MethodPost, Path: "/api/v1/worker/install/command", Handler: worker.WorkerInstallCommandHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/install/refresh", Handler: worker.WorkerRefreshKeyHandler(svcCtx)},
		// Worker日志（含任务详情/目标IP等敏感信息,限管理员读取）
		{Method: http.MethodPost, Path: "/api/v1/worker/logs/history", Handler: worker.WorkerLogsHistoryHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/logs/export", Handler: worker.WorkerLogsExportHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/worker/logs/clear", Handler: worker.WorkerLogsClearHandler(svcCtx)},
		// 全局品牌配置（写权限限管理员）
		{Method: http.MethodPost, Path: "/api/v1/branding/config/save", Handler: notify.BrandingConfigSaveHandler(svcCtx)},
		// AI配置（含明文 apiKey，读写均限管理员）
		{Method: http.MethodPost, Path: "/api/v1/ai/config/get", Handler: ai.AIConfigGetHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/ai/config/save", Handler: ai.AIConfigSaveHandler(svcCtx)},

		// 危险的批量清除操作（需要管理员权限）
		{Method: http.MethodPost, Path: "/api/v1/asset/port/clear", Handler: asset.PortClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/screenshots/clear", Handler: asset.ScreenshotsClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/site/clear", Handler: asset.SiteClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/domain/clear", Handler: asset.DomainClearHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/asset/ip/clear", Handler: asset.IPClearHandler(svcCtx)},

		// 组织管理（写操作需要管理员权限）
		{Method: http.MethodPost, Path: "/api/v1/organization/save", Handler: organization.OrganizationSaveHandler(svcCtx)},
		// M-4 接口契约兼容别名：文档中的 /organization/create 映射到同一个保存处理器
		{Method: http.MethodPost, Path: "/api/v1/organization/create", Handler: organization.OrganizationSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/organization/delete", Handler: organization.OrganizationDeleteHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/organization/updateStatus", Handler: organization.OrganizationUpdateStatusHandler(svcCtx)},

		// 通知配置（写操作需要管理员权限）
		{Method: http.MethodPost, Path: "/api/v1/notify/config/save", Handler: notify.NotifyConfigSaveHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notify/config/delete", Handler: notify.NotifyConfigDeleteHandler(svcCtx)},

		// 全局黑名单（写操作需要管理员权限）
		{Method: http.MethodPost, Path: "/api/v1/blacklist/config/save", Handler: blacklist.BlacklistConfigSaveHandler(svcCtx)},
		// M-4 接口契约兼容别名：文档中的 /blacklist/save 映射到同一个保存处理器
		{Method: http.MethodPost, Path: "/api/v1/blacklist/save", Handler: blacklist.BlacklistConfigSaveHandler(svcCtx)},

		// 容器管理（管理员权限；原随 Worker 控制台路由注册，控制台移除后并入管理员组）
		{Method: http.MethodPost, Path: "/api/v1/container/list", Handler: container.ContainerListHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/container/logs/fetch", Handler: container.ContainerLogsFetchHandler(svcCtx)},
		// 容器日志历史(本地文件读取)
		{Method: http.MethodGet, Path: "/api/v1/container/logs/dates", Handler: container.ContainerLogDatesHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/container/logs/files", Handler: container.ContainerLogFilesHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/container/logs/history", Handler: container.ContainerLogHistoryHandler(svcCtx)},
	}

	// 为管理员路由包装认证中间件 + 管理员权限中间件
	for i := range adminRoutes {
		originalHandler := adminRoutes[i].Handler
		adminRoutes[i].Handler = func(w http.ResponseWriter, r *http.Request) {
			authMiddleware.Handle(authMiddleware.RequireAdmin(http.HandlerFunc(originalHandler))).ServeHTTP(w, r)
		}
	}

	if len(adminRoutes) > 0 {
		server.AddRoutes(adminRoutes)
	}

	// 开放 API（T5.5）：第三方系统只读查询资产/漏洞/证书。
	// 复用 PAT 鉴权（含 readonly scope 校验），叠加按 token 维度的限流（超频 429）。
	openLimiter := middleware.NewTokenRateLimiter(120, time.Minute)
	openAPIRoutes := []rest.Route{
		{Method: http.MethodGet, Path: "/api/open/v1/assets", Handler: openapi.OpenAssetsHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/open/v1/assets/:id", Handler: openapi.OpenAssetDetailHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/open/v1/vulns", Handler: openapi.OpenVulnsHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/open/v1/vulns/:id", Handler: openapi.OpenVulnDetailHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/open/v1/certs", Handler: openapi.OpenCertsHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/open/v1/certs/:id", Handler: openapi.OpenCertDetailHandler(svcCtx)},
	}
	for i := range openAPIRoutes {
		original := openAPIRoutes[i].Handler
		openAPIRoutes[i].Handler = func(w http.ResponseWriter, r *http.Request) {
			// 先 PAT 鉴权 + scope 校验，再按 token 限流，最后业务处理
			authMiddleware.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				openLimiter.Handle(original).ServeHTTP(w, r)
			})).ServeHTTP(w, r)
		}
	}
	server.AddRoutes(openAPIRoutes)
}
