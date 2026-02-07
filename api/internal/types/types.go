package types

import (
	"time"
)

// ==================== 通用类型 ====================
type BaseResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type BaseRespWithId struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Id   string `json:"id,omitempty"`
}

type PageReq struct {
	Page     int `json:"page,default=1"`
	PageSize int `json:"pageSize,default=20"`
}

// ==================== 用户认证 ====================
type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResp struct {
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	Token       string `json:"token"`
	UserId      string `json:"userId"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	WorkspaceId string `json:"workspaceId"`
}

type UserInfo struct {
	Id       string `json:"id"`
	Username string `json:"username"`
	Status   string `json:"status"`
}

type UserListResp struct {
	Code  int        `json:"code"`
	Msg   string     `json:"msg"`
	Total int        `json:"total"`
	List  []UserInfo `json:"list"`
}

// ==================== 用户管理 ====================
type UserCreateReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Status   string `json:"status"`
}

type UserUpdateReq struct {
	Id       string `json:"id"`
	Username string `json:"username"`
	Status   string `json:"status"`
}

type UserDeleteReq struct {
	Id string `json:"id"`
}

type UserResetPasswordReq struct {
	Id          string `json:"id"`
	NewPassword string `json:"newPassword"`
}

// ==================== 工作空间 ====================
type Workspace struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreateTime  string `json:"createTime"`
}

type WorkspaceListResp struct {
	Code  int         `json:"code"`
	Msg   string      `json:"msg"`
	Total int         `json:"total"`
	List  []Workspace `json:"list"`
}

type WorkspaceSaveReq struct {
	Id          string `json:"id,optional"`
	Name        string `json:"name"`
	Description string `json:"description,optional"`
}

type WorkspaceDeleteReq struct {
	Id string `json:"id"`
}

// ==================== 组织管理 ====================
type Organization struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreateTime  string `json:"createTime"`
}

type OrganizationListResp struct {
	Code  int            `json:"code"`
	Msg   string         `json:"msg"`
	Total int            `json:"total"`
	List  []Organization `json:"list"`
}

type OrganizationSaveReq struct {
	Id          string `json:"id,optional"`
	Name        string `json:"name"`
	Description string `json:"description,optional"`
	Status      string `json:"status,optional"`
}

type OrganizationDeleteReq struct {
	Id string `json:"id"`
}

type OrganizationUpdateStatusReq struct {
	Id     string `json:"id"`
	Status string `json:"status"`
}

// ==================== 资产管理 ====================

// IPV4Info IPv4地址信息
type IPV4Info struct {
	IP       string `json:"ip"`
	Location string `json:"location,omitempty"`
}

// IPV6Info IPv6地址信息
type IPV6Info struct {
	IP       string `json:"ip"`
	Location string `json:"location,omitempty"`
}

// IPInfo IP地址信息
type IPInfo struct {
	IPV4 []IPV4Info `json:"ipv4,omitempty"`
	IPV6 []IPV6Info `json:"ipv6,omitempty"`
}

type Asset struct {
	Id                   string   `json:"id"`
	Authority            string   `json:"authority"`
	Host                 string   `json:"host"`
	Port                 int      `json:"port"`
	Category             string   `json:"category"`
	Service              string   `json:"service"`
	Title                string   `json:"title"`
	App                  []string `json:"app"`
	HttpStatus           string   `json:"httpStatus"`
	HttpHeader           string   `json:"httpHeader"`
	HttpBody             string   `json:"httpBody"`
	Banner               string   `json:"banner"`
	IconHash             string   `json:"iconHash"`
	IconData             string   `json:"iconData,omitempty"` // favicon 图片 base64
	Screenshot           string   `json:"screenshot"`
	Location             string   `json:"location"`
	IP                   *IPInfo  `json:"ip,omitempty"` // IP地址信息
	IsCDN                bool     `json:"isCdn"`
	IsCloud              bool     `json:"isCloud"`
	IsNew                bool     `json:"isNew"`
	IsUpdated            bool     `json:"isUpdated"`
	CreateTime           string   `json:"createTime"`
	UpdateTime           string   `json:"updateTime"`
	LastStatusChangeTime string   `json:"lastStatusChangeTime,omitempty"` // 标签状态最后变化时间
	FirstSeenTaskId      string   `json:"firstSeenTaskId,omitempty"`      // 首次发现的任务ID
	// 组织
	OrgId   string `json:"orgId,omitempty"`
	OrgName string `json:"orgName,omitempty"`
	// 风险评分
	RiskScore float64 `json:"riskScore,omitempty"`
	RiskLevel string  `json:"riskLevel,omitempty"`
}

type AssetListReq struct {
	Page         int    `json:"page,default=1"`
	PageSize     int    `json:"pageSize,default=20"`
	Query        string `json:"query,optional"`
	Host         string `json:"host,optional"`
	Port         int    `json:"port,optional"`
	Service      string `json:"service,optional"`
	Title        string `json:"title,optional"`
	App          string `json:"app,optional"`
	HttpStatus   string `json:"httpStatus,optional"`
	IconHash     string `json:"iconHash,optional"`
	OrgId        string `json:"orgId,optional"`
	OnlyNew      bool   `json:"onlyNew,optional"`
	OnlyUpdated  bool   `json:"onlyUpdated,optional"`
	ExcludeCdn   bool   `json:"excludeCdn,optional"`
	SortByUpdate bool   `json:"sortByUpdate,optional"`
	// 新增字段 - 按风险评分排序
	SortByRisk bool `json:"sortByRisk,optional"`
	// 新增字段 - 时间范围筛选（最近N天内更新的资产）
	UpdatedWithinDays int `json:"updatedWithinDays,optional"` // 0表示不限制
}

type AssetListResp struct {
	Code  int     `json:"code"`
	Msg   string  `json:"msg"`
	Total int     `json:"total"`
	List  []Asset `json:"list"`
}

type AssetStatResp struct {
	Code         int                `json:"code"`
	Msg          string             `json:"msg"`
	TotalAsset   int                `json:"totalAsset"`
	TotalHost    int                `json:"totalHost"`
	NewCount     int                `json:"newCount"`
	UpdatedCount int                `json:"updatedCount"`
	TopPorts     []StatItem         `json:"topPorts"`
	TopService   []StatItem         `json:"topService"`
	TopApp       []StatItem         `json:"topApp"`
	TopTitle     []StatItem         `json:"topTitle"`
	TopIconHash  []IconHashStatItem `json:"topIconHash,omitempty"`
	// 新增字段 - 风险等级分布
	RiskDistribution map[string]int `json:"riskDistribution,omitempty"`
}

type StatItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type IconHashStatItem struct {
	IconHash string `json:"iconHash"`
	IconData string `json:"iconData"` // base64 图片数据
	Count    int    `json:"count"`
}

type AssetDeleteReq struct {
	Id          string `json:"id"`
	WorkspaceId string `json:"workspaceId,optional"` // 可选，用于跨工作空间删除
}

type AssetBatchDeleteReq struct {
	Ids []string `json:"ids"`
}

// AssetImportReq 资产导入请求
type AssetImportReq struct {
	Targets []string `json:"targets"` // 目标列表，支持 IP:端口 或 URL 格式
}

// AssetImportResp 资产导入响应
type AssetImportResp struct {
	Code       int    `json:"code"`
	Msg        string `json:"msg"`
	Total      int    `json:"total"`      // 总数
	NewCount   int    `json:"newCount"`   // 新增数量
	SkipCount  int    `json:"skipCount"`  // 跳过数量（已存在）
	ErrorCount int    `json:"errorCount"` // 错误数量
}

type AssetHistoryReq struct {
	AssetId string `json:"assetId"`
	Limit   int    `json:"limit,default=20"`
}

// FieldChange 字段变更记录
type FieldChange struct {
	Field    string `json:"field"`    // 变更的字段名
	OldValue string `json:"oldValue"` // 旧值
	NewValue string `json:"newValue"` // 新值
}

type AssetHistoryItem struct {
	Id         string        `json:"id"`
	Authority  string        `json:"authority"`
	Host       string        `json:"host"`
	Port       int           `json:"port"`
	Service    string        `json:"service"`
	Title      string        `json:"title"`
	App        []string      `json:"app"`
	HttpStatus string        `json:"httpStatus"`
	HttpHeader string        `json:"httpHeader"`
	HttpBody   string        `json:"httpBody"`
	Banner     string        `json:"banner"`
	IconHash   string        `json:"iconHash"`
	Screenshot string        `json:"screenshot"`
	TaskId     string        `json:"taskId"`
	CreateTime string        `json:"createTime"`
	Changes    []FieldChange `json:"changes,omitempty"` // 变更详情
}

type AssetHistoryResp struct {
	Code int                `json:"code"`
	Msg  string             `json:"msg"`
	List []AssetHistoryItem `json:"list"`
}

// ==================== 站点管理 ====================
type SiteListReq struct {
	Page       int    `json:"page,default=1"`
	PageSize   int    `json:"pageSize,default=20"`
	Site       string `json:"site,optional"`
	Title      string `json:"title,optional"`
	App        string `json:"app,optional"`
	HttpStatus string `json:"httpStatus,optional"`
	OrgId      string `json:"orgId,optional"`
}

type Site struct {
	Id         string   `json:"id"`
	Site       string   `json:"site"`
	Title      string   `json:"title"`
	IP         string   `json:"ip"`
	Port       int      `json:"port"`
	Service    string   `json:"service"`
	HttpStatus string   `json:"httpStatus"`
	App        []string `json:"app"`
	Labels     []string `json:"labels"`
	Screenshot string   `json:"screenshot"`
	Location   string   `json:"location"`
	OrgId      string   `json:"orgId,omitempty"`
	OrgName    string   `json:"orgName,omitempty"`
	UpdateTime string   `json:"updateTime"`
	HttpHeader string   `json:"httpHeader,omitempty"`
	IconHash   string   `json:"iconHash,omitempty"`
	ColorTag   string   `json:"colorTag,omitempty"`
	Memo       string   `json:"memo,omitempty"`
}

type SiteListResp struct {
	Code  int    `json:"code"`
	Msg   string `json:"msg"`
	Total int    `json:"total"`
	List  []Site `json:"list"`
}

type SiteStatResp struct {
	Code       int `json:"code"`
	Total      int `json:"total"`
	HttpCount  int `json:"httpCount"`
	HttpsCount int `json:"httpsCount"`
	NewCount   int `json:"newCount"`
}

type SiteDeleteReq struct {
	Id string `json:"id"`
}

type SiteBatchDeleteReq struct {
	Ids []string `json:"ids"`
}

// ==================== 域名管理 ====================
type DomainListReq struct {
	Page       int    `json:"page,default=1"`
	PageSize   int    `json:"pageSize,default=20"`
	Domain     string `json:"domain,optional"`
	RootDomain string `json:"rootDomain,optional"`
	IP         string `json:"ip,optional"`
	OrgId      string `json:"orgId,optional"`
}

type Domain struct {
	Id         string   `json:"id"`
	Domain     string   `json:"domain"`
	RootDomain string   `json:"rootDomain"`
	IPs        []string `json:"ips"`
	CName      string   `json:"cname"`
	Source     string   `json:"source"`
	OrgId      string   `json:"orgId,omitempty"`
	OrgName    string   `json:"orgName,omitempty"`
	IsNew      bool     `json:"isNew"`
	CreateTime string   `json:"createTime"`
}

type DomainListResp struct {
	Code  int      `json:"code"`
	Msg   string   `json:"msg"`
	Total int      `json:"total"`
	List  []Domain `json:"list"`
}

type DomainStatResp struct {
	Code            int `json:"code"`
	Total           int `json:"total"`
	RootDomainCount int `json:"rootDomainCount"`
	ResolvedCount   int `json:"resolvedCount"`
	NewCount        int `json:"newCount"`
}

type DomainDeleteReq struct {
	Id string `json:"id"`
}

type DomainBatchDeleteReq struct {
	Ids []string `json:"ids"`
}

// ==================== 资产分组管理 ====================
type AssetGroupsReq struct {
	Page     int    `json:"page,default=1"`
	PageSize int    `json:"pageSize,default=20"`
	Query    string `json:"query,optional"` // 搜索关键词
}

type AssetGroup struct {
	Domain        string    `json:"domain"`        // 域名/分组名称
	Source        string    `json:"source"`        // 来源
	Status        string    `json:"status"`        // 状态：running/finished/failed/stopped
	TotalServices int       `json:"totalServices"` // 总服务数
	Duration      string    `json:"duration"`      // 持续时间
	LastUpdated   string    `json:"lastUpdated"`   // 最后更新时间（相对时间）
	FirstSeen     time.Time `json:"-"`             // 首次发现时间（内部使用）
	LatestUpdate  time.Time `json:"-"`             // 最新更新时间（内部使用）
}

type AssetGroupsResp struct {
	Code  int          `json:"code"`
	Msg   string       `json:"msg"`
	Total int          `json:"total"`
	List  []AssetGroup `json:"list"`
}

type DeleteAssetGroupReq struct {
	Domain string `json:"domain"` // 要删除的域名/分组名称
}

type DeleteAssetGroupResp struct {
	Code         int    `json:"code"`
	Msg          string `json:"msg"`
	DeletedCount int64  `json:"deletedCount"` // 删除的资产数量
}

// ==================== 资产清单管理 ====================
type AssetInventoryReq struct {
	Page         int      `json:"page,default=1"`
	PageSize     int      `json:"pageSize,default=20"`
	Query        string   `json:"query,optional"`        // 搜索关键词
	Technologies []string `json:"technologies,optional"` // 技术栈过滤
	Ports        []int    `json:"ports,optional"`        // 端口过滤
	StatusCodes  []string `json:"statusCodes,optional"`  // 状态码过滤
	Labels       []string `json:"labels,optional"`       // 标签过滤
	TimeRange    string   `json:"timeRange,optional"`    // 时间范围: all/24h/7d/30d
	SortBy       string   `json:"sortBy,optional"`       // 排序字段: time/name/port
	GroupId      string   `json:"groupId,optional"`      // 资产分组ID
	Domain       string   `json:"domain,optional"`       // 域名过滤
}

type AssetInventoryItem struct {
	Id              string   `json:"id"`
	WorkspaceId     string   `json:"workspaceId"` // 所属工作空间ID
	Host            string   `json:"host"`
	IP              string   `json:"ip"`
	Port            int      `json:"port"`
	Service         string   `json:"service"`
	Title           string   `json:"title"`
	Technologies    []string `json:"technologies"`    // 技术栈
	Labels          []string `json:"labels"`          // 自定义标签
	Status          string   `json:"status"`          // HTTP状态码
	LastUpdated     string   `json:"lastUpdated"`     // 最后更新时间（相对时间）
	FirstSeen       string   `json:"firstSeen"`       // 首次发现时间（完整时间）
	LastUpdatedFull string   `json:"lastUpdatedFull"` // 最后更新时间（完整时间）
	Screenshot      string   `json:"screenshot,omitempty"`
	IconHash        string   `json:"iconHash,omitempty"`
	IconHashBytes   string   `json:"iconHashBytes,omitempty"` // Base64编码的图标数据
	HttpHeader      string   `json:"httpHeader,omitempty"`
	HttpBody        string   `json:"httpBody,omitempty"`
	Banner          string   `json:"banner,omitempty"`
	CName           string   `json:"cname,omitempty"`
	ASN             string   `json:"asn,omitempty"`
}

type AssetInventoryResp struct {
	Code  int                  `json:"code"`
	Msg   string               `json:"msg"`
	Total int                  `json:"total"`
	List  []AssetInventoryItem `json:"list"`
}

// ==================== 截图清单管理 ====================
type ScreenshotsReq struct {
	Page          int      `json:"page,default=1"`
	PageSize      int      `json:"pageSize,default=24"`
	Query         string   `json:"query,optional"`         // 搜索关键词
	Technologies  []string `json:"technologies,optional"`  // 技术栈过滤
	Ports         []int    `json:"ports,optional"`         // 端口过滤
	StatusCodes   []string `json:"statusCodes,optional"`   // 状态码过滤
	TimeRange     string   `json:"timeRange,optional"`     // 时间范围: all/24h/7d/30d
	SortBy        string   `json:"sortBy,optional"`        // 排序字段: time/name
	Domain        string   `json:"domain,optional"`        // 域名过滤
	HasScreenshot bool     `json:"hasScreenshot,optional"` // 只显示有截图的
}

type ScreenshotItem struct {
	Id           string       `json:"id"`
	WorkspaceId  string       `json:"workspaceId"` // 工作空间ID
	Name         string       `json:"name"`        // 主机名
	Port         int          `json:"port"`
	IP           string       `json:"ip"`
	Status       string       `json:"status"`       // HTTP状态码
	StatusText   string       `json:"statusText"`   // 状态文本
	Title        string       `json:"title"`        // 页面标题
	Screenshot   string       `json:"screenshot"`   // 截图URL
	LastUpdated  string       `json:"lastUpdated"`  // 最后更新时间
	Technologies []Technology `json:"technologies"` // 技术栈
}

type Technology struct {
	Name string `json:"name"`
}

type ScreenshotsResp struct {
	Code  int              `json:"code"`
	Msg   string           `json:"msg"`
	Total int              `json:"total"`
	List  []ScreenshotItem `json:"list"`
}

// ==================== IP管理 ====================
type IPListReq struct {
	Page     int    `json:"page,default=1"`
	PageSize int    `json:"pageSize,default=20"`
	IP       string `json:"ip,optional"`
	Port     string `json:"port,optional"`
	Service  string `json:"service,optional"`
	Location string `json:"location,optional"`
	OrgId    string `json:"orgId,optional"`
}

type PortInfo struct {
	Port    int    `json:"port"`
	Service string `json:"service"`
}

type IPAsset struct {
	Id          string     `json:"id"`
	IP          string     `json:"ip"`
	Location    string     `json:"location"`
	ASN         string     `json:"asn,omitempty"`
	ISP         string     `json:"isp,omitempty"`
	Ports       []PortInfo `json:"ports"`
	Domains     []string   `json:"domains"`
	DomainCount int        `json:"domainCount"`
	OrgId       string     `json:"orgId,omitempty"`
	OrgName     string     `json:"orgName,omitempty"`
	UpdateTime  string     `json:"updateTime"`
	IsNew       bool       `json:"isNew"`
}

type IPListResp struct {
	Code  int       `json:"code"`
	Msg   string    `json:"msg"`
	Total int       `json:"total"`
	List  []IPAsset `json:"list"`
}

type IPStatResp struct {
	Code         int `json:"code"`
	Total        int `json:"total"`
	PortCount    int `json:"portCount"`
	ServiceCount int `json:"serviceCount"`
	NewCount     int `json:"newCount"`
}

type IPDeleteReq struct {
	IP string `json:"ip"`
}

type IPBatchDeleteReq struct {
	IPs []string `json:"ips"`
}

// ==================== 任务管理 ====================
type MainTask struct {
	Id           string   `json:"id"`
	TaskId       string   `json:"taskId"` // UUID，用于日志查询
	Name         string   `json:"name"`
	Target       string   `json:"target"`
	Config       string   `json:"config"` // 任务配置JSON
	ProfileId    string   `json:"profileId"`
	ProfileName  string   `json:"profileName"`
	Tags         []string `json:"tags"` // 任务标签
	Status       string   `json:"status"`
	CurrentPhase string   `json:"currentPhase"` // 当前执行阶段
	Progress     int      `json:"progress"`
	Result       string   `json:"result"`
	IsCron       bool     `json:"isCron"`
	CronRule     string   `json:"cronRule"`
	CreateTime   string   `json:"createTime"`
	StartTime    string   `json:"startTime"`    // 开始时间
	EndTime      string   `json:"endTime"`      // 结束时间
	SubTaskCount int      `json:"subTaskCount"` // 子任务总数
	SubTaskDone  int      `json:"subTaskDone"`  // 已完成子任务数
	WorkspaceId  string   `json:"workspaceId"`  // 所属工作空间ID
}

type MainTaskListReq struct {
	Page        int      `json:"page,default=1"`
	PageSize    int      `json:"pageSize,default=20"`
	Name        string   `json:"name,optional"`
	Status      string   `json:"status,optional"`
	Tags        []string `json:"tags,optional"`        // 标签过滤
	WorkspaceId string   `json:"workspaceId,optional"` // 支持从请求体传递，优先级高于header
}

type MainTaskListResp struct {
	Code  int        `json:"code"`
	Msg   string     `json:"msg"`
	Total int        `json:"total"`
	List  []MainTask `json:"list"`
}

type MainTaskCreateReq struct {
	Name        string   `json:"name"`
	Target      string   `json:"target"`
	ProfileId   string   `json:"profileId,optional"`  // 可选，兼容旧版
	TemplateId  string   `json:"templateId,optional"` // 扫描配置模板ID
	Config      string   `json:"config,optional"`     // 直接传递配置JSON
	OrgId       string   `json:"orgId,optional"`
	Tags        []string `json:"tags,optional"`        // 任务标签
	Workers     []string `json:"workers,optional"`     // 指定执行任务的 Worker 列表
	WorkspaceId string   `json:"workspaceId,optional"` // 任务所属工作空间ID
}

type TaskProfile struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Config      string `json:"config"`
}

type TaskProfileListResp struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	List []TaskProfile `json:"list"`
}

type TaskProfileSaveReq struct {
	Id          string `json:"id,optional"`
	Name        string `json:"name"`
	Description string `json:"description,optional"`
	Config      string `json:"config"`
}

type TaskProfileDeleteReq struct {
	Id string `json:"id"`
}

type MainTaskDeleteReq struct {
	Id          string `json:"id"`
	WorkspaceId string `json:"workspaceId,optional"` // 任务所属工作空间ID
}

type MainTaskBatchDeleteReq struct {
	Ids         []string `json:"ids"`
	WorkspaceId string   `json:"workspaceId,optional"` // 任务所属工作空间ID
}

type MainTaskRetryReq struct {
	Id string `json:"id"`
}

type MainTaskControlReq struct {
	Id          string `json:"id"`
	WorkspaceId string `json:"workspaceId,optional"` // 任务所属工作空间ID
}

// MainTaskUpdateReq 更新任务请求
type MainTaskUpdateReq struct {
	Id        string `json:"id"`                 // 任务ID
	Name      string `json:"name,optional"`      // 任务名称
	Target    string `json:"target,optional"`    // 扫描目标
	ProfileId string `json:"profileId,optional"` // 配置ID
}

// GetTaskLogsReq 获取任务日志请求
type GetTaskLogsReq struct {
	TaskId string `json:"taskId"`            // 任务ID
	Limit  int    `json:"limit,default=100"` // 返回条数限制
	Search string `json:"search,optional"`   // 模糊搜索关键词
}

// TaskLogEntry 任务日志条目
type TaskLogEntry struct {
	Timestamp  string `json:"timestamp"`
	Level      string `json:"level"`
	WorkerName string `json:"workerName"`
	TaskId     string `json:"taskId"`
	Message    string `json:"message"`
}

// GetTaskLogsResp 获取任务日志响应
type GetTaskLogsResp struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	List []TaskLogEntry `json:"list"`
}

// ==================== 漏洞管理 ====================
type Vul struct {
	Id         string `json:"id"`
	Authority  string `json:"authority"`
	Url        string `json:"url"`
	PocFile    string `json:"pocFile"`
	Source     string `json:"source"`
	Severity   string `json:"severity"`
	Result     string `json:"result"`
	CreateTime string `json:"createTime"`
	// 新增字段 - 时间追踪
	FirstSeenTime string `json:"firstSeenTime,omitempty"`
	LastSeenTime  string `json:"lastSeenTime,omitempty"`
	ScanCount     int    `json:"scanCount,omitempty"`
}

// VulEvidence 漏洞证据链
type VulEvidence struct {
	MatcherName       string   `json:"matcherName,omitempty"`
	ExtractedResults  []string `json:"extractedResults,omitempty"`
	CurlCommand       string   `json:"curlCommand,omitempty"`
	Request           string   `json:"request,omitempty"`
	Response          string   `json:"response,omitempty"`
	ResponseTruncated bool     `json:"responseTruncated,omitempty"`
}

// VulDetail 漏洞详情（包含知识库信息和证据链）
type VulDetail struct {
	Id         string `json:"id"`
	Authority  string `json:"authority"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Url        string `json:"url"`
	PocFile    string `json:"pocFile"`
	Source     string `json:"source"`
	Severity   string `json:"severity"`
	Result     string `json:"result"`
	CreateTime string `json:"createTime"`
	// 知识库信息
	CvssScore   float64  `json:"cvssScore,omitempty"`
	CveId       string   `json:"cveId,omitempty"`
	CweId       string   `json:"cweId,omitempty"`
	Remediation string   `json:"remediation,omitempty"`
	References  []string `json:"references,omitempty"`
	// 证据链
	Evidence *VulEvidence `json:"evidence,omitempty"`
	// 时间追踪
	FirstSeenTime string `json:"firstSeenTime,omitempty"`
	LastSeenTime  string `json:"lastSeenTime,omitempty"`
	ScanCount     int    `json:"scanCount,omitempty"`
}

