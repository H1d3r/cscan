<template>
  <div class="jsfinder-view">
    <ProTable
      ref="proTableRef"
      api="/jsfinder/list"
      
      
      rowKey="id"
      :columns="jsfinderColumns"
      :searchItems="jsfinderSearchItems"
      
      
      selection
      :searchPlaceholder="$t('jsfinder.searchPlaceholder')"
      :searchKeys="['authority', 'url', 'vulName']"
      @data-changed="$emit('data-changed')"
    >
      <!-- 自定义导出 -->
      <template #toolbar-left>
        <el-dropdown @command="handleExport">
          <el-button type="success" size="default">
            {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="selected-url" :disabled="selectedRows.length === 0">{{ $t('jsfinder.exportSelectedUrls', { count: selectedRows.length }) }}</el-dropdown-item>
              <el-dropdown-item command="all-url">{{ $t('jsfinder.exportAllUrls') }}</el-dropdown-item>
              <el-dropdown-item command="csv">{{ $t('common.exportCsv') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>

      <template #toolbar-right>
        <el-button type="danger" plain @click="handleClear">{{ $t('asset.clearData') || '清空数据' }}</el-button>
      </template>

      

      <!-- 严重程度 -->
      <template #severity="{ row }">
        <el-tag :type="getSeverityType(row.severity)" size="small">{{ getSeverityLabel(row.severity) }}</el-tag>
      </template>

      <!-- 风险标签 -->
      <template #tags="{ row }">
        <template v-if="row.tags && row.tags.length">
          <el-tag
            v-for="tag in getDisplayTags(row.tags)"
            :key="tag.value"
            size="small"
            :type="tag.type"
            class="tag-item"
          >{{ tag.label }}</el-tag>
          <el-tag v-if="row.tags.length > 4" size="small" type="info">+{{ row.tags.length - 4 }}</el-tag>
        </template>
      </template>

      <!-- 匹配规则 -->
      <template #matcherName="{ row }">
        <span class="matcher-text">{{ row.matcherName || '-' }}</span>
      </template>

      <!-- 匹配内容 -->
      <template #extractedResults="{ row }">
        <template v-if="row.extractedResults && row.extractedResults.length">
          <el-tag
            v-for="(result, idx) in row.extractedResults.slice(0, 3)"
            :key="idx"
            size="small"
            type="warning"
            class="result-tag"
          >{{ truncateText(result, 40) }}</el-tag>
          <el-tag v-if="row.extractedResults.length > 3" size="small" type="info">+{{ row.extractedResults.length - 3 }}</el-tag>
        </template>
        <span v-else class="text-muted">-</span>
      </template>

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
        
      </template>
    </ProTable>

    <!-- 详情侧边栏 -->
    <el-drawer v-model="detailVisible" :title="$t('jsfinder.detailTitle')" size="50%" direction="rtl">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('jsfinder.vulName')" :span="2">{{ currentVul.vulName }}</el-descriptions-item>
        <el-descriptions-item :label="$t('jsfinder.severity')">
          <el-tag :type="getSeverityType(currentVul.severity)">{{ getSeverityLabel(currentVul.severity) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('jsfinder.target')">{{ currentVul.authority }}</el-descriptions-item>
        <el-descriptions-item label="URL" :span="2">
          <a :href="currentVul.url" target="_blank" rel="noopener" class="url-link">{{ currentVul.url }}</a>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('jsfinder.source')">{{ currentVul.source }}</el-descriptions-item>
        <el-descriptions-item :label="$t('jsfinder.discoveryTime')">{{ currentVul.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.updateTime')">{{ currentVul.updateTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('jsfinder.verifyResult')" :span="2">
          <pre class="result-pre">{{ currentVul.result }}</pre>
        </el-descriptions-item>
      </el-descriptions>

      <!-- 匹配规则与风险内容 -->
      <template v-if="currentVul.matcherName || (currentVul.extractedResults && currentVul.extractedResults.length)">
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
            <div class="extracted-results">
              <!-- 如果有上下文片段（extractedResults[1]），只高亮片段中的关键词 -->
              <template v-if="currentVul.extractedResults.length > 1">
                <div class="extracted-item" v-html="highlightKeyword(currentVul.extractedResults[1], currentVul.extractedResults[0])"></div>
              </template>
              <!-- 如果只有关键词，直接显示并高亮 -->
              <template v-else>
                <div class="extracted-item">
                  <mark class="highlight-inline">{{ currentVul.extractedResults[0] }}</mark>
                </div>
              </template>
            </div>
          </el-descriptions-item>
        </el-descriptions>
      </template>

      <!-- 风险标签 -->
      <template v-if="currentVul.tags && currentVul.tags.length">
        <el-divider content-position="left">{{ $t('jsfinder.riskTags') }}</el-divider>
        <div class="risk-tags-container">
          <el-tag
            v-for="tag in getDisplayTags(currentVul.tags)"
            :key="tag.value"
            :type="tag.type"
            class="risk-tag"
          >{{ tag.label }}</el-tag>
        </div>
      </template>

      <!-- 证据链 -->
      <template v-if="currentVul.evidence || (currentVul.matcherName || (currentVul.extractedResults && currentVul.extractedResults.length))">
        <el-divider content-position="left">{{ $t('jsfinder.evidence') }}</el-divider>
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="$t('jsfinder.requestContent')" v-if="currentVul.request">
            <pre class="result-pre">{{ currentVul.request }}</pre>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('jsfinder.responseContent')" v-if="currentVul.response">
            <pre class="result-pre" v-html="highlightExtracted(currentVul.response, currentVul.extractedResults)"></pre>
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
  total: t('jsfinder.total'),
  critical: t('jsfinder.critical'),
  high: t('jsfinder.high'),
  medium: t('jsfinder.medium'),
  low: t('jsfinder.low'),
  info: t('jsfinder.info')
}))

const jsfinderColumns = computed(() => [
  { label: t('jsfinder.vulName'), prop: 'vulName', minWidth: 200, showOverflowTooltip: true },
  { label: t('jsfinder.severity'), prop: 'severity', slot: 'severity', width: 100 },
  { label: t('jsfinder.target'), prop: 'authority', minWidth: 150 },
  { label: 'URL', prop: 'url', minWidth: 250, showOverflowTooltip: true },
  { label: t('jsfinder.matcherName'), prop: 'matcherName', slot: 'matcherName', minWidth: 180, showOverflowTooltip: true },
  { label: t('jsfinder.extractedResults'), prop: 'extractedResults', slot: 'extractedResults', minWidth: 200 },
  { label: t('jsfinder.tags'), prop: 'tags', slot: 'tags', minWidth: 150 },
  { label: t('jsfinder.discoveryTime'), prop: 'createTime', width: 160 },
  { label: t('common.operation'), slot: 'operation', width: 120, fixed: 'right' }
])

const jsfinderSearchItems = computed(() => [
  { label: t('jsfinder.target'), prop: 'authority', type: 'input', placeholder: t('jsfinder.targetPlaceholder') },
  {
    label: t('jsfinder.severity'),
    prop: 'severity',
    type: 'select',
    options: [
      { label: t('jsfinder.critical'), value: 'critical' },
      { label: t('jsfinder.high'), value: 'high' },
      { label: t('jsfinder.medium'), value: 'medium' },
      { label: t('jsfinder.low'), value: 'low' },
      { label: t('jsfinder.info'), value: 'info' },
      { label: t('jsfinder.unknown'), value: 'unknown' }
    ]
  },
  {
    label: t('jsfinder.riskTag'),
    prop: 'tags',
    type: 'select',
    options: [
      { label: t('jsfinder.tagHighRisk'), value: 'high-risk' },
      { label: t('jsfinder.tagRisk'), value: 'risk' },
      { label: t('jsfinder.tagSensitive'), value: 'sensitive' },
      { label: t('jsfinder.tagInfoLeak'), value: 'info-leak' },
      { label: t('jsfinder.tagUnauth'), value: 'unauth' },
      { label: t('jsfinder.tagJsFile'), value: 'js-file' }
    ]
  },
  {
    label: t('jsfinder.matcherName'),
    prop: 'matcherName',
    type: 'select',
    options: [
      { label: 'IPv4', value: 'JS IPv4 Regex' },
      { label: t('jsfinder.matcherEmail'), value: 'JS Email Regex' },
      { label: t('jsfinder.matcherPhone'), value: 'JS Phone Number Regex' },
      { label: t('jsfinder.matcherIdCard'), value: 'JS ID Card Regex' },
      { label: 'JWT Token', value: 'JS JWT Token Regex' },
      { label: t('jsfinder.matcherSecret'), value: 'JS Hard-coded Secret Regex' },
      { label: t('jsfinder.matcherRelPath'), value: 'JS Relative Path Regex' },
      { label: t('jsfinder.matcherAbsUrl'), value: 'JS Absolute URL Regex' },
      { label: 'Script Src', value: 'JS Script Src Extractor' },
      { label: t('jsfinder.matcherUnauth'), value: 'JS API Unauth Check' },
      { label: t('jsfinder.matcherSensitive'), value: 'JS Sensitive Keyword Detection' }
    ]
  }
])

function getSeverityType(severity) {
  const map = { critical: 'danger', high: 'danger', medium: 'warning', low: 'info', info: 'info', unknown: 'info' }
  return map[severity] || 'info'
}

function getSeverityLabel(severity) {
  const map = {
    critical: t('jsfinder.critical'),
    high: t('jsfinder.high'),
    medium: t('jsfinder.medium'),
    low: t('jsfinder.low'),
    info: t('jsfinder.info'),
    unknown: t('jsfinder.unknown')
  }
  return map[severity] || severity
}

// 标签显示逻辑
const riskTagMap = {
  'high-risk': { label: '高危风险', type: 'danger', en: 'High Risk' },
  'risk': { label: '风险', type: 'warning', en: 'Risk' },
  'sensitive': { label: '敏感信息', type: 'warning', en: 'Sensitive' },
  'info-leak': { label: '信息泄漏', type: 'info', en: 'Info Leak' },
  'unauth': { label: '未授权', type: 'danger', en: 'Unauth' },
  'js-file': { label: 'JS文件', type: 'info', en: 'JS File' },
  'url': { label: 'API路径', type: 'info', en: 'API Path' },
  'absurl': { label: '绝对URL', type: 'info', en: 'Absolute URL' },
  'url-list': { label: 'API路径', type: 'info', en: 'API Paths' },
  'absurl-list': { label: 'URL清单', type: 'info', en: 'URL List' }
}

function getDisplayTags(tags) {
  if (!tags || !tags.length) return []
  return tags
    .filter(tag => tag !== 'jsfinder')
    .map(tag => {
      const mapped = riskTagMap[tag]
      if (mapped) {
        return { value: tag, label: mapped.label, type: mapped.type }
      }
      return { value: tag, label: tag, type: 'info' }
    })
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
  // 敏感关键词检测（命中的关键词会显示在这里）
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
  // 处理 "keyword:xxx" 格式的动态匹配规则
  if (matcherName.startsWith('keyword:')) {
    const keyword = matcherName.substring(8)
    return `Response-based keyword matching\n命中关键词: ${keyword}`
  }
  return ''
}

function truncateText(text, maxLen) {
  if (!text) return ''
  return text.length > maxLen ? text.substring(0, maxLen) + '...' : text
}

// 对文本中的匹配内容进行高亮处理
// 只对 extractedResults[0]（关键词本身）进行高亮，保留上下文片段
function highlightExtracted(text, extractedResults) {
  if (!text) return ''
  // 先转义 HTML 特殊字符，防止 XSS
  let escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')

  if (!extractedResults || !extractedResults.length) return escaped

  // 只使用第一个元素（关键词）进行高亮匹配
  const keyword = extractedResults[0]
  if (!keyword || !keyword.trim()) return escaped

  // 用占位符替换关键词（大小写不敏感）
  const placeholders = []
  const escapedKeyword = keyword
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
  
  // 使用正则表达式进行大小写不敏感匹配
  const regex = new RegExp(escapeRegexChars(escapedKeyword), 'gi')
  escaped = escaped.replace(regex, (matchStr) => {
    const placeholder = `\x00HIGHLIGHT_${placeholders.length}\x00`
    placeholders.push(matchStr)
    return placeholder
  })

  // 将占位符替换为高亮 HTML
  for (let i = 0; i < placeholders.length; i++) {
    const placeholder = `\x00HIGHLIGHT_${i}\x00`
    escaped = escaped.replace(placeholder, `<mark class="highlight-mark">${placeholders[i]}</mark>`)
  }

  return escaped
}

// 在片段中高亮关键词（用于匹配内容区域）
function highlightKeyword(text, keyword) {
  if (!text) return ''
  // 转义 HTML 特殊字符
  let escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')

  if (!keyword || !keyword.trim()) return escaped

  // 转义关键词中的特殊字符
  const escapedKeyword = keyword
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')

  // 使用正则表达式进行大小写不敏感匹配并高亮
  const regex = new RegExp(escapeRegexChars(escapedKeyword), 'gi')
  escaped = escaped.replace(regex, (matchStr) => {
    return `<mark class="highlight-inline">${matchStr}</mark>`
  })

  return escaped
}

// 转义正则表达式特殊字符
function escapeRegexChars(str) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function showDetail(row) {
  currentVul.value = row
  detailVisible.value = true
}





async function handleExport(command) {
  let data = []
  let filename = ''

  if (command === 'selected-url') {
    if (selectedRows.value.length === 0) {
      ElMessage.warning(t('jsfinder.pleaseSelect'))
      return
    }
    data = selectedRows.value
    filename = 'jsfinder_urls_selected.txt'
  } else if (command === 'csv') {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/jsfinder/list', {
        ...proTableRef.value?.searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
    } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }

    if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

    const headers = ['VulName', 'Severity', 'Target', 'URL', 'MatcherName', 'ExtractedResults', 'Tags', 'CreateTime', 'UpdateTime']
    const csvRows = [headers.join(',')]
    for (const row of data) {
      csvRows.push([
        escapeCsvField(row.vulName || ''),
        escapeCsvField(row.severity || ''),
        escapeCsvField(row.authority || ''),
        escapeCsvField(row.url || ''),
        escapeCsvField(row.matcherName || ''),
        escapeCsvField((row.extractedResults || []).join(';')),
        escapeCsvField((row.tags || []).join(';')),
        escapeCsvField(row.createTime || ''),
        escapeCsvField(row.updateTime || '')
      ].join(','))
    }
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `jsfinder_results_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link); link.click(); document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(t('asset.exportSuccess', { count: data.length }))
    return
  } else {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/jsfinder/list', {
        ...proTableRef.value?.searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
    } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }
    filename = 'jsfinder_urls_all.txt'
  }

  if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

  const seen = new Set()
  const exportData = []
  for (const row of data) {
    if (row.url && !seen.has(row.url)) { seen.add(row.url); exportData.push(row.url) }
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

async function handleClear() {
  try {
    await ElMessageBox.confirm(t('jsfinder.confirmClearAll'), t('common.warning'), { type: 'error', confirmButtonText: t('jsfinder.confirmClearBtn'), cancelButtonText: t('common.cancel') || '取消' })
    const res = await request.post('/jsfinder/clear')
    if (res.code === 0) {
      ElMessage.success(t('jsfinder.clearSuccess'))
      proTableRef.value?.loadData()
      emit('data-changed')
    } else {
      ElMessage.error(res.msg || t('jsfinder.clearFailed'))
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('清空JSFinder结果失败:', e)
      ElMessage.error(t('jsfinder.clearFailed'))
    }
  }
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

defineExpose({ refresh })
</script>

<style scoped lang="scss">
.jsfinder-view {
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

  .matcher-text {
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 12px;
    color: var(--el-color-primary);
  }

  .result-tag {
    margin: 2px 4px 2px 0;
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .text-muted {
    color: var(--el-text-color-placeholder);
  }

  .url-link {
    color: #409eff;
    text-decoration: none;
    font-family: monospace;
    &:hover { text-decoration: underline; }
  }

  .extracted-results {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .extracted-item {
    display: inline-block;
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

  .risk-tags-container {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .risk-tag {
    font-size: 13px;
  }
}
</style>
