import request from './request'

// 标签映射
export function getTagMappingList() {
  return request.post('/poc/tagmapping/list')
}

export function saveTagMapping(data) {
  return request.post('/poc/tagmapping/save', data)
}

export function deleteTagMapping(data) {
  return request.post('/poc/tagmapping/delete', data)
}

// 自定义POC
export function getCustomPocList(data) {
  return request.post('/poc/custom/list', data)
}

export function saveCustomPoc(data) {
  return request.post('/poc/custom/save', data)
}

// 批量导入自定义POC
export function batchImportCustomPoc(data) {
  return request.post('/poc/custom/batchImport', data, { timeout: 600000 })
}

export function deleteCustomPoc(data) {
  return request.post('/poc/custom/delete', data)
}

// 清空自定义POC（支持按筛选条件清空）
export function clearAllCustomPoc(data = {}) {
  return request.post('/poc/custom/clearAll', data)
}

// Nuclei默认模板
export function getNucleiTemplateList(data) {
  return request.post('/poc/nuclei/templates', data)
}

export function getNucleiTemplateCategories() {
  return request.post('/poc/nuclei/categories')
}


// 同步Nuclei模板
export function syncNucleiTemplates(data = {}) {
  return request.post('/poc/nuclei/sync', data, { timeout: 600000 })
}

// 下载Nuclei默认模板库
export function downloadNucleiTemplates(data = {}) {
  return request.post('/poc/nuclei/download', data, { timeout: 600000 })
}

// 查询下载状态
export function getDownloadStatus(taskId) {
  return request.get('/poc/nuclei/download/status', { params: { taskId } })
}

// 清空Nuclei模板
export function clearNucleiTemplates() {
  return request.post('/poc/nuclei/clear')
}

// 更新模板启用状态
export function updateTemplateEnabled(data) {
  return request.post('/poc/nuclei/updateEnabled', data)
}

// 获取模板详情
export function getNucleiTemplateDetail(data) {
  return request.post('/poc/nuclei/detail', data)
}

// 验证POC
export function validatePoc(data) {
  return request.post('/poc/custom/validate', data)
}

// 批量验证POC
export function batchValidatePoc(data) {
  return request.post('/poc/batchValidate', data)
}

// 查询POC验证结果
export function getPocValidationResult(data) {
  return request.post('/poc/queryResult', data)
}

// 自定义POC扫描现有资产
export function scanAssetsWithPoc(data) {
  return request.post('/poc/custom/scanAssets', data)
}


// AI配置
export function getAIConfig() {
  return request.post('/ai/config/get')
}

export function saveAIConfig(data) {
  return request.post('/ai/config/save', data)
}


// 验证POC语法
export function validatePocSyntax(data) {
  return request.post('/poc/custom/validateSyntax', data)
}
