package logic

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// DashboardSummaryLogic Dashboard 统一汇总（替代前端 10 个并发请求）
type DashboardSummaryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDashboardSummaryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DashboardSummaryLogic {
	return &DashboardSummaryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DashboardSummary 返回 Dashboard 首屏所需的全部统计数据
func (l *DashboardSummaryLogic) DashboardSummary() (*types.DashboardSummaryResp, error) {
	cacheKey := "dashboard_summary"
	cached, err := l.svcCtx.QueryCache.GetOrSetWithTTL(cacheKey, 60*time.Second, func() (interface{}, error) {
		return l.buildSummary()
	})
	if err != nil {
		return l.buildSummary()
	}
	if r, ok := cached.(*types.DashboardSummaryResp); ok {
		return r, nil
	}
	return l.buildSummary()
}

func (l *DashboardSummaryLogic) buildSummary() (*types.DashboardSummaryResp, error) {
	resp := &types.DashboardSummaryResp{
		Code: 0,
		Msg:  "success",
	}

	assetModel := l.svcCtx.GetAssetModel()
	vulModel := l.svcCtx.GetVulModel()
	taskModel := l.svcCtx.GetMainTaskModel()

	// 1. 资产概览（复用 AggregateOverviewStats）
	overview, _ := assetModel.AggregateOverviewStats(l.ctx)
	if overview != nil {
		resp.AssetTotal = int(overview.TotalAsset)
		resp.AssetNew = int(overview.NewCount)
	}

	// 2. 去重端口数
	portCount, _ := assetModel.DistinctPortCount(l.ctx)
	resp.PortCount = int(portCount)

	// 3. IP/域名/站点统计
	ipCount, _ := assetModel.Count(l.ctx, bson.M{"category": "ip"})
	resp.IPCount = int(ipCount)

	domainCount, _ := assetModel.Count(l.ctx, bson.M{"category": "domain"})
	resp.DomainCount = int(domainCount)

	siteCount, _ := assetModel.Count(l.ctx, bson.M{
		"$or": []bson.M{
			{"is_http": true},
			{"title": bson.M{"$exists": true, "$ne": ""}},
			{"status": bson.M{"$exists": true, "$nin": []string{"", "0"}}},
		},
	})
	resp.SiteCount = int(siteCount)

	// 4. 漏洞统计（复用 AggregateStats）
	now := time.Now()
	vulStats, _ := vulModel.AggregateStats(l.ctx, now)
	if vulStats != nil {
		resp.VulnTotal = int(vulStats.Total)
		resp.VulnOpen = int(vulStats.Open)
		resp.VulnFixed = int(vulStats.Fixed)
		resp.VulnIgnored = int(vulStats.Ignored)
		resp.VulnOpenCritical = int(vulStats.OpenCritical)
		resp.VulnOpenHigh = int(vulStats.OpenHigh)
		resp.VulnOpenMedium = int(vulStats.OpenMedium)
		resp.VulnOpenLow = int(vulStats.OpenLow)
		resp.VulnOpenInfo = int(vulStats.OpenInfo)
	}

	// 5. 任务统计（一次性聚合替代 19 次 Count）
	taskPipeline := []bson.M{
		{
			"$group": bson.M{
				"_id":    "$status",
				"count":  bson.M{"$sum": 1},
				"total":  bson.M{"$sum": 1},
				"latest": bson.M{"$max": "$update_time"},
			},
		},
	}
	taskResults, err := taskModel.Collection().Aggregate(l.ctx, taskPipeline)
	if err == nil {
		var total int64
		for taskResults.Next(l.ctx) {
			var result struct {
				ID     string `bson:"_id"`
				Count  int64  `bson:"count"`
				Total  int64  `bson:"total"`
				Latest time.Time `bson:"latest"`
			}
			if err := taskResults.Decode(&result); err == nil {
				total = result.Total
				switch result.ID {
				case model.TaskStatusSuccess:
					resp.TaskCompleted = int(result.Count)
				case model.TaskStatusStarted:
					resp.TaskRunning = int(result.Count)
				case model.TaskStatusFailure:
					resp.TaskFailed = int(result.Count)
				default:
					if result.ID == model.TaskStatusPending || result.ID == model.TaskStatusCreated {
						resp.TaskPending = int(result.Count)
					}
				}
			}
		}
		resp.TaskTotal = int(total)
	}

	// 6. 近 7 天趋势（一次性聚合）
	trendDays := make([]string, 7)
	trendCompleted := make([]int, 7)
	trendFailed := make([]int, 7)
	now = time.Now()
	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		dayEnd := dayStart.AddDate(0, 0, 1)
		idx := 6 - i
		trendDays[idx] = day.Format("01-02")

		trendPipeline := []bson.M{
			{"$match": bson.M{
				"status":      bson.M{"$in": []string{model.TaskStatusSuccess, model.TaskStatusFailure}},
				"update_time": bson.M{"$gte": dayStart, "$lt": dayEnd},
			}},
			{"$group": bson.M{
				"_id":   "$status",
				"count": bson.M{"$sum": 1},
			}},
		}
		trendResults, err := taskModel.Collection().Aggregate(l.ctx, trendPipeline)
		if err == nil {
			for trendResults.Next(l.ctx) {
				var tr struct {
					ID    string `bson:"_id"`
					Count int64  `bson:"count"`
				}
				if err := trendResults.Decode(&tr); err == nil {
					if tr.ID == model.TaskStatusSuccess {
						trendCompleted[idx] = int(tr.Count)
					} else if tr.ID == model.TaskStatusFailure {
						trendFailed[idx] = int(tr.Count)
					}
				}
			}
		}
	}
	resp.TaskTrendDays = trendDays
	resp.TaskTrendCompleted = trendCompleted
	resp.TaskTrendFailed = trendFailed

	// 7. Worker 统计（从 Redis 获取）
	workerKeys := l.svcCtx.RedisClient.Keys(l.ctx, "cscan:worker:*").Val()
	var workerOnline, workerOffline int
	for _, key := range workerKeys {
		if key == "cscan:worker:install_key" ||
			strings.Contains(key, ":control:") ||
			strings.Contains(key, ":register:") {
			continue
		}
		data, err := l.svcCtx.RedisClient.Get(l.ctx, key).Result()
		if err != nil {
			continue
		}
		var status struct {
			UpdateTime string `json:"updateTime"`
		}
		if err := json.Unmarshal([]byte(data), &status); err != nil {
			continue
		}
		if status.UpdateTime == "" {
			workerOffline++
			continue
		}
		loc := time.Local
		updateTime, err := time.ParseInLocation("2006-01-02 15:04:05", status.UpdateTime, loc)
		if err == nil && time.Since(updateTime) < 45*time.Second {
			workerOnline++
		} else {
			workerOffline++
		}
	}
	resp.WorkerOnline = workerOnline
	resp.WorkerOffline = workerOffline

	// 8. Top 数据（复用 asset stat 的聚合逻辑）
	portStats, _ := assetModel.AggregatePort(l.ctx, 10)
	for _, s := range portStats {
		resp.TopPorts = append(resp.TopPorts, types.StatItem{
			Name:  intToStr(s.Port),
			Count: s.Count,
		})
	}

	serviceStats, _ := assetModel.Aggregate(l.ctx, "service", 10)
	for _, s := range serviceStats {
		resp.TopService = append(resp.TopService, types.StatItem{
			Name:  s.Field,
			Count: s.Count,
		})
	}

	appStats, _ := assetModel.AggregateApp(l.ctx, 10)
	for _, s := range appStats {
		resp.TopApp = append(resp.TopApp, types.StatItem{
			Name:  s.Field,
			Count: s.Count,
		})
	}

	// 9. 目录扫描统计
	dirscanTotal, _ := l.svcCtx.GetDirScanResultModel().CountByFilter(l.ctx, bson.M{})
	resp.DirScans = int(dirscanTotal)

	// 10. 资产分组总数
	groupCount, _ := assetModel.Count(l.ctx, bson.M{})
	resp.Groups = int(groupCount)

	// 11. 工作台变化（资产+风险）
	cutoff := time.Now().AddDate(0, 0, -7)
	assetChanges, _ := assetModel.AggregateChangesStats(l.ctx, cutoff)
	if assetChanges != nil {
		resp.AssetChanges = &types.AssetChanges{
			Total:       assetChanges.Total,
			NewInWindow: assetChanges.NewInWindow,
			ByCategory:  assetChanges.ByCategory,
		}
		if assetChanges.Total > 0 {
			resp.AssetChanges.GrowthRate = float64(assetChanges.NewInWindow*100) / float64(assetChanges.Total)
		}
	}

	riskChanges, _ := vulModel.AggregateChangesStats(l.ctx, cutoff)
	if riskChanges != nil {
		resp.RiskChanges = &types.RiskChanges{
			Open:          riskChanges.Open,
			NewInWindow:   riskChanges.NewInWindow,
			FixedInWindow: riskChanges.FixedInWindow,
			BySeverity:    riskChanges.BySeverity,
		}
		resp.RiskChanges.NetChange = riskChanges.NewInWindow - riskChanges.FixedInWindow
	}

	return resp, nil
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
