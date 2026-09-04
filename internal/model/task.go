package model

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cscan/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 任务状态常量
const (
	TaskStatusCreated = "CREATED" // 已创建，等待手动启动
	TaskStatusPending = "PENDING" // 等待执行（已入队）
	TaskStatusStarted = "STARTED" // 执行中
	TaskStatusPaused  = "PAUSED"  // 已暂停
	TaskStatusSuccess = "SUCCESS" // 执行成功
	TaskStatusPartial = "PARTIAL" // 部分完成（存在覆盖缺口但保留了可用结果）
	TaskStatusFailure = "FAILURE" // 执行失败
	TaskStatusRevoked = "REVOKED" // 已取消
	TaskStatusStopped = "STOPPED" // 已停止
)

type TaskPhaseCoverage struct {
	Input       int `bson:"input" json:"input"`
	Attempted   int `bson:"attempted" json:"attempted"`
	Succeeded   int `bson:"succeeded" json:"succeeded"`
	TimedOut    int `bson:"timed_out" json:"timedOut"`
	Failed      int `bson:"failed" json:"failed"`
	Skipped     int `bson:"skipped" json:"skipped"`
	Uncovered   int `bson:"uncovered" json:"uncovered"`
	Unconfirmed int `bson:"unconfirmed" json:"unconfirmed"`
}

// TaskPhaseSummary is one idempotently reported sub-task phase conclusion.
// ReportKey is stable for (mainTaskId, subTaskId, phase) and is used as the
// map key in TaskScanSummary.Phases; optional fields preserve old documents.
type TaskPhaseSummary struct {
	SubTaskId               string            `bson:"sub_task_id,omitempty" json:"subTaskId,omitempty"`
	Phase                   string            `bson:"phase" json:"phase"`
	Status                  string            `bson:"status" json:"status"`
	Coverage                TaskPhaseCoverage `bson:"coverage,omitempty" json:"coverage,omitempty"`
	ReasonCodes             []string          `bson:"reason_codes,omitempty" json:"reasonCodes,omitempty"`
	UsableResults           bool              `bson:"usable_results,omitempty" json:"usableResults,omitempty"`
	Assets                  int               `bson:"assets,omitempty" json:"assets,omitempty"`
	Vulnerabilities         int               `bson:"vulnerabilities,omitempty" json:"vulnerabilities,omitempty"`
	VulnerabilityConclusion string            `bson:"vulnerability_conclusion,omitempty" json:"vulnerabilityConclusion,omitempty"`
	ResultPrefix            string            `bson:"result_prefix,omitempty" json:"-"`
	Weight                  int               `bson:"weight,omitempty" json:"weight,omitempty"`
}

// TaskScanSummary is optional so historical task documents require no migration.
type TaskScanSummary struct {
	Outcome                 string                      `bson:"outcome,omitempty" json:"outcome,omitempty"`
	Complete                bool                        `bson:"complete,omitempty" json:"complete,omitempty"`
	VulnerabilityConclusion string                      `bson:"vulnerability_conclusion,omitempty" json:"vulnerabilityConclusion,omitempty"`
	Phases                  map[string]TaskPhaseSummary `bson:"phases,omitempty" json:"phases,omitempty"`
	PhaseCount              int                         `bson:"phase_count,omitempty" json:"-"`
	Assets                  int                         `bson:"assets,omitempty" json:"assets,omitempty"`
	Vulnerabilities         int                         `bson:"vulnerabilities,omitempty" json:"vulnerabilities,omitempty"`
	WarningCodes            []string                    `bson:"warning_codes,omitempty" json:"warningCodes,omitempty"`
}

