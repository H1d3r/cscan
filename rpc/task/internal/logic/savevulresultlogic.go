package logic

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cscan/model"
	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type SaveVulResultLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveVulResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveVulResultLogic {
	return &SaveVulResultLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 保存漏洞结果
// Note: This currently saves to the {workspace}_vul collection using the legacy Vul model.
// The vulnerability scanner produces Vul objects, not ScanResult objects.
// For now, we keep this behavior and add scan_timestamp tracking for history purposes.
func (l *SaveVulResultLogic) SaveVulResult(in *pb.SaveVulResultReq) (*pb.SaveVulResultResp, error) {
	if len(in.Vuls) == 0 {
		return &pb.SaveVulResultResp{
			Success: true,
			Message: "No vulnerabilities to save",
			Total:   0,
		}, nil
	}

	workspaceId := in.WorkspaceId
	if workspaceId == "" {
		workspaceId = "default"
	}

	vulModel := l.svcCtx.GetVulModel(workspaceId)
	var savedCount int32

	for _, pbVul := range in.Vuls {
		vul := &model.Vul{
			Authority: pbVul.Authority,
			Host:      pbVul.Host,
			Port:      int(pbVul.Port),
			Url:       pbVul.Url,
			PocFile:   pbVul.PocFile,
			Source:    pbVul.Source,
			Severity:  pbVul.Severity,
			Extra:     pbVul.Extra,
			Result:    pbVul.Result,
			TaskId:    in.MainTaskId,
		}

		// 漏洞知识库关联字段
		if pbVul.CvssScore != nil {
			vul.CvssScore = *pbVul.CvssScore
		}
		if pbVul.CveId != nil {
			vul.CveId = *pbVul.CveId
		}
		if pbVul.CweId != nil {
			vul.CweId = *pbVul.CweId
		}
		if pbVul.Remediation != nil {
			vul.Remediation = *pbVul.Remediation
		}
		if len(pbVul.References) > 0 {
			vul.References = pbVul.References
		}

		// 证据链字段
		if pbVul.MatcherName != nil {
			vul.MatcherName = *pbVul.MatcherName
		}
		if len(pbVul.ExtractedResults) > 0 {
			vul.ExtractedResults = pbVul.ExtractedResults
		}
		if pbVul.CurlCommand != nil {
			vul.CurlCommand = *pbVul.CurlCommand
		}
		if pbVul.Request != nil {
			vul.Request = *pbVul.Request
		}
		if pbVul.Response != nil {
			vul.Response = *pbVul.Response
		}
		if pbVul.ResponseTruncated != nil {
			vul.ResponseTruncated = *pbVul.ResponseTruncated
		}

		// 使用Upsert避免重复
		// Note: The Upsert method in VulModel already handles scan_count and timestamps
		// which provides basic history tracking through first_seen_time and last_seen_time
		if err := vulModel.Upsert(l.ctx, vul); err != nil {
			l.Logger.Errorf("SaveVulResult: failed to upsert vul: %v", err)
			continue
		}
		savedCount++
	}

	// Update assets with risk scores
	// Group vulns by asset (Host:Port) to aggregate risk score
	assetRiskMap := make(map[string]float64) // Key: "host:port", Value: maxScore

	for _, pbVul := range in.Vuls {
		key := fmt.Sprintf("%s:%d", pbVul.Host, pbVul.Port)
		score := 0.0
		if pbVul.CvssScore != nil {
			score = *pbVul.CvssScore
		}
		if val, ok := assetRiskMap[key]; !ok || score > val {
			assetRiskMap[key] = score
		}
	}

	assetModel := l.svcCtx.GetAssetModel(workspaceId)
	for key, maxScore := range assetRiskMap {
		parts := strings.Split(key, ":")
		if len(parts) != 2 {
			continue
		}
		host := parts[0]
		port, _ := strconv.Atoi(parts[1])

		// Find existing asset
		asset, err := assetModel.FindByHostPort(l.ctx, host, port)
		if err != nil || asset == nil {
			continue
		}

		// Determine Risk Level
		riskLevel := "info"
		if maxScore >= 9.0 {
			riskLevel = "critical"
		} else if maxScore >= 7.0 {
			riskLevel = "high"
		} else if maxScore >= 4.0 {
			riskLevel = "medium"
		} else if maxScore > 0 {
			riskLevel = "low"
		}

		// Update if new score is higher
		update := bson.M{
			"last_scan_time": time.Now(),
		}
		if maxScore > asset.RiskScore {
			update["risk_score"] = maxScore
			update["risk_level"] = riskLevel
		}

		if err := assetModel.Update(l.ctx, asset.Id.Hex(), update); err != nil {
			l.Logger.Errorf("Failed to update asset risk: %v", err)
		}
	}

	l.Logger.Infof("SaveVulResult: saved %d vulnerabilities", savedCount)

	return &pb.SaveVulResultResp{
		Success: true,
		Message: "Vulnerabilities saved successfully",
		Total:   savedCount,
	}, nil
}
