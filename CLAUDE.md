# CLAUDE.md

## 一、项目架构

### 1.1 项目概述

**cscan** — 企业级分布式网络资产扫描平台（V5.8，以根目录 `VERSION` 为准）

### 1.2 技术栈

| 层级 | 技术 | 当前版本/范围 |
|------|------|---------------|
| 后端框架 | Go + go-zero 微服务框架 | Go 1.26 / go-zero v1.10.2 |
| 前端框架 | Vue 3 + Vite + Element Plus | 声明范围 Vue ^3.4 / Vite ^5.0 / Element Plus ^2.4；精确构建版本以 `web/package-lock.json` 为准 |
| 数据库 | MongoDB + Redis | MongoDB 6 / Redis 7 |
| 驱动 | mongo-driver / go-redis | v1.17.9 / v9.19.0 |
| Worker 通信 | gobwas/ws（WebSocket） | v1.4 |
| 扫描引擎 | ProjectDiscovery + Nmap/Masscan | nuclei v3.11.1 / httpx v1.10.0 / naabu v2.6.1 / subfinder v2.15.0 / ffuf v2.2.1 / fingerprintx v1.1.19 / dnsx v1.3.0；另含 ksubdomain、feroxbuster |
| 截图引擎 | Chromedp（Chrome 无头浏览器） | chromedp v0.15 |
| 资源采集 | gopsutil（CPU/内存/磁盘） | gopsutil v3.24 |
| IP 定位 | ip2region | lionsoul2014/ip2region |
| 任务调度 | robfig/cron（秒级）+ Redis Sorted Set | cron/v3 |
| 认证 | JWT（golang-jwt/v4）+ PAT 令牌 | jwt/v4 v4.5.2 |
| 前端状态管理 | Pinia | ^2.1.7 |
| 国际化 | vue-i18n | ^11.0.2 |
| 图表 | ECharts | ^5.4.3 |
| CSS 预处理 | SCSS（modern-compiler API） | sass v1.97.2 |
| 测试（Go） | testify + gopter（属性测试） | testify v1.11 / gopter v0.2 |
| 测试（前端） | Vitest + happy-dom + fast-check | vitest v4.0.17 |

> `docker/Dockerfile.worker` 中 ProjectDiscovery CLI 固定了版本，但 ksubdomain 使用 `@latest`、feroxbuster 使用 latest release；`docker/Dockerfile.api` 的 Nuclei Templates 也跟随上游 main。涉及可重现构建时应先固定这些浮动输入。

### 1.3 系统架构图

```
[Browser] → [Vue 3 Frontend (web/) :7777]
                  │
            [Vite Proxy /api → :8888]
                  │
           [HTTP API (api/) - go-zero REST :8888]
                  │
          ┌───────┴────────┐
          │                │
      [MongoDB]         [Redis]
          │                │
       全局集合     [Sorted Set 任务队列 cscan:task:queue
                     / Worker 负载 Hash / Pub/Sub 唤醒与 Cron 频道]
                    │
                  [Scheduler (internal/scheduler/)]
                  │  分块 / 优先级 / 负载均衡 / 定时 / 孤儿恢复
                  │
            [Worker nodes (worker/)]  ← Install Key 认证；WebSocket + REST；直连 MongoDB；可选直连 Redis
                  │
            [Scanner modules (internal/scanner/)]
                  │
       ┌────┬─────┼──────┬────────┬───────┬─────┐
  Subfinder Naabu Nmap Httpx Nuclei FFuf Chromedp
```

### 1.4 核心架构模式

- **任务流**: MainTask → ChunkManager 分块（默认 30 目标/片，范围 10-100）→ 每片生成 TaskInfo 入 Redis Sorted Set → Worker 通过 REST `/api/v1/worker/task/check` 长轮询拉取 → API 使用 Lua/ZPOPMIN 先检查 Worker 专属队列再检查公共队列 → 阶段化执行 → REST 上报
- **优先级调度**: ZSet score = `createTime.UnixMicro() - priority * 1_000_000`（小者先出）；服务端 5 级优先级 Background/Low/Normal/High/Urgent；可选 5 桶分队列 `cscan:task:queue:p{0..4}`（默认关闭）。Worker 本地队列仅有 Low/Normal/High/Urgent 4 级
- **负载与连接**: `cscan:worker:load` Hash 保存 Worker 心跳负载；Scheduler 提供基于任务占用量×0.5 + CPU×0.3 + Mem×0.2 的可用 Worker 选择能力。Worker 使用 REST/WebSocket 连接 API、MongoDB 双连接池直写结果，并可选直连 Redis；Redis 不可用时回退 HTTP 调度
- **孤儿恢复**: API 启动 `startOrphanedTaskRecovery`，每 5 分钟检查一次：Redis processing 超过 10 分钟的任务恢复入队，MongoDB STARTED 超过 30 分钟作为兜底。`internal/scheduler/task_recovery.go` 的自适应 RecoveryManager 当前未发现 `Start()` 调用，不应视为主恢复链路；Worker 启动时还会执行本地孤儿任务恢复
- **定时调度**: robfig/cron v3 秒级表达式；CronTask 主存 MongoDB、`cscan:cron:tasks` Hash 缓存；到点 PUBLISH `cscan:cron:execute_scan`（扫描）或 `cscan:cron:execute_space`（空间引擎）；控制频道 `reload/remove/runnow`
- **Worker 通信**: Install Key 认证注册 → WebSocket（`/api/v1/worker/ws`）长连接（AUTH/PING/LOG/LOG_BATCH/CONTROL/LOG_SYNC 消息）+ REST 拉取/回传；断网时结果落本地 `log/result_queue` 自动重放；日志以 Worker 本地 JSONL 为事实源，游标式增量同步到 API 落盘
- **认证体系（5 级）**: 无认证 → Worker Key（`X-Worker-Key` Install Key）→ JWT 或 PAT（`cscan_pat_*`，scope 校验）→ JWT+管理员 → 开放 API（PAT readonly scope + 每 token 限流 120 次/分，超频 429）

