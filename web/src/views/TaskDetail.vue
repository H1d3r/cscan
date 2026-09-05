<template>
  <div class="task-detail-page">
    <!-- 顶部工具栏 -->
    <div class="detail-toolbar">
      <el-button @click="goBack">
        <el-icon><ArrowLeft /></el-icon>
        <span>{{ $t('common.back') }}</span>
      </el-button>
      <div class="toolbar-title">
        <span class="title-text">{{ task.name || $t('task.taskDetail') }}</span>
        <el-tag v-if="task.id" :type="getStatusType(task.status, task)" effect="dark">
          {{ getStatusText(task) }}
        </el-tag>
      </div>
      <div class="toolbar-actions" v-if="task.id">
        <el-button v-if="task.status === 'CREATED' || !task.status" type="success" size="small" @click="handleStart">
          {{ $t('task.start') }}
        </el-button>
        <el-button v-if="['STARTED', 'PENDING'].includes(task.status)" type="warning" size="small" @click="handlePause">
          {{ $t('task.pause') }}
        </el-button>
        <el-button v-if="task.status === 'PAUSED'" type="success" size="small" @click="handleResume">
          {{ $t('task.resume') }}
        </el-button>
        <el-button
          v-if="['STARTED', 'PAUSED', 'PENDING', 'CREATED', ''].includes(task.status) && !['SUCCESS', 'FAILURE', 'STOPPED'].includes(task.status)"
          type="danger"
          size="small"
          @click="handleStop"
        >
          {{ $t('task.stop') }}
        </el-button>
        <el-button v-if="['SUCCESS', 'FAILURE', 'STOPPED'].includes(task.status)" type="warning" size="small" @click="handleRetry">
          {{ $t('task.retry') }}
        </el-button>
        <el-button size="small" @click="goEdit">{{ $t('task.edit') }}</el-button>
        <el-button size="small" @click="viewReport">{{ $t('task.report') }}</el-button>
        <el-button type="danger" size="small" @click="handleDelete">{{ $t('task.delete') }}</el-button>
      </div>
    </div>

    <!-- 加载骨架 -->
    <el-card v-if="loading && !task.id" shadow="never" class="section-card">
      <el-skeleton :rows="10" animated />
    </el-card>

    <!-- 任务不存在 -->
    <el-card v-else-if="notFound" shadow="never" class="section-card">
      <el-empty :description="$t('task.taskNotFound')">
        <el-button type="primary" @click="goBack">{{ $t('task.backToList') }}</el-button>
      </el-empty>
    </el-card>

    <!-- 详情主体 -->
    <template v-else-if="task.id">
      <!-- 概览卡片 -->
      <el-card class="section-card overview-card" shadow="never">
        <div class="detail-header">
          <div class="detail-header-main">
            <div class="task-title-row">
              <h3 class="task-title">{{ task.name }}</h3>
            </div>
            <div class="task-target">
              <el-icon><Aim /></el-icon>
              <span class="target-text">{{ task.target }}</span>
            </div>
            <div class="task-meta">
              <span v-if="task.profileName" class="meta-item">
                <el-icon><Document /></el-icon>{{ task.profileName }}
              </span>
            </div>
          </div>
          <div class="progress-circle-wrapper">
            <el-progress
              type="circle"
              :percentage="Math.min(task.progress || 0, 100)"
              :width="96"
              :stroke-width="8"
              :color="getProgressColor(task.status)"
            >
              <template #default="{ percentage }">
                <span class="progress-value">{{ percentage }}%</span>
              </template>
            </el-progress>
            <div class="subtask-info">
              {{ $t('task.subTask') }}: {{ task.subTaskDone || 0 }}/{{ task.subTaskCount || 0 }}
            </div>
          </div>
        </div>
      </el-card>

      <!-- 时间信息 -->
      <div class="time-cards">
        <div class="time-card">
          <el-icon class="time-icon"><Clock /></el-icon>
          <div class="time-content">
            <span class="time-label">{{ $t('common.createTime') }}</span>
            <span class="time-value">{{ task.createTime || '-' }}</span>
          </div>
        </div>
        <div class="time-card">
          <el-icon class="time-icon"><VideoPlay /></el-icon>
          <div class="time-content">
            <span class="time-label">{{ $t('task.startTime') }}</span>
            <span class="time-value">{{ task.startTime || '-' }}</span>
          </div>
        </div>
        <div class="time-card">
          <el-icon class="time-icon"><CircleCheck /></el-icon>
          <div class="time-content">
            <span class="time-label">{{ $t('task.endTime') }}</span>
            <span class="time-value">
              {{ ['SUCCESS', 'FAILURE', 'STOPPED'].includes(task.status) ? (task.endTime || '-') : '-' }}
            </span>
          </div>
        </div>
      </div>

      <!-- 扫描工作流 -->
      <el-card v-if="parsedConfig" class="section-card" shadow="never">
        <ScanWorkflow :config="parsedConfig" :current-phase="task.currentPhase" :status="task.status" />
      </el-card>

      <!-- 执行结果 -->
      <el-card v-if="task.result" class="section-card" shadow="never">
        <div class="section-title">
          <el-icon><Document /></el-icon>
          <span>{{ $t('task.executionResult') }}</span>
        </div>
        <div class="result-content">{{ task.result }}</div>
      </el-card>

      <!-- 任务日志 -->
      <el-card class="section-card" shadow="never">
        <div class="section-title log-section-title">
          <div class="title-left">
            <el-icon><Document /></el-icon>
            <span>{{ $t('task.taskLog') }}</span>
          </div>
          <div class="log-toolbar">
            <el-input
              v-model="logSearch"
              :placeholder="$t('task.searchLogs')"
              clearable
              size="small"
              style="width: 180px"
              @keyup.enter="loadTaskLogs"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-select v-model="logLevelFilter" size="small" style="width: 110px" @change="() => {}">
              <el-option :label="$t('task.allLevels')" value="all" />
              <el-option label="ERROR" value="ERROR" />
              <el-option label="WARN" value="WARN" />
              <el-option label="INFO" value="INFO" />
            </el-select>
            <el-checkbox v-model="includeDebug" size="small" @change="loadTaskLogs">
              {{ $t('common.includeDebug') }}
            </el-checkbox>
            <el-button size="small" type="primary" @click="loadTaskLogs" :loading="logLoading">
              <el-icon style="margin-right: 4px"><Refresh /></el-icon>
              {{ $t('task.refreshLogs') }}
            </el-button>
            <span class="log-count">{{ $t('task.totalLogs', { count: filteredLogs.length }) }}</span>
          </div>
        </div>
        <div v-if="logLoading && !taskLogs.length" class="log-skeleton">
          <el-skeleton :rows="5" animated />
        </div>
        <div v-else-if="!filteredLogs.length" class="log-empty">
          <el-empty :description="$t('task.noLogs')" :image-size="60" />
        </div>
        <div v-else ref="logBox" class="log-box">
          <div
            v-for="(l, idx) in filteredLogs"
            :key="idx"
            class="log-line"
            :class="logLineClass(l)"
          >
            <span class="log-ln">{{ idx + 1 }}</span>
            <span class="log-time">{{ formatLogTime(l.timestamp) }}</span>
            <span class="log-level" :class="logLevelClass(l.level)">{{ l.level || 'LOG' }}</span>
            <span v-if="l.workerName" class="log-worker">{{ l.workerName }}</span>
            <span class="log-body">{{ l.message }}</span>
          </div>
        </div>
      </el-card>

      <!-- 扫描配置概览 -->
      <el-card v-if="parsedConfig" class="section-card" shadow="never">
        <div class="section-title">
          <el-icon><Setting /></el-icon>
          <span>{{ $t('task.scanConfig') }}</span>
        </div>

        <!-- 策略概览 -->
        <div class="strategy-overview">
          <div class="strategy-card">
            <div class="strategy-header">
              <el-icon class="strategy-icon"><Operation /></el-icon>
              <span class="strategy-title">{{ $t('task.scanStrategy') }}</span>
            </div>
            <div class="strategy-stats">
              <div class="stat-item">
                <span class="stat-label">{{ $t('task.enabledModules') }}</span>
                <span class="stat-value">{{ enabledModulesCount }}/8</span>
              </div>
              <div class="stat-item">
                <span class="stat-label">{{ $t('task.taskSplit') }}</span>
                <span class="stat-value">{{ parsedConfig.batchSize > 0 ? parsedConfig.batchSize : $t('task.autoCalc') }}</span>
              </div>
              <div class="stat-item">
                <span class="stat-label">{{ $t('task.currentPhase') }}</span>
                <span class="stat-value">{{ task.currentPhase || '-' }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 模块开关状态 -->
        <div class="module-grid">
          <div class="module-card" :class="{ active: parsedConfig.domainscan?.enable }">
            <el-icon class="module-icon"><Connection /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.subdomainScan') }}</span>
              <div class="module-details" v-if="parsedConfig.domainscan?.enable">
                <span class="detail-item">{{ parsedConfig.domainscan?.subfinder !== false ? 'Subfinder' : '' }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.domainscan?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.domainscan?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.portscan?.enable !== false }">
            <el-icon class="module-icon"><Monitor /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.portScan') }}</span>
              <div class="module-details" v-if="parsedConfig.portscan?.enable !== false">
                <span class="detail-item">{{ parsedConfig.portscan?.tool || 'naabu' }}</span>
                <span class="detail-item">{{ parsedConfig.portscan?.ports || 'top100' }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.portscan?.enable !== false ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.portscan?.enable !== false ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.portidentify?.enable }">
            <el-icon class="module-icon"><Search /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.portIdentify') }}</span>
              <div class="module-details" v-if="parsedConfig.portidentify?.enable">
                <span class="detail-item">{{ parsedConfig.portidentify?.tool || 'nmap' }}</span>
                <span class="detail-item">{{ parsedConfig.portidentify?.timeout || 60 }}s</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.portidentify?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.portidentify?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.fingerprint?.enable }">
            <el-icon class="module-icon"><Stamp /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.fingerprintScan') }}</span>
              <div class="module-details" v-if="parsedConfig.fingerprint?.enable">
                <span class="detail-item">{{ parsedConfig.fingerprint?.tool === 'httpx' ? 'Httpx' : 'Wappalyzer' }}</span>
                <span class="detail-item" v-if="parsedConfig.fingerprint?.screenshot">{{ $t('task.screenshot') }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.fingerprint?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.fingerprint?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.brutescan?.enable }">
            <el-icon class="module-icon"><Key /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.weakpassScan') }}</span>
              <div class="module-details" v-if="parsedConfig.brutescan?.enable">
                <span class="detail-item" v-if="parsedConfig.brutescan?.services?.length">{{ parsedConfig.brutescan.services.join(', ') }}</span>
                <span class="detail-item" v-else>{{ $t('task.allServices') }}</span>
                <span class="detail-item">{{ parsedConfig.brutescan?.threads || 20 }} {{ $t('task.threads') }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.brutescan?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.brutescan?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.pocscan?.enable }">
            <el-icon class="module-icon"><WarnTriangleFilled /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.vulScan') }}</span>
              <div class="module-details" v-if="parsedConfig.pocscan?.enable">
                <span class="detail-item">Nuclei</span>
                <span class="detail-item">{{ parsedConfig.pocscan?.severity || 'critical,high,medium' }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.pocscan?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.pocscan?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.dirscan?.enable }">
            <el-icon class="module-icon"><FolderOpened /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.dirScan') }}</span>
              <div class="module-details" v-if="parsedConfig.dirscan?.enable">
                <span class="detail-item">{{ parsedConfig.dirscan?.tool || 'dirsearch' }}</span>
                <span class="detail-item">{{ parsedConfig.dirscan?.threads || 10 }} {{ $t('task.threads') }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.dirscan?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.dirscan?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.jsfinder?.enable }">
            <el-icon class="module-icon"><Connection /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.jsfinderScan') }}</span>
              <div class="module-details" v-if="parsedConfig.jsfinder?.enable">
                <span class="detail-item">JSFinder</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.jsfinder?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.jsfinder?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
        </div>
      </el-card>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowLeft, Clock, VideoPlay, CircleCheck, Document, Setting,
  Connection, Monitor, Stamp, WarnTriangleFilled, FolderOpened,
  Aim, Operation, Key, Search, Refresh
} from '@element-plus/icons-vue'
import ScanWorkflow from '@/components/ScanWorkflow.vue'
import {
  getTaskDetail, startTask, pauseTask, resumeTask, stopTask,
  retryTask, deleteTask, getTaskLogs
} from '@/api/task'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const task = ref({})
const loading = ref(false)
const notFound = ref(false)
let refreshTimer = null

