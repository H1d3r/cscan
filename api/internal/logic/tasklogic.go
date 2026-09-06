package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/internal/scheduler"
	"cscan/pkg/utils"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type MainTaskListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// hasAnyScanPhaseEnabled 检查任务配置中是否至少启用了一项扫描阶段
// 扫描阶段包括：子域名扫描/端口扫描/端口识别/指纹识别/弱口令扫描/目录扫描/JS扫描/漏洞扫描
func hasAnyScanPhaseEnabled(taskConfig map[string]interface{}) bool {
	phases := []string{"domainscan", "portscan", "portidentify", "fingerprint", "brutescan", "dirscan", "jsfinder", "pocscan"}
	for _, phase := range phases {
		section, ok := taskConfig[phase].(map[string]interface{})
		if !ok {
			continue
		}
		if enable, ok := section["enable"].(bool); ok && enable {
			return true
		}
	}
	return false
}

func NewMainTaskListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskListLogic {
	return &MainTaskListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskListLogic) MainTaskList(req *types.MainTaskListReq) (resp *types.MainTaskListResp, err error) {
	// 设置查询超时
	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	defer cancel()

	// 构建查询条件
	filter := bson.M{}
	if req.Name != "" {
		// 修复 M-18：转义用户输入中的正则元字符，防止 ReDoS 和意外匹配
		filter["name"] = bson.M{"$regex": regexp.QuoteMeta(req.Name), "$options": "i"}
	}
	if req.Status != "" {
		filter["status"] = req.Status
	}
	if len(req.Tags) > 0 {
		filter["tags"] = bson.M{"$in": req.Tags}
	}

	taskModel := l.svcCtx.GetMainTaskModel()

	// 查询总数
	total, err := taskModel.Count(ctx, filter)
	if err != nil {
		return &types.MainTaskListResp{Code: 500, Msg: "查询失败"}, nil
	}

	page, pageSize := model.NormalizePage(req.Page, req.PageSize)

	// 查询列表
	tasks, err := taskModel.Find(ctx, filter, page, pageSize)
	if err != nil {
		return &types.MainTaskListResp{Code: 500, Msg: "查询失败"}, nil
	}

	// 转换响应
	list := make([]types.MainTask, 0, len(tasks))

	// 批量获取Redis进度数据，减少Redis调用次数
	progressMap := make(map[string]map[string]interface{})
	if l.svcCtx.RedisClient != nil {
		taskIds := make([]string, 0, len(tasks))
		for _, t := range tasks {
			if t.Status == "STARTED" || t.Status == "PENDING" {
				taskIds = append(taskIds, t.TaskId)
			}
		}

		// 使用Pipeline批量获取
		if len(taskIds) > 0 {
			pipe := l.svcCtx.RedisClient.Pipeline()
			cmds := make(map[string]*redis.StringCmd)
			for _, taskId := range taskIds {
				mainKey := fmt.Sprintf("cscan:task:progress:%s", taskId)
				cmds[taskId] = pipe.Get(ctx, mainKey)
			}
			pipe.Exec(ctx)

			for taskId, cmd := range cmds {
				if data, err := cmd.Result(); err == nil && data != "" {
					var progressData map[string]interface{}
					if json.Unmarshal([]byte(data), &progressData) == nil {
						progressMap[taskId] = progressData
					}
				}
			}
		}
	}

	for _, t := range tasks {
		progress := t.Progress
		currentPhase := t.CurrentPhase
		subTaskDone := t.SubTaskDone
		status := t.Status

		// 如果状态为空，根据进度推断状态（兼容旧数据）
		if status == "" {
			if progress >= 100 || (t.SubTaskCount > 0 && subTaskDone >= t.SubTaskCount) {
				status = "SUCCESS"
			} else if progress > 0 || subTaskDone > 0 {
				status = "STARTED"
			} else {
				status = "CREATED"
			}
		}

		// 如果任务正在执行中或等待执行，尝试从Redis获取实时进度和当前阶段
		if (status == "STARTED" || status == "PENDING") && l.svcCtx.RedisClient != nil {
			// 使用批量获取的数据
			if progressData, ok := progressMap[t.TaskId]; ok {
				if phase, ok := progressData["currentPhase"].(string); ok && phase != "" {
					currentPhase = phase
				}
			}

			// 基于子任务完成数计算进度
			subTaskCount := t.SubTaskCount
			if subTaskCount <= 0 {
				subTaskCount = 1 // 兼容旧任务
			}

			// 进度 = 已完成子任务数 / 总子任务数 * 100
			if subTaskCount > 0 {
				progress = subTaskDone * 100 / subTaskCount
				// 确保进度不超过100%（防止异常数据）
				if progress > 100 {
					progress = 100
				}
				// 未全部完成时最多显示99%
				if progress > 99 && subTaskDone < subTaskCount {
					progress = 99
				}
			}
		}

		// 格式化开始时间和结束时间
		startTime := ""
		endTime := ""
		if t.StartTime != nil {
			startTime = t.StartTime.Local().Format("2006-01-02 15:04:05")
		}
		if t.EndTime != nil {
			endTime = t.EndTime.Local().Format("2006-01-02 15:04:05")
		}

		list = append(list, types.MainTask{
			Id:           t.Id.Hex(),
			TaskId:       t.TaskId, // UUID，用于日志查询
			Name:         t.Name,
			Target:       t.Target,
			Config:       t.Config,
			ProfileId:    t.ProfileId,
			ProfileName:  t.ProfileName,
			Tags:         t.Tags,
			Status:       status,
			CurrentPhase: currentPhase,
			Progress:     progress,
			Result:       t.Result,
			IsCron:       t.IsCron,
			CronRule:     t.CronRule,
			CreateTime:   t.CreateTime.Local().Format("2006-01-02 15:04:05"),
			StartTime:    startTime,
			EndTime:      endTime,
			SubTaskCount: t.SubTaskCount,
			SubTaskDone:  subTaskDone,
			ScanSummary:  t.ScanSummary,
		})
	}

	return &types.MainTaskListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
}

type MainTaskDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskDetailLogic {
	return &MainTaskDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// MainTaskDetail 按 id 查询单个任务详情，复用列表的状态推断与实时进度逻辑。
func (l *MainTaskDetailLogic) MainTaskDetail(req *types.MainTaskDetailReq) (*types.MainTaskDetailResp, error) {
	if req.Id == "" {
		return &types.MainTaskDetailResp{Code: 400, Msg: "id不能为空"}, nil
	}

	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	defer cancel()

	task, err := l.svcCtx.GetMainTaskModel().FindById(ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskDetail: query failed, id=%s, err=%v", req.Id, err)
		return &types.MainTaskDetailResp{Code: 500, Msg: "查询失败"}, nil
	}
	if task == nil {
		return &types.MainTaskDetailResp{Code: 404, Msg: "任务不存在"}, nil
	}

	t := *task
	progress := t.Progress
	currentPhase := t.CurrentPhase
	subTaskDone := t.SubTaskDone
	status := t.Status

	// 状态为空时按进度推断（兼容旧数据）
	if status == "" {
		if progress >= 100 || (t.SubTaskCount > 0 && subTaskDone >= t.SubTaskCount) {
			status = "SUCCESS"
		} else if progress > 0 || subTaskDone > 0 {
			status = "STARTED"
		} else {
			status = "CREATED"
		}
	}

	// 执行中/等待中：从 Redis 取实时 currentPhase 并按子任务重算进度
	if (status == "STARTED" || status == "PENDING") && l.svcCtx.RedisClient != nil {
		mainKey := fmt.Sprintf("cscan:task:progress:%s", t.TaskId)
		if data, err := l.svcCtx.RedisClient.Get(ctx, mainKey).Result(); err == nil && data != "" {
			var progressData map[string]interface{}
			if json.Unmarshal([]byte(data), &progressData) == nil {
				if phase, ok := progressData["currentPhase"].(string); ok && phase != "" {
					currentPhase = phase
				}
			}
		}
		subTaskCount := t.SubTaskCount
		if subTaskCount <= 0 {
			subTaskCount = 1
		}
		if subTaskCount > 0 {
			progress = subTaskDone * 100 / subTaskCount
			if progress > 100 {
				progress = 100
			}
			if progress > 99 && subTaskDone < subTaskCount {
				progress = 99
			}
		}
	}

	startTime := ""
	endTime := ""
	if t.StartTime != nil {
		startTime = t.StartTime.Local().Format("2006-01-02 15:04:05")
	}
	if t.EndTime != nil {
		endTime = t.EndTime.Local().Format("2006-01-02 15:04:05")
	}

	return &types.MainTaskDetailResp{
		Code: 0,
		Msg:  "success",
		Data: types.MainTask{
			Id:           t.Id.Hex(),
			TaskId:       t.TaskId,
			Name:         t.Name,
			Target:       t.Target,
			Config:       t.Config,
			ProfileId:    t.ProfileId,
			ProfileName:  t.ProfileName,
			Tags:         t.Tags,
			Status:       status,
			CurrentPhase: currentPhase,
			Progress:     progress,
			Result:       t.Result,
			IsCron:       t.IsCron,
			CronRule:     t.CronRule,
			CreateTime:   t.CreateTime.Local().Format("2006-01-02 15:04:05"),
			StartTime:    startTime,
			EndTime:      endTime,
			SubTaskCount: t.SubTaskCount,
			SubTaskDone:  subTaskDone,
			ScanSummary:  t.ScanSummary,
		},
	}, nil
}

type MainTaskCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskCreateLogic {
	return &MainTaskCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskCreateLogic) MainTaskCreate(req *types.MainTaskCreateReq) (resp *types.BaseRespWithId, err error) {
	// 校验目标格式
	if req.Target == "" {
		return &types.BaseRespWithId{Code: 400, Msg: "扫描目标不能为空"}, nil
	}
	if validationErrors := common.ValidateTargets(req.Target); len(validationErrors) > 0 {
		return &types.BaseRespWithId{Code: 400, Msg: common.FormatValidationErrors(validationErrors)}, nil
	}

	taskModel := l.svcCtx.GetMainTaskModel()

	// 构建任务配置
	taskConfig := map[string]interface{}{
		"target": req.Target,
	}

	// 添加组织ID到配置
	if req.OrgId != "" {
		taskConfig["orgId"] = req.OrgId
	}

	// 添加指定 Worker 列表到配置
	if len(req.Workers) > 0 {
		taskConfig["workers"] = req.Workers
	}

	// 优先使用直接传递的 config，否则从 template 或 profile 获取
	profileName := "自定义配置"
	if req.Config != "" {
		// 直接使用传递的配置
		var directConfig map[string]interface{}
		if err := json.Unmarshal([]byte(req.Config), &directConfig); err == nil {
			for k, v := range directConfig {
				taskConfig[k] = v
			}
		}
	} else if req.TemplateId != "" {
		// 从扫描配置模板获取配置
		template, err := l.svcCtx.ScanTemplateModel.FindById(l.ctx, req.TemplateId)
		if err != nil {
			l.Logger.Errorf("MainTaskCreate: find template failed, templateId=%s, error=%v", req.TemplateId, err)
			return &types.BaseRespWithId{Code: 500, Msg: "查询模板失败"}, nil
		}
		if template == nil {
			return &types.BaseRespWithId{Code: 400, Msg: "扫描模板不存在"}, nil
		}
		profileName = template.Name
		if template.Config != "" {
			var templateConfig map[string]interface{}
			if err := json.Unmarshal([]byte(template.Config), &templateConfig); err == nil {
				for k, v := range templateConfig {
					taskConfig[k] = v
				}
			}
		}
		// 增加模板使用计数
		l.svcCtx.ScanTemplateModel.IncrUseCount(l.ctx, req.TemplateId)
	} else if req.ProfileId != "" {
		// 从 profile 获取配置（兼容旧版）
		profile, err := l.svcCtx.ProfileModel.FindById(l.ctx, req.ProfileId)
		if err != nil {
			l.Logger.Errorf("MainTaskCreate: find profile failed, profileId=%s, error=%v", req.ProfileId, err)
			return &types.BaseRespWithId{Code: 500, Msg: "查询任务配置失败"}, nil
		}
		if profile == nil {
			return &types.BaseRespWithId{Code: 400, Msg: "任务配置不存在"}, nil
		}
		profileName = profile.Name
		if profile.Config != "" {
			var profileConfig map[string]interface{}
			if err := json.Unmarshal([]byte(profile.Config), &profileConfig); err == nil {
				for k, v := range profileConfig {
					taskConfig[k] = v
				}
			}
		}
	}

	// 注入自定义POC和标签映射
	taskConfig = common.InjectPocConfig(l.ctx, l.svcCtx, taskConfig, l.Logger)

	// 校验：至少启用一项扫描配置
	if !hasAnyScanPhaseEnabled(taskConfig) {
		return &types.BaseRespWithId{Code: 400, Msg: "请至少启用一项扫描配置（子域名扫描、端口扫描、端口识别、指纹识别、目录扫描、漏洞扫描、弱口令扫描或JS扫描）"}, nil
	}

	configBytes, _ := json.Marshal(taskConfig)

	configStr := string(configBytes)
	logLen := len(configStr)
	if logLen > 500 {
		logLen = 500
	}
	l.Logger.Infof("MainTaskCreate: config length=%d, config=%s", len(configStr), configStr[:logLen])

	// 创建主任务
	taskId := uuid.New().String()
	task := &model.MainTask{
		TaskId:      taskId,
		Name:        req.Name,
		Target:      req.Target,
		ProfileId:   req.ProfileId,
		ProfileName: profileName,
		OrgId:       req.OrgId,
		Tags:        req.Tags,
		Config:      string(configBytes),
		Status:      model.TaskStatusCreated,
		CreatedBy:   middleware.GetUserId(l.ctx),
	}

	if err := taskModel.Insert(l.ctx, task); err != nil {
		l.Logger.Errorf("MainTaskCreate: insert failed, taskId=%s, error=%v", taskId, err)
		return &types.BaseRespWithId{Code: 500, Msg: "创建任务失败: " + err.Error()}, nil
	}

	// 任务创建即登记顶层目标（pending），资产空间搜索立即可见
	l.svcCtx.GetAssetTargetMetaModel().RegisterScanTargets(l.ctx, utils.SplitTargetTokens(req.Target), "pending")

	l.Logger.Infof("Task created: taskId=%s", taskId)

	// 使用 TaskBuilder 统一处理任务启动逻辑
	builder := common.NewTaskBuilder(l.ctx, l.svcCtx)
	batchCount, err := builder.BuildAndPushSubTasks(task, taskConfig)
	if err != nil {
		l.Logger.Errorf("MainTaskCreate: failed to start task %s: %v", taskId, err)
		// 注意：任务已创建但启动失败，用户可以在前端点击重试/开始
	} else {
		l.Logger.Infof("Task created and started: taskId=%s, batches=%d", taskId, batchCount)
	}

	return &types.BaseRespWithId{Code: 0, Msg: "任务创建成功", Id: task.Id.Hex()}, nil
}

type TaskProfileListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskProfileListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TaskProfileListLogic {
	return &TaskProfileListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TaskProfileListLogic) TaskProfileList() (resp *types.TaskProfileListResp, err error) {
	profiles, err := l.svcCtx.ProfileModel.FindAll(l.ctx)
	if err != nil {
		return &types.TaskProfileListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.TaskProfile, 0, len(profiles))
	for _, p := range profiles {
		list = append(list, types.TaskProfile{
			Id:          p.Id.Hex(),
			Name:        p.Name,
			Description: p.Description,
			Config:      p.Config,
		})
	}

	return &types.TaskProfileListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

// TaskProfileSaveLogic
type TaskProfileSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskProfileSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TaskProfileSaveLogic {
	return &TaskProfileSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TaskProfileSaveLogic) TaskProfileSave(req *types.TaskProfileSaveReq) (resp *types.BaseResp, err error) {
	profile := &model.TaskProfile{
		Name:        req.Name,
		Description: req.Description,
		Config:      req.Config,
	}

	if req.Id != "" {
		// 更新
		err = l.svcCtx.ProfileModel.Update(l.ctx, req.Id, profile)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新失败"}, nil
		}
	} else {
		// 新增
		err = l.svcCtx.ProfileModel.Insert(l.ctx, profile)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "创建失败"}, nil
		}
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

// TaskProfileDeleteLogic
type TaskProfileDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskProfileDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TaskProfileDeleteLogic {
	return &TaskProfileDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TaskProfileDeleteLogic) TaskProfileDelete(req *types.TaskProfileDeleteReq) (resp *types.BaseResp, err error) {
	err = l.svcCtx.ProfileModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// MainTaskDeleteLogic 单个删除
type MainTaskDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskDeleteLogic {
	return &MainTaskDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskDeleteLogic) MainTaskDelete(req *types.MainTaskDeleteReq) (resp *types.BaseResp, err error) {
	taskModel := l.svcCtx.GetMainTaskModel()

	// Notify only the exact active generation; plaintext task-ID controls are
	// intentionally unsupported by leased-task-v1 workers.
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err == nil && task != nil {
		PublishDeleteStopSoon(l.ctx, l.svcCtx, task)
	}

	// 先删除父文档，成功后再级联删除子数据（避免父删除失败时子数据已丢失）
	err = taskModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}

	if task != nil {
		l.cascadeDeleteTaskData(task.TaskId, task.Id.Hex())
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
}

// cascadeDeleteTaskData 级联删除任务相关数据（资产、漏洞、扫描结果、目录扫描、JSFinder、执行任务）
func (l *MainTaskDeleteLogic) cascadeDeleteTaskData(taskId, mainTaskId string) {
	assetModel := l.svcCtx.GetAssetModel()
	vulModel := l.svcCtx.GetVulModel()
	scanResultModel := model.NewScanResultModel(l.svcCtx.MongoDB)
	dirscanModel := l.svcCtx.GetDirScanResultModel()
	jsfinderModel := l.svcCtx.GetJSFinderResultModel()

	// 按 taskId 删除资产
	if _, err := assetModel.DeleteByTaskId(l.ctx, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete asset failed, taskId=%s, error=%v", taskId, err)
	}
	// 按 taskId 删除漏洞
	if err := vulModel.DeleteByTaskId(l.ctx, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete vul failed, taskId=%s, error=%v", taskId, err)
	}
	// 按 jobID 删除扫描结果
	if _, err := scanResultModel.DeleteByJobID(l.ctx, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete scanresult failed, taskId=%s, error=%v", taskId, err)
	}
	// 按 task_id 删除目录扫描结果
	if _, err := dirscanModel.DeleteByFilter(l.ctx, bson.M{"task_id": taskId}); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete dirscan failed, taskId=%s, error=%v", taskId, err)
	}
	// 按 task_id 删除 JSFinder 结果
	if _, err := jsfinderModel.DeleteMany(l.ctx, bson.M{"task_id": taskId}); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete jsfinder failed, taskId=%s, error=%v", taskId, err)
	}
	// 按 main_task_id 删除执行任务
	executorModel := l.svcCtx.GetExecutorTaskModel()
	if _, err := executorModel.DeleteByMainTaskId(l.ctx, mainTaskId, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete executor_task failed, mainTaskId=%s, error=%v", mainTaskId, err)
	}
	l.Logger.Infof("cascadeDeleteTaskData: all related data deleted for taskId=%s", taskId)
}

