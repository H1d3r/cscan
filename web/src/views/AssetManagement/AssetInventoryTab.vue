<template>
  <div class="asset-inventory-tab">
    <!-- 鎼滅储鍜岃繃婊ゆ爮 -->
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
        <el-form-item :label="t('asset.assetInventoryTab.labels')">
          <el-select v-model="filters.labels" multiple :placeholder="t('asset.assetInventoryTab.selectLabel')" clearable filterable>
            <el-option
              v-for="label in filterOptions.labels"
              :key="label"
              :label="label"
              :value="label"
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

    <!-- 璧勪骇鍗＄墖鍒楄〃 -->
    <div v-loading="loading" class="assets-grid">
      <div 
        v-for="asset in assets" 
        :key="asset.id" 
        class="asset-card-wrapper"
      >
        <div 
          class="asset-card"
          @click="handleCardClick(asset)"
        >
          <!-- 宸︿晶锛氫富鏈轰俊锟?-->
          <div class="asset-left">
            <!-- 涓绘満鍚嶅拰绔彛 -->
            <div class="host-info">
              <span class="host-link" @click.stop>
                {{ asset.host }}<template v-if="asset.port && asset.port !== 0">:{{ asset.port }}</template>
              </span>
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
            
            <div class="tags-row">
              <el-tag v-if="asset.status && asset.status !== '0'" :type="getStatusType(asset.status)" size="small" class="status-tag">
                {{ asset.status }}
              </el-tag>
              
              <!-- AS缂栧彿 -->
              <el-tag v-if="asset.asn" size="small" effect="plain" class="info-tag">
                {{ asset.asn }}
              </el-tag>
              
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
            
            <!-- CNAME 淇℃伅 -->
            <div v-if="asset.cname" class="cname-info">
              <span class="label-text">CNAME:</span>
              <span class="cname-value">{{ asset.cname }}</span>
            </div>
          </div>
          
          <!-- 涓棿锛氭埅鍥惧拰鏍囬 -->
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
          
          <!-- 鍙充晶锛氭妧鏈爤 -->
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
          
          <!-- 鍙充笂瑙掞細鏃堕棿鍜屾搷锟?-->
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
    </div>

    <!-- 鍒嗛〉 -->
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
    
    <!-- 鎶€鏈爤璇︽儏瀵硅瘽锟?-->
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
    
    <!-- 娣诲姞鏍囩瀵硅瘽锟?-->
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
    
    <!-- 璧勪骇璇︽儏鎶藉眽 -->
    <AssetDetailDrawer
      v-model:visible="detailDrawerVisible"
      :asset="detailAsset"
      @preview-show="showPreview"
      @preview-hide="hidePreview"
    />
    
    <!-- 鍥剧墖棰勮娴眰 -->
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
import { debounce } from 'lodash-es'
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
import AssetDetailDrawer from '@/components/asset/AssetDetailDrawer.vue'

const { t } = useI18n()
const route = useRoute()

const loading = ref(false)
const searchQuery = ref('')
const showFilters = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const assets = ref([])
const filters = ref({
  technologies: [],
  ports: [],
  statusCodes: []
})

// 杩囨护鍣ㄩ€夐」锛堜粠鍚庣鍔ㄦ€佸姞杞斤級
const filterOptions = ref({
  technologies: [],
  ports: [],
  statusCodes: []
})

// 鎶€鏈爤瀵硅瘽
  const techDialogVisible = ref(false)
const techSearchQuery = ref('')
const currentAsset = ref(null)

// 鏍囩瀵硅瘽
  const labelDialogVisible = ref(false)
const newLabelInput = ref('')

// 璇︽儏鎶藉眽
const detailDrawerVisible = ref(false)
const detailAsset = ref(null)
const activeDetailTab = ref('overview')

// 鍥剧墖棰勮
const previewVisible = ref(false)
const previewImage = ref('')
const previewPosition = ref({ x: 0, y: 0 })
const previewSize = ref({ width: 400, height: 300 })

