package task

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/model"
	"cscan/pkg/response"
	"cscan/scheduler"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// CronTaskListReq 定时任务列表请求
type CronTaskListReq struct {
	Page     int    `json:"page,optional"`
	PageSize int    `json:"pageSize,optional"`
	Keyword  string `json:"keyword,optional"`
}

// CronTaskListResp 定时任务列表响应
type CronTaskListResp struct {
	Code int                   `json:"code"`
	Msg  string                `json:"msg"`
	Data *CronTaskListRespData `json:"data"`
}

type CronTaskListRespData struct {
	List  []*CronTaskItem `json:"list"`
	Total int             `json:"total"`
}

type CronTaskItem struct {
	Id           string `json:"id"`
	Name         string `json:"name"`
	ScheduleType string `json:"scheduleType"` // cron/once
	CronSpec     string `json:"cronSpec"`
	ScheduleTime string `json:"scheduleTime"`
	WorkspaceId  string `json:"workspaceId"`
	MainTaskId   string `json:"mainTaskId"`
	TaskName     string `json:"taskName"`
	Target       string `json:"target"`
	TargetShort  string `json:"targetShort"` // 截断后的目标（用于列表显示）
	Config       string `json:"config"`      // 任务配置JSON
	Status       string `json:"status"`
	LastRunTime  string `json:"lastRunTime"`
	NextRunTime  string `json:"nextRunTime"`
	RunCount     int64  `json:"runCount"`
}

// CronTaskSaveReq 保存定时任务请求
type CronTaskSaveReq struct {
	Id           string `json:"id,optional"`
	Name         string `json:"name"`
	ScheduleType string `json:"scheduleType"`          // cron: Cron表达式, once: 指定时间
	CronSpec     string `json:"cronSpec,optional"`     // Cron表达式
	ScheduleTime string `json:"scheduleTime,optional"` // 指定执行时间 (格式: 2006-01-02 15:04:05)
	MainTaskId   string `json:"mainTaskId"`            // 关联的任务ID（用于获取初始配置）
	WorkspaceId  string `json:"workspaceId,optional"`  // 任务所属工作空间ID
	Target       string `json:"target,optional"`       // 扫描目标（可自定义，不填则使用关联任务的目标）
	Config       string `json:"config,optional"`       // 任务配置JSON（可自定义，不填则使用关联任务的配置）
}

// CronTaskToggleReq 开关定时任务请求
type CronTaskToggleReq struct {
	Id     string `json:"id"`
	Status string `json:"status"` // enable/disable
}

// CronTaskDeleteReq 删除定时任务请求
type CronTaskDeleteReq struct {
	Id string `json:"id"`
}

// CronTaskBatchDeleteReq 批量删除定时任务请求
type CronTaskBatchDeleteReq struct {
	Ids []string `json:"ids"`
}

