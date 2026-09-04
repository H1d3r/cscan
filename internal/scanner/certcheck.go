package scanner

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// CertNameInfo 证书主体/颁发者的结构化字段（对齐 ARL cert 集合的可检索性）
type CertNameInfo struct {
	Country      string `json:"country,omitempty"`
	Province     string `json:"province,omitempty"`
	Locality     string `json:"locality,omitempty"`
	Organization string `json:"organization,omitempty"`
	OrgUnit      string `json:"orgUnit,omitempty"`
	CommonName   string `json:"commonName,omitempty"`
	Email        string `json:"email,omitempty"`
}

// CertResult certcheck 单次采集的结构化结果（ARL 风格，对齐 model.Cert）
type CertResult struct {
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	Authority    string            `json:"authority"`
	Subject      CertNameInfo      `json:"subject"`
	SubjectDN    string            `json:"subjectDN"`
	Issuer       CertNameInfo      `json:"issuer"`
	IssuerDN     string            `json:"issuerDN"`
	SerialNumber string            `json:"serialNumber"`
	SigAlg       string            `json:"sigAlg"`
	NotBefore    time.Time         `json:"notBefore"`
	NotAfter     time.Time         `json:"notAfter"`
	Version      int               `json:"version"`
	SANs         []string          `json:"sans,omitempty"`
	Fingerprints map[string]string `json:"fingerprints,omitempty"` // sha1 / sha256 / md5
	IsSelfSigned bool              `json:"isSelfSigned"`
}

// CertCheckScanner TLS 证书采集扫描器（指纹识别附加功能）
type CertCheckScanner struct {
	BaseScanner
}

// certTarget 单目标（已规范化 host:port）
type certTarget struct {
	Host      string
	Port      int
	Authority string
}

// buildCertResult 将 x509 证书解析为结构化 CertResult（ARL 风格：Subject/Issuer 结构化 + DN + SANs + 三指纹）
func buildCertResult(t certTarget, cert *x509.Certificate) *CertResult {
	sans := make([]string, 0, len(cert.DNSNames)+len(cert.IPAddresses))
	sans = append(sans, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}

	sha1Sum := sha1SumBytes(cert.Raw)
	sha256Sum := sha256SumBytes(cert.Raw)
	md5Sum := md5SumBytes(cert.Raw)
	fingerprints := map[string]string{
		"sha1":   hex.EncodeToString(sha1Sum[:]),
		"sha256": hex.EncodeToString(sha256Sum[:]),
		"md5":    hex.EncodeToString(md5Sum[:]),
	}

	return &CertResult{
		Host:         t.Host,
		Port:         t.Port,
		Authority:    t.Authority,
		Subject:      toCertNameInfo(cert.Subject),
		SubjectDN:    cert.Subject.String(),
		Issuer:       toCertNameInfo(cert.Issuer),
		IssuerDN:     cert.Issuer.String(),
		SerialNumber: cert.SerialNumber.String(),
		SigAlg:       cert.SignatureAlgorithm.String(),
		NotBefore:    cert.NotBefore,
		NotAfter:     cert.NotAfter,
		Version:      cert.Version,
		SANs:         sans,
		Fingerprints: fingerprints,
		IsSelfSigned: bytes.Equal(cert.RawSubject, cert.RawIssuer),
	}
}

// toCertNameInfo 将 pkix.Name 转换为结构化 CertNameInfo
func toCertNameInfo(name pkix.Name) CertNameInfo {
	info := CertNameInfo{
		CommonName:   name.CommonName,
		Organization: joinNonEmpty(name.Organization),
		OrgUnit:      joinNonEmpty(name.OrganizationalUnit),
		Country:      joinNonEmpty(name.Country),
		Province:     joinNonEmpty(name.Province),
		Locality:     joinNonEmpty(name.Locality),
	}
	// EmailAddress 不是 pkix.Name 标准字段，从 RDN 序列中提取（OID 1.2.840.113549.1.9.1）
	for _, atv := range name.Names {
		if atv.Type.Equal(oidEmailAddress) {
			if s, ok := atv.Value.(string); ok {
				info.Email = s
			}
		}
	}
	return info
}

