package common

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// TargetValidationError 目标校验错误
type TargetValidationError struct {
	Line    int    // 行号
	Target  string // 原始目标
	Message string // 错误信息
}

func (e *TargetValidationError) Error() string {
	return fmt.Sprintf("第%d行 '%s': %s", e.Line, e.Target, e.Message)
}

// ValidateTargets 校验目标列表
// 返回错误列表，如果全部有效则返回空切片
func ValidateTargets(target string) []TargetValidationError {
	var errors []TargetValidationError
	lines := strings.Split(target, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		lineNum := i + 1
		if err := validateSingleTarget(line); err != nil {
			errors = append(errors, TargetValidationError{
				Line:    lineNum,
				Target:  line,
				Message: err.Error(),
			})
		}
	}

	return errors
}

// validateSingleTarget 校验单个目标
func validateSingleTarget(target string) error {
	// 检查是否是带协议的URL格式 (http:// 或 https://)
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return validateURL(target)
	}

	// 检查是否是 IPv6 格式
	if isIPv6Format(target) {
		return validateIPv6Target(target)
	}

	// 去除可能的端口部分进行基础校验（仅针对 IPv4 和域名）
	host := target
	if idx := strings.LastIndex(target, ":"); idx != -1 {
		// 检查是否是 host:port 格式
		portStr := target[idx+1:]
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port <= 65535 {
			host = target[:idx]
		}
	}

	// CIDR 格式
	if strings.Contains(host, "/") {
		return validateCIDR(host)
	}

	// IP 范围格式
	if strings.Contains(host, "-") {
		parts := strings.Split(host, "-")
		if len(parts) == 2 && net.ParseIP(strings.TrimSpace(parts[0])) != nil {
			return validateIPRange(host)
		}
		// 可能是域名中包含连字符，继续检查域名
	}

	// 单个 IP
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}

	// 域名格式
	if isValidDomain(host) {
		return nil
	}

	return fmt.Errorf("无效的目标格式，请输入有效的IP、CIDR、IP范围、域名或URL")
}

// validateURL 校验URL格式
// 支持 http://domain.com 或 https://domain.com 以及带路径的URL
func validateURL(urlStr string) error {
	// 解析URL
	parsedURL, err := parseURL(urlStr)
	if err != nil {
		return fmt.Errorf("无效的URL格式: %v", err)
	}

	// 只支持 http 和 https 协议
	if parsedURL.scheme != "http" && parsedURL.scheme != "https" {
		return fmt.Errorf("仅支持 http:// 或 https:// 协议")
	}

	// 获取主机名
	hostname := parsedURL.host
	if hostname == "" {
		return fmt.Errorf("URL缺少主机名")
	}

	// 去除端口部分
	if idx := strings.LastIndex(hostname, ":"); idx != -1 {
		// 检查是否是 host:port 格式
		portStr := hostname[idx+1:]
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port <= 65535 {
			hostname = hostname[:idx]
		}
	}

	// 处理 IPv6 地址（被方括号包裹）
	if strings.HasPrefix(hostname, "[") && strings.HasSuffix(hostname, "]") {
		ipv6 := hostname[1 : len(hostname)-1]
		if ip := net.ParseIP(ipv6); ip != nil {
			return nil
		}
		return fmt.Errorf("无效的IPv6地址: %s", hostname)
	}

	// 检查是否是IPv4
	if ip := net.ParseIP(hostname); ip != nil {
		return nil
	}

	// 检查是否是有效域名
	if isValidDomain(hostname) {
		return nil
	}

	// 也允许 localhost
	if hostname == "localhost" {
		return nil
	}

	return fmt.Errorf("无效的主机名: %s", hostname)
}

// parsedURL 简单的URL解析结果
type parsedURL struct {
	scheme string
	host   string
	path   string
}

// parseURL 简单的URL解析函数
func parseURL(urlStr string) (*parsedURL, error) {
	result := &parsedURL{}

	// 解析协议
	if strings.HasPrefix(urlStr, "https://") {
		result.scheme = "https"
		urlStr = urlStr[8:]
	} else if strings.HasPrefix(urlStr, "http://") {
		result.scheme = "http"
		urlStr = urlStr[7:]
	} else {
		return nil, fmt.Errorf("不支持的协议")
	}

	// 解析主机和路径
	slashIdx := strings.Index(urlStr, "/")
	if slashIdx == -1 {
		result.host = urlStr
		result.path = ""
	} else {
		result.host = urlStr[:slashIdx]
		result.path = urlStr[slashIdx:]
	}

	if result.host == "" {
		return nil, fmt.Errorf("缺少主机名")
	}

	return result, nil
}

// isIPv6Format 检查是否是 IPv6 格式
func isIPv6Format(target string) bool {
	// [IPv6]:port 格式
	if strings.HasPrefix(target, "[") {
		return true
	}
	// 包含 Zone ID 的 IPv6
	if strings.Contains(target, "%") {
		return true
	}
	// 统计冒号数量，IPv6 至少有2个冒号
	colonCount := strings.Count(target, ":")
	return colonCount >= 2
}

