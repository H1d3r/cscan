package worker

import (
	"context"
	"fmt"
	"time"

	"cscan/internal/model"
	"cscan/internal/scanner"
)

// mongoDirectWriteTimeout 同步直写路径的单次写超时上界。
// 原实现直接透传扫描阶段 ctx（最长 6h）：Mongo 挂起时扫描协程会被长时间卡死。
// 这里为直写加独立上界，超时后由 ResultQueue 兜底，扫描主链路最多阻塞该时长。
const mongoDirectWriteTimeout = 180 * time.Second

// saveAssetResultWithFallback 扫描主链路入口：优先投递异步批量落库（AsyncResultWriter）；
// 写协程不可用或通道已满时，回退为同步直写 + 失败入本地队列（保证零丢失）。
func (w *Worker) saveAssetResultWithFallback(ctx context.Context, mainTaskID, orgID string, assets []*scanner.Asset) error {
	if len(assets) == 0 {
		return nil
	}
	if w.asyncWriter != nil && w.asyncWriter.Enqueue(&asyncWriteRequest{
		kind:       asyncWriteAssets,
		mainTaskID: mainTaskID,
		orgID:      orgID,
		assets:     assets,
	}) {
		return nil
	}
	return w.saveAssetResultSyncOrQueue(ctx, mainTaskID, orgID, assets)
}

// saveAssetResultSyncOrQueue 同步直写 MongoDB，失败后将完整请求持久化到本地队列。
// 兼作异步写协程的批量 flush 回调（回调不得再投递回异步通道，避免自循环）。
func (w *Worker) saveAssetResultSyncOrQueue(ctx context.Context, mainTaskID, orgID string, assets []*scanner.Asset) error {
	if len(assets) == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, mongoDirectWriteTimeout)
	defer cancel()
	if err := w.saveAssetResultDirect(writeCtx, mainTaskID, orgID, assets); err == nil {
		return nil
	} else {
		if w.resultQueue == nil {
			return fmt.Errorf("save assets to MongoDB failed: %v; result queue is unavailable", err)
		}
		queueErr := w.resultQueue.Enqueue(&TaskResultReq{
			MainTaskId: mainTaskID,
			OrgId:      orgID,
			Assets:     scannerAssetsToDocuments(assets),
		})
		if queueErr != nil {
			return fmt.Errorf("save assets to MongoDB failed: %v; queue fallback failed: %w", err, queueErr)
		}
		w.taskLog(mainTaskID, LevelWarn, "MongoDB asset save failed; queued %d assets for replay: %v", len(assets), err)
		return nil
	}
}

// saveVulResultWithFallback 扫描主链路入口：优先投递异步批量落库，不可用时回退同步直写。
func (w *Worker) saveVulResultWithFallback(ctx context.Context, mainTaskID string, vuls []*scanner.Vulnerability) error {
	if len(vuls) == 0 {
		return nil
	}
	if w.asyncWriter != nil && w.asyncWriter.Enqueue(&asyncWriteRequest{
		kind:       asyncWriteVuls,
		mainTaskID: mainTaskID,
		vuls:       vuls,
	}) {
		return nil
	}
	return w.saveVulResultSyncOrQueue(ctx, mainTaskID, vuls)
}

// saveVulResultSyncOrQueue 同步直写 MongoDB，失败后将漏洞请求持久化到本地队列。
// 兼作异步写协程的批量 flush 回调。
func (w *Worker) saveVulResultSyncOrQueue(ctx context.Context, mainTaskID string, vuls []*scanner.Vulnerability) error {
	if len(vuls) == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, mongoDirectWriteTimeout)
	defer cancel()
	if err := w.saveVulResultDirect(writeCtx, mainTaskID, vuls); err == nil {
		return nil
	} else {
		req := &VulResultReq{MainTaskId: mainTaskID, Vuls: make([]VulDocument, 0, len(vuls))}
		for _, vul := range vuls {
			req.Vuls = append(req.Vuls, ToVulDocument(vul, mainTaskID))
		}
		if w.resultQueue == nil {
			return fmt.Errorf("save vulnerabilities to MongoDB failed: %w", err)
		}
		if queueErr := w.resultQueue.EnqueueVul(&TaskResultReq{MainTaskId: mainTaskID}, req.Vuls); queueErr != nil {
			return fmt.Errorf("save vulnerabilities to MongoDB failed: %v; queue fallback failed: %w", err, queueErr)
		}
		w.taskLog(mainTaskID, LevelWarn, "MongoDB vulnerability save failed; queued %d vulnerabilities for replay: %v", len(vuls), err)
		return nil
	}
}

