package logic

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"time"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type IPLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewIPLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IPLogic {
	return &IPLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// IPList IP列表 - 从资产中聚合IP信息
// 显示所有有IP的资产，按IP聚合端口和域名信息
func (l *IPLogic) IPList(req *types.IPListReq) (*types.IPListResp, error) {
	resp := &types.IPListResp{Code: 0, List: []types.IPAsset{}}

	orgMap := common.LoadOrgMap(l.ctx, l.svcCtx)

	// 用于聚合IP信息
	ipMap := make(map[string]*types.IPAsset)

	assetModel := l.svcCtx.GetAssetModel()

	// 构建查询条件 - 查询有IP的资产
	// IP来源：host字段是IP 或 ip.ipv4有值
	filter := bson.M{}
	conditions := []bson.M{}

	// 基础条件：有IP的资产
	// 不加基础条件，查询所有资产然后提取IP

	// 最上方筛选 (Query)
	if req.Query != "" {
		q := regexp.QuoteMeta(req.Query)
		conditions = append(conditions, bson.M{
			"$or": []bson.M{
				{"host": bson.M{"$regex": q, "$options": "i"}},
				{"ip.ipv4.ip": bson.M{"$regex": q, "$options": "i"}},
			},
		})
	}

	// IP搜索
	if req.IP != "" {
		ipQuery := regexp.QuoteMeta(req.IP)
		conditions = append(conditions, bson.M{
			"$or": []bson.M{
				{"host": bson.M{"$regex": ipQuery, "$options": "i"}},
				{"ip.ipv4.ip": bson.M{"$regex": ipQuery, "$options": "i"}},
			},
		})
	}
	// 端口搜索
	if req.Port != "" {
		if port, err := strconv.Atoi(req.Port); err == nil {
			conditions = append(conditions, bson.M{"port": port})
		}
	}
	// 服务搜索
	if req.Service != "" {
		conditions = append(conditions, bson.M{"service": bson.M{"$regex": regexp.QuoteMeta(req.Service), "$options": "i"}})
	}
	// 位置搜索
	if req.Location != "" {
		conditions = append(conditions, bson.M{"ip.ipv4.location": bson.M{"$regex": regexp.QuoteMeta(req.Location), "$options": "i"}})
	}
	// 组织
	if req.OrgId != "" {
		conditions = append(conditions, bson.M{"org_id": req.OrgId})
	}

	if len(conditions) > 0 {
		filter["$and"] = conditions
	}

	// 查询所有匹配的资产做内存去重聚合。走 FindAllForAgg 全量查询 + AssetAggProjection 瘦投影：
	// FindWithSort 会被 NormalizePage 把 pageSize 钳到 100，只取最新 100 条资产聚合会漏 IP。
	assets, err := assetModel.FindAllForAgg(l.ctx, filter)
	if err != nil {
		l.Logger.Errorf("IPList 查询资产失败: %v", err)
		return resp, nil
	}

	// 聚合IP信息
	for _, asset := range assets {
		// 收集所有IP地址
		var ips []string
		var location string

		// 从ip.ipv4字段获取IP
		for _, ipv4 := range asset.Ip.IpV4 {
			if ipv4.IPName != "" {
				ips = append(ips, ipv4.IPName)
				if location == "" && ipv4.Location != "" {
					location = ipv4.Location
				}
			}
		}

		// 如果host是IP地址，也加入
		if common.IsIPAddress(asset.Host) && asset.Host != "" {
			found := false
			for _, ip := range ips {
				if ip == asset.Host {
					found = true
					break
				}
			}
			if !found {
				ips = append(ips, asset.Host)
			}
		}

		// 如果没有IP，跳过
		if len(ips) == 0 {
			continue
		}

		// 为每个IP创建或更新记录
		for _, ip := range ips {
			if existing, ok := ipMap[ip]; ok {
				// 更新已存在的IP记录
				// 添加端口（去重）
				if asset.Port > 0 {
					portFound := false
					for _, p := range existing.Ports {
						if p.Port == asset.Port {
							portFound = true
							break
						}
					}
					if !portFound {
						existing.Ports = append(existing.Ports, types.PortInfo{
							Port:    asset.Port,
							Service: asset.Service,
						})
					}
				}

				// 添加域名（去重）
				domain := asset.Domain
				if domain == "" && !common.IsIPAddress(asset.Host) {
					domain = asset.Host
				}
				if domain != "" {
					domainFound := false
					for _, d := range existing.Domains {
						if d == domain {
							domainFound = true
							break
						}
					}
					if !domainFound {
						existing.Domains = append(existing.Domains, domain)
						existing.DomainCount = len(existing.Domains)
					}
				}

				// 更新位置信息
				if existing.Location == "" && location != "" {
					existing.Location = location
				}

				// 更新时间取最新
				if assetUpdate := asset.UpdateTime.Local().Format("2006-01-02 15:04:05"); assetUpdate > existing.UpdateTime {
					existing.UpdateTime = assetUpdate
				}
				// 创建时间取最早
				if assetCreate := asset.CreateTime.Local().Format("2006-01-02 15:04:05"); existing.CreateTime == "" || assetCreate < existing.CreateTime {
					existing.CreateTime = assetCreate
				}
			} else {
				// 创建新的IP记录
				ports := []types.PortInfo{}
				if asset.Port > 0 {
					ports = append(ports, types.PortInfo{
						Port:    asset.Port,
						Service: asset.Service,
					})
				}

				domains := []string{}
				domain := asset.Domain
				if domain == "" && !common.IsIPAddress(asset.Host) {
					domain = asset.Host
				}
				if domain != "" {
					domains = append(domains, domain)
				}

				ipMap[ip] = &types.IPAsset{
					Id:          asset.Id.Hex(),
					IP:          ip,
					Location:    location,
					Ports:       ports,
					Domains:     domains,
					DomainCount: len(domains),
					OrgId:       asset.OrgId,
					OrgName:     orgMap[asset.OrgId],
					CreateTime:  asset.CreateTime.Local().Format("2006-01-02 15:04:05"),
					UpdateTime:  asset.UpdateTime.Local().Format("2006-01-02 15:04:05"),
					IsNew:       asset.IsNewAsset,
				}
			}
		}
	}

	// 转换为列表并排序
	allIPs := make([]types.IPAsset, 0, len(ipMap))
	for _, ip := range ipMap {
		allIPs = append(allIPs, *ip)
	}

	// 按端口数量降序排序
	sort.Slice(allIPs, func(i, j int) bool {
		return len(allIPs[i].Ports) > len(allIPs[j].Ports)
	})

	// 分页
	total := len(allIPs)
	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
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
		resp.List = allIPs[start:end]
	}
	return resp, nil
}