const (
	TaskPhaseComplete             = "COMPLETE"
	TaskPhasePartial              = "PARTIAL"
	TaskPhaseUncovered            = "UNCOVERED"
	TaskPhaseFailed               = "FAILED"
	TaskPhaseSkippedNotApplicable = "SKIPPED_NOT_APPLICABLE"
	TaskPhaseCanceled             = "CANCELED"

	VulnerabilityConclusionNoFindings         = "NO_FINDINGS"
	VulnerabilityConclusionFindings           = "FINDINGS"
	VulnerabilityConclusionNotEvaluated       = "NOT_EVALUATED"
	VulnerabilityConclusionPartiallyEvaluated = "PARTIALLY_EVALUATED"

	// TaskStatusLegacyCompleted is retained for documents written by the old
	// worker/API path. It is terminal even though it is not emitted anymore.
	TaskStatusLegacyCompleted = "COMPLETED"
)

// IsTerminalTaskStatus reports whether a task must reject late phase reports.
// PAUSED is included because the pause path persists a resumable snapshot and
// must not be mutated by an in-flight scan callback.
func IsTerminalTaskStatus(status string) bool {
	switch status {
	case TaskStatusSuccess, TaskStatusPartial, TaskStatusFailure,
		TaskStatusStopped, TaskStatusRevoked, TaskStatusPaused,
		TaskStatusLegacyCompleted:
		return true
	default:
		return false
	}
}

func terminalTaskStatuses() bson.A {
	return bson.A{
		TaskStatusSuccess, TaskStatusPartial, TaskStatusFailure,
		TaskStatusStopped, TaskStatusRevoked, TaskStatusPaused,
		TaskStatusLegacyCompleted,
	}
}

// AggregateTaskScanSummary is the pure final-outcome function. expectedReports
// includes synthetic sub-task completion reports, which count for completeness
// but are ignored when evaluating scan-phase quality.
func AggregateTaskScanSummary(currentStatus string, expectedReports int, phases map[string]TaskPhaseSummary) TaskScanSummary {
	reported := 0
	effectiveReported := 0
	weightedCompletion := false
	for _, phase := range phases {
		weight := phase.Weight
		if weight <= 0 {
			weight = 1
		}
		reported += weight
		effectiveWeight := weight
		if (phase.Phase == "complete" || phase.Phase == "subtask_complete") && weight > 1 {
			// Extra weight on a synthetic completion report is only counter
			// compensation; it cannot prove that the omitted phase identities
			// completed. Keep the raw count for the progress gate, but evaluate
			// semantic coverage as one completion report.
			effectiveWeight = 1
			weightedCompletion = true
		}
		effectiveReported += effectiveWeight
	}
	summary := TaskScanSummary{Phases: cloneTaskPhases(phases), PhaseCount: reported}
	if currentStatus == TaskStatusStopped || currentStatus == TaskStatusRevoked {
		summary.Outcome = TaskStatusStopped
		return summary
	}
	if currentStatus == TaskStatusPaused {
		summary.Outcome = TaskStatusPaused
		return summary
	}

	missing := expectedReports > effectiveReported
	degraded := missing || weightedCompletion
	failedWithoutResult := false
	if weightedCompletion {
		summary.WarningCodes = append(summary.WarningCodes, "weighted_completion_compensation")
	}
	pocUncovered := false
	anyEvaluatedPOC := false
	hasFinalTotals := false
	for _, phase := range phases {
		if phase.Phase == "complete" || phase.Phase == "subtask_complete" {
			hasFinalTotals = true
			summary.Assets += phase.Assets
			summary.Vulnerabilities += phase.Vulnerabilities
		}
	}

	for _, phase := range phases {
		if phase.Phase == "complete" || phase.Phase == "subtask_complete" {
			continue
		}
		if !hasFinalTotals {
			if phase.Assets > summary.Assets {
				summary.Assets = phase.Assets
			}
			if phase.Vulnerabilities > summary.Vulnerabilities {
				summary.Vulnerabilities = phase.Vulnerabilities
			}
		}
		usable := phase.UsableResults || phase.Assets > 0 || phase.Vulnerabilities > 0 || phase.Coverage.Succeeded > 0
		switch phase.Status {
		case TaskPhaseComplete, TaskPhaseSkippedNotApplicable:
		case TaskPhaseFailed:
			degraded = true
			if !usable {
				failedWithoutResult = true
			}
		case TaskPhasePartial, TaskPhaseUncovered, TaskPhaseCanceled:
			degraded = true
		default:
			degraded = true
			summary.WarningCodes = append(summary.WarningCodes, "unknown_phase_status")
		}
		if phase.Phase == "poc" || phase.Phase == "pocscan" {
			if phase.Status == TaskPhaseUncovered {
				pocUncovered = true
			}
			if phase.Status == TaskPhaseComplete || phase.Status == TaskPhasePartial {
				anyEvaluatedPOC = phase.Coverage.Succeeded > 0 || phase.VulnerabilityConclusion == VulnerabilityConclusionNoFindings || phase.Vulnerabilities > 0
			}
		}
		summary.WarningCodes = append(summary.WarningCodes, phase.ReasonCodes...)
	}

	if missing {
		summary.WarningCodes = append(summary.WarningCodes, "summary_missing")
	}
	if pocUncovered {
		summary.WarningCodes = append(summary.WarningCodes, "poc_uncovered")
	}
	if summary.Vulnerabilities > 0 {
		summary.VulnerabilityConclusion = VulnerabilityConclusionFindings
	} else if pocUncovered && anyEvaluatedPOC {
		summary.VulnerabilityConclusion = VulnerabilityConclusionPartiallyEvaluated
	} else if pocUncovered || missing {
		summary.VulnerabilityConclusion = VulnerabilityConclusionNotEvaluated
	} else {
		summary.VulnerabilityConclusion = VulnerabilityConclusionNoFindings
	}

	switch {
	case failedWithoutResult:
		summary.Outcome = TaskStatusFailure
	case degraded || pocUncovered:
		summary.Outcome = TaskStatusPartial
	default:
		summary.Outcome = TaskStatusSuccess
		summary.Complete = true
	}
	summary.WarningCodes = uniqueSortedStrings(summary.WarningCodes)
	return summary
}

