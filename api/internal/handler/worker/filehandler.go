package worker

import (
	"encoding/json"
	"net/http"
	"time"

	"cscan/api/internal/svc"
	"cscan/model"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// FileListReq 文件列表请求
type FileListReq struct {
	Path string `form:"path"`
}

// FileListResp 文件列表响应
type FileListResp struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    *FileListData `json:"data,omitempty"`
}

type FileListData struct {
	Path  string     `json:"path"`
	Files []FileInfo `json:"files"`
}

// FileUploadReq 文件上传请求
type FileUploadReq struct {
	Path string `json:"path"`
	Data string `json:"data"` // Base64编码的文件内容
}

// FileUploadResp 文件上传响应
type FileUploadResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// FileDownloadReq 文件下载请求
type FileDownloadReq struct {
	Path string `form:"path"`
}

// FileDownloadResp 文件下载响应
type FileDownloadResp struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    *FileDownloadData `json:"data,omitempty"`
}

type FileDownloadData struct {
	Path string `json:"path"`
	Data string `json:"data"` // Base64编码的文件内容
	Size int64  `json:"size"`
}

// FileDeleteReq 文件删除请求
type FileDeleteReq struct {
	Path string `form:"path"`
}

// FileDeleteResp 文件删除响应
type FileDeleteResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// FileMkdirReq 创建目录请求
type FileMkdirReq struct {
	Path string `json:"path"`
}

// FileMkdirResp 创建目录响应
type FileMkdirResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const fileOperationTimeout = 30 * time.Second

// WorkerFileListHandler 文件列表处理器
// GET /api/v1/worker/console/:name/files
func WorkerFileListHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// 获取Worker名称
		workerName := r.URL.Query().Get("name")
		if workerName == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileListResp{
				Code:    400,
				Message: "worker name is required",
			})
			return
		}

		// 获取路径参数
		path := r.URL.Query().Get("path")

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(workerName)
		if !ok {
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileList, workerName, path, false, "worker not connected", time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusNotFound, &FileListResp{
				Code:    404,
				Message: "worker not connected",
			})
			return
		}

		// 请求文件列表
		resp, err := conn.RequestFileList(path, fileOperationTimeout)
		if err != nil {
			logx.Errorf("[FileHandler] RequestFileList error: %v", err)
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileList, workerName, path, false, err.Error(), time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusInternalServerError, &FileListResp{
				Code:    500,
				Message: "request failed: " + err.Error(),
			})
			return
		}

		// 检查响应错误
		if resp.Error != "" {
			// 记录审计日志
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileList, workerName, path, false, resp.Error, time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusOK, &FileListResp{
				Code:    400,
				Message: resp.Error,
			})
			return
		}

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileList, workerName, path, true, "", time.Since(startTime))
		}

		httpx.WriteJson(w, http.StatusOK, &FileListResp{
			Code:    0,
			Message: "success",
			Data: &FileListData{
				Path:  resp.Path,
				Files: resp.Files,
			},
		})
	}
}

// WorkerFileUploadHandler 文件上传处理器
// POST /api/v1/worker/console/:name/files/upload
func WorkerFileUploadHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// 获取Worker名称
		workerName := r.URL.Query().Get("name")
		if workerName == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileUploadResp{
				Code:    400,
				Message: "worker name is required",
			})
			return
		}

		// 解析请求体
		var req FileUploadReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteJson(w, http.StatusBadRequest, &FileUploadResp{
				Code:    400,
				Message: "invalid request body",
			})
			return
		}

		if req.Path == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileUploadResp{
				Code:    400,
				Message: "path is required",
			})
			return
		}

		// 检查文件大小限制
		maxSize := svcCtx.Config.Console.GetMaxUploadSize()
		if int64(len(req.Data)) > maxSize {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileUpload, workerName, req.Path, false, "file too large", time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusBadRequest, &FileUploadResp{
				Code:    413,
				Message: "file too large",
			})
			return
		}

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(workerName)
		if !ok {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileUpload, workerName, req.Path, false, "worker not connected", time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusNotFound, &FileUploadResp{
				Code:    404,
				Message: "worker not connected",
			})
			return
		}

		// 请求文件上传
		resp, err := conn.RequestFileUpload(req.Path, req.Data, fileOperationTimeout)
		if err != nil {
			logx.Errorf("[FileHandler] RequestFileUpload error: %v", err)
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileUpload, workerName, req.Path, false, err.Error(), time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusInternalServerError, &FileUploadResp{
				Code:    500,
				Message: "request failed: " + err.Error(),
			})
			return
		}

		// 检查响应错误
		if resp.Error != "" {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileUpload, workerName, req.Path, false, resp.Error, time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusOK, &FileUploadResp{
				Code:    400,
				Message: resp.Error,
			})
			return
		}

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileUpload, workerName, req.Path, true, "", time.Since(startTime))
		}

		httpx.WriteJson(w, http.StatusOK, &FileUploadResp{
			Code:    0,
			Message: "success",
		})
	}
}