---

## 二、项目模块划分

### 2.1 文件与文件夹布局

```
cscan/
├── api/                          # HTTP 服务（go-zero restful 布局）
│   ├── cscan.go                  # API 服务入口点（服务根 main）
│   ├── etc/cscan.yaml            # API 配置（端口8888, JWT, MongoDB, Redis）
│   └── internal/
│       ├── config/config.go      # 配置结构体定义
│       ├── handler/              # 路由处理器（按资源域分子包）
│       │   ├── routes.go         # 统一路由注册（5 级认证）
│       │   ├── asset/ task/ vul/ worker/ fingerprint/ poc/ onlineapi/ user/
│       │   ├── organization/ blacklist/ dirscan/ subdomain/ subfinder/
│       │   ├── notify/ report/ ai/ cert/ container/ dashboard/ jsfinder/
│       │   └── weakpass/ openapi/ role/ techicon/
│       ├── logic/                # 业务逻辑层（平铺，{动作}{实体}logic.go）
│       ├── middleware/           # 中间件
│       │   ├── authmiddleware.go # JWT + PAT 双路径认证、GetUserId/GetRole、RequireAdmin
│       │   ├── workerauth.go     # Worker Install Key 认证（X-Worker-Key）
│       │   ├── ratelimit.go      # TokenRateLimiter 固定窗口限流（429 + Retry-After）
│       │   └── scopes.go         # 开放平台 scope 定义与校验（分组 × CRUD 动作矩阵）
│       ├── svc/                  # 服务上下文（DI 容器 + 服务实现）
│       │   ├── servicecontext.go # ServiceContext 核心结构体
│       │   ├── asset_service.go  # 资产列表 + 扫描结果集成
│       │   ├── docker_service.go # Docker SDK 容器管理（cscan 容器识别/日志）
│       │   ├── log_collector.go  # 容器日志采集（15s 轮询 tail → 本地按日分片文件）
│       │   ├── worker_log_writer.go # Worker 日志落盘（有界队列 + flushLoop，JSONL 按日）
│       │   ├── scanresult_service.go / history_service.go
│       │   └── sync/             # 同步服务（模板/指纹/POC 同步）
│       └── types/                # 请求/响应类型定义
├── worker/                       # Worker 服务（go-zero consumer 布局）
│   ├── main.go                   # Worker 服务入口（package main，-s/-n/-c/-k）
│   └── internal/
│       └── worker/               # package worker（31 个生产 .go + 15 个 _test.go）
│           ├── worker.go         # 核心逻辑（结构体/启动/任务分发）
│           ├── wsclient.go       # WebSocket 客户端（4 泵：读/写/心跳/日志）
│           ├── httpclient.go     # REST 客户端（X-Worker-Key，3 次重试）
│           ├── task_queue_manager.go # 本地 4 级优先级队列（sync.Cond，满则丢最低）
│           ├── resultqueue.go    # 本地结果落盘重放（断网恢复，maxSize 2000）
│           ├── worker_heartbeat.go   # 心跳/熔断/CPU 限流/控制轮询
│           ├── worker_poc_validation.go / worker_fingerprint_validation.go
│           ├── worker_result_save.go / worker_asset_generation.go
│           ├── worker_port_identify.go / worker_brute_scan.go
│           ├── worker_dir_scan.go / worker_js_finder.go
│           ├── worker_target_parse.go / worker_auto_tag.go / worker_utility.go
│           ├── filelogger.go / logsync.go / logwriter.go # 本地日志事实源 + 游标同步
│           ├── mapper.go / sysinfo.go / loadavg_*.go / restart_*.go
│           └── *_test.go         # 单元测试
├── internal/                     # 工程内部公共模块
│   ├── model/                    # MongoDB 数据模型（全局单集合）
│   │   ├── asset.go / asset_history.go / asset_target_meta.go / vul.go / task.go / scanresult.go
│   │   ├── scan_diff.go（变化基线）/ workspace_baseline.go
│   │   ├── cert.go / jsfinder.go / dirscan.go / dirscan_result.go
│   │   ├── crontask.go / scantemplate.go / task_profile.go
│   │   ├── user.go / user_token.go（PAT）/ role.go / organization.go
│   │   ├── fingerprint.go / httpservice.go / active_fingerprint.go / techicon.go
│   │   ├── poc.go（CustomPoc、NucleiTemplate）/ tag_mapping.go
│   │   ├── blacklist.go / notifyconfig.go / subdomain_dict.go / weakpass_dict.go
│   │   ├── subfinderconfig.go / apiconfig.go / commandhistory.go / worker_log.go
│   │   └── indexes.go / base.go / errors.go 及各类 write service
│   ├── scanner/                  # 扫描模块
│   │   ├── target.go / target_preprocessor.go # 目标解析与预处理
│   │   ├── subfinder.go / subdomain_bruteforce.go
│   │   ├── naabu.go / masscan.go / nmap.go / portscan.go
│   │   ├── fingerprint.go / fingerprintx.go / customfinger.go / httpx_lib.go
│   │   ├── nuclei.go / brutescan.go / brute/
│   │   ├── ffuf.go / dirscan.go / jsfinder.go
│   │   ├── certcheck.go / charsetutil.go / utils.go
│   ├── scheduler/                # 任务调度器
│   │   ├── scheduler.go          # Redis Sorted Set 优先级队列（Lua 原子弹入弹、死信）
│   │   ├── service.go            # SchedulerService 门面（cron + 同步）
│   │   ├── chunk_manager.go      # 任务分块（分片信息/进度/清理）
│   │   ├── splitter.go           # 目标拆分（CIDR/IP 范围展开、分片优先级、耗时预估）
│   │   ├── cron.go               # CronManager 定时任务（Mongo 主存 + Redis 缓存）
│   │   └── task_recovery.go      # 自适应恢复管理器实现（当前主链路未调用 Start）
│   └── onlineapi/                # 在线资产搜索（fofa.go / hunter.go / quake.go）
├── pkg/                          # 对外公共模块
│   ├── xerr/                     # 业务错误码体系（errcode.go + errors.go）
│   ├── response/response.go      # 统一 HTTP 响应封装
│   ├── executor/executor.go      # 统一任务执行器（Context 穿透 + 5 层超时管理）
│   ├── cache/local_cache.go      # 本地缓存
│   ├── geolocation/              # IP 地理位置（ip2region 提供者 + 库下载）
│   ├── httpclient/               # HTTP 客户端封装
│   ├── mapping/wappalyzer.go     # Wappalyzer 指纹映射
│   ├── notify/                   # 多渠道通知发送
│   ├── template/parser.go        # Nuclei 模板 YAML 解析（分类/元数据）
│   ├── utils/                    # 通用工具函数
│   └── logfilter.go              # logx 包装器：过滤高频轮询 access log
├── rules/                        # 默认规则与字典（8 个目录，12 个已跟踪规则文件；pocs 当前为空）
│   ├── blacklist/                # default-blacklist.txt
│   ├── fingerprint/              # 自定义指纹与主动探测路径
│   ├── http-service/             # HTTP 服务映射
│   ├── pocs/                     # 自定义 Nuclei POC（当前为空）
│   ├── scan-template/            # 扫描模板
│   ├── subdomain/                # 子域名字典
│   ├── url/                      # URL 字典
│   └── weakpass/                 # 弱口令字典
├── docker/                       # Docker 配置
│   ├── Dockerfile.api / Dockerfile.worker
│   ├── cscan-api.yaml            # 容器内配置
│   ├── mongo-init.js             # MongoDB 初始化脚本
│   └── entrypoint.sh             # 容器启动脚本
├── web/                          # Vue.js 前端
│   ├── src/
│   │   ├── main.js / App.vue / router/index.js（History 模式 + 懒加载）
│   │   ├── stores/               # Pinia（user / theme / locale / onlineSearch / branding）
│   │   ├── api/                  # 按业务域分文件（request.js + 20 个 {domain}.js）
│   │   ├── views/                # 页面视图（Dashboard / AssetManagement / Task / Poc / …）
│   │   ├── components/           # 可复用组件（asset/ 子组件等）
│   │   ├── layouts/MainLayout.vue
│   │   ├── i18n/                 # 国际化（zh-CN / en-US，38 个 section）
│   │   ├── utils/ / styles/
│   │   └── tests/                # Vitest 测试
│   ├── vite.config.js            # Vite 配置（代理、分包、SCSS、test）
│   └── package.json
├── data/                         # ip2region IPv4/IPv6 数据库
├── docker-compose.yaml           # 生产全栈（redis/mongodb/api/web/worker + 资源限制）
├── docker-compose.dev.yaml       # 仅启动本地开发依赖（MongoDB + Redis）
├── docker-compose-worker.yaml    # 独立 Worker 探针部署
├── dev.sh / dev.ps1              # 宿主机一键启动 API/Worker/Web 与开发依赖
├── .github/workflows/build-images.yml   # CI/CD（3 个镜像并行构建推送）
├── go.mod / go.sum / VERSION
└── README.md / README_EN.md / CLAUDE.md
```

