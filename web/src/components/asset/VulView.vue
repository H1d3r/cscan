<template>
  <div class="vul-view">
    <ProTable
      ref="proTableRef"
      api="/vul/list"
      statApi="/vul/stat"
      batchDeleteApi="/vul/batchDelete"
      rowKey="id"
      :columns="vulColumns"
      :searchItems="vulSearchItems"
      :statLabels="statLabels"
      selection
      :searchPlaceholder="$t('vul.targetPlaceholder')"
      :searchKeys="['authority', 'url', 'pocFile', 'vulName']"
      @data-changed="$emit('data-changed')"
    >
      <!-- 自定义导出（5种命令） -->
      <template #toolbar-left>
        <el-dropdown @command="handleExport">
          <el-button type="success" size="default">
            {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="selected-target" :disabled="selectedRows.length === 0">{{ $t('vul.exportSelectedTargets', { count: selectedRows.length }) }}</el-dropdown-item>
              <el-dropdown-item command="selected-url" :disabled="selectedRows.length === 0">{{ $t('vul.exportSelectedUrls', { count: selectedRows.length }) }}</el-dropdown-item>
              <el-dropdown-item divided command="all-target">{{ $t('vul.exportAllTargets') }}</el-dropdown-item>
              <el-dropdown-item command="all-url">{{ $t('vul.exportAllUrls') }}</el-dropdown-item>
              <el-dropdown-item command="csv">{{ $t('common.exportCsv') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>

      <template #toolbar-right>
        <el-button type="danger" plain @click="handleClear">{{ $t('vul.clearData') }}</el-button>
      </template>

      <!-- 严重程度 -->
      <template #severity="{ row }">
        <el-tag :type="getSeverityType(row.severity)" size="small">{{ getSeverityLabel(row.severity) }}</el-tag>
      </template>

      <!-- POC标签 -->
      <template #tags="{ row }">
        <template v-if="row.tags && row.tags.length">
          <el-tag v-for="tag in row.tags.slice(0, 3)" :key="tag" size="small" class="tag-item">{{ tag }}</el-tag>
          <el-tag v-if="row.tags.length > 3" size="small" type="info">+{{ row.tags.length - 3 }}</el-tag>
        </template>
      </template>

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
        <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
      </template>
    </ProTable>

    <!-- 详情侧边栏 -->
    <el-drawer v-model="detailVisible" :title="$t('vul.vulDetail')" size="50%" direction="rtl">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('vul.vulName')" :span="2">{{ currentVul.vulName }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.severity')">
          <el-tag :type="getSeverityType(currentVul.severity)">{{ getSeverityLabel(currentVul.severity) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('vul.target')">{{ currentVul.authority }}</el-descriptions-item>
        <el-descriptions-item label="URL" :span="2">{{ currentVul.url }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.pocFile')" :span="2">{{ currentVul.pocFile }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.tags')" :span="2" v-if="currentVul.tags && currentVul.tags.length">
          <el-tag v-for="tag in currentVul.tags" :key="tag" size="small" class="tag-item">{{ tag }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('vul.source')">{{ currentVul.source }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.discoveryTime')">{{ currentVul.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.updateTime')">{{ currentVul.updateTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.verifyResult')" :span="2">
          <pre class="result-pre">{{ currentVul.result }}</pre>
        </el-descriptions-item>
      </el-descriptions>
      <!-- JSFinder 专属：匹配规则与风险内容 -->
      <template v-if="currentVul.source === 'jsfinder' && (currentVul.matcherName || (currentVul.extractedResults && currentVul.extractedResults.length))">
        <el-divider content-position="left">{{ $t('jsfinder.matchRuleAndRisk') }}</el-divider>
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="$t('jsfinder.matcherName')" v-if="currentVul.matcherName">
            <div class="matcher-detail">
              <div class="matcher-name">
                <el-tag type="primary" size="small" effect="dark">{{ currentVul.matcherName }}</el-tag>
              </div>
              <div v-if="getMatcherDetail(currentVul.matcherName)" class="matcher-description">
                <span class="matcher-label">正则:</span>
                <code class="matcher-regex">{{ getMatcherDetail(currentVul.matcherName) }}</code>
              </div>
            </div>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('jsfinder.extractedResults')" v-if="currentVul.extractedResults && currentVul.extractedResults.length">
            <div style="display:flex;flex-wrap:wrap;gap:6px">
              <mark v-for="(r, idx) in currentVul.extractedResults" :key="idx" class="highlight-inline">{{ r }}</mark>
            </div>
          </el-descriptions-item>
        </el-descriptions>
      </template>
      <!-- JSFinder 专属：风险标签 -->
      <template v-if="currentVul.source === 'jsfinder' && currentVul.tags && currentVul.tags.length">
        <el-divider content-position="left">{{ $t('jsfinder.riskTags') }}</el-divider>
        <div style="display:flex;flex-wrap:wrap;gap:8px">
          <el-tag v-for="tag in currentVul.tags.filter(t => t !== 'jsfinder')" :key="tag" :type="getJsfinderTagType(tag)" size="default">{{ getJsfinderTagLabel(tag) }}</el-tag>
        </div>
      </template>
      <!-- 证据区块（通用 + JSFinder 证据） -->
      <template v-if="currentVul.evidence || (currentVul.source === 'jsfinder' && (currentVul.matcherName || (currentVul.extractedResults && currentVul.extractedResults.length)))">
        <el-divider content-position="left">{{ $t('vul.evidence') }}</el-divider>
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="$t('vul.requestContent')" v-if="currentVul.evidence?.request">
            <pre class="result-pre">{{ currentVul.evidence.request }}</pre>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('vul.responseContent')" v-if="currentVul.evidence?.response">
            <pre class="result-pre" v-html="highlightExtracted(currentVul.evidence.response, currentVul.source === 'jsfinder' ? currentVul.extractedResults : null)"></pre>
          </el-descriptions-item>
        </el-descriptions>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import request from '@/api/request'
import ProTable from '@/components/common/ProTable.vue'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])

const proTableRef = ref(null)
const detailVisible = ref(false)
const currentVul = ref({})

const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

const statLabels = computed(() => ({
  total: t('vul.totalVuls'),
  critical: t('vul.critical'),
  high: t('vul.high'),
  medium: t('vul.medium'),
  low: t('vul.low'),
  info: t('vul.info')
}))

const vulColumns = computed(() => [
  { label: t('vul.vulName'), prop: 'vulName', minWidth: 200, showOverflowTooltip: true },
  { label: t('vul.severity'), prop: 'severity', slot: 'severity', width: 100 },
  { label: t('vul.target'), prop: 'authority', minWidth: 150 },
  { label: 'URL', prop: 'url', minWidth: 250, showOverflowTooltip: true },
  { label: 'POC', prop: 'pocFile', minWidth: 200, showOverflowTooltip: true },
  { label: t('vul.tags'), prop: 'tags', slot: 'tags', minWidth: 150 },
  { label: t('vul.source'), prop: 'source', width: 100 },
  { label: t('vul.discoveryTime'), prop: 'createTime', width: 160 },
  { label: t('common.updateTime'), prop: 'updateTime', width: 160 },
  { label: t('common.operation'), slot: 'operation', width: 120, fixed: 'right' }
])

const vulSearchItems = computed(() => [
  { label: t('vul.target'), prop: 'authority', type: 'input', placeholder: t('vul.targetPlaceholder') },
  {
    label: t('vul.severity'),
    prop: 'severity',
    type: 'select',
    options: [
      { label: t('vul.critical'), value: 'critical' },
      { label: t('vul.high'), value: 'high' },
      { label: t('vul.medium'), value: 'medium' },
      { label: t('vul.low'), value: 'low' },
      { label: t('vul.info'), value: 'info' },
      { label: t('vul.unknown'), value: 'unknown' }
    ]
  },
  {
    label: t('vul.source'),
    prop: 'source',
    type: 'select',
    options: [
      { label: 'Nuclei', value: 'nuclei' },
      { label: 'JSFinder', value: 'jsfinder' }
    ]
  }
])

function getSeverityType(severity) {
  const map = { critical: 'danger', high: 'danger', medium: 'warning', low: 'info', info: 'info', unknown: 'info' }
  return map[severity] || 'info'
}

function getSeverityLabel(severity) {
  const map = {
    critical: t('vul.critical'),
    high: t('vul.high'),
    medium: t('vul.medium'),
    low: t('vul.low'),
    info: t('vul.info'),
    unknown: t('vul.unknown')
  }
  return map[severity] || severity
}

async function showDetail(row) {
  try {
    const res = await request.post('/vul/detail', { id: row.id })
    currentVul.value = res.code === 0 && res.data ? res.data : row
  } catch (e) { currentVul.value = row }
  detailVisible.value = true
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('vul.confirmDeleteVul'), t('common.tip'), { type: 'warning' })
    const res = await request.post('/vul/delete', { id: row.id })
    if (res.code === 0) {
      ElMessage.success(t('common.deleteSuccess'))
      proTableRef.value?.loadData()
      emit('data-changed')
    }
  } catch (e) {
    // cancelled
  }
}

async function handleClear() {
  try {
    await ElMessageBox.confirm(t('vul.confirmClearAll'), t('common.warning'), {
      type: 'error',
      confirmButtonText: t('vul.confirmClearBtn'),
      cancelButtonText: t('common.cancel')
    })
    const res = await request.post('/vul/clear', {})
    if (res.code === 0) {
      ElMessage.success(res.msg || t('vul.clearSuccess'))
      proTableRef.value?.loadData()
      emit('data-changed')
    } else {
      ElMessage.error(res.msg || t('vul.clearFailed'))
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('清空漏洞失败:', e)
    }
  }
}

async function handleExport(command) {
  let data = []
  let filename = ''

  if (command === 'selected-target' || command === 'selected-url') {
    if (selectedRows.value.length === 0) {
      ElMessage.warning(t('vul.pleaseSelectVuls'))
      return
    }
    data = selectedRows.value
    filename = command === 'selected-target' ? 'vul_targets_selected.txt' : 'vul_urls_selected.txt'
  } else if (command === 'csv') {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/vul/list', {
        ...proTableRef.value?.searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
    } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }

    if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

    const headers = ['VulName', 'Severity', 'Target', 'URL', 'POC', 'Tags', 'Source', 'Result', 'CreateTime', 'UpdateTime']
    const csvRows = [headers.join(',')]
    for (const row of data) {
      csvRows.push([
        escapeCsvField(row.vulName || ''),
        escapeCsvField(row.severity || ''),
        escapeCsvField(row.authority || ''),
        escapeCsvField(row.url || ''),
        escapeCsvField(row.pocFile || ''),
        escapeCsvField((row.tags || []).join(';')),
        escapeCsvField(row.source || ''),
        escapeCsvField(row.result || ''),
        escapeCsvField(row.createTime || ''),
        escapeCsvField(row.updateTime || '')
      ].join(','))
    }
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `vulnerabilities_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link); link.click(); document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(t('asset.exportSuccess', { count: data.length }))
    return
  } else {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/vul/list', {
        ...proTableRef.value?.searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
    } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }
    filename = command === 'all-target' ? 'vul_targets_all.txt' : 'vul_urls_all.txt'
  }

  if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

  const seen = new Set()
  const exportData = []
  if (command.includes('target')) {
    for (const row of data) {
      if (row.authority && !seen.has(row.authority)) { seen.add(row.authority); exportData.push(row.authority) }
    }
  } else {
    for (const row of data) {
      if (row.url && !seen.has(row.url)) { seen.add(row.url); exportData.push(row.url) }
    }
  }
  if (exportData.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url; link.download = filename
  document.body.appendChild(link); link.click(); document.body.removeChild(link)
  URL.revokeObjectURL(url)
  ElMessage.success(t('asset.exportSuccess', { count: exportData.length }))
}

function escapeCsvField(field) {
  if (field == null) return ''
  const str = String(field)
  if (str.includes(',') || str.includes('"') || str.includes('\n') || str.includes('\r')) {
    return '"' + str.replace(/"/g, '""') + '"'
  }
  return str
}

function refresh() {
  proTableRef.value?.loadData()
}

const jsfinderTagMap = {
  'high-risk': { label: '高危风险', en: 'High Risk', type: 'danger' },
  'risk': { label: '风险', en: 'Risk', type: 'warning' },
  'sensitive': { label: '敏感信息', en: 'Sensitive', type: 'warning' },
  'info-leak': { label: '信息泄漏', en: 'Info Leak', type: 'info' },
  'unauth': { label: '未授权', en: 'Unauthorized', type: 'danger' },
  'js-file': { label: 'JS文件', en: 'JS File', type: '' },
  'url-list': { label: 'API路径', en: 'API Paths', type: '' },
  'absurl-list': { label: 'URL清单', en: 'URL List', type: '' }
}

// 匹配规则详情映射（中文描述 + 正则表达式）
const matcherDetailMap = {
  // JS IPv4 地址提取
  'JS IPv4 Regex': {
    zh: 'JS 内嵌 IPv4 地址正则',
    en: 'Extract IPv4 Addresses from JS',
    regex: '\\b(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(?:\\.(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}\\b'
  },
  // JS 邮箱提取
  'JS Email Regex': {
    zh: 'JS 内嵌邮箱地址正则',
    en: 'Extract Email Addresses from JS',
    regex: '\\b[A-Za-z0-9._%+\\-]+@[A-Za-z0-9.\\-]+\\.[A-Za-z]{2,}\\b'
  },
  // JS 手机号提取
  'JS Phone Number Regex': {
    zh: 'JS 内嵌手机号正则（中国大陆）',
    en: 'Extract Phone Numbers from JS',
    regex: '\\b1[3-9][0-9]{9}\\b'
  },
  // JS 身份证号提取
  'JS ID Card Regex': {
    zh: 'JS 内嵌身份证号正则',
    en: 'Extract ID Card Numbers from JS',
    regex: '\\b[1-9][0-9]{5}(?:19|20)[0-9]{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12][0-9]|3[01])[0-9]{3}[0-9Xx]\\b'
  },
  // JS JWT Token 提取
  'JS JWT Token Regex': {
    zh: 'JS 内嵌 JWT Token 正则',
    en: 'Extract JWT Tokens from JS',
    regex: 'eyJ[A-Za-z0-9_\\-]+\\.eyJ[A-Za-z0-9_\\-]+\\.[A-Za-z0-9_\\-]+'
  },
  // JS 硬编码密钥提取
  'JS Hard-coded Secret Regex': {
    zh: 'JS 硬编码密钥正则',
    en: 'Extract Hard-coded Secrets from JS',
    regex: '(?i)(access[_\\-]?key|api[_\\-]?key|secret[_\\-]?key|secret[_\\-]?token|app[_\\-]?key|app[_\\-]?secret|auth[_\\-]?token|access[_\\-]?token|client[_\\-]?secret|private[_\\-]?key|aws[_\\-]?secret)'
  },
  // JS 相对路径提取
  'JS Relative Path Regex': {
    zh: 'JS 相对路径/API 提取正则',
    en: 'Extract Relative Paths and APIs from JS',
    regex: '["\'`](\\/[a-zA-Z0-9_\\-/.?=&%~+#@:]{1,256})["\'`]'
  },
  // JS 绝对 URL 提取
  'JS Absolute URL Regex': {
    zh: 'JS 绝对 URL 提取正则',
    en: 'Extract Absolute URLs from JS',
    regex: 'https?://[a-zA-Z0-9._\\-]+(?::\\d+)?(?:/[a-zA-Z0-9_\\-/.?=&%~+#@:]*)?'
  },
  // JS Script 标签提取
  'JS Script Src Extractor': {
    zh: 'JS Script 标签 src 属性提取',
    en: 'Extract Script Tag src Attributes',
    regex: '<script[^>]+src\\s*=\\s*["\']([^"\']+)["\']'
  },
  // 未授权访问检测
  'JS API Unauth Check': {
    zh: 'JS API 未授权访问检测',
    en: 'Unauthenticated API Access Detection',
    keywords: 'Response-based keyword matching'
  },
  // 敏感关键词检测
  'JS Sensitive Keyword Detection': {
    zh: '敏感关键词检测',
    en: 'Sensitive Keyword Detection',
    keywords: 'password, token, mobile, api_key, secret, phone, email, idcard, jwt, credit_card, AKID, AccessKeyId, etc.'
  }
}

// 默认敏感关键词列表
const defaultSensitiveKeywords = [
  'password', 'passwd', 'secret', 'token', 'access_token', 'refresh_token',
  'api_key', 'apikey', 'access_key', 'accesskey', 'secret_key', 'secretkey',
  'private_key', 'privatekey', 'client_secret', 'clientsecret',
  'AKID', 'AccessKeyId', 'SecretAccessKey',
  'phone', 'mobile', 'telephone',
  'idcard', 'id_card', 'identity_card', '身份证',
  'email', 'mail',
  'openid', 'unionid',
  'jwt', 'bearer',
  'credit_card', 'creditcard', 'cvv',
  'ssn', 'passport'
]

// 获取匹配规则详情
function getMatcherDetail(matcherName) {
  if (!matcherName) return ''
  // 精确匹配
  if (matcherDetailMap[matcherName]) {
    const detail = matcherDetailMap[matcherName]
    if (detail.regex) {
      return `${detail.zh} (${detail.en})\n正则: ${detail.regex}`
    } else if (detail.keywords) {
      return `${detail.zh} (${detail.en})\n关键词: ${detail.keywords}`
    }
  }
  // 模糊匹配（包含关系）
  for (const key of Object.keys(matcherDetailMap)) {
    if (matcherName.includes(key) || key.includes(matcherName)) {
      const detail = matcherDetailMap[key]
      if (detail.regex) {
        return `${detail.zh} (${detail.en})\n正则: ${detail.regex}`
      } else if (detail.keywords) {
        return `${detail.zh} (${detail.en})\n关键词: ${detail.keywords}`
      }
    }
  }
  // 如果匹配名称是敏感关键词（如 password, token, mobile）
  if (defaultSensitiveKeywords.includes(matcherName.toLowerCase())) {
    return `敏感关键词检测 (Sensitive Keyword)\n命中关键词: ${matcherName}`
  }
  return ''
}

function getJsfinderTagType(tag) {
  return jsfinderTagMap[tag]?.type || ''
}

function getJsfinderTagLabel(tag) {
  const mapped = jsfinderTagMap[tag]
  if (mapped) {
    const locale = t('jsfinder.tag' + tag.split('-').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(''))
    return locale !== ('jsfinder.tag' + tag.split('-').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join('')) ? locale : mapped.label
  }
  return tag
}

// 对文本中的匹配内容进行高亮处理
function highlightExtracted(text, extractedResults) {
  if (!text) return ''
  // 先转义 HTML 特殊字符，防止 XSS
  let escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')

  if (!extractedResults || !extractedResults.length) return escaped

  // 按长度降序排列，优先匹配更长的关键词
  const sorted = [...extractedResults]
    .filter(r => r && r.trim())
    .sort((a, b) => b.length - a.length)

  if (sorted.length === 0) return escaped

  // 用占位符替换避免重叠
  const placeholders = []
  for (const keyword of sorted) {
    const escapedKeyword = keyword
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
    const idx = escaped.indexOf(escapedKeyword)
    if (idx !== -1) {
      const placeholder = `\x00HIGHLIGHT_${placeholders.length}\x00`
      escaped = escaped.replace(escapedKeyword, placeholder)
      placeholders.push(escapedKeyword)
    }
  }

  // 将占位符替换为高亮 HTML
  for (let i = 0; i < placeholders.length; i++) {
    const placeholder = `\x00HIGHLIGHT_${i}\x00`
    escaped = escaped.replace(placeholder, `<mark class="highlight-mark">${placeholders[i]}</mark>`)
  }

  return escaped
}

defineExpose({ refresh })
</script>

<style scoped lang="scss">
.vul-view {
  height: 100%;

  .result-pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 300px;
    overflow: auto;
    background: var(--code-bg);
    color: var(--code-text);
    padding: 12px;
    border-radius: 6px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 13px;
    line-height: 1.5;
  }

  .tag-item {
    margin-right: 4px;
    margin-bottom: 2px;
  }

  .highlight-mark {
    background-color: #e6a23c;
    color: #fff;
    padding: 2px 6px;
    border-radius: 3px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 12px;
    font-weight: 600;
  }

  .highlight-inline {
    background-color: #e6a23c;
    color: #fff;
    padding: 2px 6px;
    border-radius: 3px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 12px;
    font-weight: 600;
  }

  .result-pre :deep(.highlight-mark) {
    background-color: #e6a23c;
    color: #fff;
    padding: 1px 3px;
    border-radius: 2px;
    font-weight: 600;
  }

  .matcher-highlight {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .matcher-detail {
    display: flex;
    flex-direction: column;
    gap: 8px;

    .matcher-name {
      display: flex;
      align-items: center;
    }

    .matcher-description {
      display: flex;
      align-items: flex-start;
      gap: 8px;
      padding: 8px;
      background: hsl(var(--muted) / 0.3);
      border-radius: 4px;
      font-size: 12px;

      .matcher-label {
        color: hsl(var(--muted-foreground));
        font-weight: 500;
        flex-shrink: 0;
      }

      .matcher-regex {
        font-family: 'Consolas', 'Monaco', monospace;
        color: hsl(var(--foreground));
        word-break: break-all;
        background: hsl(var(--card));
        padding: 2px 6px;
        border-radius: 3px;
        border: 1px solid hsl(var(--border));
      }
    }
  }
}
</style>
