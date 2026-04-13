package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DirScanResult 目录扫描结果
type DirScanResult struct {
	Id            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	WorkspaceId   string             `bson:"workspace_id" json:"workspaceId"`
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
	CreateTime    time.Time          `bson:"create_time" json:"createTime"`
	ScanTime      time.Time          `bson:"scan_time,omitempty" json:"scanTime,omitempty"` // New field for versioning
	Version       int64              `bson:"version,omitempty" json:"version,omitempty"`    // New field for versioning
}

// DirScanResultModel 目录扫描结果模型
type DirScanResultModel struct {
	coll *mongo.Collection
}

func NewDirScanResultModel(db *mongo.Database) *DirScanResultModel {
	return &DirScanResultModel{
		coll: db.Collection("dirscan_result"),
	}
}

// EnsureIndexes 创建索引
func (m *DirScanResultModel) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "workspace_id", Value: 1}}},
		{Keys: bson.D{{Key: "main_task_id", Value: 1}}},
		{Keys: bson.D{{Key: "authority", Value: 1}}},
		{Keys: bson.D{{Key: "url", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
		// New composite index for efficient scan result queries
		{Keys: bson.D{
			{Key: "workspace_id", Value: 1},
			{Key: "authority", Value: 1},
			{Key: "host", Value: 1},
			{Key: "port", Value: 1},
			{Key: "scan_time", Value: -1},
		}},
		// Index for scan_time to support versioning queries
		{Keys: bson.D{{Key: "scan_time", Value: -1}}},
		// Index for version to support versioning queries
		{Keys: bson.D{{Key: "version", Value: 1}}},
	}
	_, err := m.coll.Indexes().CreateMany(ctx, indexes)
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
	// Set scan_time if not already set
	if doc.ScanTime.IsZero() {
		doc.ScanTime = now
	}
	// Set version to 1 if not already set (for new records)
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
		// Set scan_time if not already set
		if doc.ScanTime.IsZero() {
			doc.ScanTime = now
		}
		// Set version to 1 if not already set (for new records)
		if doc.Version == 0 {
			doc.Version = 1
		}
		documents = append(documents, doc)
	}
	_, err := m.coll.InsertMany(ctx, documents)
	return err
}

// Upsert 插入或更新（基于URL去重）
func (m *DirScanResultModel) Upsert(ctx context.Context, doc *DirScanResult) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = now
	}
	// Set scan_time if not already set
	if doc.ScanTime.IsZero() {
		doc.ScanTime = now
	}
	// Set version to 1 if not already set (for new records)
	if doc.Version == 0 {
		doc.Version = 1
	}

	filter := bson.M{"url": doc.URL}
	update := bson.M{
		"$set": bson.M{
			"workspace_id":   doc.WorkspaceId,
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

// FindByFilter 根据条件查询
func (m *DirScanResultModel) FindByFilter(ctx context.Context, filter bson.M, page, pageSize int) ([]DirScanResult, error) {
	return m.FindByFilterWithSort(ctx, filter, page, pageSize, "", "")
}

// FindByFilterWithSort 根据条件查询并支持排序
func (m *DirScanResultModel) FindByFilterWithSort(ctx context.Context, filter bson.M, page, pageSize int, sortField string, sortOrder string) ([]DirScanResult, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}

	// 处理排序
	sortValue := -1 // 默认降序
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

// CountByFilter 根据条件统计
func (m *DirScanResultModel) CountByFilter(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// FindByWorkspace 根据工作空间查询
func (m *DirScanResultModel) FindByWorkspace(ctx context.Context, workspaceId string, page, pageSize int) ([]DirScanResult, error) {
	filter := bson.M{}
	if workspaceId != "" && workspaceId != "all" {
		filter["workspace_id"] = workspaceId
	}
	return m.FindByFilter(ctx, filter, page, pageSize)
}

// CountByWorkspace 根据工作空间统计
func (m *DirScanResultModel) CountByWorkspace(ctx context.Context, workspaceId string) (int64, error) {
	filter := bson.M{}
	if workspaceId != "" && workspaceId != "all" {
		filter["workspace_id"] = workspaceId
	}
	return m.CountByFilter(ctx, filter)
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

// DeleteByWorkspace 根据工作空间删除
func (m *DirScanResultModel) DeleteByWorkspace(ctx context.Context, workspaceId string) (int64, error) {
	filter := bson.M{}
	if workspaceId != "" && workspaceId != "all" {
		filter["workspace_id"] = workspaceId
	}
	result, err := m.coll.DeleteMany(ctx, filter)
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

// Stat 统计信息
func (m *DirScanResultModel) Stat(ctx context.Context, workspaceId string) (map[string]int64, error) {
	filter := bson.M{}
	if workspaceId != "" && workspaceId != "all" {
		filter["workspace_id"] = workspaceId
	}

	total, err := m.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	// 按状态码分组统计
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

	stat := map[string]int64{
		"total": total,
	}

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
