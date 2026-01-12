# CSCAN

**企业级分布式网络资产扫描平台** | Go-Zero + Vue3

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

<img src="images/cscan.png" alt="CSCAN" width="250"/>

## 功能特性

- **资产发现** - 端口扫描 (Naabu/Masscan)，端口服务识别 (Nmap)
- **子域名枚举** - Subfinder + Dnsx 集成，支持字典爆破，建议配置多数据源
- **指纹识别** - Httpx + Wappalyzer + 自定义指纹引擎，3W+ 指纹规则
- **URL 发现** - Urlfinder 集成，自动发现目标 URL 路径
- **漏洞检测** - Nuclei SDK 引擎，支持所有默认 POC，增加 800+ 自定义 POC
- **Web 截图** - Chromedp / HTTPX 引擎
- **在线数据源** - FOFA / Hunter / Quake API 聚合搜索与导入
- **报告管理** - 任务报告生成，支持 Excel 导出
- **分布式架构** - Worker 节点水平扩展，支持多节点并行扫描
- **多工作空间** - 项目隔离，团队协作
- **审计日志** - 操作记录追踪

## 快速开始

```bash
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan

# 使用管理脚本（推荐）
chmod +x cscan.sh
./cscan.sh

# 或直接启动
docker-compose up -d
```

访问 `https://ip:3443`，默认账号 `admin / 123456`

> **注意：执行扫描之前需要手动先部署 Worker 节点**

## 管理脚本

提供交互式菜单和命令行两种方式：

```bash
# 交互式菜单
./cscan.sh

# 命令行方式
./cscan.sh install    # 一键安装
./cscan.sh upgrade    # 一键升级
./cscan.sh uninstall  # 一键卸载
./cscan.sh status     # 查看状态
./cscan.sh start      # 启动服务
./cscan.sh stop       # 停止服务
./cscan.sh restart    # 重启服务
./cscan.sh logs       # 查看日志

# Windows
cscan.bat
```

升级过程会自动保留数据（MongoDB、Redis、JWT密钥等）。

## 架构说明

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Web UI    │────▶│   API 服务   │────▶│  RPC 服务   │
│  (Vue3)     │     │  (go-zero)  │     │  (go-zero)  │
└─────────────┘     └─────────────┘     └─────────────┘
                           │                   │
                           ▼                   ▼
                    ┌─────────────┐     ┌─────────────┐
                    │   MongoDB   │     │    Redis    │
                    └─────────────┘     └─────────────┘
                           ▲
                           │
                    ┌─────────────┐
                    │   Worker    │ ← 可水平扩展
                    │  (扫描节点)  │
                    └─────────────┘
```

## 项目结构

```
cscan/
├── api/                # API 服务 (HTTP 接口)
├── rpc/                # RPC 服务 (内部通信)
├── worker/             # Worker 扫描节点
├── scanner/            # 扫描引擎
│   ├── naabu.go        # Naabu 端口扫描
│   ├── masscan.go      # Masscan 端口扫描
│   ├── nmap.go         # Nmap 服务识别
│   ├── subfinder.go    # 子域名枚举
│   ├── httpx_lib.go    # HTTP 探测
│   ├── fingerprint.go  # 指纹识别
│   ├── nuclei.go       # 漏洞扫描
│   └── urlfinder.go    # URL 发现
├── scheduler/          # 任务调度器
├── model/              # 数据模型
├── onlineapi/          # 在线 API 集成 (FOFA/Hunter/Quake)
├── web/                # 前端 (Vue3 + Element Plus)
└── docker/             # Docker 配置文件
```

## 本地开发

```bash
# 1. 启动依赖服务（MongoDB + Redis）
docker-compose -f docker-compose.dev.yaml up -d

# 2. 启动 RPC 服务
go run rpc/task/task.go -f rpc/task/etc/task.yaml

# 3. 启动 API 服务
go run api/cscan.go -f api/etc/cscan.yaml

# 4. 启动前端
cd web && npm install && npm run dev

# 5. 启动 Worker（需 API 地址）
# 从 Web 界面获取安装命令，或使用 API 获取安装密钥
go run cmd/worker/main.go -k <install_key> -s http://localhost:8888
```

访问 `http://localhost:3000`

## 部署

### Docker Compose (推荐)

```bash
docker-compose up -d
```

### 独立 Worker 部署

Worker 支持独立部署在任意服务器，只需连接到 API 服务：

```bash
# Linux
./cscan-worker -k <install_key> -s http://<api_host>:8888

# Windows
cscan-worker.exe -k <install_key> -s http://<api_host>:8888
```

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端框架 | Go-Zero |
| 前端框架 | Vue 3.4 + Element Plus + Vite |
| 数据库 | MongoDB 6 |
| 缓存 | Redis 7 |
| 端口扫描 | Naabu / Masscan |
| 服务识别 | Nmap |
| 指纹识别 | Httpx + Wappalyzer |
| 漏洞扫描 | Nuclei |
| 子域名枚举 | Subfinder + Dnsx |
| URL 发现 | Urlfinder |

## License

MIT
