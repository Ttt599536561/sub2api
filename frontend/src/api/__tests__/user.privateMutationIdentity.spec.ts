import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { changePassword, getProfile, removeNotifyEmail, sendNotifyEmailCode, toggleNotifyEmail, transferAffiliateQuota, verifyNotifyEmail } from '@/api/user'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const response = { message: 'ok', id: 7, username: 'alice', transferred_quota: 30 }
const flows = [
  { name: 'profile read after mutation', start: () => getProfile(), method: 'get', url: '/user/profile', body: undefined, result: response },
  { name: 'password', start: () => changePassword('old-password', 'new-password'), method: 'put', url: '/user/password', body: { old_password: 'old-password', new_password: 'new-password' }, result: response },
  { name: 'notify code', start: () => sendNotifyEmailCode('notify@example.com'), method: 'post', url: '/user/notify-email/send-code', body: { email: 'notify@example.com' }, result: undefined },
  { name: 'notify verification', start: () => verifyNotifyEmail('notify@example.com', '123456'), method: 'post', url: '/user/notify-email/verify', body: { email: 'notify@example.com', code: '123456' }, result: undefined },
  { name: 'notify removal', start: () => removeNotifyEmail('notify@example.com'), method: 'delete', url: '/user/notify-email', body: { email: 'notify@example.com' }, result: undefined },
  { name: 'notify toggle', start: () => toggleNotifyEmail('notify@example.com', true), method: 'put', url: '/user/notify-email/toggle', body: { email: 'notify@example.com', disabled: true }, result: response },
  { name: 'affiliate balance transfer', start: () => transferAffiliateQuota(), method: 'post', url: '/user/aff/transfer', body: undefined, result: response }
]
let requests: InternalAxiosRequestConfig[]
function login(id: number, session: string) {
  localStorage.setItem('auth_user', JSON.stringify({ id }))
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_token', `token-${id}`)
}
beforeEach(() => {
  localStorage.clear(); login(7, 'session-A'); requests = []
  apiClient.defaults.adapter = async config => { requests.push(config); return { data: response, status: 200, statusText: 'OK', headers: {}, config } }
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })
describe.each(flows)('$name request ownership', flow => {
  it.each([8, 7])('does not dispatch under a replacement login for user %s', async id => {
    const pending = flow.start(), rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login(id, 'session-B'); await rejected
    expect(requests).toHaveLength(0)
  })
  it('rejects a late successful reply after another login', async () => {
    let finish!: () => void, reached!: () => void
    const dispatched = new Promise<void>(resolve => { reached = resolve })
    apiClient.defaults.adapter = config => {
      requests.push(config); reached()
      return new Promise(resolve => { finish = () => resolve({ data: response, status: 200, statusText: 'OK', headers: {}, config }) })
    }
    const pending = flow.start(), rejected = expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    await dispatched; login(8, 'session-B'); finish(); await rejected
  })
  it('preserves method, endpoint, body and same-session token rotation', async () => {
    const pending = flow.start(); localStorage.setItem('auth_token', 'rotated-token')
    await expect(pending).resolves.toEqual(flow.result)
    expect(requests[0].method).toBe(flow.method); expect(requests[0].url).toBe(flow.url)
    if (flow.body === undefined) expect(requests[0].data).toBeUndefined()
    else expect(JSON.parse(requests[0].data)).toEqual(flow.body)
    expect(requests[0].headers.get('Authorization')).toBe('Bearer rotated-token')
  })
})
