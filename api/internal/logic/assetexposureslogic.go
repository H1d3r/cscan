package logic

import (
	"context"
	"fmt"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

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

func (l *AssetExposuresLogic) AssetExposures(req *types.AssetExposuresReq, workspaceId string) (resp *types.AssetExposuresResp, err error) {
	// 获取资产信息
	assetModel := l.svcCtx.GetAssetModel(workspaceId)
	asset, err := assetModel.FindById(l.ctx, req.AssetId)
	if err != nil {
		return &types.AssetExposuresResp{Code: 404, Msg: "资产不存在"}, nil
	}

	// 构建查询条件：根据 host 和 port 查询
	authority := fmt.Sprintf("%s:%d", asset.Host, asset.Port)

	// 查询目录扫描结果
	dirScanModel := model.NewDirScanResultModel(l.svcCtx.MongoDB)
	dirScanFilter := bson.M{
		"authority": authority,
	}
	// 如果有工作空间ID，也加上工作空间过滤
	if workspaceId != "" && workspaceId != "all" {
		dirScanFilter["workspace_id"] = workspaceId
	}
	
	dirScans, err := dirScanModel.FindByFilter(l.ctx, dirScanFilter, 0, 100) // 最多返回100条
	if err != nil {
		l.Logger.Errorf("查询目录扫描结果失败: %v", err)
		dirScans = []model.DirScanResult{}
	}

	// 查询漏洞扫描结果
	vulModel := l.svcCtx.GetVulModel(workspaceId)
	vulFilter := bson.M{
		"host": asset.Host,
		"port": asset.Port,
	}
	
	vuls, err := vulModel.Find(l.ctx, vulFilter, 0, 100) // 最多返回100条
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
			ID:          v.Id.Hex(),
			Name:        v.PocFile,
			Severity:    v.Severity,
			URL:         v.Url,
			Description: v.Extra,
			CVE:         v.CveId,
			CVSS:        v.CvssScore,
			MatchedURL:  v.Url,
			DiscoveredAt: v.CreateTime.Format("2006-01-02 15:04:05"),
		})
	}

	return &types.AssetExposuresResp{
		Code:           0,
		Msg:            "success",
		DirScanResults: dirScanResults,
		VulnResults:    vulnResults,
	}, nil
}
