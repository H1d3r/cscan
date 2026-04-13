package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SubdomainDict 子域名字典
type SubdomainDict struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`               // 字典名称
	Description string             `bson:"description" json:"description"` // 描述
	Content     string             `bson:"content" json:"content"`         // 字典内容（每行一个子域名前缀）
	WordCount   int                `bson:"word_count" json:"wordCount"`    // 词条数量
	Enabled     bool               `bson:"enabled" json:"enabled"`         // 是否启用
	IsBuiltin   bool               `bson:"is_builtin" json:"isBuiltin"`    // 是否内置字典
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

// SubdomainDictModel 子域名字典模型
type SubdomainDictModel struct {
	coll *mongo.Collection
}

func NewSubdomainDictModel(db *mongo.Database) *SubdomainDictModel {
	return &SubdomainDictModel{
		coll: db.Collection("subdomain_dict"),
	}
}

func (m *SubdomainDictModel) Insert(ctx context.Context, doc *SubdomainDict) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *SubdomainDictModel) FindAll(ctx context.Context, page, pageSize int) ([]SubdomainDict, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "is_builtin", Value: -1}, {Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []SubdomainDict
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *SubdomainDictModel) Count(ctx context.Context) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{})
}

func (m *SubdomainDictModel) FindEnabled(ctx context.Context) ([]SubdomainDict, error) {
	opts := options.Find().SetSort(bson.D{{Key: "is_builtin", Value: -1}, {Key: "name", Value: 1}})
	cursor, err := m.coll.Find(ctx, bson.M{"enabled": true}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []SubdomainDict
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *SubdomainDictModel) FindById(ctx context.Context, id string) (*SubdomainDict, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc SubdomainDict
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	return &doc, err
}

func (m *SubdomainDictModel) FindByIds(ctx context.Context, ids []string) ([]SubdomainDict, error) {
	var oids []primitive.ObjectID
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			continue
		}
		oids = append(oids, oid)
	}
	if len(oids) == 0 {
		return nil, nil
	}

	cursor, err := m.coll.Find(ctx, bson.M{"_id": bson.M{"$in": oids}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []SubdomainDict
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *SubdomainDictModel) FindByName(ctx context.Context, name string) (*SubdomainDict, error) {
	var doc SubdomainDict
	err := m.coll.FindOne(ctx, bson.M{"name": name}).Decode(&doc)
	return &doc, err
}

func (m *SubdomainDictModel) Update(ctx context.Context, id string, doc *SubdomainDict) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"name":        doc.Name,
		"description": doc.Description,
		"content":     doc.Content,
		"word_count":  doc.WordCount,
		"enabled":     doc.Enabled,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *SubdomainDictModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// DeleteNonBuiltin 删除所有非内置字典
func (m *SubdomainDictModel) DeleteNonBuiltin(ctx context.Context) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{"is_builtin": bson.M{"$ne": true}})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// UpsertByName 根据名称更新或插入字典
func (m *SubdomainDictModel) UpsertByName(ctx context.Context, doc *SubdomainDict) error {
	now := time.Now()
	filter := bson.M{"name": doc.Name}
	update := bson.M{
		"$set": bson.M{
			"description": doc.Description,
			"content":     doc.Content,
			"word_count":  doc.WordCount,
			"enabled":     doc.Enabled,
			"is_builtin":  doc.IsBuiltin,
			"update_time": now,
		},
		"$setOnInsert": bson.M{
			"create_time": now,
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}
