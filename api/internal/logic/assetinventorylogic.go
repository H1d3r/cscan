package logic

import (
	"context"
	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type AssetInventoryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetInventoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetInventoryLogic {
	return &AssetInventoryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetInventory 获取资产清单
func (l *AssetInventoryLogic) AssetInventory(req *types.AssetInventoryReq, workspaceId string) (resp *types.AssetInventoryResp, err error) {
	l.Logger.Infof("AssetInventory查询: workspaceId=%s, page=%d, pageSize=%d", workspaceId, req.Page, req.PageSize)

	// 获取需要查询的工作空间列表
	wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, workspaceId)
	l.Logger.Infof("AssetInventory查询工作空间列表: %v", wsIds)

	// 用于存储所有资产
	allAssets := make([]types.AssetInventoryItem, 0)

	// 遍历所有工作空间
	for _, wsId := range wsIds {
		assetModel := l.svcCtx.GetAssetModel(wsId)

		// 构建查询条件
		filter := bson.M{}

		// 搜索关键词
		if req.Query != "" {
			filter["$or"] = []bson.M{
				{"host": bson.M{"$regex": req.Query, "$options": "i"}},
				{"title": bson.M{"$regex": req.Query, "$options": "i"}},
			}
		}

		// 域名过滤
		if req.Domain != "" {
			filter["host"] = bson.M{"$regex": req.Domain, "$options": "i"}
		}

		// 端口过滤
		if len(req.Ports) > 0 {
			filter["port"] = bson.M{"$in": req.Ports}
		}

		// 状态码过滤
		if len(req.StatusCodes) > 0 {
			filter["status"] = bson.M{"$in": req.StatusCodes}
		}

		// 标签过滤
		if len(req.Labels) > 0 {
			filter["labels"] = bson.M{"$in": req.Labels}
		}

		// 技术栈过滤
		if len(req.Technologies) > 0 {
			// 使用正则表达式匹配技术栈（不区分大小写）
			techFilters := make([]bson.M, 0, len(req.Technologies))
			for _, tech := range req.Technologies {
				techFilters = append(techFilters, bson.M{
					"app": bson.M{"$regex": tech, "$options": "i"},
				})
			}
			if len(techFilters) > 0 {
				if existingOr, ok := filter["$or"]; ok {
					// 如果已经有$or条件，需要合并
					filter["$and"] = []bson.M{
						{"$or": existingOr},
						{"$or": techFilters},
					}
					delete(filter, "$or")
				} else {
					filter["$or"] = techFilters
				}
			}
		}

		// 时间范围过滤
		if req.TimeRange != "" && req.TimeRange != "all" {
			now := time.Now()
			var startTime time.Time
			switch req.TimeRange {
			case "24h":
				startTime = now.Add(-24 * time.Hour)
			case "7d":
				startTime = now.Add(-7 * 24 * time.Hour)
			case "30d":
				startTime = now.Add(-30 * 24 * time.Hour)
			}
			if !startTime.IsZero() {
				filter["update_time"] = bson.M{"$gte": startTime}
			}
		}

		// 查询资产
		assets, err := assetModel.Find(l.ctx, filter, 0, 0)
		if err != nil {
			l.Logger.Errorf("查询工作空间 %s 资产失败: %v", wsId, err)
			continue
		}

		// 转换为清单格式
		for _, asset := range assets {
			// 获取第一个IP地址
			ip := ""
			if len(asset.Ip.IpV4) > 0 {
				ip = asset.Ip.IpV4[0].IPName
			} else if len(asset.Ip.IpV6) > 0 {
				ip = asset.Ip.IpV6[0].IPName
			}

			// 将 IconHashBytes 转换为 Base64 字符串
			iconHashBytes := ""
			if len(asset.IconHashBytes) > 0 {
				iconHashBytes = base64.StdEncoding.EncodeToString(asset.IconHashBytes)
			}

			// 确保 Labels 不为 nil
			labels := asset.Labels
			if labels == nil {
				labels = []string{}
			}

			item := types.AssetInventoryItem{
				Id:              asset.Id.Hex(),
				WorkspaceId:     wsId,
				Host:            asset.Host,
				IP:              ip,
				Port:            asset.Port,
				Service:         asset.Service,
				Title:           asset.Title,
				Technologies:    asset.App,
				Labels:          labels,
				Status:          asset.HttpStatus,
				LastUpdated:     formatTimeAgo(asset.UpdateTime),
				FirstSeen:       asset.CreateTime.Local().Format("2006-01-02 15:04:05"),
				LastUpdatedFull: asset.UpdateTime.Local().Format("2006-01-02 15:04:05"),
				Screenshot:      asset.Screenshot,
				IconHash:        asset.IconHash,
				IconHashBytes:   iconHashBytes,
				HttpHeader:      asset.HttpHeader,
				HttpBody:        asset.HttpBody,
				Banner:          asset.Banner,
				CName:           asset.CName,
			}
			allAssets = append(allAssets, item)
		}
	}

	// 排序
	sortAssets(allAssets, req.SortBy)

	// 分页
	total := len(allAssets)
	start := (req.Page - 1) * req.PageSize
	end := start + req.PageSize

	if start >= total {
		allAssets = []types.AssetInventoryItem{}
	} else {
		if end > total {
			end = total
		}
		allAssets = allAssets[start:end]
	}

	return &types.AssetInventoryResp{
		Code:  0,
		Msg:   "success",
		Total: total,
		List:  allAssets,
	}, nil
}

// sortAssets 对资产进行排序
func sortAssets(assets []types.AssetInventoryItem, sortBy string) {
	if sortBy == "name" {
		// 按主机名排序
		for i := 0; i < len(assets)-1; i++ {
			for j := i + 1; j < len(assets); j++ {
				if strings.ToLower(assets[i].Host) > strings.ToLower(assets[j].Host) {
					assets[i], assets[j] = assets[j], assets[i]
				}
			}
		}
	} else if sortBy == "port" {
		// 按端口排序
		for i := 0; i < len(assets)-1; i++ {
			for j := i + 1; j < len(assets); j++ {
				if assets[i].Port > assets[j].Port {
					assets[i], assets[j] = assets[j], assets[i]
				}
			}
		}
	}
	// 默认按时间排序（已经是最新的在前）
}

// formatTimeAgo 格式化相对时间
func formatTimeAgo(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "刚刚"
	} else if diff < time.Hour {
		return fmt.Sprintf("%d分钟前", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		return fmt.Sprintf("%d小时前", int(diff.Hours()))
	} else {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1天前"
		}
		return fmt.Sprintf("%d天前", days)
	}
}
