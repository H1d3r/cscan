import request from '@/api/request'

const DEFAULT_EXPORT_PAGE_SIZE = 100

/**
 * Fetches a complete filtered result set through bounded server-side pages.
 * The first response fixes the expected total so concurrent inserts cannot
 * keep an export loop running indefinitely.
 */
export async function fetchAllPages(api, buildPayload, pageSize = DEFAULT_EXPORT_PAGE_SIZE) {
  const rows = []
  let page = 1
  let expectedTotal = null

  while (true) {
    const response = await request.post(api, buildPayload(page, pageSize))
    if (response?.code !== 0) {
      throw new Error(response?.msg || 'Failed to fetch export page')
    }

    const payload = response?.data?.list !== undefined ? response.data : response
    const pageRows = payload?.list || []
    if (expectedTotal === null) expectedTotal = Number(payload?.total || 0)
    rows.push(...pageRows)

    if (pageRows.length < pageSize || rows.length >= expectedTotal) break
    page += 1
  }

  return rows
}
