package common

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cscan/api/internal/svc"
	"cscan/model"
	"cscan/scanner"
	"cscan/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TaskBuilder handles common task creation logic
type TaskBuilder struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	log    logx.Logger
}

func NewTaskBuilder(ctx context.Context, svcCtx *svc.ServiceContext) *TaskBuilder {
	return &TaskBuilder{
		ctx:    ctx,
		svcCtx: svcCtx,
		log:    logx.WithContext(ctx),
	}
}

// BuildAndPushSubTasks splits targets and pushes sub-tasks to Redis queue
func (b *TaskBuilder) BuildAndPushSubTasks(workspaceId string, task *model.MainTask, taskConfig map[string]interface{}) (int, error) {
	// 1. Determine Batch Size — 自动计算最佳值
	batchSize := b.CalculateOptimalBatchSize(task.Target, taskConfig)
	b.log.Infof("TaskBuilder: auto-calculated batchSize=%d for task %s", batchSize, task.TaskId)

	// 2. Split Targets
	splitter := scheduler.NewTargetSplitter(batchSize)
	batches := splitter.SplitTargets(task.Target)

	// Scheme 1: Asynchronous prewrite using context.WithoutCancel to prevent HTTP context cancellation
	bgCtx := context.WithoutCancel(b.ctx)
	timeoutCtx, cancel := context.WithTimeout(bgCtx, 10*time.Minute)
	go func(ctx context.Context, cancelFunc context.CancelFunc) {
		defer cancelFunc()
		defer func() {
			if r := recover(); r != nil {
				logx.WithContext(ctx).Errorf("TaskBuilder: panic in async prewrite for task %s: %v", task.TaskId, r)
			}
		}()
		asyncBuilder := &TaskBuilder{
			ctx:    ctx,
			svcCtx: b.svcCtx,
			log:    logx.WithContext(ctx),
		}
		if err := asyncBuilder.prewriteInitialAssets(workspaceId, task, taskConfig, batches); err != nil {
			asyncBuilder.log.Errorf("TaskBuilder: async prewrite initial assets failed for task %s: %v", task.TaskId, err)
		}
	}(timeoutCtx, cancel)

	// 3. Calculate SubTask Count
	enabledModules := b.countEnabledModules(taskConfig)
	subTaskCount := len(batches) * enabledModules

	// 4. Update Main Task Status
	now := time.Now()
	b.svcCtx.GetMainTaskModel(workspaceId).Update(b.ctx, task.Id.Hex(), bson.M{
		"status":         model.TaskStatusPending,
		"sub_task_count": subTaskCount,
		"sub_task_done":  0,
		"start_time":     now,
	})

	// 5. Cache Info to Redis
	b.cacheTaskInfo(workspaceId, task, subTaskCount, len(batches), enabledModules)

	// 6. Push Sub-Tasks
	workers := b.extractWorkers(taskConfig)

	b.log.Infof("TaskBuilder: pushing %d batches for task %s", len(batches), task.TaskId)

	for i, batch := range batches {
		if err := b.pushSingleBatch(workspaceId, task, taskConfig, batch, i, len(batches), workers); err != nil {
			b.log.Errorf("Failed to push batch %d: %v", i, err)
			// Continue pushing other batches
		}
	}

	return len(batches), nil
}

func (b *TaskBuilder) pushSingleBatch(workspaceId string, task *model.MainTask, baseConfig map[string]interface{}, batchTarget string, index, total int, workers []string) error {
	// Deep copy config
	subConfig := make(map[string]interface{})
	for k, v := range baseConfig {
		subConfig[k] = v
	}
	subConfig["target"] = batchTarget
	subConfig["subTaskIndex"] = index
	subConfig["subTaskTotal"] = total

	configBytes, err := json.Marshal(subConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal sub-task config: %w", err)
	}
	subTaskId := task.TaskId
	if total > 1 {
		subTaskId = fmt.Sprintf("%s-%d", task.TaskId, index)
	}

	schedTask := &scheduler.TaskInfo{
		TaskId:      subTaskId,
		MainTaskId:  task.Id.Hex(),
		WorkspaceId: workspaceId,
		TaskName:    task.Name,
		Config:      string(configBytes),
		Priority:    1,
		Workers:     workers,
	}

	return b.svcCtx.Scheduler.PushTask(b.ctx, schedTask)
}

func (b *TaskBuilder) prewriteInitialAssets(workspaceId string, task *model.MainTask, taskConfig map[string]interface{}, batches []string) error {
	assetModel := b.svcCtx.GetAssetModel(workspaceId)
	orgId, _ := taskConfig["orgId"].(string)
	assets := collectInitialAssets(batches)

	for _, asset := range assets {
		if err := b.upsertInitialAsset(assetModel, task, asset, orgId); err != nil {
			b.log.Errorf("TaskBuilder: prewrite asset failed for task %s target %s: %v", task.TaskId, buildPrewriteAssetKey(asset), err)
		}
	}

	return nil
}

