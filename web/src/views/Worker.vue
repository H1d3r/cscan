<template>
  <div class="worker-page">
    <el-card class="action-card">
      <el-button type="primary" @click="loadData" :loading="loading">
        <el-icon><Refresh /></el-icon>刷新状态
      </el-button>
      <el-button type="success" @click="openInstallDialog">
        <el-icon><Download /></el-icon>安装Worker
      </el-button>
      <span v-if="loading" style="margin-left: 10px; color: #909399; font-size: 12px;">正在查询Worker实时状态...</span>
      <el-switch 
        v-model="autoRefresh" 
        active-text="自动刷新(10s)" 
        style="margin-left: 15px"
        @change="toggleAutoRefresh"
      />
    </el-card>

    <el-card style="margin-bottom: 20px">
      <el-table :data="tableData" v-loading="loading" stripe max-height="500">
        <el-table-column prop="name" label="Worker名称" min-width="150">
          <template #default="{ row }">
            <span 
              class="editable-name" 
              @click="openRenameDialog(row)"
              :title="'点击修改名称'"
            >
              {{ row.name }}
              <el-icon class="edit-icon"><Edit /></el-icon>
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="IP地址" width="140">
          <template #default="{ row }">
            {{ row.ip || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="cpuLoad" label="CPU负载" width="120">
          <template #default="{ row }">
            <el-progress :percentage="Math.round(row.cpuLoad)" :stroke-width="10" :color="getLoadColor(row.cpuLoad)" />
          </template>
        </el-table-column>
        <el-table-column prop="memUsed" label="内存使用" width="120">
          <template #default="{ row }">
            <el-progress :percentage="Math.round(row.memUsed)" :stroke-width="10" :color="getLoadColor(row.memUsed)" />
          </template>
        </el-table-column>
        <el-table-column prop="taskCount" label="已执行任务" width="100" />
        <el-table-column prop="runningCount" label="正在执行" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.runningCount > 0" type="warning">{{ row.runningCount }}</el-tag>
            <span v-else>0</span>
          </template>
        </el-table-column>
        <el-table-column prop="concurrency" label="并发数" width="100">
          <template #default="{ row }">
            <span 
              class="editable-name" 
              @click="openConcurrencyDialog(row)"
              :title="'点击修改并发数'"
            >
              {{ row.concurrency || 5 }}
              <el-icon class="edit-icon"><Edit /></el-icon>
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }">
            <div>
              <el-tag :type="row.status === 'running' ? 'success' : 'danger'">
                {{ row.status === 'running' ? '运行中' : '离线' }}
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
        <el-table-column prop="updateTime" label="最后响应" width="160" />
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" :icon="Monitor" @click="openConsole(row.name)" :disabled="row.status !== 'running'">控制台</el-button>
            <el-popconfirm
              title="确定要重启该Worker吗？"
              confirm-button-text="确定"
              cancel-button-text="取消"
              @confirm="restartWorker(row.name)"
            >
              <template #reference>
                <el-button size="small" type="warning" :icon="RefreshRight" :disabled="row.status !== 'running'">重启</el-button>
              </template>
            </el-popconfirm>
            <el-popconfirm
              title="确定要删除该Worker吗？这将停止Worker并清除其数据"
              confirm-button-text="确定"
              cancel-button-text="取消"
              @confirm="deleteWorker(row.name)"
            >
              <template #reference>
                <el-button size="small" type="danger" :icon="Delete">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && tableData.length === 0" description="暂无Worker节点" />
    </el-card>

    <!-- 重命名对话框 -->
    <el-dialog v-model="renameDialogVisible" title="修改Worker名称" width="400px">
      <el-form :model="renameForm" label-width="80px">
        <el-form-item label="原名称">
          <el-input v-model="renameForm.oldName" disabled />
        </el-form-item>
        <el-form-item label="新名称">
          <el-input v-model="renameForm.newName" placeholder="请输入新的Worker名称" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="renameDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitRename" :loading="renameLoading">确定</el-button>
      </template>
    </el-dialog>

    <!-- 并发数编辑对话框 -->
    <el-dialog v-model="concurrencyDialogVisible" title="修改并发数" width="400px">
      <el-form :model="concurrencyForm" label-width="80px">
        <el-form-item label="Worker">
          <el-input v-model="concurrencyForm.name" disabled />
        </el-form-item>
        <el-form-item label="并发数">
          <el-input-number v-model="concurrencyForm.concurrency" :min="1" :max="100" />
          <span style="margin-left: 10px; color: #909399; font-size: 12px;">范围: 1-100</span>
        </el-form-item>
        <el-form-item>
          <el-alert type="info" :closable="false" show-icon>
            <template #title>
              减少并发数立即生效；增加并发数需要重启Worker
            </template>
          </el-alert>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="concurrencyDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitConcurrency" :loading="concurrencyLoading">确定</el-button>
      </template>
    </el-dialog>

    <!-- Worker安装对话框 -->
    <el-dialog v-model="installDialogVisible" title="安装Worker探针" width="800px">
      <div class="install-dialog">
        <el-alert type="success" :closable="false" style="margin-bottom: 20px">
          <template #title>
            使用 Docker 一键部署 Worker 探针，无需关心系统环境兼容性问题
          </template>
        </el-alert>

        <el-form label-width="100px" v-if="installInfo.installKey">
          <el-form-item label="安装密钥">
            <div class="key-display">
              <code>{{ installInfo.installKey }}</code>
              <el-button size="small" @click="copyToClipboard(installInfo.installKey)">复制</el-button>
              <el-button size="small" type="warning" @click="refreshInstallKey" :loading="refreshKeyLoading">刷新密钥</el-button>
            </div>
          </el-form-item>

          <el-form-item label="服务地址">
            <code style="background: #f5f7fa; padding: 8px 12px; border-radius: 4px;">{{ installInfo.serverAddr }}</code>
            <span style="margin-left: 10px; color: #909399; font-size: 12px;">（Worker 连接地址）</span>
          </el-form-item>
        </el-form>

        <el-divider content-position="left">Docker 部署命令</el-divider>

        <el-tabs v-model="installOsTab" type="border-card">
          <el-tab-pane label="Linux / macOS" name="linux">
            <div class="command-section">
              <p class="command-title">1. 下载配置文件：</p>
              <div class="command-box">
                <code>curl -O {{ installInfo.downloadUrl }}/static/docker-compose-worker.yaml</code>
                <el-button size="small" @click="copyToClipboard(`curl -O ${installInfo.downloadUrl}/static/docker-compose-worker.yaml`)">复制</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. 启动探针：</p>
              <div class="command-box">
                <code>CSCAN_SERVER={{ installInfo.serverAddr }} CSCAN_KEY={{ installInfo.installKey }} docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard(`CSCAN_SERVER=${installInfo.serverAddr} CSCAN_KEY=${installInfo.installKey} docker-compose -f docker-compose-worker.yaml up -d`)">复制</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">一键执行：</p>
              <div class="command-box">
                <code>curl -O {{ installInfo.downloadUrl }}/static/docker-compose-worker.yaml && CSCAN_SERVER={{ installInfo.serverAddr }} CSCAN_KEY={{ installInfo.installKey }} docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard(`curl -O ${installInfo.downloadUrl}/static/docker-compose-worker.yaml && CSCAN_SERVER=${installInfo.serverAddr} CSCAN_KEY=${installInfo.installKey} docker-compose -f docker-compose-worker.yaml up -d`)">复制</el-button>
              </div>
            </div>
          </el-tab-pane>

          <el-tab-pane label="Windows (PowerShell)" name="windows">
            <div class="command-section">
              <p class="command-title">1. 下载配置文件：</p>
              <div class="command-box">
                <code>{{ psDownloadCmd }}</code>
                <el-button size="small" @click="copyToClipboard(psDownloadCmd)">复制</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. 启动探针：</p>
              <div class="command-box">
                <code>{{ psStartCmd }}</code>
                <el-button size="small" @click="copyToClipboard(psStartCmd)">复制</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">一键执行：</p>
              <div class="command-box">
                <code>{{ psOneKeyCmd }}</code>
                <el-button size="small" @click="copyToClipboard(psOneKeyCmd)">复制</el-button>
              </div>
            </div>
          </el-tab-pane>

          <el-tab-pane label="Windows (CMD)" name="cmd">
            <div class="command-section">
              <p class="command-title">1. 下载配置文件：</p>
              <div class="command-box">
                <code>curl -O {{ installInfo.downloadUrl }}/static/docker-compose-worker.yaml</code>
                <el-button size="small" @click="copyToClipboard(`curl -O ${installInfo.downloadUrl}/static/docker-compose-worker.yaml`)">复制</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. 设置环境变量并启动：</p>
              <div class="command-box">
                <code>set CSCAN_SERVER={{ installInfo.serverAddr }} && set CSCAN_KEY={{ installInfo.installKey }} && docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard(`set CSCAN_SERVER=${installInfo.serverAddr} && set CSCAN_KEY=${installInfo.installKey} && docker-compose -f docker-compose-worker.yaml up -d`)">复制</el-button>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>

        <el-divider content-position="left">常用操作</el-divider>

        <div class="command-section">
          <el-row :gutter="20">
            <el-col :span="12">
              <p class="command-title">查看日志：</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml logs -f</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml logs -f')">复制</el-button>
              </div>
            </el-col>
            <el-col :span="12">
              <p class="command-title">停止探针：</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml down</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml down')">复制</el-button>
              </div>
            </el-col>
          </el-row>
          <el-row :gutter="20" style="margin-top: 10px">
            <el-col :span="12">
              <p class="command-title">重启探针：</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml restart</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml restart')">复制</el-button>
              </div>
            </el-col>
            <el-col :span="12">
              <p class="command-title">更新探针：</p>
              <div class="command-box small">
                <code>docker-compose -f docker-compose-worker.yaml pull && docker-compose -f docker-compose-worker.yaml up -d</code>
                <el-button size="small" @click="copyToClipboard('docker-compose -f docker-compose-worker.yaml pull && docker-compose -f docker-compose-worker.yaml up -d')">复制</el-button>
              </div>
            </el-col>
          </el-row>
        </div>

        <el-collapse style="margin-top: 20px">
          <el-collapse-item title="环境变量说明" name="params">
            <el-table :data="paramTableData" size="small" border>
              <el-table-column prop="param" label="变量名" width="180" />
              <el-table-column prop="desc" label="说明" />
              <el-table-column prop="default" label="默认值" width="120" />
            </el-table>
          </el-collapse-item>
        </el-collapse>
      </div>

      <template #footer>
        <el-button @click="installDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 实时日志 -->
    <el-card>
      <template #header>
        <div class="log-header">
          <span>Worker运行日志</span>
          <div class="log-filters">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索日志..."
              clearable
              size="small"
              style="width: 180px; margin-right: 10px"
              @keyup.enter="searchLogs"
              @clear="searchLogs"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-select 
              v-model="filterWorker" 
              placeholder="筛选Worker" 
              clearable 
              size="small"
              style="width: 150px; margin-right: 10px"
            >
              <el-option label="全部Worker" value="" />
              <el-option 
                v-for="worker in tableData" 
                :key="worker.name" 
                :label="worker.name" 
                :value="worker.name" 
              />
            </el-select>
            <el-select 
              v-model="filterLevel" 
              placeholder="筛选级别" 
              clearable 
              size="small"
              style="width: 120px; margin-right: 10px"
            >
              <el-option label="全部级别" value="" />
              <el-option label="INFO" value="INFO" />
              <el-option label="WARN" value="WARN" />
              <el-option label="ERROR" value="ERROR" />
              <el-option label="DEBUG" value="DEBUG" />
            </el-select>
            <el-switch v-model="autoScroll" active-text="自动滚动" style="margin-right: 15px" />
            <el-button size="small" @click="clearLogs">清空</el-button>
            <el-button size="small" :type="isConnected ? 'success' : 'danger'" @click="toggleConnection">
              {{ isConnected ? '自动刷新中' : '已暂停' }}
            </el-button>
          </div>
        </div>
      </template>
      <div ref="logContainer" class="log-container">
        <div v-for="(log, index) in filteredLogs" :key="index" class="log-item" :class="'log-' + log.level?.toLowerCase()">
          <span class="log-time">{{ log.timestamp }}</span>
          <span class="log-level">[{{ log.level }}]</span>
          <span class="log-worker">[{{ log.workerName }}]</span>
          <span class="log-message">{{ log.message }}</span>
        </div>
        <div v-if="filteredLogs.length === 0 && logs.length > 0" class="log-empty">没有匹配的日志</div>
        <div v-if="logs.length === 0" class="log-empty">暂无日志，Worker 通过 WebSocket 推送日志到服务端</div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, nextTick, watch, reactive, computed } from 'vue'
