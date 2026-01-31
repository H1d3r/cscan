<template>
  <div class="dashboard">
    <!-- 顶部欢迎区域 -->
    <div class="welcome-banner">
      <div class="welcome-content">
        <div class="welcome-text">
          <h1>{{ getGreeting() }}</h1>
          <p class="welcome-subtitle">{{ $t('dashboard.welcomeSubtitle') }}</p>
        </div>
        <div class="welcome-time">
          <div class="time-display">{{ currentTime }}</div>
          <div class="date-display">{{ currentDate }}</div>
        </div>
      </div>
    </div>

    <!-- 核心指标卡片 -->
    <div class="metrics-grid">
      <div class="metric-card assets" @click="$router.push('/asset-management')">
        <div class="metric-icon">
          <el-icon><Monitor /></el-icon>
        </div>
        <div class="metric-content">
          <div class="metric-value">{{ animatedAssets }}</div>
          <div class="metric-label">{{ $t('dashboard.totalAssets') }}</div>
          <div class="metric-trend" :class="assetTrend > 0 ? 'up' : 'down'" v-if="assetTrend !== 0">
            <el-icon><TrendCharts /></el-icon>
            <span>{{ assetTrend > 0 ? '+' : '' }}{{ assetTrend }}</span>
            <span class="trend-period">{{ $t('dashboard.thisWeek') }}</span>
          </div>
        </div>
        <div class="metric-sparkline">
          <div ref="assetSparklineRef" class="sparkline-chart"></div>
        </div>
      </div>

      <div class="metric-card vulnerabilities" @click="$router.push('/asset/vulnerability')">
        <div class="metric-icon critical">
          <el-icon><Warning /></el-icon>
        </div>
        <div class="metric-content">
          <div class="metric-value">{{ animatedVulns }}</div>
          <div class="metric-label">{{ $t('dashboard.totalVulnerabilities') }}</div>
          <div class="severity-badges">
            <span class="badge critical" v-if="vulStats.critical > 0">{{ vulStats.critical }} Critical</span>
            <span class="badge high" v-if="vulStats.high > 0">{{ vulStats.high }} High</span>
          </div>
        </div>
        <div class="metric-ring">
          <div ref="vulnRingRef" class="ring-chart"></div>
        </div>
      </div>

      <div class="metric-card tasks" @click="$router.push('/task')">
        <div class="metric-icon">
          <el-icon><List /></el-icon>
        </div>
        <div class="metric-content">
          <div class="metric-value">{{ taskStats.running }}</div>
          <div class="metric-label">{{ $t('dashboard.activeTasks') }}</div>
          <div class="task-progress">
            <div class="progress-bar">
              <div class="progress-fill" :style="{ width: taskCompletionRate + '%' }"></div>
            </div>
            <span class="progress-text">{{ taskCompletionRate }}% {{ $t('dashboard.completionRate') }}</span>
          </div>
        </div>
      </div>

      <div class="metric-card workers" @click="$router.push('/worker')">
        <div class="metric-icon" :class="workerStats.online > 0 ? 'online' : 'offline'">
          <el-icon><Connection /></el-icon>
        </div>
        <div class="metric-content">
          <div class="metric-value">{{ workerStats.online }}<span class="metric-total">/{{ workerStats.online + workerStats.offline }}</span></div>
          <div class="metric-label">{{ $t('dashboard.workersOnline') }}</div>
          <div class="worker-status">
            <div class="status-dot online"></div>
            <span>{{ $t('dashboard.systemHealthy') }}</span>
          </div>
        </div>
        <div class="worker-load">
          <div class="load-item">
            <span class="load-label">CPU</span>
            <el-progress :percentage="avgCpuLoad" :stroke-width="4" :show-text="false" />
          </div>
          <div class="load-item">
            <span class="load-label">MEM</span>
            <el-progress :percentage="avgMemUsed" :stroke-width="4" :show-text="false" />
          </div>
        </div>
      </div>
    </div>

    <!-- 主内容区域 -->
    <div class="main-content">
      <!-- 左侧：图表和统计 -->
      <div class="content-left">
        <!-- 安全态势图 -->
        <div class="chart-card security-posture">
          <div class="card-header">
            <h3>{{ $t('dashboard.securityPosture') }}</h3>
            <div class="header-actions">
              <el-radio-group v-model="postureTimeRange" size="small">
                <el-radio-button label="7d">7D</el-radio-button>
                <el-radio-button label="30d">30D</el-radio-button>
                <el-radio-button label="90d">90D</el-radio-button>
              </el-radio-group>
            </div>
          </div>
          <div ref="securityPostureChartRef" class="chart-container"></div>
        </div>

        <!-- 资产分布 -->
        <div class="chart-card asset-distribution">
          <div class="card-header">
            <h3>{{ $t('dashboard.assetDistribution') }}</h3>
            <el-radio-group v-model="distributionType" size="small">
              <el-radio-button label="port">{{ $t('dashboard.byPort') }}</el-radio-button>
              <el-radio-button label="service">{{ $t('dashboard.byService') }}</el-radio-button>
              <el-radio-button label="tech">{{ $t('dashboard.byTech') }}</el-radio-button>
            </el-radio-group>
          </div>
          <div ref="assetDistributionChartRef" class="chart-container"></div>
        </div>

        <!-- 任务执行趋势 -->
        <div class="chart-card task-trend">
          <div class="card-header">
            <h3>{{ $t('dashboard.taskTrend') }}</h3>
          </div>
          <div ref="taskTrendChartRef" class="chart-container"></div>
        </div>
      </div>

      <!-- 右侧：实时信息 -->
      <div class="content-right">
        <!-- 安全评分 -->
        <div class="info-card security-score">
          <div class="score-display">
            <div class="score-ring" ref="scoreRingRef"></div>
            <div class="score-info">
              <div class="score-value">{{ securityScore }}</div>
              <div class="score-label">{{ getScoreLevel(securityScore) }}</div>
            </div>
          </div>
          <div class="score-breakdown">
            <div class="breakdown-item">
              <span class="breakdown-label">{{ $t('dashboard.criticalRisks') }}</span>
              <span class="breakdown-value critical">{{ vulStats.critical }}</span>
            </div>
            <div class="breakdown-item">
              <span class="breakdown-label">{{ $t('dashboard.highRisks') }}</span>
              <span class="breakdown-value high">{{ vulStats.high }}</span>
            </div>
            <div class="breakdown-item">
              <span class="breakdown-label">{{ $t('dashboard.exposedPorts') }}</span>
              <span class="breakdown-value">{{ exposedPorts }}</span>
            </div>
          </div>
        </div>

        <!-- 最新威胁 -->
        <div class="info-card recent-threats">
          <div class="card-header">
            <h3>{{ $t('dashboard.recentThreats') }}</h3>
            <el-badge :value="recentVuls.length" :max="99" class="threat-badge" />
          </div>
          <div class="threat-list">
            <div v-for="vul in recentVuls" :key="vul.id" class="threat-item" @click="viewVulDetail(vul)">
              <div class="threat-severity" :class="vul.severity">
                {{ vul.severity.charAt(0).toUpperCase() }}
              </div>
              <div class="threat-info">
                <div class="threat-name">{{ vul.name }}</div>
                <div class="threat-meta">
                  <span class="threat-target">{{ vul.host }}</span>
                  <span class="threat-time">{{ vul.time }}</span>
                </div>
              </div>
            </div>
            <div v-if="recentVuls.length === 0" class="empty-threats">
              <el-icon><CircleCheck /></el-icon>
              <span>{{ $t('dashboard.noRecentThreats') }}</span>
            </div>
          </div>
        </div>

        <!-- 活动日志 -->
        <div class="info-card activity-log">
          <div class="card-header">
            <h3>{{ $t('dashboard.recentActivity') }}</h3>
          </div>
          <div class="activity-list">
            <div v-for="(activity, index) in recentActivities" :key="index" class="activity-item">
              <div class="activity-icon" :class="activity.type">
                <el-icon><component :is="getActivityIcon(activity.type)" /></el-icon>
              </div>
              <div class="activity-content">
                <div class="activity-text">{{ activity.text }}</div>
                <div class="activity-time">{{ activity.time }}</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts'
