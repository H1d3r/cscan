package logic

import (
	"context"
	"regexp"
	"sort"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type AssetFilterOptionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetFilterOptionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetFilterOptionsLogic {
	return &AssetFilterOptionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetFilterOptions 获取资产过滤器选项
// 优化点：
//  1. 用 LocalCache 缓存结果（60s TTL + singleflight 防击穿）
//  2. 用 DB 端 distinct 命令替代全表加载到内存 distinct
//  3. 仅取必要字段（app/port/http_status/labels），避免拉 body/header/screenshot 等大字段
func (l *AssetFilterOptionsLogic) AssetFilterOptions(req *types.AssetFilterOptionsReq) (resp *types.AssetFilterOptionsResp, err error) {
	cacheKey := "asset_filter_opts:" + req.Domain + ":" + boolToStr(req.HasScreenshot)

	cached, err := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, 60*time.Second, func() (interface{}, error) {
		return l.loadFilterOptions(req)
	})
	if err != nil {
		l.Logger.Errorf("AssetFilterOptions查询失败: %v", err)
		return &types.AssetFilterOptionsResp{Code: 500, Msg: "查询失败"}, nil
	}

	result, ok := cached.(*types.AssetFilterOptionsResp)
	if !ok {
		return &types.AssetFilterOptionsResp{Code: 500, Msg: "查询失败"}, nil
	}
	return result, nil
}

func (l *AssetFilterOptionsLogic) loadFilterOptions(req *types.AssetFilterOptionsReq) (*types.AssetFilterOptionsResp, error) {
	// 构建查询条件
	filter := bson.M{}
	if req.Domain != "" {
		filter["host"] = bson.M{"$regex": regexp.QuoteMeta(req.Domain), "$options": "i"}
	}
	if req.HasScreenshot {
		filter["screenshot"] = bson.M{"$ne": ""}
	}

	// 技术选项按归一化键折叠（剥掉 [来源] 后缀与 :版本号），值取展示名；
	// 展示名作为 regex 过滤条件可同时命中带后缀/版本的原始条目
	techSet := make(map[string]struct{})
	portSet := make(map[int]struct{})
	statusSet := make(map[string]struct{})
	labelSet := make(map[string]struct{})

	assetModel := l.svcCtx.GetAssetModel()

	if values, err := assetModel.Distinct(l.ctx, "app", filter); err == nil {
		seenKeys := make(map[string]struct{})
		for _, v := range values {
			s, ok := v.(string)
			if !ok || s == "" {
				continue
			}
			key := model.NormalizeAppKey(s)
			if key == "" {
				continue
			}
			if _, dup := seenKeys[key]; dup {
				continue
			}
			seenKeys[key] = struct{}{}
			techSet[model.AppDisplayName(s)] = struct{}{}
		}
	}

	if values, err := assetModel.Distinct(l.ctx, "port", filter); err == nil {
		for _, v := range values {
			if i, ok := v.(int32); ok && i > 0 {
				portSet[int(i)] = struct{}{}
			} else if i, ok := v.(int64); ok && i > 0 {
				portSet[int(i)] = struct{}{}
			} else if i, ok := v.(int); ok && i > 0 {
				portSet[i] = struct{}{}
			}
		}
	}

	if values, err := assetModel.Distinct(l.ctx, "status", filter); err == nil {
		for _, v := range values {
			// "0" 是扫描器记录的无效状态码（网关异常页等），不作为过滤选项
			if s, ok := v.(string); ok && s != "" && s != "0" {
				statusSet[s] = struct{}{}
			}
		}
	}

	if values, err := assetModel.Distinct(l.ctx, "labels", filter); err == nil {
		for _, v := range values {
			switch val := v.(type) {
			case string:
				if val != "" {
					labelSet[val] = struct{}{}
				}
			case primitive.A:
				for _, item := range val {
					if s, ok := item.(string); ok && s != "" {
						labelSet[s] = struct{}{}
					}
				}
			}
		}
	}

	technologies := make([]string, 0, len(techSet))
	for tech := range techSet {
		technologies = append(technologies, tech)
	}
	sort.Strings(technologies)

	ports := make([]int, 0, len(portSet))
	for port := range portSet {
		ports = append(ports, port)
	}
	sort.Ints(ports)

	statusCodes := make([]string, 0, len(statusSet))
	for status := range statusSet {
		statusCodes = append(statusCodes, status)
	}
	sort.Strings(statusCodes)

	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	return &types.AssetFilterOptionsResp{
		Code:         0,
		Msg:          "success",
		Technologies: technologies,
		Ports:        ports,
		StatusCodes:  statusCodes,
		Labels:       labels,
	}, nil
}

func boolToStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
