package logic

import (
	"context"
	"strings"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// SubdomainDictListLogic 子域名字典列表逻辑
type SubdomainDictListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSubdomainDictListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubdomainDictListLogic {
	return &SubdomainDictListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubdomainDictListLogic) SubdomainDictList(req *types.SubdomainDictListReq) (*types.SubdomainDictListResp, error) {
	dictModel := model.NewSubdomainDictModel(l.svcCtx.MongoDB)

	// 获取列表
	dicts, err := dictModel.FindAll(l.ctx, req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}

	// 获取总数
	total, err := dictModel.Count(l.ctx)
	if err != nil {
		return nil, err
	}

	// 转换为响应类型
	list := make([]types.SubdomainDict, 0, len(dicts))
	for _, d := range dicts {
		list = append(list, types.SubdomainDict{
			Id:          d.Id.Hex(),
			Name:        d.Name,
			Description: d.Description,
			Content:     d.Content,
			WordCount:   d.WordCount,
			Enabled:     d.Enabled,
			IsBuiltin:   d.IsBuiltin,
			CreateTime:  d.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:  d.UpdateTime.Format("2006-01-02 15:04:05"),
		})
	}

	return &types.SubdomainDictListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  list,
	}, nil
}

// SubdomainDictSaveLogic 保存子域名字典逻辑
type SubdomainDictSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSubdomainDictSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubdomainDictSaveLogic {
	return &SubdomainDictSaveLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubdomainDictSaveLogic) SubdomainDictSave(req *types.SubdomainDictSaveReq) (*types.BaseRespWithId, error) {
	dictModel := model.NewSubdomainDictModel(l.svcCtx.MongoDB)

	// 计算词条数量
	wordCount := countWords(req.Content)

	if req.Id != "" {
		// 更新
		dict := &model.SubdomainDict{
			Name:        req.Name,
			Description: req.Description,
			Content:     req.Content,
			WordCount:   wordCount,
			Enabled:     req.Enabled,
		}
		if err := dictModel.Update(l.ctx, req.Id, dict); err != nil {
			return nil, err
		}
		return &types.BaseRespWithId{Code: 0, Msg: "success", Id: req.Id}, nil
	}

	// 新增
	dict := &model.SubdomainDict{
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		WordCount:   wordCount,
		Enabled:     req.Enabled,
		IsBuiltin:   false,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}
	if err := dictModel.Insert(l.ctx, dict); err != nil {
		return nil, err
	}

	return &types.BaseRespWithId{Code: 0, Msg: "success", Id: dict.Id.Hex()}, nil
}

// SubdomainDictDeleteLogic 删除子域名字典逻辑
type SubdomainDictDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSubdomainDictDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubdomainDictDeleteLogic {
	return &SubdomainDictDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubdomainDictDeleteLogic) SubdomainDictDelete(req *types.SubdomainDictDeleteReq) (*types.BaseResp, error) {
	dictModel := model.NewSubdomainDictModel(l.svcCtx.MongoDB)

	if err := dictModel.Delete(l.ctx, req.Id); err != nil {
		return nil, err
	}

	return &types.BaseResp{Code: 0, Msg: "success"}, nil
}

// SubdomainDictClearLogic 清空子域名字典逻辑
type SubdomainDictClearLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSubdomainDictClearLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubdomainDictClearLogic {
	return &SubdomainDictClearLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubdomainDictClearLogic) SubdomainDictClear() (*types.SubdomainDictClearResp, error) {
	dictModel := model.NewSubdomainDictModel(l.svcCtx.MongoDB)

	// 只删除非内置字典
	deleted, err := dictModel.DeleteNonBuiltin(l.ctx)
	if err != nil {
		return nil, err
	}

	return &types.SubdomainDictClearResp{
		Code:    0,
		Msg:     "success",
		Deleted: int(deleted),
	}, nil
}

// SubdomainDictEnabledListLogic 获取启用的子域名字典列表逻辑
type SubdomainDictEnabledListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSubdomainDictEnabledListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubdomainDictEnabledListLogic {
	return &SubdomainDictEnabledListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubdomainDictEnabledListLogic) SubdomainDictEnabledList() (*types.SubdomainDictEnabledListResp, error) {
	dictModel := model.NewSubdomainDictModel(l.svcCtx.MongoDB)

	dicts, err := dictModel.FindEnabled(l.ctx)
	if err != nil {
		return nil, err
	}

	list := make([]types.SubdomainDictSimple, 0, len(dicts))
	for _, d := range dicts {
		list = append(list, types.SubdomainDictSimple{
			Id:        d.Id.Hex(),
			Name:      d.Name,
			WordCount: d.WordCount,
			IsBuiltin: d.IsBuiltin,
		})
	}

	return &types.SubdomainDictEnabledListResp{
		Code: 0,
		Msg:  "success",
		List: list,
	}, nil
}

// countWords 计算字典中的词条数量
func countWords(content string) int {
	count := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}
	return count
}
