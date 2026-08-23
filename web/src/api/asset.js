import request from '@/api/request'

// 获取资产清单
export function getAssetInventory(data) {
  return request({
    url: '/asset/inventory',
    method: 'post',
    data
  })
}

// 获取资产详情（按需加载完整资产，含 body/header/banner 等大字段）
export function getAssetDetail(data) {
  return request({
    url: '/asset/detail',
    method: 'post',
    data
  })
}

// 获取截图清单
export function getScreenshots(data) {
  return request({
    url: '/asset/screenshots',
    method: 'post',
    data
  })
}

// 获取资产统计
export function getAssetStat() {
  return request({
    url: '/asset/stat',
    method: 'post'
  })
}

// 删除资产
export function deleteAsset(data) {
  return request({
    url: '/asset/delete',
    method: 'post',
    data
  })
}

// 清空资产
export function clearAssets() {
  return request({
    url: '/asset/clear',
    method: 'post'
  })
}

// 清空域名
export function clearDomains() {
  return request({
    url: '/asset/domain/clear',
    method: 'post'
  })
}

// 清空 IP
export function clearIPs() {
  return request({
    url: '/asset/ip/clear',
    method: 'post'
  })
}

// 清空站点
export function clearSites() {
  return request({
    url: '/asset/site/clear',
    method: 'post'
  })
}

// 清空端口
export function clearPorts() {
  return request({
    url: '/asset/port/clear',
    method: 'post'
  })
}

// 清空截图
export function clearScreenshots() {
  return request({
    url: '/asset/screenshots/clear',
    method: 'post'
  })
}

// 获取资产历史
export function getAssetHistory(data) {
  return request({
    url: '/assets/history',
    method: 'post',
    data
  })
}

// 资产变更时间线数据源（已合并到 V2 /assets/history，返回 versions + list）
export function getAssetChangeHistory(data) {
  return request({
    url: '/assets/history',
    method: 'post',
    data
  })
}

// 导入资产
export function importAssets(data) {
  return request({
    url: '/asset/import',
    method: 'post',
    data
  })
}

// 更新资产标签
export function updateAssetLabels(data) {
  return request({
    url: '/asset/updateLabels',
    method: 'post',
    data
  })
}

// 获取资产过滤器选项（技术栈、端口、状态码）
export function getAssetFilterOptions(data) {
  return request({
    url: '/asset/filterOptions',
    method: 'post',
    data
  })
}

// 获取子域名列表（目标详情的子域名 Tab 使用，支持 rootDomain 预过滤）
export function getDomainList(data) {
  return request({
    url: '/asset/domain/list',
    method: 'post',
    data
  })
}

// 获取资产暴露面（目录扫描和漏洞扫描结果）
export function getAssetExposures(data) {
  return request({
    url: '/asset/exposures',
    method: 'post',
    data
  })
}

// T1.3：批量更新漏洞生命周期状态（open / fixed / ignored）
export function updateVulStatus(data) {
  return request({
    url: '/vul/updateStatus',
    method: 'post',
    data
  })
}

/**
 * 顶层资产 (target) API — Phase 4
 * 资产 = 主机 IP 或主域名，以 "{type}:{value}" 编码为 targetId
 */

// 顶层资产分页列表（含 exposure/risk 气泡字段）
export function getAssetTargetList(data) {
  return request({
    url: '/asset/target/list',
    method: 'post',
    data
  })
}

// 获取顶层资产详情（meta + exposure + risk 统计）
export function getAssetTargetDetail(data) {
  return request({
    url: '/asset/target/detail',
    method: 'post',
    data
  })
}

// 获取目标下的资产列表（服务级明细）
export function getAssetTargetAssets(data) {
  return request({
    url: '/asset/target/assets',
    method: 'post',
    data
  })
}

// 按资产 ID 批量获取截图/favicon（列表懒加载，列表响应不再携带大字段）
export function getAssetMedia(data) {
  return request({
    url: '/asset/media',
    method: 'post',
    data
  })
}

// 目标资产按维度聚合（host/port/ip/app/status）
export function getAssetTargetGroups(data) {
  return request({
    url: '/asset/target/groups',
    method: 'post',
    data
  })
}

// 目标关联的 TLS 证书列表
export function getAssetTargetCerts(data) {
  return request({
    url: '/asset/target/certs',
    method: 'post',
    data
  })
}

// 更新顶层资产用户字段（labels/memo/colorTag；memo/colorTag 传空串即清空）
export function updateAssetTarget(data) {
  return request({
    url: '/asset/target/update',
    method: 'post',
    data
  })
}

// 重新发现目标：重放该目标最近一次扫描任务
export function rediscoverAssetTarget(data) {
  return request({
    url: '/asset/target/rediscover',
    method: 'post',
    data
  })
}

// 删除顶层资产（可选级联删除底层 asset + vul）
export function deleteAssetTarget(data) {
  return request({
    url: '/asset/target/delete',
    method: 'post',
    data
  })
}
