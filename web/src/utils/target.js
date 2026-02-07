/**
 * 目标格式校验工具
 */

/**
 * 校验单个目标
 * @param {string} target - 目标字符串
 * @returns {string|null} - 错误信息，如果有效则返回 null
 */
export function validateSingleTarget(target) {
  target = target.trim()
  if (!target || target.startsWith('#')) {
    return null // 空行或注释行
  }

  // 检查是否是带协议的URL格式 (http:// 或 https://)
  if (target.startsWith('http://') || target.startsWith('https://')) {
    return validateURL(target)
  }

  // 检查是否是 IPv6 格式（包含多个冒号）
  if (isIPv6Format(target)) {
    return validateIPv6Target(target)
  }

  // 去除可能的端口部分（仅针对IPv4和域名）
  let host = target
  const lastColon = target.lastIndexOf(':')
  if (lastColon !== -1) {
    const portStr = target.substring(lastColon + 1)
    const port = parseInt(portStr, 10)
    if (!isNaN(port) && port > 0 && port <= 65535) {
      host = target.substring(0, lastColon)
    }
  }

  // CIDR 格式
  if (host.includes('/')) {
    return validateCIDR(host)
  }

  // IP 范围格式
  if (host.includes('-')) {
    const parts = host.split('-')
    if (parts.length === 2 && isValidIPv4(parts[0].trim())) {
      return validateIPRange(host)
    }
    // 可能是域名中包含连字符
  }

  // 单个 IP
  if (isValidIPv4(host)) {
    return null
  }

  // 域名格式
  if (isValidDomain(host)) {
    return null
  }

  return '无效的目标格式，请输入有效的IP、CIDR、IP范围、域名或URL'
}

/**
 * 校验URL格式
 * 支持 http://domain.com 或 https://domain.com 以及带路径的URL
 * @param {string} url - URL字符串
 * @returns {string|null} - 错误信息，如果有效则返回 null
 */
function validateURL(url) {
  try {
    const urlObj = new URL(url)
    
    // 只支持 http 和 https 协议
    if (urlObj.protocol !== 'http:' && urlObj.protocol !== 'https:') {
      return '仅支持 http:// 或 https:// 协议'
    }
    
    // 获取主机名
    const hostname = urlObj.hostname
    if (!hostname) {
      return 'URL缺少主机名'
    }
    
    // 检查主机名是否是有效的IP或域名
    // 处理 IPv6 地址（被方括号包裹）
    if (hostname.startsWith('[') && hostname.endsWith(']')) {
      const ipv6 = hostname.slice(1, -1)
      if (!isValidIPv6(ipv6)) {
        return `无效的IPv6地址: ${hostname}`
      }
      return null
    }
    
    // 检查是否是IPv4
    if (isValidIPv4(hostname)) {
      return null
    }
    
    // 检查是否是有效域名
    if (isValidDomain(hostname)) {
      return null
    }
    
    // 也允许 localhost
    if (hostname === 'localhost') {
      return null
    }
    
    return `无效的主机名: ${hostname}`
  } catch (e) {
    return '无效的URL格式'
  }
}

/**
 * 校验 CIDR 格式
 * @param {string} cidr - CIDR 字符串
 * @returns {string|null} - 错误信息
 */
function validateCIDR(cidr) {
  const parts = cidr.split('/')
  if (parts.length !== 2) {
    return '无效的CIDR格式'
  }

  const ipPart = parts[0]
  const maskPart = parts[1]

  // 检查掩码
  const mask = parseInt(maskPart, 10)
  if (isNaN(mask) || mask < 0 || mask > 32) {
    return `无效的子网掩码: ${maskPart}`
  }

  // 检查 IP 部分是否完整
  const octets = ipPart.split('.')
  if (octets.length !== 4) {
    const suggestion = suggestCIDRFix(ipPart, maskPart)
    return `IP地址不完整，缺少${4 - octets.length}个八位组。正确格式示例: ${suggestion}`
  }

  // 验证每个八位组
  for (let i = 0; i < octets.length; i++) {
    const val = parseInt(octets[i], 10)
    if (isNaN(val) || val < 0 || val > 255) {
      return `第${i + 1}个八位组 '${octets[i]}' 无效，应为0-255之间的数字`
    }
  }

  return null
}

/**
 * 提供 CIDR 修复建议
 */
function suggestCIDRFix(ipPart, maskPart) {
  const octets = ipPart.split('.')
  while (octets.length < 4) {
    octets.push('0')
  }
  return octets.join('.') + '/' + maskPart
}

