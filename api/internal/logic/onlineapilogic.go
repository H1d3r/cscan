package logic

import (
	"context"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"
	"cscan/onlineapi"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type OnlineAPILogic struct {
	ctx context.Context
	svc *svc.ServiceContext
}

func NewOnlineAPILogic(ctx context.Context, svc *svc.ServiceContext) *OnlineAPILogic {
	return &OnlineAPILogic{ctx: ctx, svc: svc}
}

// parseApps 解析指纹字符串，支持逗号分隔，过滤空值
func parseApps(product string) []string {
	if product == "" {
		return nil
	}

	var apps []string
	// 支持中英文逗号分隔
	parts := strings.FieldsFunc(product, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；'
	})

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			apps = append(apps, p)
		}
	}

	return apps
}

func (l *OnlineAPILogic) Search(req *types.OnlineSearchReq, workspaceId string) (*types.OnlineSearchResp, error) {
	// 获取API配置
	configModel := model.NewAPIConfigModel(l.svc.MongoDB, workspaceId)
	config, err := configModel.FindByPlatform(l.ctx, req.Platform)
	if err != nil {
		return &types.OnlineSearchResp{Code: 404, Msg: "未配置" + req.Platform + "的API密钥"}, nil
	}

	var results []types.OnlineSearchResult
	var total int

	switch req.Platform {
	case "fofa":
		client := onlineapi.NewFofaClient(config.Key, config.Version)
		result, err := client.Search(l.ctx, req.Query, req.Page, req.PageSize)
		if err != nil {
			return &types.OnlineSearchResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
		}
		total = result.Size
		assets := client.ParseResults(result)
		for _, a := range assets {
			results = append(results, types.OnlineSearchResult{
				Host: a.Host, IP: a.IP, Port: a.Port, Protocol: a.Protocol,
				Domain: a.Domain, Title: a.Title, Server: a.Server,
				Country: a.Country, City: a.City, Banner: a.Banner,
				ICP: a.ICP, Product: a.Product, OS: a.OS,
			})
		}
	case "hunter":
		client := onlineapi.NewHunterClient(config.Key)
		// Hunter API page_size 最大为100
		hunterPageSize := req.PageSize
		if hunterPageSize > 100 {
			hunterPageSize = 100
		}
		result, err := client.Search(l.ctx, req.Query, req.Page, hunterPageSize, "", "")
		if err != nil {
			return &types.OnlineSearchResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
		}
		total = result.Data.Total
		for _, a := range result.Data.Arr {
			component := ""
			if len(a.Component) > 0 {
				component = a.Component[0].Name
			}
			results = append(results, types.OnlineSearchResult{
				Host: a.URL, IP: a.IP, Port: a.Port, Protocol: a.Protocol,
				Domain: a.Domain, Title: a.WebTitle, Server: component,
				Country: a.Country, City: a.City, Banner: a.Banner,
				ICP: a.Number, Product: component, OS: a.OS,
			})
		}
	case "quake":
		client := onlineapi.NewQuakeClient(config.Key)
		result, err := client.Search(l.ctx, req.Query, req.Page, req.PageSize)
		if err != nil {
			return &types.OnlineSearchResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
		}
		// 检查是否配额用尽
		if result.Data.IsExhausted {
			return &types.OnlineSearchResp{Code: 403, Msg: "Quake API 配额已用尽，无法获取更多数据"}, nil
		}
		total = result.Meta.Pagination.Total
		for _, a := range result.Data.Items {
			results = append(results, types.OnlineSearchResult{
				Host: a.Service.HTTP.Host, IP: a.IP, Port: a.Port, Protocol: a.Service.Name,
				Title: a.Service.HTTP.Title, Server: a.Service.HTTP.Server,
				Country: a.Location.CountryCN, City: a.Location.CityCN,
			})
		}
	default:
		return &types.OnlineSearchResp{Code: 400, Msg: "不支持的平台"}, nil
	}

	return &types.OnlineSearchResp{Code: 0, Msg: "success", Total: total, List: results}, nil
}

