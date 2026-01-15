package svc

import (
	"context"
	"fmt"
	"time"

	"cscan/api/internal/config"
	"cscan/api/internal/svc/sync"
	"cscan/model"
	"cscan/rpc/task/pb"
	"cscan/scheduler"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
)

type ServiceContext struct {
	Config                  config.Config
	MongoClient             *mongo.Client
	MongoDB                 *mongo.Database
	RedisClient             *redis.Client
	TaskRpcClient           pb.TaskServiceClient
	UserModel               *model.UserModel
	WorkspaceModel          *model.WorkspaceModel
	OrganizationModel       *model.OrganizationModel
	ProfileModel            *model.TaskProfileModel
	TagMappingModel         *model.TagMappingModel
	CustomPocModel          *model.CustomPocModel
	NucleiTemplateModel     *model.NucleiTemplateModel
	FingerprintModel        *model.FingerprintModel
	HttpServiceMappingModel  *model.HttpServiceMappingModel
	HttpServiceModel         *model.HttpServiceModel // 新的HTTP服务设置模型
	ActiveFingerprintModel   *model.ActiveFingerprintModel
	CommandHistoryModel      *model.CommandHistoryModel
	AuditLogModel            *model.AuditLogModel
	NotifyConfigModel        *model.NotifyConfigModel
	ScanTemplateModel        *model.ScanTemplateModel

	// 调度器
	Scheduler *scheduler.Scheduler

	// 同步服务
	SyncMethods *sync.SyncMethods

	// 缓存的模板元数据
	TemplateCategories []string
	TemplateTags       []string
	TemplateStats      map[string]int
}

func NewServiceContext(c config.Config) *ServiceContext {
	// MongoDB连接
	fmt.Println("Connecting to MongoDB:", c.Mongo.Uri)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 配置MongoDB连接池和超时
	clientOptions := options.Client().
		ApplyURI(c.Mongo.Uri).
		SetMaxPoolSize(100).                    // 最大连接数
		SetMinPoolSize(10).                     // 最小连接数
		SetMaxConnIdleTime(30 * time.Second).   // 空闲连接超时
		SetConnectTimeout(10 * time.Second).    // 连接超时
		SetServerSelectionTimeout(10 * time.Second). // 服务器选择超时
		SetSocketTimeout(30 * time.Second)      // Socket超时

	mongoClient, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		panic(fmt.Sprintf("Failed to connect MongoDB: %v", err))
	}

	// 测试 MongoDB 连接
	if err := mongoClient.Ping(ctx, nil); err != nil {
		panic(fmt.Sprintf("MongoDB ping failed: %v\nPlease ensure MongoDB is running: docker-compose -f docker-compose.dev.yaml up -d", err))
	}
	fmt.Println("MongoDB connected successfully")

	mongoDB := mongoClient.Database(c.Mongo.DbName)

	// Redis连接 - 使用go-zero配置，增加连接池和超时设置
	fmt.Println("Connecting to Redis:", c.Redis.Host)
	rdb := redis.NewClient(&redis.Options{
		Addr:         c.Redis.Host,
		Password:     c.Redis.Pass,
		DB:           0,
		PoolSize:     100,                    // 连接池大小
		MinIdleConns: 10,                     // 最小空闲连接数
		MaxRetries:   3,                      // 最大重试次数
		DialTimeout:  5 * time.Second,        // 连接超时
		ReadTimeout:  3 * time.Second,        // 读超时
		WriteTimeout: 3 * time.Second,        // 写超时
		PoolTimeout:  4 * time.Second,        // 连接池超时
	})

	// 测试 Redis 连接
	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(fmt.Sprintf("Redis ping failed: %v\nPlease ensure Redis is running: docker-compose -f docker-compose.dev.yaml up -d", err))
	}
	fmt.Println("Redis connected successfully")

	// 创建RPC客户端（增加消息大小限制到50MB，支持大量指纹数据传输）
	fmt.Println("Connecting to RPC:", c.TaskRpc.Endpoints)
	rpcClient := zrpc.MustNewClient(c.TaskRpc, zrpc.WithDialOption(
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(50*1024*1024), // 50MB
			grpc.MaxCallSendMsgSize(50*1024*1024), // 50MB
		),
	))
	taskRpcClient := pb.NewTaskServiceClient(rpcClient.Conn())

	svcCtx := &ServiceContext{
		Config:                  c,
		MongoClient:             mongoClient,
		MongoDB:                 mongoDB,
		RedisClient:             rdb,
		TaskRpcClient:           taskRpcClient,
		UserModel:               model.NewUserModel(mongoDB),
		WorkspaceModel:          model.NewWorkspaceModel(mongoDB),
		OrganizationModel:       model.NewOrganizationModel(mongoDB),
		ProfileModel:            model.NewTaskProfileModel(mongoDB),
		TagMappingModel:         model.NewTagMappingModel(mongoDB),
		CustomPocModel:          model.NewCustomPocModel(mongoDB),
		NucleiTemplateModel:     model.NewNucleiTemplateModel(mongoDB),
		FingerprintModel:        model.NewFingerprintModel(mongoDB),
		HttpServiceMappingModel:  model.NewHttpServiceMappingModel(mongoDB),
		HttpServiceModel:         model.NewHttpServiceModel(mongoDB),
		ActiveFingerprintModel:   model.NewActiveFingerprintModel(mongoDB),
		CommandHistoryModel:      model.NewCommandHistoryModel(mongoDB),
		AuditLogModel:            model.NewAuditLogModel(mongoDB),
		NotifyConfigModel:        model.NewNotifyConfigModel(mongoDB),
		ScanTemplateModel:        model.NewScanTemplateModel(mongoDB),
		Scheduler:               scheduler.NewScheduler(rdb),
		TemplateCategories:      []string{},
		TemplateTags:            []string{},
		TemplateStats:           map[string]int{},
	}

	// 初始化同步服务
	svcCtx.SyncMethods = sync.NewSyncMethods(
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

	// 初始化内置扫描模板
	sync.InitBuiltinTemplates(svcCtx.ScanTemplateModel)

	return svcCtx
}

// GetAssetModel 根据workspaceId获取资产模型
func (s *ServiceContext) GetAssetModel(workspaceId string) *model.AssetModel {
	if workspaceId == "" {
		workspaceId = "default"
	}
	return model.NewAssetModel(s.MongoDB, workspaceId)
}

// GetMainTaskModel 根据workspaceId获取主任务模型
func (s *ServiceContext) GetMainTaskModel(workspaceId string) *model.MainTaskModel {
	if workspaceId == "" {
		workspaceId = "default"
	}
	return model.NewMainTaskModel(s.MongoDB, workspaceId)
}

// GetVulModel 根据workspaceId获取漏洞模型
func (s *ServiceContext) GetVulModel(workspaceId string) *model.VulModel {
	if workspaceId == "" {
		workspaceId = "default"
	}
	return model.NewVulModel(s.MongoDB, workspaceId)
}

// GetAssetHistoryModel 根据workspaceId获取资产历史模型
func (s *ServiceContext) GetAssetHistoryModel(workspaceId string) *model.AssetHistoryModel {
	if workspaceId == "" {
		workspaceId = "default"
	}
	return model.NewAssetHistoryModel(s.MongoDB, workspaceId)
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
		s.TemplateCategories = categories
	}

	s.TemplateTags = []string{}

	stats, err := s.NucleiTemplateModel.GetStats(ctx)
	if err == nil {
		s.TemplateStats = stats
	}

	logx.Infof("[NucleiCache] Refreshed: %d categories, stats: %v", len(s.TemplateCategories), s.TemplateStats)
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
