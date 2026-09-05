/**
 * 侧边栏菜单定义。
 * 同时作为角色菜单权限配置的数据源，保证「可配置项」与「实际菜单」严格一致。
 * index 即路由路径，需与后端 model.MenuPathList() 保持同步。
 */
import {
  Setting, Monitor, List, Search, Aim, Odometer, Stamp, Connection,
  Key, OfficeBuilding, Bell, User, UserFilled, Document,
  CircleClose, Warning, Timer, Picture, MagicStick, Operation
} from '@element-plus/icons-vue'

// buildMenu 构造菜单树，t 为 vue-i18n 的翻译函数（在 computed 中调用以响应语言切换）
export function buildMenu(t) {
  return [
    { type: 'item', index: '/dashboard', icon: Odometer, label: t('navigation.dashboard') },
    { type: 'item', index: '/asset-management/space-search', icon: Monitor, label: t('navigation.assetManagement') },
    { type: 'divider' },
    { type: 'item', index: '/task', icon: List, label: t('navigation.taskManagement') },
    { type: 'submenu', index: 'space-engine-menu', icon: Connection, label: t('navigation.spaceEngine'), items: [
      { index: '/space-engine/online-search', icon: Search, label: t('navigation.onlineSearch') },
      { index: '/space-engine/api-config', icon: Key, label: t('navigation.spaceEngineApiConfig') },
      { index: '/space-engine/cron-task', icon: Timer, label: t('navigation.spaceEngineCronTask') },
    ]},
    { type: 'submenu', index: 'scan-config-menu', icon: Operation, label: t('navigation.scanConfig'), items: [
      { index: '/cron-task', icon: Timer, label: t('navigation.cronTask') },
      { index: '/settings-subfinder', icon: Search, label: t('navigation.subdomainConfig') },
      { index: '/poc', icon: Aim, label: t('navigation.pocManagement') },
      { index: '/fingerprint', icon: Stamp, label: t('navigation.fingerprintManagement') },
      { index: '/blacklist', icon: CircleClose, label: t('navigation.blacklist') },
    ]},
    { type: 'divider' },
    { type: 'item', index: '/ai-config', icon: MagicStick, label: t('navigation.aiConfig'), adminOnly: true },
    { type: 'item', index: '/worker', icon: Connection, label: t('navigation.workerNodes') },
    { type: 'item', index: '/worker-logs', icon: Document, label: t('navigation.workerLogs') },
    { type: 'submenu', index: 'advanced-config-menu', icon: Operation, label: t('navigation.advancedConfig'), items: [
      { index: '/settings-notify', icon: Bell, label: t('navigation.notifyConfig') },
      { index: '/high-risk-filter', icon: Warning, label: t('navigation.highRiskFilter') },
    ]},
    { type: 'submenu', index: 'system-management', icon: Setting, label: t('navigation.systemManagement'), items: [
      { index: '/user', icon: User, label: t('navigation.userManagement'), adminOnly: true },
      { index: '/registration-config', icon: UserFilled, label: t('settings.registration.title'), adminOnly: true },
      { index: '/organization', icon: OfficeBuilding, label: t('navigation.organizationManagement'), adminOnly: true },
      { index: '/settings-branding', icon: Picture, label: t('navigation.brandingConfig'), adminOnly: true },
      { index: '/settings-role', icon: User, label: t('navigation.roleManagement'), adminOnly: true },
    ]},
  ]
}

/**
 * buildMenuGroups 将菜单树摊平为「分组 → 菜单项」两级结构，供角色权限配置的树形选择使用。
 * allowedPaths 为后端返回的可配置路径白名单，用于过滤掉菜单里尚未纳管的路由。
 */
export function buildMenuGroups(t, allowedPaths) {
  const allowed = Array.isArray(allowedPaths) && allowedPaths.length > 0 ? new Set(allowedPaths) : null
  const pick = path => !allowed || allowed.has(path)

  const groups = []
  let standalone = null

  for (const group of buildMenu(t)) {
    if (group.type === 'divider') continue
    if (group.type === 'item') {
      if (!pick(group.index)) continue
      if (!standalone) {
        standalone = { key: 'standalone', label: t('roleManagement.generalMenus'), children: [] }
        groups.push(standalone)
      }
      standalone.children.push({ path: group.index, label: group.label })
      continue
    }
    const children = group.items.filter(item => pick(item.index)).map(item => ({ path: item.index, label: item.label }))
    if (children.length > 0) {
      groups.push({ key: group.index, label: group.label, children })
    }
  }

  // 详情/表单等非菜单路由（如 /task/create）也需要授权，否则列表页可见但无法进入详情
  const extraLabelKeys = {
    '/task/create': 'menu.TaskCreate',
    '/task/edit/:id': 'menu.TaskEdit',
    '/task/detail': 'menu.TaskDetail',
    '/task/template': 'menu.ScanTemplate',
    '/report': 'menu.Report',
    '/cron-task/create': 'menu.CronTaskCreate',
    '/cron-task/edit/:id': 'menu.CronTaskEdit'
  }
  const grouped = new Set(groups.flatMap(g => g.children.map(c => c.path)))
  const extras = (allowedPaths || []).filter(p => !grouped.has(p))
  if (extras.length > 0) {
    groups.push({
      key: 'extra',
      label: t('roleManagement.subPageMenus'),
      children: extras.map(path => ({
        path,
        label: extraLabelKeys[path] ? t(extraLabelKeys[path]) : path
      }))
    })
  }

  return groups
}
