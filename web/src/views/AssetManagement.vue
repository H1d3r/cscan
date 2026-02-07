<template>
  <div class="asset-management">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-content">
        <h1>{{ $t('asset.title') }}</h1>
        <p class="description">
          {{ $t('asset.pageDescription') }}
        </p>
      </div>
      <div class="header-actions">
        <el-dropdown @command="handleExportCommand">
          <el-button>
            <el-icon><Download /></el-icon>
            {{ $t('common.export') }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="csv">
                <el-icon><Document /></el-icon>
                {{ $t('asset.exportCsv') }}
              </el-dropdown-item>
              <el-dropdown-item command="json">
                <el-icon><Document /></el-icon>
                {{ $t('asset.exportJson') }}
              </el-dropdown-item>
              <el-dropdown-item command="excel">
                <el-icon><Document /></el-icon>
                {{ $t('asset.exportExcel') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button type="primary" @click="handleStartScan">
          <el-icon><Search /></el-icon>
          {{ $t('asset.startScan') }}
        </el-button>
      </div>
    </div>

    <!-- 标签页 -->
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- 资产分组 -->
      <el-tab-pane name="groups" lazy>
        <template #label>
          <span class="tab-label">
            <el-icon><FolderOpened /></el-icon>
            {{ $t('asset.assetGroups') }}
          </span>
        </template>
        <keep-alive>
          <Suspense>
            <template #default>
              <AssetGroupsTab v-if="activeTab === 'groups'" />
            </template>
            <template #fallback>
              <div class="loading-container">
                <el-icon class="is-loading"><Loading /></el-icon>
                <span>{{ $t('common.loading') }}</span>
              </div>
            </template>
          </Suspense>
        </keep-alive>
      </el-tab-pane>

      <!-- 资产清单 -->
      <el-tab-pane name="inventory" lazy>
        <template #label>
          <span class="tab-label">
            <el-icon><List /></el-icon>
            {{ $t('asset.assetInventory') }}
          </span>
        </template>
        <keep-alive>
          <Suspense>
            <template #default>
              <AssetInventoryTab v-if="activeTab === 'inventory'" />
            </template>
            <template #fallback>
              <div class="loading-container">
                <el-icon class="is-loading"><Loading /></el-icon>
                <span>{{ $t('common.loading') }}</span>
              </div>
            </template>
          </Suspense>
        </keep-alive>
      </el-tab-pane>

      <!-- 截图清单 -->
      <el-tab-pane name="screenshots" lazy>
        <template #label>
          <span class="tab-label">
            <el-icon><Picture /></el-icon>
            {{ $t('asset.screenshots') }}
          </span>
        </template>
        <keep-alive>
          <Suspense>
            <template #default>
              <ScreenshotsTab v-if="activeTab === 'screenshots'" />
            </template>
            <template #fallback>
              <div class="loading-container">
                <el-icon class="is-loading"><Loading /></el-icon>
                <span>{{ $t('common.loading') }}</span>
              </div>
            </template>
          </Suspense>
        </keep-alive>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, onMounted, watch, defineAsyncComponent } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  Download,
  Search,
  FolderOpened,
  List,
  Picture,
  Loading,
  ArrowDown,
  Document
} from '@element-plus/icons-vue'
import { getAssetInventory, getAssetGroups, getScreenshots } from '@/api/asset'

// 懒加载子组件，只在需要时才加载
const AssetGroupsTab = defineAsyncComponent(() => 
  import('./AssetManagement/AssetGroupsTab.vue')
)
const AssetInventoryTab = defineAsyncComponent(() => 
  import('./AssetManagement/AssetInventoryTab.vue')
)
const ScreenshotsTab = defineAsyncComponent(() => 
  import('./AssetManagement/ScreenshotsTab.vue')
)

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

// 当前激活的标签页
const activeTab = ref('groups')

// 处理标签页切换
const handleTabChange = (tabName) => {
  // 更新URL参数，保留其他参数
  router.push({
    query: { ...route.query, tab: tabName }
  })
}

// 导出功能
const handleExportCommand = async (format) => {
  try {
    ElMessage.info(t('asset.exportPreparing'))
    
    let data = []
    let filename = ''
    
    // 根据当前标签页获取数据
    if (activeTab.value === 'groups') {
      const res = await getAssetGroups({ page: 1, pageSize: 10000 })
      if (res.code === 0) {
        data = res.list || []
        filename = `asset_groups_${formatDate()}`
      }
    } else if (activeTab.value === 'inventory') {
      const res = await getAssetInventory({ page: 1, pageSize: 10000 })
      if (res.code === 0) {
        data = res.list || []
        filename = `asset_inventory_${formatDate()}`
      }
    } else if (activeTab.value === 'screenshots') {
      const res = await getScreenshots({ page: 1, pageSize: 10000 })
      if (res.code === 0) {
        data = res.list || []
        filename = `asset_screenshots_${formatDate()}`
      }
    }
    
    if (data.length === 0) {
      ElMessage.warning(t('asset.noDataToExport'))
      return
    }
    
    // 根据格式导出
    if (format === 'csv') {
      exportToCsv(data, filename)
    } else if (format === 'json') {
      exportToJson(data, filename)
    } else if (format === 'excel') {
      exportToExcel(data, filename)
    }
    
    ElMessage.success(t('asset.exportSuccess'))
  } catch (error) {
    console.error('导出失败:', error)
    ElMessage.error(t('asset.exportFailed'))
  }
}

// 格式化日期
const formatDate = () => {
  const now = new Date()
  return `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}_${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}`
}

// 导出为 CSV
const exportToCsv = (data, filename) => {
  if (data.length === 0) return
  
  // 获取所有列名
  const headers = getExportHeaders()
  
  // 构建 CSV 内容
  let csvContent = '\uFEFF' // BOM for UTF-8
  csvContent += headers.map(h => h.label).join(',') + '\n'
  
  data.forEach(row => {
    const values = headers.map(h => {
      let value = row[h.key]
      if (Array.isArray(value)) {
        value = value.join('; ')
      }
      if (value === null || value === undefined) {
        value = ''
      }
      // 处理包含逗号或换行的值
      if (String(value).includes(',') || String(value).includes('\n') || String(value).includes('"')) {
        value = `"${String(value).replace(/"/g, '""')}"`
      }
      return value
    })
    csvContent += values.join(',') + '\n'
  })
  
  downloadFile(csvContent, `${filename}.csv`, 'text/csv;charset=utf-8')
}

// 导出为 JSON
const exportToJson = (data, filename) => {
  const jsonContent = JSON.stringify(data, null, 2)
  downloadFile(jsonContent, `${filename}.json`, 'application/json;charset=utf-8')
}

// 导出为 Excel (CSV 格式，Excel 可直接打开)
const exportToExcel = (data, filename) => {
  // 使用 CSV 格式，但文件扩展名为 .xls，Excel 可以直接打开
  if (data.length === 0) return
  
  const headers = getExportHeaders()
  
  // 构建表格 HTML
  let htmlContent = '<html xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel" xmlns="http://www.w3.org/TR/REC-html40">'
  htmlContent += '<head><meta charset="UTF-8"><!--[if gte mso 9]><xml><x:ExcelWorkbook><x:ExcelWorksheets><x:ExcelWorksheet><x:Name>Sheet1</x:Name><x:WorksheetOptions><x:DisplayGridlines/></x:WorksheetOptions></x:ExcelWorksheet></x:ExcelWorksheets></x:ExcelWorkbook></xml><![endif]--></head>'
  htmlContent += '<body><table border="1">'
  
  // 表头
  htmlContent += '<tr>'
  headers.forEach(h => {
    htmlContent += `<th style="background-color:#f0f0f0;font-weight:bold;">${escapeHtml(h.label)}</th>`
  })
  htmlContent += '</tr>'
  
  // 数据行
  data.forEach(row => {
    htmlContent += '<tr>'
    headers.forEach(h => {
      let value = row[h.key]
      if (Array.isArray(value)) {
        value = value.join('; ')
      }
      if (value === null || value === undefined) {
        value = ''
      }
      htmlContent += `<td>${escapeHtml(String(value))}</td>`
    })
    htmlContent += '</tr>'
  })
  
  htmlContent += '</table></body></html>'
  
  downloadFile(htmlContent, `${filename}.xls`, 'application/vnd.ms-excel;charset=utf-8')
}

// 获取导出列头
const getExportHeaders = () => {
  if (activeTab.value === 'groups') {
    return [
      { key: 'domain', label: t('asset.domain') },
      { key: 'totalServices', label: t('asset.totalServices') },
      { key: 'status', label: t('common.status') },
      { key: 'duration', label: t('asset.duration') },
      { key: 'lastUpdated', label: t('asset.lastUpdated') }
    ]
  } else if (activeTab.value === 'inventory') {
    return [
      { key: 'host', label: t('asset.host') },
      { key: 'port', label: t('asset.port') },
      { key: 'ip', label: t('asset.ip') },
      { key: 'title', label: t('asset.pageTitle') },
      { key: 'status', label: t('asset.statusCode') },
      { key: 'technologies', label: t('asset.technologies') },
      { key: 'labels', label: t('asset.labels') },
      { key: 'lastUpdated', label: t('asset.lastUpdated') }
    ]
  } else if (activeTab.value === 'screenshots') {
    return [
      { key: 'host', label: t('asset.host') },
      { key: 'port', label: t('asset.port') },
      { key: 'title', label: t('asset.pageTitle') },
      { key: 'status', label: t('asset.statusCode') },
      { key: 'lastUpdated', label: t('asset.lastUpdated') }
    ]
  }
  return []
}

// HTML 转义
const escapeHtml = (str) => {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
}

// 下载文件
const downloadFile = (content, filename, mimeType) => {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

// 开始扫描
const handleStartScan = () => {
  router.push('/task/create')
}

// 监听路由变化，同步标签页
watch(() => route.query.tab, (newTab) => {
  if (newTab && newTab !== activeTab.value) {
    activeTab.value = newTab
  }
}, { immediate: true })

onMounted(() => {
  // 从URL参数读取初始标签页
  if (route.query.tab) {
    activeTab.value = route.query.tab
  }
})
</script>

<style lang="scss" scoped>
.asset-management {
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

.tab-label {
  display: flex;
  align-items: center;
  gap: 8px;
  
  .el-icon {
    font-size: 16px;
  }
}

.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  color: hsl(var(--muted-foreground));
  
  .el-icon {
    font-size: 32px;
    margin-bottom: 12px;
  }
  
  span {
    font-size: 14px;
  }
}
</style>
