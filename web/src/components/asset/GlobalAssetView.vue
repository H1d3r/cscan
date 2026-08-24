<template>
  <div class="global-asset-view">
    <!-- Toolbar -->
    <div class="toolbar">
      <div class="toolbar-left">
        <el-input
          v-model="filters.query"
          :placeholder="$t('asset.globalView.searchPlaceholder')"
          clearable
          class="search-input"
          @clear="handleFilterChange"
          @keyup.enter="handleFilterChange"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <div class="toolbar-right">
        <el-select
          v-model="filters.ports"
          multiple
          collapse-tags
          clearable
          filterable
          :placeholder="$t('asset.targetView.subPorts')"
          class="filter-select"
          @change="handleFilterChange"
        >
          <el-option v-for="p in filterOptions.ports" :key="p" :value="p" :label="String(p)" />
        </el-select>

        <el-select
          v-model="filters.statusCodes"
          multiple
          collapse-tags
          clearable
          filterable
          :placeholder="$t('asset.targetView.subStatusCode')"
          class="filter-select"
          @change="handleFilterChange"
        >
          <el-option v-for="s in filterOptions.statusCodes" :key="s" :value="s" :label="String(s)" />
        </el-select>

        <el-select
          v-model="filters.technologies"
          multiple
          collapse-tags
          clearable
          filterable
          :placeholder="$t('asset.targetView.subTechnologies')"
          class="filter-select filter-select-lg"
          @change="handleFilterChange"
        >
          <el-option v-for="tech in filterOptions.technologies" :key="tech" :value="tech" :label="tech" />
        </el-select>

        <el-select
          v-model="filters.labels"
          multiple
          collapse-tags
          clearable
          filterable
          :placeholder="$t('asset.targetView.labels')"
          class="filter-select"
          @change="handleFilterChange"
        >
          <el-option v-for="label in filterOptions.labels" :key="label" :value="label" :label="label" />
        </el-select>

        <el-button link @click="handleReset">{{ $t('asset.targetView.filterReset') }}</el-button>

        <el-button plain :loading="loading" @click="fetchData">
          <el-icon><Refresh /></el-icon>
          {{ $t('common.refresh') }}
        </el-button>

        <el-button type="primary" plain :loading="exporting" :disabled="total === 0" @click="handleExport">
          <el-icon><Download /></el-icon>
          {{ exporting ? $t('asset.globalView.exporting', { done: exportProgress }) : $t('common.export') }}
        </el-button>
      </div>
    </div>

    <!-- Table -->
    <el-table :data="list" v-loading="loading" class="global-table">
      <el-table-column :label="$t('asset.globalView.colService')" min-width="300">
        <template #default="{ row }">
          <div class="service-cell">
            <div class="service-main">
              <a class="service-host" :href="serviceUrl(row)" target="_blank" rel="noopener">{{ row.host }}</a>
              <span class="service-separator">:</span>
              <span class="service-port">{{ row.port }}</span>
              <span
                v-if="getStatusCodeText(row.status)"
                class="status-badge"
                :class="getStatusCodeClass(row.status)"
              >{{ getStatusCodeText(row.status) }}</span>
            </div>
            <div v-if="row.title" class="service-title">{{ row.title }}</div>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.globalView.colIp')" min-width="150">
        <template #default="{ row }">
          <div v-if="row.ips && row.ips.length" class="ip-list">
            <span v-for="ip in row.ips.slice(0, 2)" :key="ip" class="ip-badge">{{ ip }}</span>
            <span v-if="row.ips.length > 2" class="muted">+{{ row.ips.length - 2 }}</span>
          </div>
          <span v-else class="muted">{{ row.ip || '-' }}</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.targetView.columnTechnologies')" min-width="220">
        <template #default="{ row }">
          <div v-if="row.technologies && row.technologies.length" class="tech-list">
            <TechTag v-for="tech in row.technologies.slice(0, 4)" :key="tech" :tech="tech" class="tech-tag" />
            <span v-if="row.technologies.length > 4" class="tech-more">+{{ row.technologies.length - 4 }}</span>
          </div>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
        <template #default="{ row }">
          <div v-if="row.labels && row.labels.length" class="tech-list">
            <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
            <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
          </div>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.targetView.colUpdateTime')" width="160">
        <template #default="{ row }">
          <span class="muted">{{ row.lastUpdated || '-' }}</span>
        </template>
      </el-table-column>

      <template #empty>
        <div class="empty-wrap">{{ $t('asset.targetView.noData') }}</div>
      </template>
    </el-table>

    <!-- Pagination -->
    <div v-if="total > 0" class="pagination-wrap">
      <el-pagination
        :current-page="page"
        :page-size="pageSize"
        :page-sizes="[20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        @current-change="handlePageChange"
        @size-change="handleSizeChange"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { Search, Refresh, Download } from '@element-plus/icons-vue'
