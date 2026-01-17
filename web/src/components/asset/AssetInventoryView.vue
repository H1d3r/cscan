<template>
  <div class="asset-inventory-view">
    <!-- 搜索和过滤区域 -->
    <div class="search-section">
      <el-input
        v-model="searchQuery"
        :placeholder="$t('asset.searchInHostNames')"
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
              <el-dropdown-item command="name-asc">{{ $t('asset.nameAZ') }}</el-dropdown-item>
              <el-dropdown-item command="name-desc">{{ $t('asset.nameZA') }}</el-dropdown-item>
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
        <el-button @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
        </el-button>
      </div>
    </div>

    <!-- 高级过滤器 -->
    <el-collapse-transition>
      <div v-show="showFilters" class="filters-panel">
        <el-form :inline="true" size="small">
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
          <el-form-item :label="$t('asset.labels')">
            <el-select v-model="filters.labels" multiple :placeholder="$t('common.select')" style="width: 200px">
              <el-option label="Production" value="prod" />
              <el-option label="Development" value="dev" />
            </el-select>
          </el-form-item>
          <el-form-item :label="$t('asset.domains')">
            <el-input v-model="filters.domain" placeholder="example.com" style="width: 200px" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="applyFilters">{{ $t('asset.apply') }}</el-button>
            <el-button @click="resetFilters">{{ $t('asset.reset') }}</el-button>
          </el-form-item>
        </el-form>
      </div>
    </el-collapse-transition>

    <!-- 资产列表 -->
    <div class="assets-grid" v-loading="loading">
      <div v-for="asset in assets" :key="asset.id" class="asset-card">
        <!-- 截图 -->
        <div class="asset-screenshot">
          <el-image
            v-if="asset.screenshot"
            :src="getScreenshotUrl(asset.screenshot)"
            :preview-src-list="[getScreenshotUrl(asset.screenshot)]"
            fit="cover"
            class="screenshot-img"
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
        </div>

        <!-- 资产信息 -->
        <div class="asset-info">
          <div class="asset-header">
            <a :href="getAssetUrl(asset)" target="_blank" class="asset-url">
              {{ asset.authority }}
            </a>
            <el-button
              type="danger"
              size="small"
              text
              @click="handleDelete(asset)"
              class="delete-btn"
            >
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>

          <!-- 服务标签 -->
          <div class="asset-services">
            <el-tag
              v-for="(service, idx) in getServices(asset)"
              :key="idx"
              size="small"
              :type="getServiceType(service)"
              class="service-tag"
            >
              {{ service }}
            </el-tag>
          </div>

          <!-- 更新时间 -->
          <div class="asset-footer">
            <el-tooltip placement="top">
              <template #content>
                <div>First seen: {{ asset.createTime }}</div>
                <div>Last updated: {{ asset.updateTime }}</div>
              </template>
              <span class="update-time">
                <el-icon><Clock /></el-icon>
                {{ formatTimeAgo(asset.updateTime) }}
              </span>
            </el-tooltip>
            
            <el-tag v-if="asset.isNew" type="success" size="small" effect="dark">New</el-tag>
            <el-tag v-else-if="asset.isUpdated" type="warning" size="small" effect="dark">Updated</el-tag>
          </div>
        </div>
      </div>
    </div>

    <!-- 空状态 -->
    <el-empty v-if="!loading && assets.length === 0" :description="$t('asset.noAssetsFound')" />

    <!-- 分页 -->
    <el-pagination
      v-if="assets.length > 0"
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.pageSize"
      :total="pagination.total"
      :page-sizes="[12, 24, 48, 96]"
      layout="total, sizes, prev, pager, next"
      class="pagination"
      @size-change="loadData"
      @current-change="loadData"
    />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Filter, Sort, Clock, Refresh, Picture, Delete } from '@element-plus/icons-vue'
import { getAssetList, batchDeleteAsset } from '@/api/asset'

const loading = ref(false)
const searchQuery = ref('')
const showFilters = ref(false)
const assets = ref([])

const filters = reactive({
  tech: [],
  port: '',
  labels: [],
  domain: ''
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
      ...filters
    })
    
    if (res.code === 0) {
      assets.value = res.list || []
      pagination.total = res.total || 0
    }
  } catch (error) {
    console.error('加载资产失败:', error)
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

function handleRefresh() {
  loadData()
}

function applyFilters() {
  pagination.page = 1
  loadData()
}

function resetFilters() {
  Object.assign(filters, {
    tech: [],
    port: '',
    labels: [],
    domain: ''
  })
  loadData()
}

async function handleDelete(asset) {
  try {
    await ElMessageBox.confirm(`确定删除资产 ${asset.authority} 吗？`, '提示', {
      type: 'warning'
    })
    
    const res = await batchDeleteAsset({ ids: [asset.id] })
    if (res.code === 0) {
      ElMessage.success('删除成功')
      loadData()
    }
  } catch (e) {
    // 用户取消
  }
}

function getAssetUrl(asset) {
  const scheme = asset.service === 'https' || asset.port === 443 ? 'https' : 'http'
  return `${scheme}://${asset.host}:${asset.port}`
}

function getScreenshotUrl(screenshot) {
  if (!screenshot) return ''
  if (screenshot.startsWith('data:') || screenshot.startsWith('/9j/')) {
    return screenshot.startsWith('data:') ? screenshot : `data:image/png;base64,${screenshot}`
  }
  return `/api/screenshot/${screenshot}`
}

function getServices(asset) {
  const services = []
  if (asset.service) services.push(asset.service)
  if (asset.app && asset.app.length > 0) {
    services.push(...asset.app.slice(0, 3))
  }
  return services
}

function getServiceType(service) {
  if (service.toLowerCase().includes('http')) return 'primary'
  if (service.toLowerCase().includes('ssh')) return 'warning'
  return 'info'
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
.asset-inventory-view {
  padding: 20px;
}

.search-section {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
}

.search-input {
  flex: 1;
  max-width: 500px;
}

.filter-actions {
  display: flex;
  gap: 8px;
}

.filters-panel {
  background: var(--el-fill-color-light);
  padding: 16px;
  border-radius: 8px;
  margin-bottom: 20px;
}

.assets-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 20px;
  margin-bottom: 20px;
}

.asset-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  overflow: hidden;
  transition: all 0.3s;
}

.asset-card:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  transform: translateY(-2px);
}

.asset-screenshot {
  width: 100%;
  height: 180px;
  background: var(--el-fill-color-light);
  position: relative;
}

.screenshot-img {
  width: 100%;
  height: 100%;
}

.no-screenshot {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-placeholder);
  font-size: 14px;
}

.no-screenshot .el-icon {
  font-size: 48px;
  margin-bottom: 8px;
}

.image-slot {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-placeholder);
  font-size: 48px;
}

.asset-info {
  padding: 12px;
}

.asset-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.asset-url {
  color: var(--el-color-primary);
  text-decoration: none;
  font-weight: 500;
  font-size: 14px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.asset-url:hover {
  text-decoration: underline;
}

.delete-btn {
  flex-shrink: 0;
}

.asset-services {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 8px;
  min-height: 24px;
}

.service-tag {
  font-size: 11px;
}

.asset-footer {
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
  cursor: help;
}

.pagination {
  display: flex;
  justify-content: center;
}

/* 暗黑模式适配 */
html.dark .asset-card {
  background: var(--el-bg-color-overlay);
}

html.dark .asset-card:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}
</style>
