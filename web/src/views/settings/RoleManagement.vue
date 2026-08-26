<template>
  <div class="role-management-page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('roleManagement.title') }}</span>
          <el-button type="primary" size="small" @click="showRoleDialog()">
            <el-icon><Plus /></el-icon>{{ $t('roleManagement.newRole') }}
          </el-button>
        </div>
      </template>
      <div class="role-toolbar">
        <el-input
          v-model="searchKeyword"
          :placeholder="$t('roleManagement.searchPlaceholder', '搜索角色名称')"
          clearable
          size="small"
          style="width: 200px"
        >
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
      </div>
      <el-table :data="pagedRoleList" v-loading="roleLoading" stripe max-height="500">
        <el-table-column prop="name" :label="$t('roleManagement.roleName')" min-width="120" />
        <el-table-column prop="displayName" :label="$t('roleManagement.displayName')" min-width="120" />
        <el-table-column prop="description" :label="$t('common.description')" min-width="180" />
        <el-table-column :label="$t('roleManagement.menuPermissions')" min-width="200">
          <template #default="{ row }">
            <el-tag v-for="path in row.menuPaths?.slice(0, 3)" :key="path" size="small" class="menu-tag">
              {{ path }}
            </el-tag>
            <span v-if="!row.menuPaths || row.menuPaths.length === 0" class="no-perms">{{
              $t('roleManagement.noPermissions')
            }}</span>
            <el-tag v-if="row.menuPaths && row.menuPaths.length > 3" size="small" type="info">
              +{{ row.menuPaths.length - 3 }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.isBuiltIn" type="warning">{{ $t('roleManagement.builtIn') }}</el-tag>
            <el-tag v-else type="success">{{ $t('common.custom') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="180" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showRoleDialog(row)">{{ $t('common.edit') }}</el-button>
            <el-button
              v-if="!row.isBuiltIn"
              type="danger"
              link
              size="small"
              @click="handleDeleteRole(row)"
            >{{ $t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="role-pagination">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :total="filteredRoleList.length"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next"
          small
        />
      </div>
    </el-card>

    <!-- 角色对话框 -->
    <el-dialog
      v-model="roleDialogVisible"
      :title="roleForm.id ? $t('roleManagement.editRole') : $t('roleManagement.newRole')"
      width="600px"
    >
      <el-form ref="roleFormRef" :model="roleForm" :rules="roleRules" label-width="100px">
        <el-form-item :label="$t('roleManagement.roleName')" prop="name">
          <el-input v-model="roleForm.name" :placeholder="$t('roleManagement.pleaseEnterRoleName')" :disabled="!!roleForm.id" />
        </el-form-item>
        <el-form-item :label="$t('roleManagement.displayName')" prop="displayName">
          <el-input v-model="roleForm.displayName" :placeholder="$t('roleManagement.pleaseEnterDisplayName')" />
        </el-form-item>
        <el-form-item :label="$t('common.description')">
          <el-input
            v-model="roleForm.description"
            type="textarea"
            :rows="2"
            :placeholder="$t('roleManagement.pleaseEnterDescription')"
          />
        </el-form-item>
        <el-form-item :label="$t('roleManagement.menuPermissions')">
          <el-transfer
            v-model="roleForm.menuPaths"
            :data="menuTransferData"
            :titles="[$t('roleManagement.availableMenus'), $t('roleManagement.selectedMenus')]"
            :props="{ key: 'path', label: 'path' }"
            filterable
            filter-placeholder=""
          />
        </el-form-item>
        <el-form-item v-if="!roleForm.isBuiltIn" :label="$t('roleManagement.superadmin')">
          <el-switch v-model="roleForm.isSuperadmin" />
          <span class="form-tip">{{ $t('roleManagement.superadminTip', '设置为超级管理员角色') }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="roleDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button
          type="primary"
          :loading="roleSubmitting"
          @click="handleRoleSubmit"
        >{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import {
  getRoleList, createRole, updateRole, deleteRole,
  syncRoleMenus
} from '@/api/auth'
import { useRouter } from 'vue-router'

const { t } = useI18n()
const router = useRouter()

const roleLoading = ref(false)
const roleList = ref([])
const allMenuPaths = ref([])

// 搜索与分页
const searchKeyword = ref('')
const currentPage = ref(1)
const pageSize = ref(10)

const filteredRoleList = computed(() => {
  if (!searchKeyword.value) return roleList.value
  const kw = searchKeyword.value.toLowerCase()
  return roleList.value.filter(r => r.name?.toLowerCase().includes(kw) || r.displayName?.toLowerCase().includes(kw))
})

const pagedRoleList = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredRoleList.value.slice(start, start + pageSize.value)
})

const roleDialogVisible = ref(false)
const roleSubmitting = ref(false)
const roleFormRef = ref()
const roleForm = ref({
  id: '', name: '', displayName: '', description: '', menuPaths: [], isBuiltIn: false, isSuperadmin: false
})

const roleRules = computed(() => ({
  name: [{ required: true, message: t('roleManagement.pleaseEnterRoleName'), trigger: 'blur' }],
  displayName: [{ required: true, message: t('roleManagement.pleaseEnterDisplayName'), trigger: 'blur' }]
}))

// 构建 Transfer 组件数据源
const menuTransferData = computed(() => {
  return allMenuPaths.value.map(path => ({ path, key: path, label: path }))
})

onMounted(() => {
  loadMenuPaths()
  loadRoleList()
})

async function loadMenuPaths() {
  try {
    const res = await syncRoleMenus()
    if (res.code === 0 && res.menuPaths) {
      allMenuPaths.value = res.menuPaths
    }
  } catch (err) {
    console.error('load menu paths failed:', err)
  }
}

async function loadRoleList() {
  roleLoading.value = true
  try {
    const res = await getRoleList({ page: 1, pageSize: 100 })
    if (res.code === 0) roleList.value = res.list || []
  } finally {
    roleLoading.value = false
  }
}

function showRoleDialog(row = null) {
  if (row) {
    roleForm.value = {
      id: row.id,
      name: row.name,
      displayName: row.displayName,
      description: row.description || '',
      menuPaths: [...(row.menuPaths || [])],
      isBuiltIn: row.isBuiltIn || false,
      isSuperadmin: row.isSuperadmin || false
    }
  } else {
    roleForm.value = {
      id: '', name: '', displayName: '', description: '', menuPaths: [], isBuiltIn: false, isSuperadmin: false
    }
  }
  roleDialogVisible.value = true
}

async function handleRoleSubmit() {
  if (!roleFormRef.value) return
  try {
    await roleFormRef.value.validate()
    roleSubmitting.value = true
    const payload = {
      name: roleForm.value.name,
      displayName: roleForm.value.displayName,
      description: roleForm.value.description,
      menuPaths: roleForm.value.menuPaths
    }
    if (roleForm.value.id) {
      // 更新
      payload.isSuperadmin = roleForm.value.isSuperadmin ? true : undefined
      const res = await updateRole({ ...payload, name: roleForm.value.name })
      if (res.code === 0) {
        ElMessage.success(res.msg || t('common.operationSuccess'))
        roleDialogVisible.value = false
        loadRoleList()
      } else {
        ElMessage.error(res.msg || t('common.operationFailed'))
      }
    } else {
      // 创建
      const res = await createRole(payload)
      if (res.code === 0) {
        ElMessage.success(res.msg || t('common.operationSuccess'))
        roleDialogVisible.value = false
        loadRoleList()
      } else {
        ElMessage.error(res.msg || t('common.operationFailed'))
      }
    }
  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    roleSubmitting.value = false
  }
}

async function handleDeleteRole(row) {
  try {
    await ElMessageBox.confirm(t('roleManagement.confirmDelete', `确定删除角色 "${row.displayName || row.name}"？`), t('common.tip'), { type: 'warning' })
    const res = await deleteRole({ name: row.name })
    if (res.code === 0) {
      ElMessage.success(res.msg || t('common.deleteSuccess'))
      loadRoleList()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除角色失败:', error)
    }
  }
}
</script>

<style scoped>
.role-management-page .card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 16px;
  font-weight: 500;
}

.role-toolbar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.role-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.menu-tag {
  margin-right: 4px;
  margin-bottom: 4px;
}

.no-perms {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.form-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-left: 8px;
}
</style>
