<template>
  <el-drawer
    :model-value="visible"
    :title="drawerTitle"
    size="60%"
    direction="rtl"
    destroy-on-close
    @update:model-value="handleClose"
  >
    <div v-if="asset" class="asset-detail">
      <el-tabs v-model="activeTab" class="asset-detail-tabs">
        <!-- 概览 -->
        <el-tab-pane :label="t('asset.assetDetail.overview')" name="overview">
          <!-- 顶部截图和基本信息 -->
          <div class="detail-header">
            <div
              class="detail-screenshot"
              @mouseenter="handlePreviewShow"
              @mouseleave="handlePreviewHide"
            >
              <img
                v-if="asset.screenshot"
                :src="formatScreenshotUrl(asset.screenshot)"
                :alt="asset.title"
                class="detail-screenshot-img"
                loading="lazy"
              />
              <div v-else class="detail-screenshot-placeholder">
                {{ t('asset.noScreenshot') }}
              </div>
            </div>
            <div class="detail-basic-info">
              <div class="info-row">
                <span class="info-label">URL:</span>
                <a :href="assetUrl" target="_blank" class="info-value link">
                  {{ assetUrl }}
                </a>
              </div>
              <div class="info-row">
                <span class="info-label">{{ t('asset.ip') }}:</span>
                <span class="info-value">{{ asset.ips?.length ? asset.ips.join(', ') : (asset.ip || '-') }}</span>
              </div>
              <div v-if="asset.status && asset.status !== '0'" class="info-row">
                <span class="info-label">{{ t('asset.statusCode') }}:</span>
                <el-tag :type="getStatusType(asset.status)" size="small">
                  {{ asset.status }}
                </el-tag>
              </div>
              <div v-if="asset.asn" class="info-row">
                <span class="info-label">ASN:</span>
                <span class="info-value">{{ asset.asn }}</span>
              </div>
              <div v-if="asset.title" class="info-row">
                <span class="info-label">{{ t('asset.title') }}:</span>
                <span class="info-value">{{ asset.title }}</span>
              </div>
            </div>
          </div>

          <!-- 概览内容 -->
          <div class="detail-content">
            <div class="section">
              <h4 class="section-title">{{ t('asset.assetDetail.networkInfo') }}</h4>
              <div class="info-grid">
                <div class="info-item">
                  <span class="item-label">{{ t('asset.assetDetail.host') }}:</span>
                  <span class="item-value">{{ asset.host || asset.name }}</span>
                </div>
                <div v-if="asset.port && asset.port !== 0" class="info-item">
                  <span class="item-label">{{ t('asset.assetDetail.port') }}:</span>
                  <span class="item-value">{{ asset.port }}</span>
                </div>
                <div class="info-item">
                  <span class="item-label">{{ t('asset.assetDetail.service') }}:</span>
                  <span class="item-value">{{ asset.service || '-' }}</span>
                </div>
                <div v-if="asset.cname" class="info-item">
                  <span class="item-label">{{ t('asset.assetDetail.cname') }}:</span>
                  <span class="item-value">{{ asset.cname }}</span>
                </div>
                <div v-if="asset.iconHash" class="info-item">
                  <span class="item-label">{{ t('asset.assetDetail.iconHash') }}:</span>
                  <div class="icon-hash-display">
                    <img
                      v-if="asset.iconHashBytes"
                      :src="'data:image/x-icon;base64,' + asset.iconHashBytes"
                      class="favicon-large"
                      loading="lazy"
                      @error="(e) => e.target.style.display = 'none'"
                    />
                    <span class="item-value">{{ asset.iconHash }}</span>
                  </div>
                </div>
              </div>
            </div>

            <div class="section">
              <h4 class="section-title">{{ t('asset.assetDetail.httpResponse') }}</h4>
              <div class="code-block">
                <pre>{{ asset.httpHeader || t('asset.assetDetail.noHttpData') }}</pre>
              </div>
            </div>

            <div v-if="asset.httpBody" class="section">
              <h4 class="section-title">{{ t('asset.assetDetail.httpBody') }}</h4>
              <div class="code-block">
                <pre>{{ asset.httpBody.substring(0, 1000) }}{{ asset.httpBody.length > 1000 ? '...' : '' }}</pre>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <!-- T4.3 变化时间线 -->
        <el-tab-pane :label="t('asset.timeline.tab')" name="timeline">
          <AssetTimeline
            :asset-id="asset.id"
            :authority="asset.authority"
            :host="asset.host"
            :port="asset.port"
          />
        </el-tab-pane>
      </el-tabs>
    </div>
  </el-drawer>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScreenshotUrl } from '@/utils/screenshot'
