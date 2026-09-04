package scanner

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"cscan/internal/model"
)

// CustomFingerprintEngine 自定义指纹识别引擎
// 支持ARL finger.json/webapp.json格式的规则语法
// 参考ARL项目的指纹识别方式进行优化
type CustomFingerprintEngine struct {
	fingerprints       []*model.Fingerprint // 被动指纹
	activeFingerprints []*model.Fingerprint // 主动指纹
}

// NewCustomFingerprintEngine 创建自定义指纹引擎
func NewCustomFingerprintEngine(fingerprints []*model.Fingerprint) *CustomFingerprintEngine {
	return &CustomFingerprintEngine{
		fingerprints: fingerprints,
	}
}

// NewCustomFingerprintEngineWithActive 创建包含主动指纹的引擎
func NewCustomFingerprintEngineWithActive(passiveFingerprints, activeFingerprints []*model.Fingerprint) *CustomFingerprintEngine {
	return &CustomFingerprintEngine{
		fingerprints:       passiveFingerprints,
		activeFingerprints: activeFingerprints,
	}
}

// SetActiveFingerprints 设置主动指纹
func (e *CustomFingerprintEngine) SetActiveFingerprints(fingerprints []*model.Fingerprint) {
	e.activeFingerprints = fingerprints
}

// FingerprintData 用于指纹匹配的数据
// 参考ARL的fetch_fingerprint函数设计
type FingerprintData struct {
	Title           string      // 网页标题
	Body            string      // 网页内容（UTF-8）
	BodyBytes       []byte      // 网页原始字节（用于GBK编码匹配）
	Headers         http.Header // HTTP响应头（标准格式）
	HeaderString    string      // HTTP响应头原始字符串（用于httpx等非标准格式匹配）
	Server          string      // Server头
	URL             string      // 请求URL
	FaviconHash     string      // favicon的MMH3 hash（Shodan风格）
	Cookies         string      // Set-Cookie头内容
	ResponseMissing bool        // 响应未取得；仅用于证据完整性，不改变匹配真值
	BodyTruncated   bool        // Body已截断；仅用于证据完整性
	HeaderTruncated bool        // Header已截断；仅用于证据完整性
}

// FingerprintEvidence and FingerprintFinding are scanner-facing aliases for
// the backward-compatible persistence model.
type FingerprintEvidence = model.FingerprintEvidence
type FingerprintFinding = model.FingerprintFinding
type FingerprintFindings = model.FingerprintFindings

// MatchedFingerprint 匹配到的指纹结果
type MatchedFingerprint struct {
	Name      string // 指纹名称
	Id        string // 指纹ID（MongoDB ObjectID）
	Source    string // 指纹来源
	IsBuiltin bool   // 是否内置指纹
}

// GetFingerprintCount 返回已加载的指纹数量
func (e *CustomFingerprintEngine) GetFingerprintCount() int {
	if e == nil {
		return 0
	}
	return len(e.fingerprints)
}

// GetActiveFingerprintCount 返回已加载的主动指纹数量
func (e *CustomFingerprintEngine) GetActiveFingerprintCount() int {
	if e == nil {
		return 0
	}
	return len(e.activeFingerprints)
}

// GetActiveFingerprints 获取主动指纹列表
func (e *CustomFingerprintEngine) GetActiveFingerprints() []*model.Fingerprint {
	if e == nil {
		return nil
	}
	return e.activeFingerprints
}

// ActiveFingerprintTask 主动指纹扫描任务
type ActiveFingerprintTask struct {
	BaseURL     string             // 基础URL（不含路径）
	Path        string             // 要探测的路径
	Fingerprint *model.Fingerprint // 对应的指纹规则
}

// GetActiveFingerprintTasks 获取主动指纹扫描任务列表
// 返回需要扫描的 URL+路径 组合
func (e *CustomFingerprintEngine) GetActiveFingerprintTasks(baseURL string) []ActiveFingerprintTask {
	var tasks []ActiveFingerprintTask
	if e == nil || len(e.activeFingerprints) == 0 {
		return tasks
	}

	for _, fp := range e.activeFingerprints {
		if !fp.Enabled || len(fp.ActivePaths) == 0 {
			continue
		}
		for _, path := range fp.ActivePaths {
			tasks = append(tasks, ActiveFingerprintTask{
				BaseURL:     baseURL,
				Path:        path,
				Fingerprint: fp,
			})
		}
	}
	return tasks
}

