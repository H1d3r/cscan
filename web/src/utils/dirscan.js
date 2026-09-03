/**
 * 校验目录扫描状态码。
 * Element Plus allow-create 会把自定义值保存为字符串，因此这里同时接受数字和数字字符串。
 */
export function isValidDirScanStatusCode(value) {
  const normalized = typeof value === 'string' ? value.trim() : value
  if (normalized === '') return false

  const code = Number(normalized)
  return Number.isInteger(code) && code >= 100 && code <= 599
}

/**
 * 将表单状态码转换为后端 DirScanConfig 所需的 JSON 数字数组。
 * 无效值由表单校验负责提示，此处过滤是为了避免自动保存产生 null。
 */
export function normalizeDirScanStatusCodes(values) {
  return (values || [])
    .filter(isValidDirScanStatusCode)
    .map(value => Number(typeof value === 'string' ? value.trim() : value))
}
