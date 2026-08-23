<template>
  <div class="container-logs-page">
    <el-card>
      <template #header>
        <div class="log-header">
          <span>{{ $t('container.title') }}</span>
          <div class="log-filters">
            <!-- 第一行：数据选择 -->
            <div class="filter-row">
              <el-select v-model="historyDate" size="small" style="width: 140px" :placeholder="$t('container.selectDate')" @change="loadHistoryFiles">
                <el-option v-for="d in logDates" :key="d" :label="d" :value="d" />
              </el-select>
              <el-select v-model="historyContainer" size="small" style="width: 200px" :placeholder="$t('container.selectContainer')" @change="loadHistoryLogs">
                <el-option v-for="f in historyFiles" :key="f.name" :label="f.name" :value="f.name" />
              </el-select>
              <el-select v-model="historyTail" size="small" style="width: 110px" @change="loadHistoryLogs">
                <el-option label="200 行" :value="200" />
                <el-option label="500 行" :value="500" />
                <el-option label="1000 行" :value="1000" />
                <el-option label="5000 行" :value="5000" />
              </el-select>
              <el-button size="small" type="primary" @click="loadHistoryLogs" :loading="historyLoading">
                <el-icon style="margin-right: 4px"><Refresh /></el-icon>
                {{ $t('container.refresh') }}
              </el-button>
            </div>
            <!-- 第二行：筛选和工具 -->
            <div class="filter-row">
              <el-input
                v-model="searchKeyword"
                :placeholder="$t('container.searchLogs')"
                clearable
                size="small"
                style="width: 220px"
              >
                <template #prefix>
                  <el-icon><Search /></el-icon>
                </template>
              </el-input>
              <el-select v-model="levelFilter" size="small" style="width: 140px">
                <el-option :label="$t('container.allLevels')" value="all" />
                <el-option :label="$t('container.allWithDebug')" value="all_debug" />
                <el-option label="ERROR" value="ERROR" />
                <el-option label="WARN" value="WARN" />
                <el-option label="INFO" value="INFO" />
                <el-option label="DEBUG" value="DEBUG" />
              </el-select>
              <el-dropdown size="small" @command="exportLogs">
                <el-button size="small">{{ $t('container.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="txt">{{ $t('container.exportTxt') }}</el-dropdown-item>
                    <el-dropdown-item command="json">{{ $t('container.exportJson') }}</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
              <span class="line-count">{{ $t('container.lineCount') }}: {{ filteredLines.length }}</span>
            </div>
          </div>
        </div>
      </template>

      <div class="layout">
        <!-- 日志查看区 -->
        <section class="viewer">
          <!-- 历史信息栏 -->
          <div v-if="historyContainer" class="history-info">
            <span class="history-meta">{{ historyDate }} / {{ historyContainer }}</span>
            <span v-if="historyTotal > 0" class="history-meta">
              {{ $t('container.totalLines') }}: {{ historyTotal }}
              <template v-if="historyTruncated"> ({{ $t('container.showingLast') }} {{ historyLines.length }})</template>
            </span>
          </div>

          <!-- 空状态 -->
          <div v-if="!historyContainer && !historyLoading" class="empty">
            <el-icon :size="48" style="color: var(--el-text-color-disabled)"><Document /></el-icon>
            <span>{{ $t('container.selectDateAndContainer') }}</span>
          </div>

          <!-- 日志内容 -->
          <div v-else ref="logBox" class="log-box" @scroll="onScroll">
            <div
              v-for="(l, idx) in filteredLines"
              :key="idx"
              class="log-line"
              :class="lineClass(l)"
            >
              <span class="log-ln">{{ idx + 1 }}</span>
              <span class="log-time">{{ l.time || '--:--:--' }}</span>
              <span class="log-level" :class="levelClass(l.level)">{{ l.level || 'LOG' }}</span>
              <span class="log-body">{{ l.body }}</span>
            </div>
          </div>

          <!-- 滚动到底部按钮 -->
          <transition name="el-fade-in">
            <button
              v-if="showScrollBtn"
              class="scroll-bottom-btn"
              @click="scrollToBottom"
            >
              <el-icon><Bottom /></el-icon>
              {{ $t('container.scrollToBottom') }}
            </button>
          </transition>
        </section>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { computed, nextTick, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Refresh, ArrowDown, Document, Bottom } from '@element-plus/icons-vue'
import { getLogDates, getLogFiles, getLogHistory } from '@/api/container'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// ==================== 通用状态 ====================
const searchKeyword = ref('')
const levelFilter = ref('all')
const logBox = ref(null)
const showScrollBtn = ref(false)

// ==================== 历史日志 ====================
const logDates = ref([])
const historyDate = ref('')
const historyFiles = ref([])
const historyContainer = ref('')
const historyTail = ref(500)
const historyLines = ref([])
const historyTotal = ref(0)
const historyTruncated = ref(false)
const historyLoading = ref(false)

async function loadLogDates() {
  try {
    const res = await getLogDates()
    if (res.code === 0 && res.dates) {
      logDates.value = res.dates
      if (res.dates.length && !historyDate.value) {
        historyDate.value = res.dates[0]
        await loadHistoryFiles()
      }
    }
  } catch (_) {}
}

async function loadHistoryFiles() {
  historyContainer.value = ''
  historyFiles.value = []
  historyLines.value = []
  if (!historyDate.value) return
  try {
    const res = await getLogFiles(historyDate.value)
    if (res.code === 0 && res.files) {
      historyFiles.value = res.files
      if (res.files.length) {
        historyContainer.value = res.files[0].name
        await loadHistoryLogs()
      }
    }
  } catch (_) {}
}

async function loadHistoryLogs() {
  if (!historyDate.value || !historyContainer.value) return
  historyLoading.value = true
  try {
    const res = await getLogHistory({
      date: historyDate.value,
      name: historyContainer.value,
      tail: historyTail.value
    })
    if (res.code === 0) {
      historyLines.value = (res.lines || []).map(parseHistoryLine)
      historyTotal.value = res.total || 0
      historyTruncated.value = res.truncated || false
      nextTick(scrollToBottom)
    }
  } catch (e) {
    ElMessage.error(e.message || 'error')
  } finally {
    historyLoading.value = false
  }
}

// 解析历史日志行: "2026-07-28T08:37:34.123Z [stdout] actual content"
function parseHistoryLine(raw) {
  const obj = { line: '', stream: 'stdout', ts: '', container: historyContainer.value }
  // Try Docker format: "2026-07-28T08:37:34.123Z [stdout] content"
  const m = raw.match(/^(\S+)\s+\[(\w+)\]\s+([\s\S]*)$/)
  if (m) {
    obj.ts = m[1]
    obj.stream = m[2]
    obj.line = m[3]
  } else {
    obj.line = raw
  }
  return parseLogLine(obj)
}

// ==================== 过滤 ====================
// all: 默认隐藏 DEBUG（任务流水日志）；all_debug: 全量显示
const filteredLines = computed(() => {
  const kw = searchKeyword.value.trim().toLowerCase()
  const lf = levelFilter.value
  return historyLines.value.filter(l => {
    if (lf === 'all' && l.level === 'DEBUG') return false
    if (lf !== 'all' && lf !== 'all_debug' && l.level !== lf) return false
    if (kw && !(l.raw || l.body || '').toLowerCase().includes(kw)) return false
    return true
  })
})

// ==================== 日志解析(多格式) ====================
const ANSI_RE = /\x1b\[[0-9;]*m/g
// go-zero plain 编码的等级字段带空格填充（" info "），需容忍；含 severe
const GOZERO_RE = /^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\t[ \t]*(info|error|debug|slow|stat|alert|severe|fatal)[ \t]*\t([\s\S]*)$/i
const GOZERO_SHORT_RE = /^(\d{2}:\d{2}:\d{2})\t[ \t]*(info|error|debug|slow|stat|alert|severe|fatal)[ \t]*\t([\s\S]*)$/i
const REDIS_RE = /^(\d+):([A-Z])\s+(\d{2}\s+\w{3}\s+\d{4}\s+\d{2}:\d{2}:\d{2}\.\d{3})\s+([*#-])\s+(.*)$/
const NGINX_ACCESS_RE = /^([\d.]+)\s+-\s+(\S+)\s+\[([^\]]+)\]\s+"(\S+)\s+(\S+)\s+(\S+)"\s+(\d{3})\s+(\d+|-)/
const NGINX_ERROR_RE = /^(\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2})\s+\[(\w+)\]\s+(\d+)#(\d+):\s+(.*)$/
const WORKER_INNER_RE = /^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+\[(ERROR|WARN|INFO|DEBUG|FATAL|PANIC|TRACE)\]\s+(?:\[([a-zA-Z0-9_-]+(?:-\d+)?)\]\s+)?(?:\[Task:([a-zA-Z0-9_-]+)\]\s+)?([\s\S]*)$/i
const LEVEL_RE = /\[(ERROR|WARN|INFO|DEBUG|FATAL|PANIC|TRACE)\]/i

const REDIS_LEVEL_MAP = { '*': 'INFO', '#': 'WARN', '-': 'DEBUG' }
const MONGO_SEVERITY_MAP = { F: 'FATAL', E: 'ERROR', W: 'WARN', I: 'INFO', D: 'DEBUG', D1: 'DEBUG', D2: 'DEBUG' }

// go-zero JSON 格式: {"level":"info","ts":"...","caller":"...","content":"..."}
function tryParseGoZeroJSON(raw) {
  if (!raw.startsWith('{')) return null
  try {
    const json = JSON.parse(raw)
    if (json.level && json.ts) {
      let level = (json.level || '').toUpperCase()
      if (level === 'SLOW') level = 'WARN'
      const parts = []
      if (json.caller) parts.push(`[${json.caller}]`)
      if (json.content) parts.push(json.content)
      if (json.trace) parts.push(json.trace)
      if (json.span) parts.push(JSON.stringify(json.span))
      const body = parts.join(' ') || raw
      return { stream: 'stdout', level, time: formatTimeShort(json.ts), body, container: '', raw }
    }
  } catch (_) {}
  return null
}

function parseLogLine(obj) {
  const raw = (obj.line || '').replace(ANSI_RE, '')
  const containerName = obj.container || ''
  let level = ''
  let time = obj.ts || ''  // 默认使用 Docker 日志时间戳
  let body = raw

  // 0) go-zero JSON 格式
  const gzJson = tryParseGoZeroJSON(raw)
  if (gzJson) {
    gzJson.container = containerName
    return gzJson
  }

  // 1) go-zero plain
  const gzMatch = raw.match(GOZERO_RE) || raw.match(GOZERO_SHORT_RE)
  if (gzMatch) {
    time = gzMatch[1]
    level = gzMatch[2].toUpperCase()
    if (level === 'SLOW') level = 'WARN'
    const parts = gzMatch[3].split('\t')
    body = parts[0] || gzMatch[3]
    const innerMatch = body.match(WORKER_INNER_RE)
    if (innerMatch) {
      time = innerMatch[1]
      level = innerMatch[2].toUpperCase()
      body = innerMatch[5] || body
    }
    if (body.startsWith('[HTTP]')) {
      const statusMatch = body.match(/\[HTTP\]\s+(\d{3})/)
      if (statusMatch) {
        const code = parseInt(statusMatch[1])
        if (code >= 500) level = 'ERROR'
      }
    }
    return { stream: obj.stream || 'stdout', level, time: formatTimeShort(time), body, container: containerName, raw }
  }

  // 2) Redis
  const redisMatch = raw.match(REDIS_RE)
  if (redisMatch) {
    time = redisMatch[3]
    level = REDIS_LEVEL_MAP[redisMatch[4]] || 'INFO'
    body = redisMatch[5].trim()
    return { stream: obj.stream || 'stdout', level, time: formatTimeShort(time), body, container: containerName, raw }
  }

  // 3) MongoDB JSON
  if (raw.startsWith('{')) {
    try {
      const json = JSON.parse(raw)
      level = MONGO_SEVERITY_MAP[json.s] || 'INFO'
      const parts = []
      if (json.c) parts.push(`[${json.c}]`)
      if (json.ctx) parts.push(`(${json.ctx})`)
      if (json.msg) parts.push(json.msg)
      if (json.attr) {
        const attrStr = typeof json.attr === 'string' ? json.attr : JSON.stringify(json.attr)
        parts.push(attrStr.length <= 200 ? attrStr : attrStr.slice(0, 200) + '...')
      }
      body = parts.join(' ') || raw
      return { stream: obj.stream || 'stdout', level, time: formatTimeShort(time), body, container: containerName, raw }
    } catch (_) {}
  }

  // 4) nginx error
  const nginxErrMatch = raw.match(NGINX_ERROR_RE)
  if (nginxErrMatch) {
    time = nginxErrMatch[1]
    const lvl = nginxErrMatch[2].toLowerCase()
    level = lvl === 'error' || lvl === 'crit' || lvl === 'alert' || lvl === 'emerg' ? 'ERROR' : lvl === 'warn' ? 'WARN' : 'INFO'
    body = nginxErrMatch[5]
    return { stream: obj.stream || 'stderr', level, time: formatTimeShort(time), body, container: containerName, raw }
  }

  // 5) nginx access
  const nginxAccMatch = raw.match(NGINX_ACCESS_RE)
  if (nginxAccMatch) {
    time = nginxAccMatch[3]
    const code = parseInt(nginxAccMatch[7])
    level = code >= 500 ? 'ERROR' : code >= 400 ? 'WARN' : 'INFO'
    body = `${nginxAccMatch[4]} ${nginxAccMatch[5]} → ${code}`
    return { stream: obj.stream || 'stdout', level, time: formatTimeShort(time), body, container: containerName, raw }
  }

  // 6) Worker 内嵌格式
  const workerMatch = raw.match(WORKER_INNER_RE)
  if (workerMatch) {
    time = workerMatch[1]
    level = workerMatch[2].toUpperCase()
    body = workerMatch[5] || raw
    return { stream: obj.stream || 'stdout', level, time: formatTimeShort(time), body, container: containerName, raw }
  }

  // 7) Fallback - try to extract timestamp from beginning of line
  const tsMatch = raw.match(/^(\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?)/)
  if (tsMatch) {
    time = tsMatch[1]
    body = raw.substring(tsMatch[0].length).trim()
  }
  // 如果未从内容中提取到时间戳，保留 Docker 时间戳 (obj.ts)
  const lm = raw.match(LEVEL_RE)
  if (lm) level = lm[1].toUpperCase()
  return { stream: obj.stream || 'stdout', level, time: formatTimeShort(time), body: body || raw, container: containerName, raw }
}

function formatTimeShort(time) {
  if (!time) return ''
  // ISO 8601 / Docker: 2026-07-30T17:43:03.439+08:00 or 2026-07-29T15:59:52.439576466Z
  if (time.includes('T')) {
    const parts = time.split('T')
    if (parts.length > 1) {
      const timePart = parts[1].split('.')[0] || parts[1]
      const datePart = parts[0].substring(5) // MM-DD
      return `${datePart} ${timePart}`
    }
  }
  // nginx access log: "01/Aug/2026:09:12:07 +0800"
  const nginxMatch = time.match(/^(\d{2})\/(\w{3})\/(\d{4}):(\d{2}:\d{2}:\d{2})/)
  if (nginxMatch) {
    const nginxMonthMap = { Jan: '01', Feb: '02', Mar: '03', Apr: '04', May: '05', Jun: '06', Jul: '07', Aug: '08', Sep: '09', Oct: '10', Nov: '11', Dec: '12' }
    const mm = nginxMonthMap[nginxMatch[2]] || nginxMatch[2]
    return `${mm}-${nginxMatch[1]} ${nginxMatch[4]}`
  }
  // Redis format: "29 Jul 2026 15:59:52.265"
  const redisMatch = time.match(/^(\d{2})\s+(\w{3})\s+(\d{4})\s+(\d{2}:\d{2}:\d{2})/)
  if (redisMatch) {
    const monthMap = { Jan: '01', Feb: '02', Mar: '03', Apr: '04', May: '05', Jun: '06', Jul: '07', Aug: '08', Sep: '09', Oct: '10', Nov: '11', Dec: '12' }
    const mm = monthMap[redisMatch[2]] || redisMatch[2]
    return `${mm}-${redisMatch[1]} ${redisMatch[4]}`
  }
  // Space-separated ISO: "2026-07-29 15:59:52"
  const parts = time.split(' ')
  if (parts.length > 1) {
    const datePart = parts[0].length >= 10 ? parts[0].substring(5) : parts[0] // MM-DD
    return `${datePart} ${parts[1].split('.')[0]}`
  }
  return time
}

// ==================== 样式 ====================
function lineClass(l) {
  return {
    'log-stderr': l.stream === 'stderr',
    'log-error': l.level === 'ERROR' || l.level === 'FATAL' || l.level === 'PANIC',
    'log-warn': l.level === 'WARN',
    'log-debug': l.level === 'DEBUG'
  }
}

function levelClass(level) {
  if (!level) return ''
  const map = { ERROR: 'level-error', FATAL: 'level-error', PANIC: 'level-error', WARN: 'level-warn', SLOW: 'level-warn', INFO: 'level-info', DEBUG: 'level-debug', TRACE: 'level-debug', STAT: 'level-debug' }
  return map[level] || ''
}

// ==================== 滚动 ====================
function scrollToBottom() {
  const el = logBox.value
  if (el) el.scrollTop = el.scrollHeight
}

function onScroll() {
  const el = logBox.value
  if (!el) return
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60
  showScrollBtn.value = !atBottom
}

// ==================== 导出 ====================
function exportLogs(fmt) {
  const lines = filteredLines.value
  if (!lines.length) return
  let blob, filename
  const baseName = `${historyDate.value}_${historyContainer.value}`
  if (fmt === 'json') {
    blob = new Blob([JSON.stringify(lines, null, 2)], { type: 'application/json' })
    filename = `${baseName}.json`
  } else {
    blob = new Blob([lines.map(l => {
      const parts = [l.time, l.level ? `[${l.level}]` : '', l.body]
      return parts.filter(Boolean).join(' ')
    }).join('\n')], { type: 'text/plain' })
    filename = `${baseName}.txt`
  }
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

// ==================== 初始化 ====================
loadLogDates()
</script>

<style scoped lang="scss">
.container-logs-page {
  padding: 8px 12px;
}
.log-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.log-filters {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.filter-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  &:not(:last-child) {
    margin-bottom: 8px;
  }
}
.line-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.layout {
  display: flex;
  height: calc(100vh - 220px);
  min-height: 400px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  overflow: hidden;
}

/* ========== 右侧查看器 ========== */
.viewer {
  flex: 1;
  background: var(--el-bg-color);
  display: flex;
  flex-direction: column;
  position: relative;
  overflow: hidden;
}
.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--el-text-color-secondary);
  font-size: 14px;
}

/* ========== 历史模式信息栏 ========== */
.history-info {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 6px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-lighter);
  font-size: 12px;
}
.history-meta {
  color: var(--el-text-color-secondary);
}

/* ========== 日志内容区 ========== */
.log-box {
  flex: 1;
  overflow-y: auto;
  padding: 8px 0;
  font-family: 'Cascadia Code', 'JetBrains Mono', 'Consolas', 'Menlo', monospace;
  font-size: 13px;
  line-height: 1.9; /* increased from 1.8 */
  background: #1a1b26;
}
.log-line {
  display: flex;
  align-items: baseline;
  gap: 0;
  padding: 2px 12px 2px 0;
  transition: background 0.1s;
  &:nth-child(odd) { background: rgba(255, 255, 255, 0.02); }
  &:hover { background: rgba(255, 255, 255, 0.06); }
}
.log-ln {
  display: inline-block;
  width: 48px;
  min-width: 48px;
  text-align: right;
  padding-right: 10px;
  color: #565f89;
  font-size: 11px;
  user-select: none;
  flex-shrink: 0;
}
.log-level {
  display: inline-block;
  min-width: 48px;
  padding: 0 6px;
  margin-right: 6px;
  text-align: center;
  font-size: 10px;
  font-weight: 600;
  border-radius: 3px;
  flex-shrink: 0;
  letter-spacing: 0.5px;
}
.level-error { color: #fff; background: rgba(247, 118, 142, 0.8); }
.level-warn { color: #1a1b26; background: rgba(224, 175, 104, 0.85); }
.level-info { color: #9ece6a; background: rgba(158, 206, 106, 0.12); }
.level-debug { color: #565f89; background: rgba(86, 95, 137, 0.15); }
.log-time {
  color: #7aa2f7;
  font-size: 12px;
  margin-right: 8px;
  white-space: nowrap;
  flex-shrink: 0;
  min-width: 110px;
}
.log-body {
  color: #c0caf5;
  word-break: break-all;
  white-space: pre-wrap;
  flex: 1;
  min-width: 0;
}

/* stderr/level 整行着色 */
.log-stderr .log-body { color: #f7768e; }
.log-error .log-body { color: #f7768e; }
.log-warn .log-body { color: #e0af68; }
.log-debug .log-body { color: #565f89; }

/* ========== 滚动按钮 ========== */
.scroll-bottom-btn {
  position: absolute;
  bottom: 16px;
  right: 24px;
  z-index: 10;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 14px;
  border: 1px solid var(--el-border-color);
  border-radius: 16px;
  background: var(--el-bg-color);
  color: var(--el-text-color-regular);
  font-size: 12px;
  cursor: pointer;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
  transition: all 0.2s;
  &:hover {
    color: var(--el-color-primary);
    border-color: var(--el-color-primary);
  }
}

/* ========== 亮色模式覆盖 ========== */
:global(html:not(.dark) .log-box) { background: #f8f9fc; }
:global(html:not(.dark) .log-line:nth-child(odd)) { background: rgba(0, 0, 0, 0.02); }
:global(html:not(.dark) .log-line:hover) { background: rgba(0, 0, 0, 0.05); }
:global(html:not(.dark) .log-ln) { color: #9aa0b8; }
:global(html:not(.dark) .log-time) { color: #3b6ff5; }
:global(html:not(.dark) .log-level) { color: #fff; }
:global(html:not(.dark) .level-error) { color: #fff; background: #f56c6c; }
:global(html:not(.dark) .level-warn) { color: #fff; background: #e6a23c; }
:global(html:not(.dark) .level-info) { color: #2d7d2d; background: rgba(103, 194, 58, 0.15); }
:global(html:not(.dark) .level-debug) { color: #606266; background: rgba(144, 147, 153, 0.12); }
:global(html:not(.dark) .log-body) { color: #343b58; }
:global(html:not(.dark) .log-stderr .log-body),
:global(html:not(.dark) .log-error .log-body) { color: #c64343; }
:global(html:not(.dark) .log-warn .log-body) { color: #8f5e15; }
:global(html:not(.dark) .log-debug .log-body) { color: #909399; }
</style>
