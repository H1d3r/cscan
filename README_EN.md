[中文](README.md) | English

<div align="center">
  <img src="images/logo.png" width="80" alt="CSCAN" />
</div>

<div align="center">

**CSCAN - Enterprise Distributed Network Asset Scanning Platform**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![Fingerprints](https://img.shields.io/badge/Fingerprints-41537%2B-orange)](#highlights)
[![POCs](https://img.shields.io/badge/POCs-13412%2B-red)](#highlights)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-5.7-green)](VERSION)

</div>

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

## Highlights

- **41,537+ fingerprints**: 7,523 Wappalyzer application fingerprints and 34,014 built-in custom fingerprint rules, with support for additional rules.
- **13,412+ POCs**: Images include [Nuclei Templates](https://github.com/projectdiscovery/nuclei-templates). The count follows the upstream [TEMPLATES-STATS.json](https://github.com/projectdiscovery/nuclei-templates/blob/main/TEMPLATES-STATS.json); the actual total varies with image build time and custom templates.
- **Distributed scanning**: Multiple workers, priority queues, task chunking, scheduled tasks, and offline recovery.
- **Unified asset view**: Manage sites, domains, IPs, ports, fingerprints, vulnerabilities, and scan history in one place.

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
- For production deployment, override the built-in default keys: `cp .env.example .env`, fill in strong random values, then run `docker compose up -d`

## Custom Advanced POC

Download URL: [lasest-cscan-poc.zip](http://www.txf7.cn/upload/lasest-cscan-poc.zip)

---

## Local Development

### One-Click Start (Recommended)

```bash
# Windows PowerShell
./dev.ps1

# macOS / Linux
./dev.sh
```

---

## License

[MIT](LICENSE)
