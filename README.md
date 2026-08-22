[中文](README.zh.md) | English

<div align="center">
  <img src="images/logo.png" width="80" alt="CSCAN" />
</div>

<div align="center">

**CSCAN - Enterprise Distributed Network Asset Scanning Platform**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![Fingerprints](https://img.shields.io/badge/Fingerprints-38920%2B-orange)](#highlights)
[![POCs](https://img.shields.io/badge/POCs-9000%2B-red)](#highlights)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-5.5-green)](VERSION)

</div>

## Highlights

- **38,920+ built-in fingerprint rules** — 38,920 passive fingerprints (body/title/header/icon_hash syntax, bundled out of the box) plus active probing paths for 90 products
- **9,000+ built-in POCs** — the full official Nuclei template library bundled with online updates, compatible with custom POCs and the advanced POC pack (see download link below)

<table width="100%">
  <tr>
    <td align="center"><b>Dashboard</b></td>
    <td align="center"><b>Asset Space Search</b></td>
    <td align="center"><b>Fingerprint Management</b></td>
    <td align="center"><b>Vulnerability Database</b></td>
    <td align="center"><b>Node Monitoring</b></td>
    <td align="center"><b>Notification Subscription</b></td>
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

## Quick Start

```bash
# Clone the project
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan

# Start (zero config, uses built-in default keys)
docker compose up -d

# Update to the latest images
docker compose pull && docker compose up -d
```

- Access `https://ip:7777`
- For production deployment, override the built-in default keys: `cp .env.example .env`, fill in strong random values, then `docker compose up -d`

## Custom Advanced POC

Download URL: [lasest-cscan-poc.zip](http://www.txf7.cn/upload/lasest-cscan-poc.zip)

---

## Local Development

### One-Click Start (Recommended)
```bash
# Windows PowerShell:
./dev.ps1
```
---

## License

[MIT](LICENSE)
