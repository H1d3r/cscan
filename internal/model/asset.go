package model

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
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
	FirstSeenTime        time.Time          `bson:"first_seen_time,omitempty" json:"firstSeenTime"` // 首次发现时间，用于"较昨日"统计
	LastStatusChangeTime time.Time          `bson:"last_status_change_time,omitempty" json:"lastStatusChangeTime"` // 标签状态最后变化时间

	// 新增字段 - 风险评分
	RiskScore float64 `bson:"risk_score,omitempty" json:"riskScore,omitempty"` // 0-100
	RiskLevel string  `bson:"risk_level,omitempty" json:"riskLevel,omitempty"` // critical/high/medium/low/info/unknown
}

type AssetModel struct {
	coll *mongo.Collection
}

func NewAssetModel(db *mongo.Database) *AssetModel {
	coll := db.Collection("asset")

	// 创建索引
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1}}},
		{Keys: bson.D{{Key: "authority", Value: 1}}},
		{Keys: bson.D{{Key: "update_time", Value: -1}}},
		{Keys: bson.D{{Key: "service", Value: 1}}},
		{Keys: bson.D{{Key: "app", Value: 1}}},
		// 新增索引 - 支持按风险评分排序
		{Keys: bson.D{{Key: "risk_score", Value: -1}}},
		// 新增索引 - 支持"较昨日"时间窗口统计
		{Keys: bson.D{{Key: "first_seen_time", Value: -1}}},
		// 新增索引 - 支持按状态码/端口过滤的精确匹配（避免全表扫描）
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "port", Value: 1}}},
	}
	if err := ensureIndexes(coll, indexes); err != nil {
		logx.Errorf("[AssetModel] create indexes failed for %s: %v", coll.Name(), err)
	}

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
	if doc.FirstSeenTime.IsZero() {
		doc.FirstSeenTime = now
	}
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
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// FindByIds 按 ID 列表批量查询（替代 N 次 FindById 的 N+1 查询）
// 仅返回 AssetListProjection 字段（排除 body/header/cert/banner/screenshot/icon_hash_bytes）
func (m *AssetModel) FindByIds(ctx context.Context, ids []string) ([]Asset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	oids := make([]primitive.ObjectID, 0, len(ids))
	var dropped int
	for _, id := range ids {
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			oids = append(oids, oid)
		} else {
			dropped++
		}
	}
	if dropped > 0 {
		logx.Errorf("[AssetModel] FindByIds: dropped %d invalid ObjectID(s)", dropped)
	}
	if len(oids) == 0 {
		return nil, nil
	}
	opts := options.Find().SetProjection(AssetListProjection)
	cursor, err := m.coll.Find(ctx, bson.M{"_id": bson.M{"$in": oids}}, opts)
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