// VulDetailReq 漏洞详情请求
type VulDetailReq struct {
	Id string `json:"id"`
}

// VulDetailResp 漏洞详情响应
type VulDetailResp struct {
	Code int        `json:"code"`
	Msg  string     `json:"msg"`
	Data *VulDetail `json:"data,omitempty"`
}

type VulListReq struct {
	Page      int    `json:"page,default=1"`
	PageSize  int    `json:"pageSize,default=20"`
	Authority string `json:"authority,optional"`
	Severity  string `json:"severity,optional"`
	Source    string `json:"source,optional"`
	Host      string `json:"host,optional"`
	Port      int    `json:"port,optional"`
}

type VulListResp struct {
	Code  int    `json:"code"`
	Msg   string `json:"msg"`
	Total int    `json:"total"`
	List  []Vul  `json:"list"`
}

type VulDeleteReq struct {
	Id string `json:"id"`
}

type VulBatchDeleteReq struct {
	Ids []string `json:"ids"`
}

// VulStatResp 漏洞统计响应
type VulStatResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Total    int    `json:"total"`
	Critical int    `json:"critical"`
	High     int    `json:"high"`
	Medium   int    `json:"medium"`
	Low      int    `json:"low"`
	Info     int    `json:"info"`
	Week     int    `json:"week"`  // 近7天
	Month    int    `json:"month"` // 近30天
}

