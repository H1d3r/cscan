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

// 自定义POC筛选维度与数量统计
export function getCustomPocCategories(data = {}) {
  return request.post('/poc/custom/categories', data)
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

export function getNucleiTemplateCategories(data = {}) {
  return request.post('/poc/nuclei/categories', data)
}


// 同步Nuclei模板
export function syncNucleiTemplates(data = {}) {
  return request.post('/poc/nuclei/sync', data, { timeout: 600000 })
}

// 清空Nuclei模板
export function clearNucleiTemplates() {
  return request.post('/poc/nuclei/clear')
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


// 验证POC语法
export function validatePocSyntax(data) {
  return request.post('/poc/custom/validateSyntax', data)
}
