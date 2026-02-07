<template>
  <div class="asset-inventory">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-content">
        <h1>资产清单</h1>
        <p class="description">
          查看所有已发现的资产，支持按服务、技术栈、端口、标签等多维度筛选
        </p>
      </div>
      <div class="header-actions">
        <el-button @click="startVulnerabilityScan">
          <el-icon><Search /></el-icon>
          启动漏洞扫描
        </el-button>
        <el-button @click="exportAssets">
          <el-icon><Download /></el-icon>
          导出
        </el-button>
      </div>
    </div>

    <!-- 搜索和过滤 -->
    <div class="search-filters">
      <div class="search-section">
        <el-button @click="showFilters = !showFilters" :type="showFilters ? 'primary' : 'default'">
          <el-icon><Filter /></el-icon>
          添加过滤器
        </el-button>
        <el-input
          v-model="searchQuery"
          placeholder="搜索主机名..."
          clearable
          class="search-input"
          @input="handleSearch"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>
    </div>

    <!-- 过滤器标签栏 -->
    <div class="filter-tabs">
      <el-button
        :type="activeTab === 'all' ? 'primary' : 'default'"
        @click="activeTab = 'all'"
      >
        所有服务
      </el-button>
      <el-button
        :type="activeTab === 'technologies' ? 'primary' : 'default'"
        @click="activeTab = 'technologies'"
      >
        技术栈
      </el-button>
      <el-button
        :type="activeTab === 'ports' ? 'primary' : 'default'"
        @click="activeTab = 'ports'"
      >
        端口
      </el-button>
      <el-button
        :type="activeTab === 'labels' ? 'primary' : 'default'"
        @click="activeTab = 'labels'"
      >
        标签
      </el-button>
      <el-button
        :type="activeTab === 'domains' ? 'primary' : 'default'"
        @click="activeTab = 'domains'"
      >
        域名
      </el-button>
      <el-button @click="showMoreFilters = true">
        更多
        <el-icon><ArrowDown /></el-icon>
      </el-button>
      
      <div class="filter-actions">
        <el-dropdown @command="handleSort">
          <el-button>
            <el-icon><Sort /></el-icon>
            排序
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="time">最近更新</el-dropdown-item>
              <el-dropdown-item command="name">名称</el-dropdown-item>
              <el-dropdown-item command="port">端口</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        
        <el-dropdown @command="handleTimeFilter">
          <el-button>
            <el-icon><Clock /></el-icon>
            全部时间
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="all">全部时间</el-dropdown-item>
              <el-dropdown-item command="24h">最近24小时</el-dropdown-item>
              <el-dropdown-item command="7d">最近7天</el-dropdown-item>
              <el-dropdown-item command="30d">最近30天</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        
        <el-button @click="refreshData">
          <el-icon><Refresh /></el-icon>
        </el-button>
      </div>
    </div>

    <!-- 高级过滤器面板 -->
    <div v-if="showFilters" class="advanced-filters">
      <div class="filter-group">
        <label>技术栈</label>
        <el-select v-model="filters.technologies" multiple placeholder="选择技术" clearable>
          <el-option label="Nginx" value="nginx" />
          <el-option label="Apache" value="apache" />
          <el-option label="PHP" value="php" />
          <el-option label="Java" value="java" />
          <el-option label="Node.js" value="nodejs" />
        </el-select>
      </div>
      
      <div class="filter-group">
        <label>端口</label>
        <el-select v-model="filters.ports" multiple placeholder="选择端口" clearable>
          <el-option label="80" value="80" />
          <el-option label="443" value="443" />
          <el-option label="22" value="22" />
          <el-option label="3306" value="3306" />
          <el-option label="8080" value="8080" />
        </el-select>
      </div>
      
      <div class="filter-group">
        <label>状态码</label>
        <el-select v-model="filters.statusCodes" multiple placeholder="选择状态码" clearable>
          <el-option label="200" value="200" />
          <el-option label="301" value="301" />
          <el-option label="403" value="403" />
          <el-option label="404" value="404" />
          <el-option label="500" value="500" />
        </el-select>
      </div>
      
      <div class="filter-actions-bottom">
        <el-button @click="applyFilters" type="primary">应用过滤器</el-button>
        <el-button @click="resetFilters">重置</el-button>
      </div>
    </div>

    <!-- 资产表格 -->
    <div class="assets-table">
      <el-table
        v-loading="loading"
        :data="paginatedAssets"
        style="width: 100%"
        @selection-change="handleSelectionChange"
      >
        <el-table-column type="selection" width="55" />
        
        <el-table-column label="主机" min-width="200">
          <template #default="{ row }">
            <div class="host-cell">
              <div class="host-info">
                <el-icon class="host-icon"><Monitor /></el-icon>
                <div>
                  <div class="host-name">{{ row.host }}</div>
                  <div class="host-ip">{{ row.ip }}</div>
                </div>
              </div>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column label="端口" width="100">
          <template #default="{ row }">
            <el-tag size="small">{{ row.port }}</el-tag>
          </template>
        </el-table-column>
        
        <el-table-column label="服务" width="120">
          <template #default="{ row }">
            <span class="service-name">{{ row.service || '-' }}</span>
          </template>
        </el-table-column>
        
        <el-table-column label="标题" min-width="200">
          <template #default="{ row }">
            <span class="title-text">{{ row.title || '-' }}</span>
          </template>
        </el-table-column>
        
        <el-table-column label="技术栈" min-width="200">
          <template #default="{ row }">
            <div class="tech-tags">
              <el-tag
                v-for="tech in (row.technologies || []).slice(0, 3)"
                :key="tech"
                size="small"
                class="tech-tag"
              >
                {{ tech }}
              </el-tag>
              <el-tag v-if="row.technologies && row.technologies.length > 3" size="small" type="info">
                +{{ row.technologies.length - 3 }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)" size="small">
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        
        <el-table-column label="最后更新" width="150">
          <template #default="{ row }">
            <span class="time-text">{{ formatTimeAgo(row.lastUpdated) }}</span>
          </template>
        </el-table-column>
        
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-dropdown @command="(cmd) => handleAction(cmd, row)">
              <el-button text>
                <el-icon><MoreFilled /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="view">查看详情</el-dropdown-item>
                  <el-dropdown-item command="scan">扫描漏洞</el-dropdown-item>
                  <el-dropdown-item command="screenshot">查看截图</el-dropdown-item>
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
          显示 {{ (currentPage - 1) * pageSize + 1 }}-{{ Math.min(currentPage * pageSize, totalAssets) }} 条，共 {{ totalAssets }} 条
        </div>
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="totalAssets"
          layout="sizes, prev, pager, next"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Search,
  Filter,
  Download,
  Monitor,
  Sort,
  Clock,
  Refresh,
  MoreFilled,
  ArrowDown
} from '@element-plus/icons-vue'

