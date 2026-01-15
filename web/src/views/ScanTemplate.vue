<template>
  <div class="scan-template-page">
    <el-card>
      <!-- 工具栏 -->
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input 
            v-model="filter.keyword" 
            :placeholder="$t('task.searchTemplate')" 
            clearable 
            style="width: 200px"
            @input="handleSearch"
          >
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <el-select v-model="filter.category" :placeholder="$t('task.allCategories')" clearable style="width: 140px" @change="loadTemplates">
            <el-option :label="$t('task.quickScan')" value="quick" />
            <el-option :label="$t('task.standardScan')" value="standard" />
            <el-option :label="$t('task.fullScan')" value="full" />
            <el-option :label="$t('task.customTemplate')" value="custom" />
          </el-select>
        </div>
        <div class="toolbar-right">
          <el-button type="primary" @click="showCreateDialog">
            <el-icon><Plus /></el-icon>{{ $t('task.createTemplate') }}
          </el-button>
          <el-button @click="showImportDialog">
            <el-icon><Upload /></el-icon>{{ $t('common.import') }}
          </el-button>
          <el-button @click="handleExport">
            <el-icon><Download /></el-icon>{{ $t('common.export') }}
          </el-button>
        </div>
      </div>

      <!-- 模板列表 -->
      <el-table :data="templateList" v-loading="loading" @selection-change="handleSelectionChange">
        <el-table-column type="selection" width="45" />
        <el-table-column prop="name" :label="$t('task.templateName')" min-width="180">
          <template #default="{ row }">
            <div class="template-name-cell">
              <el-tag v-if="row.isBuiltin" type="info" size="small">{{ $t('task.builtin') }}</el-tag>
              <span class="name-text">{{ row.name }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="category" :label="$t('task.category')" width="120">
          <template #default="{ row }">
            <el-tag :type="getCategoryType(row.category)" size="small">
              {{ getCategoryLabel(row.category) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" :label="$t('task.description')" min-width="200" show-overflow-tooltip />
        <el-table-column prop="useCount" :label="$t('task.useCount')" width="100" align="center" />
        <el-table-column prop="createTime" :label="$t('common.createTime')" width="160" />
        <el-table-column :label="$t('common.operation')" width="180" align="center" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showDetailDialog(row)">{{ $t('common.view') }}</el-button>
            <el-button v-if="!row.isBuiltin" type="primary" link size="small" @click="showEditDialog(row)">{{ $t('common.edit') }}</el-button>
            <el-button v-if="!row.isBuiltin" type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="loadTemplates"
          @current-change="loadTemplates"
        />
      </div>
    </el-card>

    <!-- 创建/编辑模板对话框 -->
    <el-dialog 
      v-model="editDialogVisible" 
      :title="editForm.id ? $t('task.editTemplate') : $t('task.createTemplate')" 
      width="700px"
      destroy-on-close
    >
      <el-form ref="editFormRef" :model="editForm" :rules="editRules" label-width="100px">
        <el-form-item :label="$t('task.templateName')" prop="name">
          <el-input v-model="editForm.name" :placeholder="$t('task.pleaseEnterTemplateName')" />
        </el-form-item>
        <el-form-item :label="$t('task.description')">
          <el-input v-model="editForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-row :gutter="20">
          <el-col :span="24">
            <el-form-item :label="$t('task.category')">
              <el-select v-model="editForm.category" style="width: 100%">
                <el-option :label="$t('task.quickScan')" value="quick" />
                <el-option :label="$t('task.standardScan')" value="standard" />
                <el-option :label="$t('task.fullScan')" value="full" />
                <el-option :label="$t('task.customTemplate')" value="custom" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="$t('task.scanConfig')" prop="config">
          <el-input 
            v-model="editForm.config" 
            type="textarea" 
            :rows="12" 
            :placeholder="$t('task.configJsonPlaceholder')"
            font-family="monospace"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">{{ $t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 查看详情对话框 -->
    <el-dialog v-model="detailDialogVisible" :title="$t('task.templateDetail')" width="700px">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('task.templateName')">{{ detailData.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.category')">
          <el-tag :type="getCategoryType(detailData.category)" size="small">
            {{ getCategoryLabel(detailData.category) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('task.description')" :span="2">{{ detailData.description || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('task.useCount')">{{ detailData.useCount }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.createTime')">{{ detailData.createTime }}</el-descriptions-item>
      </el-descriptions>
      <div class="config-section">
        <div class="config-title">{{ $t('task.scanConfig') }}</div>
        <pre class="config-content">{{ formatConfig(detailData.config) }}</pre>
      </div>
    </el-dialog>

    <!-- 导入对话框 -->
    <el-dialog v-model="importDialogVisible" :title="$t('task.importTemplate')" width="600px">
      <el-form label-width="100px">
        <el-form-item :label="$t('task.templateData')">
          <el-input 
            v-model="importData" 
            type="textarea" 
            :rows="15" 
            :placeholder="$t('task.pasteTemplateJson')"
          />
        </el-form-item>
        <el-form-item :label="$t('task.importOption')">
          <el-checkbox v-model="skipExisting">{{ $t('task.skipExistingTemplate') }}</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="importDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="importing" @click="handleImport">{{ $t('common.import') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Plus, Upload, Download } from '@element-plus/icons-vue'
import { 
  getScanTemplateList, 
  saveScanTemplate, 
  deleteScanTemplate,
  exportScanTemplates,
  importScanTemplates
} from '@/api/task'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const importing = ref(false)
const templateList = ref([])
const selectedRows = ref([])
const editDialogVisible = ref(false)
const detailDialogVisible = ref(false)
const importDialogVisible = ref(false)
const editFormRef = ref()
const importData = ref('')
const skipExisting = ref(true)

const filter = reactive({
  keyword: '',
  category: ''
})

const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

const editForm = reactive({
  id: '',
  name: '',
  description: '',
  category: 'custom',
  tags: [],
  config: ''
})

const detailData = ref({})

const editRules = {
  name: [{ required: true, message: () => t('task.pleaseEnterTemplateName'), trigger: 'blur' }],
  config: [{ required: true, message: () => t('task.pleaseEnterConfig'), trigger: 'blur' }]
}

let searchTimer = null

function getCategoryType(category) {
  const map = { quick: 'success', standard: 'primary', full: 'warning', custom: 'info' }
  return map[category] || 'info'
}

function getCategoryLabel(category) {
  const map = { 
    quick: t('task.quickScan'), 
    standard: t('task.standardScan'), 
    full: t('task.fullScan'), 
    custom: t('task.customTemplate') 
  }
  return map[category] || category
}

function formatConfig(config) {
  if (!config) return ''
  try {
    return JSON.stringify(JSON.parse(config), null, 2)
  } catch (e) {
    return config
  }
}

async function loadTemplates() {
  loading.value = true
  try {
    const res = await getScanTemplateList({
      page: pagination.page,
      pageSize: pagination.pageSize,
      keyword: filter.keyword,
      category: filter.category
    })
    if (res.code === 0) {
      templateList.value = res.list || []
      pagination.total = res.total || 0
    }
  } catch (e) {
    console.error('Load templates failed:', e)
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    pagination.page = 1
    loadTemplates()
  }, 300)
}

function handleSelectionChange(rows) {
  selectedRows.value = rows
}

function showCreateDialog() {
  Object.assign(editForm, {
    id: '',
    name: '',
    description: '',
    category: 'custom',
    tags: [],
    config: '{\n  "portscan": {\n    "enable": true,\n    "ports": "top100"\n  },\n  "fingerprint": {\n    "enable": true\n  }\n}'
  })
  editDialogVisible.value = true
}

function showEditDialog(row) {
  Object.assign(editForm, {
    id: row.id,
    name: row.name,
    description: row.description,
    category: row.category,
    tags: row.tags || [],
    config: formatConfig(row.config)
  })
  editDialogVisible.value = true
}

function showDetailDialog(row) {
  detailData.value = row
  detailDialogVisible.value = true
}

async function handleSave() {
  try {
    await editFormRef.value.validate()
  } catch (e) { return }

  // 验证JSON格式
  try {
    JSON.parse(editForm.config)
  } catch (e) {
    ElMessage.error(t('task.invalidJsonFormat'))
    return
  }

  saving.value = true
  try {
    const res = await saveScanTemplate({
      id: editForm.id || undefined,
      name: editForm.name,
      description: editForm.description,
      category: editForm.category,
      tags: editForm.tags,
      config: editForm.config
    })
    if (res.code === 0) {
      ElMessage.success(t('common.saveSuccess'))
      editDialogVisible.value = false
      loadTemplates()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (e) {
    ElMessage.error(t('common.operationFailed'))
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(
      t('task.confirmDeleteTemplate', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
  } catch (e) { return }

  try {
    const res = await deleteScanTemplate({ id: row.id })
    if (res.code === 0) {
      ElMessage.success(t('common.deleteSuccess'))
      loadTemplates()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (e) {
    ElMessage.error(t('common.operationFailed'))
  }
}

async function handleExport() {
  try {
    const ids = selectedRows.value.map(r => r.id)
    const res = await exportScanTemplates({ ids: ids.length > 0 ? ids : undefined })
    if (res.code === 0) {
      // 下载JSON文件
      const blob = new Blob([res.data], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `scan-templates-${new Date().toISOString().slice(0, 10)}.json`
      a.click()
      URL.revokeObjectURL(url)
      ElMessage.success(t('common.exportSuccess'))
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (e) {
    ElMessage.error(t('common.operationFailed'))
  }
}

function showImportDialog() {
  importData.value = ''
  skipExisting.value = true
  importDialogVisible.value = true
}

async function handleImport() {
  if (!importData.value.trim()) {
    ElMessage.warning(t('task.pleaseEnterImportData'))
    return
  }

  // 验证JSON格式
  try {
    JSON.parse(importData.value)
  } catch (e) {
    ElMessage.error(t('task.invalidJsonFormat'))
    return
  }

  importing.value = true
  try {
    const res = await importScanTemplates({
      data: importData.value,
      skipExisting: skipExisting.value
    })
    if (res.code === 0) {
      ElMessage.success(t('task.importResult', { imported: res.imported, skipped: res.skipped }))
      importDialogVisible.value = false
      loadTemplates()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (e) {
    ElMessage.error(t('common.operationFailed'))
  } finally {
    importing.value = false
  }
}

onMounted(() => {
  loadTemplates()
})
</script>

<style scoped>
.scan-template-page {
  padding: 20px;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.toolbar-left {
  display: flex;
  gap: 12px;
}

.toolbar-right {
  display: flex;
  gap: 8px;
}

.template-name-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}

.template-name-cell .name-text {
  font-weight: 500;
}

.pagination-wrapper {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

.config-section {
  margin-top: 16px;
}

.config-title {
  font-weight: 500;
  margin-bottom: 8px;
  color: var(--el-text-color-primary);
}

.config-content {
  background: var(--el-fill-color-light);
  padding: 12px;
  border-radius: 4px;
  font-family: monospace;
  font-size: 12px;
  max-height: 300px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
