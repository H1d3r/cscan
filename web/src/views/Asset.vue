<template>
  <div class="asset-page">
    <!-- 搜索和统计区域 -->
    <el-card class="search-card">
      <!-- Tab切换 -->
      <el-tabs v-model="activeTab" class="search-tabs">
        <el-tab-pane :label="$t('asset.quickSearch')" name="quick">
          <div class="quick-search-form">
            <div class="search-row">
              <div class="search-item">
                <label class="search-label">{{ $t('asset.host') }}</label>
                <el-input v-model="searchForm.host" :placeholder="$t('asset.ipOrDomain')" clearable @keyup.enter="handleSearch">
                  <template #prefix>
                    <el-icon><Monitor /></el-icon>
                  </template>
                </el-input>
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.port') }}</label>
                <el-input v-model.number="searchForm.port" :placeholder="$t('asset.portNumber')" clearable @keyup.enter="handleSearch">
                  <template #prefix>
                    <el-icon><Connection /></el-icon>
                  </template>
                </el-input>
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.service') }}</label>
                <el-input v-model="searchForm.service" placeholder="http/ssh..." clearable @keyup.enter="handleSearch">
                  <template #prefix>
                    <el-icon><Service /></el-icon>
                  </template>
                </el-input>
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.pageTitle') }}</label>
                <el-input v-model="searchForm.title" :placeholder="$t('asset.webPageTitle')" clearable @keyup.enter="handleSearch">
                  <template #prefix>
                    <el-icon><Document /></el-icon>
                  </template>
                </el-input>
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.app') }}</label>
                <el-input v-model="searchForm.app" :placeholder="$t('asset.fingerprintApp')" clearable @keyup.enter="handleSearch">
                  <template #prefix>
                    <el-icon><Cpu /></el-icon>
                  </template>
                </el-input>
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('task.organization') }}</label>
                <el-select v-model="searchForm.orgId" :placeholder="$t('common.allOrganizations')" clearable @change="handleSearch">
                  <el-option :label="$t('common.allOrganizations')" value="" />
                  <el-option
                    v-for="org in organizations"
                    :key="org.id"
                    :label="org.name"
                    :value="org.id"
                  />
                </el-select>
              </div>
            </div>
          </div>
        </el-tab-pane>
      <el-tab-pane :label="$t('asset.syntaxSearch')" name="syntax">
          <el-input v-model="searchForm.query" :placeholder="$t('asset.syntaxPlaceholder')" style="width: 100%" @keyup.enter="handleSearch" />
          <div class="syntax-hints">
            <span class="hint-title">{{ $t('asset.syntaxExample') }}</span>
            <span class="hint-item" @click="searchForm.query = 'port=80'">port=80</span>
            <span class="hint-item" @click="searchForm.query = 'port=80 && service=http'">port=80 && service=http</span>
            <span class="hint-item" @click="searchForm.query = 'title=&quot;后台管理&quot;'">title="后台管理"</span>
            <span class="hint-item" @click="searchForm.query = 'app=nginx && port=443'">app=nginx && port=443</span>
          </div>
        </el-tab-pane>
        <el-tab-pane :label="$t('asset.statistics')" name="stat">
          <div class="stat-panel">
            <div class="stat-column">
              <div class="stat-title">Port</div>
              <div v-for="item in stat.topPorts" :key="'port-'+item.name" class="stat-item" @click="quickFilter('port', item.name)">
                <span class="stat-count">{{ item.count }}</span>
                <span class="stat-name">{{ item.name }}</span>
              </div>
            </div>
            <div class="stat-column">
              <div class="stat-title">Service</div>
              <div v-for="item in stat.topService" :key="'svc-'+item.name" class="stat-item" @click="quickFilter('service', item.name)">
                <span class="stat-count">{{ item.count }}</span>
                <span class="stat-name">{{ item.name }}</span>
              </div>
            </div>
            <div class="stat-column">
              <div class="stat-title">App</div>
              <div v-for="item in stat.topApp" :key="'app-'+item.name" class="stat-item" @click="quickFilter('app', item.name)">
                <span class="stat-count">{{ item.count }}</span>
                <span class="stat-name">{{ item.name }}</span>
              </div>
            </div>
            <div class="stat-column">
              <div class="stat-title">Title</div>
              <div v-for="item in stat.topTitle" :key="'title-'+item.name" class="stat-item" @click="quickFilter('title', item.name)">
                <span class="stat-count">{{ item.count }}</span>
                <span class="stat-name">{{ item.name }}</span>
              </div>
            </div>
            <div class="stat-column filter-column">
              <div class="filter-options">
                <el-checkbox v-model="searchForm.onlyUpdated">{{ $t('asset.onlyUpdated') }}</el-checkbox>
                <el-checkbox v-model="searchForm.sortByUpdate">{{ $t('asset.sortByUpdate') }}</el-checkbox>
              </div>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
      <div class="search-actions">
        <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
        <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
      </div>
    </el-card>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <div class="table-header">
        <span class="total-info">{{ $t('asset.totalRecords', { total: pagination.total, current: tableData.length }) }}</span>
        <div class="table-actions">
          <el-dropdown :disabled="selectedRows.length === 0" @command="handleQuickScan">
            <el-button type="primary" size="small" :disabled="selectedRows.length === 0">
              <el-icon><VideoPlay /></el-icon>{{ $t('asset.quickScan') }} ({{ selectedRows.length }})
              <el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="portscan">{{ $t('task.portScan') }}</el-dropdown-item>
                <el-dropdown-item command="portidentify">{{ $t('task.portIdentify') }} (Nmap)</el-dropdown-item>
                <el-dropdown-item command="fingerprint">{{ $t('task.fingerprintScan') }}</el-dropdown-item>
                <el-dropdown-item command="pocscan">{{ $t('task.vulScan') }}</el-dropdown-item>
                <el-dropdown-item divided command="custom">{{ $t('asset.customScan') }}...</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-button type="danger" size="small" :disabled="selectedRows.length === 0" @click="handleBatchDelete">
            <el-icon><Delete /></el-icon>{{ $t('common.batchDelete') }} ({{ selectedRows.length }})
          </el-button>
        </div>
      </div>
      <el-table :data="tableData" v-loading="loading" stripe size="small" max-height="500" @selection-change="handleSelectionChange">
        <el-table-column type="selection" width="40" />
        <el-table-column type="index" :label="$t('asset.index')" width="60" />
        <el-table-column :label="$t('asset.assetOrg')" min-width="160">
          <template #default="{ row }">
            <div class="asset-cell">
              <a :href="getAssetUrl(row)" target="_blank" class="asset-link">{{ row.authority }}</a>
              <el-icon v-if="row.authority" class="link-icon"><Link /></el-icon>
            </div>
            <div class="org-text">{{ row.orgName || $t('common.defaultOrganization') }}</div>
          </template>
        </el-table-column>
        <el-table-column label="IP" width="140">
          <template #default="{ row }">
            <div>{{ row.host }}</div>
            <div v-if="row.location && row.location !== 'IANA'" class="location-text">{{ row.location }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.portProtocol')" width="120">
          <template #default="{ row }">
            <span class="port-text">{{ row.port }}</span>
            <span v-if="row.service" class="service-text">{{ row.service }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.titleFingerprint')" min-width="200">
          <template #default="{ row }">
            <div class="title-text" :title="row.title">{{ row.title || '-' }}</div>
            <div class="app-tags-container">
              <el-tooltip 
                v-for="app in (row.app || [])" 
                :key="app" 
                :content="getAppTooltip(app)"
                placement="top"
              >
                <el-tag 
                  size="small" 
                  :type="getAppTagType(app)" 
                  class="app-tag"
                  :class="{ 'clickable-tag': isCustomFingerprint(app) }"
                  @click="handleAppTagClick(app)"
                >
                  {{ getAppName(app) }}
                </el-tag>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.fingerprintInfo')" min-width="320">
          <template #default="{ row }">
            <div class="fingerprint-info">
              <el-tabs v-if="row.httpHeader || row.httpStatus || row.httpBody || row.banner || row.iconHash" type="border-card" class="fingerprint-tabs">
                <el-tab-pane label="Header">
                  <pre v-if="row.httpHeader || row.httpStatus" class="tab-content">{{ formatHeaderWithStatus(row) }}</pre>
                  <pre v-else-if="row.banner" class="tab-content">{{ row.banner }}</pre>
                  <span v-else class="no-data">-</span>
                </el-tab-pane>
                <el-tab-pane label="Body">
                  <pre v-if="row.httpBody" class="tab-content">{{ row.httpBody }}</pre>
                  <span v-else class="no-data">{{ $t('asset.noContent') }}</span>
                </el-tab-pane>
                <el-tab-pane label="IconHash">
                  <div v-if="row.iconHash" class="icon-hash-content">
                    <el-tooltip :content="$t('asset.clickToCopy')" placement="top">
                      <span class="icon-hash-value" @click="copyIconHash(row.iconHash)">{{ row.iconHash }}</span>
                    </el-tooltip>
                  </div>
                  <span v-else class="no-data">{{ $t('asset.noIconHash') }}</span>
                </el-tab-pane>
              </el-tabs>
              <span v-else class="no-data">-</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.webScreenshot')" width="100" align="center">
          <template #default="{ row }">
            <el-image 
              v-if="row.screenshot" 
              :src="getScreenshotUrl(row.screenshot)" 
              :preview-src-list="[getScreenshotUrl(row.screenshot)]"
              :z-index="9999"
              :preview-teleported="true"
              :hide-on-click-modal="true"
              fit="cover"
              class="screenshot-img"
            />
            <span v-else class="no-screenshot">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.updateTime')" width="160">
          <template #default="{ row }">
            <div class="update-time">{{ formatTime(row.updateTime) }}</div>
            <el-tag v-if="row.isNew" type="success" size="small" effect="dark" class="mark-tag">{{ $t('common.new') }}</el-tag>
            <el-tag v-if="row.isUpdated" type="warning" size="small" effect="dark" class="mark-tag" style="cursor: pointer" @click="showHistory(row)">{{ $t('asset.updated') }}</el-tag>
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

    <!-- 历史记录对话框 -->
    <el-dialog v-model="historyVisible" :title="$t('asset.scanHistory')" width="800px">
      <div v-if="currentAsset" class="history-current">
        <span>{{ $t('asset.currentAsset') }}: </span>
        <strong>{{ currentAsset.authority }}</strong>
        <span class="text-secondary" style="margin-left: 15px">{{ currentAsset.host }}:{{ currentAsset.port }}</span>
      </div>
      <el-table :data="historyList" v-loading="historyLoading" stripe size="small" max-height="400">
        <el-table-column prop="createTime" :label="$t('asset.scanTime')" width="160" />
        <el-table-column prop="title" :label="$t('asset.pageTitle')" min-width="150" show-overflow-tooltip />
        <el-table-column prop="service" :label="$t('asset.service')" width="80" />
        <el-table-column prop="httpStatus" :label="$t('asset.statusCode')" width="80" />
        <el-table-column :label="$t('asset.app')" min-width="150">
          <template #default="{ row }">
            <el-tag v-for="app in (row.app || []).slice(0, 3)" :key="app" size="small" type="success" style="margin-right: 4px">
              {{ app }}
            </el-tag>
            <span v-if="(row.app || []).length > 3" class="hint-secondary">+{{ (row.app || []).length - 3 }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.screenshot')" width="80" align="center">
          <template #default="{ row }">
            <el-image 
              v-if="row.screenshot" 
              :src="getScreenshotUrl(row.screenshot)" 
              :preview-src-list="[getScreenshotUrl(row.screenshot)]"
              :z-index="9999"
              :preview-teleported="true"
              fit="cover"
              style="width: 50px; height: 40px; border-radius: 4px"
            />
            <span v-else class="text-secondary">-</span>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="!historyLoading && historyList.length === 0" class="history-empty">
        {{ $t('asset.noHistory') }}
      </div>
    </el-dialog>

    <!-- 自定义扫描配置对话框 -->
    <el-dialog v-model="scanDialogVisible" :title="$t('asset.scanConfig')" width="650px" top="5vh">
      <div class="scan-target-info">
        <el-icon><Aim /></el-icon>
        <span>{{ $t('asset.selectedAssets', { count: selectedRows.length }) }}</span>
      </div>
      <el-form label-width="100px" class="scan-form">
        <el-form-item :label="$t('task.taskName')">
          <el-input v-model="scanForm.name" :placeholder="$t('asset.assetScanTask')" />
        </el-form-item>
        
        <el-divider content-position="left">{{ $t('task.portScan') }}</el-divider>
        <el-form-item :label="$t('task.enable')">
          <el-switch v-model="scanForm.portscanEnable" />
          <span class="form-hint">{{ $t('asset.rescanPortHint') }}</span>
        </el-form-item>
        <template v-if="scanForm.portscanEnable">
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item :label="$t('task.scanTool')">
                <el-radio-group v-model="scanForm.portscanTool">
                  <el-radio label="naabu">Naabu</el-radio>
                  <el-radio label="masscan">Masscan</el-radio>
                </el-radio-group>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item :label="$t('task.portRange')">
                <el-select v-model="scanForm.ports" style="width: 100%">
                  <el-option label="top100" value="top100" />
                  <el-option label="top1000" value="top1000" />
                  <el-option :label="$t('task.allPorts') + ' 1-65535'" value="1-65535" />
                </el-select>
              </el-form-item>
            </el-col>
          </el-row>
          <el-row :gutter="20">
            <el-col :span="8">
              <el-form-item :label="$t('task.scanRate')">
                <el-input-number v-model="scanForm.portscanRate" :min="100" :max="100000" style="width:100%" />
              </el-form-item>
            </el-col>
            <el-col :span="8">
              <el-form-item :label="$t('task.portThreshold')">
                <el-input-number v-model="scanForm.portThreshold" :min="0" :max="65535" style="width:100%" />
              </el-form-item>
            </el-col>
            <el-col :span="8">
              <el-form-item :label="$t('task.timeoutSeconds')">
                <el-input-number v-model="scanForm.portscanTimeout" :min="5" :max="1200" style="width:100%" />
              </el-form-item>
            </el-col>
          </el-row>
          <el-form-item :label="$t('task.advancedOptions')">
            <div style="display: block; width: 100%">
              <el-checkbox v-model="scanForm.skipHostDiscovery">{{ $t('task.skipHostDiscovery') }} (-Pn)</el-checkbox>
            </div>
            <div v-if="scanForm.portscanTool === 'naabu'" style="display: block; width: 100%; margin-top: 8px">
              <el-checkbox v-model="scanForm.excludeCDN">{{ $t('task.excludeCdnWaf') }} (-ec)</el-checkbox>
            </div>
          </el-form-item>
          <el-form-item :label="$t('task.excludeTargets')">
            <el-input v-model="scanForm.excludeHosts" placeholder="192.168.1.1,10.0.0.0/8" />
            <span class="form-hint">{{ $t('task.excludeTargetsHint') }}</span>
          </el-form-item>
        </template>
        
        <el-divider content-position="left">{{ $t('task.portIdentify') }}</el-divider>
        <el-form-item :label="$t('task.enable')">
          <el-switch v-model="scanForm.portidentifyEnable" />
          <span class="form-hint">{{ $t('asset.nmapServiceProbe') }}</span>
        </el-form-item>
        <template v-if="scanForm.portidentifyEnable">
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item :label="$t('task.timeoutSeconds')">
                <el-input-number v-model="scanForm.portidentifyTimeout" :min="5" :max="300" style="width:100%" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item :label="$t('task.nmapParams')">
                <el-input v-model="scanForm.portidentifyArgs" placeholder="-sV" />
              </el-form-item>
            </el-col>
          </el-row>
        </template>
        
        <el-divider content-position="left">{{ $t('task.fingerprintScan') }}</el-divider>
        <el-form-item :label="$t('task.enable')">
          <el-switch v-model="scanForm.fingerprintEnable" />
          <span class="form-hint">{{ $t('asset.webFingerprintProbe') }}</span>
        </el-form-item>
        <template v-if="scanForm.fingerprintEnable">
          <el-form-item :label="$t('task.probeTool')">
            <el-radio-group v-model="scanForm.fingerprintTool">
              <el-radio label="httpx">Httpx</el-radio>
              <el-radio label="builtin">Wappalyzer</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="$t('task.additionalFeatures')">
            <el-checkbox v-model="scanForm.fingerprintIconHash">{{ $t('task.iconHash') }}</el-checkbox>
            <el-checkbox v-model="scanForm.fingerprintCustomEngine">{{ $t('task.customFingerprint') }}</el-checkbox>
            <el-checkbox v-model="scanForm.fingerprintScreenshot">{{ $t('task.screenshot') }}</el-checkbox>
          </el-form-item>
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item :label="$t('task.timeoutSeconds')">
                <el-input-number v-model="scanForm.fingerprintTimeout" :min="5" :max="120" style="width:100%" />
                <span class="form-hint">{{ $t('task.concurrentByWorker') }}</span>
              </el-form-item>
            </el-col>
          </el-row>
        </template>
        
        <el-divider content-position="left">{{ $t('task.vulScan') }}</el-divider>
        <el-form-item :label="$t('task.enable')">
          <el-switch v-model="scanForm.pocscanEnable" />
          <span class="form-hint">{{ $t('task.useNucleiEngine') }}</span>
        </el-form-item>
        <template v-if="scanForm.pocscanEnable">
          <el-form-item :label="$t('asset.scanMode')">
            <el-checkbox v-model="scanForm.pocscanAutoScan" :disabled="scanForm.pocscanCustomOnly">{{ $t('task.customTagMapping') }}</el-checkbox>
            <el-checkbox v-model="scanForm.pocscanAutomaticScan" :disabled="scanForm.pocscanCustomOnly">{{ $t('task.webFingerprintAutoMatch') }}</el-checkbox>
            <el-checkbox v-model="scanForm.pocscanCustomOnly">{{ $t('task.onlyUseCustomPoc') }}</el-checkbox>
          </el-form-item>
          <el-form-item :label="$t('task.severityLevel')">
            <el-checkbox-group v-model="scanForm.pocscanSeverity">
              <el-checkbox label="critical">Critical</el-checkbox>
              <el-checkbox label="high">High</el-checkbox>
              <el-checkbox label="medium">Medium</el-checkbox>
              <el-checkbox label="low">Low</el-checkbox>
              <el-checkbox label="info">Info</el-checkbox>
            </el-checkbox-group>
          </el-form-item>
          <el-form-item :label="$t('task.targetTimeout')">
            <el-input-number v-model="scanForm.pocscanTargetTimeout" :min="30" :max="600" />
            <span class="form-hint">{{ $t('task.seconds') }}</span>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="scanDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="scanSubmitting" @click="handleScanSubmit">{{ $t('asset.createAndStart') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Link, Monitor, Connection, Service, Document, Cpu, VideoPlay, ArrowDown, Aim } from '@element-plus/icons-vue'
import { getAssetList, getAssetStat, deleteAsset, batchDeleteAsset, getAssetHistory } from '@/api/asset'
import { createTask, startTask } from '@/api/task'
import { useWorkspaceStore } from '@/stores/workspace'
import request from '@/api/request'

const { t } = useI18n()

const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const tableData = ref([])
const selectedRows = ref([])
const activeTab = ref('quick')
const historyVisible = ref(false)
const historyLoading = ref(false)
const historyList = ref([])
const currentAsset = ref(null)
const organizations = ref([])
const scanDialogVisible = ref(false)
const scanSubmitting = ref(false)

const scanForm = reactive({
  name: '',
  portscanEnable: false,
  portscanTool: 'naabu',
  portscanRate: 1000,
  ports: 'top100',
  portThreshold: 100,
  scanType: 'c',
  portscanTimeout: 60,
  skipHostDiscovery: false,
  excludeCDN: false,
  excludeHosts: '',
  portidentifyEnable: false,
  portidentifyTimeout: 30,
  portidentifyArgs: '',
  fingerprintEnable: true,
  fingerprintTool: 'httpx',
  fingerprintIconHash: true,
  fingerprintCustomEngine: true,
  fingerprintScreenshot: false,
  fingerprintTimeout: 30,
  pocscanEnable: false,
  pocscanAutoScan: true,
  pocscanAutomaticScan: true,
  pocscanCustomOnly: false,
  pocscanSeverity: ['critical', 'high', 'medium'],
  pocscanTargetTimeout: 600
})

const stat = reactive({
  total: 0,
  newCount: 0,
  updatedCount: 0,
  topPorts: [],
  topService: [],
  topApp: [],
  topTitle: []
})

const searchForm = reactive({
  query: '',
  host: '',
  port: null,
  service: '',
  title: '',
  app: '',
  orgId: '',
  onlyUpdated: false,
  sortByUpdate: true
})

const pagination = reactive({
  page: 1,
  pageSize: 50,
  total: 0
})

// 监听工作空间切换
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
    const params = {
      page: pagination.page,
      pageSize: pagination.pageSize,
      onlyUpdated: searchForm.onlyUpdated,
      sortByUpdate: searchForm.sortByUpdate,
      workspaceId: workspaceStore.currentWorkspaceId || '',
      orgId: searchForm.orgId || ''
    }

    // 根据当前Tab决定使用哪种查询方式
    if (activeTab.value === 'syntax' && searchForm.query) {
      params.query = searchForm.query
    } else {
      params.host = searchForm.host
      params.port = searchForm.port
      params.service = searchForm.service
      params.title = searchForm.title
      params.app = searchForm.app
    }

    const res = await getAssetList(params)
    if (res.code === 0) {
      tableData.value = res.list || []
      pagination.total = res.total
    }
  } finally {
    loading.value = false
  }
}

async function loadStat() {
  const res = await getAssetStat({ workspaceId: workspaceStore.currentWorkspaceId || '' })
  if (res.code === 0) {
    stat.total = res.totalAsset || 0
    stat.newCount = res.newCount || 0
    stat.updatedCount = res.updatedCount || 0
    stat.topPorts = res.topPorts || []
    stat.topService = res.topService || []
    stat.topApp = res.topApp || []
    stat.topTitle = res.topTitle || []
  }
}

async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    // 处理嵌套响应结构
    const data = res.data || res
    if (data.code === 0) {
      // 资产筛选显示所有组织（包括禁用的，用于查看历史数据）
      organizations.value = data.list || []
    }
  } catch (e) {
    console.error('Failed to load organizations:', e)
  }
}

