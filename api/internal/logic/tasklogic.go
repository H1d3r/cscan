package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/logic/common"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"
	"cscan/scheduler"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type MainTaskListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMainTaskListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MainTaskListLogic {
	return &MainTaskListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MainTaskListLogic) MainTaskList(req *types.MainTaskListReq, workspaceId string) (resp *types.MainTaskListResp, err error) {
	// 优先使用请求体中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	l.Logger.Infof("MainTaskList: reqWorkspaceId=%s, headerWorkspaceId=%s, using=%s, page=%d, pageSize=%d", 
		req.WorkspaceId, workspaceId, wsId, req.Page, req.PageSize)
	
	// 构建查询条件
	filter := bson.M{}
	if req.Name != "" {
		filter["name"] = bson.M{"$regex": req.Name, "$options": "i"}
	}
	if req.Status != "" {
		filter["status"] = req.Status
	}

	var total int64
	
	// 任务及其所属工作空间
	type taskWithWorkspace struct {
		task        model.MainTask
		workspaceId string
	}
	var tasksWithWs []taskWithWorkspace

	// 获取需要查询的工作空间列表
	wsIds := common.GetWorkspaceIds(l.ctx, l.svcCtx, wsId)
	l.Logger.Infof("MainTaskList: querying workspaces: %v", wsIds)

	// 如果查询多个工作空间
	if len(wsIds) > 1 || wsId == "" || wsId == "all" {
		for _, ws := range wsIds {
			taskModel := l.svcCtx.GetMainTaskModel(ws)
			wsTotal, _ := taskModel.Count(l.ctx, filter)
			total += wsTotal
			l.Logger.Infof("MainTaskList: workspace=%s, taskCount=%d", ws, wsTotal)
			
			wsTasks, _ := taskModel.Find(l.ctx, filter, 0, 0)
			for _, t := range wsTasks {
				tasksWithWs = append(tasksWithWs, taskWithWorkspace{task: t, workspaceId: ws})
			}
		}
		
		// 按创建时间排序
		sort.Slice(tasksWithWs, func(i, j int) bool {
			return tasksWithWs[i].task.CreateTime.After(tasksWithWs[j].task.CreateTime)
		})
		
		// 分页
		start := (req.Page - 1) * req.PageSize
		end := start + req.PageSize
		if start > len(tasksWithWs) {
			start = len(tasksWithWs)
		}
		if end > len(tasksWithWs) {
			end = len(tasksWithWs)
		}
		tasksWithWs = tasksWithWs[start:end]
	} else {
		taskModel := l.svcCtx.GetMainTaskModel(wsId)

		// 查询总数
		total, err = taskModel.Count(l.ctx, filter)
		if err != nil {
			return &types.MainTaskListResp{Code: 500, Msg: "查询失败"}, nil
		}

		// 查询列表
		tasks, err := taskModel.Find(l.ctx, filter, req.Page, req.PageSize)
		if err != nil {
			return &types.MainTaskListResp{Code: 500, Msg: "查询失败"}, nil
		}
		for _, t := range tasks {
			tasksWithWs = append(tasksWithWs, taskWithWorkspace{task: t, workspaceId: wsId})
		}
	}

	// 转换响应
	list := make([]types.MainTask, 0, len(tasksWithWs))
	for _, tw := range tasksWithWs {
		t := tw.task
		progress := t.Progress
		currentPhase := t.CurrentPhase
		subTaskDone := t.SubTaskDone
		status := t.Status
		
		// DEBUG: 打印从数据库读取的原始状态
		fmt.Printf("[TaskList] task=%s, dbStatus='%s', progress=%d, subTaskDone=%d\n", t.TaskId, t.Status, t.Progress, t.SubTaskDone)
		
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
			// 获取主任务的当前阶段
			mainKey := fmt.Sprintf("cscan:task:progress:%s", t.TaskId)
			if data, err := l.svcCtx.RedisClient.Get(l.ctx, mainKey).Result(); err == nil && data != "" {
				var progressData struct {
					CurrentPhase string `json:"currentPhase"`
				}
				if json.Unmarshal([]byte(data), &progressData) == nil {
					if progressData.CurrentPhase != "" {
						currentPhase = progressData.CurrentPhase
					}
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
			WorkspaceId:  tw.workspaceId,
		})
	}

	return &types.MainTaskListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
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