import { getAssetInventory, getAssetFilterOptions } from '@/api/asset'
import { getStatusCodeClass, getStatusCodeText } from './targetViewUtils'
import TechTag from '@/components/common/TechTag.vue'

const { t } = useI18n()

const loading = ref(false)
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)

const filters = reactive({ query: '', ports: [], statusCodes: [], technologies: [], labels: [] })
const filterOptions = ref({ ports: [], statusCodes: [], technologies: [], labels: [] })

const exporting = ref(false)
const exportProgress = ref(0)

let queryDebounce = null

// 构建 /asset/inventory 请求参数（全部目标，无 targetId 限定）
function buildParams(page, pageSize) {
  return {
    page,
    pageSize,
    query: filters.query || undefined,
    ports: filters.ports.length ? filters.ports : undefined,
    statusCodes: filters.statusCodes.length ? filters.statusCodes : undefined,
    technologies: filters.technologies.length ? filters.technologies : undefined,
    labels: filters.labels.length ? filters.labels : undefined,
  }
}

async function fetchData() {
  loading.value = true
  try {
    const res = await getAssetInventory(buildParams(page.value, pageSize.value))
    const payload = res?.data ?? res
    list.value = payload?.list || []
    total.value = payload?.total || 0
  } catch (err) {
    console.error('[GlobalAssetView] fetchData error:', err)
  } finally {
    loading.value = false
  }
}

async function fetchFilterOptions() {
  try {
    const res = await getAssetFilterOptions({})
    filterOptions.value = res?.data || res || {}
  } catch (err) {
    console.error('[GlobalAssetView] filterOptions error:', err)
  }
}

function handleFilterChange() {
  page.value = 1
  fetchData()
}

function handleReset() {
  filters.query = ''
  filters.ports = []
  filters.statusCodes = []
  filters.technologies = []
  filters.labels = []
  handleFilterChange()
}

function handlePageChange(newPage) {
  page.value = newPage
  fetchData()
}

function handleSizeChange(newSize) {
  pageSize.value = newSize
  page.value = 1
  fetchData()
}

function serviceUrl(row) {
  const scheme = row.service === 'https' || row.port === 443 ? 'https' : 'http'
  return `${scheme}://${row.host}:${row.port}`
}

