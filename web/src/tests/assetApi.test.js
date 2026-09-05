import { describe, test, expect, vi, beforeEach } from 'vitest'

// asset.js 中 request 是默认导出且被直接作为函数调用（request({ url, method, data })）
const request = vi.hoisted(() => vi.fn((cfg) => Promise.resolve({ code: 0, data: { id: 'a1' } })))

vi.mock('@/api/request', () => ({
  default: request
}))

import { getAssetDetail, getAssetInventory } from '@/api/asset'

beforeEach(() => {
  request.mockClear()
  request.mockImplementation((cfg) => Promise.resolve({ code: 0, data: { id: 'a1' } }))
})

describe('asset API 层', () => {
  test('getAssetDetail 向 /asset/detail 发送 POST，并携带 id', async () => {
    const res = await getAssetDetail({ id: 'a1' })

    expect(request).toHaveBeenCalledTimes(1)
    const arg = request.mock.calls[0][0]
    expect(arg.url).toBe('/asset/detail')
    expect(arg.method).toBe('post')
    expect(arg.data).toEqual({ id: 'a1' })
    expect(res.data.id).toBe('a1')
  })

  test('getAssetInventory 透传后端消费的 requireRecognitionOrShot 过滤参数', async () => {
    request.mockImplementation(() => Promise.resolve({ code: 0, total: 0, list: [] }))
    await getAssetInventory({ page: 1, pageSize: 10, requireRecognitionOrShot: true })

    expect(request).toHaveBeenCalledTimes(1)
    const arg = request.mock.calls[0][0]
    expect(arg.url).toBe('/asset/inventory')
    expect(arg.data.requireRecognitionOrShot).toBe(true)
  })

  test('getAssetInventory 默认不带 requireRecognitionOrShot', async () => {
    request.mockImplementation(() => Promise.resolve({ code: 0, total: 0, list: [] }))
    await getAssetInventory({ page: 1, pageSize: 10 })

    const arg = request.mock.calls[0][0]
    expect(arg.data.requireRecognitionOrShot).toBeUndefined()
  })
})
