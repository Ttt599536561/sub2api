import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient, ownedAuthRequestConfig } from '@/api/client'
import { list } from '@/api/admin/accounts'
import { listRequestDetails } from '@/api/admin/ops'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const result = { items: [], total: 0 }
const flows = [
  { name: 'account names', url: '/admin/accounts', start: (options?: ReturnType<typeof ownedAuthRequestConfig>) => list(1, 1000, { lite: '1' }, options) },
  { name: 'ops details', url: '/admin/ops/requests', start: (options?: ReturnType<typeof ownedAuthRequestConfig>) => listRequestDetails({ page: 1, page_size: 10 }, options) },
]
let requests: InternalAxiosRequestConfig[]
function login(sessionID: string) {
  localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'admin' }))
  localStorage.setItem('auth_session_id', sessionID)
  localStorage.setItem('auth_token', 'token-A')
}
beforeEach(() => {
  localStorage.clear()
  login('session-A')
  requests = []
  apiClient.defaults.adapter = async config => {
    requests.push(config)
    return { data: result, status: 200, statusText: 'OK', headers: {}, config }
  }
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })

describe.each(flows)('$name request ownership options', flow => {
  it('cancels queued requests when their login is replaced', async () => {
    const pending = flow.start(ownedAuthRequestConfig())
    const rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login('session-B')
    await rejected
    expect(requests).toHaveLength(0)
  })

  it.each([true, false])('preserves normal reads with ownership enabled: %s', async owned => {
    const pending = flow.start(owned ? ownedAuthRequestConfig() : undefined)
    localStorage.setItem('auth_token', 'rotated-token')
    await expect(pending).resolves.toEqual(result)
    expect(requests[0].url).toBe(flow.url)
    expect(requests[0].params.timezone).toEqual(expect.any(String))
    expect(requests[0].headers.get('Authorization')).toBe('Bearer rotated-token')
  })
})
