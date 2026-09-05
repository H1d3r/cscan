package logic

import (
	"context"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AssetTargetCertsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetTargetCertsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetTargetCertsLogic {
	return &AssetTargetCertsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetTargetCerts 目标或全局 TLS 证书分页列表（cert 集合按可选 host 范围过滤）。
func (l *AssetTargetCertsLogic) AssetTargetCerts(req *types.AssetTargetCertsReq) (*types.AssetTargetCertsResp, error) {
	filter := bson.M{}
	if strings.TrimSpace(req.TargetId) != "" {
		tType, tValue, err := model.DecodeTargetID(req.TargetId)
		if err != nil {
			return nil, err
		}
		filter["host"] = hostFilterForTarget(tType, tValue)
	}

	certModel := l.svcCtx.GetCertModel()
	if certModel == nil {
		return nil, xerr.NewServerError("cert model not available")
	}

	if q := strings.TrimSpace(req.Query); q != "" {
		filter["$or"] = bson.A{
			bson.M{"host": bson.M{"$regex": ".*" + regexpEscape(q) + ".*", "$options": "i"}},
			bson.M{"subject_dn": bson.M{"$regex": ".*" + regexpEscape(q) + ".*", "$options": "i"}},
			bson.M{"issuer_dn": bson.M{"$regex": ".*" + regexpEscape(q) + ".*", "$options": "i"}},
		}
	}
	req.Page, req.PageSize = normalizeListPage(req.Page, req.PageSize)

	total, err := certModel.Count(l.ctx, filter)
	if err != nil {
		l.Logger.Errorf("[AssetTargetCerts] Count fail: %v", err)
		return nil, xerr.NewServerError("")
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "not_after", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64((req.Page - 1) * req.PageSize)).
		SetLimit(int64(req.PageSize))
	docs, err := certModel.Find(l.ctx, filter, opts)
	if err != nil {
		l.Logger.Errorf("[AssetTargetCerts] Find fail: %v", err)
		return nil, xerr.NewServerError("")
	}

	now := time.Now()
	list := make([]types.AssetTargetCertItem, 0, len(docs))
	for _, c := range docs {
		status := "valid"
		switch {
		case !c.NotAfter.IsZero() && c.NotAfter.Before(now):
			status = "expired"
		case !c.NotAfter.IsZero() && c.NotAfter.Before(now.Add(30*24*time.Hour)):
			status = "expiring"
		}
		sans := c.SANs
		if sans == nil {
			sans = []string{}
		}
		list = append(list, types.AssetTargetCertItem{
			Id:         c.Id.Hex(),
			Host:       c.Host,
			Port:       c.Port,
			Authority:  c.Authority,
			SubjectCN:  c.Subject.CommonName,
			SubjectDN:  c.SubjectDN,
			IssuerOrg:  c.Issuer.Organization,
			IssuerDN:   c.IssuerDN,
			SigAlg:     c.SigAlg,
			NotBefore:  tsMilli(c.NotBefore),
			NotAfter:   tsMilli(c.NotAfter),
			SANs:       sans,
			Status:     status,
			SelfSigned: c.IsSelfSigned,
			CreateTime: tsMilli(c.CreateTime),
		})
	}
	return &types.AssetTargetCertsResp{
		Code: 0, Msg: "success", Page: req.Page, PageSize: req.PageSize, Total: total, List: list,
	}, nil
}
