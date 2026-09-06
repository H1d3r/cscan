<template>
  <div class="task-create-page">
    <el-card class="create-card">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px" class="task-form">
        <!-- 模板选择 -->
        <ScanTemplateSelect v-model="selectedTemplate" :show-save-button="!isEdit" :current-config="currentConfig"
          @config-loaded="handleTemplateConfigLoaded" />

        <!-- 基本信息 -->
        <el-form-item :label="$t('task.taskName')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('task.pleaseEnterTaskName')" />
        </el-form-item>
        <el-form-item :label="$t('task.scanTarget')" prop="target">
          <el-input v-model="form.target" type="textarea" :rows="6" :placeholder="$t('task.targetPlaceholder')" />
          <div v-if="targetStats.total > 0" style="margin-top: 6px; font-size: 12px; color: var(--el-text-color-secondary);">
            <span :style="{ color: targetStats.invalid > 0 ? 'var(--el-color-danger)' : '' }">{{
              $t('task.targetCount', { total: targetStats.total, unique: targetStats.unique, invalid: targetStats.invalid })
            }}</span>
          </div>
        </el-form-item>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item :label="$t('task.organization')">
              <el-select v-model="form.orgId" :placeholder="$t('task.selectOrganization')" clearable
                style="width: 100%">
                <el-option v-for="org in organizations" :key="org.id" :label="org.name" :value="org.id" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="$t('task.specifyWorker')">
          <el-select v-model="form.workers" multiple :placeholder="$t('task.anyWorkerExecute')" clearable
            style="width: 100%">
            <el-option v-for="w in workers" :key="w.name" :label="`${w.name} (${w.ip})`" :value="w.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('task.tags')">
          <el-select v-model="form.tags" multiple filterable allow-create default-first-option
            :placeholder="$t('task.tagsPlaceholder')" style="width: 100%">
            <el-option v-for="tag in commonTags" :key="tag" :label="tag" :value="tag" />
          </el-select>
          <span class="form-hint">{{ $t('task.tagsHint') }}</span>
        </el-form-item>
        <!-- 可折叠配置区域 -->
        <el-collapse v-model="activeCollapse" class="config-collapse" @change="handleCollapseChange">
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
                    <el-input-number v-model="form.portscanWorkers" :min="10" :max="500" style="width:100%" />
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
                    <el-input-number v-model="form.portscanTargetTimeout" :min="5" :max="3600" style="width:100%" />
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
              <!-- 强制扫描：仅在前序阶段均未启用时显示 -->
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
              <!-- 强制扫描：仅在前序阶段均未启用时显示 -->
              <el-form-item v-if="!hasPrePhaseEnabled" :label="$t('task.forceScan')">
                <el-switch v-model="form.jsfinderForceScan" />
                <span class="form-hint warning-hint">{{ $t('task.forceScanHint') }}</span>
              </el-form-item>
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
        </el-collapse>

        <!-- 操作按钮 -->
        <div class="form-actions">
          <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ isEdit ? $t('common.save') :
            $t('task.createTask') }}</el-button>
          <el-button @click="handleCancel">{{ $t('common.cancel') }}</el-button>
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
    <el-dialog v-model="pocSelectDialogVisible" :title="$t('task.selectPoc')" width="1200px"
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
                <el-table-column prop="tags" :label="$t('task.tags')" min-width="100">
                  <template #default="{ row }">
                    <el-tag v-for="tag in (row.tags || []).slice(0, 2)" :key="tag" size="small"
                      style="margin-right: 3px">{{ tag }}</el-tag>
                    <span v-if="row.tags && row.tags.length > 2" class="secondary-hint">+{{ row.tags.length - 2
                      }}</span>
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
        <el-descriptions-item :label="$t('task.tags')" :span="2">
          <el-tag v-for="tag in (currentViewPoc.tags || [])" :key="tag" size="small" style="margin-right: 5px">{{ tag
            }}</el-tag>
          <span v-if="!currentViewPoc.tags || currentViewPoc.tags.length === 0">-</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('common.description')" :span="2">{{ currentViewPoc.description || '-'
          }}</el-descriptions-item>
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
import { ref, reactive, onMounted, watch, nextTick, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Close, Search, InfoFilled } from '@element-plus/icons-vue'
import { createTask, updateTask, getTaskDetail, startTask, getWorkerList, getScanConfig, saveScanConfig } from '@/api/task'
import { getAssetTargetDetail } from '@/api/asset'
import { getNucleiTemplateList, getCustomPocList, getNucleiTemplateDetail, getNucleiTemplateCategories, getCustomPocCategories } from '@/api/poc'
import { getDirScanDictEnabledList } from '@/api/dirscan'
import { getSubdomainDictEnabledList } from '@/api/subdomain'
import ScanTemplateSelect from '@/components/ScanTemplateSelect.vue'
import request from '@/api/request'
import { validateTargets, formatValidationErrors, validateSingleTarget } from '@/utils/target'
import { isValidDirScanStatusCode, normalizeDirScanStatusCodes } from '@/utils/dirscan'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const formRef = ref()
const submitting = ref(false)
const organizations = ref([])
const workers = ref([])
const commonTags = ref([]) // 常用标签列表
const activeCollapse = ref([])
const isEdit = ref(false)

// 处理 Collapse 状态变化，移除焦点避免 aria-hidden 冲突
function handleCollapseChange() {
  if (document.activeElement instanceof HTMLElement) {
    document.activeElement.blur()
  }
}
const selectedTemplate = ref(null)

// 计算当前配置（用于保存为模板）
const currentConfig = computed(() => buildConfig())

// POC选择相关
const pocSelectDialogVisible = ref(false)
const pocSelectTab = ref('nuclei')
const nucleiTemplateList = ref([])

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

const customPocList = ref([])
const nucleiTemplateLoading = ref(false)
const customPocLoading = ref(false)
const selectAllNucleiLoading = ref(false)
const selectAllCustomLoading = ref(false)
// 标志位：防止选择全部或加载数据时selection-change清空数据
const isSelectingAll = ref(false)
const isLoadingData = ref(false)
// 查看POC内容相关
const pocContentDialogVisible = ref(false)
const pocContentLoading = ref(false)
const pocContentTitle = ref('')
const currentViewPoc = ref({})
const selectedNucleiTemplateIds = ref([])
const selectedCustomPocIds = ref([])
// 存储已选择的完整对象（用于显示名称）
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

// 目录扫描常用状态码预设
const commonStatusCodes = [200, 204, 301, 302, 307, 308, 401, 403, 405, 500]