// CSV 单元格转义：含逗号/引号/换行时用双引号包裹并转义内部引号
function csvCell(value) {
  const s = value == null ? '' : String(value)
  if (/[",\n]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`
  }
  return s
}

function rowToCsv(row) {
  return [
    csvCell(row.host),
    csvCell(row.port),
    csvCell((row.ips && row.ips.length ? row.ips : [row.ip]).filter(Boolean).join(' ')),
    csvCell(row.service),
    csvCell(row.status),
    csvCell(row.title),
    csvCell((row.technologies || []).join(' ')),
    csvCell((row.labels || []).join(' ')),
    csvCell(row.domain),
    csvCell(row.firstSeen),
    csvCell(row.lastUpdatedFull),
  ].join(',')
}

// 按当前过滤条件翻页拉取全量资产并导出 CSV（前端生成，无需后端改动）
async function handleExport() {
  if (total.value === 0) return
  exporting.value = true
  exportProgress.value = 0
  const exportPageSize = 100
  const rows = []
  try {
    let curPage = 1
    let fetched = 0
    let grandTotal = total.value
    // 循环拉取直到覆盖全量；grandTotal 以首页响应为准，防止导出期间数据增长导致死循环
    while (true) {
      const res = await getAssetInventory(buildParams(curPage, exportPageSize))
      const payload = res?.data ?? res
      const pageList = payload?.list || []
      if (curPage === 1) grandTotal = payload?.total || 0
      rows.push(...pageList)
      fetched += pageList.length
      exportProgress.value = fetched
      if (pageList.length < exportPageSize || fetched >= grandTotal) break
      curPage += 1
    }

    if (rows.length === 0) {
      ElMessage.warning(t('asset.globalView.exportEmpty'))
      return
    }

    const header = [
      t('asset.globalView.csvHost'),
      t('asset.globalView.csvPort'),
      t('asset.globalView.csvIp'),
      t('asset.globalView.csvService'),
      t('asset.globalView.csvStatus'),
      t('asset.globalView.csvTitle'),
      t('asset.globalView.csvTech'),
      t('asset.globalView.csvLabels'),
      t('asset.globalView.csvDomain'),
      t('asset.globalView.csvFirstSeen'),
      t('asset.globalView.csvLastUpdated'),
    ].join(',')

    const csv = [header, ...rows.map(rowToCsv)].join('\n')
    // \uFEFF BOM 让 Excel 正确识别 UTF-8，避免中文乱码
    const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    const ts = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')
    link.href = url
    link.download = `assets-${ts}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(t('asset.globalView.exportSuccess', { count: rows.length }))
  } catch (err) {
    console.error('[GlobalAssetView] export error:', err)
    ElMessage.error(t('asset.globalView.exportFailed'))
  } finally {
    exporting.value = false
  }
}

watch(() => filters.query, () => {
  if (queryDebounce) clearTimeout(queryDebounce)
  queryDebounce = setTimeout(handleFilterChange, 500)
})

onMounted(() => {
  fetchFilterOptions()
  fetchData()
})

onUnmounted(() => {
  if (queryDebounce) clearTimeout(queryDebounce)
})
</script>

<style scoped lang="scss">
.global-asset-view {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;

  .toolbar-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
}

.search-input {
  width: 260px;
}

.filter-select {
  width: 150px;
}

.filter-select-lg {
  width: 200px;
}

.service-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;

  .service-main {
    display: flex;
    align-items: center;
    gap: 4px;

    .service-host {
      font-weight: 500;
      font-size: 14px;
      color: var(--el-color-primary);
      text-decoration: none;

      &:hover {
        text-decoration: underline;
      }
    }

    .service-separator {
      color: var(--el-text-color-secondary);
    }

    .service-port {
      font-weight: 600;
      font-size: 14px;
    }
  }

  .service-title {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.ip-list {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;

  .ip-badge {
    display: inline-flex;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 11px;
    line-height: 16px;
    background: var(--el-fill-color);
    color: var(--el-text-color-regular);
    border: 1px solid var(--el-border-color-lighter);
  }
}

.status-badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 500;
  line-height: 16px;

  &.status-2xx { background: rgba(103, 194, 58, 0.1); color: #67c23a; }
  &.status-3xx { background: rgba(64, 158, 255, 0.1); color: #409eff; }
  &.status-4xx { background: rgba(230, 162, 60, 0.1); color: #e6a23c; }
  &.status-5xx { background: rgba(245, 108, 108, 0.1); color: #f56c6c; }
  &.status-other { background: rgba(144, 147, 153, 0.1); color: #909399; }
}

.tech-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;

  .tech-more {
    font-size: 11px;
    color: var(--el-text-color-secondary);
  }
}

.label-tag {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.empty-wrap {
  padding: 32px 0;
  color: var(--el-text-color-secondary);
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  padding-top: 4px;
}
</style>