func cloneTaskPhases(phases map[string]TaskPhaseSummary) map[string]TaskPhaseSummary {
	if len(phases) == 0 {
		return nil
	}
	result := make(map[string]TaskPhaseSummary, len(phases))
	for key, phase := range phases {
		phase.ReasonCodes = append([]string(nil), phase.ReasonCodes...)
		result[key] = phase
	}
	return result
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// AppendCoverageHint keeps the legacy Assets/Vuls/Duration prefix untouched.
func AppendCoverageHint(result string, summary TaskScanSummary) string {
	if !strings.HasPrefix(result, "Assets:") {
		result = fmt.Sprintf("Assets:%d Vuls:%d Duration:0s", summary.Assets, summary.Vulnerabilities)
	}
	if summary.Outcome == TaskStatusSuccess || summary.Outcome == "" {
		return result
	}
	incomplete := make([]string, 0)
	for _, phase := range summary.Phases {
		if phase.Phase == "complete" || phase.Phase == "subtask_complete" {
			continue
		}
		if phase.Status != TaskPhaseComplete && phase.Status != TaskPhaseSkippedNotApplicable {
			incomplete = append(incomplete, phase.Phase)
		}
	}
	incomplete = uniqueSortedStrings(incomplete)
	hint := strings.ToLower(summary.Outcome)
	if len(incomplete) > 0 {
		hint += ":" + strings.Join(incomplete, ",")
	}
	return result + " Coverage:" + hint
}

type MainTask struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TaskId      string             `bson:"task_id" json:"taskId"`
	Name        string             `bson:"name" json:"name"`
	Target      string             `bson:"target" json:"target"`
	ProfileId   string             `bson:"profile_id" json:"profileId"`
	ProfileName string             `bson:"profile_name" json:"profileName"`
	OrgId       string             `bson:"org_id,omitempty" json:"orgId"`
	Tags        []string           `bson:"tags,omitempty" json:"tags"` // 任务标签
	Status      string             `bson:"status" json:"status"`
	Progress    int                `bson:"progress" json:"progress"`
	Result      string             `bson:"result" json:"result"`
	IsCron      bool               `bson:"is_cron" json:"isCron"`
	CronRule    string             `bson:"cron_rule" json:"cronRule"`
	CronStatus  string             `bson:"cron_status" json:"cronStatus"`
	NotifyId    string             `bson:"notify_id" json:"notifyId"`
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
	StartTime   *time.Time         `bson:"start_time" json:"startTime"`
	EndTime     *time.Time         `bson:"end_time" json:"endTime"`
	CreatedBy   string             `bson:"created_by,omitempty" json:"createdBy,omitempty"` // 任务创建者用户ID
	// 任务进度保存（用于暂停/继续）
	TaskState    string `bson:"task_state" json:"taskState"`       // 任务执行状态JSON（保存已完成的阶段和数据）
	Config       string `bson:"config" json:"config"`              // 任务配置JSON
	CurrentPhase string `bson:"current_phase" json:"currentPhase"` // 当前执行阶段
	// 子任务拆分（用于分布式并发）
	SubTaskCount int              `bson:"sub_task_count" json:"subTaskCount"` // 子任务总数（目标数 × 模块数，用于进度计算）
	SubTaskDone  int              `bson:"sub_task_done" json:"subTaskDone"`   // 已完成子任务数
	BatchCount   int              `bson:"batch_count" json:"batchCount"`      // 批次数（用于暂停/停止信号分发）
	ScanSummary  *TaskScanSummary `bson:"scan_summary,omitempty" json:"scanSummary,omitempty"`
}

