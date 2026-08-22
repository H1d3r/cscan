package model

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 目录扫描/JSFinder 复验状态常量（与 vul.go 的 ReverifyStatus* 区分用途）
const (
	DirReverifyStatusResolved    = "resolved"    // 已复验通过
	DirReverifyStatusPending     = "pending"     // 待复验
	DirReverifyStatusReverifying = "reverifying" // 复验中
)

// DirScanResult 目录扫描结果
type DirScanResult struct {
	Id            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MainTaskId    string             `bson:"main_task_id" json:"mainTaskId"`
	Authority     string             `bson:"authority" json:"authority"`
	Host          string             `bson:"host" json:"host"`
	Port          int                `bson:"port" json:"port"`
	URL           string             `bson:"url" json:"url"`
	Path          string             `bson:"path" json:"path"`
	StatusCode    int                `bson:"status_code" json:"statusCode"`
	ContentLength int64              `bson:"content_length" json:"contentLength"`
	ContentType   string             `bson:"content_type" json:"contentType"`
	Title         string             `bson:"title" json:"title"`
	RedirectURL   string             `bson:"redirect_url" json:"redirectUrl"`
	ContentWords  int64              `bson:"content_words" json:"contentWords"`
	ContentLines  int64              `bson:"content_lines" json:"contentLines"`
	Duration      int64              `bson:"duration" json:"duration"`
	Request       string             `bson:"request,omitempty" json:"request,omitempty"`
	Response      string             `bson:"response,omitempty" json:"response,omitempty"`
	CreateTime    time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime    time.Time          `bson:"update_time" json:"updateTime"`
	ScanTime      time.Time          `bson:"scan_time,omitempty" json:"scanTime,omitempty"`
	Version       int64              `bson:"version,omitempty" json:"version,omitempty"`

	// 复验跟踪字段
	ReverifyStatus string    `bson:"reverify_status,omitempty" json:"reverifyStatus,omitempty"`
	LastVerifiedAt time.Time `bson:"last_verified_at,omitempty" json:"lastVerifiedAt,omitempty"`
	VerifyPending  bool      `bson:"verify_pending,omitempty" json:"verifyPending,omitempty"`

	// AI研判字段
	AIStatus     string    `bson:"ai_status,omitempty" json:"aiStatus,omitempty"`
	AIResult     string    `bson:"ai_result,omitempty" json:"aiResult,omitempty"`
	AIAnalyzedAt time.Time `bson:"ai_analyzed_at,omitempty" json:"aiAnalyzedAt,omitempty"`
	AIReason     string    `bson:"ai_reason,omitempty" json:"aiReason,omitempty"`
}

// DirScanResultModel 目录扫描结果模型
type DirScanResultModel struct {
	coll *mongo.Collection
}

// dirScanResultIndexes url 刻意用非唯一索引：UpsertMany 按 url 过滤的 upsert 已保证逻辑去重，
// 唯一索引会因存量重复数据创建失败导致索引永远缺失（写路径退化为全表扫描）。
var dirScanResultIndexes = []mongo.IndexModel{
	{Keys: bson.D{{Key: "main_task_id", Value: 1}}},
	{Keys: bson.D{{Key: "authority", Value: 1}}},
	{Keys: bson.D{{Key: "url", Value: 1}}},
	{Keys: bson.D{{Key: "create_time", Value: -1}}},
	{Keys: bson.D{{Key: "update_time", Value: -1}}},
	{Keys: bson.D{
		{Key: "authority", Value: 1},
		{Key: "host", Value: 1},
		{Key: "port", Value: 1},
		{Key: "scan_time", Value: -1},
	}},
	{Keys: bson.D{{Key: "scan_time", Value: -1}}},
	{Keys: bson.D{{Key: "version", Value: 1}}},
	{Keys: bson.D{{Key: "ai_status", Value: 1}}},
	{Keys: bson.D{{Key: "status_code", Value: 1}}},
}