/**
 * 校验 IP 范围格式
 */
function validateIPRange(ipRange) {
  const parts = ipRange.split('-')
  if (parts.length !== 2) {
    return '无效的IP范围格式'
  }

  const startIP = parts[0].trim()
  const endIP = parts[1].trim()

  if (!isValidIPv4(startIP)) {
    return `起始IP '${startIP}' 无效`
  }
  if (!isValidIPv4(endIP)) {
    return `结束IP '${endIP}' 无效`
  }

  // 检查起始IP是否小于等于结束IP
  const start = startIP.split('.').map(Number)
  const end = endIP.split('.').map(Number)

  for (let i = 0; i < 4; i++) {
    if (start[i] > end[i]) {
      return '起始IP不能大于结束IP'
    }
    if (start[i] < end[i]) {
      break
    }
  }

  return null
}

/**
 * 检查是否是 IPv6 格式（包含多个冒号、以 [ 开头、或包含 Zone ID）
 */
function isIPv6Format(target) {
  // IPv6 地址包含多个冒号，或者是 [IPv6]:port 格式
  if (target.startsWith('[')) {
    return true
  }
  // 包含 Zone ID 的 IPv6（如 fe80::1%eth0）
  if (target.includes('%')) {
    return true
  }
  // 统计冒号数量，IPv6 至少有2个冒号
  const colonCount = (target.match(/:/g) || []).length
  return colonCount >= 2
}

/**
 * 校验 IPv6 目标
 */
function validateIPv6Target(target) {
  let ipv6 = target
  
  // 处理 [IPv6]:port 格式
  if (target.startsWith('[')) {
    const closeBracket = target.indexOf(']')
    if (closeBracket === -1) {
      return '无效的IPv6格式，缺少闭合括号 ]'
    }
    ipv6 = target.substring(1, closeBracket)
    // 检查端口部分
    const remaining = target.substring(closeBracket + 1)
    if (remaining && !remaining.startsWith(':')) {
      return '无效的IPv6格式'
    }
    if (remaining.startsWith(':')) {
      const portStr = remaining.substring(1)
      const port = parseInt(portStr, 10)
      if (isNaN(port) || port <= 0 || port > 65535) {
        return `无效的端口号: ${portStr}`
      }
    }
  }

  // 去除 zone ID（如 %eth0 或 %5）
  const zoneIndex = ipv6.indexOf('%')
  if (zoneIndex !== -1) {
    ipv6 = ipv6.substring(0, zoneIndex)
  }

  // 处理 IPv6 CIDR
  if (ipv6.includes('/')) {
    const parts = ipv6.split('/')
    if (parts.length !== 2) {
      return '无效的IPv6 CIDR格式'
    }
    const mask = parseInt(parts[1], 10)
    if (isNaN(mask) || mask < 0 || mask > 128) {
      return `无效的IPv6子网掩码: ${parts[1]}`
    }
    ipv6 = parts[0]
  }

  if (isValidIPv6(ipv6)) {
    return null
  }

  return '无效的IPv6地址格式'
}

/**
 * 检查是否是有效的 IPv6 地址
 */
function isValidIPv6(ip) {
  // 简化的 IPv6 验证
  // IPv6 格式: 8组4位十六进制数，用冒号分隔
  // 支持 :: 缩写形式
  
  if (!ip || ip.length > 45) {
    return false
  }

  // 检查是否包含非法字符
  if (!/^[0-9a-fA-F:]+$/.test(ip)) {
    return false
  }

  // 处理 :: 缩写
  const doubleColonCount = (ip.match(/::/g) || []).length
  if (doubleColonCount > 1) {
    return false // 只能有一个 ::
  }

  if (doubleColonCount === 1) {
    // 有 :: 缩写的情况
    const parts = ip.split('::')
    const left = parts[0] ? parts[0].split(':') : []
    const right = parts[1] ? parts[1].split(':') : []
    
    // 总组数不能超过8
    if (left.length + right.length > 7) {
      return false
    }

    // 验证每个组
    for (const group of [...left, ...right]) {
      if (group && (group.length > 4 || !/^[0-9a-fA-F]*$/.test(group))) {
        return false
      }
    }
  } else {
    // 没有 :: 缩写，必须是完整的8组
    const groups = ip.split(':')
    if (groups.length !== 8) {
      return false
    }

    for (const group of groups) {
      if (!group || group.length > 4 || !/^[0-9a-fA-F]+$/.test(group)) {
        return false
      }
    }
  }

  return true
}