### 2.2 业务模块清单

| 模块 | 后端 Handler | 前端视图 | 说明 |
|------|-------------|---------|------|
| 仪表盘 | `handler/dashboard` | `Dashboard.vue` | 资产/风险变化聚合（7 天窗口、增长率、净变化） |
| 资产管理 | `handler/asset` | `AssetManagement.vue` + `components/asset/` + `AssetManagement/*.vue` | 端口/站点/域名/IP/截图/图标/应用/历史/diff/标签/目标（AssetTarget） |
| 资产空间搜索 | `handler/asset` | `AssetSpaceSearch.vue` | 聚合卡片视图检索（AssetInventoryCardView） |
| 任务管理 | `handler/task` | `Task.vue`, `TaskCreate.vue`, `TaskDetail.vue`, `CronTask.vue`, `CronTaskCreate.vue`, `ScanTemplate.vue` | 创建/分块/暂停/恢复/停止/重试/定时/模板/快速创建 |
| 漏洞管理 | `handler/vul` | `VulnerabilityManagement.vue` | 列表/详情/统计/状态/人工复验触发 |
| Worker 管理 | `handler/worker` | `Worker.vue`, `WorkerLogs.vue` | 注册/心跳/WebSocket/日志流 |
| 容器管理 | `handler/container` | 控制台内 | cscan 容器列表/实时日志/历史日志（本地文件） |
| 指纹管理 | `handler/fingerprint` | `Fingerprint.vue` | 指纹 CRUD/分类/同步/主动指纹/HTTP 服务映射 |
| POC 管理 | `handler/poc` | `Poc.vue` | 自定义 POC/Nuclei 模板/AI 生成/批量验证/标签映射 |
| 在线搜索 | `handler/onlineapi` | `OnlineSearch.vue` | FOFA/Hunter/Quake API 聚合、导入、空间引擎 |
| 空间引擎 | `handler/onlineapi` + `internal/scheduler/cron.go` | `space-engine/*` | 在线搜索配置 + 定时抓取（taskType=space_engine） |
| JS 扫描 | `handler/jsfinder` | `AssetManagement/JSFinderPage.vue` | 配置（高危路由/关键词）/结果/AI 研判 |
| 证书监控 | `handler/cert` | `CertAsset.vue` | TLS 证书上报、到期监控（按 not_after 升序） |
| 弱口令 | `handler/weakpass` | 任务创建页内 | 弱口令字典管理（导入/导出/服务统计） |
| 目录扫描 | `handler/dirscan` | `DirectoryManagement.vue` | 字典管理/扫描结果/AI 研判 |
| 用户管理 | `handler/user` | `settings/UserManagement.vue`, `Profile.vue` | CRUD/登录/密码重置/头像/PAT/onboarding/scanConfig |
| 角色管理 | `handler/role` | 设置页相关组件 | 角色与权限配置 |
| 技术图标 | `handler/techicon` | 指纹/资产相关组件 | 技术栈图标查询与管理 |
| 组织管理 | `handler/organization` | `settings/OrganizationManagement.vue` | 组织 CRUD/状态切换 |
| 黑名单 | `handler/blacklist` | `Blacklist.vue` | 全局黑名单规则配置（下发 Worker） |
| 通知/主题/品牌 | `handler/notify` | `settings/NotifyConfig.vue`, `settings/BrandingConfig.vue`, `HighRiskFilter.vue` | 通知配置/高危过滤器/主题/品牌 |
| 报告 | `handler/report` | `Report.vue` | 报告详情/导出/周期报告 |
| 扫描模板 | `handler/task` | `ScanTemplate.vue` | 扫描配置模板管理（task/template） |
| 开放平台 | `handler/openapi` | — | `/api/open/v1` 只读 API（PAT + 限流） |

