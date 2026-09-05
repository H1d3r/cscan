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

// 漏洞生命周期状态（T1.3）：status 字段缺失视为 open（待修复）
const (
	VulStatusOpen    = "open"
	VulStatusFixed   = "fixed"
	VulStatusIgnored = "ignored"
)

// 修复确认来源（fix_confirm_source）
const (
	VulFixSourceRescan = "auto:rescan"
	VulFixSourceProbe  = "auto:probe"
	VulFixSourceManual = "manual"
)

// 风险来源（risk_source）：由扫描器产出并经落库层按 Source 归一化写入（T3.3 打通传输链）。
//   - auto:weakpass   弱口令（brutescan 产出）
//   - auto:cert-expiry 证书到期（certcheck 双路产出）
//   - auto:info-leak   敏感信息泄露（jsfinder/dirscan 产出，T3.4 复验）
//   - auto:takeover    子域接管（subdomain_bruteforce 产出）
const (
	VulRiskSourceWeakPass   = "auto:weakpass"
	VulRiskSourceCertExpiry = "auto:cert-expiry"
	VulRiskSourceInfoLeak   = "auto:info-leak"
	VulRiskSourceTakeover   = "auto:takeover"
)

type Vul struct {
	Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Authority  string             `bson:"authority" json:"authority"`
	Host       string             `bson:"host" json:"host"`
	Port       int                `bson:"port" json:"port"`
	Url        string             `bson:"url" json:"url"`
	PocFile    string             `bson:"pocfile" json:"pocFile"`
	Source     string             `bson:"source" json:"source"`
	Severity   string             `bson:"severity" json:"severity"` // 严重级别: critical/high/medium/low/info/unknown
	Extra      string             `bson:"extra" json:"extra"`
	Result     string             `bson:"result" json:"result"`
	TaskId     string             `bson:"task_id" json:"taskId"`
	VulName    string             `bson:"vul_name,omitempty" json:"vulName,omitempty"`
	Tags       []string           `bson:"tags,omitempty" json:"tags,omitempty"`
	CreateTime time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime time.Time          `bson:"update_time" json:"updateTime"`

	// 漏洞知识库关联字段
	CvssScore   float64  `bson:"cvss_score,omitempty" json:"cvssScore,omitempty"`
	CveId       string   `bson:"cve_id,omitempty" json:"cveId,omitempty"`
	CweId       string   `bson:"cwe_id,omitempty" json:"cweId,omitempty"`
	Remediation string   `bson:"remediation,omitempty" json:"remediation,omitempty"`
	References  []string `bson:"references,omitempty" json:"references,omitempty"`

	// 证据链字段
	MatcherName       string   `bson:"matcher_name,omitempty" json:"matcherName,omitempty"`
	ExtractedResults  []string `bson:"extracted_results,omitempty" json:"extractedResults,omitempty"`
	CurlCommand       string   `bson:"curl_command,omitempty" json:"curlCommand,omitempty"`
	Request           string   `bson:"request,omitempty" json:"request,omitempty"`
	Response          string   `bson:"response,omitempty" json:"response,omitempty"`
	ResponseTruncated bool     `bson:"response_truncated,omitempty" json:"responseTruncated,omitempty"`

	// 时间追踪字段
	FirstSeenTime time.Time `bson:"first_seen_time,omitempty" json:"firstSeenTime,omitempty"`
	LastSeenTime  time.Time `bson:"last_seen_time,omitempty" json:"lastSeenTime,omitempty"`
	ScanCount     int       `bson:"scan_count,omitempty" json:"scanCount,omitempty"`

	// 风险层标记：is_risk=true 时该漏洞计入"风险"视角，false 时仅在暴露面视角展示
	// 缺失字段视为 false（暴露面视角默认）；risk 层查询显式 filter is_risk=true
	IsRisk         bool      `bson:"is_risk,omitempty" json:"isRisk,omitempty"`
	RiskAssessedAt time.Time `bson:"risk_assessed_at,omitempty" json:"riskAssessedAt,omitempty"`
	RiskSource     string    `bson:"risk_source,omitempty" json:"riskSource,omitempty"` // manual / auto:cvss / auto:weakpass / auto:info-leak

	// 漏洞生命周期状态机（T1.3）：status 缺失视为 open（待修复）
	Status           string    `bson:"status,omitempty" json:"status,omitempty"`                       // open / fixed / ignored
	FixedAt          time.Time `bson:"fixed_at,omitempty" json:"fixedAt,omitempty"`                    // 标记已修复的时间
	LastVerifiedAt   time.Time `bson:"last_verified_at,omitempty" json:"lastVerifiedAt,omitempty"`     // 最近一次复验确认时间
	FixConfirmSource string    `bson:"fix_confirm_source,omitempty" json:"fixConfirmSource,omitempty"` // auto:rescan / auto:probe / manual

	// 复验待确认标记（T3.3）：复验时目标不可达（连不上）时置位，表示"无法确认是否已修复"，
	// 区别于已确认修复（status=fixed）。不可达不得误判为已修复。
	VerifyPending bool `bson:"verify_pending,omitempty" json:"verifyPending,omitempty"`

	// ==================== 单条漏洞复验（人工触发，worker 执行复测，T-复验闭环） ====================
	// 复验状态机：
	//   ""(空)         = 未复验/空闲
	//   "reverifying"  = 复验任务已下发，worker 复测中
	//   "done"         = 复验完成（结论见 reverify_conclusion）
	ReverifyStatus string `bson:"reverify_status,omitempty" json:"reverifyStatus,omitempty"`
	// 复验结论（worker 复测后写入）：
	//   "fixed"               = 复测未发现漏洞，已修复
	//   "still_vuln"          = 复测仍能复现，漏洞依然存在
	//   "unreachable"         = 目标不可达，无法确认是否修复（verify_pending 置位）
	//   "reachable_untested"  = 目标可达但无可用复测模板，无法确认是否修复
	ReverifyConclusion string    `bson:"reverify_conclusion,omitempty" json:"reverifyConclusion,omitempty"`
	ReverifyMessage    string    `bson:"reverify_message,omitempty" json:"reverifyMessage,omitempty"`
	ReverifyAt         time.Time `bson:"reverify_at,omitempty" json:"reverifyAt,omitempty"`              // 最近一次复验时间（下发或完成）
	ReverifyBy         string    `bson:"reverify_by,omitempty" json:"reverifyBy,omitempty"`              // 复验发起/执行人
	LastReverifyTime   time.Time `bson:"last_reverify_time,omitempty" json:"lastReverifyTime,omitempty"` // 最近一次人工复验时间
}