// 日志状态
const taskLogs = ref([])
const logLoading = ref(false)
const logSearch = ref('')
const logLevelFilter = ref('all')
const includeDebug = ref(false)
const logBox = ref(null)

const taskId = computed(() => route.query.id || '')

// 解析任务配置
const parsedConfig = computed(() => {
  if (!task.value?.config) return null
  try {
    return JSON.parse(task.value.config)
  } catch (e) {
    return null
  }
})

// 启用模块数量
const enabledModulesCount = computed(() => {
  if (!parsedConfig.value) return 0
  let count = 0
  if (parsedConfig.value.domainscan?.enable) count++
  if (parsedConfig.value.portscan?.enable !== false) count++
  if (parsedConfig.value.portidentify?.enable) count++
  if (parsedConfig.value.fingerprint?.enable) count++
  if (parsedConfig.value.brutescan?.enable) count++
  if (parsedConfig.value.pocscan?.enable) count++
  if (parsedConfig.value.dirscan?.enable) count++
  if (parsedConfig.value.jsfinder?.enable) count++
  return count
})

// 是否需要自动刷新（执行中/等待中）
const shouldAutoRefresh = computed(() => {
  return ['STARTED', 'PENDING'].includes(task.value?.status)
})

// 日志过滤（客户端级别过滤，搜索由后端处理）
const filteredLogs = computed(() => {
  const lf = logLevelFilter.value
  return taskLogs.value.filter(l => {
    if (lf !== 'all' && (l.level || '').toUpperCase() !== lf) return false
    return true
  })
})

