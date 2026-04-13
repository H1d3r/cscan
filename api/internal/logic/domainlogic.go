package logic

import (
	"context"
	"strconv"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"go.mongodb.org/mongo-driver/bson"
)

type DomainLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDomainLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DomainLogic {
	return &DomainLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DomainList 域名列表 - 从资产中提取域名信息
func (l *DomainLogic) DomainList(req *types.DomainListReq, workspaceId string) (*types.DomainListResp, error) {
	resp := &types.DomainListResp{Code: 0, List: []types.Domain{}}

	workspaceIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)
	if len(workspaceIds) == 0 {
		return resp, nil
	}

	orgMap := common.LoadOrgMap(l.ctx, l.svcCtx)

	// 用于去重和聚合域名
	domainMap := make(map[string]*types.Domain)

	for _, wsId := range workspaceIds {
		assetModel := model.NewAssetModel(l.svcCtx.MongoDB, wsId)

		// 构建查询条件
		// 基础条件：category=domain 或 domain字段不为空 或 source=subfinder
		baseCondition := []bson.M{
			{"category": "domain"},
			{"domain": bson.M{"$exists": true, "$ne": ""}},
			{"source": "subfinder"},
		}

		filter := bson.M{}

		// 优先使用通用 Query 关键字（当未指定 Domain/RootDomain/IP 时）
		if req.Query != "" && req.Domain == "" && req.RootDomain == "" && req.IP == "" {
			filter["$and"] = []bson.M{
				{"$or": baseCondition},
				{"$or": []bson.M{
					{"domain": bson.M{"$regex": req.Query, "$options": "i"}},
					{"host": bson.M{"$regex": req.Query, "$options": "i"}},
					{"ip.ipv4.ip": bson.M{"$regex": req.Query, "$options": "i"}},
				}},
			}
		} else if req.Domain != "" {
			// 域名搜索
			filter["$and"] = []bson.M{
				{"$or": baseCondition},
				{"domain": bson.M{"$regex": req.Domain, "$options": "i"}},
			}
		} else if req.RootDomain != "" {
			// 根域名搜索
			filter["$and"] = []bson.M{
				{"$or": baseCondition},
				{"$or": []bson.M{
					{"domain": bson.M{"$regex": "\\." + req.RootDomain + "$", "$options": "i"}},
					{"host": bson.M{"$regex": "\\." + req.RootDomain + "$", "$options": "i"}},
				}},
			}
		} else if req.IP != "" {
			// IP搜索 - 搜索解析到该IP的域名
			filter["$and"] = []bson.M{
				{"$or": baseCondition},
				{"ip.ipv4.ip": bson.M{"$regex": req.IP, "$options": "i"}},
			}
		} else {
			// 无搜索条件，只用基础条件
			filter["$or"] = baseCondition
		}

		// 组织
		if req.OrgId != "" {
			filter["org_id"] = req.OrgId
		}

		// 查询所有匹配的资产
		assets, err := assetModel.Find(l.ctx, filter, 0, 0)
		if err != nil {
			continue
		}

		// 聚合域名信息
		for _, asset := range assets {
			// 确定域名值
			domain := asset.Domain
			if domain == "" {
				domain = asset.Host
			}
			if domain == "" {
				domain = asset.Authority
			}
			if domain == "" || common.IsIPAddress(domain) {
				continue
			}

			if existing, ok := domainMap[domain]; ok {
				// 更新已存在的域名记录 - 添加IP（去重）
				for _, ipv4 := range asset.Ip.IpV4 {
					found := false
					for _, ip := range existing.IPs {
						if ip == ipv4.IPName {
							found = true
							break
						}
					}
					if !found && ipv4.IPName != "" {
						existing.IPs = append(existing.IPs, ipv4.IPName)
					}
				}
			} else {
				// 创建新的域名记录
				rootDomain := common.GetRootDomain(domain)
				ips := []string{}
				for _, ipv4 := range asset.Ip.IpV4 {
					if ipv4.IPName != "" {
						ips = append(ips, ipv4.IPName)
					}
				}

				source := asset.Source
				if source == "" {
					if asset.Category == "domain" {
						source = "subfinder"
					} else {
						source = "scan"
					}
				}

				domainMap[domain] = &types.Domain{
					Id:         asset.Id.Hex(),
					Domain:     domain,
					RootDomain: rootDomain,
					IPs:        ips,
					CName:      asset.CName,
					Source:     source,
					OrgId:      asset.OrgId,
					OrgName:    orgMap[asset.OrgId],
					IsNew:      asset.IsNewAsset,
					CreateTime: asset.CreateTime.Local().Format("2006-01-02 15:04:05"),
				}
			}
		}
	}

	// 转换为列表
	allDomains := make([]types.Domain, 0, len(domainMap))
	for _, d := range domainMap {
		allDomains = append(allDomains, *d)
	}

	// 分页
	total := len(allDomains)
	start := (req.Page - 1) * req.PageSize
	end := start + req.PageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	resp.Total = total
	if start < total {
		resp.List = allDomains[start:end]
	}
	return resp, nil
}