import { getAssetStat } from '@/api/asset'
import request from '@/api/request'
import {
  Monitor, Warning, List, Connection, Plus, Search, Aim, Document,
  TrendCharts, CircleCheck, Checked, Clock, Upload
} from '@element-plus/icons-vue'

const { t } = useI18n()
const router = useRouter()

// 时间显示
const currentTime = ref('')
const currentDate = ref('')

// 统计数据
const assetStats = reactive({
  totalAsset: 0,
  totalHost: 0,
  newCount: 0,
  topPorts: [],
  topService: [],
  topApp: []
})

const vulStats = reactive({
  total: 0,
  critical: 0,
  high: 0,
  medium: 0,
  low: 0,
  info: 0,
  week: 0,
  month: 0
})

const taskStats = reactive({
  total: 0,
  completed: 0,
  running: 0,
  failed: 0,
  pending: 0,
  trendDays: [],
  trendCompleted: [],
  trendFailed: []
})

const workerStats = reactive({
  online: 0,
  offline: 0,
  workers: []
})

// 动画数值
const animatedAssets = ref(0)
const animatedVulns = ref(0)
const assetTrend = ref(0)
const exposedPorts = ref(0)

// 图表时间范围
const postureTimeRange = ref('7d')
const distributionType = ref('port')

