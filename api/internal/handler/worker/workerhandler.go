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

// WorkerDeleteHandler Worker删除
func WorkerDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WorkerDeleteReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.OkJson(w, &types.WorkerDeleteResp{Code: 400, Msg: "参数解析失败"})
			return
		}

		l := logic.NewWorkerDeleteLogic(r.Context(), svcCtx)
		resp, err := l.WorkerDelete(&req)
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

// WorkerLogsHandler SSE实时日志推送
func WorkerLogsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 设置SSE响应头
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// 发送连接成功消息
		fmt.Fprintf(w, "data: {\"level\":\"INFO\",\"message\":\"日志流连接成功，等待Worker日志...\",\"timestamp\":\"%s\",\"workerName\":\"API\"}\n\n",
			time.Now().Local().Format("2006-01-02 15:04:05"))
		flusher.Flush()

		// 先发送最近的历史日志
		logs, err := svcCtx.RedisClient.XRevRange(r.Context(), "cscan:worker:logs", "+", "-").Result()
		if err == nil && len(logs) > 0 {
			count := 100
			if len(logs) < count {
				count = len(logs)
			}
			for i := count - 1; i >= 0; i-- {
				if data, ok := logs[i].Values["data"].(string); ok {
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
			}
			flusher.Flush()
		}

		// 订阅Redis Pub/Sub
		pubsub := svcCtx.RedisClient.Subscribe(r.Context(), "cscan:worker:logs:realtime")
		defer pubsub.Close()

		ch := pubsub.Channel()

		// 实时推送新日志
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprintf(w, ": heartbeat\n\n")
				flusher.Flush()
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
				flusher.Flush()
			}
		}
	}
}

// WorkerLogsClearHandler 清空历史日志
func WorkerLogsClearHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := svcCtx.RedisClient.Del(r.Context(), "cscan:worker:logs").Err()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, &types.BaseResp{Code: 0, Msg: "日志已清空"})
	}
}

// WorkerLogsHistoryHandler 获取历史日志（支持分页懒加载）
func WorkerLogsHistoryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Limit    int    `json:"limit"`    // 每页数量，默认100
			LastId   string `json:"lastId"`   // 上一页最后一条日志的ID，用于分页
			Search   string `json:"search"`   // 模糊搜索关键词
			Worker   string `json:"worker"`   // 过滤指定 Worker
			Level    string `json:"level"`    // 过滤日志级别
			NewerThan string `json:"newerThan"` // 获取比此ID更新的日志（用于实时更新）
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Limit <= 0 {
			req.Limit = 100
		}
		if req.Limit > 500 {
			req.Limit = 500 // 单次最多500条
		}

		var logs []struct {
			ID     string
			Values map[string]interface{}
		}
		var err error

		// 获取日志总数
		totalCount, _ := svcCtx.RedisClient.XLen(r.Context(), "cscan:worker:logs").Result()

		if req.NewerThan != "" {
			// 获取比指定ID更新的日志（用于实时更新）
			rawLogs, e := svcCtx.RedisClient.XRange(r.Context(), "cscan:worker:logs", "("+req.NewerThan, "+").Result()
			err = e
			for _, l := range rawLogs {
				logs = append(logs, struct {
					ID     string
					Values map[string]interface{}
				}{ID: l.ID, Values: l.Values})
			}
		} else if req.LastId != "" {
			// 分页：获取比LastId更旧的日志
			rawLogs, e := svcCtx.RedisClient.XRevRange(r.Context(), "cscan:worker:logs", "("+req.LastId, "-").Result()
			err = e
			for _, l := range rawLogs {
				logs = append(logs, struct {
					ID     string
					Values map[string]interface{}
				}{ID: l.ID, Values: l.Values})
			}
		} else {
			// 首次加载：获取最新的日志
			rawLogs, e := svcCtx.RedisClient.XRevRange(r.Context(), "cscan:worker:logs", "+", "-").Result()
			err = e
			for _, l := range rawLogs {
				logs = append(logs, struct {
					ID     string
					Values map[string]interface{}
				}{ID: l.ID, Values: l.Values})
			}
		}

		if err != nil {
			response.Error(w, err)
			return
		}

		type LogWithId struct {
			Id         string `json:"id"`
			Timestamp  string `json:"timestamp"`
			Level      string `json:"level"`
			WorkerName string `json:"workerName"`
			Message    string `json:"message"`
		}

		result := make([]LogWithId, 0)
		searchLower := strings.ToLower(req.Search)
		workerLower := strings.ToLower(req.Worker)
		levelUpper := strings.ToUpper(req.Level)

		for i := 0; i < len(logs) && len(result) < req.Limit; i++ {
			if data, ok := logs[i].Values["data"].(string); ok {
				var logEntry LogWithId
				if json.Unmarshal([]byte(data), &logEntry) != nil {
					continue
				}
				logEntry.Id = logs[i].ID

				// Worker 过滤
				if req.Worker != "" && strings.ToLower(logEntry.WorkerName) != workerLower {
					continue
				}

				// Level 过滤
				if req.Level != "" && strings.ToUpper(logEntry.Level) != levelUpper {
					continue
				}

				// 搜索过滤
				if req.Search != "" {
					if !strings.Contains(strings.ToLower(logEntry.Message), searchLower) &&
						!strings.Contains(strings.ToLower(logEntry.Level), searchLower) &&
						!strings.Contains(strings.ToLower(logEntry.WorkerName), searchLower) {
						continue
					}
				}

				result = append(result, logEntry)
			}
		}

		// 如果不是获取更新的日志，需要反转结果（时间正序）
		if req.NewerThan == "" {
			for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
				result[i], result[j] = result[j], result[i]
			}
		}

		// 计算是否还有更多数据
		hasMore := len(logs) > len(result)

		httpx.OkJson(w, map[string]interface{}{
			"code":    0,
			"list":    result,
			"total":   totalCount,
			"hasMore": hasMore,
		})
	}
}

