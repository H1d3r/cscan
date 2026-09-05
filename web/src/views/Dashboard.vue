<template>
  <div class="dashboard-pd" :class="[{ 'is-dark': themeStore.isDark }, `style-${themeStore.themeStyle}`]">
    <div class="version-bar">
      <span>{{ t('dashboard.currentVersion') }} <strong>{{ currentVersion }}</strong></span>
      <span class="version-divider"></span>
      <span>
        {{ t('dashboard.latestVersion') }}
        <a :href="VERSION_SOURCE_URL" target="_blank" rel="noopener noreferrer">
          {{ versionLoading ? t('dashboard.versionLoading') : (latestVersion || t('dashboard.versionUnavailable')) }}
        </a>
      </span>
      <el-tag v-if="!versionLoading && latestVersion" :type="updateAvailable ? 'warning' : 'success'" size="small">
        {{ updateAvailable ? t('dashboard.versionUpdateAvailable') : t('dashboard.versionUpToDate') }}
      </el-tag>
    </div>

    <!-- ===== 上区：2列布局（左栏堆叠 / 右栏资产概览跨行）===== -->
    <div class="layout-top">
      <!-- 左列：开放漏洞 + 暴露面总览 -->
      <div class="top-left">
        <!-- 漏洞概览面板 -->
        <div class="panel vuln-panel">
          <div class="panel-header">
            <h2 class="panel-title">{{ t('dashboard.openVulnerabilities') }}</h2>
          </div>
          <div class="vuln-stats-row">
            <div class="vuln-stat total">
              <span class="vuln-num">{{ animatedData.vulns }}</span>
              <span class="vuln-label">{{ t('dashboard.total') }}</span>
            </div>
            <div class="vuln-stat critical">
              <span class="vuln-num">{{ stats.vulnOpenCritical }}</span>
              <span class="vuln-label">{{ t('dashboard.critical') }}</span>
            </div>
            <div class="vuln-stat high">
              <span class="vuln-num">{{ stats.vulnOpenHigh }}</span>
              <span class="vuln-label">{{ t('dashboard.high') }}</span>
            </div>
            <div class="vuln-stat medium">
              <span class="vuln-num">{{ stats.vulnOpenMedium }}</span>
              <span class="vuln-label">{{ t('dashboard.medium') }}</span>
            </div>
            <div class="vuln-stat low">
              <span class="vuln-num">{{ stats.vulnOpenLow }}</span>
              <span class="vuln-label">{{ t('dashboard.low') }}</span>
            </div>
            <div class="vuln-stat info">
              <span class="vuln-num">{{ stats.vulnOpenInfo }}</span>
              <span class="vuln-label">{{ t('dashboard.info') }}</span>
            </div>
          </div>
          <!-- 漏洞等级分布条 -->
          <div class="vuln-bar-track">
            <div class="vuln-bar-seg critical" :style="{ width: vulnBarWidth('critical') }"></div>
            <div class="vuln-bar-seg high" :style="{ width: vulnBarWidth('high') }"></div>
            <div class="vuln-bar-seg medium" :style="{ width: vulnBarWidth('medium') }"></div>
            <div class="vuln-bar-seg low" :style="{ width: vulnBarWidth('low') }"></div>
            <div class="vuln-bar-seg info" :style="{ width: vulnBarWidth('info') }"></div>
          </div>
        </div>

        <!-- 暴露面总览 -->
        <div class="panel exposure-panel">
          <div class="panel-header">
            <h2 class="panel-title">{{ t('dashboard.exposureTitle') }}</h2>
            <span class="exposure-badge">{{ t('dashboard.exposureLive') }}</span>
          </div>
          <div class="exposure-body" ref="exposureBodyRef">
            <!-- 左侧：暴露源分类 -->
            <div class="exposure-sources">
              <div class="source-item" v-for="(src, i) in exposureSources" :key="src.key" :ref="el => { if (el) sourceItemRefs[i] = el }" @click="src.route && $router.push(src.route)">
                <span class="source-dot" :style="{ background: src.color }"></span>
                <span class="source-name">{{ src.label }}</span>
                <span class="source-count">{{ src.value }}</span>
              </div>
            </div>
            <!-- 左侧连接线占位 -->
            <div class="exposure-lines-left"></div>
            <!-- 中心：发光圆 -->
            <div class="exposure-core" ref="coreRef">
              <div class="core-glow"></div>
              <div class="core-circle">
                <span class="core-num">{{ animatedData.exposureTotal }}</span>
                <span class="core-label">{{ t('dashboard.exposureSurface') }}</span>
              </div>
            </div>
            <!-- 右侧连接线占位 -->
            <div class="exposure-lines-right"></div>
            <!-- 右侧：环形统计 -->
            <div class="exposure-rings">
              <div class="ring-item" v-for="(ring, i) in exposureRings" :key="ring.key" @click="ring.route && $router.push(ring.route)">
                <div class="ring-circle" :ref="el => { if (el) ringCircleRefs[i] = el }" :style="{ '--ring-color': ring.color, '--ring-pct': ring.pct + '%' }">
                  <span class="ring-num">{{ ring.value }}</span>
                </div>
                <span class="ring-label">{{ ring.label }}</span>
              </div>
            </div>

            <!-- 全区域 SVG 连线覆盖层 -->
            <svg v-if="anchorsReady" class="exposure-lines-svg" :viewBox="`0 0 ${svgSize.w} ${svgSize.h}`" preserveAspectRatio="none">
              <defs>
                <filter id="line-glow" x="-50%" y="-50%" width="200%" height="200%">
                  <feGaussianBlur stdDeviation="1.5" result="blur" />
                  <feMerge>
                    <feMergeNode in="blur" />
                    <feMergeNode in="SourceGraphic" />
                  </feMerge>
                </filter>
              </defs>
              <g v-for="(src, i) in exposureSources" :key="'l-'+src.key">
                <path class="conn-path" :d="leftCurvePath(i)" :stroke="src.color" filter="url(#line-glow)" />
                <circle v-for="(t_val, di) in curveData[i % curveData.length].dots" :key="di"
                  r="1.6" :fill="src.color" class="conn-dot">
                  <animateMotion :path="leftCurvePath(i)" dur="2.6s" repeatCount="indefinite" :begin="`${-t_val * 2.6}s`" />
                </circle>
              </g>
              <g v-for="(ring, i) in exposureRings" :key="'r-'+ring.key">
                <path class="conn-path" :d="rightCurvePath(i)" :stroke="ring.color" filter="url(#line-glow)" />
                <circle v-for="(t_val, di) in curveData[i % curveData.length].dots" :key="di"
                  r="1.6" :fill="ring.color" class="conn-dot">
                  <animateMotion :path="rightCurvePath(i)" dur="2.6s" repeatCount="indefinite" :begin="`${-t_val * 2.6}s`" />
                </circle>
              </g>
            </svg>
          </div>
        </div>
      </div>

      <!-- 右列：资产概览（跨上区两行高度） -->
      <div class="top-right">
        <div class="panel asset-panel asset-tall">
          <div class="panel-header">
            <h2 class="panel-title">{{ t('dashboard.yourAssets') }}</h2>
          </div>
          <div class="asset-grid">
            <div class="asset-item" @click="goAsset('domain')">
              <span class="asset-num">{{ animatedData.domains }}</span>
              <span class="asset-label">{{ t('dashboard.domainLabel') }}</span>
              <span class="asset-delta" :class="{ up: stats.domainNew > 0 }">
                {{ stats.domainNew > 0 ? `+${stats.domainNew}` : '—' }}
              </span>
            </div>
            <div class="asset-item" @click="goAsset('ip')">
              <span class="asset-num">{{ animatedData.ips }}</span>
              <span class="asset-label">{{ t('dashboard.ipLabel') }}</span>
              <span class="asset-delta" :class="{ up: stats.ipNew > 0 }">
                {{ stats.ipNew > 0 ? `+${stats.ipNew}` : '—' }}
              </span>
            </div>
            <div class="asset-item" @click="goInventory()">
              <span class="asset-num">{{ animatedData.ports }}</span>
              <span class="asset-label">{{ t('dashboard.portLabel') }}</span>
              <span class="asset-delta" :class="{ up: stats.assetNew > 0 }">
                {{ stats.assetNew > 0 ? `+${stats.assetNew}` : '—' }}
              </span>
            </div>
            <div class="asset-item" @click="goAsset('site')">
              <span class="asset-num">{{ animatedData.sites }}</span>
              <span class="asset-label">{{ t('dashboard.siteLabel') }}</span>
              <span class="asset-delta" :class="{ up: stats.siteNew > 0 }">
                {{ stats.siteNew > 0 ? `+${stats.siteNew}` : '—' }}
              </span>
            </div>
          </div>
          <!-- 资产分类列表 -->
          <div class="asset-categories">
            <div class="cat-item" @click="goAsset('domain')">
              <span class="cat-dot" style="background: #3b82f6"></span>
              <span class="cat-name">{{ t('dashboard.domainLabel') }}</span>
              <span class="cat-count">{{ stats.domains }}</span>
            </div>
            <div class="cat-item" @click="goAsset('ip')">
              <span class="cat-dot" style="background: #14b8a6"></span>
              <span class="cat-name">{{ t('dashboard.ipLabel') }}</span>
              <span class="cat-count">{{ stats.ips }}</span>
            </div>
            <div class="cat-item" @click="goInventory()">
              <span class="cat-dot" style="background: #f97316"></span>
              <span class="cat-name">{{ t('dashboard.portLabel') }}</span>
              <span class="cat-count">{{ stats.portCount }}</span>
            </div>
            <div class="cat-item" @click="goAsset('site')">
              <span class="cat-dot" style="background: #8b5cf6"></span>
              <span class="cat-name">{{ t('dashboard.siteLabel') }}</span>
              <span class="cat-count">{{ stats.sites }}</span>
            </div>
            <div class="cat-item" @click="goAsset('dirscan')">
              <span class="cat-dot" style="background: #ef4444"></span>
              <span class="cat-name">{{ t('dashboard.cardDir') }}</span>
              <span class="cat-count">{{ stats.dirScans }}</span>
            </div>
            <div class="cat-item" @click="$router.push('/asset-management/space-search')">
              <span class="cat-dot" style="background: #6b7280"></span>
              <span class="cat-name">{{ t('dashboard.cardGroup') }}</span>
              <span class="cat-count">{{ stats.groups }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ===== 下区：3列 × 2行 网格 ===== -->
    <div class="layout-bottom">
      <!-- 第1行：资产变化 | 风险变化 | 执行任务/计算节点 -->
      <div class="panel change-panel">
        <div class="panel-header">
          <h2 class="panel-title">{{ t('dashboard.assetChanges') }}</h2>
        </div>
        <div class="change-body">
          <div class="change-split">
            <div class="change-main">
              <div class="change-hero">
                <span class="hero-num">{{ changes.assetTotal }}</span>
                <span class="hero-label">{{ t('dashboard.assetTotal') }}</span>
              </div>
              <div class="change-row">
                <span class="change-item up">
                  <span class="ci-num">+{{ changes.assetNewInWindow }}</span>
                  <span class="ci-label">{{ t('dashboard.newInWindow') }}</span>
                </span>
                <span class="change-item">
                  <span class="ci-num">{{ changes.assetGrowthRate }}%</span>
                  <span class="ci-label">{{ t('dashboard.growthRate') }}</span>
                </span>
              </div>
            </div>
            <div class="security-score" @click="$router.push({ path: '/asset-management/space-search', query: { view: 'global', tab: 'vuln' } })">
              <div class="score-ring" :class="scoreLevel">
                <span class="score-num">{{ securityScore }}</span>
              </div>
              <div class="score-label">
                <span class="score-text">{{ scoreText }}</span>
                <span class="score-sub">{{ t('dashboard.securityScore') }}</span>
              </div>
            </div>
          </div>
          <div class="change-cats" v-if="assetCategoryList.length">
            <div class="cat-row" v-for="c in assetCategoryList" :key="c.key">
              <span class="cat-name">{{ c.key || t('dashboard.uncategorized') }}</span>
              <span class="cat-val up">+{{ c.value }}</span>
            </div>
          </div>
        </div>
      </div>

      <div class="panel change-panel">
        <div class="panel-header">
          <h2 class="panel-title">{{ t('dashboard.riskChanges') }}</h2>
        </div>
        <div class="change-body">
          <div class="change-hero danger">
            <span class="hero-num">{{ changes.riskOpen }}</span>
            <span class="hero-label">{{ t('dashboard.riskOpen') }}</span>
          </div>
          <div class="change-row">
            <span class="change-item up">
              <span class="ci-num">+{{ changes.riskNewInWindow }}</span>
              <span class="ci-label">{{ t('dashboard.newInWindow') }}</span>
            </span>
            <span class="change-item success">
              <span class="ci-num">{{ changes.riskFixedInWindow }}</span>
              <span class="ci-label">{{ t('dashboard.riskFixed') }}</span>
            </span>
            <span class="change-item" :class="{ up: changes.riskNetChange >= 0, danger: changes.riskNetChange < 0 }">
              <span class="ci-num">{{ changes.riskNetChange >= 0 ? '+' : '' }}{{ changes.riskNetChange }}</span>
              <span class="ci-label">{{ t('dashboard.riskNetChange') }}</span>
            </span>
          </div>
        </div>
      </div>

      <div class="panel task-panel">
        <div class="panel-header">
          <h2 class="panel-title">{{ t('dashboard.cardTask') }}</h2>
        </div>
        <div class="task-stats">
          <div class="task-stat">
            <span class="task-num running">{{ taskStats.running }}</span>
            <span class="task-label">{{ t('dashboard.running') }}</span>
          </div>
          <div class="task-stat">
            <span class="task-num pending">{{ taskStats.pending }}</span>
            <span class="task-label">{{ t('dashboard.queued') }}</span>
          </div>
          <div class="task-stat">
            <span class="task-num completed">{{ taskStats.completed }}</span>
            <span class="task-label">{{ t('dashboard.completed') }}</span>
          </div>
          <div class="task-stat">
            <span class="task-num failed">{{ taskStats.failed }}</span>
            <span class="task-label">{{ t('dashboard.failed') }}</span>
          </div>
        </div>
        <div class="worker-row">
          <span class="worker-dot online"></span>
          <span class="worker-text">{{ workerStats.online }} {{ t('dashboard.workersOnline') }}</span>
          <span class="worker-dot offline"></span>
          <span class="worker-text">{{ workerStats.offline }} {{ t('dashboard.workersOffline') }}</span>
        </div>
      </div>

      <!-- 第2行：端口TOP10 | 核心服务占比 | 指纹分布 -->
      <div class="panel chart-panel">
        <div class="panel-header">
          <h2 class="panel-title">{{ t('dashboard.portTop10') }}</h2>
        </div>
        <div class="chart-body port-chart-bottom">
          <div v-if="stats.topPorts.length === 0" class="empty-hint">
            {{ t('dashboard.noPortData') }}
          </div>
          <div v-else ref="portBarChartRef" style="width:100%;height:100%;"></div>
        </div>
      </div>

      <div class="panel chart-panel">
        <div class="panel-header">
          <h2 class="panel-title">{{ t('dashboard.coreService') }}</h2>
        </div>
        <div class="chart-body service-chart">
          <div v-if="stats.topService.length === 0" class="empty-hint">
            {{ t('dashboard.noServiceData') }}
          </div>
          <div v-else ref="servicePieChartRef" style="width:100%;height:100%;"></div>
        </div>
      </div>

      <div class="panel chart-panel">
        <div class="panel-header">
          <h2 class="panel-title">{{ t('dashboard.fingerprintCategory') }}</h2>
        </div>
        <div class="chart-body service-chart">
          <div v-if="stats.topApp.length === 0" class="empty-hint">
            {{ t('dashboard.noFingerprintData') }}
          </div>
          <div v-else ref="appRoseChartRef" style="width:100%;height:100%;"></div>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
// 模块级变量：防止组件快速重挂载时重复拉取数据
let _dashboardLastLoadTime = 0
</script>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import * as echarts from 'echarts'
import request from '@/api/request'
import { getDashboardChanges } from '@/api/dashboard'
import { useThemeStore } from '@/stores/theme'

const router = useRouter()
const themeStore = useThemeStore()
const { t } = useI18n()

const VERSION_SOURCE_URL = 'https://raw.githubusercontent.com/tangxiaofeng7/cscan/main/VERSION'
const currentVersion = __APP_VERSION__
const latestVersion = ref('')
const versionLoading = ref(true)
let versionAbortController = null

function compareVersions(left, right) {
  const normalize = value => String(value || '')
    .replace(/^[^0-9]*/, '')
    .split(/[^0-9]+/)
    .filter(Boolean)
    .map(Number)
  const a = normalize(left)
  const b = normalize(right)
  const length = Math.max(a.length, b.length)
  for (let i = 0; i < length; i += 1) {
    const diff = (a[i] || 0) - (b[i] || 0)
    if (diff !== 0) return diff
  }
  return 0
}

const updateAvailable = computed(() => compareVersions(latestVersion.value, currentVersion) > 0)

async function loadLatestVersion() {
  versionAbortController?.abort()
  versionAbortController = new AbortController()
  const timeout = setTimeout(() => versionAbortController?.abort(), 5000)
  versionLoading.value = true
  try {
    const response = await fetch(VERSION_SOURCE_URL, {
      cache: 'no-store',
      signal: versionAbortController.signal
    })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    latestVersion.value = (await response.text()).trim()
  } catch (err) {
    if (err?.name !== 'AbortError') console.warn('[Dashboard] latest version unavailable:', err)
    latestVersion.value = ''
  } finally {
    clearTimeout(timeout)
    versionLoading.value = false
  }
}

// === 安全评分（加权风险密度 + 资产归一化 + 修复率加成）===
// 仅计入待处理（open）漏洞，已修复/已忽略的漏洞不再扣分
const securityScore = computed(() => {
  if (stats.vulns === 0) return 100
  const assetCount = Math.max(stats.ports, 1)
  // 加权风险值：Critical=10, High=5, Medium=2, Low=0.5, Info=0.1
  const riskValue =
    stats.vulnOpenCritical * 10 +
    stats.vulnOpenHigh * 5 +
    stats.vulnOpenMedium * 2 +
    stats.vulnOpenLow * 0.5 +
    stats.vulnOpenInfo * 0.1
  // 风险密度 = 每资产平均风险
  const density = riskValue / assetCount
  // 对数衰减：density=0→100, density↑→平缓下降
  let score = Math.round(100 / (1 + density * 3))
  // 修复率加成：已修复漏洞占比越高，最高可加 5 分
  const resolved = stats.vulnFixed + stats.vulnIgnored
  const fixRate = stats.vulns > 0 ? resolved / stats.vulns : 0
  score = Math.min(100, score + Math.round(fixRate * 5))
  return Math.max(0, score)
})
const scoreLevel = computed(() => {
  const s = securityScore.value
  if (s >= 90) return 'excellent'
  if (s >= 70) return 'good'
  if (s >= 50) return 'warning'
  return 'danger'
})
const scoreText = computed(() => {
  const s = securityScore.value
  if (s >= 90) return t('dashboard.scoreExcellent')
  if (s >= 70) return t('dashboard.scoreGood')
  if (s >= 50) return t('dashboard.scoreWarning')
  return t('dashboard.scoreDanger')
})

// SVG 曲线数据：仅用于流动光点的时间错开
const curveData = [
  { dots: [0.1, 0.35, 0.6, 0.85] },
  { dots: [0.2, 0.45, 0.7, 0.95] },
  { dots: [0.15, 0.4, 0.65, 0.9] },
  { dots: [0.05, 0.3, 0.55, 0.8] },
  { dots: [0.12, 0.37, 0.62, 0.87] },
  { dots: [0.25, 0.5, 0.75] }
]

// 连线覆盖层：整片 exposure-body 测量坐标后，把每一行起点连到中心点
const exposureBodyRef = ref(null)
const coreRef = ref(null)
const sourceItemRefs = ref([])
const ringCircleRefs = ref([])
// 初始用占位尺寸，避免与真实容器尺寸差距过大触发非均匀缩放伪影
const svgSize = ref({ w: 1, h: 1 })
const sourceAnchors = ref([])
const ringAnchors = ref([])
const centerAnchor = ref({ x: 0, y: 0 })
// 锚点未就绪前不渲染连线，避免初帧闪烁/涂抹伪影
const anchorsReady = ref(false)
let lineResizeObserver = null

function updateAnchors() {
  if (!exposureBodyRef.value) return
  const bodyRect = exposureBodyRef.value.getBoundingClientRect()
  // 容器尚未布局完成（宽度或高度为 0）时跳过，避免写入无意义坐标
  if (bodyRect.width < 2 || bodyRect.height < 2) {
    anchorsReady.value = false
    return
  }
  const left = bodyRect.left
  const top = bodyRect.top
  svgSize.value = { w: Math.max(1, bodyRect.width), h: Math.max(1, bodyRect.height) }

  sourceAnchors.value = exposureSources.value.map((_, i) => {
    const el = sourceItemRefs.value[i]
    if (!el) return { x: 0, y: 0 }
    const r = el.getBoundingClientRect()
    return { x: r.right - left, y: (r.top + r.bottom) / 2 - top }
  })

  const coreEl = coreRef.value
  if (coreEl) {
    const r = coreEl.getBoundingClientRect()
    centerAnchor.value = {
      x: (r.left + r.right) / 2 - left,
      y: (r.top + r.bottom) / 2 - top
    }
  }

  ringAnchors.value = exposureRings.value.map((_, i) => {
    const el = ringCircleRefs.value[i]
    if (!el) return { x: 0, y: 0 }
    const r = el.getBoundingClientRect()
    return { x: r.left - left, y: (r.top + r.bottom) / 2 - top }
  })

  // 所有锚点测量完成，允许渲染连线
  anchorsReady.value = true
}

// 当起点与中心点纵向重合时，贝塞尔曲线会退化为水平直线，叠加 line-glow 高斯模糊
// 后被 dilute 到几乎不可见，且右半段落入 core 实心背景被遮盖。此处注入一个微偏移 yTilt，
// 强制保留可见曲率，确保连线（含流动光点轨迹）始终可见。i 为偶数向下偏，奇数向上偏，
// 使多条线呈对称扇形而非重叠。
const CONN_Y_TILT = 14

function leftCurvePath(i) {
  const s = sourceAnchors.value[i]
  const c = centerAnchor.value
  if (!s || !c) return ''
  const tilt = Math.abs(s.y - c.y) < 2 ? (i % 2 === 0 ? CONN_Y_TILT : -CONN_Y_TILT) : 0
  const midX = (s.x + c.x) / 2
  const ctlY = s.y + tilt
  const endY = c.y + tilt
  return `M ${s.x.toFixed(1)} ${s.y.toFixed(1)} C ${midX.toFixed(1)} ${ctlY.toFixed(1)}, ${midX.toFixed(1)} ${endY.toFixed(1)}, ${c.x.toFixed(1)} ${c.y.toFixed(1)}`
}

function rightCurvePath(i) {
  const r = ringAnchors.value[i]
  const c = centerAnchor.value
  if (!r || !c) return ''
  const tilt = Math.abs(r.y - c.y) < 2 ? (i % 2 === 0 ? -CONN_Y_TILT : CONN_Y_TILT) : 0
  const midX = (c.x + r.x) / 2
  const ctlY = c.y + tilt
  const startY = r.y + tilt
  return `M ${c.x.toFixed(1)} ${c.y.toFixed(1)} C ${midX.toFixed(1)} ${ctlY.toFixed(1)}, ${midX.toFixed(1)} ${startY.toFixed(1)}, ${r.x.toFixed(1)} ${r.y.toFixed(1)}`
}

// === 暴露面总览数据 ===
const exposureSources = computed(() => [
  { key: 'ports', label: t('dashboard.exposedPorts'), value: stats.portCount, color: '#3b82f6', route: { path: '/asset-management/space-search', query: { view: 'global', tab: 'service' } } },
  { key: 'sites', label: t('dashboard.exposedSites'), value: stats.sites, color: '#8b5cf6', route: { path: '/asset-management/space-search', query: { view: 'global', tab: 'service' } } },
  { key: 'dirs', label: t('dashboard.sensitiveDirs'), value: stats.dirScans, color: '#ef4444', route: { path: '/asset-management/space-search', query: { view: 'global', tab: 'dir' } } },
  { key: 'vulns', label: t('dashboard.knownVulns'), value: stats.vulns, color: '#f97316', route: { path: '/asset-management/space-search', query: { view: 'global', tab: 'vuln' } } },
  { key: 'critical', label: t('dashboard.criticalRisks'), value: stats.vulnOpenCritical + stats.vulnOpenHigh, color: '#dc2626', route: { path: '/asset-management/space-search', query: { view: 'global', tab: 'vuln' } } }
])

const exposureRings = computed(() => {
  const totalTasks = taskStats.total || 1
  const unresolved = stats.vulnOpenCritical + stats.vulnOpenHigh + stats.vulnOpenMedium
  return [
    { key: 'scanned', label: t('dashboard.ringScanned'), value: taskStats.completed, color: '#22c55e', pct: Math.min(100, Math.round((taskStats.completed / totalTasks) * 100)), route: '/task' },
    { key: 'vulns', label: t('dashboard.ringVulns'), value: stats.vulns, color: '#f97316', pct: stats.vulns > 0 ? 100 : 0, route: { path: '/asset-management/space-search', query: { view: 'global', tab: 'vuln' } } },
    { key: 'open', label: t('dashboard.ringOpen'), value: unresolved, color: '#ef4444', pct: stats.vulns > 0 ? Math.min(100, Math.round((unresolved / stats.vulns) * 100)) : 0, route: { path: '/asset-management/space-search', query: { view: 'global', tab: 'vuln' } } }
  ]
})

// === 数据整合区 ===
const stats = reactive({
  ports: 0, portCount: 0, assetNew: 0,
  groups: 0,
  ips: 0, ipNew: 0,
  domains: 0, domainNew: 0,
  sites: 0, siteNew: 0,
  dirScans: 0,
  vulns: 0, vulnCritical: 0, vulnHigh: 0, vulnMedium: 0, vulnLow: 0, vulnInfo: 0,
  vulnOpen: 0, vulnFixed: 0, vulnIgnored: 0,
  vulnOpenCritical: 0, vulnOpenHigh: 0, vulnOpenMedium: 0, vulnOpenLow: 0, vulnOpenInfo: 0,
  topPorts: [], topService: [], topApp: []
})

const animatedData = reactive({
  ports: 0, groups: 0, ips: 0, domains: 0, sites: 0, dirScans: 0, vulns: 0, exposureTotal: 0
})

const taskStats = reactive({
  total: 0, completed: 0, running: 0, failed: 0, pending: 0,
  trendDays: [], trendCompleted: [], trendFailed: []
})

const workerStats = reactive({ online: 0, offline: 0 })

// === 工作台变化（T1.5）===
const changes = reactive({
  assetTotal: 0, assetNewInWindow: 0, assetGrowthRate: 0, assetByCategory: {},
  riskOpen: 0, riskNewInWindow: 0, riskFixedInWindow: 0, riskNetChange: 0, riskBySeverity: {}
})

const assetCategoryList = computed(() =>
  Object.keys(changes.assetByCategory).map(key => ({ key, value: changes.assetByCategory[key] }))
)

// === 图表 Ref ===
const portBarChartRef = ref()
const servicePieChartRef = ref()
const appRoseChartRef = ref()
let charts = {}
let refreshInterval = null

// === 路由跳转 ===
// 子域名/IP/端口/站点明细页已合并进资产空间搜索的目标详情 Tab，统一跳空间搜索
const exposureRouteMap = {
  domain: '/asset-management/space-search',
  ip: '/asset-management/space-search',
  port: { path: '/asset-management/space-search', query: { view: 'global', tab: 'service' } },
  site: { path: '/asset-management/space-search', query: { view: 'global', tab: 'service' } },
  dirscan: { path: '/asset-management/space-search', query: { view: 'global', tab: 'dir' } },
}
function goAsset(type) {
  const route = exposureRouteMap[type]
  if (route) router.push(route)
}
function goInventory() {
  router.push({ path: '/asset-management/space-search', query: { view: 'global', tab: 'service' } })
}

// === 辅助方法 ===
function calcPercent(part, total) {
  if (total === 0) return 0
  return Math.round((part / total) * 100)
}

function vulnBarWidth(level) {
  if (stats.vulnOpen === 0) return '0%'
  const map = { critical: stats.vulnOpenCritical, high: stats.vulnOpenHigh, medium: stats.vulnOpenMedium, low: stats.vulnOpenLow, info: stats.vulnOpenInfo }
  return `${(map[level] / stats.vulnOpen) * 100}%`
}

function animateValue(key, target, duration = 1000) {
  const start = animatedData[key]
  const change = target - start
  if (change === 0) return
  const startTime = performance.now()
  function update(currentTime) {
    const elapsed = currentTime - startTime
    const progress = Math.min(elapsed / duration, 1)
    const easeProgress = 1 - Math.pow(1 - progress, 3)
    animatedData[key] = Math.round(start + change * easeProgress)
    if (progress < 1) requestAnimationFrame(update)
  }
  requestAnimationFrame(update)
}

// === API 拉取 ===
async function silentFetch(apiRoute, params = {}) {
  try {
    const res = await request.post(apiRoute, params)
    return res.code === 0 ? res : null
  } catch (e) {
    return null
  }
}

let isLoadingData = false
let isComponentAlive = false

// Dashboard 统一汇总：单次请求获取全部首屏数据
async function loadDashboardSummary() {
  if (isLoadingData) return
  const now = Date.now()
  if (now - _dashboardLastLoadTime < 3000) return
  _dashboardLastLoadTime = now
  isLoadingData = true
  try {
    const res = await silentFetch('/dashboard/summary')
    if (!res) return
    if (!isComponentAlive) return

    // 资产概览
    stats.ports = res.assetTotal || 0
    stats.portCount = res.portCount || 0
    stats.assetNew = res.assetNew || 0
    stats.topPorts = res.topPorts || []
    stats.topService = res.topService || []
    stats.topApp = res.topApp || []
    animateValue('ports', stats.portCount)

    stats.ips = res.ipCount || 0
    stats.ipNew = 0 // summary API 不提供 ipNew，保持 0
    animateValue('ips', stats.ips)

    stats.domains = res.domainCount || 0
    stats.domainNew = 0
    animateValue('domains', stats.domains)

    stats.sites = res.siteCount || 0
    stats.siteNew = 0
    animateValue('sites', stats.sites)

    stats.dirScans = res.dirScans || 0
    animateValue('dirScans', stats.dirScans)

    stats.groups = res.groups || 0
    animateValue('groups', stats.groups)

    // 漏洞统计
    stats.vulns = res.vulnTotal || 0
    stats.vulnCritical = 0
    stats.vulnHigh = 0
    stats.vulnMedium = 0
    stats.vulnLow = 0
    stats.vulnInfo = 0
    stats.vulnOpen = res.vulnOpen || 0
    stats.vulnFixed = res.vulnFixed || 0
    stats.vulnIgnored = res.vulnIgnored || 0
    stats.vulnOpenCritical = res.vulnOpenCritical || 0
    stats.vulnOpenHigh = res.vulnOpenHigh || 0
    stats.vulnOpenMedium = res.vulnOpenMedium || 0
    stats.vulnOpenLow = res.vulnOpenLow || 0
    stats.vulnOpenInfo = res.vulnOpenInfo || 0
    animateValue('vulns', stats.vulnOpen)

    // 任务统计
    taskStats.total = res.taskTotal || 0
    taskStats.completed = res.taskCompleted || 0
    taskStats.running = res.taskRunning || 0
    taskStats.failed = res.taskFailed || 0
    taskStats.pending = res.taskPending || 0
    taskStats.trendDays = res.trendDays || []
    taskStats.trendCompleted = res.trendCompleted || []
    taskStats.trendFailed = res.trendFailed || []

    // Worker 统计
    workerStats.online = res.workerOnline || 0
    workerStats.offline = res.workerOffline || 0

    // 工作台变化
    if (res.asset && res.risk) {
      changes.assetTotal = res.asset.total || 0
      changes.assetNewInWindow = res.asset.newInWindow || 0
      changes.assetGrowthRate = res.asset.growthRate || 0
      changes.assetByCategory = res.asset.byCategory || {}
      changes.riskOpen = res.risk.open || 0
      changes.riskNewInWindow = res.risk.newInWindow || 0
      changes.riskFixedInWindow = res.risk.fixedInWindow || 0
      changes.riskNetChange = res.risk.netChange || 0
      changes.riskBySeverity = res.risk.bySeverity || {}
    }

    // 暴露面总数动画
    animateValue('exposureTotal', stats.portCount + stats.sites + stats.dirScans)
    await nextTick()
    if (!isComponentAlive) return
    initAllCharts()
  } finally {
    isLoadingData = false
  }
}

async function loadAllData() {
  await loadDashboardSummary()
}

// === 图表渲染 ===
function getThemeColors() {
  const isDark = themeStore.isDark
  return {
    text: isDark ? '#a1a1a1' : '#6b7280',
    title: isDark ? '#fafafa' : '#111827',
    line: isDark ? '#282828' : '#e5e7eb',
    tooltipBg: isDark ? '#171717' : '#ffffff',
    tooltipBorder: isDark ? '#282828' : '#e5e7eb',
    tooltipText: isDark ? '#fafafa' : '#111827',
    palette: ['#3b82f6', '#14b8a6', '#f97316', '#8b5cf6', '#ef4444', '#ec4899', '#06b6d4', '#84cc16']
  }
}

const SV_COLORS = {
  critical: '#ef4444', high: '#f97316', medium: '#eab308', low: '#3b82f6', info: '#6b7280'
}

function initAllCharts() {
  initPortBarChart()
  initServicePieChart()
  initAppRoseChart()
}

function initPortBarChart() {
  if (!portBarChartRef.value) return
  if (!charts.portBar) charts.portBar = echarts.init(portBarChartRef.value)
  const c = getThemeColors()

  let sourceData = [...stats.topPorts].sort((a, b) => a.count - b.count).slice(-10)
  const names = sourceData.map(d => String(d.name))
  const values = sourceData.map(d => d.count)

  if (names.length === 0) return

  charts.portBar.setOption({
    backgroundColor: 'transparent',
    grid: { left: '2%', right: '10%', bottom: '3%', top: '3%', containLabel: true },
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, backgroundColor: c.tooltipBg, borderColor: c.tooltipBorder, textStyle: { color: c.tooltipText, fontSize: 12 } },
    xAxis: { type: 'value', show: false },
    yAxis: { type: 'category', data: names, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { color: c.text, fontSize: 11 } },
    series: [{
      type: 'bar', data: values, barWidth: 8,
      showBackground: true,
      backgroundStyle: { color: c.line, borderRadius: 4 },
      itemStyle: { color: new echarts.graphic.LinearGradient(0, 0, 1, 0, [{ offset: 0, color: '#14b8a6' }, { offset: 1, color: '#3b82f6' }]), borderRadius: [0, 4, 4, 0] },
      label: { show: true, position: 'right', formatter: '{c}', color: c.title, fontSize: 11 }
    }]
  })
}