// MainTaskBatchDeleteLogic 批量删除
type MainTaskBatchDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskBatchDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskBatchDeleteLogic {
	return &MainTaskBatchDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// cascadeDeleteTaskData 级联删除任务相关数据（资产、漏洞、扫描结果、执行任务）
func (l *MainTaskBatchDeleteLogic) cascadeDeleteTaskData(taskId, mainTaskId string) {
	assetModel := l.svcCtx.GetAssetModel()
	vulModel := l.svcCtx.GetVulModel()
	scanResultModel := model.NewScanResultModel(l.svcCtx.MongoDB)

	// 按 taskId 删除资产
	if _, err := assetModel.DeleteByTaskId(l.ctx, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete assets failed, taskId=%s, error=%v", taskId, err)
	}
	// 按 taskId 删除漏洞
	if err := vulModel.DeleteByTaskId(l.ctx, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete vuls failed, taskId=%s, error=%v", taskId, err)
	}
	// 按 jobID 删除扫描结果
	if _, err := scanResultModel.DeleteByJobID(l.ctx, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete scan results failed, taskId=%s, error=%v", taskId, err)
	}

	executorModel := l.svcCtx.GetExecutorTaskModel()
	if _, err := executorModel.DeleteByMainTaskId(l.ctx, mainTaskId, taskId); err != nil {
		l.Logger.Errorf("cascadeDeleteTaskData: delete executor_task failed, mainTaskId=%s, error=%v", mainTaskId, err)
	}
	l.Logger.Infof("cascadeDeleteTaskData: all related data deleted for taskId=%s", taskId)
}

func (l *MainTaskBatchDeleteLogic) MainTaskBatchDelete(req *types.MainTaskBatchDeleteReq) (resp *types.BaseResp, err error) {
	if len(req.Ids) == 0 {
		return &types.BaseResp{Code: 400, Msg: "请选择要删除的任务"}, nil
	}

	taskModel := l.svcCtx.GetMainTaskModel()

	// Resolve and publish only exact-generation controls before deleting data.
	for _, id := range req.Ids {
		task, err := taskModel.FindById(l.ctx, id)
		if err == nil && task != nil {
			PublishDeleteStopSoon(l.ctx, l.svcCtx, task)

			// 级联删除任务相关数据
			l.cascadeDeleteTaskData(task.TaskId, task.Id.Hex())
		}
	}

	deleted, err := taskModel.BatchDelete(l.ctx, req.Ids)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "成功删除 " + strconv.FormatInt(deleted, 10) + " 条任务"}, nil
}