// CronTaskListHandler 定时任务列表
func CronTaskListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CronTaskListReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		workspaceId := middleware.GetWorkspaceId(r.Context())
		ctx := r.Context()

		// 从Redis获取定时任务
		cronKey := "cscan:cron:tasks"
		data, err := svcCtx.RedisClient.HGetAll(ctx, cronKey).Result()
		if err != nil {
			response.Error(w, fmt.Errorf("获取定时任务失败: %v", err))
			return
		}

		var list []*CronTaskItem
		for id, taskData := range data {
			var task scheduler.CronTask
			if err := json.Unmarshal([]byte(taskData), &task); err != nil {
				continue
			}

			// 过滤工作空间
			if workspaceId != "" && workspaceId != "all" && task.WorkspaceId != workspaceId {
				continue
			}

			// 关键字搜索
			if req.Keyword != "" && task.Name != req.Keyword && task.TaskName != req.Keyword {
				continue
			}

			// 获取运行次数
			runCountKey := fmt.Sprintf("cscan:cron:runcount:%s", id)
			runCount, _ := svcCtx.RedisClient.Get(ctx, runCountKey).Int64()

			// 截取目标显示（用于列表）
			targetShort := task.Target
			if len(targetShort) > 100 {
				targetShort = targetShort[:100] + "..."
			}

			list = append(list, &CronTaskItem{
				Id:           id,
				Name:         task.Name,
				ScheduleType: task.ScheduleType,
				CronSpec:     task.CronSpec,
				ScheduleTime: task.ScheduleTime,
				WorkspaceId:  task.WorkspaceId,
				MainTaskId:   task.MainTaskId,
				TaskName:     task.TaskName,
				Target:       task.Target, // 完整目标（用于编辑）
				TargetShort:  targetShort, // 截断目标（用于列表显示）
				Config:       task.Config, // 完整配置（用于编辑）
				Status:       task.Status,
				LastRunTime:  task.LastRunTime,
				NextRunTime:  task.NextRunTime,
				RunCount:     runCount,
			})
		}

		// 分页
		total := len(list)
		if req.Page <= 0 {
			req.Page = 1
		}
		if req.PageSize <= 0 {
			req.PageSize = 20
		}
		start := (req.Page - 1) * req.PageSize
		end := start + req.PageSize
		if start > total {
			list = []*CronTaskItem{}
		} else if end > total {
			list = list[start:]
		} else {
			list = list[start:end]
		}

		if list == nil {
			list = []*CronTaskItem{}
		}

		httpx.OkJson(w, &CronTaskListResp{
			Code: 0,
			Msg:  "success",
			Data: &CronTaskListRespData{
				List:  list,
				Total: total,
			},
		})
	}
}

