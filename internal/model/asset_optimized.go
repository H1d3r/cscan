package model

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AssetListProjection 列表查询专用投影，排除大字段
var AssetListProjection = bson.M{
	"body":            0,
	"header":          0,
	"icon_hash_bytes": 0,
	"screenshot":      0,
	"cert":            0,
	"banner":          0,
}

// AssetAggProjection 域名/IP 列表与统计聚合专用投影（FindAllForAgg 使用）：
// 仅保留跨全量文档去重/聚合所需字段（域名/IP/端口/标签/来源/时间），
// 排除 screenshot/body/header/cert/banner/icon_hash_bytes 等大字段。
var AssetAggProjection = bson.M{
	"_id":             1,
	"host":            1,
	"authority":       1,
	"port":            1,
	"category":        1,
	"ip":              1,
	"domain":          1,
	"service":         1,
	"cname":           1,
	"labels":          1,
	"org_id":          1,
	"new":             1,
	"source":          1,
	"create_time":     1,
	"update_time":     1,
	"first_seen_time": 1,
}

// AssetScreenshotProjection 资产清单/截图清单专用投影，保留 screenshot、icon_hash_bytes、header、body
//
// 已弃用：清单分页改用 AssetInventoryProjection（见下），以排除 body/header/banner 等大字段。
var AssetScreenshotProjection = bson.M{
	"cert":   0,
	"banner": 0,
}

// AssetInventoryProjection 资产清单分页专用投影：
// 保留卡片展示所需的 screenshot 与 icon_hash_bytes，排除 body/header/banner/cert 等大字段，
// 降低列表 payload（body/header 仅在资产详情抽屉按需加载）。
var AssetInventoryProjection = bson.M{
	"body":   0,
	"header": 0,
	"banner": 0,
	"cert":   0,
}

// AssetSiteProjection 站点列表专用投影，在列表投影基础上保留 icon_hash_bytes 用于展示 favicon
var AssetSiteProjection = bson.M{
	"body":       0,
	"header":     0,
	"screenshot": 0,
	"cert":       0,
	"banner":     0,
}

// AssetFingerprintProjection 指纹匹配专用投影
// 保留 body/header/icon_hash_bytes/server/title 等匹配所需字段，仅排除 screenshot/cert/banner 这三个无关大字段
// 相比 FindFull 全字段加载，可减少约 70%+ 内存占用（screenshot 通常最大）
var AssetFingerprintProjection = bson.M{
	"screenshot": 0,
	"cert":       0,
	"banner":     0,
}

// AssetDetailProjection 详情查询投影（包含所有字段）
var AssetDetailProjection = bson.M{}

// AssetMinimalProjection 最小投影（仅基础字段）
var AssetMinimalProjection = bson.M{
	"_id":       1,
	"authority": 1,
	"host":      1,
	"port":      1,
	"service":   1,
	"title":     1,
}

// AssetListItem 列表专用轻量级结构
type AssetListItem struct {
	Id                   primitive.ObjectID `bson:"_id" json:"id"`
	Authority            string             `bson:"authority" json:"authority"`
	Host                 string             `bson:"host" json:"host"`
	Port                 int                `bson:"port" json:"port"`
	Category             string             `bson:"category" json:"category"`
	Service              string             `bson:"service,omitempty" json:"service"`
	Server               string             `bson:"server,omitempty" json:"server"`
	Title                string             `bson:"title,omitempty" json:"title"`
	App                  []string           `bson:"app,omitempty" json:"app"`
	Fingerprints         []string           `bson:"fingerprints,omitempty" json:"fingerprints"`
	HttpStatus           string             `bson:"status,omitempty" json:"httpStatus"`
	IconHash             string             `bson:"icon_hash,omitempty" json:"iconHash"`
	Labels               []string           `bson:"labels,omitempty" json:"labels"`
	ColorTag             string             `bson:"color,omitempty" json:"colorTag"`
	Memo                 string             `bson:"memo,omitempty" json:"memo"`
	OrgId                string             `bson:"org_id,omitempty" json:"orgId"`
	IsCDN                bool               `bson:"cdn,omitempty" json:"isCdn"`
	IsCloud              bool               `bson:"cloud,omitempty" json:"isCloud"`
	IsHTTP               bool               `bson:"is_http" json:"isHttp"`
	IsNewAsset           bool               `bson:"new" json:"isNew"`
	IsUpdated            bool               `bson:"update" json:"isUpdated"`
	RiskScore            float64            `bson:"risk_score,omitempty" json:"riskScore"`
	RiskLevel            string             `bson:"risk_level,omitempty" json:"riskLevel"`
	TaskId               string             `bson:"taskId" json:"taskId"`
	CreateTime           time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime           time.Time          `bson:"update_time" json:"updateTime"`
	LastStatusChangeTime time.Time          `bson:"last_status_change_time,omitempty" json:"lastStatusChangeTime"`
	Ip                   IP                 `bson:"ip" json:"ip"`
}

