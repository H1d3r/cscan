<template>
  <div class="asset-space-search">
    <!-- List View -->
    <template v-if="!selectedTargetId">
      <div class="page-header">
        <div class="header-content">
          <h1>{{ $t('navigation.assetSpaceSearch') }}</h1>
          <p class="description">{{ $t('asset.spaceSearchDescription') }}</p>
        </div>
        <div class="header-actions">
          <el-radio-group v-model="listMode" size="default">
            <el-radio-button value="target">{{ $t('asset.globalView.tabTargetView') }}</el-radio-button>
            <el-radio-button value="global">{{ $t('asset.globalView.tabGlobalView') }}</el-radio-button>
          </el-radio-group>
        </div>
      </div>
      <AssetInventoryCardView
        v-if="listMode === 'target'"
        ref="cardViewRef"
        @create-target="openAddDialog"
        @start-scan="handleStartScan"
        @view-target="handleViewTarget"
        @edit-target="handleEditTarget"
      />
      <GlobalAssetView v-else />
    </template>

    <!-- Detail View -->
    <template v-else>
      <TargetDetailView
        :target-id="selectedTargetId"
        :auto-open-settings="detailAutoSettings"
        @back="handleBack"
        @view-asset="handleViewAsset"
      />
    </template>

    <!-- 手动添加资产 dialog（自旧资产概览页迁入，功能不遗失） -->
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
        <div
          v-for="(e, i) in addErrors"
          :key="i"
          class="add-error-line"
        >
          {{ $t('asset.addBatchLine', { line: e.line, target: e.target, msg: e.message }) }}
        </div>
      </div>
      <template #footer>
        <el-button @click="addDialogVisible = false">
          {{ $t('asset.addAssetCancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="addSubmitting"
          @click="handleAddSubmit"
        >
          {{ $t('asset.addAssetConfirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import AssetInventoryCardView from '@/components/asset/AssetInventoryCardView.vue'
import TargetDetailView from '@/components/asset/TargetDetailView.vue'
import GlobalAssetView from '@/components/asset/GlobalAssetView.vue'
import { importAssets } from '@/api/asset'
import { validateTargets } from '@/utils/target'

const router = useRouter()
const { t } = useI18n()

const selectedTargetId = ref('')
const detailAutoSettings = ref(false)
const cardViewRef = ref(null)
const listMode = ref('target')

function handleViewTarget(targetId) {
  detailAutoSettings.value = false
  selectedTargetId.value = targetId
}

// 目标行画笔：进入详情并直接打开目标设置抽屉（标签/备注/颜色/重发现）
function handleEditTarget(targetId) {
  detailAutoSettings.value = true
  selectedTargetId.value = targetId
}

function handleBack(newTargetId) {
  if (newTargetId && newTargetId !== selectedTargetId.value) {
    detailAutoSettings.value = false
    selectedTargetId.value = newTargetId
  } else {
    selectedTargetId.value = ''
  }
  detailAutoSettings.value = false
}

function handleViewAsset(asset) {
  // 资产详情抽屉已在 TargetDetailView 内部打开，此处仅保留事件出口
  console.log('View asset:', asset)
}

function handleStartScan() {
  router.push('/task/create')
}

// 手动添加资产（批量粘贴，自旧资产概览页迁入）
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
      // 已移除列表自动轮询，手动添加成功后主动刷新目标列表
      cardViewRef.value?.refresh()
    } else {
      ElMessage.error(res.msg || t('asset.addAssetFailed'))
    }
  } catch (e) {
    // axios 拦截器已统一提示
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
    flex-shrink: 0;
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