function quickFilter(field, value) {
  // 端口需要转换为数字
  if (field === 'port') {
    searchForm[field] = parseInt(value, 10)
  } else {
    searchForm[field] = value
  }
  activeTab.value = 'quick'
  handleSearch()
}

function getAssetUrl(row) {
  if (row.service === 'https' || row.port === 443) {
    return `https://${row.host}:${row.port}`
  }
  return `http://${row.host}:${row.port}`
}

function formatHeader(header) {
  if (!header) return ''
  // 限制显示行数，最多显示10行
  const lines = header.split('\n').slice(0, 10)
  return lines.join('\n')
}

function formatHeaderWithStatus(row) {
  let result = ''
  // 添加状态行
  if (row.httpStatus) {
    const statusText = getStatusText(row.httpStatus)
    result = `HTTP/1.1 ${row.httpStatus} ${statusText}\n`
  }
  // 添加Header内容
  if (row.httpHeader) {
    result += row.httpHeader
  }
  return result
}

function getStatusText(status) {
  const statusMap = {
    '200': 'OK',
    '201': 'Created',
    '204': 'No Content',
    '301': 'Moved Permanently',
    '302': 'Found',
    '304': 'Not Modified',
    '400': 'Bad Request',
    '401': 'Unauthorized',
    '403': 'Forbidden',
    '404': 'Not Found',
    '500': 'Internal Server Error',
    '502': 'Bad Gateway',
    '503': 'Service Unavailable'
  }
  return statusMap[status] || ''
}