// 加载任务日志（使用 task.taskId UUID，而非 MongoDB _id）
async function loadTaskLogs() {
  const logTaskId = task.value?.taskId
  if (!logTaskId) return
  logLoading.value = true
  try {
    const res = await getTaskLogs({
      taskId: logTaskId,
      limit: 500,
      search: logSearch.value.trim(),
      includeDebug: includeDebug.value
    })
    if (res.code === 0 && res.list) {
      taskLogs.value = res.list
    } else {
      taskLogs.value = []
    }
  } catch (e) {
    console.error('load task logs failed:', e)
  } finally {
    logLoading.value = false
  }
}

let logSearchTimer = null
watch(logSearch, () => {
  if (logSearchTimer) clearTimeout(logSearchTimer)
  logSearchTimer = setTimeout(() => {
    loadTaskLogs()
  }, 500)
})

function logLineClass(l) {
  return {
    'log-error': l.level === 'ERROR' || l.level === 'FATAL',
    'log-warn': l.level === 'WARN',
    'log-debug': l.level === 'DEBUG'
  }
}

function logLevelClass(level) {
  if (!level) return ''
  const map = { ERROR: 'lvl-error', FATAL: 'lvl-error', WARN: 'lvl-warn', INFO: 'lvl-info', DEBUG: 'lvl-debug' }
  return map[level.toUpperCase()] || ''
}