const form = reactive({
  id: '',
  name: '',
  target: '',
  orgId: '',
  tags: [], // 任务标签
  workers: [],
  // batchSize 由后端自动计算，前端不再设置
  // 子域名扫描
  domainscanEnable: false,
  domainscanSubfinder: true,
  domainscanBruteforce: false, // 字典爆破
  domainscanBruteforceTimeout: 30, // KSubdomain 超时时间（分钟）
  domainscanTimeout: 300,
  domainscanMaxEnumTime: 10,
  domainscanThreads: 10,
  domainscanRateLimit: 0,
  domainscanRemoveWildcard: true,
  domainscanResolveDNS: true,
  domainscanConcurrent: 50,
  subdomainDictIds: [], // 子域名暴力破解字典
  subdomainDicts: [], // 保存已选择的字典信息
  // KSubdomain增强功能
  domainscanRecursiveBrute: false, // 递归爆破
  recursiveDictIds: [], // 递归爆破字典ID列表
  recursiveDicts: [], // 保存已选择的递归字典信息
  domainscanWildcardDetect: true,  // 泛解析检测
  // 端口扫描
  portscanEnable: true,
  portscanTool: 'naabu',
  portscanRate: 3000, // 提高默认值从1000到3000
  ports: 'top100',
  portThreshold: 100,
  scanType: 'c',
  portscanTargetTimeout: 120,
  portscanProbeTimeoutMs: 1000,
  skipHostDiscovery: false,
  excludeCDN: false,
  excludeHosts: '',
  portscanWorkers: 50, // Naabu内部工作线程，默认50
  portscanRetries: 2, // 重试次数，默认2
  portscanWarmUpTime: 1, // 预热时间(秒)，默认1
  portscanVerify: false, // TCP验证，默认false
  // 端口识别
  portidentifyEnable: false,
  portidentifyTool: 'nmap',
  portidentifyTimeout: 30,
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
  fingerprintTimeout: 30,
  fingerprintFilterMode: 'http_mapping', // 过滤模式: http_mapping(HTTP映射) 或 service_mapping(服务映射)
  fingerprintForceScan: false,
  // 弱口令扫描
  brutescanEnable: false,
  brutescanServices: [],
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
  pocscanForceScan: false,
  pocscanNucleiTemplateIds: [],
  pocscanCustomPocIds: [],
  // 自定义HTTP头部
  pocscanHeaderMode: 'none', // none / preset / custom
  pocscanPresetUA: '',
  pocscanCustomHeadersText: '',
  // 保存已选择的对象信息（用于显示名称）
  pocscanNucleiTemplates: [],
  pocscanCustomPocs: [],
  // 目录扫描
  dirscanEnable: false,
  dirscanTool: 'ffuf',
  dirscanDictIds: [],
  dirscanDicts: [], // 保存已选择的字典信息
  dirscanFollowRedirect: false,
  dirscanForceScan: false,
  dirscanStatusCodes: [],
  // ffuf 高级配置
  dirscanAutoCalibration: true,
  dirscanRecursion: false,
  dirscanRecursionDepth: 2,
  jsfinderEnable: false,
  jsfinderThreads: 10,
  jsfinderTimeout: 10,
  jsfinderEnableSourcemap: true,
  jsfinderEnableUnauthCheck: true,
  jsfinderForceScan: false
})

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

const targetValidator = (rule, value, callback) => {
  if (!value) { callback(new Error(t('task.pleaseEnterTarget'))); return }
  const errors = validateTargets(value)
  errors.length > 0 ? callback(new Error(formatValidationErrors(errors))) : callback()
}

const dirscanStatusCodesValidator = (_rule, value, callback) => {
  if (!form.dirscanEnable || (value || []).every(isValidDirScanStatusCode)) {
    callback()
    return
  }
  callback(new Error(t('task.invalidStatusCode')))
}

const rules = {
  name: [{ required: true, message: () => t('task.pleaseEnterTaskName'), trigger: 'blur' }],
  target: [{ required: true, message: () => t('task.pleaseEnterTarget'), trigger: 'blur' }, { validator: targetValidator, trigger: 'blur' }],
  dirscanStatusCodes: [{ validator: dirscanStatusCodesValidator, trigger: 'change' }]
}

onMounted(async () => {
  await loadOrganizations()
  await loadWorkers()
  await loadCommonTags()

  // 检查是否是编辑模式
  if (route.query.id) {
    isEdit.value = true
    await loadTaskDetail(route.query.id)
  } else {
    // 如果传入了 targetId，先加载目标信息
    if (route.query.targetId) {
      const targetIds = route.query.targetId.split(',').filter(Boolean)
      if (targetIds.length > 0) {
        try {
          const targets = []
          for (const tid of targetIds) {
            const res = await getAssetTargetDetail({ targetId: tid })
            if (res.code === 0 && res.data && res.data.meta) {
              targets.push(res.data.meta.targetValue)
            }
          }
          if (targets.length > 0) {
            form.target = targets.join('\n')
          }
        } catch (e) { console.error('Load target detail failed:', e) }
      }
    }
    // 加载用户上次保存的扫描配置（仅在未指定 targetId 时恢复 target 字段）
    try {
      const res = await getScanConfig()
      if (res.code === 0 && res.config) {
        const config = JSON.parse(res.config)
        // 未传入 targetId 时才恢复上次目标
        if (!route.query.targetId) {
          form.target = config.target || ''
        }
        if (config.name) form.name = config.name
        // 其余配置正常加载
        const isManualMode = !!(config.pocscan?.nucleiSelectAll || config.pocscan?.customPocSelectAll ||
          (config.pocscan?.nucleiTemplateIds?.length > 0) || (config.pocscan?.customPocIds?.length > 0))
        const hasBruteforce = config.domainscan?.subdomainDictIds?.length > 0
        isLoadingConfig = true
        Object.assign(form, {
          domainscanEnable: config.domainscan?.enable ?? false,
          domainscanSubfinder: config.domainscan?.subfinder ?? true,
          domainscanBruteforce: hasBruteforce,
          domainscanBruteforceTimeout: config.domainscan?.bruteforceTimeout || 30,
          domainscanTimeout: config.domainscan?.timeout || 300,
          domainscanMaxEnumTime: config.domainscan?.maxEnumerationTime || 10,
          domainscanThreads: config.domainscan?.threads || 10,
          domainscanRateLimit: config.domainscan?.rateLimit || 0,
          domainscanRemoveWildcard: config.domainscan?.removeWildcard ?? true,
          domainscanResolveDNS: config.domainscan?.resolveDNS ?? true,
          domainscanConcurrent: config.domainscan?.concurrent || 50,
          subdomainDictIds: config.domainscan?.subdomainDictIds || [],
          domainscanRecursiveBrute: config.domainscan?.recursiveBrute ?? false,
          recursiveDictIds: config.domainscan?.recursiveDictIds || [],
          domainscanWildcardDetect: config.domainscan?.wildcardDetect ?? true,
          portscanEnable: config.portscan?.enable ?? true,
          portscanTool: config.portscan?.tool || 'naabu',
          portscanRate: config.portscan?.rate || 1000,
          ports: config.portscan?.ports || 'top100',
          portThreshold: config.portscan?.portThreshold || 100,
          scanType: config.portscan?.scanType || 'c',
          portscanTargetTimeout: config.portscan?.targetTimeout ?? config.portscan?.timeout ?? 60,
          portscanProbeTimeoutMs: config.portscan?.probeTimeoutMs ?? 1000,
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
          fingerprintScreenshot: config.fingerprint?.screenshot ?? true,
          fingerprintCert: config.fingerprint?.cert ?? true,
          fingerprintActiveScan: config.fingerprint?.activeScan ?? false,
          fingerprintActiveTimeout: config.fingerprint?.activeTimeout || 10,
          fingerprintTimeout: config.fingerprint?.targetTimeout || 30,
          fingerprintFilterMode: config.fingerprint?.filterMode || 'http_mapping',
          brutescanEnable: config.brutescan?.enable ?? false,
          brutescanServices: config.brutescan?.services || [],
          brutescanThreads: config.brutescan?.threads || 20,
          brutescanTimeout: config.brutescan?.timeout || 5,
          brutescanDelayMs: config.brutescan?.delayMs || 100,
          brutescanStopOnFirst: config.brutescan?.stopOnFirst ?? false,
          brutescanForceScan: config.brutescan?.forceScan ?? false,
          pocscanEnable: config.pocscan?.enable ?? false,
          pocscanMode: isManualMode ? 'manual' : 'auto',
          pocscanAutoScan: config.pocscan?.autoScan ?? true,
          pocscanAutomaticScan: config.pocscan?.automaticScan ?? true,
          pocscanCustomOnly: config.pocscan?.customPocOnly ?? false,
          pocscanSeverity: config.pocscan?.severity ? config.pocscan.severity.split(',') : ['critical', 'high', 'medium'],
          pocscanTargetTimeout: config.pocscan?.targetTimeout || 600,
          pocscanNucleiTemplateIds: config.pocscan?.nucleiTemplateIds || [],
          pocscanCustomPocIds: config.pocscan?.customPocIds || [],
          dirscanEnable: config.dirscan?.enable ?? false,
          dirscanTool: config.dirscan?.tool || 'ffuf',
          dirscanDictIds: config.dirscan?.dictIds || [],
          dirscanFollowRedirect: config.dirscan?.followRedirect ?? false,
          dirscanForceScan: config.dirscan?.forceScan ?? false,
          dirscanStatusCodes: config.dirscan?.statusCodes || [],
          dirscanAutoCalibration: config.dirscan?.autoCalibration ?? true,
          dirscanRecursion: config.dirscan?.recursion ?? false,
          dirscanRecursionDepth: config.dirscan?.recursionDepth || 2,
          jsfinderEnable: config.jsfinder?.enable ?? false,
          jsfinderThreads: config.jsfinder?.threads || 10,
          jsfinderTimeout: config.jsfinder?.timeout || 10,
          jsfinderEnableSourcemap: config.jsfinder?.enableSourcemap ?? true,
          jsfinderEnableUnauthCheck: config.jsfinder?.enableUnauthCheck ?? true,
          jsfinderForceScan: config.jsfinder?.forceScan ?? false
        })
        isLoadingConfig = false
        nucleiSelectAll.value = !!config.pocscan?.nucleiSelectAll
        nucleiSelectAllCount.value = nucleiSelectAll.value ? (config.pocscan?.nucleiTemplateIds?.length || 0) : 0
        Object.assign(nucleiSelectAllFilter, config.pocscan?.nucleiSelectAllFilter || {})
      }
    } catch (e) { console.error('Load scan config failed:', e) }
  }
})