// validateIPv6Target 校验 IPv6 目标
func validateIPv6Target(target string) error {
	ipv6 := target

	// 处理 [IPv6]:port 格式
	if strings.HasPrefix(target, "[") {
		closeBracket := strings.Index(target, "]")
		if closeBracket == -1 {
			return fmt.Errorf("无效的IPv6格式，缺少闭合括号 ]")
		}
		ipv6 = target[1:closeBracket]
		// 检查端口部分
		remaining := target[closeBracket+1:]
		if remaining != "" && !strings.HasPrefix(remaining, ":") {
			return fmt.Errorf("无效的IPv6格式")
		}
		if strings.HasPrefix(remaining, ":") {
			portStr := remaining[1:]
			port, err := strconv.Atoi(portStr)
			if err != nil || port <= 0 || port > 65535 {
				return fmt.Errorf("无效的端口号: %s", portStr)
			}
		}
	}

	// 去除 Zone ID（如 %eth0 或 %5）
	if zoneIndex := strings.Index(ipv6, "%"); zoneIndex != -1 {
		ipv6 = ipv6[:zoneIndex]
	}

	// 处理 IPv6 CIDR
	if strings.Contains(ipv6, "/") {
		parts := strings.Split(ipv6, "/")
		if len(parts) != 2 {
			return fmt.Errorf("无效的IPv6 CIDR格式")
		}
		mask, err := strconv.Atoi(parts[1])
		if err != nil || mask < 0 || mask > 128 {
			return fmt.Errorf("无效的IPv6子网掩码: %s", parts[1])
		}
		ipv6 = parts[0]
	}

	// 使用 Go 标准库验证 IPv6
	if ip := net.ParseIP(ipv6); ip != nil {
		return nil
	}

	return fmt.Errorf("无效的IPv6地址格式")
}

// validateCIDR 校验 CIDR 格式
func validateCIDR(cidr string) error {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return fmt.Errorf("无效的CIDR格式")
	}

	ipPart := parts[0]
	maskPart := parts[1]

	// 检查掩码是否有效
	mask, err := strconv.Atoi(maskPart)
	if err != nil || mask < 0 || mask > 32 {
		return fmt.Errorf("无效的子网掩码: %s", maskPart)
	}

	// 检查 IP 部分是否完整
	octets := strings.Split(ipPart, ".")
	if len(octets) != 4 {
		// 提供修复建议
		suggestion := suggestCIDRFix(ipPart, maskPart)
		return fmt.Errorf("IP地址不完整，缺少%d个八位组。正确格式示例: %s", 4-len(octets), suggestion)
	}

	// 验证每个八位组
	for i, octet := range octets {
		val, err := strconv.Atoi(octet)
		if err != nil || val < 0 || val > 255 {
			return fmt.Errorf("第%d个八位组 '%s' 无效，应为0-255之间的数字", i+1, octet)
		}
	}

	// 使用 Go 标准库验证
	_, _, err = net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("无效的CIDR格式: %v", err)
	}

	return nil
}

// suggestCIDRFix 提供 CIDR 修复建议
func suggestCIDRFix(ipPart, maskPart string) string {
	octets := strings.Split(ipPart, ".")
	for len(octets) < 4 {
		octets = append(octets, "0")
	}
	return strings.Join(octets, ".") + "/" + maskPart
}

// validateIPRange 校验 IP 范围格式
func validateIPRange(ipRange string) error {
	parts := strings.Split(ipRange, "-")
	if len(parts) != 2 {
		return fmt.Errorf("无效的IP范围格式")
	}

	startIP := net.ParseIP(strings.TrimSpace(parts[0]))
	endIP := net.ParseIP(strings.TrimSpace(parts[1]))

	if startIP == nil {
		return fmt.Errorf("起始IP '%s' 无效", parts[0])
	}
	if endIP == nil {
		return fmt.Errorf("结束IP '%s' 无效", parts[1])
	}

	// 检查起始IP是否小于等于结束IP
	start := startIP.To4()
	end := endIP.To4()
	if start == nil || end == nil {
		return fmt.Errorf("仅支持IPv4范围")
	}

	for i := 0; i < 4; i++ {
		if start[i] > end[i] {
			return fmt.Errorf("起始IP不能大于结束IP")
		}
		if start[i] < end[i] {
			break
		}
	}

	return nil
}

// isValidDomain 检查是否是有效的域名
func isValidDomain(domain string) bool {
	// 简单的域名正则校验
	// 允许: example.com, sub.example.com, example-site.com
	domainRegex := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	return domainRegex.MatchString(domain)
}

// FormatValidationErrors 格式化校验错误为用户友好的消息
func FormatValidationErrors(errors []TargetValidationError) string {
	if len(errors) == 0 {
		return ""
	}

	var messages []string
	for _, e := range errors {
		messages = append(messages, e.Error())
	}

	if len(errors) == 1 {
		return "目标格式错误: " + messages[0]
	}

	return fmt.Sprintf("发现%d个目标格式错误:\n%s", len(errors), strings.Join(messages, "\n"))
}
