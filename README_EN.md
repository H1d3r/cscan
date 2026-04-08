# CSCAN

**Enterprise Distributed Network Asset Scanning Platform** | Go-Zero + Vue3

[中文](README.md) | [English](README_EN.md)

[![Go](https://img.shields.io/badge/Go-1.25.1-00ADD8?style=flat&logo=go)](https://golang.org)
[![Vue](https://img.shields.io/badge/Vue-3.4-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-2.30-green)](VERSION)

## Screenshots

| Dashboard | Asset Filter | Fingerprint Management |  Vulnerability |  Nodes Monitor | Notification Config |
| :---: | :---: | :---: | :---: | :---: | :---: |
| <img src="images/dashboard.png"> | <img src="images/filter.png"> | <img src="images/finger.png"> | <img src="images/poc.png"> | <img src="images/worker.png"> | <img src="images/notice.png"> |

## Quick Start

```bash
git clone https://github.com/tangxiaofeng7/cscan.git
cd cscan

# Linux/macOS
chmod +x cscan.sh && ./cscan.sh

# Windows
.\cscan.bat
```

Access `https://ip:3443`, default account `admin / 123456`

> ⚠️ Worker nodes must be deployed before executing scans

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

## Worker Deployment

```bash
# Linux
./cscan-worker -k <install_key> -s http://<api_host>:8888

# Windows
cscan-worker.exe -k <install_key> -s http://<api_host>:8888
```

## License

MIT