function getScreenshotUrl(screenshot) {
  if (!screenshot) return ''
  // 如果是base64格式
  if (screenshot.startsWith('data:') || screenshot.startsWith('/9j/') || screenshot.startsWith('iVBOR')) {
    if (!screenshot.startsWith('data:')) {
      return `data:image/png;base64,${screenshot}`
    }
    return screenshot
  }
  // 如果是文件路径
  return `/api/screenshot/${screenshot}`
}

function formatTime(time) {
  if (!time) return '-'
  // 显示完整时间（精确到秒）
  return time
}

function handleSearch() {
  pagination.page = 1
  loadData()
}

function handleReset() {
  Object.assign(searchForm, {
    query: '',
    host: '',
    port: null,
    service: '',
    title: '',
    app: '',
    orgId: '',
    onlyUpdated: false,
    sortByUpdate: true
  })
  handleSearch()
  loadStat()
}

async function handleDelete(row) {
  await ElMessageBox.confirm(t('asset.confirmDeleteAsset'), t('common.tip'), { type: 'warning' })
  const res = await deleteAsset({ id: row.id })
  if (res.code === 0) {
    ElMessage.success(t('common.deleteSuccess'))
    loadData()
    loadStat()
  }
}

function handleSelectionChange(rows) {
  selectedRows.value = rows
}

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  await ElMessageBox.confirm(t('asset.confirmBatchDeleteAsset', { count: selectedRows.value.length }), t('common.tip'), { type: 'warning' })
  const ids = selectedRows.value.map(row => row.id)
  const res = await batchDeleteAsset({ ids })
  if (res.code === 0) {
    ElMessage.success(t('asset.batchDeleteSuccess', { count: selectedRows.value.length }))
    selectedRows.value = []
    loadData()
    loadStat()
  } else {
    ElMessage.error(res.msg)
  }
}

