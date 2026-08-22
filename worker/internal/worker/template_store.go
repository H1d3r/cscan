package worker

// TemplateStore：Worker 本地模板库（Nuclei 默认模板 + 自定义 POC）。
//
// 目标：把"每次扫描从 Mongo 拉取模板内容再写临时文件"变为"一次性同步到本地内容寻址
// 文件库，扫描时按 id/标签解析并在扫描目录建硬链接"，消除重复的内容传输与文件写入。
//
// 设计要点：
//   - 库文件按内容 sha256 前 16 位命名：天然去重、同步幂等（内容不变不重写）；
//   - 内存索引 id→(hash, enabled, severity, tags)，支持按 ID 与按标签两种解析，
//     解析语义与 config_loader.loadTemplates 的 Mongo 查询保持一致；
//   - 指纹 = 两集合的 总数/启用数/最大更新时间（nuclei 用 sync_time，自定义POC 用
//     update_time），内容变更、增删、启停均会改变指纹；任务前校验指纹，变化才重同步；
//   - 索引持久化为 index.json：进程重启且指纹未变时直接复用本地库，不再拉取内容；
//   - 清理只删除"当前索引未引用的孤儿文件"；硬链接语义（POSIX/NTFS）保证删除库文件
//     不影响已链接进运行中扫描目录的文件。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	templateStoreIndexFile   = "index.json"
	templateStoreCheckPeriod = 30 * time.Second // 指纹校验最小间隔，避免任务密集时反复 count
)

// TemplateEntry 索引项：模板 ID → 内容哈希与筛选元数据
type TemplateEntry struct {
	Hash     string   `json:"hash"`     // 内容 sha256 hex（文件名 = hash[:16] + ".yaml"）
	Enabled  bool     `json:"enabled"`  // 是否启用（与 Mongo enabled 同步）
	Severity string   `json:"severity"` // 严重级别（tags 解析时过滤）
	Tags     []string `json:"tags"`     // 标签（tags 解析时过滤）
}

// TemplateFingerprint 集合指纹：任一字段变化即触发重同步
type TemplateFingerprint struct {
	NucleiTotal     int64     `json:"nucleiTotal"`
	NucleiEnabled   int64     `json:"nucleiEnabled"`
	NucleiMaxSync   time.Time `json:"nucleiMaxSync"`
	CustomTotal     int64     `json:"customTotal"`
	CustomEnabled   int64     `json:"customEnabled"`
	CustomMaxUpdate time.Time `json:"customMaxUpdate"`
}

// TemplateStore 本地模板库
type TemplateStore struct {
	baseDir string
	logger  Logger

	mu             sync.RWMutex
	entries        map[string]*TemplateEntry // key: "n:"+templateId / "c:"+objectIdHex
	fp             TemplateFingerprint
	synced         bool // 已完成同步且指纹与库内容一致
	loadedFromDisk bool // 启动时从 index.json 加载，待指纹校验通过后转 synced
	lastCheck      time.Time
}

// NewTemplateStore 创建本地模板库；若磁盘上存在 index.json 则先行加载，
// 待首次 EnsureSynced 指纹校验通过后即视为可用
func NewTemplateStore(baseDir string, logger Logger) *TemplateStore {
	s := &TemplateStore{
		baseDir: baseDir,
		logger:  logger,
		entries: make(map[string]*TemplateEntry),
	}
	s.loadIndexFromDisk()
	return s
}

// Ready 库是否已同步（未同步时 tags/全量解析不可用，ID 解析自动回退）
func (s *TemplateStore) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.synced
}

