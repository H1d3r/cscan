<template>
  <div class="asset-management">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-content">
        <h1>资产管理</h1>
        <p class="description">
          统一管理和查看所有资产信息，包括分组、清单和截图
        </p>
      </div>
      <div class="header-actions">
        <el-button @click="handleExport">
          <el-icon><Download /></el-icon>
          导出
        </el-button>
        <el-button type="primary" @click="handleStartScan">
          <el-icon><Search /></el-icon>
          开始扫描
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
            资产分组
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
                <span>加载中...</span>
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
            资产清单
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
                <span>加载中...</span>
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
            截图清单
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
                <span>加载中...</span>
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
import {
  Download,
  Search,
  FolderOpened,
  List,
  Picture,
  Loading
} from '@element-plus/icons-vue'

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
const handleExport = () => {
  ElMessage.success('导出功能开发中')
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
