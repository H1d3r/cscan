package brute

import (
	"context"
)

// BruteResult 爆破结果
type BruteResult struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Service   string `json:"service"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	ExtraInfo string `json:"extraInfo,omitempty"` // 附加信息（如 Oracle 服务名）

	// 修复 H-9：结构化错误分类，区分认证失败与网络/协议错误
	// - 空值：凭据验证完成，Success 字段可信
	// - "network"：网络错误（超时/拒绝连接/DNS 失败）
	// - "protocol"：协议错误（握手失败/解析错误/服务非预期类型）
	// - "auth_reject"：明确认证拒绝（凭据无效）
	ErrorType string `json:"errorType,omitempty"`
}

// BrutePlugin 爆破插件接口
type BrutePlugin interface {
	// Name 返回插件名称
	Name() string
	// DefaultPort 返回默认端口
	DefaultPort() int
	// Probe 检测服务是否可用
	Probe(ctx context.Context, host string, port int) bool
	// Brute 执行爆破
	Brute(ctx context.Context, host string, port int, usernames, passwords []string, timeout int) *BruteResult
}

// BrutePluginFunc 函数类型的插件适配器
type BrutePluginFunc func(ctx context.Context, host string, port int, usernames, passwords []string, timeout int) *BruteResult

// BruteOptions 爆破选项
type BruteOptions struct {
	Host      string
	Port      int
	Usernames []string
	Passwords []string
	Timeout   int
	OnResult  func(*BruteResult)
}

// KnownServices 已知服务列表
var KnownServices = []string{
	"ssh",
	"mysql",
	"redis",
	"mongodb",
	"postgresql",
	"mssql",
	"ftp",
	"oracle",
	"smb",
	"mqtt",
}

// ServicePortMap 服务端口映射
var ServicePortMap = map[string]int{
	"ssh":        22,
	"mysql":      3306,
	"redis":      6379,
	"mongodb":    27017,
	"postgresql": 5432,
	"mssql":      1433,
	"ftp":        21,
	"oracle":     1521,
	"smb":        445,
	"mqtt":       1883,
}

// NormalizeServiceName 标准化服务名称
// 将 nmap 识别的各种服务名变体映射到统一的插件名称
// 参考 nmap-service-probes 和 nmap-services 中的服务名
func NormalizeServiceName(service string) string {
	service = toLower(service)
	switch service {
	// SSH - nmap: ssh, sshd
	case "ssh", "sshd":
		return "ssh"

	// MySQL - nmap: mysql, mariadb; mysql-proxy 也能用 mysql 协议爆破
	case "mysql", "mariadb", "mysql-proxy":
		return "mysql"

	// PostgreSQL - nmap: postgresql, postgres
	case "postgresql", "postgres":
		return "postgresql"

	// MongoDB - nmap: mongodb, mongo
	case "mongodb", "mongo":
		return "mongodb"

	// MSSQL - nmap: ms-sql-s (最常见), ms-sql, mssql, ms-sql-server
	case "ms-sql-s", "ms-sql", "mssql", "ms-sql-server":
		return "mssql"

	// FTP - nmap: ftp; vsftpd/proftpd/pure-ftpd 等是 nmap product 而非 service name
	case "ftp", "ftps", "tftp":
		return "ftp"

	// SMB - nmap: microsoft-ds (445端口), netbios-ssn (139端口), smb
	case "smb", "samba", "microsoft-ds", "netbios-ssn":
		return "smb"

	// Oracle - nmap: oracle-tns (最常见), oracle, oracle-db; lsnr/oracle-oms 是监听器/管理端口
	case "oracle", "oracle-db", "oracle-tns", "lsnr", "oracle-oms":
		return "oracle"

	// Redis - nmap: redis
	case "redis":
		return "redis"

	// MQTT - nmap: mqtt; mosquito 是常见 broker 的 product 名
	case "mqtt", "mosquito":
		return "mqtt"

	default:
		return service
	}
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}
