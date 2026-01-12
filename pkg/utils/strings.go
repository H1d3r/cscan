package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"
)

// StringUtils 字符串工具集

// TruncateString 截断字符串到指定长度
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// TruncateStringRunes 按字符数截断（支持中文）
func TruncateStringRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// RemoveWhitespace 移除所有空白字符
func RemoveWhitespace(s string) string {
	var result strings.Builder
	for _, r := range s {
		if !unicode.IsSpace(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// NormalizeWhitespace 标准化空白字符（多个空白变成单个空格）
func NormalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ContainsAny 检查字符串是否包含任意一个子串
func ContainsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ContainsAll 检查字符串是否包含所有子串
func ContainsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// MD5Hash 计算MD5哈希
func MD5Hash(s string) string {
	hash := md5.Sum([]byte(s))
	return hex.EncodeToString(hash[:])
}

// SHA256Hash 计算SHA256哈希
func SHA256Hash(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}

// ExtractBetween 提取两个标记之间的内容
func ExtractBetween(s, start, end string) string {
	startIdx := strings.Index(s, start)
	if startIdx == -1 {
		return ""
	}
	startIdx += len(start)

	endIdx := strings.Index(s[startIdx:], end)
	if endIdx == -1 {
		return ""
	}

	return s[startIdx : startIdx+endIdx]
}

// ExtractAllBetween 提取所有两个标记之间的内容
func ExtractAllBetween(s, start, end string) []string {
	var results []string
	remaining := s

	for {
		startIdx := strings.Index(remaining, start)
		if startIdx == -1 {
			break
		}
		startIdx += len(start)

		endIdx := strings.Index(remaining[startIdx:], end)
		if endIdx == -1 {
			break
		}

		results = append(results, remaining[startIdx:startIdx+endIdx])
		remaining = remaining[startIdx+endIdx+len(end):]
	}

	return results
}

// SplitLines 按行分割（处理不同的换行符）
func SplitLines(s string) []string {
	// 统一换行符
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// SplitNonEmpty 分割并过滤空字符串
func SplitNonEmpty(s, sep string) []string {
	parts := strings.Split(s, sep)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// IsBlank 检查字符串是否为空或只包含空白字符
func IsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// DefaultIfBlank 如果字符串为空则返回默认值
func DefaultIfBlank(s, defaultVal string) string {
	if IsBlank(s) {
		return defaultVal
	}
	return s
}

// SafeSubstring 安全截取子串（不会panic）
func SafeSubstring(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	if start >= end {
		return ""
	}
	return s[start:end]
}

// RegexMatch 正则匹配（返回是否匹配）
func RegexMatch(pattern, s string) bool {
	matched, _ := regexp.MatchString(pattern, s)
	return matched
}

// RegexFind 正则查找（返回第一个匹配）
func RegexFind(pattern, s string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	return re.FindString(s)
}

// RegexFindAll 正则查找所有匹配
func RegexFindAll(pattern, s string, n int) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re.FindAllString(s, n)
}

// EscapeHTML 转义HTML特殊字符
func EscapeHTML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(s)
}

// UnescapeHTML 反转义HTML特殊字符
func UnescapeHTML(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
	)
	return replacer.Replace(s)
}
