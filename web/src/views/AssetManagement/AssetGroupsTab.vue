<template>
  <div class="asset-groups-tab">
    <!-- 搜索和操作栏 -->
    <div class="toolbar">
      <el-input
        v-model="searchQuery"
        :placeholder="t('asset.assetGroupsTab.searchPlaceholder')"
        clearable
        class="search-input"
        @input="handleSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      
      <!-- 过滤按钮 -->
      <el-dropdown @command="handleFilterCommand">
        <el-button>
          <el-icon><Filter /></el-icon>
          {{ t('asset.assetGroupsTab.filter') }}
          <el-icon class="el-icon--right"><ArrowDown /></el-icon>
        </el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="all">{{ t('asset.assetGroupsTab.allStatus') }}</el-dropdown-item>
            <el-dropdown-item command="starting" divided>{{ t('asset.assetGroupsTab.starting') }}</el-dropdown-item>
            <el-dropdown-item command="running">{{ t('asset.assetGroupsTab.running') }}</el-dropdown-item>
            <el-dropdown-item command="finished">{{ t('asset.assetGroupsTab.finished') }}</el-dropdown-item>
            <el-dropdown-item command="stopped">{{ t('asset.assetGroupsTab.stopped') }}</el-dropdown-item>
            <el-dropdown-item command="failed">{{ t('asset.assetGroupsTab.failed') }}</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      
      <!-- 删除按钮 -->
      <el-button @click="handleBatchDelete" :disabled="selectedRows.length === 0">
        <el-icon><Delete /></el-icon>
        {{ t('common.delete') }}
      </el-button>
      
      <!-- 刷新按钮 -->
      <el-button @click="refreshData">
        <el-icon><Refresh /></el-icon>
      </el-button>
    </div>

    <!-- 分组表格 -->
    <el-table
      v-loading="loading"
      :data="filteredGroups"
      style="width: 100%"
      class="groups-table"
      @selection-change="handleSelectionChange"
      @row-click="handleRowClick"
    >
      <el-table-column type="selection" width="55" />
      
      <el-table-column :label="t('asset.assetGroupsTab.groupName')" min-width="250">
        <template #default="{ row }">
          <div class="group-name-cell">
            <!-- 状态图标 -->
            <el-icon :class="['status-icon', `status-${row.status}`]">
              <component :is="getStatusIcon(row.status)" />
            </el-icon>
            <div class="group-info">
              <div class="group-name">{{ row.domain }}</div>
            </div>
          </div>
        </template>
      </el-table-column>
      
      <el-table-column :label="t('asset.assetGroupsTab.servicesCount')" width="150" align="center">
        <template #default="{ row }">
          <div class="services-cell">
            <span class="stat-number">{{ row.totalServices }}</span>
            <span class="stat-label">{{ t('asset.services') }}</span>
          </div>
        </template>
      </el-table-column>
      
      <el-table-column :label="t('asset.assetGroupsTab.duration')" width="150">
        <template #default="{ row }">
          <div class="duration-cell">
            <el-icon><Clock /></el-icon>
            <span>{{ row.duration }}</span>
          </div>
        </template>
      </el-table-column>
      
      <el-table-column :label="t('asset.assetGroupsTab.lastUpdated')" width="150" sortable>
        <template #default="{ row }">
          <div class="time-cell">
            {{ row.lastUpdated }}
          </div>
        </template>
      </el-table-column>
      
      <el-table-column :label="t('asset.assetGroupsTab.operations')" width="80" fixed="right" align="center">
        <template #default="{ row }">
          <div @click.stop>
            <el-dropdown @command="(cmd) => handleAction(cmd, row)">
              <el-button text>
                <el-icon><MoreFilled /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="view">
                    <el-icon><View /></el-icon>
                    {{ t('asset.assetGroupsTab.viewAssets') }}
                  </el-dropdown-item>
                  <el-dropdown-item command="delete" divided>
                    <el-icon><Delete /></el-icon>
                    {{ t('common.delete') }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <el-pagination
      v-model:current-page="currentPage"
      v-model:page-size="pageSize"
      :total="total"
      :page-sizes="[5, 10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      class="pagination"
      @size-change="loadData"
      @current-change="loadData"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Search,
  Filter,
  Refresh,
  Delete,
  ArrowDown,
  Clock,
  MoreFilled,
  View,
  CircleCheck,
  Loading,
  CircleClose,
  Warning
} from '@element-plus/icons-vue'
import { getAssetGroups, deleteAssetGroup } from '@/api/asset'

