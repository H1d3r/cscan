<template>
  <div class="global-attack-surface-view">
    <div class="workbench-header">
      <div class="header-left">
        <el-button v-if="targetId" link class="back-button" @click="backToTargets">
          <el-icon><ArrowLeft /></el-icon>
        </el-button>
        <TargetSwitcher v-if="targetId" :target-id="targetId" @select="switchTarget" />
        <strong v-if="targetId && target?.targetValue" class="target-name">{{ target.targetValue }}</strong>
        <div v-else-if="!targetId">
          <h1>{{ $t('asset.attackSurface.title') }}</h1>
          <p>{{ $t('asset.attackSurface.subtitle') }}</p>
        </div>

        <template v-if="targetId && target">
          <span v-if="target.colorTag" class="color-dot" :style="{ background: target.colorTag }" />
          <TargetStatusBadge :status="target.scanStatus || ''" />
          <el-tag v-for="label in (target.labels || []).slice(0, 5)" :key="label" size="small">{{ label }}</el-tag>
        </template>
      </div>

      <div class="header-actions">
        <span v-if="target?.lastScanTime" class="last-scan">{{ formatRelativeTime(target.lastScanTime) }}</span>
        <el-button plain :loading="statsStatus === 'loading'" @click="fetchStats">
          <el-icon><Refresh /></el-icon>
          {{ $t('common.refresh') }}
        </el-button>
        <el-button v-if="targetId && target" circle @click="settingsOpen = true">
          <el-icon><Setting /></el-icon>
        </el-button>
        <el-button v-if="!targetId" @click="backToTargets">{{ $t('asset.attackSurface.targetView') }}</el-button>
      </div>
    </div>

    <div v-if="target?.memo" class="target-memo">{{ target.memo }}</div>

    <section class="metrics-section">
      <div v-if="statsStatus === 'loading' && metrics.length === 0" class="metrics-loading" v-loading="true" />
      <el-alert
        v-else-if="statsStatus === 'forbidden'"
        :title="$t('asset.attackSurface.statsForbidden')"
        type="warning"
        show-icon
        :closable="false"
      />
      <el-alert
        v-else-if="statsStatus === 'error'"
        :title="$t('asset.attackSurface.statsLoadFailed')"
        type="error"
        show-icon
        :closable="false"
      >
        <template #default>
          <el-button link type="primary" @click="fetchStats">{{ $t('asset.attackSurface.retry') }}</el-button>
        </template>
      </el-alert>
      <div v-else class="metric-list">
        <button
          v-for="metric in metrics"
          :key="metric.key"
          type="button"
          class="metric-bubble"
          :class="[{ active: selectedMetricKey === metric.key }, `tone-${metric.tone || 'info'}`]"
          @click="drillDown(metric)"
        >
          <span class="metric-label">{{ metricLabel(metric) }}</span>
          <strong>{{ metric.value ?? 0 }}</strong>
        </button>
      </div>
    </section>

    <el-alert
      v-if="inventoryDiagnostic"
      :title="inventoryDiagnostic"
      type="error"
      show-icon
      :closable="false"
      class="diagnostic"
    />

    <AttackSurfaceInventory
      v-model="activeTab"
      :target-id="targetId"
      :metric-filters="metricFilters"
      :reset-key="inventoryResetKey"
      @manual-tab-change="handleManualTabChange"
    />

    <TargetSettingsDrawer
      v-if="targetId"
      v-model="settingsOpen"
      :meta="target"
      @saved="fetchStats"
      @rediscovered="fetchStats"
      @deleted="backToTargets"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, Refresh, Setting } from '@element-plus/icons-vue'
import { getAssetStat } from '@/api/asset'
import { formatRelativeTime } from './targetViewUtils'
import AttackSurfaceInventory from './AttackSurfaceInventory.vue'
import TargetSettingsDrawer from './TargetSettingsDrawer.vue'
import TargetStatusBadge from './TargetStatusBadge.vue'
import TargetSwitcher from './TargetSwitcher.vue'

const OUTER_TABS = new Set(['service', 'dir', 'js', 'sensitive', 'vuln'])
const LEGACY_SERVICE_TABS = new Set(['subdomain', 'host', 'port', 'ip', 'app', 'status', 'tls'])

const props = defineProps({
  autoOpenSettings: { type: Boolean, default: false },
})
const emit = defineEmits(['settings-opened'])
const route = useRoute()
const router = useRouter()
const { t, te } = useI18n()

const targetId = computed(() => String(route.query.targetId || '').trim())
const activeTab = ref(validTab(route.query.tab))
const selectedMetricKey = ref('')
const metricFilters = ref({})
const metrics = ref([])
const target = ref(null)
const statsStatus = ref('idle')
const settingsOpen = ref(false)
const inventoryResetKey = ref(0)
const inventoryDiagnostic = ref('')
let statsRequestSeq = 0

function validTab(tab) {
  const value = String(tab || '').trim()
  if (LEGACY_SERVICE_TABS.has(value)) return 'service'
  return OUTER_TABS.has(value) ? value : 'service'
}

function routeMatches(tab, metric = '') {
  return String(route.query.tab || '') === tab
    && String(route.query.metric || '') === metric
    && !route.query.serviceTab
}

function isForbidden(error) {
  return error?.response?.status === 403 || Number(error?.status || error?.code) === 403
}