import { Refresh, Delete, Edit, RefreshRight, Search, Download, Monitor } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import request from '@/api/request'

const router = useRouter()
const loading = ref(false)
const tableData = ref([])
const logs = ref([])
const logContainer = ref(null)
const autoScroll = ref(true)
const isConnected = ref(false)
const autoRefresh = ref(true)
const filterWorker = ref('')
const filterLevel = ref('')
const searchKeyword = ref('')
let pollingTimer = null
let workerRefreshTimer = null
let logIdSet = new Set() // 用于去重

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
const paramTableData = [
  { param: 'CSCAN_SERVER', desc: 'API服务地址（必需）', default: '无' },
  { param: 'CSCAN_KEY', desc: '安装密钥（必需）', default: '无' },
  { param: 'CSCAN_NAME', desc: 'Worker名称', default: '自动生成' },
  { param: 'CSCAN_CONCURRENCY', desc: '并发数', default: '5' }
]

// 筛选后的日志
const filteredLogs = computed(() => {
  const keyword = searchKeyword.value.toLowerCase()
  return logs.value.filter(log => {
    // 筛选 Worker
    if (filterWorker.value && log.workerName !== filterWorker.value) {
      return false
    }
    // 筛选级别
    if (filterLevel.value && log.level?.toUpperCase() !== filterLevel.value) {
      return false
    }
    // 模糊搜索
    if (keyword) {
      const message = (log.message || '').toLowerCase()
      const level = (log.level || '').toLowerCase()
      const workerName = (log.workerName || '').toLowerCase()
      if (!message.includes(keyword) && !level.includes(keyword) && !workerName.includes(keyword)) {
        return false
      }
    }
    return true
  })
})

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
  startPolling()
  startWorkerRefresh()
})

