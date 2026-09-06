package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cscan/internal/notification"
	"cscan/worker/internal/worker"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stat"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

var (
	// 新参数：-s 改为 API 服务地址（支持环境变量 CSCAN_SERVER）
	serverAddr  = flag.String("s", getEnvOrDefault("CSCAN_SERVER", "http://localhost:8888"), "API server address (e.g., http://192.168.1.100:8888)")
	workerName  = flag.String("n", getEnvOrDefault("CSCAN_NAME", ""), "worker name (default: hostname-pid)")
	concurrency = flag.Int("c", getEnvIntOrDefault("CSCAN_CONCURRENCY", deriveConcurrencyFromMemory()), "concurrency")
	installKey  = flag.String("k", getEnvOrDefault("CSCAN_KEY", getEnvOrDefault("CSCAN_WORKER_KEY", "")), "install key for authentication")
)

// Version 编译期注入的版本号（Dockerfile 经 -ldflags "-X main.Version=${VERSION}" 注入）
var Version = "dev"

// getEnvOrDefault 获取环境变量，如果不存在则返回默认值
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvIntOrDefault 获取环境变量（整数），如果不存在则返回默认值
func getEnvIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// deriveConcurrencyFromMemory 在未显式设置 CSCAN_CONCURRENCY 时，依据容器 cgroup 内存上限
// （读取失败则回退到宿主机物理内存）自动推导安全并发数：既能在高配机充分利用资源，
// 又避免低配环境因 Chrome 标签数过多触发 OOM。详见 README「资源配置」。
func deriveConcurrencyFromMemory() int {
	const (
		perTabMB   = 384 // headless Chrome 单标签 + 扫描器常驻开销经验值
		utilFactor = 0.6 // 内存利用率上限，预留余量应对突发
		minConc    = 1
		maxConc    = 5
	)
	limit := readCgroupMemoryLimitBytes()
	if limit == 0 {
		limit = hostMemoryTotalBytes() // cgroup 未限制时退回宿主机物理内存
	}
	if limit == 0 {
		return 5 // 终极兜底
	}
	mb := float64(limit) / (1024 * 1024)
	conc := int(mb * utilFactor / perTabMB)
	if conc < minConc {
		conc = minConc
	}
	if conc > maxConc {
		conc = maxConc
	}
	return conc
}

// readCgroupMemoryLimitBytes 读取 cgroup 内存上限（cgroup v2 memory.max 或 v1
// memory.limit_in_bytes）。返回 0 表示未限制或读取失败。
func readCgroupMemoryLimitBytes() uint64 {
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		s := strings.TrimSpace(string(data))
		if s != "" && s != "max" {
			if v, err := strconv.ParseUint(s, 10, 64); err == nil {
				return v
			}
		}
		return 0
	}
	if data, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		s := strings.TrimSpace(string(data))
		if v, err := strconv.ParseUint(s, 10, 64); err == nil && v < ^uint64(0)/2 {
			return v
		}
	}
	return 0
}

// hostMemoryTotalBytes 读取宿主机物理内存总量（Linux /proc/meminfo）。
func hostMemoryTotalBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					return kb * 1024
				}
			}
		}
	}
	return 0
}

// validateInstallKey 验证安装密钥
// 修复 C-38：原使用 http.Post（http.DefaultClient 无超时），API 服务故障时 worker 启动会挂起。
// 现使用 http.Client 设置 10s 超时，避免长时间阻塞启动流程。
// 修复 C-39：原 defer resp.Body.Close() 在 for 循环中导致 body 资源累积到函数返回才释放；
// 且重试间隔无 jitter，与服务器重启风暴容易形成同步重试。
// 现在每次迭代内立即关闭 body，并使用 jitter 退避；
// 区分 503（基础设施故障，可重试）与 401（密钥无效，立即失败）。
func validateInstallKey(apiServer, key, name string) error {
	reqBody := map[string]string{
		"installKey": key,
		"workerName": name,
		"workerIP":   worker.GetLocalIP(),
		"workerOS":   runtime.GOOS,
		"workerArch": runtime.GOARCH,
	}
	jsonData, _ := json.Marshal(reqBody)

	// 构建API地址
	url := fmt.Sprintf("%s/api/v1/worker/validate", apiServer)

	// 带超时的 client，避免 API 故障时 worker 启动挂起
	client := &http.Client{Timeout: 10 * time.Second}

	// 发送验证请求，带重试
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			// 指数退避 + jitter，避免服务器重启后多 worker 同步重试
			backoff := time.Duration(1<<uint(i)) * time.Second
			jitter := time.Duration(rand.Int63n(int64(time.Second)))
			time.Sleep(backoff + jitter)
		}

		resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			lastErr = err
			logx.Infof("⚠️  Validation attempt %d failed: %v, retrying...", i+1, err)
			continue
		}
		// 修复 C-39：每次迭代内立即关闭 body，避免 defer 在循环中累积
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// 503 表示 Redis 等基础设施故障，可重试；401 表示密钥无效，立即失败
		if resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = fmt.Errorf("server temporarily unavailable (503)")
			logx.Infof("⚠️  Validation attempt %d: server unavailable, retrying...", i+1)
			continue
		}

		var result struct {
			Code  int    `json:"code"`
			Msg   string `json:"msg"`
			Valid bool   `json:"valid"`
		}
		json.Unmarshal(body, &result)
		if result.Code != 0 || !result.Valid {
			// 密钥无效属于业务错误，重试无意义
			return fmt.Errorf("validation failed: %s", result.Msg)
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("validation failed after 3 attempts")
	}
	return lastErr
}

