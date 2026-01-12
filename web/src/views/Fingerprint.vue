<template>
  <div class="fingerprint-page">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- 内置指纹 -->
      <el-tab-pane label="内置指纹" name="builtin">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>Wappalyzer 内置指纹库</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ stats.builtin || 0 }} 个指纹
              </span>
              <div style="margin-left: auto; display: flex; gap: 8px;">
                <el-button type="warning" size="small" @click="showBatchValidateDialog">
                  <el-icon><Search /></el-icon>批量验证
                </el-button>
                <el-button type="success" size="small" @click="showBuiltinImportDialog">
                  <el-icon><Upload /></el-icon>导入指纹
                </el-button>
                <el-dropdown @command="handleSyncCommand">
                  <el-button type="primary" size="small" :loading="syncLoading">
                    <el-icon><Refresh /></el-icon>同步指纹<el-icon class="el-icon--right"><arrow-down /></el-icon>
                  </el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="sync">增量同步（从wappalyzergo库）</el-dropdown-item>
                      <el-dropdown-item command="force">强制重新同步</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </div>
            </div>
          </template>
          <p class="tip-text">
            Wappalyzer 内置指纹已同步到数据库，可以编辑启用/禁用状态。支持上传 Wappalyzer technologies.json 格式文件导入。
          </p>
          <!-- 筛选条件 -->
          <el-form :inline="true" class="filter-form">
            <el-form-item label="分类">
              <el-select v-model="builtinFilter.category" placeholder="全部分类" clearable style="width: 150px" @change="loadBuiltinFingerprints">
                <el-option v-for="cat in categories" :key="cat" :label="cat" :value="cat" />
              </el-select>
            </el-form-item>
            <el-form-item label="搜索">
              <el-input v-model="builtinFilter.keyword" placeholder="应用名称" clearable style="width: 180px" @keyup.enter="loadBuiltinFingerprints" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="loadBuiltinFingerprints">搜索</el-button>
            </el-form-item>
          </el-form>
          <!-- 统计信息 -->
          <div class="stats-bar">
            <el-tag type="success" size="small">启用: {{ stats.enabled || 0 }}</el-tag>
            <el-tag type="info" size="small">被动: {{ stats.passive || 0 }}</el-tag>
            <el-tag type="warning" size="small">主动: {{ stats.active || 0 }}</el-tag>
            <el-tag size="small">总计: {{ stats.total || 0 }}</el-tag>
          </div>
          <!-- 指纹列表 -->
          <el-table :data="builtinFingerprints" stripe v-loading="builtinLoading" max-height="500">
            <el-table-column prop="name" label="应用名称" width="180" show-overflow-tooltip />
            <el-table-column prop="category" label="分类" width="100" />
            <el-table-column prop="website" label="官网" min-width="200" show-overflow-tooltip>
              <template #default="{ row }">
                <a v-if="row.website" :href="row.website" target="_blank" style="color: #409eff">{{ row.website }}</a>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column label="匹配规则" min-width="250">
              <template #default="{ row }">
                <el-tag v-if="row.headers && Object.keys(row.headers).length" size="small" style="margin-right: 3px">Headers</el-tag>
                <el-tag v-if="row.cookies && Object.keys(row.cookies).length" size="small" style="margin-right: 3px">Cookies</el-tag>
                <el-tag v-if="row.html && row.html.length" size="small" style="margin-right: 3px">HTML</el-tag>
                <el-tag v-if="row.scripts && row.scripts.length" size="small" style="margin-right: 3px">Scripts</el-tag>
                <el-tag v-if="row.scriptSrc && row.scriptSrc.length" size="small" style="margin-right: 3px">ScriptSrc</el-tag>
                <el-tag v-if="row.js && Object.keys(row.js).length" size="small" style="margin-right: 3px">JS</el-tag>
                <el-tag v-if="row.meta && Object.keys(row.meta).length" size="small" style="margin-right: 3px">Meta</el-tag>
                <el-tag v-if="row.css && row.css.length" size="small" style="margin-right: 3px">CSS</el-tag>
                <el-tag v-if="row.url && row.url.length" size="small" style="margin-right: 3px">URL</el-tag>
                <el-tag v-if="row.dom" size="small" style="margin-right: 3px">DOM</el-tag>
                <span v-if="!hasAnyRule(row)" style="color: #909399">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" label="状态" width="80">
              <template #default="{ row }">
                <el-switch v-model="row.enabled" @change="handleToggleEnabled(row)" size="small" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120" fixed="right">
              <template #default="{ row }">
                <el-button type="success" link size="small" @click="showValidateDialog(row)">验证</el-button>
                <el-button type="primary" link size="small" @click="showFingerprintDetail(row)">查看</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="builtinPagination.page"
            v-model:page-size="builtinPagination.pageSize"
            :total="builtinPagination.total"
            :page-sizes="[50, 100, 200]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadBuiltinFingerprints"
            @current-change="loadBuiltinFingerprints"
          />
        </el-card>
      </el-tab-pane>

      <!-- 自定义指纹 -->
      <el-tab-pane label="自定义指纹" name="custom">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>自定义指纹规则</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ customPagination.total || 0 }} 条规则
              </span>
              <div style="margin-left: auto; display: flex; gap: 8px;">
                <el-dropdown @command="handleBatchEnabledCommand">
                  <el-button type="info" size="small" :loading="batchEnabledLoading">
                    <el-icon><Operation /></el-icon>批量操作<el-icon class="el-icon--right"><arrow-down /></el-icon>
                  </el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="enableAll">全部启用</el-dropdown-item>
                      <el-dropdown-item command="disableAll">全部禁用</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
                <el-button type="danger" size="small" @click="handleClearCustomFingerprints">
                  <el-icon><Delete /></el-icon>清空
                </el-button>
                <el-button type="warning" size="small" @click="handleExportFingerprints" :loading="exportLoading">
                  <el-icon><Download /></el-icon>导出指纹
                </el-button>
                <el-button type="success" size="small" @click="showImportDialog">
                  <el-icon><Upload /></el-icon>导入指纹
                </el-button>
                <el-button type="primary" size="small" @click="showFingerprintForm()">
                  <el-icon><Plus /></el-icon>新增指纹
                </el-button>
              </div>
            </div>
          </template>

          <!-- 筛选条件 -->
          <el-form :inline="true" class="filter-form">
            <el-form-item label="搜索">
              <el-input v-model="customFilter.keyword" placeholder="应用名称或ID" clearable style="width: 200px" @keyup.enter="loadCustomFingerprints" />
            </el-form-item>
            <el-form-item label="状态">
              <el-select v-model="customFilter.enabled" placeholder="全部状态" clearable style="width: 100px" @change="loadCustomFingerprints">
                <el-option label="启用" :value="true" />
                <el-option label="禁用" :value="false" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="loadCustomFingerprints">搜索</el-button>
              <el-button @click="resetCustomFilter">重置</el-button>
            </el-form-item>
          </el-form>
          <el-table :data="customFingerprints" stripe v-loading="customLoading" max-height="500">
            <el-table-column prop="id" label="ID" width="220">
              <template #default="{ row }">
                <el-tooltip :content="'点击复制'" placement="top">
                  <span class="fingerprint-id" @click="copyToClipboard(row.id)">{{ row.id }}</span>
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column prop="name" label="应用名称" width="180" />
            <el-table-column prop="type" label="类型" width="100">
              <template #default="{ row }">
                <el-tag v-if="row.type === 'active'" type="warning" size="small">主动</el-tag>
                <el-tag v-else type="info" size="small">被动</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="匹配规则" min-width="300">
              <template #default="{ row }">
                <template v-if="row.type === 'active' && row.activePaths && row.activePaths.length">
                  <el-tag size="small" type="success">{{ row.activePaths.length }}个路径</el-tag>
                  <span style="margin-left: 5px; color: #909399; font-size: 12px">{{ row.activePaths[0] }}{{ row.activePaths.length > 1 ? '...' : '' }}</span>
                </template>
                <template v-else-if="row.rule">
                  <el-tag size="small" type="warning">自定义规则</el-tag>
                  <span style="margin-left: 5px; color: #909399; font-size: 12px">{{ truncateRule(row.rule) }}</span>
                </template>
                <template v-else>
                  <el-tag v-if="row.headers && Object.keys(row.headers).length" size="small" style="margin-right: 3px">Headers</el-tag>
                  <el-tag v-if="row.cookies && Object.keys(row.cookies).length" size="small" style="margin-right: 3px">Cookies</el-tag>
                  <el-tag v-if="row.html && row.html.length" size="small" style="margin-right: 3px">HTML</el-tag>
                  <el-tag v-if="row.scripts && row.scripts.length" size="small" style="margin-right: 3px">Scripts</el-tag>
                  <el-tag v-if="row.meta && Object.keys(row.meta).length" size="small" style="margin-right: 3px">Meta</el-tag>
                </template>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" label="状态" width="80">
              <template #default="{ row }">
                <el-switch v-model="row.enabled" @change="handleToggleEnabled(row)" size="small" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="250">
              <template #default="{ row }">
                <el-button type="success" link size="small" @click="showValidateDialog(row)">验证</el-button>
                <el-button type="warning" link size="small" @click="showMatchAssetsDialog(row)">匹配资产</el-button>
                <el-button type="primary" link size="small" @click="showFingerprintForm(row)">编辑</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteFingerprint(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="customPagination.page"
            v-model:page-size="customPagination.pageSize"
            :total="customPagination.total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadCustomFingerprints"
            @current-change="loadCustomFingerprints"
          />
        </el-card>
      </el-tab-pane>

      <!-- 主动扫描指纹 -->
      <el-tab-pane label="主动扫描指纹" name="activeFingerprint">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>主动扫描指纹规则</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ activeFingerprintStats.total || 0 }} 条规则
              </span>
              <div style="margin-left: auto; display: flex; gap: 8px;">
                <el-button type="danger" size="small" @click="handleClearActiveFingerprints">
                  <el-icon><Delete /></el-icon>清空
                </el-button>
                <el-button type="warning" size="small" @click="handleExportActiveFingerprints" :loading="activeExportLoading">
                  <el-icon><Download /></el-icon>导出YAML
                </el-button>
                <el-button type="success" size="small" @click="showActiveImportDialog">
                  <el-icon><Upload /></el-icon>导入YAML
                </el-button>
                <el-button type="primary" size="small" @click="showActiveFingerprintForm()">
                  <el-icon><Plus /></el-icon>新增规则
                </el-button>
              </div>
            </div>
          </template>
          <p class="tip-text">
            主动扫描指纹通过访问特定路径来识别应用。
            <br/>
            <span style="color: #e6a23c">关联规则：主动指纹会自动关联同名的被动指纹，扫描时先通过主动路径探测，再使用被动指纹规则匹配响应内容。</span>
          </p>
          <!-- 筛选条件 -->
          <el-form :inline="true" class="filter-form">
            <el-form-item label="搜索">
              <el-input v-model="activeFingerprintFilter.keyword" placeholder="应用名称" clearable style="width: 200px" @keyup.enter="loadActiveFingerprints" />
            </el-form-item>
            <el-form-item label="状态">
              <el-select v-model="activeFingerprintFilter.enabled" placeholder="全部状态" clearable style="width: 100px" @change="loadActiveFingerprints">
                <el-option label="启用" :value="true" />
                <el-option label="禁用" :value="false" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="loadActiveFingerprints">搜索</el-button>
              <el-button @click="resetActiveFingerprintFilter">重置</el-button>
            </el-form-item>
          </el-form>
          <!-- 统计信息 -->
          <div class="stats-bar">
            <el-tag type="success" size="small">启用: {{ activeFingerprintStats.enabled || 0 }}</el-tag>
            <el-tag size="small">总计: {{ activeFingerprintStats.total || 0 }}</el-tag>
          </div>
          <!-- 主动指纹列表 -->
          <el-table :data="activeFingerprints" stripe v-loading="activeFingerprintLoading" max-height="500">
            <el-table-column prop="name" label="应用名称" width="200" />
            <el-table-column label="探测路径" min-width="300">
              <template #default="{ row }">
                <div class="paths-preview">
                  <el-tag v-for="(path, idx) in (row.paths || []).slice(0, 3)" :key="idx" size="small" style="margin-right: 5px; margin-bottom: 3px">
                    {{ path }}
                  </el-tag>
                  <el-tag v-if="row.paths && row.paths.length > 3" size="small" type="info">
                    +{{ row.paths.length - 3 }}
                  </el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="关联被动指纹" width="150">
              <template #default="{ row }">
                <el-tag v-if="row.relatedCount > 0" type="success" size="small">
                  {{ row.relatedCount }} 个关联
                </el-tag>
                <el-tag v-else type="info" size="small">无关联</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" label="状态" width="80">
              <template #default="{ row }">
                <el-switch v-model="row.enabled" @change="handleToggleActiveFingerprintEnabled(row)" size="small" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="200">
              <template #default="{ row }">
                <el-button type="success" link size="small" @click="showActiveValidateDialog(row)">验证</el-button>
                <el-button type="info" link size="small" @click="showActiveFingerprintDetail(row)">详情</el-button>
                <el-button type="primary" link size="small" @click="showActiveFingerprintForm(row)">编辑</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteActiveFingerprint(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="activeFingerprintPagination.page"
            v-model:page-size="activeFingerprintPagination.pageSize"
            :total="activeFingerprintPagination.total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadActiveFingerprints"
            @current-change="loadActiveFingerprints"
          />
        </el-card>
      </el-tab-pane>

      <!-- HTTP映射 -->
      <el-tab-pane label="HTTP映射" name="httpServiceMapping" >
        <el-tabs v-model="httpServiceSubTab" type="card" @tab-change="handleHttpServiceSubTabChange">
          <!-- 服务映射 -->
          <el-tab-pane label="服务映射" name="serviceMapping">
            <el-card>
              <template #header>
                <div class="card-header">
                  <span>服务映射配置</span>
                  <el-button type="primary" size="small" @click="showHttpServiceMappingForm()">
                    <el-icon><Plus /></el-icon>新增映射
                  </el-button>
                </div>
              </template>
              <p class="tip-text">
                配置端口扫描识别的 Service 名称与 HTTP/非HTTP 服务的映射关系。指纹识别时会根据此配置判断是否对该端口进行 HTTP 探测。
                <br/>
                <span style="color: #e6a23c">注意：每次扫描前会实时从数据库获取最新配置，修改后立即生效。</span>
              </p>
              <!-- 筛选条件 -->
              <el-form :inline="true" class="filter-form">
                <el-form-item label="类型">
                  <el-select v-model="httpServiceFilter.isHttp" placeholder="全部类型" clearable style="width: 150px" @change="loadHttpServiceMappings">
                    <el-option label="HTTP服务" :value="true" />
                    <el-option label="非HTTP服务" :value="false" />
                  </el-select>
                </el-form-item>
                <el-form-item label="搜索">
                  <el-input v-model="httpServiceFilter.keyword" placeholder="服务名称" clearable style="width: 180px" @keyup.enter="loadHttpServiceMappings" />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="loadHttpServiceMappings">搜索</el-button>
                </el-form-item>
              </el-form>
              <!-- 统计信息 -->
              <div class="stats-bar">
                <el-tag type="success" size="small">HTTP服务: {{ httpServiceStats.httpCount || 0 }}</el-tag>
                <el-tag type="info" size="small">非HTTP服务: {{ httpServiceStats.nonHttpCount || 0 }}</el-tag>
                <el-tag size="small">总计: {{ httpServiceStats.total || 0 }}</el-tag>
              </div>
              <!-- 映射列表 -->
              <el-table :data="httpServiceMappings" stripe v-loading="httpServiceLoading" max-height="500">
                <el-table-column prop="serviceName" label="服务名称" width="180" />
                <el-table-column prop="isHttp" label="服务类型" width="120">
                  <template #default="{ row }">
                    <el-tag :type="row.isHttp ? 'success' : 'info'" size="small">
                      {{ row.isHttp ? 'HTTP服务' : '非HTTP服务' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="description" label="描述" min-width="200" />
                <el-table-column prop="enabled" label="状态" width="80">
                  <template #default="{ row }">
                    <el-switch v-model="row.enabled" @change="handleToggleHttpServiceEnabled(row)" size="small" />
                  </template>
                </el-table-column>
                <el-table-column label="操作" width="120">
                  <template #default="{ row }">
                    <el-button type="primary" link size="small" @click="showHttpServiceMappingForm(row)">编辑</el-button>
                    <el-button type="danger" link size="small" @click="handleDeleteHttpServiceMapping(row)">删除</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <el-pagination
                v-model:current-page="httpServicePagination.page"
                v-model:page-size="httpServicePagination.pageSize"
                :total="httpServicePagination.total"
                :page-sizes="[10,20, 50, 100]"
                layout="total, sizes, prev, pager, next"
                class="pagination"
                @size-change="loadHttpServiceMappings"
                @current-change="loadHttpServiceMappings"
              />
            </el-card>
          </el-tab-pane>

          <!-- Web端口设置 -->
          <el-tab-pane label="端口映射" name="webPorts">
            <el-card>
              <template #header>
                <div class="card-header">
                  <span>端口配置</span>
                  <el-button type="primary" size="small" @click="handleSaveWebPortsConfig" :loading="webPortsSaving">
                    <el-icon><Check /></el-icon>保存配置
                  </el-button>
                </div>
              </template>
              <p class="tip-text">
                配置 Web 端口和非 Web 端口列表。当端口扫描无法识别服务名称时，会根据端口号判断是否进行指纹扫描。
                <br/>
                <span style="color: #e6a23c">注意：每次扫描前会实时从数据库获取最新配置，修改后立即生效。</span>
              </p>
              
              <el-form :model="webPortsConfig" label-width="120px" v-loading="webPortsLoading" class="web-ports-form">
                <el-divider content-position="left">Web 端口（会进行指纹扫描）</el-divider>
                
                <el-form-item label="HTTP端口">
                  <div class="ports-input-wrapper">
                    <el-input
                      v-model="webPortsConfig.httpPortsText"
                      type="textarea"
                      :rows="4"
                      placeholder="输入HTTP端口，多个端口用逗号、空格或换行分隔，如: 80, 8080, 8000"
                    />
                    <div class="ports-count">
                      共 {{ parsePortsCount(webPortsConfig.httpPortsText) }} 个端口
                    </div>
                  </div>
                  <div class="form-tip">常见HTTP端口：80, 8080, 8000, 8888, 3000, 5000, 9000 等</div>
                </el-form-item>
                
                <el-form-item label="HTTPS端口">
                  <div class="ports-input-wrapper">
                    <el-input
                      v-model="webPortsConfig.httpsPortsText"
                      type="textarea"
                      :rows="3"
                      placeholder="输入HTTPS端口，多个端口用逗号、空格或换行分隔，如: 443, 8443"
                    />
                    <div class="ports-count">
                      共 {{ parsePortsCount(webPortsConfig.httpsPortsText) }} 个端口
                    </div>
                  </div>
                  <div class="form-tip">常见HTTPS端口：443, 8443, 9443, 4443 等</div>
                </el-form-item>
                
                <el-divider content-position="left">非 Web 端口（明确排除，不进行指纹扫描）</el-divider>
                
                <el-form-item label="非Web端口">
                  <div class="ports-input-wrapper">
                    <el-input
                      v-model="webPortsConfig.nonHttpPortsText"
                      type="textarea"
                      :rows="5"
                      placeholder="输入非Web端口，多个端口用逗号、空格或换行分隔，如: 22, 135, 445, 3306"
                    />
                    <div class="ports-count">
                      共 {{ parsePortsCount(webPortsConfig.nonHttpPortsText) }} 个端口
                    </div>
                  </div>
                  <div class="form-tip">
                    常见非Web端口：22(SSH), 23(Telnet), 25(SMTP), 110(POP3), 143(IMAP), 135/139/445(Windows), 3306(MySQL), 6379(Redis) 等
                  </div>
                </el-form-item>
                
                <el-form-item label="描述">
                  <el-input v-model="webPortsConfig.description" placeholder="可选描述信息" />
                </el-form-item>
              </el-form>
              
              <el-divider content-position="left">快捷操作</el-divider>
              <div class="quick-actions">
                <el-button size="small" @click="resetWebPortsToDefault">恢复默认配置</el-button>
                <el-button size="small" @click="addCommonHttpPorts">添加常用HTTP端口</el-button>
                <el-button size="small" @click="addCommonHttpsPorts">添加常用HTTPS端口</el-button>
                <el-button size="small" @click="addCommonNonHttpPorts">添加常用非Web端口</el-button>
              </div>
            </el-card>
          </el-tab-pane>
        </el-tabs>
      </el-tab-pane>
    </el-tabs>

    <!-- 指纹详情对话框 -->
    <el-dialog v-model="detailDialogVisible" :title="currentFingerprint.name || '指纹详情'" width="900px" top="5vh">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item label="应用名称">{{ currentFingerprint.name }}</el-descriptions-item>
        <el-descriptions-item label="分类">{{ currentFingerprint.category }}</el-descriptions-item>
        <el-descriptions-item label="官网" :span="2">
          <a v-if="currentFingerprint.website" :href="currentFingerprint.website" target="_blank" style="color: #409eff">{{ currentFingerprint.website }}</a>
          <span v-else>-</span>
        </el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">{{ currentFingerprint.description || '-' }}</el-descriptions-item>
        <el-descriptions-item label="CPE" :span="2" v-if="currentFingerprint.cpe">{{ currentFingerprint.cpe }}</el-descriptions-item>
      </el-descriptions>
      
      <el-divider content-position="left">匹配规则</el-divider>
      
      <div class="match-logic-tip">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            <span>匹配逻辑：<strong>不同类型</strong>之间为 <el-tag size="small" type="success">AND</el-tag> 关系，<strong>同类型内多个规则</strong>为 <el-tag size="small" type="warning">OR</el-tag> 关系</span>
          </template>
          <template #default>
            <div style="font-size: 12px; color: #909399; margin-top: 5px">
              例如：同时配置了 Headers 和 HTML 规则时，两者都需要匹配；但 HTML 内有多个正则时，只需匹配其中一个
            </div>
          </template>
        </el-alert>
      </div>
      
      <div v-if="!hasAnyRule(currentFingerprint)" class="no-rules">
        <el-empty description="该指纹没有配置匹配规则" :image-size="60" />
      </div>
      
      <div class="rules-container" v-else>
        <!-- ARL格式规则 -->
        <div class="rule-section" v-if="currentFingerprint.rule">
          <div class="rule-title"><el-tag size="small" type="warning">自定义规则</el-tag> 简化规则语法</div>
          <pre class="rule-content">{{ currentFingerprint.rule }}</pre>
          <div class="rule-help">
            <p>语法说明：</p>
            <ul>
              <li><code>body="xxx"</code> - 匹配响应体包含xxx</li>
              <li><code>title="xxx"</code> - 匹配页面标题包含xxx</li>
              <li><code>header="xxx"</code> - 匹配响应头包含xxx</li>
              <li><code>server="xxx"</code> - 匹配Server头包含xxx</li>
              <li><code>&&</code> - AND逻辑，所有条件都需满足</li>
              <li><code>||</code> - OR逻辑，满足任一条件即可</li>
            </ul>
          </div>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.headers && Object.keys(currentFingerprint.headers).length">
          <div class="rule-title"><el-tag size="small" type="primary">Headers</el-tag> HTTP响应头匹配</div>
          <pre class="rule-content">{{ JSON.stringify(currentFingerprint.headers, null, 2) }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.cookies && Object.keys(currentFingerprint.cookies).length">
          <div class="rule-title"><el-tag size="small" type="primary">Cookies</el-tag> Cookie匹配</div>
          <pre class="rule-content">{{ JSON.stringify(currentFingerprint.cookies, null, 2) }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.html && currentFingerprint.html.length">
          <div class="rule-title"><el-tag size="small" type="success">HTML</el-tag> HTML内容匹配（正则）</div>
          <pre class="rule-content">{{ currentFingerprint.html.join('\n') }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.scripts && currentFingerprint.scripts.length">
          <div class="rule-title"><el-tag size="small" type="warning">Scripts</el-tag> Script标签匹配（正则）</div>
          <pre class="rule-content">{{ currentFingerprint.scripts.join('\n') }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.scriptSrc && currentFingerprint.scriptSrc.length">
          <div class="rule-title"><el-tag size="small" type="warning">ScriptSrc</el-tag> Script src属性匹配</div>
          <pre class="rule-content">{{ currentFingerprint.scriptSrc.join('\n') }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.js && Object.keys(currentFingerprint.js).length">
          <div class="rule-title"><el-tag size="small" type="danger">JS</el-tag> JavaScript变量匹配</div>
          <pre class="rule-content">{{ JSON.stringify(currentFingerprint.js, null, 2) }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.meta && Object.keys(currentFingerprint.meta).length">
          <div class="rule-title"><el-tag size="small" type="info">Meta</el-tag> Meta标签匹配</div>
          <pre class="rule-content">{{ JSON.stringify(currentFingerprint.meta, null, 2) }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.css && currentFingerprint.css.length">
          <div class="rule-title"><el-tag size="small">CSS</el-tag> CSS选择器匹配</div>
          <pre class="rule-content">{{ currentFingerprint.css.join('\n') }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.url && currentFingerprint.url.length">
          <div class="rule-title"><el-tag size="small">URL</el-tag> URL路径匹配</div>
          <pre class="rule-content">{{ currentFingerprint.url.join('\n') }}</pre>
        </div>
        
        <div class="rule-section" v-if="currentFingerprint.dom">
          <div class="rule-title"><el-tag size="small" type="danger">DOM</el-tag> DOM选择器匹配</div>
          <pre class="rule-content">{{ formatDom(currentFingerprint.dom) }}</pre>
        </div>
      </div>
      
      <el-divider content-position="left" v-if="currentFingerprint.implies && currentFingerprint.implies.length">关联信息</el-divider>
      <div class="rule-section" v-if="currentFingerprint.implies && currentFingerprint.implies.length">
        <div class="rule-title">隐含技术（识别到此应用时自动关联）</div>
        <div style="margin-top: 8px">
          <el-tag v-for="imp in currentFingerprint.implies" :key="imp" size="small" style="margin-right: 5px; margin-bottom: 5px">{{ imp }}</el-tag>
        </div>
      </div>
      
      <template #footer>
        <el-button @click="detailDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 自定义指纹编辑对话框 -->
    <el-dialog v-model="formDialogVisible" :title="fingerprintForm.id ? '编辑指纹' : '新增指纹'" width="800px">
      <el-form ref="fingerprintFormRef" :model="fingerprintForm" :rules="fingerprintRules" label-width="100px">
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="应用名称" prop="name">
              <el-input v-model="fingerprintForm.name" placeholder="如: WordPress" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="分类" prop="category">
              <el-select v-model="fingerprintForm.category" placeholder="选择分类" filterable allow-create style="width: 100%">
                <el-option v-for="cat in categories" :key="cat" :label="cat" :value="cat" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="指纹类型">
              <el-radio-group v-model="fingerprintForm.type">
                <el-radio value="passive">被动指纹</el-radio>
                <el-radio value="active">主动指纹</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="启用">
              <el-switch v-model="fingerprintForm.enabled" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="官网">
              <el-input v-model="fingerprintForm.website" placeholder="https://example.com" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="描述">
          <el-input v-model="fingerprintForm.description" type="textarea" :rows="2" placeholder="指纹描述" />
        </el-form-item>
        
        <!-- 主动指纹路径配置 -->
        <el-form-item v-if="fingerprintForm.type === 'active'" label="探测路径" prop="activePaths">
          <el-input
            v-model="fingerprintForm.activePathsText"
            type="textarea"
            :rows="3"
            placeholder="每行一个路径，如：&#10;/admin/login.php&#10;/wp-admin/&#10;/manager/html"
            style="font-family: 'Consolas', 'Monaco', monospace"
          />
          <div class="form-tip">
            主动指纹会访问这些路径来识别应用，每行一个路径。路径应以 / 开头。
          </div>
        </el-form-item>
        
        <el-divider content-position="left">匹配规则</el-divider>
        
        <!-- ARL简化规则 -->
        <el-form-item label="自定义规则" prop="rule">
          <el-input
            v-model="fingerprintForm.rule"
            type="textarea"
            :rows="4"
            placeholder='简化规则语法，如: body="xxx" && title="xxx" || header="xxx" || icon_hash="123456"'
            style="font-family: 'Consolas', 'Monaco', monospace"
          />
          <div class="form-tip">
            <p style="margin: 5px 0 3px 0; font-weight: bold;">支持的匹配类型：</p>
            <ul style="margin: 0; padding-left: 20px; line-height: 1.8;">
              <li><code>body="关键字"</code> - 匹配响应体内容</li>
              <li><code>title="关键字"</code> - 匹配页面标题</li>
              <li><code>header="关键字"</code> - 匹配响应头</li>
              <li><code>server="关键字"</code> - 匹配Server头</li>
              <li><code>cookie="关键字"</code> - 匹配Cookie（如 rememberMe 识别Shiro）</li>
              <li><code>icon_hash="数字"</code> - 匹配favicon的MMH3哈希值</li>
            </ul>
            <p style="margin: 8px 0 3px 0; font-weight: bold;">逻辑运算符：</p>
            <ul style="margin: 0; padding-left: 20px; line-height: 1.8;">
              <li><code>&&</code> - AND逻辑，所有条件都需满足</li>
              <li><code>||</code> - OR逻辑，满足任一条件即可</li>
            </ul>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveFingerprint">保存</el-button>
      </template>
    </el-dialog>

    <!-- 导入指纹对话框（自定义指纹） -->
    <el-dialog v-model="importDialogVisible" title="导入自定义指纹" width="700px">
      <el-alert type="info" :closable="false" style="margin-bottom: 15px">
        <template #title>支持多种指纹格式，可批量上传多个文件</template>
        <template #default>
          <div style="font-size: 12px">
            <p style="margin: 5px 0;"><strong>格式1 - ARL finger.json：</strong><code>{"fingerprint": [{"cms": "xxx", "keyword": ["xxx"], "location": "body"}]}</code></p>
            <p style="margin: 5px 0;"><strong>格式2 - ARL finger.yml：</strong><code>- name: Weblogic</code> / <code>rule: body="xxx" && title="xxx"</code></p>
            <p style="margin: 5px 0;"><strong>格式3 - 简化YAML：</strong><code>AppName:</code> + <code>- 'body="xxx" || title="xxx"'</code></p>
          </div>
        </template>
      </el-alert>
      
      <el-upload
        ref="uploadRef"
        drag
        :auto-upload="false"
        :limit="500"
        accept=".json,.yml,.yaml"
        :on-change="handleFileChange"
        multiple
        :show-file-list="false"
      >
        <el-icon class="el-icon--upload"><Upload /></el-icon>
        <div class="el-upload__text">拖拽文件到此处，或 <em>点击上传</em></div>
        <template #tip>
          <div class="el-upload__tip">支持 .json / .yml / .yaml 格式文件，可批量选择多个文件</div>
        </template>
      </el-upload>
      
      <div v-if="importFiles.length > 0" class="file-preview">
        <div class="preview-header">
          <span>已选择 {{ importFiles.length }} 个文件</span>
          <el-button type="danger" link size="small" @click="clearImportFile">清除全部</el-button>
        </div>
        <el-table :data="importFiles" max-height="200" size="small">
          <el-table-column prop="name" label="文件名" show-overflow-tooltip />
          <el-table-column prop="size" label="大小" width="100">
            <template #default="{ row }">{{ formatFileSize(row.size) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="80">
            <template #default="{ $index }">
              <el-button type="danger" link size="small" @click="removeImportFile($index)">移除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      
      <template #footer>
        <el-button @click="importDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleImportFingerprints" :loading="importLoading" :disabled="importFiles.length === 0">
          导入 ({{ importFiles.length }})
        </el-button>
      </template>
    </el-dialog>

    <!-- 导入内置指纹对话框（Wappalyzer格式） -->
    <el-dialog v-model="builtinImportDialogVisible" title="导入内置指纹" width="600px">
      <el-alert type="info" :closable="false" style="margin-bottom: 15px">
        <template #title>支持 Wappalyzer fingerprints_data.json 格式</template>
        <template #default>
          <div style="font-size: 12px">
            从 <a href="https://github.com/projectdiscovery/wappalyzergo/blob/main/fingerprints_data.json" target="_blank" style="color: #409eff">wappalyzergo GitHub</a> 下载最新的 fingerprints_data.json 文件<br/>
            格式：<code>{"apps": {"应用名": {"cats": [1], "headers": {...}, "html": [...]}}}</code>
          </div>
        </template>
      </el-alert>
      
      <el-upload
        ref="builtinUploadRef"
        drag
        :auto-upload="false"
        :limit="1"
        accept=".json"
        :on-change="handleBuiltinFileChange"
        :on-exceed="handleExceed"
      >
        <el-icon class="el-icon--upload"><Upload /></el-icon>
        <div class="el-upload__text">拖拽文件到此处，或 <em>点击上传</em></div>
        <template #tip>
          <div class="el-upload__tip">支持 fingerprints_data.json 格式文件</div>
        </template>
      </el-upload>
      
      <div v-if="builtinImportContent" class="file-preview">
        <div class="preview-header">
          <span>文件预览 ({{ builtinImportFileName }})</span>
          <el-button type="danger" link size="small" @click="clearBuiltinImportFile">清除</el-button>
        </div>
        <pre class="preview-content">{{ builtinImportContent.substring(0, 500) }}{{ builtinImportContent.length > 500 ? '\n...(已截断)' : '' }}</pre>
      </div>
      
      <template #footer>
        <el-button @click="builtinImportDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleImportBuiltinFingerprints" :loading="builtinImportLoading" :disabled="!builtinImportContent">导入</el-button>
      </template>
    </el-dialog>

    <!-- 指纹验证对话框 -->
    <el-dialog v-model="validateDialogVisible" title="验证指纹" width="600px">
      <el-form label-width="80px">
        <el-form-item label="指纹名称">
          <el-input :value="validateFingerprint.name" disabled />
        </el-form-item>
        <el-form-item label="目标URL">
          <el-input v-model="validateUrl" placeholder="请输入目标URL，如 https://example.com" />
        </el-form-item>
      </el-form>
      <div v-if="validateResult" class="validate-result" :class="{ 'matched': validateResult.matched }">
        <div class="result-header">
          <el-tag :type="validateResult.matched ? 'success' : 'info'" size="large">
            {{ validateResult.matched ? '✓ 匹配成功' : '✗ 未匹配' }}
          </el-tag>
        </div>
        <pre class="result-details" v-html="formatValidateDetails(validateResult.details)"></pre>
      </div>
      <template #footer>
        <el-button @click="validateDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleValidateFingerprint" :loading="validateLoading" :disabled="!validateUrl">验证</el-button>
      </template>
    </el-dialog>

    <!-- 批量验证对话框 -->
    <el-dialog v-model="batchValidateDialogVisible" title="批量验证指纹" width="800px">
      <el-form label-width="80px">
        <el-form-item label="目标URL">
          <el-input v-model="batchValidateUrl" placeholder="请输入目标URL，如 https://example.com" />
        </el-form-item>
        <el-form-item label="指纹范围">
          <el-radio-group v-model="batchValidateScope">
            <el-radio value="all">全部指纹</el-radio>
            <el-radio value="builtin">仅内置指纹</el-radio>
            <el-radio value="custom">仅自定义指纹</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <div v-if="batchValidateResult" class="batch-validate-result">
        <div class="result-header">
          <el-tag type="success" size="large">
            共匹配到 {{ batchValidateResult.matchedCount }} 个指纹
          </el-tag>
          <span style="margin-left: 10px; color: #909399; font-size: 13px">
            耗时: {{ batchValidateResult.duration }}
          </span>
        </div>
        <div v-if="batchValidateResult.matched && batchValidateResult.matched.length > 0" class="matched-list">
          <el-table :data="batchValidateResult.matched" stripe max-height="400">
            <el-table-column prop="name" label="指纹名称" width="200" />
            <el-table-column prop="category" label="分类" width="120" />
            <el-table-column prop="source" label="来源" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="row.isBuiltin ? 'primary' : 'warning'">
                  {{ row.isBuiltin ? '内置' : '自定义' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="matchedConditions" label="命中条件" min-width="300">
              <template #default="{ row }">
                <span style="color: #f56c6c; font-size: 12px">{{ row.matchedConditions || '-' }}</span>
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div v-else class="no-match">
          <el-empty description="未匹配到任何指纹" :image-size="60" />
        </div>
      </div>
      <template #footer>
        <el-button @click="batchValidateDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleBatchValidate" :loading="batchValidateLoading" :disabled="!batchValidateUrl">开始验证</el-button>
      </template>
    </el-dialog>

    <!-- HTTP服务映射编辑对话框 -->
    <el-dialog v-model="httpServiceMappingDialogVisible" :title="httpServiceMappingForm.id ? '编辑映射' : '新增映射'" width="500px">
      <el-form ref="httpServiceMappingFormRef" :model="httpServiceMappingForm" :rules="httpServiceMappingRules" label-width="100px">
        <el-form-item label="服务名称" prop="serviceName">
          <el-input v-model="httpServiceMappingForm.serviceName" placeholder="端口扫描识别的服务名称，如: http, ssh, mysql" />
          <div style="color: #909399; font-size: 12px; margin-top: 4px;">
            服务名称应与 nmap/masscan 识别的 Service 字段一致（小写）
          </div>
        </el-form-item>
        <el-form-item label="服务类型" prop="isHttp">
          <el-radio-group v-model="httpServiceMappingForm.isHttp">
            <el-radio :value="true">HTTP服务（会进行HTTP指纹识别）</el-radio>
            <el-radio :value="false">非HTTP服务（跳过HTTP探测）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="httpServiceMappingForm.description" placeholder="可选描述" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="httpServiceMappingForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="httpServiceMappingDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveHttpServiceMapping">保存</el-button>
      </template>
    </el-dialog>

    <!-- 匹配现有资产对话框 -->
    <el-dialog v-model="matchAssetsDialogVisible" title="匹配现有资产" width="900px">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item label="指纹名称">{{ matchAssetsFingerprint.name }}</el-descriptions-item>
        <el-descriptions-item label="匹配规则">
          <el-tag v-if="matchAssetsFingerprint.rule" size="small" type="warning">自定义规则</el-tag>
          <span v-else>-</span>
        </el-descriptions-item>
      </el-descriptions>
      
      <div v-if="!matchAssetsResult" class="match-assets-tip">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            点击"开始匹配"将使用该指纹规则扫描数据库中所有已存储HTTP响应数据的资产
          </template>
          <template #default>
            <div style="font-size: 12px; color: #909399; margin-top: 5px">
              匹配依据：资产的 Title、Header、Body、IconHash 等字段
            </div>
          </template>
        </el-alert>
        <div style="margin-top: 15px">
          <el-checkbox v-model="matchAssetsUpdateAsset">
            匹配成功后自动更新资产的指纹信息
          </el-checkbox>
          <div style="font-size: 12px; color: #909399; margin-top: 5px; margin-left: 24px">
            勾选后，匹配到的资产将自动添加该指纹名称到其应用指纹列表中
          </div>
        </div>
      </div>
      
      <div v-if="matchAssetsResult" class="match-assets-result">
        <div class="result-header">
          <el-tag type="success" size="large">
            共匹配到 {{ matchAssetsResult.matchedCount }} 个资产
          </el-tag>
          <el-tag v-if="matchAssetsResult.updatedCount > 0" type="warning" size="large" style="margin-left: 10px">
            已更新 {{ matchAssetsResult.updatedCount }} 个资产
          </el-tag>
          <span style="margin-left: 10px; color: #909399; font-size: 13px">
            扫描 {{ matchAssetsResult.totalScanned }} 个资产，耗时: {{ matchAssetsResult.duration }}
          </span>
        </div>
        <div v-if="matchAssetsResult.matchedList && matchAssetsResult.matchedList.length > 0" class="matched-list">
          <el-table :data="matchAssetsResult.matchedList" stripe max-height="400">
            <el-table-column prop="authority" label="资产地址" min-width="250" show-overflow-tooltip />
            <el-table-column prop="host" label="主机" width="150" />
            <el-table-column prop="port" label="端口" width="80" />
            <el-table-column prop="title" label="标题" min-width="200" show-overflow-tooltip />
            <el-table-column prop="service" label="服务" width="100" />
          </el-table>
        </div>
        <div v-else class="no-match">
          <el-empty description="未匹配到任何资产" :image-size="60" />
        </div>
      </div>
      
      <template #footer>
        <el-button @click="matchAssetsDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleMatchAssets" :loading="matchAssetsLoading">
          {{ matchAssetsResult ? '重新匹配' : '开始匹配' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 主动指纹详情对话框 -->
    <el-dialog v-model="activeFingerprintDetailDialogVisible" :title="currentActiveFingerprint.name || '主动指纹详情'" width="800px">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item label="应用名称">{{ currentActiveFingerprint.name }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="currentActiveFingerprint.enabled ? 'success' : 'info'" size="small">
            {{ currentActiveFingerprint.enabled ? '启用' : '禁用' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">{{ currentActiveFingerprint.description || '-' }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ currentActiveFingerprint.createTime }}</el-descriptions-item>
        <el-descriptions-item label="更新时间">{{ currentActiveFingerprint.updateTime }}</el-descriptions-item>
      </el-descriptions>
      
      <el-divider content-position="left">探测路径 ({{ (currentActiveFingerprint.paths || []).length }})</el-divider>
      <div class="paths-list">
        <el-tag v-for="(path, idx) in (currentActiveFingerprint.paths || [])" :key="idx" size="small" style="margin-right: 8px; margin-bottom: 8px">
          {{ path }}
        </el-tag>
      </div>
      
      <el-divider content-position="left" v-if="currentActiveFingerprint.relatedFingerprints && currentActiveFingerprint.relatedFingerprints.length">
        关联被动指纹 ({{ currentActiveFingerprint.relatedCount }})
      </el-divider>
      <el-table v-if="currentActiveFingerprint.relatedFingerprints && currentActiveFingerprint.relatedFingerprints.length" 
                :data="currentActiveFingerprint.relatedFingerprints" stripe max-height="300" size="small">
        <el-table-column prop="name" label="名称" width="150" />
        <el-table-column prop="rule" label="匹配规则" min-width="300" show-overflow-tooltip />
        <el-table-column prop="source" label="来源" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.isBuiltin ? 'primary' : 'warning'">
              {{ row.isBuiltin ? '内置' : '自定义' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="enabled" label="状态" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      <div v-else-if="!currentActiveFingerprint.relatedFingerprints || currentActiveFingerprint.relatedFingerprints.length === 0" class="no-related">
        <el-empty description="暂无关联的被动指纹" :image-size="60">
          <template #description>
            <span style="color: #909399">创建同名的被动指纹后将自动关联</span>
          </template>
        </el-empty>
      </div>
      
      <template #footer>
        <el-button @click="activeFingerprintDetailDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 主动指纹编辑对话框 -->
    <el-dialog v-model="activeFingerprintFormDialogVisible" :title="activeFingerprintForm.id ? '编辑主动指纹' : '新增主动指纹'" width="800px">
      <el-form ref="activeFingerprintFormRef" :model="activeFingerprintForm" :rules="activeFingerprintRules" label-width="100px">
        <el-form-item label="应用名称" prop="name">
          <div style="display: flex; gap: 10px; width: 100%;">
            <el-input v-model="activeFingerprintForm.name" placeholder="如: WordPress、Nacos（与被动指纹同名可自动关联）" style="flex: 1;" />
            <el-button type="primary" @click="handleSearchRelatedPassiveFingerprint" :loading="searchPassiveLoading">
              确认
            </el-button>
          </div>
        </el-form-item>
        <el-form-item label="探测路径" prop="pathsText">
          <el-input
            v-model="activeFingerprintForm.pathsText"
            type="textarea"
            :rows="6"
            placeholder="每行一个路径，如：&#10;/admin/login.php&#10;/wp-admin/&#10;/manager/html"
            style="font-family: 'Consolas', 'Monaco', monospace"
          />
          <div class="form-tip">
            主动扫描时会访问这些路径来识别应用，每行一个路径。路径应以 / 开头。
          </div>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="activeFingerprintForm.description" type="textarea" :rows="2" placeholder="可选描述" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="activeFingerprintForm.enabled" />
        </el-form-item>
        
        <!-- 关联被动指纹区域 -->
        <el-divider content-position="left">关联被动指纹</el-divider>
        <div v-if="!activeFingerprintForm.name" class="passive-tip">
          <el-alert type="info" :closable="false" show-icon>
            请先输入应用名称并点击"确认"按钮搜索关联的被动指纹
          </el-alert>
        </div>
        <div v-else-if="searchPassiveLoading" style="text-align: center; padding: 20px;">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span style="margin-left: 8px;">搜索中...</span>
        </div>
        <div v-else>
          <div v-if="relatedPassiveFingerprints.length > 0" class="related-passive-list">
            <el-table :data="relatedPassiveFingerprints" stripe max-height="200" size="small">
              <el-table-column prop="name" label="名称" width="150" />
              <el-table-column prop="rule" label="匹配规则" min-width="250" show-overflow-tooltip />
              <el-table-column prop="source" label="来源" width="80">
                <template #default="{ row }">
                  <el-tag size="small" :type="row.isBuiltin ? 'primary' : 'warning'">
                    {{ row.isBuiltin ? '内置' : '自定义' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="80">
                <template #default="{ row }">
                  <el-button v-if="!row.isBuiltin" type="primary" link size="small" @click="handleEditRelatedPassive(row)">编辑</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
          <div v-else-if="passiveSearched" class="no-passive">
            <el-empty description="未找到同名被动指纹" :image-size="40" />
          </div>
          <div style="margin-top: 10px;">
            <el-button type="success" size="small" @click="handleAddRelatedPassive">
              <el-icon><Plus /></el-icon>新增被动指纹
            </el-button>
          </div>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="activeFingerprintFormDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveActiveFingerprint">保存</el-button>
      </template>
    </el-dialog>

    <!-- 关联被动指纹编辑对话框 -->
    <el-dialog v-model="relatedPassiveDialogVisible" :title="relatedPassiveForm.id ? '编辑被动指纹' : '新增被动指纹'" width="600px" append-to-body>
      <el-form ref="relatedPassiveFormRef" :model="relatedPassiveForm" :rules="relatedPassiveRules" label-width="100px">
        <el-form-item label="应用名称">
          <el-input :value="relatedPassiveForm.name" disabled />
          <div class="form-tip">应用名称与主动指纹保持一致</div>
        </el-form-item>
        <el-form-item label="匹配规则" prop="rule">
          <el-input
            v-model="relatedPassiveForm.rule"
            type="textarea"
            :rows="4"
            placeholder="如: body=&quot;Nacos&quot; || title=&quot;Nacos&quot;"
            style="font-family: 'Consolas', 'Monaco', monospace"
          />
          <div class="form-tip">
            语法：body="xxx" 匹配响应体，title="xxx" 匹配标题，header="xxx" 匹配响应头，&& 表示AND，|| 表示OR
          </div>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="relatedPassiveForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="relatedPassiveDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveRelatedPassive">保存</el-button>
      </template>
    </el-dialog>

    <!-- 主动指纹导入对话框 -->
    <el-dialog v-model="activeImportDialogVisible" title="导入主动扫描指纹" width="700px">
      <el-alert type="info" :closable="false" style="margin-bottom: 15px">
        <template #default>
          <div style="font-size: 12px">
            <p style="margin: 5px 0;"><strong>格式示例：</strong></p>
            <pre style="background: #f5f5f5; padding: 8px; border-radius: 4px; margin: 5px 0;">Alibaba-Nacos:
  - "/nacos/"
SpringBoot-Actuator:
  - "/actuator"
  - "/prod-api/actuator"</pre>
          </div>
        </template>
      </el-alert>
      
      <el-upload
        ref="activeUploadRef"
        drag
        :auto-upload="false"
        :limit="1"
        accept=".yaml,.yml"
        :on-change="handleActiveFileChange"
        :show-file-list="false"
      >
        <el-icon class="el-icon--upload"><Upload /></el-icon>
        <div class="el-upload__text">拖拽文件到此处，或 <em>点击上传</em></div>
        <template #tip>
          <div class="el-upload__tip">支持 .yaml / .yml 格式文件</div>
        </template>
      </el-upload>
      
      <div v-if="activeImportContent" class="file-preview">
        <div class="preview-header">
          <span>文件预览 ({{ activeImportFileName }})</span>
          <el-button type="danger" link size="small" @click="clearActiveImportFile">清除</el-button>
        </div>
        <pre class="preview-content">{{ activeImportContent.substring(0, 800) }}{{ activeImportContent.length > 800 ? '\n...(已截断)' : '' }}</pre>
      </div>
      
      <template #footer>
        <el-button @click="activeImportDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleImportActiveFingerprints" :loading="activeImportLoading" :disabled="!activeImportContent">导入</el-button>
      </template>
    </el-dialog>

    <!-- 主动指纹验证对话框 -->
    <el-dialog v-model="activeValidateDialogVisible" title="验证主动指纹" width="800px">
      <el-form label-width="100px">
        <el-form-item label="指纹名称">
          <el-input :value="activeValidateFingerprint.name" disabled />
        </el-form-item>
        <el-form-item label="探测路径">
          <div class="paths-preview">
            <el-tag v-for="(path, idx) in (activeValidateFingerprint.paths || []).slice(0, 5)" :key="idx" size="small" style="margin-right: 5px; margin-bottom: 3px">
              {{ path }}
            </el-tag>
            <el-tag v-if="activeValidateFingerprint.paths && activeValidateFingerprint.paths.length > 5" size="small" type="info">
              +{{ activeValidateFingerprint.paths.length - 5 }}
            </el-tag>
          </div>
        </el-form-item>
        <el-form-item label="目标URL">
          <el-input v-model="activeValidateUrl" placeholder="请输入目标基础URL，如 https://example.com（不含路径）" />
          <div style="color: #909399; font-size: 12px; margin-top: 4px;">
            验证时会自动拼接探测路径进行访问，并使用关联的被动指纹规则进行匹配
          </div>
        </el-form-item>
      </el-form>
      <div v-if="activeValidateResult" class="validate-result" :class="{ 'matched': activeValidateResult.matched }">
        <div class="result-header">
          <el-tag :type="activeValidateResult.matched ? 'success' : 'info'" size="large">
            {{ activeValidateResult.matched ? '✓ 匹配成功' : '✗ 未匹配' }}
          </el-tag>
        </div>
        <el-table :data="activeValidateResult.results" stripe max-height="350" size="small" style="margin-top: 15px">
          <el-table-column prop="path" label="探测路径" width="200" />
          <el-table-column prop="statusCode" label="状态码" width="80">
            <template #default="{ row }">
              <el-tag v-if="row.statusCode" :type="row.statusCode >= 200 && row.statusCode < 400 ? 'success' : 'warning'" size="small">
                {{ row.statusCode }}
              </el-tag>
              <span v-else style="color: #909399">-</span>
            </template>
          </el-table-column>
          <el-table-column prop="matched" label="匹配" width="80">
            <template #default="{ row }">
              <el-tag :type="row.matched ? 'success' : 'info'" size="small">
                {{ row.matched ? '是' : '否' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="matchedDetails" label="匹配详情" min-width="300">
            <template #default="{ row }">
              <span v-if="row.matched" style="color: #67c23a; font-size: 12px">{{ row.matchedDetails }}</span>
              <span v-else style="color: #909399; font-size: 12px">{{ row.matchedDetails }}</span>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <template #footer>
        <el-button @click="activeValidateDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleActiveValidateFingerprint" :loading="activeValidateLoading" :disabled="!activeValidateUrl">验证</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, ArrowDown, Delete, Upload, Search, Download, Operation, Loading, Check } from '@element-plus/icons-vue'
import { getFingerprintList, saveFingerprint, deleteFingerprint, getFingerprintCategories, syncFingerprints, updateFingerprintEnabled, batchUpdateFingerprintEnabled, importFingerprints, clearCustomFingerprints, validateFingerprint as validateFingerprintApi, batchValidateFingerprints, matchFingerprintAssets, getHttpServiceMappingList, saveHttpServiceMapping, deleteHttpServiceMapping, getHttpServiceConfig, saveHttpServiceConfig, getActiveFingerprintList, saveActiveFingerprint, deleteActiveFingerprint, importActiveFingerprints, exportActiveFingerprints, clearActiveFingerprints, validateActiveFingerprint } from '@/api/fingerprint'
import { saveAs } from 'file-saver'

const route = useRoute()
const router = useRouter()

// 有效的tab名称
const validTabs = ['builtin', 'custom', 'activeFingerprint', 'httpServiceMapping']

// 从URL获取初始tab
const getInitialTab = () => {
  const tab = route.query.tab
  return validTabs.includes(tab) ? tab : 'builtin'
}

const activeTab = ref(getInitialTab())

// 监听路由变化，更新activeTab
watch(() => route.query.tab, (newTab) => {
  if (validTabs.includes(newTab) && newTab !== activeTab.value) {
    activeTab.value = newTab
  }
})

// 批量操作
const batchEnabledLoading = ref(false)

// 内置指纹
const builtinFingerprints = ref([])
const builtinLoading = ref(false)
const builtinFilter = reactive({
  category: '',
  keyword: ''
})
const builtinPagination = reactive({
  page: 1,
  pageSize: 50,
  total: 0
})

// 自定义指纹
const customFingerprints = ref([])
const customLoading = ref(false)
const customFilter = reactive({
  category: '',
  keyword: '',
  enabled: null
})
const customPagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// 分类和统计
const categories = ref([])
const stats = ref({})
const syncLoading = ref(false)

// 导入对话框（自定义指纹）- 支持批量
const importDialogVisible = ref(false)
const importFiles = ref([]) // 批量文件列表
const importLoading = ref(false)
const uploadRef = ref()

// 导入对话框（内置指纹）
const builtinImportDialogVisible = ref(false)
const builtinImportContent = ref('')
const builtinImportFileName = ref('')
const builtinImportLoading = ref(false)
const builtinUploadRef = ref()

// 导出
const exportLoading = ref(false)

// 验证对话框
const validateDialogVisible = ref(false)
const validateFingerprint = ref({})
const validateUrl = ref('')
const validateResult = ref(null)
const validateLoading = ref(false)

// 批量验证对话框
const batchValidateDialogVisible = ref(false)
const batchValidateUrl = ref('')
const batchValidateScope = ref('all')
const batchValidateResult = ref(null)
const batchValidateLoading = ref(false)

// 匹配现有资产对话框
const matchAssetsDialogVisible = ref(false)
const matchAssetsFingerprint = ref({})
const matchAssetsResult = ref(null)
const matchAssetsLoading = ref(false)
const matchAssetsUpdateAsset = ref(true) // 默认勾选更新资产

// 详情对话框
const detailDialogVisible = ref(false)
const currentFingerprint = ref({})

// 编辑对话框
const formDialogVisible = ref(false)
const fingerprintFormRef = ref()
const fingerprintForm = reactive({
  id: '',
  name: '',
  category: '',
  website: '',
  description: '',
  rule: '',
  type: 'passive', // passive: 被动指纹, active: 主动指纹
  activePaths: [],
  activePathsText: '', // 用于编辑的文本形式
  source: 'custom',
  enabled: true
})
const fingerprintRules = {
  name: [{ required: true, message: '请输入应用名称', trigger: 'blur' }],
  rule: [{ required: true, message: '请输入匹配规则', trigger: 'blur' }]
}

// HTTP服务映射
const httpServiceSubTab = ref('serviceMapping') // 子tab: serviceMapping, webPorts
const httpServiceMappings = ref([])
const httpServiceLoading = ref(false)
const httpServiceFilter = reactive({
  isHttp: null,
  keyword: ''
})
const httpServiceStats = ref({
  total: 0,
  httpCount: 0,
  nonHttpCount: 0
})
const httpServicePagination = reactive({
  page: 1,
  pageSize: 10,
  total: 0
})

// HTTP服务映射编辑对话框
const httpServiceMappingDialogVisible = ref(false)
const httpServiceMappingFormRef = ref()
const httpServiceMappingForm = reactive({
  id: '',
  serviceName: '',
  isHttp: true,
  description: '',
  enabled: true
})
const httpServiceMappingRules = {
  serviceName: [{ required: true, message: '请输入服务名称', trigger: 'blur' }]
}

// Web端口配置
const webPortsLoading = ref(false)
const webPortsSaving = ref(false)
const webPortsConfig = reactive({
  httpPortsText: '',
  httpsPortsText: '',
  nonHttpPortsText: '',
  description: ''
})

// 默认端口配置
const defaultHttpPorts = [80, 8080, 8000, 8888, 8081, 8082, 8083, 8084, 8085, 8086, 8087, 8088, 8089, 8090, 9000, 9001, 9080, 3000, 3001, 5000, 5001, 8008, 8009, 8181, 8200, 8300, 8400, 8500, 8600, 8800, 8880, 8983, 9090, 9091, 9200, 9300, 10000]
const defaultHttpsPorts = [443, 8443, 9443, 4443, 10443]
const defaultNonHttpPorts = [
  // 远程管理
  22, 23, 3389, 5900, 5901, 5902,
  // 文件传输
  20, 21, 69,
  // 邮件服务
  25, 110, 143, 465, 587, 993, 995,
  // 数据库
  1433, 1521, 3306, 5432, 6379, 27017, 9042,
  // 消息队列
  5672, 6650, 9092,
  // 目录服务
  389, 636,
  // DNS
  53,
  // Windows服务
  135, 137, 138, 139, 445,
  // 其他
  111, 161, 162, 514, 1080, 1194, 1883, 2049, 2181, 3268, 3269, 11211
]

// 主动扫描指纹
const activeFingerprints = ref([])
const activeFingerprintLoading = ref(false)
const activeFingerprintFilter = reactive({
  keyword: '',
  enabled: null
})
const activeFingerprintPagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})
const activeFingerprintStats = ref({
  total: 0,
  enabled: 0
})

// 主动指纹详情对话框
const activeFingerprintDetailDialogVisible = ref(false)
const currentActiveFingerprint = ref({})

// 主动指纹编辑对话框
const activeFingerprintFormDialogVisible = ref(false)
const activeFingerprintFormRef = ref()
const activeFingerprintForm = reactive({
  id: '',
  name: '',
  paths: [],
  pathsText: '',
  description: '',
  enabled: true
})
const activeFingerprintRules = {
  name: [{ required: true, message: '请输入应用名称', trigger: 'blur' }],
  pathsText: [{ required: true, message: '请输入探测路径', trigger: 'blur' }]
}

// 主动指纹导入对话框
const activeImportDialogVisible = ref(false)
const activeImportContent = ref('')
const activeImportFileName = ref('')
const activeImportLoading = ref(false)
const activeUploadRef = ref()
const activeExportLoading = ref(false)

// 主动指纹验证对话框
const activeValidateDialogVisible = ref(false)
const activeValidateFingerprint = ref({})
const activeValidateUrl = ref('')
const activeValidateResult = ref(null)
const activeValidateLoading = ref(false)

// 关联被动指纹
const searchPassiveLoading = ref(false)
const passiveSearched = ref(false)
const relatedPassiveFingerprints = ref([])
const relatedPassiveDialogVisible = ref(false)
const relatedPassiveFormRef = ref()
const relatedPassiveForm = reactive({
  id: '',
  name: '',
  rule: '',
  enabled: true
})
const relatedPassiveRules = {
  rule: [{ required: true, message: '请输入匹配规则', trigger: 'blur' }]
}

onMounted(() => {
  // 如果URL没有tab参数，添加默认的tab参数
  if (!route.query.tab) {
    router.replace({ query: { ...route.query, tab: activeTab.value } })
  }
  loadCategories()
  // 根据当前tab加载数据
  handleTabChange(activeTab.value)
})

function handleTabChange(tab) {
  // Tab切换时更新URL
  router.replace({ query: { ...route.query, tab: tab } })
  
  if (tab === 'builtin' && builtinFingerprints.value.length === 0) {
    loadBuiltinFingerprints()
  } else if (tab === 'custom' && customFingerprints.value.length === 0) {
    loadCustomFingerprints()
  } else if (tab === 'activeFingerprint' && activeFingerprints.value.length === 0) {
    loadActiveFingerprints()
  } else if (tab === 'httpServiceMapping' && httpServiceMappings.value.length === 0) {
    loadHttpServiceMappings()
  }
}

async function loadCategories() {
  try {
    const res = await getFingerprintCategories()
    if (res.code === 0) {
      categories.value = res.categories || []
      stats.value = res.stats || {}
    }
  } catch (e) {
    console.error('Failed to load categories:', e)
  }
}

async function loadBuiltinFingerprints() {
  builtinLoading.value = true
  try {
    const res = await getFingerprintList({
      category: builtinFilter.category,
      keyword: builtinFilter.keyword,
      isBuiltin: true,
      page: builtinPagination.page,
      pageSize: builtinPagination.pageSize
    })
    if (res.code === 0) {
      builtinFingerprints.value = res.list || []
      builtinPagination.total = res.total
    }
  } finally {
    builtinLoading.value = false
  }
}

async function loadCustomFingerprints() {
  customLoading.value = true
  try {
    const params = {
      isBuiltin: false,
      category: customFilter.category,
      keyword: customFilter.keyword,
      page: customPagination.page,
      pageSize: customPagination.pageSize
    }
    // 添加状态筛选
    if (customFilter.enabled !== null && customFilter.enabled !== '') {
      params.enabled = customFilter.enabled
    }
    
    const res = await getFingerprintList(params)
    if (res.code === 0) {
      customFingerprints.value = res.list || []
      customPagination.total = res.total
    }
  } finally {
    customLoading.value = false
  }
}

function resetCustomFilter() {
  customFilter.category = ''
  customFilter.keyword = ''
  customFilter.enabled = null
  customPagination.page = 1
  loadCustomFingerprints()
}

// 批量更新启用状态
async function handleBatchEnabledCommand(command) {
  const enabled = command === 'enableAll'
  const action = enabled ? '启用' : '禁用'
  
  try {
    await ElMessageBox.confirm(
      `确定要${action}全部自定义指纹吗？`,
      '批量操作',
      { type: 'warning' }
    )
  } catch {
    return
  }
  
  batchEnabledLoading.value = true
  try {
    const res = await batchUpdateFingerprintEnabled({
      ids: [],  // 空数组，使用 all 参数
      all: true,
      enabled: enabled
    })
    if (res.code === 0) {
      ElMessage.success(res.msg)
      loadCustomFingerprints()
      loadCategories()
    } else {
      ElMessage.error(res.msg)
    }
  } catch (err) {
    ElMessage.error('操作失败: ' + (err.message || '未知错误'))
  } finally {
    batchEnabledLoading.value = false
  }
}

// 显示导入对话框
function showImportDialog() {
  importFiles.value = []
  if (uploadRef.value) {
    uploadRef.value.clearFiles()
  }
  importDialogVisible.value = true
}

// 处理文件选择（批量）
function handleFileChange(file) {
  if (!file || !file.raw) return
  
  // 检查文件类型
  const fileName = file.name.toLowerCase()
  if (!fileName.endsWith('.json') && !fileName.endsWith('.yml') && !fileName.endsWith('.yaml')) {
    ElMessage.warning(`文件 ${file.name} 不是支持的格式，已跳过`)
    return
  }
  
  // 检查是否已存在
  if (importFiles.value.some(f => f.name === file.name)) {
    return
  }
  
  // 读取文件内容
  const reader = new FileReader()
  reader.onload = (e) => {
    let content = e.target.result
    // 去除BOM头
    if (content.charCodeAt(0) === 0xFEFF) {
      content = content.slice(1)
    }
    importFiles.value.push({
      name: file.name,
      size: file.size,
      content: content
    })
  }
  reader.onerror = () => {
    ElMessage.error(`文件 ${file.name} 读取失败`)
  }
  reader.readAsText(file.raw, 'UTF-8')
}

// 移除单个导入文件
function removeImportFile(index) {
  importFiles.value.splice(index, 1)
}

// 清除所有导入文件
function clearImportFile() {
  importFiles.value = []
  if (uploadRef.value) {
    uploadRef.value.clearFiles()
  }
}

// 格式化文件大小
function formatFileSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

// 导入指纹（批量）
async function handleImportFingerprints() {
  if (importFiles.value.length === 0) {
    ElMessage.warning('请先选择文件')
    return
  }
  
  importLoading.value = true
  let totalImported = 0
  let totalSkipped = 0
  let failedFiles = []
  
  try {
    // 逐个文件导入
    for (const file of importFiles.value) {
      try {
        const res = await importFingerprints({
          content: file.content,
          format: 'auto'
        })
        
        if (res.code === 0) {
          totalImported += res.imported || 0
          totalSkipped += res.skipped || 0
        } else {
          failedFiles.push(file.name + ': ' + (res.msg || '导入失败'))
        }
      } catch (err) {
        failedFiles.push(file.name + ': ' + (err.message || '请求失败'))
      }
    }
    
    importDialogVisible.value = false
    
    // 显示导入结果
    let resultHtml = `<div style="text-align: center; font-size: 14px;">
      <p style="margin-bottom: 10px;">批量导入完成</p>
      <p><strong style="color: #67c23a; font-size: 20px;">${totalImported}</strong> 个指纹导入成功</p>
      <p><strong style="color: #909399; font-size: 20px;">${totalSkipped}</strong> 个指纹已跳过</p>`
    
    if (failedFiles.length > 0) {
      resultHtml += `<p style="color: #f56c6c; margin-top: 10px;">失败文件：</p>
        <div style="text-align: left; font-size: 12px; max-height: 100px; overflow-y: auto;">
          ${failedFiles.map(f => `<div>${f}</div>`).join('')}
        </div>`
    }
    resultHtml += '</div>'
    
    ElMessageBox.alert(resultHtml, '导入结果', {
      dangerouslyUseHTMLString: true,
      confirmButtonText: '确定',
      type: failedFiles.length > 0 ? 'warning' : 'success'
    })
    
    loadCustomFingerprints()
    loadCategories()
  } catch (err) {
    console.error('Import error:', err)
    ElMessage.error('导入请求失败: ' + (err.message || '未知错误'))
  } finally {
    importLoading.value = false
  }
}

// 显示内置指纹导入对话框
function showBuiltinImportDialog() {
  builtinImportContent.value = ''
  builtinImportFileName.value = ''
  if (builtinUploadRef.value) {
    builtinUploadRef.value.clearFiles()
  }
  builtinImportDialogVisible.value = true
}

// 处理内置指纹文件选择
function handleBuiltinFileChange(file) {
  if (!file || !file.raw) return
  
  const reader = new FileReader()
  reader.onload = (e) => {
    let content = e.target.result
    // 去除BOM头
    if (content.charCodeAt(0) === 0xFEFF) {
      content = content.slice(1)
    }
    builtinImportContent.value = content
    builtinImportFileName.value = file.name
  }
  reader.onerror = () => {
    ElMessage.error('文件读取失败')
  }
  reader.readAsText(file.raw, 'UTF-8')
}

// 清除内置指纹导入文件
function clearBuiltinImportFile() {
  builtinImportContent.value = ''
  builtinImportFileName.value = ''
  if (builtinUploadRef.value) {
    builtinUploadRef.value.clearFiles()
  }
}

// 导入内置指纹（Wappalyzer格式）
async function handleImportBuiltinFingerprints() {
  if (!builtinImportContent.value.trim()) {
    ElMessage.warning('请先选择文件')
    return
  }
  
  builtinImportLoading.value = true
  try {
    const res = await importFingerprints({
      content: builtinImportContent.value,
      format: 'wappalyzer',
      isBuiltin: true
    })
    
    if (res.code === 0) {
      builtinImportDialogVisible.value = false
      ElMessageBox.alert(
        `<div style="text-align: center; font-size: 14px;">
          <p style="margin-bottom: 10px;">导入完成</p>
          <p><strong style="color: #67c23a; font-size: 20px;">${res.imported || 0}</strong> 个指纹导入成功</p>
          <p><strong style="color: #909399; font-size: 20px;">${res.skipped || 0}</strong> 个指纹已跳过</p>
        </div>`,
        '导入结果',
        {
          dangerouslyUseHTMLString: true,
          confirmButtonText: '确定',
          type: 'success'
        }
      )
      loadBuiltinFingerprints()
      loadCategories()
    } else {
      ElMessageBox.alert(res.msg || '导入失败', '导入错误', {
        type: 'error',
        confirmButtonText: '确定'
      })
    }
  } catch (err) {
    ElMessage.error('导入请求失败: ' + (err.message || '未知错误'))
  } finally {
    builtinImportLoading.value = false
  }
}

// 截断规则显示
function truncateRule(rule) {
  if (!rule) return ''
  return rule.length > 50 ? rule.substring(0, 50) + '...' : rule
}

async function handleSyncCommand(command) {
  if (command === 'force') {
    try {
      await ElMessageBox.confirm('强制同步将删除所有内置指纹并重新导入，确定继续吗？', '提示', { type: 'warning' })
    } catch {
      return
    }
  }
  
  syncLoading.value = true
  try {
    const res = await syncFingerprints({ force: command === 'force' })
    if (res.code === 0) {
      ElMessage.success(res.msg)
      setTimeout(() => {
        loadCategories()
        loadBuiltinFingerprints()
      }, 3000)
    } else {
      ElMessage.error(res.msg)
    }
  } finally {
    syncLoading.value = false
  }
}

async function handleToggleEnabled(row) {
  try {
    const res = await updateFingerprintEnabled({ id: row.id, enabled: row.enabled })
    if (res.code !== 0) {
      row.enabled = !row.enabled
      ElMessage.error(res.msg)
    }
  } catch {
    row.enabled = !row.enabled
  }
}

function showFingerprintDetail(row) {
  currentFingerprint.value = row
  detailDialogVisible.value = true
}

function showFingerprintForm(row = null) {
  if (row) {
    Object.assign(fingerprintForm, {
      id: row.id,
      name: row.name,
      category: row.category || '',
      website: row.website || '',
      description: row.description || '',
      rule: row.rule || '',
      type: row.type || 'passive',
      activePaths: row.activePaths || [],
      activePathsText: (row.activePaths || []).join('\n'),
      source: row.source || 'custom',
      enabled: row.enabled
    })
  } else {
    Object.assign(fingerprintForm, {
      id: '',
      name: '',
      category: '',
      website: '',
      description: '',
      rule: '',
      type: 'passive',
      activePaths: [],
      activePathsText: '',
      source: 'custom',
      enabled: true
    })
  }
  formDialogVisible.value = true
}

async function handleSaveFingerprint() {
  await fingerprintFormRef.value.validate()
  // 处理主动指纹路径
  if (fingerprintForm.type === 'active' && fingerprintForm.activePathsText) {
    fingerprintForm.activePaths = fingerprintForm.activePathsText
      .split('\n')
      .map(p => p.trim())
      .filter(p => p && p.startsWith('/'))
  } else {
    fingerprintForm.activePaths = []
  }
  const res = await saveFingerprint(fingerprintForm)
  if (res.code === 0) {
    ElMessage.success('保存成功')
    formDialogVisible.value = false
    loadCustomFingerprints()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleDeleteFingerprint(row) {
  await ElMessageBox.confirm('确定删除该指纹吗？', '提示', { type: 'warning' })
  const res = await deleteFingerprint({ id: row.id })
  if (res.code === 0) {
    ElMessage.success('删除成功')
    loadCustomFingerprints()
  }
}

// 清空自定义指纹
async function handleClearCustomFingerprints() {
  try {
    await ElMessageBox.confirm(
      '确定要清空所有自定义指纹吗？此操作不可恢复！',
      '警告',
      {
        type: 'warning',
        confirmButtonText: '确定清空',
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger'
      }
    )
    
    const res = await clearCustomFingerprints({ clearAll: true })
    if (res.code === 0) {
      ElMessage.success(`已清空 ${res.deleted || 0} 个自定义指纹`)
      loadCustomFingerprints()
      loadCategories()
    } else {
      ElMessage.error(res.msg || '清空失败')
    }
  } catch {
    // 用户取消
  }
}

// 导出自定义指纹（导出为单个yml文件）
async function handleExportFingerprints() {
  if (customFingerprints.value.length === 0 && customPagination.total === 0) {
    ElMessage.warning('没有可导出的指纹')
    return
  }

  exportLoading.value = true

  try {
    // 获取所有自定义指纹
    let allFingerprints = []

    if (customPagination.total > customFingerprints.value.length) {
      // 需要获取全部数据
      const res = await getFingerprintList({
        type: 'custom',
        page: 1,
        pageSize: customPagination.total
      })
      if (res.code === 0) {
        allFingerprints = res.list || []
      } else {
        allFingerprints = customFingerprints.value
      }
    } else {
      allFingerprints = customFingerprints.value
    }

    if (allFingerprints.length === 0) {
      ElMessage.warning('没有可导出的指纹')
      return
    }

    // 生成YAML内容（ARL finger.yml 兼容格式）
    const yamlContent = allFingerprints.map(fp => {
      // 转义规则中的单引号
      const rule = (fp.rule || '').replace(/'/g, "''")
      return `- name: "${fp.name}"\n  rule: '${rule}'`
    }).join('\n\n')

    // 创建Blob并下载
    const blob = new Blob([yamlContent], { type: 'text/yaml;charset=utf-8' })
    const dateStr = new Date().toISOString().slice(0, 10)
    saveAs(blob, `custom-fingerprints-${dateStr}.yml`)

    ElMessage.success(`成功导出 ${allFingerprints.length} 个指纹`)
  } catch (e) {
    console.error('Export error:', e)
    ElMessage.error('导出失败')
  } finally {
    exportLoading.value = false
  }
}

// 检查是否有任何匹配规则
function hasAnyRule(fp) {
  if (!fp) return false
  return (
    (fp.rule && fp.rule.length > 0) ||
    (fp.headers && Object.keys(fp.headers).length > 0) ||
    (fp.cookies && Object.keys(fp.cookies).length > 0) ||
    (fp.html && fp.html.length > 0) ||
    (fp.scripts && fp.scripts.length > 0) ||
    (fp.scriptSrc && fp.scriptSrc.length > 0) ||
    (fp.js && Object.keys(fp.js).length > 0) ||
    (fp.meta && Object.keys(fp.meta).length > 0) ||
    (fp.css && fp.css.length > 0) ||
    (fp.url && fp.url.length > 0) ||
    (fp.dom && fp.dom.length > 0)
  )
}

// 格式化DOM规则显示
function formatDom(dom) {
  if (!dom) return ''
  try {
    const parsed = JSON.parse(dom)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return dom
  }
}

// 复制ID到剪贴板
function copyToClipboard(text) {
  navigator.clipboard.writeText(text).then(() => {
    ElMessage.success('ID已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败')
  })
}

// 显示验证对话框
function showValidateDialog(row) {
  validateFingerprint.value = row
  validateUrl.value = ''
  validateResult.value = null
  validateDialogVisible.value = true
}

// 执行指纹验证
async function handleValidateFingerprint() {
  if (!validateUrl.value) {
    ElMessage.warning('请输入目标URL')
    return
  }

  validateLoading.value = true
  validateResult.value = null

  try {
    const res = await validateFingerprintApi({
      id: validateFingerprint.value.id,
      url: validateUrl.value
    })

    if (res.code === 0) {
      validateResult.value = {
        matched: res.matched,
        details: res.details
      }
    } else {
      ElMessage.error(res.msg || '验证失败')
    }
  } catch (e) {
    ElMessage.error('验证请求失败: ' + e.message)
  } finally {
    validateLoading.value = false
  }
}

// 格式化验证详情，高亮命中条件
function formatValidateDetails(details) {
  if (!details) return ''
  
  // 转义HTML特殊字符
  const escapeHtml = (str) => {
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;')
  }
  
  // 按行处理
  const lines = details.split('\n')
  const formattedLines = lines.map(line => {
    const escapedLine = escapeHtml(line)
    // 高亮包含 ✓ 的行（命中条件）
    if (line.includes('✓')) {
      return `<span class="matched-condition">${escapedLine}</span>`
    }
    return escapedLine
  })
  
  return formattedLines.join('\n')
}

// 显示批量验证对话框
function showBatchValidateDialog() {
  batchValidateUrl.value = ''
  batchValidateScope.value = 'all'
  batchValidateResult.value = null
  batchValidateDialogVisible.value = true
}

// 执行批量验证
async function handleBatchValidate() {
  if (!batchValidateUrl.value) {
    ElMessage.warning('请输入目标URL')
    return
  }

  batchValidateLoading.value = true
  batchValidateResult.value = null

  try {
    const res = await batchValidateFingerprints({
      url: batchValidateUrl.value,
      scope: batchValidateScope.value
    })

    if (res.code === 0) {
      batchValidateResult.value = {
        matchedCount: res.matchedCount,
        duration: res.duration,
        matched: res.matched || []
      }
    } else {
      ElMessage.error(res.msg || '验证失败')
    }
  } catch (e) {
    ElMessage.error('验证请求失败: ' + e.message)
  } finally {
    batchValidateLoading.value = false
  }
}

// 显示匹配现有资产对话框
function showMatchAssetsDialog(row) {
  matchAssetsFingerprint.value = row
  matchAssetsResult.value = null
  matchAssetsUpdateAsset.value = true // 重置为默认勾选
  matchAssetsDialogVisible.value = true
}

// 执行匹配现有资产
async function handleMatchAssets() {
  matchAssetsLoading.value = true
  matchAssetsResult.value = null

  try {
    const res = await matchFingerprintAssets({
      fingerprintId: matchAssetsFingerprint.value.id,
      updateAsset: matchAssetsUpdateAsset.value
    })

    if (res.code === 0) {
      matchAssetsResult.value = {
        matchedCount: res.matchedCount,
        totalScanned: res.totalScanned,
        updatedCount: res.updatedCount || 0,
        duration: res.duration,
        matchedList: res.matchedList || []
      }
      if (res.updatedCount > 0) {
        ElMessage.success(`已更新 ${res.updatedCount} 个资产的指纹信息`)
      }
    } else {
      ElMessage.error(res.msg || '匹配失败')
    }
  } catch (e) {
    ElMessage.error('匹配请求失败: ' + e.message)
  } finally {
    matchAssetsLoading.value = false
  }
}

// ==================== HTTP服务映射相关方法 ====================

// HTTP服务映射全量数据（用于前端分页）
const httpServiceAllData = ref([])

// 加载HTTP服务映射列表
async function loadHttpServiceMappings() {
  httpServiceLoading.value = true
  try {
    // 构建请求参数，过滤掉 null 值
    const params = {}
    if (httpServiceFilter.isHttp !== null && httpServiceFilter.isHttp !== undefined && httpServiceFilter.isHttp !== '') {
      params.isHttp = httpServiceFilter.isHttp
    }
    if (httpServiceFilter.keyword) {
      params.keyword = httpServiceFilter.keyword
    }
    
    const res = await getHttpServiceMappingList(params)
    if (res.code === 0) {
      const list = res.list || []
      httpServiceAllData.value = list
      
      // 计算统计信息（基于当前筛选结果）
      httpServiceStats.value = {
        total: list.length,
        httpCount: list.filter(item => item.isHttp).length,
        nonHttpCount: list.filter(item => !item.isHttp).length
      }
      
      // 更新分页总数
      httpServicePagination.total = list.length
      
      // 前端分页
      const start = (httpServicePagination.page - 1) * httpServicePagination.pageSize
      const end = start + httpServicePagination.pageSize
      httpServiceMappings.value = list.slice(start, end)
    }
  } finally {
    httpServiceLoading.value = false
  }
}

// 显示HTTP服务映射编辑对话框
function showHttpServiceMappingForm(row = null) {
  if (row) {
    Object.assign(httpServiceMappingForm, {
      id: row.id,
      serviceName: row.serviceName,
      isHttp: row.isHttp,
      description: row.description || '',
      enabled: row.enabled
    })
  } else {
    Object.assign(httpServiceMappingForm, {
      id: '',
      serviceName: '',
      isHttp: true,
      description: '',
      enabled: true
    })
  }
  httpServiceMappingDialogVisible.value = true
}

// 保存HTTP服务映射
async function handleSaveHttpServiceMapping() {
  await httpServiceMappingFormRef.value.validate()
  const res = await saveHttpServiceMapping(httpServiceMappingForm)
  if (res.code === 0) {
    ElMessage.success('保存成功')
    httpServiceMappingDialogVisible.value = false
    loadHttpServiceMappings()
  } else {
    ElMessage.error(res.msg || '保存失败')
  }
}

// 切换HTTP服务映射启用状态
async function handleToggleHttpServiceEnabled(row) {
  try {
    const res = await saveHttpServiceMapping({
      id: row.id,
      serviceName: row.serviceName,
      isHttp: row.isHttp,
      description: row.description,
      enabled: row.enabled
    })
    if (res.code !== 0) {
      row.enabled = !row.enabled
      ElMessage.error(res.msg || '更新失败')
    }
  } catch {
    row.enabled = !row.enabled
  }
}

// 删除HTTP服务映射
async function handleDeleteHttpServiceMapping(row) {
  await ElMessageBox.confirm('确定删除该映射吗？', '提示', { type: 'warning' })
  const res = await deleteHttpServiceMapping({ id: row.id })
  if (res.code === 0) {
    ElMessage.success('删除成功')
    loadHttpServiceMappings()
  } else {
    ElMessage.error(res.msg || '删除失败')
  }
}

// ==================== Web端口配置相关方法 ====================

// 处理HTTP服务子tab切换
function handleHttpServiceSubTabChange(tab) {
  if (tab === 'serviceMapping' && httpServiceMappings.value.length === 0) {
    loadHttpServiceMappings()
  } else if (tab === 'webPorts' && !webPortsConfig.httpPortsText) {
    loadWebPortsConfig()
  }
}

// 加载Web端口配置
async function loadWebPortsConfig() {
  webPortsLoading.value = true
  try {
    const res = await getHttpServiceConfig()
    if (res.code === 0 && res.data) {
      const data = res.data
      webPortsConfig.httpPortsText = (data.httpPorts || []).join(', ')
      webPortsConfig.httpsPortsText = (data.httpsPorts || []).join(', ')
      webPortsConfig.nonHttpPortsText = (data.nonHttpPorts || []).join(', ')
      webPortsConfig.description = data.description || ''
    }
  } catch (e) {
    console.error('Failed to load web ports config:', e)
  } finally {
    webPortsLoading.value = false
  }
}

// 解析端口文本为数组
function parsePortsText(text) {
  if (!text) return []
  // 支持逗号、空格、换行分隔
  return text.split(/[,\s\n]+/)
    .map(s => parseInt(s.trim(), 10))
    .filter(n => !isNaN(n) && n > 0 && n <= 65535)
    .filter((v, i, a) => a.indexOf(v) === i) // 去重
    .sort((a, b) => a - b)
}

// 计算端口数量
function parsePortsCount(text) {
  return parsePortsText(text).length
}

// 保存Web端口配置
async function handleSaveWebPortsConfig() {
  const httpPorts = parsePortsText(webPortsConfig.httpPortsText)
  const httpsPorts = parsePortsText(webPortsConfig.httpsPortsText)
  const nonHttpPorts = parsePortsText(webPortsConfig.nonHttpPortsText)
  
  if (httpPorts.length === 0 && httpsPorts.length === 0) {
    ElMessage.warning('请至少配置一个Web端口')
    return
  }
  
  webPortsSaving.value = true
  try {
    const res = await saveHttpServiceConfig({
      httpPorts,
      httpsPorts,
      nonHttpPorts,
      description: webPortsConfig.description
    })
    if (res.code === 0) {
      ElMessage.success('保存成功')
      // 更新显示的文本（格式化后的）
      webPortsConfig.httpPortsText = httpPorts.join(', ')
      webPortsConfig.httpsPortsText = httpsPorts.join(', ')
      webPortsConfig.nonHttpPortsText = nonHttpPorts.join(', ')
    } else {
      ElMessage.error(res.msg || '保存失败')
    }
  } finally {
    webPortsSaving.value = false
  }
}

// 恢复默认配置
function resetWebPortsToDefault() {
  webPortsConfig.httpPortsText = defaultHttpPorts.join(', ')
  webPortsConfig.httpsPortsText = defaultHttpsPorts.join(', ')
  webPortsConfig.nonHttpPortsText = defaultNonHttpPorts.join(', ')
  webPortsConfig.description = '默认端口配置'
  ElMessage.info('已恢复默认配置，请点击保存生效')
}

// 添加常用HTTP端口
function addCommonHttpPorts() {
  const commonPorts = [80, 8080, 8000, 8888, 3000, 5000, 9000]
  const currentPorts = parsePortsText(webPortsConfig.httpPortsText)
  const newPorts = [...new Set([...currentPorts, ...commonPorts])].sort((a, b) => a - b)
  webPortsConfig.httpPortsText = newPorts.join(', ')
  ElMessage.info('已添加常用HTTP端口，请点击保存生效')
}

// 添加常用HTTPS端口
function addCommonHttpsPorts() {
  const commonPorts = [443, 8443, 9443, 4443]
  const currentPorts = parsePortsText(webPortsConfig.httpsPortsText)
  const newPorts = [...new Set([...currentPorts, ...commonPorts])].sort((a, b) => a - b)
  webPortsConfig.httpsPortsText = newPorts.join(', ')
  ElMessage.info('已添加常用HTTPS端口，请点击保存生效')
}

// 添加常用非Web端口
function addCommonNonHttpPorts() {
  const commonPorts = [22, 23, 25, 110, 143, 135, 139, 445, 3306, 3389, 5432, 6379, 27017]
  const currentPorts = parsePortsText(webPortsConfig.nonHttpPortsText)
  const newPorts = [...new Set([...currentPorts, ...commonPorts])].sort((a, b) => a - b)
  webPortsConfig.nonHttpPortsText = newPorts.join(', ')
  ElMessage.info('已添加常用非Web端口，请点击保存生效')
}

// ==================== 主动扫描指纹相关方法 ====================

// 加载主动指纹列表
async function loadActiveFingerprints() {
  activeFingerprintLoading.value = true
  try {
    const params = {
      page: activeFingerprintPagination.page,
      pageSize: activeFingerprintPagination.pageSize,
      keyword: activeFingerprintFilter.keyword
    }
    if (activeFingerprintFilter.enabled !== null && activeFingerprintFilter.enabled !== '') {
      params.enabled = activeFingerprintFilter.enabled
    }
    
    const res = await getActiveFingerprintList(params)
    if (res.code === 0) {
      activeFingerprints.value = res.list || []
      activeFingerprintPagination.total = res.total
      activeFingerprintStats.value = res.stats || { total: 0, enabled: 0 }
    }
  } finally {
    activeFingerprintLoading.value = false
  }
}

// 重置主动指纹筛选
function resetActiveFingerprintFilter() {
  activeFingerprintFilter.keyword = ''
  activeFingerprintFilter.enabled = null
  activeFingerprintPagination.page = 1
  loadActiveFingerprints()
}

// 显示主动指纹详情
function showActiveFingerprintDetail(row) {
  currentActiveFingerprint.value = row
  activeFingerprintDetailDialogVisible.value = true
}

// 显示主动指纹编辑对话框
function showActiveFingerprintForm(row = null) {
  // 重置关联被动指纹状态
  relatedPassiveFingerprints.value = []
  passiveSearched.value = false
  
  if (row) {
    Object.assign(activeFingerprintForm, {
      id: row.id,
      name: row.name,
      paths: row.paths || [],
      pathsText: (row.paths || []).join('\n'),
      description: row.description || '',
      enabled: row.enabled
    })
    // 编辑时自动搜索关联被动指纹
    if (row.name) {
      handleSearchRelatedPassiveFingerprint()
    }
  } else {
    Object.assign(activeFingerprintForm, {
      id: '',
      name: '',
      paths: [],
      pathsText: '',
      description: '',
      enabled: true
    })
  }
  activeFingerprintFormDialogVisible.value = true
}

// 搜索关联的被动指纹
async function handleSearchRelatedPassiveFingerprint() {
  if (!activeFingerprintForm.name) {
    ElMessage.warning('请先输入应用名称')
    return
  }
  
  searchPassiveLoading.value = true
  passiveSearched.value = false
  
  try {
    const res = await getFingerprintList({
      keyword: activeFingerprintForm.name,
      page: 1,
      pageSize: 100
    })
    
    if (res.code === 0) {
      // 精确匹配名称
      relatedPassiveFingerprints.value = (res.list || []).filter(fp => fp.name === activeFingerprintForm.name)
      passiveSearched.value = true
    }
  } catch (e) {
    ElMessage.error('搜索失败: ' + e.message)
  } finally {
    searchPassiveLoading.value = false
  }
}

// 新增关联被动指纹
function handleAddRelatedPassive() {
  if (!activeFingerprintForm.name) {
    ElMessage.warning('请先输入应用名称')
    return
  }
  
  Object.assign(relatedPassiveForm, {
    id: '',
    name: activeFingerprintForm.name,
    rule: '',
    enabled: true
  })
  relatedPassiveDialogVisible.value = true
}

// 编辑关联被动指纹
function handleEditRelatedPassive(row) {
  Object.assign(relatedPassiveForm, {
    id: row.id,
    name: row.name,
    rule: row.rule || '',
    enabled: row.enabled
  })
  relatedPassiveDialogVisible.value = true
}

// 保存关联被动指纹
async function handleSaveRelatedPassive() {
  await relatedPassiveFormRef.value.validate()
  
  const res = await saveFingerprint({
    id: relatedPassiveForm.id,
    name: relatedPassiveForm.name,
    rule: relatedPassiveForm.rule,
    source: 'custom',
    enabled: relatedPassiveForm.enabled
  })
  
  if (res.code === 0) {
    ElMessage.success('保存成功')
    relatedPassiveDialogVisible.value = false
    // 刷新关联被动指纹列表
    handleSearchRelatedPassiveFingerprint()
  } else {
    ElMessage.error(res.msg || '保存失败')
  }
}

// 保存主动指纹
async function handleSaveActiveFingerprint() {
  await activeFingerprintFormRef.value.validate()
  
  // 解析路径
  const paths = activeFingerprintForm.pathsText
    .split('\n')
    .map(p => p.trim())
    .filter(p => p && p.startsWith('/'))
  
  if (paths.length === 0) {
    ElMessage.warning('请输入有效的探测路径（以/开头）')
    return
  }
  
  const res = await saveActiveFingerprint({
    id: activeFingerprintForm.id,
    name: activeFingerprintForm.name,
    paths: paths,
    description: activeFingerprintForm.description,
    enabled: activeFingerprintForm.enabled
  })
  
  if (res.code === 0) {
    ElMessage.success('保存成功')
    activeFingerprintFormDialogVisible.value = false
    loadActiveFingerprints()
  } else {
    ElMessage.error(res.msg || '保存失败')
  }
}

// 切换主动指纹启用状态
async function handleToggleActiveFingerprintEnabled(row) {
  try {
    const res = await saveActiveFingerprint({
      id: row.id,
      name: row.name,
      paths: row.paths,
      description: row.description,
      enabled: row.enabled
    })
    if (res.code !== 0) {
      row.enabled = !row.enabled
      ElMessage.error(res.msg || '更新失败')
    }
  } catch {
    row.enabled = !row.enabled
  }
}

// 删除主动指纹
async function handleDeleteActiveFingerprint(row) {
  await ElMessageBox.confirm('确定删除该主动指纹吗？', '提示', { type: 'warning' })
  const res = await deleteActiveFingerprint({ id: row.id })
  if (res.code === 0) {
    ElMessage.success('删除成功')
    loadActiveFingerprints()
  } else {
    ElMessage.error(res.msg || '删除失败')
  }
}

// 清空主动指纹
async function handleClearActiveFingerprints() {
  try {
    await ElMessageBox.confirm(
      '确定要清空所有主动扫描指纹吗？此操作不可恢复！',
      '警告',
      {
        type: 'warning',
        confirmButtonText: '确定清空',
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger'
      }
    )
    
    const res = await clearActiveFingerprints()
    if (res.code === 0) {
      ElMessage.success(`已清空 ${res.deleted || 0} 个主动指纹`)
      loadActiveFingerprints()
    } else {
      ElMessage.error(res.msg || '清空失败')
    }
  } catch {
    // 用户取消
  }
}

// 显示主动指纹导入对话框
function showActiveImportDialog() {
  activeImportContent.value = ''
  activeImportFileName.value = ''
  if (activeUploadRef.value) {
    activeUploadRef.value.clearFiles()
  }
  activeImportDialogVisible.value = true
}

// 处理主动指纹文件选择
function handleActiveFileChange(file) {
  if (!file || !file.raw) return
  
  const reader = new FileReader()
  reader.onload = (e) => {
    let content = e.target.result
    // 去除BOM头
    if (content.charCodeAt(0) === 0xFEFF) {
      content = content.slice(1)
    }
    activeImportContent.value = content
    activeImportFileName.value = file.name
  }
  reader.onerror = () => {
    ElMessage.error('文件读取失败')
  }
  reader.readAsText(file.raw, 'UTF-8')
}

// 清除主动指纹导入文件
function clearActiveImportFile() {
  activeImportContent.value = ''
  activeImportFileName.value = ''
  if (activeUploadRef.value) {
    activeUploadRef.value.clearFiles()
  }
}

// 导入主动指纹
async function handleImportActiveFingerprints() {
  if (!activeImportContent.value.trim()) {
    ElMessage.warning('请先选择文件')
    return
  }
  
  activeImportLoading.value = true
  try {
    const res = await importActiveFingerprints({
      content: activeImportContent.value
    })
    
    if (res.code === 0) {
      activeImportDialogVisible.value = false
      ElMessageBox.alert(
        `<div style="text-align: center; font-size: 14px;">
          <p style="margin-bottom: 10px;">导入完成</p>
          <p><strong style="color: #67c23a; font-size: 20px;">${res.imported || 0}</strong> 个指纹新增</p>
          <p><strong style="color: #e6a23c; font-size: 20px;">${res.updated || 0}</strong> 个指纹更新</p>
        </div>`,
        '导入结果',
        {
          dangerouslyUseHTMLString: true,
          confirmButtonText: '确定',
          type: 'success'
        }
      )
      loadActiveFingerprints()
    } else {
      ElMessage.error(res.msg || '导入失败')
    }
  } catch (err) {
    ElMessage.error('导入请求失败: ' + (err.message || '未知错误'))
  } finally {
    activeImportLoading.value = false
  }
}

// 导出主动指纹
async function handleExportActiveFingerprints() {
  if (activeFingerprintStats.value.total === 0) {
    ElMessage.warning('没有可导出的指纹')
    return
  }
  
  activeExportLoading.value = true
  try {
    const res = await exportActiveFingerprints()
    if (res.code === 0 && res.content) {
      const blob = new Blob([res.content], { type: 'text/yaml;charset=utf-8' })
      const dateStr = new Date().toISOString().slice(0, 10)
      saveAs(blob, `active-fingerprints-${dateStr}.yaml`)
      ElMessage.success('导出成功')
    } else {
      ElMessage.error(res.msg || '导出失败')
    }
  } catch (err) {
    ElMessage.error('导出请求失败: ' + (err.message || '未知错误'))
  } finally {
    activeExportLoading.value = false
  }
}

// 显示主动指纹验证对话框
function showActiveValidateDialog(row) {
  activeValidateFingerprint.value = row
  activeValidateUrl.value = ''
  activeValidateResult.value = null
  activeValidateDialogVisible.value = true
}

// 执行主动指纹验证
async function handleActiveValidateFingerprint() {
  if (!activeValidateUrl.value) {
    ElMessage.warning('请输入目标URL')
    return
  }

  activeValidateLoading.value = true
  activeValidateResult.value = null

  try {
    const res = await validateActiveFingerprint({
      id: activeValidateFingerprint.value.id,
      url: activeValidateUrl.value
    })

    if (res.code === 0) {
      activeValidateResult.value = {
        matched: res.matched,
        results: res.results || []
      }
    } else {
      ElMessage.error(res.msg || '验证失败')
    }
  } catch (e) {
    ElMessage.error('验证请求失败: ' + e.message)
  } finally {
    activeValidateLoading.value = false
  }
}
</script>

<style lang="scss" scoped>
.fingerprint-page {
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .tip-text {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    margin-bottom: 15px;
    padding: 10px 12px;
    background: var(--el-fill-color-light);
    border-radius: 4px;
    border-left: 3px solid var(--el-color-primary-light-5);
    line-height: 1.6;
  }

  .filter-form {
    margin-bottom: 15px;
  }

  .stats-bar {
    margin-bottom: 15px;
    display: flex;
    gap: 10px;
  }

  .pagination {
    margin-top: 20px;
    justify-content: flex-end;
  }

  .rule-section {
    margin-bottom: 15px;
    
    .rule-title {
      font-weight: bold;
      margin-bottom: 8px;
      color: #606266;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    
    .rule-content {
      background: #1e1e1e;
      color: #d4d4d4;
      padding: 12px;
      border-radius: 4px;
      font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
      font-size: 13px;
      white-space: pre-wrap;
      word-break: break-all;
      max-height: 200px;
      overflow-y: auto;
      margin: 0;
      border: 1px solid #3c3c3c;
    }
  }

  .match-logic-tip {
    margin-bottom: 15px;
  }

  .rules-container {
    max-height: 450px;
    overflow-y: auto;
    padding-right: 10px;
  }

  .no-rules {
    padding: 20px 0;
  }

  .rule-help {
    margin-top: 10px;
    padding: 10px;
    background: var(--el-fill-color-light);
    border-radius: 4px;
    font-size: 12px;
    color: var(--el-text-color-regular);

    p {
      margin: 0 0 5px 0;
      font-weight: bold;
    }

    ul {
      margin: 0;
      padding-left: 20px;
    }

    li {
      margin: 3px 0;
    }

    code {
      background: var(--el-fill-color);
      padding: 1px 4px;
      border-radius: 3px;
      font-family: 'Consolas', 'Monaco', monospace;
    }
  }

  .form-tip {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    margin-top: 5px;
    line-height: 1.5;
  }

  .file-preview {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    .preview-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 8px 12px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
      font-size: 13px;
      color: var(--el-text-color-regular);
    }

    .preview-content {
      margin: 0;
      padding: 10px 12px;
      max-height: 200px;
      overflow-y: auto;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 12px;
      background: var(--el-fill-color-light);
      color: var(--el-text-color-regular);
      white-space: pre-wrap;
      word-break: break-all;
    }
  }

  .fingerprint-id {
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 12px;
    color: #409eff;
    cursor: pointer;
    
    &:hover {
      text-decoration: underline;
    }
  }

  .validate-result {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    &.matched {
      border-color: #67c23a;
    }

    .result-header {
      padding: 10px 15px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
    }

    .result-details {
      margin: 0;
      padding: 12px 15px;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 13px;
      background: #1e1e1e;
      color: #d4d4d4;
      white-space: pre-wrap;
      word-break: break-all;
      max-height: 300px;
      overflow-y: auto;

      :deep(.matched-condition) {
        color: #f56c6c;
        font-weight: bold;
      }
    }
  }

  .batch-validate-result {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    .result-header {
      padding: 10px 15px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
    }

    .matched-list {
      padding: 10px;
    }

    .no-match {
      padding: 20px;
    }
  }

  .match-assets-tip {
    margin-bottom: 15px;
  }

  .match-assets-result {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    .result-header {
      padding: 10px 15px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
    }

    .matched-list {
      padding: 10px;
    }

    .no-match {
      padding: 20px;
    }
  }

  .paths-preview {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
  }

  .paths-list {
    padding: 10px;
    background: var(--el-fill-color-light);
    border-radius: 4px;
    max-height: 200px;
    overflow-y: auto;
  }

  .no-related {
    padding: 20px 0;
  }

  .passive-tip {
    margin-bottom: 15px;
  }

  .related-passive-list {
    margin-bottom: 10px;
  }

  .no-passive {
    padding: 10px 0;
  }
}
</style>