func (l *MainTaskCreateLogic) MainTaskCreate(req *types.MainTaskCreateReq, workspaceId string) (resp *types.BaseRespWithId, err error) {
	// 优先使用请求体中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	if wsId == "" {
		return &types.BaseRespWithId{Code: 400, Msg: "workspaceId不能为空"}, nil
	}
	
	l.Logger.Infof("MainTaskCreate: name=%s, reqWorkspaceId=%s, headerWorkspaceId=%s, using=%s", 
		req.Name, req.WorkspaceId, workspaceId, wsId)

	// 校验目标格式
	if req.Target == "" {
		return &types.BaseRespWithId{Code: 400, Msg: "扫描目标不能为空"}, nil
	}
	if validationErrors := common.ValidateTargets(req.Target); len(validationErrors) > 0 {
		return &types.BaseRespWithId{Code: 400, Msg: common.FormatValidationErrors(validationErrors)}, nil
	}

	taskModel := l.svcCtx.GetMainTaskModel(wsId)

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

	// 优先使用直接传递的 config，否则从 profile 获取
	profileName := "自定义配置"
	if req.Config != "" {
		// 直接使用传递的配置
		var directConfig map[string]interface{}
		if err := json.Unmarshal([]byte(req.Config), &directConfig); err == nil {
			for k, v := range directConfig {
				taskConfig[k] = v
			}
		}
	} else if req.ProfileId != "" {
		// 从 profile 获取配置（兼容旧版）
		profile, err := l.svcCtx.ProfileModel.FindById(l.ctx, req.ProfileId)
		if err != nil {
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
	configBytes, _ := json.Marshal(taskConfig)
	
	configStr := string(configBytes)
	logLen := len(configStr)
	if logLen > 500 {
		logLen = 500
	}
	l.Logger.Infof("MainTaskCreate: config length=%d, config=%s", len(configStr), configStr[:logLen])

	// 创建主任务（状态为CREATED，不立即执行）
	taskId := uuid.New().String()
	task := &model.MainTask{
		TaskId:      taskId,
		Name:        req.Name,
		Target:      req.Target,
		ProfileId:   req.ProfileId,
		ProfileName: profileName,
		OrgId:       req.OrgId,
		IsCron:      req.IsCron,
		CronRule:    req.CronRule,
		Config:      string(configBytes),
		Status:      model.TaskStatusCreated, // 设置初始状态
	}

	if err := taskModel.Insert(l.ctx, task); err != nil {
		l.Logger.Errorf("MainTaskCreate: insert failed, taskId=%s, error=%v", taskId, err)
		return &types.BaseRespWithId{Code: 500, Msg: "创建任务失败: " + err.Error()}, nil
	}

	l.Logger.Infof("Task created (not started): taskId=%s, workspaceId=%s", taskId, wsId)

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

func (l *MainTaskDeleteLogic) MainTaskDelete(req *types.MainTaskDeleteReq, workspaceId string) (resp *types.BaseResp, err error) {
	// 优先使用请求体中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	if wsId == "" || wsId == "all" {
		return &types.BaseResp{Code: 400, Msg: "删除任务需要指定工作空间"}, nil
	}
	
	taskModel := l.svcCtx.GetMainTaskModel(wsId)
	
	// 先获取任务信息，发送停止信号
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err == nil && task != nil {
		// 发送停止信号到Redis（Set用于HTTP轮询，Publish用于WebSocket推送）
		ctrlKey := "cscan:task:ctrl:" + task.TaskId
		l.svcCtx.RedisClient.Set(l.ctx, ctrlKey, "STOP", 24*time.Hour)
		l.svcCtx.RedisClient.Publish(l.ctx, ctrlKey, "STOP")
		l.Logger.Infof("Sent stop signal before delete: taskId=%s", task.TaskId)
		
		// 清理任务相关的Redis数据
		taskInfoKey := "cscan:task:info:" + task.TaskId
		l.svcCtx.RedisClient.Del(l.ctx, taskInfoKey)
	}
	
	err = taskModel.Delete(l.ctx, req.Id)
	if err != nil {
		return &types.BaseResp{Code: 500, Msg: "删除失败"}, nil
	}
	return &types.BaseResp{Code: 0, Msg: "删除成功"}, nil
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

func (l *MainTaskBatchDeleteLogic) MainTaskBatchDelete(req *types.MainTaskBatchDeleteReq, workspaceId string) (resp *types.BaseResp, err error) {
	if len(req.Ids) == 0 {
		return &types.BaseResp{Code: 400, Msg: "请选择要删除的任务"}, nil
	}

	// 优先使用请求体中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	if wsId == "" || wsId == "all" {
		return &types.BaseResp{Code: 400, Msg: "删除任务需要指定工作空间"}, nil
	}

	taskModel := l.svcCtx.GetMainTaskModel(wsId)
	
	// 先获取所有任务信息，发送停止信号
	for _, id := range req.Ids {
		task, err := taskModel.FindById(l.ctx, id)
		if err == nil && task != nil {
			// 发送停止信号到Redis（Set用于HTTP轮询，Publish用于WebSocket推送）
			ctrlKey := "cscan:task:ctrl:" + task.TaskId
			l.svcCtx.RedisClient.Set(l.ctx, ctrlKey, "STOP", 24*time.Hour)
			l.svcCtx.RedisClient.Publish(l.ctx, ctrlKey, "STOP")
			l.Logger.Infof("Sent stop signal before batch delete: taskId=%s", task.TaskId)
			
			// 清理任务相关的Redis数据
			taskInfoKey := "cscan:task:info:" + task.TaskId
			l.svcCtx.RedisClient.Del(l.ctx, taskInfoKey)
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

func (l *MainTaskRetryLogic) MainTaskRetry(req *types.MainTaskRetryReq, workspaceId string) (resp *types.BaseRespWithId, err error) {
	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)

	// 获取原任务信息
	oldTask, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
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
		Config:      string(configBytes),
		Status:      model.TaskStatusCreated, // 设置初始状态
	}

	if err := taskModel.Insert(l.ctx, newTask); err != nil {
		l.Logger.Errorf("MainTaskRetry: insert new task failed, error=%v", err)
		return &types.BaseRespWithId{Code: 500, Msg: "创建新任务失败: " + err.Error()}, nil
	}

	// 从配置中获取批次大小，默认50
	// batchSize = 0 表示不拆分，使用一个很大的值
	batchSize := 50
	if bs, ok := taskConfig["batchSize"].(float64); ok {
		if bs == 0 {
			batchSize = 1000000 // 不拆分，使用一个很大的值
		} else if bs > 0 {
			batchSize = int(bs)
		}
	}

	// 使用目标拆分器判断是否需要拆分
	splitter := scheduler.NewTargetSplitter(batchSize)
	batches := splitter.SplitTargets(oldTask.Target)

	// 解析任务配置，计算启用的扫描模块数量
	config, _ := scheduler.ParseTaskConfig(string(configBytes))
	enabledModules := 0
	if config != nil {
		if config.DomainScan != nil && config.DomainScan.Enable {
			enabledModules++
		}
		if config.PortScan == nil || config.PortScan.Enable { // 端口扫描默认启用
			enabledModules++
		}
		if config.PortIdentify != nil && config.PortIdentify.Enable {
			enabledModules++
		}
		if config.Fingerprint != nil && config.Fingerprint.Enable {
			enabledModules++
		}
		if config.DirScan != nil && config.DirScan.Enable {
			enabledModules++
		}
		if config.PocScan != nil && config.PocScan.Enable {
			enabledModules++
		}
	}
	if enabledModules == 0 {
		enabledModules = 1 // 至少有一个模块
	}

	// 子任务总数 = 目标批次数 × 启用的扫描模块数
	subTaskCount := len(batches) * enabledModules

	l.Logger.Infof("Retry task %s target split into %d batches (batchSize=%d), enabledModules=%d, subTaskCount=%d", 
		newTaskId, len(batches), batchSize, enabledModules, subTaskCount)

	// 更新新任务状态为STARTED，记录子任务数量
	now := time.Now()
	taskModel.Update(l.ctx, newTask.Id.Hex(), bson.M{
		"status":         model.TaskStatusStarted,
		"sub_task_count": subTaskCount,
		"sub_task_done":  0,
		"start_time":     now,
	})

	// 保存主任务信息到 Redis
	taskInfoKey := "cscan:task:info:" + newTaskId
	taskInfoData, _ := json.Marshal(map[string]interface{}{
		"workspaceId":    workspaceId,
		"mainTaskId":     newTask.Id.Hex(),
		"subTaskCount":   subTaskCount,
		"batchCount":     len(batches),
		"enabledModules": enabledModules,
	})
	l.svcCtx.RedisClient.Set(l.ctx, taskInfoKey, taskInfoData, 24*time.Hour)

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
		subConfigBytes, _ := json.Marshal(subConfig)

		// 生成子任务ID
		subTaskId := newTaskId
		if len(batches) > 1 {
			subTaskId = newTaskId + "-" + strconv.Itoa(i)
		}

		schedTask := &scheduler.TaskInfo{
			TaskId:      subTaskId,
			MainTaskId:  newTask.Id.Hex(),
			WorkspaceId: workspaceId,
			TaskName:    newTask.Name,
			Config:      string(subConfigBytes),
			Priority:    1,
			Workers:     workers,
		}

		l.Logger.Infof("Pushing retry sub-task %d/%d: taskId=%s, targets=%d", i+1, len(batches), subTaskId, len(strings.Split(batch, "\n")))

		if err := l.svcCtx.Scheduler.PushTask(l.ctx, schedTask); err != nil {
			l.Logger.Errorf("push retry task to queue failed: %v", err)
			continue
		}

		// 保存子任务信息到 Redis（多批次时）
		if len(batches) > 1 {
			subTaskInfoKey := "cscan:task:info:" + subTaskId
			subTaskInfoData, _ := json.Marshal(map[string]interface{}{
				"workspaceId":  workspaceId,
				"mainTaskId":   newTask.Id.Hex(),
				"subTaskCount": subTaskCount,
			})
			l.svcCtx.RedisClient.Set(l.ctx, subTaskInfoKey, subTaskInfoData, 24*time.Hour)
		}
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

func (l *MainTaskStartLogic) MainTaskStart(req *types.MainTaskControlReq, workspaceId string) (resp *types.BaseResp, err error) {
	fmt.Printf("[MainTaskStart] ========== START ==========\n")
	fmt.Printf("[MainTaskStart] id=%s, reqWorkspaceId='%s', headerWorkspaceId='%s'\n", 
		req.Id, req.WorkspaceId, workspaceId)
	l.Logger.Infof("MainTaskStart: received request, id=%s, reqWorkspaceId='%s', headerWorkspaceId='%s'", 
		req.Id, req.WorkspaceId, workspaceId)
	
	// 优先使用请求中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	if wsId == "" {
		l.Logger.Errorf("MainTaskStart: workspaceId is empty")
		return &types.BaseResp{Code: 400, Msg: "workspaceId不能为空"}, nil
	}
	
	fmt.Printf("[MainTaskStart] using workspaceId='%s'\n", wsId)
	l.Logger.Infof("MainTaskStart: using workspaceId='%s'", wsId)
	taskModel := l.svcCtx.GetMainTaskModel(wsId)

	// 获取任务
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskStart: task not found, id=%s, wsId=%s, error=%v", req.Id, wsId, err)
		return &types.BaseResp{Code: 400, Msg: "任务不存在"}, nil
	}

	l.Logger.Infof("MainTaskStart: found task, id=%s, taskId=%s, currentStatus='%s', workspaceId=%s", req.Id, task.TaskId, task.Status, wsId)

	// 检查状态：只有CREATED状态或空状态可以启动
	if task.Status != model.TaskStatusCreated && task.Status != "" {
		return &types.BaseResp{Code: 400, Msg: "只有待启动状态的任务可以启动，当前状态: " + task.Status}, nil
	}

	// 解析任务配置获取目标
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		return &types.BaseResp{Code: 500, Msg: "解析任务配置失败"}, nil
	}
	target, _ := taskConfig["target"].(string)
	
	// Debug: 打印配置中的orgId
	if orgId, ok := taskConfig["orgId"].(string); ok && orgId != "" {
		l.Logger.Infof("MainTaskStart: orgId in config = %s", orgId)
	} else {
		l.Logger.Infof("MainTaskStart: orgId not found in config")
	}

	// 从配置中获取批次大小，默认50
	// batchSize = 0 表示不拆分，使用一个很大的值
	batchSize := 50
	if bs, ok := taskConfig["batchSize"].(float64); ok {
		if bs == 0 {
			batchSize = 1000000 // 不拆分，使用一个很大的值
		} else if bs > 0 {
			batchSize = int(bs)
		}
	}

	// 使用目标拆分器判断是否需要拆分
	splitter := scheduler.NewTargetSplitter(batchSize)
	batches := splitter.SplitTargets(target)

	// 解析任务配置，计算启用的扫描模块数量
	config, _ := scheduler.ParseTaskConfig(task.Config)
	enabledModules := 0
	if config != nil {
		if config.DomainScan != nil && config.DomainScan.Enable {
			enabledModules++
		}
		if config.PortScan == nil || config.PortScan.Enable { // 端口扫描默认启用
			enabledModules++
		}
		if config.PortIdentify != nil && config.PortIdentify.Enable {
			enabledModules++
		}
		if config.Fingerprint != nil && config.Fingerprint.Enable {
			enabledModules++
		}
		if config.DirScan != nil && config.DirScan.Enable {
			enabledModules++
		}
		if config.PocScan != nil && config.PocScan.Enable {
			enabledModules++
		}
	}
	if enabledModules == 0 {
		enabledModules = 1 // 至少有一个模块
	}

	// 子任务总数 = 目标批次数 × 启用的扫描模块数
	subTaskCount := len(batches) * enabledModules

	l.Logger.Infof("Task %s target split into %d batches (batchSize=%d), enabledModules=%d, subTaskCount=%d", 
		task.TaskId, len(batches), batchSize, enabledModules, subTaskCount)

	// 更新主任务状态为STARTED（直接设置为执行中，因为任务即将被推送到队列）
	now := time.Now()
	update := bson.M{
		"status":         model.TaskStatusStarted,
		"sub_task_count": subTaskCount,
		"sub_task_done":  0,
		"start_time":     now,
	}
	l.Logger.Infof("MainTaskStart: updating task %s status to STARTED", req.Id)
	fmt.Printf("[MainTaskStart] updating task %s status to STARTED, update=%+v\n", req.Id, update)
	result, err := taskModel.UpdateWithResult(l.ctx, req.Id, update)
	if err != nil {
		fmt.Printf("[MainTaskStart] ERROR: failed to update task status: %v\n", err)
		l.Logger.Errorf("MainTaskStart: failed to update task status: %v", err)
		return &types.BaseResp{Code: 500, Msg: "更新任务状态失败"}, nil
	}
	fmt.Printf("[MainTaskStart] SUCCESS: task %s updated, matchedCount=%d, modifiedCount=%d\n", req.Id, result.MatchedCount, result.ModifiedCount)
	l.Logger.Infof("MainTaskStart: task %s status updated, matchedCount=%d, modifiedCount=%d", req.Id, result.MatchedCount, result.ModifiedCount)

	// 保存主任务信息到 Redis
	taskInfoKey := "cscan:task:info:" + task.TaskId
	taskInfoData, _ := json.Marshal(map[string]interface{}{
		"workspaceId":    wsId,
		"mainTaskId":     task.Id.Hex(),
		"subTaskCount":   subTaskCount,
		"batchCount":     len(batches),
		"enabledModules": enabledModules,
	})
	l.svcCtx.RedisClient.Set(l.ctx, taskInfoKey, taskInfoData, 24*time.Hour)

	// 从配置中获取指定的 Worker 列表
	var workers []string
	if w, ok := taskConfig["workers"].([]interface{}); ok {
		for _, v := range w {
			if s, ok := v.(string); ok {
				workers = append(workers, s)
			}
		}
	}

	// 批量创建子任务
	var schedTasks []*scheduler.TaskInfo
	for i, batch := range batches {
		// 复制配置并替换目标
		subConfig := make(map[string]interface{})
		for k, v := range taskConfig {
			subConfig[k] = v
		}
		subConfig["target"] = batch
		subConfig["subTaskIndex"] = i
		subConfig["subTaskTotal"] = len(batches)
		subConfigBytes, _ := json.Marshal(subConfig)

		// 生成子任务ID
		subTaskId := task.TaskId
		if len(batches) > 1 {
			subTaskId = task.TaskId + "-" + strconv.Itoa(i)
		}

		schedTask := &scheduler.TaskInfo{
			TaskId:      subTaskId,
			MainTaskId:  task.Id.Hex(),
			WorkspaceId: wsId,
			TaskName:    task.Name,
			Config:      string(subConfigBytes),
			Priority:    1,
			Workers:     workers,
		}
		schedTasks = append(schedTasks, schedTask)

		// 只有多批次时才保存子任务信息到 Redis（单批次时使用主任务信息）
		if len(batches) > 1 {
			subTaskInfoKey := "cscan:task:info:" + subTaskId
			subTaskInfoData, _ := json.Marshal(map[string]interface{}{
				"workspaceId":  wsId,
				"mainTaskId":   task.Id.Hex(),
				"parentTaskId": task.TaskId,
				"subTaskCount": subTaskCount,
			})
			l.svcCtx.RedisClient.Set(l.ctx, subTaskInfoKey, subTaskInfoData, 24*time.Hour)
		}
	}

	// 使用批量推送提高性能
	l.Logger.Infof("Pushing %d sub-tasks to queue (batch mode)", len(schedTasks))
	if err := l.svcCtx.Scheduler.PushTaskBatch(l.ctx, schedTasks); err != nil {
		l.Logger.Errorf("push sub-tasks to queue failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "任务入队失败"}, nil
	}

	return &types.BaseResp{Code: 0, Msg: "任务已启动"}, nil
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

func (l *MainTaskPauseLogic) MainTaskPause(req *types.MainTaskControlReq, workspaceId string) (resp *types.BaseResp, err error) {
	// 优先使用请求中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	l.Logger.Infof("MainTaskPause: received request, id=%s, reqWorkspaceId=%s, headerWorkspaceId=%s, using=%s", 
		req.Id, req.WorkspaceId, workspaceId, wsId)
	
	if wsId == "" {
		return &types.BaseResp{Code: 400, Msg: "workspaceId不能为空"}, nil
	}
	
	taskModel := l.svcCtx.GetMainTaskModel(wsId)

	// 获取任务
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskPause: task not found, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 400, Msg: "任务不存在"}, nil
	}

	l.Logger.Infof("MainTaskPause: found task, id=%s, taskId=%s, status='%s', progress=%d, subTaskCount=%d, subTaskDone=%d", 
		req.Id, task.TaskId, task.Status, task.Progress, task.SubTaskCount, task.SubTaskDone)

	// 只有已完成（SUCCESS）或已失败（FAILURE）的任务不能暂停
	// 其他状态（CREATED、PENDING、STARTED、PAUSED 或空）都允许暂停
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		return &types.BaseResp{Code: 400, Msg: "已完成或已失败的任务不能暂停，当前状态: " + task.Status}, nil
	}

	// 如果已经是暂停状态，提示用户
	if task.Status == model.TaskStatusPaused {
		return &types.BaseResp{Code: 400, Msg: "任务已经处于暂停状态"}, nil
	}

	// 发送暂停信号到Redis（Set用于HTTP轮询，Publish用于WebSocket推送）
	// 1. 发送给主任务
	ctrlKey := "cscan:task:ctrl:" + task.TaskId
	l.svcCtx.RedisClient.Set(l.ctx, ctrlKey, "PAUSE", 24*time.Hour)
	l.svcCtx.RedisClient.Publish(l.ctx, ctrlKey, "PAUSE")
	
	// 2. 如果有子任务，也发送给所有子任务
	if task.SubTaskCount > 1 {
		for i := 0; i < task.SubTaskCount; i++ {
			subTaskId := fmt.Sprintf("%s-%d", task.TaskId, i)
			subCtrlKey := "cscan:task:ctrl:" + subTaskId
			l.svcCtx.RedisClient.Set(l.ctx, subCtrlKey, "PAUSE", 24*time.Hour)
			l.svcCtx.RedisClient.Publish(l.ctx, subCtrlKey, "PAUSE")
		}
		l.Logger.Infof("Task pause signal sent to %d sub-tasks", task.SubTaskCount)
	}

	// 更新状态为PAUSED
	update := bson.M{"status": model.TaskStatusPaused}
	if err := taskModel.Update(l.ctx, req.Id, update); err != nil {
		return &types.BaseResp{Code: 500, Msg: "更新任务状态失败"}, nil
	}

	l.Logger.Infof("Task paused: taskId=%s, subTaskCount=%d", task.TaskId, task.SubTaskCount)
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

