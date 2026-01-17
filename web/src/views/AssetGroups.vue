<template>
  <div class="asset-groups">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-content">
        <h1>资产分组</h1>
        <p class="description">
          创建和管理资产发现分组。通过添加域名或IP范围开始监控，升级以启用高级扫描功能
        </p>
      </div>
      <div class="header-actions">
        <el-button type="primary" @click="showCreateGroupDialog = true">
          <el-icon><Radar /></el-icon>
          开始发现
        </el-button>
      </div>
    </div>

    <!-- 搜索和过滤 -->
    <div class="search-filters">
      <el-input
        v-model="searchQuery"
        placeholder="搜索资产分组..."
        clearable
        class="search-input"
        @input="handleSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      <el-button @click="showFilterDialog = true">
        <el-icon><Filter /></el-icon>
        过滤
      </el-button>
      <el-button @click="refreshData">
        <el-icon><Refresh /></el-icon>
      </el-button>
    </div>

    <!-- 分组表格 -->
    <div class="groups-table">
      <el-table
        v-loading="loading"
        :data="filteredGroups"
        style="width: 100%"
      >
        <el-table-column type="selection" width="55" />
        
        <el-table-column label="资产分组名称" min-width="250" sortable>
          <template #default="{ row }">
            <div class="group-name-cell">
              <el-icon class="group-icon"><FolderOpened /></el-icon>
              <div>
                <div class="group-name">{{ row.domain }}</div>
                <div v-if="row.source" class="group-source">
                  <el-icon><Connection /></el-icon>
                  {{ row.source }}
                </div>
              </div>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column label="来源" width="150">
          <template #default="{ row }">
            <el-tag size="small" :type="getSourceType(row.source)">
              <el-icon><Search /></el-icon>
              {{ row.source }}
            </el-tag>
          </template>
        </el-table-column>
        
        <el-table-column label="总服务数" width="120" sortable>
          <template #default="{ row }">
            <div class="stat-cell">
              <span class="stat-number">{{ row.totalServices }}</span>
              <span class="stat-label">服务</span>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column label="持续时间" width="150">
          <template #default="{ row }">
            <span class="duration-text">{{ row.duration }}</span>
          </template>
        </el-table-column>
        
        <el-table-column label="最后更新" width="150" sortable>
          <template #default="{ row }">
            <span class="time-text">{{ formatTimeAgo(row.lastUpdated) }}</span>
          </template>
        </el-table-column>
        
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-dropdown @command="(cmd) => handleGroupAction(cmd, row)">
              <el-button text>
                <el-icon><MoreFilled /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="view">查看资产</el-dropdown-item>
                  <el-dropdown-item command="edit">编辑</el-dropdown-item>
                  <el-dropdown-item command="scan">重新扫描</el-dropdown-item>
                  <el-dropdown-item command="export">导出</el-dropdown-item>
                  <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>
      
      <!-- 分页 -->
      <div class="pagination-container">
        <div class="pagination-info">
          显示 1-{{ filteredGroups.length }} 条，共 {{ filteredGroups.length }} 条
        </div>
        <el-button text>上一页</el-button>
        <el-button text>下一页</el-button>
      </div>
    </div>

    <!-- 创建/编辑分组对话框 -->
    <el-dialog
      v-model="showCreateGroupDialog"
      :title="editingGroup ? '编辑资产分组' : '创建资产分组'"
      width="600px"
    >
      <el-form :model="groupForm" label-width="120px">
        <el-form-item label="分组名称" required>
          <el-input v-model="groupForm.name" placeholder="输入分组名称" />
        </el-form-item>
        
        <el-form-item label="发现类型" required>
          <el-select v-model="groupForm.discoveryType" placeholder="选择发现类型" style="width: 100%">
            <el-option label="自动发现" value="Auto Discovery" />
            <el-option label="手动添加" value="Manual" />
            <el-option label="API导入" value="API Import" />
          </el-select>
        </el-form-item>

        <el-form-item label="目标">
          <el-input
            v-model="groupForm.targets"
            type="textarea"
            :rows="4"
            placeholder="输入域名或IP范围，每行一个&#10;例如:&#10;example.com&#10;*.example.com&#10;192.168.1.0/24"
          />
        </el-form-item>

        <el-form-item label="描述">
          <el-input
            v-model="groupForm.description"
            type="textarea"
            :rows="2"
            placeholder="可选：描述此分组的用途"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="showCreateGroupDialog = false">取消</el-button>
        <el-button type="primary" @click="saveGroup">
          {{ editingGroup ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Search,
  Refresh,
  Radar,
  FolderOpened,
  MoreFilled,
  Filter,
  Connection
} from '@element-plus/icons-vue'
import { getAssetGroups, deleteAssetGroup } from '@/api/asset'

const router = useRouter()

// 响应式数据
const loading = ref(false)
const searchQuery = ref('')
const showCreateGroupDialog = ref(false)
const showFilterDialog = ref(false)
const editingGroup = ref(null)

// 分组表单
const groupForm = ref({
  name: '',
  description: '',
  discoveryType: 'Auto Discovery',
  targets: ''
})

// 资产分组数据
const assetGroups = ref([])

// 计算属性
const filteredGroups = computed(() => {
  if (!searchQuery.value) return assetGroups.value
  
  const query = searchQuery.value.toLowerCase()
  return assetGroups.value.filter(group =>
    group.domain.toLowerCase().includes(query) ||
    group.source?.toLowerCase().includes(query)
  )
})

// 方法
const handleSearch = () => {
  // 搜索逻辑
}

const loadAssetGroups = async () => {
  loading.value = true
  try {
    const response = await getAssetGroups({
      page: 1,
      pageSize: 100
    })
    if (response.code === 0) {
      assetGroups.value = response.list || []
    } else {
      ElMessage.error(response.msg || '加载资产分组失败')
    }
  } catch (error) {
    console.error('加载资产分组失败:', error)
    ElMessage.error('加载资产分组失败')
  } finally {
    loading.value = false
  }
}

const refreshData = async () => {
  await loadAssetGroups()
  ElMessage.success('刷新成功')
}

const viewGroupAssets = (group) => {
  // 跳转到资产清单页面并应用该分组的过滤器
  router.push({
    name: 'AssetInventory',
    query: { domain: group.domain }
  })
}

const getSourceType = (type) => {
  if (type === 'Auto Discovery') return 'success'
  if (type === 'Manual') return 'info'
  return 'warning'
}

const handleGroupAction = async (command, group) => {
  switch (command) {
    case 'view':
      viewGroupAssets(group)
      break
    case 'edit':
      editingGroup.value = group
      groupForm.value = {
        name: group.domain,
        description: group.description || '',
        discoveryType: group.source,
        targets: group.domain || ''
      }
      showCreateGroupDialog.value = true
      break
    case 'scan':
      ElMessage.success(`重新扫描分组: ${group.domain}`)
      break
    case 'export':
      ElMessage.success(`导出分组: ${group.domain}`)
      break
    case 'delete':
      try {
        await ElMessageBox.confirm(
          `确定删除分组 "${group.domain}" 吗？此操作将同时删除该分组下的所有资产清单信息，且无法恢复。`,
          '警告',
          {
            confirmButtonText: '确定',
            cancelButtonText: '取消',
            type: 'warning'
          }
        )
        
        loading.value = true
        const response = await deleteAssetGroup({ domain: group.domain })
        
        if (response.code === 0) {
          ElMessage.success(`删除成功，共删除 ${response.deletedCount} 个资产`)
          // 重新加载数据
          await loadAssetGroups()
        } else {
          ElMessage.error(response.msg || '删除失败')
        }
      } catch (error) {
        if (error !== 'cancel') {
          console.error('删除资产分组失败:', error)
          ElMessage.error('删除失败')
        }
      } finally {
        loading.value = false
      }
      break
  }
}

const saveGroup = () => {
  if (!groupForm.value.name) {
    ElMessage.warning('请输入分组名称')
    return
  }

  if (editingGroup.value) {
    // 更新现有分组
    const index = assetGroups.value.findIndex(g => g.domain === editingGroup.value.domain)
    if (index > -1) {
      assetGroups.value[index] = {
        ...assetGroups.value[index],
        domain: groupForm.value.name,
        description: groupForm.value.description,
        source: groupForm.value.discoveryType,
        lastUpdated: 'just now'
      }
    }
    ElMessage.success('分组已更新')
  } else {
    // 创建新分组
    const newGroup = {
      domain: groupForm.value.name,
      description: groupForm.value.description,
      source: groupForm.value.discoveryType,
      totalServices: 0,
      duration: '0s',
      lastUpdated: 'just now'
    }
    assetGroups.value.unshift(newGroup)
    ElMessage.success('分组已创建，开始发现资产...')
  }

  showCreateGroupDialog.value = false
  editingGroup.value = null
  groupForm.value = {
    name: '',
    description: '',
    discoveryType: 'Auto Discovery',
    targets: ''
  }
}

const formatTimeAgo = (timeStr) => {
  return timeStr
}

onMounted(() => {
  loadAssetGroups()
})
</script>

<style lang="scss" scoped>
.asset-groups {
  padding: 24px;
  background: hsl(var(--background));
  min-height: 100vh;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
  
  .header-content {
    h1 {
      font-size: 28px;
      font-weight: 600;
      color: hsl(var(--foreground));
      margin: 0 0 8px 0;
    }
    
    .description {
      color: hsl(var(--muted-foreground));
      font-size: 14px;
      margin: 0;
    }
  }
}

.search-filters {
  display: flex;
  gap: 12px;
  margin-bottom: 24px;
  
  .search-input {
    flex: 1;
    max-width: 400px;
  }
}

.groups-table {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
  padding: 16px;
  
  .group-name-cell {
    display: flex;
    align-items: center;
    gap: 12px;
    
    .group-icon {
      font-size: 20px;
      color: hsl(var(--primary));
    }
    
    .group-name {
      font-weight: 500;
      color: hsl(var(--foreground));
      font-size: 14px;
    }
    
    .group-source {
      display: flex;
      align-items: center;
      gap: 4px;
      font-size: 12px;
      color: hsl(var(--muted-foreground));
      
      .el-icon {
        font-size: 14px;
      }
    }
  }
  
  .stat-cell {
    display: flex;
    align-items: baseline;
    gap: 4px;
    
    .stat-number {
      font-weight: 600;
      color: hsl(var(--foreground));
      font-size: 16px;
    }
    
    .stat-label {
      font-size: 12px;
      color: hsl(var(--muted-foreground));
    }
  }
  
  .duration-text {
    font-size: 13px;
    color: hsl(var(--muted-foreground));
  }
  
  .time-text {
    font-size: 13px;
    color: hsl(var(--muted-foreground));
  }
}

.pagination-container {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid hsl(var(--border));
  
  .pagination-info {
    font-size: 14px;
    color: hsl(var(--muted-foreground));
  }
}
</style>