### 2.3 MongoDB 集合清单（全局单集合，36 个）

`asset`、`asset_history`、`scanresult`、`scan_diff`、`scan_template`、`maintask`、`executor_task`、`cron_task`、`task_profile`、`vul`、`cert`、`jsfinder`、`jsfinder_config`、`dirscan_dict`、`dirscan_result`、`fingerprint`、`active_fingerprint`、`http_service_config`、`http_service_mapping`、`custom_poc`、`nuclei_template`、`tag_mapping`、`user`、`user_tokens`、`command_history`、`organization`、`blacklist_config`、`notify_config`、`subdomain_dict`、`weakpass_dict`、`subfinder_provider`、`api_config`、`system_config`、`tech_icon`、`worker_log`、`workspace_baseline`

> 集合名称以 `internal/model` 的构造器和索引定义为准；新增模型时同步更新本清单。

---

## 三、代码风格与规范

### 3.1 Go 命名约定

| 元素 | 规范 | 示例 |
|------|------|------|
| 文件名 | lowercase_underscore | `scanresult_service.go`, `chunk_manager.go` |
| 包名 | 小写单词 | `model`, `svc`, `handler`, `xerr` |
| 结构体/类型 | PascalCase | `AssetModel`, `ServiceContext`, `TaskExecutor` |
| 导出函数 | PascalCase | `GetAsset()`, `NewWorker()` |
| 未导出函数 | camelCase | `parseResult()`, `recoverOrphanedTasks()` |
| 常量 | PascalCase | `PriorityUrgent`, `TaskStatusSuccess` |
| Context Key | 具名类型 `ContextKey` | `UserIdKey` |
| Handler 函数 | `{Entity}{Action}Handler` | `AssetListHandler`, `WorkerHeartbeatHandler` |
| Logic 文件 | `{动作}{实体}logic.go` | `loginlogic.go`, `jsfinderlogic.go` |

### 3.2 Vue 前端命名约定

| 元素 | 规范 | 示例 |
|------|------|------|
| 视图页面 | PascalCase.vue | `AssetManagement.vue`, `TaskCreate.vue` |
| 内容视图组件 | PascalCase + `View` 后缀 | `SiteView.vue`, `VulView.vue` |
| 标签页组件 | PascalCase + `Tab` 后缀 | `AssetInventoryTab.vue` |
| 布局组件 | PascalCase + `Layout` 后缀 | `MainLayout.vue` |
| API 文件 | camelCase.js | `asset.js`, `crontask.js` |
| Store 文件 | camelCase.js | `user.js`, `theme.js` |

### 3.3 Import 规则

**Go — 三组空行分隔**：
```go
import (
    // 1. 标准库
    "context"
    "time"

    // 2. 内部包
    "cscan/internal/model"
    "cscan/pkg/xerr"
    "cscan/api/internal/middleware"

    // 3. 第三方包
    "go.mongodb.org/mongo-driver/bson"
    "github.com/zeromicro/go-zero/core/logx"
)
```

**Vue — 使用 `@/` 别名**：
```js
import request from '@/api/request'
import { useUserStore } from '@/stores/user'
```

