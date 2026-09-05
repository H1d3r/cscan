import request from './request'

// 单条漏洞复验（worker 执行复测）：下发复验任务到 worker
export function reverifyVul(data) {
  return request.post('/vul/reverify', data)
}