// 当启用主动指纹扫描时，自动启用自定义指纹引擎（主动扫描依赖自定义指纹引擎加载指纹）
watch(() => form.fingerprintActiveScan, (newVal) => {
  if (newVal && !form.fingerprintCustomEngine) {
    form.fingerprintCustomEngine = true
  }
})

// 当取消选择暴力破解字典时，自动取消勾选字典爆破
watch(() => form.subdomainDictIds, (newVal) => {
  if (!newVal || newVal.length === 0) {
    form.domainscanBruteforce = false
  }
}, { deep: true })

// 当取消选择递归字典时，自动取消勾选递归爆破
watch(() => form.recursiveDictIds, (newVal) => {
  if (!newVal || newVal.length === 0) {
    form.domainscanRecursiveBrute = false
  }
}, { deep: true })

// 端口范围与推荐扫描参数联动（基于5并发CLI进程计算）
// key 匹配 ports 选项值，value 为各参数推荐值
const PORT_SCAN_PRESETS = {
  '80,443,8080,8443': { targetTimeout: 60,  probeTimeoutMs: 500, workers: 50,  retries: 1, rate: 3000 },
  'top100':           { targetTimeout: 120, probeTimeoutMs: 500, workers: 50,  retries: 1, rate: 3000 },
  'top1000':          { targetTimeout: 240, probeTimeoutMs: 500, workers: 80,  retries: 1, rate: 5000 },
  '1-65535':          { targetTimeout: 900, probeTimeoutMs: 500, workers: 120, retries: 1, rate: 10000 },
}
let isLoadingConfig = false
watch(() => form.ports, (val) => {
  if (isLoadingConfig) return
  const preset = PORT_SCAN_PRESETS[val]
  if (!preset) return
  form.portscanTargetTimeout = preset.targetTimeout
  form.portscanProbeTimeoutMs = preset.probeTimeoutMs
  form.portscanWorkers = preset.workers
  form.portscanRetries = preset.retries
  form.portscanRate = preset.rate
})

// 当模块启用状态变化时，仅展开/收缩对应面板，不影响其他面板
const moduleEnableMap = [
  { key: 'domainscan', prop: () => form.domainscanEnable },
  { key: 'portscan', prop: () => form.portscanEnable },
  { key: 'portidentify', prop: () => form.portidentifyEnable },
  { key: 'fingerprint', prop: () => form.fingerprintEnable },
  { key: 'brutescan', prop: () => form.brutescanEnable },
  { key: 'dirscan', prop: () => form.dirscanEnable },
  { key: 'jsfinder', prop: () => form.jsfinderEnable },
  { key: 'pocscan', prop: () => form.pocscanEnable }
]
watch(
  moduleEnableMap.map(m => m.prop),
  (newVals, oldVals) => {
    if (!oldVals) return
    for (let i = 0; i < newVals.length; i++) {
      if (newVals[i] !== oldVals[i]) {
        const name = moduleEnableMap[i].key
        const set = new Set(activeCollapse.value)
        if (newVals[i]) {
          set.add(name)
        } else {
          set.delete(name)
        }
        activeCollapse.value = [...set]
        break
      }
    }
  }
)

async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    if (res.code === 0) organizations.value = (res.list || []).filter(org => org.status === 'enable')
  } catch (e) { console.error(e) }
}

async function loadWorkers() {
  try {
    const res = await getWorkerList()
    const data = res.data || res
    if (data.code === 0) workers.value = (data.list || []).filter(w => w.status === 'running')
  } catch (e) { console.error(e) }
}

async function loadCommonTags() {
  try {
    // 从任务列表中获取常用标签
    const res = await request.post('/task/list', { page: 1, pageSize: 100 })
    if (res.code === 0 && res.list) {
      const tagSet = new Set()
      res.list.forEach(task => {
        if (task.tags && Array.isArray(task.tags)) {
          task.tags.forEach(tag => tagSet.add(tag))
        }
      })
      commonTags.value = Array.from(tagSet)
    }
  } catch (e) { console.error('Load common tags failed:', e) }
}

async function loadTaskDetail(taskId) {
  try {
    const res = await getTaskDetail({ id: taskId })
    if (res.code === 0 && res.data) {
      Object.assign(form, res.data)
      if (res.data.config) {
        const config = JSON.parse(res.data.config)
        applyConfig(config)
      }
    }
  } catch (e) { console.error(e) }
}

