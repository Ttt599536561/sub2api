import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import {
  exchangePendingOAuthCompletion, createPendingLinuxDoOAuthAccount, createPendingOIDCOAuthAccount,
  createPendingWeChatOAuthAccount, createPendingDingTalkOAuthAccount, persistOAuthTokenContext, postOAuthCompletion,
} from '../auth'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const issued = { access_token: 'old-issued-access', refresh_token: 'old-issued-refresh', expires_in: 3600 }
const flows = [
  { name: 'pending exchange', request: () => exchangePendingOAuthCompletion() },
  { name: 'linuxdo registration', request: () => createPendingLinuxDoOAuthAccount('invite') },
  { name: 'oidc registration', request: () => createPendingOIDCOAuthAccount('invite') },
  { name: 'wechat registration', request: () => createPendingWeChatOAuthAccount('invite') },
  { name: 'dingtalk registration', request: () => createPendingDingTalkOAuthAccount('invite') },
  { name: 'pending account creation', request: async () => (await postOAuthCompletion<typeof issued>('/auth/oauth/pending/create-account', { email: 'new@example.com', password: 'test-only' })).data },
  { name: 'pending account binding', request: async () => (await postOAuthCompletion<typeof issued>('/auth/oauth/pending/bind-login', { email: 'existing@example.com', password: 'test-only' })).data },
]
function replaceSession(userID = 8) {
  localStorage.setItem('auth_session_id', 'new-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: userID }))
  localStorage.setItem('auth_token', 'new-access')
  localStorage.setItem('refresh_token', 'new-refresh')
}
beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('auth_session_id', 'old-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
  localStorage.setItem('auth_token', 'old-access')
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })

describe.each(flows)('$name completion ownership', flow => {
  it.each([7, 8])('blocks delayed success before callback persistence after user %s logs in', async userID => {
    let finish!: () => void
    let started!: () => void
    const dispatched = new Promise<void>(resolve => { started = resolve })
    apiClient.defaults.adapter = (config: InternalAxiosRequestConfig) => new Promise(resolve => {
      finish = () => resolve({ status: 200, statusText: 'OK', config, headers: {}, data: { code: 0, data: issued } })
      started()
    })
    const setToken = vi.fn()
    const callback = vi.fn(async (completion: typeof issued) => {
      // Same ordering as OAuthCallbackView.finalizeTokenResponse and sibling callbacks.
      persistOAuthTokenContext(completion)
      await setToken(completion.access_token)
    })
    const result = flow.request().then(value => callback(value as typeof issued)).catch(error => error)
    await dispatched
    replaceSession(userID)
    finish()
    const error = await result
    expect(callback).not.toHaveBeenCalled()
    expect(setToken).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('new-access')
    expect(localStorage.getItem('refresh_token')).toBe('new-refresh')
    expect(error).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
  })

  it('still returns a current completion for token persistence', async () => {
    apiClient.defaults.adapter = async config => ({ status: 200, statusText: 'OK', config, headers: {}, data: { code: 0, data: issued } })
    const completion = await flow.request()
    expect(completion).toMatchObject(issued)
    persistOAuthTokenContext(completion)
    expect(localStorage.getItem('refresh_token')).toBe(issued.refresh_token)
  })

  it('does not dispatch an old completion request after a session replacement', async () => {
    const adapter = vi.fn(async config => ({ status: 200, statusText: 'OK', config, headers: {}, data: { code: 0, data: issued } }))
    apiClient.defaults.adapter = adapter
    const request = flow.request().catch(error => error)
    replaceSession()
    await request
    expect(adapter).not.toHaveBeenCalled()
  })
})