async function showHistory(row) {
  currentAsset.value = row
  historyVisible.value = true
  historyLoading.value = true
  historyList.value = []
  try {
    const res = await getAssetHistory({ assetId: row.id, limit: 20 })
    if (res.code === 0) {
      historyList.value = res.list || []
    } else {
      ElMessage.error(res.msg || t('asset.getHistoryFailed'))
    }
  } catch (e) {
    ElMessage.error(t('asset.getHistoryFailed'))
  } finally {
    historyLoading.value = false
  }
}

// 获取应用名称（去除来源标识）
function getAppName(app) {
  if (!app) return ''
  const idx = app.indexOf('[')
  return idx > 0 ? app.substring(0, idx) : app
}

// 获取应用来源（用于tooltip显示）
function getAppSource(app) {
  if (!app) return ''
  const match = app.match(/\[([^\]]+)\]$/)
  if (match) {
    const source = match[1]
    
    const sourceMap = {
      'httpx': 'httpx识别',
      'wappalyzer': 'Wappalyzer识别',
      'custom': '自定义指纹'
    }
    
    // 处理组合来源，如 httpx+wappalyzer+custom(ID1,ID2)
    if (source.includes('+')) {
      const parts = source.split('+')
      const mappedParts = parts.map(part => {
        // 处理 custom(ID1,ID2) 格式
        if (part.startsWith('custom(')) {
          const ids = part.match(/custom\(([^)]+)\)/)
          if (ids) {
            const idList = ids[1].split(',').map(id => id.trim())
            return `自定义指纹(${idList.length}个ID: ${idList.join(', ')})`
          }
          return '自定义指纹'
        }
        return sourceMap[part] || part
      })
      return mappedParts.join(' + ')
    }
    
    // 处理单一来源 custom(ID1,ID2) 格式
    if (source.startsWith('custom(')) {
      const ids = source.match(/custom\(([^)]+)\)/)
      if (ids) {
        const idList = ids[1].split(',').map(id => id.trim())
        return `自定义指纹 (${idList.length}个ID: ${idList.join(', ')})`
      }
      return '自定义指纹'
    }
    
    // 处理旧格式 custom-ID
    if (source.startsWith('custom-')) {
      const id = source.substring(7)
      return `自定义指纹 (ID: ${id})`
    }
    
    return sourceMap[source] || source
  }
  return '未知来源'
}

