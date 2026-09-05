import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login as loginApi, getUserList, getUserProfile, syncRoleMenus } from '@/api/auth'

// 默认头像路径
export const DEFAULT_AVATAR = '/default-avatar.jpg'

const ATTACK_SURFACE_PATH = '/asset-management/space-search'
const LEGACY_ATTACK_SURFACE_PATHS = [
  '/asset-management/exposure/dir',
  '/asset-management/exposure/js',
  '/asset-management/risk/sensitive-info',
  '/asset-management/risk/vuln'
]

export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('token') || '')
  const userId = ref(localStorage.getItem('userId') || '')
  const username = ref(localStorage.getItem('username') || '')
  const role = ref(localStorage.getItem('role') || '')
  const avatar = ref(localStorage.getItem('avatar') || '')
  const menuPaths = ref(JSON.parse(localStorage.getItem('menuPaths') || '[]'))
  // 系统全部受管控路径：用于区分「无权访问」与「不纳入权限管控」（如个人中心）
  const managedPaths = ref(JSON.parse(localStorage.getItem('managedPaths') || '[]'))
  // 管理员标记由后端按角色的 isSuperadmin 下发，自定义角色也可被授予管理员权限
  const adminFlag = ref(localStorage.getItem('isAdmin') === '1')
  const profile = ref({
    email: '',
    phone: '',
    status: '',
    lastLoginTime: 0,
    createTime: 0
  })

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => adminFlag.value || role.value === 'admin' || role.value === 'superadmin')
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
      setManagedPaths(res.allPaths)
      setAdminFlag(res.isAdmin === true)

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
          const roleChanged = res.role !== role.value
          role.value = res.role
          localStorage.setItem('role', res.role)
          // 管理员在后台调整了该用户的角色：立即重新拉取菜单权限
          if (roleChanged) syncMenus()
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
    managedPaths.value = []
    adminFlag.value = false
    profile.value = { email: '', phone: '', status: '', lastLoginTime: 0, createTime: 0 }

    localStorage.removeItem('token')
    localStorage.removeItem('userId')
    localStorage.removeItem('username')
    localStorage.removeItem('role')
    localStorage.removeItem('avatar')
    localStorage.removeItem('menuPaths')
    localStorage.removeItem('managedPaths')
    localStorage.removeItem('isAdmin')
  }

  function setMenuPaths(paths) {
    menuPaths.value = paths || []
    localStorage.setItem('menuPaths', JSON.stringify(menuPaths.value))
  }

  function setAdminFlag(flag) {
    adminFlag.value = !!flag
    localStorage.setItem('isAdmin', adminFlag.value ? '1' : '0')
  }

  function setManagedPaths(paths) {
    if (!Array.isArray(paths)) return
    managedPaths.value = paths
    localStorage.setItem('managedPaths', JSON.stringify(paths))
  }

  // canAccess 判定路由是否可访问。不在受管控清单内的路由（如个人中心）恒放行。
  function canAccess(routePath) {
    if (!routePath) return true
    if (!managedPaths.value.includes(routePath)) return true
    if (!menuPaths.value || menuPaths.value.length === 0) return true
    if (routePath === ATTACK_SURFACE_PATH) {
      return menuPaths.value.includes(ATTACK_SURFACE_PATH) ||
        LEGACY_ATTACK_SURFACE_PATHS.some(path => menuPaths.value.includes(path))
    }
    return menuPaths.value.includes(routePath)
  }

  // syncMenus 重新拉取当前角色的菜单权限与管理员标记（角色配置变更后调用）
  async function syncMenus() {
    if (!token.value) return
    try {
      const res = await syncRoleMenus()
      if (res && res.code === 0) {
        setMenuPaths(res.menuPaths || [])
        setManagedPaths(res.allPaths)
        setAdminFlag(res.isAdmin === true)
      }
    } catch (e) {
      // 同步失败保留本地缓存的权限，避免菜单闪空
    }
  }

  return {
    token,
    userId,
    username,
    role,
    avatar,
    menuPaths,
    managedPaths,
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
    setAdminFlag,
    setManagedPaths,
    canAccess,
    syncMenus,
    refreshProfile,
    refreshAvatar
  }
})
