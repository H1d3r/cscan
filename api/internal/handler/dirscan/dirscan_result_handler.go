package dirscan

import (
	"encoding/json"
	"net/http"

	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/model"

	"github.com/zeromicro/go-zero/rest/httpx"
	"go.mongodb.org/mongo-driver/bson"
)

// ==================== 目录扫描结果 API ====================

// DirScanResultListReq 列表请求
type DirScanResultListReq struct {
	TaskId     string `json:"taskId"`
	Authority  string `json:"authority"`
	Url        string `json:"url"`
	Path       string `json:"path"`
	StatusCode int    `json:"statusCode"`
	SizeMin    *int64 `json:"sizeMin"` // 响应大小最小值
	SizeMax    *int64 `json:"sizeMax"` // 响应大小最大值
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
	SortField  string `json:"sortField"`      // 排序字段: statusCode, contentLength, contentWords, contentLines, duration
	SortOrder  string `json:"sortOrder"`      // 排序方向: asc, desc
	Query      string `json:"query,optional"` // 全局搜索关键词
}

// DirScanResultListResp 列表响应
type DirScanResultListResp struct {
	Code  int                   `json:"code"`
	Msg   string                `json:"msg"`
	List  []model.DirScanResult `json:"list"`
	Total int64                 `json:"total"`
}

// DirScanResultListHandler 目录扫描结果列表
func DirScanResultListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req DirScanResultListReq
		// 使用 json.Decoder 解析，它对 null 值更宽容
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &DirScanResultListResp{Code: 400, Msg: "参数解析失败: " + err.Error()})
			return
		}

		if req.Page <= 0 {
			req.Page = 1
		}
		if req.PageSize <= 0 {
			req.PageSize = 20
		}

		ctx := r.Context()
		workspaceId := middleware.GetWorkspaceId(ctx)
		resultModel := model.NewDirScanResultModel(svcCtx.MongoDB)

		// 如果有通用 Query 且未指定更细的 URL/Path/Authority 条件，则将 Query 作为 URL 关键字使用
		if req.Query != "" && req.Url == "" && req.Path == "" && req.Authority == "" {
			req.Url = req.Query
		}

		// 构建查询条件 - 当 workspaceId 为空或 "all" 时查询所有
		filter := bson.M{}
		if workspaceId != "" && workspaceId != "all" {
			filter["workspace_id"] = workspaceId
		}
		if req.TaskId != "" {
			filter["main_task_id"] = req.TaskId
		}
		if req.Authority != "" {
			filter["authority"] = bson.M{"$regex": req.Authority, "$options": "i"}
		}
		if req.Url != "" {
			filter["url"] = bson.M{"$regex": req.Url, "$options": "i"}
		}
		if req.Path != "" {
			filter["path"] = bson.M{"$regex": req.Path, "$options": "i"}
		}
		if req.StatusCode > 0 {
			filter["status_code"] = req.StatusCode
		}
		// 大小范围筛选
		if req.SizeMin != nil || req.SizeMax != nil {
			sizeFilter := bson.M{}
			if req.SizeMin != nil {
				sizeFilter["$gte"] = *req.SizeMin
			}
			if req.SizeMax != nil {
				sizeFilter["$lte"] = *req.SizeMax
			}
			filter["content_length"] = sizeFilter
		}

		// 先统计总数（不带分页）
		total, _ := resultModel.CountByFilter(ctx, filter)

		// 查询数据（支持排序）
		list, err := resultModel.FindByFilterWithSort(ctx, filter, req.Page, req.PageSize, req.SortField, req.SortOrder)
		if err != nil {
			httpx.OkJson(w, &DirScanResultListResp{Code: 500, Msg: "查询失败: " + err.Error()})
			return
		}

		// 如果没有数据，返回空列表而不是 nil
		if list == nil {
			list = []model.DirScanResult{}
		}

		httpx.OkJson(w, &DirScanResultListResp{
			Code:  0,
			Msg:   "success",
			List:  list,
			Total: total,
		})
	}
}