// 单条漏洞复验状态（reverify_status）常量
const (
	ReverifyStatusIdle        = "" // 未复验
	ReverifyStatusReverifying = "reverifying"
	ReverifyStatusDone        = "done"
)

// 单条漏洞复验结论（reverify_conclusion）常量
const (
	ReverifyConclusionFixed             = "fixed"
	ReverifyConclusionStillVuln         = "still_vuln"
	ReverifyConclusionUnreachable       = "unreachable"
	ReverifyConclusionReachableUntested = "reachable_untested"
)

// VulReverifyResult worker 复测完成后回传的复验结论
type VulReverifyResult struct {
	Conclusion string    // 见 ReverifyConclusion* 常量
	Reviewer   string    // 复验发起/执行人
	Message    string    // 复测说明（如模板名/错误信息）
	ReverifyAt time.Time // 复测完成时间
}

type VulModel struct {
	coll *mongo.Collection
}

func NewVulModel(db *mongo.Database) *VulModel {
	coll := db.Collection("vul")
	// T1.3: 新增 status / risk_source 维度索引，支撑漏洞状态机统计与复验查询（T3.3/T3.4）。
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "severity", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "first_seen_time", Value: -1}}},
		{Keys: bson.D{{Key: "risk_source", Value: 1}, {Key: "status", Value: 1}}},
		// 覆盖 Upsert 过滤键，扫描写入逐条 upsert 不再全表扫描
		{Keys: bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1},
			{Key: "pocfile", Value: 1}, {Key: "url", Value: 1}}},
	}
	if err := ensureIndexes(coll, indexes); err != nil {
		logx.Errorf("[VulModel] ensureIndexes failed for %s: %v", coll.Name(), err)
	}
	return &VulModel{
		coll: coll,
	}
}