type ExecutorTask struct {
	Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TaskId     string             `bson:"task_id" json:"taskId"`
	MainTaskId string             `bson:"main_task_id" json:"mainTaskId"`
	TaskName   string             `bson:"task_name" json:"taskName"`
	Config     string             `bson:"config" json:"config"`
	Status     string             `bson:"status" json:"status"`
	Worker     string             `bson:"worker" json:"worker"`
	Result     string             `bson:"result" json:"result"`
	CreateTime time.Time          `bson:"create_time" json:"createTime"`
	StartTime  *time.Time         `bson:"start_time" json:"startTime"`
	EndTime    *time.Time         `bson:"end_time" json:"endTime"`
}

type TaskProfile struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Description string             `bson:"description" json:"description"`
	Config      string             `bson:"config" json:"config"`
	SortNumber  int                `bson:"sort_number" json:"sortNumber"`
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

type MainTaskModel struct {
	coll *mongo.Collection
}

func (m *MainTaskModel) Collection() *mongo.Collection {
	return m.coll
}

func NewMainTaskModel(db *mongo.Database) *MainTaskModel {
	coll := db.Collection("maintask")

	// 创建索引
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "task_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
		{Keys: bson.D{{Key: "tags", Value: 1}}},
		// update_time 索引：assetgroupslogic 等按 update_time 排序查询任务列表，避免 in-memory sort 报错
		{Keys: bson.D{{Key: "update_time", Value: -1}}},
		// 复合索引：status + update_time（按状态筛选并按更新时间排序）
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "update_time", Value: -1}}},
	}
	if err := ensureIndexes(coll, indexes); err != nil {
		logx.Errorf("[MainTaskModel] create indexes failed for %s: %v", coll.Name(), err)
	}

	return &MainTaskModel{
		coll: coll,
	}
}