// saveCertResultsWithFallback 扫描主链路入口：优先投递异步批量落库，不可用时回退同步直写。
// 此前 OnCertFound 逐张同步直写（每张一次 EnsureIndexes+Upsert），是连接池打满的主要放大源；
// 异步化后按 100 张/批合并为一次 SaveCerts。
func (w *Worker) saveCertResultsWithFallback(ctx context.Context, mainTaskID string, certs []*scanner.CertResult) error {
	if len(certs) == 0 {
		return nil
	}
	if w.asyncWriter != nil && w.asyncWriter.Enqueue(&asyncWriteRequest{
		kind:       asyncWriteCerts,
		mainTaskID: mainTaskID,
		certs:      certs,
	}) {
		return nil
	}
	return w.saveCertResultsSyncOrQueue(ctx, mainTaskID, certs)
}

// saveCertResultsSyncOrQueue 同步直写 MongoDB，失败后将证书请求持久化到本地队列。
// 兼作异步写协程的批量 flush 回调。
func (w *Worker) saveCertResultsSyncOrQueue(ctx context.Context, mainTaskID string, certs []*scanner.CertResult) error {
	if len(certs) == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, mongoDirectWriteTimeout)
	defer cancel()
	if err := w.saveCertResultsDirect(writeCtx, mainTaskID, certs); err == nil {
		return nil
	} else {
		req := &SaveCertResultReq{MainTaskId: mainTaskID, Results: make([]*CertResultItem, 0, len(certs))}
		for _, cert := range certs {
			req.Results = append(req.Results, certResultToItem(cert))
		}
		if w.resultQueue == nil {
			return fmt.Errorf("save certificates to MongoDB failed: %w", err)
		}
		if queueErr := w.resultQueue.EnqueueCert(req); queueErr != nil {
			return fmt.Errorf("save certificates to MongoDB failed: %v; queue fallback failed: %w", err, queueErr)
		}
		w.taskLog(mainTaskID, LevelWarn, "MongoDB certificate save failed; queued %d certificates for replay: %v", len(certs), err)
		return nil
	}
}

// saveJSFinderResultWithFallback 扫描主链路入口：优先投递异步批量落库，不可用时回退同步直写。
func (w *Worker) saveJSFinderResultWithFallback(ctx context.Context, mainTaskID string, results []*JSFinderResultItem) error {
	if len(results) == 0 {
		return nil
	}
	if w.asyncWriter != nil && w.asyncWriter.Enqueue(&asyncWriteRequest{
		kind:       asyncWriteJS,
		mainTaskID: mainTaskID,
		jsResults:  results,
	}) {
		return nil
	}
	return w.saveJSFinderResultSyncOrQueue(ctx, mainTaskID, results)
}

// saveJSFinderResultSyncOrQueue 同步直写 MongoDB，失败后将 JS 结果持久化到本地队列。
// 兼作异步写协程的批量 flush 回调。
func (w *Worker) saveJSFinderResultSyncOrQueue(ctx context.Context, mainTaskID string, results []*JSFinderResultItem) error {
	if len(results) == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, mongoDirectWriteTimeout)
	defer cancel()
	if err := w.saveJSFinderResultDirect(writeCtx, mainTaskID, results); err == nil {
		return nil
	} else {
		req := &SaveJSFinderResultReq{MainTaskId: mainTaskID, Results: results}
		if w.resultQueue == nil {
			return fmt.Errorf("save JSFinder results to MongoDB failed: %w", err)
		}
		if queueErr := w.resultQueue.EnqueueJS(req); queueErr != nil {
			return fmt.Errorf("save JSFinder results to MongoDB failed: %v; queue fallback failed: %w", err, queueErr)
		}
		w.taskLog(mainTaskID, LevelWarn, "MongoDB JSFinder save failed; queued %d findings for replay: %v", len(results), err)
		return nil
	}
}

