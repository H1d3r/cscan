// Package template provides utilities for parsing Nuclei template YAML files.
package template

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Classification represents the vulnerability classification information from Nuclei templates.
// It contains CVSS metrics, CVE/CWE identifiers for vulnerability categorization.
type Classification struct {
	CvssMetrics string  `yaml:"cvss-metrics" json:"cvssMetrics,omitempty"` // CVSS vector string
	CvssScore   float64 `yaml:"cvss-score" json:"cvssScore,omitempty"`     // CVSS score (0-10)
	CveId       string  `yaml:"cve-id" json:"cveId,omitempty"`             // CVE identifier (e.g., CVE-2021-34473)
	CweId       string  `yaml:"cwe-id" json:"cweId,omitempty"`             // CWE identifier (e.g., CWE-79)
}

// TemplateInfo represents the info section of a Nuclei template.
// It contains metadata about the vulnerability including name, severity, references, and remediation.
type TemplateInfo struct {
	Name           string                 `yaml:"name" json:"name,omitempty"`
	Author         interface{}            `yaml:"author" json:"author,omitempty"` // 可能是字符串或数组
	Severity       string                 `yaml:"severity" json:"severity,omitempty"`
	Description    string                 `yaml:"description" json:"description,omitempty"`
	Reference      []string               `yaml:"reference" json:"reference,omitempty"`
	Remediation    string                 `yaml:"remediation" json:"remediation,omitempty"`
	Classification *Classification        `yaml:"classification" json:"classification,omitempty"`
	Tags           string                 `yaml:"tags" json:"tags,omitempty"`
	Metadata       map[string]interface{} `yaml:"metadata" json:"metadata,omitempty"`
}

// GetAuthor 规范化作者字段（字符串或数组均支持），多个作者以逗号连接
func (t *TemplateInfo) GetAuthor() string {
	if t == nil || t.Author == nil {
		return ""
	}
	switch v := t.Author.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, a := range v {
			if s, ok := a.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// validSeverities 官方模板库有效严重级别（不再有 unknown）
var validSeverities = map[string]bool{
	"critical": true,
	"high":     true,
	"medium":   true,
	"low":      true,
	"info":     true,
}

// NormalizeSeverity 规范化严重级别：非法/空/unknown 一律归为 info（官方模板库已无 Unknown 级别）
func NormalizeSeverity(severity string) string {
	s := strings.ToLower(strings.TrimSpace(severity))
	if validSeverities[s] {
		return s
	}
	return "info"
}

// protocolKeys Nuclei 模板顶层请求协议键，顺序即展示优先级
var protocolKeys = []string{"dns", "http", "network", "tcp", "ssl", "file", "websocket", "headless", "code", "cloud", "dast", "javascript", "mcp", "workflow"}

// ParseProtocol 从模板 YAML 内容解析请求协议类型（http/dns/network/ssl/file/headless 等）
// 返回第一个命中的协议；无法识别时返回空字符串
func ParseProtocol(content string) string {
	if content == "" {
		return ""
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return ""
	}
	for _, key := range protocolKeys {
		if _, ok := doc[key]; ok {
			return key
		}
	}
	return ""
}

// GetVendor 从 metadata 提取厂商名
func (t *TemplateInfo) GetVendor() string {
	return metadataString(t, "vendor")
}

// GetProduct 从 metadata 提取产品名
func (t *TemplateInfo) GetProduct() string {
	return metadataString(t, "product")
}

func metadataString(t *TemplateInfo, key string) string {
	if t == nil {
		return ""
	}
	v, ok := t.Metadata[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case int, int64, float64, bool:
		return fmt.Sprintf("%v", s)
	default:
		return ""
	}
}

// templateWrapper is used to extract only the info section from a Nuclei template.
type templateWrapper struct {
	Id   string        `yaml:"id"`
	Info *TemplateInfo `yaml:"info"`
}

// ParseTemplateInfo parses a Nuclei template YAML content and extracts the info section.
// It handles missing fields gracefully by returning zero values for missing data.
// Returns an error only if the YAML is malformed.
func ParseTemplateInfo(content string) (*TemplateInfo, error) {
	if content == "" {
		return &TemplateInfo{}, nil
	}

	var wrapper templateWrapper
	if err := yaml.Unmarshal([]byte(content), &wrapper); err != nil {
		return nil, err
	}

	// Return empty TemplateInfo if info section is missing
	if wrapper.Info == nil {
		return &TemplateInfo{}, nil
	}

	return wrapper.Info, nil
}

// GetCveIds extracts CVE IDs from the template info.
// It handles both single CVE ID in classification and multiple CVE IDs separated by commas.
// Returns an empty slice if no CVE IDs are found.
func (t *TemplateInfo) GetCveIds() []string {
	if t == nil || t.Classification == nil || t.Classification.CveId == "" {
		return nil
	}

	// Handle multiple CVE IDs separated by commas
	cveId := strings.TrimSpace(t.Classification.CveId)
	if cveId == "" {
		return nil
	}

	// Split by comma and clean up each ID
	parts := strings.Split(cveId, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned := strings.TrimSpace(part)
		if cleaned != "" {
			result = append(result, cleaned)
		}
	}

	return result
}

// GetCweIds extracts CWE IDs from the template info.
// It handles both single CWE ID and multiple CWE IDs separated by commas.
// Returns an empty slice if no CWE IDs are found.
func (t *TemplateInfo) GetCweIds() []string {
	if t == nil || t.Classification == nil || t.Classification.CweId == "" {
		return nil
	}

	// Handle multiple CWE IDs separated by commas
	cweId := strings.TrimSpace(t.Classification.CweId)
	if cweId == "" {
		return nil
	}

	// Split by comma and clean up each ID
	parts := strings.Split(cweId, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned := strings.TrimSpace(part)
		if cleaned != "" {
			result = append(result, cleaned)
		}
	}

	return result
}

// GetCvssScore returns the CVSS score from the template info.
// Returns 0 if no CVSS score is available.
func (t *TemplateInfo) GetCvssScore() float64 {
	if t == nil || t.Classification == nil {
		return 0
	}
	return t.Classification.CvssScore
}

// GetCvssMetrics returns the CVSS metrics string from the template info.
// Returns empty string if no CVSS metrics are available.
func (t *TemplateInfo) GetCvssMetrics() string {
	if t == nil || t.Classification == nil {
		return ""
	}
	return t.Classification.CvssMetrics
}

// GetReferences returns the reference URLs from the template info.
// Returns nil if no references are available.
func (t *TemplateInfo) GetReferences() []string {
	if t == nil {
		return nil
	}
	return t.Reference
}

// GetRemediation returns the remediation advice from the template info.
// Returns empty string if no remediation is available.
func (t *TemplateInfo) GetRemediation() string {
	if t == nil {
		return ""
	}
	return t.Remediation
}
