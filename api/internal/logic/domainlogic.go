package logic

import (
	"context"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// extractDomainFromAsset 从资产中提取域名，DomainList 与 DomainStat 共用。
// 回退顺序: Domain → Host → Authority（去端口），跳过 IP 地址。
func extractDomainFromAsset(asset *model.Asset) string {
	domain := asset.Domain
	if domain == "" {
		domain = asset.Host
	}
	if domain == "" {
		// Authority 格式为 "host:port"，需去除端口后再判断
		addr := asset.Authority
		if h, _, err := net.SplitHostPort(addr); err == nil {
			domain = h
		} else {
			domain = addr
		}
	}
	if domain == "" || common.IsIPAddress(domain) {
		return ""
	}
	return domain
}

type DomainLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDomainLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DomainLogic {
	return &DomainLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DomainList 域名列表 - 从资产中提取域名信息
func (l *DomainLogic) DomainList(req *types.DomainListReq) (*types.DomainListResp, error) {
	resp := &types.DomainListResp{Code: 0, List: []types.Domain{}}

	orgMap := common.LoadOrgMap(l.ctx, l.svcCtx)

	assetModel := l.svcCtx.GetAssetModel()

	// 构建查询条件
	baseCondition := []bson.M{
		{"category": "domain"},
		{"domain": bson.M{"$exists": true, "$ne": ""}},
		{"source": "subfinder"},
	}

	filter := bson.M{}

	// 优先使用通用 Query 关键字（当未指定 Domain/RootDomain/IP 时）
	if req.Query != "" && req.Domain == "" && req.RootDomain == "" && req.IP == "" {
		q := regexp.QuoteMeta(req.Query)
		filter["$and"] = []bson.M{
			{"$or": baseCondition},
			{"$or": []bson.M{
				{"domain": bson.M{"$regex": q, "$options": "i"}},
				{"host": bson.M{"$regex": q, "$options": "i"}},
				{"ip.ipv4.ip": bson.M{"$regex": q, "$options": "i"}},
			}},
		}
	} else if req.Domain != "" {
		filter["$and"] = []bson.M{
			{"$or": baseCondition},
			{"domain": bson.M{"$regex": regexp.QuoteMeta(req.Domain), "$options": "i"}},
		}
	} else if req.RootDomain != "" {
		escapedRoot := regexp.QuoteMeta(req.RootDomain)
		conditions := []bson.M{
			{"$or": baseCondition},
			{"$or": []bson.M{
				{"domain": bson.M{"$regex": "(^|\\.)" + escapedRoot + "$", "$options": "i"}},
				{"host": bson.M{"$regex": "(^|\\.)" + escapedRoot + "$", "$options": "i"}},
			}},
		}
		if q := strings.TrimSpace(req.Query); q != "" {
			qm := regexp.QuoteMeta(q)
			conditions = append(conditions, bson.M{"$or": []bson.M{
				{"domain": bson.M{"$regex": qm, "$options": "i"}},
				{"host": bson.M{"$regex": qm, "$options": "i"}},
				{"cname": bson.M{"$regex": qm, "$options": "i"}},
				{"ip.ipv4.ip": bson.M{"$regex": qm, "$options": "i"}},
			}})
		}
		if len(req.Labels) > 0 {
			conditions = append(conditions, bson.M{"labels": bson.M{"$in": req.Labels}})
		}
		filter["$and"] = conditions
	} else if req.IP != "" {
		filter["$and"] = []bson.M{
			{"$or": baseCondition},
			{"ip.ipv4.ip": bson.M{"$regex": regexp.QuoteMeta(req.IP), "$options": "i"}},
		}
	} else {
		filter["$or"] = baseCondition
	}

	if req.OrgId != "" {
		filter["org_id"] = req.OrgId
	}
	if len(req.Labels) > 0 && req.RootDomain == "" {
		appendBSONAndCondition(filter, bson.M{"labels": bson.M{"$in": req.Labels}})
	}
	if req.TargetId != "" {
		targetType, targetValue, err := model.DecodeTargetID(req.TargetId)
		if err != nil {
			return nil, err
		}
		appendBSONAndCondition(filter, bson.M{"host": hostFilterForTarget(targetType, targetValue)})
	}

	// 外部明细接口始终使用有界分页。
	req.Page, req.PageSize = normalizeListPage(req.Page, req.PageSize)
	resp.Page, resp.PageSize = req.Page, req.PageSize

	// 服务端聚合（$group by domain + $facet 分页），避免全量加载资产到内存
	aggResults, total, err := assetModel.AggregateDomains(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		l.Logger.Errorf("DomainList 聚合查询失败: %v", err)
		return resp, nil
	}

	// 转换为响应类型
	list := make([]types.Domain, 0, len(aggResults))
	for _, r := range aggResults {
		list = append(list, types.Domain{
			Id:         r.Id.Hex(),
			Domain:     r.Domain,
			RootDomain: common.GetRootDomain(r.Domain),
			IPs:        r.IPs,
			CName:      r.CName,
			Source:     r.Source,
			Labels:     r.Labels,
			OrgId:      r.OrgId,
			OrgName:    orgMap[r.OrgId],
			IsNew:      r.IsNew,
			CreateTime: r.CreateTime.Local().Format("2006-01-02 15:04:05"),
			UpdateTime: r.UpdateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	resp.Total = total
	resp.List = list
	return resp, nil
}

// DomainStat 域名统计
// 优化点：原实现全表加载所有资产到内存只为 distinct 域名/根域名/解析数/新增数
// 现改用 FindWithSort+AssetListProjection 限制字段 + 整体结果走 60s 缓存
func (l *DomainLogic) DomainStat() (*types.DomainStatResp, error) {
	cacheKey := "domain_stat"
	cached, err := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, 60*time.Second, func() (interface{}, error) {
		resp := &types.DomainStatResp{Code: 0}

		domainSet := make(map[string]bool)
		rootDomainSet := make(map[string]bool)
		resolvedCount := 0
		newCount := 0
		since := time.Now().AddDate(0, 0, -1)

		filter := bson.M{
			"$or": []bson.M{
				{"category": "domain"},
				{"domain": bson.M{"$exists": true, "$ne": ""}},
				{"source": "subfinder"},
			},
		}

		assetModel := l.svcCtx.GetAssetModel()

		// 全量查询 + AssetAggProjection 瘦投影（FindWithSort 会被 NormalizePage 钳到 100 条导致漏统计）
		assets, err := assetModel.FindAllForAgg(l.ctx, filter)
		if err != nil {
			l.Logger.Errorf("DomainStat 查询资产失败: %v", err)
			return resp, nil
		}

		for _, asset := range assets {
			domain := extractDomainFromAsset(&asset)
			if domain == "" {
				continue
			}

			if !domainSet[domain] {
				domainSet[domain] = true
				rootDomainSet[common.GetRootDomain(domain)] = true

				// 检查是否已解析（有IP）
				if len(asset.Ip.IpV4) > 0 || len(asset.Ip.IpV6) > 0 {
					resolvedCount++
				}

				// 检查是否新增（首次发现在近 24 小时内）
				firstSeen := asset.FirstSeenTime
				if firstSeen.IsZero() {
					firstSeen = asset.CreateTime
				}
				if !firstSeen.Before(since) {
					newCount++
				}
			}
		}

		resp.Total = len(domainSet)
		resp.RootDomainCount = len(rootDomainSet)
		resp.ResolvedCount = resolvedCount
		resp.NewCount = newCount

		return resp, nil
	})
	if err != nil {
		return &types.DomainStatResp{Code: 0}, nil
	}
	if r, ok := cached.(*types.DomainStatResp); ok {
		return r, nil
	}
	return &types.DomainStatResp{Code: 0}, nil
}

// DomainDelete 删除域名（删除该域名对应的所有资产）
func (l *DomainLogic) DomainDelete(req *types.DomainDeleteReq) (*types.BaseResp, error) {
	assetModel := l.svcCtx.GetAssetModel()

	// 先通过ID找到域名值
	asset, err := assetModel.FindById(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "查询失败"}, nil
	}
	var domainName string
	if asset != nil {
		domainName = extractDomainFromAsset(asset)
	}

	if domainName == "" {
		return &types.BaseResp{Code: 500, Msg: "删除失败，域名不存在"}, nil
	}

	// 删除所有包含该域名的资产
	// authority 正则需转义域名中的点，否则 "a.com" 会误匹配 "axcom"。
	escapedDomain := regexp.QuoteMeta(domainName)
	filter := bson.M{
		"$or": []bson.M{
			{"domain": domainName},
			{"host": domainName},
			{"authority": bson.M{"$regex": "^" + escapedDomain + "(:|$)"}},
		},
	}
	totalDeleted, _ := assetModel.DeleteByFilter(l.ctx, filter)

	if totalDeleted == 0 {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	l.svcCtx.QueryCache.Delete("domain_stat")
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// DomainBatchDelete 批量删除域名
func (l *DomainLogic) DomainBatchDelete(req *types.DomainBatchDeleteReq) (*types.BaseResp, error) {
	if len(req.Ids) == 0 {
		return &types.BaseResp{Code: 400, Msg: "请选择要删除的域名"}, nil
	}

	assetModel := l.svcCtx.GetAssetModel()

	// 先收集所有要删除的域名值
	assets, err := assetModel.FindByIds(l.ctx, req.Ids)
	if err != nil {
		l.Logger.Errorf("DomainBatchDelete 查询资产失败: %v", err)
	}

	domainNames := make(map[string]bool)
	for _, asset := range assets {
		domainName := extractDomainFromAsset(&asset)
		if domainName != "" {
			domainNames[domainName] = true
		}
	}

	if len(domainNames) == 0 {
		return &types.BaseResp{Code: 500, Msg: "删除失败，未找到匹配的域名"}, nil
	}

	// 构建域名列表
	domains := make([]string, 0, len(domainNames))
	for d := range domainNames {
		domains = append(domains, d)
	}

	// 删除所有包含这些域名的资产
	filter := bson.M{
		"$or": []bson.M{
			{"domain": bson.M{"$in": domains}},
			{"host": bson.M{"$in": domains}},
		},
	}
	totalDeleted, _ := assetModel.DeleteByFilter(l.ctx, filter)

	if totalDeleted == 0 {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	l.svcCtx.QueryCache.Delete("domain_stat")
	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(int64(len(domainNames)), 10) + " 个域名"}, nil
}
