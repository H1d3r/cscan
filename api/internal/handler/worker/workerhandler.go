package worker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/pkg/response"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// WorkerListHandler Worker列表
func WorkerListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewWorkerListLogic(r.Context(), svcCtx)
		resp, err := l.WorkerList()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WorkerRenameHandler Worker重命名
func WorkerRenameHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WorkerRenameReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &types.WorkerRenameResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		l := logic.NewWorkerRenameLogic(r.Context(), svcCtx)
		resp, err := l.WorkerRename(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WorkerRestartHandler Worker重启
func WorkerRestartHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WorkerRestartReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &types.WorkerRestartResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		l := logic.NewWorkerRestartLogic(r.Context(), svcCtx)
		resp, err := l.WorkerRestart(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WorkerSetConcurrencyHandler Worker设置并发数
func WorkerSetConcurrencyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WorkerSetConcurrencyReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &types.WorkerSetConcurrencyResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		l := logic.NewWorkerSetConcurrencyLogic(r.Context(), svcCtx)
		resp, err := l.WorkerSetConcurrency(&req)
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}

// WorkerLogsClearHandler 清空历史日志
func WorkerLogsClearHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svcCtx.WorkerLogReader == nil {
			response.Error(w, xerr.NewServerError("worker log reader not initialized"))
			return
		}
		if err := svcCtx.WorkerLogReader.Clear(); err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, &types.BaseResp{Code: 0, Msg: "日志已清空"})
	}
}

// WorkerLogsHistoryHandler 获取 Worker 历史日志（从文件读取）
func WorkerLogsHistoryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Limit        int    `json:"limit"`        // 返回条数，默认500
			Worker       string `json:"worker"`       // 指定 Worker 名称
			Level        string `json:"level"`        // 过滤日志级别
			Search       string `json:"search"`       // 模糊搜索关键词
			Date         string `json:"date"`         // 指定日期 YYYY-MM-DD，空则取最新
			IncludeDebug bool   `json:"includeDebug"` // 是否包含 DEBUG 级别日志，默认 false
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Limit <= 0 {
			req.Limit = 500
		}
		if req.Limit > 10000 {
			req.Limit = 10000
		}

		// 从 MongoDB 读取日志
		var entries []svc.WorkerLogEntry
		var err error
		if req.Worker != "" {
			entries, err = svcCtx.WorkerLogReader.ReadTail(req.Worker, req.Date, req.Limit)
		} else {
			// 未指定 Worker，读取所有 Worker 的日志合并
			if req.Date == "" {
				req.Date, _ = svcCtx.WorkerLogReader.FindLatestDate()
			}
			workers, _ := svcCtx.WorkerLogReader.ListWorkers(req.Date)
			for _, wn := range workers {
				e, _ := svcCtx.WorkerLogReader.ReadTail(wn, req.Date, req.Limit*5)
				entries = append(entries, e...)
			}
		}

		if err != nil {
			response.Error(w, err)
			return
		}

		// 过滤
		searchLower := strings.ToLower(req.Search)
		levelUpper := strings.ToUpper(req.Level)
		result := make([]svc.WorkerLogEntry, 0, req.Limit)
		for _, e := range entries {
			// DEBUG 级别默认过滤（可通过 IncludeDebug 开启，或显式指定 Level=DEBUG）
			if !req.IncludeDebug && levelUpper != "DEBUG" && strings.EqualFold(e.Level, "DEBUG") {
				continue
			}
			if req.Level != "" && strings.ToUpper(e.Level) != levelUpper {
				continue
			}
			if req.Search != "" {
				if !strings.Contains(strings.ToLower(e.Msg), searchLower) &&
					!strings.Contains(strings.ToLower(e.Worker), searchLower) &&
					!strings.Contains(strings.ToLower(e.Level), searchLower) {
					continue
				}
			}
			result = append(result, e)
		}

		// 返回最后 N 条
		if len(result) > req.Limit {
			result = result[len(result)-req.Limit:]
		}

		httpx.OkJson(w, map[string]interface{}{
			"code":  0,
			"list":  result,
			"total": len(result),
		})
	}
}

// WorkerLogsExportHandler 导出日志（从文件读取）
func WorkerLogsExportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Format       string `json:"format"`       // json, txt, csv
			Search       string `json:"search"`       // 模糊搜索关键词
			Worker       string `json:"worker"`       // 过滤指定 Worker
			Level        string `json:"level"`        // 过滤日志级别
			Date         string `json:"date"`         // 指定日期
			IncludeDebug bool   `json:"includeDebug"` // 是否包含 DEBUG 级别日志，默认 false
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Format == "" {
			req.Format = "json"
		}

		const maxExportCount = 10000
		var entries []svc.WorkerLogEntry
		if req.Worker != "" {
			entries, _ = svcCtx.WorkerLogReader.ReadTail(req.Worker, req.Date, maxExportCount)
		} else {
			if req.Date == "" {
				req.Date, _ = svcCtx.WorkerLogReader.FindLatestDate()
			}
			workers, _ := svcCtx.WorkerLogReader.ListWorkers(req.Date)
			for _, wn := range workers {
				e, _ := svcCtx.WorkerLogReader.ReadTail(wn, req.Date, maxExportCount)
				entries = append(entries, e...)
			}
		}

		searchLower := strings.ToLower(req.Search)
		levelUpper := strings.ToUpper(req.Level)
		result := make([]svc.WorkerLogEntry, 0)
		for _, e := range entries {
			// DEBUG 级别默认过滤（可通过 IncludeDebug 开启，或显式指定 Level=DEBUG）
			if !req.IncludeDebug && levelUpper != "DEBUG" && strings.EqualFold(e.Level, "DEBUG") {
				continue
			}
			if req.Level != "" && strings.ToUpper(e.Level) != levelUpper {
				continue
			}
			if req.Worker != "" && e.Worker != req.Worker {
				continue
			}
			if req.Search != "" {
				if !strings.Contains(strings.ToLower(e.Msg), searchLower) &&
					!strings.Contains(strings.ToLower(e.Worker), searchLower) {
					continue
				}
			}
			result = append(result, e)
		}

		filename := fmt.Sprintf("worker-logs-%s", time.Now().Format("20060102-150405"))

		switch req.Format {
		case "txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.txt", filename))
			for _, log := range result {
				fmt.Fprintf(w, "%s [%s] [%s] %s\n", log.Ts, log.Level, log.Worker, log.Msg)
			}
		case "csv":
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
			w.Write([]byte{0xEF, 0xBB, 0xBF})
			fmt.Fprintln(w, "Timestamp,Level,Worker,Message")
			for _, log := range result {
				msg := strings.ReplaceAll(log.Msg, "\"", "\"\"")
				fmt.Fprintf(w, "\"%s\",\"%s\",\"%s\",\"%s\"\n", log.Ts, log.Level, log.Worker, msg)
			}
		default: // json
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.json", filename))
			json.NewEncoder(w).Encode(map[string]interface{}{
				"exportTime": time.Now().Format("2006-01-02 15:04:05"),
				"total":      len(result),
				"logs":       result,
			})
		}
	}
}
