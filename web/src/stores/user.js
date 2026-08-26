import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login as loginApi, getUserList, getUserProfile, syncRoleMenus } from '@/api/auth'

// 默认头像路径
export const DEFAULT_AVATAR = '/default-avatar.jpg'

export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('token') || '')
  const userId = ref(localStorage.getItem('userId') || '')
  const username = ref(localStorage.getItem('username') || '')
  const role = ref(localStorage.getItem('role') || '')
  const avatar = ref(localStorage.getItem('avatar') || '')
  const menuPaths = ref(JSON.parse(localStorage.getItem('menuPaths') || '[]'))
  const profile = ref({
    email: '',
    phone: '',
    status: '',
    lastLoginTime: 0,
    createTime: 0
  })

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => role.value === 'admin' || role.value === 'superadmin')
  const avatarSrc = computed(() => avatar.value || DEFAULT_AVATAR)

  let profileRequestVersion = 0

  async function login(loginForm) {
    const res = await loginApi(loginForm)
    if (res.code === 0) {
      if (typeof res.token !== 'string' || !res.token.trim() ||
        typeof res.userId !== 'string' || !res.userId.trim() ||
        typeof res.username !== 'string' || !res.username.trim() ||
        typeof res.role !== 'string' || !res.role.trim()) {
        throw new Error('登录响应缺少有效认证信息')
      }
      token.value = res.token
      userId.value = res.userId
      username.value = res.username
      role.value = res.role

      localStorage.setItem('token', res.token)
      localStorage.setItem('userId', res.userId)
      localStorage.setItem('username', res.username)
      localStorage.setItem('role', res.role)
      if (res.menuPaths && Array.isArray(res.menuPaths)) {
        menuPaths.value = res.menuPaths
        localStorage.setItem('menuPaths', JSON.stringify(res.menuPaths))
      }

      await refreshProfile()
    }
    return res
  }

  // refreshProfile 拉取当前用户个人信息（含头像、邮箱、电话、登录时间）
  async function refreshProfile() {
    const requestVersion = ++profileRequestVersion
    const requestToken = token.value
    if (!requestToken) return
    try {
      const res = await getUserProfile()
      if (requestVersion !== profileRequestVersion || token.value !== requestToken || !token.value) return
      if (res.code === 0) {
        setAvatar(res.avatar || '')
        setUsername(res.username || username.value)
        if (res.role) {
          role.value = res.role
          localStorage.setItem('role', res.role)
        }
        profile.value = {
          email: res.email || '',
          phone: res.phone || '',
          status: res.status || '',
          lastLoginTime: res.lastLoginTime || 0,
          createTime: res.createTime || 0
        }
      }
    } catch (e) {
      // ignore
    }
  }

  // 旧入口保留：仅刷新头像（向后兼容 UserManagement.vue 等调用点）
  async function refreshAvatar() {
    return refreshProfile()
  }

  function setAvatar(url) {
    avatar.value = url || ''
    if (url) {
      localStorage.setItem('avatar', url)
    } else {
      localStorage.removeItem('avatar')
    }
  }

  function setUsername(name) {
    if (!name) return
    username.value = name
    localStorage.setItem('username', name)
  }

  function setProfile(partial) {
    profile.value = { ...profile.value, ...partial }
  }

  function logout() {
    profileRequestVersion++
    token.value = ''
    userId.value = ''
    username.value = ''
    role.value = ''
    avatar.value = ''
    menuPaths.value = []
    profile.value = { email: '', phone: '', status: '', lastLoginTime: 0, createTime: 0 }

    localStorage.removeItem('token')
    localStorage.removeItem('userId')
    localStorage.removeItem('username')
    localStorage.removeItem('role')
    localStorage.removeItem('avatar')
    localStorage.removeItem('menuPaths')
  }

  function setMenuPaths(paths) {
    menuPaths.value = paths || []
    localStorage.setItem('menuPaths', JSON.stringify(menuPaths.value))
  }

  return {
    token,
    userId,
    username,
    role,
    avatar,
    menuPaths,
    avatarSrc,
    profile,
    isLoggedIn,
    isAdmin,
    login,
    logout,
    setAvatar,
    setUsername,
    setProfile,
    setMenuPaths,
    refreshProfile,
    refreshAvatar
  }
})
