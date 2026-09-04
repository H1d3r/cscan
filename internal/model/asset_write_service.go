package model

import (
	"context"
	"fmt"
	"time"

	"cscan/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ScannerAsset 扫描器资产数据传输对象（避免循环依赖）
type ScannerAsset struct {
	Authority                    string
	Host                         string
	Port                         int
	Category                     string
	Service                      string
	Server                       string
	Banner                       string
	Title                        string
	App                          []string
	FingerprintFindings          FingerprintFindings
	FingerprintFindingsCollected bool
	HttpStatus                   string
	HttpHeader                   string
	HttpBody                     string
	Cert                         string
	IconHash                     string
	IconData                     []byte
	Screenshot                   string
	IsCDN                        bool
	CName                        string
	IsCloud                      bool
	IsHTTP                       bool
	IPV4                         []ScannerIPInfo
	IPV6                         []ScannerIPInfo
	Source                       string
}

// ScannerIPInfo IP信息传输对象
type ScannerIPInfo struct {
	IP       string
	Location string
}

// SaveAssetsResult 资产保存结果
type SaveAssetsResult struct {
	TotalAsset      int32
	NewAsset        int32
	UpdateAsset     int32
	AttemptedWrites int32
	FailedWrites    int
	FirstWriteErr   error
}

// AssetWriteService 资产写入服务，封装完整的资产保存业务逻辑
type AssetWriteService struct {
	db              *mongo.Database
	assetModel      *AssetModel
	historyModel    *AssetHistoryModel
	diffModel       *ScanDiffModel
	targetMetaModel *AssetTargetMetaModel
	mainTaskModel   *MainTaskModel
}

// NewAssetWriteService 创建资产写入服务
func NewAssetWriteService(db *mongo.Database) *AssetWriteService {
	return &AssetWriteService{
		db:              db,
		assetModel:      NewAssetModel(db),
		historyModel:    NewAssetHistoryModel(db),
		diffModel:       NewScanDiffModel(db),
		targetMetaModel: NewAssetTargetMetaModel(db),
		mainTaskModel:   NewMainTaskModel(db),
	}
}

// SaveAssets 保存资产列表（完整业务逻辑从 RPC 层迁移）
func (s *AssetWriteService) SaveAssets(ctx context.Context, mainTaskID, orgID string, assets []*ScannerAsset) (*SaveAssetsResult, error) {
	if len(assets) == 0 {
		return &SaveAssetsResult{}, nil
	}

	var diffs []ScanDiff
	seenTargets := make(map[string]struct{})
	var totalAsset, newAsset, updateAsset int32
	var failedWrites int
	var firstWriteErr error
	var attemptedWrites int32
	now := time.Now()

	// 任务标签传播：扫描任务配置的 tags 打到本任务发现的资产与顶层目标上，
	// 让"任务标签 → 资产标签筛选"链路生效（$addToSet 合并，不覆盖用户手工标签）
	taskTags := s.loadTaskTags(ctx, mainTaskID)

	for _, pbAsset := range assets {
		asset := s.mapScannerAssetToModel(pbAsset, mainTaskID, orgID)
		if len(taskTags) > 0 {
			asset.Labels = mergeUnique(asset.Labels, taskTags)
		}

		// 如果Source为空，设置默认值
		if asset.Source == "" {
			asset.Source = "scan"
		}

		// 处理IP信息
		s.processIPInfo(asset, pbAsset)

		// 处理CName
		if pbAsset.CName != "" {
			asset.CName = pbAsset.CName
		}

		// 设置Domain字段
		if asset.Category == "domain" || !utils.IsIPAddress(asset.Host) {
			asset.Domain = asset.Host
		}

		// 尝试继承基础域名的IP和CName
		s.inheritFromBaseDomain(ctx, asset)

		// 将新资产的 IP/Location 回填到基础域名资产
		s.backfillLocationToBaseDomain(ctx, asset)

		// 从同名无端口域名资产继承元数据（不删除域名记录）
		s.inheritFromNoPortAsset(ctx, asset)

		// 检查是否已存在
		var existing *Asset
		var err error

		if asset.Port > 0 {
			existing, err = s.assetModel.FindByHostPort(ctx, asset.Host, asset.Port)
		} else {
			existing, err = s.assetModel.FindByAuthorityOnly(ctx, asset.Authority)
		}

		if err != nil {
			// 查询失败不能等同于不存在，否则短暂的 Mongo 错误会把当前结果
			// 丢进竞争插入路径，最终既不更新旧资产也可能丢失新字段。
			logx.Errorf("[AssetWriteService] Find existing asset failed: %v", err)
			failedWrites++
			if firstWriteErr == nil {
				firstWriteErr = err
			}
			continue
		}
		if existing == nil {
			// 新资产
			asset.Id = primitive.NewObjectID()
			asset.CreateTime = now
			asset.UpdateTime = now
			asset.IsNewAsset = true
			asset.IsUpdated = false
			asset.FirstSeenTime = now
			asset.LastTaskId = ""
			asset.FirstSeenTaskId = mainTaskID
			asset.LastStatusChangeTime = now

			attemptedWrites++
			inserted, ierr := s.assetModel.InsertIfAbsent(ctx, asset)
			if ierr != nil {
				logx.Errorf("[AssetWriteService] Insert asset failed: %v", ierr)
				failedWrites++
				if firstWriteErr == nil {
					firstWriteErr = ierr
				}
				continue
			}
			if inserted {
				newAsset++

				// 记录首次发现历史，确保时间线不为空
				firstFound := SnapshotFromAsset(asset, mainTaskID, now, nil)
				if err := s.historyModel.Insert(ctx, firstFound); err != nil {
					logx.Errorf("[AssetWriteService] Insert first-found history failed: %v", err)
				}

				diffs = append(diffs, ScanDiff{
					TaskId:     mainTaskID,
					DiffType:   ScanDiffTypeAsset,
					ChangeType: ScanDiffChangeAdded,
					TargetKey:  asset.Authority,
					Summary:    asset.Host,
				})
			} else {
				// 并发竞态：同一 host:port/authority 已被其它协程写入
				//（异步批量落库通道满回退同步直写时与后台 flush 并发），
				// 跳过新增记账避免重复文档与双计数，字段值以下次扫描自然收敛
				logx.Infof("[AssetWriteService] concurrent insert detected, skip new-asset bookkeeping: %s", asset.Authority)
			}
		} else {
			// 更新已存在的资产
			isDifferentTask := existing.TaskId != mainTaskID

			opts := AssetWriteOptions{
				TaskId:          mainTaskID,
				IsDifferentTask: isDifferentTask,
			}
			update, changes := BuildAssetUpdateDoc(asset, existing, opts)

			// 只有当任务ID不同时才保存历史记录
			if isDifferentTask {
				exists, _ := s.historyModel.ExistsByAssetIdAndTaskId(ctx, existing.Id.Hex(), existing.TaskId)
				if !exists && len(changes) > 0 {
					history := SnapshotFromAsset(existing, existing.TaskId, existing.UpdateTime, changes)
					if err := s.historyModel.Insert(ctx, history); err != nil {
						logx.Errorf("[AssetWriteService] Insert asset history failed: %v", err)
					} else {
						logx.Infof("[AssetWriteService] 保存资产变更历史: assetId=%s, oldTaskId=%s, newTaskId=%s, changes=%d",
							existing.Id.Hex(), existing.TaskId, mainTaskID, len(changes))
					}
				}
			}

			attemptedWrites++
			if err := s.assetModel.UpdateWithRaw(ctx, existing.Id.Hex(), update); err != nil {
				logx.Errorf("[AssetWriteService] Update asset failed: %v", err)
				failedWrites++
				if firstWriteErr == nil {
					firstWriteErr = err
				}
				continue
			}

			if isDifferentTask {
				updateAsset++
				if len(changes) > 0 {
					diffs = append(diffs, ScanDiff{
						TaskId:     mainTaskID,
						DiffType:   ScanDiffTypeAsset,
						ChangeType: ScanDiffChangeUpdated,
						TargetKey:  existing.Authority,
						Summary:    existing.Host,
						Changes:    changes,
					})
				}
			}
		}
		totalAsset++

		// 登记/刷新顶层资产 meta
		targetKey := asset.Host + "\x00" + asset.Domain
		if _, ok := seenTargets[targetKey]; ok {
			continue
		}
		seenTargets[targetKey] = struct{}{}
		if err := s.targetMetaModel.EnsureForAsset(ctx, asset.Host, asset.Domain, nil); err != nil {
			logx.Errorf("[AssetWriteService] upsert target meta host=%s fail: %v", asset.Host, err)
			continue
		}

		tType, tValue := ResolveAssetTarget(asset.Host, asset.Domain)
		if tType != "" && tValue != "" {
			targetId := EncodeTargetID(tType, tValue)
			if len(taskTags) > 0 {
				if err := s.targetMetaModel.AddLabelsToTarget(ctx, targetId, taskTags); err != nil {
					logx.Errorf("[AssetWriteService] merge target labels id=%s fail: %v", targetId, err)
				}
			}
			if err := s.targetMetaModel.UpdateLastScanTime(ctx, targetId, now); err != nil {
				logx.Errorf("[AssetWriteService] update last_scan_time id=%s fail: %v", targetId, err)
			}
		}
	}

	logx.Infof("[AssetWriteService] SaveAssets: total=%d, new=%d, update=%d, attemptedWrites=%d, failedWrites=%d",
		totalAsset, newAsset, updateAsset, attemptedWrites, failedWrites)

	// 批量写入变化快照
	if len(diffs) > 0 {
		if err := s.diffModel.BatchInsert(ctx, diffs); err != nil {
			logx.Errorf("[AssetWriteService] [ScanDiff] batch insert failed (task=%s): %v", mainTaskID, err)
		} else {
			logx.Infof("[AssetWriteService] [ScanDiff] wrote %d diff records for task=%s", len(diffs), mainTaskID)
		}
	}

	// 全部写入失败判定
	if attemptedWrites > 0 && failedWrites == int(attemptedWrites) {
		return nil, fmt.Errorf("SaveAssets: %d/%d asset writes failed (sample err: %v)", failedWrites, attemptedWrites, firstWriteErr)
	}

	return &SaveAssetsResult{
		TotalAsset:      totalAsset,
		NewAsset:        newAsset,
		UpdateAsset:     updateAsset,
		AttemptedWrites: attemptedWrites,
		FailedWrites:    failedWrites,
		FirstWriteErr:   firstWriteErr,
	}, nil
}

// loadTaskTags 按主任务 ID 读取任务标签。mainTaskID 非法（手动导入等场景）
// 或任务不存在时返回 nil，不阻断资产写入。
func (s *AssetWriteService) loadTaskTags(ctx context.Context, mainTaskID string) []string {
	if mainTaskID == "" {
		return nil
	}
	task, err := s.mainTaskModel.FindById(ctx, mainTaskID)
	if err != nil || task == nil || len(task.Tags) == 0 {
		return nil
	}
	return task.Tags
}

// mergeUnique 合并两个标签切片并去重（保持出现顺序）。
func mergeUnique(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, v := range base {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range extra {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// mapScannerAssetToModel 将 ScannerAsset 映射为 model.Asset
func (s *AssetWriteService) mapScannerAssetToModel(sa *ScannerAsset, mainTaskID, orgID string) *Asset {
	return &Asset{
		Authority:                    sa.Authority,
		Host:                         sa.Host,
		Port:                         sa.Port,
		Category:                     sa.Category,
		Service:                      sa.Service,
		Title:                        sa.Title,
		App:                          sa.App,
		FingerprintFindings:          append(FingerprintFindings(nil), sa.FingerprintFindings...),
		FingerprintFindingsCollected: sa.FingerprintFindingsCollected,
		HttpStatus:                   sa.HttpStatus,
		HttpHeader:                   sa.HttpHeader,
		HttpBody:                     sa.HttpBody,
		IconHash:                     sa.IconHash,
		IconHashBytes:                sa.IconData,
		Screenshot:                   sa.Screenshot,
		Server:                       sa.Server,
		Banner:                       sa.Banner,
		IsHTTP:                       sa.IsHTTP,
		TaskId:                       mainTaskID,
		Source:                       sa.Source,
		CName:                        sa.CName,
		OrgId:                        orgID,
	}
}

// processIPInfo 处理IP信息
func (s *AssetWriteService) processIPInfo(asset *Asset, sa *ScannerAsset) {
	if len(sa.IPV4) > 0 {
		for _, ip := range sa.IPV4 {
			asset.Ip.IpV4 = append(asset.Ip.IpV4, IPV4{
				IPName:   ip.IP,
				Location: ip.Location,
			})
		}
	} else if utils.IsIPv4(asset.Host) {
		asset.Ip.IpV4 = append(asset.Ip.IpV4, IPV4{
			IPName: asset.Host,
		})
	}

	if len(sa.IPV6) > 0 {
		for _, ip := range sa.IPV6 {
			asset.Ip.IpV6 = append(asset.Ip.IpV6, IPV6{
				IPName:   ip.IP,
				Location: ip.Location,
			})
		}
	} else if utils.IsIPv6(asset.Host) {
		asset.Ip.IpV6 = append(asset.Ip.IpV6, IPV6{
			IPName: asset.Host,
		})
	}
}

// inheritFromBaseDomain 尝试继承基础域名的IP和CName
func (s *AssetWriteService) inheritFromBaseDomain(ctx context.Context, asset *Asset) {
	if asset.Port > 0 && len(asset.Ip.IpV4) == 0 && len(asset.Ip.IpV6) == 0 && !utils.IsIPAddress(asset.Host) {
		baseAsset, _ := s.assetModel.FindByAuthorityOnly(ctx, asset.Host)
		if baseAsset != nil {
			asset.Ip = baseAsset.Ip
			if asset.CName == "" {
				asset.CName = baseAsset.CName
			}
			if asset.Domain == "" {
				asset.Domain = baseAsset.Domain
			}
			if asset.OrgId == "" {
				asset.OrgId = baseAsset.OrgId
			}
		}
	}
}

// backfillLocationToBaseDomain 将新资产的 IP/Location 回填到基础域名资产
func (s *AssetWriteService) backfillLocationToBaseDomain(ctx context.Context, asset *Asset) {
	if asset.Port > 0 && (len(asset.Ip.IpV4) > 0 || len(asset.Ip.IpV6) > 0) && !utils.IsIPAddress(asset.Host) {
		baseAsset, _ := s.assetModel.FindByAuthorityOnly(ctx, asset.Host)
		if baseAsset != nil {
			needsUpdate := false

			for i, ipv4 := range baseAsset.Ip.IpV4 {
				if ipv4.Location == "" {
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
				if err := s.assetModel.UpdateWithRaw(ctx, baseAsset.Id.Hex(), bson.M{
					"$set": bson.M{"ip": baseAsset.Ip},
				}); err == nil {
					logx.Infof("[AssetWriteService] 已回填Location到基础域名资产: %s", asset.Host)
				}
			}
		}
	}
}

// inheritFromNoPortAsset 当保存带端口的域名资产时，从同名的无端口域名资产继承元数据。
// 不删除无端口资产——它是 subfinder 发现的域名记录，子域名菜单页依赖 category="domain" 的记录。
func (s *AssetWriteService) inheritFromNoPortAsset(ctx context.Context, asset *Asset) {
	if asset.Port > 0 && !utils.IsIPAddress(asset.Host) {
		noPortAsset, err := s.assetModel.FindByAuthorityOnly(ctx, asset.Host)
		if err == nil && noPortAsset != nil {
			if asset.CName == "" && noPortAsset.CName != "" {
				asset.CName = noPortAsset.CName
			}
			if asset.Domain == "" && noPortAsset.Domain != "" {
				asset.Domain = noPortAsset.Domain
			}
			if asset.OrgId == "" && noPortAsset.OrgId != "" {
				asset.OrgId = noPortAsset.OrgId
			}
			if len(asset.Ip.IpV4) == 0 && len(asset.Ip.IpV6) == 0 && (len(noPortAsset.Ip.IpV4) > 0 || len(noPortAsset.Ip.IpV6) > 0) {
				asset.Ip = noPortAsset.Ip
			}
		}
	}
}