// replayAssetResult 从本地队列重放资产结果到 MongoDB（回放路径不带 fallback，避免二次入队）。
func (w *Worker) replayAssetResult(ctx context.Context, req *TaskResultReq) error {
	if w.mongoDB == nil {
		return fmt.Errorf("mongoDB unavailable; asset replay requires direct MongoDB connection")
	}
	if len(req.Assets) == 0 {
		return nil
	}
	assets := make([]*model.ScannerAsset, len(req.Assets))
	for i := range req.Assets {
		assets[i] = assetDocumentToScannerAsset(&req.Assets[i])
	}
	svc := model.NewAssetWriteService(w.mongoDB)
	result, err := svc.SaveAssets(ctx, req.MainTaskId, req.OrgId, assets)
	if err != nil {
		return err
	}
	w.taskLog(req.MainTaskId, LevelInfo, "[Replay] assets saved: total=%d, new=%d, update=%d",
		result.TotalAsset, result.NewAsset, result.UpdateAsset)
	return nil
}

func assetDocumentToScannerAsset(doc *AssetDocument) *model.ScannerAsset {
	ipv4 := make([]model.ScannerIPInfo, len(doc.Ipv4))
	for i, ip := range doc.Ipv4 {
		ipv4[i] = model.ScannerIPInfo{IP: ip.IP, Location: ip.Location}
	}
	ipv6 := make([]model.ScannerIPInfo, len(doc.Ipv6))
	for i, ip := range doc.Ipv6 {
		ipv6[i] = model.ScannerIPInfo{IP: ip.IP, Location: ip.Location}
	}
	return &model.ScannerAsset{
		Authority:                    doc.Authority,
		Host:                         doc.Host,
		Port:                         int(doc.Port),
		Category:                     doc.Category,
		Service:                      doc.Service,
		Server:                       doc.Server,
		Banner:                       doc.Banner,
		Title:                        doc.Title,
		App:                          doc.App,
		FingerprintFindings:          append(model.FingerprintFindings(nil), doc.FingerprintFindings...),
		FingerprintFindingsCollected: doc.FingerprintFindingsCollected,
		HttpStatus:                   doc.HttpStatus,
		HttpHeader:                   doc.HttpHeader,
		HttpBody:                     doc.HttpBody,
		Cert:                         doc.Cert,
		IconHash:                     doc.IconHash,
		IsCDN:                        doc.IsCdn,
		CName:                        doc.Cname,
		IsCloud:                      doc.IsCloud,
		IsHTTP:                       doc.IsHttp,
		IPV4:                         ipv4,
		IPV6:                         ipv6,
		Screenshot:                   doc.Screenshot,
		Source:                       doc.Source,
		IconData:                     doc.IconData,
	}
}

func certResultToItem(cert *scanner.CertResult) *CertResultItem {
	return &CertResultItem{
		Host: cert.Host, Port: cert.Port, Authority: cert.Authority, Subject: cert.Subject,
		SubjectDN: cert.SubjectDN, Issuer: cert.Issuer, IssuerDN: cert.IssuerDN,
		SerialNumber: cert.SerialNumber, SigAlg: cert.SigAlg, NotBefore: cert.NotBefore,
		NotAfter: cert.NotAfter, Version: cert.Version, SANs: cert.SANs,
		Fingerprints: cert.Fingerprints, IsSelfSigned: cert.IsSelfSigned,
	}
}

func scannerAssetsToDocuments(assets []*scanner.Asset) []AssetDocument {
	documents := make([]AssetDocument, 0, len(assets))
	for _, asset := range assets {
		documents = append(documents, AssetDocument{
			Authority: asset.Authority, Host: asset.Host, Port: int32(asset.Port), Category: asset.Category,
			Service: asset.Service, Server: asset.Server, Banner: asset.Banner, Title: asset.Title,
			App:                          asset.App,
			FingerprintFindings:          append(scanner.FingerprintFindings(nil), asset.FingerprintFindings...),
			FingerprintFindingsCollected: asset.FingerprintFindingsCollected,
			HttpStatus:                   asset.HttpStatus, HttpHeader: asset.HttpHeader, HttpBody: asset.HttpBody,
			Cert: asset.Cert, IconHash: asset.IconHash, IsCdn: asset.IsCDN, Cname: asset.CName, IsCloud: asset.IsCloud,
			Ipv4: ipv4ToDocuments(asset.IPV4), Ipv6: ipv6ToDocuments(asset.IPV6),
			Screenshot: asset.Screenshot, IsHttp: asset.IsHTTP, Source: asset.Source, IconData: asset.IconData,
		})
	}
	return documents
}

func ipv4ToDocuments(ipv4 []scanner.IPInfo) []IPV4Info {
	if len(ipv4) == 0 {
		return nil
	}
	out := make([]IPV4Info, len(ipv4))
	for i, ip := range ipv4 {
		out[i] = IPV4Info{IP: ip.IP, Location: ip.Location}
	}
	return out
}

