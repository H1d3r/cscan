<template>
  <div class="settings-page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>系统配置</span>
        </div>
      </template>
      
      <el-tabs v-model="activeTab" tab-position="left" class="settings-tabs" @tab-change="handleSettingsTabChange">
        <!-- 在线API配置 -->
        <el-tab-pane label="在线API配置" name="onlineapi">
          <div class="tab-content">
            <el-alert type="info" :closable="false" style="margin-bottom: 20px">
              <template #title>配置在线搜索API密钥，用于Fofa、Hunter、Quake等平台的资产搜索</template>
            </el-alert>
            
            <el-tabs v-model="apiConfigTab" type="card">
              <el-tab-pane label="Fofa" name="fofa">
                <el-form label-width="100px" style="max-width: 500px; margin-top: 20px">
                  <el-form-item label="API版本">
                    <el-radio-group v-model="apiConfigs.fofa.version">
                      <el-radio value="v1">v1 (fofa.info)</el-radio>
                      <el-radio value="v5">v5 (v5.fofa.info)</el-radio>
                    </el-radio-group>
                  </el-form-item>
                  <el-form-item label="API Key">
                    <el-input v-model="apiConfigs.fofa.key" placeholder="Fofa API Key" show-password />
                  </el-form-item>
                  <el-form-item>
                    <el-button type="primary" @click="saveApiConfig('fofa')">保存</el-button>
                    <el-button type="success" @click="openApiUrl(apiConfigs.fofa.version === 'v5' ? 'https://v5.fofa.info/userInfo' : 'https://fofa.info/userInfo')">申请API</el-button>
                  </el-form-item>
                </el-form>
              </el-tab-pane>
              <el-tab-pane label="Hunter" name="hunter">
                <el-form label-width="100px" style="max-width: 500px; margin-top: 20px">
                  <el-form-item label="API Key">
                    <el-input v-model="apiConfigs.hunter.key" placeholder="Hunter API Key" show-password />
                  </el-form-item>
                  <el-form-item>
                    <el-button type="primary" @click="saveApiConfig('hunter')">保存</el-button>
                    <el-button type="success" @click="openApiUrl('https://hunter.qianxin.com/home/myInfo')">申请API</el-button>
                  </el-form-item>
                </el-form>
              </el-tab-pane>
              <el-tab-pane label="Quake" name="quake">
                <el-form label-width="100px" style="max-width: 500px; margin-top: 20px">
                  <el-form-item label="API Key">
                    <el-input v-model="apiConfigs.quake.key" placeholder="Quake API Key" show-password />
                  </el-form-item>
                  <el-form-item>
                    <el-button type="primary" @click="saveApiConfig('quake')">保存</el-button>
                    <el-button type="success" @click="openApiUrl('https://quake.360.net/quake/#/personal?tab=message')">申请API</el-button>
                  </el-form-item>
                </el-form>
              </el-tab-pane>
            </el-tabs>
          </div>
        </el-tab-pane>

        <!-- Subfinder数据源配置 -->
        <el-tab-pane label="子域名扫描配置" name="subfinder">
          <div class="tab-content">
            <el-alert type="info" :closable="false" style="margin-bottom: 20px">
              <template #title>Subfinder用于子域名枚举，配置API密钥可以获取更多数据源的结果。点击数据源名称可跳转到官网获取API密钥。</template>
            </el-alert>
            
            <el-table :data="subfinderProviders" v-loading="subfinderLoading" max-height="500" stripe>
              <el-table-column prop="name" label="数据源" width="130">
                <template #default="{ row }">
                  <span>{{ row.name }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="description" label="描述" width="180" show-overflow-tooltip />
              <el-table-column prop="keyFormat" label="密钥格式" width="140" />
              <el-table-column label="API密钥" min-width="200">
                <template #default="{ row }">
                  <el-input
                    v-model="row.keyInput"
                    :placeholder="row.maskedKey || row.keyFormat"
                    size="small"
                    clearable
                  />
                </template>
              </el-table-column>
              <el-table-column label="状态" width="70">
                <template #default="{ row }">
                  <el-switch v-model="row.enabled" size="small" />
                </template>
              </el-table-column>
              <el-table-column label="操作" width="140">
                <template #default="{ row }">
                  <el-button type="primary" link size="small" @click="saveSubfinderProvider(row)">保存</el-button>
                  <el-button type="success" link size="small" @click="openApiUrl(row.url)">申请API</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <!-- 工作空间 -->
        <el-tab-pane label="工作空间" name="workspace">
          <div class="tab-content">
            <div class="tab-action-bar">
              <el-button type="primary" @click="showWorkspaceDialog()">
                <el-icon><Plus /></el-icon>新建工作空间
              </el-button>
            </div>
            <el-table :data="workspaceList" v-loading="workspaceLoading" stripe max-height="500">
              <el-table-column prop="name" label="名称" min-width="150" />
              <el-table-column prop="description" label="描述" min-width="250" />
              <el-table-column prop="status" label="状态" width="100">
                <template #default="{ row }">
                  <el-tag :type="row.status === 'enable' ? 'success' : 'danger'">
                    {{ row.status === 'enable' ? '启用' : '禁用' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="createTime" label="创建时间" width="160" />
              <el-table-column label="操作" width="150" fixed="right">
                <template #default="{ row }">
                  <el-button type="primary" link size="small" @click="showWorkspaceDialog(row)">编辑</el-button>
                  <el-button type="danger" link size="small" @click="handleDeleteWorkspace(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <!-- 组织管理 -->
        <el-tab-pane label="组织管理" name="organization">
          <div class="tab-content">
            <div class="tab-action-bar">
              <el-button type="primary" @click="showOrgDialog()">
                <el-icon><Plus /></el-icon>新建组织
              </el-button>
            </div>
            <el-table :data="orgList" v-loading="orgLoading" stripe max-height="500">
              <el-table-column prop="name" label="组织名称" min-width="150" />
              <el-table-column prop="description" label="描述" min-width="250" />
              <el-table-column prop="status" label="状态" width="100">
                <template #default="{ row }">
                  <el-switch
                    v-model="row.status"
                    active-value="enable"
                    inactive-value="disable"
                    @change="handleOrgStatusChange(row)"
                  />
                </template>
              </el-table-column>
              <el-table-column prop="createTime" label="创建时间" width="160" />
              <el-table-column label="操作" width="150" fixed="right">
                <template #default="{ row }">
                  <el-button type="primary" link size="small" @click="showOrgDialog(row)">编辑</el-button>
                  <el-button type="danger" link size="small" @click="handleDeleteOrg(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <!-- 通知配置 -->
        <el-tab-pane label="通知配置" name="notify">
          <div class="tab-content">
            <el-alert type="info" :closable="false" style="margin-bottom: 20px">
              <template #title>配置任务完成通知，支持邮件、飞书、钉钉、企微、Slack、Discord、Telegram、Teams、Gotify、Webhook等渠道。可配置高危过滤，仅在检测到高危项时通知。</template>
            </el-alert>
            
            <div class="tab-action-bar">
              <el-button type="primary" @click="showNotifyDialog()">
                <el-icon><Plus /></el-icon>添加通知渠道
              </el-button>
            </div>
            
            <el-table :data="notifyConfigList" v-loading="notifyLoading" stripe max-height="500">
              <el-table-column prop="name" label="名称" min-width="120" />
              <el-table-column label="渠道" width="140">
                <template #default="{ row }">
                  <el-tag>{{ getProviderName(row.provider) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="高危过滤" width="100">
                <template #default="{ row }">
                  <el-tag v-if="row.highRiskFilter && row.highRiskFilter.enabled" type="warning" size="small">已启用</el-tag>
                  <el-tag v-else type="info" size="small">全部通知</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="status" label="状态" width="100">
                <template #default="{ row }">
                  <el-switch
                    v-model="row.status"
                    active-value="enable"
                    inactive-value="disable"
                    @change="handleNotifyStatusChange(row)"
                  />
                </template>
              </el-table-column>
              <el-table-column prop="updateTime" label="更新时间" width="160" />
              <el-table-column label="操作" width="200" fixed="right">
                <template #default="{ row }">
                  <el-button type="primary" link size="small" @click="showNotifyDialog(row)">编辑</el-button>
                  <el-button type="success" link size="small" @click="handleTestNotify(row)" :loading="row.testing">测试</el-button>
                  <el-button type="danger" link size="small" @click="handleDeleteNotify(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>


        <!-- 用户管理 -->
        <el-tab-pane label="用户管理" name="user">
          <div class="tab-content">
            <div class="tab-action-bar">
              <el-button type="primary" @click="showUserDialog()">
                <el-icon><Plus /></el-icon>新建用户
              </el-button>
            </div>
            <el-table :data="userList" v-loading="userLoading" stripe max-height="500">
              <el-table-column prop="username" label="用户名" min-width="150" />
              <el-table-column prop="status" label="状态" width="100">
                <template #default="{ row }">
                  <el-tag :type="row.status === 'enable' ? 'success' : 'danger'">
                    {{ row.status === 'enable' ? '启用' : '禁用' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="200" fixed="right">
                <template #default="{ row }">
                  <el-button type="primary" link size="small" @click="showUserDialog(row)">编辑</el-button>
                  <el-button type="warning" link size="small" @click="showResetPasswordDialog(row)">重置密码</el-button>
                  <el-button type="danger" link size="small" @click="handleDeleteUser(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 工作空间对话框 -->
    <el-dialog v-model="workspaceDialogVisible" :title="workspaceForm.id ? '编辑工作空间' : '新建工作空间'" width="500px">
      <el-form ref="workspaceFormRef" :model="workspaceForm" :rules="workspaceRules" label-width="80px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="workspaceForm.name" placeholder="请输入名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="workspaceForm.description" type="textarea" :rows="3" placeholder="请输入描述" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="workspaceDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="workspaceSubmitting" @click="handleWorkspaceSubmit">确定</el-button>
      </template>
    </el-dialog>

    <!-- 用户对话框 -->
    <el-dialog v-model="userDialogVisible" :title="userForm.id ? '编辑用户' : '新建用户'" width="500px">
      <el-form ref="userFormRef" :model="userForm" :rules="userRules" label-width="80px">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="userForm.username" placeholder="请输入用户名" />
        </el-form-item>
        <el-form-item v-if="!userForm.id" label="密码" prop="password">
          <el-input v-model="userForm.password" type="password" placeholder="请输入密码" />
        </el-form-item>
        <el-form-item label="状态" prop="status">
          <el-select v-model="userForm.status" placeholder="请选择状态">
            <el-option label="启用" value="enable" />
            <el-option label="禁用" value="disable" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="userSubmitting" @click="handleUserSubmit">确定</el-button>
      </template>
    </el-dialog>

    <!-- 重置密码对话框 -->
    <el-dialog v-model="resetPasswordVisible" title="重置密码" width="400px">
      <el-form ref="resetFormRef" :model="resetForm" :rules="resetRules" label-width="80px">
        <el-form-item label="新密码" prop="newPassword">
          <el-input v-model="resetForm.newPassword" type="password" placeholder="请输入新密码" />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input v-model="resetForm.confirmPassword" type="password" placeholder="请再次输入新密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="resetPasswordVisible = false">取消</el-button>
        <el-button type="primary" :loading="resetting" @click="handleResetPassword">确定</el-button>
      </template>
    </el-dialog>

    <!-- 组织对话框 -->
    <el-dialog v-model="orgDialogVisible" :title="orgForm.id ? '编辑组织' : '新建组织'" width="500px">
      <el-form ref="orgFormRef" :model="orgForm" :rules="orgRules" label-width="80px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="orgForm.name" placeholder="请输入组织名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="orgForm.description" type="textarea" :rows="3" placeholder="请输入描述" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="orgDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="orgSubmitting" @click="handleOrgSubmit">确定</el-button>
      </template>
    </el-dialog>

    <!-- 通知配置对话框 -->
    <el-dialog v-model="notifyDialogVisible" :title="notifyForm.id ? '编辑通知配置' : '添加通知渠道'" width="700px">
      <el-form ref="notifyFormRef" :model="notifyForm" :rules="notifyRules" label-width="120px">
        <el-form-item label="渠道类型" prop="provider">
          <el-select v-model="notifyForm.provider" placeholder="请选择通知渠道" @change="handleProviderChange" :disabled="!!notifyForm.id">
            <el-option v-for="p in notifyProviders" :key="p.id" :label="p.name" :value="p.id">
              <span>{{ p.name }}</span>
              <span style="color: #999; font-size: 12px; margin-left: 8px">{{ p.description }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="配置名称" prop="name">
          <el-input v-model="notifyForm.name" placeholder="请输入配置名称" />
        </el-form-item>
        
        <!-- 动态配置字段 -->
        <template v-if="currentProviderFields.length > 0">
          <el-divider content-position="left">渠道配置</el-divider>
          <el-form-item 
            v-for="field in currentProviderFields" 
            :key="field.name" 
            :label="field.label"
            :prop="'configData.' + field.name"
            :rules="field.required ? [{ required: true, message: '请输入' + field.label, trigger: 'blur' }] : []"
          >
            <el-input 
              v-if="field.type === 'text'" 
              v-model="notifyForm.configData[field.name]" 
              :placeholder="field.placeholder" 
            />
            <el-input 
              v-else-if="field.type === 'password'" 
              v-model="notifyForm.configData[field.name]" 
              :placeholder="field.placeholder" 
              show-password 
            />
            <el-input 
              v-else-if="field.type === 'textarea'" 
              v-model="notifyForm.configData[field.name]" 
              type="textarea" 
              :rows="3" 
              :placeholder="field.placeholder" 
            />
            <el-input-number 
              v-else-if="field.type === 'number'" 
              v-model="notifyForm.configData[field.name]" 
              :placeholder="field.placeholder" 
              controls-position="right"
            />
            <el-switch 
              v-else-if="field.type === 'switch'" 
              v-model="notifyForm.configData[field.name]" 
            />
            <el-select 
              v-else-if="field.type === 'select'" 
              v-model="notifyForm.configData[field.name]" 
              :placeholder="field.placeholder || '请选择'"
              clearable
            >
              <el-option v-for="opt in field.options" :key="opt" :label="opt || '默认'" :value="opt" />
            </el-select>
          </el-form-item>
        </template>
        
        <!-- 高危过滤配置 -->
        <el-divider content-position="left">高危过滤配置</el-divider>
        <el-form-item label="启用高危过滤">
          <el-switch v-model="notifyForm.highRiskFilter.enabled" />
          <span style="color: #909399; font-size: 12px; margin-left: 10px">
            关闭时全部通知，开启后仅在检测到配置的高危项时通知
          </span>
        </el-form-item>
        
        <template v-if="notifyForm.highRiskFilter.enabled">
          <el-form-item label="高危指纹">
            <el-select 
              v-model="notifyForm.highRiskFilter.highRiskFingerprints" 
              multiple 
              filterable 
              allow-create
              default-first-option
              placeholder="选择或输入高危指纹名称"
              style="width: 100%"
            >
              <el-option v-for="fp in commonFingerprints" :key="fp" :label="fp" :value="fp" />
            </el-select>
            <div style="color: #909399; font-size: 12px; margin-top: 4px">
              常见高危指纹: Weblogic, Struts2, Shiro, Fastjson, Log4j, Spring等
            </div>
          </el-form-item>
          
          <el-form-item label="高危端口">
            <el-select 
              v-model="notifyForm.highRiskFilter.highRiskPorts" 
              multiple 
              filterable 
              allow-create
              default-first-option
              placeholder="选择或输入高危端口"
              style="width: 100%"
              :reserve-keyword="false"
            >
              <el-option v-for="port in commonHighRiskPorts" :key="port.value" :label="port.label" :value="port.value" />
            </el-select>
            <div style="color: #909399; font-size: 12px; margin-top: 4px">
              常见高危端口: 22(SSH), 3389(RDP), 6379(Redis), 27017(MongoDB)等
            </div>
          </el-form-item>
          
          <el-form-item label="高危POC级别">
            <el-checkbox-group v-model="notifyForm.highRiskFilter.highRiskPocSeverities">
              <el-checkbox label="critical">严重 (Critical)</el-checkbox>
              <el-checkbox label="high">高危 (High)</el-checkbox>
              <el-checkbox label="medium">中危 (Medium)</el-checkbox>
              <el-checkbox label="low">低危 (Low)</el-checkbox>
            </el-checkbox-group>
            <div style="color: #909399; font-size: 12px; margin-top: 4px">
              选择需要通知的漏洞严重级别
            </div>
          </el-form-item>
        </template>
        
        <el-divider content-position="left">消息模板（可选）</el-divider>
        <el-form-item label="自定义模板">
          <el-input 
            v-model="notifyForm.messageTemplate" 
            type="textarea" 
            :rows="4" 
            placeholder="留空使用默认模板。支持变量: {{.TaskName}}, {{.Status}}, {{.AssetCount}}, {{.VulCount}}, {{.Duration}}" 
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="notifyDialogVisible = false">取消</el-button>
        <el-button type="success" @click="handleTestNotifyForm" :loading="notifyTesting">测试</el-button>
        <el-button type="primary" :loading="notifySubmitting" @click="handleNotifySubmit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>


<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import request from '@/api/request'
import { getSubfinderProviderList, getSubfinderProviderInfo, saveSubfinderProvider as saveSubfinderProviderApi } from '@/api/subfinder'
import { getUserList, createUser, updateUser, deleteUser, resetUserPassword } from '@/api/auth'
import { getNotifyProviders, getNotifyConfigList, saveNotifyConfig, deleteNotifyConfig, testNotifyConfig } from '@/api/notify'

const route = useRoute()
const router = useRouter()

// 有效的tab名称
const validTabs = ['onlineapi', 'subfinder', 'workspace', 'organization', 'notify', 'user']

// 从URL获取初始tab
const getInitialTab = () => {
  const tab = route.query.tab
  return validTabs.includes(tab) ? tab : 'onlineapi'
}

const activeTab = ref(getInitialTab())

// Tab切换时更新URL
function handleSettingsTabChange(tabName) {
  router.replace({ query: { ...route.query, tab: tabName } })
}

// 监听路由变化，更新activeTab
watch(() => route.query.tab, (newTab) => {
  if (validTabs.includes(newTab) && newTab !== activeTab.value) {
    activeTab.value = newTab
  }
})
const apiConfigTab = ref('fofa')
const subfinderLoading = ref(false)
const subfinderProviders = ref([])

const apiConfigs = reactive({
  fofa: { key: '', secret: '', version: 'v1' },
  hunter: { key: '', secret: '' },
  quake: { key: '', secret: '' }
})

// 工作空间相关
const workspaceLoading = ref(false)
const workspaceList = ref([])
const workspaceDialogVisible = ref(false)
const workspaceSubmitting = ref(false)
const workspaceFormRef = ref()
const workspaceForm = reactive({ id: '', name: '', description: '' })
const workspaceRules = { name: [{ required: true, message: '请输入名称', trigger: 'blur' }] }

// 用户管理相关
const userLoading = ref(false)
const userList = ref([])
const userDialogVisible = ref(false)
const userSubmitting = ref(false)
const userFormRef = ref()
const userForm = ref({ id: '', username: '', password: '', status: 'enable' })
const userRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
  status: [{ required: true, message: '请选择状态', trigger: 'change' }]
}

// 重置密码相关
const resetPasswordVisible = ref(false)
const resetting = ref(false)
const resetFormRef = ref()
const resetForm = ref({ id: '', newPassword: '', confirmPassword: '' })
const resetRules = {
  newPassword: [{ required: true, message: '请输入新密码', trigger: 'blur' }],
  confirmPassword: [
    { required: true, message: '请再次输入新密码', trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        if (value !== resetForm.value.newPassword) {
          callback(new Error('两次输入的密码不一致'))
        } else {
          callback()
        }
      },
      trigger: 'blur'
    }
  ]
}

// 组织管理相关
const orgLoading = ref(false)
const orgList = ref([])
const orgDialogVisible = ref(false)
const orgSubmitting = ref(false)
const orgFormRef = ref()
const orgForm = reactive({ id: '', name: '', description: '' })
const orgRules = { name: [{ required: true, message: '请输入组织名称', trigger: 'blur' }] }

// 通知配置相关
const notifyLoading = ref(false)
const notifyConfigList = ref([])
const notifyProviders = ref([])
const notifyDialogVisible = ref(false)
const notifySubmitting = ref(false)
const notifyTesting = ref(false)
const notifyFormRef = ref()
const notifyForm = ref({ 
  id: '', 
  name: '', 
  provider: '', 
  configData: {}, 
  messageTemplate: '', 
  status: 'enable',
  highRiskFilter: {
    enabled: false,
    highRiskFingerprints: [],
    highRiskPorts: [],
    highRiskPocSeverities: []
  }
})
const notifyRules = {
  provider: [{ required: true, message: '请选择通知渠道', trigger: 'change' }],
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }]
}
const currentProviderFields = ref([])

// 常见高危指纹列表
const commonFingerprints = ref([
  'Weblogic', 'Struts2', 'Shiro', 'Fastjson', 'Log4j', 'Spring', 'SpringBoot',
  'Tomcat', 'JBoss', 'Jenkins', 'Elasticsearch', 'Solr', 'Zabbix', 'Grafana',
  'phpMyAdmin', 'ThinkPHP', 'Laravel', 'Drupal', 'WordPress', 'Joomla',
  'Redis', 'MongoDB', 'MySQL', 'PostgreSQL', 'Oracle', 'MSSQL',
  'Nacos', 'Dubbo', 'RocketMQ', 'Kafka', 'RabbitMQ', 'ActiveMQ',
  'Harbor', 'Kubernetes', 'Docker', 'Rancher', 'Portainer'
])

// 常见高危端口列表
const commonHighRiskPorts = ref([
  { label: '22 (SSH)', value: 22 },
  { label: '23 (Telnet)', value: 23 },
  { label: '445 (SMB)', value: 445 },
  { label: '1433 (MSSQL)', value: 1433 },
  { label: '1521 (Oracle)', value: 1521 },
  { label: '3306 (MySQL)', value: 3306 },
  { label: '3389 (RDP)', value: 3389 },
  { label: '5432 (PostgreSQL)', value: 5432 },
  { label: '5900 (VNC)', value: 5900 },
  { label: '6379 (Redis)', value: 6379 },
  { label: '7001 (Weblogic)', value: 7001 },
  { label: '8080 (Tomcat)', value: 8080 },
  { label: '8443 (HTTPS-Alt)', value: 8443 },
  { label: '8848 (Nacos)', value: 8848 },
  { label: '9000 (PHP-FPM)', value: 9000 },
  { label: '9200 (Elasticsearch)', value: 9200 },
  { label: '11211 (Memcached)', value: 11211 },
  { label: '27017 (MongoDB)', value: 27017 }
])

onMounted(() => {
  // 如果URL没有tab参数，添加默认的tab参数
  if (!route.query.tab) {
    router.replace({ query: { ...route.query, tab: activeTab.value } })
  }
  loadApiConfigs()
  loadSubfinderProviders()
})

// 监听tab切换，按需加载数据
watch(activeTab, (val) => {
  if (val === 'workspace' && workspaceList.value.length === 0) {
    loadWorkspaceList()
  } else if (val === 'user' && userList.value.length === 0) {
    loadUserList()
  } else if (val === 'organization' && orgList.value.length === 0) {
    loadOrgList()
  } else if (val === 'notify' && notifyProviders.value.length === 0) {
    loadNotifyProviders()
    loadNotifyConfigList()
  }
})

// 在线API配置
async function loadApiConfigs() {
  const res = await request.post('/onlineapi/config/list', {})
  if (res.code === 0 && res.list) {
    res.list.forEach(item => {
      if (apiConfigs[item.platform]) {
        apiConfigs[item.platform].key = item.key
        apiConfigs[item.platform].secret = item.secret
        if (item.platform === 'fofa' && item.version) {
          apiConfigs.fofa.version = item.version
        }
      }
    })
  }
}

async function saveApiConfig(platform) {
  const config = apiConfigs[platform]
  const data = {
    platform,
    key: config.key,
    secret: config.secret
  }
  // Fofa需要传递版本信息
  if (platform === 'fofa') {
    data.version = config.version
  }
  const res = await request.post('/onlineapi/config/save', data)
  if (res.code === 0) {
    ElMessage.success('保存成功')
  } else {
    ElMessage.error(res.msg || '保存失败')
  }
}

// Subfinder配置
async function loadSubfinderProviders() {
  subfinderLoading.value = true
  try {
    const infoRes = await getSubfinderProviderInfo()
    if (infoRes.code !== 0) {
      ElMessage.error(infoRes.msg || '获取数据源信息失败')
      return
    }

    const listRes = await getSubfinderProviderList()
    const configuredMap = {}
    if (listRes.code === 0 && listRes.list) {
      listRes.list.forEach(item => {
        configuredMap[item.provider] = item
      })
    }

    subfinderProviders.value = infoRes.list.map(info => {
      const configured = configuredMap[info.provider]
      return {
        ...info,
        keyInput: '',
        enabled: configured ? configured.status === 'enable' : false,
        configured: !!configured,
        maskedKey: configured && configured.keys?.length > 0 ? configured.keys[0] : ''
      }
    })
  } finally {
    subfinderLoading.value = false
  }
}

async function saveSubfinderProvider(row) {
  if (!row.keyInput && !row.configured) {
    ElMessage.warning('请输入API密钥')
    return
  }

  const data = {
    provider: row.provider,
    keys: row.keyInput ? [row.keyInput] : [],
    status: row.enabled ? 'enable' : 'disable',
    description: row.description
  }

  const res = await saveSubfinderProviderApi(data)
  if (res.code === 0) {
    ElMessage.success('保存成功')
    row.configured = true
    row.keyInput = ''
    await loadSubfinderProviders()
  } else {
    ElMessage.error(res.msg || '保存失败')
  }
}

function openApiUrl(url) {
  window.open(url, '_blank')
}

// 工作空间管理
async function loadWorkspaceList() {
  workspaceLoading.value = true
  try {
    const res = await request.post('/workspace/list', { page: 1, pageSize: 100 })
    if (res.code === 0) workspaceList.value = res.list || []
  } finally {
    workspaceLoading.value = false
  }
}

function showWorkspaceDialog(row = null) {
  if (row) {
    Object.assign(workspaceForm, { id: row.id, name: row.name, description: row.description })
  } else {
    Object.assign(workspaceForm, { id: '', name: '', description: '' })
  }
  workspaceDialogVisible.value = true
}

async function handleWorkspaceSubmit() {
  await workspaceFormRef.value.validate()
  workspaceSubmitting.value = true
  try {
    const res = await request.post('/workspace/save', workspaceForm)
    if (res.code === 0) {
      ElMessage.success(workspaceForm.id ? '更新成功' : '创建成功')
      workspaceDialogVisible.value = false
      loadWorkspaceList()
    } else {
      ElMessage.error(res.msg)
    }
  } finally {
    workspaceSubmitting.value = false
  }
}

async function handleDeleteWorkspace(row) {
  await ElMessageBox.confirm('确定删除该工作空间吗？', '提示', { type: 'warning' })
  const res = await request.post('/workspace/delete', { id: row.id })
  if (res.code === 0) {
    ElMessage.success('删除成功')
    loadWorkspaceList()
  }
}

// 用户管理
async function loadUserList() {
  userLoading.value = true
  try {
    const res = await getUserList({ page: 1, pageSize: 100 })
    if (res.code === 0) userList.value = res.list || []
  } finally {
    userLoading.value = false
  }
}

function showUserDialog(row = null) {
  if (row) {
    userForm.value = { ...row, password: '' }
  } else {
    userForm.value = { id: '', username: '', password: '', status: 'enable' }
  }
  userDialogVisible.value = true
}

async function handleUserSubmit() {
  if (!userFormRef.value) return
  try {
    await userFormRef.value.validate()
    userSubmitting.value = true
    const api = userForm.value.id ? updateUser : createUser
    const res = await api(userForm.value)
    if (res.code === 0) {
      ElMessage.success(res.msg || '操作成功')
      userDialogVisible.value = false
      loadUserList()
    } else {
      ElMessage.error(res.msg || '操作失败')
    }
  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    userSubmitting.value = false
  }
}

async function handleDeleteUser(row) {
  try {
    await ElMessageBox.confirm('确定要删除该用户吗？', '提示', { type: 'warning' })
    const res = await deleteUser({ id: row.id })
    if (res.code === 0) {
      ElMessage.success(res.msg || '删除成功')
      loadUserList()
    } else {
      ElMessage.error(res.msg || '删除失败')
    }
  } catch (error) {}
}

function showResetPasswordDialog(row) {
  resetForm.value = { id: row.id, newPassword: '', confirmPassword: '' }
  resetPasswordVisible.value = true
}

async function handleResetPassword() {
  if (!resetFormRef.value) return
  try {
    await resetFormRef.value.validate()
    resetting.value = true
    const res = await resetUserPassword({
      id: resetForm.value.id,
      newPassword: resetForm.value.newPassword
    })
    if (res.code === 0) {
      ElMessage.success(res.msg || '密码重置成功')
      resetPasswordVisible.value = false
    } else {
      ElMessage.error(res.msg || '密码重置失败')
    }
  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    resetting.value = false
  }
}

// 组织管理
async function loadOrgList() {
  orgLoading.value = true
  try {
    const res = await request.post('/organization/list', { page: 1, pageSize: 100 })
    const data = res.data || res
    if (data.code === 0) orgList.value = data.list || []
  } finally {
    orgLoading.value = false
  }
}

function showOrgDialog(row = null) {
  if (row) {
    Object.assign(orgForm, { id: row.id, name: row.name, description: row.description })
  } else {
    Object.assign(orgForm, { id: '', name: '', description: '' })
  }
  orgDialogVisible.value = true
}

async function handleOrgSubmit() {
  await orgFormRef.value.validate()
  orgSubmitting.value = true
  try {
    const res = await request.post('/organization/save', orgForm)
    const data = res.data || res
    if (data.code === 0) {
      ElMessage.success(orgForm.id ? '更新成功' : '创建成功')
      orgDialogVisible.value = false
      loadOrgList()
    } else {
      ElMessage.error(data.msg)
    }
  } finally {
    orgSubmitting.value = false
  }
}

async function handleDeleteOrg(row) {
  await ElMessageBox.confirm('确定删除该组织吗？', '提示', { type: 'warning' })
  const res = await request.post('/organization/delete', { id: row.id })
  const data = res.data || res
  if (data.code === 0) {
    ElMessage.success('删除成功')
    loadOrgList()
  }
}

async function handleOrgStatusChange(row) {
  const res = await request.post('/organization/updateStatus', {
    id: row.id,
    status: row.status
  })
  const data = res.data || res
  if (data.code === 0) {
    ElMessage.success('状态更新成功')
  } else {
    row.status = row.status === 'enable' ? 'disable' : 'enable'
    ElMessage.error(data.msg || '状态更新失败')
  }
}

// 通知配置管理
async function loadNotifyProviders() {
  try {
    const res = await getNotifyProviders()
    if (res.code === 0) {
      notifyProviders.value = res.list || []
    }
  } catch (error) {
    console.error('加载通知提供者失败:', error)
  }
}

async function loadNotifyConfigList() {
  notifyLoading.value = true
  try {
    const res = await getNotifyConfigList()
    if (res.code === 0) {
      notifyConfigList.value = (res.list || []).map(item => ({ ...item, testing: false }))
    }
  } finally {
    notifyLoading.value = false
  }
}

function getProviderName(providerId) {
  const provider = notifyProviders.value.find(p => p.id === providerId)
  return provider ? provider.name : providerId
}

function handleProviderChange(providerId) {
  const provider = notifyProviders.value.find(p => p.id === providerId)
  currentProviderFields.value = provider ? provider.configFields || [] : []
  // 重置配置数据
  notifyForm.value.configData = {}
}

function showNotifyDialog(row = null) {
  if (row) {
    // 编辑模式
    let configData = {}
    try {
      configData = JSON.parse(row.config || '{}')
    } catch (e) {
      configData = {}
    }
    // 处理高危过滤配置
    const highRiskFilter = row.highRiskFilter || {
      enabled: false,
      highRiskFingerprints: [],
      highRiskPorts: [],
      highRiskPocSeverities: []
    }
    notifyForm.value = {
      id: row.id,
      name: row.name,
      provider: row.provider,
      configData: configData,
      messageTemplate: row.messageTemplate || '',
      status: row.status,
      highRiskFilter: {
        enabled: highRiskFilter.enabled || false,
        highRiskFingerprints: highRiskFilter.highRiskFingerprints || [],
        highRiskPorts: highRiskFilter.highRiskPorts || [],
        highRiskPocSeverities: highRiskFilter.highRiskPocSeverities || []
      }
    }
    // 加载对应provider的字段
    const provider = notifyProviders.value.find(p => p.id === row.provider)
    currentProviderFields.value = provider ? provider.configFields || [] : []
  } else {
    // 新增模式
    notifyForm.value = { 
      id: '', 
      name: '', 
      provider: '', 
      configData: {}, 
      messageTemplate: '', 
      status: 'enable',
      highRiskFilter: {
        enabled: false,
        highRiskFingerprints: [],
        highRiskPorts: [],
        highRiskPocSeverities: []
      }
    }
    currentProviderFields.value = []
  }
  notifyDialogVisible.value = true
}

async function handleNotifySubmit() {
  if (!notifyFormRef.value) return
  try {
    await notifyFormRef.value.validate()
    notifySubmitting.value = true
    
    const data = {
      id: notifyForm.value.id,
      name: notifyForm.value.name,
      provider: notifyForm.value.provider,
      config: JSON.stringify(notifyForm.value.configData),
      messageTemplate: notifyForm.value.messageTemplate,
      status: notifyForm.value.status,
      highRiskFilter: notifyForm.value.highRiskFilter
    }
    
    const res = await saveNotifyConfig(data)
    if (res.code === 0) {
      ElMessage.success(res.msg || '保存成功')
      notifyDialogVisible.value = false
      loadNotifyConfigList()
    } else {
      ElMessage.error(res.msg || '保存失败')
    }
  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    notifySubmitting.value = false
  }
}

async function handleNotifyStatusChange(row) {
  const data = {
    id: row.id,
    name: row.name,
    provider: row.provider,
    config: row.config,
    messageTemplate: row.messageTemplate,
    status: row.status,
    highRiskFilter: row.highRiskFilter
  }
  const res = await saveNotifyConfig(data)
  if (res.code === 0) {
    ElMessage.success('状态更新成功')
  } else {
    row.status = row.status === 'enable' ? 'disable' : 'enable'
    ElMessage.error(res.msg || '状态更新失败')
  }
}

async function handleTestNotify(row) {
  row.testing = true
  try {
    const res = await testNotifyConfig({
      provider: row.provider,
      config: row.config,
      messageTemplate: row.messageTemplate
    })
    if (res.code === 0) {
      ElMessage.success(res.msg || '测试成功，请检查是否收到通知')
    } else {
      ElMessage.error(res.msg || '测试失败')
    }
  } finally {
    row.testing = false
  }
}

async function handleTestNotifyForm() {
  if (!notifyForm.value.provider) {
    ElMessage.warning('请先选择通知渠道')
    return
  }
  notifyTesting.value = true
  try {
    const res = await testNotifyConfig({
      provider: notifyForm.value.provider,
      config: JSON.stringify(notifyForm.value.configData),
      messageTemplate: notifyForm.value.messageTemplate
    })
    if (res.code === 0) {
      ElMessage.success(res.msg || '测试成功，请检查是否收到通知')
    } else {
      ElMessage.error(res.msg || '测试失败')
    }
  } finally {
    notifyTesting.value = false
  }
}

async function handleDeleteNotify(row) {
  try {
    await ElMessageBox.confirm('确定要删除该通知配置吗？', '提示', { type: 'warning' })
    const res = await deleteNotifyConfig(row.id)
    if (res.code === 0) {
      ElMessage.success(res.msg || '删除成功')
      loadNotifyConfigList()
    } else {
      ElMessage.error(res.msg || '删除失败')
    }
  } catch (error) {}
}
</script>

<style lang="scss" scoped>
.settings-page {
  .card-header {
    font-size: 16px;
    font-weight: 500;
  }

  .settings-tabs {
    min-height: 500px;

    :deep(.el-tabs__item) {
      height: 50px;
      line-height: 50px;
    }
  }

  .tab-content {
    padding: 0 20px;
  }

  .tab-action-bar {
    margin-bottom: 16px;
  }
}
</style>
