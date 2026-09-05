中文 | [English](README_EN.md)

<div align="center">
  <img src="images/logo.png" width="80" alt="CSCAN" />
</div>

<div align="center">

**CSCAN - 企业级分布式网络资产扫描平台**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![Fingerprints](https://img.shields.io/badge/%E6%8C%87%E7%BA%B9-41537%2B-orange)](#平台亮点)
[![POCs](https://img.shields.io/badge/POC-13412%2B-red)](#平台亮点)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-5.7-green)](VERSION)

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

## 平台亮点

- **41,537+ 条指纹**：包含 7,523 个 Wappalyzer 应用指纹和 34,014 条内置自定义指纹规则，并支持继续扩展。
- **13,412+ 个 POC**：镜像集成 [Nuclei Templates](https://github.com/projectdiscovery/nuclei-templates)，数量依据上游 [TEMPLATES-STATS.json](https://github.com/projectdiscovery/nuclei-templates/blob/main/TEMPLATES-STATS.json)；实际数量随镜像构建时间和自定义模板变化。
- **分布式扫描**：支持多 Worker、优先级队列、任务分片、定时任务和断线恢复。
- **完整资产视图**：统一管理站点、域名、IP、端口、指纹、漏洞和扫描历史。

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
- 生产部署请覆盖内置默认密钥：`cp .env.example .env`，填入强随机值后再执行 `docker compose up -d`

## 自定义高级 POC

下载地址：[lasest-cscan-poc.zip](http://www.txf7.cn/upload/lasest-cscan-poc.zip)

---

## 本地开发

### 一键启动（推荐）

```bash
# Windows PowerShell
./dev.ps1

# macOS / Linux
./dev.sh
```

---

## License

[MIT](LICENSE)