// TaskStatResp 任务统计响应
type TaskStatResp struct {
	Code      int    `json:"code"`
	Msg       string `json:"msg"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Running   int    `json:"running"`
	Failed    int    `json:"failed"`
	Pending   int    `json:"pending"`
	// 近7天每日趋势
	TrendDays      []string `json:"trendDays"`      // 日期标签
	TrendCompleted []int    `json:"trendCompleted"` // 每日完成数
	TrendFailed    []int    `json:"trendFailed"`    // 每日失败数
}

// ==================== Worker管理 ====================
type Worker struct {
	Name         string          `json:"name"`
	IP           string          `json:"ip"`
	CPULoad      float64         `json:"cpuLoad"`
	MemUsed      float64         `json:"memUsed"`
	TaskCount    int             `json:"taskCount"`    // 已执行任务数
	RunningCount int             `json:"runningCount"` // 正在执行任务数
	Concurrency  int             `json:"concurrency"`  // 并发数
	Status       string          `json:"status"`
	UpdateTime   string          `json:"updateTime"`
	Tools        map[string]bool `json:"tools"` // 工具安装状态
	// 智能调度器状态
	SchedulerMode        string `json:"schedulerMode,omitempty"`        // 调度模式: aggressive, normal, conservative, critical
	EffectiveConcurrency int    `json:"effectiveConcurrency,omitempty"` // 实际生效的并发数
	IsThrottled          bool   `json:"isThrottled,omitempty"`          // 是否处于限流状态
	HealthStatus         string `json:"healthStatus,omitempty"`         // 健康状态: healthy, warning, overloaded, throttled
}

type WorkerListResp struct {
	Code int      `json:"code"`
	Msg  string   `json:"msg"`
	List []Worker `json:"list"`
}

type WorkerDeleteReq struct {
	Name string `json:"name"` // Worker名称
}

type WorkerDeleteResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type WorkerRenameReq struct {
	OldName string `json:"oldName"` // 原Worker名称
	NewName string `json:"newName"` // 新Worker名称
}

type WorkerRenameResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type WorkerRestartReq struct {
	Name string `json:"name"` // Worker名称
}

type WorkerRestartResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type WorkerSetConcurrencyReq struct {
	Name        string `json:"name"`        // Worker名称
	Concurrency int    `json:"concurrency"` // 新的并发数
}

type WorkerSetConcurrencyResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// ==================== 在线API搜索 ====================
type OnlineSearchReq struct {
	Platform string `json:"platform"` // fofa/hunter/quake
	Query    string `json:"query"`
	Page     int    `json:"page,default=1"`
	PageSize int    `json:"pageSize,default=20"`
}

type OnlineSearchResult struct {
	Host     string `json:"host"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Domain   string `json:"domain"`
	Title    string `json:"title"`
	Server   string `json:"server"`
	Country  string `json:"country"`
	City     string `json:"city"`
	Banner   string `json:"banner"`
	ICP      string `json:"icp"`
	Product  string `json:"product"`
	OS       string `json:"os"`
}

type OnlineSearchResp struct {
	Code  int                  `json:"code"`
	Msg   string               `json:"msg"`
	Total int                  `json:"total"`
	List  []OnlineSearchResult `json:"list"`
}

type OnlineImportReq struct {
	Platform string               `json:"platform"`
	Assets   []OnlineSearchResult `json:"assets"`
}

// OnlineImportAllReq 导入全部资产请求
type OnlineImportAllReq struct {
	Platform string `json:"platform"` // fofa/hunter/quake
	Query    string `json:"query"`
	PageSize int    `json:"pageSize,default=100"`
	MaxPages int    `json:"maxPages,default=10"` // 最大导入页数，防止过多消耗API配额
}

// OnlineImportAllResp 导入全部资产响应
type OnlineImportAllResp struct {
	Code         int    `json:"code"`
	Msg          string `json:"msg"`
	TotalFetched int    `json:"totalFetched"` // 获取到的总数
	TotalImport  int    `json:"totalImport"`  // 成功导入数
	TotalPages   int    `json:"totalPages"`   // 总页数
}

// ==================== API配置 ====================
type APIConfig struct {
	Id         string `json:"id"`
	Platform   string `json:"platform"`
	Key        string `json:"key"`
	Secret     string `json:"secret"`
	Version    string `json:"version"` // fofa版本: v4/v5
	Status     string `json:"status"`
	CreateTime string `json:"createTime"`
}

type APIConfigListResp struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	List []APIConfig `json:"list"`
}

type APIConfigSaveReq struct {
	Id       string `json:"id,optional"`
	Platform string `json:"platform"`
	Key      string `json:"key"`
	Secret   string `json:"secret,optional"`
	Version  string `json:"version,optional"` // fofa版本: v4/v5
}

// ==================== POC标签映射 ====================
type TagMapping struct {
	Id          string   `json:"id"`
	AppName     string   `json:"appName"`
	NucleiTags  []string `json:"nucleiTags"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	CreateTime  string   `json:"createTime"`
}

type TagMappingListResp struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	List []TagMapping `json:"list"`
}

type TagMappingSaveReq struct {
	Id          string   `json:"id,optional"`
	AppName     string   `json:"appName"`
	NucleiTags  []string `json:"nucleiTags"`
	Description string   `json:"description,optional"`
	Enabled     bool     `json:"enabled"`
}

type TagMappingDeleteReq struct {
	Id string `json:"id"`
}

// ==================== 自定义POC ====================
type CustomPoc struct {
	Id          string   `json:"id"`
	Name        string   `json:"name"`
	TemplateId  string   `json:"templateId"`
	Severity    string   `json:"severity"`
	Tags        []string `json:"tags"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Enabled     bool     `json:"enabled"`
	CreateTime  string   `json:"createTime"`
}

type CustomPocListReq struct {
	Page       int    `json:"page,default=1"`
	PageSize   int    `json:"pageSize,default=20"`
	Name       string `json:"name,optional"`       // 按名称筛选
	TemplateId string `json:"templateId,optional"` // 按模板ID筛选
	Severity   string `json:"severity,optional"`   // 按严重级别筛选
	Tag        string `json:"tag,optional"`        // 按标签筛选
	Enabled    *bool  `json:"enabled,optional"`    // 按状态筛选
}

type CustomPocListResp struct {
	Code  int         `json:"code"`
	Msg   string      `json:"msg"`
	Total int         `json:"total"`
	List  []CustomPoc `json:"list"`
}

type CustomPocSaveReq struct {
	Id          string   `json:"id,optional"`
	Name        string   `json:"name"`
	TemplateId  string   `json:"templateId"`
	Severity    string   `json:"severity"`
	Tags        []string `json:"tags,optional"`
	Author      string   `json:"author,optional"`
	Description string   `json:"description,optional"`
	Content     string   `json:"content"`
	Enabled     bool     `json:"enabled"`
}

type CustomPocDeleteReq struct {
	Id string `json:"id"`
}

// ValidatePocSyntaxReq 验证POC语法请求
type ValidatePocSyntaxReq struct {
	Content string `json:"content"` // POC YAML内容
}

// ValidatePocSyntaxResp 验证POC语法响应
type ValidatePocSyntaxResp struct {
	Code  int    `json:"code"`
	Msg   string `json:"msg"`
	Valid bool   `json:"valid"` // 是否有效
	Error string `json:"error"` // 错误信息（如果无效）
}

// CustomPocBatchImportReq 批量导入自定义POC请求
type CustomPocBatchImportReq struct {
	Pocs []CustomPocSaveReq `json:"pocs"` // POC列表
}

