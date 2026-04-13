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

// TypedScanner 类型安全的扫描器接口（泛型版本）
// 用于需要强类型选项的扫描器实现
type TypedScanner[T ScannerOptions] interface {
	Scanner
	// ScanWithOptions 使用类型安全的选项执行扫描
	ScanWithOptions(ctx context.Context, config *ScanConfig, opts T) (*ScanResult, error)
}

// ScanConfig 扫描配置
type ScanConfig struct {
	Target      string      `json:"target"`
	Targets     []string    `json:"targets"`
	Assets      []*Asset    `json:"assets"`
	Options     interface{} `json:"options"`
	WorkspaceId string      `json:"workspaceId"`
	MainTaskId  string      `json:"mainTaskId"`
	// TaskLogger 任务日志回调，用于将扫描日志推送到任务日志流
	TaskLogger func(level, format string, args ...interface{}) `json:"-"`
	// OnProgress 进度回调，参数为当前进度(0-100)和描述
	OnProgress func(progress int, message string) `json:"-"`
	// OnAssetUpdated 局部资产完成更新时的回调事件（用于流式结果更新）
	OnAssetUpdated func(asset *Asset) `json:"-"`
}

// GetTypedOptions 从 ScanConfig 中提取类型安全的选项
// 如果 Options 已经是目标类型，直接返回
// 否则返回 nil 和 false
func GetTypedOptions[T ScannerOptions](config *ScanConfig) (T, bool) {
	if config.Options == nil {
		var zero T
		return zero, false
	}
	if opts, ok := config.Options.(T); ok {
		return opts, true
	}
	var zero T
	return zero, false
}

// ScanResult 扫描结果
type ScanResult struct {
	WorkspaceId     string           `json:"workspaceId"`
	MainTaskId      string           `json:"mainTaskId"`
	Assets          []*Asset         `json:"assets"`
	Vulnerabilities []*Vulnerability `json:"vulnerabilities"`
}

// Asset 资产
type Asset struct {
	Authority  string   `json:"authority"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	Category   string   `json:"category"` // ipv4/ipv6/domain/url
	Service    string   `json:"service"`
	Server     string   `json:"server"`
	Banner     string   `json:"banner"`
	Title      string   `json:"title"`
	App        []string `json:"app"`
	HttpStatus string   `json:"httpStatus"`
	HttpHeader string   `json:"httpHeader"`
	HttpBody   string   `json:"httpBody"`
	Cert       string   `json:"cert"`
	IconHash   string   `json:"iconHash"`
	IconData   []byte   `json:"iconData,omitempty"` // favicon 图片原始数据
	Screenshot string   `json:"screenshot"`
	IsCDN      bool     `json:"isCdn"`
	CName      string   `json:"cname"`
	IsCloud    bool     `json:"isCloud"`
	IsHTTP     bool     `json:"isHttp"` // 是否为HTTP服务
	IPV4       []IPInfo `json:"ipv4"`
	IPV6       []IPInfo `json:"ipv6"`
	Source     string   `json:"source"` // 资产来源: subfinder, portscan, urlfinder, etc.
	// 目录扫描相关字段
	Path          string `json:"path,omitempty"`          // 发现的路径
	ContentLength int64  `json:"contentLength,omitempty"` // 响应内容长度
	ContentType   string `json:"contentType,omitempty"`   // 响应内容类型
	ContentWords  int64  `json:"contentWords,omitempty"`  // 响应单词数
	ContentLines  int64  `json:"contentLines,omitempty"`  // 响应行数
	Duration      int64  `json:"duration,omitempty"`      // 请求耗时(ms)
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
	Authority string   `json:"authority"`
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	Url       string   `json:"url"`
	PocFile   string   `json:"pocFile"`
	Source    string   `json:"source"`
	Severity  string   `json:"severity"`
	Extra     string   `json:"extra"`
	Result    string   `json:"result"`
	VulName   string   `json:"vulName,omitempty"`
	Tags      []string `json:"tags,omitempty"`

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
