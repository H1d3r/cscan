<template>
  <el-container class="layout-container">
    <!-- 侧边栏 -->
    <el-aside :width="isCollapse ? '64px' : '250px'" :class="['aside', { collapsed: isCollapse }]">
      <div class="logo">
        <img src="/logo.png" alt="logo" />
        <span v-show="!isCollapse">CSCAN</span>
      </div>

      <div class="menu-wrapper">
        <el-menu :default-active="$route.path" :default-openeds="defaultOpeneds" :collapse="isCollapse" router
          :unique-opened="false">
          <!-- 主控台分组 -->
          <el-menu-item index="/dashboard">
            <el-icon>
              <Odometer />
            </el-icon>
            <template #title>{{ $t('navigation.dashboard') }}</template>
          </el-menu-item>
          <!-- 资产管理分组 -->
          <el-sub-menu index="asset-group">
            <template #title>
              <el-icon><Monitor /></el-icon>
              <span>{{ $t('navigation.assetManagement') }}</span>
            </template>
            <el-menu-item index="/asset-management">
              <template #title>{{ $t('navigation.assetManagement') }}</template>
            </el-menu-item>
            <el-menu-item index="/asset/directory">
              <template #title>{{ $t('asset.dirManagement') }}</template>
            </el-menu-item>
            <el-menu-item index="/asset/vulnerability">
              <template #title>{{ $t('asset.vulManagement') }}</template>
            </el-menu-item>
          </el-sub-menu>

          <!-- 分割线 -->
          <div class="menu-divider"></div>

          <!-- 扫描分组 -->
            <el-menu-item index="/task">
              <el-icon>
                <List />
              </el-icon>
              <template #title>{{ $t('navigation.taskManagement') }}</template>
            </el-menu-item>
            <el-menu-item index="/cron-task">
              <el-icon>
                <Timer />
              </el-icon>
              <template #title>{{ $t('navigation.cronTask') }}</template>
            </el-menu-item>
            <el-menu-item index="/poc">
              <el-icon>
                <Aim />
              </el-icon>
              <template #title>{{ $t('navigation.pocManagement') }}</template>
            </el-menu-item>
            <el-menu-item index="/fingerprint">
              <el-icon>
                <Stamp />
              </el-icon>
              <template #title>{{ $t('navigation.fingerprintManagement') }}</template>
            </el-menu-item>
                        <el-menu-item index="/blacklist">
              <el-icon>
                <CircleClose />
              </el-icon>
              <template #title>{{ $t('navigation.blacklist') }}</template>
            </el-menu-item>
            <el-menu-item index="/settings?tab=subfinder">
              <el-icon>
                <Search />
              </el-icon>
              <template #title>{{ $t('navigation.subdomainConfig') }}</template>
          </el-menu-item>

          <!-- 分割线 -->
          <div class="menu-divider"></div>

          <!-- 工具分组 -->
          <el-menu-item index="/online-search">
            <el-icon>
              <Search />
            </el-icon>
            <template #title>{{ $t('navigation.onlineSearch') }}</template>

          </el-menu-item>
            <el-menu-item index="/settings?tab=onlineapi">
              <el-icon>
                <Key />
              </el-icon>
              <template #title>{{ $t('navigation.onlineApiConfig') }}</template>
            </el-menu-item>

          <!-- 分割线 -->
          <div class="menu-divider"></div>

          <!-- 系统管理分组 -->
            <el-menu-item index="/worker">
              <el-icon>
                <Connection />
              </el-icon>
              <template #title>{{ $t('navigation.workerNodes') }}</template>
            </el-menu-item>
            <el-menu-item index="/worker-logs">
              <el-icon>
                <Document />
              </el-icon>
              <template #title>{{ $t('navigation.workerLogs') }}</template>
            </el-menu-item>
            <el-menu-item index="/settings?tab=notify">
              <el-icon>
                <Bell />
              </el-icon>
              <template #title>{{ $t('navigation.notifyConfig') }}</template>
            </el-menu-item>
            <el-menu-item index="/high-risk-filter">
              <el-icon>
                <Warning />
              </el-icon>
              <template #title>{{ $t('navigation.highRiskFilter') }}</template>
            </el-menu-item>
            <el-menu-item index="/settings?tab=workspace">
              <el-icon>
                <Folder />
              </el-icon>
              <template #title>{{ $t('navigation.workspaceManagement') }}</template>
            </el-menu-item>
            <el-menu-item index="/settings?tab=organization">
              <el-icon>
                <OfficeBuilding />
              </el-icon>
              <template #title>{{ $t('navigation.organizationManagement') }}</template>
            </el-menu-item>
            <el-menu-item index="/settings?tab=user">
              <el-icon>
                <User />
              </el-icon>
              <template #title>{{ $t('navigation.userManagement') }}</template>
            </el-menu-item>

        </el-menu>
      </div>

    </el-aside>

    <el-container>
      <!-- 顶部导航 -->
      <el-header class="header">
        <div class="header-left">
          <el-icon class="collapse-btn" @click="isCollapse = !isCollapse">
            <Fold v-if="!isCollapse" />
            <Expand v-else />
          </el-icon>
          <!-- 工作空间选择 -->
          <el-select v-model="workspaceStore.currentWorkspaceId" :placeholder="$t('common.allWorkspaces')"
            style="width: 160px; margin-right: 16px;" @change="handleWorkspaceChange">
            <el-option :label="$t('common.allWorkspaces')" value="all" />
            <el-option v-for="ws in workspaceStore.workspaces" :key="ws.id" :label="ws.name" :value="ws.id" />
          </el-select>
          <el-breadcrumb separator="/">
            <el-breadcrumb-item :to="{ path: '/' }">{{ $t('common.home') }}</el-breadcrumb-item>
            <el-breadcrumb-item>{{ $route.meta.title }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <div class="header-right">
          <!-- 语言切换 -->
          <LanguageSwitcher />
          <!-- 主题切换 -->
          <ThemeSwitcher />
          <el-dropdown @command="handleCommand">
            <span class="user-info">
              <el-avatar :size="32" icon="User" />
              <span class="username">{{ userStore.username }}</span>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="logout">{{ $t('auth.logout') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <!-- 主内容区 -->
      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useThemeStore } from '@/stores/theme'
import { useWorkspaceStore } from '@/stores/workspace'
import LanguageSwitcher from '@/components/LanguageSwitcher.vue'
import ThemeSwitcher from '@/components/ThemeSwitcher.vue'
import { Setting, Monitor, List, Search, Aim, Odometer, Stamp, Connection, Fold, Expand, Key, Folder, OfficeBuilding, Bell, User, Document, CircleClose, Warning, Timer } from '@element-plus/icons-vue'

const router = useRouter()
const userStore = useUserStore()
const themeStore = useThemeStore()
const workspaceStore = useWorkspaceStore()
const isCollapse = ref(false)
const defaultOpeneds = ref(['scan-group', 'system-group'])

onMounted(() => {
  workspaceStore.loadWorkspaces()
})

function handleWorkspaceChange(val) {
  workspaceStore.setCurrentWorkspace(val)
  // 触发页面刷新数据
  window.dispatchEvent(new CustomEvent('workspace-changed', { detail: val }))
}

function handleCommand(command) {
  if (command === 'logout') {
    userStore.logout()
    router.push('/login')
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
    height: 64px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: hsl(var(--sidebar-foreground));
    font-size: 18px;
    font-weight: 600;
    letter-spacing: 2px;
    border-bottom: 1px solid hsl(var(--sidebar-border));
    flex-shrink: 0;

    img {
      width: 36px;
      height: 36px;
      margin-right: 10px;
      border-radius: 6px;
      background: transparent;
      flex-shrink: 0;
    }

    span {
      white-space: nowrap;
      overflow: hidden;
      // 文字随容器宽度自然显示/隐藏，无动画
    }
  }

  .menu-wrapper {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;

    &::-webkit-scrollbar {
      width: 4px;
    }

    &::-webkit-scrollbar-thumb {
      background: hsl(var(--sidebar-border));
      border-radius: 2px;
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
      padding: 0 12px !important; // 使用padding而不是复杂的定位
      overflow: hidden;
      white-space: nowrap;
      position: relative;

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
        background: var(--el-color-primary) !important;
        color: var(--el-color-primary-contrast, #fff) !important;
        box-shadow: 0 2px 8px var(--el-color-primary-light-5);
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
        padding: 0 12px !important; // 使用padding而不是复杂的定位
        overflow: hidden;
        white-space: nowrap;
        position: relative;

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
          padding-left: 50px !important;
          min-width: auto;
          height: 36px;
          line-height: 36px;
          font-size: 13px;
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

// 简化的深度选择器，只处理必要的样式覆盖
:deep(.el-menu) {
  .el-menu-item, .el-sub-menu .el-sub-menu__title {
    // 重置所有可能的隐藏样式
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
  max-width: 1500px;
  width: 1500px;
  margin: 0 auto;
  
  /* 隐藏滚动条 */
  &::-webkit-scrollbar {
    display: none;
  }
  -ms-overflow-style: none;
  scrollbar-width: none;
}

// 收起状态下logo图标居中
.aside.collapsed {
  .logo {
    img {
      margin-right: 0; // 收起时图标居中
    }
  }
}
</style>
