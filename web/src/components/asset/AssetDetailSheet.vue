<template>
  <el-drawer
    v-model="visible"
    :title="asset?.host ? `${asset.host}:${asset.port}` : ''"
    size="640px"
    class="asset-detail-sheet"
  >
    <el-tabs v-model="activeTab" class="detail-tabs" @tab-change="handleTabChange">
      <el-tab-pane :label="$t('asset.assetDetail.details')" name="detail" lazy>
        <div v-loading="loading" class="detail-body">
          <template v-if="asset">
            <div class="section">
              <div class="section-header general">
                <el-icon><MapLocation /></el-icon>
                {{ $t('asset.targetView.secGeneral') }}
              </div>
              <div class="kv"><label>Domain</label><span class="mono">{{ asset.host }}</span></div>
              <div class="kv">
                <label>HTTP</label>
                <span v-if="getStatusCodeText(asset.status)" class="cert-status" :class="`cert-${statusClass}`">{{ getStatusCodeText(asset.status) }}</span>
                <span v-else class="muted">-</span>
              </div>
              <div class="kv"><label>{{ $t('asset.targetView.pageTitleCol') }}</label><span>{{ asset.title || '-' }}</span></div>
              <div class="kv">
                <label>IP</label>
                <div v-if="asset.ips && asset.ips.length" class="ip-badges">
                  <span v-for="ip in asset.ips" :key="ip" class="ip-badge">{{ ip }}</span>
                </div>
                <span v-else class="muted">{{ $t('asset.targetView.noIps') }}</span>
              </div>
              <div v-if="asset.screenshot" class="screenshot-wrap">
                <ScreenshotHoverPreview
                  :src="getScreenshotDataUrl(asset.screenshot)"
                  :alt="asset.title || asset.host"
                >
                  <el-image
                    :src="getScreenshotDataUrl(asset.screenshot)"
                    :preview-src-list="[getScreenshotDataUrl(asset.screenshot)]"
                    fit="contain"
                    lazy
                    preview-teleported
                  />
                </ScreenshotHoverPreview>
              </div>
            </div>

            <div class="section">
              <div class="section-header network">
                <el-icon><Connection /></el-icon>
                {{ $t('asset.targetView.secNetwork') }}
              </div>
              <div class="kv"><label>Host</label><span class="mono">{{ asset.host }}</span></div>
              <div class="kv"><label>Port</label><span class="mono">{{ asset.port }}</span></div>
              <div class="kv"><label>{{ $t('asset.targetView.server') }}</label><span>{{ asset.cname || asset.service || '-' }}</span></div>
            </div>

            <div v-if="cert" class="section">
              <div class="section-header cert">
                <el-icon><Lock /></el-icon>
                {{ $t('asset.targetView.secCertification') }}
              </div>
              <div class="kv">
                <label>SSL</label>
                <span class="cert-status" :class="`cert-${cert.status}`">
                  <el-icon><Lock /></el-icon>
                  {{ certStatusLabel(cert.status) }}
                </span>
              </div>
              <div class="kv"><label>{{ $t('asset.targetView.colIssuer') }}</label><span>{{ cert.issuerOrg || cert.issuerDn || '-' }}</span></div>
              <div class="kv"><label>CN</label><span class="mono">{{ cert.subjectCn || '-' }}</span></div>
              <div class="kv">
                <label>{{ $t('asset.targetView.colSans') }}</label>
                <div class="san-list">
                  <el-tag v-for="san in cert.sans.slice(0, 8)" :key="san" size="small">{{ san }}</el-tag>
                  <span v-if="cert.sans.length > 8" class="muted">+{{ cert.sans.length - 8 }}</span>
                  <span v-if="!cert.sans.length" class="muted">-</span>
                </div>
              </div>
              <div class="kv"><label>{{ $t('asset.targetView.colValidFrom') }}</label><span>{{ formatDate(cert.notBefore) }}</span></div>
              <div class="kv"><label>{{ $t('asset.targetView.colExpiresOn') }}</label><span>{{ formatDate(cert.notAfter) }}</span></div>
            </div>

            <div class="section">
              <div class="section-header tech">
                <el-icon><Files /></el-icon>
                {{ $t('asset.targetView.secTechnologies') }}
              </div>
              <div v-if="asset.technologies && asset.technologies.length" class="tech-list">
                <TechTag v-for="tech in asset.technologies" :key="tech" :tech="tech" />
              </div>
              <span v-else class="muted">-</span>
            </div>

            <div v-if="asset.httpHeader" class="section">
              <div class="section-header http">
                <el-icon><Document /></el-icon>
                {{ $t('asset.targetView.secHttpResponse') }}
              </div>
              <pre class="code-block">{{ asset.httpHeader }}</pre>
            </div>

            <div v-if="asset.httpBody" class="section">
              <div class="section-header http">
                <el-icon><Document /></el-icon>
                {{ $t('asset.targetView.secBody') }}
              </div>
              <pre class="code-block">{{ asset.httpBody }}</pre>
            </div>
          </template>
        </div>
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.timeline.tab')" name="timeline" lazy>
        <div class="timeline-body">
          <AssetTimeline
            v-if="activeTab === 'timeline' && asset"
            :asset-id="asset.id"
            :host="asset.host"
            :port="asset.port"
          />
        </div>
      </el-tab-pane>
    </el-tabs>
  </el-drawer>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MapLocation, Connection, Lock, Files, Document } from '@element-plus/icons-vue'
