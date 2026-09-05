package logic

import (
	"context"
	"fmt"
	"strings"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"go.mongodb.org/mongo-driver/bson"
)

type attackSurfaceMetricDefinition struct {
	key      string
	labelKey string
	tone     string
	tab      string
	filters  map[string]interface{}
}

var attackSurfaceMetricRegistry = []attackSurfaceMetricDefinition{
	{key: "exposure.subdomains", labelKey: "asset.metrics.subdomains", tone: "info", tab: "service", filters: map[string]interface{}{}},
	{key: "exposure.ips", labelKey: "asset.metrics.ips", tone: "info", tab: "service", filters: map[string]interface{}{}},
	{key: "exposure.ports", labelKey: "asset.metrics.ports", tone: "info", tab: "service", filters: map[string]interface{}{}},
	{key: "exposure.sites", labelKey: "asset.metrics.sites", tone: "info", tab: "service", filters: map[string]interface{}{"webOnly": true}},
	{key: "exposure.dirs", labelKey: "asset.metrics.dirs", tone: "info", tab: "dir", filters: map[string]interface{}{}},
	{key: "exposure.js", labelKey: "asset.metrics.js", tone: "info", tab: "js", filters: map[string]interface{}{}},
	{key: "exposure.screenshots", labelKey: "asset.metrics.screenshots", tone: "info", tab: "service", filters: map[string]interface{}{"hasScreenshot": true}},
	{key: "risk.sensitiveInfo", labelKey: "asset.metrics.sensitiveInfo", tone: "warning", tab: "sensitive", filters: map[string]interface{}{"aiResult": "risk", "aiStatus": "completed"}},
	{key: "risk.vulnTotal", labelKey: "asset.metrics.vulnTotal", tone: "danger", tab: "vuln", filters: map[string]interface{}{}},
}