// 根据来源返回标签类型
// 多来源合并时使用紫色(primary)表示高可信度
function getAppTagType(app) {
  if (!app) return 'info'
  // 三个来源都匹配 - 紫色(高可信度)
  if (app.includes('[httpx+wappalyzer+custom(')) return 'danger'
  // 两个来源匹配 - 紫色
  if (app.includes('[httpx+wappalyzer]')) return 'primary'
  if (app.includes('[httpx+custom(')) return 'danger'
  if (app.includes('[wappalyzer+custom(')) return 'danger'
  // 单一来源
  if (app.includes('[httpx]')) return 'success'
  if (app.includes('[wappalyzer]')) return 'success'
  if (app.includes('[builtin]')) return 'warning'
  if (app.includes('custom(') || app.includes('[custom-')) return 'danger'
  return 'info'
}

// 判断是否包含自定义指纹
function isCustomFingerprint(app) {
  return app && (app.includes('custom(') || app.includes('[custom-'))
}

// 获取自定义指纹的ID列表
function getCustomFingerprintIds(app) {
  if (!app) return []
  
  // 匹配 custom(ID1,ID2,ID3) 格式，支持在任意位置
  const match = app.match(/custom\(([^)]+)\)/)
  if (match) {
    return match[1].split(',').map(id => id.trim())
  }
  
  // 旧格式兼容: [custom-ID]
  const oldFormatMatch = app.match(/\[custom-([^\]]+)\]$/)
  if (oldFormatMatch) {
    return [oldFormatMatch[1]]
  }
  
  return []
}