// 最新漏洞和活动
const recentVuls = ref([])
const recentActivities = ref([])

// 图表引用
const assetSparklineRef = ref()
const vulnRingRef = ref()
const securityPostureChartRef = ref()
const assetDistributionChartRef = ref()
const taskTrendChartRef = ref()
const scoreRingRef = ref()

let charts = {}
let timeInterval = null
let refreshInterval = null

// 计算属性
const securityScore = computed(() => {
  if (vulStats.total === 0 && assetStats.totalAsset === 0) return 100
  const criticalPenalty = vulStats.critical * 15
  const highPenalty = vulStats.high * 8
  const mediumPenalty = vulStats.medium * 3
  const score = Math.max(0, 100 - criticalPenalty - highPenalty - mediumPenalty)
  return Math.round(score)
})

const taskCompletionRate = computed(() => {
  if (taskStats.total === 0) return 0
  return Math.round((taskStats.completed / taskStats.total) * 100)
})

const avgCpuLoad = computed(() => {
  if (workerStats.workers.length === 0) return 0
  const total = workerStats.workers.reduce((sum, w) => sum + (w.cpuLoad || 0), 0)
  return Math.round(total / workerStats.workers.length)
})

const avgMemUsed = computed(() => {
  if (workerStats.workers.length === 0) return 0
  const total = workerStats.workers.reduce((sum, w) => sum + (w.memUsed || 0), 0)
  return Math.round(total / workerStats.workers.length)
})

// 方法
function getGreeting() {
  const hour = new Date().getHours()
  if (hour < 12) return t('dashboard.goodMorning')
  if (hour < 18) return t('dashboard.goodAfternoon')
  return t('dashboard.goodEvening')
}

function updateTime() {
  const now = new Date()
  currentTime.value = now.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  currentDate.value = now.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })
}

function getScoreLevel(score) {
  if (score >= 90) return t('dashboard.excellent')
  if (score >= 70) return t('dashboard.good')
  if (score >= 50) return t('dashboard.warning')
  return t('dashboard.critical')
}

function getActivityIcon(type) {
  const icons = { scan: Clock, task: List, vuln: Warning, asset: Monitor }
  return icons[type] || Clock
}

function formatTime(timeStr) {
  if (!timeStr) return ''
  const date = new Date(timeStr)
  const now = new Date()
  const diff = now - date
  if (diff < 60000) return t('dashboard.justNow')
  if (diff < 3600000) return Math.floor(diff / 60000) + t('dashboard.minutesAgo')
  if (diff < 86400000) return Math.floor(diff / 3600000) + t('dashboard.hoursAgo')
  return Math.floor(diff / 86400000) + t('dashboard.daysAgo')
}

function animateValue(ref, target, duration = 1000) {
  const start = ref.value
  const change = target - start
  const startTime = performance.now()
  
  function update(currentTime) {
    const elapsed = currentTime - startTime
    const progress = Math.min(elapsed / duration, 1)
    const easeProgress = 1 - Math.pow(1 - progress, 3)
    ref.value = Math.round(start + change * easeProgress)
    if (progress < 1) requestAnimationFrame(update)
  }
  requestAnimationFrame(update)
}

async function loadAllData() {
  await Promise.all([
    loadAssetStats(),
    loadVulStats(),
    loadTaskStats(),
    loadWorkerStats()
  ])
  await nextTick()
  initAllCharts()
}