func (m *VulModel) Insert(ctx context.Context, doc *Vul) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *VulModel) FindById(ctx context.Context, id string) (*Vul, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc Vul
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *VulModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]Vul, error) {
	page, pageSize = NormalizePage(page, pageSize)
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})
	// 列表场景排除超大字段（请求/响应/curl 命令），列表接口不需要这些证据链数据
	// 详情接口应使用 FindById 获取完整字段
	opts.SetProjection(bson.D{
		{Key: "request", Value: 0},
		{Key: "response", Value: 0},
		{Key: "curl_command", Value: 0},
	})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Vul
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *VulModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// FindBySeveritySort 按"严重度等级降序 + first_seen_time 降序 + create_time 降序"分页查询（T4.3）。
// 严重度为字符串，无法直接排序，故用 $addFields 计算 severity_rank（critical=5..info=1, 其他=0）后排序。
func (m *VulModel) FindBySeveritySort(ctx context.Context, filter bson.M, page, pageSize int) ([]Vul, error) {
	page, pageSize = NormalizePage(page, pageSize)
	severityRankStage := bson.D{
		{Key: "$switch", Value: bson.D{
			{Key: "branches", Value: bson.A{
				bson.D{{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{"$severity", "critical"}}}}, {Key: "then", Value: 5}},
				bson.D{{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{"$severity", "high"}}}}, {Key: "then", Value: 4}},
				bson.D{{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{"$severity", "medium"}}}}, {Key: "then", Value: 3}},
				bson.D{{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{"$severity", "low"}}}}, {Key: "then", Value: 2}},
				bson.D{{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{"$severity", "info"}}}}, {Key: "then", Value: 1}},
			}},
			{Key: "default", Value: 0},
		}},
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$addFields", Value: bson.D{
			{Key: "severity_rank", Value: severityRankStage},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "severity_rank", Value: -1},
			{Key: "first_seen_time", Value: -1},
			{Key: "create_time", Value: -1},
		}}},
	}
	if page > 0 && pageSize > 0 {
		pipeline = append(pipeline,
			bson.D{{Key: "$skip", Value: int64((page - 1) * pageSize)}},
			bson.D{{Key: "$limit", Value: int64(pageSize)}},
		)
	}
	pipeline = append(pipeline, bson.D{{Key: "$project", Value: bson.D{
		{Key: "request", Value: 0},
		{Key: "response", Value: 0},
		{Key: "curl_command", Value: 0},
		{Key: "severity_rank", Value: 0},
	}}})

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Vul
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// EstimatedCount 使用集合元数据快速估算文档总数（O(1)），仅适用于空 filter 场景
func (m *VulModel) EstimatedCount(ctx context.Context) (int64, error) {
	return m.coll.EstimatedDocumentCount(ctx)
}

type VulSeverityStats struct {
	Total    int64 `bson:"total"`
	Critical int64 `bson:"critical"`
	High     int64 `bson:"high"`
	Medium   int64 `bson:"medium"`
	Low      int64 `bson:"low"`
	Info     int64 `bson:"info"`
	Week     int64 `bson:"week"`
	Month    int64 `bson:"month"`
	// T1.3: 生命周期状态计数（缺失 status 视为 open）
	Open    int64 `bson:"open"`
	Fixed   int64 `bson:"fixed"`
	Ignored int64 `bson:"ignored"`
	// 按严重等级拆分的待处理（open）计数，用于安全评分仅计入未修复漏洞
	OpenCritical int64 `bson:"openCritical"`
	OpenHigh     int64 `bson:"openHigh"`
	OpenMedium   int64 `bson:"openMedium"`
	OpenLow      int64 `bson:"openLow"`
	OpenInfo     int64 `bson:"openInfo"`
}

// AggregateStats 聚合统计漏洞总数、严重级别分布以及近7/30天数量
func (m *VulModel) AggregateStats(ctx context.Context, now time.Time) (*VulSeverityStats, error) {
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, 0, -30)

	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "critical", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "critical"}}}, 1, 0}}}}}},
			{Key: "high", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "high"}}}, 1, 0}}}}}},
			{Key: "medium", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "medium"}}}, 1, 0}}}}}},
			{Key: "low", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "low"}}}, 1, 0}}}}}},
			{Key: "info", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "info"}}}, 1, 0}}}}}},
			{Key: "week", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$gte", Value: bson.A{"$create_time", weekAgo}}}, 1, 0}}}}}},
			{Key: "month", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$gte", Value: bson.A{"$create_time", monthAgo}}}, 1, 0}}}}}},
			// T1.3: 状态维度。open 包含缺失 status 的存量数据（缺失视为 open）。
			{Key: "open", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$and", Value: bson.A{bson.D{{Key: "$ne", Value: bson.A{"$status", "fixed"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "ignored"}}}}}}, 1, 0}}}}}},
			{Key: "fixed", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "fixed"}}}, 1, 0}}}}}},
			{Key: "ignored", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "ignored"}}}, 1, 0}}}}}},
			// 按严重等级拆分的待处理计数（status 非 fixed 且非 ignored）
			{Key: "openCritical", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$and", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "critical"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "fixed"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "ignored"}}}}}}, 1, 0}}}}}},
			{Key: "openHigh", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$and", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "high"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "fixed"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "ignored"}}}}}}, 1, 0}}}}}},
			{Key: "openMedium", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$and", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "medium"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "fixed"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "ignored"}}}}}}, 1, 0}}}}}},
			{Key: "openLow", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$and", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "low"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "fixed"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "ignored"}}}}}}, 1, 0}}}}}},
			{Key: "openInfo", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$and", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$severity", "info"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "fixed"}}}, bson.D{{Key: "$ne", Value: bson.A{"$status", "ignored"}}}}}}, 1, 0}}}}}},
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	stats := &VulSeverityStats{}
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