function initServicePieChart() {
  if (!servicePieChartRef.value || stats.topService.length === 0) return
  if (!charts.servicePie) charts.servicePie = echarts.init(servicePieChartRef.value)
  const c = getThemeColors()

  const data = stats.topService.slice(0, 8).map((d, i) => ({
    name: d.name, value: d.count, itemStyle: { color: c.palette[i % c.palette.length] }
  }))

  charts.servicePie.setOption({
    backgroundColor: 'transparent',
    tooltip: { trigger: 'item', backgroundColor: c.tooltipBg, borderColor: c.tooltipBorder, textStyle: { color: c.tooltipText, fontSize: 12 } },
    legend: { show: false },
    series: [{
      type: 'pie', radius: ['40%', '70%'], center: ['50%', '50%'],
      itemStyle: { borderColor: themeStore.isDark ? '#171717' : '#ffffff', borderWidth: 2 },
      label: { color: c.text, fontSize: 11, formatter: '{b} ({d}%)' },
      labelLine: { lineStyle: { color: c.line }, smooth: true, length: 10, length2: 8 },
      data: data
    }]
  })
}

function initAppRoseChart() {
  if (!appRoseChartRef.value || stats.topApp.length === 0) return
  if (!charts.appRose) charts.appRose = echarts.init(appRoseChartRef.value)
  const c = getThemeColors()

  const data = [...stats.topApp].slice(0, 8).reverse().map((d, i) => ({
    name: d.name, value: d.count, itemStyle: { color: c.palette[i % c.palette.length] }
  }))

  charts.appRose.setOption({
    backgroundColor: 'transparent',
    tooltip: { trigger: 'item', backgroundColor: c.tooltipBg, borderColor: c.tooltipBorder, textStyle: { color: c.tooltipText, fontSize: 12 } },
    legend: { show: false },
    series: [{
      type: 'pie', roseType: 'area',
      radius: ['15%', '75%'], center: ['50%', '50%'],
      itemStyle: { borderRadius: 4, borderColor: themeStore.isDark ? '#171717' : '#ffffff', borderWidth: 2 },
      label: { color: c.text, fontSize: 11 },
      labelLine: { lineStyle: { color: c.line }, smooth: true },
      data: data
    }]
  })
}

