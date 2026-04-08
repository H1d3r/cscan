import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '@/api/request'

export function useAssetView(options) {
  const { t } = useI18n()
  const {
    apiPrefix,
    viewType, // 'app', 'icon', 'port'
    localePrefix = options.viewType,
    exportHeaders,
    exportRowFormatter
  } = options

  const proTableRef = ref(null)
  const organizations = ref([])

  const selectedRows = computed(() => proTableRef.value?.selectedRows || [])

  const statLabels = computed(() => ({
    total: t(`asset.${localePrefix}View.total`),
    newCount: t(`asset.${localePrefix}View.newCount`)
  }))

  async function loadOrganizations() {
    try {
      const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
      if (res.code === 0) organizations.value = res.list || []
    } catch (e) {
      console.error(e)
    }
  }

  async function handleDelete(row, emit) {
    try {
      await ElMessageBox.confirm(t(`asset.${localePrefix}View.confirmDelete`), t('common.tip'), { type: 'warning' })
      const res = await request.post(`${apiPrefix}/delete`, { id: row.id })
      if (res.code === 0) {
        ElMessage.success(t('common.deleteSuccess'))
        proTableRef.value?.loadData()
        emit('data-changed')
      }
    } catch (e) {
      // cancelled
    }
  }

  async function handleClear(emit) {
    try {
      await ElMessageBox.confirm(t(`asset.${localePrefix}View.confirmClear`), t('common.warning'), { type: 'error' })
      const res = await request.post(`${apiPrefix}/clear`)
      if (res.code === 0) {
        ElMessage.success(t('asset.clearSuccess'))
        proTableRef.value?.loadData()
        emit('data-changed')
      }
    } catch (e) {
      // cancelled
    }
  }

  function downloadBlob(filename, blob) {
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  function escapeCsvField(field) {
    if (field == null) return ''
    const str = String(field)
    if (str.includes(',') || str.includes('"') || str.includes('\n') || str.includes('\r')) {
      return '"' + str.replace(/"/g, '""') + '"'
    }
    return str
  }

  async function handleExport(command) {
    let data = []

    if (command === 'selected' || command === 'selected-ports') {
      if (selectedRows.value.length === 0) {
        ElMessage.warning(t('common.pleaseSelect'))
        return
      }
      data = selectedRows.value
    } else {
      ElMessage.info(t('asset.gettingAllData'))
      try {
        const res = await request.post(`${apiPrefix}/list`, { ...proTableRef.value?.searchForm, page: 1, pageSize: 10000 })
        if (res.code === 0) {
          data = res.list || []
        } else {
          ElMessage.error(t('asset.getDataFailed'))
          return
        }
      } catch (e) {
        ElMessage.error(t('asset.getDataFailed'))
        return
      }
    }

    if (data.length === 0) {
      ElMessage.warning(t('asset.noDataToExport'))
      return
    }

    if (command === 'csv') {
      const csvRows = [exportHeaders.join(',')]
      for (const row of data) {
        const formattedRow = exportRowFormatter(row).map(escapeCsvField)
        csvRows.push(formattedRow.join(','))
      }
      downloadBlob(`${viewType}s_${new Date().toISOString().slice(0, 10)}.csv`, new Blob(['\uFEFF' + csvRows.join('\n')], { type: 'text/csv;charset=utf-8' }))
      ElMessage.success(t('asset.exportSuccess', { count: data.length }))
      return
    }

    // Default txt export for the primary field
    const lines = data.map(row => row[viewType]).filter(Boolean)
    if (lines.length === 0) {
      ElMessage.warning(t('asset.noDataToExport'))
      return
    }
    downloadBlob(command === 'selected' || command === 'selected-ports' ? `${viewType}s_selected.txt` : `${viewType}s_all.txt`, new Blob([lines.join('\n')], { type: 'text/plain;charset=utf-8' }))
    ElMessage.success(t('asset.exportSuccess', { count: lines.length }))
  }

  return {
    t,
    proTableRef,
    organizations,
    selectedRows,
    statLabels,
    loadOrganizations,
    handleDelete,
    handleClear,
    handleExport
  }
}