// MainTaskRetryLogic 重新执行任务
type MainTaskRetryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskRetryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskRetryLogic {
	return &MainTaskRetryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskRetryLogic) MainTaskRetry(req *types.MainTaskRetryReq) (resp *types.BaseRespWithId, err error) {
	taskModel := l.svcCtx.GetMainTaskModel()
	oldTask, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskRetry: find task failed, id=%s, error=%v", req.Id, err)
		return &types.BaseRespWithId{Code: 500, Msg: "查询任务失败"}, nil
	}
	if oldTask == nil {
		return &types.BaseRespWithId{Code: 400, Msg: "任务不存在"}, nil
	}

	// 构建任务配置
	taskConfig := map[string]interface{}{
		"target": oldTask.Target,
	}

	// 优先使用任务自带的Config（资产管理下发的任务），否则从ProfileId获取
	if oldTask.Config != "" {
		// 任务自带配置（资产管理下发的任务）
		var savedConfig map[string]interface{}
		if err := json.Unmarshal([]byte(oldTask.Config), &savedConfig); err == nil {
			for k, v := range savedConfig {
				taskConfig[k] = v
			}
		}
	} else if oldTask.ProfileId != "" {
		// 从配置模板获取（任务管理创建的任务）
		profile, err := l.svcCtx.ProfileModel.FindById(l.ctx, oldTask.ProfileId)
		if err != nil {
			l.Logger.Errorf("MainTaskRestart: find profile failed, profileId=%s, error=%v", oldTask.ProfileId, err)
			return &types.BaseRespWithId{Code: 500, Msg: "查询任务配置失败"}, nil
		}
		if profile == nil {
			return &types.BaseRespWithId{Code: 400, Msg: "任务配置不存在"}, nil
		}
		if profile.Config != "" {
			var profileConfig map[string]interface{}
			if err := json.Unmarshal([]byte(profile.Config), &profileConfig); err == nil {
				for k, v := range profileConfig {
					taskConfig[k] = v
				}
			}
		}
	} else {
		return &types.BaseRespWithId{Code: 400, Msg: "任务配置不存在"}, nil
	}

	// 注入自定义POC和标签映射
	taskConfig = common.InjectPocConfig(l.ctx, l.svcCtx, taskConfig, l.Logger)
	configBytes, _ := json.Marshal(taskConfig)

	// 创建新任务（而不是复用旧任务）
	newTaskId := uuid.New().String()
	newTask := &model.MainTask{
		TaskId:      newTaskId,
		Name:        oldTask.Name + " (重试)",
		Target:      oldTask.Target,
		ProfileId:   oldTask.ProfileId,
		ProfileName: oldTask.ProfileName,
		OrgId:       oldTask.OrgId,
		Tags:        oldTask.Tags,
		Config:      string(configBytes),
		Status:      model.TaskStatusCreated, // 设置初始状态
		CreatedBy:   middleware.GetUserId(l.ctx),
	}

	if err := taskModel.Insert(l.ctx, newTask); err != nil {
		l.Logger.Errorf("MainTaskRetry: insert new task failed, error=%v", err)
		return &types.BaseRespWithId{Code: 500, Msg: "创建新任务失败: " + err.Error()}, nil
	}

	// 使用 TaskBuilder 统一处理任务启动逻辑
	builder := common.NewTaskBuilder(l.ctx, l.svcCtx)
	batchCount, err := builder.BuildAndPushSubTasks(newTask, taskConfig)
	if err != nil {
		l.Logger.Errorf("MainTaskRetry: failed to start task %s: %v", newTaskId, err)
	} else {
		l.Logger.Infof("MainTaskRetry: task %s started with %d batches", newTaskId, batchCount)
	}

	return &types.BaseRespWithId{Code: 0, Msg: "已创建新任务并开始执行", Id: newTask.Id.Hex()}, nil
}

