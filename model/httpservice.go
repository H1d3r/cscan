package model

import (
	"context"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// HttpServiceConfig HTTP服务设置
// 用于配置哪些端口和服务名称被识别为HTTP服务
type HttpServiceConfig struct {
	Id           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	HttpPorts    []int              `bson:"http_ports" json:"httpPorts"`         // HTTP端口列表
	HttpsPorts   []int              `bson:"https_ports" json:"httpsPorts"`       // HTTPS端口列表
	NonHttpPorts []int              `bson:"non_http_ports" json:"nonHttpPorts"`  // 非HTTP端口列表（明确排除）
	Description  string             `bson:"description" json:"description"`      // 描述
	CreateTime   time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime   time.Time          `bson:"update_time" json:"updateTime"`
}

// 注意：HttpServiceMapping 定义在 fingerprint.go 中，这里复用该定义

// HttpServiceModel HTTP服务设置模型
type HttpServiceModel struct {
	configColl  *mongo.Collection
	mappingColl *mongo.Collection

	// 缓存
	httpPorts    map[int]bool    // HTTP端口缓存
	httpsPorts   map[int]bool    // HTTPS端口缓存
	nonHttpPorts map[int]bool    // 非HTTP端口缓存
	serviceCache map[string]bool // 服务名称缓存: serviceName -> isHttp
	mu           sync.RWMutex
}

// 默认HTTP端口列表
var defaultHttpPorts = []int{
	80, 8080, 8000, 8888, 8081, 8082, 8083, 8084, 8085, 8086,
	8087, 8088, 8089, 8090, 9000, 9001, 9080, 3000, 3001, 5000,
	5001, 8008, 8009, 8181, 8200, 8300, 8400, 8500, 8600, 8800,
	8880, 8983, 9090, 9091, 9200, 9300, 10000,
}

// 默认HTTPS端口列表
var defaultHttpsPorts = []int{
	443, 8443, 9443, 4443, 10443,
}

// 默认非HTTP端口列表（明确排除的端口）
var defaultNonHttpPorts = []int{
	// 远程管理
	22,    // SSH
	23,    // Telnet
	3389,  // RDP
	5900,  // VNC
	5901,  // VNC
	5902,  // VNC
	// 文件传输
	20,    // FTP-data
	21,    // FTP
	69,    // TFTP
	// 邮件服务
	25,    // SMTP
	110,   // POP3
	143,   // IMAP
	465,   // SMTPS
	587,   // SMTP Submission
	993,   // IMAPS
	995,   // POP3S
	// 数据库
	1433,  // MSSQL
	1521,  // Oracle
	3306,  // MySQL
	5432,  // PostgreSQL
	6379,  // Redis
	27017, // MongoDB
	9042,  // Cassandra
	// 消息队列
	5672,  // RabbitMQ AMQP
	6650,  // Pulsar
	9092,  // Kafka
	// 目录服务
	389,   // LDAP
	636,   // LDAPS
	// DNS
	53,    // DNS
	// Windows服务
	135,   // RPC
	137,   // NetBIOS Name
	138,   // NetBIOS Datagram
	139,   // NetBIOS Session
	445,   // SMB
	// 其他常见非HTTP服务
	111,   // RPC Portmapper
	161,   // SNMP
	162,   // SNMP Trap
	514,   // Syslog
	1080,  // SOCKS
	1194,  // OpenVPN
	1883,  // MQTT
	2049,  // NFS
	2181,  // ZooKeeper
	3268,  // LDAP Global Catalog
	3269,  // LDAPS Global Catalog
	11211, // Memcached
}

// 默认HTTP服务名称映射
var defaultServiceMappings = map[string]bool{
	"http":       true,
	"https":      true,
	"http-proxy": true,
	"http-alt":   true,
	"https-alt":  true,
	"ssl/http":   true,
	"ssl/https":  true,
	"http-mgmt":  true,
	"http-rpc-epmap": true,
	// 非HTTP服务
	"ssh":     false,
	"ftp":     false,
	"smtp":    false,
	"pop3":    false,
	"imap":    false,
	"mysql":   false,
	"redis":   false,
	"mongodb": false,
	"mssql":   false,
	"oracle":  false,
	"postgresql": false,
	"telnet":  false,
	"rdp":     false,
	"vnc":     false,
	"ldap":    false,
	"dns":     false,
	"smb":     false,
}

func NewHttpServiceModel(db *mongo.Database) *HttpServiceModel {
	configColl := db.Collection("http_service_config")
	mappingColl := db.Collection("http_service_mapping")

	// 创建服务映射的唯一索引
	mappingColl.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "service_name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	m := &HttpServiceModel{
		configColl:   configColl,
		mappingColl:  mappingColl,
		httpPorts:    make(map[int]bool),
		httpsPorts:   make(map[int]bool),
		nonHttpPorts: make(map[int]bool),
		serviceCache: make(map[string]bool),
	}

	// 初始化时加载缓存
	m.RefreshCache(context.Background())

	return m
}

