package utils

import (
	"net"
	"strings"
)

// BlacklistMatcher 黑名单匹配器
type BlacklistMatcher struct {
	domainPatterns []string     // 域名模式（支持通配符）
	ipAddresses    []net.IP     // 单个IP地址
	ipNetworks     []*net.IPNet // IP网段（CIDR）
	keywords       []string     // 关键词
}

// NewBlacklistMatcher 创建黑名单匹配器
func NewBlacklistMatcher(rules []string) *BlacklistMatcher {
	m := &BlacklistMatcher{}
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" || strings.HasPrefix(rule, "#") {
			continue
		}
		m.addRule(rule)
	}
	return m
}

// addRule 添加规则
func (m *BlacklistMatcher) addRule(rule string) {
	// 检查是否为CIDR格式
	if strings.Contains(rule, "/") {
		_, ipNet, err := net.ParseCIDR(rule)
		if err == nil {
			m.ipNetworks = append(m.ipNetworks, ipNet)
			return
		}
	}

	// 检查是否为IP地址
	if ip := net.ParseIP(rule); ip != nil {
		m.ipAddresses = append(m.ipAddresses, ip)
		return
	}

	// 检查是否为域名模式（包含通配符或看起来像域名）
	if strings.Contains(rule, "*") || strings.Contains(rule, ".") {
		m.domainPatterns = append(m.domainPatterns, strings.ToLower(rule))
		return
	}

	// 其他作为关键词处理
	m.keywords = append(m.keywords, strings.ToLower(rule))
}

// IsBlacklisted 检查目标是否在黑名单中
// target 可以是域名、IP地址或URL
func (m *BlacklistMatcher) IsBlacklisted(target string) bool {
	if target == "" {
		return false
	}

	target = strings.TrimSpace(target)
	targetLower := strings.ToLower(target)

	// 提取主机名（处理URL格式）
	host := extractHost(target)
	hostLower := strings.ToLower(host)

	// 检查IP地址
	if ip := net.ParseIP(host); ip != nil {
		if m.matchIP(ip) {
			return true
		}
	}

	// 检查域名模式
	if m.matchDomain(hostLower) {
		return true
	}

	// 检查关键词
	for _, keyword := range m.keywords {
		if strings.Contains(targetLower, keyword) {
			return true
		}
	}

	return false
}

// IsIPBlacklisted 检查IP是否在黑名单中
func (m *BlacklistMatcher) IsIPBlacklisted(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return m.matchIP(parsedIP)
}

// IsDomainBlacklisted 检查域名是否在黑名单中
func (m *BlacklistMatcher) IsDomainBlacklisted(domain string) bool {
	return m.matchDomain(strings.ToLower(domain))
}

// matchIP 匹配IP地址
func (m *BlacklistMatcher) matchIP(ip net.IP) bool {
	// 检查单个IP
	for _, blackIP := range m.ipAddresses {
		if blackIP.Equal(ip) {
			return true
		}
	}

	// 检查IP网段
	for _, ipNet := range m.ipNetworks {
		if ipNet.Contains(ip) {
			return true
		}
	}

	return false
}

// matchDomain 匹配域名
func (m *BlacklistMatcher) matchDomain(domain string) bool {
	for _, pattern := range m.domainPatterns {
		if matchWildcard(pattern, domain) {
			return true
		}
	}
	return false
}

// matchWildcard 通配符匹配
// 支持 * 匹配任意字符，*.example.com 匹配所有子域名
func matchWildcard(pattern, target string) bool {
	// 精确匹配
	if pattern == target {
		return true
	}

	// 处理 *.domain.com 格式
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // 保留 .domain.com
		// 匹配 .domain.com 结尾，或者精确匹配 domain.com
		if strings.HasSuffix(target, suffix) {
			return true
		}
		// 精确匹配去掉 *. 后的部分
		if target == pattern[2:] {
			return true
		}
	}

	// 处理 *keyword* 格式
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		keyword := pattern[1 : len(pattern)-1]
		if strings.Contains(target, keyword) {
			return true
		}
	}

	// 处理 keyword* 格式（前缀匹配）
	if strings.HasSuffix(pattern, "*") && !strings.HasPrefix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}

	// 处理 *keyword 格式（后缀匹配）
	if strings.HasPrefix(pattern, "*") && !strings.HasSuffix(pattern, "*") {
		suffix := pattern[1:]
		if strings.HasSuffix(target, suffix) {
			return true
		}
	}

	return false
}

