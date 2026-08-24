<template>
  <div class="job-status-badge" :class="statusClass">
    <el-icon class="status-icon" :class="{ spinning: status === 'in_progress' }">
      <component :is="config.icon" />
    </el-icon>
    <span class="status-text">{{ label }}</span>
  </div>
</template>

<script setup>
import { computed, markRaw } from 'vue'
import { useI18n } from 'vue-i18n'
import { Clock, Loading, CircleCheckFilled, CircleCloseFilled, RemoveFilled, Minus } from '@element-plus/icons-vue'

const props = defineProps({
  status: { type: String, default: '' },
})

const { t } = useI18n()

// 对齐 open-asm JobStatusBadge：outline 胶囊 + 图标，色板 yellow/purple/green/red/gray
const statusConfigs = {
  unscanned:  { icon: markRaw(Minus),             color: '#9ca3af', labelKey: 'asset.targetView.statusUnscanned' },
  pending:    { icon: markRaw(Clock),             color: '#eab308', labelKey: 'asset.targetView.statusPending' },
  in_progress:{ icon: markRaw(Loading),           color: '#a855f7', labelKey: 'asset.targetView.statusInProgress' },
  completed:  { icon: markRaw(CircleCheckFilled), color: '#22c55e', labelKey: 'asset.targetView.statusCompleted' },
  failed:     { icon: markRaw(CircleCloseFilled), color: '#ef4444', labelKey: 'asset.targetView.statusFailed' },
  cancelled:  { icon: markRaw(RemoveFilled),      color: '#6b7280', labelKey: 'asset.targetView.statusCancelled' },
  skipped:    { icon: markRaw(RemoveFilled),      color: '#9ca3af', labelKey: 'asset.targetView.statusCancelled' },
}

// 空/未知状态归为「未扫描」（如空间引擎导入资产），不再兜底成「等待中」
const config = computed(() => statusConfigs[props.status] || statusConfigs.unscanned)
const statusClass = computed(() => `status-${props.status || 'unscanned'}`)
const label = computed(() => t(config.value.labelKey))
</script>

<style scoped lang="scss">
.job-status-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 12px;
  border-radius: 9999px;
  border: 1px solid var(--el-border-color);
  background: transparent;
  font-size: 13px;
  line-height: 1;
  white-space: nowrap;
  user-select: none;

  .status-icon {
    font-size: 15px;
  }

  .spinning {
    animation: spin 1s linear infinite;
  }
}

.status-unscanned   { color: #9ca3af; border-color: rgba(156, 163, 175, 0.4); }
.status-pending     { color: #eab308; border-color: rgba(234, 179, 8, 0.4); }
.status-in_progress { color: #a855f7; border-color: rgba(168, 85, 247, 0.4); }
.status-completed   { color: #22c55e; border-color: rgba(34, 197, 94, 0.4); }
.status-failed      { color: #ef4444; border-color: rgba(239, 68, 68, 0.4); }
.status-cancelled,
.status-skipped     { color: #6b7280; border-color: rgba(107, 114, 128, 0.4); }

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