const showPreview = (asset, event) => {
  if (!asset.screenshot) return
  
  previewImage.value = formatScreenshotUrl(asset.screenshot)
  previewVisible.value = true
  
  // 璁＄畻棰勮浣嶇疆
  const rect = event.currentTarget.getBoundingClientRect()
  
  // 妫€鏌ユ槸鍚﹀湪鎶藉眽鎴栧璇濇涓紙閫氳繃妫€鏌ョ埗鍏冪礌绫诲悕
  const isInDrawer = event.currentTarget.closest('.el-drawer__body') !== null
  const isInDialog = event.currentTarget.closest('.el-dialog__body') !== null
  const isInDetailView = isInDrawer || isInDialog
  
  let previewWidth, previewHeight, padding
  
  if (isInDetailView) {
    // 鍦ㄨ鎯呰鍥句腑锛屼娇鐢ㄦ洿澶х殑棰勮灏哄
    previewWidth = Math.min(800, window.innerWidth * 0.5) // 鏈€锟?00px鎴栧睆骞曞搴︾殑50%
    previewHeight = Math.min(900, window.innerHeight * 0.8) // 鏈€锟?00px鎴栧睆骞曢珮搴︾殑80%
    padding = 30
  } else {
    // 鍦ㄥ垪琛ㄨ鍥句腑锛屼娇鐢ㄨ緝灏忕殑棰勮灏哄
    previewWidth = 400
    previewHeight = 300
    padding = 20
  }
  
  previewSize.value = { width: previewWidth, height: previewHeight }
  
  // 榛樿鏄剧ず鍦ㄥ彸
  let x = rect.right + padding
  let y = rect.top
  
  // 濡傛灉鍙充晶绌洪棿涓嶅锛屾樉绀哄湪宸︿晶
  if (x + previewWidth > window.innerWidth) {
    x = rect.left - previewWidth - padding
  }
  
  // 濡傛灉涓嬫柟绌洪棿涓嶅锛屽悜涓婅皟
  if (y + previewHeight > window.innerHeight) {
    y = window.innerHeight - previewHeight - padding
  }
  
  // 纭繚涓嶈秴鍑洪《
  if (y < padding) {
    y = padding
  }
  
  // 纭繚涓嶈秴鍑哄乏
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

// 妯℃嫙鏁版嵁锛堢敤浜庡紑鍙戞祴璇曪級
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
    asn: '', // 娌℃湁 ASN 鏁版嵁
    ip: '124.221.31.220',
    url: 'http://cscan.txf7.cn',
    screenshot: null,
    title: 'CSCAN - 瀹屾暣瀹夊叏涓夊悎涓€',
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
      // 浣跨敤妯℃嫙鏁版嵁
      assets.value = mockAssets
      total.value = mockAssets.length
    } else {
      // 璋冪敤鐪熷疄 API
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
        // 杞崲鍚庣鏁版嵁鏍煎紡涓哄墠绔牸
  assets.value = (res.list || []).map(item => ({
          id: item.id,
          workspaceId: item.workspaceId, // 淇濆瓨宸ヤ綔绌洪棿ID锛岀敤浜庡垹
  host: item.host,
          port: item.port,
          status: String(item.status || '200'),
          asn: item.asn || '', // 绌哄瓧绗︿覆锛屼笉鏄剧ず榛樿
  ip: item.ip || '',
          url: item.port && item.port !== 0 ? `${item.port === 443 ? 'https' : 'http'}://${item.host}:${item.port}` : `http://${item.host}`,
          screenshot: item.screenshot || '',
          title: item.title || item.host,
          cname: item.cname || '',
          technologies: item.technologies || [],
          labels: item.labels || [], // 鑷畾涔夋爣
  iconHash: item.iconHash || '',
          iconHashBytes: item.iconHashBytes || '',
          httpHeader: item.httpHeader || '',
          httpBody: item.httpBody || '',
          banner: item.banner || '',
          lastUpdated: item.lastUpdated || '鏈煡',
          firstSeen: item.firstSeen || '',
          lastUpdatedFull: item.lastUpdatedFull || ''
        }))
        total.value = res.total || 0
      } else {
        ElMessage.error(res.msg || t('asset.assetInventoryTab.loadFailed'))
      }
    }
  } catch (error) {
    console.error('鍔犺浇澶辫触:', error)
    ElMessage.error(t('asset.assetInventoryTab.loadFailed'))
  } finally {
    loading.value = false
  }
}

