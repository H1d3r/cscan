package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type IPV4 struct {
	IPName   string `bson:"ip" json:"ip"`
	IPInt    uint32 `bson:"uint32" json:"uint32"`
	Location string `bson:"location" json:"location"`
}

type IPV6 struct {
	IPName   string `bson:"ip" json:"ip"`
	Location string `bson:"location" json:"location"`
}

type IP struct {
	IpV4 []IPV4 `bson:"ipv4,omitempty" json:"ipv4,omitempty"`
	IpV6 []IPV6 `bson:"ipv6,omitempty" json:"ipv6,omitempty"`
}

type Asset struct {
	Id                   primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Authority            string             `bson:"authority" json:"authority"`
	Host                 string             `bson:"host" json:"host"`
	Port                 int                `bson:"port" json:"port"`
	Category             string             `bson:"category" json:"category"`
	Ip                   IP                 `bson:"ip" json:"ip"`
	Domain               string             `bson:"domain,omitempty" json:"domain"`
	Service              string             `bson:"service,omitempty" json:"service"`
	Server               string             `bson:"server,omitempty" json:"server"`
	Banner               string             `bson:"banner,omitempty" json:"banner"`
	Title                string             `bson:"title,omitempty" json:"title"`
	App                  []string           `bson:"app,omitempty" json:"app"`
	Fingerprints         []string           `bson:"fingerprints,omitempty" json:"fingerprints,omitempty"`
	HttpStatus           string             `bson:"status,omitempty" json:"httpStatus"`
	HttpHeader           string             `bson:"header,omitempty" json:"httpHeader"`
	HttpBody             string             `bson:"body,omitempty" json:"httpBody"`
	Cert                 string             `bson:"cert,omitempty" json:"cert"`
	IconHash             string             `bson:"icon_hash,omitempty" json:"iconHash"`
	IconHashFile         string             `bson:"icon_hash_file,omitempty" json:"iconHashFile"`
	IconHashBytes        []byte             `bson:"icon_hash_bytes,omitempty" json:"-"`
	Screenshot           string             `bson:"screenshot,omitempty" json:"screenshot"`
	Labels               []string           `bson:"labels,omitempty" json:"labels"` // 自定义标签
	OrgId                string             `bson:"org_id,omitempty" json:"orgId"`
	ColorTag             string             `bson:"color,omitempty" json:"colorTag"`
	Memo                 string             `bson:"memo,omitempty" json:"memo"`
	IsCDN                bool               `bson:"cdn,omitempty" json:"isCdn"`
	CName                string             `bson:"cname,omitempty" json:"cname"`
	IsCloud              bool               `bson:"cloud,omitempty" json:"isCloud"`
	IsHTTP               bool               `bson:"is_http" json:"isHttp"`
	IsNewAsset           bool               `bson:"new" json:"isNew"`
	IsUpdated            bool               `bson:"update" json:"isUpdated"`
	TaskId               string             `bson:"taskId" json:"taskId"`
	LastTaskId           string             `bson:"last_task_id,omitempty" json:"lastTaskId"`            // 上一个发现此资产的任务ID
	FirstSeenTaskId      string             `bson:"first_seen_task_id,omitempty" json:"firstSeenTaskId"` // 首次发现此资产的任务ID
	Source               string             `bson:"source,omitempty" json:"source"`
	CreateTime           time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime           time.Time          `bson:"update_time" json:"updateTime"`
	LastStatusChangeTime time.Time          `bson:"last_status_change_time,omitempty" json:"lastStatusChangeTime"` // 标签状态最后变化时间

	// 新增字段 - 风险评分
	RiskScore float64 `bson:"risk_score,omitempty" json:"riskScore,omitempty"` // 0-100
	RiskLevel string  `bson:"risk_level,omitempty" json:"riskLevel,omitempty"` // critical/high/medium/low/info/unknown
}

type AssetModel struct {
	coll *mongo.Collection
}

