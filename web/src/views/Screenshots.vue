<template>
  <div class="screenshots">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-content">
        <h1>{{ $t('navigation.screenshots') }}</h1>
        <p class="description">
          自动捕获的网页截图，快速识别登录页面、管理后台等重要资产
        </p>
      </div>
      <div class="header-actions">
        <el-button @click="exportData">
          <el-icon><Download /></el-icon>
          {{ $t('common.export') }}
        </el-button>
      </div>
    </div>

    <!-- 搜索和过滤 -->
    <div class="search-filters">
      <div class="search-section">
        <el-button @click="showFilters = !showFilters" :type="showFilters ? 'primary' : 'default'">
          <el-icon><Filter /></el-icon>
          {{ $t('common.addFilters') }}
        </el-button>
        <el-input
          v-model="searchQuery"
          :placeholder="$t('asset.searchInScreenshots')"
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
        所有截图
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

    <!-- 截图网格 -->
    <div class="screenshots-container">
      <div v-loading="loading" class="screenshots-grid">
        <div
          v-for="screenshot in paginatedScreenshots"
          :key="screenshot.id"
          class="screenshot-card"
          @click="viewScreenshotDetails(screenshot)"
        >
          <!-- 截图图片 -->
          <div 
            class="screenshot-image-container"
            @mouseenter="showPreview(screenshot, $event)"
            @mouseleave="hidePreview"
          >
            <img
              v-if="screenshot.screenshot"
              :src="formatScreenshotUrl(screenshot.screenshot)"
              :alt="screenshot.name"
              class="screenshot-image"
              loading="lazy"
              @error="handleScreenshotError"
            />
            <div v-else class="no-screenshot">
              <el-icon><Picture /></el-icon>
              <span>{{ $t('assetInventory.noScreenshot') }}</span>
            </div>
            
            <!-- 状态标签 -->
            <div class="screenshot-status">
              <el-tag :type="getStatusType(screenshot.status)" size="small">
                {{ screenshot.status }}
              </el-tag>
            </div>
          </div>

          <!-- 截图信息 -->
          <div class="screenshot-info">
            <div class="screenshot-title">
              <el-icon class="icon"><Monitor /></el-icon>
              <span class="name">{{ screenshot.name }}</span>
              <span class="port">:{{ screenshot.port }}</span>
            </div>
            
            <div class="screenshot-meta">
              <span class="page-title">{{ screenshot.title || '无标题' }}</span>
            </div>
            
            <div class="screenshot-details">
              <span class="ip">{{ screenshot.ip }}</span>
              <span class="time">{{ formatTimeAgo(screenshot.lastUpdated) }}</span>
            </div>
            
            <!-- 技术标签 -->
            <div v-if="screenshot.technologies && screenshot.technologies.length" class="tech-tags">
              <el-tag
                v-for="tech in screenshot.technologies.slice(0, 3)"
                :key="tech.name"
                size="small"
                class="tech-tag"
              >
                {{ tech.name }}
              </el-tag>
              <el-tag v-if="screenshot.technologies.length > 3" size="small" type="info">
                +{{ screenshot.technologies.length - 3 }}
              </el-tag>
            </div>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
      <div v-if="!loading && filteredScreenshots.length === 0" class="empty-state">
        <el-empty description="暂无截图数据">
          <el-button type="primary" @click="$router.push('/task/create')">
            创建扫描任务
          </el-button>
        </el-empty>
      </div>

      <!-- 分页 -->
      <div v-if="filteredScreenshots.length > 0" class="pagination-container">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[12, 24, 48, 96]"
          :total="totalScreenshots"
          layout="total, sizes, prev, pager, next"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </div>

    <!-- 截图详情对话框 -->
    <el-dialog
      v-model="showDetailsDialog"
      :title="selectedScreenshot?.name"
      width="900px"
      class="screenshot-dialog"
    >
      <div v-if="selectedScreenshot" class="screenshot-details-content">
        <!-- 大图预览 -->
        <div 
          class="large-screenshot"
          @mouseenter="showPreview(selectedScreenshot, $event)"
          @mouseleave="hidePreview"
        >
          <img
            v-if="selectedScreenshot.screenshot"
            :src="formatScreenshotUrl(selectedScreenshot.screenshot)"
            :alt="selectedScreenshot.name"
            class="large-image"
            loading="lazy"
            @error="handleScreenshotError"
          />
          <div v-else class="no-screenshot-large">
            <el-icon><Picture /></el-icon>
            <span>无截图</span>
          </div>
        </div>
        
        <!-- 详细信息 -->
        <div class="details-info">
          <div class="info-section">
            <h3>基本信息</h3>
            <div class="info-grid">
              <div class="info-item">
                <label>主机名:</label>
                <span>{{ selectedScreenshot.name }}</span>
              </div>
              <div class="info-item">
                <label>IP地址:</label>
                <span>{{ selectedScreenshot.ip }}</span>
              </div>
              <div class="info-item">
                <label>端口:</label>
                <span>{{ selectedScreenshot.port }}</span>
              </div>
              <div class="info-item">
                <label>状态:</label>
                <el-tag :type="getStatusType(selectedScreenshot.status)">
                  {{ selectedScreenshot.status }} {{ selectedScreenshot.statusText }}
                </el-tag>
              </div>
              <div class="info-item">
                <label>页面标题:</label>
                <span>{{ selectedScreenshot.title || '无标题' }}</span>
              </div>
              <div class="info-item">
                <label>发现时间:</label>
                <span>{{ formatDateTime(selectedScreenshot.lastUpdated) }}</span>
              </div>
            </div>
          </div>
          
          <div v-if="selectedScreenshot.technologies && selectedScreenshot.technologies.length" class="info-section">
            <h3>技术栈</h3>
            <div class="tech-list">
              <el-tag
                v-for="tech in selectedScreenshot.technologies"
                :key="tech.name"
                class="tech-tag"
              >
                {{ tech.name }}
              </el-tag>
            </div>
          </div>
        </div>
      </div>
      
      <template #footer>
        <el-button @click="showDetailsDialog = false">关闭</el-button>
        <el-button type="primary" @click="openInNewTab(selectedScreenshot)">
          在新标签页打开
        </el-button>
      </template>
    </el-dialog>
    
    <!-- 图片预览浮层 -->
    <Teleport to="body">
      <Transition name="preview-fade">
        <div
          v-if="previewVisible"
          class="screenshot-preview-overlay"
          :style="{
            left: previewPosition.x + 'px',
            top: previewPosition.y + 'px',
            width: previewSize.width + 'px',
            maxHeight: previewSize.height + 'px'
          }"
        >
          <div class="preview-container">
            <img
              :src="previewImage"
              alt="Screenshot Preview"
              class="preview-image"
              @error="handleScreenshotError"
            />
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  Search,
  Filter,
  Download,
  Monitor,
  Picture,
  Sort,
  Clock,
  Refresh,
  ArrowDown
} from '@element-plus/icons-vue'
import { getScreenshots } from '@/api/asset'
import { formatScreenshotUrl, handleScreenshotError } from '@/utils/screenshot'

