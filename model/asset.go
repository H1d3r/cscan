package model

import (
	"context"
	"strings"
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
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1}}},
		{Keys: bson.D{{Key: "authority", Value: 1}}},
		{Keys: bson.D{{Key: "update_time", Value: -1}}},
		{Keys: bson.D{{Key: "service", Value: 1}}},
		{Keys: bson.D{{Key: "app", Value: 1}}},
		// 新增索引 - 支持按风险评分排序
		{Keys: bson.D{{Key: "risk_score", Value: -1}}},
	}
	ensureIndexes(coll, indexes)

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
	opts.SetProjection(AssetListProjection)
	opts.SetSort(bson.D{{Key: sortField, Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Asset
	for cursor.Next(ctx) {
		var doc Asset
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindFull 全字段查询（不裁剪 body/header 等大字段），用于指纹匹配等需要完整数据的场景
func (m *AssetModel) FindFull(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "update_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Asset
	for cursor.Next(ctx) {
		var doc Asset
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, cursor.Err()
}

// FindWithScreenshot 截图查询专用，包含 screenshot 字段
func (m *AssetModel) FindWithScreenshot(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetProjection(AssetScreenshotProjection)
	opts.SetSort(bson.D{{Key: "update_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Asset
	for cursor.Next(ctx) {
		var doc Asset
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, cursor.Err()
}

// FindWithOffset 支持自定义 skip 和 limit（用于多工作空间聚合分页）
// sortField 以 "-" 前缀表示降序（如 "-update_time"），无前缀表示升序（如 "host"）
func (m *AssetModel) FindWithOffset(ctx context.Context, filter bson.M, skip, limit int64, sortField string) ([]Asset, error) {
	opts := options.Find()
	if skip > 0 {
		opts.SetSkip(skip)
	}
	if limit > 0 {
		opts.SetLimit(limit)
	}
	// 排除 body/header/cert 等超大字段，但保留 screenshot 供卡片视图展示
	opts.SetProjection(AssetScreenshotProjection)
	sortOrder := 1 // 升序
	if strings.HasPrefix(sortField, "-") {
		sortField = sortField[1:]
		sortOrder = -1 // 降序
	}
	opts.SetSort(bson.D{{Key: sortField, Value: sortOrder}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Asset
	for cursor.Next(ctx) {
		var doc Asset
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := cursor.Err(); err != nil {
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
	opts.SetProjection(AssetListProjection)
	opts.SetSort(bson.D{{Key: "risk_score", Value: sortOrder}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Asset
	for cursor.Next(ctx) {
		var doc Asset
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := cursor.Err(); err != nil {
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
	for cursor.Next(ctx) {
		var res struct {
			Level string `bson:"_id"`
			Count int    `bson:"count"`
		}
		if err := cursor.Decode(&res); err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	if err := cursor.Err(); err != nil {
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

// UpdateWithRaw 使用原始 MongoDB 更新文档更新资产（支持 $addToSet 等操作符）
func (m *AssetModel) UpdateWithRaw(ctx context.Context, id string, rawUpdate bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	// 确保 $set 中包含 update_time
	if setFields, ok := rawUpdate["$set"].(bson.M); ok {
		setFields["update_time"] = time.Now()
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, rawUpdate)
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
		{{Key: "$match", Value: bson.D{
			{Key: field, Value: bson.D{
				{Key: "$exists", Value: true},
				{Key: "$ne", Value: ""},
			}},
		}}},
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
	for cursor.Next(ctx) {
		var res StatResult
		if err := cursor.Decode(&res); err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	if err := cursor.Err(); err != nil {
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
		{{Key: "$match", Value: bson.D{
			{Key: "port", Value: bson.D{{Key: "$gt", Value: 0}}},
		}}},
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
	for cursor.Next(ctx) {
		var res PortStatResult
		if err := cursor.Decode(&res); err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

type PortStatResult struct {
	Port  int `bson:"_id"`
	Count int `bson:"count"`
}

type AssetOverviewStats struct {
	TotalAsset   int64 `bson:"totalAsset"`
	NewCount     int64 `bson:"newCount"`
	UpdatedCount int64 `bson:"updatedCount"`
}

// AggregateOverviewStats 聚合统计资产总数、新资产数、更新资产数
func (m *AssetModel) AggregateOverviewStats(ctx context.Context) (*AssetOverviewStats, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "totalAsset", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "newCount", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{"$new", 1, 0}}}}}},
			{Key: "updatedCount", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{"$update", 1, 0}}}}}},
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	stats := &AssetOverviewStats{}
	if cursor.Next(ctx) {
		if err := cursor.Decode(stats); err != nil {
			return nil, err
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}

	return stats, nil
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
	for cursor.Next(ctx) {
		var res StatResult
		if err := cursor.Decode(&res); err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	if err := cursor.Err(); err != nil {
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
		// 标记是否包含二进制图标数据，便于分组时优先保留有图标的记录
		{{Key: "$addFields", Value: bson.D{
			{Key: "has_icon_data", Value: bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$icon_hash_bytes"}}, "binData"}}}},
		}}},
		// 同一 icon_hash 下优先让有 icon_hash_bytes 的文档排在前面
		{{Key: "$sort", Value: bson.D{{Key: "icon_hash", Value: 1}, {Key: "has_icon_data", Value: -1}}}},
		// 按 icon_hash 分组，统计数量并直接保留一份图标数据，避免 N+1 查询
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$icon_hash"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "iconData", Value: bson.D{{Key: "$first", Value: "$icon_hash_bytes"}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stats []IconHashStatResult
	for cursor.Next(ctx) {
		var stat IconHashStatResult
		if err := cursor.Decode(&stat); err != nil {
			return nil, err
		}
		stats = append(stats, stat)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}

	return stats, nil
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
	for cursor.Next(ctx) {
		var doc AssetHistory
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := cursor.Err(); err != nil {
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
	for cursor.Next(ctx) {
		var doc AssetHistory
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := cursor.Err(); err != nil {
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
		"source":                  doc.Source,
		"is_http":                 doc.IsHTTP,
		"update_time":             now,
		"update":                  true, // 标记为有更新
		"last_status_change_time": now,  // 更新状态变更时间，确保排在前面
	}

	// 有值才更新的字段，避免用空值覆盖已有数据
	if doc.Category != "" {
		setFields["category"] = doc.Category
	}
	if doc.Server != "" {
		setFields["server"] = doc.Server
	}
	if doc.Banner != "" {
		setFields["banner"] = doc.Banner
	}
	if doc.Title != "" {
		setFields["title"] = doc.Title
	}
	if len(doc.App) > 0 {
		setFields["app"] = doc.App
	}
	if doc.HttpStatus != "" {
		setFields["status"] = doc.HttpStatus
	}
	if doc.HttpHeader != "" {
		setFields["header"] = doc.HttpHeader
	}
	if doc.HttpBody != "" {
		setFields["body"] = doc.HttpBody
	}
	if doc.Screenshot != "" {
		setFields["screenshot"] = doc.Screenshot
	}
	if doc.IconHash != "" {
		setFields["icon_hash"] = doc.IconHash
	}
	if len(doc.IconHashBytes) > 0 {
		setFields["icon_hash_bytes"] = doc.IconHashBytes
	}
	if doc.CName != "" {
		setFields["cname"] = doc.CName
	}
	if doc.Domain != "" {
		setFields["domain"] = doc.Domain
	}
	if doc.Cert != "" {
		setFields["cert"] = doc.Cert
	}
	if doc.Ip.IpV4 != nil || doc.Ip.IpV6 != nil {
		setFields["ip"] = doc.Ip
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
		setFields := bson.M{
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
		}
		if len(asset.IconHashBytes) > 0 {
			setFields["icon_hash_bytes"] = asset.IconHashBytes
		}
		update := bson.M{
			"$set": setFields,
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

// PortListAggregateResult 端口列表聚合结果
type PortListAggregateResult struct {
	Port       int       `bson:"_id"`
	AssetCount int       `bson:"assetCount"`
	Hosts      []string  `bson:"hosts"`
	Services   []string  `bson:"services"`
	UpdateTime time.Time `bson:"updateTime"`
}

// AggregatePortList 聚合端口列表，支持分页和总数统计
func (m *AssetModel) AggregatePortList(ctx context.Context, filter bson.M, skip, limit int) ([]PortListAggregateResult, int64, error) {
	// 确保端口大于0
	matchObj := bson.M{}
	for k, v := range filter {
		matchObj[k] = v
	}
	// 如果过滤条件中没有对port做特殊处理，强制port>0
	if _, ok := matchObj["port"]; !ok {
		matchObj["port"] = bson.M{"$gt": 0}
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchObj}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$port"},
			{Key: "assetCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "hosts", Value: bson.D{{Key: "$addToSet", Value: "$host"}}},
			{Key: "services", Value: bson.D{{Key: "$addToSet", Value: "$service"}}},
			{Key: "updateTime", Value: bson.D{{Key: "$max", Value: "$update_time"}}},
		}}},
	}

	// 使用 facet 进行总量统计和分页
	facet := bson.D{
		{Key: "metadata", Value: bson.A{
			bson.D{{Key: "$count", Value: "total"}},
		}},
		{Key: "data", Value: bson.A{
			bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
			bson.D{{Key: "$skip", Value: skip}},
			bson.D{{Key: "$limit", Value: limit}},
		}},
	}

	pipeline = append(pipeline, bson.D{{Key: "$facet", Value: facet}})

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var result []struct {
		Metadata []struct {
			Total int64 `bson:"total"`
		} `bson:"metadata"`
		Data []PortListAggregateResult `bson:"data"`
	}

	if err := cursor.All(ctx, &result); err != nil {
		return nil, 0, err
	}

	if len(result) == 0 {
		return []PortListAggregateResult{}, 0, nil
	}

	total := int64(0)
	if len(result[0].Metadata) > 0 {
		total = result[0].Metadata[0].Total
	}

	return result[0].Data, total, nil
}