// MatchActiveFingerprint 匹配主动指纹
// 对指定路径的响应进行指纹匹配
func (e *CustomFingerprintEngine) MatchActiveFingerprint(fp *model.Fingerprint, data *FingerprintData) bool {
	if fp == nil || !fp.Enabled {
		return false
	}

	// 检查是否有匹配规则
	hasRule := fp.Rule != "" || len(fp.HTML) > 0 || len(fp.Headers) > 0 || len(fp.Scripts) > 0
	if !hasRule {
		// 如果没有规则，尝试通过 title 匹配（回退策略）
		if matchTitleFallback(fp.Name, data.Title) {
			logx.Debugf("Active fingerprint '%s' matched by title fallback: %s", fp.Name, data.Title)
			return true
		}
		logx.Debugf("Active fingerprint '%s' has no matching rule, skipping", fp.Name)
		return false
	}

	// 优先使用Rule字段（ARL格式规则语法）
	if fp.Rule != "" {
		matched := e.matchRule(fp.Rule, data)
		if matched {
			logx.Debugf("Active fingerprint '%s' matched by Rule: %s", fp.Name, fp.Rule)
			return true
		}
		// 规则不匹配时，尝试通过 title 匹配（回退策略）
		if matchTitleFallback(fp.Name, data.Title) {
			logx.Debugf("Active fingerprint '%s' matched by title fallback: %s", fp.Name, data.Title)
			return true
		}
		return false
	}

	// 使用ARL webapp.json格式规则
	if e.matchARLWebappRules(fp, data) {
		logx.Debugf("Active fingerprint '%s' matched by ARL rules", fp.Name)
		return true
	}

	// 使用Wappalyzer格式规则
	if e.matchWappalyzerRules(fp, data) {
		logx.Debugf("Active fingerprint '%s' matched by Wappalyzer rules", fp.Name)
		return true
	}

	// 最后尝试通过 title 匹配（回退策略）
	if matchTitleFallback(fp.Name, data.Title) {
		logx.Debugf("Active fingerprint '%s' matched by title fallback: %s", fp.Name, data.Title)
		return true
	}

	return false
}

// matchTitleFallback 通过 title 进行回退匹配
// 支持忽略大小写、连字符和空格的差异
func matchTitleFallback(fpName, title string) bool {
	if title == "" || fpName == "" {
		return false
	}
	// 标准化：转小写，将连字符替换为空格
	normalizedName := strings.ToLower(strings.ReplaceAll(fpName, "-", " "))
	normalizedTitle := strings.ToLower(strings.ReplaceAll(title, "-", " "))
	return strings.Contains(normalizedTitle, normalizedName)
}

// Match 执行指纹匹配，返回匹配到的应用名称列表（兼容旧接口）
func (e *CustomFingerprintEngine) Match(data *FingerprintData) []string {
	results := e.MatchWithId(data)
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Name
	}
	return names
}

// MatchWithId executes fingerprint matching and projects raw explanatory
// findings back to the legacy name/ID result. This keeps existing callers and
// duplicate preference semantics unchanged.
func (e *CustomFingerprintEngine) MatchWithId(data *FingerprintData) []MatchedFingerprint {
	findings := e.MatchWithEvidence(data)
	var matched []MatchedFingerprint
	matchedByName := make(map[string]int)
	for _, finding := range findings {
		if !finding.RawMatched {
			continue
		}
		match := MatchedFingerprint{
			Name:      finding.Name,
			Id:        finding.FingerprintID,
			Source:    finding.Source,
			IsBuiltin: finding.IsBuiltin,
		}
		key := strings.ToLower(finding.Name)
		if index, exists := matchedByName[key]; exists {
			if shouldPreferFingerprintMatch(match, matched[index]) {
				matched[index] = match
			}
			continue
		}
		matchedByName[key] = len(matched)
		matched = append(matched, match)
	}
	return matched
}

// MatchWithEvidence evaluates every enabled fingerprint with the existing rule
// interpreters and returns one finding per rule. Evidence is derived only after
// RawMatched is decided, so explanation generation cannot alter AND/OR,
// negation, ARL/Wappalyzer, or GBK truth semantics.
func (e *CustomFingerprintEngine) MatchWithEvidence(data *FingerprintData) FingerprintFindings {
	if e == nil || data == nil {
		return FingerprintFindings{}
	}
	findings := make(FingerprintFindings, 0, len(e.fingerprints))
	if len(e.fingerprints) == 0 {
		logx.Debug("[CustomFingerprint] no fingerprints loaded")
		return findings
	}

	logx.Debugf("[CustomFingerprint] MatchWithEvidence: evaluating %d fingerprints (enabled) against url=%s title=%q server=%q bodyLen=%d",
		len(e.fingerprints), data.URL, data.Title, data.Server, len(data.Body))

	for _, fp := range e.fingerprints {
		if !fp.Enabled {
			continue
		}

		rawMatched := e.rawFingerprintMatch(fp, data)
		finding := newFingerprintFinding(fp, rawMatched)
		if rawMatched {
			finding.Evidence = e.collectFingerprintEvidence(fp, data)
			logx.Debugf("[CustomFingerprint] MATCH: source=%s name=%s evidence=%d", finding.Source, fp.Name, len(finding.Evidence))
		} else {
			logx.Debugf("[CustomFingerprint] NO MATCH: source=%s name=%s", finding.Source, fp.Name)
		}
		findings = append(findings, finding)
	}

	logx.Debugf("[CustomFingerprint] MatchWithEvidence result: evaluated=%d", len(findings))
	return findings
}

func (e *CustomFingerprintEngine) rawFingerprintMatch(fp *model.Fingerprint, data *FingerprintData) bool {
	if fp.Rule != "" {
		return e.matchRule(fp.Rule, data)
	}
	if e.matchARLWebappRules(fp, data) {
		return true
	}
	return e.matchWappalyzerRules(fp, data)
}