// NewDirScanResultModel 全局集合模型
func NewDirScanResultModel(db *mongo.Database) *DirScanResultModel {
	coll := db.Collection("dirscan_result")
	if err := ensureIndexes(coll, dirScanResultIndexes); err != nil {
		logx.Errorf("[DirScanResultModel] ensureIndexes failed for %s: %v", coll.Name(), err)
	}
	return &DirScanResultModel{
		coll: coll,
	}
}

// Collection 返回底层集合
func (m *DirScanResultModel) Collection() *mongo.Collection {
	return m.coll
}

// EnsureIndexes 创建索引
func (m *DirScanResultModel) EnsureIndexes(ctx context.Context) error {
	_, err := m.coll.Indexes().CreateMany(ctx, dirScanResultIndexes)
	return err
}

// Insert 插入单条记录
func (m *DirScanResultModel) Insert(ctx context.Context, doc *DirScanResult) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}
	if doc.UpdateTime.IsZero() {
		doc.UpdateTime = now
	}
	if doc.ScanTime.IsZero() {
		doc.ScanTime = now
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

// InsertMany 批量插入
func (m *DirScanResultModel) InsertMany(ctx context.Context, docs []*DirScanResult) error {
	if len(docs) == 0 {
		return nil
	}
	now := time.Now()
	var documents []interface{}
	for _, doc := range docs {
		if doc.Id.IsZero() {
			doc.Id = primitive.NewObjectID()
		}
		if doc.CreateTime.IsZero() {
			doc.CreateTime = now
		}
		if doc.UpdateTime.IsZero() {
			doc.UpdateTime = now
		}
		if doc.ScanTime.IsZero() {
			doc.ScanTime = now
		}
		if doc.Version == 0 {
			doc.Version = 1
		}
		documents = append(documents, doc)
	}
	opts := options.InsertMany().SetOrdered(false)
	_, err := m.coll.InsertMany(ctx, documents, opts)
	return err
}

// Upsert 插入或更新（基于URL去重）。
// 注意：request/response 在重复扫描时更新，但 AI 研判字段（ai_*）不覆盖，保护人工/AI 标注结果。
func (m *DirScanResultModel) Upsert(ctx context.Context, doc *DirScanResult) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}
	if doc.ScanTime.IsZero() {
		doc.ScanTime = now
	}
	if doc.Version == 0 {
		doc.Version = 1
	}

	filter := bson.M{"url": doc.URL}
	update := bson.M{
		"$set": bson.M{
			"main_task_id":   doc.MainTaskId,
			"authority":      doc.Authority,
			"host":           doc.Host,
			"port":           doc.Port,
			"path":           doc.Path,
			"status_code":    doc.StatusCode,
			"content_length": doc.ContentLength,
			"content_type":   doc.ContentType,
			"title":          doc.Title,
			"redirect_url":   doc.RedirectURL,
			"content_words":  doc.ContentWords,
			"content_lines":  doc.ContentLines,
			"duration":       doc.Duration,
			"scan_time":      doc.ScanTime,
			"version":        doc.Version,
			"update_time":    now,
			// request/response 大字段随扫描结果更新
			"request":  doc.Request,
			"response": doc.Response,
		},
		"$setOnInsert": bson.M{
			"_id":         doc.Id,
			"create_time": doc.CreateTime,
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

// UpsertMany 批量 upsert：基于URL去重，重复扫描刷新update_time，AI研判字段不被覆盖。
func (m *DirScanResultModel) UpsertMany(ctx context.Context, docs []*DirScanResult) error {
	if len(docs) == 0 {
		return nil
	}
	now := time.Now()
	const batchSize = 200
	for start := 0; start < len(docs); start += batchSize {
		end := start + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[start:end]

		var models []mongo.WriteModel
		for _, doc := range batch {
			if doc.CreateTime.IsZero() {
				doc.CreateTime = now
			}
			if doc.ScanTime.IsZero() {
				doc.ScanTime = now
			}
			if doc.Version == 0 {
				doc.Version = 1
			}

			filter := bson.M{"url": doc.URL}
			setOnInsert := bson.M{
				"_id":          primitive.NewObjectID(),
				"main_task_id": doc.MainTaskId,
				"create_time":  doc.CreateTime,
				"authority":    doc.Authority,
				"host":         doc.Host,
				"port":         doc.Port,
				"path":         doc.Path,
			}
			set := bson.M{
				"update_time":    now,
				"scan_time":      doc.ScanTime,
				"version":        doc.Version,
				"status_code":    doc.StatusCode,
				"content_length": doc.ContentLength,
				"content_type":   doc.ContentType,
				"title":          doc.Title,
				"redirect_url":   doc.RedirectURL,
				"content_words":  doc.ContentWords,
				"content_lines":  doc.ContentLines,
				"duration":       doc.Duration,
				"request":        doc.Request,
				"response":       doc.Response,
			}
			update := bson.M{"$setOnInsert": setOnInsert, "$set": set}
			model := mongo.NewUpdateOneModel().
				SetFilter(filter).
				SetUpdate(update).
				SetUpsert(true)
			models = append(models, model)
		}
		opts := options.BulkWrite().SetOrdered(false)
		if _, err := m.coll.BulkWrite(ctx, models, opts); err != nil {
			return err
		}
	}
	return nil
}

// FindByFilter 根据条件查询
func (m *DirScanResultModel) FindByFilter(ctx context.Context, filter bson.M, page, pageSize int) ([]DirScanResult, error) {
	page, pageSize = NormalizePage(page, pageSize)
	return m.FindByFilterWithSort(ctx, filter, page, pageSize, "", "")
}

// FindByFilterWithSort 根据条件查询并支持排序
func (m *DirScanResultModel) FindByFilterWithSort(ctx context.Context, filter bson.M, page, pageSize int, sortField string, sortOrder string) ([]DirScanResult, error) {
	page, pageSize = NormalizePage(page, pageSize)
	return m.FindByFilterWithSortAndProjection(ctx, filter, page, pageSize, sortField, sortOrder, nil)
}

// FindByFilterWithSortAndProjection 支持投影的查询（列表页排除大字段时使用）
func (m *DirScanResultModel) FindByFilterWithSortAndProjection(ctx context.Context, filter bson.M, page, pageSize int, sortField string, sortOrder string, projection bson.M) ([]DirScanResult, error) {
	page, pageSize = NormalizePage(page, pageSize)
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	if projection != nil {
		opts.SetProjection(projection)
	}

	sortValue := -1
	if sortOrder == "asc" {
		sortValue = 1
	}
	switch sortField {
	case "statusCode":
		opts.SetSort(bson.D{{Key: "status_code", Value: sortValue}, {Key: "create_time", Value: -1}})
	case "contentLength":
		opts.SetSort(bson.D{{Key: "content_length", Value: sortValue}, {Key: "create_time", Value: -1}})
	case "contentWords":
		opts.SetSort(bson.D{{Key: "content_words", Value: sortValue}, {Key: "create_time", Value: -1}})
	case "contentLines":
		opts.SetSort(bson.D{{Key: "content_lines", Value: sortValue}, {Key: "create_time", Value: -1}})
	case "duration":
		opts.SetSort(bson.D{{Key: "duration", Value: sortValue}, {Key: "create_time", Value: -1}})
	default:
		opts.SetSort(bson.D{{Key: "create_time", Value: -1}})
	}

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []DirScanResult
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindByID 按 _id 取单条结果（含 request/response 大字段，供详情按需加载）
func (m *DirScanResultModel) FindByID(ctx context.Context, id string) (*DirScanResult, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc DirScanResult
	if err := m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// CountByFilter 根据条件统计
func (m *DirScanResultModel) CountByFilter(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// EstimatedCount 快速估算集合总文档数（O(1)）
func (m *DirScanResultModel) EstimatedCount(ctx context.Context) (int64, error) {
	return m.coll.EstimatedDocumentCount(ctx)
}

// FindByTaskId 根据任务ID查询
func (m *DirScanResultModel) FindByTaskId(ctx context.Context, taskId string) ([]DirScanResult, error) {
	return m.FindByFilter(ctx, bson.M{"main_task_id": taskId}, 0, 0)
}

// Delete 删除单条记录
func (m *DirScanResultModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// DeleteByIds 批量删除
func (m *DirScanResultModel) DeleteByIds(ctx context.Context, ids []string) (int64, error) {
	var oids []primitive.ObjectID
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
	result, err := m.coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": oids}})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// DeleteByFilter 根据条件删除
func (m *DirScanResultModel) DeleteByFilter(ctx context.Context, filter bson.M) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// DeleteMany 批量删除（兼容JSFinder风格）
func (m *DirScanResultModel) DeleteMany(ctx context.Context, filter bson.M) (int64, error) {
	res, err := m.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// Stat 统计信息
func (m *DirScanResultModel) Stat(ctx context.Context) (map[string]int64, error) {
	filter := bson.M{}

	total, err := m.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	pipeline := []bson.M{
		{"$match": filter},
		{"$group": bson.M{
			"_id":   "$status_code",
			"count": bson.M{"$sum": 1},
		}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	stat := map[string]int64{"total": total}
	var results []struct {
		Id    int   `bson:"_id"`
		Count int64 `bson:"count"`
	}
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	for _, r := range results {
		switch {
		case r.Id >= 200 && r.Id < 300:
			stat["status_2xx"] += r.Count
		case r.Id >= 300 && r.Id < 400:
			stat["status_3xx"] += r.Count
		case r.Id >= 400 && r.Id < 500:
			stat["status_4xx"] += r.Count
		case r.Id >= 500:
			stat["status_5xx"] += r.Count
		}
	}
	return stat, nil
}

// FindFoundForReverify 取待复验的已发现目录/文件（状态码 2xx/3xx，视为暴露）。
// 修复 M-11：排除已 resolved 的记录，并按 last_reverified_time 升序排序以轮转目标，避免饥饿。
func (m *DirScanResultModel) FindFoundForReverify(ctx context.Context, limit int) ([]DirScanResult, error) {
	filter := bson.M{
		"status_code":  bson.M{"$gte": 200, "$lt": 400},
		// 排除已复验为 resolved 的记录（原实现不过滤，已修复的目标会被反复选取）
		"reverify_status": bson.M{"$ne": DirReverifyStatusResolved},
	}
	opts := options.Find()
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	// 按上次复验时间升序轮转：从未复验过的优先，最早复验的优先
	opts.SetSort(bson.D{
		{Key: "last_verified_at", Value: 1},
		{Key: "create_time", Value: 1},
	})
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []DirScanResult
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// MarkReverify 批量回写复验结果。
func (m *DirScanResultModel) MarkReverify(ctx context.Context, ids []string, status string, verifiedAt time.Time, pending bool) error {
	oids := toObjectIDs(ids)
	if len(oids) == 0 {
		return nil
	}
	_, err := m.coll.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": oids}}, bson.M{"$set": bson.M{
		"reverify_status":  status,
		"last_verified_at": verifiedAt,
		"verify_pending":   pending,
	}})
	return err
}

// UpdateAIResult 回写单条AI研判结果
func (m *DirScanResultModel) UpdateAIResult(ctx context.Context, id string, status, result, reason string, analyzedAt time.Time) error {
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
func (m *DirScanResultModel) FindPendingForAnalysis(ctx context.Context, limit int64) ([]*DirScanResult, error) {
	filter := bson.M{"ai_status": bson.M{"$ne": "completed"}}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []*DirScanResult
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindPendingByFilter 按自定义过滤条件拉取待研判数据（自动加上 ai_status != completed）
func (m *DirScanResultModel) FindPendingByFilter(ctx context.Context, filter bson.M, limit int64) ([]*DirScanResult, error) {
	filter["ai_status"] = bson.M{"$ne": "completed"}
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []*DirScanResult
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindPendingByIds 按ID列表拉取待研判数据（自动过滤已完成的）
func (m *DirScanResultModel) FindPendingByIds(ctx context.Context, ids []string) ([]*DirScanResult, error) {
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
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []*DirScanResult
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}