func (m *MainTaskModel) Insert(ctx context.Context, doc *MainTask) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	doc.Status = TaskStatusCreated
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *MainTaskModel) FindById(ctx context.Context, id string) (*MainTask, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc MainTask
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *MainTaskModel) FindByTaskId(ctx context.Context, taskId string) (*MainTask, error) {
	var doc MainTask
	err := m.coll.FindOne(ctx, bson.M{"task_id": taskId}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *MainTaskModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]MainTask, error) {
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

	var docs []MainTask
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindAllWithSort 查询所有匹配的任务，支持自定义排序
// 排除 task_state/config 大字段（仅在任务详情/恢复时按需查询）
func (m *MainTaskModel) FindAllWithSort(ctx context.Context, filter bson.M, sort bson.D) ([]MainTask, error) {
	opts := options.Find()
	opts.SetSort(sort)
	opts.SetProjection(bson.D{
		{Key: "task_state", Value: 0},
		{Key: "config", Value: 0},
	})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []MainTask
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindRecent 返回最近 N 条任务（按 update_time 降序），仅投影分组状态推断所需的最小字段集。
// 用于 assetgroupslogic 推断每个域名的最新任务状态，避免全表扫描。
func (m *MainTaskModel) FindRecent(ctx context.Context, limit int) ([]MainTask, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "update_time", Value: -1}}).
		SetProjection(bson.D{
			{Key: "task_state", Value: 0},
			{Key: "config", Value: 0},
			{Key: "result", Value: 0},
			{Key: "sub_task_count", Value: 0},
			{Key: "sub_task_done", Value: 0},
			{Key: "batch_count", Value: 0},
			{Key: "current_phase", Value: 0},
			{Key: "cron_rule", Value: 0},
			{Key: "cron_status", Value: 0},
		})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := m.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []MainTask
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *MainTaskModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

// EstimatedCount 使用集合元数据快速估算文档总数（O(1)），仅适用于空 filter 场景
func (m *MainTaskModel) EstimatedCount(ctx context.Context) (int64, error) {
	return m.coll.EstimatedDocumentCount(ctx)
}

func (m *MainTaskModel) Update(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	if err == nil {
		m.syncScanStatusIfStatusChange(ctx, id, update)
	}
	return err
}

// mapTaskStatusToScanStatus 主任务状态 → 资产空间搜索的目标扫描状态
// （与 assettargetlistlogic 懒同步的 mapTaskStatusToScan 保持同一口径）
func mapTaskStatusToScanStatus(status string) string {
	switch status {
	case TaskStatusCreated, TaskStatusPending, TaskStatusPaused:
		return "pending"
	case TaskStatusStarted:
		return "in_progress"
	case TaskStatusSuccess, TaskStatusPartial:
		return "completed"
	case TaskStatusFailure:
		return "failed"
	case TaskStatusRevoked, TaskStatusStopped:
		return "cancelled"
	default:
		return ""
	}
}

// syncScanStatusIfStatusChange update 中含状态字段时，把目标扫描状态同步到 asset_target_meta。
// FindById 失败（如 ctx 超时）静默跳过，退回列表懒同步路径。
func (m *MainTaskModel) syncScanStatusIfStatusChange(ctx context.Context, id string, update bson.M) {
	status, ok := update["status"].(string)
	if !ok {
		return
	}
	scanStatus := mapTaskStatusToScanStatus(status)
	if scanStatus == "" {
		return
	}
	task, err := m.FindById(ctx, id)
	if err != nil || task == nil {
		return
	}
	m.syncScanStatusToTargets(ctx, task.Target, scanStatus)
}

// syncScanStatusToTargets 任务状态流转时第一时间把扫描状态写入 asset_target_meta，
// 使资产空间搜索的「扫描状态」与任务管理实时同步。
// API 与 Worker 两条写入路径共用 MainTaskModel，在模型层挂钩即可全覆盖。
// 同步失败仅记日志，不影响任务状态写入本身。
func (m *MainTaskModel) syncScanStatusToTargets(ctx context.Context, target, scanStatus string) {
	if target == "" || scanStatus == "" {
		return
	}
	n := NewAssetTargetMetaModel(m.coll.Database()).RegisterScanTargets(ctx, utils.SplitTargetTokens(target), scanStatus)
	if n > 0 {
		logx.WithContext(ctx).Infof("[MainTaskModel] synced scan_status=%s to %d targets", scanStatus, n)
	}
}

