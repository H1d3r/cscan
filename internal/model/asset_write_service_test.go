package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssetWriteService_MapScannerAssetToModel(t *testing.T) {
	service := &AssetWriteService{}

	sa := &ScannerAsset{
		Authority:  "example.com:80",
		Host:       "example.com",
		Port:       80,
		Category:   "domain",
		Service:    "http",
		Title:      "Test Site",
		App:        []string{"nginx"},
		HttpStatus: "200",
		IsHTTP:     true,
		Source:     "scan",
		CName:      "cdn.example.com",
	}

	asset := service.mapScannerAssetToModel(sa, "task123", "org456")

	assert.Equal(t, "example.com:80", asset.Authority)
	assert.Equal(t, "example.com", asset.Host)
	assert.Equal(t, 80, asset.Port)
	assert.Equal(t, "domain", asset.Category)
	assert.Equal(t, "http", asset.Service)
	assert.Equal(t, "Test Site", asset.Title)
	assert.Equal(t, []string{"nginx"}, asset.App)
	assert.Equal(t, "200", asset.HttpStatus)
	assert.True(t, asset.IsHTTP)
	assert.Equal(t, "scan", asset.Source)
	assert.Equal(t, "cdn.example.com", asset.CName)
	assert.Equal(t, "task123", asset.TaskId)
	assert.Equal(t, "org456", asset.OrgId)
}

func TestAssetWriteService_ProcessIPInfo(t *testing.T) {
	service := &AssetWriteService{}

	sa := &ScannerAsset{
		Host: "192.168.1.1",
		IPV4: []ScannerIPInfo{
			{IP: "192.168.1.1", Location: "Local"},
		},
	}

	asset := &Asset{Host: "192.168.1.1"}
	service.processIPInfo(asset, sa)

	assert.Len(t, asset.Ip.IpV4, 1)
	assert.Equal(t, "192.168.1.1", asset.Ip.IpV4[0].IPName)
	assert.Equal(t, "Local", asset.Ip.IpV4[0].Location)
}

func TestAssetWriteService_ProcessIPInfo_IPv6(t *testing.T) {
	service := &AssetWriteService{}

	sa := &ScannerAsset{
		Host: "example.com",
		IPV6: []ScannerIPInfo{
			{IP: "2001:db8::1", Location: "CloudProvider"},
		},
	}

	asset := &Asset{Host: "example.com"}
	service.processIPInfo(asset, sa)

	assert.Len(t, asset.Ip.IpV6, 1)
	assert.Equal(t, "2001:db8::1", asset.Ip.IpV6[0].IPName)
	assert.Equal(t, "CloudProvider", asset.Ip.IpV6[0].Location)
}

func TestVulWriteService_DeriveRiskSource(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{"brutescan", "brutescan", VulRiskSourceWeakPass},
		{"certcheck", "certcheck", VulRiskSourceCertExpiry},
		{"subdomain_takeover", "subdomain_takeover", VulRiskSourceTakeover},
		{"other", "nuclei", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vul := &ScannerVulnerability{Source: tt.source}
			result := deriveRiskSource(vul)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseHostFromUrl(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		expectedHost string
		expectedPort int
	}{
		{"http default port", "http://example.com/path", "example.com", 80},
		{"https default port", "https://example.com/path", "example.com", 443},
		{"custom port", "http://example.com:8080/path", "example.com", 8080},
		{"no scheme", "example.com:9000", "example.com", 9000},
		{"empty", "", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := parseHostFromUrl(tt.url)
			assert.Equal(t, tt.expectedHost, host)
			assert.Equal(t, tt.expectedPort, port)
		})
	}
}
