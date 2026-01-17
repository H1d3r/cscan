<template>
  <div class="site-page">
    <!-- 搜索区域 -->
    <el-card class="search-card">
      <el-form :inline="true" class="search-form">
        <el-form-item :label="$t('site.site')">
          <el-input v-model="searchForm.site" :placeholder="$t('site.url')" clearable @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item :label="$t('site.pageTitle')">
          <el-input v-model="searchForm.title" :placeholder="$t('site.pageTitle')" clearable @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item :label="$t('site.fingerprint')">
          <el-input v-model="searchForm.app" :placeholder="$t('site.fingerprint')" clearable @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item :label="$t('site.statusCode')">
          <el-select v-model="searchForm.httpStatus" :placeholder="$t('common.all')" clearable style="width: 100px">
            <el-option label="200" value="200" />
            <el-option label="301" value="301" />
            <el-option label="302" value="302" />
            <el-option label="403" value="403" />
            <el-option label="404" value="404" />
            <el-option label="500" value="500" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('site.organization')">
          <el-select v-model="searchForm.orgId" :placeholder="$t('common.allOrganizations')" clearable style="width: 140px">
            <el-option :label="$t('common.allOrganizations')" value="" />
            <el-option v-for="org in organizations" :key="org.id" :label="org.name" :value="org.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 统计信息 -->
    <el-row :gutter="16" class="stat-row">
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-value">{{ stat.total }}</div>
          <div class="stat-label">{{ $t('site.totalSites') }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-value">{{ stat.httpCount }}</div>
          <div class="stat-label">{{ $t('site.httpSites') }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-value">{{ stat.httpsCount }}</div>
          <div class="stat-label">{{ $t('site.httpsSites') }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-value">{{ stat.newCount }}</div>
          <div class="stat-label">{{ $t('site.newSites') }}</div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <div class="table-header">
        <span class="total-info">{{ $t('common.total') }} {{ pagination.total }} {{ $t('site.site') }}</span>
        <div class="table-actions">
          <el-button type="danger" size="small" :disabled="selectedRows.length === 0" @click="handleBatchDelete">
            {{ $t('common.batchDelete') }} ({{ selectedRows.length }})
          </el-button>
        </div>
      </div>
      
      <el-table :data="tableData" v-loading="loading" stripe @selection-change="handleSelectionChange">
        <el-table-column type="selection" width="40" />
        <el-table-column :label="$t('site.site')" min-width="280">
          <template #default="{ row }">
            <div class="site-cell">
              <el-image 
                v-if="row.screenshot" 
                :src="formatScreenshotUrl(row.screenshot)" 
                :preview-src-list="[formatScreenshotUrl(row.screenshot)]"
                :z-index="9999"
                :preview-teleported="true"
                :hide-on-click-modal="true"
                fit="cover"
                lazy
                class="site-screenshot"
              />
              <div class="site-info">
                <a :href="row.site" target="_blank" class="site-link">{{ row.site }}</a>
                <div class="site-title" :title="row.title">{{ row.title || '-' }}</div>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="IP" width="140">
          <template #default="{ row }">
            <div>{{ row.ip }}</div>
            <div v-if="row.location" class="location-text">{{ row.location }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.status')" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.httpStatus)" size="small">{{ row.httpStatus || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('site.fingerprint')" min-width="200">
          <template #default="{ row }">
            <div class="app-tags">
              <el-tag v-for="app in (row.app || []).slice(0, 5)" :key="app" size="small" type="success" class="app-tag">
                {{ getAppName(app) }}
              </el-tag>
              <span v-if="(row.app || []).length > 5" class="more-apps">+{{ (row.app || []).length - 5 }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('site.organization')" width="120">
          <template #default="{ row }">
            {{ row.orgName || $t('common.defaultOrganization') }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.updateTime')" width="160">
          <template #default="{ row }">
            {{ row.updateTime }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :total="pagination.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        class="pagination"
        @size-change="loadData"
        @current-change="loadData"
      />
    </el-card>

    <!-- 详情对话框 -->
    <el-dialog v-model="detailVisible" :title="$t('site.siteDetail')" width="800px">
      <el-descriptions v-if="currentSite" :column="2" border>
        <el-descriptions-item :label="$t('site.siteUrl')" :span="2">
          <a :href="currentSite.site" target="_blank">{{ currentSite.site }}</a>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('site.pageTitle')" :span="2">{{ currentSite.title || '-' }}</el-descriptions-item>
        <el-descriptions-item label="IP">{{ currentSite.ip }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.port')">{{ currentSite.port }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.statusCode')">{{ currentSite.httpStatus }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.service')">{{ currentSite.service }}</el-descriptions-item>
        <el-descriptions-item :label="$t('site.fingerprint')" :span="2">
          <el-tag v-for="app in (currentSite.app || [])" :key="app" size="small" type="success" style="margin-right: 4px">
            {{ app }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('site.responseHeader')" :span="2">
          <pre class="header-pre">{{ currentSite.httpHeader || '-' }}</pre>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '@/api/request'
import { useWorkspaceStore } from '@/stores/workspace'
import { formatScreenshotUrl } from '@/utils/screenshot'

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const tableData = ref([])
const selectedRows = ref([])
const organizations = ref([])
const detailVisible = ref(false)
const currentSite = ref(null)

const searchForm = reactive({
  site: '',
  title: '',
  app: '',
  httpStatus: '',
  orgId: ''
})

const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

const stat = reactive({
  total: 0,
  httpCount: 0,
  httpsCount: 0,
  newCount: 0
})

function handleWorkspaceChanged() {
  pagination.page = 1
  loadData()
  loadStat()
}

onMounted(() => {
  loadData()
  loadStat()
  loadOrganizations()
  window.addEventListener('workspace-changed', handleWorkspaceChanged)
})

onUnmounted(() => {
  window.removeEventListener('workspace-changed', handleWorkspaceChanged)
})

async function loadData() {
  loading.value = true
  try {
    const res = await request.post('/asset/site/list', {
      page: pagination.page,
      pageSize: pagination.pageSize,
      site: searchForm.site,
      title: searchForm.title,
      app: searchForm.app,
      httpStatus: searchForm.httpStatus,
      orgId: searchForm.orgId
    })
    if (res.code === 0) {
      tableData.value = res.list || []
      pagination.total = res.total || 0
    }
  } finally {
    loading.value = false
  }
}

async function loadStat() {
  try {
    const res = await request.post('/asset/site/stat', {})
    if (res.code === 0) {
      stat.total = res.total || 0
      stat.httpCount = res.httpCount || 0
      stat.httpsCount = res.httpsCount || 0
      stat.newCount = res.newCount || 0
    }
  } catch (e) {
    console.error('Failed to load stat:', e)
  }
}

async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    if (res.code === 0) {
      organizations.value = res.list || []
    }
  } catch (e) {
    console.error('Failed to load organizations:', e)
  }
}

function handleSearch() {
  pagination.page = 1
  loadData()
}

function handleReset() {
  Object.assign(searchForm, { site: '', title: '', app: '', httpStatus: '', orgId: '' })
  handleSearch()
}

function handleSelectionChange(rows) {
  selectedRows.value = rows
}

async function handleDelete(row) {
  await ElMessageBox.confirm(t('site.confirmDeleteSite'), t('common.tip'), { type: 'warning' })
  const res = await request.post('/asset/delete', { id: row.id })
  if (res.code === 0) {
    ElMessage.success(t('common.deleteSuccess'))
    loadData()
  }
}

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  await ElMessageBox.confirm(t('site.confirmBatchDeleteSite', { count: selectedRows.value.length }), t('common.tip'), { type: 'warning' })
  const ids = selectedRows.value.map(row => row.id)
  const res = await request.post('/asset/batchDelete', { ids })
  if (res.code === 0) {
    ElMessage.success(t('common.deleteSuccess'))
    selectedRows.value = []
    loadData()
  }
}

function showDetail(row) {
  currentSite.value = row
  detailVisible.value = true
}

function getStatusType(status) {
  if (!status) return 'info'
  const code = parseInt(status)
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'warning'
  return 'danger'
}

function getAppName(app) {
  if (!app) return ''
  const idx = app.indexOf('[')
  return idx > 0 ? app.substring(0, idx) : app
}
</script>

<style scoped>
.site-page {
  .search-card {
    margin-bottom: 16px;
  }
  
  .stat-row {
    margin-bottom: 16px;
    
    .stat-card {
      text-align: center;
      
      .stat-value {
        font-size: 28px;
        font-weight: 600;
        color: var(--el-color-primary);
      }
      
      .stat-label {
        color: var(--el-text-color-secondary);
        margin-top: 8px;
      }
    }
  }
  
  .table-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    
    .total-info {
      color: var(--el-text-color-secondary);
    }
  }
  
  .site-cell {
    display: flex;
    align-items: center;
    
    .site-screenshot {
      width: 80px;
      height: 60px;
      border-radius: 4px;
      margin-right: 12px;
      flex-shrink: 0;
    }
    
    .site-info {
      overflow: hidden;
      
      .site-link {
        color: var(--el-color-primary);
        text-decoration: none;
        display: block;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        
        &:hover {
          text-decoration: underline;
        }
      }
      
      .site-title {
        color: var(--el-text-color-secondary);
        font-size: 12px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        margin-top: 4px;
      }
    }
  }
  
  .location-text {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  
  .app-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    
    .app-tag {
      max-width: 100px;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    
    .more-apps {
      color: var(--el-text-color-secondary);
      font-size: 12px;
    }
  }
  
  .pagination {
    margin-top: 16px;
    justify-content: flex-end;
  }
  
  .header-pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 200px;
    overflow: auto;
    font-size: 12px;
  }
}
</style>