function applyConfig(config) {
  // 判断POC模式：有全选标记或nucleiTemplateIds/customPocIds，则为手动模式
  const isManualMode = !!(config.pocscan?.nucleiSelectAll || config.pocscan?.customPocSelectAll ||
    (config.pocscan?.nucleiTemplateIds?.length > 0) || (config.pocscan?.customPocIds?.length > 0))

  // 判断是否启用字典爆破：如果有subdomainDictIds则启用
  const hasBruteforce = config.domainscan?.subdomainDictIds?.length > 0

  // 恢复上次的任务名称和扫描目标
  if (config.name) form.name = config.name
  if (config.target) form.target = config.target

  isLoadingConfig = true
  Object.assign(form, {
    // batchSize 由后端自动计算，不再从配置加载
    // 子域名扫描
    domainscanEnable: config.domainscan?.enable ?? false,
    domainscanSubfinder: config.domainscan?.subfinder ?? true,
    domainscanBruteforce: hasBruteforce,
    domainscanBruteforceTimeout: config.domainscan?.bruteforceTimeout || 30,
    domainscanTimeout: config.domainscan?.timeout || 300,
    domainscanMaxEnumTime: config.domainscan?.maxEnumerationTime || 10,
    domainscanThreads: config.domainscan?.threads || 10,
    domainscanRateLimit: config.domainscan?.rateLimit || 0,
    domainscanRemoveWildcard: config.domainscan?.removeWildcard ?? true,
    domainscanResolveDNS: config.domainscan?.resolveDNS ?? true,
    domainscanConcurrent: config.domainscan?.concurrent || 50,
    subdomainDictIds: config.domainscan?.subdomainDictIds || [],
    // KSubdomain增强功能
    domainscanRecursiveBrute: config.domainscan?.recursiveBrute ?? false,
    recursiveDictIds: config.domainscan?.recursiveDictIds || [],
    domainscanWildcardDetect: config.domainscan?.wildcardDetect ?? true,
    // 端口扫描
    portscanEnable: config.portscan?.enable ?? true,
    portscanTool: config.portscan?.tool || 'naabu',
    portscanRate: config.portscan?.rate || 1000,
    ports: config.portscan?.ports || 'top100',
    portThreshold: config.portscan?.portThreshold || 100,
    scanType: config.portscan?.scanType || 'c',
    portscanTargetTimeout: config.portscan?.targetTimeout ?? config.portscan?.timeout ?? 60,
    portscanProbeTimeoutMs: config.portscan?.probeTimeoutMs ?? 1000,
    skipHostDiscovery: config.portscan?.skipHostDiscovery ?? false,
    excludeCDN: config.portscan?.excludeCDN ?? false,
    excludeHosts: config.portscan?.excludeHosts || '',
    // 端口识别
    portidentifyEnable: config.portidentify?.enable ?? false,
    portidentifyTool: config.portidentify?.tool || 'nmap',
    portidentifyTimeout: config.portidentify?.timeout || 30,
    portidentifyConcurrency: config.portidentify?.concurrency || 10,
    portidentifyArgs: config.portidentify?.args || '',
    portidentifyUDP: config.portidentify?.udp ?? false,
    portidentifyFastMode: config.portidentify?.fastMode ?? false,
    // 指纹识别
    fingerprintEnable: config.fingerprint?.enable ?? true,
    fingerprintTool: config.fingerprint?.tool || (config.fingerprint?.httpx ? 'httpx' : 'builtin'),
    fingerprintIconHash: config.fingerprint?.iconHash ?? true,
    fingerprintCustomEngine: config.fingerprint?.customEngine ?? false,
    fingerprintScreenshot: config.fingerprint?.screenshot ?? true,
    fingerprintCert: config.fingerprint?.cert ?? true,
    fingerprintActiveScan: config.fingerprint?.activeScan ?? false,
    fingerprintActiveTimeout: config.fingerprint?.activeTimeout || 10,
    fingerprintTimeout: config.fingerprint?.targetTimeout || 30,
    fingerprintFilterMode: config.fingerprint?.filterMode || 'http_mapping',
    // 弱口令扫描
    brutescanEnable: config.brutescan?.enable ?? false,
    brutescanServices: config.brutescan?.services || [],
    brutescanThreads: config.brutescan?.threads || 20,
    brutescanTimeout: config.brutescan?.timeout || 5,
    brutescanDelayMs: config.brutescan?.delayMs || 100,
    brutescanStopOnFirst: config.brutescan?.stopOnFirst ?? false,
    brutescanForceScan: config.brutescan?.forceScan ?? false,
    // 漏洞扫描
    pocscanEnable: config.pocscan?.enable ?? false,
    pocscanMode: isManualMode ? 'manual' : 'auto',
    pocscanAutoScan: config.pocscan?.autoScan ?? true,
    pocscanAutomaticScan: config.pocscan?.automaticScan ?? true,
    pocscanCustomOnly: config.pocscan?.customPocOnly ?? false,
    pocscanSeverity: config.pocscan?.severity ? config.pocscan.severity.split(',') : ['critical', 'high', 'medium'],
    pocscanTargetTimeout: config.pocscan?.targetTimeout || 600,
    pocscanNucleiTemplateIds: config.pocscan?.nucleiTemplateIds || [],
    pocscanCustomPocIds: config.pocscan?.customPocIds || [],
    ...parseCustomHeaders(config.pocscan?.customHeaders),
    // 目录扫描
    dirscanEnable: config.dirscan?.enable ?? false,
    dirscanTool: config.dirscan?.tool || 'ffuf',
    dirscanDictIds: config.dirscan?.dictIds || [],
    dirscanFollowRedirect: config.dirscan?.followRedirect ?? false,
    dirscanForceScan: config.dirscan?.forceScan ?? false,
    dirscanStatusCodes: config.dirscan?.statusCodes || [],
    dirscanAutoCalibration: config.dirscan?.autoCalibration ?? true,
    dirscanRecursion: config.dirscan?.recursion ?? false,
    dirscanRecursionDepth: config.dirscan?.recursionDepth || 2,
    // JS扫描
    jsfinderEnable: config.jsfinder?.enable ?? false,
    jsfinderThreads: config.jsfinder?.threads || 10,
    jsfinderTimeout: config.jsfinder?.timeout || 10,
    jsfinderEnableSourcemap: config.jsfinder?.enableSourcemap ?? true,
    jsfinderEnableUnauthCheck: config.jsfinder?.enableUnauthCheck ?? true,
    jsfinderForceScan: config.jsfinder?.forceScan ?? false
  })
  isLoadingConfig = false

  // 恢复手动全选状态（后端已展开的 ID 列表数量仅作展示，不加载列表）
  nucleiSelectAll.value = !!config.pocscan?.nucleiSelectAll
  nucleiSelectAllCount.value = nucleiSelectAll.value ? (config.pocscan?.nucleiTemplateIds?.length || 0) : 0
  Object.assign(nucleiSelectAllFilter, config.pocscan?.nucleiSelectAllFilter || {})
  customPocSelectAll.value = !!config.pocscan?.customPocSelectAll
  customPocSelectAllCount.value = customPocSelectAll.value ? (config.pocscan?.customPocIds?.length || 0) : 0
  Object.assign(customPocSelectAllFilter, config.pocscan?.customPocSelectAllFilter || {})

  // 根据启用的模块动态设置折叠面板展开状态
  const enabled = []
  if (config.domainscan?.enable) enabled.push('domainscan')
  if (config.portscan?.enable ?? true) enabled.push('portscan')
  if (config.portidentify?.enable) enabled.push('portidentify')
  if (config.fingerprint?.enable ?? true) enabled.push('fingerprint')
  if (config.brutescan?.enable) enabled.push('brutescan')
  if (config.dirscan?.enable) enabled.push('dirscan')
  if (config.jsfinder?.enable) enabled.push('jsfinder')
  if (config.pocscan?.enable) enabled.push('pocscan')
  activeCollapse.value = enabled

  // 恢复选择的模板
  if (config.template !== undefined) {
    selectedTemplate.value = config.template
  }
}

// 处理模板配置加载
function handleTemplateConfigLoaded(config) {
  applyConfig(config)
}

// 防抖保存配置
let saveConfigTimer = null
function debounceSaveConfig() {
  if (saveConfigTimer) clearTimeout(saveConfigTimer)
  saveConfigTimer = setTimeout(() => {
    const config = buildConfig()
    saveScanConfig({ config: JSON.stringify(config) }).catch(e => console.error('Auto save config failed:', e))
  }, 500)
}

