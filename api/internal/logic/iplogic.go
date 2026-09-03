package logic

import (
	"context"
	"regexp"
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

	assetModel := l.svcCtx.GetAssetModel()

	// 构建查询条件
	filter := bson.M{}
	conditions := []bson.M{}

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

	// 分页参数
	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)

	// 服务端聚合（$group by IP + $facet 分页），避免全量加载资产到内存
	aggResults, total, err := assetModel.AggregateIPs(l.ctx, filter, req.Page, req.PageSize)
	if err != nil {
		l.Logger.Errorf("IPList 聚合查询失败: %v", err)
		return resp, nil
	}

	// 转换为响应类型
	list := make([]types.IPAsset, 0, len(aggResults))
	for _, r := range aggResults {
		ports := make([]types.PortInfo, 0, len(r.Ports))
		for _, p := range r.Ports {
			ports = append(ports, types.PortInfo{Port: p.Port, Service: p.Service})
		}
		list = append(list, types.IPAsset{
			Id:          r.Id.Hex(),
			IP:          r.IP,
			Location:    r.Location,
			Ports:       ports,
			Domains:     r.Domains,
			DomainCount: len(r.Domains),
			OrgId:       r.OrgId,
			OrgName:     orgMap[r.OrgId],
			CreateTime:  r.CreateTime.Local().Format("2006-01-02 15:04:05"),
			UpdateTime:  r.UpdateTime.Local().Format("2006-01-02 15:04:05"),
			IsNew:       r.IsNew,
		})
	}

	resp.Total = total
	resp.List = list
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
