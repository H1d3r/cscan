package utils

import (
	"math/rand"
	"net"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// IsIPAddress 判断是否为IP地址
func IsIPAddress(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil
}

// GetRootDomain 获取根域名
// 使用 publicsuffix 正确处理多级TLD（如 .com.cn, .co.uk 等）
// 例如: test.com.cn -> test.com.cn, api.test.com.cn -> test.com.cn
func GetRootDomain(domain string) string {
	// 清理域名
	domain = strings.TrimSpace(domain)
	domain = strings.ToLower(domain)

	// 如果是IP地址，直接返回
	if IsIPAddress(domain) {
		return domain
	}

	// 使用 publicsuffix 获取有效TLD+1（即根域名）
	// EffectiveTLDPlusOne 返回公共后缀加上一级，例如：
	// - test.com.cn -> test.com.cn (com.cn 是公共后缀)
	// - api.test.com.cn -> test.com.cn
	// - example.com -> example.com (com 是公共后缀)
	// - sub.example.com -> example.com
	rootDomain, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		// 解析失败时回退到简单逻辑
		parts := strings.Split(domain, ".")
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "." + parts[len(parts)-1]
		}
		return domain
	}

	return rootDomain
}

// IsValidDomain 检查是否是有效的域名
func IsValidDomain(domain string) bool {
	domainRegex := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	return domainRegex.MatchString(domain)
}

// UniqueStrings 去重字符串切片
func UniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// RandomInt 生成指定范围内的随机整数 [min, max]
func RandomInt(min, max int) int {
	rand.Seed(time.Now().UnixNano())
	return rand.Intn(max-min+1) + min
}

// ==================== 目标解析工具函数 ====================

// IsSubdomain 判断是否为子域名（非根域名）
// 例如: api.example.com 返回 true, example.com 返回 false
func IsSubdomain(domain string) bool {
	// 移除可能的协议前缀
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")

	// 移除端口
	if idx := strings.Index(domain, ":"); idx > 0 {
		domain = domain[:idx]
	}

	// 移除路径
	if idx := strings.Index(domain, "/"); idx > 0 {
		domain = domain[:idx]
	}

	// 如果是IP地址，不是子域名
	if IsIPAddress(domain) {
		return false
	}

	parts := strings.Split(domain, ".")
	// 子域名至少有3个部分（如 sub.example.com）
	return len(parts) > 2
}

// ParseTargetInfo 解析目标信息
type TargetInfo struct {
	Raw         string // 原始输入
	Host        string // 主机（不含端口）
	Port        int    // 端口（0表示未指定）
	Path        string // URL路径（如 /admin/）
	IsIP        bool   // 是否为IP地址
	IsDomain    bool   // 是否为域名
	IsSubdomain bool   // 是否为子域名
	HasPort     bool   // 是否指定了端口
	Protocol    string // 协议（http/https，空表示未指定）
}

// ParseTarget 解析单个目标，提取主机、端口、协议、路径等信息
// 支持格式：http://example.com/admin/, example.com:8080/path, example.com
func ParseTarget(target string) *TargetInfo {
	target = strings.TrimSpace(target)
	info := &TargetInfo{Raw: target}

	// 解析协议
	if strings.HasPrefix(target, "https://") {
		info.Protocol = "https"
		target = strings.TrimPrefix(target, "https://")
	} else if strings.HasPrefix(target, "http://") {
		info.Protocol = "http"
		target = strings.TrimPrefix(target, "http://")
	}

	// 提取路径（保留路径信息）
	if idx := strings.Index(target, "/"); idx > 0 {
		info.Path = target[idx:] // 保留完整路径，如 /admin/login
		target = target[:idx]
	}

	// 解析端口
	if idx := strings.LastIndex(target, ":"); idx > 0 {
		portStr := target[idx+1:]
		if port := parsePort(portStr); port > 0 {
			info.Port = port
			info.HasPort = true
			target = target[:idx]
		}
	}

	info.Host = target

	// 判断类型
	if IsIPAddress(target) {
		info.IsIP = true
	} else if IsValidDomain(target) {
		info.IsDomain = true
		info.IsSubdomain = IsSubdomain(target)
	}

	return info
}

// parsePort 解析端口号
func parsePort(s string) int {
	port := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		port = port*10 + int(c-'0')
	}
	if port > 0 && port <= 65535 {
		return port
	}
	return 0
}

// ParseTargetsWithPorts 解析多个目标，分离带端口和不带端口的目标
// 返回: (带端口的目标列表, 不带端口的目标列表)
func ParseTargetsWithPorts(targets string) (withPort []string, withoutPort []string) {
	lines := strings.Split(targets, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		info := ParseTarget(line)
		if info.HasPort || info.Protocol != "" {
			// 带端口或协议的目标
			withPort = append(withPort, line)
		} else {
			// 不带端口的目标
			withoutPort = append(withoutPort, line)
		}
	}
	return
}

// BuildTargetWithPort 构建带端口的目标字符串
func BuildTargetWithPort(host string, port int) string {
	if port > 0 {
		return host + ":" + portToString(port)
	}
	return host
}

// portToString 端口转字符串
func portToString(port int) string {
	if port <= 0 {
		return ""
	}
	result := ""
	for port > 0 {
		result = string(rune('0'+port%10)) + result
		port /= 10
	}
	return result
}