// MainTaskStartLogic 启动任务
type MainTaskStartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskStartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskStartLogic {
	return &MainTaskStartLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskStartLogic) MainTaskStart(req *types.MainTaskControlReq) (resp *types.BaseResp, err error) {
	l.Logger.Infof("[MainTaskStart] ========== START ==========")
	l.Logger.Infof("[MainTaskStart] id=%s", req.Id)

	taskModel := l.svcCtx.GetMainTaskModel()
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskStart: find task failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "查询任务失败"}, nil
	}
	if task == nil {
		l.Logger.Errorf("MainTaskStart: task not found, id=%s", req.Id)
		return &types.BaseResp{Code: 400, Msg: "任务不存在"}, nil
	}

	l.Logger.Infof("MainTaskStart: found task, id=%s, taskId=%s, currentStatus='%s'", req.Id, task.TaskId, task.Status)

	// 检查状态：只有CREATED状态或空状态可以启动
	if task.Status != model.TaskStatusCreated && task.Status != "" {
		return &types.BaseResp{Code: 400, Msg: "只有待启动状态的任务可以启动，当前状态: " + task.Status}, nil
	}

	// 解析任务配置获取目标
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		return &types.BaseResp{Code: 500, Msg: "解析任务配置失败"}, nil
	}

	// Debug: 打印配置中的orgId
	if orgId, ok := taskConfig["orgId"].(string); ok && orgId != "" {
		l.Logger.Infof("MainTaskStart: orgId in config = %s", orgId)
	} else {
		l.Logger.Infof("MainTaskStart: orgId not found in config")
	}

	// 使用 TaskBuilder 统一处理任务启动逻辑
	builder := common.NewTaskBuilder(l.ctx, l.svcCtx)
	batchCount, err := builder.BuildAndPushSubTasks(task, taskConfig)
	if err != nil {
		l.Logger.Errorf("MainTaskStart: failed to start task %s: %v", task.TaskId, err)
		return &types.BaseResp{Code: 500, Msg: fmt.Sprintf("启动任务失败: %v", err)}, nil
	}

	// 构建响应消息
	message := fmt.Sprintf("任务已启动，共拆分为 %d 个分片", batchCount)
	return &types.BaseResp{Code: 0, Msg: message}, nil
}

// MainTaskPauseLogic 暂停任务
type MainTaskPauseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskPauseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskPauseLogic {
	return &MainTaskPauseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskPauseLogic) MainTaskPause(req *types.MainTaskControlReq) (resp *types.BaseResp, err error) {
	l.Logger.Infof("MainTaskPause: received request, id=%s", req.Id)

	taskModel := l.svcCtx.GetMainTaskModel()
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskPause: find task failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "查询任务失败"}, nil
	}
	if task == nil {
		l.Logger.Errorf("MainTaskPause: task not found, id=%s", req.Id)
		return &types.BaseResp{Code: 400, Msg: "任务不存在"}, nil
	}

	l.Logger.Infof("MainTaskPause: found task, id=%s, taskId=%s, status='%s', progress=%d, subTaskCount=%d, subTaskDone=%d",
		req.Id, task.TaskId, task.Status, task.Progress, task.SubTaskCount, task.SubTaskDone)

	// Pause requires an already-claimed durable dispatch. A CREATED task has no
	// immutable executor manifest or generation to resume, so it must be started
	// rather than transitioned into an unrecoverable PAUSED state.
	if task.Status == model.TaskStatusCreated || task.Status == "" || task.DispatchGeneration == "" {
		return &types.BaseResp{Code: 400, Msg: "任务尚未启动，无法暂停"}, nil
	}
	canPause := task.Status == model.TaskStatusPending || task.Status == model.TaskStatusStarted
	if !canPause {
		if task.Status == model.TaskStatusPaused {
			return &types.BaseResp{Code: 400, Msg: "任务已经处于暂停状态"}, nil
		}
		return &types.BaseResp{Code: 400, Msg: "当前状态不可暂停: " + task.Status}, nil
	}

	intent := model.TaskControlIntent{
		IntentID:           uuid.NewString(),
		Action:             model.TaskControlActionPause,
		DispatchGeneration: task.DispatchGeneration,
		CreatedTime:        time.Now().UTC().Truncate(time.Millisecond),
	}
	committed, err := taskModel.CommitControlIntent(l.ctx, req.Id, task.DispatchGeneration,
		[]string{task.Status}, model.TaskStatusPaused, intent, nil)
	if err != nil {
		if errors.Is(err, model.ErrTaskDispatchConflict) {
			return &types.BaseResp{Code: 409, Msg: "任务状态或执行批次已发生变化"}, nil
		}
		return &types.BaseResp{Code: 500, Msg: "更新任务状态失败"}, nil
	}

	// Redis is a latency optimization after the durable CAS. A bounded,
	// request-independent pass publishes every exact canonical child and the
	// periodic reconciler repairs any failure.
	ReconcileControlIntentSoon(l.svcCtx, committed.Id.Hex())
	l.Logger.Infof("Task pause intent persisted: taskId=%s generation=%s intentId=%s",
		committed.TaskId, committed.DispatchGeneration, committed.ControlIntent.IntentID)
	return &types.BaseResp{Code: 0, Msg: "任务已暂停"}, nil
}

