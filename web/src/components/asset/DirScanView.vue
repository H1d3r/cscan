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
      :searchPlaceholder="$t('dirscan.targetPlaceholder')"
      :searchKeys="['authority']"
      @data-changed="$emit('data-changed')"
    >
      <!-- 自定义导出 -->
      <template #toolbar-left>
        <el-dropdown @command="handleExport">
          <el-button type="success" size="default">
            {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="all-url">{{ $t('dirscan.exportAllUrl') }}</el-dropdown-item>
              <el-dropdown-item command="csv">{{ $t('dirscan.exportCsv') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>

      <template #toolbar-right>
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

      <!-- 发现时间 -->
      <template #createTime="{ row }">
        {{ formatTime(row.createTime) }}
      </template>

      <!-- 更新时间 -->
      <template #updateTime="{ row }">
        {{ formatTime(row.scanTime) }}
      </template>

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
      </template>
    </ProTable>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import request from '@/api/request'
import ProTable from '@/components/common/ProTable.vue'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])

const proTableRef = ref(null)

const statLabels = computed(() => ({
  total: t('dirscan.total'),
  status_2xx: '2xx',
  status_3xx: '3xx',
  status_4xx: '4xx',
  status_5xx: '5xx'
}))

const dirColumns = computed(() => [
  { label: t('dirscan.target'), prop: 'authority', minWidth: 150 },
  { label: 'URL', prop: 'url', slot: 'url', minWidth: 250, showOverflowTooltip: true },
  { label: t('dirscan.path'), prop: 'path', width: 120, showOverflowTooltip: true },
  { label: t('dirscan.statusCode'), prop: 'statusCode', slot: 'statusCode', width: 80, align: 'center' },
  { label: t('dirscan.size'), prop: 'contentLength', slot: 'contentLength', width: 80, align: 'right' },
  { label: t('task.contentWords'), prop: 'contentWords', width: 70, align: 'right' },
  { label: t('task.contentLines'), prop: 'contentLines', width: 70, align: 'right' },
  { label: t('task.duration'), prop: 'duration', slot: 'duration', width: 80, align: 'right' },
  { label: t('dirscan.title'), prop: 'title', minWidth: 100, showOverflowTooltip: true },
  { label: t('dirscan.contentType'), prop: 'contentType', width: 130, showOverflowTooltip: true },
  { label: t('dirscan.redirectUrl'), prop: 'redirectUrl', minWidth: 120, showOverflowTooltip: true },
  { label: t('common.createTime'), prop: 'createTime', slot: 'createTime', width: 150 },
  { label: t('common.updateTime'), prop: 'scanTime', slot: 'updateTime', width: 150 },
  { label: t('common.operation'), slot: 'operation', width: 70, fixed: 'right', align: 'center' }
])

const dirSearchItems = computed(() => [
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
])

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

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('dirscan.confirmDelete'), t('common.tip'), { type: 'warning' })
    const res = await request.post('/dirscan/result/delete', { id: row.id })
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
    if (e !== 'cancel') {
      console.error('清空目录扫描失败:', e)
    }
  }
}

async function handleExport(command) {
  ElMessage.info(t('asset.gettingAllData'))
  let data = []
  try {
    const res = await request.post('/dirscan/result/list', {
      ...proTableRef.value?.searchForm, page: 1, pageSize: 10000
    })
    if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
  } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }

  if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

  if (command === 'csv') {
    const headers = ['URL', 'Path', 'StatusCode', 'ContentLength', 'ContentWords', 'ContentLines', 'Duration(ms)', 'ContentType', 'Title', 'RedirectUrl', 'Host', 'Port', 'Authority', 'CreateTime', 'UpdateTime']
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
        escapeCsvField(row.scanTime || '')
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

  // all-url: 导出去重URL
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
}
</style>
