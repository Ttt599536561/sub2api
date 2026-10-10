import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { getPlatformQuotas, resetPlatformQuotaWindow, updatePlatformQuotas } from '@/api/admin/users'
import { syncUpstreamModels, syncUpstreamModelsPreview } from '@/api/admin/accounts'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const quotaPayload = [{ platform: 'openai', daily_limit_usd: 70, weekly_limit_usd: null, monthly_limit_usd: null }]
const previewPayload = { platform: 'openai', type: 'apikey', api_key: 'upstream-key', base_url: 'https://upstream.example/v1' }
const result = { models: ['model-A'], platform_quotas: quotaPayload }
const flows = [
  { name: 'quota read', start: () => getPlatformQuotas(7), url: '/admin/users/7/platform-quotas', method: 'get', body: undefined },
  { name: 'quota save', start: () => updatePlatformQuotas(7, quotaPayload), url: '/admin/users/7/platform-quotas', method: 'put', body: { quotas: quotaPayload } },
  { name: 'quota reset', start: () => resetPlatformQuotaWindow(7, 'openai', 'daily'), url: '/admin/users/7/platform-quotas/reset', method: 'post', body: { platform: 'openai', window: 'daily' } },
  { name: 'saved account model sync', start: () => syncUpstreamModels(7), url: '/admin/accounts/7/models/sync-upstream', method: 'post', body: undefined },
  { name: 'model sync preview', start: () => syncUpstreamModelsPreview(previewPayload), url: '/admin/accounts/models/sync-upstream-preview', method: 'post', body: previewPayload }
]
let requests: InternalAxiosRequestConfig[]
function login(id: number, session: string) {
  localStorage.setItem('auth_user', JSON.stringify({ id, role: 'admin' }))
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_token', `token-${id}`)
}
beforeEach(() => {
  localStorage.clear(); login(7, 'session-A'); requests = []
  apiClient.defaults.adapter = async config => {
    requests.push(config)
    return { data: result, status: 200, statusText: 'OK', headers: {}, config }
  }
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })

describe.each(flows)('$name administrator session ownership', flow => {
  it('cancels before dispatch under a replacement administrator', async () => {
    const pending = flow.start()
    const rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login(8, 'session-B'); await rejected
    expect(requests).toHaveLength(0)
  })

  it('rejects a delayed reply from the previous administrator login', async () => {
    let finish!: () => void
    let reached!: () => void
    const dispatched = new Promise<void>(resolve => { reached = resolve })
    apiClient.defaults.adapter = config => {
      requests.push(config); reached()
      return new Promise(resolve => { finish = () => resolve({ data: result, status: 200, statusText: 'OK', headers: {}, config }) })
    }
    const pending = flow.start()
    const rejected = expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    await dispatched; login(7, 'session-B'); finish(); await rejected
  })

  it('preserves endpoint, body, GET timezone and access-token rotation in the same login', async () => {
    const pending = flow.start(); localStorage.setItem('auth_token', 'rotated-token')
    await expect(pending).resolves.toEqual(result)
    expect(requests[0].url).toBe(flow.url)
    expect(requests[0].method).toBe(flow.method)
    if (flow.body === undefined) expect(requests[0].data).toBeUndefined()
    else expect(JSON.parse(requests[0].data)).toEqual(flow.body)
    if (flow.method === 'get') expect(requests[0].params.timezone).toEqual(expect.any(String))
    expect(requests[0].headers.get('Authorization')).toBe('Bearer rotated-token')
  })
})
