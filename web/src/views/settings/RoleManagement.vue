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
            <span v-if="!row.menuPaths || row.menuPaths.length === 0" class="no-perms">{{
              $t('roleManagement.noPermissions')
            }}</span>
            <template v-else>
              <el-tag v-for="path in row.menuPaths.slice(0, 3)" :key="path" size="small" class="menu-tag">
                {{ pathLabel(path) }}
              </el-tag>
              <el-tag v-if="row.menuPaths.length > 3" size="small" type="info">
                +{{ row.menuPaths.length - 3 }}
              </el-tag>
            </template>
          </template>
        </el-table-column>
        <el-table-column :label="$t('roleManagement.builtIn')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.isBuiltIn" type="warning">{{ $t('roleManagement.builtIn') }}</el-tag>
            <el-tag v-else type="success">{{ $t('common.custom') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('roleManagement.superadmin')" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.isSuperadmin" type="danger">{{ $t('common.yes') }}</el-tag>
            <span v-else class="no-perms">{{ $t('common.no') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="180" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showRoleDialog(row)">{{
              row.name === 'superadmin' ? $t('common.detail') : $t('common.edit')
            }}</el-button>
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
      width="720px"
    >
      <el-form ref="roleFormRef" :model="roleForm" :rules="roleRules" label-width="100px">
        <el-form-item :label="$t('roleManagement.roleName')" prop="name">
          <el-input v-model="roleForm.name" :placeholder="$t('roleManagement.pleaseEnterRoleName')" :disabled="!!roleForm.id" />
          <div v-if="!roleForm.id" class="form-tip">{{ $t('roleManagement.roleNameTip') }}</div>
        </el-form-item>
        <el-form-item :label="$t('roleManagement.displayName')" prop="displayName">
          <el-input v-model="roleForm.displayName" :placeholder="$t('roleManagement.pleaseEnterDisplayName')" :disabled="isReadonlyRole" />
        </el-form-item>
        <el-form-item :label="$t('common.description')">
          <el-input
            v-model="roleForm.description"
            type="textarea"
            :rows="2"
            :placeholder="$t('roleManagement.pleaseEnterDescription')"
            :disabled="isReadonlyRole"
          />
        </el-form-item>
        <el-form-item :label="$t('roleManagement.menuPermissions')">
          <div class="menu-perm-panel">
            <div class="menu-perm-toolbar">
              <el-button link type="primary" size="small" :disabled="isReadonlyRole" @click="selectAllMenus">{{
                $t('roleManagement.selectAll')
              }}</el-button>
              <el-button link type="primary" size="small" :disabled="isReadonlyRole" @click="clearAllMenus">{{
                $t('roleManagement.clearAll')
              }}</el-button>
              <span class="menu-perm-count">{{
                $t('roleManagement.selectedCount', { count: roleForm.menuPaths.length })
              }}</span>
            </div>
            <div v-for="group in menuGroups" :key="group.key" class="menu-perm-group">
              <el-checkbox
                :model-value="groupState(group).all"
                :indeterminate="groupState(group).some"
                :disabled="isReadonlyRole"
                @change="toggleGroup(group, $event)"
              >{{ group.label }}</el-checkbox>
              <div class="menu-perm-items">
                <el-checkbox
                  v-for="child in group.children"
                  :key="child.path"
                  v-model="roleForm.menuPaths"
                  :value="child.path"
                  :disabled="isReadonlyRole"
                >{{ child.label }}</el-checkbox>
              </div>
            </div>
          </div>
        </el-form-item>
        <el-form-item v-if="!roleForm.isBuiltIn" :label="$t('roleManagement.superadmin')">
          <el-switch v-model="roleForm.isSuperadmin" />
          <span class="form-tip">{{ $t('roleManagement.superadminTip') }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="roleDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button
          v-if="!isReadonlyRole"
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
  getRoleMenuOptions
} from '@/api/auth'
import { useUserStore } from '@/stores/user'
import { buildMenuGroups } from '@/config/menu'

const { t } = useI18n()
const userStore = useUserStore()

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

// superadmin 为系统兜底角色，后端禁止修改，此处仅供查看
const isReadonlyRole = computed(() => roleForm.value.name === 'superadmin')

const roleRules = computed(() => ({
  name: [{ required: true, message: t('roleManagement.pleaseEnterRoleName'), trigger: 'blur' }],
  displayName: [{ required: true, message: t('roleManagement.pleaseEnterDisplayName'), trigger: 'blur' }]
}))

// 按侧边栏分组展示可授权菜单，与实际菜单结构保持一致
const menuGroups = computed(() => buildMenuGroups(t, allMenuPaths.value))

// path → 菜单名映射，用于列表页把路径显示为可读名称
const pathLabelMap = computed(() => {
  const map = {}
  for (const group of menuGroups.value) {
    for (const child of group.children) map[child.path] = child.label
  }
  return map
})

function pathLabel(path) {
  return pathLabelMap.value[path] || path
}

// groupState 计算分组勾选态：all 全选、some 半选
function groupState(group) {
  const selected = group.children.filter(c => roleForm.value.menuPaths.includes(c.path)).length
  return { all: selected === group.children.length, some: selected > 0 && selected < group.children.length }
}

function toggleGroup(group, checked) {
  const paths = group.children.map(c => c.path)
  const current = new Set(roleForm.value.menuPaths)
  paths.forEach(p => checked ? current.add(p) : current.delete(p))
  roleForm.value.menuPaths = allMenuPaths.value.filter(p => current.has(p))
}

function selectAllMenus() {
  roleForm.value.menuPaths = [...allMenuPaths.value]
}

function clearAllMenus() {
  roleForm.value.menuPaths = []
}

onMounted(() => {
  loadMenuPaths()
  loadRoleList()
})

async function loadMenuPaths() {
  try {
    const res = await getRoleMenuOptions()
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
    const res = await getRoleList({})
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
  } catch (error) {
    return
  }

  roleSubmitting.value = true
  try {
    const payload = {
      name: roleForm.value.name,
      displayName: roleForm.value.displayName,
      description: roleForm.value.description,
      menuPaths: roleForm.value.menuPaths,
      isSuperadmin: roleForm.value.isSuperadmin
    }
    const res = roleForm.value.id ? await updateRole(payload) : await createRole(payload)
    if (res.code === 0) {
      ElMessage.success(res.msg || t('common.operationSuccess'))
      roleDialogVisible.value = false
      loadRoleList()
      // 若改动的是当前登录用户的角色，立即刷新自己的菜单权限
      if (roleForm.value.name === userStore.role) userStore.syncMenus()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } finally {
    roleSubmitting.value = false
  }
}

async function handleDeleteRole(row) {
  try {
    await ElMessageBox.confirm(
      t('roleManagement.confirmDelete', { name: row.displayName || row.name }),
      t('common.tip'),
      { type: 'warning' }
    )
  } catch (error) {
    return
  }

  const res = await deleteRole({ name: row.name })
  if (res.code === 0) {
    ElMessage.success(res.msg || t('common.deleteSuccess'))
    loadRoleList()
  } else {
    ElMessage.error(res.msg || t('common.operationFailed'))
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

.menu-perm-panel {
  width: 100%;
  max-height: 340px;
  overflow-y: auto;
  padding: 8px 12px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
}

.menu-perm-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.menu-perm-count {
  margin-left: auto;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.menu-perm-group {
  padding: 8px 0;
}

.menu-perm-group + .menu-perm-group {
  border-top: 1px solid var(--el-border-color-lighter);
}

.menu-perm-items {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 20px;
  padding-left: 24px;
}
</style>
