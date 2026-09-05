<template>
  <div class="asset-space-search">
    <GlobalAttackSurfaceView
      v-if="isGlobalView"
      :auto-open-settings="autoOpenSettings"
      @settings-opened="autoOpenSettings = false"
    />

    <template v-else>
      <div class="page-header">
        <div class="header-content">
          <h1>{{ $t('navigation.assetManagement') }}</h1>
          <p class="description">{{ $t('asset.spaceSearchDescription') }}</p>
        </div>
        <div class="header-actions">
          <div class="header-search" />
          <el-radio-group :model-value="'target'" size="default" @change="handleListModeChange">
            <el-radio-button value="target">{{ $t('asset.globalView.tabTargetView') }}</el-radio-button>
            <el-radio-button value="global">{{ $t('asset.globalView.tabGlobalView') }}</el-radio-button>
          </el-radio-group>
        </div>
      </div>
      <AssetInventoryCardView
        ref="cardViewRef"
        @create-target="openAddDialog"
        @start-scan="handleStartScan"
        @view-target="handleViewTarget"
        @edit-target="handleEditTarget"
      />
    </template>

    <el-dialog
      v-model="addDialogVisible"
      :title="$t('asset.manualAddAssetTitle')"
      width="640px"
      :close-on-click-modal="false"
    >
      <div class="add-batch-hint">{{ $t('asset.addBatchHint') }}</div>
      <el-input
        v-model="addForm.targets"
        type="textarea"
        :rows="10"
        :placeholder="$t('asset.addBatchPlaceholder')"
      />
      <div v-if="addErrors.length" class="add-errors">
        <div v-for="(e, i) in addErrors" :key="i" class="add-error-line">
          {{ $t('asset.addBatchLine', { line: e.line, target: e.target, msg: e.message }) }}
        </div>
      </div>
      <template #footer>
        <el-button @click="addDialogVisible = false">
          {{ $t('asset.addAssetCancel') }}
        </el-button>
        <el-button type="primary" :loading="addSubmitting" @click="handleAddSubmit">
          {{ $t('asset.addAssetConfirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import AssetInventoryCardView from '@/components/asset/AssetInventoryCardView.vue'
import GlobalAttackSurfaceView from '@/components/asset/GlobalAttackSurfaceView.vue'
import { importAssets } from '@/api/asset'
import { validateTargets } from '@/utils/target'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const cardViewRef = ref(null)
const autoOpenSettings = ref(false)
const isGlobalView = computed(() => route.query.view === 'global')

function workbenchQuery(targetId = '') {
  const query = { view: 'global', tab: 'service' }
  if (targetId) query.targetId = targetId
  return query
}

function handleListModeChange(mode) {
  if (mode === 'global') {
    autoOpenSettings.value = false
    router.replace({ path: '/asset-management/space-search', query: workbenchQuery() })
  }
}

function openTarget(targetId, openSettings = false) {
  autoOpenSettings.value = openSettings
  router.push({
    path: '/asset-management/space-search',
    query: workbenchQuery(targetId),
  })
}

function handleViewTarget(targetId) {
  openTarget(targetId, false)
}

function handleEditTarget(targetId) {
  openTarget(targetId, true)
}

function handleStartScan(targetIds) {
  if (targetIds && targetIds.length > 0) {
    router.push({ path: '/task/create', query: { targetId: targetIds.join(',') } })
  } else {
    router.push('/task/create')
  }
}

const addDialogVisible = ref(false)
const addSubmitting = ref(false)
const addForm = reactive({ targets: '' })
const addErrors = ref([])

function openAddDialog() {
  addForm.targets = ''
  addErrors.value = []
  addDialogVisible.value = true
}

async function handleAddSubmit() {
  const errors = validateTargets(addForm.targets)
  addErrors.value = errors
  if (errors.length) return

  const lines = addForm.targets
    .split('\n')
    .map(s => s.trim())
    .filter(s => s && !s.startsWith('#'))
  if (!lines.length) {
    ElMessage.warning(t('asset.addBatchEmptyTip'))
    return
  }

  addSubmitting.value = true
  try {
    const res = await importAssets({ targets: lines })
    if (res.code === 0) {
      ElMessage.success(res.msg || t('asset.addAssetSuccess'))
      addDialogVisible.value = false
      addForm.targets = ''
      addErrors.value = []
      cardViewRef.value?.refresh()
    } else {
      ElMessage.error(res.msg || t('asset.addAssetFailed'))
    }
  } catch {
    // The request interceptor provides the global notification.
  } finally {
    addSubmitting.value = false
  }
}
</script>

<style lang="scss" scoped>
.asset-space-search {
  padding: 24px;
  background: hsl(var(--background));
  min-height: 100vh;
}

.page-header {
  margin-bottom: 24px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;

  .header-content {
    h1 {
      font-size: 28px;
      font-weight: 600;
      color: hsl(var(--foreground));
      margin: 0 0 8px 0;
    }

    .description {
      color: hsl(var(--muted-foreground));
      font-size: 14px;
      margin: 0;
    }
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;

    .header-search {
      width: 260px;
    }
  }
}

.add-batch-hint {
  margin-bottom: 12px;
  font-size: 13px;
  color: hsl(var(--muted-foreground));
  line-height: 1.6;
}

.add-errors {
  margin-top: 12px;
  max-height: 160px;
  overflow-y: auto;
  padding: 8px 12px;
  border-radius: 6px;
  background: hsl(var(--destructive) / 0.08);
  border: 1px solid hsl(var(--destructive) / 0.3);
  font-size: 12px;
  color: hsl(var(--destructive));
}

.add-error-line {
  line-height: 1.5;
  word-break: break-all;
}
</style>