const { t } = useI18n()

// 响应式数据
const loading = ref(false)
const searchQuery = ref('')
const showFilters = ref(false)
const showMoreFilters = ref(false)
const activeTab = ref('all')
const filterStatus = ref('')
const filterTime = ref('')
const currentPage = ref(1)
const pageSize = ref(24)
const showDetailsDialog = ref(false)
const selectedScreenshot = ref(null)

// 图片预览
const previewVisible = ref(false)
const previewImage = ref('')
const previewPosition = ref({ x: 0, y: 0 })
const previewSize = ref({ width: 500, height: 400 })

const showPreview = (screenshot, event) => {
  if (!screenshot.screenshot) return
  
  previewImage.value = formatScreenshotUrl(screenshot.screenshot)
  previewVisible.value = true
  
  // 计算预览位置
  const rect = event.currentTarget.getBoundingClientRect()
  
  // 检查是否在抽屉或对话框中
  const isInDrawer = event.currentTarget.closest('.el-drawer__body') !== null
  const isInDialog = event.currentTarget.closest('.el-dialog__body') !== null
  const isInDetailView = isInDrawer || isInDialog
  
  let previewWidth, previewHeight, padding
  
  if (isInDetailView) {
    // 在详情视图中，使用更大的预览尺寸
    previewWidth = Math.min(800, window.innerWidth * 0.5)
    previewHeight = Math.min(900, window.innerHeight * 0.8)
    padding = 30
  } else {
    // 在列表视图中，使用较小的预览尺寸
    previewWidth = 500
    previewHeight = 400
    padding = 20
  }
  
  previewSize.value = { width: previewWidth, height: previewHeight }
  
  // 默认显示在右侧
  let x = rect.right + padding
  let y = rect.top
  
  // 如果右侧空间不够，显示在左侧
  if (x + previewWidth > window.innerWidth) {
    x = rect.left - previewWidth - padding
  }
  
  // 如果下方空间不够，向上调整
  if (y + previewHeight > window.innerHeight) {
    y = window.innerHeight - previewHeight - padding
  }
  
  // 确保不超出顶部
  if (y < padding) {
    y = padding
  }
  
  // 确保不超出左侧
  if (x < padding) {
    x = padding
  }
  
  previewPosition.value = { x, y }
}

