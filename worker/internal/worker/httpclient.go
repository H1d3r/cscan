package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"cscan/internal/model"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

// WorkerHTTPClient Worker HTTP 客户端
type WorkerHTTPClient struct {
	baseURL      string
	installKey   string
	httpClient   *http.Client
	workerName   string
	instanceID   string
	taskProtocol int
}

// NewWorkerHTTPClient retains constructor compatibility while creating a v1
// process identity. Worker construction should use NewWorkerHTTPClientForInstance
// so HTTP and direct Redis transports share one identity.
func NewWorkerHTTPClient(baseURL, installKey, workerName string) *WorkerHTTPClient {
	return NewWorkerHTTPClientForInstance(baseURL, installKey, workerName, uuid.NewString(), scheduler.TaskProtocolV1)
}

func NewWorkerHTTPClientForInstance(baseURL, installKey, workerName, instanceID string, taskProtocol int) *WorkerHTTPClient {
	return &WorkerHTTPClient{
		baseURL:      baseURL,
		installKey:   installKey,
		workerName:   workerName,
		instanceID:   instanceID,
		taskProtocol: taskProtocol,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// ==================== Request/Response Types ====================

// TaskCheckReq 任务拉取请求
type TaskCheckReq struct {
	WorkerName   string `json:"workerName"`
	InstanceID   string `json:"instanceId"`
	TaskProtocol int    `json:"taskProtocol"`
}

// TaskCheckResp 任务拉取响应
type TaskCheckResp struct {
	Code               int    `json:"code"`
	Msg                string `json:"msg"`
	IsExist            bool   `json:"isExist"`
	IsFinished         bool   `json:"isFinished"`
	TaskId             string `json:"taskId"`
	MainTaskId         string `json:"mainTaskId"`
	Config             string `json:"config"`
	LeaseToken         string `json:"leaseToken"`
	DispatchGeneration string `json:"dispatchGeneration"`
}

// TaskUpdateReq 任务状态更新请求
type TaskUpdateReq struct {
	TaskId     string `json:"taskId"`
	MainTaskId string `json:"mainTaskId,omitempty"`
	LeaseToken string `json:"leaseToken,omitempty"`
	State      string `json:"state"`
	Worker     string `json:"worker"`
	Result     string `json:"result"`
	Progress   int    `json:"progress"`
	Phase      string `json:"phase"`
	TaskState  string `json:"taskState,omitempty"`
}

// TaskUpdateResp 任务状态更新响应
type TaskUpdateResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// IPV4Info IPv4信息
type IPV4Info struct {
	IP       string `json:"ip"`
	IPInt    uint32 `json:"ipInt"`
	Location string `json:"location"`
}

// IPV6Info IPv6信息
type IPV6Info struct {
	IP       string `json:"ip"`
	Location string `json:"location"`
}

// AssetDocument 资产文档
type AssetDocument struct {
	Authority                    string                      `json:"authority"`
	Host                         string                      `json:"host"`
	Port                         int32                       `json:"port"`
	Category                     string                      `json:"category"`
	Service                      string                      `json:"service"`
	Server                       string                      `json:"server"`
	Banner                       string                      `json:"banner"`
	Title                        string                      `json:"title"`
	App                          []string                    `json:"app"`
	FingerprintFindings          scanner.FingerprintFindings `json:"fingerprintFindings,omitempty"`
	FingerprintFindingsCollected bool                        `json:"fingerprintFindingsCollected,omitempty"`
	HttpStatus                   string                      `json:"httpStatus"`
	HttpHeader                   string                      `json:"httpHeader"`
	HttpBody                     string                      `json:"httpBody"`
	Cert                         string                      `json:"cert"`
	IconHash                     string                      `json:"iconHash"`
	IsCdn                        bool                        `json:"isCdn"`
	Cname                        string                      `json:"cname"`
	IsCloud                      bool                        `json:"isCloud"`
	Ipv4                         []IPV4Info                  `json:"ipv4"`
	Ipv6                         []IPV6Info                  `json:"ipv6"`
	Screenshot                   string                      `json:"screenshot"`
	IsHttp                       bool                        `json:"isHttp"`
	Source                       string                      `json:"source"`
	IconData                     []byte                      `json:"iconData"`
}

// TaskResultReq 资产结果上报请求
type TaskResultReq struct {
	MainTaskId string          `json:"mainTaskId"`
	OrgId      string          `json:"orgId"`
	Assets     []AssetDocument `json:"assets"`
}

// TaskResultResp 资产结果上报响应
type TaskResultResp struct {
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	Success     bool   `json:"success"`
	TotalAsset  int32  `json:"totalAsset"`
	NewAsset    int32  `json:"newAsset"`
	UpdateAsset int32  `json:"updateAsset"`
}

// VulDocument 漏洞文档
type VulDocument struct {
	Authority         string   `json:"authority"`
	Host              string   `json:"host"`
	Port              int32    `json:"port"`
	Url               string   `json:"url"`
	PocFile           string   `json:"pocFile"`
	Source            string   `json:"source"`
	RiskSource        string   `json:"riskSource,omitempty"`
	Severity          string   `json:"severity"`
	Extra             string   `json:"extra"`
	Result            string   `json:"result"`
	TaskId            string   `json:"taskId"`
	VulName           *string  `json:"vulName,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	CvssScore         *float64 `json:"cvssScore,omitempty"`
	CveId             *string  `json:"cveId,omitempty"`
	CweId             *string  `json:"cweId,omitempty"`
	Remediation       *string  `json:"remediation,omitempty"`
	References        []string `json:"references,omitempty"`
	MatcherName       *string  `json:"matcherName,omitempty"`
	ExtractedResults  []string `json:"extractedResults,omitempty"`
	CurlCommand       *string  `json:"curlCommand,omitempty"`
	Request           *string  `json:"request,omitempty"`
	Response          *string  `json:"response,omitempty"`
	ResponseTruncated *bool    `json:"responseTruncated,omitempty"`
}

// VulResultReq 漏洞结果上报请求
type VulResultReq struct {
	MainTaskId string        `json:"mainTaskId"`
	Vuls       []VulDocument `json:"vuls"`
}

// VulReverifyReq 漏洞复验结果上报请求
type VulReverifyReq struct {
	VulnId     string `json:"vulnId"`
	Conclusion string `json:"conclusion"`
	Reviewer   string `json:"reviewer"`
	Message    string `json:"message"`
	ReverifyAt string `json:"reverifyAt"`
}

// VulReverifyResp 漏洞复验结果上报响应
type VulReverifyResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// HeartbeatReq 心跳请求
type HeartbeatReq struct {
	WorkerName         string  `json:"workerName"`
	InstanceID         string  `json:"instanceId"`
	TaskProtocol       int     `json:"taskProtocol"`
	IP                 string  `json:"ip"`
	CpuLoad            float64 `json:"cpuLoad"`
	MemUsed            float64 `json:"memUsed"`
	TaskStartedNumber  int32   `json:"taskStartedNumber"`
	TaskExecutedNumber int32   `json:"taskExecutedNumber"`
	IsDaemon           bool    `json:"isDaemon"`
	Concurrency        int     `json:"concurrency"`
}

// HeartbeatResp 心跳响应
type HeartbeatResp struct {
	Code               int    `json:"code"`
	Msg                string `json:"msg"`
	Status             string `json:"status"`
	ManualStopFlag     bool   `json:"manualStopFlag"`
	ManualReloadFlag   bool   `json:"manualReloadFlag"`
	ManualInitEnvFlag  bool   `json:"manualInitEnvFlag"`
	ManualSyncFlag     bool   `json:"manualSyncFlag"`
	DesiredConcurrency int    `json:"desiredConcurrency,omitempty"`
}

// SubTaskDoneReq 子任务完成请求
type SubTaskDoneReq struct {
	TaskId      string                  `json:"taskId"`
	MainTaskId  string                  `json:"mainTaskId"`
	LeaseToken  string                  `json:"leaseToken"`
	Phase       string                  `json:"phase"`
	IsCompleted bool                    `json:"isCompleted"`
	IncrAmount  int                     `json:"incrAmount"`
	PhaseResult *model.TaskPhaseSummary `json:"phaseResult,omitempty"`
	TaskSummary *model.TaskScanSummary  `json:"taskSummary,omitempty"`
}

// SubTaskDoneResp 子任务完成响应
type SubTaskDoneResp struct {
	Code                int                    `json:"code"`
	Msg                 string                 `json:"msg"`
	Success             bool                   `json:"success"`
	SubTaskDone         int32                  `json:"subTaskDone"`
	SubTaskCount        int32                  `json:"subTaskCount"`
	AllDone             bool                   `json:"allDone"`
	Recorded            bool                   `json:"recorded,omitempty"`
	LeaseClosed         bool                   `json:"leaseClosed,omitempty"`
	Finalized           bool                   `json:"finalized,omitempty"`
	FinalizationPending bool                   `json:"finalizationPending,omitempty"`
	ScanSummary         *model.TaskScanSummary `json:"scanSummary,omitempty"`
}

// ==================== HTTP Client Methods ====================

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries     int           // 最大重试次数
	InitialBackoff time.Duration // 初始退避时间
	MaxBackoff     time.Duration // 最大退避时间
	BackoffFactor  float64       // 退避因子
}

// DefaultRetryConfig 默认重试配置
var DefaultRetryConfig = RetryConfig{
	MaxRetries:     3,
	InitialBackoff: 1 * time.Second,
	MaxBackoff:     30 * time.Second,
	BackoffFactor:  2.0,
}

// isRetryableError 判断是否为可重试的错误
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// 连接错误、超时错误可重试
	errStr := err.Error()
	return contains(errStr, "connection refused") ||
		contains(errStr, "connection reset") ||
		contains(errStr, "timeout") ||
		contains(errStr, "no such host") ||
		contains(errStr, "network is unreachable") ||
		contains(errStr, "i/o timeout") ||
		contains(errStr, "deadline exceeded") ||
		contains(errStr, "EOF")
}

// contains 检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsLower(s, substr)))
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if matchLower(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func matchLower(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// doRequest 执行HTTP请求（内部方法，带重试逻辑）
func (c *WorkerHTTPClient) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	return c.doRequestWithRetry(ctx, method, path, body, DefaultRetryConfig)
}

// doRequestWithRetry 执行HTTP请求（带自定义重试配置）
func (c *WorkerHTTPClient) doRequestWithRetry(ctx context.Context, method, path string, body interface{}, retryConfig RetryConfig) ([]byte, error) {
	var lastErr error
	backoff := retryConfig.InitialBackoff

	for attempt := 0; attempt <= retryConfig.MaxRetries; attempt++ {
		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// 如果不是第一次尝试，等待退避时间
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			// 计算下一次退避时间
			backoff = time.Duration(float64(backoff) * retryConfig.BackoffFactor)
			if backoff > retryConfig.MaxBackoff {
				backoff = retryConfig.MaxBackoff
			}
		}

		respBody, err := c.doRequestOnce(ctx, method, path, body)
		if err == nil {
			return respBody, nil
		}

		lastErr = err

		// 认证失败不重试
		if err.Error() == "authentication failed: invalid install key" {
			return nil, err
		}

		// 判断是否可重试
		if !isRetryableError(err) {
			return nil, err
		}

		// 记录重试日志
		if attempt < retryConfig.MaxRetries {
			logx.Infof("[HTTPClient] Request failed (attempt %d/%d): %v, retrying in %v...", attempt+1, retryConfig.MaxRetries+1, err, backoff)
		}
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", retryConfig.MaxRetries+1, lastErr)
}

// doRequestOnce 执行单次HTTP请求
func (c *WorkerHTTPClient) doRequestOnce(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body failed: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Worker-Key", c.installKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed: invalid install key")
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// CheckTask 任务拉取
func (c *WorkerHTTPClient) CheckTask(ctx context.Context) (*TaskCheckResp, error) {
	req := &TaskCheckReq{
		WorkerName:   c.workerName,
		InstanceID:   c.instanceID,
		TaskProtocol: c.taskProtocol,
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, "/api/v1/worker/task/check", req)
	if err != nil {
		return nil, err
	}

	var resp TaskCheckResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}
	if resp.Code != 0 {
		return &resp, fmt.Errorf("task check rejected: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return &resp, nil
}

// UpdateTask 任务状态更新
func (c *WorkerHTTPClient) UpdateTask(ctx context.Context, req *TaskUpdateReq) (*TaskUpdateResp, error) {
	respBody, err := c.doRequest(ctx, http.MethodPost, "/api/v1/worker/task/update", req)
	if err != nil {
		return nil, err
	}

	var resp TaskUpdateResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}

	if resp.Code != 0 || !resp.Success {
		if resp.Code == http.StatusLocked {
			return &resp, scheduler.ErrTaskParentFenced
		}
		if resp.Code == http.StatusConflict {
			return &resp, scheduler.ErrTaskLeaseConflict
		}
		if resp.Code == http.StatusTooEarly {
			return &resp, scheduler.ErrTaskOperationBusy
		}
		return &resp, fmt.Errorf("task update rejected: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return &resp, nil
}

// RenewTaskLease reuses the task-update endpoint with only taskId and the
// current lease token populated.
func (c *WorkerHTTPClient) RenewTaskLease(ctx context.Context, taskID, leaseToken string) error {
	_, err := c.UpdateTask(ctx, &TaskUpdateReq{TaskId: taskID, LeaseToken: leaseToken})
	return err
}

func (c *WorkerHTTPClient) SaveVulReverify(ctx context.Context, req *VulReverifyReq) (*VulReverifyResp, error) {
	respBody, err := c.doRequest(ctx, http.MethodPost, "/api/v1/worker/task/vul/reverify", req)
	if err != nil {
		return nil, err
	}

	var resp VulReverifyResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}

	return &resp, nil
}

// Heartbeat 心跳
func (c *WorkerHTTPClient) Heartbeat(ctx context.Context, req *HeartbeatReq) (*HeartbeatResp, error) {
	if req == nil {
		return nil, fmt.Errorf("heartbeat request is required")
	}
	req.WorkerName = c.workerName
	req.InstanceID = c.instanceID
	req.TaskProtocol = c.taskProtocol
	// 心跳不做内部重试（调用方 sendHeartbeatWithRetry 已有重试逻辑）
	respBody, err := c.doRequestOnce(ctx, http.MethodPost, "/api/v1/worker/heartbeat", req)
	if err != nil {
		return nil, err
	}

	var resp HeartbeatResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}
	if resp.Code != 0 {
		return &resp, fmt.Errorf("heartbeat rejected: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return &resp, nil
}

// IncrSubTaskDone 递增子任务完成数
func (c *WorkerHTTPClient) IncrSubTaskDone(ctx context.Context, req *SubTaskDoneReq) (*SubTaskDoneResp, error) {
	respBody, err := c.doRequest(ctx, http.MethodPost, "/api/v1/worker/task/subtask/done", req)
	if err != nil {
		return nil, err
	}

	var resp SubTaskDoneResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}

	if resp.Code != 0 || !resp.Success {
		if resp.Code == http.StatusLocked {
			return &resp, scheduler.ErrTaskParentFenced
		}
		if resp.Code == http.StatusConflict {
			return &resp, scheduler.ErrTaskLeaseConflict
		}
		if resp.Code == http.StatusTooEarly {
			return &resp, scheduler.ErrTaskOperationBusy
		}
		return &resp, fmt.Errorf("subtask report rejected: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return &resp, nil
}

// WorkerOfflineReq Worker离线通知请求
type WorkerOfflineReq struct {
	WorkerName   string `json:"workerName"`
	InstanceID   string `json:"instanceId"`
	TaskProtocol int    `json:"taskProtocol"`
}

// WorkerOfflineResp Worker离线通知响应
type WorkerOfflineResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// NotifyOffline 通知服务器Worker即将离线
func (c *WorkerHTTPClient) NotifyOffline(ctx context.Context) (*WorkerOfflineResp, error) {
	req := &WorkerOfflineReq{
		WorkerName:   c.workerName,
		InstanceID:   c.instanceID,
		TaskProtocol: c.taskProtocol,
	}

	// 使用较短的超时，避免阻塞停止流程
	shortCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	respBody, err := c.doRequestOnce(shortCtx, http.MethodPost, "/api/v1/worker/offline", req)
	if err != nil {
		return nil, err
	}

	var resp WorkerOfflineResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}
	if resp.Code != 0 || !resp.Success {
		return &resp, fmt.Errorf("offline notification rejected: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return &resp, nil
}

// ==================== Task Control Polling ====================

// TaskControlReq polls controls for exact task generations.
type TaskControlReq struct {
	WorkerName string                        `json:"workerName"`
	Targets    []scheduler.TaskControlTarget `json:"targets"`
}

// TaskControlResp carries the strict Redis/WebSocket control envelope.
type TaskControlResp struct {
	Code    int                             `json:"code"`
	Msg     string                          `json:"msg"`
	Success bool                            `json:"success"`
	Signals []scheduler.TaskControlEnvelope `json:"signals"`
}

// GetTaskControlSignals 获取任务控制信号（HTTP轮询）
func (c *WorkerHTTPClient) GetTaskControlSignals(ctx context.Context, targets []scheduler.TaskControlTarget) (*TaskControlResp, error) {
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return nil, err
		}
	}
	req := &TaskControlReq{WorkerName: c.workerName, Targets: targets}

	// 任务控制轮询不做内部重试（调用方 controlPollingLoop 已有周期性重试）
	respBody, err := c.doRequestOnce(ctx, http.MethodPost, "/api/v1/worker/task/control", req)
	if err != nil {
		return nil, err
	}

	var resp TaskControlResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}
	if resp.Code != 0 || !resp.Success {
		return &resp, fmt.Errorf("task control polling rejected: code=%d msg=%s", resp.Code, resp.Msg)
	}
	for index := range resp.Signals {
		if err := resp.Signals[index].Validate(); err != nil {
			return nil, fmt.Errorf("invalid task control envelope: %w", err)
		}
	}
	return &resp, nil
}

// ==================== Dir Scan Result ====================

// DirScanResultDocument 目录扫描结果文档
type DirScanResultDocument struct {
	Authority     string `json:"authority"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	URL           string `json:"url"`
	Path          string `json:"path"`
	StatusCode    int    `json:"statusCode"`
	ContentLength int64  `json:"contentLength"`
	ContentType   string `json:"contentType"`
	Title         string `json:"title"`
	RedirectURL   string `json:"redirectUrl"`
	ContentWords  int64  `json:"contentWords"`
	ContentLines  int64  `json:"contentLines"`
	Duration      int64  `json:"duration"`
	Request       string `json:"request,omitempty"`
	Response      string `json:"response,omitempty"`
}

// JSFinderResultItem JSFinder 扫描结果项
type JSFinderResultItem struct {
	Authority        string   `json:"authority"`
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	URL              string   `json:"url"`
	Severity         string   `json:"severity"`
	VulName          string   `json:"vulName"`
	Result           string   `json:"result"`
	Tags             []string `json:"tags"`
	MatcherName      string   `json:"matcherName,omitempty"`
	ExtractedResults []string `json:"extractedResults,omitempty"`
	CurlCommand      string   `json:"curlCommand,omitempty"`
	Request          string   `json:"request,omitempty"`
	Response         string   `json:"response,omitempty"`
}

// SaveJSFinderResultReq 保存 JSFinder 扫描结果请求
type SaveJSFinderResultReq struct {
	MainTaskId string                `json:"mainTaskId,omitempty"`
	Results    []*JSFinderResultItem `json:"results"`
}

// BaseResp 通用响应
type BaseResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

// CertResultItem 证书采集结果项
type CertResultItem struct {
	Host         string               `json:"host"`
	Port         int                  `json:"port"`
	Authority    string               `json:"authority"`
	Subject      scanner.CertNameInfo `json:"subject"`
	SubjectDN    string               `json:"subjectDN"`
	Issuer       scanner.CertNameInfo `json:"issuer"`
	IssuerDN     string               `json:"issuerDN"`
	SerialNumber string               `json:"serialNumber"`
	SigAlg       string               `json:"sigAlg"`
	NotBefore    time.Time            `json:"notBefore"`
	NotAfter     time.Time            `json:"notAfter"`
	Version      int                  `json:"version"`
	SANs         []string             `json:"sans,omitempty"`
	Fingerprints map[string]string    `json:"fingerprints,omitempty"`
	IsSelfSigned bool                 `json:"isSelfSigned"`
}

// SaveCertResultReq 保存证书采集结果请求
type SaveCertResultReq struct {
	MainTaskId string            `json:"mainTaskId,omitempty"`
	Results    []*CertResultItem `json:"results"`
}

// SaveDirScanResultReq 保存目录扫描结果请求
type SaveDirScanResultReq struct {
	MainTaskId string                  `json:"mainTaskId,omitempty"`
	Results    []DirScanResultDocument `json:"results"`
}

// ==================== Task Recovery ====================

// TaskRecoveryReq 任务恢复请求
type TaskRecoveryReq struct {
	WorkerName   string `json:"workerName"`
	InstanceID   string `json:"instanceId"`
	TaskProtocol int    `json:"taskProtocol"`
}

// RecoveredTaskInfo 恢复的任务信息
type RecoveredTaskInfo struct {
	TaskId     string `json:"taskId"`
	MainTaskId string `json:"mainTaskId"`
	Status     string `json:"status"`
	StartTime  string `json:"startTime"`
}

// TaskRecoveryResp 任务恢复响应
type TaskRecoveryResp struct {
	Code           int                 `json:"code"`
	Msg            string              `json:"msg"`
	Success        bool                `json:"success"`
	RecoveredTasks []RecoveredTaskInfo `json:"recoveredTasks"`
	RecoveredCount int                 `json:"recoveredCount"`
}

// RecoverTasks Worker 启动时恢复未完成的任务
func (c *WorkerHTTPClient) RecoverTasks(ctx context.Context) (*TaskRecoveryResp, error) {
	req := &TaskRecoveryReq{
		WorkerName:   c.workerName,
		InstanceID:   c.instanceID,
		TaskProtocol: c.taskProtocol,
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, "/api/v1/worker/task/recovery", req)
	if err != nil {
		return nil, err
	}

	var resp TaskRecoveryResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}
	if resp.Code != 0 || !resp.Success {
		return &resp, fmt.Errorf("task recovery rejected: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return &resp, nil
}
