<div align="center">
  <img src="images/logo.png" width="80" alt="CSCAN" />
</div>

<div align="center">

**CSCAN-企业级分布式网络资产扫描平台**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-5.4-green)](VERSION)


</div>

<table width="100%">
  <tr>
    <td align="center"><b>控制台</b></td>
    <td align="center"><b>资产空间搜索</b></td>
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

## 快速开始

```bash
# 克隆项目
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan

# 启动（零配置，使用内置默认密钥）
docker compose up -d

# 更新镜像
docker compose pull && docker compose up -d
```

- 访问 `https://ip:7777`
- 生产部署请覆盖内置默认密钥：`cp .env.example .env`，填入强随机值后再 `docker compose up -d`
---

## 本地开发

### 一键启动（推荐）
```bash
# Windows PowerShell:
./dev.ps1
```
---

## License

[MIT](LICENSE)