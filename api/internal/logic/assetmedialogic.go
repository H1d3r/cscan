package logic

import (
	"context"
	"encoding/base64"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/pkg/xerr"

	"github.com/zeromicro/go-zero/core/logx"
)

type AssetMediaLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetMediaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetMediaLogic {
	return &AssetMediaLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AssetMedia 按资产 ID 批量返回截图与 favicon（仅这两个大字段）。
// 列表接口不再携带截图（单页可达数百 KB），前端拿到列表后用本接口懒加载，
// 表格先渲染、媒体渐进填充，弱网下列表页不再被截图拖慢。
func (l *AssetMediaLogic) AssetMedia(req *types.AssetMediaReq) (*types.AssetMediaResp, error) {
	resp := &types.AssetMediaResp{Code: 0, Msg: "success", Data: []types.AssetMediaItem{}}
	if len(req.Ids) == 0 {
		return resp, nil
	}
	// 上限保护：列表懒加载按页取（≤100），超过按前 100 截断
	if len(req.Ids) > 100 {
		req.Ids = req.Ids[:100]
	}

	assetModel := l.svcCtx.GetAssetModel()
	if assetModel == nil {
		return nil, xerr.NewServerError("asset model not available")
	}

	assets, err := assetModel.FindMediaByIds(l.ctx, req.Ids)
	if err != nil {
		l.Logger.Errorf("[AssetMedia] Find fail: %v", err)
		return nil, xerr.NewServerError("")
	}

	for _, a := range assets {
		item := types.AssetMediaItem{Id: a.Id.Hex()}
		if a.Screenshot != "" {
			item.Screenshot = a.Screenshot
		}
		if len(a.IconHashBytes) > 0 {
			item.IconBase64 = base64.StdEncoding.EncodeToString(a.IconHashBytes)
		}
		resp.Data = append(resp.Data, item)
	}
	return resp, nil
}
