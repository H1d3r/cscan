package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// DomainAggResult 域名聚合结果（MongoDB $group 输出）
type DomainAggResult struct {
	Domain     string    `bson:"_id" json:"domain"`
	Id         primitive.ObjectID `bson:"id" json:"id"`
	IPs        []string  `bson:"ips" json:"ips"`
	CName      string    `bson:"cname" json:"cname"`
	Source     string    `bson:"source" json:"source"`
	Labels     []string  `bson:"labels" json:"labels"`
	OrgId      string    `bson:"org_id" json:"orgId"`
	IsNew      bool      `bson:"is_new" json:"isNew"`
	CreateTime time.Time `bson:"create_time" json:"createTime"`
	UpdateTime time.Time `bson:"update_time" json:"updateTime"`
	AssetCount int       `bson:"asset_count" json:"assetCount"`
}

// IPAggResult IP聚合结果（MongoDB $group 输出）
type IPAggResult struct {
	IP         string    `bson:"_id" json:"ip"`
	Id         primitive.ObjectID `bson:"id" json:"id"`
	Location   string    `bson:"location" json:"location"`
	Ports      []PortAgg `bson:"ports" json:"ports"`
	Domains    []string  `bson:"domains" json:"domains"`
	OrgId      string    `bson:"org_id" json:"orgId"`
	IsNew      bool      `bson:"is_new" json:"isNew"`
	CreateTime time.Time `bson:"create_time" json:"createTime"`
	UpdateTime time.Time `bson:"update_time" json:"updateTime"`
	AssetCount int       `bson:"asset_count" json:"assetCount"`
}

// PortAgg 端口聚合中间结构
type PortAgg struct {
	Port    int    `bson:"port" json:"port"`
	Service string `bson:"service" json:"service"`
}

// ipRegex 用于过滤 IP 地址（域名聚合时排除 IP）
const ipRegexPattern = `^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`

// buildDomainExtractExpr 构建域名提取表达式：Domain → Host → Authority（去端口）
func buildDomainExtractExpr() interface{} {
	return bson.M{
		"$switch": bson.M{
			"branches": []bson.M{
				{
					"case": bson.M{"$and": []interface{}{
						bson.M{"$ne": []interface{}{"$domain", nil}},
						bson.M{"$ne": []interface{}{"$domain", ""}},
					}},
					"then": "$domain",
				},
				{
					"case": bson.M{"$and": []interface{}{
						bson.M{"$ne": []interface{}{"$host", nil}},
						bson.M{"$ne": []interface{}{"$host", ""}},
					}},
					"then": "$host",
				},
			},
			"default": bson.M{
				"$let": bson.M{
					"vars": bson.M{
						"parts": bson.M{"$split": []interface{}{
							bson.M{"$ifNull": []interface{}{"$authority", ""}},
							":",
						}},
					},
					"in": bson.M{"$arrayElemAt": []interface{}{"$$parts", 0}},
				},
			},
		},
	}
}