// CustomPocBatchImportResp 批量导入自定义POC响应
type CustomPocBatchImportResp struct {
	Code     int      `json:"code"`
	Msg      string   `json:"msg"`
	Imported int      `json:"imported"` // 成功导入数量
	Failed   int      `json:"failed"`   // 失败数量
	Errors   []string `json:"errors"`   // 错误信息列表
}

// CustomPocClearAllReq 清空自定义POC请求（支持按筛选条件清空）
type CustomPocClearAllReq struct {
	Name       string `json:"name,optional"`       // 按名称筛选
	TemplateId string `json:"templateId,optional"` // 按模板ID筛选
	Severity   string `json:"severity,optional"`   // 按严重级别筛选
	Tag        string `json:"tag,optional"`        // 按标签筛选
	Enabled    *bool  `json:"enabled,optional"`    // 按状态筛选
}

// CustomPocClearAllResp 清空自定义POC响应
type CustomPocClearAllResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Deleted int    `json:"deleted"` // 删除数量
}

// CustomPocScanAssetsReq 自定义POC扫描现有资产请求
type CustomPocScanAssetsReq struct {
	PocId       string `json:"pocId"`                // POC ID
	UpdateAsset bool   `json:"updateAsset,optional"` // 发现漏洞后是否更新资产
}

// CustomPocScanAssetsResp 自定义POC扫描现有资产响应
type CustomPocScanAssetsResp struct {
	Code         int                     `json:"code"`
	Msg          string                  `json:"msg"`
	TotalScanned int                     `json:"totalScanned"` // 扫描的资产总数
	VulnCount    int                     `json:"vulnCount"`    // 发现的漏洞数
	Duration     string                  `json:"duration"`     // 耗时
	VulnList     []CustomPocScanVulnItem `json:"vulnList"`     // 漏洞列表
	TaskIds      []string                `json:"taskIds"`      // 任务ID列表（用于前端监听日志）
}

// CustomPocScanVulnItem 扫描发现的漏洞项
type CustomPocScanVulnItem struct {
	AssetId   string `json:"assetId"`
	Authority string `json:"authority"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Title     string `json:"title"`
	Matched   bool   `json:"matched"`
	Details   string `json:"details,omitempty"`
}

// ==================== Nuclei默认模板 ====================
type NucleiTemplateListReq struct {
	Category string `json:"category,optional"` // 分类筛选
	Severity string `json:"severity,optional"` // 严重级别筛选
	Tag      string `json:"tag,optional"`      // 标签筛选
	Keyword  string `json:"keyword,optional"`  // 关键词搜索
	Page     int    `json:"page,default=1"`
	PageSize int    `json:"pageSize,default=50"`
	// 新增字段 - CVSS评分筛选和CVE搜索
	MinCvssScore float64 `json:"minCvssScore,optional"` // 最小CVSS评分筛选
	CveId        string  `json:"cveId,optional"`        // CVE编号搜索
}

type NucleiTemplate struct {
	Id          string   `json:"id"`          // 模板ID
	Name        string   `json:"name"`        // 模板名称
	Author      string   `json:"author"`      // 作者
	Severity    string   `json:"severity"`    // 严重级别
	Description string   `json:"description"` // 描述
	Tags        []string `json:"tags"`        // 标签
	Category    string   `json:"category"`    // 分类(目录名)
	FilePath    string   `json:"filePath"`    // 文件路径
	// 新增字段 - 漏洞知识库
	CvssScore   float64  `json:"cvssScore,omitempty"`   // CVSS评分
	CvssMetrics string   `json:"cvssMetrics,omitempty"` // CVSS向量
	CveIds      []string `json:"cveIds,omitempty"`      // CVE编号列表
	CweIds      []string `json:"cweIds,omitempty"`      // CWE编号列表
	References  []string `json:"references,omitempty"`  // 参考链接
	Remediation string   `json:"remediation,omitempty"` // 修复建议
}

type NucleiTemplateListResp struct {
	Code  int              `json:"code"`
	Msg   string           `json:"msg"`
	Total int              `json:"total"`
	List  []NucleiTemplate `json:"list"`
}

type NucleiTemplateCategoriesResp struct {
	Code       int            `json:"code"`
	Msg        string         `json:"msg"`
	Categories []string       `json:"categories"` // 分类列表
	Severities []string       `json:"severities"` // 严重级别列表
	Tags       []string       `json:"tags"`       // 常用标签列表
	Stats      map[string]int `json:"stats"`      // 统计信息
}

type NucleiTemplateUpdateEnabledReq struct {
	TemplateIds []string `json:"templateIds"` // 模板ID列表
	Enabled     bool     `json:"enabled"`     // 启用/禁用
}

type NucleiTemplateDetailReq struct {
	TemplateId string `json:"templateId"` // 模板ID
}

type NucleiTemplateDetailResp struct {
	Code int                        `json:"code"`
	Msg  string                     `json:"msg"`
	Data *NucleiTemplateWithContent `json:"data"`
}

// NucleiTemplateSyncReq 同步Nuclei模板请求
type NucleiTemplateSyncReq struct {
	Force     bool                       `json:"force,optional"`     // 是否强制清空后导入
	Templates []NucleiTemplateUploadItem `json:"templates,optional"` // 上传的模板列表
}

// NucleiTemplateUploadItem 上传的模板项
type NucleiTemplateUploadItem struct {
	Path    string `json:"path"`    // 相对路径
	Content string `json:"content"` // 模板内容
}

// NucleiTemplateSyncResp 同步Nuclei模板响应
type NucleiTemplateSyncResp struct {
	Code         int    `json:"code"`
	Msg          string `json:"msg"`
	SuccessCount int    `json:"successCount"` // 成功数量
	ErrorCount   int    `json:"errorCount"`   // 失败数量
}

// NucleiTemplateDownloadReq 下载Nuclei模板库请求
type NucleiTemplateDownloadReq struct {
	Force bool `json:"force,optional"` // 是否强制重新下载
}

// NucleiTemplateDownloadResp 下载Nuclei模板库响应
type NucleiTemplateDownloadResp struct {
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	TaskId string `json:"taskId,omitempty"` // 下载任务ID
}

// NucleiTemplateDownloadStatusReq 查询下载状态请求
type NucleiTemplateDownloadStatusReq struct {
	TaskId string `form:"taskId"` // 下载任务ID
}

// NucleiTemplateDownloadStatusResp 查询下载状态响应
type NucleiTemplateDownloadStatusResp struct {
	Code          int    `json:"code"`
	Msg           string `json:"msg"`
	Status        string `json:"status"`        // pending/downloading/completed/failed
	Progress      int    `json:"progress"`      // 进度百分比 0-100
	TemplateCount int    `json:"templateCount"` // 已下载模板数量
	Error         string `json:"error"`         // 错误信息
}

type NucleiTemplateWithContent struct {
	Id          string   `json:"id"`          // 模板ID
	Name        string   `json:"name"`        // 模板名称
	Author      string   `json:"author"`      // 作者
	Severity    string   `json:"severity"`    // 严重级别
	Description string   `json:"description"` // 描述
	Tags        []string `json:"tags"`        // 标签
	FilePath    string   `json:"filePath"`    // 文件路径
	Content     string   `json:"content"`     // YAML内容
	// 新增字段 - 漏洞知识库
	CvssScore   float64  `json:"cvssScore,omitempty"`   // CVSS评分
	CvssMetrics string   `json:"cvssMetrics,omitempty"` // CVSS向量
	CveIds      []string `json:"cveIds,omitempty"`      // CVE编号列表
	CweIds      []string `json:"cweIds,omitempty"`      // CWE编号列表
	References  []string `json:"references,omitempty"`  // 参考链接
	Remediation string   `json:"remediation,omitempty"` // 修复建议
}

// ==================== 指纹管理 ====================
type Fingerprint struct {
	Id          string            `json:"id"`
	Name        string            `json:"name"`
	Website     string            `json:"website"`
	Icon        string            `json:"icon"`
	Description string            `json:"description"`
	Headers     map[string]string `json:"headers"`
	Cookies     map[string]string `json:"cookies"`
	HTML        []string          `json:"html"`
	Scripts     []string          `json:"scripts"`
	ScriptSrc   []string          `json:"scriptSrc"`
	JS          map[string]string `json:"js"`
	Meta        map[string]string `json:"meta"`
	CSS         []string          `json:"css"`
	URL         []string          `json:"url"`
	Dom         string            `json:"dom"`
	Rule        string            `json:"rule"`   // ARL格式规则
	Source      string            `json:"source"` // 来源: wappalyzer, arl, custom
	Implies     []string          `json:"implies"`
	Excludes    []string          `json:"excludes"`
	CPE         string            `json:"cpe"`
	IsBuiltin   bool              `json:"isBuiltin"`
	Enabled     bool              `json:"enabled"`
	CreateTime  string            `json:"createTime"`
	UpdateTime  string            `json:"updateTime"`
}

type FingerprintListReq struct {
	Page      int    `json:"page,default=1"`
	PageSize  int    `json:"pageSize,default=50"`
	Keyword   string `json:"keyword,optional"`
	Source    string `json:"source,optional"` // 来源筛选: arl, custom
	IsBuiltin *bool  `json:"isBuiltin,optional"`
	Enabled   *bool  `json:"enabled,optional"`
}

type FingerprintListResp struct {
	Code  int           `json:"code"`
	Msg   string        `json:"msg"`
	Total int           `json:"total"`
	List  []Fingerprint `json:"list"`
}

type FingerprintSaveReq struct {
	Id          string            `json:"id,optional"`
	Name        string            `json:"name"`
	Website     string            `json:"website,optional"`
	Icon        string            `json:"icon,optional"`
	Description string            `json:"description,optional"`
	Rule        string            `json:"rule,optional"`   // ARL格式规则
	Source      string            `json:"source,optional"` // 来源: custom, arl
	Headers     map[string]string `json:"headers,optional"`
	Cookies     map[string]string `json:"cookies,optional"`
	HTML        []string          `json:"html,optional"`
	Scripts     []string          `json:"scripts,optional"`
	Meta        map[string]string `json:"meta,optional"`
	CSS         []string          `json:"css,optional"`
	URL         []string          `json:"url,optional"`
	Implies     []string          `json:"implies,optional"`
	Excludes    []string          `json:"excludes,optional"`
	Enabled     bool              `json:"enabled"`
	Type        string            `json:"type,optional"`        // 类型: passive, active
	ActivePaths []string          `json:"activePaths,optional"` // 主动指纹探测路径
}

type FingerprintDeleteReq struct {
	Id string `json:"id"`
}

type FingerprintCategoriesResp struct {
	Code       int              `json:"code"`
	Msg        string           `json:"msg"`
	Categories []string         `json:"categories"`
	Stats      map[string]int64 `json:"stats"`
}

type FingerprintSyncReq struct {
	Force bool `json:"force"` // 强制重新同步
}

type FingerprintImportReq struct {
	Content   string `json:"content"`            // 文件内容
	Format    string `json:"format"`             // 格式: auto, arl-json, arl-yaml, finger-json, finger-yaml, wappalyzer
	IsBuiltin bool   `json:"isBuiltin,optional"` // 是否导入为内置指纹
}

type FingerprintImportResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Imported int    `json:"imported"` // 导入数量
	Skipped  int    `json:"skipped"`  // 跳过数量
}

// FingerprintImportFromFileReq 从文件/目录导入指纹
type FingerprintImportFromFileReq struct {
	Path string `json:"path"` // 文件或目录路径
}

// FingerprintClearCustomReq 清空自定义指纹请求
type FingerprintClearCustomReq struct {
	Source string `json:"source,optional"` // 可选：按来源清空，如 arl, arl-finger, custom；为空则清空所有自定义指纹
}

// FingerprintClearCustomResp 清空自定义指纹响应
type FingerprintClearCustomResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Deleted int    `json:"deleted"` // 删除数量
}

// FingerprintValidateReq 验证指纹请求
type FingerprintValidateReq struct {
	Id  string `json:"id,optional"` // 指纹ID（验证已有指纹）
	Url string `json:"url"`         // 目标URL
}

// FingerprintValidateResp 验证指纹响应
type FingerprintValidateResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Matched bool   `json:"matched"` // 是否匹配
	Details string `json:"details"` // 匹配详情
}

// FingerprintMatchAssetsReq 验证指纹匹配现有资产请求
type FingerprintMatchAssetsReq struct {
	FingerprintId string `json:"fingerprintId"`        // 指纹ID
	Limit         int    `json:"limit,optional"`       // 最大匹配数量，默认100
	UpdateAsset   bool   `json:"updateAsset,optional"` // 是否更新匹配到的资产的指纹信息
}

// FingerprintMatchAssetsResp 验证指纹匹配现有资产响应
type FingerprintMatchAssetsResp struct {
	Code         int                       `json:"code"`
	Msg          string                    `json:"msg"`
	MatchedCount int                       `json:"matchedCount"` // 匹配数量
	TotalScanned int                       `json:"totalScanned"` // 扫描资产总数
	UpdatedCount int                       `json:"updatedCount"` // 更新的资产数量
	Duration     string                    `json:"duration"`     // 耗时
	MatchedList  []FingerprintMatchedAsset `json:"matchedList"`  // 匹配的资产列表
}

// FingerprintMatchedAsset 匹配的资产信息
type FingerprintMatchedAsset struct {
	Id        string `json:"id"`
	Authority string `json:"authority"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Title     string `json:"title"`
	Service   string `json:"service"`
}

