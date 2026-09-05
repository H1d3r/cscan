package logic

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type VulListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewVulListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VulListLogic {
	return &VulListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *VulListLogic) VulList(req *types.VulListReq) (resp *types.VulListResp, err error) {
	// 构建查询条件
	filter := bson.M{}
	// 如果提供了通用 Query 且未显式指定 Authority/Host，则按多个字段模糊匹配
	if req.Query != "" && req.Authority == "" && req.Host == "" {
		q := regexp.QuoteMeta(req.Query)
		filter["$or"] = []bson.M{
			{"authority": bson.M{"$regex": q, "$options": "i"}},
			{"host": bson.M{"$regex": q, "$options": "i"}},
			{"url": bson.M{"$regex": q, "$options": "i"}},
			{"pocfile": bson.M{"$regex": q, "$options": "i"}},
			{"vul_name": bson.M{"$regex": q, "$options": "i"}},
		}
	}
	if req.Authority != "" {
		authQuery := req.Authority
		if strings.HasPrefix(authQuery, "http://") {
			authQuery = strings.TrimPrefix(authQuery, "http://")
		} else if strings.HasPrefix(authQuery, "https://") {
			authQuery = strings.TrimPrefix(authQuery, "https://")
		}
		authQuery = regexp.QuoteMeta(authQuery)
		filter["authority"] = bson.M{"$regex": authQuery, "$options": "i"}
	}
	if req.Severity != "" {
		filter["severity"] = req.Severity
	}
	if req.Source != "" {
		filter["source"] = req.Source
	}
	// 支持按host和port筛选（用于资产详情页查询漏洞）
	if req.Host != "" {
		filter["host"] = req.Host
	}
	if req.TargetId != "" {
		targetType, targetValue, decodeErr := model.DecodeTargetID(req.TargetId)
		if decodeErr != nil {
			return nil, decodeErr
		}
		appendBSONAndCondition(filter, bson.M{"host": hostFilterForTarget(targetType, targetValue)})
	}
	if req.Port > 0 {
		filter["port"] = req.Port
	}
	// Phase 5: 敏感信息/敏感目录页面通过下列 3 个字段做服务端固定过滤。
	if req.IsRisk != nil {
		filter["is_risk"] = *req.IsRisk
	}
	if req.RiskSource != "" {
		filter["risk_source"] = req.RiskSource
	}
	// T1.3: 按生命周期状态过滤（不传时行为不变）
	if req.Status != "" {
		filter["status"] = req.Status
	}
	// T4.3: 快速筛选——"🆕 新发现"：first_seen_time 在窗口内（口径与 dashboard/changes 的 riskNewInWindow 一致，默认 7 天）
	if req.IsNew {
		days := req.FirstSeenWithinDays
		if days <= 0 {
			days = 7
		}
		cutoff := time.Now().AddDate(0, 0, -days)
		filter["first_seen_time"] = bson.M{"$gte": cutoff}
	}
	// T4.3: 快速筛选——"待确认"：目标不可达、待复验确认
	if req.VerifyPending {
		filter["verify_pending"] = true
	}
	if req.HighRiskOnly {
		appendBSONAndCondition(filter, bson.M{"$or": highRiskVulnerabilityClause()})
	}
	if len(req.KeywordAny) > 0 {
		orClauses := make([]bson.M, 0, len(req.KeywordAny)*2)
		for _, kw := range req.KeywordAny {
			if kw == "" {
				continue
			}
			escaped := regexp.QuoteMeta(kw)
			orClauses = append(orClauses,
				bson.M{"vul_name": bson.M{"$regex": escaped, "$options": "i"}},
				bson.M{"tags": kw},
			)
		}
		if len(orClauses) > 0 {
			// 与已有 $or（通用 Query 模糊）冲突时后者优先，避免语义错乱：
			// 已有 Query 时不再叠加 keyword $or，让调用方二选一。
			if _, exists := filter["$or"]; !exists {
				filter["$or"] = orClauses
			}
		}
	}

	var total int64
	var vuls []model.Vul

	req.Page, req.PageSize = normalizeListPage(req.Page, req.PageSize)
	vulModel := l.svcCtx.GetVulModel()

	// 查询总数
	total, err = vulModel.Count(l.ctx, filter)
	if err != nil {
		return &types.VulListResp{Code: 500, Msg: "查询失败"}, nil
	}

	// 查询列表
	if req.Sort == "severity" {
		vuls, err = vulModel.FindBySeveritySort(l.ctx, filter, req.Page, req.PageSize)
	} else {
		vuls, err = vulModel.Find(l.ctx, filter, req.Page, req.PageSize)
	}
	if err != nil {
		return &types.VulListResp{Code: 500, Msg: "查询失败"}, nil
	}

	// 转换响应
	list := make([]types.Vul, 0, len(vuls))
	for _, v := range vuls {
		vul := types.Vul{
			Id:               v.Id.Hex(),
			Authority:        v.Authority,
			Url:              v.Url,
			PocFile:          v.PocFile,
			Source:           v.Source,
			Severity:         v.Severity,
			Result:           v.Result,
			VulName:          v.VulName,
			Tags:             v.Tags,
			CreateTime:       v.CreateTime.Local().Format("2006-01-02 15:04:05"),
			UpdateTime:       v.UpdateTime.Local().Format("2006-01-02 15:04:05"),
			ScanCount:        v.ScanCount,
			MatcherName:      v.MatcherName,
			ExtractedResults: v.ExtractedResults,
			// T1.3: 状态字段
			Status:         v.Status,
			FixedAt:        formatTimeIfNotZero(v.FixedAt),
			LastVerifiedAt: formatTimeIfNotZero(v.LastVerifiedAt),
			// 单条复验状态与结论
			ReverifyStatus:     v.ReverifyStatus,
			ReverifyConclusion: v.ReverifyConclusion,
			ReverifyAt:         formatTimeIfNotZero(v.ReverifyAt),
			ReverifyBy:         v.ReverifyBy,
			ReverifyMessage:    v.ReverifyMessage,
		}
		// 新增字段 - 时间追踪
		if !v.FirstSeenTime.IsZero() {
			vul.FirstSeenTime = v.FirstSeenTime.Local().Format("2006-01-02 15:04:05")
		}
		if !v.LastSeenTime.IsZero() {
			vul.LastSeenTime = v.LastSeenTime.Local().Format("2006-01-02 15:04:05")
		}
		list = append(list, vul)
	}

	return &types.VulListResp{
		Code:     0,
		Msg:      "success",
		Page:     req.Page,
		PageSize: req.PageSize,
		Total:    int(total),
		List:     list,
	}, nil
}

