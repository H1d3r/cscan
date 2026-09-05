<template>
  <div class="attack-surface-inventory">
    <el-tabs v-model="currentTab" class="inventory-tabs" @tab-click="handleTabClick">
      <el-tab-pane :label="$t('asset.attackSurface.tabs.service')" name="service" lazy>
        <GlobalAssetView
          v-if="currentTab === 'service'"
          :key="contentKey"
          :target-id="targetId"
          :metric-filters="metricFilters"
        />
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.attackSurface.tabs.dir')" name="dir" lazy>
        <DirScanView
          v-if="currentTab === 'dir'"
          :key="contentKey"
          :extra-params="baseExtraParams"
          :sync-url="false"
        />
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.attackSurface.tabs.js')" name="js" lazy>
        <JSFinderView
          v-if="currentTab === 'js'"
          :key="contentKey"
          :extra-params="baseExtraParams"
          :sync-url="false"
        />
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.attackSurface.tabs.sensitive')" name="sensitive" lazy>
        <JSFinderView
          v-if="currentTab === 'sensitive'"
          :key="contentKey"
          :extra-params="sensitiveExtraParams"
          :sync-url="false"
          mode="sensitive"
        />
      </el-tab-pane>

      <el-tab-pane :label="$t('asset.attackSurface.tabs.vuln')" name="vuln" lazy>
        <VulView
          v-if="currentTab === 'vuln'"
          :key="contentKey"
          :extra-params="baseExtraParams"
          :sync-url="false"
        />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import GlobalAssetView from './GlobalAssetView.vue'
import DirScanView from './DirScanView.vue'
import JSFinderView from './JSFinderView.vue'
import VulView from './VulView.vue'

const props = defineProps({
  modelValue: { type: String, required: true },
  targetId: { type: String, default: '' },
  metricFilters: { type: Object, default: () => ({}) },
  resetKey: { type: Number, default: 0 },
})

const emit = defineEmits(['update:modelValue', 'manual-tab-change'])

const currentTab = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value),
})

const baseExtraParams = computed(() => ({
  targetId: props.targetId || undefined,
  ...(props.metricFilters || {}),
}))

const sensitiveExtraParams = computed(() => ({
  targetId: props.targetId || undefined,
  aiResult: 'risk',
  aiStatus: 'completed',
  ...(props.metricFilters || {}),
}))

const filterSignature = computed(() => JSON.stringify(props.metricFilters || {}))
const contentKey = computed(() => `${props.targetId}:${props.modelValue}:${props.resetKey}:${filterSignature.value}`)

function handleTabClick(pane) {
  emit('manual-tab-change', String(pane.paneName || 'service'))
}
</script>

<style scoped lang="scss">
.attack-surface-inventory {
  min-width: 0;
}

.inventory-tabs {
  :deep(.el-tabs__header) {
    margin-bottom: 20px;
  }

  :deep(.el-tabs__item) {
    font-weight: 500;
  }
}
</style>