// UpdateWithResult 更新任务并返回结果
func (m *MainTaskModel) UpdateWithResult(ctx context.Context, id string, update bson.M) (*mongo.UpdateResult, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	update["update_time"] = time.Now()
	result, err := m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	if err == nil {
		m.syncScanStatusIfStatusChange(ctx, id, update)
	}
	return result, err
}

func (m *MainTaskModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

func (m *MainTaskModel) BatchDelete(ctx context.Context, ids []string) (int64, error) {
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

// UpdateByTaskId 根据 taskId 更新任务
func (m *MainTaskModel) UpdateByTaskId(ctx context.Context, taskId string, update bson.M) error {
	update["update_time"] = time.Now()
	_, err := m.coll.UpdateOne(ctx, bson.M{"task_id": taskId}, bson.M{"$set": update})
	return err
}

// IncrSubTaskDone 递增已完成子任务数 (已废弃，请使用 IncrSubTaskDoneAtomic)
func (m *MainTaskModel) IncrSubTaskDone(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{
		"$inc": bson.M{"sub_task_done": 1},
		"$set": bson.M{"update_time": time.Now()},
	})
	return err
}

// IncrSubTaskDoneAtomic 原子递增子任务完成数
// 使用 FindOneAndUpdate 实现原子操作，防止并发导致计数超过上限
// 返回更新后的文档，如果已达上限则返回当前文档但不递增
// 返回值: (更新后的任务, 是否实际递增了, 错误)
func (m *MainTaskModel) IncrSubTaskDoneAtomic(ctx context.Context, id string, incrAmount int) (*MainTask, bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, false, err
	}

	if incrAmount <= 0 {
		incrAmount = 1
	}

	now := time.Now()

	// 使用聚合管道执行 FindOneAndUpdate：把 sub_task_done 钳制到 [当前值, sub_task_count]。
	// 关键修正：旧实现仅过滤 done < count 再 $inc incrAmount，当 incrAmount > (count - done)
	// 时（如黑名单一次性补齐 expectedIncr - incrSent）会让 done 一次越过 count，
	// 产生 done > count 的倒挂（实测出现「3/2」）。这里用 $min(done+incr, count) 确保递增后 done <= count，
	// 既补齐到上限，又绝不越界。filter 的 done < count 仍保留以契合「未到上限才动」语义。
	filter := bson.M{
		"_id": oid,
		"$expr": bson.M{
			"$lt": bson.A{"$sub_task_done", "$sub_task_count"},
		},
	}

	// 聚合管道更新：sub_task_done = min(当前done + incrAmount, sub_task_count)
	pipeline := []bson.M{
		{
			"$set": bson.M{
				"sub_task_done": bson.M{
					"$min": bson.A{
						bson.M{"$add": bson.A{"$sub_task_done", incrAmount}},
						"$sub_task_count",
					},
				},
				"update_time": now,
			},
		},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, filter, pipeline, opts).Decode(&task)

	if err == mongo.ErrNoDocuments {
		// 没有匹配的文档，可能是已达上限或文档不存在
		// 尝试获取当前文档状态
		currentTask, findErr := m.FindById(ctx, id)
		if findErr != nil {
			return nil, false, findErr
		}
		// 返回当前文档，但标记为未递增
		return currentTask, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	return &task, true, nil
}

// TaskPhaseReportKey returns a Mongo-safe stable key for one sub-task phase.
func TaskPhaseReportKey(subTaskID, phase string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(subTaskID + "\x00" + phase))
}

