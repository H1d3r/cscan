package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/config"
	"cscan/api/internal/handler"
	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/internal/model"
	"cscan/internal/onlineapi"
	"cscan/pkg"

	"cscan/internal/scheduler"
	"cscan/pkg/utils"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"go.mongodb.org/mongo-driver/bson"
)

var configFile = flag.String("f", "etc/cscan.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config

	conf.MustLoad(*configFile, &c)

	// H-13 修复：必须在任何 logx 调用之前初始化 logger，否则日志可能丢失或格式错误
	logx.MustSetup(c.Log)
	logx.DisableStat()

	// 安装日志过滤器：抑制 Worker 高频轮询接口的 access log
	// Worker 空闲时会每秒多次轮询 /api/v1/worker/task/check 和 /api/v1/worker/heartbeat，
	// 产生大量无意义的 [HTTP] access log，污染日志文件。
	// 通过包装 logx.Writer，过滤掉这些高频路径的正常 Info 级别日志（错误日志保留）。
	originalWriter := logx.Reset()
	filteredWriter := pkg.NewFilteredLogWriter(originalWriter, []string{
		"/api/v1/worker/task/check",
		"/api/v1/worker/heartbeat",
		"/api/v1/worker/task/control",
	})
	logx.SetWriter(filteredWriter)

	// 从环境变量加载 JWT secret（优先级高于配置文件）
	c.LoadSecretFromEnv()
	if c.Auth.AccessSecret == "" {
		// 开发模式豁免：以下任一条件成立时，允许使用随机 secret，方便本地调试：
		//   1) 显式声明 CSCAN_DEV=1
		//   2) 配置文件 Mode: dev
		//   3) 通过 `go run` 启动（本地开发最常见方式，自动豁免，无需手动设置环境变量）
		// 生产环境（Docker 镜像内以编译二进制运行，且已注入 CSCAN_JWT_SECRET）不满足上述条件，
		// 若未配置 secret 则拒绝启动——历史事故表明，随机 secret 会导致 API 重启后所有 token 失效，
		// 且每次重启 secret 不同，多副本部署时 token 互相不认。
		if isLocalDev(c) {
			c.Auth.AccessSecret = uuid.New().String()
			logx.Info("WARNING: running in DEV mode, using auto-generated JWT secret (NOT suitable for production)")
		} else {
			logx.Error("JWT secret not configured. Set CSCAN_JWT_SECRET environment variable.")
			logx.Error("For local development, either set CSCAN_DEV=1, set Mode: dev, or run via `go run` to allow auto-generated secret.")
			os.Exit(1)
		}
	}

	fmt.Println(`
   ______ _____  ______          _   _ 
  / ____/ ____|/ __ \ \        / / | \ | |
 | |   | (___ | |  | \ \  /\  / /|  \| |
 | |    \___ \| |  | |\ \/  \/ / | .  |
 | |________) | |__| | \  /\  /  | |\  |
  \_____|_____/ \____/   \/  \/   |_| \_| 
                                         `)
	fmt.Println("---------------------------------------------------------")
	logx.Info("CScan API Service Starting")
	logx.Infof("Config loaded from: %s", *configFile)
	fmt.Println("---------------------------------------------------------")
	// 创建服务上下文
	svcCtx, err := svc.NewServiceContext(c)
	if err != nil {
		logx.Errorf("Failed to initialize service: %v", err)
		return
	}

	// 创建HTTP服务器
	server := rest.MustNewServer(c.RestConf,
		rest.WithNotFoundHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"code":404,"msg":"接口不存在","data":null}`))
		})),
		rest.WithCors("*"), // 启用 CORS，允许所有来源（生产环境应配置具体域名）
	)
	defer server.Stop()

	handler.RegisterHandlers(server, svcCtx)

	// 创建任务调度器服务（复用svcCtx中已有的Scheduler实例，避免创建重复实例）
	schedulerSvc := scheduler.NewSchedulerService(svcCtx.Scheduler, svcCtx.RedisClient, svcCtx.SyncMethods, &cronTaskSourceAdapter{model: svcCtx.CronTaskModel})
	go schedulerSvc.Start()

	// 启动定时任务执行消息订阅
	go startCronExecuteSubscriber(svcCtx, schedulerSvc.GetScheduler())

	// 启动孤儿任务恢复后台任务（每 5 分钟检查一次）
	go startOrphanedTaskRecovery(svcCtx)

	logx.Infof("CScan API is running at: %s:%d", c.Host, c.Port)
	logx.Infof("Environment: %s | LogLevel: %s", c.Mode, c.Log.Level)
	server.Start()
}

// CronTaskType 定时任务类型
const (
	CronTaskTypeScan        = "scan"         // 资产扫描任务
	CronTaskTypeSpaceEngine = "space_engine" // 空间引擎拉取任务
)

// CronExecuteMessage 定时任务执行消息（统一格式，兼容资产扫描与空间引擎拉取）
type CronExecuteMessage struct {
	// 通用字段
	TaskType   string `json:"taskType"`   // 任务类型：scan / space_engine
	CronTaskId string `json:"cronTaskId"` // 定时任务ID
	TaskName   string `json:"taskName"`   // 任务名称

	// 资产扫描任务字段（taskType=scan）
	Target string `json:"target"`
	Config string `json:"config"`

	// 空间引擎拉取任务字段（taskType=space_engine）
	TargetMode          string   `json:"targetMode"`          // 目标模式：asset_ids / template / custom
	AssetIds            []string `json:"assetIds"`            // 资产ID列表（targetMode=asset_ids 时使用）
	EnableSubdomainPull bool     `json:"enableSubdomainPull"` // 是否启用子域名拉取
	ConfigSource        string   `json:"configSource"`        // 配置来源：default / template / custom
	TemplateId          string   `json:"templateId"`          // 模板ID（configSource=template 时使用）
	Platform            string   `json:"platform"`            // 平台：fofa/hunter/quake
	Query               string   `json:"query"`               // 查询语句
	MaxResults          int      `json:"maxResults"`          // 最大结果数
}

// startCronExecuteSubscriber 启动定时任务执行消息订阅（含自动重连）。
// 订阅两个频道：
//   - "cscan:cron:execute_scan"  -> 资产扫描任务，调用 createAndPushCronTask
//   - "cscan:cron:execute_space" -> 空间引擎拉取任务，调用 createAndPushSpaceEngineTask
func startCronExecuteSubscriber(svcCtx *svc.ServiceContext, sched *scheduler.Scheduler) {
	ctx := context.Background()
	retryDelay := 5 * time.Second
	maxRetryDelay := 60 * time.Second
	// 空间引擎并发限制（最多同时 5 个，防止内存和 API 配额耗尽）
	spaceEngineSem := make(chan struct{}, 5)

	for {
		pubsub := svcCtx.RedisClient.Subscribe(ctx, "cscan:cron:execute_scan", "cscan:cron:execute_space")

		logx.Info("Cron execute subscriber started (channels: execute_scan, execute_space)")

		ch := pubsub.Channel()
		for msg := range ch {
			// 成功接收消息说明连接正常，重置退避延迟
			retryDelay = 5 * time.Second

			var execMsg CronExecuteMessage
			if err := json.Unmarshal([]byte(msg.Payload), &execMsg); err != nil {
				logx.Errorf("Failed to parse cron execute message: %v", err)
				continue
			}

			// 修复 M-19：空间引擎任务包含长时间分页拉取（可能数分钟），必须在独立 goroutine 中执行，
			// 否则会阻塞 Pub/Sub 消费循环，期间其他消息无法消费，Pub/Sub 断连或缓冲溢出会丢任务。
			// 普通扫描任务本身只做入队（快速完成），可直接执行。
			msgCopy := execMsg // 闭包捕获副本
			switch msg.Channel {
			case "cscan:cron:execute_space":
				msgCopy.TaskType = CronTaskTypeSpaceEngine
				logx.Infof("Received space-engine cron message: cronTaskId=%s, platform=%s", msgCopy.CronTaskId, msgCopy.Platform)
				go func(m CronExecuteMessage) {
					spaceEngineSem <- struct{}{}        // 获取并发槽
					defer func() { <-spaceEngineSem }() // 释放并发槽
					if err := createAndPushSpaceEngineTask(ctx, svcCtx, sched, &m); err != nil {
						logx.Errorf("Failed to create space-engine task: %v", err)
					}
				}(msgCopy)
			case "cscan:cron:execute_scan":
				fallthrough
			default:
				// 兼容旧消息：若未指定 taskType 则默认为 scan
				if msgCopy.TaskType == "" {
					msgCopy.TaskType = CronTaskTypeScan
				}
				if msgCopy.TaskType == CronTaskTypeSpaceEngine {
					logx.Infof("Received space-engine cron message (from scan channel, taskType override): cronTaskId=%s", msgCopy.CronTaskId)
					go func(m CronExecuteMessage) {
						spaceEngineSem <- struct{}{}
						defer func() { <-spaceEngineSem }()
						if err := createAndPushSpaceEngineTask(ctx, svcCtx, sched, &m); err != nil {
							logx.Errorf("Failed to create space-engine task: %v", err)
						}
					}(msgCopy)
				} else {
					logx.Infof("Received scan cron message: cronTaskId=%s, taskName=%s", msgCopy.CronTaskId, msgCopy.TaskName)
					// 扫描任务入队是快速操作，直接执行
					if err := createAndPushCronTask(ctx, svcCtx, sched, &msgCopy); err != nil {
						logx.Errorf("Failed to create cron task: %v", err)
					}
				}
			}
		}
		pubsub.Close()
		logx.Errorf("[CronExecuteSubscriber] Redis subscription disconnected, reconnecting in %v...", retryDelay)
		time.Sleep(retryDelay)
		// 指数退避，最大60秒
		if retryDelay < maxRetryDelay {
			retryDelay = retryDelay * 2
			if retryDelay > maxRetryDelay {
				retryDelay = maxRetryDelay
			}
		}
	}
}

// createAndPushCronTask 创建定时任务的 MainTask 并推送到队列
func createAndPushCronTask(ctx context.Context, svcCtx *svc.ServiceContext, sched *scheduler.Scheduler, msg *CronExecuteMessage) error {
	// === 目标实时解析（执行时获取最新资产） ===
	finalTarget := msg.Target

	// 如果是资产选择模式，实时从 AssetTargetMeta 查询最新资产
	if msg.TargetMode == "asset" && len(msg.AssetIds) > 0 {
		metaModel := svcCtx.GetAssetTargetMetaModel()
		metas, err := metaModel.FindByIDs(ctx, msg.AssetIds)
		if err != nil {
			logx.Errorf("Failed to resolve asset targets for cron task %s: %v, falling back to stored target", msg.CronTaskId, err)
		} else {
			var assetValues []string
			for _, m := range metas {
				if m.TargetValue != "" {
					assetValues = append(assetValues, m.TargetValue)
				}
			}
			resolvedAssetTarget := strings.Join(assetValues, "\n")
			if strings.TrimSpace(finalTarget) != "" {
				finalTarget = strings.TrimSpace(resolvedAssetTarget) + "\n" + strings.TrimSpace(finalTarget)
			} else {
				finalTarget = resolvedAssetTarget
			}
			logx.Infof("Cron task %s: resolved %d assets to targets at execution time", msg.CronTaskId, len(assetValues))
		}
	}

	if strings.TrimSpace(finalTarget) == "" {
		return fmt.Errorf("no valid targets resolved for cron task %s", msg.CronTaskId)
	}

	// === 配置处理：如果是模板模式，实时展开最新模板配置 ===
	finalConfig := msg.Config
	if msg.ConfigSource == "template" && msg.TemplateId != "" {
		tmpl, err := svcCtx.ScanTemplateModel.FindById(ctx, msg.TemplateId)
		if err != nil || tmpl == nil {
			logx.Errorf("Failed to load template %s for cron task %s: %v, using stored config", msg.TemplateId, msg.CronTaskId, err)
		} else if tmpl.Config != "" {
			finalConfig = tmpl.Config
			logx.Infof("Cron task %s: loaded latest template config for template %s", msg.CronTaskId, msg.TemplateId)
		}
	}

	// === 处理 enableSubdomainPull：从数据库拉取已存在的子域名加入目标列表 ===
	// 注意：这不是开启子域名爆破扫描，而是把数据库中已有的子域名资产也加入扫描目标
	if msg.EnableSubdomainPull {
		// 收集根域名（从已解析的目标中提取域名类型的目标）
		var rootDomains []string
		domainSet := make(map[string]struct{})
		for _, t := range strings.Split(finalTarget, "\n") {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			// 如果不是IP，认为是域名，提取根域名
			if !isIPAddress(t) {
				root := extractRootDomain(t)
				if root != "" {
					if _, exists := domainSet[root]; !exists {
						domainSet[root] = struct{}{}
						rootDomains = append(rootDomains, root)
					}
				}
			}
		}

		if len(rootDomains) > 0 {
			// 从 Asset 集合中查询属于这些根域名的子域名（host 字段）
			assetModel := svcCtx.GetAssetModel()
			subdomainHosts, err := assetModel.FindSubdomainHostsByRootDomain(ctx, rootDomains)
			if err != nil {
				logx.Errorf("Failed to query subdomains for roots %v: %v", rootDomains, err)
			} else if len(subdomainHosts) > 0 {
				// 将子域名追加到目标列表（去重：已经在finalTarget中的不重复添加）
				existingTargets := make(map[string]struct{})
				for _, t := range strings.Split(finalTarget, "\n") {
					existingTargets[strings.TrimSpace(strings.ToLower(t))] = struct{}{}
				}
				var addedSubdomains []string
				for _, sd := range subdomainHosts {
					if _, exists := existingTargets[strings.ToLower(sd)]; !exists {
						addedSubdomains = append(addedSubdomains, sd)
					}
				}
				if len(addedSubdomains) > 0 {
					finalTarget = strings.TrimSpace(finalTarget) + "\n" + strings.Join(addedSubdomains, "\n")
					logx.Infof("Cron task %s: enableSubdomainPull pulled %d existing subdomains from DB (added %d new)",
						msg.CronTaskId, len(subdomainHosts), len(addedSubdomains))
				}
			}
		}
	}

	// 解析任务配置
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(finalConfig), &taskConfig); err != nil {
		return fmt.Errorf("failed to parse task config: %v", err)
	}

	// 生成新的任务ID
	newTaskId := uuid.New().String()

	// 创建新的 MainTask
	taskModel := svcCtx.GetMainTaskModel()
	newTask := &model.MainTask{
		TaskId:   newTaskId,
		Name:     fmt.Sprintf("%s (定时)", msg.TaskName),
		Target:   finalTarget,
		Config:   finalConfig,
		Status:   model.TaskStatusCreated,
		IsCron:   true,
		CronRule: msg.CronTaskId,
	}

	if err := taskModel.Insert(ctx, newTask); err != nil {
		return fmt.Errorf("failed to insert main task: %v", err)
	}

	logx.Infof("Created cron main task: taskId=%s, name=%s", newTaskId, newTask.Name)

	// 计算子任务数量（基于目标数量和启用的模块数）
	targets := strings.Split(finalTarget, "\n")
	var validTargets []string
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t != "" {
			validTargets = append(validTargets, t)
		}
	}

	// 计算启用的模块数（与 worker/worker.go 中的模块执行逻辑保持一致）
	// 规则：所有模块（含 portscan）必须显式 enable == true 才计数
	enabledModules := utils.CountEnabledModules(taskConfig)

	// 用于自动 batch 计算时避免除零；真实计数 enabledModules 在 subTaskCount 中单独使用
	modulesForBatching := enabledModules
	if modulesForBatching == 0 {
		modulesForBatching = 1
	}

	// 自动计算最佳批次大小
	// 子任务总数控制在 10~30 范围内，避免碎片化或过度聚合
	const (
		minSubTasks = 10
		maxSubTasks = 30
		minBatch    = 20
		maxBatch    = 200
	)
	targetCount := len(validTargets)
	optimalBatches := (minSubTasks + maxSubTasks) / 2 / modulesForBatching
	if optimalBatches < 1 {
		optimalBatches = 1
	}
	batchSize := targetCount / optimalBatches
	if batchSize < 1 {
		batchSize = 1
	}
	if batchSize < minBatch {
		batchSize = minBatch
	}
	if batchSize > maxBatch {
		batchSize = maxBatch
	}
	if targetCount <= minBatch {
		batchSize = targetCount
	}

	// 6/29 OOM 修复：针对"大目标 × 多模块"场景降低 batchSize
	// 当 targets × modules > 10000（如 2741×7=19187）时，单批 200 目标 × 7 模块
	// 会让 worker 单批任务内存压力过大（JSFinder 抓取外链累积响应缓冲）
	// 此类场景下降低单批到 50 目标，避免触发 worker OOM
	totalScanUnits := targetCount * enabledModules
	oomUnsafe := totalScanUnits > 10000
	if oomUnsafe && batchSize > 50 {
		batchSize = 50
		logx.Infof("Cron task %s: large scale (targets=%d × modules=%d = %d > 10000), reducing batchSize to %d",
			newTaskId, targetCount, enabledModules, totalScanUnits, batchSize)
	}

	// 如果用户显式设置了 batchSize > 0，优先使用
	// 修复 M1：但若处于 OOM 不安全场景且用户值 > 50，强制限制为 50，避免 OOM 保护被静默绕过
	if bs, ok := taskConfig["batchSize"].(float64); ok && bs > 0 {
		if oomUnsafe && int(bs) > 50 {
			logx.Errorf("Cron task %s: user batchSize=%d exceeds safe limit (50) for large scale (%d units), overriding to 50",
				newTaskId, int(bs), totalScanUnits)
		} else {
			batchSize = int(bs)
		}
	}
	logx.Infof("Cron task %s: auto-calculated batchSize=%d (targets=%d, modules=%d)", newTaskId, batchSize, targetCount, enabledModules)

	var batches []string
	for i := 0; i < len(validTargets); i += batchSize {
		end := i + batchSize
		if end > len(validTargets) {
			end = len(validTargets)
		}
		batches = append(batches, strings.Join(validTargets[i:end], "\n"))
	}
	if len(batches) == 0 {
		batches = []string{finalTarget}
	}

	// subTaskCount = 批次数 × (启用模块数 + 1)
	// 与 worker 端 expectedTaskIncr = CountEnabledModules + 1 口径一致（每模块 1 次 + 最终"完成" 1 次）。
	// 进度 = done / subTaskCount × 100。两侧口径必须一致，否则会出现 done > count 倒挂。
	// 无任何模块启用时，subTaskCount = 批次数。
	subTaskCount := len(batches) * (enabledModules + 1)
	if enabledModules == 0 {
		subTaskCount = len(batches)
	}

	// 更新任务状态为 STARTED
	now := time.Now()
	taskModel.Update(ctx, newTask.Id.Hex(), map[string]interface{}{
		"status":         model.TaskStatusPending,
		"sub_task_count": subTaskCount,
		"sub_task_done":  0,
		"start_time":     now,
	})

	// 保存主任务信息到 Redis
	taskInfoKey := "cscan:task:info:" + newTaskId
	taskInfoData, err := json.Marshal(map[string]interface{}{
		"mainTaskId":     newTask.Id.Hex(),
		"subTaskCount":   subTaskCount,
		"batchCount":     len(batches),
		"enabledModules": enabledModules,
	})
	if err != nil {
		logx.Errorf("Failed to marshal task info for redis: %v", err)
	} else {
		svcCtx.RedisClient.Set(ctx, taskInfoKey, taskInfoData, 24*time.Hour)
	}

	// 从配置中获取指定的 Worker 列表
	var workers []string
	if w, ok := taskConfig["workers"].([]interface{}); ok {
		for _, v := range w {
			if s, ok := v.(string); ok {
				workers = append(workers, s)
			}
		}
	}

	// 为每个批次创建子任务并推送到队列
	for i, batch := range batches {
		// 复制配置并替换目标
		subConfig := make(map[string]interface{})
		for k, v := range taskConfig {
			subConfig[k] = v
		}
		subConfig["target"] = batch
		subConfig["subTaskIndex"] = i
		subConfig["subTaskTotal"] = len(batches)
		subConfigBytes, err := json.Marshal(subConfig)
		if err != nil {
			logx.Errorf("Failed to marshal sub config: %v", err)
			continue
		}

		// 生成子任务ID
		subTaskId := newTaskId
		if len(batches) > 1 {
			subTaskId = newTaskId + "-" + strconv.Itoa(i)
		}

		schedTask := &scheduler.TaskInfo{
			TaskId:     subTaskId,
			MainTaskId: newTask.Id.Hex(),
			TaskName:   newTask.Name,
			Config:     string(subConfigBytes),
			Priority:   0,
			Workers:    workers,
		}

		logx.Infof("Pushing cron sub-task %d/%d: taskId=%s, targets=%d", i+1, len(batches), subTaskId, len(strings.Split(batch, "\n")))

		if err := sched.PushTask(ctx, schedTask); err != nil {
			logx.Errorf("Failed to push cron task to queue: %v", err)
			continue
		}

		// 保存子任务信息到 Redis（多批次时）
		if len(batches) > 1 {
			subTaskInfoKey := "cscan:task:info:" + subTaskId
			subTaskInfoData, err := json.Marshal(map[string]interface{}{
				"mainTaskId":   newTask.Id.Hex(),
				"subTaskCount": subTaskCount,
			})
			if err != nil {
				logx.Errorf("Failed to marshal sub task info for redis: %v", err)
			} else {
				svcCtx.RedisClient.Set(ctx, subTaskInfoKey, subTaskInfoData, 24*time.Hour)
			}
		}
	}

	logx.Infof("Cron task created and pushed: taskId=%s, batches=%d, subTaskCount=%d", newTaskId, len(batches), subTaskCount)
	return nil
}

// createAndPushSpaceEngineTask 创建空间引擎拉取任务（Fofa/Hunter/Quake），
// 复用 onlineapilogic.ImportAll 的在线搜索+资产导入逻辑，并创建一条标记为 space_engine 来源的 MainTask。
// 该任务不入扫描队列，而是在主侧 API 进程中同步执行（定时触发通常在夜间，执行时间可接受）。
func createAndPushSpaceEngineTask(ctx context.Context, svcCtx *svc.ServiceContext, sched *scheduler.Scheduler, msg *CronExecuteMessage) error {
	platform := strings.ToLower(strings.TrimSpace(msg.Platform))
	if platform != "fofa" && platform != "hunter" && platform != "quake" {
		return fmt.Errorf("unsupported platform for space engine task: %s", msg.Platform)
	}
	if strings.TrimSpace(msg.Query) == "" {
		return fmt.Errorf("empty query for space engine task, cronTaskId=%s", msg.CronTaskId)
	}

	// 1) 获取 API 配置
	configModel := model.NewAPIConfigModel(svcCtx.MongoDB)
	apiCfg, err := configModel.FindByPlatform(ctx, platform)
	if err != nil || apiCfg == nil {
		return fmt.Errorf("api key not configured for platform=%s", platform)
	}

	// 2) 获取模型
	assetModel := svcCtx.GetAssetModel()
	targetMetaModel := svcCtx.GetAssetTargetMetaModel()
	historyModel := svcCtx.GetAssetHistoryModel()

	// 3) 分页拉取并导入资产
	pageSize := 100
	if platform == "hunter" || platform == "quake" {
		pageSize = 100
	}

	// maxResults: 0或负数表示不限制（拉取全部），正数表示最大条数
	maxResults := msg.MaxResults
	hasMaxLimit := maxResults > 0
	maxPages := 0 // 0 = 不限制页数
	if hasMaxLimit {
		maxPages = (maxResults + pageSize - 1) / pageSize
		if maxPages < 1 {
			maxPages = 1
		}
	}

	totalFetched := 0
	totalImport := 0
	apiTotal := 0
	hasAPITotal := false
	currentPage := 1
	var lastErr error

	logx.Infof("Space-engine cron task started: cronTaskId=%s, platform=%s, query=%s, maxResults=%d (limit=%v), pageSize=%d",
		msg.CronTaskId, platform, msg.Query, maxResults, hasMaxLimit, pageSize)

PageLoop:
	for {
		if hasMaxLimit && currentPage > maxPages {
			logx.Infof("Space-engine reached max pages limit: maxPages=%d, stopping", maxPages)
			break
		}
		// 如果有API返回的total，且已经拉完了，停止
		if hasAPITotal && totalFetched >= apiTotal {
			logx.Infof("Space-engine fetched all data: totalFetched=%d >= apiTotal=%d, stopping", totalFetched, apiTotal)
			break
		}

		// 修复 M-18：根据剩余额度裁剪本页请求数量，避免整页导入后超量
		// 原实现 ceil(maxResults/pageSize) 向上取整后整页请求，例如 maxResults=150、pageSize=100 时会请求 2 页共 200 条
		effectivePageSize := pageSize
		if hasMaxLimit {
			remaining := maxResults - totalFetched
			if remaining <= 0 {
				logx.Infof("Space-engine reached maxResults=%d limit before fetch (fetched=%d), stopping", maxResults, totalFetched)
				break
			}
			if remaining < effectivePageSize {
				effectivePageSize = remaining
			}
		}

		var results []struct {
			Host, IP, Domain, Protocol, Title, Server, Country, City, Banner, Product string
			Port                                                                      int
		}
		rawResultCount := 0

		switch platform {
		case "fofa":
			client := onlineapi.NewFofaClient(apiCfg.Key, apiCfg.Version)
			result, err := client.Search(ctx, msg.Query, currentPage, effectivePageSize)
			if err != nil {
				if currentPage == 1 {
					lastErr = fmt.Errorf("fofa search page 1 failed: %v", err)
					break PageLoop
				}
				logx.Errorf("Space-engine fofa page=%d failed: %v, stopping", currentPage, err)
				break PageLoop
			}
			if currentPage == 1 && result.Size > 0 {
				apiTotal = result.Size
				hasAPITotal = true
				logx.Infof("Space-engine fofa API reports total=%d results", apiTotal)
			}
			rawResultCount = len(result.Results)
			assets := client.ParseResults(result)
			for _, a := range assets {
				results = append(results, struct {
					Host, IP, Domain, Protocol, Title, Server, Country, City, Banner, Product string
					Port                                                                      int
				}{a.Host, a.IP, a.Domain, a.Protocol, a.Title, a.Server, a.Country, a.City, a.Banner, a.Product, a.Port})
			}
		case "hunter":
			client := onlineapi.NewHunterClient(apiCfg.Key)
			result, err := client.Search(ctx, msg.Query, currentPage, effectivePageSize, "", "")
			if err != nil {
				if currentPage == 1 {
					lastErr = fmt.Errorf("hunter search page 1 failed: %v", err)
					break PageLoop
				}
				logx.Errorf("Space-engine hunter page=%d failed: %v, stopping", currentPage, err)
				break PageLoop
			}
			if currentPage == 1 {
				apiTotal = result.Data.Total
				hasAPITotal = true
				logx.Infof("Space-engine hunter API reports total=%d results, rest_quota=%s",
					apiTotal, result.Data.RestQuota)
			}
			rawResultCount = len(result.Data.Arr)
			for _, a := range result.Data.Arr {
				component := ""
				if len(a.Component) > 0 {
					component = a.Component[0].Name
				}
				results = append(results, struct {
					Host, IP, Domain, Protocol, Title, Server, Country, City, Banner, Product string
					Port                                                                      int
				}{a.URL, a.IP, a.Domain, a.Protocol, a.WebTitle, component, a.Country, a.City, a.Banner, component, a.Port})
			}
		case "quake":
			client := onlineapi.NewQuakeClient(apiCfg.Key)
			result, err := client.Search(ctx, msg.Query, currentPage, effectivePageSize)
			if err != nil {
				if currentPage == 1 {
					lastErr = fmt.Errorf("quake search page 1 failed: %v", err)
					break PageLoop
				}
				logx.Errorf("Space-engine quake page=%d failed: %v, stopping", currentPage, err)
				break PageLoop
			}
			if result.Data.IsExhausted {
				logx.Infof("Space-engine quake quota exhausted at page=%d, stopping", currentPage)
				break PageLoop
			}
			if currentPage == 1 {
				apiTotal = result.Meta.Pagination.Total
				hasAPITotal = true
				logx.Infof("Space-engine quake API reports total=%d results", apiTotal)
			}
			rawResultCount = len(result.Data.Items)
			for _, a := range result.Data.Items {
				results = append(results, struct {
					Host, IP, Domain, Protocol, Title, Server, Country, City, Banner, Product string
					Port                                                                      int
				}{a.Service.HTTP.Host, a.IP, "", a.Service.Name, a.Service.HTTP.Title, a.Service.HTTP.Server, a.Location.CountryCN, a.Location.CityCN, "", "", a.Port})
			}
		}

		if rawResultCount == 0 {
			logx.Infof("Space-engine page=%d returned 0 results, stopping", currentPage)
			break
		}
		totalFetched += rawResultCount
		logx.Infof("Space-engine page=%d: got %d results (total fetched=%d)", currentPage, rawResultCount, totalFetched)

		// 导入当前页资产
		importedThisPage := 0
		for _, a := range results {
			// 修复 M-18：防御性截断：本页实际结果可能因 API 不支持精确分页而超出剩余额度
			if hasMaxLimit && totalFetched-rawResultCount+importedThisPage >= maxResults {
				logx.Infof("Space-engine truncating page results at maxResults=%d", maxResults)
				break
			}
			asset := onlineapi.BuildAsset(a.Host, a.IP, a.Domain, a.Protocol, a.Title, a.Server, a.Country, a.City, a.Banner, a.Product, a.Port, platform)
			asset.Source = "space_engine-" + platform
			if asset.Host == "" {
				continue
			}
			res, err := assetModel.UpsertWithResult(ctx, asset)
			if err == nil {
				totalImport++
				if res.IsNew {
					// 记录首次发现历史，确保时间线不为空
					newAsset, _ := assetModel.FindByAuthorityOnly(ctx, asset.Authority)
					if newAsset != nil {
						firstFound := model.SnapshotFromAsset(newAsset, msg.CronTaskId, time.Now(), nil)
						if histErr := historyModel.Insert(ctx, firstFound); histErr != nil {
							logx.Errorf("[SpaceEngine] Insert first-found history failed: %v", histErr)
						}
					}
				}
			}
			// 同步创建/更新顶层资产（AssetTargetMeta），确保资产出现在资产概览中
			// 使用 BuildAsset 清理后的 asset.Host（已去除URL前缀和端口）和 asset.Domain
			if err := targetMetaModel.EnsureForAsset(ctx, asset.Host, asset.Domain, nil); err != nil {
				logx.Errorf("Failed to ensure target meta for host=%s: %v", asset.Host, err)
			}
			importedThisPage++
		}

		// 如果有限制且已达到上限，停止
		if hasMaxLimit && totalFetched >= maxResults {
			logx.Infof("Space-engine reached maxResults=%d limit (fetched=%d), stopping", maxResults, totalFetched)
			break
		}
		currentPage++
	}

	// 4) 记录结果日志
	logx.Infof("Space-engine cron task finished: cronTaskId=%s, platform=%s, fetched=%d, imported=%d, apiTotal=%d, err=%v",
		msg.CronTaskId, platform, totalFetched, totalImport, apiTotal, lastErr)

	// 失效统计缓存，确保 Dashboard 与列表页数据一致
	if totalImport > 0 {
		svcCtx.QueryCache.Clear()
	}

	if lastErr != nil {
		return lastErr
	}
	return nil
}

// isIPAddress 判断字符串是否为IP地址
func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

// isLocalDev 判断是否处于本地开发模式，可豁免 JWT secret 强校验。
// 触发条件（满足任一即可）：
//   - 显式设置环境变量 CSCAN_DEV=1
//   - 配置文件 Mode: dev
//   - 进程由 `go run` 启动（临时二进制路径包含 "go-build"）
//
// 该判定仅用于开发期随机 secret 豁免，不影响任何业务逻辑；生产环境以 Docker
// 镜像中的编译二进制运行，路径不含 "go-build" 且已注入 CSCAN_JWT_SECRET，不会误判。
func isLocalDev(c config.Config) bool {
	if os.Getenv("CSCAN_DEV") == "1" {
		return true
	}
	if c.Mode == "dev" {
		return true
	}
	if isGoRun() {
		return true
	}
	return false
}

// isGoRun 通过可执行文件路径判断进程是否由 `go run` 启动。
// go run 会在系统临时目录下生成 go-build*/bNNN/exe/<name> 临时二进制并立即执行，
// 其路径恒定包含 "go-build"，可作为本地开发（而非生产编译二进制）的可靠信号。
func isGoRun() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(exe), "go-build")
}

// extractRootDomain 从域名中提取根域名（简单实现：取最后两段）
func extractRootDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimSuffix(domain, ".")
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return ""
	}
	// 处理常见的二级域名后缀（.com.cn, .co.jp 等）
	suffixes := map[string]bool{
		"com.cn": true, "net.cn": true, "org.cn": true, "gov.cn": true, "edu.cn": true, "ac.cn": true,
		"co.uk": true, "org.uk": true, "me.uk": true,
		"co.jp": true, "ne.jp": true, "or.jp": true, "ac.jp": true,
		"com.hk": true, "org.hk": true, "edu.hk": true,
		"com.au": true, "net.au": true, "org.au": true, "co.nz": true, "net.nz": true,
		"co.za": true, "org.za": true, "co.in": true, "net.in": true, "org.in": true,
		"com.br": true, "com.mx": true, "com.tw": true, "com.sg": true,
	}
	if len(parts) >= 3 {
		twoLevel := parts[len(parts)-2] + "." + parts[len(parts)-1]
		if suffixes[twoLevel] && len(parts) >= 3 {
			return strings.Join(parts[len(parts)-3:], ".")
		}
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

const (
	orphanedTaskCheckInterval = 5 * time.Minute
	orphanedTaskThreshold     = 10 * time.Minute
)

// startOrphanedTaskRecovery 启动孤儿任务恢复后台任务
// 定期检查并恢复卡住的任务（状态为 STARTED 但长时间没有更新的任务）
func startOrphanedTaskRecovery(svcCtx *svc.ServiceContext) {
	logx.Info("Orphaned task recovery background job started")

	ticker := time.NewTicker(orphanedTaskCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		// 优先通过 Redis 心跳快速检测离线 Worker 的任务
		logic.RecoverOrphanedByHeartbeat(context.Background(), svcCtx)
		// 兜底：通过 MongoDB update_time 阈值检测卡住的任务
		logic.RecoverOrphanedTasks(context.Background(), svcCtx, orphanedTaskThreshold)
		// MongoDB 兜底恢复：直接查询 STARTED 状态超过 30 分钟未更新的任务
		logic.RecoverStaleMongoTasks(context.Background(), svcCtx, 30*time.Minute)
		logic.CleanupStaleProcessingTasks(context.Background(), svcCtx, "")
	}
}

// cronTaskSourceAdapter 适配器：将model.CronTaskModel适配为scheduler.CronTaskSource接口
type cronTaskSourceAdapter struct {
	model *model.CronTaskModel
}

func (a *cronTaskSourceAdapter) FindAllCronTasks(ctx context.Context) ([]scheduler.CronTaskData, error) {
	tasks, err := a.model.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]scheduler.CronTaskData, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, scheduler.CronTaskData{
			CronTaskId:          t.CronTaskId,
			Name:                t.Name,
			TaskType:            t.TaskType,
			ScheduleType:        t.ScheduleType,
			CronSpec:            t.CronSpec,
			ScheduleTime:        t.ScheduleTime,
			Status:              t.Status,
			LastRunTime:         t.LastRunTime,
			NextRunTime:         t.NextRunTime,
			TargetMode:          t.TargetMode,
			Target:              t.Target,
			AssetIds:            t.AssetIds,
			OrgId:               t.OrgId,
			EnableSubdomainPull: t.EnableSubdomainPull,
			ConfigSource:        t.ConfigSource,
			TemplateId:          t.TemplateId,
			Config:              t.Config,
			Platform:            t.Platform,
			Query:               t.Query,
			MaxResults:          t.MaxResults,
		})
	}
	return result, nil
}

func (a *cronTaskSourceAdapter) FindCronTaskByCronTaskId(ctx context.Context, cronTaskId string) (*scheduler.CronTaskData, error) {
	t, err := a.model.FindByCronTaskId(ctx, cronTaskId)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	return &scheduler.CronTaskData{
		CronTaskId:          t.CronTaskId,
		Name:                t.Name,
		TaskType:            t.TaskType,
		ScheduleType:        t.ScheduleType,
		CronSpec:            t.CronSpec,
		ScheduleTime:        t.ScheduleTime,
		Status:              t.Status,
		LastRunTime:         t.LastRunTime,
		NextRunTime:         t.NextRunTime,
		TargetMode:          t.TargetMode,
		Target:              t.Target,
		AssetIds:            t.AssetIds,
		OrgId:               t.OrgId,
		EnableSubdomainPull: t.EnableSubdomainPull,
		ConfigSource:        t.ConfigSource,
		TemplateId:          t.TemplateId,
		Config:              t.Config,
		Platform:            t.Platform,
		Query:               t.Query,
		MaxResults:          t.MaxResults,
	}, nil
}

func (a *cronTaskSourceAdapter) UpdateCronTaskRunInfo(ctx context.Context, cronTaskId string, lastRunTime, nextRunTime, status string) error {
	update := bson.M{
		"last_run_time": lastRunTime,
		"next_run_time": nextRunTime,
		"status":        status,
	}
	return a.model.UpdateByCronTaskId(ctx, cronTaskId, update)
}
