<template>
  <div class="task-page">
    <!-- 操作栏 -->
    <el-card class="action-card">
      <el-button type="primary" @click="goToCreateTask">
        <el-icon><Plus /></el-icon>新建任务
      </el-button>
      <el-switch
        v-model="autoRefresh"
        style="margin-left: 20px"
        active-text="自动刷新(间隔30秒)"
        inactive-text=""
        @change="handleAutoRefreshChange"
      />
    </el-card>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <div style="margin-bottom: 15px">
        <el-button type="danger" :disabled="selectedRows.length === 0" @click="handleBatchDelete">
          <el-icon><Delete /></el-icon>批量删除 ({{ selectedRows.length }})
        </el-button>
      </div>
      <el-table :data="tableData" v-loading="loading" stripe max-height="500" @selection-change="handleSelectionChange">
        <el-table-column type="selection" width="50" />
        <el-table-column prop="name" label="任务名称" min-width="150" />
        <el-table-column prop="target" label="扫描目标" min-width="200" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status, row)">{{ getStatusText(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="progress" label="进度" width="150">
          <template #default="{ row }">
            <div>
              <el-progress :percentage="row.progress" :stroke-width="6" />
              <div v-if="row.subTaskCount > 1" class="sub-task-info">
                子任务: {{ row.subTaskDone }}/{{ row.subTaskCount }}
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="定时任务" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.isCron" type="success" size="small">{{ row.cronRule }}</el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="createTime" label="创建时间" width="160" />
        <el-table-column prop="startTime" label="开始时间" width="160">
          <template #default="{ row }">
            {{ row.startTime || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="endTime" label="结束时间" width="160">
          <template #default="{ row }">
            {{ row.endTime || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="320" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status === 'CREATED' || !row.status" type="success" link size="small" @click="handleStart(row)">启动</el-button>
            <el-button v-if="row.status === 'CREATED' || !row.status" type="warning" link size="small" @click="goToEditTask(row)">编辑</el-button>
            <el-button v-if="['STARTED', 'PENDING'].includes(row.status)" type="warning" link size="small" @click="handlePause(row)">暂停</el-button>
            <el-button v-if="row.status === 'PAUSED'" type="success" link size="small" @click="handleResume(row)">继续</el-button>
            <el-button v-if="['STARTED', 'PAUSED', 'PENDING', 'CREATED', ''].includes(row.status) && row.status !== 'SUCCESS' && row.status !== 'FAILURE' && row.status !== 'STOPPED'" type="danger" link size="small" @click="handleStop(row)">停止</el-button>
            <el-button type="primary" link size="small" @click="showDetail(row)">详情</el-button>
            <el-button type="info" link size="small" @click="showLogs(row)">日志</el-button>
            <el-button type="info" link size="small" @click="viewReport(row)">报告</el-button>
            <el-button v-if="['SUCCESS', 'FAILURE', 'STOPPED'].includes(row.status)" type="warning" link size="small" @click="handleRetry(row)">重新执行</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">删除</el-button>
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

    <!-- 任务详情对话框 -->
    <el-dialog v-model="detailVisible" title="任务详情" width="850px">
      <el-descriptions :column="2" border>
        <el-descriptions-item label="任务名称">{{ currentTask.name }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(currentTask.status, currentTask)">{{ getStatusText(currentTask) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="进度">
          <div style="display: flex; align-items: center; gap: 10px;">
            <el-progress :percentage="currentTask.progress" :stroke-width="10" style="width: 120px" />
            <span style="color: #909399; font-size: 12px;">{{ currentTask.subTaskDone || 0 }}/{{ currentTask.subTaskCount || 0 }}</span>
          </div>
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ currentTask.createTime }}</el-descriptions-item>
        <el-descriptions-item label="开始时间">{{ currentTask.startTime || '-' }}</el-descriptions-item>
        <el-descriptions-item label="结束时间">
          {{ (currentTask.status === 'SUCCESS' || currentTask.status === 'FAILURE' || currentTask.status === 'STOPPED') ? (currentTask.endTime || '-') : '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="扫描目标" :span="2">
          <div style="max-height: 100px; overflow-y: auto; white-space: pre-wrap">{{ currentTask.target }}</div>
        </el-descriptions-item>
        <el-descriptions-item label="执行结果" :span="2">
          <div style="max-height: 100px; overflow-y: auto">{{ currentTask.result || '-' }}</div>
        </el-descriptions-item>
      </el-descriptions>
      
      <!-- 任务配置详情 -->
      <div v-if="parsedConfig" class="config-section">
        <h4 style="margin: 15px 0 10px">扫描配置</h4>
        <el-descriptions :column="3" border size="small">
          <el-descriptions-item label="子域名扫描">
            <el-tag :type="parsedConfig.domainscan?.enable ? 'success' : 'info'" size="small">
              {{ parsedConfig.domainscan?.enable ? '开启' : '关闭' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="端口扫描">
            <el-tag :type="parsedConfig.portscan?.enable !== false ? 'success' : 'info'" size="small">
              {{ parsedConfig.portscan?.enable !== false ? '开启' : '关闭' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="端口识别">
            <el-tag :type="parsedConfig.portidentify?.enable ? 'success' : 'info'" size="small">
              {{ parsedConfig.portidentify?.enable ? '开启' : '关闭' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="指纹识别">
            <el-tag :type="parsedConfig.fingerprint?.enable ? 'success' : 'info'" size="small">
              {{ parsedConfig.fingerprint?.enable ? '开启' : '关闭' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="漏洞扫描">
            <el-tag :type="parsedConfig.pocscan?.enable ? 'success' : 'info'" size="small">
              {{ parsedConfig.pocscan?.enable ? '开启' : '关闭' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="目录扫描">
            <el-tag :type="parsedConfig.dirscan?.enable ? 'success' : 'info'" size="small">
              {{ parsedConfig.dirscan?.enable ? '开启' : '关闭' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="任务拆分">
            {{ parsedConfig.batchSize === 0 ? '不拆分' : ((parsedConfig.batchSize || 50) + ' 个/批') }}
          </el-descriptions-item>
        </el-descriptions>
        
        <!-- 子域名扫描配置 -->
        <div v-if="parsedConfig.domainscan?.enable" class="config-detail">
          <el-descriptions :column="4" border size="small" title="子域名扫描配置">
            <el-descriptions-item label="使用Subfinder">{{ parsedConfig.domainscan?.subfinder !== false ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="超时时间">{{ parsedConfig.domainscan?.timeout || 300 }}秒</el-descriptions-item>
            <el-descriptions-item label="并发线程">{{ parsedConfig.domainscan?.threads || 10 }}</el-descriptions-item>
            <el-descriptions-item label="DNS解析">{{ parsedConfig.domainscan?.resolveDNS ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="去除泛解析">{{ parsedConfig.domainscan?.removeWildcard ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="并发数">{{ parsedConfig.domainscan?.concurrent || 50 }}</el-descriptions-item>
            <el-descriptions-item label="速率限制">{{ parsedConfig.domainscan?.rateLimit || 0 }}</el-descriptions-item>
            <el-descriptions-item label="字典爆破">{{ parsedConfig.domainscan?.subdomainDictIds?.length ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.domainscan?.subdomainDictIds?.length" label="爆破字典数" :span="2">
              {{ parsedConfig.domainscan.subdomainDictIds.length }} 个
            </el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.domainscan?.recursiveBrute" label="递归爆破">是</el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.domainscan?.wildcardDetect" label="泛解析检测">是</el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.domainscan?.subdomainCrawl" label="子域名爬取">是</el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.domainscan?.takeoverCheck" label="接管检测">是</el-descriptions-item>
          </el-descriptions>
        </div>
        
        <!-- 端口扫描配置 -->
        <div v-if="parsedConfig.portscan?.enable !== false" class="config-detail">
          <el-descriptions :column="4" border size="small" title="端口扫描配置">
            <el-descriptions-item label="扫描工具">{{ parsedConfig.portscan?.tool || 'naabu' }}</el-descriptions-item>
            <el-descriptions-item label="端口范围">{{ parsedConfig.portscan?.ports || 'top100' }}</el-descriptions-item>
            <el-descriptions-item label="扫描速率">{{ parsedConfig.portscan?.rate || 1000 }}</el-descriptions-item>
            <el-descriptions-item label="端口阈值">{{ parsedConfig.portscan?.portThreshold || 100 }}</el-descriptions-item>
            <el-descriptions-item label="扫描类型">{{ parsedConfig.portscan?.scanType === 's' ? 'SYN' : 'CONNECT' }}</el-descriptions-item>
            <el-descriptions-item label="超时时间">{{ parsedConfig.portscan?.timeout || 60 }}秒</el-descriptions-item>
            <el-descriptions-item label="跳过主机发现">{{ parsedConfig.portscan?.skipHostDiscovery ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="排除CDN/WAF">{{ parsedConfig.portscan?.excludeCDN ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="排除目标">{{ parsedConfig.portscan?.excludeHosts || '-' }}</el-descriptions-item>
          </el-descriptions>
        </div>
        
        <!-- 端口识别配置 -->
        <div v-if="parsedConfig.portidentify?.enable" class="config-detail">
          <el-descriptions :column="3" border size="small" title="端口识别配置">
            <el-descriptions-item label="超时时间">{{ parsedConfig.portidentify?.timeout || 30 }}秒</el-descriptions-item>
            <el-descriptions-item label="额外参数">{{ parsedConfig.portidentify?.args || '-' }}</el-descriptions-item>
          </el-descriptions>
        </div>
        
        <!-- 指纹识别配置 -->
        <div v-if="parsedConfig.fingerprint?.enable" class="config-detail">
          <el-descriptions :column="4" border size="small" title="指纹识别配置">
            <el-descriptions-item label="探测工具">
              <el-tag :type="parsedConfig.fingerprint?.tool === 'httpx' ? 'primary' : 'success'" size="small">
                {{ parsedConfig.fingerprint?.tool === 'httpx' ? 'Httpx' : 'Wappalyzer (内置)' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="Icon Hash">{{ parsedConfig.fingerprint?.iconHash ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="自定义指纹">{{ parsedConfig.fingerprint?.customEngine ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="网页截图">{{ parsedConfig.fingerprint?.screenshot ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="主动探测">{{ parsedConfig.fingerprint?.activeScan ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="超时时间">{{ parsedConfig.fingerprint?.targetTimeout || parsedConfig.fingerprint?.timeout || 30 }}秒</el-descriptions-item>
            <el-descriptions-item label="并发数">{{ parsedConfig.fingerprint?.concurrency || 10 }}</el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.fingerprint?.activeScan" label="主动探测超时">{{ parsedConfig.fingerprint?.activeTimeout || 5 }}秒</el-descriptions-item>
          </el-descriptions>
        </div>
        
        <!-- 漏洞扫描配置 -->
        <div v-if="parsedConfig.pocscan?.enable" class="config-detail">
          <el-descriptions :column="3" border size="small" title="漏洞扫描配置">
            <el-descriptions-item label="POC来源">
              {{ parsedConfig.pocscan?.customPocOnly ? '仅自定义POC' : '默认模板+自定义POC' }}
            </el-descriptions-item>
            <el-descriptions-item label="自动扫描(自定义标签)">{{ parsedConfig.pocscan?.autoScan ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="自动扫描(内置映射)">{{ parsedConfig.pocscan?.automaticScan ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="严重级别">{{ parsedConfig.pocscan?.severity || 'critical,high,medium' }}</el-descriptions-item>
            <el-descriptions-item label="目标超时">{{ parsedConfig.pocscan?.targetTimeout || 600 }}秒</el-descriptions-item>
            <el-descriptions-item label="并发数">{{ parsedConfig.pocscan?.concurrency || 25 }}</el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.pocscan?.nucleiTemplateIds?.length" label="指定Nuclei模板" :span="3">
              {{ parsedConfig.pocscan.nucleiTemplateIds.length }} 个
            </el-descriptions-item>
            <el-descriptions-item v-if="parsedConfig.pocscan?.customPocIds?.length" label="指定自定义POC" :span="3">
              {{ parsedConfig.pocscan.customPocIds.length }} 个
            </el-descriptions-item>
          </el-descriptions>
        </div>
        
        <!-- 目录扫描配置 -->
        <div v-if="parsedConfig.dirscan?.enable" class="config-detail">
          <el-descriptions :column="4" border size="small" title="目录扫描配置">
            <el-descriptions-item label="并发数">{{ parsedConfig.dirscan?.threads || parsedConfig.dirscan?.concurrency || 10 }}</el-descriptions-item>
            <el-descriptions-item label="超时时间">{{ parsedConfig.dirscan?.timeout || 10 }}秒</el-descriptions-item>
            <el-descriptions-item label="跟随重定向">{{ parsedConfig.dirscan?.followRedirect ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="状态码过滤">{{ parsedConfig.dirscan?.statusCodes || '200,301,302,403' }}</el-descriptions-item>
            <el-descriptions-item label="使用字典">
              {{ parsedConfig.dirscan?.dictIds?.length ? (parsedConfig.dirscan.dictIds.length + ' 个') : '默认字典' }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </div>
    </el-dialog>

    <!-- 新建/编辑任务对话框 - Tab页布局 -->
    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑任务' : '新建任务'" width="720px" top="5vh" class="task-dialog">
      <el-tabs v-model="activeTab" class="task-tabs">
        <!-- 基本信息 Tab -->
        <el-tab-pane label="基本信息" name="basic">
          <el-form ref="formRef" :model="form" :rules="rules" label-width="100px" class="tab-form">
            <el-form-item label="任务名称" prop="name">
              <el-input v-model="form.name" placeholder="请输入任务名称" />
            </el-form-item>
            <el-form-item label="扫描目标" prop="target">
              <el-input v-model="form.target" type="textarea" :rows="6" placeholder="每行一个目标，支持格式:&#10;• IP: 192.168.1.1&#10;• CIDR: 192.168.1.0/24&#10;• IP范围: 192.168.1.1-192.168.1.100&#10;• 域名: example.com" />
            </el-form-item>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="工作空间">
                  <el-select v-model="form.workspaceId" placeholder="选择工作空间" clearable style="width: 100%">
                    <el-option v-for="ws in workspaceStore.workspaces" :key="ws.id" :label="ws.name" :value="ws.id" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="所属组织">
                  <el-select v-model="form.orgId" placeholder="选择组织" clearable style="width: 100%">
                    <el-option v-for="org in organizations" :key="org.id" :label="org.name" :value="org.id" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="指定Worker">
              <el-select v-model="form.workers" multiple placeholder="不选则任意Worker执行" clearable style="width: 100%">
                <el-option v-for="w in workers" :key="w.name" :label="`${w.name} (${w.ip})`" :value="w.name" />
              </el-select>
            </el-form-item>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="定时任务">
                  <el-switch v-model="form.isCron" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item v-if="form.isCron" label="Cron表达式">
                  <el-input v-model="form.cronRule" placeholder="0 0 * * *" />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-tab-pane>

        <!-- 子域名扫描 Tab -->
        <el-tab-pane name="domainscan">
          <template #label>
            <span>子域名扫描 <el-tag v-if="form.domainscanEnable" type="success" size="small" style="margin-left:4px">开</el-tag></span>
          </template>
          <el-form label-width="120px" class="tab-form">
            <el-form-item label="启用">
              <el-switch v-model="form.domainscanEnable" />
              <span class="form-hint">针对域名目标进行子域名枚举</span>
            </el-form-item>
            <template v-if="form.domainscanEnable">
              <el-form-item label="使用Subfinder">
                <el-switch v-model="form.domainscanSubfinder" />
                <span class="form-hint">使用Subfinder进行子域名枚举</span>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item label="超时时间(秒)">
                    <el-input-number v-model="form.domainscanTimeout" :min="60" :max="3600" style="width:100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="最大枚举时间(分)">
                    <el-input-number v-model="form.domainscanMaxEnumTime" :min="1" :max="60" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item label="速率限制">
                    <el-input-number v-model="form.domainscanRateLimit" :min="0" :max="1000" style="width:100%" />
                    <span class="form-hint">0=不限制</span>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="扫描选项">
                <el-checkbox v-model="form.domainscanRemoveWildcard">移除泛解析域名</el-checkbox>
              </el-form-item>
              <el-form-item label="DNS解析">
                <el-checkbox v-model="form.domainscanResolveDNS">解析子域名DNS</el-checkbox>
                <span class="form-hint">并发数由Worker设置控制</span>
              </el-form-item>
            </template>
            <el-alert v-if="!form.domainscanEnable" type="info" :closable="false" show-icon>
              <template #title>子域名扫描使用 Subfinder 对域名目标进行子域名枚举，发现的子域名将自动加入扫描目标</template>
            </el-alert>
          </el-form>
        </el-tab-pane>

        <!-- 端口扫描 Tab -->
        <el-tab-pane name="portscan">
          <template #label>
            <span>端口扫描 <el-tag v-if="form.portscanEnable" type="success" size="small" style="margin-left:4px">开</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item label="启用">
              <el-switch v-model="form.portscanEnable" />
            </el-form-item>
            <template v-if="form.portscanEnable">
              <el-form-item label="扫描工具">
                <el-radio-group v-model="form.portscanTool">
                  <el-radio label="naabu">Naabu (推荐)</el-radio>
                  <el-radio label="masscan" :disabled="!availableTools.masscan">
                    Masscan <span v-if="!availableTools.masscan" class="tool-tip">(未安装)</span>
                  </el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item label="端口范围">
                <el-select v-model="form.ports" filterable allow-create default-first-option style="width: 100%">
                  <el-option label="top100 - 常用100端口" value="top100" />
                  <el-option label="top1000 - 常用1000端口" value="top1000" />
                  <el-option label="80,443,8080,8443 - Web常用" value="80,443,8080,8443" />
                  <el-option label="1-65535 - 全端口" value="1-65535" />
                </el-select>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item label="扫描速率">
                    <el-input-number v-model="form.portscanRate" :min="100" :max="100000" style="width:100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="端口阈值">
                    <el-input-number v-model="form.portThreshold" :min="0" :max="65535" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item v-if="form.portscanTool === 'naabu'" label="扫描类型">
                    <el-radio-group v-model="form.scanType">
                      <el-radio label="c">CONNECT</el-radio>
                      <el-radio label="s">SYN</el-radio>
                    </el-radio-group>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="超时(秒)">
                    <el-input-number v-model="form.portscanTimeout" :min="5" :max="1200" style="width:100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="高级选项">
                <div style="display: block; width: 100%">
                  <el-checkbox v-model="form.skipHostDiscovery">跳过主机发现 (-Pn)</el-checkbox>
                  <span class="form-hint">跳过主机存活检测，直接扫描端口</span>
                </div>
                <div v-if="form.portscanTool === 'naabu'" style="display: block; width: 100%; margin-top: 8px">
                  <el-checkbox v-model="form.excludeCDN">排除 CDN/WAF (-ec)</el-checkbox>
                  <span class="form-hint">CDN/WAF IP 仅扫描 80,443 端口</span>
                </div>
              </el-form-item>
              <el-form-item label="排除目标">
                <el-input v-model="form.excludeHosts" placeholder="192.168.1.1,10.0.0.0/8" />
                <span class="form-hint">排除的 IP/CIDR，逗号分隔</span>
              </el-form-item>
            </template>
          </el-form>
        </el-tab-pane>

        <!-- 端口识别 Tab -->
        <el-tab-pane name="portidentify">
          <template #label>
            <span>端口识别 <el-tag v-if="form.portidentifyEnable" type="success" size="small" style="margin-left:4px">开</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item label="启用">
              <el-switch v-model="form.portidentifyEnable" :disabled="!availableTools.nmap" />
              <span v-if="!availableTools.nmap" class="tool-tip" style="margin-left:10px">(Nmap 未安装)</span>
            </el-form-item>
            <template v-if="form.portidentifyEnable">
              <el-form-item label="超时(秒)">
                <el-input-number v-model="form.portidentifyTimeout" :min="5" :max="300" />
                <span class="form-hint">单个主机超时时间</span>
              </el-form-item>
              <el-form-item label="Nmap参数">
                <el-input v-model="form.portidentifyArgs" placeholder="-sV --version-intensity 5" />
              </el-form-item>
            </template>
            <el-alert v-if="!form.portidentifyEnable" type="info" :closable="false" show-icon>
              <template #title>端口识别使用 Nmap 对开放端口进行服务版本探测</template>
            </el-alert>
          </el-form>
        </el-tab-pane>

        <!-- 指纹识别 Tab -->
        <el-tab-pane name="fingerprint">
          <template #label>
            <span>指纹识别 <el-tag v-if="form.fingerprintEnable" type="success" size="small" style="margin-left:4px">开</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item label="启用">
              <el-switch v-model="form.fingerprintEnable" />
            </el-form-item>
            <template v-if="form.fingerprintEnable">
              <el-form-item label="探测工具">
                <el-radio-group v-model="form.fingerprintTool">
                  <el-radio label="httpx">Httpx (推荐)</el-radio>
                  <el-radio label="builtin">Wappalyzer (内置)</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item label="附加功能">
                <el-checkbox v-model="form.fingerprintIconHash">Icon Hash</el-checkbox>
                <el-checkbox v-model="form.fingerprintCustomEngine">自定义指纹</el-checkbox>
                <el-checkbox v-model="form.fingerprintScreenshot">网页截图</el-checkbox>
              </el-form-item>
              <el-row :gutter="20">
                <el-col :span="12">
                  <el-form-item label="超时(秒)">
                    <el-input-number v-model="form.fingerprintTimeout" :min="5" :max="120" style="width:100%" />
                    <span class="form-hint">并发数由Worker设置控制</span>
                  </el-form-item>
                </el-col>
              </el-row>
            </template>
          </el-form>
        </el-tab-pane>

        <!-- 漏洞扫描 Tab -->
        <el-tab-pane name="pocscan">
          <template #label>
            <span>漏洞扫描 <el-tag v-if="form.pocscanEnable" type="success" size="small" style="margin-left:4px">开</el-tag></span>
          </template>
          <el-form label-width="100px" class="tab-form">
            <el-form-item label="启用">
              <el-switch v-model="form.pocscanEnable" />
              <span class="form-hint">使用 Nuclei 引擎</span>
            </el-form-item>
            <template v-if="form.pocscanEnable">
              <el-form-item label="自动扫描">
                <el-checkbox v-model="form.pocscanAutoScan" :disabled="form.pocscanCustomOnly">自定义标签映射</el-checkbox>
                <el-checkbox v-model="form.pocscanAutomaticScan" :disabled="form.pocscanCustomOnly">Web指纹自动匹配</el-checkbox>
              </el-form-item>
              <el-form-item label="自定义POC">
                <el-checkbox v-model="form.pocscanCustomOnly">只使用自定义POC</el-checkbox>
              </el-form-item>
              <el-form-item label="严重级别">
                <el-checkbox-group v-model="form.pocscanSeverity">
                  <el-checkbox label="critical">Critical</el-checkbox>
                  <el-checkbox label="high">High</el-checkbox>
                  <el-checkbox label="medium">Medium</el-checkbox>
                  <el-checkbox label="low">Low</el-checkbox>
                  <el-checkbox label="info">Info</el-checkbox>
                </el-checkbox-group>
              </el-form-item>
              <el-form-item label="目标超时">
                <el-input-number v-model="form.pocscanTargetTimeout" :min="30" :max="600" />
                <span class="form-hint">秒</span>
              </el-form-item>
            </template>
          </el-form>
        </el-tab-pane>

        <!-- 高级设置 Tab -->
        <el-tab-pane label="高级设置" name="advanced">
          <el-form label-width="100px" class="tab-form">
            <el-form-item label="任务拆分">
              <el-input-number v-model="form.batchSize" :min="0" :max="1000" :step="10" />
              <span class="form-hint">每批目标数量，0=不拆分</span>
            </el-form-item>
          </el-form>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <div class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ isEdit ? '保存' : '创建任务' }}</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 任务日志对话框 -->
    <el-dialog v-model="logDialogVisible" title="任务日志" width="1000px" @close="closeLogDialog">
      <div class="log-progress" v-if="currentLogTask">
        <div class="progress-info">
          <span class="task-name">{{ currentLogTask.name }}</span>
          <el-tag :type="getStatusType(currentLogTask.status, currentLogTask)" size="small">{{ getStatusText(currentLogTask) }}</el-tag>
        </div>
        <el-progress :percentage="currentLogTask.progress" :status="currentLogTask.status === 'SUCCESS' ? 'success' : (currentLogTask.status === 'FAILURE' ? 'exception' : '')" :stroke-width="12" />
      </div>
      <div class="log-filter">
        <el-input v-model="logSearchKeyword" placeholder="搜索日志..." clearable size="small" style="width: 180px; margin-right: 10px">
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
        <el-select v-model="logWorkerFilter" placeholder="筛选Worker" clearable size="small" style="width: 150px">
          <el-option label="全部Worker" value="" />
          <el-option v-for="w in logWorkers" :key="w" :label="w" :value="w" />
        </el-select>
        <el-select v-model="logLevelFilter" placeholder="筛选级别" clearable size="small" style="width: 120px; margin-left: 10px">
          <el-option label="全部级别" value="" />
          <el-option label="DEBUG" value="DEBUG" />
          <el-option label="INFO" value="INFO" />
          <el-option label="WARN" value="WARN" />
          <el-option label="ERROR" value="ERROR" />
        </el-select>
        <el-switch v-model="logAutoRefresh" size="small" active-text="自动刷新" style="margin-left: 15px" @change="handleLogAutoRefreshChange" />
        <span class="log-stats">共 {{ filteredLogs.length }} 条日志</span>
      </div>
      <div class="log-container" ref="logContainerRef">
        <div v-if="filteredLogs.length === 0" class="log-empty">暂无日志</div>
        <div v-for="(log, index) in filteredLogs" :key="index" class="log-entry" :class="'log-' + log.level.toLowerCase()">
          <span class="log-time">{{ formatLogTime(log.timestamp) }}</span>
          <span class="log-level">[{{ log.level }}]</span>
          <span class="log-worker">{{ log.workerName }}</span>
          <span class="log-message">{{ log.displayMessage }}</span>
        </div>
      </div>
      <template #footer>
        <el-button @click="closeLogDialog">关闭</el-button>
        <el-button type="primary" @click="refreshLogs">刷新</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Search } from '@element-plus/icons-vue'
import { getTaskList, createTask, deleteTask, batchDeleteTask, retryTask, startTask, pauseTask, resumeTask, stopTask, updateTask, getTaskLogs, getWorkerList, saveScanConfig, getScanConfig } from '@/api/task'
import { useWorkspaceStore } from '@/stores/workspace'
import { validateTargets, formatValidationErrors } from '@/utils/target'
import request from '@/api/request'

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const detailVisible = ref(false)
const logDialogVisible = ref(false)
const tableData = ref([])
const organizations = ref([])
const workers = ref([])
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
  isCron: false,
  cronRule: '',
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
  portidentifyTimeout: 30,
  portidentifyArgs: '',
  fingerprintEnable: true,
  fingerprintTool: 'httpx',
  fingerprintIconHash: true,
  fingerprintCustomEngine: false,
  fingerprintScreenshot: false,
  fingerprintTimeout: 30,
  pocscanEnable: false,
  pocscanAutoScan: true,
  pocscanAutomaticScan: true,
  pocscanCustomOnly: false,
  pocscanSeverity: ['critical', 'high', 'medium'],
  pocscanTargetTimeout: 600
})

const targetValidator = (rule, value, callback) => {
  if (!value) { callback(new Error('请输入扫描目标')); return }
  const errors = validateTargets(value)
  errors.length > 0 ? callback(new Error(formatValidationErrors(errors))) : callback()
}

const rules = {
  name: [{ required: true, message: '请输入任务名称', trigger: 'blur' }],
  target: [{ required: true, message: '请输入扫描目标', trigger: 'blur' }, { validator: targetValidator, trigger: 'blur' }]
}

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
    const res = await getTaskList({ page: pagination.page, pageSize: pagination.pageSize, workspaceId: workspaceStore.currentWorkspaceId || '' })
    if (res.code === 0) { tableData.value = res.list || []; pagination.total = res.total }
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

// 获取状态显示文本（简化状态显示，不按扫描模块显示）
function getStatusText(row) {
  const statusMap = {
    CREATED: '待启动',
    PENDING: '等待执行',
    STARTED: '执行中',
    PAUSED: '已暂停',
    SUCCESS: '已完成',
    FAILURE: '执行失败',
    STOPPED: '已停止',
    REVOKED: '已取消'
  }
  
  // 如果有状态值，直接返回映射
  if (row?.status && statusMap[row.status]) {
    return statusMap[row.status]
  }
  
  // 如果状态为空，根据进度推断状态
  if (!row?.status) {
    if (row?.progress >= 100 || (row?.subTaskCount > 0 && row?.subTaskDone >= row?.subTaskCount)) {
      return '已完成'
    }
    if (row?.progress > 0 || row?.subTaskDone > 0) {
      return '执行中'
    }
    return '待启动'
  }
  
  return row?.status || '未知'
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

function resetForm() {
  Object.assign(form, {
    id: '', name: '', target: '', workspaceId: '', orgId: '', isCron: false, cronRule: '', workers: [],
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
    portidentifyTimeout: config.portidentify?.timeout || 30,
    portidentifyArgs: config.portidentify?.args || '',
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
    portidentify: { enable: form.portidentifyEnable, timeout: form.portidentifyTimeout, args: form.portidentifyArgs },
    fingerprint: { enable: form.fingerprintEnable, tool: form.fingerprintTool, iconHash: form.fingerprintIconHash, customEngine: form.fingerprintCustomEngine, screenshot: form.fingerprintScreenshot, targetTimeout: form.fingerprintTimeout },
    pocscan: { enable: form.pocscanEnable, useNuclei: true, autoScan: form.pocscanAutoScan, automaticScan: form.pocscanAutomaticScan, customPocOnly: form.pocscanCustomOnly, severity: form.pocscanSeverity.join(','), targetTimeout: form.pocscanTargetTimeout }
  }
}

// 扫描配置字段列表（用于监听变化自动保存）
const scanConfigFields = [
  'batchSize',
  'domainscanEnable', 'domainscanSubfinder', 'domainscanTimeout', 'domainscanMaxEnumTime', 'domainscanThreads', 'domainscanRateLimit', 'domainscanAll', 'domainscanRecursive', 'domainscanRemoveWildcard', 'domainscanResolveDNS', 'domainscanConcurrent',
  'portscanEnable', 'portscanTool', 'portscanRate', 'ports', 'portThreshold', 'scanType', 'portscanTimeout', 'skipHostDiscovery', 'excludeCDN', 'excludeHosts',
  'portidentifyEnable', 'portidentifyTimeout', 'portidentifyArgs',
  'fingerprintEnable', 'fingerprintTool', 'fingerprintIconHash', 'fingerprintCustomEngine', 'fingerprintScreenshot', 'fingerprintTimeout',
  'pocscanEnable', 'pocscanAutoScan', 'pocscanAutomaticScan', 'pocscanCustomOnly', 'pocscanSeverity', 'pocscanTargetTimeout'
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
    const data = { name: form.name, target: form.target, workspaceId: form.workspaceId, orgId: form.orgId, isCron: form.isCron, cronRule: form.cronRule, workers: form.workers, config: configStr }
    let res
    if (isEdit.value) {
      res = await updateTask({ id: form.id, ...data })
    } else {
      res = await createTask(data)
    }
    if (res.code === 0) {
      ElMessage.success(isEdit.value ? '任务更新成功' : '任务创建成功')
      dialogVisible.value = false
      loadData()
    } else { ElMessage.error(res.msg) }
  } finally { submitting.value = false }
}

async function handleDelete(row) {
  await ElMessageBox.confirm('确定删除该任务吗？', '提示', { type: 'warning' })
  const res = await deleteTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success('删除成功'), loadData()) : ElMessage.error(res.msg)
}

function handleSelectionChange(rows) { selectedRows.value = rows }

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  // 检查是否所有选中的任务都在同一个工作空间
  const workspaceIds = [...new Set(selectedRows.value.map(row => row.workspaceId))]
  if (workspaceIds.length > 1) {
    ElMessage.warning('批量删除只能删除同一工作空间的任务，请分别删除')
    return
  }
  await ElMessageBox.confirm(`确定删除选中的 ${selectedRows.value.length} 条任务吗？`, '提示', { type: 'warning' })
  const res = await batchDeleteTask({ ids: selectedRows.value.map(row => row.id), workspaceId: workspaceIds[0] })
  res.code === 0 ? (ElMessage.success('删除成功'), selectedRows.value = [], loadData()) : ElMessage.error(res.msg)
}

async function handleRetry(row) {
  await ElMessageBox.confirm('确定重新执行该任务吗？将创建一个新任务来执行。', '提示', { type: 'warning' })
  const res = await retryTask({ id: row.id })
  if (res.code === 0) {
    ElMessage.success(res.msg || '已创建新任务并开始执行')
    loadData()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleStart(row) {
  const res = await startTask({ id: row.id, workspaceId: row.workspaceId })
  if (res.code === 0) {
    ElMessage.success('任务已启动')
    loadData()
    // 延迟再刷新一次，等待 Worker 拉取任务后状态更新
    setTimeout(() => loadData(), 2000)
  } else {
    ElMessage.error(res.msg)
  }
}

async function handlePause(row) {
  await ElMessageBox.confirm('确定暂停该任务吗？', '提示', { type: 'warning' })
  const res = await pauseTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success('任务已暂停'), loadData()) : ElMessage.error(res.msg)
}

async function handleResume(row) {
  const res = await resumeTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success('任务已继续'), loadData()) : ElMessage.error(res.msg)
}

async function handleStop(row) {
  await ElMessageBox.confirm('确定停止该任务吗？', '提示', { type: 'warning' })
  const res = await stopTask({ id: row.id, workspaceId: row.workspaceId })
  res.code === 0 ? (ElMessage.success('任务已停止'), loadData()) : ElMessage.error(res.msg)
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
  .tool-tip { color: #f56c6c; font-size: 12px; }
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
  background-color: #1e1e1e;
  border-radius: 4px;
  padding: 10px;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  line-height: 1.6;
}

.log-empty { color: var(--el-text-color-secondary); text-align: center; padding: 20px; }
.log-entry { padding: 2px 0; white-space: pre-wrap; word-break: break-all; }
.log-time { color: #6a9955; margin-right: 8px; font-size: 11px; }
.log-level { font-weight: bold; margin-right: 6px; min-width: 45px; display: inline-block; font-size: 11px; }
.log-worker { color: #569cd6; margin-right: 6px; font-size: 11px; }
.log-message { color: #d4d4d4; }
.log-debug .log-level { color: #9e9e9e; }
.log-info .log-level { color: #4fc3f7; }
.log-warn .log-level, .log-warning .log-level { color: #ffb74d; }
.log-error .log-level { color: #ef5350; }

.config-section {
  margin-top: 15px;
  h4 { color: var(--el-text-color-primary); font-weight: 500; }
}

.config-detail {
  margin-top: 10px;
}
</style>