const { t } = useI18n()

const router = useRouter()
const loading = ref(false)
const searchQuery = ref('')
const statusFilter = ref('all')
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const groups = ref([])
const selectedRows = ref([])

// 模拟数据（用于开发测试）
const useMockData = false

const mockGroups = [
  {
    id: '1',
    domain: 'txf7.cn',
    source: 'Auto Discovery',
    status: 'finished',
    totalServices: 4,
    duration: '4m26s',
    lastUpdated: '23h ago'
  },
  {
    id: '2',
    domain: 'leapmotor.com',
    source: 'Auto Discovery',
    status: 'running',
    totalServices: 103,
    duration: '7h21s',
    lastUpdated: '8mo ago'
  }
]

const filteredGroups = computed(() => {
  let result = groups.value
  
  // 搜索过滤
  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    result = result.filter(g =>
      g.domain?.toLowerCase().includes(query) ||
      g.source?.toLowerCase().includes(query)
    )
  }
  
  // 状态过滤
  if (statusFilter.value !== 'all') {
    result = result.filter(g => g.status === statusFilter.value)
  }
  
  return result
})

// 获取状态图标
const getStatusIcon = (status) => {
  const iconMap = {
    finished: CircleCheck,
    running: Loading,
    stopped: CircleClose,
    failed: Warning,
    starting: Loading
  }
  return iconMap[status] || CircleCheck
}

const handleFilterCommand = (command) => {
  statusFilter.value = command
  currentPage.value = 1
  loadData()
}

const handleSelectionChange = (selection) => {
  selectedRows.value = selection
}

const handleRowClick = (row) => {
  // 点击行跳转到资产清单
  viewAssets(row)
}

const handleAction = (command, row) => {
  if (command === 'view') {
    viewAssets(row)
  } else if (command === 'delete') {
    deleteGroup(row)
  }
}

