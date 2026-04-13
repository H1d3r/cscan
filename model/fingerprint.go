package model

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// HttpServiceMapping HTTP服务标识映射
// 用于判断端口扫描识别的Service是否为HTTP/HTTPS服务
type HttpServiceMapping struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ServiceName string             `bson:"service_name" json:"serviceName"` // 服务名称（小写）如: http, https, http-proxy
	IsHttp      bool               `bson:"is_http" json:"isHttp"`           // 是否为HTTP服务
	Description string             `bson:"description" json:"description"`  // 描述
	Enabled     bool               `bson:"enabled" json:"enabled"`          // 是否启用
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

// HttpServiceMappingModel HTTP服务映射模型
type HttpServiceMappingModel struct {
	coll  *mongo.Collection
	cache map[string]bool // 缓存: serviceName -> isHttp
	mu    sync.RWMutex
}

func NewHttpServiceMappingModel(db *mongo.Database) *HttpServiceMappingModel {
	coll := db.Collection("http_service_mapping")
	// 创建唯一索引
	coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "service_name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	m := &HttpServiceMappingModel{
		coll:  coll,
		cache: make(map[string]bool),
	}
	// 初始化时加载缓存
	m.RefreshCache(context.Background())
	return m
}

func (m *HttpServiceMappingModel) Insert(ctx context.Context, doc *HttpServiceMapping) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	if err == nil {
		m.RefreshCache(ctx)
	}
	return err
}

func (m *HttpServiceMappingModel) FindAll(ctx context.Context) ([]HttpServiceMapping, error) {
	// 按创建时间倒序排列，新增的排最前
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	cursor, err := m.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []HttpServiceMapping
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindWithFilter 带筛选条件查询
func (m *HttpServiceMappingModel) FindWithFilter(ctx context.Context, isHttp *bool, keyword string) ([]HttpServiceMapping, error) {
	filter := bson.M{}
	if isHttp != nil {
		filter["is_http"] = *isHttp
	}
	if keyword != "" {
		filter["service_name"] = bson.M{"$regex": keyword, "$options": "i"}
	}
	// 按创建时间倒序排列，新增的排最前
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []HttpServiceMapping
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *HttpServiceMappingModel) FindEnabled(ctx context.Context) ([]HttpServiceMapping, error) {
	cursor, err := m.coll.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []HttpServiceMapping
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *HttpServiceMappingModel) FindById(ctx context.Context, id string) (*HttpServiceMapping, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc HttpServiceMapping
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	return &doc, err
}

func (m *HttpServiceMappingModel) FindByServiceName(ctx context.Context, serviceName string) (*HttpServiceMapping, error) {
	var doc HttpServiceMapping
	err := m.coll.FindOne(ctx, bson.M{"service_name": serviceName}).Decode(&doc)
	return &doc, err
}

func (m *HttpServiceMappingModel) Update(ctx context.Context, id string, doc *HttpServiceMapping) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"service_name": doc.ServiceName,
		"is_http":      doc.IsHttp,
		"description":  doc.Description,
		"enabled":      doc.Enabled,
		"update_time":  time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	if err == nil {
		m.RefreshCache(ctx)
	}
	return err
}

func (m *HttpServiceMappingModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	if err == nil {
		m.RefreshCache(ctx)
	}
	return err
}

// RefreshCache 刷新缓存
func (m *HttpServiceMappingModel) RefreshCache(ctx context.Context) {
	docs, err := m.FindEnabled(ctx)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[string]bool)
	for _, doc := range docs {
		m.cache[doc.ServiceName] = doc.IsHttp
	}
}

// IsHttpService 判断服务是否为HTTP服务（使用缓存）
func (m *HttpServiceMappingModel) IsHttpService(serviceName string) (isHttp bool, found bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	isHttp, found = m.cache[serviceName]
	return
}

// GetHttpServices 获取所有HTTP服务名称列表
func (m *HttpServiceMappingModel) GetHttpServices() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var services []string
	for name, isHttp := range m.cache {
		if isHttp {
			services = append(services, name)
		}
	}
	return services
}