// IPStat IP统计
// 优化点：原实现全表加载所有资产到内存只为 distinct IP/port/service/newIPs，且每个 ws 一次全表 scan
// 现改用：
//  1. DB 端 distinct 命令替代内存 distinct（port/service 直接 distinct 字段）
//  2. IP 仍需内存聚合（来源 ip.ipv4.ip + host），但用 FindWithSort+Projection 限制字段
//  3. 整体结果走 60s 缓存（统计数据不需要实时）
func (l *IPLogic) IPStat() (*types.IPStatResp, error) {
	cacheKey := "ip_stat"
	cached, err := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, 60*time.Second, func() (interface{}, error) {
		resp := &types.IPStatResp{Code: 0}

		ipSet := make(map[string]bool)
		portSet := make(map[int]bool)
		serviceSet := make(map[string]bool)
		newIPs := make(map[string]bool)

		assetModel := l.svcCtx.GetAssetModel()

		// 用 DB 端 distinct 替代内存 distinct（port/service）
		if values, err := assetModel.Distinct(l.ctx, "port", nil); err == nil {
			for _, v := range values {
				if i, ok := v.(int32); ok && i > 0 {
					portSet[int(i)] = true
				} else if i, ok := v.(int64); ok && i > 0 {
					portSet[int(i)] = true
				} else if i, ok := v.(int); ok && i > 0 {
					portSet[i] = true
				}
			}
		}
		if values, err := assetModel.Distinct(l.ctx, "service", nil); err == nil {
			for _, v := range values {
				if s, ok := v.(string); ok && s != "" {
					serviceSet[s] = true
				}
			}
		}

		// IP 需要聚合 ip.ipv4.ip + host(IP)，全量查询 + 瘦投影（FindWithSort 会被钳到 100 条漏统计）
		assets, err := assetModel.FindAllForAgg(l.ctx, bson.M{})
		if err != nil {
			l.Logger.Errorf("IPStat 查询资产失败: %v", err)
			return resp, nil
		}

		for _, asset := range assets {
			var ips []string
			for _, ipv4 := range asset.Ip.IpV4 {
				if ipv4.IPName != "" {
					ips = append(ips, ipv4.IPName)
				}
			}
			if common.IsIPAddress(asset.Host) && asset.Host != "" {
				found := false
				for _, ip := range ips {
					if ip == asset.Host {
						found = true
						break
					}
				}
				if !found {
					ips = append(ips, asset.Host)
				}
			}

			firstSeen := asset.FirstSeenTime
			if firstSeen.IsZero() {
				firstSeen = asset.CreateTime
			}
			isNewAsset := !firstSeen.Before(time.Now().AddDate(0, 0, -1))

			for _, ip := range ips {
				if !ipSet[ip] {
					ipSet[ip] = true
					if isNewAsset {
						newIPs[ip] = true
					}
				}
			}
		}

		resp.Total = len(ipSet)
		resp.PortCount = len(portSet)
		resp.ServiceCount = len(serviceSet)
		resp.NewCount = len(newIPs)

		return resp, nil
	})
	if err != nil {
		return &types.IPStatResp{Code: 0}, nil
	}
	if r, ok := cached.(*types.IPStatResp); ok {
		return r, nil
	}
	return &types.IPStatResp{Code: 0}, nil
}

// IPDelete 删除IP（删除该IP下所有资产）
// 优化点：原实现先 Find 再 BatchDelete（两次查询）；改直接 DeleteByFilter（一次删除）
func (l *IPLogic) IPDelete(req *types.IPDeleteReq) (*types.BaseResp, error) {
	if req.IP == "" {
		return &types.BaseResp{Code: 400, Msg: "IP不能为空"}, nil
	}

	filter := bson.M{
		"$or": []bson.M{
			{"host": req.IP},
			{"ip.ipv4.ip": req.IP},
		},
	}

	assetModel := l.svcCtx.GetAssetModel()
	deleted, err := assetModel.DeleteByFilter(l.ctx, filter)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(deleted, 10) + " 条资产"}, nil
}

// IPBatchDelete 批量删除IP
func (l *IPLogic) IPBatchDelete(req *types.IPBatchDeleteReq) (*types.BaseResp, error) {
	if len(req.IPs) == 0 {
		return &types.BaseResp{Code: 400, Msg: "请选择要删除的IP"}, nil
	}

	filter := bson.M{
		"$or": []bson.M{
			{"host": bson.M{"$in": req.IPs}},
			{"ip.ipv4.ip": bson.M{"$in": req.IPs}},
		},
	}

	assetModel := l.svcCtx.GetAssetModel()
	deleted, err := assetModel.DeleteByFilter(l.ctx, filter)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(deleted, 10) + " 条资产"}, nil
}