func (m *AssetModel) FindByAuthority(ctx context.Context, authority, taskId string) (*Asset, error) {
	var doc Asset
	filter := bson.M{"authority": authority, "taskId": taskId}
	err := m.coll.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// FindByAuthorityOnly 按authority查找资产
func (m *AssetModel) FindByAuthorityOnly(ctx context.Context, authority string) (*Asset, error) {
	var doc Asset
	filter := bson.M{"authority": authority}
	err := m.coll.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *AssetModel) FindByHostPort(ctx context.Context, host string, port int) (*Asset, error) {
	var doc Asset
	filter := bson.M{"host": host, "port": port}
	err := m.coll.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// InsertIfAbsent 以 (host,port)/authority 为键原子插入：键已存在时不写入并返回 false。
// 用 $setOnInsert upsert 取代「先查后插」，消除并发写入
// （异步批量落库通道满回退同步直写时与后台 flush 并发）产生重复文档的竞态，
// 重复文档会让资产数量虚高且各处统计口径永久对不上。
func (m *AssetModel) InsertIfAbsent(ctx context.Context, asset *Asset) (bool, error) {
	var filter bson.M
	if asset.Port > 0 {
		filter = bson.M{"host": asset.Host, "port": asset.Port}
	} else {
		filter = bson.M{"authority": asset.Authority}
	}
	res, err := m.coll.UpdateOne(ctx, filter,
		bson.M{"$setOnInsert": asset},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return false, err
	}
	return res.UpsertedCount > 0, nil
}

func (m *AssetModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	page, pageSize = NormalizePage(page, pageSize)
	return m.FindWithSort(ctx, filter, page, pageSize, "update_time")
}

func (m *AssetModel) FindWithSort(ctx context.Context, filter bson.M, page, pageSize int, sortField string) ([]Asset, error) {
	page, pageSize = NormalizePage(page, pageSize)
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

// FindSubdomainHostsByRootDomain 查询根域名下所有已存在的子域名 host 列表（去重）
// 用于定时任务"同步拉取所有子域名"功能，把数据库中已有的子域名资产加入扫描目标
func (m *AssetModel) FindSubdomainHostsByRootDomain(ctx context.Context, rootDomains []string) ([]string, error) {
	if len(rootDomains) == 0 {
		return nil, nil
	}

	// 构造正则：匹配 host == root 或 host 以 .root 结尾
	var regexPatterns []bson.M
	for _, root := range rootDomains {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		regexPatterns = append(regexPatterns, bson.M{
			"host": bson.M{"$regex": "(^|\\.)" + regexp.QuoteMeta(root) + "$", "$options": "i"},
		})
	}

	if len(regexPatterns) == 0 {
		return nil, nil
	}

	filter := bson.M{"$or": regexPatterns}
	opts := options.Find().SetProjection(bson.M{"host": 1, "_id": 0})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	hostSet := make(map[string]struct{})
	var hosts []string
	for cursor.Next(ctx) {
		var result struct {
			Host string `bson:"host"`
		}
		if err := cursor.Decode(&result); err != nil {
			continue
		}
		h := strings.TrimSpace(result.Host)
		if h != "" {
			if _, exists := hostSet[h]; !exists {
				hostSet[h] = struct{}{}
				hosts = append(hosts, h)
			}
		}
	}
	return hosts, cursor.Err()
}

// AssetGroupAggRow 是 AggregateGroupByDomain 返回的轻量行，仅包含分组所需字段。
type AssetGroupAggRow struct {
	Host       string    `bson:"host" json:"host"`
	Domain     string    `bson:"domain" json:"domain"`
	CreateTime time.Time `bson:"create_time" json:"createTime"`
	UpdateTime time.Time `bson:"update_time" json:"updateTime"`
}

// AggregateGroupByDomain 通过 aggregation pipeline 仅投影分组所需的最小字段集，
// 避免加载完整 asset 文档（排除 body/header/screenshot/cert/banner 等大字段）。
// 返回值用于在 Go 侧按根域名再次聚合（publicsuffix 无法在 MongoDB 表达式中执行）。
func (m *AssetModel) AggregateGroupByDomain(ctx context.Context) ([]AssetGroupAggRow, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$project", Value: bson.M{
			"host":        1,
			"domain":      1,
			"create_time": 1,
			"update_time": 1,
			"_id":         0,
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rows []AssetGroupAggRow
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// findPagedWithProjection 按 update_time 降序的分页查询公共实现，projection 决定返回字段集合
func (m *AssetModel) findPagedWithProjection(ctx context.Context, filter bson.M, page, pageSize int, projection bson.M) ([]Asset, error) {
	page, pageSize = NormalizePage(page, pageSize)
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetProjection(projection)
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

// AssetTargetGroupResult 目标维度聚合行（host/port/ip/app/status 分组通用）。
type AssetTargetGroupResult struct {
	Id       interface{} `bson:"_id"`
	Count    int         `bson:"count"`
	Location string      `bson:"location"`
	Extras   []string    `bson:"extras"`
	Labels   []string    `bson:"labels"`
	// Name 仅 app 分组使用：归一化键作为 _id 分组时的组内首个原始条目，
	// 保留大小写/版本作为前端展示名（前端 getTechName 再做归一化展示）。
	Name string `bson:"name"`
}

// AggregateTargetGroups 执行目标维度聚合 pipeline，返回 {key, count, location, extras} 行。
// pipeline 由调用方构造（元素为完整 stage 文档，如 {"$match": {...}}、{"$group": {...}}）。
func (m *AssetModel) AggregateTargetGroups(ctx context.Context, pipeline []bson.M) ([]AssetTargetGroupResult, error) {
	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []AssetTargetGroupResult
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// FindForTargetInventory 目标资产列表查询：排除 body/header/banner/cert/screenshot/icon_hash_bytes 大字段，update_time 降序。
// 截图与 favicon 由前端拿到列表后经 /asset/media 按 id 懒加载，避免列表页一次携带全量截图（单页可达数百 KB）。
func (m *AssetModel) FindForTargetInventory(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	page, pageSize = NormalizePage(page, pageSize)
	return m.findPagedWithProjection(ctx, filter, page, pageSize, AssetListProjection)
}

// FindAllForAgg 按 filter 全量查询（仅投影 AssetAggProjection，update_time 降序），
// 供 DomainList/IPList/DomainStat/IPStat 等"全量文档去重聚合后再内存分页"的接口使用。
// 不能走 FindWithSort：NormalizePage 出于外部接口防滥用把 pageSize 钳到 100，
// 会把去重数据源截断成最新 100 条资产，导致列表数量与统计口径不一致。
func (m *AssetModel) FindAllForAgg(ctx context.Context, filter bson.M) ([]Asset, error) {
	opts := options.Find().
		SetProjection(AssetAggProjection).
		SetSort(bson.D{{Key: "update_time", Value: -1}})

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

// FindMediaByIds 按资产 ID 批量查询媒体大字段（screenshot + icon_hash_bytes），供列表懒加载接口使用。
func (m *AssetModel) FindMediaByIds(ctx context.Context, ids []string) ([]Asset, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return nil, nil
	}

	opts := options.Find().SetProjection(bson.M{"_id": 1, "screenshot": 1, "icon_hash_bytes": 1})
	cursor, err := m.coll.Find(ctx, bson.M{"_id": bson.M{"$in": oids}}, opts)
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

// FindForFingerprint 按指纹匹配所需字段查询（排除 screenshot/cert/banner 三个二进制大字段）
// 仅用于指纹匹配场景；如需全字段文档，请使用 FindWithSort 或 FindById。
func (m *AssetModel) FindForFingerprint(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	page, pageSize = NormalizePage(page, pageSize)
	return m.findPagedWithProjection(ctx, filter, page, pageSize, AssetFingerprintProjection)
}

// FindWithScreenshot 截图查询专用，包含 screenshot 字段
func (m *AssetModel) FindWithScreenshot(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	page, pageSize = NormalizePage(page, pageSize)
	return m.findPagedWithProjection(ctx, filter, page, pageSize, AssetScreenshotProjection)
}

// FindForSite 站点列表专用，保留 icon_hash_bytes 用于展示 favicon
func (m *AssetModel) FindForSite(ctx context.Context, filter bson.M, page, pageSize int) ([]Asset, error) {
	page, pageSize = NormalizePage(page, pageSize)
	return m.findPagedWithProjection(ctx, filter, page, pageSize, AssetSiteProjection)
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
	page, pageSize = NormalizePage(page, pageSize)
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

// AggregateInventoryPaged 跨工作空间集合的分页查询（单租户塌缩后 wsIds 通常为单元素）。
//
// 优化点（替代原 $unionWith + $facet 方案）：
//   - total：空 filter 时走 O(1) 的 EstimatedDocumentCount，否则走 CountDocuments；
//     不再依赖 $facet 把全局 $sort 结果全量物化，避免大数据量触发 MongoDB 100MB 内存上限。
//   - list：使用 Find + 索引排序（默认按 update_time 降序，复用索引）+ skip/limit，
//     仅将当前页所需文档拉回，避免整表排序后切片。
//
// sortField 以 "-" 前缀表示降序（如 "-update_time"），无前缀表示升序。
func (m *AssetModel) AggregateInventoryPaged(ctx context.Context, filter bson.M, skip, limit int64, sortField string) (int64, []Asset, error) {
	sortKey := sortField
	sortOrder := 1
	if strings.HasPrefix(sortKey, "-") {
		sortKey = sortKey[1:]
		sortOrder = -1
	}

	// 总数：空过滤走估算（O(1)），带过滤走精确计数
	var total int64
	var err error
	if len(filter) == 0 {
		total, err = m.coll.EstimatedDocumentCount(ctx)
	} else {
		total, err = m.coll.CountDocuments(ctx, filter)
	}
	if err != nil {
		return 0, nil, err
	}
	if total == 0 {
		return 0, []Asset{}, nil
	}

	opts := options.Find().
		SetProjection(AssetInventoryProjection).
		SetSort(bson.D{{Key: sortKey, Value: sortOrder}}).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return 0, nil, err
	}
	defer cursor.Close(ctx)

	var assets []Asset
	if err := cursor.All(ctx, &assets); err != nil {
		return 0, nil, err
	}

	return total, assets, nil
}

// EstimatedCount 使用集合元数据快速估算文档总数（O(1)），仅适用于空 filter 场景
// 用于列表分页总数统计，比 CountDocuments(bson.M{}) 快几个数量级
func (m *AssetModel) EstimatedCount(ctx context.Context) (int64, error) {
	return m.coll.EstimatedDocumentCount(ctx)
}

// Distinct 返回指定字段的不重复值列表（在 DB 端完成去重，避免全表加载到内存）
func (m *AssetModel) Distinct(ctx context.Context, field string, filter bson.M) ([]interface{}, error) {
	return m.coll.Distinct(ctx, field, filter)
}

// CountByTaskId 根据任务ID统计资产数量
func (m *AssetModel) CountByTaskId(ctx context.Context, taskId string) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{"taskId": taskId})
}

// CountByTaskIdWithPort 按任务ID统计有真实端口的服务资产数（port>0），
// 与资产空间搜索「服务」列表口径一致，剔除端口 0 的子域名占位记录。
func (m *AssetModel) CountByTaskIdWithPort(ctx context.Context, taskId string) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{"taskId": taskId, "port": bson.M{"$gt": 0}})
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
	// 仅在调用方未显式设置 update_time 时推进，避免 no-op 写入回归字段
	if _, ok := update["update_time"]; !ok {
		update["update_time"] = time.Now()
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// UpdateWithRaw 使用原始 MongoDB 更新文档更新资产（支持 $addToSet 等操作符）
func (m *AssetModel) UpdateWithRaw(ctx context.Context, id string, rawUpdate bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	// 仅在 $set 未显式包含 update_time 时推进，让 helper / 调用方可以精确控制
	if setFields, ok := rawUpdate["$set"].(bson.M); ok {
		if _, has := setFields["update_time"]; !has {
			setFields["update_time"] = time.Now()
		}
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

// UpdateManyByFilter 批量更新满足条件的资产
func (m *AssetModel) UpdateManyByFilter(ctx context.Context, filter bson.M, update bson.M) (int64, error) {
	result, err := m.coll.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
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
// NewCount 语义：first_seen_time >= since 的资产数（用于"较昨日"统计）
// 兼容：first_seen_time 缺失时回退到 create_time
func (m *AssetModel) AggregateOverviewStats(ctx context.Context) (*AssetOverviewStats, error) {
	since := time.Now().AddDate(0, 0, -1)
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "totalAsset", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "newCount", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$gte", Value: bson.A{
					bson.D{{Key: "$ifNull", Value: bson.A{"$first_seen_time", "$create_time"}}},
					since,
				}}},
				1, 0,
			}}}}}},
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

