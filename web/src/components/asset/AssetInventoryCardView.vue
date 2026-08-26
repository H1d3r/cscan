<template>
  <div class="targets-view">
    <!-- defer：目标 .header-search 由同一页面树渲染，需等整树挂载后再解析，否则目标为 null 会中断挂载 -->
    <Teleport defer to=".asset-space-search .header-search">
      <el-input
        v-model="searchQuery"
        :placeholder="$t('asset.targetView.searchPlaceholder')"
        clearable
        class="search-input"
        @clear="handleSearch"
        @keyup.enter="handleSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
    </Teleport>

    <!-- Toolbar：过滤器 + 按钮靠右（搜索框位于页面头部） -->
    <div class="toolbar">
      <div class="toolbar-right">
        <TargetTypeFilter v-model="typeFilter" />

        <ScanStatusFilter v-model="statusFilter" />

        <ScopeFilter v-model="scopeFilter" />

        <el-button plain @click="$emit('create-target')">
          <el-icon><Plus /></el-icon>
          {{ $t('asset.manualAddAsset') }}
        </el-button>

        <el-button type="primary" @click="handleStartScan">
          <el-icon><Search /></el-icon>
          {{ $t('asset.startScan') }}
        </el-button>

        <el-button
          type="danger"
          plain
          :disabled="selectedRows.length === 0"
          @click="handleBatchDelete"
        >
          <el-icon><Delete /></el-icon>
          {{ $t('common.delete') }}{{ selectedRows.length ? ` (${selectedRows.length})` : '' }}
        </el-button>

        <el-button plain :loading="loading" @click="fetchData(true)">
          <el-icon><Refresh /></el-icon>
          {{ $t('common.refresh') }}
        </el-button>
      </div>
    </div>

    <!-- Skeleton：首次加载且无数据时显示骨架行（对齐 open-asm DataTable skeleton） -->
    <div v-if="loading && list.length === 0" class="table-skeleton">
      <div v-for="i in pageSize > 10 ? 10 : pageSize" :key="i" class="skeleton-row">
        <div class="skeleton-bar" :style="{ width: '34%' }" />
        <div class="skeleton-bar" :style="{ width: '12%' }" />
        <div class="skeleton-bar" :style="{ width: '16%' }" />
        <div class="skeleton-bar" :style="{ width: '14%' }" />
      </div>
    </div>

    <!-- Table -->
    <el-table
      v-else
      :data="list"
      v-loading="loading"
      class="target-table"
      row-key="id"
      @selection-change="handleSelectionChange"
      @row-click="handleRowClick"
    >
      <el-table-column type="selection" width="42" reserve-selection />

      <el-table-column
        :label="$t('asset.targetView.columnTarget')"
        min-width="320"
      >
        <template #default="{ row }">
          <div class="target-cell">
            <div class="target-main">
              <span
                v-if="row.colorTag"
                class="color-dot"
                :style="{ background: row.colorTag }"
                :title="$t('asset.targetView.colorTag')"
              />
              <span class="target-value">{{ row.targetValue }}</span>

              <el-tooltip
                v-if="row.source"
                :content="`${$t('asset.targetView.sourceSync')}: ${row.source}`"
                placement="top"
                effect="dark"
                :show-arrow="false"
                popper-class="source-sync-tip"
              >
                <span class="source-icon">
                  <el-icon><Link /></el-icon>
                </span>
              </el-tooltip>

              <span v-if="row.internalNetworkId" class="internal-badge">
                {{ $t('asset.targetView.internalBadge') }}
              </span>

              <el-tag
                v-for="label in (row.labels || []).slice(0, 3)"
                :key="label"
                size="small"
                class="row-label-tag"
              >
                {{ label }}
              </el-tag>
              <el-tooltip
                v-if="row.labels && row.labels.length > 3"
                :content="row.labels.join(', ')"
                placement="top"
              >
                <span class="labels-more">+{{ row.labels.length - 3 }}</span>
              </el-tooltip>

              <el-tooltip :content="$t('asset.targetView.editTarget')" placement="top">
                <span class="edit-icon" @click.stop="emit('edit-target', row.id)">
                  <el-icon><EditPen /></el-icon>
                </span>
              </el-tooltip>
            </div>
            <div v-if="row.memo" class="target-memo" :title="row.memo">
              <el-icon><EditPen /></el-icon>
              <span class="memo-text">{{ row.memo }}</span>
            </div>
          </div>
        </template>
      </el-table-column>

      <el-table-column
        :label="$t('asset.targetView.columnServices')"
        width="140"
      >
        <template #default="{ row }">
          <span class="services-count">
            {{ row.totalAssetServices || 0 }} {{ $t('asset.targetView.services') }}
          </span>
        </template>
      </el-table-column>

      <el-table-column
        :label="$t('asset.targetView.columnLastDiscovery')"
        width="180"
      >
        <template #default="{ row }">
          <span class="muted-text">{{ formatRelativeTime(row.lastScanTime) }}</span>
        </template>
      </el-table-column>

      <el-table-column
        :label="$t('asset.targetView.columnScanStatus')"
        width="170"
      >
        <template #default="{ row }">
          <TargetStatusBadge :status="row.scanStatus" />
        </template>
      </el-table-column>

      <template #empty>
        <div class="empty-wrap">{{ $t('asset.targetView.noTargets') }}</div>
      </template>
    </el-table>

    <!-- Pagination -->
    <div v-if="total > 0" class="pagination-wrap">
      <el-pagination
        :current-page="page"
        :page-size="pageSize"
        :page-sizes="[5, 10, 20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        @current-change="handlePageChange"
        @size-change="handleSizeChange"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { Search, Plus, Link, Delete, EditPen, Refresh } from '@element-plus/icons-vue'
