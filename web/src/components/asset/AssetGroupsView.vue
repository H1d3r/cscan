<template>
  <div class="asset-groups-view">
    <!-- 搜索和过滤区域 -->
    <div class="search-section">
      <el-input
        v-model="searchQuery"
        :placeholder="$t('asset.searchAssetGroups')"
        clearable
        @input="handleSearch"
        class="search-input"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      
      <div class="filter-actions">
        <el-button @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
        </el-button>
      </div>
    </div>

    <!-- 资产分组表格 -->
    <el-table
      :data="filteredGroups"
      v-loading="loading"
      stripe
      class="groups-table"
    >
      <el-table-column type="selection" width="40" />
      
      <el-table-column :label="$t('asset.assetGroupName')" min-width="200">
        <template #default="{ row }">
          <div class="group-name">
            <el-icon class="group-icon"><FolderOpened /></el-icon>
            <span class="name-text">{{ row.domain }}</span>
            <el-tag size="small" type="info" class="count-badge">{{ row.totalServices }}</el-tag>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.source')" width="150">
        <template #default="{ row }">
          <el-tag size="small" type="success">
            <el-icon><Compass /></el-icon>
            {{ row.source }}
          </el-tag>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.totalServices')" width="120" align="center">
        <template #default="{ row }">
          <span class="service-count">{{ row.totalServices }} {{ $t('asset.services') }}</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.duration')" width="120">
        <template #default="{ row }">
          <span class="duration-text">{{ row.duration }}</span>
        </template>
      </el-table-column>

      <el-table-column :label="$t('asset.lastUpdated')" width="150" sortable>
        <template #default="{ row }">
          <span class="time-text">{{ row.lastUpdated }}</span>
        </template>
      </el-table-column>

      <el-table-column width="120" align="right">
        <template #default="{ row }">
          <el-button type="primary" size="small" @click="viewGroupDetails(row)">
            {{ $t('asset.scan') }}
          </el-button>
          <el-dropdown trigger="click" @command="handleCommand($event, row)">
            <el-button text>
              <el-icon><MoreFilled /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="view">{{ $t('asset.viewDetails') }}</el-dropdown-item>
                <el-dropdown-item command="export">{{ $t('asset.export') }}</el-dropdown-item>
                <el-dropdown-item command="delete" divided>{{ $t('asset.delete') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.pageSize"
      :total="pagination.total"
      :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      class="pagination"
      @size-change="loadData"
      @current-change="loadData"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Refresh, FolderOpened, Compass, MoreFilled } from '@element-plus/icons-vue'
import { getAssetGroups } from '@/api/asset'

const emit = defineEmits(['view-details'])

const loading = ref(false)
const searchQuery = ref('')
const groups = ref([])

const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// 过滤后的分组数据
const filteredGroups = computed(() => {
  if (!searchQuery.value) return groups.value
  const query = searchQuery.value.toLowerCase()
  return groups.value.filter(group => 
    group.domain.toLowerCase().includes(query)
  )
})

// 加载资产分组数据
async function loadData() {
  loading.value = true
  try {
    // 调用后端API获取按域名分组的资产统计
    const res = await getAssetGroups({
      page: pagination.page,
      pageSize: pagination.pageSize
    })
    
    if (res.code === 0) {
      groups.value = res.list || []
      pagination.total = res.total || 0
    }
  } catch (error) {
    console.error('加载资产分组失败:', error)
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  // 搜索逻辑已在computed中处理
}

function handleRefresh() {
  searchQuery.value = ''
  loadData()
}



function viewGroupDetails(row) {
  emit('view-details', row)
}

function handleCommand(command, row) {
  switch (command) {
    case 'view':
      viewGroupDetails(row)
      break
    case 'export':
      ElMessage.info('导出功能待实现')
      break
    case 'delete':
      handleDelete(row)
      break
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除分组 ${row.domain} 吗？`, '提示', {
      type: 'warning'
    })
    // 调用删除API
    ElMessage.success('删除成功')
    loadData()
  } catch (e) {
    // 用户取消
  }
}

onMounted(() => {
  loadData()
})

defineExpose({ refresh: loadData })
</script>

<style scoped>
.asset-groups-view {
  padding: 20px;
}

.search-section {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
}

.search-input {
  flex: 1;
  max-width: 400px;
}

.filter-actions {
  display: flex;
  gap: 8px;
}

.groups-table {
  margin-bottom: 20px;
}

.group-name {
  display: flex;
  align-items: center;
  gap: 8px;
}

.group-icon {
  color: var(--el-color-primary);
  font-size: 18px;
}

.name-text {
  font-weight: 500;
}

.count-badge {
  margin-left: auto;
}

.service-count {
  color: var(--el-text-color-regular);
  font-size: 13px;
}

.duration-text,
.time-text {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.pagination {
  display: flex;
  justify-content: flex-end;
}
</style>
