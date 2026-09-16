import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import axios from 'axios'
import { apiClient } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { refreshAuthTokens } from '@/api/tokenRefresh'

const authAPI = vi.hoisted(() => ({ login: vi.fn(), login2FA: vi.fn(), register: vi.fn(), getCurrentUser: vi.fn(), logout: vi.fn(), refreshToken: vi.fn() }))
const passkeyAPI = vi.hoisted(() => ({ login: vi.fn() }))
vi.mock('@/api', () => ({ authAPI, passkeyAPI, isTotp2FARequired: () => false }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))

const user = { id: 7, role: 'admin', username: 'administrator' }
const response = { access_token: 'token-7', refresh_token: 'refresh-7', expires_in: 3600, user }
const credentials = { email: 'test@example.com', password: 'password' }

describe('API requests across authentication sessions', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.resetAllMocks()
    authAPI.login.mockResolvedValue(response)
    authAPI.login2FA.mockResolvedValue(response)
    authAPI.register.mockResolvedValue(response)
    authAPI.getCurrentUser.mockResolvedValue({ data: user })
    passkeyAPI.login.mockResolvedValue(response)
    vi.spyOn(axios, 'post').mockResolvedValue({ data: { code: 0, data: { access_token: 'refreshed-token', refresh_token: 'refreshed-refresh', expires_in: 3600 } } })
    window.history.replaceState({}, '', '/login')
  })

  afterEach(async () => { await useAuthStore().logout(); vi.restoreAllMocks() })

  it('does not apply a profile response from the previous login session', async () => {
    const store = useAuthStore()
    await store.login(credentials)
    let finish!: (value: unknown) => void
    authAPI.getCurrentUser.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const old = store.refreshUser()
    const stale = expect(old).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    authAPI.login.mockResolvedValueOnce({ ...response, access_token: 'new-token', user: { ...user, id: 8 } })
    await store.login(credentials)
    finish({ data: user })
    await stale

    expect(store.user?.id).toBe(8)
    expect(JSON.parse(localStorage.getItem('auth_user')!).id).toBe(8)
    expect(store.token).toBe('new-token')
  })

  it('does not apply a proactive token refresh from the previous login session', async () => {
    const store = useAuthStore()
    let finish!: (value: unknown) => void
    authAPI.refreshToken.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    authAPI.login.mockResolvedValueOnce({ ...response, expires_in: 1 })
    await store.login(credentials)
    authAPI.login.mockResolvedValueOnce({ ...response, access_token: 'new-token', user: { ...user, id: 8 } })
    await store.login(credentials)
    const currentExpiry = localStorage.getItem('token_expires_at')
    finish({ access_token: 'old-token-result', refresh_token: 'old-refresh-result', expires_in: 100 })
    await Promise.resolve()
    await Promise.resolve()

    expect(store.token).toBe('new-token')
    expect(localStorage.getItem('token_expires_at')).toBe(currentExpiry)
  })

  it.each(['success', 'failure'] as const)('does not clear a newly logged-in user after an old logout %s', async outcome => {
    const store = useAuthStore()
    await store.login(credentials)
    let finish!: () => void
    let fail!: (error: Error) => void
    authAPI.logout.mockImplementationOnce(() => new Promise<void>((resolve, reject) => { finish = resolve; fail = reject }))
    const old = store.logout()
    authAPI.login.mockResolvedValueOnce({ ...response, access_token: 'new-token', user: { ...user, id: 8 } })
    await store.login(credentials)
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    try {
      if (outcome === 'success') finish()
      else fail(new Error('old logout failed'))
      await old
      expect(store.user?.id).toBe(8)
      expect(localStorage.getItem('auth_token')).toBe('new-token')
    } finally { warning.mockRestore() }
  })

  it('does not share an in-flight token refresh with a new login session', async () => {
    const store = useAuthStore()
    await store.login(credentials)
    let finish!: (value: unknown) => void
    vi.mocked(axios.post).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const old = refreshAuthTokens()
    const oldFailure = expect(old).rejects.toThrow('Session changed during token refresh')
    await store.login(credentials)
    const fresh = refreshAuthTokens()
    try {
      expect(axios.post).toHaveBeenCalledTimes(2)
    } finally {
      finish({ data: { code: 0, data: { access_token: 'old-token', refresh_token: 'old-refresh', expires_in: 3600 } } })
      await oldFailure
    }
    await expect(fresh).resolves.toMatchObject({ access_token: 'refreshed-token' })
    expect(localStorage.getItem('auth_token')).toBe('refreshed-token')
  })

  it('can still log out when session marker storage is full', async () => {
    const store = useAuthStore()
    await store.login(credentials)
    const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('Storage full', 'QuotaExceededError')
    })
    try {
      await expect(store.logout()).resolves.toBeUndefined()
      expect(store.isAuthenticated).toBe(false)
      expect(localStorage.getItem('auth_token')).toBeNull()
    } finally { storage.mockRestore() }
  })

  it.each(['login', 'login2FA', 'passkey', 'register', 'oauth', 'refreshUser'] as const)('does not clear the new session when an old %s request is rejected as stale', async operation => {
    const store = useAuthStore()
    await store.login(credentials)
    const failure = { status: 401, code: 'AUTH_SESSION_CHANGED' }
    const api = operation === 'passkey' ? passkeyAPI.login : operation === 'oauth' || operation === 'refreshUser'
      ? authAPI.getCurrentUser : authAPI[operation]
    let fail!: (error: unknown) => void
    api.mockImplementationOnce(() => new Promise((_resolve, reject) => { fail = reject }))
    const stale = operation === 'login' ? store.login(credentials)
      : operation === 'login2FA' ? store.login2FA('temporary', '123456')
      : operation === 'passkey' ? store.loginWithPasskey()
      : operation === 'register' ? store.register({ ...credentials, username: 'administrator' })
      : operation === 'oauth' ? store.setToken('oauth-token') : store.refreshUser()
    const rejected = expect(stale).rejects.toBe(failure)
    authAPI.login.mockResolvedValueOnce({ ...response, access_token: 'new-token', user: { ...user, id: 8 } })
    await store.login(credentials)
    fail(failure)
    await rejected

    expect(store.user?.id).toBe(8)
    expect(store.isAuthenticated).toBe(true)
    expect(localStorage.getItem('auth_token')).toBe('new-token')
  })

  it.each([401, 423])('rejects a delayed %s from every replaced login session without global effects', async status => {
    const store = useAuthStore()
    await store.login(credentials)
    const listener = vi.fn()
    window.addEventListener('admin-compliance-required', listener)
    try {
      const transitions = [
        () => store.login(credentials),
        () => store.login2FA('temporary', '123456'),
        () => store.loginWithPasskey(),
        () => store.register({ ...credentials, username: 'administrator' }),
        () => store.setToken('oauth-token'),
        async () => { await store.logout(); await store.login(credentials) },
        async () => {
          authAPI.login.mockResolvedValueOnce({ ...response, access_token: 'token-8', user: { ...user, id: 8 } })
          await store.login(credentials)
          await store.login(credentials)
        },
        () => store.logout()
      ]
      for (const transition of transitions) {
        let finish!: () => void
        let started!: () => void
        const dispatched = new Promise<void>(resolve => { started = resolve })
        const adapter = vi.fn().mockImplementationOnce(config => new Promise((_resolve, reject) => {
          finish = () => reject({ config, response: { status, data: { code: status === 423 ? 'ADMIN_COMPLIANCE_ACK_REQUIRED' : 'UNAUTHORIZED' } } })
          started()
        })).mockResolvedValue({ status: 200, data: { code: 0, data: {} }, headers: {}, config: {} })
        apiClient.defaults.adapter = adapter
        const request = apiClient.post('/announcements/1/read')
        const rejected = expect(request).rejects.toMatchObject({ status })
        await dispatched
        await transition()
        const currentToken = localStorage.getItem('auth_token')
        finish()
        await rejected

        expect(adapter).toHaveBeenCalledTimes(1)
        expect(axios.post).not.toHaveBeenCalled()
        expect(listener).not.toHaveBeenCalled()
        expect(localStorage.getItem('auth_token')).toBe(currentToken)
      }
    } finally { window.removeEventListener('admin-compliance-required', listener) }
  })

  it.each([401, 423])('keeps same-session token and profile refresh behavior for %s', async status => {
    const store = useAuthStore()
    await store.login(credentials)
    const listener = vi.fn()
    window.addEventListener('admin-compliance-required', listener)
    let finish!: () => void
    let started!: () => void
    const dispatched = new Promise<void>(resolve => { started = resolve })
    const adapter = vi.fn().mockImplementationOnce(config => new Promise((_resolve, reject) => {
      finish = () => reject({ config, response: { status, data: { code: status === 423 ? 'ADMIN_COMPLIANCE_ACK_REQUIRED' : 'UNAUTHORIZED' } } })
      started()
    })).mockResolvedValue({ status: 200, data: { code: 0, data: {} }, headers: {}, config: {} })
    apiClient.defaults.adapter = adapter
    try {
      const request = apiClient.post('/announcements/1/read')
      const result = status === 401 ? expect(request).resolves.toMatchObject({ status: 200 }) : expect(request).rejects.toMatchObject({ status: 423 })
      await dispatched
      localStorage.setItem('auth_token', 'refreshed-token')
      authAPI.getCurrentUser.mockResolvedValueOnce({ data: { ...user, username: 'refreshed profile' } })
      await store.refreshUser()
      finish()
      await result

      expect(adapter).toHaveBeenCalledTimes(status === 401 ? 2 : 1)
      if (status === 401) expect(adapter.mock.calls[1][0].headers.Authorization).toBe('Bearer refreshed-token')
      expect(listener).toHaveBeenCalledTimes(status === 423 ? 1 : 0)
    } finally { window.removeEventListener('admin-compliance-required', listener) }
  })

  it('does not retry an old operation after the same user logs in during token refresh', async () => {
    const store = useAuthStore()
    await store.login(credentials)
    let finish!: (value: unknown) => void
    let started!: () => void
    const refreshing = new Promise<void>(resolve => { started = resolve })
    vi.mocked(axios.post).mockImplementationOnce(() => new Promise(resolve => {
      finish = resolve
      started()
    }))
    const adapter = vi.fn().mockImplementationOnce(config => Promise.reject({ config, response: { status: 401, data: {} } }))
      .mockResolvedValue({ status: 200, data: { code: 0, data: {} }, headers: {}, config: {} })
    apiClient.defaults.adapter = adapter
    const request = apiClient.post('/announcements/1/read')
    const rejected = expect(request).rejects.toMatchObject({ status: 401, code: 'AUTH_SESSION_CHANGED' })
    await refreshing
    await store.login(credentials)
    finish({ data: { code: 0, data: { access_token: 'old-refresh-result', refresh_token: 'old-refresh-result-token', expires_in: 3600 } } })
    await rejected

    expect(adapter).toHaveBeenCalledTimes(1)
    expect(localStorage.getItem('auth_token')).toBe('token-7')
  })
})