// PocValidateReq 验证POC请求
type PocValidateReq struct {
	Id      string `json:"id,optional"`      // POC ID（验证已有POC）
	Url     string `json:"url"`              // 目标URL
	PocType string `json:"pocType,optional"` // POC类型: nuclei, custom (默认custom)
}

// PocValidateResp 验证POC响应
type PocValidateResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Matched  bool   `json:"matched"`  // 是否匹配/存在漏洞
	Severity string `json:"severity"` // 严重级别
	Details  string `json:"details"`  // 匹配详情
	TaskId   string `json:"taskId"`   // 任务ID（用于查询结果）
}

// PocBatchValidateReq 批量POC验证请求
type PocBatchValidateReq struct {
	Urls        []string `json:"urls"`                 // 目标URL列表
	PocType     string   `json:"pocType,optional"`     // POC类型: nuclei, custom, all (默认all)
	Severities  []string `json:"severities,optional"`  // 严重级别过滤
	Tags        []string `json:"tags,optional"`        // 标签过滤
	Timeout     int      `json:"timeout,optional"`     // 超时时间（秒，默认30）
	UseTemplate bool     `json:"useTemplate,optional"` // 是否使用默认模板（默认true）
	UseCustom   bool     `json:"useCustom,optional"`   // 是否使用自定义POC（默认true）
	Concurrency int      `json:"concurrency,optional"` // 并发数（默认10）
}

// PocValidationResult POC验证结果
type PocValidationResult struct {
	PocId      string   `json:"pocId"`      // POC ID
	PocName    string   `json:"pocName"`    // POC名称
	TemplateId string   `json:"templateId"` // 模板ID
	Severity   string   `json:"severity"`   // 严重级别
	Matched    bool     `json:"matched"`    // 是否匹配
	MatchedUrl string   `json:"matchedUrl"` // 匹配的URL
	Details    string   `json:"details"`    // 匹配详情
	Output     string   `json:"output"`     // 输出信息
	PocType    string   `json:"pocType"`    // POC类型: nuclei, custom
	Tags       []string `json:"tags"`       // 标签
}

// PocBatchValidateResp 批量POC验证响应
type PocBatchValidateResp struct {
	Code         int                   `json:"code"`
	Msg          string                `json:"msg"`
	TotalUrls    int                   `json:"totalUrls"`    // 总URL数量
	TotalPocs    int                   `json:"totalPocs"`    // 总POC数量
	MatchedCount int                   `json:"matchedCount"` // 匹配数量
	Duration     string                `json:"duration"`     // 耗时
	Results      []PocValidationResult `json:"results"`      // 验证结果列表
	UrlStats     map[string]int        `json:"urlStats"`     // 每个URL的匹配统计
	TaskId       string                `json:"taskId"`       // 任务ID（用于查询结果）
	BatchId      string                `json:"batchId"`      // 批次ID（用于查询结果）
}

// PocValidationResultQueryReq 查询POC验证结果请求
type PocValidationResultQueryReq struct {
	TaskId  string `json:"taskId,optional"`  // 任务ID
	BatchId string `json:"batchId,optional"` // 批次ID
}

// PocValidationResultQueryResp 查询POC验证结果响应
type PocValidationResultQueryResp struct {
	Code           int                   `json:"code"`
	Msg            string                `json:"msg"`
	Status         string                `json:"status"`         // 任务状态
	CompletedCount int                   `json:"completedCount"` // 已完成数量
	TotalCount     int                   `json:"totalCount"`     // 总数量
	Results        []PocValidationResult `json:"results"`        // 验证结果列表
	CreateTime     string                `json:"createTime"`     // 创建时间
	UpdateTime     string                `json:"updateTime"`     // 更新时间
}

// FingerprintBatchValidateReq 批量验证指纹请求
type FingerprintBatchValidateReq struct {
	Url   string `json:"url"`            // 目标URL
	Scope string `json:"scope,optional"` // 范围: all, builtin, custom
}

// FingerprintBatchValidateResp 批量验证指纹响应
type FingerprintBatchValidateResp struct {
	Code         int                      `json:"code"`
	Msg          string                   `json:"msg"`
	MatchedCount int                      `json:"matchedCount"` // 匹配数量
	Duration     string                   `json:"duration"`     // 耗时
	Matched      []MatchedFingerprintInfo `json:"matched"`      // 匹配的指纹列表
}

// MatchedFingerprintInfo 匹配的指纹信息
type MatchedFingerprintInfo struct {
	Id                string `json:"id"`
	Name              string `json:"name"`
	IsBuiltin         bool   `json:"isBuiltin"`
	MatchedConditions string `json:"matchedConditions"` // 命中的条件
}

// ==================== HTTP服务设置 ====================
// HttpServiceConfig HTTP服务端口配置
type HttpServiceConfig struct {
	HttpPorts    []int  `json:"httpPorts"`    // HTTP端口列表
	HttpsPorts   []int  `json:"httpsPorts"`   // HTTPS端口列表
	NonHttpPorts []int  `json:"nonHttpPorts"` // 非HTTP端口列表（明确排除）
	Description  string `json:"description"`  // 描述
}

// HttpServiceConfigGetResp 获取HTTP服务配置响应
type HttpServiceConfigGetResp struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data HttpServiceConfig `json:"data"`
}

// HttpServiceConfigSaveReq 保存HTTP服务配置请求
type HttpServiceConfigSaveReq struct {
	HttpPorts    []int  `json:"httpPorts"`
	HttpsPorts   []int  `json:"httpsPorts"`
	NonHttpPorts []int  `json:"nonHttpPorts,optional"`
	Description  string `json:"description,optional"`
}

// HttpServiceMapping HTTP服务映射
type HttpServiceMapping struct {
	Id          string `json:"id"`
	ServiceName string `json:"serviceName"` // 服务名称（小写）
	IsHttp      bool   `json:"isHttp"`      // 是否为HTTP服务
	Description string `json:"description"` // 描述
	Enabled     bool   `json:"enabled"`     // 是否启用
	CreateTime  string `json:"createTime"`
}

type HttpServiceMappingListReq struct {
	IsHttp  *bool  `json:"isHttp,optional"`  // 筛选：是否为HTTP服务
	Keyword string `json:"keyword,optional"` // 搜索：服务名称
}

type HttpServiceMappingListResp struct {
	Code int                  `json:"code"`
	Msg  string               `json:"msg"`
	List []HttpServiceMapping `json:"list"`
}

type HttpServiceMappingSaveReq struct {
	Id          string `json:"id,optional"`
	ServiceName string `json:"serviceName"`
	IsHttp      bool   `json:"isHttp"`
	Description string `json:"description,optional"`
	Enabled     bool   `json:"enabled"`
}

type HttpServiceMappingDeleteReq struct {
	Id string `json:"id"`
}

// HttpServiceExportReq 导出HTTP服务映射请求
type HttpServiceExportReq struct {
	Format string `json:"format,optional"` // 导出格式: txt (默认)
}

// HttpServiceExportResp 导出HTTP服务映射响应
type HttpServiceExportResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Content string `json:"content"` // 导出内容
}

// HttpServiceImportReq 导入HTTP服务映射请求
type HttpServiceImportReq struct {
	Content string `json:"content"` // 导入内容
}

// HttpServiceImportResp 导入HTTP服务映射响应
type HttpServiceImportResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Imported int    `json:"imported"` // 导入数量
	Skipped  int    `json:"skipped"`  // 跳过数量（重复）
}

// ==================== 报告管理 ====================
type ReportDetailReq struct {
	TaskId string `json:"taskId"`
}