// DomainStat 域名统计
func (l *DomainLogic) DomainStat(workspaceId string) (*types.DomainStatResp, error) {
	resp := &types.DomainStatResp{Code: 0}

	workspaceIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)
	if len(workspaceIds) == 0 {
		return resp, nil
	}

	domainSet := make(map[string]bool)
	rootDomainSet := make(map[string]bool)
	resolvedCount := 0
	newCount := 0

	for _, wsId := range workspaceIds {
		assetModel := model.NewAssetModel(l.svcCtx.MongoDB, wsId)

		// 查询域名类型资产
		filter := bson.M{
			"$or": []bson.M{
				{"category": "domain"},
				{"domain": bson.M{"$exists": true, "$ne": ""}},
				{"source": "subfinder"},
			},
		}

		assets, err := assetModel.Find(l.ctx, filter, 0, 0)
		if err != nil {
			continue
		}

		for _, asset := range assets {
			domain := asset.Domain
			if domain == "" {
				domain = asset.Host
			}
			if domain == "" || common.IsIPAddress(domain) {
				continue
			}

			if !domainSet[domain] {
				domainSet[domain] = true
				rootDomainSet[common.GetRootDomain(domain)] = true

				// 检查是否已解析（有IP）
				if len(asset.Ip.IpV4) > 0 || len(asset.Ip.IpV6) > 0 {
					resolvedCount++
				}

				// 检查是否新增
				if asset.IsNewAsset {
					newCount++
				}
			}
		}
	}

	resp.Total = len(domainSet)
	resp.RootDomainCount = len(rootDomainSet)
	resp.ResolvedCount = resolvedCount
	resp.NewCount = newCount

	return resp, nil
}

// DomainDelete 删除域名（删除该域名对应的所有资产）
func (l *DomainLogic) DomainDelete(req *types.DomainDeleteReq, workspaceId string) (*types.BaseResp, error) {
	workspaceIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)

	// 先通过ID找到域名值
	var domainName string
	for _, wsId := range workspaceIds {
		assetModel := model.NewAssetModel(l.svcCtx.MongoDB, wsId)
		asset, err := assetModel.FindById(l.ctx, req.Id)
		if err == nil && asset != nil {
			// 获取域名值
			domainName = asset.Domain
			if domainName == "" {
				domainName = asset.Host
			}
			if domainName == "" {
				domainName = asset.Authority
			}
			break
		}
	}

	if domainName == "" || common.IsIPAddress(domainName) {
		return &types.BaseResp{Code: 500, Msg: "删除失败，域名不存在"}, nil
	}

	// 删除所有包含该域名的资产
	var totalDeleted int64
	for _, wsId := range workspaceIds {
		assetModel := model.NewAssetModel(l.svcCtx.MongoDB, wsId)
		// 构建查询条件：匹配domain、host或authority等于该域名的资产
		filter := bson.M{
			"$or": []bson.M{
				{"domain": domainName},
				{"host": domainName},
				{"authority": bson.M{"$regex": "^" + domainName + "(:|$)"}},
			},
		}
		deleted, _ := assetModel.DeleteByFilter(l.ctx, filter)
		totalDeleted += deleted
	}

	if totalDeleted == 0 {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// DomainBatchDelete 批量删除域名
func (l *DomainLogic) DomainBatchDelete(req *types.DomainBatchDeleteReq, workspaceId string) (*types.BaseResp, error) {
	if len(req.Ids) == 0 {
		return &types.BaseResp{Code: 400, Msg: "请选择要删除的域名"}, nil
	}

	workspaceIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)

	// 先收集所有要删除的域名值
	domainNames := make(map[string]bool)
	for _, wsId := range workspaceIds {
		assetModel := model.NewAssetModel(l.svcCtx.MongoDB, wsId)
		for _, id := range req.Ids {
			asset, err := assetModel.FindById(l.ctx, id)
			if err == nil && asset != nil {
				domainName := asset.Domain
				if domainName == "" {
					domainName = asset.Host
				}
				if domainName == "" {
					domainName = asset.Authority
				}
				if domainName != "" && !common.IsIPAddress(domainName) {
					domainNames[domainName] = true
				}
			}
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
	var totalDeleted int64
	for _, wsId := range workspaceIds {
		assetModel := model.NewAssetModel(l.svcCtx.MongoDB, wsId)
		filter := bson.M{
			"$or": []bson.M{
				{"domain": bson.M{"$in": domains}},
				{"host": bson.M{"$in": domains}},
			},
		}
		deleted, _ := assetModel.DeleteByFilter(l.ctx, filter)
		totalDeleted += deleted
	}

	if totalDeleted == 0 {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(int64(len(domainNames)), 10) + " 个域名"}, nil
}