// AggregateDomains 服务端域名聚合（$group by domain + $facet 分页）。
// 替代 FindAllForAgg + 内存去重的模式，避免全量加载资产到内存。
func (m *AssetModel) AggregateDomains(ctx context.Context, filter bson.M, page, pageSize int) ([]DomainAggResult, int, error) {
	// 8. 分面数据管道：page>0 && pageSize>0 时分页，否则返回全部
	dataPipeline := mongo.Pipeline{}
	if page > 0 && pageSize > 0 {
		dataPipeline = mongo.Pipeline{
			{{Key: "$skip", Value: int64((page - 1) * pageSize)}},
			{{Key: "$limit", Value: int64(pageSize)}},
		}
	}

	pipeline := mongo.Pipeline{
		// 1. 匹配
		{{Key: "$match", Value: filter}},
		// 2. 提取域名（Domain → Host → Authority）
		{{Key: "$addFields", Value: bson.M{"_domain": buildDomainExtractExpr()}}},
		// 3. 过滤空域名和 IP 地址
		{{Key: "$match", Value: bson.M{
			"$expr": bson.M{"$and": []interface{}{
				bson.M{"$ne": []interface{}{"$_domain", ""}},
				bson.M{"$not": []interface{}{
					bson.M{"$regexMatch": bson.M{"input": "$_domain", "regex": ipRegexPattern}},
				}},
			}},
		}}},
		// 4. 提取所有 IPv4 地址
		{{Key: "$addFields", Value: bson.M{
			"_all_ips": bson.M{
				"$map": bson.M{
					"input": bson.M{"$ifNull": []interface{}{"$ip.ipv4", []interface{}{}}},
					"as":    "v",
					"in":    "$$v.ip",
				},
			},
		}}},
		// 5. 按域名分组
		{{Key: "$group", Value: bson.M{
			"_id":           "$_domain",
			"id":            bson.M{"$first": "$_id"},
			"all_ipv4_arrs": bson.M{"$push": bson.M{"$ifNull": []interface{}{"$_all_ips", []interface{}{}}}},
			"all_labels":    bson.M{"$push": bson.M{"$ifNull": []interface{}{"$labels", []interface{}{}}}},
			"cname":         bson.M{"$first": "$cname"},
			"source":        bson.M{"$first": "$source"},
			"category":      bson.M{"$first": "$category"},
			"org_id":        bson.M{"$first": "$org_id"},
			"is_new":        bson.M{"$first": "$new"},
			"create_time":   bson.M{"$min": "$create_time"},
			"update_time":   bson.M{"$max": "$update_time"},
			"asset_count":   bson.M{"$sum": 1},
		}}},
		// 6. 展平 IP 和标签（$reduce + $setUnion 去重）
		{{Key: "$addFields", Value: bson.M{
			"ips": bson.M{
				"$reduce": bson.M{
					"input":        "$all_ipv4_arrs",
					"initialValue": []interface{}{},
					"in": bson.M{"$setUnion": []interface{}{"$$value", "$$this"}},
				},
			},
			"labels": bson.M{
				"$reduce": bson.M{
					"input":        "$all_labels",
					"initialValue": []interface{}{},
					"in": bson.M{"$setUnion": []interface{}{"$$value", "$$this"}},
				},
			},
			"source": bson.M{
				"$cond": bson.M{
					"if": bson.M{"$or": []interface{}{
						bson.M{"$eq": []interface{}{"$source", nil}},
						bson.M{"$eq": []interface{}{"$source", ""}},
					}},
					"then": bson.M{
						"$cond": bson.M{
							"if":   bson.M{"$eq": []interface{}{"$category", "domain"}},
							"then": "subfinder",
							"else": "scan",
						},
					},
					"else": "$source",
				},
			},
		}}},
		// 7. 排序
		{{Key: "$sort", Value: bson.D{{Key: "update_time", Value: -1}}}},
		// 8. 分面：count + 分页数据
		{{Key: "$facet", Value: bson.M{
			"total": mongo.Pipeline{{{Key: "$count", Value: "count"}}},
			"data":  dataPipeline,
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var result []struct {
		Total []struct {
		 Count int `bson:"count"`
		} `bson:"total"`
		Data []DomainAggResult `bson:"data"`
	}
	if err := cursor.All(ctx, &result); err != nil {
		return nil, 0, err
	}
	if len(result) == 0 {
		return nil, 0, nil
	}

	total := 0
	if len(result[0].Total) > 0 {
		total = result[0].Total[0].Count
	}
	return result[0].Data, total, nil
}

// AggregateIPs 服务端 IP 聚合（$group by IP + $facet 分页）。
// 替代 FindAllForAgg + 内存去重的模式，避免全量加载资产到内存。
func (m *AssetModel) AggregateIPs(ctx context.Context, filter bson.M, page, pageSize int) ([]IPAggResult, int, error) {
	// 分面数据管道：page>0 && pageSize>0 时分页，否则返回全部
	dataPipelineIP := mongo.Pipeline{}
	if page > 0 && pageSize > 0 {
		dataPipelineIP = mongo.Pipeline{
			{{Key: "$skip", Value: int64((page - 1) * pageSize)}},
			{{Key: "$limit", Value: int64(pageSize)}},
		}
	}

	pipeline := mongo.Pipeline{
		// 1. 匹配
		{{Key: "$match", Value: filter}},
		// 2. 提取所有 IP（ipv4 数组 + host 如果是 IP）
		{{Key: "$addFields", Value: bson.M{
			"_ipv4_list": bson.M{
				"$map": bson.M{
					"input": bson.M{"$ifNull": []interface{}{"$ip.ipv4", []interface{}{}}},
					"as":    "v",
					"in":    "$$v.ip",
				},
			},
			"_host_is_ip": bson.M{
				"$cond": bson.M{
					"if":   bson.M{"$regexMatch": bson.M{"input": bson.M{"$ifNull": []interface{}{"$host", ""}}, "regex": ipRegexPattern}},
					"then": "$host",
					"else": nil,
				},
			},
		}}},
		// 3. 合并为单一 IP 数组
		{{Key: "$addFields", Value: bson.M{
			"_all_ips": bson.M{
				"$setUnion": []interface{}{
					bson.M{"$ifNull": []interface{}{"$_ipv4_list", []interface{}{}}},
					bson.M{
						"$cond": bson.M{
							"if":   bson.M{"$ne": []interface{}{"$_host_is_ip", nil}},
							"then": []interface{}{"$_host_is_ip"},
							"else": []interface{}{},
						},
					},
				},
			},
		}}},
		// 4. 展开 IP（每个 IP 一条文档）
		{{Key: "$unwind", Value: "$_all_ips"}},
		// 5. 过滤空 IP
		{{Key: "$match", Value: bson.M{"_all_ips": bson.M{"$ne": ""}}}},
		// 6. 按 IP 分组
		{{Key: "$group", Value: bson.M{
			"_id":    "$_all_ips",
			"id":     bson.M{"$first": "$_id"},
			"org_id": bson.M{"$first": "$org_id"},
			"is_new": bson.M{"$first": "$new"},
			"create_time": bson.M{"$min": "$create_time"},
			"update_time": bson.M{"$max": "$update_time"},
			"asset_count": bson.M{"$sum": 1},
			// 端口去重：收集 {port, service} 对
			"port_set": bson.M{"$addToSet": bson.M{
				"$cond": bson.M{
					"if":   bson.M{"$gt": []interface{}{"$port", 0}},
					"then": bson.M{"port": "$port", "service": "$service"},
					"else": nil,
				},
			}},
			// 域名去重：收集域名（domain 或 host 非 IP）
			"domain_set": bson.M{"$addToSet": bson.M{
				"$cond": bson.M{
					"if": bson.M{"$and": []interface{}{
						bson.M{"$ne": []interface{}{"$domain", nil}},
						bson.M{"$ne": []interface{}{"$domain", ""}},
					}},
					"then": "$domain",
					"else": bson.M{
						"$cond": bson.M{
							"if": bson.M{"$and": []interface{}{
								bson.M{"$ne": []interface{}{"$host", nil}},
								bson.M{"$ne": []interface{}{"$host", ""}},
								bson.M{"$not": []interface{}{
									bson.M{"$regexMatch": bson.M{"input": bson.M{"$ifNull": []interface{}{"$host", ""}}, "regex": ipRegexPattern}},
								}},
							}},
							"then": "$host",
							"else": nil,
						},
					},
				},
			}},
			// 位置：取第一个非空
			"location": bson.M{"$first": bson.M{
				"$let": bson.M{
					"vars": bson.M{
						"loc": bson.M{"$arrayElemAt": []interface{}{
							bson.M{"$filter": bson.M{
								"input": bson.M{"$map": bson.M{
									"input": bson.M{"$ifNull": []interface{}{"$ip.ipv4", []interface{}{}}},
									"as":    "v",
									"cond":  bson.M{"$ne": []interface{}{"$$v.location", ""}},
									"in":    "$$v.location",
								}},
								"cond": bson.M{"$ne": []interface{}{"$$this", ""}},
							}},
							0,
						}},
					},
					"in": bson.M{"$ifNull": []interface{}{"$$loc", ""}},
				},
			}},
		}}},
		// 7. 清理 null 值
		{{Key: "$addFields", Value: bson.M{
			"ports": bson.M{
				"$filter": bson.M{
					"input": "$port_set",
					"cond":  bson.M{"$ne": []interface{}{"$$this", nil}},
				},
			},
			"domains": bson.M{
				"$filter": bson.M{
					"input": "$domain_set",
					"cond":  bson.M{"$ne": []interface{}{"$$this", nil}},
				},
			},
		}}},
		// 8. 添加端口数字段并排序
		{{Key: "$addFields", Value: bson.M{
			"port_count": bson.M{"$size": "$ports"},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "port_count", Value: -1}}}},
		// 9. 分面：count + 分页数据
		{{Key: "$facet", Value: bson.M{
			"total": mongo.Pipeline{{{Key: "$count", Value: "count"}}},
			"data":  dataPipelineIP,
		}}},
	}

	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var result []struct {
		Total []struct {
			Count int `bson:"count"`
		} `bson:"total"`
		Data []IPAggResult `bson:"data"`
	}
	if err := cursor.All(ctx, &result); err != nil {
		return nil, 0, err
	}
	if len(result) == 0 {
		return nil, 0, nil
	}

	total := 0
	if len(result[0].Total) > 0 {
		total = result[0].Total[0].Count
	}
	return result[0].Data, total, nil
}
