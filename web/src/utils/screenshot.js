/**
 * 截图工具函数
 * 处理截图数据的显示和格式化
 */

/**
 * 获取正确的截图 URL
 * @param {string} screenshot - 截图数据（可能是 base64 或完整的 data URI）
 * @returns {string} 正确格式的 data URI 或空字符串
 */
export function getScreenshotDataUrl(screenshot) {
  if (!screenshot) {
    return ''
  }

  // 如果已经是完整的 data URI，直接返回
  if (screenshot.startsWith('data:image/')) {
    return screenshot
  }

  // 如果是纯 base64 字符串，添加 data URI 前缀
  // 默认假设是 JPEG 格式（最常见）
  if (screenshot.match(/^[A-Za-z0-9+/=]+$/)) {
    return `data:image/jpeg;base64,${screenshot}`
  }

  // 如果以 / 开头，可能是 base64 数据（JPEG 通常以 /9j/ 开头）
  if (screenshot.startsWith('/9j/') || screenshot.startsWith('iVBOR')) {
    return `data:image/jpeg;base64,${screenshot}`
  }

  // 其他情况，尝试作为 JPEG 处理
  return `data:image/jpeg;base64,${screenshot}`
}

/**
 * 检测图片格式
 * @param {string} base64Data - base64 数据
 * @returns {string} MIME 类型
 */
export function detectImageFormat(base64Data) {
  if (!base64Data) return 'image/jpeg'

  // JPEG: /9j/
  if (base64Data.startsWith('/9j/') || base64Data.startsWith('iVBOR')) {
    return 'image/jpeg'
  }
  
  // PNG: iVBOR
  if (base64Data.startsWith('iVBOR')) {
    return 'image/png'
  }
  
  // GIF: R0lGOD
  if (base64Data.startsWith('R0lGOD')) {
    return 'image/gif'
  }
  
  // WebP: UklGR
  if (base64Data.startsWith('UklGR')) {
    return 'image/webp'
  }

  // 默认 JPEG
  return 'image/jpeg'
}

/**
 * 获取带正确格式的截图 URL
 * @param {string} screenshot - 截图数据
 * @returns {string} 完整的 data URI
 */
export function formatScreenshotUrl(screenshot) {
  if (!screenshot) {
    return ''
  }

  // 如果已经是完整的 data URI
  if (screenshot.startsWith('data:image/')) {
    return screenshot
  }

  // 检测图片格式
  const mimeType = detectImageFormat(screenshot)
  
  // 返回完整的 data URI
  return `data:${mimeType};base64,${screenshot}`
}

/**
 * 验证截图数据是否有效
 * @param {string} screenshot - 截图数据
 * @returns {boolean} 是否有效
 */
export function isValidScreenshot(screenshot) {
  if (!screenshot || typeof screenshot !== 'string') {
    return false
  }

  // 检查是否是有效的 data URI
  if (screenshot.startsWith('data:image/')) {
    return true
  }

  // 检查是否是有效的 base64 字符串
  // base64 只包含 A-Z, a-z, 0-9, +, /, = 字符
  return /^[A-Za-z0-9+/=]+$/.test(screenshot) || 
         screenshot.startsWith('/9j/') || 
         screenshot.startsWith('iVBOR')
}

/**
 * 处理图片加载错误
 * @param {Event} event - 错误事件
 */
export function handleScreenshotError(event) {
  const target = event.target
  if (target && target.parentNode) {
    // 隐藏失败的图片
    target.style.display = 'none'
    
    // 可以在这里添加错误日志
    console.warn('Screenshot failed to load:', target.src?.substring(0, 50))
  }
}

export default {
  getScreenshotDataUrl,
  detectImageFormat,
  formatScreenshotUrl,
  isValidScreenshot,
  handleScreenshotError
}