type ReportAsset struct {
	Authority  string   `json:"authority"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	Service    string   `json:"service"`
	Title      string   `json:"title"`
	App        []string `json:"app"`
	HttpStatus string   `json:"httpStatus"`
	Server     string   `json:"server"`
	IconHash   string   `json:"iconHash"`
	Screenshot string   `json:"screenshot"`
	CreateTime string   `json:"createTime"`
}

type ReportVul struct {
	Authority  string `json:"authority"`
	Url        string `json:"url"`
	PocFile    string `json:"pocFile"`
	Severity   string `json:"severity"`
	Result     string `json:"result"`
	CreateTime string `json:"createTime"`
}

// ReportDirScan 报告中的目录扫描结果
type ReportDirScan struct {
	Authority     string `json:"authority"`
	URL           string `json:"url"`
	Path          string `json:"path"`
	StatusCode    int    `json:"statusCode"`
	ContentLength int64  `json:"contentLength"`
	ContentType   string `json:"contentType"`
	Title         string `json:"title"`
	CreateTime    string `json:"createTime"`
}

// ReportDirScanStat 目录扫描统计
type ReportDirScanStat struct {
	Total     int `json:"total"`
	Status2xx int `json:"status_2xx"`
	Status3xx int `json:"status_3xx"`
	Status4xx int `json:"status_4xx"`
	Status5xx int `json:"status_5xx"`
}

type ReportData struct {
	TaskId       string            `json:"taskId"`
	TaskName     string            `json:"taskName"`
	Target       string            `json:"target"`
	Status       string            `json:"status"`
	CreateTime   string            `json:"createTime"`
	AssetCount   int               `json:"assetCount"`
	VulCount     int               `json:"vulCount"`
	DirScanCount int               `json:"dirScanCount"`
	Assets       []ReportAsset     `json:"assets"`
	Vuls         []ReportVul       `json:"vuls"`
	DirScans     []ReportDirScan   `json:"dirScans"`
	DirScanStat  ReportDirScanStat `json:"dirScanStat"`
	TopPorts     []StatItem        `json:"topPorts"`
	TopServices  []StatItem        `json:"topServices"`
	TopApps      []StatItem        `json:"topApps"`
	VulStats     map[string]int    `json:"vulStats"`
}

type ReportDetailResp struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data *ReportData `json:"data"`
}

type ReportExportReq struct {
	TaskId string `json:"taskId"`
	Format string `json:"format,optional"` // excel, pdf (默认excel)
}

// ==================== 用户扫描配置 ====================
type SaveScanConfigReq struct {
	Config string `json:"config"` // 扫描配置JSON
}

type GetScanConfigResp struct {
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	Config string `json:"config"` // 扫描配置JSON
}

// ==================== Subfinder数据源配置 ====================
type SubfinderProvider struct {
	Id          string   `json:"id"`
	Provider    string   `json:"provider"`    // 数据源名称
	Keys        []string `json:"keys"`        // API密钥列表（脱敏后）
	Status      string   `json:"status"`      // enable/disable
	Description string   `json:"description"` // 描述
	CreateTime  string   `json:"createTime"`
	UpdateTime  string   `json:"updateTime"`
}

type SubfinderProviderListResp struct {
	Code int                 `json:"code"`
	Msg  string              `json:"msg"`
	List []SubfinderProvider `json:"list"`
}

type SubfinderProviderSaveReq struct {
	Provider    string   `json:"provider"`             // 数据源名称
	Keys        []string `json:"keys"`                 // API密钥列表
	Status      string   `json:"status,optional"`      // enable/disable
	Description string   `json:"description,optional"` // 描述
}

// SubfinderProviderMeta 数据源元信息（用于前端展示）
type SubfinderProviderMeta struct {
	Provider    string `json:"provider"`    // 数据源标识
	Name        string `json:"name"`        // 显示名称
	Description string `json:"description"` // 描述
	KeyFormat   string `json:"keyFormat"`   // 密钥格式说明
	URL         string `json:"url"`         // 获取API密钥的URL
}

type SubfinderProviderInfoResp struct {
	Code int                     `json:"code"`
	Msg  string                  `json:"msg"`
	List []SubfinderProviderMeta `json:"list"`
}

// ==================== AI辅助 ====================

type GeneratePocReq struct {
	Description string `json:"description,optional"` // 漏洞描述
	VulnType    string `json:"vulnType,optional"`    // 漏洞类型
	CveId       string `json:"cveId,optional"`       // CVE编号
	Reference   string `json:"reference,optional"`   // 参考信息
}

type GeneratePocResp struct {
	Code int              `json:"code"`
	Msg  string           `json:"msg"`
	Data *GeneratePocData `json:"data,omitempty"`
}

type GeneratePocData struct {
	Content string `json:"content"` // 生成的POC YAML内容
}

// AI配置
type AIConfig struct {
	Id         string `json:"id"`
	Protocol   string `json:"protocol"` // openai/anthropic/gemini
	BaseUrl    string `json:"baseUrl"`  // 服务地址
	ApiKey     string `json:"apiKey"`   // API密钥
	Model      string `json:"model"`    // 模型名称
	Status     string `json:"status"`   // enable/disable
	CreateTime string `json:"createTime"`
	UpdateTime string `json:"updateTime"`
}

type AIConfigGetResp struct {
	Code int       `json:"code"`
	Msg  string    `json:"msg"`
	Data *AIConfig `json:"data,omitempty"`
}

type AIConfigSaveReq struct {
	Protocol string `json:"protocol"` // openai/anthropic/gemini
	BaseUrl  string `json:"baseUrl"`
	ApiKey   string `json:"apiKey"`
	Model    string `json:"model"`
}

// ==================== Worker安装管理 ====================

// WorkerInstallCommandReq 获取Worker安装命令请求
type WorkerInstallCommandReq struct {
	ServerAddr string `json:"serverAddr,optional"` // API服务地址（可选，默认自动获取）
	RpcAddr    string `json:"rpcAddr,optional"`    // RPC服务地址（可选，默认自动获取）
	RedisAddr  string `json:"redisAddr,optional"`  // Redis地址（可选，默认自动获取）
}

// WorkerInstallCommandResp 获取Worker安装命令响应
type WorkerInstallCommandResp struct {
	Code       int               `json:"code"`
	Msg        string            `json:"msg"`
	InstallKey string            `json:"installKey"` // 安装密钥
	ServerAddr string            `json:"serverAddr"` // API服务地址
	RpcAddr    string            `json:"rpcAddr"`    // RPC服务地址
	RedisAddr  string            `json:"redisAddr"`  // Redis地址
	Commands   map[string]string `json:"commands"`   // 各平台安装命令
}

// WorkerRefreshKeyResp 刷新安装密钥响应
type WorkerRefreshKeyResp struct {
	Code       int    `json:"code"`
	Msg        string `json:"msg"`
	InstallKey string `json:"installKey"` // 新的安装密钥
}

// WorkerValidateKeyReq 验证安装密钥请求（Worker调用）
type WorkerValidateKeyReq struct {
	InstallKey string `json:"installKey"` // 安装密钥
	WorkerName string `json:"workerName"` // Worker名称
	WorkerIP   string `json:"workerIP"`   // Worker IP
	WorkerOS   string `json:"workerOS"`   // 操作系统
	WorkerArch string `json:"workerArch"` // 架构
}

// WorkerValidateKeyResp 验证安装密钥响应
type WorkerValidateKeyResp struct {
	Code  int    `json:"code"`
	Msg   string `json:"msg"`
	Valid bool   `json:"valid"` // 是否有效
}

// WorkerBinaryInfoResp Worker二进制文件信息响应
type WorkerBinaryInfoResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Filename string `json:"filename"` // 文件名
	OS       string `json:"os"`       // 操作系统
	Arch     string `json:"arch"`     // 架构
	Version  string `json:"version"`  // 版本
}

// ==================== 主动扫描指纹 ====================

// ActiveFingerprint 主动扫描指纹
type ActiveFingerprint struct {
	Id                  string        `json:"id"`
	Name                string        `json:"name"`        // 应用名称（用于关联被动指纹）
	Paths               []string      `json:"paths"`       // 主动探测路径列表
	Description         string        `json:"description"` // 描述
	Enabled             bool          `json:"enabled"`     // 是否启用
	CreateTime          string        `json:"createTime"`
	UpdateTime          string        `json:"updateTime"`
	RelatedFingerprints []Fingerprint `json:"relatedFingerprints"` // 关联的被动指纹列表
	RelatedCount        int           `json:"relatedCount"`        // 关联的被动指纹数量
}

// ActiveFingerprintListReq 主动指纹列表请求
type ActiveFingerprintListReq struct {
	Page     int    `json:"page,default=1"`
	PageSize int    `json:"pageSize,default=20"`
	Keyword  string `json:"keyword,optional"` // 搜索关键词
	Enabled  *bool  `json:"enabled,optional"` // 状态筛选
}

// ActiveFingerprintListResp 主动指纹列表响应
type ActiveFingerprintListResp struct {
	Code  int                 `json:"code"`
	Msg   string              `json:"msg"`
	Total int                 `json:"total"`
	List  []ActiveFingerprint `json:"list"`
	Stats map[string]int64    `json:"stats"` // 统计信息
}

// ActiveFingerprintSaveReq 保存主动指纹请求
type ActiveFingerprintSaveReq struct {
	Id          string   `json:"id,optional"`
	Name        string   `json:"name"`
	Paths       []string `json:"paths"`
	Description string   `json:"description,optional"`
	Enabled     bool     `json:"enabled"`
}

// ActiveFingerprintDeleteReq 删除主动指纹请求
type ActiveFingerprintDeleteReq struct {
	Id string `json:"id"`
}

// ActiveFingerprintImportReq 导入主动指纹请求（YAML格式）
type ActiveFingerprintImportReq struct {
	Content string `json:"content"` // YAML内容（dir.yaml格式）
}

// ActiveFingerprintImportResp 导入主动指纹响应
type ActiveFingerprintImportResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	Imported int    `json:"imported"` // 导入数量
	Updated  int    `json:"updated"`  // 更新数量
}

// ActiveFingerprintExportResp 导出主动指纹响应
type ActiveFingerprintExportResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Content string `json:"content"` // YAML内容
}

// ActiveFingerprintClearResp 清空主动指纹响应
type ActiveFingerprintClearResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Deleted int    `json:"deleted"` // 删除数量
}

// ActiveFingerprintValidateReq 验证主动指纹请求
type ActiveFingerprintValidateReq struct {
	Id  string `json:"id"`  // 主动指纹ID
	Url string `json:"url"` // 目标URL（基础URL，不含路径）
}

// ActiveFingerprintValidateResp 验证主动指纹响应
type ActiveFingerprintValidateResp struct {
	Code    int                             `json:"code"`
	Msg     string                          `json:"msg"`
	Matched bool                            `json:"matched"` // 是否匹配
	Results []ActiveFingerprintValidateItem `json:"results"` // 每个路径的验证结果
}

// ActiveFingerprintValidateItem 主动指纹验证结果项
type ActiveFingerprintValidateItem struct {
	Path           string `json:"path"`           // 探测路径
	StatusCode     int    `json:"statusCode"`     // HTTP状态码
	Matched        bool   `json:"matched"`        // 是否匹配
	MatchedRule    string `json:"matchedRule"`    // 匹配的规则名称
	MatchedDetails string `json:"matchedDetails"` // 匹配详情
}

// ==================== 目录扫描字典 ====================

// DirScanDict 目录扫描字典
type DirScanDict struct {
	Id          string `json:"id"`
	Name        string `json:"name"`        // 字典名称
	Description string `json:"description"` // 描述
	Content     string `json:"content"`     // 字典内容（每行一个路径）
	PathCount   int    `json:"pathCount"`   // 路径数量
	Enabled     bool   `json:"enabled"`     // 是否启用
	IsBuiltin   bool   `json:"isBuiltin"`   // 是否内置字典
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`
}

// DirScanDictListReq 目录扫描字典列表请求
type DirScanDictListReq struct {
	Page     int   `json:"page,default=1"`
	PageSize int   `json:"pageSize,default=20"`
	Enabled  *bool `json:"enabled,optional"` // 状态筛选
}

// DirScanDictListResp 目录扫描字典列表响应
type DirScanDictListResp struct {
	Code  int           `json:"code"`
	Msg   string        `json:"msg"`
	Total int           `json:"total"`
	List  []DirScanDict `json:"list"`
}

// DirScanDictSaveReq 保存目录扫描字典请求
type DirScanDictSaveReq struct {
	Id          string `json:"id,optional"`
	Name        string `json:"name"`
	Description string `json:"description,optional"`
	Content     string `json:"content"`
	Enabled     bool   `json:"enabled"`
}

// DirScanDictDeleteReq 删除目录扫描字典请求
type DirScanDictDeleteReq struct {
	Id string `json:"id"`
}

// DirScanDictClearResp 清空目录扫描字典响应
type DirScanDictClearResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Deleted int    `json:"deleted"` // 删除数量
}

// DirScanDictEnabledListResp 启用的目录扫描字典列表响应（用于任务创建时选择）
type DirScanDictEnabledListResp struct {
	Code int                 `json:"code"`
	Msg  string              `json:"msg"`
	List []DirScanDictSimple `json:"list"`
}

// DirScanDictSimple 简化的目录扫描字典信息（用于选择列表）
type DirScanDictSimple struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	PathCount int    `json:"pathCount"`
	IsBuiltin bool   `json:"isBuiltin"`
}

