<template>
  <div class="jsfinder-view">
    <ProTable
      ref="proTableRef"
      api="/jsfinder/list"
      
      
      rowKey="id"
      :columns="jsfinderColumns"
      :searchItems="jsfinderSearchItems"
      :extra-params="props.extraParams"
      :transform-payload="transformListPayload"

      
      selection
      :searchPlaceholder="$t('jsfinder.searchPlaceholder')"
      :searchKeys="['authority', 'url', 'vulName']"
      @data-changed="$emit('data-changed')"
    >
      <!-- 自定义导出 -->
      <template #toolbar-left>
        <!-- 批量AI研判按钮（仅JS模式显示） -->
        <div v-if="props.mode === 'js'" style="display: flex; align-items: center; margin-right: 8px;">
          <el-input-number
            v-model="aiConcurrency"
            :min="1"
            :max="5"
            :step="1"
            :disabled="batchAnalyzing"
            size="small"
            style="width: 90px; margin-right: 4px;"
            :title="$t('jsfinder.aiConcurrency')"
          />
          <el-button
            type="primary"
            :loading="batchAnalyzing && !batchTaskId"
            :disabled="batchAnalyzing"
            @click="handleBatchAnalyze"
          >
            {{ batchAnalyzing ? $t('jsfinder.aiAnalyzing') : $t('jsfinder.batchAIAnalyze') }}
          </el-button>
          <!-- 进度条 -->
          <el-progress
            v-if="batchAnalyzing && batchTaskId"
            :percentage="batchProgressPercent"
            :stroke-width="18"
            style="width: 200px; margin-left: 8px;"
            :text-inside="true"
            :status="batchProgressStatus"
          />
          <span v-if="batchAnalyzing && batchTaskId" style="margin-left: 8px; font-size: 13px; color: var(--el-text-color-regular);">
            {{ batchProcessed }}/{{ batchTotal }}
          </span>
          <!-- 停止按钮 -->
          <el-button
            v-if="batchAnalyzing && batchTaskId && batchStatus === 'running'"
            type="danger"
            size="small"
            plain
            style="margin-left: 8px;"
            @click="handleStopBatch"
          >
            {{ $t('jsfinder.stopBatchAnalyze') }}
          </el-button>
        </div>
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
        <el-button plain @click="proTableRef?.loadData()">
          <el-icon><Refresh /></el-icon>
          {{ $t('common.refresh') }}
        </el-button>
        <el-button type="danger" plain @click="handleClear">{{ $t('asset.clearData') || '清空数据' }}</el-button>
      </template>

      

      <!-- 严重程度 -->
      <template #severity="{ row }">
        <el-tag :type="getSeverityType(row.severity)" size="small">{{ getSeverityLabel(row.severity) }}</el-tag>
      </template>

      <!-- AI研判状态 -->
      <template #aiStatus="{ row }">
        <el-tag v-if="row.aiStatus === 'completed'" :type="row.aiResult === 'risk' ? 'danger' : 'success'" size="small">
          {{ row.aiResult === 'risk' ? $t('jsfinder.aiResultRisk') : $t('jsfinder.aiResultNoRisk') }}
        </el-tag>
        <el-tag v-else type="info" size="small">{{ $t('jsfinder.aiNotAnalyzed') }}</el-tag>
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

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
        <!-- 单条AI研判按钮（仅JS模式且未完成研判时显示） -->
        <el-button
          v-if="props.mode === 'js' && row.aiStatus !== 'completed'"
          type="success"
          link
          size="small"
          :loading="analyzingId === row.id"
          @click="handleSingleAnalyze(row)"
        >{{ $t('jsfinder.aiAnalyze') }}</el-button>
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
        <el-descriptions-item :label="$t('jsfinder.aiResult')" v-if="currentVul.aiStatus === 'completed'" :span="2">
          <el-tag :type="currentVul.aiResult === 'risk' ? 'danger' : 'success'" size="small">
            {{ currentVul.aiResult === 'risk' ? $t('jsfinder.aiResultRisk') : $t('jsfinder.aiResultNoRisk') }}
          </el-tag>
          <span v-if="currentVul.aiReason" style="margin-left: 12px; color: var(--el-text-color-regular);">
            {{ currentVul.aiReason }}
          </span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('jsfinder.aiAnalyzedAt')" v-if="currentVul.aiStatus === 'completed'">
          {{ currentVul.aiAnalyzedAt }}
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
              <div v-for="(item, idx) in currentVul.extractedResults" :key="idx" class="extracted-item">
                <el-tag size="small" type="info" style="margin-right: 8px;">#{{ idx + 1 }}</el-tag>
                <code class="extracted-content">{{ item }}</code>
              </div>
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
import { ref, computed, onUnmounted, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, Refresh } from '@element-plus/icons-vue'
import request from '@/api/request'
import { getJSFinderDetail, analyzeJSByAI, batchAnalyzeJSByAI, getBatchAnalyzeProgress, stopBatchAnalyze } from '@/api/jsfinder'
import ProTable from '@/components/common/ProTable.vue'