async function loadAssetStats() {
  try {
    const res = await getAssetStat()
    if (res.code === 0) {
      assetStats.totalAsset = res.totalAsset || 0
      assetStats.totalHost = res.totalHost || 0
      assetStats.newCount = res.newCount || 0
      assetStats.topPorts = res.topPorts || []
      assetStats.topService = res.topService || []
      assetStats.topApp = res.topApp || []
      assetTrend.value = res.newCount || 0
      exposedPorts.value = assetStats.topPorts.length
      animateValue(animatedAssets, assetStats.totalAsset)
    }
  } catch (e) {
    console.error('Failed to load asset stats:', e)
  }
}

async function loadVulStats() {
  try {
    const statRes = await request.post('/vul/stat')
    if (statRes.code === 0) {
      Object.assign(vulStats, {
        total: statRes.total || 0,
        critical: statRes.critical || 0,
        high: statRes.high || 0,
        medium: statRes.medium || 0,
        low: statRes.low || 0,
        info: statRes.info || 0,
        week: statRes.week || 0,
        month: statRes.month || 0
      })
      animateValue(animatedVulns, vulStats.total)
    }

    const listRes = await request.post('/vul/list', { page: 1, pageSize: 5 })
    if (listRes.code === 0) {
      recentVuls.value = (listRes.list || []).map(v => ({
        id: v.id,
        name: v.vulName || v.pocFile || 'Unknown',
        severity: v.severity || 'info',
        host: v.host || '',
        time: formatTime(v.createTime)
      }))
    }
  } catch (e) {
    console.error('Failed to load vul stats:', e)
  }
}

async function loadTaskStats() {
  try {
    const res = await request.post('/task/stat')
    if (res.code === 0) {
      Object.assign(taskStats, {
        total: res.total || 0,
        completed: res.completed || 0,
        running: res.running || 0,
        failed: res.failed || 0,
        pending: res.pending || 0,
        trendDays: res.trendDays || [],
        trendCompleted: res.trendCompleted || [],
        trendFailed: res.trendFailed || []
      })
      
      // 生成活动日志
      recentActivities.value = [
        { type: 'task', text: t('dashboard.activityTaskCompleted', { count: taskStats.completed }), time: t('dashboard.today') },
        { type: 'vuln', text: t('dashboard.activityVulnsFound', { count: vulStats.week }), time: t('dashboard.thisWeek') },
        { type: 'asset', text: t('dashboard.activityAssetsDiscovered', { count: assetStats.newCount }), time: t('dashboard.thisWeek') }
      ]
    }
  } catch (e) {
    console.error('Failed to load task stats:', e)
  }
}

async function loadWorkerStats() {
  try {
    const res = await request.post('/worker/list')
    if (res.code === 0) {
      const list = res.list || []
      workerStats.workers = list
      workerStats.online = list.filter(w => w.status === 'running').length
      workerStats.offline = list.filter(w => w.status !== 'running').length
    }
  } catch (e) {
    console.error('Failed to load worker stats:', e)
  }
}

function initAllCharts() {
  initSecurityPostureChart()
  initAssetDistributionChart()
  initTaskTrendChart()
  initScoreRing()
  initVulnRing()
}