func ipv6ToDocuments(ipv6 []scanner.IPInfo) []IPV6Info {
	if len(ipv6) == 0 {
		return nil
	}
	out := make([]IPV6Info, len(ipv6))
	for i, ip := range ipv6 {
		out[i] = IPV6Info{IP: ip.IP, Location: ip.Location}
	}
	return out
}

func vulDocumentToScanner(doc *VulDocument) (*scanner.Vulnerability, error) {
	if doc == nil {
		return nil, fmt.Errorf("nil vulnerability document")
	}
	vul := &scanner.Vulnerability{
		Authority: doc.Authority, Host: doc.Host, Port: int(doc.Port), Url: doc.Url,
		PocFile: doc.PocFile, Source: doc.Source, RiskSource: doc.RiskSource, Severity: doc.Severity,
		Extra: doc.Extra, Result: doc.Result, Tags: doc.Tags,
		ExtractedResults: doc.ExtractedResults, ResponseTruncated: valueOrFalse(doc.ResponseTruncated),
	}
	if doc.VulName != nil {
		vul.VulName = *doc.VulName
	}
	if doc.CvssScore != nil {
		vul.CvssScore = *doc.CvssScore
	}
	if doc.CveId != nil {
		vul.CveId = *doc.CveId
	}
	if doc.CweId != nil {
		vul.CweId = *doc.CweId
	}
	if doc.Remediation != nil {
		vul.Remediation = *doc.Remediation
	}
	if doc.References != nil {
		vul.References = doc.References
	}
	if doc.MatcherName != nil {
		vul.MatcherName = *doc.MatcherName
	}
	if doc.CurlCommand != nil {
		vul.CurlCommand = *doc.CurlCommand
	}
	if doc.Request != nil {
		vul.Request = *doc.Request
	}
	if doc.Response != nil {
		vul.Response = *doc.Response
	}
	return vul, nil
}

func valueOrFalse(value *bool) bool {
	return value != nil && *value
}

// saveAssetResultDirect 将扫描资产直接写入 MongoDB
func (w *Worker) saveAssetResultDirect(ctx context.Context, mainTaskID, orgID string, assets []*scanner.Asset) error {
	if w.mongoDB == nil || len(assets) == 0 {
		return nil
	}

	scannerAssets := make([]*model.ScannerAsset, len(assets))
	for i, asset := range assets {
		scannerAssets[i] = scannerAssetToDTO(asset)
	}

	svc := model.NewAssetWriteService(w.mongoDB)
	result, err := svc.SaveAssets(ctx, mainTaskID, orgID, scannerAssets)
	if err != nil {
		w.taskLog(mainTaskID, LevelError, "[MongoDirect] SaveAssets failed: %v", err)
		return err
	}

	w.taskLog(mainTaskID, LevelInfo, "[MongoDirect] assets saved: total=%d, new=%d, update=%d",
		result.TotalAsset, result.NewAsset, result.UpdateAsset)
	return nil
}

// saveVulResultDirect 将漏洞结果直接写入 MongoDB
func (w *Worker) saveVulResultDirect(ctx context.Context, mainTaskID string, vuls []*scanner.Vulnerability) error {
	if w.mongoDB == nil || len(vuls) == 0 {
		return nil
	}

	scannerVuls := make([]*model.ScannerVulnerability, len(vuls))
	for i, vul := range vuls {
		scannerVuls[i] = scannerVulToDTO(vul)
	}

	svc := model.NewVulWriteService(w.mongoDB)
	result, err := svc.SaveVuls(ctx, mainTaskID, scannerVuls)
	if err != nil {
		w.taskLog(mainTaskID, LevelError, "[MongoDirect] SaveVuls failed: %v", err)
		return err
	}

	w.taskLog(mainTaskID, LevelInfo, "[MongoDirect] vuls saved: total=%d, new=%d",
		result.SavedCount, result.NewVulCount)
	return nil
}

