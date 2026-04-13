package svc

import (
	"context"

	"cscan/model"
	"cscan/rpc/task/pb"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

// Storage interface abstracts all data operations
// This eliminates the need for multiple model dependencies in ServiceContext
type Storage interface {
	// User management
	UserModel() *model.UserModel
	// Workspace management
	WorkspaceModel() *model.WorkspaceModel
	// Organization management
	OrganizationModel() *model.OrganizationModel
	// Asset management
	AssetModel(workspaceId string) *model.AssetModel
	// Task management
	TaskModel(workspaceId string) *model.MainTaskModel
	// Vulnerability management
	VulModel(workspaceId string) *model.VulModel
	// Configuration management
	ProfileModel() *model.TaskProfileModel
	CustomPocModel() *model.CustomPocModel
	NucleiTemplateModel() *model.NucleiTemplateModel
	FingerprintModel() *model.FingerprintModel
	NotifyConfigModel() *model.NotifyConfigModel
}

// Cache interface abstracts all caching operations
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration int64) error
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, keys ...string) (int64, error)
	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key string, values ...interface{}) error
	HDel(ctx context.Context, key string, fields ...string) error
}

// RPCClient interface abstracts RPC operations
type RPCClient interface {
	TaskService() pb.TaskServiceClient
}

// Validator interface abstracts input validation
type Validator interface {
	ValidateStruct(s interface{}) error
	ValidateVar(field interface{}, tag string) error
}

// Implementation structs

// MongoStorage implements Storage interface using MongoDB
type MongoStorage struct {
	db *mongo.Database
}

func NewMongoStorage(db *mongo.Database) Storage {
	return &MongoStorage{db: db}
}

func (s *MongoStorage) UserModel() *model.UserModel {
	return model.NewUserModel(s.db)
}

func (s *MongoStorage) WorkspaceModel() *model.WorkspaceModel {
	return model.NewWorkspaceModel(s.db)
}

func (s *MongoStorage) OrganizationModel() *model.OrganizationModel {
	return model.NewOrganizationModel(s.db)
}

func (s *MongoStorage) AssetModel(workspaceId string) *model.AssetModel {
	return model.NewAssetModel(s.db, workspaceId)
}

func (s *MongoStorage) TaskModel(workspaceId string) *model.MainTaskModel {
	return model.NewMainTaskModel(s.db, workspaceId)
}

func (s *MongoStorage) VulModel(workspaceId string) *model.VulModel {
	return model.NewVulModel(s.db, workspaceId)
}

func (s *MongoStorage) ProfileModel() *model.TaskProfileModel {
	return model.NewTaskProfileModel(s.db)
}

func (s *MongoStorage) CustomPocModel() *model.CustomPocModel {
	return model.NewCustomPocModel(s.db)
}

func (s *MongoStorage) NucleiTemplateModel() *model.NucleiTemplateModel {
	return model.NewNucleiTemplateModel(s.db)
}

func (s *MongoStorage) FingerprintModel() *model.FingerprintModel {
	return model.NewFingerprintModel(s.db)
}

func (s *MongoStorage) NotifyConfigModel() *model.NotifyConfigModel {
	return model.NewNotifyConfigModel(s.db)
}

// RedisCache implements Cache interface using Redis
type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) Cache {
	return &RedisCache{client: client}
}

func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, expiration int64) error {
	return c.client.Set(ctx, key, value, 0).Err()
}

func (c *RedisCache) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

func (c *RedisCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	return c.client.Exists(ctx, keys...).Result()
}

func (c *RedisCache) HGet(ctx context.Context, key, field string) (string, error) {
	return c.client.HGet(ctx, key, field).Result()
}

func (c *RedisCache) HSet(ctx context.Context, key string, values ...interface{}) error {
	return c.client.HSet(ctx, key, values...).Err()
}

func (c *RedisCache) HDel(ctx context.Context, key string, fields ...string) error {
	return c.client.HDel(ctx, key, fields...).Err()
}

// TaskRPCClient implements RPCClient interface
type TaskRPCClient struct {
	client pb.TaskServiceClient
}

func NewTaskRPCClient(client pb.TaskServiceClient) RPCClient {
	return &TaskRPCClient{client: client}
}

func (r *TaskRPCClient) TaskService() pb.TaskServiceClient {
	return r.client
}

// DefaultValidator implements Validator interface
type DefaultValidator struct{}

func NewDefaultValidator() Validator {
	return &DefaultValidator{}
}

func (v *DefaultValidator) ValidateStruct(s interface{}) error {
	// Implementation would use a validation library like go-playground/validator
	return nil
}

func (v *DefaultValidator) ValidateVar(field interface{}, tag string) error {
	// Implementation would use a validation library like go-playground/validator
	return nil
}
