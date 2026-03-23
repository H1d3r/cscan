<template>
  <div class="dirscan-view">
    <!-- 全局搜索区域 -->
    <el-card class="search-card">
      <el-form :model="searchForm" inline>
        <el-form-item :label="$t('dirscan.target')">
          <el-input v-model="searchForm.authority" :placeholder="$t('dirscan.targetPlaceholder')" clearable @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item :label="$t('dirscan.path')">
          <el-input v-model="searchForm.path" :placeholder="$t('dirscan.pathPlaceholder')" clearable @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item :label="$t('dirscan.statusCode')">
          <el-select v-model="searchForm.statusCode" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option label="200" :value="200" />
            <el-option label="301" :value="301" />
            <el-option label="302" :value="302" />
            <el-option label="403" :value="403" />
            <el-option label="404" :value="404" />
            <el-option label="500" :value="500" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
          <el-button type="danger" plain @click="handleClear">{{ $t('dirscan.clearData') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 统计信息 -->
    <el-row :gutter="16" class="stat-row">
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-value">{{ stat.total }}</div>
          <div class="stat-label">{{ $t('dirscan.total') }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card status-2xx">
          <div class="stat-value">{{ stat.status_2xx || 0 }}</div>
          <div class="stat-label">2xx</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card status-3xx">
          <div class="stat-value">{{ stat.status_3xx || 0 }}</div>
          <div class="stat-label">3xx</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card status-4xx">
          <div class="stat-value">{{ stat.status_4xx || 0 }}</div>
          <div class="stat-label">4xx</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card status-5xx">
          <div class="stat-value">{{ stat.status_5xx || 0 }}</div>
          <div class="stat-label">5xx</div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 按目标分组的折叠面板 -->
    <el-card class="collapse-card" v-loading="loading">
      <div class="collapse-header">
        <span class="total-info">{{ $t('dirscan.totalTargets', { targets: Object.keys(groupedData).length, records: pagination.total }) }}</span>
        <div class="collapse-actions">
          <el-button size="small" @click="expandAll">{{ $t('dirscan.expandAll') }}</el-button>
          <el-button size="small" @click="collapseAll">{{ $t('dirscan.collapseAll') }}</el-button>
          <el-dropdown style="margin-left: 10px" @command="handleExport">
            <el-button type="success" size="small">
              {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="all-url">{{ $t('dirscan.exportAllUrl') }}</el-dropdown-item>
                <el-dropdown-item command="csv">{{ $t('dirscan.exportCsv') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </div>

      <el-skeleton :loading="loading && Object.keys(groupedData).length === 0" animated :count="5">
        <template #template>
          <div style="padding: 10px 0; display: flex; flex-direction: column; gap: 15px;">
            <div style="display: flex; gap: 10px; align-items: center;">
              <el-skeleton-item variant="circle" style="width: 20px; height: 20px;" />
              <el-skeleton-item variant="rect" style="width: 200px; height: 20px;" />
              <el-skeleton-item variant="rect" style="width: 30px; height: 18px; border-radius: 10px" />
            </div>
            <div style="display: flex; gap: 10px; align-items: center; padding-left: 20px;">
              <el-skeleton-item variant="rect" style="width: 150px; height: 30px;" />
              <el-skeleton-item variant="rect" style="width: 100px; height: 30px;" />
              <el-skeleton-item variant="rect" style="width: 100px; height: 30px;" />
              <el-skeleton-item variant="rect" style="width: 100px; height: 30px;" />
            </div>
            <div style="display: flex; gap: 10px; align-items: center; padding-left: 20px;">
              <el-skeleton-item variant="rect" style="flex: 1; height: 40px;" />
            </div>
            <div style="display: flex; gap: 10px; align-items: center; padding-left: 20px;">
              <el-skeleton-item variant="rect" style="flex: 1; height: 40px;" />
            </div>
          </div>
        </template>
        <template #default>
          <el-collapse v-model="activeNames" class="target-collapse">
            <el-collapse-item v-for="(items, authority) in groupedData" :key="authority" :name="authority">
              <template #title>
                <div class="collapse-title">
                  <span class="target-name">{{ authority }}</span>
                  <el-badge :value="getFilteredItems(authority).length" :max="999" type="primary" style="margin-left: 10px" />
                </div>
              </template>
              <!-- 目标内筛选栏 -->
              <div class="target-filter-bar">
                <el-input
                  v-model="getTargetFilter(authority).path"
                  :placeholder="$t('dirscan.filterByPath')"
                  size="small"
                  clearable
                  style="width: 180px; margin-right: 8px"
                />
                <el-select
                  v-model="getTargetFilter(authority).statusCode"
                  :placeholder="$t('dirscan.statusCode')"
                  size="small"
                  clearable
                  style="width: 100px; margin-right: 8px"
                >
                  <el-option v-for="code in getTargetStatusCodes(authority)" :key="code" :label="code" :value="code" />
                </el-select>
                <el-input
                  v-model="getTargetFilter(authority).sizeMin"
                  :placeholder="$t('dirscan.sizeMin')"
                  size="small"
                  clearable
                  style="width: 100px; margin-right: 4px"
                  type="number"
                />
                <span class="filter-separator">-</span>
                <el-input
                  v-model="getTargetFilter(authority).sizeMax"
                  :placeholder="$t('dirscan.sizeMax')"
                  size="small"
                  clearable
                  style="width: 100px; margin-right: 8px"
                  type="number"
                />
                <el-button size="small" @click="clearTargetFilter(authority)">{{ $t('common.reset') }}</el-button>
              </div>
              <el-table :data="getFilteredItems(authority)" stripe size="small" table-layout="fixed" @sort-change="(e) => handleTargetSortChange(authority, e)">
                <el-table-column prop="url" label="URL" min-width="200" show-overflow-tooltip>
                  <template #default="{ row }">
                    <a :href="row.url" target="_blank" rel="noopener" class="url-link">{{ row.url }}</a>
                  </template>
                </el-table-column>
                <el-table-column prop="path" :label="$t('dirscan.path')" width="120" show-overflow-tooltip />
                <el-table-column prop="statusCode" :label="$t('dirscan.statusCode')" width="80" sortable="custom" align="center">
                  <template #default="{ row }">
                    <el-tag :type="getStatusType(row.statusCode)" size="small">{{ row.statusCode }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="contentLength" :label="$t('dirscan.size')" width="80" sortable="custom" align="right">
                  <template #default="{ row }">{{ formatSize(row.contentLength) }}</template>
                </el-table-column>
                <el-table-column prop="contentWords" :label="$t('task.contentWords')" width="70" sortable="custom" align="right">
                  <template #default="{ row }">{{ row.contentWords || 0 }}</template>
                </el-table-column>
                <el-table-column prop="contentLines" :label="$t('task.contentLines')" width="70" sortable="custom" align="right">
                  <template #default="{ row }">{{ row.contentLines || 0 }}</template>
                </el-table-column>
                <el-table-column prop="duration" :label="$t('task.duration')" width="80" sortable="custom" align="right">
                  <template #default="{ row }">{{ row.duration ? row.duration + 'ms' : '-' }}</template>
                </el-table-column>
                <el-table-column prop="title" :label="$t('dirscan.title')" min-width="100" show-overflow-tooltip />
                <el-table-column prop="contentType" :label="$t('dirscan.contentType')" width="130" show-overflow-tooltip />
                <el-table-column prop="redirectUrl" :label="$t('dirscan.redirectUrl')" min-width="120" show-overflow-tooltip />
                <el-table-column prop="createTime" :label="$t('dirscan.discoveryTime')" width="150">
                  <template #default="{ row }">{{ formatTime(row.createTime) }}</template>
                </el-table-column>
                <el-table-column :label="$t('common.operation')" width="70" fixed="right" align="center">
                  <template #default="{ row }">
                    <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </el-collapse-item>
          </el-collapse>
        </template>
      </el-skeleton>

      <el-empty v-if="Object.keys(groupedData).length === 0 && !loading" :description="$t('common.noData')" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import request from '@/api/request'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])

const loading = ref(false)
const tableData = ref([])
const activeNames = ref([])

const searchForm = reactive({ authority: '', path: '', statusCode: null })
const sortForm = reactive({ sortField: '', sortOrder: '' })
const stat = reactive({ total: 0, status_2xx: 0, status_3xx: 0, status_4xx: 0, status_5xx: 0 })
const pagination = reactive({ page: 1, pageSize: 1000, total: 0 })

// 每个目标的独立筛选/排序状态
const targetFilters = reactive({})
const targetSorts = reactive({})

function getTargetFilter(authority) {
  if (!targetFilters[authority]) {
    targetFilters[authority] = { path: '', statusCode: null, sizeMin: '', sizeMax: '' }
  }
  return targetFilters[authority]
}

function clearTargetFilter(authority) {
  targetFilters[authority] = { path: '', statusCode: null, sizeMin: '', sizeMax: '' }
  delete targetSorts[authority]
}

// 获取目标下所有出现过的状态码（用于筛选下拉）
function getTargetStatusCodes(authority) {
  const items = groupedData.value[authority] || []
  const codes = new Set()
  for (const item of items) {
    if (item.statusCode) codes.add(item.statusCode)
  }
  return Array.from(codes).sort()
}

// 获取经过目标内筛选和排序后的数据
function getFilteredItems(authority) {
  let items = groupedData.value[authority] || []
  const filter = targetFilters[authority]

  if (filter) {
    if (filter.path) {
      const lower = filter.path.toLowerCase()
      items = items.filter(item => (item.path || '').toLowerCase().includes(lower))
    }
    if (filter.statusCode != null) {
      items = items.filter(item => item.statusCode === filter.statusCode)
    }
    if (filter.sizeMin !== '' && filter.sizeMin != null) {
      const min = Number(filter.sizeMin)
      if (!isNaN(min)) items = items.filter(item => (item.contentLength || 0) >= min)
    }
    if (filter.sizeMax !== '' && filter.sizeMax != null) {
      const max = Number(filter.sizeMax)
      if (!isNaN(max)) items = items.filter(item => (item.contentLength || 0) <= max)
    }
  }

  // 目标内排序
  const sort = targetSorts[authority]
  if (sort && sort.prop && sort.order) {
    const prop = sort.prop
    const asc = sort.order === 'ascending'
    items = [...items].sort((a, b) => {
      const va = a[prop] ?? 0
      const vb = b[prop] ?? 0
      if (va < vb) return asc ? -1 : 1
      if (va > vb) return asc ? 1 : -1
      return 0
    })
  }

  return items
}

function handleTargetSortChange(authority, { prop, order }) {
  if (order) {
    targetSorts[authority] = { prop, order }
  } else {
    delete targetSorts[authority]
  }
}

// 按目标分组数据
const groupedData = computed(() => {
  const groups = {}
  for (const item of tableData.value) {
    const key = item.authority || 'unknown'
    if (!groups[key]) groups[key] = []
    groups[key].push(item)
  }
  return groups
})

function handleWorkspaceChanged() { loadData(); loadStat() }

onMounted(() => {
  loadData(); loadStat()
  window.addEventListener('workspace-changed', handleWorkspaceChanged)
})
onUnmounted(() => { window.removeEventListener('workspace-changed', handleWorkspaceChanged) })

async function loadData() {
  loading.value = true
  try {
    const params = { page: 1, pageSize: pagination.pageSize }
    if (searchForm.authority) params.authority = searchForm.authority
    if (searchForm.path) params.path = searchForm.path
    if (searchForm.statusCode != null) params.statusCode = searchForm.statusCode
    if (sortForm.sortField) params.sortField = sortForm.sortField
    if (sortForm.sortOrder) params.sortOrder = sortForm.sortOrder

    const res = await request.post('/dirscan/result/list', params)
    if (res.code === 0) {
      tableData.value = res.list || []
      pagination.total = res.total || 0
      // 默认展开第一个目标
      const keys = Object.keys(groupedData.value)
      if (keys.length > 0 && activeNames.value.length === 0) {
        activeNames.value = [keys[0]]
      }
    }
  } catch (e) {
    console.error('[DirScan] loadData error:', e)
  } finally {
    loading.value = false
  }
}

async function loadStat() {
  try {
    const res = await request.post('/dirscan/result/stat', {})
    if (res.code === 0 && res.stat) {
      stat.total = res.stat.total || 0
      stat.status_2xx = res.stat.status_2xx || 0
      stat.status_3xx = res.stat.status_3xx || 0
      stat.status_4xx = res.stat.status_4xx || 0
      stat.status_5xx = res.stat.status_5xx || 0
    }
  } catch (e) { console.error(e) }
}

function handleSearch() { loadData() }
function handleReset() {
  Object.assign(searchForm, { authority: '', path: '', statusCode: null })
  Object.assign(sortForm, { sortField: '', sortOrder: '' })
  handleSearch()
}

function expandAll() { activeNames.value = Object.keys(groupedData.value) }
function collapseAll() { activeNames.value = [] }

function getStatusType(code) {
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'warning'
  if (code >= 400 && code < 500) return 'danger'
  if (code >= 500) return 'danger'
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

async function handleDelete(row) {
  await ElMessageBox.confirm(t('dirscan.confirmDelete'), t('common.tip'), { type: 'warning' })
  const res = await request.post('/dirscan/result/delete', { id: row.id })
  if (res.code === 0) { ElMessage.success(t('common.deleteSuccess')); loadData(); loadStat() }
}

async function handleClear() {
  await ElMessageBox.confirm(t('dirscan.confirmClear'), t('common.warning'), { type: 'error', confirmButtonText: t('dirscan.confirmClearBtn'), cancelButtonText: t('common.cancel') })
  const res = await request.post('/dirscan/result/clear', {})
  if (res.code === 0) { ElMessage.success(res.msg || t('dirscan.clearSuccess')); loadData(); loadStat(); emit('data-changed') }
  else { ElMessage.error(res.msg || t('dirscan.clearFailed')) }
}

async function handleExport(command) {
  if (tableData.value.length === 0) {
    ElMessage.warning(t('dirscan.noDataToExport'))
    return
  }

  if (command === 'csv') {
    // CSV导出所有字段（含新字段）
    const headers = ['URL', 'Path', 'StatusCode', 'ContentLength', 'ContentWords', 'ContentLines', 'Duration(ms)', 'ContentType', 'Title', 'RedirectUrl', 'Host', 'Port', 'Authority', 'CreateTime']
    const csvRows = [headers.join(',')]

    for (const row of tableData.value) {
      const values = [
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
        escapeCsvField(row.createTime || '')
      ]
      csvRows.push(values.join(','))
    }

    // 添加BOM以支持Excel正确识别UTF-8
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `dirscan_results_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)

    ElMessage.success(t('dirscan.exportSuccess', { count: tableData.value.length }))
    return
  }

  // 原有的URL导出逻辑
  const seen = new Set()
  const exportData = []
  for (const row of tableData.value) {
    if (row.url && !seen.has(row.url)) {
      seen.add(row.url)
      exportData.push(row.url)
    }
  }

  if (exportData.length === 0) {
    ElMessage.warning(t('dirscan.noDataToExport'))
    return
  }

  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'dirscan_urls_all.txt'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)

  ElMessage.success(t('dirscan.exportSuccess', { count: exportData.length }))
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
.dirscan-view {
  .search-card { margin-bottom: 16px; }
  .stat-row {
    margin-bottom: 16px;
    .stat-card {
      text-align: center;
      .stat-value { font-size: 24px; font-weight: 600; color: var(--el-color-primary); }
      .stat-label { color: var(--el-text-color-secondary); margin-top: 8px; font-size: 13px; }
      &.status-2xx .stat-value { color: #67c23a; }
      &.status-3xx .stat-value { color: #e6a23c; }
      &.status-4xx .stat-value { color: #f56c6c; }
      &.status-5xx .stat-value { color: #909399; }
    }
  }
  .collapse-card {
    .collapse-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 16px;
      .total-info { color: var(--el-text-color-secondary); font-size: 14px; }
    }
  }
  .target-collapse {
    .collapse-title {
      display: flex;
      align-items: center;
      .target-name {
        font-weight: 500;
        color: var(--el-color-primary);
      }
    }
  }
  .target-filter-bar {
    display: flex;
    align-items: center;
    padding: 8px 0 12px 0;
    flex-wrap: wrap;
    gap: 4px;
    .filter-separator {
      color: var(--el-text-color-secondary);
      margin: 0 2px;
    }
  }
  .url-link {
    color: var(--el-color-primary);
    text-decoration: none;
    &:hover { text-decoration: underline; }
  }
}
</style>
