<template>
  <div class="scan-workflow">
    <div class="workflow-title">
      <el-icon><Operation /></el-icon>
      <span>{{ $t('task.scanWorkflow') }}</span>
    </div>
    <div class="workflow-container">
      <!-- 工作流节点 -->
      <div class="workflow-nodes">
        <template v-for="(phase, index) in enabledPhases" :key="phase.key">
          <!-- 节点 -->
          <div 
            class="workflow-node" 
            :class="getNodeClass(phase)"
            @click="toggleExpand(phase.key)"
          >
            <div class="node-icon">
              <el-icon v-if="phase.status === 'success'"><CircleCheck /></el-icon>
              <el-icon v-else-if="phase.status === 'running'" class="is-loading"><Loading /></el-icon>
              <el-icon v-else-if="phase.status === 'failed'"><CircleClose /></el-icon>
              <el-icon v-else-if="phase.status === 'skipped'"><Remove /></el-icon>
              <el-icon v-else><Clock /></el-icon>
            </div>
            <div class="node-content">
              <span class="node-name">{{ phase.name.value }}</span>
              <span v-if="phase.duration" class="node-duration">{{ phase.duration }}</span>
            </div>
          </div>
          <!-- 连接线 -->
          <div v-if="index < enabledPhases.length - 1" class="workflow-connector">
            <div class="connector-line" :class="{ active: getConnectorActive(index) }"></div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleCheck, Loading, CircleClose, Remove, Clock, Operation } from '@element-plus/icons-vue'

const { t } = useI18n()

const props = defineProps({
  config: {
    type: Object,
    default: () => ({})
  },
  currentPhase: {
    type: String,
    default: ''
  },
  status: {
    type: String,
    default: ''
  }
})

// 所有扫描阶段定义
const allPhases = [
  { key: 'domainscan', name: computed(() => t('task.workflowSubdomainScan')), configKey: 'domainscan', progress: 10 },
  { key: 'portscan', name: computed(() => t('task.workflowPortScan')), configKey: 'portscan', progress: 20 },
  { key: 'portidentify', name: computed(() => t('task.workflowPortIdentify')), configKey: 'portidentify', progress: 40 },
  { key: 'fingerprint', name: computed(() => t('task.workflowFingerprint')), configKey: 'fingerprint', progress: 60 },
  { key: 'dirscan', name: computed(() => t('task.workflowDirScan')), configKey: 'dirscan', progress: 70 },
  { key: 'pocscan', name: computed(() => t('task.workflowVulScan')), configKey: 'pocscan', progress: 80 }
]

// 阶段名称映射（后端返回的中文名称 -> key）
const phaseNameMap = {
  '子域名扫描': 'domainscan',
  '端口扫描': 'portscan',
  '端口识别': 'portidentify',
  '指纹识别': 'fingerprint',
  '目录扫描': 'dirscan',
  '漏洞扫描': 'pocscan',
  '完成': 'completed'
}

// 计算启用的阶段（不包含状态）
const enabledPhaseKeys = computed(() => {
  const config = props.config || {}
  const phases = []
  
  allPhases.forEach(phase => {
    const phaseConfig = config[phase.configKey]
    // 端口扫描默认启用（enable !== false）
    const isEnabled = phase.configKey === 'portscan' 
      ? phaseConfig?.enable !== false 
      : phaseConfig?.enable === true
    
    if (isEnabled) {
      phases.push(phase)
    }
  })
  
  return phases
})

// 计算启用的阶段（包含状态）
const enabledPhases = computed(() => {
  return enabledPhaseKeys.value.map(phase => ({
    ...phase,
    status: getPhaseStatus(phase.key),
    duration: null
  }))
})