/**
 * 检查是否是有效的 IPv4 地址
 */
function isValidIPv4(ip) {
  const parts = ip.split('.')
  if (parts.length !== 4) return false
  
  for (const part of parts) {
    const num = parseInt(part, 10)
    if (isNaN(num) || num < 0 || num > 255 || part !== String(num)) {
      return false
    }
  }
  return true
}

/**
 * 检查是否是有效的域名
 */
function isValidDomain(domain) {
  // 域名规则：
  // 1. 每个标签可以是1-63个字符
  // 2. 标签由字母、数字、连字符组成
  // 3. 标签不能以连字符开头或结尾
  // 4. 顶级域名至少2个字符
  // 5. 支持单字符子域名如 m.example.com
  
  // 更宽松的域名正则，支持单字符子域名
  const domainRegex = /^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.([a-zA-Z]{2,})$/
  
  // 简化版：直接检查基本格式
  // 1. 不能以点或连字符开头结尾
  // 2. 不能有连续的点
  // 3. 每个标签不能以连字符开头或结尾
  if (domain.startsWith('.') || domain.endsWith('.') || 
      domain.startsWith('-') || domain.endsWith('-') ||
      domain.includes('..')) {
    return false
  }
  
  // 分割成标签检查
  const labels = domain.split('.')
  if (labels.length < 2) {
    return false // 至少要有一个子域和顶级域
  }
  
  // 检查每个标签
  for (let i = 0; i < labels.length; i++) {
    const label = labels[i]
    
    // 标签长度1-63
    if (label.length === 0 || label.length > 63) {
      return false
    }
    
    // 标签不能以连字符开头或结尾
    if (label.startsWith('-') || label.endsWith('-')) {
      return false
    }
    
    // 标签只能包含字母、数字、连字符
    if (!/^[a-zA-Z0-9\-]+$/.test(label)) {
      return false
    }
  }
  
  // 顶级域名至少2个字符，且只能是字母
  const tld = labels[labels.length - 1]
  if (tld.length < 2 || !/^[a-zA-Z]+$/.test(tld)) {
    return false
  }
  
  return true
}

/**
 * 校验多行目标
 * 支持以下分隔符：换行、逗号、分号、空格
 * @param {string} targets - 多行目标字符串
 * @returns {Array<{line: number, target: string, message: string}>} - 错误列表
 */
export function validateTargets(targets) {
  const errors = []
  const lines = targets.split('\n')

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim()
    if (!line || line.startsWith('#')) {
      continue
    }

    // 支持逗号、分号、空格分隔的多个目标
    // 先按逗号分割，再按分号分割，最后按空格分割
    const targetsInLine = splitTargets(line)
    
    for (const target of targetsInLine) {
      const trimmedTarget = target.trim()
      if (!trimmedTarget) continue
      
      const error = validateSingleTarget(trimmedTarget)
      if (error) {
        errors.push({
          line: i + 1,
          target: trimmedTarget,
          message: error
        })
      }
    }
  }

  return errors
}

/**
 * 分割一行中的多个目标
 * 支持逗号、分号、空格作为分隔符
 * @param {string} line - 一行目标字符串
 * @returns {string[]} - 分割后的目标数组
 */
function splitTargets(line) {
  // 优先按逗号分割
  if (line.includes(',')) {
    return line.split(',').map(t => t.trim()).filter(t => t)
  }
  // 其次按分号分割
  if (line.includes(';')) {
    return line.split(';').map(t => t.trim()).filter(t => t)
  }
  // 最后按空格分割（但要注意不要分割带端口的目标如 192.168.1.1:8080）
  // 空格分隔只在没有其他分隔符时使用，且要确保不是单个目标
  if (line.includes(' ') && !line.includes(':')) {
    return line.split(/\s+/).map(t => t.trim()).filter(t => t)
  }
  // 默认返回整行作为单个目标
  return [line]
}

/**
 * 格式化校验错误为用户友好的消息
 * @param {Array} errors - 错误列表
 * @returns {string} - 格式化的错误消息
 */
export function formatValidationErrors(errors) {
  if (errors.length === 0) return ''

  if (errors.length === 1) {
    const e = errors[0]
    return `第${e.line}行 '${e.target}': ${e.message}`
  }

  const messages = errors.map(e => `第${e.line}行 '${e.target}': ${e.message}`)
  return `发现${errors.length}个目标格式错误:\n${messages.join('\n')}`
}
