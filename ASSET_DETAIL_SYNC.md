# 资产详情数据同步方案

## 当前状态
- AssetInventoryTab 和 ScreenshotsTab 都有各自的详情抽屉
- 两者都调用相同的 API：`getAssetHistory` 和 `getAssetExposures`
- 数据源已经统一，但需要确保数据格式一致

## 实现方案

### 方案一：共享组件（已实现）
创建了 `AssetDetailDrawer.vue` 组件，可以被两个Tab共享使用。

优点：
- 代码复用，维护成本低
- 数据格式统一
- 样式一致

缺点：
- 需要修改现有组件，改动较大

### 方案二：统一数据格式（推荐）
保留各自的详情抽屉，但确保：
1. 使用相同的 API 调用
2. 使用相同的数据转换逻辑
3. 使用相同的辅助函数

优点：
- 改动最小
- 各组件独立性强
- 数据已经统一

## 数据格式规范

### 资产基本信息
```javascript
{
  id: String,
  host: String,
  port: Number,
  status: String,  // 必须是字符串类型
  ip: String,
  url: String,
  screenshot: String,
  title: String,
  cname: String,
  technologies: Array<String>,
  labels: Array<String>,
  iconHash: String,
  iconHashBytes: String,
  httpHeader: String,
  httpBody: String,
  banner: String,
  asn: String,
  service: String,
  workspaceId: String
}
```

### 变更记录
```javascript
{
  time: String,  // 格式化后的时间
  taskId: String,
  changes: Array<{
    field: String,
    oldValue: String,
    newValue: String
  }>
}
```

### 目录扫描结果
```javascript
{
  url: String,
  path: String,
  status: String,  // 必须是字符串类型
  contentLength: Number,
  responseTime: Number,
  title: String
}
```

### 漏洞扫描结果
```javascript
{
  id: String,
  name: String,
  severity: String,
  description: String,
  cvss: Number,
  cve: String,
  matchedUrl: String,
  discoveredAt: String
}
```

## 关键函数

### 状态类型转换
```javascript
const getStatusType = (status) => {
  const statusStr = String(status || '')
  if (statusStr.startsWith('2')) return 'success'
  if (statusStr.startsWith('3')) return 'warning'
  if (statusStr.startsWith('4') || statusStr.startsWith('5')) return 'danger'
  return 'info'
}
```

### 字段名翻译
```javascript
const translateFieldName = (field) => {
  const fieldMap = {
    'title': t('asset.field.title'),
    'service': t('asset.field.service'),
    'httpStatus': t('asset.field.httpStatus'),
    'app': t('asset.field.app'),
    'iconHash': t('asset.field.iconHash'),
    'server': t('asset.field.server'),
    'banner': t('asset.field.banner')
  }
  return fieldMap[field] || field
}
```

### 日期格式化
```javascript
const formatDateTime = (dateStr) => {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}
```

## 检查清单

### AssetInventoryTab.vue
- [x] 使用 `getAssetHistory` API
- [x] 使用 `getAssetExposures` API
- [x] status 字段转换为字符串
- [x] 使用统一的 `getStatusType` 函数
- [x] 使用统一的 `translateFieldName` 函数
- [x] 使用统一的 `formatDateTime` 函数

### ScreenshotsTab.vue
- [ ] 检查是否使用 `getAssetHistory` API
- [ ] 检查是否使用 `getAssetExposures` API
- [ ] 检查 status 字段是否转换为字符串
- [ ] 检查是否使用统一的 `getStatusType` 函数
- [ ] 检查是否有详情抽屉实现
- [ ] 如果有，确保数据格式与 AssetInventoryTab 一致

## 下一步行动

1. 检查 ScreenshotsTab 的详情抽屉实现
2. 确保两个组件使用相同的数据转换逻辑
3. 测试数据同步性
