<template>
  <div class="task-page">
    <!-- 操作栏 -->
    <el-card class="action-card" :body-style="{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '10px' }">
      <el-button type="primary" @click="goToCreateTask">
        <el-icon>
          <Plus />
        </el-icon>{{ $t('task.newTask') }}
      </el-button>
      <el-button @click="goToTemplateManage">
        <el-icon>
          <Document />
        </el-icon>{{ $t('task.scanTemplate') }}
      </el-button>
      <el-switch v-model="autoRefresh" style="margin-left: 10px" :active-text="$t('task.autoRefresh')" inactive-text=""
        @change="handleAutoRefreshChange" />
      <el-button :loading="loading" style="margin-left: auto" @click="loadData">
        <el-icon>
          <Refresh />
        </el-icon>{{ $t('common.refresh') }}
      </el-button>
    </el-card>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <div style="margin-bottom: 15px; display: flex; justify-content: space-between; align-items: center;">
        <div>
          <el-button type="danger" :disabled="selectedRows.length === 0" @click="handleBatchDelete">
            <el-icon>
              <Delete />
            </el-icon>{{ $t('task.batchDelete') }} ({{ selectedRows.length }})
          </el-button>
        </div>
        <div style="display: flex; gap: 10px;">
          <el-select v-model="filterTags" multiple filterable :placeholder="$t('task.filterByTags')" clearable
            style="width: 250px" @change="loadData">
            <el-option v-for="tag in allTags" :key="tag" :label="tag" :value="tag" />
          </el-select>
        </div>
      </div>
      <el-skeleton :loading="loading && tableData.length === 0" animated :count="10">
        <template #template>
          <div style="padding: 10px 0; display: flex; gap: 10px;">
            <el-skeleton-item variant="rect" style="width: 30px; height: 30px;" />
            <el-skeleton-item variant="rect" style="width: 150px; height: 30px;" />
            <el-skeleton-item variant="rect" style="width: 250px; height: 30px;" />
            <el-skeleton-item variant="rect" style="width: 100px; height: 30px;" />
            <el-skeleton-item variant="rect" style="width: 150px; height: 30px;" />
            <el-skeleton-item variant="rect" style="flex: 1; height: 30px;" />
          </div>
        </template>
        <template #default>
          <el-table :data="tableData" v-loading="loading && tableData.length > 0" stripe max-height="500"
            @selection-change="handleSelectionChange">
            <el-table-column type="selection" width="50" />
            <el-table-column prop="name" :label="$t('task.taskName')" min-width="150">
              <template #default="{ row }">
                <span>{{ row.name }}</span>
                <el-tag v-if="row.isCron && row.cronRule" type="info" size="small" effect="plain"
                  style="margin-left: 6px;">
                  {{ getCronSourceLabel(row) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="target" :label="$t('task.scanTarget')" min-width="150" show-overflow-tooltip />
            <el-table-column prop="status" :label="$t('task.status')" width="150">
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
                <el-button v-if="row.status === 'CREATED' || !row.status" type="success" link size="small"
                  @click="handleStart(row)">{{ $t('task.start') }}</el-button>
                <el-button v-if="row.status === 'CREATED' || !row.status" type="warning" link size="small"
                  @click="goToEditTask(row)">{{ $t('task.edit') }}</el-button>
                <el-button v-if="['STARTED', 'PENDING'].includes(row.status)" type="warning" link size="small"
                  @click="handlePause(row)">{{ $t('task.pause') }}</el-button>
                <el-button v-if="row.status === 'PAUSED'" type="success" link size="small" @click="handleResume(row)">{{
                  $t('task.resume') }}</el-button>
                <el-button
                  v-if="['STARTED', 'PAUSED', 'PENDING', 'CREATED', ''].includes(row.status) && row.status !== 'SUCCESS' && row.status !== 'FAILURE' && row.status !== 'STOPPED'"
                  type="danger" link size="small" @click="handleStop(row)">{{ $t('task.stop') }}</el-button>
                <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('task.detail') }}</el-button>
                <el-button type="info" link size="small" @click="showLogs(row)">{{ $t('task.logs') }}</el-button>
                <el-button type="info" link size="small" @click="viewReport(row)">{{ $t('task.report') }}</el-button>
                <el-button v-if="['SUCCESS', 'FAILURE', 'STOPPED'].includes(row.status)" type="warning" link
                  size="small" @click="handleRetry(row)">{{ $t('task.retry') }}</el-button>
                <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('task.delete')
                }}</el-button>
              </template>
            </el-table-column>
          </el-table>
        </template>
      </el-skeleton>
      <el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize"
        :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next"
        class="pagination" @size-change="loadData" @current-change="loadData" />
    </el-card>

    <!-- 新建/编辑任务对话框 - Tab页布局 -->
    <el-dialog v-model="dialogVisible" :title="isEdit ? $t('task.editTask') : $t('task.newTask')" width="720px"
      top="5vh" class="task-dialog">
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
            <el-form-item :label="$t('task.organization')">
              <el-select v-model="form.orgId" :placeholder="$t('task.selectOrganization')" clearable
                style="width: 100%">
                <el-option v-for="org in organizations" :key="org.id" :label="org.name" :value="org.id" />
              </el-select>
            </el-form-item>
            <el-form-item :label="$t('task.specifyWorker')">
              <el-select v-model="form.workers" multiple :placeholder="$t('task.anyWorkerExecute')" clearable
                style="width: 100%">
                <el-option v-for="w in workers" :key="w.name" :label="`${w.name} (${w.ip})`" :value="w.name" />
              </el-select>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- 子域名扫描 Tab -->
        <el-tab-pane name="domainscan">
          <template #label>
            <span>{{ $t('task.subdomainScan') }} <el-tag v-if="form.domainscanEnable" type="success" size="small"
                style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
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
            <span>{{ $t('task.portScan') }} <el-tag v-if="form.portscanEnable" type="success" size="small"
                style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
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
                  <el-form-item :label="$t('task.portscanTargetTimeout')">
                    <el-input-number v-model="form.portscanTargetTimeout" :min="5" :max="1200" style="width:100%" />
                    <span class="form-hint">{{ $t('task.portscanTargetTimeoutHint') }}</span>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-row :gutter="20" v-if="form.portscanTool === 'naabu'">
                <el-col :span="12">
                  <el-form-item :label="$t('task.naabuProbeTimeoutMs')">
                    <el-input-number v-model="form.portscanProbeTimeoutMs" :min="50" :max="10000" :step="50" style="width:100%" />
                    <span class="form-hint">{{ $t('task.naabuProbeTimeoutHint') }}</span>
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
            <span>{{ $t('task.portIdentify') }} <el-tag v-if="form.portidentifyEnable" type="success" size="small"
                style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
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
            <span>{{ $t('task.fingerprintScan') }} <el-tag v-if="form.fingerprintEnable" type="success" size="small"
                style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
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
                <el-checkbox v-model="form.fingerprintCert">{{ $t('task.cert') }}</el-checkbox>
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
            <span>{{ $t('task.vulScan') }} <el-tag v-if="form.pocscanEnable" type="success" size="small"
                style="margin-left:4px">{{ $t('task.enabled') }}</el-tag></span>
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
                <el-checkbox v-model="form.pocscanAutoScan" :disabled="form.pocscanCustomOnly">{{
                  $t('task.customTagMapping') }}</el-checkbox>
                <el-checkbox v-model="form.pocscanAutomaticScan" :disabled="form.pocscanCustomOnly">{{
                  $t('task.webFingerprintAutoMatch') }}</el-checkbox>
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
      </el-tabs>
      <template #footer>
        <div class="dialog-footer">
          <el-button @click="dialogVisible = false">{{ $t('common.cancel') }}</el-button>
          <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ isEdit ? $t('common.save') :
            $t('task.createTask') }}</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 任务日志对话框 -->
    <!-- 任务日志内联面板 -->
    <el-card v-if="taskLogPanelVisible" class="task-log-panel-card">
      <div class="log-panel-header">
        <div class="log-panel-title">
          <el-icon>
            <Document />
          </el-icon>
          <span>{{ $t('task.taskLog') }} - {{ currentLogTask?.name }}</span>
          <el-tag :type="getStatusType(currentLogTask?.status, currentLogTask)" size="small">{{
            getStatusText(currentLogTask) }}</el-tag>
        </div>
        <div class="log-panel-actions">
          <el-input v-model="logSearchKeyword" :placeholder="$t('task.searchLogs')" clearable size="small"
            style="width: 180px">
            <template #prefix><el-icon>
                <Search />
              </el-icon></template>
          </el-input>
          <el-select v-model="logWorkerFilter" :placeholder="$t('task.filterWorker')" clearable size="small"
            style="width: 150px">
            <el-option :label="$t('task.allWorkers')" value="" />
            <el-option v-for="w in logWorkers" :key="w" :label="w" :value="w" />
          </el-select>
          <el-select v-model="logLevelFilter" :placeholder="$t('task.filterLevel')" clearable size="small"
            style="width: 120px">
            <el-option :label="$t('task.allLevels')" value="" />
            <el-option label="DEBUG" value="DEBUG" />
            <el-option label="INFO" value="INFO" />
            <el-option label="WARN" value="WARN" />
            <el-option label="ERROR" value="ERROR" />
          </el-select>
          <el-checkbox v-model="logIncludeDebug" size="small" @change="refreshLogs">{{ $t('common.includeDebug') }}</el-checkbox>
          <el-button type="primary" size="small" :loading="logLoading" @click="refreshLogs">
            <el-icon style="margin-right: 4px">
              <Refresh />
            </el-icon>{{ $t('common.refresh') }}
          </el-button>
          <el-button size="small" @click="closeLogPanel">{{ $t('common.close') }}</el-button>
        </div>
      </div>
      <el-progress v-if="currentLogTask" :percentage="Math.min(currentLogTask.progress || 0, 100)"
        :status="currentLogTask.status === 'SUCCESS' ? 'success' : (currentLogTask.status === 'FAILURE' ? 'exception' : '')"
        :stroke-width="8" style="margin-bottom: 10px" />
      <div class="log-container" ref="logContainerRef">
        <div v-if="filteredLogs.length === 0" class="log-empty">{{ $t('task.noLogs') }}</div>
        <div v-for="(log, index) in filteredLogs" :key="index" class="log-entry"
          :class="'log-' + log.level.toLowerCase()">
          <span class="log-time">{{ formatLogTime(log.timestamp) }}</span>
          <span class="log-level">[{{ log.level }}]</span>
          <span class="log-worker">{{ log.workerName }}</span>
          <span class="log-message">{{ log.displayMessage }}</span>
        </div>
      </div>
      <div class="log-panel-footer">
        <span class="log-count-badge">{{ filteredLogs.length }} / {{ taskLogs.length }}</span>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Search, Document, Refresh } from '@element-plus/icons-vue'