// GetNonHttpServices 获取所有非HTTP服务名称列表
func (m *HttpServiceMappingModel) GetNonHttpServices() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var services []string
	for name, isHttp := range m.cache {
		if !isHttp {
			services = append(services, name)
		}
	}
	return services
}

// FingerprintType 指纹类型
type FingerprintType string

const (
	FingerprintTypePassive FingerprintType = "passive" // 被动指纹：通过响应内容识别
	FingerprintTypeActive  FingerprintType = "active"  // 主动指纹：通过访问特定路径识别
)

// Fingerprint 指纹规则
type Fingerprint struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`               // 应用名称
	Category    string             `bson:"category" json:"category"`       // 分类: cms, framework, server, etc.
	Website     string             `bson:"website" json:"website"`         // 官网
	Icon        string             `bson:"icon" json:"icon"`               // 图标URL
	Description string             `bson:"description" json:"description"` // 描述
	// 指纹类型
	Type FingerprintType `bson:"type" json:"type"` // 指纹类型: passive(被动), active(主动)
	// 主动指纹专用字段
	ActivePaths []string `bson:"active_paths" json:"activePaths"` // 主动探测路径列表，如 ["/admin/login.php", "/wp-admin/"]
	// 匹配规则 - Wappalyzer格式
	Headers   map[string]string `bson:"headers" json:"headers"`     // HTTP头匹配 {"Server": "nginx"}
	Cookies   map[string]string `bson:"cookies" json:"cookies"`     // Cookie匹配
	HTML      []string          `bson:"html" json:"html"`           // HTML内容匹配（正则）
	Scripts   []string          `bson:"scripts" json:"scripts"`     // JS脚本路径匹配（正则）
	ScriptSrc []string          `bson:"scriptSrc" json:"scriptSrc"` // Script src匹配
	JS        map[string]string `bson:"js" json:"js"`               // JS变量匹配
	Meta      map[string]string `bson:"meta" json:"meta"`           // Meta标签匹配
	CSS       []string          `bson:"css" json:"css"`             // CSS匹配（正则）
	URL       []string          `bson:"url" json:"url"`             // URL路径匹配（正则）
	Dom       string            `bson:"dom" json:"dom"`             // DOM选择器匹配（JSON字符串）
	// 匹配规则 - ARL/自定义格式（简化规则语法）
	Rule string `bson:"rule" json:"rule"` // ARL格式规则: body="xxx" && title="xxx"
	// 其他
	Implies    []string  `bson:"implies" json:"implies"`      // 隐含的其他技术
	Excludes   []string  `bson:"excludes" json:"excludes"`    // 排除的技术
	CPE        string    `bson:"cpe" json:"cpe"`              // CPE标识
	Source     string    `bson:"source" json:"source"`        // 来源: wappalyzer, arl, custom
	IsBuiltin  bool      `bson:"is_builtin" json:"isBuiltin"` // 是否内置指纹
	Enabled    bool      `bson:"enabled" json:"enabled"`      // 是否启用
	CreateTime time.Time `bson:"create_time" json:"createTime"`
	UpdateTime time.Time `bson:"update_time" json:"updateTime"`
}

// FingerprintModel 指纹模型
type FingerprintModel struct {
	coll *mongo.Collection
}

func NewFingerprintModel(db *mongo.Database) *FingerprintModel {
	coll := db.Collection("fingerprint")
	// 创建索引 - name+rule 组合唯一，允许同名不同规则的指纹
	coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "name", Value: 1}, {Key: "rule", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "name", Value: 1}}},
		{Keys: bson.D{{Key: "category", Value: 1}}},
		{Keys: bson.D{{Key: "is_builtin", Value: 1}}},
		{Keys: bson.D{{Key: "enabled", Value: 1}}},
		{Keys: bson.D{{Key: "type", Value: 1}}}, // 指纹类型索引
	})
	return &FingerprintModel{coll: coll}
}

func (m *FingerprintModel) Insert(ctx context.Context, doc *Fingerprint) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *FingerprintModel) Upsert(ctx context.Context, doc *Fingerprint) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	doc.UpdateTime = time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = doc.UpdateTime
	}

	// 使用 name + rule 作为去重条件，只有两者都相同才视为重复
	filter := bson.M{"name": doc.Name, "rule": doc.Rule}
	update := bson.M{"$set": doc}
	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *FingerprintModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]Fingerprint, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	// 按创建时间倒序排序，最新添加的在前
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}, {Key: "name", Value: 1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Fingerprint
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *FingerprintModel) FindAll(ctx context.Context) ([]Fingerprint, error) {
	return m.Find(ctx, bson.M{}, 0, 0)
}

func (m *FingerprintModel) FindEnabled(ctx context.Context) ([]Fingerprint, error) {
	return m.Find(ctx, bson.M{"enabled": true}, 0, 0)
}

// FindPassiveEnabled 查询启用的被动指纹（用于默认指纹扫描）
func (m *FingerprintModel) FindPassiveEnabled(ctx context.Context) ([]Fingerprint, error) {
	// 被动指纹：type为空或为passive
	filter := bson.M{
		"enabled": true,
		"$or": []bson.M{
			{"type": ""},
			{"type": nil},
			{"type": FingerprintTypePassive},
		},
	}
	return m.Find(ctx, filter, 0, 0)
}

// FindActiveEnabled 查询启用的主动指纹（用于主动指纹扫描）
func (m *FingerprintModel) FindActiveEnabled(ctx context.Context) ([]Fingerprint, error) {
	filter := bson.M{
		"enabled": true,
		"type":    FingerprintTypeActive,
	}
	return m.Find(ctx, filter, 0, 0)
}

// FindByType 按类型查询指纹
func (m *FingerprintModel) FindByType(ctx context.Context, fpType FingerprintType, page, pageSize int) ([]Fingerprint, error) {
	var filter bson.M
	if fpType == FingerprintTypePassive || fpType == "" {
		// 被动指纹：type为空或为passive
		filter = bson.M{
			"$or": []bson.M{
				{"type": ""},
				{"type": nil},
				{"type": FingerprintTypePassive},
			},
		}
	} else {
		filter = bson.M{"type": fpType}
	}
	return m.Find(ctx, filter, page, pageSize)
}

func (m *FingerprintModel) FindCustom(ctx context.Context) ([]Fingerprint, error) {
	return m.Find(ctx, bson.M{"is_builtin": false}, 0, 0)
}

func (m *FingerprintModel) FindBuiltin(ctx context.Context) ([]Fingerprint, error) {
	return m.Find(ctx, bson.M{"is_builtin": true}, 0, 0)
}

func (m *FingerprintModel) FindById(ctx context.Context, id string) (*Fingerprint, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc Fingerprint
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	return &doc, err
}

func (m *FingerprintModel) FindByName(ctx context.Context, name string) (*Fingerprint, error) {
	var doc Fingerprint
	err := m.coll.FindOne(ctx, bson.M{"name": name}).Decode(&doc)
	return &doc, err
}

// FindByNames 批量按名称查询指纹（用于主动指纹关联被动指纹规则）
func (m *FingerprintModel) FindByNames(ctx context.Context, names []string) ([]*Fingerprint, error) {
	if len(names) == 0 {
		return nil, nil
	}
	filter := bson.M{"name": bson.M{"$in": names}, "enabled": true}
	cursor, err := m.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []*Fingerprint
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *FingerprintModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

func (m *FingerprintModel) Update(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *FingerprintModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

func (m *FingerprintModel) GetCategories(ctx context.Context) ([]string, error) {
	results, err := m.coll.Distinct(ctx, "category", bson.M{})
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

func (m *FingerprintModel) GetStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)

	// 总数
	total, _ := m.coll.CountDocuments(ctx, bson.M{})
	stats["total"] = total

	// 内置数量
	builtin, _ := m.coll.CountDocuments(ctx, bson.M{"is_builtin": true})
	stats["builtin"] = builtin

	// 自定义数量
	custom, _ := m.coll.CountDocuments(ctx, bson.M{"is_builtin": false})
	stats["custom"] = custom

	// 启用数量
	enabled, _ := m.coll.CountDocuments(ctx, bson.M{"enabled": true})
	stats["enabled"] = enabled

	// 被动指纹数量（type为空或passive）
	passive, _ := m.coll.CountDocuments(ctx, bson.M{
		"$or": []bson.M{
			{"type": ""},
			{"type": nil},
			{"type": FingerprintTypePassive},
		},
	})
	stats["passive"] = passive

	// 主动指纹数量
	active, _ := m.coll.CountDocuments(ctx, bson.M{"type": FingerprintTypeActive})
	stats["active"] = active

	return stats, nil
}

// DeleteAll 删除所有指纹
func (m *FingerprintModel) DeleteAll(ctx context.Context) error {
	_, err := m.coll.DeleteMany(ctx, bson.M{})
	return err
}

// DeleteBuiltin 删除所有内置指纹
func (m *FingerprintModel) DeleteBuiltin(ctx context.Context) error {
	_, err := m.coll.DeleteMany(ctx, bson.M{"is_builtin": true})
	return err
}

// DeleteCustom 删除所有自定义指纹（非内置）
func (m *FingerprintModel) DeleteCustom(ctx context.Context) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{"is_builtin": false})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// DeleteBySource 按来源删除指纹
func (m *FingerprintModel) DeleteBySource(ctx context.Context, source string) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{"source": source})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// BulkUpsert 批量插入或更新指纹
// 去重原则：只有 name 和 rule 都完全相同才视为重复
// 返回: 新插入数量, 更新数量(包括匹配但未修改的), 错误
func (m *FingerprintModel) BulkUpsert(ctx context.Context, docs []*Fingerprint) (int, int, error) {
	if len(docs) == 0 {
		return 0, 0, nil
	}

	var models []mongo.WriteModel
	now := time.Now()

	for _, doc := range docs {
		if doc.Id.IsZero() {
			doc.Id = primitive.NewObjectID()
		}
		doc.UpdateTime = now
		if doc.CreateTime.IsZero() {
			doc.CreateTime = now
		}

		// 使用 name + rule 作为去重条件，只有两者都相同才视为重复
		filter := bson.M{"name": doc.Name, "rule": doc.Rule}
		update := bson.M{"$set": doc}
		model := mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true)
		models = append(models, model)
	}

	// 分批执行，每批500条
	batchSize := 500
	var inserted, matched int

	for i := 0; i < len(models); i += batchSize {
		end := i + batchSize
		if end > len(models) {
			end = len(models)
		}

		result, err := m.coll.BulkWrite(ctx, models[i:end], options.BulkWrite().SetOrdered(false))
		if err != nil {
			// 记录错误但继续处理
			fmt.Printf("BulkWrite error: %v\n", err)
			continue
		}
		inserted += int(result.UpsertedCount)
		// MatchedCount 包括已存在的记录（无论是否修改）
		matched += int(result.MatchedCount)
	}

	// 返回新插入数量和匹配更新数量
	return inserted, matched, nil
}

// BatchUpdateEnabled 批量更新指纹启用状态
func (m *FingerprintModel) BatchUpdateEnabled(ctx context.Context, filter bson.M, enabled bool) (int64, error) {
	update := bson.M{
		"$set": bson.M{
			"enabled":     enabled,
			"update_time": time.Now(),
		},
	}
	result, err := m.coll.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

// ==================== 主动扫描指纹规则 ====================

// ActiveFingerprint 主动扫描指纹规则
// 独立存储主动探测路径，通过应用名称关联被动指纹
type ActiveFingerprint struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`               // 应用名称（用于关联被动指纹）
	Paths       []string           `bson:"paths" json:"paths"`             // 主动探测路径列表
	Description string             `bson:"description" json:"description"` // 描述
	Enabled     bool               `bson:"enabled" json:"enabled"`         // 是否启用
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

