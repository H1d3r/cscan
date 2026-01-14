<template>
  <div class="worker-logs-page">
    <el-card>
      <template #header>
        <div class="log-header">
          <span>{{ $t('worker.workerRunningLogs') }}</span>
          <div class="log-filters">
            <el-input
              v-model="searchKeyword"
              :placeholder="$t('worker.searchLogs')"
              clearable
              size="small"
              style="width: 180px; margin-right: 10px"
              @keyup.enter="resetAndFetch"
              @clear="resetAndFetch"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-select 
              v-model="filterWorker" 
              :placeholder="$t('worker.filterWorker')" 
              clearable 
              size="small"
              style="width: 150px; margin-right: 10px"
              @change="resetAndFetch"
            >
              <el-option :label="$t('worker.allWorkers')" value="" />
              <el-option 
                v-for="worker in workerList" 
                :key="worker.name" 
                :label="worker.name" 
                :value="worker.name" 
              />
            </el-select>
            <el-select 
              v-model="filterLevel" 
              :placeholder="$t('worker.filterLevel')" 
              clearable 
              size="small"
              style="width: 120px; margin-right: 10px"
              @change="resetAndFetch"
            >
              <el-option :label="$t('worker.allLevels')" value="" />
              <el-option label="INFO" value="INFO" />
              <el-option label="WARN" value="WARN" />
              <el-option label="ERROR" value="ERROR" />
              <el-option label="DEBUG" value="DEBUG" />
            </el-select>
            <el-switch v-model="autoScroll" :active-text="$t('worker.autoScrolling')" style="margin-right: 15px" />
            <el-dropdown trigger="click" style="margin-right: 10px" @command="handleExport">
              <el-button size="small">
                {{ $t('worker.exportLogs') }} <el-icon class="el-icon--right"><ArrowDown /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="json">{{ $t('worker.exportJson') }}</el-dropdown-item>
                  <el-dropdown-item command="txt">{{ $t('worker.exportTxt') }}</el-dropdown-item>
                  <el-dropdown-item command="csv">{{ $t('worker.exportCsv') }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-button size="small" @click="clearLogs">{{ $t('worker.clear') }}</el-button>
            <el-button size="small" :type="isConnected ? 'success' : 'danger'" @click="toggleConnection">
              {{ isConnected ? $t('worker.autoRefreshing') : $t('worker.paused') }}
            </el-button>
          </div>
        </div>
      </template>
      <div class="log-stats">
        <span>{{ $t('worker.totalLogs') }}: {{ totalCount }}</span>
        <span style="margin-left: 15px">{{ $t('worker.loadedLogs') }}: {{ logs.length }}</span>
        <span v-if="hasMore" style="margin-left: 15px; color: var(--el-color-primary); cursor: pointer" @click="loadMore">
          {{ $t('worker.loadMore') }}
        </span>
      </div>
      <div ref="logContainer" class="log-container" @scroll="handleScroll">
        <div v-if="loadingMore" class="log-loading">{{ $t('worker.loadingMore') }}</div>
        <div v-for="log in logs" :key="log.id" class="log-item" :class="'log-' + log.level?.toLowerCase()">
          <span class="log-time">{{ log.timestamp }}</span>
          <span class="log-level">[{{ log.level }}]</span>
          <span class="log-worker">[{{ log.workerName }}]</span>
          <span class="log-message">{{ log.message }}</span>
        </div>
        <div v-if="logs.length === 0 && !loading" class="log-empty">{{ $t('worker.noLogsYet') }}</div>
        <div v-if="loading && logs.length === 0" class="log-empty">{{ $t('worker.loadingLogs') }}</div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { Search, ArrowDown } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import request from '@/api/request'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const logs = ref([])
const logContainer = ref(null)
const autoScroll = ref(true)
const isConnected = ref(false)
const filterWorker = ref('')
const filterLevel = ref('')
const searchKeyword = ref('')
const workerList = ref([])
const totalCount = ref(0)
const hasMore = ref(false)
const loading = ref(false)
const loadingMore = ref(false)
let pollingTimer = null
let newestLogId = ''

onMounted(() => {
  loadWorkerList()
  fetchInitialLogs()
  startPolling()
})

onUnmounted(() => {
  stopPolling()
})

watch(() => logs.value.length, () => {
  if (autoScroll.value) {
    nextTick(() => {
      if (logContainer.value) {
        logContainer.value.scrollTop = logContainer.value.scrollHeight
      }
    })
  }
})

async function loadWorkerList() {
  try {
    const res = await request.post('/worker/list')
    if (res.code === 0) workerList.value = res.list || []
  } catch (e) {
    console.error('Load worker list error:', e)
  }
}

function startPolling() {
  if (pollingTimer) return
  isConnected.value = true
  pollingTimer = setInterval(fetchNewLogs, 2000)
}

function stopPolling() {
  if (pollingTimer) {
    clearInterval(pollingTimer)
    pollingTimer = null
  }
  isConnected.value = false
}

// 首次加载日志
async function fetchInitialLogs() {
  loading.value = true
  try {
    const res = await request.post('/worker/logs/history', {
      limit: 100,
      search: searchKeyword.value,
      worker: filterWorker.value,
      level: filterLevel.value
    })
    if (res.code === 0) {
      logs.value = res.list || []
      totalCount.value = res.total || 0
      hasMore.value = res.hasMore || false
      // 记录最新的日志ID用于增量更新
      if (logs.value.length > 0) {
        newestLogId = logs.value[logs.value.length - 1].id
      }
    }
  } catch (e) {
    console.error('Fetch initial logs error:', e)
  } finally {
    loading.value = false
  }
}

// 获取新日志（增量更新）
async function fetchNewLogs() {
  if (!newestLogId) return
  try {
    const res = await request.post('/worker/logs/history', {
      newerThan: newestLogId,
      search: searchKeyword.value,
      worker: filterWorker.value,
      level: filterLevel.value
    })
    if (res.code === 0 && res.list && res.list.length > 0) {
      logs.value.push(...res.list)
      totalCount.value = res.total || totalCount.value
      newestLogId = res.list[res.list.length - 1].id
    }
  } catch (e) {
    console.error('Fetch new logs error:', e)
    isConnected.value = false
  }
}

// 加载更多历史日志（向上滚动时）
async function loadMore() {
  if (loadingMore.value || !hasMore.value || logs.value.length === 0) return
  loadingMore.value = true
  
  const oldestLogId = logs.value[0].id
  const oldScrollHeight = logContainer.value?.scrollHeight || 0
  
  try {
    const res = await request.post('/worker/logs/history', {
      limit: 100,
      lastId: oldestLogId,
      search: searchKeyword.value,
      worker: filterWorker.value,
      level: filterLevel.value
    })
    if (res.code === 0 && res.list && res.list.length > 0) {
      logs.value.unshift(...res.list)
      hasMore.value = res.hasMore || false
      // 保持滚动位置
      nextTick(() => {
        if (logContainer.value) {
          const newScrollHeight = logContainer.value.scrollHeight
          logContainer.value.scrollTop = newScrollHeight - oldScrollHeight
        }
      })
    } else {
      hasMore.value = false
    }
  } catch (e) {
    console.error('Load more logs error:', e)
  } finally {
    loadingMore.value = false
  }
}

// 滚动到顶部时加载更多
function handleScroll() {
  if (!logContainer.value) return
  if (logContainer.value.scrollTop < 50 && hasMore.value && !loadingMore.value) {
    loadMore()
  }
}

// 重置并重新获取
function resetAndFetch() {
  logs.value = []
  newestLogId = ''
  hasMore.value = false
  fetchInitialLogs()
}

function toggleConnection() {
  if (isConnected.value) {
    stopPolling()
  } else {
    startPolling()
  }
}

async function clearLogs() {
  try {
    const res = await request.post('/worker/logs/clear')
    if (res.code === 0) {
      logs.value = []
      newestLogId = ''
      totalCount.value = 0
      hasMore.value = false
    }
  } catch (e) {
    console.error('Clear logs error:', e)
  }
}

async function handleExport(format) {
  try {
    ElMessage.info(t('worker.exportingLogs'))
    const response = await request.post('/worker/logs/export', {
      format,
      search: searchKeyword.value,
      worker: filterWorker.value,
      level: filterLevel.value
    }, { responseType: 'blob' })
    
    const blob = new Blob([response], { 
      type: format === 'json' ? 'application/json' : 
            format === 'csv' ? 'text/csv' : 'text/plain' 
    })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
    link.download = `worker-logs-${timestamp}.${format}`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)
    ElMessage.success(t('worker.exportSuccess'))
  } catch (e) {
    console.error('Export logs error:', e)
    ElMessage.error(t('worker.exportFailed'))
  }
}
</script>

