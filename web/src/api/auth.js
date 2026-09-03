import request from './request'

export function login(data) {
  return request.post('/login', data)
}

export function register(data) {
  return request.post('/register', data)
}

export function getUserList(data) {
  return request.post('/user/list', data)
}

export function createUser(data) {
  return request.post('/user/create', data)
}

export function updateUser(data) {
  return request.post('/user/update', data)
}

export function deleteUser(data) {
  return request.post('/user/delete', data)
}

export function resetUserPassword(data) {
  return request.post('/user/resetPassword', data)
}

// ==================== 注册配置 ====================
export function getRegistrationConfig() {
  return request.post('/registration/config/get')
}

export function saveRegistrationConfig(data) {
  return request.post('/registration/config/save', data)
}

// ==================== 用户审核 ====================
export function approveUser(data) {
  return request.post('/user/approve', data)
}

// 用户头像上传（multipart）
export function uploadUserAvatar(file) {
  const formData = new FormData()
  formData.append('file', file)
  return request.post('/user/avatar/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
}

// ==================== 个人中心 ====================
export function getUserProfile() {
  return request.post('/user/profile/get')
}

export function updateUserProfile(data) {
  return request.post('/user/profile/update', data)
}

export function changeUserPassword(data) {
  return request.post('/user/password/change', data)
}

// ==================== 个人 API Token ====================
export function createUserToken(data) {
  return request.post('/user/token/create', data)
}

export function listUserTokens() {
  return request.post('/user/token/list')
}

export function setUserTokenStatus(data) {
  return request.post('/user/token/setStatus', data)
}

export function getUserTokenScopes() {
  return request.post('/user/token/scopes')
}

// ==================== 引导式首次体验 (T4.2) ====================
export function getOnboardingStatus() {
  return request.post('/user/onboarding/status')
}

export function completeOnboarding() {
  return request.post('/user/onboarding/complete')
}

// ==================== 角色管理 ====================
export function getRoleList(data) {
  return request.post('/role/list', data)
}

export function getRoleDetail(data) {
  return request.post('/role/detail', data)
}

export function createRole(data) {
  return request.post('/role/create', data)
}

export function updateRole(data) {
  return request.post('/role/update', data)
}

export function deleteRole(data) {
  return request.post('/role/delete', data)
}

export function syncRoleMenus() {
  return request.post('/role/menus/sync')
}

// 系统全部可配置菜单路径（管理员权限）
export function getRoleMenuOptions() {
  return request.post('/role/menus/options')
}
