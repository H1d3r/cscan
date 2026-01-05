# CSCAN

**分布式网络资产扫描平台** | Go-Zero + Vue3

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## 目录
<details>
<summary>⭐ 介绍 </summary>

- **资产发现** - 端口扫描 (Naabu/Masscan)，端口服务识别（Nmap）
- **指纹识别** - Httpx + Wappalyzer + 自定义指纹引擎，3W+自定义指纹
- **漏洞检测** - Nuclei SDK引擎，800+自定义POC
- **Web 截图** - Chromedp / HTTPX 引擎
- **在线数据源** - FOFA / Hunter / Quake API 聚合搜索与导入
- **报告管理** - 任务报告生成，支持 Excel 导出
- **分布式架构** - Worker 节点水平扩展，支持多节点并行扫描
- **多工作空间** - 项目隔离，团队协作
</details>

<details open>
<summary>⭐ 快速开始 </summary>

```bash
#自动选择 amd64架构 或arm64架构
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan
docker-compose up -d
```

访问 `http://localhost:3000`，默认账号 `admin / 123456`
</details>

<details>
<summary>⭐ 本地开发 </summary>

```bash
# 1. 启动依赖服务（MongoDB + Redis）
docker-compose -f docker-compose.dev.yaml up -d

# 2. 启动 RPC 服务
go run rpc/task/task.go -f rpc/task/etc/task.yaml

# 3. 启动 API 服务
go run api/cscan.go -f api/etc/cscan.yaml

# 4. 启动 Worker
# 从Web界面获取安装密钥
go run cmd/worker/main.go -k <install_key> -s http://localhost:8888

# 5. 启动前端
cd web; npm install; npm run dev
```
</details>

## License

MIT

## 加入交流群
<img src="images/cscan.png" alt="交流群" width="200"/>
