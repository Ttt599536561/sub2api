import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { authAPI as persistingAuthAPI } from '@/api/auth'

const transportPost = vi.hoisted(() => vi.fn())
vi.mock('@/api/client', () => ({
  apiClient: { post: transportPost },
  ownedAuthRequestConfig: () => ({ authIdentity: { sessionID: localStorage.getItem('auth_session_id'), userID: null } }),
}))

const api = vi.hoisted(() => ({
  login: vi.fn(), login2FA: vi.fn(), register: vi.fn(), getCurrentUser: vi.fn(), logout: vi.fn(), refreshToken: vi.fn(),
}))
const passkeyAPI = vi.hoisted(() => ({ login: vi.fn() }))
vi.mock('@/api', () => ({ authAPI: api, passkeyAPI, isTotp2FARequired: () => false }))
const credentials = { email: 'review@example.com', password: 'test-only-password' }
const oldResponse = {
  access_token: 'old-login-token', refresh_token: 'old-refresh', expires_in: 3600,
  user: { id: 7, username: 'old-user', role: 'user' },
}
const newResponse = {
  access_token: 'replacement-login-token', refresh_token: 'replacement-refresh', expires_in: 3600,
  user: { id: 8, username: 'replacement-user', role: 'user' },
}
const flows = [
  { name: 'password login', request: () => api.login, start: (store: ReturnType<typeof useAuthStore>) => store.login(credentials) },
  { name: '2FA login', request: () => api.login2FA, start: (store: ReturnType<typeof useAuthStore>) => store.login2FA('old-temp-token', '123456') },
  { name: 'passkey login', request: () => passkeyAPI.login, start: (store: ReturnType<typeof useAuthStore>) => store.loginWithPasskey() },
  { name: 'registration', request: () => api.register, start: (store: ReturnType<typeof useAuthStore>) => store.register(credentials) },
]

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  vi.useFakeTimers()
  vi.resetAllMocks()
  api.login.mockResolvedValue(newResponse)
})
afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
  localStorage.clear()
})

describe.each(flows)('delayed $name after a replacement login', flow => {
  it.each([503, 429, 0, 'success'] as const)('preserves the new account when the old attempt finishes with %s', async outcome => {
    const store = useAuthStore()
    let finish!: (value: unknown) => void
    let fail!: (reason: unknown) => void
    flow.request().mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject }))
    const oldAttempt = flow.start(store).then(value => ({ value }), error => ({ error }))
    await store.login(credentials)
    const replacementSession = localStorage.getItem('auth_session_id')
    if (outcome === 'success') finish(oldResponse)
    else fail({ status: outcome, code: outcome === 0 ? 'ERR_NETWORK' : 'SERVER_REJECTION' })
    const result = await oldAttempt

    expect(store.token).toBe(newResponse.access_token)
    expect(store.user?.id).toBe(8)
    expect(localStorage.getItem('auth_token')).toBe(newResponse.access_token)
    expect(localStorage.getItem('refresh_token')).toBe(newResponse.refresh_token)
    expect(localStorage.getItem('auth_session_id')).toBe(replacementSession)
    if (outcome === 'success') expect(result).toMatchObject({ error: { code: 'AUTH_SESSION_CHANGED' } })
  })
})

it('still clears the original session after a definitive current login failure', async () => {
  const store = useAuthStore()
  await store.login(credentials)
  api.login.mockRejectedValueOnce({ status: 503 })
  await expect(store.login(credentials)).rejects.toMatchObject({ status: 503 })
  expect(store.token).toBeNull()
  expect(localStorage.getItem('auth_token')).toBeNull()
})

it.each([
  { name: 'password login', start: () => persistingAuthAPI.login(credentials) },
  { name: '2FA login', start: () => persistingAuthAPI.login2FA({ temp_token: 'temp', totp_code: '123456' }) },
  { name: 'registration', start: () => persistingAuthAPI.register(credentials) },
])('guards the real $name API before it persists a stale successful response', async flow => {
  localStorage.setItem('auth_session_id', 'initial-session')
  let finish!: (value: unknown) => void
  transportPost.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const oldAttempt = flow.start().then(value => ({ value }), error => ({ error }))
  localStorage.setItem('auth_session_id', 'replacement-session')
  localStorage.setItem('auth_user', JSON.stringify(newResponse.user))
  localStorage.setItem('auth_token', newResponse.access_token)
  localStorage.setItem('refresh_token', newResponse.refresh_token)
  finish({ data: oldResponse })
  const result = await oldAttempt

  expect(localStorage.getItem('auth_token')).toBe(newResponse.access_token)
  expect(localStorage.getItem('refresh_token')).toBe(newResponse.refresh_token)
  expect(JSON.parse(localStorage.getItem('auth_user')!).id).toBe(8)
  expect(result).toMatchObject({ error: { code: 'AUTH_SESSION_CHANGED' } })
})
it.each(flows)('also fences a delayed $name success after the same user logs in again', async flow => {
  const store = useAuthStore()
  let finish!: (value: unknown) => void
  flow.request().mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const oldAttempt = flow.start(store).catch(error => error)
  api.login.mockResolvedValueOnce({ ...newResponse, user: { ...newResponse.user, id: 7 } })
  await store.login(credentials)
  finish(oldResponse)
  await expect(oldAttempt).resolves.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
  expect(store.user?.id).toBe(7)
  expect(store.token).toBe(newResponse.access_token)
})

it.each([
  { name: 'password login', start: () => persistingAuthAPI.login(credentials) },
  { name: '2FA login', start: () => persistingAuthAPI.login2FA({ temp_token: 'temp', totp_code: '123456' }) },
  { name: 'registration', start: () => persistingAuthAPI.register(credentials) },
])('still persists a current successful real $name API response', async flow => {
  transportPost.mockResolvedValueOnce({ data: oldResponse })
  await expect(flow.start()).resolves.toEqual(oldResponse)
  expect(localStorage.getItem('auth_token')).toBe(oldResponse.access_token)
  expect(localStorage.getItem('refresh_token')).toBe(oldResponse.refresh_token)
  expect(JSON.parse(localStorage.getItem('auth_user')!).id).toBe(7)
})
it('clears partial auth when the current successful login cannot persist its token', async () => {
  const store = useAuthStore()
  const storageError = new DOMException('Storage is full', 'QuotaExceededError')
  const original = Storage.prototype.setItem
  const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, key, value) {
    if (key === 'auth_token') throw storageError
    return original.call(this, key, value)
  })
  try {
    await expect(store.login(credentials)).rejects.toBe(storageError)
    expect(store.token).toBeNull()
    expect(store.user).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
  } finally { storage.mockRestore() }
})