// localStorage key for batch task persistence
const BATCH_TASK_STORAGE_KEY = 'cscan_jsfinder_batch_task'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])
const props = defineProps({
  extraParams: {
    type: Object,
    default: () => ({})
  },
  // 组件模式: 'js'(JS菜单,默认) 或 'sensitive'(敏感信息页面)
  mode: {
    type: String,
    default: 'js'
  }
})

const proTableRef = ref(null)
const detailVisible = ref(false)
const currentVul = ref({})

const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

// AI研判相关状态
const analyzingId = ref('')
const batchAnalyzing = ref(false)
const aiConcurrency = ref(1)
const batchTaskId = ref('')
const batchTotal = ref(0)
const batchCompleted = ref(0)
const batchRiskCount = ref(0)
const batchNoRiskCount = ref(0)
const batchFailedCount = ref(0)
const batchStatus = ref('')
let batchTimer = null

// 已处理条数（成功+失败），用于进度显示
const batchProcessed = computed(() => batchCompleted.value + batchFailedCount.value)

// 进度百分比
const batchProgressPercent = computed(() => {
  if (batchTotal.value === 0) return 0
  return Math.min(100, Math.round((batchProcessed.value / batchTotal.value) * 100))
})

// 进度条状态颜色
const batchProgressStatus = computed(() => {
  if (batchStatus.value === 'completed') return 'success'
  if (batchStatus.value === 'failed') return 'exception'
  return ''
})

// localStorage持久化辅助函数
function saveBatchTaskToStorage() {
  if (batchTaskId.value && batchStatus.value === 'running') {
    const data = {
      taskId: batchTaskId.value,
      total: batchTotal.value,
      completed: batchCompleted.value,
      savedAt: Date.now()
    }
    localStorage.setItem(BATCH_TASK_STORAGE_KEY, JSON.stringify(data))
  } else {
    localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
  }
}

function loadBatchTaskFromStorage() {
  try {
    const raw = localStorage.getItem(BATCH_TASK_STORAGE_KEY)
    if (!raw) return null
    const data = JSON.parse(raw)
    // 超过2小时的任务视为过期（避免显示很久以前的失败任务）
    if (data.savedAt && Date.now() - data.savedAt > 2 * 60 * 60 * 1000) {
      localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
      return null
    }
    return data
  } catch {
    return null
  }
}

// 挂载时恢复未完成的批量任务
onMounted(() => {
  if (props.mode !== 'js') return
  const saved = loadBatchTaskFromStorage()
  if (saved && saved.taskId) {
    batchTaskId.value = saved.taskId
    batchTotal.value = saved.total || 0
    batchCompleted.value = saved.completed || 0
    batchStatus.value = 'running'
    batchAnalyzing.value = true
    // 立即查询一次当前进度，然后开始轮询
    startBatchPolling()
  }
})

const statLabels = computed(() => ({
  total: t('jsfinder.total'),
  critical: t('jsfinder.critical'),
  high: t('jsfinder.high'),
  medium: t('jsfinder.medium'),
  low: t('jsfinder.low'),
  info: t('jsfinder.info')
}))

