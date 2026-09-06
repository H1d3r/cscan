<template>
  <div class="cron-task-create-page">
    <!-- 页面头部 -->
    <div class="page-header">
      <el-button link @click="handleBack">
        <el-icon>
          <ArrowLeft />
        </el-icon>{{ $t('common.back') || '返回' }}
      </el-button>
      <h2 class="page-title">{{ isEdit ? ($t('cronTask.editCronTask') || '编辑定时扫描') : ($t('cronTask.newCronTask') ||
        '新建定时扫描') }}</h2>
    </div>

    <el-card class="create-card">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px" class="cron-task-form">
        <!-- 基本信息 -->
        <el-form-item :label="$t('cronTask.cronTaskName')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('cronTask.pleaseEnterName')" />
        </el-form-item>

        <!-- 目标选择区域 -->
        <el-form-item :label="$t('cronTask.scanTarget')" required>
          <el-radio-group v-model="form.targetMode" class="target-mode-switch">
            <el-radio-button label="manual">{{ $t('cronTask.targetSourceManual') }}</el-radio-button>
            <el-radio-button label="asset">{{ $t('cronTask.targetSourceAsset') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <!-- 手动输入模式 -->
        <el-form-item v-if="form.targetMode === 'manual'" :label="$t('cronTask.targetContent')" prop="target">
          <el-input v-model="form.target" type="textarea" :rows="4" :placeholder="$t('cronTask.targetPlaceholder')" />
          <div class="form-hint">{{ $t('cronTask.targetHint') }}</div>
        </el-form-item>

        <!-- 选择资产模式 -->
        <el-form-item v-if="form.targetMode === 'asset'" :label="$t('cronTask.targetSourceAsset')" prop="assetIds">
          <div class="asset-selector">
            <!-- 组织筛选 + 全选按钮 -->
            <div class="asset-selector-toolbar">
              <el-select v-model="form.orgId" :placeholder="$t('cronTask.allOrgs')" clearable filterable
                style="width: 240px" @change="loadAssetTargets">
                <el-option v-for="org in organizationList" :key="org.id" :label="org.name" :value="org.id" />
              </el-select>
              <el-input v-model="assetTargetFilter.keyword" :placeholder="$t('cronTask.searchIpDomainTag')" clearable
                style="width: 240px; margin-left: 10px" @keyup.enter="loadAssetTargets" @clear="loadAssetTargets">
                <template #append>
                  <el-button @click="loadAssetTargets">{{ $t('common.search') }}</el-button>
                </template>
              </el-input>
              <el-button type="primary" link style="margin-left: 10px" @click="selectAllAssets">{{
                $t('cronTask.selectAll')
              }}</el-button>
              <el-button type="warning" link @click="clearAssetSelection" v-if="form.assetIds.length > 0">{{
                $t('cronTask.clearSelection') }}</el-button>
              <span class="selected-count-hint">{{ $t('cronTask.selectedCount', { count: form.assetIds.length })
              }}</span>
            </div>
            <!-- 资产列表表格 -->
            <el-table ref="assetTargetTableRef" :data="assetTargetList" v-loading="assetTargetLoading" max-height="320"
              border class="asset-target-table" @selection-change="handleAssetSelectionChange" row-key="id">
              <el-table-column type="selection" width="45" :reserve-selection="true" />
              <el-table-column prop="type" :label="$t('cronTask.type')" width="80">
                <template #default="{ row }">
                  <el-tag :type="row.type === 'domain' ? 'success' : 'primary'" size="small">
                    {{ row.type === 'domain' ? $t('cronTask.domain') : $t('cronTask.ip') }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="value" :label="$t('cronTask.value')" min-width="180" show-overflow-tooltip />
              <el-table-column prop="labels" :label="$t('cronTask.tag')" min-width="160" show-overflow-tooltip>
                <template #default="{ row }">
                  <el-tag v-for="(label, idx) in (row.labels || []).slice(0, 3)" :key="idx" size="small" type="info"
                    class="asset-label-tag">
                    {{ label }}
                  </el-tag>
                  <span v-if="row.labels && row.labels.length > 3" class="secondary-hint">+{{ row.labels.length - 3
                  }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="lastScanTime" :label="$t('cronTask.lastScanTime')" width="170">
                <template #default="{ row }">
                  {{ formatAssetTimestamp(row.lastScanTime) }}
                </template>
              </el-table-column>
            </el-table>
            <!-- 分页 -->
            <el-pagination v-model:current-page="assetTargetPagination.page"
              v-model:page-size="assetTargetPagination.pageSize" :total="assetTargetPagination.total"
              :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" class="asset-pagination" small
              @size-change="loadAssetTargets" @current-change="loadAssetTargets" />
          </div>
        </el-form-item>

        <!-- 同步拉取所有子域名 -->
        <el-form-item v-if="form.targetMode === 'asset'" :label="$t('cronTask.subdomainSync')">
          <el-checkbox v-model="form.enableSubdomainPull">{{ $t('cronTask.syncAllSubdomains') }}</el-checkbox>
          <div class="form-hint">{{ $t('cronTask.subdomainSyncHint') }}</div>
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
              <el-tag v-for="preset in cronPresets" :key="preset.value" size="small" class="preset-tag"
                @click="form.cronSpec = preset.value; validateCron()">
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
          <el-date-picker v-model="form.scheduleTimeDate" type="datetime" :placeholder="$t('common.pleaseSelect')"
            format="YYYY-MM-DD HH:mm:ss" value-format="YYYY-MM-DD HH:mm:ss" :disabled-date="disabledDate"
            style="width: 100%" @change="onScheduleTimeChange" />
          <div class="form-hint">{{ $t('cronTask.onceExecHint') }}</div>
        </el-form-item>

        <!-- 扫描配置来源 -->
        <el-form-item :label="$t('cronTask.scanConfig')" required>
          <el-radio-group v-model="form.configSource" class="config-source-switch">
            <el-radio-button label="template">{{ $t('cronTask.scanTemplate') }}</el-radio-button>
            <el-radio-button label="custom">{{ $t('cronTask.customConfig') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <!-- 扫描模板选择 -->
        <el-form-item v-if="form.configSource === 'template'" :label="$t('cronTask.selectTemplate')" prop="templateId">
          <el-select v-model="form.templateId" :placeholder="$t('cronTask.selectTemplatePlaceholder')"
            style="width: 100%" filterable @change="onTemplateSelect">
            <el-option v-for="tpl in scanTemplateList" :key="tpl.id" :label="tpl.name" :value="tpl.id">
              <div class="template-option">
                <span class="template-name">{{ tpl.name }}</span>
                <span v-if="tpl.category" class="template-category">{{ tpl.category }}</span>
              </div>
            </el-option>
          </el-select>
          <div class="form-hint" v-if="form.templateId">
            {{ $t('cronTask.templateSelectedHint') }}
            <el-button type="primary" link size="small" @click="previewTemplateConfig" v-if="selectedTemplateConfig">{{
              $t('cronTask.viewConfig') }}</el-button>
          </div>
        </el-form-item>

        <!-- 扫描配置折叠面板（自定义配置模式） -->
        <el-collapse v-if="form.configSource === 'custom'" v-model="activeCollapse" class="config-collapse">
          <!-- 子域名扫描 -->
          <el-collapse-item name="domainscan">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.subdomainScan') }}</span>
                <span v-if="form.domainscanEnable" class="config-summary">{{ domainscanSummary }}</span>
                <el-switch v-model="form.domainscanEnable" size="small" @click.stop />
              </div>
            </template>
            <p class="module-desc">{{ $t('task.subdomainEnumHint') }}</p>
            <template v-if="form.domainscanEnable">
              <el-form-item :label="$t('task.scanTool')">
                <el-checkbox v-model="form.domainscanSubfinder">Subfinder ({{ $t('task.passiveEnum') }})</el-checkbox>
                <el-checkbox v-model="form.domainscanBruteforce"
                  :disabled="!form.subdomainDictIds || !form.subdomainDictIds.length">KSubdomain ({{
                    $t('task.dictBrute') }})</el-checkbox>
                <span class="form-hint">{{ $t('task.multiScanHint') }}</span>
              </el-form-item>

              <!-- 左右分栏布局 -->
              <el-row :gutter="24" class="scan-tools-layout">
                <!-- 左侧：Subfinder 配置 -->
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
                      <el-form-item :label="$t('task.rateLimit')">
                        <el-input-number v-model="form.domainscanRateLimit" :min="0" :max="1000" style="width:100%" />
                        <span class="form-hint">0={{ $t('task.noLimit') }}</span>
                      </el-form-item>
                      <el-form-item :label="$t('task.scanOptions')">
                        <el-checkbox v-model="form.domainscanRemoveWildcard">{{ $t('task.removeWildcardDomain')
                        }}</el-checkbox>
                      </el-form-item>
                      <el-form-item :label="$t('task.dnsResolve')">
                        <el-checkbox v-model="form.domainscanResolveDNS">{{ $t('task.resolveSubdomainDns')
                        }}</el-checkbox>
                        <span class="form-hint">{{ $t('task.concurrentByWorker') }}</span>
                      </el-form-item>
                    </template>
                    <div v-else class="scan-tool-disabled-hint">
                      <el-icon>
                        <InfoFilled />
                      </el-icon>
                      <span>{{ $t('task.enableSubfinderFirst') }}</span>
                    </div>
                  </div>
                </el-col>

                <!-- 右侧：KSubdomain 配置 -->
                <el-col :span="12">
                  <div class="scan-tool-section">
                    <div class="scan-tool-header">
                      <span class="scan-tool-title">{{ $t('task.ksubdomainDictBrute') }}</span>
                      <el-tag :type="form.domainscanBruteforce ? 'success' : 'info'" size="small">
                        {{ form.domainscanBruteforce ? $t('task.started') : $t('task.notStarted') }}
                      </el-tag>
                    </div>
                    <!-- 字典选择（始终显示，作为启用字典爆破的前提） -->
                    <el-form-item :label="$t('task.bruteforceDict')">
                      <div class="selected-dict-summary">
                        <el-tag type="primary" size="small"
                          v-if="form.subdomainDictIds && form.subdomainDictIds.length">
                          {{ $t('task.selectedCount', { count: form.subdomainDictIds.length }) }}
                        </el-tag>
                        <span v-else class="warning-hint">
                          {{ $t('task.selectDictFirst') }}
                        </span>
                        <el-button type="primary" link @click="showSubdomainDictSelectDialog">{{ $t('task.selectDict')
                        }}</el-button>
                      </div>
                      <span class="form-hint">{{ $t('task.ksubdomainBruteHint') }}</span>
                    </el-form-item>
                    <template v-if="form.domainscanBruteforce">
                      <el-form-item :label="$t('task.bruteforceTimeout') + ' (' + $t('task.minutes') + ')'">
                        <el-input-number v-model="form.domainscanBruteforceTimeout" :min="1" :max="120"
                          style="width:100%" />
                        <span class="form-hint">{{ $t('task.ksubdomainTimeoutHint') }}</span>
                      </el-form-item>
                    </template>
                    <template v-if="form.domainscanBruteforce">
                      <el-form-item :label="$t('task.enhancedFeatures')">
                        <div style="display: flex; flex-direction: column; gap: 8px;">
                          <div style="display: flex; align-items: center; gap: 8px;">
                            <el-checkbox v-model="form.domainscanRecursiveBrute"
                              :disabled="!form.recursiveDictIds || !form.recursiveDictIds.length">{{
                                $t('task.recursiveBrute') }}</el-checkbox>
                            <el-button type="primary" link size="small" @click="showRecursiveDictSelectDialog">{{
                              $t('task.selectRecursiveDict') }}</el-button>
                            <el-tag type="primary" size="small"
                              v-if="form.recursiveDictIds && form.recursiveDictIds.length">
                              {{ $t('task.selectedCount', { count: form.recursiveDictIds.length }) }}
                            </el-tag>
                          </div>
                          <span class="form-hint" style="margin-left: 24px; margin-top: -4px;">
                            {{ (!form.recursiveDictIds || !form.recursiveDictIds.length) ?
                              $t('task.selectRecursiveDictFirst') : $t('task.recursiveBruteHint') }}
                          </span>
                          <el-checkbox v-model="form.domainscanWildcardDetect">{{ $t('task.wildcardDetect')
                          }}</el-checkbox>
                          <span class="form-hint" style="margin-left: 24px; margin-top: -4px;">{{
                            $t('task.wildcardDetectHint') }}</span>


                        </div>
                      </el-form-item>
                    </template>
                    <div v-if="!form.domainscanBruteforce && form.subdomainDictIds && form.subdomainDictIds.length"
                      class="scan-tool-disabled-hint">
                      <el-icon>
                        <InfoFilled />
                      </el-icon>
                      <span>{{ $t('task.canEnableKSubdomain') }}</span>
                    </div>
                  </div>
                </el-col>
              </el-row>
            </template>
          </el-collapse-item>

          <!-- 端口扫描 -->
          <el-collapse-item name="portscan">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.portScan') }}</span>
                <span v-if="form.portscanEnable" class="config-summary">{{ portscanSummary }}</span>
                <el-switch v-model="form.portscanEnable" size="small" @click.stop />
              </div>
            </template>
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
                    <span class="form-hint">{{ $t('task.packetsPerSecond') }}</span>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.portThreshold')">
                    <el-input-number v-model="form.portThreshold" :min="0" :max="65535" style="width:100%" />
                    <span class="form-hint">{{ $t('task.skipIfExceeded') }}</span>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-row :gutter="20" v-if="form.portscanTool === 'naabu'">
                <el-col :span="12">
                  <el-form-item :label="$t('task.workers')">
                    <el-input-number v-model="form.portscanWorkers" :min="10" :max="200" style="width:100%" />
                    <span class="form-hint">{{ $t('task.internalThreads') }}</span>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.retries')">
                    <el-input-number v-model="form.portscanRetries" :min="0" :max="5" style="width:100%" />
                    <span class="form-hint">{{ $t('task.retryCount') }}</span>
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
              <el-row :gutter="20" v-if="form.portscanTool === 'naabu'">
                <el-col :span="12">
                  <el-form-item :label="$t('task.warmUpTime')">
                    <el-input-number v-model="form.portscanWarmUpTime" :min="0" :max="10" style="width:100%" />
                    <span class="form-hint">{{ $t('task.warmUpTimeHint') }}</span>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.tcpVerify')">
                    <el-switch v-model="form.portscanVerify" />
                    <span class="form-hint">{{ $t('task.tcpVerifyHint') }}</span>
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
          </el-collapse-item>

          <!-- 端口识别 -->
          <el-collapse-item name="portidentify">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.portIdentify') }}</span>
                <span v-if="form.portidentifyEnable" class="config-summary">{{ portidentifySummary }}</span>
                <el-switch v-model="form.portidentifyEnable" size="small" @click.stop />
              </div>
            </template>
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
          </el-collapse-item>

          <!-- 指纹识别 -->
          <el-collapse-item name="fingerprint">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.fingerprintScan') }}</span>
                <span v-if="form.fingerprintEnable" class="config-summary">{{ fingerprintSummary }}</span>
                <el-switch v-model="form.fingerprintEnable" size="small" @click.stop />
              </div>
            </template>
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
                <span class="form-hint">{{ form.fingerprintTool === 'httpx' ? $t('task.httpxWappalyzer') :
                  $t('task.sdkWappalyzer') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.additionalFeatures')">
                <el-checkbox v-model="form.fingerprintIconHash">{{ $t('task.iconHash') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintCustomEngine">{{ $t('task.customFingerprint') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintScreenshot">{{ $t('task.screenshot') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintCert">{{ $t('task.cert') }}</el-checkbox>
              </el-form-item>
              <el-form-item :label="$t('task.filterMode')">
                <el-radio-group v-model="form.fingerprintFilterMode">
                  <el-radio label="http_mapping">{{ $t('task.httpMappingMode') }}</el-radio>
                  <el-radio label="service_mapping">{{ $t('task.serviceMappingMode') }}</el-radio>
                </el-radio-group>
                <span class="form-hint">{{ form.fingerprintFilterMode === 'http_mapping' ?
                  $t('task.httpMappingModeHint') : $t('task.serviceMappingModeHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.activeScan')">
                <el-checkbox v-model="form.fingerprintActiveScan">{{ $t('task.enableActiveScan') }}</el-checkbox>
                <span class="form-hint">{{ $t('task.activeScanHint') }}</span>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item :label="$t('task.timeoutSeconds')">
                    <el-input-number v-model="form.fingerprintTimeout" :min="5" :max="120" style="width:100%" />
                    <span class="form-hint">{{ $t('task.concurrentByWorker') }}</span>
                  </el-form-item>
                </el-col>
                <el-col :span="12" v-if="form.fingerprintActiveScan">
                  <el-form-item :label="$t('task.activeTimeoutSeconds')">
                    <el-input-number v-model="form.fingerprintActiveTimeout" :min="5" :max="60" style="width:100%" />
                    <span class="form-hint">{{ $t('task.activeProbeTimeout') }}</span>
                  </el-form-item>
                </el-col>
              </el-row>
            </template>
          </el-collapse-item>

          <!-- 弱口令扫描 -->
          <el-collapse-item name="brutescan">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.weakpassScan') }}</span>
                <span v-if="form.brutescanEnable" class="config-summary">{{ brutescanSummary }}</span>
                <el-switch v-model="form.brutescanEnable" size="small" @click.stop />
              </div>
            </template>
            <p class="module-desc">{{ $t('task.weakpassScanHint') }}</p>
            <template v-if="form.brutescanEnable">
              <el-form-item v-if="!hasPrePhaseEnabled" :label="$t('task.forceScan')">
                <el-switch v-model="form.brutescanForceScan" />
                <span class="form-hint warning-hint">{{ $t('task.forceScanHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.targetService')">
                <el-checkbox-group v-model="form.brutescanServices" class="service-checkbox-group">
                  <el-row :gutter="16">
                    <el-col :span="6" v-for="service in bruteServiceOptions" :key="service.value">
                      <el-checkbox :label="service.value" :value="service.value">
                        {{ service.label }}
                      </el-checkbox>
                    </el-col>
                  </el-row>
                </el-checkbox-group>
                <span class="form-hint">{{ $t('task.weakpassServiceHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.scanStrategy')">
                <el-checkbox v-model="form.brutescanStopOnFirst">{{ $t('task.stopOnFirstFound') }}</el-checkbox>
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 目录扫描 -->
          <el-collapse-item name="dirscan">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.dirScan') }}</span>
                <span v-if="form.dirscanEnable" class="config-summary">{{ dirscanSummary }}</span>
                <el-switch v-model="form.dirscanEnable" size="small" @click.stop />
              </div>
            </template>
            <p class="module-desc">{{ $t('task.dirScanHint') }}</p>
            <template v-if="form.dirscanEnable">
              <el-form-item :label="$t('task.scanTool')">
                <el-radio-group v-model="form.dirscanTool">
                  <el-radio label="ffuf">ffuf ({{ $t('task.recommended') }})</el-radio>
                  <el-radio label="feroxbuster">Feroxbuster</el-radio>
                </el-radio-group>
              </el-form-item>
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
                  <span v-if="!form.dirscanDictIds.length" class="secondary-hint">
                    {{ $t('task.noDictSelected') }}
                  </span>
                  <el-button type="primary" link @click="showDictSelectDialog">{{ $t('task.selectDict') }}</el-button>
                </div>
              </el-form-item>
              <el-form-item :label="$t('task.followRedirect')">
                <el-switch v-model="form.dirscanFollowRedirect" />
              </el-form-item>
              <el-form-item prop="dirscanStatusCodes" :label="$t('task.statusCodes')">
                <el-select v-model="form.dirscanStatusCodes" multiple filterable allow-create default-first-option
                  style="width:100%" :placeholder="$t('task.statusCodesPlaceholder')">
                  <el-option v-for="code in commonStatusCodes" :key="code" :label="String(code)" :value="code" />
                </el-select>
                <span class="form-hint">{{ $t('task.statusCodesHint') }}</span>
              </el-form-item>
              <!-- ffuf 高级配置 -->
              <el-divider content-position="left">{{ $t('task.ffufAdvanced') }}</el-divider>
              <el-form-item :label="$t('task.autoCalibration')">
                <el-switch v-model="form.dirscanAutoCalibration" />
                <span class="form-hint">{{ $t('task.autoCalibrationHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.recursion')">
                <el-switch v-model="form.dirscanRecursion" />
              </el-form-item>
              <el-form-item v-if="form.dirscanRecursion" :label="$t('task.recursionDepth')">
                <el-input-number v-model="form.dirscanRecursionDepth" :min="1" :max="10" style="width:100%" />
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- JS扫描 -->
          <el-collapse-item name="jsfinder">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.jsfinderScan') }}</span>
                <span v-if="form.jsfinderEnable" class="config-summary">{{ jsfinderSummary }}</span>
                <el-switch v-model="form.jsfinderEnable" size="small" @click.stop />
              </div>
            </template>
            <p class="module-desc">{{ $t('task.jsfinderScanHint') }}</p>
            <template v-if="form.jsfinderEnable">
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item :label="$t('task.concurrentThreads')">
                    <el-input-number v-model="form.jsfinderThreads" :min="1" :max="100" style="width:100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item :label="$t('task.requestTimeoutSeconds')">
                    <el-input-number v-model="form.jsfinderTimeout" :min="1" :max="60" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item :label="$t('task.enableSourcemap')">
                <el-switch v-model="form.jsfinderEnableSourcemap" />
                <span class="form-hint">{{ $t('task.enableSourcemapHint') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.enableUnauthCheck')">
                <el-switch v-model="form.jsfinderEnableUnauthCheck" />
                <span class="form-hint">{{ $t('task.enableUnauthCheckHint') }}</span>
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 漏洞扫描 -->
          <el-collapse-item name="pocscan">
            <template #title>
              <div class="collapse-title-wrapper">
                <span class="collapse-title">{{ $t('task.vulScan') }}</span>
                <span v-if="form.pocscanEnable" class="config-summary">{{ pocscanSummary }}</span>
                <el-switch v-model="form.pocscanEnable" size="small" @click.stop />
              </div>
            </template>
            <p class="module-desc">{{ $t('task.useNucleiEngine') }}</p>
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

              <!-- 自动匹配模式 -->
              <template v-if="form.pocscanMode === 'auto'">
                <el-form-item :label="$t('task.autoScan')">
                  <el-checkbox v-model="form.pocscanAutoScan" :disabled="form.pocscanCustomOnly">{{
                    $t('task.customTagMapping') }}</el-checkbox>
                  <el-checkbox v-model="form.pocscanAutomaticScan"
                    :disabled="form.pocscanCustomOnly || !form.fingerprintEnable">{{ $t('task.webFingerprintAutoMatch')
                    }}</el-checkbox>
                  <span v-if="!form.fingerprintEnable && !form.pocscanCustomOnly" class="form-hint warning-hint">{{
                    $t('task.needFingerprintScan') }}</span>
                </el-form-item>
                <el-form-item :label="$t('task.customPoc')">
                  <el-checkbox v-model="form.pocscanCustomOnly">{{ $t('task.onlyUseCustomPoc') }}</el-checkbox>
                </el-form-item>
              </template>

              <!-- 手动选择模式 -->
              <template v-if="form.pocscanMode === 'manual'">
                <el-form-item :label="$t('task.selectedPoc')">
                  <div class="selected-poc-summary">
                    <el-tag type="primary" size="small" v-if="nucleiSelectAll">
                      {{ $t('task.defaultTemplate') }}: {{ $t('task.allSelectedCount', { count: nucleiSelectAllCount })
                      }}
                    </el-tag>
                    <el-tag type="primary" size="small" v-else-if="form.pocscanNucleiTemplateIds.length">
                      {{ $t('task.defaultTemplate') }}: {{ form.pocscanNucleiTemplateIds.length }}
                    </el-tag>
                    <el-tag type="warning" size="small" v-if="customPocSelectAll">
                      {{ $t('task.customPoc') }}: {{ $t('task.allSelectedCount', { count: customPocSelectAllCount }) }}
                    </el-tag>
                    <el-tag type="warning" size="small" v-else-if="form.pocscanCustomPocIds.length">
                      {{ $t('task.customPoc') }}: {{ form.pocscanCustomPocIds.length }}
                    </el-tag>
                    <span
                      v-if="!nucleiSelectAll && !customPocSelectAll && !form.pocscanNucleiTemplateIds.length && !form.pocscanCustomPocIds.length"
                      class="secondary-hint">
                      {{ $t('task.noPocSelected') }}
                    </span>
                    <el-button type="primary" link @click="showPocSelectDialog">{{ $t('task.selectPoc') }}</el-button>
                  </div>
                </el-form-item>
              </template>

              <el-form-item v-if="form.pocscanMode !== 'manual'" :label="$t('task.severityLevel')">
                <el-checkbox-group v-model="form.pocscanSeverity">
                  <el-checkbox label="critical">Critical</el-checkbox>
                  <el-checkbox label="high">High</el-checkbox>
                  <el-checkbox label="medium">Medium</el-checkbox>
                  <el-checkbox label="low">Low</el-checkbox>
                  <el-checkbox label="info">Info</el-checkbox>
                  <el-checkbox label="unknown">Unknown</el-checkbox>
                </el-checkbox-group>
              </el-form-item>
              <el-form-item :label="$t('cronTask.requestRate')">
                <el-input-number v-model="form.pocscanRateLimit" :min="1" :max="2000" />
              </el-form-item>
              <el-form-item :label="$t('cronTask.templateConcurrency')">
                <el-input-number v-model="form.pocscanConcurrency" :min="1" :max="500" />
              </el-form-item>
              <el-form-item :label="$t('task.targetTimeout')">
                <el-input-number v-model="form.pocscanTargetTimeout" :min="30" :max="600" />
                <span class="form-hint">{{ $t('task.seconds') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.customHeaders')">
                <el-radio-group v-model="form.pocscanHeaderMode" style="margin-bottom: 8px;">
                  <el-radio label="none">{{ $t('task.noCustomHeader') }}</el-radio>
                  <el-radio label="preset">{{ $t('task.presetUA') }}</el-radio>
                  <el-radio label="custom">{{ $t('task.customInput') }}</el-radio>
                </el-radio-group>
                <template v-if="form.pocscanHeaderMode === 'preset'">
                  <el-select v-model="form.pocscanPresetUA" :placeholder="$t('task.selectUA')" style="width: 100%;">
                    <el-option-group :label="$t('task.uaDesktop')">
                      <el-option label="Chrome (Windows)"
                        value="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36" />
                      <el-option label="Firefox (macOS)"
                        value="Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:123.0) Gecko/20100101 Firefox/123.0" />
                      <el-option label="Edge (Windows)"
                        value="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36 Edg/122.0.0.0" />
                    </el-option-group>
                    <el-option-group :label="$t('task.uaMobile')">
                      <el-option label="Safari (iPhone)"
                        value="Mozilla/5.0 (iPhone; CPU iPhone OS 17_3_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.3.1 Mobile/15E148 Safari/604.1" />
                      <el-option label="Chrome (Android)"
                        value="Mozilla/5.0 (Linux; Android 13; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Mobile Safari/537.36" />
                    </el-option-group>
                    <el-option-group :label="$t('task.uaSpider')">
                      <el-option label="Baiduspider"
                        value="Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)" />
                      <el-option label="Googlebot"
                        value="Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" />
                    </el-option-group>
                    <el-option-group :label="$t('task.uaApp')">
                      <el-option label="WeChat (Android)"
                        value="Mozilla/5.0 (Linux; Android 13; ALN-AL00 Build/HUAWEIALN-AL00; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/116.0.0.0 Mobile Safari/537.36 XWEB/1160065 MMWEBSDK/20231202 MicroMessenger/8.0.47.2560 WeChat/arm64 Weixin NetType/WIFI" />
                    </el-option-group>
                  </el-select>
                </template>
                <template v-if="form.pocscanHeaderMode === 'custom'">
                  <el-input v-model="form.pocscanCustomHeadersText" type="textarea" :rows="4"
                    :placeholder="$t('task.customHeadersPlaceholder')" />
                </template>
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 高级设置 -->
        </el-collapse>

        <!-- 操作按钮 -->
        <div class="form-actions">
          <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ isEdit ? ($t('common.save') || '保存')
            :
            ($t('common.confirm') || '确定') }}</el-button>
          <el-button @click="handleBack">{{ $t('common.cancel') || '取消' }}</el-button>
        </div>
      </el-form>
    </el-card>

    <!-- 目录扫描字典选择对话框 -->
    <el-dialog v-model="dictSelectDialogVisible" :title="$t('task.selectDirScanDict')" width="800px"
      @open="handleDictDialogOpen">
      <el-table ref="dictTableRef" :data="dictList" v-loading="dictLoading" max-height="400"
        @selection-change="handleDictSelectionChange" row-key="id">
        <el-table-column type="selection" width="45" :reserve-selection="true" />
        <el-table-column prop="name" :label="$t('task.dictName')" min-width="150" />
        <el-table-column prop="pathCount" :label="$t('task.pathCount')" width="100" />
        <el-table-column prop="isBuiltin" :label="$t('common.type')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">{{ row.isBuiltin ? $t('task.builtin') :
              $t('task.custom') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="dictSelectDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmDictSelection">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 子域名字典选择对话框 -->
    <el-dialog v-model="subdomainDictSelectDialogVisible" :title="$t('task.selectSubdomainDict')" width="800px"
      @open="handleSubdomainDictDialogOpen">
      <el-table ref="subdomainDictTableRef" :data="subdomainDictList" v-loading="subdomainDictLoading" max-height="400"
        @selection-change="handleSubdomainDictSelectionChange" row-key="id">
        <el-table-column type="selection" width="45" :reserve-selection="true" />
        <el-table-column prop="name" :label="$t('task.dictName')" min-width="150" />
        <el-table-column prop="wordCount" :label="$t('task.wordCount')" width="100" />
        <el-table-column prop="isBuiltin" :label="$t('common.type')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">{{ row.isBuiltin ? $t('task.builtin') :
              $t('task.custom') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="subdomainDictSelectDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmSubdomainDictSelection">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 递归爆破字典选择对话框 -->
    <el-dialog v-model="recursiveDictSelectDialogVisible" :title="$t('task.selectRecursiveDict')" width="800px"
      @open="handleRecursiveDictDialogOpen">
      <el-table ref="recursiveDictTableRef" :data="recursiveDictList" v-loading="recursiveDictLoading" max-height="400"
        @selection-change="handleRecursiveDictSelectionChange" row-key="id">
        <el-table-column type="selection" width="45" :reserve-selection="true" />
        <el-table-column prop="name" :label="$t('task.dictName')" min-width="150" />
        <el-table-column prop="wordCount" :label="$t('task.wordCount')" width="100" />
        <el-table-column prop="isBuiltin" :label="$t('common.type')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">{{ row.isBuiltin ? $t('task.builtin') :
              $t('task.custom') }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="recursiveDictSelectDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmRecursiveDictSelection">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- POC选择对话框 -->
    <el-dialog v-model="pocSelectDialogVisible" :title="$t('task.selectPoc')" width="1260px"
      @open="handlePocDialogOpen">
      <div class="poc-select-container">
        <!-- 左侧：POC列表 -->
        <div class="poc-select-left">
          <el-tabs v-model="pocSelectTab">
            <!-- 默认模板 -->
            <el-tab-pane :label="$t('task.defaultTemplate')" name="nuclei">
              <el-form :inline="true" class="poc-filter-form">
                <el-form-item>
                  <el-input v-model="nucleiTemplateFilter.keyword" :placeholder="$t('poc.searchAllPlaceholder')" clearable
                    style="width: 170px" @keyup.enter="searchNucleiTemplatesForSelect"
                    @clear="searchNucleiTemplatesForSelect" />
                </el-form-item>
                <el-form-item>
                  <el-select v-model="nucleiTemplateFilter.tag" :placeholder="$t('poc.filterTag')" clearable filterable
                    style="width: 120px" @change="searchNucleiTemplatesForSelect">
                    <el-option v-for="t in nucleiTemplateFacets.tags" :key="t.value" :label="`${t.value} (${t.count})`"
                      :value="t.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="nucleiTemplateFilter.category" :placeholder="$t('poc.filterCategory')" clearable
                    filterable style="width: 120px" @change="searchNucleiTemplatesForSelect">
                    <el-option v-for="c in nucleiTemplateFacets.categories" :key="c.value"
                      :label="`${c.value} (${c.count})`" :value="c.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="nucleiTemplateFilter.severities" :placeholder="$t('task.level')" multiple
                    collapse-tags collapse-tags-tooltip clearable style="width: 140px"
                    @change="searchNucleiTemplatesForSelect">
                    <el-option v-for="s in severityLevelOptions" :key="s" :label="s" :value="s" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="nucleiTemplateFilter.protocols" :placeholder="$t('poc.filterProtocol')" multiple
                    filterable collapse-tags collapse-tags-tooltip clearable style="width: 130px"
                    @change="searchNucleiTemplatesForSelect">
                    <el-option v-for="p in nucleiTemplateFacets.protocols" :key="p.value"
                      :label="`${p.value} (${p.count})`" :value="p.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="nucleiTemplateFilter.hasCve" placeholder="CVE" clearable style="width: 110px"
                    @change="searchNucleiTemplatesForSelect">
                    <el-option :label="$t('poc.cveYes') + ` (${nucleiTemplateFacets.cveTrue})`" :value="true" />
                    <el-option :label="$t('poc.cveNo') + ` (${nucleiTemplateFacets.cveFalse})`" :value="false" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="nucleiTemplateFilter.products" :placeholder="$t('poc.productPlaceholder')"
                    multiple filterable collapse-tags collapse-tags-tooltip clearable style="width: 170px"
                    @change="searchNucleiTemplatesForSelect">
                    <el-option v-for="p in nucleiTemplateFacets.products" :key="p.value"
                      :label="`${p.value} (${p.count})`" :value="p.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" size="small" @click="searchNucleiTemplatesForSelect">{{ $t('common.search')
                    }}</el-button>
                  <el-button size="small" @click="resetNucleiTemplateFilter">{{ $t('common.reset') }}</el-button>
                  <el-button v-if="!nucleiSelectAll" type="success" size="small" @click="selectAllNucleiTemplates"
                    :loading="selectAllNucleiLoading">{{ $t('task.selectAll') }}</el-button>
                  <el-button v-if="nucleiSelectAll || selectedNucleiTemplateIds.length > 0" type="warning" size="small"
                    @click="deselectAllNucleiTemplates">{{ $t('task.deselectAll') }}</el-button>
                </el-form-item>
              </el-form>
              <div v-if="nucleiSelectAll" class="select-all-tip">
                {{ $t('task.selectAllHint', { count: nucleiSelectAllCount }) }}
              </div>
              <el-table ref="nucleiTableRef" :data="nucleiTemplateList" v-loading="nucleiTemplateLoading"
                max-height="400" @selection-change="handleNucleiSelectionChange" row-key="id">
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
                    <el-button type="primary" link size="small" @click="viewPocContent(row, 'nuclei')">{{
                      $t('common.view') }}</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <el-pagination v-model:current-page="nucleiTemplatePagination.page"
                v-model:page-size="nucleiTemplatePagination.pageSize" :total="nucleiTemplatePagination.total"
                :page-sizes="[50, 100, 200]" layout="total, sizes, prev, pager, next" class="poc-pagination"
                @size-change="loadNucleiTemplatesForSelect" @current-change="loadNucleiTemplatesForSelect" />
            </el-tab-pane>

            <!-- 自定义POC -->
            <el-tab-pane :label="$t('task.customPoc')" name="custom">
              <el-form :inline="true" class="poc-filter-form">
                <el-form-item>
                  <el-input v-model="customPocFilter.keyword" :placeholder="$t('poc.searchAllPlaceholder')" clearable
                    style="width: 170px" @keyup.enter="searchCustomPocsForSelect" @clear="searchCustomPocsForSelect" />
                </el-form-item>
                <el-form-item>
                  <el-select v-model="customPocFilter.tag" :placeholder="$t('poc.filterTag')" clearable filterable
                    style="width: 120px" @change="searchCustomPocsForSelect">
                    <el-option v-for="t in customPocFacets.tags" :key="t.value" :label="`${t.value} (${t.count})`"
                      :value="t.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="customPocFilter.severities" :placeholder="$t('task.level')" multiple
                    collapse-tags collapse-tags-tooltip clearable style="width: 140px"
                    @change="searchCustomPocsForSelect">
                    <el-option v-for="s in severityLevelOptions" :key="s" :label="s" :value="s" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="customPocFilter.protocols" :placeholder="$t('poc.filterProtocol')" multiple
                    filterable collapse-tags collapse-tags-tooltip clearable style="width: 130px"
                    @change="searchCustomPocsForSelect">
                    <el-option v-for="p in customPocFacets.protocols" :key="p.value"
                      :label="`${p.value} (${p.count})`" :value="p.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="customPocFilter.hasCve" placeholder="CVE" clearable style="width: 110px"
                    @change="searchCustomPocsForSelect">
                    <el-option :label="$t('poc.cveYes') + ` (${customPocFacets.cveTrue})`" :value="true" />
                    <el-option :label="$t('poc.cveNo') + ` (${customPocFacets.cveFalse})`" :value="false" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-select v-model="customPocFilter.products" :placeholder="$t('poc.productPlaceholder')" multiple
                    filterable collapse-tags collapse-tags-tooltip clearable style="width: 170px"
                    @change="searchCustomPocsForSelect">
                    <el-option v-for="p in customPocFacets.products" :key="p.value" :label="`${p.value} (${p.count})`"
                      :value="p.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" size="small" @click="searchCustomPocsForSelect">{{ $t('common.search')
                    }}</el-button>
                  <el-button size="small" @click="resetCustomPocFilter">{{ $t('common.reset') }}</el-button>
                  <el-button v-if="!customPocSelectAll" type="success" size="small" @click="selectAllCustomPocs"
                    :loading="selectAllCustomLoading">{{ $t('task.selectAll') }}</el-button>
                  <el-button v-if="customPocSelectAll || selectedCustomPocIds.length > 0" type="warning" size="small"
                    @click="deselectAllCustomPocs">{{ $t('task.deselectAll') }}</el-button>
                </el-form-item>
              </el-form>
              <div v-if="customPocSelectAll" class="select-all-tip">
                {{ $t('task.selectAllHint', { count: customPocSelectAllCount }) }}
              </div>
              <el-table ref="customPocTableRef" :data="customPocList" v-loading="customPocLoading" max-height="400"
                @selection-change="handleCustomPocSelectionChange" row-key="id">
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
                    <el-button type="primary" link size="small" @click="viewPocContent(row, 'custom')">{{
                      $t('common.view') }}</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <el-pagination v-model:current-page="customPocPagination.page"
                v-model:page-size="customPocPagination.pageSize" :total="customPocPagination.total"
                :page-sizes="[50, 100, 200]" layout="total, sizes, prev, pager, next" class="poc-pagination"
                @size-change="loadCustomPocsForSelect" @current-change="loadCustomPocsForSelect" />
            </el-tab-pane>
          </el-tabs>
        </div>

        <!-- 右侧：已选择列表 -->
        <div class="poc-select-right">
          <div class="selected-header">
            <span>{{ $t('task.selected') }} ({{ (nucleiSelectAll ? nucleiSelectAllCount :
              selectedNucleiTemplates.length) +
              (customPocSelectAll ? customPocSelectAllCount : selectedCustomPocs.length) }})</span>
            <el-button type="danger" link size="small" @click="clearAllSelections"
              v-if="nucleiSelectAll || customPocSelectAll || selectedNucleiTemplates.length + selectedCustomPocs.length > 0">
              {{ $t('task.clearAll') }}
            </el-button>
          </div>
          <div class="selected-search">
            <el-input v-model="selectedPocSearchKeyword" :placeholder="$t('task.searchSelected')" clearable size="small"
              :prefix-icon="Search" />
          </div>
          <div class="selected-list">
            <!-- 默认模板 -->
            <div v-if="nucleiSelectAll" class="selected-group">
              <div class="group-header">
                <span>{{ $t('task.defaultTemplate') }}: {{ $t('task.allSelectedCount', { count: nucleiSelectAllCount })
                }}</span>
                <el-button type="danger" link size="small" @click="deselectAllNucleiTemplates">{{ $t('task.deselectAll')
                }}</el-button>
              </div>
              <div v-if="hasNucleiSelectAllFilter" class="selected-all-conditions">
                <el-tag v-if="nucleiSelectAllFilter.keyword" size="small">{{ $t('task.keyword') }}: {{
                  nucleiSelectAllFilter.keyword }}</el-tag>
                <el-tag v-if="nucleiSelectAllFilter.tag" size="small">{{ $t('task.tags') }}: {{
                  nucleiSelectAllFilter.tag }}</el-tag>
                <el-tag v-if="nucleiSelectAllFilter.category" size="small">{{ $t('task.category') }}: {{
                  nucleiSelectAllFilter.category }}</el-tag>
                <el-tag v-if="nucleiSelectAllFilter.severities.length" size="small">{{ $t('task.level') }}: {{
                  nucleiSelectAllFilter.severities.join(',') }}</el-tag>
                <el-tag v-if="nucleiSelectAllFilter.protocols.length" size="small">{{ $t('poc.filterProtocol') }}: {{
                  nucleiSelectAllFilter.protocols.join(',') }}</el-tag>
                <el-tag v-if="nucleiSelectAllFilter.products.length" size="small">{{ $t('poc.filterProduct') }}: {{
                  nucleiSelectAllFilter.products.join(',') }}</el-tag>
                <el-tag v-if="nucleiSelectAllFilter.hasCve !== null" size="small">CVE: {{ nucleiSelectAllFilter.hasCve
                  ? $t('poc.cveYes') : $t('poc.cveNo') }}</el-tag>
              </div>
              <div v-else class="selected-all-conditions">{{ $t('task.allTemplates') }}</div>
            </div>
            <div v-else-if="filteredSelectedNucleiTemplates.length > 0" class="selected-group">
              <div class="group-header">
                <span>{{ $t('task.defaultTemplate') }} ({{ filteredSelectedNucleiTemplates.length }}<template
                    v-if="selectedPocSearchKeyword">/{{ selectedNucleiTemplates.length }}</template>)</span>
                <el-button type="danger" link size="small" @click="clearNucleiSelections">{{ $t('task.clear')
                }}</el-button>
              </div>
              <div class="selected-items">
                <div v-for="item in filteredSelectedNucleiTemplates" :key="item.id" class="selected-item">
                  <span class="item-name" :title="item.name || item.id">{{ item.name || item.id }}</span>
                  <el-icon class="item-remove" @click="removeNucleiTemplate(item.id)">
                    <Close />
                  </el-icon>
                </div>
              </div>
            </div>
            <!-- 自定义POC -->
            <div v-if="customPocSelectAll" class="selected-group">
              <div class="group-header">
                <span>{{ $t('task.customPoc') }}: {{ $t('task.allSelectedCount', { count: customPocSelectAllCount })
                }}</span>
                <el-button type="danger" link size="small" @click="deselectAllCustomPocs">{{ $t('task.deselectAll')
                }}</el-button>
              </div>
              <div v-if="hasCustomPocSelectAllFilter" class="selected-all-conditions">
                <el-tag v-if="customPocSelectAllFilter.keyword" size="small">{{ $t('task.keyword') }}: {{
                  customPocSelectAllFilter.keyword }}</el-tag>
                <el-tag v-if="customPocSelectAllFilter.tag" size="small">{{ $t('task.tags') }}: {{
                  customPocSelectAllFilter.tag }}</el-tag>
                <el-tag v-if="customPocSelectAllFilter.severities.length" size="small">{{ $t('task.level') }}: {{
                  customPocSelectAllFilter.severities.join(',') }}</el-tag>
                <el-tag v-if="customPocSelectAllFilter.protocols.length" size="small">{{ $t('poc.filterProtocol') }}: {{
                  customPocSelectAllFilter.protocols.join(',') }}</el-tag>
                <el-tag v-if="customPocSelectAllFilter.products.length" size="small">{{ $t('poc.filterProduct') }}: {{
                  customPocSelectAllFilter.products.join(',') }}</el-tag>
                <el-tag v-if="customPocSelectAllFilter.hasCve !== null" size="small">CVE: {{ customPocSelectAllFilter.hasCve
                  ? $t('poc.cveYes') : $t('poc.cveNo') }}</el-tag>
              </div>
              <div v-else class="selected-all-conditions">{{ $t('task.allPocs') }}</div>
            </div>
            <div v-else-if="filteredSelectedCustomPocs.length > 0" class="selected-group">
              <div class="group-header">
                <span>{{ $t('task.customPoc') }} ({{ filteredSelectedCustomPocs.length }}<template
                    v-if="selectedPocSearchKeyword">/{{ selectedCustomPocs.length }}</template>)</span>
                <el-button type="danger" link size="small" @click="clearCustomPocSelections">{{ $t('task.clear')
                }}</el-button>
              </div>
              <div class="selected-items">
                <div v-for="item in filteredSelectedCustomPocs" :key="item.id" class="selected-item">
                  <span class="item-name" :title="item.name">{{ item.name }}</span>
                  <el-icon class="item-remove" @click="removeCustomPoc(item.id)">
                    <Close />
                  </el-icon>
                </div>
              </div>
            </div>
            <!-- 空状态 -->
            <div
              v-if="!nucleiSelectAll && !customPocSelectAll && filteredSelectedNucleiTemplates.length === 0 && filteredSelectedCustomPocs.length === 0"
              class="selected-empty">
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
        <el-descriptions-item :label="$t('task.templateId')">{{ currentViewPoc.id || currentViewPoc.templateId
        }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.name')">{{ currentViewPoc.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.severityLevel')">
          <el-tag :type="getSeverityType(currentViewPoc.severity)" size="small">{{ currentViewPoc.severity }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('task.author')">{{ currentViewPoc.author || '-' }}</el-descriptions-item>
      </el-descriptions>
      <div class="poc-content-wrapper" v-loading="pocContentLoading">
        <el-input v-model="currentViewPoc.content" type="textarea" :rows="18" readonly />
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
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Close, Search, InfoFilled } from '@element-plus/icons-vue'
import {
  getCronTaskList,
  getCronTaskDetail,
  saveCronTask,
  validateCronSpec
} from '@/api/crontask'
import { getScanTemplateList, getScanTemplateDetail } from '@/api/task'
import { getNucleiTemplateList, getCustomPocList, getNucleiTemplateCategories, getCustomPocCategories } from '@/api/poc'
import { getDirScanDictEnabledList } from '@/api/dirscan'
import { getSubdomainDictEnabledList } from '@/api/subdomain'
import request from '@/api/request'
import { validateTargets, formatValidationErrors } from '@/utils/target'
import { isValidDirScanStatusCode, normalizeDirScanStatusCodes } from '@/utils/dirscan'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const isEdit = ref(false)
const submitting = ref(false)
const formRef = ref(null)

// 目录扫描常用状态码预设
const commonStatusCodes = [200, 204, 301, 302, 307, 308, 401, 403, 405, 500]

// 提取默认表单值，供 form 初始化和 resetForm 共用
function getDefaultForm() {
  return {
    id: '',
    name: '',
    scheduleType: 'cron',
    cronSpec: '0 0 2 * * *',
    scheduleTime: '',
    scheduleTimeDate: null,
    // 目标来源：manual 手动输入 / asset 资产选择
    targetMode: 'manual',
    target: '',
    orgId: '',
    assetIds: [],
    enableSubdomainPull: false,
    // 扫描配置来源：template 扫描模板 / custom 自定义配置
    configSource: 'custom',
    templateId: '',
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
    portscanTargetTimeout: 120,
    portscanProbeTimeoutMs: 1000,
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
    portidentifyTimeout: 60,
    portidentifyConcurrency: 10,
    portidentifyArgs: '-sV --version-intensity 5',
    portidentifyUDP: false,
    portidentifyFastMode: false,
    portidentifyForceScan: false,
    // 指纹识别
    fingerprintEnable: true,
    fingerprintTool: 'httpx',
    fingerprintIconHash: true,
    fingerprintCustomEngine: false,
    fingerprintScreenshot: true,
    fingerprintCert: true,
    fingerprintActiveScan: false,
    fingerprintActiveTimeout: 10,
    fingerprintTimeout: 90,
    fingerprintFilterMode: 'http_mapping',
    fingerprintForceScan: false,
    // 弱口令扫描
    brutescanEnable: false,
    brutescanServices: [],
    brutescanThreads: 20,
    brutescanTimeout: 5,
    brutescanDelayMs: 100,
    brutescanStopOnFirst: false,
    brutescanForceScan: false,
    // 漏洞扫描
    pocscanEnable: false,
    pocscanMode: 'auto',
    pocscanAutoScan: true,
    pocscanAutomaticScan: true,
    pocscanCustomOnly: false,
    pocscanSeverity: ['critical', 'high', 'medium'],
    pocscanTargetTimeout: 600,
    pocscanRateLimit: 800,
    pocscanConcurrency: 80,
    pocscanForceScan: false,
    pocscanNucleiTemplateIds: [],
    pocscanCustomPocIds: [],
    pocscanHeaderMode: 'none',
    pocscanPresetUA: '',
    pocscanCustomHeadersText: '',
    pocscanNucleiTemplates: [],
    pocscanCustomPocs: [],
    // 目录扫描
    dirscanEnable: false,
    dirscanTool: 'ffuf',
    dirscanDictIds: [],
    dirscanDicts: [],
    dirscanFollowRedirect: false,
    dirscanForceScan: false,
    dirscanStatusCodes: [],
    dirscanAutoCalibration: true,
    dirscanRecursion: false,
    dirscanRecursionDepth: 2,
    jsfinderEnable: false,
    jsfinderThreads: 10,
    jsfinderTimeout: 10,
    jsfinderEnableSourcemap: true,
    jsfinderEnableUnauthCheck: true
  }
}

const form = reactive(getDefaultForm())

// 扫描配置折叠面板
const activeCollapse = ref(['portscan', 'fingerprint'])

// 判断是否有前序扫描阶段启用（用于控制强制扫描开关的显隐）
const hasPrePhaseEnabled = computed(() => {
  return form.domainscanEnable || form.portscanEnable ||
    form.portidentifyEnable || form.fingerprintEnable
})

// 各模块关键配置摘要（折叠状态下显示）
const domainscanSummary = computed(() => {
  const tools = []
  if (form.domainscanSubfinder) tools.push('Subfinder')
  if (form.domainscanBruteforce) tools.push('KSubdomain')
  return tools.length ? tools.join(' + ') : t('task.notStarted')
})

const portscanSummary = computed(() => {
  const tool = form.portscanTool === 'naabu' ? 'Naabu' : 'Masscan'
  return `${tool} | ${form.ports} | ${form.portscanRate}${t('task.summarySuffix')}`
})

const portidentifySummary = computed(() => {
  const tool = form.portidentifyTool === 'nmap' ? 'Nmap' : 'Fingerprintx'
  return `${tool} | ${t('task.summaryTimeout', { n: form.portidentifyTimeout })}`
})

const fingerprintSummary = computed(() => {
  const tool = form.fingerprintTool === 'httpx' ? 'Httpx' : t('task.builtinEngine')
  const features = []
  if (form.fingerprintIconHash) features.push(t('task.iconHash'))
  if (form.fingerprintScreenshot) features.push(t('task.screenshot'))
  if (form.fingerprintCert) features.push(t('task.cert'))
  return features.length ? `${tool} | ${features.join(' ')}` : tool
})

const brutescanSummary = computed(() => {
  if (!form.brutescanServices || !form.brutescanServices.length) return t('task.summaryNoService')
  return form.brutescanServices.slice(0, 4).join(',') + (form.brutescanServices.length > 4 ? '...' : '')
})

const dirscanSummary = computed(() => {
  const parts = []
  if (form.dirscanDictIds && form.dirscanDictIds.length) {
    parts.push(t('task.selectedCount', { count: form.dirscanDictIds.length }))
  } else {
    parts.push(t('task.summaryNoDict'))
  }
  if (form.dirscanRecursion) parts.push(t('task.recursion'))
  return parts.join(' | ')
})

const jsfinderSummary = computed(() => {
  const parts = [t('task.summaryThreads', { n: form.jsfinderThreads })]
  if (form.jsfinderEnableSourcemap) parts.push('Sourcemap')
  if (form.jsfinderEnableUnauthCheck) parts.push(t('task.enableUnauthCheck'))
  return parts.join(' | ')
})

const pocscanSummary = computed(() => {
  const mode = form.pocscanMode === 'auto' ? t('task.autoMatch') : t('task.manualSelect')
  if (form.pocscanMode === 'manual') {
    const counts = []
    if (nucleiSelectAll.value) counts.push(`${t('task.allTemplates')}:${nucleiSelectAllCount.value}`)
    else if (form.pocscanNucleiTemplateIds.length) counts.push(`${t('task.templates')}:${form.pocscanNucleiTemplateIds.length}`)
    if (customPocSelectAll.value) counts.push(`${t('task.allPocs')}:${customPocSelectAllCount.value}`)
    else if (form.pocscanCustomPocIds.length) counts.push(`${t('task.pocs')}:${form.pocscanCustomPocIds.length}`)
    return counts.length ? `${mode} | ${counts.join(' ')}` : `${mode} | ${t('task.summaryNoPoc')}`
  }
  const severity = form.pocscanSeverity && form.pocscanSeverity.length ? form.pocscanSeverity.join(',') : '-'
  return `${mode} | ${severity}`
})

// 目录扫描字典选择相关
const dictSelectDialogVisible = ref(false)
const dictList = ref([])
const dictLoading = ref(false)
const dictTableRef = ref()
const selectedDictRows = ref([])

// 子域名字典选择相关
const subdomainDictSelectDialogVisible = ref(false)
const subdomainDictList = ref([])
const subdomainDictLoading = ref(false)
const subdomainDictTableRef = ref()
const selectedSubdomainDictRows = ref([])

// 递归爆破字典选择相关
const recursiveDictSelectDialogVisible = ref(false)
const recursiveDictList = ref([])
const recursiveDictLoading = ref(false)
const recursiveDictTableRef = ref()
const selectedRecursiveDictRows = ref([])

// POC选择相关
const pocSelectDialogVisible = ref(false)
const pocSelectTab = ref('nuclei')
const nucleiTemplateList = ref([])
const customPocList = ref([])
const nucleiTemplateLoading = ref(false)
const customPocLoading = ref(false)
const selectAllNucleiLoading = ref(false)
const selectAllCustomLoading = ref(false)
const nucleiTableRef = ref()
const customPocTableRef = ref()
const selectedNucleiTemplateIds = ref([])
const selectedCustomPocIds = ref([])
const selectedNucleiTemplates = ref([])
const selectedCustomPocs = ref([])
const selectedPocSearchKeyword = ref('')
// 手动全选状态：前端只记录选择意图（标记 + 筛选条件），由后端按条件查询展开
// 筛选字段与 POC 管理页保持一致（默认模板：keyword/tag/category/severities/protocols/products/hasCve）
const nucleiSelectAll = ref(false) // 默认模板是否全选
const nucleiSelectAllCount = ref(0) // 全选数量（仅展示，不加载列表）
const nucleiSelectAllFilter = reactive({ keyword: '', tag: '', category: '', severities: [], protocols: [], products: [], hasCve: null })
const customPocSelectAll = ref(false) // 自定义POC是否全选
const customPocSelectAllCount = ref(0)
const customPocSelectAllFilter = reactive({ keyword: '', tag: '', severities: [], protocols: [], products: [], hasCve: null })

// 全选筛选条件是否有非空项（用于展示条件标签）
const hasNucleiSelectAllFilter = computed(() => Object.values(nucleiSelectAllFilter).some(v => (Array.isArray(v) ? v.length > 0 : v !== '' && v !== null)))
const hasCustomPocSelectAllFilter = computed(() => Object.values(customPocSelectAllFilter).some(v => (Array.isArray(v) ? v.length > 0 : v !== '' && v !== null)))
// 防护标志：数据加载或批量选择期间，跳过 selection-change 事件处理
const isLoadingData = ref(false)
const isSelectingAll = ref(false)
// 选择POC弹窗筛选条件（与 POC 管理页的默认模板/自定义POC TAB 保持一致；已无 unknown 级别）
const severityLevelOptions = ['critical', 'high', 'medium', 'low', 'info']
const nucleiTemplateFilter = reactive({
  keyword: '',
  tag: '',
  category: '',
  severities: [],
  protocols: [],
  products: [],
  hasCve: null
})
const customPocFilter = reactive({
  keyword: '',
  tag: '',
  severities: [],
  protocols: [],
  products: [],
  hasCve: null
})
// 筛选维度选项 + 数量统计（随筛选条件联动，来自分面统计接口）
const nucleiTemplateFacets = reactive({ categories: [], severities: [], protocols: [], products: [], tags: [], cveTrue: 0, cveFalse: 0 })
const customPocFacets = reactive({ severities: [], protocols: [], products: [], tags: [], cveTrue: 0, cveFalse: 0 })
const nucleiTemplatePagination = reactive({ page: 1, pageSize: 50, total: 0 })
const customPocPagination = reactive({ page: 1, pageSize: 50, total: 0 })

// 弱口令扫描服务选项
const bruteServiceOptions = [
  { label: 'SSH', value: 'ssh' },
  { label: 'MySQL', value: 'mysql' },
  { label: 'Redis', value: 'redis' },
  { label: 'MongoDB', value: 'mongodb' },
  { label: 'PostgreSQL', value: 'postgresql' },
  { label: 'MSSQL', value: 'mssql' },
  { label: 'FTP', value: 'ftp' },
  { label: 'Oracle', value: 'oracle' },
  { label: 'SMB', value: 'smb' },
  { label: 'MQTT', value: 'mqtt' },
]

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
  scheduleType: [{ required: true, message: t('common.pleaseSelect'), trigger: 'change' }],
  templateId: [{
    required: true,
    validator: (rule, value, callback) => {
      if (form.configSource === 'template' && !value) {
        callback(new Error(t('cronTask.selectTemplatePlaceholder')))
      } else {
        callback()
      }
    },
    trigger: 'change'
  }],
  target: [{
    required: true,
    validator: (rule, value, callback) => {
      if (form.targetMode === 'manual' && !value) {
        callback(new Error(t('cronTask.targetPlaceholder')))
      } else if (form.targetMode === 'manual' && value) {
        const errors = validateTargets(value)
        if (errors.length > 0) {
          callback(new Error(formatValidationErrors(errors)))
        } else {
          callback()
        }
      } else {
        callback()
      }
    },
    trigger: 'blur'
  }],
  assetIds: [{
    required: true,
    validator: (rule, value, callback) => {
      if (form.targetMode === 'asset' && (!form.assetIds || form.assetIds.length === 0)) {
        callback(new Error(t('cronTask.assetRequired')))
      } else {
        callback()
      }
    },
    trigger: 'change'
  }],
  dirscanStatusCodes: [{
    validator: (_rule, value, callback) => {
      if (form.configSource !== 'custom' || !form.dirscanEnable || (value || []).every(isValidDirScanStatusCode)) {
        callback()
      } else {
        callback(new Error(t('task.invalidStatusCode')))
      }
    },
    trigger: 'change'
  }],
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

// ===== 资产选择相关 =====
const organizationList = ref([])
const assetTargetList = ref([])
const assetTargetLoading = ref(false)
const assetTargetTableRef = ref()
const assetTargetFilter = reactive({ keyword: '' })
const assetTargetPagination = reactive({ page: 1, pageSize: 20, total: 0 })
// 选中资产对象（用于回显显示）
const selectedAssetRows = ref([])

// 扫描模板相关
const scanTemplateList = ref([])
const scanTemplateLoading = ref(false)
const selectedTemplateConfig = ref(null)

// 获取资产目标列表
async function fetchAssetTargetList(params) {
  try {
    const res = await request({
      url: '/asset/target/list',
      method: 'post',
      data: params
    })
    return res
  } catch (e) {
    return { code: -1, list: [], total: 0 }
  }
}

// 格式化资产时间戳（毫秒时间戳 → YYYY-MM-DD HH:mm）
function formatAssetTimestamp(ms) {
  if (!ms) return '-'
  const d = new Date(ms)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 将后端资产数据映射为前端表格行格式
function mapAssetItem(item) {
  return {
    ...item,
    type: item.targetType || item.type,
    value: item.targetValue || item.value,
  }
}

// 防护标志：数据加载或批量选择期间，跳过 selection-change 事件处理
const isLoadingAssetData = ref(false)
const isSelectingAllAssets = ref(false)

// 加载组织列表
async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    if (res.code === 0) {
      organizationList.value = res.list || []
    }
  } catch (e) {
    console.error('loadOrganizationsFailed:', e)
  }
}

// 加载资产目标列表
async function loadAssetTargets() {
  assetTargetLoading.value = true
  isLoadingAssetData.value = true
  try {
    const params = {
      page: assetTargetPagination.page,
      pageSize: assetTargetPagination.pageSize,
      query: assetTargetFilter.keyword || undefined
    }
    const res = await fetchAssetTargetList(params)
    if (res.code === 0) {
      // 将后端返回的 targetType/targetValue 映射为前端表格使用的 type/value 字段
      const rawList = res.list || res.data?.list || []
      assetTargetList.value = rawList.map(mapAssetItem)
      assetTargetPagination.total = res.total || res.data?.total || 0
      await nextTick()
      restoreAssetTableSelection()
    }
  } catch (e) {
    console.error('loadAssetTargetsFailed:', e)
  } finally {
    assetTargetLoading.value = false
    setTimeout(() => { isLoadingAssetData.value = false }, 100)
  }
}

// 恢复资产表格选中状态
function restoreAssetTableSelection() {
  if (!assetTargetTableRef.value) return
  const selectedIds = new Set(form.assetIds)
  assetTargetList.value.forEach(row => {
    if (selectedIds.has(row.id)) {
      assetTargetTableRef.value.toggleRowSelection(row, true)
    }
  })
}

// 资产表格多选变化
function handleAssetSelectionChange(selection) {
  if (isSelectingAllAssets.value || isLoadingAssetData.value) return

  const currentPageIds = new Set(assetTargetList.value.map(a => a.id))
  const currentPageSelected = selection.filter(a => currentPageIds.has(a.id))
  const currentPageSelectedIds = new Set(currentPageSelected.map(a => a.id))

  // 保留其他页选中项
  const otherPageItems = selectedAssetRows.value.filter(a => !currentPageIds.has(a.id))
  const otherPageIds = form.assetIds.filter(id => !currentPageIds.has(id))

  selectedAssetRows.value = [...otherPageItems, ...currentPageSelected]
  form.assetIds = [...otherPageIds, ...Array.from(currentPageSelectedIds)]
  // assetIds 由程序赋值，不触发原生 change 事件，手动校验以同步错误态
  if (formRef.value) formRef.value.validateField('assetIds')
}

// 全选资产（当前筛选条件下的全部资产）
async function selectAllAssets() {
  isSelectingAllAssets.value = true
  try {
    const filterArgs = {
      query: assetTargetFilter.keyword || undefined
    }
    const firstRes = await fetchAssetTargetList({ page: 1, pageSize: 1, ...filterArgs })
    if (firstRes.code !== 0) return
    const total = firstRes.total || firstRes.data?.total || 0
    if (total === 0) {
      ElMessage.warning('没有可选择的资产')
      return
    }
    // 后端 NormalizePage 将 pageSize 上限截断为 100，按 100 逐页拉取当前筛选条件下的全部资产
    const pageSize = 100
    const totalPages = Math.ceil(total / pageSize)
    const allRows = []
    for (let p = 1; p <= totalPages; p++) {
      const res = await fetchAssetTargetList({ page: p, pageSize, ...filterArgs })
      if (res.code === 0) {
        allRows.push(...(res.list || res.data?.list || []))
      }
    }
    // 合并去重
    const existingIds = new Set(form.assetIds)
    let addedCount = 0
    allRows.forEach(row => {
      const mapped = mapAssetItem(row)
      if (!existingIds.has(mapped.id)) {
        form.assetIds.push(mapped.id)
        selectedAssetRows.value.push(mapped)
        existingIds.add(mapped.id)
        addedCount++
      }
    })
    await nextTick()
    if (assetTargetTableRef.value) {
      assetTargetList.value.forEach(row => assetTargetTableRef.value.toggleRowSelection(row, true))
    }
    ElMessage.success(`已选择 ${form.assetIds.length} 个资产（新增 ${addedCount} 个）`)
  } catch (e) {
    console.error('selectAllAssetsFailed:', e)
    ElMessage.error('全选失败')
  } finally {
    isSelectingAllAssets.value = false
  }
}

// 清空资产选择
function clearAssetSelection() {
  form.assetIds = []
  selectedAssetRows.value = []
  if (assetTargetTableRef.value) assetTargetTableRef.value.clearSelection()
  // 同步校验态：清空后应显示"未选择资产"错误
  if (formRef.value) formRef.value.validateField('assetIds')
}

// 加载扫描模板列表
async function loadScanTemplates() {
  scanTemplateLoading.value = true
  try {
    const res = await getScanTemplateList({ page: 1, pageSize: 200 })
    if (res.code === 0) {
      scanTemplateList.value = res.list || res.data?.list || []
    }
  } catch (e) {
    console.error('loadScanTemplatesFailed:', e)
  } finally {
    scanTemplateLoading.value = false
  }
}

// 选择扫描模板时加载其配置预览
async function onTemplateSelect(templateId) {
  if (!templateId) {
    selectedTemplateConfig.value = null
    return
  }
  try {
    const res = await getScanTemplateDetail({ id: templateId })
    if (res.code === 0) {
      selectedTemplateConfig.value = res.data || res
    }
  } catch (e) {
    console.error('loadTemplateDetailFailed:', e)
  }
}

// 查看模板配置（简单弹窗显示 JSON）
function previewTemplateConfig() {
  if (!selectedTemplateConfig.value) return
  try {
    const cfg = typeof selectedTemplateConfig.value.config === 'string'
      ? JSON.parse(selectedTemplateConfig.value.config)
      : selectedTemplateConfig.value.config
    ElMessageBox.alert(
      JSON.stringify(cfg, null, 2),
      '模板配置预览',
      { dangerouslyUseHTMLString: false, confirmButtonText: '关闭' }
    )
  } catch (e) {
    ElMessage.warning('配置解析失败')
  }
}

// 验证Cron表达式
async function validateCron() {
  if (!form.cronSpec) {
    cronValidation.valid = false
    cronValidation.error = t('cronTask.cronValidateError')
    cronValidation.nextTimes = []
    return
  }

  try {
    const res = await validateCronSpec({ cronSpec: form.cronSpec })
    if (res.code === 0 && res.data) {
      cronValidation.valid = res.data.valid
      if (res.data.valid) {
        cronValidation.error = ''
        cronValidation.nextTimes = res.data.nextTimes || []
      } else {
        cronValidation.error = res.data.message || t('cronTask.cronValidateError')
        cronValidation.nextTimes = []
      }
    } else {
      cronValidation.valid = false
      cronValidation.error = res.msg || t('cronTask.cronValidateError')
      cronValidation.nextTimes = []
    }
  } catch (error) {
    cronValidation.valid = false
    cronValidation.error = t('cronTask.validateRequestError')
    cronValidation.nextTimes = []
  }
}

// 端口范围与单目标扫描上限联动（推荐值）
const PORT_TIMEOUT_MAP = {
  'top100': 120,
  'top1000': 200,
  '80,443,8080,8443': 60,
  '1-65535': 900,
}
let isLoadingConfig = false
watch(() => form.ports, (val) => {
  if (!isLoadingConfig && PORT_TIMEOUT_MAP[val] !== undefined) {
    form.portscanTargetTimeout = PORT_TIMEOUT_MAP[val]
  }
})

// Cron 表达式实时校验（输入防抖 400ms，避免频繁请求后端）
let cronDebounceTimer = null
watch(() => form.cronSpec, (val) => {
  if (form.scheduleType !== 'cron') return
  if (cronDebounceTimer) clearTimeout(cronDebounceTimer)
  if (!val) {
    cronValidation.valid = false
    cronValidation.error = ''
    cronValidation.nextTimes = []
    return
  }
  cronDebounceTimer = setTimeout(() => {
    validateCron()
  }, 400)
})

// 构建自定义HTTP头部
function buildCustomHeaders() {
  const headers = []
  if (form.pocscanHeaderMode === 'preset' && form.pocscanPresetUA) {
    headers.push('User-Agent: ' + form.pocscanPresetUA)
  } else if (form.pocscanHeaderMode === 'custom' && form.pocscanCustomHeadersText) {
    const lines = form.pocscanCustomHeadersText.split('\n')
    for (const line of lines) {
      const trimmed = line.trim()
      if (trimmed && trimmed.includes(':')) {
        headers.push(trimmed)
      }
    }
  }
  return headers
}

// 解析自定义HTTP头部（回显用）
function parseCustomHeaders(headers) {
  if (!headers || headers.length === 0) {
    return { pocscanHeaderMode: 'none', pocscanPresetUA: '', pocscanCustomHeadersText: '' }
  }
  if (headers.length === 1 && headers[0].toLowerCase().startsWith('user-agent:')) {
    const ua = headers[0].substring(headers[0].indexOf(':') + 1).trim()
    return { pocscanHeaderMode: 'preset', pocscanPresetUA: ua, pocscanCustomHeadersText: '' }
  }
  return { pocscanHeaderMode: 'custom', pocscanPresetUA: '', pocscanCustomHeadersText: headers.join('\n') }
}

// 将扁平表单字段构建为嵌套配置结构
function buildConfig() {
  const config = {
    domainscan: {
      enable: form.domainscanEnable,
      subfinder: form.domainscanSubfinder,
      timeout: form.domainscanTimeout,
      maxEnumerationTime: form.domainscanMaxEnumTime,
      threads: form.domainscanThreads,
      rateLimit: form.domainscanRateLimit,
      removeWildcard: form.domainscanRemoveWildcard,
      resolveDNS: form.domainscanResolveDNS,
      concurrent: form.domainscanConcurrent,
      subdomainDictIds: form.domainscanBruteforce ? (form.subdomainDictIds || []) : [],
      bruteforceTimeout: form.domainscanBruteforce ? (form.domainscanBruteforceTimeout || 30) : 30,
      recursiveBrute: form.domainscanBruteforce ? form.domainscanRecursiveBrute : false,
      recursiveDictIds: (form.domainscanBruteforce && form.domainscanRecursiveBrute) ? (form.recursiveDictIds || []) : [],
      wildcardDetect: form.domainscanBruteforce ? form.domainscanWildcardDetect : false,
    },
    portscan: {
      enable: form.portscanEnable,
      tool: form.portscanTool,
      rate: form.portscanRate,
      ports: form.ports,
      portThreshold: form.portThreshold,
      scanType: form.scanType,
      targetTimeout: form.portscanTargetTimeout,
      probeTimeoutMs: form.portscanProbeTimeoutMs,
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
      cert: form.fingerprintCert,
      activeScan: form.fingerprintActiveScan,
      activeTimeout: form.fingerprintActiveTimeout,
      targetTimeout: form.fingerprintTimeout,
      filterMode: form.fingerprintFilterMode,
      forceScan: form.fingerprintForceScan && !form.portscanEnable && !form.portidentifyEnable
    },
    brutescan: {
      enable: form.brutescanEnable,
      services: form.brutescanServices,
      threads: form.brutescanThreads,
      timeout: form.brutescanTimeout,
      delayMs: form.brutescanDelayMs,
      stopOnFirst: form.brutescanStopOnFirst,
      forceScan: form.brutescanForceScan && !hasPrePhaseEnabled.value
    },
    pocscan: {
      enable: form.pocscanEnable,
      mode: form.pocscanMode,
      useNuclei: true,
      forceScan: form.pocscanForceScan && !hasPrePhaseEnabled.value,
      autoScan: form.pocscanAutoScan,
      automaticScan: form.pocscanAutomaticScan,
      customPocOnly: form.pocscanCustomOnly,
      severity: form.pocscanSeverity.join(','),
      targetTimeout: form.pocscanTargetTimeout,
      rateLimit: form.pocscanRateLimit,
      concurrency: form.pocscanConcurrency,
      nucleiTemplateIds: form.pocscanNucleiTemplateIds || [],
      customPocIds: form.pocscanCustomPocIds || [],
      customHeaders: buildCustomHeaders()
    },
    dirscan: {
      enable: form.dirscanEnable,
      tool: form.dirscanTool,
      dictIds: form.dirscanDictIds || [],
      followRedirect: form.dirscanFollowRedirect,
      forceScan: form.dirscanForceScan && !hasPrePhaseEnabled.value,
      statusCodes: normalizeDirScanStatusCodes(form.dirscanStatusCodes),
      autoCalibration: form.dirscanAutoCalibration,
      recursion: form.dirscanRecursion,
      recursionDepth: form.dirscanRecursionDepth
    },
    jsfinder: {
      enable: form.jsfinderEnable,
      threads: form.jsfinderThreads,
      timeout: form.jsfinderTimeout,
      enableSourcemap: form.jsfinderEnableSourcemap ? undefined : false,
      enableUnauthCheck: form.jsfinderEnableUnauthCheck ? undefined : false
    }
  }

  // 根据POC模式设置不同的配置
  if (form.pocscanMode === 'manual') {
    // 手动选择模式：全选只传标记与筛选条件，由后端查询展开
    config.pocscan.nucleiSelectAll = nucleiSelectAll.value
    config.pocscan.nucleiSelectAllFilter = nucleiSelectAll.value ? { ...nucleiSelectAllFilter } : undefined
    config.pocscan.customPocSelectAll = customPocSelectAll.value
    config.pocscan.customPocSelectAllFilter = customPocSelectAll.value ? { ...customPocSelectAllFilter } : undefined
    if (!nucleiSelectAll.value) {
      config.pocscan.nucleiTemplateIds = form.pocscanNucleiTemplateIds || []
    }
    if (!customPocSelectAll.value) {
      config.pocscan.customPocIds = form.pocscanCustomPocIds || []
    }
    config.pocscan.autoScan = false
    config.pocscan.automaticScan = false
    config.pocscan.customPocOnly = false
    // 手动模式下不叠加严重级别过滤：所选即所扫（severity 选择器在该模式下不展示，
    // 若继续携带默认值会在扫描时静默丢弃低危模板）
    config.pocscan.severity = ''
  } else {
    if (form.pocscanCustomOnly) {
      config.pocscan.autoScan = false
      config.pocscan.automaticScan = false
      config.pocscan.customPocOnly = true
    } else {
      config.pocscan.autoScan = form.pocscanAutoScan
      config.pocscan.automaticScan = form.pocscanAutomaticScan
      config.pocscan.customPocOnly = false
    }
  }

  return config
}

// 将嵌套配置结构映射回扁平表单字段（编辑回显用）
function applyConfig(config) {
  isLoadingConfig = true
  if (config.domainscan) {
    form.domainscanEnable = config.domainscan.enable ?? false
    form.domainscanSubfinder = config.domainscan.subfinder ?? true
    form.domainscanBruteforce = !!(config.domainscan.subdomainDictIds && config.domainscan.subdomainDictIds.length)
    form.domainscanBruteforceTimeout = config.domainscan.bruteforceTimeout ?? 30
    form.domainscanTimeout = config.domainscan.timeout ?? 300
    form.domainscanMaxEnumTime = config.domainscan.maxEnumerationTime ?? 10
    form.domainscanThreads = config.domainscan.threads ?? 10
    form.domainscanRateLimit = config.domainscan.rateLimit ?? 0
    form.domainscanRemoveWildcard = config.domainscan.removeWildcard ?? true
    form.domainscanResolveDNS = config.domainscan.resolveDNS ?? true
    form.domainscanConcurrent = config.domainscan.concurrent ?? 50
    form.subdomainDictIds = config.domainscan.subdomainDictIds || []
    form.domainscanRecursiveBrute = config.domainscan.recursiveBrute ?? false
    form.recursiveDictIds = config.domainscan.recursiveDictIds || []
    form.domainscanWildcardDetect = config.domainscan.wildcardDetect ?? true
  }
  if (config.portscan) {
    form.portscanEnable = config.portscan.enable ?? true
    form.portscanTool = config.portscan.tool ?? 'naabu'
    form.portscanRate = config.portscan.rate ?? 3000
    form.ports = config.portscan.ports ?? 'top100'
    form.portThreshold = config.portscan.portThreshold ?? 100
    form.scanType = config.portscan.scanType ?? 'c'
    form.portscanTargetTimeout = config.portscan.targetTimeout ?? config.portscan.timeout ?? 60
    form.portscanProbeTimeoutMs = config.portscan.probeTimeoutMs ?? 1000
    form.skipHostDiscovery = config.portscan.skipHostDiscovery ?? false
    form.excludeCDN = config.portscan.excludeCDN ?? false
    form.excludeHosts = config.portscan.excludeHosts ?? ''
    form.portscanWorkers = config.portscan.workers ?? 50
    form.portscanRetries = config.portscan.retries ?? 2
    form.portscanWarmUpTime = config.portscan.warmUpTime ?? 1
    form.portscanVerify = config.portscan.verify ?? false
  }
  if (config.portidentify) {
    form.portidentifyEnable = config.portidentify.enable ?? false
    form.portidentifyTool = config.portidentify.tool ?? 'nmap'
    form.portidentifyTimeout = config.portidentify.timeout ?? 60
    form.portidentifyConcurrency = config.portidentify.concurrency ?? 10
    form.portidentifyArgs = config.portidentify.args ?? ''
    form.portidentifyUDP = config.portidentify.udp ?? false
    form.portidentifyFastMode = config.portidentify.fastMode ?? false
    form.portidentifyForceScan = config.portidentify.forceScan ?? false
  }
  if (config.fingerprint) {
    form.fingerprintEnable = config.fingerprint.enable ?? true
    form.fingerprintTool = config.fingerprint.tool ?? 'httpx'
    form.fingerprintIconHash = config.fingerprint.iconHash ?? true
    form.fingerprintCustomEngine = config.fingerprint.customEngine ?? false
    form.fingerprintScreenshot = config.fingerprint.screenshot ?? true
    form.fingerprintCert = config.fingerprint.cert ?? true
    form.fingerprintActiveScan = config.fingerprint.activeScan ?? false
    form.fingerprintActiveTimeout = config.fingerprint.activeTimeout ?? 10
    form.fingerprintTimeout = config.fingerprint.targetTimeout ?? 90
    form.fingerprintFilterMode = config.fingerprint.filterMode ?? 'http_mapping'
    form.fingerprintForceScan = config.fingerprint.forceScan ?? false
  }
  if (config.brutescan) {
    form.brutescanEnable = config.brutescan.enable ?? false
    form.brutescanServices = config.brutescan.services || []
    form.brutescanThreads = config.brutescan.threads || 20
    form.brutescanTimeout = config.brutescan.timeout || 5
    form.brutescanDelayMs = config.brutescan.delayMs || 100
    form.brutescanStopOnFirst = config.brutescan.stopOnFirst ?? false
    form.brutescanForceScan = config.brutescan.forceScan ?? false
  }
  if (config.pocscan) {
    form.pocscanEnable = config.pocscan.enable ?? false
    form.pocscanMode = config.pocscan.mode ?? 'auto'
    form.pocscanAutoScan = config.pocscan.autoScan ?? true
    form.pocscanAutomaticScan = config.pocscan.automaticScan ?? true
    form.pocscanCustomOnly = config.pocscan.customPocOnly ?? false
    form.pocscanSeverity = typeof config.pocscan.severity === 'string'
      ? config.pocscan.severity.split(',').filter(Boolean) : (config.pocscan.severity || ['critical', 'high', 'medium'])
    form.pocscanTargetTimeout = config.pocscan.targetTimeout ?? 600
    form.pocscanRateLimit = config.pocscan.rateLimit ?? 800
    form.pocscanConcurrency = config.pocscan.concurrency ?? 80
    form.pocscanNucleiTemplateIds = config.pocscan.nucleiTemplateIds || []
    form.pocscanCustomPocIds = config.pocscan.customPocIds || []
    form.pocscanForceScan = config.pocscan.forceScan ?? false
    // 恢复手动全选状态（后端已展开的 ID 列表数量仅作展示，不加载列表）
    nucleiSelectAll.value = !!config.pocscan.nucleiSelectAll
    nucleiSelectAllCount.value = nucleiSelectAll.value ? (config.pocscan.nucleiTemplateIds?.length || 0) : 0
    Object.assign(nucleiSelectAllFilter, config.pocscan.nucleiSelectAllFilter || {})
    customPocSelectAll.value = !!config.pocscan.customPocSelectAll
    customPocSelectAllCount.value = customPocSelectAll.value ? (config.pocscan.customPocIds?.length || 0) : 0
    Object.assign(customPocSelectAllFilter, config.pocscan.customPocSelectAllFilter || {})
    const headerResult = parseCustomHeaders(config.pocscan.customHeaders)
    form.pocscanHeaderMode = headerResult.pocscanHeaderMode
    form.pocscanPresetUA = headerResult.pocscanPresetUA
    form.pocscanCustomHeadersText = headerResult.pocscanCustomHeadersText
  }
  if (config.dirscan) {
    form.dirscanEnable = config.dirscan.enable ?? false
    form.dirscanTool = config.dirscan.tool || 'ffuf'
    form.dirscanDictIds = config.dirscan.dictIds || []
    form.dirscanFollowRedirect = config.dirscan.followRedirect ?? false
    form.dirscanForceScan = config.dirscan.forceScan ?? false
    form.dirscanStatusCodes = config.dirscan.statusCodes || []
    form.dirscanAutoCalibration = config.dirscan.autoCalibration ?? true
    form.dirscanRecursion = config.dirscan.recursion ?? false
    form.dirscanRecursionDepth = config.dirscan.recursionDepth ?? 2
  }
  if (config.jsfinder) {
    form.jsfinderEnable = config.jsfinder.enable ?? false
    form.jsfinderThreads = config.jsfinder.threads ?? 10
    form.jsfinderTimeout = config.jsfinder.timeout ?? 10
    form.jsfinderEnableSourcemap = config.jsfinder.enableSourcemap ?? true
    form.jsfinderEnableUnauthCheck = config.jsfinder.enableUnauthCheck ?? true
  }
  isLoadingConfig = false
}

// 提交表单
async function handleSubmit() {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (valid) {
      submitting.value = true
      try {
        // 构建提交数据
        const submitData = {
          id: form.id,
          name: form.name,
          taskType: 'scan',
          scheduleType: form.scheduleType,
          cronSpec: form.cronSpec,
          scheduleTime: form.scheduleTime,
          targetMode: form.targetMode,
          target: form.targetMode === 'manual' ? form.target : '',
          assetIds: form.targetMode === 'asset' ? form.assetIds : [],
          orgId: form.targetMode === 'asset' ? form.orgId : '',
          enableSubdomainPull: form.targetMode === 'asset' ? form.enableSubdomainPull : false,
          configSource: form.configSource,
          templateId: form.configSource === 'template' ? form.templateId : ''
        }
        // 自定义配置模式下序列化扫描配置
        if (form.configSource === 'custom') {
          const config = buildConfig()
          submitData.config = JSON.stringify(config)
        } else {
          submitData.config = ''
        }

        const res = await saveCronTask(submitData)
        if (res.code === 0) {
          ElMessage.success(isEdit.value ? t('cronTask.updateSuccess') : t('cronTask.createSuccess'))
          router.push('/cron-task')
        } else {
          ElMessage.error(res.msg || (isEdit.value ? t('common.updateFailed') : t('common.createFailed')))
        }
      } catch (error) {
        console.error('saveCronTaskFailed:', error)
        ElMessage.error(isEdit.value ? t('common.updateFailed') : t('common.createFailed'))
      } finally {
        submitting.value = false
      }
    }
  })
}

// (Removed duplicate POC selection states)

async function handleDictDialogOpen() {
  dictLoading.value = true
  try {
    const res = await getDirScanDictEnabledList()
    if (res.code === 0) {
      // 后端 DirScanDictEnabledListResp 列表字段为 list（与 TaskCreate.vue 保持一致）
      dictList.value = res.list || []
      nextTick(() => {
        if (dictTableRef.value && form.dirscanDictIds) {
          dictList.value.forEach(row => {
            if (form.dirscanDictIds.includes(row.id)) {
              dictTableRef.value.toggleRowSelection(row, true)
            }
          })
        }
      })
    }
  } catch (e) { } finally { dictLoading.value = false }
  dictSelectDialogVisible.value = true
}

async function handleSubdomainDictDialogOpen() {
  subdomainDictLoading.value = true
  try {
    const res = await getSubdomainDictEnabledList()
    if (res.code === 0) {
      // 后端 SubdomainDictEnabledListResp 列表字段为 list
      subdomainDictList.value = res.list || []
      nextTick(() => {
        if (subdomainDictTableRef.value && form.subdomainDictIds) {
          subdomainDictList.value.forEach(row => {
            if (form.subdomainDictIds.includes(row.id)) {
              subdomainDictTableRef.value.toggleRowSelection(row, true)
            }
          })
        }
      })
    }
  } catch (e) { } finally { subdomainDictLoading.value = false }
  subdomainDictSelectDialogVisible.value = true
}

async function handleRecursiveDictDialogOpen() {
  recursiveDictLoading.value = true
  try {
    const res = await getSubdomainDictEnabledList()
    if (res.code === 0) {
      // 后端 SubdomainDictEnabledListResp 列表字段为 list
      recursiveDictList.value = res.list || []
      nextTick(() => {
        if (recursiveDictTableRef.value && form.recursiveDictIds) {
          recursiveDictList.value.forEach(row => {
            if (form.recursiveDictIds.includes(row.id)) {
              recursiveDictTableRef.value.toggleRowSelection(row, true)
            }
          })
        }
      })
    }
  } catch (e) { } finally { recursiveDictLoading.value = false }
  recursiveDictSelectDialogVisible.value = true
}

// 构建默认模板筛选请求参数（与 POC 管理页一致）
function buildNucleiTemplateFilterPayload() {
  return {
    keyword: nucleiTemplateFilter.keyword,
    tag: nucleiTemplateFilter.tag,
    category: nucleiTemplateFilter.category,
    severities: [...nucleiTemplateFilter.severities],
    protocols: [...nucleiTemplateFilter.protocols],
    products: [...nucleiTemplateFilter.products],
    hasCve: nucleiTemplateFilter.hasCve
  }
}

// 构建自定义POC筛选请求参数（与 POC 管理页一致）
function buildCustomPocFilterPayload() {
  return {
    keyword: customPocFilter.keyword,
    tag: customPocFilter.tag,
    severities: [...customPocFilter.severities],
    protocols: [...customPocFilter.protocols],
    products: [...customPocFilter.products],
    hasCve: customPocFilter.hasCve
  }
}

async function loadNucleiTemplateFacets() {
  try {
    const res = await getNucleiTemplateCategories(buildNucleiTemplateFilterPayload())
    if (res.code === 0) {
      nucleiTemplateFacets.categories = res.categories || []
      nucleiTemplateFacets.severities = res.severities || []
      nucleiTemplateFacets.protocols = res.protocols || []
      nucleiTemplateFacets.products = res.products || []
      nucleiTemplateFacets.tags = res.tags || []
      nucleiTemplateFacets.cveTrue = res.cveStats?.true || 0
      nucleiTemplateFacets.cveFalse = res.cveStats?.false || 0
    }
  } catch (e) {
    console.error('Load nuclei template facets failed:', e)
  }
}

async function loadCustomPocFacets() {
  try {
    const res = await getCustomPocCategories(buildCustomPocFilterPayload())
    if (res.code === 0) {
      customPocFacets.severities = res.severities || []
      customPocFacets.protocols = res.protocols || []
      customPocFacets.products = res.products || []
      customPocFacets.tags = res.tags || []
      customPocFacets.cveTrue = res.cveStats?.true || 0
      customPocFacets.cveFalse = res.cveStats?.false || 0
    }
  } catch (e) {
    console.error('Load custom poc facets failed:', e)
  }
}

// 应用筛选：重置到第一页，列表与分面计数同时刷新
function searchNucleiTemplatesForSelect() {
  nucleiTemplatePagination.page = 1
  loadNucleiTemplatesForSelect()
  loadNucleiTemplateFacets()
}

function searchCustomPocsForSelect() {
  customPocPagination.page = 1
  loadCustomPocsForSelect()
  loadCustomPocFacets()
}

// 重置默认模板筛选条件
function resetNucleiTemplateFilter() {
  nucleiTemplateFilter.keyword = ''
  nucleiTemplateFilter.tag = ''
  nucleiTemplateFilter.category = ''
  nucleiTemplateFilter.severities = []
  nucleiTemplateFilter.protocols = []
  nucleiTemplateFilter.products = []
  nucleiTemplateFilter.hasCve = null
  searchNucleiTemplatesForSelect()
}

// 重置自定义POC筛选条件
function resetCustomPocFilter() {
  customPocFilter.keyword = ''
  customPocFilter.tag = ''
  customPocFilter.severities = []
  customPocFilter.protocols = []
  customPocFilter.products = []
  customPocFilter.hasCve = null
  searchCustomPocsForSelect()
}

async function loadNucleiTemplatesForSelect() {
  nucleiTemplateLoading.value = true
  isLoadingData.value = true
  try {
    const res = await getNucleiTemplateList({
      page: nucleiTemplatePagination.page, pageSize: nucleiTemplatePagination.pageSize,
      ...buildNucleiTemplateFilterPayload()
    })
    if (res.code === 0) {
      nucleiTemplateList.value = res.list || []
      nucleiTemplatePagination.total = res.total || 0
      await nextTick()
      restoreNucleiTableSelection()
    }
  } catch (error) {
    // Select 的 change 事件不会 await 此请求；在此消费拒绝，避免取消或网络错误成为未处理 Promise。
    console.error('Load Nuclei templates failed:', error)
  } finally {
    nucleiTemplateLoading.value = false
    // 延迟重置，避免 toggleRowSelection 触发的 selection-change 覆盖跨页选择
    setTimeout(() => { isLoadingData.value = false }, 100)
  }
}

// 恢复当前页 Nuclei 表格的选中状态（不影响其他页）
function restoreNucleiTableSelection() {
  if (!nucleiTableRef.value) return
  // 全选状态下表格选择被禁用，无需恢复
  if (nucleiSelectAll.value || customPocSelectAll.value) return
  const selectedIds = new Set(selectedNucleiTemplateIds.value)
  nucleiTemplateList.value.forEach(row => {
    if (selectedIds.has(row.id)) {
      nucleiTableRef.value.toggleRowSelection(row, true)
    }
  })
}

async function loadCustomPocsForSelect() {
  customPocLoading.value = true
  isLoadingData.value = true
  try {
    const res = await getCustomPocList({
      page: customPocPagination.page, pageSize: customPocPagination.pageSize,
      ...buildCustomPocFilterPayload(),
      enabled: true // 只显示启用的POC
    })
    if (res.code === 0) {
      customPocList.value = res.list || []
      customPocPagination.total = res.total || 0
      await nextTick()
      restoreCustomPocTableSelection()
    }
  } catch (error) {
    // Select 的 change 事件不会 await 此请求；在此消费拒绝，避免取消或网络错误成为未处理 Promise。
    console.error('Load custom POC failed:', error)
  } finally {
    customPocLoading.value = false
    setTimeout(() => { isLoadingData.value = false }, 100)
  }
}

// 恢复当前页自定义 POC 表格的选中状态
function restoreCustomPocTableSelection() {
  if (!customPocTableRef.value) return
  // 全选状态下表格选择被禁用，无需恢复
  if (nucleiSelectAll.value || customPocSelectAll.value) return
  const selectedIds = new Set(selectedCustomPocIds.value)
  customPocList.value.forEach(row => {
    if (selectedIds.has(row.id)) {
      customPocTableRef.value.toggleRowSelection(row, true)
    }
  })
}

async function handlePocDialogOpen() {
  // 并行加载两个 Tab 的首页数据与筛选维度统计（restore 在各自 load 内完成）
  await Promise.all([loadNucleiTemplatesForSelect(), loadCustomPocsForSelect(), loadNucleiTemplateFacets(), loadCustomPocFacets()])
}

function handleDictSelectionChange(val) { selectedDictRows.value = val }
function handleSubdomainDictSelectionChange(val) { selectedSubdomainDictRows.value = val }
function handleRecursiveDictSelectionChange(val) { selectedRecursiveDictRows.value = val }

function handleNucleiSelectionChange(selection) {
  // 数据加载或"选择全部"期间跳过，避免覆盖跨页选择
  if (isSelectingAll.value || isLoadingData.value) return
  // 全选状态下表格勾选不生效（选择意图已由全选标记表达）
  if (nucleiSelectAll.value) {
    if (nucleiTableRef.value) nucleiTableRef.value.clearSelection()
    return
  }

  const currentPageIds = new Set(nucleiTemplateList.value.map(t => t.id))
  const currentPageSelectedIds = new Set(selection.map(t => t.id))
  const currentPageSelectedItems = selection.filter(t => currentPageIds.has(t.id))

  // 保留其他页的 ID
  const newSelectedIds = selectedNucleiTemplateIds.value.filter(id => !currentPageIds.has(id))
  currentPageSelectedIds.forEach(id => newSelectedIds.push(id))
  selectedNucleiTemplateIds.value = newSelectedIds

  // 保留其他页的对象
  const otherPageItems = selectedNucleiTemplates.value.filter(t => !currentPageIds.has(t.id))
  selectedNucleiTemplates.value = [...otherPageItems, ...currentPageSelectedItems]
}

function handleCustomPocSelectionChange(selection) {
  if (isSelectingAll.value || isLoadingData.value) return
  // 全选状态下表格勾选不生效（选择意图已由全选标记表达）
  if (customPocSelectAll.value) {
    if (customPocTableRef.value) customPocTableRef.value.clearSelection()
    return
  }

  const currentPageIds = new Set(customPocList.value.map(p => p.id))
  const currentPageSelectedIds = new Set(selection.map(p => p.id))
  const currentPageSelectedItems = selection.filter(p => currentPageIds.has(p.id))

  const newSelectedIds = selectedCustomPocIds.value.filter(id => !currentPageIds.has(id))
  currentPageSelectedIds.forEach(id => newSelectedIds.push(id))
  selectedCustomPocIds.value = newSelectedIds

  const otherPageItems = selectedCustomPocs.value.filter(p => !currentPageIds.has(p.id))
  selectedCustomPocs.value = [...otherPageItems, ...currentPageSelectedItems]
}

async function selectAllNucleiTemplates() {
  selectAllNucleiLoading.value = true
  isSelectingAll.value = true
  try {
    // 记录全选条件（当前对话框筛选，与列表接口参数一致，由后端按相同条件展开）
    Object.assign(nucleiSelectAllFilter, buildNucleiTemplateFilterPayload())

    // 只查询数量（pageSize=1），不加载列表
    const res = await getNucleiTemplateList({ page: 1, pageSize: 1, ...buildNucleiTemplateFilterPayload() })
    if (res.code !== 0) return
    const total = res.total || 0
    if (total === 0) {
      ElMessage.warning(t('task.noMatchingTemplate'))
      resetNucleiSelectAll()
      return
    }
    // 进入全选状态：清空手动选择列表
    nucleiSelectAll.value = true
    nucleiSelectAllCount.value = total
    selectedNucleiTemplateIds.value = []
    selectedNucleiTemplates.value = []
    if (nucleiTableRef.value) nucleiTableRef.value.clearSelection()
    ElMessage.success(t('task.allSelectedCount', { count: total }))
  } catch (e) {
    console.error('selectAllNucleiTemplatesFailed', e)
    resetNucleiSelectAll()
    ElMessage.error(t('task.selectAllFailed') || 'Select all failed')
  } finally {
    selectAllNucleiLoading.value = false
    isSelectingAll.value = false
  }
}

function deselectAllNucleiTemplates() {
  clearNucleiSelections()
  ElMessage.success(t('task.allTemplatesDeselected'))
}

async function selectAllCustomPocs() {
  selectAllCustomLoading.value = true
  isSelectingAll.value = true
  try {
    // 记录全选条件（当前对话框筛选，与列表接口参数一致，由后端按相同条件展开）
    Object.assign(customPocSelectAllFilter, buildCustomPocFilterPayload())
    // 只查询数量（pageSize=1），不加载列表
    const res = await getCustomPocList({ page: 1, pageSize: 1, ...buildCustomPocFilterPayload(), enabled: true })
    if (res.code !== 0) return
    const total = res.total || 0
    if (total === 0) {
      ElMessage.warning(t('task.noMatchingPoc'))
      resetCustomPocSelectAll()
      return
    }
    // 进入全选状态：清空手动选择列表
    customPocSelectAll.value = true
    customPocSelectAllCount.value = total
    selectedCustomPocIds.value = []
    selectedCustomPocs.value = []
    if (customPocTableRef.value) customPocTableRef.value.clearSelection()
    ElMessage.success(t('task.allSelectedCount', { count: total }))
  } catch (e) {
    console.error('selectAllCustomPocsFailed', e)
    resetCustomPocSelectAll()
    ElMessage.error(t('task.selectAllFailed') || 'Select all failed')
  } finally {
    selectAllCustomLoading.value = false
    isSelectingAll.value = false
  }
}

function deselectAllCustomPocs() {
  clearCustomPocSelections()
  ElMessage.success(t('task.allPocsDeselected'))
}

function clearAllSelections() {
  selectedNucleiTemplates.value = []
  selectedNucleiTemplateIds.value = []
  selectedCustomPocs.value = []
  selectedCustomPocIds.value = []
  resetNucleiSelectAll()
  resetCustomPocSelectAll()
  if (nucleiTableRef.value) nucleiTableRef.value.clearSelection()
  if (customPocTableRef.value) customPocTableRef.value.clearSelection()
}

// 重置默认模板全选状态
function resetNucleiSelectAll() {
  nucleiSelectAll.value = false
  nucleiSelectAllCount.value = 0
  nucleiSelectAllFilter.keyword = ''
  nucleiSelectAllFilter.tag = ''
  nucleiSelectAllFilter.category = ''
  nucleiSelectAllFilter.severities = []
  nucleiSelectAllFilter.protocols = []
  nucleiSelectAllFilter.products = []
  nucleiSelectAllFilter.hasCve = null
}

// 重置自定义POC全选状态
function resetCustomPocSelectAll() {
  customPocSelectAll.value = false
  customPocSelectAllCount.value = 0
  customPocSelectAllFilter.keyword = ''
  customPocSelectAllFilter.tag = ''
  customPocSelectAllFilter.severities = []
  customPocSelectAllFilter.protocols = []
  customPocSelectAllFilter.products = []
  customPocSelectAllFilter.hasCve = null
}

function clearNucleiSelections() {
  selectedNucleiTemplates.value = []
  selectedNucleiTemplateIds.value = []
  resetNucleiSelectAll()
  if (nucleiTableRef.value) nucleiTableRef.value.clearSelection()
}

function clearCustomPocSelections() {
  selectedCustomPocs.value = []
  selectedCustomPocIds.value = []
  resetCustomPocSelectAll()
  if (customPocTableRef.value) customPocTableRef.value.clearSelection()
}

function getSeverityType(severity) {
  const map = { critical: 'danger', high: 'warning', medium: 'primary', low: 'info', info: 'info', unknown: 'info' }
  return map[severity?.toLowerCase()] || 'info'
}

function disabledDate(time) { return time.getTime() < Date.now() - 86400000 }
function onScheduleTimeChange(val) { form.scheduleTime = val }
function handlePocModeChange(val) {
  if (val !== 'manual') {
    form.pocscanNucleiTemplateIds = []
    form.pocscanCustomPocIds = []
    selectedNucleiTemplates.value = []
    selectedCustomPocs.value = []
    selectedNucleiTemplateIds.value = []
    selectedCustomPocIds.value = []
    resetNucleiSelectAll()
    resetCustomPocSelectAll()
  }
}

watch(() => pocSelectTab.value, (newVal) => {
  if (newVal === 'nuclei' && nucleiTemplateList.value.length === 0) {
    loadNucleiTemplatesForSelect()
  } else if (newVal === 'custom' && customPocList.value.length === 0) {
    loadCustomPocsForSelect()
  }
})

// 切换到"选择资产"模式时自动加载资产列表
watch(() => form.targetMode, (newMode) => {
  if (newMode === 'asset' && assetTargetList.value.length === 0) {
    assetTargetPagination.page = 1
    loadAssetTargets()
  }
})

function fillFormFromRow(row) {
  isEdit.value = true
  // 重置Cron验证状态
  cronValidation.valid = false
  cronValidation.nextTimes = []
  cronValidation.error = ''
  // 重置资产选择状态
  selectedAssetRows.value = []
  assetTargetPagination.page = 1
  assetTargetFilter.keyword = ''
  selectedTemplateConfig.value = null
  // 重置 POC 选择状态
  selectedNucleiTemplates.value = []
  selectedCustomPocs.value = []
  selectedNucleiTemplateIds.value = []
  selectedCustomPocIds.value = []
  Object.assign(form, getDefaultForm())
  // 回填字段
  form.id = row.id
  form.name = row.name
  form.scheduleType = row.scheduleType
  form.cronSpec = row.cronSpec
  form.scheduleTime = row.scheduleTime
  form.scheduleTimeDate = row.scheduleTime || null
  // 目标模式回填（兼容旧数据：有 assetIds 走资产模式，否则手动输入）
  if (row.targetMode === 'asset' || (row.assetIds && row.assetIds.length)) {
    form.targetMode = 'asset'
    form.target = ''
    form.orgId = row.orgId || ''
    form.assetIds = row.assetIds || []
    form.enableSubdomainPull = !!row.enableSubdomainPull
    // 加载资产列表并恢复选中
    loadAssetTargets()
  } else {
    form.targetMode = 'manual'
    form.target = row.target || ''
    form.orgId = ''
    form.assetIds = []
    form.enableSubdomainPull = false
  }
  // 扫描配置来源回填（兼容旧数据：有 templateId 走模板，否则自定义）
  if (row.configSource === 'template' || row.templateId) {
    form.configSource = 'template'
    form.templateId = row.templateId || ''
    if (form.templateId) onTemplateSelect(form.templateId)
  } else {
    form.configSource = 'custom'
    form.templateId = ''
  }
  form.config = row.config
  // 使用 applyConfig 正确将嵌套配置映射到扁平表单字段
  if (row.config) {
    try {
      const configObj = typeof row.config === 'string' ? JSON.parse(row.config) : row.config
      applyConfig(configObj)
    } catch (e) {
      console.error('parseConfigFailed', e)
    }
  }
  // 编辑回显：根据已保存的ID加载POC/字典名称
  loadEditSelectionNames()
}

// 分页拉取直到命中所有 ID 或遍历完总数；用于编辑回显 POC 名称（避免 pageSize 截断）
async function fetchMatchingByIds(apiFn, idSet, matcher) {
  const pageSize = 5000
  const firstRes = await apiFn({ page: 1, pageSize })
  if (firstRes.code !== 0 || !firstRes.list) return []
  const matched = firstRes.list.filter(matcher)
  const total = firstRes.total || firstRes.list.length
  const totalPages = Math.ceil(total / pageSize)
  for (let page = 2; page <= totalPages; page++) {
    if (matched.length >= idSet.size) break
    const res = await apiFn({ page, pageSize })
    if (res.code === 0 && res.list) matched.push(...res.list.filter(matcher))
  }
  return matched
}

// 编辑回显时，根据已保存的ID批量加载POC模板名称和字典名称
async function loadEditSelectionNames() {
  // 恢复 POC 模板名称
  if (form.pocscanNucleiTemplateIds.length > 0) {
    try {
      const idSet = new Set(form.pocscanNucleiTemplateIds)
      const matched = await fetchMatchingByIds(getNucleiTemplateList, idSet, t => idSet.has(t.id))
      selectedNucleiTemplates.value = matched
      selectedNucleiTemplateIds.value = matched.map(t => t.id)
      form.pocscanNucleiTemplates = matched
    } catch (e) {
      console.error('loadNucleiTemplateNamesFailed', e)
    }
  } else {
    selectedNucleiTemplates.value = []
    selectedNucleiTemplateIds.value = []
    form.pocscanNucleiTemplates = []
  }
  if (form.pocscanCustomPocIds.length > 0) {
    try {
      const idSet = new Set(form.pocscanCustomPocIds)
      const matched = await fetchMatchingByIds(getCustomPocList, idSet, p => idSet.has(p.id))
      selectedCustomPocs.value = matched
      selectedCustomPocIds.value = matched.map(p => p.id)
      form.pocscanCustomPocs = matched
    } catch (e) {
      console.error('loadCustomPocNamesFailed', e)
    }
  } else {
    selectedCustomPocs.value = []
    selectedCustomPocIds.value = []
    form.pocscanCustomPocs = []
  }
  // 恢复目录扫描字典名称
  if (form.dirscanDictIds.length > 0) {
    try {
      const res = await getDirScanDictEnabledList()
      if (res.code === 0 && res.data) {
        form.dirscanDicts = res.data.filter(d => form.dirscanDictIds.includes(d.id))
      }
    } catch (e) {
      console.error('loadDirScanDictNamesFailed', e)
    }
  }
  // 恢复子域名字典名称
  if (form.subdomainDictIds.length > 0) {
    try {
      const res = await getSubdomainDictEnabledList()
      if (res.code === 0 && res.data) {
        form.subdomainDicts = res.data.filter(d => form.subdomainDictIds.includes(d.id))
      }
    } catch (e) {
      console.error('loadSubdomainDictNamesFailed', e)
    }
  }
  // 恢复递归字典名称
  if (form.recursiveDictIds.length > 0) {
    try {
      const res = await getSubdomainDictEnabledList()
      if (res.code === 0 && res.data) {
        form.recursiveDicts = res.data.filter(d => form.recursiveDictIds.includes(d.id))
      }
    } catch (e) {
      console.error('loadRecursiveDictNamesFailed', e)
    }
  }
}

function showDictSelectDialog() {
  dictSelectDialogVisible.value = true
}

function showSubdomainDictSelectDialog() {
  subdomainDictSelectDialogVisible.value = true
}

function showRecursiveDictSelectDialog() {
  recursiveDictSelectDialogVisible.value = true
}

function showPocSelectDialog() {
  // 从 form 恢复已选 ID 和对象（编辑回显后打开对话框时必需）
  selectedNucleiTemplateIds.value = [...(form.pocscanNucleiTemplateIds || [])]
  selectedCustomPocIds.value = [...(form.pocscanCustomPocIds || [])]
  selectedNucleiTemplates.value = [...(form.pocscanNucleiTemplates || [])]
  selectedCustomPocs.value = [...(form.pocscanCustomPocs || [])]
  selectedPocSearchKeyword.value = ''
  pocSelectDialogVisible.value = true
}



function confirmDictSelection() {
  if (!form.dirscanDicts) form.dirscanDicts = []
  form.dirscanDicts = [...selectedDictRows.value]
  form.dirscanDictIds = selectedDictRows.value.map(item => item.id)
  dictSelectDialogVisible.value = false
}

function confirmSubdomainDictSelection() {
  if (!form.subdomainDicts) form.subdomainDicts = []
  form.subdomainDicts = [...selectedSubdomainDictRows.value]
  form.subdomainDictIds = selectedSubdomainDictRows.value.map(item => item.id)
  subdomainDictSelectDialogVisible.value = false
}

function confirmRecursiveDictSelection() {
  if (!form.recursiveDicts) form.recursiveDicts = []
  form.recursiveDicts = [...selectedRecursiveDictRows.value]
  form.recursiveDictIds = selectedRecursiveDictRows.value.map(item => item.id)
  recursiveDictSelectDialogVisible.value = false
}

function confirmPocSelection() {
  form.pocscanNucleiTemplateIds = [...selectedNucleiTemplateIds.value]
  form.pocscanCustomPocIds = [...selectedCustomPocIds.value]
  // 保存对象信息用于下次打开对话框时显示
  form.pocscanNucleiTemplates = [...selectedNucleiTemplates.value]
  form.pocscanCustomPocs = [...selectedCustomPocs.value]
  pocSelectDialogVisible.value = false
}

function removeNucleiTemplate(id) {
  selectedNucleiTemplateIds.value = selectedNucleiTemplateIds.value.filter(i => i !== id)
  selectedNucleiTemplates.value = selectedNucleiTemplates.value.filter(item => item.id !== id)
  // 同步表格选中状态
  if (nucleiTableRef.value) {
    const row = nucleiTemplateList.value.find(t => t.id === id)
    if (row) nucleiTableRef.value.toggleRowSelection(row, false)
  }
}

function removeCustomPoc(id) {
  selectedCustomPocIds.value = selectedCustomPocIds.value.filter(i => i !== id)
  selectedCustomPocs.value = selectedCustomPocs.value.filter(item => item.id !== id)
  if (customPocTableRef.value) {
    const row = customPocList.value.find(p => p.id === id)
    if (row) customPocTableRef.value.toggleRowSelection(row, false)
  }
}

function viewPocContent(row, type) {
  currentViewPoc.value = { name: row.name, content: row.content || "Content preview not loaded." }
  pocContentDialogVisible.value = true
}

function copyPocContent() {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(currentViewPoc.value.content)
  }
}

// 截取目标显示（保留供内部使用）
function truncateTarget(target, maxLen = 40) {
  if (!target) return ''
  const firstLine = target.split('\n')[0]
  if (firstLine.length > maxLen) {
    return firstLine.substring(0, maxLen) + '...'
  }
  return firstLine
}

// 返回列表
function handleBack() {
  if (window.history.length > 1) {
    router.back()
  } else {
    router.push('/cron-task')
  }
}

// 重置新建表单
function resetForm() {
  isEdit.value = false
  Object.assign(form, getDefaultForm())
  selectedNucleiTemplates.value = []
  selectedCustomPocs.value = []
  selectedNucleiTemplateIds.value = []
  selectedCustomPocIds.value = []
  selectedAssetRows.value = []
  selectedTemplateConfig.value = null
  assetTargetPagination.page = 1
  assetTargetFilter.keyword = ''
  cronValidation.valid = false
  cronValidation.nextTimes = []
  cronValidation.error = ''
}

// 加载编辑详情
async function loadDetail(id) {
  try {
    // 优先使用详情接口
    let row = null
    try {
      const detailRes = await getCronTaskDetail({ id })
      if (detailRes.code === 0 && detailRes.data) {
        row = detailRes.data
      }
    } catch (e) {
      console.warn('getCronTaskDetail failed, fallback to list:', e)
    }
    // 兜底：从列表接口中按 ID 过滤
    if (!row) {
      const listRes = await getCronTaskList({ page: 1, pageSize: 100, keyword: '', taskType: 'scan' })
      if (listRes.code === 0) {
        const list = listRes.data?.list || listRes.list || []
        row = list.find(item => String(item.id) === String(id))
      }
    }
    if (row) {
      fillFormFromRow(row)
    } else {
      ElMessage.error('未找到该定时任务')
      router.push('/cron-task')
    }
  } catch (e) {
    console.error('loadCronTaskDetailFailed:', e)
    ElMessage.error('加载任务详情失败')
  }
}

onMounted(async () => {
  await loadOrganizations()
  await loadScanTemplates()
  // 编辑模式：根据路由参数加载数据
  if (route.params.id) {
    await loadDetail(route.params.id)
  } else {
    resetForm()
  }
})
</script>

<style scoped>
.cron-task-create-page {
  width: 100%;
}

.page-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.page-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.create-card {
  margin-bottom: 20px;
  width: 100%;
}

.create-card :deep(.el-card__body) {
  width: 100%;
}

.cron-task-form {
  padding: 20px 40px;
}

.form-actions {
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  display: flex;
  justify-content: center;
  gap: 12px;
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
  margin: 20px 0;
  width: 100%;
}

.config-collapse :deep(.el-collapse-item__header) {
  background: var(--el-fill-color-light);
  padding: 0 16px;
  font-size: 14px;
  font-weight: 500;
  height: 44px;
  line-height: 44px;
}

.config-collapse :deep(.el-collapse-item__header):hover {
  background: var(--el-fill-color);
}

.config-collapse :deep(.el-collapse-item__wrap) {
  border: none;
}

.config-collapse :deep(.el-collapse-item__content) {
  padding: 20px 16px;
}

.collapse-title-wrapper {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: 1;
  padding-right: 8px;
  gap: 8px;
}

.collapse-title {
  font-weight: 500;
}

.config-summary {
  flex: 1;
  text-align: right;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.module-desc {
  margin: 0 0 16px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.scan-tools-layout {
  margin-top: 10px;
}

.scan-tool-section {
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  padding: 16px;
  min-height: 280px;
}

.scan-tool-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.scan-tool-title {
  font-weight: 600;
  font-size: 14px;
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
  height: 520px;
}

.poc-select-left {
  flex: 1;
  overflow: auto;
  min-width: 0;
}

.poc-select-right {
  width: 340px;
  flex-shrink: 0;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
  display: flex;
  flex-direction: column;
  background: var(--el-fill-color-blank);
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

.select-all-tip {
  padding: 6px 12px;
  margin-bottom: 8px;
  background: var(--el-color-success-light-9);
  color: var(--el-color-success);
  border-radius: 4px;
  font-size: 12px;
}

.selected-all-conditions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 4px 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
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
  flex-direction: column;
  gap: 4px;
}

.selected-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 5px 10px;
  background: var(--el-fill-color-light);
  border-radius: 3px;
  font-size: 12px;
}

.selected-item:hover {
  background: var(--el-fill-color);
}

.item-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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

/* ===== 目标模式切换 ===== */
.target-mode-switch,
.config-source-switch {
  width: 100%;
}

/* ===== 资产选择器 ===== */
.asset-selector {
  width: 100%;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 12px;
  background: var(--el-fill-color-blank);
}

.asset-selector-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 10px;
}

.selected-count-hint {
  margin-left: auto;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.asset-target-table {
  width: 100%;
}

.asset-label-tag {
  margin-right: 4px;
  margin-bottom: 2px;
}

.asset-pagination {
  margin-top: 10px;
  justify-content: flex-end;
}

/* ===== 扫描模板选项 ===== */
.template-option {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.template-option .template-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.template-option .template-category {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  margin-left: 10px;
  flex-shrink: 0;
}
</style>