// VulChangeStats 工作台风险变化统计结果
type VulChangeStats struct {
	Open          int64
	NewInWindow   int64
	FixedInWindow int64
	BySeverity    map[string]int64
}

// AggregateChangesStats 统计待处理风险、窗口内新发现与已修复、新增按严重度分布（单次 $facet 聚合）
// 口径：open = 非 fixed 且非 ignored（缺失 status 视为 open）；新发现用 first_seen_time 窗口；
// 已修复用 status=fixed 且 fixed_at 在窗口内
func (m *VulModel) AggregateChangesStats(ctx context.Context, cutoff time.Time) (*VulChangeStats, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$facet", Value: bson.D{
			{Key: "open", Value: bson.A{
				bson.D{{Key: "$match", Value: bson.D{{Key: "status", Value: bson.D{{Key: "$nin", Value: bson.A{"fixed", "ignored"}}}}}}},
				bson.D{{Key: "$count", Value: "c"}},
			}},
			{Key: "newBySev", Value: bson.A{
				bson.D{{Key: "$match", Value: bson.D{{Key: "first_seen_time", Value: bson.D{{Key: "$gte", Value: cutoff}}}}}},
				bson.D{{Key: "$group", Value: bson.D{
					{Key: "_id", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$severity", "unknown"}}}},
					{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
				}}},
			}},
			{Key: "fixed", Value: bson.A{
				bson.D{{Key: "$match", Value: bson.D{
					{Key: "status", Value: "fixed"},
					{Key: "fixed_at", Value: bson.D{{Key: "$gte", Value: cutoff}}},
				}}},
				bson.D{{Key: "$count", Value: "c"}},
			}},
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	result := &VulChangeStats{BySeverity: map[string]int64{}}
	if cursor.Next(ctx) {
		var facet struct {
			Open []struct {
				C int64 `bson:"c"`
			} `bson:"open"`
			NewBySev []struct {
				ID    string `bson:"_id"`
				Count int64  `bson:"count"`
			} `bson:"newBySev"`
			Fixed []struct {
				C int64 `bson:"c"`
			} `bson:"fixed"`
		}
		if err := cursor.Decode(&facet); err != nil {
			return nil, err
		}
		if len(facet.Open) > 0 {
			result.Open = facet.Open[0].C
		}
		for _, s := range facet.NewBySev {
			result.BySeverity[s.ID] = s.Count
			result.NewInWindow += s.Count
		}
		if len(facet.Fixed) > 0 {
			result.FixedInWindow = facet.Fixed[0].C
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// CountByTaskId 根据任务ID统计漏洞数量
func (m *VulModel) CountByTaskId(ctx context.Context, taskId string) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{"task_id": taskId})
}

func (m *VulModel) Delete(ctx context.Context, id string) (int64, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return 0, err
	}
	res, err := m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (m *VulModel) DeleteByTaskId(ctx context.Context, taskId string) error {
	_, err := m.coll.DeleteMany(ctx, bson.M{"task_id": taskId})
	return err
}

// Upsert 插入或更新漏洞（基于 host+port+pocFile+url 去重）。
// 返回 UpdateResult 以便调用方通过 UpsertedCount 判定是否为本次新增（T1.1 diff 写入需要）。
// T1.3: 命中已存在且状态为 fixed 的漏洞时，自动复活为 open（修复后又复现的正确语义）；
// 其余状态（open/ignored）保持不变，不覆盖用户标记。新增漏洞默认 status=open。
func (m *VulModel) Upsert(ctx context.Context, doc *Vul) (*mongo.UpdateResult, error) {
	now := time.Now()
	filter := bson.M{
		"host":    doc.Host,
		"port":    doc.Port,
		"pocfile": doc.PocFile,
		"url":     doc.Url,
	}

	// 预查现有记录，用于修复态复活判断（T1.3）。查询失败不阻塞 upsert 主流程。
	var existing Vul
	hadExisting := false
	if rerr := m.coll.FindOne(ctx, filter).Decode(&existing); rerr == nil {
		hadExisting = true
	} else if rerr != mongo.ErrNoDocuments {
		logx.Errorf("[VulModel] Upsert pre-check failed: %v", rerr)
	}

	setFields := bson.M{
		"authority":   doc.Authority,
		"source":      doc.Source,
		"severity":    doc.Severity,
		"extra":       doc.Extra,
		"result":      doc.Result,
		"task_id":     doc.TaskId,
		"update_time": now,
		// 新增字段 - 漏洞知识库关联
		"cvss_score":  doc.CvssScore,
		"cve_id":      doc.CveId,
		"cwe_id":      doc.CweId,
		"remediation": doc.Remediation,
		"references":  doc.References,
		// 新增字段 - 证据链
		"matcher_name":       doc.MatcherName,
		"extracted_results":  doc.ExtractedResults,
		"curl_command":       doc.CurlCommand,
		"request":            doc.Request,
		"response":           doc.Response,
		"response_truncated": doc.ResponseTruncated,
		// 新增字段 - 漏洞名称和标签
		"vul_name": doc.VulName,
		"tags":     doc.Tags,
		// 新增字段 - 时间追踪
		"last_seen_time": now,
	}

	// T1.3: 修复态复活。固定状态被再次扫到 → 回到 open，并清空修复时间/来源。
	if hadExisting && shouldResurrect(existing.Status) {
		setFields["status"] = VulStatusOpen
		setFields["fixed_at"] = time.Time{} // 清空修复时间（omitempty 下零值即视为未修复）
		setFields["fix_confirm_source"] = ""
	}

	update := bson.M{
		"$set": setFields,
		"$inc": bson.M{
			"scan_count": 1, // 新增：扫描计数
		},
		"$setOnInsert": bson.M{
			"_id":             primitive.NewObjectID(),
			"create_time":     now,
			"first_seen_time": now,           // 新增：首次发现时间
			"status":          VulStatusOpen, // T1.3: 新增漏洞默认 open
		},
	}
	opts := options.Update().SetUpsert(true)
	res, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return res, err
}

// MarkFixed 将指定漏洞标记为已修复（支持批量）。返回实际修改条数。
func (m *VulModel) MarkFixed(ctx context.Context, ids []string, source string) (int64, error) {
	oids := toObjectIDs(ids)
	if len(oids) == 0 {
		return 0, nil
	}
	now := time.Now()
	res, err := m.coll.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": oids}}, bson.M{
		"$set": bson.M{
			"status":             VulStatusFixed,
			"fixed_at":           now,
			"fix_confirm_source": source,
		},
	})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// MarkOpen 将指定漏洞重新打开为待修复（修复后又出现，或人工撤销忽略）。
func (m *VulModel) MarkOpen(ctx context.Context, ids []string, source string) (int64, error) {
	oids := toObjectIDs(ids)
	if len(oids) == 0 {
		return 0, nil
	}
	res, err := m.coll.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": oids}}, bson.M{
		"$set": bson.M{
			"status":             VulStatusOpen,
			"fix_confirm_source": source,
		},
		"$unset": bson.M{"fixed_at": ""},
	})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// MarkIgnored 将指定漏洞标记为已忽略（不再计入待处理）。
func (m *VulModel) MarkIgnored(ctx context.Context, ids []string) (int64, error) {
	oids := toObjectIDs(ids)
	if len(oids) == 0 {
		return 0, nil
	}
	res, err := m.coll.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": oids}}, bson.M{
		"$set": bson.M{"status": VulStatusIgnored},
	})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// MarkReverifyStarted 人工触发单条漏洞复验时调用：将漏洞标记为"复验中"，