const jsfinderColumns = computed(() => {
  const cols = [
    { label: t('jsfinder.vulName'), prop: 'vulName', minWidth: 200, showOverflowTooltip: true },
    { label: t('jsfinder.severity'), prop: 'severity', slot: 'severity', width: 100 },
  ]
  // JS模式显示AI研判状态列
  if (props.mode === 'js') {
    cols.push({ label: t('jsfinder.aiStatus'), prop: 'aiStatus', slot: 'aiStatus', width: 110 })
  }
  cols.push(
    { label: t('jsfinder.target'), prop: 'authority', minWidth: 150 },
    { label: 'URL', prop: 'url', minWidth: 250, showOverflowTooltip: true },
    { label: t('jsfinder.matcherName'), prop: 'matcherName', slot: 'matcherName', minWidth: 180, showOverflowTooltip: true },
    { label: t('jsfinder.tags'), prop: 'tags', slot: 'tags', minWidth: 150 },
    { label: t('jsfinder.discoveryTime'), prop: 'createTime', width: 160 },
    { label: t('common.updateTime'), prop: 'updateTime', width: 160 },
    { label: t('common.operation'), slot: 'operation', width: props.mode === 'js' ? 180 : 120, fixed: 'right' }
  )
  return cols
})

const jsfinderSearchItems = computed(() => {
  const items = [
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
  ]
  // JS模式保留标签过滤，敏感信息模式移除（由后端固定过滤aiResult=risk）
  if (props.mode === 'js') {
    // AI研判状态筛选（与列显示对齐：AI未研判/有风险/无风险）
    items.push({
      label: t('jsfinder.aiStatus'),
      prop: 'aiStatus',
      type: 'select',
      options: [
        { label: t('jsfinder.aiNotAnalyzed'), value: 'pending' },
        { label: t('jsfinder.aiRisk'), value: 'risk' },
        { label: t('jsfinder.aiNoRisk'), value: 'no_risk' }
      ]
    })
    items.push({
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
    })
  }
  items.push({
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
  })
  return items
})

// 列表请求参数转换：将前端 aiStatus 筛选值映射到后端的 aiStatus/aiResult 参数
// AI未研判 -> aiStatus=pending；有风险 -> aiResult=risk；无风险 -> aiResult=no_risk
// completed（研判已完成，来自敏感信息页固定过滤）保留原样，不得覆盖已有的 aiResult
function transformListPayload(payload) {
  if (payload.aiStatus === 'risk' || payload.aiStatus === 'no_risk') {
    payload.aiResult = payload.aiStatus
    delete payload.aiStatus
  }
  return payload
}

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
  'high-risk': { labelKey: 'jsfinder.tagHighRisk', type: 'danger' },
  'risk': { labelKey: 'jsfinder.tagRisk', type: 'warning' },
  'sensitive': { labelKey: 'jsfinder.tagSensitive', type: 'warning' },
  'info-leak': { labelKey: 'jsfinder.tagInfoLeak', type: 'info' },
  'unauth': { labelKey: 'jsfinder.tagUnauth', type: 'danger' },
  'js-file': { labelKey: 'jsfinder.tagJsFile', type: 'info' },
  'url': { labelKey: 'jsfinder.tagInfoLeak', type: 'info' },
  'absurl': { labelKey: 'jsfinder.tagInfoLeak', type: 'info' },
  'url-list': { labelKey: 'jsfinder.tagInfoLeak', type: 'info' },
  'absurl-list': { labelKey: 'jsfinder.tagInfoLeak', type: 'info' }
}

function getDisplayTags(tags) {
  if (!tags || !tags.length) return []
  return tags
    .filter(tag => tag !== 'jsfinder')
    .map(tag => {
      const mapped = riskTagMap[tag]
      if (mapped) {
        return { value: tag, label: t(mapped.labelKey), type: mapped.type }
      }
      return { value: tag, label: tag, type: 'info' }
    })
}

