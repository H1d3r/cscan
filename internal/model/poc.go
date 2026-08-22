package model

import (
	"context"
	"log"
	"time"

	"cscan/pkg/template"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TagMapping 应用标签到Nuclei标签的映射
// 用于基于Wappalyzer识别的应用自动选择对应的POC
type TagMapping struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	AppName     string             `bson:"app_name" json:"appName"`        // Wappalyzer识别的应用名称
	NucleiTags  []string           `bson:"nuclei_tags" json:"nucleiTags"`  // 对应的Nuclei标签
	Description string             `bson:"description" json:"description"` // 描述
	Enabled     bool               `bson:"enabled" json:"enabled"`         // 是否启用
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

// CustomPoc 自定义POC
type CustomPoc struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`               // POC名称
	TemplateId  string             `bson:"template_id" json:"templateId"`  // 模板ID（唯一标识）
	Severity    string             `bson:"severity" json:"severity"`       // 严重级别: critical/high/medium/low/info
	Tags        []string           `bson:"tags" json:"tags"`               // 标签
	Author      string             `bson:"author" json:"author"`           // 作者
	Description string             `bson:"description" json:"description"` // 描述
	Protocol    string             `bson:"protocol" json:"protocol"`       // 请求协议: http/dns/network/ssl/file/headless等
	Vendor      string             `bson:"vendor,omitempty" json:"vendor,omitempty"`   // 厂商(metadata.vendor)
	Product     string             `bson:"product,omitempty" json:"product,omitempty"` // 产品(metadata.product)
	Content     string             `bson:"content" json:"content"`         // YAML内容
	Enabled     bool               `bson:"enabled" json:"enabled"`         // 是否启用
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`

	// 漏洞知识库字段
	CvssScore   float64  `bson:"cvss_score,omitempty" json:"cvssScore,omitempty"`     // CVSS评分
	CvssMetrics string   `bson:"cvss_metrics,omitempty" json:"cvssMetrics,omitempty"` // CVSS向量
	CveIds      []string `bson:"cve_ids,omitempty" json:"cveIds,omitempty"`           // CVE编号列表
	CweIds      []string `bson:"cwe_ids,omitempty" json:"cweIds,omitempty"`           // CWE编号列表
	References  []string `bson:"references,omitempty" json:"references,omitempty"`    // 参考链接
	Remediation string   `bson:"remediation,omitempty" json:"remediation,omitempty"`  // 修复建议
}

// TagMappingModel 标签映射模型
type TagMappingModel struct {
	coll *mongo.Collection
}

func NewTagMappingModel(db *mongo.Database) *TagMappingModel {
	return &TagMappingModel{
		coll: db.Collection("tag_mapping"),
	}
}

func (m *TagMappingModel) Insert(ctx context.Context, doc *TagMapping) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *TagMappingModel) FindAll(ctx context.Context) ([]TagMapping, error) {
	opts := options.Find().SetSort(bson.D{{Key: "app_name", Value: 1}})
	cursor, err := m.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []TagMapping
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *TagMappingModel) FindEnabled(ctx context.Context) ([]TagMapping, error) {
	cursor, err := m.coll.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []TagMapping
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *TagMappingModel) FindById(ctx context.Context, id string) (*TagMapping, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc TagMapping
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *TagMappingModel) FindByAppName(ctx context.Context, appName string) (*TagMapping, error) {
	var doc TagMapping
	err := m.coll.FindOne(ctx, bson.M{"app_name": appName}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *TagMappingModel) Update(ctx context.Context, id string, doc *TagMapping) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"app_name":    doc.AppName,
		"nuclei_tags": doc.NucleiTags,
		"description": doc.Description,
		"enabled":     doc.Enabled,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *TagMappingModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// CustomPocModel 自定义POC模型
type CustomPocModel struct {
	coll *mongo.Collection
}

func NewCustomPocModel(db *mongo.Database) *CustomPocModel {
	coll := db.Collection("custom_poc")
	coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "template_id", Value: 1}}},
		{Keys: bson.D{{Key: "severity", Value: 1}}},
		{Keys: bson.D{{Key: "protocol", Value: 1}}},
		{Keys: bson.D{{Key: "tags", Value: 1}}},
		{Keys: bson.D{{Key: "cve_ids", Value: 1}}},
		{Keys: bson.D{{Key: "product", Value: 1}}},
	})
	// 历史数据迁移：官方无 Unknown 级别；protocol 等字段缺失时从已存content重新解析回填
	go migrateCustomPocs(coll)
	return &CustomPocModel{coll: coll}
}

