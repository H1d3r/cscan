package model

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ScannerVulnerability 扫描器漏洞数据传输对象（避免循环依赖）
type ScannerVulnerability struct {
	Authority         string
	Host              string
	Port              int
	Url               string
	PocFile           string
	Source            string
	RiskSource        string
	Severity          string
	Extra             string
	Result            string
	VulName           string
	Tags              []string
	CvssScore         float64
	CveId             string
	CweId             string
	Remediation       string
	References        []string
	MatcherName       string
	ExtractedResults  []string
	CurlCommand       string
	Request           string
	Response          string
	ResponseTruncated bool
}

// SaveVulsResult 漏洞保存结果
type SaveVulsResult struct {
	SavedCount  int32
	NewVulCount int32
}

// VulWriteService 漏洞写入服务，封装完整的漏洞保存业务逻辑
type VulWriteService struct {
	db           *mongo.Database
	vulModel     *VulModel
	assetModel   *AssetModel
	diffModel    *ScanDiffModel
	historyModel *AssetHistoryModel
	assetCache   *AssetCache
}

// AssetCache 资产缓存，用于批量处理时减少数据库查询
type AssetCache struct {
	assets map[string]*Asset
}

// NewAssetCache 创建资产缓存
func NewAssetCache() *AssetCache {
	return &AssetCache{
		assets: make(map[string]*Asset),
	}
}

