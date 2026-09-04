package scanner

import (
	"context"
)

// ScannerOptions 扫描器选项接口
// 所有扫描器的选项结构体都应该实现此接口
// 用于类型安全的配置验证
type ScannerOptions interface {
	// Validate 验证选项是否有效
	// 返回 nil 表示验证通过，否则返回描述性错误
	Validate() error
}

// Scanner 扫描器接口
type Scanner interface {
	// Name 扫描器名称
	Name() string
	// Scan 执行扫描
	Scan(ctx context.Context, config *ScanConfig) (*ScanResult, error)
}

// ScanEventLogger receives bounded structured execution facts in addition to the
// existing human-readable TaskLogger stream. Callers that do not need
// structured events may leave it nil.
type ScanEventLogger func(event, phase, outcome string, fields map[string]interface{})

const (
	EventNaabuParseComplete  = "naabu_parse_complete"
	EventNmapPortResult      = "nmap_port_result"
	EventSchemeProbeComplete = "scheme_probe_complete"
	EventHTTPXPhaseComplete  = "httpx_phase_complete"
	EventFingerprintDecision = "fingerprint_decision"
)

// ScanConfig 扫描配置
type ScanConfig struct {
	Target            string      `json:"target"`
	Targets           []string    `json:"targets"`
	Assets            []*Asset    `json:"assets"`
	Options           interface{} `json:"options"`
	MainTaskId        string      `json:"mainTaskId"`
	WorkerConcurrency int         `json:"-"` // Worker 自适应并发数，scanner 模块用作 Worker Pool 默认值
	// TaskLogger 任务日志回调，用于将扫描日志推送到任务日志流
	TaskLogger func(level, format string, args ...interface{}) `json:"-"`
	// EventLogger 可选结构化事件回调；字段在持久化边界再次白名单过滤。
	EventLogger ScanEventLogger `json:"-"`
	// OnProgress 进度回调，参数为当前进度(0-100)和描述
	OnProgress func(progress int, message string) `json:"-"`
	// OnAssetUpdated 局部资产完成更新时的回调事件（用于流式结果更新）
	OnAssetUpdated func(asset *Asset) `json:"-"`
	// OnTargetDone 单目标/单命令扫描完成后的回调，参数为已完成的 target 与本次产出资产
	OnTargetDone func(target string, assets []*Asset) `json:"-"`
	// OnCertFound 证书采集完成后的流式回调，用于即时入库
	OnCertFound func(cert *CertResult) `json:"-"`
}

// ScanResult 扫描结果
type ScanResult struct {
	MainTaskId          string               `json:"mainTaskId"`
	Assets              []*Asset             `json:"assets"`
	Vulnerabilities     []*Vulnerability     `json:"vulnerabilities"`
	JSFinderResults     []*JSFinderResult    `json:"jsfinderResults,omitempty"`
	CertResults         []*CertResult        `json:"certResults,omitempty"`         // 证书采集结果（T2.1 certcheck 扫描器产出，由 T2.2 落库）
	SkippedHosts        []string             `json:"skippedHosts,omitempty"`        // 因端口阈值超限被跳过的主机列表
	DNSFailedHosts      []string             `json:"dnsFailedHosts,omitempty"`      // DNS解析失败的主机列表
	Diagnostic          *ScanDiagnostic      `json:"diagnostic,omitempty"`          // 可选扫描执行诊断；旧调用方可忽略
	PortIdentifyResults []PortIdentifyResult `json:"portIdentifyResults,omitempty"` // Nmap逐host-port识别结果
}