function initSecurityPostureChart() {
  if (!securityPostureChartRef.value) return
  
  charts.securityPosture?.dispose()
  charts.securityPosture = echarts.init(securityPostureChartRef.value)
  
  const days = taskStats.trendDays.length > 0 ? taskStats.trendDays : generateDays(7)
  const vulnData = taskStats.trendFailed.length > 0 ? taskStats.trendFailed : [0, 0, 0, 0, 0, 0, 0]
  const assetData = taskStats.trendCompleted.length > 0 ? taskStats.trendCompleted : [0, 0, 0, 0, 0, 0, 0]
  
  charts.securityPosture.setOption({
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis', backgroundColor: 'hsl(var(--card))', borderColor: 'hsl(var(--border))', textStyle: { color: 'hsl(var(--foreground))' } },
    legend: { data: [t('dashboard.vulnerabilities'), t('dashboard.assetsScanned')], textStyle: { color: 'hsl(var(--muted-foreground))' }, bottom: 0 },
    grid: { left: '3%', right: '4%', bottom: '15%', top: '10%', containLabel: true },
    xAxis: { type: 'category', data: days, axisLine: { lineStyle: { color: 'hsl(var(--border))' } }, axisLabel: { color: 'hsl(var(--muted-foreground))' } },
    yAxis: { type: 'value', axisLine: { lineStyle: { color: 'hsl(var(--border))' } }, axisLabel: { color: 'hsl(var(--muted-foreground))' }, splitLine: { lineStyle: { color: 'hsl(var(--border))' } } },
    series: [
      { name: t('dashboard.vulnerabilities'), type: 'line', smooth: true, data: vulnData, itemStyle: { color: '#f56c6c' }, areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(245, 108, 108, 0.3)' }, { offset: 1, color: 'rgba(245, 108, 108, 0)' }] } } },
      { name: t('dashboard.assetsScanned'), type: 'bar', data: assetData, itemStyle: { color: 'hsl(var(--primary))', borderRadius: [4, 4, 0, 0] } }
    ]
  })
}

function initAssetDistributionChart() {
  if (!assetDistributionChartRef.value) return
  
  charts.assetDistribution?.dispose()
  charts.assetDistribution = echarts.init(assetDistributionChartRef.value)
  
  let data = []
  if (distributionType.value === 'port') {
    data = assetStats.topPorts.slice(0, 10).map(item => ({ name: String(item.name), value: item.count }))
  } else if (distributionType.value === 'service') {
    data = assetStats.topService.slice(0, 10).map(item => ({ name: item.name, value: item.count }))
  } else {
    data = assetStats.topApp.slice(0, 10).map(item => ({ name: item.name, value: item.count }))
  }
  
  if (data.length === 0) {
    data = [{ name: t('dashboard.noData'), value: 1 }]
  }
  
  charts.assetDistribution.setOption({
    backgroundColor: 'transparent',
    tooltip: { trigger: 'item', backgroundColor: 'hsl(var(--card))', borderColor: 'hsl(var(--border))', textStyle: { color: 'hsl(var(--foreground))' } },
    series: [{
      type: 'pie',
      radius: ['40%', '70%'],
      center: ['50%', '50%'],
      data: data,
      label: { color: 'hsl(var(--foreground))', fontSize: 12 },
      labelLine: { lineStyle: { color: 'hsl(var(--border))' } },
      emphasis: { itemStyle: { shadowBlur: 10, shadowOffsetX: 0, shadowColor: 'rgba(0, 0, 0, 0.5)' } }
    }]
  })
}

function initTaskTrendChart() {
  if (!taskTrendChartRef.value) return
  
  charts.taskTrend?.dispose()
  charts.taskTrend = echarts.init(taskTrendChartRef.value)
  
  const days = taskStats.trendDays.length > 0 ? taskStats.trendDays : generateDays(7)
  const completedData = taskStats.trendCompleted.length > 0 ? taskStats.trendCompleted : [0, 0, 0, 0, 0, 0, 0]
  const failedData = taskStats.trendFailed.length > 0 ? taskStats.trendFailed : [0, 0, 0, 0, 0, 0, 0]
  
  charts.taskTrend.setOption({
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis', backgroundColor: 'hsl(var(--card))', borderColor: 'hsl(var(--border))', textStyle: { color: 'hsl(var(--foreground))' } },
    legend: { data: [t('dashboard.completed'), t('dashboard.failed')], textStyle: { color: 'hsl(var(--muted-foreground))' }, bottom: 0 },
    grid: { left: '3%', right: '4%', bottom: '15%', top: '10%', containLabel: true },
    xAxis: { type: 'category', data: days, axisLine: { lineStyle: { color: 'hsl(var(--border))' } }, axisLabel: { color: 'hsl(var(--muted-foreground))' } },
    yAxis: { type: 'value', axisLine: { lineStyle: { color: 'hsl(var(--border))' } }, axisLabel: { color: 'hsl(var(--muted-foreground))' }, splitLine: { lineStyle: { color: 'hsl(var(--border))' } } },
    series: [
      { name: t('dashboard.completed'), type: 'bar', stack: 'total', data: completedData, itemStyle: { color: '#67c23a', borderRadius: [4, 4, 0, 0] } },
      { name: t('dashboard.failed'), type: 'bar', stack: 'total', data: failedData, itemStyle: { color: '#f56c6c', borderRadius: [4, 4, 0, 0] } }
    ]
  })
}

function initScoreRing() {
  if (!scoreRingRef.value) return
  
  charts.scoreRing?.dispose()
  charts.scoreRing = echarts.init(scoreRingRef.value)
  
  const score = securityScore.value
  let color = '#67c23a'
  if (score < 50) color = '#f56c6c'
  else if (score < 70) color = '#e6a23c'
  else if (score < 90) color = '#409eff'
  
  charts.scoreRing.setOption({
    backgroundColor: 'transparent',
    series: [{
      type: 'gauge',
      startAngle: 90,
      endAngle: -270,
      pointer: { show: false },
      progress: { show: true, overlap: false, roundCap: true, clip: false, itemStyle: { color: color } },
      axisLine: { lineStyle: { width: 12, color: [[1, 'hsl(var(--muted))']] } },
      splitLine: { show: false },
      axisTick: { show: false },
      axisLabel: { show: false },
      data: [{ value: score }],
      detail: { show: false }
    }]
  })
}

function initVulnRing() {
  if (!vulnRingRef.value) return
  
  charts.vulnRing?.dispose()
  charts.vulnRing = echarts.init(vulnRingRef.value)
  
  const data = [
    { value: vulStats.critical, name: 'Critical', itemStyle: { color: '#f56c6c' } },
    { value: vulStats.high, name: 'High', itemStyle: { color: '#e6a23c' } },
    { value: vulStats.medium, name: 'Medium', itemStyle: { color: '#409eff' } },
    { value: vulStats.low, name: 'Low', itemStyle: { color: '#67c23a' } },
    { value: vulStats.info, name: 'Info', itemStyle: { color: '#909399' } }
  ].filter(d => d.value > 0)
  
  if (data.length === 0) {
    data.push({ value: 1, name: 'None', itemStyle: { color: 'hsl(var(--muted))' } })
  }
  
  charts.vulnRing.setOption({
    backgroundColor: 'transparent',
    series: [{
      type: 'pie',
      radius: ['60%', '85%'],
      center: ['50%', '50%'],
      data: data,
      label: { show: false },
      emphasis: { scale: false }
    }]
  })
}

function generateDays(count) {
  const days = []
  for (let i = count - 1; i >= 0; i--) {
    const date = new Date()
    date.setDate(date.getDate() - i)
    days.push(date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }))
  }
  return days
}