onUnmounted(() => {
  stopPolling()
  stopWorkerRefresh()
})

watch(filteredLogs, () => {
  if (autoScroll.value) {
    nextTick(() => {
      if (logContainer.value) {
        logContainer.value.scrollTop = logContainer.value.scrollHeight
      }
    })
  }
}, { deep: true })

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

function startPolling() {
  if (pollingTimer) return
  console.log('[Polling] Starting...')
  isConnected.value = true
  // 立即获取一次
  fetchLogsHistory()
  // 每2秒轮询一次
  pollingTimer = setInterval(fetchLogsHistory, 2000)
}

function stopPolling() {
  if (pollingTimer) {
    clearInterval(pollingTimer)
    pollingTimer = null
  }
  isConnected.value = false
}

async function fetchLogsHistory() {
  try {
    const res = await request.post('/worker/logs/history', { limit: 200 })
    if (res.code === 0 && res.list && res.list.length > 0) {
      // 找出新日志（使用timestamp+message作为唯一标识）
      let hasNew = false
      for (const log of res.list) {
        const logId = (log.timestamp || '') + (log.message || '')
        if (!logIdSet.has(logId)) {
          logIdSet.add(logId)
          logs.value.push(log)
          hasNew = true
        }
      }
      // 限制日志数量和去重集合大小
      if (logs.value.length > 1000) {
        const removed = logs.value.splice(0, logs.value.length - 500)
        removed.forEach(l => logIdSet.delete((l.timestamp || '') + (l.message || '')))
      }
    }
  } catch (e) {
    console.error('[Polling] Fetch logs error:', e)
    isConnected.value = false
  }
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
    // 清空服务端历史日志
    const res = await request.post('/worker/logs/clear')
    if (res.code === 0) {
      // 清空本地日志
      logs.value = []
      logIdSet.clear()
    }
  } catch (e) {
    console.error('Clear logs error:', e)
  }
}

