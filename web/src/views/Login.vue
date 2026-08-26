<template>
  <!-- 系统状态检测中：骨架加载，避免登录表单先闪现再切换到向导 -->
  <div v-if="systemChecking" class="login-loading">
    <el-icon class="is-loading" :size="32"><Loading /></el-icon>
  </div>
  <!-- 首次部署：渲染引导式注册向导 -->
  <SetupWizard v-else-if="showSetupWizard" @completed="onWizardCompleted" />

  <!-- 正常登录/注册 -->
  <div v-else class="login-container">
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

    <div :class="['login-box', `style-${themeStore.themeStyle}`]">
      <div class="login-header">
        <img class="login-logo" :src="brandingStore.logoSrc" alt="logo" />
        <h1>{{ brandingStore.displayTitle }}</h1>
        <p v-if="isRegisterMode">{{ $t('auth.register') }}</p>
        <p v-else>{{ $t('auth.loginTitle') }}</p>
      </div>

      <!-- 登录表单 -->
      <el-form v-if="!isRegisterMode" ref="loginFormRef" :model="loginForm" :rules="loginRules" class="login-form">
        <el-form-item prop="username">
          <el-input
            v-model="loginForm.username"
            :placeholder="$t('auth.username')"
            prefix-icon="User"
            size="large"
          />
        </el-form-item>
        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
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
            :loading="loginLoading"
            class="login-btn"
            @click="handleLogin"
          >
            {{ $t('auth.login') }}
          </el-button>
        </el-form-item>
        <div v-if="registerEnabled" class="form-footer">
          <span class="toggle-text" @click="toggleMode">
            {{ $t('auth.noAccount') }} <span class="toggle-link">{{ $t('auth.register') }}</span>
          </span>
        </div>
      </el-form>

      <!-- 注册表单 -->
      <el-form v-else ref="registerFormRef" :model="registerForm" :rules="registerRules" class="login-form">
        <el-form-item prop="username">
          <el-input
            v-model="registerForm.username"
            :placeholder="$t('auth.username')"
            prefix-icon="User"
            size="large"
          />
        </el-form-item>
        <el-form-item prop="password">
          <el-input
            v-model="registerForm.password"
            type="password"
            :placeholder="$t('auth.password')"
            prefix-icon="Lock"
            size="large"
            show-password
          />
        </el-form-item>
        <el-form-item prop="confirmPassword">
          <el-input
            v-model="registerForm.confirmPassword"
            type="password"
            :placeholder="$t('auth.confirmPassword')"
            prefix-icon="Lock"
            size="large"
            show-password
            @keyup.enter="handleRegister"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            size="large"
            :loading="registerLoading"
            class="login-btn"
            @click="handleRegister"
          >
            {{ $t('auth.register') }}
          </el-button>
        </el-form-item>
        <div class="form-footer">
          <span class="toggle-text" @click="toggleMode">
            {{ $t('auth.hasAccount') }} <span class="toggle-link">{{ $t('auth.login') }}</span>
          </span>
        </div>
      </el-form>
    </div>

  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useUserStore } from '@/stores/user'
import { useThemeStore } from '@/stores/theme'
import { useLocaleStore } from '@/stores/locale'
import { useBrandingStore } from '@/stores/branding'
import { Sunny, Moon, Position, Loading } from '@element-plus/icons-vue'
import { register as apiRegister } from '@/api/auth'
import SetupWizard from '@/components/SetupWizard.vue'

const router = useRouter()
const { t } = useI18n()
const userStore = useUserStore()
const themeStore = useThemeStore()
const localeStore = useLocaleStore()
const brandingStore = useBrandingStore()

const isRegisterMode = ref(false)
const loginFormRef = ref()
const registerFormRef = ref()
const loginLoading = ref(false)
const registerLoading = ref(false)
const systemHasUsers = ref(null) // null=未检测, false=无用户(首次部署), true=已有用户
const systemChecking = ref(true) // 系统状态检测中，避免登录表单先闪现
const registerEnabled = ref(false) // 注册开关（后端 /system/status 返回，默认关闭）

const showSetupWizard = computed(() => systemHasUsers.value === false)

const loginForm = reactive({
  username: '',
  password: ''
})

const registerForm = reactive({
  username: '',
  password: '',
  confirmPassword: ''
})