// cleanHost removes protocol, paths, and port from host string
func cleanHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "http://") {
		host = strings.TrimPrefix(host, "http://")
	} else if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	}
	// Remove path
	if idx := strings.Index(host, "/"); idx > 0 {
		host = host[:idx]
	}
	// Remove port if present (e.g. example.com:8080 -> example.com)
	// Ignore IPv6 brackets for now, assuming standard output from APIs
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		// Verify if it's likely a port (not part of IPv6 address without brackets)
		// Simple check: if there are multiple colons, it might be IPv6.
		// But usually host:port has only one colon or is [ipv6]:port.
		// If input is 1.2.3.4:80, colons=1.
		// If input is example.com:80, colons=1.
		// If input is ::1, colons>1.
		if strings.Count(host, ":") == 1 || strings.Contains(host, "]:") {
			host = host[:idx]
		}
	}
	return host
}

func (l *OnlineAPILogic) Import(req *types.OnlineImportReq, workspaceId string) (*types.BaseResp, error) {
	assetModel := l.svc.GetAssetModel(workspaceId)

	count := 0
	for _, a := range req.Assets {
		apps := parseApps(a.Product)

		// Use IP as host if available, otherwise clean the Host field
		host := a.IP
		if host == "" {
			host = cleanHost(a.Host)
		}

		// Skip if host is empty
		if host == "" {
			continue
		}

		// Construct correct Authority format (host:port)
		authority := fmt.Sprintf("%s:%d", host, a.Port)

		// 自动添加标签
		// 使用 title case: fofa -> Fofa
		platformTag := req.Platform
		if len(platformTag) > 0 {
			platformTag = strings.ToUpper(platformTag[:1]) + platformTag[1:]
		}
		if platformTag == "" {
			platformTag = "OnlineAPI"
		}

		labels := []string{"OnlineAPI", platformTag}

		asset := &model.Asset{
			Authority: authority,
			Host:      host,
			Port:      a.Port,
			Service:   a.Protocol,
			Title:     a.Title,
			App:       apps,
			Source:    "onlineapi", // 明确来源
			Labels:    labels,      // 添加标签
			IsHTTP:    a.Protocol == "http" || a.Protocol == "https",
			// Map optional fields
			Domain: a.Domain,
			Server: a.Server,
			Banner: a.Banner,
			// Initialize default fields to ensure compatibility
			IsNewAsset: true,
			CreateTime: time.Now(),
			UpdateTime: time.Now(),
		}

		// Populate IP info if available
		if a.IP != "" {
			asset.Ip = model.IP{
				IpV4: []model.IPV4{{IPName: a.IP, Location: a.Country + " " + a.City}},
			}
		}

		// 使用 Upsert，如果资产已存在，Upsert 方法内应处理标签合并逻辑(虽然model层目前Upsert是覆盖set，
		// 但通常导入希望覆盖或标记。如果需要保留原有标签，需要修改Model层逻辑，但作为导入功能，标记来源优先)
		// 注意：model.Asset.Upsert 目前是 $set labels，会覆盖旧标签。
		// 为了不覆盖用户已有的自定义标签，我们应该改为 AddLabel 或在 Upsert 中做特殊处理。
		// 但由于 Upsert 是全量更新，这里我们假设导入是“更新/新增”操作。
		// 如果想保留原标签，需要先查后更，这会影响性能。
		// 考虑到性能，暂且覆盖标签或仅在新建时添加。
		// 修正策略：为了简单且高效，我们让 Upsert 处理基本信息，标签作为属性之一。
		// 如果资产已存在，用户可能已经打过标签，覆盖可能不妥。
		// 但在线导入通常是新资产或更新基础信息。
		// 让我们依赖 model.Asset 的逻辑。当前 logic 中构造了 asset 对象。

		if err := assetModel.Upsert(l.ctx, asset); err == nil {
			count++
		}
	}

	return &types.BaseResp{Code: 0, Msg: fmt.Sprintf("成功导入%d条资产", count)}, nil
}

