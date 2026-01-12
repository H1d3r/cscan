package scanner

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// TargetType 目标类型
type TargetType string

const (
	TargetTypeIPv4   TargetType = "ipv4"
	TargetTypeIPv6   TargetType = "ipv6"
	TargetTypeCIDR   TargetType = "cidr"
	TargetTypeRange  TargetType = "range"
	TargetTypeDomain TargetType = "domain"
	TargetTypeURL    TargetType = "url"
)

// Target 解析后的目标
type Target struct {
	Raw      string     // 原始输入
	Type     TargetType // 目标类型
	Host     string     // 主机（IP或域名）
	Port     int        // 端口（如果有）
	IPs      []string   // 展开后的IP列表（用于CIDR和Range）
	Protocol string     // 协议（http/https）
}

// TargetParser 目标解析器
// 统一处理各种格式的扫描目标，消除各扫描器中的重复解析逻辑
type TargetParser struct {
	domainRegex *regexp.Regexp
	urlRegex    *regexp.Regexp
}

// NewTargetParser 创建目标解析器
func NewTargetParser() *TargetParser {
	return &TargetParser{
		domainRegex: regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`),
		urlRegex:    regexp.MustCompile(`^(https?://)?([^/:]+)(:\d+)?(/.*)?$`),
	}
}

// Parse 解析单个目标
func (p *TargetParser) Parse(raw string) *Target {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return nil
	}

	target := &Target{Raw: raw}

	// URL格式
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return p.parseURL(raw)
	}

	// CIDR格式
	if strings.Contains(raw, "/") {
		return p.parseCIDR(raw)
	}

	// IP范围格式 (192.168.1.1-100)
	if strings.Contains(raw, "-") && p.looksLikeIPRange(raw) {
		return p.parseIPRange(raw)
	}

	// IP:Port 格式
	if host, port, ok := p.parseHostPort(raw); ok {
		target.Host = host
		target.Port = port
		target.Type = p.detectHostType(host)
		return target
	}

	// 单个IP或域名
	target.Host = raw
	target.Type = p.detectHostType(raw)
	return target
}

// ParseMultiple 解析多个目标（换行分隔）
func (p *TargetParser) ParseMultiple(input string) []*Target {
	var targets []*Target
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		if t := p.Parse(line); t != nil {
			targets = append(targets, t)
		}
	}
	return targets
}

// ExpandAll 展开所有目标为单个IP/域名列表
func (p *TargetParser) ExpandAll(input string) []string {
	targets := p.ParseMultiple(input)
	var result []string
	seen := make(map[string]bool)

	for _, t := range targets {
		hosts := t.Expand()
		for _, h := range hosts {
			if !seen[h] {
				seen[h] = true
				result = append(result, h)
			}
		}
	}
	return result
}

// Expand 展开目标为单个IP/域名列表
func (t *Target) Expand() []string {
	if len(t.IPs) > 0 {
		return t.IPs
	}
	if t.Host != "" {
		if t.Port > 0 {
			return []string{fmt.Sprintf("%s:%d", t.Host, t.Port)}
		}
		return []string{t.Host}
	}
	return nil
}

// parseURL 解析URL
func (p *TargetParser) parseURL(raw string) *Target {
	target := &Target{Raw: raw, Type: TargetTypeURL}

	if strings.HasPrefix(raw, "https://") {
		target.Protocol = "https"
		raw = strings.TrimPrefix(raw, "https://")
	} else {
		target.Protocol = "http"
		raw = strings.TrimPrefix(raw, "http://")
	}

	// 分离路径
	if idx := strings.Index(raw, "/"); idx > 0 {
		raw = raw[:idx]
	}

	// 分离端口
	if host, port, ok := p.parseHostPort(raw); ok {
		target.Host = host
		target.Port = port
	} else {
		target.Host = raw
		if target.Protocol == "https" {
			target.Port = 443
		} else {
			target.Port = 80
		}
	}

	return target
}

// parseCIDR 解析CIDR
func (p *TargetParser) parseCIDR(raw string) *Target {
	target := &Target{Raw: raw, Type: TargetTypeCIDR}

	_, ipnet, err := net.ParseCIDR(raw)
	if err != nil {
		// 解析失败，当作普通目标
		target.Host = raw
		target.Type = p.detectHostType(raw)
		return target
	}

	// 展开CIDR
	var ips []string
	for ip := ipnet.IP.Mask(ipnet.Mask); ipnet.Contains(ip); incIPLocal(ip) {
		ips = append(ips, ip.String())
	}

	// 移除网络地址和广播地址
	if len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	}

	target.IPs = ips
	return target
}