// severityRank 将严重度字符串映射为可排序等级（T4.3）
func severityRank(s string) int {
	switch s {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// VulLogic 漏洞管理逻辑
type VulLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewVulLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VulLogic {
	return &VulLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *VulLogic) VulDelete(req *types.VulDeleteReq) (resp *types.BaseResp, err error) {
	vulModel := l.svcCtx.GetVulModel()
	count, err := vulModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败: " + err.Error()}, nil
	}
	if count == 0 {
		return &types.BaseResp{Code: 404, Msg: "漏洞不存在"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

func (l *VulLogic) VulBatchDelete(req *types.VulBatchDeleteReq) (resp *types.BaseResp, err error) {
	vulModel := l.svcCtx.GetVulModel()
	deleted, err := vulModel.BatchDelete(l.ctx, req.Ids)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败: " + err.Error()}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(deleted, 10) + " 条记录"}, nil
}

func (l *VulLogic) VulClear() (resp *types.BaseResp, err error) {
	vulModel := l.svcCtx.GetVulModel()
	deleted, err := vulModel.Clear(l.ctx)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "清空失败: " + err.Error()}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "成功清空 " + strconv.FormatInt(deleted, 10) + " 条漏洞"}, nil
}

// VulStatLogic 漏洞统计逻辑
type VulStatLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewVulStatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VulStatLogic {
	return &VulStatLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *VulStatLogic) VulStat() (resp *types.VulStatResp, err error) {
	// 聚合统计走 60s 缓存（带 singleflight 防击穿），扫描完成可主动失效
	cacheKey := "vul_stat"
	cached, cacheErr := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, 60*time.Second, func() (interface{}, error) {
		now := time.Now()

		vulModel := l.svcCtx.GetVulModel()
		stats, statErr := vulModel.AggregateStats(l.ctx, now)
		if statErr != nil {
			l.Logger.Errorf("[VulStat] aggregation failed: %v", statErr)
			return nil, statErr
		}

		return &types.VulStatResp{
			Code:         0,
			Msg:          "success",
			Total:        int(stats.Total),
			Critical:     int(stats.Critical),
			High:         int(stats.High),
			Medium:       int(stats.Medium),
			Low:          int(stats.Low),
			Info:         int(stats.Info),
			Week:         int(stats.Week),
			Month:        int(stats.Month),
			Open:         int(stats.Open),
			Fixed:        int(stats.Fixed),
			Ignored:      int(stats.Ignored),
			OpenCritical: int(stats.OpenCritical),
			OpenHigh:     int(stats.OpenHigh),
			OpenMedium:   int(stats.OpenMedium),
			OpenLow:      int(stats.OpenLow),
			OpenInfo:     int(stats.OpenInfo),
		}, nil
	})
	if cacheErr != nil {
		return &types.VulStatResp{Code: 500, Msg: "统计查询失败"}, nil
	}
	if r, ok := cached.(*types.VulStatResp); ok {
		return r, nil
	}
	return &types.VulStatResp{Code: 500, Msg: "统计查询失败"}, nil
}

// VulDetailLogic 漏洞详情逻辑
type VulDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewVulDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VulDetailLogic {
	return &VulDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *VulDetailLogic) VulDetail(req *types.VulDetailReq) (resp *types.VulDetailResp, err error) {
	if req.Id == "" {
		return &types.VulDetailResp{Code: 400, Msg: "漏洞ID不能为空"}, nil
	}

	var vul *model.Vul

	vulModel := l.svcCtx.GetVulModel()
	vul, err = vulModel.FindById(l.ctx, req.Id)

	if vul == nil {
		return &types.VulDetailResp{Code: 404, Msg: "漏洞不存在"}, nil
	}

	// 构建漏洞详情
	detail := &types.VulDetail{
		Id:         vul.Id.Hex(),
		Authority:  vul.Authority,
		Host:       vul.Host,
		Port:       vul.Port,
		Url:        vul.Url,
		PocFile:    vul.PocFile,
		Source:     vul.Source,
		Severity:   vul.Severity,
		Result:     vul.Result,
		VulName:    vul.VulName,
		Tags:       vul.Tags,
		CreateTime: vul.CreateTime.Local().Format("2006-01-02 15:04:05"),
		UpdateTime: vul.UpdateTime.Local().Format("2006-01-02 15:04:05"),
		// 知识库信息
		CvssScore:   vul.CvssScore,
		CveId:       vul.CveId,
		CweId:       vul.CweId,
		Remediation: vul.Remediation,
		References:  vul.References,
		// 时间追踪
		ScanCount: vul.ScanCount,
		// T1.3: 状态字段
		Status:           vul.Status,
		FixedAt:          formatTimeIfNotZero(vul.FixedAt),
		LastVerifiedAt:   formatTimeIfNotZero(vul.LastVerifiedAt),
		FixConfirmSource: vul.FixConfirmSource,
		// 单条复验状态与结论
		ReverifyStatus:     vul.ReverifyStatus,
		ReverifyConclusion: vul.ReverifyConclusion,
		ReverifyAt:         formatTimeIfNotZero(vul.ReverifyAt),
		ReverifyBy:         vul.ReverifyBy,
		ReverifyMessage:    vul.ReverifyMessage,
	}

	// 时间追踪字段
	if !vul.FirstSeenTime.IsZero() {
		detail.FirstSeenTime = vul.FirstSeenTime.Local().Format("2006-01-02 15:04:05")
	}
	if !vul.LastSeenTime.IsZero() {
		detail.LastSeenTime = vul.LastSeenTime.Local().Format("2006-01-02 15:04:05")
	}

	// 证据链
	if vul.MatcherName != "" || len(vul.ExtractedResults) > 0 || vul.CurlCommand != "" || vul.Request != "" || vul.Response != "" {
		detail.Evidence = &types.VulEvidence{
			MatcherName:       vul.MatcherName,
			ExtractedResults:  vul.ExtractedResults,
			CurlCommand:       vul.CurlCommand,
			Request:           vul.Request,
			Response:          vul.Response,
			ResponseTruncated: vul.ResponseTruncated,
		}
	}

	return &types.VulDetailResp{
		Code: 0,
		Msg:  "success",
		Data: detail,
	}, nil
}

// VulUpdateStatusLogic 漏洞状态批量更新（T1.3）
type VulUpdateStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewVulUpdateStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VulUpdateStatusLogic {
	return &VulUpdateStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// VulUpdateStatus 批量更新漏洞生命周期状态（open/fixed/ignored）。
// 校验状态合法性；对所有相关 workspace 执行批量更新并汇总实际修改条数。
func (l *VulUpdateStatusLogic) VulUpdateStatus(req *types.VulUpdateStatusReq) (resp *types.VulUpdateStatusResp, err error) {
	if len(req.Ids) == 0 {
		return &types.VulUpdateStatusResp{Code: 400, Msg: "请选择要更新的漏洞"}, nil
	}
	// 校验状态合法性（防止非法字符串写入）
	var valid bool
	switch req.Status {
	case model.VulStatusOpen, model.VulStatusFixed, model.VulStatusIgnored:
		valid = true
	}
	if !valid {
		return &types.VulUpdateStatusResp{Code: 400, Msg: "非法的漏洞状态: " + req.Status}, nil
	}

	source := model.VulFixSourceManual
	vulModel := l.svcCtx.GetVulModel()
	var totalUpdated int64
	switch req.Status {
	case model.VulStatusFixed:
		totalUpdated, err = vulModel.MarkFixed(l.ctx, req.Ids, source)
	case model.VulStatusOpen:
		totalUpdated, err = vulModel.MarkOpen(l.ctx, req.Ids, source)
	case model.VulStatusIgnored:
		totalUpdated, err = vulModel.MarkIgnored(l.ctx, req.Ids)
	}
	if err != nil {
		l.Logger.Errorf("[VulUpdateStatus] update failed: %v", err)
		return &types.VulUpdateStatusResp{
			Code:    500,
			Msg:     "状态更新失败: " + err.Error(),
			Updated: int(totalUpdated),
		}, nil
	}

	// 失效漏洞统计缓存，使工作台安全评分即时反映状态变更
	l.svcCtx.QueryCache.Delete("vul_stat")

	return &types.VulUpdateStatusResp{
		Code:    0,
		Msg:     "success",
		Updated: int(totalUpdated),
	}, nil
}
