import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { prepareOAuthBindAccessTokenCookie } from '@/api/auth'
import { startOAuthBinding } from '@/api/user'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const actualWindow = window
const initialHref = `${actualWindow.location.origin}/profile`
let target: { href: string; origin: string; pathname: string }
let requests: InternalAxiosRequestConfig[]
function login(id: number, session: string) {
  localStorage.setItem('auth_user', JSON.stringify({ id }))
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_token', `token-${id}`)
}
beforeEach(() => {
  localStorage.clear(); login(7, 'session-A'); target = { href: initialHref, origin: actualWindow.location.origin, pathname: '/profile' }; requests = []
  vi.stubGlobal('window', new Proxy(actualWindow, {
    get(object, key) {
      if (key === 'location') return target
      const value = Reflect.get(object, key, object)
      return typeof value === 'function' ? value.bind(object) : value
    }
  }))
  apiClient.defaults.adapter = async config => { requests.push(config); return { data: {}, status: 200, statusText: 'OK', headers: {}, config } }
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; vi.unstubAllGlobals(); localStorage.clear() })
describe('OAuth binding keeps the owner from cookie preparation through navigation', () => {
  it.each([8, 7])('does not dispatch preparation or navigate after user %s replaces the login', async id => {
    const pending = startOAuthBinding('linuxdo', { redirectTo: '/profile' })
    const rejected = expect(pending).rejects.toSatisfy(axios.isCancel)
    login(id, 'session-B'); await rejected
    expect(requests).toHaveLength(0); expect(target.href).toBe(initialHref)
  })
  it.each(['session', 'unmount'])('does not navigate when %s changes while preparation is pending', async change => {
    let finish!: () => void, reached!: () => void, mounted = true
    const dispatched = new Promise<void>(resolve => { reached = resolve })
    apiClient.defaults.adapter = config => {
      requests.push(config); reached()
      return new Promise(resolve => { finish = () => resolve({ data: {}, status: 200, statusText: 'OK', headers: {}, config }) })
    }
    const pending = startOAuthBinding('linuxdo', { redirectTo: '/profile', isCurrent: () => mounted })
    const settled = pending.catch(error => error)
    await dispatched
    if (change === 'session') { login(8, 'session-B'); actualWindow.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' })) }
    else mounted = false
    finish(); const result = await settled
    if (change === 'session') expect(result).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(target.href).toBe(initialHref)
  })
  it('keeps normal route parameters and same-session token rotation', async () => {
    const pending = startOAuthBinding('linuxdo', { redirectTo: '/profile?tab=identity' })
    localStorage.setItem('auth_token', 'rotated-token'); await pending
    expect(requests[0].url).toBe('/auth/oauth/bind-token'); expect(requests[0].data).toBeUndefined()
    expect(requests[0].headers.get('Authorization')).toBe('Bearer rotated-token')
    expect(target.href).toBe('/api/v1/auth/oauth/linuxdo/bind/start?redirect=%2Fprofile%3Ftab%3Didentity&intent=bind_current_user')
  })
  it('preserves default preparation for existing callback callers', async () => {
    await prepareOAuthBindAccessTokenCookie()
    expect(requests).toHaveLength(1); expect(requests[0].url).toBe('/auth/oauth/bind-token')
    expect(requests[0].data).toBeUndefined(); expect(target.href).toBe(initialHref)
  })
})