// 响应式数据
const loading = ref(false)
const searchQuery = ref('')
const showFilters = ref(false)
const showMoreFilters = ref(false)
const activeTab = ref('all')
const currentPage = ref(1)
const pageSize = ref(20)
const selectedAssets = ref([])

// 过滤器
const filters = ref({
  technologies: [],
  ports: [],
  statusCodes: [],
  timeRange: 'all',
  sortBy: 'time'
})

// 模拟资产数据
const assets = ref([
  {
    id: 1,
    host: 'cscan.txt7.cn',
    ip: '124.221.31.220',
    port: 443,
    service: 'https',
    title: 'CSCAN - 网络安全扫描平台',
    technologies: ['Nginx:1.14', 'Vue.js', 'Element Plus'],
    status: '200',
    lastUpdated: new Date(Date.now() - 2 * 60 * 60 * 1000)
  },
  {
    id: 2,
    host: 'admin.example.com',
    ip: '192.168.1.100',
    port: 443,
    service: 'https',
    title: '管理后台登录',
    technologies: ['Apache', 'PHP', 'MySQL'],
    status: '200',
    lastUpdated: new Date(Date.now() - 5 * 60 * 60 * 1000)
  },
  {
    id: 3,
    host: 'api.example.com',
    ip: '192.168.1.101',
    port: 8080,
    service: 'http',
    title: 'API Documentation',
    technologies: ['Node.js', 'Express'],
    status: '200',
    lastUpdated: new Date(Date.now() - 1 * 24 * 60 * 60 * 1000)
  }
])