// WorkerLogsExportHandler 导出日志
func WorkerLogsExportHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Format string `json:"format"` // json, txt, csv
			Search string `json:"search"` // 模糊搜索关键词
			Worker string `json:"worker"` // 过滤指定 Worker
			Level  string `json:"level"`  // 过滤日志级别
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Format == "" {
			req.Format = "json"
		}

		logs, err := svcCtx.RedisClient.XRevRange(r.Context(), "cscan:worker:logs", "+", "-").Result()
		if err != nil {
			response.Error(w, err)
			return
		}

		type LogEntry struct {
			Timestamp  string `json:"timestamp"`
			Level      string `json:"level"`
			WorkerName string `json:"workerName"`
			Message    string `json:"message"`
		}

		result := make([]LogEntry, 0)
		searchLower := strings.ToLower(req.Search)
		workerLower := strings.ToLower(req.Worker)
		levelUpper := strings.ToUpper(req.Level)

		// 遍历所有日志，不限制数量
		for i := len(logs) - 1; i >= 0; i-- {
			if data, ok := logs[i].Values["data"].(string); ok {
				var logEntry LogEntry
				if json.Unmarshal([]byte(data), &logEntry) != nil {
					continue
				}

				// Worker 过滤
				if req.Worker != "" && strings.ToLower(logEntry.WorkerName) != workerLower {
					continue
				}

				// Level 过滤
				if req.Level != "" && strings.ToUpper(logEntry.Level) != levelUpper {
					continue
				}

				// 搜索过滤
				if req.Search != "" {
					if !strings.Contains(strings.ToLower(logEntry.Message), searchLower) &&
						!strings.Contains(strings.ToLower(logEntry.Level), searchLower) &&
						!strings.Contains(strings.ToLower(logEntry.WorkerName), searchLower) {
						continue
					}
				}

				result = append(result, logEntry)
			}
		}

		filename := fmt.Sprintf("worker-logs-%s", time.Now().Format("20060102-150405"))

		switch req.Format {
		case "txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.txt", filename))
			for _, log := range result {
				fmt.Fprintf(w, "%s [%s] [%s] %s\n", log.Timestamp, log.Level, log.WorkerName, log.Message)
			}
		case "csv":
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
			// 写入 BOM 以支持 Excel 正确识别 UTF-8
			w.Write([]byte{0xEF, 0xBB, 0xBF})
			fmt.Fprintln(w, "Timestamp,Level,Worker,Message")
			for _, log := range result {
				// CSV 转义：双引号需要转义为两个双引号
				msg := strings.ReplaceAll(log.Message, "\"", "\"\"")
				fmt.Fprintf(w, "\"%s\",\"%s\",\"%s\",\"%s\"\n", log.Timestamp, log.Level, log.WorkerName, msg)
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
