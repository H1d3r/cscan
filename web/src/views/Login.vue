<template>
  <div class="login-container">
    <!-- 主题和语言切换按钮 -->
    <div class="controls">
      <!-- 语言切换 -->
      <div class="control-btn" @click="localeStore.toggleLocale">
        <el-icon><Position /></el-icon>
        <span>{{ localeStore.currentLocale === 'zh-CN' ? 'EN' : '中' }}</span>
      </div>
      <!-- 主题切换 -->
      <div class="control-btn" @click="themeStore.toggleTheme">
        <el-icon v-if="themeStore.isDark"><Sunny /></el-icon>
        <el-icon v-else><Moon /></el-icon>
      </div>
    </div>
    
    <div class="login-box">
      <div class="login-header">
        <h1>CSCAN</h1>
        <p>{{ $t('auth.loginTitle') }}</p>
      </div>
      <el-form ref="formRef" :model="form" :rules="rules" class="login-form">
        <el-form-item prop="username">
          <el-input
            v-model="form.username"
            :placeholder="$t('auth.username')"
            prefix-icon="User"
            size="large"
          />
        </el-form-item>
        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            :placeholder="$t('auth.password')"
            prefix-icon="Lock"
            size="large"
            show-password
            @keyup.enter="handleLogin"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            size="large"
            :loading="loading"
            class="login-btn"
            @click="handleLogin"
          >
            {{ $t('auth.login') }}
          </el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- 首次登录强制修改密码弹窗 -->
    <el-dialog
      v-model="resetDialogVisible"
      :title="$t('auth.forceResetTitle', '首次登录请修改密码')"
      width="400px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="false"
      destroy-on-close
    >
      <el-form ref="resetFormRef" :model="resetForm" :rules="resetRules" label-width="auto" label-position="top">
        <el-form-item :label="$t('auth.newPassword', '新密码')" prop="newPassword">
          <el-input v-model="resetForm.newPassword" type="password" show-password />
        </el-form-item>
        <el-form-item :label="$t('auth.confirmPassword', '确认密码')" prop="confirmPassword">
          <el-input v-model="resetForm.confirmPassword" type="password" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button type="primary" :loading="resetLoading" @click="handleResetSubmit" style="width: 100%;">
          {{ $t('common.confirm', '确认修改进入系统') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useUserStore } from '@/stores/user'
import { useThemeStore } from '@/stores/theme'
import { useLocaleStore } from '@/stores/locale'
import { Sunny, Moon, Position } from '@element-plus/icons-vue'
import { resetUserPassword } from '@/api/auth'

const router = useRouter()
const { t } = useI18n()
const userStore = useUserStore()
const themeStore = useThemeStore()
const localeStore = useLocaleStore()
const formRef = ref()
const loading = ref(false)

const form = reactive({
  username: '',
  password: ''
})

const rules = computed(() => ({
  username: [{ required: true, message: t('auth.pleaseEnterUsername'), trigger: 'blur' }],
  password: [{ required: true, message: t('auth.pleaseEnterPassword'), trigger: 'blur' }]
}))

// 强制修密码逻辑
const resetDialogVisible = ref(false)
const resetLoading = ref(false)
const resetFormRef = ref()
const resetForm = reactive({
  newPassword: '',
  confirmPassword: ''
})

const resetRules = computed(() => {
  const validatePass2 = (rule, value, callback) => {
    if (value === '') {
      callback(new Error(t('auth.pleaseConfirmPassword', '请再次输入密码')))
    } else if (value !== resetForm.newPassword) {
      callback(new Error(t('auth.passwordMismatch', '两次输入密码不一致')))
    } else {
      callback()
    }
  }
  return {
    newPassword: [
      { required: true, message: t('auth.pleaseEnterNewPassword', '请输入新密码'), trigger: 'blur' },
      { min: 6, message: t('auth.passwordMinLengths', '密码长度不能小于6位'), trigger: 'blur' }
    ],
    confirmPassword: [
      { required: true, validator: validatePass2, trigger: 'blur' }
    ]
  }
})

async function handleLogin() {
  await formRef.value.validate()
  loading.value = true
  try {
    const res = await userStore.login(form)
    if (res.code === 0) {
      if (res.needChangePwd) {
        resetDialogVisible.value = true
        return // 中断后续的弹窗和页面跳转，等待密码重置
      }
      ElMessage.success(t('auth.loginSuccess'))
      router.push('/dashboard')
    } else {
      ElMessage.error(res.msg || t('auth.loginFailed'))
    }
  } finally {
    loading.value = false
  }
}

async function handleResetSubmit() {
  await resetFormRef.value.validate()
  resetLoading.value = true
  try {
    const res = await resetUserPassword({
      id: userStore.userId,
      newPassword: resetForm.newPassword
    })
    if (res.code === 0) {
      ElMessage.success(t('auth.passwordResetSuccess', '密码修改成功，欢迎进入系统'))
      resetDialogVisible.value = false
      router.push('/dashboard')
    } else {
      ElMessage.error(res.msg || t('auth.passwordResetFailed', '密码修改失败'))
    }
  } catch (error) {
    console.error(error)
    ElMessage.error(t('auth.passwordResetFailed', '请求失败'))
  } finally {
    resetLoading.value = false
  }
}
</script>

<style scoped>
.login-container {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: hsl(var(--background));
  position: relative;
  transition: background 0.3s;
}

.controls {
  position: absolute;
  top: 20px;
  right: 20px;
  display: flex;
  gap: 12px;
}

.control-btn {
  cursor: pointer;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  color: hsl(var(--muted-foreground));
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  transition: all 0.3s;
  
  &:hover {
    transform: scale(1.1);
    border-color: hsl(var(--primary));
    color: hsl(var(--primary));
  }
  
  .el-icon {
    font-size: 18px;
  }
  
  span {
    font-size: 12px;
    font-weight: 600;
  }
}

.login-box {
  width: 400px;
  padding: 40px;
  background: hsl(var(--card));
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.1);
  border: 1px solid hsl(var(--border));
  transition: all 0.3s;
}

.login-header {
  text-align: center;
  margin-bottom: 30px;

  h1 {
    font-size: 32px;
    color: hsl(var(--foreground));
    margin: 0 0 10px;
    letter-spacing: 4px;
    font-weight: 600;
  }

  p {
    color: hsl(var(--muted-foreground));
    margin: 0;
    font-size: 14px;
  }
}

.login-form {
  :deep(.el-input__wrapper) {
    background: hsl(var(--background));
    border: 1px solid hsl(var(--border));
    box-shadow: none;
    border-radius: 8px;
    
    &:hover {
      border-color: hsl(var(--border));
    }
    
    &.is-focus {
      border-color: hsl(var(--primary));
    }
  }
  
  :deep(.el-input__inner) {
    color: hsl(var(--foreground));
    
    &::placeholder {
      color: hsl(var(--muted-foreground));
    }
  }
  
  :deep(.el-input__prefix) {
    color: hsl(var(--muted-foreground));
  }

  .login-btn {
    width: 100%;
    height: 44px;
    background: hsl(var(--primary));
    color: hsl(var(--primary-foreground));
    border: none;
    border-radius: 8px;
    font-size: 16px;
    font-weight: 500;
    letter-spacing: 2px;
    
    &:hover {
      background: hsl(var(--primary) / 0.9);
    }
  }
}
</style>