function metricLabel(metric) {
  if (metric.labelKey && te(metric.labelKey)) return t(metric.labelKey)
  return metric.label || metric.labelKey || metric.key
}

function workbenchQuery(tab = activeTab.value, metric = selectedMetricKey.value) {
  const query = { view: 'global', tab: validTab(tab) }
  if (targetId.value) query.targetId = targetId.value
  if (metric) query.metric = metric
  return query
}

function syncUrl(tab = activeTab.value, metric = '') {
  router.replace({
    path: '/asset-management/space-search',
    query: workbenchQuery(tab, metric),
  }).catch(() => {})
}

function restoreFromRoute() {
  inventoryDiagnostic.value = ''
  const routeTab = validTab(route.query.tab)
  const metricKey = String(route.query.metric || '')
  if (!metricKey) {
    selectedMetricKey.value = ''
    metricFilters.value = {}
    activeTab.value = routeTab
    if (!routeMatches(routeTab)) syncUrl(routeTab)
    return
  }

  const metric = metrics.value.find(item => item.key === metricKey)
  const drilldownTab = String(metric?.drilldown?.tab || '')
  if (!metric || !OUTER_TABS.has(drilldownTab)) {
    selectedMetricKey.value = ''
    metricFilters.value = {}
    activeTab.value = 'service'
    syncUrl('service')
    return
  }

  selectedMetricKey.value = metric.key
  metricFilters.value = { ...(metric.drilldown?.filters || {}) }
  activeTab.value = drilldownTab
  if (!routeMatches(drilldownTab, metric.key)) syncUrl(drilldownTab, metric.key)
}

function maybeOpenSettings() {
  if (props.autoOpenSettings && target.value) {
    settingsOpen.value = true
    emit('settings-opened')
  }
}

async function fetchStats() {
  const seq = ++statsRequestSeq
  statsStatus.value = 'loading'
  try {
    const response = await getAssetStat(targetId.value ? { targetId: targetId.value } : {})
    if (seq !== statsRequestSeq) return
    const payload = response?.data ?? {}
    metrics.value = (payload.metrics || []).filter(metric => metric?.applicable === true)
    target.value = payload.target || null
    statsStatus.value = 'success'
    restoreFromRoute()
    maybeOpenSettings()
  } catch (error) {
    if (seq !== statsRequestSeq) return
    statsStatus.value = isForbidden(error) ? 'forbidden' : 'error'
  }
}

function drillDown(metric) {
  const tab = String(metric?.drilldown?.tab || '')
  if (!OUTER_TABS.has(tab)) {
    inventoryDiagnostic.value = t('asset.attackSurface.unsupportedTab', { tab: tab || '-' })
    return
  }

  inventoryDiagnostic.value = ''
  selectedMetricKey.value = metric.key
  metricFilters.value = { ...(metric.drilldown?.filters || {}) }
  activeTab.value = tab
  inventoryResetKey.value += 1
  syncUrl(tab, metric.key)
}

function handleManualTabChange(tab) {
  const nextTab = validTab(tab)
  inventoryDiagnostic.value = ''
  selectedMetricKey.value = ''
  metricFilters.value = {}
  activeTab.value = nextTab
  inventoryResetKey.value += 1
  syncUrl(nextTab)
}

function switchTarget(newTargetId) {
  settingsOpen.value = false
  router.replace({
    path: '/asset-management/space-search',
    query: { view: 'global', targetId: newTargetId, tab: 'service' },
  })
}

function backToTargets() {
  settingsOpen.value = false
  router.replace({ path: '/asset-management/space-search', query: {} })
}

watch(targetId, () => {
  target.value = null
  metrics.value = []
  selectedMetricKey.value = ''
  metricFilters.value = {}
  activeTab.value = validTab(route.query.tab)
  inventoryResetKey.value += 1
  fetchStats()
})

watch(
  () => [route.query.tab, route.query.serviceTab, route.query.metric],
  () => {
    if (statsStatus.value === 'success') restoreFromRoute()
  }
)

watch(() => props.autoOpenSettings, maybeOpenSettings)

onMounted(fetchStats)
</script>

<style scoped lang="scss">
.global-attack-surface-view {
  min-width: 0;
}

.workbench-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;

  .header-left,
  .header-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  h1 {
    margin: 0;
    font-size: 26px;
  }

  p {
    margin: 4px 0 0;
    color: var(--el-text-color-secondary);
  }
}

.back-button {
  font-size: 20px;
}

.color-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}

.last-scan,
.target-memo {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.target-memo {
  margin-bottom: 12px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
}

.metrics-section {
  min-height: 58px;
  margin-bottom: 18px;
}

.metrics-loading {
  height: 58px;
}

.metric-list {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.metric-bubble {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--el-border-color);
  border-radius: 999px;
  padding: 7px 12px;
  background: var(--el-bg-color);
  color: var(--el-text-color-regular);
  cursor: pointer;
  transition: 0.15s ease;

  &:hover,
  &.active {
    border-color: var(--el-color-primary);
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }

  &.tone-danger strong { color: var(--el-color-danger); }
  &.tone-warning strong { color: var(--el-color-warning); }
  &.tone-success strong { color: var(--el-color-success); }
}

.metric-label {
  font-size: 12px;
}

.diagnostic {
  margin-bottom: 12px;
}

@media (max-width: 900px) {
  .workbench-header {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
