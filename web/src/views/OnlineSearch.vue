<template>
  <div class="online-search-page">
    <!-- 搜索区域 -->
    <el-card class="search-card">
      <el-form :model="store.searchForm" inline>
        <el-form-item :label="$t('onlineSearch.dataSource')">
          <el-select v-model="store.searchForm.source" style="width: 120px" @change="handleSourceChange">
            <el-option label="Fofa" value="fofa" />
            <el-option label="Hunter" value="hunter" />
            <el-option label="Quake" value="quake" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('onlineSearch.queryStatement')" style="flex: 1">
          <el-input
            v-model="store.searchForm.query"
            :placeholder="$t('onlineSearch.queryPlaceholder')"
            style="width: 400px"
            @keyup.enter="handleSearch"
          />
        </el-form-item>
        <el-form-item :label="$t('onlineSearch.quantity')">
          <el-select v-model="store.searchForm.size" style="width: 100px">
            <el-option :value="10" label="10" />
            <el-option :value="50" label="50" />
            <el-option :value="100" label="100" />
            <el-option v-if="store.searchForm.source === 'fofa'" :value="500" label="500" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="loading" @click="handleSearch">{{ $t('onlineSearch.search') }}</el-button>
          <el-button @click="handleImport" :disabled="!tableData.length">{{ $t('onlineSearch.importCurrent') }}</el-button>
          <el-button type="success" @click="handleImportAll" :disabled="!total" :loading="importAllLoading">{{ $t('onlineSearch.importAll') }}</el-button>
          <el-button @click="showHelpDialog">{{ $t('onlineSearch.syntaxHelp') }}</el-button>
        </el-form-item>
      </el-form>

      <!-- 快捷查询 -->
      <div class="quick-search">
        <span class="label">{{ $t('onlineSearch.quickQuery') }}</span>
        <el-tag
          v-for="item in quickQueries"
          :key="item.query"
          class="quick-tag"
          @click="applyQuickQuery(item)"
        >
          {{ item.label }}
        </el-tag>
      </div>
    </el-card>

    <!-- 结果表格 -->
    <el-card class="result-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('onlineSearch.searchResult') }}</span>
          <span v-if="total > 0" class="total">{{ $t('onlineSearch.total') }} {{ total }} {{ $t('onlineSearch.items') }}</span>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" stripe max-height="500">
        <el-table-column type="selection" width="50" />
        <el-table-column prop="ip" :label="$t('onlineSearch.ip')" width="140" />
        <el-table-column prop="port" :label="$t('onlineSearch.port')" width="80" />
        <el-table-column prop="protocol" :label="$t('onlineSearch.protocol')" width="80" />
        <el-table-column prop="domain" :label="$t('onlineSearch.domain')" min-width="150" show-overflow-tooltip />
        <el-table-column prop="title" :label="$t('onlineSearch.pageTitle')" min-width="200" show-overflow-tooltip />
        <el-table-column prop="server" :label="$t('onlineSearch.server')" width="120" show-overflow-tooltip />
        <el-table-column prop="product" :label="$t('onlineSearch.product')" width="120" show-overflow-tooltip />
        <el-table-column prop="country" :label="$t('onlineSearch.country')" width="80" />
        <el-table-column prop="city" :label="$t('onlineSearch.city')" width="100" />
        <el-table-column prop="icp" :label="$t('onlineSearch.icpRecord')" width="150" show-overflow-tooltip />
      </el-table>

      <el-pagination
        v-if="total > 0"
        v-model:current-page="store.searchForm.page"
        :page-size="store.searchForm.size"
        :total="total"
        layout="total, prev, pager, next"
        class="pagination"
        @current-change="handleSearch"
      />
    </el-card>

    <!-- 语法帮助对话框 -->
    <el-dialog v-model="helpDialogVisible" :title="$t('onlineSearch.syntaxHelp')" width="650px">
      <el-tabs v-model="helpTab">
        <el-tab-pane label="Fofa" name="fofa">
          <div class="syntax-help">
            <p><code>ip="1.1.1.1"</code> - Search by IP</p>
            <p><code>domain="example.com"</code> - Search by domain</p>
            <p><code>title="admin"</code> - Search by title</p>
            <p><code>body="content"</code> - Search by body content</p>
            <p><code>port="80"</code> - Search by port</p>
            <p><code>icp="ICP"</code> - Search by ICP record</p>
            <p><code>org="company"</code> - Search by organization</p>
            <p>Combined: <code>ip="1.1.1.1" && port="80"</code></p>
          </div>
        </el-tab-pane>
        <el-tab-pane label="Hunter" name="hunter">
          <div class="syntax-help">
            <p><code>ip="1.1.1.1"</code> - Search by IP</p>
            <p><code>domain.suffix="example.com"</code> - Search by domain suffix</p>
            <p><code>web.title="admin"</code> - Search by web title</p>
            <p><code>icp.name="company"</code> - Search by ICP name</p>
            <p><code>icp.number="ICP"</code> - Search by ICP number</p>
            <p><code>port="443"</code> - Search by port</p>
          </div>
        </el-tab-pane>
        <el-tab-pane label="Quake" name="quake">
          <div class="syntax-help">
            <p><code>ip:"1.1.1.1"</code> - Search by IP</p>
            <p><code>domain:"example.com"</code> - Search by domain</p>
            <p><code>title:"admin"</code> - Search by title</p>
            <p><code>service:"http"</code> - Search by service</p>
            <p><code>port:"80"</code> - Search by port</p>
            <p><code>country:"CN"</code> - Search by country</p>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '@/api/request'
