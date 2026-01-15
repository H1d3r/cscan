import { createRouter, createWebHistory } from 'vue-router'
import { useUserStore } from '@/stores/user'

// 动态导入重试包装器，解决 chunk 加载失败问题
function lazyLoad(importFn) {
  return () => {
    return importFn().catch((err) => {
      // 如果是 chunk 加载失败，尝试刷新页面
      if (err.message.includes('Failed to fetch dynamically imported module') ||
          err.message.includes('Loading chunk') ||
          err.message.includes('Loading CSS chunk')) {
        console.warn('[Router] Chunk load failed, reloading page...', err)
        window.location.reload()
        return new Promise(() => {}) // 阻止后续执行
      }
      throw err
    })
  }
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
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: lazyLoad(() => import('@/views/Dashboard.vue')),
        meta: { title: '工作台', icon: 'Odometer' }
      },
      {
        path: 'asset-management',
        name: 'AssetManagement',
        component: lazyLoad(() => import('@/views/AssetManagement.vue')),
        meta: { title: '资产管理', icon: 'DataAnalysis' }
      },
      {
        path: 'site',
        name: 'Site',
        component: lazyLoad(() => import('@/views/Site.vue')),
        meta: { title: '站点管理', icon: 'Monitor', hidden: true }
      },
      {
        path: 'domain',
        name: 'Domain',
        component: lazyLoad(() => import('@/views/Domain.vue')),
        meta: { title: '域名管理', icon: 'Link', hidden: true }
      },
      {
        path: 'ip',
        name: 'IP',
        component: lazyLoad(() => import('@/views/IP.vue')),
        meta: { title: 'IP管理', icon: 'Position', hidden: true }
      },
      {
        path: 'asset',
        name: 'Asset',
        component: lazyLoad(() => import('@/views/Asset.vue')),
        meta: { title: '资产管理', icon: 'Monitor', hidden: true }
      },
      {
        path: 'task/create',
        name: 'TaskCreate',
        component: lazyLoad(() => import('@/views/TaskCreate.vue')),
        meta: { title: '新建任务', icon: 'List', hidden: true }
      },
      {
        path: 'task/edit/:id',
        name: 'TaskEdit',
        component: lazyLoad(() => import('@/views/TaskCreate.vue')),
        meta: { title: '编辑任务', icon: 'List', hidden: true }
      },
      {
        path: 'task',
        name: 'Task',
        component: lazyLoad(() => import('@/views/Task.vue')),
        meta: { title: '任务管理', icon: 'List' }
      },
      {
        path: 'task/template',
        name: 'ScanTemplate',
        component: lazyLoad(() => import('@/views/ScanTemplate.vue')),
        meta: { title: '扫描模板', icon: 'Document', hidden: true }
      },
      {
        path: 'cron-task',
        name: 'CronTask',
        component: lazyLoad(() => import('@/views/CronTask.vue')),
        meta: { title: '定时扫描', icon: 'Timer' }
      },
      {
        path: 'vul',
        name: 'Vul',
        component: lazyLoad(() => import('@/views/Vul.vue')),
        meta: { title: '漏洞管理', icon: 'Warning', hidden: true }
      },
      {
        path: 'online-search',
        name: 'OnlineSearch',
        component: lazyLoad(() => import('@/views/OnlineSearch.vue')),
        meta: { title: '在线搜索', icon: 'Search' }
      },
      {
        path: 'workspace',
        name: 'Workspace',
        redirect: '/settings?tab=workspace',
        meta: { title: '工作空间', icon: 'Folder', hidden: true }
      },
      {
        path: 'worker',
        name: 'Worker',
        component: lazyLoad(() => import('@/views/Worker.vue')),
        meta: { title: 'Worker节点', icon: 'Connection' }
      },
      {
        path: 'worker-logs',
        name: 'WorkerLogs',
        component: lazyLoad(() => import('@/views/WorkerLogs.vue')),
        meta: { title: '运行日志', icon: 'Document' }
      },
      {
        path: 'blacklist',
        name: 'Blacklist',
        component: lazyLoad(() => import('@/views/Blacklist.vue')),
        meta: { title: '全局黑名单', icon: 'CircleClose' }
      },
      {
        path: 'high-risk-filter',
        name: 'HighRiskFilter',
        component: lazyLoad(() => import('@/views/HighRiskFilter.vue')),
        meta: { title: '高危过滤配置', icon: 'Warning' }
      },
      {
        path: 'worker/console/:name',
        name: 'WorkerConsole',
        component: lazyLoad(() => import('@/views/WorkerConsole.vue')),
        meta: { title: 'Worker控制台', icon: 'Monitor', hidden: true }
      },
      {
        path: 'poc',
        name: 'Poc',
        component: lazyLoad(() => import('@/views/Poc.vue')),
        meta: { title: 'POC管理', icon: 'Aim' }
      },
      {
        path: 'fingerprint',
        name: 'Fingerprint',
        component: lazyLoad(() => import('@/views/Fingerprint.vue')),
        meta: { title: '指纹管理', icon: 'Stamp' }
      },
      {
        path: 'report',
        name: 'Report',
        component: lazyLoad(() => import('@/views/Report.vue')),
        meta: { title: '扫描报告', icon: 'Document', hidden: true }
      },
      {
        path: 'user',
        name: 'User',
        redirect: '/settings?tab=user',
        meta: { title: '用户管理', icon: 'User', roles: ['superadmin'], hidden: true }
      },
      {
        path: 'organization',
        name: 'Organization',
        redirect: '/settings?tab=organization',
        meta: { title: '组织管理', icon: 'OfficeBuilding', hidden: true }
      },
      {
        path: 'settings',
        name: 'Settings',
        component: lazyLoad(() => import('@/views/Settings.vue')),
        meta: { title: '系统配置', icon: 'Setting' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach((to, from, next) => {
  const userStore = useUserStore()
  
  if (to.meta.requiresAuth !== false && !userStore.token) {
    next('/login')
  } else if (to.path === '/login' && userStore.token) {
    next('/dashboard')
  } else {
    next()
  }
})

export default router
