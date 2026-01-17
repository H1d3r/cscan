<template>
  <div class="vul-view">
    <!-- 搜索区域 -->
    <el-card class="search-card">
      <el-form :model="searchForm" inline>
        <el-form-item :label="$t('vul.target')">
          <el-input v-model="searchForm.authority" :placeholder="$t('vul.targetPlaceholder')" clearable @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item :label="$t('vul.severity')">
          <el-select v-model="searchForm.severity" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option :label="$t('vul.critical')" value="critical" />
            <el-option :label="$t('vul.high')" value="high" />
            <el-option :label="$t('vul.medium')" value="medium" />
            <el-option :label="$t('vul.low')" value="low" />
            <el-option :label="$t('vul.info')" value="info" />
            <el-option :label="$t('vul.unknown')" value="unknown" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('vul.source')">
          <el-select v-model="searchForm.source" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option label="Nuclei" value="nuclei" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
          <el-button type="danger" plain @click="handleClear">
            {{ $t('vul.clearData') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 统计信息 -->
    <el-row :gutter="16" class="stat-row">
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-value">{{ stat.total }}</div>
          <div class="stat-label">{{ $t('vul.totalVuls') }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card critical">
          <div class="stat-value">{{ stat.critical }}</div>
          <div class="stat-label">{{ $t('vul.critical') }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card high">
          <div class="stat-value">{{ stat.high }}</div>
          <div class="stat-label">{{ $t('vul.high') }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card medium">
          <div class="stat-value">{{ stat.medium }}</div>
          <div class="stat-label">{{ $t('vul.medium') }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card low">
          <div class="stat-value">{{ stat.low }}</div>
          <div class="stat-label">{{ $t('vul.low') }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card info">
          <div class="stat-value">{{ stat.info }}</div>
          <div class="stat-label">{{ $t('vul.info') }}</div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <div class="table-header">
        <span class="total-info">{{ $t('vul.totalVulsCount', { count: pagination.total }) }}</span>
        <div class="table-actions">
          <el-button type="danger" size="small" :disabled="selectedRows.length === 0" @click="handleBatchDelete">
            {{ $t('common.batchDelete') }} ({{ selectedRows.length }})
          </el-button>
          <el-dropdown style="margin-left: 10px" @command="handleExport">
            <el-button type="success" size="small">
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
        </div>
      </div>
      <el-table :data="tableData" v-loading="loading" stripe max-height="500" @selection-change="handleSelectionChange">
        <el-table-column type="selection" width="50" />
        <el-table-column prop="authority" :label="$t('vul.target')" min-width="150" />
        <el-table-column prop="url" label="URL" min-width="250" show-overflow-tooltip />
        <el-table-column prop="pocFile" label="POC" min-width="200" show-overflow-tooltip />
        <el-table-column prop="severity" :label="$t('vul.severity')" width="100">
          <template #default="{ row }">
            <el-tag :type="getSeverityType(row.severity)" size="small">{{ getSeverityLabel(row.severity) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="source" :label="$t('vul.source')" width="100" />
        <el-table-column prop="createTime" :label="$t('vul.discoveryTime')" width="160" />
        <el-table-column :label="$t('common.operation')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :total="pagination.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        class="pagination"
        @size-change="loadData"
        @current-change="loadData"
      />
    </el-card>

    <!-- 详情对话框 -->
    <el-dialog v-model="detailVisible" :title="$t('vul.vulDetail')" width="800px">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('vul.target')">{{ currentVul.authority }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.severity')">
          <el-tag :type="getSeverityType(currentVul.severity)">{{ getSeverityLabel(currentVul.severity) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="URL" :span="2">{{ currentVul.url }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.pocFile')" :span="2">{{ currentVul.pocFile }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.source')">{{ currentVul.source }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.discoveryTime')">{{ currentVul.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.verifyResult')" :span="2">
          <pre class="result-pre">{{ currentVul.result }}</pre>
        </el-descriptions-item>
      </el-descriptions>
      <template v-if="currentVul.evidence">
        <el-divider content-position="left">{{ $t('vul.evidence') }}</el-divider>
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="$t('vul.curlCommand')" v-if="currentVul.evidence.curlCommand">
            <pre class="result-pre">{{ currentVul.evidence.curlCommand }}</pre>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('vul.requestContent')" v-if="currentVul.evidence.request">
            <pre class="result-pre">{{ currentVul.evidence.request }}</pre>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('vul.responseContent')" v-if="currentVul.evidence.response">
            <pre class="result-pre">{{ currentVul.evidence.response }}</pre>
          </el-descriptions-item>
        </el-descriptions>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import request from '@/api/request'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])

const loading = ref(false)
const tableData = ref([])
const detailVisible = ref(false)
const currentVul = ref({})
const selectedRows = ref([])

const searchForm = reactive({ authority: '', severity: '', source: '' })
const stat = reactive({ total: 0, critical: 0, high: 0, medium: 0, low: 0, info: 0 })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })

function handleWorkspaceChanged() { pagination.page = 1; loadData(); loadStat() }

onMounted(() => {
  loadData(); loadStat()
  window.addEventListener('workspace-changed', handleWorkspaceChanged)
})
onUnmounted(() => { window.removeEventListener('workspace-changed', handleWorkspaceChanged) })

async function loadData() {
  loading.value = true
  try {
    const res = await request.post('/vul/list', {
      ...searchForm, page: pagination.page, pageSize: pagination.pageSize
    })
    if (res.code === 0) { tableData.value = res.list || []; pagination.total = res.total }
  } finally { loading.value = false }
}

async function loadStat() {
  try {
    const res = await request.post('/vul/stat', {})
    if (res.code === 0) {
      stat.total = res.total || 0
      stat.critical = res.critical || 0
      stat.high = res.high || 0
      stat.medium = res.medium || 0
      stat.low = res.low || 0
      stat.info = res.info || 0
    }
  } catch (e) { console.error(e) }
}

function handleSearch() { pagination.page = 1; loadData() }
function handleReset() {
  Object.assign(searchForm, { authority: '', severity: '', source: '' })
  handleSearch()
}

function handleSelectionChange(rows) { selectedRows.value = rows }

function getSeverityType(severity) {
  const map = { critical: 'danger', high: 'danger', medium: 'warning', low: 'info', info: '', unknown: 'info' }
  return map[severity] || ''
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
  await ElMessageBox.confirm(t('vul.confirmDeleteVul'), t('common.tip'), { type: 'warning' })
  const res = await request.post('/vul/delete', { id: row.id })
  if (res.code === 0) { ElMessage.success(t('common.deleteSuccess')); loadData(); loadStat() }
}

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  await ElMessageBox.confirm(t('vul.confirmBatchDelete', { count: selectedRows.value.length }), t('common.tip'), { type: 'warning' })
  const ids = selectedRows.value.map(row => row.id)
  const res = await request.post('/vul/batchDelete', { ids })
  if (res.code === 0) { ElMessage.success(t('common.deleteSuccess')); selectedRows.value = []; loadData(); loadStat(); emit('data-changed') }
}

async function handleClear() {
  await ElMessageBox.confirm(t('vul.confirmClearAll'), t('common.warning'), { type: 'error', confirmButtonText: t('vul.confirmClearBtn'), cancelButtonText: t('common.cancel') })
  const res = await request.post('/vul/clear', {})
  if (res.code === 0) { ElMessage.success(res.msg || t('vul.clearSuccess')); selectedRows.value = []; loadData(); loadStat(); emit('data-changed') }
  else { ElMessage.error(res.msg || t('vul.clearFailed')) }
}

// 导出功能
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
    // CSV导出所有字段
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/vul/list', {
        ...searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) {
        data = res.list || []
      } else {
        ElMessage.error(t('asset.getDataFailed'))
        return
      }
    } catch (e) {
      ElMessage.error(t('asset.getDataFailed'))
      return
    }
    
    if (data.length === 0) {
      ElMessage.warning(t('asset.noDataToExport'))
      return
    }
    
    const headers = ['Target', 'URL', 'POC', 'Severity', 'Source', 'Result', 'CreateTime']
    const csvRows = [headers.join(',')]
    
    for (const row of data) {
      const values = [
        escapeCsvField(row.authority || ''),
        escapeCsvField(row.url || ''),
        escapeCsvField(row.pocFile || ''),
        escapeCsvField(row.severity || ''),
        escapeCsvField(row.source || ''),
        escapeCsvField(row.result || ''),
        escapeCsvField(row.createTime || '')
      ]
      csvRows.push(values.join(','))
    }
    
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `vulnerabilities_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    
    ElMessage.success(t('asset.exportSuccess', { count: data.length }))
    return
  } else {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/vul/list', {
        ...searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) {
        data = res.list || []
      } else {
        ElMessage.error(t('asset.getDataFailed'))
        return
      }
    } catch (e) {
      ElMessage.error(t('asset.getDataFailed'))
      return
    }
    filename = command === 'all-target' ? 'vul_targets_all.txt' : 'vul_urls_all.txt'
  }
  
  if (data.length === 0) {
    ElMessage.warning(t('asset.noDataToExport'))
    return
  }
  
  // 提取数据并去重
  const seen = new Set()
  const exportData = []
  
  if (command.includes('target')) {
    for (const row of data) {
      if (row.authority && !seen.has(row.authority)) {
        seen.add(row.authority)
        exportData.push(row.authority)
      }
    }
  } else {
    for (const row of data) {
      if (row.url && !seen.has(row.url)) {
        seen.add(row.url)
        exportData.push(row.url)
      }
    }
  }
  
  if (exportData.length === 0) {
    ElMessage.warning(t('asset.noDataToExport'))
    return
  }
  
  // 下载文件
  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
  
  ElMessage.success(t('asset.exportSuccess', { count: exportData.length }))
}

// CSV字段转义
function escapeCsvField(field) {
  if (field == null) return ''
  const str = String(field)
  if (str.includes(',') || str.includes('"') || str.includes('\n') || str.includes('\r')) {
    return '"' + str.replace(/"/g, '""') + '"'
  }
  return str
}

function refresh() { loadData(); loadStat() }

defineExpose({ refresh })
</script>

<style scoped>
.vul-view {
  .search-card { margin-bottom: 16px; }
  .stat-row {
    margin-bottom: 16px;
    .stat-card {
      text-align: center;
      .stat-value { font-size: 24px; font-weight: 600; color: var(--el-color-primary); }
      .stat-label { color: var(--el-text-color-secondary); margin-top: 8px; font-size: 13px; }
      &.critical .stat-value { color: #f56c6c; }
      &.high .stat-value { color: #e6a23c; }
      &.medium .stat-value { color: #f0ad4e; }
      &.low .stat-value { color: #909399; }
      &.info .stat-value { color: #409eff; }
    }
  }
  .table-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    .total-info { color: var(--el-text-color-secondary); font-size: 14px; }
  }
  .pagination { margin-top: 20px; justify-content: flex-end; }
  .result-pre {
    margin: 0; white-space: pre-wrap; word-break: break-all; max-height: 300px; overflow: auto;
    background: var(--code-bg); color: var(--code-text); padding: 12px; border-radius: 6px;
    font-family: 'Consolas', 'Monaco', monospace; font-size: 13px; line-height: 1.5;
  }
}
</style>

