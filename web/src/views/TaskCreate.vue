<template>
  <div class="task-create-page">
    <el-card class="create-card">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px" class="task-form">
        <!-- 基本信息 -->
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
                <el-option v-for="ws in workspaces" :key="ws.id" :label="ws.name" :value="ws.id" />
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
        <!-- 可折叠配置区域 -->
        <el-collapse v-model="activeCollapse" class="config-collapse">
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
                <el-checkbox v-model="form.domainscanBruteforce" :disabled="!form.subdomainDictIds || !form.subdomainDictIds.length">Dnsx ({{ $t('task.dictBrute') }})</el-checkbox>
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
                        <el-checkbox v-model="form.domainscanRemoveWildcard">{{ $t('task.removeWildcardDomain') }}</el-checkbox>
                      </el-form-item>
                      <el-form-item :label="$t('task.dnsResolve')">
                        <el-checkbox v-model="form.domainscanResolveDNS">{{ $t('task.resolveSubdomainDns') }}</el-checkbox>
                        <span class="form-hint">{{ $t('task.concurrentByWorker') }}</span>
                      </el-form-item>
                    </template>
                    <div v-else class="scan-tool-disabled-hint">
                      <el-icon><InfoFilled /></el-icon>
                      <span>{{ $t('task.enableSubfinderFirst') }}</span>
                    </div>
                  </div>
                </el-col>
                
                <!-- 右侧：Dnsx 配置 -->
                <el-col :span="12">
                  <div class="scan-tool-section">
                    <div class="scan-tool-header">
                      <span class="scan-tool-title">{{ $t('task.dnsxDictBrute') }}</span>
                      <el-tag :type="form.domainscanBruteforce ? 'success' : 'info'" size="small">
                        {{ form.domainscanBruteforce ? $t('task.started') : $t('task.notStarted') }}
                      </el-tag>
                    </div>
                    <!-- 字典选择（始终显示，作为启用字典爆破的前提） -->
                    <el-form-item :label="$t('task.bruteforceDict')">
                      <div class="selected-dict-summary">
                        <el-tag type="primary" size="small" v-if="form.subdomainDictIds && form.subdomainDictIds.length">
                          {{ $t('task.selectedCount', { count: form.subdomainDictIds.length }) }}
                        </el-tag>
                        <span v-else class="warning-hint">
                          {{ $t('task.selectDictFirst') }}
                        </span>
                        <el-button type="primary" link @click="showSubdomainDictSelectDialog">{{ $t('task.selectDict') }}</el-button>
                      </div>
                      <span class="form-hint">{{ $t('task.dnsxBruteHint') }}</span>
                    </el-form-item>
                    <template v-if="form.domainscanBruteforce">
                      <el-form-item :label="$t('task.enhancedFeatures')">
                        <div style="display: flex; flex-direction: column; gap: 8px;">
                          <div style="display: flex; align-items: center; gap: 8px;">
                            <el-checkbox 
                              v-model="form.domainscanRecursiveBrute" 
                              :disabled="!form.recursiveDictIds || !form.recursiveDictIds.length"
                            >{{ $t('task.recursiveBrute') }}</el-checkbox>
                            <el-button type="primary" link size="small" @click="showRecursiveDictSelectDialog">{{ $t('task.selectRecursiveDict') }}</el-button>
                            <el-tag type="primary" size="small" v-if="form.recursiveDictIds && form.recursiveDictIds.length">
                              {{ $t('task.selectedCount', { count: form.recursiveDictIds.length }) }}
                            </el-tag>
                          </div>
                          <span class="form-hint" style="margin-left: 24px; margin-top: -4px;">
                            {{ (!form.recursiveDictIds || !form.recursiveDictIds.length) ? $t('task.selectRecursiveDictFirst') : $t('task.recursiveBruteHint') }}
                          </span>
                          <el-checkbox v-model="form.domainscanWildcardDetect">{{ $t('task.wildcardDetect') }}</el-checkbox>
                          <span class="form-hint" style="margin-left: 24px; margin-top: -4px;">{{ $t('task.wildcardDetectHint') }}</span>
                          <el-checkbox v-model="form.domainscanSubdomainCrawl">{{ $t('task.subdomainCrawl') }}</el-checkbox>
                          <span class="form-hint" style="margin-left: 24px; margin-top: -4px;">{{ $t('task.subdomainCrawlHint') }}</span>
                          <el-checkbox v-model="form.domainscanTakeoverCheck">{{ $t('task.takeoverCheck') }}</el-checkbox>
                          <span class="form-hint" style="margin-left: 24px; margin-top: -4px;">{{ $t('task.takeoverCheckHint') }}</span>
                        </div>
                      </el-form-item>
                    </template>
                    <div v-if="!form.domainscanBruteforce && form.subdomainDictIds && form.subdomainDictIds.length" class="scan-tool-disabled-hint">
                      <el-icon><InfoFilled /></el-icon>
                      <span>{{ $t('task.canEnableDnsx') }}</span>
                    </div>
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
                  <el-form-item :label="$t('task.timeoutSeconds')">
                    <el-input-number v-model="form.portscanTimeout" :min="5" :max="1200" style="width:100%" />
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
              <span class="collapse-title">{{ $t('task.portIdentify') }} <el-tag v-if="form.portidentifyEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.portidentifyEnable" />
            </el-form-item>
            <template v-if="form.portidentifyEnable">
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
              <span class="collapse-title">{{ $t('task.fingerprintScan') }} <el-tag v-if="form.fingerprintEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.fingerprintEnable" />
            </el-form-item>
            <template v-if="form.fingerprintEnable">
              <el-form-item :label="$t('task.probeTool')">
                <el-radio-group v-model="form.fingerprintTool">
                  <el-radio label="httpx">Httpx</el-radio>
                  <el-radio label="builtin">{{ $t('task.builtinEngine') }}</el-radio>
                </el-radio-group>
                <span class="form-hint">{{ form.fingerprintTool === 'httpx' ? $t('task.httpxWappalyzer') : $t('task.sdkWappalyzer') }}</span>
              </el-form-item>
              <el-form-item :label="$t('task.additionalFeatures')">
                <el-checkbox v-model="form.fingerprintIconHash">{{ $t('task.iconHash') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintCustomEngine">{{ $t('task.customFingerprint') }}</el-checkbox>
                <el-checkbox v-model="form.fingerprintScreenshot">{{ $t('task.screenshot') }}</el-checkbox>
              </el-form-item>
              <el-form-item :label="$t('task.filterMode')">
                <el-radio-group v-model="form.fingerprintFilterMode">
                  <el-radio label="http_mapping">{{ $t('task.httpMappingMode') }}</el-radio>
                  <el-radio label="service_mapping">{{ $t('task.serviceMappingMode') }}</el-radio>
                </el-radio-group>
                <span class="form-hint">{{ form.fingerprintFilterMode === 'http_mapping' ? $t('task.httpMappingModeHint') : $t('task.serviceMappingModeHint') }}</span>
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

          <!-- 目录扫描 -->
          <el-collapse-item name="dirscan">
            <template #title>
              <span class="collapse-title">{{ $t('task.dirScan') }} <el-tag v-if="form.dirscanEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.dirscanEnable" />
              <span class="form-hint">{{ $t('task.dirScanHint') }}</span>
            </el-form-item>
            <template v-if="form.dirscanEnable">
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
              <el-form-item :label="$t('task.validStatusCodes')">
                <el-checkbox-group v-model="form.dirscanStatusCodes">
                  <el-checkbox :label="200">200</el-checkbox>
                  <el-checkbox :label="201">201</el-checkbox>
                  <el-checkbox :label="204">204</el-checkbox>
                  <el-checkbox :label="301">301</el-checkbox>
                  <el-checkbox :label="302">302</el-checkbox>
                  <el-checkbox :label="401">401</el-checkbox>
                  <el-checkbox :label="403">403</el-checkbox>
                  <el-checkbox :label="500">500</el-checkbox>
                </el-checkbox-group>
              </el-form-item>
              <el-form-item :label="$t('task.followRedirect')">
                <el-switch v-model="form.dirscanFollowRedirect" />
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- 漏洞扫描 -->
          <el-collapse-item name="pocscan">
            <template #title>
              <span class="collapse-title">{{ $t('task.vulScan') }} <el-tag v-if="form.pocscanEnable" type="success" size="small">{{ $t('task.started') }}</el-tag></span>
            </template>
            <el-form-item :label="$t('task.enable')">
              <el-switch v-model="form.pocscanEnable" />
              <span class="form-hint">{{ $t('task.useNucleiEngine') }}</span>
            </el-form-item>
            <template v-if="form.pocscanEnable">
              <el-form-item :label="$t('task.pocSource')">
                <el-radio-group v-model="form.pocscanMode" @change="handlePocModeChange">
                  <el-radio label="auto">{{ $t('task.autoMatch') }}</el-radio>
                  <el-radio label="manual">{{ $t('task.manualSelect') }}</el-radio>
                </el-radio-group>
              </el-form-item>
              
              <!-- 自动匹配模式 -->
              <template v-if="form.pocscanMode === 'auto'">
                <el-form-item :label="$t('task.autoScan')">
                  <el-checkbox v-model="form.pocscanAutoScan" :disabled="form.pocscanCustomOnly">{{ $t('task.customTagMapping') }}</el-checkbox>
                  <el-checkbox v-model="form.pocscanAutomaticScan" :disabled="form.pocscanCustomOnly || !form.fingerprintEnable">{{ $t('task.webFingerprintAutoMatch') }}</el-checkbox>
                  <span v-if="!form.fingerprintEnable && !form.pocscanCustomOnly" class="form-hint warning-hint">{{ $t('task.needFingerprintScan') }}</span>
                </el-form-item>
                <el-form-item :label="$t('task.customPoc')">
                  <el-checkbox v-model="form.pocscanCustomOnly">{{ $t('task.onlyUseCustomPoc') }}</el-checkbox>
                </el-form-item>
              </template>
              
              <!-- 手动选择模式 -->
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
                  <el-checkbox label="unknown">Unknown</el-checkbox>
                </el-checkbox-group>
              </el-form-item>
              <el-form-item :label="$t('task.targetTimeout')">
                <el-input-number v-model="form.pocscanTargetTimeout" :min="30" :max="600" />
                <span class="form-hint">{{ $t('task.seconds') }}</span>
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

        <!-- 操作按钮 -->
        <div class="form-actions">
          <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ isEdit ? $t('common.save') : $t('task.createTask') }}</el-button>
          <el-button @click="handleCancel">{{ $t('common.cancel') }}</el-button>
        </div>
      </el-form>
    </el-card>

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
                    <el-option label="Unknown" value="unknown" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-input v-model="nucleiTemplateFilter.tag" :placeholder="$t('task.tags')" clearable style="width: 120px" @keyup.enter="loadNucleiTemplatesForSelect" />
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
                <el-table-column prop="tags" :label="$t('task.tags')" min-width="100">
                  <template #default="{ row }">
                    <el-tag v-for="tag in (row.tags || []).slice(0, 2)" :key="tag" size="small" style="margin-right: 3px">{{ tag }}</el-tag>
                    <span v-if="row.tags && row.tags.length > 2" class="secondary-hint">+{{ row.tags.length - 2 }}</span>
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
                    <el-option label="Unknown" value="unknown" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-input v-model="customPocFilter.tag" :placeholder="$t('task.tags')" clearable style="width: 120px" @keyup.enter="loadCustomPocsForSelect" />
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
                <span>{{ $t('task.defaultTemplate') }} ({{ filteredSelectedNucleiTemplates.length }}<template v-if="selectedPocSearchKeyword">/{{ selectedNucleiTemplates.length }}</template>)</span>
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
                <span>{{ $t('task.customPoc') }} ({{ filteredSelectedCustomPocs.length }}<template v-if="selectedPocSearchKeyword">/{{ selectedCustomPocs.length }}</template>)</span>
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
        <el-descriptions-item :label="$t('task.tags')" :span="2">
          <el-tag v-for="tag in (currentViewPoc.tags || [])" :key="tag" size="small" style="margin-right: 5px">{{ tag }}</el-tag>
          <span v-if="!currentViewPoc.tags || currentViewPoc.tags.length === 0">-</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('common.description')" :span="2">{{ currentViewPoc.description || '-' }}</el-descriptions-item>
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
import { ref, reactive, onMounted, watch, nextTick, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Close, Search, InfoFilled } from '@element-plus/icons-vue'
import { createTask, updateTask, getTaskDetail, startTask, getWorkerList, getScanConfig, saveScanConfig } from '@/api/task'
import { getNucleiTemplateList, getCustomPocList, getNucleiTemplateDetail } from '@/api/poc'
import { getDirScanDictEnabledList } from '@/api/dirscan'
import { getSubdomainDictEnabledList } from '@/api/subdomain'
import { useWorkspaceStore } from '@/stores/workspace'
import request from '@/api/request'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const formRef = ref()
const submitting = ref(false)
const workspaces = ref([])
const organizations = ref([])
const workers = ref([])
const activeCollapse = ref(['portscan', 'fingerprint'])
const isEdit = ref(false)

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
const nucleiTemplateFilter = reactive({
  keyword: '',
  severity: '',
  category: '',
  tag: ''
})
const customPocFilter = reactive({
  name: '',
  severity: '',
  tag: ''
})
const nucleiTemplatePagination = reactive({ page: 1, pageSize: 50, total: 0 })
const customPocPagination = reactive({ page: 1, pageSize: 50, total: 0 })

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
  domainscanBruteforce: false, // 字典爆破
  domainscanTimeout: 300,
  domainscanMaxEnumTime: 10,
  domainscanThreads: 10,
  domainscanRateLimit: 0,
  domainscanRemoveWildcard: true,
  domainscanResolveDNS: true,
  domainscanConcurrent: 50,
  subdomainDictIds: [], // 子域名暴力破解字典
  subdomainDicts: [], // 保存已选择的字典信息
  // Dnsx增强功能
  domainscanRecursiveBrute: false, // 递归爆破
  recursiveDictIds: [], // 递归爆破字典ID列表
  recursiveDicts: [], // 保存已选择的递归字典信息
  domainscanWildcardDetect: true,  // 泛解析检测
  domainscanSubdomainCrawl: false, // 子域爬取
  domainscanTakeoverCheck: false,  // 子域接管检查
  // 端口扫描
  portscanEnable: true,
  portscanTool: 'naabu',
  portscanRate: 3000, // 提高默认值从1000到3000
  ports: 'top100',
  portThreshold: 100,
  scanType: 'c',
  portscanTimeout: 60,
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
  portidentifyArgs: '',
  portidentifyUDP: false,
  portidentifyFastMode: false,
  // 指纹识别
  fingerprintEnable: true,
  fingerprintTool: 'httpx',
  fingerprintIconHash: true,
  fingerprintCustomEngine: false,
  fingerprintScreenshot: false,
  fingerprintActiveScan: false,
  fingerprintActiveTimeout: 10,
  fingerprintTimeout: 30,
  fingerprintFilterMode: 'http_mapping', // 过滤模式: http_mapping(HTTP映射) 或 service_mapping(服务映射)
  // 漏洞扫描
  pocscanEnable: false,
  pocscanMode: 'auto',
  pocscanAutoScan: true,
  pocscanAutomaticScan: true,
  pocscanCustomOnly: false,
  pocscanSeverity: ['critical', 'high', 'medium'],
  pocscanTargetTimeout: 600,
  pocscanNucleiTemplateIds: [],
  pocscanCustomPocIds: [],
  // 保存已选择的对象信息（用于显示名称）
  pocscanNucleiTemplates: [],
  pocscanCustomPocs: [],
  // 目录扫描
  dirscanEnable: false,
  dirscanDictIds: [],
  dirscanDicts: [], // 保存已选择的字典信息
  dirscanThreads: 50,
  dirscanTimeout: 10,
  dirscanStatusCodes: [200, 301, 302, 401, 403],
  dirscanFollowRedirect: false
})

