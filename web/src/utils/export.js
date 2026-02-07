/**
 * 通用导出工具
 * 用于统一处理 CSV、JSON、Excel 格式的数据导出
 */

/**
 * 下载文件
 * @param {string} content - 文件内容
 * @param {string} filename - 文件名
 * @param {string} mimeType - MIME类型
 */
export function downloadFile(content, filename, mimeType) {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

/**
 * 生成带时间戳的文件名
 * @param {string} prefix - 文件名前缀
 * @param {string} extension - 文件扩展名
 * @returns {string} 完整文件名
 */
export function generateFilename(prefix, extension) {
  const now = new Date()
  const timestamp = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}_${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}`
  return `${prefix}_${timestamp}.${extension}`
}

/**
 * 转义CSV单元格值
 * @param {any} value - 原始值
 * @returns {string} 转义后的值
 */
function escapeCSVValue(value) {
  if (value === null || value === undefined) {
    return ''
  }
  if (Array.isArray(value)) {
    value = value.join('; ')
  }
  const str = String(value)
  // 如果包含逗号、换行或双引号，需要用双引号包裹并转义内部双引号
  if (str.includes(',') || str.includes('\n') || str.includes('"')) {
    return `"${str.replace(/"/g, '""')}"`
  }
  return str
}

/**
 * 导出数据为CSV格式
 * @param {Array} data - 数据数组
 * @param {Array} columns - 列定义 [{key: 'fieldName', label: '列标题'}, ...]
 * @param {string} filenamePrefix - 文件名前缀
 */
export function exportToCSV(data, columns, filenamePrefix = 'export') {
  if (!data || data.length === 0) {
    console.warn('No data to export')
    return false
  }

  // 生成表头
  const headers = columns.map(col => escapeCSVValue(col.label))
  
  // 生成数据行
  const rows = data.map(row => {
    return columns.map(col => {
      let value = row[col.key]
      // 支持嵌套属性访问，如 'user.name'
      if (col.key.includes('.')) {
        const keys = col.key.split('.')
        value = keys.reduce((obj, key) => obj?.[key], row)
      }
      // 支持自定义格式化函数
      if (col.formatter && typeof col.formatter === 'function') {
        value = col.formatter(value, row)
      }
      return escapeCSVValue(value)
    }).join(',')
  })

  // 添加BOM以支持Excel正确识别UTF-8
  const csvContent = '\uFEFF' + [headers.join(','), ...rows].join('\n')
  const filename = generateFilename(filenamePrefix, 'csv')
  
  downloadFile(csvContent, filename, 'text/csv;charset=utf-8')
  return true
}

/**
 * 导出数据为JSON格式
 * @param {Array} data - 数据数组
 * @param {string} filenamePrefix - 文件名前缀
 */
export function exportToJSON(data, filenamePrefix = 'export') {
  if (!data || data.length === 0) {
    console.warn('No data to export')
    return false
  }

  const jsonContent = JSON.stringify(data, null, 2)
  const filename = generateFilename(filenamePrefix, 'json')
  
  downloadFile(jsonContent, filename, 'application/json;charset=utf-8')
  return true
}

/**
 * 转义HTML特殊字符
 * @param {string} str - 原始字符串
 * @returns {string} 转义后的字符串
 */
function escapeHtml(str) {
  if (str === null || str === undefined) return ''
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
}

/**
 * 导出数据为Excel格式（HTML表格，Excel可直接打开）
 * @param {Array} data - 数据数组
 * @param {Array} columns - 列定义 [{key: 'fieldName', label: '列标题'}, ...]
 * @param {string} filenamePrefix - 文件名前缀
 */