func NewAssetModel(db *mongo.Database, workspaceId string) *AssetModel {
	coll := db.Collection(workspaceId + "_asset")

	// 创建索引
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1}}},
		{Keys: bson.D{{Key: "authority", Value: 1}}},
		{Keys: bson.D{{Key: "update_time", Value: -1}}},
		{Keys: bson.D{{Key: "service", Value: 1}}},
		{Keys: bson.D{{Key: "app", Value: 1}}},
		// 新增索引 - 支持按风险评分排序
		{Keys: bson.D{{Key: "risk_score", Value: -1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes)

	return &AssetModel{
		coll: coll,
	}
}

func (m *AssetModel) Insert(ctx context.Context, doc *Asset) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	doc.IsNewAsset = true
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *AssetModel) FindById(ctx context.Context, id string) (*Asset, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc Asset
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (m *AssetModel) FindByAuthority(ctx context.Context, authority, taskId string) (*Asset, error) {
	var doc Asset
	filter := bson.M{"authority": authority, "taskId": taskId}
	err := m.coll.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// FindByAuthorityOnly 只按authority查找资产（不限制taskId）
func (m *AssetModel) FindByAuthorityOnly(ctx context.Context, authority string) (*Asset, error) {
	var doc Asset
	filter := bson.M{"authority": authority}
	err := m.coll.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (m *AssetModel) FindByHostPort(ctx context.Context, host string, port int) (*Asset, error) {
	var doc Asset
	filter := bson.M{"host": host, "port": port}
	err := m.coll.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (m *AssetModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	return m.FindWithSort(ctx, filter, page, pageSize, "update_time")
}

func (m *AssetModel) FindWithSort(ctx context.Context, filter bson.M, page, pageSize int, sortField string) ([]Asset, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: sortField, Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Asset
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindByRiskScore 按风险评分排序查询资产
func (m *AssetModel) FindByRiskScore(ctx context.Context, filter bson.M, page, pageSize int, ascending bool) ([]Asset, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	sortOrder := -1 // 默认降序（高风险在前）
	if ascending {
		sortOrder = 1
	}
	opts.SetSort(bson.D{{Key: "risk_score", Value: sortOrder}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Asset
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// UpdateRiskScore 更新资产风险评分
func (m *AssetModel) UpdateRiskScore(ctx context.Context, id string, riskScore float64, riskLevel string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"risk_score":  riskScore,
		"risk_level":  riskLevel,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// AggregateRiskLevel 统计各风险等级的资产数量
func (m *AssetModel) AggregateRiskLevel(ctx context.Context) (map[string]int, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$risk_level"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		Level string `bson:"_id"`
		Count int    `bson:"count"`
	}
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	stats := make(map[string]int)
	for _, r := range results {
		if r.Level != "" {
			stats[r.Level] = r.Count
		} else {
			// 未评分的资产归类为 "unknown"
			stats["unknown"] = r.Count
		}
	}
	return stats, nil
}

func (m *AssetModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// CountByTaskId 根据任务ID统计资产数量
func (m *AssetModel) CountByTaskId(ctx context.Context, taskId string) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{"taskId": taskId})
}

// CountNewByTaskId 根据任务ID统计新发现的资产数量
func (m *AssetModel) CountNewByTaskId(ctx context.Context, taskId string) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{"taskId": taskId, "new": true})
}

// FindByTaskId 根据任务ID查找资产列表
func (m *AssetModel) FindByTaskId(ctx context.Context, taskId string) ([]Asset, error) {
	return m.Find(ctx, bson.M{"taskId": taskId}, 0, 0)
}

func (m *AssetModel) Update(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// UpdateLabels 更新资产标签
func (m *AssetModel) UpdateLabels(ctx context.Context, id string, labels []string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"labels": labels,
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// AddLabel 添加单个标签
func (m *AssetModel) AddLabel(ctx context.Context, id string, label string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"$addToSet": bson.M{"labels": label}, // 使用 $addToSet 避免重复
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, update)
	return err
}

// RemoveLabel 删除单个标签
func (m *AssetModel) RemoveLabel(ctx context.Context, id string, label string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"$pull": bson.M{"labels": label},
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, update)
	return err
}

func (m *AssetModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

func (m *AssetModel) BatchDelete(ctx context.Context, ids []string) (int64, error) {
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
	result, err := m.coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": oids}})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// DeleteByFilter 根据条件删除资产
func (m *AssetModel) DeleteByFilter(ctx context.Context, filter bson.M) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// Clear 清空所有资产
func (m *AssetModel) Clear(ctx context.Context) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

func (m *AssetModel) Aggregate(ctx context.Context, field string, limit int) ([]StatResult, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$" + field},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []StatResult
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

type StatResult struct {
	Field string `bson:"_id"`
	Count int    `bson:"count"`
}

// AggregatePort 专门用于端口统计（端口是int类型）
func (m *AssetModel) AggregatePort(ctx context.Context, limit int) ([]PortStatResult, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$port"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []PortStatResult
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

type PortStatResult struct {
	Port  int `bson:"_id"`
	Count int `bson:"count"`
}

// AggregateApp 专门用于app字段统计（app是数组类型，需要先展开）
func (m *AssetModel) AggregateApp(ctx context.Context, limit int) ([]StatResult, error) {
	pipeline := mongo.Pipeline{
		// 先过滤掉app为空的资产
		{{Key: "$match", Value: bson.D{
			{Key: "app", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$ne", Value: nil}, {Key: "$ne", Value: bson.A{}}}},
		}}},
		// 展开app数组
		{{Key: "$unwind", Value: "$app"}},
		// 按app分组统计
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$app"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []StatResult
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// IconHashStatResult IconHash统计结果（包含图片数据）
type IconHashStatResult struct {
	IconHash string `bson:"_id"`
	IconData []byte `bson:"iconData"`
	Count    int    `bson:"count"`
}

// AggregateIconHash 统计 IconHash（包含图片数据）
func (m *AssetModel) AggregateIconHash(ctx context.Context, limit int) ([]IconHashStatResult, error) {
	pipeline := mongo.Pipeline{
		// 过滤有 icon_hash 的资产
		{{Key: "$match", Value: bson.D{
			{Key: "icon_hash", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$ne", Value: ""}}},
		}}},
		// 按 icon_hash 分组，取第一个图片数据
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$icon_hash"},
			{Key: "iconData", Value: bson.D{{Key: "$first", Value: "$icon_hash_bytes"}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []IconHashStatResult
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// AssetHistory 资产历史记录
type AssetHistory struct {
	Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	AssetId    string             `bson:"assetId" json:"assetId"`
	Authority  string             `bson:"authority" json:"authority"`
	Host       string             `bson:"host" json:"host"`
	Port       int                `bson:"port" json:"port"`
	Service    string             `bson:"service,omitempty" json:"service"`
	Title      string             `bson:"title,omitempty" json:"title"`
	App        []string           `bson:"app,omitempty" json:"app"`
	HttpStatus string             `bson:"status,omitempty" json:"httpStatus"`
	HttpHeader string             `bson:"header,omitempty" json:"httpHeader"`
	HttpBody   string             `bson:"body,omitempty" json:"httpBody"`
	Banner     string             `bson:"banner,omitempty" json:"banner"`
	IconHash   string             `bson:"icon_hash,omitempty" json:"iconHash"`
	Screenshot string             `bson:"screenshot,omitempty" json:"screenshot"`
	TaskId     string             `bson:"taskId" json:"taskId"`
	CreateTime time.Time          `bson:"create_time" json:"createTime"`
	// 变更详情
	Changes []FieldChange `bson:"changes,omitempty" json:"changes,omitempty"`
}

// FieldChange 字段变更记录
type FieldChange struct {
	Field    string `bson:"field" json:"field"`       // 变更的字段名
	OldValue string `bson:"oldValue" json:"oldValue"` // 旧值
	NewValue string `bson:"newValue" json:"newValue"` // 新值
}

// AssetHistoryModel 资产历史模型
type AssetHistoryModel struct {
	coll *mongo.Collection
}

func NewAssetHistoryModel(db *mongo.Database, workspaceId string) *AssetHistoryModel {
	return &AssetHistoryModel{
		coll: db.Collection(workspaceId + "_asset_history"),
	}
}

func (m *AssetHistoryModel) Insert(ctx context.Context, doc *AssetHistory) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	doc.CreateTime = time.Now()
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *AssetHistoryModel) FindByAssetId(ctx context.Context, assetId string, limit int) ([]AssetHistory, error) {
	opts := options.Find()
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := m.coll.Find(ctx, bson.M{"assetId": assetId}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []AssetHistory
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *AssetHistoryModel) FindByAuthority(ctx context.Context, authority string, limit int) ([]AssetHistory, error) {
	opts := options.Find()
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := m.coll.Find(ctx, bson.M{"authority": authority}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []AssetHistory
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// Clear 清空所有历史记录
func (m *AssetHistoryModel) Clear(ctx context.Context) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// ExistsByAssetIdAndTaskId 检查是否已存在同一资产同一任务的历史记录
func (m *AssetHistoryModel) ExistsByAssetIdAndTaskId(ctx context.Context, assetId, taskId string) (bool, error) {
	count, err := m.coll.CountDocuments(ctx, bson.M{"assetId": assetId, "taskId": taskId})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Upsert 插入或更新资产
func (m *AssetModel) Upsert(ctx context.Context, doc *Asset) error {
	// 仅按 authority 匹配，忽略 taskId，确保同一资产被合并
	filter := bson.M{"authority": doc.Authority}

	now := time.Now()
	setFields := bson.M{
		"host":                    doc.Host,
		"port":                    doc.Port,
		"service":                 doc.Service,
		"title":                   doc.Title,
		"app":                     doc.App,
		"source":                  doc.Source,
		"is_http":                 doc.IsHTTP,
		"update_time":             now,
		"update":                  true, // 标记为有更新
		"last_status_change_time": now,  // 更新状态变更时间，确保排在前面
	}

	// 如果有标签，使用 $addToSet 批量添加，避免覆盖原有标签
	// 注意：由于 setFields 是 $set 操作，如果直接放 labels 会覆盖。
	// 我们将 labels 拿出来单独处理，或者使用 Pipeline 更新（MongoDB 4.2+）。
	// 为了兼容性和简便，对于 Upsert，我们分两步：
	// 1. $setOnInsert 初始化 labels
	// 2. $addToSet 追加新 labels
	// 但 Upsert 一次操作无法同时 $set 和 $addToSet 同一个字段(如果 $set 包含它)。
	// 且 doc.Labels 是 []string。
	// 这里我们策略调整：
	// 如果是新导入，我们希望带上标签。
	// 如果是已存在，我们希望追加标签。
	// MongoDB 的 $addToSet with $each 可以做到追加。
	update := bson.M{
		"$set": setFields,
		"$setOnInsert": bson.M{
			"_id":             primitive.NewObjectID(),
			"create_time":     now,
			"new":             true,
			"first_seen_time": now,
		},
	}

	// 如果有标签需要更新
	if len(doc.Labels) > 0 {
		update["$addToSet"] = bson.M{
			"labels": bson.M{"$each": doc.Labels},
		}
	}

	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

// BulkUpsert 批量插入或更新资产
func (m *AssetModel) BulkUpsert(ctx context.Context, assets []*Asset) (*mongo.BulkWriteResult, error) {
	if len(assets) == 0 {
		return nil, nil
	}

	now := time.Now()
	var models []mongo.WriteModel
	for _, asset := range assets {
		filter := bson.M{"host": asset.Host, "port": asset.Port}
		update := bson.M{
			"$set": bson.M{
				"authority":   asset.Authority,
				"category":    asset.Category,
				"service":     asset.Service,
				"server":      asset.Server,
				"banner":      asset.Banner,
				"title":       asset.Title,
				"app":         asset.App,
				"status":      asset.HttpStatus,
				"header":      asset.HttpHeader,
				"body":        asset.HttpBody,
				"cert":        asset.Cert,
				"icon_hash":   asset.IconHash,
				"screenshot":  asset.Screenshot,
				"cdn":         asset.IsCDN,
				"cname":       asset.CName,
				"cloud":       asset.IsCloud,
				"is_http":     asset.IsHTTP,
				"taskId":      asset.TaskId,
				"source":      asset.Source,
				"update_time": now,
				"update":      true,
			},
			"$setOnInsert": bson.M{
				"_id":         primitive.NewObjectID(),
				"create_time": now,
				"new":         true,
			},
		}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true))
	}

	opts := options.BulkWrite().SetOrdered(false)
	return m.coll.BulkWrite(ctx, models, opts)
}