const rules = {
  name: [{ required: true, message: () => t('task.pleaseEnterTaskName'), trigger: 'blur' }],
  target: [{ required: true, message: () => t('task.pleaseEnterTarget'), trigger: 'blur' }]
}

onMounted(async () => {
  await loadWorkspaces()
  await loadOrganizations()
  await loadWorkers()
  
  // 检查是否是编辑模式
  if (route.query.id) {
    isEdit.value = true
    await loadTaskDetail(route.query.id)
  } else {
    // 加载用户上次保存的扫描配置
    try {
      const res = await getScanConfig()
      if (res.code === 0 && res.config) {
        const config = JSON.parse(res.config)
        applyConfig(config)
      }
    } catch (e) { console.error('加载扫描配置失败:', e) }
    
    // 设置默认工作空间
    let wsId = workspaceStore.currentWorkspaceId
    if (wsId === 'all' || !wsId) {
      const defaultWs = workspaces.value.find(ws => ws.name === '默认工作空间')
      wsId = defaultWs ? defaultWs.id : (workspaces.value.length > 0 ? workspaces.value[0].id : '')
    }
    form.workspaceId = wsId
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

async function loadWorkspaces() {
  try {
    const res = await request.post('/workspace/list', { page: 1, pageSize: 100 })
    if (res.code === 0) workspaces.value = res.list || []
  } catch (e) { console.error(e) }
}

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
  // 判断POC模式：如果有nucleiTemplateIds或customPocIds，则为手动模式
  const isManualMode = (config.pocscan?.nucleiTemplateIds?.length > 0) || (config.pocscan?.customPocIds?.length > 0)
  
  // 判断是否启用字典爆破：如果有subdomainDictIds则启用
  const hasBruteforce = config.domainscan?.subdomainDictIds?.length > 0
  
  Object.assign(form, {
    batchSize: config.batchSize || 50,
    // 子域名扫描
    domainscanEnable: config.domainscan?.enable ?? false,
    domainscanSubfinder: config.domainscan?.subfinder ?? true,
    domainscanBruteforce: hasBruteforce,
    domainscanTimeout: config.domainscan?.timeout || 300,
    domainscanMaxEnumTime: config.domainscan?.maxEnumerationTime || 10,
    domainscanThreads: config.domainscan?.threads || 10,
    domainscanRateLimit: config.domainscan?.rateLimit || 0,
    domainscanRemoveWildcard: config.domainscan?.removeWildcard ?? true,
    domainscanResolveDNS: config.domainscan?.resolveDNS ?? true,
    domainscanConcurrent: config.domainscan?.concurrent || 50,
    subdomainDictIds: config.domainscan?.subdomainDictIds || [],
    // Dnsx增强功能
    domainscanRecursiveBrute: config.domainscan?.recursiveBrute ?? false,
    recursiveDictIds: config.domainscan?.recursiveDictIds || [],
    domainscanWildcardDetect: config.domainscan?.wildcardDetect ?? true,
    domainscanSubdomainCrawl: config.domainscan?.subdomainCrawl ?? false,
    domainscanTakeoverCheck: config.domainscan?.takeoverCheck ?? false,
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
    fingerprintScreenshot: config.fingerprint?.screenshot ?? false,
    fingerprintActiveScan: config.fingerprint?.activeScan ?? false,
    fingerprintActiveTimeout: config.fingerprint?.activeTimeout || 10,
    fingerprintTimeout: config.fingerprint?.targetTimeout || 30,
    fingerprintFilterMode: config.fingerprint?.filterMode || 'http_mapping',
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
    // 目录扫描
    dirscanEnable: config.dirscan?.enable ?? false,
    dirscanDictIds: config.dirscan?.dictIds || [],
    dirscanThreads: config.dirscan?.threads || 50,
    dirscanTimeout: config.dirscan?.timeout || 10,
    dirscanStatusCodes: config.dirscan?.statusCodes || [200, 301, 302, 401, 403],
    dirscanFollowRedirect: config.dirscan?.followRedirect ?? false
  })
}

// 防抖保存配置
let saveConfigTimer = null
function debounceSaveConfig() {
  if (saveConfigTimer) clearTimeout(saveConfigTimer)
  saveConfigTimer = setTimeout(() => {
    const config = buildConfig()
    saveScanConfig({ config: JSON.stringify(config) }).catch(e => console.error('自动保存配置失败:', e))
  }, 500)
}

// 监听扫描配置变化，自动保存（仅在新建任务时）
// 使用 getter 函数返回配置字段的快照
watch(
  () => JSON.stringify({
    batchSize: form.batchSize,
    domainscanEnable: form.domainscanEnable,
    domainscanSubfinder: form.domainscanSubfinder,
    domainscanBruteforce: form.domainscanBruteforce,
    domainscanTimeout: form.domainscanTimeout,
    domainscanMaxEnumTime: form.domainscanMaxEnumTime,
    domainscanThreads: form.domainscanThreads,
    domainscanRateLimit: form.domainscanRateLimit,
    domainscanRemoveWildcard: form.domainscanRemoveWildcard,
    domainscanResolveDNS: form.domainscanResolveDNS,
    domainscanConcurrent: form.domainscanConcurrent,
    subdomainDictIds: form.subdomainDictIds,
    // Dnsx增强功能
    domainscanRecursiveBrute: form.domainscanRecursiveBrute,
    recursiveDictIds: form.recursiveDictIds,
    domainscanWildcardDetect: form.domainscanWildcardDetect,
    domainscanSubdomainCrawl: form.domainscanSubdomainCrawl,
    domainscanTakeoverCheck: form.domainscanTakeoverCheck,
    portscanEnable: form.portscanEnable,
    portscanTool: form.portscanTool,
    portscanRate: form.portscanRate,
    ports: form.ports,
    portThreshold: form.portThreshold,
    scanType: form.scanType,
    portscanTimeout: form.portscanTimeout,
    skipHostDiscovery: form.skipHostDiscovery,
    excludeCDN: form.excludeCDN,
    excludeHosts: form.excludeHosts,
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
    fingerprintActiveScan: form.fingerprintActiveScan,
    fingerprintActiveTimeout: form.fingerprintActiveTimeout,
    fingerprintTimeout: form.fingerprintTimeout,
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
    dirscanThreads: form.dirscanThreads,
    dirscanTimeout: form.dirscanTimeout,
    dirscanStatusCodes: form.dirscanStatusCodes,
    dirscanFollowRedirect: form.dirscanFollowRedirect
  }),
  () => {
    if (!isEdit.value) {
      debounceSaveConfig()
    }
  }
)

function buildConfig() {
  const config = {
    batchSize: form.batchSize,
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
      // Dnsx增强功能（只有启用字典爆破时才生效）
      recursiveBrute: form.domainscanBruteforce ? form.domainscanRecursiveBrute : false,
      recursiveDictIds: (form.domainscanBruteforce && form.domainscanRecursiveBrute) ? (form.recursiveDictIds || []) : [],
      wildcardDetect: form.domainscanBruteforce ? form.domainscanWildcardDetect : false,
      subdomainCrawl: form.domainscanBruteforce ? form.domainscanSubdomainCrawl : false,
      takeoverCheck: form.domainscanBruteforce ? form.domainscanTakeoverCheck : false
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
      excludeHosts: form.excludeHosts
    },
    portidentify: {
      enable: form.portidentifyEnable,
      tool: form.portidentifyTool,
      timeout: form.portidentifyTimeout,
      concurrency: form.portidentifyConcurrency,
      args: form.portidentifyArgs,
      udp: form.portidentifyUDP,
      fastMode: form.portidentifyFastMode
    },
    fingerprint: {
      enable: form.fingerprintEnable,
      tool: form.fingerprintTool,
      iconHash: form.fingerprintIconHash,
      customEngine: form.fingerprintCustomEngine,
      screenshot: form.fingerprintScreenshot,
      activeScan: form.fingerprintActiveScan,
      activeTimeout: form.fingerprintActiveTimeout,
      targetTimeout: form.fingerprintTimeout,
      filterMode: form.fingerprintFilterMode
    },
    pocscan: {
      enable: form.pocscanEnable,
      useNuclei: true,
      severity: form.pocscanSeverity.join(','),
      targetTimeout: form.pocscanTargetTimeout
    },
    dirscan: {
      enable: form.dirscanEnable,
      dictIds: form.dirscanDictIds,
      threads: form.dirscanThreads,
      timeout: form.dirscanTimeout,
      statusCodes: form.dirscanStatusCodes,
      followRedirect: form.dirscanFollowRedirect
    }
  }

  // 根据POC模式设置不同的配置
  if (form.pocscanMode === 'manual') {
    // 手动选择模式
    config.pocscan.nucleiTemplateIds = form.pocscanNucleiTemplateIds
    config.pocscan.customPocIds = form.pocscanCustomPocIds
    config.pocscan.autoScan = false
    config.pocscan.automaticScan = false
    config.pocscan.customPocOnly = false
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

async function handleSubmit() {
  try {
    await formRef.value.validate()
  } catch (e) { return }

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
        console.warn('保存用户配置失败:', e)
        // 不阻断任务创建流程
      }
    }

    const config = buildConfig()
    const params = {
      name: form.name,
      target: form.target,
      workspaceId: form.workspaceId,
      orgId: form.orgId,
      workers: form.workers,
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
        await startTask({ id: res.id, workspaceId: form.workspaceId })
        ElMessage.success(t('task.taskStarted'))
      }
      router.push('/task')
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
  if (mode === 'manual' && !form.pocscanNucleiTemplateIds.length && !form.pocscanCustomPocIds.length) {
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
  // 加载当前页数据
  await Promise.all([loadNucleiTemplatesForSelect(), loadCustomPocsForSelect()])
  // 等待DOM更新后恢复选中状态
  await nextTick()
  restoreTableSelections()
}

function restoreTableSelections() {
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

async function loadNucleiTemplatesForSelect() {
  nucleiTemplateLoading.value = true
  isLoadingData.value = true
  try {
    const res = await getNucleiTemplateList({
      page: nucleiTemplatePagination.page,
      pageSize: nucleiTemplatePagination.pageSize,
      keyword: nucleiTemplateFilter.keyword,
      severity: nucleiTemplateFilter.severity,
      category: nucleiTemplateFilter.category,
      tag: nucleiTemplateFilter.tag
    })
    if (res.code === 0) {
      nucleiTemplateList.value = res.list || []
      nucleiTemplatePagination.total = res.total || 0
      // 等待DOM更新后恢复当前页的选中状态
      await nextTick()
      restoreNucleiTableSelection()
    }
  } catch (e) {
    console.error('加载Nuclei模板失败:', e)
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
      name: customPocFilter.name,
      severity: customPocFilter.severity,
      tag: customPocFilter.tag,
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
    console.error('加载自定义POC失败:', e)
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
  // 清空表格选择状态
  if (nucleiTableRef.value) {
    nucleiTableRef.value.clearSelection()
  }
  if (customPocTableRef.value) {
    customPocTableRef.value.clearSelection()
  }
}

// 选择全部Nuclei模板（根据当前筛选条件）
async function selectAllNucleiTemplates() {
  selectAllNucleiLoading.value = true
  isSelectingAll.value = true
  try {
    // 先获取总数
    const firstRes = await getNucleiTemplateList({
      page: 1,
      pageSize: 1,
      keyword: nucleiTemplateFilter.keyword,
      severity: nucleiTemplateFilter.severity,
      category: nucleiTemplateFilter.category,
      tag: nucleiTemplateFilter.tag
    })
    if (firstRes.code !== 0) {
      throw new Error(firstRes.msg || '获取数据失败')
    }
    
    const total = firstRes.total || 0
    if (total === 0) {
      ElMessage.warning(t('task.noMatchingTemplate'))
      return
    }
    
    // 分页获取所有数据
    const pageSize = 5000
    const totalPages = Math.ceil(total / pageSize)
    const allTemplates = []
    
    for (let page = 1; page <= totalPages; page++) {
      const res = await getNucleiTemplateList({
        page,
        pageSize,
        keyword: nucleiTemplateFilter.keyword,
        severity: nucleiTemplateFilter.severity,
        category: nucleiTemplateFilter.category,
        tag: nucleiTemplateFilter.tag
      })
      if (res.code === 0 && res.list) {
        allTemplates.push(...res.list)
      }
    }
    
    // 合并到已选择列表（去重）
    const existingIds = new Set(selectedNucleiTemplateIds.value)
    let addedCount = 0
    allTemplates.forEach(t => {
      if (!existingIds.has(t.id)) {
        selectedNucleiTemplateIds.value.push(t.id)
        selectedNucleiTemplates.value.push({ id: t.id, name: t.name })
        addedCount++
      }
    })
    
    // 更新当前页表格选中状态
    await nextTick()
    if (nucleiTableRef.value) {
      nucleiTemplateList.value.forEach(row => {
        nucleiTableRef.value.toggleRowSelection(row, true)
      })
    }
    const addedText = addedCount < allTemplates.length ? t('task.newlyAdded', { count: addedCount }) : ''
    ElMessage.success(t('task.selectedTemplatesCount', { total: allTemplates.length, added: addedText }))
  } catch (e) {
    console.error('选择全部失败:', e)
    ElMessage.error(t('task.selectAllFailed'))
  } finally {
    selectAllNucleiLoading.value = false
    isSelectingAll.value = false
  }
}

// 选择全部自定义POC（根据当前筛选条件）
async function selectAllCustomPocs() {
  selectAllCustomLoading.value = true
  isSelectingAll.value = true
  try {
    // 先获取总数
    const firstRes = await getCustomPocList({
      page: 1,
      pageSize: 1,
      name: customPocFilter.name,
      severity: customPocFilter.severity,
      tag: customPocFilter.tag,
      enabled: true
    })
    if (firstRes.code !== 0) {
      throw new Error(firstRes.msg || '获取数据失败')
    }
    
    const total = firstRes.total || 0
    if (total === 0) {
      ElMessage.warning(t('task.noMatchingPoc'))
      return
    }
    
    // 分页获取所有数据
    const pageSize = 5000
    const totalPages = Math.ceil(total / pageSize)
    const allPocs = []
    
    for (let page = 1; page <= totalPages; page++) {
      const res = await getCustomPocList({
        page,
        pageSize,
        name: customPocFilter.name,
        severity: customPocFilter.severity,
        tag: customPocFilter.tag,
        enabled: true
      })
      if (res.code === 0 && res.list) {
        allPocs.push(...res.list)
      }
    }
    
    // 合并到已选择列表（去重）
    const existingIds = new Set(selectedCustomPocIds.value)
    let addedCount = 0
    allPocs.forEach(p => {
      if (!existingIds.has(p.id)) {
        selectedCustomPocIds.value.push(p.id)
        selectedCustomPocs.value.push({ id: p.id, name: p.name, templateId: p.templateId })
        addedCount++
      }
    })
    
    // 更新当前页表格选中状态
    await nextTick()
    if (customPocTableRef.value) {
      customPocList.value.forEach(row => {
        customPocTableRef.value.toggleRowSelection(row, true)
      })
    }
    const addedText = addedCount < allPocs.length ? t('task.newlyAdded', { count: addedCount }) : ''
    ElMessage.success(t('task.selectedPocsCount', { total: allPocs.length, added: addedText }))
  } catch (e) {
    console.error('选择全部失败:', e)
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
      console.error('获取POC内容失败:', e)
      currentViewPoc.value.content = t('task.getContentFailed')
    } finally {
      pocContentLoading.value = false
    }
  }
}

// 复制POC内容
function copyPocContent() {
  if (currentViewPoc.value.content) {
    navigator.clipboard.writeText(currentViewPoc.value.content).then(() => {
      ElMessage.success(t('task.copiedToClipboard'))
    }).catch(() => {
      ElMessage.error(t('task.copyFailed'))
    })
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
    console.error('加载字典列表失败:', e)
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
    console.error('加载子域名字典列表失败:', e)
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
    console.error('加载递归字典列表失败:', e)
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

    .collapse-title {
      display: flex;
      align-items: center;
      gap: 10px;
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