// 获取自定义指纹的ID（兼容旧代码，返回第一个ID）
function getCustomFingerprintId(app) {
  const ids = getCustomFingerprintIds(app)
  return ids.length > 0 ? ids[0] : ''
}

// 获取tooltip内容
function getAppTooltip(app) {
  if (!app) return ''
  
  // 先获取来源信息
  const sourceInfo = getAppSource(app)
  
  // 如果包含自定义指纹，添加点击复制提示
  if (isCustomFingerprint(app)) {
    const ids = getCustomFingerprintIds(app)
    if (ids.length > 0) {
      return `${sourceInfo}\n点击复制指纹ID`
    }
  }
  
  return sourceInfo
}

// 处理指纹标签点击
function handleAppTagClick(app) {
  if (isCustomFingerprint(app)) {
    const ids = getCustomFingerprintIds(app)
    if (ids.length > 0) {
      const textToCopy = ids.length > 1 ? ids.join(',') : ids[0]
      const successMsg = ids.length > 1 
        ? `已复制${ids.length}个指纹ID: ${textToCopy}` 
        : `已复制指纹ID: ${textToCopy}`
      
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(textToCopy).then(() => {
          ElMessage.success(successMsg)
        }).catch(() => {
          fallbackCopyToClipboard(textToCopy, successMsg)
        })
      } else {
        fallbackCopyToClipboard(textToCopy, successMsg)
      }
    }
  }
}

// 复制IconHash
function copyIconHash(hash) {
  const successMsg = `已复制IconHash: ${hash}`
  
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(hash).then(() => {
      ElMessage.success(successMsg)
    }).catch(() => {
      fallbackCopyToClipboard(hash, successMsg)
    })
  } else {
    fallbackCopyToClipboard(hash, successMsg)
  }
}

function fallbackCopyToClipboard(text, successMsg) {
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
      ElMessage.success(successMsg)
    } else {
      ElMessage.error('复制失败')
    }
  } catch (err) {
    console.error('复制失败:', err)
    ElMessage.error('复制失败')
  }
}

// 快速扫描处理
function handleQuickScan(command) {
  if (selectedRows.value.length === 0) {
    ElMessage.warning(t('asset.pleaseSelectAssets'))
    return
  }
  
  // 重置扫描表单（与 Task.vue 保持一致）
  Object.assign(scanForm, {
    name: '',
    portscanEnable: false,
    portscanTool: 'naabu',
    portscanRate: 1000,
    ports: 'top100',
    portThreshold: 100,
    scanType: 'c',
    portscanTimeout: 60,
    skipHostDiscovery: false,
    excludeCDN: false,
    excludeHosts: '',
    portidentifyEnable: false,
    portidentifyTimeout: 30,
    portidentifyArgs: '',
    fingerprintEnable: false,
    fingerprintTool: 'httpx',
    fingerprintIconHash: true,
    fingerprintCustomEngine: true,
    fingerprintScreenshot: false,
    fingerprintTimeout: 30,
    pocscanEnable: false,
    pocscanAutoScan: true,
    pocscanAutomaticScan: true,
    pocscanCustomOnly: false,
    pocscanSeverity: ['critical', 'high', 'medium'],
    pocscanTargetTimeout: 600
  })
  
  // 根据命令设置默认配置
  switch (command) {
    case 'portscan':
      scanForm.name = '端口扫描'
      scanForm.portscanEnable = true
      break
    case 'portidentify':
      scanForm.name = '端口识别'
      scanForm.portidentifyEnable = true
      break
    case 'fingerprint':
      scanForm.name = '指纹识别'
      scanForm.fingerprintEnable = true
      break
    case 'pocscan':
      scanForm.name = '漏洞扫描'
      scanForm.pocscanEnable = true
      break
    case 'custom':
      scanForm.name = '资产扫描'
      break
  }
  
  // 所有扫描都弹出配置对话框
  scanDialogVisible.value = true
}

// 构建扫描目标
function buildScanTargets() {
  const targets = []
  for (const row of selectedRows.value) {
    // 携带端口信息：IP:Port 格式
    if (row.host && row.port) {
      targets.push(`${row.host}:${row.port}`)
    } else if (row.host) {
      targets.push(row.host)
    }
  }
  return targets.join('\n')
}