// DirScanResultStatReq 统计请求（不需要参数，从 context 获取 workspaceId）
type DirScanResultStatReq struct{}

// DirScanResultStatResp 统计响应
type DirScanResultStatResp struct {
	Code int              `json:"code"`
	Msg  string           `json:"msg"`
	Stat map[string]int64 `json:"stat"`
}

// DirScanResultStatHandler 目录扫描结果统计
func DirScanResultStatHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		workspaceId := middleware.GetWorkspaceId(ctx)
		resultModel := model.NewDirScanResultModel(svcCtx.MongoDB)

		stat, err := resultModel.Stat(ctx, workspaceId)
		if err != nil {
			httpx.OkJson(w, &DirScanResultStatResp{Code: 500, Msg: "统计失败: " + err.Error()})
			return
		}

		httpx.OkJson(w, &DirScanResultStatResp{
			Code: 0,
			Msg:  "success",
			Stat: stat,
		})
	}
}

// DirScanResultDeleteReq 删除请求
type DirScanResultDeleteReq struct {
	Id string `json:"id"`
}

// DirScanResultDeleteResp 删除响应
type DirScanResultDeleteResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// DirScanResultDeleteHandler 删除目录扫描结果
func DirScanResultDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req DirScanResultDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.OkJson(w, &DirScanResultDeleteResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if req.Id == "" {
			httpx.OkJson(w, &DirScanResultDeleteResp{Code: 400, Msg: "ID不能为空"})
			return
		}

		ctx := r.Context()
		resultModel := model.NewDirScanResultModel(svcCtx.MongoDB)

		if err := resultModel.Delete(ctx, req.Id); err != nil {
			httpx.OkJson(w, &DirScanResultDeleteResp{Code: 500, Msg: "删除失败: " + err.Error()})
			return
		}

		httpx.OkJson(w, &DirScanResultDeleteResp{Code: 0, Msg: "删除成功"})
	}
}

// DirScanResultBatchDeleteReq 批量删除请求
type DirScanResultBatchDeleteReq struct {
	Ids []string `json:"ids"`
}

// DirScanResultBatchDeleteResp 批量删除响应
type DirScanResultBatchDeleteResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Deleted int64  `json:"deleted"`
}

// DirScanResultBatchDeleteHandler 批量删除目录扫描结果
func DirScanResultBatchDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req DirScanResultBatchDeleteReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.OkJson(w, &DirScanResultBatchDeleteResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		if len(req.Ids) == 0 {
			httpx.OkJson(w, &DirScanResultBatchDeleteResp{Code: 400, Msg: "ID列表不能为空"})
			return
		}

		ctx := r.Context()
		resultModel := model.NewDirScanResultModel(svcCtx.MongoDB)

		deleted, err := resultModel.DeleteByIds(ctx, req.Ids)
		if err != nil {
			httpx.OkJson(w, &DirScanResultBatchDeleteResp{Code: 500, Msg: "删除失败: " + err.Error()})
			return
		}

		httpx.OkJson(w, &DirScanResultBatchDeleteResp{
			Code:    0,
			Msg:     "删除成功",
			Deleted: deleted,
		})
	}
}

// DirScanResultClearReq 清空请求（不需要参数，从 context 获取 workspaceId）
type DirScanResultClearReq struct{}

// DirScanResultClearResp 清空响应
type DirScanResultClearResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Deleted int64  `json:"deleted"`
}

// DirScanResultClearHandler 清空目录扫描结果
func DirScanResultClearHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		workspaceId := middleware.GetWorkspaceId(ctx)
		resultModel := model.NewDirScanResultModel(svcCtx.MongoDB)

		deleted, err := resultModel.DeleteByWorkspace(ctx, workspaceId)
		if err != nil {
			httpx.OkJson(w, &DirScanResultClearResp{Code: 500, Msg: "清空失败: " + err.Error()})
			return
		}

		httpx.OkJson(w, &DirScanResultClearResp{
			Code:    0,
			Msg:     "清空成功",
			Deleted: deleted,
		})
	}
}
