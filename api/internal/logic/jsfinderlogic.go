package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// jsfinderListCacheTTL 列表结果缓存时长，平衡刷新延迟与重复扫描开销
	jsfinderListCacheTTL = 30 * time.Second
)

// jsfinderListProjection 列表查询投影：排除大字段，
// request/response/curl_command/result/extracted_results 等大字段剥离后单行内存占用大幅下降，
// 跨 ws 合并 + 内存分页可移除硬上限，从而修复"深页取空"问题；大字段由 /jsfinder/detail 按需加载。
var jsfinderListProjection = bson.M{
	"request":           0,
	"response":          0,
	"curl_command":      0,
	"result":            0,
	"extracted_results": 0,
}

// JSFinderConfigLogic JSFinder 配置逻辑
type JSFinderConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewJSFinderConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *JSFinderConfigLogic {
	return &JSFinderConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Get 获取 JSFinder 配置（不存在则返回内置默认值）
func (l *JSFinderConfigLogic) Get() (*types.JSFinderConfigResp, error) {
	m := model.NewJSFinderConfigModel(l.svcCtx.MongoDB)
	doc, err := m.Get(l.ctx)
	if err != nil {
		l.Errorf("[JSFinderConfig] Get error: %v", err)
		return &types.JSFinderConfigResp{Code: 500, Msg: "获取JSFinder配置失败"}, nil
	}

	updateTime := ""
	if !doc.UpdateTime.IsZero() {
		updateTime = doc.UpdateTime.Format("2006-01-02 15:04:05")
	}

	return &types.JSFinderConfigResp{
		Code: 0,
		Msg:  "success",
		Data: &types.JSFinderConfig{
			HighRiskRoutes:       doc.HighRiskRoutes,
			AuthRequiredKeywords: doc.AuthRequiredKeywords,
			SensitiveKeywords:    doc.SensitiveKeywords,
			DomainBlacklist:      doc.DomainBlacklist,
			UpdateTime:           updateTime,
		},
	}, nil
}

// Save 保存 JSFinder 配置
func (l *JSFinderConfigLogic) Save(req *types.JSFinderConfigSaveReq) (*types.JSFinderConfigResp, error) {
	m := model.NewJSFinderConfigModel(l.svcCtx.MongoDB)

	doc := &model.JSFinderConfig{
		HighRiskRoutes:       sanitizeJSFinderList(req.HighRiskRoutes),
		AuthRequiredKeywords: sanitizeJSFinderList(req.AuthRequiredKeywords),
		SensitiveKeywords:    sanitizeJSFinderList(req.SensitiveKeywords),
		DomainBlacklist:      sanitizeJSFinderList(req.DomainBlacklist),
		UpdateTime:           time.Now(),
	}

	if err := m.Save(l.ctx, doc); err != nil {
		l.Errorf("[JSFinderConfig] Save error: %v", err)
		return &types.JSFinderConfigResp{Code: 500, Msg: "保存JSFinder配置失败"}, nil
	}

	return &types.JSFinderConfigResp{
		Code: 0,
		Msg:  "保存成功",
		Data: &types.JSFinderConfig{
			HighRiskRoutes:       doc.HighRiskRoutes,
			AuthRequiredKeywords: doc.AuthRequiredKeywords,
			SensitiveKeywords:    doc.SensitiveKeywords,
			DomainBlacklist:      doc.DomainBlacklist,
			UpdateTime:           doc.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	}, nil
}

// Reset 重置为内置默认值
func (l *JSFinderConfigLogic) Reset() (*types.JSFinderConfigResp, error) {
	m := model.NewJSFinderConfigModel(l.svcCtx.MongoDB)

	def := model.NewDefaultJSFinderConfig()
	if err := m.Save(l.ctx, def); err != nil {
		l.Errorf("[JSFinderConfig] Reset error: %v", err)
		return &types.JSFinderConfigResp{Code: 500, Msg: "重置JSFinder配置失败"}, nil
	}

	return &types.JSFinderConfigResp{
		Code: 0,
		Msg:  "重置成功",
		Data: &types.JSFinderConfig{
			HighRiskRoutes:       def.HighRiskRoutes,
			AuthRequiredKeywords: def.AuthRequiredKeywords,
			SensitiveKeywords:    def.SensitiveKeywords,
			DomainBlacklist:      def.DomainBlacklist,
			UpdateTime:           def.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	}, nil
}

// sanitizeJSFinderList 去除空字符串与首尾空格，保留顺序与重复（用户自管去重）
func sanitizeJSFinderList(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

type JSFinderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewJSFinderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *JSFinderLogic {
	return &JSFinderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SaveJSFinderResult 保存 JSFinder 扫描结果
func (l *JSFinderLogic) SaveJSFinderResult(req *types.SaveJSFinderResultReq) error {
	if len(req.Results) == 0 {
		return nil
	}

	modelResults := make([]*model.JSFinderResult, 0, len(req.Results))
	for _, r := range req.Results {
		modelResults = append(modelResults, &model.JSFinderResult{
			MainTaskId:       req.MainTaskId,
			Authority:        r.Authority,
			Host:             r.Host,
			Port:             r.Port,
			URL:              r.URL,
			Severity:         r.Severity,
			VulName:          r.VulName,
			Result:           r.Result,
			Tags:             r.Tags,
			MatcherName:      r.MatcherName,
			ExtractedResults: r.ExtractedResults,
			CurlCommand:      r.CurlCommand,
			Request:          r.Request,
			Response:         r.Response,
		})
	}

	m := l.svcCtx.GetJSFinderResultModel()
	// 确保索引存在
	_ = m.EnsureIndexes(l.ctx)

	// 使用 UpsertMany 替代 InsertMany：重复扫描时更新 update_time，避免产生重复脏数据
	// 修复：UpsertMany 失败时必须向上返回错误，使 API 返回非 0 code，
	// 否则 worker 会误以为保存成功而静默丢数据。UpsertMany 为幂等操作，重试安全。
	if err := m.UpsertMany(l.ctx, modelResults); err != nil {
		l.Logger.Errorf("SaveJSFinderResult UpsertMany Error: %v", err)
		return err
	}

	return nil
}

// GetJSFinderList 获取 JSFinder 结果列表（带 30s 缓存）
func (l *JSFinderLogic) GetJSFinderList(req *types.JSFinderListReq) (*types.JSFinderListResp, error) {
	req.Page, req.PageSize = normalizeListPage(req.Page, req.PageSize)
	tagsKey, _ := json.Marshal(req.TagsAny)
	cacheKey := fmt.Sprintf("jsfinder_list:%d:%d:%s:%s:%s:%s:%s:%s:%s:%s",
		req.Page, req.PageSize, req.Query, req.Severity, req.Tags, req.MatcherName, req.AIStatus, req.AIResult, string(tagsKey), req.TargetId)

	cached, cerr := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, jsfinderListCacheTTL, func() (interface{}, error) {
		return l.getJSFinderListUncached(req)
	})
	if cerr != nil {
		l.Logger.Errorf("[JSFinder] 缓存读取失败: %v", cerr)
		return l.getJSFinderListUncached(req)
	}
	if r, ok := cached.(*types.JSFinderListResp); ok && r != nil {
		return r, nil
	}
	return l.getJSFinderListUncached(req)
}

// getJSFinderListUncached 无缓存版本：实际查询逻辑
func (l *JSFinderLogic) getJSFinderListUncached(req *types.JSFinderListReq) (*types.JSFinderListResp, error) {
	// 使用 $and 数组组合所有条件，避免多个 $or 互相覆盖
	var andConditions []bson.M

	if req.TargetId != "" {
		targetType, targetValue, err := model.DecodeTargetID(req.TargetId)
		if err != nil {
			return nil, err
		}
		andConditions = append(andConditions, bson.M{"host": hostFilterForTarget(targetType, targetValue)})
	}
	if req.Query != "" {
		andConditions = append(andConditions, bson.M{"$or": []bson.M{
			{"url": primitive.Regex{Pattern: req.Query, Options: "i"}},
			{"vul_name": primitive.Regex{Pattern: req.Query, Options: "i"}},
			{"host": primitive.Regex{Pattern: req.Query, Options: "i"}},
		}})
	}

	if req.Severity != "" {
		andConditions = append(andConditions, bson.M{"severity": req.Severity})
	}

	if req.Tags != "" {
		andConditions = append(andConditions, bson.M{"tags": req.Tags})
	}

	if len(req.TagsAny) > 0 {
		andConditions = append(andConditions, bson.M{"tags": bson.M{"$in": req.TagsAny}})
	}

	if req.MatcherName != "" {
		andConditions = append(andConditions, bson.M{"matcher_name": req.MatcherName})
	}

	if req.AIResult != "" {
		andConditions = append(andConditions, bson.M{"ai_result": req.AIResult})
	}
	if req.AIStatus != "" {
		if req.AIStatus == "pending" {
			// 待研判：ai_status 为空/不存在/显式 pending 都算未研判
			andConditions = append(andConditions, bson.M{"$or": []bson.M{
				{"ai_status": bson.M{"$exists": false}},
				{"ai_status": ""},
				{"ai_status": "pending"},
			}})
		} else {
			andConditions = append(andConditions, bson.M{"ai_status": req.AIStatus})
		}
	}

	filter := bson.M{}
	if len(andConditions) > 0 {
		filter["$and"] = andConditions
	}

	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 {
		req.PageSize = 10
	}

	m := l.svcCtx.GetJSFinderResultModel()

	var total int64
	var allResults []*model.JSFinderResult
	var err error

	if len(filter) == 0 {
		total, err = m.EstimatedCount(l.ctx)
	} else {
		total, err = m.Count(l.ctx, filter)
	}
	if err != nil {
		return nil, xerr.NewServerError("Count JSFinderResult Error: " + err.Error())
	}

	opt := options.Find().
		SetSkip(int64((req.Page - 1) * req.PageSize)).
		SetLimit(int64(req.PageSize)).
		SetSort(bson.D{{Key: "create_time", Value: -1}, {Key: "_id", Value: -1}}).
		SetProjection(jsfinderListProjection)

	allResults, err = m.Find(l.ctx, filter, opt)
	if err != nil {
		return nil, xerr.NewServerError("Find JSFinderResult Error: " + err.Error())
	}

	respList := make([]*types.JSFinderResult, 0, len(allResults))
	for _, r := range allResults {
		aiAnalyzedAt := ""
		if !r.AIAnalyzedAt.IsZero() {
			aiAnalyzedAt = r.AIAnalyzedAt.Format("2006-01-02 15:04:05")
		}
		respList = append(respList, &types.JSFinderResult{
			Id:               r.Id.Hex(),
			MainTaskId:       r.MainTaskId,
			TaskName:         r.TaskName,
			Authority:        r.Authority,
			Host:             r.Host,
			Port:             r.Port,
			URL:              r.URL,
			Severity:         r.Severity,
			VulName:          r.VulName,
			Result:           r.Result,
			Tags:             r.Tags,
			MatcherName:      r.MatcherName,
			ExtractedResults: r.ExtractedResults,
			CurlCommand:      r.CurlCommand,
			Request:          r.Request,
			Response:         r.Response,
			CreateTime:       r.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:       r.UpdateTime.Format("2006-01-02 15:04:05"),
			AIStatus:         r.AIStatus,
			AIResult:         r.AIResult,
			AIAnalyzedAt:     aiAnalyzedAt,
			AIReason:         r.AIReason,
		})
	}

	return &types.JSFinderListResp{
		Code:     0,
		Msg:      "success",
		Page:     req.Page,
		PageSize: req.PageSize,
		Total:    total,
		List:     respList,
	}, nil
}

// ClearJSFinderResults 清空 JSFinder 结果
func (l *JSFinderLogic) ClearJSFinderResults() error {
	m := l.svcCtx.GetJSFinderResultModel()
	_, err := m.DeleteMany(l.ctx, bson.M{})
	if err != nil {
		l.Logger.Errorf("ClearJSFinderResults Error: %v", err)
		return xerr.NewServerError("清空JSFinder结果失败: " + err.Error())
	}

	return nil
}

// GetJSFinderDetail 按 id 取单条 JSFinder 结果（含 request/response/curl_command 大字段）。
// 列表查询已投影剥离这些大字段，详情按需回填。
func (l *JSFinderLogic) GetJSFinderDetail(req *types.JSFinderDetailReq) (*types.JSFinderDetailResp, error) {
	id := strings.TrimSpace(req.Id)
	if id == "" {
		return &types.JSFinderDetailResp{Code: 400, Msg: "id 不能为空"}, nil
	}

	doc, err := l.svcCtx.GetJSFinderResultModel().FindByID(l.ctx, id)
	if err != nil || doc == nil {
		return &types.JSFinderDetailResp{Code: 404, Msg: "未找到该 JSFinder 结果"}, nil
	}
	aiAnalyzedAt := ""
	if !doc.AIAnalyzedAt.IsZero() {
		aiAnalyzedAt = doc.AIAnalyzedAt.Format("2006-01-02 15:04:05")
	}
	return &types.JSFinderDetailResp{
		Code: 0,
		Msg:  "success",
		Data: &types.JSFinderResult{
			Id:               doc.Id.Hex(),
			MainTaskId:       doc.MainTaskId,
			TaskName:         doc.TaskName,
			Authority:        doc.Authority,
			Host:             doc.Host,
			Port:             doc.Port,
			URL:              doc.URL,
			Severity:         doc.Severity,
			VulName:          doc.VulName,
			Result:           doc.Result,
			Tags:             doc.Tags,
			MatcherName:      doc.MatcherName,
			ExtractedResults: doc.ExtractedResults,
			CurlCommand:      doc.CurlCommand,
			Request:          doc.Request,
			Response:         doc.Response,
			CreateTime:       doc.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:       doc.UpdateTime.Format("2006-01-02 15:04:05"),
			AIStatus:         doc.AIStatus,
			AIResult:         doc.AIResult,
			AIAnalyzedAt:     aiAnalyzedAt,
			AIReason:         doc.AIReason,
		},
	}, nil
}

// ==================== JSFinder AI研判 ====================

// jsfinderAIBatchTasks 全局批量任务状态表（内存map + mutex，单实例场景适用）
var jsfinderAIBatchTasks sync.Map // taskId -> *batchTaskState

type batchTaskState struct {
	mu          sync.Mutex
	TaskId      string
	Total       int64
	Completed   int64         // 成功研判条数
	RiskCount   int64         // 有风险条数
	NoRiskCount int64         // 无风险条数
	FailedCount int64         // 研判失败条数
	Status      string        // running/completed/failed/stopped/stopping
	StopCh      chan struct{} // 停止信号通道
	EndTime     time.Time     // 任务结束时间（用于TTL清理）
	consecFail  int32         // 连续AI调用失败次数（原子访问），用于熔断
}

func init() {
	// 定期清理已完成/失败超过 1 小时的批量任务，防止内存泄漏
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-1 * time.Hour)
			jsfinderAIBatchTasks.Range(func(key, value any) bool {
				state := value.(*batchTaskState)
				state.mu.Lock()
				shouldDelete := (state.Status == "completed" || state.Status == "failed" || state.Status == "stopped") && !state.EndTime.IsZero() && state.EndTime.Before(cutoff)
				state.mu.Unlock()
				if shouldDelete {
					jsfinderAIBatchTasks.Delete(key)
				}
				return true
			})
		}
	}()
}

// AnalyzeSingle 对单条JSFinder结果进行AI研判
func (l *JSFinderLogic) AnalyzeSingle(req *types.JSFinderAIAnalyzeReq) (*types.JSFinderAIAnalyzeResp, error) {
	// 1. 加载AI配置
	aiCfg, err := l.loadAIConfig()
	if err != nil {
		return &types.JSFinderAIAnalyzeResp{Code: 500, Msg: err.Error()}, nil
	}

	// 2. 查找目标记录（需要含result/extracted_results内容，不使用列表投影）
	m := l.svcCtx.GetJSFinderResultModel()
	doc, err := m.FindByID(l.ctx, req.Id)
	if err != nil {
		return &types.JSFinderAIAnalyzeResp{Code: 404, Msg: "未找到该记录"}, nil
	}

	// 3. 构造Prompt并调用大模型
	result, reason, err := l.callAIAnalysis(aiCfg, doc)
	if err != nil {
		return &types.JSFinderAIAnalyzeResp{Code: 500, Msg: "AI研判失败: " + err.Error()}, nil
	}

	// 4. 回写数据库
	now := time.Now()
	aiResult := "no_risk"
	if result == "risk" {
		aiResult = "risk"
	}
	if err := m.UpdateAIResult(l.ctx, req.Id, "completed", aiResult, reason, now); err != nil {
		return &types.JSFinderAIAnalyzeResp{Code: 500, Msg: "结果保存失败: " + err.Error()}, nil
	}

	return &types.JSFinderAIAnalyzeResp{
		Code: 0, Msg: "success",
		Data: &types.JSFinderAIAnalyzeData{
			Id: req.Id, AIStatus: "completed", AIResult: aiResult,
			AIReason: reason, AIAnalyzedAt: now.Format("2006-01-02 15:04:05"),
		},
	}, nil
}

// BatchAnalyzeAsync 启动批量研判异步任务，立即返回
// 支持三种模式：
// 1. req.Ids 非空：研判选中的数据
// 2. req.Ids 为空但有筛选条件：研判符合筛选条件的未研判数据
// 3. 都为空：研判所有未研判数据
func (l *JSFinderLogic) BatchAnalyzeAsync(req *types.JSFinderAIBatchAnalyzeReq) (*types.JSFinderAIBatchAnalyzeResp, error) {
	// 构造与列表查询一致的过滤条件（使用 $and 组合，避免 $or 互相覆盖）
	var andConditions []bson.M
	if req.Query != "" {
		andConditions = append(andConditions, bson.M{"$or": []bson.M{
			{"url": primitive.Regex{Pattern: req.Query, Options: "i"}},
			{"vul_name": primitive.Regex{Pattern: req.Query, Options: "i"}},
			{"host": primitive.Regex{Pattern: req.Query, Options: "i"}},
		}})
	}
	if req.Severity != "" {
		andConditions = append(andConditions, bson.M{"severity": req.Severity})
	}
	if req.Tags != "" {
		andConditions = append(andConditions, bson.M{"tags": req.Tags})
	}
	if len(req.TagsAny) > 0 {
		andConditions = append(andConditions, bson.M{"tags": bson.M{"$in": req.TagsAny}})
	}
	if req.MatcherName != "" {
		andConditions = append(andConditions, bson.M{"matcher_name": req.MatcherName})
	}
	// 当指定了 aiResult 筛选时（如 "有风险"/"无风险"），按筛选条件匹配；
	// 未指定时默认只处理未研判数据
	if req.AIResult != "" {
		andConditions = append(andConditions, bson.M{"ai_result": req.AIResult})
	} else {
		// 强制加上未研判条件：ai_status 不是 completed（包含空/不存在/pending）
		andConditions = append(andConditions, bson.M{"ai_status": bson.M{"$ne": "completed"}})
	}

	filter := bson.M{}
	if len(andConditions) > 0 {
		filter["$and"] = andConditions
	}

	var pendingDocs []*model.JSFinderResult
	m := l.svcCtx.GetJSFinderResultModel()

	if len(req.Ids) > 0 {
		// 模式1：按选中的ID列表
		oids := make([]primitive.ObjectID, 0, len(req.Ids))
		for _, id := range req.Ids {
			oid, err := primitive.ObjectIDFromHex(id)
			if err == nil {
				oids = append(oids, oid)
			}
		}
		idFilter := bson.M{
			"_id":       bson.M{"$in": oids},
			"ai_status": bson.M{"$ne": "completed"},
		}
		pendingDocs, _ = m.Find(l.ctx, idFilter, options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}}))
	} else {
		// 模式2/3：按过滤条件（可能为空，表示所有未研判）
		pendingDocs, _ = m.Find(l.ctx, filter, options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}}))
	}

	if len(pendingDocs) == 0 {
		return &types.JSFinderAIBatchAnalyzeResp{Code: 0, Msg: "无待研判数据", Total: 0}, nil
	}

	// 提前校验AI配置，避免启动goroutine后才发现配置缺失
	if _, err := l.loadAIConfig(); err != nil {
		return &types.JSFinderAIBatchAnalyzeResp{Code: 500, Msg: err.Error()}, nil
	}

	taskId := primitive.NewObjectID().Hex()
	state := &batchTaskState{
		TaskId: taskId,
		Total:  int64(len(pendingDocs)),
		Status: "running",
		StopCh: make(chan struct{}),
	}
	jsfinderAIBatchTasks.Store(taskId, state)

	// 启动goroutine异步处理
	go l.runBatchAnalysis(taskId, state, pendingDocs, clampAIConcurrency(req.Concurrency))

	return &types.JSFinderAIBatchAnalyzeResp{
		Code: 0, Msg: "批量研判任务已启动", TaskId: taskId, Total: int64(len(pendingDocs)),
	}, nil
}

