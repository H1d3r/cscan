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
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
		}
		if len(pbAsset.Ipv6) > 0 {
			for _, ip := range pbAsset.Ipv6 {
				asset.Ip.IpV6 = append(asset.Ip.IpV6, model.IPV6{
					IPName:   ip.Ip,
					Location: ip.Location,
				})
			}
		}

		// 处理CName
		if pbAsset.Cname != "" {
			asset.CName = pbAsset.Cname
		}

		// 设置Domain字段 - 如果Host不是IP地址，则设置为Domain
		if asset.Category == "domain" || !utils.IsIPAddress(asset.Host) {
			asset.Domain = asset.Host
		}

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
				"app":         asset.App,
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

			if err := assetModel.Update(l.ctx, existing.Id.Hex(), updateFields); err != nil {
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