func newFingerprintFinding(fp *model.Fingerprint, rawMatched bool) FingerprintFinding {
	source := fp.Source
	if source == "" {
		source = "custom"
	}
	return FingerprintFinding{
		FingerprintID: fp.Id.Hex(),
		Name:          fp.Name,
		Source:        source,
		IsBuiltin:     fp.IsBuiltin,
		RawMatched:    rawMatched,
		ConflictGroup: fp.ConflictGroup,
		Coexistence:   append([]string(nil), fp.Coexistence...),
		ExclusiveWith: append([]string(nil), fp.ExclusiveWith...),
	}
}

func (e *CustomFingerprintEngine) collectFingerprintEvidence(fp *model.Fingerprint, data *FingerprintData) []FingerprintEvidence {
	if fp.Rule != "" {
		return e.collectRuleEvidence(fp.Rule, data)
	}
	if e.matchARLWebappRules(fp, data) {
		return e.collectARLEvidence(fp, data)
	}
	if e.matchWappalyzerRules(fp, data) {
		return e.collectWappalyzerEvidence(fp, data)
	}
	return nil
}

func (e *CustomFingerprintEngine) collectRuleEvidence(rule string, data *FingerprintData) []FingerprintEvidence {
	branches := splitByOperator(strings.TrimSpace(rule), "||")
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" || !e.matchRuleAnd(branch, data) {
			continue
		}
		conditions := splitByOperator(branch, "&&")
		evidence := make([]FingerprintEvidence, 0, len(conditions))
		for _, condition := range conditions {
			condition = strings.TrimSpace(condition)
			if condition == "" || !e.matchSingleCondition(condition, data) {
				continue
			}
			channel, negated := evidenceConditionChannel(condition)
			if channel == "" {
				continue
			}
			evidence = append(evidence, newFingerprintEvidence(channel, negated, data))
		}
		return evidence
	}
	return nil
}

func (e *CustomFingerprintEngine) collectARLEvidence(fp *model.Fingerprint, data *FingerprintData) []FingerprintEvidence {
	var evidence []FingerprintEvidence
	for _, keyword := range fp.HTML {
		if e.matchBodyWithEncoding(data, keyword) {
			evidence = append(evidence, newFingerprintEvidence("body", false, data))
		}
	}
	for key, pattern := range fp.Headers {
		if pattern == "" {
			if data.Headers.Get(key) != "" {
				evidence = append(evidence, newFingerprintEvidence("header", false, data))
			}
		} else if containsIgnoreCase(formatHeadersToString(data.Headers), pattern) {
			evidence = append(evidence, newFingerprintEvidence("header", false, data))
		}
	}
	return evidence
}

func (e *CustomFingerprintEngine) collectWappalyzerEvidence(fp *model.Fingerprint, data *FingerprintData) []FingerprintEvidence {
	var evidence []FingerprintEvidence
	if len(fp.Headers) > 0 {
		evidence = append(evidence, newFingerprintEvidence("header", false, data))
	}
	if len(fp.HTML) > 0 || len(fp.Meta) > 0 || len(fp.CSS) > 0 {
		evidence = append(evidence, newFingerprintEvidence("body", false, data))
	}
	if len(fp.Scripts) > 0 || len(fp.ScriptSrc) > 0 {
		evidence = append(evidence, newFingerprintEvidence("script", false, data))
	}
	if len(fp.Cookies) > 0 {
		evidence = append(evidence, newFingerprintEvidence("cookie", false, data))
	}
	if len(fp.URL) > 0 {
		evidence = append(evidence, newFingerprintEvidence("url", false, data))
	}
	return evidence
}

func evidenceConditionChannel(condition string) (string, bool) {
	condition = strings.TrimSpace(condition)
	negated := false
	separator := "=\""
	if strings.Contains(condition, "!=\"") {
		separator = "!=\""
		negated = true
	}
	index := strings.Index(condition, separator)
	if index <= 0 {
		index = strings.Index(condition, "=")
		if index <= 0 {
			return "", negated
		}
	}
	conditionType := strings.ToLower(strings.TrimSpace(condition[:index]))
	switch conditionType {
	case "body", "body_regex", "body_re":
		return "body", negated
	case "title", "title_regex", "title_re":
		return "title", negated
	case "header":
		return "header", negated
	case "server":
		return "header", negated
	case "url":
		return "url", negated
	case "icon_hash", "favicon_hash":
		return "favicon", negated
	case "cookie":
		return "cookie", negated
	case "status":
		return "status", negated
	default:
		return "", negated
	}
}

func newFingerprintEvidence(channel string, negated bool, data *FingerprintData) FingerprintEvidence {
	pattern := channel + " condition"
	if negated {
		pattern += " (negated)"
	}
	material := fingerprintEvidenceMaterial(channel, data)
	digest := sha256.Sum256([]byte(material))
	return FingerprintEvidence{
		Channel:            channel,
		Pattern:            truncateEvidenceSummary(pattern),
		MatchedValueDigest: fmt.Sprintf("sha256:%x", digest[:16]),
		Strength:           fingerprintEvidenceStrength(channel),
		Complete:           fingerprintEvidenceComplete(channel, data),
	}
}

func fingerprintEvidenceMaterial(channel string, data *FingerprintData) string {
	switch channel {
	case "body", "script":
		if data.Body != "" {
			return data.Body
		}
		return string(data.BodyBytes)
	case "title":
		return data.Title
	case "header", "status":
		return formatHeadersToString(data.Headers) + data.HeaderString + data.Server
	case "cookie":
		return data.Cookies + data.HeaderString + formatHeadersToString(data.Headers)
	case "url":
		return data.URL
	case "favicon":
		return data.FaviconHash
	default:
		return ""
	}
}