// === 生命周期 ===
function handleResize() {
  Object.values(charts).forEach(chart => chart?.resize())
}

watch(() => themeStore.isDark, () => {
  nextTick(() => {
    Object.values(charts).forEach(chart => chart?.dispose())
    charts = {}
    initAllCharts()
  })
})

onMounted(() => {
  isComponentAlive = true
  loadLatestVersion()
  window.addEventListener('resize', handleResize)

  // 1) 挂载后立刻同步测量一次锚点：此时 DOM 已就绪，getBoundingClientRect()
  //    会强制同步布局，拿到 exposure-body 的真实尺寸。这一步「不」依赖任何 await，
  //    避免 loadAllData 在数据加载期间让 exposure-body 短暂返回 0 尺寸，也避免
  //    await 把 nextTick 里的测量往后推，从而在默认 1x1 viewBox 下用
  //    preserveAspectRatio="none" 非均匀缩放、配合 drop-shadow / feGaussianBlur
  //    滤镜、以及从默认锚点出发的 animateMotion 在左上角形成异常涂抹伪影。
  updateAnchors()

  // 2) 测量之后「再」注册 ResizeObserver，顺序明确：
  //    先拍一张初始快照，再观察后续尺寸变化（数据回填 / 字体加载引发的重排会
  //    自动触发回调复测）。注意：observe 后浏览器会异步回调一次初始尺寸，
  //    等价于用真实尺寸再校正一次，只会更准。
  lineResizeObserver = new ResizeObserver(updateAnchors)
  if (exposureBodyRef.value) lineResizeObserver.observe(exposureBodyRef.value)

  // 3) 最后异步加载数据。数据返回后若发生过 0 尺寸跳变（导致第 1 步提前 return、
  //    anchorsReady 仍为 false），这里补一次测量，确保连线覆盖层最终能正确渲染。
  loadAllData().then(() => {
    if (!isComponentAlive) return
    updateAnchors()
    refreshInterval = setInterval(() => {
      if (isComponentAlive) loadAllData()
    }, 60000)
  })
})

