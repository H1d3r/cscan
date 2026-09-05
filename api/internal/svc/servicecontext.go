package svc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"cscan/api/internal/config"
	svcsync "cscan/api/internal/svc/sync"
	"cscan/internal/model"
	"cscan/internal/scheduler"
	"cscan/pkg/cache"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ServiceContext struct {
	Config      config.Config
	MongoClient *mongo.Client
	MongoDB     *mongo.Database
	RedisClient *redis.Client
	// WorkerDefaultKey 默认 Worker 认证密钥（来自环境变量 CSCAN_WORKER_KEY）。
	// 与 Redis install_key 独立，互不影响。为空时仅校验 Redis install_key。
	WorkerDefaultKey        string
	UserModel               *model.UserModel
	UserTokenModel          *model.UserTokenModel
	OrganizationModel       *model.OrganizationModel
	ProfileModel            *model.TaskProfileModel
	TagMappingModel         *model.TagMappingModel
	CustomPocModel          *model.CustomPocModel
	NucleiTemplateModel     *model.NucleiTemplateModel
	FingerprintModel        *model.FingerprintModel
	HttpServiceMappingModel *model.HttpServiceMappingModel
	HttpServiceModel        *model.HttpServiceModel // 新的HTTP服务设置模型
	ActiveFingerprintModel  *model.ActiveFingerprintModel
	CommandHistoryModel     *model.CommandHistoryModel
	NotifyConfigModel       *model.NotifyConfigModel
	ScanTemplateModel       *model.ScanTemplateModel
	CronTaskModel           *model.CronTaskModel
	WeakpassDictModel       *model.WeakpassDictModel
	SubfinderProviderModel  *model.SubfinderProviderModel
	TechIconModel           *model.TechIconModel
	RoleModel               *model.RoleModel

	// 技术栈图标元数据（wappalyzergo 内嵌指纹解析出的 名称→图标文件名 映射）
	TechIconMeta *TechIconMeta

	// 调度器
	Scheduler *scheduler.Scheduler

	// 同步服务
	SyncMethods *svcsync.SyncMethods

	// 扫描结果服务
	ScanResultService *ScanResultService
	HistoryService    *HistoryService

	// Docker 容器服务(可选;docker.sock 不可达时为 nil,容器接口返回 503)
	DockerService *DockerService

	// 容器日志采集器(后台写本地文件,可选)
	LogCollector *LogCollector

	// Worker 日志读取器（从 MongoDB worker_log 集合读取）
	WorkerLogReader *WorkerLogReader

	// 缓存的模板元数据（并发安全）
	templateMu         sync.RWMutex
	TemplateCategories []string
	TemplateTags       []string
	TemplateStats      map[string]int

	// 查询聚合结果缓存（filterOptions/iconStat/appStat/siteStat/vulStat/assetStat/orgMap）
	// 短 TTL（30~60s）+ singleflight 防击穿，扫描完成可主动失效
	QueryCache *cache.LocalCache
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	// MongoDB连接
	logx.Infof("Connecting to MongoDB: %s", c.Mongo.Uri)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 配置MongoDB连接池和超时
	clientOptions := options.Client().
		ApplyURI(c.Mongo.Uri).
		SetMaxPoolSize(100).                         // 最大连接数
		SetMinPoolSize(5).                           // 最小连接数（修复 D2：原 10，避免小内存机器预占过多连接）
		SetMaxConnIdleTime(30 * time.Second).        // 空闲连接超时
		SetConnectTimeout(10 * time.Second).         // 连接超时
		SetServerSelectionTimeout(30 * time.Second). // 服务器选择超时（修复 D2：原 10s，DNS 抖动时易触发连接池被清空）
		SetSocketTimeout(30 * time.Second).          // Socket超时
		SetHeartbeatInterval(10 * time.Second).      // 心跳间隔（修复 D2：更快探测到死连接，及时重建连接）
		SetRetryReads(true).                         // 瞬时故障自动重试（修复 D2）
		SetRetryWrites(true)                         // 瞬时故障自动重试（修复 D2）

	mongoClient, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}

	// 测试 MongoDB 连接
	if err := mongoClient.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("ping MongoDB: %w", err)
	}
	logx.Info("MongoDB connected successfully")

	mongoDB := mongoClient.Database(c.Mongo.DbName)

	// Redis连接 - 使用go-zero配置，增加连接池和超时设置
	logx.Infof("Connecting to Redis: %s", c.Redis.Host)
	rdb := redis.NewClient(&redis.Options{
		Addr:         c.Redis.Host,
		Password:     c.Redis.Pass,
		DB:           0,
		PoolSize:     100,             // 连接池大小
		MinIdleConns: 10,              // 最小空闲连接数
		MaxRetries:   3,               // 最大重试次数
		DialTimeout:  5 * time.Second, // 连接超时
		ReadTimeout:  3 * time.Second, // 读超时
		WriteTimeout: 3 * time.Second, // 写超时
		PoolTimeout:  4 * time.Second, // 连接池超时
	})

	// 测试 Redis 连接
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	logx.Info("Redis connected successfully")

	svcCtx := &ServiceContext{
		Config:                  c,
		MongoClient:             mongoClient,
		MongoDB:                 mongoDB,
		RedisClient:             rdb,
		WorkerDefaultKey:        os.Getenv("CSCAN_WORKER_KEY"),
		UserModel:               model.NewUserModel(mongoDB),
		UserTokenModel:          model.NewUserTokenModel(mongoDB),
		OrganizationModel:       model.NewOrganizationModel(mongoDB),
		ProfileModel:            model.NewTaskProfileModel(mongoDB),
		TagMappingModel:         model.NewTagMappingModel(mongoDB),
		CustomPocModel:          model.NewCustomPocModel(mongoDB),
		NucleiTemplateModel:     model.NewNucleiTemplateModel(mongoDB),
		FingerprintModel:        model.NewFingerprintModel(mongoDB),
		HttpServiceMappingModel: model.NewHttpServiceMappingModel(mongoDB),
		HttpServiceModel:        model.NewHttpServiceModel(mongoDB),
		ActiveFingerprintModel:  model.NewActiveFingerprintModel(mongoDB),
		CommandHistoryModel:     model.NewCommandHistoryModel(mongoDB),
		NotifyConfigModel:       model.NewNotifyConfigModel(mongoDB),
		ScanTemplateModel:       model.NewScanTemplateModel(mongoDB),
		CronTaskModel:           model.NewCronTaskModel(mongoDB),
		WeakpassDictModel:       model.NewWeakpassDictModel(mongoDB),
		SubfinderProviderModel:  model.NewSubfinderProviderModel(mongoDB),
		TechIconModel:           model.NewTechIconModel(mongoDB),
		RoleModel:               model.NewRoleModel(mongoDB),
		TechIconMeta:            NewTechIconMeta(),
		Scheduler:               scheduler.NewScheduler(rdb),
		ScanResultService:       NewScanResultService(mongoDB),
		HistoryService:          NewHistoryService(mongoDB),
		TemplateCategories:      []string{},
		TemplateTags:            []string{},
		TemplateStats:           map[string]int{},
		QueryCache:              cache.NewLocalCache(60 * time.Second),
	}

	// 初始化 Docker 服务(可选,失败仅记录告警)
	if ds, err := NewDockerService(c.Docker); err != nil {
		logx.Errorf("[Docker] service unavailable: %v", err)
	} else {
		svcCtx.DockerService = ds
	}

	// 初始化容器日志采集器(可选,后台持续写入本地文件)
	if lc, err := NewLogCollector(c.Docker, c.Docker.LogDir, c.Docker.RetentionDays); err != nil {
		logx.Errorf("[LogCollector] unavailable: %v", err)
	} else {
		svcCtx.LogCollector = lc
		lc.Start()
	}

	// 初始化 Worker 日志读取器（从 MongoDB 读取）
	svcCtx.WorkerLogReader = NewWorkerLogReader(svcCtx.MongoDB)

	// 初始化同步服务
	svcCtx.SyncMethods = svcsync.NewSyncMethods(
		svcCtx.NucleiTemplateModel,
		svcCtx.FingerprintModel,
		svcCtx.CustomPocModel,
		svcCtx.ActiveFingerprintModel,
		model.NewDirScanDictModel(svcCtx.MongoDB),
		model.NewSubdomainDictModel(svcCtx.MongoDB),
	)

	// 设置HTTP服务模型（用于启动时导入）
	svcCtx.SyncMethods.SetHttpServiceModel(svcCtx.HttpServiceModel)

	// 设置黑名单模型（用于启动时导入默认黑名单）
	svcCtx.SyncMethods.SetBlacklistModel(model.NewBlacklistConfigModel(svcCtx.MongoDB))

	// 设置弱口令字典模型（用于启动时导入默认字典）
	svcCtx.SyncMethods.SetWeakpassDictModel(svcCtx.WeakpassDictModel)

	// 初始化内置扫描模板
	svcsync.InitBuiltinTemplates(svcCtx.ScanTemplateModel)

	// 解析技术栈图标元数据（内嵌指纹数据，失败仅降级图标功能，不影响启动）
	svcCtx.TechIconMeta.Load()

	// 初始化 JSFinder 全局配置（不存在则写入内置默认值）
	svcsync.InitJSFinderConfig(model.NewJSFinderConfigModel(svcCtx.MongoDB))

	// 为已存在的内置模板补全 jsfinder 字段（标准扫描默认开启）
	svcsync.MigrateBuiltinTemplatesAddJSFinder(svcCtx.ScanTemplateModel)

	// 初始化内置角色（superadmin / admin / user），失败仅告警不阻塞启动
	if err := svcCtx.RoleModel.EnsureIndexes(); err != nil {
		logx.Errorf("[Role] ensure indexes failed: %v", err)
	}
	roleCtx, roleCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := svcCtx.RoleModel.EnsureBuiltInRoles(roleCtx); err != nil {
		logx.Errorf("[Role] init built-in roles failed: %v", err)
	}
	roleCancel()

	return svcCtx, nil
}

