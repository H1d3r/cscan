import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import request from '@/api/request'

// 可用的颜色主题列表（支持明暗两种模式）
export const COLOR_THEMES = [
  { value: 'default', label: 'theme.default', color: '#3b82f6', darkColor: '#60a5fa', contrastColor: '#ffffff' },
  { value: 'pure-white', label: 'theme.pureWhite', color: '#64748b', darkColor: '#94a3b8', contrastColor: '#ffffff', lightBorder: true },
  { value: 'forest-green', label: 'theme.forestGreen', color: '#009100', darkColor: '#22c55e', contrastColor: '#ffffff' },
  { value: 'ocean-blue', label: 'theme.oceanBlue', color: '#0ea5e9', darkColor: '#38bdf8', contrastColor: '#ffffff' },
  { value: 'sunset-orange', label: 'theme.sunsetOrange', color: '#f97316', darkColor: '#fb923c', contrastColor: '#ffffff' },
  { value: 'royal-purple', label: 'theme.royalPurple', color: '#8b5cf6', darkColor: '#a78bfa', contrastColor: '#ffffff' },
  { value: 'cherry-blossom', label: 'theme.cherryBlossom', color: '#ec4899', darkColor: '#f472b6', contrastColor: '#ffffff' },
  { value: 'midnight-teal', label: 'theme.midnightTeal', color: '#14b8a6', darkColor: '#2dd4bf', contrastColor: '#ffffff' },
  { value: 'quantum-rose', label: 'theme.quantumRose', color: '#e11d48', darkColor: '#fb7185', contrastColor: '#ffffff' },
  { value: 'vercel', label: 'theme.vercel', color: '#000000', darkColor: '#ffffff', contrastColor: '#ffffff', darkContrastColor: '#000000' },
  { value: 'clean-slate', label: 'theme.cleanSlate', color: '#334155', darkColor: '#94a3b8', contrastColor: '#ffffff' },
]

// 将 HEX 颜色转换为 RGB
function hexToRgb(hex) {
  const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex)
  return result ? {
    r: parseInt(result[1], 16),
    g: parseInt(result[2], 16),
    b: parseInt(result[3], 16)
  } : null
}

// 调整颜色亮度
function adjustBrightness(hex, percent) {
  const rgb = hexToRgb(hex)
  if (!rgb) return hex
  
  const adjust = (value) => {
    const adjusted = Math.round(value + (255 - value) * (percent / 100))
    return Math.min(255, Math.max(0, adjusted))
  }
  
  if (percent > 0) {
    // 变亮
    return `rgb(${adjust(rgb.r)}, ${adjust(rgb.g)}, ${adjust(rgb.b)})`
  } else {
    // 变暗
    const darken = (value) => Math.round(value * (1 + percent / 100))
    return `rgb(${darken(rgb.r)}, ${darken(rgb.g)}, ${darken(rgb.b)})`
  }
}

// 设置 Element Plus 主色变量
function setElementPlusPrimaryColor(color, contrastColor = '#ffffff') {
  const root = document.documentElement
  
  // 设置主色
  root.style.setProperty('--el-color-primary', color)
  
  // 设置对比色（用于按钮文字等）
  root.style.setProperty('--el-color-primary-contrast', contrastColor)
  
  // 设置主色的亮色变体（用于 hover、disabled 等状态）
  root.style.setProperty('--el-color-primary-light-3', adjustBrightness(color, 30))
  root.style.setProperty('--el-color-primary-light-5', adjustBrightness(color, 50))
  root.style.setProperty('--el-color-primary-light-7', adjustBrightness(color, 70))
  root.style.setProperty('--el-color-primary-light-8', adjustBrightness(color, 80))
  root.style.setProperty('--el-color-primary-light-9', adjustBrightness(color, 90))
  
  // 设置主色的暗色变体（用于 active 状态）
  root.style.setProperty('--el-color-primary-dark-2', adjustBrightness(color, -20))
}

