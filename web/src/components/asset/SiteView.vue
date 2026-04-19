<template>
  <div class="site-view">
    <ProTable
      ref="proTableRef"
      api="/asset/site/list"
      statApi="/asset/site/stat"
      batchDeleteApi="/asset/site/batchDelete"
      rowKey="id"
      :columns="siteColumns"
      :searchItems="siteSearchItems"
      :statLabels="statLabels"
      :csvFormatter="csvFormatter"
      selection
      :searchPlaceholder="$t('site.sitePlaceholder')"
      @data-changed="$emit('data-changed')"
      :searchKeys="['site']"
    >
      <!-- 自定义导出 -->
      <template #toolbar-left>
        <el-dropdown @command="handleExport">
          <el-button type="success" size="default">
            {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="selected-site" :disabled="selectedRows.length === 0">{{ $t('site.exportSelectedSites', { count: selectedRows.length }) }}</el-dropdown-item>
              <el-dropdown-item divided command="all-site">{{ $t('site.exportAllSites') }}</el-dropdown-item>
              <el-dropdown-item command="csv">{{ $t('common.exportCsv') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>

      <template #toolbar-right>
        <el-button type="danger" plain @click="handleClear">{{ $t('asset.clearData') }}</el-button>
      </template>

      <!-- 站点 -->
      <template #site="{ row }">
        <div class="site-cell">
          <a :href="row.site" target="_blank" class="site-link">{{ row.site }}</a>
        </div>
      </template>

      <!-- 标题 -->
      <template #title="{ row }">
        {{ row.title || '-' }}
      </template>

      <!-- 状态码 -->
      <template #statusCode="{ row }">
        <el-tag :type="getStatusType(row.httpStatus)" size="small">{{ row.httpStatus || '-' }}</el-tag>
      </template>

      <!-- 指纹 -->
      <template #fingerprint="{ row }">
        <el-tag v-for="app in (row.app || []).slice(0, 3)" :key="app" size="small" type="success" style="margin: 2px">
          {{ app }}
        </el-tag>
        <span v-if="(row.app || []).length > 3" class="more-apps">+{{ (row.app || []).length - 3 }}</span>
      </template>

      <!-- 截图 -->
      <template #screenshot="{ row }">
        <el-image
          v-if="row.screenshot"
          :src="getScreenshotUrl(row.screenshot)"
          :preview-src-list="[getScreenshotUrl(row.screenshot)]"
          :z-index="9999"
          :preview-teleported="true"
          :hide-on-click-modal="true"
          fit="cover"
          class="screenshot-img"
        />
        <span v-else>-</span>
      </template>

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
        <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
      </template>
    </ProTable>

    <!-- 详情侧边栏 -->
    <el-drawer v-model="detailVisible" :title="$t('site.siteDetail')" size="50%" direction="rtl">
      <el-descriptions :column="2" border v-if="currentSite">
        <el-descriptions-item :label="$t('site.siteAddress')" :span="2">
          <a :href="currentSite.site" target="_blank" class="site-link">{{ currentSite.site }}</a>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('site.title')" :span="2">{{ currentSite.title || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.statusCode')">
          <el-tag :type="getStatusType(currentSite.httpStatus)" size="small">{{ currentSite.httpStatus || '-' }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="Server">{{ currentSite.server || '-' }}</el-descriptions-item>
        <el-descriptions-item label="IP">{{ currentSite.ip || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.location')">{{ currentSite.location || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.organization')">{{ currentSite.orgName || $t('common.defaultOrganization') }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.createTime')">{{ currentSite.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.updateTime')">{{ currentSite.updateTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.fingerprint')" :span="2">
          <el-tag v-for="app in (currentSite.app || [])" :key="app" size="small" type="success" style="margin: 2px">
            {{ app }}
          </el-tag>
          <span v-if="!(currentSite.app || []).length">-</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('site.screenshot')" :span="2" v-if="currentSite.screenshot">
          <el-image
            :src="getScreenshotUrl(currentSite.screenshot)"
            :preview-src-list="[getScreenshotUrl(currentSite.screenshot)]"
            :z-index="9999"
            :preview-teleported="true"
            :hide-on-click-modal="true"
            fit="contain"
            style="max-width: 400px; max-height: 300px; cursor: pointer;"
          />
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button @click="detailVisible = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import request from '@/api/request'
import { clearAssets } from '@/api/asset'
import ProTable from '@/components/common/ProTable.vue'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])

const proTableRef = ref(null)
const organizations = ref([])
const detailVisible = ref(false)
const currentSite = ref(null)

const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

const statLabels = computed(() => ({
  total: t('site.totalSites'),
  httpCount: t('site.httpSites'),
  httpsCount: t('site.httpsSites'),
  newCount: t('site.newSites')
}))

const siteColumns = computed(() => [
  { label: t('site.site'), prop: 'site', slot: 'site', minWidth: 280 },
  { label: t('site.title'), prop: 'title', slot: 'title', minWidth: 200, showOverflowTooltip: true },
  { label: t('site.statusCode'), prop: 'httpStatus', slot: 'statusCode', width: 80, align: 'center' },
  { label: t('site.fingerprint'), prop: 'app', slot: 'fingerprint', minWidth: 180 },
  { label: t('site.screenshot'), prop: 'screenshot', slot: 'screenshot', width: 100, align: 'center' },
  { label: t('common.createTime'), prop: 'createTime', width: 160 },
  { label: t('common.updateTime'), prop: 'updateTime', width: 160 },
  { label: t('common.operation'), slot: 'operation', width: 120, fixed: 'right' }
])

const siteSearchItems = computed(() => [
  { label: t('site.site'), prop: 'site', type: 'input', placeholder: t('site.sitePlaceholder') },
  { label: t('site.title'), prop: 'title', type: 'input', placeholder: t('site.titlePlaceholder') },
  { label: t('site.app'), prop: 'app', type: 'input', placeholder: t('site.appPlaceholder') },
  { label: t('site.statusCode'), prop: 'httpStatus', type: 'input', placeholder: '200/404...' },
  {
    label: t('site.organization'),
    prop: 'orgId',
    type: 'select',
    options: [
      { label: t('common.allOrganizations'), value: '' },
      ...organizations.value.map(org => ({ label: org.name, value: org.id }))
    ]
  }
])

function csvFormatter(row, col) {
  if (col.prop === 'app') {
    return (row.app || []).join(';')
  }
}

async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    if (res.code === 0) organizations.value = res.list || []
  } catch (e) { console.error(e) }
}

function getStatusType(status) {
  if (!status) return 'info'
  const code = parseInt(status)
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'warning'
  if (code >= 400) return 'danger'
  return 'info'
}

function getScreenshotUrl(screenshot) {
  if (!screenshot) return ''
  if (screenshot.startsWith('data:') || screenshot.startsWith('/9j/') || screenshot.startsWith('iVBOR')) {
    return screenshot.startsWith('data:') ? screenshot : `data:image/png;base64,${screenshot}`
  }
  return `/api/screenshot/${screenshot}`
}

function showDetail(row) {
  currentSite.value = row
  detailVisible.value = true
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('site.confirmDeleteSite'), t('common.tip'), { type: 'warning' })
    const res = await request.post('/asset/site/batchDelete', { ids: [row.id] })
    if (res.code === 0) {
      ElMessage.success(t('common.deleteSuccess'))
      proTableRef.value?.loadData()
      emit('data-changed')
    }
  } catch (e) {
    // cancelled
  }
}

