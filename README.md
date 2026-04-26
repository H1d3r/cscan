<div align="center">
  <img src="images/logo.png" width="80" alt="CSCAN" />
</div>

<div align="center">

**CSCAN-企业级分布式网络资产扫描平台**

[![Go](https://img.shields.io/badge/Go-1.25.1-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-3.0-green)](VERSION)

[中文](README.md) · [English](README_EN.md)

</div>

---

| 控制台 | 资产检索 | 指纹管理 | 漏洞库 | 节点监控 | 通知订阅 |
|:---:|:---:|:---:|:---:|:---:|:---:|
| <img src="images/dashboard.png" width="200"> | <img src="images/filter.png" width="200"> | <img src="images/finger.png" width="200"> | <img src="images/poc.png" width="200"> | <img src="images/worker.png" width="200"> | <img src="images/notice.png" width="200"> |

---

## 功能特性

### 扫描引擎

| 扫描阶段 | 说明 | 工具 |
|:---|:---|:---|
| 子域名扫描 | 子域名枚举与发现 | Subfinder / Ksubdomain |
| 端口扫描 | 全端口/指定端口快速扫描 | Naabu / Masscan |
| 端口识别 | 服务版本识别 | Nmap / Fingerprintx |
| 指纹识别 | Web 指纹与 Icon Hash 识别 | HTTPX / 内置引擎 |
| 弱口令扫描 | 多服务弱口令爆破 | 内置引擎 |
| 目录扫描 | 目录与文件枚举 | FFUF |
| 漏洞扫描 | POC 漏洞验证与扫描 | Nuclei |

### 核心能力

- **分布式架构** - Master/Worker 分离，支持多节点弹性扩缩容
- **流水线编排** - 扫描阶段自动串联，前序结果自动传递给后续阶段
- **弱口令字典管理** - 内置默认字典，支持自定义字典增删改查、导入导出
- **定时任务** - Cron 表达式驱动的周期性扫描任务
- **资产分组** - 按域名自动聚合资产，实时反映任务状态
- **多工作空间** - 租户级数据隔离，支持组织/团队维度管理
- **通知订阅** - 扫描结果实时推送（钉钉/飞书/企业微信/邮件/Webhook）

---

## 快速开始

```bash
# 克隆项目
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan

# Linux/macOS
chmod +x cscan.sh && ./cscan.sh

# Windows
.\cscan.bat
```

> 访问 `https://ip:7777`，默认账号 `admin / 123456`
>
> 执行扫描前需先部署 Worker 节点

---

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

---

## Worker 部署

```bash
# Linux
./cscan-worker -k <install_key> -s http://<api_host>:8888

# Windows
cscan-worker.exe -k <install_key> -s http://<api_host>:8888
```

---

## License

MIT