// migrateCustomPocs 一次性历史数据规范化（幂等）
func migrateCustomPocs(coll *mongo.Collection) {
	ctx := context.Background()
	// severity 空/unknown -> info
	if res, err := coll.UpdateMany(ctx,
		bson.M{"severity": bson.M{"$in": []string{"", "unknown"}}},
		bson.M{"$set": bson.M{"severity": "info"}},
	); err == nil && res.ModifiedCount > 0 {
		log.Printf("[CustomPoc] migrated severity unknown->info: %d docs", res.ModifiedCount)
	}
	// protocol/vendor/product/cve 等字段缺失 -> 重新解析已存content回填
	backfillCustomPocMetadata(ctx, coll)
}

// backfillCustomPocMetadata 解析存量POC content回填 协议/厂商/产品/知识库字段
func backfillCustomPocMetadata(ctx context.Context, coll *mongo.Collection) {
	filter := bson.M{"protocol": bson.M{"$exists": false}}
	count, err := coll.CountDocuments(ctx, filter)
	if err != nil || count == 0 {
		return
	}
	log.Printf("[CustomPoc] backfilling metadata from stored content: %d docs", count)

	opts := options.Find().SetProjection(bson.M{"content": 1}).SetBatchSize(500)
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		log.Printf("[CustomPoc] backfill find failed: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var models []mongo.WriteModel
	for cursor.Next(ctx) {
		var doc struct {
			Id      primitive.ObjectID `bson:"_id"`
			Content string             `bson:"content"`
		}
		if err := cursor.Decode(&doc); err != nil || doc.Id.IsZero() {
			continue
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": doc.Id}).
			SetUpdate(bson.M{"$set": contentMetadataSet(doc.Content)}))
		if len(models) >= 500 {
			if _, err := coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
				log.Printf("[CustomPoc] backfill bulk write failed: %v", err)
			}
			models = models[:0]
		}
	}
	if len(models) > 0 {
		if _, err := coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
			log.Printf("[CustomPoc] backfill bulk write failed: %v", err)
		}
	}
	log.Printf("[CustomPoc] backfill metadata completed: %d docs", count)
}

// contentMetadataSet 解析模板内容生成 协议/厂商/产品/漏洞知识库 字段集合（缺失字段写空值避免重复回填）
func contentMetadataSet(content string) bson.M {
	set := bson.M{
		"vendor":       "",
		"product":      "",
		"cvss_score":   0,
		"cvss_metrics": "",
		"cve_ids":      []string{},
		"cwe_ids":      []string{},
		"references":   []string{},
		"remediation":  "",
	}
	if info, err := template.ParseTemplateInfo(content); err == nil && info != nil {
		set["vendor"] = info.GetVendor()
		set["product"] = info.GetProduct()
		set["cvss_score"] = info.GetCvssScore()
		set["cvss_metrics"] = info.GetCvssMetrics()
		if v := info.GetCveIds(); len(v) > 0 {
			set["cve_ids"] = v
		}
		if v := info.GetCweIds(); len(v) > 0 {
			set["cwe_ids"] = v
		}
		if v := info.GetReferences(); len(v) > 0 {
			set["references"] = v
		}
		if v := info.GetRemediation(); v != "" {
			set["remediation"] = v
		}
	}
	if protocol := template.ParseProtocol(content); protocol != "" {
		set["protocol"] = protocol
	} else {
		set["protocol"] = ""
	}
	return set
}

