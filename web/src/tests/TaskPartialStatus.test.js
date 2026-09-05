import { describe, expect, test, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import zhCN from '@/i18n/locales/zh-CN.json'
import enUS from '@/i18n/locales/en-US.json'
import Task from '@/views/Task.vue'
import TaskDetail from '@/views/TaskDetail.vue'
import { getTaskList, getTaskDetail } from '@/api/task'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/task', query: { id: 'partial-task' } }),
  useRouter: () => ({ back: vi.fn(), push: vi.fn(), replace: vi.fn() })
}))

vi.mock('@/api/task', () => ({
  getTaskList: vi.fn().mockResolvedValue({
    code: 0,
    list: [{ id: 'partial-task', name: 'completed partial task', target: 'example.com', status: 'PARTIAL', progress: 100, subTaskDone: 6, subTaskCount: 6 }],
    total: 1
  }),
  getTaskDetail: vi.fn().mockResolvedValue({
    code: 0,
    data: { id: 'partial-task', taskId: 'partial-task', name: 'completed partial task', target: 'example.com', status: 'PARTIAL', progress: 100, subTaskDone: 6, subTaskCount: 6 }
  }),
  createTask: vi.fn(),
  deleteTask: vi.fn(),
  batchDeleteTask: vi.fn(),
  retryTask: vi.fn(),
  startTask: vi.fn(),
  pauseTask: vi.fn(),
  resumeTask: vi.fn(),
  stopTask: vi.fn(),
  updateTask: vi.fn(),
  getTaskLogs: vi.fn().mockResolvedValue({ code: 0, list: [] }),
  getWorkerList: vi.fn().mockResolvedValue({ code: 0, list: [] }),
  saveScanConfig: vi.fn(),
  getScanConfig: vi.fn()
}))

vi.mock('@/api/request', () => ({
  default: { post: vi.fn().mockResolvedValue({ code: 0, list: [] }) }
}))

function mountStatusMapper(component) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': zhCN, 'en-US': enUS }
  })
  const wrapper = mount(component, { global: { plugins: [i18n, ElementPlus] } })
  return { i18n, wrapper }
}

describe('task PARTIAL status localization', () => {
  test.each([Task, TaskDetail])('maps PARTIAL to the existing completed translation in %s', async (component) => {
    const { i18n, wrapper } = mountStatusMapper(component)

    expect(wrapper.vm.getStatusText({ status: 'PARTIAL' })).toBe('已完成')
    i18n.global.locale.value = 'en-US'
    await wrapper.vm.$nextTick()
    expect(wrapper.vm.getStatusText({ status: 'PARTIAL' })).toBe('Completed')
    expect(wrapper.vm.getStatusText({ status: 'ARCHIVED' })).toBe('ARCHIVED')

    wrapper.unmount()
  })

  test.each([
    { component: Task, tagSelector: '.el-table .el-tag', request: getTaskList },
    { component: TaskDetail, tagSelector: '.detail-toolbar .el-tag', request: getTaskDetail }
  ])('renders PARTIAL as completed and updates the visible tag when locale changes in $component.__name', async ({ component, tagSelector, request }) => {
    const { i18n, wrapper } = mountStatusMapper(component)
    await flushPromises()

    const statusTag = () => wrapper.findAll(tagSelector).find(tag => tag.text() === '已完成' || tag.text() === 'Completed')
    expect(statusTag()?.text()).toBe('已完成')

    const requestCount = request.mock.calls.length
    i18n.global.locale.value = 'en-US'
    await wrapper.vm.$nextTick()
    expect(statusTag()?.text()).toBe('Completed')

    i18n.global.locale.value = 'zh-CN'
    await wrapper.vm.$nextTick()
    expect(statusTag()?.text()).toBe('已完成')
    expect(request).toHaveBeenCalledTimes(requestCount)

    wrapper.unmount()
  })
})
