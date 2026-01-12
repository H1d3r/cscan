package utils

import (
	"net"
	"strings"
)

// IPUtils IP工具集
// 提供IP地址相关的通用操作

// IsPrivateIP 判断是否为私有IP
func IsPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// 私有IP范围
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16", // Link-local
		"fc00::/7",       // IPv6 ULA
		"fe80::/10",      // IPv6 Link-local
		"::1/128",        // IPv6 Loopback
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}

// IsPublicIP 判断是否为公网IP
func IsPublicIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return !IsPrivateIP(ip) && !parsedIP.IsLoopback() && !parsedIP.IsUnspecified()
}

// IsIPv4 判断是否为IPv4地址
func IsIPv4(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() != nil
}

// IsIPv6 判断是否为IPv6地址
func IsIPv6(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() == nil
}

// NormalizeIP 标准化IP地址格式
func NormalizeIP(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ip
	}
	return parsedIP.String()
}

// IPToUint32 将IPv4转换为uint32（用于排序和范围计算）
func IPToUint32(ip string) uint32 {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return 0
	}
	ip4 := parsedIP.To4()
	if ip4 == nil {
		return 0
	}
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}

// Uint32ToIP 将uint32转换为IPv4字符串
func Uint32ToIP(n uint32) string {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)).String()
}

// GetLocalIP 获取本机IP地址
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

// ExtractHostFromURL 从URL中提取主机名
func ExtractHostFromURL(url string) string {
	// 移除协议
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")

	// 移除路径
	if idx := strings.Index(url, "/"); idx > 0 {
		url = url[:idx]
	}

	// 移除端口
	if idx := strings.LastIndex(url, ":"); idx > 0 {
		// 确保不是IPv6地址
		if !strings.Contains(url, "[") {
			url = url[:idx]
		}
	}

	return url
}

// ExtractPortFromURL 从URL中提取端口
func ExtractPortFromURL(url string) int {
	// 移除协议
	isHTTPS := strings.HasPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")

	// 移除路径
	if idx := strings.Index(url, "/"); idx > 0 {
		url = url[:idx]
	}

	// 提取端口
	if idx := strings.LastIndex(url, ":"); idx > 0 {
		// 确保不是IPv6地址
		if !strings.Contains(url, "[") {
			portStr := url[idx+1:]
			var port int
			for _, c := range portStr {
				if c >= '0' && c <= '9' {
					port = port*10 + int(c-'0')
				} else {
					break
				}
			}
			if port > 0 && port <= 65535 {
				return port
			}
		}
	}

	// 默认端口
	if isHTTPS {
		return 443
	}
	return 80
}

// SplitHostPort 分离主机和端口（支持IPv6）
func SplitHostPort(hostport string) (host string, port string) {
	// IPv6格式: [::1]:8080
	if strings.HasPrefix(hostport, "[") {
		if idx := strings.LastIndex(hostport, "]:"); idx > 0 {
			return hostport[1:idx], hostport[idx+2:]
		}
		// 没有端口的IPv6: [::1]
		if strings.HasSuffix(hostport, "]") {
			return hostport[1 : len(hostport)-1], ""
		}
		return hostport, ""
	}

	// IPv4/域名格式
	if idx := strings.LastIndex(hostport, ":"); idx > 0 {
		return hostport[:idx], hostport[idx+1:]
	}

	return hostport, ""
}