<style lang="scss" scoped>
.worker-logs-page {
  .log-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    
    .log-filters {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: 5px;
    }
  }

  .log-stats {
    padding: 8px 0;
    font-size: 13px;
    color: hsl(var(--muted-foreground));
    border-bottom: 1px solid hsl(var(--border));
    margin-bottom: 10px;
  }

  .log-container {
    height: 600px;
    overflow-y: auto;
    background: hsl(var(--muted));
    border: 1px solid hsl(var(--border));
    border-radius: 4px;
    padding: 10px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 12px;
  }

  .log-loading {
    text-align: center;
    padding: 10px;
    color: var(--el-color-primary);
    font-size: 12px;
  }

  .log-item {
    padding: 2px 0;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;

    .log-time { color: hsl(var(--muted-foreground)); margin-right: 10px; }
    .log-level { display: inline-block; width: 60px; margin-right: 8px; font-weight: bold; }
    .log-worker { color: hsl(var(--primary)); margin-right: 8px; }
    .log-message { color: hsl(var(--foreground)); }

    &.log-info .log-level { color: var(--el-color-success); }
    &.log-warn .log-level { color: var(--el-color-warning); }
    &.log-error .log-level { color: var(--el-color-danger); }
    &.log-debug .log-level { color: var(--el-color-primary); }
  }

  .log-empty {
    color: hsl(var(--muted-foreground));
    text-align: center;
    padding: 50px;
  }
}
</style>