// 计算属性
const filteredAssets = computed(() => {
  let filtered = assets.value
  
  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    filtered = filtered.filter(asset =>
      asset.host.toLowerCase().includes(query) ||
      asset.ip.includes(query) ||
      asset.title?.toLowerCase().includes(query)
    )
  }
  
  if (filters.value.technologies.length > 0) {
    filtered = filtered.filter(asset =>
      asset.technologies.some(tech =>
        filters.value.technologies.some(filter =>
          tech.toLowerCase().includes(filter.toLowerCase())
        )
      )
    )
  }
  
  if (filters.value.ports.length > 0) {
    filtered = filtered.filter(asset =>
      filters.value.ports.includes(String(asset.port))
    )
  }
  
  if (filters.value.statusCodes.length > 0) {
    filtered = filtered.filter(asset =>
      filters.value.statusCodes.includes(asset.status)
    )
  }
  
  return filtered
})

const totalAssets = computed(() => filteredAssets.value.length)

const paginatedAssets = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  const end = start + pageSize.value
  return filteredAssets.value.slice(start, end)
})

// 方法
const handleSearch = () => {
  currentPage.value = 1
}

const handleSort = (command) => {
  filters.value.sortBy = command
  ElMessage.success(`按${command}排序`)
}

const handleTimeFilter = (command) => {
  filters.value.timeRange = command
  ElMessage.success(`时间范围: ${command}`)
}

const applyFilters = () => {
  currentPage.value = 1
  showFilters.value = false
  ElMessage.success('过滤器已应用')
}

const resetFilters = () => {
  filters.value = {
    technologies: [],
    ports: [],
    statusCodes: [],
    timeRange: 'all',
    sortBy: 'time'
  }
  currentPage.value = 1
  ElMessage.success('过滤器已重置')
}

const refreshData = async () => {
  loading.value = true
  try {
    await new Promise(resolve => setTimeout(resolve, 1000))
    ElMessage.success('刷新成功')
  } finally {
    loading.value = false
  }
}

const startVulnerabilityScan = () => {
  if (selectedAssets.value.length === 0) {
    ElMessage.warning('请先选择要扫描的资产')
    return
  }
  // 跳转到新建任务页面，并传递选中的资产作为扫描目标
  const targets = selectedAssets.value.map(a => `${a.host}:${a.port}`).join('\n')
  // 使用 sessionStorage 临时存储目标
  sessionStorage.setItem('scanTargets', targets)
  ElMessage.success(`已选择 ${selectedAssets.value.length} 个资产，正在跳转到扫描任务创建页面...`)
  setTimeout(() => {
    window.location.href = '/task/create'
  }, 500)
}

const exportAssets = async () => {
  if (filteredAssets.value.length === 0) {
    ElMessage.warning('没有可导出的数据')
    return
  }
  
  try {
    ElMessage.info('正在准备导出数据...')
    
    // 准备导出数据
    const exportList = filteredAssets.value.map(item => ({
      host: item.host,
      port: item.port,
      ip: item.ip,
      service: item.service || '',
      title: item.title || '',
      status: item.status,
      technologies: (item.technologies || []).join('; '),
      lastUpdated: formatTimeAgo(item.lastUpdated)
    }))
    
    // 生成 CSV
    const headers = ['主机', '端口', 'IP', '服务', '标题', '状态码', '技术栈', '最后更新']
    
    let csvContent = '\uFEFF' // BOM for UTF-8
    csvContent += headers.join(',') + '\n'
    
    exportList.forEach(row => {
      const values = [
        row.host,
        row.port,
        row.ip,
        row.service,
        `"${(row.title || '').replace(/"/g, '""')}"`,
        row.status,
        `"${(row.technologies || '').replace(/"/g, '""')}"`,
        row.lastUpdated
      ]
      csvContent += values.join(',') + '\n'
    })
    
    // 下载文件
    const now = new Date()
    const filename = `asset_inventory_${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}.csv`
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    
    ElMessage.success('导出成功')
  } catch (error) {
    console.error('导出失败:', error)
    ElMessage.error('导出失败')
  }
}

