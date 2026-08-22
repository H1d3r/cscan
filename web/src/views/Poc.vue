<template>
  <div class="poc-page">
    <!-- 常驻全局批量验证任务进度条 -->
    <div v-if="persistentTask" class="persistent-task-bar">
      <el-card shadow="hover" class="persistent-task-card">
        <div class="persistent-task-content">
          <div class="persistent-task-info">
            <el-icon :size="18" :class="persistentTask.status === 'running' ? 'rotating' : ''">
              <Loading v-if="persistentTask.status === 'running'" />
              <CircleCheck v-else-if="persistentTask.status === 'completed'" color="#67c23a" />
              <CircleClose v-else-if="persistentTask.status === 'failed'" color="#f56c6c" />
            </el-icon>
            <span class="persistent-task-title">
              {{ persistentTask.status === 'running' ? 'POC批量验证进行中' : persistentTask.status === 'completed' ? 'POC批量验证已完成' : 'POC批量验证失败' }}
            </span>
            <el-tag size="small" :type="getPocScopeTagType(persistentTask.scope)">{{ getPocScopeLabel(persistentTask.scope) }}</el-tag>
            <span class="persistent-task-url">{{ persistentTask.urlCount }} 个目标</span>
          </div>
          <div v-if="persistentTask.status === 'running'" class="persistent-task-progress">
            <el-progress
              :percentage="persistentTask.total > 0 ? Math.min(99, Math.floor(persistentTask.completed / persistentTask.total * 100)) : 0"
              :stroke-width="8"
              style="width: 280px;"
            />
            <span class="persistent-task-stat">{{ persistentTask.completed }}/{{ persistentTask.total || '?' }}</span>
          </div>
          <div v-else-if="persistentTask.status === 'completed'" class="persistent-task-result">
            <el-tag type="danger" size="small">漏洞 {{ persistentTask.matched || 0 }} 个</el-tag>
            <el-tag type="info" size="small" style="margin-left: 4px;">共 {{ persistentTask.total || 0 }} 个</el-tag>
          </div>
          <div class="persistent-task-actions">
            <el-button v-if="persistentTask.status === 'completed'" type="primary" size="small" @click="showGlobalBatchResultDialog">
              查看结果
            </el-button>
            <el-button type="info" size="small" text @click="dismissPersistentTask">
              <el-icon><Close /></el-icon>
            </el-button>
          </div>
        </div>
      </el-card>
    </div>

    <div class="tabs-with-action">
      <el-tabs v-model="activeTab" @tab-change="handleTabChange" class="flex-grow-tabs">
      <!-- Nuclei默认模板 -->
      <el-tab-pane :label="$t('poc.defaultTemplates')" name="nucleiTemplates">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('poc.nucleiTemplateLib') }}</span>
              <span class="card-header-hint">
                {{ $t('poc.totalTemplates', { count: templateStats.total || 0 }) }}
              </span>
              <el-button type="primary" size="small" style="margin-left: auto" :loading="syncLoading" @click="handleOpenDownloadDialog">
                <el-icon><Refresh /></el-icon>{{ $t('poc.syncTemplate') }}
              </el-button>
              <el-button type="danger" size="small" plain style="margin-left: 10px" @click="handleClearTemplates">
                {{ $t('poc.clearTemplates') }}
              </el-button>
            </div>
          </template>
          <p class="tip-text">
            {{ $t('poc.templateLibTip') }}
          </p>
          <!-- 筛选条件（仿官方模板库：每个筛选项带数量统计） -->
          <div class="template-filters">
            <div class="filter-row">
              <div class="filter-row-label">{{ $t('poc.filterSearch') }}</div>
              <div class="filter-row-controls">
                <el-input v-model="templateFilter.keyword" :placeholder="$t('poc.searchAllPlaceholder')" clearable style="width: 320px" @keyup.enter="handleTemplateSearch" />
                <el-select v-model="templateFilter.tag" :placeholder="$t('poc.filterTag')" clearable filterable style="width: 150px" @change="handleTemplateSearch">
                  <el-option v-for="t in templateFacets.tags" :key="t.value" :label="`${t.value} (${t.count})`" :value="t.value" />
                </el-select>
                <el-select v-model="templateFilter.category" :placeholder="$t('poc.filterCategory')" clearable filterable style="width: 140px" @change="handleTemplateSearch">
                  <el-option v-for="c in templateFacets.categories" :key="c.value" :label="`${c.value} (${c.count})`" :value="c.value" />
                </el-select>
                <el-button type="primary" @click="handleTemplateSearch">{{ $t('common.search') }}</el-button>
                <el-button @click="resetTemplateFilter">{{ $t('common.reset') }}</el-button>
              </div>
            </div>
            <div class="filter-row">
              <div class="filter-row-label">{{ $t('poc.filterLevel') }}</div>
              <div class="facet-group">
                <button
                  v-for="level in severityLevels"
                  :key="level"
                  type="button"
                  class="facet-chip"
                  :class="[`facet-sev-${level}`, { 'is-active': templateFilter.severities.includes(level), 'is-disabled': !templateFilter.severities.includes(level) && severityFacetCount(level) === 0 }]"
                  :disabled="!templateFilter.severities.includes(level) && severityFacetCount(level) === 0"
                  @click="toggleSeverityFacet(level)"
                >
                  <span class="facet-sev-dot" :class="`dot-${level}`"></span>
                  <span class="facet-name">{{ level }}</span>
                  <span class="facet-count">{{ severityFacetCount(level) }}</span>
                </button>
              </div>
            </div>
            <div class="filter-row">
              <div class="filter-row-label">{{ $t('poc.filterProtocol') }}</div>
              <div class="facet-group">
                <button
                  v-for="p in templateFacets.protocols"
                  :key="p.value"
                  type="button"
                  class="facet-chip"
                  :class="{ 'is-active': templateFilter.protocols.includes(p.value) }"
                  @click="toggleProtocolFacet(p.value)"
                >
                  <span class="facet-name">{{ p.value }}</span>
                  <span class="facet-count">{{ p.count }}</span>
                </button>
              </div>
            </div>
            <div class="filter-row">
              <div class="filter-row-label">CVE</div>
              <div class="facet-group">
                <button
                  type="button"
                  class="facet-chip"
                  :class="{ 'is-active': templateFilter.hasCve === true }"
                  @click="toggleCveFacet(true)"
                >
                  <span class="facet-name">{{ $t('poc.cveYes') }}</span>
                  <span class="facet-count">{{ templateFacets.cveTrue }}</span>
                </button>
                <button
                  type="button"
                  class="facet-chip"
                  :class="{ 'is-active': templateFilter.hasCve === false }"
                  @click="toggleCveFacet(false)"
                >
                  <span class="facet-name">{{ $t('poc.cveNo') }}</span>
                  <span class="facet-count">{{ templateFacets.cveFalse }}</span>
                </button>
              </div>
            </div>
            <div class="filter-row">
              <div class="filter-row-label">{{ $t('poc.filterProduct') }}</div>
              <div class="filter-row-controls">
                <el-select
                  v-model="templateFilter.products"
                  :placeholder="$t('poc.productPlaceholder')"
                  multiple
                  filterable
                  clearable
                  collapse-tags
                  collapse-tags-tooltip
                  style="width: 320px"
                  @change="handleTemplateSearch"
                >
                  <el-option v-for="p in templateFacets.products" :key="p.value" :label="`${p.value} (${p.count})`" :value="p.value" />
                </el-select>
              </div>
            </div>
          </div>
          <!-- 批量操作 -->
          <div class="stats-bar" v-if="selectedTemplates.length > 0">
            <el-button
              type="success"
              size="small"
              @click="showTemplateBatchValidateDialog"
            >
              {{ $t('poc.batchValidate') }} ({{ selectedTemplates.length }})
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
            <el-table-column prop="id" :label="$t('poc.templateId')" width="200" show-overflow-tooltip />
            <el-table-column prop="name" :label="$t('poc.name')" min-width="180" show-overflow-tooltip />
            <el-table-column prop="severity" :label="$t('poc.level')" width="90" sortable :sort-method="sortBySeverity">
              <template #default="{ row }">
                <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="protocol" :label="$t('poc.protocol')" width="90">
              <template #default="{ row }">
                <el-tag v-if="row.protocol" effect="plain" size="small">{{ row.protocol }}</el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column prop="cveIds" :label="'CVE'" width="140">
              <template #default="{ row }">
                <el-tag v-if="row.cveIds && row.cveIds.length" type="danger" effect="plain" size="small">
                  {{ row.cveIds[0] }}{{ row.cveIds.length > 1 ? ` +${row.cveIds.length - 1}` : '' }}
                </el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column prop="product" :label="$t('poc.product')" width="110" show-overflow-tooltip>
              <template #default="{ row }">
                {{ row.product || row.vendor || '-' }}
              </template>
            </el-table-column>
            <el-table-column prop="tags" :label="$t('poc.tags')" min-width="160">
              <template #default="{ row }">
                <el-tag v-for="tag in (row.tags || [])" :key="tag" size="small" style="margin-right: 3px">
                  {{ tag }}
                </el-tag>
                <span v-if="row.tags && row.tags.length > 3" class="more-count">+{{ row.tags.length - 3 }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="author" :label="$t('poc.author')" width="100" show-overflow-tooltip />
            <el-table-column :label="$t('poc.operation')" width="120" fixed="right">
              <template #default="{ row }">
                <el-button type="success" link size="small" @click="showTemplateValidateDialog(row)">{{ $t('poc.validate') }}</el-button>
                <el-button type="primary" link size="small" @click="showTemplateContent(row)">{{ $t('poc.view') }}</el-button>
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
      <el-tab-pane :label="$t('poc.tagMapping')" name="tagMapping">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('poc.appTagMappingConfig') }}</span>
              <span class="card-header-hint">
                {{ $t('poc.totalMappings', { count: tagMappings.length || 0 }) }}
              </span>
              <el-button type="primary" size="small" style="margin-left: auto" @click="showTagMappingForm()">
                <el-icon><Plus /></el-icon>{{ $t('poc.addMapping') }}
              </el-button>
            </div>
          </template>
          <p class="tip-text">
            {{ $t('poc.tagMappingTip') }}
          </p>
          <el-table :data="tagMappings" stripe v-loading="tagMappingLoading" max-height="500">
            <el-table-column prop="appName" :label="$t('poc.appName')" width="180" />
            <el-table-column prop="nucleiTags" :label="$t('poc.pocTags')" min-width="250">
              <template #default="{ row }">
                <el-tag v-for="tag in row.nucleiTags" :key="tag" size="small" style="margin-right: 5px">
                  {{ tag }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="description" :label="$t('poc.description')" min-width="150" />
            <el-table-column prop="enabled" :label="$t('poc.status')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? $t('poc.enabled') : $t('poc.disabled') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('poc.operation')" width="120">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="showTagMappingForm(row)">{{ $t('poc.edit') }}</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteTagMapping(row)">{{ $t('poc.delete') }}</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <!-- 自定义POC -->
      <el-tab-pane :label="$t('poc.customPoc')" name="customPoc">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('poc.customNucleiPoc') }}</span>
              <span class="card-header-hint">
                {{ $t('poc.totalPocs', { count: pocPagination.total || 0 }) }}
              </span>
              <div style="margin-left: auto">
                <el-button type="danger" size="small" @click="handleClearAllPocs" :loading="clearPocLoading" style="margin-right: 10px">
                  <el-icon><Delete /></el-icon>{{ $t('poc.clearPoc') }}
                </el-button>
                <el-button type="warning" size="small" @click="handleExportPocs" :loading="exportPocLoading" style="margin-right: 10px">
                  <el-icon><Download /></el-icon>{{ $t('poc.exportPoc') }}
                </el-button>
                <el-button type="success" size="small" @click="showImportPocDialog" style="margin-right: 10px">
                  <el-icon><Upload /></el-icon>{{ $t('poc.importPoc') }}
                </el-button>
                <el-button type="primary" size="small" @click="showCustomPocForm()">
                  <el-icon><Plus /></el-icon>{{ $t('poc.addPoc') }}
                </el-button>
              </div>
            </div>
          </template>
          <!-- 筛选条件（仿默认模板库：每个筛选项带数量统计） -->
          <div class="template-filters">
            <div class="filter-row">
              <div class="filter-row-label">{{ $t('poc.filterSearch') }}</div>
              <div class="filter-row-controls">
                <el-input v-model="customPocFilter.keyword" :placeholder="$t('poc.searchAllPlaceholder')" clearable style="width: 300px" @keyup.enter="handlePocSearch" />
                <el-select v-model="customPocFilter.tag" :placeholder="$t('poc.filterTag')" clearable filterable style="width: 140px" @change="handlePocSearch">
                  <el-option v-for="t in customPocFacets.tags" :key="t.value" :label="`${t.value} (${t.count})`" :value="t.value" />
                </el-select>
                <el-select v-model="customPocFilter.enabled" :placeholder="$t('poc.allStatus')" clearable style="width: 110px" @change="handlePocSearch">
                  <el-option :label="$t('poc.enabled')" :value="true" />
                  <el-option :label="$t('poc.disabled')" :value="false" />
                </el-select>
                <el-button type="primary" @click="handlePocSearch">{{ $t('common.search') }}</el-button>
                <el-button @click="resetCustomPocFilter">{{ $t('common.reset') }}</el-button>
              </div>
            </div>
            <div class="filter-row">
              <div class="filter-row-label">{{ $t('poc.filterLevel') }}</div>
              <div class="facet-group">
                <button
                  v-for="level in severityLevels"
                  :key="level"
                  type="button"
                  class="facet-chip"
                  :class="[`facet-sev-${level}`, { 'is-active': customPocFilter.severities.includes(level), 'is-disabled': !customPocFilter.severities.includes(level) && pocSeverityFacetCount(level) === 0 }]"
                  :disabled="!customPocFilter.severities.includes(level) && pocSeverityFacetCount(level) === 0"
                  @click="togglePocSeverityFacet(level)"
                >
                  <span class="facet-sev-dot" :class="`dot-${level}`"></span>
                  <span class="facet-name">{{ level }}</span>
                  <span class="facet-count">{{ pocSeverityFacetCount(level) }}</span>
                </button>
              </div>
            </div>
            <div class="filter-row" v-if="customPocFacets.protocols.length">
              <div class="filter-row-label">{{ $t('poc.filterProtocol') }}</div>
              <div class="facet-group">
                <button
                  v-for="p in customPocFacets.protocols"
                  :key="p.value"
                  type="button"
                  class="facet-chip"
                  :class="{ 'is-active': customPocFilter.protocols.includes(p.value) }"
                  @click="togglePocProtocolFacet(p.value)"
                >
                  <span class="facet-name">{{ p.value }}</span>
                  <span class="facet-count">{{ p.count }}</span>
                </button>
              </div>
            </div>
            <div class="filter-row" v-if="customPocFacets.cveTrue || customPocFacets.cveFalse">
              <div class="filter-row-label">CVE</div>
              <div class="facet-group">
                <button
                  type="button"
                  class="facet-chip"
                  :class="{ 'is-active': customPocFilter.hasCve === true }"
                  @click="togglePocCveFacet(true)"
                >
                  <span class="facet-name">{{ $t('poc.cveYes') }}</span>
                  <span class="facet-count">{{ customPocFacets.cveTrue }}</span>
                </button>
                <button
                  type="button"
                  class="facet-chip"
                  :class="{ 'is-active': customPocFilter.hasCve === false }"
                  @click="togglePocCveFacet(false)"
                >
                  <span class="facet-name">{{ $t('poc.cveNo') }}</span>
                  <span class="facet-count">{{ customPocFacets.cveFalse }}</span>
                </button>
              </div>
            </div>
            <div class="filter-row" v-if="customPocFacets.products.length">
              <div class="filter-row-label">{{ $t('poc.filterProduct') }}</div>
              <div class="filter-row-controls">
                <el-select
                  v-model="customPocFilter.products"
                  :placeholder="$t('poc.productPlaceholder')"
                  multiple
                  filterable
                  clearable
                  collapse-tags
                  collapse-tags-tooltip
                  style="width: 320px"
                  @change="handlePocSearch"
                >
                  <el-option v-for="p in customPocFacets.products" :key="p.value" :label="`${p.value} (${p.count})`" :value="p.value" />
                </el-select>
              </div>
            </div>
          </div>
          <el-table :data="customPocs" stripe v-loading="customPocLoading" max-height="500">
            <el-table-column prop="templateId" :label="$t('poc.templateId')" width="200" show-overflow-tooltip />
            <el-table-column prop="name" :label="$t('poc.name')" min-width="160" show-overflow-tooltip />
            <el-table-column prop="severity" :label="$t('poc.severityLevel')" width="90" sortable :sort-method="sortBySeverity">
              <template #default="{ row }">
                <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="protocol" :label="$t('poc.protocol')" width="90">
              <template #default="{ row }">
                <el-tag v-if="row.protocol" effect="plain" size="small">{{ row.protocol }}</el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column prop="cveIds" :label="'CVE'" width="140">
              <template #default="{ row }">
                <el-tag v-if="row.cveIds && row.cveIds.length" type="danger" effect="plain" size="small">
                  {{ row.cveIds[0] }}{{ row.cveIds.length > 1 ? ` +${row.cveIds.length - 1}` : '' }}
                </el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column prop="tags" :label="$t('poc.tags')" min-width="180">
              <template #default="{ row }">
                <el-tag v-for="tag in row.tags" :key="tag" size="small" style="margin-right: 3px">
                  {{ tag }}
                </el-tag>
                <span v-if="row.tags && row.tags.length > 3" class="more-count">+{{ row.tags.length - 3 }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" :label="$t('poc.status')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? $t('poc.enabled') : $t('poc.disabled') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('poc.operation')" width="300">
              <template #default="{ row }">
                <el-button type="success" link size="small" @click="showPocValidateDialog(row)">{{ $t('poc.validate') }}</el-button>
                <el-button type="warning" link size="small" @click="showScanAssetsDialog(row)">{{ $t('poc.scanAssets') }}</el-button>
                <el-button type="primary" link size="small" @click="showCustomPocForm(row)">{{ $t('poc.edit') }}</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteCustomPoc(row)">{{ $t('poc.delete') }}</el-button>
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
      <el-tab-pane :label="$t('poc.dirscanDict')" name="dirscanDict">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('poc.dirscanDictManage') }}</span>
              <span class="card-header-hint">
                {{ $t('poc.totalDicts', { count: dirscanDictPagination.total || 0 }) }}
              </span>
              <div style="margin-left: auto">
                <el-button type="danger" size="small" @click="handleClearDirscanDict" :loading="clearDictLoading" style="margin-right: 10px">
                  <el-icon><Delete /></el-icon>{{ $t('poc.clearCustomDict') }}
                </el-button>
                <el-button type="primary" size="small" @click="showDirscanDictForm()">
                  <el-icon><Plus /></el-icon>{{ $t('poc.addDict') }}
                </el-button>
              </div>
            </div>
          </template>
          <p class="tip-text">
            {{ $t('poc.dirscanDictTip') }}
          </p>
          <el-table :data="dirscanDicts" stripe v-loading="dirscanDictLoading" max-height="500">
            <el-table-column prop="name" :label="$t('poc.dictName')" width="200" />
            <el-table-column prop="description" :label="$t('poc.description')" min-width="200" show-overflow-tooltip />
            <el-table-column prop="pathCount" :label="$t('poc.pathCount')" width="100" />
            <el-table-column prop="isBuiltin" :label="$t('poc.dictType')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">
                  {{ row.isBuiltin ? $t('poc.builtin') : $t('poc.custom') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" :label="$t('poc.status')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? $t('poc.enabled') : $t('poc.disabled') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('poc.operation')" width="150">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="showDirscanDictForm(row)">{{ $t('poc.edit') }}</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteDirscanDict(row)">{{ $t('poc.delete') }}</el-button>
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
      <el-tab-pane :label="$t('poc.subdomainDict')" name="subdomainDict">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('poc.subdomainDictManage') }}</span>
              <span class="text-muted" style="font-size: 13px; margin-left: 10px">
                {{ $t('poc.totalDicts', { count: subdomainDictPagination.total || 0 }) }}
              </span>
              <div style="margin-left: auto">
                <el-button type="danger" size="small" @click="handleClearSubdomainDict" :loading="clearSubdomainDictLoading" style="margin-right: 10px">
                  <el-icon><Delete /></el-icon>{{ $t('poc.clearCustomDict') }}
                </el-button>
                <el-button type="primary" size="small" @click="showSubdomainDictForm()">
                  <el-icon><Plus /></el-icon>{{ $t('poc.addDict') }}
                </el-button>
              </div>
            </div>
          </template>
          <p class="tip-text">
            {{ $t('poc.subdomainDictTip') }}
          </p>
          <el-table :data="subdomainDicts" stripe v-loading="subdomainDictLoading" max-height="500">
            <el-table-column prop="name" :label="$t('poc.dictName')" width="200" />
            <el-table-column prop="description" :label="$t('poc.description')" min-width="200" show-overflow-tooltip />
            <el-table-column prop="wordCount" :label="$t('poc.wordCount')" width="100" />
            <el-table-column prop="isBuiltin" :label="$t('poc.dictType')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">
                  {{ row.isBuiltin ? $t('poc.builtin') : $t('poc.custom') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" :label="$t('poc.status')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                  {{ row.enabled ? $t('poc.enabled') : $t('poc.disabled') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('poc.operation')" width="150">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="showSubdomainDictForm(row)">{{ $t('poc.edit') }}</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteSubdomainDict(row)">{{ $t('poc.delete') }}</el-button>
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

      <!-- 弱口令字典 -->
      <el-tab-pane :label="$t('poc.weakpassDict')" name="weakpassDict">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('poc.weakpassDictManage') }}</span>
              <span class="text-muted" style="font-size: 13px; margin-left: 10px">
                {{ $t('poc.totalDicts', { count: weakpassDictPagination.total || 0 }) }}
              </span>
              <div style="margin-left: auto">
                <el-button type="danger" size="small" @click="handleClearWeakpassDict" :loading="clearWeakpassDictLoading" style="margin-right: 10px">
                  <el-icon><Delete /></el-icon>{{ $t('poc.clearCustomDict') }}
                </el-button>
                <el-button type="primary" size="small" @click="showWeakpassDictForm()">
                  <el-icon><Plus /></el-icon>{{ $t('poc.addDict') }}
                </el-button>
              </div>
            </div>
          </template>
          <p class="tip-text">
            {{ $t('poc.weakpassDictTip') }}
          </p>
          <el-table :data="weakpassDicts" stripe v-loading="weakpassDictLoading" max-height="500">
            <el-table-column prop="name" :label="$t('poc.dictName')" width="180" />
            <el-table-column prop="description" :label="$t('poc.description')" min-width="150" show-overflow-tooltip />
            <el-table-column prop="service" :label="$t('poc.serviceType')" width="100">
              <template #default="{ row }">
                {{ getServiceLabel(row.service) }}
              </template>
            </el-table-column>
            <el-table-column prop="wordCount" :label="$t('poc.wordCount')" width="80" />
            <el-table-column prop="isBuiltin" :label="$t('poc.dictAttribute')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.isBuiltin ? 'info' : 'success'" size="small">
                  {{ row.isBuiltin ? $t('poc.builtin') : $t('poc.custom') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('poc.operation')" width="150">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="showWeakpassDictForm(row)">{{ $t('poc.edit') }}</el-button>
                <el-button type="danger" link size="small" @click="handleDeleteWeakpassDict(row)">{{ $t('poc.delete') }}</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-model:current-page="weakpassDictPagination.page"
            v-model:page-size="weakpassDictPagination.pageSize"
            :total="weakpassDictPagination.total"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            class="pagination"
            @size-change="loadWeakpassDicts"
            @current-change="loadWeakpassDicts"
          />
        </el-card>
      </el-tab-pane>

      <!-- JSFinder 配置 -->
      <el-tab-pane :label="$t('poc.jsfinderConfigTab')" name="jsfinderConfig">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('poc.jsfinderConfig') }}</span>
              <div style="margin-left: auto">
                <el-button size="small" @click="handleResetJSFinderConfig" :loading="jsfinderResetLoading" style="margin-right: 10px">
                  <el-icon><RefreshLeft /></el-icon>{{ $t('poc.jsfinderResetDefault') }}
                </el-button>
                <el-button type="primary" size="small" @click="handleSaveJSFinderConfig" :loading="jsfinderSaveLoading">
                  <el-icon><Check /></el-icon>{{ $t('poc.save') }}
                </el-button>
              </div>
            </div>
          </template>
          <p class="tip-text">{{ $t('poc.jsfinderConfigTip') }}</p>
          <div class="jsfinder-search-bar">
            <el-input
              v-model="jsfinderSearchQuery"
              :placeholder="$t('poc.jsfinderSearchPlaceholder')"
              clearable
              :prefix-icon="Search"
              size="default"
            />
          </div>
          <el-row :gutter="16" v-loading="jsfinderLoading">
            <el-col :xs="24" :md="6" v-for="field in jsfinderFields" :key="field.key" style="margin-bottom: 16px">
              <div class="jsfinder-list-card">
                <div class="jsfinder-list-title">
                  {{ $t(field.labelKey) }}
                  <span v-if="jsfinderSearchQuery && jsfinderMatchCount(field.key) > 0" class="jsfinder-match-badge">
                    {{ $t('poc.jsfinderMatchCount', { count: jsfinderMatchCount(field.key) }) }}
                  </span>
                </div>
                <div class="jsfinder-list-hint">{{ $t(field.hintKey) }}</div>
                <el-input
                  type="textarea"
                  :model-value="jsfinderText(field.key)"
                  @input="val => jsfinderUpdateText(field.key, val)"
                  :autosize="{ minRows: 8, maxRows: 20 }"
                />
                <div v-if="jsfinderSearchQuery && getMatchedLines(field.key).length" class="jsfinder-match-strip">
                  <span
                    v-for="(line, i) in getMatchedLines(field.key)"
                    :key="i"
                    class="jsfinder-match-chip"
                    v-html="highlightLine(line, jsfinderSearchQuery)"
                  />
                </div>
              </div>
            </el-col>
          </el-row>
        </el-card>
      </el-tab-pane>
      </el-tabs>
      <div class="tabs-action-buttons">
        <el-button type="warning" size="small" @click="showGlobalBatchValidateDialog">
          <el-icon><Search /></el-icon>{{ $t('poc.batchValidate') }}
        </el-button>
      </div>
    </div>

    <!-- 目录扫描字典编辑对话框 -->
    <el-dialog v-model="dirscanDictDialogVisible" :title="dirscanDictForm.id ? $t('poc.editDict') : $t('poc.addDictTitle')" width="700px">
      <el-form ref="dirscanDictFormRef" :model="dirscanDictForm" :rules="dirscanDictRules" label-width="100px">
        <el-form-item :label="$t('poc.dictNameLabel')" prop="name">
          <el-input v-model="dirscanDictForm.name" :placeholder="$t('poc.dictNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.descriptionLabel')">
          <el-input v-model="dirscanDictForm.description" :placeholder="$t('poc.descriptionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.pathListLabel')" prop="content">
          <div style="width: 100%">
            <div class="text-muted hint-text">
              {{ $t('poc.pathListHint') }}
            </div>
            <el-input
              v-model="dirscanDictForm.content"
              type="textarea"
              :rows="15"
              placeholder="/admin&#10;/login&#10;/api&#10;/backup&#10;/.git&#10;/config"
            />
            <div class="text-muted hint-text" style="margin-top: 8px">
              {{ $t('poc.currentPathCount') }}: {{ countDictPaths(dirscanDictForm.content) }}
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="$t('poc.enableLabel')">
          <el-switch v-model="dirscanDictForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dirscanDictDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
        <el-button type="primary" @click="handleSaveDirscanDict">{{ $t('poc.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 子域名字典编辑对话框 -->
    <el-dialog v-model="subdomainDictDialogVisible" :title="subdomainDictForm.id ? $t('poc.editDict') : $t('poc.addDictTitle')" width="700px">
      <el-form ref="subdomainDictFormRef" :model="subdomainDictForm" :rules="subdomainDictRules" label-width="100px">
        <el-form-item :label="$t('poc.dictNameLabel')" prop="name">
          <el-input v-model="subdomainDictForm.name" :placeholder="$t('poc.dictNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.descriptionLabel')">
          <el-input v-model="subdomainDictForm.description" :placeholder="$t('poc.descriptionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.wordListLabel')" prop="content">
          <div style="width: 100%">
            <div class="text-muted hint-text">
              {{ $t('poc.wordListHint') }}
            </div>
            <el-input
              v-model="subdomainDictForm.content"
              type="textarea"
              :rows="15"
              placeholder="www&#10;mail&#10;ftp&#10;admin&#10;api&#10;dev&#10;test"
            />
            <div class="text-muted hint-text" style="margin-top: 8px">
              {{ $t('poc.currentWordCount') }}: {{ countSubdomainWords(subdomainDictForm.content) }}
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="$t('poc.enableLabel')">
          <el-switch v-model="subdomainDictForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="subdomainDictDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
        <el-button type="primary" @click="handleSaveSubdomainDict">{{ $t('poc.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 弱口令字典编辑对话框 -->
    <el-dialog v-model="weakpassDictDialogVisible" :title="weakpassDictForm.id ? $t('poc.editDict') : $t('poc.addDictTitle')" width="700px">
      <el-form ref="weakpassDictFormRef" :model="weakpassDictForm" :rules="weakpassDictRules" label-width="100px">
        <el-form-item :label="$t('poc.dictNameLabel')" prop="name">
          <el-input v-model="weakpassDictForm.name" :placeholder="$t('poc.dictNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.descriptionLabel')">
          <el-input v-model="weakpassDictForm.description" :placeholder="$t('poc.descriptionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.serviceType')" prop="service">
          <el-select v-model="weakpassDictForm.service" :placeholder="$t('poc.selectServiceType')" style="width: 200px">
            <el-option v-for="opt in serviceOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('poc.wordListLabel')" prop="content">
          <div style="width: 100%">
            <div class="text-muted hint-text">
              {{ $t('poc.weakpassWordListHint') }}
            </div>
            <el-input
              v-model="weakpassDictForm.content"
              type="textarea"
              :rows="15"
              :placeholder="'root:password\nadmin:123456'"
            />
            <div class="text-muted hint-text" style="margin-top: 8px">
              {{ $t('poc.currentWordCount') }}: {{ countWeakpassWords(weakpassDictForm.content) }}
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="$t('poc.enableLabel')">
          <el-switch v-model="weakpassDictForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="weakpassDictDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
        <el-button type="primary" @click="handleSaveWeakpassDict">{{ $t('poc.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 标签映射编辑对话框 -->
    <el-dialog v-model="tagMappingDialogVisible" :title="tagMappingForm.id ? $t('poc.editMapping') : $t('poc.addMappingTitle')" width="500px">
      <el-form ref="tagMappingFormRef" :model="tagMappingForm" :rules="tagMappingRules" label-width="100px">
        <el-form-item :label="$t('poc.appNameLabel')" prop="appName">
          <el-input v-model="tagMappingForm.appName" :placeholder="$t('poc.appNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.nucleiTagsLabel')" prop="nucleiTagsInput">
          <el-input 
            v-model="tagMappingForm.nucleiTagsInput" 
            :placeholder="$t('poc.nucleiTagsPlaceholder')"
            style="width: 100%"
          />
          <div class="text-muted hint-text" style="margin-top: 4px;">
            {{ $t('poc.commonTags') }}
          </div>
        </el-form-item>
        <el-form-item :label="$t('poc.descriptionLabel')">
          <el-input v-model="tagMappingForm.description" :placeholder="$t('poc.descriptionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.enableLabel')">
          <el-switch v-model="tagMappingForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tagMappingDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
        <el-button type="primary" @click="handleSaveTagMapping">{{ $t('poc.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 自定义POC编辑对话框 -->
    <el-dialog v-model="customPocDialogVisible" :title="customPocForm.id ? $t('poc.editPoc') : $t('poc.addPocTitle')" width="900px">
      <el-form ref="customPocFormRef" :model="customPocForm" :rules="customPocRules" label-width="100px">
        <el-form-item :label="$t('poc.yamlContent')" prop="content">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; display: flex; justify-content: space-between; align-items: center;">
              <span class="text-muted hint-text">{{ $t('poc.yamlHint') }}</span>
              <el-button v-if="isAdmin" type="primary" size="small" @click="showAiAssistDialog" :icon="MagicStick">{{ $t('poc.aiAssist') }}</el-button>
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
        <el-divider content-position="left">{{ $t('poc.parseResult') }}</el-divider>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item :label="$t('poc.templateIdLabel')" prop="templateId">
              <el-input v-model="customPocForm.templateId" :placeholder="$t('poc.templateIdParsed')" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('poc.nameLabel')" prop="name">
              <el-input v-model="customPocForm.name" :placeholder="$t('poc.nameParsed')" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item :label="$t('poc.severityLabel')" prop="severity">
              <el-select v-model="customPocForm.severity" style="width: 100%">
                <el-option label="Critical" value="critical" />
                <el-option label="High" value="high" />
                <el-option label="Medium" value="medium" />
                <el-option label="Low" value="low" />
                <el-option label="Info" value="info" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('poc.authorLabel')">
              <el-input v-model="customPocForm.author" :placeholder="$t('poc.authorParsed')" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="$t('poc.tagsLabel')">
          <el-input 
            v-model="customPocForm.tagsInput" 
            :placeholder="$t('poc.tagsParsed')"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item :label="$t('poc.descriptionLabel')">
          <el-input v-model="customPocForm.description" type="textarea" :rows="2" :placeholder="$t('poc.descriptionParsed')" />
        </el-form-item>
        <el-form-item :label="$t('poc.enableLabel')">
          <el-switch v-model="customPocForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="customPocDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
        <el-button @click="parseYamlContent">{{ $t('poc.reparseYaml') }}</el-button>
        <el-button type="success" @click="handleValidatePocSyntax" :loading="syntaxValidating">{{ $t('poc.validateSyntax') }}</el-button>
        <el-button type="primary" @click="handleSaveCustomPoc">{{ $t('poc.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- AI辅助编写POC对话框 -->
    <el-dialog v-model="aiAssistDialogVisible" :title="$t('poc.aiAssistTitle')" width="700px">
      <!-- AI服务配置已迁移至 系统管理 > AI配置 -->
      <el-alert type="info" :closable="false" style="margin-bottom: 15px">
        <template #title>
          {{ $t('poc.aiConfigMovedHint') }}
          <router-link to="/ai-config" class="ai-config-link">{{ $t('navigation.aiConfig') }}</router-link>
        </template>
      </el-alert>

      <el-form label-width="100px">
        <el-form-item :label="$t('poc.vulnDescription')">
          <el-input
            v-model="aiAssistForm.description"
            type="textarea"
            :rows="4"
            :placeholder="$t('poc.vulnDescPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="$t('poc.vulnType')">
          <el-select v-model="aiAssistForm.vulnType" :placeholder="$t('poc.selectVulnType')" style="width: 100%">
            <el-option :label="$t('poc.vulnTypeSqli')" value="sqli" />
            <el-option :label="$t('poc.vulnTypeXss')" value="xss" />
            <el-option :label="$t('poc.vulnTypeRce')" value="rce" />
            <el-option :label="$t('poc.vulnTypeLfi')" value="lfi" />
            <el-option :label="$t('poc.vulnTypeSsrf')" value="ssrf" />
            <el-option :label="$t('poc.vulnTypeUnauth')" value="unauth" />
            <el-option :label="$t('poc.vulnTypeInfoDisclosure')" value="info-disclosure" />
            <el-option :label="$t('poc.vulnTypeCve')" value="cve" />
            <el-option :label="$t('poc.vulnTypeOther')" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('poc.cveId')" v-if="aiAssistForm.vulnType === 'cve'">
          <el-input v-model="aiAssistForm.cveId" :placeholder="$t('poc.cveIdPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('poc.referenceInfo')">
          <el-input
            v-model="aiAssistForm.reference"
            type="textarea"
            :rows="2"
            :placeholder="$t('poc.referencePlaceholder')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="aiAssistDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
        <el-button type="primary" @click="generatePocWithAi" :loading="aiGenerating">{{ $t('poc.generatePoc') }}</el-button>
      </template>
    </el-dialog> 

    <!-- 导入POC对话框 -->
    <el-dialog
      v-model="importPocDialogVisible"
      :title="$t('poc.importPocTitle')"
      width="550px"
      :close-on-click-modal="!importPocLoading"
      :close-on-press-escape="!importPocLoading"
      :show-close="!importPocLoading"
    >
      <!-- 导入中显示进度 -->
      <div v-if="importPocLoading" class="download-progress">
        <el-progress
          :percentage="importPocProgress"
          :status="importPocStatus === 'failed' ? 'exception' : (importPocStatus === 'completed' ? 'success' : '')"
          :stroke-width="20"
          striped
          striped-flow
        />
        <div class="progress-info">
          <span v-if="importPocStatus === 'extracting'">{{ $t('poc.extracting') }}</span>
          <span v-else-if="importPocStatus === 'importing'">{{ $t('poc.importingPocs') }}</span>
          <span v-else-if="importPocStatus === 'completed'">{{ $t('poc.importCompleted') }}</span>
          <span v-else-if="importPocStatus === 'failed'" class="error-text">{{ importPocError }}</span>
          <span v-if="uploadedFileCount > 0" class="template-count">
            {{ $t('poc.scannedFiles', { count: uploadedFileCount }) }}
          </span>
        </div>
      </div>

      <!-- 上传ZIP包 -->
      <template v-else>
        <div class="upload-section">
          <el-checkbox v-model="importPocEnabled" style="margin-bottom: 12px">{{ $t('poc.enableAfterImport') }}</el-checkbox>
          <el-upload
            ref="importPocZipUploadRef"
            drag
            :auto-upload="false"
            :limit="1"
            accept=".zip"
            :on-change="handleImportPocZipChange"
            :on-exceed="handleImportPocZipExceed"
          >
            <el-icon class="el-icon--upload"><Upload /></el-icon>
            <div class="el-upload__text">
              {{ $t('poc.dragZipHere') }} <em>{{ $t('poc.clickToSelect') }}</em>
            </div>
            <template #tip>
              <div class="el-upload__tip">
                {{ $t('poc.pocZipTip') }}
              </div>
            </template>
          </el-upload>
          <div v-if="importPocZipFile" class="selected-file">
            <el-tag type="success">{{ importPocZipFile.name }} ({{ formatFileSize(importPocZipFile.size) }})</el-tag>
          </div>
        </div>
      </template>

      <template #footer>
        <template v-if="importPocStatus === 'completed'">
          <el-button type="primary" @click="handleImportPocComplete">{{ $t('poc.done') }}</el-button>
        </template>
        <template v-else-if="importPocStatus === 'failed'">
          <el-button @click="resetImportPocDialog">{{ $t('poc.retry') }}</el-button>
        </template>
        <template v-else-if="!importPocLoading">
          <el-button @click="importPocDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
          <el-button type="primary" @click="handleImportPocZip" :disabled="!importPocZipFile">
            {{ $t('poc.startImport') }}
          </el-button>
        </template>
        <template v-else>
          <el-button disabled>{{ $t('poc.processing') }}...</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- 查看模板内容对话框 -->
    <el-dialog v-model="templateContentDialogVisible" :title="currentTemplate.name || $t('poc.templateContent')" width="900px">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item :label="$t('poc.templateId')">{{ currentTemplate.id }}</el-descriptions-item>
        <el-descriptions-item :label="$t('poc.severityLevel')">
          <el-tag :type="getSeverityType(currentTemplate.severity)" size="small">{{ currentTemplate.severity }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('poc.protocol')">{{ currentTemplate.protocol || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('poc.category')">{{ currentTemplate.category || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('poc.product')">{{ currentTemplate.product || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('poc.vendor')">{{ currentTemplate.vendor || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="'CVE'" :span="2">
          <template v-if="currentTemplate.cveIds && currentTemplate.cveIds.length">
            <el-tag v-for="cve in currentTemplate.cveIds" :key="cve" type="danger" effect="plain" size="small" style="margin-right: 5px">{{ cve }}</el-tag>
          </template>
          <span v-else>-</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('poc.author')">{{ currentTemplate.author || '-' }}</el-descriptions-item>
        <el-descriptions-item label="CVSS">{{ currentTemplate.cvssScore || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('poc.tags')" :span="2">
          <el-tag v-for="tag in (currentTemplate.tags || [])" :key="tag" size="small" style="margin-right: 5px">{{ tag }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('poc.description')" :span="2">{{ currentTemplate.description || '-' }}</el-descriptions-item>
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
        <el-button @click="templateContentDialogVisible = false">{{ $t('poc.close') }}</el-button>
        <el-button type="primary" @click="copyTemplateContent">{{ $t('poc.copyContent') }}</el-button>
      </template>
    </el-dialog>

    <!-- 同步Nuclei模板库对话框 -->
    <el-dialog 
      v-model="downloadTemplateDialogVisible" 
      :title="$t('poc.syncTemplateLib')" 
      width="550px"
      :close-on-click-modal="!downloadTemplateLoading"
      :close-on-press-escape="!downloadTemplateLoading"
      :show-close="!downloadTemplateLoading"
    >
      <!-- 处理中显示进度 -->
      <div v-if="downloadTemplateLoading" class="download-progress">
        <el-progress 
          :percentage="downloadProgress" 
          :status="downloadStatus === 'failed' ? 'exception' : (downloadStatus === 'completed' ? 'success' : '')"
          :stroke-width="20"
          striped
          striped-flow
        />
        <div class="progress-info">
          <span v-if="downloadStatus === 'downloading'">{{ $t('poc.downloadInProgress') }}</span>
          <span v-else-if="downloadStatus === 'extracting'">{{ $t('poc.extracting') }}</span>
          <span v-else-if="downloadStatus === 'completed'">{{ $t('poc.downloadCompleted') }}</span>
          <span v-else-if="downloadStatus === 'failed'" class="error-text">{{ downloadError }}</span>
          <span v-if="downloadTemplateCount > 0" class="template-count">
            {{ $t('poc.downloadedTemplates', { count: downloadTemplateCount }) }}
          </span>
        </div>
      </div>
      
      <!-- 上传ZIP包 -->
      <template v-else>
        <div class="upload-section">
          <el-upload
            ref="zipUploadRef"
            drag
            :auto-upload="false"
            :limit="1"
            accept=".zip"
            :on-change="handleZipFileChange"
            :on-exceed="handleZipExceed"
          >
            <el-icon class="el-icon--upload"><Upload /></el-icon>
            <div class="el-upload__text">
              {{ $t('poc.dragZipHere') }} <em>{{ $t('poc.clickToSelect') }}</em>
            </div>
            <template #tip>
              <div class="el-upload__tip">
                {{ $t('poc.zipTip') }}
              </div>
            </template>
          </el-upload>
          <div v-if="selectedZipFile" class="selected-file">
            <el-tag type="success">{{ selectedZipFile.name }} ({{ formatFileSize(selectedZipFile.size) }})</el-tag>
          </div>
        </div>
      </template>
      
      <template #footer>
        <template v-if="downloadStatus === 'completed'">
          <el-button type="primary" @click="handleUploadComplete">{{ $t('poc.done') }}</el-button>
        </template>
        <template v-else-if="downloadStatus === 'failed'">
          <el-button @click="resetDownloadDialog">{{ $t('poc.retry') }}</el-button>
        </template>
        <template v-else-if="!downloadTemplateLoading">
          <el-button @click="downloadTemplateDialogVisible = false">{{ $t('poc.cancel') }}</el-button>
          <el-button type="primary" @click="handleUploadZip" :disabled="!selectedZipFile">
            {{ $t('poc.startImport') }}
          </el-button>
        </template>
        <template v-else>
          <el-button disabled>{{ $t('poc.processing') }}...</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- POC验证对话框 -->
    <el-dialog v-model="pocValidateDialogVisible" :title="$t('poc.validatePoc')" width="700px" @close="handleValidateDialogClose">
      <el-form label-width="80px">
        <el-form-item :label="$t('poc.pocName')">
          <el-input :value="validatePoc.name" disabled />
        </el-form-item>
        <el-form-item :label="$t('poc.templateId')">
          <el-input :value="validatePoc.templateId" disabled />
        </el-form-item>
        <el-form-item :label="$t('poc.targetUrl')">
          <el-input v-model="pocValidateUrl" :placeholder="$t('poc.targetUrlPlaceholder')" />
        </el-form-item>
      </el-form>
      
      <!-- 执行日志区域 -->
      <div v-if="pocValidateLoading || pocValidateLogs.length > 0" class="validate-logs">
        <div class="logs-header">
          <span>{{ $t('poc.executionLog') }}</span>
          <el-checkbox v-model="pocValidateIncludeDebug" size="small" @change="restartValidationLogs">{{ $t('common.includeDebug') }}</el-checkbox>
          <el-tag v-if="pocValidateLoading" type="warning" size="small">{{ $t('poc.executing') }}</el-tag>
          <el-tag v-else-if="pocValidateResult && pocValidateResult.matched" type="success" size="small">{{ $t('poc.vulnFound') }}</el-tag>
          <el-tag v-else-if="pocValidateResult" type="info" size="small">{{ $t('poc.completed') }}</el-tag>
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
            {{ pocValidateResult.matched ? '✓ ' + $t('poc.vulnFound') : '✗ ' + $t('poc.validateCompleteNoVuln') }}
          </el-tag>
          <el-tag :type="getSeverityType(pocValidateResult.severity)" size="small" style="margin-left: 10px">
            {{ pocValidateResult.severity }}
          </el-tag>
        </div>
        <pre class="result-details">{{ pocValidateResult.details }}</pre>
      </div>
      <template #footer>
        <el-button @click="pocValidateDialogVisible = false">{{ $t('poc.close') }}</el-button>
        <el-button type="primary" @click="handleValidatePoc" :loading="pocValidateLoading" :disabled="!pocValidateUrl">{{ $t('poc.validate') }}</el-button>
      </template>
    </el-dialog>

    <!-- 全局批量验证POC对话框 -->
    <el-dialog v-model="globalBatchValidateDialogVisible" :title="$t('poc.batchValidatePoc')" width="650px">
      <el-form label-width="100px">
        <el-form-item :label="$t('poc.selectPocType')">
          <el-radio-group v-model="globalBatchScope">
            <el-radio label="all">{{ $t('poc.allPoc') }}</el-radio>
            <el-radio label="template">{{ $t('poc.defaultTemplates') }}</el-radio>
            <el-radio label="custom">{{ $t('poc.customPoc') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="$t('poc.targetUrlLabel')">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 10px;">
              <el-radio-group v-model="globalBatchTargetInputType" size="small">
                <el-radio-button value="text">{{ $t('poc.textInput') }}</el-radio-button>
                <el-radio-button value="file">{{ $t('poc.fileUpload') }}</el-radio-button>
              </el-radio-group>
              <span class="text-muted hint-text">
                {{ globalBatchTargetInputType === 'text' ? $t('poc.oneUrlPerLine') : $t('poc.supportsTxtFile') }}
              </span>
            </div>
            <el-input 
              v-if="globalBatchTargetInputType === 'text'"
              v-model="globalBatchValidateUrls" 
              type="textarea" 
              :rows="5" 
              :placeholder="$t('poc.targetUrlsPlaceholder')"
            />
            <el-upload
              v-else
              ref="globalBatchUrlUploadRef"
              :auto-upload="false"
              :limit="1"
              accept=".txt"
              :on-change="handleGlobalBatchUrlFileChange"
              :on-remove="handleGlobalBatchUrlFileRemove"
              drag
            >
              <el-icon class="el-icon--upload"><upload-filled /></el-icon>
              <div class="el-upload__text">{{ $t('poc.uploadHint') }}</div>
              <template #tip>
                <div class="el-upload__tip">{{ $t('poc.onlyTxtFile') }}</div>
              </template>
            </el-upload>
            <div v-if="globalBatchTargetUrls.length > 0" class="text-success hint-text" style="margin-top: 8px;">
              {{ $t('poc.parsedUrls', { count: globalBatchTargetUrls.length }) }}
            </div>
          </div>
        </el-form-item>
      </el-form>
      <el-alert type="info" :closable="false" show-icon style="margin-top: 10px;">
        <template #title>提交后任务将在后台运行，可在页面顶部查看进度，完成后点击「查看结果」查看详情。</template>
      </el-alert>
      <template #footer>
        <el-button @click="globalBatchValidateDialogVisible = false">{{ $t('poc.close') }}</el-button>
        <el-button type="primary" @click="handleGlobalBatchValidate" :loading="globalBatchValidateLoading" :disabled="globalBatchTargetUrls.length === 0">
          开始验证
        </el-button>
      </template>
    </el-dialog>

    <!-- 全局批量验证结果对话框 -->
    <el-dialog v-model="globalBatchResultDialogVisible" title="POC批量验证结果" width="900px">
      <div v-if="globalBatchValidateResults.length > 0" class="batch-validate-results">
        <div class="results-header" style="margin-bottom: 15px;">
          <el-tag type="danger" size="large">发现漏洞: {{ globalBatchValidateResults.filter(r => r.matched).length }}</el-tag>
          <el-tag type="info" size="large" style="margin-left: 8px;">未匹配: {{ globalBatchValidateResults.filter(r => !r.matched).length }}</el-tag>
          <el-dropdown style="margin-left: auto" @command="handleGlobalExportResults">
            <el-button type="success" size="small">
              导出结果<el-icon class="el-icon--right"><arrow-down /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="all">{{ $t('poc.exportAll') }}</el-dropdown-item>
                <el-dropdown-item command="matched">{{ $t('poc.exportMatched') }}</el-dropdown-item>
                <el-dropdown-item command="csv">{{ $t('poc.exportCsv') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
        <el-table :data="globalBatchValidateResults" max-height="400" stripe>
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
      <div v-else>
        <el-empty description="暂无结果" :image-size="80" />
      </div>
      <template #footer>
        <el-button @click="globalBatchResultDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 默认模板批量验证对话框 -->
    <el-dialog v-model="templateBatchValidateDialogVisible" :title="$t('poc.batchValidatePoc')" width="900px" @close="handleBatchValidateDialogClose">
      <el-form label-width="100px">
        <el-form-item :label="$t('poc.selectedTemplates')">
          <div class="selected-templates">
            <el-tag v-for="tpl in selectedTemplates.slice(0, 10)" :key="tpl.id" size="small" style="margin-right: 5px; margin-bottom: 5px">
              {{ tpl.name || tpl.id }}
            </el-tag>
            <span v-if="selectedTemplates.length > 10" class="text-muted">+{{ selectedTemplates.length - 10 }}</span>
          </div>
        </el-form-item>
        <el-form-item :label="$t('poc.targetUrlLabel')">
          <div style="width: 100%">
            <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 10px;">
              <el-radio-group v-model="batchTargetInputType" size="small">
                <el-radio-button value="text">{{ $t('poc.textInput') }}</el-radio-button>
                <el-radio-button value="file">{{ $t('poc.fileUpload') }}</el-radio-button>
              </el-radio-group>
              <span class="text-muted hint-text">
                {{ batchTargetInputType === 'text' ? $t('poc.oneUrlPerLine') : $t('poc.supportsTxtFile') }}
              </span>
            </div>
            <el-input 
              v-if="batchTargetInputType === 'text'"
              v-model="templateBatchValidateUrls" 
              type="textarea" 
              :rows="5" 
              :placeholder="$t('poc.targetUrlsPlaceholder')"
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
              <div class="el-upload__text">{{ $t('poc.uploadHint') }}</div>
              <template #tip>
                <div class="el-upload__tip">{{ $t('poc.onlyTxtFile') }}</div>
              </template>
            </el-upload>
            <div v-if="batchTargetUrls.length > 0" class="text-success hint-text" style="margin-top: 8px;">
              {{ $t('poc.parsedUrls', { count: batchTargetUrls.length }) }}
            </div>
          </div>
        </el-form-item>
      </el-form>
      
      <!-- 批量验证进度 -->
      <div v-if="templateBatchValidateLoading || templateBatchValidateResults.length > 0" class="batch-validate-progress">
        <div class="progress-header">
          <span>{{ $t('poc.validateProgress') }}: {{ templateBatchValidateProgress.completed }}/{{ templateBatchValidateProgress.total }}</span>
          <el-progress 
            :percentage="templateBatchValidateProgress.total > 0 ? Math.round(templateBatchValidateProgress.completed / templateBatchValidateProgress.total * 100) : 0" 
            :status="templateBatchValidateLoading ? '' : 'success'"
            style="width: 200px; margin-left: 15px"
          />
        </div>
        
        <!-- 执行日志 -->
        <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 5px;">
          <el-checkbox v-model="batchValidateIncludeDebug" size="small" @change="restartBatchLogs">{{ $t('common.includeDebug') }}</el-checkbox>
        </div>
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
          <span>{{ $t('poc.validateResult') }}</span>
          <el-tag type="danger" size="small" style="margin-left: 10px">
            {{ $t('poc.foundVulns') }}: {{ templateBatchValidateResults.filter(r => r.matched).length }}
          </el-tag>
          <el-tag type="info" size="small" style="margin-left: 5px">
            {{ $t('poc.notMatched') }}: {{ templateBatchValidateResults.filter(r => !r.matched).length }}
          </el-tag>
          <el-dropdown style="margin-left: auto" @command="handleExportResults">
            <el-button type="success" size="small">
              {{ $t('poc.exportResult') }}<el-icon class="el-icon--right"><arrow-down /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="all">{{ $t('poc.exportAll') }}</el-dropdown-item>
                <el-dropdown-item command="matched">{{ $t('poc.exportMatched') }}</el-dropdown-item>
                <el-dropdown-item command="csv">{{ $t('poc.exportCsv') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
        <el-table :data="templateBatchValidateResults" max-height="250" size="small">
          <el-table-column prop="pocName" :label="$t('poc.templateName')" min-width="150" show-overflow-tooltip />
          <el-table-column prop="severity" :label="$t('poc.level')" width="80">
            <template #default="{ row }">
              <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="matched" :label="$t('poc.result')" width="80">
            <template #default="{ row }">
              <el-tag :type="row.matched ? 'danger' : 'info'" size="small">
                {{ row.matched ? $t('poc.matched') : $t('poc.notMatched') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="matchedUrl" :label="$t('poc.matchedUrl')" min-width="200" show-overflow-tooltip />
        </el-table>
      </div>
      
      <template #footer>
        <el-button @click="templateBatchValidateDialogVisible = false">{{ $t('poc.close') }}</el-button>
        <el-button type="primary" @click="handleTemplateBatchValidate" :loading="templateBatchValidateLoading" :disabled="batchTargetUrls.length === 0">
          {{ $t('poc.startValidate') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 扫描现有资产对话框 -->
    <el-dialog v-model="scanAssetsDialogVisible" :title="$t('poc.scanExistingAssets')" width="900px" @close="handleScanAssetsDialogClose">
      <el-descriptions :column="2" border size="small" style="margin-bottom: 15px">
        <el-descriptions-item :label="$t('poc.pocName')">{{ scanAssetsPoc.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('poc.templateId')">{{ scanAssetsPoc.templateId }}</el-descriptions-item>
        <el-descriptions-item :label="$t('poc.severityLevel')">
          <el-tag :type="getSeverityType(scanAssetsPoc.severity)" size="small">{{ scanAssetsPoc.severity }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('poc.tags')">
          <el-tag v-for="tag in (scanAssetsPoc.tags || [])" :key="tag" size="small" style="margin-right: 3px">{{ tag }}</el-tag>
        </el-descriptions-item>
      </el-descriptions>
      
      <div v-if="!scanAssetsStarted" class="scan-assets-tip">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            {{ $t('poc.scanAssetsTip') }}
          </template>
          <template #default>
            <div class="text-muted hint-text" style="margin-top: 5px">
              {{ $t('poc.scanAssetsTipDetail') }}
            </div>
          </template>
        </el-alert>
      </div>
      
      <!-- 扫描进度 -->
      <div v-if="scanAssetsStarted" class="scan-assets-progress">
        <div class="progress-header">
          <span>{{ $t('poc.scanProgress') }}: {{ scanAssetsProgress.completed }}/{{ scanAssetsProgress.total }}</span>
          <el-progress 
            :percentage="scanAssetsProgress.total > 0 ? Math.round(scanAssetsProgress.completed / scanAssetsProgress.total * 100) : 0" 
            :status="scanAssetsLoading ? '' : 'success'"
            style="width: 200px; margin-left: 15px"
          />
          <el-tag v-if="scanAssetsProgress.vulnCount > 0" type="danger" size="small" style="margin-left: 15px">
            {{ $t('poc.foundVulns') }}: {{ scanAssetsProgress.vulnCount }}
          </el-tag>
        </div>
        
        <!-- 执行日志 -->
        <div class="validate-logs" style="margin-top: 15px">
          <div class="logs-header">
            <span>{{ $t('poc.executionLog') }}</span>
            <el-checkbox v-model="scanAssetsIncludeDebug" size="small" @change="restartScanAssetsLogs">{{ $t('common.includeDebug') }}</el-checkbox>
            <el-tag v-if="scanAssetsLoading" type="warning" size="small">{{ $t('poc.scanning') }}</el-tag>
            <el-tag v-else type="success" size="small">{{ $t('poc.scanCompleted') }}</el-tag>
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
        <el-button @click="scanAssetsDialogVisible = false">{{ $t('poc.close') }}</el-button>
        <el-button type="primary" @click="handleScanAssets" :loading="scanAssetsLoading" :disabled="scanAssetsLoading">
          {{ scanAssetsStarted ? $t('poc.rescan') : $t('poc.startScan') }}
        </el-button>
      </template>
    </el-dialog>
    
    <!-- 隐藏的文件选择器 - 放在根级别确保ref正确绑定 -->
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

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import DOMPurify from 'dompurify'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, ArrowDown, UploadFilled, Upload, Download, Delete, MagicStick, FolderOpened, RefreshLeft, Check, Search, Loading, CircleCheck, CircleClose, Close } from '@element-plus/icons-vue'
import { getTagMappingList, saveTagMapping, deleteTagMapping, getCustomPocList, getCustomPocCategories, saveCustomPoc, batchImportCustomPoc, deleteCustomPoc, clearAllCustomPoc, getNucleiTemplateList, getNucleiTemplateCategories, syncNucleiTemplates, clearNucleiTemplates, getNucleiTemplateDetail, validatePoc as validatePocApi, getPocValidationResult, scanAssetsWithPoc, validatePocSyntax, batchValidatePoc } from '@/api/poc'
import { DEFAULT_AI_CONFIG, loadAIConfig, chat, extractYamlBlock } from '@/utils/aiClient'
import { getDirScanDictList, saveDirScanDict, deleteDirScanDict, clearDirScanDict } from '@/api/dirscan'
import { getSubdomainDictList, saveSubdomainDict, deleteSubdomainDict, clearSubdomainDict } from '@/api/subdomain'
import { getWeakpassDictList, saveWeakpassDict, deleteWeakpassDict, clearWeakpassDict } from '@/api/weakpass'
import { getJSFinderConfig, saveJSFinderConfig, resetJSFinderConfig } from '@/api/jsfinder'
import { useUserStore } from '@/stores/user'
import jsYaml from 'js-yaml'
import JSZip from 'jszip'
import { saveAs } from 'file-saver'
import { getTaskLogs } from '@/api/task'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const userStore = useUserStore()

// 有效的tab名称
const validTabs = ['nucleiTemplates', 'tagMapping', 'customPoc', 'dirscanDict', 'subdomainDict', 'weakpassDict', 'jsfinderConfig']

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
const selectedTemplates = ref([])
const templateStats = ref({})
// 严重级别固定展示顺序（官方模板库已无 Unknown 级别）
const severityLevels = ['critical', 'high', 'medium', 'low', 'info']
// 各筛选维度选项 + 数量统计（随筛选条件联动）
const templateFacets = reactive({
  categories: [],
  severities: [],
  protocols: [],
  products: [],
  tags: [],
  cveTrue: 0,
  cveFalse: 0
})
const templateFilter = reactive({
  category: '',
  severities: [],
  protocols: [],
  products: [],
  hasCve: null,
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

// 同步模板（上传ZIP）
const downloadTemplateDialogVisible = ref(false)
const downloadTemplateLoading = ref(false)
const downloadProgress = ref(0)
const downloadStatus = ref('') // extracting/downloading/completed/failed
const downloadTemplateCount = ref(0)
const downloadError = ref('')
const selectedZipFile = ref(null)
const zipUploadRef = ref(null)

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
const tagMappingRules = computed(() => ({
  appName: [{ required: true, message: t('poc.appNamePlaceholder'), trigger: 'blur' }],
  nucleiTagsInput: [{ required: true, message: t('poc.nucleiTagsPlaceholder'), trigger: 'blur' }]
}))

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
const dirscanDictRules = computed(() => ({
  name: [{ required: true, message: t('poc.dictNamePlaceholder'), trigger: 'blur' }],
  content: [{ required: true, message: t('poc.pathListHint'), trigger: 'blur' }]
}))
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
const subdomainDictRules = computed(() => ({
  name: [{ required: true, message: t('poc.dictNamePlaceholder'), trigger: 'blur' }],
  content: [{ required: true, message: t('poc.wordListHint'), trigger: 'blur' }]
}))
const subdomainDictPagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// 弱口令字典
const weakpassDicts = ref([])
const weakpassDictLoading = ref(false)
const weakpassDictDialogVisible = ref(false)
const weakpassDictFormRef = ref()
const clearWeakpassDictLoading = ref(false)
const weakpassDictForm = reactive({
  id: '',
  name: '',
  description: '',
  service: 'common',
  content: '',
  enabled: true
})
const weakpassDictRules = computed(() => ({
  name: [{ required: true, message: t('poc.dictNamePlaceholder'), trigger: 'blur' }],
  service: [{ required: true, message: t('poc.selectServiceType'), trigger: 'change' }],
  content: [{ required: true, message: t('poc.wordListHint'), trigger: 'blur' }]
}))
const weakpassDictPagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// JSFinder 配置
const jsfinderLoading = ref(false)
const jsfinderSaveLoading = ref(false)
const jsfinderResetLoading = ref(false)
const jsfinderLoaded = ref(false)
const jsfinderConfig = reactive({
  highRiskRoutes: [],
  authRequiredKeywords: [],
  sensitiveKeywords: [],
  domainBlacklist: []
})
const jsfinderSearchQuery = ref('')
const jsfinderFields = [
  { key: 'highRiskRoutes', labelKey: 'poc.jsfinderHighRiskRoutes', hintKey: 'poc.jsfinderHighRiskRoutesHint' },
  { key: 'authRequiredKeywords', labelKey: 'poc.jsfinderAuthRequiredKeywords', hintKey: 'poc.jsfinderAuthRequiredKeywordsHint' },
  { key: 'sensitiveKeywords', labelKey: 'poc.jsfinderSensitiveKeywords', hintKey: 'poc.jsfinderSensitiveKeywordsHint' },
  { key: 'domainBlacklist', labelKey: 'poc.jsfinderDomainBlacklist', hintKey: 'poc.jsfinderDomainBlacklistHint' }
]

// 服务类型选项
const serviceOptions = [
  { value: 'common', label: '通用' },
  { value: 'ssh', label: 'SSH' },
  { value: 'mysql', label: 'MySQL' },
  { value: 'redis', label: 'Redis' },
  { value: 'mongodb', label: 'MongoDB' },
  { value: 'postgresql', label: 'PostgreSQL' },
  { value: 'mssql', label: 'MSSQL' },
  { value: 'ftp', label: 'FTP' },
  { value: 'oracle', label: 'Oracle' },
  { value: 'smb', label: 'SMB' },
  { value: 'mqtt', label: 'MQTT' }
]

// AI辅助编写POC
const aiAssistDialogVisible = ref(false)
const aiGenerating = ref(false)
const aiAssistForm = reactive({
  description: '',
  vulnType: '',
  cveId: '',
  reference: ''
})

// AI配置（含明文 apiKey，接口仅对管理员开放，故 AI 辅助能力也限管理员）
const isAdmin = computed(() => userStore.role === 'admin' || userStore.role === 'superadmin')
const aiConfig = ref({ ...DEFAULT_AI_CONFIG })

async function loadAiConfig() {
  if (!isAdmin.value) return
  aiConfig.value = await loadAIConfig()
}

// 自定义POC筛选条件
const customPocFilter = reactive({
  keyword: '',
  severities: [],
  protocols: [],
  products: [],
  hasCve: null,
  tag: '',
  enabled: null
})
// 自定义POC各筛选维度选项 + 数量统计（随筛选条件联动）
const customPocFacets = reactive({
  severities: [],
  protocols: [],
  products: [],
  tags: [],
  cveTrue: 0,
  cveFalse: 0
})

// 导入POC
const importPocDialogVisible = ref(false)
const importPocEnabled = ref(true)
const importPocLoading = ref(false)
const uploadedFileCount = ref(0)
const importPocZipFile = ref(null)
const importPocZipUploadRef = ref(null)
const importPocProgress = ref(0)
const importPocStatus = ref('') // extracting/importing/completed/failed
const importPocError = ref('')
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
const customPocRules = computed(() => ({
  name: [{ required: true, message: t('poc.nameParsed'), trigger: 'blur' }],
  templateId: [{ required: true, message: t('poc.templateIdParsed'), trigger: 'blur' }],
  severity: [{ required: true, message: t('poc.severityLabel'), trigger: 'change' }],
  content: [{ required: true, message: t('poc.yamlContent'), trigger: 'blur' }]
}))
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
const pocValidateIncludeDebug = ref(false)
const logsContainerRef = ref(null)
let logPollTimer = null
let logPollLastCount = 0
let currentTaskId = null

// 扫描现有资产
const scanAssetsDialogVisible = ref(false)
const scanAssetsPoc = ref({})
const scanAssetsLoading = ref(false)
const scanAssetsStarted = ref(false)
const scanAssetsLogs = ref([])
const scanAssetsLogsRef = ref(null)
const scanAssetsIncludeDebug = ref(false)
const scanAssetsProgress = reactive({
  total: 0,
  completed: 0,
  vulnCount: 0
})
let scanAssetsTaskIds = []
let scanAssetsLogPollTimer = null
let scanAssetsLogLastCount = 0
let scanAssetsPollTimer = null

// 默认模板批量验证
const templateBatchValidateDialogVisible = ref(false)
const templateBatchValidateUrls = ref('') // 多行URL文本
const batchTargetInputType = ref('text') // 输入类型: text 或 file
const batchUrlUploadRef = ref(null)
const templateBatchValidateLoading = ref(false)
const templateBatchValidateLogs = ref([])
const templateBatchValidateResults = ref([])
const batchValidateIncludeDebug = ref(false)
const templateBatchValidateProgress = reactive({ total: 0, completed: 0 })
const batchLogsContainerRef = ref(null)
const currentBatchTaskIds = ref([]) // 当前批次的任务ID列表
let batchLogPollTimer = null
let batchLogLastCount = 0
let batchPollTimer = null
let currentBatchId = null

// 全局批量验证（Tab右侧按钮）
const globalBatchValidateDialogVisible = ref(false)
const globalBatchResultDialogVisible = ref(false)
const globalBatchScope = ref('all')
const globalBatchValidateUrls = ref('')
const globalBatchTargetInputType = ref('text')
const globalBatchUrlUploadRef = ref(null)
const globalBatchValidateLoading = ref(false)
const globalBatchValidateLogs = ref([])
const globalBatchValidateResults = ref([])
const globalBatchValidateProgress = reactive({ total: 0, completed: 0 })
const globalBatchLogsContainerRef = ref(null)
let globalBatchPollTimer = null
let globalCurrentBatchId = null
const POC_GLOBAL_BATCH_STORAGE_KEY = 'cscan_poc_global_batch_task'

// 常驻任务状态（页面顶部进度条）
const persistentTask = ref(null)

// 计算属性：解析全局批量URL为数组
const globalBatchTargetUrls = computed(() => {
  if (!globalBatchValidateUrls.value) return []
  const urls = globalBatchValidateUrls.value
    .split('\n')
    .map(url => url.trim())
    .filter(url => url && (url.startsWith('http://') || url.startsWith('https://')))
  return [...new Set(urls)]
})

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
  // 恢复持久化的批量验证任务
  restorePersistentTask()
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
    loadCustomPocFacets()
  } else if (tab === 'dirscanDict' && dirscanDicts.value.length === 0) {
    loadDirscanDicts()
  } else if (tab === 'subdomainDict' && subdomainDicts.value.length === 0) {
    loadSubdomainDicts()
  } else if (tab === 'weakpassDict' && weakpassDicts.value.length === 0) {
    loadWeakpassDicts()
  } else if (tab === 'jsfinderConfig' && !jsfinderLoaded.value) {
    loadJSFinderConfigData()
  }
}

async function loadNucleiTemplateCategories() {
  try {
    const res = await getNucleiTemplateCategories(buildTemplateFilterPayload())
    if (res.code === 0) {
      templateFacets.categories = res.categories || []
      templateFacets.severities = res.severities || []
      templateFacets.protocols = res.protocols || []
      templateFacets.products = res.products || []
      templateFacets.tags = res.tags || []
      templateFacets.cveTrue = res.cveStats?.true || 0
      templateFacets.cveFalse = res.cveStats?.false || 0
      templateStats.value = res.stats || {}
    }
  } catch (e) {
    console.error('Failed to load template categories:', e)
  }
}

function buildTemplateFilterPayload() {
  return {
    category: templateFilter.category,
    tag: templateFilter.tag,
    keyword: templateFilter.keyword,
    severities: [...templateFilter.severities],
    protocols: [...templateFilter.protocols],
    products: [...templateFilter.products],
    hasCve: templateFilter.hasCve
  }
}

async function loadNucleiTemplates() {
  nucleiTemplateLoading.value = true
  try {
    const res = await getNucleiTemplateList({
      ...buildTemplateFilterPayload(),
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

// 应用筛选（重置到第一页，列表与分面计数同时刷新）
function handleTemplateSearch() {
  templatePagination.page = 1
  loadNucleiTemplates()
  loadNucleiTemplateCategories()
}

function severityFacetCount(level) {
  const item = templateFacets.severities.find(s => s.value === level)
  return item ? item.count : 0
}

function toggleSeverityFacet(level) {
  const idx = templateFilter.severities.indexOf(level)
  if (idx >= 0) {
    templateFilter.severities.splice(idx, 1)
  } else {
    templateFilter.severities.push(level)
  }
  handleTemplateSearch()
}

function toggleProtocolFacet(protocol) {
  const idx = templateFilter.protocols.indexOf(protocol)
  if (idx >= 0) {
    templateFilter.protocols.splice(idx, 1)
  } else {
    templateFilter.protocols.push(protocol)
  }
  handleTemplateSearch()
}

// CVE true/false 为互斥选择，再次点击取消
function toggleCveFacet(val) {
  templateFilter.hasCve = templateFilter.hasCve === val ? null : val
  handleTemplateSearch()
}

function resetTemplateFilter() {
  templateFilter.category = ''
  templateFilter.severities = []
  templateFilter.protocols = []
  templateFilter.products = []
  templateFilter.hasCve = null
  templateFilter.tag = ''
  templateFilter.keyword = ''
  handleTemplateSearch()
}

// 打开同步模板对话框
function handleOpenDownloadDialog() {
  resetDownloadDialog()
  downloadTemplateDialogVisible.value = true
}

// 重置上传对话框状态
function resetDownloadDialog() {
  downloadTemplateLoading.value = false
  downloadProgress.value = 0
  downloadStatus.value = ''
  downloadTemplateCount.value = 0
  downloadError.value = ''
  selectedZipFile.value = null
  if (zipUploadRef.value) {
    zipUploadRef.value.clearFiles()
  }
}

// 格式化文件大小
function formatFileSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

// 处理ZIP文件选择
function handleZipFileChange(file) {
  if (file.raw.type !== 'application/zip' && !file.name.endsWith('.zip')) {
    ElMessage.error(t('poc.onlyZipAllowed'))
    zipUploadRef.value?.clearFiles()
    selectedZipFile.value = null
    return
  }
  selectedZipFile.value = file.raw
}

function handleZipExceed() {
  ElMessage.warning(t('poc.onlyOneFile'))
}

// 上传ZIP包并解析
async function handleUploadZip() {
  if (!selectedZipFile.value) return
  
  downloadTemplateLoading.value = true
  downloadStatus.value = 'extracting'
  downloadProgress.value = 10
  
  try {
    const zip = await JSZip.loadAsync(selectedZipFile.value)
    downloadProgress.value = 30
    
    // 查找所有yaml文件
    const yamlFiles = []
    const filePromises = []
    
    zip.forEach((relativePath, zipEntry) => {
      if (!zipEntry.dir && (relativePath.endsWith('.yaml') || relativePath.endsWith('.yml'))) {
        // 跳过隐藏文件和特殊目录
        if (relativePath.includes('/.') || relativePath.startsWith('.')) return
        filePromises.push(
          zipEntry.async('string').then(content => {
            yamlFiles.push({ path: relativePath, content })
          })
        )
      }
    })
    
    await Promise.all(filePromises)
    downloadProgress.value = 60
    downloadTemplateCount.value = yamlFiles.length
    
    if (yamlFiles.length === 0) {
      downloadStatus.value = 'failed'
      downloadError.value = t('poc.noTemplatesInZip')
      downloadTemplateLoading.value = false
      return
    }
    
    // 批量同步到数据库
    downloadStatus.value = 'downloading'
    downloadProgress.value = 70

    const batchSize = 200
    let successCount = 0

    for (let i = 0; i < yamlFiles.length; i += batchSize) {
      const batch = yamlFiles.slice(i, i + batchSize)
      const res = await syncNucleiTemplates({
        templates: batch,
        force: i === 0
      })
      if (res.code === 0) {
        successCount += res.successCount || batch.length
      }
      downloadProgress.value = 70 + Math.floor((i / yamlFiles.length) * 25)
    }
    
    downloadProgress.value = 100
    downloadStatus.value = 'completed'
    downloadTemplateCount.value = successCount
    downloadTemplateLoading.value = false
    
  } catch (error) {
    console.error('解析ZIP失败:', error)
    downloadStatus.value = 'failed'
    downloadError.value = t('poc.zipParseFailed') + ': ' + (error.message || '')
    downloadTemplateLoading.value = false
  }
}

// 上传完成后关闭对话框并刷新模板列表
function handleUploadComplete() {
  downloadTemplateDialogVisible.value = false
  resetDownloadDialog()
  ElMessage.success(t('poc.syncSuccess'))
  loadNucleiTemplateCategories()
  loadNucleiTemplates()
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
    await ElMessageBox.confirm(t('poc.confirmClearTemplates'), t('common.warning'), { 
      type: 'error', 
      confirmButtonText: t('poc.confirmClearTemplatesBtn'), 
      cancelButtonText: t('poc.cancel') 
    })
  } catch {
    return
  }
  
  syncLoading.value = true
  try {
    const res = await clearNucleiTemplates()
    if (res.code === 0) {
      ElMessage.success(res.msg || t('poc.clearSuccess'))
      loadNucleiTemplateCategories()
      loadNucleiTemplates()
    } else {
      ElMessage.error(res.msg || t('poc.clearFailed'))
    }
  } catch (e) {
    ElMessage.error(t('poc.clearFailed') + ': ' + e.message)
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
      ElMessage.success(t('poc.copiedToClipboard'))
    } else {
      ElMessage.error(t('poc.copyFailed'))
    }
  } catch (err) {
    console.error('复制失败:', err)
    ElMessage.error(t('poc.copyFailed'))
  }
}

function copyTemplateContent() {
  if (currentTemplate.value.content) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(currentTemplate.value.content).then(() => {
        ElMessage.success(t('poc.copiedToClipboard'))
      }).catch(() => {
        fallbackCopyToClipboard(currentTemplate.value.content)
      })
    } else {
      fallbackCopyToClipboard(currentTemplate.value.content)
    }
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

function buildCustomPocFilterPayload() {
  return {
    tag: customPocFilter.tag,
    keyword: customPocFilter.keyword,
    enabled: customPocFilter.enabled,
    severities: [...customPocFilter.severities],
    protocols: [...customPocFilter.protocols],
    products: [...customPocFilter.products],
    hasCve: customPocFilter.hasCve
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
    console.error('Failed to load custom poc facets:', e)
  }
}

// 应用筛选（重置到第一页，列表与分面计数同时刷新）
function handlePocSearch() {
  pocPagination.page = 1
  loadCustomPocs()
  loadCustomPocFacets()
}

function pocSeverityFacetCount(level) {
  const item = customPocFacets.severities.find(s => s.value === level)
  return item ? item.count : 0
}

function togglePocSeverityFacet(level) {
  const idx = customPocFilter.severities.indexOf(level)
  if (idx >= 0) {
    customPocFilter.severities.splice(idx, 1)
  } else {
    customPocFilter.severities.push(level)
  }
  handlePocSearch()
}

function togglePocProtocolFacet(protocol) {
  const idx = customPocFilter.protocols.indexOf(protocol)
  if (idx >= 0) {
    customPocFilter.protocols.splice(idx, 1)
  } else {
    customPocFilter.protocols.push(protocol)
  }
  handlePocSearch()
}

// CVE true/false 为互斥选择，再次点击取消
function togglePocCveFacet(val) {
  customPocFilter.hasCve = customPocFilter.hasCve === val ? null : val
  handlePocSearch()
}

async function loadCustomPocs() {
  customPocLoading.value = true
  try {
    const res = await getCustomPocList({
      ...buildCustomPocFilterPayload(),
      page: pocPagination.page,
      pageSize: pocPagination.pageSize
    })
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
  customPocFilter.keyword = ''
  customPocFilter.severities = []
  customPocFilter.protocols = []
  customPocFilter.products = []
  customPocFilter.hasCve = null
  customPocFilter.tag = ''
  customPocFilter.enabled = null
  handlePocSearch()
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
    ElMessage.success(t('poc.saveSuccess'))
    tagMappingDialogVisible.value = false
    loadTagMappings()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleDeleteTagMapping(row) {
  await ElMessageBox.confirm(t('poc.confirmDeleteMapping'), t('common.tip'), { type: 'warning' })
  const res = await deleteTagMapping({ id: row.id })
  if (res.code === 0) {
    ElMessage.success(t('poc.deleteSuccess'))
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
    ElMessage.warning(t('poc.pleaseEnterPocContent'))
    return
  }
  
  syntaxValidating.value = true
  try {
    const res = await validatePocSyntax({ content: customPocForm.content })
    if (res.code === 0) {
      if (res.valid) {
        ElMessage.success(t('poc.syntaxValidatePass'))
      } else {
        ElMessage.error(t('poc.syntaxError') + ': ' + res.error)
      }
    } else {
      ElMessage.error(res.msg || t('poc.validateFailed'))
    }
  } catch (e) {
    console.error('验证POC语法失败:', e)
    ElMessage.error(t('poc.validateFailed') + ': ' + e.message)
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
    ElMessage.success(t('poc.saveSuccess'))
    customPocDialogVisible.value = false
    // 保存成功后清除AI生成的缓存
    aiGeneratedPocCache.value = ''
    handlePocSearch()
  } else {
    ElMessage.error(res.msg)
  }
}

async function handleDeleteCustomPoc(row) {
  await ElMessageBox.confirm(t('poc.confirmDeletePoc'), t('common.tip'), { type: 'warning' })
  const res = await deleteCustomPoc({ id: row.id })
  if (res.code === 0) {
    ElMessage.success(t('poc.deleteSuccess'))
    handlePocSearch()
  }
}

// ==================== 导出POC相关函数 ====================

// 导出所有自定义POC（每个POC一个文件，打包成ZIP）
async function handleExportPocs() {
  if (customPocs.value.length === 0) {
    ElMessage.warning(t('poc.noPocToExport'))
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
      ElMessage.warning(t('poc.noPocToExport'))
      return
    }
    
    // 创建ZIP文件
    const zip = new JSZip()
    
    // 统计导出情况
    let exportedCount = 0
    let skippedCount = 0
    
    // 每个POC创建一个单独的文件
    for (const poc of allPocs) {
      // 跳过没有内容的POC
      if (!poc.content || poc.content.trim() === '') {
        skippedCount++
        console.warn(`Skipping POC with empty content: ${poc.templateId || poc.name}`)
        continue
      }
      
      // 使用templateId作为文件名，清理非法字符
      const fileName = (poc.templateId || poc.name || 'poc')
        .replace(/[<>:"/\\|?*]/g, '-')
        .replace(/\s+/g, '-')
      zip.file(`${fileName}.yaml`, poc.content)
      exportedCount++
    }
    
    // 检查是否有有效的POC可导出
    if (exportedCount === 0) {
      ElMessage.warning(t('poc.noValidPocToExport'))
      return
    }
    
    // 生成ZIP并下载
    const content = await zip.generateAsync({ type: 'blob' })
    const dateStr = new Date().toISOString().slice(0, 10)
    saveAs(content, `custom-pocs-${dateStr}.zip`)
    
    // 显示导出结果
    if (skippedCount > 0) {
      ElMessage.warning(t('poc.exportedPocsWithSkipped', { exported: exportedCount, skipped: skippedCount }))
    } else {
      ElMessage.success(t('poc.exportedPocs', { count: exportedCount }))
    }
  } catch (e) {
    console.error('Export error:', e)
    ElMessage.error(t('poc.exportError'))
  } finally {
    exportPocLoading.value = false
  }
}

// 清空自定义POC（按当前筛选条件）
async function handleClearAllPocs() {
  if (customPocs.value.length === 0 && pocPagination.total === 0) {
    ElMessage.warning(t('poc.noPocToClear'))
    return
  }

  // 检查是否有筛选条件
  const hasFilter = customPocFilter.keyword || customPocFilter.severities.length || customPocFilter.protocols.length ||
    customPocFilter.products.length || customPocFilter.hasCve !== null || customPocFilter.tag || customPocFilter.enabled !== null

  // 构建提示信息
  let confirmMsg = ''
  if (hasFilter) {
    const filterDesc = []
    if (customPocFilter.keyword) filterDesc.push(t('poc.filterNameContains', { name: customPocFilter.keyword }))
    if (customPocFilter.severities.length) filterDesc.push(t('poc.filterSeverityIs', { severity: customPocFilter.severities.join('/') }))
    if (customPocFilter.protocols.length) filterDesc.push(`${t('poc.filterProtocol')}: ${customPocFilter.protocols.join('/')}`)
    if (customPocFilter.products.length) filterDesc.push(`${t('poc.filterProduct')}: ${customPocFilter.products.join('/')}`)
    if (customPocFilter.hasCve === true) filterDesc.push(t('poc.cveYes'))
    if (customPocFilter.hasCve === false) filterDesc.push(t('poc.cveNo'))
    if (customPocFilter.tag) filterDesc.push(t('poc.filterTagContains', { tag: customPocFilter.tag }))
    if (customPocFilter.enabled === true) filterDesc.push(t('poc.filterStatusEnabled'))
    if (customPocFilter.enabled === false) filterDesc.push(t('poc.filterStatusDisabled'))
    confirmMsg = t('poc.confirmClearFilteredPoc', { filter: filterDesc.join('、'), count: pocPagination.total })
  } else {
    confirmMsg = t('poc.confirmClearPoc', { count: pocPagination.total })
  }

  try {
    await ElMessageBox.confirm(
      confirmMsg,
      t('poc.dangerOperation'),
      {
        type: 'warning',
        confirmButtonText: t('poc.confirmClearBtn'),
        cancelButtonText: t('poc.cancel'),
        confirmButtonClass: 'el-button--danger'
      }
    )

    clearPocLoading.value = true

    // 传递筛选条件（与列表筛选保持一致）
    const res = await clearAllCustomPoc(buildCustomPocFilterPayload())
    if (res.code === 0) {
      ElMessage.success(t('poc.clearedPocs', { count: res.deleted || pocPagination.total }))
      handlePocSearch()
    } else {
      ElMessage.error(res.msg || t('poc.clearFailed'))
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('Clear error:', e)
      ElMessage.error(t('poc.clearFailed'))
    }
  } finally {
    clearPocLoading.value = false
  }
}

// ==================== 导入POC相关函数 ====================

// 显示导入对话框
function showImportPocDialog() {
  importPocEnabled.value = true
  uploadedFileCount.value = 0
  importPocZipFile.value = null
  importPocProgress.value = 0
  importPocStatus.value = ''
  importPocError.value = ''
  importPocLoading.value = false
  importPocDialogVisible.value = true
}

// 处理ZIP文件选择
function handleImportPocZipChange(file) {
  if (file.raw.type !== 'application/zip' && !file.name.endsWith('.zip')) {
    ElMessage.error(t('poc.onlyZipAllowed'))
    importPocZipUploadRef.value?.clearFiles()
    importPocZipFile.value = null
    return
  }
  importPocZipFile.value = file.raw
}

function handleImportPocZipExceed() {
  ElMessage.warning(t('poc.onlyOneFile'))
}

// 重置导入对话框
function resetImportPocDialog() {
  importPocZipFile.value = null
  importPocProgress.value = 0
  importPocStatus.value = ''
  importPocError.value = ''
  importPocLoading.value = false
  uploadedFileCount.value = 0
  if (importPocZipUploadRef.value) {
    importPocZipUploadRef.value.clearFiles()
  }
}

// 导入完成
function handleImportPocComplete() {
  importPocDialogVisible.value = false
  handlePocSearch()
}

// 上传ZIP包并解析导入
async function handleImportPocZip() {
  if (!importPocZipFile.value) return

  importPocLoading.value = true
  importPocStatus.value = 'extracting'
  importPocProgress.value = 10

  try {
    const zip = await JSZip.loadAsync(importPocZipFile.value)
    importPocProgress.value = 30

    // 查找所有yaml文件
    const yamlFiles = []
    const filePromises = []

    zip.forEach((relativePath, zipEntry) => {
      if (!zipEntry.dir && (relativePath.endsWith('.yaml') || relativePath.endsWith('.yml'))) {
        // 跳过隐藏文件和特殊目录
        if (relativePath.includes('/.') || relativePath.startsWith('.')) return
        filePromises.push(
          zipEntry.async('string').then(content => {
            yamlFiles.push({ path: relativePath, content })
          })
        )
      }
    })

    await Promise.all(filePromises)
    importPocProgress.value = 50
    uploadedFileCount.value = yamlFiles.length

    if (yamlFiles.length === 0) {
      importPocStatus.value = 'failed'
      importPocError.value = t('poc.noPocsInZip')
      importPocLoading.value = false
      return
    }

    // 解析并去重
    const pocsToImport = []
    const seenTemplateIds = new Set()
    const seenContents = new Set()

    for (const file of yamlFiles) {
      try {
        if (!file.content || file.content.trim().length === 0) continue

        const parsed = parseYamlToPreview(file.content)
        if (parsed) {
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
        console.error('解析文件失败:', file.path, e)
      }
    }

    if (pocsToImport.length === 0) {
      importPocStatus.value = 'failed'
      importPocError.value = t('poc.noPocsInZip')
      importPocLoading.value = false
      return
    }

    // 分批导入
    importPocStatus.value = 'importing'
    importPocProgress.value = 60

    const batchSize = 200
    let successCount = 0
    let failCount = 0

    for (let i = 0; i < pocsToImport.length; i += batchSize) {
      const batch = pocsToImport.slice(i, i + batchSize)
      try {
        const res = await batchImportCustomPoc({ pocs: batch })
        if (res.code === 0) {
          successCount += res.imported || batch.length
          failCount += res.failed || 0
        } else {
          failCount += batch.length
        }
      } catch (e) {
        failCount += batch.length
        console.error('批量导入失败:', e)
      }
      importPocProgress.value = 60 + Math.floor(((i + batch.length) / pocsToImport.length) * 35)
    }

    importPocProgress.value = 100
    uploadedFileCount.value = successCount

    if (successCount > 0) {
      importPocStatus.value = 'completed'
      importPocLoading.value = false
      // 导入成功后自动关闭对话框并刷新列表
      importPocDialogVisible.value = false
      handlePocSearch()
      ElMessage.success(t('poc.importComplete', { success: successCount, failed: failCount }))
    } else {
      importPocStatus.value = 'failed'
      importPocError.value = t('poc.importFailed')
      importPocLoading.value = false
    }

  } catch (error) {
    console.error('解析ZIP失败:', error)
    importPocStatus.value = 'failed'
    importPocError.value = t('poc.zipParseFailed') + ': ' + (error.message || '')
    importPocLoading.value = false
  }
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

    // severity（已无 Unknown 级别，unknown 归一为 info）
    const severityMatch = infoBlock.match(/^\s+severity:\s*(.+)$/m)
    if (severityMatch) {
      const severity = severityMatch[1].trim().toLowerCase()
      if (['critical', 'high', 'medium', 'low', 'info'].includes(severity)) {
        result.severity = severity
      } else if (severity === 'unknown') {
        result.severity = 'info'
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

const severityOrder = { critical: 0, high: 1, medium: 2, low: 3, info: 4 }
function sortBySeverity(a, b) {
  return (severityOrder[a.severity] ?? 99) - (severityOrder[b.severity] ?? 99)
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
    
    // severity（已无 Unknown 级别，unknown 归一为 info）
    const severityMatch = infoBlock.match(/^\s+severity:\s*(.+)$/m)
    if (severityMatch) {
      const severity = severityMatch[1].trim().toLowerCase()
      if (['critical', 'high', 'medium', 'low', 'info'].includes(severity)) {
        customPocForm.severity = severity
      } else if (severity === 'unknown') {
        customPocForm.severity = 'info'
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
  if (scanAssetsLogPollTimer) {
    clearInterval(scanAssetsLogPollTimer)
    scanAssetsLogPollTimer = null
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
  if (scanAssetsLogPollTimer) {
    clearInterval(scanAssetsLogPollTimer)
  }

  scanAssetsTaskIds = taskIds
  scanAssetsLogLastCount = 0

  // Immediately fetch once
  fetchScanAssetsLogs()

  // Poll every 3 seconds
  scanAssetsLogPollTimer = setInterval(fetchScanAssetsLogs, 3000)
}

function restartScanAssetsLogs() {
  scanAssetsLogs.value = []
  scanAssetsLogLastCount = 0
  fetchScanAssetsLogs()
}

async function fetchScanAssetsLogs() {
  if (scanAssetsTaskIds.length === 0) return
  try {
    // Fetch logs for the first task (batch task)
    const res = await getTaskLogs({ taskId: scanAssetsTaskIds[0], limit: 500, includeDebug: scanAssetsIncludeDebug.value })
    if (res.code === 0 && res.list) {
      const newLogs = res.list.slice(scanAssetsLogLastCount)
      scanAssetsLogLastCount = res.list.length
      for (const log of newLogs) {
        let displayMsg = log.message || ''
        // Check if log belongs to any of the current scan tasks
        const matchedTaskId = scanAssetsTaskIds.find(tid => displayMsg.includes(tid))
        if (matchedTaskId) {
          const taskIdPrefix = `[${matchedTaskId}] `
          if (displayMsg.startsWith(taskIdPrefix)) {
            displayMsg = displayMsg.substring(taskIdPrefix.length)
          }
          scanAssetsLogs.value.push({
            level: log.level || 'INFO',
            message: displayMsg,
            timestamp: log.timestamp || new Date().toLocaleTimeString()
          })
          // Check for vulnerability markers
          if (displayMsg.includes('✓') || displayMsg.includes('Vulnerability found') || displayMsg.includes('发现漏洞')) {
            scanAssetsProgress.vulnCount++
          }
          const vulMatch = displayMsg.match(/vuls[:=]\s*(\d+)|(\d+)\s*vuls found/i)
          if (vulMatch) {
            const count = vulMatch[1] || vulMatch[2]
            if (count) scanAssetsProgress.vulnCount = parseInt(count)
          }
          if (scanAssetsLogs.value.length > 200) {
            scanAssetsLogs.value.shift()
          }
          scrollScanAssetsLogsToBottom()
        }
      }
    }
  } catch (e) { /* ignore */ }
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
        
        // 如果结果中没有（可能是异步保存延迟），尝试使用进度中的计数（从日志解析的）
        if (vulnCount === 0 && scanAssetsProgress.vulnCount > 0) {
          vulnCount = scanAssetsProgress.vulnCount
        } else {
          scanAssetsProgress.vulnCount = vulnCount
        }
        
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
  cleanupBatchValidation()
  // 清理全局批量验证轮询
  if (globalBatchPollTimer) {
    clearInterval(globalBatchPollTimer)
    globalBatchPollTimer = null
  }
})

// 清理验证相关资源
function cleanupValidation() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (logPollTimer) {
    clearInterval(logPollTimer)
    logPollTimer = null
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
  if (logPollTimer) {
    clearInterval(logPollTimer)
  }
  pocValidateLogs.value = []
  currentTaskId = taskId
  logPollLastCount = 0

  // Immediately fetch once
  fetchValidationLogs(taskId)

  // Poll every 3 seconds
  logPollTimer = setInterval(() => fetchValidationLogs(taskId), 3000)
}

function restartValidationLogs() {
  if (!currentTaskId) return
  pocValidateLogs.value = []
  logPollLastCount = 0
  fetchValidationLogs(currentTaskId)
}

async function fetchValidationLogs(taskId) {
  try {
    const res = await getTaskLogs({ taskId, limit: 200, includeDebug: pocValidateIncludeDebug.value })
    if (res.code === 0 && res.list) {
      const newLogs = res.list.slice(logPollLastCount)
      logPollLastCount = res.list.length
      for (const log of newLogs) {
        let displayMsg = log.message || ''
        const taskIdPrefix = `[${taskId}] `
        if (displayMsg.startsWith(taskIdPrefix)) {
          displayMsg = displayMsg.substring(taskIdPrefix.length)
        }
        pocValidateLogs.value.push({
          level: log.level || 'INFO',
          message: displayMsg,
          timestamp: log.timestamp || new Date().toLocaleTimeString()
        })
      }
      if (pocValidateLogs.value.length > 50) {
        pocValidateLogs.value = pocValidateLogs.value.slice(-50)
      }
      scrollLogsToBottom()
    }
  } catch (e) { /* ignore */ }
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
      if (logPollTimer) {
        clearInterval(logPollTimer)
        logPollTimer = null
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
          if (logPollTimer) {
            clearInterval(logPollTimer)
            logPollTimer = null
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
  if (batchLogPollTimer) {
    clearInterval(batchLogPollTimer)
    batchLogPollTimer = null
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
  if (batchLogPollTimer) {
    clearInterval(batchLogPollTimer)
  }
  currentBatchId = batchId
  batchLogLastCount = 0

  // Immediately fetch once
  fetchBatchLogs()

  // Poll every 3 seconds
  batchLogPollTimer = setInterval(fetchBatchLogs, 3000)
}

function restartBatchLogs() {
  templateBatchValidateLogs.value = []
  batchLogLastCount = 0
  fetchBatchLogs()
}

async function fetchBatchLogs() {
  if (currentBatchTaskIds.value.length === 0) return
  try {
    // Fetch logs for each task in the batch
    for (const taskId of currentBatchTaskIds.value) {
      const res = await getTaskLogs({ taskId, limit: 500, includeDebug: batchValidateIncludeDebug.value })
      if (res.code === 0 && res.list) {
        const newLogs = res.list.slice(batchLogLastCount)
        batchLogLastCount = res.list.length
        for (const log of newLogs) {
          let displayMsg = log.message || ''
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
          setTimeout(() => {
            if (batchLogsContainerRef.value) {
              batchLogsContainerRef.value.scrollTop = batchLogsContainerRef.value.scrollHeight
            }
          }, 50)
        }
      }
    }
  } catch (e) { /* ignore */ }
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

// ==================== 全局批量验证（Tab右侧按钮） ====================

function showGlobalBatchValidateDialog() {
  if (persistentTask.value && persistentTask.value.status === 'running') {
    ElMessage.warning('已有POC批量验证任务正在运行，请等待完成')
    return
  }
  globalBatchScope.value = 'all'
  globalBatchValidateUrls.value = ''
  globalBatchTargetInputType.value = 'text'
  globalBatchValidateResults.value = []
  globalBatchValidateProgress.total = 0
  globalBatchValidateProgress.completed = 0
  globalCurrentBatchId = null
  globalBatchValidateLoading.value = false
  globalBatchValidateDialogVisible.value = true
}

function saveGlobalBatchTaskToStorage() {
  if (!globalCurrentBatchId) return
  const data = {
    batchId: globalCurrentBatchId,
    scope: globalBatchScope.value,
    urls: globalBatchValidateUrls.value,
    status: globalBatchValidateLoading.value ? 'running' : 'completed',
    total: globalBatchValidateProgress.total,
    completed: globalBatchValidateProgress.completed,
    matched: globalBatchValidateResults.value.filter(r => r.matched).length,
    results: !globalBatchValidateLoading.value ? globalBatchValidateResults.value : null,
    savedAt: Date.now()
  }
  localStorage.setItem(POC_GLOBAL_BATCH_STORAGE_KEY, JSON.stringify(data))
}

// 恢复常驻任务
function restorePersistentTask() {
  const saved = localStorage.getItem(POC_GLOBAL_BATCH_STORAGE_KEY)
  if (!saved) return
  try {
    const info = JSON.parse(saved)
    if (info.batchId && Date.now() - info.savedAt < 2 * 60 * 60 * 1000) {
      globalCurrentBatchId = info.batchId
      globalBatchScope.value = info.scope || 'all'
      globalBatchValidateUrls.value = info.urls || ''
      globalBatchValidateProgress.total = info.total || 0
      globalBatchValidateProgress.completed = info.completed || 0
      globalBatchValidateResults.value = info.results || []
      globalBatchValidateLoading.value = info.status === 'running'

      const urlCount = (info.urls || '').split('\n').filter(u => u.trim()).length

      persistentTask.value = {
        batchId: info.batchId,
        scope: info.scope,
        status: info.status || 'running',
        total: info.total,
        completed: info.completed,
        matched: info.matched || 0,
        urlCount
      }

      if (info.status === 'running') {
        startGlobalBatchPolling(info.batchId)
      }
    } else {
      localStorage.removeItem(POC_GLOBAL_BATCH_STORAGE_KEY)
    }
  } catch (e) {
    localStorage.removeItem(POC_GLOBAL_BATCH_STORAGE_KEY)
  }
}

function updatePersistentTask(status) {
  if (!persistentTask.value) return
  const matched = globalBatchValidateResults.value.filter(r => r.matched).length
  persistentTask.value = {
    ...persistentTask.value,
    status,
    total: globalBatchValidateProgress.total,
    completed: globalBatchValidateProgress.completed,
    matched
  }
}

function dismissPersistentTask() {
  persistentTask.value = null
  localStorage.removeItem(POC_GLOBAL_BATCH_STORAGE_KEY)
  if (globalBatchPollTimer) {
    clearInterval(globalBatchPollTimer)
    globalBatchPollTimer = null
  }
  globalCurrentBatchId = null
  globalBatchValidateLoading.value = false
}

function showGlobalBatchResultDialog() {
  globalBatchResultDialogVisible.value = true
}

function getPocScopeTagType(scope) {
  switch (scope) {
    case 'template': return 'primary'
    case 'custom': return 'warning'
    default: return 'info'
  }
}
function getPocScopeLabel(scope) {
  switch (scope) {
    case 'template': return '默认模板'
    case 'custom': return '自定义POC'
    default: return '全部POC'
  }
}

function handleGlobalBatchValidateDialogClose() {
  // 弹窗关闭不影响后台任务
}

function handleGlobalBatchUrlFileChange(file) {
  const reader = new FileReader()
  reader.onload = (e) => {
    globalBatchValidateUrls.value = e.target.result
  }
  reader.readAsText(file.raw)
}

function handleGlobalBatchUrlFileRemove() {
  globalBatchValidateUrls.value = ''
}

async function handleGlobalBatchValidate() {
  const urls = globalBatchTargetUrls.value
  if (urls.length === 0) {
    ElMessage.warning('请输入有效的目标URL')
    return
  }

  globalBatchValidateLoading.value = true
  globalBatchValidateResults.value = []
  globalBatchValidateProgress.total = urls.length
  globalBatchValidateProgress.completed = 0

  const scope = globalBatchScope.value
  const useTemplate = scope === 'all' || scope === 'template'
  const useCustom = scope === 'all' || scope === 'custom'

  try {
    const res = await batchValidatePoc({
      urls: urls,
      useTemplate: useTemplate,
      useCustom: useCustom,
      timeout: 30,
      concurrency: 10
    })

    if (res.code === 0 && res.batchId) {
      globalCurrentBatchId = res.batchId

      // 初始化常驻任务
      persistentTask.value = {
        batchId: res.batchId,
        scope: scope,
        status: 'running',
        total: urls.length,
        completed: 0,
        matched: 0,
        urlCount: urls.length
      }

      saveGlobalBatchTaskToStorage()
      startGlobalBatchPolling(res.batchId)

      // 关闭弹窗
      globalBatchValidateDialogVisible.value = false
      ElMessage.success('POC批量验证任务已提交，可在页面顶部查看进度')
    } else {
      globalBatchValidateLoading.value = false
      ElMessage.error(res.msg || '下发失败')
    }
  } catch (e) {
    globalBatchValidateLoading.value = false
    ElMessage.error('验证请求失败: ' + e.message)
  }
}

function startGlobalBatchPolling(batchId) {
  globalBatchPollTimer = setInterval(async () => {
    try {
      const res = await getPocValidationResult({ batchId })
      if (res.code === 0) {
        globalBatchValidateProgress.completed = res.completedCount || 0
        globalBatchValidateProgress.total = res.totalCount || globalBatchValidateProgress.total

        if (res.results && res.results.length > 0) {
          globalBatchValidateResults.value = res.results
        }

        if (res.status === 'SUCCESS' || res.status === 'FAILURE') {
          clearInterval(globalBatchPollTimer)
          globalBatchPollTimer = null
          globalBatchValidateLoading.value = false
          globalBatchValidateResults.value = res.results || []

          const matchedCount = (res.results || []).filter(r => r.matched).length

          if (res.status === 'SUCCESS') {
            updatePersistentTask('completed')
            saveGlobalBatchTaskToStorage()
            if (matchedCount > 0) {
              ElMessage.success(`批量验证完成，发现 ${matchedCount} 个漏洞`)
            } else {
              ElMessage.info('批量验证完成，未发现漏洞')
            }
          } else {
            updatePersistentTask('failed')
            saveGlobalBatchTaskToStorage()
            ElMessage.error('批量验证失败')
          }
        } else {
          updatePersistentTask('running')
          saveGlobalBatchTaskToStorage()
        }
      }
    } catch (e) {
      console.error('Global batch poll error:', e)
    }
  }, 2000)
}

function handleGlobalExportResults(command) {
  let dataToExport = globalBatchValidateResults.value
  if (command === 'matched') {
    dataToExport = dataToExport.filter(r => r.matched)
  }
  if (dataToExport.length === 0) {
    ElMessage.warning('没有可导出的数据')
    return
  }

  const timestamp = new Date().toISOString().slice(0, 19).replace(/[:-]/g, '')

  if (command === 'csv') {
    const headers = ['POC名称', '模板ID', '级别', '结果', '匹配URL', '详情']
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
    const jsonContent = JSON.stringify(dataToExport, null, 2)
    const blob = new Blob([jsonContent], { type: 'application/json' })
    downloadFile(blob, `poc_validation_results_${timestamp}.json`)
  }
  ElMessage.success('导出成功')
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

  if (!aiConfig.value.baseUrl) {
    ElMessage.warning('请先在 系统管理 > AI配置 中配置AI服务地址')
    return
  }

  aiGenerating.value = true
  try {
    const content = extractYamlBlock(await chat({ config: aiConfig.value, prompt: buildPocPrompt() }))

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
    if (e.code === 'NETWORK') {
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

// ==================== 弱口令字典相关方法 ====================

// 加载弱口令字典列表
async function loadWeakpassDicts() {
  weakpassDictLoading.value = true
  try {
    const res = await getWeakpassDictList({
      page: weakpassDictPagination.page,
      pageSize: weakpassDictPagination.pageSize
    })
    if (res.code === 0) {
      weakpassDicts.value = res.list || []
      weakpassDictPagination.total = res.total || 0
    }
  } catch (e) {
    console.error('加载弱口令字典失败:', e)
  } finally {
    weakpassDictLoading.value = false
  }
}

// 显示弱口令字典编辑表单
function showWeakpassDictForm(row = null) {
  if (row) {
    Object.assign(weakpassDictForm, {
      id: row.id,
      name: row.name,
      description: row.description || '',
      service: row.service,
      content: row.content || '',
      enabled: row.enabled
    })
  } else {
    Object.assign(weakpassDictForm, {
      id: '',
      name: '',
      description: '',
      service: 'common',
      content: '',
      enabled: true
    })
  }
  weakpassDictDialogVisible.value = true
}

// 保存弱口令字典
async function handleSaveWeakpassDict() {
  try {
    await weakpassDictFormRef.value.validate()
  } catch (e) {
    return
  }

  try {
    const res = await saveWeakpassDict({
      id: weakpassDictForm.id || undefined,
      name: weakpassDictForm.name,
      description: weakpassDictForm.description,
      service: weakpassDictForm.service,
      content: weakpassDictForm.content,
      enabled: weakpassDictForm.enabled
    })
    if (res.code === 0) {
      ElMessage.success(weakpassDictForm.id ? '更新成功' : '创建成功')
      weakpassDictDialogVisible.value = false
      loadWeakpassDicts()
    } else {
      ElMessage.error(res.msg || '保存失败')
    }
  } catch (e) {
    console.error('保存弱口令字典失败:', e)
    ElMessage.error('保存失败')
  }
}

// 删除弱口令字典
async function handleDeleteWeakpassDict(row) {
  try {
    await ElMessageBox.confirm(`确定要删除字典 "${row.name}" 吗？`, '确认删除', {
      type: 'warning'
    })
    const res = await deleteWeakpassDict({ id: row.id })
    if (res.code === 0) {
      ElMessage.success('删除成功')
      loadWeakpassDicts()
    } else {
      ElMessage.error(res.msg || '删除失败')
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('删除弱口令字典失败:', e)
    }
  }
}

// 清空自定义弱口令字典
async function handleClearWeakpassDict() {
  try {
    await ElMessageBox.confirm('确定要清空所有自定义字典吗？内置字典不会被删除。', '确认清空', {
      type: 'warning'
    })
    clearWeakpassDictLoading.value = true
    const res = await clearWeakpassDict()
    if (res.code === 0) {
      ElMessage.success(`已清空 ${res.deleted} 个自定义字典`)
      loadWeakpassDicts()
    } else {
      ElMessage.error(res.msg || '清空失败')
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('清空弱口令字典失败:', e)
    }
  } finally {
    clearWeakpassDictLoading.value = false
  }
}

// 计算弱口令词条数量
function countWeakpassWords(content) {
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

// 获取服务类型标签
function getServiceLabel(service) {
  const opt = serviceOptions.find(o => o.value === service)
  return opt ? opt.label : service
}

// ==================== JSFinder 配置 ====================
function applyJSFinderData(data) {
  jsfinderConfig.highRiskRoutes = Array.isArray(data?.highRiskRoutes) ? [...data.highRiskRoutes] : []
  jsfinderConfig.authRequiredKeywords = Array.isArray(data?.authRequiredKeywords) ? [...data.authRequiredKeywords] : []
  jsfinderConfig.sensitiveKeywords = Array.isArray(data?.sensitiveKeywords) ? [...data.sensitiveKeywords] : []
  jsfinderConfig.domainBlacklist = Array.isArray(data?.domainBlacklist) ? [...data.domainBlacklist] : []
}

function jsfinderText(key) {
  return (jsfinderConfig[key] || []).join('\n')
}

function jsfinderUpdateText(key, val) {
  jsfinderConfig[key] = val.split('\n').map(s => s.trim()).filter(Boolean)
}

function escapeRegex(str) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function escapeHtml(str) {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function jsfinderMatchCount(key) {
  if (!jsfinderSearchQuery.value) return 0
  const text = jsfinderText(key).toLowerCase()
  const q = jsfinderSearchQuery.value.toLowerCase()
  let count = 0
  let idx = 0
  while ((idx = text.indexOf(q, idx)) !== -1) {
    count++
    idx += q.length
  }
  return count
}

function getMatchedLines(key) {
  if (!jsfinderSearchQuery.value) return []
  const items = jsfinderConfig[key] || []
  const q = jsfinderSearchQuery.value.toLowerCase()
  return items.filter(item => item.toLowerCase().includes(q))
}

function highlightLine(line, query) {
  const escaped = escapeHtml(line)
  if (!query) return escaped
  const q = escapeHtml(query)
  const regex = new RegExp(`(${escapeRegex(q)})`, 'gi')
  return DOMPurify.sanitize(escaped.replace(regex, '<mark class="jsfinder-highlight">$1</mark>'))
}

async function loadJSFinderConfigData() {
  jsfinderLoading.value = true
  try {
    const res = await getJSFinderConfig()
    if (res.code === 0) {
      applyJSFinderData(res.data)
      jsfinderLoaded.value = true
    } else {
      ElMessage.error(res.msg || t('poc.loadFailed'))
    }
  } catch (e) {
    console.error('加载JSFinder配置失败:', e)
    ElMessage.error(t('poc.loadFailed'))
  } finally {
    jsfinderLoading.value = false
  }
}


async function handleSaveJSFinderConfig() {
  jsfinderSaveLoading.value = true
  try {
    const res = await saveJSFinderConfig({
      highRiskRoutes: jsfinderConfig.highRiskRoutes,
      authRequiredKeywords: jsfinderConfig.authRequiredKeywords,
      sensitiveKeywords: jsfinderConfig.sensitiveKeywords,
      domainBlacklist: jsfinderConfig.domainBlacklist
    })
    if (res.code === 0) {
      ElMessage.success(t('poc.jsfinderSaveSuccess'))
      if (res.data) applyJSFinderData(res.data)
    } else {
      ElMessage.error(res.msg || t('poc.saveFailed'))
    }
  } catch (e) {
    console.error('保存JSFinder配置失败:', e)
    ElMessage.error(t('poc.saveFailed'))
  } finally {
    jsfinderSaveLoading.value = false
  }
}

async function handleResetJSFinderConfig() {
  try {
    await ElMessageBox.confirm(t('poc.jsfinderResetConfirm'), t('common.confirm'), { type: 'warning' })
  } catch (e) {
    return
  }
  jsfinderResetLoading.value = true
  try {
    const res = await resetJSFinderConfig()
    if (res.code === 0) {
      ElMessage.success(t('poc.jsfinderResetSuccess'))
      if (res.data) applyJSFinderData(res.data)
    } else {
      ElMessage.error(res.msg || t('poc.saveFailed'))
    }
  } catch (e) {
    console.error('重置JSFinder配置失败:', e)
    ElMessage.error(t('poc.saveFailed'))
  } finally {
    jsfinderResetLoading.value = false
  }
}
</script>

<style lang="scss" scoped>
// 对话框内容会被 teleport 到 body，样式需置于顶层而非嵌套在 .poc-page 下
.ai-config-link {
  color: var(--el-color-primary);
  font-weight: 500;
}

.poc-page {
  .persistent-task-bar {
    margin-bottom: 12px;
  }
  .persistent-task-card {
    border-radius: 8px;
    border-left: 4px solid var(--el-color-danger);
    :deep(.el-card__body) {
      padding: 12px 16px;
    }
  }
  .persistent-task-content {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
  }
  .persistent-task-info {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
  }
  .persistent-task-title {
    font-weight: 600;
    font-size: 14px;
    white-space: nowrap;
  }
  .persistent-task-url {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    white-space: nowrap;
  }
  .persistent-task-progress {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .persistent-task-stat {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    white-space: nowrap;
    min-width: 60px;
  }
  .persistent-task-result {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .persistent-task-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
  }
  .rotating {
    animation: rotating 1.5s linear infinite;
  }
  @keyframes rotating {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .tabs-with-action {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .flex-grow-tabs {
    flex: 1;
    min-width: 0;
  }
  .tabs-action-buttons {
    padding-top: 4px;
    flex-shrink: 0;
    display: flex;
    gap: 8px;
  }

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

  .jsfinder-list-card {
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 6px;
    padding: 12px 14px;
    background: var(--el-fill-color-blank);
    height: 100%;
    box-sizing: border-box;
  }

  .jsfinder-list-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--el-text-color-primary);
    margin-bottom: 4px;
  }

  .jsfinder-list-hint {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    line-height: 1.5;
    margin-bottom: 10px;
  }

  .jsfinder-search-bar {
    margin-bottom: 16px;
  }

  .jsfinder-match-badge {
    display: inline-block;
    margin-left: 8px;
    padding: 2px 8px;
    background: var(--el-color-warning-light-3);
    color: var(--el-color-warning-dark-2);
    border-radius: 10px;
    font-size: 12px;
    font-weight: 600;
  }

  .jsfinder-match-strip {
    margin-top: 6px;
    padding: 4px 8px;
    background: var(--el-fill-color-light);
    border-radius: 4px;
    font-size: 12px;
    line-height: 1.8;
    max-height: 80px;
    overflow-y: auto;
    border: 1px solid var(--el-border-color-lighter);
  }

  .jsfinder-match-chip {
    display: inline-block;
    margin: 2px 4px 2px 0;
    padding: 0 6px;
    background: var(--el-fill-color-blank);
    border-radius: 3px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: middle;
  }

  .jsfinder-highlight {
    background-color: #e6a23c;
    color: #fff;
    padding: 1px 2px;
    border-radius: 2px;
  }

  .filter-form {
    margin-bottom: 15px;
  }

  // 模板库筛选面板（仿官方模板库：筛选项 + 数量统计）
  .template-filters {
    margin-bottom: 15px;
    padding: 12px 15px;
    background: var(--el-fill-color-light);
    border-radius: 6px;

    .filter-row {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: 8px;

      & + .filter-row {
        margin-top: 10px;
      }
    }

    .filter-row-label {
      flex-shrink: 0;
      font-size: 13px;
      font-weight: 600;
      color: var(--el-text-color-regular);
      min-width: 32px;
    }

    .filter-row-controls {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: 8px;
    }

    .facet-group {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: 6px;
    }

    .facet-chip {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      padding: 3px 10px;
      font-size: 12px;
      line-height: 20px;
      border: 1px solid var(--el-border-color);
      border-radius: 12px;
      background: var(--el-fill-color-blank);
      color: var(--el-text-color-regular);
      cursor: pointer;
      transition: all 0.2s;

      &:hover {
        border-color: var(--el-color-primary);
        color: var(--el-color-primary);
      }

      &.is-active {
        background: var(--el-color-primary-light-9);
        border-color: var(--el-color-primary);
        color: var(--el-color-primary);
        font-weight: 600;
      }

      &.is-disabled {
        cursor: not-allowed;
        opacity: 0.45;
      }

      .facet-name {
        text-transform: capitalize;
      }

      .facet-count {
        padding: 0 6px;
        font-size: 11px;
        line-height: 16px;
        border-radius: 8px;
        background: var(--el-fill-color);
        color: var(--el-text-color-secondary);
      }

      &.is-active .facet-count {
        background: var(--el-color-primary-light-8);
        color: var(--el-color-primary);
      }

      // 严重级别色点（与官方模板库配色一致）
      .facet-sev-dot {
        width: 8px;
        height: 8px;
        border-radius: 50%;
        flex-shrink: 0;

        &.dot-critical { background: #e74856; }
        &.dot-high { background: #f7630c; }
        &.dot-medium { background: #ffb900; }
        &.dot-low { background: #0078d4; }
        &.dot-info { background: #6b7280; }
      }
    }
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

  .download-progress {
    padding: 20px 0;
    
    .progress-info {
      margin-top: 15px;
      text-align: center;
      color: var(--el-text-color-secondary);
      font-size: 14px;
      
      .error-text {
        color: var(--el-color-danger);
      }
      
      .template-count {
        display: block;
        margin-top: 8px;
        color: var(--el-color-success);
        font-weight: 500;
      }
    }
  }

  .upload-section {
    .selected-file {
      margin-top: 15px;
      text-align: center;
    }
  }

  .template-content-wrapper {
    :deep(.el-textarea__inner) {
      background-color: var(--code-bg);
      color: var(--code-text);
      border: 1px solid var(--code-border);
    }
  }

  .yaml-editor-wrapper {
    :deep(.el-textarea__inner) {
      background-color: var(--code-bg);
      color: var(--code-text);
      border: 1px solid var(--code-border);
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
      gap: 10px;
      font-weight: 500;
    }

    .logs-content {
      background: var(--code-bg);
      color: var(--code-text);
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
      background: var(--code-bg);
      color: var(--code-text);
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
      background: var(--code-bg);
      color: var(--code-text);
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
