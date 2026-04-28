package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// JSFinderResult JSFinder 扫描结果
type JSFinderResult struct {
	Id               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	WorkspaceId      string             `bson:"workspace_id" json:"workspaceId"`
	MainTaskId       string             `bson:"main_task_id,omitempty" json:"mainTaskId,omitempty"`
	TaskName         string             `bson:"task_name,omitempty" json:"taskName,omitempty"`
	Authority        string             `bson:"authority" json:"authority"`
	Host             string             `bson:"host" json:"host"`
	Port             int                `bson:"port" json:"port"`
	URL              string             `bson:"url" json:"url"`
	Severity         string             `bson:"severity" json:"severity"`
	VulName          string             `bson:"vul_name" json:"vulName"`
	Result           string             `bson:"result" json:"result"`
	Tags             []string           `bson:"tags" json:"tags"`
	MatcherName      string             `bson:"matcher_name,omitempty" json:"matcherName,omitempty"`
	ExtractedResults []string           `bson:"extracted_results,omitempty" json:"extractedResults,omitempty"`
	CurlCommand      string             `bson:"curl_command,omitempty" json:"curlCommand,omitempty"`
	Request          string             `bson:"request,omitempty" json:"request,omitempty"`
	Response         string             `bson:"response,omitempty" json:"response,omitempty"`
	CreateTime       time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime       time.Time          `bson:"update_time" json:"updateTime"`
}

// JSFinderResultModel JSFinder 结果模型
type JSFinderResultModel struct {
	coll *mongo.Collection
}

// NewJSFinderResultModel 多租户模型实例化
func NewJSFinderResultModel(db *mongo.Database, workspaceId string) *JSFinderResultModel {
	coll := db.Collection(workspaceId + "_jsfinder")
	return &JSFinderResultModel{coll: coll}
}

// InsertMany 批量插入
func (m *JSFinderResultModel) InsertMany(ctx context.Context, results []*JSFinderResult) error {
	if len(results) == 0 {
		return nil
	}
	docs := make([]interface{}, len(results))
	now := time.Now()
	for i, r := range results {
		if r.Id.IsZero() {
			r.Id = primitive.NewObjectID()
		}
		if r.CreateTime.IsZero() {
			r.CreateTime = now
		}
		r.UpdateTime = now
		docs[i] = r
	}
	opts := options.InsertMany().SetOrdered(false)
	_, err := m.coll.InsertMany(ctx, docs, opts)
	return err
}

// EnsureIndexes 确保索引存在
func (m *JSFinderResultModel) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "host", Value: 1}}},
		{Keys: bson.D{{Key: "main_task_id", Value: 1}}},
		{Keys: bson.D{{Key: "severity", Value: 1}}},
		{Keys: bson.D{{Key: "url", Value: 1}}},
		// 创建唯一索引，防止单个任务中对相同的 URL 重复插入
		{
			Keys: bson.D{
				{Key: "main_task_id", Value: 1},
				{Key: "authority", Value: 1},
				{Key: "url", Value: 1},
				{Key: "vul_name", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetBackground(true),
		},
	}
	_, err := m.coll.Indexes().CreateMany(ctx, indexes)
	return err
}

// Find 查询列表
func (m *JSFinderResultModel) Find(ctx context.Context, filter bson.M, opt *options.FindOptions) ([]*JSFinderResult, error) {
	cursor, err := m.coll.Find(ctx, filter, opt)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var results []*JSFinderResult
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// Count 计数
func (m *JSFinderResultModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// DeleteMany 批量删除
func (m *JSFinderResultModel) DeleteMany(ctx context.Context, filter bson.M) (int64, error) {
	res, err := m.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
