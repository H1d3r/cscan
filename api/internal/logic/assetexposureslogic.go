package logic

import (
	"context"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
)

type AssetExposuresLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetExposuresLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetExposuresLogic {
	return &AssetExposuresLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetExposuresLogic) AssetExposures(req *types.AssetExposuresReq) (resp *types.AssetExposuresResp, err error) {
	// 获取资产信息
	assetModel := l.svcCtx.GetAssetModel()
	asset, err := assetModel.FindById(l.ctx, req.AssetId)
	if err != nil {
		l.Logger.Errorf("[AssetExposures] FindById 失败: %v", err)
		return &types.AssetExposuresResp{Code: 500, Msg: "查询失败"}, nil
	}
	if asset == nil {
		l.Logger.Errorf("资产不存在: assetId=%s", req.AssetId)
		return &types.AssetExposuresResp{Code: 404, Msg: "资产不存在"}, nil
	}

	// 查询目录扫描结果
	dirScanModel := model.NewDirScanResultModel(l.svcCtx.MongoDB)

	// 构建查询条件：优先使用资产的 authority，同时支持 host+port 回退匹配
	dirScanFilter := bson.M{
		"$or": []bson.M{
			{"authority": asset.Authority},
			{"host": asset.Host, "port": asset.Port},
		},
	}

	dirScans, err := dirScanModel.FindByFilter(l.ctx, dirScanFilter, 0, 100) // 最多返回100条
	if err != nil {
		l.Logger.Errorf("查询目录扫描结果失败: %v", err)
		dirScans = []model.DirScanResult{}
	}

	// 查询漏洞扫描结果
	vulModel := l.svcCtx.GetVulModel()
	vulFilter := bson.M{
		"host": asset.Host,
		"port": asset.Port,
	}

	vuls, err := vulModel.Find(l.ctx, vulFilter, 1, 100) // 最多返回100条
	if err != nil {
		l.Logger.Errorf("查询漏洞扫描结果失败: %v", err)
		vuls = []model.Vul{}
	}

	// 转换目录扫描结果
	dirScanResults := make([]types.DirScanResultItem, 0, len(dirScans))
	for _, ds := range dirScans {
		dirScanResults = append(dirScanResults, types.DirScanResultItem{
			URL:           ds.URL,
			Path:          ds.Path,
			Status:        ds.StatusCode,
			ContentLength: ds.ContentLength,
			ContentType:   ds.ContentType,
			Title:         ds.Title,
			RedirectURL:   ds.RedirectURL,
		})
	}

	// 转换漏洞扫描结果
	vulnResults := make([]types.VulnResultItem, 0, len(vuls))
	for _, v := range vuls {
		vulnResults = append(vulnResults, types.VulnResultItem{
			ID:           v.Id.Hex(),
			Name:         v.PocFile,
			Severity:     v.Severity,
			URL:          v.Url,
			Description:  v.Extra,
			CVE:          v.CveId,
			CVSS:         v.CvssScore,
			MatchedURL:   v.Url,
			DiscoveredAt: v.CreateTime.Format("2006-01-02 15:04:05"),
		})
	}

	l.Logger.Infof("资产 %s 暴露面数据: 目录扫描=%d, 漏洞扫描=%d", req.AssetId, len(dirScanResults), len(vulnResults))

	return &types.AssetExposuresResp{
		Code:           0,
		Msg:            "success",
		DirScanResults: dirScanResults,
		VulnResults:    vulnResults,
	}, nil
}