// saveCertResultsDirect 将证书结果直接写入 MongoDB
func (w *Worker) saveCertResultsDirect(ctx context.Context, mainTaskID string, certs []*scanner.CertResult) error {
	if w.mongoDB == nil || len(certs) == 0 {
		return nil
	}

	scannerCerts := make([]*model.ScannerCert, len(certs))
	for i, c := range certs {
		scannerCerts[i] = &model.ScannerCert{
			Host:         c.Host,
			Port:         c.Port,
			Authority:    c.Authority,
			Subject:      model.CertNameInfo(c.Subject),
			SubjectDN:    c.SubjectDN,
			Issuer:       model.CertNameInfo(c.Issuer),
			IssuerDN:     c.IssuerDN,
			SerialNumber: c.SerialNumber,
			SigAlg:       c.SigAlg,
			NotBefore:    c.NotBefore,
			NotAfter:     c.NotAfter,
			Version:      c.Version,
			SANs:         c.SANs,
			Fingerprints: c.Fingerprints,
			IsSelfSigned: c.IsSelfSigned,
		}
	}

	svc := model.NewCertWriteService(w.mongoDB)
	if err := svc.SaveCerts(ctx, mainTaskID, scannerCerts); err != nil {
		w.taskLog(mainTaskID, LevelError, "[MongoDirect] SaveCerts failed: %v", err)
		return err
	}

	w.taskLog(mainTaskID, LevelInfo, "[MongoDirect] certs saved: %d certificates", len(certs))
	return nil
}

// saveJSFinderResultDirect 将 JSFinder 结果直接写入 MongoDB
func (w *Worker) saveJSFinderResultDirect(ctx context.Context, mainTaskID string, results []*JSFinderResultItem) error {
	if w.mongoDB == nil || len(results) == 0 {
		return nil
	}

	scannerResults := make([]*model.ScannerJSFinderResult, len(results))
	for i, r := range results {
		scannerResults[i] = &model.ScannerJSFinderResult{
			Authority:        r.Authority,
			Host:             r.Host,
			Port:             r.Port,
			URL:              r.URL,
			Severity:         r.Severity,
			VulName:          r.VulName,
			Result:           r.Result,
			Tags:             r.Tags,
			MatcherName:      r.MatcherName,
			ExtractedResults: r.ExtractedResults,
			CurlCommand:      r.CurlCommand,
			Request:          r.Request,
			Response:         r.Response,
		}
	}

	svc := model.NewJSFinderWriteService(w.mongoDB)
	if err := svc.SaveResults(ctx, mainTaskID, scannerResults); err != nil {
		w.taskLog(mainTaskID, LevelError, "[MongoDirect] SaveJSFinderResult failed: %v", err)
		return err
	}

	w.taskLog(mainTaskID, LevelInfo, "[MongoDirect] JS results saved: %d findings", len(results))
	return nil
}

// saveDirScanResultsWithFallback 扫描主链路入口：优先投递异步批量落库，不可用时回退同步直写。
func (w *Worker) saveDirScanResultsWithFallback(ctx context.Context, mainTaskID string, results []DirScanResultDocument) error {
	if len(results) == 0 {
		return nil
	}
	if w.asyncWriter != nil && w.asyncWriter.Enqueue(&asyncWriteRequest{
		kind:       asyncWriteDirScan,
		mainTaskID: mainTaskID,
		dirResults: results,
	}) {
		return nil
	}
	return w.saveDirScanResultsSyncOrQueue(ctx, mainTaskID, results)
}

// saveDirScanResultsSyncOrQueue 同步直写 MongoDB，失败后将目录扫描结果持久化到本地队列。
// 兼作异步写协程的批量 flush 回调。
func (w *Worker) saveDirScanResultsSyncOrQueue(ctx context.Context, mainTaskID string, results []DirScanResultDocument) error {
	if len(results) == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, mongoDirectWriteTimeout)
	defer cancel()
	if err := w.saveDirScanResultsDirect(writeCtx, mainTaskID, results); err == nil {
		return nil
	} else {
		req := &SaveDirScanResultReq{MainTaskId: mainTaskID, Results: results}
		if w.resultQueue == nil {
			return fmt.Errorf("save dirscan results to MongoDB failed: %w", err)
		}
		if queueErr := w.resultQueue.EnqueueDirScan(req); queueErr != nil {
			return fmt.Errorf("save dirscan results to MongoDB failed: %v; queue fallback failed: %w", err, queueErr)
		}
		w.taskLog(mainTaskID, LevelWarn, "MongoDB dirscan save failed; queued %d results for replay: %v", len(results), err)
		return nil
	}
}

