package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// NotifyConfig 通知配置
type NotifyConfig struct {
	Id              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name            string             `bson:"name" json:"name"`                         // 配置名称
	Provider        string             `bson:"provider" json:"provider"`                 // 提供者类型: smtp, feishu, dingtalk, wecom, slack, discord, telegram, teams, gotify, webhook
	Config          string             `bson:"config" json:"config"`                     // JSON格式的配置详情
	Status          string             `bson:"status" json:"status"`                     // enable/disable
	MessageTemplate string             `bson:"message_template" json:"messageTemplate"`  // 自定义消息模板
	CreateTime      time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime      time.Time          `bson:"update_time" json:"updateTime"`
	// 高危过滤配置
	HighRiskFilter *HighRiskFilter `bson:"high_risk_filter,omitempty" json:"highRiskFilter,omitempty"`
}

// HighRiskFilter 高危过滤配置
type HighRiskFilter struct {
	Enabled            bool     `bson:"enabled" json:"enabled"`                         // 是否启用高危过滤，false时全部通知
	HighRiskFingerprints []string `bson:"high_risk_fingerprints" json:"highRiskFingerprints"` // 高危指纹列表
	HighRiskPorts        []int    `bson:"high_risk_ports" json:"highRiskPorts"`               // 高危端口列表
	HighRiskPocSeverities []string `bson:"high_risk_poc_severities" json:"highRiskPocSeverities"` // 高危POC严重级别: critical, high
}

// NotifyConfigModel 通知配置模型
type NotifyConfigModel struct {
	coll *mongo.Collection
}

// NewNotifyConfigModel 创建通知配置模型
func NewNotifyConfigModel(db *mongo.Database) *NotifyConfigModel {
	coll := db.Collection("notify_config")

	// 创建索引
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "provider", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes)

	return &NotifyConfigModel{coll: coll}
}

// Insert 插入配置
func (m *NotifyConfigModel) Insert(ctx context.Context, doc *NotifyConfig) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	if doc.Status == "" {
		doc.Status = "enable"
	}
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

// FindById 根据ID查找
func (m *NotifyConfigModel) FindById(ctx context.Context, id string) (*NotifyConfig, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc NotifyConfig
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	return &doc, err
}

// FindByProvider 根据提供者类型查找
func (m *NotifyConfigModel) FindByProvider(ctx context.Context, provider string) (*NotifyConfig, error) {
	var doc NotifyConfig
	err := m.coll.FindOne(ctx, bson.M{"provider": provider, "status": "enable"}).Decode(&doc)
	return &doc, err
}

// FindAll 查找所有配置
func (m *NotifyConfigModel) FindAll(ctx context.Context) ([]NotifyConfig, error) {
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	cursor, err := m.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NotifyConfig
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindEnabled 查找所有启用的配置
func (m *NotifyConfigModel) FindEnabled(ctx context.Context) ([]NotifyConfig, error) {
	cursor, err := m.coll.Find(ctx, bson.M{"status": "enable"})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NotifyConfig
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// Update 更新配置
func (m *NotifyConfigModel) Update(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// Delete 删除配置
func (m *NotifyConfigModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// Upsert 根据provider更新或插入
func (m *NotifyConfigModel) Upsert(ctx context.Context, doc *NotifyConfig) error {
	now := time.Now()
	doc.UpdateTime = now

	filter := bson.M{"provider": doc.Provider}
	setFields := bson.M{
		"name":             doc.Name,
		"config":           doc.Config,
		"status":           doc.Status,
		"message_template": doc.MessageTemplate,
		"update_time":      now,
	}
	// 添加高危过滤配置
	if doc.HighRiskFilter != nil {
		setFields["high_risk_filter"] = doc.HighRiskFilter
	}

	update := bson.M{
		"$set": setFields,
		"$setOnInsert": bson.M{
			"_id":         primitive.NewObjectID(),
			"provider":    doc.Provider,
			"create_time": now,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}