// GetConfig 获取HTTP服务配置
func (m *HttpServiceModel) GetConfig(ctx context.Context) (*HttpServiceConfig, error) {
	var config HttpServiceConfig
	err := m.configColl.FindOne(ctx, bson.M{}).Decode(&config)
	if err == mongo.ErrNoDocuments {
		// 返回默认配置
		return &HttpServiceConfig{
			HttpPorts:    defaultHttpPorts,
			HttpsPorts:   defaultHttpsPorts,
			NonHttpPorts: defaultNonHttpPorts,
		}, nil
	}
	return &config, err
}

// SaveConfig 保存HTTP服务配置
func (m *HttpServiceModel) SaveConfig(ctx context.Context, config *HttpServiceConfig) error {
	now := time.Now()
	config.UpdateTime = now

	// 使用upsert，确保只有一条配置记录
	opts := options.Update().SetUpsert(true)
	update := bson.M{
		"$set": bson.M{
			"http_ports":     config.HttpPorts,
			"https_ports":    config.HttpsPorts,
			"non_http_ports": config.NonHttpPorts,
			"description":    config.Description,
			"update_time":    now,
		},
		"$setOnInsert": bson.M{
			"create_time": now,
		},
	}

	_, err := m.configColl.UpdateOne(ctx, bson.M{}, update, opts)
	if err == nil {
		m.RefreshCache(ctx)
	}
	return err
}