// 监听扫描配置变化，自动保存（仅在新建任务时）
// 使用 getter 函数返回配置字段的快照
watch(
  () => JSON.stringify({
    name: form.name,
    target: form.target,
    domainscanEnable: form.domainscanEnable,
    domainscanSubfinder: form.domainscanSubfinder,
    domainscanBruteforce: form.domainscanBruteforce,
    domainscanBruteforceTimeout: form.domainscanBruteforceTimeout,
    domainscanTimeout: form.domainscanTimeout,
    domainscanMaxEnumTime: form.domainscanMaxEnumTime,
    domainscanThreads: form.domainscanThreads,
    domainscanRateLimit: form.domainscanRateLimit,
    domainscanRemoveWildcard: form.domainscanRemoveWildcard,
    domainscanResolveDNS: form.domainscanResolveDNS,
    domainscanConcurrent: form.domainscanConcurrent,
    subdomainDictIds: form.subdomainDictIds,
    // KSubdomain增强功能
    domainscanRecursiveBrute: form.domainscanRecursiveBrute,
    recursiveDictIds: form.recursiveDictIds,
    domainscanWildcardDetect: form.domainscanWildcardDetect,
    portscanEnable: form.portscanEnable,
    portscanTool: form.portscanTool,
    portscanRate: form.portscanRate,
    ports: form.ports,
    portThreshold: form.portThreshold,
    scanType: form.scanType,
    portscanTargetTimeout: form.portscanTargetTimeout,
    portscanProbeTimeoutMs: form.portscanProbeTimeoutMs,
    skipHostDiscovery: form.skipHostDiscovery,
    excludeCDN: form.excludeCDN,
    excludeHosts: form.excludeHosts,
    workers: form.portscanWorkers,
    retries: form.portscanRetries,
    warmUpTime: form.portscanWarmUpTime,
    verify: form.portscanVerify,
    portidentifyEnable: form.portidentifyEnable,
    portidentifyTool: form.portidentifyTool,
    portidentifyTimeout: form.portidentifyTimeout,
    portidentifyConcurrency: form.portidentifyConcurrency,
    portidentifyArgs: form.portidentifyArgs,
    portidentifyUDP: form.portidentifyUDP,
    portidentifyFastMode: form.portidentifyFastMode,
    fingerprintEnable: form.fingerprintEnable,
    fingerprintTool: form.fingerprintTool,
    fingerprintIconHash: form.fingerprintIconHash,
    fingerprintCustomEngine: form.fingerprintCustomEngine,
    fingerprintScreenshot: form.fingerprintScreenshot,
    fingerprintCert: form.fingerprintCert,
    fingerprintActiveScan: form.fingerprintActiveScan,
    fingerprintActiveTimeout: form.fingerprintActiveTimeout,
    fingerprintTimeout: form.fingerprintTimeout,
    brutescanEnable: form.brutescanEnable,
    brutescanServices: form.brutescanServices,
    brutescanThreads: form.brutescanThreads,
    brutescanTimeout: form.brutescanTimeout,
    brutescanDelayMs: form.brutescanDelayMs,
    brutescanStopOnFirst: form.brutescanStopOnFirst,
    brutescanForceScan: form.brutescanForceScan,
    pocscanEnable: form.pocscanEnable,
    pocscanMode: form.pocscanMode,
    pocscanAutoScan: form.pocscanAutoScan,
    pocscanAutomaticScan: form.pocscanAutomaticScan,
    pocscanCustomOnly: form.pocscanCustomOnly,
    pocscanSeverity: form.pocscanSeverity,
    pocscanTargetTimeout: form.pocscanTargetTimeout,
    pocscanNucleiTemplateIds: form.pocscanNucleiTemplateIds,
    pocscanCustomPocIds: form.pocscanCustomPocIds,
    // 目录扫描
    dirscanEnable: form.dirscanEnable,
    dirscanDictIds: form.dirscanDictIds,
    dirscanFollowRedirect: form.dirscanFollowRedirect,
    dirscanForceScan: form.dirscanForceScan,
    dirscanStatusCodes: form.dirscanStatusCodes,
    dirscanAutoCalibration: form.dirscanAutoCalibration,
    dirscanRecursion: form.dirscanRecursion,
    dirscanRecursionDepth: form.dirscanRecursionDepth
  }),
  () => {
    if (!isEdit.value) {
      debounceSaveConfig()
    }
  }
)

function parseCustomHeaders(headers) {
  if (!headers || headers.length === 0) {
    return { pocscanHeaderMode: 'none', pocscanPresetUA: '', pocscanCustomHeadersText: '' }
  }
  // 检查是否只有一个 User-Agent 头（可能是预设UA）
  if (headers.length === 1 && headers[0].toLowerCase().startsWith('user-agent:')) {
    const ua = headers[0].substring(headers[0].indexOf(':') + 1).trim()
    return { pocscanHeaderMode: 'preset', pocscanPresetUA: ua, pocscanCustomHeadersText: '' }
  }
  return { pocscanHeaderMode: 'custom', pocscanPresetUA: '', pocscanCustomHeadersText: headers.join('\n') }
}

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