// CronTaskSaveHandler 保存定时任务
func CronTaskSaveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CronTaskSaveReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		if req.Name == "" {
			response.ParamError(w, "任务名称不能为空")
			return
		}
		if req.MainTaskId == "" {
			response.ParamError(w, "请选择关联的扫描任务")
			return
		}
		if req.ScheduleType == "" {
			req.ScheduleType = "cron"
		}

		var nextRunTime string

		// 验证调度配置
		if req.ScheduleType == "cron" {
			if req.CronSpec == "" {
				response.ParamError(w, "Cron表达式不能为空")
				return
			}
			parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
			schedule, err := parser.Parse(req.CronSpec)
			if err != nil {
				response.ParamError(w, fmt.Sprintf("无效的Cron表达式: %v", err))
				return
			}
			nextRunTime = schedule.Next(time.Now()).Local().Format("2006-01-02 15:04:05")
		} else if req.ScheduleType == "once" {
			if req.ScheduleTime == "" {
				response.ParamError(w, "请选择执行时间")
				return
			}
			// 验证时间格式
			t, err := time.ParseInLocation("2006-01-02 15:04:05", req.ScheduleTime, time.Local)
			if err != nil {
				response.ParamError(w, "时间格式无效，请使用 YYYY-MM-DD HH:mm:ss 格式")
				return
			}
			if t.Before(time.Now()) {
				response.ParamError(w, "执行时间不能早于当前时间")
				return
			}
			nextRunTime = req.ScheduleTime
		} else {
			response.ParamError(w, "无效的调度类型")
			return
		}

		workspaceId := req.WorkspaceId
		if workspaceId == "" || workspaceId == "all" {
			workspaceId = middleware.GetWorkspaceId(r.Context())
		}
		ctx := r.Context()
		cronKey := "cscan:cron:tasks"

		// 获取关联任务的信息
		var mainTask *model.MainTask
		var foundWorkspaceId string

		if workspaceId == "" || workspaceId == "all" {
			// 遍历所有工作空间查找任务
			workspaces, _ := svcCtx.WorkspaceModel.FindAll(ctx)
			workspaceIds := []string{"default"}
			for _, ws := range workspaces {
				workspaceIds = append(workspaceIds, ws.Id.Hex())
			}

			for _, wsId := range workspaceIds {
				taskModel := svcCtx.GetMainTaskModel(wsId)
				task, err := taskModel.FindByTaskId(ctx, req.MainTaskId)
				if err == nil && task != nil {
					mainTask = task
					foundWorkspaceId = wsId
					break
				}
			}
		} else {
			taskModel := svcCtx.GetMainTaskModel(workspaceId)
			mainTask, _ = taskModel.FindByTaskId(ctx, req.MainTaskId)
			foundWorkspaceId = workspaceId
		}

		if mainTask == nil {
			response.Error(w, fmt.Errorf("关联的任务不存在"))
			return
		}
		workspaceId = foundWorkspaceId

		// 确定使用的目标和配置（优先使用请求中的自定义值，否则使用关联任务的值）
		target := req.Target
		if target == "" {
			target = mainTask.Target
		}
		config := req.Config
		if config == "" {
			config = mainTask.Config
		}

		var task scheduler.CronTask
		isNew := req.Id == ""

		if isNew {
			// 新建
			task = scheduler.CronTask{
				Id:           uuid.New().String(),
				Name:         req.Name,
				ScheduleType: req.ScheduleType,
				CronSpec:     req.CronSpec,
				ScheduleTime: req.ScheduleTime,
				WorkspaceId:  workspaceId,
				MainTaskId:   req.MainTaskId,
				TaskName:     mainTask.Name,
				Target:       target,
				Config:       config,
				Status:       "enable", // 新建后默认启用
				NextRunTime:  nextRunTime,
			}
		} else {
			// 更新 - 先获取现有任务
			existingData, err := svcCtx.RedisClient.HGet(ctx, cronKey, req.Id).Result()
			if err != nil {
				response.Error(w, fmt.Errorf("定时任务不存在"))
				return
			}
			if err := json.Unmarshal([]byte(existingData), &task); err != nil {
				response.Error(w, fmt.Errorf("解析任务数据失败"))
				return
			}

			task.Name = req.Name
			task.ScheduleType = req.ScheduleType
			task.CronSpec = req.CronSpec
			task.ScheduleTime = req.ScheduleTime
			task.MainTaskId = req.MainTaskId
			task.TaskName = mainTask.Name
			task.Target = target
			task.Config = config
			task.NextRunTime = nextRunTime
		}

		// 保存到Redis
		data, _ := json.Marshal(task)
		if err := svcCtx.RedisClient.HSet(ctx, cronKey, task.Id, data).Err(); err != nil {
			response.Error(w, fmt.Errorf("保存定时任务失败: %v", err))
			return
		}

		// 如果任务是启用状态，需要重新注册到调度器
		if task.Status == "enable" {
			svcCtx.RedisClient.Publish(ctx, "cscan:cron:reload", task.Id)
		}

		response.SuccessWithMsg(w, "保存成功")
	}
}

// CronTaskToggleHandler 开关定时任务
func CronTaskToggleHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CronTaskToggleReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		if req.Id == "" {
			response.ParamError(w, "任务ID不能为空")
			return
		}
		if req.Status != "enable" && req.Status != "disable" {
			response.ParamError(w, "状态值无效")
			return
		}

		ctx := r.Context()
		cronKey := "cscan:cron:tasks"

		// 获取现有任务
		existingData, err := svcCtx.RedisClient.HGet(ctx, cronKey, req.Id).Result()
		if err != nil {
			response.Error(w, fmt.Errorf("任务不存在"))
			return
		}

		var task scheduler.CronTask
		if err := json.Unmarshal([]byte(existingData), &task); err != nil {
			response.Error(w, fmt.Errorf("解析任务数据失败"))
			return
		}

		task.Status = req.Status

		// 如果启用，更新下次运行时间
		if req.Status == "enable" {
			if task.ScheduleType == "cron" {
				parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
				if schedule, err := parser.Parse(task.CronSpec); err == nil {
					task.NextRunTime = schedule.Next(time.Now()).Local().Format("2006-01-02 15:04:05")
				}
			} else if task.ScheduleType == "once" {
				// 检查指定时间是否已过
				t, _ := time.ParseInLocation("2006-01-02 15:04:05", task.ScheduleTime, time.Local)
				if t.Before(time.Now()) {
					response.Error(w, fmt.Errorf("指定的执行时间已过，请修改执行时间"))
					return
				}
				task.NextRunTime = task.ScheduleTime
			}
		}

		// 保存到Redis
		data, _ := json.Marshal(task)
		if err := svcCtx.RedisClient.HSet(ctx, cronKey, task.Id, data).Err(); err != nil {
			response.Error(w, fmt.Errorf("更新定时任务失败: %v", err))
			return
		}

		// 通知调度器
		svcCtx.RedisClient.Publish(ctx, "cscan:cron:reload", task.Id)

		msg := "已启用"
		if req.Status == "disable" {
			msg = "已禁用"
		}
		response.SuccessWithMsg(w, msg)
	}
}