// EnsureSynced 指纹校验，变化时全量重同步；带节流避免密集任务下反复查询计数
func (s *TemplateStore) EnsureSynced(ctx context.Context, db *mongo.Database) error {
	if db == nil {
		return fmt.Errorf("mongo db unavailable")
	}

	s.mu.RLock()
	synced, last := s.synced, s.lastCheck
	s.mu.RUnlock()
	if synced && time.Since(last) < templateStoreCheckPeriod {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.synced && time.Since(s.lastCheck) < templateStoreCheckPeriod {
		return nil
	}

	fp, err := fetchTemplateFingerprint(ctx, db)
	if err != nil {
		return fmt.Errorf("fingerprint: %w", err)
	}
	s.lastCheck = time.Now()
	// 磁盘加载的索引经指纹校验有效（库内容未变化），直接转为可用，免全量拉取
	if (s.synced || s.loadedFromDisk) && s.fp.equal(fp) {
		s.synced, s.loadedFromDisk = true, false
		return nil
	}
	if err := s.sync(ctx, db, fp); err != nil {
		return err
	}
	s.fp, s.synced, s.loadedFromDisk = fp, true, false
	return nil
}

// equal 指纹比较。不直接用 ==：time.Time 的 == 受 loc 指针影响，
// 同一时刻经不同解码路径得到的 time.Time 可能不相等
func (a TemplateFingerprint) equal(b TemplateFingerprint) bool {
	return a.NucleiTotal == b.NucleiTotal && a.NucleiEnabled == b.NucleiEnabled &&
		a.NucleiMaxSync.Unix() == b.NucleiMaxSync.Unix() &&
		a.CustomTotal == b.CustomTotal && a.CustomEnabled == b.CustomEnabled &&
		a.CustomMaxUpdate.Unix() == b.CustomMaxUpdate.Unix()
}

// fetchTemplateFingerprint 用便宜的计数/最值查询计算集合指纹
func fetchTemplateFingerprint(ctx context.Context, db *mongo.Database) (TemplateFingerprint, error) {
	var fp TemplateFingerprint
	nuclei := db.Collection("nuclei_template")
	custom := db.Collection("custom_poc")

	var err error
	if fp.NucleiTotal, err = nuclei.CountDocuments(ctx, bson.M{}); err != nil {
		return fp, err
	}
	if fp.NucleiEnabled, err = nuclei.CountDocuments(ctx, bson.M{"enabled": true}); err != nil {
		return fp, err
	}
	if fp.CustomTotal, err = custom.CountDocuments(ctx, bson.M{}); err != nil {
		return fp, err
	}
	if fp.CustomEnabled, err = custom.CountDocuments(ctx, bson.M{"enabled": true}); err != nil {
		return fp, err
	}

	var maxDoc struct {
		SyncTime time.Time `bson:"sync_time"`
	}
	if err := nuclei.FindOne(ctx, bson.M{},
		options.FindOne().SetSort(bson.D{{Key: "sync_time", Value: -1}}).SetProjection(bson.M{"sync_time": 1}),
	).Decode(&maxDoc); err == nil {
		fp.NucleiMaxSync = maxDoc.SyncTime
	} else if err != mongo.ErrNoDocuments {
		return fp, err
	}

	var maxCustom struct {
		UpdateTime time.Time `bson:"update_time"`
	}
	if err := custom.FindOne(ctx, bson.M{},
		options.FindOne().SetSort(bson.D{{Key: "update_time", Value: -1}}).SetProjection(bson.M{"update_time": 1}),
	).Decode(&maxCustom); err == nil {
		fp.CustomMaxUpdate = maxCustom.UpdateTime
	} else if err != mongo.ErrNoDocuments {
		return fp, err
	}
	return fp, nil
}

// sync 全量同步：拉取两集合全部文档内容，内容寻址写盘，重建索引并清理孤儿文件。
// 调用方需持有 s.mu 写锁。
func (s *TemplateStore) sync(ctx context.Context, db *mongo.Database, fp TemplateFingerprint) error {
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("mkdir store: %w", err)
	}

	newEntries := make(map[string]*TemplateEntry)
	written := 0

	// Nuclei 默认模板
	type nucleiDoc struct {
		TemplateId string   `bson:"template_id"`
		Severity   string   `bson:"severity"`
		Tags       []string `bson:"tags"`
		Enabled    bool     `bson:"enabled"`
		Content    string   `bson:"content"`
	}
	nCursor, err := db.Collection("nuclei_template").Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{
			"template_id": 1, "severity": 1, "tags": 1, "enabled": 1, "content": 1,
		}).SetBatchSize(500))
	if err != nil {
		return fmt.Errorf("query nuclei templates: %w", err)
	}
	defer nCursor.Close(ctx)
	for nCursor.Next(ctx) {
		var doc nucleiDoc
		if err := nCursor.Decode(&doc); err != nil || doc.TemplateId == "" || doc.Content == "" {
			continue
		}
		ok, err := s.ensureFile(doc.Content, &written)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		newEntries["n:"+doc.TemplateId] = &TemplateEntry{
			Hash: hashOfContent(doc.Content), Enabled: doc.Enabled,
			Severity: doc.Severity, Tags: doc.Tags,
		}
	}
	if err := nCursor.Err(); err != nil {
		return fmt.Errorf("iterate nuclei templates: %w", err)
	}

	// 自定义 POC
	type customDoc struct {
		Id       interface{} `bson:"_id"`
		Severity string      `bson:"severity"`
		Tags     []string    `bson:"tags"`
		Enabled  bool        `bson:"enabled"`
		Content  string      `bson:"content"`
	}
	cCursor, err := db.Collection("custom_poc").Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{
			"severity": 1, "tags": 1, "enabled": 1, "content": 1,
		}).SetBatchSize(500))
	if err != nil {
		return fmt.Errorf("query custom pocs: %w", err)
	}
	defer cCursor.Close(ctx)
	for cCursor.Next(ctx) {
		var doc customDoc
		if err := cCursor.Decode(&doc); err != nil || doc.Content == "" || doc.Id == nil {
			continue
		}
		ok, err := s.ensureFile(doc.Content, &written)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		newEntries["c:"+objectIdHex(doc.Id)] = &TemplateEntry{
			Hash: hashOfContent(doc.Content), Enabled: doc.Enabled,
			Severity: doc.Severity, Tags: doc.Tags,
		}
	}
	if err := cCursor.Err(); err != nil {
		return fmt.Errorf("iterate custom pocs: %w", err)
	}

	orphans := s.cleanupOrphans(newEntries)
	s.entries = newEntries
	s.persistIndex(fp)

	if s.logger != nil {
		s.logger.Info("TemplateStore synced: %d entries (%d files written, %d orphans removed), dir=%s",
			len(newEntries), written, orphans, s.baseDir)
	}
	return nil
}