import AssetTimeline from './AssetTimeline.vue'
import TechTag from '@/components/common/TechTag.vue'
import ScreenshotHoverPreview from '@/components/common/ScreenshotHoverPreview.vue'
import { getAssetDetail, getAssetTargetCerts } from '@/api/asset'
import { getScreenshotDataUrl } from '@/utils/screenshot'
import { getStatusCodeText } from './targetViewUtils'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  assetId: { type: String, default: '' },
  targetId: { type: String, default: '' },
})

const emit = defineEmits(['update:modelValue'])

const { t } = useI18n()
const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v),
})

const activeTab = ref('detail')
const asset = ref(null)
const loading = ref(false)
const certs = ref([])

const statusClass = computed(() => {
  const code = parseInt(asset.value?.status)
  if (code >= 200 && code < 300) return 'valid'
  if (code >= 300 && code < 500) return 'expiring'
  return 'expired'
})

const cert = computed(() => {
  if (!certs.value.length || !asset.value) return null
  return certs.value.find(c => c.host === asset.value.host) || null
})

function certStatusLabel(status) {
  const map = {
    valid: 'asset.targetView.sslValid',
    expiring: 'asset.targetView.sslExpiring',
    expired: 'asset.targetView.sslExpired',
  }
  return t(map[status] || map.valid)
}

function formatDate(ms) {
  if (!ms) return '-'
  return new Date(ms).toLocaleString()
}

async function loadAssetDetail() {
  if (!props.assetId || loading.value) return
  loading.value = true
  asset.value = null
  certs.value = []
  try {
    const [detailRes, certRes] = await Promise.all([
      getAssetDetail({ id: props.assetId }),
      props.targetId
        ? getAssetTargetCerts({ targetId: props.targetId }).catch(() => null)
        : Promise.resolve(null),
    ])
    asset.value = detailRes?.data || null
    certs.value = certRes?.data?.list || certRes?.list || []
  } catch (err) {
    console.error('[AssetDetailSheet] load error:', err)
  } finally {
    loading.value = false
  }
}

function handleTabChange(tab) {
  if (tab === 'detail' && visible.value && !asset.value) loadAssetDetail()
}

watch(() => props.modelValue, open => {
  if (!open || !props.assetId) return
  activeTab.value = 'detail'
  loadAssetDetail()
})
</script>

<style scoped lang="scss">
.detail-tabs {
  :deep(.el-tabs__header) {
    margin-bottom: 18px;
  }
}

.detail-body {
  display: flex;
  flex-direction: column;
  gap: 20px;
  min-height: 120px;
}

.timeline-body {
  min-height: 120px;
}

.section {
  .section-header {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    margin-bottom: 10px;

    &.general { color: #3b82f6; }
    &.network { color: #6366f1; }
    &.cert { color: #22c55e; }
    &.tech { color: #a855f7; }
    &.http { color: #64748b; }
  }
}

.kv {
  display: flex;
  gap: 12px;
  padding: 4px 0;
  font-size: 13px;
  align-items: flex-start;

  label {
    width: 90px;
    flex-shrink: 0;
    color: var(--el-text-color-secondary);
  }

  span,
  .mono {
    color: var(--el-text-color-primary);
    word-break: break-all;
  }
}

.mono {
  font-family: var(--el-font-family-mono, monospace);
}

.muted {
  color: var(--el-text-color-secondary);
}

.ip-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;

  .ip-badge {
    display: inline-flex;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 11px;
    line-height: 16px;
    background: var(--el-fill-color);
    border: 1px solid var(--el-border-color-lighter);
    color: var(--el-text-color-regular);
  }
}

.screenshot-wrap {
  margin-top: 8px;

  .el-image {
    width: 100%;
    border-radius: 6px;
    border: 1px solid var(--el-border-color-lighter);
  }
}

.detail-screenshot-hover-preview {
  width: min(50vw, 960px);
  height: min(50vh, 620px);
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 32px);
}

.cert-status {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 500;
  line-height: 18px;

  &.cert-valid { background: rgba(103, 194, 58, 0.1); color: #67c23a; }
  &.cert-expiring { background: rgba(230, 162, 60, 0.1); color: #e6a23c; }
  &.cert-expired { background: rgba(245, 108, 108, 0.1); color: #f56c6c; }
}

.san-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.tech-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.code-block {
  max-height: 260px;
  overflow: auto;
  padding: 10px;
  border-radius: 6px;
  background: var(--el-fill-color-darker, #1e1e1e);
  color: var(--el-text-color-primary);
  font-family: var(--el-font-family-mono, monospace);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
  border: 1px solid var(--el-border-color-lighter);
}
</style>