// roleAdminCacheTTL 角色管理员标志的本地缓存时长。
// 角色权限调整后最迟 30s 生效，换取管理员路由无需每请求查一次 MongoDB。
const roleAdminCacheTTL = 30 * time.Second

// IsAdminRole 判定角色是否具备管理员接口权限。
// 内置 superadmin/admin 直接放行（角色集合不可用时仍能登录后台）；
// 其余角色查 role.is_superadmin 标志，结果本地缓存。
func (s *ServiceContext) IsAdminRole(ctx context.Context, roleName string) bool {
	if roleName == "" {
		return false
	}
	if roleName == model.RoleSuperadmin || roleName == model.RoleAdmin {
		return true
	}
	if s.RoleModel == nil {
		return false
	}

	resolve := func() (interface{}, error) {
		queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		role, err := s.RoleModel.FindByName(queryCtx, roleName)
		if err != nil {
			return nil, err
		}
		return role != nil && role.IsSuperadmin, nil
	}

	var v interface{}
	var err error
	if s.QueryCache != nil {
		v, err = s.QueryCache.GetOrSetWithTTL("role:admin:"+roleName, roleAdminCacheTTL, resolve)
	} else {
		v, err = resolve()
	}
	if err != nil {
		logx.Errorf("[Role] resolve admin flag failed: role=%s err=%v", roleName, err)
		return false
	}
	isAdmin, _ := v.(bool)
	return isAdmin
}

