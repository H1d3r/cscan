<template>
  <div class="worker-console">
    <el-page-header @back="goBack" :title="$t('workerConsole.backToWorkerList')">
      <template #content>
        <span class="worker-title">{{ workerName }} - {{ $t('workerConsole.probeConsole') }}</span>
        <el-tag :type="workerStatus === 'running' ? 'success' : 'danger'" style="margin-left: 10px">
          {{ workerStatus === 'running' ? $t('workerConsole.online') : $t('workerConsole.offline') }}
        </el-tag>
      </template>
    </el-page-header>

    <el-tabs v-model="activeTab" type="border-card" style="margin-top: 20px">
      <!-- 系统信息 -->
      <el-tab-pane :label="$t('workerConsole.systemInfo')" name="info">
        <div v-loading="infoLoading">
          <el-descriptions :column="2" border v-if="workerInfo">
            <el-descriptions-item :label="$t('workerConsole.workerName')">{{ workerInfo.name }}</el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.ipAddress')">{{ workerInfo.ip }}</el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.os')">{{ workerInfo.os }}</el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.arch')">{{ workerInfo.arch }}</el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.version')">{{ workerInfo.version || '-' }}</el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.uptime')">{{ formatUptime(workerInfo.uptime) }}</el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.cpuUsage')">
              <el-progress :percentage="Math.round(workerInfo.cpuLoad || 0)" :color="getLoadColor(workerInfo.cpuLoad)" />
            </el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.memUsage')">
              <el-progress :percentage="getMemPercent(workerInfo)" :color="getLoadColor(getMemPercent(workerInfo))" />
              <span class="secondary-text">
                {{ formatBytes(workerInfo.memUsed) }} / {{ formatBytes(workerInfo.memTotal) }}
              </span>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.diskUsage')">
              <el-progress :percentage="getDiskPercent(workerInfo)" :color="getLoadColor(getDiskPercent(workerInfo))" />
              <span class="secondary-text">
                {{ formatBytes(workerInfo.diskUsed) }} / {{ formatBytes(workerInfo.diskTotal) }}
              </span>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('workerConsole.taskStats')">
              {{ $t('workerConsole.executed') }} {{ workerInfo.taskStarted || 0 }} | {{ $t('workerConsole.running') }} {{ workerInfo.taskRunning || 0 }}
            </el-descriptions-item>
          </el-descriptions>

          <el-empty v-if="!workerInfo" :description="$t('workerConsole.cannotGetWorkerInfo')" />
        </div>
      </el-tab-pane>

      <!-- 文件管理 -->
      <el-tab-pane :label="$t('workerConsole.fileManagement')" name="files">
        <div class="file-manager">
          <div class="file-toolbar">
            <el-breadcrumb separator="/">
              <el-breadcrumb-item 
                v-for="(part, index) in pathParts" 
                :key="index"
                @click="navigateToPath(index)"
                class="path-item"
              >
                {{ part || $t('workerConsole.rootDir') }}
              </el-breadcrumb-item>
            </el-breadcrumb>
            <div class="file-actions">
              <el-button size="small" @click="refreshFiles" :loading="filesLoading">
                <el-icon><Refresh /></el-icon>{{ $t('workerConsole.refresh') }}
              </el-button>
              <el-button size="small" type="primary" @click="showCreateDirDialog">
                <el-icon><FolderAdd /></el-icon>{{ $t('workerConsole.newFolder') }}
              </el-button>
              <el-upload
                :show-file-list="false"
                :before-upload="handleUpload"
                :disabled="filesLoading"
              >
                <el-button size="small" type="success">
                  <el-icon><Upload /></el-icon>{{ $t('workerConsole.uploadFile') }}
                </el-button>
              </el-upload>
            </div>
          </div>

          <el-table :data="fileList" v-loading="filesLoading" @row-dblclick="handleFileClick" stripe>
            <el-table-column prop="name" :label="$t('workerConsole.name')" min-width="200">
              <template #default="{ row }">
                <span class="file-name" :class="{ 'is-dir': row.isDir }">
                  <el-icon v-if="row.isDir"><Folder /></el-icon>
                  <el-icon v-else><Document /></el-icon>
                  {{ row.name }}
                </span>
              </template>
            </el-table-column>
            <el-table-column prop="size" :label="$t('workerConsole.size')" width="120">
              <template #default="{ row }">
                {{ row.isDir ? '-' : formatBytes(row.size) }}
              </template>
            </el-table-column>
            <el-table-column prop="mode" :label="$t('workerConsole.permission')" width="120" />
            <el-table-column prop="modTime" :label="$t('workerConsole.modifyTime')" width="180">
              <template #default="{ row }">
                {{ formatTime(row.modTime) }}
              </template>
            </el-table-column>
            <el-table-column :label="$t('common.operation')" width="150" fixed="right">
              <template #default="{ row }">
                <el-button v-if="!row.isDir" size="small" type="primary" link @click="downloadFileHandler(row)">
                  {{ $t('workerConsole.download') }}
                </el-button>
                <el-popconfirm
                  :title="$t('workerConsole.confirmDelete', { name: row.name })"
                  @confirm="deleteFileHandler(row)"
                >
                  <template #reference>
                    <el-button size="small" type="danger" link>{{ $t('workerConsole.delete') }}</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <!-- 终端 -->
      <el-tab-pane :label="$t('workerConsole.terminal')" name="terminal">
        <div class="terminal-container">
          <div class="terminal-toolbar">
            <el-button size="small" type="primary" @click="connectTerminal" :disabled="terminalConnected">
              {{ terminalConnected ? $t('workerConsole.connected') : $t('workerConsole.connectTerminal') }}
            </el-button>
            <el-button size="small" @click="disconnectTerminal" :disabled="!terminalConnected">
              {{ $t('workerConsole.disconnect') }}
            </el-button>
            <el-button size="small" @click="clearTerminal">{{ $t('workerConsole.clearScreen') }}</el-button>
            <span v-if="terminalConnected" class="terminal-status connected">
              <el-icon><CircleCheck /></el-icon> {{ $t('workerConsole.terminalConnected') }}
            </span>
          </div>
          <div ref="terminalRef" class="terminal-output"></div>
          <div class="terminal-input" v-if="terminalConnected">
            <span class="prompt">$</span>
            <el-input
              v-model="terminalInput"
              :placeholder="$t('workerConsole.enterCommand')"
              @keyup.enter="sendCommand"
              :disabled="!terminalConnected"
            />
          </div>
        </div>
      </el-tab-pane>

      <!-- 审计日志 -->
      <el-tab-pane :label="$t('workerConsole.auditLog')" name="audit">
        <div style="margin-bottom: 15px; display: flex; justify-content: flex-end;">
          <el-popconfirm
            :title="$t('workerConsole.confirmClearLog')"
            :confirm-button-text="$t('common.confirm')"
            :cancel-button-text="$t('common.cancel')"
            @confirm="clearAuditLogsHandler"
          >
            <template #reference>
              <el-button type="danger" size="small" :loading="auditClearing">{{ $t('workerConsole.clearLog') }}</el-button>
            </template>
          </el-popconfirm>
        </div>
        <el-table :data="auditLogs" v-loading="auditLoading" stripe>
          <el-table-column prop="createTime" :label="$t('workerConsole.time')" width="180">
            <template #default="{ row }">
              {{ formatAuditTime(row.createTime) }}
            </template>
          </el-table-column>
          <el-table-column prop="username" :label="$t('workerConsole.operator')" width="120">
            <template #default="{ row }">
              {{ row.username || row.clientIp || '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="type" :label="$t('workerConsole.actionType')" width="140">
            <template #default="{ row }">
              <el-tag :type="getActionType(row.type)" size="small">{{ getActionLabel(row.type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="$t('workerConsole.actionTarget')" min-width="200">
            <template #default="{ row }">
              {{ row.path || row.command || row.sessionId || '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="success" :label="$t('workerConsole.actionResult')" width="100">
            <template #default="{ row }">
              <el-tag :type="row.success ? 'success' : 'danger'" size="small">
                {{ row.success ? $t('workerConsole.success') : $t('workerConsole.failed') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="$t('workerConsole.detail')" min-width="200" show-overflow-tooltip>
            <template #default="{ row }">
              <span v-if="row.error" class="error-text">{{ row.error }}</span>
              <span v-else-if="row.duration">{{ $t('workerConsole.duration') }} {{ row.duration }}ms</span>
              <span v-else class="secondary-text">-</span>
            </template>
          </el-table-column>
        </el-table>
        <el-pagination
          v-model:current-page="auditPage"
          :page-size="20"
          :total="auditTotal"
          layout="total, prev, pager, next"
          @current-change="loadAuditLogs"
          style="margin-top: 15px; justify-content: flex-end"
        />
      </el-tab-pane>
    </el-tabs>

    <!-- 新建文件夹对话框 -->
    <el-dialog v-model="createDirDialogVisible" :title="$t('workerConsole.createFolder')" width="400px">
      <el-input v-model="newDirName" :placeholder="$t('workerConsole.enterFolderName')" />
      <template #footer>
        <el-button @click="createDirDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="createDirHandler" :loading="createDirLoading">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { 
  Refresh, FolderAdd, Upload, Folder, Document, CircleCheck 
} from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { 
  getWorkerInfo, listFiles, uploadFile, downloadFile, deleteFile, createDir,
  openTerminal, closeTerminal, execCommand, getAuditLogs, clearAuditLogs 
} from '@/api/worker'
import { useUserStore } from '@/stores/user'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const workerName = ref('')
const workerStatus = ref('offline')
const activeTab = ref('info')

// 系统信息
const workerInfo = ref(null)
const infoLoading = ref(false)

// 文件管理
const currentPath = ref('.')
const fileList = ref([])
const filesLoading = ref(false)
const createDirDialogVisible = ref(false)
const newDirName = ref('')
const createDirLoading = ref(false)

// 终端
const terminalRef = ref(null)
const terminalConnected = ref(false)
const terminalInput = ref('')
const terminalOutput = ref([])
const terminalSessionId = ref('')
let terminalWs = null

// 审计日志
const auditLogs = ref([])
const auditLoading = ref(false)
const auditPage = ref(1)
const auditClearing = ref(false)
const auditTotal = ref(0)

const pathParts = computed(() => {
  if (!currentPath.value || currentPath.value === '.') return ['']
  return ['', ...currentPath.value.split('/').filter(p => p && p !== '.')]
})

onMounted(() => {
  workerName.value = route.params.name || route.query.name
  if (!workerName.value) {
    ElMessage.error(t('workerConsole.workerNotSpecified'))
    router.push('/worker')
    return
  }
  loadWorkerInfo()
})

onUnmounted(() => {
  disconnectTerminal()
})

watch(activeTab, (tab) => {
  if (tab === 'files') {
    // 文件管理：首次加?
    if (fileList.value.length === 0) {
      loadFiles()
    }
  } else if (tab === 'audit') {
    // 审计日志：每次切换都刷新
    loadAuditLogs()
  }
})

function goBack() {
  router.push('/worker')
}

async function loadWorkerInfo() {
  infoLoading.value = true
  try {
    const res = await getWorkerInfo(workerName.value)
    if (res.code === 0) {
      workerInfo.value = res.data
      workerStatus.value = 'running'
    } else {
      workerStatus.value = 'offline'
      ElMessage.warning(res.message || t('workerConsole.getWorkerInfoFailed'))
    }
  } catch (e) {
    workerStatus.value = 'offline'
    ElMessage.error(t('workerConsole.getWorkerInfoFailed') + ': ' + e.message)
  } finally {
    infoLoading.value = false
  }
}

async function loadFiles() {
  filesLoading.value = true
  try {
    const res = await listFiles(workerName.value, currentPath.value)
    if (res.code === 0 && res.data) {
      fileList.value = res.data.files || []
    } else {
      ElMessage.error(res.message || t('workerConsole.getFileListFailed'))
    }
  } catch (e) {
    ElMessage.error(t('workerConsole.getFileListFailed') + ': ' + e.message)
  } finally {
    filesLoading.value = false
  }
}

function refreshFiles() {
  loadFiles()
}

function handleFileClick(row) {
  if (row.isDir) {
    currentPath.value = currentPath.value === '.' 
      ? row.name 
      : `${currentPath.value}/${row.name}`
    loadFiles()
  }
}

function navigateToPath(index) {
  if (index === 0) {
    currentPath.value = '.'
  } else {
    currentPath.value = pathParts.value.slice(1, index + 1).join('/')
  }
  loadFiles()
}

function showCreateDirDialog() {
  newDirName.value = ''
  createDirDialogVisible.value = true
}

async function createDirHandler() {
  if (!newDirName.value.trim()) {
    ElMessage.warning(t('workerConsole.enterFolderName'))
    return
  }
  createDirLoading.value = true
  try {
    const path = currentPath.value === '.' 
      ? newDirName.value 
      : `${currentPath.value}/${newDirName.value}`
    const res = await createDir(workerName.value, path)
    if (res.code === 0) {
      ElMessage.success(t('workerConsole.createSuccess'))
      createDirDialogVisible.value = false
      loadFiles()
    } else {
      ElMessage.error(res.message || t('workerConsole.createFailed'))
    }
  } catch (e) {
    ElMessage.error(t('workerConsole.createFailed') + ': ' + e.message)
  } finally {
    createDirLoading.value = false
  }
}

async function handleUpload(file) {
  try {
    const res = await uploadFile(workerName.value, currentPath.value, file)
    if (res.code === 0) {
      ElMessage.success(t('workerConsole.uploadSuccess'))
      loadFiles()
    } else {
      ElMessage.error(res.message || t('workerConsole.uploadFailed'))
    }
  } catch (e) {
    ElMessage.error(t('workerConsole.uploadFailed') + ': ' + e.message)
  }
  return false
}

async function downloadFileHandler(row) {
  try {
    const path = currentPath.value === '.' ? row.name : `${currentPath.value}/${row.name}`
    const res = await downloadFile(workerName.value, path)
    if (res.code === 0 && res.data) {
      // 将Base64 转为 Blob 并下载
      const byteCharacters = atob(res.data.data)
      const byteNumbers = new Array(byteCharacters.length)
      for (let i = 0; i < byteCharacters.length; i++) {
        byteNumbers[i] = byteCharacters.charCodeAt(i)
      }
      const byteArray = new Uint8Array(byteNumbers)
      const blob = new Blob([byteArray])
      const url = window.URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = row.name
      link.click()
      window.URL.revokeObjectURL(url)
    } else {
      ElMessage.error(res.message || t('workerConsole.downloadFailed'))
    }
  } catch (e) {
    ElMessage.error(t('workerConsole.downloadFailed') + ': ' + e.message)
  }
}

async function deleteFileHandler(row) {
  try {
    const path = currentPath.value === '.' ? row.name : `${currentPath.value}/${row.name}`
    const res = await deleteFile(workerName.value, path)
    if (res.code === 0) {
      ElMessage.success(t('workerConsole.deleteSuccess'))
      loadFiles()
    } else {
      ElMessage.error(res.message || t('workerConsole.deleteFailed'))
    }
  } catch (e) {
    ElMessage.error(t('workerConsole.deleteFailed') + ': ' + e.message)
  }
}

// 终端功能
async function connectTerminal() {
  // 直接连接 WebSocket，WebSocket handler 会自动打开终端会话
  connectTerminalWS()
}

function connectTerminalWS() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  // 生成一?sessionId
  terminalSessionId.value = Date.now().toString()
  
  // 获取 token 用于 WebSocket 认证
  const token = userStore.token || ''
  
  const wsUrl = `${protocol}//${window.location.host}/api/v1/worker/console/terminal?name=${workerName.value}&sessionId=${terminalSessionId.value}&token=${encodeURIComponent(token)}`
  
  console.log('[Terminal] Connecting to:', wsUrl)
  terminalWs = new WebSocket(wsUrl)
  
  terminalWs.onopen = () => {
    console.log('[Terminal] WebSocket connected')
    terminalConnected.value = true
    appendTerminalOutput(t('workerConsole.terminalConnected') + '\n', 'info')
  }
  
  terminalWs.onmessage = (event) => {
    console.log('[Terminal] Received:', event.data)
    try {
      const data = JSON.parse(event.data)
      if (data.type === 'output') {
        appendTerminalOutput(data.data)
      } else if (data.type === 'error') {
        appendTerminalOutput(data.error || data.data, 'error')
      } else if (data.type === 'pong') {
        // 心跳响应，忽?
      }
    } catch (e) {
      // ?JSON 数据，直接显?
      appendTerminalOutput(event.data)
    }
  }
  
  terminalWs.onclose = (event) => {
    console.log('[Terminal] WebSocket closed:', event.code, event.reason)
    terminalConnected.value = false
    appendTerminalOutput('\n' + t('workerConsole.terminalDisconnected') + '\n', 'info')
  }
  
  terminalWs.onerror = (error) => {
    console.error('[Terminal] WebSocket error:', error)
    ElMessage.error(t('workerConsole.terminalConnectError'))
  }
}

function disconnectTerminal() {
  if (terminalWs) {
    terminalWs.close()
    terminalWs = null
  }
  // WebSocket 断开时服务端会自动关闭会话，无需再调?closeTerminal API
  terminalSessionId.value = ''
  terminalConnected.value = false
}

function sendCommand() {
  if (!terminalInput.value.trim() || !terminalWs) return
  
  const cmd = terminalInput.value
  appendTerminalOutput(`$ ${cmd}\n`, 'command')
  
  // 发送命令到 WebSocket
  terminalWs.send(JSON.stringify({
    type: 'input',
    command: cmd  // 使用 command 字段，后端会记录命令历史
  }))
  
  terminalInput.value = ''
}

function appendTerminalOutput(text, type = 'output') {
  if (!terminalRef.value) return
  
  const span = document.createElement('span')
  span.textContent = text
  span.className = `terminal-${type}`
  terminalRef.value.appendChild(span)
  terminalRef.value.scrollTop = terminalRef.value.scrollHeight
}

function clearTerminal() {
  if (terminalRef.value) {
    terminalRef.value.innerHTML = ''
  }
}

// 审计日志
async function loadAuditLogs() {
  auditLoading.value = true
  try {
    const res = await getAuditLogs(workerName.value, auditPage.value, 20)
    if (res.code === 0) {
      auditLogs.value = res.list || res.data?.list || []
      auditTotal.value = res.total || res.data?.total || 0
    } else {
      ElMessage.error(res.message || t('workerConsole.getAuditLogFailed'))
    }
  } catch (e) {
    ElMessage.error(t('workerConsole.getAuditLogFailed') + ': ' + e.message)
  } finally {
    auditLoading.value = false
  }
}

// 清空审计日志
async function clearAuditLogsHandler() {
  auditClearing.value = true
  try {
    const res = await clearAuditLogs(workerName.value)
    if (res.code === 0) {
      ElMessage.success(res.msg || res.data?.msg || t('workerConsole.auditLogCleared'))
      auditLogs.value = []
      auditTotal.value = 0
      auditPage.value = 1
    } else {
      ElMessage.error(res.message || t('workerConsole.clearAuditLogFailed'))
    }
  } catch (e) {
    ElMessage.error(t('workerConsole.clearAuditLogFailed') + ': ' + e.message)
  } finally {
    auditClearing.value = false
  }
}

// 工具函数
function formatUptime(seconds) {
  if (!seconds) return '-'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const mins = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days} ${t('workerConsole.days')} ${hours} ${t('workerConsole.hours')}`
  if (hours > 0) return `${hours} ${t('workerConsole.hours')} ${mins} ${t('workerConsole.minutes')}`
  return `${mins} ${t('workerConsole.minutes')}`
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

function formatTime(timestamp) {
  if (!timestamp) return '-'
  const date = new Date(timestamp * 1000)
  return date.toLocaleString('zh-CN')
}

function formatAuditTime(timeStr) {
  if (!timeStr) return '-'
  const date = new Date(timeStr)
  return date.toLocaleString('zh-CN')
}

function getLoadColor(value) {
  if (value < 50) return 'var(--el-color-success)'
  if (value < 80) return 'var(--el-color-warning)'
  return 'var(--el-color-danger)'
}

function getMemPercent(info) {
  if (!info?.memTotal) return 0
  return Math.round((info.memUsed / info.memTotal) * 100)
}

function getDiskPercent(info) {
  if (!info?.diskTotal) return 0
  return Math.round((info.diskUsed / info.diskTotal) * 100)
}

function getActionType(action) {
  const types = {
    'file_upload': 'primary',
    'file_download': 'success',
    'file_delete': 'danger',
    'file_list': 'info',
    'file_mkdir': 'primary',
    'terminal_exec': 'warning',
    'terminal_open': 'success',
    'terminal_close': 'info',
    'console_info': 'info'
  }
  return types[action] || 'info'
}

function getActionLabel(action) {
  const labels = {
    'file_upload': t('workerConsole.fileUpload'),
    'file_download': t('workerConsole.fileDownload'),
    'file_delete': t('workerConsole.fileDelete'),
    'file_list': t('workerConsole.fileList'),
    'file_mkdir': t('workerConsole.fileMkdir'),
    'terminal_exec': t('workerConsole.terminalExec'),
    'terminal_open': t('workerConsole.terminalOpen'),
    'terminal_close': t('workerConsole.terminalClose'),
    'console_info': t('workerConsole.consoleInfo')
  }
  return labels[action] || action
}
</script>


<style scoped>
.worker-console {
  .worker-title {
    font-size: 16px;
    font-weight: 600;
  }

  .secondary-text {
    margin-left: 8px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }

  .error-text {
    color: var(--el-color-danger);
  }

  .terminal-status {
    margin-left: 10px;
    font-size: 12px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    
    &.connected {
      color: var(--el-color-success);
    }
  }

  .file-manager {
    .file-toolbar {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 15px;
      padding: 10px;
      background: var(--el-fill-color-light);
      border-radius: 4px;

      .path-item {
        cursor: pointer;
        &:hover {
          color: var(--el-color-primary);
        }
      }

      .file-actions {
        display: flex;
        gap: 10px;
      }
    }

    .file-name {
      display: flex;
      align-items: center;
      gap: 6px;
      cursor: pointer;

      &.is-dir {
        color: var(--el-color-primary);
        font-weight: 500;
      }

      &:hover {
        color: var(--el-color-primary);
      }
    }
  }

  .terminal-container {
    .terminal-toolbar {
      margin-bottom: 10px;
      display: flex;
      align-items: center;
      gap: 10px;
    }

    .terminal-output {
      height: 400px;
      background: var(--code-bg);
      border-radius: 4px;
      padding: 10px;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 13px;
      color: var(--el-text-color-primary);
      overflow-y: auto;
      white-space: pre-wrap;
      word-break: break-all;

      :deep(.terminal-output) {
        color: var(--el-text-color-primary);
      }

      :deep(.terminal-command) {
        color: var(--el-color-primary);
      }

      :deep(.terminal-error) {
        color: var(--el-color-danger);
      }

      :deep(.terminal-info) {
        color: var(--el-color-success);
      }
    }

    .terminal-input {
      display: flex;
      align-items: center;
      margin-top: 10px;
      background: var(--code-bg);
      border-radius: 4px;
      padding: 5px 10px;

      .prompt {
        color: var(--el-color-primary);
        font-family: 'Consolas', 'Monaco', monospace;
        margin-right: 8px;
      }

      :deep(.el-input__wrapper) {
        background: transparent;
        box-shadow: none;
      }

      :deep(.el-input__inner) {
        color: var(--el-text-color-primary);
        font-family: 'Consolas', 'Monaco', monospace;
      }
    }
  }
}
</style>