const handleSelectionChange = (selection) => {
  selectedAssets.value = selection
}

const handleAction = async (command, row) => {
  switch (command) {
    case 'view':
      // 跳转到资产管理页面的资产清单标签页，并搜索该资产
      window.location.href = `/asset-management?tab=inventory&domain=${encodeURIComponent(row.host)}`
      break
    case 'scan':
      // 将资产添加到扫描目标并跳转
      sessionStorage.setItem('scanTargets', `${row.host}:${row.port}`)
      ElMessage.success(`正在跳转到扫描任务创建页面...`)
      setTimeout(() => {
        window.location.href = '/task/create'
      }, 500)
      break
    case 'screenshot':
      // 跳转到截图页面并搜索该资产
      window.location.href = `/asset-management?tab=screenshots&domain=${encodeURIComponent(row.host)}`
      break
    case 'delete':
      try {
        await ElMessageBox.confirm(
          `确定删除资产 "${row.host}" 吗？`,
          '警告',
          {
            confirmButtonText: '确定',
            cancelButtonText: '取消',
            type: 'warning'
          }
        )
        const index = assets.value.findIndex(a => a.id === row.id)
        if (index > -1) {
          assets.value.splice(index, 1)
        }
        ElMessage.success('删除成功')
      } catch {
        // 用户取消
      }
      break
  }
}

const handleSizeChange = (size) => {
  pageSize.value = size
  currentPage.value = 1
}

const handleCurrentChange = (page) => {
  currentPage.value = page
}

const getStatusType = (status) => {
  if (status.startsWith('2')) return 'success'
  if (status.startsWith('3')) return 'warning'
  if (status.startsWith('4') || status.startsWith('5')) return 'danger'
  return 'info'
}

const formatTimeAgo = (date) => {
  const now = new Date()
  const diff = now - date
  const hours = Math.floor(diff / (1000 * 60 * 60))
  
  if (hours < 1) {
    const minutes = Math.floor(diff / (1000 * 60))
    return `${minutes}分钟前`
  } else if (hours < 24) {
    return `${hours}小时前`
  } else {
    const days = Math.floor(hours / 24)
    return `${days}天前`
  }
}

onMounted(() => {
  // 初始化
})
</script>

<style lang="scss" scoped>
.asset-inventory {
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
  
  .header-actions {
    display: flex;
    gap: 12px;
  }
}

.search-filters {
  margin-bottom: 16px;
  
  .search-section {
    display: flex;
    gap: 12px;
    align-items: center;
    
    .search-input {
      flex: 1;
      max-width: 400px;
    }
  }
}

.filter-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
  flex-wrap: wrap;
  align-items: center;
  
  .filter-actions {
    margin-left: auto;
    display: flex;
    gap: 8px;
  }
}

.advanced-filters {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
  padding: 20px;
  margin-bottom: 24px;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 16px;
  
  .filter-group {
    label {
      display: block;
      font-size: 14px;
      font-weight: 500;
      color: hsl(var(--foreground));
      margin-bottom: 8px;
    }
  }
  
  .filter-actions-bottom {
    grid-column: 1 / -1;
    display: flex;
    gap: 12px;
    justify-content: flex-end;
    padding-top: 16px;
    border-top: 1px solid hsl(var(--border));
  }
}

.assets-table {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
  padding: 16px;
  
  .host-cell {
    .host-info {
      display: flex;
      align-items: center;
      gap: 12px;
      
      .host-icon {
        font-size: 20px;
        color: hsl(var(--primary));
      }
      
      .host-name {
        font-weight: 500;
        color: hsl(var(--foreground));
        font-size: 14px;
      }
      
      .host-ip {
        font-size: 12px;
        color: hsl(var(--muted-foreground));
      }
    }
  }
  
  .service-name {
    font-size: 13px;
    color: hsl(var(--foreground));
  }
  
  .title-text {
    font-size: 13px;
    color: hsl(var(--muted-foreground));
  }
  
  .tech-tags {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    
    .tech-tag {
      font-size: 11px;
    }
  }
  
  .time-text {
    font-size: 12px;
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