// 记录复验发起人与下发时间（T-复验闭环）。worker 完成复测后由 ApplyReverifyResult 收尾。
func (m *VulModel) MarkReverifyStarted(ctx context.Context, id, reviewer string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{
		"$set": bson.M{
			"reverify_status":     ReverifyStatusReverifying,
			"reverify_by":         reviewer,
			"reverify_at":         now,
			"last_reverify_time":  now,
			"reverify_conclusion": "",
			"reverify_message":    "",
			"verify_pending":      false,
		},
	})
	return err
}

// ApplyReverifyResult worker 复测完成后收尾：根据结论更新漏洞状态、复验结论与时间（T-复验闭环）。
//   - fixed:              置为 fixed（fix_confirm_source=auto:rescan），清 verify_pending
//   - still_vuln:         复测仍能复现 → 复活为 open（修复态被再次复现的正确语义），清 verify_pending
//   - unreachable:        目标不可达 → 仅置 verify_pending，不改 status（不误判为已修复）
//   - reachable_untested: 目标可达但无可用复测模板 → 仅更新复验时间，保留原 status
func (m *VulModel) ApplyReverifyResult(ctx context.Context, id string, r *VulReverifyResult) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	now := r.ReverifyAt
	if now.IsZero() {
		now = time.Now()
	}

	setFields := bson.M{
		"reverify_status":     ReverifyStatusDone,
		"reverify_at":         now,
		"reverify_by":         r.Reviewer,
		"reverify_conclusion": r.Conclusion,
		"reverify_message":    r.Message,
		"last_reverify_time":  now,
	}

	switch r.Conclusion {
	case ReverifyConclusionFixed:
		setFields["status"] = VulStatusFixed
		setFields["fixed_at"] = now
		setFields["fix_confirm_source"] = VulFixSourceRescan
		setFields["verify_pending"] = false
	case ReverifyConclusionStillVuln:
		setFields["status"] = VulStatusOpen
		setFields["fixed_at"] = time.Time{} // 清空修复时间（omitempty 下零值即视为未修复）
		setFields["fix_confirm_source"] = ""
		setFields["last_verified_at"] = now
		setFields["last_seen_time"] = now
		setFields["verify_pending"] = false
	case ReverifyConclusionUnreachable:
		// 仅置待确认，不改 status（不可达不得误判为已修复）
		setFields["verify_pending"] = true
	case ReverifyConclusionReachableUntested:
		// 目标可达但无可用复测模板：保留原 status，仅更新复验时间
		setFields["last_verified_at"] = now
		setFields["verify_pending"] = false
	}

	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": setFields})
	return err
}