// 构建扫描配置（与 Task.vue 的 buildConfig 保持一致）
function buildScanConfig() {
  return {
    batchSize: 50,
    portscan: {
      enable: scanForm.portscanEnable,
      tool: scanForm.portscanTool,
      rate: scanForm.portscanRate,
      ports: scanForm.ports,
      portThreshold: scanForm.portThreshold,
      scanType: scanForm.scanType,
      timeout: scanForm.portscanTimeout,
      skipHostDiscovery: scanForm.skipHostDiscovery,
      excludeCDN: scanForm.excludeCDN,
      excludeHosts: scanForm.excludeHosts
    },
    portidentify: {
      enable: scanForm.portidentifyEnable,
      timeout: scanForm.portidentifyTimeout,
      args: scanForm.portidentifyArgs
    },
    fingerprint: {
      enable: scanForm.fingerprintEnable,
      tool: scanForm.fingerprintTool,
      iconHash: scanForm.fingerprintIconHash,
      customEngine: scanForm.fingerprintCustomEngine,
      screenshot: scanForm.fingerprintScreenshot,
      targetTimeout: scanForm.fingerprintTimeout
    },
    pocscan: {
      enable: scanForm.pocscanEnable,
      useNuclei: true,
      autoScan: scanForm.pocscanAutoScan,
      automaticScan: scanForm.pocscanAutomaticScan,
      customPocOnly: scanForm.pocscanCustomOnly,
      severity: scanForm.pocscanSeverity.join(','),
      targetTimeout: scanForm.pocscanTargetTimeout
    }
  }
}

// 创建扫描任务
async function doCreateScanTask() {
  const targets = buildScanTargets()
  if (!targets) {
    ElMessage.warning(t('asset.noValidTargets'))
    return
  }
  
  const config = buildScanConfig()
  const taskName = scanForm.name || t('asset.assetScan')
  
  try {
    scanSubmitting.value = true
    const wsId = workspaceStore.currentWorkspaceId === 'all' ? '' : (workspaceStore.currentWorkspaceId || '')
    const res = await createTask({
      name: `${taskName} (${selectedRows.value.length}${t('asset.assetsCount')})`,
      target: targets,
      workspaceId: wsId,
      config: JSON.stringify(config)
    })
    
    if (res.code === 0) {
      // 创建成功后自动启动任务
      const taskId = res.id || res.taskId
      if (taskId) {
        const startRes = await startTask({ id: taskId, workspaceId: wsId || res.workspaceId })
        if (startRes.code === 0) {
          ElMessage.success(t('asset.taskCreatedAndStarted'))
        } else {
          ElMessage.success(t('asset.taskCreatedButStartFailed') + (startRes.msg || ''))
        }
      } else {
        ElMessage.success(t('asset.taskCreatedSuccess'))
      }
      scanDialogVisible.value = false
      selectedRows.value = []
    } else {
      ElMessage.error(res.msg || t('asset.createTaskFailed'))
    }
  } catch (e) {
    ElMessage.error(t('asset.createTaskFailed'))
  } finally {
    scanSubmitting.value = false
  }
}

// 提交自定义扫描
async function handleScanSubmit() {
  if (!scanForm.portscanEnable && !scanForm.portidentifyEnable && !scanForm.fingerprintEnable && !scanForm.pocscanEnable) {
    ElMessage.warning(t('asset.pleaseSelectScanStage'))
    return
  }
  await doCreateScanTask()
}
</script>