func collectInitialAssets(batches []string) []*scanner.Asset {
	// Scheme 2: Pass a generator that avoids synchronous DNS lookups during API request
	return collectInitialAssetsWithGenerator(batches, scanner.GenerateAssetsFromTargetsWithoutDNS)
}

func collectInitialAssetsWithGenerator(batches []string, generator func(string) []*scanner.Asset) []*scanner.Asset {
	seen := make(map[string]struct{})
	collected := make([]*scanner.Asset, 0)

	for _, batch := range batches {
		assets := generator(batch)
		for _, asset := range assets {
			if asset == nil || asset.Host == "" {
				continue
			}

			key := buildPrewriteAssetKey(asset)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			collected = append(collected, asset)
		}
	}

	return collected
}

func buildPrewriteAssetKey(asset *scanner.Asset) string {
	if asset == nil {
		return ""
	}
	if asset.Port > 0 {
		return fmt.Sprintf("%s:%d", asset.Host, asset.Port)
	}
	return asset.Authority
}

func (b *TaskBuilder) upsertInitialAsset(assetModel *model.AssetModel, task *model.MainTask, scanAsset *scanner.Asset, orgId string) error {
	asset := convertScannerAssetToModelAsset(task, scanAsset, orgId)

	var existing *model.Asset
	var err error
	if asset.Port > 0 {
		existing, err = assetModel.FindByHostPort(b.ctx, asset.Host, asset.Port)
	} else {
		existing, err = assetModel.FindByAuthorityOnly(b.ctx, asset.Authority)
	}

	if err != nil && err != mongo.ErrNoDocuments {
		return err
	}

	if existing == nil {
		asset.IsNewAsset = true
		asset.IsUpdated = false
		asset.LastTaskId = ""
		asset.FirstSeenTaskId = task.TaskId
		asset.LastStatusChangeTime = time.Now()
		return assetModel.Insert(b.ctx, asset)
	}

	updateFields := bson.M{}
	if len(existing.Ip.IpV4) == 0 && len(existing.Ip.IpV6) == 0 && (len(asset.Ip.IpV4) > 0 || len(asset.Ip.IpV6) > 0) {
		updateFields["ip"] = asset.Ip
	}
	if existing.CName == "" && asset.CName != "" {
		updateFields["cname"] = asset.CName
	}
	if existing.Domain == "" && asset.Domain != "" {
		updateFields["domain"] = asset.Domain
	}
	if existing.Source == "" && asset.Source != "" {
		updateFields["source"] = asset.Source
	}
	if existing.OrgId == "" && asset.OrgId != "" {
		updateFields["org_id"] = asset.OrgId
	}
	if existing.TaskId == "" && asset.TaskId != "" {
		updateFields["taskId"] = asset.TaskId
	}
	if existing.Category == "" && asset.Category != "" {
		updateFields["category"] = asset.Category
	}
	if existing.Host == "" && asset.Host != "" {
		updateFields["host"] = asset.Host
	}
	if existing.Authority == "" && asset.Authority != "" {
		updateFields["authority"] = asset.Authority
	}
	if existing.Port == 0 && asset.Port > 0 {
		updateFields["port"] = asset.Port
	}
	if !existing.IsHTTP && asset.IsHTTP {
		updateFields["is_http"] = asset.IsHTTP
	}

	if len(updateFields) == 0 {
		return nil
	}
	return assetModel.Update(b.ctx, existing.Id.Hex(), updateFields)
}

func convertScannerAssetToModelAsset(task *model.MainTask, scanAsset *scanner.Asset, orgId string) *model.Asset {
	now := time.Now()
	asset := &model.Asset{
		Authority:       scanAsset.Authority,
		Host:            scanAsset.Host,
		Port:            scanAsset.Port,
		Category:        scanAsset.Category,
		CName:           scanAsset.CName,
		IsHTTP:          scanAsset.IsHTTP,
		TaskId:          task.TaskId,
		Source:          scanAsset.Source,
		OrgId:           orgId,
		CreateTime:      now,
		UpdateTime:      now,
		IsNewAsset:      true,
		IsUpdated:       false,
		FirstSeenTaskId: task.TaskId,
	}
	if asset.Source == "" {
		asset.Source = "user_input"
	}
	// 复制 IconData 到 IconHashBytes（用于指纹匹配时重新计算 MMH3 hash）
	if len(scanAsset.IconData) > 0 {
		asset.IconHashBytes = scanAsset.IconData
	}
	switch scanAsset.Category {
	case "domain":
		asset.Domain = scanAsset.Host
	case "ipv4":
		asset.Ip.IpV4 = append(asset.Ip.IpV4, model.IPV4{IPName: scanAsset.Host})
	case "ipv6":
		asset.Ip.IpV6 = append(asset.Ip.IpV6, model.IPV6{IPName: scanAsset.Host})
	}
	for _, ip := range scanAsset.IPV4 {
		asset.Ip.IpV4 = append(asset.Ip.IpV4, model.IPV4{IPName: ip.IP, Location: ip.Location})
	}
	for _, ip := range scanAsset.IPV6 {
		asset.Ip.IpV6 = append(asset.Ip.IpV6, model.IPV6{IPName: ip.IP, Location: ip.Location})
	}
	return asset
}