### 3.4 依赖注入

**ServiceContext 作为根 DI 容器**（`api/internal/svc/servicecontext.go`）：

```go
// 工厂函数创建，所有依赖通过构造函数注入
svcCtx := svc.NewServiceContext(config)

// 模型工厂（集合为全局单集合）
assetModel := svcCtx.GetAssetModel()    // 集合: asset
taskModel := svcCtx.GetMainTaskModel()  // 集合: maintask
vulModel := svcCtx.GetVulModel()        // 集合: vul
```

**服务构造器模式**：
```go
type AssetAggregationService struct {
    db *mongo.Database
}

func NewAssetAggregationService(db *mongo.Database) *AssetAggregationService {
    return &AssetAggregationService{db: db}
}
```

**MongoDB 模型构造器模式**：
```go
func NewAssetModel(db *mongo.Database) *AssetModel {
    coll := db.Collection("asset")  // 全局单集合
    return &AssetModel{coll: coll}
}
```

### 3.5 日志规范

- **必须**使用 go-zero 的 `logx`（`logx.Infof / logx.Errorf / logx.Info`）
- **禁止**在业务逻辑中使用 `fmt.Println`
- 日志前缀标签约定：`[模块名]` 格式，如 `[OrphanedTaskRecovery]`
- 高频轮询日志（worker task/check、heartbeat、control）经 `pkg/logfilter.go` 的 `NewFilteredLogWriter` 过滤
- Worker 日志双通道：本地 JSONL 文件为事实源（`log/YYYY-MM-DD.jsonl`，7 天/500MB 上限）+ WebSocket `LOG_BATCH` 增量同步到 API 落盘（`log/worker/YYYY-MM-DD/{worker}.log`，由 `WorkerLogWriter` 有界队列写入）

### 3.6 异常处理

**错误码体系**（`pkg/xerr/errcode.go`）：

| 范围 | 类型 | 示例 |
|------|------|------|
| 0 | 成功 | `OK = 0` |
| 400-500 | HTTP 标准码 | `ParamError=400`, `Unauthorized=401`, `Forbidden=403`, `NotFound=404`, `ServerError=500` |
| 10001-10099 | 用户错误 | `UserNotFound=10001`, `UserPasswordError=10002`, `UserDisabled=10003` |
| 10101-10199 | 任务错误 | `TaskNotFound=10101`, `TaskStatusError=10103` |
| 10301-10399 | 资产错误 | `AssetNotFound=10301` |
| 10401-10699 | 其他业务错误 | `VulNotFound=10401`, `FingerprintNotFound=10501`, `PocNotFound=10601` |

**错误使用方式**：
```go
xerr.NewCodeError(xerr.UserNotFound)          // 使用预定义消息
xerr.NewCodeErrorMsg(xerr.ParamError, "自定义消息") // 自定义消息
xerr.NewParamError("字段不能为空")               // 参数错误快捷函数
xerr.NewServerError("")                        // 服务器错误快捷函数
xerr.NewNotFoundError("")                      // 资源不存在快捷函数
```

**统一响应封装**（`pkg/response/response.go`）：
```go
// 统一响应结构: { "code": 0, "msg": "success", "data": {...} }
response.Success(w, data)                     // 成功
response.Error(w, err)                        // 自动识别 CodeError 或普通 error
response.ErrorWithCode(w, xerr.NotFound, "")  // 指定错误码
response.ParamError(w, "参数校验失败")          // 参数错误
```

### 3.7 参数校验与认证

- 前端请求拦截器自动注入 `Authorization: Bearer <token>` Header
- 用户信息：`GetUserId(ctx)` / `GetUsername(ctx)` / `GetRole(ctx)`；管理员检查：`middleware.RequireAdmin(next)`（校验 `role == "admin"` 或 `"superadmin"`）
- **PAT 认证**：`Authorization` 令牌以 `cscan_pat_` 开头走 PAT 路径（`AuthMiddleware.WithPAT`），校验状态/过期 + `ScopeAllowed` scope；PAT 明文仅创建时返回一次，库中存 HMAC 哈希；修改/重置密码吊销该用户全部 PAT
- **开放 API**：`/api/open/v1/*` 复用 PAT 认证（只读 scope）+ `TokenRateLimiter`（120 次/分，key = tokenId > userId > IP，超频 429）
- **Worker 认证**：`X-Worker-Key` 请求头，双密钥校验（环境变量 `CSCAN_WORKER_KEY` 或 Redis install_key）；Redis 故障返回 503 而非 401
- 任务配置校验：分片配置经 `internal/scheduler/splitter.go` 的 `ValidateChunkConfig` 校验；扫描模块参数在各 logic 层按需校验

### 3.8 Struct Tag 规范

所有 MongoDB 模型**必须同时包含 `bson` 和 `json` 标签**：
```go
type Asset struct {
    Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
    Host       string             `bson:"host" json:"host"`
    Port       int                `bson:"port" json:"port"`
    CreateTime time.Time          `bson:"create_time" json:"createTime"`
    UpdateTime time.Time          `bson:"update_time" json:"updateTime"`
}
```

注意 bson 使用 `snake_case`，json 使用 `camelCase`。

### 3.9 API 路由规范