import TargetTypeFilter from './TargetTypeFilter.vue'
import ScanStatusFilter from './ScanStatusFilter.vue'
import ScopeFilter from './ScopeFilter.vue'
import TargetStatusBadge from './TargetStatusBadge.vue'
import { getAssetTargetList, deleteAssetTarget } from '@/api/asset'
import { formatRelativeTime } from './targetViewUtils'

const props = defineProps({
  workspaceId: { type: String, default: '' },
})

const emit = defineEmits(['create-target', 'start-scan', 'view-target', 'edit-target'])

const { t } = useI18n()
const loading = ref(false)
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const selectedRows = ref([])

const searchQuery = ref('')
const typeFilter = ref('')
const statusFilter = ref('')
const scopeFilter = ref('')

let searchDebounce = null

// force=true 时携带 refresh 参数，后端跳过 30s 查询缓存强制重算（手动刷新按钮触发）
async function fetchData(force = false) {
  loading.value = true
  try {
    const params = {
      workspaceId: props.workspaceId,
      page: page.value,
      pageSize: pageSize.value,
      query: searchQuery.value || undefined,
      targetType: typeFilter.value || undefined,
      scanStatus: statusFilter.value || undefined,
      scope: scopeFilter.value || undefined,
      refresh: force || undefined,
    }

    // 响应拦截器返回 {code,msg,total,list}（顶层无 data 包裹）
    const res = await getAssetTargetList(params)
    const payload = res?.data ?? res
    list.value = payload?.list || []
    total.value = payload?.total || 0
  } catch (err) {
    console.error('[TargetsView] fetchData error:', err)
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  page.value = 1
  fetchData()
}

function handlePageChange(newPage) {
  page.value = newPage
  fetchData()
}

function handleSizeChange(newSize) {
  pageSize.value = newSize
  page.value = 1
  fetchData()
}

function handleRowClick(row) {
  emit('view-target', row.id)
}

function handleSelectionChange(rows) {
  selectedRows.value = rows || []
}

// 批量删除目标（自旧资产概览页迁入：级联删除底层资产）
async function handleStartScan() {
  const targetIds = selectedRows.value.map(row => row.id)
  emit('start-scan', targetIds)
}

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  try {
    await ElMessageBox.confirm(
      t('asset.assetGroupsTab.confirmBatchDelete', { count: selectedRows.value.length }),
      t('common.batchDelete'),
      { type: 'warning', confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel') }
    )
  } catch {
    return
  }
  loading.value = true
  try {
    await Promise.all(
      selectedRows.value.map(row => deleteAssetTarget({ targetId: row.id, deleteAssets: true }))
    )
    ElMessage.success(t('asset.assetGroupsTab.deleteSuccess'))
    selectedRows.value = []
    await fetchData()
  } catch (err) {
    console.error('[TargetsView] batch delete error:', err)
    ElMessage.error(t('asset.assetGroupsTab.deleteFailed'))
  } finally {
    loading.value = false
  }
}

// 搜索输入防抖 500ms（对齐 open-asm useDebounce(500)）
watch(searchQuery, () => {
  if (searchDebounce) clearTimeout(searchDebounce)
  searchDebounce = setTimeout(handleSearch, 500)
})

watch([typeFilter, statusFilter, scopeFilter], () => {
  page.value = 1
  fetchData()
})

onMounted(fetchData)

onUnmounted(() => {
  if (searchDebounce) clearTimeout(searchDebounce)
})

// 供父页面手动刷新（如手动添加资产后），强制绕过后端缓存
defineExpose({ refresh: () => fetchData(true) })
</script>

<style scoped lang="scss">
// source 图标 tooltip：深浅主题统一深色气泡 + 白字，隐藏箭头避免多余方块
:global(.source-sync-tip.el-popper) {
  color: #ffffff !important;
  background: #1a1a1a !important;

  .el-popper__arrow {
    display: none !important;
  }
}

.targets-view {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;

  .toolbar-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
}

.search-input {
  width: 260px;
}

.table-skeleton {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 4px 0;

  .skeleton-row {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 14px 16px;
    border-bottom: 1px solid var(--el-border-color-extra-light);

    &:last-child {
      border-bottom: none;
    }

    .skeleton-bar {
      height: 14px;
      border-radius: 4px;
      background: var(--el-fill-color);
      animation: skeleton-pulse 1.5s ease-in-out infinite;
    }
  }
}

@keyframes skeleton-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

.target-table {
  cursor: pointer;

  :deep(.el-table__row) {
    transition: background-color 0.15s ease;

    &:hover {
      background-color: var(--el-fill-color-light);
    }
  }
}

.target-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;

  .target-main {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .color-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex-shrink: 0;
    border: 1px solid var(--el-border-color);
  }

  .target-value {
    font-weight: 500;
    font-size: 14px;
    color: var(--el-text-color-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-label-tag {
    flex-shrink: 0;
    max-width: 110px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .labels-more {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    flex-shrink: 0;
    cursor: default;
  }

  .edit-icon {
    display: inline-flex;
    align-items: center;
    color: var(--el-text-color-secondary);
    font-size: 14px;
    cursor: pointer;
    flex-shrink: 0;
    transition: color 0.15s ease;

    &:hover {
      color: var(--el-color-primary);
    }
  }

  .target-memo {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    font-size: 12px;
    color: var(--el-text-color-secondary);

    .el-icon {
      font-size: 12px;
      flex-shrink: 0;
    }

    .memo-text {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  .source-icon {
    display: inline-flex;
    align-items: center;
    color: var(--el-text-color-secondary);
    font-size: 14px;
    cursor: help;
    flex-shrink: 0;

    // 深色模式下图标本身用前景色，保证可见
    html.dark &,
    :global(html.dark) & {
      color: var(--el-text-color-primary);
    }
  }

  .internal-badge {
    display: inline-flex;
    align-items: center;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 11px;
    line-height: 16px;
    background: var(--el-fill-color);
    color: var(--el-text-color-regular);
    border: 1px solid var(--el-border-color);
    white-space: nowrap;
    flex-shrink: 0;
  }
}

.services-count {
  font-weight: 500;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.muted-text {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.empty-wrap {
  padding: 32px 0;
  color: var(--el-text-color-secondary);
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  padding-top: 4px;
}
</style>