func (b *TaskBuilder) countEnabledModules(configMap map[string]interface{}) int {
	// 与 worker/worker.go 中的 enabledPhases 计算保持一致
	// 关键规则：portscan 默认启用（enable != false），其他模块需要 enable == true
	count := 0

	// DomainScan
	if ds, ok := configMap["domainscan"].(map[string]interface{}); ok {
		if enable, ok := ds["enable"].(bool); ok && enable {
			count++
		}
	}

	// PortScan (default enabled if missing or nil, same as worker logic)
	if ps, ok := configMap["portscan"].(map[string]interface{}); ok {
		if enable, ok := ps["enable"].(bool); ok && enable {
			count++
		}
		// Note: if portscan exists but enable is not true (or is false), don't count
		// This matches worker logic: if config.PortScan != nil && config.PortScan.Enable
	} else {
		// portscan is nil or doesn't exist, count as enabled (default behavior)
		count++
	}

	// Other modules (must be explicitly enabled)
	// 修复：将 brutescan 也纳入统计，与 portidentify 等模块保持一致
	modules := []string{"portidentify", "fingerprint", "brutescan", "dirscan", "jsfinder", "pocscan"}
	for _, mod := range modules {
		if m, ok := configMap[mod].(map[string]interface{}); ok {
			if enable, ok := m["enable"].(bool); ok && enable {
				count++
			}
		}
	}

	if count == 0 {
		return 1
	}
	return count
}

func (b *TaskBuilder) cacheTaskInfo(workspaceId string, task *model.MainTask, subTaskCount, batchCount, modules int) {
	key := fmt.Sprintf("cscan:task:info:%s", task.TaskId)
	data := map[string]interface{}{
		"workspaceId":    workspaceId,
		"mainTaskId":     task.Id.Hex(),
		"subTaskCount":   subTaskCount,
		"batchCount":     batchCount,
		"enabledModules": modules,
	}
	bytes, _ := json.Marshal(data)
	b.svcCtx.RedisClient.Set(b.ctx, key, bytes, 24*time.Hour)
}

func (b *TaskBuilder) extractWorkers(config map[string]interface{}) []string {
	var workers []string
	if w, ok := config["workers"].([]interface{}); ok {
		for _, v := range w {
			if s, ok := v.(string); ok {
				workers = append(workers, s)
			}
		}
	}
	return workers
}

// CalculateOptimalBatchSize 根据目标数量和启用的模块自动计算最佳批次大小
// 核心原则：控制子任务总数（batches × modules）在合理范围内（10~30），
// 避免碎片化（子任务过多导致调度开销大）和过度聚合（单批次过大导致超时）
func (b *TaskBuilder) CalculateOptimalBatchSize(target string, taskConfig map[string]interface{}) int {
	// 如果用户显式设置了 batchSize > 0，优先使用（向后兼容）
	if bs, ok := taskConfig["batchSize"].(float64); ok && bs > 0 {
		return int(bs)
	}

	// 计算目标总数
	splitter := scheduler.NewTargetSplitter(1000000) // 用大值获取总目标数
	targetCount := splitter.GetTargetCount(target)

	// 获取启用的模块数
	enabledModules := b.countEnabledModules(taskConfig)

	// 最佳子任务总数范围：10~30
	// 太少 → 单批次过大 → POC扫描超时
	// 太多 → 调度开销大、进度卡顿感明显
	const (
		minSubTasks = 10
		maxSubTasks = 30
	)

	// 反推最佳批次数 = 子任务总数 / 模块数
	optimalBatches := (minSubTasks + maxSubTasks) / 2 / enabledModules
	if optimalBatches < 1 {
		optimalBatches = 1
	}

	// 反推最佳 batchSize = 目标总数 / 批次数
	batchSize := targetCount / optimalBatches
	if batchSize < 1 {
		batchSize = 1
	}

	// 限制 batchSize 在合理范围内
	// 最小 20：避免过度碎片化（如 batchSize=5 导致子任务爆炸）
	// 最大 200：避免单批次过大导致 POC 超时
	const (
		minBatchSize = 20
		maxBatchSize = 200
	)
	if batchSize < minBatchSize {
		batchSize = minBatchSize
	}
	if batchSize > maxBatchSize {
		batchSize = maxBatchSize
	}

	// 如果目标数量小于 minBatchSize，则不拆分
	if targetCount <= minBatchSize {
		return targetCount
	}

	return batchSize
}