onUnmounted(() => {
  isComponentAlive = false
  versionAbortController?.abort()
  clearInterval(refreshInterval)
  refreshInterval = null
  Object.values(charts).forEach(chart => chart?.dispose())
  window.removeEventListener('resize', handleResize)
  lineResizeObserver?.disconnect()
})
</script>

<style scoped lang="scss">
.dashboard-pd {
  padding: 20px 24px;
  background: hsl(var(--background));
  min-height: calc(100vh - 60px);
  color: hsl(var(--foreground));
  transition: background-color 0.2s ease;
}

.version-bar {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  min-height: 28px;
  margin-bottom: 12px;
  color: hsl(var(--muted-foreground));
  font-size: 13px;

  strong {
    color: hsl(var(--foreground));
    font-weight: 600;
  }

  a {
    margin-left: 4px;
    color: hsl(var(--primary));
    font-weight: 600;
    text-decoration: none;

    &:hover { text-decoration: underline; }
  }

  .version-divider {
    width: 1px;
    height: 14px;
    background: hsl(var(--border));
  }
}

// === 安全评分 ===
.security-score {
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  flex-shrink: 0;
  padding: 8px 14px;
  border-radius: var(--radius, 8px);
  border: 1px solid hsl(var(--border));
  transition: border-color 0.15s ease;

  &:hover {
    border-color: hsl(var(--muted-foreground) / 0.4);
  }
}

