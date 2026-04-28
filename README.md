<div align="center">
  <img src="images/logo.png" width="80" alt="CSCAN" />
</div>

<div align="center">

**CSCAN-企业级分布式网络资产扫描平台**

[![Go](https://img.shields.io/badge/Go-1.25.1-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-3.1-green)](VERSION)

[中文](README.md) · [English](README_EN.md)

</div>

---

<table width="100%">
  <tr>
    <td align="center"><b>控制台</b></td>
    <td align="center"><b>资产检索</b></td>
    <td align="center"><b>指纹管理</b></td>
    <td align="center"><b>漏洞库</b></td>
    <td align="center"><b>节点监控</b></td>
    <td align="center"><b>通知订阅</b></td>
  </tr>
  <tr>
    <td align="center"><img src="images/dashboard.png"></td>
    <td align="center"><img src="images/filter.png"></td>
    <td align="center"><img src="images/finger.png"></td>
    <td align="center"><img src="images/poc.png"></td>
    <td align="center"><img src="images/worker.png"></td>
    <td align="center"><img src="images/notice.png"></td>
  </tr>
</table>

---

## 功能特性
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
