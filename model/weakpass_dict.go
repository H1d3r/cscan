package model

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// WeakpassDict 弱口令字典 - 使用 "用户名:密码" 格式
type WeakpassDict struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`               // 字典名称
	Description string             `bson:"description" json:"description"` // 描述
	Service     string             `bson:"service" json:"service"`         // 服务类型：ssh/ftp/mysql/.../common
	Content     string             `bson:"content" json:"content"`         // 字典内容（每行一个 "用户名:密码"）
	WordCount   int                `bson:"word_count" json:"wordCount"`    // 词条数量（用户:密码组合数）
	Enabled     bool               `bson:"enabled" json:"enabled"`         // 是否启用
	IsBuiltin   bool               `bson:"is_builtin" json:"isBuiltin"`    // 是否内置字典
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

// WeakpassDictImportResult 导入结果
type WeakpassDictImportResult struct {
	Imported int
	Updated  int
	Skipped  int
	Errors   []string
}

// WeakpassDictParseResult 解析结果
type WeakpassDictParseResult struct {
	TotalLines   int
	ValidLines   int
	EmptyLines   int
	CommentLines int
	Groups       []WeakpassDictGroupResult
}

// WeakpassDictGroupResult 分组解析结果
type WeakpassDictGroupResult struct {
	Service   string
	LineCount int
	Lines     []string
}

// WeakpassEntry 弱口令条目
type WeakpassEntry struct {
	Username string
	Password string
}

// ParseWeakpassLine 解析一行弱口令内容
// 支持格式: 用户名:密码, 用户名: (空密码), :密码 (空用户名)
func ParseWeakpassLine(line string) (entry WeakpassEntry, isValid bool) {
	// 去除首尾空白
	line = strings.TrimSpace(line)

	// 跳过空行
	if line == "" {
		return entry, false
	}

	// 跳过注释行
	if strings.HasPrefix(line, "#") {
		return entry, false
	}

	// 按冒号分割，最多分割1次（密码中可能包含冒号）
	parts := strings.SplitN(line, ":", 2)

	if len(parts) == 2 {
		entry.Username = strings.TrimSpace(parts[0])
		entry.Password = strings.TrimSpace(parts[1])
	} else if len(parts) == 1 {
		// 只有冒号前的内容，没有密码
		entry.Username = strings.TrimSpace(parts[0])
		entry.Password = ""
	} else {
		return entry, false
	}

	// 用户名和密码都为空，跳过
	if entry.Username == "" && entry.Password == "" {
		return entry, false
	}

	isValid = true
	return entry, true
}

// ParseWeakpassDict 解析弱口令字典内容
func ParseWeakpassDict(content string) []WeakpassEntry {
	entries := make([]WeakpassEntry, 0)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		entry, isValid := ParseWeakpassLine(line)
		if isValid {
			entries = append(entries, entry)
		}
	}

	return entries
}

// ParseGroupedWeakpassDict 解析分组格式的弱口令字典
// 格式: [service]\nuser:pass\nuser:pass\n[service2]\nuser:pass
func ParseGroupedWeakpassDict(content string) map[string][]WeakpassEntry {
	groups := make(map[string][]WeakpassEntry)
	currentService := "common" // 默认服务类型
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 空行跳过（但仍然重置为空密码状态）
		if trimmed == "" {
			continue
		}

		// 检查是否是服务分组标记
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			currentService = strings.ToLower(strings.Trim(trimmed, "[]"))
			// 确保分组存在
			if _, ok := groups[currentService]; !ok {
				groups[currentService] = make([]WeakpassEntry, 0)
			}
			continue
		}

		// 解析条目
		entry, isValid := ParseWeakpassLine(trimmed)
		if isValid {
			groups[currentService] = append(groups[currentService], entry)
		}
	}

	return groups
}

// SplitUserPasswordDicts 分离用户名和密码字典
// 返回: 用户名列表, 密码列表
func SplitUserPasswordDicts(content string) ([]string, []string) {
	entries := ParseWeakpassDict(content)
	usernameSet := make(map[string]struct{})
	passwordSet := make(map[string]struct{})

	for _, entry := range entries {
		if entry.Username != "" {
			usernameSet[entry.Username] = struct{}{}
		}
		if entry.Password != "" {
			passwordSet[entry.Password] = struct{}{}
		}
	}

	usernames := make([]string, 0, len(usernameSet))
	for u := range usernameSet {
		usernames = append(usernames, u)
	}

	passwords := make([]string, 0, len(passwordSet))
	for p := range passwordSet {
		passwords = append(passwords, p)
	}

	return usernames, passwords
}

// WeakpassDictModel 弱口令字典模型
type WeakpassDictModel struct {
	coll *mongo.Collection
}

func NewWeakpassDictModel(db *mongo.Database) *WeakpassDictModel {
	return &WeakpassDictModel{
		coll: db.Collection("weakpass_dict"),
	}
}

func (m *WeakpassDictModel) Insert(ctx context.Context, doc *WeakpassDict) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *WeakpassDictModel) FindAll(ctx context.Context, page, pageSize int, service, name string) ([]WeakpassDict, error) {
	filter := bson.M{}
	if service != "" {
		filter["service"] = service
	}
	if name != "" {
		filter["name"] = bson.M{"$regex": name, "$options": "i"}
	}

	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "is_builtin", Value: -1}, {Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []WeakpassDict
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *WeakpassDictModel) Count(ctx context.Context, service string) (int64, error) {
	filter := bson.M{}
	if service != "" {
		filter["service"] = service
	}
	return m.coll.CountDocuments(ctx, filter)
}

func (m *WeakpassDictModel) FindEnabled(ctx context.Context, service string) ([]WeakpassDict, error) {
	filter := bson.M{"enabled": true}
	if service != "" {
		filter["service"] = service
	}

	opts := options.Find().SetSort(bson.D{{Key: "is_builtin", Value: -1}, {Key: "name", Value: 1}})
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []WeakpassDict
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *WeakpassDictModel) FindById(ctx context.Context, id string) (*WeakpassDict, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc WeakpassDict
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	return &doc, err
}

func (m *WeakpassDictModel) FindByIds(ctx context.Context, ids []string) ([]WeakpassDict, error) {
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

	var docs []WeakpassDict
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *WeakpassDictModel) FindByName(ctx context.Context, name string) (*WeakpassDict, error) {
	var doc WeakpassDict
	err := m.coll.FindOne(ctx, bson.M{"name": name}).Decode(&doc)
	return &doc, err
}

func (m *WeakpassDictModel) Update(ctx context.Context, id string, doc *WeakpassDict) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"name":        doc.Name,
		"description": doc.Description,
		"service":     doc.Service,
		"content":     doc.Content,
		"word_count":  doc.WordCount,
		"enabled":     doc.Enabled,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *WeakpassDictModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	// 不允许删除内置字典
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid, "is_builtin": bson.M{"$ne": true}})
	return err
}

// DeleteNonBuiltin 删除所有非内置字典
func (m *WeakpassDictModel) DeleteNonBuiltin(ctx context.Context) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{"is_builtin": bson.M{"$ne": true}})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// UpsertByName 根据名称更新或插入字典
func (m *WeakpassDictModel) UpsertByName(ctx context.Context, doc *WeakpassDict) error {
	now := time.Now()
	filter := bson.M{"name": doc.Name}
	update := bson.M{
		"$set": bson.M{
			"description": doc.Description,
			"service":     doc.Service,
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

// InitBuiltinDicts 初始化内置字典 - 从文件导入
// 字典文件格式: [service]\nuser:pass\nuser:pass\n[service2]\nuser:pass
func (m *WeakpassDictModel) InitBuiltinDicts(ctx context.Context) error {
	// 确定字典文件路径
	dictFile := "/app/poc/custom-weakpass/default-weakpass.txt"

	// 尝试多个可能的位置
	if _, err := os.Stat(dictFile); os.IsNotExist(err) {
		dictFile = "poc/custom-weakpass/default-weakpass.txt"
	}
	if _, err := os.Stat(dictFile); os.IsNotExist(err) {
		dictFile = "../poc/custom-weakpass/default-weakpass.txt"
	}
	if _, err := os.Stat(dictFile); os.IsNotExist(err) {
		dictFile = "../../poc/custom-weakpass/default-weakpass.txt"
	}
	if _, err := os.Stat(dictFile); os.IsNotExist(err) {
		logx.Errorf("[WeakpassDict] Weakpass dict file not found at: %s", dictFile)
		return fmt.Errorf("weakpass dict file not found")
	}

	return m.initFromFile(ctx, dictFile)
}

// initFromFile 从文件导入内置字典
func (m *WeakpassDictModel) initFromFile(ctx context.Context, filePath string) error {
	logx.Infof("[WeakpassDict] Loading builtin dicts from: %s", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		logx.Errorf("[WeakpassDict] Failed to read dict file: %v", err)
		return fmt.Errorf("failed to read dict file: %w", err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		logx.Info("[WeakpassDict] Dict file is empty")
		return nil
	}

	// 解析分组格式的字典
	groups := ParseGroupedWeakpassDict(content)

	if len(groups) == 0 {
		logx.Info("[WeakpassDict] No valid entries found in dict file")
		return nil
	}

	logx.Infof("[WeakpassDict] Parsed %d service groups from file", len(groups))

	// 按服务类型分组导入
	totalImported := 0
	totalSkipped := 0

	for service, entries := range groups {
		if len(entries) == 0 {
			continue
		}

		// 生成字典名称
		dictName := getServiceDictName(service)

		// 将条目转回内容格式
		var dictContent strings.Builder
		for _, entry := range entries {
			dictContent.WriteString(entry.Username)
			dictContent.WriteString(":")
			dictContent.WriteString(entry.Password)
			dictContent.WriteString("\n")
		}

		dict := &WeakpassDict{
			Name:        dictName,
			Description: getServiceDictDesc(service),
			Service:     service,
			Content:     dictContent.String(),
			WordCount:   len(entries),
			Enabled:     true,
			IsBuiltin:   true,
		}

		// 检查是否已存在
		existing, _ := m.FindByName(ctx, dictName)
		if existing != nil && existing.IsBuiltin {
			// 已存在且是内置字典，更新内容但不改变启用状态
			dict.Id = existing.Id
			dict.Enabled = existing.Enabled
			dict.CreateTime = existing.CreateTime
			if err := m.Update(ctx, dict.Id.Hex(), dict); err != nil {
				logx.Errorf("[WeakpassDict] Failed to update dict %s: %v", dictName, err)
				totalSkipped++
			} else {
				totalImported++
			}
		} else {
			// 新建字典
			if err := m.UpsertByName(ctx, dict); err != nil {
				logx.Errorf("[WeakpassDict] Failed to upsert dict %s: %v", dictName, err)
				totalSkipped++
			} else {
				totalImported++
			}
		}
	}

	logx.Infof("[WeakpassDict] Builtin dicts initialized: %d imported/updated, %d skipped", totalImported, totalSkipped)
	return nil
}

// getServiceDictName 根据服务类型获取字典名称
func getServiceDictName(service string) string {
	serviceNames := map[string]string{
		"ssh":           "SSH-弱口令",
		"mysql":         "MySQL-弱口令",
		"redis":         "Redis-弱口令",
		"mongodb":       "MongoDB-弱口令",
		"postgresql":    "PostgreSQL-弱口令",
		"mssql":         "MSSQL-弱口令",
		"ftp":           "FTP-弱口令",
		"oracle":        "Oracle-弱口令",
		"smb":           "SMB-弱口令",
		"mqtt":          "MQTT-弱口令",
		"rdp":           "RDP-弱口令",
		"telnet":        "Telnet-弱口令",
		"vnc":           "VNC-弱口令",
		"rsync":         "Rsync-弱口令",
		"docker":        "DockerRegistry-弱口令",
		"ldap":          "LDAP-弱口令",
		"memcached":     "Memcached-弱口令",
		"elasticsearch": "Elasticsearch-弱口令",
		"kibana":        "Kibana-弱口令",
		"common":        "通用-弱口令",
	}

	if name, ok := serviceNames[service]; ok {
		return name
	}
	return service + "-弱口令"
}

// getServiceDictDesc 根据服务类型获取字典描述
func getServiceDictDesc(service string) string {
	descriptions := map[string]string{
		"ssh":           "SSH服务弱口令字典",
		"mysql":         "MySQL服务弱口令字典，支持无密码认证",
		"redis":         "Redis服务弱口令字典，支持无密码认证",
		"mongodb":       "MongoDB服务弱口令字典，支持无密码认证",
		"postgresql":    "PostgreSQL服务弱口令字典",
		"mssql":         "MSSQL服务弱口令字典，支持无密码认证",
		"ftp":           "FTP服务弱口令字典",
		"oracle":        "Oracle服务弱口令字典",
		"smb":           "SMB服务弱口令字典，支持无密码认证",
		"mqtt":          "MQTT服务弱口令字典，支持匿名认证",
		"rdp":           "RDP服务弱口令字典",
		"telnet":        "Telnet服务弱口令字典",
		"vnc":           "VNC服务弱口令字典（用户名留空，仅密码）",
		"rsync":         "Rsync服务弱口令字典",
		"docker":        "Docker Registry服务弱口令字典",
		"ldap":          "LDAP服务弱口令字典",
		"memcached":     "Memcached服务弱口令字典",
		"elasticsearch": "Elasticsearch服务弱口令字典",
		"kibana":        "Kibana服务弱口令字典",
		"common":        "通用弱口令列表，包含常见用户名和密码组合",
	}

	if desc, ok := descriptions[service]; ok {
		return desc
	}
	return service + "服务弱口令字典"
}