import AssetTimeline from '@/components/asset/AssetTimeline.vue'

const { t } = useI18n()
const activeTab = ref('overview')

const props = defineProps({
  visible: {
    type: Boolean,
    default: false
  },
  asset: {
    type: Object,
    default: null
  }
})

const emit = defineEmits(['update:visible', 'preview-show', 'preview-hide'])

// Computed properties
const drawerTitle = computed(() => {
  if (!props.asset) return ''
  const host = props.asset.host || props.asset.name
  const port = props.asset.port && props.asset.port !== 0 ? `:${props.asset.port}` : ''
  return `${host}${port}`
})

const assetUrl = computed(() => {
  if (!props.asset) return ''
  if (props.asset.url) return props.asset.url
  const host = props.asset.host || props.asset.name
  const port = props.asset.port
  if (port && port !== 0) {
    return `${port === 443 ? 'https' : 'http'}://${host}:${port}`
  }
  return `http://${host}`
})

// Methods
const handleClose = (value) => {
  emit('update:visible', value)
}

const handlePreviewShow = (event) => {
  emit('preview-show', props.asset, event)
}

const handlePreviewHide = () => {
  emit('preview-hide')
}

const getStatusType = (status) => {
  const code = parseInt(status)
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'warning'
  if (code >= 400 && code < 500) return 'danger'
  if (code >= 500) return 'danger'
  return 'info'
}
</script>

<style scoped lang="scss">
.asset-detail {
  padding: 0;
}

.detail-header {
  display: flex;
  gap: 20px;
  margin-bottom: 24px;
  padding: 16px;
  background: hsl(var(--muted) / 0.5);
  border-radius: 8px;
  border: 1px solid hsl(var(--border));
}

.detail-screenshot {
  flex-shrink: 0;
  width: 300px;
  height: 200px;
  border-radius: 8px;
  overflow: hidden;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.detail-screenshot-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.detail-screenshot-placeholder {
  color: hsl(var(--muted-foreground));
  font-size: 14px;
}

.detail-basic-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.info-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.info-label {
  font-weight: 500;
  color: hsl(var(--muted-foreground));
  min-width: 80px;
}

.info-value {
  color: hsl(var(--foreground));

  &.link {
    color: hsl(var(--primary));
    text-decoration: none;

    &:hover {
      text-decoration: underline;
    }
  }
}

.detail-content {
  padding: 0;
}

.section {
  margin-bottom: 24px;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
  color: hsl(var(--foreground));
  margin-bottom: 16px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 16px;
}

.info-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.item-label {
  font-size: 12px;
  color: hsl(var(--muted-foreground));
}

.item-value {
  font-size: 14px;
  color: hsl(var(--foreground));
}

.icon-hash-display {
  display: flex;
  align-items: center;
  gap: 8px;
}

.favicon-large {
  width: 24px;
  height: 24px;
}

.code-block {
  background: hsl(var(--muted) / 0.5);
  border: 1px solid hsl(var(--border));
  border-radius: 4px;
  padding: 12px;
  overflow-x: auto;

  pre {
    margin: 0;
    font-family: 'Courier New', monospace;
    font-size: 12px;
    line-height: 1.5;
    color: hsl(var(--foreground));
    white-space: pre-wrap;
    word-break: break-all;
  }
}

// Element Plus Drawer 深色主题覆盖
:deep(.el-drawer) {
  background: hsl(var(--background));

  .el-drawer__header {
    color: hsl(var(--foreground));
    border-bottom: 1px solid hsl(var(--border));
    margin-bottom: 0;
    padding-bottom: 16px;
  }

  .el-drawer__title {
    color: hsl(var(--foreground));
  }

  .el-drawer__body {
    background: hsl(var(--background));
  }
}
</style>