// CountByStatus 按状态统计漏洞数量（status 为空时返回全部）。
func (m *VulModel) CountByStatus(ctx context.Context, status string) (int64, error) {
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	return m.coll.CountDocuments(ctx, filter)
}

// toObjectIDs 将字符串 ID 列表转换为 ObjectID，跳过非法值。
func toObjectIDs(ids []string) []primitive.ObjectID {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			oids = append(oids, oid)
		}
	}
	return oids
}

// shouldResurrect 判断命中已存在漏洞时是否应复活为 open（T1.3）。
// 仅当原状态为 fixed 时复活；open/ignored 保留用户标记。
func shouldResurrect(existingStatus string) bool {
	return existingStatus == VulStatusFixed
}

// DeleteByFilter 按条件批量删除漏洞
func (m *VulModel) DeleteByFilter(ctx context.Context, filter bson.M) (int64, error) {
	res, err := m.coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// BatchDelete 批量删除漏洞
func (m *VulModel) BatchDelete(ctx context.Context, ids []string) (int64, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			oids = append(oids, oid)
		}
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

// Clear 清空所有漏洞
func (m *VulModel) Clear(ctx context.Context) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// FindByHostPort 根据host和port查找漏洞列表（用于风险评分计算）
func (m *VulModel) FindByHostPort(ctx context.Context, host string, port int) ([]Vul, error) {
	filter := bson.M{
		"host": host,
		"port": port,
	}
	opts := options.Find()
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []Vul
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// StatBySeverityInRange 统计指定时间范围内漏洞按严重级别的分布（周期报告 T5.1）。
// 使用聚合管道 $match + $group，避免全量拉取明细，支撑大区间万级数据快速统计。
func (m *VulModel) StatBySeverityInRange(ctx context.Context, start, end time.Time) (map[string]int64, error) {
	match := bson.M{}
	if !start.IsZero() || !end.IsZero() {
		tr := bson.M{}
		if !start.IsZero() {
			tr["$gte"] = start
		}
		if !end.IsZero() {
			tr["$lt"] = end
		}
		match["create_time"] = tr
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$severity"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}
	cur, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	result := map[string]int64{}
	for cur.Next(ctx) {
		var row struct {
			ID    string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		sev := row.ID
		if sev == "" {
			sev = "unknown"
		}
		result[sev] = row.Count
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