function viewVulDetail(vul) {
  router.push(`/asset/vulnerability?vulId=${vul.id}`)
}

function handleResize() {
  Object.values(charts).forEach(chart => chart?.resize())
}

function handleWorkspaceChanged() {
  loadAllData()
}

watch(distributionType, () => {
  initAssetDistributionChart()
})

watch(postureTimeRange, () => {
  initSecurityPostureChart()
})

onMounted(async () => {
  updateTime()
  timeInterval = setInterval(updateTime, 1000)
  await loadAllData()
  refreshInterval = setInterval(loadAllData, 60000) // 每分钟刷新
  window.addEventListener('resize', handleResize)
  window.addEventListener('workspace-changed', handleWorkspaceChanged)
})

onUnmounted(() => {
  clearInterval(timeInterval)
  clearInterval(refreshInterval)
  Object.values(charts).forEach(chart => chart?.dispose())
  window.removeEventListener('resize', handleResize)
  window.removeEventListener('workspace-changed', handleWorkspaceChanged)
})
</script>

<style scoped lang="scss">
.dashboard {
  padding: 24px;
  min-height: 100%;
  background: hsl(var(--background));
}

// 欢迎横幅
.welcome-banner {
  background: linear-gradient(135deg, hsl(var(--primary)) 0%, hsl(var(--primary) / 0.8) 100%);
  border-radius: 16px;
  padding: 24px 32px;
  margin-bottom: 24px;
  color: hsl(var(--primary-foreground));
  
  .welcome-content {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  
  .welcome-text {
    h1 {
      font-size: 28px;
      font-weight: 700;
      margin: 0 0 8px 0;
    }
    
    .welcome-subtitle {
      font-size: 14px;
      opacity: 0.9;
      margin: 0;
    }
  }
  
  .welcome-time {
    text-align: right;
    
    .time-display {
      font-size: 32px;
      font-weight: 600;
      font-variant-numeric: tabular-nums;
    }
    
    .date-display {
      font-size: 14px;
      opacity: 0.9;
    }
  }
}

// 核心指标网格
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 20px;
  margin-bottom: 24px;
  
  @media (max-width: 1400px) {
    grid-template-columns: repeat(2, 1fr);
  }
  
  @media (max-width: 768px) {
    grid-template-columns: 1fr;
  }
}