function formatLogTime(ts) {
  if (!ts) return '--:--:--'
  // ISO 格式 → MM-DD HH:MM:SS
  if (ts.includes('T')) {
    const parts = ts.split('T')
    if (parts.length > 1) {
      return `${parts[0].substring(5)} ${parts[1].split('.')[0]}`
    }
  }
  return ts
}

function getStatusType(status, row) {
  const map = { CREATED: 'info', PENDING: 'warning', STARTED: 'primary', PAUSED: 'warning', SUCCESS: 'success', FAILURE: 'danger', STOPPED: 'info', REVOKED: 'info' }
  if (status && map[status]) return map[status]
  if (!status && row) {
    if (row.progress >= 100 || (row.subTaskCount > 0 && row.subTaskDone >= row.subTaskCount)) return 'success'
    if (row.progress > 0 || row.subTaskDone > 0) return 'primary'
    return 'info'
  }
  return 'info'
}

function getProgressColor(status) {
  const root = document.documentElement
  const getVar = (name) => getComputedStyle(root).getPropertyValue(name).trim()
  const colorMap = {
    CREATED: getVar('--status-info') || '#909399',
    PENDING: getVar('--status-warning') || '#E6A23C',
    STARTED: getVar('--status-primary') || '#409EFF',
    PAUSED: getVar('--status-warning') || '#E6A23C',
    SUCCESS: getVar('--status-success') || '#67C23A',
    FAILURE: getVar('--status-danger') || '#F56C6C',
    STOPPED: getVar('--status-info') || '#909399',
    REVOKED: getVar('--status-info') || '#909399'
  }
  return colorMap[status] || getVar('--status-primary') || '#409EFF'
}

