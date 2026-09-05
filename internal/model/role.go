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

	// 内置角色名，与 user.role 字段取值一致
	RoleSuperadmin = "superadmin"
	RoleAdmin      = "admin"
	RoleUser       = "user"
)

// Role 角色定义
type Role struct {
	Id           interface{} `bson:"_id,omitempty" json:"id"`
	Name         string      `bson:"name" json:"name"`                // 角色名: superadmin / admin / user / 自定义
	DisplayName  string      `bson:"display_name" json:"displayName"` // 显示名
	Description  string      `bson:"description,omitempty" json:"description"`
	MenuPaths    []string    `bson:"menu_paths" json:"menuPaths"`       // 可访问的菜单路径列表
	IsBuiltIn    bool        `bson:"is_built_in" json:"isBuiltIn"`      // 是否内置角色（内置角色不允许删除）
	IsSuperadmin bool        `bson:"is_superadmin" json:"isSuperadmin"` // 是否拥有管理员接口权限
	CreateTime   time.Time   `bson:"create_time" json:"createTime"`
	UpdateTime   time.Time   `bson:"update_time" json:"updateTime"`
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

// EnsureBuiltInRoles 初始化内置角色。
// 已存在的角色只补齐内置标记与管理员标记，不覆盖管理员已调整过的 menu_paths；
// superadmin 例外——始终强制全量菜单，避免误配置把自己锁在系统外。
func (m *RoleModel) EnsureBuiltInRoles(ctx context.Context) error {
	all := MenuPathList()
	builtIns := []Role{
		{Name: RoleSuperadmin, DisplayName: "超级管理员", Description: "拥有全部菜单与管理接口权限", MenuPaths: all, IsBuiltIn: true, IsSuperadmin: true},
		{Name: RoleAdmin, DisplayName: "管理员", Description: "拥有全部菜单与管理接口权限", MenuPaths: all, IsBuiltIn: true, IsSuperadmin: true},
		{Name: RoleUser, DisplayName: "普通用户", Description: "可查看资产与漏洞、创建扫描任务", MenuPaths: DefaultUserMenuPaths(), IsBuiltIn: true, IsSuperadmin: false},
	}

	for i := range builtIns {
		r := builtIns[i]
		set := bson.M{
			"is_built_in":   true,
			"is_superadmin": r.IsSuperadmin,
			"update_time":   time.Now(),
		}
		if r.Name == RoleSuperadmin {
			set["menu_paths"] = all
		}
		update := bson.M{
			"$set": set,
			"$setOnInsert": bson.M{
				"name":         r.Name,
				"display_name": r.DisplayName,
				"description":  r.Description,
				"menu_paths":   r.MenuPaths,
				"create_time":  time.Now(),
			},
		}
		if r.Name == RoleSuperadmin {
			delete(update["$setOnInsert"].(bson.M), "menu_paths")
		}
		if _, err := m.coll.UpdateOne(ctx, bson.M{"name": r.Name}, update, options.Update().SetUpsert(true)); err != nil {
			return err
		}

		// 旧版本可能存在缺少 menu_paths 字段的角色文档，补齐默认值避免菜单全空
		if _, err := m.coll.UpdateOne(ctx,
			bson.M{"name": r.Name, "menu_paths": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{"menu_paths": r.MenuPaths}},
		); err != nil {
			return err
		}
	}
	return nil
}

// CountUsersByRole 统计仍在使用指定角色的用户数，用于删除角色前的占用检查
func (m *RoleModel) CountUsersByRole(ctx context.Context, roleName string) (int64, error) {
	return m.coll.Database().Collection(UserCollection).CountDocuments(ctx, bson.M{"role": roleName})
}

// MenuPathList 返回系统全部可配置菜单路径（与前端路由一致）
func MenuPathList() []string {
	return []string{
		"/dashboard",
		"/asset-management/space-search",
		"/task",
		"/task/create",
		"/task/edit/:id",
		"/task/detail",
		"/task/template",
		"/report",
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
		"/high-risk-filter",
		"/user",
		"/registration-config",
		"/organization",
		"/settings-branding",
		"/settings-role",
	}
}

// DefaultUserMenuPaths 内置 user 角色的默认菜单：资产/任务可见，系统管理不可见
func DefaultUserMenuPaths() []string {
	return []string{
		"/dashboard",
		"/asset-management/space-search",
		"/task",
		"/task/create",
		"/task/edit/:id",
		"/task/detail",
		"/task/template",
		"/report",
		"/cron-task",
		"/cron-task/create",
		"/cron-task/edit/:id",
		"/worker",
	}
}

// IsValidMenuPath 校验菜单路径是否在系统可配置范围内
func IsValidMenuPath(path string) bool {
	for _, p := range MenuPathList() {
		if p == path {
			return true
		}
	}
	return false
}