func fingerprintEvidenceStrength(channel string) string {
	switch channel {
	case "favicon":
		return "strong"
	case "title", "header", "cookie", "script", "url":
		return "medium"
	default:
		return "weak"
	}
}

func fingerprintEvidenceComplete(channel string, data *FingerprintData) bool {
	if data.ResponseMissing || responseDataMissing(data) {
		return false
	}
	switch channel {
	case "body", "script":
		return !data.BodyTruncated && !strings.Contains(strings.ToLower(data.Body), "[truncated]")
	case "header", "cookie", "status":
		return !data.HeaderTruncated && !strings.Contains(strings.ToLower(data.HeaderString), "[truncated]")
	default:
		return true
	}
}

func responseDataMissing(data *FingerprintData) bool {
	return data.Title == "" && data.Body == "" && len(data.BodyBytes) == 0 && len(data.Headers) == 0 &&
		data.HeaderString == "" && data.Server == "" && data.Cookies == "" && data.FaviconHash == ""
}

func truncateEvidenceSummary(summary string) string {
	const maxEvidenceSummaryBytes = 64
	if len(summary) <= maxEvidenceSummaryBytes {
		return summary
	}
	return summary[:maxEvidenceSummaryBytes]
}

func appendOrReplaceMatchedFingerprint(matched []MatchedFingerprint, matchedByName map[string]int, fp *model.Fingerprint) []MatchedFingerprint {
	match := newMatchedFingerprint(fp)
	key := strings.ToLower(fp.Name)
	if index, exists := matchedByName[key]; exists {
		if shouldPreferFingerprintMatch(match, matched[index]) {
			matched[index] = match
		}
		return matched
	}
	matchedByName[key] = len(matched)
	return append(matched, match)
}

func shouldPreferFingerprintMatch(candidate, current MatchedFingerprint) bool {
	if candidate.Source == "wappalyzer" && current.Source != "wappalyzer" {
		return true
	}
	if candidate.IsBuiltin && !current.IsBuiltin {
		return true
	}
	return false
}

func newMatchedFingerprint(fp *model.Fingerprint) MatchedFingerprint {
	source := fp.Source
	if source == "" {
		source = "custom"
	}
	return MatchedFingerprint{
		Name:      fp.Name,
		Id:        fp.Id.Hex(),
		Source:    source,
		IsBuiltin: fp.IsBuiltin,
	}
}

// matchARLWebappRules 匹配ARL webapp.json格式规则
// 参考ARL的fetch_fingerprint函数：单个规则字段内是OR关系
func (e *CustomFingerprintEngine) matchARLWebappRules(fp *model.Fingerprint, data *FingerprintData) bool {
	// 检查是否有ARL格式的规则
	hasRule := false

	// HTML/Body匹配 - 支持UTF-8和GBK双编码匹配
	if len(fp.HTML) > 0 {
		hasRule = true
		for _, keyword := range fp.HTML {
			if e.matchBodyWithEncoding(data, keyword) {
				return true // OR关系，匹配一个即可
			}
		}
	}

	// Headers匹配
	if len(fp.Headers) > 0 {
		hasRule = true
		for key, pattern := range fp.Headers {
			// ARL格式：headers数组中的值直接在整个header中搜索
			if pattern == "" {
				// 只检查key是否存在
				if data.Headers.Get(key) != "" {
					return true
				}
			} else {
				// 检查header值是否包含pattern
				headerStr := formatHeadersToString(data.Headers)
				if containsIgnoreCase(headerStr, pattern) {
					return true
				}
			}
		}
	}

	// 如果没有任何规则，返回false
	return hasRule && false
}

// matchRule 匹配ARL格式规则
// 支持: body="xxx", title="xxx", header="xxx", server="xxx"
// 逻辑: && (AND), || (OR)
func (e *CustomFingerprintEngine) matchRule(rule string, data *FingerprintData) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false
	}

	// 处理OR逻辑 (||) - 优先级低于AND
	parts := splitByOperator(rule, "||")
	if len(parts) > 1 {
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if e.matchRuleAnd(part, data) {
				return true
			}
		}
		return false
	}

	// 没有OR，处理AND逻辑
	return e.matchRuleAnd(rule, data)
}

// matchRuleAnd 处理AND逻辑
func (e *CustomFingerprintEngine) matchRuleAnd(rule string, data *FingerprintData) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false
	}

	parts := splitByOperator(rule, "&&")
	if len(parts) > 1 {
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !e.matchSingleCondition(part, data) {
				return false
			}
		}
		return true
	}

	// 单个条件匹配
	return e.matchSingleCondition(rule, data)
}

