package svc

import (
	"context"
	"fmt"
	"time"

	"cscan/api/internal/config"
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

// RefactoredServiceContext represents the new simplified service context
// using interface-based dependency injection
type RefactoredServiceContext struct {
	Config    config.Config
	Storage   Storage      // Single storage interface
	Cache     Cache        // Single cache interface  
	RPC       RPCClient    // Single RPC interface
	Validator Validator    // Input validation
	Scheduler *scheduler.Scheduler
}

// NewRefactoredServiceContext creates a new service context with dependency injection
func NewRefactoredServiceContext(c config.Config) *RefactoredServiceContext {
	// MongoDB connection
	logx.Infof("Connecting to MongoDB: %s", c.Mongo.Uri)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(c.Mongo.Uri))
	if err != nil {
		panic(fmt.Sprintf("Failed to connect MongoDB: %v", err))
	}

	// Test MongoDB connection
	if err := mongoClient.Ping(ctx, nil); err != nil {
		panic(fmt.Sprintf("MongoDB ping failed: %v\nPlease ensure MongoDB is running: docker-compose -f docker-compose.dev.yaml up -d", err))
	}
	logx.Info("MongoDB connected successfully")

	mongoDB := mongoClient.Database(c.Mongo.DbName)

	// Redis connection
	logx.Infof("Connecting to Redis: %s", c.Redis.Host)
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Host,
		Password: c.Redis.Pass,
		DB:       0,
	})

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(fmt.Sprintf("Redis ping failed: %v\nPlease ensure Redis is running: docker-compose -f docker-compose.dev.yaml up -d", err))
	}
	logx.Info("Redis connected successfully")

	// Create RPC client
	logx.Infof("Connecting to RPC: %v", c.TaskRpc.Endpoints)
	rpcClient := zrpc.MustNewClient(c.TaskRpc, zrpc.WithDialOption(
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(50*1024*1024), // 50MB
			grpc.MaxCallSendMsgSize(50*1024*1024), // 50MB
		),
	))
	taskRpcClient := pb.NewTaskServiceClient(rpcClient.Conn())

	// Initialize dependencies through interfaces
	storage := NewMongoStorage(mongoDB)
	cache := NewRedisCache(rdb)
	rpc := NewTaskRPCClient(taskRpcClient)
	validator := NewDefaultValidator()
	scheduler := scheduler.NewScheduler(rdb)

	return &RefactoredServiceContext{
		Config:    c,
		Storage:   storage,
		Cache:     cache,
		RPC:       rpc,
		Validator: validator,
		Scheduler: scheduler,
	}
}

// GetStorage returns the storage interface
func (s *RefactoredServiceContext) GetStorage() Storage {
	return s.Storage
}

// GetCache returns the cache interface
func (s *RefactoredServiceContext) GetCache() Cache {
	return s.Cache
}

// GetRPC returns the RPC interface
func (s *RefactoredServiceContext) GetRPC() RPCClient {
	return s.RPC
}

// GetValidator returns the validator interface
func (s *RefactoredServiceContext) GetValidator() Validator {
	return s.Validator
}

// GetScheduler returns the scheduler
func (s *RefactoredServiceContext) GetScheduler() *scheduler.Scheduler {
	return s.Scheduler
}

// Backward compatibility methods - these delegate to the new interfaces
// This ensures existing code continues to work during the transition

// GetAssetModel returns asset model for backward compatibility
func (s *RefactoredServiceContext) GetAssetModel(workspaceId string) *model.AssetModel {
	if workspaceId == "" {
		workspaceId = "default"
	}
	return s.Storage.AssetModel(workspaceId)
}

// GetMainTaskModel returns task model for backward compatibility
func (s *RefactoredServiceContext) GetMainTaskModel(workspaceId string) *model.MainTaskModel {
	if workspaceId == "" {
		workspaceId = "default"
	}
	return s.Storage.TaskModel(workspaceId)
}

// GetVulModel returns vulnerability model for backward compatibility
func (s *RefactoredServiceContext) GetVulModel(workspaceId string) *model.VulModel {
	if workspaceId == "" {
		workspaceId = "default"
	}
	return s.Storage.VulModel(workspaceId)
}

// GetUserModel returns user model for backward compatibility
func (s *RefactoredServiceContext) GetUserModel() *model.UserModel {
	return s.Storage.UserModel()
}

// GetWorkspaceModel returns workspace model for backward compatibility
func (s *RefactoredServiceContext) GetWorkspaceModel() *model.WorkspaceModel {
	return s.Storage.WorkspaceModel()
}

// GetOrganizationModel returns organization model for backward compatibility
func (s *RefactoredServiceContext) GetOrganizationModel() *model.OrganizationModel {
	return s.Storage.OrganizationModel()
}