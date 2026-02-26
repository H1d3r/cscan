<template>
  <div class="task-page">
    <!-- 操作栏 -->
    <el-card class="action-card">
      <el-button type="primary" @click="goToCreateTask">
        <el-icon><Plus /></el-icon>{{ $t('task.newTask') }}
      </el-button>
      <el-button @click="goToTemplateManage">
        <el-icon><Document /></el-icon>{{ $t('task.scanTemplate') }}
      </el-button>
      <el-switch
        v-model="autoRefresh"
        style="margin-left: 20px"
        :active-text="$t('task.autoRefresh')"
        inactive-text=""
        @change="handleAutoRefreshChange"
      />
    </el-card>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <div style="margin-bottom: 15px; display: flex; justify-content: space-between; align-items: center;">
        <div>
          <el-button type="danger" :disabled="selectedRows.length === 0" @click="handleBatchDelete">
            <el-icon><Delete /></el-icon>{{ $t('task.batchDelete') }} ({{ selectedRows.length }})
          </el-button>
        </div>
        <div style="display: flex; gap: 10px;">
          <el-select 
            v-model="filterTags" 
            multiple 
            filterable 
            :placeholder="$t('task.filterByTags')" 
            clearable 
            style="width: 250px"
            @change="loadData"
          >
            <el-option v-for="tag in allTags" :key="tag" :label="tag" :value="tag" />
          </el-select>
        </div>
      </div>
      <el-table :data="tableData" v-loading="loading" stripe max-height="500" @selection-change="handleSelectionChange">
        <el-table-column type="selection" width="50" />
        <el-table-column prop="name" :label="$t('task.taskName')" min-width="150" />
        <el-table-column prop="target" :label="$t('task.scanTarget')" min-width="150" show-overflow-tooltip />
        <el-table-column prop="status" :label="$t('task.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status, row)">{{ getStatusText(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="progress" :label="$t('task.progress')" width="150">
          <template #default="{ row }">
            <div>
              <el-progress :percentage="Math.min(row.progress || 0, 100)" :stroke-width="6" />
              <div v-if="row.subTaskCount > 1" class="sub-task-info">
                {{ $t('task.subTask') }}: {{ row.subTaskDone }}/{{ row.subTaskCount }}
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="createTime" :label="$t('common.createTime')" width="160" />
        <el-table-column prop="startTime" :label="$t('task.startTime')" width="160">
          <template #default="{ row }">
            {{ row.startTime || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="endTime" :label="$t('task.endTime')" width="160">
          <template #default="{ row }">
            {{ row.endTime || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="300" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status === 'CREATED' || !row.status" type="success" link size="small" @click="handleStart(row)">{{ $t('task.start') }}</el-button>
            <el-button v-if="row.status === 'CREATED' || !row.status" type="warning" link size="small" @click="goToEditTask(row)">{{ $t('task.edit') }}</el-button>
            <el-button v-if="['STARTED', 'PENDING'].includes(row.status)" type="warning" link size="small" @click="handlePause(row)">{{ $t('task.pause') }}</el-button>
            <el-button v-if="row.status === 'PAUSED'" type="success" link size="small" @click="handleResume(row)">{{ $t('task.resume') }}</el-button>
            <el-button v-if="['STARTED', 'PAUSED', 'PENDING', 'CREATED', ''].includes(row.status) && row.status !== 'SUCCESS' && row.status !== 'FAILURE' && row.status !== 'STOPPED'" type="danger" link size="small" @click="handleStop(row)">{{ $t('task.stop') }}</el-button>
            <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('task.detail') }}</el-button>
            <el-button type="info" link size="small" @click="showLogs(row)">{{ $t('task.logs') }}</el-button>
            <el-button type="info" link size="small" @click="viewReport(row)">{{ $t('task.report') }}</el-button>
            <el-button v-if="['SUCCESS', 'FAILURE', 'STOPPED'].includes(row.status)" type="warning" link size="small" @click="handleRetry(row)">{{ $t('task.retry') }}</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('task.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :total="pagination.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        class="pagination"
        @size-change="loadData"
        @current-change="loadData"
      />
    </el-card>

    <!-- 任务详情对话框 - 现代化设计 -->
    <el-dialog v-model="detailVisible" :title="$t('task.taskDetail')" width="900px" class="task-detail-dialog" destroy-on-close>
      <!-- 顶部任务概览卡片 -->
      <div class="detail-header">
        <div class="detail-header-main">
          <div class="task-title-row">
            <h3 class="task-title">{{ currentTask.name }}</h3>
            <el-tag :type="getStatusType(currentTask.status, currentTask)" size="large" effect="dark" class="status-tag">
              {{ getStatusText(currentTask) }}
            </el-tag>
          </div>
          <div class="task-target">
            <el-icon><Aim /></el-icon>
            <span class="target-text">{{ currentTask.target }}</span>
          </div>
        </div>
        
        <!-- 进度环形图 -->
        <div class="progress-circle-wrapper">
          <el-progress 
            type="circle" 
            :percentage="Math.min(currentTask.progress || 0, 100)" 
            :width="90"
            :stroke-width="8"
            :color="getProgressColor(currentTask.status)"
          >
            <template #default="{ percentage }">
              <span class="progress-value">{{ percentage }}%</span>
            </template>
          </el-progress>
          <div class="subtask-info">{{ currentTask.subTaskDone || 0 }}/{{ currentTask.subTaskCount || 0 }}</div>
        </div>
      </div>

      <!-- 时间信息卡片 -->
      <div class="time-cards">
        <div class="time-card">
          <el-icon class="time-icon"><Clock /></el-icon>
          <div class="time-content">
            <span class="time-label">{{ $t('common.createTime') }}</span>
            <span class="time-value">{{ currentTask.createTime || '-' }}</span>
          </div>
        </div>
        <div class="time-card">
          <el-icon class="time-icon"><VideoPlay /></el-icon>
          <div class="time-content">
            <span class="time-label">{{ $t('task.startTime') }}</span>
            <span class="time-value">{{ currentTask.startTime || '-' }}</span>
          </div>
        </div>
        <div class="time-card">
          <el-icon class="time-icon"><CircleCheck /></el-icon>
          <div class="time-content">
            <span class="time-label">{{ $t('task.endTime') }}</span>
            <span class="time-value">{{ ['SUCCESS', 'FAILURE', 'STOPPED'].includes(currentTask.status) ? (currentTask.endTime || '-') : '-' }}</span>
          </div>
        </div>
      </div>

      <!-- 扫描工作流 -->
      <ScanWorkflow 
        v-if="parsedConfig"
        :config="parsedConfig"
        :current-phase="currentTask.currentPhase"
        :status="currentTask.status"
      />

      <!-- 执行结果 -->
      <div v-if="currentTask.result" class="result-section">
        <div class="section-title">
          <el-icon><Document /></el-icon>
          <span>{{ $t('task.executionResult') }}</span>
        </div>
        <div class="result-content">{{ currentTask.result }}</div>
      </div>
      
      <!-- 扫描配置概览 -->
      <div v-if="parsedConfig" class="config-section-modern">
        <div class="section-title">
          <el-icon><Setting /></el-icon>
          <span>{{ $t('task.scanConfig') }}</span>
        </div>
        
        <!-- 扫描策略概览卡片 -->
        <div class="strategy-overview">
          <div class="strategy-card">
            <div class="strategy-header">
              <el-icon class="strategy-icon"><Operation /></el-icon>
              <span class="strategy-title">{{ $t('task.scanStrategy') }}</span>
            </div>
            <div class="strategy-stats">
              <div class="stat-item">
                <span class="stat-label">{{ $t('task.enabledModules') }}</span>
                <span class="stat-value">{{ enabledModulesCount }}/6</span>
              </div>
              <div class="stat-item">
                <span class="stat-label">{{ $t('task.taskSplit') }}</span>
                <span class="stat-value">{{ parsedConfig.batchSize === 0 ? $t('task.noSplit') : (parsedConfig.batchSize || 50) }}</span>
              </div>
              <div class="stat-item">
                <span class="stat-label">{{ $t('task.currentPhase') }}</span>
                <span class="stat-value">{{ currentTask.currentPhase || '-' }}</span>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 模块开关状态 -->
        <div class="module-grid">
          <div class="module-card" :class="{ active: parsedConfig.domainscan?.enable }">
            <el-icon class="module-icon"><Connection /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.subdomainScan') }}</span>
              <div class="module-details" v-if="parsedConfig.domainscan?.enable">
                <span class="detail-item">{{ parsedConfig.domainscan?.subfinder !== false ? 'Subfinder' : '' }}</span>
                <span class="detail-item" v-if="parsedConfig.domainscan?.subdomainDictIds?.length">{{ $t('task.dictBrute') }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.domainscan?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.domainscan?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.portscan?.enable !== false }">
            <el-icon class="module-icon"><Monitor /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.portScan') }}</span>
              <div class="module-details" v-if="parsedConfig.portscan?.enable !== false">
                <span class="detail-item">{{ parsedConfig.portscan?.tool || 'naabu' }}</span>
                <span class="detail-item">{{ parsedConfig.portscan?.ports || 'top100' }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.portscan?.enable !== false ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.portscan?.enable !== false ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.portidentify?.enable }">
            <el-icon class="module-icon"><Search /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.portIdentify') }}</span>
              <div class="module-details" v-if="parsedConfig.portidentify?.enable">
                <span class="detail-item">{{ parsedConfig.portidentify?.tool || 'nmap' }}</span>
                <span class="detail-item">{{ parsedConfig.portidentify?.timeout || 30 }}s</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.portidentify?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.portidentify?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.fingerprint?.enable }">
            <el-icon class="module-icon"><Stamp /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.fingerprintScan') }}</span>
              <div class="module-details" v-if="parsedConfig.fingerprint?.enable">
                <span class="detail-item">{{ parsedConfig.fingerprint?.tool === 'httpx' ? 'Httpx' : 'Wappalyzer' }}</span>
                <span class="detail-item" v-if="parsedConfig.fingerprint?.screenshot">{{ $t('task.screenshot') }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.fingerprint?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.fingerprint?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.pocscan?.enable }">
            <el-icon class="module-icon"><WarnTriangleFilled /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.vulScan') }}</span>
              <div class="module-details" v-if="parsedConfig.pocscan?.enable">
                <span class="detail-item">Nuclei</span>
                <span class="detail-item">{{ parsedConfig.pocscan?.severity || 'critical,high,medium' }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.pocscan?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.pocscan?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
          <div class="module-card" :class="{ active: parsedConfig.dirscan?.enable }">
            <el-icon class="module-icon"><FolderOpened /></el-icon>
            <div class="module-info">
              <span class="module-name">{{ $t('task.dirScan') }}</span>
              <div class="module-details" v-if="parsedConfig.dirscan?.enable">
                <span class="detail-item">{{ parsedConfig.dirscan?.threads || 10 }} {{ $t('task.threads') }}</span>
                <span class="detail-item" v-if="parsedConfig.dirscan?.dictIds?.length">{{ parsedConfig.dirscan.dictIds.length }} {{ $t('task.dicts') }}</span>
              </div>
            </div>
            <el-tag :type="parsedConfig.dirscan?.enable ? 'success' : 'info'" size="small" effect="plain">
              {{ parsedConfig.dirscan?.enable ? $t('task.enabled') : $t('task.disabled') }}
            </el-tag>
          </div>
        </div>
        
        <!-- 详细配置 - 折叠面板 -->
        <el-collapse v-model="activeConfigPanels" class="config-collapse">
          <!-- 子域名扫描配置 -->
          <el-collapse-item v-if="parsedConfig.domainscan?.enable" name="domainscan">
            <template #title>
              <div class="collapse-title">
                <el-icon><Connection /></el-icon>
                <span>{{ $t('task.subdomainScan') }}</span>
              </div>
            </template>
            <div class="config-grid">
              <div class="config-item">
                <span class="config-label">{{ $t('task.useSubfinder') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.subfinder !== false ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.timeout') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.timeout || 300 }}{{ $t('task.seconds') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.maxEnumTime') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.maxEnumerationTime || 10 }}{{ $t('task.minutes') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.concurrentThreads') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.threads || 10 }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.dnsResolve') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.resolveDNS ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.removeWildcard') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.removeWildcard ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.concurrent') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.concurrent || 50 }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.rateLimit') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.rateLimit || 0 }} req/s</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.bruteforceEngine') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.bruteforceEngine || 'puredns' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.dictBrute') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan?.subdomainDictIds?.length ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div v-if="parsedConfig.domainscan?.subdomainDictIds?.length" class="config-item">
                <span class="config-label">{{ $t('task.dictCount') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan.subdomainDictIds.length }}</span>
              </div>
              <div v-if="parsedConfig.domainscan?.bandwidth" class="config-item">
                <span class="config-label">{{ $t('task.bandwidth') }}</span>
                <span class="config-value">{{ parsedConfig.domainscan.bandwidth }}</span>
              </div>
            </div>
          </el-collapse-item>
          
          <!-- 端口扫描配置 -->
          <el-collapse-item v-if="parsedConfig.portscan?.enable !== false" name="portscan">
            <template #title>
              <div class="collapse-title">
                <el-icon><Monitor /></el-icon>
                <span>{{ $t('task.portScan') }}</span>
              </div>
            </template>
            <div class="config-grid">
              <div class="config-item">
                <span class="config-label">{{ $t('task.scanTool') }}</span>
                <span class="config-value highlight">{{ parsedConfig.portscan?.tool || 'naabu' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.portRange') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.ports || 'top100' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.scanRate') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.rate || 1000 }} pps</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.portThreshold') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.portThreshold || 100 }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.scanType') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.scanType === 's' ? 'SYN' : 'CONNECT' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.timeout') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.timeout || 60 }}{{ $t('task.seconds') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.skipHostDiscovery') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.skipHostDiscovery ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.excludeCdnWaf') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.excludeCDN ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.retries') }}</span>
                <span class="config-value">{{ parsedConfig.portscan?.retries || 3 }}</span>
              </div>
              <div v-if="parsedConfig.portscan?.excludeHosts" class="config-item full-width">
                <span class="config-label">{{ $t('task.excludeTargets') }}</span>
                <span class="config-value">{{ parsedConfig.portscan.excludeHosts }}</span>
              </div>
            </div>
          </el-collapse-item>
          
          <!-- 端口识别配置 -->
          <el-collapse-item v-if="parsedConfig.portidentify?.enable" name="portidentify">
            <template #title>
              <div class="collapse-title">
                <el-icon><Search /></el-icon>
                <span>{{ $t('task.portIdentify') }}</span>
              </div>
            </template>
            <div class="config-grid">
              <div class="config-item">
                <span class="config-label">{{ $t('task.identifyTool') }}</span>
                <span class="config-value highlight">{{ parsedConfig.portidentify?.tool || 'nmap' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.timeout') }}</span>
                <span class="config-value">{{ parsedConfig.portidentify?.timeout || 30 }}{{ $t('task.seconds') }}</span>
              </div>
              <div v-if="parsedConfig.portidentify?.tool === 'fingerprintx'" class="config-item">
                <span class="config-label">{{ $t('task.concurrent') }}</span>
                <span class="config-value">{{ parsedConfig.portidentify?.concurrency || 10 }}</span>
              </div>
              <div v-if="parsedConfig.portidentify?.tool === 'fingerprintx'" class="config-item">
                <span class="config-label">{{ $t('task.scanUDP') }}</span>
                <span class="config-value">{{ parsedConfig.portidentify?.udp ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div v-if="parsedConfig.portidentify?.tool === 'fingerprintx'" class="config-item">
                <span class="config-label">{{ $t('task.fastMode') }}</span>
                <span class="config-value">{{ parsedConfig.portidentify?.fastMode ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div v-if="parsedConfig.portidentify?.args && parsedConfig.portidentify?.tool === 'nmap'" class="config-item full-width">
                <span class="config-label">{{ $t('task.extraParams') }}</span>
                <span class="config-value code">{{ parsedConfig.portidentify.args }}</span>
              </div>
            </div>
          </el-collapse-item>
          
          <!-- 指纹识别配置 -->
          <el-collapse-item v-if="parsedConfig.fingerprint?.enable" name="fingerprint">
            <template #title>
              <div class="collapse-title">
                <el-icon><Stamp /></el-icon>
                <span>{{ $t('task.fingerprintScan') }}</span>
              </div>
            </template>
            <div class="config-grid">
              <div class="config-item">
                <span class="config-label">{{ $t('task.probeTool') }}</span>
                <el-tag :type="parsedConfig.fingerprint?.tool === 'httpx' ? 'primary' : 'success'" size="small">
                  {{ parsedConfig.fingerprint?.tool === 'httpx' ? 'Httpx' : 'Wappalyzer' }}
                </el-tag>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.iconHash') }}</span>
                <span class="config-value">{{ parsedConfig.fingerprint?.iconHash ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.customFingerprint') }}</span>
                <span class="config-value">{{ parsedConfig.fingerprint?.customEngine ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.screenshot') }}</span>
                <span class="config-value">{{ parsedConfig.fingerprint?.screenshot ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.activeScan') }}</span>
                <span class="config-value">{{ parsedConfig.fingerprint?.activeScan ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.timeout') }}</span>
                <span class="config-value">{{ parsedConfig.fingerprint?.targetTimeout || parsedConfig.fingerprint?.timeout || 30 }}{{ $t('task.seconds') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.concurrent') }}</span>
                <span class="config-value">{{ parsedConfig.fingerprint?.concurrency || 10 }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.filterMode') }}</span>
                <span class="config-value">{{ parsedConfig.fingerprint?.filterMode || 'default' }}</span>
              </div>
            </div>
          </el-collapse-item>
          
          <!-- 漏洞扫描配置 -->
          <el-collapse-item v-if="parsedConfig.pocscan?.enable" name="pocscan">
            <template #title>
              <div class="collapse-title">
                <el-icon><WarnTriangleFilled /></el-icon>
                <span>{{ $t('task.vulScan') }}</span>
              </div>
            </template>
            <div class="config-grid">
              <div class="config-item">
                <span class="config-label">{{ $t('task.scanEngine') }}</span>
                <span class="config-value highlight">Nuclei</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.pocSource') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan?.customPocOnly ? $t('task.customPocOnly') : $t('task.defaultAndCustom') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.autoScanCustomTag') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan?.autoScan ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.autoScanBuiltinMapping') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan?.automaticScan ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.severityLevel') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan?.severity || 'critical,high,medium' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.targetTimeout') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan?.targetTimeout || 600 }}{{ $t('task.seconds') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.concurrent') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan?.concurrency || 25 }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.rateLimit') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan?.rateLimit || 150 }} req/s</span>
              </div>
              <div v-if="parsedConfig.pocscan?.pocTypes?.length" class="config-item full-width">
                <span class="config-label">{{ $t('task.pocTypes') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan.pocTypes.join(', ') }}</span>
              </div>
              <div v-if="parsedConfig.pocscan?.nucleiTemplateIds?.length" class="config-item full-width">
                <span class="config-label">{{ $t('task.specifyNucleiTemplate') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan.nucleiTemplateIds.length }} {{ $t('task.templates') }}</span>
              </div>
              <div v-if="parsedConfig.pocscan?.customPocIds?.length" class="config-item full-width">
                <span class="config-label">{{ $t('task.customPocs') }}</span>
                <span class="config-value">{{ parsedConfig.pocscan.customPocIds.length }} {{ $t('task.pocs') }}</span>
              </div>
            </div>
          </el-collapse-item>
          
          <!-- 目录扫描配置 -->
          <el-collapse-item v-if="parsedConfig.dirscan?.enable" name="dirscan">
            <template #title>
              <div class="collapse-title">
                <el-icon><FolderOpened /></el-icon>
                <span>{{ $t('task.dirScan') }}</span>
              </div>
            </template>
            <div class="config-grid">
              <div class="config-item">
                <span class="config-label">{{ $t('task.scanTool') }}</span>
                <span class="config-value highlight">{{ parsedConfig.dirscan?.tool || 'dirsearch' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.concurrent') }}</span>
                <span class="config-value">{{ parsedConfig.dirscan?.threads || parsedConfig.dirscan?.concurrency || 10 }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.timeout') }}</span>
                <span class="config-value">{{ parsedConfig.dirscan?.timeout || 10 }}{{ $t('task.seconds') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.followRedirect') }}</span>
                <span class="config-value">{{ parsedConfig.dirscan?.followRedirect ? $t('common.yes') : $t('common.no') }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.statusCodeFilter') }}</span>
                <span class="config-value">{{ parsedConfig.dirscan?.statusCodes || '200,301,302,403' }}</span>
              </div>
              <div class="config-item">
                <span class="config-label">{{ $t('task.useDict') }}</span>
                <span class="config-value">{{ parsedConfig.dirscan?.dictIds?.length ? (parsedConfig.dirscan.dictIds.length + ' ' + $t('task.dicts')) : $t('task.defaultDict') }}</span>
              </div>
              <div v-if="parsedConfig.dirscan?.extensions" class="config-item">
                <span class="config-label">{{ $t('task.fileExtensions') }}</span>
                <span class="config-value">{{ parsedConfig.dirscan.extensions }}</span>
              </div>
              <div v-if="parsedConfig.dirscan?.recursiveDepth" class="config-item">
                <span class="config-label">{{ $t('task.recursiveDepth') }}</span>
                <span class="config-value">{{ parsedConfig.dirscan.recursiveDepth }}</span>
              </div>
            </div>
          </el-collapse-item>
        </el-collapse>
      </div>
    </el-dialog>

    <!-- 新建/编辑任务对话框 - Tab页布局 -->
    <el-dialog v-model="dialogVisible" :title="isEdit ? $t('task.editTask') : $t('task.newTask')" width="720px" top="5vh" class="task-dialog">
      <el-tabs v-model="activeTab" class="task-tabs">
        <!-- 基本信息 Tab -->
        <el-tab-pane :label="$t('task.basicInfo')" name="basic">
          <el-form ref="formRef" :model="form" :rules="rules" label-width="100px" class="tab-form">
            <el-form-item :label="$t('task.taskName')" prop="name">
              <el-input v-model="form.name" :placeholder="$t('task.pleaseEnterTaskName')" />
            </el-form-item>
            <el-form-item :label="$t('task.scanTarget')" prop="target">
              <el-input v-model="form.target" type="textarea" :rows="6" :placeholder="$t('task.targetPlaceholder')" />
            </el-form-item>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('task.workspace')">
                  <el-select v-model="form.workspaceId" :placeholder="$t('task.selectWorkspace')" clearable style="width: 100%">
                    <el-option v-for="ws in workspaceStore.workspaces" :key="ws.id" :label="ws.name" :value="ws.id" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('task.organization')">
                  <el-select v-model="form.orgId" :placeholder="$t('task.selectOrganization')" clearable style="width: 100%">
                    <el-option v-for="org in organizations" :key="org.id" :label="org.name" :value="org.id" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item :label="$t('task.specifyWorker')">
              <el-select v-model="form.workers" multiple :placeholder="$t('task.anyWorkerExecute')" clearable style="width: 100%">
                <el-option v-for="w in workers" :key="w.name" :label="`${w.name} (${w.ip})`" :value="w.name" />
              </el-select>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- 子域名扫描 Tab -->
        <el-tab-pane name="domainscan">
          <template #label>
            <span>{{ $t('task.subdomainScan') }} <el-tag v-if="form.domainscanEnable" type="success" size="small" style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
          </template>
          <el-form label-width="120px" class="tab-form">
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.domainscanEnable" />
              <span class="form-hint">{{ $t('task.subdomainEnumHint') }}</span>
            </el-form-item>
            <template v-if="form.domainscanEnable">
              <el-form-item :label="$t('task.useSubfinder')">
                <el-switch v-model="form.domainscanSubfinder" />
                <span class="form-hint">{{ $t('task.subfinderPassiveEnum') }}</span>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item :label="$t('task.timeoutSeconds')">
                    <el-input-number v-model="form.domainscanTimeout" :min="60" :max="3600" style="width:100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.maxEnumTime')">
                    <el-input-number v-model="form.domainscanMaxEnumTime" :min="1" :max="60" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item :label="$t('task.rateLimit')">
                    <el-input-number v-model="form.domainscanRateLimit" :min="0" :max="1000" style="width:100%" />
                    <span class="form-hint">0={{ $t('task.noLimit') }}</span>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item :label="$t('task.scanOptions')">
                <el-checkbox v-model="form.domainscanRemoveWildcard">{{ $t('task.removeWildcardDomain') }}</el-checkbox>
              </el-form-item>
              <el-form-item :label="$t('task.dnsResolve')">
                <el-checkbox v-model="form.domainscanResolveDNS">{{ $t('task.resolveSubdomainDns') }}</el-checkbox>
                <span class="form-hint">{{ $t('task.concurrentByWorker') }}</span>
              </el-form-item>
            </template>
            <el-alert v-if="!form.domainscanEnable" type="info" :closable="false" show-icon>
              <template #title>{{ $t('task.subdomainEnumDesc') }}</template>
            </el-alert>
          </el-form>
        </el-tab-pane>

        <!-- 端口扫描 Tab -->
        <el-tab-pane name="portscan">
          <template #label>
            <span>{{ $t('task.portScan') }} <el-tag v-if="form.portscanEnable" type="success" size="small" style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.portscanEnable" />
            </el-form-item>
            <template v-if="form.portscanEnable">
              <el-form-item :label="$t('task.scanTool')">
                <el-radio-group v-model="form.portscanTool">
                  <el-radio label="naabu">Naabu ({{ $t('task.recommended') }})</el-radio>
                  <el-radio label="masscan" :disabled="!availableTools.masscan">
                    Masscan <span v-if="!availableTools.masscan" class="tool-tip">({{ $t('task.notInstalled') }})</span>
                  </el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item :label="$t('task.portRange')">
                <el-select v-model="form.ports" filterable allow-create default-first-option style="width: 100%">
                  <el-option :label="$t('task.top100Ports')" value="top100" />
                  <el-option :label="$t('task.top1000Ports')" value="top1000" />
                  <el-option :label="'80,443,8080,8443 - ' + $t('task.webCommon')" value="80,443,8080,8443" />
                  <el-option :label="'1-65535 - ' + $t('task.allPorts')" value="1-65535" />
                </el-select>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item :label="$t('task.scanRate')">
                    <el-input-number v-model="form.portscanRate" :min="100" :max="100000" style="width:100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.portThreshold')">
                    <el-input-number v-model="form.portThreshold" :min="0" :max="65535" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item v-if="form.portscanTool === 'naabu'" :label="$t('task.scanType')">
                    <el-radio-group v-model="form.scanType">
                      <el-radio label="c">CONNECT</el-radio>
                      <el-radio label="s">SYN</el-radio>
                    </el-radio-group>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.timeoutSeconds')">
                    <el-input-number v-model="form.portscanTimeout" :min="5" :max="1200" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item :label="$t('task.advancedOptions')">
                <div style="display: block; width: 100%">
                  <el-checkbox v-model="form.skipHostDiscovery">{{ $t('task.skipHostDiscovery') }} (-Pn)</el-checkbox>
                  <span class="form-hint">{{ $t('task.skipHostDiscoveryHint') }}</span>
                </div>
                <div v-if="form.portscanTool === 'naabu'" style="display: block; width: 100%; margin-top: 8px">
                  <el-checkbox v-model="form.excludeCDN">{{ $t('task.excludeCdnWaf') }} (-ec)</el-checkbox>
                  <span class="form-hint">{{ $t('task.excludeCdnHint') }}</span>
                </div>
              </el-form-item>
              <el-form-item :label="$t('task.excludeTargets')">
                <el-input v-model="form.excludeHosts" placeholder="192.168.1.1,10.0.0.0/8" />
                <span class="form-hint">{{ $t('task.excludeTargetsHint') }}</span>
              </el-form-item>
            </template>
          </el-form>
        </el-tab-pane>

        <!-- 端口识别 Tab -->
        <el-tab-pane name="portidentify">
          <template #label>
            <span>{{ $t('task.portIdentify') }} <el-tag v-if="form.portidentifyEnable" type="success" size="small" style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.portidentifyEnable" />
            </el-form-item>
            <template v-if="form.portidentifyEnable">
              <!-- 强制扫描：仅在端口扫描未启用时显示 -->
              <el-form-item v-if="!form.portscanEnable" :label="$t('task.forceScan')">
                <el-switch v-model="form.portidentifyForceScan" />
                <span class="form-hint warning-hint">{{ $t('task.forceScanHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.identifyTool')">
                <el-radio-group v-model="form.portidentifyTool">
                  <el-radio label="nmap">Nmap</el-radio>
                  <el-radio label="fingerprintx">Fingerprintx</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item :label="$t('task.timeoutSeconds')">
                <el-input-number v-model="form.portidentifyTimeout" :min="5" :max="300" />
                <span class="form-hint">{{ $t('task.singleHostTimeout') }}</span>
              </el-form-item>
              <el-form-item v-if="form.portidentifyTool === 'fingerprintx'" :label="$t('task.concurrent')">
                <el-input-number v-model="form.portidentifyConcurrency" :min="1" :max="100" />
              </el-form-item>
              <el-form-item v-if="form.portidentifyTool === 'nmap'" :label="$t('task.nmapParams')">
                <el-input v-model="form.portidentifyArgs" placeholder="-sV --version-intensity 5" />
              </el-form-item>
              <el-form-item v-if="form.portidentifyTool === 'fingerprintx'" :label="$t('task.scanUDP')">
                <el-switch v-model="form.portidentifyUDP" />
              </el-form-item>
              <el-form-item v-if="form.portidentifyTool === 'fingerprintx'" :label="$t('task.fastMode')">
                <el-switch v-model="form.portidentifyFastMode" />
              </el-form-item>
            </template>
            <el-alert v-if="!form.portidentifyEnable" type="info" :closable="false" show-icon>
              <template #title>{{ $t('task.portIdentifyDesc') }}</template>
            </el-alert>
          </el-form>
        </el-tab-pane>

        <!-- 指纹识别 Tab -->
        <el-tab-pane name="fingerprint">
          <template #label>
            <span>{{ $t('task.fingerprintScan') }} <el-tag v-if="form.fingerprintEnable" type="success" size="small" style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.fingerprintEnable" />
            </el-form-item>
            <template v-if="form.fingerprintEnable">
              <!-- 强制扫描：仅在端口扫描和端口识别均未启用时显示 -->
              <el-form-item v-if="!form.portscanEnable && !form.portidentifyEnable" :label="$t('task.forceScan')">
                <el-switch v-model="form.fingerprintForceScan" />
                <span class="form-hint warning-hint">{{ $t('task.forceScanHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.probeTool')">
                <el-radio-group v-model="form.fingerprintTool">
                  <el-radio label="httpx">Httpx ({{ $t('task.recommended') }})</el-radio>
                  <el-radio label="builtin">Wappalyzer ({{ $t('task.builtinEngine') }})</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item :label="$t('task.additionalFeatures')">
                <el-checkbox v-model="form.fingerprintIconHash">{{ $t('task.iconHash') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintCustomEngine">{{ $t('task.customFingerprint') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintScreenshot">{{ $t('task.screenshot') }}</el-checkbox>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item :label="$t('task.timeoutSeconds')">
                    <el-input-number v-model="form.fingerprintTimeout" :min="5" :max="120" style="width:100%" />
                    <span class="form-hint">{{ $t('task.concurrentByWorker') }}</span>
                  </el-form-item>
                </el-col>
              </el-row>
            </template>
          </el-form>
        </el-tab-pane>

        <!-- 漏洞扫描 Tab -->
        <el-tab-pane name="pocscan">
          <template #label>
            <span>{{ $t('task.vulScan') }} <el-tag v-if="form.pocscanEnable" type="success" size="small" style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.pocscanEnable" />
              <span class="form-hint">{{ $t('task.useNucleiEngine') }}</span>
            </el-form-item>
            <template v-if="form.pocscanEnable">
              <!-- 强制扫描：仅在前序阶段均未启用时显示 -->
              <el-form-item v-if="!hasPrePhaseEnabled" :label="$t('task.forceScan')">
                <el-switch v-model="form.pocscanForceScan" />
                <span class="form-hint warning-hint">{{ $t('task.forceScanHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.autoScan')">
                <el-checkbox v-model="form.pocscanAutoScan" :disabled="form.pocscanCustomOnly">{{ $t('task.customTagMapping') }}</el-checkbox>
                <el-checkbox v-model="form.pocscanAutomaticScan" :disabled="form.pocscanCustomOnly">{{ $t('task.webFingerprintAutoMatch') }}</el-checkbox>
              </el-form-item>
              <el-form-item :label="$t('task.customPoc')">
                <el-checkbox v-model="form.pocscanCustomOnly">{{ $t('task.onlyUseCustomPoc') }}</el-checkbox>
              </el-form-item>
              <el-form-item :label="$t('task.severityLevel')">
                <el-checkbox-group v-model="form.pocscanSeverity">
                  <el-checkbox label="critical">Critical</el-checkbox>
                  <el-checkbox label="high">High</el-checkbox>
                  <el-checkbox label="medium">Medium</el-checkbox>
                  <el-checkbox label="low">Low</el-checkbox>
                  <el-checkbox label="info">Info</el-checkbox>
                </el-checkbox-group>
              </el-form-item>
              <el-form-item :label="$t('task.targetTimeout')">
                <el-input-number v-model="form.pocscanTargetTimeout" :min="30" :max="600" />
                <span class="form-hint">{{ $t('task.seconds') }}</span>
              </el-form-item>
            </template>
          </el-form>
        </el-tab-pane>

        <!-- 高级设置 Tab -->
        <el-tab-pane :label="$t('task.advancedSettings')" name="advanced">
          <el-form label-width="100px" class="tab-form">
            <el-form-item :label="$t('task.taskSplit')">
              <el-input-number v-model="form.batchSize" :min="0" :max="1000" :step="10" />
              <span class="form-hint">{{ $t('task.batchTargetCount') }}</span>
            </el-form-item>
          </el-form>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <div class="dialog-footer">
          <el-button @click="dialogVisible = false">{{ $t('common.cancel') }}</el-button>
          <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ isEdit ? $t('common.save') : $t('task.createTask') }}</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 任务日志对话框 -->
    <el-dialog v-model="logDialogVisible" :title="$t('task.taskLog')" width="1000px" @close="closeLogDialog">
      <div class="log-progress" v-if="currentLogTask">
        <div class="progress-info">
          <span class="task-name">{{ currentLogTask.name }}</span>
          <el-tag :type="getStatusType(currentLogTask.status, currentLogTask)" size="small">{{ getStatusText(currentLogTask) }}</el-tag>
        </div>
        <el-progress :percentage="Math.min(currentLogTask.progress || 0, 100)" :status="currentLogTask.status === 'SUCCESS' ? 'success' : (currentLogTask.status === 'FAILURE' ? 'exception' : '')" :stroke-width="12" />
      </div>
      <div class="log-filter">
        <el-input v-model="logSearchKeyword" :placeholder="$t('task.searchLogs')" clearable size="small" style="width: 180px; margin-right: 10px">
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
        <el-select v-model="logWorkerFilter" :placeholder="$t('task.filterWorker')" clearable size="small" style="width: 150px">
          <el-option :label="$t('task.allWorkers')" value="" />
          <el-option v-for="w in logWorkers" :key="w" :label="w" :value="w" />
        </el-select>
        <el-select v-model="logLevelFilter" :placeholder="$t('task.filterLevel')" clearable size="small" style="width: 120px; margin-left: 10px">
          <el-option :label="$t('task.allLevels')" value="" />
          <el-option label="DEBUG" value="DEBUG" />
          <el-option label="INFO" value="INFO" />
          <el-option label="WARN" value="WARN" />
          <el-option label="ERROR" value="ERROR" />
        </el-select>
        <el-switch v-model="logAutoRefresh" size="small" :active-text="$t('task.autoRefreshLogs')" style="margin-left: 15px" @change="handleLogAutoRefreshChange" />
        <span class="log-stats">{{ $t('task.totalLogs', { count: filteredLogs.length }) }}</span>
      </div>
      <div class="log-container" ref="logContainerRef">
        <div v-if="filteredLogs.length === 0" class="log-empty">{{ $t('task.noLogs') }}</div>
        <div v-for="(log, index) in filteredLogs" :key="index" class="log-entry" :class="'log-' + log.level.toLowerCase()">
          <span class="log-time">{{ formatLogTime(log.timestamp) }}</span>
          <span class="log-level">[{{ log.level }}]</span>
          <span class="log-worker">{{ log.workerName }}</span>
          <span class="log-message">{{ log.displayMessage }}</span>
        </div>
      </div>
      <template #footer>
        <el-button @click="closeLogDialog">{{ $t('common.close') }}</el-button>
        <el-button type="primary" @click="refreshLogs">{{ $t('common.refresh') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Search, Clock, VideoPlay, CircleCheck, Document, Setting, Connection, Monitor, Stamp, WarnTriangleFilled, FolderOpened, Grid, Aim, Operation } from '@element-plus/icons-vue'
import ScanWorkflow from '@/components/ScanWorkflow.vue'
import { getTaskList, createTask, deleteTask, batchDeleteTask, retryTask, startTask, pauseTask, resumeTask, stopTask, updateTask, getTaskLogs, getWorkerList, saveScanConfig, getScanConfig } from '@/api/task'
import { useWorkspaceStore } from '@/stores/workspace'
import { validateTargets, formatValidationErrors } from '@/utils/target'
import request from '@/api/request'

const router = useRouter()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const detailVisible = ref(false)
const activeConfigPanels = ref([]) // 折叠面板展开状态
const logDialogVisible = ref(false)
const tableData = ref([])
const organizations = ref([])
const workers = ref([])
const allTags = ref([]) // 所有标签列表
const filterTags = ref([]) // 过滤标签
const formRef = ref()
const logContainerRef = ref()
const currentTask = ref({})
const selectedRows = ref([])
const autoRefresh = ref(true)
const activeTab = ref('basic')
const isEdit = ref(false)
const taskLogs = ref([])
const currentLogTaskId = ref('')
const currentLogTask = ref(null)
const logIdSet = new Set()
const logWorkerFilter = ref('')
const logLevelFilter = ref('')
const logSearchKeyword = ref('')
const logAutoRefresh = ref(true)
let refreshTimer = null
let logEventSource = null
let logPollingTimer = null

const pagination = reactive({ page: 1, pageSize: 20, total: 0 })

const form = reactive({
  id: '',
  name: '',
  target: '',
  workspaceId: '',
  orgId: '',
  workers: [],
  batchSize: 50,
  // 子域名扫描
  domainscanEnable: false,
  domainscanSubfinder: true,
  domainscanTimeout: 300,
  domainscanMaxEnumTime: 10,
  domainscanThreads: 10,
  domainscanRateLimit: 0,
  domainscanRemoveWildcard: true,
  domainscanResolveDNS: true,
  domainscanConcurrent: 50,
  // 端口扫描
  portscanEnable: true,
  portscanTool: 'naabu',
  portscanRate: 1000,
  ports: 'top100',
  portThreshold: 100,
  scanType: 'c',
  portscanTimeout: 60,
  skipHostDiscovery: false,
  excludeCDN: false,
  excludeHosts: '',
  portidentifyEnable: false,
  portidentifyTool: 'nmap',
  portidentifyTimeout: 30,
  portidentifyConcurrency: 10,
  portidentifyArgs: '',
  portidentifyUDP: false,
  portidentifyFastMode: false,
  portidentifyForceScan: false,
  portidentifyTimeout: 30,
  portidentifyArgs: '',
  fingerprintEnable: true,
  fingerprintTool: 'httpx',
  fingerprintIconHash: true,
  fingerprintCustomEngine: false,
  fingerprintScreenshot: false,
  fingerprintForceScan: false,
  fingerprintTimeout: 30,
  pocscanEnable: false,
  pocscanAutoScan: true,
  pocscanAutomaticScan: true,
  pocscanCustomOnly: false,
  pocscanSeverity: ['critical', 'high', 'medium'],
  pocscanTargetTimeout: 600,
  pocscanForceScan: false
})

const targetValidator = (rule, value, callback) => {
  if (!value) { callback(new Error(t('task.pleaseEnterTarget'))); return }
  const errors = validateTargets(value)
  errors.length > 0 ? callback(new Error(formatValidationErrors(errors))) : callback()
}

const rules = {
  name: [{ required: true, message: t('task.pleaseEnterTaskName'), trigger: 'blur' }],
  target: [{ required: true, message: t('task.pleaseEnterTarget'), trigger: 'blur' }, { validator: targetValidator, trigger: 'blur' }]
}

// 判断是否有前序扫描阶段启用（用于控制强制扫描开关的显隐）
const hasPrePhaseEnabled = computed(() => {
  return form.domainscanEnable || form.portscanEnable ||
         form.portidentifyEnable || form.fingerprintEnable
})

const logWorkers = computed(() => {
  const set = new Set()
  taskLogs.value.forEach(log => { if (log.workerName) set.add(log.workerName) })
  return Array.from(set).sort()
})

const filteredLogs = computed(() => {
  const keyword = logSearchKeyword.value.toLowerCase()
  return taskLogs.value.filter(log => {
    if (logWorkerFilter.value && log.workerName !== logWorkerFilter.value) return false
    if (logLevelFilter.value && log.level !== logLevelFilter.value) return false
    if (keyword) {
      const msg = (log.displayMessage || log.message || '').toLowerCase()
      if (!msg.includes(keyword) && !(log.level || '').toLowerCase().includes(keyword)) return false
    }
    return true
  })
})

const availableTools = computed(() => {
  const tools = { nmap: false, masscan: false }
  for (const w of workers.value) {
    if (w.tools) {
      if (w.tools.nmap) tools.nmap = true
      if (w.tools.masscan) tools.masscan = true
    }
  }
  return tools
})

// 监听工具可用性，自动关闭不可用的功能
watch(availableTools, (tools) => {
  if (!tools.nmap && form.portidentifyEnable) {
    form.portidentifyEnable = false
  }
  if (!tools.masscan && form.portscanTool === 'masscan') {
    form.portscanTool = 'naabu'
  }
}, { immediate: true })

function formatLogTime(timestamp) {
  if (!timestamp) return ''
  const match = timestamp.match(/(\d{2}:\d{2}:\d{2})/)
  return match ? match[1] : timestamp
}

function parseLogMessage(log) {
  let message = log.message || '', subTask = 'main'
  const subMatch = message.match(/^\[Sub-(\d+)\]\s*/)
  if (subMatch) { subTask = subMatch[1]; message = message.replace(subMatch[0], '') }
  return { ...log, subTask, displayMessage: message }
}

onMounted(() => {
  loadData()
  loadOrganizations()
  loadWorkers()
  if (autoRefresh.value) startAutoRefresh()
  window.addEventListener('workspace-changed', () => { pagination.page = 1; loadData() })
})

onUnmounted(() => {
  stopAutoRefresh()
  if (logEventSource) { logEventSource.close(); logEventSource = null }
})

function handleAutoRefreshChange(val) { val ? startAutoRefresh() : stopAutoRefresh() }
function startAutoRefresh() { stopAutoRefresh(); refreshTimer = setInterval(() => loadData(), 30000) }
function stopAutoRefresh() { if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null } }

async function loadData() {
  loading.value = true
  try {
    const params = { 
      page: pagination.page, 
      pageSize: pagination.pageSize, 
      workspaceId: workspaceStore.currentWorkspaceId || '' 
    }
    if (filterTags.value && filterTags.value.length > 0) {
      params.tags = filterTags.value
    }
    const res = await getTaskList(params)
    if (res.code === 0) { 
      tableData.value = res.list || []
      pagination.total = res.total 
      // 收集所有标签
      const tagSet = new Set()
      res.list.forEach(task => {
        if (task.tags && Array.isArray(task.tags)) {
          task.tags.forEach(tag => tagSet.add(tag))
        }
      })
      allTags.value = Array.from(tagSet)
    }
  } finally { loading.value = false }
}

async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    const data = res.data || res
    if (data.code === 0) organizations.value = (data.list || []).filter(org => org.status === 'enable')
  } catch (e) { console.error('Failed to load organizations:', e) }
}

async function loadWorkers() {
  try {
    const res = await getWorkerList()
    const data = res.data || res
    if (data.code === 0) workers.value = (data.list || []).filter(w => w.status === 'running')
  } catch (e) { console.error('Failed to load workers:', e) }
}

function getStatusType(status, row) {
  const map = { CREATED: 'info', PENDING: 'warning', STARTED: 'primary', PAUSED: 'warning', SUCCESS: 'success', FAILURE: 'danger', STOPPED: 'info', REVOKED: 'info' }
  
  // 如果有状态值，直接返回映射
  if (status && map[status]) {
    return map[status]
  }
  
  // 如果状态为空，根据进度推断状态类型
  if (!status && row) {
    if (row.progress >= 100 || (row.subTaskCount > 0 && row.subTaskDone >= row.subTaskCount)) {
      return 'success'
    }
    if (row.progress > 0 || row.subTaskDone > 0) {
      return 'primary'
    }
    return 'info'
  }
  
  return 'info'
}

// 获取进度环颜色
function getProgressColor(status) {
  // 使用 CSS 变量，通过 getComputedStyle 获取
  const root = document.documentElement
  const getVar = (name) => getComputedStyle(root).getPropertyValue(name).trim()
  
  const colorMap = {
    CREATED: getVar('--status-info') || '#909399',
    PENDING: getVar('--status-warning') || '#E6A23C',
    STARTED: getVar('--status-primary') || '#409EFF',
    PAUSED: getVar('--status-warning') || '#E6A23C',
    SUCCESS: getVar('--status-success') || '#67C23A',
    FAILURE: getVar('--status-danger') || '#F56C6C',
    STOPPED: getVar('--status-info') || '#909399',
    REVOKED: getVar('--status-info') || '#909399'
  }
  return colorMap[status] || getVar('--status-primary') || '#409EFF'
}

// 获取状态显示文本（简化状态显示，不按扫描模块显示）
function getStatusText(row) {
  const statusMap = {
    CREATED: t('task.created'),
    PENDING: t('task.pendingExec'),
    STARTED: t('task.executing'),
    PAUSED: t('task.paused'),
    SUCCESS: t('task.completed'),
    FAILURE: t('task.execFailed'),
    STOPPED: t('task.stopped'),
    REVOKED: t('task.revoked')
  }
  
  // 如果有状态值，直接返回映射
  if (row?.status && statusMap[row.status]) {
    return statusMap[row.status]
  }
  
  // 如果状态为空，根据进度推断状态
  if (!row?.status) {
    if (row?.progress >= 100 || (row?.subTaskCount > 0 && row?.subTaskDone >= row?.subTaskCount)) {
      return t('task.completed')
    }
    if (row?.progress > 0 || row?.subTaskDone > 0) {
      return t('task.executing')
    }
    return t('task.created')
  }
  
  return row?.status || t('task.unknown')
}

// 解析任务配置
const parsedConfig = computed(() => {
  if (!currentTask.value?.config) return null
  try {
    return JSON.parse(currentTask.value.config)
  } catch (e) {
    return null
  }
})

// 计算启用的模块数量
const enabledModulesCount = computed(() => {
  if (!parsedConfig.value) return 0
  let count = 0
  if (parsedConfig.value.domainscan?.enable) count++
  if (parsedConfig.value.portscan?.enable !== false) count++
  if (parsedConfig.value.portidentify?.enable) count++
  if (parsedConfig.value.fingerprint?.enable) count++
  if (parsedConfig.value.pocscan?.enable) count++
  if (parsedConfig.value.dirscan?.enable) count++
  return count
})

function resetForm() {
  Object.assign(form, {
    id: '', name: '', target: '', workspaceId: '', orgId: '', workers: [],
    batchSize: 50,
    // 子域名扫描
    domainscanEnable: false, domainscanSubfinder: true, domainscanTimeout: 300, domainscanMaxEnumTime: 10,
    domainscanThreads: 10, domainscanRateLimit: 0,
    domainscanRemoveWildcard: true, domainscanResolveDNS: true, domainscanConcurrent: 50,
    // 端口扫描
    portscanEnable: true, portscanTool: 'naabu', portscanRate: 1000, ports: 'top100',
    portThreshold: 100, scanType: 'c', portscanTimeout: 60, skipHostDiscovery: false, portidentifyEnable: false, portidentifyTimeout: 30,
    portidentifyArgs: '', fingerprintEnable: true, fingerprintTool: 'httpx', fingerprintIconHash: true,
    fingerprintCustomEngine: false, fingerprintScreenshot: false,
    fingerprintTimeout: 30, pocscanEnable: false, pocscanAutoScan: true,
    pocscanAutomaticScan: true, pocscanCustomOnly: false, pocscanSeverity: ['critical', 'high', 'medium'],
    pocscanTargetTimeout: 600
  })
}

// 跳转到新建任务页面
function goToCreateTask() {
  router.push('/task/create')
}

// 跳转到模板管理页面
function goToTemplateManage() {
  router.push('/task/template')
}

// 跳转到编辑任务页面
function goToEditTask(row) {
  router.push({ path: '/task/create', query: { id: row.id } })
}

async function showCreateDialog() {
  loadWorkers()
  isEdit.value = false
  resetForm()
  // 加载用户上次保存的扫描配置
  try {
    const res = await getScanConfig()
    if (res.code === 0 && res.config) {
      const config = JSON.parse(res.config)
      applyConfig(config)
    }
  } catch (e) { console.error('加载扫描配置失败:', e) }
  let wsId = workspaceStore.currentWorkspaceId
  if (wsId === 'all' || !wsId) {
    const defaultWs = workspaceStore.workspaces.find(ws => ws.name === '默认工作空间')
    wsId = defaultWs ? defaultWs.id : (workspaceStore.workspaces.length > 0 ? workspaceStore.workspaces[0].id : '')
  }
  form.workspaceId = wsId
  activeTab.value = 'basic'
  dialogVisible.value = true
}

// 应用配置到表单
function applyConfig(config) {
  Object.assign(form, {
    batchSize: config.batchSize || 50,
    // 子域名扫描
    domainscanEnable: config.domainscan?.enable ?? false,
    domainscanSubfinder: config.domainscan?.subfinder ?? true,
    domainscanTimeout: config.domainscan?.timeout || 300,
    domainscanMaxEnumTime: config.domainscan?.maxEnumerationTime || 10,
    domainscanThreads: config.domainscan?.threads || 10,
    domainscanRateLimit: config.domainscan?.rateLimit || 0,
    domainscanAll: config.domainscan?.all ?? false,
    domainscanRecursive: config.domainscan?.recursive ?? false,
    domainscanRemoveWildcard: config.domainscan?.removeWildcard ?? true,
    domainscanResolveDNS: config.domainscan?.resolveDNS ?? true,
    domainscanConcurrent: config.domainscan?.concurrent || 50,
    // 端口扫描
    portscanEnable: config.portscan?.enable ?? true,
    portscanTool: config.portscan?.tool || 'naabu',
    portscanRate: config.portscan?.rate || 1000,
    ports: config.portscan?.ports || 'top100',
    portThreshold: config.portscan?.portThreshold || 100,
    scanType: config.portscan?.scanType || 'c',
    portscanTimeout: config.portscan?.timeout || 60,
    skipHostDiscovery: config.portscan?.skipHostDiscovery ?? false,
    excludeCDN: config.portscan?.excludeCDN ?? false,
    excludeHosts: config.portscan?.excludeHosts || '',
    portidentifyEnable: config.portidentify?.enable ?? false,
    portidentifyTool: config.portidentify?.tool || 'nmap',
    portidentifyTimeout: config.portidentify?.timeout || 30,
    portidentifyConcurrency: config.portidentify?.concurrency || 10,
    portidentifyArgs: config.portidentify?.args || '',
    portidentifyUDP: config.portidentify?.udp ?? false,
    portidentifyFastMode: config.portidentify?.fastMode ?? false,
    fingerprintEnable: config.fingerprint?.enable ?? true,
    fingerprintTool: config.fingerprint?.tool || (config.fingerprint?.httpx ? 'httpx' : 'builtin'),
    fingerprintIconHash: config.fingerprint?.iconHash ?? true,
    fingerprintCustomEngine: config.fingerprint?.customEngine ?? false,
    fingerprintScreenshot: config.fingerprint?.screenshot ?? false,
    fingerprintTimeout: config.fingerprint?.targetTimeout || 30,
    pocscanEnable: config.pocscan?.enable ?? false,
    pocscanAutoScan: config.pocscan?.autoScan ?? true,
    pocscanAutomaticScan: config.pocscan?.automaticScan ?? true,
    pocscanCustomOnly: config.pocscan?.customPocOnly ?? false,
    pocscanSeverity: config.pocscan?.severity ? config.pocscan.severity.split(',') : ['critical', 'high', 'medium'],
    pocscanTargetTimeout: config.pocscan?.targetTimeout || 600
  })
}

function showDetail(row) { currentTask.value = row; detailVisible.value = true }

function handleEdit(row) {
  loadWorkers()
  isEdit.value = true
  resetForm()
  Object.assign(form, { id: row.id, name: row.name, target: row.target, workspaceId: row.workspaceId || '' })
  // 解析已保存的配置
  if (row.config) {
    try {
      const config = JSON.parse(row.config)
      applyConfig(config)
    } catch (e) { console.error('Parse config error:', e) }
  }
  activeTab.value = 'basic'
  dialogVisible.value = true
}

function buildConfig() {
  return {
    batchSize: form.batchSize,
    domainscan: { enable: form.domainscanEnable, subfinder: form.domainscanSubfinder, timeout: form.domainscanTimeout, maxEnumerationTime: form.domainscanMaxEnumTime, threads: form.domainscanThreads, rateLimit: form.domainscanRateLimit, all: form.domainscanAll, recursive: form.domainscanRecursive, removeWildcard: form.domainscanRemoveWildcard, resolveDNS: form.domainscanResolveDNS, concurrent: form.domainscanConcurrent },
    portscan: { enable: form.portscanEnable, tool: form.portscanTool, rate: form.portscanRate, ports: form.ports, portThreshold: form.portThreshold, scanType: form.scanType, timeout: form.portscanTimeout, skipHostDiscovery: form.skipHostDiscovery, excludeCDN: form.excludeCDN, excludeHosts: form.excludeHosts },
    portidentify: { enable: form.portidentifyEnable, tool: form.portidentifyTool, timeout: form.portidentifyTimeout, concurrency: form.portidentifyConcurrency, args: form.portidentifyArgs, udp: form.portidentifyUDP, fastMode: form.portidentifyFastMode, forceScan: form.portidentifyForceScan && !form.portscanEnable },
    fingerprint: { enable: form.fingerprintEnable, tool: form.fingerprintTool, iconHash: form.fingerprintIconHash, customEngine: form.fingerprintCustomEngine, screenshot: form.fingerprintScreenshot, targetTimeout: form.fingerprintTimeout, forceScan: form.fingerprintForceScan && !form.portscanEnable && !form.portidentifyEnable },
    pocscan: { enable: form.pocscanEnable, useNuclei: true, forceScan: form.pocscanForceScan && !hasPrePhaseEnabled.value, autoScan: form.pocscanAutoScan, automaticScan: form.pocscanAutomaticScan, customPocOnly: form.pocscanCustomOnly, severity: form.pocscanSeverity.join(','), targetTimeout: form.pocscanTargetTimeout }
  }
}

// 扫描配置字段列表（用于监听变化自动保存）
const scanConfigFields = [
  'batchSize',
  'domainscanEnable', 'domainscanSubfinder', 'domainscanTimeout', 'domainscanMaxEnumTime', 'domainscanThreads', 'domainscanRateLimit', 'domainscanAll', 'domainscanRecursive', 'domainscanRemoveWildcard', 'domainscanResolveDNS', 'domainscanConcurrent',
  'portscanEnable', 'portscanTool', 'portscanRate', 'ports', 'portThreshold', 'scanType', 'portscanTimeout', 'skipHostDiscovery', 'excludeCDN', 'excludeHosts',
  'portidentifyEnable', 'portidentifyTool', 'portidentifyTimeout', 'portidentifyConcurrency', 'portidentifyArgs', 'portidentifyUDP', 'portidentifyFastMode', 'portidentifyForceScan',
  'fingerprintEnable', 'fingerprintTool', 'fingerprintIconHash', 'fingerprintCustomEngine', 'fingerprintScreenshot', 'fingerprintTimeout', 'fingerprintForceScan',
  'pocscanEnable', 'pocscanAutoScan', 'pocscanAutomaticScan', 'pocscanCustomOnly', 'pocscanSeverity', 'pocscanTargetTimeout', 'pocscanForceScan'
]

// 防抖保存配置
let saveConfigTimer = null
function debounceSaveConfig() {
  if (saveConfigTimer) clearTimeout(saveConfigTimer)
  saveConfigTimer = setTimeout(() => {
    const config = buildConfig()
    saveScanConfig({ config: JSON.stringify(config) }).catch(e => console.error('自动保存配置失败:', e))
  }, 500)
}

// 监听扫描配置变化，自动保存（仅在新建任务对话框打开且非编辑模式时）
watch(
  () => scanConfigFields.map(f => form[f]),
  () => {
    if (dialogVisible.value && !isEdit.value) {
      debounceSaveConfig()
    }
  },
  { deep: true }
)

async function handleSubmit() {
  await formRef.value.validate()
  submitting.value = true
  try {
    const config = buildConfig()
    const configStr = JSON.stringify(config)
    const data = { name: form.name, target: form.target, workspaceId: form.workspaceId, orgId: form.orgId, workers: form.workers, config: configStr }
    let res
    if (isEdit.value) {
      res = await updateTask({ id: form.id, ...data })
    } else {
      res = await createTask(data)
    }
    if (res.code === 0) {
      ElMessage.success(isEdit.value ? t('task.taskUpdateSuccess') : t('task.taskCreateSuccess'))
      dialogVisible.value = false
      loadData()
    } else { ElMessage.error(res.msg) }
  } finally { submitting.value = false }
}

async function handleDelete(row) {
  await ElMessageBox.confirm(t('task.confirmDeleteTask'), t('common.tip'), { type: 'warning' })
  const res = await deleteTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success(t('task.deleteSuccess')), loadData()) : ElMessage.error(res.msg)
}

function handleSelectionChange(rows) { selectedRows.value = rows }

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  // 检查是否所有选中的任务都在同一个工作空间
  const workspaceIds = [...new Set(selectedRows.value.map(row => row.workspaceId))]
  if (workspaceIds.length > 1) {
    ElMessage.warning(t('task.batchDeleteSameWorkspace'))
    return
  }
  await ElMessageBox.confirm(t('task.confirmBatchDelete', { count: selectedRows.value.length }), t('common.tip'), { type: 'warning' })
  const res = await batchDeleteTask({ ids: selectedRows.value.map(row => row.id), workspaceId: workspaceIds[0] })
  res.code === 0 ? (ElMessage.success(t('task.deleteSuccess')), selectedRows.value = [], loadData()) : ElMessage.error(res.msg)
}

async function handleRetry(row) {
  await ElMessageBox.confirm(t('task.confirmRetry'), t('common.tip'), { type: 'warning' })
  const res = await retryTask({ id: row.id })
  if (res.code === 0) {
    ElMessage.success(res.msg || t('task.newTaskCreated'))
    loadData()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleStart(row) {
  const res = await startTask({ id: row.id, workspaceId: row.workspaceId })
  if (res.code === 0) {
    ElMessage.success(t('task.taskStarted'))
    loadData()
    // 延迟再刷新一次，等待 Worker 拉取任务后状态更新
    setTimeout(() => loadData(), 2000)
  } else {
    ElMessage.error(res.msg)
  }
}

async function handlePause(row) {
  await ElMessageBox.confirm(t('task.confirmPause'), t('common.tip'), { type: 'warning' })
  const res = await pauseTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success(t('task.taskPaused')), loadData()) : ElMessage.error(res.msg)
}

async function handleResume(row) {
  const res = await resumeTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success(t('task.taskResumed')), loadData()) : ElMessage.error(res.msg)
}

async function handleStop(row) {
  await ElMessageBox.confirm(t('task.confirmStop'), t('common.tip'), { type: 'warning' })
  const res = await stopTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success(t('task.taskStopped')), loadData()) : ElMessage.error(res.msg)
}

function viewReport(row) { router.push({ path: '/report', query: { taskId: row.id } }) }

async function showLogs(row) {
  currentLogTaskId.value = row.taskId
  currentLogTask.value = { ...row }
  taskLogs.value = []
  logIdSet.clear()
  logDialogVisible.value = true
  await refreshLogs()
  if (logAutoRefresh.value) { connectLogStream(); startLogPolling() }
}

async function refreshLogs() {
  if (!currentLogTaskId.value) return
  try {
    const task = tableData.value.find(t => t.id === currentLogTask.value?.id)
    if (task) currentLogTask.value = { ...task }
    const res = await getTaskLogs({ taskId: currentLogTaskId.value, limit: 500 })
    if (res.code === 0) {
      for (const log of (res.list || [])) {
        const logId = (log.timestamp || '') + (log.message || '')
        if (!logIdSet.has(logId)) { logIdSet.add(logId); taskLogs.value.push(parseLogMessage(log)) }
      }
      taskLogs.value.sort((a, b) => (a.timestamp || '').localeCompare(b.timestamp || ''))
      scrollToBottom()
    }
  } catch (err) { console.error('Failed to load task logs:', err) }
}

function startLogPolling() {
  if (logPollingTimer || !logAutoRefresh.value) return
  logPollingTimer = setInterval(async () => {
    if (logDialogVisible.value && currentLogTaskId.value && logAutoRefresh.value) { await loadData(); await refreshLogs() }
  }, 2000)
}

function handleLogAutoRefreshChange(val) {
  if (val) { startLogPolling(); connectLogStream() }
  else { stopLogPolling(); if (logEventSource) { logEventSource.close(); logEventSource = null } }
}

function scrollToBottom() {
  setTimeout(() => { if (logContainerRef.value) logContainerRef.value.scrollTop = logContainerRef.value.scrollHeight }, 100)
}

function connectLogStream() {
  if (logEventSource) { logEventSource.close(); logEventSource = null }
  if (!currentLogTaskId.value) return
  const token = localStorage.getItem('token')
  const baseUrl = import.meta.env.VITE_API_BASE_URL || ''
  logEventSource = new EventSource(`${baseUrl}/api/v1/task/logs/stream?taskId=${currentLogTaskId.value}&token=${token}`)
  logEventSource.onmessage = (event) => {
    try {
      const log = JSON.parse(event.data)
      const logId = (log.timestamp || '') + (log.message || '')
      if (!logIdSet.has(logId)) { logIdSet.add(logId); taskLogs.value.push(parseLogMessage(log)); scrollToBottom() }
    } catch (err) { console.error('Failed to parse log:', err) }
  }
  logEventSource.onerror = () => {}
}

function stopLogPolling() { if (logPollingTimer) { clearInterval(logPollingTimer); logPollingTimer = null } }

function closeLogDialog() {
  logDialogVisible.value = false
  currentLogTaskId.value = ''
  currentLogTask.value = null
  taskLogs.value = []
  logIdSet.clear()
  logWorkerFilter.value = ''
  logLevelFilter.value = ''
  if (logEventSource) { logEventSource.close(); logEventSource = null }
  stopLogPolling()
}
</script>

<style lang="scss" scoped>
.task-page {
  .action-card { margin-bottom: 20px; }
  .pagination { margin-top: 20px; justify-content: flex-end; }
  .form-hint { margin-left: 10px; color: var(--el-text-color-secondary); font-size: 12px; }
  .sub-task-info { font-size: 11px; color: var(--el-text-color-secondary); margin-top: 2px; }
  .tool-tip { color: var(--el-color-danger); font-size: 12px; }
  .progress-hint { color: var(--el-text-color-secondary); font-size: 12px; }
}

.task-dialog {
  :deep(.el-dialog__body) { padding: 10px 20px 0; }
}

.task-tabs {
  :deep(.el-tabs__header) { margin-bottom: 15px; }
  :deep(.el-tabs__item) { font-size: 14px; }
}

.tab-form {
  min-height: 320px;
  padding: 10px 0;
}

.dialog-footer {
  padding-top: 10px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.log-progress {
  margin-bottom: 15px;
  padding: 12px 15px;
  background-color: var(--el-fill-color-light);
  border-radius: 6px;
  .progress-info {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 10px;
    .task-name { font-weight: 500; font-size: 14px; }
  }
}

.log-filter {
  margin-bottom: 10px;
  display: flex;
  align-items: center;
  .log-stats { margin-left: auto; color: var(--el-text-color-secondary); font-size: 12px; }
}

.log-container {
  max-height: 450px;
  overflow-y: auto;
  background-color: var(--code-bg);
  border-radius: 4px;
  padding: 10px;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  line-height: 1.6;
}

.log-empty { color: var(--el-text-color-secondary); text-align: center; padding: 20px; }
.log-entry { padding: 2px 0; white-space: pre-wrap; word-break: break-all; }
.log-time { color: var(--el-color-success); margin-right: 8px; font-size: 11px; }
.log-level { font-weight: bold; margin-right: 6px; min-width: 45px; display: inline-block; font-size: 11px; }
.log-worker { color: var(--el-color-primary); margin-right: 6px; font-size: 11px; }
.log-message { color: var(--el-text-color-primary); }
.log-debug .log-level { color: var(--el-text-color-secondary); }
.log-info .log-level { color: var(--el-color-info); }
.log-warn .log-level, .log-warning .log-level { color: var(--el-color-warning); }
.log-error .log-level { color: var(--el-color-danger); }

.config-section {
  margin-top: 15px;
  h4 { color: var(--el-text-color-primary); font-weight: 500; }
}

.config-detail {
  margin-top: 10px;
}

/* 任务详情对话框现代化样式 */
.task-detail-dialog {
  :deep(.el-dialog__body) {
    padding: 0 20px 20px;
    max-height: 70vh;
    overflow-y: auto;
  }
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 20px;
  background: linear-gradient(135deg, var(--el-fill-color-light) 0%, var(--el-fill-color-lighter) 100%);
  border-radius: 12px;
  margin-bottom: 16px;
  border: 1px solid var(--el-border-color-lighter);
}

.detail-header-main {
  flex: 1;
  min-width: 0;
}

.task-title-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.task-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.status-tag {
  font-size: 13px;
  padding: 6px 12px;
  border-radius: 6px;
}

.task-target {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  .el-icon { margin-top: 2px; flex-shrink: 0; }
  .target-text {
    word-break: break-all;
    line-height: 1.5;
    max-height: 60px;
    overflow-y: auto;
  }
}

.progress-circle-wrapper {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  .progress-value {
    font-size: 16px;
    font-weight: 600;
    color: var(--el-text-color-primary);
  }
  .subtask-info {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}

.time-cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-bottom: 16px;
}

.time-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 10px;
  transition: all 0.2s ease;
  &:hover {
    background: var(--el-fill-color-light);
    transform: translateY(-1px);
  }
}

.time-icon {
  font-size: 20px;
  color: var(--el-color-primary);
  flex-shrink: 0;
}

.time-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.time-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.time-value {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.result-section {
  margin-bottom: 16px;
  padding: 14px 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 10px;
  border-left: 3px solid var(--el-color-info);
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  margin-bottom: 10px;
  .el-icon { color: var(--el-color-primary); }
}

.result-content {
  font-size: 13px;
  color: var(--el-text-color-regular);
  line-height: 1.6;
  max-height: 80px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.config-section-modern {
  background: var(--el-fill-color-lighter);
  border-radius: 12px;
  padding: 16px;
}

.strategy-overview {
  margin-bottom: 16px;
}

.strategy-card {
  background: var(--el-bg-color);
  border-radius: 10px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
}

.strategy-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  .strategy-icon {
    font-size: 18px;
    color: var(--el-color-primary);
  }
  .strategy-title {
    font-size: 14px;
    font-weight: 500;
    color: var(--el-text-color-primary);
  }
}

.strategy-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
}

.stat-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  .stat-label {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .stat-value {
    font-size: 16px;
    font-weight: 600;
    color: var(--el-color-primary);
  }
}

.module-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
  margin-bottom: 14px;
}

.module-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px;
  background: var(--el-bg-color);
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
  transition: all 0.2s ease;
  &.active {
    border-color: var(--el-color-success);
    background: var(--el-fill-color-light);
  }
}

html.dark .module-card.active {
  border-color: var(--el-color-success);
  background: rgba(103, 194, 58, 0.15);
}

.module-icon {
  font-size: 20px;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
  .active & { color: var(--el-color-success); }
}

.module-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.module-name {
  font-size: 13px;
  color: var(--el-text-color-regular);
  font-weight: 500;
}

.module-details {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  .detail-item {
    font-size: 11px;
    color: var(--el-text-color-secondary);
    background: var(--el-fill-color-light);
    padding: 2px 6px;
    border-radius: 4px;
  }
}

.batch-info {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background: var(--el-bg-color);
  border-radius: 6px;
  font-size: 13px;
  color: var(--el-text-color-regular);
  margin-bottom: 14px;
  .el-icon { color: var(--el-color-primary); }
}

.config-collapse {
  border: none;
  :deep(.el-collapse-item__header) {
    background: var(--el-bg-color);
    border-radius: 8px;
    padding: 0 12px;
    height: 44px;
    border: 1px solid var(--el-border-color-lighter);
    margin-bottom: 8px;
    &:hover { background: var(--el-fill-color-light); }
  }
  :deep(.el-collapse-item__wrap) {
    border: none;
    background: transparent;
  }
  :deep(.el-collapse-item__content) {
    padding: 0 0 12px;
  }
}

.collapse-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  .el-icon { color: var(--el-color-primary); }
}

.config-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
  padding: 12px;
  background: var(--el-bg-color);
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
}

.config-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  &.full-width { grid-column: span 4; }
}

.config-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.config-value {
  font-size: 13px;
  color: var(--el-text-color-primary);
  font-weight: 500;
  &.highlight { color: var(--el-color-primary); }
  &.code {
    font-family: 'Consolas', 'Monaco', monospace;
    background: var(--el-fill-color-light);
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 12px;
  }
}
</style>