// ImportAll 导入全部资产（自动遍历所有页面）
func (l *OnlineAPILogic) ImportAll(req *types.OnlineImportAllReq, workspaceId string) (*types.OnlineImportAllResp, error) {
	// 获取API配置
	configModel := model.NewAPIConfigModel(l.svc.MongoDB, workspaceId)
	config, err := configModel.FindByPlatform(l.ctx, req.Platform)
	if err != nil {
		return &types.OnlineImportAllResp{Code: 404, Msg: "未配置" + req.Platform + "的API密钥"}, nil
	}

	assetModel := l.svc.GetAssetModel(workspaceId)
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	// Hunter 和 Quake 单次最大 100
	if req.Platform == "hunter" || req.Platform == "quake" {
		if pageSize > 100 {
			pageSize = 100
		}
	}

	// maxPages <= 0 表示不限制页数
	maxPages := req.MaxPages
	hasMaxPages := maxPages > 0

	totalFetched := 0
	totalImport := 0
	currentPage := 1

PageLoop:
	for {
		// 如果设置了最大页数限制，检查是否超过
		if hasMaxPages && currentPage > maxPages {
			break
		}

		var results []types.OnlineSearchResult

		switch req.Platform {
		case "fofa":
			client := onlineapi.NewFofaClient(config.Key, config.Version)
			result, err := client.Search(l.ctx, req.Query, currentPage, pageSize)
			if err != nil {
				if currentPage == 1 {
					return &types.OnlineImportAllResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
				}
				break PageLoop
			}
			assets := client.ParseResults(result)
			for _, a := range assets {
				results = append(results, types.OnlineSearchResult{
					Host: a.Host, IP: a.IP, Port: a.Port, Protocol: a.Protocol,
					Domain: a.Domain, Title: a.Title, Server: a.Server,
					Country: a.Country, City: a.City, Banner: a.Banner,
					ICP: a.ICP, Product: a.Product, OS: a.OS,
				})
			}
		case "hunter":
			client := onlineapi.NewHunterClient(config.Key)
			// Hunter API page_size 最大为100
			hunterPageSize := pageSize
			if hunterPageSize > 100 {
				hunterPageSize = 100
			}
			result, err := client.Search(l.ctx, req.Query, currentPage, hunterPageSize, "", "")
			if err != nil {
				if currentPage == 1 {
					return &types.OnlineImportAllResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
				}
				break PageLoop
			}
			for _, a := range result.Data.Arr {
				component := ""
				if len(a.Component) > 0 {
					component = a.Component[0].Name
				}
				results = append(results, types.OnlineSearchResult{
					Host: a.URL, IP: a.IP, Port: a.Port, Protocol: a.Protocol,
					Domain: a.Domain, Title: a.WebTitle, Server: component,
					Country: a.Country, City: a.City, Banner: a.Banner,
					ICP: a.Number, Product: component, OS: a.OS,
				})
			}
		case "quake":
			client := onlineapi.NewQuakeClient(config.Key)
			result, err := client.Search(l.ctx, req.Query, currentPage, pageSize)
			if err != nil {
				if currentPage == 1 {
					return &types.OnlineImportAllResp{Code: 500, Msg: "查询失败: " + err.Error()}, nil
				}
				break PageLoop
			}
			// 检查是否配额用尽
			if result.Data.IsExhausted {
				break PageLoop
			}
			for _, a := range result.Data.Items {
				results = append(results, types.OnlineSearchResult{
					Host: a.Service.HTTP.Host, IP: a.IP, Port: a.Port, Protocol: a.Service.Name,
					Title: a.Service.HTTP.Title, Server: a.Service.HTTP.Server,
					Country: a.Location.CountryCN, City: a.Location.CityCN,
				})
			}
		default:
			return &types.OnlineImportAllResp{Code: 400, Msg: "不支持的平台"}, nil
		}

		// 没有更多数据了
		if len(results) == 0 {
			break
		}

		totalFetched += len(results)

		// 导入当前页的资产
		for _, a := range results {
			apps := parseApps(a.Product)

			// Use IP as host if available, otherwise clean the Host field
			host := a.IP
			if host == "" {
				host = cleanHost(a.Host)
			}

			// Skip if host is empty
			if host == "" {
				continue
			}

			// Construct correct Authority format (host:port)
			authority := fmt.Sprintf("%s:%d", host, a.Port)

			// 自动添加标签
			platformTag := req.Platform
			if len(platformTag) > 0 {
				platformTag = strings.ToUpper(platformTag[:1]) + platformTag[1:]
			}
			labels := []string{"OnlineAPI", platformTag}

			asset := &model.Asset{
				Authority: authority,
				Host:      host,
				Port:      a.Port,
				Service:   a.Protocol,
				Title:     a.Title,
				App:       apps,
				Source:    "onlineapi-" + req.Platform, // 明确来源
				Labels:    labels,                      // 添加标签
				IsHTTP:    a.Protocol == "http" || a.Protocol == "https",
				// Map optional fields
				Domain: a.Domain,
				Server: a.Server,
				Banner: a.Banner,
				// Initialize default fields
				IsNewAsset: true,
				CreateTime: time.Now(),
				UpdateTime: time.Now(),
			}

			// Populate IP info if available
			if a.IP != "" {
				asset.Ip = model.IP{
					IpV4: []model.IPV4{{IPName: a.IP, Location: a.Country + " " + a.City}},
				}
			}

			if err := assetModel.Upsert(l.ctx, asset); err == nil {
				totalImport++
			}
		}

		// 如果当前页返回的数据量小于 pageSize，说明已经是最后一页
		// 注意：对于 Quake，配额用尽时会返回空数组，上面已经处理
		if len(results) < pageSize {
			break
		}

		currentPage++
	}

	totalPages := currentPage
	return &types.OnlineImportAllResp{
		Code:         0,
		Msg:          fmt.Sprintf("成功导入%d条资产（共获取%d条，%d页）", totalImport, totalFetched, totalPages),
		TotalFetched: totalFetched,
		TotalImport:  totalImport,
		TotalPages:   totalPages,
	}, nil
}