// runBatchAnalysis 批量研判实际执行逻辑（在独立goroutine中运行）
func (l *JSFinderLogic) runBatchAnalysis(taskId string, state *batchTaskState, pendingDocs []*model.JSFinderResult, concurrency int) {
	// 使用独立的Background context，避免HTTP请求context被cancel导致后台任务中断
	bgCtx := context.Background()

	// 1. 加载AI配置（全局配置）
	aiCfg, err := l.loadAIConfigWithCtx(bgCtx)
	if err != nil {
		state.mu.Lock()
		state.Status = "failed"
		state.EndTime = time.Now()
		state.mu.Unlock()
		logx.Errorf("[JSFinder-AI] batch task %s config error: %v", taskId, err)
		return
	}

	// 2. 使用goroutine池处理（并发度由调用方控制，默认1，最大5）
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	stopped := int32(0) // 原子标记是否已停止/熔断
	m := l.svcCtx.GetJSFinderResultModel()

	for _, doc := range pendingDocs {
		// 检查是否收到停止信号
		select {
		case <-state.StopCh:
			atomic.StoreInt32(&stopped, 1)
			logx.Infof("[JSFinder-AI] batch task %s received stop signal, stopping...", taskId)
		default:
		}
		if atomic.LoadInt32(&stopped) == 1 {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(d *model.JSFinderResult) {
			defer wg.Done()
			defer func() { <-sem }()

			// 已停止/熔断，未处理数据保持待研判状态
			if atomic.LoadInt32(&stopped) == 1 {
				return
			}

			result, reason, err := l.callAIAnalysis(aiCfg, d)
			if err != nil {
				atomic.AddInt64(&state.FailedCount, 1)
				logx.Errorf("[JSFinder-AI] doc %s analyze error: %v", d.Id.Hex(), err)
				// 连续失败达到阈值，判定AI服务中断并熔断后续研判
				if atomic.AddInt32(&state.consecFail, 1) >= aiFailFastThreshold {
					l.abortBatchTask(state, &stopped)
				}
				return
			}
			atomic.StoreInt32(&state.consecFail, 0)

			now := time.Now()
			aiResult := "no_risk"
			if result == "risk" {
				aiResult = "risk"
			}
			// 仅研判成功才回写库；失败保持待研判状态，便于重试
			if err := m.UpdateAIResult(context.Background(), d.Id.Hex(), "completed", aiResult, reason, now); err != nil {
				atomic.AddInt64(&state.FailedCount, 1)
				logx.Errorf("[JSFinder-AI] doc %s save result error: %v", d.Id.Hex(), err)
				return
			}
			if aiResult == "risk" {
				atomic.AddInt64(&state.RiskCount, 1)
			} else {
				atomic.AddInt64(&state.NoRiskCount, 1)
			}
			atomic.AddInt64(&state.Completed, 1)
		}(doc)
	}
	wg.Wait()

	state.mu.Lock()
	// 熔断（failed）与用户停止（stopping）已在执行中置位，此处只收尾运行中任务
	switch state.Status {
	case "running":
		state.Status = "completed"
	case "stopping":
		state.Status = "stopped"
	}
	state.EndTime = time.Now()
	state.mu.Unlock()
	logx.Infof("[JSFinder-AI] batch task %s finished: status=%s, completed=%d/%d, risk=%d, noRisk=%d, failed=%d",
		taskId, state.Status, state.Completed, state.Total, state.RiskCount, state.NoRiskCount, state.FailedCount)
}

// abortBatchTask AI 服务连续失败熔断，终止批量任务（仅运行中状态允许，防重复关闭 StopCh）
func (l *JSFinderLogic) abortBatchTask(state *batchTaskState, stopped *int32) {
	atomic.StoreInt32(stopped, 1)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Status != "running" {
		return
	}
	state.Status = "failed"
	close(state.StopCh)
	logx.Errorf("[JSFinder-AI] batch task %s aborted: AI service consecutive failures", state.TaskId)
}

// StopBatchTask 停止批量研判任务
func (l *JSFinderLogic) StopBatchTask(taskId string) error {
	v, ok := jsfinderAIBatchTasks.Load(taskId)
	if !ok {
		return fmt.Errorf("任务不存在或已结束")
	}
	state := v.(*batchTaskState)
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.Status != "running" {
		return fmt.Errorf("任务当前状态不允许停止: %s", state.Status)
	}

	// 关闭通道发送停止信号（close保证所有等待者都能收到）
	close(state.StopCh)
	state.Status = "stopping"
	logx.Infof("[JSFinder-AI] batch task %s stop signal sent", taskId)
	return nil
}

// GetBatchProgress 查询批量研判进度
func (l *JSFinderLogic) GetBatchProgress(req *types.JSFinderAIBatchProgressReq) (*types.JSFinderAIBatchProgressResp, error) {
	v, ok := jsfinderAIBatchTasks.Load(req.TaskId)
	if !ok {
		return &types.JSFinderAIBatchProgressResp{Code: 404, Msg: "任务不存在"}, nil
	}
	state := v.(*batchTaskState)
	state.mu.Lock()
	defer state.mu.Unlock()
	return &types.JSFinderAIBatchProgressResp{
		Code: 0, Msg: "success",
		Total: state.Total, Completed: state.Completed,
		RiskCount: state.RiskCount, NoRiskCount: state.NoRiskCount, FailedCount: state.FailedCount,
		Status: state.Status,
	}, nil
}

// ==================== AI研判辅助方法 ====================

// loadAIConfig 加载AI配置（系统级配置）
func (l *JSFinderLogic) loadAIConfig() (*model.APIConfig, error) {
	return l.loadAIConfigWithCtx(l.ctx)
}

// loadAIConfigWithCtx 使用指定context加载AI配置（供后台goroutine使用独立context）
func (l *JSFinderLogic) loadAIConfigWithCtx(ctx context.Context) (*model.APIConfig, error) {
	cfgModel := model.NewAPIConfigModel(l.svcCtx.MongoDB)
	doc, err := cfgModel.FindByPlatform(ctx, "ai")
	if err == nil && doc != nil {
		return doc, nil
	}

	return nil, fmt.Errorf("未配置AI服务，请先在系统设置中配置AI")
}

// callAIAnalysis 调用大模型研判单条记录
// 返回 (result="risk"/"no_risk", reason文本, error)
func (l *JSFinderLogic) callAIAnalysis(cfg *model.APIConfig, doc *model.JSFinderResult) (string, string, error) {
	client := NewAIClientFromConfig(cfg)

	prompt := buildJSAnalysisPrompt(doc)

	ctx, cancel := context.WithTimeout(context.Background(), aiCallTimeout)
	defer cancel()

	content, err := client.Chat(ctx, prompt, 1024)
	if err != nil {
		return "", "", err
	}

	return parseAIAnalysisResult(content)
}

// buildJSAnalysisPrompt 构造JS扫描结果的风险研判Prompt
func buildJSAnalysisPrompt(doc *model.JSFinderResult) string {
	var sb strings.Builder
	sb.WriteString("你是一个Web安全专家，请判断以下JS扫描发现是否构成真实的敏感信息泄露风险。\n\n")
	sb.WriteString(fmt.Sprintf("目标URL: %s\n", doc.URL))
	sb.WriteString(fmt.Sprintf("发现类型: %s\n", doc.VulName))
	sb.WriteString(fmt.Sprintf("严重级别: %s\n", doc.Severity))
	if doc.MatcherName != "" {
		sb.WriteString(fmt.Sprintf("匹配规则: %s\n", doc.MatcherName))
	}
	if len(doc.ExtractedResults) > 0 {
		sb.WriteString(fmt.Sprintf("匹配内容:\n%s\n", strings.Join(doc.ExtractedResults, "\n")))
	}
	sb.WriteString(fmt.Sprintf("原始结果: %s\n", doc.Result))
	sb.WriteString("\n请判断该发现是否为真实的敏感信息泄露风险（如硬编码密钥、真实token/密码、未授权接口泄露真实数据等）。\n")
	sb.WriteString("注意：普通路径、URL列表、IP地址、公开邮箱、示例值等不构成风险；只有真正有效的密钥、token、密码、身份证、手机号等敏感数据泄露才标记为有风险。\n")
	sb.WriteString(`请严格按以下JSON格式回复（不要有其他内容）：
{"result": "risk" 或 "no_risk", "reason": "简短说明判断理由，不超过100字"}`)
	return sb.String()
}

// parseAIAnalysisResult 解析AI返回的JSON结果
func parseAIAnalysisResult(content string) (string, string, error) {
	content = strings.TrimSpace(content)
	// 提取被```json ```包裹的内容
	if strings.Contains(content, "```") {
		if idx := strings.Index(content, "```"); idx >= 0 {
			rest := content[idx+3:]
			if strings.HasPrefix(rest, "json") {
				rest = rest[4:]
			}
			if end := strings.Index(rest, "```"); end >= 0 {
				content = rest[:end]
			}
		}
	}
	content = strings.TrimSpace(content)

	var parsed struct {
		Result string `json:"result"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		// 降级容错：文本中包含"risk"且不包含"no_risk"则判定为risk
		lower := strings.ToLower(content)
		if strings.Contains(lower, "no_risk") {
			return "no_risk", "AI返回格式异常，降级判断为无风险", nil
		}
		if strings.Contains(lower, "risk") {
			return "risk", "AI返回格式异常，降级判断为有风险", nil
		}
		return "no_risk", "AI返回格式异常，默认无风险", nil
	}
	result := strings.ToLower(parsed.Result)
	if result != "risk" {
		result = "no_risk"
	}
	return result, parsed.Reason, nil
}
