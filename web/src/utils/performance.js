/**
 * 前端性能监控工具
 */

// 是否启用非错误日志输出（设为 false 禁用）
const ENABLE_LOGS = false

// 封装的日志函数，只在启用时输出
const log = (...args) => ENABLE_LOGS && console.log(...args)
const group = (...args) => ENABLE_LOGS && console.group(...args)
const groupEnd = () => ENABLE_LOGS && console.groupEnd()

/**
 * 记录页面加载性能指标
 */
export function logPagePerformance() {
  if (typeof window === 'undefined' || !window.performance) {
    return
  }

  // 等待页面完全加载
  window.addEventListener('load', () => {
    setTimeout(() => {
      const perfData = window.performance.timing
      const pageLoadTime = perfData.loadEventEnd - perfData.navigationStart
      const domReadyTime = perfData.domContentLoadedEventEnd - perfData.navigationStart
      const dnsTime = perfData.domainLookupEnd - perfData.domainLookupStart
      const tcpTime = perfData.connectEnd - perfData.connectStart
      const ttfbTime = perfData.responseStart - perfData.navigationStart
      const downloadTime = perfData.responseEnd - perfData.responseStart
      const domParseTime = perfData.domInteractive - perfData.responseEnd

      group('📊 页面性能指标')
      log(`⏱️  页面完全加载时间: ${pageLoadTime}ms`)
      log(`📄 DOM 就绪时间: ${domReadyTime}ms`)
      log(`🌐 DNS 查询时间: ${dnsTime}ms`)
      log(`🔌 TCP 连接时间: ${tcpTime}ms`)
      log(`⚡ 首字节时间 (TTFB): ${ttfbTime}ms`)
      log(`📥 资源下载时间: ${downloadTime}ms`)
      log(`🔨 DOM 解析时间: ${domParseTime}ms`)
      groupEnd()

      // 获取资源加载信息
      const resources = window.performance.getEntriesByType('resource')
      const jsResources = resources.filter(r => r.name.endsWith('.js'))
      const cssResources = resources.filter(r => r.name.endsWith('.css'))

      group('📦 资源加载统计')
      log(`JavaScript 文件数: ${jsResources.length}`)
      log(`CSS 文件数: ${cssResources.length}`)
      log(`总资源数: ${resources.length}`)
      groupEnd()

      // 显示最大的 JS 文件
      const largestJS = jsResources
        .sort((a, b) => b.transferSize - a.transferSize)
        .slice(0, 5)

      if (largestJS.length > 0) {
        group('📊 最大的 5 个 JS 文件')
        largestJS.forEach((resource, index) => {
          const size = (resource.transferSize / 1024).toFixed(2)
          const duration = resource.duration.toFixed(2)
          const name = resource.name.split('/').pop()
          log(`${index + 1}. ${name}: ${size}KB (${duration}ms)`)
        })
        groupEnd()
      }
    }, 0)
  })
}

/**
 * 监控组件加载时间
 */
export function measureComponentLoad(componentName) {
  const startTime = performance.now()
  
  return () => {
    const endTime = performance.now()
    const loadTime = (endTime - startTime).toFixed(2)
    log(`🎯 组件 [${componentName}] 加载耗时: ${loadTime}ms`)
  }
}

/**
 * 监控路由切换性能
 */
export function setupRouterPerformance(router) {
  router.beforeEach((to, from, next) => {
    // 记录路由切换开始时间
    window.__routeStartTime = performance.now()
    next()
  })

  router.afterEach((to, from) => {
    if (window.__routeStartTime) {
      const endTime = performance.now()
      const duration = (endTime - window.__routeStartTime).toFixed(2)
      log(`🚀 路由切换 [${from.path} → ${to.path}] 耗时: ${duration}ms`)
      delete window.__routeStartTime
    }
  })
}

/**
 * 获取 Web Vitals 指标
 */
export function getWebVitals() {
  if (!window.PerformanceObserver) {
    console.warn('浏览器不支持 PerformanceObserver')
    return
  }

  // Largest Contentful Paint (LCP)
  try {
    const lcpObserver = new PerformanceObserver((list) => {
      const entries = list.getEntries()
      const lastEntry = entries[entries.length - 1]
      log(`🎨 LCP (最大内容绘制): ${lastEntry.renderTime || lastEntry.loadTime}ms`)
    })
    lcpObserver.observe({ entryTypes: ['largest-contentful-paint'] })
  } catch (e) {
    // 忽略不支持的情况
  }

  // First Input Delay (FID)
  try {
    const fidObserver = new PerformanceObserver((list) => {
      const entries = list.getEntries()
      entries.forEach((entry) => {
        log(`⚡ FID (首次输入延迟): ${entry.processingStart - entry.startTime}ms`)
      })
    })
    fidObserver.observe({ entryTypes: ['first-input'] })
  } catch (e) {
    // 忽略不支持的情况
  }

  // Cumulative Layout Shift (CLS)
  try {
    let clsScore = 0
    const clsObserver = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (!entry.hadRecentInput) {
          clsScore += entry.value
        }
      }
      log(`📐 CLS (累积布局偏移): ${clsScore.toFixed(4)}`)
    })
    clsObserver.observe({ entryTypes: ['layout-shift'] })
  } catch (e) {
    // 忽略不支持的情况
  }
}

/**
 * 监控懒加载组件
 */
export function trackLazyComponent(componentName) {
  const startMark = `${componentName}-start`
  const endMark = `${componentName}-end`
  const measureName = `${componentName}-load`

  performance.mark(startMark)

  return () => {
    performance.mark(endMark)
    performance.measure(measureName, startMark, endMark)
    
    const measure = performance.getEntriesByName(measureName)[0]
    log(`🔄 懒加载组件 [${componentName}] 耗时: ${measure.duration.toFixed(2)}ms`)
    
    // 清理标记
    performance.clearMarks(startMark)
    performance.clearMarks(endMark)
    performance.clearMeasures(measureName)
  }
}

/**
 * 在开发环境启用性能监控
 */
export function enablePerformanceMonitoring() {
  if (import.meta.env.DEV) {
    log('🔍 性能监控已启用')
    logPagePerformance()
    getWebVitals()
  }
}
