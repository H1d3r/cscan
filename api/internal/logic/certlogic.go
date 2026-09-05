package logic

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
)

type CertLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCertLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CertLogic {
	return &CertLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SaveCerts worker 上报的证书结果批量 upsert
func (l *CertLogic) SaveCerts(req *types.SaveCertReq) error {
	if len(req.Results) == 0 {
		return nil
	}

	docs := make([]*model.Cert, 0, len(req.Results))
	for _, r := range req.Results {
		authority := r.Authority
		if authority == "" {
			authority = r.Host + ":" + strconv.Itoa(r.Port)
		}
		docs = append(docs, &model.Cert{
			TaskId:       req.MainTaskId,
			Host:         r.Host,
			Port:         r.Port,
			Authority:    authority,
			Subject:      model.CertNameInfo(r.Subject),
			SubjectDN:    r.SubjectDN,
			Issuer:       model.CertNameInfo(r.Issuer),
			IssuerDN:     r.IssuerDN,
			SerialNumber: r.SerialNumber,
			SigAlg:       r.SigAlg,
			NotBefore:    parseCertTime(r.NotBefore),
			NotAfter:     parseCertTime(r.NotAfter),
			Version:      r.Version,
			SANs:         r.SANs,
			Fingerprints: r.Fingerprints,
			IsSelfSigned: r.IsSelfSigned,
		})
	}

	m := l.svcCtx.GetCertModel()
	// 修复 M-15：EnsureIndexes 和 UpsertMany 的错误必须向上返回，不能吞掉
	if err := m.EnsureIndexes(l.ctx); err != nil {
		l.Logger.Errorf("SaveCerts EnsureIndexes Error: %v", err)
		return xerr.NewServerError("ensure indexes failed: " + err.Error())
	}
	if err := m.UpsertMany(l.ctx, docs); err != nil {
		l.Logger.Errorf("SaveCerts UpsertMany Error: %v", err)
		return xerr.NewServerError("upsert certs failed: " + err.Error())
	}
	return nil
}

// GetCertList 证书列表（分页 + 多维过滤，默认按到期时间升序：最紧急在前）
func (l *CertLogic) GetCertList(req *types.CertListReq) (*types.CertListResp, error) {
	filter := l.buildCertFilter(req)

	if req.Page < 1 {
		req.Page = 1
	}
	req.Page, req.PageSize = model.NormalizePage(req.Page, req.PageSize)
	if req.PageSize < 1 {
		req.PageSize = 10
	}

	sortDir := -1
	if req.Sort == "notAfter" {
		sortDir = 1
	}

	m := l.svcCtx.GetCertModel()
	total, err := m.Count(l.ctx, filter)
	if err != nil {
		return nil, xerr.NewServerError("Count cert Error: " + err.Error())
	}
	opt := options.Find().
		SetSkip(int64((req.Page - 1) * req.PageSize)).
		SetLimit(int64(req.PageSize)).
		SetSort(bson.D{{Key: "not_after", Value: sortDir}})
	allResults, err := m.Find(l.ctx, filter, opt)
	if err != nil {
		return nil, xerr.NewServerError("Find cert Error: " + err.Error())
	}

	respList := make([]*types.Cert, 0, len(allResults))
	for _, r := range allResults {
		respList = append(respList, toCertType(r))
	}

	return &types.CertListResp{
		Code:  0,
		Msg:   "success",
		Total: total,
		List:  respList,
	}, nil
}

// GetCertDetail 单条证书详情
func (l *CertLogic) GetCertDetail(req *types.CertDetailReq) (*types.CertDetailResp, error) {
	id := strings.TrimSpace(req.Id)
	if id == "" {
		return &types.CertDetailResp{Code: 400, Msg: "id 不能为空"}, nil
	}

	doc, err := l.svcCtx.GetCertModel().FindByID(l.ctx, id)
	if err != nil {
		return &types.CertDetailResp{Code: 500, Msg: "查询失败"}, nil
	}
	if doc == nil {
		return &types.CertDetailResp{Code: xerr.CertNotFound, Msg: "未找到该证书"}, nil
	}
	return &types.CertDetailResp{
		Code: 0,
		Msg:  "success",
		Data: toCertType(doc),
	}, nil
}

// buildCertFilter 构造证书列表过滤条件
func (l *CertLogic) buildCertFilter(req *types.CertListReq) bson.M {
	filter := bson.M{}
	if req.Query != "" {
		q := regexp.QuoteMeta(req.Query)
		filter["$or"] = []bson.M{
			{"host": bson.M{"$regex": q, "$options": "i"}},
			{"authority": bson.M{"$regex": q, "$options": "i"}},
			{"subject_dn": bson.M{"$regex": q, "$options": "i"}},
			{"issuer_dn": bson.M{"$regex": q, "$options": "i"}},
			{"sans": bson.M{"$regex": q, "$options": "i"}},
		}
	}
	if req.Host != "" {
		filter["host"] = strings.TrimSpace(req.Host)
	}
	if req.Port > 0 {
		filter["port"] = req.Port
	}
	if req.Issuer != "" {
		filter["issuer_dn"] = bson.M{"$regex": regexp.QuoteMeta(req.Issuer), "$options": "i"}
	}
	if req.ExpiredBefore != "" {
		if ts, err := strconv.ParseInt(req.ExpiredBefore, 10, 64); err == nil {
			filter["not_after"] = bson.M{"$lte": time.Unix(ts, 0)}
		}
	}
	if req.ExpiredAfter != "" {
		if ts, err := strconv.ParseInt(req.ExpiredAfter, 10, 64); err == nil {
			if existing, ok := filter["not_after"].(bson.M); ok {
				existing["$gte"] = time.Unix(ts, 0)
			} else {
				filter["not_after"] = bson.M{"$gte": time.Unix(ts, 0)}
			}
		}
	}
	// 有效期筛选：valid=有效（未过期） / invalid=无效（已过期）
	now := time.Now()
	switch req.Validity {
	case "valid":
		if existing, ok := filter["not_after"].(bson.M); ok {
			existing["$gte"] = now
		} else {
			filter["not_after"] = bson.M{"$gte": now}
		}
	case "invalid":
		if existing, ok := filter["not_after"].(bson.M); ok {
			existing["$lt"] = now
		} else {
			filter["not_after"] = bson.M{"$lt": now}
		}
	}
	return filter
}

// parseCertTime 解析证书时间字符串（worker 经 JSON 上报的为 RFC3339 格式）
func parseCertTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		if t2, err2 := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err2 == nil {
			return t2
		}
		return time.Time{}
	}
	return t
}

// toCertType 将 model.Cert 转换为 API 类型（时间格式化）
func toCertType(r *model.Cert) *types.Cert {
	if r == nil {
		return nil
	}
	return &types.Cert{
		Id:           r.Id.Hex(),
		TaskId:       r.TaskId,
		Host:         r.Host,
		Port:         r.Port,
		Authority:    r.Authority,
		Subject:      types.CertNameInfo(r.Subject),
		SubjectDN:    r.SubjectDN,
		Issuer:       types.CertNameInfo(r.Issuer),
		IssuerDN:     r.IssuerDN,
		SerialNumber: r.SerialNumber,
		SigAlg:       r.SigAlg,
		NotBefore:    formatTimeIfNotZero(r.NotBefore),
		NotAfter:     formatTimeIfNotZero(r.NotAfter),
		Version:      r.Version,
		SANs:         r.SANs,
		Fingerprints: r.Fingerprints,
		IsSelfSigned: r.IsSelfSigned,
		CreateTime:   formatTimeIfNotZero(r.CreateTime),
		UpdateTime:   formatTimeIfNotZero(r.UpdateTime),
	}
}
