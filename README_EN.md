<div align="center">
  <img src="images/logo.png" width="80" alt="CSCAN" />
</div>


<div align="center">

**CSCAN-Enterprise Distributed Network Asset Scanning Platform**

[![Go](https://img.shields.io/badge/Go-1.25.1-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-2.33-green)](VERSION)

[中文](README.md) · [English](README_EN.md)

</div>

---

| Dashboard | Asset Filter | Fingerprint | Vulnerability | Nodes Monitor | Notification |
|:---:|:---:|:---:|:---:|:---:|:---:|
| <img src="images/dashboard.png" width="200"> | <img src="images/filter.png" width="200"> | <img src="images/finger.png" width="200"> | <img src="images/poc.png" width="200"> | <img src="images/worker.png" width="200"> | <img src="images/notice.png" width="200"> |

---

## Quick Start

```bash
# Clone the repository
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan

# Linux/macOS
chmod +x cscan.sh && ./cscan.sh

# Windows
.\cscan.bat
```

> Access `https://ip:3443`, default account `admin / 123456`
>
> Worker nodes must be deployed before executing scans

---

## Local Development

```bash
# 1. Start dependencies
docker-compose -f docker-compose.dev.yaml up -d

# 2. Start services
go run rpc/task/task.go -f rpc/task/etc/task.yaml
go run api/cscan.go -f api/etc/cscan.yaml

# 3. Start frontend
cd web ; npm install ; npm run dev

# 4. Start Worker
go run cmd/worker/main.go -k <install_key> -s http://localhost:8888
```

---

## Worker Deployment

```bash
# Linux
./cscan-worker -k <install_key> -s http://<api_host>:8888

# Windows
cscan-worker.exe -k <install_key> -s http://<api_host>:8888
```

---

## License

MIT