// 获取阶段状态
function getPhaseStatus(phaseKey) {
  const currentPhaseKey = phaseNameMap[props.currentPhase] || ''
  const taskStatus = props.status
  const phases = enabledPhaseKeys.value
  
  // 任务已完成
  if (['SUCCESS', 'FAILURE', 'STOPPED', 'REVOKED'].includes(taskStatus)) {
    if (taskStatus === 'SUCCESS') {
      return 'success'
    }
    // 失败/停止时，当前阶段之前的都是成功，当前阶段是失败
    const currentIndex = phases.findIndex(p => p.key === currentPhaseKey)
    const phaseIndex = phases.findIndex(p => p.key === phaseKey)
    
    if (currentPhaseKey === 'completed' || phaseIndex < currentIndex) {
      return 'success'
    } else if (phaseIndex === currentIndex) {
      return taskStatus === 'FAILURE' ? 'failed' : 'success'
    }
    return 'pending'
  }
  
  // 任务进行中
  if (['STARTED', 'PENDING'].includes(taskStatus)) {
    const currentIndex = phases.findIndex(p => p.key === currentPhaseKey)
    const phaseIndex = phases.findIndex(p => p.key === phaseKey)
    
    if (phaseIndex < currentIndex) {
      return 'success'
    } else if (phaseIndex === currentIndex) {
      return 'running'
    }
    return 'pending'
  }
  
  return 'pending'
}

// 获取节点样式类
function getNodeClass(phase) {
  return {
    'is-success': phase.status === 'success',
    'is-running': phase.status === 'running',
    'is-failed': phase.status === 'failed',
    'is-skipped': phase.status === 'skipped',
    'is-pending': phase.status === 'pending'
  }
}

// 获取连接线是否激活
function getConnectorActive(index) {
  if (index >= enabledPhaseKeys.value.length - 1) return false
  const phases = enabledPhases.value
  const currentPhase = phases[index]
  return currentPhase?.status === 'success'
}

// 展开/收起
const expandedPhase = ref(null)
function toggleExpand(key) {
  expandedPhase.value = expandedPhase.value === key ? null : key
}
</script>

<style scoped>
.scan-workflow {
  margin-bottom: 20px;
}

.workflow-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  margin-bottom: 16px;
}

.workflow-container {
  background: var(--el-bg-color-page);
  border-radius: 8px;
  padding: 20px;
  overflow-x: auto;
}

.workflow-nodes {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: fit-content;
  gap: 0;
}

.workflow-node {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.3s;
  min-width: 100px;
}

.workflow-node:hover {
  border-color: var(--el-color-primary);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.workflow-node.is-success {
  border-color: var(--el-color-success);
  background: var(--el-fill-color-light);
}

html.dark .workflow-node.is-success {
  background: rgba(103, 194, 58, 0.15);
}

.workflow-node.is-success .node-icon {
  color: var(--el-color-success);
}

.workflow-node.is-running {
  border-color: var(--el-color-primary);
  background: var(--el-fill-color-light);
}

html.dark .workflow-node.is-running {
  background: rgba(64, 158, 255, 0.15);
}

.workflow-node.is-running .node-icon {
  color: var(--el-color-primary);
}

.workflow-node.is-failed {
  border-color: var(--el-color-danger);
  background: var(--el-fill-color-light);
}

html.dark .workflow-node.is-failed {
  background: rgba(245, 108, 108, 0.15);
}

.workflow-node.is-failed .node-icon {
  color: var(--el-color-danger);
}

.workflow-node.is-pending {
  opacity: 0.6;
}

.workflow-node.is-pending .node-icon {
  color: var(--el-text-color-secondary);
}

.node-icon {
  font-size: 18px;
  display: flex;
  align-items: center;
}

.node-icon .is-loading {
  animation: rotating 1.5s linear infinite;
}

@keyframes rotating {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.node-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.node-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}

.node-duration {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.workflow-connector {
  width: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.connector-line {
  width: 100%;
  height: 2px;
  background: var(--el-border-color);
  position: relative;
}

.connector-line.active {
  background: var(--el-color-success);
}

.connector-line::after {
  content: '';
  position: absolute;
  right: -4px;
  top: 50%;
  transform: translateY(-50%);
  border: 4px solid transparent;
  border-left-color: var(--el-border-color);
}

.connector-line.active::after {
  border-left-color: var(--el-color-success);
}
</style>
