import request from './request'

// 获取弱口令字典列表
export function getWeakpassDictList(params) {
  return request.post('/weakpass/dict/list', params)
}

// 获取启用的弱口令字典列表
export function getWeakpassDictEnabledList() {
  return request.post('/weakpass/dict/enabled')
}

// 获取指定字典
export function getWeakpassDict(id) {
  return request.post('/weakpass/dict/detail', { id })
}

// 保存弱口令字典
export function saveWeakpassDict(data) {
  return request.post('/weakpass/dict/save', data)
}

// 删除弱口令字典
export function deleteWeakpassDict(params) {
  return request.post('/weakpass/dict/delete', params)
}

// 清空所有非内置字典
export function clearWeakpassDict() {
  return request.post('/weakpass/dict/clear')
}

// 导入弱口令字典
// params: { content: string, format: 'auto'|'simple'|'grouped', name: string, service: string, mergeSame: boolean }
export function importWeakpassDict(params) {
  return request.post('/weakpass/dict/import', params)
}

// 导出弱口令字典
// params: { ids: string[], format: 'simple'|'grouped'|'merged', name: string }
export function exportWeakpassDict(params) {
  return request.post('/weakpass/dict/export', params)
}

// 解析弱口令字典（预览）
// params: { content: string }
export function parseWeakpassDict(params) {
  return request.post('/weakpass/dict/parse', params)
}

// 获取服务类型统计
export function getWeakpassDictStats() {
  return request.post('/weakpass/dict/stats')
}