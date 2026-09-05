<template>
  <div class="dirscan-view">
    <ProTable
      ref="proTableRef"
      api="/dirscan/result/list"
      statApi="/dirscan/result/stat"
      rowKey="id"
      :columns="dirColumns"
      :searchItems="dirSearchItems"
      :statLabels="statLabels"
      :extra-params="props.extraParams"
      :transform-payload="transformListPayload"
      :sync-url="props.syncUrl"
      :searchPlaceholder="$t('dirscan.targetPlaceholder')"
      :searchKeys="['authority']"
      selection
      @data-changed="$emit('data-changed')"
    >
      <!-- 工具栏左侧：批量AI研判+导出 -->
      <template #toolbar-left>
        <div v-if="enableAI" style="display: flex; align-items: center; margin-right: 8px;">
          <el-input-number
            v-model="aiConcurrency"
            :min="1"
            :max="5"
            :step="1"
            :disabled="batchAnalyzing"
            size="small"
            style="width: 90px; margin-right: 4px;"
            :title="$t('dirscan.aiConcurrency')"
          />
          <el-button
            type="primary"
            :loading="batchAnalyzing && !batchTaskId"
            :disabled="batchAnalyzing"
            @click="handleBatchAnalyze"
          >
            {{ batchAnalyzing ? $t('dirscan.aiAnalyzing') : $t('dirscan.batchAIAnalyze') }}
          </el-button>
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
          <el-button
            v-if="batchAnalyzing && batchTaskId && batchStatus === 'running'"
            type="danger"
            size="small"
            plain
            style="margin-left: 8px;"
            @click="handleStopBatch"
          >
            {{ $t('dirscan.stopBatchAnalyze') }}
          </el-button>
        </div>
        <el-dropdown @command="handleExport">
          <el-button type="success" size="default">
            {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="selected-url" :disabled="selectedRows.length === 0">{{ $t('dirscan.exportSelectedUrls', { count: selectedRows.length }) }}</el-dropdown-item>
              <el-dropdown-item command="all-url">{{ $t('dirscan.exportAllUrl') }}</el-dropdown-item>
              <el-dropdown-item command="csv">{{ $t('dirscan.exportCsv') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>

      <template #toolbar-right>
        <el-button plain @click="proTableRef?.loadData()">
          <el-icon><Refresh /></el-icon>
          {{ $t('common.refresh') }}
        </el-button>
        <el-button type="danger" plain @click="handleClear">{{ $t('dirscan.clearData') }}</el-button>
      </template>

      <!-- URL -->
      <template #url="{ row }">
        <a :href="row.url" target="_blank" rel="noopener" class="url-link">{{ row.url }}</a>
      </template>

      <!-- 状态码 -->
      <template #statusCode="{ row }">
        <el-tag :type="getStatusType(row.statusCode)" size="small">{{ row.statusCode }}</el-tag>
      </template>

      <!-- 大小 -->
      <template #contentLength="{ row }">
        {{ formatSize(row.contentLength) }}
      </template>

      <!-- 耗时 -->
      <template #duration="{ row }">
        {{ row.duration ? row.duration + 'ms' : '-' }}
      </template>

      <!-- AI研判状态 -->
      <template #aiStatus="{ row }">
        <el-tag v-if="row.aiStatus === 'completed'" :type="row.aiResult === 'risk' ? 'danger' : 'success'" size="small">
          {{ row.aiResult === 'risk' ? $t('dirscan.aiResultRisk') : $t('dirscan.aiResultNoRisk') }}
        </el-tag>
        <el-tag v-else type="info" size="small">{{ $t('dirscan.aiNotAnalyzed') }}</el-tag>
      </template>

      <!-- 发现时间 -->
      <template #createTime="{ row }">
        {{ formatTime(row.createTime) }}
      </template>

      <!-- 更新时间 -->
      <template #updateTime="{ row }">
        {{ formatTime(row.updateTime || row.scanTime) }}
      </template>

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
        <el-button
          v-if="enableAI && row.aiStatus !== 'completed'"
          type="success"
          link
          size="small"
          :loading="analyzingId === row.id"
          @click="handleSingleAnalyze(row)"
        >{{ $t('dirscan.aiAnalyze') }}</el-button>
        <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
      </template>
    </ProTable>

    <!-- 详情侧边栏 -->
    <el-drawer v-model="detailVisible" :title="$t('dirscan.detailTitle')" size="50%" direction="rtl">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('dirscan.path')" :span="2">{{ currentDetail.path }}</el-descriptions-item>
        <el-descriptions-item label="URL" :span="2">
          <a :href="currentDetail.url" target="_blank" rel="noopener" class="url-link">{{ currentDetail.url }}</a>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('dirscan.statusCode')">
          <el-tag :type="getStatusType(currentDetail.statusCode)">{{ currentDetail.statusCode }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('dirscan.target')">{{ currentDetail.authority }}</el-descriptions-item>
        <el-descriptions-item :label="$t('dirscan.contentType')">{{ currentDetail.contentType || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('dirscan.size')">{{ formatSize(currentDetail.contentLength) }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.contentWords')">{{ currentDetail.contentWords || 0 }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.contentLines')">{{ currentDetail.contentLines || 0 }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.duration')">{{ currentDetail.duration ? currentDetail.duration + 'ms' : '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('dirscan.title')" :span="2">{{ currentDetail.title || '-' }}</el-descriptions-item>
        <el-descriptions-item v-if="currentDetail.redirectUrl" :label="$t('dirscan.redirectUrl')" :span="2">{{ currentDetail.redirectUrl }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.createTime')">{{ currentDetail.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.updateTime')">{{ currentDetail.updateTime || currentDetail.scanTime }}</el-descriptions-item>
        <el-descriptions-item v-if="enableAI && currentDetail.aiStatus === 'completed'" :label="$t('dirscan.aiResult')" :span="2">
          <el-tag :type="currentDetail.aiResult === 'risk' ? 'danger' : 'success'" size="small">
            {{ currentDetail.aiResult === 'risk' ? $t('dirscan.aiResultRisk') : $t('dirscan.aiResultNoRisk') }}
          </el-tag>
          <span v-if="currentDetail.aiReason" style="margin-left: 12px; color: var(--el-text-color-regular);">
            {{ currentDetail.aiReason }}
          </span>
        </el-descriptions-item>
        <el-descriptions-item v-if="enableAI && currentDetail.aiStatus === 'completed'" :label="$t('dirscan.aiAnalyzedAt')">
          {{ currentDetail.aiAnalyzedAt }}
        </el-descriptions-item>
      </el-descriptions>

      <!-- 请求/响应内容 -->
      <template v-if="currentDetail.response || currentDetail.request">
        <el-divider content-position="left">{{ $t('dirscan.requestResponse') }}</el-divider>
        <el-descriptions :column="1" border>
          <el-descriptions-item v-if="currentDetail.request" :label="$t('dirscan.requestContent')">
            <pre class="result-pre">{{ currentDetail.request }}</pre>
          </el-descriptions-item>
          <el-descriptions-item v-if="currentDetail.response" :label="$t('dirscan.responseContent')">
            <pre class="result-pre">{{ currentDetail.response }}</pre>
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
import {
  getDirScanDetail,
  analyzeDirByAI,
  batchAnalyzeDirByAI,
  getBatchAnalyzeProgress as getDirBatchProgress,
  stopBatchAnalyze as stopDirBatchAnalyze
} from '@/api/dirscan'
import ProTable from '@/components/common/ProTable.vue'
import { fetchAllPages } from '@/utils/pagedRequest'

const BATCH_TASK_STORAGE_KEY = 'cscan_dirscan_batch_task'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])
const props = defineProps({
  extraParams: {
    type: Object,
    default: () => ({})
  },
  syncUrl: {
    type: Boolean,
    default: true
  },
  // 是否启用AI研判功能（默认true；敏感目录页面可传false）
  enableAI: {
    type: Boolean,
    default: true
  }
})

const proTableRef = ref(null)
const detailVisible = ref(false)
const currentDetail = ref({})
const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

// AI研判状态
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

const batchProgressPercent = computed(() => {
  if (batchTotal.value === 0) return 0
  return Math.min(100, Math.round((batchProcessed.value / batchTotal.value) * 100))
})
const batchProgressStatus = computed(() => {
  if (batchStatus.value === 'completed') return 'success'
  if (batchStatus.value === 'failed') return 'exception'
  return ''
})

function saveBatchTaskToStorage() {
  if (batchTaskId.value && batchStatus.value === 'running') {
    localStorage.setItem(BATCH_TASK_STORAGE_KEY, JSON.stringify({
      taskId: batchTaskId.value,
      total: batchTotal.value,
      completed: batchCompleted.value,
      savedAt: Date.now()
    }))
  } else {
    localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
  }
}

function loadBatchTaskFromStorage() {
  try {
    const raw = localStorage.getItem(BATCH_TASK_STORAGE_KEY)
    if (!raw) return null
    const data = JSON.parse(raw)
    if (data.savedAt && Date.now() - data.savedAt > 2 * 60 * 60 * 1000) {
      localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
      return null
    }
    return data
  } catch { return null }
}

onMounted(() => {
  if (!props.enableAI) return
  const saved = loadBatchTaskFromStorage()
  if (saved && saved.taskId) {
    batchTaskId.value = saved.taskId
    batchTotal.value = saved.total || 0
    batchCompleted.value = saved.completed || 0
    batchStatus.value = 'running'
    batchAnalyzing.value = true
    startBatchPolling()
  }
})

onUnmounted(() => {
  if (batchTimer) clearInterval(batchTimer)
})

const statLabels = computed(() => ({
  total: t('dirscan.total'),
  status_2xx: '2xx',
  status_3xx: '3xx',
  status_4xx: '4xx',
  status_5xx: '5xx'
}))

const dirColumns = computed(() => {
  const cols = [
    { label: t('dirscan.target'), prop: 'authority', minWidth: 150 },
    { label: 'URL', prop: 'url', slot: 'url', minWidth: 250, showOverflowTooltip: true },
    { label: t('dirscan.path'), prop: 'path', width: 120, showOverflowTooltip: true },
    { label: t('dirscan.statusCode'), prop: 'statusCode', slot: 'statusCode', width: 80, align: 'center', sortable: 'custom' },
    { label: t('dirscan.size'), prop: 'contentLength', slot: 'contentLength', width: 90, align: 'right', sortable: 'custom' },
    { label: t('task.contentWords'), prop: 'contentWords', width: 70, align: 'right', sortable: 'custom' },
    { label: t('task.duration'), prop: 'duration', slot: 'duration', width: 80, align: 'right', sortable: 'custom' },
    { label: t('dirscan.title'), prop: 'title', minWidth: 120, showOverflowTooltip: true },
  ]
  if (props.enableAI) {
    cols.push({ label: t('dirscan.aiStatus'), prop: 'aiStatus', slot: 'aiStatus', width: 110 })
  }
  cols.push(
    { label: t('common.createTime'), prop: 'createTime', slot: 'createTime', width: 150 },
    { label: t('common.updateTime'), prop: 'updateTime', slot: 'updateTime', width: 150 },
    { label: t('common.operation'), slot: 'operation', width: props.enableAI ? 200 : 120, fixed: 'right', align: 'center' }
  )
  return cols
})

const dirSearchItems = computed(() => {
  const items = [
    { label: 'URL', prop: 'url', type: 'input', placeholder: t('dirscan.targetPlaceholder') },
    { label: t('dirscan.path'), prop: 'path', type: 'input', placeholder: t('dirscan.pathPlaceholder') },
    {
      label: t('dirscan.statusCode'),
      prop: 'statusCode',
      type: 'select',
      options: [
        { label: '200', value: 200 },
        { label: '301', value: 301 },
        { label: '302', value: 302 },
        { label: '403', value: 403 },
        { label: '404', value: 404 },
        { label: '500', value: 500 }
      ]
    }
  ]
  if (props.enableAI) {
    items.push({
      label: t('dirscan.aiStatus'),
      prop: 'aiStatus',
      type: 'select',
      options: [
        { label: t('dirscan.aiNotAnalyzed'), value: 'pending' },
        { label: t('dirscan.aiRisk'), value: 'risk' },
        { label: t('dirscan.aiNoRisk'), value: 'no_risk' }
      ]
    })
  }
  return items
})

// 列表请求参数转换：将前端 aiStatus 筛选值映射到后端的 aiStatus/aiResult 参数
// AI未研判 -> aiStatus=pending；有风险 -> aiResult=risk；无风险 -> aiResult=no_risk
// completed（研判已完成，来自外部固定过滤）保留原样，不得覆盖已有的 aiResult
function transformListPayload(payload) {
  if (payload.aiStatus === 'risk' || payload.aiStatus === 'no_risk') {
    payload.aiResult = payload.aiStatus
    delete payload.aiStatus
  }
  return payload
}

function getStatusType(code) {
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'warning'
  if (code >= 400) return 'danger'
  return 'info'
}

function formatTime(time) {
  if (!time) return '-'
  const d = new Date(time)
  if (isNaN(d.getTime())) return time
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatSize(bytes) {
  if (!bytes || bytes < 0) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

async function showDetail(row) {
  if (!row.request && !row.response && row.id) {
    try {
      const res = await getDirScanDetail({ id: row.id })
      if (res.code === 0 && res.data) {
        currentDetail.value = res.data
      } else {
        currentDetail.value = row
      }
    } catch {
      currentDetail.value = row
    }
  } else {
    currentDetail.value = row
  }
  detailVisible.value = true
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('dirscan.confirmDelete'), t('common.tip'), { type: 'warning' })
    const res = await request.post('/dirscan/result/delete', { id: row.id })
    if (res.code === 0) {
      ElMessage.success(t('common.deleteSuccess'))
      proTableRef.value?.loadData()
      emit('data-changed')
    }
  } catch (e) { /* cancelled */ }
}

async function handleClear() {
  try {
    await ElMessageBox.confirm(t('dirscan.confirmClear'), t('common.warning'), {
      type: 'error',
      confirmButtonText: t('dirscan.confirmClearBtn'),
      cancelButtonText: t('common.cancel')
    })
    const res = await request.post('/dirscan/result/clear', {})
    if (res.code === 0) {
      ElMessage.success(res.msg || t('dirscan.clearSuccess'))
      proTableRef.value?.loadData()
      emit('data-changed')
    } else {
      ElMessage.error(res.msg || t('dirscan.clearFailed'))
    }
  } catch (e) {
    if (e !== 'cancel') console.error('清空目录扫描失败:', e)
  }
}

// ==================== AI研判相关 ====================

async function handleSingleAnalyze(row) {
  analyzingId.value = row.id
  try {
    const res = await analyzeDirByAI({ id: row.id })
    if (res.code === 0 && res.data) {
      row.aiStatus = res.data.aiStatus
      row.aiResult = res.data.aiResult
      row.aiReason = res.data.aiReason
      row.aiAnalyzedAt = res.data.aiAnalyzedAt
      ElMessage.success(
        res.data.aiResult === 'risk' ? t('dirscan.aiAnalyzedRisk') : t('dirscan.aiAnalyzedNoRisk')
      )
    } else {
      ElMessage.error(res.msg || t('dirscan.aiAnalyzeFailed'))
    }
  } catch (e) {
    console.error('AI研判失败:', e)
    ElMessage.error(t('dirscan.aiAnalyzeFailed'))
  } finally {
    analyzingId.value = ''
  }
}

async function handleBatchAnalyze() {
  let confirmMsg = t('dirscan.batchAIAnalyzeConfirm')
  const params = { concurrency: aiConcurrency.value }
  const selected = selectedRows.value
  if (selected.length > 0) {
    params.ids = selected.map(r => r.id)
    confirmMsg = t('dirscan.batchAnalyzeSelectedConfirm', { count: selected.length })
  } else {
    const currentSearch = proTableRef.value?.searchForm || {}
    const hasFilter = currentSearch.url || currentSearch.path || currentSearch.statusCode || currentSearch.authority || currentSearch.aiStatus
    if (hasFilter) {
      if (currentSearch.url) params.query = currentSearch.url
      if (currentSearch.path) params.path = currentSearch.path
      if (currentSearch.statusCode) params.statusCode = currentSearch.statusCode
      if (currentSearch.authority) params.authority = currentSearch.authority
      if (currentSearch.aiStatus) {
        if (currentSearch.aiStatus === 'pending') {
          params.aiStatus = 'pending'
        } else {
          params.aiResult = currentSearch.aiStatus
        }
      }
      confirmMsg = t('dirscan.batchAnalyzeFilteredConfirm')
    }
  }
  try {
    await ElMessageBox.confirm(confirmMsg, t('common.warning'),
      { type: 'warning', confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel') })
  } catch { return }

  batchAnalyzing.value = true
  batchTotal.value = 0
  batchCompleted.value = 0
  batchRiskCount.value = 0
  batchNoRiskCount.value = 0
  batchFailedCount.value = 0
  batchStatus.value = 'running'
  try {
    const res = await batchAnalyzeDirByAI(params)
    if (res.code === 0) {
      if (res.total === 0) {
        ElMessage.info(t('dirscan.noPendingData'))
        batchAnalyzing.value = false
        batchStatus.value = ''
        return
      }
      batchTaskId.value = res.taskId
      batchTotal.value = res.total
      saveBatchTaskToStorage()
      ElMessage.success(t('dirscan.batchTaskStarted', { total: res.total }))
      startBatchPolling()
    } else {
      ElMessage.error(res.msg || t('dirscan.batchStartFailed'))
      batchAnalyzing.value = false
      batchStatus.value = ''
    }
  } catch (e) {
    console.error('启动批量研判失败:', e)
    ElMessage.error(t('dirscan.batchStartFailed'))
    batchAnalyzing.value = false
    batchStatus.value = ''
  }
}

function startBatchPolling() {
  if (batchTimer) clearInterval(batchTimer)
  batchTimer = setInterval(async () => {
    try {
      const res = await getDirBatchProgress({ taskId: batchTaskId.value })
      if (res.code !== 0) {
        // 任务不存在（如服务重启导致内存态丢失），停止轮询避免卡死
        clearInterval(batchTimer); batchTimer = null
        batchAnalyzing.value = false
        batchStatus.value = ''
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.warning(t('dirscan.batchTaskLost'))
        return
      }
      batchCompleted.value = res.completed
      batchTotal.value = res.total
      batchRiskCount.value = res.riskCount || 0
      batchNoRiskCount.value = res.noRiskCount || 0
      batchFailedCount.value = res.failedCount || 0
      batchStatus.value = res.status
      saveBatchTaskToStorage()
      if (res.status === 'completed') {
        clearInterval(batchTimer); batchTimer = null
        batchAnalyzing.value = false
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.success(t('dirscan.batchAnalyzeDone', {
          completed: res.completed, total: res.total,
          risk: res.riskCount || 0, noRisk: res.noRiskCount || 0, failed: res.failedCount || 0
        }))
        proTableRef.value?.loadData()
        emit('data-changed')
      } else if (res.status === 'failed') {
        clearInterval(batchTimer); batchTimer = null
        batchAnalyzing.value = false
        // 任务失败（AI服务中断），清除持久化；未研判数据保持待研判，刷新列表
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.error(t('dirscan.batchAnalyzeFailed', {
          completed: res.completed, total: res.total,
          risk: res.riskCount || 0, noRisk: res.noRiskCount || 0, failed: res.failedCount || 0,
          unprocessed: res.total - res.completed - (res.failedCount || 0)
        }))
        proTableRef.value?.loadData()
        emit('data-changed')
      } else if (res.status === 'stopped') {
        clearInterval(batchTimer); batchTimer = null
        batchAnalyzing.value = false
        localStorage.removeItem(BATCH_TASK_STORAGE_KEY)
        ElMessage.warning(t('dirscan.batchAnalyzeStopped', {
          completed: res.completed, total: res.total,
          risk: res.riskCount || 0, noRisk: res.noRiskCount || 0, failed: res.failedCount || 0
        }))
        proTableRef.value?.loadData()
        emit('data-changed')
      }
    } catch (e) { /* ignore */ }
  }, 2000)
}

async function handleStopBatch() {
  try {
    await ElMessageBox.confirm(t('dirscan.confirmStopBatch'), t('dirscan.stopBatchAnalyze'),
      { confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel'), type: 'warning' })
  } catch { return }
  try {
    const res = await stopDirBatchAnalyze({ taskId: batchTaskId.value })
    if (res.code === 0) {
      ElMessage.info(t('dirscan.stopSignalSent'))
    } else {
      ElMessage.error(res.msg || t('dirscan.stopFailed'))
    }
  } catch (e) { ElMessage.error(t('dirscan.stopFailed')) }
}

// ==================== 导出 ====================

async function handleExport(command) {
  let data = []
  if (command === 'selected-url') {
    data = selectedRows.value
  } else {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      data = await fetchAllPages('/dirscan/result/list', (page, pageSize) => transformListPayload({
        ...(proTableRef.value?.searchForm || {}),
        ...(props.extraParams || {}),
        page,
        pageSize,
      }))
    } catch (e) {
      ElMessage.error(t('asset.getDataFailed'))
      return
    }
  }
  if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

  if (command === 'csv') {
    const headers = ['URL', 'Path', 'StatusCode', 'ContentLength', 'ContentWords', 'ContentLines', 'Duration(ms)', 'ContentType', 'Title', 'RedirectUrl', 'Host', 'Port', 'Authority', 'CreateTime', 'UpdateTime', 'AIStatus', 'AIResult']
    const csvRows = [headers.join(',')]
    for (const row of data) {
      csvRows.push([
        escapeCsvField(row.url || ''),
        escapeCsvField(row.path || ''),
        row.statusCode || '',
        row.contentLength || 0,
        row.contentWords || 0,
        row.contentLines || 0,
        row.duration || 0,
        escapeCsvField(row.contentType || ''),
        escapeCsvField(row.title || ''),
        escapeCsvField(row.redirectUrl || ''),
        escapeCsvField(row.host || ''),
        row.port || '',
        escapeCsvField(row.authority || ''),
        escapeCsvField(row.createTime || ''),
        escapeCsvField(row.updateTime || row.scanTime || ''),
        escapeCsvField(row.aiStatus || ''),
        escapeCsvField(row.aiResult || '')
      ].join(','))
    }
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `dirscan_results_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link); link.click(); document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(t('dirscan.exportSuccess', { count: data.length }))
    return
  }

  if (command === 'selected-url') {
    const urls = selectedRows.value.map(r => r.url).filter(Boolean)
    if (urls.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }
    const blob = new Blob([urls.join('\n')], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url; link.download = 'dirscan_urls_selected.txt'
    document.body.appendChild(link); link.click(); document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(t('dirscan.exportSuccess', { count: urls.length }))
    return
  }

  // all-url
  const seen = new Set()
  const exportData = []
  for (const row of data) {
    if (row.url && !seen.has(row.url)) { seen.add(row.url); exportData.push(row.url) }
  }
  if (exportData.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }
  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url; link.download = 'dirscan_urls_all.txt'
  document.body.appendChild(link); link.click(); document.body.removeChild(link)
  URL.revokeObjectURL(url)
  ElMessage.success(t('dirscan.exportSuccess', { count: exportData.length }))
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
.dirscan-view {
  height: 100%;

  .url-link {
    color: #409eff;
    text-decoration: none;
    font-family: monospace;
    &:hover { text-decoration: underline; }
  }

  .result-pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 400px;
    overflow: auto;
    background: var(--code-bg);
    color: var(--code-text);
    padding: 12px;
    border-radius: 6px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 13px;
    line-height: 1.5;
  }
}
</style>
