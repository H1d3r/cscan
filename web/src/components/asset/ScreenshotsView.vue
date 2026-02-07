<template>
  <div class="screenshots-view">
    <!-- 搜索和过滤区域 -->
    <div class="search-section">
      <el-input
        v-model="searchQuery"
        :placeholder="$t('asset.searchInScreenshots')"
        clearable
        @input="handleSearch"
        class="search-input"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      
      <div class="filter-actions">
        <el-button @click="showFilters = !showFilters">
          <el-icon><Filter /></el-icon>
          {{ $t('asset.addFilters') }}
        </el-button>
        <el-dropdown @command="handleSort">
          <el-button>
            <el-icon><Sort /></el-icon>
            {{ $t('asset.sort') }}
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="time-desc">{{ $t('asset.latestFirst') }}</el-dropdown-item>
              <el-dropdown-item command="time-asc">{{ $t('asset.oldestFirst') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-dropdown @command="handleTimeFilter">
          <el-button>
            <el-icon><Clock /></el-icon>
            {{ $t('asset.allTime') }}
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="all">{{ $t('asset.allTime') }}</el-dropdown-item>
              <el-dropdown-item command="1">{{ $t('asset.last24Hours') }}</el-dropdown-item>
              <el-dropdown-item command="7">{{ $t('asset.last7Days') }}</el-dropdown-item>
              <el-dropdown-item command="30">{{ $t('asset.last30Days') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button @click="showExportMenu">
          <el-icon><Download /></el-icon>
          {{ $t('asset.export') }}
        </el-button>
      </div>
    </div>

    <!-- 高级过滤器 -->
    <el-collapse-transition>
      <div v-show="showFilters" class="filters-panel">
        <el-form :inline="true" size="small">
          <el-form-item :label="$t('asset.response')">
            <el-select v-model="filters.response" :placeholder="$t('common.select')" style="width: 150px">
              <el-option :label="$t('common.all')" value="" />
              <el-option label="200" value="200" />
              <el-option label="404" value="404" />
              <el-option label="500" value="500" />
            </el-select>
          </el-form-item>
          <el-form-item :label="$t('asset.technologies')">
            <el-select v-model="filters.tech" multiple :placeholder="$t('common.select')" style="width: 200px">
              <el-option label="Nginx" value="nginx" />
              <el-option label="Apache" value="apache" />
              <el-option label="PHP" value="php" />
            </el-select>
          </el-form-item>
          <el-form-item :label="$t('asset.ports')">
            <el-input v-model="filters.port" placeholder="80,443" style="width: 150px" />
          </el-form-item>
          <el-form-item :label="$t('asset.title')">
            <el-input v-model="filters.title" :placeholder="$t('asset.pageTitle')" style="width: 200px" />
          </el-form-item>
          <el-form-item :label="$t('asset.contentLength')">
            <el-input v-model="filters.contentLength" placeholder="Min-Max" style="width: 150px" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="applyFilters">{{ $t('asset.apply') }}</el-button>
            <el-button @click="resetFilters">{{ $t('asset.reset') }}</el-button>
          </el-form-item>
        </el-form>
      </div>
    </el-collapse-transition>

    <!-- 截图网格 -->
    <div class="screenshots-grid" v-loading="loading">
      <div v-for="item in screenshots" :key="item.id" class="screenshot-card">
        <!-- 截图图片 -->
        <div class="screenshot-image" @click="viewDetails(item)">
          <el-image
            v-if="item.screenshot"
            :src="getScreenshotUrl(item.screenshot)"
            :preview-src-list="[getScreenshotUrl(item.screenshot)]"
            fit="cover"
            class="image"
          >
            <template #error>
              <div class="image-slot">
                <el-icon><Picture /></el-icon>
              </div>
            </template>
          </el-image>
          <div v-else class="no-screenshot">
            <el-icon><Picture /></el-icon>
            <span>{{ $t('asset.noScreenshot') }}</span>
          </div>
          
          <!-- 悬浮信息 -->
          <div class="screenshot-overlay">
            <div class="overlay-info">
              <el-tag size="small" :type="getStatusType(item.httpStatus)">
                {{ item.httpStatus || 'N/A' }}
              </el-tag>
              <span class="content-length">{{ formatSize(item.contentLength) }}</span>
            </div>
          </div>
        </div>

        <!-- 截图信息 -->
        <div class="screenshot-info">
          <a :href="getAssetUrl(item)" target="_blank" class="screenshot-url" :title="item.authority">
            {{ item.authority }}
          </a>
          
          <div class="screenshot-title" :title="item.title">
            {{ item.title || $t('asset.noContent') }}
          </div>

          <!-- 技术标签 -->
          <div class="screenshot-tags">
            <el-tag
              v-for="(tech, idx) in getTechnologies(item)"
              :key="idx"
              size="small"
              class="tech-tag"
            >
              {{ tech }}
            </el-tag>
          </div>

          <!-- 底部信息 -->
          <div class="screenshot-footer">
            <span class="update-time">
              <el-icon><Clock /></el-icon>
              {{ formatTimeAgo(item.updateTime) }}
            </span>
            <el-button
              type="text"
              size="small"
              @click="viewDetails(item)"
              class="details-btn"
            >
              {{ $t('asset.details') }}
            </el-button>
          </div>
        </div>
      </div>
    </div>

    <!-- 空状态 -->
    <el-empty v-if="!loading && screenshots.length === 0" :description="$t('asset.noScreenshotsFound')" />

    <!-- 分页 -->
    <el-pagination
      v-if="screenshots.length > 0"
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.pageSize"
      :total="pagination.total"
      :page-sizes="[12, 24, 48, 96]"
      layout="total, sizes, prev, pager, next"
      class="pagination"
      @size-change="loadData"
      @current-change="loadData"
    />

    <!-- 详情对话框 -->
    <el-dialog
      v-model="detailsVisible"
      :title="currentItem?.authority"
      width="900px"
    >
      <div v-if="currentItem" class="details-content">
        <el-image
          v-if="currentItem.screenshot"
          :src="getScreenshotUrl(currentItem.screenshot)"
          :preview-src-list="[getScreenshotUrl(currentItem.screenshot)]"
          fit="contain"
          class="details-image"
        />
        
        <el-descriptions :column="2" border class="details-desc">
          <el-descriptions-item label="URL">
            <a :href="getAssetUrl(currentItem)" target="_blank">{{ getAssetUrl(currentItem) }}</a>
          </el-descriptions-item>
          <el-descriptions-item label="Status">
            <el-tag :type="getStatusType(currentItem.httpStatus)">{{ currentItem.httpStatus }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="Title" :span="2">{{ currentItem.title || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Server">{{ currentItem.server || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Content Length">{{ formatSize(currentItem.contentLength) }}</el-descriptions-item>
          <el-descriptions-item label="Technologies" :span="2">
            <el-tag v-for="(tech, idx) in getTechnologies(currentItem)" :key="idx" size="small" style="margin: 2px">
              {{ tech }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="First Seen">{{ currentItem.createTime }}</el-descriptions-item>
          <el-descriptions-item label="Last Updated">{{ currentItem.updateTime }}</el-descriptions-item>
        </el-descriptions>
      </div>
      
      <template #footer>
        <el-button @click="detailsVisible = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Filter, Sort, Clock, Download, Picture } from '@element-plus/icons-vue'
import { getAssetList } from '@/api/asset'

const loading = ref(false)
const searchQuery = ref('')
const showFilters = ref(false)
const screenshots = ref([])
const detailsVisible = ref(false)
const currentItem = ref(null)

const filters = reactive({
  response: '',
  tech: [],
  port: '',
  title: '',
  contentLength: ''
})

const pagination = reactive({
  page: 1,
  pageSize: 24,
  total: 0
})

const sortBy = ref('time-desc')
const timeFilter = ref('all')

async function loadData() {
  loading.value = true
  try {
    const res = await getAssetList({
      page: pagination.page,
      pageSize: pagination.pageSize,
      query: searchQuery.value,
      sortBy: sortBy.value,
      timeFilter: timeFilter.value,
      hasScreenshot: true, // 只获取有截图的资产
      ...filters
    })
    
    if (res.code === 0) {
      screenshots.value = res.list || []
      pagination.total = res.total || 0
    }
  } catch (error) {
    console.error('加载截图失败:', error)
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  pagination.page = 1
  loadData()
}

function handleSort(command) {
  sortBy.value = command
  loadData()
}

function handleTimeFilter(command) {
  timeFilter.value = command
  loadData()
}

function applyFilters() {
  pagination.page = 1
  loadData()
}

function resetFilters() {
  Object.assign(filters, {
    response: '',
    tech: [],
    port: '',
    title: '',
    contentLength: ''
  })
  loadData()
}

function showExportMenu() {
  if (tableData.value.length === 0) {
    ElMessage.warning('没有可导出的数据')
    return
  }
  
  try {
    ElMessage.info('正在准备导出数据...')
    
    // 准备导出数据
    const exportList = tableData.value.map(item => ({
      host: item.host,
      port: item.port,
      ip: item.ip || '',
      title: item.title || '',
      status: item.status || '',
      service: item.service || '',
      technologies: getTechnologies(item).join('; ')
    }))
    
    // 生成 CSV
    const headers = ['主机', '端口', 'IP', '标题', '状态码', '服务', '技术栈']
    
    let csvContent = '\uFEFF' // BOM for UTF-8
    csvContent += headers.join(',') + '\n'
    
    exportList.forEach(row => {
      const values = [
        row.host,
        row.port,
        row.ip,
        `"${(row.title || '').replace(/"/g, '""')}"`,
        row.status,
        row.service,
        `"${(row.technologies || '').replace(/"/g, '""')}"`
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
    
    ElMessage.success('导出成功')
  } catch (error) {
    console.error('导出失败:', error)
    ElMessage.error('导出失败')
  }
}

function viewDetails(item) {
  currentItem.value = item
  detailsVisible.value = true
}

function getAssetUrl(item) {
  const scheme = item.service === 'https' || item.port === 443 ? 'https' : 'http'
  return `${scheme}://${item.host}:${item.port}`
}

function getScreenshotUrl(screenshot) {
  if (!screenshot) return ''
  if (screenshot.startsWith('data:') || screenshot.startsWith('/9j/')) {
    return screenshot.startsWith('data:') ? screenshot : `data:image/png;base64,${screenshot}`
  }
  return `/api/screenshot/${screenshot}`
}

function getTechnologies(item) {
  const techs = []
  if (item.service) techs.push(item.service)
  if (item.app && item.app.length > 0) {
    techs.push(...item.app.slice(0, 2))
  }
  return techs
}

function getStatusType(status) {
  if (!status) return 'info'
  if (status >= 200 && status < 300) return 'success'
  if (status >= 300 && status < 400) return 'warning'
  return 'danger'
}

function formatSize(bytes) {
  if (!bytes || bytes < 0) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

function formatTimeAgo(time) {
  if (!time) return ''
  const date = new Date(time)
  const now = new Date()
  const diff = now - date
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(hours / 24)
  
  if (days > 0) return `${days} days ago`
  if (hours > 0) return `${hours} hours ago`
  return 'Just now'
}

onMounted(() => {
  loadData()
})

defineExpose({ refresh: loadData })
</script>

<style scoped>
.screenshots-view {
  padding: 20px;
}

.search-section {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.search-input {
  flex: 1;
  min-width: 300px;
  max-width: 500px;
}

.filter-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.filters-panel {
  background: var(--el-fill-color-light);
  padding: 16px;
  border-radius: 8px;
  margin-bottom: 20px;
}

.screenshots-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 20px;
  margin-bottom: 20px;
}

.screenshot-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  overflow: hidden;
  transition: all 0.3s;
}

.screenshot-card:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  transform: translateY(-2px);
}

.screenshot-image {
  width: 100%;
  height: 200px;
  background: var(--el-fill-color-light);
  position: relative;
  cursor: pointer;
  overflow: hidden;
}

.image {
  width: 100%;
  height: 100%;
}

.no-screenshot {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-placeholder);
  font-size: 64px;
}

.image-slot {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-placeholder);
  font-size: 64px;
}

.screenshot-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.3s;
}

.screenshot-image:hover .screenshot-overlay {
  opacity: 1;
}

.overlay-info {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.content-length {
  color: white;
  font-size: 14px;
  font-weight: 500;
}

.screenshot-info {
  padding: 12px;
}

.screenshot-url {
  display: block;
  color: var(--el-color-primary);
  text-decoration: none;
  font-weight: 500;
  font-size: 14px;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.screenshot-url:hover {
  text-decoration: underline;
}

.screenshot-title {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-bottom: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.screenshot-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 8px;
  min-height: 24px;
}

.tech-tag {
  font-size: 11px;
}

.screenshot-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-top: 8px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.update-time {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.details-btn {
  padding: 0;
}

.pagination {
  display: flex;
  justify-content: center;
}

.details-content {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.details-image {
  width: 100%;
  max-height: 400px;
}

.details-desc {
  margin-top: 16px;
}

/* 暗黑模式适配 */
html.dark .screenshot-card {
  background: var(--el-bg-color-overlay);
}

html.dark .screenshot-card:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}
</style>
