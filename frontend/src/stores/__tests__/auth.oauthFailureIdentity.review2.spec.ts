import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { getCurrentUser } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const api = vi.hoisted(() => ({ login: vi.fn(), getCurrentUser: vi.fn(), logout: vi.fn(), refreshToken: vi.fn() }))
vi.mock('@/api', () => ({ authAPI: api, passkeyAPI: { login: vi.fn() }, isTotp2FARequired: () => false }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const credentials = { email: 'review@example.com', password: 'test-only-password' }
const replacement = {
  access_token: 'replacement-access', refresh_token: 'replacement-refresh', expires_in: 3600,
  user: { id: 8, username: 'replacement', role: 'user' },
}

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  vi.useFakeTimers()
  vi.resetAllMocks()
  api.login.mockResolvedValue(replacement)
  window.history.replaceState({}, '', '/login')
})
afterEach(() => {
  apiClient.defaults.adapter = originalAdapter
  vi.clearAllTimers()
  vi.useRealTimers()
  localStorage.clear()
})

function expectReplacement(store: ReturnType<typeof useAuthStore>, session: string | null) {
  expect(store.user?.id).toBe(8)
  expect(store.isAuthenticated).toBe(true)
  expect(store.token).toBe(replacement.access_token)
  expect(localStorage.getItem('auth_token')).toBe(replacement.access_token)
  expect(localStorage.getItem('refresh_token')).toBe(replacement.refresh_token)
  expect(localStorage.getItem('auth_session_id')).toBe(session)
}

describe('OAuth profile failures after a replacement login', () => {
  it.each([0, 429, 503])('preserves the new login after a delayed store profile failure %s', async status => {
    let reject!: (reason: unknown) => void
    api.getCurrentUser.mockReturnValueOnce(new Promise((_resolve, failure) => { reject = failure }))
    const store = useAuthStore()
    const old = store.setToken('oauth-access').catch(error => error)
    await store.login(credentials)
    const session = localStorage.getItem('auth_session_id')
    reject({ status, code: status ? 'SERVER_REJECTION' : 'ERR_NETWORK' })
    const result = await old
    expectReplacement(store, session)
    expect(result.code).toBe('AUTH_SESSION_CHANGED')
  })

  it.each([0, 429, 503])('preserves the new login through the real API and Axios error interceptor for %s', async status => {
    let fail!: () => void
    let started!: () => void
    const dispatched = new Promise<void>(resolve => { started = resolve })
    api.getCurrentUser.mockImplementationOnce(getCurrentUser)
    apiClient.defaults.adapter = (config: InternalAxiosRequestConfig) => new Promise((_resolve, reject) => {
      fail = () => reject(new AxiosError('Old OAuth profile failed', status ? 'ERR_BAD_RESPONSE' : 'ERR_NETWORK', config, undefined,
        status ? { status, statusText: 'Unavailable', headers: {}, config, data: { code: status, message: 'Old OAuth profile failed' } } : undefined))
      started()
    })
    const store = useAuthStore()
    const old = store.setToken('oauth-access').catch(error => error)
    await dispatched
    await store.login(credentials)
    const session = localStorage.getItem('auth_session_id')
    fail()
    const result = await old
    expectReplacement(store, session)
    expect(result.code).toBe('AUTH_SESSION_CHANGED')
  })

  it('still clears partial OAuth state when its own current profile request fails', async () => {
    const failure = { status: 503, message: 'Unavailable' }
    api.getCurrentUser.mockRejectedValueOnce(failure)
    const store = useAuthStore()
    await expect(store.setToken('current-oauth')).rejects.toBe(failure)
    expect(store.user).toBeNull()
    expect(store.token).toBeNull()
    expect(localStorage.getItem('auth_token')).toBeNull()
  })
})