// Asset 资产
type Asset struct {
	Authority           string              `json:"authority"`
	Host                string              `json:"host"`
	Port                int                 `json:"port"`
	Category            string              `json:"category"` // ipv4/ipv6/domain/url
	Service             string              `json:"service"`
	Server              string              `json:"server"`
	Banner              string              `json:"banner"`
	Title               string              `json:"title"`
	App                 []string            `json:"app"`
	FingerprintFindings FingerprintFindings `json:"fingerprintFindings,omitempty"`
	// FingerprintFindingsCollected distinguishes an intentional empty result from
	// a phase that did not obtain a usable response. It is transport-only.
	FingerprintFindingsCollected bool     `json:"-"`
	HttpStatus                   string   `json:"httpStatus"`
	HttpHeader                   string   `json:"httpHeader"`
	HttpBody                     string   `json:"httpBody"`
	Cert                         string   `json:"cert"`
	IconHash                     string   `json:"iconHash"`
	IconData                     []byte   `json:"iconData,omitempty"` // favicon 图片原始数据
	Screenshot                   string   `json:"screenshot"`
	IsCDN                        bool     `json:"isCdn"`
	CName                        string   `json:"cname"`
	IsCloud                      bool     `json:"isCloud"`
	IsHTTP                       bool     `json:"isHttp"`                        // 是否为HTTP服务
	ProtocolProbeStatus          string   `json:"protocolProbeStatus,omitempty"` // HTTP/HTTPS 协议探测结论
	IPV4                         []IPInfo `json:"ipv4"`
	IPV6                         []IPInfo `json:"ipv6"`
	Source                       string   `json:"source"` // 资产来源: subfinder, portscan, urlfinder, etc.
	// 目录扫描相关字段
	Path          string `json:"path,omitempty"`          // 发现的路径
	ContentLength int64  `json:"contentLength,omitempty"` // 响应内容长度
	ContentType   string `json:"contentType,omitempty"`   // 响应内容类型
	ContentWords  int64  `json:"contentWords,omitempty"`  // 响应单词数
	ContentLines  int64  `json:"contentLines,omitempty"`  // 响应行数
	Duration      int64  `json:"duration,omitempty"`      // 请求耗时(ms)
	RequestRaw    string `json:"requestRaw,omitempty"`    // HTTP请求原文（AI研判用）
	ResponseRaw   string `json:"responseRaw,omitempty"`   // HTTP响应原文（AI研判用）
	// 子域接管检测字段
	TakeoverRisk    bool   `json:"takeoverRisk,omitempty"`    // 是否存在接管风险
	TakeoverService string `json:"takeoverService,omitempty"` // 可接管的服务
	TakeoverCName   string `json:"takeoverCname,omitempty"`   // 指向的CNAME
}

// IPInfo IP信息
type IPInfo struct {
	IP       string `json:"ip"`
	Location string `json:"location"`
}

// Vulnerability 漏洞
type Vulnerability struct {
	Authority string `json:"authority"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Url       string `json:"url"`
	PocFile   string `json:"pocFile"`
	Source    string `json:"source"`
	// 风险来源（如 auto:cert-expiry / auto:weakpass / auto:info-leak），供风险视图与复验按来源查询
	RiskSource string   `json:"riskSource,omitempty"`
	Severity   string   `json:"severity"`
	Extra      string   `json:"extra"`
	Result     string   `json:"result"`
	VulName    string   `json:"vulName,omitempty"`
	Tags       []string `json:"tags,omitempty"`

	// 漏洞知识库关联字段
	CvssScore   float64  `json:"cvssScore,omitempty"`
	CveId       string   `json:"cveId,omitempty"`
	CweId       string   `json:"cweId,omitempty"`
	Remediation string   `json:"remediation,omitempty"`
	References  []string `json:"references,omitempty"`

	// 证据链字段
	MatcherName       string   `json:"matcherName,omitempty"`
	ExtractedResults  []string `json:"extractedResults,omitempty"`
	CurlCommand       string   `json:"curlCommand,omitempty"`
	Request           string   `json:"request,omitempty"`
	Response          string   `json:"response,omitempty"`
	ResponseTruncated bool     `json:"responseTruncated,omitempty"`
}

// BaseScanner 基础扫描器
type BaseScanner struct {
	name string
}

func (s *BaseScanner) Name() string {
	return s.name
}
