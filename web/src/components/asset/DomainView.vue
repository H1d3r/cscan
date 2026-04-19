<template>
  <div class="domain-view">
    <ProTable
      ref="proTableRef"
      api="/asset/domain/list"
      statApi="/asset/domain/stat"
      batchDeleteApi="/asset/domain/batchDelete"
      rowKey="id"
      :columns="domainColumns"
      :searchItems="domainSearchItems"
      :statLabels="statLabels"
      selection
      :searchPlaceholder="$t('domain.searchDomains')"
      @data-changed="$emit('data-changed')"
      :searchKeys="['domain']"
    >
      <template #toolbar-left>
        <el-dropdown @command="handleExport">
          <el-button type="success" size="default">
            {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="selected" :disabled="selectedRows.length === 0">{{ $t('common.exportSelected') || '导出选中项' }} ({{ selectedRows.length }})</el-dropdown-item>
              <el-dropdown-item divided command="all">{{ $t('common.exportAll') || '导出所有' }}</el-dropdown-item>
              <el-dropdown-item command="csv">{{ $t('common.exportCsv') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>

      <template #toolbar-right>
        <el-button type="danger" plain @click="handleClear">{{ $t('asset.clearData') }}</el-button>
      </template>

      <!-- 域名 -->
      <template #domain="{ row }">
        <div class="domain-cell">
          <span class="domain-name">{{ row.domain }}</span>
          <el-tag v-if="row.isNew" type="success" size="small" effect="dark" class="new-tag">{{ $t('common.new') }}</el-tag>
        </div>
      </template>

      <!-- 根域名 -->
      <template #rootDomain="{ row }">
        {{ row.rootDomain || '-' }}
      </template>

      <!-- 解析 IP -->
      <template #ips="{ row }">
        <div v-if="row.ips && row.ips.length > 0">
          <el-tag v-for="ip in row.ips.slice(0, 3)" :key="ip" size="small" type="info" style="margin-right: 4px">{{ ip }}</el-tag>
          <span v-if="row.ips.length > 3" class="more-ips">+{{ row.ips.length - 3 }}</span>
        </div>
        <span v-else class="no-resolve">{{ $t('domain.notResolved') }}</span>
      </template>

      <!-- CNAME -->
      <template #cname="{ row }">
        {{ row.cname || '-' }}
      </template>

      <!-- 来源 -->
      <template #source="{ row }">
        <el-tag size="small">{{ row.source || 'subfinder' }}</el-tag>
      </template>

      <!-- 所属组织 -->
      <template #org="{ row }">
        {{ row.orgName || $t('common.defaultOrganization') }}
      </template>

      <!-- 操作 -->
      <template #operation="{ row }">
        <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
      </template>
    </ProTable>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import request from '@/api/request'
import { clearAssets, getAssetFilterOptions } from '@/api/asset'
import ProTable from '@/components/common/ProTable.vue'

const { t } = useI18n()
const emit = defineEmits(['data-changed'])

const proTableRef = ref(null)
const organizations = ref([])
const filterOptions = ref({ ports: [], technologies: [], labels: [], statusCodes: [] })

const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

// 统计标签映射
const statLabels = computed(() => ({
  total: t('domain.totalDomains'),
  rootDomainCount: t('domain.rootDomainCount'),
  resolvedCount: t('domain.resolvedCount'),
  newCount: t('domain.newDomains')
}))

// 表格列配置
const domainColumns = computed(() => [
  { label: t('domain.domain'), prop: 'domain', slot: 'domain', minWidth: 250 },
  { label: t('domain.rootDomain'), prop: 'rootDomain', slot: 'rootDomain', width: 160 },
  { label: t('domain.resolveIP'), prop: 'ips', slot: 'ips', minWidth: 200 },
  { label: t('domain.cname'), prop: 'cname', slot: 'cname', width: 180 },
  { label: t('domain.source'), prop: 'source', slot: 'source', width: 100 },
  { label: t('domain.organization'), prop: 'orgName', slot: 'org', width: 120 },
  { label: t('domain.discoveryTime'), prop: 'createTime', width: 160 },
  { label: t('common.updateTime'), prop: 'updateTime', width: 160 },
  { label: t('common.operation'), slot: 'operation', width: 80, fixed: 'right' }
])

// 高级过滤器配置
const domainSearchItems = computed(() => [
  { label: t('domain.domain'), prop: 'domain', type: 'input' },
  { label: t('domain.rootDomain'), prop: 'rootDomain', type: 'input' },
  { 
    label: t('domain.resolveIP'), 
    prop: 'ip', 
    type: 'input',
    placeholder: t('domain.searchSingleIP')
  },
  {
    label: t('domain.labels'),
    prop: 'labels', 
    type: 'select',
    options: filterOptions.value.labels,
    multiple: true,
    allowCreate: true
  },
  {
    label: t('domain.organization'),
    prop: 'orgId',
    type: 'select',
    options: [
      { label: t('common.allOrganizations'), value: '' },
      ...organizations.value.map(org => ({ label: org.name, value: org.id }))
    ]
  }
])


async function loadFilterOptions() {
  try {
    const res = await getAssetFilterOptions({})
    if (res.code === 0) {
      filterOptions.value = {
        technologies: res.technologies || [],
        ports: res.ports || [],
        statusCodes: res.statusCodes || [],
        labels: res.labels || []
      }
    }
  } catch (e) {
    console.error(e)
  }
}

async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    if (res.code === 0) organizations.value = res.list || []
  } catch (e) {
    console.error(e)
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('domain.confirmDeleteDomain'), t('common.tip'), { type: 'warning' })
    const res = await request.post('/asset/domain/delete', { id: row.id })
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

  if (command === 'selected') {
    if (selectedRows.value.length === 0) { ElMessage.warning(t('common.pleaseSelect') || '请先选择要导出的项'); return }
    data = selectedRows.value
  } else {
    ElMessage.info(t('asset.gettingAllData') || '正在获取全部数据...')
    try {
      const res = await request.post('/asset/domain/list', { ...proTableRef.value?.searchForm, page: 1, pageSize: 10000 })
      if (res.code === 0) { data = res.list || [] } else { ElMessage.error(t('asset.getDataFailed')); return }
    } catch (e) { ElMessage.error(t('asset.getDataFailed')); return }
  }

  if (data.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }

  if (command === 'csv') {
    const headers = ['Domain', 'RootDomain', 'IPs', 'CNAME', 'Source', 'Organization', 'CreateTime', 'UpdateTime']
    const csvRows = [headers.join(',')]
    for (const row of data) {
      csvRows.push([
        escapeCsvField(row.domain || ''),
        escapeCsvField(row.rootDomain || ''),
        escapeCsvField((row.ips || []).join(';')),
        escapeCsvField(row.cname || ''),
        escapeCsvField(row.source || ''),
        escapeCsvField(row.orgName || ''),
        escapeCsvField(row.createTime || ''),
        escapeCsvField(row.updateTime || '')
      ].join(','))
    }
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `domains_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link); link.click(); document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(t('asset.exportSuccess', { count: data.length }) || `成功导出 ${data.length} 条数据`)
    return
  }

  // selected / all — export domain list as txt
  const seen = new Set()
  const exportData = []
  for (const row of data) {
    if (row.domain && !seen.has(row.domain)) { seen.add(row.domain); exportData.push(row.domain) }
  }
  if (exportData.length === 0) { ElMessage.warning(t('asset.noDataToExport')); return }
  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url; link.download = command === 'selected' ? 'domains_selected.txt' : 'domains_all.txt'
  document.body.appendChild(link); link.click(); document.body.removeChild(link)
  URL.revokeObjectURL(url)
  ElMessage.success(t('asset.exportSuccess', { count: exportData.length }) || `成功导出 ${exportData.length} 条数据`)
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

function handleWorkspaceChanged() {
  loadOrganizations()
}

onMounted(() => {
  loadOrganizations()
  loadFilterOptions()
  window.addEventListener('workspace-changed', handleWorkspaceChanged)
})

onUnmounted(() => {
  window.removeEventListener('workspace-changed', handleWorkspaceChanged)
})

defineExpose({ refresh })
</script>

<style scoped lang="scss">
.domain-view {
  height: 100%;

  .domain-cell {
    display: flex;
    align-items: center;

    .domain-name {
      font-family: monospace;
    }
    .new-tag {
      margin-left: 8px;
    }
  }

  .more-ips {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .no-resolve {
    color: var(--el-text-color-placeholder);
  }
}
</style>
