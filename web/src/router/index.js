import { createRouter, createWebHistory } from 'vue-router'
import { useUserStore } from '@/stores/user'

// 动态导入重试包装器，解决 chunk 加载失败问题
// 返回纯异步函数（非 defineAsyncComponent），避免 Vue Router 警告
function lazyLoad(importFn) {
  return () => importFn().then((module) => {
    sessionStorage.removeItem('cscan:chunk-reload')
    return module
  }).catch((err) => {
    const message = err instanceof Error ? err.message : String(err)
    const isChunkError = message.includes('Failed to fetch dynamically imported module') ||
      message.includes('Loading chunk') ||
      message.includes('Loading CSS chunk')
    if (!isChunkError) throw err

    const hasRetried = sessionStorage.getItem('cscan:chunk-reload') === '1'
    if (!hasRetried) {
      sessionStorage.setItem('cscan:chunk-reload', '1')
      window.location.reload()
      return new Promise(() => {})
    }

    sessionStorage.removeItem('cscan:chunk-reload')
    throw new Error('页面资源加载失败，请刷新后重试')
  })
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: lazyLoad(() => import('@/views/Login.vue')),
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    component: lazyLoad(() => import('@/layouts/MainLayout.vue')),
    redirect: '/dashboard',
    meta: { requiresAuth: true },
    children: [
      // 本数组与 MainLayout.vue 侧边栏菜单严格同序、一一对应。
      // 分节标题与菜单分组同名；
      // 标注「非菜单」的是仅由页面内 router.push 进入的详情/表单页。

      // ===== 主控台 =====
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: lazyLoad(() => import('@/views/Dashboard.vue')),
        meta: { title: 'menu.Dashboard', icon: 'Odometer' }
      },

      // ===== 攻击面管理 =====
      // 单一入口提供目标视图与带分类 Tab 的全局视图；旧地址重定向到对应 Tab。
      {
        path: 'asset-management',
        redirect: 'asset-management/space-search',
        children: [
          {
            path: 'space-search',
            name: 'AssetSpaceSearch',
            component: lazyLoad(() => import('@/views/AssetSpaceSearch.vue')),
            meta: { title: 'menu.AssetSpaceSearch', icon: 'Monitor' }
          },
          {
            path: 'exposure/dir',
            redirect: to => ({ path: '/asset-management/space-search', query: { ...to.query, view: 'global', tab: 'dir' } })
          },
          {
            path: 'exposure/js',
            redirect: to => ({ path: '/asset-management/space-search', query: { ...to.query, view: 'global', tab: 'js' } })
          },
          {
            path: 'risk/sensitive-info',
            redirect: to => ({ path: '/asset-management/space-search', query: { ...to.query, view: 'global', tab: 'sensitive' } })
          },
          {
            path: 'risk/vuln',
            redirect: to => ({ path: '/asset-management/space-search', query: { ...to.query, view: 'global', tab: 'vuln' } })
          },
        ]
      },

      // ===== 任务管理 =====
      {
        path: 'task',
        name: 'Task',
        component: lazyLoad(() => import('@/views/Task.vue')),
        meta: { title: 'menu.Task', icon: 'List' }
      },
      // 非菜单：由 Task.vue / AssetManagement.vue 内跳转
      {
        path: 'task/create',
        name: 'TaskCreate',
        component: lazyLoad(() => import('@/views/TaskCreate.vue')),
        meta: { title: 'menu.TaskCreate', icon: 'List' }
      },
      {
        path: 'task/edit/:id',
        name: 'TaskEdit',
        component: lazyLoad(() => import('@/views/TaskCreate.vue')),
        meta: { title: 'menu.TaskEdit', icon: 'List' }
      },
      {
        path: 'task/detail',
        name: 'TaskDetail',
        component: lazyLoad(() => import('@/views/TaskDetail.vue')),
        meta: { title: 'menu.TaskDetail', icon: 'List' }
      },
      {
        path: 'task/template',
        name: 'ScanTemplate',
        component: lazyLoad(() => import('@/views/ScanTemplate.vue')),
        meta: { title: 'menu.ScanTemplate', icon: 'Document' }
      },

      // ===== 空间引擎 =====
      {
        path: 'space-engine/online-search',
        name: 'SpaceEngineOnlineSearch',
        component: lazyLoad(() => import('@/views/OnlineSearch.vue')),
        meta: { title: 'menu.SpaceEngineOnlineSearch', icon: 'Search' }
      },
      {
        path: 'space-engine/api-config',
        name: 'SpaceEngineApiConfig',
        component: lazyLoad(() => import('@/views/space-engine/ApiConfig.vue')),
        meta: { title: 'menu.SpaceEngineApiConfig', icon: 'Key' }
      },
      {
        path: 'space-engine/cron-task',
        name: 'SpaceEngineCronTask',
        component: lazyLoad(() => import('@/views/space-engine/CronTask.vue')),
        meta: { title: 'menu.SpaceEngineCronTask', icon: 'Timer' }
      },

      // ===== 扫描配置 =====
      {
        path: 'cron-task',
        name: 'CronTask',
        component: lazyLoad(() => import('@/views/CronTask.vue')),
        meta: { title: 'menu.CronTask', icon: 'Timer' }
      },
      // 非菜单：由 CronTask.vue 内跳转
      {
        path: 'cron-task/create',
        name: 'CronTaskCreate',
        component: lazyLoad(() => import('@/views/CronTaskCreate.vue')),
        meta: { title: 'menu.CronTaskCreate', icon: 'Timer' }
      },
      {
        path: 'cron-task/edit/:id',
        name: 'CronTaskEdit',
        component: lazyLoad(() => import('@/views/CronTaskCreate.vue')),
        meta: { title: 'menu.CronTaskEdit', icon: 'Timer' }
      },
      {
        path: 'settings-subfinder',
        name: 'SubfinderConfig',
        component: lazyLoad(() => import('@/views/settings/SubfinderConfig.vue')),
        meta: { title: 'menu.subdomainConfig', icon: 'Search' }
      },
      {
        path: 'poc',
        name: 'Poc',
        component: lazyLoad(() => import('@/views/Poc.vue')),
        meta: { title: 'menu.Poc', icon: 'Aim' }
      },
      {
        path: 'fingerprint',
        name: 'Fingerprint',
        component: lazyLoad(() => import('@/views/Fingerprint.vue')),
        meta: { title: 'menu.Fingerprint', icon: 'Stamp' }
      },
      {
        path: 'blacklist',
        name: 'Blacklist',
        component: lazyLoad(() => import('@/views/Blacklist.vue')),
        meta: { title: 'menu.Blacklist', icon: 'CircleClose' }
      },

      // ===== AI 配置 / Worker =====
      {
        path: 'ai-config',
        name: 'AIConfig',
        component: lazyLoad(() => import('@/views/AIConfig.vue')),
        meta: { title: 'menu.AIConfig', icon: 'MagicStick', roles: ['admin', 'superadmin'] }
      },
      {
        path: 'worker',
        name: 'Worker',
        component: lazyLoad(() => import('@/views/Worker.vue')),
        meta: { title: 'menu.Worker', icon: 'Connection' }
      },
      {
        path: 'worker-logs',
        name: 'WorkerLogs',
        component: lazyLoad(() => import('@/views/WorkerLogs.vue')),
        meta: { title: 'menu.WorkerLogs', icon: 'Document' }
      },

      // ===== 高级配置 =====
      {
        path: 'settings-notify',
        name: 'NotifyConfig',
        component: lazyLoad(() => import('@/views/settings/NotifyConfig.vue')),
        meta: { title: 'menu.notifyConfig', icon: 'Bell' }
      },
      {
        path: 'high-risk-filter',
        name: 'HighRiskFilter',
        component: lazyLoad(() => import('@/views/HighRiskFilter.vue')),
        meta: { title: 'menu.HighRiskFilter', icon: 'Warning' }
      },

      // ===== 系统管理 =====
      {
        path: 'user',
        name: 'User',
        component: lazyLoad(() => import('@/views/settings/UserManagement.vue')),
        meta: { title: 'menu.User', icon: 'User', roles: ['admin', 'superadmin'] }
      },
      {
        path: 'registration-config',
        name: 'RegistrationConfig',
        component: lazyLoad(() => import('@/views/settings/RegistrationConfig.vue')),
        meta: { title: 'settings.registration.title', icon: 'UserFilled', roles: ['admin', 'superadmin'] }
      },
      {
        path: 'organization',
        name: 'Organization',
        component: lazyLoad(() => import('@/views/settings/OrganizationManagement.vue')),
        meta: { title: 'menu.Organization', icon: 'OfficeBuilding', roles: ['admin', 'superadmin'] }
      },
      {
        path: 'settings-branding',
        name: 'BrandingConfig',
        component: lazyLoad(() => import('@/views/settings/BrandingConfig.vue')),
        meta: { title: 'menu.brandingConfig', icon: 'Picture', roles: ['admin', 'superadmin'] }
      },
      {
        path: 'settings-role',
        name: 'RoleManagement',
        component: lazyLoad(() => import('@/views/settings/RoleManagement.vue')),
        meta: { title: 'menu.RoleManagement', icon: 'User', roles: ['admin', 'superadmin'] }
      },

      // ===== 非菜单页 =====
      // report：由 Task.vue / TaskDetail.vue 的「查看报告」跳转
      {
        path: 'report',
        name: 'Report',
        component: lazyLoad(() => import('@/views/Report.vue')),
        meta: { title: 'menu.Report', icon: 'Document' }
      },
      // profile：由顶栏用户下拉菜单跳转
      {
        path: 'profile',
        name: 'Profile',
        component: lazyLoad(() => import('@/views/Profile.vue')),
        meta: { title: 'menu.Profile', icon: 'User' }
      },
    ]
  },
  // 404 兜底路由：未匹配的路径重定向到 Dashboard
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    redirect: '/dashboard'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach((to, from, next) => {
  const userStore = useUserStore()

  // 路由切换时显示顶部加载进度条（修复 BUG #4：懒加载 chunk 首次加载空白）
  if (to.name !== from.name) {
    document.documentElement.classList.add('app-route-loading')
  }

  if (to.meta.requiresAuth !== false && !userStore.token) {
    next('/login')
  } else if (to.path === '/login' && userStore.token) {
    next('/dashboard')
  } else if (to.meta.roles && !to.meta.roles.includes(userStore.role) && !userStore.isAdmin) {
    // 角色不匹配：拦截直接输入 URL 的越权访问（后端亦有对应校验）
    next('/dashboard')
  } else if (userStore.token && !userStore.canAccess(matchedRoutePath(to))) {
    // 菜单权限不足：拦截直接输入 URL 绕过侧边栏过滤（后端管理接口亦有对应校验）
    next(fallbackRoute(userStore))
  } else {
    next()
  }
})

// matchedRoutePath 取匹配到的最深层路由的路径模式（如 /task/edit/:id），
// 而非带实参的 to.path，以便与角色配置的菜单路径精确比对。
function matchedRoutePath(to) {
  const matched = to.matched
  return matched.length > 0 ? matched[matched.length - 1].path : to.path
}

// fallbackRoute 无权访问时的落点：优先仪表盘，否则取角色第一个有权限的菜单
function fallbackRoute(userStore) {
  if (userStore.canAccess('/dashboard')) return '/dashboard'
  const first = (userStore.menuPaths || []).find(p => !p.includes(':'))
  return first || '/profile'
}

// 路由加载完成后隐藏顶部进度条
router.afterEach(() => {
  setTimeout(() => {
    document.documentElement.classList.remove('app-route-loading')
  }, 300)
})

export default router
