# CSCAN

**企业级分布式网络资产扫描平台** | Go-Zero + Vue3

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

<img src="images/cscan.png" alt="CSCAN" width="250"/>

## 功能特性

| 模块 | 功能 | 工具 |
|------|------|------|
| 资产发现 | 端口扫描、服务识别 | Naabu / Masscan / Nmap |
| 子域名枚举 | 被动枚举 + 字典爆破 | Subfinder + Dnsx |
| 指纹识别 | Web 指纹、3W+ 规则 | Httpx + Wappalyzer + 自定义引擎 |
| URL 发现 | 路径爬取 | Urlfinder |
| 漏洞检测 | POC 扫描、800+ 自定义 POC | Nuclei SDK |
| Web 截图 | 页面快照 | Chromedp / HTTPX |
| 在线数据源 | API 聚合搜索 | FOFA / Hunter / Quake |

**平台能力**：分布式架构 · 多工作空间 · 报告导出 · 审计日志

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

## 架构

```
┌─────────┐     ┌─────────┐     ┌─────────┐
│  Web UI │────▶│   API   │────▶│   RPC   │
│ (Vue3)  │     │(go-zero)│     │(go-zero)│
└─────────┘     └────┬────┘     └────┬────┘
                     │               │
                     ▼               ▼
              ┌──────────┐    ┌──────────┐
              │ MongoDB  │    │  Redis   │
              └────┬─────┘    └──────────┘
                   │
            ┌──────┴──────┐
            │   Worker    │ ← 水平扩展
            │  (扫描节点)  │
            └─────────────┘
```

## 项目结构

```
cscan/
├── api/          # HTTP API 服务
├── rpc/          # RPC 内部通信
├── worker/       # 扫描节点
├── scanner/      # 扫描引擎
├── scheduler/    # 任务调度
├── model/        # 数据模型
├── pkg/          # 公共工具库
├── onlineapi/    # FOFA/Hunter/Quake 集成
├── web/          # Vue3 前端
└── docker/       # Docker 配置
```

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

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go-Zero |
| 前端 | Vue 3.4 + Element Plus + Vite |
| 存储 | MongoDB 6 + Redis 7 |
| 扫描 | Naabu / Masscan / Nmap / Subfinder / Httpx / Nuclei |

## License

MIT