// extractHost 从URL或目标中提取主机名
func extractHost(target string) string {
	// 移除协议
	if idx := strings.Index(target, "://"); idx != -1 {
		target = target[idx+3:]
	}

	// 移除路径
	if idx := strings.Index(target, "/"); idx != -1 {
		target = target[:idx]
	}

	// 移除端口（但保留IPv6地址）
	if strings.HasPrefix(target, "[") {
		// IPv6格式 [::1]:8080
		if idx := strings.LastIndex(target, "]:"); idx != -1 {
			target = target[1:idx]
		} else if strings.HasSuffix(target, "]") {
			target = target[1 : len(target)-1]
		}
	} else {
		// IPv4或域名格式
		if idx := strings.LastIndex(target, ":"); idx != -1 {
			// 确保不是IPv6地址
			if !strings.Contains(target[:idx], ":") {
				target = target[:idx]
			}
		}
	}

	return target
}

// FilterTargets 过滤目标列表，返回不在黑名单中的目标
func (m *BlacklistMatcher) FilterTargets(targets []string) []string {
	var filtered []string
	for _, target := range targets {
		if !m.IsBlacklisted(target) {
			filtered = append(filtered, target)
		}
	}
	return filtered
}

// GetBlacklistedTargets 获取在黑名单中的目标
func (m *BlacklistMatcher) GetBlacklistedTargets(targets []string) []string {
	var blacklisted []string
	for _, target := range targets {
		if m.IsBlacklisted(target) {
			blacklisted = append(blacklisted, target)
		}
	}
	return blacklisted
}

// IsEmpty 检查黑名单是否为空
func (m *BlacklistMatcher) IsEmpty() bool {
	return len(m.domainPatterns) == 0 &&
		len(m.ipAddresses) == 0 &&
		len(m.ipNetworks) == 0 &&
		len(m.keywords) == 0
}

// RuleCount 返回规则数量
func (m *BlacklistMatcher) RuleCount() int {
	return len(m.domainPatterns) + len(m.ipAddresses) + len(m.ipNetworks) + len(m.keywords)
}

// NewExcludeHostsMatcher 从逗号分隔的排除目标字符串创建匹配器
// 支持 IP 地址和 CIDR 格式，如 "192.168.1.1,10.0.0.0/8"
func NewExcludeHostsMatcher(excludeHosts string) *BlacklistMatcher {
	if excludeHosts == "" {
		return nil
	}

	var rules []string
	parts := strings.Split(excludeHosts, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			rules = append(rules, part)
		}
	}

	if len(rules) == 0 {
		return nil
	}

	return NewBlacklistMatcher(rules)
}

// MergeMatchers 合并多个匹配器的规则
// 返回一个新的匹配器，包含所有输入匹配器的规则
func MergeMatchers(matchers ...*BlacklistMatcher) *BlacklistMatcher {
	merged := &BlacklistMatcher{}

	for _, m := range matchers {
		if m == nil {
			continue
		}
		merged.domainPatterns = append(merged.domainPatterns, m.domainPatterns...)
		merged.ipAddresses = append(merged.ipAddresses, m.ipAddresses...)
		merged.ipNetworks = append(merged.ipNetworks, m.ipNetworks...)
		merged.keywords = append(merged.keywords, m.keywords...)
	}

	return merged
}

// FilterAssetsByIP 根据IP过滤资产列表
// 检查资产的所有IPv4地址，如果任一IP在黑名单中则过滤该资产
func (m *BlacklistMatcher) FilterAssetsByIP(hosts []string, ipv4Map map[string][]string) []string {
	if m == nil || m.IsEmpty() {
		return hosts
	}

	var filtered []string
	for _, host := range hosts {
		// 检查主机名/域名本身
		if m.IsBlacklisted(host) {
			continue
		}

		// 检查该主机解析出的所有IP
		if ips, ok := ipv4Map[host]; ok {
			isBlacklisted := false
			for _, ip := range ips {
				if m.IsIPBlacklisted(ip) {
					isBlacklisted = true
					break
				}
			}
			if isBlacklisted {
				continue
			}
		}

		filtered = append(filtered, host)
	}

	return filtered
}