// DistinctPortCount 统计去重端口数（port > 0），与 AggregatePortList 的 total 口径一致
func (m *AssetModel) DistinctPortCount(ctx context.Context) (int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "port", Value: bson.D{{Key: "$gt", Value: 0}}}}}},
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$port"}}}},
		{{Key: "$count", Value: "total"}},
	}
	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	var result struct {
		Total int64 `bson:"total"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 0, err
		}
	}
	return result.Total, nil
}

// AssetChangeStats 工作台资产变化统计结果
type AssetChangeStats struct {
	Total       int64
	NewInWindow int64
	ByCategory  map[string]int64
}

// AggregateChangesStats 统计资产总数、窗口内新增数及新增分类分布（单次 $facet 聚合）
// 窗口口径与 T1.2 一致：first_seen_time（缺失回退 create_time）>= cutoff
func (m *AssetModel) AggregateChangesStats(ctx context.Context, cutoff time.Time) (*AssetChangeStats, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$facet", Value: bson.D{
			{Key: "total", Value: bson.A{bson.D{{Key: "$count", Value: "c"}}}},
			{Key: "newByCat", Value: bson.A{
				bson.D{{Key: "$match", Value: bson.D{{Key: "$expr", Value: bson.D{{Key: "$gte", Value: bson.A{
					bson.D{{Key: "$ifNull", Value: bson.A{"$first_seen_time", "$create_time"}}}, cutoff}}}}}}},
				bson.D{{Key: "$group", Value: bson.D{
					{Key: "_id", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$category", ""}}}},
					{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
				}}},
			}},
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	result := &AssetChangeStats{ByCategory: map[string]int64{}}
	if cursor.Next(ctx) {
		var facet struct {
			Total    []struct{ C int64 `bson:"c"` } `bson:"total"`
			NewByCat []struct {
				ID    string `bson:"_id"`
				Count int64  `bson:"count"`
			} `bson:"newByCat"`
		}
		if err := cursor.Decode(&facet); err != nil {
			return nil, err
		}
		if len(facet.Total) > 0 {
			result.Total = facet.Total[0].C
		}
		for _, c := range facet.NewByCat {
			result.ByCategory[c.ID] = c.Count
			result.NewInWindow += c.Count
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// SiteStatsResult 站点统计结果（一次 $facet 聚合替代 4 次 CountDocuments）
type SiteStatsResult struct {
	Total    int64 `bson:"total" json:"total"`
	Http     int64 `bson:"http" json:"http"`
	Https    int64 `bson:"https" json:"https"`
	NewCount int64 `bson:"newCount" json:"newCount"`
}

// AggregateSiteStats 用 $facet 一次聚合站点统计（替代 4 次 CountDocuments）
// webFilter: Web 资产过滤条件（is_http/service=http,https/title/screenshot）
// httpFilter: HTTP 过滤条件（service=http 或 port=80）
// httpsFilter: HTTPS 过滤条件（service=https 或 port=443）
// newFilter: 新增过滤条件（new=true）
func (m *AssetModel) AggregateSiteStats(ctx context.Context, webFilter, httpFilter, httpsFilter, newFilter bson.M) (*SiteStatsResult, error) {
	// 合并 filter：每个子过滤条件都要先 $match webFilter
	// 注意：bson.A 是 []interface{}，其元素无法做类型推断，必须显式写 bson.D{...}
	pipeline := mongo.Pipeline{
		{{Key: "$facet", Value: bson.D{
			{Key: "total", Value: bson.A{
				bson.D{{Key: "$match", Value: webFilter}},
				bson.D{{Key: "$count", Value: "n"}},
			}},
			{Key: "http", Value: bson.A{
				bson.D{{Key: "$match", Value: mergeFilters(webFilter, httpFilter)}},
				bson.D{{Key: "$count", Value: "n"}},
			}},
			{Key: "https", Value: bson.A{
				bson.D{{Key: "$match", Value: mergeFilters(webFilter, httpsFilter)}},
				bson.D{{Key: "$count", Value: "n"}},
			}},
			{Key: "newCount", Value: bson.A{
				bson.D{{Key: "$match", Value: mergeFilters(webFilter, newFilter)}},
				bson.D{{Key: "$count", Value: "n"}},
			}},
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	type facetResult struct {
		Total []struct {
			N int64 `bson:"n"`
		} `bson:"total"`
		Http []struct {
			N int64 `bson:"n"`
		} `bson:"http"`
		Https []struct {
			N int64 `bson:"n"`
		} `bson:"https"`
		NewCount []struct {
			N int64 `bson:"n"`
		} `bson:"newCount"`
	}

	var fr facetResult
	if cursor.Next(ctx) {
		if err := cursor.Decode(&fr); err != nil {
			return nil, err
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}

	result := &SiteStatsResult{}
	if len(fr.Total) > 0 {
		result.Total = fr.Total[0].N
	}
	if len(fr.Http) > 0 {
		result.Http = fr.Http[0].N
	}
	if len(fr.Https) > 0 {
		result.Https = fr.Https[0].N
	}
	if len(fr.NewCount) > 0 {
		result.NewCount = fr.NewCount[0].N
	}
	return result, nil
}

// mergeFilters 合并两个 filter（用 $and）
func mergeFilters(a, b bson.M) bson.M {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	return bson.M{"$and": []bson.M{a, b}}
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
		// 归一化技术键：剥掉 [来源] 后缀与 :版本号、忽略大小写，
		// 同一技术的多变体（"Nginx[httpx]" vs "Nginx:1.18[custom(id)]"）折叠为一组
		{{Key: "$addFields", Value: bson.D{{Key: "appKey", Value: bson.D{
			{Key: "$toLower", Value: bson.D{
				{Key: "$trim", Value: bson.D{
					{Key: "input", Value: bson.D{
						{Key: "$arrayElemAt", Value: bson.A{
							bson.D{{Key: "$split", Value: bson.A{
								bson.D{{Key: "$arrayElemAt", Value: bson.A{
									bson.D{{Key: "$split", Value: bson.A{"$app", "["}}},
									0,
								}}},
								":",
							}}},
							0,
						}},
					}},
				}},
			}},
		}}}}},
		// 同一资产内的变体先折叠成一条，保证 count = 含该技术的资产数而非变体计数
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "asset", Value: "$_id"},
				{Key: "key", Value: "$appKey"},
			}},
			{Key: "app", Value: bson.D{{Key: "$first", Value: "$app"}}},
		}}},
		// 按归一化键分组计数；_id 回写为组内首个原始条目（调用方按 app 原串做 $in 精确匹配）
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$_id.key"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "app", Value: bson.D{{Key: "$first", Value: "$app"}}},
		}}},
		{{Key: "$set", Value: bson.D{{Key: "_id", Value: "$app"}}}},
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
	Category   string             `bson:"category,omitempty" json:"category"`
	Service    string             `bson:"service,omitempty" json:"service"`
	Server     string             `bson:"server,omitempty" json:"server"`
	Title      string             `bson:"title,omitempty" json:"title"`
	App        []string           `bson:"app,omitempty" json:"app"`
	HttpStatus string             `bson:"status,omitempty" json:"httpStatus"`
	HttpHeader string             `bson:"header,omitempty" json:"httpHeader"`
	HttpBody   string             `bson:"body,omitempty" json:"httpBody"`
	Banner     string             `bson:"banner,omitempty" json:"banner"`
	Cert       string             `bson:"cert,omitempty" json:"cert"`
	IconHash   string             `bson:"icon_hash,omitempty" json:"iconHash"`
	Screenshot string             `bson:"screenshot,omitempty" json:"screenshot"`
	Domain     string             `bson:"domain,omitempty" json:"domain"`
	CName      string             `bson:"cname,omitempty" json:"cname"`
	Source     string             `bson:"source,omitempty" json:"source"`
	OrgId      string             `bson:"org_id,omitempty" json:"orgId"`
	Labels     []string           `bson:"labels,omitempty" json:"labels"`
	Memo       string             `bson:"memo,omitempty" json:"memo"`
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

func NewAssetHistoryModel(db *mongo.Database) *AssetHistoryModel {
	return &AssetHistoryModel{
		coll: db.Collection("asset_history"),
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

// SnapshotFromAsset builds an AssetHistory snapshot from an existing asset,
// used as the "old state" record before a cross-task or manual update.
// `changes` carries the field-level diff against this snapshot.
func SnapshotFromAsset(a *Asset, taskId string, createTime time.Time, changes []FieldChange) *AssetHistory {
	if createTime.IsZero() {
		createTime = a.UpdateTime
	}
	return &AssetHistory{
		AssetId:    a.Id.Hex(),
		Authority:  a.Authority,
		Host:       a.Host,
		Port:       a.Port,
		Category:   a.Category,
		Service:    a.Service,
		Server:     a.Server,
		Title:      a.Title,
		App:        a.App,
		HttpStatus: a.HttpStatus,
		HttpHeader: a.HttpHeader,
		HttpBody:   a.HttpBody,
		Banner:     a.Banner,
		Cert:       a.Cert,
		IconHash:   a.IconHash,
		Screenshot: a.Screenshot,
		Domain:     a.Domain,
		CName:      a.CName,
		Source:     a.Source,
		OrgId:      a.OrgId,
		Labels:     a.Labels,
		Memo:       a.Memo,
		TaskId:     taskId,
		CreateTime: createTime,
		Changes:    changes,
	}
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

// DeleteByFilter 按条件删除历史记录（供顶层资产级联删除复用）
func (m *AssetHistoryModel) DeleteByFilter(ctx context.Context, filter bson.M) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, filter)
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

// UpsertByAuthority performs an authority-keyed upsert using a pre-built update
// document (typically produced by BuildAssetUpdateDoc).
// Existing must be the same asset as the caller already pre-read (or nil for a
// brand-new asset). Existing is taken here purely for call-site clarity; the
// actual $setOnInsert block is determined by the doc's presence of the key.
func (m *AssetModel) UpsertByAuthority(ctx context.Context, authority string, update bson.M) error {
	if authority == "" {
		return errors.New("asset authority cannot be empty")
	}
	filter := bson.M{"authority": authority}
	uopts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, uopts)
	return err
}

// UpsertResult Upsert操作结果
type UpsertResult struct {
	IsNew bool // 是否为新插入（true=新增，false=已存在/更新）
}

// UpsertWithResult 插入或更新资产，返回是否为新增
func (m *AssetModel) UpsertWithResult(ctx context.Context, doc *Asset) (*UpsertResult, error) {
	if doc.Authority == "" {
		return nil, errors.New("asset authority cannot be empty")
	}
	if doc.Host == "" {
		return nil, errors.New("asset host cannot be empty")
	}

	filter := bson.M{"authority": doc.Authority}
	existing, _ := m.FindByAuthorityOnly(ctx, doc.Authority)
	opts := AssetWriteOptions{
		TaskId:               doc.TaskId,
		IsDifferentTask:      existing != nil && existing.TaskId != doc.TaskId && doc.TaskId != "",
		AllowClearUserFields: false,
	}
	update, _ := BuildAssetUpdateDoc(doc, existing, opts)

	uopts := options.Update().SetUpsert(true)
	result, err := m.coll.UpdateOne(ctx, filter, update, uopts)
	if err != nil {
		return nil, err
	}
	return &UpsertResult{IsNew: result.UpsertedCount > 0}, nil
}

// Upsert 插入或更新资产
// 该方法保留向后兼容：内部预读 existing 后委托给 BuildAssetUpdateDoc，
// 由统一的 helper 控制 $set / $setOnInsert / $addToSet 的语义。
// 新代码应直接调用 BuildAssetUpdateDoc，按需选择是否预读。
func (m *AssetModel) Upsert(ctx context.Context, doc *Asset) error {
	// authority 是资产唯一标识（host:port），为空会导致多条空 authority 资产互相覆盖、
	// 前端展示异常以及后续去重逻辑失效，必须在此前置拦截。
	if doc.Authority == "" {
		return errors.New("asset authority cannot be empty")
	}
	if doc.Host == "" {
		return errors.New("asset host cannot be empty")
	}

	// 仅按 authority 匹配，忽略 taskId，确保同一资产被合并
	filter := bson.M{"authority": doc.Authority}

	// 预读 existing 以驱动 helper 的 diff / 状态字段门控
	existing, _ := m.FindByAuthorityOnly(ctx, doc.Authority)
	opts := AssetWriteOptions{
		TaskId:               doc.TaskId,
		IsDifferentTask:      existing != nil && existing.TaskId != doc.TaskId && doc.TaskId != "",
		AllowClearUserFields: false,
	}
	update, _ := BuildAssetUpdateDoc(doc, existing, opts)

	uopts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, uopts)
	return err
}

// BulkUpsert 批量插入或更新资产
// 与 Upsert 语义对齐：所有业务字段采用 omit-if-empty 保护，
// 避免空值覆盖已有数据。状态字段 update_time / last_status_change_time 在写入时推进。
func (m *AssetModel) BulkUpsert(ctx context.Context, assets []*Asset) (*mongo.BulkWriteResult, error) {
	if len(assets) == 0 {
		return nil, nil
	}

	now := time.Now()
	var models []mongo.WriteModel
	for _, asset := range assets {
		filter := bson.M{"host": asset.Host, "port": asset.Port}
		setFields := bson.M{
			"authority":              asset.Authority,
			"host":                   asset.Host,
			"port":                   asset.Port,
			"is_http":                asset.IsHTTP,
			"cdn":                    asset.IsCDN,
			"cloud":                  asset.IsCloud,
			"update_time":            now,
			"update":                 true,
			"last_status_change_time": now,
		}
		// 业务字段 omit-if-empty，避免覆盖已有数据
		if asset.Category != "" {
			setFields["category"] = asset.Category
		}
		if asset.Service != "" {
			setFields["service"] = asset.Service
		}
		if asset.Server != "" {
			setFields["server"] = asset.Server
		}
		if asset.Banner != "" {
			setFields["banner"] = asset.Banner
		}
		if asset.Title != "" {
			setFields["title"] = asset.Title
		}
		if asset.HttpStatus != "" {
			setFields["status"] = asset.HttpStatus
		}
		if asset.HttpHeader != "" {
			setFields["header"] = asset.HttpHeader
		}
		if asset.HttpBody != "" {
			setFields["body"] = asset.HttpBody
		}
		if asset.Cert != "" {
			setFields["cert"] = asset.Cert
		}
		if asset.IconHash != "" {
			setFields["icon_hash"] = asset.IconHash
		}
		if asset.Screenshot != "" {
			setFields["screenshot"] = asset.Screenshot
		}
		if asset.CName != "" {
			setFields["cname"] = asset.CName
		}
		if asset.Domain != "" {
			setFields["domain"] = asset.Domain
		}
		if asset.Source != "" {
			setFields["source"] = asset.Source
		}
		if asset.TaskId != "" {
			setFields["taskId"] = asset.TaskId
		}
		if len(asset.IconHashBytes) > 0 {
			setFields["icon_hash_bytes"] = asset.IconHashBytes
		}
		if len(asset.Ip.IpV4) > 0 || len(asset.Ip.IpV6) > 0 {
			setFields["ip"] = asset.Ip
		}

		update := bson.M{
			"$set": setFields,
			"$setOnInsert": bson.M{
				"_id":                primitive.NewObjectID(),
				"create_time":        now,
				"new":                true,
				"first_seen_time":    now,
				"first_seen_task_id": asset.TaskId,
			},
		}
		// app 合并而非覆盖
		if len(asset.App) > 0 {
			update["$addToSet"] = bson.M{"app": bson.M{"$each": asset.App}}
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
	CreateTime time.Time `bson:"createTime"`
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
			{Key: "createTime", Value: bson.D{{Key: "$min", Value: "$create_time"}}},
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