const handleSearch = debounce(() => {
  currentPage.value = 1
  loadData()
}, 300)

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

// 鑾峰彇婕忔礊涓ラ噸绋嬪害鐨勬爣绛剧被
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
  
  // 妫€鏌ユ槸鍚﹀凡瀛樺湪
  if (currentAsset.value.labels.includes(newLabelInput.value.trim())) {
    ElMessage.warning(t('asset.assetInventoryTab.labelExists'))
    return
  }
  
  currentAsset.value.labels.push(newLabelInput.value.trim())
  const newLabel = newLabelInput.value.trim()
  newLabelInput.value = ''
  
  // 璋冪敤 API 淇濆瓨鏍囩
  try {
    const res = await updateAssetLabels({
      id: currentAsset.value.id,
      labels: currentAsset.value.labels,
      workspaceId: currentAsset.value.workspaceId // 明确传递资产所属的工作空间ID
    })
    
    if (res.code === 0) {
      ElMessage.success(t('asset.assetInventoryTab.labelAddSuccess'))
    } else {
      // 澶辫触鏃跺洖
      const index = currentAsset.value.labels.indexOf(newLabel)
      if (index > -1) {
        currentAsset.value.labels.splice(index, 1)
      }
      ElMessage.error(res.msg || t('asset.assetInventoryTab.labelAddFailed'))
    }
  } catch (error) {
    // 澶辫触鏃跺洖
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
    
    // 璋冪敤 API 淇濆瓨鏍囩
    try {
      const res = await updateAssetLabels({
        id: asset.id,
        labels: asset.labels,
        workspaceId: asset.workspaceId // 明确传递资产所属的工作空间ID
      })
      
      if (res.code === 0) {
        ElMessage.success(t('asset.assetInventoryTab.labelDeleteSuccess'))
      } else {
        // 澶辫触鏃跺洖
        asset.labels.splice(index, 0, removedLabel)
        ElMessage.error(res.msg || t('asset.assetInventoryTab.labelDeleteFailed'))
      }
    } catch (error) {
      // 澶辫触鏃跺洖
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
      t('asset.assetInventoryTab.confirmDelete', { name: `${asset.host}${asset.port && asset.port !== 0 ? ':' + asset.port : ''}` }),
      t('common.warning'),
      {
        confirmButtonText: t('common.confirm'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )
    
    // 璋冪敤鍒犻櫎 API锛屼紶閫掕祫浜D鍜屽伐浣滅┖闂碔D
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
    // 鐢ㄦ埛鍙栨秷鎴栧垹闄ゅけ
  if (error !== 'cancel') {
      console.error('鍒犻櫎澶辫触:', error)
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
  
  // 绔嬪嵆鍔犺浇鎵€鏈夋暟鎹紙涓庢埅鍥炬竻鍗曚繚鎸佷竴鑷达級
  // 异步加载额外数据，忽略错误
  if (asset.id) {
    // 静默加载，不阻塞UI
    loadAssetHistory(asset.id).catch(() => {})
    loadAssetExposures(asset.id).catch(() => {})
  }
}

// 鍔犺浇璧勪骇鍙樻洿璁板綍
const loadAssetHistory = async (assetId) => {
  try {
    const res = await getAssetHistory({
      assetId: assetId,
      limit: 50
    })
    
    if (res.code === 0 && res.list) {
      // 杞崲鏁版嵁鏍煎紡
      detailAsset.value.changelogs = res.list.map(item => ({
        time: formatDateTime(item.createTime),
        taskId: item.taskId,
        changes: item.changes || []
      }))
    } else {
      // API返回非0代码，静默处理
      if (detailAsset.value) {
        detailAsset.value.changelogs = []
      }
    }
  } catch (error) {
    console.debug('加载变更记录失败:', error.message)
    if (detailAsset.value) {
      detailAsset.value.changelogs = []
    }
  }
}

// 鍔犺浇璧勪骇鏆撮湶闈㈡暟
  const loadAssetExposures = async (assetId) => {
  try {
    const res = await getAssetExposures({
      assetId: assetId
    })
    
    if (res.code === 0) {
      // 鏇存柊鐩綍鎵弿缁撴灉
      detailAsset.value.dirScanResults = (res.dirScanResults || []).map(item => ({
        url: item.url,
        path: item.path,
        status: String(item.status || ''),
        contentLength: item.contentLength,
        responseTime: 0, // 鍚庣鏆傛湭杩斿洖鍝嶅簲鏃堕棿
        title: item.title || ''
      }))
      
      // 鏇存柊婕忔礊鎵弿缁撴灉
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

    } else {
      // API返回非0代码，静默处理
      if (detailAsset.value) {
        detailAsset.value.dirScanResults = []
        detailAsset.value.vulnScanResults = []
      }
    }
  } catch (error) {
    console.debug('加载暴露面数据失败:', error.message)
    if (detailAsset.value) {
      detailAsset.value.dirScanResults = []
      detailAsset.value.vulnScanResults = []
    }
  }
}

// 鏍煎紡鍖栨棩鏈熸椂
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

// 缈昏瘧瀛楁鍚嶇О
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

// 璁＄畻鏆撮湶闈㈡暟閲忥紙绔彛鏈嶅姟 + 鐩綍鎵弿 + 婕忔礊鎵弿
  const getExposuresCount = (asset) => {
  if (!asset) return 0
  let count = 1 // 鑷冲皯鏈変竴涓鍙ｆ湇
  count += (asset.dirScanResults?.length || 0)
  count += (asset.vulnScanResults?.length || 0)
  return count
}

// 鍔犺浇杩囨护鍣ㄩ€夐」
const loadFilterOptions = async () => {
  try {
    const res = await getAssetFilterOptions({
      domain: route.query.domain || ''
    })
    
    if (res.code === 0) {
      filterOptions.value = {
        technologies: res.technologies || [],
        ports: res.ports || [],
        statusCodes: res.statusCodes || [],
        labels: res.labels || []
      }
    }
  } catch (error) {
    console.error('鍔犺浇杩囨护鍣ㄩ€夐」澶辫触:', error)
  }
}

// 鐩戝惉璺敱鍙傛暟鍙樺寲
watch(() => route.query.domain, (newDomain) => {
  if (newDomain) {
    searchQuery.value = newDomain
    handleSearch()
  }
}, { immediate: true })

onMounted(() => {
  // 鍔犺浇杩囨护鍣ㄩ€夐」
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
  
  .asset-card-wrapper {
    display: flex;
    flex-direction: column;
    gap: 0;
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
    border-radius: 8px 8px 0 0;
    transition: all 0.2s;
    align-items: start;
    cursor: pointer;
    
    &:hover {
      border-color: hsl(var(--primary) / 0.5);
      box-shadow: 0 2px 8px hsl(var(--primary) / 0.1);
    }
    
    .asset-card-wrapper:not(:has(.dir-scan-details)) & {
      border-radius: 8px;
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
  
  // 璧勪骇璇︽儏鎶藉眽鏍峰紡
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
      
      // 鐩綍鎵弿缁撴灉鏍峰紡
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
      
      // 婕忔礊鎵弿缁撴灉鏍峰紡
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

// 鍥剧墖棰勮鏍峰紡
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

// 棰勮鍔ㄧ敾
.preview-fade-enter-active,
.preview-fade-leave-active {
  transition: opacity 0.2s ease;
}

.preview-fade-enter-from,
.preview-fade-leave-to {
  opacity: 0;
}
</style>
    if (detailAsset.value) {
      detailAsset.value.dirScanResults = []
      detailAsset.value.vulnScanResults = []
    }
