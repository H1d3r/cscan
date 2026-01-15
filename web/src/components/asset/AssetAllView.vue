<template>
  <div class="asset-all-view">
    <!-- 搜索区域 -->
    <el-card class="search-card">
      <el-tabs v-model="activeTab" class="search-tabs">
        <el-tab-pane :label="$t('asset.quickSearch')" name="quick">
          <div class="quick-search-form">
            <div class="search-row">
              <div class="search-item">
                <label class="search-label">{{ $t('asset.host') }}</label>
                <el-input v-model="searchForm.host" :placeholder="$t('asset.ipOrDomain')" clearable @keyup.enter="handleSearch" />
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.port') }}</label>
                <el-input v-model.number="searchForm.port" :placeholder="$t('asset.portNumber')" clearable @keyup.enter="handleSearch" />
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.service') }}</label>
                <el-input v-model="searchForm.service" placeholder="http/ssh..." clearable @keyup.enter="handleSearch" />
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.pageTitle') }}</label>
                <el-input v-model="searchForm.title" :placeholder="$t('asset.webPageTitle')" clearable @keyup.enter="handleSearch" />
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('asset.app') }}</label>
                <el-input v-model="searchForm.app" :placeholder="$t('asset.fingerprintApp')" clearable @keyup.enter="handleSearch" />
              </div>
              <div class="search-item">
                <label class="search-label">{{ $t('domain.organization') }}</label>
                <el-select v-model="searchForm.orgId" :placeholder="$t('common.allOrganizations')" clearable @change="handleSearch">
                  <el-option :label="$t('common.allOrganizations')" value="" />
                  <el-option v-for="org in organizations" :key="org.id" :label="org.name" :value="org.id" />
                </el-select>
              </div>
            </div>
          </div>
        </el-tab-pane>
        <el-tab-pane :label="$t('asset.statistics')" name="stat">
          <div class="stat-panel">
            <div class="stat-column">
              <div class="stat-title">Port</div>
              <div v-for="item in stat.topPorts" :key="'port-'+item.name" class="stat-item" @click="quickFilter('port', item.name)">
                <span class="stat-count">{{ item.count }}</span>
                <span class="stat-name">{{ item.name }}</span>
              </div>
            </div>
            <div class="stat-column">
              <div class="stat-title">Service</div>
              <div v-for="item in stat.topService" :key="'svc-'+item.name" class="stat-item" @click="quickFilter('service', item.name)">
                <span class="stat-count">{{ item.count }}</span>
                <span class="stat-name">{{ item.name }}</span>
              </div>
            </div>
            <div class="stat-column">
              <div class="stat-title">App</div>
              <div v-for="item in stat.topApp" :key="'app-'+item.name" class="stat-item" @click="quickFilter('app', item.name)">
                <span class="stat-count">{{ item.count }}</span>
                <span class="stat-name">{{ item.name }}</span>
              </div>
            </div>
            <div class="stat-column">
              <div class="stat-title">IconHash</div>
              <div v-for="item in stat.topIconHash" :key="'icon-'+item.iconHash" class="stat-item stat-item-icon" @click="quickFilter('iconHash', item.iconHash)">
                <span class="stat-count">{{ item.count }}</span>
                <img 
                  v-if="item.iconData && getIconDataUrl(item.iconData)" 
                  :src="getIconDataUrl(item.iconData)" 
                  class="stat-icon-img" 
                  :title="item.iconHash"
                  @error="handleIconError($event)"
                />
                <span v-else class="stat-name" :title="item.iconHash">{{ truncateText(item.iconHash, 10) }}</span>
              </div>
            </div>
            <div class="stat-column filter-column">
              <el-checkbox v-model="searchForm.onlyUpdated">{{ $t('asset.onlyUpdated') }}</el-checkbox>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
      <div class="search-actions">
        <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
        <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        <el-button type="danger" plain @click="handleClear">{{ $t('asset.clearData') }}</el-button>
      </div>
    </el-card>

    <!-- 数据表格 -->
    <el-card class="table-card">
      <div class="table-header">
        <span class="total-info">{{ $t('common.total') }} {{ pagination.total }} {{ $t('common.items') }}</span>
        <div class="table-actions">
          <el-button type="primary" size="small" @click="showImportDialog">
            {{ $t('common.import') }}{{ $t('dashboard.asset') }}
          </el-button>
          <el-button type="danger" size="small" :disabled="selectedRows.length === 0" @click="handleBatchDelete">
            {{ $t('common.batchDelete') }} ({{ selectedRows.length }})
          </el-button>
          <el-dropdown style="margin-left: 10px" @command="handleExport">
            <el-button type="success" size="small">
              {{ $t('common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="selected-target" :disabled="selectedRows.length === 0">{{ $t('vul.exportSelectedTargets', { count: selectedRows.length }) }}</el-dropdown-item>
                <el-dropdown-item command="selected-url" :disabled="selectedRows.length === 0">{{ $t('vul.exportSelectedUrls', { count: selectedRows.length }) }}</el-dropdown-item>
                <el-dropdown-item divided command="all-target">{{ $t('vul.exportAllTargets') }}</el-dropdown-item>
                <el-dropdown-item command="all-url">{{ $t('vul.exportAllUrls') }}</el-dropdown-item>
                <el-dropdown-item command="csv">{{ $t('common.exportCsv') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </div>
      <el-table :data="tableData" v-loading="loading" stripe size="small" max-height="500" @selection-change="handleSelectionChange">
        <el-table-column type="selection" width="40" />
        <el-table-column type="index" :label="$t('asset.index')" width="60" />
        <el-table-column :label="$t('dashboard.asset')" min-width="200">
          <template #default="{ row }">
            <div class="asset-cell">
              <a :href="getAssetUrl(row)" target="_blank" class="asset-link">{{ row.authority }}</a>
            </div>
            <div class="org-text">{{ row.orgName || $t('common.defaultOrganization') }}</div>
          </template>
        </el-table-column>
        <el-table-column label="IP" width="140">
          <template #default="{ row }">
            <div>{{ getDisplayIP(row) }}</div>
            <div v-if="row.location" class="location-text">{{ row.location }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.portProtocol')" width="120">
          <template #default="{ row }">
            <span class="port-text">{{ row.port > 0 ? row.port : '-' }}</span>
            <span v-if="row.service" class="service-text">{{ row.service }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.pageTitle')" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ row.title || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('common.detail')" min-width="400">
          <template #default="{ row }">
            <el-tabs v-model="row._activeTab" size="small" class="inline-tabs" v-if="hasAnyTabContent(row)">
              <el-tab-pane :label="$t('asset.fingerprint')" name="app" v-if="row.app && row.app.length > 0">
                <div class="tab-content tab-content-app">
                  <el-tag v-for="app in (row.app || [])" :key="app" size="small" type="success" style="margin: 2px">
                    {{ getAppName(app) }}
                  </el-tag>
                </div>
              </el-tab-pane>
              <el-tab-pane label="Header" name="header" v-if="row.httpHeader">
                <div class="tab-content tab-content-header">
                  <pre class="inline-pre">{{ row.httpHeader }}</pre>
                </div>
              </el-tab-pane>
              <el-tab-pane label="Body" name="body" v-if="row.httpBody">
                <div class="tab-content tab-content-body">
                  <pre class="inline-pre">{{ row.httpBody }}</pre>
                </div>
              </el-tab-pane>
              <el-tab-pane label="IconHash" name="iconhash" v-if="row.iconHash">
                <div class="tab-content tab-content-iconhash">
                  <div class="iconhash-display">
                    <img 
                      v-if="row.iconData && getIconDataUrl(row.iconData)" 
                      :src="getIconDataUrl(row.iconData)" 
                      class="iconhash-img" 
                      :title="row.iconHash"
                      @error="handleIconError($event)"
                    />
                      <el-tag type="info" size="small">{{ row.iconHash }}</el-tag>
                  </div>
                </div>
              </el-tab-pane>
              <el-tab-pane name="vul" v-if="rowVulMap[row.id] && rowVulMap[row.id].length > 0">
                <template #label>
                  <span>Vuln</span>
                  <el-badge :value="getRowVulCount(row)" type="danger" :max="99" style="margin-left: 2px" />
                </template>
                <div class="tab-content tab-content-vul">
                  <el-tag v-for="vul in rowVulMap[row.id]" :key="vul.id" :type="getSeverityType(vul.severity)" size="small" style="margin: 2px" :title="vul.pocFile">
                    {{ vul.pocFile }}
                  </el-tag>
                </div>
              </el-tab-pane>
              <el-tab-pane name="dirscan" v-if="rowDirScanMap[row.id] && rowDirScanMap[row.id].length > 0">
                <template #label>
                  <span>{{ $t('asset.dirManagement') }}</span>
                  <el-badge :value="rowDirScanMap[row.id].length" type="primary" :max="99" style="margin-left: 2px" />
                </template>
                <div class="tab-content tab-content-dirscan">
                  <el-collapse v-model="row._dirScanCollapse" accordion size="small">
                    <el-collapse-item v-for="(item, idx) in rowDirScanMap[row.id].slice(0, 10)" :key="item.id || idx" :name="idx">
                      <template #title>
                        <el-tag :type="getDirScanStatusType(item.statusCode)" size="small" style="margin-right: 8px">{{ item.statusCode }}</el-tag>
                        <span class="dirscan-path">{{ item.path }}</span>
                      </template>
                      <div class="dirscan-detail">
                        <div><a :href="item.url" target="_blank" class="url-link">{{ item.url }}</a></div>
                        <div v-if="item.title" class="dirscan-title">{{ $t('asset.pageTitle') }}: {{ item.title }}</div>
                        <div class="dirscan-meta">{{ $t('asset.contentLength') }}: {{ formatDirScanSize(item.contentLength) }}</div>
                      </div>
                    </el-collapse-item>
                  </el-collapse>
                  <div v-if="rowDirScanMap[row.id].length > 10" class="dirscan-more">
                    {{ $t('common.total') }}{{ rowDirScanMap[row.id].length }} {{ $t('common.items') }}，<el-button type="primary" link size="small" @click="showDirScanDetail(row)">{{ $t('dashboard.viewAll') }}</el-button>
                  </div>
                </div>
              </el-tab-pane>
            </el-tabs>
            <span v-else class="no-data">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('asset.screenshot')" width="90" align="center">
          <template #default="{ row }">
            <el-image 
              v-if="row.screenshot" 
              :src="getScreenshotUrl(row.screenshot)" 
              :preview-src-list="[getScreenshotUrl(row.screenshot)]" 
              :z-index="9999"
              :preview-teleported="true"
              :hide-on-click-modal="true"
              fit="cover" 
              class="screenshot-img" 
            />
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.updateTime')" width="160">
          <template #default="{ row }">
            <div>{{ row.updateTime }}</div>
            <el-tag v-if="row.isNew" type="success" size="small" effect="dark">{{ $t('common.new') }}</el-tag>
            <el-tag v-if="row.isUpdated && !row.isNew" type="warning" size="small" effect="dark" style="cursor: pointer" @click="showHistory(row)">{{ $t('asset.updated') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showDetail(row)">{{ $t('common.detail') }}</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
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

    <!-- 详情对话框 -->
    <el-dialog v-model="detailVisible" :title="$t('common.detail')" width="900px">
      <el-descriptions :column="2" border v-if="currentAsset">
        <el-descriptions-item :label="$t('dashboard.asset')" :span="2">
          <a :href="getAssetUrl(currentAsset)" target="_blank" class="asset-link">{{ currentAsset.authority }}</a>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('asset.host')">{{ currentAsset.host }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.port')">{{ currentAsset.port }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.service')">{{ currentAsset.service || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.scanType')">{{ currentAsset.scheme || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.pageTitle')" :span="2">{{ currentAsset.title || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.statusCode')">{{ currentAsset.httpStatus || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.server')">{{ currentAsset.server || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('ip.location')" :span="2">{{ currentAsset.location || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('domain.organization')">{{ currentAsset.orgName || $t('common.defaultOrganization') }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.updateTime')">{{ currentAsset.updateTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.fingerprint')" :span="2">
          <el-tag v-for="app in (currentAsset.app || [])" :key="app" size="small" type="success" style="margin: 2px">
            {{ app }}
          </el-tag>
          <span v-if="!(currentAsset.app || []).length">-</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('asset.screenshot')" :span="2" v-if="currentAsset.screenshot">
          <el-image 
            :src="getScreenshotUrl(currentAsset.screenshot)" 
            :preview-src-list="[getScreenshotUrl(currentAsset.screenshot)]"
            :z-index="9999"
            :preview-teleported="true"
            :hide-on-click-modal="true"
            fit="contain"
            style="max-width: 400px; max-height: 300px; cursor: pointer;"
          />
        </el-descriptions-item>
      </el-descriptions>
      
      <!-- 详情Tab页 -->
      <el-tabs v-model="detailActiveTab" style="margin-top: 15px" v-if="currentAsset">
        <el-tab-pane label="HTTP Header" name="header">
          <div class="detail-content-box">
            <pre v-if="currentAsset.httpHeader" class="detail-pre">{{ currentAsset.httpHeader }}</pre>
            <el-empty v-else :description="$t('common.noData')" :image-size="60" />
          </div>
        </el-tab-pane>
        <el-tab-pane label="HTTP Body" name="body">
          <div class="detail-content-box">
            <pre v-if="currentAsset.httpBody" class="detail-pre">{{ truncateBody(currentAsset.httpBody) }}</pre>
            <el-empty v-else :description="$t('common.noData')" :image-size="60" />
          </div>
        </el-tab-pane>
        <el-tab-pane label="Icon Hash" name="iconhash">
          <div class="detail-content-box">
            <div v-if="currentAsset.iconHash" class="iconhash-info">
              <div class="iconhash-value">
                <img 
                  v-if="currentAsset.iconData && getIconDataUrl(currentAsset.iconData)" 
                  :src="getIconDataUrl(currentAsset.iconData)" 
                  class="iconhash-detail-img"
                  @error="handleIconError($event)"
                />
                <span class="label">Hash:</span>
                <el-tag type="info" size="small">{{ currentAsset.iconHash }}</el-tag>
                <el-button type="primary" link size="small" @click="copyIconHash">{{ $t('common.copy') }}</el-button>
              </div>
              <div v-if="currentAsset.iconHashFile" class="iconhash-file">
                <span class="label">{{ $t('vul.pocFile') }}:</span>
                <span>{{ currentAsset.iconHashFile }}</span>
              </div>
            </div>
            <el-empty v-else :description="$t('common.noData')" :image-size="60" />
          </div>
        </el-tab-pane>
        <el-tab-pane name="vul">
          <template #label>
            <span>{{ $t('asset.vulnerability') }}</span>
            <el-badge v-if="assetVulList.length > 0" :value="assetVulList.length" type="danger" style="margin-left: 5px" />
          </template>
          <div class="detail-content-box">
            <el-table v-if="assetVulList.length > 0" :data="assetVulList" stripe size="small" max-height="300">
              <el-table-column prop="pocFile" :label="$t('vul.vulName')" min-width="200" show-overflow-tooltip />
              <el-table-column prop="severity" :label="$t('vul.severity')" width="90">
                <template #default="{ row }">
                  <el-tag :type="getSeverityType(row.severity)" size="small">{{ row.severity }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="url" label="URL" min-width="200" show-overflow-tooltip />
              <el-table-column prop="createTime" :label="$t('common.createTime')" width="160">
                <template #default="{ row }">{{ formatTime(row.createTime) }}</template>
              </el-table-column>
            </el-table>
            <el-empty v-else :description="$t('dashboard.noVulData')" :image-size="60" />
          </div>
        </el-tab-pane>
      </el-tabs>
      
      <template #footer>
        <el-button @click="detailVisible = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-dialog>

    <!-- 历史记录对话框 -->
    <el-dialog v-model="historyVisible" :title="$t('asset.scanHistory')" width="900px">
      <div v-if="currentHistoryAsset" style="margin-bottom: 15px">
        <el-tag type="info">{{ currentHistoryAsset.authority }}</el-tag>
      </div>
      <el-table :data="historyList" v-loading="historyLoading" stripe size="small" max-height="500">
        <el-table-column prop="createTime" :label="$t('asset.scanTime')" width="160" />
        <el-table-column prop="title" :label="$t('asset.pageTitle')" min-width="150" show-overflow-tooltip />
        <el-table-column prop="httpStatus" :label="$t('asset.statusCode')" width="80" />
        <el-table-column :label="$t('asset.fingerprint')" min-width="150">
          <template #default="{ row }">
            <el-tag v-for="app in (row.app || []).slice(0, 3)" :key="app" size="small" type="success" style="margin: 2px">
              {{ getAppName(app) }}
            </el-tag>
            <span v-if="(row.app || []).length > 3" class="more-apps">+{{ (row.app || []).length - 3 }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="100">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showHistoryDetail(row)">{{ $t('common.detail') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!historyLoading && historyList.length === 0" :description="$t('asset.noHistory')" />
      <template #footer>
        <el-button @click="historyVisible = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-dialog>

    <!-- 历史详情对话框 -->
    <el-dialog v-model="historyDetailVisible" :title="$t('asset.scanHistory')" width="800px">
      <el-descriptions :column="2" border size="small" v-if="currentHistoryDetail">
        <el-descriptions-item :label="$t('asset.scanTime')" :span="2">{{ currentHistoryDetail.createTime }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.pageTitle')" :span="2">{{ currentHistoryDetail.title || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.statusCode')">{{ currentHistoryDetail.httpStatus || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.service')">{{ currentHistoryDetail.service || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('asset.fingerprint')" :span="2">
          <el-tag v-for="app in (currentHistoryDetail.app || [])" :key="app" size="small" type="success" style="margin: 2px">{{ app }}</el-tag>
          <span v-if="!(currentHistoryDetail.app || []).length">-</span>
        </el-descriptions-item>
      </el-descriptions>
      <el-tabs v-model="historyDetailTab" style="margin-top: 15px" v-if="currentHistoryDetail">
        <el-tab-pane label="Header" name="header">
          <div class="detail-content-box">
            <pre v-if="currentHistoryDetail.httpHeader" class="detail-pre">{{ currentHistoryDetail.httpHeader }}</pre>
            <el-empty v-else :description="$t('common.noData')" :image-size="60" />
          </div>
        </el-tab-pane>
        <el-tab-pane label="Body" name="body">
          <div class="detail-content-box">
            <pre v-if="currentHistoryDetail.httpBody" class="detail-pre">{{ truncateBody(currentHistoryDetail.httpBody) }}</pre>
            <el-empty v-else :description="$t('common.noData')" :image-size="60" />
          </div>
        </el-tab-pane>
        <el-tab-pane label="IconHash" name="iconhash">
          <div class="detail-content-box">
            <el-tag v-if="currentHistoryDetail.iconHash" type="info">{{ currentHistoryDetail.iconHash }}</el-tag>
            <el-empty v-else :description="$t('common.noData')" :image-size="60" />
          </div>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="historyDetailVisible = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-dialog>

    <!-- 目录扫描详情对话?-->
    <el-dialog v-model="dirScanDetailVisible" title="目录扫描结果" width="900px">
      <div v-if="dirScanDetailRow" style="margin-bottom: 15px">
        <el-tag type="info">{{ dirScanDetailRow.authority }}</el-tag>
        <span style="margin-left: 10px; color: var(--el-text-color-secondary)">共 {{ dirScanDetailData.length }} 条记录</span>
      </div>
      <el-table :data="dirScanDetailData" stripe size="small" max-height="500">
        <el-table-column prop="url" label="URL" min-width="300" show-overflow-tooltip>
          <template #default="{ row }">
            <a :href="row.url" target="_blank" rel="noopener" class="url-link">{{ row.url }}</a>
          </template>
        </el-table-column>
        <el-table-column prop="path" label="路径" min-width="120" show-overflow-tooltip />
        <el-table-column prop="statusCode" label="状态码" width="90">
          <template #default="{ row }">
            <el-tag :type="getDirScanStatusType(row.statusCode)" size="small">{{ row.statusCode }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="contentLength" label="大小" width="90">
          <template #default="{ row }">{{ formatDirScanSize(row.contentLength) }}</template>
        </el-table-column>
        <el-table-column prop="title" label="标题" min-width="120" show-overflow-tooltip />
        <el-table-column prop="createTime" label="发现时间" width="150" />
      </el-table>
      <template #footer>
        <el-button @click="dirScanDetailVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 导入资产对话?-->
    <el-dialog v-model="importDialogVisible" title="导入资产" width="600px">
      <el-form label-width="80px">
        <el-form-item label="目标列表">
          <el-input
            v-model="importTargets"
            type="textarea"
            :rows="12"
            placeholder="每行一个目标，支持以下格式?
?IP:端口 (?192.168.1.1:80)
?URL (?http://example.com:8080)
?域名 (?example.com，默?0端口)
?https://example.com (默认443端口)"
          />
        </el-form-item>
        <el-form-item>
          <div class="import-tips">
              <span>共 {{ importTargetCount }} 个目标</span>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="importDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleImport" :loading="importLoading" :disabled="importTargetCount === 0">
          导入
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, watch, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import { getAssetList, getAssetStat, batchDeleteAsset, clearAsset, importAsset } from '@/api/asset'
import { useWorkspaceStore } from '@/stores/workspace'
import request from '@/api/request'

const emit = defineEmits(['data-changed'])

const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const tableData = ref([])
const selectedRows = ref([])
const activeTab = ref('quick')
const organizations = ref([])
const detailVisible = ref(false)
const currentAsset = ref(null)
const detailActiveTab = ref('header')
const assetVulList = ref([])
const fingerprintList = ref([])
const rowVulMap = ref({}) // 存储每行资产的漏洞数?

// 历史记录相关
const historyVisible = ref(false)
const historyLoading = ref(false)
const historyList = ref([])
const currentHistoryAsset = ref(null)
const historyDetailVisible = ref(false)
const currentHistoryDetail = ref(null)
const historyDetailTab = ref('header')

// 目录扫描相关
const rowDirScanMap = ref({}) // 存储每行资产的目录扫描数?
const dirScanDetailVisible = ref(false)
const dirScanDetailData = ref([])
const dirScanDetailRow = ref(null)

// 导入相关
const importDialogVisible = ref(false)
const importTargets = ref('')
const importLoading = ref(false)
const importTargetCount = computed(() => {
  if (!importTargets.value.trim()) return 0
  return importTargets.value.trim().split('\n').filter(line => line.trim()).length
})

const stat = reactive({ topPorts: [], topService: [], topApp: [], topIconHash: [] })
const searchForm = reactive({ host: '', port: null, service: '', title: '', app: '', orgId: '', onlyUpdated: false, iconHash: '' })
const pagination = reactive({ page: 1, pageSize: 50, total: 0 })

function handleWorkspaceChanged() { pagination.page = 1; loadData(); loadStat() }

onMounted(() => {
  loadData(); loadStat(); loadOrganizations(); loadFingerprints()
  window.addEventListener('workspace-changed', handleWorkspaceChanged)
})
onUnmounted(() => { window.removeEventListener('workspace-changed', handleWorkspaceChanged) })

// 监听tableData变化，为每行添加默认Tab和加载漏洞数?
watch(tableData, (newData) => {
  if (newData && newData.length > 0) {
    newData.forEach(row => {
      // 设置默认显示的Tab（优先显示有内容的Tab?
      if (!row._activeTab) {
        row._activeTab = getDefaultTab(row)
      }
    })
    loadAllRowVuls(newData)
    loadAllRowDirScans(newData)
  }
}, { immediate: true })

// 判断行是否有任何Tab内容
function hasAnyTabContent(row) {
  return (row.app && row.app.length > 0) || 
         row.httpHeader || 
         row.httpBody || 
         row.iconHash || 
         (rowVulMap.value[row.id] && rowVulMap.value[row.id].length > 0) ||
         (rowDirScanMap.value[row.id] && rowDirScanMap.value[row.id].length > 0)
}

// 获取默认显示的Tab
function getDefaultTab(row) {
  if (row.app && row.app.length > 0) return 'app'
  if (row.httpHeader) return 'header'
  if (row.httpBody) return 'body'
  if (row.iconHash) return 'iconhash'
  return 'app'
}

// 批量加载所有行的漏洞数?
async function loadAllRowVuls(rows) {
  rowVulMap.value = {}
  for (const row of rows) {
    try {
      const res = await request.post('/vul/list', { host: row.host, port: row.port, page: 1, pageSize: 10 })
      if (res.code === 0 && res.list && res.list.length > 0) {
        rowVulMap.value[row.id] = res.list
      }
    } catch (e) { /* ignore */ }
  }
}

// 获取行的漏洞数量
function getRowVulCount(row) {
  return rowVulMap.value[row.id]?.length || 0
}

async function loadData() {
  loading.value = true
  try {
    const res = await getAssetList({
      page: pagination.page, pageSize: pagination.pageSize,
      host: searchForm.host, port: searchForm.port, service: searchForm.service,
      title: searchForm.title, app: searchForm.app, orgId: searchForm.orgId,
      onlyUpdated: searchForm.onlyUpdated, iconHash: searchForm.iconHash
    })
    if (res.code === 0) { tableData.value = res.list || []; pagination.total = res.total }
  } finally { loading.value = false }
}

async function loadStat() {
  const res = await getAssetStat({})
  if (res.code === 0) {
    stat.topPorts = res.topPorts || []
    stat.topService = res.topService || []
    stat.topApp = res.topApp || []
    stat.topIconHash = res.topIconHash || []
  }
}

async function loadOrganizations() {
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    if (res.code === 0) organizations.value = res.list || []
  } catch (e) { console.error(e) }
}

async function loadFingerprints() {
  try {
    const res = await request.post('/fingerprint/list', { page: 1, pageSize: 500, enabled: true })
    if (res.code === 0) fingerprintList.value = res.list || []
  } catch (e) { console.error(e) }
}

// 批量加载所有行的目录扫描数?
async function loadAllRowDirScans(rows) {
  rowDirScanMap.value = {}
  for (const row of rows) {
    try {
      const res = await request.post('/dirscan/result/list', { authority: row.authority, page: 1, pageSize: 50 })
      if (res.code === 0 && res.list && res.list.length > 0) {
        rowDirScanMap.value[row.id] = res.list
      }
    } catch (e) { /* ignore */ }
  }
}

// 显示目录扫描详情对话?
function showDirScanDetail(row) {
  dirScanDetailRow.value = row
  dirScanDetailData.value = rowDirScanMap.value[row.id] || []
  dirScanDetailVisible.value = true
}

function getDirScanStatusType(code) {
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'warning'
  if (code >= 400 && code < 500) return 'danger'
  if (code >= 500) return 'danger'
  return 'info'
}

function formatDirScanSize(bytes) {
  if (!bytes || bytes < 0) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

function quickFilter(field, value) {
  if (field === 'app') {
    // App筛选：去掉 [xxx] 后缀，只用纯指纹名称查询
    searchForm.app = cleanAppName(value)
  } else if (field === 'iconHash') {
    searchForm.iconHash = value
  } else {
    searchForm[field] = field === 'port' ? parseInt(value, 10) : value
  }
  activeTab.value = 'quick'
  handleSearch()
}

// 清理指纹名称，去?[custom(xxx)] 等后缀
function cleanAppName(app) {
  if (!app) return ''
  return app.replace(/\s*\[.*\]\s*$/, '').trim()
}

function handleSearch() { pagination.page = 1; loadData() }
function handleReset() {
  Object.assign(searchForm, { host: '', port: null, service: '', title: '', app: '', orgId: '', onlyUpdated: false, iconHash: '' })
  handleSearch(); loadStat()
}

function handleSelectionChange(rows) { selectedRows.value = rows }

async function handleBatchDelete() {
  if (selectedRows.value.length === 0) return
  await ElMessageBox.confirm(`确定删除选中?${selectedRows.value.length} 条资产吗？`, '提示', { type: 'warning' })
  const ids = selectedRows.value.map(row => row.id)
  const res = await batchDeleteAsset({ ids })
  if (res.code === 0) { ElMessage.success('删除成功'); selectedRows.value = []; loadData(); loadStat(); emit('data-changed') }
}

async function handleDelete(row) {
  await ElMessageBox.confirm('确定删除该资产吗？', '提示', { type: 'warning' })
  const res = await batchDeleteAsset({ ids: [row.id] })
  if (res.code === 0) { ElMessage.success('删除成功'); loadData(); loadStat(); emit('data-changed') }
}

async function showDetail(row) {
  currentAsset.value = row
  detailActiveTab.value = 'header'
  assetVulList.value = []
  detailVisible.value = true
  // 加载该资产的漏洞列表
  loadAssetVulList(row.host, row.port)
}

async function loadAssetVulList(host, port) {
  try {
    const res = await request.post('/vul/list', { host, port, page: 1, pageSize: 100 })
    if (res.code === 0) {
      assetVulList.value = res.list || []
    }
  } catch (e) { console.error(e) }
}

// 显示历史记录
async function showHistory(row) {
  currentHistoryAsset.value = row
  historyList.value = []
  historyVisible.value = true
  historyLoading.value = true
  try {
    const res = await request.post('/asset/history', { assetId: row.id, limit: 20 })
    if (res.code === 0) {
      historyList.value = res.list || []
    }
  } catch (e) { 
    console.error(e) 
  } finally {
    historyLoading.value = false
  }
}

// 显示历史详情
function showHistoryDetail(row) {
  currentHistoryDetail.value = row
  historyDetailTab.value = 'header'
  historyDetailVisible.value = true
}

// 显示导入对话?
function showImportDialog() {
  importTargets.value = ''
  importDialogVisible.value = true
}

// 执行导入
  async function handleImport() {
    if (!importTargets.value.trim()) {
      ElMessage.warning('请输入要导入的目标')
      return
    }
  
  const targets = importTargets.value.trim().split('\n').filter(line => line.trim())
  if (targets.length === 0) {
    ElMessage.warning('请输入要导入的目标')
    return
  }
  
  importLoading.value = true
  try {
    const res = await importAsset({ targets })
    if (res.code === 0) {
      ElMessage.success(res.msg || '导入成功')
      importDialogVisible.value = false
      loadData()
      loadStat()
      emit('data-changed')
    } else {
      ElMessage.error(res.msg || '导入失败')
    }
  } catch (e) {
    ElMessage.error('导入失败: ' + e.message)
  } finally {
    importLoading.value = false
  }
}

// 导出功能
async function handleExport(command) {
  let data = []
  let filename = ''
  
  if (command === 'selected-target' || command === 'selected-url') {
    if (selectedRows.value.length === 0) {
      ElMessage.warning('请先选择要导出的资产')
      return
    }
    data = selectedRows.value
    filename = command === 'selected-target' ? 'asset_targets_selected.txt' : 'asset_urls_selected.txt'
  } else if (command === 'csv') {
    // CSV导出所有字段
    ElMessage.info('正在获取全部数据...')
    try {
      const res = await getAssetList({
        ...searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) {
        data = res.list || []
      } else {
        ElMessage.error('获取数据失败')
        return
      }
    } catch (e) {
      ElMessage.error('获取数据失败')
      return
    }
    
    if (data.length === 0) {
      ElMessage.warning('没有可导出的数据')
      return
    }
    
    const headers = ['Authority', 'Host', 'Port', 'Service', 'Scheme', 'Title', 'StatusCode', 'Server', 'Location', 'Apps', 'IconHash', 'Organization', 'UpdateTime']
    const csvRows = [headers.join(',')]
    
    for (const row of data) {
      const values = [
        escapeCsvField(row.authority || ''),
        escapeCsvField(row.host || ''),
        row.port || '',
        escapeCsvField(row.service || ''),
        escapeCsvField(row.scheme || ''),
        escapeCsvField(row.title || ''),
        row.httpStatus || '',
        escapeCsvField(row.server || ''),
        escapeCsvField(row.location || ''),
        escapeCsvField((row.app || []).join(';')),
        escapeCsvField(row.iconHash || ''),
        escapeCsvField(row.orgName || ''),
        escapeCsvField(row.updateTime || '')
      ]
      csvRows.push(values.join(','))
    }
    
    const BOM = '\uFEFF'
    const blob = new Blob([BOM + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `assets_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    
    ElMessage.success(`成功导出 ${data.length} 条数据`)
    return
  } else {
    ElMessage.info('正在获取全部数据...')
    try {
      const res = await getAssetList({
        ...searchForm, page: 1, pageSize: 10000
      })
      if (res.code === 0) {
        data = res.list || []
      } else {
        ElMessage.error('获取数据失败')
        return
      }
    } catch (e) {
      ElMessage.error('获取数据失败')
      return
    }
    filename = command === 'all-target' ? 'asset_targets_all.txt' : 'asset_urls_all.txt'
  }
  
  if (data.length === 0) {
    ElMessage.warning('没有可导出的数据')
    return
  }
  
  const seen = new Set()
  const exportData = []
  
  if (command.includes('target')) {
    for (const row of data) {
      const target = row.authority || (row.host + ':' + row.port)
      if (target && !seen.has(target)) {
        seen.add(target)
        exportData.push(target)
      }
    }
  } else {
    for (const row of data) {
      const scheme = row.service === 'https' || row.port === 443 ? 'https' : 'http'
      const url = `${scheme}://${row.host}:${row.port}`
      if (!seen.has(url)) {
        seen.add(url)
        exportData.push(url)
      }
    }
  }
  
  if (exportData.length === 0) {
    ElMessage.warning('没有可导出的数据')
    return
  }
  
  const blob = new Blob([exportData.join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
  
  ElMessage.success(`成功导出 ${exportData.length} 条数据`)
}

// CSV字段转义
function escapeCsvField(field) {
  if (field == null) return ''
  const str = String(field)
  if (str.includes(',') || str.includes('"') || str.includes('\n') || str.includes('\r')) {
    return '"' + str.replace(/"/g, '""') + '"'
  }
  return str
}

async function handleClear() {
  try {
    await ElMessageBox.confirm('确定清空所有资产数据吗？此操作不可恢复！', '警告', { type: 'error', confirmButtonText: '确定清空', cancelButtonText: '取消' })
    const res = await clearAsset()
    if (res.code === 0) { 
      ElMessage.success(res.msg || '清空成功')
      selectedRows.value = []
      loadData()
      loadStat()
      emit('data-changed') 
    } else { 
      ElMessage.error(res.msg || '清空失败') 
    }
  } catch (e) {
    if (e !== 'cancel') {
      console.error('清空资产失败:', e)
      ElMessage.error('清空资产失败')
    }
  }
}

function getAssetUrl(row) {
  const scheme = row.service === 'https' || row.port === 443 ? 'https' : 'http'
  return `${scheme}://${row.host}:${row.port}`
}

// 获取显示的IP地址
// 如果资产有解析到的IP地址，优先显示IP；否则显示host（可能是域名?
function getDisplayIP(row) {
  // 检查是否有解析到的IPv4地址
  if (row.ip && row.ip.ipv4 && row.ip.ipv4.length > 0 && row.ip.ipv4[0].ip) {
    return row.ip.ipv4[0].ip
  }
  // 检查是否有解析到的IPv6地址
  if (row.ip && row.ip.ipv6 && row.ip.ipv6.length > 0 && row.ip.ipv6[0].ip) {
    return row.ip.ipv6[0].ip
  }
  // 如果host本身就是IP地址，直接返?
  if (isIPAddress(row.host)) {
    return row.host
  }
  // 否则返回 "-"，表示没有IP信息
  return '-'
}

// 判断是否为IP地址
function isIPAddress(str) {
  if (!str) return false
  // IPv4 正则
  const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/
  // IPv6 简单正?
  const ipv6Regex = /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$/
  return ipv4Regex.test(str) || ipv6Regex.test(str)
}

function getAppName(app) {
  if (!app) return ''
  const idx = app.indexOf('[')
  return idx > 0 ? app.substring(0, idx) : app
}

function getScreenshotUrl(screenshot) {
  if (!screenshot) return ''
  if (screenshot.startsWith('data:') || screenshot.startsWith('/9j/') || screenshot.startsWith('iVBOR')) {
    return screenshot.startsWith('data:') ? screenshot : `data:image/png;base64,${screenshot}`
  }
  return `/api/screenshot/${screenshot}`
}

function truncateBody(body) {
  if (!body) return ''
  const maxLen = 5000
  if (body.length > maxLen) {
    return body.substring(0, maxLen) + '\n\n... [内容过长，已截断]'
  }
  return body
}

function truncateText(text, maxLen = 100) {
  if (!text) return ''
  if (text.length > maxLen) {
    return text.substring(0, maxLen) + '...'
  }
  return text
}

function getIconDataUrl(iconData) {
  if (!iconData || iconData.length === 0) return ''
  
  // 如果已经?data URL，直接返?
  if (typeof iconData === 'string' && iconData.startsWith('data:')) return iconData
  
  // 确保是字符串类型
  const base64Str = typeof iconData === 'string' ? iconData : ''
  if (!base64Str) return ''
  
  // 根据 base64 解码后的魔数判断图片类型
  try {
    const binaryStr = atob(base64Str)
    if (binaryStr.length < 4) return ''
    
    // 跳过开头的空白字符
    let start = 0
    while (start < binaryStr.length && (binaryStr[start] === ' ' || binaryStr[start] === '\t' || binaryStr[start] === '\n' || binaryStr[start] === '\r')) {
      start++
    }
    
    // 检测是否为 HTML/XML 内容（无效的图片数据?
    if (binaryStr[start] === '<') {
      const header = binaryStr.substring(start, start + 100).toLowerCase()
      if (header.startsWith('<!doctype') || header.startsWith('<html') || header.startsWith('<?xml')) {
        return '' // HTML/XML 内容，不是有效图?
      }
      // SVG 是有效的图片格式
      if (header.startsWith('<svg')) {
        return `data:image/svg+xml;base64,${base64Str}`
      }
      return '' // 其他 XML/HTML 内容
    }
    
    const bytes = new Uint8Array(binaryStr.length)
    for (let i = 0; i < binaryStr.length; i++) {
      bytes[i] = binaryStr.charCodeAt(i)
    }
    
    // 检测图片格式魔?
    // PNG: 89 50 4E 47
    if (bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4E && bytes[3] === 0x47) {
      return `data:image/png;base64,${base64Str}`
    }
    // JPEG: FF D8 FF
    if (bytes[0] === 0xFF && bytes[1] === 0xD8 && bytes[2] === 0xFF) {
      return `data:image/jpeg;base64,${base64Str}`
    }
    // GIF: 47 49 46 38 (GIF8)
    if (bytes[0] === 0x47 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x38) {
      return `data:image/gif;base64,${base64Str}`
    }
    // ICO: 00 00 01 00 ?00 00 02 00 (CUR)
    if (bytes[0] === 0x00 && bytes[1] === 0x00 && (bytes[2] === 0x01 || bytes[2] === 0x02) && bytes[3] === 0x00) {
      return `data:image/x-icon;base64,${base64Str}`
    }
    // BMP: 42 4D (BM)
    if (bytes[0] === 0x42 && bytes[1] === 0x4D) {
      return `data:image/bmp;base64,${base64Str}`
    }
    // WEBP: RIFF...WEBP
    if (bytes.length >= 12 && bytes[0] === 0x52 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x46 &&
        bytes[8] === 0x57 && bytes[9] === 0x45 && bytes[10] === 0x42 && bytes[11] === 0x50) {
      return `data:image/webp;base64,${base64Str}`
    }
    
    // 未识别的格式，返回空（不显示?
    return ''
  } catch (e) {
    return ''
  }
}

function copyIconHash() {
  if (currentAsset.value && currentAsset.value.iconHash) {
    const hash = currentAsset.value.iconHash
    
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(hash).then(() => {
        ElMessage.success('已复制IconHash')
      }).catch(() => {
        fallbackCopyToClipboard(hash)
      })
    } else {
      fallbackCopyToClipboard(hash)
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
      ElMessage.success('已复制IconHash')
    } else {
      ElMessage.error('复制失败')
    }
  } catch (err) {
    console.error('复制失败:', err)
    ElMessage.error('复制失败')
  }
}

// 处理图片加载错误
function handleIconError(event) {
  // 隐藏加载失败的图?
  event.target.style.display = 'none'
}

function getSeverityType(severity) {
  const map = { critical: 'danger', high: 'danger', medium: 'warning', low: 'info', info: 'success', unknown: 'info' }
  return map[severity?.toLowerCase()] || 'info'
}

function formatTime(time) {
  if (!time) return '-'
  if (typeof time === 'string') return time
  return new Date(time).toLocaleString()
}

function refresh() { loadData(); loadStat() }

defineExpose({ refresh })
</script>

<style scoped>
.asset-all-view {
  .search-card { margin-bottom: 15px; }
  .search-tabs { :deep(.el-tabs__header) { margin-bottom: 10px; } }
  .quick-search-form .search-row {
    display: flex; flex-wrap: wrap; gap: 16px;
    .search-item {
      display: flex; flex-direction: column; min-width: 140px; flex: 1; max-width: 180px;
      .search-label { font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 6px; }
    }
  }
  .search-actions { margin-top: 16px; padding-top: 12px; border-top: 1px solid var(--el-border-color-lighter); text-align: right; }
  .stat-panel {
    display: flex; gap: 30px;
    .stat-column {
      min-width: 140px;
      .stat-title { font-weight: bold; margin-bottom: 8px; padding-bottom: 5px; border-bottom: 2px solid #409eff; }
      .stat-item { display: flex; align-items: center; padding: 3px 0; cursor: pointer;
        &:hover { background: var(--el-fill-color); }
        .stat-count { min-width: 30px; padding: 1px 6px; margin-right: 8px; background: #409eff; color: #fff; border-radius: 3px; font-size: 12px; }
        .stat-name { color: #409eff; font-size: 13px; }
      }
      .stat-item-icon {
        .stat-icon-img { width: 16px; height: 16px; margin-right: 4px; vertical-align: middle; border-radius: 2px; }
      }
    }
    .filter-column { margin-left: auto; }
  }
  .table-card {
    .table-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px;
      .total-info { color: var(--el-text-color-secondary); font-size: 14px; }
    }
    .asset-cell .asset-link { color: #409eff; text-decoration: none; &:hover { text-decoration: underline; } }
    .org-text, .location-text { color: var(--el-text-color-secondary); font-size: 12px; }
    .port-text { font-weight: 500; margin-right: 8px; }
    .service-text { color: #67c23a; font-size: 12px; }
    .more-apps { color: var(--el-text-color-secondary); font-size: 12px; margin-left: 4px; }
    .no-data { color: var(--el-text-color-placeholder); font-size: 12px; }
    .screenshot-img { width: 70px; height: 50px; border-radius: 4px; cursor: pointer; }
    .pagination { margin-top: 15px; justify-content: flex-end; }
  }
  
  /* 表格内嵌Tab样式 */
  .inline-tabs {
    :deep(.el-tabs__header) {
      margin-bottom: 5px;
    }
    :deep(.el-tabs__nav-wrap::after) {
      height: 1px;
    }
    :deep(.el-tabs__item) {
      height: 28px;
      line-height: 28px;
      font-size: 12px;
      padding: 0 10px;
    }
    :deep(.el-tabs__content) {
      padding: 0;
    }
    .tab-content {
      min-height: 30px;
      max-height: 80px;
      overflow: hidden;
      font-size: 12px;
      &.tab-content-dirscan {
        max-height: 250px;
        overflow-y: auto;
        overflow-x: hidden;
      }
    }
    .tab-content-app {
      /* 指纹Tab：最大高度约三行标签（每行约26px） */
      max-height: 78px;
      overflow: auto;
    }
    .tab-content-scroll {
      max-height: 150px;
      overflow: auto;
    }
    .tab-content-header {
      /* Header Tab：增加高度以显示更多内容 */
      max-height: 200px;
      overflow: auto;
    }
    .tab-content-body {
      /* Body Tab：增加高度以显示更多内容 */
      max-height: 200px;
      overflow: auto;
    }
    .tab-content-vul {
      /* Vuln Tab：增加高度以显示更多漏洞 */
      max-height: 150px;
      overflow: auto;
    }
    .tab-content-dirscan {
      /* 目录扫描Tab：增加高度以显示更多内容 */
      max-height: 250px;
      overflow-y: auto;
      overflow-x: hidden;
    }
    .tab-content-iconhash {
      .iconhash-display {
        display: flex;
        align-items: center;
        gap: 8px;
        .iconhash-img {
          width: 24px;
          height: 24px;
          border-radius: 4px;
          border: 1px solid var(--el-border-color-lighter);
        }
      }
    }
    .inline-pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-all;
      font-family: 'Consolas', 'Monaco', monospace;
      font-size: 11px;
      line-height: 1.4;
      color: var(--el-text-color-regular);
    }
  }
  
  /* 详情Tab页样式 */
  .detail-content-box {
    min-height: 150px;
    max-height: 350px;
    overflow: auto;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 4px;
    padding: 10px;
    background: var(--el-fill-color-lighter);
  }
  .detail-pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 12px;
    line-height: 1.5;
  }
  .iconhash-info {
    .iconhash-value {
      display: flex;
      align-items: center;
      gap: 10px;
      margin-bottom: 10px;
      .label { color: var(--el-text-color-secondary); }
    }
    .iconhash-file {
      .label { color: var(--el-text-color-secondary); margin-right: 10px; }
    }
    .iconhash-detail-img {
      width: 32px;
      height: 32px;
      border-radius: 4px;
      border: 1px solid var(--el-border-color-lighter);
    }
  }
  
  /* 导入提示样式 */
  .import-tips {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  
  /* 目录扫描 Tab 样式 */
  .tab-content-dirscan {
    max-height: 250px !important;
    overflow-y: auto !important;
    overflow-x: hidden;
    .dirscan-path {
      font-size: 12px;
      color: var(--el-text-color-regular);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      max-width: 250px;
    }
    .dirscan-detail {
      font-size: 12px;
      padding: 5px 0;
      .dirscan-title, .dirscan-meta {
        color: var(--el-text-color-secondary);
        margin-top: 4px;
      }
    }
    .dirscan-more {
      margin-top: 8px;
      font-size: 12px;
      color: var(--el-text-color-secondary);
    }
    :deep(.el-collapse) {
      border: none;
    }
    :deep(.el-collapse-item__header) {
      height: 32px;
      line-height: 32px;
      font-size: 12px;
      background: transparent;
    }
    :deep(.el-collapse-item__wrap) {
      background: transparent;
    }
    :deep(.el-collapse-item__content) {
      padding-bottom: 8px;
    }
  }
  
  .url-link {
    color: var(--el-color-primary);
    text-decoration: none;
    &:hover { text-decoration: underline; }
  }
}
</style>