function buildConfig() {
  const config = {
    name: form.name,
    target: form.target,
    template: selectedTemplate.value,
    // batchSize 由后端根据目标数量和Worker并发数自动计算
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
      // 只有启用字典爆破时才传递字典ID和增强功能配置
      subdomainDictIds: form.domainscanBruteforce ? (form.subdomainDictIds || []) : [],
      bruteforceTimeout: form.domainscanBruteforce ? (form.domainscanBruteforceTimeout || 30) : 30,
      // KSubdomain增强功能（只有启用字典爆破时才生效）
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
      customOnly: form.pocscanCustomOnly,
      severity: form.pocscanSeverity.join(','),
      targetTimeout: form.pocscanTargetTimeout,
      nucleiTemplateIds: form.pocscanNucleiTemplateIds || [],
      customPocIds: form.pocscanCustomPocIds || [],
      customHeaders: buildCustomHeaders()
    },
    dirscan: {
      enable: form.dirscanEnable,
      tool: form.dirscanTool,
      dictIds: form.dirscanDictIds,
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
      enableUnauthCheck: form.jsfinderEnableUnauthCheck ? undefined : false,
      forceScan: form.jsfinderForceScan && !hasPrePhaseEnabled.value
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
      config.pocscan.nucleiTemplateIds = form.pocscanNucleiTemplateIds
    }
    if (!customPocSelectAll.value) {
      config.pocscan.customPocIds = form.pocscanCustomPocIds
    }
    config.pocscan.autoScan = false
    config.pocscan.automaticScan = false
    config.pocscan.customPocOnly = false
    // 手动模式下不叠加严重级别过滤：所选即所扫（severity 选择器在该模式下不展示，
    // 若继续携带默认值会在扫描时静默丢弃低危模板）
    config.pocscan.severity = ''
  } else {
    // 自动匹配模式
    if (form.pocscanCustomOnly) {
      // 只使用自定义POC时，禁用自动扫描
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

// 目标实时计数：总数 / 去重后 / 非法
const targetStats = computed(() => {
  const raw = (form.target || '').trim()
  if (!raw) return { total: 0, unique: 0, dedup: 0, invalid: 0 }
  const lines = raw.split(/[\n\r]+/).map(s => s.trim()).filter(s => s && !s.startsWith('#'))
  const items = []
  for (const line of lines) {
    for (const w of line.split(/[;,]+/)) {
      const t = w.trim()
      if (t) items.push(t)
    }
  }
  const total = items.length
  const unique = new Set(items.map(s => s.toLowerCase())).size
  const dedup = total - unique
  const invalid = items.filter(s => validateSingleTarget(s) !== null).length
  return { total, unique, dedup, invalid }
})

async function handleSubmit() {
  try {
    await formRef.value.validate()
  } catch (e) { return }

  // 校验：至少启用一项扫描配置
  const anyScanEnabled = form.domainscanEnable || form.portscanEnable ||
    form.portidentifyEnable || form.fingerprintEnable ||
    form.brutescanEnable || form.dirscanEnable || form.jsfinderEnable || form.pocscanEnable
  if (!anyScanEnabled) {
    ElMessage.error(t('task.noScanConfigEnabled'))
    return
  }

  submitting.value = true
  try {
    // 清除防抖定时器，确保最新配置被保存
    if (saveConfigTimer) {
      clearTimeout(saveConfigTimer)
      saveConfigTimer = null
    }

    // 在新建任务时，先保存配置到用户配置
    if (!isEdit.value) {
      try {
        const config = buildConfig()
        await saveScanConfig({ config: JSON.stringify(config) })
      } catch (e) {
        ElMessage.warning(t('task.defaultConfigSaveFailed'))
        // 不阻断任务创建流程
      }
    }

    const config = buildConfig()
    const params = {
      name: form.name,
      target: form.target,
      orgId: form.orgId,
      workers: form.workers,
      tags: form.tags,
      config: JSON.stringify(config)
    }

    let res
    if (isEdit.value) {
      params.id = form.id
      res = await updateTask(params)
    } else {
      res = await createTask(params)
    }

    if (res.code === 0) {
      ElMessage.success(isEdit.value ? t('task.taskUpdateSuccess') : t('task.taskCreateSuccess'))
      if (!isEdit.value && res.id) {
        // 任务已创建为草稿，启动扫描前显式确认，避免误触立即执行昂贵扫描
        let startConfirmed = false
        try {
          await ElMessageBox.confirm(
            t('task.confirmStartAfterCreate'),
            t('task.confirmStartTitle'),
            { type: 'warning', confirmButtonText: t('task.startNow'), cancelButtonText: t('task.saveAsDraft') }
          )
          startConfirmed = true
        } catch (e) {
          startConfirmed = false
        }
        if (startConfirmed) {
          try {
            const startRes = await startTask({ id: res.id })
            if (startRes && startRes.code === 0) {
              ElMessage.success(t('task.taskStarted'))
            } else {
              // 启动失败不回滚已创建任务，提示用户手动启动
              ElMessage.warning(t('task.taskCreatedButStartFailed'))
            }
          } catch (e) {
            ElMessage.warning(t('task.taskCreatedButStartFailed'))
          }
        }
      }
      // 跳转回任务列表并带上新建任务 id，触发列表延迟刷新以更新任务状态
      router.push({ path: '/task', query: isEdit.value ? {} : { created: res.id || '1' } })
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } finally {
    submitting.value = false
  }
}

function handleCancel() {
  router.push('/task')
}

// POC选择相关方法
const nucleiTableRef = ref()
const customPocTableRef = ref()

function getSeverityType(severity) {
  const map = { critical: 'danger', high: 'warning', medium: '', low: 'info', info: 'success', unknown: 'info' }
  return map[severity] || 'info'
}

function handlePocModeChange(mode) {
  if (mode === 'manual' && !form.pocscanNucleiTemplateIds.length && !form.pocscanCustomPocIds.length && !nucleiSelectAll.value && !customPocSelectAll.value) {
    // 切换到手动模式时，初始化选择
    selectedNucleiTemplateIds.value = []
    selectedCustomPocIds.value = []
  }
}

function showPocSelectDialog() {
  // 恢复之前的选择（ID和对象信息）
  selectedNucleiTemplateIds.value = [...form.pocscanNucleiTemplateIds]
  selectedCustomPocIds.value = [...form.pocscanCustomPocIds]
  selectedNucleiTemplates.value = [...(form.pocscanNucleiTemplates || [])]
  selectedCustomPocs.value = [...(form.pocscanCustomPocs || [])]
  // 清空搜索关键词
  selectedPocSearchKeyword.value = ''
  pocSelectDialogVisible.value = true
}

async function handlePocDialogOpen() {
  // 加载当前页数据与筛选维度统计
  await Promise.all([loadNucleiTemplatesForSelect(), loadCustomPocsForSelect(), loadNucleiTemplateFacets(), loadCustomPocFacets()])
  // 等待DOM更新后恢复选中状态
  await nextTick()
  restoreTableSelections()
}

function restoreTableSelections() {
  // 全选状态下表格选择被禁用，无需恢复
  if (nucleiSelectAll.value || customPocSelectAll.value) return
  // 恢复Nuclei模板选中状态
  if (nucleiTableRef.value && nucleiTemplateList.value.length > 0) {
    const selectedIds = new Set(selectedNucleiTemplateIds.value)
    nucleiTemplateList.value.forEach(row => {
      if (selectedIds.has(row.id)) {
        nucleiTableRef.value.toggleRowSelection(row, true)
      }
    })
  }
  // 恢复自定义POC选中状态
  if (customPocTableRef.value && customPocList.value.length > 0) {
    const selectedIds = new Set(selectedCustomPocIds.value)
    customPocList.value.forEach(row => {
      if (selectedIds.has(row.id)) {
        customPocTableRef.value.toggleRowSelection(row, true)
      }
    })
  }
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
      page: nucleiTemplatePagination.page,
      pageSize: nucleiTemplatePagination.pageSize,
      ...buildNucleiTemplateFilterPayload()
    })
    if (res.code === 0) {
      nucleiTemplateList.value = res.list || []
      nucleiTemplatePagination.total = res.total || 0
      // 等待DOM更新后恢复当前页的选中状态
      await nextTick()
      restoreNucleiTableSelection()
    }
  } catch (e) {
    console.error('Load Nuclei templates failed:', e)
  } finally {
    nucleiTemplateLoading.value = false
    // 延迟重置标志位，确保selection-change事件处理完成
    setTimeout(() => { isLoadingData.value = false }, 100)
  }
}

// 恢复Nuclei表格选中状态
function restoreNucleiTableSelection() {
  if (!nucleiTableRef.value) return
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
      page: customPocPagination.page,
      pageSize: customPocPagination.pageSize,
      ...buildCustomPocFilterPayload(),
      enabled: true // 只显示启用的POC
    })
    if (res.code === 0) {
      customPocList.value = res.list || []
      customPocPagination.total = res.total || 0
      // 等待DOM更新后恢复当前页的选中状态
      await nextTick()
      restoreCustomPocTableSelection()
    }
  } catch (e) {
    console.error('Load custom POC failed:', e)
  } finally {
    customPocLoading.value = false
    // 延迟重置标志位，确保selection-change事件处理完成
    setTimeout(() => { isLoadingData.value = false }, 100)
  }
}

// 恢复自定义POC表格选中状态
function restoreCustomPocTableSelection() {
  if (!customPocTableRef.value) return
  const selectedIds = new Set(selectedCustomPocIds.value)
  customPocList.value.forEach(row => {
    if (selectedIds.has(row.id)) {
      customPocTableRef.value.toggleRowSelection(row, true)
    }
  })
}

function handleNucleiSelectionChange(selection) {
  // 如果正在执行"选择全部"或加载数据操作，跳过处理
  if (isSelectingAll.value || isLoadingData.value) return
  // 全选状态下表格勾选不生效（选择意图已由全选标记表达）
  if (nucleiSelectAll.value) {
    if (nucleiTableRef.value) nucleiTableRef.value.clearSelection()
    return
  }

  // 获取当前页的所有ID
  const currentPageIds = new Set(nucleiTemplateList.value.map(t => t.id))
  // 获取当前页选中的ID和对象
  const currentPageSelectedIds = new Set(selection.map(t => t.id))
  const currentPageSelectedItems = selection.filter(t => currentPageIds.has(t.id))

  // 保留其他页的选择ID
  const newSelectedIds = selectedNucleiTemplateIds.value.filter(id => !currentPageIds.has(id))
  currentPageSelectedIds.forEach(id => newSelectedIds.push(id))
  selectedNucleiTemplateIds.value = newSelectedIds

  // 保留其他页的选择对象，添加当前页选中的对象
  const otherPageItems = selectedNucleiTemplates.value.filter(t => !currentPageIds.has(t.id))
  selectedNucleiTemplates.value = [...otherPageItems, ...currentPageSelectedItems]
}

function handleCustomPocSelectionChange(selection) {
  // 如果正在执行"选择全部"或加载数据操作，跳过处理
  if (isSelectingAll.value || isLoadingData.value) return
  // 全选状态下表格勾选不生效（选择意图已由全选标记表达）
  if (customPocSelectAll.value) {
    if (customPocTableRef.value) customPocTableRef.value.clearSelection()
    return
  }

  // 获取当前页的所有ID
  const currentPageIds = new Set(customPocList.value.map(p => p.id))
  // 获取当前页选中的ID和对象
  const currentPageSelectedIds = new Set(selection.map(p => p.id))
  const currentPageSelectedItems = selection.filter(p => currentPageIds.has(p.id))

  // 保留其他页的选择ID
  const newSelectedIds = selectedCustomPocIds.value.filter(id => !currentPageIds.has(id))
  currentPageSelectedIds.forEach(id => newSelectedIds.push(id))
  selectedCustomPocIds.value = newSelectedIds

  // 保留其他页的选择对象，添加当前页选中的对象
  const otherPageItems = selectedCustomPocs.value.filter(p => !currentPageIds.has(p.id))
  selectedCustomPocs.value = [...otherPageItems, ...currentPageSelectedItems]
}

