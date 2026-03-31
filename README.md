# CSCAN

**企业级分布式网络资产扫描平台** | Go-Zero + Vue3

[中文](README.md) | [English](README_EN.md)

[![Go](https://img.shields.io/badge/Go-1.25.1-00ADD8?style=flat&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-2.24-green)](VERSION)

## 快速开始

```bash
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan

# Linux/macOS
chmod +x cscan.sh && ./cscan.sh

# Windows
.\cscan.bat
```

访问 `https://ip:3443`，默认账号 `admin / 123456`

> ⚠️ 执行扫描前需先部署 Worker 节点

## 本地开发

```bash
# 1. 启动依赖
docker-compose -f docker-compose.dev.yaml up -d

# 2. 启动服务
go run rpc/task/task.go -f rpc/task/etc/task.yaml
go run api/cscan.go -f api/etc/cscan.yaml

# 3. 启动前端
cd web ; npm install ; npm run dev

# 4. 启动 Worker
go run cmd/worker/main.go -k <install_key> -s http://localhost:8888
```

## Worker 部署

```bash
# Linux
./cscan-worker -k <install_key> -s http://<api_host>:8888

# Windows
cscan-worker.exe -k <install_key> -s http://<api_host>:8888
```

## License

MIT