async function handleClear() {
  try {
    await ElMessageBox.confirm(t('asset.confirmClearAll'), t('common.warning'), {
      type: 'error',
      confirmButtonText: t('asset.confirmClearBtn'),
      cancelButtonText: t('common.cancel')
    })
    const res = await clearAssets()
    if (res.code === 0) {
      ElMessage.success(res.msg || t('asset.clearSuccess'))
      proTableRef.value?.loadData()
      emit('data-changed')
    } else {
      ElMessage.error(res.msg || t('asset.clearFailed'))
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('清空资产失败:', e)
      ElMessage.error(t('asset.clearFailed'))
    }
  }
}

async function handleExport(command) {
  let data = []
  let filename = ''

  if (command === 'selected-site') {
    if (selectedRows.value.length === 0) {
      ElMessage.warning(t('site.pleaseSelectSites'))
      return
    }
    data = selectedRows.value
    filename = 'sites_selected.txt'
  } else if (command === 'csv') {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/asset/site/list', {
        ...proTableRef.value?.searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
    } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }

    if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

    const headers = ['Site', 'Title', 'StatusCode', 'Server', 'IP', 'Location', 'Apps', 'Organization', 'CreateTime', 'UpdateTime']
    const csvRows = [headers.join(',')]
    for (const row of data) {
      const values = [
        escapeCsvField(row.site || ''),
        escapeCsvField(row.title || ''),
        row.httpStatus || '',
        escapeCsvField(row.server || ''),
        escapeCsvField(row.ip || ''),
        escapeCsvField(row.location || ''),
        escapeCsvField((row.app || []).join(';')),
        escapeCsvField(row.orgName || ''),
        escapeCsvField(row.createTime || ''),
        escapeCsvField(row.updateTime || '')
      ]
      csvRows.push(values.join(','))
    }
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `sites_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link); link.click(); document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(t('asset.exportSuccess', { count: data.length }))
    return
  } else {
    ElMessage.info(t('asset.gettingAllData'))
    try {
      const res = await request.post('/asset/site/list', {
        ...proTableRef.value?.searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
    } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }
    filename = 'sites_all.txt'
  }

  if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

  const seen = new Set()
  const exportData = []
  for (const row of data) {
    if (row.site && !seen.has(row.site)) { seen.add(row.site); exportData.push(row.site) }
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

function handleWorkspaceChanged() {
  loadOrganizations()
}

onMounted(() => {
  loadOrganizations()
  window.addEventListener('workspace-changed', handleWorkspaceChanged)
})

onUnmounted(() => {
  window.removeEventListener('workspace-changed', handleWorkspaceChanged)
})

function refresh() {
  proTableRef.value?.loadData()
}

defineExpose({ refresh })
</script>

<style scoped lang="scss">
.site-view {
  height: 100%;

  .site-cell .site-link {
    color: #409eff;
    text-decoration: none;
    font-family: monospace;
    &:hover { text-decoration: underline; }
  }
  .more-apps {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .screenshot-img {
    width: 80px;
    height: 60px;
    border-radius: 4px;
    cursor: pointer;
  }
}
</style>
