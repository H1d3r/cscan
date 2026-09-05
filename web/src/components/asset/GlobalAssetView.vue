<template>
  <div class="global-asset-view">
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

    <el-alert
      v-if="requestStatus === 'forbidden'"
      :title="$t('asset.attackSurface.dataForbidden')"
      type="warning"
      show-icon
      :closable="false"
    />
    <el-alert
      v-else-if="requestStatus === 'error'"
      :title="$t('asset.attackSurface.loadFailed')"
      type="error"
      show-icon
      :closable="false"
    >
      <template #default>
        <el-button link type="primary" @click="fetchData">{{ $t('asset.attackSurface.retry') }}</el-button>
      </template>
    </el-alert>

    <el-table
      :data="list"
      v-loading="loading"
      class="global-table row-clickable"
      @row-click="openAssetDetail"
    >
      <el-table-column :label="$t('asset.globalView.colSubdomain')" min-width="220">
        <template #default="{ row }">
          <div class="endpoint-cell">
            <a class="endpoint-host" :href="serviceUrl(row)" target="_blank" rel="noopener" @click.stop>{{ row.host || '-' }}</a>
            <div v-if="row.title" class="endpoint-title">{{ row.title }}</div>
            <div v-if="row.domain && row.domain !== row.host" class="endpoint-meta">{{ row.domain }}</div>
            <div v-if="row.cname" class="endpoint-meta">CNAME: {{ row.cname }}</div>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.globalView.colPort')" width="115">
        <template #default="{ row }">
          <div class="port-cell">
            <strong>{{ row.port || '-' }}</strong>
            <span>{{ row.service || '-' }}</span>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.globalView.colIp')" min-width="160">
        <template #default="{ row }">
          <div v-if="row.ips && row.ips.length" class="ip-list">
            <span v-for="ip in row.ips.slice(0, 2)" :key="ip" class="ip-badge">{{ ip }}</span>
            <span v-if="row.ips.length > 2" class="muted">+{{ row.ips.length - 2 }}</span>
          </div>
          <span v-else class="muted">{{ row.ip || '-' }}</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.globalView.colApplication')" min-width="200">
        <template #default="{ row }">
          <div v-if="row.technologies && row.technologies.length" class="tech-list">
            <TechTag v-for="tech in row.technologies.slice(0, 4)" :key="tech" :tech="tech" class="tech-tag" />
            <span v-if="row.technologies.length > 4" class="tech-more">+{{ row.technologies.length - 4 }}</span>
          </div>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.globalView.colStatus')" width="100" align="center">
        <template #default="{ row }">
          <span
            v-if="getStatusCodeText(row.status)"
            class="status-badge"
            :class="getStatusCodeClass(row.status)"
          >{{ getStatusCodeText(row.status) }}</span>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.globalView.colTls')" min-width="220">
        <template #default="{ row }">
          <div v-if="row.tls" class="tls-cell">
            <div class="tls-heading">
              <el-tag :type="tlsTagType(row.tls.status)" size="small">{{ tlsStatusLabel(row.tls.status) }}</el-tag>
              <el-button link type="primary" @click.stop="showCertificates(row)">{{ $t('asset.globalView.viewCertificate') }}</el-button>
            </div>
            <div class="tls-subject">{{ row.tls.subjectCn || '-' }}</div>
            <div v-if="row.tls.issuerOrg" class="tls-meta">{{ row.tls.issuerOrg }}</div>
            <div v-if="row.tls.notAfter" class="tls-meta">{{ $t('asset.globalView.tlsExpires', { date: formatCertificateExpiry(row.tls.notAfter) }) }}</div>
          </div>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.globalView.colScreenshot')" width="112" align="center">
        <template #default="{ row }">
          <ScreenshotHoverPreview
            v-if="row.screenshot"
            :src="formatScreenshotUrl(row.screenshot)"
            :alt="row.title || row.host"
          >
            <el-image
              :src="formatScreenshotUrl(row.screenshot)"
              fit="cover"
              lazy
              class="screenshot-thumb"
            >
              <template #error>
                <div class="screenshot-fallback"><el-icon><Picture /></el-icon></div>
              </template>
            </el-image>
          </ScreenshotHoverPreview>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.targetView.labels')" min-width="150">
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

    <el-drawer
      v-model="certDrawerVisible"
      :title="$t('asset.globalView.certificateDrawer', { target: certTarget })"
      size="58%"
    >
      <el-table :data="certList" v-loading="certLoading">
        <el-table-column :label="$t('asset.globalView.colService')" min-width="180">
          <template #default="{ row }">
            <span class="mono">{{ row.host }}:{{ row.port }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.globalView.certificateSubject')" min-width="220">
          <template #default="{ row }">{{ row.subject?.commonName || row.subjectDN || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('asset.globalView.certificateIssuer')" min-width="220">
          <template #default="{ row }">{{ row.issuer?.organization || row.issuerDN || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('asset.globalView.certificateExpiry')" min-width="170" prop="notAfter" />
        <template #empty>
          <div class="empty-wrap">{{ $t('asset.globalView.certificateEmpty') }}</div>
        </template>
      </el-table>
    </el-drawer>

    <AssetDetailSheet
      v-model="assetSheetOpen"
      :asset-id="assetSheetId"
      :target-id="targetId"
    />

    <div v-if="total > 0" class="pagination-wrap">
      <el-pagination
        :current-page="page"
        :page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
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
import { Search, Refresh, Download, Picture } from '@element-plus/icons-vue'
import { getAssetInventory, getAssetFilterOptions } from '@/api/asset'
import { getCertList } from '@/api/cert'
import { getStatusCodeClass, getStatusCodeText } from './targetViewUtils'
import { formatScreenshotUrl } from '@/utils/screenshot'
import ScreenshotHoverPreview from '@/components/common/ScreenshotHoverPreview.vue'
import AssetDetailSheet from './AssetDetailSheet.vue'
import TechTag from '@/components/common/TechTag.vue'

const { t } = useI18n()

const props = defineProps({
  scopeQuery: {
    type: String,
    default: ''
  },
  targetId: {
    type: String,
    default: ''
  },
  metricFilters: {
    type: Object,
    default: () => ({})
  }
})

const loading = ref(false)
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const requestStatus = ref('idle')

const filters = reactive({ query: props.scopeQuery, ports: [], statusCodes: [], technologies: [], labels: [] })
const filterOptions = ref({ ports: [], statusCodes: [], technologies: [], labels: [] })

const exporting = ref(false)
const exportProgress = ref(0)
const certDrawerVisible = ref(false)
const certLoading = ref(false)
const certTarget = ref('')
const certList = ref([])
const assetSheetOpen = ref(false)
const assetSheetId = ref('')

let queryDebounce = null
let dataRequestSeq = 0
let filterOptionsRequestSeq = 0
let certRequestSeq = 0

function buildParams(requestPage, requestPageSize) {
  return {
    page: requestPage,
    pageSize: requestPageSize,
    targetId: props.targetId || undefined,
    query: filters.query || undefined,
    ports: filters.ports.length ? filters.ports : undefined,
    statusCodes: filters.statusCodes.length ? filters.statusCodes : undefined,
    technologies: filters.technologies.length ? filters.technologies : undefined,
    labels: filters.labels.length ? filters.labels : undefined,
    ...(props.metricFilters || {}),
  }
}

function isForbidden(error) {
  return error?.response?.status === 403 || Number(error?.status || error?.code) === 403
}

async function fetchData() {
  const seq = ++dataRequestSeq
  loading.value = true
  requestStatus.value = 'loading'
  try {
    const res = await getAssetInventory(buildParams(page.value, pageSize.value))
    if (seq !== dataRequestSeq) return
    const payload = res?.data ?? res
    list.value = payload?.list || []
    total.value = payload?.total || 0
    requestStatus.value = 'success'
  } catch (err) {
    if (seq !== dataRequestSeq) return
    console.error('[GlobalAssetView] fetchData error:', err)
    requestStatus.value = isForbidden(err) ? 'forbidden' : 'error'
  } finally {
    if (seq === dataRequestSeq) loading.value = false
  }
}

async function fetchFilterOptions() {
  const seq = ++filterOptionsRequestSeq
  try {
    const res = await getAssetFilterOptions({ targetId: props.targetId || undefined })
    if (seq !== filterOptionsRequestSeq) return
    filterOptions.value = res?.data || res || {}
  } catch (err) {
    if (seq === filterOptionsRequestSeq) console.error('[GlobalAssetView] filterOptions error:', err)
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

function openAssetDetail(row) {
  if (!row?.id) return
  assetSheetId.value = row.id
  assetSheetOpen.value = true
}

function tlsTagType(status) {
  if (status === 'expired') return 'danger'
  if (status === 'expiring') return 'warning'
  return 'success'
}

function tlsStatusLabel(status) {
  return t(`asset.globalView.tlsStatus.${status || 'valid'}`)
}

function formatCertificateExpiry(value) {
  const date = new Date(Number(value))
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleDateString()
}

async function showCertificates(row) {
  const seq = ++certRequestSeq
  certTarget.value = `${row.host}:${row.port}`
  certDrawerVisible.value = true
  certLoading.value = true
  certList.value = []
  try {
    const res = await getCertList({ host: row.host, port: row.port, page: 1, pageSize: 100 })
    if (seq !== certRequestSeq) return
    certList.value = res?.list || res?.data?.list || []
  } catch (err) {
    if (seq === certRequestSeq) console.error('[GlobalAssetView] certificate query error:', err)
  } finally {
    if (seq === certRequestSeq) certLoading.value = false
  }
}

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

watch(() => props.scopeQuery, value => {
  if (filters.query === value) return
  filters.query = value || ''
  page.value = 1
})

watch(
  () => [props.targetId, props.metricFilters],
  () => {
    page.value = 1
    fetchFilterOptions()
    fetchData()
  },
  { deep: true }
)

watch(certDrawerVisible, visible => {
  if (!visible) {
    certRequestSeq += 1
    certLoading.value = false
  }
})

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
  dataRequestSeq += 1
  filterOptionsRequestSeq += 1
  certRequestSeq += 1
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

.global-table {
  width: 100%;
}

.row-clickable {
  :deep(.el-table__body tr) {
    cursor: pointer;
  }

  :deep(.el-table__body tr:hover > td.el-table__cell) {
    background: var(--el-fill-color-light);
  }
}

.endpoint-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.endpoint-host {
  font-weight: 500;
  font-size: 14px;
  color: var(--el-color-primary);
  text-decoration: none;
  word-break: break-all;

  &:hover {
    text-decoration: underline;
  }
}

.endpoint-title,
.endpoint-meta,
.tls-meta {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.port-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;

  span {
    color: var(--el-text-color-secondary);
    font-size: 12px;
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

.tls-cell {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.tls-heading {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tls-subject {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.screenshot-thumb {
  width: 72px;
  height: 48px;
  cursor: default;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
}

.screenshot-fallback,
.screenshot-hover-error {
  display: grid;
  color: var(--el-text-color-secondary);
  place-items: center;
  background: var(--el-fill-color-light);
}

.screenshot-fallback {
  width: 72px;
  height: 48px;
}

.screenshot-hover-preview,
.screenshot-hover-error {
  width: min(50vw, 960px);
  height: min(50vh, 620px);
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 32px);
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

.mono {
  font-family: var(--el-font-family-mono, monospace);
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
