<template>
  <div class="high-risk-filter-page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('navigation.highRiskFilter') }}</span>
          <el-button type="primary" size="small" @click="saveConfig" :loading="saving">
            {{ $t('highRiskFilter.saveConfig') }}
          </el-button>
        </div>
      </template>

      <el-alert type="info" :closable="false" style="margin-bottom: 20px">
        <template #title>
          {{ $t('highRiskFilter.alertDescription') }}
        </template>
      </el-alert>

      <el-form label-width="120px">
        <el-form-item :label="$t('highRiskFilter.enableFilter')">
          <el-switch v-model="filterConfig.enabled" />
          <span class="hint-secondary" style="margin-left: 10px">
            {{ $t('highRiskFilter.enableFilterHint') }}
          </span>
        </el-form-item>

        <template v-if="filterConfig.enabled">
          <el-divider content-position="left">{{ $t('highRiskFilter.newAssetNotify') }}</el-divider>
          <el-form-item :label="$t('highRiskFilter.newAssetNotifyLabel')">
            <el-switch v-model="filterConfig.newAssetNotify" />
            <span class="hint-secondary" style="margin-left: 10px">
              {{ $t('highRiskFilter.newAssetNotifyHint') }}
            </span>
          </el-form-item>

          <el-divider content-position="left">{{ $t('highRiskFilter.highRiskFingerprints') }}</el-divider>
          <el-form-item :label="$t('highRiskFilter.selectFingerprint')">
            <el-select 
              v-model="filterConfig.highRiskFingerprints" 
              multiple 
              filterable 
              allow-create
              default-first-option
              :placeholder="$t('highRiskFilter.fingerprintPlaceholder')"
              style="width: 100%"
              :loading="fingerprintLoading"
            >
              <el-option-group v-if="existingFingerprints.length > 0" :label="$t('highRiskFilter.existingFingerprints')">
                <el-option v-for="fp in existingFingerprints" :key="fp" :label="fp" :value="fp" />
              </el-option-group>
              <el-option-group v-if="customFingerprints.length > 0" :label="$t('highRiskFilter.fingerprintLibrary')">
                <el-option v-for="fp in customFingerprints" :key="'custom-' + fp" :label="fp" :value="fp" />
              </el-option-group>
            </el-select>
            <div class="hint-secondary" style="margin-top: 4px">
              {{ $t('highRiskFilter.fingerprintHint') }}
            </div>
          </el-form-item>

          <el-divider content-position="left">{{ $t('highRiskFilter.highRiskPorts') }}</el-divider>
          <el-form-item :label="$t('highRiskFilter.selectPort')">
            <el-select 
              v-model="filterConfig.highRiskPorts" 
              multiple 
              filterable 
              allow-create
              default-first-option
              :placeholder="$t('highRiskFilter.portPlaceholder')"
              style="width: 100%"
              :loading="portLoading"
              :reserve-keyword="false"
            >
              <el-option-group :label="$t('highRiskFilter.existingPorts')">
                <el-option v-for="port in existingPorts" :key="port.value" :label="port.label" :value="port.value" />
              </el-option-group>
            </el-select>
            <div class="hint-secondary" style="margin-top: 4px">
              {{ $t('highRiskFilter.portHint') }}
            </div>
          </el-form-item>

          <el-divider content-position="left">{{ $t('highRiskFilter.highRiskSeverity') }}</el-divider>
          <el-form-item :label="$t('highRiskFilter.vulnSeverity')">
            <el-checkbox-group v-model="filterConfig.highRiskPocSeverities">
              <el-checkbox label="critical">{{ $t('highRiskFilter.critical') }}</el-checkbox>
              <el-checkbox label="high">{{ $t('highRiskFilter.high') }}</el-checkbox>
              <el-checkbox label="medium">{{ $t('highRiskFilter.medium') }}</el-checkbox>
              <el-checkbox label="low">{{ $t('highRiskFilter.low') }}</el-checkbox>
            </el-checkbox-group>
            <div class="hint-secondary" style="margin-top: 4px">
              {{ $t('highRiskFilter.severityHint') }}
            </div>
          </el-form-item>
        </template>
      </el-form>
    </el-card>

    <!-- 当前配置预览 -->
    <el-card style="margin-top: 20px" v-if="filterConfig.enabled">
      <template #header>
        <span>{{ $t('highRiskFilter.configPreview') }}</span>
      </template>
      <el-descriptions :column="1" border>
        <el-descriptions-item :label="$t('highRiskFilter.newAssetNotifyLabel')">
          <el-tag :type="filterConfig.newAssetNotify ? 'success' : 'info'" size="small">
            {{ filterConfig.newAssetNotify ? $t('highRiskFilter.enabled') : $t('highRiskFilter.disabled') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('highRiskFilter.highRiskFingerprints')">
          <el-tag v-for="fp in filterConfig.highRiskFingerprints" :key="fp" style="margin-right: 5px; margin-bottom: 5px">
            {{ fp }}
          </el-tag>
          <span v-if="filterConfig.highRiskFingerprints.length === 0" class="text-secondary">{{ $t('highRiskFilter.notConfigured') }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('highRiskFilter.highRiskPorts')">
          <el-tag v-for="port in filterConfig.highRiskPorts" :key="port" type="warning" style="margin-right: 5px; margin-bottom: 5px">
            {{ port }}
          </el-tag>
          <span v-if="filterConfig.highRiskPorts.length === 0" class="text-secondary">{{ $t('highRiskFilter.notConfigured') }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('highRiskFilter.vulnSeverity')">
          <el-tag v-for="level in filterConfig.highRiskPocSeverities" :key="level" :type="getSeverityType(level)" style="margin-right: 5px">
            {{ getSeverityLabel(level) }}
          </el-tag>
          <span v-if="filterConfig.highRiskPocSeverities.length === 0" class="text-secondary">{{ $t('highRiskFilter.notConfigured') }}</span>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import request from '@/api/request'

const { t } = useI18n()

const saving = ref(false)
const fingerprintLoading = ref(false)
const portLoading = ref(false)

// 从数据库获取的已有数据
const existingFingerprints = ref([])
const customFingerprints = ref([])
const existingPorts = ref([])

// 过滤配置
const filterConfig = reactive({
  enabled: false,
  highRiskFingerprints: [],
  highRiskPorts: [],
  highRiskPocSeverities: [],
  newAssetNotify: false
})

onMounted(() => {
  loadExistingFingerprints()
  loadExistingPorts()
  loadFilterConfig()
})

// 加载已有指纹（从扫描结果中）
async function loadExistingFingerprints() {
  fingerprintLoading.value = true
  try {
    // 获取扫描结果中已识别的指纹
    const res = await request.post('/asset/fingerprints/list', { limit: 500 })
    const fingerprintList = res.data?.list || res.list || []
    if (res.code === 0 && fingerprintList.length > 0) {
      existingFingerprints.value = [...new Set(fingerprintList.map(item => item.name || item))]
    }
    
    // 获取指纹库中的指纹名称
    const fpRes = await request.post('/fingerprint/list', { page: 1, pageSize: 500 })
    const fpList = fpRes.data?.list || fpRes.list || []
    if (fpRes.code === 0 && fpList.length > 0) {
      customFingerprints.value = [...new Set(fpList.map(item => item.name))]
    }
  } catch (e) {
    console.error('Load fingerprints error:', e)
  } finally {
    fingerprintLoading.value = false
  }
}

// 加载已有端口（从扫描结果中）
async function loadExistingPorts() {
  portLoading.value = true
  try {
    const res = await request.post('/asset/ports/stats', {})
    const portList = res.data?.list || res.list || []
    if (res.code === 0 && portList.length > 0) {
      existingPorts.value = portList.map(item => ({
        label: `${item.port} (${item.service || '未知'}) - ${item.count}个`,
        value: item.port
      }))
    }
  } catch (e) {
    console.error('Load ports error:', e)
  } finally {
    portLoading.value = false
  }
}

// 加载已保存的过滤配置
async function loadFilterConfig() {
  try {
    const res = await request.post('/notify/highrisk/config/get', {})
    const config = res.data?.config || res.config
    if (res.code === 0 && config) {
      filterConfig.enabled = config.enabled || false
      filterConfig.highRiskFingerprints = config.highRiskFingerprints || []
      filterConfig.highRiskPorts = config.highRiskPorts || []
      filterConfig.highRiskPocSeverities = config.highRiskPocSeverities || []
      filterConfig.newAssetNotify = config.newAssetNotify || false
    }
  } catch (e) {
    console.error('Load filter config error:', e)
  }
}

// 保存配置
async function saveConfig() {
  saving.value = true
  try {
    const res = await request.post('/notify/highrisk/config/save', {
      enabled: filterConfig.enabled,
      highRiskFingerprints: filterConfig.highRiskFingerprints,
      highRiskPorts: filterConfig.highRiskPorts,
      highRiskPocSeverities: filterConfig.highRiskPocSeverities,
      newAssetNotify: filterConfig.newAssetNotify
    })
    if (res.code === 0) {
      ElMessage.success(t('common.operationSuccess'))
      loadFilterConfig()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (e) {
    ElMessage.error(t('common.operationFailed') + ': ' + e.message)
  } finally {
    saving.value = false
  }
}

function getSeverityType(level) {
  const types = { critical: 'danger', high: 'warning', medium: '', low: 'info' }
  return types[level] || 'info'
}

function getSeverityLabel(level) {
  const labels = { 
    critical: t('highRiskFilter.critical'), 
    high: t('highRiskFilter.high'), 
    medium: t('highRiskFilter.medium'), 
    low: t('highRiskFilter.low') 
  }
  return labels[level] || level
}
</script>

<style scoped>
.high-risk-filter-page {
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
}
</style>
