package logic

import (
	"context"
	"sort"
	"strings"
	"time"

	"cscan/model"
	"cscan/pkg/utils"
	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// compareAssetChanges 比较资产变更，返回变更详情列表
func compareAssetChanges(old *model.Asset, new *model.Asset) []model.FieldChange {
	var changes []model.FieldChange

	// 比较标题
	if old.Title != new.Title {
		changes = append(changes, model.FieldChange{
			Field:    "title",
			OldValue: truncateForChange(old.Title, 200),
			NewValue: truncateForChange(new.Title, 200),
		})
	}

	// 比较服务
	if old.Service != new.Service {
		changes = append(changes, model.FieldChange{
			Field:    "service",
			OldValue: old.Service,
			NewValue: new.Service,
		})
	}

	// 比较HTTP状态码
	if old.HttpStatus != new.HttpStatus {
		changes = append(changes, model.FieldChange{
			Field:    "httpStatus",
			OldValue: old.HttpStatus,
			NewValue: new.HttpStatus,
		})
	}

	// 比较指纹/应用
	oldApps := sortedJoin(old.App)
	newApps := sortedJoin(new.App)
	if oldApps != newApps {
		changes = append(changes, model.FieldChange{
			Field:    "app",
			OldValue: truncateForChange(oldApps, 500),
			NewValue: truncateForChange(newApps, 500),
		})
	}

	// 比较IconHash
	if old.IconHash != new.IconHash {
		changes = append(changes, model.FieldChange{
			Field:    "iconHash",
			OldValue: old.IconHash,
			NewValue: new.IconHash,
		})
	}

	// 比较Server
	if old.Server != new.Server {
		changes = append(changes, model.FieldChange{
			Field:    "server",
			OldValue: old.Server,
			NewValue: new.Server,
		})
	}

	// 比较Banner（截断）
	if old.Banner != new.Banner {
		changes = append(changes, model.FieldChange{
			Field:    "banner",
			OldValue: truncateForChange(old.Banner, 200),
			NewValue: truncateForChange(new.Banner, 200),
		})
	}

	return changes
}

// sortedJoin 排序后拼接字符串数组
func sortedJoin(arr []string) string {
	if len(arr) == 0 {
		return ""
	}
	sorted := make([]string, len(arr))
	copy(sorted, arr)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}

// truncateForChange 截断字符串用于变更记录
func truncateForChange(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

type SaveTaskResultLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveTaskResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveTaskResultLogic {
	return &SaveTaskResultLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// SaveTaskResult 保存任务结果
func (l *SaveTaskResultLogic) SaveTaskResult(in *pb.SaveTaskResultReq) (*pb.SaveTaskResultResp, error) {
	if len(in.Assets) == 0 {
		return &pb.SaveTaskResultResp{
			Success: true,
			Message: "No assets to save",
		}, nil
	}

	workspaceId := in.WorkspaceId
	if workspaceId == "" {
		workspaceId = "default"
	}

	assetModel := l.svcCtx.GetAssetModel(workspaceId)

	var totalAsset, newAsset, updateAsset int32
	now := time.Now()

	for _, pbAsset := range in.Assets {
		// 转换为model.Asset
		asset := &model.Asset{
			Authority:     pbAsset.Authority,
			Host:          pbAsset.Host,
			Port:          int(pbAsset.Port),
			Category:      pbAsset.Category,
			Service:       pbAsset.Service,
			Title:         pbAsset.Title,
			App:           pbAsset.App,
			HttpStatus:    pbAsset.HttpStatus,
			HttpHeader:    pbAsset.HttpHeader,
			HttpBody:      pbAsset.HttpBody,
			IconHash:      pbAsset.IconHash,
			IconHashBytes: pbAsset.IconData,
			Screenshot:    pbAsset.Screenshot,
			Server:        pbAsset.Server,
			Banner:        pbAsset.Banner,
			IsHTTP:        pbAsset.IsHttp,
			TaskId:        in.MainTaskId,
			Source:        pbAsset.Source,
			OrgId:         in.OrgId,
		}

		// 如果Source为空，设置默认值
		if asset.Source == "" {
			asset.Source = "scan"
		}

		// 处理IP信息
		if len(pbAsset.Ipv4) > 0 {
			for _, ip := range pbAsset.Ipv4 {
				asset.Ip.IpV4 = append(asset.Ip.IpV4, model.IPV4{
					IPName:   ip.Ip,
					Location: ip.Location,
				})
			}
		} else if utils.IsIPv4(asset.Host) {
			asset.Ip.IpV4 = append(asset.Ip.IpV4, model.IPV4{
				IPName: asset.Host,
			})
		}

		if len(pbAsset.Ipv6) > 0 {
			for _, ip := range pbAsset.Ipv6 {
				asset.Ip.IpV6 = append(asset.Ip.IpV6, model.IPV6{
					IPName:   ip.Ip,
					Location: ip.Location,
				})
			}
		} else if utils.IsIPv6(asset.Host) {
			asset.Ip.IpV6 = append(asset.Ip.IpV6, model.IPV6{
				IPName: asset.Host,
			})
		}

		// 处理CName
		if pbAsset.Cname != "" {
			asset.CName = pbAsset.Cname
		}

		// 设置Domain字段 - 如果Host不是IP地址，则设置为Domain
		if asset.Category == "domain" || !utils.IsIPAddress(asset.Host) {
			asset.Domain = asset.Host
		}

		// ========= 尝试继承基础域名的IP和CName =========
		if asset.Port > 0 && len(asset.Ip.IpV4) == 0 && len(asset.Ip.IpV6) == 0 && !utils.IsIPAddress(asset.Host) {
			baseAsset, _ := assetModel.FindByAuthorityOnly(l.ctx, asset.Host)
			if baseAsset != nil {
				asset.Ip = baseAsset.Ip // 继承 IP 数组
				if asset.CName == "" {
					asset.CName = baseAsset.CName // 继承 CName
				}
				if asset.Domain == "" {
					asset.Domain = baseAsset.Domain
				}
				if asset.OrgId == "" {
					asset.OrgId = baseAsset.OrgId
				}
			}
		}

		// ========= 将新资产的 IP/Location 回填到基础域名资产 =========
		// 当端口扫描（如 Naabu）发现了域名对应的 IP 和 Location，
		// 而基础域名资产（由子域名扫描创建）的 Location 为空时，
		// 将 Location 回填到基础域名资产，使前端能立即显示地理位置
		if asset.Port > 0 && (len(asset.Ip.IpV4) > 0 || len(asset.Ip.IpV6) > 0) && !utils.IsIPAddress(asset.Host) {
			baseAsset, _ := assetModel.FindByAuthorityOnly(l.ctx, asset.Host)
			if baseAsset != nil {
				needsUpdate := false
				// 检查是否有 Location 需要回填（基础域名资产的 IP 缺少 Location）
				for i, ipv4 := range baseAsset.Ip.IpV4 {
					if ipv4.Location == "" {
						// 在新资产的 IP 列表中查找匹配的 IP
						for _, newIpv4 := range asset.Ip.IpV4 {
							if newIpv4.IPName == ipv4.IPName && newIpv4.Location != "" {
								baseAsset.Ip.IpV4[i].Location = newIpv4.Location
								needsUpdate = true
								break
							}
						}
					}
				}
				for i, ipv6 := range baseAsset.Ip.IpV6 {
					if ipv6.Location == "" {
						for _, newIpv6 := range asset.Ip.IpV6 {
							if newIpv6.IPName == ipv6.IPName && newIpv6.Location != "" {
								baseAsset.Ip.IpV6[i].Location = newIpv6.Location
								needsUpdate = true
								break
							}
						}
					}
				}
				if needsUpdate {
					if err := assetModel.UpdateWithRaw(l.ctx, baseAsset.Id.Hex(), bson.M{
						"$set": bson.M{"ip": baseAsset.Ip},
					}); err == nil {
						l.Logger.Infof("已回填Location到基础域名资产: %s", asset.Host)
					}
				}
			}
		}
		// ===============================================

		// ========= 当保存带端口的域名资产时，删除同名的无端口资产 =========
		// 这样可以避免同一个域名出现"www.example.com"和"www.example.com:80"两条记录
		// 删除前先合并基础资产的特有字段（CName/Domain/OrgId/IP），防止数据丢失
		if asset.Port > 0 && !utils.IsIPAddress(asset.Host) {
			// 查找同名的无端口资产
			noPortAsset, err := assetModel.FindByAuthorityOnly(l.ctx, asset.Host)
			if err == nil && noPortAsset != nil {
				// 合并基础资产的特有字段到新资产（仅当新资产没有这些字段时）
				if asset.CName == "" && noPortAsset.CName != "" {
					asset.CName = noPortAsset.CName
				}
				if asset.Domain == "" && noPortAsset.Domain != "" {
					asset.Domain = noPortAsset.Domain
				}
				if asset.OrgId == "" && noPortAsset.OrgId != "" {
					asset.OrgId = noPortAsset.OrgId
				}
				// 如果新资产没有 IP 信息，从基础资产继承
				if len(asset.Ip.IpV4) == 0 && len(asset.Ip.IpV6) == 0 && (len(noPortAsset.Ip.IpV4) > 0 || len(noPortAsset.Ip.IpV6) > 0) {
					asset.Ip = noPortAsset.Ip
				}
				// 删除无端口的同名资产
				if deleteErr := assetModel.Delete(l.ctx, noPortAsset.Id.Hex()); deleteErr == nil {
					l.Logger.Infof("已删除同名无端口资产: %s (被 %s:%d 替代)", asset.Host, asset.Host, asset.Port)
				}
			}
		}
		// ===============================================

		// 检查是否已存在
		var existing *model.Asset
		var err error

		if asset.Port > 0 {
			// 有端口的资产，按host:port查找
			existing, err = assetModel.FindByHostPort(l.ctx, asset.Host, asset.Port)
		} else {
			// 无端口的资产（如域名），按authority查找（不限制taskId）
			existing, err = assetModel.FindByAuthorityOnly(l.ctx, asset.Authority)
		}

		if err != nil || existing == nil {
			// 新资产
			asset.Id = primitive.NewObjectID()
			asset.CreateTime = now
			asset.UpdateTime = now
			asset.IsNewAsset = true
			asset.IsUpdated = false
			asset.LastTaskId = ""                 // 新资产没有上一个任务
			asset.FirstSeenTaskId = in.MainTaskId // 记录首次发现的任务ID
			asset.LastStatusChangeTime = now      // 记录状态变化时间

			if err := assetModel.Insert(l.ctx, asset); err != nil {
				l.Logger.Errorf("Insert asset failed: %v", err)
				continue
			}
			newAsset++
		} else {
			// 更新已存在的资产
			// 判断是否是不同任务的更新
			// 只要任务ID不同（或者之前没有任务ID），就认为是新一轮扫描
			isDifferentTask := existing.TaskId != in.MainTaskId

			// 只有当任务ID不同时才保存历史记录（表示是新一轮扫描，需要记录上一次的状态）
			// 这样可以确保在任务的第一次保存时就记录历史，避免后续更新覆盖旧状态
			if isDifferentTask {
				historyModel := l.svcCtx.GetAssetHistoryModel(workspaceId)

				// 检查是否已存在同一任务的历史记录（避免重复）
				exists, _ := historyModel.ExistsByAssetIdAndTaskId(l.ctx, existing.Id.Hex(), existing.TaskId)
				if !exists {
					// 计算变更详情
					changes := compareAssetChanges(existing, asset)

					// 只有当有实际变更时才保存历史记录
					if len(changes) > 0 {
						// 保存上一次扫描的状态作为历史记录
						history := &model.AssetHistory{
							AssetId:    existing.Id.Hex(),
							Authority:  existing.Authority,
							Host:       existing.Host,
							Port:       existing.Port,
							Service:    existing.Service,
							Title:      existing.Title,
							App:        existing.App,
							HttpStatus: existing.HttpStatus,
							HttpHeader: existing.HttpHeader,
							HttpBody:   existing.HttpBody,
							IconHash:   existing.IconHash,
							Screenshot: existing.Screenshot,
							Banner:     existing.Banner,
							TaskId:     existing.TaskId,     // 使用旧的任务ID
							CreateTime: existing.UpdateTime, // 使用旧的更新时间
							Changes:    changes,             // 记录变更详情
						}
						if err := historyModel.Insert(l.ctx, history); err != nil {
							l.Logger.Errorf("Insert asset history failed: %v", err)
							// 继续更新资产，不中断
						} else {
							l.Logger.Infof("保存资产变更历史: assetId=%s, oldTaskId=%s, newTaskId=%s, changes=%d",
								existing.Id.Hex(), existing.TaskId, in.MainTaskId, len(changes))
						}
					}
				}
			}

			// 更新资产
			updateFields := map[string]interface{}{
				"authority":   asset.Authority,
				"service":     asset.Service,
				"title":       asset.Title,
				"status":      asset.HttpStatus,
				"header":      asset.HttpHeader,
				"body":        asset.HttpBody,
				"icon_hash":   asset.IconHash,
				"screenshot":  asset.Screenshot,
				"server":      asset.Server,
				"banner":      asset.Banner,
				"is_http":     asset.IsHTTP,
				"taskId":      asset.TaskId,
				"update_time": now,
			}

			// 只有不同任务更新时才设置更新标签
			if isDifferentTask {
				updateFields["update"] = true
				updateFields["new"] = false
				updateFields["last_task_id"] = existing.TaskId // 记录上一个任务ID
				updateFields["last_status_change_time"] = now  // 记录状态变化时间
				updateAsset++
			}
			// 同一任务内的更新不改变 new/update 标签

			// 更新 IconData
			if len(asset.IconHashBytes) > 0 {
				updateFields["icon_hash_bytes"] = asset.IconHashBytes
			}

			// 更新IP信息
			if len(asset.Ip.IpV4) > 0 || len(asset.Ip.IpV6) > 0 {
				updateFields["ip"] = asset.Ip
			}

			// 更新CName
			if asset.CName != "" {
				updateFields["cname"] = asset.CName
			}

			// 更新Domain
			if asset.Domain != "" {
				updateFields["domain"] = asset.Domain
			}

			// 更新OrgId
			if asset.OrgId != "" {
				updateFields["org_id"] = asset.OrgId
			}

			// 更新Source
			if asset.Source != "" {
				updateFields["source"] = asset.Source
			}

			// 更新Category
			if asset.Category != "" {
				updateFields["category"] = asset.Category
			}

			rawUpdate := bson.M{"$set": updateFields}
			if len(asset.App) > 0 {
				rawUpdate["$addToSet"] = bson.M{
					"app": bson.M{"$each": asset.App},
				}
			}

			if err := assetModel.UpdateWithRaw(l.ctx, existing.Id.Hex(), rawUpdate); err != nil {
				l.Logger.Errorf("Update asset failed: %v", err)
				continue
			}
		}
		totalAsset++
	}

	l.Logger.Infof("SaveTaskResult: total=%d, new=%d, update=%d", totalAsset, newAsset, updateAsset)

	return &pb.SaveTaskResultResp{
		Success:     true,
		Message:     "Assets saved successfully",
		TotalAsset:  totalAsset,
		NewAsset:    newAsset,
		UpdateAsset: updateAsset,
	}, nil
}
