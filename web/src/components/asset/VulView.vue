<template>
  <div class="vul-view">
    <ProTable
      ref="proTableRef"
      api="/vul/list"
      statApi="/vul/stat"
      batchDeleteApi="/vul/batchDelete"
      rowKey="id"
      :columns="vulColumns"
      :searchItems="vulSearchItems"
      :statLabels="statLabels"
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
        <el-button type="danger" plain @click="handleClear">{{ $t('vul.clearData') }}</el-button>
      </template>

      <!-- 严重程度 -->
      <template #severity="{ row }">
        <el-tag :type="getSeverityType(row.severity)" size="small">{{ getSeverityLabel(row.severity) }}</el-tag>
      </template>

      <!-- POC标签 -->
      <template #tags="{ row }">
        <template v-if="row.tags && row.tags.length">
          <el-tag v-for="tag in row.tags.slice(0, 3)" :key="tag" size="small" class="tag-item">{{ tag }}</el-tag>
          <el-tag v-if="row.tags.length > 3" size="small" type="info">+{{ row.tags.length - 3 }}</el-tag>
        </template>
      </template>

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
        <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
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
        <el-descriptions-item :label="$t('vul.source')">{{ currentVul.source }}</el-descriptions-item>
        <el-descriptions-item :label="$t('vul.discoveryTime')">{{ currentVul.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.updateTime')">{{ currentVul.updateTime }}</el-descriptions-item>
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
    </el-drawer>
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
const detailVisible = ref(false)
const currentVul = ref({})

const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

const statLabels = computed(() => ({
  total: t('vul.totalVuls'),
  critical: t('vul.critical'),
  high: t('vul.high'),
  medium: t('vul.medium'),
  low: t('vul.low'),
  info: t('vul.info')
}))

const vulColumns = computed(() => [
  { label: t('vul.vulName'), prop: 'vulName', minWidth: 200, showOverflowTooltip: true },
  { label: t('vul.severity'), prop: 'severity', slot: 'severity', width: 100 },
  { label: t('vul.target'), prop: 'authority', minWidth: 150 },
  { label: 'URL', prop: 'url', minWidth: 250, showOverflowTooltip: true },
  { label: 'POC', prop: 'pocFile', minWidth: 200, showOverflowTooltip: true },
  { label: t('vul.tags'), prop: 'tags', slot: 'tags', minWidth: 150 },
  { label: t('vul.source'), prop: 'source', width: 100 },
  { label: t('vul.discoveryTime'), prop: 'createTime', width: 160 },
  { label: t('common.updateTime'), prop: 'updateTime', width: 160 },
  { label: t('common.operation'), slot: 'operation', width: 120, fixed: 'right' }
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
    label: t('vul.source'),
    prop: 'source',
    type: 'select',
    options: [
      { label: 'Nuclei', value: 'nuclei' }
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

    const headers = ['VulName', 'Severity', 'Target', 'URL', 'POC', 'Tags', 'Source', 'Result', 'CreateTime', 'UpdateTime']
    const csvRows = [headers.join(',')]
    for (const row of data) {
      csvRows.push([
        escapeCsvField(row.vulName || ''),
        escapeCsvField(row.severity || ''),
        escapeCsvField(row.authority || ''),
        escapeCsvField(row.url || ''),
        escapeCsvField(row.pocFile || ''),
        escapeCsvField((row.tags || []).join(';')),
        escapeCsvField(row.source || ''),
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

defineExpose({ refresh })
</script>

<style scoped lang="scss">
.vul-view {
  height: 100%;

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
}
</style>
