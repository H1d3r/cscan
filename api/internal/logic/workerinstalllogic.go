package logic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// WorkerInstallLogic Worker安装逻辑
type WorkerInstallLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewWorkerInstallLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WorkerInstallLogic {
	return &WorkerInstallLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// generateInstallKey 生成安装密钥
func generateInstallKey() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:16]
}

// GetInstallCommand 获取Worker安装命令
// Worker 现在采用单端口通信架构，只需连接 API 服务端口
func (l *WorkerInstallLogic) GetInstallCommand(req *types.WorkerInstallCommandReq) (*types.WorkerInstallCommandResp, error) {
	rdb := l.svcCtx.RedisClient

	// 获取或生成安装密钥
	installKeyKey := "cscan:worker:install_key"
	installKey, err := rdb.Get(l.ctx, installKeyKey).Result()
	if err != nil || installKey == "" {
		// 生成新的安装密钥
		installKey = generateInstallKey()
		// 保存到Redis，永不过期（可以通过重新生成来更新）
		rdb.Set(l.ctx, installKeyKey, installKey, 0)
		l.Logger.Infof("[WorkerInstall] Generated new install key: %s", installKey)
	}

	// 构建API服务地址
	// 优先使用请求参数中的主机名，端口始终使用配置中的 API 端口
	serverAddr := req.ServerAddr
	// 移除 serverAddr 中的协议前缀，统一处理
	serverAddrClean := serverAddr
	if len(serverAddrClean) > 8 && serverAddrClean[:8] == "https://" {
		serverAddrClean = serverAddrClean[8:]
	} else if len(serverAddrClean) > 7 && serverAddrClean[:7] == "http://" {
		serverAddrClean = serverAddrClean[7:]
	}

	// 从配置中获取 API 服务端口
	apiPort := l.svcCtx.Config.Port
	if apiPort == 0 {
		apiPort = 8888 // 默认端口
	}

	// 如果前端没有传地址，或者只传了主机名，则使用配置中的端口
	if serverAddrClean == "" {
		serverAddrClean = fmt.Sprintf("localhost:%d", apiPort)
	} else {
		// 提取主机名部分（去掉可能的端口），然后使用配置中的端口
		host := serverAddrClean
		if idx := len(host) - 1; idx > 0 {
			for i := len(host) - 1; i >= 0; i-- {
				if host[i] == ':' {
					host = host[:i]
					break
				}
				if host[i] == ']' || host[i] < '0' || host[i] > '9' {
					break
				}
			}
		}
		serverAddrClean = fmt.Sprintf("%s:%d", host, apiPort)
	}

	// 生成各平台的安装命令（单端口通信，只需 -k 和 -s 参数）
	commands := make(map[string]string)

	// Linux安装命令
	commands["linux"] = fmt.Sprintf(
		`curl -fsSL http://%s/api/v1/worker/download?os=linux -o cscan-worker && chmod +x cscan-worker && ./cscan-worker -k %s -s %s`,
		serverAddrClean, installKey, serverAddrClean,
	)

	// Linux一键安装（后台运行）
	commands["linux_daemon"] = fmt.Sprintf(
		`curl -fsSL http://%s/api/v1/worker/download?os=linux -o cscan-worker && chmod +x cscan-worker && nohup ./cscan-worker -k %s -s %s > /dev/null 2>&1 &`,
		serverAddrClean, installKey, serverAddrClean,
	)

	// Windows安装命令（PowerShell）
	commands["windows"] = fmt.Sprintf(
		`Invoke-WebRequest -Uri "http://%s/api/v1/worker/download?os=windows" -OutFile cscan-worker.exe; .\cscan-worker.exe -k %s -s %s`,
		serverAddrClean, installKey, serverAddrClean,
	)

	// Windows安装命令（CMD + certutil）
	commands["windows_cmd"] = fmt.Sprintf(
		`certutil -urlcache -split -f http://%s/api/v1/worker/download?os=windows cscan-worker.exe && cscan-worker.exe -k %s -s %s`,
		serverAddrClean, installKey, serverAddrClean,
	)

	// // macOS安装命令
	// commands["darwin"] = fmt.Sprintf(
	// 	`curl -fsSL http://%s/api/v1/worker/download?os=darwin -o cscan-worker && chmod +x cscan-worker && ./cscan-worker -k %s -s %s`,
	// 	serverAddrClean, installKey, serverAddrClean,
	// )

	// 注意：RpcAddr 和 RedisAddr 保留在响应中以保持向后兼容，但不再使用
	return &types.WorkerInstallCommandResp{
		Code:       0,
		Msg:        "success",
		InstallKey: installKey,
		ServerAddr: serverAddrClean,
		RpcAddr:    "", // 已废弃，Worker 不再直接连接 RPC
		RedisAddr:  "", // 已废弃，Worker 不再直接连接 Redis
		Commands:   commands,
	}, nil
}