const handleBatchDelete = async () => {
  if (selectedRows.value.length === 0) return
  
  try {
    await ElMessageBox.confirm(
      t('asset.assetGroupsTab.confirmBatchDelete', { count: selectedRows.value.length }),
      t('common.batchDelete'),
      {
        confirmButtonText: t('common.confirm'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )
    
    // 批量调用删除API
    loading.value = true
    try {
      let totalDeleted = 0
      const promises = selectedRows.value.map(row => 
        deleteAssetGroup({ domain: row.domain })
      )
      
      const results = await Promise.all(promises)
      
      results.forEach(res => {
        if (res.code === 0) {
          totalDeleted += res.deletedCount || 0
        }
      })
      
      ElMessage.success(t('asset.assetGroupsTab.deleteSuccess') + ` (${t('asset.assetGroupsTab.deletedCount')}: ${totalDeleted})`)
      // 重新加载数据
      await loadData()
    } catch (error) {
      console.error('批量删除失败:', error)
      ElMessage.error(t('asset.assetGroupsTab.deleteFailed'))
    } finally {
      loading.value = false
    }
  } catch {
    // 用户取消
  }
}

const loadData = async () => {
  loading.value = true
  try {
    if (useMockData) {
      // 使用模拟数据
      groups.value = mockGroups
      total.value = mockGroups.length
    } else {
      // 调用真实 API
      const res = await getAssetGroups({
        page: currentPage.value,
        pageSize: pageSize.value,
        query: searchQuery.value,
        status: statusFilter.value !== 'all' ? statusFilter.value : undefined
      })
      
      if (res.code === 0) {
        groups.value = res.list || []
        total.value = res.total || 0
        
        // 调试：打印接收到的数据
        console.log('资产分组数据:', groups.value)
        groups.value.forEach(group => {
          console.log(`分组: ${group.domain}, 状态: ${group.status}, 服务数: ${group.totalServices}`)
        })
      } else {
        ElMessage.error(res.msg || '加载失败')
      }
    }
  } catch (error) {
    console.error('加载失败:', error)
    ElMessage.error('加载失败，请稍后重试')
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  currentPage.value = 1
  loadData()
}

const refreshData = () => {
  loadData()
  ElMessage.success(t('asset.assetGroupsTab.refreshSuccess'))
}

const viewAssets = (row) => {
  // 跳转到资产清单标签页并传递域名过滤
  router.push({
    path: '/asset-management',
    query: { tab: 'inventory', domain: row.domain }
  })
}

const deleteGroup = async (row) => {
  try {
    await ElMessageBox.confirm(
      t('asset.assetGroupsTab.confirmDelete', { name: row.domain }), 
      t('common.warning'), 
      {
        confirmButtonText: t('common.confirm'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )
    
    // 调用删除API
    loading.value = true
    try {
      const res = await deleteAssetGroup({ domain: row.domain })
      
      if (res.code === 0) {
        ElMessage.success(t('asset.assetGroupsTab.deleteSuccess') + ` (${t('asset.assetGroupsTab.deletedCount')}: ${res.deletedCount})`)
        // 重新加载数据
        await loadData()
      } else {
        ElMessage.error(res.msg || t('asset.assetGroupsTab.deleteFailed'))
      }
    } catch (error) {
      console.error('删除资产分组失败:', error)
      ElMessage.error(t('asset.assetGroupsTab.deleteFailed'))
    } finally {
      loading.value = false
    }
  } catch {
    // 用户取消
  }
}

onMounted(() => {
  loadData()
})
</script>

<style lang="scss" scoped>
.asset-groups-tab {
  .toolbar {
    display: flex;
    gap: 12px;
    margin-bottom: 16px;
    
    .search-input {
      flex: 1;
      max-width: 400px;
    }
  }
  
  .groups-table {
    margin-bottom: 16px;
    
    :deep(.el-table__row) {
      cursor: pointer;
      
      &:hover {
        background-color: hsl(var(--muted) / 0.5);
      }
    }
    
    // 操作列样式 - 移除白框
    :deep(.el-table__cell) {
      .cell {
        > div {
          background: transparent !important;
        }
      }
    }
    
    .group-name-cell {
      display: flex;
      align-items: center;
      gap: 12px;
      
      .status-icon {
        font-size: 20px;
        flex-shrink: 0;
        
        &.status-finished {
          color: #52c41a;
        }
        
        &.status-running {
          color: #1890ff;
          animation: rotate 1s linear infinite;
        }
        
        &.status-stopped {
          color: #ff4d4f;
        }
        
        &.status-failed {
          color: #faad14;
        }
        
        &.status-starting {
          color: #1890ff;
          animation: rotate 1s linear infinite;
        }
      }
      
      .group-info {
        flex: 1;
        min-width: 0;
      }
      
      .group-name {
        font-weight: 500;
        color: hsl(var(--foreground));
        font-size: 14px;
      }
    }
    
    .services-cell {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 2px;
      
      .stat-number {
        font-weight: 600;
        font-size: 18px;
        color: hsl(var(--foreground));
      }
      
      .stat-label {
        font-size: 12px;
        color: hsl(var(--muted-foreground));
      }
    }
    
    .duration-cell {
      display: flex;
      align-items: center;
      gap: 6px;
      color: hsl(var(--muted-foreground));
      
      .el-icon {
        font-size: 14px;
      }
    }
    
    .time-cell {
      color: hsl(var(--muted-foreground));
      font-size: 13px;
    }
  }
  
  .pagination {
    margin-top: 16px;
  }
}

@keyframes rotate {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
</style>