// ==================== 子域名字典 ====================

// SubdomainDict 子域名字典
type SubdomainDict struct {
	Id          string `json:"id"`
	Name        string `json:"name"`        // 字典名称
	Description string `json:"description"` // 描述
	Content     string `json:"content"`     // 字典内容（每行一个子域名前缀）
	WordCount   int    `json:"wordCount"`   // 词条数量
	Enabled     bool   `json:"enabled"`     // 是否启用
	IsBuiltin   bool   `json:"isBuiltin"`   // 是否内置字典
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`
}

// SubdomainDictListReq 子域名字典列表请求
type SubdomainDictListReq struct {
	Page     int   `json:"page,default=1"`
	PageSize int   `json:"pageSize,default=20"`
	Enabled  *bool `json:"enabled,optional"` // 状态筛选
}

// SubdomainDictListResp 子域名字典列表响应
type SubdomainDictListResp struct {
	Code  int             `json:"code"`
	Msg   string          `json:"msg"`
	Total int             `json:"total"`
	List  []SubdomainDict `json:"list"`
}

// SubdomainDictSaveReq 保存子域名字典请求
type SubdomainDictSaveReq struct {
	Id          string `json:"id,optional"`
	Name        string `json:"name"`
	Description string `json:"description,optional"`
	Content     string `json:"content"`
	Enabled     bool   `json:"enabled"`
}

// SubdomainDictDeleteReq 删除子域名字典请求
type SubdomainDictDeleteReq struct {
	Id string `json:"id"`
}

// SubdomainDictClearResp 清空子域名字典响应
type SubdomainDictClearResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Deleted int    `json:"deleted"` // 删除数量
}

// SubdomainDictEnabledListResp 启用的子域名字典列表响应（用于任务创建时选择）
type SubdomainDictEnabledListResp struct {
	Code int                   `json:"code"`
	Msg  string                `json:"msg"`
	List []SubdomainDictSimple `json:"list"`
}

// SubdomainDictSimple 简化的子域名字典信息（用于选择列表）
type SubdomainDictSimple struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	WordCount int    `json:"wordCount"`
	IsBuiltin bool   `json:"isBuiltin"`
}

// ==================== 通知配置 ====================

// NotifyConfig 通知配置
type NotifyConfig struct {
	Id              string          `json:"id"`
	Name            string          `json:"name"`            // 配置名称
	Provider        string          `json:"provider"`        // 提供者类型
	Config          string          `json:"config"`          // JSON格式的配置详情
	Status          string          `json:"status"`          // enable/disable
	MessageTemplate string          `json:"messageTemplate"` // 自定义消息模板
	WebURL          string          `json:"webUrl"`          // 前端URL，用于生成报告链接
	CreateTime      string          `json:"createTime"`
	UpdateTime      string          `json:"updateTime"`
	HighRiskFilter  *HighRiskFilter `json:"highRiskFilter,omitempty"` // 高危过滤配置
}

// HighRiskFilter 高危过滤配置
type HighRiskFilter struct {
	Enabled               bool     `json:"enabled"`               // 是否启用高危过滤，false时全部通知
	HighRiskFingerprints  []string `json:"highRiskFingerprints"`  // 高危指纹列表
	HighRiskPorts         []int    `json:"highRiskPorts"`         // 高危端口列表
	HighRiskPocSeverities []string `json:"highRiskPocSeverities"` // 高危POC严重级别: critical, high
	NewAssetNotify        bool     `json:"newAssetNotify"`        // 新资产发现时通知
}

// NotifyConfigListResp 通知配置列表响应
type NotifyConfigListResp struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	List []NotifyConfig `json:"list"`
}

// NotifyConfigSaveReq 保存通知配置请求
type NotifyConfigSaveReq struct {
	Id              string          `json:"id,optional"`
	Name            string          `json:"name,optional"`
	Provider        string          `json:"provider"`
	Config          string          `json:"config"`
	Status          string          `json:"status,optional"`
	MessageTemplate string          `json:"messageTemplate,optional"`
	WebURL          string          `json:"webUrl,optional"`         // 前端URL，用于生成报告链接
	HighRiskFilter  *HighRiskFilter `json:"highRiskFilter,optional"` // 高危过滤配置
}

// NotifyConfigDeleteReq 删除通知配置请求
type NotifyConfigDeleteReq struct {
	Id string `json:"id"`
}

// NotifyConfigTestReq 测试通知配置请求
type NotifyConfigTestReq struct {
	Provider        string `json:"provider"`
	Config          string `json:"config"`
	MessageTemplate string `json:"messageTemplate,optional"`
}

// NotifyProvider 通知提供者信息
type NotifyProvider struct {
	Id           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	ConfigFields []NotifyConfigField `json:"configFields"`
}

// NotifyConfigField 通知配置字段
type NotifyConfigField struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text, password, number, textarea, switch, select
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []string `json:"options,omitempty"` // 用于select类型
}

// NotifyProviderListResp 通知提供者列表响应
type NotifyProviderListResp struct {
	Code int              `json:"code"`
	Msg  string           `json:"msg"`
	List []NotifyProvider `json:"list"`
}

// ==================== 全局黑名单 ====================

// BlacklistConfig 黑名单配置
type BlacklistConfig struct {
	Rules      string `json:"rules"`      // 黑名单规则，每行一条
	Status     string `json:"status"`     // enable/disable
	UpdateTime string `json:"updateTime"` // 更新时间
}

// BlacklistConfigResp 黑名单配置响应
type BlacklistConfigResp struct {
	Code int              `json:"code"`
	Msg  string           `json:"msg"`
	Data *BlacklistConfig `json:"data,omitempty"`
}

// BlacklistConfigSaveReq 保存黑名单配置请求
type BlacklistConfigSaveReq struct {
	Rules  string `json:"rules"`           // 黑名单规则，每行一条
	Status string `json:"status,optional"` // enable/disable，默认enable
}

// BlacklistRulesResp 黑名单规则列表响应（供Worker使用）
type BlacklistRulesResp struct {
	Code  int      `json:"code"`
	Msg   string   `json:"msg"`
	Rules []string `json:"rules"` // 解析后的规则列表
}

// ==================== 高危过滤全局配置 ====================

// HighRiskFilterConfig 高危过滤全局配置
type HighRiskFilterConfig struct {
	Enabled               bool     `json:"enabled"`               // 是否启用高危过滤
	HighRiskFingerprints  []string `json:"highRiskFingerprints"`  // 高危指纹列表
	HighRiskPorts         []int    `json:"highRiskPorts"`         // 高危端口列表
	HighRiskPocSeverities []string `json:"highRiskPocSeverities"` // 高危POC严重级别
	NewAssetNotify        bool     `json:"newAssetNotify"`        // 新资产发现时通知
	UpdateTime            string   `json:"updateTime"`            // 更新时间
}

// HighRiskFilterConfigResp 高危过滤配置响应
type HighRiskFilterConfigResp struct {
	Code   int                   `json:"code"`
	Msg    string                `json:"msg"`
	Config *HighRiskFilterConfig `json:"config,omitempty"`
}

// HighRiskFilterConfigSaveReq 保存高危过滤配置请求
type HighRiskFilterConfigSaveReq struct {
	Enabled               bool     `json:"enabled"`
	HighRiskFingerprints  []string `json:"highRiskFingerprints,optional"`
	HighRiskPorts         []int    `json:"highRiskPorts,optional"`
	HighRiskPocSeverities []string `json:"highRiskPocSeverities,optional"`
	NewAssetNotify        bool     `json:"newAssetNotify,optional"`
}

// ==================== 资产指纹和端口统计 ====================

// AssetFingerprintsListReq 资产指纹列表请求
type AssetFingerprintsListReq struct {
	Limit int `json:"limit,default=500"`
}

// AssetFingerprintsListResp 资产指纹列表响应
type AssetFingerprintsListResp struct {
	Code int      `json:"code"`
	Msg  string   `json:"msg"`
	List []string `json:"list"`
}

// AssetPortsStatsResp 资产端口统计响应
type AssetPortsStatsResp struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	List []PortStatItem `json:"list"`
}

// PortStatItem 端口统计项
type PortStatItem struct {
	Port    int    `json:"port"`
	Service string `json:"service"`
	Count   int64  `json:"count"`
}

// ==================== 扫描配置模板 ====================

// ScanTemplate 扫描配置模板
type ScanTemplate struct {
	Id          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"` // quick/standard/full/custom
	Tags        []string `json:"tags"`
	Config      string   `json:"config"`    // 扫描配置JSON
	IsBuiltin   bool     `json:"isBuiltin"` // 是否内置模板
	UseCount    int      `json:"useCount"`  // 使用次数
	CreateTime  string   `json:"createTime"`
}

// ScanTemplateListReq 模板列表请求
type ScanTemplateListReq struct {
	Page     int      `json:"page,default=1"`
	PageSize int      `json:"pageSize,default=20"`
	Keyword  string   `json:"keyword,optional"`  // 搜索关键词
	Category string   `json:"category,optional"` // 分类筛选
	Tags     []string `json:"tags,optional"`     // 标签筛选
}

// ScanTemplateListResp 模板列表响应
type ScanTemplateListResp struct {
	Code  int            `json:"code"`
	Msg   string         `json:"msg"`
	Total int            `json:"total"`
	List  []ScanTemplate `json:"list"`
}

// ScanTemplateSaveReq 保存模板请求
type ScanTemplateSaveReq struct {
	Id          string   `json:"id,optional"`
	Name        string   `json:"name"`
	Description string   `json:"description,optional"`
	Category    string   `json:"category,optional"`
	Tags        []string `json:"tags,optional"`
	Config      string   `json:"config"`
	SortNumber  int      `json:"sortNumber,optional"`
}

// ScanTemplateDeleteReq 删除模板请求
type ScanTemplateDeleteReq struct {
	Id string `json:"id"`
}

// ScanTemplateDetailReq 模板详情请求
type ScanTemplateDetailReq struct {
	Id string `json:"id"`
}

// ScanTemplateDetailResp 模板详情响应
type ScanTemplateDetailResp struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Data *ScanTemplate `json:"data,omitempty"`
}

// ScanTemplateFromTaskReq 从任务创建模板请求
type ScanTemplateFromTaskReq struct {
	TaskId      string `json:"taskId"`               // 任务ID
	Name        string `json:"name"`                 // 模板名称
	Description string `json:"description,optional"` // 模板描述
}

// ScanTemplateCategoriesResp 模板分类响应
type ScanTemplateCategoriesResp struct {
	Code       int      `json:"code"`
	Msg        string   `json:"msg"`
	Categories []string `json:"categories"`
	Tags       []string `json:"tags"`
}

// ScanTemplateExportReq 导出模板请求
type ScanTemplateExportReq struct {
	Ids []string `json:"ids,optional"` // 指定导出的模板ID，为空则导出全部
}

// ScanTemplateExportItem 导出模板项
type ScanTemplateExportItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Config      string   `json:"config"`
}

// ScanTemplateExportResp 导出模板响应
type ScanTemplateExportResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data string `json:"data"` // JSON格式的模板数据
}

// ScanTemplateImportReq 导入模板请求
type ScanTemplateImportReq struct {
	Data         string `json:"data"`                  // JSON格式的模板数据
	SkipExisting bool   `json:"skipExisting,optional"` // 是否跳过已存在的模板
}

