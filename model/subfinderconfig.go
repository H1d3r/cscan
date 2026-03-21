package model

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SubfinderProvider Subfinder数据源配置
type SubfinderProvider struct {
	Id          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Provider    string             `bson:"provider" json:"provider"`       // 数据源名称: binaryedge, censys, shodan, github等
	Keys        []string           `bson:"keys" json:"keys"`               // API密钥列表（支持多个）
	Status      string             `bson:"status" json:"status"`           // enable/disable
	Description string             `bson:"description" json:"description"` // 描述
	CreateTime  time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime  time.Time          `bson:"update_time" json:"updateTime"`
}

// SubfinderProviderModel Subfinder数据源配置模型
type SubfinderProviderModel struct {
	coll *mongo.Collection
}

// NewSubfinderProviderModel 创建Subfinder数据源配置模型
func NewSubfinderProviderModel(db *mongo.Database) *SubfinderProviderModel {
	coll := db.Collection("subfinder_provider")

	// 创建索引
	ctx := context.Background()
	coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "provider", Value: 1}},
	})

	return &SubfinderProviderModel{coll: coll}
}

// Insert 插入配置
func (m *SubfinderProviderModel) Insert(ctx context.Context, doc *SubfinderProvider) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	if doc.Status == "" {
		doc.Status = "enable"
	}
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

// FindByProvider 根据数据源名称查找
func (m *SubfinderProviderModel) FindByProvider(ctx context.Context, provider string) (*SubfinderProvider, error) {
	var doc SubfinderProvider
	err := m.coll.FindOne(ctx, bson.M{"provider": provider}).Decode(&doc)
	return &doc, err
}

// FindAll 查找所有配置
func (m *SubfinderProviderModel) FindAll(ctx context.Context) ([]SubfinderProvider, error) {
	cursor, err := m.coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []SubfinderProvider
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindEnabled 查找所有启用的配置
func (m *SubfinderProviderModel) FindEnabled(ctx context.Context) ([]SubfinderProvider, error) {
	cursor, err := m.coll.Find(ctx, bson.M{"status": "enable"})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []SubfinderProvider
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// Update 更新配置
func (m *SubfinderProviderModel) Update(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// Upsert 更新或插入配置（按provider）
func (m *SubfinderProviderModel) Upsert(ctx context.Context, doc *SubfinderProvider) error {
	now := time.Now()
	doc.UpdateTime = now

	filter := bson.M{"provider": doc.Provider}
	update := bson.M{
		"$set": bson.M{
			"keys":        doc.Keys,
			"status":      doc.Status,
			"description": doc.Description,
			"update_time": now,
		},
		"$setOnInsert": bson.M{
			"_id":         primitive.NewObjectID(),
			"provider":    doc.Provider,
			"create_time": now,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := m.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

// Delete 删除配置
func (m *SubfinderProviderModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// GetProviderConfig 获取所有启用的配置，返回map格式供Subfinder使用
func (m *SubfinderProviderModel) GetProviderConfig(ctx context.Context) (map[string][]string, error) {
	providers, err := m.FindEnabled(ctx)
	if err != nil {
		return nil, err
	}

	config := make(map[string][]string)
	for _, p := range providers {
		if len(p.Keys) > 0 {
			config[p.Provider] = p.Keys
		}
	}
	return config, nil
}

// SubfinderProviderInfo 数据源信息（用于前端展示）
var SubfinderProviderInfo = []struct {
	Provider    string `json:"provider"`
	Name        string `json:"name"`
	Description string `json:"description"`
	KeyFormat   string `json:"keyFormat"` // 密钥格式说明
	URL         string `json:"url"`       // 获取API密钥的URL
}{
	{"binaryedge", "BinaryEdge", "BinaryEdge网络安全数据平台", "API Key", "https://app.binaryedge.io/account/api"},
	{"bufferover", "BufferOver", "BufferOver DNS数据", "API Key", "https://tls.bufferover.run/"},
	{"c99", "C99", "C99子域名枚举", "API Key", "https://api.c99.nl/"},
	{"censys", "Censys", "Censys互联网搜索引擎", "API_ID:API_SECRET", "https://censys.io/account/api"},
	{"certspotter", "CertSpotter", "证书透明度日志监控", "API Key", "https://sslmate.com/account/api_credentials"},
	{"chaos", "Chaos", "ProjectDiscovery Chaos数据", "API Key", "https://chaos.projectdiscovery.io/"},
	{"chinaz", "Chinaz", "站长之家", "API Key", "https://www.chinaz.com/"},
	{"dnsdb", "DNSDB", "Farsight DNSDB", "API Key", "https://www.dnsdb.info/"},
	{"fofa", "FOFA", "FOFA网络空间搜索引擎", "API Key", "https://fofa.info/userInfo"},
	{"fullhunt", "FullHunt", "FullHunt攻击面管理", "API Key", "https://fullhunt.io/"},
	{"github", "GitHub", "GitHub代码搜索", "Personal Access Token", "https://github.com/settings/tokens"},
	{"hunter", "Hunter", "鹰图平台", "API Key", "https://hunter.qianxin.com/home/myInfo"},
	{"intelx", "IntelX", "Intelligence X", "API Key", "https://intelx.io/account?tab=developer"},
	{"netlas", "Netlas", "Netlas网络资产搜索", "API Key", "https://netlas.io/"},
	{"passivetotal", "PassiveTotal", "RiskIQ PassiveTotal", "email:key", "https://community.riskiq.com/settings"},
	{"quake", "Quake", "360 Quake", "API Key", "https://quake.360.net/quake/#/personal?tab=message"},
	{"robtex", "Robtex", "Robtex DNS数据", "API Key", "https://www.robtex.com/dashboard/"},
	{"securitytrails", "SecurityTrails", "SecurityTrails DNS历史", "API Key", "https://securitytrails.com/app/account/credentials"},
	{"shodan", "Shodan", "Shodan搜索引擎", "API Key", "https://account.shodan.io/"},
	{"threatbook", "ThreatBook", "微步在线", "API Key", "https://x.threatbook.com/v5/myApi"},
	{"virustotal", "VirusTotal", "VirusTotal", "API Key", "https://www.virustotal.com/gui/my-apikey"},
	{"whoisxmlapi", "WhoisXML API", "WhoisXML API", "API Key", "https://whoisxmlapi.com/"},
	{"zoomeye", "ZoomEye", "ZoomEye网络空间搜索", "API Key", "https://www.zoomeye.org/profile"},
	{"zoomeyeapi", "ZoomEye API", "ZoomEye API (国际版)", "API Key", "https://www.zoomeye.org/profile"},
	{"bevigil", "BeVigil", "BeVigil移动应用安全", "API Key", "https://bevigil.com/"},
	{"builtwith", "BuiltWith", "BuiltWith技术分析", "API Key", "https://builtwith.com/"},
	{"dnsrepo", "DNSRepo", "DNSRepo DNS记录", "API Key", "https://dnsrepo.noc.org/"},
	{"facebook", "Facebook", "Facebook证书透明度", "App ID|App Secret", "https://developers.facebook.com/"},
	{"redhuntlabs", "RedHunt Labs", "RedHunt Labs攻击面侦察", "API Key", "https://devportal.redhuntlabs.com/"},
	{"hackertarget", "HackerTarget", "HackerTarget在线扫描(可选Key)", "API Key", "https://hackertarget.com/scan-membership/"},
	{"reconeer", "Reconeer", "Reconeer子域名侦察", "API Key", "https://www.reconeer.com/"},
	{"urlscan", "URLScan", "URLScan.io网页扫描", "API Key", "https://urlscan.io/user/profile/"},
}
