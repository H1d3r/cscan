package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ==================== 接口定义 ====================

// Identifiable 可标识接口
type Identifiable interface {
	GetId() primitive.ObjectID
	SetId(id primitive.ObjectID)
}

// Timestamped 时间戳接口
type Timestamped interface {
	SetCreateTime(t time.Time)
	SetUpdateTime(t time.Time)
}

// ==================== 分页参数 ====================

// PageParams 分页参数
type PageParams struct {
	Page     int    // 页码（从1开始）
	PageSize int    // 每页数量
	SortBy   string // 排序字段
	SortDesc bool   // 是否降序
}

// DefaultPageParams 默认分页参数
func DefaultPageParams() PageParams {
	return PageParams{
		Page:     1,
		PageSize: 20,
		SortBy:   "create_time",
		SortDesc: true,
	}
}

// ToFindOptions 转换为 MongoDB FindOptions
func (p PageParams) ToFindOptions() *options.FindOptions {
	opts := options.Find()
	if p.Page > 0 && p.PageSize > 0 {
		opts.SetSkip(int64((p.Page - 1) * p.PageSize))
		opts.SetLimit(int64(p.PageSize))
	}
	if p.SortBy != "" {
		sortOrder := 1
		if p.SortDesc {
			sortOrder = -1
		}
		opts.SetSort(bson.D{{Key: p.SortBy, Value: sortOrder}})
	}
	return opts
}

// ==================== 查询结果 ====================

// PageResult 分页结果
type PageResult[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// NewPageResult 创建分页结果
func NewPageResult[T any](items []T, total int64, params PageParams) *PageResult[T] {
	return &PageResult[T]{
		Items:    items,
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}
}

// BaseModel 泛型基础模型
type BaseModel[T any] struct {
	Coll *mongo.Collection
}

// NewBaseModel 创建基础模型
func NewBaseModel[T any](coll *mongo.Collection) *BaseModel[T] {
	return &BaseModel[T]{Coll: coll}
}

// Insert 插入文档
func (m *BaseModel[T]) Insert(ctx context.Context, doc *T) error {
	_, err := m.Coll.InsertOne(ctx, doc)
	return err
}

// FindById 根据ID查找
func (m *BaseModel[T]) FindById(ctx context.Context, id string) (*T, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc T
	err = m.Coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// FindOne 查找单个文档
func (m *BaseModel[T]) FindOne(ctx context.Context, filter bson.M) (*T, error) {
	var doc T
	err := m.Coll.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// Find 查找多个文档
func (m *BaseModel[T]) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]T, error) {
	return m.FindWithSort(ctx, filter, page, pageSize, "create_time", -1)
}

// FindWithSort 带排序查找
func (m *BaseModel[T]) FindWithSort(ctx context.Context, filter bson.M, page, pageSize int, sortField string, sortOrder int) ([]T, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: sortField, Value: sortOrder}})

	cursor, err := m.Coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []T
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindAll 查找所有文档
func (m *BaseModel[T]) FindAll(ctx context.Context) ([]T, error) {
	return m.Find(ctx, bson.M{}, 0, 0)
}

// Count 统计数量
func (m *BaseModel[T]) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.Coll.CountDocuments(ctx, filter)
}

// UpdateById 根据ID更新
func (m *BaseModel[T]) UpdateById(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.Coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// UpdateOne 更新单个文档
func (m *BaseModel[T]) UpdateOne(ctx context.Context, filter bson.M, update bson.M) error {
	update["update_time"] = time.Now()
	_, err := m.Coll.UpdateOne(ctx, filter, bson.M{"$set": update})
	return err
}

// DeleteById 根据ID删除
func (m *BaseModel[T]) DeleteById(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.Coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// DeleteMany 批量删除
func (m *BaseModel[T]) DeleteMany(ctx context.Context, filter bson.M) (int64, error) {
	result, err := m.Coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// BatchDeleteByIds 根据ID列表批量删除
func (m *BaseModel[T]) BatchDeleteByIds(ctx context.Context, ids []string) (int64, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			continue
		}
		oids = append(oids, oid)
	}
	if len(oids) == 0 {
		return 0, nil
	}
	return m.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": oids}})
}

// EnsureIndexes 创建索引
func (m *BaseModel[T]) EnsureIndexes(ctx context.Context, indexes []mongo.IndexModel) error {
	if len(indexes) == 0 {
		return nil
	}
	_, err := m.Coll.Indexes().CreateMany(ctx, indexes)
	return err
}

// BulkWrite 批量写入
func (m *BaseModel[T]) BulkWrite(ctx context.Context, models []mongo.WriteModel) (*mongo.BulkWriteResult, error) {
	if len(models) == 0 {
		return nil, nil
	}
	opts := options.BulkWrite().SetOrdered(false)
	return m.Coll.BulkWrite(ctx, models, opts)
}

// FindWithPage 分页查询
func (m *BaseModel[T]) FindWithPage(ctx context.Context, filter bson.M, params PageParams) (*PageResult[T], error) {
	// 查询总数
	total, err := m.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// 查询数据
	cursor, err := m.Coll.Find(ctx, filter, params.ToFindOptions())
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []T
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	return NewPageResult(docs, total, params), nil
}

// Exists 检查是否存在
func (m *BaseModel[T]) Exists(ctx context.Context, filter bson.M) (bool, error) {
	count, err := m.Coll.CountDocuments(ctx, filter, options.Count().SetLimit(1))
	return count > 0, err
}

// Upsert 插入或更新
func (m *BaseModel[T]) Upsert(ctx context.Context, filter bson.M, update bson.M) error {
	update["update_time"] = time.Now()
	opts := options.Update().SetUpsert(true)
	_, err := m.Coll.UpdateOne(ctx, filter, bson.M{"$set": update}, opts)
	return err
}

// Aggregate 聚合查询
func (m *BaseModel[T]) Aggregate(ctx context.Context, pipeline mongo.Pipeline) ([]bson.M, error) {
	cursor, err := m.Coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// CountByField 按字段统计
func (m *BaseModel[T]) CountByField(ctx context.Context, field string, limit int) ([]FieldCount, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$" + field},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
	}
	if limit > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$limit", Value: limit}})
	}

	cursor, err := m.Coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []FieldCount
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// FieldCount 字段统计结果
type FieldCount struct {
	Field interface{} `bson:"_id"`
	Count int         `bson:"count"`
}

// Clear 清空集合
func (m *BaseModel[T]) Clear(ctx context.Context) (int64, error) {
	result, err := m.Coll.DeleteMany(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}