func (m *CustomPocModel) Insert(ctx context.Context, doc *CustomPoc) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *CustomPocModel) FindAll(ctx context.Context, page, pageSize int) ([]CustomPoc, error) {
	page, pageSize = NormalizePage(page, pageSize)
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []CustomPoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindWithFilter 带筛选条件的查询
func (m *CustomPocModel) FindWithFilter(ctx context.Context, filter bson.M, page, pageSize int) ([]CustomPoc, error) {
	page, pageSize = NormalizePage(page, pageSize)
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

	var docs []CustomPoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// SelectAll 全选场景批量查询：无分页上限，仅投影选择所需轻量字段（name/template_id）
func (m *CustomPocModel) SelectAll(ctx context.Context, filter bson.M) ([]CustomPoc, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "create_time", Value: -1}}).
		SetProjection(bson.M{"name": 1, "template_id": 1})
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []CustomPoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// CountWithFilter 带筛选条件的计数
func (m *CustomPocModel) CountWithFilter(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

func (m *CustomPocModel) FindEnabled(ctx context.Context) ([]CustomPoc, error) {
	cursor, err := m.coll.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []CustomPoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *CustomPocModel) FindByTags(ctx context.Context, tags []string) ([]CustomPoc, error) {
	cursor, err := m.coll.Find(ctx, bson.M{
		"enabled": true,
		"tags":    bson.M{"$in": tags},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []CustomPoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *CustomPocModel) FindById(ctx context.Context, id string) (*CustomPoc, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc CustomPoc
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// FindByTemplateId 根据模板ID查找自定义POC
func (m *CustomPocModel) FindByTemplateId(ctx context.Context, templateId string) (*CustomPoc, error) {
	var doc CustomPoc
	err := m.coll.FindOne(ctx, bson.M{"template_id": templateId}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *CustomPocModel) Count(ctx context.Context) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{})
}

func (m *CustomPocModel) Update(ctx context.Context, id string, doc *CustomPoc) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"name":         doc.Name,
		"template_id":  doc.TemplateId,
		"severity":     doc.Severity,
		"tags":         doc.Tags,
		"author":       doc.Author,
		"description":  doc.Description,
		"content":      doc.Content,
		"enabled":      doc.Enabled,
		"update_time":  time.Now(),
		"protocol":     doc.Protocol,
		"vendor":       doc.Vendor,
		"product":      doc.Product,
		"cvss_score":   doc.CvssScore,
		"cvss_metrics": doc.CvssMetrics,
		"cve_ids":      doc.CveIds,
		"cwe_ids":      doc.CweIds,
		"references":   doc.References,
		"remediation":  doc.Remediation,
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *CustomPocModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// DeleteAll 删除所有自定义POC
func (m *CustomPocModel) DeleteAll(ctx context.Context) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// DeleteWithFilter 按条件删除自定义POC
func (m *CustomPocModel) DeleteWithFilter(ctx context.Context, filter bson.M) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// FindByIds 根据ID列表获取自定义POC
func (m *CustomPocModel) FindByIds(ctx context.Context, ids []string) ([]CustomPoc, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// 转换字符串ID为ObjectID
	oids := make([]primitive.ObjectID, 0, len(ids))
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

	var docs []CustomPoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// NucleiTemplate Nuclei默认模板（从模板目录同步）
type NucleiTemplate struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TemplateId  string             `bson:"template_id" json:"templateId"`  // 模板ID
	Name        string             `bson:"name" json:"name"`               // 模板名称
	Author      string             `bson:"author" json:"author"`           // 作者
	Severity    string             `bson:"severity" json:"severity"`       // 严重级别: critical/high/medium/low/info
	Description string             `bson:"description" json:"description"` // 描述
	Tags        []string           `bson:"tags" json:"tags"`               // 标签
	Category    string             `bson:"category" json:"category"`       // 分类(目录名)
	Protocol    string             `bson:"protocol" json:"protocol"`       // 请求协议: http/dns/network/ssl/file/headless等
	Vendor      string             `bson:"vendor,omitempty" json:"vendor,omitempty"`   // 厂商(metadata.vendor)
	Product     string             `bson:"product,omitempty" json:"product,omitempty"` // 产品(metadata.product)
	FilePath    string             `bson:"file_path" json:"filePath"`      // 相对文件路径
	Content     string             `bson:"content" json:"content"`         // YAML内容
	Enabled     bool               `bson:"enabled" json:"enabled"`         // 是否启用
	SyncTime    time.Time          `bson:"sync_time" json:"syncTime"`      // 同步时间

	// 漏洞知识库字段
	CvssScore   float64  `bson:"cvss_score,omitempty" json:"cvssScore,omitempty"`     // CVSS评分
	CvssMetrics string   `bson:"cvss_metrics,omitempty" json:"cvssMetrics,omitempty"` // CVSS向量
	CveIds      []string `bson:"cve_ids,omitempty" json:"cveIds,omitempty"`           // CVE编号列表
	CweIds      []string `bson:"cwe_ids,omitempty" json:"cweIds,omitempty"`           // CWE编号列表
	References  []string `bson:"references,omitempty" json:"references,omitempty"`    // 参考链接
	Remediation string   `bson:"remediation,omitempty" json:"remediation,omitempty"`  // 修复建议
}

// NucleiTemplateModel Nuclei模板模型
type NucleiTemplateModel struct {
	coll *mongo.Collection
}

func NewNucleiTemplateModel(db *mongo.Database) *NucleiTemplateModel {
	coll := db.Collection("nuclei_template")
	// 创建索引
	coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "template_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "category", Value: 1}}},
		{Keys: bson.D{{Key: "severity", Value: 1}}},
		{Keys: bson.D{{Key: "protocol", Value: 1}}},
		{Keys: bson.D{{Key: "tags", Value: 1}}},
		{Keys: bson.D{{Key: "name", Value: "text"}, {Key: "template_id", Value: "text"}, {Key: "description", Value: "text"}}},
		// 支持CVSS和CVE查询的索引
		{Keys: bson.D{{Key: "cvss_score", Value: -1}}},
		{Keys: bson.D{{Key: "cve_ids", Value: 1}}},
		{Keys: bson.D{{Key: "product", Value: 1}}},
	})
	// 历史数据迁移：官方模板库已无 Unknown 级别；protocol 缺失时从分类回填
	go migrateNucleiTemplates(coll)
	return &NucleiTemplateModel{coll: coll}
}

// migrateNucleiTemplates 一次性历史数据规范化（幂等，失败仅记录不影响启动）
func migrateNucleiTemplates(coll *mongo.Collection) {
	ctx := context.Background()
	// severity 空/unknown -> info
	if res, err := coll.UpdateMany(ctx,
		bson.M{"severity": bson.M{"$in": []string{"", "unknown"}}},
		bson.M{"$set": bson.M{"severity": "info"}},
	); err == nil && res.ModifiedCount > 0 {
		log.Printf("[NucleiTemplate] migrated severity unknown->info: %d docs", res.ModifiedCount)
	}
	// protocol 缺失 -> 取 category（同步模板的顶级目录即协议）
	if res, err := coll.UpdateMany(ctx,
		bson.M{"$or": []bson.M{{"protocol": bson.M{"$exists": false}}, {"protocol": ""}}},
		mongo.Pipeline{{{Key: "$set", Value: bson.M{"protocol": "$category"}}}},
	); err == nil && res.ModifiedCount > 0 {
		log.Printf("[NucleiTemplate] backfilled protocol from category: %d docs", res.ModifiedCount)
	}
	// 历史数据无 vendor/product 字段 -> 重新解析已存模板内容回填（一次性，新同步数据自带这些字段）
	backfillNucleiMetadata(ctx, coll)
}

// backfillNucleiMetadata 解析存量模板content回填 vendor/product（并以内容为准修正 protocol）
func backfillNucleiMetadata(ctx context.Context, coll *mongo.Collection) {
	filter := bson.M{"product": bson.M{"$exists": false}}
	count, err := coll.CountDocuments(ctx, filter)
	if err != nil || count == 0 {
		return
	}
	log.Printf("[NucleiTemplate] backfilling vendor/product from stored content: %d docs", count)

	opts := options.Find().
		SetProjection(bson.M{"template_id": 1, "content": 1, "category": 1}).
		SetBatchSize(500)
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		log.Printf("[NucleiTemplate] backfill find failed: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var models []mongo.WriteModel
	for cursor.Next(ctx) {
		var doc struct {
			TemplateId string `bson:"template_id"`
			Content    string `bson:"content"`
			Category   string `bson:"category"`
		}
		if err := cursor.Decode(&doc); err != nil || doc.TemplateId == "" {
			continue
		}

		set := bson.M{}
		if info, err := template.ParseTemplateInfo(doc.Content); err == nil && info != nil {
			set["vendor"] = info.GetVendor()
			set["product"] = info.GetProduct()
		} else {
			set["vendor"] = ""
			set["product"] = ""
		}
		if protocol := template.ParseProtocol(doc.Content); protocol != "" {
			set["protocol"] = protocol
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"template_id": doc.TemplateId}).
			SetUpdate(bson.M{"$set": set}))

		if len(models) >= 500 {
			if _, err := coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
				log.Printf("[NucleiTemplate] backfill bulk write failed: %v", err)
			}
			models = models[:0]
		}
	}
	if len(models) > 0 {
		if _, err := coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
			log.Printf("[NucleiTemplate] backfill bulk write failed: %v", err)
		}
	}
	log.Printf("[NucleiTemplate] backfill vendor/product completed: %d docs", count)
}