// RecordPhaseSummaryAtomic stores one report by stable key and advances progress
// only on the first report. A retry overwrites the same summary without adding
// to sub_task_done or phase_count again.
func (m *MainTaskModel) RecordPhaseSummaryAtomic(ctx context.Context, id, reportKey string, phase TaskPhaseSummary, incrAmount int) (*MainTask, bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, false, err
	}
	if incrAmount <= 0 {
		incrAmount = 1
	}
	phasePath := "scan_summary.phases." + reportKey
	now := time.Now()
	terminalStatuses := terminalTaskStatuses()
	filter := bson.M{
		"_id":     oid,
		phasePath: bson.M{"$exists": false},
		"status":  bson.M{"$nin": terminalStatuses},
	}
	pipeline := []bson.M{{"$set": bson.M{
		phasePath:                  phase,
		"scan_summary.phase_count": bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$scan_summary.phase_count", 0}}, incrAmount}},
		"sub_task_done": bson.M{"$min": bson.A{
			bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$sub_task_done", 0}}, incrAmount}},
			"$sub_task_count",
		}},
		"update_time": now,
	}}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var task MainTask
	err = m.coll.FindOneAndUpdate(ctx, filter, pipeline, opts).Decode(&task)
	if err == nil {
		return &task, true, nil
	}
	if err != mongo.ErrNoDocuments {
		return nil, false, err
	}

	// Duplicate retry: refresh the value with $set, but never increment progress.
	// Once a task is terminal, ignore late retries so the persisted phase map and
	// the already-published aggregate outcome cannot diverge.
	current, err := m.FindById(ctx, id)
	if err != nil || current == nil {
		return current, false, err
	}
	if IsTerminalTaskStatus(current.Status) {
		return current, false, nil
	}
	if _, err = m.coll.UpdateOne(ctx, bson.M{
		"_id":    oid,
		"status": bson.M{"$nin": terminalStatuses},
	}, bson.M{"$set": bson.M{phasePath: phase, "update_time": now}}); err != nil {
		return nil, false, err
	}
	current, err = m.FindById(ctx, id)
	return current, false, err
}

// FinalizeFromScanSummary computes the outcome from persisted phase reports and
// atomically claims the terminal transition. Its boolean return is the sole
// notification gate, so concurrent finalizers emit at most one notification.
func (m *MainTaskModel) FinalizeFromScanSummary(ctx context.Context, id string) (*TaskScanSummary, bool, error) {
	task, err := m.FindById(ctx, id)
	if err != nil || task == nil {
		return nil, false, err
	}
	if task.Status == TaskStatusStopped || task.Status == TaskStatusRevoked || task.Status == TaskStatusPaused {
		summary := AggregateTaskScanSummary(task.Status, task.SubTaskCount, taskPhaseMap(task.ScanSummary))
		return &summary, false, nil
	}
	phases := taskPhaseMap(task.ScanSummary)
	summary := AggregateTaskScanSummary(task.Status, task.SubTaskCount, phases)
	if task.SubTaskDone < task.SubTaskCount || summary.PhaseCount < task.SubTaskCount {
		return &summary, false, nil
	}

	resultPrefix := task.Result
	for _, phase := range phases {
		if strings.HasPrefix(phase.ResultPrefix, "Assets:") {
			resultPrefix = phase.ResultPrefix
			break
		}
	}
	resultText := AppendCoverageHint(resultPrefix, summary)
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, false, err
	}
	now := time.Now()
	filter := bson.M{
		"_id":    oid,
		"status": bson.M{"$nin": terminalTaskStatuses()},
		"$expr": bson.M{"$and": bson.A{
			bson.M{"$gte": bson.A{"$sub_task_done", "$sub_task_count"}},
			bson.M{"$gte": bson.A{"$scan_summary.phase_count", "$sub_task_count"}},
		}},
	}
	update := bson.M{"$set": bson.M{
		"status": summary.Outcome, "progress": 100, "result": resultText,
		"scan_summary": summary, "end_time": now, "update_time": now,
	}}
	res, err := m.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, false, err
	}
	if res.ModifiedCount > 0 {
		scanStatus := "completed"
		if summary.Outcome == TaskStatusFailure {
			scanStatus = "failed"
		}
		m.syncScanStatusToTargets(ctx, task.Target, scanStatus)
	}
	return &summary, res.ModifiedCount > 0, nil
}

func taskPhaseMap(summary *TaskScanSummary) map[string]TaskPhaseSummary {
	if summary == nil {
		return nil
	}
	return summary.Phases
}