const hidePreview = () => {
  previewVisible.value = false
}

// 截图数据
const screenshots = ref([])
const totalScreenshots = ref(0)

// 计算属性
const filteredScreenshots = computed(() => {
  return screenshots.value
})

const paginatedScreenshots = computed(() => {
  return screenshots.value
})

// 方法
const loadScreenshots = async () => {
  loading.value = true
  try {
    const res = await getScreenshots({
      page: currentPage.value,
      pageSize: pageSize.value,
      query: searchQuery.value,
      technologies: [],
      ports: [],
      statusCodes: filterStatus.value ? [filterStatus.value] : [],
      timeRange: filterTime.value || 'all',
      hasScreenshot: true
    })
    
    if (res.code === 0) {
      screenshots.value = res.list || []
      totalScreenshots.value = res.total || 0
    } else {
      ElMessage.error(res.msg || '加载失败')
    }
  } catch (error) {
    console.error('加载截图失败:', error)
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  currentPage.value = 1
  loadScreenshots()
}

const handleSort = (command) => {
  ElMessage.success(`按${command}排序`)
  loadScreenshots()
}

const handleTimeFilter = (command) => {
  filterTime.value = command
  currentPage.value = 1
  loadScreenshots()
}

const refreshData = async () => {
  await loadScreenshots()
  ElMessage.success('刷新成功')
}

const applyFilters = () => {
  currentPage.value = 1
  loadScreenshots()
}

const resetFilters = () => {
  filterStatus.value = ''
  filterTime.value = ''
  currentPage.value = 1
  loadScreenshots()
}

const handleSizeChange = (size) => {
  pageSize.value = size
  currentPage.value = 1
  loadScreenshots()
}

const handleCurrentChange = (page) => {
  currentPage.value = page
  loadScreenshots()
}

const viewScreenshotDetails = (screenshot) => {
  selectedScreenshot.value = screenshot
  showDetailsDialog.value = true
}

const exportData = async () => {
  if (screenshots.value.length === 0) {
    ElMessage.warning(t('asset.noDataToExport'))
    return
  }
  
  try {
    ElMessage.info(t('asset.exportPreparing'))
    
    // 准备导出数据
    const exportList = screenshots.value.map(item => ({
      host: item.name || item.host,
      port: item.port,
      ip: item.ip,
      title: item.title || '',
      status: item.status,
      technologies: (item.technologies || []).map(t => t.name || t).join('; '),
      lastUpdated: item.lastUpdated
    }))
    
    // 生成 CSV
    const headers = [
      t('asset.host'),
      t('asset.port'),
      t('asset.ip'),
      t('asset.pageTitle'),
      t('asset.statusCode'),
      t('asset.technologies'),
      t('asset.lastUpdated')
    ]
    
    let csvContent = '\uFEFF' // BOM for UTF-8
    csvContent += headers.join(',') + '\n'
    
    exportList.forEach(row => {
      const values = [
        row.host,
        row.port,
        row.ip,
        `"${(row.title || '').replace(/"/g, '""')}"`,
        row.status,
        `"${(row.technologies || '').replace(/"/g, '""')}"`,
        row.lastUpdated
      ]
      csvContent += values.join(',') + '\n'
    })
    
    // 下载文件
    const now = new Date()
    const filename = `screenshots_${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}.csv`
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    
    ElMessage.success(t('asset.exportSuccess'))
  } catch (error) {
    console.error('导出失败:', error)
    ElMessage.error(t('asset.exportFailed'))
  }
}

const getStatusType = (status) => {
  if (status.startsWith('2')) return 'success'
  if (status.startsWith('3')) return 'warning'
  if (status.startsWith('4') || status.startsWith('5')) return 'danger'
  return 'info'
}

const formatTimeAgo = (dateStr) => {
  // dateStr 已经是格式化后的相对时间字符串
  return dateStr
}

const formatDateTime = (dateStr) => {
  return dateStr
}

const openInNewTab = (screenshot) => {
  const url = `http://${screenshot.name}:${screenshot.port}`
  window.open(url, '_blank')
}

onMounted(() => {
  loadScreenshots()
})
</script>

<style lang="scss" scoped>
.screenshots {
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

.screenshots-container {
  .screenshots-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 20px;
    margin-bottom: 24px;
  }
}

.screenshot-card {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
  overflow: hidden;
  cursor: pointer;
  transition: all 0.2s ease;
  
  &:hover {
    border-color: hsl(var(--primary));
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
    transform: translateY(-2px);
  }
}

.screenshot-image-container {
  position: relative;
  height: 200px;
  background: hsl(var(--muted));
  overflow: hidden;
  
  .screenshot-image {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  
  .no-screenshot {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: hsl(var(--muted-foreground));
    
    .el-icon {
      font-size: 48px;
      margin-bottom: 8px;
    }
  }
  
  .screenshot-status {
    position: absolute;
    top: 8px;
    right: 8px;
  }
}

.screenshot-info {
  padding: 16px;
  
  .screenshot-title {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 8px;
    
    .icon {
      color: hsl(var(--muted-foreground));
      font-size: 16px;
    }
    
    .name {
      font-weight: 500;
      color: hsl(var(--foreground));
      font-size: 14px;
    }
    
    .port {
      color: hsl(var(--primary));
      font-weight: 500;
      font-size: 14px;
    }
  }
  
  .screenshot-meta {
    margin-bottom: 8px;
    
    .page-title {
      font-size: 13px;
      color: hsl(var(--muted-foreground));
      display: block;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }
  
  .screenshot-details {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    color: hsl(var(--muted-foreground));
    margin-bottom: 12px;
  }
  
  .tech-tags {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    
    .tech-tag {
      font-size: 11px;
    }
  }
}

.empty-state {
  padding: 60px 20px;
  text-align: center;
}

.pagination-container {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}

.screenshot-dialog {
  .screenshot-details-content {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }
  
  .large-screenshot {
    width: 100%;
    max-height: 500px;
    background: hsl(var(--muted));
    border-radius: 8px;
    overflow: hidden;
    display: flex;
    align-items: center;
    justify-content: center;
    
    .large-image {
      width: 100%;
      height: auto;
      max-height: 500px;
      object-fit: contain;
    }
    
    .no-screenshot-large {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      height: 300px;
      color: hsl(var(--muted-foreground));
      
      .el-icon {
        font-size: 64px;
        margin-bottom: 16px;
      }
    }
  }
  
  .details-info {
    .info-section {
      margin-bottom: 24px;
      
      h3 {
        margin: 0 0 16px 0;
        color: hsl(var(--foreground));
        font-size: 16px;
        font-weight: 600;
      }
      
      .info-grid {
        display: grid;
        grid-template-columns: repeat(2, 1fr);
        gap: 12px;
        
        .info-item {
          display: flex;
          align-items: center;
          gap: 8px;
          
          label {
            font-weight: 500;
            color: hsl(var(--muted-foreground));
            min-width: 80px;
          }
          
          span {
            color: hsl(var(--foreground));
          }
        }
      }
      
      .tech-list {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
      }
    }
  }
}

// 图片预览样式
.screenshot-preview-overlay {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  max-width: 90vw;
  
  .preview-container {
    background: hsl(var(--card));
    border: 2px solid hsl(var(--primary));
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
    overflow: hidden;
    width: 100%;
    height: 100%;
    
    .preview-image {
      width: 100%;
      height: 100%;
      object-fit: contain;
      display: block;
    }
  }
}

// 预览动画
.preview-fade-enter-active,
.preview-fade-leave-active {
  transition: opacity 0.2s ease;
}

.preview-fade-enter-from,
.preview-fade-leave-to {
  opacity: 0;
}
</style>