// 匹配规则详情映射
const matcherDetailMap = {
  'JS IPv4 Regex': { key: 'vul.matcherIpv4', regex: '\\b(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(?:\\.(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}\\b' },
  'JS Email Regex': { key: 'vul.matcherEmail', regex: '\\b[A-Za-z0-9._%+\\-]+@[A-Za-z0-9.\\-]+\\.[A-Za-z]{2,}\\b' },
  'JS Phone Number Regex': { key: 'vul.matcherPhone', regex: '\\b1[3-9][0-9]{9}\\b' },
  'JS ID Card Regex': { key: 'vul.matcherIdCard', regex: '\\b[1-9][0-9]{5}(?:19|20)[0-9]{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12][0-9]|3[01])[0-9]{3}[0-9Xx]\\b' },
  'JS JWT Token Regex': { key: 'vul.matcherIpv4', regex: 'eyJ[A-Za-z0-9_\\-]+\\.eyJ[A-Za-z0-9_\\-]+\\.[A-Za-z0-9_\\-]+' },
  'JS Hard-coded Secret Regex': { key: 'vul.matcherSecret', regex: '(?i)(access[_\\-]?key|api[_\\-]?key|secret[_\\-]?key|secret[_\\-]?token|app[_\\-]?key|app[_\\-]?secret|auth[_\\-]?token|access[_\\-]?token|client[_\\-]?secret|private[_\\-]?key|aws[_\\-]?secret)' },
  'JS Relative Path Regex': { key: 'vul.matcherRelPath', regex: '["\'`](\\/[a-zA-Z0-9_\\-/.?=&%~+#@:]{1,256})["\'`]' },
  'JS Absolute URL Regex': { key: 'vul.matcherAbsUrl', regex: 'https?://[a-zA-Z0-9._\\-]+(?::\\d+)?(?:/[a-zA-Z0-9_\\-/.?=&%~+#@:]*)?' },
  'JS Script Src Extractor': { key: 'vul.matcherIpv4', regex: '<script[^>]+src\\s*=\\s*["\']([^"\']+)["\']' },
  'JS API Unauth Check': { key: 'vul.matcherUnauth', keywords: 'Response-based keyword matching' },
  'JS Sensitive Keyword Detection': { key: 'vul.matcherSensitive', keywords: 'password, token, mobile, api_key, secret, phone, email, idcard, jwt, credit_card, AKID, AccessKeyId, etc.' }
}