// 检测系统是否已有用户（首次部署时显示引导式注册向导）
async function checkSystemStatus() {
  try {
    const res = await fetch('/api/v1/system/status', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}'
    })
    const data = await res.json()
    if (data.code === 0) {
      systemHasUsers.value = data.hasUsers
      registerEnabled.value = data.registerEnabled === true
    }
  } catch (e) {
    // 检测失败不影响正常登录流程
  } finally {
    systemChecking.value = false
  }
}

onMounted(() => {
  // 未登录时 main.js 不会预加载品牌配置，登录页自行拉取公开的 /branding/config/get
  brandingStore.load()
  checkSystemStatus()
})

// 向导完成后刷新系统状态，切回登录界面
function onWizardCompleted() {
  systemHasUsers.value = true
  // 首装完成后注册默认关闭，需管理员显式开启
  registerEnabled.value = false
}

const validateConfirmPassword = (rule, value, callback) => {
  if (!value) {
    callback(new Error(t('auth.pleaseConfirmPassword')))
  } else if (value !== registerForm.password) {
    callback(new Error(t('auth.passwordMismatch')))
  } else {
    callback()
  }
}

const loginRules = computed(() => ({
  username: [{ required: true, message: t('auth.pleaseEnterUsername'), trigger: 'blur' }],
  password: [{ required: true, message: t('auth.pleaseEnterPassword'), trigger: 'blur' }]
}))

const registerRules = computed(() => ({
  username: [{ required: true, message: t('auth.pleaseEnterUsername'), trigger: 'blur' }],
  password: [
    { required: true, message: t('auth.pleaseEnterPassword'), trigger: 'blur' },
    { min: 8, message: t('user.passwordMinLength'), trigger: 'blur' },
    { pattern: /[A-Z]/, message: t('user.passwordNeedUpper'), trigger: 'blur' },
    { pattern: /[a-z]/, message: t('user.passwordNeedLower'), trigger: 'blur' },
    { pattern: /[0-9]/, message: t('user.passwordNeedDigit'), trigger: 'blur' }
  ],
  confirmPassword: [{ validator: validateConfirmPassword, trigger: 'blur' }]
}))

function toggleMode() {
  // 注册关闭时禁止切入注册模式（入口已隐藏，此处兜底）
  if (!isRegisterMode.value && !registerEnabled.value) return
  isRegisterMode.value = !isRegisterMode.value
}

async function handleLogin() {
  try {
    await loginFormRef.value.validate()
  } catch (e) {
    return
  }
  loginLoading.value = true
  try {
    const res = await userStore.login(loginForm)
    if (res.code === 0) {
      ElMessage.success(t('auth.loginSuccess'))
      router.push('/dashboard')
    } else if (res.code === 10004) {
      ElMessage.warning(t('login.pendingApproval'))
    } else if (res.code === 10003) {
      ElMessage.error(t('login.accountDisabled'))
    } else {
      ElMessage.error(res.msg || t('auth.loginFailed'))
    }
  } catch (e) {
    // 网络异常已由全局响应拦截器统一提示，此处仅兜底防止未处理拒绝
  } finally {
    loginLoading.value = false
  }
}

async function handleRegister() {
  try {
    await registerFormRef.value.validate()
  } catch (e) {
    return
  }
  registerLoading.value = true
  try {
    const res = await apiRegister({
      username: registerForm.username,
      password: registerForm.password
    })
    if (res.code === 0) {
      ElMessage.success(t('auth.registerSuccess'))
      // 注册成功后使用 userStore.login 自动登录（保存 token + 加载资料）
      const loginRes = await userStore.login({
        username: registerForm.username,
        password: registerForm.password
      })
      if (loginRes.code === 0) {
        router.push('/dashboard')
      }
    } else {
      ElMessage.error(res.msg || t('auth.registerFailed'))
    }
  } catch (e) {
    // 网络异常已由全局响应拦截器统一提示，此处仅兜底防止未处理拒绝
  } finally {
    registerLoading.value = false
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

.login-loading {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: hsl(var(--background));
  color: hsl(var(--muted-foreground));
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
  max-width: 100%;
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

  .login-logo {
    width: 64px;
    height: 64px;
    object-fit: contain;
    margin-bottom: 12px;
    border-radius: 8px;
  }

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

.form-footer {
  text-align: center;
  margin-top: 8px;

  .toggle-text {
    font-size: 13px;
    color: hsl(var(--muted-foreground));
  }

  .toggle-link {
    color: hsl(var(--primary));
    cursor: pointer;
    font-weight: 500;

    &:hover {
      text-decoration: underline;
    }
  }
}
</style>
