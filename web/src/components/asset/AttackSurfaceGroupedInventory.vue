<template>
  <div class="grouped-inventory">
    <el-alert
      v-if="status === 'forbidden'"
      :title="$t('asset.attackSurface.dataForbidden')"
      type="warning"
      show-icon
      :closable="false"
    />
    <el-alert
      v-else-if="status === 'error'"
      :title="$t('asset.attackSurface.loadFailed')"
      type="error"
      show-icon
      :closable="false"
    >
      <template #default>
        <el-button link type="primary" @click="loadData">{{ $t('asset.attackSurface.retry') }}</el-button>
      </template>
    </el-alert>

    <template v-else>
      <el-table v-loading="status === 'loading'" :data="list" class="inventory-table">
        <template v-if="tab === 'subdomain'">
          <el-table-column :label="$t('asset.attackSurface.columns.domain')" prop="domain" min-width="240" />
          <el-table-column :label="$t('asset.attackSurface.columns.resolvedIps')" min-width="220">
            <template #default="{ row }">{{ (row.ips || []).join(', ') || '-' }}</template>
          </el-table-column>
          <el-table-column :label="$t('asset.attackSurface.columns.cname')" prop="cname" min-width="180" />
          <el-table-column :label="$t('asset.attackSurface.columns.source')" prop="source" width="130" />
          <el-table-column :label="$t('asset.attackSurface.columns.updated')" prop="updateTime" width="180" />
        </template>

        <template v-else-if="tab === 'tls'">
          <el-table-column :label="$t('asset.attackSurface.columns.host')" min-width="220">
            <template #default="{ row }"><span class="mono">{{ row.host }}:{{ row.port }}</span></template>
          </el-table-column>
          <el-table-column :label="$t('asset.attackSurface.columns.subject')" min-width="220">
            <template #default="{ row }">{{ row.subjectDn || row.subjectCn || '-' }}</template>
          </el-table-column>
          <el-table-column :label="$t('asset.attackSurface.columns.issuer')" min-width="200">
            <template #default="{ row }">{{ row.issuerOrg || row.issuerDn || '-' }}</template>
          </el-table-column>
          <el-table-column :label="$t('asset.attackSurface.columns.validFrom')" width="170">
            <template #default="{ row }">{{ formatDate(row.notBefore) }}</template>
          </el-table-column>
          <el-table-column :label="$t('asset.attackSurface.columns.expires')" width="190">
            <template #default="{ row }">
              <el-tag :type="certTagType(row.status)" size="small">
                {{ formatDate(row.notAfter) }} · {{ certStatusLabel(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
        </template>

        <template v-else>
          <el-table-column :label="groupLabel" min-width="240">
            <template #default="{ row }">
              <span class="mono">{{ row.key }}</span>
            </template>
          </el-table-column>
          <el-table-column
            v-if="tab === 'ip'"
            :label="$t('asset.attackSurface.columns.location')"
            prop="location"
            min-width="180"
          />
          <el-table-column v-if="tab === 'port'" :label="$t('asset.attackSurface.columns.services')" min-width="240">
            <template #default="{ row }">
              <el-tag v-for="item in (row.extras || []).slice(0, 8)" :key="item" size="small" class="item-tag">{{ item }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.attackSurface.columns.labels')" min-width="220">
            <template #default="{ row }">
              <el-tag v-for="label in (row.labels || []).slice(0, 6)" :key="label" size="small" class="item-tag">{{ label }}</el-tag>
              <span v-if="!(row.labels || []).length">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('asset.attackSurface.columns.services')" prop="count" width="140" />
        </template>

        <template #empty>
          <div class="empty-state">{{ $t('asset.attackSurface.noData') }}</div>
        </template>
      </el-table>

      <div class="pagination-wrap">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @current-change="loadData"
          @size-change="handleSizeChange"
        />
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAssetTargetCerts, getAssetTargetGroups, getDomainList } from '@/api/asset'

const props = defineProps({
  tab: { type: String, required: true },
  targetId: { type: String, default: '' },
  metricFilters: { type: Object, default: () => ({}) },
})

const { t } = useI18n()
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const status = ref('idle')
let requestSeq = 0

const groupLabel = computed(() => ({
  port: t('asset.attackSurface.serviceTabs.port'),
  ip: t('asset.attackSurface.serviceTabs.ip'),
  app: t('asset.attackSurface.columns.technology'),
  status: t('asset.attackSurface.columns.statusCode'),
}[props.tab] || t('asset.attackSurface.columns.value')))

function isForbidden(error) {
  return error?.response?.status === 403 || Number(error?.status || error?.code) === 403
}

function buildParams() {
  return {
    page: page.value,
    pageSize: pageSize.value,
    targetId: props.targetId || undefined,
    ...(props.metricFilters || {}),
  }
}

async function loadData() {
  const seq = ++requestSeq
  status.value = 'loading'
  try {
    let response
    if (props.tab === 'subdomain') {
      response = await getDomainList(buildParams())
    } else if (props.tab === 'tls') {
      response = await getAssetTargetCerts(buildParams())
    } else {
      response = await getAssetTargetGroups({ ...buildParams(), groupBy: props.tab })
    }
    if (seq !== requestSeq) return
    const payload = response?.data ?? response ?? {}
    list.value = payload.list || []
    total.value = Number(payload.total || 0)
    status.value = 'success'
  } catch (error) {
    if (seq !== requestSeq) return
    list.value = []
    total.value = 0
    status.value = isForbidden(error) ? 'forbidden' : 'error'
  }
}

function handleSizeChange() {
  page.value = 1
  loadData()
}

function formatDate(value) {
  if (!value) return '-'
  const date = new Date(Number(value))
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString()
}

function certTagType(certStatus) {
  if (certStatus === 'expired') return 'danger'
  if (certStatus === 'expiring') return 'warning'
  return 'success'
}

function certStatusLabel(certStatus) {
  if (certStatus === 'expired') return t('asset.targetView.certExpired')
  if (certStatus === 'expiring') return t('asset.targetView.certExpiring')
  if (certStatus === 'valid') return t('asset.targetView.certValid')
  return certStatus || '-'
}

watch(
  () => [props.tab, props.targetId, props.metricFilters],
  () => {
    page.value = 1
    loadData()
  },
  { deep: true }
)

onMounted(loadData)
</script>

<style scoped lang="scss">
.grouped-inventory {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.inventory-table {
  width: 100%;
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
}

.item-tag {
  margin: 2px 6px 2px 0;
}

.mono {
  font-family: var(--el-font-family-mono, monospace);
}

.empty-state {
  padding: 28px 0;
  color: var(--el-text-color-secondary);
}
</style>