// connectWorkerRedis 初始化 Worker 直连 Redis 客户端（用于任务调度层）
func connectWorkerRedis() *redis.Client {
	addr := os.Getenv("CSCAN_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	password := os.Getenv("CSCAN_REDIS_PASSWORD")
	db := 0
	if dbStr := os.Getenv("CSCAN_REDIS_DB"); dbStr != "" {
		if v, err := strconv.Atoi(dbStr); err == nil {
			db = v
		}
	}

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     20,
		MinIdleConns: 2,
		MaxRetries:   3,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		logx.Errorf("[Worker][Redis] ping failed: %v", err)
		return nil
	}

	logx.Infof("[Worker][Redis] connected, addr=%s, db=%d", addr, db)
	return client
}

// connectWorkerMongo 仅用于 Worker 直连 MongoDB 写入资产，使用独享连接池。
func connectWorkerMongo(uri string) (*mongo.Client, *mongo.Database) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().
		ApplyURI(uri).
		SetMaxPoolSize(20).
		SetMinPoolSize(2).
		SetMaxConnecting(5)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		logx.Errorf("[Worker][MongoDirect] connect failed: %v", err)
		return nil, nil
	}
	if err := client.Ping(ctx, nil); err != nil {
		logx.Errorf("[Worker][MongoDirect] ping failed: %v", err)
		_ = client.Disconnect(context.Background())
		return nil, nil
	}

	dbName := "cscan"
	if cs, err := connstring.Parse(uri); err == nil && cs.Database != "" {
		dbName = cs.Database
	}
	logx.Infof("[Worker][MongoDirect] connected, db=%s, pool=%d", dbName, 20)
	return client, client.Database(dbName)
}

// connectStatusMongo 任务状态写专用连接（独立持久化）。
// 与结果数据写池（connectWorkerMongo，池=20）完全隔离：状态写 QPS 低但优先级高，
// 独立小池（max=5）即可保证扫描结果写入把数据池打满时，
// IncrSubTaskDone / current_phase / MarkTaskCompleted 等状态回写不被饿死，
// 避免平台侧任务"卡在 Fingerprint 终止"。连接失败时回退共用数据池（不阻断启动）。
func connectStatusMongo(uri string) (*mongo.Client, *mongo.Database) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().
		ApplyURI(uri).
		SetMaxPoolSize(5).
		SetMinPoolSize(1).
		SetMaxConnecting(2)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		logx.Errorf("[Worker][MongoStatus] connect failed: %v (fall back to data pool for status writes)", err)
		return nil, nil
	}
	if err := client.Ping(ctx, nil); err != nil {
		logx.Errorf("[Worker][MongoStatus] ping failed: %v (fall back to data pool for status writes)", err)
		_ = client.Disconnect(context.Background())
		return nil, nil
	}

	dbName := "cscan"
	if cs, err := connstring.Parse(uri); err == nil && cs.Database != "" {
		dbName = cs.Database
	}
	logx.Infof("[Worker][MongoStatus] connected, db=%s, pool=5 (independent status write pool)", dbName)
	return client, client.Database(dbName)
}