func (m *NucleiTemplateModel) Upsert(ctx context.Context, doc *NucleiTemplate) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	doc.SyncTime = time.Now()

	filter := bson.M{"template_id": doc.TemplateId}
	// $set 不能包含 _id，否则已存在文档更新会报错（_id 不可变）
	update := bson.M{
		"$set": bson.M{
			"template_id":  doc.TemplateId,
			"name":         doc.Name,
			"author":       doc.Author,
			"severity":     doc.Severity,
			"description":  doc.Description,
			"tags":         doc.Tags,
			"category":     doc.Category,
			"protocol":     doc.Protocol,
			"vendor":       doc.Vendor,
			"product":      doc.Product,
			"file_path":    doc.FilePath,
			"content":      doc.Content,
			"enabled":      doc.Enabled,
			"sync_time":    doc.SyncTime,
			"cvss_score":   doc.CvssScore,
			"cvss_metrics": doc.CvssMetrics,
			"cve_ids":      doc.CveIds,
			"cwe_ids":      doc.CweIds,
			"references":   doc.References,
			"remediation":  doc.Remediation,
		},
		"$setOnInsert": bson.M{"_id": doc.Id},
	}
	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *NucleiTemplateModel) BulkUpsert(ctx context.Context, docs []*NucleiTemplate) error {
	if len(docs) == 0 {
		return nil
	}

	var models []mongo.WriteModel
	now := time.Now()
	for _, doc := range docs {
		if doc.Id.IsZero() {
			doc.Id = primitive.NewObjectID()
		}
		doc.SyncTime = now

		filter := bson.M{"template_id": doc.TemplateId}
		// $set 不能包含 _id，否则已存在文档更新会报错（_id 不可变）
		update := bson.M{
			"$set": bson.M{
				"template_id":  doc.TemplateId,
				"name":         doc.Name,
				"author":       doc.Author,
				"severity":     doc.Severity,
				"description":  doc.Description,
				"tags":         doc.Tags,
				"category":     doc.Category,
				"protocol":     doc.Protocol,
				"vendor":       doc.Vendor,
				"product":      doc.Product,
				"file_path":    doc.FilePath,
				"content":      doc.Content,
				"enabled":      doc.Enabled,
				"sync_time":    doc.SyncTime,
				"cvss_score":   doc.CvssScore,
				"cvss_metrics": doc.CvssMetrics,
				"cve_ids":      doc.CveIds,
				"cwe_ids":      doc.CweIds,
				"references":   doc.References,
				"remediation":  doc.Remediation,
			},
			"$setOnInsert": bson.M{"_id": doc.Id},
		}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true))
	}

	opts := options.BulkWrite().SetOrdered(false)
	_, err := m.coll.BulkWrite(ctx, models, opts)
	return err
}

