<template>
  <div class="user-page">
    <el-card class="action-card">
      <el-button type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>{{ $t('user.newUser') }}
      </el-button>
    </el-card>

    <el-card>
      <el-table :data="tableData" v-loading="loading" stripe max-height="500">
        <el-table-column prop="username" :label="$t('user.userName')" min-width="150" />
        <el-table-column prop="status" :label="$t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'enable' ? 'success' : 'danger'">
              {{ row.status === 'enable' ? $t('common.enabled') : $t('common.disabled') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.operation')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="showEditDialog(row)">{{ $t('common.edit') }}</el-button>
            <el-button type="warning" link size="small" @click="showResetPasswordDialog(row)">{{ $t('user.resetPassword') }}</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建/编辑用户对话框 -->
    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="500px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item :label="$t('user.userName')" prop="username">
          <el-input v-model="form.username" :placeholder="$t('user.pleaseEnterUsername')" />
        </el-form-item>
        <el-form-item v-if="!form.id" :label="$t('user.password')" prop="password">
          <el-input v-model="form.password" type="password" :placeholder="$t('user.pleaseEnterPassword')" />
        </el-form-item>
        <el-form-item :label="$t('common.status')" prop="status">
          <el-select v-model="form.status" :placeholder="$t('user.pleaseSelectStatus')">
            <el-option :label="$t('common.enabled')" value="enable" />
            <el-option :label="$t('common.disabled')" value="disable" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 重置密码对话框 -->
    <el-dialog v-model="resetPasswordVisible" :title="$t('user.resetPassword')" width="400px">
      <el-form ref="resetFormRef" :model="resetForm" :rules="resetRules" label-width="80px">
        <el-form-item :label="$t('user.oldPassword')" prop="oldPassword">
          <el-input v-model="resetForm.oldPassword" type="password" :placeholder="$t('user.pleaseEnterOldPassword')" show-password />
        </el-form-item>
        <el-form-item :label="$t('user.newPassword')" prop="newPassword">
          <el-input v-model="resetForm.newPassword" type="password" :placeholder="$t('user.pleaseEnterNewPassword')" show-password />
        </el-form-item>
        <el-form-item :label="$t('user.confirmPassword')" prop="confirmPassword">
          <el-input v-model="resetForm.confirmPassword" type="password" :placeholder="$t('user.pleaseConfirmPassword')" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="resetPasswordVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleResetPassword" :loading="resetting">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getUserList, createUser, updateUser, deleteUser, resetUserPassword } from '@/api/auth'

const { t } = useI18n()
const loading = ref(false)
const tableData = ref([])
const dialogVisible = ref(false)
const resetPasswordVisible = ref(false)
const submitting = ref(false)
const resetting = ref(false)

const form = ref({
  id: '',
  username: '',
  password: '',
  status: 'enable'
})

const resetForm = ref({
  id: '',
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

const formRef = ref()
const resetFormRef = ref()

const rules = computed(() => ({
  username: [{ required: true, message: t('user.pleaseEnterUsername'), trigger: 'blur' }],
  password: [{ required: true, message: t('user.pleaseEnterPassword'), trigger: 'blur' }],
  status: [{ required: true, message: t('user.pleaseSelectStatus'), trigger: 'change' }]
}))

const resetRules = computed(() => ({
  oldPassword: [{ required: true, message: t('user.pleaseEnterOldPassword'), trigger: 'blur' }],
  newPassword: [{ required: true, message: t('user.pleaseEnterNewPassword'), trigger: 'blur' }],
  confirmPassword: [
    { required: true, message: t('user.pleaseConfirmPassword'), trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        if (value !== resetForm.value.newPassword) {
          callback(new Error(t('user.passwordMismatch')))
        } else {
          callback()
        }
      },
      trigger: 'blur'
    }
  ]
}))

const dialogTitle = computed(() => form.value.id ? t('user.editUser') : t('user.newUser'))

onMounted(() => loadData())

async function loadData() {
  loading.value = true
  try {
    const res = await getUserList({ page: 1, pageSize: 100 })
    if (res.code === 0) tableData.value = res.list || []
  } finally {
    loading.value = false
  }
}

function showCreateDialog() {
  form.value = { id: '', username: '', password: '', status: 'enable' }
  dialogVisible.value = true
}

function showEditDialog(row) {
  form.value = { ...row, password: '' }
  dialogVisible.value = true
}

function showResetPasswordDialog(row) {
  resetForm.value = { id: row.id, oldPassword: '', newPassword: '', confirmPassword: '' }
  resetPasswordVisible.value = true
}

async function handleSubmit() {
  if (!formRef.value) return
  
  try {
    await formRef.value.validate()
    submitting.value = true
    
    const api = form.value.id ? updateUser : createUser
    const res = await api(form.value)
    
    if (res.code === 0) {
      ElMessage.success(res.msg || t('common.operationSuccess'))
      dialogVisible.value = false
      loadData()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    submitting.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('user.confirmDeleteUser'), t('common.tip'), {
      confirmButtonText: t('common.confirm'),
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    })
    
    const res = await deleteUser({ id: row.id })
    if (res.code === 0) {
      ElMessage.success(res.msg || t('common.deleteSuccess'))
      loadData()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (error) {
    // 用户取消删除
  }
}

async function handleResetPassword() {
  if (!resetFormRef.value) return
  
  try {
    await resetFormRef.value.validate()
    resetting.value = true
    
    const res = await resetUserPassword({
      id: resetForm.value.id,
      oldPassword: resetForm.value.oldPassword,
      newPassword: resetForm.value.newPassword
    })
    
    if (res.code === 0) {
      ElMessage.success(res.msg || t('user.passwordResetSuccess'))
      resetPasswordVisible.value = false
    } else {
      ElMessage.error(res.msg || t('user.passwordResetFailed'))
    }
  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    resetting.value = false
  }
}
</script>

<style scoped>
.user-page {
  .action-card { margin-bottom: 20px; }
}
</style>

