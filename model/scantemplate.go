package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ScanTemplate 扫描配置模板
type ScanTemplate struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`               // 模板名称
	Description string             `bson:"description" json:"description"` // 模板描述
	Category    string             `bson:"category" json:"category"`       // 分类：quick/standard/full/custom
	Tags        []string           `bson:"tags" json:"tags"`               // 标签
	Config      string             `bson:"config" json:"config"`           // 扫描配置JSON
	IsBuiltin   bool               `bson:"is_builtin" json:"isBuiltin"`    // 是否内置模板
	UseCount    int                `bson:"use_count" json:"useCount"`      // 使用次数
	SortNumber  int                `bson:"sort_number" json:"sortNumber"`  // 排序号
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

// GetId 实现 Identifiable 接口
func (s *ScanTemplate) GetId() primitive.ObjectID {
	return s.Id
}

// SetId 实现 Identifiable 接口
func (s *ScanTemplate) SetId(id primitive.ObjectID) {
	s.Id = id
}

// SetCreateTime 实现 Timestamped 接口
func (s *ScanTemplate) SetCreateTime(t time.Time) {
	s.CreateTime = t
}

// SetUpdateTime 实现 Timestamped 接口
func (s *ScanTemplate) SetUpdateTime(t time.Time) {
	s.UpdateTime = t
}

// ScanTemplateModel 扫描模板数据模型
type ScanTemplateModel struct {
	*BaseModel[ScanTemplate]
}

// NewScanTemplateModel 创建扫描模板模型
func NewScanTemplateModel(db *mongo.Database) *ScanTemplateModel {
	coll := db.Collection("scan_template")

	// 创建索引
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "name", Value: 1}}},
		{Keys: bson.D{{Key: "category", Value: 1}}},
		{Keys: bson.D{{Key: "tags", Value: 1}}},
		{Keys: bson.D{{Key: "is_builtin", Value: 1}}},
		{Keys: bson.D{{Key: "use_count", Value: -1}}},
		{Keys: bson.D{{Key: "sort_number", Value: 1}}},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes)

	return &ScanTemplateModel{
		BaseModel: NewBaseModel[ScanTemplate](coll),
	}
}

// FindByCategory 按分类查找模板
func (m *ScanTemplateModel) FindByCategory(ctx context.Context, category string, page, pageSize int) ([]ScanTemplate, error) {
	filter := bson.M{"category": category}
	return m.FindWithSort(ctx, filter, page, pageSize, "sort_number", 1)
}

// FindBuiltinTemplates 查找内置模板
func (m *ScanTemplateModel) FindBuiltinTemplates(ctx context.Context) ([]ScanTemplate, error) {
	filter := bson.M{"is_builtin": true}
	return m.FindWithSort(ctx, filter, 0, 0, "sort_number", 1)
}

// SearchTemplates 搜索模板（内部平台，所有模板对所有用户可见）
func (m *ScanTemplateModel) SearchTemplates(ctx context.Context, keyword, category string, tags []string, page, pageSize int) ([]ScanTemplate, int64, error) {
	filter := bson.M{}

	if keyword != "" {
		filter["$or"] = []bson.M{
			{"name": bson.M{"$regex": keyword, "$options": "i"}},
			{"description": bson.M{"$regex": keyword, "$options": "i"}},
		}
	}

	if category != "" {
		filter["category"] = category
	}

	if len(tags) > 0 {
		filter["tags"] = bson.M{"$in": tags}
	}

	total, err := m.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	templates, err := m.FindWithSort(ctx, filter, page, pageSize, "sort_number", 1)
	if err != nil {
		return nil, 0, err
	}

	return templates, total, nil
}

// IncrUseCount 增加使用次数
func (m *ScanTemplateModel) IncrUseCount(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.Coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{
		"$inc": bson.M{"use_count": 1},
		"$set": bson.M{"update_time": time.Now()},
	})
	return err
}

// GetCategories 获取所有分类
func (m *ScanTemplateModel) GetCategories(ctx context.Context) ([]string, error) {
	results, err := m.Coll.Distinct(ctx, "category", bson.M{})
	if err != nil {
		return nil, err
	}

	categories := make([]string, 0, len(results))
	for _, r := range results {
		if s, ok := r.(string); ok && s != "" {
			categories = append(categories, s)
		}
	}
	return categories, nil
}

// GetAllTags 获取所有标签
func (m *ScanTemplateModel) GetAllTags(ctx context.Context) ([]string, error) {
	results, err := m.Coll.Distinct(ctx, "tags", bson.M{})
	if err != nil {
		return nil, err
	}

	tags := make([]string, 0, len(results))
	for _, r := range results {
		if s, ok := r.(string); ok && s != "" {
			tags = append(tags, s)
		}
	}
	return tags, nil
}

// Update 更新模板
func (m *ScanTemplateModel) Update(ctx context.Context, id string, template *ScanTemplate) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"name":        template.Name,
		"description": template.Description,
		"category":    template.Category,
		"tags":        template.Tags,
		"config":      template.Config,
		"sort_number": template.SortNumber,
		"update_time": time.Now(),
	}
	_, err = m.Coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// ExistsName 检查名称是否存在
func (m *ScanTemplateModel) ExistsName(ctx context.Context, name string, excludeId string) (bool, error) {
	filter := bson.M{"name": name}
	if excludeId != "" {
		oid, err := primitive.ObjectIDFromHex(excludeId)
		if err == nil {
			filter["_id"] = bson.M{"$ne": oid}
		}
	}
	return m.Exists(ctx, filter)
}

// BatchInsert 批量插入模板
func (m *ScanTemplateModel) BatchInsert(ctx context.Context, templates []ScanTemplate) error {
	if len(templates) == 0 {
		return nil
	}

	now := time.Now()
	docs := make([]interface{}, len(templates))
	for i := range templates {
		if templates[i].Id.IsZero() {
			templates[i].Id = primitive.NewObjectID()
		}
		templates[i].CreateTime = now
		templates[i].UpdateTime = now
		docs[i] = templates[i]
	}

	_, err := m.Coll.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	return err
}