func (l *OnlineAPILogic) ConfigList(workspaceId string) (*types.APIConfigListResp, error) {
	configModel := model.NewAPIConfigModel(l.svc.MongoDB, workspaceId)
	docs, err := configModel.FindAll(l.ctx)
	if err != nil {
		return &types.APIConfigListResp{Code: 500, Msg: "查询失败"}, nil
	}

	list := make([]types.APIConfig, 0, len(docs))
	for _, doc := range docs {
		list = append(list, types.APIConfig{
			Id:         doc.Id.Hex(),
			Platform:   doc.Platform,
			Key:        doc.Key,
			Secret:     maskSecret(doc.Secret),
			Version:    doc.Version,
			Status:     doc.Status,
			CreateTime: doc.CreateTime.Local().Format("2006-01-02 15:04:05"),
		})
	}

	return &types.APIConfigListResp{Code: 0, Msg: "success", List: list}, nil
}

func (l *OnlineAPILogic) ConfigSave(req *types.APIConfigSaveReq, workspaceId string) (*types.BaseResp, error) {
	configModel := model.NewAPIConfigModel(l.svc.MongoDB, workspaceId)

	if req.Id != "" {
		update := bson.M{
			"key":         req.Key,
			"secret":      req.Secret,
			"version":     req.Version,
			"update_time": time.Now(),
		}
		if err := configModel.Update(l.ctx, req.Id, update); err != nil {
			return &types.BaseResp{Code: 500, Msg: "更新失败"}, nil
		}
	} else {
		doc := &model.APIConfig{
			Id:       primitive.NewObjectID(),
			Platform: req.Platform,
			Key:      req.Key,
			Secret:   req.Secret,
			Version:  req.Version,
			Status:   "enable",
		}
		if err := configModel.Insert(l.ctx, doc); err != nil {
			return &types.BaseResp{Code: 500, Msg: "保存失败"}, nil
		}
	}

	return &types.BaseResp{Code: 0, Msg: "保存成功"}, nil
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}