func main() {
	flag.Parse()
	// CSCAN_CONCURRENCY 未显式设置时，按内存上限自动推导并发，便于低配防 OOM、高配提吞吐
	if os.Getenv("CSCAN_CONCURRENCY") == "" {
		logx.Infof("🔧 CSCAN_CONCURRENCY 未设置，按容器内存上限自动推导并发数: %d", *concurrency)
	}
	logx.MustSetup(logx.LogConf{
		ServiceName: "cscan-worker",
		Mode:        "console",             // 开启控制台颜色
		Encoding:    "plain",               // 纯文本格式
		TimeFormat:  "2006-01-02 15:04:05", // 完整日期时间
		Level:       "debug",               // 任务流水日志以 DEBUG 级别输出到控制台（前端默认隐藏）
		Stat:        false,                 // 关闭资源统计
		// 6/29 OOM 修复：单条日志最大 4KB，截断超长内容（如完整 HTTP 响应体）；
		// 可通过环境变量 CSCAN_WORKER_LOG_MAX_CONTENT_LENGTH 调整
		MaxContentLength: uint32(getEnvIntOrDefault("CSCAN_WORKER_LOG_MAX_CONTENT_LENGTH", 4096)),
	})
	// 禁用额外的统计输出
	stat.DisableLog()
	fmt.Println(`
	______ _____  ______          _   _ 
	/ ____/ ____|/ __ \ \        / / | \ | |
	| |   | (___ | |  | \ \  /\  / /|  \| |
	| |    \___ \| |  | |\ \/  \/ / | .  |
	| |________) | |__| | \  /\  /  | |\  |
	\_____|_____/ \____/   \/  \/   |_| \_| 
					WORKER NODE            `)
	fmt.Println("---------------------------------------------------------")
	logx.Info("🚀 Initializing CScan Worker Node...")

	// 生成Worker名称
	name := *workerName
	if name == "" {
		name = worker.GetWorkerName()
	}

	// 开发模式下跳过安装密钥验证（同 API 的 CSCAN_DEV=1 行为）
	if os.Getenv("CSCAN_DEV") == "1" {
		logx.Info("⚠️  Dev mode (CSCAN_DEV=1): skipping install key validation")
	} else if *installKey == "" {
		logx.Error("❌ Error: install key is required (-k flag)")
		logx.Error("   Please get the install key from the admin panel")
		os.Exit(1)
	}

	// 确定API服务器地址
	apiServer := *serverAddr
	// 确保地址有协议前缀
	if !strings.HasPrefix(apiServer, "http://") && !strings.HasPrefix(apiServer, "https://") {
		apiServer = "http://" + apiServer
	}

	fmt.Println("---------------------------------------------------------")
	logx.Infof("🔗 Connecting to API Server: %s", apiServer)
	logx.Infof("🔑 Validating Identity for: %s", name)

	// 开发模式下跳过 API 密钥验证
	if os.Getenv("CSCAN_DEV") == "1" {
		logx.Info("⚠️  Dev mode (CSCAN_DEV=1): skipping API key validation")
	} else {
		// 验证安装密钥
		if err := validateInstallKey(apiServer, *installKey, name); err != nil {
			logx.Errorf("❌ Authentication failed: %v", err)
			os.Exit(1)
		}
		logx.Info("✅ Identity verified successfully")
	}

	// 获取本机IP
	ip := worker.GetLocalIP()

	// 直连 MongoDB 用于扫描结果写入（Phase 1 后为必填项）
	mongoURI := os.Getenv("CSCAN_MONGO_URI")
	if mongoURI == "" {
		logx.Error("❌ Error: CSCAN_MONGO_URI is required for direct MongoDB write")
		logx.Error("   Please set CSCAN_MONGO_URI environment variable")
		os.Exit(1)
	}

	mongoClient, mongoDB := connectWorkerMongo(mongoURI)
	if mongoDB == nil {
		logx.Error("❌ Error: Failed to connect to MongoDB")
		os.Exit(1)
	}

	// 任务状态写独立连接池（独立持久化）：结果数据写与状态回写隔离，
	// 数据池过载时状态写仍可完成；连接失败自动回退数据池。
	statusClient, statusDB := connectStatusMongo(mongoURI)
	if statusDB == nil {
		logx.Errorf("[Worker][MongoStatus] status pool unavailable, status writes will share the data pool")
	}

	config := worker.WorkerConfig{
		Name:        name,
		IP:          ip,
		ServerAddr:  apiServer,
		InstallKey:  *installKey,
		Concurrency: *concurrency,
	}

	w, err := worker.NewWorker(config)
	if err != nil {
		logx.Errorf("❌ Create worker failed: %v", err)
		os.Exit(1)
	}

	// 设置 MongoDB 连接（Phase 1 后为必需）
	w.SetMongoDB(mongoClient, mongoDB)

	// 设置任务状态写专用连接池（独立持久化；nil 时内部回退数据池）。
	// 必须在 SetRedis 之前调用：SchedulerClient 构造时绑定状态库。
	w.SetStatusMongoDB(statusClient, statusDB)

	// 初始化 Redis 直连（Phase 2：任务调度层直连 Redis）
	rdb := connectWorkerRedis()
	if rdb != nil {
		if err := w.SetRedis(rdb); err != nil {
			logx.Errorf("❌ Initialize direct scheduler failed: %v; falling back to HTTP scheduling", err)
			rdb = nil
		}
	} else {
		logx.Error("❌ Error: Failed to connect to Redis, falling back to HTTP scheduling")
	}

	// 初始化通知服务（Phase 3：任务完成/失败触发通知）
	if rdb != nil {
		notifySvc := notification.NewService(mongoDB, rdb)
		w.SetNotifyService(notifySvc)
		logx.Info("[Worker] Notification service initialized")
	}

	// 启动Worker
	if err := w.Start(); err != nil {
		logx.Errorf("❌ Start worker failed: %v", err)
		w.Stop()
		os.Exit(1)
	}

	fmt.Println("---------------------------------------------------------")
	logx.Infof("✅ Worker is running successfully")
	logx.Infof("   Name:        %s", name)
	logx.Infof("   IP:          %s", ip)
	logx.Infof("   Concurrency: %d threads", *concurrency)
	fmt.Println("---------------------------------------------------------")

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n---------------------------------------------------------")
	logx.Info("🛑 Shutting down worker...")
	w.Stop()
	logx.Info("👋 Bye!")
}