// ScanTemplateImportResp 导入模板响应
type ScanTemplateImportResp struct {
	Code     int      `json:"code"`
	Msg      string   `json:"msg"`
	Imported int      `json:"imported"` // 成功导入数量
	Skipped  int      `json:"skipped"`  // 跳过数量
	Errors   []string `json:"errors"`   // 错误信息
}

// ScanTemplateUseReq 使用模板请求
type ScanTemplateUseReq struct {
	Id string `json:"id"`
}

// ==================== 全局主题配置 ====================

// ThemeConfig 全局主题配置
type ThemeConfig struct {
	Theme      string `json:"theme"`      // 主题模式: light/dark/system
	ColorTheme string `json:"colorTheme"` // 颜色主题: default/pure-white/forest-green/...
	UpdateTime string `json:"updateTime,omitempty"`
}

// ThemeConfigResp 主题配置响应
type ThemeConfigResp struct {
	Code   int          `json:"code"`
	Msg    string       `json:"msg"`
	Config *ThemeConfig `json:"config,omitempty"`
}

// ThemeConfigSaveReq 保存主题配置请求
type ThemeConfigSaveReq struct {
	Theme      string `json:"theme"`
	ColorTheme string `json:"colorTheme"`
}

// ==================== 资产标签管理 ====================
type AssetUpdateLabelsReq struct {
	Id          string   `json:"id"`
	Labels      []string `json:"labels"`
	WorkspaceId string   `json:"workspaceId,optional"` // 可选，指定工作空间
}

type AssetAddLabelReq struct {
	Id          string `json:"id"`
	Label       string `json:"label"`
	WorkspaceId string `json:"workspaceId,optional"` // 可选，指定工作空间
}

type AssetRemoveLabelReq struct {
	Id          string `json:"id"`
	Label       string `json:"label"`
	WorkspaceId string `json:"workspaceId,optional"` // 可选，指定工作空间
}

// ==================== 资产过滤器选项 ====================
type AssetFilterOptionsReq struct {
	Domain        string `json:"domain,optional"`        // 域名过滤
	HasScreenshot bool   `json:"hasScreenshot,optional"` // 是否只查询有截图的资产
}

type AssetFilterOptionsResp struct {
	Code         int      `json:"code"`
	Msg          string   `json:"msg"`
	Technologies []string `json:"technologies"` // 所有技术栈选项
	Ports        []int    `json:"ports"`        // 所有端口选项
	StatusCodes  []string `json:"statusCodes"`  // 所有状态码选项
	Labels       []string `json:"labels"`       // 所有标签选项
}

// ==================== 资产暴露面 ====================
type AssetExposuresReq struct {
	AssetId string `json:"assetId"` // 资产ID
}

type DirScanResultItem struct {
	URL           string `json:"url"`
	Path          string `json:"path"`
	Status        int    `json:"status"`
	ContentLength int64  `json:"contentLength"`
	ContentType   string `json:"contentType,omitempty"`
	Title         string `json:"title,omitempty"`
	RedirectURL   string `json:"redirectUrl,omitempty"`
}

type VulnResultItem struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Severity     string  `json:"severity"`
	URL          string  `json:"url"`
	Description  string  `json:"description,omitempty"`
	CVE          string  `json:"cve,omitempty"`
	CVSS         float64 `json:"cvss,omitempty"`
	MatchedURL   string  `json:"matchedUrl,omitempty"`
	DiscoveredAt string  `json:"discoveredAt,omitempty"`
}

type AssetExposuresResp struct {
	Code           int                 `json:"code"`
	Msg            string              `json:"msg"`
	DirScanResults []DirScanResultItem `json:"dirScanResults"` // 目录扫描结果
	VulnResults    []VulnResultItem    `json:"vulnResults"`    // 漏洞扫描结果
}

// ==================== 扫描结果集成 API ====================

// AssetsWithScansReq 获取带扫描摘要的资产列表请求
type AssetsWithScansReq struct {
	WorkspaceId string `json:"workspaceId,optional"` // 工作空间ID（从中间件获取）
	Page        int    `json:"page,default=1"`
	PageSize    int    `json:"pageSize,default=20"`
	Query       string `json:"query,optional"`
	Host        string `json:"host,optional"`
	Port        int    `json:"port,optional"`
	Service     string `json:"service,optional"`
}

// AssetWithScans 带扫描摘要的资产
type AssetWithScans struct {
	Asset             Asset  `json:"asset"`
	DirScanCount      int64  `json:"dirScanCount"`
	VulnScanCount     int64  `json:"vulnScanCount"`
	HighRiskVulnCount int64  `json:"highRiskVulnCount"`
	LastScanTime      string `json:"lastScanTime,omitempty"`
}

// AssetsWithScansResp 获取带扫描摘要的资产列表响应
type AssetsWithScansResp struct {
	Code  int              `json:"code"`
	Msg   string           `json:"msg"`
	Total int64            `json:"total"`
	List  []AssetWithScans `json:"list"`
}

// AssetDirScansReq 获取资产目录扫描结果请求
type AssetDirScansReq struct {
	AssetId string `json:"assetId"`           // 资产ID
	Limit   int    `json:"limit,default=100"` // 每页数量
	Offset  int    `json:"offset,default=0"`  // 偏移量
}

// AssetDirScansResp 获取资产目录扫描结果响应
type AssetDirScansResp struct {
	Code     int                 `json:"code"`
	Msg      string              `json:"msg"`
	Total    int64               `json:"total"`
	Results  []DirScanResultItem `json:"results"`
	ScanTime string              `json:"scanTime,omitempty"`
}

// AssetVulnScansReq 获取资产漏洞扫描结果请求
type AssetVulnScansReq struct {
	AssetId string `json:"assetId"`          // 资产ID
	Limit   int    `json:"limit,default=50"` // 每页数量
	Offset  int    `json:"offset,default=0"` // 偏移量
}

// AssetVulnScansResp 获取资产漏洞扫描结果响应
type AssetVulnScansResp struct {
	Code     int              `json:"code"`
	Msg      string           `json:"msg"`
	Total    int64            `json:"total"`
	Results  []VulnResultItem `json:"results"`
	ScanTime string           `json:"scanTime,omitempty"`
}

// AssetScanHistoryReq 获取资产历史扫描版本请求
type AssetScanHistoryReq struct {
	AssetId   string `json:"assetId"`            // 资产ID
	StartTime string `json:"startTime,optional"` // 开始时间（RFC3339格式）
	EndTime   string `json:"endTime,optional"`   // 结束时间（RFC3339格式）
}

// HistoricalVersion 历史扫描版本
type HistoricalVersion struct {
	VersionId      string `json:"versionId"`
	ScanTimestamp  string `json:"scanTimestamp"`
	DirScanCount   int64  `json:"dirScanCount"`
	VulnScanCount  int64  `json:"vulnScanCount"`
	ChangesSummary string `json:"changesSummary"`
}

// AssetScanHistoryResp 获取资产历史扫描版本响应
type AssetScanHistoryResp struct {
	Code     int                 `json:"code"`
	Msg      string              `json:"msg"`
	Versions []HistoricalVersion `json:"versions"`
}

// CompareVersionsReq 比较两个历史版本请求
type CompareVersionsReq struct {
	VersionId1 string `json:"versionId1"` // 版本1 ID
	VersionId2 string `json:"versionId2"` // 版本2 ID
}

// CompareVersionsResp 比较两个历史版本响应
type CompareVersionsResp struct {
	Code             int               `json:"code"`
	Msg              string            `json:"msg"`
	Version1         HistoricalVersion `json:"version1"`
	Version2         HistoricalVersion `json:"version2"`
	DirScansAdded    int64             `json:"dirScansAdded"`
	DirScansRemoved  int64             `json:"dirScansRemoved"`
	VulnsAdded       int64             `json:"vulnsAdded"`
	VulnsRemoved     int64             `json:"vulnsRemoved"`
	ComparisonDetail string            `json:"comparisonDetail"`
}

// ==================== 任务分片管理 ====================

// ChunkProgressReq 分片进度查询请求
type ChunkProgressReq struct {
	TaskId string `json:"taskId"` // 任务ID
}

// ChunkStatus 分片状态
type ChunkStatus struct {
	ChunkId     string `json:"chunkId"`     // 分片ID
	Status      string `json:"status"`      // 状态：PENDING/STARTED/SUCCESS/FAILURE
	StartTime   string `json:"startTime"`   // 开始时间
	EndTime     string `json:"endTime"`     // 结束时间
	Duration    int64  `json:"duration"`    // 执行时长（秒）
	TargetCount int    `json:"targetCount"` // 目标数量
	AssetCount  int    `json:"assetCount"`  // 发现资产数
	VulCount    int    `json:"vulCount"`    // 发现漏洞数
	ErrorMsg    string `json:"errorMsg"`    // 错误信息
	WorkerName  string `json:"workerName"`  // 执行Worker
}

// ChunkProgressResp 分片进度查询响应
type ChunkProgressResp struct {
	Code            int           `json:"code"`
	Msg             string        `json:"msg"`
	TaskId          string        `json:"taskId"`          // 任务ID
	TotalChunks     int           `json:"totalChunks"`     // 总分片数
	CompletedChunks int           `json:"completedChunks"` // 已完成分片数
	FailedChunks    int           `json:"failedChunks"`    // 失败分片数
	RunningChunks   int           `json:"runningChunks"`   // 运行中分片数
	TotalTargets    int           `json:"totalTargets"`    // 总目标数
	CompletionRate  float64       `json:"completionRate"`  // 完成百分比
	Chunks          []ChunkStatus `json:"chunks"`          // 分片状态列表
}

// ChunkPreviewReq 分片预览请求
type ChunkPreviewReq struct {
	Target             string `json:"target"`                      // 扫描目标
	Config             string `json:"config,optional"`             // 任务配置JSON
	MaxTargetsPerChunk int    `json:"maxTargetsPerChunk,optional"` // 每个分片最大目标数
	MinChunkSize       int    `json:"minChunkSize,optional"`       // 最小分片大小
	MaxChunkSize       int    `json:"maxChunkSize,optional"`       // 最大分片大小
}

// ChunkPreviewResp 分片预览响应
type ChunkPreviewResp struct {
	Code             int     `json:"code"`
	Msg              string  `json:"msg"`
	TotalTargets     int     `json:"totalTargets"`     // 总目标数
	ChunkCount       int     `json:"chunkCount"`       // 分片数量
	ChunkSize        int     `json:"chunkSize"`        // 平均分片大小
	NeedSplit        bool    `json:"needSplit"`        // 是否需要拆分
	EstimatedTime    int     `json:"estimatedTime"`    // 预估执行时间（秒）
	RecommendedSize  int     `json:"recommendedSize"`  // 推荐分片大小
	MaxMemoryUsage   float64 `json:"maxMemoryUsage"`   // 最大内存使用量（字节）
	ParallelCapacity int     `json:"parallelCapacity"` // 并行处理能力
}

// ==================== 任务恢复统计 ====================

// TaskRecoveryStatsResp 任务恢复统计响应
type TaskRecoveryStatsResp struct {
	Code            int    `json:"code"`
	Msg             string `json:"msg"`
	ProcessingTasks int    `json:"processingTasks"` // 正在处理的任务数
	OnlineWorkers   int    `json:"onlineWorkers"`   // 在线Worker数
	TotalWorkers    int    `json:"totalWorkers"`    // 总Worker数
	CheckInterval   string `json:"checkInterval"`   // 检查间隔
	TaskTimeout     string `json:"taskTimeout"`     // 任务超时时间
}
