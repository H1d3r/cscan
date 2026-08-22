<template>
  <div class="vul-view">
    <!-- T4.3 快速筛选 -->
    <div class="vul-filter-tabs">
      <el-radio-group v-model="activeFilter" size="default" @change="onFilterChange">
        <el-radio-button value="all">{{ t('vul.filterAll') }}</el-radio-button>
        <el-radio-button value="new">{{ t('vul.filterNew') }}</el-radio-button>
        <el-radio-button value="critical">{{ t('vul.critical') }}</el-radio-button>
        <el-radio-button value="high">{{ t('vul.high') }}</el-radio-button>
        <el-radio-button value="medium">{{ t('vul.medium') }}</el-radio-button>
        <el-radio-button value="open">{{ t('vul.statusOpen') }}</el-radio-button>
        <el-radio-button value="fixed">{{ t('vul.filterFixed') }}</el-radio-button>
        <el-radio-button value="pending">{{ t('vul.pending') }}</el-radio-button>
        <el-radio-button value="ignored">{{ t('vul.statusIgnored') }}</el-radio-button>
      </el-radio-group>
    </div>

    <ProTable
      ref="proTableRef"
      api="/vul/list"
      statApi="/vul/stat"
      batchDeleteApi="/vul/batchDelete"
      rowKey="id"
      :columns="vulColumns"
      :searchItems="vulSearchItems"
      :statLabels="statLabels"
      :extraParams="mergedExtraParams"
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
        <el-button plain @click="proTableRef?.loadData()">
          <el-icon><RefreshRight /></el-icon>
          {{ $t('common.refresh') }}
        </el-button>
        <el-button type="danger" plain @click="handleClear">{{ $t('vul.clearData') }}</el-button>
      </template>

      <!-- 漏洞名称（含新发现标记） -->
      <template #vulName="{ row }">
        <div class="vul-name-cell">
          <el-tag v-if="isNewlyFound(row)" type="danger" size="small" effect="dark" round class="new-vul-tag">
            {{ t('vul.filterNew') }}
          </el-tag>
          <span class="vul-name-text">{{ row.vulName }}</span>
        </div>
      </template>

      <!-- 严重程度 -->
      <template #severity="{ row }">
        <el-tag :type="getSeverityType(row.severity)" size="small">{{ getSeverityLabel(row.severity) }}</el-tag>
      </template>

      <!-- 生命周期状态（T1.3） -->
      <template #status="{ row }">
        <el-tag :type="getStatusType(row.status)" size="small">{{ getStatusLabel(row.status) }}</el-tag>
      </template>

      <!-- 复验待确认标记（T3.3）：目标不可达时置位 -->
      <template #verifyPending="{ row }">
        <el-tag v-if="row.verifyPending" type="warning" size="small">{{ t('vul.pending') }}</el-tag>
        <span v-else style="color: var(--el-text-color-secondary)">-</span>
      </template>

      <!-- 复验状态（单条复验闭环） -->
      <template #reverifyStatus="{ row }">
        <el-tag v-if="reverifyingMap[row.id] || row.reverifyStatus === 'reverifying'" type="warning" size="small" effect="light" style="display:inline-flex;align-items:center;white-space:nowrap">
          <el-icon class="is-loading" style="margin-right:4px"><Loading /></el-icon>{{ t('vul.reverifyReverifying') }}
        </el-tag>
        <template v-else-if="row.reverifyStatus === 'done' && row.reverifyConclusion">
          <el-tag :type="getReverifyConclusionType(row.reverifyConclusion)" size="small">
            {{ getReverifyConclusionLabel(row.reverifyConclusion) }}
          </el-tag>
        </template>
        <span v-else style="color: var(--el-text-color-secondary)">-</span>
      </template>

      <!-- POC标签 -->
      <template #tags="{ row }">
        <template v-if="row.tags && row.tags.length">
          <el-tag v-for="tag in row.tags.slice(0, 3)" :key="tag" size="small" class="tag-item">{{ tag }}</el-tag>
          <el-tag v-if="row.tags.length > 3" size="small" type="info">+{{ row.tags.length - 3 }}</el-tag>
        </template>
      </template>

      <!-- 操作：核心按钮 + 更多下拉 -->
      <template #operation="{ row }">
        <div class="operation-cell">
          <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
          <el-button type="success" link size="small" @click="handleMarkFixed(row)" v-if="row.status !== 'fixed'">{{ $t('vul.markFixed') }}</el-button>
          <el-button type="info" link size="small" @click="handleReopen(row)" v-if="row.status === 'fixed' || row.status === 'ignored'">{{ $t('vul.reopen') }}</el-button>
          <el-dropdown trigger="click" @command="(cmd) => handleOperation(cmd, row)">
            <el-button link size="small" type="primary">
              {{ $t('common.more') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item v-if="row.status !== 'ignored'" command="markIgnored">
                  <el-icon><CircleClose /></el-icon>{{ $t('vul.markIgnored') }}
                </el-dropdown-item>
                <el-dropdown-item command="reverify">
                  <el-icon><RefreshRight /></el-icon>{{ $t('vul.reverify') }}
                </el-dropdown-item>
                <el-dropdown-item command="delete" divided>
                  <span class="dropdown-danger"><el-icon><Delete /></el-icon>{{ $t('common.delete') }}</span>
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
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
        <el-descriptions-item :label="$t('vul.discoveryTime')">{{ currentVul.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.updateTime')">{{ currentVul.updateTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.status')">
          <el-tag :type="getStatusType(currentVul.status)" size="small">{{ getStatusLabel(currentVul.status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('vul.fixedAt')" v-if="currentVul.fixedAt">{{ currentVul.fixedAt }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.fixConfirmSource')" v-if="currentVul.fixConfirmSource">{{ currentVul.fixConfirmSource }}</el-descriptions-item>
        <!-- 复验信息（单条复验闭环） -->
        <el-descriptions-item :label="$t('vul.reverifyStatus')">
          <el-tag v-if="reverifyingMap[currentVul.id] || currentVul.reverifyStatus === 'reverifying'" type="warning" size="small" effect="light" style="display:inline-flex;align-items:center;white-space:nowrap">
            <el-icon class="is-loading" style="margin-right:4px"><Loading /></el-icon>{{ $t('vul.reverifyReverifying') }}
          </el-tag>
          <el-tag v-else-if="currentVul.reverifyStatus === 'done' && currentVul.reverifyConclusion" :type="getReverifyConclusionType(currentVul.reverifyConclusion)" size="small">
            {{ getReverifyConclusionLabel(currentVul.reverifyConclusion) }}
          </el-tag>
          <span v-else style="color: var(--el-text-color-secondary)">-</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('vul.reverifyAt')" v-if="currentVul.reverifyAt">{{ currentVul.reverifyAt }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.reverifyBy')" v-if="currentVul.reverifyBy">{{ currentVul.reverifyBy }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.reverifyMessage')" :span="2" v-if="currentVul.reverifyMessage">{{ currentVul.reverifyMessage }}</el-descriptions-item>
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
import { ref, computed, reactive, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, Loading, CircleClose, RefreshRight, Delete } from '@element-plus/icons-vue'
import request from '@/api/request'
import { updateVulStatus } from '@/api/asset'
import { reverifyVul } from '@/api/vul'
import { getWorkerList } from '@/api/task'
import ProTable from '@/components/common/ProTable.vue'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])

// 允许父页面（如敏感信息/敏感目录页）注入固定过滤参数：
// { isRisk: true, riskSource: 'auto:info-leak', keywordAny: [...] }
const props = defineProps({
  extraParams: {
    type: Object,
    default: () => ({})
  }
})

const proTableRef = ref(null)
const detailVisible = ref(false)
const currentVul = ref({})

// 单条复验：记录正在复验中的漏洞 id（用于列表与详情的状态展示）
const reverifyingMap = reactive({})
const reverifyTimers = {}

// T4.3: 快速筛选状态
const activeFilter = ref('all')
// 新发现窗口（天），与 dashboard/changes 默认窗口一致
const NEW_WINDOW_DAYS = 7

// T4.3: 筛选 tab → 下发参数（与工作台风险变化卡片口径一致）
const filterParams = computed(() => {
  switch (activeFilter.value) {
    case 'new':
      return { isNew: true }
    case 'critical':
      return { severity: 'critical' }
    case 'high':
      return { severity: 'high' }
    case 'medium':
      return { severity: 'medium' }
    case 'open':
      return { status: 'open' }
    case 'fixed':
      return { status: 'fixed' }
    case 'pending':
      return { verifyPending: true }
    case 'ignored':
      return { status: 'ignored' }
    default:
      return {}
  }
})

// T4.3: 合并父级固定过滤 + 快速筛选 + 默认严重度排序（不破坏敏感信息页既有过滤）
const mergedExtraParams = computed(() => ({
  ...props.extraParams,
  ...filterParams.value,
  sort: 'severity'
}))

function onFilterChange() {
  // extraParams 变化会触发 ProTable 重新拉取
  proTableRef.value?.loadData()
}

// T4.3: 判断某行是否为"新发现"（first_seen_time 在窗口内，与 riskNewInWindow 口径一致）
function isNewlyFound(row) {
  if (!row || !row.firstSeenTime) return false
  const t = new Date(String(row.firstSeenTime).replace(' ', 'T'))
  if (isNaN(t.getTime())) return false
  const cutoff = Date.now() - NEW_WINDOW_DAYS * 24 * 3600 * 1000
  return t.getTime() >= cutoff
}

const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

const statLabels = computed(() => ({
  total: t('vul.totalVuls'),
  critical: t('vul.critical'),
  high: t('vul.high'),
  medium: t('vul.medium'),
  low: t('vul.low'),
  info: t('vul.info'),
  open: t('vul.statusOpen'),
  fixed: t('vul.statusFixed'),
  ignored: t('vul.statusIgnored')
}))

const vulColumns = computed(() => [
  { label: t('vul.vulName'), prop: 'vulName', slot: 'vulName', minWidth: 220, showOverflowTooltip: false },
  { label: t('vul.severity'), prop: 'severity', slot: 'severity', width: 90 },
  { label: t('vul.status'), prop: 'status', slot: 'status', width: 90 },
  { label: t('vul.firstSeen'), prop: 'firstSeenTime', width: 160, showOverflowTooltip: false },
  { label: t('vul.target'), prop: 'authority', minWidth: 150 },
  { label: 'URL', prop: 'url', minWidth: 250, showOverflowTooltip: false },
  { label: t('vul.discoveryTime'), prop: 'createTime', width: 160, showOverflowTooltip: false },
  { label: t('vul.lastVerifiedAt'), prop: 'lastVerifiedAt', width: 160, showOverflowTooltip: false },
  { label: t('vul.reverifyStatus'), prop: 'reverifyStatus', slot: 'reverifyStatus', width: 150 },
  { label: t('common.updateTime'), prop: 'updateTime', width: 160, showOverflowTooltip: false },
  { label: t('common.operation'), slot: 'operation', width: 170, fixed: 'right' }
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
    label: t('vul.status'),
    prop: 'status',
    type: 'select',
    options: [
      { label: t('vul.statusOpen'), value: 'open' },
      { label: t('vul.statusFixed'), value: 'fixed' },
      { label: t('vul.statusIgnored'), value: 'ignored' }
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

// T1.3：生命周期状态展示
function getStatusType(status) {
  const map = { open: 'danger', fixed: 'success', ignored: 'info' }
  return map[status] || 'danger' // 缺失 status 视为 open
}

function getStatusLabel(status) {
  const map = {
    open: t('vul.statusOpen'),
    fixed: t('vul.statusFixed'),
    ignored: t('vul.statusIgnored')
  }
  return map[status] || t('vul.statusOpen') // 缺失 status 视为 open
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

// T1.3：漏洞生命周期状态变更（open / fixed / ignored）
async function handleMarkFixed(row) {
  await changeVulStatus([row.id], 'fixed')
}

async function handleMarkIgnored(row) {
  await changeVulStatus([row.id], 'ignored')
}

async function handleReopen(row) {
  await changeVulStatus([row.id], 'open')
}

function handleOperation(cmd, row) {
  const actions = {
    markIgnored: handleMarkIgnored,
    reverify: handleReverify,
    delete: handleDelete
  }
  actions[cmd]?.(row)
}

async function changeVulStatus(ids, status) {
  try {
    const res = await updateVulStatus({ ids, status })
    if (res.code === 0) {
      ElMessage.success(t('vul.statusUpdated', { count: res.updated }))
      proTableRef.value?.loadData()
      emit('data-changed')
    } else {
      ElMessage.error(res.msg || t('vul.statusUpdateFailed'))
    }
  } catch (e) {
    ElMessage.error(t('vul.statusUpdateFailed'))
  }
}

// 单条漏洞复验：下发复验任务到 worker，并轮询结果形成闭环
async function handleReverify(row) {
  try {
    await ElMessageBox.confirm(t('vul.confirmReverify'), t('vul.reverify'), { type: 'warning' })
  } catch (e) {
    return // 用户取消
  }

  // 检查是否有在线 Worker
  try {
    const workerRes = await getWorkerList()
    const workerData = workerRes.data || workerRes
    const onlineWorkers = (workerData.list || []).filter(w => w.status === 'running')
    if (onlineWorkers.length === 0) {
      ElMessage.warning(t('vul.noWorkerOnline'))
      return
    }
  } catch (e) {
    ElMessage.warning(t('vul.noWorkerOnline'))
    return
  }

  try {
    const res = await reverifyVul({ ids: [row.id] })
    if (res.code !== 0) {
      ElMessage.error(res.msg || t('vul.reverifyFail'))
      return
    }
    ElMessage.success(t('vul.reverifyStarted'))
    startReverifyPoll(row)
  } catch (e) {
    ElMessage.error(t('vul.reverifyFail'))
  }
}

// 启动复验结果轮询：每 3 秒查询一次漏洞详情，直到复验完成
function startReverifyPoll(row) {
  const id = row.id
  reverifyingMap[id] = true
  const poll = async () => {
    try {
      const res = await request.post('/vul/detail', { id })
      const data = res.code === 0 && res.data ? res.data : null
      if (data && data.reverifyStatus === 'done') {
        // 复验完成，合并最新数据并刷新列表
        if (detailVisible.value && currentVul.value.id === id) {
          Object.assign(currentVul.value, data)
        }
        ElMessage.success(t('vul.reverifyDone', {
          conclusion: getReverifyConclusionLabel(data.reverifyConclusion)
        }))
        stopReverifyPoll(id)
        proTableRef.value?.loadData()
        emit('data-changed')
      } else {
        // 仍在进行中，继续轮询
        reverifyTimers[id] = setTimeout(poll, 3000)
      }
    } catch (e) {
      // 查询失败时停止轮询，避免死循环
      stopReverifyPoll(id)
    }
  }
  reverifyTimers[id] = setTimeout(poll, 3000)
}

function stopReverifyPoll(id) {
  if (reverifyTimers[id]) {
    clearTimeout(reverifyTimers[id])
    delete reverifyTimers[id]
  }
  delete reverifyingMap[id]
}

function getReverifyConclusionType(conclusion) {
  const map = {
    fixed: 'success',
    still_vuln: 'danger',
    unreachable: 'warning',
    reachable_untested: 'info'
  }
  return map[conclusion] || 'info'
}

function getReverifyConclusionLabel(conclusion) {
  const map = {
    fixed: t('vul.reverifyConclusionFixed'),
    still_vuln: t('vul.reverifyConclusionStillVuln'),
    unreachable: t('vul.reverifyConclusionUnreachable'),
    reachable_untested: t('vul.reverifyConclusionUntested')
  }
  return map[conclusion] || conclusion
}

onBeforeUnmount(() => {
  // 组件卸载时清理复验轮询定时器
  Object.values(reverifyTimers).forEach((timer) => clearTimeout(timer))
})

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

    const headers = ['VulName', 'Severity', 'Target', 'URL', 'POC', 'Tags', 'Status', 'Result', 'CreateTime', 'UpdateTime']
    const csvRows = [headers.join(',')]
    for (const row of data) {
      csvRows.push([
        escapeCsvField(row.vulName || ''),
        escapeCsvField(row.severity || ''),
        escapeCsvField(row.authority || ''),
        escapeCsvField(row.url || ''),
        escapeCsvField(row.pocFile || ''),
        escapeCsvField((row.tags || []).join(';')),
        escapeCsvField(row.status || ''),
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
  'high-risk': { labelKey: 'vul.tagHighRisk', type: 'danger' },
  'risk': { labelKey: 'vul.tagRisk', type: 'warning' },
  'sensitive': { labelKey: 'vul.tagSensitive', type: 'warning' },
  'info-leak': { labelKey: 'vul.tagInfoLeak', type: 'info' },
  'unauth': { labelKey: 'vul.tagUnauth', type: 'danger' },
  'js-file': { labelKey: 'vul.tagJsFile', type: '' },
  'url-list': { labelKey: 'vul.tagUrlList', type: '' },
  'absurl-list': { labelKey: 'vul.tagAbsurlList', type: '' }
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
  return ''
}

function getJsfinderTagType(tag) {
  return jsfinderTagMap[tag]?.type || ''
}

function getJsfinderTagLabel(tag) {
  const mapped = jsfinderTagMap[tag]
  if (mapped) {
    return t(mapped.labelKey)
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

  .vul-filter-tabs {
    margin-bottom: 16px;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
  }

  .vul-name-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: nowrap;
  }

  .vul-name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .new-vul-tag {
    flex-shrink: 0;
    font-weight: 600;
    padding: 0 8px;
    height: 20px;
    line-height: 18px;
  }

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

  .operation-cell {
    display: flex;
    align-items: center;
    gap: 4px;
    white-space: nowrap;
  }

  // 鼠标悬停右侧固定操作栏时，将其层级提升到 table tooltip 之上，
  // 确保能优先捕获鼠标事件并隐藏 tooltip，保护操作按钮不被遮挡。
  // Element Plus 2.4 用 sticky 列（el-table-fixed-column--right），旧版用 .el-table__fixed-right。
  :deep(.el-table-fixed-column--right), :deep(.el-table__fixed-right) {
    &:hover {
      z-index: 9999 !important;
    }
  }

  .dropdown-danger {
    color: var(--el-color-danger);
    display: flex;
    align-items: center;
    gap: 6px;
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
