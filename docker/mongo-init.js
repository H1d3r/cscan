// MongoDB初始化脚本
db = db.getSiblingDB('cscan');

// 创建默认工作空间
var workspaceResult = db.workspace.insertOne({
    name: "默认工作空间",
    description: "系统默认工作空间",
    status: "enable",
    create_time: new Date(),
    update_time: new Date()
});

var defaultWorkspaceId = workspaceResult.insertedId.toString();

// 创建用户集合并插入默认管理员，关联默认工作空间
db.user.insertOne({
    username: "admin",
    password: "$2a$10$Y/T1J1j6tEB9KQI2FlpyNOK3DY2eT54Ml1ukG.dMrbCjMt5Ic7MwK", // 123456的bcrypt哈希
    role: "superadmin",
    status: "enable",
    workspace_ids: [defaultWorkspaceId],
    create_time: new Date(),
    update_time: new Date()
});

// 创建默认任务配置
db.task_profile.insertOne({
    name: "默认扫描",
    description: "cscan项目默认扫描配置",
    config: JSON.stringify({
        batchSize: 5,
        domainscan: { enable: false, timeout: 30, maxEnumerationTime: 10, threads: 10, rateLimit: 0, all: false, recursive: false, removeWildcard: true, resolveDNS: true, concurrent: 50 },
        portscan: { enable: true, tool: "naabu", rate: 1000, ports: "top1000", portThreshold: 50, scanType: "c", timeout: 600, skipHostDiscovery: false },
        portidentify: { enable: false, timeout: 30, args: "" },
        fingerprint: { enable: true, httpx: true, iconHash: true, wappalyzer: false, customEngine: true, screenshot: true, targetTimeout: 30, concurrency: 2 },
        pocscan: { enable: true, useNuclei: true, autoScan: true, automaticScan: true, customPocOnly: false, severity: "critical,high,medium,low,info", targetTimeout: 600 }
    }),
    sort_number: 1,
    create_time: new Date(),
    update_time: new Date()
});

// 创建索引
db.user.createIndex({ username: 1 }, { unique: true });
db.workspace.createIndex({ name: 1 });
db.task_profile.createIndex({ sort_number: 1 });

// 创建HTTP服务映射集合和索引（数据通过启动时自动导入）
db.http_service_mapping.createIndex({ service_name: 1 }, { unique: true });

// 创建Subfinder数据源配置集合和索引
db.subfinder_provider.createIndex({ provider: 1 }, { unique: true });

// 初始化Subfinder数据源配置（默认禁用，需要用户配置API密钥后启用）
var now = new Date();
var subfinderProviders = [
    { provider: "binaryedge", keys: [], status: "disable", description: "BinaryEdge网络安全数据平台", create_time: now, update_time: now },
    { provider: "bufferover", keys: [], status: "disable", description: "BufferOver DNS数据", create_time: now, update_time: now },
    { provider: "c99", keys: [], status: "disable", description: "C99子域名枚举", create_time: now, update_time: now },
    { provider: "censys", keys: [], status: "disable", description: "Censys互联网搜索引擎 (格式: API_ID:API_SECRET)", create_time: now, update_time: now },
    { provider: "certspotter", keys: [], status: "disable", description: "证书透明度日志监控", create_time: now, update_time: now },
    { provider: "chaos", keys: [], status: "disable", description: "ProjectDiscovery Chaos数据", create_time: now, update_time: now },
    { provider: "fofa", keys: [], status: "disable", description: "FOFA网络空间搜索引擎", create_time: now, update_time: now },
    { provider: "fullhunt", keys: [], status: "disable", description: "FullHunt攻击面管理", create_time: now, update_time: now },
    { provider: "github", keys: [], status: "disable", description: "GitHub代码搜索 (Personal Access Token)", create_time: now, update_time: now },
    { provider: "hunter", keys: [], status: "disable", description: "鹰图平台", create_time: now, update_time: now },
    { provider: "intelx", keys: [], status: "disable", description: "Intelligence X", create_time: now, update_time: now },
    { provider: "passivetotal", keys: [], status: "disable", description: "RiskIQ PassiveTotal (格式: email:key)", create_time: now, update_time: now },
    { provider: "quake", keys: [], status: "disable", description: "360 Quake", create_time: now, update_time: now },
    { provider: "securitytrails", keys: [], status: "disable", description: "SecurityTrails DNS历史", create_time: now, update_time: now },
    { provider: "shodan", keys: [], status: "disable", description: "Shodan搜索引擎", create_time: now, update_time: now },
    { provider: "threatbook", keys: [], status: "disable", description: "微步在线", create_time: now, update_time: now },
    { provider: "virustotal", keys: [], status: "disable", description: "VirusTotal", create_time: now, update_time: now },
    { provider: "zoomeye", keys: [], status: "disable", description: "ZoomEye网络空间搜索 (格式: username:password 或 API Key)", create_time: now, update_time: now }
];

db.subfinder_provider.insertMany(subfinderProviders);

print("CSCAN MongoDB初始化完成");
print("注意: HTTP服务映射数据将在API服务启动时从 poc/custom-http 目录自动导入");