function confirmPocSelection() {
  form.pocscanNucleiTemplateIds = [...selectedNucleiTemplateIds.value]
  form.pocscanCustomPocIds = [...selectedCustomPocIds.value]
  // 保存对象信息用于下次打开时显示
  form.pocscanNucleiTemplates = [...selectedNucleiTemplates.value]
  form.pocscanCustomPocs = [...selectedCustomPocs.value]
  pocSelectDialogVisible.value = false
}

// 清除所有选择
function clearAllSelections() {
  selectedNucleiTemplateIds.value = []
  selectedNucleiTemplates.value = []
  selectedCustomPocIds.value = []
  selectedCustomPocs.value = []
  resetNucleiSelectAll()
  resetCustomPocSelectAll()
  // 清空表格选择状态
  if (nucleiTableRef.value) {
    nucleiTableRef.value.clearSelection()
  }
  if (customPocTableRef.value) {
    customPocTableRef.value.clearSelection()
  }
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

// 选择全部Nuclei模板（根据当前筛选条件）：只记录全选标记与条件，由后端查询展开
async function selectAllNucleiTemplates() {
  selectAllNucleiLoading.value = true
  isSelectingAll.value = true
  try {
    // 记录全选条件（当前对话框筛选，与列表接口参数一致，由后端按相同条件展开）
    Object.assign(nucleiSelectAllFilter, buildNucleiTemplateFilterPayload())

    // 只查询数量（pageSize=1），不加载列表
    const res = await getNucleiTemplateList({ page: 1, pageSize: 1, ...buildNucleiTemplateFilterPayload() })
    if (res.code !== 0) {
      throw new Error(res.msg || 'Failed to fetch data')
    }

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
    if (nucleiTableRef.value) {
      nucleiTableRef.value.clearSelection()
    }
    ElMessage.success(t('task.allSelectedCount', { count: total }))
  } catch (e) {
    console.error('Select all failed:', e)
    resetNucleiSelectAll()
    ElMessage.error(t('task.selectAllFailed'))
  } finally {
    selectAllNucleiLoading.value = false
    isSelectingAll.value = false
  }
}

// 选择全部自定义POC（根据当前筛选条件）：只记录全选标记与条件，由后端查询展开
async function selectAllCustomPocs() {
  selectAllCustomLoading.value = true
  isSelectingAll.value = true
  try {
    // 记录全选条件（当前对话框筛选，与列表接口参数一致，由后端按相同条件展开）
    Object.assign(customPocSelectAllFilter, buildCustomPocFilterPayload())

    // 只查询数量（pageSize=1），不加载列表
    const res = await getCustomPocList({ page: 1, pageSize: 1, ...buildCustomPocFilterPayload(), enabled: true })
    if (res.code !== 0) {
      throw new Error(res.msg || 'Failed to fetch data')
    }

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
    if (customPocTableRef.value) {
      customPocTableRef.value.clearSelection()
    }
    ElMessage.success(t('task.allSelectedCount', { count: total }))
  } catch (e) {
    console.error('Select all failed:', e)
    resetCustomPocSelectAll()
    ElMessage.error(t('task.selectAllFailed'))
  } finally {
    selectAllCustomLoading.value = false
    isSelectingAll.value = false
  }
}

// 清除Nuclei模板选择
function clearNucleiSelections() {
  selectedNucleiTemplateIds.value = []
  selectedNucleiTemplates.value = []
  resetNucleiSelectAll()
  if (nucleiTableRef.value) {
    nucleiTableRef.value.clearSelection()
  }
}

// 取消选择全部Nuclei模板（按钮调用）
function deselectAllNucleiTemplates() {
  clearNucleiSelections()
  ElMessage.success(t('task.allTemplatesDeselected'))
}

// 清除自定义POC选择
function clearCustomPocSelections() {
  selectedCustomPocIds.value = []
  selectedCustomPocs.value = []
  resetCustomPocSelectAll()
  if (customPocTableRef.value) {
    customPocTableRef.value.clearSelection()
  }
}

// 取消选择全部自定义POC（按钮调用）
function deselectAllCustomPocs() {
  clearCustomPocSelections()
  ElMessage.success(t('task.allPocsDeselected'))
}

// 移除单个Nuclei模板
function removeNucleiTemplate(id) {
  selectedNucleiTemplateIds.value = selectedNucleiTemplateIds.value.filter(i => i !== id)
  selectedNucleiTemplates.value = selectedNucleiTemplates.value.filter(t => t.id !== id)
  // 更新表格选择状态
  if (nucleiTableRef.value) {
    const row = nucleiTemplateList.value.find(t => t.id === id)
    if (row) {
      nucleiTableRef.value.toggleRowSelection(row, false)
    }
  }
}

// 移除单个自定义POC
function removeCustomPoc(id) {
  selectedCustomPocIds.value = selectedCustomPocIds.value.filter(i => i !== id)
  selectedCustomPocs.value = selectedCustomPocs.value.filter(p => p.id !== id)
  // 更新表格选择状态
  if (customPocTableRef.value) {
    const row = customPocList.value.find(p => p.id === id)
    if (row) {
      customPocTableRef.value.toggleRowSelection(row, false)
    }
  }
}

// 查看POC内容
async function viewPocContent(row, type) {
  currentViewPoc.value = { ...row }
  pocContentTitle.value = type === 'nuclei' ? t('task.defaultTemplateContent') : t('task.customPocContent')
  pocContentDialogVisible.value = true

  // 如果没有content字段，需要从后端获取
  if (!row.content) {
    pocContentLoading.value = true
    try {
      if (type === 'nuclei') {
        // 后端API需要templateId参数（模板字符串ID，如CVE-2021-xxxx）
        const res = await getNucleiTemplateDetail({ templateId: row.id })
        if (res.code === 0 && res.data) {
          currentViewPoc.value = { ...currentViewPoc.value, ...res.data }
        } else {
          currentViewPoc.value.content = res.msg || t('task.getContentFailed')
        }
      } else {
        // 自定义POC通常在列表中已包含content
        currentViewPoc.value.content = row.content || t('task.noContent')
      }
    } catch (e) {
      console.error('Get POC content failed:', e)
      currentViewPoc.value.content = t('task.getContentFailed')
    } finally {
      pocContentLoading.value = false
    }
  }
}

// 复制POC内容
function copyPocContent() {
  if (currentViewPoc.value.content) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(currentViewPoc.value.content).then(() => {
        ElMessage.success(t('task.copiedToClipboard'))
      }).catch(() => {
        fallbackCopyToClipboard(currentViewPoc.value.content)
      })
    } else {
      fallbackCopyToClipboard(currentViewPoc.value.content)
    }
  }
}

function fallbackCopyToClipboard(text) {
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.left = '-999999px'
    textarea.style.top = '-999999px'
    document.body.appendChild(textarea)
    textarea.focus()
    textarea.select()
    const successful = document.execCommand('copy')
    document.body.removeChild(textarea)

    if (successful) {
      ElMessage.success(t('task.copiedToClipboard'))
    } else {
      ElMessage.error(t('task.copyFailed'))
    }
  } catch (err) {
    console.error('Copy failed:', err)
    ElMessage.error(t('task.copyFailed'))
  }
}

// ==================== 目录扫描字典选择相关方法 ====================

