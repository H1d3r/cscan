package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cscan/worker"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stat"
)

var (
	// 新参数：-s 改为 API 服务地址（支持环境变量 CSCAN_SERVER）
	serverAddr  = flag.String("s", getEnvOrDefault("CSCAN_SERVER", "http://localhost:8888"), "API server address (e.g., http://192.168.1.100:8888)")
	workerName  = flag.String("n", getEnvOrDefault("CSCAN_NAME", ""), "worker name (default: hostname-pid)")
	concurrency = flag.Int("c", getEnvIntOrDefault("CSCAN_CONCURRENCY", 5), "concurrency")
	installKey  = flag.String("k", getEnvOrDefault("CSCAN_KEY", ""), "install key for authentication")
)

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

// validateInstallKey 验证安装密钥
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

	// 发送验证请求，带重试
	for i := 0; i < 3; i++ {
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			logx.Infof("⚠️  Validation attempt %d failed: %v, retrying...", i+1, err)
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Code  int    `json:"code"`
			Msg   string `json:"msg"`
			Valid bool   `json:"valid"`
		}
		json.Unmarshal(body, &result)
		if result.Code != 0 || !result.Valid {
			return fmt.Errorf("validation failed: %s", result.Msg)
		}
		return nil
	}
	return fmt.Errorf("validation failed after 3 attempts")
}

func main() {
	flag.Parse()
	logx.MustSetup(logx.LogConf{
		ServiceName: "cscan-worker",
		Mode:        "console",  // 开启控制台颜色
		Encoding:    "plain",    // 纯文本格式
		TimeFormat:  "15:04:05", // 简洁时间格式
		Level:       "info",     // 日志级别
		Stat:        false,      // 关闭资源统计
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

	// 强制要求安装密钥
	if *installKey == "" {
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

	// 验证安装密钥
	if err := validateInstallKey(apiServer, *installKey, name); err != nil {
		logx.Errorf("❌ Authentication failed: %v", err)
		os.Exit(1)
	}
	logx.Info("✅ Identity verified successfully")
	// 获取本机IP
	ip := worker.GetLocalIP()

	config := worker.WorkerConfig{
		Name:        name,
		IP:          ip,
		ServerAddr:  apiServer,
		InstallKey:  *installKey,
		Concurrency: *concurrency,
		Timeout:     3600,
	}

	w, err := worker.NewWorker(config)
	if err != nil {
		logx.Errorf("❌ Create worker failed: %v", err)
		os.Exit(1)
	}

	// 启动Worker
	w.Start()

	fmt.Println("---------------------------------------------------------")
	logx.Infof("✅ Worker is running successfully")
	logx.Infof("   Name:        %s", name)
	logx.Infof("   IP:          %s", ip)
	logx.Infof("   Concurrency: %d threads", *concurrency)
	logx.Infof("📡 Waiting for tasks from dispatch center...")
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