// MainTaskResumeLogic 继续任务
type MainTaskResumeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskResumeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskResumeLogic {
	return &MainTaskResumeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskResumeLogic) MainTaskResume(req *types.MainTaskControlReq) (resp *types.BaseResp, err error) {
	l.Logger.Infof("MainTaskResume: id=%s", req.Id)

	taskModel := l.svcCtx.GetMainTaskModel()
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskResume: find task failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "查询任务失败"}, nil
	}
	if task == nil {
		return &types.BaseResp{Code: 400, Msg: "任务不存在"}, nil
	}
	if task.Status != model.TaskStatusPaused {
		return &types.BaseResp{Code: 400, Msg: "只有已暂停的任务可以继续"}, nil
	}
	previousGeneration := task.DispatchGeneration
	var pauseIntent *model.TaskControlIntent
	if task.ControlIntent != nil {
		intentCopy := *task.ControlIntent
		if !intentCopy.IsValid() || intentCopy.Action != model.TaskControlActionPause ||
			intentCopy.DispatchGeneration != previousGeneration {
			return &types.BaseResp{Code: 409, Msg: "暂停控制意图已发生变化"}, nil
		}
		pauseIntent = &intentCopy
	}

	// Preparation is read-only. Any model/cache/config failure leaves PAUSED and
	// all control keys intact, so the same request can be retried safely.
	schedTasks, subTaskIDs, err := prepareResumeTaskPlan(l.ctx, l.svcCtx, task)
	if err != nil {
		l.Logger.Errorf("MainTaskResume: prepare resume plan failed: %v", err)
		return &types.BaseResp{Code: 409, Msg: "无法安全恢复任务: " + err.Error()}, nil
	}
	quiescent, err := l.svcCtx.Scheduler.IsTaskBatchQuiescent(l.ctx, subTaskIDs)
	if err != nil {
		l.Logger.Errorf("MainTaskResume: quiescence check failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "检查暂停提交状态失败"}, nil
	}
	if !quiescent {
		return &types.BaseResp{Code: 409, Msg: "任务仍在提交暂停快照，请稍后重试"}, nil
	}
	pauseControls := make([]scheduler.TaskControlEnvelope, 0, len(subTaskIDs))
	if pauseIntent != nil {
		for _, childID := range subTaskIDs {
			pauseControls = append(pauseControls, scheduler.TaskControlEnvelope{
				IntentID:           pauseIntent.IntentID,
				MainTaskID:         task.Id.Hex(),
				TaskID:             childID,
				Action:             scheduler.TaskControlActionPause,
				DispatchGeneration: previousGeneration,
				Timestamp:          pauseIntent.CreatedTime,
			})
		}
	}
	dispatchGeneration := uuid.NewString()
	dispatchTime := model.NextDispatchCreateTime(task.DispatchCreateTime)
	stableCreateTime := dispatchTime.Local().Format("2006-01-02 15:04:05")
	definitions := make([]model.ExecutorTask, 0, len(schedTasks))
	for _, schedTask := range schedTasks {
		schedTask.CreateTime = stableCreateTime
		schedTask.DispatchGeneration = dispatchGeneration
		definitions = append(definitions, model.ExecutorTask{
			TaskId:             schedTask.TaskId,
			MainTaskId:         task.Id.Hex(),
			TaskName:           schedTask.TaskName,
			Config:             schedTask.Config,
			Priority:           schedTask.Priority,
			DispatchGeneration: dispatchGeneration,
		})
	}
	if err := model.NewTaskDispatchManifestModel(l.svcCtx.MongoDB).Persist(
		l.ctx, task.Id.Hex(), dispatchGeneration, model.DispatchIntentResume, dispatchTime, definitions,
	); err != nil {
		l.Logger.Errorf("MainTaskResume: persist immutable resume manifest failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "持久化恢复批次失败"}, nil
	}

	// The new generation and retirement of the exact old PAUSE intent share one
	// Mongo CAS. An absent intent is allowed only when reconciliation already
	// acknowledged it; a racing STOP changes the status/intent and wins.
	claimed, err := taskModel.ClaimResumeDispatch(l.ctx, task.Id.Hex(), previousGeneration,
		dispatchGeneration, pauseIntent, bson.M{"dispatch_create_time": dispatchTime})
	if err != nil {
		l.Logger.Errorf("MainTaskResume: durable dispatch claim failed: %v", err)
		return &types.BaseResp{Code: 409, Msg: "任务状态已发生变化"}, nil
	}
	task.DispatchGeneration = claimed.DispatchGeneration
	task.DispatchIntent = claimed.DispatchIntent
	task.DispatchCreateTime = claimed.DispatchCreateTime
	task.Status = claimed.Status

	if err := l.svcCtx.GetExecutorTaskModel().ActivateDispatchDefinitions(
		l.ctx, task.Id.Hex(), dispatchGeneration, dispatchTime, definitions,
	); err != nil {
		l.Logger.Errorf("MainTaskResume: activate exact resume dispatch failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "恢复批次已认领，后台将安全重试"}, nil
	}

	markerKey := fmt.Sprintf("cscan:task:resume:%s:%s", task.Id.Hex(), dispatchGeneration)
	if err := l.svcCtx.Scheduler.ResumeTaskBatch(l.ctx, schedTasks, pauseControls, markerKey); err != nil {
		l.Logger.Errorf("MainTaskResume: atomic queue commit failed: %v", err)
		if errors.Is(err, scheduler.ErrTaskAlreadyProcessing) || errors.Is(err, scheduler.ErrTaskOperationBusy) {
			return &types.BaseResp{Code: 409, Msg: "任务仍有活动执行或提交操作；后台将安全重试"}, nil
		}
		return &types.BaseResp{Code: 500, Msg: "任务发布结果不确定，后台将安全重试"}, nil
	}

	l.Logger.Infof("MainTaskResume: task resumed successfully, taskId=%s, generation=%s, subTasks=%d", task.TaskId, dispatchGeneration, len(schedTasks))
	return &types.BaseResp{Code: 0, Msg: "任务已继续"}, nil
}

// MainTaskStopLogic 停止任务
type MainTaskStopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskStopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskStopLogic {
	return &MainTaskStopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskStopLogic) MainTaskStop(req *types.MainTaskControlReq) (resp *types.BaseResp, err error) {
	taskModel := l.svcCtx.GetMainTaskModel()
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskStop: find task failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "查询任务失败"}, nil
	}
	if task == nil {
		return &types.BaseResp{Code: 400, Msg: "任务不存在"}, nil
	}

	// 检查状态：STARTED, PAUSED, PENDING, CREATED 或空状态可以停止
	canStop := task.Status == model.TaskStatusStarted ||
		task.Status == model.TaskStatusPaused ||
		task.Status == model.TaskStatusPending ||
		task.Status == model.TaskStatusCreated ||
		task.Status == ""
	if !canStop {
		return &types.BaseResp{Code: 400, Msg: "当前状态不可停止"}, nil
	}

	now := time.Now().UTC()
	fields := bson.M{
		"result":   "任务已手动停止",
		"end_time": now,
	}
	// A never-dispatched task has no workers or generation to notify, so status
	// alone is the durable result and no control intent is created.
	if task.Status == model.TaskStatusCreated || task.Status == "" || task.DispatchGeneration == "" {
		transitioned, err := taskModel.TransitionDispatchStatus(l.ctx, req.Id, task.DispatchGeneration,
			[]string{task.Status}, model.TaskStatusStopped, fields)
		if err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新任务状态失败"}, nil
		}
		if !transitioned {
			return &types.BaseResp{Code: 409, Msg: "任务状态或执行批次已发生变化"}, nil
		}
		return &types.BaseResp{Code: 0, Msg: "任务已停止"}, nil
	}

	intent := model.TaskControlIntent{
		IntentID:           uuid.NewString(),
		Action:             model.TaskControlActionStop,
		DispatchGeneration: task.DispatchGeneration,
		CreatedTime:        now.Truncate(time.Millisecond),
	}
	committed, err := taskModel.CommitControlIntent(l.ctx, req.Id, task.DispatchGeneration,
		[]string{model.TaskStatusPending, model.TaskStatusStarted, model.TaskStatusPaused},
		model.TaskStatusStopped, intent, fields)
	if err != nil {
		if errors.Is(err, model.ErrTaskDispatchConflict) {
			return &types.BaseResp{Code: 409, Msg: "任务状态或执行批次已发生变化"}, nil
		}
		return &types.BaseResp{Code: 500, Msg: "更新任务状态失败"}, nil
	}

	ReconcileControlIntentSoon(l.svcCtx, committed.Id.Hex())
	l.Logger.Infof("Task stop intent persisted: taskId=%s generation=%s intentId=%s",
		committed.TaskId, committed.DispatchGeneration, committed.ControlIntent.IntentID)
	return &types.BaseResp{Code: 0, Msg: "任务已停止"}, nil
}

