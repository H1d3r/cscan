<template>
  <el-container class="layout-container">
    <!-- 侧边栏 -->
    <el-aside :width="asideWidth"
      :class="['aside', `style-${themeStore.themeStyle}`, { collapsed: isCollapse, 'mobile-open': mobileDrawerOpen }]">
      <div class="logo">
        <img :src="brandingStore.logoSrc" alt="logo" />
        <span v-show="!isCollapse">{{ brandingStore.displayTitle }}</span>
      </div>

      <div class="menu-wrapper">
        <div v-show="!isCollapse" class="menu-search">
          <el-input v-model="searchKeyword" :placeholder="t('common.menuSearch')" clearable size="small"
            :prefix-icon="Search" />
        </div>
        <el-menu ref="menuRef" :default-active="$route.path" :collapse="isCollapse"
          router :unique-opened="false">
          <template v-for="(group, gi) in filteredMenuData" :key="group.index || `g-${gi}`">
            <div v-if="group.type === 'divider'" v-show="!searchKeyword" class="menu-divider"></div>
            <el-menu-item v-else-if="group.type === 'item' && (!group.adminOnly || isAdmin)"
              v-show="!searchKeyword || matchLabel(group.label)" :index="group.index">
              <el-icon>
                <component :is="group.icon" />
              </el-icon>
              <template #title>{{ group.label }}</template>
            </el-menu-item>
            <el-sub-menu v-else-if="group.type === 'submenu'"
              v-show="!searchKeyword || subMenuHasMatch(group)" :index="group.index">
              <template #title>
                <el-icon>
                  <component :is="group.icon" />
                </el-icon>
                <span>{{ group.label }}</span>
              </template>
              <template v-for="item in group.items" :key="item.index">
                <el-menu-item v-if="!item.adminOnly || isAdmin"
                  v-show="!searchKeyword || matchLabel(group.label) || matchLabel(item.label)" :index="item.index">
                  <el-icon v-if="item.icon">
                    <component :is="item.icon" />
                  </el-icon>
                  <template #title>{{ item.label }}</template>
                </el-menu-item>
              </template>
            </el-sub-menu>
          </template>
        </el-menu>
        <div v-if="searchKeyword && !hasAnyResult" class="menu-no-results">
          <el-icon>
            <Search />
          </el-icon>
          <span>{{ t('common.noData') }}</span>
        </div>
      </div>

    </el-aside>

    <!-- 移动端抽屉遮罩 -->
    <div v-if="mobileDrawerOpen" class="sidebar-backdrop" @click="isCollapse = true"></div>

    <el-container>
      <!-- 顶部导航 -->
      <el-header :class="['header', `style-${themeStore.themeStyle}`]">
        <div class="header-left">
          <el-icon class="collapse-btn" @click="isCollapse = !isCollapse">
            <Fold v-if="!isCollapse" />
            <Expand v-else />
          </el-icon>
          <el-breadcrumb separator="/">
            <el-breadcrumb-item :to="{ path: '/' }">{{ $t('common.home') }}</el-breadcrumb-item>
            <el-breadcrumb-item>{{ $t($route.meta.title) }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <div class="header-right">
          <!-- 扫描引导 -->
          <el-tooltip :content="$t('onboarding.scanGuideBtn')" placement="bottom" popper-class="scan-guide-tooltip">
            <div class="scan-guide-btn" @click="showOnboarding = true">
              <el-icon>
                <Aim />
              </el-icon>
            </div>
          </el-tooltip>
          <!-- 语言切换 -->
          <LanguageSwitcher />
          <!-- 主题切换 -->
          <ThemeSwitcher />
          <el-dropdown @command="handleCommand">
            <span class="user-info">
              <el-avatar :size="32" :src="userStore.avatarSrc" />
              <span class="username">{{ userStore.username }}</span>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="profile">{{ $t('auth.personalCenter') }}</el-dropdown-item>
                <el-dropdown-item divided command="logout">{{ $t('auth.logout') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <!-- 主内容区 -->
      <el-main class="main">
        <router-view v-slot="{ Component }">
          <transition name="fade-transform" mode="out-in">
            <component :is="Component" :key="$route.path" />
          </transition>
        </router-view>
      </el-main>

      <!-- 扫描引导弹窗（首次登录自动弹出 + 顶栏按钮手动唤起）-->
      <OnboardingGuide v-if="showOnboarding" @finished="showOnboarding = false" />
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useUserStore } from '@/stores/user'
import { useThemeStore } from '@/stores/theme'
import { useBrandingStore } from '@/stores/branding'
import LanguageSwitcher from '@/components/LanguageSwitcher.vue'
import ThemeSwitcher from '@/components/ThemeSwitcher.vue'
import OnboardingGuide from '@/components/OnboardingGuide.vue'
import { getOnboardingStatus } from '@/api/auth'
import { shouldShowOnboarding } from '@/utils/onboarding'
import { buildMenu } from '@/config/menu'
import { Search, Fold, Expand, Aim } from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const userStore = useUserStore()
const themeStore = useThemeStore()
const brandingStore = useBrandingStore()
const isCollapse = ref(false)
const isMobile = ref(false)

// === 菜单搜索 ===
const searchKeyword = ref('')
const menuRef = ref()
const isAdmin = computed(() => userStore.isAdmin)

// 菜单数据结构（数据驱动渲染 + 搜索过滤），随语言切换重新翻译
const menu = computed(() => buildMenu(t))

// 根据用户角色的 menuPaths 过滤菜单（menuPaths 为空表示不受限）
const filteredMenuData = computed(() => {
  const permitted = userStore.menuPaths
  if (!permitted || permitted.length === 0) return menu.value

  return menu.value
    .map(group => group.type === 'submenu'
      ? { ...group, items: group.items.filter(item => permitted.includes(item.index)) }
      : group)
    .filter(group => {
      if (group.type === 'divider') return true
      if (group.type === 'item') return permitted.includes(group.index)
      return group.items.length > 0
    })
})

function matchLabel(label) {
  if (!searchKeyword.value) return true
  return label.toLowerCase().includes(searchKeyword.value.trim().toLowerCase())
}

function subMenuHasMatch(group) {
  if (matchLabel(group.label)) return true
  return group.items.some(item => (!item.adminOnly || isAdmin.value) && matchLabel(item.label))
}

const hasAnyResult = computed(() => filteredMenuData.value.some(group => {
  if (group.type === 'divider') return false
  if (group.adminOnly && !isAdmin.value) return false
  if (group.type === 'item') return matchLabel(group.label)
  if (group.type === 'submenu') return subMenuHasMatch(group)
  return false
}))

// 搜索时自动展开/收起子菜单；清空搜索仅保留当前路由所在子菜单展开
watch(searchKeyword, (val) => {
  if (!menuRef.value) return
  nextTick(() => {
    menu.value.forEach(group => {
      if (group.type !== 'submenu') return
      const keepOpen = val ? subMenuHasMatch(group) : group.items.some(item => item.index === route.path)
      keepOpen ? menuRef.value.open(group.index) : menuRef.value.close(group.index)
    })
  })
})

// 移动端断点：< 768px 视为移动端，侧边栏切换为抽屉模式
const MOBILE_BREAKPOINT = 768

// 抽屉模式：移动端侧边栏固定宽度 250px 浮层；桌面端按 isCollapse 取 64/250
const asideWidth = computed(() => isMobile.value ? '250px' : (isCollapse.value ? '64px' : '250px'))
// 移动端抽屉是否展开（桌面端恒为 false）
const mobileDrawerOpen = computed(() => isMobile.value && !isCollapse.value)

function checkMobile() {
  const mobile = window.innerWidth < MOBILE_BREAKPOINT
  isMobile.value = mobile
  // 进入移动端自动收起，避免 250px 撑出水平溢出
  if (mobile) isCollapse.value = true
}

// === 扫描引导：首次登录自动弹出，顶栏按钮可手动唤起 ===
const showOnboarding = ref(false)
async function checkOnboarding() {
  try {
    const res = await getOnboardingStatus()
    if (res && res.code === 0 && shouldShowOnboarding(res)) {
      showOnboarding.value = true
    }
  } catch (e) {
    // 引导检查失败不应阻塞主界面
  }
}

onMounted(() => {
  // 响应式：初始化移动端判定 + 监听视口变化
  checkMobile()
  window.addEventListener('resize', checkMobile)
  // 刷新当前登录用户信息（头像、邮箱等可能在其他会话中已变更）
  userStore.refreshProfile()
  // 首次登录自动弹出扫描引导
  checkOnboarding()
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})

// 移动端路由切换后自动收起抽屉，避免遮挡内容
watch(() => route.path, () => {
  if (isMobile.value && !isCollapse.value) isCollapse.value = true
})

function handleCommand(command) {
  if (command === 'logout') {
    userStore.logout()
    router.push('/login')
  } else if (command === 'profile') {
    router.push('/profile')
  }
}
</script>

<style lang="scss" scoped>
.layout-container {
  height: 100vh;
  display: flex;
}

.aside {
  background: hsl(var(--sidebar));
  color: hsl(var(--sidebar-foreground));
  transition: width 0.3s ease; // 只有宽度过渡，使用简单的ease
  overflow: hidden;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  border-right: 1px solid hsl(var(--sidebar-border));
  display: flex;
  flex-direction: column;
  flex-shrink: 0;

  .logo {
    min-height: 64px;
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    color: hsl(var(--sidebar-foreground));
    font-size: 16px;
    font-weight: 600;
    letter-spacing: 1px;
    border-bottom: 1px solid hsl(var(--sidebar-border));
    flex-shrink: 0;

    img {
      width: 36px;
      height: 36px;
      border-radius: 6px;
      background: transparent;
      flex-shrink: 0;
      object-fit: contain;
    }

    span {
      max-width: 100%;
      text-align: center;
      line-height: 1.25;
      word-break: break-word;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
  }

  .menu-wrapper {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    scroll-behavior: smooth;

    &::-webkit-scrollbar {
      width: 4px;
    }

    &::-webkit-scrollbar-thumb {
      background: hsl(var(--sidebar-border));
      border-radius: 2px;
    }
  }

  .menu-search {
    padding: 8px 12px 4px;
    flex-shrink: 0;

    :deep(.el-input) {
      .el-input__wrapper {
        background: hsl(var(--sidebar-accent) / 0.5);
        border-radius: 8px;
        box-shadow: none;
        border: 1px solid hsl(var(--sidebar-border));
        transition: border-color 0.25s ease, box-shadow 0.25s ease;

        &:hover {
          border-color: hsl(var(--sidebar-primary) / 0.3);
        }

        &.is-focus {
          border-color: hsl(var(--sidebar-primary) / 0.5);
          box-shadow: 0 0 0 2px hsl(var(--sidebar-primary) / 0.1);
        }
      }

      .el-input__inner {
        color: hsl(var(--sidebar-foreground));
        font-size: 13px;

        &::placeholder {
          color: hsl(var(--sidebar-foreground) / 0.4);
        }
      }

      .el-input__prefix,
      .el-input__suffix {
        color: hsl(var(--sidebar-foreground) / 0.4);
      }
    }
  }

  .menu-no-results {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 32px 0;
    color: hsl(var(--sidebar-foreground) / 0.35);
    font-size: 13px;

    .el-icon {
      font-size: 22px;
    }
  }

  .menu-divider {
    height: 1px;
    background: hsl(var(--sidebar-border));
    margin: 8px 16px;
  }

  .el-menu {
    border-right: none;
    background: transparent !important;

    .el-menu-item {
      margin: 2px 8px;
      border-radius: 8px;
      height: 40px;
      line-height: 40px;
      color: hsl(var(--sidebar-foreground));
      display: flex;
      align-items: center;
      padding: 0 12px !important;
      overflow: hidden;
      white-space: nowrap;
      position: relative;
      transition: background-color 0.2s ease, color 0.2s ease;

      .el-icon {
        font-size: 18px;
        width: 18px;
        height: 18px;
        display: flex;
        align-items: center;
        justify-content: center;
        flex-shrink: 0;
        margin-right: 12px; // 图标和文字之间的间距
      }

      span {
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        flex: 1;
      }

      &:hover {
        background: hsl(var(--sidebar-accent)) !important;
        color: hsl(var(--sidebar-accent-foreground)) !important;
      }

      &.is-active {
        background: hsl(var(--sidebar-primary) / 0.18) !important;
        color: hsl(var(--sidebar-primary)) !important;
        font-weight: 600;
        box-shadow: none;
      }
    }

    .el-sub-menu {
      .el-sub-menu__title {
        margin: 2px 8px;
        border-radius: 8px;
        height: 40px;
        line-height: 40px;
        color: hsl(var(--sidebar-foreground));
        display: flex;
        align-items: center;
        padding: 0 12px !important;
        overflow: hidden;
        white-space: nowrap;
        position: relative;
        transition: background-color 0.2s ease, color 0.2s ease;

        .el-icon {
          font-size: 18px;
          width: 18px;
          height: 18px;
          display: flex;
          align-items: center;
          justify-content: center;
          flex-shrink: 0;
          margin-right: 12px; // 图标和文字之间的间距
        }

        span {
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
          flex: 1;
        }

        &:hover {
          background: hsl(var(--sidebar-accent)) !important;
          color: hsl(var(--sidebar-accent-foreground)) !important;
        }
      }

      &.is-opened>.el-sub-menu__title {
        color: hsl(var(--sidebar-foreground));
      }

      .el-menu {
        background: transparent !important;

        .el-menu-item {
          padding-left: 44px !important;
          min-width: auto;
          height: 36px;
          line-height: 36px;
          font-size: 13px;

          .el-icon {
            margin-right: 8px;
          }
        }
      }
    }

    // 收起状态：让Element Plus处理，只调整必要的样式
    &.el-menu--collapse {
      .el-menu-item {
        margin: 2px 0;
        justify-content: center;
        padding: 0 !important;
      }

      .el-sub-menu {
        .el-sub-menu__title {
          margin: 2px 0;
          justify-content: center;
          padding: 0 !important;
        }
      }
    }
  }

}

// 深度选择器：样式覆盖 + 平滑过渡
:deep(.el-menu) {

  .el-menu-item,
  .el-sub-menu .el-sub-menu__title {

    .el-icon {
      display: flex !important;
      visibility: visible !important;
      opacity: 1 !important;
    }

    span {
      display: block !important;
      visibility: visible !important;
      opacity: 1 !important;
    }
  }

  // 子菜单展开/收起：平滑高度过渡
  .el-sub-menu .el-menu {
    transition: height 0.3s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.25s ease;
    overflow: hidden;
  }

  // 箭头旋转：平滑过渡
  .el-sub-menu__title .el-sub-menu__icon-arrow {
    transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  }

  // 折叠态弹出菜单：平滑出现
  .el-popper.el-menu--vertical {
    transition: opacity 0.2s ease, transform 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  }
}

.header {
  background: hsl(var(--background));
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  height: 64px;
  border-bottom: 1px solid hsl(var(--border));
  transition: background 0.3s;

  .header-left {
    display: flex;
    align-items: center;

    .collapse-btn {
      font-size: 20px;
      cursor: pointer;
      margin-right: 20px;
      color: hsl(var(--muted-foreground));
      transition: color 0.3s;

      &:hover {
        color: hsl(var(--primary));
      }
    }
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 16px;

    .theme-switch {
      width: 36px;
      height: 36px;
      display: flex;
      align-items: center;
      justify-content: center;
      border-radius: 8px;
      cursor: pointer;
      color: hsl(var(--muted-foreground));
      transition: all 0.3s;

      &:hover {
        background: hsl(var(--accent));
        color: hsl(var(--primary));
      }

      .el-icon {
        font-size: 18px;
      }
    }

    .scan-guide-btn {
      width: 36px;
      height: 36px;
      display: flex;
      align-items: center;
      justify-content: center;
      border-radius: 8px;
      cursor: pointer;
      color: hsl(var(--muted-foreground));
      background: transparent;
      border: 1px solid transparent;
      transition: all 0.3s;

      &:hover {
        background: hsl(var(--accent));
        color: hsl(var(--primary));
      }

      .el-icon {
        font-size: 18px;
      }
    }

    .user-info {
      display: flex;
      align-items: center;
      cursor: pointer;
      padding: 4px 8px;
      border-radius: 8px;
      transition: background 0.3s;

      &:hover {
        background: hsl(var(--accent));
      }

      .username {
        margin-left: 8px;
        color: hsl(var(--foreground));
      }
    }
  }
}

.main {
  background: hsl(var(--background));
  padding: 20px;
  overflow-y: auto;
  overflow-x: hidden;
  transition: background 0.3s;
  flex: 1;
  width: 100%;
  margin: 0 auto;

  /* 隐藏滚动条 */
  &::-webkit-scrollbar {
    display: none;
  }

  -ms-overflow-style: none;
  scrollbar-width: none;
}

/* 移动端抽屉遮罩 */
.sidebar-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  z-index: 1000;
}

/* 移动端响应式：< 768px 侧边栏改为浮层抽屉，避免水平溢出 */
@media (max-width: 767px) {
  .aside {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    z-index: 1001;
    width: 250px !important;
    transform: translateX(-100%);
    transition: transform 0.3s ease;

    &.mobile-open {
      transform: translateX(0);
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.28);
    }
  }

  .header {
    padding: 0 12px;

    .header-left .collapse-btn {
      margin-right: 10px;
    }

    .header-right {
      gap: 8px;

      .user-info .username {
        display: none;
      }
    }
  }

  .main {
    padding: 12px;
  }
}

/* fade-transform 动画 */
.fade-transform-leave-active,
.fade-transform-enter-active {
  transition: all 0.1s ease-out;
}

.fade-transform-enter-from {
  opacity: 0;
  transform: translateX(-10px);
}

.fade-transform-leave-to {
  opacity: 0;
  transform: translateX(10px);
}
</style>