- 所有端点前缀：`/api/v1/*`；开放平台只读 API 前缀 `/api/open/v1/*`
- HTTP 方法：除健康检查 (`GET /health`)、Worker WebSocket (`GET /api/v1/worker/ws`)、静态文件与部分查询外，**业务接口均为 POST**
- 五层认证级别（见 1.4）：无认证 / Worker Key / JWT+PAT / JWT+管理员 / 开放 API
- 路由注册顺序即中间件叠加顺序：Worker → 认证 → 管理员 → 开放 API
- 接口契约兼容：保留别名路由（如 `/organization/create` → save、`/blacklist/save` → config/save、`/task/cron/create`）

### 3.10 Redis 使用规范

| 用途 | Key 模式 | 数据结构 |
|------|---------|---------|
| 任务队列 | `cscan:task:queue` | Sorted Set（score = UnixMicro - priority×1e6） |
| 优先级分桶 | `cscan:task:queue:p{0..4}` | Sorted Set（可选，默认关闭） |
| Worker 专属队列 | `cscan:task:queue:worker:{name}` | Sorted Set（任务显式指定 Workers 时） |
| 处理中集合 | `cscan:task:processing` | Set |
| 任务信息 | `cscan:task:info:{taskId}` | String (JSON, 24h TTL) |
| 执行信息 | `cscan:task:execution:{taskId}` | String (JSON, 1h TTL，phase/progress/retryCount) |
| 重试计数 | `cscan:task:retry:{taskId}` | String (INCR, 24h TTL) |
| 任务状态 | `cscan:task:status:{taskId}` | String (24h) |
| Worker 负载 | `cscan:worker:load` | Hash (field=workerName, value=负载 JSON) |
| Worker 心跳 | `cscan:worker:{workerName}` | String |
| 任务唤醒 | `cscan:task:available` | Pub/Sub（入队后 PUBLISH "1"） |
| 任务控制 | `cscan:task:ctrl:{taskId}` | Pub/Sub + Key（5min TTL） |
| 死信队列 | `cscan:task:deadletter` | List（+ `:alert` 告警频道） |
| 分片信息 | `cscan:chunk:info/status/task:{id}` | String (24h TTL) |
| 定时任务缓存 | `cscan:cron:tasks` | Hash |
| 定时触发 | `cscan:cron:execute_scan` / `cscan:cron:execute_space` | Pub/Sub |
| 定时控制 | `cscan:cron:reload` / `cscan:cron:remove` / `cscan:cron:runnow` | Pub/Sub |

### 3.11 前端其他规范

- **组件风格**：始终使用 `<script setup>` Composition API
- **UI 组件**：Element Plus 全量引入，图标全局注册（可直接在模板中使用 `<Edit />`）
- **样式**：SCSS，使用 Element Plus CSS 变量实现暗色模式
- **国际化**：模板中使用 `$t('key')`，语言文件位于 `web/src/i18n/locales/`（zh-CN / en-US，按 section 嵌套）
- **状态管理**：Pinia stores 位于 `web/src/stores/`（user 内含用户状态）
- **路由懒加载**：所有组件通过 `lazyLoad()` 包装，chunk 加载失败自动刷新
- **路由 meta 字段**：`requiresAuth`（默认 true）、`title`（中文标题）、`icon`（Element Plus 图标名）、`hidden`（隐藏菜单）
- **纯 JavaScript 项目**：无 TypeScript，所有文件为 `.js` / `.vue`

---

## 四、测试与质量

### 4.1 Go 单元测试

测试文件分布于 `api/internal/svc/`、`api/internal/handler/`、`internal/scheduler/`、`worker/internal/worker/`、`internal/model/`、`pkg/` 目录。

**表格驱动测试模式**：
```go
testCases := []struct {
    name     string
    input    string
    expected int
}{
    {"empty", "", 0},
    {"valid", "test", 4},
}
for _, tc := range testCases {
    t.Run(tc.name, func(t *testing.T) { /* 断言 */ })
}
```

**属性测试模式（gopter）**：
```go
parameters := gopter.DefaultTestParameters()
parameters.MinSuccessfulTests = 100
properties := gopter.NewProperties(parameters)

properties.Property("属性描述", prop.ForAll(
    func(port int) bool {
        if port <= 0 || port > 65535 {
            return true  // guard clause 跳过无效输入
        }
        return someInvariant(port)
    },
    gen.IntRange(1, 65535),
))
properties.TestingRun(t)
```

### 4.2 Go 集成测试

- `handler/worker/heartbeat_concurrency_test.go` — Worker 心跳并发上报测试
- `handler/asset/scanresult_integration_test.go` — 扫描结果集成测试（需本地 MongoDB）
- `handler/api_compatibility_test.go` — API 兼容性测试
- `middleware/authmiddleware_pat_test.go`、`middleware/openapi_scope_test.go`、`middleware/ratelimit_test.go`、`middleware/scopes_test.go` — PAT/scope/限流测试
- `internal/scheduler/scheduler_bucket_test.go` — 分桶队列测试；`internal/scheduler/benchmark_test.go` — 入队/出队/分片基准（Redis 不可用自动 skip）
- `worker/internal/worker/task_queue_manager_test.go`、`worker/internal/worker/concurrency_test.go` — 优先级队列与并发调整测试
- `internal/model/*_test.go`、`pkg/executor/executor_test.go` 等

### 4.3 前端测试

框架已配置（Vitest + happy-dom + fast-check），配置在 `web/vite.config.js` 中：
```js
test: {
    globals: true,
    environment: 'happy-dom',
    setupFiles: ['./src/tests/setup.js'],
    coverage: { provider: 'v8', reporter: ['text', 'json', 'html'] }
}
```

---

## 五、项目构建、测试与运行

### 5.1 环境与配置