import ScanWorkflow from '@/components/ScanWorkflow.vue'
import { getTaskList, createTask, deleteTask, batchDeleteTask, retryTask, startTask, pauseTask, resumeTask, stopTask, updateTask, getTaskLogs, getWorkerList, saveScanConfig, getScanConfig } from '@/api/task'
import { validateTargets, formatValidationErrors } from '@/utils/target'
import request from '@/api/request'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const taskLogPanelVisible = ref(false)
const logLoading = ref(false)
const tableData = ref([])
const organizations = ref([])
const workers = ref([])
const allTags = ref([]) // 所有标签列表
const filterTags = ref([]) // 过滤标签
const formRef = ref()
const logContainerRef = ref()
const selectedRows = ref([])
const autoRefresh = ref(true)
const activeTab = ref('basic')
const isEdit = ref(false)
const taskLogs = ref([])
const currentLogTaskId = ref('')
const currentLogTask = ref(null)
const logWorkerFilter = ref('')
const logLevelFilter = ref('')
const logSearchKeyword = ref('')
const logIncludeDebug = ref(false) // 是否包含 DEBUG 级别日志（默认不含，用于与容器日志对齐排查）
let refreshTimer = null

const pagination = reactive({ page: 1, pageSize: 20, total: 0 })

const form = reactive({
  id: '',
  name: '',
  target: '',
  orgId: '',
  workers: [],
  batchSize: 0,
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
  portscanTargetTimeout: 60,
  portscanProbeTimeoutMs: 1000,
  skipHostDiscovery: false,
  excludeCDN: false,
  excludeHosts: '',
  portidentifyEnable: false,
  portidentifyTool: 'nmap',
  portidentifyTimeout: 60,
  portidentifyConcurrency: 10,
  portidentifyArgs: '-sV --version-intensity 5',
  portidentifyUDP: false,
  portidentifyFastMode: false,
  portidentifyForceScan: false,
  portidentifyTimeout: 60,
  fingerprintEnable: true,
  fingerprintTool: 'httpx',
  fingerprintIconHash: true,
  fingerprintCustomEngine: false,
  fingerprintScreenshot: true,
  fingerprintCert: false,
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
  // 从新建任务页跳转回来时，Worker 拉取任务需要短暂时间，
  // 立即刷新只能看到 PENDING（等待执行）状态，延迟再刷新一次让状态更新为执行中
  if (route.query.created) {
    setTimeout(() => loadData(), 2000)
    // 清除 query 参数，避免刷新页面时重复触发延迟刷新
    router.replace({ path: route.path, query: {} })
  }
})

