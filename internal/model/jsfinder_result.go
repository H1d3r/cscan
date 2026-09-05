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

	// AI研判字段
	AIStatus     string    `bson:"ai_status,omitempty" json:"aiStatus,omitempty"` // pending/completed，空值等价于pending
	AIResult     string    `bson:"ai_result,omitempty" json:"aiResult,omitempty"` // risk/no_risk
	AIAnalyzedAt time.Time `bson:"ai_analyzed_at,omitempty" json:"aiAnalyzedAt,omitempty"`
	AIReason     string    `bson:"ai_reason,omitempty" json:"aiReason,omitempty"` // AI判断理由
}

// JSFinderResultModel JSFinder 结果模型
type JSFinderResultModel struct {
	coll *mongo.Collection
}

// NewJSFinderResultModel creates a new JSFinderResultModel
func NewJSFinderResultModel(db *mongo.Database) *JSFinderResultModel {
	coll := db.Collection("jsfinder")
	return &JSFinderResultModel{coll: coll}
}

// InsertMany 批量插入（保留以兼容旧调用方，新代码建议使用 UpsertMany）
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

// UpsertMany 批量 upsert：按唯一键（main_task_id, authority, url, vul_name, result）匹配，
// 已存在则仅刷新 update_time 并更新可变字段（保留 AI 研判结果等人工标注字段），
// 不存在则插入新记录。防止循环扫描导致重复脏数据。
func (m *JSFinderResultModel) UpsertMany(ctx context.Context, results []*JSFinderResult) error {
	if len(results) == 0 {
		return nil
	}
	now := time.Now()

	// 为避免单次批量过大，每 200 条分批执行
	const batchSize = 200
	for start := 0; start < len(results); start += batchSize {
		end := start + batchSize
		if end > len(results) {
			end = len(results)
		}
		batch := results[start:end]

		var models []mongo.WriteModel
		for _, r := range batch {
			if r.CreateTime.IsZero() {
				r.CreateTime = now
			}
			// 唯一键过滤条件
			filter := bson.M{
				"main_task_id": r.MainTaskId,
				"authority":    r.Authority,
				"url":          r.URL,
				"vul_name":     r.VulName,
				"result":       r.Result,
			}

			// $setOnInsert：仅插入时设置的不可变字段（不可与 $set 字段重叠，否则 MongoDB 报冲突）
			setOnInsert := bson.M{
				"_id":         primitive.NewObjectID(),
				"create_time": r.CreateTime,
			}
			// AI 研判字段仅在插入时设置（非空时），保护已有标注不被覆盖
			if r.AIStatus != "" {
				setOnInsert["ai_status"] = r.AIStatus
			}
			if r.AIResult != "" {
				setOnInsert["ai_result"] = r.AIResult
			}
			if r.AIReason != "" {
				setOnInsert["ai_reason"] = r.AIReason
			}
			if !r.AIAnalyzedAt.IsZero() {
				setOnInsert["ai_analyzed_at"] = r.AIAnalyzedAt
			}

			// $set：每次都更新的可变字段（update_time 始终刷新，
			// 其他扫描相关字段用新值覆盖，但 AI 研判字段不触碰，保护人工标注结果）
			set := bson.M{
				"update_time": now,
				// 这些字段在重新扫描时可能变化，覆盖更新
				"host":              r.Host,
				"port":              r.Port,
				"severity":          r.Severity,
				"tags":              r.Tags,
				"matcher_name":      r.MatcherName,
				"extracted_results": r.ExtractedResults,
				"curl_command":      r.CurlCommand,
				"request":           r.Request,
				"response":          r.Response,
				"task_name":         r.TaskName,
			}

			update := bson.M{
				"$setOnInsert": setOnInsert,
				"$set":         set,
			}

			model := mongo.NewUpdateOneModel().
				SetFilter(filter).
				SetUpdate(update).
				SetUpsert(true)
			models = append(models, model)
		}

		opts := options.BulkWrite().SetOrdered(false)
		if _, err := m.coll.BulkWrite(ctx, models, opts); err != nil {
			// Ordered=false 时部分失败不影响其他条目，记录日志但不中断
			return err
		}
	}
	return nil
}

