import request from './request'

// ==================== 任务管理 ====================

export function getTaskList(data) {
  return request.post('/task/list', data)
}

export function createTask(data) {
  return request.post('/task/create', data)
}

export function deleteTask(data) {
  return request.post('/task/delete', data)
}

export function batchDeleteTask(data) {
  return request.post('/task/batchDelete', data)
}

export function getTaskProfileList() {
  return request.post('/task/profile/list')
}

export function saveTaskProfile(data) {
  return request.post('/task/profile/save', data)
}

export function deleteTaskProfile(data) {
  return request.post('/task/profile/delete', data)
}

export function retryTask(data) {
  return request.post('/task/retry', data)
}

export function startTask(data) {
  return request.post('/task/start', data)
}

export function pauseTask(data) {
  return request.post('/task/pause', data)
}

export function resumeTask(data) {
  return request.post('/task/resume', data)
}

export function stopTask(data) {
  return request.post('/task/stop', data)
}

export function updateTask(data) {
  return request.post('/task/update', data)
}

export function getTaskDetail(data) {
  return request.post('/task/detail', data)
}

export function getTaskLogs(data) {
  return request.post('/task/logs', data)
}

export function getWorkerList() {
  return request.post('/worker/list')
}

// 用户扫描配置
export function saveScanConfig(data) {
  return request.post('/user/scanConfig/save', data)
}

export function getScanConfig() {
  return request.post('/user/scanConfig/get')
}

// ==================== 扫描配置模板 ====================

// 获取模板列表
export function getScanTemplateList(data) {
  return request.post('/task/template/list', data)
}

// 保存模板
export function saveScanTemplate(data) {
  return request.post('/task/template/save', data)
}

// 删除模板
export function deleteScanTemplate(data) {
  return request.post('/task/template/delete', data)
}

// 获取模板详情
export function getScanTemplateDetail(data) {
  return request.post('/task/template/detail', data)
}

// 从任务创建模板
export function createTemplateFromTask(data) {
  return request.post('/task/template/fromTask', data)
}

// 获取模板分类和标签
export function getScanTemplateCategories() {
  return request.post('/task/template/categories')
}

// 导出模板
export function exportScanTemplates(data) {
  return request.post('/task/template/export', data)
}

// 导入模板
export function importScanTemplates(data) {
  return request.post('/task/template/import', data)
}

// 使用模板（增加使用计数）
export function useScanTemplate(data) {
  return request.post('/task/template/use', data)
}