// 默认敏感关键词列表
const defaultSensitiveKeywords = [
  'password', 'passwd', 'secret', 'token', 'access_token', 'refresh_token',
  'api_key', 'apikey', 'access_key', 'accesskey', 'secret_key', 'secretkey',
  'private_key', 'privatekey', 'client_secret', 'clientsecret',
  'AKID', 'AccessKeyId', 'SecretAccessKey',
  'phone', 'mobile', 'telephone',
  'idcard', 'id_card', 'identity_card',
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
      return `${t(detail.key)}\n${t('vul.matcherDetailRegex')}: ${detail.regex}`
    } else if (detail.keywords) {
      return `${t(detail.key)}\n${t('vul.matcherDetailKeywords')}: ${detail.keywords}`
    }
  }
  // 模糊匹配（包含关系）
  for (const key of Object.keys(matcherDetailMap)) {
    if (matcherName.includes(key) || key.includes(matcherName)) {
      const detail = matcherDetailMap[key]
      if (detail.regex) {
        return `${t(detail.key)}\n${t('vul.matcherDetailRegex')}: ${detail.regex}`
      } else if (detail.keywords) {
        return `${t(detail.key)}\n${t('vul.matcherDetailKeywords')}: ${detail.keywords}`
      }
    }
  }
  // 如果匹配名称是敏感关键词（如 password, token, mobile）
  if (defaultSensitiveKeywords.includes(matcherName.toLowerCase())) {
    return `${t('vul.matcherSensitive')}\n${t('vul.matcherDetailKeywords')}: ${matcherName}`
  }
  // 处理 "keyword:xxx" 格式的动态匹配规则
  if (matcherName.startsWith('keyword:')) {
    const keyword = matcherName.substring(8)
    return `${t('vul.matcherUnauth')}\n${t('vul.matcherDetailKeywords')}: ${keyword}`
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

async function showDetail(row) {
  // 列表查询已投影排除 request/response/curl_command 大字段，需要按需回填
  if (!row.request && !row.response && row.id) {
    try {
      const res = await getJSFinderDetail({
        id: row.id
      })
      if (res.code === 0 && res.data) {
        currentVul.value = res.data
      } else {
        currentVul.value = row
      }
    } catch {
      currentVul.value = row
    }
  } else {
    currentVul.value = row
  }
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
        ...proTableRef.value?.searchForm, page: 0, pageSize: 0
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
        ...proTableRef.value?.searchForm, page: 0, pageSize: 0
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

// ==================== AI研判相关逻辑 ====================

// 单条研判
async function handleSingleAnalyze(row) {
  analyzingId.value = row.id
  try {
    const res = await analyzeJSByAI({ id: row.id })
    if (res.code === 0 && res.data) {
      // 回填到当前行
      row.aiStatus = res.data.aiStatus
      row.aiResult = res.data.aiResult
      row.aiReason = res.data.aiReason
      row.aiAnalyzedAt = res.data.aiAnalyzedAt
      ElMessage.success(
        res.data.aiResult === 'risk'
          ? t('jsfinder.aiAnalyzedRisk')
          : t('jsfinder.aiAnalyzedNoRisk')
      )
    } else {
      ElMessage.error(res.msg || t('jsfinder.aiAnalyzeFailed'))
    }
  } catch (e) {
    console.error('AI研判失败:', e)
    ElMessage.error(t('jsfinder.aiAnalyzeFailed'))
  } finally {
    analyzingId.value = ''
  }
}

// 批量研判
async function handleBatchAnalyze() {
  // 确定批量研判的范围和确认文案
  let confirmMsg = t('jsfinder.batchAIAnalyzeConfirm')
  const params = { concurrency: aiConcurrency.value }

  const selected = selectedRows.value
  if (selected.length > 0) {
    // 模式1：有选中数据，研判选中的数据
    params.ids = selected.map(row => row.id)
    confirmMsg = t('jsfinder.batchAnalyzeSelectedConfirm', { count: selected.length })
  } else {
    // 模式2/3：获取当前筛选条件
    const currentSearch = proTableRef.value?.searchForm || {}
    const hasFilter = currentSearch.authority || currentSearch.severity || currentSearch.tags || currentSearch.matcherName || currentSearch.aiStatus
    if (hasFilter) {
      // 模式2：有筛选条件，研判符合条件的未研判数据
      if (currentSearch.authority) params.query = currentSearch.authority
      if (currentSearch.severity) params.severity = currentSearch.severity
      if (currentSearch.tags) params.tags = currentSearch.tags
      if (currentSearch.matcherName) params.matcherName = currentSearch.matcherName
      if (currentSearch.aiStatus) {
        // 筛选值与列显示对齐：pending→未研判, risk→有风险, no_risk→无风险
        if (currentSearch.aiStatus === 'pending') {
          params.aiStatus = 'pending'
        } else {
          params.aiResult = currentSearch.aiStatus // 'risk' 或 'no_risk'
        }
      }
      confirmMsg = t('jsfinder.batchAnalyzeFilteredConfirm')
    }
    // 模式3：无选中无筛选，研判所有未研判数据，使用默认文案
  }

  try {
    await ElMessageBox.confirm(
      confirmMsg,
      t('common.warning'),
      { type: 'warning', confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel') }
    )
  } catch {
    return
  }

  batchAnalyzing.value = true
  batchTotal.value = 0
  batchCompleted.value = 0
  batchRiskCount.value = 0
  batchNoRiskCount.value = 0
  batchFailedCount.value = 0
  batchStatus.value = 'running'
  try {
    const res = await batchAnalyzeJSByAI(params)
    if (res.code === 0) {
      if (res.total === 0) {
        ElMessage.info(t('jsfinder.noPendingData'))
        batchAnalyzing.value = false
        batchStatus.value = ''
        return
      }
      batchTaskId.value = res.taskId
      batchTotal.value = res.total
      // 持久化任务状态
      saveBatchTaskToStorage()
      ElMessage.success(t('jsfinder.batchTaskStarted', { total: res.total }))
      // 开始轮询进度
      startBatchPolling()
    } else {
      ElMessage.error(res.msg || t('jsfinder.batchStartFailed'))
      batchAnalyzing.value = false
      batchStatus.value = ''
    }
  } catch (e) {
    console.error('启动批量研判失败:', e)
    ElMessage.error(t('jsfinder.batchStartFailed'))
    batchAnalyzing.value = false
    batchStatus.value = ''
  }
}

// 轮询批量研判进度（每2秒查询一次）
function startBatchPolling() {
  if (batchTimer) clearInterval(batchTimer)
  batchTimer = setInterval(async () => {
    try {
      const res = await getBatchAnalyzeProgress({ taskId: batchTaskId.value })
      if (res.code !== 0) {
        // 任务不存在（如服务重启导致内存态丢失），停止轮询避免卡死
        clearInterval(batchTimer)
        batchTimer = null
        batchAnalyzing.value = false
        batchStatus.value = ''
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.warning(t('jsfinder.batchTaskLost'))
        return
      }
      batchCompleted.value = res.completed
      batchTotal.value = res.total
      batchRiskCount.value = res.riskCount || 0
      batchNoRiskCount.value = res.noRiskCount || 0
      batchFailedCount.value = res.failedCount || 0
      batchStatus.value = res.status
      // 进度变化时更新localStorage
      saveBatchTaskToStorage()
      if (res.status === 'completed') {
        clearInterval(batchTimer)
        batchTimer = null
        batchAnalyzing.value = false
        // 任务完成，清除持久化
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.success(t('jsfinder.batchAnalyzeDone', {
          completed: res.completed, total: res.total,
          risk: res.riskCount || 0, noRisk: res.noRiskCount || 0, failed: res.failedCount || 0
        }))
        proTableRef.value?.loadData()
        emit('data-changed')
      } else if (res.status === 'failed') {
        clearInterval(batchTimer)
        batchTimer = null
        batchAnalyzing.value = false
        // 任务失败（AI服务中断），清除持久化；未研判数据保持待研判，刷新列表
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.error(t('jsfinder.batchAnalyzeFailed', {
          completed: res.completed, total: res.total,
          risk: res.riskCount || 0, noRisk: res.noRiskCount || 0, failed: res.failedCount || 0,
          unprocessed: res.total - res.completed - (res.failedCount || 0)
        }))
        proTableRef.value?.loadData()
        emit('data-changed')
      } else if (res.status === 'stopped') {
        clearInterval(batchTimer)
        batchTimer = null
        batchAnalyzing.value = false
        // 任务已停止，清除持久化
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.warning(t('jsfinder.batchAnalyzeStopped', {
          completed: res.completed, total: res.total,
          risk: res.riskCount || 0, noRisk: res.noRiskCount || 0, failed: res.failedCount || 0
        }))
        proTableRef.value?.loadData()
        emit('data-changed')
      }
      // running/stopping状态继续轮询
    } catch (e) {
      // 忽略单次轮询错误
    }
  }, 2000)
}

// 停止批量研判
async function handleStopBatch() {
  try {
    await ElMessageBox.confirm(
      t('jsfinder.confirmStopBatch'),
      t('jsfinder.stopBatchAnalyze'),
      { confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel'), type: 'warning' }
    )
  } catch {
    return // 用户取消
  }

  try {
    const res = await stopBatchAnalyze({ taskId: batchTaskId.value })
    if (res.code === 0) {
      ElMessage.info(t('jsfinder.stopSignalSent'))
    } else {
      ElMessage.error(res.msg || t('jsfinder.stopFailed'))
    }
  } catch (e) {
    ElMessage.error(t('jsfinder.stopFailed'))
  }
}

// 组件卸载时清理定时器
onUnmounted(() => {
  if (batchTimer) clearInterval(batchTimer)
})

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

  .extracted-results {
    max-height: 300px;
    overflow-y: auto;
  }
  .extracted-item {
    display: block;
    margin-bottom: 6px;
    word-break: break-all;
    line-height: 1.6;
  }
  .extracted-content {
    background: var(--el-fill-color-light);
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 12px;
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