// TaskStatLogic 任务统计逻辑
type TaskStatLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskStatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TaskStatLogic {
	return &TaskStatLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TaskStatLogic) TaskStat() (resp *types.TaskStatResp, err error) {
	now := time.Now()
	trendDays := make([]string, 7)
	trendCompleted := make([]int, 7)
	trendFailed := make([]int, 7)

	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		trendDays[6-i] = day.Format("01-02")
	}

	taskModel := l.svcCtx.GetMainTaskModel()
	coll := taskModel.Collection()

	var totalCount int64

	// 单次聚合：统计各状态总数 + 近7天每日完成/失败数
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"status": bson.M{"$in": []string{
					model.TaskStatusSuccess,
					model.TaskStatusStarted,
					model.TaskStatusFailure,
					model.TaskStatusPending,
					model.TaskStatusCreated,
				}},
			},
		},
		{
			"$group": bson.M{
				"_id":    "$status",
				"count":  bson.M{"$sum": 1},
				"latest": bson.M{"$max": "$update_time"},
			},
		},
	}

	resultCursor, err := coll.Aggregate(l.ctx, pipeline)
	if err == nil {
		defer resultCursor.Close(l.ctx)
		for resultCursor.Next(l.ctx) {
			var r struct {
				ID     string    `bson:"_id"`
				Count  int64     `bson:"count"`
				Latest time.Time `bson:"latest"`
			}
			if err := resultCursor.Decode(&r); err != nil {
				continue
			}
			totalCount += r.Count
			switch r.ID {
			case model.TaskStatusSuccess:
				resp.Completed = int(r.Count)
			case model.TaskStatusStarted:
				resp.Running = int(r.Count)
			case model.TaskStatusFailure:
				resp.Failed = int(r.Count)
			case model.TaskStatusPending, model.TaskStatusCreated:
				resp.Pending += int(r.Count)
			}
		}
	}

	// 近7天趋势：单次聚合按日期+状态分组
	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		dayEnd := dayStart.AddDate(0, 0, 1)
		idx := 6 - i
		trendDays[idx] = day.Format("01-02")

		dayPipeline := []bson.M{
			{"$match": bson.M{
				"status":      bson.M{"$in": []string{model.TaskStatusSuccess, model.TaskStatusFailure}},
				"update_time": bson.M{"$gte": dayStart, "$lt": dayEnd},
			}},
			{"$group": bson.M{
				"_id":   "$status",
				"count": bson.M{"$sum": 1},
			}},
		}
		dayCursor, err := coll.Aggregate(l.ctx, dayPipeline)
		if err == nil {
			for dayCursor.Next(l.ctx) {
				var dr struct {
					ID    string `bson:"_id"`
					Count int64  `bson:"count"`
				}
				if err := dayCursor.Decode(&dr); err != nil {
					continue
				}
				if dr.ID == model.TaskStatusSuccess {
					trendCompleted[idx] = int(dr.Count)
				} else if dr.ID == model.TaskStatusFailure {
					trendFailed[idx] = int(dr.Count)
				}
			}
			dayCursor.Close(l.ctx)
		}
	}

	resp = &types.TaskStatResp{
		Code:           0,
		Msg:            "success",
		Total:          int(totalCount),
		TrendDays:      trendDays,
		TrendCompleted: trendCompleted,
		TrendFailed:    trendFailed,
	}
	return resp, nil
}

// MainTaskUpdateLogic 更新任务逻辑
type MainTaskUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskUpdateLogic {
	return &MainTaskUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskUpdateLogic) MainTaskUpdate(req *types.MainTaskUpdateReq) (resp *types.BaseResp, err error) {
	taskModel := l.svcCtx.GetMainTaskModel()

	// 如果更新了目标，校验目标格式
	if req.Target != "" {
		if validationErrors := common.ValidateTargets(req.Target); len(validationErrors) > 0 {
			return &types.BaseResp{Code: 400, Msg: common.FormatValidationErrors(validationErrors)}, nil
		}
	}

	// 获取任务
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskUpdate: find task failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "查询任务失败"}, nil
	}
	if task == nil {
		l.Logger.Errorf("MainTaskUpdate: task not found, id=%s", req.Id)
		return &types.BaseResp{Code: 40001, Msg: "任务不存在"}, nil
	}

	// 检查状态：只有CREATED状态可以编辑
	if task.Status != model.TaskStatusCreated {
		l.Logger.Infof("MainTaskUpdate: task status not allowed, id=%s, status=%s", req.Id, task.Status)
		return &types.BaseResp{Code: 40002, Msg: "任务状态不允许编辑，只有待启动状态的任务可以编辑"}, nil
	}

	// 构建更新字段
	update := bson.M{}

	if req.Name != "" {
		update["name"] = req.Name
	}

	if req.Target != "" {
		update["target"] = req.Target
	}

	if req.Tags != nil {
		update["tags"] = req.Tags
	}

	if req.ProfileId != "" {
		// 验证配置是否存在
		profile, err := l.svcCtx.ProfileModel.FindById(l.ctx, req.ProfileId)
		if err != nil {
			l.Logger.Errorf("MainTaskUpdate: find profile failed, profileId=%s, error=%v", req.ProfileId, err)
			return &types.BaseResp{Code: 500, Msg: "查询任务配置失败"}, nil
		}
		if profile == nil {
			return &types.BaseResp{Code: 400, Msg: "任务配置不存在"}, nil
		}
		update["profile_id"] = req.ProfileId
		update["profile_name"] = profile.Name

		// 更新任务配置
		taskConfig := map[string]interface{}{
			"target": task.Target,
		}
		if req.Target != "" {
			taskConfig["target"] = req.Target
		}
		// 合并 profile 配置
		if profile.Config != "" {
			var profileConfig map[string]interface{}
			if err := json.Unmarshal([]byte(profile.Config), &profileConfig); err == nil {
				for k, v := range profileConfig {
					taskConfig[k] = v
				}
			}
		}
		// 注入自定义POC和标签映射
		taskConfig = common.InjectPocConfig(l.ctx, l.svcCtx, taskConfig, l.Logger)
		configBytes, _ := json.Marshal(taskConfig)
		update["config"] = string(configBytes)
	} else if req.Target != "" {
		// 只更新了target，需要重新生成config
		taskConfig := map[string]interface{}{
			"target": req.Target,
		}
		// 获取当前profile配置
		if task.ProfileId != "" {
			profile, err := l.svcCtx.ProfileModel.FindById(l.ctx, task.ProfileId)
			if err == nil && profile != nil && profile.Config != "" {
				var profileConfig map[string]interface{}
				if err := json.Unmarshal([]byte(profile.Config), &profileConfig); err == nil {
					for k, v := range profileConfig {
						taskConfig[k] = v
					}
				}
			}
		}
		// 注入自定义POC和标签映射
		taskConfig = common.InjectPocConfig(l.ctx, l.svcCtx, taskConfig, l.Logger)
		configBytes, _ := json.Marshal(taskConfig)
		update["config"] = string(configBytes)
	}

	if len(update) == 0 {
		return &types.BaseResp{Code: 400, Msg: "没有需要更新的字段"}, nil
	}

	// 再次检查状态（防止并发修改）
	task, err = taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskUpdate: re-find task failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "查询任务失败"}, nil
	}
	if task == nil {
		return &types.BaseResp{Code: 40001, Msg: "任务不存在"}, nil
	}
	if task.Status != model.TaskStatusCreated {
		return &types.BaseResp{Code: 40002, Msg: "任务状态已变更，无法编辑"}, nil
	}

	// 执行更新
	if err := taskModel.Update(l.ctx, req.Id, update); err != nil {
		l.Logger.Errorf("MainTaskUpdate: update failed, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 500, Msg: "更新任务失败"}, nil
	}

	l.Logger.Infof("MainTaskUpdate: task updated, id=%s", req.Id)
	return &types.BaseResp{Code: 0, Msg: "任务更新成功"}, nil
}

