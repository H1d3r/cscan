<template>
  <div class="target-inventory">
    <!-- Filter bar -->
    <div class="filter-bar">
      <el-input
        v-model="filters.query"
        :placeholder="$t('asset.targetView.filterValue')"
        clearable
        class="filter-input"
        @clear="handleFilterChange"
        @keyup.enter="handleFilterChange"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>

      <el-select
        v-model="filters.ports"
        multiple
        collapse-tags
        clearable
        filterable
        :placeholder="$t('asset.targetView.subPorts')"
        class="filter-select"
        @change="handleFilterChange"
      >
        <el-option v-for="p in filterOptions.ports" :key="p" :value="p" :label="String(p)" />
      </el-select>

      <el-select
        v-model="filters.statusCodes"
        multiple
        collapse-tags
        clearable
        filterable
        :placeholder="$t('asset.targetView.subStatusCode')"
        class="filter-select"
        @change="handleFilterChange"
      >
        <el-option v-for="s in filterOptions.statusCodes" :key="s" :value="s" :label="String(s)" />
      </el-select>

      <el-select
        v-model="filters.technologies"
        multiple
        collapse-tags
        clearable
        filterable
        :placeholder="$t('asset.targetView.subTechnologies')"
        class="filter-select filter-select-lg"
        @change="handleFilterChange"
      >
        <el-option v-for="tech in filterOptions.technologies" :key="tech" :value="tech" :label="tech" />
      </el-select>

      <el-select
        v-model="filters.labels"
        multiple
        collapse-tags
        clearable
        filterable
        :placeholder="$t('asset.targetView.labels')"
        class="filter-select"
        @change="handleFilterChange"
      >
        <el-option v-for="label in filterOptions.labels" :key="label" :value="label" :label="label" />
      </el-select>

      <el-button link @click="handleReset">{{ $t('asset.targetView.filterReset') }}</el-button>

      <el-button link :class="{ 'is-loading': refreshing }" @click="handleManualRefresh">
        <el-icon v-if="refreshing" class="is-loading"><Loading /></el-icon>
        <el-icon v-else><Refresh /></el-icon>
        {{ $t('common.refresh') }}
      </el-button>
    </div>

    <!-- Sub tabs -->
    <!-- 子域名 Tab 仅域名目标可见：el-tabs 直接子节点不能用 v-if（注释占位会导致渲染崩溃），
         改为 pane 常驻 + 内容条件渲染 + IP 目标时 CSS 隐藏页签 -->
    <el-tabs v-model="activeTab" :class="{ 'hide-subdomain-tab': !isDomainTarget }" class="inventory-tabs" @tab-change="handleTabChange">
      <el-tab-pane :label="$t('asset.targetView.tabSubdomain')" name="subdomain">
        <template v-if="isDomainTarget">
          <el-table :data="subdomains" v-loading="subdomainsLoading" class="inv-table">
            <el-table-column :label="$t('asset.targetView.colDomainName')" min-width="220">
              <template #default="{ row }">
                <div class="domain-cell">
                  <span class="mono-value">{{ row.domain }}</span>
                  <el-tag v-if="row.isNew" size="small" type="danger" class="new-tag">NEW</el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="$t('asset.targetView.colResolvedIps')" min-width="220">
              <template #default="{ row }">
                <div v-if="row.ips && row.ips.length" class="tech-list">
                  <span v-for="ip in row.ips.slice(0, 5)" :key="ip" class="ip-badge">{{ ip }}</span>
                  <span v-if="row.ips.length > 5" class="muted">+{{ row.ips.length - 5 }}</span>
                </div>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('asset.targetView.colCname')" min-width="160">
              <template #default="{ row }">
                <span class="muted">{{ row.cname || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
              <template #default="{ row }">
                <div v-if="row.labels && row.labels.length" class="tech-list">
                  <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
                  <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
                </div>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('asset.targetView.colSource')" width="110">
              <template #default="{ row }">
                <span class="muted">{{ row.source || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('asset.targetView.colUpdateTime')" width="150">
              <template #default="{ row }">
                <span class="muted">{{ row.updateTime || '-' }}</span>
              </template>
            </el-table-column>
            <template #empty>
              <div class="empty-wrap">{{ $t('asset.targetView.noSubdomains') }}</div>
            </template>
          </el-table>

          <div v-if="subdomainsTotal > subdomainsPageSize" class="pagination-wrap">
            <el-pagination
              :current-page="subdomainsPage"
              :page-size="subdomainsPageSize"
              :total="subdomainsTotal"
              layout="total, prev, pager, next"
              @current-change="handleSubdomainsPage"
            />
          </div>
        </template>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.targetView.subServices')" name="services">
        <el-table
          :data="services"
          v-loading="servicesLoading"
          class="inv-table row-clickable"
          @row-click="row => $emit('view-asset', row)"
        >
          <el-table-column :label="$t('asset.targetView.columnService')" min-width="320">
            <template #default="{ row }">
              <div class="service-cell">
                <div class="service-main">
                  <img
                    v-if="row.iconBase64"
                    :src="`data:image/png;base64,${row.iconBase64}`"
                    class="svc-favicon"
                    @error="$event.target.style.display = 'none'"
                  >
                  <a class="service-host" :href="serviceUrl(row)" target="_blank" rel="noopener" @click.stop>{{ row.host }}</a>
                  <span class="service-separator">:</span>
                  <span class="service-port">{{ row.port }}</span>
                  <span
                    v-if="getStatusCodeText(row.statusCode)"
                    class="status-badge"
                    :class="getStatusCodeClass(row.statusCode)"
                  >{{ getStatusCodeText(row.statusCode) }}</span>
                </div>
                <div v-if="row.title" class="service-title">{{ row.title }}</div>
                <div v-if="row.ips && row.ips.length" class="ip-list">
                  <el-icon class="ip-icon"><Connection /></el-icon>
                  <span v-for="ip in row.ips.slice(0, 2)" :key="ip" class="ip-badge">{{ ip }}</span>
                  <span v-if="row.ips.length > 2" class="ip-more">+{{ row.ips.length - 2 }}</span>
                </div>
              </div>
            </template>
          </el-table-column>

          <el-table-column :label="$t('asset.targetView.columnScreenshot')" width="150" align="center">
            <template #default="{ row }">
              <el-image
                v-if="row.screenshot"
                :src="screenshotUrl(row.screenshot)"
                :preview-src-list="[screenshotUrl(row.screenshot)]"
                fit="cover"
                class="screenshot-thumb"
                preview-teleported
                @click.stop
              >
                <template #error>
                  <div class="screenshot-placeholder"><el-icon><Picture /></el-icon></div>
                </template>
              </el-image>
              <div v-else class="screenshot-placeholder"><el-icon><Picture /></el-icon></div>
            </template>
          </el-table-column>

          <el-table-column :label="$t('asset.targetView.columnTechnologies')" min-width="200">
            <template #default="{ row }">
              <div v-if="row.tech && row.tech.length" class="tech-list">
                <TechTag v-for="tech in row.tech.slice(0, 4)" :key="tech" :tech="tech" class="tech-tag" />
                <span v-if="row.tech.length > 4" class="tech-more">+{{ row.tech.length - 4 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>

          <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
            <template #default="{ row }">
              <div v-if="row.labels && row.labels.length" class="tech-list">
                <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
                <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>

          <el-table-column :label="$t('asset.targetView.colCertificate')" min-width="200">
            <template #default="{ row }">
              <div v-if="certByHost[row.host]" class="cert-cell">
                <span class="cert-status" :class="`cert-${certByHost[row.host].status}`">
                  {{ certStatusLabel(certByHost[row.host].status) }}
                </span>
                <div v-if="certByHost[row.host].issuerOrg" class="cert-issuer">
                  <el-icon><OfficeBuilding /></el-icon>
                  {{ certByHost[row.host].issuerOrg }}
                </div>
                <div v-if="certByHost[row.host].sans && certByHost[row.host].sans.length" class="cert-sans">
                  <el-icon><Briefcase /></el-icon>
                  {{ certByHost[row.host].sans.slice(0, 2).join(', ') }}
                  <span v-if="certByHost[row.host].sans.length > 2">+{{ certByHost[row.host].sans.length - 2 }}</span>
                </div>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>

          <el-table-column :label="$t('asset.targetView.colTimeCol')" width="140">
            <template #default="{ row }">
              <span class="muted">{{ formatRelativeTime(row.updateTime || row.createTime) }}</span>
            </template>
          </el-table-column>

          <template #empty>
            <div class="empty-wrap">{{ $t('asset.targetView.noData') }}</div>
          </template>
        </el-table>

        <div v-if="servicesTotal > servicesPageSize" class="pagination-wrap">
          <el-pagination
            :current-page="servicesPage"
            :page-size="servicesPageSize"
            :total="servicesTotal"
            layout="total, prev, pager, next"
            @current-change="handleServicesPage"
          />
        </div>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.targetView.subHosts')" name="host" lazy>
        <el-table :data="groups" v-loading="groupsLoading" class="inv-table">
          <el-table-column :label="$t('asset.targetView.columnTarget')">
            <template #default="{ row }">
              <span class="mono-value">{{ row.key }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
            <template #default="{ row }">
              <div v-if="row.labels && row.labels.length" class="tech-list">
                <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
                <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.numOfServices')" width="200">
            <template #default="{ row }">
              <span class="muted">{{ row.count }} {{ $t('asset.targetView.services') }}</span>
            </template>
          </el-table-column>
          <template #empty>
            <div class="empty-wrap">{{ $t('asset.targetView.noData') }}</div>
          </template>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.targetView.subPorts')" name="port" lazy>
        <el-table :data="groups" v-loading="groupsLoading" class="inv-table">
          <el-table-column :label="$t('asset.targetView.subPorts')">
            <template #default="{ row }">
              <span class="mono-value port-value">{{ row.key }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.columnService')">
            <template #default="{ row }">
              <div class="tech-list">
                <el-tag v-for="svc in (row.extras || []).slice(0, 6)" :key="svc" size="small" class="tech-tag">
                  {{ svc }}
                </el-tag>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
            <template #default="{ row }">
              <div v-if="row.labels && row.labels.length" class="tech-list">
                <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
                <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.numOfServices')" width="200">
            <template #default="{ row }">
              <span class="muted">{{ row.count }} {{ $t('asset.targetView.services') }}</span>
            </template>
          </el-table-column>
          <template #empty>
            <div class="empty-wrap">{{ $t('asset.targetView.noData') }}</div>
          </template>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.targetView.subIps')" name="ip" lazy>
        <el-table :data="groups" v-loading="groupsLoading" class="inv-table">
          <el-table-column label="IP">
            <template #default="{ row }">
              <span class="mono-value">{{ row.key }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.colLocation')">
            <template #default="{ row }">
              <span class="muted">{{ row.location || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
            <template #default="{ row }">
              <div v-if="row.labels && row.labels.length" class="tech-list">
                <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
                <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.numOfServices')" width="200">
            <template #default="{ row }">
              <span class="muted">{{ row.count }} {{ $t('asset.targetView.services') }}</span>
            </template>
          </el-table-column>
          <template #empty>
            <div class="empty-wrap">{{ $t('asset.targetView.noData') }}</div>
          </template>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.targetView.subTechnologies')" name="app" lazy>
        <el-table :data="groups" v-loading="groupsLoading" class="inv-table">
          <el-table-column :label="$t('asset.targetView.columnTechnologies')">
            <template #default="{ row }">
              <TechTag :tech="row.key" />
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
            <template #default="{ row }">
              <div v-if="row.labels && row.labels.length" class="tech-list">
                <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
                <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.numOfServices')" width="200">
            <template #default="{ row }">
              <span class="muted">{{ row.count }} {{ $t('asset.targetView.services') }}</span>
            </template>
          </el-table-column>
          <template #empty>
            <div class="empty-wrap">{{ $t('asset.targetView.noData') }}</div>
          </template>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.targetView.subStatusCode')" name="status" lazy>
        <el-table :data="groups" v-loading="groupsLoading" class="inv-table">
          <el-table-column :label="$t('asset.targetView.subStatusCode')" width="160">
            <template #default="{ row }">
              <span class="status-badge" :class="getStatusCodeClass(row.key)">{{ row.key }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.labels')" min-width="160">
            <template #default="{ row }">
              <div v-if="row.labels && row.labels.length" class="tech-list">
                <el-tag v-for="label in row.labels.slice(0, 4)" :key="label" size="small" class="label-tag">{{ label }}</el-tag>
                <span v-if="row.labels.length > 4" class="tech-more">+{{ row.labels.length - 4 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.numOfServices')" width="200">
            <template #default="{ row }">
              <span class="muted">{{ row.count }} {{ $t('asset.targetView.services') }}</span>
            </template>
          </el-table-column>
          <template #empty>
            <div class="empty-wrap">{{ $t('asset.targetView.noData') }}</div>
          </template>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.targetView.subTls')" name="tls" lazy>
        <el-table :data="certs" v-loading="certsLoading" class="inv-table">
          <el-table-column label="Host" min-width="200">
            <template #default="{ row }">
              <span class="mono-value">{{ row.host }}:{{ row.port }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.colSubjectDn')" min-width="220">
            <template #default="{ row }">
              <span class="muted dn-value">{{ row.subjectDn || row.subjectCn || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.colIssuer')" min-width="160">
            <template #default="{ row }">
              <span class="muted">{{ row.issuerOrg || row.issuerDn || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.colValidFrom')" width="150">
            <template #default="{ row }">
              <span class="muted">{{ formatDate(row.notBefore) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.targetView.colExpiresOn')" width="170">
            <template #default="{ row }">
              <span class="cert-status" :class="`cert-${row.status}`">
                {{ formatDate(row.notAfter) }} · {{ certStatusLabel(row.status) }}
              </span>
            </template>
          </el-table-column>
          <template #empty>
            <div class="empty-wrap">{{ $t('asset.targetView.noCerts') }}</div>
          </template>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, Picture, Connection, OfficeBuilding, Briefcase, Refresh, Loading } from '@element-plus/icons-vue'
import { getAssetTargetAssets, getAssetTargetGroups, getAssetTargetCerts, getAssetFilterOptions, getDomainList } from '@/api/asset'
import { formatRelativeTime, getStatusCodeClass, getStatusCodeText } from './targetViewUtils'
import { getScreenshotDataUrl } from '@/utils/screenshot'
import TechTag from '@/components/common/TechTag.vue'

const { t } = useI18n()

const props = defineProps({
  targetId: { type: String, required: true },
})

const emit = defineEmits(['view-asset'])

const activeTab = ref('services')
const filters = reactive({ query: '', ports: [], statusCodes: [], technologies: [], labels: [] })
const filterOptions = ref({ ports: [], statusCodes: [], technologies: [], labels: [] })

const services = ref([])
const servicesLoading = ref(false)
const servicesTotal = ref(0)
const servicesPage = ref(1)
const servicesPageSize = 20

const groups = ref([])
const groupsLoading = ref(false)

const certs = ref([])
const certsLoading = ref(false)

// 子域名 Tab（仅域名目标）：targetId 形如 "domain:example.com"
const isDomainTarget = computed(() => props.targetId.startsWith('domain:'))
const targetValue = computed(() => props.targetId.slice(props.targetId.indexOf(':') + 1))
const subdomains = ref([])
const subdomainsLoading = ref(false)
const subdomainsTotal = ref(0)
const subdomainsPage = ref(1)
const subdomainsPageSize = 20

const refreshing = ref(false)
let filterDebounce = null

// host → cert 摘要（Services 列证书徽章用）
const certByHost = computed(() => {
  const map = {}
  for (const c of certs.value) {
    if (!map[c.host]) map[c.host] = c
  }
  return map
})

function serviceUrl(row) {
  const scheme = row.service === 'https' || row.port === 443 ? 'https' : 'http'
  return `${scheme}://${row.host}:${row.port}`
}

function certStatusLabel(status) {
  const map = {
    valid: 'asset.targetView.certValid',
    expiring: 'asset.targetView.certExpiring',
    expired: 'asset.targetView.certExpired',
  }
  return t(map[status] || map.valid)
}

// 截图原始值为纯 base64，需要补 data URI 前缀才能渲染
function screenshotUrl(screenshot) {
  return getScreenshotDataUrl(screenshot)
}

function formatDate(ms) {
  if (!ms) return '-'
  return new Date(ms).toLocaleDateString()
}

async function fetchServices() {
  servicesLoading.value = true
  try {
    const res = await getAssetTargetAssets({
      targetId: props.targetId,
      page: servicesPage.value,
      pageSize: servicesPageSize,
      query: filters.query || undefined,
      ports: filters.ports.length ? filters.ports : undefined,
      statusCodes: filters.statusCodes.length ? filters.statusCodes : undefined,
      technologies: filters.technologies.length ? filters.technologies : undefined,
      labels: filters.labels.length ? filters.labels : undefined,
    })
    if (res?.data) {
      services.value = res.data.list || []
      servicesTotal.value = res.data.total || 0
    }
  } catch (err) {
    console.error('[TargetInventory] fetchServices error:', err)
  } finally {
    servicesLoading.value = false
  }
}

async function fetchGroups() {
  if (activeTab.value === 'services' || activeTab.value === 'tls') return
  groupsLoading.value = true
  try {
    const res = await getAssetTargetGroups({
      targetId: props.targetId,
      groupBy: activeTab.value,
      query: filters.query || undefined,
      ports: filters.ports.length ? filters.ports : undefined,
      statusCodes: filters.statusCodes.length ? filters.statusCodes : undefined,
      technologies: filters.technologies.length ? filters.technologies : undefined,
      labels: filters.labels.length ? filters.labels : undefined,
    })
    groups.value = res?.data?.list || res?.list || []
  } catch (err) {
    console.error('[TargetInventory] fetchGroups error:', err)
    groups.value = []
  } finally {
    groupsLoading.value = false
  }
}

async function fetchCerts() {
  certsLoading.value = true
  try {
    const res = await getAssetTargetCerts({ targetId: props.targetId })
    certs.value = res?.data?.list || res?.list || []
  } catch (err) {
    console.error('[TargetInventory] fetchCerts error:', err)
    certs.value = []
  } finally {
    certsLoading.value = false
  }
}

async function fetchFilterOptions() {
  try {
    const res = await getAssetFilterOptions({})
    filterOptions.value = res?.data || res || {}
  } catch (err) {
    console.error('[TargetInventory] filterOptions error:', err)
  }
}

function handleFilterChange() {
  servicesPage.value = 1
  subdomainsPage.value = 1
  fetchServices()
  fetchGroups()
  fetchCerts()
  fetchSubdomains()
}

function handleReset() {
  filters.query = ''
  filters.ports = []
  filters.statusCodes = []
  filters.technologies = []
  filters.labels = []
  handleFilterChange()
}

async function fetchSubdomains() {
  if (!isDomainTarget.value) return
  subdomainsLoading.value = true
  try {
    const res = await getDomainList({
      rootDomain: targetValue.value,
      page: subdomainsPage.value,
      pageSize: subdomainsPageSize,
      query: filters.query || undefined,
      labels: filters.labels.length ? filters.labels : undefined,
    })
    const payload = res?.data ?? res
    subdomains.value = payload?.list || []
    subdomainsTotal.value = payload?.total || 0
  } catch (err) {
    console.error('[TargetInventory] fetchSubdomains error:', err)
    subdomains.value = []
  } finally {
    subdomainsLoading.value = false
  }
}

function handleSubdomainsPage(p) {
  subdomainsPage.value = p
  fetchSubdomains()
}

function handleTabChange() {
  if (activeTab.value === 'services') fetchServices()
  else if (activeTab.value === 'tls') fetchCerts()
  else if (activeTab.value === 'subdomain') fetchSubdomains()
  else fetchGroups()
}

// 手动刷新：重新拉取过滤选项与当前各视图数据（不重置分页）
async function handleManualRefresh() {
  refreshing.value = true
  try {
    fetchFilterOptions()
    await Promise.all([fetchServices(), fetchCerts(), fetchSubdomains()])
    if (activeTab.value !== 'services' && activeTab.value !== 'tls' && activeTab.value !== 'subdomain') {
      await fetchGroups()
    }
  } finally {
    refreshing.value = false
  }
}

// 供详情页气泡下钻调用：切换到指定子 Tab 并拉取数据；
// refresh 供详情页头部刷新按钮联动
defineExpose({
  activateTab(name) {
    if (!name) return
    activeTab.value = name
    handleTabChange()
  },
  refresh: handleManualRefresh,
})

function handleServicesPage(p) {
  servicesPage.value = p
  fetchServices()
}

watch(() => filters.query, () => {
  if (filterDebounce) clearTimeout(filterDebounce)
  filterDebounce = setTimeout(handleFilterChange, 500)
})

watch(() => props.targetId, () => {
  servicesPage.value = 1
  handleFilterChange()
})

onMounted(() => {
  fetchFilterOptions()
  fetchServices()
  fetchCerts()
})

onUnmounted(() => {
  if (filterDebounce) clearTimeout(filterDebounce)
})
</script>

<style scoped lang="scss">
.target-inventory {
  // IP 目标隐藏子域名页签（pane 常驻避免 el-tabs 注释占位崩溃）
  .inventory-tabs.hide-subdomain-tab {
    :deep(#tab-subdomain) {
      display: none;
    }
  }

  .filter-bar {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin-bottom: 12px;

    .filter-input {
      width: 220px;
    }

    .filter-select {
      width: 150px;
    }

    .filter-select-lg {
      width: 220px;
    }
  }
}

.inv-table {
  &.row-clickable {
    cursor: pointer;

    :deep(.el-table__row):hover {
      background-color: var(--el-fill-color-light);
    }
  }
}

.service-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;

  .service-main {
    display: flex;
    align-items: center;
    gap: 4px;

    .svc-favicon {
      width: 16px;
      height: 16px;
      margin-right: 2px;
      border-radius: 2px;
      flex-shrink: 0;
      object-fit: contain;
    }

    .service-host {
      font-weight: 500;
      font-size: 14px;
      color: var(--el-color-primary);
      text-decoration: none;

      &:hover {
        text-decoration: underline;
      }
    }

    .service-separator {
      color: var(--el-text-color-secondary);
    }

    .service-port {
      font-weight: 600;
      font-size: 14px;
    }
  }

  .service-title {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ip-list {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;

    .ip-icon {
      font-size: 12px;
      color: var(--el-text-color-secondary);
    }

    .ip-badge {
      display: inline-flex;
      padding: 1px 6px;
      border-radius: 3px;
      font-size: 11px;
      line-height: 16px;
      background: var(--el-fill-color);
      color: var(--el-text-color-regular);
      border: 1px solid var(--el-border-color-lighter);
    }

    .ip-more {
      font-size: 11px;
      color: var(--el-text-color-secondary);
    }
  }
}

.status-badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 500;
  line-height: 16px;

  &.status-2xx { background: rgba(103, 194, 58, 0.1); color: #67c23a; }
  &.status-3xx { background: rgba(64, 158, 255, 0.1); color: #409eff; }
  &.status-4xx { background: rgba(230, 162, 60, 0.1); color: #e6a23c; }
  &.status-5xx { background: rgba(245, 108, 108, 0.1); color: #f56c6c; }
  &.status-other { background: rgba(144, 147, 153, 0.1); color: #909399; }
}

.screenshot-thumb {
  width: 120px;
  height: 80px;
  border-radius: 4px;
  overflow: hidden;
}

.screenshot-placeholder {
  width: 120px;
  height: 80px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  color: var(--el-text-color-secondary);
  font-size: 24px;
}

.tech-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;

  .tech-more {
    font-size: 11px;
    color: var(--el-text-color-secondary);
  }
}

.label-tag {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cert-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;

  .cert-issuer,
  .cert-sans {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.cert-status {
  display: inline-flex;
  align-items: center;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 500;
  line-height: 16px;

  &.cert-valid { background: rgba(103, 194, 58, 0.1); color: #67c23a; }
  &.cert-expiring { background: rgba(230, 162, 60, 0.1); color: #e6a23c; }
  &.cert-expired { background: rgba(245, 108, 108, 0.1); color: #f56c6c; }
}

.muted {
  color: var(--el-text-color-secondary);
}

.mono-value {
  font-family: var(--el-font-family-mono, monospace);
  font-weight: 500;

  &.port-value {
    font-size: 15px;
  }
}

.dn-value {
  font-size: 12px;
  word-break: break-all;
}

.domain-cell {
  display: flex;
  align-items: center;
  gap: 6px;

  .new-tag {
    flex-shrink: 0;
  }
}

.empty-wrap {
  padding: 32px 0;
  color: var(--el-text-color-secondary);
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  padding-top: 8px;
}
</style>
