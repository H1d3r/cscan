// 任务终态集合，与后端 internal/scheduler/task_transitions.go 的 isTerminalState 保持一致
export const TERMINAL_TASK_STATUSES = ['SUCCESS', 'PARTIAL', 'FAILURE', 'STOPPED', 'REVOKED', 'COMPLETED']

export function isTerminalTaskStatus(status) {
  return TERMINAL_TASK_STATUSES.includes(status)
}

// 结果等同"完成"的终态（PARTIAL 表示有覆盖缺口但任务已跑完，展示按成功处理）
export function isSuccessLikeTaskStatus(status) {
  return status === 'SUCCESS' || status === 'PARTIAL' || status === 'COMPLETED'
}

// 后端 phase 上报使用中文名（worker 报告 "完成"/"端口扫描" 等），统一映射为展示 key
const PHASE_KEY_MAP = {
  '子域名扫描': 'workflowSubdomainScan',
  '端口扫描': 'workflowPortScan',
  '端口识别': 'workflowPortIdentify',
  '指纹识别': 'workflowFingerprint',
  '弱口令扫描': 'workflowWeakpassScan',
  '目录扫描': 'workflowDirScan',
  'JS扫描': 'workflowJSFinder',
  '漏洞扫描': 'workflowVulScan',
  '完成': 'completed'
}

// phaseKey -> i18n key（供无法使用 vue-i18n 的场景外的统一展示）
export function localizeTaskPhase(phase, translate) {
  if (!phase) return phase
  const key = PHASE_KEY_MAP[phase]
  if (!key) return phase
  return translate ? translate(`task.${key}`) : key
}