function getLoadColor(value) {
  if (value < 50) return '#67C23A'
  if (value < 80) return '#E6A23C'
  return '#F56C6C'
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
    'healthy': '正常',
    'warning': '负载较高',
    'overloaded': '过载',
    'throttled': '限流中'
  }
  return texts[status] || status
}

async function deleteWorker(workerName) {
  try {
    const res = await request.post('/worker/delete', { name: workerName })
    if (res.code === 0) {
      ElMessage.success('Worker已删除，停止信号已发送')
      loadData()
    } else {
      ElMessage.error(res.msg || '删除失败')
    }
  } catch (e) {
    ElMessage.error('删除失败: ' + e.message)
  }
}

async function restartWorker(workerName) {
  try {
    const res = await request.post('/worker/restart', { name: workerName })
    if (res.code === 0) {
      ElMessage.success('重启命令已发送')
      // 延迟刷新，等待Worker重启
      setTimeout(() => loadData(), 3000)
    } else {
      ElMessage.error(res.msg || '重启失败')
    }
  } catch (e) {
    ElMessage.error('重启失败: ' + e.message)
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
    ElMessage.warning('并发数必须在1-100之间')
    return
  }

  concurrencyLoading.value = true
  try {
    const res = await request.post('/worker/concurrency', {
      name: concurrencyForm.name,
      concurrency: concurrencyForm.concurrency
    })
    if (res.code === 0) {
      ElMessage.success('并发数设置命令已发送')
      concurrencyDialogVisible.value = false
      // 延迟刷新，等待Worker更新状态
      setTimeout(() => loadData(), 1000)
    } else {
      ElMessage.error(res.msg || '设置失败')
    }
  } catch (e) {
    ElMessage.error('设置失败: ' + e.message)
  } finally {
    concurrencyLoading.value = false
  }
}