// 显示字典选择对话框
function showDictSelectDialog() {
  selectedDictIds.value = [...form.dirscanDictIds]
  dictSelectDialogVisible.value = true
}

// 字典对话框打开时加载数据
async function handleDictDialogOpen() {
  await loadDictList()
  // 恢复选中状态
  await nextTick()
  restoreDictTableSelection()
}

// 加载字典列表
async function loadDictList() {
  dictLoading.value = true
  try {
    const res = await getDirScanDictEnabledList()
    if (res.code === 0) {
      dictList.value = res.list || []
    }
  } catch (e) {
    console.error('Load dictionary list failed:', e)
  } finally {
    dictLoading.value = false
  }
}

// 恢复字典表格选中状态
function restoreDictTableSelection() {
  if (!dictTableRef.value) return
  const selectedIds = new Set(selectedDictIds.value)
  dictList.value.forEach(row => {
    if (selectedIds.has(row.id)) {
      dictTableRef.value.toggleRowSelection(row, true)
    }
  })
}

// 字典选择变化
function handleDictSelectionChange(selection) {
  selectedDictIds.value = selection.map(d => d.id)
}

// 确认字典选择
function confirmDictSelection() {
  form.dirscanDictIds = [...selectedDictIds.value]
  form.dirscanDicts = dictList.value.filter(d => selectedDictIds.value.includes(d.id))
  dictSelectDialogVisible.value = false
}

// ==================== 子域名字典选择相关方法 ====================

// 显示子域名字典选择对话框
function showSubdomainDictSelectDialog() {
  selectedSubdomainDictIds.value = [...(form.subdomainDictIds || [])]
  subdomainDictSelectDialogVisible.value = true
}

// 子域名字典对话框打开时加载数据
async function handleSubdomainDictDialogOpen() {
  await loadSubdomainDictList()
  // 恢复选中状态
  await nextTick()
  restoreSubdomainDictTableSelection()
}

// 加载子域名字典列表
async function loadSubdomainDictList() {
  subdomainDictLoading.value = true
  try {
    const res = await getSubdomainDictEnabledList()
    if (res.code === 0) {
      subdomainDictList.value = res.list || []
    }
  } catch (e) {
    console.error('Load subdomain dictionary list failed:', e)
  } finally {
    subdomainDictLoading.value = false
  }
}

// 恢复子域名字典表格选中状态
function restoreSubdomainDictTableSelection() {
  if (!subdomainDictTableRef.value) return
  const selectedIds = new Set(selectedSubdomainDictIds.value)
  subdomainDictList.value.forEach(row => {
    if (selectedIds.has(row.id)) {
      subdomainDictTableRef.value.toggleRowSelection(row, true)
    }
  })
}

// 子域名字典选择变化
function handleSubdomainDictSelectionChange(selection) {
  selectedSubdomainDictIds.value = selection.map(d => d.id)
}

// 确认子域名字典选择
function confirmSubdomainDictSelection() {
  form.subdomainDictIds = [...selectedSubdomainDictIds.value]
  form.subdomainDicts = subdomainDictList.value.filter(d => selectedSubdomainDictIds.value.includes(d.id))
  subdomainDictSelectDialogVisible.value = false
}

// ==================== 递归爆破字典选择相关方法 ====================

// 显示递归字典选择对话框
function showRecursiveDictSelectDialog() {
  selectedRecursiveDictIds.value = [...(form.recursiveDictIds || [])]
  recursiveDictSelectDialogVisible.value = true
}

// 递归字典对话框打开时加载数据
async function handleRecursiveDictDialogOpen() {
  await loadRecursiveDictList()
  // 恢复选中状态
  await nextTick()
  restoreRecursiveDictTableSelection()
}

// 加载递归字典列表（复用子域名字典列表）
async function loadRecursiveDictList() {
  recursiveDictLoading.value = true
  try {
    const res = await getSubdomainDictEnabledList()
    if (res.code === 0) {
      recursiveDictList.value = res.list || []
    }
  } catch (e) {
    console.error('Load recursive dictionary list failed:', e)
  } finally {
    recursiveDictLoading.value = false
  }
}

// 恢复递归字典表格选中状态
function restoreRecursiveDictTableSelection() {
  if (!recursiveDictTableRef.value) return
  const selectedIds = new Set(selectedRecursiveDictIds.value)
  recursiveDictList.value.forEach(row => {
    if (selectedIds.has(row.id)) {
      recursiveDictTableRef.value.toggleRowSelection(row, true)
    }
  })
}

// 递归字典选择变化
function handleRecursiveDictSelectionChange(selection) {
  selectedRecursiveDictIds.value = selection.map(d => d.id)
}

// 确认递归字典选择
function confirmRecursiveDictSelection() {
  form.recursiveDictIds = [...selectedRecursiveDictIds.value]
  form.recursiveDicts = recursiveDictList.value.filter(d => selectedRecursiveDictIds.value.includes(d.id))
  recursiveDictSelectDialogVisible.value = false
}

</script>

<style lang="scss" scoped>
.task-create-page {
  .create-card {
    .task-form {
      padding: 20px 40px;
    }
  }

  .config-collapse {
    margin: 20px 0;

    :deep(.el-collapse-item__header) {
      background: var(--el-fill-color-light);
      padding: 0 16px;
      font-size: 14px;
      font-weight: 500;
      height: 44px;
      line-height: 44px;

      &:hover {
        background: var(--el-fill-color);
      }
    }

    :deep(.el-collapse-item__wrap) {
      border: none;
    }

    :deep(.el-collapse-item__content) {
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
  }

  .form-hint {
    margin-left: 10px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }

  .secondary-hint {
    color: var(--el-text-color-secondary);
  }

  .warning-hint {
    color: var(--el-color-warning);
    font-size: 12px;
  }

  .form-actions {
    margin-top: 30px;
    padding-top: 20px;
    border-top: 1px solid var(--el-border-color-lighter);

    .el-button {
      min-width: 100px;
    }
  }

  .selected-poc-summary {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  // 扫描工具左右分栏布局
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
    padding: 12px;
    background: var(--el-fill-color);
    border-radius: 4px;
    color: var(--el-text-color-secondary);
    font-size: 13px;
    margin-top: 10px;
  }

  .filter-group {
    margin-top: 10px;
    padding: 16px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 6px;
    background: var(--el-fill-color-lighter);
  }

  .filter-group-title {
    font-size: 13px;
    font-weight: 500;
    color: var(--el-text-color-secondary);
    margin-bottom: 12px;
  }
}

.poc-filter-form {
  margin-bottom: 10px;
}

.poc-pagination {
  margin-top: 15px;
}

.poc-select-container {
  display: flex;
  gap: 20px;
  min-height: 500px;
}

.poc-select-left {
  flex: 1;
  min-width: 0;
}

.poc-select-right {
  width: 280px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
  display: flex;
  flex-direction: column;
}

.selected-header {
  padding: 12px 15px;
  border-bottom: 1px solid var(--el-border-color-light);
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 500;
  background: var(--el-fill-color-light);
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

  &:last-child {
    margin-bottom: 0;
  }
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
  border-bottom: 1px dashed var(--el-border-color-lighter);
  margin-bottom: 8px;
}

.selected-items {
  max-height: 180px;
  overflow-y: auto;
}

.selected-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 8px;
  margin-bottom: 4px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 12px;

  &:hover {
    background: var(--el-fill-color);
  }

  &:last-child {
    margin-bottom: 0;
  }
}

.item-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-right: 8px;
}

.item-remove {
  cursor: pointer;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;

  &:hover {
    color: var(--el-color-danger);
  }
}

.selected-empty {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100px;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

.poc-content-wrapper {
  :deep(.el-textarea__inner) {
    font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', monospace;
    font-size: 13px;
    line-height: 1.5;
    background: var(--el-fill-color-light);
    resize: none;
  }
}
</style>