// splitByOperator 按操作符分割，考虑引号内的内容
func splitByOperator(rule, op string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(rule); i++ {
		c := rule[i]

		if (c == '"' || c == '\'') && (i == 0 || rule[i-1] != '\\') {
			if !inQuote {
				inQuote = true
				quoteChar = c
			} else if c == quoteChar {
				inQuote = false
			}
		}

		if !inQuote && i+len(op) <= len(rule) && rule[i:i+len(op)] == op {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			i += len(op) - 1
			continue
		}

		current.WriteByte(c)
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}

// matchSingleCondition 匹配单个条件
// 参考ARL的规则格式：body="xxx", title="xxx", header="xxx", icon_hash="xxx"
func (e *CustomFingerprintEngine) matchSingleCondition(condition string, data *FingerprintData) bool {
	condition = strings.TrimSpace(condition)

	// 解析条件: type="value" 或 type!="value"
	var condType, value string
	var negate bool

	if idx := strings.Index(condition, "!=\""); idx > 0 {
		condType = strings.TrimSpace(condition[:idx])
		// idx+2 跳过 !=" 中的 !=，保留开头的引号给 extractQuotedValue 处理
		rawValue := condition[idx+2:]
		value = extractQuotedValue(rawValue)
		negate = true
	} else if idx := strings.Index(condition, "=\""); idx > 0 {
		condType = strings.TrimSpace(condition[:idx])
		// idx+1 跳过 ="中的 =，保留开头的引号给 extractQuotedValue 处理
		rawValue := condition[idx+1:]
		value = extractQuotedValue(rawValue)
		negate = false
	} else {
		// 尝试简单的 = 分割（不带引号的格式）
		if idx := strings.Index(condition, "="); idx > 0 {
			condType = strings.TrimSpace(condition[:idx])
			value = strings.TrimSpace(condition[idx+1:])
			// 去掉可能的引号
			value = strings.Trim(value, "\"'")
			negate = false
		} else {
			return false
		}
	}

	// 空值条件（如 body="" 或 body=）无意义，且 contains(x, "") 恒为 true，
	// 会让规则无条件匹配任何响应（曾导致主动指纹对任意目标恒命中），直接判不匹配
	if value == "" {
		return false
	}

	var result bool
	switch strings.ToLower(condType) {
	case "body":
		// 参考ARL：同时支持UTF-8和GBK编码匹配
		result = e.matchBodyWithEncoding(data, value)
	case "title":
		result = containsIgnoreCase(data.Title, value)
	case "header":
		// 同时检查标准http.Header和原始header字符串
		// 因为httpx返回的header格式可能是非标准的（如set_cookie而不是Set-Cookie）
		headerResult := containsInHeaders(data.Headers, value)
		headerStrResult := containsInHeaderString(data.HeaderString, value)
		result = headerResult || headerStrResult
	case "server":
		result = containsIgnoreCase(data.Server, value)
	case "url":
		result = containsIgnoreCase(data.URL, value)
	case "body_regex", "body_re":
		result = matchRegex(data.Body, value)
	case "title_regex", "title_re":
		result = matchRegex(data.Title, value)
	// ARL特有的icon_hash匹配
	case "icon_hash", "favicon_hash":
		result = e.matchIconHash(data, value)
	// Cookie匹配（用于识别Shiro等框架）
	case "cookie":
		// 同时检查Cookies字段和header字符串中的cookie
		result = containsIgnoreCase(data.Cookies, value) || containsInHeaderString(data.HeaderString, value)
	// 状态码匹配
	case "status":
		// 从HeaderString中提取状态码
		result = strings.Contains(data.HeaderString, value)
	default:
		logx.Debugf("Unknown condition type: %s", condType)
		return false
	}

	if negate {
		return !result
	}
	return result
}

// matchBodyWithEncoding 同时支持UTF-8和GBK编码匹配
// 参考ARL的fetch_fingerprint函数实现
func (e *CustomFingerprintEngine) matchBodyWithEncoding(data *FingerprintData, keyword string) bool {
	// 空关键字 contains 恒为 true，直接判不匹配（防止ARL html字段中的空串规则恒命中）
	if keyword == "" {
		return false
	}
	// 1. 先尝试UTF-8匹配
	if containsIgnoreCase(data.Body, keyword) {
		return true
	}

	// 2. 尝试将keyword转换为GBK编码后在原始字节中匹配
	// 这是ARL的做法：html.encode("gbk") in content
	if len(data.BodyBytes) > 0 {
		gbkKeyword, err := encodeToGBK(keyword)
		if err == nil && len(gbkKeyword) > 0 {
			if bytes.Contains(data.BodyBytes, gbkKeyword) {
				return true
			}
		}
	}

	return false
}

// matchIconHash 匹配favicon hash
// 参考ARL的icon_hash匹配方式
func (e *CustomFingerprintEngine) matchIconHash(data *FingerprintData, expectedHash string) bool {
	if data.FaviconHash == "" || expectedHash == "" {
		return false
	}
	// 支持直接比较或包含比较
	return data.FaviconHash == expectedHash || strings.Contains(data.FaviconHash, expectedHash)
}

// encodeToGBK 将UTF-8字符串转换为GBK编码
func encodeToGBK(s string) ([]byte, error) {
	reader := transform.NewReader(strings.NewReader(s), simplifiedchinese.GBK.NewEncoder())
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(reader)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// formatHeadersToString 将http.Header格式化为字符串
func formatHeadersToString(headers http.Header) string {
	var sb strings.Builder
	for key, values := range headers {
		for _, value := range values {
			sb.WriteString(key)
			sb.WriteString(": ")
			sb.WriteString(value)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// extractQuotedValue 提取引号内的值
// 支持规则值内部包含未转义引号，取最左侧和最右侧引号之间的内容
func extractQuotedValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return s
	}

	if s[0] == '"' || s[0] == '\'' {
		quoteChar := s[0]
		end := -1
		for i := len(s) - 1; i > 0; i-- {
			if s[i] == quoteChar && !isEscapedQuote(s, i) {
				end = i
				break
			}
		}
		if end == -1 {
			end = len(s)
		}
		return unescapeQuotes(s[1:end], quoteChar)
	}

	if s[len(s)-1] == '"' || s[len(s)-1] == '\'' {
		return s[:len(s)-1]
	}

	return s
}

func isEscapedQuote(s string, pos int) bool {
	backslashCount := 0
	for i := pos - 1; i >= 0 && s[i] == '\\'; i-- {
		backslashCount++
	}
	return backslashCount%2 == 1
}

// unescapeQuotes 将转义的引号还原
// 例如：id=\"swagger-ui 还原为 id="swagger-ui
func unescapeQuotes(s string, quoteChar byte) string {
	switch quoteChar {
	case '"':
		return strings.ReplaceAll(s, `\"`, `"`)
	case '\'':
		return strings.ReplaceAll(s, `\'`, `'`)
	}
	return s
}

// containsIgnoreCase 不区分大小写的包含检查
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// containsInHeaders 检查headers中是否包含指定值
func containsInHeaders(headers http.Header, value string) bool {
	if headers == nil || value == "" {
		return false
	}
	valueLower := strings.ToLower(value)
	for key, values := range headers {
		if strings.Contains(strings.ToLower(key), valueLower) {
			return true
		}
		for _, v := range values {
			if strings.Contains(strings.ToLower(v), valueLower) {
				return true
			}
		}
	}
	return false
}

// containsInHeaderString 检查header字符串中是否包含指定值
// 用于处理httpx返回的非标准header格式
func containsInHeaderString(headerStr, value string) bool {
	if headerStr == "" || value == "" {
		return false
	}
	return strings.Contains(strings.ToLower(headerStr), strings.ToLower(value))
}

// matchRegex 正则匹配
func matchRegex(s, pattern string) bool {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return false
	}
	return re.MatchString(s)
}

// matchWappalyzerRules 匹配Wappalyzer格式规则
// Wappalyzer的html、scripts、css等字段是正则表达式
func (e *CustomFingerprintEngine) matchWappalyzerRules(fp *model.Fingerprint, data *FingerprintData) bool {
	hasRule := false
	allMatch := true

	// Headers匹配 - key需要大小写不敏感匹配
	if len(fp.Headers) > 0 {
		hasRule = true
		headerMatch := false
		for key, pattern := range fp.Headers {
			// 遍历响应头，大小写不敏感匹配key
			for hKey, hVal := range data.Headers {
				if strings.EqualFold(hKey, key) {
					headerValue := strings.Join(hVal, " ")
					if pattern == "" {
						// 只要header存在就匹配
						headerMatch = true
						break
					}
					// pattern是正则表达式
					if matchRegexOrContains(headerValue, pattern) {
						headerMatch = true
						break
					}
				}
			}
			if headerMatch {
				break
			}
		}
		if !headerMatch {
			allMatch = false
		}
	}

	// HTML匹配 - pattern是正则表达式
	if len(fp.HTML) > 0 && allMatch {
		hasRule = true
		htmlMatch := false
		for _, pattern := range fp.HTML {
			if matchRegexOrContains(data.Body, pattern) {
				htmlMatch = true
				break
			}
		}
		if !htmlMatch {
			allMatch = false
		}
	}

	// Meta匹配
	if len(fp.Meta) > 0 && allMatch {
		hasRule = true
		metaMatch := false
		for name, pattern := range fp.Meta {
			if matchMetaTag(data.Body, name, pattern) {
				metaMatch = true
				break
			}
		}
		if !metaMatch {
			allMatch = false
		}
	}

	// Scripts匹配 - pattern是正则表达式，匹配script标签的src属性
	if len(fp.Scripts) > 0 && allMatch {
		hasRule = true
		scriptMatch := false
		// 提取所有script src
		scriptSrcRe := regexp.MustCompile(`(?i)<script[^>]*src=["']([^"']+)["']`)
		scriptSrcs := scriptSrcRe.FindAllStringSubmatch(data.Body, -1)
		for _, pattern := range fp.Scripts {
			for _, src := range scriptSrcs {
				if len(src) > 1 && matchRegexOrContains(src[1], pattern) {
					scriptMatch = true
					break
				}
			}
			if scriptMatch {
				break
			}
		}
		if !scriptMatch {
			allMatch = false
		}
	}

	// ScriptSrc匹配
	if len(fp.ScriptSrc) > 0 && allMatch {
		hasRule = true
		scriptSrcMatch := false
		scriptSrcRe := regexp.MustCompile(`(?i)<script[^>]*src=["']([^"']+)["']`)
		scriptSrcs := scriptSrcRe.FindAllStringSubmatch(data.Body, -1)
		for _, pattern := range fp.ScriptSrc {
			for _, src := range scriptSrcs {
				if len(src) > 1 && matchRegexOrContains(src[1], pattern) {
					scriptSrcMatch = true
					break
				}
			}
			if scriptSrcMatch {
				break
			}
		}
		if !scriptSrcMatch {
			allMatch = false
		}
	}

	// CSS匹配
	if len(fp.CSS) > 0 && allMatch {
		hasRule = true
		cssMatch := false
		for _, pattern := range fp.CSS {
			if matchRegexOrContains(data.Body, pattern) {
				cssMatch = true
				break
			}
		}
		if !cssMatch {
			allMatch = false
		}
	}

	// Cookies匹配
	if len(fp.Cookies) > 0 && allMatch {
		hasRule = true
		cookieMatch := false
		// 同时检查Cookies字段和header中的Set-Cookie
		cookieStr := data.Cookies
		if cookieStr == "" && data.Headers != nil {
			cookieStr = data.Headers.Get("Set-Cookie")
		}
		for name, pattern := range fp.Cookies {
			if containsIgnoreCase(cookieStr, name) {
				if pattern == "" || matchRegexOrContains(cookieStr, pattern) {
					cookieMatch = true
					break
				}
			}
		}
		if !cookieMatch {
			allMatch = false
		}
	}

	// URL匹配
	if len(fp.URL) > 0 && allMatch {
		hasRule = true
		urlMatch := false
		for _, pattern := range fp.URL {
			if matchRegexOrContains(data.URL, pattern) {
				urlMatch = true
				break
			}
		}
		if !urlMatch {
			allMatch = false
		}
	}

	return hasRule && allMatch
}

// matchRegexOrContains 尝试正则匹配，如果正则无效则回退到字符串包含匹配
func matchRegexOrContains(text, pattern string) bool {
	// 空pattern恒匹配任何文本（Wappalyzer字段中的空串规则会恒命中），判不匹配；
	// header/meta的"仅存在性检查"语义在调用方单独处理，不经过此处
	if pattern == "" {
		return false
	}
	// 尝试编译为正则表达式
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		// 正则无效，回退到字符串包含匹配
		return containsIgnoreCase(text, pattern)
	}
	return re.MatchString(text)
}

// matchMetaTag 匹配Meta标签
func matchMetaTag(body, name, pattern string) bool {
	// 简单的meta标签匹配
	metaPattern := `(?i)<meta[^>]*name\s*=\s*["']?` + regexp.QuoteMeta(name) + `["']?[^>]*content\s*=\s*["']([^"']+)["']`
	re, err := regexp.Compile(metaPattern)
	if err != nil {
		return false
	}
	matches := re.FindStringSubmatch(body)
	if len(matches) < 2 {
		return false
	}
	if pattern == "" {
		return true
	}
	return matchRegex(matches[1], pattern)
}

// ParseARLFingerYAML 解析ARL finger.yml格式
type ARLFingerprint struct {
	Name string `yaml:"name"`
	Rule string `yaml:"rule"`
}

// ConvertARLToFingerprint 将ARL格式转换为Fingerprint
func ConvertARLToFingerprint(arl *ARLFingerprint) *model.Fingerprint {
	return &model.Fingerprint{
		Name:      arl.Name,
		Rule:      arl.Rule,
		Source:    "arl",
		IsBuiltin: false,
		Enabled:   true,
		Category:  guessCategory(arl.Name, arl.Rule),
	}
}

// guessCategory 根据名称和规则猜测分类
func guessCategory(name, rule string) string {
	nameLower := strings.ToLower(name)
	ruleLower := strings.ToLower(rule)

	if strings.Contains(nameLower, "oa") || strings.Contains(nameLower, "办公") {
		return "OA"
	}
	if strings.Contains(nameLower, "cms") {
		return "CMS"
	}
	if strings.Contains(nameLower, "erp") {
		return "ERP"
	}
	if strings.Contains(nameLower, "crm") {
		return "CRM"
	}
	if strings.Contains(nameLower, "vpn") || strings.Contains(ruleLower, "vpn") {
		return "VPN"
	}
	if strings.Contains(nameLower, "weblogic") || strings.Contains(nameLower, "tomcat") ||
		strings.Contains(nameLower, "nginx") || strings.Contains(nameLower, "apache") {
		return "Web servers"
	}
	if strings.Contains(nameLower, "spring") || strings.Contains(nameLower, "struts") {
		return "Web frameworks"
	}
	if strings.Contains(nameLower, "kibana") || strings.Contains(nameLower, "grafana") {
		return "Analytics"
	}
	if strings.Contains(nameLower, "jenkins") || strings.Contains(nameLower, "gitlab") {
		return "Dev tools"
	}

	return "Miscellaneous"
}

// CalculateMMH3Hash 计算Shodan风格的MMH3 favicon hash
// 这是ARL和Shodan使用的icon_hash算法
func CalculateMMH3Hash(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// Shodan的favicon hash计算方式：
	// 1. Base64编码
	// 2. 计算MMH3 hash
	b64 := base64.StdEncoding.EncodeToString(data)
	hash := mmh3Hash32([]byte(b64))
	return fmt.Sprintf("%d", int32(hash))
}

// mmh3Hash32 MurmurHash3 32位实现
// 参考Shodan的favicon hash算法
func mmh3Hash32(data []byte) uint32 {
	const (
		c1 = 0xcc9e2d51
		c2 = 0x1b873593
		r1 = 15
		r2 = 13
		m  = 5
		n  = 0xe6546b64
	)

	length := len(data)
	h := uint32(0) // seed = 0

	// 处理4字节块
	nblocks := length / 4
	for i := 0; i < nblocks; i++ {
		k := binary.LittleEndian.Uint32(data[i*4:])
		k *= c1
		k = (k << r1) | (k >> (32 - r1))
		k *= c2

		h ^= k
		h = (h << r2) | (h >> (32 - r2))
		h = h*m + n
	}

	// 处理剩余字节
	tail := data[nblocks*4:]
	var k uint32
	switch len(tail) {
	case 3:
		k ^= uint32(tail[2]) << 16
		fallthrough
	case 2:
		k ^= uint32(tail[1]) << 8
		fallthrough
	case 1:
		k ^= uint32(tail[0])
		k *= c1
		k = (k << r1) | (k >> (32 - r1))
		k *= c2
		h ^= k
	}

	// 最终混合
	h ^= uint32(length)
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16

	return h
}

// ARLFingerJSON ARL finger.json格式的指纹规则
type ARLFingerJSON struct {
	CMS      string   `json:"cms"`
	Method   string   `json:"method"`
	Location string   `json:"location"`
	Keyword  []string `json:"keyword"`
}

// ARLWebappJSON ARL webapp.json格式的指纹规则
type ARLWebappJSON struct {
	Cats     []int    `json:"cats"`
	Headers  []string `json:"headers"`
	HTML     []string `json:"html"`
	Title    []string `json:"title"`
	Icon     string   `json:"icon"`
	Website  string   `json:"website"`
	FofaRule string   `json:"fofa_rule"`
}

// ConvertARLFingerJSONToFingerprint 将ARL finger.json格式转换为Fingerprint
// 参考ARL的finger.json规则格式
func ConvertARLFingerJSONToFingerprint(arl *ARLFingerJSON) *model.Fingerprint {
	fp := &model.Fingerprint{
		Name:      arl.CMS,
		Source:    "arl-finger",
		IsBuiltin: false,
		Enabled:   true,
		Category:  guessCategory(arl.CMS, ""),
	}

	// 解析location字段，确定匹配类型
	// 格式: "rule: body", "rule: title", "rule: icon_hash"
	location := strings.ToLower(arl.Location)

	// 构建ARL格式的规则字符串
	var rules []string
	for _, kw := range arl.Keyword {
		if strings.Contains(location, "body") {
			rules = append(rules, fmt.Sprintf(`body="%s"`, kw))
		} else if strings.Contains(location, "title") {
			rules = append(rules, fmt.Sprintf(`title="%s"`, kw))
		} else if strings.Contains(location, "icon_hash") || strings.Contains(location, "header") {
			// icon_hash在ARL中有时放在header位置，实际是cookie或header匹配
			// 检查是否是数字（icon_hash）
			if isNumeric(kw) || strings.HasPrefix(kw, "-") {
				rules = append(rules, fmt.Sprintf(`icon_hash="%s"`, kw))
			} else {
				// 可能是cookie或header中的关键字
				rules = append(rules, fmt.Sprintf(`header="%s"`, kw))
			}
		}
	}

	// 多个关键字之间是OR关系
	if len(rules) > 0 {
		fp.Rule = strings.Join(rules, " || ")
	}

	return fp
}

// ConvertARLWebappJSONToFingerprint 将ARL webapp.json格式转换为Fingerprint
// 参考ARL的webapp.json规则格式
func ConvertARLWebappJSONToFingerprint(name string, arl *ARLWebappJSON) *model.Fingerprint {
	fp := &model.Fingerprint{
		Name:      name,
		Website:   arl.Website,
		Source:    "arl-webapp",
		IsBuiltin: false,
		Enabled:   true,
		Category:  guessCategory(name, ""),
	}

	// 构建规则字符串，各字段内是OR关系
	var allRules []string

	// HTML/Body规则
	for _, html := range arl.HTML {
		allRules = append(allRules, fmt.Sprintf(`body="%s"`, html))
	}

	// Title规则
	for _, title := range arl.Title {
		allRules = append(allRules, fmt.Sprintf(`title="%s"`, title))
	}

	// Headers规则
	for _, header := range arl.Headers {
		allRules = append(allRules, fmt.Sprintf(`header="%s"`, header))
	}

	// 所有规则之间是OR关系
	if len(allRules) > 0 {
		fp.Rule = strings.Join(allRules, " || ")
	}

	return fp
}

// isNumeric 检查字符串是否为数字（包括负数）
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	start := 0
	if s[0] == '-' || s[0] == '+' {
		start = 1
	}
	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return start < len(s)
}

// BatchConvertARLFingerJSON 批量转换ARL finger.json格式规则
func BatchConvertARLFingerJSON(rules []ARLFingerJSON) []*model.Fingerprint {
	var fps []*model.Fingerprint
	seen := make(map[string]bool)

	for _, rule := range rules {
		fp := ConvertARLFingerJSONToFingerprint(&rule)
		// 合并同名规则（ARL中同一个CMS可能有多条规则）
		if existing, ok := seen[fp.Name]; ok && existing {
			// 找到已存在的规则，合并Rule字段
			for i, existingFp := range fps {
				if existingFp.Name == fp.Name && fp.Rule != "" {
					if existingFp.Rule != "" {
						fps[i].Rule = existingFp.Rule + " || " + fp.Rule
					} else {
						fps[i].Rule = fp.Rule
					}
					break
				}
			}
		} else {
			fps = append(fps, fp)
			seen[fp.Name] = true
		}
	}

	return fps
}