import { useOnlineSearchStore } from '@/stores/onlineSearch'

const { t } = useI18n()
const store = useOnlineSearchStore()

const loading = ref(false)
const importAllLoading = ref(false)
const helpDialogVisible = ref(false)
const helpTab = ref('fofa')

// 使用 store 中的数据
const tableData = computed(() => store.tableData)
const total = computed(() => store.total)

const quickQueries = computed(() => [
  { label: t('onlineSearch.ipSearch'), query: 'ip="1.1.1.1"' },
  { label: t('onlineSearch.domainSearch'), query: 'domain="example.com"' },
  { label: t('onlineSearch.titleSearch'), query: 'title="admin"' },
  { label: t('onlineSearch.icpSearch'), query: 'icp="ICP"' },
  { label: t('onlineSearch.portSearch'), query: 'port="3389"' },
])

async function handleSearch() {
  if (!store.searchForm.query) {
    ElMessage.warning(t('onlineSearch.enterQuery'))
    return
  }

  loading.value = true
  try {
    const res = await request.post('/onlineapi/search', {
      platform: store.searchForm.source,
      query: store.searchForm.query,
      page: store.searchForm.page,
      pageSize: store.searchForm.size
    })
    if (res.code === 0) {
      store.saveState(store.searchForm, res.list || [], res.total || 0)
    } else {
      ElMessage.error(res.msg || t('onlineSearch.searchFailed'))
    }
  } finally {
    loading.value = false
  }
}

function applyQuickQuery(item) {
  store.searchForm.query = item.query
}

// 数据源切换时，如果当前数量超过限制则自动调整
function handleSourceChange() {
  if (store.searchForm.source !== 'fofa' && store.searchForm.size > 100) {
    store.searchForm.size = 100
  }
}

async function handleImport() {
  await ElMessageBox.confirm(t('onlineSearch.confirmImportCurrent', { count: tableData.value.length }), t('common.tip'))
  
  const res = await request.post('/onlineapi/import', { 
    assets: tableData.value,
    platform: store.searchForm.source 
  })
  
  if (res.code === 0) {
    ElMessage.success(res.msg || t('onlineSearch.importSuccess'))
  } else {
    ElMessage.error(res.msg || t('onlineSearch.importFailed'))
  }
}

async function handleImportAll() {
  if (!store.searchForm.query) {
    ElMessage.warning(t('onlineSearch.enterQueryFirst'))
    return
  }

  // 计算预计导入数量
  const estimatedCount = total.value
  
  await ElMessageBox.confirm(
    t('onlineSearch.confirmImportAll', { count: estimatedCount }),
    t('onlineSearch.importAllTitle'),
    { type: 'warning' }
  )

  importAllLoading.value = true
  try {
    // Hunter 和 Quake 单次最多 100，Fofa 可以 500
    const pageSize = store.searchForm.source === 'fofa' ? store.searchForm.size : Math.min(store.searchForm.size, 100)
    
    const res = await request.post('/onlineapi/importAll', {
      platform: store.searchForm.source,
      query: store.searchForm.query,
      pageSize: pageSize,
      maxPages: 0  // 0 表示不限制页数
    })

    if (res.code === 0) {
      ElMessage.success(res.msg || t('onlineSearch.importSuccess'))
    } else {
      ElMessage.error(res.msg || t('onlineSearch.importFailed'))
    }
  } finally {
    importAllLoading.value = false
  }
}

function showHelpDialog() {
  helpDialogVisible.value = true
}
</script>

<style scoped>
.online-search-page {
  .search-card {
    margin-bottom: 20px;

    .quick-search {
      margin-top: 10px;
      padding-top: 10px;
      border-top: 1px solid var(--el-border-color);

      .label {
        color: var(--el-text-color-regular);
        margin-right: 10px;
      }

      .quick-tag {
        cursor: pointer;
        margin-right: 8px;

        &:hover {
          background: var(--el-color-primary);
          color: var(--el-color-white);
        }
      }
    }
  }

  .result-card {
    margin-bottom: 20px;

    .card-header {
      display: flex;
      justify-content: space-between;
      align-items: center;

      .total {
        color: var(--el-text-color-secondary);
        font-size: 14px;
      }
    }

    .pagination {
      margin-top: 20px;
      justify-content: flex-end;
    }
  }

  .syntax-help {
    p {
      margin: 8px 0;
      line-height: 1.6;

      code {
        background: var(--el-fill-color-light);
        padding: 2px 6px;
        border-radius: 4px;
        color: var(--el-color-primary);
      }
    }
  }
}
</style>

