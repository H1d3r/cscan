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
        <el-table-column prop="name" :label="$t('worker.workerName')" min-width="160">
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
        <el-table-column prop="ip" :label="$t('worker.ipAddress')" width="130">
          <template #default="{ row }">
            {{ row.ip || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="cpuLoad" :label="$t('worker.cpuLoad')" width="110">
          <template #default="{ row }">
            <el-progress :percentage="Math.round(row.cpuLoad)" :stroke-width="10" :color="getLoadColor(row.cpuLoad)" />
          </template>
        </el-table-column>
        <el-table-column prop="memUsed" :label="$t('worker.memUsage')" width="110">
          <template #default="{ row }">
            <el-progress :percentage="Math.round(row.memUsed)" :stroke-width="10" :color="getLoadColor(row.memUsed)" />
          </template>
        </el-table-column>
        <el-table-column prop="taskCount" :label="$t('worker.executedTasks')" width="95" />
        <el-table-column prop="runningCount" :label="$t('worker.runningTasks')" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.runningCount > 0" type="warning">
              {{ row.runningCount }}
            </el-tag>
            <span v-else>0</span>
          </template>
        </el-table-column>
        <el-table-column prop="concurrency" :label="$t('worker.concurrency')" width="110">
          <template #default="{ row }">
            <div class="concurrency-cell">
              <span
                class="editable-name"
                @click="openConcurrencyDialog(row)"
                :title="$t('worker.clickToEditConcurrency')"
              >
                {{ row.concurrency || 5 }}
                <el-icon class="edit-icon"><Edit /></el-icon>
              </span>
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
        <el-table-column prop="updateTime" :label="$t('worker.lastResponse')" width="165" />
        <el-table-column :label="$t('common.operation')" width="260" fixed="right">
          <template #default="{ row }">
            <el-button
              link
              size="small"
              type="info"
              @click="toggleLogPanel(row.name)"
            >{{ $t('worker.logs') }}</el-button>
            <el-popconfirm
              :title="$t('worker.confirmRestart')"
              :confirm-button-text="$t('common.confirm')"
              :cancel-button-text="$t('common.cancel')"
              @confirm="restartWorker(row.name)"
            >
              <template #reference>
                <el-button
                  link
                  size="small"
                  type="warning"
                  :disabled="row.status !== 'running'"
                >{{ $t('worker.restart') }}</el-button>
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
        <el-button
          type="primary"
          @click="submitConcurrency"
          :loading="concurrencyLoading"
        >{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- Worker安装对话框 -->
    <el-dialog v-model="installDialogVisible" :title="$t('worker.installWorkerProbe')" width="860px">
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
              <el-button
                size="small"
                @click="copyToClipboard(installInfo.installKey)"
              >{{ $t('common.copy') }}</el-button>
              <el-button
                size="small"
                type="warning"
                @click="refreshInstallKey"
                :loading="refreshKeyLoading"
              >{{ $t('common.refreshKey') }}</el-button>
            </div>
          </el-form-item>

          <el-form-item :label="$t('worker.serverAddress')">
            <code class="server-addr-code">{{ installInfo.serverAddr }}</code>
            <span
              style="margin-left: 10px; color: var(--el-text-color-secondary); font-size: 12px;"
            >（{{ $t('worker.workerConnectAddress') }}）</span>
          </el-form-item>

          <el-form-item :label="$t('worker.mongoAddress')">
            <code class="server-addr-code">{{ installInfo.mongoUri || 'localhost:27017/cscan' }}</code>
            <span
              style="margin-left: 10px; color: var(--el-text-color-secondary); font-size: 12px;"
            >（{{ $t('worker.mongoConnectAddress') }}）</span>
          </el-form-item>

          <el-form-item :label="$t('worker.redisAddress')">
            <code class="server-addr-code">{{ installInfo.redisAddr || 'localhost:6379' }}</code>
            <span
              style="margin-left: 10px; color: var(--el-text-color-secondary); font-size: 12px;"
            >（{{ $t('worker.redisConnectAddress') }}）</span>
          </el-form-item>
        </el-form>

        <el-divider content-position="left">{{ $t('worker.dockerDeployCommand') }}</el-divider>

        <el-tabs v-model="installOsTab" type="border-card">
          <el-tab-pane label="Linux / macOS" name="linux">
            <div class="command-section">
              <p class="command-title">1. {{ $t('worker.downloadConfig') }}</p>
              <div class="command-box">
                <code>curl -O {{ installInfo.downloadUrl }}/static/worker-tune.sh</code>
                <el-button
                  size="small"
                  @click="copyToClipboard(linuxDownloadCmd)"
                >{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. {{ $t('worker.startProbe') }}</p>
              <div class="command-box">
                <code>{{ linuxStartCmd }}</code>
                <el-button
                  size="small"
                  @click="copyToClipboard(linuxStartCmd)"
                >{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">{{ $t('worker.oneKeyExecute') }}</p>
              <div class="command-box">
                <code>{{ linuxOneKeyCmd }}</code>
                <el-button
                  size="small"
                  @click="copyToClipboard(linuxOneKeyCmd)"
                >{{ $t('common.copy') }}</el-button>
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
                <code>curl -O {{ installInfo.downloadUrl }}/static/worker-tune.ps1</code>
                <el-button
                  size="small"
                  @click="copyToClipboard(cmdDownloadCmd)"
                >{{ $t('common.copy') }}</el-button>
              </div>

              <p class="command-title" style="margin-top: 15px">2. {{ $t('worker.setEnvAndStart') }}</p>
              <div class="command-box">
                <code>{{ cmdStartCmd }}</code>
                <el-button
                  size="small"
                  @click="copyToClipboard(cmdStartCmd)"
                >{{ $t('common.copy') }}</el-button>
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
                <code>docker compose -f docker-compose-worker.yaml logs -f</code>
                <el-button
                  size="small"
                  @click="copyToClipboard('docker compose -f docker-compose-worker.yaml logs -f')"
                >{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.stopProbe') }}</p>
              <div class="command-box small">
                <code>docker compose -f docker-compose-worker.yaml down</code>
                <el-button
                  size="small"
                  @click="copyToClipboard('docker compose -f docker-compose-worker.yaml down')"
                >{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
          </el-row>
          <el-row :gutter="20" style="margin-top: 10px">
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.restartProbe') }}</p>
              <div class="command-box small">
                <code>docker compose -f docker-compose-worker.yaml restart</code>
                <el-button
                  size="small"
                  @click="copyToClipboard('docker compose -f docker-compose-worker.yaml restart')"
                >{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.updateProbe') }}</p>
              <div class="command-box small">
                <code>{{ updateProbeCmd }}</code>
                <el-button
                  size="small"
                  @click="copyToClipboard(updateProbeCmd)"
                >{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
          </el-row>
        </div>

        <el-divider content-position="left">{{ $t('worker.distributedDeploy') }}</el-divider>

        <div class="command-section">
          <el-alert type="info" :closable="false" style="margin-bottom: 12px">
            <template #title>
              {{ $t('worker.distributedDeployNote') }}
            </template>
          </el-alert>
          <p class="command-title">{{ $t('worker.dockerComposeTitle') }}</p>
          <div class="command-box small">
            <code>{{ dockerComposeCmd }}</code>
            <el-button size="small" @click="copyToClipboard(dockerComposeCmd)">{{ $t('common.copy') }}</el-button>
          </div>
          <el-row :gutter="20" style="margin-top: 15px">
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.distributedDockerTitle') }}</p>
              <div class="command-box small">
                <code>{{ dockerRunCmd }}</code>
                <el-button size="small" @click="copyToClipboard(dockerRunCmd)">{{ $t('common.copy') }}</el-button>
              </div>
            </el-col>
            <el-col :span="12">
              <p class="command-title">{{ $t('worker.distributedEnvTitle') }}</p>
              <div class="command-box small">
                <code>CSCAN_SERVER={{
                  installInfo.serverAddr
                }}<br>CSCAN_KEY={{
                  installInfo.installKey
                }}<br>CSCAN_MONGO_URI={{
                  installInfo.mongoUri || 'mongodb://localhost:27017/cscan'
                }}<br>CSCAN_REDIS_ADDR={{
                  installInfo.redisAddr || 'localhost:6379'
                }}<br>CSCAN_REDIS_PASSWORD={{
                  installInfo.redisPassword || '(' + $t('common.none') + ')'
                }}</code>
                <el-button
                  size="small"
                  @click="copyToClipboard(distributedEnvText)"
                >{{ $t('common.copy') }}</el-button>
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

    <transition name="el-zoom-in-top">
      <el-card v-if="workerLogPanelVisible" class="log-panel-card">
        <div class="log-panel-header">
          <div class="log-panel-title">
            <el-icon><Document /></el-icon>
            <span>{{ $t('worker.logs') }} - {{ logDialogWorker }}</span>
          </div>
          <div class="log-panel-actions">
            <el-input
              v-model="logSearch"
              :placeholder="$t('container.searchLogs')"
              clearable
              size="small"
              style="width: 200px"
            >
              <template #prefix><el-icon><Search /></el-icon></template>
            </el-input>
            <el-select
              v-model="logLevelFilter"
              size="small"
              style="width: 110px"
              :placeholder="$t('container.allLevels')"
              @change="fetchWorkerLogs"
            >
              <el-option :label="$t('container.allLevels')" value="all" />
              <el-option label="ERROR" value="ERROR" />
              <el-option label="WARN" value="WARN" />
              <el-option label="INFO" value="INFO" />
              <el-option label="DEBUG" value="DEBUG" />
            </el-select>
            <el-checkbox v-model="logIncludeDebug" size="small" @change="fetchWorkerLogs">{{ $t('common.includeDebug') }}</el-checkbox>
            <el-button type="primary" size="small" :loading="logLoading" @click="fetchWorkerLogs">
              <el-icon style="margin-right: 4px"><Refresh /></el-icon>{{ $t('common.refresh') }}
            </el-button>
            <el-button size="small" @click="closeLogPanel">{{ $t('worker.closeLogPanel') }}</el-button>
          </div>
        </div>
        <div class="worker-log-container" ref="workerLogBox" :class="{ 'is-dark': isLogDark }">
          <div v-if="!filteredLogLines.length && !logLoading" class="log-empty-state">
            <el-icon :size="40" style="color: var(--el-text-color-disabled)"><Document /></el-icon>
            <span>{{ $t('container.noLogs') }}</span>
          </div>
          <div
            v-for="(l, idx) in filteredLogLines"
            :key="idx"
            class="wlog-line"
            :class="{
              'wlog-error': l.level === 'ERROR' || l.level === 'FATAL',
              'wlog-warn': l.level === 'WARN' || l.level === 'SLOW',
              'wlog-debug': l.level === 'DEBUG'
            }"
          >
            <span class="wlog-ln">{{ idx + 1 }}</span>
            <span class="wlog-level" :class="'wlevel-' + (l.level || 'log').toLowerCase()">{{ l.level || 'LOG' }}</span>
            <span v-if="l.time" class="wlog-time">{{ l.time }}</span>
            <span v-if="l.taskId" class="wlog-task" :title="l.taskId">[..{{ l.taskId.slice(-4) }}]</span>
            <span class="wlog-body">{{ l.body }}</span>
          </div>
        </div>
        <div class="log-panel-footer">
          <span class="log-count-badge">{{ filteredLogLines.length }} / {{ logLines.length }}</span>
        </div>
      </el-card>
    </transition>

    <!-- 重命名对话框 -->
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, reactive, computed, nextTick } from 'vue'
import { Refresh, Edit, RefreshRight, Download, Monitor, Document, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '@/api/request'
import { useI18n } from 'vue-i18n'
import { useThemeStore } from '@/stores/theme'

const { t } = useI18n()
const themeStore = useThemeStore()
const isLogDark = computed(() => themeStore.isDark)
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
  mongoUri: '',      // MongoDB 地址（分布式 Worker 直连用）
  redisAddr: '',     // Redis 地址（Worker 直连调度层）
  redisPassword: '', // Redis 密码
  commands: {}
})

// 参数说明表格数据
const paramTableData = computed(() => [
  { param: 'CSCAN_SERVER', desc: t('worker.serverAddressRequired'), default: t('common.no') },
  { param: 'CSCAN_KEY', desc: t('worker.installKeyRequired'), default: t('common.no') },
  { param: 'CSCAN_NAME', desc: t('worker.workerNameDesc'), default: 'cscan-probe' },
  { param: 'CSCAN_MONGO_URI', desc: t('worker.mongoUriDesc'), default: t('common.no') },
  { param: 'CSCAN_REDIS_ADDR', desc: t('worker.redisAddrDesc'), default: t('common.no') },
  { param: 'CSCAN_REDIS_PASSWORD', desc: t('worker.redisPasswordDesc'), default: t('common.none') },
  { param: 'CSCAN_CONCURRENCY', desc: t('worker.concurrencyDesc'), default: t('worker.autoDerive') }
])

// PowerShell 命令计算属性
const psDownloadCmd = computed(() => {
  return `Invoke-WebRequest -Uri "${installInfo.downloadUrl}/static/worker-tune.ps1" -OutFile "worker-tune.ps1"`
})

const psStartCmd = computed(() => {
  return `$env:CSCAN_SERVER="${installInfo.serverAddr}"; `
    + `$env:CSCAN_KEY="${installInfo.installKey}"; `
    + 'powershell -NoProfile -ExecutionPolicy Bypass -File worker-tune.ps1'
})

const psOneKeyCmd = computed(() => {
  return `${psDownloadCmd.value}; ${psStartCmd.value}`
})

// Linux / macOS 命令计算属性
const linuxDownloadCmd = computed(() => {
  return `curl -O ${installInfo.downloadUrl}/static/worker-tune.sh`
})

const linuxStartCmd = computed(() => {
  return `CSCAN_SERVER=${installInfo.serverAddr} `
    + `CSCAN_KEY=${installInfo.installKey} bash worker-tune.sh`
})

const linuxOneKeyCmd = computed(() => {
  return `${linuxDownloadCmd.value} && ${linuxStartCmd.value}`
})

// Windows (CMD) 命令计算属性
const cmdDownloadCmd = computed(() => {
  return `curl -O ${installInfo.downloadUrl}/static/worker-tune.ps1`
})

const cmdStartCmd = computed(() => {
  return `set CSCAN_SERVER=${installInfo.serverAddr} && `
    + `set CSCAN_KEY=${installInfo.installKey} && `
    + 'powershell -NoProfile -ExecutionPolicy Bypass -File worker-tune.ps1'
})

// 更新探针命令（docker compose pull + up）
const updateProbeCmd = 'docker compose -f docker-compose-worker.yaml pull '
  + '&& docker compose -f docker-compose-worker.yaml up -d'

// 分布式部署环境变量文本（复制到剪贴板用）
const distributedEnvText = computed(() => {
  const mongo = installInfo.mongoUri || 'mongodb://localhost:27017/cscan'
  const redisAddr = installInfo.redisAddr || 'localhost:6379'
  let text = `CSCAN_SERVER=${installInfo.serverAddr}\n`
    + `CSCAN_KEY=${installInfo.installKey}\n`
    + `CSCAN_MONGO_URI=${mongo}\n`
    + `CSCAN_REDIS_ADDR=${redisAddr}`
  if (installInfo.redisPassword) {
    text += `\nCSCAN_REDIS_PASSWORD=${installInfo.redisPassword}`
  }
  return text
})

// Docker Compose 部署命令（与 docker-compose-worker.yaml 对齐）
const dockerComposeCmd = computed(() => {
  const mongo = installInfo.mongoUri || 'mongodb://localhost:27017/cscan'
  const redisAddr = installInfo.redisAddr || 'localhost:6379'
  let cmd = `CSCAN_SERVER=${installInfo.serverAddr} `
    + `CSCAN_KEY=${installInfo.installKey} `
    + `CSCAN_MONGO_URI=${mongo} `
    + `CSCAN_REDIS_ADDR=${redisAddr}`
  if (installInfo.redisPassword) {
    cmd += ` CSCAN_REDIS_PASSWORD=${installInfo.redisPassword}`
  }
  cmd += ` \\\n  docker compose -f docker-compose-worker.yaml up -d`
  return cmd
})

// 分布式 docker 部署命令
const dockerRunCmd = computed(() => {
  const server = installInfo.serverAddr
  const key = installInfo.installKey
  const mongo = installInfo.mongoUri || 'mongodb://localhost:27017/cscan'
  const redisAddr = installInfo.redisAddr || 'localhost:6379'
  let cmd = `docker run -d --name cscan-worker --network host \
-e CSCAN_SERVER=${server} -e CSCAN_KEY=${key} \
-e CSCAN_MONGO_URI=${mongo} \
-e CSCAN_REDIS_ADDR=${redisAddr}`
  if (installInfo.redisPassword) {
    cmd += ` -e CSCAN_REDIS_PASSWORD=${installInfo.redisPassword}`
  }
  cmd += ` \\\nregistry.cn-hangzhou.aliyuncs.com/txf7/cscan-worker:latest`
  return cmd
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

let isComponentAlive = false

onMounted(() => {
  isComponentAlive = true
  loadData()
  startWorkerRefresh()
})

onUnmounted(() => {
  isComponentAlive = false
  stopWorkerRefresh()
})

async function loadData() {
  loading.value = true
  try {
    const res = await request.post('/worker/list')
    if (!isComponentAlive) return
    if (res.code === 0) tableData.value = res.list || []
  } finally {
    if (isComponentAlive) loading.value = false
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
      installInfo.mongoUri = res.mongoUri || ''
      installInfo.redisAddr = res.redisAddr || ''
      installInfo.redisPassword = res.redisPassword || ''
      installInfo.commands = res.commands || {}
    } else {
      ElMessage.error(res.msg || t('worker.getInstallCommandFailed'))
    }
  } catch (e) {
    ElMessage.error(t('worker.getInstallCommandFailed') + ': ' + e.message)
  }
}

async function refreshInstallKey() {
  const workerCount = tableData.value.length
  try {
    await ElMessageBox.confirm(
      t('worker.refreshKeyConfirmMsg', { count: workerCount }),
      t('worker.refreshKeyConfirmTitle'),
      { type: 'warning', confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel') }
    )
  } catch (e) {
    return
  }
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

// ==================== Worker 日志内联面板 ====================
const workerLogPanelVisible = ref(false)
const logDialogWorker = ref('')
const logLines = ref([])
const workerLogBox = ref(null)
const logSearch = ref('')
const logLevelFilter = ref('all')
const logIncludeDebug = ref(false)
const logLoading = ref(false)

const filteredLogLines = computed(() => {
  const kw = logSearch.value.trim().toLowerCase()
  const lf = logLevelFilter.value
  return logLines.value.filter(l => {
    if (lf !== 'all' && l.level !== lf) return false
    if (kw && !(l.body || '').toLowerCase().includes(kw) && !(l.taskId || '').toLowerCase().includes(kw)) return false
    return true
  })
})

async function fetchWorkerLogs() {
  const workerName = logDialogWorker.value
  if (!workerName) return
  logLoading.value = true
  try {
    const res = await request.post('/worker/logs/history', {
      worker: workerName,
      limit: 500,
      includeDebug: logIncludeDebug.value || logLevelFilter.value === 'DEBUG'
    })
    if (res.code === 0) {
      const list = res.list || []
      logLines.value = list.map(item => ({
        level: (item.level || '').toUpperCase(),
        time: formatLogTime(item.ts || ''),
        taskId: item.taskId || '',
        body: item.msg || ''
      }))
      nextTick(() => {
        const el = workerLogBox.value
        if (el) el.scrollTop = el.scrollHeight
      })
    } else if (res.msg) {
      ElMessage.error(res.msg)
    }
  } catch (e) {
    // ignore
  } finally {
    logLoading.value = false
  }
}

function formatLogTime(ts) {
  if (!ts) return ''
  // Handle ISO 8601 format: 2026-07-30T17:43:03.439+08:00
  if (ts.includes('T')) {
    const parts = ts.split('T')
    if (parts.length > 1) {
      return parts[1].split('.')[0] || parts[1]
    }
  }
  const parts = ts.split(' ')
  return parts.length > 1 ? parts[1] : ts
}

function toggleLogPanel(workerName) {
  // 如果点击的是同一个 Worker，切换关闭
  if (workerLogPanelVisible.value && logDialogWorker.value === workerName) {
    closeLogPanel()
    return
  }
  logDialogWorker.value = workerName
  workerLogPanelVisible.value = true
  logLines.value = []
  logSearch.value = ''
  logLevelFilter.value = 'all'
  logIncludeDebug.value = false
  fetchWorkerLogs()
}

function closeLogPanel() {
  workerLogPanelVisible.value = false
  logDialogWorker.value = ''
  logLines.value = []
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
      background: var(--code-bg);
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

/* ========== Worker 日志内联面板 ========== */
.log-panel-card {
  transition: all 0.3s ease;
  border-top: 2px solid var(--el-color-primary) !important;
  margin-bottom: 20px;
}
.log-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}
.log-panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.log-panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.log-panel-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding-top: 8px;
  border-top: 1px solid var(--el-border-color-lighter);
  margin-top: 8px;
}
.log-count-badge {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

/* 日志内容区 (复用 WorkerLogs 视觉) */
.worker-log-container {
  height: 500px;
  overflow-y: auto;
  border-radius: 6px;
  padding: 8px 0;
  font-family: 'Cascadia Code', 'JetBrains Mono', 'Consolas', 'Menlo', monospace;
  font-size: 13px;
  line-height: 1.7;
  /* Light mode (default) */
  background: #f8f9fc;
}
.worker-log-container.is-dark {
  background: #1a1b26;
}
.log-empty-state {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--el-text-color-secondary);
  font-size: 14px;
}
.wlog-line {
  display: flex;
  align-items: baseline;
  gap: 0;
  padding: 2px 12px 2px 0;
  transition: background 0.1s;
  &:nth-child(odd) { background: rgba(0, 0, 0, 0.02); }
  &:hover { background: rgba(0, 0, 0, 0.05); }
}
.worker-log-container.is-dark .wlog-line {
  &:nth-child(odd) { background: rgba(255, 255, 255, 0.02); }
  &:hover { background: rgba(255, 255, 255, 0.06); }
}
.wlog-ln {
  display: inline-block;
  width: 48px;
  min-width: 48px;
  text-align: right;
  padding-right: 10px;
  font-size: 11px;
  user-select: none;
  flex-shrink: 0;
  color: #9aa0b8;
}
.worker-log-container.is-dark .wlog-ln { color: #565f89; }
.wlog-level {
  display: inline-block;
  min-width: 48px;
  padding: 0 6px;
  margin-right: 6px;
  text-align: center;
  font-size: 10px;
  font-weight: 600;
  border-radius: 3px;
  flex-shrink: 0;
  letter-spacing: 0.5px;
}
/* Light level badges */
.wlevel-error, .wlevel-fatal { color: #fff; background: #f56c6c; }
.wlevel-warn, .wlevel-slow { color: #fff; background: #e6a23c; }
.wlevel-info { color: #2d7d2d; background: rgba(103, 194, 58, 0.15); }
.wlevel-debug { color: #606266; background: rgba(144, 147, 153, 0.12); }
.wlevel-log { color: #3b6ff5; background: rgba(59, 111, 245, 0.08); }
/* Dark level badges */
.worker-log-container.is-dark .wlevel-error,
.worker-log-container.is-dark .wlevel-fatal { color: #fff; background: rgba(247, 118, 142, 0.8); }
.worker-log-container.is-dark .wlevel-warn,
.worker-log-container.is-dark .wlevel-slow { color: #1a1b26; background: rgba(224, 175, 104, 0.85); }
.worker-log-container.is-dark .wlevel-info { color: #9ece6a; background: rgba(158, 206, 106, 0.12); }
.worker-log-container.is-dark .wlevel-debug { color: #565f89; background: rgba(86, 95, 137, 0.15); }
.worker-log-container.is-dark .wlevel-log { color: #7aa2f7; background: rgba(122, 162, 247, 0.1); }
.wlog-time {
  font-size: 12px;
  margin-right: 8px;
  white-space: nowrap;
  flex-shrink: 0;
  min-width: 110px;
  color: #9699a3;
}
.worker-log-container.is-dark .wlog-time { color: #565f89; }
.wlog-task {
  display: inline-block;
  padding: 0 5px;
  margin-right: 6px;
  font-size: 11px;
  color: #3b6ff5;
  background: rgba(59, 111, 245, 0.08);
  border-radius: 3px;
  flex-shrink: 0;
}
.worker-log-container.is-dark .wlog-task {
  color: #7aa2f7;
  background: rgba(122, 162, 247, 0.1);
}
.wlog-body {
  color: #343b58;
  word-break: break-all;
  white-space: pre-wrap;
  flex: 1;
  min-width: 0;
}
.worker-log-container.is-dark .wlog-body { color: #c0caf5; }
.worker-log-container.is-dark .wlog-error .wlog-body { color: #f7768e; }
.worker-log-container.is-dark .wlog-warn .wlog-body { color: #e0af68; }
.worker-log-container.is-dark .wlog-debug .wlog-body { color: #565f89; }
.wlog-error .wlog-body { color: #c64343; }
.wlog-warn .wlog-body { color: #8f5e15; }
.wlog-debug .wlog-body { color: #9699a3; }
</style>