// SelectAll 全选场景批量查询：无分页上限，仅投影选择所需轻量字段（template_id/name）
func (m *NucleiTemplateModel) SelectAll(ctx context.Context, filter bson.M) ([]NucleiTemplate, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "severity", Value: 1}, {Key: "name", Value: 1}}).
		SetProjection(bson.M{"template_id": 1, "name": 1})
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NucleiTemplate
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *NucleiTemplateModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]NucleiTemplate, error) {
	page, pageSize = NormalizePage(page, pageSize)
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "severity", Value: 1}, {Key: "name", Value: 1}})
	// 排除content字段，提高查询性能
	opts.SetProjection(bson.M{"content": 0})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NucleiTemplate
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *NucleiTemplateModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// FindByTemplateId 根据模板ID获取完整模板（包含content）
func (m *NucleiTemplateModel) FindByTemplateId(ctx context.Context, templateId string) (*NucleiTemplate, error) {
	var doc NucleiTemplate
	err := m.coll.FindOne(ctx, bson.M{"template_id": templateId}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *NucleiTemplateModel) GetCategories(ctx context.Context) ([]string, error) {
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

// FacetItem 分面统计项（筛选值 + 数量）
type FacetItem struct {
	Value string `bson:"_id" json:"value"`
	Count int    `bson:"count" json:"count"`
}

// facetCounts 在 filter 基础上统计指定字段的分面计数（跳过空值，按数量降序）
// field 为标量字符串字段名（severity/protocol/product等）
func facetCounts(ctx context.Context, coll *mongo.Collection, filter bson.M, field string, limit int) ([]FacetItem, error) {
	if filter == nil {
		filter = bson.M{}
	}
	pipeline := []bson.M{
		{"$match": filter},
		{"$match": bson.M{field: bson.M{"$nin": []interface{}{"", nil}}}},
		{"$group": bson.M{"_id": "$" + field, "count": bson.M{"$sum": 1}}},
		{"$sort": bson.M{"count": -1, "_id": 1}},
	}
	if limit > 0 {
		pipeline = append(pipeline, bson.M{"$limit": limit})
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []FacetItem
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// tagFacetCounts 在 filter 基础上统计标签分面计数（$unwind 展开后按标签聚合）
func tagFacetCounts(ctx context.Context, coll *mongo.Collection, filter bson.M, limit int) ([]FacetItem, error) {
	if filter == nil {
		filter = bson.M{}
	}
	pipeline := []bson.M{
		{"$match": filter},
		{"$match": bson.M{"tags.0": bson.M{"$exists": true}}},
		{"$unwind": "$tags"},
		{"$match": bson.M{"tags": bson.M{"$nin": []interface{}{"", nil}}}},
		{"$group": bson.M{"_id": "$tags", "count": bson.M{"$sum": 1}}},
		{"$sort": bson.M{"count": -1, "_id": 1}},
	}
	if limit > 0 {
		pipeline = append(pipeline, bson.M{"$limit": limit})
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []FacetItem
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// cveFacetCounts 在 filter 基础上统计 有CVE / 无CVE 文档数量
func cveFacetCounts(ctx context.Context, coll *mongo.Collection, filter bson.M) (withCve int64, withoutCve int64, err error) {
	countCve := func(exists bool) (int64, error) {
		f := make(bson.M, len(filter)+1)
		for k, v := range filter {
			f[k] = v
		}
		f["cve_ids.0"] = bson.M{"$exists": exists}
		return coll.CountDocuments(ctx, f)
	}
	withCve, err = countCve(true)
	if err != nil {
		return 0, 0, err
	}
	withoutCve, err = countCve(false)
	if err != nil {
		return 0, 0, err
	}
	return withCve, withoutCve, nil
}

// GetFacetCounts 在 filter 基础上统计指定字段的分面计数（跳过空值，按数量降序）
// field 为标量字段名（severity/protocol/product/category）
func (m *NucleiTemplateModel) GetFacetCounts(ctx context.Context, filter bson.M, field string, limit int) ([]FacetItem, error) {
	return facetCounts(ctx, m.coll, filter, field, limit)
}

// GetTagFacetCounts 在 filter 基础上统计标签分面计数（$unwind 展开后按标签聚合）
func (m *NucleiTemplateModel) GetTagFacetCounts(ctx context.Context, filter bson.M, limit int) ([]FacetItem, error) {
	return tagFacetCounts(ctx, m.coll, filter, limit)
}

// GetCveFacetCounts 在 filter 基础上统计 有CVE / 无CVE 模板数量
func (m *NucleiTemplateModel) GetCveFacetCounts(ctx context.Context, filter bson.M) (withCve int64, withoutCve int64, err error) {
	return cveFacetCounts(ctx, m.coll, filter)
}

// GetFacetCounts 自定义POC: 在 filter 基础上统计指定字段的分面计数
func (m *CustomPocModel) GetFacetCounts(ctx context.Context, filter bson.M, field string, limit int) ([]FacetItem, error) {
	return facetCounts(ctx, m.coll, filter, field, limit)
}

// GetTagFacetCounts 自定义POC: 标签分面计数
func (m *CustomPocModel) GetTagFacetCounts(ctx context.Context, filter bson.M, limit int) ([]FacetItem, error) {
	return tagFacetCounts(ctx, m.coll, filter, limit)
}

// GetCveFacetCounts 自定义POC: 有/无CVE 数量统计
func (m *CustomPocModel) GetCveFacetCounts(ctx context.Context, filter bson.M) (withCve int64, withoutCve int64, err error) {
	return cveFacetCounts(ctx, m.coll, filter)
}

func (m *NucleiTemplateModel) GetTags(ctx context.Context, limit int) ([]string, error) {
	return m.GetTagsByFilter(ctx, nil, limit)
}

// GetTagsByFilter 在 filter 基础上获取热门标签（带数量）
func (m *NucleiTemplateModel) GetTagsByFilter(ctx context.Context, filter bson.M, limit int) ([]string, error) {
	results, err := m.GetTagFacetCounts(ctx, filter, limit)
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(results))
	for _, r := range results {
		tags = append(tags, r.Value)
	}
	return tags, nil
}

func (m *NucleiTemplateModel) GetStats(ctx context.Context) (map[string]int, error) {
	stats := make(map[string]int)

	// 使用聚合管道一次性统计所有严重级别
	pipeline := []bson.M{
		{"$group": bson.M{
			"_id":   "$severity",
			"count": bson.M{"$sum": 1},
		}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return stats, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		Id    string `bson:"_id"`
		Count int    `bson:"count"`
	}
	if err = cursor.All(ctx, &results); err != nil {
		return stats, err
	}

	total := 0
	for _, r := range results {
		if r.Id != "" {
			stats[r.Id] = r.Count
			total += r.Count
		}
	}
	stats["total"] = total

	return stats, nil
}

func (m *NucleiTemplateModel) DeleteAll(ctx context.Context) error {
	_, err := m.coll.DeleteMany(ctx, bson.M{})
	return err
}

// FindEnabled 获取启用的模板
func (m *NucleiTemplateModel) FindEnabled(ctx context.Context) ([]NucleiTemplate, error) {
	cursor, err := m.coll.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NucleiTemplate
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindEnabledByFilter 根据条件获取启用的模板
func (m *NucleiTemplateModel) FindEnabledByFilter(ctx context.Context, filter bson.M) ([]NucleiTemplate, error) {
	filter["enabled"] = true
	cursor, err := m.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NucleiTemplate
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindBySeverity 根据严重级别获取启用的模板
func (m *NucleiTemplateModel) FindBySeverity(ctx context.Context, severities []string) ([]NucleiTemplate, error) {
	filter := bson.M{
		"enabled":  true,
		"severity": bson.M{"$in": severities},
	}
	cursor, err := m.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NucleiTemplate
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindByTags 根据标签获取启用的模板
func (m *NucleiTemplateModel) FindByTags(ctx context.Context, tags []string) ([]NucleiTemplate, error) {
	filter := bson.M{
		"enabled": true,
		"tags":    bson.M{"$in": tags},
	}
	cursor, err := m.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NucleiTemplate
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// UpdateEnabled 更新模板启用状态
func (m *NucleiTemplateModel) UpdateEnabled(ctx context.Context, templateId string, enabled bool) error {
	_, err := m.coll.UpdateOne(ctx, bson.M{"template_id": templateId}, bson.M{"$set": bson.M{"enabled": enabled}})
	return err
}

// BatchUpdateEnabled 批量更新模板启用状态
func (m *NucleiTemplateModel) BatchUpdateEnabled(ctx context.Context, templateIds []string, enabled bool) error {
	_, err := m.coll.UpdateMany(ctx, bson.M{"template_id": bson.M{"$in": templateIds}}, bson.M{"$set": bson.M{"enabled": enabled}})
	return err
}

// FindByIds 根据ID列表获取模板（包含content）
func (m *NucleiTemplateModel) FindByIds(ctx context.Context, ids []string) ([]NucleiTemplate, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	cursor, err := m.coll.Find(ctx, bson.M{"template_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []NucleiTemplate
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}