// WorkerFileDownloadHandler 文件下载处理器
// GET /api/v1/worker/console/:name/files/download
func WorkerFileDownloadHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// 获取Worker名称
		workerName := r.URL.Query().Get("name")
		if workerName == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileDownloadResp{
				Code:    400,
				Message: "worker name is required",
			})
			return
		}

		// 获取路径参数
		path := r.URL.Query().Get("path")
		if path == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileDownloadResp{
				Code:    400,
				Message: "path is required",
			})
			return
		}

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(workerName)
		if !ok {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDownload, workerName, path, false, "worker not connected", time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusNotFound, &FileDownloadResp{
				Code:    404,
				Message: "worker not connected",
			})
			return
		}

		// 请求文件下载
		resp, err := conn.RequestFileDownload(path, fileOperationTimeout)
		if err != nil {
			logx.Errorf("[FileHandler] RequestFileDownload error: %v", err)
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDownload, workerName, path, false, err.Error(), time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusInternalServerError, &FileDownloadResp{
				Code:    500,
				Message: "request failed: " + err.Error(),
			})
			return
		}

		// 检查响应错误
		if resp.Error != "" {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDownload, workerName, path, false, resp.Error, time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusOK, &FileDownloadResp{
				Code:    400,
				Message: resp.Error,
			})
			return
		}

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDownload, workerName, path, true, "", time.Since(startTime))
		}

		httpx.WriteJson(w, http.StatusOK, &FileDownloadResp{
			Code:    0,
			Message: "success",
			Data: &FileDownloadData{
				Path: resp.Path,
				Data: resp.Data,
				Size: resp.Size,
			},
		})
	}
}

// WorkerFileDeleteHandler 文件删除处理器
// DELETE /api/v1/worker/console/:name/files
func WorkerFileDeleteHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// 获取Worker名称
		workerName := r.URL.Query().Get("name")
		if workerName == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileDeleteResp{
				Code:    400,
				Message: "worker name is required",
			})
			return
		}

		// 获取路径参数
		path := r.URL.Query().Get("path")
		if path == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileDeleteResp{
				Code:    400,
				Message: "path is required",
			})
			return
		}

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(workerName)
		if !ok {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDelete, workerName, path, false, "worker not connected", time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusNotFound, &FileDeleteResp{
				Code:    404,
				Message: "worker not connected",
			})
			return
		}

		// 请求文件删除
		resp, err := conn.RequestFileDelete(path, fileOperationTimeout)
		if err != nil {
			logx.Errorf("[FileHandler] RequestFileDelete error: %v", err)
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDelete, workerName, path, false, err.Error(), time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusInternalServerError, &FileDeleteResp{
				Code:    500,
				Message: "request failed: " + err.Error(),
			})
			return
		}

		// 检查响应错误
		if resp.Error != "" {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDelete, workerName, path, false, resp.Error, time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusOK, &FileDeleteResp{
				Code:    400,
				Message: resp.Error,
			})
			return
		}

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileDelete, workerName, path, true, "", time.Since(startTime))
		}

		httpx.WriteJson(w, http.StatusOK, &FileDeleteResp{
			Code:    0,
			Message: "success",
		})
	}
}

// WorkerFileMkdirHandler 创建目录处理器
// POST /api/v1/worker/console/:name/files/mkdir
func WorkerFileMkdirHandler(svcCtx *svc.ServiceContext, wsHandler *WorkerWSHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// 获取Worker名称
		workerName := r.URL.Query().Get("name")
		if workerName == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileMkdirResp{
				Code:    400,
				Message: "worker name is required",
			})
			return
		}

		// 解析请求体
		var req FileMkdirReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteJson(w, http.StatusBadRequest, &FileMkdirResp{
				Code:    400,
				Message: "invalid request body",
			})
			return
		}

		if req.Path == "" {
			httpx.WriteJson(w, http.StatusBadRequest, &FileMkdirResp{
				Code:    400,
				Message: "path is required",
			})
			return
		}

		// 获取Worker连接
		conn, ok := wsHandler.GetConnection(workerName)
		if !ok {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileMkdir, workerName, req.Path, false, "worker not connected", time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusNotFound, &FileMkdirResp{
				Code:    404,
				Message: "worker not connected",
			})
			return
		}

		// 请求创建目录
		resp, err := conn.RequestFileMkdir(req.Path, fileOperationTimeout)
		if err != nil {
			logx.Errorf("[FileHandler] RequestFileMkdir error: %v", err)
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileMkdir, workerName, req.Path, false, err.Error(), time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusInternalServerError, &FileMkdirResp{
				Code:    500,
				Message: "request failed: " + err.Error(),
			})
			return
		}

		// 检查响应错误
		if resp.Error != "" {
			if auditSvc := GetAuditService(); auditSvc != nil {
				auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileMkdir, workerName, req.Path, false, resp.Error, time.Since(startTime))
			}
			httpx.WriteJson(w, http.StatusOK, &FileMkdirResp{
				Code:    400,
				Message: resp.Error,
			})
			return
		}

		// 记录审计日志
		if auditSvc := GetAuditService(); auditSvc != nil {
			auditSvc.RecordFileOperation(r.Context(), r, model.AuditLogTypeFileMkdir, workerName, req.Path, true, "", time.Since(startTime))
		}

		httpx.WriteJson(w, http.StatusOK, &FileMkdirResp{
			Code:    0,
			Message: "success",
		})
	}
}