// MenuPathsForRole 返回角色可访问的菜单路径。
// 角色未落库（历史数据 / 集合异常）时回退全量菜单，保证升级过程中不锁死已有账号。
func (s *ServiceContext) MenuPathsForRole(ctx context.Context, roleName string) []string {
	if s.RoleModel == nil {
		return model.MenuPathList()
	}
	role, err := s.RoleModel.FindByName(ctx, roleName)
	if err != nil {
		logx.Errorf("[Role] query menu paths failed: role=%s err=%v", roleName, err)
		return model.MenuPathList()
	}
	if role == nil {
		return model.MenuPathList()
	}
	if role.MenuPaths == nil {
		return []string{}
	}
	return role.MenuPaths
}

// ValidateWorkerKey 校验 Worker 密钥（双密钥接受）。
// 优先匹配环境变量 CSCAN_WORKER_KEY（默认 Worker 用，纯内存比较，无外部依赖），
// 再匹配 Redis install_key（手动探针用，可刷新、UI 展示）。
// 返回 (valid, infraError)：infraError=true 表示 Redis 基础设施故障（调用方应返回 503）。
func (s *ServiceContext) ValidateWorkerKey(ctx context.Context, providedKey string) (valid bool, infraError bool) {
	if providedKey == "" {
		return false, false
	}

	// 1. 优先校验环境变量默认密钥（内存比较，永不产生 infraError）
	if s.WorkerDefaultKey != "" &&
		subtle.ConstantTimeCompare([]byte(providedKey), []byte(s.WorkerDefaultKey)) == 1 {
		return true, false
	}

	// 2. 再校验 Redis install_key（手动探针用）
	keyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	storedKey, err := s.RedisClient.Get(keyCtx, "cscan:worker:install_key").Result()
	cancel()

	if err != nil {
		if errors.Is(err, redis.Nil) {
			// install_key 未配置：默认密钥也未命中 → 无效（401），非基础设施故障
			return false, false
		}
		// Redis 基础设施故障 → infraError（503）
		logx.Errorf("[WorkerAuth] Redis unavailable during worker key validation: %v", err)
		return false, true
	}
	if storedKey == "" {
		return false, false
	}
	if subtle.ConstantTimeCompare([]byte(providedKey), []byte(storedKey)) == 1 {
		return true, false
	}
	return false, false
}