<style lang="scss" scoped>
.asset-page {
  .search-card {
    margin-bottom: 15px;
    
    .search-tabs {
      :deep(.el-tabs__header) {
        margin-bottom: 10px;
      }
      
      // 快捷查询表单样式
      .quick-search-form {
        .search-row {
          display: flex;
          flex-wrap: wrap;
          gap: 16px;
          
          .search-item {
            display: flex;
            flex-direction: column;
            min-width: 140px;
            flex: 1;
            max-width: 180px;
            
            .search-label {
              font-size: 12px;
              color: var(--el-text-color-secondary);
              margin-bottom: 6px;
              font-weight: 500;
            }
            
            :deep(.el-input) {
              .el-input__wrapper {
                border-radius: 8px;
                box-shadow: 0 0 0 1px var(--el-border-color) inset;
                transition: all 0.2s;
                
                &:hover {
                  box-shadow: 0 0 0 1px var(--el-color-primary-light-5) inset;
                }
                
                &.is-focus {
                  box-shadow: 0 0 0 1px var(--el-color-primary) inset;
                }
              }
              
              .el-input__prefix {
                color: var(--el-text-color-placeholder);
              }
            }
            
            :deep(.el-select) {
              width: 100%;
              
              .el-input__wrapper {
                border-radius: 8px;
                box-shadow: 0 0 0 1px var(--el-border-color) inset;
                transition: all 0.2s;
                
                &:hover {
                  box-shadow: 0 0 0 1px var(--el-color-primary-light-5) inset;
                }
              }
            }
          }
        }
      }
      
      .syntax-hints {
        margin-top: 8px;
        font-size: 12px;
        color: var(--el-text-color-secondary);
        
        .hint-title {
          margin-right: 10px;
        }
        
        .hint-item {
          display: inline-block;
          padding: 2px 8px;
          margin-right: 10px;
          background: var(--el-fill-color-light);
          border-radius: 3px;
          color: var(--el-text-color-regular);
          cursor: pointer;
          transition: all 0.2s;
          
          &:hover {
            background: var(--el-fill-color);
            color: var(--el-color-primary);
          }
        }
        
html.dark .hint-item:hover {
  background: rgba(64, 158, 255, 0.15);
}
      }
    }

    .search-actions {
      margin-top: 16px;
      padding-top: 12px;
      border-top: 1px solid var(--el-border-color-lighter);
      text-align: right;
      
      .el-button {
        min-width: 80px;
      }
    }
  }

  .stat-panel {
    display: flex;
    gap: 30px;
    
    .stat-column {
      min-width: 140px;
      
      .stat-title {
        font-weight: bold;
        color: var(--el-text-color-primary);
        margin-bottom: 8px;
        padding-bottom: 5px;
        border-bottom: 2px solid var(--el-color-primary);
      }
      
      .stat-item {
        display: flex;
        align-items: center;
        padding: 3px 0;
        cursor: pointer;
        
        &:hover {
          background: var(--el-fill-color);
        }
        
        .stat-count {
          display: inline-block;
          min-width: 30px;
          padding: 1px 6px;
          margin-right: 8px;
          background: var(--el-color-primary);
          color: var(--el-color-white);
          border-radius: 3px;
          font-size: 12px;
          text-align: center;
        }
        
        .stat-name {
          color: var(--el-color-primary);
          font-size: 13px;
        }
      }
    }
    
    .filter-column {
      margin-left: auto;
      
      .filter-options {
        display: flex;
        flex-direction: column;
        gap: 8px;
      }
    }
  }

  .table-card {
    .table-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 10px;
      
      .total-info {
        color: var(--el-text-color-regular);
        font-size: 13px;
      }
    }

    .asset-cell {
      display: flex;
      align-items: center;
      
      .asset-link {
        color: var(--el-color-primary);
        text-decoration: none;
        
        &:hover {
          text-decoration: underline;
        }
      }
      
      .link-icon {
        margin-left: 4px;
        font-size: 12px;
        color: var(--el-color-primary);
      }
    }
    
    .org-text, .location-text {
      color: var(--el-text-color-secondary);
      font-size: 12px;
    }

    .port-text {
      font-weight: 500;
      margin-right: 8px;
    }

    .service-text {
      color: var(--el-color-success);
      font-size: 12px;
    }
    
    .title-text {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .app-tags-container {
      display: flex;
      flex-wrap: wrap;
      gap: 4px;
      max-height: 80px;
      overflow-y: auto;
    }

    .app-tag {
      margin: 0;
      flex-shrink: 0;
    }

    .clickable-tag {
      cursor: pointer;
      
      &:hover {
        opacity: 0.8;
        transform: scale(1.05);
      }
    }

    .mark-tag {
      margin-right: 4px;
      margin-top: 2px;
    }

    .fingerprint-info {
      .fingerprint-tabs {
        :deep(.el-tabs__header) {
          margin-bottom: 0;
          background: var(--el-fill-color-darker);
          border-color: var(--el-border-color);
          border-radius: 4px 4px 0 0;
        }
        
        :deep(.el-tabs__nav) {
          border: none;
        }
        
        :deep(.el-tabs__item) {
          padding: 0 12px;
          height: 28px;
          line-height: 28px;
          font-size: 12px;
          color: var(--el-text-color-secondary);
          border: none;
          
          &.is-active {
            color: var(--el-color-primary);
            background: var(--el-bg-color-overlay);
            border-radius: 4px 4px 0 0;
          }
          
          &:hover {
            color: var(--el-color-primary);
          }
        }
        
        :deep(.el-tabs__content) {
          padding: 0;
          background: transparent;
        }
        
        :deep(.el-tab-pane) {
          padding: 0;
        }
      }
      
      .tab-content {
        margin: 0;
        padding: 8px;
        background: var(--el-fill-color-light);
        font-size: 11px;
        line-height: 1.5;
        max-height: 160px;
        overflow-y: auto;
        white-space: pre-wrap;
        word-break: break-all;
        color: var(--el-text-color-regular);
        font-family: Consolas, Monaco, monospace;
        border: 1px solid var(--el-border-color);
        border-top: none;
        border-radius: 0 0 4px 4px;
      }
      
      .no-data {
        display: block;
        padding: 10px;
        color: var(--el-text-color-secondary);
        font-size: 12px;
        text-align: center;
        background: var(--el-fill-color-light);
      }
    }

    .screenshot-img {
      width: 80px;
      height: 60px;
      border-radius: 4px;
      cursor: pointer;
      border: 1px solid var(--el-border-color);
    }

    .no-screenshot {
      color: var(--el-text-color-secondary);
    }

    .icon-hash-content {
      padding: 8px;
      background: var(--el-fill-color-light);
      border: 1px solid var(--el-border-color);
      border-top: none;
      border-radius: 0 0 4px 4px;
      
      .icon-hash-value {
        font-family: Consolas, Monaco, monospace;
        font-size: 13px;
        color: var(--el-color-primary);
        cursor: pointer;
        
        &:hover {
          text-decoration: underline;
        }
      }
    }

    .no-data {
      color: var(--el-text-color-secondary);
    }

    .update-time {
      font-size: 12px;
      color: var(--el-text-color-regular);
    }

    .pagination {
      margin-top: 15px;
      justify-content: flex-end;
    }
  }

  .history-current {
    margin-bottom: 15px;
    padding: 10px;
    background: var(--el-fill-color-light);
    border-radius: 4px;
  }

  .history-empty {
    text-align: center;
    padding: 40px;
    color: var(--el-text-color-secondary);
  }
  
  .table-actions {
    display: flex;
    gap: 10px;
  }
  
  .scan-target-info {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px;
    background: var(--el-fill-color-light);
    border-radius: 6px;
    margin-bottom: 20px;
    border-left: 3px solid var(--el-color-primary);
    color: var(--el-color-primary);
    font-weight: 500;
  }
  
html.dark .scan-target-info {
  background: rgba(64, 158, 255, 0.15);
}
  
  .scan-form {
    .form-hint {
      margin-left: 10px;
      color: var(--el-text-color-secondary);
      font-size: 12px;
    }
    
    :deep(.el-divider__text) {
      font-size: 13px;
      color: var(--el-text-color-secondary);
    }
  }
}
</style>