// RefreshInstallKey 刷新安装密钥
func (l *WorkerInstallLogic) RefreshInstallKey() (*types.WorkerRefreshKeyResp, error) {
	rdb := l.svcCtx.RedisClient

	// 生成新的安装密钥
	newKey := generateInstallKey()

	// 保存到Redis
	installKeyKey := "cscan:worker:install_key"
	rdb.Set(l.ctx, installKeyKey, newKey, 0)

	l.Logger.Infof("[WorkerInstall] Refreshed install key: %s", newKey)

	return &types.WorkerRefreshKeyResp{
		Code:       0,
		Msg:        "安装密钥已刷新",
		InstallKey: newKey,
	}, nil
}

// ValidateInstallKey 验证安装密钥
func (l *WorkerInstallLogic) ValidateInstallKey(req *types.WorkerValidateKeyReq) (*types.WorkerValidateKeyResp, error) {
	rdb := l.svcCtx.RedisClient

	// 获取当前安装密钥
	installKeyKey := "cscan:worker:install_key"
	storedKey, err := rdb.Get(l.ctx, installKeyKey).Result()
	if err != nil || storedKey == "" {
		return &types.WorkerValidateKeyResp{
			Code:  401,
			Msg:   "安装密钥未配置",
			Valid: false,
		}, nil
	}

	// 验证密钥
	if req.InstallKey != storedKey {
		l.Logger.Errorf("[WorkerInstall] Invalid install key attempt: %s", req.InstallKey)
		return &types.WorkerValidateKeyResp{
			Code:  401,
			Msg:   "安装密钥无效",
			Valid: false,
		}, nil
	}

	// 记录Worker注册信息
	workerInfo := fmt.Sprintf(`{"ip":"%s","os":"%s","arch":"%s","registerTime":"%s"}`,
		req.WorkerIP, req.WorkerOS, req.WorkerArch, time.Now().Format("2006-01-02 15:04:05"))
	registerKey := fmt.Sprintf("cscan:worker:register:%s", req.WorkerName)
	rdb.Set(l.ctx, registerKey, workerInfo, 24*time.Hour)

	l.Logger.Infof("[WorkerInstall] Worker registered: name=%s, ip=%s, os=%s, arch=%s",
		req.WorkerName, req.WorkerIP, req.WorkerOS, req.WorkerArch)

	return &types.WorkerValidateKeyResp{
		Code:  0,
		Msg:   "验证成功",
		Valid: true,
	}, nil
}

// GetWorkerBinaryInfo 获取Worker二进制文件信息
func (l *WorkerInstallLogic) GetWorkerBinaryInfo(osType, arch string) (*types.WorkerBinaryInfoResp, error) {
	// 默认值
	if osType == "" {
		osType = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}

	// 构建文件名
	filename := "cscan-worker"
	if osType == "windows" {
		filename = "cscan-worker.exe"
	}

	// 这里可以从配置或数据库获取实际的二进制文件路径
	// 目前返回基本信息
	return &types.WorkerBinaryInfoResp{
		Code:     0,
		Msg:      "success",
		Filename: filename,
		OS:       osType,
		Arch:     arch,
		Version:  "1.0.0",
	}, nil
}
