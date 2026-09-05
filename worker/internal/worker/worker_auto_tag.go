package worker

import (
	"context"
	"sort"
	"strings"

	"cscan/internal/model"
	"cscan/internal/scanner"
	"cscan/internal/scheduler"
	"cscan/pkg/mapping"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AssetGroup 资产组，具有相同标签集的资产归为一组。
// Untagged groups are explicit applicable groups with no automatic template
// selector; they must remain visible as uncovered instead of being discarded.
type AssetGroup struct {
	GroupKey string
	Assets   []*scanner.Asset
	Tags     []string
	Untagged bool
}

// PocVulnerabilityConclusion separates a legitimate zero-finding result from
// a scan that did not evaluate any applicable asset.
type PocVulnerabilityConclusion string

const (
	PocConclusionFindings           PocVulnerabilityConclusion = "FINDINGS"
	PocConclusionNoFindings         PocVulnerabilityConclusion = "NO_FINDINGS"
	PocConclusionNotEvaluated       PocVulnerabilityConclusion = "NOT_EVALUATED"
	PocConclusionPartiallyEvaluated PocVulnerabilityConclusion = "PARTIALLY_EVALUATED"
)

// PocGroupResult is the auditable outcome of one applicable asset group.
type PocGroupResult struct {
	GroupKey            string              `json:"groupKey" bson:"group_key"`
	Tags                []string            `json:"tags,omitempty" bson:"tags,omitempty"`
	AssetCount          int                 `json:"assetCount" bson:"asset_count"`
	RequestedTemplates  int                 `json:"requestedTemplates" bson:"requested_templates"`
	ValidTemplates      int                 `json:"validTemplates" bson:"valid_templates"`
	ExecutedTemplates   int                 `json:"executedTemplates" bson:"executed_templates"`
	ScannedAssets       int                 `json:"scannedAssets" bson:"scanned_assets"`
	Vulnerabilities     int                 `json:"vulnerabilities" bson:"vulnerabilities"`
	Status              scanner.PhaseStatus `json:"status" bson:"status"`
	ReasonCode          string              `json:"reasonCode,omitempty" bson:"reason_code,omitempty"`
	TemplateLoadOutcome TemplateLoadOutcome `json:"templateLoadOutcome" bson:"template_load_outcome"`
	TemplateSource      string              `json:"templateSource,omitempty" bson:"template_source,omitempty"`
	InvalidTemplates    int                 `json:"invalidTemplates" bson:"invalid_templates"`
}

// PocCoverageResult aggregates group and asset coverage without conflating an
// empty vulnerability slice with a successful scan.
type PocCoverageResult struct {
	Groups                  []PocGroupResult           `json:"groups" bson:"groups"`
	TotalGroups             int                        `json:"totalGroups" bson:"total_groups"`
	ScannedGroups           int                        `json:"scannedGroups" bson:"scanned_groups"`
	UncoveredGroups         int                        `json:"uncoveredGroups" bson:"uncovered_groups"`
	FailedGroups            int                        `json:"failedGroups" bson:"failed_groups"`
	TotalAssets             int                        `json:"totalAssets" bson:"total_assets"`
	ScannedAssets           int                        `json:"scannedAssets" bson:"scanned_assets"`
	UncoveredAssets         int                        `json:"uncoveredAssets" bson:"uncovered_assets"`
	ValidTemplates          int                        `json:"validTemplates" bson:"valid_templates"`
	ExecutedTemplates       int                        `json:"executedTemplates" bson:"executed_templates"`
	Vulnerabilities         int                        `json:"vulnerabilities" bson:"vulnerabilities"`
	Status                  scanner.PhaseStatus        `json:"status" bson:"status"`
	VulnerabilityConclusion PocVulnerabilityConclusion `json:"vulnerabilityConclusion" bson:"vulnerability_conclusion"`
	VulnerabilityResults    []*scanner.Vulnerability   `json:"-" bson:"-"`
}

type pocTemplateLoader func(context.Context, []string, []string) (TemplateLoadResult, error)

// generateAssetTags 为单个资产生成标签（基于自定义标签映射和Wappalyzer映射）
func (w *Worker) generateAssetTags(asset *scanner.Asset, pocConfig *scheduler.PocScanConfig) []string {
	tagSet := make(map[string]bool)

	for _, app := range asset.App {
		appName := parseAppName(app)
		if appName == "" || !isConfirmedFingerprintApp(asset, app, appName) {
			continue
		}
		appNameLower := strings.ToLower(appName)
		matched := false

		// 模式1: 基于自定义标签映射
		if pocConfig.AutoScan && pocConfig.TagMappings != nil {
			for mappedApp, tags := range pocConfig.TagMappings {
				if strings.ToLower(mappedApp) == appNameLower {
					for _, tag := range tags {
						tagSet[tag] = true
					}
					matched = true
					break
				}
			}
		}

		// 模式2: 基于Wappalyzer内置映射
		if pocConfig.AutomaticScan {
			if tags, ok := mapping.WappalyzerNucleiMapping[appNameLower]; ok {
				for _, tag := range tags {
					tagSet[tag] = true
				}
				matched = true
			} else if strings.Contains(appNameLower, " ") {
				// 拆分多词 Nmap 产品名，逐词尝试匹配映射
				for _, part := range strings.Fields(appNameLower) {
					if partTags, ok := mapping.WappalyzerNucleiMapping[part]; ok {
						for _, tag := range partTags {
							tagSet[tag] = true
						}
						matched = true
					}
				}
			}
		}

		// 兜底: 未匹配任何映射时，将指纹名小写作为标签传入POC扫描
		if !matched && (pocConfig.AutoScan || pocConfig.AutomaticScan) {
			tagSet[appNameLower] = true
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	return tags
}

// isConfirmedFingerprintApp keeps legacy/non-fingerprint App entries usable,
// while preventing a custom/active candidate retained in historical or mixed
// data from expanding automatic POC scope. New scans only place CONFIRMED
// findings in App, so this is a defense-in-depth compatibility boundary.
func isConfirmedFingerprintApp(asset *scanner.Asset, formattedApp, appName string) bool {
	if asset == nil || len(asset.FingerprintFindings) == 0 {
		return true
	}
	hasFinding := false
	for _, finding := range asset.FingerprintFindings {
		if !strings.EqualFold(strings.TrimSpace(finding.Name), strings.TrimSpace(appName)) {
			continue
		}
		hasFinding = true
		if finding.Decision == "CONFIRMED" {
			return true
		}
	}
	if !hasFinding {
		return true
	}
	lower := strings.ToLower(formattedApp)
	return !strings.Contains(lower, "[custom") && !strings.Contains(lower, "+custom") &&
		!strings.Contains(lower, "[active") && !strings.Contains(lower, "+active")
}

// groupAssetsByTags 按标签集对资产进行分组。
// 无标签资产属于适用但未覆盖的显式 untagged 分组，不能静默丢弃。
func (w *Worker) groupAssetsByTags(assets []*scanner.Asset, pocConfig *scheduler.PocScanConfig) []*AssetGroup {
	groups := make(map[string]*AssetGroup)

	for _, asset := range assets {
		tags := w.generateAssetTags(asset, pocConfig)
		sortedTags := append([]string(nil), tags...)
		sort.Strings(sortedTags)
		key := strings.Join(sortedTags, ",")
		untagged := len(sortedTags) == 0
		if untagged {
			key = "untagged"
		}

		if _, ok := groups[key]; !ok {
			groups[key] = &AssetGroup{
				GroupKey: key,
				Assets:   make([]*scanner.Asset, 0),
				Tags:     sortedTags,
				Untagged: untagged,
			}
		}
		groups[key].Assets = append(groups[key].Assets, asset)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]*AssetGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, groups[key])
	}
	return result
}

// executePocGroups runs the same grouped POC path for production and
// deterministic tests. Template loading and Nuclei execution remain injectable,
// while all status/conclusion decisions are centralized here.
func executePocGroups(
	ctx context.Context,
	groups []*AssetGroup,
	severities []string,
	baseOptions *scanner.NucleiOptions,
	loader pocTemplateLoader,
	nuclei scanner.Scanner,
	taskLogger func(level, format string, args ...interface{}),
) PocCoverageResult {
	return executePocGroupsWithEvents(ctx, groups, severities, baseOptions, loader, nuclei, taskLogger, nil)
}

func executePocGroupsWithEvents(
	ctx context.Context,
	groups []*AssetGroup,
	severities []string,
	baseOptions *scanner.NucleiOptions,
	loader pocTemplateLoader,
	nuclei scanner.Scanner,
	taskLogger func(level, format string, args ...interface{}),
	eventLogger scanner.ScanEventLogger,
) PocCoverageResult {
	summary := PocCoverageResult{Groups: make([]PocGroupResult, 0, len(groups))}
	if len(groups) == 0 {
		summary.Status = scanner.PhaseSkippedNotApplicable
		summary.VulnerabilityConclusion = PocConclusionNotEvaluated
		return summary
	}

	for _, group := range groups {
		groupResult := PocGroupResult{
			GroupKey:   group.GroupKey,
			Tags:       append([]string(nil), group.Tags...),
			AssetCount: len(group.Assets),
		}
		summary.TotalGroups++
		summary.TotalAssets += groupResult.AssetCount

		if err := ctx.Err(); err != nil {
			groupResult.Status = scanner.PhaseCanceled
			groupResult.ReasonCode = scanner.ReasonCanceled
			appendPocGroupEvent(&summary, groupResult, eventLogger)
			continue
		}

		if group.Untagged {
			groupResult.Status = scanner.PhaseUncovered
			groupResult.TemplateLoadOutcome = TemplateLoadNoMatch
			groupResult.ReasonCode = "untagged_assets"
			appendPocGroupEvent(&summary, groupResult, eventLogger)
			continue
		}

		loadResult, loadErr := loader(ctx, group.Tags, severities)
		groupResult.RequestedTemplates = loadResult.Requested
		groupResult.ValidTemplates = loadResult.Loaded
		groupResult.InvalidTemplates = loadResult.Invalid
		groupResult.TemplateLoadOutcome = loadResult.Outcome
		groupResult.TemplateSource = loadResult.Source
		groupResult.ReasonCode = loadResult.ReasonCode
		if groupResult.ValidTemplates == 0 {
			groupResult.ValidTemplates = len(loadResult.Contents) + len(loadResult.FileRefs)
		}

		if loadErr != nil && groupResult.ValidTemplates == 0 {
			if ctx.Err() != nil {
				groupResult.Status = scanner.PhaseCanceled
				groupResult.ReasonCode = scanner.ReasonCanceled
			} else {
				groupResult.Status = scanner.PhaseFailed
				if groupResult.ReasonCode == "" {
					groupResult.ReasonCode = "template_load_failed"
				}
			}
			appendPocGroupEvent(&summary, groupResult, eventLogger)
			continue
		}
		if groupResult.ValidTemplates == 0 {
			groupResult.Status = templateZeroCoverageStatus(loadResult.Outcome)
			if groupResult.ReasonCode == "" {
				groupResult.ReasonCode = templateLoadReason(loadResult.Outcome)
			}
			appendPocGroupEvent(&summary, groupResult, eventLogger)
			continue
		}
		if nuclei == nil {
			groupResult.Status = scanner.PhaseFailed
			groupResult.ReasonCode = "nuclei_unavailable"
			appendPocGroupEvent(&summary, groupResult, eventLogger)
			continue
		}

		opts := scanner.NucleiOptions{}
		if baseOptions != nil {
			opts = *baseOptions
		}
		opts.Tags = append([]string(nil), group.Tags...)
		opts.CustomTemplates = append([]string(nil), loadResult.Contents...)
		opts.TemplateFileRefs = append([]string(nil), loadResult.FileRefs...)
		opts.AutoScan = false
		opts.AutomaticScan = false
		opts.CustomPocOnly = false
		opts.TagMappings = nil

		result, scanErr := nuclei.Scan(ctx, &scanner.ScanConfig{
			Assets:      group.Assets,
			Options:     &opts,
			TaskLogger:  taskLogger,
			EventLogger: eventLogger,
		})
		applyPocScanResult(&groupResult, result, scanErr)
		if loadErr != nil || loadResult.Invalid > 0 {
			if groupResult.Status == scanner.PhaseComplete {
				groupResult.Status = scanner.PhasePartial
			}
			if groupResult.ReasonCode == "" {
				groupResult.ReasonCode = templateLoadReason(loadResult.Outcome)
			}
		}
		if result != nil {
			summary.VulnerabilityResults = append(summary.VulnerabilityResults, result.Vulnerabilities...)
		}
		appendPocGroupEvent(&summary, groupResult, eventLogger)
	}

	finalizePocCoverage(&summary)
	return summary
}

func appendPocGroupEvent(summary *PocCoverageResult, group PocGroupResult, eventLogger scanner.ScanEventLogger) {
	summary.Groups = append(summary.Groups, group)
	if eventLogger == nil {
		return
	}
	eventLogger(EventPocTemplateLoad, "poc", string(group.Status), map[string]interface{}{
		"group_key": group.GroupKey, "tags": append([]string(nil), group.Tags...),
		"asset_count": group.AssetCount, "requested": group.RequestedTemplates,
		"loaded": group.ValidTemplates, "invalid": group.InvalidTemplates,
		"source": group.TemplateSource, "outcome": string(group.TemplateLoadOutcome),
		"reason_code": group.ReasonCode, "scanned_assets": group.ScannedAssets,
		"vulnerabilities": group.Vulnerabilities,
	})
}

func applyPocScanResult(group *PocGroupResult, result *scanner.ScanResult, scanErr error) {
	if result != nil {
		group.Vulnerabilities = len(result.Vulnerabilities)
	}
	if scanErr != nil {
		if result != nil && result.Diagnostic != nil && result.Diagnostic.Coverage.Succeeded > 0 {
			group.Status = scanner.PhasePartial
			group.ScannedAssets = minInt(group.AssetCount, result.Diagnostic.Coverage.Succeeded)
			group.ExecutedTemplates = group.ValidTemplates
		} else if result != nil && result.Diagnostic != nil && result.Diagnostic.Status == scanner.PhaseCanceled {
			group.Status = scanner.PhaseCanceled
			group.ReasonCode = scanner.ReasonCanceled
		} else {
			group.Status = scanner.PhaseFailed
			group.ReasonCode = scanner.ReasonExecutionError
		}
		return
	}
	if result != nil && result.Diagnostic != nil {
		diagnostic := result.Diagnostic
		group.Status = diagnostic.Status
		group.ScannedAssets = minInt(group.AssetCount, diagnostic.Coverage.Succeeded)
		if diagnostic.Coverage.Attempted > 0 {
			group.ExecutedTemplates = group.ValidTemplates
		}
		if group.ReasonCode == "" && len(diagnostic.WarningCodes) > 0 {
			group.ReasonCode = diagnostic.WarningCodes[0]
		}
		return
	}

	// Legacy/fake scanners without diagnostics retain the historical contract:
	// a nil error means every supplied asset completed against every template.
	group.Status = scanner.PhaseComplete
	group.ScannedAssets = group.AssetCount
	group.ExecutedTemplates = group.ValidTemplates
}

func finalizePocCoverage(summary *PocCoverageResult) {
	canceled := false
	partial := false
	for _, group := range summary.Groups {
		summary.ValidTemplates += group.ValidTemplates
		summary.ExecutedTemplates += group.ExecutedTemplates
		summary.ScannedAssets += group.ScannedAssets
		summary.Vulnerabilities += group.Vulnerabilities
		if group.ScannedAssets > 0 {
			summary.ScannedGroups++
		}
		uncoveredAssets := group.AssetCount - group.ScannedAssets
		if uncoveredAssets > 0 {
			summary.UncoveredAssets += uncoveredAssets
		}
		switch group.Status {
		case scanner.PhaseCanceled:
			canceled = true
		case scanner.PhaseUncovered:
			summary.UncoveredGroups++
		case scanner.PhaseFailed:
			summary.FailedGroups++
		case scanner.PhasePartial:
			partial = true
		}
	}

	switch {
	case canceled:
		summary.Status = scanner.PhaseCanceled
	case summary.TotalAssets == 0:
		summary.Status = scanner.PhaseSkippedNotApplicable
	case summary.ScannedAssets == 0 && summary.FailedGroups > 0:
		summary.Status = scanner.PhaseFailed
	case summary.ScannedAssets == 0:
		summary.Status = scanner.PhaseUncovered
	case summary.ScannedAssets < summary.TotalAssets || summary.UncoveredGroups > 0 || summary.FailedGroups > 0 || partial:
		summary.Status = scanner.PhasePartial
	default:
		summary.Status = scanner.PhaseComplete
	}

	switch {
	case summary.Vulnerabilities > 0:
		summary.VulnerabilityConclusion = PocConclusionFindings
	case summary.Status == scanner.PhaseComplete:
		summary.VulnerabilityConclusion = PocConclusionNoFindings
	case summary.ScannedAssets == 0:
		summary.VulnerabilityConclusion = PocConclusionNotEvaluated
	default:
		summary.VulnerabilityConclusion = PocConclusionPartiallyEvaluated
	}
}

func templateZeroCoverageStatus(outcome TemplateLoadOutcome) scanner.PhaseStatus {
	switch outcome {
	case TemplateLoadStoreUnavailable, TemplateLoadDBError, TemplateLoadInvalidContent:
		return scanner.PhaseFailed
	default:
		return scanner.PhaseUncovered
	}
}

func templateLoadReason(outcome TemplateLoadOutcome) string {
	switch outcome {
	case TemplateLoadFiltered:
		return "templates_filtered"
	case TemplateLoadStoreUnavailable:
		return "template_store_unavailable"
	case TemplateLoadDBError:
		return "template_load_db_error"
	case TemplateLoadInvalidContent:
		return "template_content_invalid"
	default:
		return "templates_no_match"
	}
}

func templatePhaseReason(result TemplateLoadResult, loadErr error) string {
	if loadErr != nil {
		return scanner.ReasonTemplateUnavailable
	}
	switch result.Outcome {
	case TemplateLoadStoreUnavailable, TemplateLoadDBError:
		return scanner.ReasonTemplateUnavailable
	case TemplateLoadInvalidContent:
		return scanner.ReasonTemplateInvalid
	case TemplateLoadFiltered, TemplateLoadNoMatch:
		return scanner.ReasonTemplateNoMatch
	default:
		if result.Invalid > 0 {
			return scanner.ReasonTemplateInvalid
		}
		return ""
	}
}

func applyExplicitPocTemplateCoverage(pocPhaseResult PhaseResult, templateLoadResult TemplateLoadResult, templateLoadErr error, assetCount int) PhaseResult {
	loaded := templateLoadResult.Loaded
	if loaded == 0 {
		loaded = len(templateLoadResult.Contents) + len(templateLoadResult.FileRefs)
	}
	reasonCode := templatePhaseReason(templateLoadResult, templateLoadErr)
	partialLoad := templateLoadErr != nil || templateLoadResult.Invalid > 0 ||
		(templateLoadResult.Requested > 0 && loaded < templateLoadResult.Requested)
	if partialLoad {
		if pocPhaseResult.Status == scanner.PhaseComplete {
			pocPhaseResult.Status = scanner.PhasePartial
		}
		if reasonCode != "" {
			pocPhaseResult.ReasonCodes = append(pocPhaseResult.ReasonCodes, reasonCode)
		}
	}
	if pocPhaseResult.Status != scanner.PhaseSkippedNotApplicable || assetCount <= 0 {
		return pocPhaseResult
	}

	coverage := scanner.Coverage{Input: assetCount, Uncovered: assetCount}
	if templateLoadErr != nil || templateLoadResult.Outcome == TemplateLoadStoreUnavailable ||
		templateLoadResult.Outcome == TemplateLoadDBError || templateLoadResult.Outcome == TemplateLoadInvalidContent {
		coverage.Attempted = assetCount
		coverage.Failed = assetCount
	}
	pocPhaseResult = NewPhaseResult("poc", coverage, false, reasonCode)
	pocPhaseResult.VulnerabilityConclusion = model.VulnerabilityConclusionNotEvaluated
	return pocPhaseResult
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getTemplatesByTags 通过 HTTP 接口从数据库获取符合标签的模板
func (w *Worker) getTemplatesByTags(ctx context.Context, tags []string, severities []string) []string {
	if len(tags) == 0 {
		return nil
	}

	// 直连 MongoDB 获取模板
	resp, err := w.loadTemplates(ctx, &TemplatesReq{
		Tags:       tags,
		Severities: severities,
	})
	if err != nil {
		w.logger.Error("GetTemplates HTTP failed: %v", err)
		return nil
	}

	if !resp.Success {
		w.logger.Error("GetTemplates failed: %s", resp.Msg)
		return nil
	}

	w.logger.Info("GetTemplatesByTags: fetched %d templates for tags %v", resp.Count, tags)
	return resp.Templates
}

// getTemplatesByIds 通过 HTTP 接口根据ID列表获取模板内容
func (w *Worker) getTemplatesByIds(ctx context.Context, nucleiTemplateIds, customPocIds []string) []string {
	if len(nucleiTemplateIds) == 0 && len(customPocIds) == 0 {
		return nil
	}

	// 直连 MongoDB 获取模板
	resp, err := w.loadTemplates(ctx, &TemplatesReq{
		NucleiTemplateIds: nucleiTemplateIds,
		CustomPocIds:      customPocIds,
	})
	if err != nil {
		w.logger.Error("GetTemplates HTTP failed: %v", err)
		return nil
	}

	if !resp.Success {
		w.logger.Error("GetTemplates failed: %s", resp.Msg)
		return nil
	}

	w.logger.Info("GetTemplatesByIds: requested nucleiIds=%d customPocIds=%d, fetched %d templates", len(nucleiTemplateIds), len(customPocIds), resp.Count)
	return resp.Templates
}

// getAllCustomPocs 获取所有自定义POC
func (w *Worker) getAllCustomPocs(ctx context.Context, severities []string) []string {
	// 直连 MongoDB 获取所有自定义POC
	resp, err := w.loadTemplates(ctx, &TemplatesReq{
		Severities:    severities,
		CustomPocOnly: true,
	})
	if err != nil {
		w.logger.Error("GetAllCustomPocs HTTP failed: %v", err)
		return nil
	}

	if !resp.Success {
		w.logger.Error("GetAllCustomPocs failed: %s", resp.Msg)
		return nil
	}

	w.logger.Info("GetAllCustomPocs: fetched %d custom POC templates", resp.Count)
	return resp.Templates
}

// parseAppName 解析应用名称，去除版本号和来源标�?
func parseAppName(app string) string {
	appName := app
	// 先去�?[source] 后缀
	if idx := strings.Index(appName, "["); idx > 0 {
		appName = appName[:idx]
	}
	// 再去除 :version 后缀
	if idx := strings.Index(appName, ":"); idx > 0 {
		appName = appName[:idx]
	}
	return strings.TrimSpace(appName)
}

// loadCustomFingerprints 加载自定义指纹到指纹扫描器
// activeScan: 是否启用主动扫描，如果启用则同时加载主动指纹
func (w *Worker) loadCustomFingerprints(ctx context.Context, fpScanner *scanner.FingerprintScanner, activeScan bool) (passiveCount, activeCount int) {
	// 添加 panic 恢复机制
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Load custom fingerprints panic recovered: %v, stack: %s", r, string(getStackTrace()))
		}
	}()

	// 直连 MongoDB 获取被动指纹配置
	var passiveFingerprints []*model.Fingerprint
	passiveFpMap := make(map[string]*model.Fingerprint)

	resp, err := w.loadFingerprints(ctx, true)
	if err != nil {
		w.logger.Error("GetFingerprints HTTP failed: %v", err)
		// 不直接返回，继续尝试加载主动指纹
	} else if !resp.Success {
		w.logger.Error("GetFingerprints failed: %s", resp.Msg)
		// 不直接返回，继续尝试加载主动指纹
	} else {
		// 转换为model.Fingerprint（被动指纹）
		for _, fp := range resp.Fingerprints {
			mfp := &model.Fingerprint{
				Name:          fp.Name,
				Category:      fp.Category,
				Rule:          fp.Rule,
				Source:        fp.Source,
				Headers:       fp.Headers,
				Cookies:       fp.Cookies,
				HTML:          fp.Html,
				Scripts:       fp.Scripts,
				ScriptSrc:     fp.ScriptSrc,
				Meta:          fp.Meta,
				CSS:           fp.Css,
				URL:           fp.Url,
				ConflictGroup: fp.ConflictGroup,
				Coexistence:   append([]string(nil), fp.Coexistence...),
				ExclusiveWith: append([]string(nil), fp.ExclusiveWith...),
				IsBuiltin:     fp.IsBuiltin,
				Enabled:       fp.Enabled,
			}
			// 解析ID
			if fp.Id != "" {
				if oid, err := primitive.ObjectIDFromHex(fp.Id); err == nil {
					mfp.Id = oid
				}
			}
			passiveFingerprints = append(passiveFingerprints, mfp)
			// 存入映射（小写名称作为key，支持不区分大小写匹配）
			passiveFpMap[strings.ToLower(fp.Name)] = mfp
		}
	}

	// 如果启用主动扫描，加载主动指纹
	var activeFingerprints []*model.Fingerprint
	if activeScan {
		activeResp, err := w.loadActiveFingerprints(ctx, true)
		if err != nil {
			w.logger.Warn("GetActiveFingerprints HTTP failed: %v", err)
		} else if activeResp.Success && len(activeResp.Fingerprints) > 0 {
			for _, afp := range activeResp.Fingerprints {
				// 创建主动指纹对象，直接使用API返回的规则（已包含关联的被动指纹规则）
				mfp := &model.Fingerprint{
					Name:        afp.Name,
					ActivePaths: afp.Paths,
					Enabled:     afp.Enabled,
					Type:        model.FingerprintTypeActive,
					// 使用API返回的匹配规则（服务端已关联被动指纹）
					Rule:      afp.Rule,
					Headers:   afp.Headers,
					Cookies:   afp.Cookies,
					HTML:      afp.Html,
					Scripts:   afp.Scripts,
					ScriptSrc: afp.ScriptSrc,
					Meta:      afp.Meta,
					CSS:       afp.Css,
					URL:       afp.Url,
				}

				// 如果API没有返回规则，尝试从本地被动指纹映射获取
				if mfp.Rule == "" && len(mfp.HTML) == 0 && len(mfp.Headers) == 0 {
					if passiveFp := passiveFpMap[strings.ToLower(afp.Name)]; passiveFp != nil {
						mfp.Rule = passiveFp.Rule
						mfp.Headers = passiveFp.Headers
						mfp.Cookies = passiveFp.Cookies
						mfp.HTML = passiveFp.HTML
						mfp.Scripts = passiveFp.Scripts
						mfp.ScriptSrc = passiveFp.ScriptSrc
						mfp.Meta = passiveFp.Meta
						mfp.CSS = passiveFp.CSS
						mfp.URL = passiveFp.URL
						mfp.Category = passiveFp.Category
					} else {
						w.logger.Warn("Active fingerprint '%s' has no matching rule", afp.Name)
					}
				}

				// 解析ID
				if afp.Id != "" {
					if oid, err := primitive.ObjectIDFromHex(afp.Id); err == nil {
						mfp.Id = oid
					}
				}
				activeFingerprints = append(activeFingerprints, mfp)
			}
		}
	}

	// 创建自定义指纹引擎并设置到扫描器
	// 即使被动指纹为空，只要有主动指纹也要创建引擎
	if len(passiveFingerprints) > 0 || len(activeFingerprints) > 0 {
		var customEngine *scanner.CustomFingerprintEngine
		if len(activeFingerprints) > 0 {
			customEngine = scanner.NewCustomFingerprintEngineWithActive(passiveFingerprints, activeFingerprints)
		} else {
			customEngine = scanner.NewCustomFingerprintEngine(passiveFingerprints)
		}
		fpScanner.SetCustomFingerprintEngine(customEngine)
	}
	return len(passiveFingerprints), len(activeFingerprints)
}

// filterSkippedHostsAssets 过滤掉因端口阈值超限被跳过的主机的资产