// GetTaskLogsLogic 获取任务日志逻辑
type GetTaskLogsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTaskLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskLogsLogic {
	return &GetTaskLogsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTaskLogsLogic) GetTaskLogs(req *types.GetTaskLogsReq) (resp *types.GetTaskLogsResp, err error) {
	if req.TaskId == "" {
		return &types.GetTaskLogsResp{Code: 400, Msg: "任务ID不能为空", List: []types.TaskLogEntry{}}, nil
	}

	// 校验任务存在且当前用户有权限访问
	taskModel := l.svcCtx.GetMainTaskModel()
	task, taskErr := taskModel.FindByTaskId(l.ctx, req.TaskId)
	if taskErr != nil {
		l.Logger.Errorf("GetTaskLogs: query task failed, taskId=%s, error=%v", req.TaskId, taskErr)
		return &types.GetTaskLogsResp{Code: 500, Msg: "查询任务失败", List: []types.TaskLogEntry{}}, nil
	}
	if task == nil {
		return &types.GetTaskLogsResp{Code: 404, Msg: "任务不存在", List: []types.TaskLogEntry{}}, nil
	}
	// 权限校验：管理员可访问所有任务；非管理员仅可访问自己创建的任务
	// 旧数据无 created_by 字段，允许访问以保持兼容
	currentUserId := middleware.GetUserId(l.ctx)
	currentRole := middleware.GetRole(l.ctx)
	if !l.svcCtx.IsAdminRole(l.ctx, currentRole) && task.CreatedBy != "" && task.CreatedBy != currentUserId {
		return &types.GetTaskLogsResp{Code: 403, Msg: "无权访问该任务日志", List: []types.TaskLogEntry{}}, nil
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 10000 {
		limit = 10000
	}

	if l.svcCtx.WorkerLogReader == nil {
		return &types.GetTaskLogsResp{Code: 0, Msg: "日志读取器未初始化", List: []types.TaskLogEntry{}}, nil
	}

	var entries []svc.WorkerLogEntry
	var nextCursor int64
	var readErr error

	// 有 cursor 时使用增量读取，否则全量读取
	if req.Cursor > 0 || req.AfterTime != "" {
		entries, nextCursor, readErr = l.svcCtx.WorkerLogReader.ReadByTaskIdAfter(req.TaskId, req.Cursor, req.AfterTime, limit)
	} else {
		entries, readErr = l.svcCtx.WorkerLogReader.ReadByTaskIdAll(req.TaskId, limit)
	}
	if readErr != nil {
		l.Logger.Errorf("GetTaskLogs: read logs failed, taskId=%s, error=%v", req.TaskId, readErr)
		return &types.GetTaskLogsResp{Code: 500, Msg: "读取日志失败", List: []types.TaskLogEntry{}}, nil
	}

	result := make([]types.TaskLogEntry, 0, len(entries))
	searchLower := strings.ToLower(req.Search)

	for _, e := range entries {
		// 模糊搜索过滤
		if req.Search != "" {
			if !strings.Contains(strings.ToLower(e.Msg), searchLower) &&
				!strings.Contains(strings.ToLower(e.Level), searchLower) &&
				!strings.Contains(strings.ToLower(e.Worker), searchLower) {
				continue
			}
		}
		// DEBUG 级别默认过滤（可通过 IncludeDebug 开启，用于与容器日志对齐排查）
		if !req.IncludeDebug && strings.EqualFold(e.Level, "DEBUG") {
			continue
		}
		result = append(result, types.TaskLogEntry{
			Timestamp:  e.Ts,
			Level:      e.Level,
			WorkerName: e.Worker,
			TaskId:     e.TaskId,
			Message:    e.Msg,
		})
		if len(result) >= limit {
			break
		}
	}

	l.Logger.Infof("GetTaskLogs: returned %d logs for taskId=%s, nextCursor=%d", len(result), req.TaskId, nextCursor)
	return &types.GetTaskLogsResp{Code: 0, Msg: "success", List: result, NextCursor: nextCursor}, nil
}

// getMainTaskIdFromLog 从日志中的taskId提取主任务ID
func getMainTaskIdFromLog(taskId string) string {
	// 查找最后一个 "-" 后面是否是数字
	lastDash := strings.LastIndex(taskId, "-")
	if lastDash > 0 && lastDash < len(taskId)-1 {
		suffix := taskId[lastDash+1:]
		// 检查后缀是否全是数字
		isNumber := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				isNumber = false
				break
			}
		}
		if isNumber {
			return taskId[:lastDash]
		}
	}
	return taskId
}
