package logic

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/xuri/excelize/v2"
	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

// ReportDetailLogic 报告详情
type ReportDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReportDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReportDetailLogic {
	return &ReportDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ReportDetailLogic) ReportDetail(req *types.ReportDetailReq, workspaceId string) (*types.ReportDetailResp, error) {
	l.Logger.Infof("ReportDetail: taskId=%s, workspaceId=%s", req.TaskId, workspaceId)
	
	// 获取任务信息
	// 当 workspaceId 为 "all" 或空时，需要遍历所有工作空间查找任务
	var task *model.MainTask
	var err error
	var actualWorkspaceId string
	
	if workspaceId == "" || workspaceId == "all" {
		// 获取所有工作空间
		wsModel := model.NewWorkspaceModel(l.svcCtx.MongoDB)
		workspaces, _ := wsModel.FindAll(l.ctx)
		
		// 先尝试 default 工作空间
		wsIds := []string{"default"}
		for _, ws := range workspaces {
			wsIds = append(wsIds, ws.Id.Hex())
		}
		
		// 遍历所有工作空间查找任务
		for _, wsId := range wsIds {
			taskModel := l.svcCtx.GetMainTaskModel(wsId)
			task, err = taskModel.FindById(l.ctx, req.TaskId)
			if err == nil && task != nil {
				actualWorkspaceId = wsId
				l.Logger.Infof("Found task in workspace: %s", wsId)
				break
			}
		}
		
		if task == nil {
			l.Logger.Errorf("FindById failed in all workspaces: %v", err)
			return &types.ReportDetailResp{Code: 400, Msg: "任务不存在"}, nil
		}
	} else {
		actualWorkspaceId = workspaceId
		taskModel := l.svcCtx.GetMainTaskModel(workspaceId)
		task, err = taskModel.FindById(l.ctx, req.TaskId)
		if err != nil {
			l.Logger.Errorf("FindById failed: %v", err)
			return &types.ReportDetailResp{Code: 400, Msg: "任务不存在"}, nil
		}
	}
	
	// 资产保存时使用的是 task.Id.Hex() (ObjectID) 作为 taskId
	// 所以查询时也需要使用 ObjectID
	queryTaskId := task.Id.Hex()
	l.Logger.Infof("Found task: name=%s, taskId(UUID)=%s, objectId=%s, actualWorkspaceId=%s", task.Name, task.TaskId, queryTaskId, actualWorkspaceId)

	// 获取资产列表
	assetModel := l.svcCtx.GetAssetModel(actualWorkspaceId)
	
	// 构建查询条件：匹配主任务ID或子任务ID（子任务格式: {mainTaskId}-{index}）
	// 注意：子任务ID格式是 {UUID}-{index}，但资产保存时使用的是 ObjectID
	assetFilter := bson.M{
		"$or": []bson.M{
			{"taskId": queryTaskId},                                    // 主任务ID (ObjectID)
			{"taskId": bson.M{"$regex": "^" + queryTaskId + "-\\d+$"}}, // 子任务ID (ObjectID-index)
			{"taskId": task.TaskId},                                    // 兼容：UUID格式
			{"taskId": bson.M{"$regex": "^" + task.TaskId + "-\\d+$"}}, // 兼容：UUID-index格式
		},
	}
	assets, err := assetModel.Find(l.ctx, assetFilter, 0, 0)
	if err != nil {
		l.Logger.Errorf("查询资产失败: %v", err)
	}
	l.Logger.Infof("Found %d assets for task (objectId=%s, UUID=%s)", len(assets), queryTaskId, task.TaskId)

	// 获取漏洞列表
	vulModel := l.svcCtx.GetVulModel(actualWorkspaceId)
	// 同样匹配主任务ID或子任务ID
	vulFilter := bson.M{
		"$or": []bson.M{
			{"task_id": queryTaskId},                                    // 主任务ID (ObjectID)
			{"task_id": bson.M{"$regex": "^" + queryTaskId + "-\\d+$"}}, // 子任务ID (ObjectID-index)
			{"task_id": task.TaskId},                                    // 兼容：UUID格式
			{"task_id": bson.M{"$regex": "^" + task.TaskId + "-\\d+$"}}, // 兼容：UUID-index格式
		},
	}
	vuls, err := vulModel.Find(l.ctx, vulFilter, 0, 0)
	if err != nil {
		l.Logger.Errorf("查询漏洞失败: %v", err)
	}
	l.Logger.Infof("Found %d vuls for task (objectId=%s, UUID=%s)", len(vuls), queryTaskId, task.TaskId)

	// 获取目录扫描结果
	dirScanModel := l.svcCtx.GetDirScanResultModel()
	dirScanFilter := bson.M{
		"$or": []bson.M{
			{"main_task_id": queryTaskId},
			{"main_task_id": bson.M{"$regex": "^" + queryTaskId + "-\\d+$"}},
			{"main_task_id": task.TaskId},
			{"main_task_id": bson.M{"$regex": "^" + task.TaskId + "-\\d+$"}},
		},
	}
	// 如果有 actualWorkspaceId，添加过滤条件
	if actualWorkspaceId != "" && actualWorkspaceId != "all" {
		dirScanFilter["workspace_id"] = actualWorkspaceId
	}
	dirScans, err := dirScanModel.FindByFilter(l.ctx, dirScanFilter, 1, 1000)
	if err != nil {
		l.Logger.Errorf("查询目录扫描结果失败: %v", err)
	}
	l.Logger.Infof("Found %d dirscan results for task (objectId=%s, UUID=%s)", len(dirScans), queryTaskId, task.TaskId)

	// 统计信息
	portStats := make(map[int]int)
	serviceStats := make(map[string]int)
	appStats := make(map[string]int)
	severityStats := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0, "info": 0, "unknown": 0}

	for _, asset := range assets {
		portStats[asset.Port]++
		if asset.Service != "" {
			serviceStats[asset.Service]++
		}
		for _, app := range asset.App {
			appStats[app]++
		}
	}

	for _, vul := range vuls {
		severity := strings.ToLower(vul.Severity)
		if _, ok := severityStats[severity]; ok {
			severityStats[severity]++
		}
	}

	// 目录扫描统计
	dirScanStat := types.ReportDirScanStat{Total: len(dirScans)}
	for _, ds := range dirScans {
		if ds.StatusCode >= 200 && ds.StatusCode < 300 {
			dirScanStat.Status2xx++
		} else if ds.StatusCode >= 300 && ds.StatusCode < 400 {
			dirScanStat.Status3xx++
		} else if ds.StatusCode >= 400 && ds.StatusCode < 500 {
			dirScanStat.Status4xx++
		} else if ds.StatusCode >= 500 {
			dirScanStat.Status5xx++
		}
	}

	// 转换资产列表
	assetList := make([]types.ReportAsset, 0, len(assets))
	for _, a := range assets {
		assetList = append(assetList, types.ReportAsset{
			Authority:  a.Authority,
			Host:       a.Host,
			Port:       a.Port,
			Service:    a.Service,
			Title:      a.Title,
			App:        a.App,
			HttpStatus: a.HttpStatus,
			Server:     a.Server,
			IconHash:   a.IconHash,
			Screenshot: a.Screenshot,
			CreateTime: a.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	// 转换漏洞列表
	vulList := make([]types.ReportVul, 0, len(vuls))
	for _, v := range vuls {
		vulList = append(vulList, types.ReportVul{
			Authority:  v.Authority,
			Url:        v.Url,
			PocFile:    v.PocFile,
			Severity:   v.Severity,
			Result:     v.Result,
			CreateTime: v.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	// 转换统计
	topPorts := make([]types.StatItem, 0)
	for port, count := range portStats {
		topPorts = append(topPorts, types.StatItem{Name: strconv.Itoa(port), Count: count})
	}
	topServices := make([]types.StatItem, 0)
	for svc, count := range serviceStats {
		topServices = append(topServices, types.StatItem{Name: svc, Count: count})
	}
	topApps := make([]types.StatItem, 0)
	for app, count := range appStats {
		topApps = append(topApps, types.StatItem{Name: app, Count: count})
	}

	// 转换目录扫描结果列表
	dirScanList := make([]types.ReportDirScan, 0, len(dirScans))
	for _, ds := range dirScans {
		dirScanList = append(dirScanList, types.ReportDirScan{
			Authority:     ds.Authority,
			URL:           ds.URL,
			Path:          ds.Path,
			StatusCode:    ds.StatusCode,
			ContentLength: ds.ContentLength,
			ContentType:   ds.ContentType,
			Title:         ds.Title,
			CreateTime:    ds.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.ReportDetailResp{
		Code: 0,
		Msg:  "success",
		Data: &types.ReportData{
			TaskId:       req.TaskId,
			TaskName:     task.Name,
			Target:       task.Target,
			Status:       task.Status,
			CreateTime:   task.CreateTime.Local().Format("2006-01-02 15:04:05"),
			AssetCount:   len(assets),
			VulCount:     len(vuls),
			DirScanCount: len(dirScans),
			Assets:       assetList,
			Vuls:         vulList,
			DirScans:     dirScanList,
			DirScanStat:  dirScanStat,
			TopPorts:     topPorts,
			TopServices:  topServices,
			TopApps:      topApps,
			VulStats:     severityStats,
		},
	}, nil
}

// ReportExportLogic 报告导出
type ReportExportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReportExportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReportExportLogic {
	return &ReportExportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ReportExportLogic) ReportExport(req *types.ReportExportReq, workspaceId string) ([]byte, string, error) {
	// 获取任务信息
	// 当 workspaceId 为 "all" 或空时，需要遍历所有工作空间查找任务
	var task *model.MainTask
	var err error
	var actualWorkspaceId string
	
	if workspaceId == "" || workspaceId == "all" {
		// 获取所有工作空间
		wsModel := model.NewWorkspaceModel(l.svcCtx.MongoDB)
		workspaces, _ := wsModel.FindAll(l.ctx)
		
		// 先尝试 default 工作空间
		wsIds := []string{"default"}
		for _, ws := range workspaces {
			wsIds = append(wsIds, ws.Id.Hex())
		}
		
		// 遍历所有工作空间查找任务
		for _, wsId := range wsIds {
			taskModel := l.svcCtx.GetMainTaskModel(wsId)
			task, err = taskModel.FindById(l.ctx, req.TaskId)
			if err == nil && task != nil {
				actualWorkspaceId = wsId
				break
			}
		}
		
		if task == nil {
			return nil, "", fmt.Errorf("任务不存在")
		}
	} else {
		actualWorkspaceId = workspaceId
		taskModel := l.svcCtx.GetMainTaskModel(workspaceId)
		task, err = taskModel.FindById(l.ctx, req.TaskId)
		if err != nil {
			return nil, "", fmt.Errorf("任务不存在")
		}
	}

	// 资产保存时使用的是 task.Id.Hex() (ObjectID) 作为 taskId
	queryTaskId := task.Id.Hex()

	// 获取资产列表（匹配主任务ID或子任务ID，同时兼容UUID和ObjectID格式）
	assetModel := l.svcCtx.GetAssetModel(actualWorkspaceId)
	assetFilter := bson.M{
		"$or": []bson.M{
			{"taskId": queryTaskId},
			{"taskId": bson.M{"$regex": "^" + queryTaskId + "-\\d+$"}},
			{"taskId": task.TaskId},
			{"taskId": bson.M{"$regex": "^" + task.TaskId + "-\\d+$"}},
		},
	}
	assets, _ := assetModel.Find(l.ctx, assetFilter, 0, 0)

	// 获取漏洞列表（匹配主任务ID或子任务ID，同时兼容UUID和ObjectID格式）
	vulModel := l.svcCtx.GetVulModel(actualWorkspaceId)
	vulFilter := bson.M{
		"$or": []bson.M{
			{"task_id": queryTaskId},
			{"task_id": bson.M{"$regex": "^" + queryTaskId + "-\\d+$"}},
			{"task_id": task.TaskId},
			{"task_id": bson.M{"$regex": "^" + task.TaskId + "-\\d+$"}},
		},
	}
	vuls, _ := vulModel.Find(l.ctx, vulFilter, 0, 0)

	// 获取目录扫描结果
	dirScanModel := l.svcCtx.GetDirScanResultModel()
	dirScanFilter := bson.M{
		"$or": []bson.M{
			{"main_task_id": queryTaskId},
			{"main_task_id": bson.M{"$regex": "^" + queryTaskId + "-\\d+$"}},
			{"main_task_id": task.TaskId},
			{"main_task_id": bson.M{"$regex": "^" + task.TaskId + "-\\d+$"}},
		},
	}
	if actualWorkspaceId != "" && actualWorkspaceId != "all" {
		dirScanFilter["workspace_id"] = actualWorkspaceId
	}
	dirScans, _ := dirScanModel.FindByFilter(l.ctx, dirScanFilter, 1, 10000)

	// 创建Excel文件
	f := excelize.NewFile()
	defer f.Close()

	// 概览Sheet
	f.SetSheetName("Sheet1", "概览")
	f.SetCellValue("概览", "A1", "扫描报告")
	f.SetCellValue("概览", "A3", "任务名称")
	f.SetCellValue("概览", "B3", task.Name)
	f.SetCellValue("概览", "A4", "扫描目标")
	f.SetCellValue("概览", "B4", task.Target)
	f.SetCellValue("概览", "A5", "任务状态")
	f.SetCellValue("概览", "B5", task.Status)
	f.SetCellValue("概览", "A6", "创建时间")
	f.SetCellValue("概览", "B6", task.CreateTime.Local().Format("2006-01-02 15:04:05"))
	f.SetCellValue("概览", "A7", "资产数量")
	f.SetCellValue("概览", "B7", len(assets))
	f.SetCellValue("概览", "A8", "漏洞数量")
	f.SetCellValue("概览", "B8", len(vuls))
	f.SetCellValue("概览", "A9", "目录扫描数量")
	f.SetCellValue("概览", "B9", len(dirScans))

	// 设置概览样式
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 16},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	f.SetCellStyle("概览", "A1", "A1", titleStyle)
	f.MergeCell("概览", "A1", "B1")

	// 资产Sheet
	f.NewSheet("资产列表")
	assetHeaders := []string{"地址", "主机", "端口", "服务", "标题", "应用", "状态码", "Server", "IconHash", "发现时间"}
	for i, h := range assetHeaders {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue("资产列表", cell, h)
	}
	for i, a := range assets {
		row := i + 2
		f.SetCellValue("资产列表", fmt.Sprintf("A%d", row), a.Authority)
		f.SetCellValue("资产列表", fmt.Sprintf("B%d", row), a.Host)
		f.SetCellValue("资产列表", fmt.Sprintf("C%d", row), a.Port)
		f.SetCellValue("资产列表", fmt.Sprintf("D%d", row), a.Service)
		f.SetCellValue("资产列表", fmt.Sprintf("E%d", row), a.Title)
		f.SetCellValue("资产列表", fmt.Sprintf("F%d", row), strings.Join(a.App, ", "))
		f.SetCellValue("资产列表", fmt.Sprintf("G%d", row), a.HttpStatus)
		f.SetCellValue("资产列表", fmt.Sprintf("H%d", row), a.Server)
		f.SetCellValue("资产列表", fmt.Sprintf("I%d", row), a.IconHash)
		f.SetCellValue("资产列表", fmt.Sprintf("J%d", row), a.CreateTime.Local().Format("2006-01-02 15:04:05"))
	}

	// 漏洞Sheet
	f.NewSheet("漏洞列表")
	vulHeaders := []string{"地址", "URL", "POC", "严重级别", "结果", "发现时间"}
	for i, h := range vulHeaders {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue("漏洞列表", cell, h)
	}
	for i, v := range vuls {
		row := i + 2
		f.SetCellValue("漏洞列表", fmt.Sprintf("A%d", row), v.Authority)
		f.SetCellValue("漏洞列表", fmt.Sprintf("B%d", row), v.Url)
		f.SetCellValue("漏洞列表", fmt.Sprintf("C%d", row), v.PocFile)
		f.SetCellValue("漏洞列表", fmt.Sprintf("D%d", row), v.Severity)
		f.SetCellValue("漏洞列表", fmt.Sprintf("E%d", row), v.Result)
		f.SetCellValue("漏洞列表", fmt.Sprintf("F%d", row), v.CreateTime.Local().Format("2006-01-02 15:04:05"))
	}

	// 目录扫描Sheet
	f.NewSheet("目录扫描")
	dirScanHeaders := []string{"目标", "URL", "路径", "状态码", "大小", "类型", "标题", "发现时间"}
	for i, h := range dirScanHeaders {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue("目录扫描", cell, h)
	}
	for i, ds := range dirScans {
		row := i + 2
		f.SetCellValue("目录扫描", fmt.Sprintf("A%d", row), ds.Authority)
		f.SetCellValue("目录扫描", fmt.Sprintf("B%d", row), ds.URL)
		f.SetCellValue("目录扫描", fmt.Sprintf("C%d", row), ds.Path)
		f.SetCellValue("目录扫描", fmt.Sprintf("D%d", row), ds.StatusCode)
		f.SetCellValue("目录扫描", fmt.Sprintf("E%d", row), ds.ContentLength)
		f.SetCellValue("目录扫描", fmt.Sprintf("F%d", row), ds.ContentType)
		f.SetCellValue("目录扫描", fmt.Sprintf("G%d", row), ds.Title)
		f.SetCellValue("目录扫描", fmt.Sprintf("H%d", row), ds.CreateTime.Local().Format("2006-01-02 15:04:05"))
	}

	// 设置列宽
	f.SetColWidth("资产列表", "A", "A", 30)
	f.SetColWidth("资产列表", "B", "B", 15)
	f.SetColWidth("资产列表", "E", "E", 40)
	f.SetColWidth("资产列表", "F", "F", 30)
	f.SetColWidth("漏洞列表", "A", "A", 30)
	f.SetColWidth("漏洞列表", "B", "B", 50)
	f.SetColWidth("漏洞列表", "C", "C", 40)
	f.SetColWidth("漏洞列表", "E", "E", 50)
	f.SetColWidth("目录扫描", "A", "A", 25)
	f.SetColWidth("目录扫描", "B", "B", 50)
	f.SetColWidth("目录扫描", "C", "C", 30)
	f.SetColWidth("目录扫描", "G", "G", 30)

	// 写入buffer
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, "", err
	}

	filename := fmt.Sprintf("report_%s_%s.xlsx", task.Name, time.Now().Format("20060102150405"))
	return buf.Bytes(), filename, nil
}
