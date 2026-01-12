import request from './request'

// 获取支持的通知提供者列表
export function getNotifyProviders() {
  return request.post('/notify/providers', {})
}

// 获取通知配置列表
export function getNotifyConfigList() {
  return request.post('/notify/config/list', {})
}

// 保存通知配置
export function saveNotifyConfig(data) {
  return request.post('/notify/config/save', data)
}

// 删除通知配置
export function deleteNotifyConfig(id) {
  return request.post('/notify/config/delete', { id })
}

// 测试通知配置
export function testNotifyConfig(data) {
  return request.post('/notify/config/test', data)
}

// 获取指纹列表（用于高危指纹选择）
export function getFingerprintList(params = {}) {
  return request.post('/fingerprint/list', params)
}

// 获取POC严重级别选项
export function getPocSeverityOptions() {
  return [
    { label: '严重 (Critical)', value: 'critical' },
    { label: '高危 (High)', value: 'high' },
    { label: '中危 (Medium)', value: 'medium' },
    { label: '低危 (Low)', value: 'low' }
  ]
}