// CronTaskDeleteHandler 删除定时任务
func CronTaskDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CronTaskDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		if req.Id == "" {
			response.ParamError(w, "任务ID不能为空")
			return
		}

		ctx := r.Context()
		cronKey := "cscan:cron:tasks"

		// 删除任务
		if err := svcCtx.RedisClient.HDel(ctx, cronKey, req.Id).Err(); err != nil {
			response.Error(w, fmt.Errorf("删除定时任务失败: %v", err))
			return
		}

		// 删除运行次数记录
		runCountKey := fmt.Sprintf("cscan:cron:runcount:%s", req.Id)
		svcCtx.RedisClient.Del(ctx, runCountKey)

		// 通知调度器移除任务
		svcCtx.RedisClient.Publish(ctx, "cscan:cron:remove", req.Id)

		response.SuccessWithMsg(w, "删除成功")
	}
}

// CronTaskBatchDeleteHandler 批量删除定时任务
func CronTaskBatchDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CronTaskBatchDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		if len(req.Ids) == 0 {
			response.ParamError(w, "请选择要删除的任务")
			return
		}

		ctx := r.Context()
		cronKey := "cscan:cron:tasks"

		successCount := 0
		for _, id := range req.Ids {
			// 删除任务
			if err := svcCtx.RedisClient.HDel(ctx, cronKey, id).Err(); err == nil {
				successCount++
				// 删除运行次数记录
				runCountKey := fmt.Sprintf("cscan:cron:runcount:%s", id)
				svcCtx.RedisClient.Del(ctx, runCountKey)
				// 通知调度器移除任务
				svcCtx.RedisClient.Publish(ctx, "cscan:cron:remove", id)
			}
		}

		response.SuccessWithMsg(w, fmt.Sprintf("成功删除 %d 个定时任务", successCount))
	}
}

// CronTaskRunNowHandler 立即执行定时任务
func CronTaskRunNowHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CronTaskDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		if req.Id == "" {
			response.ParamError(w, "任务ID不能为空")
			return
		}

		ctx := r.Context()

		// 通知调度器立即执行
		svcCtx.RedisClient.Publish(ctx, "cscan:cron:runnow", req.Id)

		response.SuccessWithMsg(w, "已触发执行")
	}
}

// ValidateCronSpecHandler 验证Cron表达式
func ValidateCronSpecHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CronSpec string `json:"cronSpec"`
		}
		if err := httpx.Parse(r, &req); err != nil {
			response.ParamError(w, err.Error())
			return
		}

		parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		schedule, err := parser.Parse(req.CronSpec)
		if err != nil {
			httpx.OkJson(w, map[string]interface{}{
				"code": 1,
				"msg":  fmt.Sprintf("无效的Cron表达式: %v", err),
				"data": nil,
			})
			return
		}

		// 计算接下来5次执行时间
		var nextTimes []string
		t := time.Now()
		for i := 0; i < 5; i++ {
			t = schedule.Next(t)
			nextTimes = append(nextTimes, t.Local().Format("2006-01-02 15:04:05"))
		}

		httpx.OkJson(w, map[string]interface{}{
			"code": 0,
			"msg":  "success",
			"data": map[string]interface{}{
				"valid":     true,
				"nextTimes": nextTimes,
			},
		})
	}
}
