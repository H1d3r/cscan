import request from './request'

// 获取指纹列表
export function getFingerprintList(data) {
  return request.post('/fingerprint/list', data)
}

// 保存指纹
export function saveFingerprint(data) {
  return request.post('/fingerprint/save', data)
}

// 删除指纹
export function deleteFingerprint(data) {
  return request.post('/fingerprint/delete', data)
}

// 获取指纹分类和统计
export function getFingerprintCategories() {
  return request.post('/fingerprint/categories')
}

// 同步指纹
export function syncFingerprints(data = {}) {
  return request.post('/fingerprint/sync', data)
}

// 更新指纹启用状态
export function updateFingerprintEnabled(data) {
  return request.post('/fingerprint/updateEnabled', data)
}

// 批量更新指纹启用状态
export function batchUpdateFingerprintEnabled(data) {
  return request.post('/fingerprint/batchUpdateEnabled', data)
}

// 导入指纹
export function importFingerprints(data) {
  return request.post('/fingerprint/import', data)
}

// 清空自定义指纹
export function clearCustomFingerprints(data = {}) {
  return request.post('/fingerprint/clearCustom', data)
}

// 验证指纹
export function validateFingerprint(data) {
  return request.post('/fingerprint/validate', data)
}

// 批量验证指纹
export function batchValidateFingerprints(data) {
  return request.post('/fingerprint/batchValidate', data)
}

// 匹配现有资产
export function matchFingerprintAssets(data) {
  return request.post('/fingerprint/matchAssets', data)
}


// ==================== HTTP服务映射 API ====================

// 获取HTTP服务映射列表
export function getHttpServiceMappingList(data = {}) {
  return request.post('/fingerprint/httpservice/list', data)
}

// 保存HTTP服务映射
export function saveHttpServiceMapping(data) {
  return request.post('/fingerprint/httpservice/save', data)
}

// 删除HTTP服务映射
export function deleteHttpServiceMapping(data) {
  return request.post('/fingerprint/httpservice/delete', data)
}

// 获取HTTP端口配置
export function getHttpServiceConfig() {
  return request.get('/httpservice/config')
}

// 保存HTTP端口配置
export function saveHttpServiceConfig(data) {
  return request.post('/httpservice/config', data)
}


// ==================== 主动扫描指纹 API ====================

// 获取主动指纹列表
export function getActiveFingerprintList(data = {}) {
  return request.post('/fingerprint/active/list', data)
}

// 保存主动指纹
export function saveActiveFingerprint(data) {
  return request.post('/fingerprint/active/save', data)
}

// 删除主动指纹
export function deleteActiveFingerprint(data) {
  return request.post('/fingerprint/active/delete', data)
}

// 导入主动指纹（YAML格式）
export function importActiveFingerprints(data) {
  return request.post('/fingerprint/active/import', data)
}

// 导出主动指纹（YAML格式）
export function exportActiveFingerprints() {
  return request.post('/fingerprint/active/export')
}

// 清空主动指纹
export function clearActiveFingerprints() {
  return request.post('/fingerprint/active/clear')
}

// 验证主动指纹
export function validateActiveFingerprint(data) {
  return request.post('/fingerprint/active/validate', data)
}