onUnmounted(() => {
  stopAutoRefresh()
})

function handleAutoRefreshChange(val) { val ? startAutoRefresh() : stopAutoRefresh() }
function startAutoRefresh() { stopAutoRefresh(); refreshTimer = setInterval(() => loadData(), 30000) }
function stopAutoRefresh() { if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null } }

async function loadData() {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      pageSize: pagination.pageSize
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

// 获取定时任务来源标签
function getCronSourceLabel(row) {
  if (!row.isCron || !row.cronRule) return ''
  // 根据任务名称或配置判断来源
  if (row.name && row.name.includes('空间引擎')) return '空间引擎'
  if (row.name && row.name.includes('(定时)')) return '定时扫描'
  return '定时任务'
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


function resetForm() {
  Object.assign(form, {
    id: '', name: '', target: '', orgId: '', workers: [],
    batchSize: 0,
    // 子域名扫描
    domainscanEnable: false, domainscanSubfinder: true, domainscanTimeout: 300, domainscanMaxEnumTime: 10,
    domainscanThreads: 10, domainscanRateLimit: 0,
    domainscanRemoveWildcard: true, domainscanResolveDNS: true, domainscanConcurrent: 50,
    // 端口扫描
    portscanEnable: true, portscanTool: 'naabu', portscanRate: 1000, ports: 'top100',
    portThreshold: 50, scanType: 'c', portscanTargetTimeout: 60, portscanProbeTimeoutMs: 1000, skipHostDiscovery: false, portidentifyEnable: false, portidentifyTimeout: 60,
    portidentifyArgs: '', fingerprintEnable: true, fingerprintTool: 'httpx', fingerprintIconHash: true,
    fingerprintCustomEngine: false, fingerprintScreenshot: true, fingerprintCert: false,
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
  activeTab.value = 'basic'
  dialogVisible.value = true
}

// 应用配置到表单
function applyConfig(config) {
  Object.assign(form, {
    batchSize: config.batchSize || 0,
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
    portThreshold: config.portscan?.portThreshold || 50,
    scanType: config.portscan?.scanType || 'c',
    portscanTargetTimeout: config.portscan?.targetTimeout ?? config.portscan?.timeout ?? 60,
    portscanProbeTimeoutMs: config.portscan?.probeTimeoutMs ?? 1000,
    skipHostDiscovery: config.portscan?.skipHostDiscovery ?? false,
    excludeCDN: config.portscan?.excludeCDN ?? false,
    excludeHosts: config.portscan?.excludeHosts || '',
    portidentifyEnable: config.portidentify?.enable ?? false,
    portidentifyTool: config.portidentify?.tool || 'nmap',
    portidentifyTimeout: config.portidentify?.timeout || 60,
    portidentifyConcurrency: config.portidentify?.concurrency || 10,
    portidentifyArgs: config.portidentify?.args || '',
    portidentifyUDP: config.portidentify?.udp ?? false,
    portidentifyFastMode: config.portidentify?.fastMode ?? false,
    fingerprintEnable: config.fingerprint?.enable ?? true,
    fingerprintTool: config.fingerprint?.tool || (config.fingerprint?.httpx ? 'httpx' : 'builtin'),
    fingerprintIconHash: config.fingerprint?.iconHash ?? true,
    fingerprintCustomEngine: config.fingerprint?.customEngine ?? false,
    fingerprintScreenshot: config.fingerprint?.screenshot ?? true,
    fingerprintCert: config.fingerprint?.cert ?? false,
    fingerprintTimeout: config.fingerprint?.targetTimeout || 30,
    pocscanEnable: config.pocscan?.enable ?? false,
    pocscanAutoScan: config.pocscan?.autoScan ?? true,
    pocscanAutomaticScan: config.pocscan?.automaticScan ?? true,
    pocscanCustomOnly: config.pocscan?.customPocOnly ?? false,
    pocscanSeverity: config.pocscan?.severity ? config.pocscan.severity.split(',') : ['critical', 'high', 'medium'],
    pocscanTargetTimeout: config.pocscan?.targetTimeout || 600
  })
}

function showDetail(row) {
  router.push({ path: '/task/detail', query: { id: row.id } })
}


function buildConfig() {
  return {
    batchSize: form.batchSize,
    domainscan: { enable: form.domainscanEnable, subfinder: form.domainscanSubfinder, timeout: form.domainscanTimeout, maxEnumerationTime: form.domainscanMaxEnumTime, threads: form.domainscanThreads, rateLimit: form.domainscanRateLimit, all: form.domainscanAll, recursive: form.domainscanRecursive, removeWildcard: form.domainscanRemoveWildcard, resolveDNS: form.domainscanResolveDNS, concurrent: form.domainscanConcurrent },
    portscan: { enable: form.portscanEnable, tool: form.portscanTool, rate: form.portscanRate, ports: form.ports, portThreshold: form.portThreshold, scanType: form.scanType, targetTimeout: form.portscanTargetTimeout, probeTimeoutMs: form.portscanProbeTimeoutMs, skipHostDiscovery: form.skipHostDiscovery, excludeCDN: form.excludeCDN, excludeHosts: form.excludeHosts },
    portidentify: { enable: form.portidentifyEnable, tool: form.portidentifyTool, timeout: form.portidentifyTimeout, concurrency: form.portidentifyConcurrency, args: form.portidentifyArgs, udp: form.portidentifyUDP, fastMode: form.portidentifyFastMode, forceScan: form.portidentifyForceScan && !form.portscanEnable },
    fingerprint: { enable: form.fingerprintEnable, tool: form.fingerprintTool, iconHash: form.fingerprintIconHash, customEngine: form.fingerprintCustomEngine, screenshot: form.fingerprintScreenshot, cert: form.fingerprintCert, targetTimeout: form.fingerprintTimeout, forceScan: form.fingerprintForceScan && !form.portscanEnable && !form.portidentifyEnable },
    pocscan: { enable: form.pocscanEnable, useNuclei: true, forceScan: form.pocscanForceScan && !hasPrePhaseEnabled.value, autoScan: form.pocscanAutoScan, automaticScan: form.pocscanAutomaticScan, customPocOnly: form.pocscanCustomOnly, severity: form.pocscanSeverity.join(','), targetTimeout: form.pocscanTargetTimeout }
  }
}

// 扫描配置字段列表（用于监听变化自动保存）
const scanConfigFields = [
  'batchSize',
  'domainscanEnable', 'domainscanSubfinder', 'domainscanTimeout', 'domainscanMaxEnumTime', 'domainscanThreads', 'domainscanRateLimit', 'domainscanAll', 'domainscanRecursive', 'domainscanRemoveWildcard', 'domainscanResolveDNS', 'domainscanConcurrent',
  'portscanEnable', 'portscanTool', 'portscanRate', 'ports', 'portThreshold', 'scanType', 'portscanTargetTimeout', 'portscanProbeTimeoutMs', 'skipHostDiscovery', 'excludeCDN', 'excludeHosts',
  'portidentifyEnable', 'portidentifyTool', 'portidentifyTimeout', 'portidentifyConcurrency', 'portidentifyArgs', 'portidentifyUDP', 'portidentifyFastMode', 'portidentifyForceScan',
  'fingerprintEnable', 'fingerprintTool', 'fingerprintIconHash', 'fingerprintCustomEngine', 'fingerprintScreenshot', 'fingerprintCert', 'fingerprintTimeout', 'fingerprintForceScan',
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
    const data = { name: form.name, target: form.target, orgId: form.orgId, workers: form.workers, config: configStr }
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
      // 新建任务后延迟再刷新一次，等待 Worker 拉取任务后状态从“等待执行”更新为“执行中”
      if (!isEdit.value) {
        setTimeout(() => loadData(), 2000)
      }
    } else { ElMessage.error(res.msg) }
  } finally { submitting.value = false }
}

async function handleDelete(row) {
  await ElMessageBox.confirm(t('task.confirmDeleteTask'), t('common.tip'), { type: 'warning' })
  const res = await deleteTask({ id: row.id })
  res.code === 0 ? (ElMessage.success(t('task.deleteSuccess')), loadData()) : ElMessage.error(res.msg)
}

function handleSelectionChange(rows) { selectedRows.value = rows }

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  await ElMessageBox.confirm(t('task.confirmBatchDelete', { count: selectedRows.value.length }), t('common.tip'), { type: 'warning' })
  const res = await batchDeleteTask({ ids: selectedRows.value.map(row => row.id) })
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
  const res = await startTask({ id: row.id })
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
  const res = await pauseTask({ id: row.id })
  res.code === 0 ? (ElMessage.success(t('task.taskPaused')), loadData()) : ElMessage.error(res.msg)
}

async function handleResume(row) {
  const res = await resumeTask({ id: row.id })
  res.code === 0 ? (ElMessage.success(t('task.taskResumed')), loadData()) : ElMessage.error(res.msg)
}

async function handleStop(row) {
  await ElMessageBox.confirm(t('task.confirmStop'), t('common.tip'), { type: 'warning' })
  const res = await stopTask({ id: row.id })
  res.code === 0 ? (ElMessage.success(t('task.taskStopped')), loadData()) : ElMessage.error(res.msg)
}

function viewReport(row) { router.push({ path: '/report', query: { taskId: row.id } }) }

async function showLogs(row) {
  currentLogTaskId.value = row.taskId
  currentLogTask.value = { ...row }
  taskLogs.value = []
  taskLogPanelVisible.value = true
  logLoading.value = false
  // 打开日志对话框时自动刷新一次（纯手动刷新模式，不再自动轮询/SSE）
  await refreshLogs()
}

async function refreshLogs() {
  if (!currentLogTaskId.value) return
  logLoading.value = true
  try {
    const task = tableData.value.find(t => t.id === currentLogTask.value?.id)
    if (task) currentLogTask.value = { ...task }
    const res = await getTaskLogs({ taskId: currentLogTaskId.value, limit: 500, includeDebug: logIncludeDebug.value })
    if (res.code === 0) {
      // 每次刷新都是完整请求，直接替换日志列表（不再使用 logIdSet 去重）
      taskLogs.value = (res.list || []).map(log => parseLogMessage(log))
      taskLogs.value.sort((a, b) => (a.timestamp || '').localeCompare(b.timestamp || ''))
      scrollToBottom()
    }
  } catch (err) { console.error('Failed to load task logs:', err) }
  finally {
    logLoading.value = false
  }
}

function scrollToBottom() {
  setTimeout(() => { if (logContainerRef.value) logContainerRef.value.scrollTop = logContainerRef.value.scrollHeight }, 100)
}

function closeLogPanel() {
  taskLogPanelVisible.value = false
  currentLogTaskId.value = ''
  currentLogTask.value = null
  taskLogs.value = []
  logWorkerFilter.value = ''
  logLevelFilter.value = ''
  logLoading.value = false
}
</script>

<style lang="scss" scoped>
.task-page {
  .action-card {
    margin-bottom: 20px;
  }

  .pagination {
    margin-top: 20px;
    justify-content: flex-end;
  }

  .form-hint {
    margin-left: 10px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }

  .sub-task-info {
    font-size: 11px;
    color: var(--el-text-color-secondary);
    margin-top: 2px;
  }

  .tool-tip {
    color: var(--el-color-danger);
    font-size: 12px;
  }

  .progress-hint {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
}

.task-dialog {
  :deep(.el-dialog__body) {
    padding: 10px 20px 0;
  }
}

.task-tabs {
  :deep(.el-tabs__header) {
    margin-bottom: 15px;
  }

  :deep(.el-tabs__item) {
    font-size: 14px;
  }
}

.tab-form {
  min-height: 320px;
  padding: 10px 0;
}

.dialog-footer {
  padding-top: 10px;
  border-top: 1px solid var(--el-border-color-lighter);
}

/* ========== 任务日志内联面板 ========== */
.task-log-panel-card {
  transition: all 0.3s ease;
  border-top: 2px solid var(--el-color-primary) !important;
  margin-bottom: 20px;
}

.log-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.log-panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.log-panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.log-panel-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding-top: 8px;
  border-top: 1px solid var(--el-border-color-lighter);
  margin-top: 8px;
}

.log-count-badge {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
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

.log-empty {
  color: var(--el-text-color-secondary);
  text-align: center;
  padding: 20px;
}

.log-entry {
  padding: 2px 0;
  white-space: pre-wrap;
  word-break: break-all;
}

.log-time {
  color: var(--el-color-success);
  margin-right: 8px;
  font-size: 11px;
}

.log-level {
  font-weight: bold;
  margin-right: 6px;
  min-width: 45px;
  display: inline-block;
  font-size: 11px;
}

.log-worker {
  color: var(--el-color-primary);
  margin-right: 6px;
  font-size: 11px;
}

.log-message {
  color: var(--el-text-color-primary);
}

.log-debug .log-level {
  color: var(--el-text-color-secondary);
}

.log-info .log-level {
  color: var(--el-color-info);
}

.log-warn .log-level,
.log-warning .log-level {
  color: var(--el-color-warning);
}

.log-error .log-level {
  color: var(--el-color-danger);
}

.config-section {
  margin-top: 15px;

  h4 {
    color: var(--el-text-color-primary);
    font-weight: 500;
  }
}

.config-detail {
  margin-top: 10px;
}
</style>
