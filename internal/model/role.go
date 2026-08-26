package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	RoleCollection = "role"
)

// Role 角色定义
type Role struct {
	Id            interface{} `bson:"_id,omitempty" json:"id"`
	Name          string      `bson:"name" json:"name"`            // 角色名: superadmin / admin / user / readonly / custom_xxx
	DisplayName   string      `bson:"display_name" json:"displayName"` // 显示名
	Description   string      `bson:"description,omitempty" json:"description"`
	MenuPaths     []string    `bson:"menu_paths" json:"menuPaths"`       // 可访问的菜单路径列表
	IsBuiltIn     bool        `bson:"is_built_in" json:"isBuiltIn"`      // 是否内置角色（内置角色不允许删除）
	IsSuperadmin  bool        `bson:"is_superadmin" json:"isSuperadmin"` // 是否拥有超级管理员全部权限
	CreateTime    time.Time   `bson:"create_time" json:"createTime"`
	UpdateTime    time.Time   `bson:"update_time" json:"updateTime"`
}

// RoleModel 角色数据访问
type RoleModel struct {
	coll *mongo.Collection
}

func NewRoleModel(db *mongo.Database) *RoleModel {
	return &RoleModel{
		coll: db.Collection(RoleCollection),
	}
}

func (m *RoleModel) EnsureIndexes() error {
	idx := mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	_, err := m.coll.Indexes().CreateOne(context.Background(), idx)
	return err
}

func (m *RoleModel) FindByName(ctx context.Context, name string) (*Role, error) {
	var doc Role
	err := m.coll.FindOne(ctx, bson.M{"name": name}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *RoleModel) FindAll(ctx context.Context) ([]Role, error) {
	cursor, err := m.coll.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "is_built_in", Value: -1}, {Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []Role
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *RoleModel) Insert(ctx context.Context, doc *Role) error {
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *RoleModel) Update(ctx context.Context, name string, update bson.M) error {
	update["update_time"] = time.Now()
	_, err := m.coll.UpdateOne(ctx, bson.M{"name": name}, bson.M{"$set": update})
	return err
}

func (m *RoleModel) Delete(ctx context.Context, name string) error {
	_, err := m.coll.DeleteOne(ctx, bson.M{"name": name})
	return err
}

// MenuPathList 返回系统全部可配置菜单路径（与前端路由一致）
func MenuPathList() []string {
	return []string{
		"/dashboard",
		"/asset-management/space-search",
		"/asset-management/exposure/dir",
		"/asset-management/exposure/js",
		"/asset-management/risk/sensitive-info",
		"/asset-management/risk/vuln",
		"/task",
		"/task/create",
		"/task/edit/:id",
		"/task/detail",
		"/task/template",
		"/space-engine/online-search",
		"/space-engine/api-config",
		"/space-engine/cron-task",
		"/cron-task",
		"/cron-task/create",
		"/cron-task/edit/:id",
		"/settings-subfinder",
		"/poc",
		"/fingerprint",
		"/blacklist",
		"/ai-config",
		"/worker",
		"/worker-logs",
		"/settings-notify",
		"/settings-reverify",
		"/high-risk-filter",
		"/user",
		"/registration-config",
		"/organization",
		"/settings-branding",
		"/settings-role",
	}
}
