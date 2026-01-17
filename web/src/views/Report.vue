<template>
  <div class="report-page">
    <!-- 报告头部 -->
    <el-card class="report-header" v-if="reportData">
      <div class="header-content">
        <div class="title-section">
          <h2>{{ reportData.taskName }}</h2>
          <el-tag :type="getStatusType(reportData.status)" size="large">{{ reportData.status }}</el-tag>
        </div>
        <div class="action-section">
          <el-button type="primary" @click="handleExport" :loading="exporting">
            <el-icon><Download /></el-icon>{{ $t('report.exportExcel') }}
          </el-button>
          <el-button @click="goBack">
            <el-icon><Back /></el-icon>{{ $t('report.back') }}
          </el-button>
        </div>
      </div>
      <el-descriptions :column="5" border class="task-info">
        <el-descriptions-item :label="$t('report.scanTarget')">
          <div class="target-text">{{ reportData.target }}</div>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('common.createTime')">{{ reportData.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('report.assetCount')">
          <span class="stat-number">{{ reportData.assetCount }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('report.vulCount')">
          <span class="stat-number danger">{{ reportData.vulCount }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('report.dirScanCount')">
          <span class="stat-number info">{{ reportData.dirScanCount || 0 }}</span>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- Tab页切换展示 -->
    <el-tabs v-model="activeTab" type="border-card" class="report-tabs" v-if="reportData">
      <!-- 概览Tab -->
      <el-tab-pane :label="$t('report.overview')" name="overview">
        <el-row :gutter="20" class="stats-row">
          <!-- 漏洞统计 -->
          <el-col :span="6">
            <el-card class="stat-card">
              <template #header>{{ $t('report.vulLevelDistribution') }}</template>
              <div class="vul-stats">
                <div class="vul-item critical">
                  <span class="label">Critical</span>
                  <span class="count">{{ reportData.vulStats?.critical || 0 }}</span>
                </div>
                <div class="vul-item high">
                  <span class="label">High</span>
                  <span class="count">{{ reportData.vulStats?.high || 0 }}</span>
                </div>
                <div class="vul-item medium">
                  <span class="label">Medium</span>
                  <span class="count">{{ reportData.vulStats?.medium || 0 }}</span>
                </div>
                <div class="vul-item low">
                  <span class="label">Low</span>
                  <span class="count">{{ reportData.vulStats?.low || 0 }}</span>
                </div>
                <div class="vul-item info">
                  <span class="label">Info</span>
                  <span class="count">{{ reportData.vulStats?.info || 0 }}</span>
                </div>
              </div>
            </el-card>
          </el-col>
          <!-- 端口统计 -->
          <el-col :span="6">
            <el-card class="stat-card">
              <template #header>{{ $t('report.topPorts') }}</template>
              <div class="top-list">
                <div v-for="item in topPorts" :key="item.name" class="top-item">
                  <span class="name">{{ item.name }}</span>
                  <span class="count">{{ item.count }}</span>
                </div>
              </div>
            </el-card>
          </el-col>
          <!-- 服务统计 -->
          <el-col :span="6">
            <el-card class="stat-card">
              <template #header>{{ $t('report.topServices') }}</template>
              <div class="top-list">
                <div v-for="item in topServices" :key="item.name" class="top-item">
                  <span class="name">{{ item.name || '-' }}</span>
                  <span class="count">{{ item.count }}</span>
                </div>
              </div>
            </el-card>
          </el-col>
          <!-- 应用统计 -->
          <el-col :span="6">
            <el-card class="stat-card">
              <template #header>{{ $t('report.topApps') }}</template>
              <div class="top-list">
                <div v-for="item in topApps" :key="item.name" class="top-item">
                  <span class="name">{{ item.name }}</span>
                  <span class="count">{{ item.count }}</span>
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <!-- 目录扫描统计 -->
        <el-row :gutter="16" class="dirscan-stat-row" v-if="reportData.dirScanCount > 0">
          <el-col :span="4">
            <el-card class="stat-card-small">
              <div class="stat-value">{{ reportData.dirScanStat?.total || 0 }}</div>
              <div class="stat-label">{{ $t('report.dirScanTotal') }}</div>
            </el-card>
          </el-col>
          <el-col :span="4">
            <el-card class="stat-card-small status-2xx">
              <div class="stat-value">{{ reportData.dirScanStat?.status_2xx || 0 }}</div>
              <div class="stat-label">2xx</div>
            </el-card>
          </el-col>
          <el-col :span="4">
            <el-card class="stat-card-small status-3xx">
              <div class="stat-value">{{ reportData.dirScanStat?.status_3xx || 0 }}</div>
              <div class="stat-label">3xx</div>
            </el-card>
          </el-col>
          <el-col :span="4">
            <el-card class="stat-card-small status-4xx">
              <div class="stat-value">{{ reportData.dirScanStat?.status_4xx || 0 }}</div>
              <div class="stat-label">4xx</div>
            </el-card>
          </el-col>
          <el-col :span="4">
            <el-card class="stat-card-small status-5xx">
              <div class="stat-value">{{ reportData.dirScanStat?.status_5xx || 0 }}</div>
              <div class="stat-label">5xx</div>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>

      <!-- 资产列表Tab -->
      <el-tab-pane :label="$t('report.assetList')" name="assets">
        <div class="tab-header">
          <span class="total-info">{{ $t('common.total') }} {{ reportData.assets?.length || 0 }} {{ $t('report.records') }}</span>
          <el-input v-model="assetSearch" :placeholder="$t('report.searchAsset')" style="width: 250px" clearable />
        </div>
        <el-table :data="filteredAssets" stripe max-height="500">
          <el-table-column prop="authority" :label="$t('report.address')" min-width="200" show-overflow-tooltip>
            <template #default="{ row }">
              <div class="asset-cell">
                <span class="authority">{{ row.authority }}</span>
                <el-tag v-if="row.httpStatus" size="small" :type="getHttpStatusType(row.httpStatus)">
                  {{ row.httpStatus }}
                </el-tag>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="service" :label="$t('report.service')" width="100" />
          <el-table-column prop="title" :label="$t('report.pageTitle')" min-width="200" show-overflow-tooltip />
          <el-table-column :label="$t('report.app')" min-width="200">
            <template #default="{ row }">
              <div class="app-tags">
                <el-tooltip v-for="app in (row.app || [])" :key="app" :content="getAppSource(app)" placement="top">
                  <el-tag size="small" :type="getAppTagType(app)" class="app-tag">{{ getAppName(app) }}</el-tag>
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="server" label="Server" width="120" show-overflow-tooltip />
          <el-table-column :label="$t('report.screenshot')" width="100">
            <template #default="{ row }">
              <el-image v-if="row.screenshot" :src="formatScreenshotUrl(row.screenshot)" 
                :preview-src-list="[formatScreenshotUrl(row.screenshot)]" :z-index="9999" :preview-teleported="true"
                fit="cover" lazy style="width: 60px; height: 40px; cursor: pointer; border-radius: 4px">
                <template #error><div class="image-error"><el-icon><Picture /></el-icon></div></template>
              </el-image>
              <span v-else>-</span>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 漏洞列表Tab -->
      <el-tab-pane :label="$t('report.vulList')" name="vuls" v-if="reportData.vuls?.length > 0">
        <div class="tab-header">
          <span class="total-info">{{ $t('common.total') }} {{ reportData.vuls?.length || 0 }} {{ $t('report.records') }}</span>
          <el-input v-model="vulSearch" :placeholder="$t('report.searchVul')" style="width: 250px" clearable />
        </div>
        <el-table :data="filteredVuls" stripe max-height="500">
          <el-table-column prop="severity" :label="$t('report.level')" width="100">
            <template #default="{ row }">
              <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="authority" :label="$t('report.target')" width="180" show-overflow-tooltip />
          <el-table-column prop="url" label="URL" min-width="250" show-overflow-tooltip />
          <el-table-column prop="pocFile" label="POC" min-width="200" show-overflow-tooltip />
          <el-table-column prop="result" :label="$t('report.result')" min-width="200" show-overflow-tooltip />
          <el-table-column prop="createTime" :label="$t('report.discoveryTime')" width="160" />
        </el-table>
      </el-tab-pane>

      <!-- 目录扫描Tab -->
      <el-tab-pane :label="$t('report.dirScan')" name="dirscan" v-if="reportData.dirScanCount > 0">
        <div class="tab-header">
          <span class="total-info">{{ $t('common.total') }} {{ Object.keys(groupedDirScans).length }} {{ $t('report.targets') }}，{{ reportData.dirScans?.length || 0 }} {{ $t('report.records') }}</span>
          <div class="tab-actions">
            <el-button size="small" @click="expandAllDirScan">{{ $t('report.expandAll') }}</el-button>
            <el-button size="small" @click="collapseAllDirScan">{{ $t('report.collapseAll') }}</el-button>
            <el-button type="success" size="small" @click="exportDirScanUrls">{{ $t('report.exportUrl') }}</el-button>
          </div>
        </div>
        <el-collapse v-model="dirScanActiveNames" class="target-collapse">
          <el-collapse-item v-for="(items, authority) in groupedDirScans" :key="authority" :name="authority">
            <template #title>
              <div class="collapse-title">
                <span class="target-name">{{ authority }}</span>
                <el-badge :value="items.length" :max="999" type="primary" style="margin-left: 10px" />
              </div>
            </template>
            <el-table :data="items" stripe size="small" max-height="400">
              <el-table-column prop="url" label="URL" min-width="300" show-overflow-tooltip>
                <template #default="{ row }">
                  <a :href="row.url" target="_blank" rel="noopener" class="url-link">{{ row.url }}</a>
                </template>
              </el-table-column>
              <el-table-column prop="path" :label="$t('report.path')" min-width="120" show-overflow-tooltip />
              <el-table-column prop="statusCode" :label="$t('report.statusCode')" width="90">
                <template #default="{ row }">
                  <el-tag :type="getDirScanStatusType(row.statusCode)" size="small">{{ row.statusCode }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="contentLength" :label="$t('report.size')" width="90">
                <template #default="{ row }">{{ formatSize(row.contentLength) }}</template>
              </el-table-column>
              <el-table-column prop="title" :label="$t('report.pageTitle')" min-width="120" show-overflow-tooltip />
              <el-table-column prop="createTime" :label="$t('report.discoveryTime')" width="150" />
            </el-table>
          </el-collapse-item>
        </el-collapse>
        <el-empty v-if="Object.keys(groupedDirScans).length === 0" :description="$t('report.noDirScanData')" />
      </el-tab-pane>
    </el-tabs>

    <!-- 加载状态 -->
    <div v-if="loading" class="loading-container">
      <el-icon class="is-loading" :size="40"><Loading /></el-icon>
      <p>{{ $t('report.loadingReport') }}</p>
    </div>
    
    <!-- 无数据状态 -->
    <div v-if="!loading && reportData && reportData.assetCount === 0 && reportData.vulCount === 0 && reportData.dirScanCount === 0" class="empty-container">
      <el-empty :description="$t('report.noScanResult')">
        <template #description>
          <p>{{ $t('report.noScanResult') }}</p>
          <p class="empty-hint">{{ $t('report.noScanResultReason') }}</p>
        </template>
      </el-empty>
    </div>
    
    <!-- 任务不存在 -->
    <div v-if="!loading && !reportData" class="empty-container">
      <el-empty :description="$t('report.taskNotExist')" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Download, Back, Loading, Picture } from '@element-plus/icons-vue'
import { getReportDetail, exportReport } from '@/api/report'
import { formatScreenshotUrl } from '@/utils/screenshot'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const exporting = ref(false)
const reportData = ref(null)
const assetSearch = ref('')
const vulSearch = ref('')
const activeTab = ref('overview')
const dirScanActiveNames = ref([])

const topPorts = computed(() => (reportData.value?.topPorts || []).slice(0, 5))
const topServices = computed(() => (reportData.value?.topServices || []).slice(0, 5))
const topApps = computed(() => (reportData.value?.topApps || []).slice(0, 5))

const filteredAssets = computed(() => {
  const assets = reportData.value?.assets || []
  if (!assetSearch.value) return assets
  const keyword = assetSearch.value.toLowerCase()
  return assets.filter(a => 
    a.authority?.toLowerCase().includes(keyword) ||
    a.title?.toLowerCase().includes(keyword) ||
    a.service?.toLowerCase().includes(keyword) ||
    (a.app || []).some(app => app.toLowerCase().includes(keyword))
  )
})

const filteredVuls = computed(() => {
  const vuls = reportData.value?.vuls || []
  if (!vulSearch.value) return vuls
  const keyword = vulSearch.value.toLowerCase()
  return vuls.filter(v => 
    v.authority?.toLowerCase().includes(keyword) ||
    v.url?.toLowerCase().includes(keyword) ||
    v.pocFile?.toLowerCase().includes(keyword) ||
    v.severity?.toLowerCase().includes(keyword)
  )
})

// 按目标分组目录扫描结果
const groupedDirScans = computed(() => {
  const dirScans = reportData.value?.dirScans || []
  const groups = {}
  for (const item of dirScans) {
    const key = item.authority || 'unknown'
    if (!groups[key]) groups[key] = []
    groups[key].push(item)
  }
  return groups
})

onMounted(() => {
  const taskId = route.query.taskId
  if (taskId) {
    loadReport(taskId)
  } else {
    ElMessage.error(t('report.missingTaskId'))
    router.push('/task')
  }
})

async function loadReport(taskId) {
  loading.value = true
  try {
    const res = await getReportDetail({ taskId })
    if (res.code === 0) {
      reportData.value = res.data
      // 默认展开第一个目录扫描目标
      const keys = Object.keys(groupedDirScans.value)
      if (keys.length > 0) dirScanActiveNames.value = [keys[0]]
    } else {
      ElMessage.error(res.msg || t('report.loadReportFailed'))
    }
  } catch (e) {
    ElMessage.error(t('report.loadReportFailed'))
  } finally {
    loading.value = false
  }
}

async function handleExport() {
  if (!reportData.value) return
  exporting.value = true
  try {
    const res = await exportReport({ taskId: reportData.value.taskId })
    const blob = new Blob([res], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `report_${reportData.value.taskName}_${new Date().toISOString().slice(0,10)}.xlsx`
    link.click()
    window.URL.revokeObjectURL(url)
    ElMessage.success(t('report.exportSuccess'))
  } catch (e) {
    ElMessage.error(t('report.exportFailed'))
  } finally {
    exporting.value = false
  }
}

function goBack() { router.push('/task') }

function expandAllDirScan() { dirScanActiveNames.value = Object.keys(groupedDirScans.value) }
function collapseAllDirScan() { dirScanActiveNames.value = [] }

function exportDirScanUrls() {
  const dirScans = reportData.value?.dirScans || []
  if (dirScans.length === 0) {
    ElMessage.warning(t('report.noDataToExport'))
    return
  }
  const seen = new Set()
  const exportData = []
  for (const row of dirScans) {
    if (row.url && !seen.has(row.url)) {
      seen.add(row.url)
      exportData.push(row.url)
    }
  }
  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `dirscan_urls_${reportData.value.taskName}.txt`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
  ElMessage.success(t('report.exportSuccessCount', { count: exportData.length }))
}

function getStatusType(status) {
  const map = { SUCCESS: 'success', FAILURE: 'danger', STARTED: 'primary', PENDING: 'warning', CREATED: 'info', STOPPED: 'info' }
  return map[status] || 'info'
}

function getHttpStatusType(status) {
  if (status?.startsWith('2')) return 'success'
  if (status?.startsWith('3')) return 'warning'
  if (status?.startsWith('4') || status?.startsWith('5')) return 'danger'
  return 'info'
}

function getSeverityType(severity) {
  const map = { critical: 'danger', high: 'danger', medium: 'warning', low: 'info', info: '', unknown: 'info' }
  return map[severity?.toLowerCase()] || 'info'
}

function getDirScanStatusType(code) {
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'warning'
  if (code >= 400 && code < 500) return 'danger'
  if (code >= 500) return 'danger'
  return 'info'
}

function formatSize(bytes) {
  if (!bytes || bytes < 0) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

function getAppName(app) {
  if (!app) return ''
  const idx = app.indexOf('[')
  return idx > 0 ? app.substring(0, idx) : app
}

function getAppSource(app) {
  if (!app) return ''
  const match = app.match(/\[([^\]]+)\]$/)
  if (match) {
    const source = match[1]
    const sourceMap = { 'httpx': 'httpx识别', 'wappalyzer': 'Wappalyzer识别', 'custom': '自定义指纹', 'builtin': '内置指纹' }
    if (source.includes('+')) {
      const parts = source.split('+')
      return parts.map(part => {
        if (part.startsWith('custom(')) {
          const ids = part.match(/custom\(([^)]+)\)/)
          return ids ? `自定义指纹(${ids[1].split(',').length}个)` : '自定义指纹'
        }
        return sourceMap[part] || part
      }).join(' + ')
    }
    if (source.startsWith('custom(')) {
      const ids = source.match(/custom\(([^)]+)\)/)
      return ids ? `自定义指纹 (${ids[1].split(',').length}个)` : '自定义指纹'
    }
    return sourceMap[source] || source
  }
  return '未知来源'
}

function getAppTagType(app) {
  if (!app) return 'info'
  if (app.includes('[httpx+wappalyzer+custom(')) return 'danger'
  if (app.includes('[httpx+wappalyzer]')) return 'primary'
  if (app.includes('[httpx]') || app.includes('[wappalyzer]')) return 'success'
  if (app.includes('[builtin]')) return 'warning'
  if (app.includes('custom(')) return 'danger'
  return 'info'
}
</script>

<style lang="scss" scoped>
.report-page {
  padding: 20px;

  .report-header {
    margin-bottom: 20px;
    .header-content {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 20px;
      .title-section {
        display: flex;
        align-items: center;
        gap: 15px;
        h2 { margin: 0; font-size: 24px; }
      }
      .action-section { display: flex; gap: 10px; }
    }
    .target-text {
      max-width: 300px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .stat-number {
      font-size: 18px;
      font-weight: bold;
      color: var(--el-color-primary);
      &.danger { color: var(--el-color-danger); }
      &.info { color: var(--el-color-info); }
    }
  }

  .report-tabs { margin-bottom: 20px; }

  .tab-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 15px;
    .total-info { color: var(--el-text-color-secondary); font-size: 14px; }
    .tab-actions { display: flex; gap: 10px; }
  }

  .stats-row {
    margin-bottom: 20px;
    .stat-card {
      height: 250px;
      .vul-stats {
        .vul-item {
          display: flex;
          justify-content: space-between;
          padding: 8px 12px;
          margin-bottom: 5px;
          border-radius: 4px;
          &.critical { background: var(--el-fill-color-light); border-left: 3px solid var(--el-color-danger); .count { color: var(--el-color-danger); } }
          &.high { background: var(--el-fill-color-light); border-left: 3px solid var(--el-color-warning); .count { color: var(--el-color-warning); } }
          &.medium { background: var(--el-fill-color-light); border-left: 3px solid var(--el-color-primary); .count { color: var(--el-color-primary); } }
          &.low { background: var(--el-fill-color-light); border-left: 3px solid var(--el-color-success); .count { color: var(--el-color-success); } }
          &.info { background: var(--el-fill-color-light); .count { color: var(--el-text-color-secondary); } }
          .count { font-weight: bold; }
        }
      }
      
html.dark .vul-stats {
  .vul-item {
    &.critical { background: rgba(245, 108, 108, 0.15); }
    &.high { background: rgba(230, 162, 60, 0.15); }
    &.medium { background: rgba(64, 158, 255, 0.15); }
    &.low { background: rgba(103, 194, 58, 0.15); }
  }
}
      .top-list {
        .top-item {
          display: flex;
          justify-content: space-between;
          padding: 6px 0;
          border-bottom: 1px solid var(--el-border-color-lighter);
          &:last-child { border-bottom: none; }
          .name { max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
          .count { color: var(--el-color-primary); font-weight: bold; }
        }
      }
    }
  }

  .dirscan-stat-row {
    margin-bottom: 20px;
    .stat-card-small {
      text-align: center;
      padding: 15px;
      .stat-value { font-size: 20px; font-weight: 600; color: var(--el-color-primary); }
      .stat-label { color: var(--el-text-color-secondary); margin-top: 5px; font-size: 12px; }
      &.status-2xx .stat-value { color: var(--el-color-success); }
      &.status-3xx .stat-value { color: var(--el-color-warning); }
      &.status-4xx .stat-value { color: var(--el-color-danger); }
      &.status-5xx .stat-value { color: var(--el-text-color-secondary); }
    }
  }

  .target-collapse {
    .collapse-title {
      display: flex;
      align-items: center;
      .target-name { font-weight: 500; color: var(--el-color-primary); }
    }
  }

  .url-link {
    color: var(--el-color-primary);
    text-decoration: none;
    &:hover { text-decoration: underline; }
  }

  .asset-cell {
    display: flex;
    align-items: center;
    gap: 8px;
    .authority { font-family: monospace; }
  }

  .app-tags { display: flex; flex-wrap: wrap; gap: 4px; }
  .app-tag { margin: 0; flex-shrink: 0; }

  .image-error {
    width: 60px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--el-fill-color-light);
    border-radius: 4px;
    color: var(--el-text-color-secondary);
  }

  .loading-container {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 300px;
    color: var(--el-text-color-secondary);
  }
  
  .empty-container {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 60px 0;
  }

  .empty-hint {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
}
</style>