// ActiveFingerprintWithRelation 带关联信息的主动指纹
type ActiveFingerprintWithRelation struct {
	ActiveFingerprint
	RelatedFingerprints []Fingerprint `bson:"-" json:"relatedFingerprints"` // 关联的被动指纹列表
	RelatedCount        int           `bson:"-" json:"relatedCount"`        // 关联的被动指纹数量
}

// ActiveFingerprintModel 主动扫描指纹模型
type ActiveFingerprintModel struct {
	coll *mongo.Collection
}

func NewActiveFingerprintModel(db *mongo.Database) *ActiveFingerprintModel {
	coll := db.Collection("active_fingerprint")
	// 创建索引
	coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "enabled", Value: 1}}},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
	})
	return &ActiveFingerprintModel{coll: coll}
}

func (m *ActiveFingerprintModel) Insert(ctx context.Context, doc *ActiveFingerprint) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *ActiveFingerprintModel) Upsert(ctx context.Context, doc *ActiveFingerprint) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	doc.UpdateTime = time.Now()
	if doc.CreateTime.IsZero() {
		doc.CreateTime = doc.UpdateTime
	}

	filter := bson.M{"name": doc.Name}
	update := bson.M{"$set": doc}
	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *ActiveFingerprintModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]ActiveFingerprint, error) {
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []ActiveFingerprint
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *ActiveFingerprintModel) FindAll(ctx context.Context) ([]ActiveFingerprint, error) {
	return m.Find(ctx, bson.M{}, 0, 0)
}