// GetMappings 获取所有服务映射
func (m *HttpServiceModel) GetMappings(ctx context.Context) ([]HttpServiceMapping, error) {
	opts := options.Find().SetSort(bson.D{{Key: "service_name", Value: 1}})
	cursor, err := m.mappingColl.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []HttpServiceMapping
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// GetEnabledMappings 获取启用的服务映射
func (m *HttpServiceModel) GetEnabledMappings(ctx context.Context) ([]HttpServiceMapping, error) {
	cursor, err := m.mappingColl.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []HttpServiceMapping
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// SaveMapping 保存服务映射
func (m *HttpServiceModel) SaveMapping(ctx context.Context, mapping *HttpServiceMapping) error {
	now := time.Now()
	mapping.UpdateTime = now

	if mapping.Id.IsZero() {
		// 新增
		mapping.Id = primitive.NewObjectID()
		mapping.CreateTime = now
		_, err := m.mappingColl.InsertOne(ctx, mapping)
		if err == nil {
			m.RefreshCache(ctx)
		}
		return err
	}

	// 更新
	update := bson.M{
		"$set": bson.M{
			"service_name": mapping.ServiceName,
			"is_http":      mapping.IsHttp,
			"description":  mapping.Description,
			"enabled":      mapping.Enabled,
			"update_time":  now,
		},
	}
	_, err := m.mappingColl.UpdateOne(ctx, bson.M{"_id": mapping.Id}, update)
	if err == nil {
		m.RefreshCache(ctx)
	}
	return err
}

// DeleteMapping 删除服务映射
func (m *HttpServiceModel) DeleteMapping(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.mappingColl.DeleteOne(ctx, bson.M{"_id": oid})
	if err == nil {
		m.RefreshCache(ctx)
	}
	return err
}

// RefreshCache 刷新缓存
func (m *HttpServiceModel) RefreshCache(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 加载端口配置
	config, err := m.GetConfig(ctx)
	if err == nil {
		m.httpPorts = make(map[int]bool)
		m.httpsPorts = make(map[int]bool)
		m.nonHttpPorts = make(map[int]bool)
		for _, port := range config.HttpPorts {
			m.httpPorts[port] = true
		}
		for _, port := range config.HttpsPorts {
			m.httpsPorts[port] = true
		}
		for _, port := range config.NonHttpPorts {
			m.nonHttpPorts[port] = true
		}
	} else {
		// 使用默认端口
		m.httpPorts = make(map[int]bool)
		m.httpsPorts = make(map[int]bool)
		m.nonHttpPorts = make(map[int]bool)
		for _, port := range defaultHttpPorts {
			m.httpPorts[port] = true
		}
		for _, port := range defaultHttpsPorts {
			m.httpsPorts[port] = true
		}
		for _, port := range defaultNonHttpPorts {
			m.nonHttpPorts[port] = true
		}
	}

	// 加载服务映射
	mappings, err := m.GetEnabledMappings(ctx)
	if err == nil {
		m.serviceCache = make(map[string]bool)
		for _, mapping := range mappings {
			m.serviceCache[mapping.ServiceName] = mapping.IsHttp
		}
	} else {
		// 使用默认映射
		m.serviceCache = make(map[string]bool)
		for name, isHttp := range defaultServiceMappings {
			m.serviceCache[name] = isHttp
		}
	}
}

// IsHttpPort 判断端口是否为HTTP端口
func (m *HttpServiceModel) IsHttpPort(port int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.httpPorts[port] || m.httpsPorts[port]
}

// IsHttpsPort 判断端口是否为HTTPS端口
func (m *HttpServiceModel) IsHttpsPort(port int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.httpsPorts[port]
}

// IsNonHttpPort 判断端口是否为非HTTP端口（明确排除）
func (m *HttpServiceModel) IsNonHttpPort(port int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nonHttpPorts[port]
}

// IsHttpService 判断服务名称是否为HTTP服务
func (m *HttpServiceModel) IsHttpService(serviceName string) (isHttp bool, found bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	isHttp, found = m.serviceCache[serviceName]
	return
}

// CheckIsHttp 综合判断是否为HTTP服务（端口+服务名称）
func (m *HttpServiceModel) CheckIsHttp(serviceName string, port int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. 先检查是否在非HTTP端口列表中（明确排除）
	if m.nonHttpPorts[port] {
		return false
	}

	// 2. 检查服务名称映射
	if isHttp, found := m.serviceCache[serviceName]; found {
		return isHttp
	}

	// 3. 检查HTTP/HTTPS端口
	return m.httpPorts[port] || m.httpsPorts[port]
}

// GetHttpPorts 获取所有HTTP端口列表
func (m *HttpServiceModel) GetHttpPorts() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ports := make([]int, 0, len(m.httpPorts))
	for port := range m.httpPorts {
		ports = append(ports, port)
	}
	return ports
}

// GetHttpsPorts 获取所有HTTPS端口列表
func (m *HttpServiceModel) GetHttpsPorts() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ports := make([]int, 0, len(m.httpsPorts))
	for port := range m.httpsPorts {
		ports = append(ports, port)
	}
	return ports
}

// GetAllHttpPorts 获取所有HTTP/HTTPS端口列表
func (m *HttpServiceModel) GetAllHttpPorts() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ports := make([]int, 0, len(m.httpPorts)+len(m.httpsPorts))
	for port := range m.httpPorts {
		ports = append(ports, port)
	}
	for port := range m.httpsPorts {
		ports = append(ports, port)
	}
	return ports
}

// GetNonHttpPorts 获取所有非HTTP端口列表
func (m *HttpServiceModel) GetNonHttpPorts() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ports := make([]int, 0, len(m.nonHttpPorts))
	for port := range m.nonHttpPorts {
		ports = append(ports, port)
	}
	return ports
}

// InitDefaultData 初始化默认数据（如果数据库为空）
func (m *HttpServiceModel) InitDefaultData(ctx context.Context) error {
	// 检查是否已有配置
	count, err := m.configColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}

	if count == 0 {
		// 初始化默认端口配置
		config := &HttpServiceConfig{
			HttpPorts:    defaultHttpPorts,
			HttpsPorts:   defaultHttpsPorts,
			NonHttpPorts: defaultNonHttpPorts,
			Description:  "默认HTTP服务端口配置",
		}
		if err := m.SaveConfig(ctx, config); err != nil {
			return err
		}
	}

	// 检查是否已有服务映射
	mappingCount, err := m.mappingColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}

	if mappingCount == 0 {
		// 初始化默认服务映射
		for name, isHttp := range defaultServiceMappings {
			mapping := &HttpServiceMapping{
				ServiceName: name,
				IsHttp:      isHttp,
				Description: "",
				Enabled:     true,
			}
			if err := m.SaveMapping(ctx, mapping); err != nil {
				// 忽略重复键错误
				continue
			}
		}
	}

	return nil
}