.metric-card {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 16px;
  padding: 20px;
  display: flex;
  align-items: flex-start;
  gap: 16px;
  cursor: pointer;
  transition: all 0.3s ease;
  position: relative;
  overflow: hidden;
  
  &:hover {
    transform: translateY(-4px);
    box-shadow: 0 12px 24px rgba(0, 0, 0, 0.1);
    border-color: hsl(var(--primary) / 0.3);
  }
  
  .metric-icon {
    width: 48px;
    height: 48px;
    border-radius: 12px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: hsl(var(--primary) / 0.1);
    color: hsl(var(--primary));
    font-size: 24px;
    flex-shrink: 0;
    
    &.critical { background: rgba(245, 108, 108, 0.1); color: #f56c6c; }
    &.online { background: rgba(103, 194, 58, 0.1); color: #67c23a; }
    &.offline { background: rgba(144, 147, 153, 0.1); color: #909399; }
  }
  
  .metric-content {
    flex: 1;
    min-width: 0;
  }
  
  .metric-value {
    font-size: 32px;
    font-weight: 700;
    color: hsl(var(--foreground));
    line-height: 1.2;
    
    .metric-total {
      font-size: 18px;
      font-weight: 400;
      color: hsl(var(--muted-foreground));
    }
  }
  
  .metric-label {
    font-size: 14px;
    color: hsl(var(--muted-foreground));
    margin-top: 4px;
  }
  
  .metric-trend {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 8px;
    font-size: 12px;
    
    &.up { color: #67c23a; }
    &.down { color: #f56c6c; }
    
    .trend-period {
      color: hsl(var(--muted-foreground));
      margin-left: 4px;
    }
  }
  
  .severity-badges {
    display: flex;
    gap: 8px;
    margin-top: 8px;
    
    .badge {
      padding: 2px 8px;
      border-radius: 4px;
      font-size: 11px;
      font-weight: 500;
      
      &.critical { background: rgba(245, 108, 108, 0.2); color: #f56c6c; }
      &.high { background: rgba(230, 162, 60, 0.2); color: #e6a23c; }
    }
  }
  
  .task-progress {
    margin-top: 8px;
    
    .progress-bar {
      height: 6px;
      background: hsl(var(--muted));
      border-radius: 3px;
      overflow: hidden;
      
      .progress-fill {
        height: 100%;
        background: hsl(var(--primary));
        border-radius: 3px;
        transition: width 0.3s ease;
      }
    }
    
    .progress-text {
      font-size: 12px;
      color: hsl(var(--muted-foreground));
      margin-top: 4px;
      display: block;
    }
  }
  
  .worker-status {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
    font-size: 12px;
    color: #67c23a;
    
    .status-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      
      &.online { background: #67c23a; box-shadow: 0 0 8px rgba(103, 194, 58, 0.5); }
    }
  }
  
  .worker-load {
    position: absolute;
    right: 20px;
    top: 20px;
    width: 80px;
    
    .load-item {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 8px;
      
      .load-label {
        font-size: 10px;
        color: hsl(var(--muted-foreground));
        width: 24px;
      }
      
      :deep(.el-progress) {
        flex: 1;
      }
    }
  }
  
  .metric-sparkline, .metric-ring {
    position: absolute;
    right: 16px;
    bottom: 16px;
    width: 80px;
    height: 40px;
    
    .sparkline-chart, .ring-chart {
      width: 100%;
      height: 100%;
    }
  }
  
  .metric-ring {
    width: 60px;
    height: 60px;
    top: 50%;
    transform: translateY(-50%);
  }
}

// 主内容区域
.main-content {
  display: grid;
  grid-template-columns: 1fr 380px;
  gap: 24px;
  
  @media (max-width: 1200px) {
    grid-template-columns: 1fr;
  }
}

.content-left {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.content-right {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

// 图表卡片
.chart-card {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 16px;
  padding: 20px;
  
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    
    h3 {
      font-size: 16px;
      font-weight: 600;
      color: hsl(var(--foreground));
      margin: 0;
    }
  }
  
  .chart-container {
    height: 280px;
  }
}

// 信息卡片
.info-card {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 16px;
  padding: 20px;
  
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
    
    h3 {
      font-size: 16px;
      font-weight: 600;
      color: hsl(var(--foreground));
      margin: 0;
    }
  }
}

// 安全评分
.security-score {
  .score-display {
    display: flex;
    align-items: center;
    gap: 20px;
    margin-bottom: 20px;
    
    .score-ring {
      width: 100px;
      height: 100px;
    }
    
    .score-info {
      .score-value {
        font-size: 48px;
        font-weight: 700;
        color: hsl(var(--foreground));
        line-height: 1;
      }
      
      .score-label {
        font-size: 14px;
        color: hsl(var(--muted-foreground));
        margin-top: 4px;
      }
    }
  }
  
  .score-breakdown {
    display: flex;
    flex-direction: column;
    gap: 12px;
    
    .breakdown-item {
      display: flex;
      justify-content: space-between;
      align-items: center;
      
      .breakdown-label {
        font-size: 13px;
        color: hsl(var(--muted-foreground));
      }
      
      .breakdown-value {
        font-size: 14px;
        font-weight: 600;
        color: hsl(var(--foreground));
        
        &.critical { color: #f56c6c; }
        &.high { color: #e6a23c; }
      }
    }
  }
}

// 最新威胁
.recent-threats {
  .threat-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  
  .threat-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    background: hsl(var(--muted) / 0.3);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s;
    
    &:hover {
      background: hsl(var(--muted) / 0.5);
    }
    
    .threat-severity {
      width: 32px;
      height: 32px;
      border-radius: 8px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 14px;
      font-weight: 700;
      color: white;
      
      &.critical { background: #f56c6c; }
      &.high { background: #e6a23c; }
      &.medium { background: #409eff; }
      &.low { background: #67c23a; }
      &.info { background: #909399; }
    }
    
    .threat-info {
      flex: 1;
      min-width: 0;
      
      .threat-name {
        font-size: 13px;
        font-weight: 500;
        color: hsl(var(--foreground));
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
      }
      
      .threat-meta {
        display: flex;
        gap: 8px;
        font-size: 12px;
        color: hsl(var(--muted-foreground));
        margin-top: 4px;
        
        .threat-target {
          max-width: 120px;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }
      }
    }
  }
  
  .empty-threats {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 40px 20px;
    color: hsl(var(--muted-foreground));
    
    .el-icon {
      font-size: 48px;
      color: #67c23a;
      margin-bottom: 12px;
    }
  }
}

// 快速操作
.quick-actions {
  .action-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
  }
  
  .action-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 16px;
    background: hsl(var(--muted) / 0.3);
    border-radius: 12px;
    cursor: pointer;
    transition: all 0.2s;
    
    &:hover {
      background: hsl(var(--primary) / 0.1);
      color: hsl(var(--primary));
      
      .el-icon {
        color: hsl(var(--primary));
      }
    }
    
    .el-icon {
      font-size: 24px;
      color: hsl(var(--muted-foreground));
      transition: color 0.2s;
    }
    
    span {
      font-size: 12px;
      color: hsl(var(--foreground));
      text-align: center;
    }
  }
}

// 活动日志
.activity-log {
  .activity-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  
  .activity-item {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    
    .activity-icon {
      width: 32px;
      height: 32px;
      border-radius: 8px;
      display: flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
      
      &.task { background: rgba(64, 158, 255, 0.1); color: #409eff; }
      &.vuln { background: rgba(245, 108, 108, 0.1); color: #f56c6c; }
      &.asset { background: rgba(103, 194, 58, 0.1); color: #67c23a; }
      &.scan { background: rgba(230, 162, 60, 0.1); color: #e6a23c; }
    }
    
    .activity-content {
      flex: 1;
      
      .activity-text {
        font-size: 13px;
        color: hsl(var(--foreground));
      }
      
      .activity-time {
        font-size: 12px;
        color: hsl(var(--muted-foreground));
        margin-top: 2px;
      }
    }
  }
}
</style>