// MarkTaskCompleted atomically marks a task successful. Deprecated for scan
// tasks: semantic finalization must use FinalizeFromScanSummary.
func (m *MainTaskModel) MarkTaskCompleted(ctx context.Context, id string) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, err
	}

	now := time.Now()

	// 条件：sub_task_done >= sub_task_count 且状态不是 SUCCESS
	filter := bson.M{
		"_id": oid,
		"$expr": bson.M{
			"$gte": bson.A{"$sub_task_done", "$sub_task_count"},
		},
		"status": bson.M{"$ne": TaskStatusSuccess},
	}

	update := bson.M{
		"$set": bson.M{
			"status":      TaskStatusSuccess,
			"progress":    100,
			"end_time":    now,
			"update_time": now,
		},
	}

	result, err := m.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}

	if result.ModifiedCount > 0 {
		if task, ferr := m.FindById(ctx, id); ferr == nil && task != nil {
			m.syncScanStatusToTargets(ctx, task.Target, "completed")
		}
	}

	return result.ModifiedCount > 0, nil
}

// ExecutorTaskModel
type ExecutorTaskModel struct {
	coll *mongo.Collection
}

func NewExecutorTaskModel(db *mongo.Database) *ExecutorTaskModel {
	return &ExecutorTaskModel{
		coll: db.Collection("executor_task"),
	}
}

func (m *ExecutorTaskModel) Insert(ctx context.Context, doc *ExecutorTask) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	doc.CreateTime = time.Now()
	doc.Status = TaskStatusPending
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

// UpdateByTaskId 按 task_id 更新执行任务字段（Worker 直连 MongoDB 写回状态/结果）
func (m *ExecutorTaskModel) UpdateByTaskId(ctx context.Context, taskId string, fields map[string]interface{}) error {
	if taskId == "" {
		return errors.New("executor task id cannot be empty")
	}
	if len(fields) == 0 {
		return nil
	}

	set := bson.M{}
	for k, v := range fields {
		set[k] = v
	}
	if _, ok := set["end_time"]; !ok {
		if status, ok := set["status"].(string); ok && (status == TaskStatusSuccess || status == TaskStatusPartial || status == TaskStatusFailure) {
			set["end_time"] = time.Now()
		}
	}

	_, err := m.coll.UpdateOne(ctx, bson.M{"task_id": taskId}, bson.M{"$set": set})
	return err
}

func (m *ExecutorTaskModel) FindByMainTaskId(ctx context.Context, mainTaskId string, page, pageSize int) ([]ExecutorTask, error) {
	page, pageSize = NormalizePage(page, pageSize)
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, bson.M{"main_task_id": mainTaskId}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []ExecutorTask
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// DeleteByMainTaskId deletes all executor tasks for a given main task
func (m *ExecutorTaskModel) DeleteByMainTaskId(ctx context.Context, mainTaskId string) (int64, error) {
	result, err := m.coll.DeleteMany(ctx, bson.M{"main_task_id": mainTaskId})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// TaskProfileModel
type TaskProfileModel struct {
	coll *mongo.Collection
}

func NewTaskProfileModel(db *mongo.Database) *TaskProfileModel {
	return &TaskProfileModel{
		coll: db.Collection("task_profile"),
	}
}

func (m *TaskProfileModel) FindAll(ctx context.Context) ([]TaskProfile, error) {
	opts := options.Find().SetSort(bson.D{{Key: "sort_number", Value: 1}})
	cursor, err := m.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []TaskProfile
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *TaskProfileModel) FindById(ctx context.Context, id string) (*TaskProfile, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var doc TaskProfile
	err = m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *TaskProfileModel) Insert(ctx context.Context, doc *TaskProfile) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *TaskProfileModel) Update(ctx context.Context, id string, doc *TaskProfile) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"name":        doc.Name,
		"description": doc.Description,
		"config":      doc.Config,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *TaskProfileModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}
