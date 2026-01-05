# CSCAN

**分布式网络资产扫描平台** | Go-Zero + Vue3

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

<img src="images/cscan.png" alt="CSCAN" width="600"/>

## 功能特性

- **资产发现** - 端口扫描 (Naabu/Masscan)，端口服务识别 (Nmap)
- **子域名枚举** - Subfinder 集成，支持多数据源
- **指纹识别** - Httpx + Wappalyzer + 自定义指纹引擎，3W+ 指纹规则
- **漏洞检测** - Nuclei SDK 引擎，800+ 自定义 POC
- **Web 截图** - Chromedp / HTTPX 引擎
- **在线数据源** - FOFA / Hunter / Quake API 聚合搜索与导入
- **报告管理** - 任务报告生成，支持 Excel 导出
- **分布式架构** - Worker 节点水平扩展，支持多节点并行扫描
- **多工作空间** - 项目隔离，团队协作

## 快速开始

```bash
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan
docker-compose up -d
```

访问 `http://ip:3000`，默认账号 `admin / 123456`

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
├── scanner/            # 扫描引擎 (Naabu/Nmap/Nuclei/Httpx)
├── scheduler/          # 任务调度器
├── model/              # 数据模型
├── onlineapi/          # 在线 API 集成 (FOFA/Hunter/Quake)
├── web/                # 前端 (Vue3 + Element Plus)
└── docker/             # Docker 配置文件
```

## 本地开发

### 1. 启动依赖服务

```bash
docker-compose -f docker-compose.dev.yaml up -d
```

### 2. 启动后端服务

```bash
# RPC 服务
go run rpc/task/task.go -f rpc/task/etc/task.yaml

# API 服务
go run api/cscan.go -f api/etc/cscan.yaml
```

### 3. 启动 Worker

从 Web 界面获取安装密钥后：

```bash
go run cmd/worker/main.go -k <install_key> -s http://localhost:8888
```

Worker 参数说明：

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-k` | 安装密钥 (必须) | - |
| `-s` | API 服务地址 (必须) | http://localhost:8888 |
| `-n` | Worker 名称 | 自动生成 |
| `-c` | 并发数 | 5 |

### 4. 启动前端

```bash
cd web
npm install
npm run dev
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
| 前端框架 | Vue 3 + Element Plus |
| 数据库 | MongoDB |
| 缓存 | Redis |
| 端口扫描 | Naabu / Masscan |
| 服务识别 | Nmap |
| 指纹识别 | Httpx + Wappalyzer |
| 漏洞扫描 | Nuclei |
| 子域名枚举 | Subfinder |

## License

MIT