func (s *ServiceContext) GetAssetModel() *model.AssetModel {
	return model.NewAssetModel(s.MongoDB)
}

func (s *ServiceContext) GetMainTaskModel() *model.MainTaskModel {
	return model.NewMainTaskModel(s.MongoDB)
}

func (s *ServiceContext) GetVulModel() *model.VulModel {
	return model.NewVulModel(s.MongoDB)
}

func (s *ServiceContext) GetAssetHistoryModel() *model.AssetHistoryModel {
	return model.NewAssetHistoryModel(s.MongoDB)
}

func (s *ServiceContext) GetAssetTargetMetaModel() *model.AssetTargetMetaModel {
	return model.NewAssetTargetMetaModel(s.MongoDB)
}

// GetDirScanResultModel 获取目录扫描结果模型
func (s *ServiceContext) GetDirScanResultModel() *model.DirScanResultModel {
	return model.NewDirScanResultModel(s.MongoDB)
}

// RefreshTemplateCache 刷新模板元数据缓存
func (s *ServiceContext) RefreshTemplateCache() {
	ctx := context.Background()

	categories, err := s.NucleiTemplateModel.GetCategories(ctx)
	if err == nil {
		s.templateMu.Lock()
		s.TemplateCategories = categories
		s.templateMu.Unlock()
	}

	tags := []string{}

	stats, err := s.NucleiTemplateModel.GetStats(ctx)
	if err == nil {
		s.templateMu.Lock()
		s.TemplateStats = stats
		s.templateMu.Unlock()
	}

	s.templateMu.Lock()
	s.TemplateTags = tags
	s.templateMu.Unlock()

	s.templateMu.RLock()
	logx.Infof("[NucleiCache] Refreshed: %d categories, stats: %v", len(s.TemplateCategories), s.TemplateStats)
	s.templateMu.RUnlock()
}

// SyncNucleiTemplates 同步Nuclei模板
func (s *ServiceContext) SyncNucleiTemplates() {
	s.SyncMethods.SyncNucleiTemplates()
}

// SyncWappalyzerFingerprints 同步Wappalyzer指纹
func (s *ServiceContext) SyncWappalyzerFingerprints() {
	s.SyncMethods.SyncWappalyzerFingerprints()
}

// ImportCustomPocAndFingerprints 导入自定义POC和指纹
func (s *ServiceContext) ImportCustomPocAndFingerprints() {
	s.SyncMethods.ImportCustomPocAndFingerprints()
}

func (s *ServiceContext) GetJSFinderResultModel() *model.JSFinderResultModel {
	return model.NewJSFinderResultModel(s.MongoDB)
}

func (s *ServiceContext) GetCertModel() *model.CertModel {
	return model.NewCertModel(s.MongoDB)
}

func (s *ServiceContext) GetExecutorTaskModel() *model.ExecutorTaskModel {
	return model.NewExecutorTaskModel(s.MongoDB)
}