// EnsureIndexes 确保索引存在
func (m *JSFinderResultModel) EnsureIndexes(ctx context.Context) error {
	// 兼容旧版唯一索引：删除仅含 4 字段的旧索引，重建含 result 的 5 字段版本
	cursor, err := m.coll.Indexes().List(ctx)
	if err == nil {
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var idx bson.M
			if cursor.Decode(&idx) == nil {
				if name, _ := idx["name"].(string); name != "" && name != "_id_" {
					if keys, ok := idx["key"].(bson.M); ok {
						if _, hasResult := keys["result"]; !hasResult {
							if _, hasMain := keys["main_task_id"]; hasMain {
								if _, hasAuth := keys["authority"]; hasAuth {
									if _, hasURL := keys["url"]; hasURL {
										if _, hasVul := keys["vul_name"]; hasVul {
											_, _ = m.coll.Indexes().DropOne(ctx, name)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "host", Value: 1}}},
		{Keys: bson.D{{Key: "main_task_id", Value: 1}}},
		{Keys: bson.D{{Key: "severity", Value: 1}}},
		{Keys: bson.D{{Key: "url", Value: 1}}},
		// create_time 降序索引：JSFinder 列表按 create_time:-1 排序分页，避免 in-memory sort
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
		// AI研判状态索引，优化待研判数据查询
		{Keys: bson.D{{Key: "ai_status", Value: 1}}},
		// 唯一索引含 result 字段，允许同类型同来源的不同发现共存
		{
			Keys: bson.D{
				{Key: "main_task_id", Value: 1},
				{Key: "authority", Value: 1},
				{Key: "url", Value: 1},
				{Key: "vul_name", Value: 1},
				{Key: "result", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetBackground(true),
		},
	}
	_, err = m.coll.Indexes().CreateMany(ctx, indexes)
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

// FindByID 按 _id 取单条 JSFinder 结果（含 request/response/curl_command 等大字段），
// 供详情按需加载：列表查询已投影排除大字段，详情走本方法避免全量回传。
func (m *JSFinderResultModel) FindByID(ctx context.Context, id string) (*JSFinderResult, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc JSFinderResult
	if err := m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Count 计数
func (m *JSFinderResultModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// EstimatedCount 使用集合元数据快速估算文档总数（O(1)），仅适用于空 filter 场景
func (m *JSFinderResultModel) EstimatedCount(ctx context.Context) (int64, error) {
	return m.coll.EstimatedDocumentCount(ctx)
}

// DeleteMany 批量删除
func (m *JSFinderResultModel) DeleteMany(ctx context.Context, filter bson.M) (int64, error) {
	res, err := m.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// UpdateAIResult 回写单条AI研判结果
func (m *JSFinderResultModel) UpdateAIResult(ctx context.Context, id string, status, result, reason string, analyzedAt time.Time) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"ai_status":      status,
		"ai_result":      result,
		"ai_reason":      reason,
		"ai_analyzed_at": analyzedAt,
		"update_time":    analyzedAt,
	}})
	return err
}

// FindPendingForAnalysis 拉取待研判数据（ai_status != "completed"）
// limit <= 0 表示不限制
func (m *JSFinderResultModel) FindPendingForAnalysis(ctx context.Context, limit int64) ([]*JSFinderResult, error) {
	filter := bson.M{"ai_status": bson.M{"$ne": "completed"}}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	return m.Find(ctx, filter, opts)
}

// FindPendingByFilter 按自定义过滤条件拉取待研判数据（自动加上ai_status != completed）
func (m *JSFinderResultModel) FindPendingByFilter(ctx context.Context, filter bson.M, limit int64) ([]*JSFinderResult, error) {
	// 强制加上未研判条件
	filter["ai_status"] = bson.M{"$ne": "completed"}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	return m.Find(ctx, filter, opts)
}

// FindPendingByIds 按ID列表拉取待研判数据（自动过滤已完成的）
func (m *JSFinderResultModel) FindPendingByIds(ctx context.Context, ids []string) ([]*JSFinderResult, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return nil, nil
	}
	filter := bson.M{
		"_id":       bson.M{"$in": oids},
		"ai_status": bson.M{"$ne": "completed"},
	}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	return m.Find(ctx, filter, opts)
}

// CountPending 统计待研判数量
func (m *JSFinderResultModel) CountPending(ctx context.Context) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{"ai_status": bson.M{"$ne": "completed"}})
}

// CountPendingByFilter 按自定义过滤条件统计待研判数量
func (m *JSFinderResultModel) CountPendingByFilter(ctx context.Context, filter bson.M) (int64, error) {
	filter["ai_status"] = bson.M{"$ne": "completed"}
	return m.coll.CountDocuments(ctx, filter)
}
