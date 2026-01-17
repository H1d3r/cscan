<template>
  <div class="screenshots-tab">
    <!-- 搜索和过滤栏 -->
    <div class="toolbar">
      <el-input
        v-model="searchQuery"
        :placeholder="t('asset.screenshotsTab.searchPlaceholder')"
        clearable
        class="search-input"
        @input="handleSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      <el-button @click="showFilters = !showFilters">
        <el-icon><Filter /></el-icon>
        {{ t('asset.screenshotsTab.filters') }}
      </el-button>
      <el-button @click="refreshData">
        <el-icon><Refresh /></el-icon>
        {{ t('asset.screenshotsTab.refresh') }}
      </el-button>
    </div>

    <!-- 高级过滤器 -->
    <div v-if="showFilters" class="filters-panel">
      <el-form :inline="true">
        <el-form-item :label="t('asset.screenshotsTab.statusCodes')">
          <el-select v-model="filters.statusCodes" multiple :placeholder="t('asset.screenshotsTab.selectStatus')" clearable filterable>
            <el-option
              v-for="code in filterOptions.statusCodes"
              :key="code"
              :label="code"
              :value="code"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('asset.screenshotsTab.timeRange')">
          <el-select v-model="filters.timeRange" :placeholder="t('asset.screenshotsTab.selectTime')" clearable>
            <el-option :label="t('asset.screenshotsTab.allTime')" value="all" />
            <el-option :label="t('asset.screenshotsTab.last24h')" value="24h" />
            <el-option :label="t('asset.screenshotsTab.last7d')" value="7d" />
            <el-option :label="t('asset.screenshotsTab.last30d')" value="30d" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="applyFilters">{{ t('asset.screenshotsTab.apply') }}</el-button>
          <el-button @click="resetFilters">{{ t('asset.screenshotsTab.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- 截图网格 -->
    <div v-loading="loading" class="screenshots-grid">
      <div
        v-for="item in screenshots"
        :key="item.id"
        class="screenshot-card"
        @click="viewDetails(item)"
      >
        <!-- 截图图片 -->
        <div 
          class="screenshot-image-container"
          @mouseenter="showPreview(item, $event)"
          @mouseleave="hidePreview"
        >
          <img
            v-if="item.screenshot"
            :src="formatScreenshotUrl(item.screenshot)"
            :alt="item.name"
            class="screenshot-image"
            loading="lazy"
            @error="handleScreenshotError"
          />
          <div v-else class="no-screenshot">
            <el-icon><Picture /></el-icon>
            <span>{{ t('asset.screenshotsTab.noScreenshot') }}</span>
          </div>
          
          <!-- 状态标签 -->
          <div class="screenshot-status">
            <el-tag :type="getStatusType(item.status)" size="small">
              {{ item.status }}
            </el-tag>
          </div>
        </div>

        <!-- 截图信息 -->
        <div class="screenshot-info">
          <div class="screenshot-title">
            <el-icon class="icon"><Monitor /></el-icon>
            <span class="name">{{ item.name }}</span>
            <span class="port">:{{ item.port }}</span>
          </div>
          
          <div class="screenshot-meta">
            <span class="page-title">{{ item.title || t('asset.screenshotsTab.noTitle') }}</span>
          </div>
          
          <div class="screenshot-details">
            <span class="ip">{{ item.ip }}</span>
            <span class="time">{{ item.lastUpdated }}</span>
          </div>
          
          <!-- 技术标签 -->
          <div v-if="item.technologies && item.technologies.length" class="tech-tags">
            <el-tag
              v-for="tech in item.technologies.slice(0, 3)"
              :key="tech.name"
              size="small"
              class="tech-tag"
            >
              {{ tech.name }}
            </el-tag>
            <el-tag v-if="item.technologies.length > 3" size="small" type="info">
              +{{ item.technologies.length - 3 }}
            </el-tag>
          </div>
        </div>
      </div>
    </div>

    <!-- 空状态 -->
    <div v-if="!loading && screenshots.length === 0" class="empty-state">
      <el-empty description="暂无截图数据" />
    </div>

    <!-- 分页 -->
    <el-pagination
      v-if="screenshots.length > 0"
      v-model:current-page="currentPage"
      v-model:page-size="pageSize"
      :total="total"
      :page-sizes="[5, 10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      class="pagination"
      @size-change="loadData"
      @current-change="loadData"
    />

    <!-- 详情抽屉 -->
    <el-drawer
      v-model="showDetailsDialog"
      :title="selectedItem?.name + ':' + selectedItem?.port"
      size="60%"
      direction="rtl"
    >
      <div v-if="selectedItem" class="asset-detail">
        <!-- 顶部截图和基本信息 -->
        <div class="detail-header">
          <div 
            class="detail-screenshot"
            @mouseenter="showPreview(selectedItem, $event)"
            @mouseleave="hidePreview"
          >
            <img 
              v-if="selectedItem.screenshot"
              :src="formatScreenshotUrl(selectedItem.screenshot)"
              :alt="selectedItem.title"
              class="detail-screenshot-img"
            />
            <div v-else class="detail-screenshot-placeholder">
              {{ t('asset.screenshotsTab.noScreenshot') }}
            </div>
          </div>
          <div class="detail-basic-info">
            <div class="info-row">
              <span class="info-label">URL:</span>
              <a :href="`${selectedItem.port === 443 ? 'https' : 'http'}://${selectedItem.name}:${selectedItem.port}`" target="_blank" class="info-value link">
                {{ `${selectedItem.port === 443 ? 'https' : 'http'}://${selectedItem.name}:${selectedItem.port}` }}
              </a>
            </div>
            <div class="info-row">
              <span class="info-label">{{ t('asset.ip') }}:</span>
              <span class="info-value">{{ selectedItem.ip || '-' }}</span>
            </div>
            <div class="info-row">
              <span class="info-label">{{ t('asset.statusCode') }}:</span>
              <el-tag :type="getStatusType(selectedItem.status)" size="small">
                {{ selectedItem.status }}
              </el-tag>
            </div>
            <div v-if="selectedItem.title" class="info-row">
              <span class="info-label">{{ t('asset.title') }}:</span>
              <span class="info-value">{{ selectedItem.title }}</span>
            </div>
          </div>
        </div>
        
        <!-- 标签页 -->
        <el-tabs v-model="activeDetailTab" class="detail-tabs">
          <!-- Overview 标签页 -->
          <el-tab-pane :label="t('asset.assetDetail.overview')" name="overview">
            <div class="tab-content">
              <div class="section">
                <h4 class="section-title">{{ t('asset.assetDetail.networkInfo') }}</h4>
                <div class="info-grid">
                  <div class="info-item">
                    <span class="item-label">{{ t('asset.assetDetail.host') }}:</span>
                    <span class="item-value">{{ selectedItem.name }}</span>
                  </div>
                  <div class="info-item">
                    <span class="item-label">{{ t('asset.assetDetail.port') }}:</span>
                    <span class="item-value">{{ selectedItem.port }}</span>
                  </div>
                  <div class="info-item">
                    <span class="item-label">{{ t('asset.ip') }}:</span>
                    <span class="item-value">{{ selectedItem.ip || '-' }}</span>
                  </div>
                  <div class="info-item">
                    <span class="item-label">{{ t('asset.statusCode') }}:</span>
                    <span class="item-value">{{ selectedItem.status }} {{ selectedItem.statusText || '' }}</span>
                  </div>
                  <div v-if="selectedItem.title" class="info-item">
                    <span class="item-label">{{ t('asset.title') }}:</span>
                    <span class="item-value">{{ selectedItem.title }}</span>
                  </div>
                  <div v-if="selectedItem.lastUpdated" class="info-item">
                    <span class="item-label">{{ t('asset.lastUpdated') }}:</span>
                    <span class="item-value">{{ selectedItem.lastUpdated }}</span>
                  </div>
                </div>
              </div>
              
              <div v-if="selectedItem.httpHeader" class="section">
                <h4 class="section-title">{{ t('asset.assetDetail.httpResponse') }}</h4>
                <div class="code-block">
                  <pre>{{ selectedItem.httpHeader }}</pre>
                </div>
              </div>
              
              <div v-if="selectedItem.httpBody" class="section">
                <h4 class="section-title">{{ t('asset.assetDetail.httpBody') }}</h4>
                <div class="code-block">
                  <pre>{{ selectedItem.httpBody.substring(0, 1000) }}{{ selectedItem.httpBody.length > 1000 ? '...' : '' }}</pre>
                </div>
              </div>
            </div>
          </el-tab-pane>
          
          <!-- Exposures 标签页 -->
          <el-tab-pane name="exposures">
            <template #label>
              <span>{{ t('asset.assetDetail.exposures') }} <el-badge :value="getExposuresCount()" class="tab-badge" /></span>
            </template>
            <div class="tab-content">
              <div class="exposure-item">
                <div class="exposure-header">
                  <el-tag size="small">{{ selectedItem.port }}</el-tag>
                  <span class="exposure-service">{{ selectedItem.service || t('asset.assetDetail.unknown') }}</span>
                </div>
                <div class="exposure-details">
                  <div class="detail-item">
                    <span class="detail-label">{{ t('asset.assetDetail.protocol') }}:</span>
                    <span class="detail-value">{{ selectedItem.port === 443 ? 'HTTPS' : 'HTTP' }}</span>
                  </div>
                  <div v-if="selectedItem.banner" class="detail-item">
                    <span class="detail-label">{{ t('asset.assetDetail.banner') }}:</span>
                    <div class="code-block small">
                      <pre>{{ selectedItem.banner }}</pre>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </el-tab-pane>
          
          <!-- Technologies 标签页 -->
          <el-tab-pane name="technologies">
            <template #label>
              <span>{{ t('asset.assetDetail.technologies') }} <el-badge :value="selectedItem.technologies?.length || 0" class="tab-badge" /></span>
            </template>
            <div class="tab-content">
              <div v-if="selectedItem.technologies && selectedItem.technologies.length > 0" class="tech-list-detail">
                <div v-for="(tech, index) in selectedItem.technologies" :key="index" class="tech-item-detail">
                  <div class="tech-icon">
                    <el-icon><Box /></el-icon>
                  </div>
                  <div class="tech-info">
                    <div class="tech-name">{{ typeof tech === 'string' ? tech : tech.name }}</div>
                    <div class="tech-category">{{ t('asset.assetDetail.techCategory') }}</div>
                  </div>
                </div>
              </div>
              <div v-else class="empty-state">
                {{ t('asset.assetDetail.noTechDetected') }}
              </div>
            </div>
          </el-tab-pane>
          
          <!-- Changelogs 标签页 -->
          <el-tab-pane name="changelogs">
            <template #label>
              <span>{{ t('asset.assetDetail.changelogs') }} <el-badge :value="selectedItem.changelogs?.length || 0" class="tab-badge" /></span>
            </template>
            <div class="tab-content">
              <div v-if="selectedItem.changelogs && selectedItem.changelogs.length > 0" class="changelog-list">
                <div v-for="(log, index) in selectedItem.changelogs" :key="index" class="changelog-item">
                  <div class="changelog-header">
                    <span class="changelog-time">{{ log.time }}</span>
                    <el-tag size="small" type="info">{{ log.taskId }}</el-tag>
                  </div>
                  <div class="changelog-changes">
                    <div v-for="(change, idx) in log.changes" :key="idx" class="change-item">
                      <span class="change-field">{{ translateFieldName(change.field) }}:</span>
                      <span class="change-old">{{ change.oldValue || '-' }}</span>
                      <el-icon class="change-arrow"><Right /></el-icon>
                      <span class="change-new">{{ change.newValue || '-' }}</span>
                    </div>
                  </div>
                </div>
              </div>
              <div v-else class="empty-state">
                {{ t('asset.assetDetail.noChangeHistory') }}
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-drawer>
    
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
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  Search,
  Filter,
  Refresh,
  Picture,
  Monitor,
  Link,
  Box,
  Right
} from '@element-plus/icons-vue'
import { getScreenshots, getAssetFilterOptions, getAssetHistory, getAssetExposures } from '@/api/asset'
import { formatScreenshotUrl, handleScreenshotError } from '@/utils/screenshot'

const { t } = useI18n()

const loading = ref(false)
const searchQuery = ref('')
const showFilters = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const screenshots = ref([])
const showDetailsDialog = ref(false)
const selectedItem = ref(null)
const activeDetailTab = ref('overview')
const filters = ref({
  statusCodes: [],
  timeRange: 'all'
})

// 过滤器选项（从后端动态加载）
const filterOptions = ref({
  statusCodes: []
})

// 图片预览
const previewVisible = ref(false)
const previewImage = ref('')
const previewPosition = ref({ x: 0, y: 0 })
const previewSize = ref({ width: 500, height: 400 })

const showPreview = (item, event) => {
  if (!item.screenshot) return
  
  previewImage.value = formatScreenshotUrl(item.screenshot)
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

const loadData = async () => {
  loading.value = true
  try {
    const res = await getScreenshots({
      page: currentPage.value,
      pageSize: pageSize.value,
      query: searchQuery.value,
      technologies: [],
      ports: [],
      statusCodes: filters.value.statusCodes,
      timeRange: filters.value.timeRange,
      hasScreenshot: true
    })
    if (res.code === 0) {
      screenshots.value = res.list || []
      total.value = res.total || 0
    }
  } catch (error) {
    console.error('加载失败:', error)
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
  ElMessage.success(t('asset.screenshotsTab.refreshSuccess'))
}

const applyFilters = () => {
  currentPage.value = 1
  loadData()
}

const resetFilters = () => {
  filters.value = {
    statusCodes: [],
    timeRange: 'all'
  }
  currentPage.value = 1
  loadData()
}

const viewDetails = async (item) => {
  selectedItem.value = {
    ...item,
    changelogs: [],
    dirScanResults: [],
    vulnScanResults: []
  }
  showDetailsDialog.value = true
  activeDetailTab.value = 'overview'
  
  // 异步加载变更记录和暴露面数据
  if (item.id) {
    await Promise.all([
      loadAssetHistory(item.id),
      loadAssetExposures(item.id)
    ])
  }
}

// 加载资产变更记录
const loadAssetHistory = async (assetId) => {
  try {
    const res = await getAssetHistory({
      assetId: assetId,
      limit: 50
    })
    
    if (res.code === 0 && res.list && selectedItem.value) {
      selectedItem.value.changelogs = res.list.map(item => ({
        time: formatDateTime(item.createTime),
        taskId: item.taskId,
        changes: item.changes || []
      }))
    }
  } catch (error) {
    console.error('加载变更记录失败:', error)
  }
}

// 加载资产暴露面数据
const loadAssetExposures = async (assetId) => {
  try {
    const res = await getAssetExposures({
      assetId: assetId
    })
    
    if (res.code === 0 && selectedItem.value) {
      selectedItem.value.dirScanResults = (res.dirScanResults || []).map(item => ({
        url: item.url,
        path: item.path,
        status: String(item.status || ''),
        contentLength: item.contentLength,
        responseTime: 0,
        title: item.title || ''
      }))
      
      selectedItem.value.vulnScanResults = (res.vulnResults || []).map(item => ({
        id: item.id,
        name: item.name,
        severity: item.severity,
        description: item.description || '',
        cvss: item.cvss || 0,
        cve: item.cve || '',
        matchedUrl: item.matchedUrl || item.url,
        discoveredAt: item.discoveredAt || ''
      }))
    }
  } catch (error) {
    console.error('加载暴露面数据失败:', error)
  }
}

// 格式化日期时间
const formatDateTime = (dateStr) => {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

// 翻译字段名称
const translateFieldName = (field) => {
  const fieldMap = {
    'title': t('asset.field.title'),
    'service': t('asset.field.service'),
    'httpStatus': t('asset.field.httpStatus'),
    'app': t('asset.field.app'),
    'iconHash': t('asset.field.iconHash'),
    'server': t('asset.field.server'),
    'banner': t('asset.field.banner')
  }
  return fieldMap[field] || field
}

// 计算暴露面数量
const getExposuresCount = () => {
  if (!selectedItem.value) return 0
  const dirCount = selectedItem.value.dirScanResults?.length || 0
  const vulnCount = selectedItem.value.vulnScanResults?.length || 0
  return 1 + dirCount + vulnCount
}

const openInNewTab = () => {
  if (selectedItem.value) {
    const url = `http://${selectedItem.value.name}:${selectedItem.value.port}`
    window.open(url, '_blank')
  }
}

const getStatusType = (status) => {
  const statusStr = String(status || '')
  if (statusStr.startsWith('2')) return 'success'
  if (statusStr.startsWith('3')) return 'warning'
  if (statusStr.startsWith('4') || statusStr.startsWith('5')) return 'danger'
  return 'info'
}

// 加载过滤器选项
const loadFilterOptions = async () => {
  try {
    const res = await getAssetFilterOptions({
      hasScreenshot: true
    })
    
    if (res.code === 0) {
      filterOptions.value = {
        statusCodes: res.statusCodes || []
      }
    }
  } catch (error) {
    console.error('加载过滤器选项失败:', error)
  }
}

onMounted(() => {
  loadFilterOptions()
  loadData()
})
</script>

<style lang="scss" scoped>
.screenshots-tab {
  .toolbar {
    display: flex;
    gap: 12px;
    margin-bottom: 16px;
    
    .search-input {
      flex: 1;
      max-width: 400px;
    }
  }
  
  .filters-panel {
    background: hsl(var(--card));
    border: 1px solid hsl(var(--border));
    border-radius: 8px;
    padding: 16px;
    margin-bottom: 16px;
    
    :deep(.el-select) {
      min-width: 200px;
    }
  }
  
  .screenshots-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 20px;
    margin-bottom: 24px;
  }
  
  .screenshot-card {
    background: hsl(var(--card));
    border: 1px solid hsl(var(--border));
    border-radius: 8px;
    overflow: hidden;
    cursor: pointer;
    transition: all 0.2s;
    
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
      }
      
      .name {
        font-weight: 500;
        color: hsl(var(--foreground));
      }
      
      .port {
        color: hsl(var(--primary));
        font-weight: 500;
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
  
  .pagination {
    margin-top: 16px;
  }
  
  // 资产详情抽屉样式
  .asset-detail {
    .detail-header {
      display: grid;
      grid-template-columns: 300px 1fr;
      gap: 24px;
      margin-bottom: 24px;
      padding-bottom: 24px;
      border-bottom: 1px solid hsl(var(--border));
      
      .detail-screenshot {
        width: 100%;
        aspect-ratio: 16 / 10;
        border-radius: 8px;
        overflow: hidden;
        background: hsl(var(--muted) / 0.3);
        cursor: pointer;
        
        .detail-screenshot-img {
          width: 100%;
          height: 100%;
          object-fit: cover;
        }
        
        .detail-screenshot-placeholder {
          width: 100%;
          height: 100%;
          display: flex;
          align-items: center;
          justify-content: center;
          color: hsl(var(--muted-foreground));
          font-style: italic;
        }
      }
      
      .detail-basic-info {
        display: flex;
        flex-direction: column;
        gap: 12px;
        
        .info-row {
          display: flex;
          align-items: center;
          gap: 12px;
          
          .info-label {
            font-weight: 500;
            color: hsl(var(--muted-foreground));
            min-width: 60px;
          }
          
          .info-value {
            color: hsl(var(--foreground));
            word-break: break-all;
            
            &.link {
              color: hsl(var(--primary));
              text-decoration: none;
              
              &:hover {
                text-decoration: underline;
              }
            }
          }
        }
      }
    }
    
    .detail-tabs {
      :deep(.el-tabs__item) {
        .tab-badge {
          margin-left: 8px;
        }
      }
    }
    
    .tab-content {
      padding: 16px 0;
      
      .section {
        margin-bottom: 24px;
        
        &:last-child {
          margin-bottom: 0;
        }
        
        .section-title {
          font-size: 16px;
          font-weight: 600;
          color: hsl(var(--foreground));
          margin: 0 0 16px 0;
        }
        
        .info-grid {
          display: grid;
          grid-template-columns: repeat(2, 1fr);
          gap: 16px;
          
          .info-item {
            display: flex;
            gap: 12px;
            
            .item-label {
              font-weight: 500;
              color: hsl(var(--muted-foreground));
              min-width: 80px;
            }
            
            .item-value {
              color: hsl(var(--foreground));
              word-break: break-all;
            }
          }
        }
        
        .code-block {
          background: hsl(var(--muted) / 0.3);
          border: 1px solid hsl(var(--border));
          border-radius: 6px;
          padding: 16px;
          overflow-x: auto;
          
          &.small {
            padding: 12px;
          }
          
          pre {
            margin: 0;
            font-family: 'Courier New', monospace;
            font-size: 13px;
            line-height: 1.6;
            color: hsl(var(--foreground));
            white-space: pre-wrap;
            word-break: break-all;
          }
        }
      }
      
      .exposure-item {
        padding: 16px;
        background: hsl(var(--card));
        border: 1px solid hsl(var(--border));
        border-radius: 8px;
        
        .exposure-header {
          display: flex;
          align-items: center;
          gap: 12px;
          margin-bottom: 16px;
          
          .exposure-service {
            font-weight: 500;
            color: hsl(var(--foreground));
          }
        }
        
        .exposure-details {
          display: flex;
          flex-direction: column;
          gap: 12px;
          
          .detail-item {
            .detail-label {
              font-weight: 500;
              color: hsl(var(--muted-foreground));
              margin-right: 8px;
            }
            
            .detail-value {
              color: hsl(var(--foreground));
            }
          }
        }
      }
      
      .tech-list-detail {
        display: flex;
        flex-direction: column;
        gap: 12px;
        
        .tech-item-detail {
          display: flex;
          align-items: flex-start;
          gap: 16px;
          padding: 16px;
          background: hsl(var(--card));
          border: 1px solid hsl(var(--border));
          border-radius: 8px;
          transition: all 0.2s;
          
          &:hover {
            border-color: hsl(var(--primary) / 0.3);
            background: hsl(var(--muted) / 0.3);
          }
          
          .tech-icon {
            width: 40px;
            height: 40px;
            display: flex;
            align-items: center;
            justify-content: center;
            background: hsl(var(--primary) / 0.1);
            border-radius: 8px;
            flex-shrink: 0;
            
            .el-icon {
              font-size: 24px;
              color: hsl(var(--primary));
            }
          }
          
          .tech-info {
            flex: 1;
            
            .tech-name {
              font-size: 15px;
              font-weight: 500;
              color: hsl(var(--foreground));
              margin-bottom: 4px;
            }
            
            .tech-category {
              font-size: 13px;
              color: hsl(var(--muted-foreground));
            }
          }
        }
      }
      
      .changelog-list {
        display: flex;
        flex-direction: column;
        gap: 16px;
        
        .changelog-item {
          padding: 16px;
          background: hsl(var(--card));
          border: 1px solid hsl(var(--border));
          border-radius: 8px;
          
          .changelog-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 12px;
            
            .changelog-time {
              font-size: 13px;
              color: hsl(var(--muted-foreground));
            }
          }
          
          .changelog-changes {
            display: flex;
            flex-direction: column;
            gap: 8px;
            
            .change-item {
              display: flex;
              align-items: center;
              gap: 8px;
              font-size: 13px;
              
              .change-field {
                font-weight: 500;
                color: hsl(var(--foreground));
                min-width: 100px;
              }
              
              .change-old {
                color: hsl(var(--muted-foreground));
                text-decoration: line-through;
              }
              
              .change-arrow {
                color: hsl(var(--muted-foreground));
              }
              
              .change-new {
                color: hsl(var(--primary));
                font-weight: 500;
              }
            }
          }
        }
      }
      
      .empty-state {
        text-align: center;
        padding: 48px 0;
        color: hsl(var(--muted-foreground));
        font-style: italic;
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

