<template>
  <div class="poc-page">
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- Nuclei默认模板 -->
      <el-tab-pane label="默认模板" name="nucleiTemplates">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>Nuclei 默认模板库</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ templateStats.total || 0 }} 个模板
              </span>
              <el-dropdown style="margin-left: auto" @command="handleSyncCommand">
                <el-button type="primary" size="small" :loading="syncLoading">
                  <el-icon><Refresh /></el-icon>同步模板<el-icon class="el-icon--right"><arrow-down /></el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="local">从本地文件夹导入</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
              <el-button type="danger" size="small" plain style="margin-left: 10px" @click="handleClearTemplates">
                清空模板
              </el-button>
              <input 
                ref="folderInputRef" 
                type="file" 
                webkitdirectory 
                directory 
                multiple 
                style="display: none" 
                @change="handleFolderSelect"
              />
            </div>
          </template>
          <p class="tip-text">
            Nuclei 模板库，支持从本地 nuclei-templates 文件夹导入模板。扫描时将根据任务配置的严重级别从数据库加载模板。
          </p>
          <!-- 筛选条件 -->
          <el-form :inline="true" class="filter-form">
            <el-form-item label="分类">
              <el-select v-model="templateFilter.category" placeholder="全部分类" clearable style="width: 150px" @change="loadNucleiTemplates">
                <el-option v-for="cat in templateCategories" :key="cat" :label="cat" :value="cat" />
              </el-select>
            </el-form-item>
            <el-form-item label="级别">
              <el-select v-model="templateFilter.severity" placeholder="全部级别" clearable style="width: 120px" @change="loadNucleiTemplates">
                <el-option label="Critical" value="critical" />
                <el-option label="High" value="high" />
                <el-option label="Medium" value="medium" />
                <el-option label="Low" value="low" />
                <el-option label="Info" value="info" />
                <el-option label="Unknown" value="unknown" />
              </el-select>
            </el-form-item>
            <el-form-item label="标签">
              <el-input v-model="templateFilter.tag" placeholder="输入标签" clearable style="width: 150px" @keyup.enter="loadNucleiTemplates" />
            </el-form-item>
            <el-form-item label="搜索">
              <el-input v-model="templateFilter.keyword" placeholder="名称/ID/描述" clearable style="width: 180px" @keyup.enter="loadNucleiTemplates" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="loadNucleiTemplates">搜索</el-button>
            </el-form-item>
          </el-form>
          <!-- 统计信息和批量操作 -->
          <div class="stats-bar" v-if="templateStats.total">
            <el-tag type="danger" size="small">Critical: {{ templateStats.critical || 0 }}</el-tag>
            <el-tag type="warning" size="small">High: {{ templateStats.high || 0 }}</el-tag>
            <el-tag size="small">Medium: {{ templateStats.medium || 0 }}</el-tag>
            <el-tag type="info" size="small">Low: {{ templateStats.low || 0 }}</el-tag>
            <el-tag type="success" size="small">Info: {{ templateStats.info || 0 }}</el-tag>
            <el-button 
              v-if="selectedTemplates.length > 0" 
              type="success" 
              size="small" 
              style="margin-left: 20px"
              @click="showTemplateBatchValidateDialog"
            >
              批量验证 ({{ selectedTemplates.length }})
            </el-button>
          </div>
          <!-- 模板列表 -->
          <el-table 
            :data="nucleiTemplates" 
            stripe 
            v-loading="nucleiTemplateLoading" 
            max-height="500"
            @selection-change="handleTemplateSelectionChange"
          >
            <el-table-column type="selection" width="45" />
            <el-table-column prop="id" label="模板ID" width="200" show-overflow-tooltip />
            <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
            <el-table-column prop="severity" label="级别" width="90">
              <template #default="{ row }">
                <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="category" label="分类" width="100" />
            <el-table-column prop="tags" label="标签" min-width="180">
              <template #default="{ row }">
                <el-tag v-for="tag in (row.tags || [])" :key="tag" size="small" style="margin-right: 3px">
                  {{ tag }}
                </el-tag>
                <span v-if="row.tags && row.tags.length > 4" style="color: #909399">+{{ row.tags.length - 4 }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="author" label="作者" width="100" show-overflow-tooltip />
            <el-table-column label="操作" width="120" fixed="right">
              <template #default="{ row }">
                <el-button type="success" link size="small" @click="showTemplateValidateDialog(row)">验证</el-button>
                <el-button type="primary" link size="small" @click="showTemplateContent(row)">查看</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="templatePagination.page"
            v-model:page-size="templatePagination.pageSize"
            :total="templatePagination.total"
            :page-sizes="[50, 100, 200]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadNucleiTemplates"
            @current-change="loadNucleiTemplates"
          />
        </el-card>
      </el-tab-pane>

      <!-- 标签映射 -->
      <el-tab-pane label="标签映射" name="tagMapping">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>应用标签映射配置</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ tagMappings.length || 0 }} 条映射
              </span>
              <el-button type="primary" size="small" style="margin-left: auto" @click="showTagMappingForm()">
                <el-icon><Plus /></el-icon>新增映射
              </el-button>
            </div>
          </template>
          <p class="tip-text">
            配置 Wappalyzer 识别的应用名称与 Nuclei 标签的映射关系，扫描时会根据识别到的应用自动选择对应的 POC 进行检测。
          </p>
          <el-table :data="tagMappings" stripe v-loading="tagMappingLoading" max-height="500">
            <el-table-column prop="appName" label="应用名称" width="180" />
            <el-table-column prop="nucleiTags" label="POC标签（Tag）" min-width="250">
              <template #default="{ row }">
                <el-tag v-for="tag in row.nucleiTags" :key="tag" size="small" style="margin-right: 5px">
                  {{ tag }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="description" label="描述" min-width="150" />
            <el-table-column prop="enabled" label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? '启用' : '禁用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="showTagMappingForm(row)">编辑</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteTagMapping(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <!-- 自定义POC -->
      <el-tab-pane label="自定义POC" name="customPoc">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>自定义 Nuclei POC</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ pocPagination.total || 0 }} 个POC
              </span>
              <div style="margin-left: auto">
                <el-button type="danger" size="small" @click="handleClearAllPocs" :loading="clearPocLoading" style="margin-right: 10px">
                  <el-icon><Delete /></el-icon>清空
                </el-button>
                <el-button type="warning" size="small" @click="handleExportPocs" :loading="exportPocLoading" style="margin-right: 10px">
                  <el-icon><Download /></el-icon>导出POC
                </el-button>
                <el-button type="success" size="small" @click="showImportPocDialog" style="margin-right: 10px">
                  <el-icon><Upload /></el-icon>导入POC
                </el-button>
                <el-button type="primary" size="small" @click="showCustomPocForm()">
                  <el-icon><Plus /></el-icon>新增POC
                </el-button>
              </div>
            </div>
          </template>
          <!-- 筛选条件 -->
          <el-form :inline="true" class="filter-form">
            <el-form-item label="名称">
              <el-input v-model="customPocFilter.name" placeholder="POC名称" clearable style="width: 150px" @keyup.enter="loadCustomPocs" />
            </el-form-item>
            <el-form-item label="模板ID">
              <el-input v-model="customPocFilter.templateId" placeholder="模板ID" clearable style="width: 150px" @keyup.enter="loadCustomPocs" />
            </el-form-item>
            <el-form-item label="级别">
              <el-select v-model="customPocFilter.severity" placeholder="全部级别" clearable style="width: 120px" @change="loadCustomPocs">
                <el-option label="Critical" value="critical" />
                <el-option label="High" value="high" />
                <el-option label="Medium" value="medium" />
                <el-option label="Low" value="low" />
                <el-option label="Info" value="info" />
                <el-option label="Unknown" value="unknown" />
              </el-select>
            </el-form-item>
            <el-form-item label="标签">
              <el-input v-model="customPocFilter.tag" placeholder="输入标签" clearable style="width: 120px" @keyup.enter="loadCustomPocs" />
            </el-form-item>
            <el-form-item label="状态">
              <el-select v-model="customPocFilter.enabled" placeholder="全部状态" clearable style="width: 100px" @change="loadCustomPocs">
                <el-option label="启用" :value="true" />
                <el-option label="禁用" :value="false" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="loadCustomPocs">搜索</el-button>
              <el-button @click="resetCustomPocFilter">重置</el-button>
            </el-form-item>
          </el-form>
          <el-table :data="customPocs" stripe v-loading="customPocLoading" max-height="500">
            <el-table-column prop="name" label="名称" width="250" />
            <el-table-column prop="templateId" label="模板ID" width="250" />
            <el-table-column prop="severity" label="严重级别" width="100">
              <template #default="{ row }">
                <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="tags" label="标签" min-width="200">
              <template #default="{ row }">
                <el-tag v-for="tag in row.tags" :key="tag" size="small" style="margin-right: 5px">
                  {{ tag }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? '启用' : '禁用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="300">
              <template #default="{ row }">
                <el-button type="success" link size="small" @click="showPocValidateDialog(row)">验证</el-button>
                <el-button type="warning" link size="small" @click="showScanAssetsDialog(row)">扫描资产</el-button>
                <el-button type="primary" link size="small" @click="showCustomPocForm(row)">编辑</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteCustomPoc(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="pocPagination.page"
            v-model:page-size="pocPagination.pageSize"
            :total="pocPagination.total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadCustomPocs"
            @current-change="loadCustomPocs"
          />
        </el-card>
      </el-tab-pane>

      <!-- 目录扫描字典 -->
      <el-tab-pane label="目录扫描字典" name="dirscanDict">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>目录扫描字典管理</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ dirscanDictPagination.total || 0 }} 个字典
              </span>
              <div style="margin-left: auto">
                <el-button type="danger" size="small" @click="handleClearDirscanDict" :loading="clearDictLoading" style="margin-right: 10px">
                  <el-icon><Delete /></el-icon>清空自定义
                </el-button>
                <el-button type="primary" size="small" @click="showDirscanDictForm()">
                  <el-icon><Plus /></el-icon>新增字典
                </el-button>
              </div>
            </div>
          </template>
          <p class="tip-text">
            管理目录扫描使用的字典文件，支持自定义路径列表。创建任务时可选择要使用的字典进行目录扫描。
          </p>
          <el-table :data="dirscanDicts" stripe v-loading="dirscanDictLoading" max-height="500">
            <el-table-column prop="name" label="字典名称" width="200" />
            <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
            <el-table-column prop="pathCount" label="路径数量" width="100" />
            <el-table-column prop="isBuiltin" label="类型" width="80">
              <template #default="{ row }">
                <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">
                  {{ row.isBuiltin ? '内置' : '自定义' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? '启用' : '禁用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="150">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="showDirscanDictForm(row)">编辑</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteDirscanDict(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="dirscanDictPagination.page"
            v-model:page-size="dirscanDictPagination.pageSize"
            :total="dirscanDictPagination.total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadDirscanDicts"
            @current-change="loadDirscanDicts"
          />
        </el-card>
      </el-tab-pane>

      <!-- 子域名字典 -->
      <el-tab-pane label="子域名字典" name="subdomainDict">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>子域名字典管理</span>
              <span style="color: #909399; font-size: 13px; margin-left: 10px">
                共 {{ subdomainDictPagination.total || 0 }} 个字典
              </span>
              <div style="margin-left: auto">
                <el-button type="danger" size="small" @click="handleClearSubdomainDict" :loading="clearSubdomainDictLoading" style="margin-right: 10px">
                  <el-icon><Delete /></el-icon>清空自定义
                </el-button>
                <el-button type="primary" size="small" @click="showSubdomainDictForm()">
                  <el-icon><Plus /></el-icon>新增字典
                </el-button>
              </div>
            </div>
          </template>
          <p class="tip-text">
            管理子域名暴力破解使用的字典文件，支持自定义子域名前缀列表。创建任务时可选择要使用的字典进行子域名枚举。
          </p>
          <el-table :data="subdomainDicts" stripe v-loading="subdomainDictLoading" max-height="500">
            <el-table-column prop="name" label="字典名称" width="200" />
            <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
            <el-table-column prop="wordCount" label="词条数量" width="100" />
            <el-table-column prop="isBuiltin" label="类型" width="80">
              <template #default="{ row }">
                <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">
                  {{ row.isBuiltin ? '内置' : '自定义' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? '启用' : '禁用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="150">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="showSubdomainDictForm(row)">编辑</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteSubdomainDict(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="subdomainDictPagination.page"
            v-model:page-size="subdomainDictPagination.pageSize"
            :total="subdomainDictPagination.total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadSubdomainDicts"
            @current-change="loadSubdomainDicts"
          />
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- 目录扫描字典编辑对话框 -->
    <el-dialog v-model="dirscanDictDialogVisible" :title="dirscanDictForm.id ? '编辑字典' : '新增字典'" width="700px">
      <el-form ref="dirscanDictFormRef" :model="dirscanDictForm" :rules="dirscanDictRules" label-width="100px">
        <el-form-item label="字典名称" prop="name">
          <el-input v-model="dirscanDictForm.name" placeholder="输入字典名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="dirscanDictForm.description" placeholder="可选描述" />
        </el-form-item>
        <el-form-item label="路径列表" prop="content">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; color: #909399; font-size: 12px">
              每行一个路径，以 / 开头，支持 # 开头的注释行
            </div>
            <el-input
              v-model="dirscanDictForm.content"
              type="textarea"
              :rows="15"
              placeholder="/admin&#10;/login&#10;/api&#10;/backup&#10;/.git&#10;/config"
            />
            <div style="margin-top: 8px; color: #909399; font-size: 12px">
              当前路径数量: {{ countDictPaths(dirscanDictForm.content) }}
            </div>
          </div>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="dirscanDictForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dirscanDictDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveDirscanDict">保存</el-button>
      </template>
    </el-dialog>

    <!-- 子域名字典编辑对话框 -->
    <el-dialog v-model="subdomainDictDialogVisible" :title="subdomainDictForm.id ? '编辑字典' : '新增字典'" width="700px">
      <el-form ref="subdomainDictFormRef" :model="subdomainDictForm" :rules="subdomainDictRules" label-width="100px">
        <el-form-item label="字典名称" prop="name">
          <el-input v-model="subdomainDictForm.name" placeholder="输入字典名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="subdomainDictForm.description" placeholder="可选描述" />
        </el-form-item>
        <el-form-item label="词条列表" prop="content">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; color: #909399; font-size: 12px">
              每行一个子域名前缀，支持 # 开头的注释行
            </div>
            <el-input
              v-model="subdomainDictForm.content"
              type="textarea"
              :rows="15"
              placeholder="www&#10;mail&#10;ftp&#10;admin&#10;api&#10;dev&#10;test"
            />
            <div style="margin-top: 8px; color: #909399; font-size: 12px">
              当前词条数量: {{ countSubdomainWords(subdomainDictForm.content) }}
            </div>
          </div>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="subdomainDictForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="subdomainDictDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveSubdomainDict">保存</el-button>
      </template>
    </el-dialog>

    <!-- 标签映射编辑对话框 -->
    <el-dialog v-model="tagMappingDialogVisible" :title="tagMappingForm.id ? '编辑映射' : '新增映射'" width="500px">
      <el-form ref="tagMappingFormRef" :model="tagMappingForm" :rules="tagMappingRules" label-width="100px">
        <el-form-item label="应用名称" prop="appName">
          <el-input v-model="tagMappingForm.appName" placeholder="Wappalyzer识别的应用名称，如: WordPress" />
        </el-form-item>
        <el-form-item label="Nuclei标签" prop="nucleiTagsInput">
          <el-input 
            v-model="tagMappingForm.nucleiTagsInput" 
            placeholder="输入Nuclei标签，多个用逗号分隔，如: wordpress,wp-plugin,cve"
            style="width: 100%"
          />
          <div style="color: #909399; font-size: 12px; margin-top: 4px;">
            常用标签: wordpress, apache, nginx, php, java, cve, rce, sqli, xss, lfi
          </div>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="tagMappingForm.description" placeholder="可选描述" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="tagMappingForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tagMappingDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveTagMapping">保存</el-button>
      </template>
    </el-dialog>

    <!-- 自定义POC编辑对话框 -->
    <el-dialog v-model="customPocDialogVisible" :title="customPocForm.id ? '编辑POC' : '新增POC'" width="900px">
      <el-form ref="customPocFormRef" :model="customPocForm" :rules="customPocRules" label-width="100px">
        <el-form-item label="YAML内容" prop="content">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; display: flex; justify-content: space-between; align-items: center;">
              <span style="color: #909399; font-size: 12px">粘贴或编辑 Nuclei YAML 模板，下方字段将自动从 YAML 中解析</span>
              <el-button type="primary" size="small" @click="showAiAssistDialog" :icon="MagicStick">AI辅助</el-button>
            </div>
            <div class="yaml-editor-wrapper">
              <el-input
                v-model="customPocForm.content"
                type="textarea"
                :rows="18"
                placeholder="Nuclei YAML模板内容"
                @input="parseYamlContent"
              />
            </div>
          </div>
        </el-form-item>
        <el-divider content-position="left">解析结果（自动从YAML提取，可手动修改）</el-divider>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="模板ID" prop="templateId">
              <el-input v-model="customPocForm.templateId" placeholder="从YAML的id字段解析" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="名称" prop="name">
              <el-input v-model="customPocForm.name" placeholder="从YAML的info.name解析" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="严重级别" prop="severity">
              <el-select v-model="customPocForm.severity" style="width: 100%">
                <el-option label="Critical" value="critical" />
                <el-option label="High" value="high" />
                <el-option label="Medium" value="medium" />
                <el-option label="Low" value="low" />
                <el-option label="Info" value="info" />
                <el-option label="Unknown" value="unknown" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="作者">
              <el-input v-model="customPocForm.author" placeholder="从YAML的info.author解析" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="标签">
          <el-input 
            v-model="customPocForm.tagsInput" 
            placeholder="从YAML的info.tags解析，多个用逗号分隔"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="customPocForm.description" type="textarea" :rows="2" placeholder="从YAML的info.description解析" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="customPocForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="customPocDialogVisible = false">取消</el-button>
        <el-button @click="parseYamlContent">重新解析YAML</el-button>
        <el-button type="success" @click="handleValidatePocSyntax" :loading="syntaxValidating">验证语法</el-button>
        <el-button type="primary" @click="handleSaveCustomPoc">保存</el-button>
      </template>
    </el-dialog>

    <!-- AI辅助编写POC对话框 -->
    <el-dialog v-model="aiAssistDialogVisible" title="AI辅助编写POC" width="700px">
      <!-- AI配置折叠面板 -->
      <el-collapse v-model="aiConfigCollapse" style="margin-bottom: 15px;">
        <el-collapse-item title="AI服务配置" name="config">
          <el-form label-width="100px" size="small">
            <el-form-item label="协议类型">
              <el-radio-group v-model="aiConfig.protocol">
                <el-radio-button value="openai">OpenAI</el-radio-button>
                <el-radio-button value="anthropic">Anthropic</el-radio-button>
                <el-radio-button value="gemini">Gemini</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="服务地址">
              <el-input v-model="aiConfig.baseUrl" placeholder="http://127.0.0.1:8045" />
              <div style="color: #909399; font-size: 12px; margin-top: 4px;">
                {{ aiConfig.protocol === 'openai' ? 'OpenAI: /v1/chat/completions' : aiConfig.protocol === 'anthropic' ? 'Anthropic: /v1/messages' : 'Gemini: /v1beta/models/...' }}
              </div>
            </el-form-item>
            <el-form-item label="API Key">
              <el-input v-model="aiConfig.apiKey" placeholder="API密钥" show-password />
            </el-form-item>
            <el-form-item label="模型">
              <el-select v-model="aiConfig.model" placeholder="选择模型" style="width: 100%" allow-create filterable>
                <el-option label="gemini-2.5-flash" value="gemini-2.5-flash" />
                <el-option label="gemini-2.5-pro" value="gemini-2.5-pro" />
                <el-option label="claude-sonnet-4-20250514" value="claude-sonnet-4-20250514" />
                <el-option label="claude-3-5-sonnet-20241022" value="claude-3-5-sonnet-20241022" />
                <el-option label="gpt-4o" value="gpt-4o" />
                <el-option label="gpt-4o-mini" value="gpt-4o-mini" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="small" @click="saveAiConfig" :loading="aiSaving">保存配置</el-button>
              <el-button size="small" @click="testAiConnection" :loading="aiTesting">测试连接</el-button>
            </el-form-item>
          </el-form>
        </el-collapse-item>
      </el-collapse>

      <el-form label-width="100px">
        <el-form-item label="漏洞描述">
          <el-input
            v-model="aiAssistForm.description"
            type="textarea"
            :rows="4"
            placeholder="请描述要检测的漏洞，例如：&#10;• 检测目标是否存在SQL注入漏洞，路径为/api/search，参数为keyword&#10;• 检测Apache Tomcat CVE-2020-1938 AJP文件读取漏洞&#10;• 检测目标是否存在未授权访问，路径为/admin"
          />
        </el-form-item>
        <el-form-item label="漏洞类型">
          <el-select v-model="aiAssistForm.vulnType" placeholder="选择漏洞类型" style="width: 100%">
            <el-option label="SQL注入" value="sqli" />
            <el-option label="XSS跨站脚本" value="xss" />
            <el-option label="命令注入" value="rce" />
            <el-option label="文件包含/读取" value="lfi" />
            <el-option label="SSRF" value="ssrf" />
            <el-option label="未授权访问" value="unauth" />
            <el-option label="信息泄露" value="info-disclosure" />
            <el-option label="CVE漏洞" value="cve" />
            <el-option label="其他" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item label="CVE编号" v-if="aiAssistForm.vulnType === 'cve'">
          <el-input v-model="aiAssistForm.cveId" placeholder="例如：CVE-2021-44228" />
        </el-form-item>
        <el-form-item label="参考信息">
          <el-input
            v-model="aiAssistForm.reference"
            type="textarea"
            :rows="2"
            placeholder="可选：提供漏洞的参考链接、PoC代码片段等"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="aiAssistDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="generatePocWithAi" :loading="aiGenerating">生成POC</el-button>
      </template>
    </el-dialog>

    <!-- 导入POC对话框 -->
    <el-dialog v-model="importPocDialogVisible" title="导入POC" width="900px">
      <el-form label-width="100px">
        <el-form-item label="POC格式">
          <el-radio-group v-model="importPocFormat" @change="handleImportFormatChange">
            <el-radio-button value="nuclei">Nuclei</el-radio-button>
            <el-radio-button value="xray">XRAY (自动转换)</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="导入方式" v-if="importPocFormat === 'xray'">
          <el-radio-group v-model="importPocType">
            <el-radio-button value="text">文本粘贴</el-radio-button>
            <el-radio-button value="file">文件上传</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <!-- Nuclei格式：从本地文件夹导入 -->
        <el-form-item v-if="importPocFormat === 'nuclei'" label="选择文件夹">
          <div style="width: 100%">
            <el-button type="primary" @click="customPocFolderInputRef?.click()" :loading="importPocLoading">
              <el-icon><FolderOpened /></el-icon>选择本地文件夹
            </el-button>
            <input 
              ref="customPocFolderInputRef" 
              type="file" 
              webkitdirectory 
              directory 
              multiple 
              style="display: none" 
              @change="handleCustomPocFolderSelect"
            />
            <div style="margin-top: 8px; color: #909399; font-size: 12px">
              选择包含 Nuclei YAML 模板的文件夹，将自动扫描所有 .yaml/.yml 文件
            </div>
            <div v-if="uploadedFileCount > 0" style="margin-top: 10px; color: #67c23a; font-size: 13px">
              <el-icon><UploadFilled /></el-icon> 已扫描 {{ uploadedFileCount }} 个文件
            </div>
          </div>
        </el-form-item>
        <!-- XRAY格式：文本粘贴 -->
        <el-form-item v-if="importPocFormat === 'xray' && importPocType === 'text'" label="POC内容">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; color: #909399; font-size: 12px">
              粘贴 XRAY YAML POC 内容，将自动转换为 Nuclei 格式，支持一次导入多个POC（用 --- 分隔）
            </div>
            <div class="yaml-editor-wrapper">
              <el-input
                v-model="importPocContent"
                type="textarea"
                :rows="18"
                placeholder="粘贴 XRAY YAML POC 内容..."
                @input="parseImportContent"
              />
            </div>
          </div>
        </el-form-item>
        <!-- XRAY格式：文件上传 -->
        <el-form-item v-if="importPocFormat === 'xray' && importPocType === 'file'" label="上传文件">
          <div style="width: 100%">
            <el-upload
              ref="importPocUploadRef"
              :auto-upload="false"
              :limit="500"
              accept=".yaml,.yml"
              :on-change="handleImportFileChange"
              :on-remove="handleImportFileRemove"
              :before-upload="() => false"
              multiple
              drag
              :show-file-list="false"
            >
              <el-icon class="el-icon--upload"><upload-filled /></el-icon>
              <div class="el-upload__text">拖拽文件到此处，或 <em>点击上传</em></div>
              <template #tip>
                <div class="el-upload__tip">
                  支持 .yaml / .yml 文件，可批量选择多个文件
                  <span style="color: #e6a23c">（XRAY格式将自动转换为Nuclei格式）</span>
                </div>
              </template>
            </el-upload>
            <div v-if="uploadedFileCount > 0" style="margin-top: 10px; color: #67c23a; font-size: 13px">
              <el-icon><UploadFilled /></el-icon> 已上传 {{ uploadedFileCount }} 个文件
            </div>
          </div>
        </el-form-item>
      </el-form>
      
      <!-- 解析预览（仅XRAY格式显示） -->
      <div v-if="importPocFormat === 'xray' && importPocPreviews.length > 0" class="import-preview">
        <div class="preview-header">
          <span>解析预览 ({{ importPocPreviews.length }} 个POC)</span>
          <el-tag type="warning" size="small" style="margin-left: 10px">已转换为Nuclei格式</el-tag>
          <el-checkbox v-model="importPocEnabled" style="margin-left: 15px">导入后启用</el-checkbox>
        </div>
        <el-table :data="importPocPreviews" max-height="300" size="small">
          <el-table-column prop="templateId" label="模板ID" width="180" show-overflow-tooltip />
          <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
          <el-table-column prop="severity" label="级别" width="90">
            <template #default="{ row }">
              <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="author" label="作者" width="100" show-overflow-tooltip />
          <el-table-column prop="tags" label="标签" min-width="150">
            <template #default="{ row }">
              <el-tag v-for="tag in (row.tags || []).slice(0, 3)" :key="tag" size="small" style="margin-right: 3px">
                {{ tag }}
              </el-tag>
              <span v-if="row.tags && row.tags.length > 3" style="color: #909399">+{{ row.tags.length - 3 }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row, $index }">
              <el-button type="primary" link size="small" @click="previewConvertedPoc(row)">预览</el-button>
              <el-button type="danger" link size="small" @click="removeImportPreview($index)">移除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      
      <template #footer>
        <el-button @click="importPocDialogVisible = false">取消</el-button>
        <el-button v-if="importPocFormat === 'xray'" @click="clearImportContent">清空</el-button>
        <el-button v-if="importPocFormat === 'xray'" type="primary" @click="handleImportPocs" :loading="importPocLoading" :disabled="importPocPreviews.length === 0">
          导入 ({{ importPocPreviews.length }})
        </el-button>
      </template>
    </el-dialog>

    <!-- 预览转换后的POC对话框 -->
    <el-dialog v-model="convertedPocPreviewVisible" title="转换后的POC预览" width="800px">
      <el-input
        v-model="convertedPocPreviewContent"
        type="textarea"
        :rows="25"
        readonly
        style="font-family: 'Consolas', 'Monaco', monospace; font-size: 13px"
      />
      <template #footer>
        <el-button @click="convertedPocPreviewVisible = false">关闭</el-button>
        <el-button type="primary" @click="copyConvertedPoc">复制内容</el-button>
      </template>
    </el-dialog>

    <!-- 查看模板内容对话框 -->
    <el-dialog v-model="templateContentDialogVisible" :title="currentTemplate.name || '模板内容'" width="900px">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item label="模板ID">{{ currentTemplate.id }}</el-descriptions-item>
        <el-descriptions-item label="严重级别">
          <el-tag :type="getSeverityType(currentTemplate.severity)" size="small">{{ currentTemplate.severity }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="分类">{{ currentTemplate.category }}</el-descriptions-item>
        <el-descriptions-item label="作者">{{ currentTemplate.author }}</el-descriptions-item>
        <el-descriptions-item label="标签" :span="2">
          <el-tag v-for="tag in (currentTemplate.tags || [])" :key="tag" size="small" style="margin-right: 5px">{{ tag }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">{{ currentTemplate.description || '-' }}</el-descriptions-item>
      </el-descriptions>
      <div class="template-content-wrapper">
        <el-input
          v-model="currentTemplate.content"
          type="textarea"
          :rows="20"
          readonly
          style="font-family: 'Consolas', 'Monaco', monospace; font-size: 13px"
        />
      </div>
      <template #footer>
        <el-button @click="templateContentDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="copyTemplateContent">复制内容</el-button>
      </template>
    </el-dialog>

    <!-- POC验证对话框 -->
    <el-dialog v-model="pocValidateDialogVisible" title="验证POC" width="700px" @close="handleValidateDialogClose">
      <el-form label-width="80px">
        <el-form-item label="POC名称">
          <el-input :value="validatePoc.name" disabled />
        </el-form-item>
        <el-form-item label="模板ID">
          <el-input :value="validatePoc.templateId" disabled />
        </el-form-item>
        <el-form-item label="目标URL">
          <el-input v-model="pocValidateUrl" placeholder="请输入目标URL，如 https://example.com" />
        </el-form-item>
      </el-form>
      
      <!-- 执行日志区域 -->
      <div v-if="pocValidateLoading || pocValidateLogs.length > 0" class="validate-logs">
        <div class="logs-header">
          <span>执行日志</span>
          <el-tag v-if="pocValidateLoading" type="warning" size="small">执行中...</el-tag>
          <el-tag v-else-if="pocValidateResult && pocValidateResult.matched" type="success" size="small">发现漏洞</el-tag>
          <el-tag v-else-if="pocValidateResult" type="info" size="small">完成</el-tag>
        </div>
        <div class="logs-content" ref="logsContainerRef">
          <div v-for="(log, index) in pocValidateLogs" :key="index" :class="['log-line', 'log-' + log.level.toLowerCase()]">
            <span class="log-time">{{ log.timestamp }}</span>
            <span class="log-level">[{{ log.level }}]</span>
            <span class="log-msg">{{ log.message }}</span>
          </div>
        </div>
      </div>
      
      <!-- 验证结果区域 -->
      <div v-if="pocValidateResult && !pocValidateLoading" class="validate-result">
        <div class="result-header">
          <el-tag :type="pocValidateResult.matched ? 'danger' : 'info'" size="large">
            {{ pocValidateResult.matched ? '✓ 发现漏洞' : '✗ 未发现漏洞' }}
          </el-tag>
          <el-tag :type="getSeverityType(pocValidateResult.severity)" size="small" style="margin-left: 10px">
            {{ pocValidateResult.severity }}
          </el-tag>
        </div>
        <pre class="result-details">{{ pocValidateResult.details }}</pre>
      </div>
      <template #footer>
        <el-button @click="pocValidateDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleValidatePoc" :loading="pocValidateLoading" :disabled="!pocValidateUrl">验证</el-button>
      </template>
    </el-dialog>

    <!-- 默认模板批量验证对话框 -->
    <el-dialog v-model="templateBatchValidateDialogVisible" title="批量验证POC" width="900px" @close="handleBatchValidateDialogClose">
      <el-form label-width="100px">
        <el-form-item label="已选模板">
          <div class="selected-templates">
            <el-tag v-for="tpl in selectedTemplates.slice(0, 10)" :key="tpl.id" size="small" style="margin-right: 5px; margin-bottom: 5px">
              {{ tpl.name || tpl.id }}
            </el-tag>
            <span v-if="selectedTemplates.length > 10" style="color: #909399">+{{ selectedTemplates.length - 10 }} 个</span>
          </div>
        </el-form-item>
        <el-form-item label="目标URL">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 10px;">
              <el-radio-group v-model="batchTargetInputType" size="small">
                <el-radio-button value="text">文本输入</el-radio-button>
                <el-radio-button value="file">文件上传</el-radio-button>
              </el-radio-group>
              <span style="color: #909399; font-size: 12px;">
                {{ batchTargetInputType === 'text' ? '每行一个URL' : '支持 .txt 文件，每行一个URL' }}
              </span>
            </div>
            <el-input 
              v-if="batchTargetInputType === 'text'"
              v-model="templateBatchValidateUrls" 
              type="textarea" 
              :rows="5" 
              placeholder="请输入目标URL，每行一个，如：&#10;https://example1.com&#10;https://example2.com&#10;https://example3.com"
            />
            <el-upload
              v-else
              ref="batchUrlUploadRef"
              :auto-upload="false"
              :limit="1"
              accept=".txt"
              :on-change="handleBatchUrlFileChange"
              :on-remove="handleBatchUrlFileRemove"
              drag
            >
              <el-icon class="el-icon--upload"><upload-filled /></el-icon>
              <div class="el-upload__text">拖拽文件到此处，或 <em>点击上传</em></div>
              <template #tip>
                <div class="el-upload__tip">仅支持 .txt 文件，每行一个URL</div>
              </template>
            </el-upload>
            <div v-if="batchTargetUrls.length > 0" style="margin-top: 8px; color: #67c23a; font-size: 12px;">
              已解析 {{ batchTargetUrls.length }} 个目标URL
            </div>
          </div>
        </el-form-item>
      </el-form>
      
      <!-- 批量验证进度 -->
      <div v-if="templateBatchValidateLoading || templateBatchValidateResults.length > 0" class="batch-validate-progress">
        <div class="progress-header">
          <span>验证进度: {{ templateBatchValidateProgress.completed }}/{{ templateBatchValidateProgress.total }}</span>
          <el-progress 
            :percentage="templateBatchValidateProgress.total > 0 ? Math.round(templateBatchValidateProgress.completed / templateBatchValidateProgress.total * 100) : 0" 
            :status="templateBatchValidateLoading ? '' : 'success'"
            style="width: 200px; margin-left: 15px"
          />
        </div>
        
        <!-- 执行日志 -->
        <div class="logs-content" ref="batchLogsContainerRef" style="max-height: 150px;">
          <div v-for="(log, index) in templateBatchValidateLogs" :key="index" :class="['log-line', 'log-' + log.level.toLowerCase()]">
            <span class="log-time">{{ log.timestamp }}</span>
            <span class="log-level">[{{ log.level }}]</span>
            <span class="log-msg">{{ log.message }}</span>
          </div>
        </div>
      </div>
      
      <!-- 批量验证结果 -->
      <div v-if="templateBatchValidateResults.length > 0" class="batch-validate-results">
        <div class="results-header">
          <span>验证结果</span>
          <el-tag type="danger" size="small" style="margin-left: 10px">
            发现漏洞: {{ templateBatchValidateResults.filter(r => r.matched).length }}
          </el-tag>
          <el-tag type="info" size="small" style="margin-left: 5px">
            未匹配: {{ templateBatchValidateResults.filter(r => !r.matched).length }}
          </el-tag>
          <el-dropdown style="margin-left: auto" @command="handleExportResults">
            <el-button type="success" size="small">
              导出结果<el-icon class="el-icon--right"><arrow-down /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="all">导出全部</el-dropdown-item>
                <el-dropdown-item command="matched">仅导出匹配</el-dropdown-item>
                <el-dropdown-item command="csv">导出CSV</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
        <el-table :data="templateBatchValidateResults" max-height="250" size="small">
          <el-table-column prop="pocName" label="模板名称" min-width="150" show-overflow-tooltip />
          <el-table-column prop="severity" label="级别" width="80">
            <template #default="{ row }">
              <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="matched" label="结果" width="80">
            <template #default="{ row }">
              <el-tag :type="row.matched ? 'danger' : 'info'" size="small">
                {{ row.matched ? '匹配' : '未匹配' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="matchedUrl" label="匹配URL" min-width="200" show-overflow-tooltip />
        </el-table>
      </div>
      
      <template #footer>
        <el-button @click="templateBatchValidateDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleTemplateBatchValidate" :loading="templateBatchValidateLoading" :disabled="batchTargetUrls.length === 0">
          开始验证
        </el-button>
      </template>
    </el-dialog>

    <!-- 扫描现有资产对话框 -->
    <el-dialog v-model="scanAssetsDialogVisible" title="扫描现有资产" width="900px" @close="handleScanAssetsDialogClose">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item label="POC名称">{{ scanAssetsPoc.name }}</el-descriptions-item>
        <el-descriptions-item label="模板ID">{{ scanAssetsPoc.templateId }}</el-descriptions-item>
        <el-descriptions-item label="严重级别">
          <el-tag :type="getSeverityType(scanAssetsPoc.severity)" size="small">{{ scanAssetsPoc.severity }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="标签">
          <el-tag v-for="tag in (scanAssetsPoc.tags || [])" :key="tag" size="small" style="margin-right: 3px">{{ tag }}</el-tag>
        </el-descriptions-item>
      </el-descriptions>
      
      <div v-if="!scanAssetsStarted" class="scan-assets-tip">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            点击"开始扫描"将使用此POC对当前工作空间的所有HTTP资产进行漏洞扫描
          </template>
          <template #default>
            <div style="margin-top: 5px; color: #909399; font-size: 12px">
              扫描任务将异步执行，发现的漏洞会显示在"漏洞管理"页面
            </div>
          </template>
        </el-alert>
      </div>
      
      <!-- 扫描进度 -->
      <div v-if="scanAssetsStarted" class="scan-assets-progress">
        <div class="progress-header">
          <span>扫描进度: {{ scanAssetsProgress.completed }}/{{ scanAssetsProgress.total }}</span>
          <el-progress 
            :percentage="scanAssetsProgress.total > 0 ? Math.round(scanAssetsProgress.completed / scanAssetsProgress.total * 100) : 0" 
            :status="scanAssetsLoading ? '' : 'success'"
            style="width: 200px; margin-left: 15px"
          />
          <el-tag v-if="scanAssetsProgress.vulnCount > 0" type="danger" size="small" style="margin-left: 15px">
            发现漏洞: {{ scanAssetsProgress.vulnCount }}
          </el-tag>
        </div>
        
        <!-- 执行日志 -->
        <div class="validate-logs" style="margin-top: 15px">
          <div class="logs-header">
            <span>执行日志</span>
            <el-tag v-if="scanAssetsLoading" type="warning" size="small">扫描中...</el-tag>
            <el-tag v-else type="success" size="small">扫描完成</el-tag>
          </div>
          <div class="logs-content" ref="scanAssetsLogsRef" style="max-height: 300px;">
            <div v-for="(log, index) in scanAssetsLogs" :key="index" :class="['log-line', 'log-' + log.level.toLowerCase()]">
              <span class="log-time">{{ log.timestamp }}</span>
              <span class="log-level">[{{ log.level }}]</span>
              <span class="log-msg">{{ log.message }}</span>
            </div>
          </div>
        </div>
      </div>
      
      <template #footer>
        <el-button @click="scanAssetsDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleScanAssets" :loading="scanAssetsLoading" :disabled="scanAssetsLoading">
          {{ scanAssetsStarted ? '重新扫描' : '开始扫描' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, ArrowDown, UploadFilled, Upload, Download, Delete, MagicStick, FolderOpened } from '@element-plus/icons-vue'
import { getTagMappingList, saveTagMapping, deleteTagMapping, getCustomPocList, saveCustomPoc, batchImportCustomPoc, deleteCustomPoc, clearAllCustomPoc, getNucleiTemplateList, getNucleiTemplateCategories, syncNucleiTemplates, clearNucleiTemplates, getNucleiTemplateDetail, validatePoc as validatePocApi, getPocValidationResult, scanAssetsWithPoc, getAIConfig, saveAIConfig, validatePocSyntax } from '@/api/poc'
import { getDirScanDictList, saveDirScanDict, deleteDirScanDict, clearDirScanDict } from '@/api/dirscan'
import { getSubdomainDictList, saveSubdomainDict, deleteSubdomainDict, clearSubdomainDict } from '@/api/subdomain'
import jsYaml from 'js-yaml'
import JSZip from 'jszip'
import { saveAs } from 'file-saver'

const route = useRoute()
const router = useRouter()

// 有效的tab名称
const validTabs = ['nucleiTemplates', 'tagMapping', 'customPoc', 'dirscanDict', 'subdomainDict']

// 从URL获取初始tab
const getInitialTab = () => {
  const tab = route.query.tab
  return validTabs.includes(tab) ? tab : 'nucleiTemplates'
}

const activeTab = ref(getInitialTab())

// 监听路由变化，更新activeTab
watch(() => route.query.tab, (newTab) => {
  if (validTabs.includes(newTab) && newTab !== activeTab.value) {
    activeTab.value = newTab
  }
})

// Nuclei默认模板
const nucleiTemplates = ref([])
const nucleiTemplateLoading = ref(false)
const templateCategories = ref([])
const templateTags = ref([])
const selectedTemplates = ref([])
const templateStats = ref({})
const templateFilter = reactive({
  category: '',
  severity: '',
  tag: '',
  keyword: ''
})
const templatePagination = reactive({
  page: 1,
  pageSize: 50,
  total: 0
})
const syncLoading = ref(false)
const folderInputRef = ref(null)
const forceImport = ref(false)
const templateContentDialogVisible = ref(false)
const currentTemplate = ref({})

// 标签映射
const tagMappings = ref([])
const tagMappingLoading = ref(false)
const tagMappingDialogVisible = ref(false)
const tagMappingFormRef = ref()
const tagMappingForm = reactive({
  id: '',
  appName: '',
  nucleiTags: [],
  nucleiTagsInput: '', // 用户输入的逗号分隔标签
  description: '',
  enabled: true
})
const tagMappingRules = {
  appName: [{ required: true, message: '请输入应用名称', trigger: 'blur' }],
  nucleiTagsInput: [{ required: true, message: '请输入Nuclei标签', trigger: 'blur' }]
}

// 自定义POC
const customPocs = ref([])
const customPocLoading = ref(false)
const customPocDialogVisible = ref(false)
const syntaxValidating = ref(false) // 语法验证中

// 目录扫描字典
const dirscanDicts = ref([])
const dirscanDictLoading = ref(false)
const dirscanDictDialogVisible = ref(false)
const dirscanDictFormRef = ref()
const clearDictLoading = ref(false)
const dirscanDictForm = reactive({
  id: '',
  name: '',
  description: '',
  content: '',
  enabled: true
})
const dirscanDictRules = {
  name: [{ required: true, message: '请输入字典名称', trigger: 'blur' }],
  content: [{ required: true, message: '请输入路径列表', trigger: 'blur' }]
}
const dirscanDictPagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// 子域名字典
const subdomainDicts = ref([])
const subdomainDictLoading = ref(false)
const subdomainDictDialogVisible = ref(false)
const subdomainDictFormRef = ref()
const clearSubdomainDictLoading = ref(false)
const subdomainDictForm = reactive({
  id: '',
  name: '',
  description: '',
  content: '',
  enabled: true
})
const subdomainDictRules = {
  name: [{ required: true, message: '请输入字典名称', trigger: 'blur' }],
  content: [{ required: true, message: '请输入词条列表', trigger: 'blur' }]
}
const subdomainDictPagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// AI辅助编写POC
const aiAssistDialogVisible = ref(false)
const aiGenerating = ref(false)
const aiAssistForm = reactive({
  description: '',
  vulnType: '',
  cveId: '',
  reference: ''
})

// AI配置
const aiConfig = reactive({
  protocol: 'anthropic', // openai/anthropic/gemini
  baseUrl: 'http://127.0.0.1:8045',
  apiKey: '',
  model: 'gemini-2.5-flash'
})
const aiConfigCollapse = ref([]) // 折叠面板状态
const aiTesting = ref(false) // 测试连接状态
const aiSaving = ref(false) // 保存配置状态

// 从数据库加载AI配置
async function loadAiConfig() {
  try {
    const res = await getAIConfig()
    if (res.code === 0 && res.data) {
      aiConfig.protocol = res.data.protocol || 'anthropic'
      aiConfig.baseUrl = res.data.baseUrl || 'http://127.0.0.1:8045'
      aiConfig.apiKey = res.data.apiKey || ''
      aiConfig.model = res.data.model || 'gemini-2.5-flash'
    }
  } catch (e) {
    console.error('加载AI配置失败:', e)
  }
}

// 保存AI配置到数据库
async function saveAiConfig() {
  if (!aiConfig.baseUrl) {
    ElMessage.warning('请输入服务地址')
    return
  }
  
  aiSaving.value = true
  try {
    const res = await saveAIConfig({
      protocol: aiConfig.protocol,
      baseUrl: aiConfig.baseUrl,
      apiKey: aiConfig.apiKey,
      model: aiConfig.model
    })
    if (res.code === 0) {
      ElMessage.success('AI配置已保存')
    } else {
      ElMessage.error(res.msg || '保存配置失败')
    }
  } catch (e) {
    console.error('保存AI配置失败:', e)
    ElMessage.error('保存配置失败')
  } finally {
    aiSaving.value = false
  }
}

// 测试AI服务连接
async function testAiConnection() {
  if (!aiConfig.baseUrl) {
    ElMessage.warning('请输入服务地址')
    return
  }
  
  aiTesting.value = true
  try {
    let response
    
    if (aiConfig.protocol === 'openai') {
      // OpenAI 协议
      response = await fetch(`${aiConfig.baseUrl}/v1/chat/completions`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${aiConfig.apiKey}`
        },
        body: JSON.stringify({
          model: aiConfig.model,
          max_tokens: 10,
          messages: [
            { role: 'user', content: 'Hi' }
          ]
        })
      })
    } else if (aiConfig.protocol === 'gemini') {
      // Gemini 协议
      response = await fetch(`${aiConfig.baseUrl}/v1beta/models/${aiConfig.model}:generateContent?key=${aiConfig.apiKey}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          contents: [
            { parts: [{ text: 'Hi' }] }
          ]
        })
      })
    } else {
      // Anthropic 协议 (默认)
      response = await fetch(`${aiConfig.baseUrl}/v1/messages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': aiConfig.apiKey,
          'anthropic-version': '2023-06-01'
        },
        body: JSON.stringify({
          model: aiConfig.model,
          max_tokens: 10,
          messages: [
            { role: 'user', content: 'Hi' }
          ]
        })
      })
    }
    
    if (response.ok) {
      ElMessage.success('连接成功')
    } else {
      const errorText = await response.text()
      ElMessage.error(`连接失败: ${response.status} ${errorText.substring(0, 100)}`)
    }
  } catch (e) {
    if (e.message.includes('Failed to fetch') || e.message.includes('NetworkError')) {
      ElMessage.error('无法连接到AI服务，请确保服务已启动')
    } else {
      ElMessage.error('连接测试失败: ' + e.message)
    }
  } finally {
    aiTesting.value = false
  }
}

// 自定义POC筛选条件
const customPocFilter = reactive({
  name: '',
  templateId: '',
  severity: '',
  tag: '',
  enabled: null
})

// 导入POC
const importPocDialogVisible = ref(false)
const importPocType = ref('text')
const importPocFormat = ref('nuclei') // nuclei 或 xray
const importPocContent = ref('')
const importPocPreviews = ref([])
const importPocEnabled = ref(true)
const importPocLoading = ref(false)
const uploadedFileCount = ref(0)
const importPocUploadRef = ref(null)
const customPocFolderInputRef = ref(null)
const convertedPocPreviewVisible = ref(false)
const convertedPocPreviewContent = ref('')
const exportPocLoading = ref(false)
const clearPocLoading = ref(false)
const customPocFormRef = ref()
const customPocForm = reactive({
  id: '',
  name: '',
  templateId: '',
  severity: 'medium',
  tags: [],
  tagsInput: '', // 用户输入的逗号分隔标签
  author: '',
  description: '',
  content: '',
  enabled: true
})
// AI生成的POC临时保存（关闭对话框后保留，直到生成新的POC或保存成功）
const aiGeneratedPocCache = ref('')
const customPocRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  templateId: [{ required: true, message: '请输入模板ID', trigger: 'blur' }],
  severity: [{ required: true, message: '请选择严重级别', trigger: 'change' }],
  content: [{ required: true, message: '请输入YAML内容', trigger: 'blur' }]
}
const pocPagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// POC验证
const pocValidateDialogVisible = ref(false)
const validatePoc = ref({})
const pocValidateUrl = ref('')
const pocValidateResult = ref(null)
const pocValidateLoading = ref(false)
const pocValidateLogs = ref([])
const logsContainerRef = ref(null)
let logEventSource = null
let currentTaskId = null

// 扫描现有资产
const scanAssetsDialogVisible = ref(false)
const scanAssetsPoc = ref({})
const scanAssetsLoading = ref(false)
const scanAssetsStarted = ref(false)
const scanAssetsLogs = ref([])
const scanAssetsLogsRef = ref(null)
const scanAssetsProgress = reactive({
  total: 0,
  completed: 0,
  vulnCount: 0
})
let scanAssetsTaskIds = []
let scanAssetsEventSource = null
let scanAssetsPollTimer = null

// 默认模板批量验证
const templateBatchValidateDialogVisible = ref(false)
const templateBatchValidateUrls = ref('') // 多行URL文本
const batchTargetInputType = ref('text') // 输入类型: text 或 file
const batchUrlUploadRef = ref(null)
const templateBatchValidateLoading = ref(false)
const templateBatchValidateLogs = ref([])
const templateBatchValidateResults = ref([])
const templateBatchValidateProgress = reactive({ total: 0, completed: 0 })
const batchLogsContainerRef = ref(null)
const currentBatchTaskIds = ref([]) // 当前批次的任务ID列表
let batchLogEventSource = null
let batchPollTimer = null
let currentBatchId = null

// 计算属性：解析多行URL为数组（自动去重）
const batchTargetUrls = computed(() => {
  if (!templateBatchValidateUrls.value) return []
  const urls = templateBatchValidateUrls.value
    .split('\n')
    .map(url => url.trim())
    .filter(url => url && (url.startsWith('http://') || url.startsWith('https://')))
  // 使用Set去重
  return [...new Set(urls)]
})

onMounted(() => {
  // 如果URL没有tab参数，添加默认的tab参数
  if (!route.query.tab) {
    router.replace({ query: { ...route.query, tab: activeTab.value } })
  }
  // 加载AI配置
  loadAiConfig()
  // 根据当前tab加载数据
  handleTabChange(activeTab.value)
})

function handleTabChange(tab) {
  // Tab切换时更新URL
  router.replace({ query: { ...route.query, tab: tab } })
  
  if (tab === 'nucleiTemplates' && nucleiTemplates.value.length === 0) {
    loadNucleiTemplateCategories()
    loadNucleiTemplates()
  } else if (tab === 'tagMapping' && tagMappings.value.length === 0) {
    loadTagMappings()
  } else if (tab === 'customPoc' && customPocs.value.length === 0) {
    loadCustomPocs()
  } else if (tab === 'dirscanDict' && dirscanDicts.value.length === 0) {
    loadDirscanDicts()
  } else if (tab === 'subdomainDict' && subdomainDicts.value.length === 0) {
    loadSubdomainDicts()
  }
}

async function loadNucleiTemplateCategories() {
  try {
    const res = await getNucleiTemplateCategories()
    if (res.code === 0) {
      templateCategories.value = res.categories || []
      templateTags.value = res.tags || []
      templateStats.value = res.stats || {}
    }
  } catch (e) {
    console.error('Failed to load template categories:', e)
  }
}

async function loadNucleiTemplates() {
  nucleiTemplateLoading.value = true
  try {
    const res = await getNucleiTemplateList({
      category: templateFilter.category,
      severity: templateFilter.severity,
      tag: templateFilter.tag,
      keyword: templateFilter.keyword,
      page: templatePagination.page,
      pageSize: templatePagination.pageSize
    })
    if (res.code === 0) {
      nucleiTemplates.value = res.list || []
      templatePagination.total = res.total
    } else {
      ElMessage.error(res.msg || '加载模板失败')
    }
  } finally {
    nucleiTemplateLoading.value = false
  }
}

async function handleSyncCommand(command) {
  if (command === 'local') {
    forceImport.value = false
    folderInputRef.value?.click()
  }
}

// 处理文件夹选择
async function handleFolderSelect(event) {
  const files = event.target.files
  if (!files || files.length === 0) return
  
  // 筛选 .yaml 和 .yml 文件
  const yamlFiles = Array.from(files).filter(file => {
    const name = file.name.toLowerCase()
    return (name.endsWith('.yaml') || name.endsWith('.yml')) && !file.webkitRelativePath.includes('/.git/')
  })
  
  if (yamlFiles.length === 0) {
    ElMessage.warning('未找到有效的模板文件（.yaml/.yml）')
    event.target.value = ''
    return
  }
  
  ElMessage.info(`正在导入 ${yamlFiles.length} 个模板文件...`)
  syncLoading.value = true
  
  try {
    // 读取所有文件内容
    const templates = []
    for (const file of yamlFiles) {
      try {
        const content = await readFileContent(file)
        // 获取相对路径作为模板路径
        const relativePath = file.webkitRelativePath || file.name
        templates.push({
          path: relativePath,
          content: content
        })
      } catch (e) {
        console.error('读取文件失败:', file.name, e)
      }
    }
    
    if (templates.length === 0) {
      ElMessage.error('没有成功读取任何模板文件')
      return
    }
    
    // 分批上传（每批100个）
    const batchSize = 100
    let successCount = 0
    let errorCount = 0
    
    for (let i = 0; i < templates.length; i += batchSize) {
      const batch = templates.slice(i, i + batchSize)
      const isFirstBatch = i === 0
      
      try {
        const res = await syncNucleiTemplates({
          templates: batch,
          force: forceImport.value && isFirstBatch // 只在第一批时清空
        })
        if (res.code === 0) {
          successCount += res.successCount || batch.length
          errorCount += res.errorCount || 0
        } else {
          errorCount += batch.length
        }
      } catch (e) {
        errorCount += batch.length
      }
      
      // 显示进度
      const progress = Math.min(i + batchSize, templates.length)
      ElMessage.info(`导入进度: ${progress}/${templates.length}`)
    }
    
    ElMessage.success(`导入完成！成功: ${successCount}, 失败: ${errorCount}`)
    
    // 刷新数据
    setTimeout(() => {
      loadNucleiTemplateCategories()
      loadNucleiTemplates()
    }, 1000)
    
  } catch (e) {
    ElMessage.error('导入失败: ' + e.message)
  } finally {
    syncLoading.value = false
    event.target.value = '' // 清空input以便重复选择
  }
}

// 读取文件内容
function readFileContent(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = (e) => resolve(e.target.result)
    reader.onerror = (e) => reject(e)
    reader.readAsText(file)
  })
}

// 清空模板
async function handleClearTemplates() {
  try {
    await ElMessageBox.confirm('确定清空所有Nuclei默认模板吗？此操作不可恢复！', '警告', { 
      type: 'error', 
      confirmButtonText: '确定清空', 
      cancelButtonText: '取消' 
    })
  } catch {
    return
  }
  
  syncLoading.value = true
  try {
    const res = await clearNucleiTemplates()
    if (res.code === 0) {
      ElMessage.success(res.msg || '清空成功')
      loadNucleiTemplateCategories()
      loadNucleiTemplates()
    } else {
      ElMessage.error(res.msg || '清空失败')
    }
  } catch (e) {
    ElMessage.error('清空失败: ' + e.message)
  } finally {
    syncLoading.value = false
  }
}

async function showTemplateContent(row) {
  // 需要从API获取完整内容
  const res = await getNucleiTemplateDetail({ templateId: row.id })
  if (res.code === 0 && res.data) {
    currentTemplate.value = res.data
    // 如果内容为空，提示用户强制同步
    if (!res.data.content) {
      currentTemplate.value.content = '# YAML内容为空\n# 请点击"同步模板" -> "从本地文件夹导入"来更新模板内容'
    }
  } else {
    currentTemplate.value = { ...row, content: '加载失败，请重试' }
  }
  templateContentDialogVisible.value = true
}

function copyTemplateContent() {
  if (currentTemplate.value.content) {
    navigator.clipboard.writeText(currentTemplate.value.content)
    ElMessage.success('已复制到剪贴板')
  }
}

async function loadTagMappings() {
  tagMappingLoading.value = true
  try {
    const res = await getTagMappingList()
    if (res.code === 0) {
      tagMappings.value = res.list || []
    }
  } finally {
    tagMappingLoading.value = false
  }
}

async function loadCustomPocs() {
  customPocLoading.value = true
  try {
    const params = {
      page: pocPagination.page,
      pageSize: pocPagination.pageSize
    }
    // 添加筛选条件
    if (customPocFilter.name) {
      params.name = customPocFilter.name
    }
    if (customPocFilter.templateId) {
      params.templateId = customPocFilter.templateId
    }
    if (customPocFilter.severity) {
      params.severity = customPocFilter.severity
    }
    if (customPocFilter.tag) {
      params.tag = customPocFilter.tag
    }
    if (customPocFilter.enabled !== null && customPocFilter.enabled !== '') {
      params.enabled = customPocFilter.enabled
    }
    
    const res = await getCustomPocList(params)
    if (res.code === 0) {
      customPocs.value = res.list || []
      pocPagination.total = res.total
    }
  } finally {
    customPocLoading.value = false
  }
}

// 重置自定义POC筛选条件
function resetCustomPocFilter() {
  customPocFilter.name = ''
  customPocFilter.templateId = ''
  customPocFilter.severity = ''
  customPocFilter.tag = ''
  customPocFilter.enabled = null
  pocPagination.page = 1
  loadCustomPocs()
}

function showTagMappingForm(row = null) {
  if (row) {
    Object.assign(tagMappingForm, {
      id: row.id,
      appName: row.appName,
      nucleiTags: row.nucleiTags || [],
      nucleiTagsInput: (row.nucleiTags || []).join(', '), // 转换为逗号分隔字符串
      description: row.description,
      enabled: row.enabled
    })
  } else {
    Object.assign(tagMappingForm, {
      id: '',
      appName: '',
      nucleiTags: [],
      nucleiTagsInput: '',
      description: '',
      enabled: true
    })
  }
  tagMappingDialogVisible.value = true
}

async function handleSaveTagMapping() {
  await tagMappingFormRef.value.validate()
  // 将逗号分隔的字符串转换为数组
  const tagsArray = tagMappingForm.nucleiTagsInput
    .split(/[,，]/) // 支持中英文逗号
    .map(tag => tag.trim())
    .filter(tag => tag !== '')
  
  const submitData = {
    id: tagMappingForm.id,
    appName: tagMappingForm.appName,
    nucleiTags: tagsArray,
    description: tagMappingForm.description,
    enabled: tagMappingForm.enabled
  }
  
  const res = await saveTagMapping(submitData)
  if (res.code === 0) {
    ElMessage.success('保存成功')
    tagMappingDialogVisible.value = false
    loadTagMappings()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleDeleteTagMapping(row) {
  await ElMessageBox.confirm('确定删除该映射吗？', '提示', { type: 'warning' })
  const res = await deleteTagMapping({ id: row.id })
  if (res.code === 0) {
    ElMessage.success('删除成功')
    loadTagMappings()
  }
}

function showCustomPocForm(row = null) {
  if (row) {
    Object.assign(customPocForm, {
      id: row.id,
      name: row.name,
      templateId: row.templateId,
      severity: row.severity,
      tags: row.tags || [],
      tagsInput: (row.tags || []).join(', '), // 转换为逗号分隔字符串
      author: row.author,
      description: row.description,
      content: row.content,
      enabled: row.enabled
    })
  } else {
    // 新建时检查是否有AI生成的缓存
    const cachedContent = aiGeneratedPocCache.value
    Object.assign(customPocForm, {
      id: '',
      name: '',
      templateId: '',
      severity: 'medium',
      tags: [],
      tagsInput: '',
      author: '',
      description: '',
      content: cachedContent || getNucleiTemplate(),
      enabled: true
    })
    // 自动解析内容
    parseYamlContent()
  }
  customPocDialogVisible.value = true
}

// 验证POC语法
async function handleValidatePocSyntax() {
  if (!customPocForm.content) {
    ElMessage.warning('请先输入POC内容')
    return
  }
  
  syntaxValidating.value = true
  try {
    const res = await validatePocSyntax({ content: customPocForm.content })
    if (res.code === 0) {
      if (res.valid) {
        ElMessage.success('POC语法验证通过')
      } else {
        ElMessage.error('POC语法错误: ' + res.error)
      }
    } else {
      ElMessage.error(res.msg || '验证失败')
    }
  } catch (e) {
    console.error('验证POC语法失败:', e)
    ElMessage.error('验证失败: ' + e.message)
  } finally {
    syntaxValidating.value = false
  }
}

async function handleSaveCustomPoc() {
  await customPocFormRef.value.validate()
  // 将逗号分隔的字符串转换为数组
  const tagsArray = customPocForm.tagsInput
    .split(/[,，]/) // 支持中英文逗号
    .map(tag => tag.trim())
    .filter(tag => tag !== '')
  
  const submitData = {
    id: customPocForm.id,
    name: customPocForm.name,
    templateId: customPocForm.templateId,
    severity: customPocForm.severity,
    tags: tagsArray,
    author: customPocForm.author,
    description: customPocForm.description,
    content: customPocForm.content,
    enabled: customPocForm.enabled
  }
  
  const res = await saveCustomPoc(submitData)
  if (res.code === 0) {
    ElMessage.success('保存成功')
    customPocDialogVisible.value = false
    // 保存成功后清除AI生成的缓存
    aiGeneratedPocCache.value = ''
    loadCustomPocs()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleDeleteCustomPoc(row) {
  await ElMessageBox.confirm('确定删除该POC吗？', '提示', { type: 'warning' })
  const res = await deleteCustomPoc({ id: row.id })
  if (res.code === 0) {
    ElMessage.success('删除成功')
    loadCustomPocs()
  }
}

// ==================== 导出POC相关函数 ====================

// 导出所有自定义POC（每个POC一个文件，打包成ZIP）
async function handleExportPocs() {
  if (customPocs.value.length === 0) {
    ElMessage.warning('没有可导出的POC')
    return
  }
  
  exportPocLoading.value = true
  
  try {
    // 获取所有POC（可能需要分页获取全部）
    let allPocs = []
    
    // 如果当前页数据不是全部，需要获取全部数据
    if (pocPagination.total > customPocs.value.length) {
      const res = await getCustomPocList({ page: 1, pageSize: pocPagination.total })
      if (res.code === 0) {
        allPocs = res.list || []
      } else {
        allPocs = customPocs.value
      }
    } else {
      allPocs = customPocs.value
    }
    
    if (allPocs.length === 0) {
      ElMessage.warning('没有可导出的POC')
      return
    }
    
    // 创建ZIP文件
    const zip = new JSZip()
    
    // 每个POC创建一个单独的文件
    for (const poc of allPocs) {
      // 使用templateId作为文件名，清理非法字符
      const fileName = (poc.templateId || poc.name || 'poc')
        .replace(/[<>:"/\\|?*]/g, '-')
        .replace(/\s+/g, '-')
      zip.file(`${fileName}.yaml`, poc.content)
    }
    
    // 生成ZIP并下载
    const content = await zip.generateAsync({ type: 'blob' })
    const dateStr = new Date().toISOString().slice(0, 10)
    saveAs(content, `custom-pocs-${dateStr}.zip`)
    
    ElMessage.success(`成功导出 ${allPocs.length} 个POC`)
  } catch (e) {
    console.error('Export error:', e)
    ElMessage.error('导出失败')
  } finally {
    exportPocLoading.value = false
  }
}

// 清空自定义POC（按当前筛选条件）
async function handleClearAllPocs() {
  if (customPocs.value.length === 0 && pocPagination.total === 0) {
    ElMessage.warning('没有可清空的POC')
    return
  }
  
  // 检查是否有筛选条件
  const hasFilter = customPocFilter.name || customPocFilter.templateId || customPocFilter.severity || customPocFilter.tag || customPocFilter.enabled !== null
  
  // 构建提示信息
  let confirmMsg = ''
  if (hasFilter) {
    const filterDesc = []
    if (customPocFilter.name) filterDesc.push(`名称含"${customPocFilter.name}"`)
    if (customPocFilter.templateId) filterDesc.push(`模板ID含"${customPocFilter.templateId}"`)
    if (customPocFilter.severity) filterDesc.push(`级别为"${customPocFilter.severity}"`)
    if (customPocFilter.tag) filterDesc.push(`标签含"${customPocFilter.tag}"`)
    if (customPocFilter.enabled === true) filterDesc.push('状态为启用')
    if (customPocFilter.enabled === false) filterDesc.push('状态为禁用')
    confirmMsg = `确定要清空符合条件的POC吗？\n筛选条件：${filterDesc.join('、')}\n共 ${pocPagination.total} 个POC将被删除，此操作不可恢复！`
  } else {
    confirmMsg = `确定要清空所有自定义POC吗？共 ${pocPagination.total} 个POC将被删除，此操作不可恢复！`
  }
  
  try {
    await ElMessageBox.confirm(
      confirmMsg,
      '危险操作',
      {
        type: 'warning',
        confirmButtonText: '确定清空',
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger'
      }
    )
    
    clearPocLoading.value = true
    
    // 传递筛选条件
    const params = {}
    if (customPocFilter.name) params.name = customPocFilter.name
    if (customPocFilter.templateId) params.templateId = customPocFilter.templateId
    if (customPocFilter.severity) params.severity = customPocFilter.severity
    if (customPocFilter.tag) params.tag = customPocFilter.tag
    if (customPocFilter.enabled !== null && customPocFilter.enabled !== '') params.enabled = customPocFilter.enabled
    
    const res = await clearAllCustomPoc(params)
    if (res.code === 0) {
      ElMessage.success(`成功清空 ${res.deleted || pocPagination.total} 个POC`)
      loadCustomPocs()
    } else {
      ElMessage.error(res.msg || '清空失败')
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('Clear error:', e)
      ElMessage.error('清空失败')
    }
  } finally {
    clearPocLoading.value = false
  }
}

// ==================== 导入POC相关函数 ====================

// 显示导入对话框
function showImportPocDialog() {
  importPocType.value = 'text'
  importPocFormat.value = 'nuclei'
  importPocContent.value = ''
  importPocPreviews.value = []
  importPocEnabled.value = true
  uploadedFileCount.value = 0
  importPocDialogVisible.value = true
}

// 处理导入格式切换
function handleImportFormatChange() {
  // 切换格式时清空预览
  importPocPreviews.value = []
  importPocContent.value = ''
  uploadedFileCount.value = 0
  if (importPocUploadRef.value) {
    importPocUploadRef.value.clearFiles()
  }
}

// 处理自定义POC文件夹选择（Nuclei格式，直接导入）
async function handleCustomPocFolderSelect(event) {
  const files = event.target.files
  if (!files || files.length === 0) return
  
  // 筛选 .yaml 和 .yml 文件
  const yamlFiles = Array.from(files).filter(file => {
    const name = file.name.toLowerCase()
    return (name.endsWith('.yaml') || name.endsWith('.yml')) && !file.webkitRelativePath.includes('/.git/')
  })
  
  if (yamlFiles.length === 0) {
    ElMessage.warning('未找到有效的模板文件（.yaml/.yml）')
    event.target.value = ''
    return
  }
  
  ElMessage.info(`正在导入 ${yamlFiles.length} 个模板文件...`)
  uploadedFileCount.value = yamlFiles.length
  importPocLoading.value = true
  
  const pocsToImport = []
  const seenTemplateIds = new Set()
  const seenContents = new Set()
  
  for (const file of yamlFiles) {
    try {
      const content = await readFileContent(file)
      if (!content || content.trim().length === 0) continue
      
      const parsed = parseYamlToPreview(content)
      if (parsed) {
        // 检查是否已存在相同templateId或相同内容
        const contentHash = parsed.content.trim()
        if (!seenTemplateIds.has(parsed.templateId) && !seenContents.has(contentHash)) {
          seenTemplateIds.add(parsed.templateId)
          seenContents.add(contentHash)
          pocsToImport.push({
            name: parsed.name,
            templateId: parsed.templateId,
            severity: parsed.severity,
            tags: parsed.tags,
            author: parsed.author,
            description: parsed.description,
            content: parsed.content,
            enabled: importPocEnabled.value
          })
        }
      }
    } catch (e) {
      console.error('读取文件失败:', file.name, e)
    }
  }
  
  if (pocsToImport.length === 0) {
    ElMessage.warning('未能解析任何有效的POC文件')
    importPocLoading.value = false
    event.target.value = ''
    return
  }
  
  // 直接批量导入
  try {
    const res = await batchImportCustomPoc({ pocs: pocsToImport })
    
    if (res.code === 0) {
      const successCount = res.imported || pocsToImport.length
      const failCount = res.failed || 0
      ElMessage.success(res.msg || `成功导入 ${successCount} 个POC${failCount > 0 ? `，${failCount} 个失败` : ''}`)
      importPocDialogVisible.value = false
      loadCustomPocs()
    } else {
      ElMessage.error(res.msg || '导入失败')
    }
  } catch (e) {
    console.error('批量导入失败:', e)
    ElMessage.error('导入失败: ' + e.message)
  } finally {
    importPocLoading.value = false
    event.target.value = '' // 清空input以便重复选择
  }
}

// 解析导入的YAML内容
function parseImportContent() {
  if (!importPocContent.value.trim()) {
    importPocPreviews.value = []
    return
  }
  
  // 支持多个POC用 --- 分隔
  const yamlDocs = importPocContent.value.split(/\n---\s*\n/)
  const previews = []
  const seenTemplateIds = new Set()
  const seenContents = new Set()
  
  for (const doc of yamlDocs) {
    const trimmedDoc = doc.trim()
    if (!trimmedDoc) continue
    
    let parsed
    if (importPocFormat.value === 'xray') {
      parsed = parseXrayToNuclei(trimmedDoc)
    } else {
      parsed = parseYamlToPreview(trimmedDoc)
    }
    if (parsed) {
      // 检查是否已存在相同templateId或相同内容
      const contentHash = parsed.content.trim()
      if (!seenTemplateIds.has(parsed.templateId) && !seenContents.has(contentHash)) {
        seenTemplateIds.add(parsed.templateId)
        seenContents.add(contentHash)
        previews.push(parsed)
      }
    }
  }
  
  importPocPreviews.value = previews
}

// 解析单个YAML文档为预览对象
function parseYamlToPreview(content) {
  const result = {
    templateId: '',
    name: '',
    author: '',
    severity: 'medium',
    description: '',
    tags: [],
    content: content
  }
  
  // 解析 id 字段
  const idMatch = content.match(/^id:\s*(.+)$/m)
  if (idMatch) {
    result.templateId = idMatch[1].trim()
  }
  
  // 解析 info 块中的字段
  const infoMatch = content.match(/info:\s*\n((?:\s+.+\n?)+)/m)
  if (infoMatch) {
    const infoBlock = infoMatch[1]
    
    // name
    const nameMatch = infoBlock.match(/^\s+name:\s*(.+)$/m)
    if (nameMatch) {
      result.name = nameMatch[1].trim()
    }
    
    // author
    const authorMatch = infoBlock.match(/^\s+author:\s*(.+)$/m)
    if (authorMatch) {
      result.author = authorMatch[1].trim()
    }
    
    // severity
    const severityMatch = infoBlock.match(/^\s+severity:\s*(.+)$/m)
    if (severityMatch) {
      const severity = severityMatch[1].trim().toLowerCase()
      if (['critical', 'high', 'medium', 'low', 'info', 'unknown'].includes(severity)) {
        result.severity = severity
      }
    }
    
    // description (支持多行)
    const descMatch = infoBlock.match(/^\s+description:\s*\|?\s*\n?((?:\s{4,}.+\n?)*)/m)
    if (descMatch && descMatch[1]) {
      result.description = descMatch[1].trim()
    } else {
      const descSimpleMatch = infoBlock.match(/^\s+description:\s*(.+)$/m)
      if (descSimpleMatch) {
        result.description = descSimpleMatch[1].trim()
      }
    }
    
    // tags
    const tagsMatch = infoBlock.match(/^\s+tags:\s*(.+)$/m)
    if (tagsMatch) {
      const tagsStr = tagsMatch[1].trim()
      result.tags = tagsStr.split(',').map(t => t.trim()).filter(t => t)
    }
  }
  
  // 如果没有解析到必要字段，返回null
  if (!result.templateId && !result.name) {
    return null
  }
  
  // 如果没有name，使用templateId
  if (!result.name) {
    result.name = result.templateId
  }
  
  return result
}

// ==================== XRAY POC 转 Nuclei POC ====================

// 解析 XRAY POC 并转换为 Nuclei 格式
function parseXrayToNuclei(xrayContent) {
  try {
    // 解析 XRAY POC 的基本信息
    const xrayPoc = parseXrayPoc(xrayContent)
    if (!xrayPoc) {
      console.warn('Failed to parse XRAY POC')
      return null
    }
    
    // 转换为 Nuclei 格式
    const nucleiContent = convertToNucleiFormat(xrayPoc)
    
    // 从 vulnerability.level 获取严重级别
    const severity = xrayPoc.detail?.vulnerability?.level || xrayPoc.detail?.severity || 'medium'
    
    return {
      templateId: xrayPoc.name || 'xray-converted-poc',
      name: xrayPoc.detail?.name || xrayPoc.name || 'Converted POC',
      author: xrayPoc.detail?.author || 'xray-converter',
      severity: mapXraySeverity(severity),
      description: xrayPoc.detail?.description || '',
      tags: extractXrayTags(xrayPoc),
      content: nucleiContent
    }
  } catch (e) {
    console.error('XRAY to Nuclei conversion error:', e)
    return null
  }
}

// 解析 XRAY POC 结构
function parseXrayPoc(content) {
  const poc = {
    name: '',
    transport: 'http',
    set: {},
    rules: {},
    expression: '',
    detail: {
      author: '',
      links: [],
      vulnerability: { id: '', level: '' }
    }
  }
  
  // 使用 js-yaml 解析更可靠
  try {
    const yaml = jsYaml.load(content)
    if (!yaml) return null
    
    poc.name = yaml.name || ''
    poc.transport = yaml.transport || 'http'
    
    // 解析 set 变量
    if (yaml.set) {
      poc.set = yaml.set
    }
    
    // 解析 detail
    if (yaml.detail) {
      poc.detail.author = yaml.detail.author || ''
      poc.detail.links = yaml.detail.links || []
      if (yaml.detail.vulnerability) {
        poc.detail.vulnerability = {
          id: yaml.detail.vulnerability.id || '',
          level: yaml.detail.vulnerability.level || 'medium'
        }
      }
    }
    
    // 解析 rules
    if (yaml.rules) {
      for (const [ruleName, ruleData] of Object.entries(yaml.rules)) {
        const rule = {
          request: { method: 'GET', path: '/', headers: {}, body: '' },
          expression: ''
        }
        
        if (ruleData.request) {
          rule.request.method = ruleData.request.method || 'GET'
          rule.request.path = ruleData.request.path || '/'
          rule.request.headers = ruleData.request.headers || {}
          rule.request.body = ruleData.request.body || ''
        }
        
        rule.expression = ruleData.expression || ''
        poc.rules[ruleName] = rule
      }
    }
    
    // 解析顶层 expression
    poc.expression = yaml.expression || ''
    
    if (!poc.name && Object.keys(poc.rules).length === 0) {
      return null
    }
    
    return poc
  } catch (e) {
    console.error('YAML parse error:', e)
    return null
  }
}

// 解析 XRAY rules (保留作为备用，主要使用 YAML 解析)
function parseXrayRules(rulesContent) {
  const rules = {}
  
  // 匹配每个规则块 - XRAY 使用 4 空格缩进
  const ruleMatches = rulesContent.matchAll(/^[ ]{4}(\w+):\s*\n((?:[ ]{6,}.+\n?)+)/gm)
  
  for (const match of ruleMatches) {
    const ruleName = match[1]
    const ruleContent = match[2]
    
    const rule = {
      request: { method: 'GET', path: '/', headers: {}, body: '' },
      expression: ''
    }
    
    // 解析 request
    const requestMatch = ruleContent.match(/request:\s*\n((?:\s+.+\n?)+?)(?=\s+expression:|$)/m)
    if (requestMatch) {
      const reqBlock = requestMatch[1]
      
      const methodMatch = reqBlock.match(/method:\s*(.+)/m)
      if (methodMatch) rule.request.method = methodMatch[1].trim()
      
      const pathMatch = reqBlock.match(/path:\s*(.+)/m)
      if (pathMatch) rule.request.path = pathMatch[1].trim().replace(/^["']|["']$/g, '')
      
      const headersMatch = reqBlock.match(/headers:\s*\n((?:\s+.+:\s*.+\n?)+)/m)
      if (headersMatch) {
        const headerLines = headersMatch[1].match(/^\s+(.+?):\s*(.+)$/gm)
        if (headerLines) {
          for (const line of headerLines) {
            const hMatch = line.match(/^\s+(.+?):\s*(.+)$/)
            if (hMatch) {
              rule.request.headers[hMatch[1].trim()] = hMatch[2].trim().replace(/^["']|["']$/g, '')
            }
          }
        }
      }
      
      const bodyMatch = reqBlock.match(/body:\s*\|?\s*\n?([\s\S]*?)(?=\n\s+\w+:|$)/m)
      if (bodyMatch) {
        rule.request.body = bodyMatch[1].trim().replace(/^["']|["']$/g, '')
      } else {
        const simpleBodyMatch = reqBlock.match(/body:\s*(.+)$/m)
        if (simpleBodyMatch) rule.request.body = simpleBodyMatch[1].trim().replace(/^["']|["']$/g, '')
      }
    }
    
    // 解析 expression
    const exprMatch = ruleContent.match(/expression:\s*(.+)/m)
    if (exprMatch) rule.expression = exprMatch[1].trim()
    
    rules[ruleName] = rule
  }
  
  return rules
}

// 转换为 Nuclei 格式
function convertToNucleiFormat(xrayPoc) {
  const lines = []
  
  // ID
  lines.push(`id: ${xrayPoc.name || 'converted-poc'}`)
  lines.push('')
  
  // Info 块
  lines.push('info:')
  lines.push(`  name: ${xrayPoc.detail?.name || xrayPoc.name || 'Converted POC'}`)
  lines.push(`  author: ${xrayPoc.detail?.author || 'xray-converter'}`)
  
  // 从 vulnerability.level 获取严重级别
  const severity = xrayPoc.detail?.vulnerability?.level || xrayPoc.detail?.severity || 'medium'
  lines.push(`  severity: ${mapXraySeverity(severity)}`)
  
  if (xrayPoc.detail?.description) {
    lines.push(`  description: ${xrayPoc.detail.description}`)
  }
  
  // Tags
  const tags = extractXrayTags(xrayPoc)
  if (tags.length > 0) {
    lines.push(`  tags: ${tags.join(',')}`)
  }
  
  // Reference links
  if (xrayPoc.detail?.links && xrayPoc.detail.links.length > 0) {
    lines.push('  reference:')
    for (const link of xrayPoc.detail.links) {
      lines.push(`    - ${link}`)
    }
  }
  
  lines.push('')
  
  // HTTP requests
  const ruleNames = Object.keys(xrayPoc.rules)
  if (ruleNames.length > 0) {
    lines.push('http:')
    
    for (const ruleName of ruleNames) {
      const rule = xrayPoc.rules[ruleName]
      
      // 使用 raw 请求格式
      lines.push('  - raw:')
      lines.push('      - |')
      
      // 构建 raw HTTP 请求
      const method = rule.request.method || 'GET'
      const path = convertXrayPath(rule.request.path, xrayPoc.set)
      lines.push(`        ${method} ${path} HTTP/1.1`)
      lines.push('        Host: {{Hostname}}')
      
      // Headers
      for (const [key, value] of Object.entries(rule.request.headers || {})) {
        if (key.toLowerCase() !== 'host') {
          const convertedValue = convertXrayVariables(value, xrayPoc.set)
          lines.push(`        ${key}: ${convertedValue}`)
        }
      }
      
      // Body
      if (rule.request.body) {
        lines.push('')
        const bodyContent = convertXrayVariables(rule.request.body, xrayPoc.set)
        // 处理多行 body
        const bodyLines = bodyContent.split('\n')
        for (const bodyLine of bodyLines) {
          lines.push(`        ${bodyLine.trim()}`)
        }
      }
      
      lines.push('')
      
      // Matchers
      const matchers = convertXrayExpression(rule.expression)
      if (matchers.length > 0) {
        lines.push('    matchers-condition: and')
        lines.push('    matchers:')
        for (const matcher of matchers) {
          lines.push(`      - type: ${matcher.type}`)
          if (matcher.part) {
            lines.push(`        part: ${matcher.part}`)
          }
          if (matcher.words) {
            lines.push('        words:')
            for (const word of matcher.words) {
              lines.push(`          - "${word}"`)
            }
          }
          if (matcher.regex) {
            lines.push('        regex:')
            for (const r of matcher.regex) {
              lines.push(`          - "${r}"`)
            }
          }
          if (matcher.status) {
            lines.push('        status:')
            for (const s of matcher.status) {
              lines.push(`          - ${s}`)
            }
          }
        }
      }
    }
  }
  
  return lines.join('\n')
}

// 转换 XRAY 路径变量
function convertXrayPath(path, setVars) {
  if (!path) return '/'
  
  let result = path
  // 替换 {{变量}} 为 Nuclei 格式
  result = result.replace(/\{\{(\w+)\}\}/g, (match, varName) => {
    if (setVars && setVars[varName]) {
      // 如果是 randomInt 等函数，转换为 Nuclei 格式
      const value = setVars[varName]
      if (value.includes('randomInt')) {
        return '{{rand_int(1000, 9999)}}'
      }
      if (value.includes('randomLowercase')) {
        return '{{rand_base(8)}}'
      }
      return `{{${varName}}}`
    }
    return match
  })
  
  return result
}

// 转换 XRAY 变量
function convertXrayVariables(str, setVars) {
  if (!str) return str
  
  let result = str
  result = result.replace(/\{\{(\w+)\}\}/g, (match, varName) => {
    if (setVars && setVars[varName]) {
      const value = setVars[varName]
      if (value.includes('randomInt')) {
        return '{{rand_int(1000, 9999)}}'
      }
      if (value.includes('randomLowercase')) {
        return '{{rand_base(8)}}'
      }
    }
    return match
  })
  
  return result
}

// 转换 XRAY expression 为 Nuclei matchers
function convertXrayExpression(expression) {
  const matchers = []
  
  if (!expression) return matchers
  
  // 解析 response.status == xxx
  const statusMatch = expression.match(/response\.status\s*==\s*(\d+)/g)
  if (statusMatch) {
    const statuses = statusMatch.map(m => {
      const match = m.match(/(\d+)/)
      return match ? parseInt(match[1]) : null
    }).filter(s => s !== null)
    
    if (statuses.length > 0) {
      matchers.push({
        type: 'status',
        status: statuses
      })
    }
  }
  
  // 解析 response.body.bcontains(b"xxx") 或 response.body.contains("xxx")
  const bodyContainsMatch = expression.match(/response\.body\.b?contains\s*\(\s*b?["']([^"']+)["']\s*\)/g)
  if (bodyContainsMatch) {
    const words = bodyContainsMatch.map(m => {
      const match = m.match(/\(\s*b?["']([^"']+)["']\s*\)/)
      return match ? match[1] : null
    }).filter(w => w !== null)
    
    if (words.length > 0) {
      matchers.push({
        type: 'word',
        part: 'body',
        words: words
      })
    }
  }
  
  // 解析 response.headers["xxx"].contains("xxx") 或 response.headers[xxx].startsWith(xxx)
  const headerMatch = expression.match(/response\.headers\[["']?(\w+)["']?\]\.(contains|startsWith)\s*\(\s*["']?([^)"']+)["']?\s*\)/g)
  if (headerMatch) {
    const words = headerMatch.map(m => {
      const match = m.match(/\.(contains|startsWith)\s*\(\s*["']?([^)"']+)["']?\s*\)/)
      return match ? match[2] : null
    }).filter(w => w !== null && !w.match(/^\w+$/)) // 排除变量名
    
    if (words.length > 0) {
      matchers.push({
        type: 'word',
        part: 'header',
        words: words
      })
    }
  }
  
  // 解析 response.body.bmatches(xxx) 或 "xxx".bmatches(response.body) - 正则匹配
  const regexMatch = expression.match(/(?:response\.body\.b?matches\s*\(\s*["']([^"']+)["']\s*\)|["']([^"']+)["']\.b?matches\s*\(\s*response\.body\s*\))/g)
  if (regexMatch) {
    const regexes = regexMatch.map(m => {
      const match = m.match(/["']([^"']+)["']/)
      return match ? match[1] : null
    }).filter(r => r !== null)
    
    if (regexes.length > 0) {
      matchers.push({
        type: 'regex',
        part: 'body',
        regex: regexes
      })
    }
  }
  
  // 如果没有解析到任何 matcher，添加一个默认的
  if (matchers.length === 0) {
    matchers.push({
      type: 'status',
      status: [200]
    })
  }
  
  return matchers
}

// 映射 XRAY 严重级别到 Nuclei
function mapXraySeverity(xraySeverity) {
  if (!xraySeverity) return 'medium'
  
  const severityMap = {
    'critical': 'critical',
    'high': 'high',
    'medium': 'medium',
    'low': 'low',
    'info': 'info',
    'informational': 'info'
  }
  
  return severityMap[xraySeverity.toLowerCase()] || 'medium'
}

// 从 XRAY POC 提取标签
function extractXrayTags(xrayPoc) {
  const tags = ['xray-converted']
  
  // 从 name 中提取可能的标签
  if (xrayPoc.name) {
    const name = xrayPoc.name.toLowerCase()
    
    // 常见漏洞类型
    if (name.includes('rce') || name.includes('command')) tags.push('rce')
    if (name.includes('sqli') || name.includes('sql-injection') || name.includes('sql_injection')) tags.push('sqli')
    if (name.includes('xss')) tags.push('xss')
    if (name.includes('ssrf')) tags.push('ssrf')
    if (name.includes('lfi') || name.includes('file-read') || name.includes('readfile')) tags.push('lfi')
    if (name.includes('rfi')) tags.push('rfi')
    if (name.includes('xxe')) tags.push('xxe')
    if (name.includes('upload') || name.includes('writefile')) tags.push('fileupload')
    if (name.includes('unauth') || name.includes('bypass') || name.includes('unauthorized')) tags.push('unauth')
    if (name.includes('disclosure') || name.includes('leak')) tags.push('exposure')
    if (name.includes('deserialization')) tags.push('deserialization')
    if (name.includes('directory_traversal') || name.includes('path-traversal')) tags.push('traversal')
    
    // CVE
    const cveMatch = name.match(/cve-\d{4}-\d+/i)
    if (cveMatch) tags.push(cveMatch[0].toLowerCase())
    
    // CNVD
    const cnvdMatch = name.match(/cnvd-\d{4}-\d+/i)
    if (cnvdMatch) tags.push(cnvdMatch[0].toLowerCase())
  }
  
  // 从 vulnerability id 提取
  if (xrayPoc.detail?.vulnerability?.id) {
    const vulnId = xrayPoc.detail.vulnerability.id
    // CT-xxx 格式
    if (vulnId.startsWith('CT-')) {
      tags.push(vulnId.toLowerCase())
    }
    // CVE 格式
    if (vulnId.toLowerCase().startsWith('cve-')) {
      tags.push(vulnId.toLowerCase())
    }
  }
  
  return [...new Set(tags)] // 去重
}

// 预览转换后的 POC
function previewConvertedPoc(poc) {
  convertedPocPreviewContent.value = poc.content
  convertedPocPreviewVisible.value = true
}

// 复制转换后的 POC
function copyConvertedPoc() {
  navigator.clipboard.writeText(convertedPocPreviewContent.value).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败')
  })
}

// 处理文件上传
function handleImportFileChange(uploadFile, uploadFiles) {
  // uploadFile 是当前变化的文件，uploadFiles 是所有文件列表
  console.log('File change:', uploadFile?.name, 'Total files:', uploadFiles?.length)
  
  if (!uploadFile || !uploadFile.raw) {
    console.warn('No file or raw data')
    return
  }
  
  // 检查文件类型
  const fileName = uploadFile.name.toLowerCase()
  if (!fileName.endsWith('.yaml') && !fileName.endsWith('.yml')) {
    ElMessage.warning(`文件 ${uploadFile.name} 不是YAML文件，已跳过`)
    return
  }
  
  // 检查文件是否已处理过（通过标记）
  if (uploadFile._processed) {
    return
  }
  uploadFile._processed = true
  uploadedFileCount.value++
  
  const reader = new FileReader()
  reader.onload = (e) => {
    const content = e.target.result
    console.log('File content loaded:', uploadFile.name, 'Length:', content?.length)
    
    if (!content || content.trim().length === 0) {
      ElMessage.warning(`文件 ${uploadFile.name} 内容为空`)
      return
    }
    
    let parsed
    if (importPocFormat.value === 'xray') {
      parsed = parseXrayToNuclei(content)
    } else {
      parsed = parseYamlToPreview(content)
    }
    
    if (parsed) {
      // 检查是否已存在相同templateId或相同内容
      const existsByTemplateId = importPocPreviews.value.some(p => p.templateId === parsed.templateId)
      const existsByContent = importPocPreviews.value.some(p => p.content.trim() === parsed.content.trim())
      
      if (!existsByTemplateId && !existsByContent) {
        importPocPreviews.value.push(parsed)
        console.log('Added POC:', parsed.templateId)
      } else {
        console.log(`模板 ${parsed.templateId} 已存在（ID或内容重复），已跳过`)
      }
    } else {
      ElMessage.warning(`文件 ${uploadFile.name} 解析失败，请检查格式`)
    }
  }
  reader.onerror = (err) => {
    console.error('File read error:', err)
    ElMessage.error(`文件 ${uploadFile.name} 读取失败`)
  }
  reader.readAsText(uploadFile.raw)
}

// 处理文件移除
function handleImportFileRemove(file) {
  // 文件移除时不做处理，预览列表由用户手动管理
}

// 移除预览项
function removeImportPreview(index) {
  importPocPreviews.value.splice(index, 1)
}

// 清空导入内容
function clearImportContent() {
  importPocContent.value = ''
  importPocPreviews.value = []
  uploadedFileCount.value = 0
  if (importPocUploadRef.value) {
    importPocUploadRef.value.clearFiles()
  }
}

// 执行导入
async function handleImportPocs() {
  if (importPocPreviews.value.length === 0) {
    ElMessage.warning('没有可导入的POC')
    return
  }
  
  importPocLoading.value = true
  
  try {
    // 准备批量导入数据
    const pocs = importPocPreviews.value.map(poc => ({
      name: poc.name,
      templateId: poc.templateId,
      severity: poc.severity,
      tags: poc.tags,
      author: poc.author,
      description: poc.description,
      content: poc.content,
      enabled: importPocEnabled.value
    }))
    
    // 尝试批量导入
    const res = await batchImportCustomPoc({ pocs })
    
    if (res.code === 0) {
      const successCount = res.imported || pocs.length
      const failCount = res.failed || 0
      ElMessage.success(`成功导入 ${successCount} 个POC${failCount > 0 ? `，${failCount} 个失败` : ''}`)
      importPocDialogVisible.value = false
      loadCustomPocs()
    } else {
      // 批量导入失败，回退到逐个导入
      console.warn('批量导入API失败，回退到逐个导入:', res.msg)
      await fallbackSingleImport()
    }
  } catch (e) {
    // API不存在或出错，回退到逐个导入
    console.warn('批量导入API异常，回退到逐个导入:', e)
    await fallbackSingleImport()
  } finally {
    importPocLoading.value = false
  }
}

// 回退到逐个导入
async function fallbackSingleImport() {
  let successCount = 0
  let failCount = 0
  
  for (const poc of importPocPreviews.value) {
    const submitData = {
      name: poc.name,
      templateId: poc.templateId,
      severity: poc.severity,
      tags: poc.tags,
      author: poc.author,
      description: poc.description,
      content: poc.content,
      enabled: importPocEnabled.value
    }
    
    try {
      const res = await saveCustomPoc(submitData)
      if (res.code === 0) {
        successCount++
      } else {
        failCount++
        console.error(`导入 ${poc.templateId} 失败:`, res.msg)
      }
    } catch (e) {
      failCount++
      console.error(`导入 ${poc.templateId} 失败:`, e)
    }
  }
  
  if (successCount > 0) {
    ElMessage.success(`成功导入 ${successCount} 个POC${failCount > 0 ? `，${failCount} 个失败` : ''}`)
    importPocDialogVisible.value = false
    loadCustomPocs()
  } else {
    ElMessage.error('导入失败')
  }
}

// ==================== 导入POC相关函数结束 ====================

function getSeverityType(severity) {
  const map = {
    critical: 'danger',
    high: 'warning',
    medium: '',
    low: 'info',
    info: 'success'
  }
  return map[severity] || 'info'
}

function getNucleiTemplate() {
  return `id: custom-poc-template

info:
  name: Custom POC Template
  author: your-name
  severity: medium
  description: Description of the vulnerability
  tags: custom,poc

http:
  - method: GET
    path:
      - "{{BaseURL}}/vulnerable-path"

    matchers-condition: and
    matchers:
      - type: status
        status:
          - 200

      - type: word
        words:
          - "vulnerable-keyword"
        part: body
`
}

// 解析YAML内容，提取字段
function parseYamlContent() {
  const content = customPocForm.content
  if (!content) return

  // 解析 id 字段
  const idMatch = content.match(/^id:\s*(.+)$/m)
  if (idMatch) {
    customPocForm.templateId = idMatch[1].trim()
  }

  // 解析 info 块中的字段
  const infoMatch = content.match(/info:\s*\n((?:\s+.+\n?)+)/m)
  if (infoMatch) {
    const infoBlock = infoMatch[1]
    
    // name
    const nameMatch = infoBlock.match(/^\s+name:\s*(.+)$/m)
    if (nameMatch) {
      customPocForm.name = nameMatch[1].trim()
    }
    
    // author
    const authorMatch = infoBlock.match(/^\s+author:\s*(.+)$/m)
    if (authorMatch) {
      customPocForm.author = authorMatch[1].trim()
    }
    
    // severity
    const severityMatch = infoBlock.match(/^\s+severity:\s*(.+)$/m)
    if (severityMatch) {
      const severity = severityMatch[1].trim().toLowerCase()
      if (['critical', 'high', 'medium', 'low', 'info', 'unknown'].includes(severity)) {
        customPocForm.severity = severity
      }
    }
    
    // description
    const descMatch = infoBlock.match(/^\s+description:\s*(.+)$/m)
    if (descMatch) {
      customPocForm.description = descMatch[1].trim()
    }
    
    // tags (可能是逗号分隔或YAML数组)
    const tagsMatch = infoBlock.match(/^\s+tags:\s*(.+)$/m)
    if (tagsMatch) {
      const tagsStr = tagsMatch[1].trim()
      // 处理逗号分隔的标签
      const tags = tagsStr.split(',').map(t => t.trim()).filter(t => t)
      customPocForm.tags = tags
      customPocForm.tagsInput = tags.join(', ') // 同步更新输入框
    }
  }
}

// 显示POC验证对话框
function showPocValidateDialog(row) {
  validatePoc.value = row
  pocValidateUrl.value = ''
  pocValidateResult.value = null
  pocValidateLogs.value = []
  currentTaskId = null
  pocValidateDialogVisible.value = true
}

// 显示扫描现有资产对话框
function showScanAssetsDialog(row) {
  scanAssetsPoc.value = row
  scanAssetsStarted.value = false
  scanAssetsLogs.value = []
  scanAssetsTaskIds = []
  scanAssetsProgress.total = 0
  scanAssetsProgress.completed = 0
  scanAssetsProgress.vulnCount = 0
  scanAssetsDialogVisible.value = true
}

// 清理扫描资产相关资源
function cleanupScanAssets() {
  if (scanAssetsPollTimer) {
    clearInterval(scanAssetsPollTimer)
    scanAssetsPollTimer = null
  }
  if (scanAssetsEventSource) {
    scanAssetsEventSource.close()
    scanAssetsEventSource = null
  }
  scanAssetsTaskIds = []
}

// 对话框关闭时清理
function handleScanAssetsDialogClose() {
  cleanupScanAssets()
  scanAssetsLoading.value = false
}

// 开始监听扫描资产日志流
function startScanAssetsLogStream(taskIds) {
  // 关闭之前的连接
  if (scanAssetsEventSource) {
    scanAssetsEventSource.close()
  }
  
  scanAssetsTaskIds = taskIds
  
  // 连接SSE日志流
  const baseUrl = import.meta.env.VITE_API_BASE_URL || ''
  const token = localStorage.getItem('token')
  scanAssetsEventSource = new EventSource(`${baseUrl}/api/v1/worker/logs/stream?token=${token}`)
  
  scanAssetsEventSource.onmessage = (event) => {
    try {
      const log = JSON.parse(event.data)
      // 检查日志是否属于当前扫描的任务
      const matchedTaskId = scanAssetsTaskIds.find(tid => log.message && log.message.includes(tid))
      if (matchedTaskId) {
        // 提取日志中的关键信息，去掉taskId前缀
        let displayMsg = log.message
        const taskIdPrefix = `[${matchedTaskId}] `
        if (displayMsg.startsWith(taskIdPrefix)) {
          displayMsg = displayMsg.substring(taskIdPrefix.length)
        }
        
        scanAssetsLogs.value.push({
          level: log.level || 'INFO',
          message: displayMsg,
          timestamp: log.timestamp || new Date().toLocaleTimeString()
        })
        
        // 检查是否发现漏洞（匹配日志中的漏洞标记）
        if (displayMsg.includes('✓') || displayMsg.includes('Vulnerability found') || displayMsg.includes('发现漏洞')) {
          scanAssetsProgress.vulnCount++
        }
        
        // 从完成日志中提取漏洞数（格式：Batch scan completed: targets=X, vuls=Y, duration=Zs）
        const vulMatch = displayMsg.match(/vuls[=:]\s*(\d+)/i)
        if (vulMatch) {
          scanAssetsProgress.vulnCount = parseInt(vulMatch[1])
        }
        
        // 限制日志数量
        if (scanAssetsLogs.value.length > 200) {
          scanAssetsLogs.value.shift()
        }
        // 滚动到底部
        scrollScanAssetsLogsToBottom()
      }
    } catch (e) {
      // 忽略解析错误
    }
  }
  
  scanAssetsEventSource.onerror = () => {
    // 连接错误时不做处理
  }
}

// 滚动扫描日志到底部
function scrollScanAssetsLogsToBottom() {
  if (scanAssetsLogsRef.value) {
    scanAssetsLogsRef.value.scrollTop = scanAssetsLogsRef.value.scrollHeight
  }
}

// 轮询扫描任务状态
function startScanAssetsPoll() {
  if (scanAssetsPollTimer) {
    clearInterval(scanAssetsPollTimer)
  }
  
  scanAssetsPollTimer = setInterval(async () => {
    if (scanAssetsTaskIds.length === 0) {
      clearInterval(scanAssetsPollTimer)
      scanAssetsPollTimer = null
      return
    }
    
    // 只有一个批量任务，检查它的状态
    const taskId = scanAssetsTaskIds[0]
    
    try {
      const res = await getPocValidationResult({ taskId })
      if (res.code === 0 && (res.status === 'SUCCESS' || res.status === 'FAILURE')) {
        clearInterval(scanAssetsPollTimer)
        scanAssetsPollTimer = null
        scanAssetsLoading.value = false
        
        // 从结果中获取漏洞数
        let vulnCount = 0
        if (res.results && res.results.length > 0) {
          vulnCount = res.results.filter(r => r.matched).length
        }
        scanAssetsProgress.vulnCount = vulnCount
        scanAssetsProgress.completed = scanAssetsProgress.total
        
        scanAssetsLogs.value.push({
          level: 'INFO',
          message: `扫描完成，共扫描 ${scanAssetsProgress.total} 个资产，发现 ${vulnCount} 个漏洞`,
          timestamp: new Date().toLocaleTimeString()
        })
        scrollScanAssetsLogsToBottom()
        
        if (vulnCount > 0) {
          ElMessage.warning(`扫描完成，发现 ${vulnCount} 个漏洞`)
        } else {
          ElMessage.success('扫描完成，未发现漏洞')
        }
      }
    } catch (e) {
      // 忽略查询错误
    }
  }, 2000)
}

// 执行扫描现有资产
async function handleScanAssets() {
  // 清理之前的资源
  cleanupScanAssets()
  
  scanAssetsLoading.value = true
  scanAssetsStarted.value = true
  scanAssetsLogs.value = []
  scanAssetsProgress.total = 0
  scanAssetsProgress.completed = 0
  scanAssetsProgress.vulnCount = 0

  // 添加初始日志
  scanAssetsLogs.value.push({
    level: 'INFO',
    message: '正在提交扫描任务...',
    timestamp: new Date().toLocaleTimeString()
  })

  try {
    const res = await scanAssetsWithPoc({
      pocId: scanAssetsPoc.value.id
    })

    if (res.code === 0) {
      scanAssetsProgress.total = res.totalScanned
      
      scanAssetsLogs.value.push({
        level: 'INFO',
        message: `已创建批量扫描任务，目标: ${res.totalScanned} 个资产`,
        timestamp: new Date().toLocaleTimeString()
      })
      
      if (res.taskIds && res.taskIds.length > 0) {
        // 开始监听日志流
        startScanAssetsLogStream(res.taskIds)
        // 开始轮询任务状态
        startScanAssetsPoll()
      } else {
        scanAssetsLoading.value = false
        scanAssetsLogs.value.push({
          level: 'INFO',
          message: res.msg || '扫描任务已提交',
          timestamp: new Date().toLocaleTimeString()
        })
      }
    } else {
      scanAssetsLoading.value = false
      scanAssetsLogs.value.push({
        level: 'ERROR',
        message: res.msg || '扫描失败',
        timestamp: new Date().toLocaleTimeString()
      })
      ElMessage.error(res.msg || '扫描失败')
    }
  } catch (e) {
    scanAssetsLoading.value = false
    scanAssetsLogs.value.push({
      level: 'ERROR',
      message: '扫描请求失败: ' + e.message,
      timestamp: new Date().toLocaleTimeString()
    })
    ElMessage.error('扫描请求失败: ' + e.message)
  }
}

// 轮询定时器
let pollTimer = null

// 清理轮询定时器和日志流
onUnmounted(() => {
  cleanupValidation()
  cleanupScanAssets()
})

// 清理验证相关资源
function cleanupValidation() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (logEventSource) {
    logEventSource.close()
    logEventSource = null
  }
  currentTaskId = null
}

// 对话框关闭时清理
function handleValidateDialogClose() {
  cleanupValidation()
  pocValidateLoading.value = false
}

// 开始监听日志流
function startLogStream(taskId) {
  // 关闭之前的连接
  if (logEventSource) {
    logEventSource.close()
  }
  
  pocValidateLogs.value = []
  currentTaskId = taskId
  
  // 连接SSE日志流（需要通过query参数传递token，因为EventSource不支持自定义Header）
  const baseUrl = import.meta.env.VITE_API_BASE_URL || ''
  const token = localStorage.getItem('token')
  logEventSource = new EventSource(`${baseUrl}/api/v1/worker/logs/stream?token=${token}`)
  
  logEventSource.onmessage = (event) => {
    try {
      const log = JSON.parse(event.data)
      // 只显示包含当前taskId的日志
      if (log.message && log.message.includes(taskId)) {
        // 提取日志中的关键信息，去掉taskId前缀
        let displayMsg = log.message
        const taskIdPrefix = `[${taskId}] `
        if (displayMsg.startsWith(taskIdPrefix)) {
          displayMsg = displayMsg.substring(taskIdPrefix.length)
        }
        
        pocValidateLogs.value.push({
          level: log.level || 'INFO',
          message: displayMsg,
          timestamp: log.timestamp || new Date().toLocaleTimeString()
        })
        // 限制日志数量
        if (pocValidateLogs.value.length > 50) {
          pocValidateLogs.value.shift()
        }
        // 滚动到底部
        scrollLogsToBottom()
      }
    } catch (e) {
      // 忽略解析错误
    }
  }
  
  logEventSource.onerror = () => {
    // 连接错误时不做处理，轮询会继续
  }
}

// 滚动日志到底部
function scrollLogsToBottom() {
  setTimeout(() => {
    if (logsContainerRef.value) {
      logsContainerRef.value.scrollTop = logsContainerRef.value.scrollHeight
    }
  }, 50)
}

// 执行POC验证
async function handleValidatePoc() {
  if (!pocValidateUrl.value) {
    ElMessage.warning('请输入目标URL')
    return
  }

  pocValidateLoading.value = true
  pocValidateResult.value = null
  pocValidateLogs.value = []

  // 清理之前的轮询和日志流
  cleanupValidation()

  // 添加初始日志
  pocValidateLogs.value.push({
    level: 'INFO',
    message: '正在提交验证任务...',
    timestamp: new Date().toLocaleTimeString()
  })

  try {
    const res = await validatePocApi({
      id: validatePoc.value.id,
      url: pocValidateUrl.value,
      pocType: validatePoc.value.pocType || 'custom'
    })

    if (res.code === 0) {
      pocValidateLogs.value.push({
        level: 'INFO',
        message: `任务已下发，TaskId: ${res.taskId}`,
        timestamp: new Date().toLocaleTimeString()
      })

      // 如果返回了taskId，开始监听日志和轮询结果
      if (res.taskId) {
        startLogStream(res.taskId)
        startPollingResult(res.taskId)
      }
    } else {
      pocValidateLogs.value.push({
        level: 'ERROR',
        message: res.msg || '验证失败',
        timestamp: new Date().toLocaleTimeString()
      })
      ElMessage.error(res.msg || '验证失败')
      pocValidateLoading.value = false
    }
  } catch (e) {
    pocValidateLogs.value.push({
      level: 'ERROR',
      message: '验证请求失败: ' + e.message,
      timestamp: new Date().toLocaleTimeString()
    })
    ElMessage.error('验证请求失败: ' + e.message)
    pocValidateLoading.value = false
  }
}

// 开始轮询查询结果
function startPollingResult(taskId) {
  let pollCount = 0
  const maxPollCount = 60 // 最多轮询60次（约2分钟）

  pollTimer = setInterval(async () => {
    pollCount++
    
    if (pollCount > maxPollCount) {
      clearInterval(pollTimer)
      pollTimer = null
      if (logEventSource) {
        logEventSource.close()
        logEventSource = null
      }
      pocValidateLoading.value = false
      pocValidateLogs.value.push({
        level: 'ERROR',
        message: '验证超时，请检查Worker状态',
        timestamp: new Date().toLocaleTimeString()
      })
      pocValidateResult.value = {
        matched: false,
        severity: validatePoc.value.severity,
        details: '验证超时，请稍后重试或检查Worker状态',
        status: 'TIMEOUT'
      }
      return
    }

    try {
      const res = await getPocValidationResult({ taskId })
      
      if (res.code === 0) {
        // 更新状态显示
        if (res.status === 'SUCCESS' || res.status === 'FAILURE') {
          // 任务完成
          clearInterval(pollTimer)
          pollTimer = null
          if (logEventSource) {
            logEventSource.close()
            logEventSource = null
          }
          pocValidateLoading.value = false

          if (res.results && res.results.length > 0) {
            const result = res.results[0]
            
            pocValidateLogs.value.push({
              level: result.matched ? 'INFO' : 'INFO',
              message: result.matched ? `发现漏洞！目标: ${result.matchedUrl}` : '验证完成，未发现漏洞',
              timestamp: new Date().toLocaleTimeString()
            })
            
            pocValidateResult.value = {
              matched: result.matched,
              severity: result.severity || validatePoc.value.severity,
              details: result.matched 
                ? `目标: ${result.matchedUrl}\n详情: ${result.details || '匹配成功'}${result.output ? '\n\n输出:\n' + result.output : ''}`
                : `目标: ${result.matchedUrl}\n${result.details || '未发现漏洞'}`,
              status: res.status
            }
            
            if (result.matched) {
              ElMessage.success('发现漏洞！')
            } else {
              ElMessage.info('验证完成，未发现漏洞')
            }
          } else {
            pocValidateLogs.value.push({
              level: 'INFO',
              message: res.status === 'FAILURE' ? '验证失败' : '验证完成',
              timestamp: new Date().toLocaleTimeString()
            })
            pocValidateResult.value = {
              matched: false,
              severity: validatePoc.value.severity,
              details: res.status === 'FAILURE' ? '验证失败' : '验证完成，未发现漏洞',
              status: res.status
            }
          }
        }
      }
    } catch (e) {
      console.error('Poll result error:', e)
    }
  }, 2000) // 每2秒轮询一次
}

// 默认模板选择变化
function handleTemplateSelectionChange(selection) {
  selectedTemplates.value = selection
}

// 显示单个默认模板验证对话框
function showTemplateValidateDialog(row) {
  validatePoc.value = {
    id: row._id || row.id,
    name: row.name,
    templateId: row.id,
    severity: row.severity,
    pocType: 'nuclei'  // 标记为nuclei默认模板
  }
  pocValidateUrl.value = ''
  pocValidateResult.value = null
  pocValidateLogs.value = []
  currentTaskId = null
  pocValidateDialogVisible.value = true
}

// 显示默认模板批量验证对话框
function showTemplateBatchValidateDialog() {
  if (selectedTemplates.value.length === 0) {
    ElMessage.warning('请先选择要验证的模板')
    return
  }
  templateBatchValidateUrls.value = ''
  batchTargetInputType.value = 'text'
  templateBatchValidateLogs.value = []
  templateBatchValidateResults.value = []
  templateBatchValidateProgress.total = 0
  templateBatchValidateProgress.completed = 0
  currentBatchId = null
  templateBatchValidateDialogVisible.value = true
}

// 处理批量URL文件上传
function handleBatchUrlFileChange(file) {
  const reader = new FileReader()
  reader.onload = (e) => {
    templateBatchValidateUrls.value = e.target.result
  }
  reader.readAsText(file.raw)
}

// 处理批量URL文件移除
function handleBatchUrlFileRemove() {
  templateBatchValidateUrls.value = ''
}

// 导出验证结果
function handleExportResults(command) {
  let dataToExport = templateBatchValidateResults.value
  
  if (command === 'matched') {
    dataToExport = dataToExport.filter(r => r.matched)
  }
  
  if (dataToExport.length === 0) {
    ElMessage.warning('没有可导出的数据')
    return
  }
  
  const timestamp = new Date().toISOString().slice(0, 19).replace(/[:-]/g, '')
  
  if (command === 'csv') {
    // 导出CSV格式
    const headers = ['模板名称', '模板ID', '级别', '结果', '匹配URL', '详情']
    const rows = dataToExport.map(r => [
      r.pocName || '',
      r.templateId || r.pocId || '',
      r.severity || '',
      r.matched ? '匹配' : '未匹配',
      r.matchedUrl || '',
      (r.details || '').replace(/[\n\r]/g, ' ')
    ])
    
    const csvContent = [headers, ...rows]
      .map(row => row.map(cell => `"${cell}"`).join(','))
      .join('\n')
    
    const blob = new Blob(['\ufeff' + csvContent], { type: 'text/csv;charset=utf-8' })
    downloadFile(blob, `poc_validation_results_${timestamp}.csv`)
  } else {
    // 导出JSON格式
    const jsonContent = JSON.stringify(dataToExport, null, 2)
    const blob = new Blob([jsonContent], { type: 'application/json' })
    downloadFile(blob, `poc_validation_results_${timestamp}.json`)
  }
  
  ElMessage.success('导出成功')
}

// 下载文件
function downloadFile(blob, filename) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

// 清理批量验证资源
function cleanupBatchValidation() {
  if (batchPollTimer) {
    clearInterval(batchPollTimer)
    batchPollTimer = null
  }
  if (batchLogEventSource) {
    batchLogEventSource.close()
    batchLogEventSource = null
  }
  currentBatchId = null
  currentBatchTaskIds.value = []
}

// 批量验证对话框关闭
function handleBatchValidateDialogClose() {
  cleanupBatchValidation()
  templateBatchValidateLoading.value = false
}

// 开始批量验证日志流
function startBatchLogStream(batchId) {
  if (batchLogEventSource) {
    batchLogEventSource.close()
  }
  
  currentBatchId = batchId
  
  // 连接SSE日志流（需要通过query参数传递token，因为EventSource不支持自定义Header）
  const baseUrl = import.meta.env.VITE_API_BASE_URL || ''
  const token = localStorage.getItem('token')
  batchLogEventSource = new EventSource(`${baseUrl}/api/v1/worker/logs/stream?token=${token}`)
  
  batchLogEventSource.onmessage = (event) => {
    try {
      const log = JSON.parse(event.data)
      // 只显示当前批次任务的日志
      if (log.message && currentBatchTaskIds.value.length > 0) {
        // 检查日志是否属于当前批次的任务
        const isCurrentBatchLog = currentBatchTaskIds.value.some(taskId => log.message.includes(taskId))
        if (!isCurrentBatchLog) return
        
        let displayMsg = log.message
        // 去掉taskId前缀
        const taskIdMatch = displayMsg.match(/^\[poc-validate-\d+\]\s*/)
        if (taskIdMatch) {
          displayMsg = displayMsg.substring(taskIdMatch[0].length)
        }
        
        templateBatchValidateLogs.value.push({
          level: log.level || 'INFO',
          message: displayMsg,
          timestamp: log.timestamp || new Date().toLocaleTimeString()
        })
        
        if (templateBatchValidateLogs.value.length > 100) {
          templateBatchValidateLogs.value.shift()
        }
        
        // 滚动到底部
        setTimeout(() => {
          if (batchLogsContainerRef.value) {
            batchLogsContainerRef.value.scrollTop = batchLogsContainerRef.value.scrollHeight
          }
        }, 50)
      }
    } catch (e) {
      // 忽略解析错误
    }
  }
}

// 执行默认模板批量验证
async function handleTemplateBatchValidate() {
  const urls = batchTargetUrls.value
  if (urls.length === 0) {
    ElMessage.warning('请输入有效的目标URL')
    return
  }
  
  if (selectedTemplates.value.length === 0) {
    ElMessage.warning('请先选择要验证的模板')
    return
  }

  templateBatchValidateLoading.value = true
  templateBatchValidateLogs.value = []
  templateBatchValidateResults.value = []
  
  // 总任务数 = 模板数 × URL数
  const totalTasks = selectedTemplates.value.length * urls.length
  templateBatchValidateProgress.total = totalTasks
  templateBatchValidateProgress.completed = 0
  
  cleanupBatchValidation()

  templateBatchValidateLogs.value.push({
    level: 'INFO',
    message: `正在提交批量验证任务，${selectedTemplates.value.length} 个模板 × ${urls.length} 个目标 = ${totalTasks} 个任务...`,
    timestamp: new Date().toLocaleTimeString()
  })

  // 为每个模板和URL组合创建验证任务
  const taskIds = []
  const batchId = `batch-${Date.now()}`
  currentBatchId = batchId
  currentBatchTaskIds.value = [] // 清空之前的任务ID

  for (const url of urls) {
    for (const tpl of selectedTemplates.value) {
      try {
        const res = await validatePocApi({
          id: tpl._id || tpl.id,
          url: url,
          pocType: 'nuclei'
        })

        if (res.code === 0 && res.taskId) {
          taskIds.push(res.taskId)
        } else {
          templateBatchValidateLogs.value.push({
            level: 'ERROR',
            message: `${tpl.name || tpl.id} -> ${url} 下发失败: ${res.msg}`,
            timestamp: new Date().toLocaleTimeString()
          })
        }
      } catch (e) {
        templateBatchValidateLogs.value.push({
          level: 'ERROR',
          message: `${tpl.name || tpl.id} -> ${url} 下发失败: ${e.message}`,
          timestamp: new Date().toLocaleTimeString()
        })
      }
    }
  }

  if (taskIds.length > 0) {
    // 保存任务ID列表并启动日志流
    currentBatchTaskIds.value = taskIds
    startBatchLogStream(batchId)
    
    templateBatchValidateLogs.value.push({
      level: 'INFO',
      message: `共下发 ${taskIds.length} 个任务，开始轮询结果...`,
      timestamp: new Date().toLocaleTimeString()
    })
    startBatchPolling(batchId, taskIds)
  } else {
    templateBatchValidateLoading.value = false
    templateBatchValidateLogs.value.push({
      level: 'ERROR',
      message: '所有任务下发失败',
      timestamp: new Date().toLocaleTimeString()
    })
  }
}

// 批量验证轮询
function startBatchPolling(batchId, taskIds) {
  let pollCount = 0
  const maxPollCount = 120 // 最多轮询120次（约4分钟）
  const completedTasks = new Set()

  batchPollTimer = setInterval(async () => {
    pollCount++
    
    if (pollCount > maxPollCount) {
      cleanupBatchValidation()
      templateBatchValidateLoading.value = false
      templateBatchValidateLogs.value.push({
        level: 'ERROR',
        message: '批量验证超时',
        timestamp: new Date().toLocaleTimeString()
      })
      return
    }

    // 轮询每个任务的结果
    for (const taskId of taskIds) {
      if (completedTasks.has(taskId)) continue
      
      try {
        const res = await getPocValidationResult({ taskId })
        
        if (res.code === 0 && (res.status === 'SUCCESS' || res.status === 'FAILURE')) {
          completedTasks.add(taskId)
          templateBatchValidateProgress.completed = completedTasks.size
          
          if (res.results && res.results.length > 0) {
            for (const result of res.results) {
              templateBatchValidateResults.value.push(result)
            }
          }
        }
      } catch (e) {
        console.error('Poll batch result error:', e)
      }
    }

    // 检查是否全部完成
    if (completedTasks.size >= taskIds.length) {
      cleanupBatchValidation()
      templateBatchValidateLoading.value = false
      
      const matchedCount = templateBatchValidateResults.value.filter(r => r.matched).length
      templateBatchValidateLogs.value.push({
        level: 'INFO',
        message: `批量验证完成，发现 ${matchedCount} 个漏洞`,
        timestamp: new Date().toLocaleTimeString()
      })
      
      if (matchedCount > 0) {
        ElMessage.success(`批量验证完成，发现 ${matchedCount} 个漏洞`)
      } else {
        ElMessage.info('批量验证完成，未发现漏洞')
      }
    }
  }, 2000)
}

// 显示AI辅助对话框
function showAiAssistDialog() {
  aiAssistForm.description = ''
  aiAssistForm.vulnType = ''
  aiAssistForm.cveId = ''
  aiAssistForm.reference = ''
  aiAssistDialogVisible.value = true
}

// 使用AI生成POC
async function generatePocWithAi() {
  if (!aiAssistForm.description && !aiAssistForm.cveId) {
    ElMessage.warning('请输入漏洞描述或CVE编号')
    return
  }
  
  if (!aiConfig.baseUrl) {
    ElMessage.warning('请先配置AI服务地址')
    aiConfigCollapse.value = ['config']
    return
  }
  
  aiGenerating.value = true
  try {
    // 构建提示词
    const prompt = buildPocPrompt()
    
    let response
    let content = ''
    
    if (aiConfig.protocol === 'openai') {
      // OpenAI 协议
      response = await fetch(`${aiConfig.baseUrl}/v1/chat/completions`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${aiConfig.apiKey}`
        },
        body: JSON.stringify({
          model: aiConfig.model,
          max_tokens: 4096,
          messages: [
            { role: 'user', content: prompt }
          ]
        })
      })
      
      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(`AI服务请求失败: ${response.status} ${errorText}`)
      }
      
      const data = await response.json()
      if (data.choices && data.choices.length > 0) {
        content = data.choices[0].message?.content || ''
      }
    } else if (aiConfig.protocol === 'gemini') {
      // Gemini 协议
      response = await fetch(`${aiConfig.baseUrl}/v1beta/models/${aiConfig.model}:generateContent?key=${aiConfig.apiKey}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          contents: [
            { parts: [{ text: prompt }] }
          ],
          generationConfig: {
            maxOutputTokens: 4096
          }
        })
      })
      
      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(`AI服务请求失败: ${response.status} ${errorText}`)
      }
      
      const data = await response.json()
      if (data.candidates && data.candidates.length > 0) {
        const parts = data.candidates[0].content?.parts || []
        content = parts.map(p => p.text || '').join('')
      }
    } else {
      // Anthropic 协议 (默认)
      response = await fetch(`${aiConfig.baseUrl}/v1/messages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': aiConfig.apiKey,
          'anthropic-version': '2023-06-01'
        },
        body: JSON.stringify({
          model: aiConfig.model,
          max_tokens: 4096,
          messages: [
            { role: 'user', content: prompt }
          ]
        })
      })
      
      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(`AI服务请求失败: ${response.status} ${errorText}`)
      }
      
      const data = await response.json()
      if (data.content && data.content.length > 0) {
        content = data.content[0].text || ''
      }
    }
    
    // 提取YAML代码块
    const yamlMatch = content.match(/```ya?ml\n([\s\S]*?)```/)
    if (yamlMatch) {
      content = yamlMatch[1].trim()
    } else {
      // 尝试直接使用内容（如果看起来像YAML）
      if (content.includes('id:') && content.includes('info:')) {
        // 移除可能的markdown标记
        content = content.replace(/```ya?ml\n?/g, '').replace(/```\n?/g, '').trim()
      }
    }
    
    if (!content || !content.includes('id:')) {
      throw new Error('AI返回的内容不是有效的Nuclei POC格式')
    }
    
    // 保存到缓存（关闭对话框后仍可恢复）
    aiGeneratedPocCache.value = content
    // 将生成的POC填入编辑框
    customPocForm.content = content
    // 自动解析YAML
    parseYamlContent()
    aiAssistDialogVisible.value = false
    ElMessage.success('POC生成成功，请检查并修改后保存')
  } catch (e) {
    console.error('AI生成POC失败:', e)
    if (e.message.includes('Failed to fetch') || e.message.includes('NetworkError')) {
      ElMessage.error('无法连接到AI服务，请确保服务已启动')
    } else {
      ElMessage.error('AI生成POC失败: ' + (e.message || '未知错误'))
    }
  } finally {
    aiGenerating.value = false
  }
}

// 构建POC生成提示词
function buildPocPrompt() {
  const vulnTypeMap = {
    'sqli': 'SQL注入',
    'xss': 'XSS跨站脚本',
    'rce': '命令注入/远程代码执行',
    'lfi': '文件包含/文件读取',
    'ssrf': 'SSRF服务端请求伪造',
    'unauth': '未授权访问',
    'info-disclosure': '信息泄露',
    'cve': 'CVE漏洞',
    'other': '其他'
  }
  
  let prompt = `你是一个专业的安全研究员，擅长编写Nuclei漏洞检测模板。请根据以下信息生成一个Nuclei YAML格式的POC模板。

要求：
1. 生成标准的Nuclei YAML模板格式
2. 包含完整的id、info、http/tcp等部分
3. 使用合适的匹配器(matchers)来检测漏洞
4. 添加适当的标签(tags)
5. 只输出YAML代码，不要其他解释

`

  if (aiAssistForm.cveId) {
    prompt += `CVE编号: ${aiAssistForm.cveId}\n`
  }
  
  if (aiAssistForm.vulnType) {
    prompt += `漏洞类型: ${vulnTypeMap[aiAssistForm.vulnType] || aiAssistForm.vulnType}\n`
  }
  
  if (aiAssistForm.description) {
    prompt += `漏洞描述: ${aiAssistForm.description}\n`
  }
  
  if (aiAssistForm.reference) {
    prompt += `参考信息: ${aiAssistForm.reference}\n`
  }
  
  prompt += `
请生成Nuclei POC模板：`

  return prompt
}

// ==================== 目录扫描字典相关方法 ====================

// 加载目录扫描字典列表
async function loadDirscanDicts() {
  dirscanDictLoading.value = true
  try {
    const res = await getDirScanDictList({
      page: dirscanDictPagination.page,
      pageSize: dirscanDictPagination.pageSize
    })
    if (res.code === 0) {
      dirscanDicts.value = res.list || []
      dirscanDictPagination.total = res.total || 0
    }
  } catch (e) {
    console.error('加载目录扫描字典失败:', e)
  } finally {
    dirscanDictLoading.value = false
  }
}

// 显示字典编辑表单
function showDirscanDictForm(row = null) {
  if (row) {
    Object.assign(dirscanDictForm, {
      id: row.id,
      name: row.name,
      description: row.description || '',
      content: row.content || '',
      enabled: row.enabled
    })
  } else {
    Object.assign(dirscanDictForm, {
      id: '',
      name: '',
      description: '',
      content: '',
      enabled: true
    })
  }
  dirscanDictDialogVisible.value = true
}

// 保存目录扫描字典
async function handleSaveDirscanDict() {
  try {
    await dirscanDictFormRef.value.validate()
  } catch (e) {
    return
  }

  try {
    const res = await saveDirScanDict({
      id: dirscanDictForm.id || undefined,
      name: dirscanDictForm.name,
      description: dirscanDictForm.description,
      content: dirscanDictForm.content,
      enabled: dirscanDictForm.enabled
    })
    if (res.code === 0) {
      ElMessage.success(dirscanDictForm.id ? '更新成功' : '创建成功')
      dirscanDictDialogVisible.value = false
      loadDirscanDicts()
    } else {
      ElMessage.error(res.msg || '保存失败')
    }
  } catch (e) {
    console.error('保存字典失败:', e)
    ElMessage.error('保存失败')
  }
}

// 删除目录扫描字典
async function handleDeleteDirscanDict(row) {
  try {
    await ElMessageBox.confirm(`确定要删除字典 "${row.name}" 吗？`, '确认删除', {
      type: 'warning'
    })
    const res = await deleteDirScanDict({ id: row.id })
    if (res.code === 0) {
      ElMessage.success('删除成功')
      loadDirscanDicts()
    } else {
      ElMessage.error(res.msg || '删除失败')
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('删除字典失败:', e)
    }
  }
}

// 清空自定义目录扫描字典
async function handleClearDirscanDict() {
  try {
    await ElMessageBox.confirm('确定要清空所有自定义字典吗？内置字典不会被删除。', '确认清空', {
      type: 'warning'
    })
    clearDictLoading.value = true
    const res = await clearDirScanDict()
    if (res.code === 0) {
      ElMessage.success(`已清空 ${res.deleted} 个自定义字典`)
      loadDirscanDicts()
    } else {
      ElMessage.error(res.msg || '清空失败')
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('清空字典失败:', e)
    }
  } finally {
    clearDictLoading.value = false
  }
}

// 计算字典路径数量
function countDictPaths(content) {
  if (!content) return 0
  const lines = content.split('\n')
  let count = 0
  for (const line of lines) {
    const trimmed = line.trim()
    if (trimmed && !trimmed.startsWith('#')) {
      count++
    }
  }
  return count
}

// ==================== 子域名字典相关方法 ====================

// 加载子域名字典列表
async function loadSubdomainDicts() {
  subdomainDictLoading.value = true
  try {
    const res = await getSubdomainDictList({
      page: subdomainDictPagination.page,
      pageSize: subdomainDictPagination.pageSize
    })
    if (res.code === 0) {
      subdomainDicts.value = res.list || []
      subdomainDictPagination.total = res.total || 0
    }
  } catch (e) {
    console.error('加载子域名字典失败:', e)
  } finally {
    subdomainDictLoading.value = false
  }
}

// 显示子域名字典编辑表单
function showSubdomainDictForm(row = null) {
  if (row) {
    Object.assign(subdomainDictForm, {
      id: row.id,
      name: row.name,
      description: row.description || '',
      content: row.content || '',
      enabled: row.enabled
    })
  } else {
    Object.assign(subdomainDictForm, {
      id: '',
      name: '',
      description: '',
      content: '',
      enabled: true
    })
  }
  subdomainDictDialogVisible.value = true
}

// 保存子域名字典
async function handleSaveSubdomainDict() {
  try {
    await subdomainDictFormRef.value.validate()
  } catch (e) {
    return
  }

  try {
    const res = await saveSubdomainDict({
      id: subdomainDictForm.id || undefined,
      name: subdomainDictForm.name,
      description: subdomainDictForm.description,
      content: subdomainDictForm.content,
      enabled: subdomainDictForm.enabled
    })
    if (res.code === 0) {
      ElMessage.success(subdomainDictForm.id ? '更新成功' : '创建成功')
      subdomainDictDialogVisible.value = false
      loadSubdomainDicts()
    } else {
      ElMessage.error(res.msg || '保存失败')
    }
  } catch (e) {
    console.error('保存子域名字典失败:', e)
    ElMessage.error('保存失败')
  }
}

// 删除子域名字典
async function handleDeleteSubdomainDict(row) {
  try {
    await ElMessageBox.confirm(`确定要删除字典 "${row.name}" 吗？`, '确认删除', {
      type: 'warning'
    })
    const res = await deleteSubdomainDict({ id: row.id })
    if (res.code === 0) {
      ElMessage.success('删除成功')
      loadSubdomainDicts()
    } else {
      ElMessage.error(res.msg || '删除失败')
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('删除子域名字典失败:', e)
    }
  }
}

// 清空自定义子域名字典
async function handleClearSubdomainDict() {
  try {
    await ElMessageBox.confirm('确定要清空所有自定义字典吗？内置字典不会被删除。', '确认清空', {
      type: 'warning'
    })
    clearSubdomainDictLoading.value = true
    const res = await clearSubdomainDict()
    if (res.code === 0) {
      ElMessage.success(`已清空 ${res.deleted} 个自定义字典`)
      loadSubdomainDicts()
    } else {
      ElMessage.error(res.msg || '清空失败')
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('清空子域名字典失败:', e)
    }
  } finally {
    clearSubdomainDictLoading.value = false
  }
}

// 计算子域名词条数量
function countSubdomainWords(content) {
  if (!content) return 0
  const lines = content.split('\n')
  let count = 0
  for (const line of lines) {
    const trimmed = line.trim()
    if (trimmed && !trimmed.startsWith('#')) {
      count++
    }
  }
  return count
}
</script>

<style lang="scss" scoped>
.poc-page {
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
    align-items: center;
    flex-wrap: wrap;
  }

  .pagination {
    margin-top: 20px;
    justify-content: flex-end;
  }

  .template-content-wrapper {
    :deep(.el-textarea__inner) {
      background-color: #1e1e1e;
      color: #d4d4d4;
      border: 1px solid #3c3c3c;
    }
  }

  .yaml-editor-wrapper {
    :deep(.el-textarea__inner) {
      background-color: #1e1e1e;
      color: #d4d4d4;
      border: 1px solid #3c3c3c;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 13px;
    }
  }

  .validate-logs {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    .logs-header {
      padding: 8px 15px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
      display: flex;
      justify-content: space-between;
      align-items: center;
      font-weight: 500;
    }

    .logs-content {
      background: #1e1e1e;
      color: #d4d4d4;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 12px;
      padding: 10px;
      max-height: 200px;
      overflow-y: auto;

      .log-line {
        padding: 2px 0;
        line-height: 1.5;

        .log-time {
          color: #6a9955;
          margin-right: 8px;
        }

        .log-level {
          margin-right: 8px;
          font-weight: bold;
        }

        &.log-info .log-level {
          color: #4fc3f7;
        }

        &.log-error .log-level {
          color: #f44336;
        }

        &.log-warn .log-level {
          color: #ff9800;
        }

        .log-msg {
          color: #d4d4d4;
        }
      }
    }
  }

  .validate-result {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

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
    }
  }

  .selected-templates {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
  }

  .batch-validate-progress {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    .progress-header {
      padding: 10px 15px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
      display: flex;
      align-items: center;
    }

    .logs-content {
      background: #1e1e1e;
      color: #d4d4d4;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 12px;
      padding: 10px;
      overflow-y: auto;

      .log-line {
        padding: 2px 0;
        line-height: 1.5;

        .log-time {
          color: #6a9955;
          margin-right: 8px;
        }

        .log-level {
          margin-right: 8px;
          font-weight: bold;
        }

        &.log-info .log-level {
          color: #4fc3f7;
        }

        &.log-error .log-level {
          color: #f44336;
        }

        .log-msg {
          color: #d4d4d4;
        }
      }
    }
  }

  .batch-validate-results {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    .results-header {
      padding: 10px 15px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
      display: flex;
      align-items: center;
      font-weight: 500;
    }
  }

  .import-preview {
    margin-top: 15px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    overflow: hidden;

    .preview-header {
      padding: 10px 15px;
      background: var(--el-fill-color-light);
      border-bottom: 1px solid var(--el-border-color);
      display: flex;
      align-items: center;
      font-weight: 500;
    }
  }

  .scan-assets-tip {
    margin-bottom: 15px;
  }

  .scan-assets-result {
    .result-header {
      margin-bottom: 15px;
      display: flex;
      align-items: center;
    }

    .vuln-list {
      margin-top: 15px;
    }
  }
}
</style>
