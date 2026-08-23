package logic

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type ScreenshotsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewScreenshotsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScreenshotsLogic {
	return &ScreenshotsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Screenshots 获取截图清单
func (l *ScreenshotsLogic) Screenshots(req *types.ScreenshotsReq) (resp *types.ScreenshotsResp, err error) {
	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
	l.Logger.Infof("Screenshots查询: page=%d, pageSize=%d", req.Page, req.PageSize)

	// 用于存储所有截图
	allScreenshots := make([]types.ScreenshotItem, 0)

	assetModel := l.svcCtx.GetAssetModel()

	// 构建查询条件
	filter := bson.M{}

	// 只查询有截图的资产（如果指定）
	if req.HasScreenshot {
		filter["screenshot"] = bson.M{"$ne": "", "$exists": true}
	}

	// 搜索关键词
	if req.Query != "" {
		q := regexp.QuoteMeta(req.Query)
		filter["$or"] = []bson.M{
			{"host": bson.M{"$regex": q, "$options": "i"}},
			{"title": bson.M{"$regex": q, "$options": "i"}},
		}
	}

	// 域名过滤
	if req.Domain != "" {
		filter["host"] = bson.M{"$regex": regexp.QuoteMeta(req.Domain), "$options": "i"}
	}

	// 端口过滤
	if len(req.Ports) > 0 {
		filter["port"] = bson.M{"$in": req.Ports}
	}

	// 状态码过滤
	if len(req.StatusCodes) > 0 {
		filter["status"] = bson.M{"$in": req.StatusCodes}
	}

	// 技术栈过滤
	if len(req.Technologies) > 0 {
		techFilters := make([]bson.M, 0, len(req.Technologies))
		for _, tech := range req.Technologies {
			techFilters = append(techFilters, bson.M{
				"app": bson.M{"$regex": regexp.QuoteMeta(tech), "$options": "i"},
			})
		}
		if len(techFilters) > 0 {
			if existingOr, ok := filter["$or"]; ok {
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
	assets, err := assetModel.FindWithScreenshot(l.ctx, filter, 0, 0)
	if err != nil {
		l.Logger.Errorf("查询资产失败: %v", err)
	}

	// 转换为截图格式
	for _, asset := range assets {
		// 获取首个IP及所有IP列表
		ip := ""
		var ips []string
		if true {
			if len(asset.Ip.IpV4) > 0 {
				ip = asset.Ip.IpV4[0].IPName
			} else if len(asset.Ip.IpV6) > 0 {
				ip = asset.Ip.IpV6[0].IPName
			}

			for _, v4 := range asset.Ip.IpV4 {
				ips = append(ips, v4.IPName)
			}
			for _, v6 := range asset.Ip.IpV6 {
				ips = append(ips, v6.IPName)
			}
		}

		// 转换技术栈（同一技术的来源后缀变体折叠为一条展示）
		dedupedApps := model.MergeAppsDedup(nil, asset.App)
		technologies := make([]types.Technology, 0, len(dedupedApps))
		for _, app := range dedupedApps {
			technologies = append(technologies, types.Technology{Name: app})
		}

		// 获取状态文本
		statusText := getStatusText(asset.HttpStatus)

		item := types.ScreenshotItem{
			Id:           asset.Id.Hex(),
			Name:         asset.Host,
			Port:         asset.Port,
			IP:           ip,
			Ips:          ips,
			Status:       asset.HttpStatus,
			StatusText:   statusText,
			Title:        asset.Title,
			Screenshot:   asset.Screenshot,
			CreateTime:   formatScreenshotTime(asset.CreateTime),
			UpdateTime:   formatScreenshotTime(asset.UpdateTime),
			LastUpdated:  formatScreenshotTime(asset.UpdateTime),
			Technologies: technologies,
			HttpHeader:   asset.HttpHeader,
			HttpBody:     asset.HttpBody,
			Service:      asset.Service,
			Authority:    asset.Authority,
		}
		allScreenshots = append(allScreenshots, item)
	}

	// 排序
	sortScreenshots(allScreenshots, req.SortBy)

	// 分页
	total := len(allScreenshots)
	start := (req.Page - 1) * req.PageSize
	end := start + req.PageSize

	if start >= total {
		allScreenshots = []types.ScreenshotItem{}
	} else {
		if end > total {
			end = total
		}
		allScreenshots = allScreenshots[start:end]
	}

	return &types.ScreenshotsResp{
		Code:  0,
		Msg:   "success",
		Total: total,
		List:  allScreenshots,
	}, nil
}

// sortScreenshots 对截图进行排序
func sortScreenshots(screenshots []types.ScreenshotItem, sortBy string) {
	if sortBy == "name" {
		// 按主机名排序
		for i := 0; i < len(screenshots)-1; i++ {
			for j := i + 1; j < len(screenshots); j++ {
				if strings.ToLower(screenshots[i].Name) > strings.ToLower(screenshots[j].Name) {
					screenshots[i], screenshots[j] = screenshots[j], screenshots[i]
				}
			}
		}
	}
	// 默认按时间排序（已经是最新的在前）
}

// formatScreenshotTime 格式化截图时间
func formatScreenshotTime(t time.Time) string {
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

// getStatusText 获取状态文本
func getStatusText(status string) string {
	statusMap := map[string]string{
		"200": "OK",
		"201": "Created",
		"204": "No Content",
		"301": "Moved Permanently",
		"302": "Found",
		"304": "Not Modified",
		"400": "Bad Request",
		"401": "Unauthorized",
		"403": "Forbidden",
		"404": "Not Found",
		"500": "Internal Server Error",
		"502": "Bad Gateway",
		"503": "Service Unavailable",
	}

	if text, ok := statusMap[status]; ok {
		return text
	}
	return ""
}