// ensureFile 内容寻址写盘：文件已存在（内容未变）则跳过；返回 false 表示内容为空
func (s *TemplateStore) ensureFile(content string, written *int) (bool, error) {
	if content == "" {
		return false, nil
	}
	path := filepath.Join(s.baseDir, fileNameOfHash(hashOfContent(content)))
	if _, err := os.Stat(path); err == nil {
		return true, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("write store file: %w", err)
	}
	*written++
	return true, nil
}

// cleanupOrphans 删除新索引未引用的库文件；硬链接语义保证不影响运行中的扫描
func (s *TemplateStore) cleanupOrphans(entries map[string]*TemplateEntry) int {
	referenced := make(map[string]bool, len(entries))
	for _, e := range entries {
		referenced[fileNameOfHash(e.Hash)] = true
	}
	dirents, err := os.ReadDir(s.baseDir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, d := range dirents {
		name := d.Name()
		if d.IsDir() || name == templateStoreIndexFile || filepath.Ext(name) != ".yaml" {
			continue
		}
		if !referenced[name] {
			if os.Remove(filepath.Join(s.baseDir, name)) == nil {
				removed++
			}
		}
	}
	return removed
}

// persistIndex 索引落盘（临时文件 + 原子改名），供重启后免拉取复用
func (s *TemplateStore) persistIndex(fp TemplateFingerprint) {
	data, err := json.Marshal(struct {
		Fingerprint TemplateFingerprint        `json:"fingerprint"`
		Entries     map[string]*TemplateEntry  `json:"entries"`
	}{Fingerprint: fp, Entries: s.entries})
	if err != nil {
		return
	}
	tmp := filepath.Join(s.baseDir, templateStoreIndexFile+".tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(s.baseDir, templateStoreIndexFile))
}

// loadIndexFromDisk 启动时加载磁盘索引；指纹校验通过后才标记 synced
func (s *TemplateStore) loadIndexFromDisk() {
	data, err := os.ReadFile(filepath.Join(s.baseDir, templateStoreIndexFile))
	if err != nil {
		return
	}
	var idx struct {
		Fingerprint TemplateFingerprint       `json:"fingerprint"`
		Entries     map[string]*TemplateEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &idx); err != nil || len(idx.Entries) == 0 {
		return
	}
	s.fp = idx.Fingerprint
	s.entries = idx.Entries
	s.loadedFromDisk = true
	if s.logger != nil {
		s.logger.Info("TemplateStore loaded index from disk: %d entries, dir=%s", len(idx.Entries), s.baseDir)
	}
}

// MaterializeIDs 按 ID 解析启用模板的库文件路径。
// 索引未命中的 ID 通过返回值交还调用方回退 Mongo 内容加载（禁用项静默跳过，
// 与 loadTemplates 的 enabled 过滤语义一致）。
func (s *TemplateStore) MaterializeIDs(nucleiIds, customIds []string) (paths, missedNucleiIds, missedCustomIds []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, id := range nucleiIds {
		if e, ok := s.entries["n:"+id]; ok {
			if !e.Enabled {
				continue
			}
			if p := s.entryPath(e); p != "" {
				paths = append(paths, p)
				continue
			}
		}
		missedNucleiIds = append(missedNucleiIds, id)
	}
	for _, id := range customIds {
		if e, ok := s.entries["c:"+id]; ok {
			if !e.Enabled {
				continue
			}
			if p := s.entryPath(e); p != "" {
				paths = append(paths, p)
				continue
			}
		}
		missedCustomIds = append(missedCustomIds, id)
	}
	return paths, missedNucleiIds, missedCustomIds
}

// MaterializeByTags 按标签/严重级别解析（等价 loadTemplates tags 分支：
// nuclei 模板与自定义 POC 都按 enabled + tags 交集 + severity 过滤）。
// ok=false 表示库未同步或不可用，调用方需回退 Mongo。
func (s *TemplateStore) MaterializeByTags(tags, severities []string) (paths []string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.synced {
		return nil, false
	}

	sevSet := make(map[string]bool, len(severities))
	for _, s := range severities {
		sevSet[s] = true
	}
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[t] = true
	}

	for _, e := range s.entries {
		if !e.Enabled {
			continue
		}
		if len(sevSet) > 0 && !sevSet[e.Severity] {
			continue
		}
		if len(tagSet) > 0 && !hasAnyTag(e.Tags, tagSet) {
			continue
		}
		if p := s.entryPath(e); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, true
}

// MaterializeCustomPocs 全部启用自定义 POC（按严重级别过滤），等价 getAllCustomPocs。
// ok=false 表示库未同步或不可用，调用方需回退 Mongo。
func (s *TemplateStore) MaterializeCustomPocs(severities []string) (paths []string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.synced {
		return nil, false
	}

	sevSet := make(map[string]bool, len(severities))
	for _, s := range severities {
		sevSet[s] = true
	}
	for key, e := range s.entries {
		if len(key) < 2 || key[:2] != "c:" || !e.Enabled {
			continue
		}
		if len(sevSet) > 0 && !sevSet[e.Severity] {
			continue
		}
		if p := s.entryPath(e); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, true
}

// entryPath 返回库文件绝对路径；文件缺失（异常状态）返回空串由调用方按未命中处理。
// 读锁内 Stat 是安全的：孤儿清理持写锁执行，不会并发删除。
func (s *TemplateStore) entryPath(e *TemplateEntry) string {
	if len(e.Hash) < 16 {
		return ""
	}
	p := filepath.Join(s.baseDir, fileNameOfHash(e.Hash))
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func hasAnyTag(tags []string, tagSet map[string]bool) bool {
	for _, t := range tags {
		if tagSet[t] {
			return true
		}
	}
	return false
}

func hashOfContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func fileNameOfHash(hash string) string {
	return hash[:16] + ".yaml"
}

// objectIdHex 兼容 _id 的 ObjectID 与字符串两种形态
func objectIdHex(v interface{}) string {
	type hexer interface{ Hex() string }
	if o, ok := v.(hexer); ok {
		return o.Hex()
	}
	return fmt.Sprintf("%v", v)
}

// ==================== Worker 集成：库优先解析 + Mongo 内容回退 ====================

// ensureTemplateStore POC 扫描前的库同步检查（指纹节流，通常只做 4 次便宜的 count 查询）；
// 失败仅告警不阻断，后续解析自动回退 Mongo 内容加载
func (w *Worker) ensureTemplateStore(ctx context.Context) {
	if w.mongoDB == nil || w.templateStore == nil {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := w.templateStore.EnsureSynced(syncCtx, w.mongoDB); err != nil {
		w.logger.Warn("TemplateStore sync check failed: %v, POC scan falls back to Mongo content loading", err)
	}
}

// resolveTemplatesByIds 手动选择模式：优先本地库按 ID 解析库文件，
// 索引未命中的 ID 回退 Mongo 内容加载（禁用项在两侧语义一致地跳过）
func (w *Worker) resolveTemplatesByIds(ctx context.Context, nucleiIds, customIds []string) (contents []string, refs []string) {
	if w.templateStore != nil {
		var missedN, missedC []string
		refs, missedN, missedC = w.templateStore.MaterializeIDs(nucleiIds, customIds)
		if len(missedN) > 0 || len(missedC) > 0 {
			contents = w.getTemplatesByIds(ctx, missedN, missedC)
		}
		return contents, refs
	}
	return w.getTemplatesByIds(ctx, nucleiIds, customIds), nil
}

// resolveTemplatesByTags 自动分组模式：优先本地库按标签/严重级别解析
//（nuclei 模板 + 自定义 POC 同时命中，语义同 loadTemplates tags 分支），库不可用回退 Mongo
func (w *Worker) resolveTemplatesByTags(ctx context.Context, tags, severities []string) (contents []string, refs []string) {
	if w.templateStore != nil {
		if paths, ok := w.templateStore.MaterializeByTags(tags, severities); ok {
			return nil, paths
		}
	}
	return w.getTemplatesByTags(ctx, tags, severities), nil
}

// resolveAllCustomPocs 仅自定义POC模式：优先本地库解析全部启用自定义POC，库不可用回退 Mongo
func (w *Worker) resolveAllCustomPocs(ctx context.Context, severities []string) (contents []string, refs []string) {
	if w.templateStore != nil {
		if paths, ok := w.templateStore.MaterializeCustomPocs(severities); ok {
			return nil, paths
		}
	}
	return w.getAllCustomPocs(ctx, severities), nil
}