.score-ring {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 3px solid;

  &.excellent { border-color: #22c55e; .score-num { color: #22c55e; } }
  &.good { border-color: #3b82f6; .score-num { color: #3b82f6; } }
  &.warning { border-color: #f97316; .score-num { color: #f97316; } }
  &.danger { border-color: #ef4444; .score-num { color: #ef4444; } }
}

.score-num {
  font-size: 15px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.score-label {
  display: flex;
  flex-direction: column;
}

.score-text {
  font-size: 13px;
  font-weight: 600;
  color: hsl(var(--foreground));
}

.score-sub {
  font-size: 11px;
  color: hsl(var(--muted-foreground));
}

// ===== 上区：2列布局（左栏堆叠 / 右栏资产概览跨行）=====
.layout-top {
  display: grid;
  grid-template-columns: 1fr 360px;
  gap: 16px;
  // 关键：stretch 让右列自动拉伸到与左列等高
  align-items: stretch;

  @media (max-width: 1200px) {
    grid-template-columns: 1fr;
    align-items: start;
  }
}

.top-left {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

// 右列容器：让内部面板填满
.top-right {
  display: flex;
  flex-direction: column;
}

// 资产概览在上区右侧跨行高度：填满父容器
.asset-tall {
  flex: 1;
  display: flex;
  flex-direction: column;
}

// ===== 下区：3列 × 2行 网格 =====
.layout-bottom {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-top: 16px;

  @media (max-width: 1100px) {
    grid-template-columns: 1fr 1fr;
  }
  @media (max-width: 768px) {
    grid-template-columns: 1fr;
  }
}

// === 面板通用 ===
.panel {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: var(--radius, 8px);
  padding: 16px;
  transition: border-color 0.15s ease;

  &:hover {
    border-color: hsl(var(--muted-foreground) / 0.25);
  }
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.panel-title {
  font-size: 14px;
  font-weight: 600;
  color: hsl(var(--foreground));
}

// === 漏洞面板 ===
.vuln-stats-row {
  display: flex;
  gap: 0;
  margin-bottom: 12px;
}

.vuln-stat {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 10px 4px;
  border-right: 1px solid hsl(var(--border));

  &:last-child { border-right: none; }

  .vuln-num {
    font-size: 22px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: hsl(var(--foreground));
  }

  .vuln-label {
    font-size: 11px;
    color: hsl(var(--muted-foreground));
    margin-top: 2px;
  }

  &.total .vuln-num { color: hsl(var(--foreground)); }
  &.critical .vuln-num { color: #ef4444; }
  &.high .vuln-num { color: #f97316; }
  &.medium .vuln-num { color: #eab308; }
  &.low .vuln-num { color: #3b82f6; }
  &.info .vuln-num { color: #6b7280; }

  // 允许在窄屏下收缩，避免 6 列 min-content 撑破面板导致横向溢出
  min-width: 0;
}

// 窄屏：漏洞统计 6 列换行为 3 列，防止横向溢出
@media (max-width: 480px) {
  .vuln-stats-row {
    flex-wrap: wrap;
  }

  .vuln-stat {
    flex: 0 0 33.333%;
    padding: 8px 4px;
    border-right: none;

    .vuln-num { font-size: 18px; }
  }
}

.vuln-bar-track {
  height: 6px;
  border-radius: 3px;
  background: hsl(var(--muted));
  display: flex;
  overflow: hidden;
}

.vuln-bar-seg {
  height: 100%;
  transition: width 0.6s ease;

  &.critical { background: #ef4444; }
  &.high { background: #f97316; }
  &.medium { background: #eab308; }
  &.low { background: #3b82f6; }
  &.info { background: #6b7280; }
}

// === 图表面板 ===
.chart-panel {
  .chart-body {
    height: 240px;
    width: 100%;
  }

  // 下区第2行：端口 TOP 10 高度适配下区卡片
  .chart-body.port-chart-bottom {
    height: 220px;
  }
}

.service-chart {
  height: 200px;
}

.empty-hint {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: hsl(var(--muted-foreground));
  font-size: 13px;
}

// === 资产面板 ===
.asset-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 16px;
}

.asset-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 8px;
  border-radius: var(--radius, 8px);
  background: hsl(var(--muted) / 0.4);
  cursor: pointer;
  transition: background 0.15s;

  &:hover {
    background: hsl(var(--muted) / 0.7);
  }

  .asset-num {
    font-size: 20px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: hsl(var(--foreground));
  }

  .asset-label {
    font-size: 11px;
    color: hsl(var(--muted-foreground));
    margin-top: 2px;
  }

  .asset-delta {
    font-size: 11px;
    color: hsl(var(--muted-foreground));
    margin-top: 4px;
    font-variant-numeric: tabular-nums;

    &.up { color: #22c55e; }
  }
}

.asset-categories {
  border-top: 1px solid hsl(var(--border));
  padding-top: 12px;
}

.cat-item {
  display: flex;
  align-items: center;
  padding: 7px 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.12s;

  &:hover {
    background: hsl(var(--muted) / 0.5);
  }

  .cat-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-right: 10px;
    flex-shrink: 0;
  }

  .cat-name {
    font-size: 13px;
    color: hsl(var(--foreground));
    flex: 1;
  }

  .cat-count {
    font-size: 13px;
    font-weight: 600;
    color: hsl(var(--muted-foreground));
    font-variant-numeric: tabular-nums;
  }
}

// === 任务面板 ===
.task-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  margin-bottom: 14px;
}

.task-stat {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 8px 4px;

  .task-num {
    font-size: 18px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;

    &.running { color: #3b82f6; }
    &.pending { color: #f97316; }
    &.completed { color: #22c55e; }
    &.failed { color: #ef4444; }
  }

  .task-label {
    font-size: 11px;
    color: hsl(var(--muted-foreground));
    margin-top: 2px;
  }
}

.worker-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid hsl(var(--border));
}

.worker-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;

  &.online { background: #22c55e; }
  &.offline { background: #6b7280; }
}

.worker-text {
  font-size: 12px;
  color: hsl(var(--muted-foreground));
}

// === 暴露面总览面板 ===
.exposure-panel {
  overflow: visible;
}

.exposure-badge {
  font-size: 11px;
  font-weight: 500;
  padding: 2px 8px;
  border-radius: 999px;
  background: rgba(34, 197, 94, 0.1);
  color: #22c55e;
  border: 1px solid rgba(34, 197, 94, 0.2);
  display: flex;
  align-items: center;
  gap: 4px;

  &::before {
    content: '';
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: #22c55e;
    animation: pulse-dot 2s infinite;
  }
}

@keyframes pulse-dot {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.exposure-body {
  display: grid;
  grid-template-columns: auto minmax(80px, 1fr) auto minmax(80px, 1fr) auto;
  align-items: center;
  min-height: 220px;
  position: relative;
}

// 左侧暴露源
.exposure-sources {
  display: flex;
  flex-direction: column;
  gap: 4px;
  position: relative;
  z-index: 1;
}

.source-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;

  &:hover {
    background: hsl(var(--muted) / 0.5);
  }

  .source-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .source-name {
    font-size: 12px;
    color: hsl(var(--foreground));
    white-space: nowrap;
  }

  .source-count {
    font-size: 13px;
    font-weight: 700;
    color: hsl(var(--foreground));
    font-variant-numeric: tabular-nums;
    margin-left: auto;
  }
}

// 全区域 SVG 连线覆盖层
.exposure-lines-svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  z-index: 0;
  pointer-events: none;
  overflow: visible;
}

// 左右连接线区域（现在仅作为网格占位）
.exposure-lines-left,
.exposure-lines-right {
  display: flex;
  flex-direction: column;
  gap: 4px;
  justify-content: center;
  padding-top: 9px;
  padding-bottom: 9px;
}

.exposure-lines-right {
  gap: 14px;
  padding-top: 0;
  padding-bottom: 14px;
}

// 单条连接线（兼容旧结构，当前由覆盖层统一绘制）
.conn-line {
  height: 30px;
  display: flex;
  align-items: center;
  position: relative;
}

.conn-svg {
  width: 100%;
  height: 30px;
  display: block;
  overflow: visible;
}

.conn-path {
  fill: none;
  stroke-opacity: 0.45;
  stroke-width: 1.2;
  stroke-linecap: round;
  // 保证连线宽度在任何分辨率下都保持 1.2px，不随 SVG 缩放变化
  vector-effect: non-scaling-stroke;
}

.conn-dot {
  filter: drop-shadow(0 0 2px currentColor);
  opacity: 0.9;
}

// 中心发光圆
.exposure-core {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 160px;
  height: 160px;
  flex-shrink: 0;
  z-index: 1;
}

.core-glow {
  position: absolute;
  inset: -10px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(59, 130, 246, 0.25) 0%, rgba(59, 130, 246, 0.08) 50%, transparent 75%);
  animation: core-breathe 3s ease-in-out infinite;
}

@keyframes core-breathe {
  0%, 100% { transform: scale(1); opacity: 0.7; }
  50% { transform: scale(1.12); opacity: 1; }
}

.core-circle {
  position: relative;
  width: 120px;
  height: 120px;
  border-radius: 50%;
  border: 2px solid rgba(59, 130, 246, 0.5);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: hsl(var(--card));
  box-shadow:
    0 0 24px rgba(59, 130, 246, 0.2),
    0 0 60px rgba(59, 130, 246, 0.08),
    inset 0 0 24px rgba(59, 130, 246, 0.05);
  animation: ring-pulse 3s ease-in-out infinite;

  .core-num {
    font-size: 30px;
    font-weight: 800;
    color: hsl(var(--foreground));
    font-variant-numeric: tabular-nums;
    line-height: 1;
  }

  .core-label {
    font-size: 10px;
    color: hsl(var(--muted-foreground));
    margin-top: 5px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
}

@keyframes ring-pulse {
  0%, 100% { box-shadow: 0 0 24px rgba(59,130,246,0.2), 0 0 60px rgba(59,130,246,0.08), inset 0 0 24px rgba(59,130,246,0.05); }
  50% { box-shadow: 0 0 32px rgba(59,130,246,0.35), 0 0 80px rgba(59,130,246,0.12), inset 0 0 30px rgba(59,130,246,0.08); }
}

// 右侧环形统计
.exposure-rings {
  display: flex;
  flex-direction: column;
  gap: 14px;
  flex-shrink: 0;
  padding-bottom: 14px;
  position: relative;
  z-index: 1;
}

.ring-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  padding: 4px;
  border-radius: 6px;
  transition: background 0.15s;

  &:hover {
    background: hsl(var(--muted) / 0.5);
  }
}

.ring-circle {
  width: 54px;
  height: 54px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background:
    conic-gradient(var(--ring-color) var(--ring-pct), hsl(var(--muted)) var(--ring-pct));
  position: relative;
  box-shadow: 0 0 8px var(--ring-color);

  &::before {
    content: '';
    position: absolute;
    inset: 4px;
    border-radius: 50%;
    background: hsl(var(--card));
  }

  .ring-num {
    position: relative;
    font-size: 13px;
    font-weight: 700;
    color: hsl(var(--foreground));
    font-variant-numeric: tabular-nums;
  }
}

.ring-label {
  font-size: 10px;
  color: hsl(var(--muted-foreground));
}

// === 变化卡片 ===
.change-panel {
  .change-body {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  // 左侧变化数据 + 右侧安全评分并排
  .change-split {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;

    @media (max-width: 768px) {
      flex-wrap: wrap;
    }
  }

  .change-main {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .change-hero {
    display: flex;
    flex-direction: column;
    align-items: flex-start;

    .hero-num {
      font-size: 32px;
      font-weight: 800;
      line-height: 1;
      color: hsl(var(--foreground));
      font-variant-numeric: tabular-nums;
    }

    .hero-label {
      font-size: 12px;
      color: hsl(var(--muted-foreground));
      margin-top: 4px;
    }

    &.danger .hero-num { color: #ef4444; }
  }

  .change-row {
    display: flex;
    gap: 24px;
    flex-wrap: wrap;
  }

  .change-item {
    display: flex;
    flex-direction: column;

    .ci-num {
      font-size: 20px;
      font-weight: 700;
      color: hsl(var(--foreground));
      font-variant-numeric: tabular-nums;
    }

    .ci-label {
      font-size: 11px;
      color: hsl(var(--muted-foreground));
      margin-top: 2px;
    }

    &.up .ci-num { color: #22c55e; }
    &.success .ci-num { color: #22c55e; }
    &.danger .ci-num { color: #ef4444; }
  }

  .change-cats {
    display: flex;
    flex-direction: column;
    gap: 4px;
    border-top: 1px solid hsl(var(--border));
    padding-top: 12px;
  }

  .cat-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 12px;

    .cat-name { color: hsl(var(--foreground)); }
    .cat-val { font-weight: 600; font-variant-numeric: tabular-nums; }
  }
}

// === 款式适配（变量驱动，支持全部 10 个款式）===
.dashboard-pd[class*='style-'] {
  .panel {
    border-radius: var(--style-radius-stat, 8px);
    box-shadow: var(--style-shadow-stat, none);
  }
  .security-score { border-radius: var(--style-radius-bubble, 6px); }
  .asset-item { border-radius: var(--style-radius-bubble, 6px); }
}

// === 暗色微调 ===
.dashboard-pd.is-dark {
  .asset-item {
    background: hsl(var(--muted) / 0.3);
    &:hover { background: hsl(var(--muted) / 0.5); }
  }
  .cat-item:hover {
    background: hsl(var(--muted) / 0.3);
  }
  .core-glow {
    background: radial-gradient(circle, rgba(59, 130, 246, 0.35) 0%, rgba(59, 130, 246, 0.12) 50%, transparent 75%);
  }
  .core-circle {
    box-shadow:
      0 0 30px rgba(59, 130, 246, 0.3),
      0 0 80px rgba(59, 130, 246, 0.12),
      inset 0 0 30px rgba(59, 130, 246, 0.08);
  }
  .conn-path {
    stroke-opacity: 0.45;
  }
}
</style>