export const useThemeStore = defineStore('theme', () => {
  // 主题模式（亮色/暗色/跟随系统）
  const theme = ref('system')
  // 颜色主题
  const colorTheme = ref('default')
  // 是否为暗色模式
  const isDark = ref(false)
  // 是否已从服务端加载
  const loaded = ref(false)

  // 从服务端加载主题配置
  async function loadFromServer() {
    try {
      const res = await request.post('/theme/config/get')
      if (res.code === 0 && res.config) {
        theme.value = res.config.theme || 'system'
        colorTheme.value = res.config.colorTheme || 'default'
        loaded.value = true
        updateTheme()
      }
    } catch (e) {
      console.error('Failed to load theme config:', e)
      // 加载失败时使用本地存储的配置
      theme.value = localStorage.getItem('theme') || 'system'
      colorTheme.value = localStorage.getItem('colorTheme') || 'default'
    }
  }

  // 保存到服务端
  async function saveToServer() {
    try {
      await request.post('/theme/config/save', {
        theme: theme.value,
        colorTheme: colorTheme.value
      })
    } catch (e) {
      console.error('Failed to save theme config:', e)
    }
  }

  // 初始化主题
  async function initTheme() {
    // 先从服务端加载
    await loadFromServer()
    updateTheme()
  }

  // 更新主题
  function updateTheme() {
    const root = document.documentElement
    
    // 移除所有主题类
    root.classList.remove('light', 'dark')
    // 移除所有颜色主题类
    COLOR_THEMES.forEach(t => {
      if (t.value !== 'default') {
        root.classList.remove(`theme-${t.value}`)
      }
    })
    // 移除旧的主题类
    root.classList.remove('theme-cosmic-night', 'theme-vercel-dark')
    
    // 确定是否使用暗色模式
    let shouldBeDark = false
    
    if (theme.value === 'dark') {
      shouldBeDark = true
    } else if (theme.value === 'system') {
      shouldBeDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    }
    
    isDark.value = shouldBeDark
    
    // 应用主题类
    root.classList.add(shouldBeDark ? 'dark' : 'light')
    
    // 应用颜色主题
    if (colorTheme.value !== 'default') {
      root.classList.add(`theme-${colorTheme.value}`)
    }
    
    // 设置 Element Plus 主色
    const currentColorTheme = COLOR_THEMES.find(t => t.value === colorTheme.value)
    if (currentColorTheme) {
      // 根据明暗模式选择合适的颜色
      const primaryColor = shouldBeDark && currentColorTheme.darkColor 
        ? currentColorTheme.darkColor 
        : currentColorTheme.color
      // 根据明暗模式选择对比色
      const contrastColor = shouldBeDark && currentColorTheme.darkContrastColor
        ? currentColorTheme.darkContrastColor
        : currentColorTheme.contrastColor || '#ffffff'
      setElementPlusPrimaryColor(primaryColor, contrastColor)
    }
    
    // 同时保存到本地存储（作为备份）
    localStorage.setItem('theme', theme.value)
    localStorage.setItem('colorTheme', colorTheme.value)
  }

  // 切换主题模式
  function setTheme(newTheme) {
    theme.value = newTheme
    updateTheme()
    saveToServer()
  }

  // 切换颜色主题
  function setColorTheme(newColorTheme) {
    colorTheme.value = newColorTheme
    updateTheme()
    saveToServer()
  }

  // 切换暗色模式
  function toggleTheme() {
    if (theme.value === 'light') {
      setTheme('dark')
    } else if (theme.value === 'dark') {
      setTheme('light')
    } else {
      // 如果是 system，则切换到相反的模式
      const systemIsDark = window.matchMedia('(prefers-color-scheme: dark)').matches
      setTheme(systemIsDark ? 'light' : 'dark')
    }
  }

  // 监听系统主题变化
  function watchSystemTheme() {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    
    const handleChange = () => {
      if (theme.value === 'system') {
        updateTheme()
      }
    }
    
    mediaQuery.addEventListener('change', handleChange)
    
    return () => {
      mediaQuery.removeEventListener('change', handleChange)
    }
  }

  // 监听主题变化
  watch([theme, colorTheme], () => {
    updateTheme()
  })

  return {
    theme,
    colorTheme,
    isDark,
    loaded,
    initTheme,
    loadFromServer,
    setTheme,
    setColorTheme,
    toggleTheme,
    watchSystemTheme,
    COLOR_THEMES,
  }
})