function getStatusText(row) {
  const statusMap = {
    CREATED: t('task.created'),
    PENDING: t('task.pendingExec'),
    STARTED: t('task.executing'),
    PAUSED: t('task.paused'),
    PARTIAL: t('task.completed'),
    SUCCESS: t('task.completed'),
    FAILURE: t('task.execFailed'),
    STOPPED: t('task.stopped'),
    REVOKED: t('task.revoked')
  }
  if (row?.status && statusMap[row.status]) return statusMap[row.status]
  if (!row?.status) {
    if (row?.progress >= 100 || (row?.subTaskCount > 0 && row?.subTaskDone >= row?.subTaskCount)) return t('task.completed')
    if (row?.progress > 0 || row?.subTaskDone > 0) return t('task.executing')
    return t('task.created')
  }
  return row?.status || t('task.unknown')
}

async function loadDetail() {
  if (!taskId.value) {
    notFound.value = true
    return
  }
  loading.value = true
  try {
    const res = await getTaskDetail({ id: taskId.value })
    if (res.code === 0 && res.data) {
      task.value = res.data
      notFound.value = false
    } else if (res.code === 404) {
      notFound.value = true
      task.value = {}
    } else {
      ElMessage.error(res.msg || t('task.taskNotFound'))
      notFound.value = true
    }
  } catch (e) {
    console.error('load task detail failed:', e)
    ElMessage.error((e && e.message) || t('task.taskNotFound'))
    notFound.value = true
  } finally {
    loading.value = false
  }
  // 根据状态启停自动刷新
  scheduleAutoRefresh()
}

function scheduleAutoRefresh() {
  stopAutoRefresh()
  if (shouldAutoRefresh.value) {
    refreshTimer = setInterval(() => {
      loadDetail()
      loadTaskLogs()
    }, 5000)
  }
}