func buildAttackSurfaceStatData(ctx context.Context, svcCtx *svc.ServiceContext, targetID string) (types.AttackSurfaceStatData, error) {
	targetID = strings.TrimSpace(targetID)
	data := types.AttackSurfaceStatData{
		Metrics: make([]types.AttackSurfaceMetric, 0, len(attackSurfaceMetricRegistry)),
	}

	assetFilter := bson.M{}
	var hostFilter interface{}
	if targetID != "" {
		targetType, targetValue, err := model.DecodeTargetID(targetID)
		if err != nil {
			return data, fmt.Errorf("invalid targetId: %w", err)
		}
		hostFilter = hostFilterForTarget(targetType, targetValue)
		assetFilter["host"] = hostFilter

		target := types.AttackSurfaceTarget{
			Id:          targetID,
			TargetValue: targetValue,
			Labels:      []string{},
		}
		meta, err := svcCtx.GetAssetTargetMetaModel().FindByID(ctx, targetID)
		if err != nil {
			return data, fmt.Errorf("find target metadata: %w", err)
		}
		if meta != nil {
			target = attackSurfaceTargetFromMeta(*meta)
		}
		data.Target = &target
	}

	assetModel := svcCtx.GetAssetModel()
	values := make(map[string]int, len(attackSurfaceMetricRegistry))

	domainFilter := bson.M{"$or": bson.A{
		bson.M{"category": "domain"},
		bson.M{"domain": bson.M{"$exists": true, "$ne": ""}},
		bson.M{"source": "subfinder"},
	}}
	if hostFilter != nil {
		domainFilter = bson.M{"$and": bson.A{domainFilter, bson.M{"host": hostFilter}}}
	}
	_, domainTotal, err := assetModel.AggregateDomains(ctx, domainFilter, 1, 1)
	if err != nil {
		return data, fmt.Errorf("count subdomains: %w", err)
	}
	values["exposure.subdomains"] = domainTotal

	ips, err := assetModel.Distinct(ctx, "ip.ipv4.ip", assetFilter)
	if err != nil {
		return data, fmt.Errorf("count IP addresses: %w", err)
	}
	values["exposure.ips"] = countNonEmpty(ips)

	portFilter := cloneBSONFilter(assetFilter)
	portFilter["port"] = bson.M{"$gt": 0}
	ports, err := assetModel.Distinct(ctx, "port", portFilter)
	if err != nil {
		return data, fmt.Errorf("count ports: %w", err)
	}
	values["exposure.ports"] = countNonEmpty(ports)

	webFilter := cloneBSONFilter(assetFilter)
	webFilter["port"] = bson.M{"$gt": 0}
	webFilter["$or"] = bson.A{
		bson.M{"is_http": true},
		bson.M{"service": bson.M{"$in": bson.A{"http", "https"}}},
		bson.M{"title": bson.M{"$exists": true, "$ne": ""}},
		bson.M{"screenshot": bson.M{"$exists": true, "$ne": ""}},
	}
	if values["exposure.sites"], err = countAssets(ctx, assetModel, webFilter); err != nil {
		return data, fmt.Errorf("count sites: %w", err)
	}

	screenshotFilter := cloneBSONFilter(assetFilter)
	screenshotFilter["screenshot"] = bson.M{"$exists": true, "$nin": bson.A{"", nil}}
	if values["exposure.screenshots"], err = countAssets(ctx, assetModel, screenshotFilter); err != nil {
		return data, fmt.Errorf("count screenshots: %w", err)
	}

	collectionFilter := bson.M{}
	if hostFilter != nil {
		collectionFilter["host"] = hostFilter
	}
	dirModel := svcCtx.GetDirScanResultModel()
	dirCount, err := dirModel.CountByFilter(ctx, collectionFilter)
	if err != nil {
		return data, fmt.Errorf("count directory results: %w", err)
	}
	values["exposure.dirs"] = int(dirCount)

	jsModel := svcCtx.GetJSFinderResultModel()
	jsCount, err := jsModel.Count(ctx, collectionFilter)
	if err != nil {
		return data, fmt.Errorf("count JavaScript results: %w", err)
	}
	values["exposure.js"] = int(jsCount)

	sensitiveInfoFilter := cloneBSONFilter(collectionFilter)
	sensitiveInfoFilter["ai_result"] = "risk"
	sensitiveInfoFilter["ai_status"] = "completed"
	sensitiveInfoCount, err := jsModel.Count(ctx, sensitiveInfoFilter)
	if err != nil {
		return data, fmt.Errorf("count sensitive information: %w", err)
	}
	values["risk.sensitiveInfo"] = int(sensitiveInfoCount)

	vulModel := svcCtx.GetVulModel()
	vulCount, err := vulModel.Count(ctx, collectionFilter)
	if err != nil {
		return data, fmt.Errorf("count vulnerabilities: %w", err)
	}
	values["risk.vulnTotal"] = int(vulCount)

	for _, definition := range attackSurfaceMetricRegistry {
		data.Metrics = append(data.Metrics, types.AttackSurfaceMetric{
			Key:        definition.key,
			LabelKey:   definition.labelKey,
			Value:      values[definition.key],
			Tone:       definition.tone,
			Applicable: true,
			Drilldown: types.AttackSurfaceDrilldown{
				Tab:     definition.tab,
				Filters: cloneMetricFilters(definition.filters),
			},
		})
	}
	return data, nil
}

func attackSurfaceTargetFromMeta(meta model.AssetTargetMeta) types.AttackSurfaceTarget {
	labels := meta.Labels
	if labels == nil {
		labels = []string{}
	}
	return types.AttackSurfaceTarget{
		Id:           meta.Id,
		TargetValue:  meta.TargetValue,
		Labels:       labels,
		Memo:         meta.Memo,
		ColorTag:     meta.ColorTag,
		ScanStatus:   meta.ScanStatus,
		LastScanTime: tsMilli(meta.LastScanTime),
	}
}

func cloneBSONFilter(source bson.M) bson.M {
	clone := make(bson.M, len(source)+1)
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func cloneMetricFilters(source map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func countAssets(ctx context.Context, assetModel *model.AssetModel, filter bson.M) (int, error) {
	count, err := assetModel.Count(ctx, filter)
	return int(count), err
}

func highRiskVulnerabilityClause() bson.A {
	return bson.A{
		bson.M{"severity": bson.M{"$in": bson.A{"critical", "high"}}},
		bson.M{"cvss_score": bson.M{"$gte": 7.0}},
	}
}