func (l *MainTaskResumeLogic) MainTaskResume(req *types.MainTaskControlReq, workspaceId string) (resp *types.BaseResp, err error) {
	// 优先使用请求中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	if wsId == "" {
		return &types.BaseResp{Code: 400, Msg: "workspaceId不能为空"}, nil
	}
	
	l.Logger.Infof("MainTaskResume: id=%s, workspaceId=%s", req.Id, wsId)
	
	taskModel := l.svcCtx.GetMainTaskModel(wsId)

	// 获取任务
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskResume: task not found, id=%s, error=%v", req.Id, err)
		return &types.BaseResp{Code: 400, Msg: "任务不存在"}, nil
	}

	l.Logger.Infof("MainTaskResume: found task, taskId=%s, status=%s, subTaskCount=%d, subTaskDone=%d", 
		task.TaskId, task.Status, task.SubTaskCount, task.SubTaskDone)

	// 检查状态：只有PAUSED状态可以继续
	if task.Status != model.TaskStatusPaused {
		return &types.BaseResp{Code: 400, Msg: "只有已暂停的任务可以继续"}, nil
	}

	// 清除暂停信号
	ctrlKey := "cscan:task:ctrl:" + task.TaskId
	l.svcCtx.RedisClient.Del(l.ctx, ctrlKey)
	
	// 如果有子任务，也清除所有子任务的暂停信号
	if task.SubTaskCount > 1 {
		for i := 0; i < task.SubTaskCount; i++ {
			subTaskId := fmt.Sprintf("%s-%d", task.TaskId, i)
			subCtrlKey := "cscan:task:ctrl:" + subTaskId
			l.svcCtx.RedisClient.Del(l.ctx, subCtrlKey)
		}
		l.Logger.Infof("MainTaskResume: cleared pause signals for %d sub-tasks", task.SubTaskCount)
	}

	// 更新状态为STARTED
	update := bson.M{"status": model.TaskStatusStarted}
	if err := taskModel.Update(l.ctx, req.Id, update); err != nil {
		l.Logger.Errorf("MainTaskResume: failed to update status, error=%v", err)
		return &types.BaseResp{Code: 500, Msg: "更新任务状态失败"}, nil
	}
	l.Logger.Infof("MainTaskResume: status updated to STARTED")

	// 解析任务配置
	var taskConfig map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &taskConfig); err != nil {
		l.Logger.Errorf("MainTaskResume: failed to parse config, error=%v", err)
		return &types.BaseResp{Code: 500, Msg: "解析任务配置失败"}, nil
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

	// 获取目标
	target, _ := taskConfig["target"].(string)

	// 从配置中获取批次大小，默认50
	batchSize := 50
	if bs, ok := taskConfig["batchSize"].(float64); ok {
		if bs == 0 {
			batchSize = 1000000
		} else if bs > 0 {
			batchSize = int(bs)
		}
	}

	// 使用目标拆分器判断是否需要拆分
	splitter := scheduler.NewTargetSplitter(batchSize)
	batches := splitter.SplitTargets(target)

	// 如果任务有保存的状态，注入到配置中
	if task.TaskState != "" {
		taskConfig["resumeState"] = task.TaskState
	}

	// 重新推送所有子任务到队列（从已完成的位置继续）
	// 注意：这里简化处理，重新推送所有批次，Worker 会根据 resumeState 跳过已完成的阶段
	var schedTasks []*scheduler.TaskInfo
	for i, batch := range batches {
		// 复制配置并替换目标
		subConfig := make(map[string]interface{})
		for k, v := range taskConfig {
			subConfig[k] = v
		}
		subConfig["target"] = batch
		subConfig["subTaskIndex"] = i
		subConfig["subTaskTotal"] = len(batches)
		subConfigBytes, _ := json.Marshal(subConfig)

		// 生成子任务ID
		subTaskId := task.TaskId
		if len(batches) > 1 {
			subTaskId = task.TaskId + "-" + strconv.Itoa(i)
		}

		schedTask := &scheduler.TaskInfo{
			TaskId:      subTaskId,
			MainTaskId:  task.Id.Hex(),
			WorkspaceId: wsId,
			TaskName:    task.Name,
			Config:      string(subConfigBytes),
			Priority:    1,
			Workers:     workers,
		}
		schedTasks = append(schedTasks, schedTask)

		// 保存子任务信息到 Redis（多批次时）
		if len(batches) > 1 {
			subTaskInfoKey := "cscan:task:info:" + subTaskId
			subTaskInfoData, _ := json.Marshal(map[string]interface{}{
				"workspaceId":  wsId,
				"mainTaskId":   task.Id.Hex(),
				"parentTaskId": task.TaskId,
				"subTaskCount": task.SubTaskCount,
			})
			l.svcCtx.RedisClient.Set(l.ctx, subTaskInfoKey, subTaskInfoData, 24*time.Hour)
		}
	}

	// 批量推送任务
	l.Logger.Infof("MainTaskResume: pushing %d sub-tasks to queue", len(schedTasks))
	if err := l.svcCtx.Scheduler.PushTaskBatch(l.ctx, schedTasks); err != nil {
		l.Logger.Errorf("MainTaskResume: push tasks to queue failed: %v", err)
		return &types.BaseResp{Code: 500, Msg: "任务入队失败"}, nil
	}

	l.Logger.Infof("MainTaskResume: task resumed successfully, taskId=%s, subTasks=%d", task.TaskId, len(schedTasks))
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

func (l *MainTaskStopLogic) MainTaskStop(req *types.MainTaskControlReq, workspaceId string) (resp *types.BaseResp, err error) {
	// 优先使用请求中的 workspaceId
	wsId := req.WorkspaceId
	if wsId == "" {
		wsId = workspaceId
	}
	if wsId == "" {
		return &types.BaseResp{Code: 400, Msg: "workspaceId不能为空"}, nil
	}
	
	taskModel := l.svcCtx.GetMainTaskModel(wsId)

	// 获取任务
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
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

	// 发送停止信号到Redis（Set用于HTTP轮询，Publish用于WebSocket推送）
	// 1. 发送给主任务
	ctrlKey := "cscan:task:ctrl:" + task.TaskId
	l.svcCtx.RedisClient.Set(l.ctx, ctrlKey, "STOP", 24*time.Hour)
	l.svcCtx.RedisClient.Publish(l.ctx, ctrlKey, "STOP")
	
	// 2. 如果有子任务，也发送给所有子任务
	if task.SubTaskCount > 1 {
		for i := 0; i < task.SubTaskCount; i++ {
			subTaskId := fmt.Sprintf("%s-%d", task.TaskId, i)
			subCtrlKey := "cscan:task:ctrl:" + subTaskId
			l.svcCtx.RedisClient.Set(l.ctx, subCtrlKey, "STOP", 24*time.Hour)
			l.svcCtx.RedisClient.Publish(l.ctx, subCtrlKey, "STOP")
		}
		l.Logger.Infof("Task stop signal sent to %d sub-tasks", task.SubTaskCount)
	}

	// 更新状态为STOPPED，设置结束时间
	now := time.Now()
	update := bson.M{
		"status":   model.TaskStatusStopped,
		"result":   "任务已手动停止",
		"end_time": now,
	}
	if err := taskModel.Update(l.ctx, req.Id, update); err != nil {
		return &types.BaseResp{Code: 500, Msg: "更新任务状态失败"}, nil
	}

	l.Logger.Infof("Task stopped: taskId=%s", task.TaskId)
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

func (l *TaskStatLogic) TaskStat(workspaceId string) (resp *types.TaskStatResp, err error) {
	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)

	// 统计总数
	total, _ := taskModel.Count(l.ctx, bson.M{})

	// 按状态统计
	completed, _ := taskModel.Count(l.ctx, bson.M{"status": model.TaskStatusSuccess})
	running, _ := taskModel.Count(l.ctx, bson.M{"status": model.TaskStatusStarted})
	failed, _ := taskModel.Count(l.ctx, bson.M{"status": model.TaskStatusFailure})
	pending, _ := taskModel.Count(l.ctx, bson.M{"status": bson.M{"$in": []string{model.TaskStatusPending, model.TaskStatusCreated}}})

	// 近7天每日趋势统计
	now := time.Now()
	trendDays := make([]string, 7)
	trendCompleted := make([]int, 7)
	trendFailed := make([]int, 7)

	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		dayEnd := dayStart.AddDate(0, 0, 1)

		idx := 6 - i
		trendDays[idx] = dayStart.Format("01-02")

		// 统计当天完成的任务
		completedCount, _ := taskModel.Count(l.ctx, bson.M{
			"status":      model.TaskStatusSuccess,
			"update_time": bson.M{"$gte": dayStart, "$lt": dayEnd},
		})
		trendCompleted[idx] = int(completedCount)

		// 统计当天失败的任务
		failedCount, _ := taskModel.Count(l.ctx, bson.M{
			"status":      model.TaskStatusFailure,
			"update_time": bson.M{"$gte": dayStart, "$lt": dayEnd},
		})
		trendFailed[idx] = int(failedCount)
	}

	return &types.TaskStatResp{
		Code:           0,
		Msg:            "success",
		Total:          int(total),
		Completed:      int(completed),
		Running:        int(running),
		Failed:         int(failed),
		Pending:        int(pending),
		TrendDays:      trendDays,
		TrendCompleted: trendCompleted,
		TrendFailed:    trendFailed,
	}, nil
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

func (l *MainTaskUpdateLogic) MainTaskUpdate(req *types.MainTaskUpdateReq, workspaceId string) (resp *types.BaseResp, err error) {
	taskModel := l.svcCtx.GetMainTaskModel(workspaceId)

	// 如果更新了目标，校验目标格式
	if req.Target != "" {
		if validationErrors := common.ValidateTargets(req.Target); len(validationErrors) > 0 {
			return &types.BaseResp{Code: 400, Msg: common.FormatValidationErrors(validationErrors)}, nil
		}
	}

	// 获取任务
	task, err := taskModel.FindById(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("MainTaskUpdate: task not found, id=%s, error=%v", req.Id, err)
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

	if req.ProfileId != "" {
		// 验证配置是否存在
		profile, err := l.svcCtx.ProfileModel.FindById(l.ctx, req.ProfileId)
		if err != nil {
			l.Logger.Errorf("MainTaskUpdate: profile not found, profileId=%s, error=%v", req.ProfileId, err)
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
			if err == nil && profile.Config != "" {
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

	l.Logger.Infof("MainTaskUpdate: task updated, id=%s, workspaceId=%s", req.Id, workspaceId)
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

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// 从Redis Stream读取任务专属日志 (cscan:task:logs:{taskId})
	streamKey := "cscan:task:logs:" + req.TaskId
	l.Logger.Infof("GetTaskLogs: querying Redis stream key=%s, limit=%d, search=%s", streamKey, limit, req.Search)
	
	logs, err := l.svcCtx.RedisClient.XRevRange(l.ctx, streamKey, "+", "-").Result()
	if err != nil {
		l.Logger.Errorf("GetTaskLogs: failed to read logs from Redis, taskId=%s, streamKey=%s, error=%v", req.TaskId, streamKey, err)
		// 返回空列表而不是错误
		return &types.GetTaskLogsResp{Code: 0, Msg: "Redis查询失败: " + err.Error(), List: []types.TaskLogEntry{}}, nil
	}

	l.Logger.Infof("GetTaskLogs: found %d log entries in Redis stream", len(logs))

	// 解析日志条目
	result := make([]types.TaskLogEntry, 0)
	searchLower := strings.ToLower(req.Search)

	// XRevRange返回的是倒序，我们需要正序显示，所以从后往前遍历
	for i := len(logs) - 1; i >= 0; i-- {
		if data, ok := logs[i].Values["data"].(string); ok {
			var entry types.TaskLogEntry
			if err := json.Unmarshal([]byte(data), &entry); err == nil {
				// 放宽匹配条件：匹配主任务ID或子任务ID
				if entry.TaskId == req.TaskId || getMainTaskIdFromLog(entry.TaskId) == req.TaskId {
					// 模糊搜索过滤
					if req.Search != "" {
						// 搜索 message、level、workerName 字段（不区分大小写）
						if !strings.Contains(strings.ToLower(entry.Message), searchLower) &&
							!strings.Contains(strings.ToLower(entry.Level), searchLower) &&
							!strings.Contains(strings.ToLower(entry.WorkerName), searchLower) {
							continue
						}
					}
					result = append(result, entry)
					// 达到限制数量后停止
					if len(result) >= limit {
						break
					}
				}
			} else {
				l.Logger.Errorf("GetTaskLogs: failed to unmarshal log entry: %v", err)
			}
		}
	}

	l.Logger.Infof("GetTaskLogs: returned %d logs for taskId=%s", len(result), req.TaskId)
	return &types.GetTaskLogsResp{Code: 0, Msg: "success", List: result}, nil
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
