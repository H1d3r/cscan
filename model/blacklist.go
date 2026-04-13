package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// BlacklistConfig 全局黑名单配置
type BlacklistConfig struct {
	Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Rules      string             `bson:"rules" json:"rules"`   // 黑名单规则，每行一条
	Status     string             `bson:"status" json:"status"` // enable/disable
	CreateTime time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime time.Time          `bson:"update_time" json:"updateTime"`
}

// BlacklistConfigModel 黑名单配置模型
type BlacklistConfigModel struct {
	coll *mongo.Collection
}

// NewBlacklistConfigModel 创建黑名单配置模型
func NewBlacklistConfigModel(db *mongo.Database) *BlacklistConfigModel {
	coll := db.Collection("blacklist_config")

	// 创建索引
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes)

	return &BlacklistConfigModel{coll: coll}
}

// Get 获取黑名单配置（只有一条记录）
func (m *BlacklistConfigModel) Get(ctx context.Context) (*BlacklistConfig, error) {
	var doc BlacklistConfig
	err := m.coll.FindOne(ctx, bson.M{}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		// 返回默认配置
		return &BlacklistConfig{
			Rules:  "",
			Status: "enable",
		}, nil
	}
	return &doc, err
}

// Save 保存黑名单配置（Upsert）
func (m *BlacklistConfigModel) Save(ctx context.Context, doc *BlacklistConfig) error {
	now := time.Now()
	doc.UpdateTime = now

	// 查找现有记录
	existing, err := m.Get(ctx)
	if err != nil && err != mongo.ErrNoDocuments {
		return err
	}

	if existing != nil && !existing.Id.IsZero() {
		// 更新现有记录
		update := bson.M{
			"rules":       doc.Rules,
			"status":      doc.Status,
			"update_time": now,
		}
		_, err = m.coll.UpdateOne(ctx, bson.M{"_id": existing.Id}, bson.M{"$set": update})
		return err
	}

	// 插入新记录
	doc.Id = primitive.NewObjectID()
	doc.CreateTime = now
	if doc.Status == "" {
		doc.Status = "enable"
	}
	_, err = m.coll.InsertOne(ctx, doc)
	return err
}

// GetRules 获取启用的黑名单规则列表
func (m *BlacklistConfigModel) GetRules(ctx context.Context) ([]string, error) {
	doc, err := m.Get(ctx)
	if err != nil {
		return nil, err
	}

	if doc.Status != "enable" || doc.Rules == "" {
		return nil, nil
	}

	return ParseBlacklistRules(doc.Rules), nil
}

// ParseBlacklistRules 解析黑名单规则字符串为规则列表
func ParseBlacklistRules(rulesStr string) []string {
	if rulesStr == "" {
		return nil
	}

	var rules []string
	lines := splitLines(rulesStr)
	for _, line := range lines {
		line = trimSpace(line)
		// 跳过空行和注释
		if line == "" || line[0] == '#' {
			continue
		}
		rules = append(rules, line)
	}
	return rules
}

// splitLines 按行分割字符串
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// trimSpace 去除首尾空白
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// Upsert 更新或插入配置
func (m *BlacklistConfigModel) Upsert(ctx context.Context, doc *BlacklistConfig) error {
	now := time.Now()
	doc.UpdateTime = now

	filter := bson.M{}
	update := bson.M{
		"$set": bson.M{
			"rules":       doc.Rules,
			"status":      doc.Status,
			"update_time": now,
		},
		"$setOnInsert": bson.M{
			"_id":         primitive.NewObjectID(),
			"create_time": now,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}