// replayDirScanResult 从本地队列重放目录扫描结果到 MongoDB
func (w *Worker) replayDirScanResult(ctx context.Context, req *SaveDirScanResultReq) error {
	if w.mongoDB == nil {
		return fmt.Errorf("mongoDB unavailable; dirscan replay requires direct MongoDB connection")
	}
	return w.saveDirScanResultsDirect(ctx, req.MainTaskId, req.Results)
}

// saveDirScanResultsDirect 将目录扫描结果直接写入 MongoDB
func (w *Worker) saveDirScanResultsDirect(ctx context.Context, mainTaskID string, results []DirScanResultDocument) error {
	if w.mongoDB == nil || len(results) == 0 {
		return nil
	}

	scannerResults := make([]*model.ScannerDirScanResult, len(results))
	for i, r := range results {
		scannerResults[i] = &model.ScannerDirScanResult{
			Authority:     r.Authority,
			Host:          r.Host,
			Port:          r.Port,
			URL:           r.URL,
			Path:          r.Path,
			StatusCode:    r.StatusCode,
			ContentLength: r.ContentLength,
			ContentType:   r.ContentType,
			Title:         r.Title,
			RedirectURL:   r.RedirectURL,
			ContentWords:  r.ContentWords,
			ContentLines:  r.ContentLines,
			Duration:      r.Duration,
			Request:       r.Request,
			Response:      r.Response,
		}
	}

	svc := model.NewDirScanWriteService(w.mongoDB)
	if err := svc.SaveResults(ctx, mainTaskID, scannerResults); err != nil {
		w.taskLog(mainTaskID, LevelError, "[MongoDirect] SaveDirScanResults failed: %v", err)
		return err
	}

	w.taskLog(mainTaskID, LevelInfo, "[MongoDirect] dir scan results saved: %d paths", len(results))
	return nil
}

// scannerAssetToDTO 将 scanner.Asset 转换为 model.ScannerAsset DTO
func scannerAssetToDTO(asset *scanner.Asset) *model.ScannerAsset {
	dto := &model.ScannerAsset{
		Authority:                    asset.Authority,
		Host:                         asset.Host,
		Port:                         asset.Port,
		Category:                     asset.Category,
		Service:                      asset.Service,
		Title:                        asset.Title,
		App:                          asset.App,
		FingerprintFindings:          append(model.FingerprintFindings(nil), asset.FingerprintFindings...),
		FingerprintFindingsCollected: asset.FingerprintFindingsCollected,
		Cert:                         asset.Cert,
		HttpStatus:                   asset.HttpStatus,
		HttpHeader:                   asset.HttpHeader,
		HttpBody:                     asset.HttpBody,
		IconHash:                     asset.IconHash,
		IconData:                     asset.IconData,
		Screenshot:                   asset.Screenshot,
		Server:                       asset.Server,
		Banner:                       asset.Banner,
		IsCDN:                        asset.IsCDN,
		CName:                        asset.CName,
		IsCloud:                      asset.IsCloud,
		IsHTTP:                       asset.IsHTTP,
		Source:                       asset.Source,
	}

	for _, ip := range asset.IPV4 {
		dto.IPV4 = append(dto.IPV4, model.ScannerIPInfo{
			IP:       ip.IP,
			Location: ip.Location,
		})
	}

	for _, ip := range asset.IPV6 {
		dto.IPV6 = append(dto.IPV6, model.ScannerIPInfo{
			IP:       ip.IP,
			Location: ip.Location,
		})
	}

	return dto
}

// scannerVulToDTO 将 scanner.Vulnerability 转换为 model.ScannerVulnerability DTO
func scannerVulToDTO(vul *scanner.Vulnerability) *model.ScannerVulnerability {
	return &model.ScannerVulnerability{
		Authority:         vul.Authority,
		Host:              vul.Host,
		Port:              vul.Port,
		Url:               vul.Url,
		PocFile:           vul.PocFile,
		Source:            vul.Source,
		RiskSource:        vul.RiskSource,
		Severity:          vul.Severity,
		Result:            vul.Result,
		Extra:             vul.Extra,
		VulName:           vul.VulName,
		Tags:              vul.Tags,
		CvssScore:         vul.CvssScore,
		CveId:             vul.CveId,
		CweId:             vul.CweId,
		Remediation:       vul.Remediation,
		References:        vul.References,
		MatcherName:       vul.MatcherName,
		ExtractedResults:  vul.ExtractedResults,
		CurlCommand:       vul.CurlCommand,
		Request:           vul.Request,
		Response:          vul.Response,
		ResponseTruncated: vul.ResponseTruncated,
	}
}