**一键启动完整本地栈（推荐）**：
```bash
# Windows PowerShell
./dev.ps1
# macOS / Linux
./dev.sh
```

脚本先通过 `docker-compose.dev.yaml` 启动 MongoDB 与 Redis，再在宿主机分别运行 API、Worker 和 Vite；该 Compose 文件本身不包含应用服务。

**关键配置文件**：
| 文件 | 说明 |
|------|------|
| `api/etc/cscan.yaml` | 本地 API 配置：监听 127.0.0.1:8888，超时 300s，MaxBytes 100MB，JWT 24h |
| `docker/cscan-api.yaml` | 容器 API 配置：监听 0.0.0.0:8888 |

**MongoDB 连接池**：MaxPoolSize=100, MinPoolSize=5, ConnectTimeout=10s

**Redis 连接池**：PoolSize=100, MinIdleConns=10, MaxRetries=3

**Worker 自动并发**：由 `worker/main.go` 根据系统资源推导，并限制在 1-5；显式配置值需按代码中的独立校验逻辑处理。

### 5.2 构建命令

```bash
# Go 后端（仓库根目录）
mkdir -p bin
go build -o bin/cscan-api ./api/cscan.go
go build -o bin/cscan-worker ./worker/main.go
go mod download                         # 下载依赖；仅维护依赖时运行 go mod tidy

# Vue 前端
cd web
npm ci                                  # 按 package-lock.json 安装
npm run build                           # 生产构建（terser 压缩，移除 console）
npm run dev                             # 开发服务器（长驻命令）
```

### 5.3 测试命令

```bash
# Go 测试
go test ./...                                           # 运行所有测试
go test -v ./api/internal/svc/... -run TestFunctionName # 指定测试函数（替换占位名称）
go test -cover ./...                                    # 覆盖率
go test -race ./...                                     # 竞态检测

# Vue 前端测试
cd web
npm run test -- --run                                   # Vitest 单次运行
npx vitest run src/tests/MyComponent.test.js             # 单文件（替换占位路径）
npm run test:coverage -- --run                          # 覆盖率单次运行
npm run test:e2e                                        # Playwright E2E
```

### 5.4 生产部署

```bash
# 全栈部署（redis/mongodb/api/web/worker 五服务）
docker compose pull
docker compose up -d

# 独立 Worker 探针部署
CSCAN_SERVER=http://your-server:8888 CSCAN_KEY=your-key \
  CSCAN_MONGO_URI=mongodb://user:pass@host:27017/cscan?authSource=admin \
  CSCAN_REDIS_ADDR=host:6379 CSCAN_REDIS_PASSWORD=pass \
  docker compose -f docker-compose-worker.yaml up -d
```

生产全栈包含 redis、mongodb、cscan-api、cscan-web、cscan-worker 五个服务。访问 `https://ip:7777`（容器 443→宿主机 7777）或 `http://ip:3000`（容器 80→宿主机 3000）；首次部署按页面引导注册第一个管理员账号（首个注册用户自动获得超级管理员权限，无内置默认账号）。
配置全部经环境变量注入（见 `.env.example`）：`CSCAN_JWT_SECRET` / `CSCAN_MONGO_ROOT_*` / `CSCAN_REDIS_PASSWORD` / `CSCAN_WORKER_KEY`。Mongo URI 必须带 `?authSource=admin`（root 用户位于 admin 库）。

### 5.5 本地编译与浏览器/接口验证

> 本节为开发自测速查；所有命令均在仓库根目录执行。

#### 5.5.1 编译验证（关键陷阱）

**Windows + PowerShell 下，`go` 的 stderr 会被管道吞掉**：`go build ./... | Out-File` 或 `| Select-Object` 后 PowerShell 自身仍返回 exit 0，**即使有编译错误也显示成功**。判断编译成败**必须把 stderr 写入文件再读**：

```powershell
go build ./... 2>&1 | Out-File -Encoding ascii build.log
# 随后用编辑器/Read 打开 build.log，确认无 "# ..." 错误行
```

#### 5.5.2 一键启动完整本地栈

```powershell
./dev.ps1
```

macOS/Linux 使用 `./dev.sh`。脚本通过 Compose 启动 MongoDB/Redis，并在宿主机用 `go run` 启动 API、Worker，再用 Vite 启动 Web；退出脚本时停止应用和依赖栈，开发卷默认保留。

健康检查：`curl http://127.0.0.1:8888/health`，成功响应为 `{"code":0,"msg":"healthy","data":{"status":"ok"}}`；依赖异常时返回 HTTP 503。前端访问 `http://localhost:7777`。

#### 5.5.3 接口冒烟（免登录）

**响应约定**：多数业务逻辑同时返回统一 body（`code=0` 表示成功），但中间件、健康检查、静态资源和开放 API 会使用 401、403、404、429、500、503 等真实 HTTP 状态。冒烟测试必须同时断言 HTTP status 与响应体 `code`，不能假定所有响应均为 200。

全新 MongoDB 无内置用户。自测方式：
1. **自签 JWT**（推荐，免改库）：用 `CSCAN_JWT_SECRET` 以 HS256 签发含 `userId/username/role` 的 token，`Authorization: Bearer <token>` 调用鉴权路由；
2. **注册首用户**：通过登录页注册创建第一个用户，自动获得 superadmin 权限。

#### 5.5.4 浏览器验证（路由遍历 / i18n）

