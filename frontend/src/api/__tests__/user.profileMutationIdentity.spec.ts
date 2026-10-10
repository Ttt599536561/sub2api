import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { bindEmailIdentity, unbindAuthIdentity, updateProfile } from '@/api/user'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const user = { id: 7, email: 'alice@example.com', username: 'alice' }
const flows = [
  { name: 'profile', start: () => updateProfile({ username: 'saved-name' }), url: '/user', method: 'put' },
  { name: 'email binding', start: () => bindEmailIdentity({ email: 'new@example.com', verify_code: '123456', password: 'password' }), url: '/user/account-bindings/email', method: 'post' },
  { name: 'identity unbinding', start: () => unbindAuthIdentity('linuxdo'), url: '/user/account-bindings/linuxdo', method: 'delete' }
]
let requests: InternalAxiosRequestConfig[]
function login(id: number, session: string) {
  localStorage.setItem('auth_user', JSON.stringify({ id }))
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_token', `token-${id}`)
}
beforeEach(() => {
  localStorage.clear(); login(7, 'session-A'); requests = []
  apiClient.defaults.adapter = async config => {
    requests.push(config)
    return { data: user, status: 200, statusText: 'OK', headers: {}, config }
  }
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })

describe.each(flows)('$name API ownership', flow => {
  it('cancels before dispatch when the login is replaced', async () => {
    const pending = flow.start()
    const rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login(8, 'session-B'); await rejected
    expect(requests).toHaveLength(0)
  })

  it('rejects the old successful response after the login is replaced', async () => {
    let finish!: () => void
    let reached!: () => void
    const dispatched = new Promise<void>(resolve => { reached = resolve })
    apiClient.defaults.adapter = config => {
      requests.push(config); reached()
      return new Promise(resolve => { finish = () => resolve({ data: user, status: 200, statusText: 'OK', headers: {}, config }) })
    }
    const pending = flow.start()
    const rejected = expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    await dispatched; login(8, 'session-B'); finish(); await rejected
  })

  it('keeps the endpoint and same-session access token rotation', async () => {
    const pending = flow.start()
    localStorage.setItem('auth_token', 'rotated-token')
    await expect(pending).resolves.toEqual(user)
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe(flow.url)
    expect(requests[0].method).toBe(flow.method)
    expect(requests[0].headers.get('Authorization')).toBe('Bearer rotated-token')
  })
})