export function exportToExcel(data, columns, filenamePrefix = 'export') {
  if (!data || data.length === 0) {
    console.warn('No data to export')
    return false
  }

  // 构建HTML表格
  let htmlContent = `<html xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel" xmlns="http://www.w3.org/TR/REC-html40">
<head>
<meta charset="UTF-8">
<!--[if gte mso 9]><xml><x:ExcelWorkbook><x:ExcelWorksheets><x:ExcelWorksheet><x:Name>Sheet1</x:Name><x:WorksheetOptions><x:DisplayGridlines/></x:WorksheetOptions></x:ExcelWorksheet></x:ExcelWorksheets></x:ExcelWorkbook></xml><![endif]-->
</head>
<body>
<table border="1">`

  // 表头
  htmlContent += '<tr>'
  columns.forEach(col => {
    htmlContent += `<th style="background-color:#f0f0f0;font-weight:bold;">${escapeHtml(col.label)}</th>`
  })
  htmlContent += '</tr>'

  // 数据行
  data.forEach(row => {
    htmlContent += '<tr>'
    columns.forEach(col => {
      let value = row[col.key]
      // 支持嵌套属性访问
      if (col.key.includes('.')) {
        const keys = col.key.split('.')
        value = keys.reduce((obj, key) => obj?.[key], row)
      }
      // 支持自定义格式化函数
      if (col.formatter && typeof col.formatter === 'function') {
        value = col.formatter(value, row)
      }
      if (Array.isArray(value)) {
        value = value.join('; ')
      }
      htmlContent += `<td>${escapeHtml(value)}</td>`
    })
    htmlContent += '</tr>'
  })

  htmlContent += '</table></body></html>'

  const filename = generateFilename(filenamePrefix, 'xls')
  downloadFile(htmlContent, filename, 'application/vnd.ms-excel;charset=utf-8')
  return true
}

/**
 * 通用导出函数，支持多种格式
 * @param {Array} data - 数据数组
 * @param {Array} columns - 列定义
 * @param {string} format - 导出格式 'csv' | 'json' | 'excel'
 * @param {string} filenamePrefix - 文件名前缀
 */
export function exportData(data, columns, format = 'csv', filenamePrefix = 'export') {
  switch (format.toLowerCase()) {
    case 'csv':
      return exportToCSV(data, columns, filenamePrefix)
    case 'json':
      return exportToJSON(data, filenamePrefix)
    case 'excel':
    case 'xls':
    case 'xlsx':
      return exportToExcel(data, columns, filenamePrefix)
    default:
      console.error(`Unsupported export format: ${format}`)
      return false
  }
}

/**
 * 预定义的资产导出列配置
 */
export const ASSET_EXPORT_COLUMNS = {
  // 资产清单列
  inventory: [
    { key: 'host', label: '主机' },
    { key: 'port', label: '端口' },
    { key: 'ip', label: 'IP' },
    { key: 'title', label: '标题' },
    { key: 'status', label: '状态码' },
    { key: 'technologies', label: '技术栈' },
    { key: 'labels', label: '标签' },
    { key: 'lastUpdated', label: '最后更新' }
  ],
  // 资产分组列
  groups: [
    { key: 'domain', label: '域名' },
    { key: 'totalServices', label: '服务数' },
    { key: 'status', label: '状态' },
    { key: 'duration', label: '持续时间' },
    { key: 'lastUpdated', label: '最后更新' }
  ],
  // 截图列
  screenshots: [
    { key: 'host', label: '主机' },
    { key: 'port', label: '端口' },
    { key: 'ip', label: 'IP' },
    { key: 'title', label: '标题' },
    { key: 'status', label: '状态码' },
    { key: 'technologies', label: '技术栈' },
    { key: 'lastUpdated', label: '最后更新' }
  ],
  // 漏洞列
  vulnerabilities: [
    { key: 'name', label: '漏洞名称' },
    { key: 'severity', label: '严重程度' },
    { key: 'host', label: '主机' },
    { key: 'port', label: '端口' },
    { key: 'template', label: '模板' },
    { key: 'description', label: '描述' },
    { key: 'createTime', label: '发现时间' }
  ],
  // 目录扫描列
  dirScans: [
    { key: 'url', label: 'URL' },
    { key: 'path', label: '路径' },
    { key: 'status', label: '状态码' },
    { key: 'size', label: '大小' },
    { key: 'title', label: '标题' },
    { key: 'createTime', label: '发现时间' }
  ]
}

export default {
  downloadFile,
  generateFilename,
  exportToCSV,
  exportToJSON,
  exportToExcel,
  exportData,
  ASSET_EXPORT_COLUMNS
}