async function submitRename() {
  if (!renameForm.newName.trim()) {
    ElMessage.warning('请输入新的Worker名称')
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
      ElMessage.success('重命名成功')
      renameDialogVisible.value = false
      loadData()
    } else {
      ElMessage.error(res.msg || '重命名失败')
    }
  } catch (e) {
    ElMessage.error('重命名失败: ' + e.message)
  } finally {
    renameLoading.value = false
  }
}

// 搜索日志（服务端搜索，用于大量日志场景）
async function searchLogs() {
  // 前端已通过 computed 实现实时过滤，此函数可用于触发服务端搜索
  // 当前实现：前端过滤，无需额外操作
}

// Worker安装相关方法
async function openInstallDialog() {
  installDialogVisible.value = true
  await loadInstallCommand()
}

async function loadInstallCommand() {
  try {
    // 当前浏览器访问地址（用于下载配置文件）
    const currentUrl = window.location.origin
    
    const res = await request.post('/worker/install/command', {})
    if (res.code === 0) {
      installInfo.installKey = res.installKey
      // 下载地址用浏览器当前地址，API地址用后端返回的地址
      installInfo.downloadUrl = currentUrl
      installInfo.serverAddr = res.serverAddr ? `http://${res.serverAddr}` : currentUrl
      installInfo.commands = res.commands || {}
    } else {
      ElMessage.error(res.msg || '获取安装命令失败')
    }
  } catch (e) {
    ElMessage.error('获取安装命令失败: ' + e.message)
  }
}

async function refreshInstallKey() {
  refreshKeyLoading.value = true
  try {
    const res = await request.post('/worker/install/refresh')
    if (res.code === 0) {
      installInfo.installKey = res.installKey
      ElMessage.success('安装密钥已刷新')
      // 重新加载安装命令
      await loadInstallCommand()
    } else {
      ElMessage.error(res.msg || '刷新失败')
    }
  } catch (e) {
    ElMessage.error('刷新失败: ' + e.message)
  } finally {
    refreshKeyLoading.value = false
  }
}

function copyToClipboard(text) {
  if (!text) {
    ElMessage.warning('内容为空')
    return
  }
  navigator.clipboard.writeText(text).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    // 降级方案
    const textarea = document.createElement('textarea')
    textarea.value = text
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
    ElMessage.success('已复制到剪贴板')
  })
}

function openConsole(workerName) {
  router.push(`/worker/console/${workerName}`)
}
</script>

<style lang="scss" scoped>
.worker-page {
  .action-card { margin-bottom: 20px; }

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

  .log-container {
    height: 400px;
    overflow-y: auto;
    background: #1e1e1e;
    border-radius: 4px;
    padding: 10px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 12px;
  }

  .log-item {
    padding: 2px 0;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;

    .log-time {
      color: #6a9955;
      margin-right: 10px;
    }

    .log-level {
      display: inline-block;
      width: 60px;
      margin-right: 8px;
      font-weight: bold;
    }

    .log-worker {
      color: #569cd6;
      margin-right: 8px;
    }

    .log-message {
      color: #d4d4d4;
    }

    &.log-info .log-level { color: #4ec9b0; }
    &.log-warn .log-level { color: #dcdcaa; }
    &.log-error .log-level { color: #f14c4c; }
    &.log-debug .log-level { color: #9cdcfe; }
  }

  .log-empty {
    color: #6a6a6a;
    text-align: center;
    padding: 50px;
  }

  .editable-name {
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    
    &:hover {
      color: #409eff;
      
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
}

// Worker安装对话框样式
.install-dialog {
  .key-display {
    display: flex;
    align-items: center;
    gap: 10px;
    
    code {
      background: #f5f7fa;
      padding: 8px 12px;
      border-radius: 4px;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 14px;
      color: #e6a23c;
      font-weight: bold;
    }
  }

  .command-section {
    .command-title {
      margin: 0 0 8px 0;
      font-size: 13px;
      color: #606266;
    }

    .command-box {
      display: flex;
      align-items: flex-start;
      gap: 10px;
      background: #1e1e1e;
      padding: 12px;
      border-radius: 4px;
      
      code {
        flex: 1;
        font-family: 'Consolas', 'Monaco', monospace;
        font-size: 12px;
        color: #d4d4d4;
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
