<template>
  <div class="target-switcher">
    <el-select
      :model-value="targetId"
      filterable
      remote
      :remote-method="searchTargets"
      :loading="loading"
      :placeholder="$t('asset.targetView.switcherPlaceholder')"
      class="switcher-select"
      popper-class="target-switcher-popper"
      @change="handleChange"
      @visible-change="handleVisible"
    >
      <template #prefix>
        <el-icon class="switcher-icon">
          <Sort />
        </el-icon>
      </template>
      <el-option
        v-for="opt in options"
        :key="opt.id"
        :value="opt.id"
        :label="opt.targetValue"
      >
        <div class="option-row">
          <el-icon v-if="opt.targetType === 'domain'" class="option-icon" color="#3b82f6">
            <MapLocation />
          </el-icon>
          <el-icon v-else class="option-icon" color="#f97316">
            <Monitor />
          </el-icon>
          <span class="option-value">{{ opt.targetValue }}</span>
          <el-icon v-if="opt.id === targetId" class="option-check" color="#67c23a">
            <Check />
          </el-icon>
        </div>
      </el-option>
      <template #empty>
        <div class="empty-tip">{{ $t('asset.targetView.switcherNoMatch') }}</div>
      </template>
    </el-select>
  </div>
</template>

<script setup>
import { onUnmounted, ref } from 'vue'
import { Sort, Check, MapLocation, Monitor } from '@element-plus/icons-vue'
import { getAssetTargetList } from '@/api/asset'

const props = defineProps({
  targetId: { type: String, required: true },
})

const emit = defineEmits(['select'])

const options = ref([])
const loading = ref(false)
let searchRequestSeq = 0

async function searchTargets(query) {
  const seq = ++searchRequestSeq
  loading.value = true
  try {
    const res = await getAssetTargetList({
      page: 1,
      pageSize: 50,
      query: query || undefined,
    })
    if (seq !== searchRequestSeq) return
    // 响应拦截器返回 {code,msg,total,list}（顶层无 data 包裹）
    const payload = res?.data ?? res
    options.value = payload?.list || []
  } catch (err) {
    if (seq !== searchRequestSeq) return
    console.error('[TargetSwitcher] search error:', err)
    options.value = []
  } finally {
    if (seq === searchRequestSeq) loading.value = false
  }
}

function handleVisible(visible) {
  if (visible && options.value.length === 0) {
    searchTargets('')
  }
}

function handleChange(id) {
  if (id && id !== props.targetId) {
    emit('select', id)
  }
}

onUnmounted(() => {
  searchRequestSeq += 1
})
</script>

<style scoped lang="scss">
.target-switcher {
  .switcher-select {
    width: 260px;

    :deep(.el-input__wrapper) {
      font-weight: 600;
      font-size: 16px;
    }
  }

  .switcher-icon {
    color: var(--el-text-color-secondary);
  }
}

.option-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;

  .option-icon {
    flex-shrink: 0;
  }

  .option-value {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .option-check {
    flex-shrink: 0;
  }
}

.empty-tip {
  padding: 12px;
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
</style>
