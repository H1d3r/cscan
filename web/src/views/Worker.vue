<template>
  <div class="worker-page">
    <el-card class="action-card">
      <el-button type="primary" @click="loadData" :loading="loading">
        <el-icon><Refresh /></el-icon>{{ $t('worker.refreshStatus') }}
      </el-button>
      <el-button type="success" @click="openInstallDialog">
        <el-icon><Download /></el-icon>{{ $t('worker.installWorker') }}
      </el-button>
      <span v-if="loading" class="loading-hint">{{ $t('worker.queryingStatus') }}</span>
      <el-switch 
        v-model="autoRefresh" 
        :active-text="$t('worker.autoRefresh')" 
        style="margin-left: 15px"
        @change="toggleAutoRefresh"
      />
    </el-card>

    <el-card style="margin-bottom: 20px">
      <el-table :data="tableData" v-loading="loading" stripe max-height="500">
        <el-table-column prop="name" :label="$t('worker.workerName')" min-width="150">
          <template #default="{ row }">
            <span 
              class="editable-name" 
              @click="openRenameDialog(row)"
              :title="$t('worker.clickToEditName')"
            >
              {{ row.name }}
              <el-icon class="edit-icon"><Edit /></el-icon>
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="ip" :label="$t('worker.ipAddress')" width="140">
          <template #default="{ row }">
            {{ row.ip || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="cpuLoad" :label="$t('worker.cpuLoad')" width="120">
          <template #default="{ row }">
            <el-progress :percentage="Math.round(row.cpuLoad)" :stroke-width="10" :color="getLoadColor(row.cpuLoad)" />
          </template>
        </el-table-column>
        <el-table-column prop="memUsed" :label="$t('worker.memUsage')" width="120">
          <template #default="{ row }">
            <el-progress :percentage="Math.round(row.memUsed)" :stroke-width="10" :color="getLoadColor(row.memUsed)" />
          </template>
        </el-table-column>
        <el-table-column prop="taskCount" :label="$t('worker.executedTasks')" width="100" />
        <el-table-column prop="runningCount" :label="$t('worker.runningTasks')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.runningCount > 0" type="warning">{{ row.runningCount }}</el-tag>
            <span v-else>0</span>
          </template>
        </el-table-column>
        <el-table-column prop="concurrency" :label="$t('worker.concurrency')" width="140">
          <template #default="{ row }">
            <div class="concurrency-cell">
              <span 
                class="editable-name" 
                @click="openConcurrencyDialog(row)"
                :title="$t('worker.clickToEditConcurrency')"
              >
                {{ row.effectiveConcurrency || row.concurrency || 5 }}
                <el-icon class="edit-icon"><Edit /></el-icon>
              </span>
              <el-tag 
                v-if="row.schedulerMode && row.schedulerMode !== 'normal'" 
                :type="getSchedulerModeType(row.schedulerMode)"
                size="small"
                style="margin-left: 4px"
              >
                {{ getSchedulerModeText(row.schedulerMode) }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="status" :label="$t('worker.status')" width="120">
          <template #default="{ row }">
            <div>
              <el-tag :type="row.status === 'running' ? 'success' : 'danger'">
                {{ row.status === 'running' ? $t('worker.running') : $t('worker.offline') }}
              </el-tag>
              <el-tag 
                v-if="row.healthStatus && row.healthStatus !== 'healthy' && row.status === 'running'" 
                :type="getHealthStatusType(row.healthStatus)"
                size="small"
                style="margin-left: 4px"
              >
                {{ getHealthStatusText(row.healthStatus) }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="updateTime" :label="$t('worker.lastResponse')" width="160" />
        <el-table-column :label="$t('common.operation')" width="280" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" :icon="Monitor" @click="openConsole(row.name)" :disabled="row.status !== 'running'">{{ $t('worker.console') }}</el-button>
            <el-popconfirm
              :title="$t('worker.confirmRestart')"
              :confirm-button-text="$t('common.confirm')"
              :cancel-button-text="$t('common.cancel')"
              @confirm="restartWorker(row.name)"
            >
              <template #reference>
                <el-button size="small" type="warning" :icon="RefreshRight" :disabled="row.status !== 'running'">{{ $t('worker.restart') }}</el-button>
              </template>
            </el-popconfirm>
            <el-popconfirm
              :title="$t('worker.confirmDelete')"
              :confirm-button-text="$t('common.confirm')"
              :cancel-button-text="$t('common.cancel')"
              @confirm="deleteWorker(row.name)"
            >
              <template #reference>
                <el-button size="small" type="danger" :icon="Delete">{{ $t('common.delete') }}</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && tableData.length === 0" :description="$t('worker.noWorkerNodes')" />
    </el-card>

    <!-- 重命名对话框 -->
    <el-dialog v-model="renameDialogVisible" :title="$t('worker.modifyWorkerName')" width="400px">
      <el-form :model="renameForm" label-width="80px">
        <el-form-item :label="$t('worker.originalName')">
          <el-input v-model="renameForm.oldName" disabled />
        </el-form-item>
        <el-form-item :label="$t('worker.newName')">
          <el-input v-model="renameForm.newName" :placeholder="$t('worker.enterNewWorkerName')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="renameDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="submitRename" :loading="renameLoading">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 并发数编辑对话框 -->
    <el-dialog v-model="concurrencyDialogVisible" :title="$t('worker.modifyConcurrency')" width="400px">
      <el-form :model="concurrencyForm" label-width="80px">
        <el-form-item label="Worker">
          <el-input v-model="concurrencyForm.name" disabled />
        </el-form-item>
        <el-form-item :label="$t('worker.concurrency')">
          <el-input-number v-model="concurrencyForm.concurrency" :min="1" :max="100" />
          <span class="hint-text">{{ $t('worker.concurrencyRange') }}</span>
        </el-form-item>
        <el-form-item>
          <el-alert type="info" :closable="false" show-icon>
            <template #title>
              {{ $t('worker.concurrencyNote') }}
            </template>
          </el-alert>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="concurrencyDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="submitConcurrency" :loading="concurrencyLoading">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- Worker安装对话框 -->
    <el-dialog v-model="installDialogVisible" :title="$t('worker.installWorkerProbe')" width="800px">
      <div class="install-dialog">
        <el-alert type="success" :closable="false" style="margin-bottom: 20px">
          <template #title>
            {{ $t('worker.dockerDeployNote') }}
          </template>
        </el-alert>

        <el-form label-width="100px" v-if="installInfo.installKey">
          <el-form-item :label="$t('worker.installKey')">
            <div class="key-display">
              <code>{{ installInfo.installKey }}</code>
              <el-button size="small" @click="copyToClipboard(installInfo.installKey)">{{ $t('common.copy') }}</el-button>
              <el-button size="small" type="warning" @click="refreshInstallKey" :loading="refreshKeyLoading">{{ $t('common.refreshKey') }}</el-button>
            </div>
          </el-form-item>

          <el-form-item :label="$t('worker.serverAddress')">
            <code class="server-addr-code">{{ installInfo.serverAddr }}</code>
            <span style="margin-left: 10px; color: var(--el-text-color-secondary); font-size: 12px;">（{{ $t('worker.workerConnectAddress') }}）</span>
          </el-form-item>
        </el-form>

        <el-divider content-position="left">{{ $t('worker.dockerDeployCommand') }}</el-divider>

        <el-tabs v-model="installOsTab" type="border-card">
          <el-tab-pane label="Linux / macOS" name="linux">
            <div class="command-section">
              <p class="command-title">1. {{ $t('worker.downloadConfig') }}</p>
              <div class="command-box">
                <code>curl -O {{ installInfo.downloadUrl }}/static/docker-compose-worker.yaml</code>
                <el-button size="small" @click="copyToClipboard(`curl -O ${installInfo.downloadUrl}/static/docker-compose-worker.yaml`)">{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. {{ $t('worker.startProbe') }}</p>
              <div class="command-box">
                <code>CSCAN_SERVER={{ installInfo.serverAddr }} CSCAN_KEY={{ installInfo.installKey }} docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard(`CSCAN_SERVER=${installInfo.serverAddr} CSCAN_KEY=${installInfo.installKey} docker-compose -f docker-compose-worker.yaml up -d`)">{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">{{ $t('worker.oneKeyExecute') }}</p>
              <div class="command-box">
                <code>curl -O {{ installInfo.downloadUrl }}/static/docker-compose-worker.yaml && CSCAN_SERVER={{ installInfo.serverAddr }} CSCAN_KEY={{ installInfo.installKey }} docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard(`curl -O ${installInfo.downloadUrl}/static/docker-compose-worker.yaml && CSCAN_SERVER=${installInfo.serverAddr} CSCAN_KEY=${installInfo.installKey} docker-compose -f docker-compose-worker.yaml up -d`)">{{ $t('common.copy') }}</el-button>
              </div>
            </div>
          </el-tab-pane>

          <el-tab-pane label="Windows (PowerShell)" name="windows">
            <div class="command-section">
              <p class="command-title">1. {{ $t('worker.downloadConfig') }}</p>
              <div class="command-box">
                <code>{{ psDownloadCmd }}</code>
                <el-button size="small" @click="copyToClipboard(psDownloadCmd)">{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. {{ $t('worker.startProbe') }}</p>
              <div class="command-box">
                <code>{{ psStartCmd }}</code>
                <el-button size="small" @click="copyToClipboard(psStartCmd)">{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">{{ $t('worker.oneKeyExecute') }}</p>
              <div class="command-box">
                <code>{{ psOneKeyCmd }}</code>
                <el-button size="small" @click="copyToClipboard(psOneKeyCmd)">{{ $t('common.copy') }}</el-button>
              </div>
            </div>
          </el-tab-pane>

          <el-tab-pane label="Windows (CMD)" name="cmd">
            <div class="command-section">
              <p class="command-title">1. {{ $t('worker.downloadConfig') }}</p>
              <div class="command-box">
                <code>curl -O {{ installInfo.downloadUrl }}/static/docker-compose-worker.yaml</code>
                <el-button size="small" @click="copyToClipboard(`curl -O ${installInfo.downloadUrl}/static/docker-compose-worker.yaml`)">{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. {{ $t('worker.setEnvAndStart') }}</p>
              <div class="command-box">
                <code>set CSCAN_SERVER={{ installInfo.serverAddr }} && set CSCAN_KEY={{ installInfo.installKey }} && docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard(`set CSCAN_SERVER=${installInfo.serverAddr} && set CSCAN_KEY=${installInfo.installKey} && docker-compose -f docker-compose-worker.yaml up -d`)">{{ $t('common.copy') }}</el-button>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>

        <el-divider content-position="left">{{ $t('worker.commonOperations') }}</el-divider>

        <div class="command-section">
          <el-row :gutter="20">
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.viewLogs') }}</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml logs -f</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml logs -f')">{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.stopProbe') }}</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml down</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml down')">{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
          </el-row>
          <el-row :gutter="20" style="margin-top: 10px">
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.restartProbe') }}</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml restart</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml restart')">{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.updateProbe') }}</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml pull && docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml pull && docker-compose -f docker-compose-worker.yaml up -d')">{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
          </el-row>
        </div>

        <el-collapse style="margin-top: 20px">
          <el-collapse-item :title="$t('worker.envVarDescription')" name="params">
            <el-table :data="paramTableData" size="small" border>
              <el-table-column prop="param" :label="$t('worker.variable')" width="180" />
              <el-table-column prop="desc" :label="$t('fingerprint.description')" />
              <el-table-column prop="default" :label="$t('worker.defaultValue')" width="120" />
            </el-table>
          </el-collapse-item>
        </el-collapse>
      </div>

      <template #footer>
        <el-button @click="installDialogVisible = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, reactive, computed } from 'vue'
import { Refresh, Delete, Edit, RefreshRight, Download, Monitor } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import request from '@/api/request'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const tableData = ref([])
const autoRefresh = ref(true)
let workerRefreshTimer = null

// Worker安装相关
const installDialogVisible = ref(false)
const installOsTab = ref('linux')
const refreshKeyLoading = ref(false)
const installInfo = reactive({
  installKey: '',
  serverAddr: '',    // API 服务地址（Worker 连接用）
  downloadUrl: '',   // 下载地址（当前浏览器地址）
  commands: {}
})

// 参数说明表格数据
const paramTableData = computed(() => [
  { param: 'CSCAN_SERVER', desc: t('worker.serverAddressRequired'), default: t('common.no') },
  { param: 'CSCAN_KEY', desc: t('worker.installKeyRequired'), default: t('common.no') },
  { param: 'CSCAN_NAME', desc: t('worker.workerNameDesc'), default: t('worker.autoGenerate') },
  { param: 'CSCAN_CONCURRENCY', desc: t('worker.concurrencyDesc'), default: '5' }
])

// PowerShell 命令计算属性
const psDownloadCmd = computed(() => {
  return `Invoke-WebRequest -Uri "${installInfo.downloadUrl}/static/docker-compose-worker.yaml" -OutFile "docker-compose-worker.yaml"`
})

const psStartCmd = computed(() => {
  return `$env:CSCAN_SERVER="${installInfo.serverAddr}"; $env:CSCAN_KEY="${installInfo.installKey}"; docker-compose -f docker-compose-worker.yaml up -d`
})

const psOneKeyCmd = computed(() => {
  return `${psDownloadCmd.value}; ${psStartCmd.value}`
})

// 重命名相关
const renameDialogVisible = ref(false)
const renameLoading = ref(false)
const renameForm = reactive({
  oldName: '',
  newName: ''
})

// 并发数编辑相关
const concurrencyDialogVisible = ref(false)
const concurrencyLoading = ref(false)
const concurrencyForm = reactive({
  name: '',
  concurrency: 5
})

onMounted(() => {
  loadData()
  startWorkerRefresh()
})

onUnmounted(() => {
  stopWorkerRefresh()
})

async function loadData() {
  loading.value = true
  try {
    const res = await request.post('/worker/list')
    if (res.code === 0) tableData.value = res.list || []
  } finally {
    loading.value = false
  }
}

function startWorkerRefresh() {
  if (workerRefreshTimer) return
  // 每10秒自动刷新Worker列表（因为每次查询需要约1.5秒等待Worker响应）
  workerRefreshTimer = setInterval(() => {
    if (autoRefresh.value && !loading.value) {
      loadData()
    }
  }, 10000)
}

function stopWorkerRefresh() {
  if (workerRefreshTimer) {
    clearInterval(workerRefreshTimer)
    workerRefreshTimer = null
  }
}

function toggleAutoRefresh(val) {
  if (val) {
    startWorkerRefresh()
  } else {
    stopWorkerRefresh()
  }
}

function getLoadColor(value) {
  if (value < 50) return 'var(--el-color-success)'
  if (value < 80) return 'var(--el-color-warning)'
  return 'var(--el-color-danger)'
}

function getHealthStatusType(status) {
  const types = {
    'healthy': 'success',
    'warning': 'warning',
    'overloaded': 'danger',
    'throttled': 'info'
  }
  return types[status] || 'info'
}

function getHealthStatusText(status) {
  const texts = {
    'healthy': t('worker.healthy'),
    'warning': t('worker.warning'),
    'overloaded': t('worker.overloaded'),
    'throttled': t('worker.throttled')
  }
  return texts[status] || status
}

function getSchedulerModeType(mode) {
  const types = {
    'aggressive': 'success',
    'normal': '',
    'conservative': 'warning',
    'critical': 'danger'
  }
  return types[mode] || 'info'
}

function getSchedulerModeText(mode) {
  const texts = {
    'aggressive': t('worker.modeAggressive'),
    'normal': t('worker.modeNormal'),
    'conservative': t('worker.modeConservative'),
    'critical': t('worker.modeCritical')
  }
  return texts[mode] || mode
}

async function deleteWorker(workerName) {
  try {
    const res = await request.post('/worker/delete', { name: workerName })
    if (res.code === 0) {
      ElMessage.success(t('worker.workerDeleted'))
      loadData()
    } else {
      ElMessage.error(res.msg || t('worker.deleteFailed'))
    }
  } catch (e) {
    ElMessage.error(t('worker.deleteFailed') + ': ' + e.message)
  }
}

async function restartWorker(workerName) {
  try {
    const res = await request.post('/worker/restart', { name: workerName })
    if (res.code === 0) {
      ElMessage.success(t('worker.restartCommandSent'))
      // 延迟刷新，等待Worker重启
      setTimeout(() => loadData(), 3000)
    } else {
      ElMessage.error(res.msg || t('worker.restartFailed'))
    }
  } catch (e) {
    ElMessage.error(t('worker.restartFailed') + ': ' + e.message)
  }
}

function openRenameDialog(row) {
  renameForm.oldName = row.name
  renameForm.newName = row.name
  renameDialogVisible.value = true
}

function openConcurrencyDialog(row) {
  concurrencyForm.name = row.name
  concurrencyForm.concurrency = row.concurrency || 5
  concurrencyDialogVisible.value = true
}

async function submitConcurrency() {
  if (concurrencyForm.concurrency < 1 || concurrencyForm.concurrency > 100) {
    ElMessage.warning(t('worker.concurrencyMustBe'))
    return
  }

  concurrencyLoading.value = true
  try {
    const res = await request.post('/worker/concurrency', {
      name: concurrencyForm.name,
      concurrency: concurrencyForm.concurrency
    })
    if (res.code === 0) {
      ElMessage.success(t('worker.concurrencyCommandSent'))
      concurrencyDialogVisible.value = false
      // 延迟刷新，等待Worker更新状态
      setTimeout(() => loadData(), 500)
    } else {
      ElMessage.error(res.msg || t('worker.setFailed'))
    }
  } catch (e) {
    ElMessage.error(t('worker.setFailed') + ': ' + e.message)
  } finally {
    concurrencyLoading.value = false
  }
}

async function submitRename() {
  if (!renameForm.newName.trim()) {
    ElMessage.warning(t('worker.enterNewWorkerName'))
    return
  }
  if (renameForm.newName === renameForm.oldName) {
    renameDialogVisible.value = false
    return
  }

  renameLoading.value = true
  try {
    const res = await request.post('/worker/rename', {
      oldName: renameForm.oldName,
      newName: renameForm.newName.trim()
    })
    if (res.code === 0) {
      ElMessage.success(t('worker.renameSuccess'))
      renameDialogVisible.value = false
      loadData()
    } else {
      ElMessage.error(res.msg || t('worker.renameFailed'))
    }
  } catch (e) {
    ElMessage.error(t('worker.renameFailed') + ': ' + e.message)
  } finally {
    renameLoading.value = false
  }
}

// Worker安装相关方法
async function openInstallDialog() {
  installDialogVisible.value = true
  await loadInstallCommand()
}

async function loadInstallCommand() {
  try {
    // 只传主机名，让后端决定端口
    const hostname = window.location.hostname
    
    const res = await request.post('/worker/install/command', { serverAddr: hostname })
    if (res.code === 0) {
      installInfo.installKey = res.installKey
      // 使用后端返回的完整地址
      const apiUrl = `http://${res.serverAddr}`
      installInfo.downloadUrl = apiUrl
      installInfo.serverAddr = apiUrl
      installInfo.commands = res.commands || {}
    } else {
      ElMessage.error(res.msg || t('worker.getInstallCommandFailed'))
    }
  } catch (e) {
    ElMessage.error(t('worker.getInstallCommandFailed') + ': ' + e.message)
  }
}

async function refreshInstallKey() {
  refreshKeyLoading.value = true
  try {
    const res = await request.post('/worker/install/refresh')
    if (res.code === 0) {
      installInfo.installKey = res.installKey
      ElMessage.success(t('worker.installKeyRefreshed'))
      // 重新加载安装命令
      await loadInstallCommand()
    } else {
      ElMessage.error(res.msg || t('worker.refreshFailed'))
    }
  } catch (e) {
    ElMessage.error(t('worker.refreshFailed') + ': ' + e.message)
  } finally {
    refreshKeyLoading.value = false
  }
}

function copyToClipboard(text) {
  if (!text) {
    ElMessage.warning(t('worker.contentEmpty'))
    return
  }
  
  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(() => {
      ElMessage.success(t('worker.copiedToClipboard'))
    }).catch(() => {
      // 降级方案
      fallbackCopyToClipboard(text)
    })
  } else {
    // 直接使用降级方案
    fallbackCopyToClipboard(text)
  }
}

function fallbackCopyToClipboard(text) {
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.left = '-999999px'
    textarea.style.top = '-999999px'
    document.body.appendChild(textarea)
    textarea.focus()
    textarea.select()
    const successful = document.execCommand('copy')
    document.body.removeChild(textarea)
    
    if (successful) {
      ElMessage.success(t('worker.copiedToClipboard'))
    } else {
      ElMessage.error(t('worker.copyFailed'))
    }
  } catch (err) {
    console.error('复制失败:', err)
    ElMessage.error(t('worker.copyFailed'))
  }
}

function openConsole(workerName) {
  router.push(`/worker/console/${workerName}`)
}
</script>

<style lang="scss" scoped>
.worker-page {
  .action-card { margin-bottom: 20px; }

  .editable-name {
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    
    &:hover {
      color: var(--el-color-primary);
      
      .edit-icon {
        opacity: 1;
      }
    }
    
    .edit-icon {
      opacity: 0;
      font-size: 14px;
      transition: opacity 0.2s;
    }
  }

  .concurrency-cell {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
  }

  .hint-text {
    margin-left: 10px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }

  .loading-hint {
    margin-left: 15px;
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
}

// Worker安装对话框样式
.install-dialog {
  .key-display {
    display: flex;
    align-items: center;
    gap: 10px;
    
    code {
      background: var(--el-fill-color-light);
      padding: 8px 12px;
      border-radius: 4px;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 14px;
      color: var(--el-color-warning);
      font-weight: bold;
    }
  }

  // 服务地址样式
  .server-addr-code {
    background: var(--el-fill-color-light);
    color: var(--el-text-color-regular);
    padding: 8px 12px;
    border-radius: 4px;
    font-family: 'Consolas', 'Monaco', monospace;
  }

  .command-section {
    .command-title {
      margin: 0 0 8px 0;
      font-size: 13px;
      color: var(--el-text-color-secondary);
    }

    .command-box {
      display: flex;
      align-items: flex-start;
      gap: 10px;
      background: var(--el-fill-color-darker, #1e1e1e);
      padding: 12px;
      border-radius: 4px;
      
      code {
        flex: 1;
        font-family: 'Consolas', 'Monaco', monospace;
        font-size: 12px;
        color: var(--el-text-color-primary);
        word-break: break-all;
        white-space: pre-wrap;
        line-height: 1.6;
      }
      
      .el-button {
        flex-shrink: 0;
      }

      &.small {
        padding: 8px 10px;
        
        code {
          font-size: 11px;
        }
      }
    }
  }
}
</style>