func joinNonEmpty(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return strings.Join(ss, ",")
}

// isHTTPSAsset resolves the asset's persisted protocol evidence instead of
// independently guessing from Service or a well-known port.
func isHTTPSAsset(a *Asset) bool {
	resolution := resolveAssetScheme(a)
	return resolution.HasEvidence && resolution.Scheme == SchemeHTTPS
}

// tlsCertPorts 除 HTTPS 外常见承载 TLS 的端口白名单（ARL 会对所有非 80 端口尝试握手，
// cscan 收敛到一组常见 TLS 端口以避免对非 TLS 端口的大量无效拨号）。
var tlsCertPorts = map[int]bool{
	443: true, 465: true, 636: true, 993: true, 995: true, 9920: true,
	8443: true, 9443: true, 447: true, 448: true, 7474: true, 9000: true, 9090: true,
}

// isCertFetchTarget uses the shared scheme resolver for HTTP assets. Verified
// HTTP (including HTTP:443) is never overridden by the TLS-port fallback;
// non-HTTP TLS services retain the historical certificate-port allowlist.
func isCertFetchTarget(a *Asset) bool {
	if a == nil {
		return false
	}
	resolution := resolveAssetScheme(a)
	if resolution.HasEvidence && resolution.SelectedEvidence.Kind == SchemeEvidenceSuccessfulResponse {
		return resolution.Scheme == SchemeHTTPS
	}
	if normalizeScheme(a.Service) != "" {
		return resolution.HasEvidence && resolution.Scheme == SchemeHTTPS
	}
	if !tlsCertPorts[a.Port] {
		return false
	}
	certResolution := ResolveScheme([]SchemeEvidence{{Scheme: SchemeHTTPS, Kind: SchemeEvidencePortHint}})
	return certResolution.HasEvidence && certResolution.Scheme == SchemeHTTPS
}

// FetchCert 对单个 host:port 执行 TLS 握手并解析证书，失败返回 nil（不影响整体）
func FetchCert(ctx context.Context, host string, port int, timeout time.Duration) *CertResult {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	t := certTarget{Host: host, Port: port, Authority: addr}
	logx.Debugf("[CertCheck] fetching cert host=%s port=%d addr=%s timeout=%v", host, port, addr, timeout)

	type dialResult struct {
		conn *tls.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	go func() {
		// 抓取场景必须取到过期/域名不匹配的证书——这正是要采集的情形；
		// 因此关闭证书校验，仅建立 TLS 连接读取对端证书。
		c, e := tls.DialWithDialer(
			&net.Dialer{Timeout: timeout},
			"tcp", addr,
			&tls.Config{InsecureSkipVerify: true, ServerName: host},
		)
		ch <- dialResult{conn: c, err: e}
	}()

	select {
	case <-ctx.Done():
		logx.Debugf("[CertCheck] cancelled host=%s port=%d", host, port)
		return nil
	case res := <-ch:
		if res.err != nil {
			logx.Debugf("[CertCheck] TLS dial failed host=%s port=%d err=%v", host, port, res.err)
			return nil
		}
		defer res.conn.Close()
		certs := res.conn.ConnectionState().PeerCertificates
		if len(certs) == 0 {
			logx.Debugf("[CertCheck] no certificates host=%s port=%d", host, port)
			return nil
		}
		result := buildCertResult(t, certs[0])
		logx.Debugf("[CertCheck] cert fetched host=%s port=%d subject=%q issuer=%q notBefore=%s notAfter=%s",
			host, port, result.Subject, result.Issuer, result.NotBefore, result.NotAfter)
		return result
	}
}

// oidEmailAddress PKCS#9 emailAddress OID (1.2.840.113549.1.9.1)
var oidEmailAddress = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}

// sha1SumBytes / sha256SumBytes / md5SumBytes 返回对应哈希的定长字节数组。
func sha1SumBytes(b []byte) [20]byte   { return sha1.Sum(b) }
func sha256SumBytes(b []byte) [32]byte { return sha256.Sum256(b) }
func md5SumBytes(b []byte) [16]byte    { return md5.Sum(b) }

// 保留 strings 引用占位（避免误删后被其他文件引用报错）
var _ = strings.TrimSpace
