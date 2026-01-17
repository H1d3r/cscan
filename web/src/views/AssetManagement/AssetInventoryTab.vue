<template>
  <div class="asset-inventory-tab">
    <!-- 搜索和过滤栏 -->
    <div class="toolbar">
      <el-input
        v-model="searchQuery"
        :placeholder="t('asset.assetInventoryTab.searchPlaceholder')"
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
        {{ t('asset.assetInventoryTab.filters') }}
      </el-button>
      <el-button @click="refreshData">
        <el-icon><Refresh /></el-icon>
        {{ t('asset.assetInventoryTab.refresh') }}
      </el-button>
    </div>

    <!-- 高级过滤器 -->
    <div v-if="showFilters" class="filters-panel">
      <el-form :inline="true">
        <el-form-item :label="t('asset.assetInventoryTab.technologies')">
          <el-select v-model="filters.technologies" multiple :placeholder="t('asset.assetInventoryTab.selectTech')" clearable filterable>
            <el-option
              v-for="tech in filterOptions.technologies"
              :key="tech"
              :label="tech"
              :value="tech"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('asset.assetInventoryTab.ports')">
          <el-select v-model="filters.ports" multiple :placeholder="t('asset.assetInventoryTab.selectPort')" clearable filterable>
            <el-option
              v-for="port in filterOptions.ports"
              :key="port"
              :label="String(port)"
              :value="port"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('asset.assetInventoryTab.statusCodes')">
          <el-select v-model="filters.statusCodes" multiple :placeholder="t('asset.assetInventoryTab.selectStatus')" clearable filterable>
            <el-option
              v-for="code in filterOptions.statusCodes"
              :key="code"
              :label="code"
              :value="code"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="applyFilters">{{ t('asset.assetInventoryTab.apply') }}</el-button>
          <el-button @click="resetFilters">{{ t('asset.assetInventoryTab.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- 资产卡片列表 -->
    <div v-loading="loading" class="assets-grid">
      <div 
        v-for="asset in assets" 
        :key="asset.id" 
        class="asset-card"
        @click="handleCardClick(asset)"
      >
        <!-- 左侧：主机信息 -->
        <div class="asset-left">
          <!-- 主机名和端口 -->
          <div class="host-info">
            <a :href="asset.url" target="_blank" class="host-link">
              {{ asset.host }}:{{ asset.port }}
            </a>
            <div v-if="asset.ip" class="host-ip">{{ asset.ip }}</div>
            <div v-if="asset.iconHash" class="host-icon-info">
              <img 
                v-if="asset.iconHashBytes"
                :src="'data:image/x-icon;base64,' + asset.iconHashBytes"
                class="favicon"
                @error="(e) => e.target.style.display = 'none'"
              />
              <span class="icon-hash">{{ asset.iconHash }}</span>
            </div>
          </div>
          
          <!-- 标签行 -->
          <div class="tags-row">
            <!-- 状态码 -->
            <el-tag :type="getStatusType(asset.status)" size="small" class="status-tag">
              {{ asset.status }}
            </el-tag>
            
            <!-- AS编号 -->
            <el-tag v-if="asset.asn" size="small" effect="plain" class="info-tag">
              {{ asset.asn }}
            </el-tag>
            
            <!-- 自定义标签 -->
            <el-tag
              v-for="(label, index) in (asset.labels || [])"
              :key="index"
              size="small"
              closable
              class="custom-label"
              @close.stop="handleRemoveLabel(asset, index)"
            >
              {{ label }}
            </el-tag>
            
            <el-button 
              text 
              size="small" 
              class="add-label-btn"
              @click.stop="handleAddLabel(asset)"
            >
              <el-icon><Plus /></el-icon>
              {{ t('asset.assetInventoryTab.addLabels') }}
            </el-button>
          </div>
          
          <!-- CNAME 信息 -->
          <div v-if="asset.cname" class="cname-info">
            <span class="label-text">CNAME:</span>
            <span class="cname-value">{{ asset.cname }}</span>
          </div>
        </div>
        
        <!-- 中间：截图和标题 -->
        <div class="asset-center">
          <div 
            v-if="asset.screenshot" 
            class="screenshot-wrapper"
            @mouseenter="showPreview(asset, $event)"
            @mouseleave="hidePreview"
          >
            <img 
              :src="formatScreenshotUrl(asset.screenshot)"
              :alt="asset.title"
              class="screenshot-img"
              loading="lazy"
              @error="handleScreenshotError"
            />
          </div>
          <div v-else class="screenshot-placeholder-text">
            {{ t('asset.noScreenshot') }}
          </div>
          <div class="title-text">{{ asset.title || '-' }}</div>
        </div>
        
        <!-- 右侧：技术栈 -->
        <div class="asset-right">
          <div v-if="asset.technologies && asset.technologies.length > 0" class="tech-list">
            <el-tag
              v-for="(tech, index) in asset.technologies.slice(0, 5)"
              :key="index"
              size="small"
              class="tech-tag"
            >
              {{ tech }}
            </el-tag>
            <el-button
              v-if="asset.technologies.length > 5"
              text
              size="small"
              class="more-btn"
              @click.stop="showAllTechnologies(asset)"
            >
              +{{ asset.technologies.length - 5 }} {{ t('common.more') }}
            </el-button>
          </div>
          <div v-else class="no-tech">
            {{ t('asset.assetInventoryTab.noTechnologies') }}
          </div>
        </div>
        
        <!-- 右上角：时间和操作 -->
        <div class="asset-meta">
          <el-tooltip placement="left" effect="dark">
            <template #content>
              <div class="time-tooltip">
                <div class="tooltip-row">
                  <span class="tooltip-label">{{ t('asset.firstSeen') }}</span>
                  <span class="tooltip-value">{{ asset.firstSeen }}</span>
                </div>
                <div class="tooltip-row">
                  <span class="tooltip-label">{{ t('asset.lastUpdated') }}</span>
                  <span class="tooltip-value">{{ asset.lastUpdatedFull }}</span>
                </div>
              </div>
            </template>
            <span class="time-text">{{ asset.lastUpdated }}</span>
          </el-tooltip>
          <el-icon class="delete-icon" @click.stop="handleDelete(asset)">
            <Delete />
          </el-icon>
        </div>
      </div>
    </div>

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
    
    <!-- 技术栈详情对话框 -->
    <el-dialog
      v-model="techDialogVisible"
      :title="t('asset.assetInventoryTab.allTechnologies')"
      width="600px"
    >
      <div class="tech-dialog-content">
        <el-input
          v-model="techSearchQuery"
          :placeholder="t('asset.assetInventoryTab.searchTech')"
          clearable
          class="tech-search"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
        <div class="tech-tags-wrapper">
          <el-tag
            v-for="(tech, index) in filteredTechnologies"
            :key="index"
            size="small"
            class="tech-tag-large"
          >
            {{ tech }}
          </el-tag>
        </div>
      </div>
    </el-dialog>
    
    <!-- 添加标签对话框 -->
    <el-dialog
      v-model="labelDialogVisible"
      :title="t('asset.assetInventoryTab.addLabelsTitle')"
      width="500px"
    >
      <div class="label-dialog-content">
        <el-input
          v-model="newLabelInput"
          :placeholder="t('asset.assetInventoryTab.enterLabel')"
          @keyup.enter="handleAddNewLabel"
        >
          <template #append>
            <el-button @click="handleAddNewLabel">{{ t('asset.assetInventoryTab.add') }}</el-button>
          </template>
        </el-input>
        <div v-if="currentAsset && currentAsset.labels && currentAsset.labels.length > 0" class="current-labels">
          <div class="label-section-title">{{ t('asset.assetInventoryTab.currentLabels') }}</div>
          <el-tag
            v-for="(label, index) in currentAsset.labels"
            :key="index"
            size="small"
            closable
            class="label-item"
            @close="handleRemoveLabel(currentAsset, index)"
          >
            {{ label }}
          </el-tag>
        </div>
      </div>
    </el-dialog>
    
    <!-- 资产详情抽屉 -->
    <el-drawer
      v-model="detailDrawerVisible"
      :title="detailAsset?.host + ':' + detailAsset?.port"
      size="60%"
      direction="rtl"
    >
      <div v-if="detailAsset" class="asset-detail">
        <!-- 顶部截图和基本信息 -->
        <div class="detail-header">
          <div 
            class="detail-screenshot"
            @mouseenter="showPreview(detailAsset, $event)"
            @mouseleave="hidePreview"
          >
            <img 
              v-if="detailAsset.screenshot"
              :src="formatScreenshotUrl(detailAsset.screenshot)"
              :alt="detailAsset.title"
              class="detail-screenshot-img"
            />
            <div v-else class="detail-screenshot-placeholder">
              {{ t('asset.noScreenshot') }}
            </div>
          </div>
          <div class="detail-basic-info">
            <div class="info-row">
              <span class="info-label">URL:</span>
              <a :href="detailAsset.url" target="_blank" class="info-value link">
                {{ detailAsset.url }}
              </a>
            </div>
            <div class="info-row">
              <span class="info-label">{{ t('asset.ip') }}:</span>
              <span class="info-value">{{ detailAsset.ip || '-' }}</span>
            </div>
            <div class="info-row">
              <span class="info-label">{{ t('asset.statusCode') }}:</span>
              <el-tag :type="getStatusType(detailAsset.status)" size="small">
                {{ detailAsset.status }}
              </el-tag>
            </div>
            <div v-if="detailAsset.asn" class="info-row">
              <span class="info-label">ASN:</span>
              <span class="info-value">{{ detailAsset.asn }}</span>
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
                    <span class="item-value">{{ detailAsset.host }}</span>
                  </div>
                  <div class="info-item">
                    <span class="item-label">{{ t('asset.assetDetail.port') }}:</span>
                    <span class="item-value">{{ detailAsset.port }}</span>
                  </div>
                  <div class="info-item">
                    <span class="item-label">{{ t('asset.assetDetail.service') }}:</span>
                    <span class="item-value">{{ detailAsset.service || '-' }}</span>
                  </div>
                  <div v-if="detailAsset.cname" class="info-item">
                    <span class="item-label">{{ t('asset.assetDetail.cname') }}:</span>
                    <span class="item-value">{{ detailAsset.cname }}</span>
                  </div>
                  <div v-if="detailAsset.iconHash" class="info-item">
                    <span class="item-label">{{ t('asset.assetDetail.iconHash') }}:</span>
                    <div class="icon-hash-display">
                      <img 
                        v-if="detailAsset.iconHashBytes"
                        :src="'data:image/x-icon;base64,' + detailAsset.iconHashBytes"
                        class="favicon-large"
                        @error="(e) => e.target.style.display = 'none'"
                      />
                      <span class="item-value">{{ detailAsset.iconHash }}</span>
                    </div>
                  </div>
                </div>
              </div>
              
              <div class="section">
                <h4 class="section-title">{{ t('asset.assetDetail.httpResponse') }}</h4>
                <div class="code-block">
                  <pre>{{ detailAsset.httpHeader || t('asset.assetDetail.noHttpData') }}</pre>
                </div>
              </div>
              
              <div v-if="detailAsset.httpBody" class="section">
                <h4 class="section-title">{{ t('asset.assetDetail.httpBody') }}</h4>
                <div class="code-block">
                  <pre>{{ detailAsset.httpBody.substring(0, 1000) }}{{ detailAsset.httpBody.length > 1000 ? '...' : '' }}</pre>
                </div>
              </div>
            </div>
          </el-tab-pane>
          
          <!-- Exposures 标签页 (暴露面：目录扫描、漏洞扫描) -->
          <el-tab-pane name="exposures">
            <template #label>
              <span>{{ t('asset.assetDetail.exposures') }} <el-badge :value="getExposuresCount(detailAsset)" class="tab-badge" /></span>
            </template>
            <div class="tab-content">
              <!-- 端口服务 -->
              <div class="section">
                <h4 class="section-title">{{ t('asset.assetDetail.portServices') }}</h4>
                <div class="exposure-item">
                  <div class="exposure-header">
                    <el-tag size="small">{{ detailAsset.port }}</el-tag>
                    <span class="exposure-service">{{ detailAsset.service || t('asset.assetDetail.unknown') }}</span>
                  </div>
                  <div class="exposure-details">
                    <div class="detail-item">
                      <span class="detail-label">{{ t('asset.assetDetail.protocol') }}:</span>
                      <span class="detail-value">{{ detailAsset.port === 443 ? 'HTTPS' : 'HTTP' }}</span>
                    </div>
                    <div v-if="detailAsset.banner" class="detail-item">
                      <span class="detail-label">{{ t('asset.assetDetail.banner') }}:</span>
                      <div class="code-block small">
                        <pre>{{ detailAsset.banner }}</pre>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              
              <!-- 目录扫描结果 -->
              <div class="section">
                <h4 class="section-title">
                  {{ t('asset.assetDetail.dirScanResults') }}
                  <el-badge :value="detailAsset.dirScanResults?.length || 0" class="count-badge" />
                </h4>
                <div v-if="detailAsset.dirScanResults && detailAsset.dirScanResults.length > 0" class="dir-scan-list">
                  <div v-for="(dir, index) in detailAsset.dirScanResults" :key="index" class="dir-scan-item">
                    <div class="dir-scan-header">
                      <a :href="dir.url" target="_blank" class="dir-url">{{ dir.path }}</a>
                      <el-tag :type="getStatusType(dir.status)" size="small">{{ dir.status }}</el-tag>
                    </div>
                    <div class="dir-scan-meta">
                      <span class="meta-item">
                        <el-icon><Document /></el-icon>
                        {{ dir.contentLength || 0 }} bytes
                      </span>
                      <span class="meta-item">
                        <el-icon><Clock /></el-icon>
                        {{ dir.responseTime || 0 }}ms
                      </span>
                      <span v-if="dir.title" class="meta-item">
                        <el-icon><Document /></el-icon>
                        {{ dir.title }}
                      </span>
                    </div>
                  </div>
                </div>
                <div v-else class="empty-state">
                  {{ t('asset.assetDetail.noDirScanResults') }}
                </div>
              </div>
              
              <!-- 漏洞扫描结果 -->
              <div class="section">
                <h4 class="section-title">
                  {{ t('asset.assetDetail.vulnScanResults') }}
                  <el-badge :value="detailAsset.vulnScanResults?.length || 0" class="count-badge" />
                </h4>
                <div v-if="detailAsset.vulnScanResults && detailAsset.vulnScanResults.length > 0" class="vuln-scan-list">
                  <div v-for="(vuln, index) in detailAsset.vulnScanResults" :key="index" class="vuln-scan-item">
                    <div class="vuln-scan-header">
                      <div class="vuln-title-row">
                        <el-tag :type="getVulnSeverityType(vuln.severity)" size="small" class="severity-tag">
                          {{ vuln.severity }}
                        </el-tag>
                        <span class="vuln-name">{{ vuln.name }}</span>
                      </div>
                      <span class="vuln-id">{{ vuln.id }}</span>
                    </div>
                    <div v-if="vuln.description" class="vuln-description">
                      {{ vuln.description }}
                    </div>
                    <div class="vuln-meta">
                      <span v-if="vuln.cvss" class="meta-item">
                        <el-icon><Warning /></el-icon>
                        CVSS: {{ vuln.cvss }}
                      </span>
                      <span v-if="vuln.cve" class="meta-item">
                        <el-icon><Document /></el-icon>
                        {{ vuln.cve }}
                      </span>
                      <span class="meta-item">
                        <el-icon><Clock /></el-icon>
                        {{ vuln.discoveredAt }}
                      </span>
                    </div>
                    <div v-if="vuln.matchedUrl" class="vuln-matched-url">
                      <span class="matched-label">{{ t('asset.assetDetail.matchedUrl') }}:</span>
                      <a :href="vuln.matchedUrl" target="_blank" class="matched-url">{{ vuln.matchedUrl }}</a>
                    </div>
                  </div>
                </div>
                <div v-else class="empty-state">
                  {{ t('asset.assetDetail.noVulnScanResults') }}
                </div>
              </div>
            </div>
          </el-tab-pane>
          
          <!-- Technologies 标签页 (Web指纹) -->
          <el-tab-pane name="technologies">
            <template #label>
              <span>{{ t('asset.assetDetail.technologies') }} <el-badge :value="detailAsset.technologies?.length || 0" class="tab-badge" /></span>
            </template>
            <div class="tab-content">
              <div v-if="detailAsset.technologies && detailAsset.technologies.length > 0" class="tech-list-detail">
                <div v-for="(tech, index) in detailAsset.technologies" :key="index" class="tech-item-detail">
                  <div class="tech-icon">
                    <el-icon><Box /></el-icon>
                  </div>
                  <div class="tech-info">
                    <div class="tech-name">{{ tech }}</div>
                    <div class="tech-category">{{ t('asset.assetDetail.techCategory') }}</div>
                  </div>
                </div>
              </div>
              <div v-else class="empty-state">
                {{ t('asset.assetDetail.noTechDetected') }}
              </div>
            </div>
          </el-tab-pane>
          
          <!-- Changelogs 标签页 (变更记录) -->
          <el-tab-pane name="changelogs">
            <template #label>
              <span>{{ t('asset.assetDetail.changelogs') }} <el-badge :value="detailAsset.changelogs?.length || 0" class="tab-badge" /></span>
            </template>
            <div class="tab-content">
              <div v-if="detailAsset.changelogs && detailAsset.changelogs.length > 0" class="changelog-list">
                <div v-for="(log, index) in detailAsset.changelogs" :key="index" class="changelog-item">
                  <div class="changelog-header">
                    <div class="changelog-time-info">
                      <el-icon class="time-icon"><Clock /></el-icon>
                      <span class="changelog-time">{{ log.time }}</span>
                    </div>
                    <el-tag size="small" type="info">{{ log.taskId }}</el-tag>
                  </div>
                  <div class="changelog-changes">
                    <div v-for="(change, idx) in log.changes" :key="idx" class="change-item">
                      <div class="change-field-name">
                        <el-icon class="field-icon"><Edit /></el-icon>
                        <span class="field-label">{{ translateFieldName(change.field) }}</span>
                      </div>
                      <div class="change-values">
                        <div class="change-value-box old-value">
                          <div class="value-label">{{ t('asset.assetDetail.oldValue') }}</div>
                          <div class="value-content">{{ change.oldValue || '-' }}</div>
                        </div>
                        <el-icon class="change-arrow"><Right /></el-icon>
                        <div class="change-value-box new-value">
                          <div class="value-label">{{ t('asset.assetDetail.newValue') }}</div>
                          <div class="value-content">{{ change.newValue || '-' }}</div>
                        </div>
                      </div>
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
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Search,
  Filter,
  Refresh,
  Picture,
  Plus,
  Delete,
  Right,
  Box,
  Document,
  Clock,
  Warning,
  Edit
} from '@element-plus/icons-vue'
import { getAssetInventory, updateAssetLabels, getAssetFilterOptions, deleteAsset, getAssetHistory, getAssetExposures } from '@/api/asset'
import { formatScreenshotUrl, handleScreenshotError } from '@/utils/screenshot'

const { t } = useI18n()
const route = useRoute()

const loading = ref(false)
const searchQuery = ref('')
const showFilters = ref(false)
const currentPage = ref(1)
const pageSize = ref(5)
const total = ref(0)
const assets = ref([])
const filters = ref({
  technologies: [],
  ports: [],
  statusCodes: []
})

// 过滤器选项（从后端动态加载）
const filterOptions = ref({
  technologies: [],
  ports: [],
  statusCodes: []
})

// 技术栈对话框
const techDialogVisible = ref(false)
const techSearchQuery = ref('')
const currentAsset = ref(null)

// 标签对话框
const labelDialogVisible = ref(false)
const newLabelInput = ref('')

// 详情抽屉
const detailDrawerVisible = ref(false)
const detailAsset = ref(null)
const activeDetailTab = ref('overview')

// 图片预览
const previewVisible = ref(false)
const previewImage = ref('')
const previewPosition = ref({ x: 0, y: 0 })
const previewSize = ref({ width: 400, height: 300 })

const showPreview = (asset, event) => {
  if (!asset.screenshot) return
  
  previewImage.value = formatScreenshotUrl(asset.screenshot)
  previewVisible.value = true
  
  // 计算预览位置
  const rect = event.currentTarget.getBoundingClientRect()
  
  // 检查是否在抽屉或对话框中（通过检查父元素类名）
  const isInDrawer = event.currentTarget.closest('.el-drawer__body') !== null
  const isInDialog = event.currentTarget.closest('.el-dialog__body') !== null
  const isInDetailView = isInDrawer || isInDialog
  
  let previewWidth, previewHeight, padding
  
  if (isInDetailView) {
    // 在详情视图中，使用更大的预览尺寸
    previewWidth = Math.min(800, window.innerWidth * 0.5) // 最大800px或屏幕宽度的50%
    previewHeight = Math.min(900, window.innerHeight * 0.8) // 最大900px或屏幕高度的80%
    padding = 30
  } else {
    // 在列表视图中，使用较小的预览尺寸
    previewWidth = 400
    previewHeight = 300
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

const filteredTechnologies = computed(() => {
  if (!currentAsset.value || !currentAsset.value.technologies) return []
  
  const query = techSearchQuery.value.toLowerCase()
  if (!query) return currentAsset.value.technologies
  
  return currentAsset.value.technologies.filter(tech => 
    tech.toLowerCase().includes(query)
  )
})

// 模拟数据（用于开发测试）
const useMockData = false

const mockAssets = [
  {
    id: '1',
    workspaceId: 'default',
    host: 'business.leapmotor.com',
    port: 443,
    status: '200',
    asn: 'AS4808',
    ip: '47.246.23.179',
    url: 'https://business.leapmotor.com',
    screenshot: '/9j/4AAQSkZJRgABAQAAAQABAAD...',
    title: 'business.leapmotor.com',
    cname: 'business.leapmotor.com.w.cdngslb.com',
    technologies: ['Vue.js 3.6.2', 'Axios', 'Day.js', 'Webpack', 'core-js 3.16.2', 'jQuery UI 1.10.1'],
    lastUpdated: '9 months ago',
    firstSeen: 'Apr 28, 2025, 07:41 UTC',
    lastUpdatedFull: 'May 1, 2025, 12:09 UTC'
  },
  {
    id: '2',
    workspaceId: 'default',
    host: 'cscan.txf7.cn',
    port: 80,
    status: '200',
    asn: '', // 没有 ASN 数据
    ip: '124.221.31.220',
    url: 'http://cscan.txf7.cn',
    screenshot: null,
    title: 'CSCAN - 完整安全三合一',
    technologies: ['Nginx 1.18.0'],
    lastUpdated: '1 day ago',
    firstSeen: 'Jan 15, 2026, 10:30 UTC',
    lastUpdatedFull: 'Jan 16, 2026, 14:22 UTC'
  }
]

const loadData = async () => {
  loading.value = true
  try {
    if (useMockData) {
      // 使用模拟数据
      assets.value = mockAssets
      total.value = mockAssets.length
    } else {
      // 调用真实 API
      const params = {
        page: currentPage.value,
        pageSize: pageSize.value,
        query: searchQuery.value,
        domain: route.query.domain || '',
        technologies: filters.value.technologies,
        ports: filters.value.ports,
        statusCodes: filters.value.statusCodes,
        timeRange: 'all',
        sortBy: 'time'
      }
      
      const res = await getAssetInventory(params)
      
      if (res.code === 0) {
        // 转换后端数据格式为前端格式
        assets.value = (res.list || []).map(item => ({
          id: item.id,
          workspaceId: item.workspaceId, // 保存工作空间ID，用于删除
          host: item.host,
          port: item.port,
          status: String(item.status || '200'),
          asn: item.asn || '', // 空字符串，不显示默认值
          ip: item.ip || '',
          url: `${item.port === 443 ? 'https' : 'http'}://${item.host}:${item.port}`,
          screenshot: item.screenshot || '',
          title: item.title || item.host,
          cname: item.cname || '',
          technologies: item.technologies || [],
          labels: item.labels || [], // 自定义标签
          iconHash: item.iconHash || '',
          iconHashBytes: item.iconHashBytes || '',
          httpHeader: item.httpHeader || '',
          httpBody: item.httpBody || '',
          banner: item.banner || '',
          lastUpdated: item.lastUpdated || '未知',
          firstSeen: item.firstSeen || '',
          lastUpdatedFull: item.lastUpdatedFull || ''
        }))
        total.value = res.total || 0
      } else {
        ElMessage.error(res.msg || t('asset.assetInventoryTab.loadFailed'))
      }
    }
  } catch (error) {
    console.error('加载失败:', error)
    ElMessage.error(t('asset.assetInventoryTab.loadFailed'))
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
  ElMessage.success(t('asset.assetInventoryTab.refreshSuccess'))
}

const applyFilters = () => {
  currentPage.value = 1
  loadData()
}

const resetFilters = () => {
  filters.value = {
    technologies: [],
    ports: [],
    statusCodes: []
  }
  currentPage.value = 1
  loadData()
}

const getStatusType = (status) => {
  const statusStr = String(status || '')
  if (statusStr.startsWith('2')) return 'success'
  if (statusStr.startsWith('3')) return 'warning'
  if (statusStr.startsWith('4') || statusStr.startsWith('5')) return 'danger'
  return 'info'
}

// 获取漏洞严重程度的标签类型
const getVulnSeverityType = (severity) => {
  const severityLower = severity?.toLowerCase()
  if (severityLower === 'critical') return 'danger'
  if (severityLower === 'high') return 'danger'
  if (severityLower === 'medium') return 'warning'
  if (severityLower === 'low') return 'info'
  return 'info'
}

const handleAddLabel = (asset) => {
  currentAsset.value = asset
  newLabelInput.value = ''
  labelDialogVisible.value = true
}

const handleAddNewLabel = async () => {
  if (!newLabelInput.value.trim()) {
    ElMessage.warning(t('asset.assetInventoryTab.enterLabelName'))
    return
  }
  
  if (!currentAsset.value.labels) {
    currentAsset.value.labels = []
  }
  
  // 检查是否已存在
  if (currentAsset.value.labels.includes(newLabelInput.value.trim())) {
    ElMessage.warning(t('asset.assetInventoryTab.labelExists'))
    return
  }
  
  currentAsset.value.labels.push(newLabelInput.value.trim())
  const newLabel = newLabelInput.value.trim()
  newLabelInput.value = ''
  
  // 调用 API 保存标签
  try {
    const res = await updateAssetLabels({
      id: currentAsset.value.id,
      labels: currentAsset.value.labels
    })
    
    if (res.code === 0) {
      ElMessage.success(t('asset.assetInventoryTab.labelAddSuccess'))
    } else {
      // 失败时回滚
      const index = currentAsset.value.labels.indexOf(newLabel)
      if (index > -1) {
        currentAsset.value.labels.splice(index, 1)
      }
      ElMessage.error(res.msg || t('asset.assetInventoryTab.labelAddFailed'))
    }
  } catch (error) {
    // 失败时回滚
    const index = currentAsset.value.labels.indexOf(newLabel)
    if (index > -1) {
      currentAsset.value.labels.splice(index, 1)
    }
    ElMessage.error(t('asset.assetInventoryTab.labelAddFailed'))
  }
}

const handleRemoveLabel = async (asset, index) => {
  if (asset.labels && asset.labels.length > index) {
    const removedLabel = asset.labels[index]
    asset.labels.splice(index, 1)
    
    // 调用 API 保存标签
    try {
      const res = await updateAssetLabels({
        id: asset.id,
        labels: asset.labels
      })
      
      if (res.code === 0) {
        ElMessage.success(t('asset.assetInventoryTab.labelDeleteSuccess'))
      } else {
        // 失败时回滚
        asset.labels.splice(index, 0, removedLabel)
        ElMessage.error(res.msg || t('asset.assetInventoryTab.labelDeleteFailed'))
      }
    } catch (error) {
      // 失败时回滚
      asset.labels.splice(index, 0, removedLabel)
      ElMessage.error(t('asset.assetInventoryTab.labelDeleteFailed'))
    }
  }
}

const showAllTechnologies = (asset) => {
  currentAsset.value = asset
  techSearchQuery.value = ''
  techDialogVisible.value = true
}

const handleDelete = async (asset) => {
  try {
    await ElMessageBox.confirm(
      t('asset.assetInventoryTab.confirmDelete', { name: `${asset.host}:${asset.port}` }),
      t('common.warning'),
      {
        confirmButtonText: t('common.confirm'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )
    
    // 调用删除 API，传递资产ID和工作空间ID
    const res = await deleteAsset({ 
      id: asset.id,
      workspaceId: asset.workspaceId
    })
    if (res.code === 0) {
      ElMessage.success(t('asset.assetInventoryTab.deleteSuccess'))
      loadData()
    } else {
      ElMessage.error(res.msg || t('asset.assetInventoryTab.deleteFailed'))
    }
  } catch (error) {
    // 用户取消或删除失败
    if (error !== 'cancel') {
      console.error('删除失败:', error)
      ElMessage.error(t('asset.assetInventoryTab.deleteFailed'))
    }
  }
}

const handleCardClick = async (asset) => {
  detailAsset.value = {
    ...asset,
    httpHeader: asset.httpHeader || '',
    httpBody: asset.httpBody || '',
    banner: asset.banner || '',
    changelogs: [],
    dirScanResults: [],
    vulnScanResults: []
  }
  activeDetailTab.value = 'overview'
  detailDrawerVisible.value = true
  
  // 异步加载变更记录
  loadAssetHistory(asset.id)
  
  // 异步加载暴露面数据（目录扫描和漏洞扫描结果）
  loadAssetExposures(asset.id)
}

// 加载资产变更记录
const loadAssetHistory = async (assetId) => {
  try {
    const res = await getAssetHistory({
      assetId: assetId,
      limit: 50
    })
    
    if (res.code === 0 && res.list) {
      // 转换数据格式
      detailAsset.value.changelogs = res.list.map(item => ({
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
    
    if (res.code === 0) {
      // 更新目录扫描结果
      detailAsset.value.dirScanResults = (res.dirScanResults || []).map(item => ({
        url: item.url,
        path: item.path,
        status: String(item.status || ''),
        contentLength: item.contentLength,
        responseTime: 0, // 后端暂未返回响应时间
        title: item.title || ''
      }))
      
      // 更新漏洞扫描结果
      detailAsset.value.vulnScanResults = (res.vulnResults || []).map(item => ({
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

// 计算暴露面数量（端口服务 + 目录扫描 + 漏洞扫描）
const getExposuresCount = (asset) => {
  if (!asset) return 0
  let count = 1 // 至少有一个端口服务
  count += (asset.dirScanResults?.length || 0)
  count += (asset.vulnScanResults?.length || 0)
  return count
}

// 加载过滤器选项
const loadFilterOptions = async () => {
  try {
    const res = await getAssetFilterOptions({
      domain: route.query.domain || ''
    })
    
    if (res.code === 0) {
      filterOptions.value = {
        technologies: res.technologies || [],
        ports: res.ports || [],
        statusCodes: res.statusCodes || []
      }
    }
  } catch (error) {
    console.error('加载过滤器选项失败:', error)
  }
}

// 监听路由参数变化
watch(() => route.query.domain, (newDomain) => {
  if (newDomain) {
    searchQuery.value = newDomain
    handleSearch()
  }
}, { immediate: true })

onMounted(() => {
  // 加载过滤器选项
  loadFilterOptions()
  
  // 检查初始 URL 参数
  if (route.query.domain) {
    searchQuery.value = route.query.domain
    handleSearch()
  } else {
    loadData()
  }
})
</script>

<style lang="scss" scoped>
.asset-inventory-tab {
  .toolbar {
    display: flex;
    gap: 12px;
    margin-bottom: 16px;
    
    .search-input {
      flex: 1;
      max-width: 500px;
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
  
  .assets-grid {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-bottom: 16px;
  }
  
  .asset-card {
    position: relative;
    display: grid;
    grid-template-columns: 2fr 1fr 1.5fr;
    gap: 24px;
    padding: 16px;
    padding-top: 40px;
    background: hsl(var(--card));
    border: 1px solid hsl(var(--border));
    border-radius: 8px;
    transition: all 0.2s;
    align-items: start;
    cursor: pointer;
    
    &:hover {
      border-color: hsl(var(--primary) / 0.5);
      box-shadow: 0 2px 8px hsl(var(--primary) / 0.1);
    }
  }
  
  .asset-left {
    display: flex;
    flex-direction: column;
    gap: 8px;
    
    .host-info {
      display: flex;
      flex-direction: column;
      gap: 4px;
      
      .host-link {
        font-size: 16px;
        font-weight: 500;
        color: hsl(var(--foreground));
        text-decoration: none;
        
        &:hover {
          color: hsl(var(--primary));
          text-decoration: underline;
        }
      }
      
      .host-ip {
        font-size: 13px;
        color: hsl(var(--muted-foreground));
        font-family: monospace;
      }
      
      .host-icon-info {
        display: flex;
        align-items: center;
        gap: 8px;
        margin-top: 4px;
        
        .favicon {
          width: 16px;
          height: 16px;
          object-fit: contain;
        }
        
        .icon-hash {
          font-size: 12px;
          color: hsl(var(--muted-foreground));
          font-family: monospace;
        }
      }
    }
    
    .tags-row {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
      
      .status-tag {
        font-weight: 500;
      }
      
      .info-tag {
        font-size: 12px;
      }
      
      .add-label-btn {
        font-size: 12px;
        padding: 0 8px;
        height: 24px;
      }
      
      .custom-label {
        font-size: 12px;
        background: hsl(var(--primary) / 0.1);
        border-color: hsl(var(--primary) / 0.3);
        color: hsl(var(--primary));
      }
    }
    
    .cname-info {
      font-size: 12px;
      color: hsl(var(--muted-foreground));
      
      .label-text {
        font-weight: 500;
        margin-right: 4px;
      }
      
      .cname-value {
        word-break: break-all;
      }
    }
  }
  
  .asset-center {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    
    .screenshot-wrapper {
      width: 100%;
      aspect-ratio: 16 / 10;
      border-radius: 6px;
      overflow: hidden;
      background: hsl(var(--muted) / 0.3);
      display: flex;
      align-items: center;
      justify-content: center;
      
      .screenshot-img {
        width: 100%;
        height: 100%;
        object-fit: cover;
      }
    }
    
    .screenshot-placeholder-text {
      width: 100%;
      text-align: center;
      font-size: 13px;
      color: hsl(var(--muted-foreground));
      font-style: italic;
      padding: 8px 0;
    }
    
    .title-text {
      font-size: 13px;
      color: hsl(var(--muted-foreground));
      text-align: center;
      width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }
  
  .asset-right {
    display: flex;
    flex-direction: column;
    justify-content: center;
    padding-right: 80px;
    
    .tech-list {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      align-items: flex-start;
      
      .tech-tag {
        font-size: 12px;
      }
      
      .more-btn {
        font-size: 12px;
        padding: 0 8px;
        height: 24px;
      }
    }
    
    .no-tech {
      font-size: 13px;
      color: hsl(var(--muted-foreground));
      font-style: italic;
    }
  }
  
  .asset-meta {
    position: absolute;
    top: 16px;
    right: 16px;
    display: flex;
    align-items: center;
    gap: 12px;
    
    .time-text {
      font-size: 12px;
      color: hsl(var(--muted-foreground));
      cursor: help;
      
      &:hover {
        color: hsl(var(--foreground));
      }
    }
    
    .bookmark-icon {
      font-size: 16px;
      color: hsl(var(--muted-foreground));
      cursor: pointer;
      
      &:hover {
        color: hsl(var(--primary));
      }
    }
    
    .delete-icon {
      font-size: 16px;
      color: hsl(var(--muted-foreground));
      cursor: pointer;
      
      &:hover {
        color: hsl(var(--danger));
      }
    }
  }
  
  .time-tooltip {
    .tooltip-row {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      padding: 4px 0;
      
      &:not(:last-child) {
        border-bottom: 1px solid rgba(255, 255, 255, 0.1);
      }
      
      .tooltip-label {
        font-weight: 500;
        color: rgba(255, 255, 255, 0.8);
      }
      
      .tooltip-value {
        color: rgba(255, 255, 255, 0.95);
      }
    }
  }
  
  .pagination {
    margin-top: 16px;
  }
  
  .tech-dialog-content {
    .tech-search {
      margin-bottom: 16px;
    }
    
    .tech-tags-wrapper {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      max-height: 400px;
      overflow-y: auto;
      
      .tech-tag-large {
        font-size: 13px;
      }
    }
  }
  
  .label-dialog-content {
    .current-labels {
      margin-top: 20px;
      
      .label-section-title {
        font-size: 14px;
        font-weight: 500;
        color: hsl(var(--foreground));
        margin-bottom: 12px;
      }
      
      .label-item {
        margin-right: 8px;
        margin-bottom: 8px;
        background: hsl(var(--primary) / 0.1);
        border-color: hsl(var(--primary) / 0.3);
        color: hsl(var(--primary));
      }
    }
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
          
          .icon-hash-display {
            display: flex;
            align-items: center;
            gap: 12px;
            
            .favicon-large {
              width: 32px;
              height: 32px;
              object-fit: contain;
              border: 1px solid hsl(var(--border));
              border-radius: 4px;
              padding: 4px;
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
      
      .tech-grid {
        display: flex;
        flex-wrap: wrap;
        gap: 12px;
        
        .tech-tag-detail {
          font-size: 14px;
          padding: 8px 16px;
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
          padding: 20px;
          background: hsl(var(--card));
          border: 1px solid hsl(var(--border));
          border-radius: 8px;
          transition: all 0.2s;
          
          &:hover {
            box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
          }
          
          .changelog-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 16px;
            padding-bottom: 12px;
            border-bottom: 1px solid hsl(var(--border));
            
            .changelog-time-info {
              display: flex;
              align-items: center;
              gap: 8px;
              
              .time-icon {
                color: hsl(var(--primary));
                font-size: 16px;
              }
              
              .changelog-time {
                font-size: 14px;
                font-weight: 500;
                color: hsl(var(--foreground));
              }
            }
          }
          
          .changelog-changes {
            display: flex;
            flex-direction: column;
            gap: 16px;
            
            .change-item {
              display: flex;
              flex-direction: column;
              gap: 12px;
              padding: 12px;
              background: hsl(var(--muted) / 0.3);
              border-radius: 6px;
              
              .change-field-name {
                display: flex;
                align-items: center;
                gap: 8px;
                
                .field-icon {
                  color: hsl(var(--primary));
                  font-size: 16px;
                }
                
                .field-label {
                  font-weight: 600;
                  color: hsl(var(--foreground));
                  font-size: 14px;
                }
              }
              
              .change-values {
                display: flex;
                align-items: center;
                gap: 16px;
                
                .change-value-box {
                  flex: 1;
                  padding: 12px;
                  border-radius: 6px;
                  border: 1px solid hsl(var(--border));
                  
                  .value-label {
                    font-size: 12px;
                    color: hsl(var(--muted-foreground));
                    margin-bottom: 6px;
                    font-weight: 500;
                  }
                  
                  .value-content {
                    font-size: 13px;
                    word-break: break-all;
                    line-height: 1.5;
                  }
                  
                  &.old-value {
                    background: hsl(var(--destructive) / 0.05);
                    
                    .value-content {
                      color: hsl(var(--muted-foreground));
                      text-decoration: line-through;
                    }
                  }
                  
                  &.new-value {
                    background: hsl(var(--primary) / 0.05);
                    
                    .value-content {
                      color: hsl(var(--primary));
                      font-weight: 500;
                    }
                  }
                }
                
                .change-arrow {
                  color: hsl(var(--muted-foreground));
                  font-size: 20px;
                  flex-shrink: 0;
                }
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
      
      // 目录扫描结果样式
      .dir-scan-list {
        display: flex;
        flex-direction: column;
        gap: 12px;
        
        .dir-scan-item {
          padding: 12px;
          background: hsl(var(--muted) / 0.3);
          border-radius: 6px;
          border: 1px solid hsl(var(--border));
          
          .dir-scan-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 8px;
            
            .dir-url {
              font-size: 14px;
              font-weight: 500;
              color: hsl(var(--primary));
              text-decoration: none;
              
              &:hover {
                text-decoration: underline;
              }
            }
          }
          
          .dir-scan-meta {
            display: flex;
            gap: 16px;
            font-size: 12px;
            color: hsl(var(--muted-foreground));
            
            .meta-item {
              display: flex;
              align-items: center;
              gap: 4px;
              
              .el-icon {
                font-size: 14px;
              }
            }
          }
        }
      }
      
      // 漏洞扫描结果样式
      .vuln-scan-list {
        display: flex;
        flex-direction: column;
        gap: 16px;
        
        .vuln-scan-item {
          padding: 16px;
          background: hsl(var(--muted) / 0.3);
          border-radius: 6px;
          border: 1px solid hsl(var(--border));
          
          .vuln-scan-header {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            margin-bottom: 12px;
            
            .vuln-title-row {
              display: flex;
              align-items: center;
              gap: 8px;
              flex: 1;
              
              .severity-tag {
                font-weight: 600;
              }
              
              .vuln-name {
                font-size: 15px;
                font-weight: 500;
                color: hsl(var(--foreground));
              }
            }
            
            .vuln-id {
              font-size: 12px;
              color: hsl(var(--muted-foreground));
              font-family: monospace;
            }
          }
          
          .vuln-description {
            font-size: 13px;
            color: hsl(var(--muted-foreground));
            line-height: 1.6;
            margin-bottom: 12px;
          }
          
          .vuln-meta {
            display: flex;
            gap: 16px;
            font-size: 12px;
            color: hsl(var(--muted-foreground));
            margin-bottom: 8px;
            
            .meta-item {
              display: flex;
              align-items: center;
              gap: 4px;
              
              .el-icon {
                font-size: 14px;
              }
            }
          }
          
          .vuln-matched-url {
            font-size: 12px;
            padding-top: 8px;
            border-top: 1px solid hsl(var(--border));
            
            .matched-label {
              color: hsl(var(--muted-foreground));
              margin-right: 8px;
            }
            
            .matched-url {
              color: hsl(var(--primary));
              text-decoration: none;
              word-break: break-all;
              
              &:hover {
                text-decoration: underline;
              }
            }
          }
        }
      }
      
      .count-badge {
        margin-left: 8px;
        
        :deep(.el-badge__content) {
          background-color: hsl(var(--primary));
        }
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

