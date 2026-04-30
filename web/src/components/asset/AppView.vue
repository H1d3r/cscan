<template>
  <div class="app-view">
    <ProTable
      ref="proTableRef"
      api="/asset/app/list"
      statApi="/asset/app/stat"
      batchDeleteApi="/asset/app/batchDelete"
      rowKey="id"
      :columns="appColumns"
      :searchItems="searchItems"
      :statLabels="statLabels"
      selection
      :searchPlaceholder="t('asset.appView.searchPlaceholder')"
      @data-changed="$emit('data-changed')"
      :searchKeys="['app']"
    >
      <template #toolbar-left>
        <el-dropdown @command="handleExport">
          <el-button type="success" size="default">
            {{ $t('common.export') || '导出' }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="selected" :disabled="selectedRows.length === 0">
                {{ t('asset.appView.exportSelected', { count: selectedRows.length }) }}
              </el-dropdown-item>
              <el-dropdown-item divided command="all">{{ t('asset.appView.exportAll') }}</el-dropdown-item>
              <el-dropdown-item command="csv">{{ t('asset.appView.exportCsv') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>

      <template #toolbar-right>
        <el-button type="danger" plain @click="handleClear($emit)">{{ $t('asset.clearData') || '清空数据' }}</el-button>
      </template>

      <template #app="{ row }">
        <div class="app-cell font-bold">
          <span>{{ row.app }}</span>
        </div>
      </template>

      <template #assets="{ row }">
        <div v-if="row.assets && row.assets.length > 0">
          <el-tag v-for="ast in row.assets.slice(0, 3)" :key="ast" size="small" type="info" class="mr-1">{{ ast }}</el-tag>
          <span v-if="row.assets.length > 3" class="text-xs text-gray-500">+{{ row.assets.length - 3 }}</span>
        </div>
        <span v-else class="text-gray-400">-</span>
      </template>

      <template #org="{ row }">
        {{ row.orgName || $t('common.defaultOrganization') || '默认组织' }}
      </template>

      <template #operation="{ row }">
        <el-button type="primary" link size="small" @click="viewAssets(row)">{{ t('asset.appView.viewAssets') }}</el-button>
        <el-button type="danger" link size="small" @click="handleDelete(row, $emit)">{{ $t('common.delete') || '删除' }}</el-button>
      </template>
    </ProTable>
  </div>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import { ArrowDown } from '@element-plus/icons-vue'
import ProTable from '@/components/common/ProTable.vue'
import { useAssetView } from '@/composables/useAssetView'

const emit = defineEmits(['data-changed'])

const {
  t, proTableRef, organizations, selectedRows, statLabels,
  loadOrganizations, handleDelete, handleClear, handleExport
} = useAssetView({
  apiPrefix: '/asset/app',
  viewType: 'app',
  exportHeaders: ['App', 'Category', 'Assets', 'Organization', 'CreateTime', 'UpdateTime'],
  exportRowFormatter: row => [
    row.app || '',
    row.category || '',
    (row.assets || []).join(';'),
    row.orgName || '',
    row.createTime || '',
    row.updateTime || ''
  ]
})

const appColumns = computed(() => [
  { label: t('asset.appView.columns.app'), prop: 'app', slot: 'app', minWidth: 300 },
  { label: t('asset.appView.columns.assets'), prop: 'assets', slot: 'assets', minWidth: 250 },
  { label: t('asset.appView.columns.organization'), prop: 'orgName', slot: 'org', width: 120 },
  { label: t('asset.appView.columns.createTime'), prop: 'createTime', width: 160 },
  { label: t('common.updateTime'), prop: 'updateTime', width: 160 },
  { label: t('asset.appView.columns.operation'), slot: 'operation', width: 140, fixed: 'right' }
])

const searchItems = computed(() => [
  { label: '关联资产', prop: 'assets', type: 'input' },
  {
    label: t('asset.appView.filters.organization'), prop: 'orgId', type: 'select',
    options: [{ label: t('asset.appView.filters.allOrganizations'), value: '' }, ...organizations.value.map(org => ({ label: org.name, value: org.id }))]
  }
])

function viewAssets(row) {
  window.location.href = `/asset-management?tab=inventory&app=${encodeURIComponent(row.app)}`
}

onMounted(() => {
  loadOrganizations()
})

defineExpose({
  refresh: () => proTableRef.value?.loadData()
})
</script>

<style scoped>
.app-view {
  height: 100%;
}
.app-cell {
  display: flex;
  align-items: center;
}
.mr-1 {
  margin-right: 4px;
}
</style>