func (m *ActiveFingerprintModel) FindEnabled(ctx context.Context) ([]ActiveFingerprint, error) {
	return m.Find(ctx, bson.M{"enabled": true}, 0, 0)
}

func (m *ActiveFingerprintModel) FindById(ctx context.Context, id string) (*ActiveFingerprint, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc ActiveFingerprint
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	return &doc, err
}

func (m *ActiveFingerprintModel) FindByName(ctx context.Context, name string) (*ActiveFingerprint, error) {
	var doc ActiveFingerprint
	err := m.coll.FindOne(ctx, bson.M{"name": name}).Decode(&doc)
	return &doc, err
}

func (m *ActiveFingerprintModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

func (m *ActiveFingerprintModel) Update(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *ActiveFingerprintModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

func (m *ActiveFingerprintModel) DeleteAll(ctx context.Context) error {
	_, err := m.coll.DeleteMany(ctx, bson.M{})
	return err
}

func (m *ActiveFingerprintModel) GetStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)
	total, _ := m.coll.CountDocuments(ctx, bson.M{})
	stats["total"] = total
	enabled, _ := m.coll.CountDocuments(ctx, bson.M{"enabled": true})
	stats["enabled"] = enabled
	return stats, nil
}

// BulkUpsert 批量插入或更新
func (m *ActiveFingerprintModel) BulkUpsert(ctx context.Context, docs []*ActiveFingerprint) (int, int, error) {
	if len(docs) == 0 {
		return 0, 0, nil
	}

	var models []mongo.WriteModel
	now := time.Now()

	for _, doc := range docs {
		doc.UpdateTime = now
		if doc.CreateTime.IsZero() {
			doc.CreateTime = now
		}

		filter := bson.M{"name": doc.Name}
		// 更新时不包含 _id 字段，避免 upsert 时修改已存在文档的 _id
		update := bson.M{
			"$set": bson.M{
				"name":        doc.Name,
				"paths":       doc.Paths,
				"description": doc.Description,
				"enabled":     doc.Enabled,
				"update_time": doc.UpdateTime,
			},
			"$setOnInsert": bson.M{
				"create_time": doc.CreateTime,
			},
		}
		model := mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true)
		models = append(models, model)
	}

	result, err := m.coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		return 0, 0, err
	}
	return int(result.UpsertedCount), int(result.MatchedCount), nil
}
