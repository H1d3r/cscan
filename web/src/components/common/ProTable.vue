<template>
  <el-card class="pro-table-wrapper" shadow="never">


    <!-- Global Toolbar (Top) -->
    <div v-if="showTopToolbar" class="pro-table-top-toolbar">
      <div v-if="searchPlaceholder || $slots.search" class="global-search">
        <slot name="search">
          <el-autocomplete
            v-if="searchPlaceholder"
            v-model="searchQuery"
            :fetch-suggestions="querySearch"
            :placeholder="searchPlaceholder"
            clearable
            @keyup.enter="handleSearch"
            class="search-input"
          >
            <template #prefix>
              <el-icon><Search /></el-icon>
            </template>
          </el-autocomplete>
        </slot>
      </div>

      <div v-if="showTopFilterButton" class="header-actions">
        <el-button
          @click="showFilters = !showFilters"
        >
          <el-icon><Filter /></el-icon>
          {{ $t('asset.assetInventoryTab.filters') || '过滤器' }}
        </el-button>
      </div>
    </div>


    <!-- Filters Panel -->
    <div v-if="showFilters && searchItems && searchItems.length > 0" class="filters-panel">
      <el-form :model="searchForm" :inline="true" size="default">
        <el-form-item
          v-for="item in searchItems"
          :key="item.prop"
          :label="item.label"
        >
          <el-autocomplete
            v-if="item.type === 'input'"
            v-model="searchForm[item.prop]"
            :placeholder="item.placeholder || `请输入 ${item.label}`"
            :type="item.inputType || 'text'"
            clearable
            :fetch-suggestions="(qs, cb) => queryFieldSearch(qs, cb, item.prop)"
            @keyup.enter="handleSearch"
            v-bind="item.props || {}"
          />
          <el-select
            v-else-if="item.type === 'select'"
            v-model="searchForm[item.prop]"
            :placeholder="item.placeholder || `请选择 ${item.label}`"
            clearable
            filterable
            :multiple="item.multiple"
            :allow-create="item.allowCreate"
            style="min-width: 200px"
            v-bind="item.props || {}"
          >
            <el-option
              v-for="opt in item.options"
              :key="typeof opt === 'object' ? opt.value : opt"
              :label="typeof opt === 'object' ? opt.label : opt"
              :value="typeof opt === 'object' ? opt.value : opt"
            />
          </el-select>
        </el-form-item>

        <div class="filter-actions-row">
          <el-button type="primary" @click="applyFilters">{{ $t('asset.assetInventoryTab.apply') || '应用' }}</el-button>
          <el-button @click="resetSearch">{{ $t('asset.assetInventoryTab.reset') || '重置' }}</el-button>
        </div>
      </el-form>

      <slot name="stat-panel"></slot>
    </div>

    <!-- Batch Actions & Stats Toolbar -->
    <div class="pro-table-batch-toolbar">
      <div class="batch-left">
        <!-- Batch Delete Button -->
        <el-button
          v-if="batchDeleteApi"
          type="danger"
          size="default"
          :disabled="selectedRows.length === 0"
          @click="handleBatchDelete"
        >
          {{ $t('common.batchDelete') || '批量删除' }} ({{ selectedRows.length }})
        </el-button>

        <!-- Export Dropdown -->
        <el-dropdown
          v-if="exportApi"
          @command="handleExport"
        >
          <el-button type="success" size="default">
            {{ $t('common.export') || '导出' }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="selected" :disabled="selectedRows.length === 0">
                {{ $t('common.exportSelected') || '导出选中项' }} ({{ selectedRows.length }})
              </el-dropdown-item>
              <el-dropdown-item divided command="all">
                {{ $t('common.exportAll') || '导出所有' }}
              </el-dropdown-item>
              <el-dropdown-item command="csv">
                {{ $t('common.exportCsv') || '导出 CSV' }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>

        <el-button
          v-if="showInlineFilterButton"
          @click="showFilters = !showFilters"
        >
          <el-icon><Filter /></el-icon>
          {{ $t('asset.assetInventoryTab.filters') || '过滤器' }}
        </el-button>

        <slot name="toolbar-left"></slot>
      </div>
      <div class="batch-right">
        <!-- Inline stats -->
        <template v-if="statApi && Object.keys(stats).length > 0">
          <el-tag
            v-for="(label, key) in statLabels"
            :key="key"
            type="info"
            effect="plain"
            class="stat-tag"
            style="margin-left: 8px;"
          >
            {{ label }}: <span class="stat-value">{{ stats[key] !== undefined ? stats[key] : 0 }}</span>
          </el-tag>
        </template>

        <slot name="toolbar-right"></slot>
      </div>
    </div>

    <!-- Main Table -->
    <el-table v-loading="loading" :data="tableData" v-bind="$attrs" @selection-change="handleSelectionChange" @sort-change="handleSortChange">
      <el-table-column v-if="selection" type="selection" width="40" />

      <template v-for="(col, index) in columns" :key="index">
        <!-- If column has a slot -->
        <el-table-column v-if="col.slot" v-bind="col">
          <template #default="{ row }">
            <slot :name="col.slot" :row="row"></slot>
          </template>
        </el-table-column>

        <!-- Default rendering -->
        <el-table-column v-else v-bind="col">
          <template #default="{ row }">
            {{ row[col.prop] }}
          </template>
        </el-table-column>
      </template>
    </el-table>

    <!-- Pagination -->
    <div class="pro-table-footer">
      <slot name="footer">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          :total="pagination.total"
          @size-change="loadData"
          @current-change="loadData"
        />
      </slot>
    </div>
  </el-card>
</template>

<script setup>
import { ref, reactive, watch, onMounted, onUnmounted, useSlots } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, Filter, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import request from '@/api/request'
import { debounce } from 'lodash-es'

defineOptions({
  name: 'ProTable',
  inheritAttrs: false
})

const props = defineProps({
  searchPlaceholder: {
    type: String,
    default: ''
  },
  api: {
    type: String,
    default: ''
  },
  columns: {
    type: Array,
    default: () => []
  },
  statApi: {
    type: String,
    default: ''
  },
  statLabels: {
    type: Object,
    default: () => ({})
  },
  searchItems: {
    type: Array,
    default: () => []
  },
  batchDeleteApi: {
    type: String,
    default: ''
  },
  exportApi: {
    type: String,
    default: ''
  },
  csvFormatter: {
    type: Function,
    default: null
  },
  searchKeys: {
    type: Array,
    default: () => []
  },
  rowKey: {
    type: String,
    default: 'id'
  },
  selection: {
    type: Boolean,
    default: false
  },
  // 固定的额外查询参数，会被合并到 loadData 的 payload 中（不写入 URL）。
  // 用于父页面注入服务端固定过滤条件（例如敏感信息页强制 isRisk=true）。
  extraParams: {
    type: Object,
    default: () => ({})
  },
  // 可选的请求参数转换函数，在发送请求前对 payload 做最后修改
  // 用于将前端筛选值映射到后端实际参数名（例如 aiStatus 的不同值映射到 aiStatus/aiResult）
  transformPayload: {
    type: Function,
    default: null
  }
})

const router = useRouter()
const route = useRoute()

// State
const searchQuery = ref('')
const showFilters = ref(false)
const loading = ref(false)
const tableData = ref([])
const selectedRows = ref([])
const stats = ref({})
const isInitialLoad = ref(true)
// 请求序号：快速切换搜索/翻页时旧响应可能晚于新响应返回，丢弃过期响应避免覆盖新数据
let loadDataSeq = 0
const pagination = reactive({
  page: 1,
  pageSize: 10,
  total: 0
})
const sortState = reactive({
  prop: '',
  order: ''
})

// I18n
const { t } = useI18n()
const emit = defineEmits(['data-changed'])

const hasFilterButton = props.searchItems && props.searchItems.length > 0
const hasSearchToolbar = !!props.searchPlaceholder
const hasCustomSearchSlot = !!useSlots().search
const showTopFilterButton = hasFilterButton && (hasSearchToolbar || hasCustomSearchSlot)
const showInlineFilterButton = hasFilterButton && !showTopFilterButton
const showTopToolbar = hasSearchToolbar || hasCustomSearchSlot || showTopFilterButton

// Placeholder for advanced search form, currently synced with url
const searchForm = reactive({})

// Keys that ProTable should never read from or write to URL (managed by parent components)
const EXTERNAL_QUERY_KEYS = new Set(['tab', 'subTab'])

// Get the set of valid search field keys from searchItems prop
function getSearchItemKeys() {
  return new Set((props.searchItems || []).map(item => item.prop))
}

// Initialize state from URL query
function initQueryFromUrl() {
  const query = route.query
  if (query.page) pagination.page = parseInt(query.page) || 1
  if (query.pageSize) pagination.pageSize = parseInt(query.pageSize) || 10

  const searchItemKeys = getSearchItemKeys()

  for (const key in query) {
    if (key === 'page' || key === 'pageSize' || EXTERNAL_QUERY_KEYS.has(key)) {
      continue
    }
    if (key === 'query') {
      searchQuery.value = query[key]
    } else if (searchItemKeys.size === 0 || searchItemKeys.has(key)) {
      searchForm[key] = query[key]
    }
  }
}

// Sync ProTable state to URL query, preserving all external params
function syncQueryToUrl() {
  const proTableParams = {
    page: pagination.page,
    pageSize: pagination.pageSize,
    query: searchQuery.value,
    ...searchForm
  }

  // Remove empty ProTable values
  Object.keys(proTableParams).forEach(key => {
    if (proTableParams[key] === '' || proTableParams[key] === null || proTableParams[key] === undefined) {
      delete proTableParams[key]
    }
  })

  // Start from current URL query to preserve tab/subTab and any other external params,
  // then override only ProTable-owned keys,
  // and also clean up any stale ProTable keys (e.g. old searchForm fields no longer set)
  const currentQuery = { ...route.query }

  // Remove old ProTable-managed keys from currentQuery before merging
  // (page, pageSize, query, and all searchItems props)
  const searchItemKeys = getSearchItemKeys()
  Object.keys(currentQuery).forEach(key => {
    if (key === 'page' || key === 'pageSize' || key === 'query' || searchItemKeys.has(key)) {
      delete currentQuery[key]
    }
  })

  const finalQuery = { ...currentQuery, ...proTableParams }

  // Clean up empty values
  Object.keys(finalQuery).forEach(key => {
    if (finalQuery[key] === '' || finalQuery[key] === null || finalQuery[key] === undefined) {
      delete finalQuery[key]
    }
  })

  router.replace({ path: route.path, query: finalQuery }).catch(() => {})
}

// Data fetching
async function loadStats() {
  if (!props.statApi) return
  try {
    const payload = { query: searchQuery.value,
      ...searchForm }
    const res = await request.post(props.statApi, payload)
    if (res.code === 0) {
      // 处理嵌套的 data 包装情况
      stats.value = res.data?.stat || res.data || res.stat || res || {}
    }
  } catch (error) {
    console.error('Failed to load stats:', error)
  }
}

async function loadData() {
  if (props.statApi) loadStats()

  if (!props.api) return

  const seq = ++loadDataSeq
  loading.value = true

  // Skip URL sync on initial mount to avoid race condition with parent components
  // (e.g. AssetInventoryTab writing subTab via async router.replace)
  if (!isInitialLoad.value) {
    syncQueryToUrl()
  }
  isInitialLoad.value = false

  try {
    const payload = {
      page: pagination.page,
      pageSize: pagination.pageSize,
      query: searchQuery.value,
      ...(props.extraParams || {}),
      ...searchForm
    }

    // Add sort parameters if sorting is active
    if (sortState.prop && sortState.order) {
      payload.sortField = sortState.prop
      payload.sortOrder = sortState.order === 'ascending' ? 'asc' : 'desc'
    }

    // Cast specific fields to Number if they are defined as numbers in searchItems
    if (props.searchItems) {
      props.searchItems.forEach(item => {
        if (item.inputType === 'number' && payload[item.prop] !== undefined && payload[item.prop] !== '') {
          const numVal = Number(payload[item.prop])
          if (!isNaN(numVal)) {
            payload[item.prop] = numVal
          }
        }
      })
    }

    // 应用自定义 payload 转换（例如将 aiStatus 筛选值映射到 aiStatus/aiResult）
    const finalPayload = props.transformPayload ? props.transformPayload(payload) : payload

    const res = await request.post(props.api, finalPayload)
    if (seq !== loadDataSeq) return // 已有更新的请求发出，丢弃本次过期响应
    if (res.code === 0) {
      tableData.value = res.list || []
      pagination.total = res.total || 0
    }
  } catch (error) {
    console.error('Failed to load table data:', error)
  } finally {
    if (seq === loadDataSeq) loading.value = false
  }
}

// Search autocomplete based on current table data
function querySearch(queryString, cb) {
  if (!tableData.value || tableData.value.length === 0) {
    cb([])
    return
  }

  const uniqueValues = new Set()
  tableData.value.forEach(row => {
    const keysToSearch = props.searchKeys && props.searchKeys.length > 0 ? props.searchKeys : Object.keys(row)

    for (const key of keysToSearch) {
      const val = row[key]
      if (val !== undefined && val !== null && val !== '') {
        if (typeof val === 'string' || typeof val === 'number') {
          // Skip long IDs and dates
          const strVal = String(val)
          if (strVal.length < 50 && !strVal.includes('T00:00:00') && key !== 'id') {
            uniqueValues.add(strVal)
          }
        } else if (Array.isArray(val)) {
          val.forEach(v => {
            if (v && typeof v === 'object' && v.ip) uniqueValues.add(String(v.ip))
            else if (v && typeof v === 'object' && v.host) uniqueValues.add(String(v.host))
            else if (typeof v !== 'object') uniqueValues.add(String(v))
          })
        }
      }
    }
  })

  const results = Array.from(uniqueValues).map(v => ({ value: String(v) }))
  if (queryString) {
    const lowerQuery = String(queryString).toLowerCase()
    cb(results.filter(item => item.value.toLowerCase().includes(lowerQuery)).slice(0, 20))
  } else {
    cb(results.slice(0, 20))
  }
}

// Reset and reload helper
const handleSearch = debounce(() => {
  pagination.page = 1
  loadData()
}, 300)

// 监听全局搜索框内容变化，自动触发查询（与组件事件解耦）
watch(searchQuery, () => {
  handleSearch()
})

function queryFieldSearch(queryString, cb, prop) {
  if (!tableData.value || tableData.value.length === 0) {
    cb([])
    return
  }

  const uniqueValues = new Set()
  tableData.value.forEach(row => {
    let val = row[prop]

    if (val !== undefined && val !== null && val !== '') {
      // 针对特殊字段展开数组匹配
      if (Array.isArray(val)) {
        val.forEach(v => {
          if (v && typeof v === 'object' && v.ip) uniqueValues.add(String(v.ip))
          else if (v && typeof v === 'object' && v.host) uniqueValues.add(String(v.host))
          else if (typeof v !== 'object') uniqueValues.add(String(v))
        })
      } else {
        uniqueValues.add(String(val))
      }
    }
  })

  const results = Array.from(uniqueValues).map(v => ({ value: String(v) }))
  if (queryString) {
    const lowerQuery = String(queryString).toLowerCase()
    cb(results.filter(item => item.value.toLowerCase().includes(lowerQuery)).slice(0, 20))
  } else {
    cb(results.slice(0, 20))
  }
}

// Apply filters manually
function applyFilters() {
  pagination.page = 1
  loadData()
}

// Reset advanced search and global search query
function resetSearch() {
  searchQuery.value = ''
  for (const key in searchForm) {
    delete searchForm[key]
  }
  pagination.page = 1
  loadData()
}

// Selection changes
function handleSelectionChange(rows) {
  selectedRows.value = rows
}

// Sort changes
function handleSortChange({ prop, order }) {
  sortState.prop = prop
  sortState.order = order
  pagination.page = 1
  loadData()
}

// Batch delete
async function handleBatchDelete() {
  if (selectedRows.value.length === 0 || !props.batchDeleteApi) return

  try {
    await ElMessageBox.confirm(
      t('common.confirmBatchDelete', { count: selectedRows.value.length }) || `Are you sure you want to delete ${selectedRows.value.length} items?`,
      t('common.tip') || 'Warning',
      { type: 'warning' }
    )
  } catch (e) {
    return
  }

  const keys = selectedRows.value.map(row => row[props.rowKey])

  // The backend usually expects ips, domains, etc. Here we pass a generic key based on rowKey
  // e.g. if rowKey is 'ip', we pass { ips: [...] }
  // We'll pass both `ids` and the pluralized rowKey just to be safe, or just adapt based on how the backend expects it.
  const payloadKey = props.rowKey + 's'
  const payload = {}
  payload[payloadKey] = keys
  payload['ids'] = keys // fallback

  const res = await request.post(props.batchDeleteApi, payload)
  if (res.code === 0) {
    ElMessage.success(t('common.deleteSuccess') || 'Delete success')
    selectedRows.value = []
    loadData()
    emit('data-changed')
  }
}

// Export handling
async function handleExport(command) {
  if (!props.exportApi) return

  let data = []
  let filename = ''

  if (command === 'selected') {
    if (selectedRows.value.length === 0) {
      ElMessage.warning(t('common.pleaseSelect') || 'Please select items first')
      return
    }
    data = selectedRows.value
    filename = `export_selected_${new Date().getTime()}.txt`
  } else if (command === 'csv') {
    ElMessage.info(t('common.gettingAllData') || 'Getting all data...')
    try {
      const res = await request.post(props.exportApi || props.api, {
        query: searchQuery.value,
      ...searchForm, page: 0, pageSize: 0
      })
      if (res.code === 0) {
        // 处理嵌套的 data 包装情况
        const dataContainer = res.data?.list !== undefined ? res.data : res
        data = dataContainer.list || []
      } else {
        ElMessage.error(t('common.getDataFailed') || 'Failed to get data')
        return
      }
    } catch (e) {
      ElMessage.error(t('common.getDataFailed') || 'Failed to get data')
      return
    }

    if (data.length === 0) {
      ElMessage.warning(t('common.noDataToExport') || 'No data to export')
      return
    }

    const exportColumns = props.columns.filter(c => c.label && c.prop)
    const headers = exportColumns.map(c => c.label)
    const csvRows = [headers.join(',')]

    for (const row of data) {
      const values = exportColumns.map(c => {
        let val = row[c.prop]

        if (props.csvFormatter) {
          const formattedVal = props.csvFormatter(row, c)
          if (formattedVal !== undefined) {
             val = formattedVal
          }
        }

        if (Array.isArray(val)) {
          val = val.join('; ')
        } else if (typeof val === 'object' && val !== null) {
          val = JSON.stringify(val)
        }
        return escapeCsvField(val)
      })
      csvRows.push(values.join(','))
    }

    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `export_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)

    ElMessage.success(t('common.exportSuccess', { count: data.length }) || `Exported ${data.length} items`)
    return
    } else {
    // Export All (TXT)
    ElMessage.info(t('common.gettingAllData') || 'Getting all data...')
    try {
      const res = await request.post(props.exportApi || props.api, {
        query: searchQuery.value,
      ...searchForm, page: 0, pageSize: 0
      })
      if (res.code === 0) {
        // 处理嵌套的 data 包装情况
        const dataContainer = res.data?.list !== undefined ? res.data : res
        data = dataContainer.list || []
      } else {
        ElMessage.error(t('common.getDataFailed') || 'Failed to get data')
        return
      }
    } catch (e) {
      ElMessage.error(t('common.getDataFailed') || 'Failed to get data')
      return
    }
    filename = `export_all_${new Date().getTime()}.txt`
  }

  if (data.length === 0) {
    ElMessage.warning(t('common.noDataToExport') || 'No data to export')
    return
  }

  const seen = new Set()
  const exportData = []
  for (const row of data) {
    const keyVal = row[props.rowKey]
    if (keyVal && !seen.has(keyVal)) {
      seen.add(keyVal)
      exportData.push(keyVal)
    }
  }

  if (exportData.length === 0) {
    ElMessage.warning(t('common.noDataToExport') || 'No data to export')
    return
  }

  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)

  ElMessage.success(t('common.exportSuccess', { count: exportData.length }) || `Exported ${exportData.length} items`)
}

// CSV field escaping helper
function escapeCsvField(field) {
  if (field == null) return ''
  const str = String(field)
  if (str.includes(',') || str.includes('"') || str.includes('\n') || str.includes('\r')) {
    return '"' + str.replace(/"/g, '""') + '"'
  }
  return str
}

onMounted(() => {
  initQueryFromUrl()
  loadData()
})

onUnmounted(() => {
})

// Expose methods and state to parent component
defineExpose({
  tableData,
  selectedRows,
  loading,
  pagination,
  searchForm,
  sortState,
  loadData,
  handleSearch
})
</script>

<style scoped lang="scss">
.pro-table-wrapper {
  display: flex;
  flex-direction: column;
  height: 100%;

  :deep(.el-card__body) {
    display: flex;
    flex-direction: column;
    padding: 16px;
    height: 100%;
    box-sizing: border-box;
  }
}


.pro-table-top-toolbar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  align-items: center;

  .global-search {
    flex: 1;
    max-width: 500px;

    .search-input {
      width: 100%;
    }
  }

  .header-actions {
    display: flex;
    gap: 8px;
  }
}

.filters-panel {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 16px;

  :deep(.el-form-item) {
    margin-bottom: 16px;
  }

  .filter-actions-row {
    margin-top: 8px;
    display: block;
  }
}

.pro-table-batch-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;

  .batch-left, .batch-right {
    display: flex;
    align-items: center;
    gap: 12px;
  }
}

.pro-table-footer {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>