// FindListOptimized 优化的列表查询（使用投影减少数据传输）
func (m *AssetModel) FindListOptimized(ctx context.Context, filter bson.M, page, pageSize int) ([]*AssetListItem, int64, error) {
	opts := options.Find().
		SetProjection(AssetListProjection).
		SetSort(bson.D{{Key: "update_time", Value: -1}})
	if page > 0 && pageSize > 0 {
		page, pageSize = NormalizePage(page, pageSize)
		opts.SetSkip(int64((page - 1) * pageSize)).SetLimit(int64(pageSize))
	}

	// 并行执行查询和计数
	var (
		items []*AssetListItem
		total int64
		wg    sync.WaitGroup
		errCh = make(chan error, 2)
	)

	wg.Add(2)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("FindListOptimized query goroutine panic: %v", r)
			}
		}()
		defer wg.Done()
		cursor, err := m.coll.Find(ctx, filter, opts)
		if err != nil {
			errCh <- err
			return
		}
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var doc AssetListItem
			if err := cursor.Decode(&doc); err != nil {
				errCh <- err
				return
			}
			items = append(items, &doc)
		}
		if err := cursor.Err(); err != nil {
			errCh <- err
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("FindListOptimized count goroutine panic: %v", r)
			}
		}()
		defer wg.Done()
		var err error
		total, err = m.coll.CountDocuments(ctx, filter)
		if err != nil {
			errCh <- err
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return nil, 0, err
		}
	}

	return items, total, nil
}

// FindListOptimizedWithSort 优化的列表查询（带自定义排序）
func (m *AssetModel) FindListOptimizedWithSort(ctx context.Context, filter bson.M, page, pageSize int, sortField string, sortOrder int) ([]*AssetListItem, int64, error) {
	opts := options.Find().
		SetProjection(AssetListProjection).
		SetSort(bson.D{{Key: sortField, Value: sortOrder}})
	if page > 0 && pageSize > 0 {
		page, pageSize = NormalizePage(page, pageSize)
		opts.SetSkip(int64((page - 1) * pageSize)).SetLimit(int64(pageSize))
	}

	var (
		items []*AssetListItem
		total int64
		wg    sync.WaitGroup
		errCh = make(chan error, 2)
	)

	wg.Add(2)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("FindListOptimizedWithSort query goroutine panic: %v", r)
			}
		}()
		defer wg.Done()
		cursor, err := m.coll.Find(ctx, filter, opts)
		if err != nil {
			errCh <- err
			return
		}
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var doc AssetListItem
			if err := cursor.Decode(&doc); err != nil {
				errCh <- err
				return
			}
			items = append(items, &doc)
		}
		if err := cursor.Err(); err != nil {
			errCh <- err
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("FindListOptimizedWithSort count goroutine panic: %v", r)
			}
		}()
		defer wg.Done()
		var err error
		total, err = m.coll.CountDocuments(ctx, filter)
		if err != nil {
			errCh <- err
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return nil, 0, err
		}
	}

	return items, total, nil
}

// FindByIdsOptimized 批量查询资产（优化版）
func (m *AssetModel) FindByIdsOptimized(ctx context.Context, ids []string) ([]*AssetListItem, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			continue
		}
		oids = append(oids, oid)
	}

	if len(oids) == 0 {
		return []*AssetListItem{}, nil
	}

	filter := bson.M{"_id": bson.M{"$in": oids}}
	opts := options.Find().SetProjection(AssetListProjection)

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []*AssetListItem
	if err = cursor.All(ctx, &items); err != nil {
		return nil, err
	}

	return items, nil
}

// FindMinimal 最小字段查询（用于关联查询）
func (m *AssetModel) FindMinimal(ctx context.Context, filter bson.M, limit int) ([]AssetListItem, error) {
	opts := options.Find().
		SetProjection(AssetMinimalProjection).
		SetLimit(int64(limit))

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []AssetListItem
	if err = cursor.All(ctx, &items); err != nil {
		return nil, err
	}

	return items, nil
}

// CountConcurrent 并发统计多个条件
func (m *AssetModel) CountConcurrent(ctx context.Context, filters map[string]bson.M) (map[string]int64, error) {
	results := make(map[string]int64)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, len(filters))

	for key, filter := range filters {
		wg.Add(1)
		go func(k string, f bson.M) {
			defer wg.Done()
			count, err := m.coll.CountDocuments(ctx, f)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			results[k] = count
			mu.Unlock()
		}(key, filter)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}