前端国际化检查用 Headless Chrome 遍历 `web/src/router/index.js` 全路由，切英文 locale 采集硬编码中文：
- 遍历脚本预注入 console/network/JS error + `overflowX` 采集钩子，期望 `netErr=0 / jsErr=0 / overflowX=0`；
- i18n 遍历切 `en-US`，命中未翻译中文即定位到源码 `$t()` 缺失处；
- 文案接入规范：模板 `{{ $t('section.key') }}`、`<script setup>` 内 `const { t } = useI18n()` 后 `t('section.key')`；语言文件 `web/src/i18n/locales/{zh-CN,en-US}.json` 按 section 嵌套。修改后使用 `python3 -m json.tool web/src/i18n/locales/zh-CN.json >/dev/null && python3 -m json.tool web/src/i18n/locales/en-US.json >/dev/null` 校验 JSON。

---

## 六、Git 工作流程

### 6.1 分支策略

- 主分支：`main`
- CI/CD 触发：push 到 `main` 或 `master`（忽略 `*.md`、`LICENSE`、`.gitignore` 变更）

### 6.2 CI/CD

`.github/workflows/build-images.yml` — 3 个并行 Job：

| Job | 镜像 | Dockerfile |
|-----|------|-----------|
| `build-api` | `cscan-api` | `docker/Dockerfile.api` |
| `build-web` | `cscan-web` | `web/Dockerfile`（nginx + 多架构） |
| `build-worker` | `cscan-worker` | `docker/Dockerfile.worker` |

所有镜像推送至阿里云容器镜像服务（`registry.cn-hangzhou.aliyuncs.com/txf7`），使用 GitHub Actions cache。

### 6.3 .gitignore 要点

```
web/node_modules/    # 前端依赖
web/dist/            # 前端构建产物
*.exe                # 编译产物
output/              # 输出目录
.DS_Store
screenshot/          # 截图缓存
.env                 # 环境变量
docs/                # 本地文档（已被忽略）
.claude/             # AI 助手配置
/data                # 本地数据
/log                 # 运行日志
.playwright-mcp      # Playwright MCP 痕迹
```

---

## 七、文档目录

### 7.1 文档存储规范

| 文件 | 位置 | 说明 |
|------|------|------|
| `README.md` | 根目录 | 默认中文项目说明、功能特性、快速开始 |
| `README_EN.md` | 根目录 | 英文项目说明，可由 README.md 顶部切换 |
| `CLAUDE.md` | 根目录 | AI 编码助手指导文件 |
| `VERSION` | 根目录 | 当前版本号（V5.8） |
| `LICENSE` | 根目录 | MIT 许可证 |

项目 `docs/` 目录已被 `.gitignore` 忽略（GOZERO_ARCHITECTURE.md / IMPLEMENTATION_SUMMARY.md 已随 go-zero 改造回退删除），无 lint 配置文件（ESLint/Prettier/golangci-lint），无 Makefile。

---

## 八、关键规则

1. **全局集合**: 所有集合均为全局单集合，无 workspace 隔离
2. **保留用户数据**: 更新资产时**必须**保留 `labels`、`memo`、`color_tag`、布尔标志（`isNew`/`isUpdated`）、风险字段、任务追踪字段
3. **API 稳定性**: 禁止修改已有端点路径或 HTTP 方法；新增兼容接口可映射到同一处理器（别名路由模式）
4. **错误码**: 使用 `pkg/xerr/errcode.go` 中的错误码常量
5. **日志**: 使用 go-zero `logx` — 禁止业务逻辑中使用 `fmt.Println`；日志前缀 `[模块名]`
6. **旧数据兼容**: 优雅处理缺少新字段的旧数据（fallback 逻辑，如 `Version==0` 自动赋值 `1`，`ScanTime` 零值回退 `CreateTime`，CronTask 无 `taskType` 时补 `scan`）
7. **类型安全**: 禁止使用 `as any`、`@ts-ignore` 等类型抑制
8. **MongoDB**: 始终使用 `primitive.ObjectID` 作为 `_id`；必须包含 `create_time` 和 `update_time` 字段；`bson` + `json` 双标签（bson snake_case，json camelCase）
9. **响应格式**: 统一使用 `pkg/response` 封装，返回 `{ "code": 0, "msg": "success", "data": {...} }` 结构
10. **任务队列纪律**: 任务入队/弹出/重排必须走 scheduler 的 Lua 原子脚本（防双消费、防丢失）；恢复重排队必须先入队再摘除 processing
11. **交互语言**：工具与模型交互强制使用 **English**；用户输出强制使用 **中文**。
12. **代码主权**：外部模型生成的代码仅作为逻辑参考（Prototype），最终交付代码**必须经过重构**，确保无冗余、企业级标准。
13. **风格定义**：整体代码风格**始终定位**为，精简高效、毫无冗余。该要求同样适用于注释与文档，且对于这两者，严格遵循**非必要不形成**的核心原则。
14. **仅对需求做针对性改动**：严禁影响用户现有的其他功能。
15. **判断依据**：始终以项目代码、grok的搜索结果作为判断依据，严禁使用一般知识进行猜测，允许向用户表明自己的不确定性。在调用编程语言的非内置库时，必须启用grok搜索，以文档作为判断依据进行编码。例如，在调用fastapi库对api接口进行封装时，必须使用联网搜索的最新结果作为依据、阅读官方文档说明编写代码，严禁使用已知的一般知识进行直接编码，这样会直接造成用户项目的崩坏。
16. **禁用临时脚本**：更新任何文件时，不得使用临时脚本、批量代码工具或程序自动改写内容；应使用直接、逐文件、便于人工审阅的修改方式完成更新。