// parseIPRange 解析IP范围
func (p *TargetParser) parseIPRange(raw string) *Target {
	target := &Target{Raw: raw, Type: TargetTypeRange}

	parts := strings.Split(raw, "-")
	if len(parts) != 2 {
		target.Host = raw
		return target
	}

	startIP := net.ParseIP(strings.TrimSpace(parts[0]))
	endPart := strings.TrimSpace(parts[1])

	// 支持两种格式: 192.168.1.1-192.168.1.100 或 192.168.1.1-100
	var endIP net.IP
	if net.ParseIP(endPart) != nil {
		endIP = net.ParseIP(endPart)
	} else {
		// 短格式: 只有最后一段
		if startIP == nil {
			target.Host = raw
			return target
		}
		startParts := strings.Split(parts[0], ".")
		if len(startParts) != 4 {
			target.Host = raw
			return target
		}
		endIP = net.ParseIP(fmt.Sprintf("%s.%s.%s.%s",
			startParts[0], startParts[1], startParts[2], endPart))
	}

	if startIP == nil || endIP == nil {
		target.Host = raw
		return target
	}

	// 展开IP范围
	var ips []string
	ip := make(net.IP, len(startIP))
	copy(ip, startIP)
	for ; !ip.Equal(endIP); incIPLocal(ip) {
		ips = append(ips, ip.String())
	}
	ips = append(ips, endIP.String())

	target.IPs = ips
	return target
}

// parseHostPort 解析 host:port 格式
func (p *TargetParser) parseHostPort(raw string) (host string, port int, ok bool) {
	// IPv6 格式: [::1]:8080
	if strings.HasPrefix(raw, "[") {
		if idx := strings.LastIndex(raw, "]:"); idx > 0 {
			host = raw[1:idx]
			portStr := raw[idx+2:]
			if p, err := strconv.Atoi(portStr); err == nil {
				return host, p, true
			}
		}
		return "", 0, false
	}

	// IPv4/域名格式: host:port
	if idx := strings.LastIndex(raw, ":"); idx > 0 {
		host = raw[:idx]
		portStr := raw[idx+1:]
		if p, err := strconv.Atoi(portStr); err == nil {
			return host, p, true
		}
	}

	return "", 0, false
}

// detectHostType 检测主机类型
func (p *TargetParser) detectHostType(host string) TargetType {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return TargetTypeIPv4
		}
		return TargetTypeIPv6
	}
	return TargetTypeDomain
}

// looksLikeIPRange 判断是否像IP范围（而不是带连字符的域名）
func (p *TargetParser) looksLikeIPRange(raw string) bool {
	parts := strings.Split(raw, "-")
	if len(parts) != 2 {
		return false
	}
	// 第一部分必须是IP
	return net.ParseIP(strings.TrimSpace(parts[0])) != nil
}

// incIPLocal IP自增（本地使用，避免与utils.go冲突）
func incIPLocal(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// ==================== 端口解析 ====================

// PortParser 端口解析器
type PortParser struct{}

// NewPortParser 创建端口解析器
func NewPortParser() *PortParser {
	return &PortParser{}
}

// Parse 解析端口字符串
// 支持格式: "80", "80,443", "80-100", "80,443,8080-8090"
func (p *PortParser) Parse(portStr string) []int {
	if portStr == "" {
		return nil
	}

	var ports []int
	seen := make(map[int]bool)

	parts := strings.Split(portStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// 范围格式: 80-100
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
				if err1 == nil && err2 == nil && start <= end {
					for port := start; port <= end; port++ {
						if port > 0 && port <= 65535 && !seen[port] {
							seen[port] = true
							ports = append(ports, port)
						}
					}
				}
			}
			continue
		}

		// 单个端口
		if port, err := strconv.Atoi(part); err == nil {
			if port > 0 && port <= 65535 && !seen[port] {
				seen[port] = true
				ports = append(ports, port)
			}
		}
	}

	return ports
}

// ==================== 辅助函数 ====================

// ParseTargetsNew 解析目标（新版本，使用 TargetParser）
func ParseTargetsNew(target string) []string {
	parser := NewTargetParser()
	return parser.ExpandAll(target)
}

// ParsePortsNew 解析端口（新版本，使用 PortParser）
func ParsePortsNew(portStr string) []int {
	parser := NewPortParser()
	return parser.Parse(portStr)
}

// GetCategoryNew 获取目标分类（新版本）
func GetCategoryNew(host string) string {
	parser := NewTargetParser()
	return string(parser.detectHostType(host))
}
