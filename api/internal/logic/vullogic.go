package logic

import (
	"context"
	"sort"
	"strconv"
	"time"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

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

func (l *VulListLogic) VulList(req *types.VulListReq, workspaceId string) (resp *types.VulListResp, err error) {
	// 构建查询条件
	filter := bson.M{}
	if req.Authority != "" {
		filter["authority"] = bson.M{"$regex": req.Authority, "$options": "i"}
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
	if req.Port > 0 {
		filter["port"] = req.Port
	}

	var total int64
	var vuls []model.Vul

	// 获取需要查询的工作空间列表
	wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)

	// 如果查询多个工作空间
	if len(wsIds) > 1 || workspaceId == "" || workspaceId == "all" {
		// 收集所有工作空间的数据
		var allVuls []model.Vul
		for _, wsId := range wsIds {
			vulModel := l.svcCtx.GetVulModel(wsId)
			wsTotal, _ := vulModel.Count(l.ctx, filter)
			total += wsTotal

			wsVuls, _ := vulModel.Find(l.ctx, filter, 0, 0)
			allVuls = append(allVuls, wsVuls...)
		}

		// 按创建时间排序
		sort.Slice(allVuls, func(i, j int) bool {
			return allVuls[i].CreateTime.After(allVuls[j].CreateTime)
		})

		// 分页
		start := (req.Page - 1) * req.PageSize
		end := start + req.PageSize
		if start > len(allVuls) {
			start = len(allVuls)
		}
		if end > len(allVuls) {
			end = len(allVuls)
		}
		vuls = allVuls[start:end]
	} else {
		vulModel := l.svcCtx.GetVulModel(workspaceId)

		// 查询总数
		total, err = vulModel.Count(l.ctx, filter)
		if err != nil {
			return &types.VulListResp{Code: 500, Msg: "查询失败"}, nil
		}

		// 查询列表
		vuls, err = vulModel.Find(l.ctx, filter, req.Page, req.PageSize)
		if err != nil {
			return &types.VulListResp{Code: 500, Msg: "查询失败"}, nil
		}
	}

	// 转换响应
	list := make([]types.Vul, 0, len(vuls))
	for _, v := range vuls {
		vul := types.Vul{
			Id:         v.Id.Hex(),
			Authority:  v.Authority,
			Url:        v.Url,
			PocFile:    v.PocFile,
			Source:     v.Source,
			Severity:   v.Severity,
			Result:     v.Result,
			CreateTime: v.CreateTime.Local().Format("2006-01-02 15:04:05"),
			ScanCount:  v.ScanCount,
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
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
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

func (l *VulLogic) VulDelete(req *types.VulDeleteReq, workspaceId string) (resp *types.BaseResp, err error) {
	vulModel := l.svcCtx.GetVulModel(workspaceId)
	if err := vulModel.Delete(l.ctx, req.Id); err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败: " + err.Error()}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

func (l *VulLogic) VulBatchDelete(req *types.VulBatchDeleteReq, workspaceId string) (resp *types.BaseResp, err error) {
	vulModel := l.svcCtx.GetVulModel(workspaceId)
	deleted, err := vulModel.BatchDelete(l.ctx, req.Ids)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败: " + err.Error()}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(deleted, 10) + " 条记录"}, nil
}

func (l *VulLogic) VulClear(workspaceId string) (resp *types.BaseResp, err error) {
	vulModel := l.svcCtx.GetVulModel(workspaceId)
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

func (l *VulStatLogic) VulStat(workspaceId string) (resp *types.VulStatResp, err error) {
	var total, critical, high, medium, low, info, week, month int64

	// 获取需要查询的工作空间列表
	wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)

	for _, wsId := range wsIds {
		vulModel := l.svcCtx.GetVulModel(wsId)

		// 统计总数
		c, _ := vulModel.Count(l.ctx, bson.M{})
		total += c

		// 按严重级别统计
		c, _ = vulModel.Count(l.ctx, bson.M{"severity": "critical"})
		critical += c
		c, _ = vulModel.Count(l.ctx, bson.M{"severity": "high"})
		high += c
		c, _ = vulModel.Count(l.ctx, bson.M{"severity": "medium"})
		medium += c
		c, _ = vulModel.Count(l.ctx, bson.M{"severity": "low"})
		low += c
		c, _ = vulModel.Count(l.ctx, bson.M{"severity": "info"})
		info += c

		// 近7天和近30天统计
		now := time.Now()
		weekAgo := now.AddDate(0, 0, -7)
		monthAgo := now.AddDate(0, 0, -30)

		c, _ = vulModel.Count(l.ctx, bson.M{"create_time": bson.M{"$gte": weekAgo}})
		week += c
		c, _ = vulModel.Count(l.ctx, bson.M{"create_time": bson.M{"$gte": monthAgo}})
		month += c
	}

	return &types.VulStatResp{
		Code:     0,
		Msg:      "success",
		Total:    int(total),
		Critical: int(critical),
		High:     int(high),
		Medium:   int(medium),
		Low:      int(low),
		Info:     int(info),
		Week:     int(week),
		Month:    int(month),
	}, nil
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

func (l *VulDetailLogic) VulDetail(req *types.VulDetailReq, workspaceId string) (resp *types.VulDetailResp, err error) {
	if req.Id == "" {
		return &types.VulDetailResp{Code: 400, Msg: "漏洞ID不能为空"}, nil
	}

	var vul *model.Vul

	// 如果是全部空间模式，遍历所有工作空间查找
	if workspaceId == "" || workspaceId == "all" {
		wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, "all")
		for _, wsId := range wsIds {
			vulModel := l.svcCtx.GetVulModel(wsId)
			if v, err := vulModel.FindById(l.ctx, req.Id); err == nil && v != nil {
				vul = v
				break
			}
		}
	} else {
		vulModel := l.svcCtx.GetVulModel(workspaceId)
		vul, err = vulModel.FindById(l.ctx, req.Id)
	}

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
		CreateTime: vul.CreateTime.Local().Format("2006-01-02 15:04:05"),
		// 知识库信息
		CvssScore:   vul.CvssScore,
		CveId:       vul.CveId,
		CweId:       vul.CweId,
		Remediation: vul.Remediation,
		References:  vul.References,
		// 时间追踪
		ScanCount: vul.ScanCount,
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
