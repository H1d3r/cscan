<template>
  <div class="scan-template-select">
    <!-- 模板选择区域 -->
    <div class="template-header">
      <span class="template-label">{{ $t('task.scanTemplate') }}</span>
      <el-button type="primary" link @click="showTemplateDialog">
        {{ selectedTemplate ? $t('task.changeTemplate') : $t('task.selectTemplate') }}
      </el-button>
      <el-button v-if="showSaveButton" type="success" link @click="showSaveDialog">
        {{ $t('task.saveAsTemplate') }}
      </el-button>
    </div>
    
    <!-- 已选模板显示 -->
    <div v-if="selectedTemplate" class="selected-template">
      <el-tag :type="getCategoryType(selectedTemplate.category)" effect="light">
        {{ getCategoryLabel(selectedTemplate.category) }}
      </el-tag>
      <span class="template-name">{{ selectedTemplate.name }}</span>
      <span class="template-desc">{{ selectedTemplate.description }}</span>
      <el-button type="danger" link size="small" @click="clearTemplate">
        <el-icon><Close /></el-icon>
      </el-button>
    </div>

    <!-- 模板选择对话框 -->
    <el-dialog 
      v-model="dialogVisible" 
      :title="$t('task.selectScanTemplate')" 
      width="900px"
      destroy-on-close
    >
      <!-- 筛选区域 -->
      <div class="template-filter">
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

      <!-- 模板列表 -->
      <el-table :data="templateList" v-loading="loading" max-height="400" @row-click="handleSelectTemplate">
        <el-table-column prop="name" :label="$t('task.templateName')" min-width="150">
          <template #default="{ row }">
            <div class="template-name-cell">
              <el-tag v-if="row.isBuiltin" type="info" size="small">{{ $t('task.builtin') }}</el-tag>
              <span>{{ row.name }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="category" :label="$t('task.category')" width="100">
          <template #default="{ row }">
            <el-tag :type="getCategoryType(row.category)" size="small">
              {{ getCategoryLabel(row.category) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" :label="$t('task.description')" min-width="200" show-overflow-tooltip />
        <el-table-column prop="useCount" :label="$t('task.useCount')" width="80" align="center" />
        <el-table-column :label="$t('common.operation')" width="120" align="center">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click.stop="confirmSelect(row)">{{ $t('common.select') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="template-pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next"
          @size-change="loadTemplates"
          @current-change="loadTemplates"
        />
      </div>
    </el-dialog>

    <!-- 保存模板对话框 -->
    <el-dialog v-model="saveDialogVisible" :title="$t('task.saveAsTemplate')" width="500px">
      <el-form ref="saveFormRef" :model="saveForm" :rules="saveRules" label-width="100px">
        <el-form-item :label="$t('task.templateName')" prop="name">
          <el-input v-model="saveForm.name" :placeholder="$t('task.pleaseEnterTemplateName')" />
        </el-form-item>
        <el-form-item :label="$t('task.description')">
          <el-input v-model="saveForm.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item :label="$t('task.category')">
          <el-select v-model="saveForm.category" style="width: 100%">
            <el-option :label="$t('task.quickScan')" value="quick" />
            <el-option :label="$t('task.standardScan')" value="standard" />
            <el-option :label="$t('task.fullScan')" value="full" />
            <el-option :label="$t('task.customTemplate')" value="custom" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="saveDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="handleSaveTemplate">{{ $t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Search, Close } from '@element-plus/icons-vue'
import { getScanTemplateList, saveScanTemplate, useScanTemplate } from '@/api/task'

const props = defineProps({
  modelValue: { type: Object, default: null },
  showSaveButton: { type: Boolean, default: true },
  currentConfig: { type: Object, default: null }
})

const emit = defineEmits(['update:modelValue', 'select', 'configLoaded'])

const { t } = useI18n()
const dialogVisible = ref(false)
const saveDialogVisible = ref(false)
const loading = ref(false)
const saving = ref(false)
const templateList = ref([])
const selectedTemplate = ref(props.modelValue)
const saveFormRef = ref()

const filter = reactive({
  keyword: '',
  category: ''
})

const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

const saveForm = reactive({
  name: '',
  description: '',
  category: 'custom'
})

const saveRules = {
  name: [{ required: true, message: () => t('task.pleaseEnterTemplateName'), trigger: 'blur' }]
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

function showTemplateDialog() {
  dialogVisible.value = true
  loadTemplates()
}

function showSaveDialog() {
  saveForm.name = ''
  saveForm.description = ''
  saveForm.category = 'custom'
  saveDialogVisible.value = true
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

function handleSelectTemplate(row) {
  confirmSelect(row)
}

async function confirmSelect(template) {
  // 增加使用计数
  try {
    await useScanTemplate({ id: template.id })
  } catch (e) { /* ignore */ }

  selectedTemplate.value = template
  emit('update:modelValue', template)
  emit('select', template)
  
  // 解析配置并触发事件
  if (template.config) {
    try {
      const config = JSON.parse(template.config)
      emit('configLoaded', config)
    } catch (e) {
      console.error('Parse template config failed:', e)
    }
  }
  
  dialogVisible.value = false
  ElMessage.success(t('task.templateSelected'))
}

function clearTemplate() {
  selectedTemplate.value = null
  emit('update:modelValue', null)
}

async function handleSaveTemplate() {
  try {
    await saveFormRef.value.validate()
  } catch (e) { return }

  if (!props.currentConfig) {
    ElMessage.warning(t('task.noConfigToSave'))
    return
  }

  saving.value = true
  try {
    const res = await saveScanTemplate({
      name: saveForm.name,
      description: saveForm.description,
      category: saveForm.category,
      config: JSON.stringify(props.currentConfig)
    })
    if (res.code === 0) {
      ElMessage.success(t('task.templateSaved'))
      saveDialogVisible.value = false
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (e) {
    ElMessage.error(t('common.operationFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  if (props.modelValue) {
    selectedTemplate.value = props.modelValue
  }
})
</script>

<style scoped>
.scan-template-select {
  margin-bottom: 16px;
}

.template-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.template-label {
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.selected-template {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}

.selected-template .template-name {
  font-weight: 500;
}

.selected-template .template-desc {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.template-filter {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.template-name-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}

.template-pagination {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

.form-hint {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