function stopAutoRefresh() {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

function goBack() {
  router.push('/task')
}

function goEdit() {
  router.push({ path: '/task/create', query: { id: task.value.id } })
}

function viewReport() {
  router.push({ path: '/report', query: { taskId: task.value.id } })
}

async function handleStart() {
  const res = await startTask({ id: task.value.id })
  if (res.code === 0) {
    ElMessage.success(t('task.taskStarted'))
    await loadDetail()
    setTimeout(() => loadDetail(), 2000)
  } else {
    ElMessage.error(res.msg)
  }
}

async function handlePause() {
  await ElMessageBox.confirm(t('task.confirmPause'), t('common.tip'), { type: 'warning' })
  const res = await pauseTask({ id: task.value.id })
  if (res.code === 0) {
    ElMessage.success(t('task.taskPaused'))
    await loadDetail()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleResume() {
  const res = await resumeTask({ id: task.value.id })
  if (res.code === 0) {
    ElMessage.success(t('task.taskResumed'))
    await loadDetail()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleStop() {
  await ElMessageBox.confirm(t('task.confirmStop'), t('common.tip'), { type: 'warning' })
  const res = await stopTask({ id: task.value.id })
  if (res.code === 0) {
    ElMessage.success(t('task.taskStopped'))
    await loadDetail()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleRetry() {
  await ElMessageBox.confirm(t('task.confirmRetry'), t('common.tip'), { type: 'warning' })
  const res = await retryTask({ id: task.value.id })
  if (res.code === 0) {
    ElMessage.success(res.msg || t('task.newTaskCreated'))
    await loadDetail()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleDelete() {
  await ElMessageBox.confirm(t('task.confirmDeleteTask'), t('common.tip'), { type: 'warning' })
  const res = await deleteTask({ id: task.value.id })
  if (res.code === 0) {
    ElMessage.success(t('task.deleteSuccess'))
    stopAutoRefresh()
    router.push('/task')
  } else {
    ElMessage.error(res.msg)
  }
}

onMounted(async () => {
  await loadDetail()
  loadTaskLogs()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style lang="scss" scoped>
.task-detail-page {
  padding: 16px 20px;

  --log-bg: #1a1b26;
  --log-line-odd-bg: rgba(255, 255, 255, 0.02);
  --log-line-hover-bg: rgba(255, 255, 255, 0.06);
  --log-ln-color: #565f89;
  --log-time-color: #7aa2f7;
  --log-body-color: #c0caf5;
  --log-worker-color: #7aa2f7;
  --log-worker-bg: rgba(122, 162, 247, 0.1);
  --lvl-error-color: #fff;
  --lvl-error-bg: rgba(247, 118, 142, 0.8);
  --lvl-warn-color: #1a1b26;
  --lvl-warn-bg: rgba(224, 175, 104, 0.85);
  --lvl-info-color: #9ece6a;
  --lvl-info-bg: rgba(158, 206, 106, 0.12);
  --lvl-debug-color: #565f89;
  --lvl-debug-bg: rgba(86, 95, 137, 0.15);
  --log-error-color: #f7768e;
  --log-warn-color: #e0af68;
  --log-debug-color: #565f89;

  .detail-toolbar {
    display: flex;
    align-items: center;
    gap: 16px;
    margin-bottom: 16px;
    flex-wrap: wrap;

    .toolbar-title {
      display: flex;
      align-items: center;
      gap: 10px;
      flex: 1;
      min-width: 200px;

      .title-text {
        font-size: 18px;
        font-weight: 600;
        color: var(--el-text-color-primary);
      }
    }

    .toolbar-actions {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }
  }

  .section-card {
    margin-bottom: 16px;
  }

  .overview-card {
    .detail-header {
      display: flex;
      align-items: center;
      gap: 24px;
      flex-wrap: wrap;

      .detail-header-main {
        flex: 1;
        min-width: 280px;

        .task-title {
          margin: 0;
          font-size: 20px;
          font-weight: 600;
          color: var(--el-text-color-primary);
        }

        .task-target {
          display: flex;
          align-items: center;
          gap: 6px;
          margin-top: 10px;
          color: var(--el-text-color-regular);

          .target-text {
            word-break: break-all;
          }
        }

        .task-meta {
          display: flex;
          align-items: center;
          gap: 18px;
          margin-top: 10px;
          flex-wrap: wrap;

          .meta-item {
            display: inline-flex;
            align-items: center;
            gap: 4px;
            font-size: 13px;
            color: var(--el-text-color-secondary);
          }
        }
      }

      .progress-circle-wrapper {
        text-align: center;

        .progress-value {
          font-size: 18px;
          font-weight: 600;
          color: var(--el-text-color-primary);
        }

        .subtask-info {
          margin-top: 6px;
          font-size: 12px;
          color: var(--el-text-color-secondary);
        }
      }
    }
  }

  .time-cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 12px;
    margin-bottom: 16px;

    .time-card {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 14px 16px;
      background: var(--el-fill-color-light);
      border-radius: 8px;
      border: 1px solid var(--el-border-color-lighter);

      .time-icon {
        font-size: 22px;
        color: var(--el-color-primary);
      }

      .time-content {
        display: flex;
        flex-direction: column;

        .time-label {
          font-size: 12px;
          color: var(--el-text-color-secondary);
        }

        .time-value {
          font-size: 14px;
          color: var(--el-text-color-primary);
          margin-top: 2px;
        }
      }
    }
  }

  .section-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 15px;
    font-weight: 600;
    color: var(--el-text-color-primary);
    margin-bottom: 14px;
  }

  .result-content {
    padding: 12px 14px;
    background: var(--el-fill-color-light);
    border-radius: 6px;
    color: var(--el-text-color-regular);
    white-space: pre-wrap;
    word-break: break-all;
    font-size: 13px;
    line-height: 1.6;
  }

  // 日志区域
  .log-section-title {
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;

    .title-left {
      display: flex;
      align-items: center;
      gap: 8px;
    }
  }

  .log-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;

    .log-count {
      font-size: 12px;
      color: var(--el-text-color-secondary);
      white-space: nowrap;
    }
  }

  .log-skeleton {
    padding: 16px;
  }

  .log-empty {
    padding: 20px 0;
  }

  .log-box {
    max-height: 400px;
    overflow-y: auto;
    padding: 8px 0;
    font-family: 'Cascadia Code', 'JetBrains Mono', 'Consolas', 'Menlo', monospace;
    font-size: 12px;
    line-height: 1.8;
    background: var(--log-bg);
    border-radius: 6px;
  }

  .log-line {
    display: flex;
    align-items: baseline;
    gap: 0;
    padding: 2px 12px 2px 0;
    transition: background 0.1s;
    &:nth-child(odd) { background: var(--log-line-odd-bg); }
    &:hover { background: var(--log-line-hover-bg); }
  }

  .log-ln {
    display: inline-block;
    width: 42px;
    min-width: 42px;
    text-align: right;
    padding-right: 10px;
    color: var(--log-ln-color);
    font-size: 11px;
    user-select: none;
    flex-shrink: 0;
  }

  .log-time {
    color: var(--log-time-color);
    font-size: 11px;
    margin-right: 8px;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .log-level {
    display: inline-block;
    min-width: 44px;
    padding: 0 5px;
    margin-right: 6px;
    text-align: center;
    font-size: 10px;
    font-weight: 600;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .lvl-error { color: var(--lvl-error-color); background: var(--lvl-error-bg); }
  .lvl-warn { color: var(--lvl-warn-color); background: var(--lvl-warn-bg); }
  .lvl-info { color: var(--lvl-info-color); background: var(--lvl-info-bg); }
  .lvl-debug { color: var(--lvl-debug-color); background: var(--lvl-debug-bg); }

  .log-worker {
    display: inline-block;
    padding: 0 5px;
    margin-right: 6px;
    font-size: 11px;
    color: var(--log-worker-color);
    background: var(--log-worker-bg);
    border-radius: 3px;
    flex-shrink: 0;
  }

  .log-body {
    color: var(--log-body-color);
    word-break: break-all;
    white-space: pre-wrap;
    flex: 1;
    min-width: 0;
  }

  .log-error .log-body { color: var(--log-error-color); }
  .log-warn .log-body { color: var(--log-warn-color); }
  .log-debug .log-body { color: var(--log-debug-color); }

  .strategy-overview {
    margin-bottom: 16px;

    .strategy-card {
      padding: 14px 16px;
      background: var(--el-fill-color-light);
      border-radius: 8px;
      border: 1px solid var(--el-border-color-lighter);

      .strategy-header {
        display: flex;
        align-items: center;
        gap: 8px;
        margin-bottom: 12px;

        .strategy-icon {
          font-size: 18px;
          color: var(--el-color-primary);
        }

        .strategy-title {
          font-weight: 600;
          color: var(--el-text-color-primary);
        }
      }

      .strategy-stats {
        display: flex;
        gap: 28px;
        flex-wrap: wrap;

        .stat-item {
          display: flex;
          flex-direction: column;

          .stat-label {
            font-size: 12px;
            color: var(--el-text-color-secondary);
          }

          .stat-value {
            font-size: 16px;
            font-weight: 600;
            color: var(--el-text-color-primary);
            margin-top: 2px;
          }
        }
      }
    }
  }

  .module-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: 12px;

    .module-card {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 12px 14px;
      background: var(--el-fill-color-light);
      border-radius: 8px;
      border: 1px solid var(--el-border-color-lighter);
      transition: all 0.2s;

      &.active {
        border-color: var(--el-color-primary-light-5);
        background: var(--el-color-primary-light-9);
      }

      .module-icon {
        font-size: 24px;
        color: var(--el-text-color-secondary);
      }

      &.active .module-icon {
        color: var(--el-color-primary);
      }

      .module-info {
        flex: 1;
        min-width: 0;

        .module-name {
          font-size: 14px;
          font-weight: 500;
          color: var(--el-text-color-primary);
        }

        .module-details {
          display: flex;
          gap: 10px;
          margin-top: 4px;
          flex-wrap: wrap;

          .detail-item {
            font-size: 12px;
            color: var(--el-text-color-secondary);
          }
        }
      }
    }
  }
}

:global(html:not(.dark) .task-detail-page) {
  --log-bg: #f8f9fc;
  --log-line-odd-bg: rgba(0, 0, 0, 0.02);
  --log-line-hover-bg: rgba(0, 0, 0, 0.05);
  --log-ln-color: #9aa0b8;
  --log-time-color: #3b6ff5;
  --log-body-color: #343b58;
  --log-worker-color: #3b6ff5;
  --log-worker-bg: rgba(59, 111, 245, 0.08);
  --lvl-error-color: #fff;
  --lvl-error-bg: #f56c6c;
  --lvl-warn-color: #fff;
  --lvl-warn-bg: #e6a23c;
  --lvl-info-color: #2d7d2d;
  --lvl-info-bg: rgba(103, 194, 58, 0.15);
  --lvl-debug-color: #606266;
  --lvl-debug-bg: rgba(144, 147, 153, 0.12);
  --log-error-color: #c64343;
  --log-warn-color: #8f5e15;
  --log-debug-color: #909399;
}
</style>
