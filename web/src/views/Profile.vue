<template>
  <div class="profile-page">
    <el-card shadow="never" class="profile-card">
      <template #header>
        <div class="profile-header">
          <span class="title">{{ $t('auth.personalCenter') }}</span>
          <span class="subtitle">{{ userStore.username }}</span>
        </div>
      </template>

      <el-tabs v-model="activeTab" class="profile-tabs">
        <!-- Tab 1: 个人信息 -->
        <el-tab-pane :label="$t('profile.tabInfo')" name="info">
          <el-form ref="profileFormRef" :model="profileForm" :rules="profileRules" label-width="100px" class="profile-form">
            <el-form-item :label="$t('profile.avatar')">
              <div class="avatar-updater">
                <el-avatar :size="80" :src="avatarPreview" />
                <el-upload
                  :show-file-list="false"
                  :before-upload="handleAvatarBeforeUpload"
                  :http-request="handleAvatarUpload"
                  :accept="'image/png,image/jpeg,image/gif,image/webp'"
                  class="avatar-upload-btn"
                >
                  <el-button size="small">{{ $t('user.changeAvatar') }}</el-button>
                  <template #tip>
                    <div class="avatar-tip">{{ $t('user.avatarTip', 'JPG/PNG/GIF/WebP, 最大 2MB') }}</div>
                  </template>
                </el-upload>
                <el-button v-if="profileForm.avatar" text type="danger" size="small" @click="clearAvatar">
                  {{ $t('common.delete') }}
                </el-button>
              </div>
            </el-form-item>
            <el-form-item :label="$t('profile.username')" prop="username">
              <el-input v-model="profileForm.username" :disabled="isAdmin" />
            </el-form-item>
            <el-form-item :label="$t('profile.email')" prop="email">
              <el-input v-model="profileForm.email" :placeholder="$t('profile.emailPlaceholder', '可选')" />
            </el-form-item>
            <el-form-item :label="$t('profile.phone')" prop="phone">
              <el-input v-model="profileForm.phone" :placeholder="$t('profile.phonePlaceholder', '可选')" />
            </el-form-item>
            <el-form-item :label="$t('profile.role')">
              <el-tag>{{ roleLabel }}</el-tag>
            </el-form-item>
            <el-form-item :label="$t('profile.status')">
              <el-tag :type="userStore.profile.status === 'enable' ? 'success' : 'danger'">
                {{ userStore.profile.status === 'enable' ? $t('common.enabled') : $t('common.disabled') }}
              </el-tag>
            </el-form-item>
            <el-form-item v-if="userStore.profile.lastLoginTime" :label="$t('profile.lastLogin')">
              <span>{{ formatTime(userStore.profile.lastLoginTime) }}</span>
            </el-form-item>
            <el-form-item v-if="userStore.profile.createTime" :label="$t('profile.createTime')">
              <span>{{ formatTime(userStore.profile.createTime) }}</span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="profileSaving" @click="handleSaveProfile">{{ $t('profile.save') }}</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- Tab 2: 修改密码 -->
        <el-tab-pane :label="$t('profile.tabPassword')" name="password">
          <el-alert type="warning" :closable="false" class="pwd-alert">
            <template #title>
              {{ $t('profile.passwordChangeNotice', '修改密码后将自动退出登录，且所有个人 Token 会被吊销。') }}
            </template>
          </el-alert>
          <el-form ref="pwdFormRef" :model="pwdForm" :rules="pwdRules" label-width="120px" class="pwd-form">
            <el-form-item :label="$t('user.oldPassword')" prop="oldPassword">
              <el-input v-model="pwdForm.oldPassword" type="password" show-password />
            </el-form-item>
            <el-form-item :label="$t('user.newPassword')" prop="newPassword">
              <el-input v-model="pwdForm.newPassword" type="password" show-password />
            </el-form-item>
            <el-form-item :label="$t('user.confirmPassword')" prop="confirmPassword">
              <el-input v-model="pwdForm.confirmPassword" type="password" show-password />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="pwdSaving" @click="handleChangePassword">
                {{ $t('profile.tabPassword') }}
              </el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- Tab 3: API Token -->
        <el-tab-pane :label="$t('profile.tabToken')" name="token">
          <div class="token-toolbar">
            <el-button type="primary" :icon="Plus" @click="showCreateTokenDialog">{{ $t('user.tokenCreate') }}</el-button>
            <el-button @click="loadTokens" :icon="Refresh">{{ $t('common.refresh') }}</el-button>
          </div>
          <el-table :data="tokens" v-loading="tokensLoading" border stripe class="token-table">
            <el-table-column prop="name" :label="$t('user.tokenName')" min-width="140" />
            <el-table-column :label="$t('user.tokenScopes', '可调用 API')" min-width="220">
              <template #default="{ row }">
                <el-tag v-if="!row.scopes || row.scopes.includes('*')" type="success">{{ $t('user.tokenScopeAll', '全部') }}</el-tag>
                <div v-else class="scope-cell-tags">
                  <el-tag v-for="s in row.scopes" :key="s" size="small" effect="plain" class="scope-tag">{{ scopeLabel(s) }}</el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="$t('user.tokenExpires')" width="180">
              <template #default="{ row }">
                <span v-if="row.expiresAt">{{ formatTime(row.expiresAt) }}</span>
                <el-tag v-else type="success">{{ $t('user.tokenPermanent') }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('user.tokenLastUsed', '最近使用')" width="200">
              <template #default="{ row }">
                <div v-if="row.lastUsedAt">
                  <div>{{ formatTime(row.lastUsedAt) }}</div>
                  <div class="muted">{{ row.lastUsedIp || '-' }}</div>
                </div>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('common.status')" width="100">
              <template #default="{ row }">
                <el-switch
                  :model-value="row.status === 'enable'"
                  :loading="row._statusLoading"
                  :disabled="!!row._statusLoading"
                  @change="(val) => handleToggleStatus(row, val)"
                />
              </template>
            </el-table-column>
            <template #empty>
              <span>{{ $t('user.tokenEmpty', '暂无 Token') }}</span>
            </template>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 创建 Token 对话框 -->
    <el-dialog v-model="tokenDialogVisible" :title="$t('user.tokenCreate')" width="720px" top="6vh" append-to-body destroy-on-close>
      <el-form ref="tokenFormRef" :model="tokenForm" :rules="tokenRules" label-width="100px">
        <el-form-item :label="$t('user.tokenName')" prop="name">
          <el-input v-model="tokenForm.name" :placeholder="$t('user.tokenNamePlaceholder', '如：ci-deploy')" />
        </el-form-item>
        <el-form-item :label="$t('user.tokenExpires')" prop="expiresType">
          <el-radio-group v-model="tokenForm.expiresType">
            <el-radio value="permanent">{{ $t('user.tokenPermanent') }}</el-radio>
            <el-radio value="custom">{{ $t('user.tokenCustom') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="tokenForm.expiresType === 'custom'" :label="$t('user.tokenExpireAt', '过期时间')" prop="expiresAt">
          <el-date-picker v-model="tokenForm.expiresAt" type="datetime" :placeholder="$t('user.tokenPickTime', '选择过期时间')" style="width:100%" />
        </el-form-item>
        <el-form-item :label="$t('user.tokenScopes', '可调用 API')">
          <el-radio-group v-model="tokenForm.scopeMode">
            <el-radio value="all">{{ $t('user.tokenScopeAll', '全部 API') }}</el-radio>
            <el-radio value="custom">{{ $t('user.tokenScopeCustom', '自定义分组') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="tokenForm.scopeMode === 'custom'" :label="$t('user.tokenScopeGroups', 'API 分组')">
          <div class="scope-matrix">
            <div class="scope-matrix-toolbar">
              <el-checkbox :model-value="allSelected" :indeterminate="someSelected && !allSelected"
                @change="handleSelectAll">{{ $t('user.tokenScopeSelectAll', '全选') }}</el-checkbox>
              <el-button text size="small" :disabled="noneSelected" @click="handleSelectNone(true)">{{ $t('user.tokenScopeSelectNone', '全不选') }}</el-button>
              <span class="scope-summary">{{ selectedCount }} / {{ totalCount }}</span>
            </div>
            <el-checkbox-group v-model="tokenForm.scopes" class="scope-rows">
              <div v-for="g in scopeGroups" :key="g.value" class="scope-row">
                <div class="scope-row-label">
                  <el-checkbox :model-value="isGroupAll(g)" :indeterminate="isGroupSome(g)"
                    @change="toggleGroupAll(g, $event)">{{ g.label }}</el-checkbox>
                  <span class="scope-row-desc">{{ g.description }}</span>
                </div>
                <div class="scope-row-actions">
                  <el-checkbox v-for="a in g.actions" :key="g.value + ':' + a"
                    :value="g.value + ':' + a" :label="actionLabel(a)" />
                </div>
              </div>
            </el-checkbox-group>
            <div class="scope-tip">{{ $t('user.tokenScopeTip', '未勾选的分组下所有 API 都将被拒绝') }}</div>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tokenDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="tokenCreating" @click="handleCreateToken">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- Token 明文展示对话框：仅在新建后显示，可复制 -->
    <el-dialog v-model="tokenRevealVisible" :title="$t('user.tokenCreated', 'Token 已创建')" width="560px" append-to-body :close-on-click-modal="false">
      <el-alert type="warning" :closable="false">
        <template #title>{{ $t('user.tokenCreatedWarn') }}</template>
      </el-alert>
      <el-input v-model="currentRevealToken" readonly class="token-reveal">
        <template #append>
          <el-button @click="copyCurrentToken">{{ $t('common.copy', '复制') }}</el-button>
        </template>
      </el-input>
      <template #footer>
        <el-button type="primary" @click="tokenRevealVisible = false">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useUserStore, DEFAULT_AVATAR } from '@/stores/user'
import {
  getUserProfile,
  updateUserProfile,
  changeUserPassword,
  createUserToken,
  listUserTokens,
  setUserTokenStatus,
  getUserTokenScopes,
  uploadUserAvatar
} from '@/api/auth'
import { validatePasswordStrength } from '@/utils/validators'

const router = useRouter()
const { t } = useI18n()
const userStore = useUserStore()

const activeTab = ref('info')

// ============== Tab 1: 个人信息 ==============
const profileFormRef = ref()
const profileForm = reactive({ username: '', email: '', phone: '', avatar: '' })
const profileSaving = ref(false)
const avatarUploading = ref(false)
const avatarPreview = computed(() => profileForm.avatar || DEFAULT_AVATAR)

const isAdmin = computed(() => userStore.isAdmin)
const roleLabel = computed(() => (userStore.isAdmin ? t('user.admin') : t('user.user')))

const profileRules = computed(() => ({
  username: [{ required: true, message: t('user.pleaseEnterUsername'), trigger: 'blur' }]
}))

function handleAvatarBeforeUpload(file) {
  const allowed = ['image/jpeg', 'image/png', 'image/gif', 'image/webp']
  if (!allowed.includes(file.type)) {
    ElMessage.error(t('user.avatarFormatError', '仅支持 JPG/PNG/GIF/WebP 格式'))
    return false
  }
  if (file.size > 2 * 1024 * 1024) {
    ElMessage.error(t('user.avatarTooLarge', '头像文件不能超过 2MB'))
    return false
  }
  return true
}

async function handleAvatarUpload({ file }) {
  if (!file) return
  avatarUploading.value = true
  try {
    const res = await uploadUserAvatar(file)
    if (res.code === 0 && res.avatar) {
      profileForm.avatar = res.avatar
      ElMessage.success(t('user.avatarUploadSuccess', '头像上传成功'))
    } else {
      ElMessage.error(res.msg || t('user.avatarUploadFailed', '头像上传失败'))
    }
  } finally {
    avatarUploading.value = false
  }
}

function clearAvatar() {
  profileForm.avatar = ''
}

async function handleSaveProfile() {
  if (!profileFormRef.value) return
  try {
    await profileFormRef.value.validate()
    profileSaving.value = true
    const res = await updateUserProfile({
      username: profileForm.username,
      email: profileForm.email,
      phone: profileForm.phone,
      avatar: profileForm.avatar
    })
    if (res.code === 0) {
      ElMessage.success(res.msg || t('common.operationSuccess'))
      await userStore.refreshProfile()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } finally {
    profileSaving.value = false
  }
}

async function loadProfile() {
  const res = await getUserProfile()
  if (res.code === 0) {
    profileForm.username = res.username || ''
    profileForm.email = res.email || ''
    profileForm.phone = res.phone || ''
    profileForm.avatar = res.avatar || ''
    userStore.setProfile({
      email: res.email || '',
      phone: res.phone || '',
      status: res.status || '',
      lastLoginTime: res.lastLoginTime || 0,
      createTime: res.createTime || 0
    })
  }
}

// ============== Tab 2: 修改密码 ==============
const pwdFormRef = ref()
const pwdForm = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })
const pwdSaving = ref(false)

const pwdRules = computed(() => ({
  oldPassword: [{ required: true, message: t('user.pleaseEnterOldPassword'), trigger: 'blur' }],
  newPassword: [
    { required: true, message: t('user.pleaseEnterNewPassword'), trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        const err = validatePasswordStrength(value)
        if (err) callback(new Error(err))
        else callback()
      },
      trigger: 'blur'
    }
  ],
  confirmPassword: [
    { required: true, message: t('user.pleaseConfirmPassword'), trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        if (value !== pwdForm.newPassword) callback(new Error(t('user.passwordMismatch')))
        else callback()
      },
      trigger: 'blur'
    }
  ]
}))

async function handleChangePassword() {
  if (!pwdFormRef.value) return
  try {
    await pwdFormRef.value.validate()
    pwdSaving.value = true
    const res = await changeUserPassword({
      oldPassword: pwdForm.oldPassword,
      newPassword: pwdForm.newPassword
    })
    if (res.code === 0) {
      ElMessage.success(res.msg || t('profile.passwordChanged', '密码修改成功，请重新登录'))
      // 立即清除 token，防止旧 token 继续发请求触发大量 401
      userStore.logout()
      router.push('/login')
    } else {
      ElMessage.error(res.msg || t('user.passwordResetFailed'))
    }
  } catch (e) {
    // validation failed
  } finally {
    pwdSaving.value = false
  }
}

// ============== Tab 3: Token ==============
const tokens = ref([])
const tokensLoading = ref(false)
const tokenDialogVisible = ref(false)
const tokenCreating = ref(false)
const tokenFormRef = ref()
const tokenForm = reactive({ name: '', expiresType: 'permanent', expiresAt: null, scopeMode: 'all', scopes: [] })
const scopeOptions = ref([])
const scopeGroups = ref([])
const scopeActions = ref([])
const tokenRules = computed(() => ({
  name: [{ required: true, message: t('user.tokenNameRequired', '请输入名称'), trigger: 'blur' }]
}))
const tokenRevealVisible = ref(false)
const currentRevealToken = ref('')

async function loadTokens() {
  tokensLoading.value = true
  try {
    const res = await listUserTokens()
    if (res.code === 0) {
      tokens.value = res.list || []
    } else {
      ElMessage.error(res.msg || t('common.loadFailed'))
    }
  } finally {
    tokensLoading.value = false
  }
}

function showCreateTokenDialog() {
  tokenForm.name = ''
  tokenForm.expiresType = 'permanent'
  tokenForm.expiresAt = null
  tokenForm.scopeMode = 'all'
  tokenForm.scopes = []
  tokenDialogVisible.value = true
}

watch(() => tokenForm.scopeMode, (mode) => {
  if (mode === 'custom' && !scopeGroups.value.length) loadScopes()
})

async function loadScopes() {
  try {
    const res = await getUserTokenScopes()
    if (res.code === 0) {
      scopeOptions.value = res.list || []
      scopeGroups.value = res.groups || []
      scopeActions.value = res.actions || []
    }
  } catch (e) {
    // 静默失败：分组选择器仅是辅助，失败时不阻塞 Token 创建
  }
}

const ACTION_LABELS = {
  read: () => t('user.tokenScopeActionRead', '读'),
  create: () => t('user.tokenScopeActionCreate', '增'),
  update: () => t('user.tokenScopeActionUpdate', '改'),
  delete: () => t('user.tokenScopeActionDelete', '删')
}

function actionLabel(a) {
  const fn = ACTION_LABELS[a]
  return fn ? fn() : a
}

function scopeLabel(value) {
  if (value === '*') return t('user.tokenScopeAll', '全部')
  const [g, a] = value.split(':')
  const group = scopeGroups.value.find(x => x.value === g)
  const grpLabel = group ? group.label : g
  if (!a) return grpLabel
  return `${grpLabel}·${actionLabel(a)}`
}

// ===== Scope matrix: 全选 / 全不选 / 单组全选 =====
const scopeTotalCount = computed(() => scopeGroups.value.reduce((n, g) => n + (g.actions?.length || 0), 0))
const selectedCount = computed(() => tokenForm.scopes.length)
const totalCount = computed(() => scopeTotalCount.value)
const allSelected = computed(() => scopeTotalCount.value > 0 && tokenForm.scopes.length === scopeTotalCount.value)
const noneSelected = computed(() => tokenForm.scopes.length === 0)
const someSelected = computed(() => tokenForm.scopes.length > 0)

function handleSelectAll(val) {
  if (val) {
    const all = []
    for (const g of scopeGroups.value) {
      for (const a of (g.actions || [])) all.push(g.value + ':' + a)
    }
    tokenForm.scopes = all
  } else {
    tokenForm.scopes = []
  }
}

function handleSelectNone() {
  tokenForm.scopes = []
}

function groupScopes(g) {
  return (g.actions || []).map(a => g.value + ':' + a)
}

function isGroupAll(g) {
  const acts = groupScopes(g)
  return acts.length > 0 && acts.every(s => tokenForm.scopes.includes(s))
}

function isGroupSome(g) {
  const acts = groupScopes(g)
  return acts.some(s => tokenForm.scopes.includes(s)) && !isGroupAll(g)
}

function toggleGroupAll(g, checked) {
  const acts = groupScopes(g)
  if (checked) {
    const set = new Set(tokenForm.scopes)
    for (const s of acts) set.add(s)
    tokenForm.scopes = Array.from(set)
  } else {
    tokenForm.scopes = tokenForm.scopes.filter(s => !acts.includes(s))
  }
}

async function handleCreateToken() {
  if (!tokenFormRef.value) return
  try {
    await tokenFormRef.value.validate()
    if (tokenForm.scopeMode === 'custom' && tokenForm.scopes.length === 0) {
      ElMessage.error(t('user.tokenScopeEmpty', '请至少勾选一个 API 分组'))
      return
    }
    tokenCreating.value = true
    let expiresAt = 0
    if (tokenForm.expiresType === 'custom' && tokenForm.expiresAt) {
      expiresAt = Math.floor(new Date(tokenForm.expiresAt).getTime() / 1000)
    }
    const scopes = tokenForm.scopeMode === 'all' ? ['*'] : tokenForm.scopes
    const res = await createUserToken({ name: tokenForm.name, expiresAt, scopes })
    if (res.code === 0) {
      tokenDialogVisible.value = false
      currentRevealToken.value = res.token
      tokenRevealVisible.value = true
      loadTokens()
    } else {
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } finally {
    tokenCreating.value = false
  }
}

// 切换启用/禁用，作用于已存在的 PAT
async function handleToggleStatus(row, val) {
  const target = val ? 'enable' : 'disable'
  const prev = row.status
  row._statusLoading = true
  try {
    const res = await setUserTokenStatus({ id: row.id, status: target })
    if (res.code === 0) {
      row.status = target
      ElMessage.success(t('common.operationSuccess'))
    } else {
      row.status = prev
      ElMessage.error(res.msg || t('common.operationFailed'))
    }
  } catch (e) {
    row.status = prev
  } finally {
    row._statusLoading = false
  }
}

// 查看和复制历史 Token 明文已移除；明文只在新建成功后的当前对话框中展示一次。

function copyCurrentToken() {
  if (!currentRevealToken.value) return
  navigator.clipboard?.writeText(currentRevealToken.value)
  ElMessage.success(t('common.copySuccess', '已复制'))
}

// ============== 通用工具 ==============
function formatTime(unix) {
  if (!unix) return '-'
  const d = new Date(unix * 1000)
  const pad = n => (n < 10 ? '0' + n : n)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

onMounted(() => {
  loadProfile()
  loadTokens()
  loadScopes()
})
</script>

<style lang="scss" scoped>
.profile-page {
  width: 100%;
}
.profile-card {
  width: 100%;
}
.profile-header {
  display: flex;
  align-items: baseline;
  gap: 12px;
  .title { font-size: 18px; font-weight: 600; }
  .subtitle { color: var(--el-text-color-secondary); font-size: 14px; }
}
.profile-form, .pwd-form { max-width: 560px; margin-top: 12px; }
.avatar-updater {
  display: flex; align-items: center; gap: 12px;
  .avatar-tip { color: var(--el-text-color-secondary); font-size: 12px; }
}
.pwd-alert { margin-bottom: 16px; }
.token-toolbar { display: flex; gap: 8px; margin-bottom: 12px; }
.token-table .muted { color: var(--el-text-color-secondary); font-size: 12px; }
.token-reveal { margin-top: 16px; }
.scope-cell-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  .scope-tag { font-size: 11px; }
}
.scope-matrix {
  width: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  padding: 12px 16px;

  .scope-matrix-toolbar {
    display: flex;
    align-items: center;
    gap: 16px;
    padding-bottom: 8px;
    margin-bottom: 8px;
    border-bottom: 1px dashed var(--el-border-color);

    .scope-summary {
      color: var(--el-text-color-secondary);
      font-size: 12px;
      margin-left: auto;
    }
  }

  .scope-rows {
    display: flex;
    flex-direction: column;

    .scope-row {
      display: flex;
      align-items: center;
      gap: 16px;
      padding: 6px 0;
      border-bottom: 1px dashed var(--el-border-color-lighter);

      &:last-child { border-bottom: none; }

      .scope-row-label {
        flex: 0 0 200px;
        display: flex;
        flex-direction: column;
        gap: 2px;

        .scope-row-desc {
          color: var(--el-text-color-secondary);
          font-size: 11px;
          padding-left: 22px;
        }
      }

      .scope-row-actions {
        flex: 1;
        display: flex;
        flex-wrap: wrap;
        gap: 8px 20px;
      }
    }
  }

  .scope-tip {
    color: var(--el-text-color-secondary);
    font-size: 12px;
    margin-top: 8px;
  }
}
</style>
