package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cscan/pkg/notify"
	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type IncrSubTaskDoneLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewIncrSubTaskDoneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IncrSubTaskDoneLogic {
	return &IncrSubTaskDoneLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 递增子任务完成数（模块级别）
// 使用原子操作防止并发导致计数超过上限
func (l *IncrSubTaskDoneLogic) IncrSubTaskDone(in *pb.IncrSubTaskDoneReq) (*pb.IncrSubTaskDoneResp, error) {
	l.Logger.Infof("IncrSubTaskDone: taskId=%s, mainTaskId=%s, phase=%s", in.TaskId, in.MainTaskId, in.Phase)

	if in.WorkspaceId == "" || in.MainTaskId == "" {
		return &pb.IncrSubTaskDoneResp{
			Success: false,
			Message: "workspaceId or mainTaskId is empty",
		}, nil
	}

	// 获取任务模型
	taskModel := l.svcCtx.GetMainTaskModel(in.WorkspaceId)

	// 使用原子操作递增 sub_task_done，防止超过上限
	task, incremented, err := taskModel.IncrSubTaskDoneAtomic(l.ctx, in.MainTaskId)
	if err != nil {
		l.Logger.Errorf("IncrSubTaskDone: failed to incr atomic, mainTaskId=%s, error=%v", in.MainTaskId, err)
		return &pb.IncrSubTaskDoneResp{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	if !incremented {
		// 已达上限，记录警告但不视为错误
		l.Logger.Infof("IncrSubTaskDone: already at limit, mainTaskId=%s, done=%d, total=%d",
			in.MainTaskId, task.SubTaskDone, task.SubTaskCount)
	}

	// 判断是否全部完成
	allDone := task.SubTaskDone >= task.SubTaskCount
	l.Logger.Infof("IncrSubTaskDone: mainTaskId=%s, phase=%s, done=%d, total=%d, allDone=%v, incremented=%v",
		in.MainTaskId, in.Phase, task.SubTaskDone, task.SubTaskCount, allDone, incremented)

	// 计算进度百分比，确保不超过100
	progress := calculateProgress(task.SubTaskDone, task.SubTaskCount)

	// 更新进度和当前阶段
	update := bson.M{
		"progress":      progress,
		"current_phase": in.Phase,
	}

	if err := taskModel.Update(l.ctx, in.MainTaskId, update); err != nil {
		l.Logger.Errorf("IncrSubTaskDone: failed to update progress, mainTaskId=%s, error=%v", in.MainTaskId, err)
	}

	// 如果全部完成，使用原子操作更新状态
	if allDone {
		updated, err := taskModel.MarkTaskCompleted(l.ctx, in.MainTaskId)
		if err != nil {
			l.Logger.Errorf("IncrSubTaskDone: failed to mark completed, mainTaskId=%s, error=%v", in.MainTaskId, err)
		} else if updated {
			l.Logger.Infof("IncrSubTaskDone: task marked as completed, mainTaskId=%s", in.MainTaskId)
			// 只有成功更新状态时才发送通知（避免重复通知）
			l.sendTaskNotification(in.WorkspaceId, in.MainTaskId, "SUCCESS")
		} else {
			l.Logger.Infof("IncrSubTaskDone: task already completed, mainTaskId=%s", in.MainTaskId)
		}
	}

	return &pb.IncrSubTaskDoneResp{
		Success:      true,
		Message:      "ok",
		SubTaskDone:  int32(task.SubTaskDone),
		SubTaskCount: int32(task.SubTaskCount),
		AllDone:      allDone,
	}, nil
}

// calculateProgress 计算进度百分比，确保不超过100
func calculateProgress(done, total int) int {
	if total <= 0 {
		return 0
	}
	progress := done * 100 / total
	if progress > 100 {
		progress = 100
	}
	return progress
}

// sendTaskNotification 发送任务完成通知
func (l *IncrSubTaskDoneLogic) sendTaskNotification(workspaceId, mainTaskId, status string) {
	// 获取任务详情
	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)
	task, err := taskModel.FindById(l.ctx, mainTaskId)
	if err != nil {
		l.Logger.Errorf("sendTaskNotification: failed to get task, mainTaskId=%s, error=%v", mainTaskId, err)
		return
	}

	// 获取资产和漏洞统计
	assetModel := l.svcCtx.GetAssetModel(workspaceId)
	vulModel := l.svcCtx.GetVulModel(workspaceId)

	assetCount, _ := assetModel.CountByTaskId(l.ctx, mainTaskId)
	vulCount, _ := vulModel.CountByTaskId(l.ctx, mainTaskId)

	// 获取启用的通知配置
	configs, err := l.svcCtx.NotifyConfigModel.FindEnabled(l.ctx)
	if err != nil {
		l.Logger.Errorf("sendTaskNotification: failed to get notify configs, error=%v", err)
		return
	}

	if len(configs) == 0 {
		l.Logger.Infof("sendTaskNotification: no enabled notify configs")
		return
	}

	// 构建通知配置列表
	var configItems []notify.ConfigItem
	var webURL string // 用于生成报告URL
	for _, c := range configs {
		item := notify.ConfigItem{
			Provider:        c.Provider,
			Config:          c.Config,
			Status:          c.Status,
			MessageTemplate: c.MessageTemplate,
			WebURL:          c.WebURL,
		}
		// 转换高危过滤配置
		if c.HighRiskFilter != nil {
			item.HighRiskFilter = &notify.HighRiskFilter{
				Enabled:              c.HighRiskFilter.Enabled,
				HighRiskFingerprints: c.HighRiskFilter.HighRiskFingerprints,
				HighRiskPorts:        c.HighRiskFilter.HighRiskPorts,
				HighRiskPocSeverities: c.HighRiskFilter.HighRiskPocSeverities,
			}
		}
		configItems = append(configItems, item)
		// 获取第一个配置的WebURL作为报告URL的基础
		if webURL == "" && c.WebURL != "" {
			webURL = c.WebURL
		}
	}

	// 构建报告URL
	reportURL := ""
	if webURL != "" {
		// 去除末尾的斜杠
		webURL = strings.TrimSuffix(webURL, "/")
		reportURL = fmt.Sprintf("%s/report?taskId=%s", webURL, mainTaskId)
	}

	// 构建通知结果
	result := &notify.NotifyResult{
		TaskId:      mainTaskId,
		TaskName:    task.Name,
		Status:      status,
		AssetCount:  int(assetCount),
		VulCount:    int(vulCount),
		WorkspaceId: workspaceId,
		ReportURL:   reportURL,
	}

	// 设置时间（处理指针类型）
	if task.StartTime != nil {
		result.StartTime = *task.StartTime
	}
	if task.EndTime != nil {
		result.EndTime = *task.EndTime
	}

	// 计算耗时
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

	// 收集高危信息（用于高危过滤判断）
	result.HighRiskInfo = l.collectHighRiskInfo(workspaceId, mainTaskId, configItems)

	// 异步发送通知
	notify.SendNotificationAsync(l.ctx, configItems, result)
	l.Logger.Infof("sendTaskNotification: notification queued for task %s, status=%s", mainTaskId, status)
}

// collectHighRiskInfo 收集任务的高危信息
func (l *IncrSubTaskDoneLogic) collectHighRiskInfo(workspaceId, mainTaskId string, configs []notify.ConfigItem) *notify.HighRiskInfo {
	// 检查是否有配置启用了高危过滤
	hasHighRiskFilter := false
	var allFingerprints []string
	var allPorts []int
	var allSeverities []string

	for _, cfg := range configs {
		if cfg.HighRiskFilter != nil && cfg.HighRiskFilter.Enabled {
			hasHighRiskFilter = true
			allFingerprints = append(allFingerprints, cfg.HighRiskFilter.HighRiskFingerprints...)
			allPorts = append(allPorts, cfg.HighRiskFilter.HighRiskPorts...)
			allSeverities = append(allSeverities, cfg.HighRiskFilter.HighRiskPocSeverities...)
		}
	}

	// 如果没有配置启用高危过滤，不需要收集
	if !hasHighRiskFilter {
		return nil
	}

	info := &notify.HighRiskInfo{
		HighRiskFingerprints:  []string{},
		HighRiskPorts:         []int{},
		HighRiskVulSeverities: make(map[string]int),
	}

	// 收集高危指纹（从资产的指纹中匹配）
	if len(allFingerprints) > 0 {
		assetModel := l.svcCtx.GetAssetModel(workspaceId)
		assets, err := assetModel.FindByTaskId(l.ctx, mainTaskId)
		if err == nil {
			fingerprintSet := make(map[string]bool)
			for _, fp := range allFingerprints {
				fingerprintSet[fp] = true
			}
			foundFpSet := make(map[string]bool)
			for _, asset := range assets {
				for _, fp := range asset.Fingerprints {
					if fingerprintSet[fp] && !foundFpSet[fp] {
						info.HighRiskFingerprints = append(info.HighRiskFingerprints, fp)
						foundFpSet[fp] = true
					}
				}
			}
		}
	}

	// 收集高危端口（从资产的端口中匹配）
	if len(allPorts) > 0 {
		assetModel := l.svcCtx.GetAssetModel(workspaceId)
		assets, err := assetModel.FindByTaskId(l.ctx, mainTaskId)
		if err == nil {
			portSet := make(map[int]bool)
			for _, port := range allPorts {
				portSet[port] = true
			}
			foundPortSet := make(map[int]bool)
			for _, asset := range assets {
				if portSet[asset.Port] && !foundPortSet[asset.Port] {
					info.HighRiskPorts = append(info.HighRiskPorts, asset.Port)
					foundPortSet[asset.Port] = true
				}
			}
		}
	}

	// 收集高危漏洞统计
	if len(allSeverities) > 0 {
		vulModel := l.svcCtx.GetVulModel(workspaceId)
		vuls, err := vulModel.Find(l.ctx, bson.M{"task_id": mainTaskId}, 0, 0)
		if err == nil {
			severitySet := make(map[string]bool)
			for _, s := range allSeverities {
				severitySet[s] = true
			}
			for _, vul := range vuls {
				if severitySet[vul.Severity] {
					info.HighRiskVulSeverities[vul.Severity]++
					info.HighRiskVulCount++
				}
			}
		}
	}

	return info
}
