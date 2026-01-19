import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
import enUs from 'element-plus/dist/locale/en.mjs'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import 'element-plus/dist/index.css'
// 引入 Element Plus 官方暗黑模式样式
import 'element-plus/theme-chalk/dark/css-vars.css'

import App from './App.vue'
import router from './router'
import { setupI18n, i18n } from './i18n'

import './styles/index.css'

// 性能监控（仅开发环境）
import { enablePerformanceMonitoring, setupRouterPerformance } from './utils/performance'

const app = createApp(App)
const pinia = createPinia()

// 注册所有图标
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

app.use(pinia)
app.use(router)

// 设置国际化
setupI18n(app)

// 根据当前语言设置 Element Plus 语言
const currentLocale = i18n.global.locale.value
const elementLocale = currentLocale === 'zh-CN' ? zhCn : enUs
app.use(ElementPlus, { locale: elementLocale })

// 监听语言变化，更新 Element Plus 语言
import { watch } from 'vue'
watch(() => i18n.global.locale.value, (newLocale) => {
  // 这里可以动态更新 Element Plus 的语言，但需要重新创建应用实例
  // 简单的方式是刷新页面或者在组件中处理
})

// 初始化主题
import { useThemeStore } from './stores/theme'
const themeStore = useThemeStore()
themeStore.initTheme()
themeStore.watchSystemTheme()

// 初始化工作空间状态
import { useWorkspaceStore } from './stores/workspace'
const workspaceStore = useWorkspaceStore()
workspaceStore.initialize()

// 启用性能监控（仅开发环境）
if (import.meta.env.DEV) {
  enablePerformanceMonitoring()
  setupRouterPerformance(router)
}

app.mount('#app')

