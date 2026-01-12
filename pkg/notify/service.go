package notify

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// NotifyService 通知服务（用于API层调用）
type NotifyService struct {
	manager *NotifyManager
}

// NewNotifyService 创建通知服务
func NewNotifyService() *NotifyService {
	return &NotifyService{
		manager: NewNotifyManager(),
	}
}

// LoadConfigsFromDB 从数据库配置加载提供者
// configs 是从数据库读取的配置列表
func (s *NotifyService) LoadConfigsFromDB(configs []ConfigItem) error {
	return s.manager.LoadConfigs(configs)
}

// SendTaskNotification 发送任务完成通知
func (s *NotifyService) SendTaskNotification(ctx context.Context, result *NotifyResult) error {
	return s.manager.Send(ctx, result)
}

// TaskCompleteInfo 任务完成信息（用于从Redis或数据库获取）
type TaskCompleteInfo struct {
	TaskId      string    `json:"taskId"`
	TaskName    string    `json:"taskName"`
	Status      string    `json:"status"`
	AssetCount  int       `json:"assetCount"`
	VulCount    int       `json:"vulCount"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
	WorkspaceId string    `json:"workspaceId"`
}

// BuildNotifyResult 从任务完成信息构建通知结果
func BuildNotifyResult(info *TaskCompleteInfo) *NotifyResult {
	duration := ""
	if !info.StartTime.IsZero() && !info.EndTime.IsZero() {
		d := info.EndTime.Sub(info.StartTime)
		if d.Hours() >= 1 {
			duration = d.Round(time.Minute).String()
		} else if d.Minutes() >= 1 {
			duration = d.Round(time.Second).String()
		} else {
			duration = d.Round(time.Millisecond).String()
		}
	}

	return &NotifyResult{
		TaskId:      info.TaskId,
		TaskName:    info.TaskName,
		Status:      info.Status,
		AssetCount:  info.AssetCount,
		VulCount:    info.VulCount,
		Duration:    duration,
		StartTime:   info.StartTime,
		EndTime:     info.EndTime,
		WorkspaceId: info.WorkspaceId,
	}
}

// SendNotificationAsync 异步发送通知（不阻塞主流程）
// 支持高危过滤：如果配置了高危过滤且未检测到高危项，则跳过该配置的通知
func SendNotificationAsync(ctx context.Context, configs []ConfigItem, result *NotifyResult) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logx.Errorf("SendNotificationAsync panic: %v", r)
			}
		}()

		// 创建独立的context，不受父context取消影响
		notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 过滤需要发送通知的配置
		filteredConfigs := filterConfigsByHighRisk(configs, result)
		if len(filteredConfigs) == 0 {
			logx.Infof("SendNotificationAsync: no configs to send after high-risk filtering, taskId=%s", result.TaskId)
			return
		}

		manager := NewNotifyManager()
		if err := manager.LoadConfigs(filteredConfigs); err != nil {
			logx.Errorf("Load notify configs failed: %v", err)
			return
		}

		if err := manager.Send(notifyCtx, result); err != nil {
			logx.Errorf("Send notification failed: %v", err)
		} else {
			logx.Infof("Task notification sent: taskId=%s, status=%s", result.TaskId, result.Status)
		}
	}()
}

// filterConfigsByHighRisk 根据高危配置过滤通知配置
// 如果配置启用了高危过滤但未检测到高危项，则跳过该配置
func filterConfigsByHighRisk(configs []ConfigItem, result *NotifyResult) []ConfigItem {
	var filtered []ConfigItem
	for _, cfg := range configs {
		// 如果未启用高危过滤，直接添加
		if cfg.HighRiskFilter == nil || !cfg.HighRiskFilter.Enabled {
			filtered = append(filtered, cfg)
			continue
		}

		// 启用了高危过滤，检查是否有匹配的高危项
		if shouldNotifyByHighRisk(cfg.HighRiskFilter, result) {
			filtered = append(filtered, cfg)
		} else {
			logx.Infof("filterConfigsByHighRisk: skipping provider %s due to no high-risk match", cfg.Provider)
		}
	}
	return filtered
}

// shouldNotifyByHighRisk 检查是否应该根据高危配置发送通知
func shouldNotifyByHighRisk(filter *HighRiskFilter, result *NotifyResult) bool {
	if result.HighRiskInfo == nil {
		return false
	}

	// 检查高危指纹
	if len(filter.HighRiskFingerprints) > 0 && len(result.HighRiskInfo.HighRiskFingerprints) > 0 {
		for _, configFp := range filter.HighRiskFingerprints {
			for _, foundFp := range result.HighRiskInfo.HighRiskFingerprints {
				if configFp == foundFp {
					return true
				}
			}
		}
	}

	// 检查高危端口
	if len(filter.HighRiskPorts) > 0 && len(result.HighRiskInfo.HighRiskPorts) > 0 {
		for _, configPort := range filter.HighRiskPorts {
			for _, foundPort := range result.HighRiskInfo.HighRiskPorts {
				if configPort == foundPort {
					return true
				}
			}
		}
	}

	// 检查高危POC严重级别
	if len(filter.HighRiskPocSeverities) > 0 && result.HighRiskInfo.HighRiskVulSeverities != nil {
		for _, severity := range filter.HighRiskPocSeverities {
			if count, ok := result.HighRiskInfo.HighRiskVulSeverities[severity]; ok && count > 0 {
				return true
			}
		}
	}

	return false
}

// ParseConfigJSON 解析配置JSON
func ParseConfigJSON(configJSON string) (map[string]interface{}, error) {
	var config map[string]interface{}
	err := json.Unmarshal([]byte(configJSON), &config)
	return config, err
}