func (c *AssetCache) getKey(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func (c *AssetCache) getOrCreate(ctx context.Context, assetModel *AssetModel, historyModel *AssetHistoryModel, mainTaskID, host string, port int) *Asset {
	key := c.getKey(host, port)
	if asset, ok := c.assets[key]; ok {
		return asset
	}

	asset, _ := assetModel.FindByHostPort(ctx, host, port)
	if asset == nil {
		asset = &Asset{
			Host:       host,
			Port:       port,
			Authority:  fmt.Sprintf("%s:%d", host, port),
			Service:    "http",
			IsHTTP:     true,
			Source:     "poc_scan",
			CreateTime: time.Now(),
			UpdateTime: time.Now(),
		}
		if port == 443 || port == 8443 {
			asset.Service = "https"
		}
		if err := assetModel.Insert(ctx, asset); err != nil {
			logx.Errorf("[VulWriteService] Failed to create asset for vul: %v", err)
			return nil
		}
		// 记录首次发现历史，确保时间线不为空
		if historyModel != nil {
			firstFound := SnapshotFromAsset(asset, mainTaskID, time.Now(), nil)
			if err := historyModel.Insert(ctx, firstFound); err != nil {
				logx.Errorf("[VulWriteService] Insert first-found history failed: %v", err)
			}
		}
		asset, _ = assetModel.FindByHostPort(ctx, host, port)
	}
	if asset != nil {
		c.assets[key] = asset
	}
	return asset
}

// NewVulWriteService 创建漏洞写入服务
func NewVulWriteService(db *mongo.Database) *VulWriteService {
	return &VulWriteService{
		db:           db,
		vulModel:     NewVulModel(db),
		assetModel:   NewAssetModel(db),
		diffModel:    NewScanDiffModel(db),
		historyModel: NewAssetHistoryModel(db),
		assetCache:   NewAssetCache(),
	}
}

// SaveVuls 保存漏洞列表（完整业务逻辑从 RPC 层迁移）
func (s *VulWriteService) SaveVuls(ctx context.Context, mainTaskID string, vuls []*ScannerVulnerability) (*SaveVulsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if len(vuls) == 0 {
		return &SaveVulsResult{}, nil
	}

	var vulDiffs []ScanDiff
	assetRiskMap := make(map[string]float64)
	assetVulCount := make(map[string]int)

	var savedCount int32
	var newVulCount int32

	for _, pbVul := range vuls {
		host := pbVul.Host
		port := pbVul.Port

		if host == "" && pbVul.Url != "" {
			parsedHost, parsedPort := parseHostFromUrl(pbVul.Url)
			if parsedHost != "" {
				host = parsedHost
				if port == 0 {
					port = parsedPort
				}
			}
		}

		s.assetCache.getOrCreate(ctx, s.assetModel, s.historyModel, mainTaskID, host, port)

		vul := &Vul{
			Authority: pbVul.Authority,
			Host:      host,
			Port:      port,
			Url:       pbVul.Url,
			PocFile:   pbVul.PocFile,
			Source:    pbVul.Source,
			Severity:  pbVul.Severity,
			Extra:     pbVul.Extra,
			Result:    pbVul.Result,
			TaskId:    mainTaskID,
		}

		if pbVul.CvssScore != 0 {
			vul.CvssScore = pbVul.CvssScore
		}
		if pbVul.CveId != "" {
			vul.CveId = pbVul.CveId
		}
		if pbVul.CweId != "" {
			vul.CweId = pbVul.CweId
		}
		if pbVul.Remediation != "" {
			vul.Remediation = pbVul.Remediation
		}
		if len(pbVul.References) > 0 {
			vul.References = pbVul.References
		}

		if pbVul.MatcherName != "" {
			vul.MatcherName = pbVul.MatcherName
		}
		if len(pbVul.ExtractedResults) > 0 {
			vul.ExtractedResults = pbVul.ExtractedResults
		}
		if pbVul.CurlCommand != "" {
			vul.CurlCommand = pbVul.CurlCommand
		}
		if pbVul.Request != "" {
			vul.Request = pbVul.Request
		}
		if pbVul.Response != "" {
			vul.Response = pbVul.Response
		}
		vul.ResponseTruncated = pbVul.ResponseTruncated

		if pbVul.VulName != "" {
			vul.VulName = pbVul.VulName
		}
		if len(pbVul.Tags) > 0 {
			vul.Tags = pbVul.Tags
		}

		if rs := deriveRiskSource(pbVul); rs != "" {
			vul.RiskSource = rs
		}

		logx.Infof("[VulWriteService] poc=%s vulName=%q tags=%v", pbVul.PocFile, pbVul.VulName, pbVul.Tags)

		res, err := s.vulModel.Upsert(ctx, vul)
		if err != nil {
			logx.Errorf("[VulWriteService] failed to upsert vul (host=%s port=%d poc=%s): %v", host, port, pbVul.PocFile, err)
			return &SaveVulsResult{SavedCount: savedCount}, err
		}
		savedCount++

		key := fmt.Sprintf("%s:%d", host, port)
		score := vul.CvssScore * 10
		if val, ok := assetRiskMap[key]; !ok || score > val {
			assetRiskMap[key] = score
		}

		isNewVul := res != nil && res.UpsertedCount > 0
		if isNewVul {
			newVulCount++
			assetVulCount[key]++
			vulDiffs = append(vulDiffs, ScanDiff{
				TaskId:     mainTaskID,
				DiffType:   ScanDiffTypeVul,
				ChangeType: ScanDiffChangeAdded,
				Severity:   pbVul.Severity,
				TargetKey:  fmt.Sprintf("%s:%d:%s", host, port, pbVul.PocFile),
				Summary:    vul.VulName,
			})
		}
	}

	// 批量更新资产风险评分
	for key, maxScore := range assetRiskMap {
		parts := strings.Split(key, ":")
		if len(parts) != 2 {
			continue
		}
		host := parts[0]
		port, _ := strconv.Atoi(parts[1])

		asset := s.assetCache.getOrCreate(ctx, s.assetModel, s.historyModel, mainTaskID, host, port)
		if asset == nil {
			continue
		}

		riskLevel := "info"
		if maxScore >= 90 {
			riskLevel = "critical"
		} else if maxScore >= 70 {
			riskLevel = "high"
		} else if maxScore >= 40 {
			riskLevel = "medium"
		} else if maxScore > 0 {
			riskLevel = "low"
		}

		setFields := bson.M{
			"last_scan_time": time.Now(),
		}
		needUpdate := false
		if maxScore > asset.RiskScore {
			setFields["risk_score"] = maxScore
			setFields["risk_level"] = riskLevel
			needUpdate = true
		}

		rawUpdate := bson.M{
			"$set": setFields,
		}

		newCount := assetVulCount[key]
		if newCount > 0 {
			rawUpdate["$inc"] = bson.M{"vul_count": int(newCount)}
			needUpdate = true
		}

		if needUpdate {
			if err := s.assetModel.UpdateWithRaw(ctx, asset.Id.Hex(), rawUpdate); err != nil {
				logx.Errorf("[VulWriteService] Failed to update asset risk/vul_count: %v", err)
			}
		}
	}

	logx.Infof("[VulWriteService] SaveVuls: saved %d vulnerabilities (new=%d), updated %d assets", savedCount, newVulCount, len(assetRiskMap))

	if len(vulDiffs) > 0 {
		if err := s.diffModel.BatchInsert(ctx, vulDiffs); err != nil {
			logx.Errorf("[VulWriteService] [ScanDiff] vul batch insert failed (task=%s): %v", mainTaskID, err)
		} else {
			logx.Infof("[VulWriteService] [ScanDiff] wrote %d vul diff records for task=%s", len(vulDiffs), mainTaskID)
		}
	}

	for _, pbVul := range vuls {
		logx.Infof("[VulWriteService] Saved vul: host=%s, port=%d, url=%s, pocFile=%s, severity=%s, vulName=%s",
			pbVul.Host, pbVul.Port, pbVul.Url, pbVul.PocFile, pbVul.Severity, pbVul.VulName)
	}

	return &SaveVulsResult{
		SavedCount:  savedCount,
		NewVulCount: newVulCount,
	}, nil
}

// deriveRiskSource 依据漏洞来源归一化 risk_source
func deriveRiskSource(pbVul *ScannerVulnerability) string {
	switch pbVul.Source {
	case "brutescan":
		return VulRiskSourceWeakPass
	case "certcheck":
		return VulRiskSourceCertExpiry
	case "subdomain_takeover":
		return VulRiskSourceTakeover
	}
	return ""
}

// parseHostFromUrl 从 URL 解析 host 和 port
func parseHostFromUrl(rawUrl string) (string, int) {
	if rawUrl == "" {
		return "", 0
	}

	if !strings.Contains(rawUrl, "://") {
		rawUrl = "http://" + rawUrl
	}

	u, err := url.Parse(rawUrl)
	if err != nil {
		return "", 0
	}

	host := u.Hostname()
	port := 80

	if u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	} else if u.Scheme == "https" {
		port = 443
	}

	return host, port
}
