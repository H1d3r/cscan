<template>
  <div class="cron-task-page">
    <!-- 操作栏 -->
    <el-card class="action-card">
      <el-button type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>{{ $t('cronTask.newCronTask') }}
      </el-button>
      <el-button @click="loadData">
        <el-icon><Refresh /></el-icon>{{ $t('common.refresh') }}
      </el-button>
      <el-button 
        type="danger" 
        :disabled="selectedRows.length === 0"
        @click="handleBatchDelete"
      >
        <el-icon><Delete /></el-icon>{{ $t('common.batchDelete') }} {{ selectedRows.length > 0 ? `(${selectedRows.length})` : '' }}
      </el-button>
    </el-card>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <el-table 
        :data="tableData" 
        v-loading="loading" 
        stripe
        @selection-change="handleSelectionChange"
      >
        <el-table-column type="selection" width="50" />
        <el-table-column prop="name" :label="$t('cronTask.cronTaskName')" min-width="140" />
        <el-table-column prop="taskName" :label="$t('cronTask.relatedTask')" min-width="140">
          <template #default="{ row }">
            <span class="task-link" @click="goToTask(row)">{{ row.taskName }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="targetShort" :label="$t('cronTask.scanTarget')" min-width="180" show-overflow-tooltip />
        <el-table-column :label="$t('cronTask.scheduleType')" width="180">
          <template #default="{ row }">
            <div v-if="row.scheduleType === 'cron'">
              <el-tag type="primary" size="small">{{ $t('cronTask.cronExec').split(' ')[0] }}</el-tag>
              <el-tooltip :content="getCronDescription(row.cronSpec)" placement="top">
                <code class="cron-code">{{ row.cronSpec }}</code>
              </el-tooltip>
            </div>
            <div v-else>
              <el-tag type="warning" size="small">{{ $t('cronTask.onceExec').split(' ')[0] }}</el-tag>
              <span class="schedule-time">{{ row.scheduleTime }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('cronTask.status')" width="80">
          <template #default="{ row }">
            <el-switch
              v-model="row.status"
              active-value="enable"
              inactive-value="disable"
              @change="handleToggle(row)"
            />
          </template>
        </el-table-column>
        <el-table-column prop="nextRunTime" :label="$t('cronTask.nextRunTime')" width="160">
          <template #default="{ row }">
            <span v-if="row.status === 'enable' && row.nextRunTime">{{ row.nextRunTime }}</span>
            <span v-else class="text-muted">{{ row.status === 'disable' ? $t('cronTask.disabled') : '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="lastRunTime" :label="$t('cronTask.lastRunTime')" width="160">
          <template #default="{ row }">
            {{ row.lastRunTime || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="runCount" :label="$t('cronTask.runCount')" width="90">
          <template #default="{ row }">
            <el-tag type="info" size="small">{{ row.runCount || 0 }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button type="success" link size="small" @click="handleRunNow(row)">
              <el-icon><VideoPlay /></el-icon>{{ $t('cronTask.runNow') }}
            </el-button>
            <el-button type="primary" link size="small" @click="handleEdit(row)">
              <el-icon><Edit /></el-icon>{{ $t('common.edit') }}
            </el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">
              <el-icon><Delete /></el-icon>{{ $t('common.delete') }}
            </el-button>
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

    <!-- 新建/编辑对话框 -->
    <el-dialog v-model="dialogVisible" :title="isEdit ? $t('cronTask.editCronTask') : $t('cronTask.newCronTask')" width="1000px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
        <el-form-item :label="$t('cronTask.cronTaskName')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('cronTask.pleaseEnterName')" />
        </el-form-item>
        
        <el-form-item :label="$t('cronTask.relatedTask')" prop="mainTaskId">
          <el-select 
            v-model="form.mainTaskId" 
            :placeholder="$t('cronTask.pleaseSelectTask')" 
            style="width: 100%" 
            filterable
            @change="onTaskSelect"
          >
            <el-option 
              v-for="task in taskList" 
              :key="task.taskId" 
              :label="task.name" 
              :value="task.taskId"
            >
              <div class="task-option">
                <span class="task-name">{{ task.name }}</span>
                <span class="task-target">{{ truncateTarget(task.target) }}</span>
              </div>
            </el-option>
          </el-select>
        </el-form-item>

        <el-form-item :label="$t('cronTask.scanTarget')" prop="target">
          <el-input 
            v-model="form.target" 
            type="textarea" 
            :rows="4" 
            :placeholder="$t('cronTask.targetPlaceholder')"
          />
          <div class="form-hint">{{ $t('cronTask.targetHint') }}</div>
        </el-form-item>

        <el-form-item :label="$t('cronTask.scheduleType')" prop="scheduleType">
          <el-radio-group v-model="form.scheduleType">
            <el-radio label="cron">{{ $t('cronTask.cronExec') }}</el-radio>
            <el-radio label="once">{{ $t('cronTask.onceExec') }}</el-radio>
          </el-radio-group>
        </el-form-item>

        <!-- Cron表达式 -->
        <el-form-item v-if="form.scheduleType === 'cron'" :label="$t('cronTask.cronExpression')" prop="cronSpec">
          <el-input v-model="form.cronSpec" :placeholder="$t('cronTask.cronPlaceholder')">
            <template #append>
              <el-button @click="validateCron">{{ $t('cronTask.validate') }}</el-button>
            </template>
          </el-input>
          <div class="cron-help">
            <div class="cron-presets">
              <span class="preset-label">{{ $t('cronTask.quickSelect') }}</span>
              <el-tag 
                v-for="preset in cronPresets" 
                :key="preset.value" 
                size="small" 
                class="preset-tag"
                @click="form.cronSpec = preset.value; validateCron()"
              >
                {{ preset.label }}
              </el-tag>
            </div>
            <div v-if="cronValidation.valid" class="cron-next-times">
              <div class="next-label">{{ $t('cronTask.next5Times') }}</div>
              <div v-for="(time, index) in cronValidation.nextTimes" :key="index" class="next-time">
                {{ index + 1 }}. {{ time }}
              </div>
            </div>
            <div v-else-if="cronValidation.error" class="cron-error">
              {{ cronValidation.error }}
            </div>
          </div>
        </el-form-item>

        <!-- 指定时间 -->
        <el-form-item v-if="form.scheduleType === 'once'" :label="$t('cronTask.execTime')" prop="scheduleTime">
          <el-date-picker
            v-model="form.scheduleTimeDate"
            type="datetime"
            :placeholder="$t('common.pleaseSelect')"
            format="YYYY-MM-DD HH:mm:ss"
            value-format="YYYY-MM-DD HH:mm:ss"
            :disabled-date="disabledDate"
            style="width: 100%"
            @change="onScheduleTimeChange"
          />
          <div class="form-hint">{{ $t('cronTask.onceExecHint') }}</div>
        </el-form-item>

        <!-- 新建时提示：复用关联任务配置 -->
        <el-alert
          v-if="!isEdit && form.mainTaskId"
          :title="$t('cronTask.reuseConfigHint') || '将复用关联任务的扫描配置，创建后可在编辑中调整'"
          type="info"
          :closable="false"
          show-icon
          style="margin-bottom: 15px"
        />

        <!-- 扫描配置折叠面板 - 仅编辑时显示 -->
        <el-collapse v-if="isEdit" v-model="activeCollapse" class="config-collapse">
          <!-- 子域名扫描 -->
          <el-collapse-item name="domainscan">
            <template #title>
              <span class="collapse-title">{{ $t('task.subdomainScan') }} <el-tag v-if="form.domainscanEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.domainscanEnable" />
              <span class="form-hint">{{ $t('task.subdomainEnumHint') }}</span>
            </el-form-item>
            <template v-if="form.domainscanEnable">
              <el-form-item :label="$t('task.scanTool')">
                <el-checkbox v-model="form.domainscanSubfinder">Subfinder ({{ $t('task.passiveEnum') }})</el-checkbox>
                <el-checkbox v-model="form.domainscanBruteforce" :disabled="!form.subdomainDictIds || !form.subdomainDictIds.length">KSubdomain ({{ $t('task.dictBrute') }})</el-checkbox>
              </el-form-item>
              <el-row :gutter="24" class="scan-tools-layout">
                <el-col :span="12">
                  <div class="scan-tool-section">
                    <div class="scan-tool-header">
                      <span class="scan-tool-title">{{ $t('task.subfinderPassiveEnum') }}</span>
                      <el-tag :type="form.domainscanSubfinder ? 'success' : 'info'" size="small">
                        {{ form.domainscanSubfinder ? $t('task.started') : $t('task.notStarted') }}
                      </el-tag>
                    </div>
                    <template v-if="form.domainscanSubfinder">
                      <el-form-item :label="$t('task.timeoutSeconds')">
                        <el-input-number v-model="form.domainscanTimeout" :min="60" :max="3600" style="width:100%" />
                      </el-form-item>
                      <el-form-item :label="$t('task.maxEnumTime') + '(' + $t('task.minutes') + ')'">
                        <el-input-number v-model="form.domainscanMaxEnumTime" :min="1" :max="60" style="width:100%" />
                      </el-form-item>
                      <el-form-item :label="$t('task.scanOptions')">
                        <el-checkbox v-model="form.domainscanRemoveWildcard">{{ $t('task.removeWildcardDomain') }}</el-checkbox>
                      </el-form-item>
                      <el-form-item :label="$t('task.dnsResolve')">
                        <el-checkbox v-model="form.domainscanResolveDNS">{{ $t('task.resolveSubdomainDns') }}</el-checkbox>
                      </el-form-item>
                    </template>
                  </div>
                </el-col>
                <el-col :span="12">
                  <div class="scan-tool-section">
                    <div class="scan-tool-header">
                      <span class="scan-tool-title">{{ $t('task.ksubdomainDictBrute') }}</span>
                      <el-tag :type="form.domainscanBruteforce ? 'success' : 'info'" size="small">
                        {{ form.domainscanBruteforce ? $t('task.started') : $t('task.notStarted') }}
                      </el-tag>
                    </div>
                    <el-form-item :label="$t('task.bruteforceDict')">
                      <div class="selected-dict-summary">
                        <el-tag type="primary" size="small" v-if="form.subdomainDictIds && form.subdomainDictIds.length">
                          {{ $t('task.selectedCount', { count: form.subdomainDictIds.length }) }}
                        </el-tag>
                        <span v-else class="warning-hint">{{ $t('task.selectDictFirst') }}</span>
                        <el-button type="primary" link @click="showSubdomainDictSelectDialog">{{ $t('task.selectDict') }}</el-button>
                      </div>
                    </el-form-item>
                    <template v-if="form.domainscanBruteforce">
                      <el-form-item :label="$t('task.bruteforceTimeout') + ' (' + $t('task.minutes') + ')'">
                        <el-input-number v-model="form.domainscanBruteforceTimeout" :min="1" :max="120" style="width:100%" />
                      </el-form-item>
                    </template>
                  </div>
                </el-col>
              </el-row>
            </template>
          </el-collapse-item>

          <!-- 端口扫描 -->
          <el-collapse-item name="portscan">
            <template #title>
              <span class="collapse-title">{{ $t('task.portScan') }} <el-tag v-if="form.portscanEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.portscanEnable" />
            </el-form-item>
            <template v-if="form.portscanEnable">
              <el-form-item :label="$t('task.scanTool')">
                <el-radio-group v-model="form.portscanTool">
                  <el-radio label="naabu">Naabu ({{ $t('task.recommended') }})</el-radio>
                  <el-radio label="masscan">Masscan</el-radio>
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
                  <el-form-item :label="$t('task.timeoutSeconds')">
                    <el-input-number v-model="form.portscanTimeout" :min="5" :max="1200" style="width:100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item v-if="form.portscanTool === 'naabu'" :label="$t('task.scanType')">
                    <el-radio-group v-model="form.scanType">
                      <el-radio label="c">CONNECT</el-radio>
                      <el-radio label="s">SYN</el-radio>
                    </el-radio-group>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item :label="$t('task.advancedOptions')">
                <el-checkbox v-model="form.skipHostDiscovery">{{ $t('task.skipHostDiscovery') }} (-Pn)</el-checkbox>
                <el-checkbox v-if="form.portscanTool === 'naabu'" v-model="form.excludeCDN">{{ $t('task.excludeCdnWaf') }} (-ec)</el-checkbox>
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 端口识别 -->
          <el-collapse-item name="portidentify">
            <template #title>
              <span class="collapse-title">{{ $t('task.portIdentify') }} <el-tag v-if="form.portidentifyEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
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
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 指纹识别 -->
          <el-collapse-item name="fingerprint">
            <template #title>
              <span class="collapse-title">{{ $t('task.fingerprintScan') }} <el-tag v-if="form.fingerprintEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
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
                  <el-radio label="httpx">Httpx</el-radio>
                  <el-radio label="builtin">{{ $t('task.builtinEngine') }}</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item :label="$t('task.additionalFeatures')">
                <el-checkbox v-model="form.fingerprintIconHash">{{ $t('task.iconHash') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintCustomEngine">{{ $t('task.customFingerprint') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintScreenshot">{{ $t('task.screenshot') }}</el-checkbox>
              </el-form-item>
              <el-form-item :label="$t('task.timeoutSeconds')">
                <el-input-number v-model="form.fingerprintTimeout" :min="5" :max="120" />
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 目录扫描 -->
          <el-collapse-item name="dirscan">
            <template #title>
              <span class="collapse-title">{{ $t('task.dirScan') }} <el-tag v-if="form.dirscanEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.dirscanEnable" />
            </el-form-item>
            <template v-if="form.dirscanEnable">
              <!-- 强制扫描：仅在前序阶段均未启用时显示 -->
              <el-form-item v-if="!hasPrePhaseEnabled" :label="$t('task.forceScan')">
                <el-switch v-model="form.dirscanForceScan" />
                <span class="form-hint warning-hint">{{ $t('task.forceScanHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.scanDict')">
                <div class="selected-dict-summary">
                  <el-tag type="primary" size="small" v-if="form.dirscanDictIds.length">
                    {{ $t('task.selectedCount', { count: form.dirscanDictIds.length }) }}
                  </el-tag>
                  <span v-if="!form.dirscanDictIds.length" class="secondary-hint">{{ $t('task.noDictSelected') }}</span>
                  <el-button type="primary" link @click="showDictSelectDialog">{{ $t('task.selectDict') }}</el-button>
                </div>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item :label="$t('task.concurrentThreads')">
                    <el-input-number v-model="form.dirscanThreads" :min="1" :max="200" style="width:100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.requestTimeoutSeconds')">
                    <el-input-number v-model="form.dirscanTimeout" :min="1" :max="60" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
            </template>
          </el-collapse-item>

          <!-- 漏洞扫描 -->
          <el-collapse-item name="pocscan">
            <template #title>
              <span class="collapse-title">{{ $t('task.vulScan') }} <el-tag v-if="form.pocscanEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.pocscanEnable" />
            </el-form-item>
            <template v-if="form.pocscanEnable">
              <!-- 强制扫描：仅在前序阶段均未启用时显示 -->
              <el-form-item v-if="!hasPrePhaseEnabled" :label="$t('task.forceScan')">
                <el-switch v-model="form.pocscanForceScan" />
                <span class="form-hint warning-hint">{{ $t('task.forceScanHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.pocSource')">
                <el-radio-group v-model="form.pocscanMode" @change="handlePocModeChange">
                  <el-radio label="auto">{{ $t('task.autoMatch') }}</el-radio>
                  <el-radio label="manual">{{ $t('task.manualSelect') }}</el-radio>
                </el-radio-group>
              </el-form-item>
              <template v-if="form.pocscanMode === 'auto'">
                <el-form-item :label="$t('task.autoScan')">
                  <el-checkbox v-model="form.pocscanAutoScan" :disabled="form.pocscanCustomOnly">{{ $t('task.customTagMapping') }}</el-checkbox>
                  <el-checkbox v-model="form.pocscanAutomaticScan" :disabled="form.pocscanCustomOnly || !form.fingerprintEnable">{{ $t('task.webFingerprintAutoMatch') }}</el-checkbox>
                </el-form-item>
                <el-form-item :label="$t('task.customPoc')">
                  <el-checkbox v-model="form.pocscanCustomOnly">{{ $t('task.onlyUseCustomPoc') }}</el-checkbox>
                </el-form-item>
              </template>
              <template v-if="form.pocscanMode === 'manual'">
                <el-form-item :label="$t('task.selectedPoc')">
                  <div class="selected-poc-summary">
                    <el-tag type="primary" size="small" v-if="form.pocscanNucleiTemplateIds.length">
                      {{ $t('task.defaultTemplate') }}: {{ form.pocscanNucleiTemplateIds.length }}
                    </el-tag>
                    <el-tag type="warning" size="small" v-if="form.pocscanCustomPocIds.length">
                      {{ $t('task.customPoc') }}: {{ form.pocscanCustomPocIds.length }}
                    </el-tag>
                    <span v-if="!form.pocscanNucleiTemplateIds.length && !form.pocscanCustomPocIds.length" class="secondary-hint">
                      {{ $t('task.noPocSelected') }}
                    </span>
                    <el-button type="primary" link @click="showPocSelectDialog">{{ $t('task.selectPoc') }}</el-button>
                  </div>
                </el-form-item>
              </template>
              <el-form-item :label="$t('task.severityLevel')">
                <el-checkbox-group v-model="form.pocscanSeverity">
                  <el-checkbox label="critical">Critical</el-checkbox>
                  <el-checkbox label="high">High</el-checkbox>
                  <el-checkbox label="medium">Medium</el-checkbox>
                  <el-checkbox label="low">Low</el-checkbox>
                  <el-checkbox label="info">Info</el-checkbox>
                </el-checkbox-group>
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 高级设置 -->
          <el-collapse-item name="advanced">
            <template #title>
              <span class="collapse-title">{{ $t('task.advancedSettings') }}</span>
            </template>
            <el-form-item :label="$t('task.taskSplit')">
              <el-input-number v-model="form.batchSize" :min="0" :max="1000" :step="10" />
              <span class="form-hint">{{ $t('task.batchTargetCount') }}</span>
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 目录扫描字典选择对话框 -->
    <el-dialog v-model="dictSelectDialogVisible" :title="$t('task.selectDirScanDict')" width="800px" @open="handleDictDialogOpen">
      <el-table 
        ref="dictTableRef"
        :data="dictList" 
        v-loading="dictLoading" 
        max-height="400"
        @selection-change="handleDictSelectionChange"
        row-key="id"
      >
        <el-table-column type="selection" width="45" :reserve-selection="true" />
        <el-table-column prop="name" :label="$t('task.dictName')" min-width="150" />
        <el-table-column prop="pathCount" :label="$t('task.pathCount')" width="100" />
        <el-table-column prop="isBuiltin" :label="$t('common.type')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">{{ row.isBuiltin ? $t('task.builtin') : $t('task.custom') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="dictSelectDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmDictSelection">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 子域名字典选择对话框 -->
    <el-dialog v-model="subdomainDictSelectDialogVisible" :title="$t('task.selectSubdomainDict')" width="800px" @open="handleSubdomainDictDialogOpen">
      <el-table 
        ref="subdomainDictTableRef"
        :data="subdomainDictList" 
        v-loading="subdomainDictLoading" 
        max-height="400"
        @selection-change="handleSubdomainDictSelectionChange"
        row-key="id"
      >
        <el-table-column type="selection" width="45" :reserve-selection="true" />
        <el-table-column prop="name" :label="$t('task.dictName')" min-width="150" />
        <el-table-column prop="wordCount" :label="$t('task.wordCount')" width="100" />
        <el-table-column prop="isBuiltin" :label="$t('common.type')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">{{ row.isBuiltin ? $t('task.builtin') : $t('task.custom') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="subdomainDictSelectDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmSubdomainDictSelection">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 递归爆破字典选择对话框 -->
    <el-dialog v-model="recursiveDictSelectDialogVisible" :title="$t('task.selectRecursiveDict')" width="800px" @open="handleRecursiveDictDialogOpen">
      <el-table 
        ref="recursiveDictTableRef"
        :data="recursiveDictList" 
        v-loading="recursiveDictLoading" 
        max-height="400"
        @selection-change="handleRecursiveDictSelectionChange"
        row-key="id"
      >
        <el-table-column type="selection" width="45" :reserve-selection="true" />
        <el-table-column prop="name" :label="$t('task.dictName')" min-width="150" />
        <el-table-column prop="wordCount" :label="$t('task.wordCount')" width="100" />
        <el-table-column prop="isBuiltin" :label="$t('common.type')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">{{ row.isBuiltin ? $t('task.builtin') : $t('task.custom') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="recursiveDictSelectDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmRecursiveDictSelection">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- POC选择对话框 -->
    <el-dialog v-model="pocSelectDialogVisible" :title="$t('task.selectPoc')" width="1200px" @open="handlePocDialogOpen">
      <div class="poc-select-container">
        <!-- 左侧：POC列表 -->
        <div class="poc-select-left">
          <el-tabs v-model="pocSelectTab">
            <!-- 默认模板 -->
            <el-tab-pane :label="$t('task.defaultTemplate')" name="nuclei">
              <el-form :inline="true" class="poc-filter-form">
                <el-form-item>
                  <el-input v-model="nucleiTemplateFilter.keyword" :placeholder="$t('task.nameOrId')" clearable style="width: 150px" @keyup.enter="loadNucleiTemplatesForSelect" />
                </el-form-item>
                <el-form-item>
                  <el-select v-model="nucleiTemplateFilter.severity" :placeholder="$t('task.level')" clearable style="width: 100px" @change="loadNucleiTemplatesForSelect">
                    <el-option label="Critical" value="critical" />
                    <el-option label="High" value="high" />
                    <el-option label="Medium" value="medium" />
                    <el-option label="Low" value="low" />
                    <el-option label="Info" value="info" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" size="small" @click="loadNucleiTemplatesForSelect">{{ $t('common.search') }}</el-button>
                  <el-button type="success" size="small" @click="selectAllNucleiTemplates" :loading="selectAllNucleiLoading">{{ $t('task.selectAll') }}</el-button>
                  <el-button type="warning" size="small" @click="deselectAllNucleiTemplates" v-if="selectedNucleiTemplateIds.length > 0">{{ $t('task.deselectAll') }}</el-button>
                </el-form-item>
              </el-form>
              <el-table 
                ref="nucleiTableRef"
                :data="nucleiTemplateList" 
                v-loading="nucleiTemplateLoading" 
                max-height="400"
                @selection-change="handleNucleiSelectionChange"
                row-key="id"
              >
                <el-table-column type="selection" width="45" :reserve-selection="true" />
                <el-table-column prop="id" :label="$t('task.templateId')" width="180" show-overflow-tooltip />
                <el-table-column prop="name" :label="$t('common.name')" min-width="150" show-overflow-tooltip />
                <el-table-column prop="severity" :label="$t('task.level')" width="80">
                  <template #default="{ row }">
                    <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('common.operation')" width="60" fixed="right">
                  <template #default="{ row }">
                    <el-button type="primary" link size="small" @click="viewPocContent(row, 'nuclei')">{{ $t('common.view') }}</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <el-pagination
                v-model:current-page="nucleiTemplatePagination.page"
                v-model:page-size="nucleiTemplatePagination.pageSize"
                :total="nucleiTemplatePagination.total"
                :page-sizes="[50, 100, 200]"
                layout="total, sizes, prev, pager, next"
                class="poc-pagination"
                @size-change="loadNucleiTemplatesForSelect"
                @current-change="loadNucleiTemplatesForSelect"
              />
            </el-tab-pane>

            <!-- 自定义POC -->
            <el-tab-pane :label="$t('task.customPoc')" name="custom">
              <el-form :inline="true" class="poc-filter-form">
                <el-form-item>
                  <el-input v-model="customPocFilter.name" :placeholder="$t('common.name')" clearable style="width: 150px" @keyup.enter="loadCustomPocsForSelect" />
                </el-form-item>
                <el-form-item>
                  <el-select v-model="customPocFilter.severity" :placeholder="$t('task.level')" clearable style="width: 100px" @change="loadCustomPocsForSelect">
                    <el-option label="Critical" value="critical" />
                    <el-option label="High" value="high" />
                    <el-option label="Medium" value="medium" />
                    <el-option label="Low" value="low" />
                    <el-option label="Info" value="info" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" size="small" @click="loadCustomPocsForSelect">{{ $t('common.search') }}</el-button>
                  <el-button type="success" size="small" @click="selectAllCustomPocs" :loading="selectAllCustomLoading">{{ $t('task.selectAll') }}</el-button>
                  <el-button type="warning" size="small" @click="deselectAllCustomPocs" v-if="selectedCustomPocIds.length > 0">{{ $t('task.deselectAll') }}</el-button>
                </el-form-item>
              </el-form>
              <el-table 
                ref="customPocTableRef"
                :data="customPocList" 
                v-loading="customPocLoading" 
                max-height="400"
                @selection-change="handleCustomPocSelectionChange"
                row-key="id"
              >
                <el-table-column type="selection" width="45" :reserve-selection="true" />
                <el-table-column prop="name" :label="$t('common.name')" min-width="150" show-overflow-tooltip />
                <el-table-column prop="templateId" :label="$t('task.templateId')" width="150" show-overflow-tooltip />
                <el-table-column prop="severity" :label="$t('task.level')" width="80">
                  <template #default="{ row }">
                    <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('common.operation')" width="60" fixed="right">
                  <template #default="{ row }">
                    <el-button type="primary" link size="small" @click="viewPocContent(row, 'custom')">{{ $t('common.view') }}</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <el-pagination
                v-model:current-page="customPocPagination.page"
                v-model:page-size="customPocPagination.pageSize"
                :total="customPocPagination.total"
                :page-sizes="[50, 100, 200]"
                layout="total, sizes, prev, pager, next"
                class="poc-pagination"
                @size-change="loadCustomPocsForSelect"
                @current-change="loadCustomPocsForSelect"
              />
            </el-tab-pane>
          </el-tabs>
        </div>

        <!-- 右侧：已选择列表 -->
        <div class="poc-select-right">
          <div class="selected-header">
            <span>{{ $t('task.selected') }} ({{ selectedNucleiTemplates.length + selectedCustomPocs.length }})</span>
            <el-button type="danger" link size="small" @click="clearAllSelections" v-if="selectedNucleiTemplates.length + selectedCustomPocs.length > 0">
              {{ $t('task.clearAll') }}
            </el-button>
          </div>
          <div class="selected-search">
            <el-input v-model="selectedPocSearchKeyword" :placeholder="$t('task.searchSelected')" clearable size="small" :prefix-icon="Search" />
          </div>
          <div class="selected-list">
            <!-- 默认模板 -->
            <div v-if="filteredSelectedNucleiTemplates.length > 0" class="selected-group">
              <div class="group-header">
                <span>{{ $t('task.defaultTemplate') }} ({{ filteredSelectedNucleiTemplates.length }})</span>
                <el-button type="danger" link size="small" @click="clearNucleiSelections">{{ $t('task.clear') }}</el-button>
              </div>
              <div class="selected-items">
                <div v-for="item in filteredSelectedNucleiTemplates" :key="item.id" class="selected-item">
                  <span class="item-name" :title="item.name || item.id">{{ item.name || item.id }}</span>
                  <el-icon class="item-remove" @click="removeNucleiTemplate(item.id)"><Close /></el-icon>
                </div>
              </div>
            </div>
            <!-- 自定义POC -->
            <div v-if="filteredSelectedCustomPocs.length > 0" class="selected-group">
              <div class="group-header">
                <span>{{ $t('task.customPoc') }} ({{ filteredSelectedCustomPocs.length }})</span>
                <el-button type="danger" link size="small" @click="clearCustomPocSelections">{{ $t('task.clear') }}</el-button>
              </div>
              <div class="selected-items">
                <div v-for="item in filteredSelectedCustomPocs" :key="item.id" class="selected-item">
                  <span class="item-name" :title="item.name">{{ item.name }}</span>
                  <el-icon class="item-remove" @click="removeCustomPoc(item.id)"><Close /></el-icon>
                </div>
              </div>
            </div>
            <!-- 空状态 -->
            <div v-if="filteredSelectedNucleiTemplates.length === 0 && filteredSelectedCustomPocs.length === 0" class="selected-empty">
              <span>{{ selectedPocSearchKeyword ? $t('task.noMatchingResults') : $t('task.noPocSelected') }}</span>
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <el-button @click="pocSelectDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmPocSelection">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 查看POC内容对话框 -->
    <el-dialog v-model="pocContentDialogVisible" :title="pocContentTitle" width="800px">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item :label="$t('task.templateId')">{{ currentViewPoc.id || currentViewPoc.templateId }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.name')">{{ currentViewPoc.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.severityLevel')">
          <el-tag :type="getSeverityType(currentViewPoc.severity)" size="small">{{ currentViewPoc.severity }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('task.author')">{{ currentViewPoc.author || '-' }}</el-descriptions-item>
      </el-descriptions>
      <div class="poc-content-wrapper" v-loading="pocContentLoading">
        <el-input
          v-model="currentViewPoc.content"
          type="textarea"
          :rows="18"
          readonly
        />
      </div>
      <template #footer>
        <el-button @click="pocContentDialogVisible = false">{{ $t('common.close') }}</el-button>
        <el-button type="primary" @click="copyPocContent">{{ $t('task.copyContent') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Edit, Delete, VideoPlay, Close, Search, InfoFilled } from '@element-plus/icons-vue'
import { 
  getCronTaskList, 
  saveCronTask, 
  toggleCronTask, 
  deleteCronTask,
  batchDeleteCronTask,
  runCronTaskNow,
  validateCronSpec 
} from '@/api/crontask'
import { getTaskList } from '@/api/task'
import { getNucleiTemplateList, getCustomPocList, getNucleiTemplateDetail } from '@/api/poc'
import { getDirScanDictEnabledList } from '@/api/dirscan'
import { getSubdomainDictEnabledList } from '@/api/subdomain'

const router = useRouter()
const { t } = useI18n()
const loading = ref(false)
const tableData = ref([])
const selectedRows = ref([])
const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

const dialogVisible = ref(false)
const isEdit = ref(false)
const submitting = ref(false)
const formRef = ref(null)
const form = reactive({
  id: '',
  name: '',
  scheduleType: 'cron',
  cronSpec: '0 0 2 * * *',
  scheduleTime: '',
  scheduleTimeDate: null,
  mainTaskId: '',
  target: '',
  config: '',
  // 子域名扫描
  domainscanEnable: false,
  domainscanSubfinder: true,
  domainscanBruteforce: false,
  domainscanBruteforceTimeout: 30,
  domainscanTimeout: 300,
  domainscanMaxEnumTime: 10,
  domainscanThreads: 10,
  domainscanRateLimit: 0,
  domainscanRemoveWildcard: true,
  domainscanResolveDNS: true,
  domainscanConcurrent: 50,
  subdomainDictIds: [],
  subdomainDicts: [],
  domainscanRecursiveBrute: false,
  recursiveDictIds: [],
  recursiveDicts: [],
  domainscanWildcardDetect: true,
  // 端口扫描
  portscanEnable: true,
  portscanTool: 'naabu',
  portscanRate: 3000,
  ports: 'top100',
  portThreshold: 100,
  scanType: 'c',
  portscanTimeout: 60,
  skipHostDiscovery: false,
  excludeCDN: false,
  excludeHosts: '',
  portscanWorkers: 50,
  portscanRetries: 2,
  portscanWarmUpTime: 1,
  portscanVerify: false,
  // 端口识别
  portidentifyEnable: false,
  portidentifyTool: 'nmap',
  portidentifyTimeout: 30,
  portidentifyConcurrency: 10,
  portidentifyArgs: '',
  portidentifyUDP: false,
  portidentifyFastMode: false,
  portidentifyForceScan: false,
  // 指纹识别
  fingerprintEnable: true,
  fingerprintTool: 'httpx',
  fingerprintIconHash: true,
  fingerprintCustomEngine: false,
  fingerprintScreenshot: false,
  fingerprintActiveScan: false,
  fingerprintActiveTimeout: 10,
  fingerprintTimeout: 30,
  fingerprintFilterMode: 'http_mapping',
  fingerprintForceScan: false,
  // 漏洞扫描
  pocscanEnable: false,
  pocscanMode: 'auto',
  pocscanAutoScan: true,
  pocscanAutomaticScan: true,
  pocscanCustomOnly: false,
  pocscanSeverity: ['critical', 'high', 'medium'],
  pocscanTargetTimeout: 600,
  pocscanForceScan: false,
  pocscanNucleiTemplateIds: [],
  pocscanCustomPocIds: [],
  pocscanNucleiTemplates: [],
  pocscanCustomPocs: [],
  // 目录扫描
  dirscanEnable: false,
  dirscanDictIds: [],
  dirscanDicts: [],
  dirscanThreads: 50,
  dirscanTimeout: 10,
  dirscanStatusCodes: [200, 301, 302, 401, 403],
  dirscanFollowRedirect: false,
  dirscanForceScan: false,
  // 高级设置
  batchSize: 50
})

// 扫描配置折叠面板
const activeCollapse = ref(['portscan', 'fingerprint'])

// 判断是否有前序扫描阶段启用（用于控制强制扫描开关的显隐）
const hasPrePhaseEnabled = computed(() => {
  return form.domainscanEnable || form.portscanEnable ||
         form.portidentifyEnable || form.fingerprintEnable
})

// 目录扫描字典选择相关
const dictSelectDialogVisible = ref(false)
const dictList = ref([])
const dictLoading = ref(false)
const dictTableRef = ref()
const selectedDictIds = ref([])

// 子域名字典选择相关
const subdomainDictSelectDialogVisible = ref(false)
const subdomainDictList = ref([])
const subdomainDictLoading = ref(false)
const subdomainDictTableRef = ref()
const selectedSubdomainDictIds = ref([])

// 递归爆破字典选择相关
const recursiveDictSelectDialogVisible = ref(false)
const recursiveDictList = ref([])
const recursiveDictLoading = ref(false)
const recursiveDictTableRef = ref()
const selectedRecursiveDictIds = ref([])

// POC选择相关
const pocSelectDialogVisible = ref(false)
const pocSelectTab = ref('nuclei')
const nucleiTemplateList = ref([])
const customPocList = ref([])
const nucleiTemplateLoading = ref(false)
const customPocLoading = ref(false)
const selectAllNucleiLoading = ref(false)
const selectAllCustomLoading = ref(false)
const isSelectingAll = ref(false)
const isLoadingData = ref(false)
const nucleiTableRef = ref()
const customPocTableRef = ref()
const selectedNucleiTemplateIds = ref([])
const selectedCustomPocIds = ref([])
const selectedNucleiTemplates = ref([])
const selectedCustomPocs = ref([])
const selectedPocSearchKeyword = ref('')
const nucleiTemplateFilter = reactive({ keyword: '', severity: '', category: '', tag: '' })
const customPocFilter = reactive({ name: '', severity: '', tag: '' })
const nucleiTemplatePagination = reactive({ page: 1, pageSize: 50, total: 0 })
const customPocPagination = reactive({ page: 1, pageSize: 50, total: 0 })

// 查看POC内容相关
const pocContentDialogVisible = ref(false)
const pocContentLoading = ref(false)
const pocContentTitle = ref('')
const currentViewPoc = ref({})

// 过滤后的已选择列表
const filteredSelectedNucleiTemplates = computed(() => {
  if (!selectedPocSearchKeyword.value) return selectedNucleiTemplates.value
  const keyword = selectedPocSearchKeyword.value.toLowerCase()
  return selectedNucleiTemplates.value.filter(t => 
    (t.name && t.name.toLowerCase().includes(keyword)) || 
    (t.id && t.id.toLowerCase().includes(keyword))
  )
})

const filteredSelectedCustomPocs = computed(() => {
  if (!selectedPocSearchKeyword.value) return selectedCustomPocs.value
  const keyword = selectedPocSearchKeyword.value.toLowerCase()
  return selectedCustomPocs.value.filter(p => 
    (p.name && p.name.toLowerCase().includes(keyword)) || 
    (p.templateId && p.templateId.toLowerCase().includes(keyword)) ||
    (p.id && p.id.toLowerCase().includes(keyword))
  )
})

const rules = {
  name: [{ required: true, message: t('cronTask.pleaseEnterName'), trigger: 'blur' }],
  mainTaskId: [{ required: true, message: t('cronTask.pleaseSelectTask'), trigger: 'change' }],
  scheduleType: [{ required: true, message: t('common.pleaseSelect'), trigger: 'change' }],
  cronSpec: [{ 
    required: true, 
    validator: (rule, value, callback) => {
      if (form.scheduleType === 'cron' && !value) {
        callback(new Error(t('cronTask.cronValidateError')))
      } else {
        callback()
      }
    },
    trigger: 'blur' 
  }],
  scheduleTime: [{
    required: true,
    validator: (rule, value, callback) => {
      if (form.scheduleType === 'once' && !form.scheduleTimeDate) {
        callback(new Error(t('common.pleaseSelect')))
      } else {
        callback()
      }
    },
    trigger: 'change'
  }]
}

const cronPresets = computed(() => [
  { label: t('cronTask.everyHour'), value: '0 0 * * * *' },
  { label: t('cronTask.everyDay2am'), value: '0 0 2 * * *' },
  { label: t('cronTask.everyMonday'), value: '0 0 3 * * 1' },
  { label: t('cronTask.every6hours'), value: '0 0 */6 * * *' }
])

const cronValidation = reactive({
  valid: false,
  nextTimes: [],
  error: ''
})

const taskList = ref([])

const selectedTask = computed(() => {
  return taskList.value.find(t => t.taskId === form.mainTaskId)
})

// 加载数据
async function loadData() {
  loading.value = true
  try {
    const res = await getCronTaskList({
      page: pagination.page,
      pageSize: pagination.pageSize
    })
    // 调试日志已移除
    if (res.code === 0) {
      tableData.value = res.data?.list || []
      pagination.total = res.data?.total || 0
    } else {
      console.error('加载定时任务失败:', res.msg)
    }
  } catch (error) {
    console.error('加载定时任务失败:', error)
  } finally {
    loading.value = false
  }
}

// 加载任务列表（只加载已创建状态的任务作为模板）
async function loadTaskList() {
  try {
    const res = await getTaskList({ page: 1, pageSize: 500 })
    if (res.code === 0) {
      // 过滤出可用的任务（已创建或已完成的任务）
      // 注意：list直接在res下，不是res.data.list
      taskList.value = (res.list || []).filter(t => 
        ['CREATED', 'SUCCESS', 'FAILURE', 'STOPPED'].includes(t.status)
      )
    }
  } catch (error) {
    console.error('加载任务列表失败:', error)
  }
}

// 截取目标显示
function truncateTarget(target, maxLen = 40) {
  if (!target) return ''
  const firstLine = target.split('\n')[0]
  if (firstLine.length > maxLen) {
    return firstLine.substring(0, maxLen) + '...'
  }
  return firstLine
}

// 显示创建对话框
function showCreateDialog() {
  isEdit.value = false
  Object.assign(form, {
    id: '',
    name: '',
    scheduleType: 'cron',
    cronSpec: '0 0 2 * * *',
    scheduleTime: '',
    scheduleTimeDate: null,
    mainTaskId: '',
    target: '',
    config: ''
  })
  // 重置扫描配置为默认值
  resetScanConfig()
  cronValidation.valid = false
  cronValidation.nextTimes = []
  cronValidation.error = ''
  dialogVisible.value = true
  loadTaskList()
}

// 编辑
function handleEdit(row) {
  isEdit.value = true
  Object.assign(form, {
    id: row.id,
    name: row.name,
    scheduleType: row.scheduleType || 'cron',
    cronSpec: row.cronSpec || '',
    scheduleTime: row.scheduleTime || '',
    scheduleTimeDate: row.scheduleTime || null,
    mainTaskId: row.mainTaskId,
    target: row.target || '',
    config: row.config || ''
  })
  // 重置并应用扫描配置
  resetScanConfig()
  if (row.config) {
    try {
      const config = JSON.parse(row.config)
      applyConfig(config)
    } catch (e) {
      console.error('解析配置失败:', e)
    }
  }
  cronValidation.valid = false
  cronValidation.nextTimes = []
  cronValidation.error = ''
  dialogVisible.value = true
  loadTaskList()
  if (form.scheduleType === 'cron' && form.cronSpec) {
    validateCron()
  }
}

// 选择任务时自动填充名称、目标和配置
function onTaskSelect(taskId) {
  const task = taskList.value.find(t => t.taskId === taskId)
  if (task) {
    if (!form.name) {
      form.name = `${t('cronTask.title')}-${task.name}`
    }
    // 新建时自动填充目标
    if (!isEdit.value && !form.target) {
      form.target = task.target || ''
    }
    
    // 同步扫描配置
    if (task.config) {
      try {
        // 先重置配置为默认值，避免配置混淆
        // 注意：这会重置所有扫描参数，如果用户已经修改了部分参数可能会被覆盖
        // 但符合"选择关联任务后展示对应设置的扫描配置"的需求
        const config = JSON.parse(task.config)
        
        // 只有在非编辑模式，或者用户确认要覆盖时才应用配置
        // 这里简化逻辑：只要切换任务就覆盖配置，因为这是"关联任务"的主要目的
        applyConfig(config)
        
        ElMessage.success(t('common.success') || '配置已同步')
      } catch (e) {
        console.error('解析任务配置失败:', e)
        ElMessage.warning('扫描配置同步失败')
      }
    }
  }
}

// 时间选择变化
function onScheduleTimeChange(val) {
  form.scheduleTime = val
}

// 禁用过去的日期
function disabledDate(time) {
  return time.getTime() < Date.now() - 24 * 60 * 60 * 1000
}

// 验证Cron表达式
async function validateCron() {
  if (!form.cronSpec) {
    cronValidation.valid = false
    cronValidation.error = t('cronTask.cronValidateError')
    return
  }
  try {
    const res = await validateCronSpec({ cronSpec: form.cronSpec })
    if (res.code === 0 && res.data?.valid) {
      cronValidation.valid = true
      cronValidation.nextTimes = res.data.nextTimes || []
      cronValidation.error = ''
    } else {
      cronValidation.valid = false
      cronValidation.nextTimes = []
      cronValidation.error = res.msg || t('cronTask.cronValidateError')
    }
  } catch (error) {
    cronValidation.valid = false
    cronValidation.error = t('cronTask.cronValidateError')
  }
}

// 提交表单
async function handleSubmit() {
  try {
    await formRef.value.validate()
  } catch {
    return
  }

  // 额外验证
  if (form.scheduleType === 'once' && !form.scheduleTimeDate) {
    ElMessage.error(t('common.pleaseSelect'))
    return
  }

  submitting.value = true
  try {
    // 获取选中任务的workspaceId
    const task = selectedTask.value
    // 构建扫描配置
    const config = buildConfig()
    const data = {
      id: form.id,
      name: form.name,
      scheduleType: form.scheduleType,
      cronSpec: form.scheduleType === 'cron' ? form.cronSpec : '',
      scheduleTime: form.scheduleType === 'once' ? form.scheduleTime : '',
      mainTaskId: form.mainTaskId,
      workspaceId: task?.workspaceId || '',
      target: form.target,
      config: JSON.stringify(config)
    }
    const res = await saveCronTask(data)
    if (res.code === 0) {
      ElMessage.success(isEdit.value ? t('cronTask.updateSuccess') : t('cronTask.createSuccess'))
      dialogVisible.value = false
      loadData()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    ElMessage.error(t('common.operationFailed'))
  } finally {
    submitting.value = false
  }
}

// 开关任务
async function handleToggle(row) {
  try {
    const res = await toggleCronTask({ id: row.id, status: row.status })
    if (res.code === 0) {
      ElMessage.success(t('cronTask.statusUpdateSuccess'))
      loadData()
    } else {
      row.status = row.status === 'enable' ? 'disable' : 'enable'
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    row.status = row.status === 'enable' ? 'disable' : 'enable'
    ElMessage.error(t('common.operationFailed'))
  }
}

// 立即执行
async function handleRunNow(row) {
  try {
    await ElMessageBox.confirm(t('cronTask.runNow') + '?', t('common.confirm'), { type: 'warning' })
    const res = await runCronTaskNow({ id: row.id })
    if (res.code === 0) {
      ElMessage.success(t('cronTask.runSuccess'))
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.operationFailed'))
    }
  }
}

// 删除
async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('cronTask.confirmDelete'), t('common.confirm'), { type: 'warning' })
    const res = await deleteCronTask({ id: row.id })
    if (res.code === 0) {
      ElMessage.success(t('cronTask.deleteSuccess'))
      loadData()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.operationFailed'))
    }
  }
}

// 选择变化
function handleSelectionChange(rows) {
  selectedRows.value = rows
}

// 批量删除
async function handleBatchDelete() {
  if (selectedRows.value.length === 0) {
    ElMessage.warning(t('common.pleaseSelect'))
    return
  }
  try {
    await ElMessageBox.confirm(
      t('cronTask.confirmBatchDelete', { count: selectedRows.value.length }), 
      t('common.batchDelete'), 
      { type: 'warning' }
    )
    
    const ids = selectedRows.value.map(row => row.id)
    const res = await batchDeleteCronTask({ ids })
    
    if (res.code === 0) {
      ElMessage.success(res.msg || t('cronTask.deleteSuccess'))
      selectedRows.value = []
      loadData()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.operationFailed'))
    }
  }
}

// 跳转到任务详情
function goToTask(row) {
  router.push('/task')
}

// 获取Cron描述
function getCronDescription(cronSpec) {
  if (!cronSpec) return ''
  const parts = cronSpec.split(' ')
  if (parts.length !== 6) return cronSpec
  
  const [sec, min, hour, day, month, week] = parts
  let desc = ''
  
  if (week !== '*') {
    const weekNames = [t('common.sunday') || 'Sun', t('common.monday') || 'Mon', t('common.tuesday') || 'Tue', t('common.wednesday') || 'Wed', t('common.thursday') || 'Thu', t('common.friday') || 'Fri', t('common.saturday') || 'Sat']
    desc += `${weekNames[parseInt(week)] || week} `
  }
  if (month !== '*') desc += `${month}M `
  if (day !== '*') desc += `${day}D `
  if (hour !== '*') desc += `${hour}h`
  if (min !== '*') desc += `${min}m`
  if (sec !== '*' && sec !== '0') desc += `${sec}s`
  
  return desc || cronSpec
}

// ==================== 字典选择相关方法 ====================

// 目录扫描字典对话框打开时
async function handleDictDialogOpen() {
  dictLoading.value = true
  try {
    const res = await getDirScanDictEnabledList()
    if (res.code === 0) {
      dictList.value = res.data?.list || []
      // 恢复之前的选择
      await nextTick()
      if (dictTableRef.value && form.dirscanDictIds.length > 0) {
        dictList.value.forEach(row => {
          if (form.dirscanDictIds.includes(row.id)) {
            dictTableRef.value.toggleRowSelection(row, true)
          }
        })
      }
    }
  } catch (error) {
    console.error('加载目录扫描字典列表失败:', error)
  } finally {
    dictLoading.value = false
  }
}

// 目录扫描字典选择变化
function handleDictSelectionChange(selection) {
  selectedDictIds.value = selection.map(item => item.id)
}

// 确认目录扫描字典选择
function confirmDictSelection() {
  form.dirscanDictIds = [...selectedDictIds.value]
  form.dirscanDicts = dictList.value.filter(d => selectedDictIds.value.includes(d.id))
  dictSelectDialogVisible.value = false
}

// 显示目录扫描字典选择对话框
function showDictSelectDialog() {
  selectedDictIds.value = [...form.dirscanDictIds]
  dictSelectDialogVisible.value = true
}

// 子域名字典对话框打开时
async function handleSubdomainDictDialogOpen() {
  subdomainDictLoading.value = true
  try {
    const res = await getSubdomainDictEnabledList()
    if (res.code === 0) {
      subdomainDictList.value = res.data?.list || []
      // 恢复之前的选择
      await nextTick()
      if (subdomainDictTableRef.value && form.subdomainDictIds.length > 0) {
        subdomainDictList.value.forEach(row => {
          if (form.subdomainDictIds.includes(row.id)) {
            subdomainDictTableRef.value.toggleRowSelection(row, true)
          }
        })
      }
    }
  } catch (error) {
    console.error('加载子域名字典列表失败:', error)
  } finally {
    subdomainDictLoading.value = false
  }
}

// 子域名字典选择变化
function handleSubdomainDictSelectionChange(selection) {
  selectedSubdomainDictIds.value = selection.map(item => item.id)
}

// 确认子域名字典选择
function confirmSubdomainDictSelection() {
  form.subdomainDictIds = [...selectedSubdomainDictIds.value]
  form.subdomainDicts = subdomainDictList.value.filter(d => selectedSubdomainDictIds.value.includes(d.id))
  subdomainDictSelectDialogVisible.value = false
}

// 显示子域名字典选择对话框
function showSubdomainDictSelectDialog() {
  selectedSubdomainDictIds.value = [...form.subdomainDictIds]
  subdomainDictSelectDialogVisible.value = true
}

// 递归爆破字典对话框打开时
async function handleRecursiveDictDialogOpen() {
  recursiveDictLoading.value = true
  try {
    const res = await getSubdomainDictEnabledList()
    if (res.code === 0) {
      recursiveDictList.value = res.data?.list || []
      // 恢复之前的选择
      await nextTick()
      if (recursiveDictTableRef.value && form.recursiveDictIds.length > 0) {
        recursiveDictList.value.forEach(row => {
          if (form.recursiveDictIds.includes(row.id)) {
            recursiveDictTableRef.value.toggleRowSelection(row, true)
          }
        })
      }
    }
  } catch (error) {
    console.error('加载递归爆破字典列表失败:', error)
  } finally {
    recursiveDictLoading.value = false
  }
}

// 递归爆破字典选择变化
function handleRecursiveDictSelectionChange(selection) {
  selectedRecursiveDictIds.value = selection.map(item => item.id)
}

// 确认递归爆破字典选择
function confirmRecursiveDictSelection() {
  form.recursiveDictIds = [...selectedRecursiveDictIds.value]
  form.recursiveDicts = recursiveDictList.value.filter(d => selectedRecursiveDictIds.value.includes(d.id))
  recursiveDictSelectDialogVisible.value = false
}

// 显示递归爆破字典选择对话框
function showRecursiveDictSelectDialog() {
  selectedRecursiveDictIds.value = [...form.recursiveDictIds]
  recursiveDictSelectDialogVisible.value = true
}

// ==================== POC选择相关方法 ====================

// POC对话框打开时
async function handlePocDialogOpen() {
  // 恢复之前的选择
  selectedNucleiTemplateIds.value = [...form.pocscanNucleiTemplateIds]
  selectedCustomPocIds.value = [...form.pocscanCustomPocIds]
  // 加载数据
  await Promise.all([
    loadNucleiTemplatesForSelect(),
    loadCustomPocsForSelect()
  ])
}

// 加载Nuclei模板列表
async function loadNucleiTemplatesForSelect() {
  nucleiTemplateLoading.value = true
  isLoadingData.value = true
  try {
    const res = await getNucleiTemplateList({
      page: nucleiTemplatePagination.page,
      pageSize: nucleiTemplatePagination.pageSize,
      keyword: nucleiTemplateFilter.keyword,
      severity: nucleiTemplateFilter.severity,
      tag: nucleiTemplateFilter.tag
    })
    if (res.code === 0) {
      nucleiTemplateList.value = res.data?.list || []
      nucleiTemplatePagination.total = res.data?.total || 0
      // 恢复选择状态
      await nextTick()
      if (nucleiTableRef.value) {
        nucleiTemplateList.value.forEach(row => {
          if (selectedNucleiTemplateIds.value.includes(row.id)) {
            nucleiTableRef.value.toggleRowSelection(row, true)
          }
        })
      }
    }
  } catch (error) {
    console.error('加载Nuclei模板列表失败:', error)
  } finally {
    nucleiTemplateLoading.value = false
    isLoadingData.value = false
  }
}

// 加载自定义POC列表
async function loadCustomPocsForSelect() {
  customPocLoading.value = true
  isLoadingData.value = true
  try {
    const res = await getCustomPocList({
      page: customPocPagination.page,
      pageSize: customPocPagination.pageSize,
      name: customPocFilter.name,
      severity: customPocFilter.severity,
      tag: customPocFilter.tag
    })
    if (res.code === 0) {
      customPocList.value = res.data?.list || []
      customPocPagination.total = res.data?.total || 0
      // 恢复选择状态
      await nextTick()
      if (customPocTableRef.value) {
        customPocList.value.forEach(row => {
          if (selectedCustomPocIds.value.includes(row.id)) {
            customPocTableRef.value.toggleRowSelection(row, true)
          }
        })
      }
    }
  } catch (error) {
    console.error('加载自定义POC列表失败:', error)
  } finally {
    customPocLoading.value = false
    isLoadingData.value = false
  }
}

// Nuclei模板选择变化
function handleNucleiSelectionChange(selection) {
  if (isSelectingAll.value || isLoadingData.value) return
  selectedNucleiTemplateIds.value = selection.map(item => item.id)
  selectedNucleiTemplates.value = selection.map(item => ({ id: item.id, name: item.name }))
}

// 自定义POC选择变化
function handleCustomPocSelectionChange(selection) {
  if (isSelectingAll.value || isLoadingData.value) return
  selectedCustomPocIds.value = selection.map(item => item.id)
  selectedCustomPocs.value = selection.map(item => ({ id: item.id, name: item.name, templateId: item.templateId }))
}

// 选择全部Nuclei模板
async function selectAllNucleiTemplates() {
  selectAllNucleiLoading.value = true
  isSelectingAll.value = true
  try {
    const res = await getNucleiTemplateList({
      page: 1,
      pageSize: 10000,
      keyword: nucleiTemplateFilter.keyword,
      severity: nucleiTemplateFilter.severity,
      tag: nucleiTemplateFilter.tag
    })
    if (res.code === 0) {
      const allItems = res.data?.list || []
      selectedNucleiTemplateIds.value = allItems.map(item => item.id)
      selectedNucleiTemplates.value = allItems.map(item => ({ id: item.id, name: item.name }))
      // 更新表格选择状态
      await nextTick()
      if (nucleiTableRef.value) {
        nucleiTemplateList.value.forEach(row => {
          nucleiTableRef.value.toggleRowSelection(row, true)
        })
      }
      ElMessage.success(t('task.selectedCount', { count: allItems.length }))
    }
  } catch (error) {
    console.error('选择全部Nuclei模板失败:', error)
  } finally {
    selectAllNucleiLoading.value = false
    isSelectingAll.value = false
  }
}

// 取消选择全部Nuclei模板
function deselectAllNucleiTemplates() {
  selectedNucleiTemplateIds.value = []
  selectedNucleiTemplates.value = []
  if (nucleiTableRef.value) {
    nucleiTableRef.value.clearSelection()
  }
}

// 选择全部自定义POC
async function selectAllCustomPocs() {
  selectAllCustomLoading.value = true
  isSelectingAll.value = true
  try {
    const res = await getCustomPocList({
      page: 1,
      pageSize: 10000,
      name: customPocFilter.name,
      severity: customPocFilter.severity,
      tag: customPocFilter.tag
    })
    if (res.code === 0) {
      const allItems = res.data?.list || []
      selectedCustomPocIds.value = allItems.map(item => item.id)
      selectedCustomPocs.value = allItems.map(item => ({ id: item.id, name: item.name, templateId: item.templateId }))
      // 更新表格选择状态
      await nextTick()
      if (customPocTableRef.value) {
        customPocList.value.forEach(row => {
          customPocTableRef.value.toggleRowSelection(row, true)
        })
      }
      ElMessage.success(t('task.selectedCount', { count: allItems.length }))
    }
  } catch (error) {
    console.error('选择全部自定义POC失败:', error)
  } finally {
    selectAllCustomLoading.value = false
    isSelectingAll.value = false
  }
}

// 取消选择全部自定义POC
function deselectAllCustomPocs() {
  selectedCustomPocIds.value = []
  selectedCustomPocs.value = []
  if (customPocTableRef.value) {
    customPocTableRef.value.clearSelection()
  }
}

// 清空所有选择
function clearAllSelections() {
  clearNucleiSelections()
  clearCustomPocSelections()
}

// 清空Nuclei选择
function clearNucleiSelections() {
  selectedNucleiTemplateIds.value = []
  selectedNucleiTemplates.value = []
  if (nucleiTableRef.value) {
    nucleiTableRef.value.clearSelection()
  }
}

// 清空自定义POC选择
function clearCustomPocSelections() {
  selectedCustomPocIds.value = []
  selectedCustomPocs.value = []
  if (customPocTableRef.value) {
    customPocTableRef.value.clearSelection()
  }
}

// 移除单个Nuclei模板
function removeNucleiTemplate(id) {
  const index = selectedNucleiTemplateIds.value.indexOf(id)
  if (index > -1) {
    selectedNucleiTemplateIds.value.splice(index, 1)
    selectedNucleiTemplates.value = selectedNucleiTemplates.value.filter(t => t.id !== id)
    // 更新表格选择状态
    if (nucleiTableRef.value) {
      const row = nucleiTemplateList.value.find(r => r.id === id)
      if (row) {
        nucleiTableRef.value.toggleRowSelection(row, false)
      }
    }
  }
}

// 移除单个自定义POC
function removeCustomPoc(id) {
  const index = selectedCustomPocIds.value.indexOf(id)
  if (index > -1) {
    selectedCustomPocIds.value.splice(index, 1)
    selectedCustomPocs.value = selectedCustomPocs.value.filter(p => p.id !== id)
    // 更新表格选择状态
    if (customPocTableRef.value) {
      const row = customPocList.value.find(r => r.id === id)
      if (row) {
        customPocTableRef.value.toggleRowSelection(row, false)
      }
    }
  }
}

// 确认POC选择
function confirmPocSelection() {
  form.pocscanNucleiTemplateIds = [...selectedNucleiTemplateIds.value]
  form.pocscanCustomPocIds = [...selectedCustomPocIds.value]
  form.pocscanNucleiTemplates = [...selectedNucleiTemplates.value]
  form.pocscanCustomPocs = [...selectedCustomPocs.value]
  pocSelectDialogVisible.value = false
}

// 显示POC选择对话框
function showPocSelectDialog() {
  pocSelectDialogVisible.value = true
}

// 查看POC内容
async function viewPocContent(poc, type) {
  pocContentLoading.value = true
  pocContentDialogVisible.value = true
  pocContentTitle.value = poc.name || poc.id
  currentViewPoc.value = { ...poc, content: '' }
  
  try {
    if (type === 'nuclei') {
      const res = await getNucleiTemplateDetail({ id: poc.id })
      if (res.code === 0) {
        currentViewPoc.value = { ...poc, ...res.data, content: res.data?.content || '' }
      }
    } else {
      // 自定义POC已经有content字段
      currentViewPoc.value = { ...poc, content: poc.content || '' }
    }
  } catch (error) {
    console.error('获取POC内容失败:', error)
  } finally {
    pocContentLoading.value = false
  }
}

// 复制POC内容
function copyPocContent() {
  if (currentViewPoc.value.content) {
    navigator.clipboard.writeText(currentViewPoc.value.content)
    ElMessage.success(t('common.copySuccess') || '复制成功')
  }
}

// 获取严重等级标签类型
function getSeverityType(severity) {
  const map = {
    critical: 'danger',
    high: 'warning',
    medium: '',
    low: 'info',
    info: 'info',
    unknown: 'info'
  }
  return map[severity] || 'info'
}

// POC模式变化处理
function handlePocModeChange(mode) {
  if (mode === 'manual') {
    // 切换到手动模式时，打开选择对话框
    showPocSelectDialog()
  }
}

// 构建扫描配置对象
function buildConfig() {
  return {
    batchSize: form.batchSize,
    domainscan: {
      enable: form.domainscanEnable,
      subfinder: form.domainscanSubfinder,
      bruteforce: form.domainscanBruteforce,
      bruteforceTimeout: form.domainscanBruteforceTimeout,
      timeout: form.domainscanTimeout,
      maxEnumTime: form.domainscanMaxEnumTime,
      threads: form.domainscanThreads,
      rateLimit: form.domainscanRateLimit,
      removeWildcard: form.domainscanRemoveWildcard,
      resolveDNS: form.domainscanResolveDNS,
      concurrent: form.domainscanConcurrent,
      subdomainDictIds: form.subdomainDictIds || [],
      recursiveBrute: form.domainscanRecursiveBrute,
      recursiveDictIds: form.recursiveDictIds || [],
      wildcardDetect: form.domainscanWildcardDetect
    },
    portscan: {
      enable: form.portscanEnable,
      tool: form.portscanTool,
      rate: form.portscanRate,
      ports: form.ports,
      portThreshold: form.portThreshold,
      scanType: form.scanType,
      timeout: form.portscanTimeout,
      skipHostDiscovery: form.skipHostDiscovery,
      excludeCDN: form.excludeCDN,
      excludeHosts: form.excludeHosts,
      workers: form.portscanWorkers,
      retries: form.portscanRetries,
      warmUpTime: form.portscanWarmUpTime,
      verify: form.portscanVerify
    },
    portidentify: {
      enable: form.portidentifyEnable,
      tool: form.portidentifyTool,
      timeout: form.portidentifyTimeout,
      concurrency: form.portidentifyConcurrency,
      args: form.portidentifyArgs,
      udp: form.portidentifyUDP,
      fastMode: form.portidentifyFastMode,
      forceScan: form.portidentifyForceScan && !form.portscanEnable
    },
    fingerprint: {
      enable: form.fingerprintEnable,
      tool: form.fingerprintTool,
      iconHash: form.fingerprintIconHash,
      customEngine: form.fingerprintCustomEngine,
      screenshot: form.fingerprintScreenshot,
      activeScan: form.fingerprintActiveScan,
      activeTimeout: form.fingerprintActiveTimeout,
      timeout: form.fingerprintTimeout,
      filterMode: form.fingerprintFilterMode,
      forceScan: form.fingerprintForceScan && !form.portscanEnable && !form.portidentifyEnable
    },
    pocscan: {
      enable: form.pocscanEnable,
      mode: form.pocscanMode,
      forceScan: form.pocscanForceScan && !hasPrePhaseEnabled.value,
      autoScan: form.pocscanAutoScan,
      automaticScan: form.pocscanAutomaticScan,
      customOnly: form.pocscanCustomOnly,
      severity: form.pocscanSeverity,
      targetTimeout: form.pocscanTargetTimeout,
      nucleiTemplateIds: form.pocscanNucleiTemplateIds || [],
      customPocIds: form.pocscanCustomPocIds || []
    },
    dirscan: {
      enable: form.dirscanEnable,
      dictIds: form.dirscanDictIds || [],
      threads: form.dirscanThreads,
      timeout: form.dirscanTimeout,
      statusCodes: form.dirscanStatusCodes,
      followRedirect: form.dirscanFollowRedirect,
      forceScan: form.dirscanForceScan && !hasPrePhaseEnabled.value
    }
  }
}

// 从配置对象应用到表单
function applyConfig(config) {
  if (!config) return
  
  // 高级设置
  if (config.batchSize !== undefined) form.batchSize = config.batchSize
  
  // 子域名扫描
  if (config.domainscan) {
    const ds = config.domainscan
    form.domainscanEnable = ds.enable ?? false
    form.domainscanSubfinder = ds.subfinder ?? true
    form.domainscanBruteforce = ds.bruteforce ?? false
    form.domainscanBruteforceTimeout = ds.bruteforceTimeout ?? 30
    form.domainscanTimeout = ds.timeout ?? 300
    form.domainscanMaxEnumTime = ds.maxEnumTime ?? 10
    form.domainscanThreads = ds.threads ?? 10
    form.domainscanRateLimit = ds.rateLimit ?? 0
    form.domainscanRemoveWildcard = ds.removeWildcard ?? true
    form.domainscanResolveDNS = ds.resolveDNS ?? true
    form.domainscanConcurrent = ds.concurrent ?? 50
    form.subdomainDictIds = ds.subdomainDictIds || []
    form.domainscanRecursiveBrute = ds.recursiveBrute ?? false
    form.recursiveDictIds = ds.recursiveDictIds || []
    form.domainscanWildcardDetect = ds.wildcardDetect ?? true
  }
  
  // 端口扫描
  if (config.portscan) {
    const ps = config.portscan
    form.portscanEnable = ps.enable ?? true
    form.portscanTool = ps.tool || 'naabu'
    form.portscanRate = ps.rate ?? 3000
    form.ports = ps.ports || 'top100'
    form.portThreshold = ps.portThreshold ?? 100
    form.scanType = ps.scanType || 'c'
    form.portscanTimeout = ps.timeout ?? 60
    form.skipHostDiscovery = ps.skipHostDiscovery ?? false
    form.excludeCDN = ps.excludeCDN ?? false
    form.excludeHosts = ps.excludeHosts || ''
    form.portscanWorkers = ps.workers ?? 50
    form.portscanRetries = ps.retries ?? 2
    form.portscanWarmUpTime = ps.warmUpTime ?? 1
    form.portscanVerify = ps.verify ?? false
  }
  
  // 端口识别
  if (config.portidentify) {
    const pi = config.portidentify
    form.portidentifyEnable = pi.enable ?? false
    form.portidentifyTool = pi.tool || 'nmap'
    form.portidentifyTimeout = pi.timeout ?? 30
    form.portidentifyConcurrency = pi.concurrency ?? 10
    form.portidentifyArgs = pi.args || ''
    form.portidentifyUDP = pi.udp ?? false
    form.portidentifyFastMode = pi.fastMode ?? false
  }
  
  // 指纹识别
  if (config.fingerprint) {
    const fp = config.fingerprint
    form.fingerprintEnable = fp.enable ?? true
    form.fingerprintTool = fp.tool || 'httpx'
    form.fingerprintIconHash = fp.iconHash ?? true
    form.fingerprintCustomEngine = fp.customEngine ?? false
    form.fingerprintScreenshot = fp.screenshot ?? false
    form.fingerprintActiveScan = fp.activeScan ?? false
    form.fingerprintActiveTimeout = fp.activeTimeout ?? 10
    form.fingerprintTimeout = fp.timeout ?? 30
    form.fingerprintFilterMode = fp.filterMode || 'http_mapping'
  }
  
  // 漏洞扫描
  if (config.pocscan) {
    const poc = config.pocscan
    form.pocscanEnable = poc.enable ?? false
    form.pocscanMode = poc.mode || 'auto'
    form.pocscanAutoScan = poc.autoScan ?? true
    form.pocscanAutomaticScan = poc.automaticScan ?? true
    form.pocscanCustomOnly = poc.customOnly ?? false
    form.pocscanSeverity = poc.severity || ['critical', 'high', 'medium']
    form.pocscanTargetTimeout = poc.targetTimeout ?? 600
    form.pocscanNucleiTemplateIds = poc.nucleiTemplateIds || []
    form.pocscanCustomPocIds = poc.customPocIds || []
    // 清空已选择的对象列表，后续可按需加载
    selectedNucleiTemplates.value = []
    selectedCustomPocs.value = []
  }
  
  // 目录扫描
  if (config.dirscan) {
    const dir = config.dirscan
    form.dirscanEnable = dir.enable ?? false
    form.dirscanDictIds = dir.dictIds || []
    form.dirscanThreads = dir.threads ?? 50
    form.dirscanTimeout = dir.timeout ?? 10
    form.dirscanStatusCodes = dir.statusCodes || [200, 301, 302, 401, 403]
    form.dirscanFollowRedirect = dir.followRedirect ?? false
  }
}

// 重置扫描配置为默认值
function resetScanConfig() {
  // 子域名扫描
  form.domainscanEnable = false
  form.domainscanSubfinder = true
  form.domainscanBruteforce = false
  form.domainscanBruteforceTimeout = 30
  form.domainscanTimeout = 300
  form.domainscanMaxEnumTime = 10
  form.domainscanThreads = 10
  form.domainscanRateLimit = 0
  form.domainscanRemoveWildcard = true
  form.domainscanResolveDNS = true
  form.domainscanConcurrent = 50
  form.subdomainDictIds = []
  form.subdomainDicts = []
  form.domainscanRecursiveBrute = false
  form.recursiveDictIds = []
  form.recursiveDicts = []
  form.domainscanWildcardDetect = true
  // 端口扫描
  form.portscanEnable = true
  form.portscanTool = 'naabu'
  form.portscanRate = 3000
  form.ports = 'top100'
  form.portThreshold = 100
  form.scanType = 'c'
  form.portscanTimeout = 60
  form.skipHostDiscovery = false
  form.excludeCDN = false
  form.excludeHosts = ''
  form.portscanWorkers = 50
  form.portscanRetries = 2
  form.portscanWarmUpTime = 1
  form.portscanVerify = false
  // 端口识别
  form.portidentifyEnable = false
  form.portidentifyTool = 'nmap'
  form.portidentifyTimeout = 30
  form.portidentifyConcurrency = 10
  form.portidentifyArgs = ''
  form.portidentifyUDP = false
  form.portidentifyFastMode = false
  // 指纹识别
  form.fingerprintEnable = true
  form.fingerprintTool = 'httpx'
  form.fingerprintIconHash = true
  form.fingerprintCustomEngine = false
  form.fingerprintScreenshot = false
  form.fingerprintActiveScan = false
  form.fingerprintActiveTimeout = 10
  form.fingerprintTimeout = 30
  form.fingerprintFilterMode = 'http_mapping'
  // 漏洞扫描
  form.pocscanEnable = false
  form.pocscanMode = 'auto'
  form.pocscanAutoScan = true
  form.pocscanAutomaticScan = true
  form.pocscanCustomOnly = false
  form.pocscanSeverity = ['critical', 'high', 'medium']
  form.pocscanTargetTimeout = 600
  form.pocscanNucleiTemplateIds = []
  form.pocscanCustomPocIds = []
  selectedNucleiTemplates.value = []
  selectedCustomPocs.value = []
  // 目录扫描
  form.dirscanEnable = false
  form.dirscanDictIds = []
  form.dirscanDicts = []
  form.dirscanThreads = 50
  form.dirscanTimeout = 10
  form.dirscanStatusCodes = [200, 301, 302, 401, 403]
  form.dirscanFollowRedirect = false
  // 高级设置
  form.batchSize = 50
}

onMounted(() => {
  loadData()
})
</script>

<style scoped>
.cron-task-page {
  padding: 20px;
}

.action-card {
  margin-bottom: 20px;
}

.table-card {
  margin-bottom: 20px;
}

.pagination {
  margin-top: 20px;
  justify-content: flex-end;
}

.cron-code {
  background: var(--el-fill-color-light);
  padding: 2px 6px;
  border-radius: 4px;
  font-family: monospace;
  font-size: 12px;
  margin-left: 6px;
}

.schedule-time {
  font-size: 12px;
  color: var(--el-text-color-regular);
  margin-left: 6px;
}

.text-muted {
  color: var(--el-text-color-placeholder);
}

.task-link {
  color: var(--el-color-primary);
  cursor: pointer;
}

.task-link:hover {
  text-decoration: underline;
}

.task-option {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.task-option .task-name {
  flex: 1;
}

.task-option .task-target {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selected-task-info {
  margin-top: 8px;
}

.cron-help {
  margin-top: 10px;
}

.cron-presets {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}

.preset-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.preset-tag {
  cursor: pointer;
}

.preset-tag:hover {
  background: var(--el-color-primary-light-7);
}

.cron-next-times {
  background: var(--el-fill-color-lighter);
  padding: 10px;
  border-radius: 4px;
  font-size: 12px;
}

.next-label {
  color: var(--el-text-color-secondary);
  margin-bottom: 5px;
}

.next-time {
  color: var(--el-text-color-regular);
  line-height: 1.8;
}

.cron-error {
  color: var(--el-color-danger);
  font-size: 12px;
}

.form-hint {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  margin-top: 5px;
}

/* 扫描配置折叠面板样式 */
.config-collapse {
  margin-top: 20px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
}

.collapse-title {
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 8px;
}

.scan-tools-layout {
  margin-top: 10px;
}

.scan-tool-section {
  background: var(--el-fill-color-lighter);
  border-radius: 6px;
  padding: 15px;
  min-height: 200px;
}

.scan-tool-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 15px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.scan-tool-title {
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.scan-tool-disabled-hint {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--el-text-color-placeholder);
  padding: 20px;
  justify-content: center;
}

.selected-dict-summary {
  display: flex;
  align-items: center;
  gap: 10px;
}

.selected-poc-summary {
  display: flex;
  align-items: center;
  gap: 10px;
}

.warning-hint {
  color: var(--el-color-warning);
  font-size: 12px;
}

.secondary-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

/* POC选择对话框样式 */
.poc-select-container {
  display: flex;
  gap: 20px;
  height: 500px;
}

.poc-select-left {
  flex: 1;
  overflow: auto;
}

.poc-select-right {
  width: 280px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
  display: flex;
  flex-direction: column;
}

.selected-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 15px;
  border-bottom: 1px solid var(--el-border-color-light);
  font-weight: 500;
}

.selected-search {
  padding: 10px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.selected-list {
  flex: 1;
  overflow-y: auto;
  padding: 10px;
}

.selected-group {
  margin-bottom: 15px;
}

.group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 5px 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.selected-items {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}

.selected-item {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  background: var(--el-fill-color-light);
  border-radius: 3px;
  font-size: 12px;
  max-width: 100%;
}

.item-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 200px;
}

.item-remove {
  cursor: pointer;
  color: var(--el-text-color-placeholder);
  flex-shrink: 0;
}

.item-remove:hover {
  color: var(--el-color-danger);
}

.selected-empty {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100px;
  color: var(--el-text-color-placeholder);
}

.poc-filter-form {
  margin-bottom: 10px;
}

.poc-pagination {
  margin-top: 10px;
  justify-content: flex-end;
}

.poc-content-wrapper {
  min-height: 300px;
}
</style>
