package notification

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cscan/internal/model"
	"cscan/pkg/notify"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Service 通知服务，封装任务完成/失败的通知触发逻辑。
// 供 API / Worker 直连 MongoDB 时独立调用。
type Service struct {
	db  *mongo.Database
	rdb *redis.Client
}

// NewService 创建通知服务
func NewService(db *mongo.Database, rdb *redis.Client) *Service {
	return &Service{db: db, rdb: rdb}
}

// NotifyTaskCompleted 任务完成时触发通知（幂等）。
// 使用 Redis SETNX cscan:task:notified:{id} TTL 24h 防止重复触发。
func (s *Service) NotifyTaskCompleted(ctx context.Context, mainTaskID, status string) error {
	// 1. 幂等检查：SETNX cscan:task:notified:{taskId} "1" 86400
	notifiedKey := fmt.Sprintf("cscan:task:notified:%s", mainTaskID)
	ok, err := s.rdb.SetNX(ctx, notifiedKey, "1", 24*time.Hour).Result()
	if err != nil {
		logx.Errorf("[Notification] SETNX %s failed: %v", notifiedKey, err)
		// Redis 故障时放行，确保通知不丢失
	} else if !ok {
		logx.Infof("[Notification] already notified, mainTaskId=%s", mainTaskID)
		return nil
	}

	// 2. 查询主任务信息
	taskModel := model.NewMainTaskModel(s.db)
	task, err := taskModel.FindById(ctx, mainTaskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task == nil {
		return fmt.Errorf("task not found: %s", mainTaskID)
	}

	// 3. Baseline 抑制：首次扫描完成时建立基线，首次新增不进通知
	baselineModel := model.NewWorkspaceBaselineModel(s.db)
	baselineJustEstablished := false
	if existing, gerr := baselineModel.Get(ctx, ""); gerr == nil && existing == nil {
		if _, eerr := baselineModel.Establish(ctx, "", mainTaskID); eerr != nil {
			logx.Errorf("[Notification] baseline establish failed: %v", eerr)
		} else {
			baselineJustEstablished = true
			logx.Infof("[Notification] baseline established for task=%s", mainTaskID)
		}
	}

	// 4. 查询资产和漏洞统计
	// 资产数用 port>0 的服务资产口径（与资产空间搜索「服务」列表一致），
	// 剔除端口 0 的子域名占位记录，避免通知数与页面数对不上
	assetModel := model.NewAssetModel(s.db)
	vulModel := model.NewVulModel(s.db)
	assetCount, _ := assetModel.CountByTaskIdWithPort(ctx, mainTaskID)
	vulCount, _ := vulModel.CountByTaskId(ctx, mainTaskID)

	// 5. 获取启用的通知配置
	notifyConfigModel := model.NewNotifyConfigModel(s.db)
	configs, err := notifyConfigModel.FindEnabled(ctx)
	if err != nil {
		return fmt.Errorf("find notify configs: %w", err)
	}
	if len(configs) == 0 {
		logx.Infof("[Notification] no enabled notify configs, mainTaskId=%s", mainTaskID)
		return nil
	}

	// 6. 构建通知配置列表
	var configItems []notify.ConfigItem
	var webURL string
	for _, c := range configs {
		item := notify.ConfigItem{
			Provider:        c.Provider,
			Config:          c.Config,
			Status:          c.Status,
			MessageTemplate: c.MessageTemplate,
			WebURL:          c.WebURL,
		}
		if c.HighRiskFilter != nil {
			item.HighRiskFilter = &notify.HighRiskFilter{
				Enabled:               c.HighRiskFilter.Enabled,
				HighRiskFingerprints:  c.HighRiskFilter.HighRiskFingerprints,
				HighRiskPorts:         c.HighRiskFilter.HighRiskPorts,
				HighRiskPocSeverities: c.HighRiskFilter.HighRiskPocSeverities,
				NewAssetNotify:        c.HighRiskFilter.NewAssetNotify,
				NewRiskNotify:         c.HighRiskFilter.NewRiskNotify,
				FixedNotify:           c.HighRiskFilter.FixedNotify,
			}
		}
		configItems = append(configItems, item)
		if webURL == "" && c.WebURL != "" {
			webURL = c.WebURL
		}
	}

	// 7. 加载全局高危过滤配置并合并
	s.mergeGlobalHighRiskFilter(ctx, configItems)

	// 8. 构建报告 URL
	reportURL := ""
	if webURL != "" {
		reportURL = fmt.Sprintf("%s/report?taskId=%s", strings.TrimSuffix(webURL, "/"), mainTaskID)
	}

	// 9. 构建通知结果
	result := &notify.NotifyResult{
		TaskId:      mainTaskID,
		TaskName:    task.Name,
		Status:      status,
		AssetCount:  int(assetCount),
		VulCount:    int(vulCount),
		ReportURL:   reportURL,
	}
	if task.StartTime != nil {
		result.StartTime = *task.StartTime
	}
	if task.EndTime != nil {
		result.EndTime = *task.EndTime
	}
	if task.StartTime != nil && task.EndTime != nil {
		d := task.EndTime.Sub(*task.StartTime)
		if d.Hours() >= 1 {
			result.Duration = d.Round(time.Minute).String()
		} else if d.Minutes() >= 1 {
			result.Duration = d.Round(time.Second).String()
		} else {
			result.Duration = d.Round(time.Millisecond).String()
		}
	}

	// 10. 收集高危信息
	result.HighRiskInfo = collectHighRiskInfo(ctx, s.db, mainTaskID, configItems)

	// 11. 首次扫描（刚建立基线）不产生新增资产通知
	if baselineJustEstablished && result.HighRiskInfo != nil {
		result.HighRiskInfo.NewAssetCount = 0
		result.HighRiskInfo.NewAssetList = nil
	}

	// 12. 异步发送通知
	notify.SendNotificationAsync(ctx, configItems, result)
	logx.Infof("[Notification] notification queued: mainTaskId=%s, status=%s, assets=%d, vuls=%d", mainTaskID, status, assetCount, vulCount)

	return nil
}

// mergeGlobalHighRiskFilter 从 system_config 集合加载全局高危过滤配置并合并到配置项
func (s *Service) mergeGlobalHighRiskFilter(ctx context.Context, configItems []notify.ConfigItem) {
	collection := s.db.Collection("system_config")

	var result struct {
		Key    string   `bson:"key"`
		Config bson.Raw `bson:"config"`
	}

	if err := collection.FindOne(ctx, bson.M{"key": "high_risk_filter_config"}).Decode(&result); err != nil {
		return
	}

	var config struct {
		Enabled               bool     `bson:"enabled" json:"enabled"`
		HighRiskFingerprints  []string `bson:"high_risk_fingerprints" json:"highRiskFingerprints"`
		HighRiskPorts         []int    `bson:"high_risk_ports" json:"highRiskPorts"`
		HighRiskPocSeverities []string `bson:"high_risk_poc_severities" json:"highRiskPocSeverities"`
		NewAssetNotify        bool     `bson:"new_asset_notify" json:"newAssetNotify"`
		NewRiskNotify         *bool    `bson:"new_risk_notify,omitempty" json:"newRiskNotify,omitempty"`
		FixedNotify           *bool    `bson:"fixed_notify,omitempty" json:"fixedNotify,omitempty"`
	}

	if err := bson.Unmarshal(result.Config, &config); err != nil {
		return
	}

	if !config.Enabled {
		return
	}

	globalFilter := &notify.HighRiskFilter{
		Enabled:               config.Enabled,
		HighRiskFingerprints:  config.HighRiskFingerprints,
		HighRiskPorts:         config.HighRiskPorts,
		HighRiskPocSeverities: config.HighRiskPocSeverities,
		NewAssetNotify:        config.NewAssetNotify,
		NewRiskNotify:         config.NewRiskNotify,
		FixedNotify:           config.FixedNotify,
	}

	for i := range configItems {
		if configItems[i].HighRiskFilter == nil {
			configItems[i].HighRiskFilter = globalFilter
		} else if configItems[i].HighRiskFilter.Enabled {
			hasValid := len(configItems[i].HighRiskFilter.HighRiskFingerprints) > 0 ||
				len(configItems[i].HighRiskFilter.HighRiskPorts) > 0 ||
				len(configItems[i].HighRiskFilter.HighRiskPocSeverities) > 0 ||
				configItems[i].HighRiskFilter.NewAssetNotify
			if !hasValid {
				configItems[i].HighRiskFilter = globalFilter
			}
		}
	}
}
