import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import { authAPI } from '../auth'
import { passkeyAPI } from '../passkey'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const credentialGet = vi.fn()
const originalCredentials = Object.getOwnPropertyDescriptor(navigator, 'credentials')
const begin = { session_token: 'old-ceremony', options: { publicKey: { challenge: 'AQID', rpId: 'example.com' } } }
const issued = { access_token: 'issued-token', refresh_token: 'issued-refresh', expires_in: 3600, user: { id: 7 } }
class TestCredential {
  id = 'credential'; rawId = new Uint8Array([1]).buffer; type = 'public-key'; authenticatorAttachment = 'platform'
  response = { authenticatorData: new Uint8Array([2]).buffer, clientDataJSON: new Uint8Array([3]).buffer, signature: new Uint8Array([4]).buffer, userHandle: null }
  getClientExtensionResults() { return {} }
}
const success = (config: InternalAxiosRequestConfig, data: unknown) => ({ status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data } })
function unauthorized(config: InternalAxiosRequestConfig): never {
  throw new AxiosError('Old credential rejected', 'ERR_BAD_REQUEST', config, undefined, {
    status: 401, statusText: 'Unauthorized', headers: {}, config, data: { code: 401, message: 'Old credential rejected' },
  })
}
const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => success(config, issued))
function replaceSession() {
  localStorage.setItem('auth_session_id', 'replacement-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
  localStorage.setItem('auth_token', 'replacement-token')
}
const loginFlows = [
  { name: 'login', start: () => authAPI.login({ email: 'old@example.com', password: 'old-password' }) },
  { name: '2FA', start: () => authAPI.login2FA({ temp_token: 'old-temp', totp_code: '123456' }) },
  { name: 'registration', start: () => authAPI.register({ email: 'old@example.com', password: 'old-password' }) },
]

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('auth_session_id', 'original-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
  localStorage.setItem('auth_token', 'original-token')
  window.history.replaceState({}, '', '/login')
  adapter.mockReset().mockImplementation(async config => unauthorized(config))
  apiClient.defaults.adapter = adapter
  credentialGet.mockReset().mockResolvedValue(new TestCredential())
  vi.stubGlobal('PublicKeyCredential', TestCredential)
  Object.defineProperty(navigator, 'credentials', { configurable: true, value: { get: credentialGet } })
})
afterEach(() => {
  apiClient.defaults.adapter = originalAdapter
  localStorage.clear()
  vi.unstubAllGlobals()
  if (originalCredentials) Object.defineProperty(navigator, 'credentials', originalCredentials)
  else delete (navigator as { credentials?: CredentialsContainer }).credentials
})

describe.each(loginFlows)('$name ownership before a real Axios 401', flow => {
  it('cancels before dispatch so an old credential failure cannot clear the replacement account', async () => {
    const request = flow.start().catch(error => error)
    replaceSession()
    const result = await request
    expect(adapter.mock.calls.map(([config]) => config.headers.get('Authorization'))).toEqual([])
    expect(localStorage.getItem('auth_token')).toBe('replacement-token')
    expect(result).toMatchObject({ code: 'ERR_CANCELED' })
  })
  it('allows a token rotation in the same login session', async () => {
    adapter.mockImplementation(async config => success(config, issued))
    const request = flow.start()
    localStorage.setItem('auth_token', 'rotated-token')
    await expect(request).resolves.toEqual(issued)
    expect(adapter.mock.calls[0][0].headers.get('Authorization')).toBe('Bearer rotated-token')
  })
})

describe('passkey ceremony ownership', () => {
  it('cancels the begin request before dispatch after a replacement login', async () => {
    const request = passkeyAPI.login().catch(error => error)
    replaceSession()
    await request
    expect(adapter.mock.calls.map(([config]) => config.headers.get('Authorization'))).toEqual([])
    expect(credentialGet).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('replacement-token')
  })

  it.each(['begin', 'ceremony'] as const)('keeps the original owner when the session changes during %s', async stage => {
    let release!: () => void
    let reached!: () => void
    const waiting = new Promise<void>(resolve => { reached = resolve })
    adapter.mockImplementation(async config => {
      if (config.url?.endsWith('/begin')) {
        if (stage === 'begin') await new Promise<void>(resolve => { release = resolve; reached() })
        return success(config, begin)
      }
      return unauthorized(config)
    })
    if (stage === 'ceremony') credentialGet.mockImplementationOnce(async () => {
      await new Promise<void>(resolve => { release = resolve; reached() })
      return new TestCredential()
    })
    const request = passkeyAPI.login().catch(error => error)
    await waiting
    replaceSession()
    release()
    await request

    expect(adapter.mock.calls.map(([config]) => [config.url, config.headers.get('Authorization')])).toEqual([
      ['/auth/passkey/login/begin', 'Bearer original-token'],
    ])
    if (stage === 'begin') expect(credentialGet).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('replacement-token')
  })

  it('finishes a normal ceremony after same-session access token rotation', async () => {
    adapter.mockImplementation(async config => success(config, config.url?.endsWith('/begin') ? begin : issued))
    credentialGet.mockImplementationOnce(async () => {
      localStorage.setItem('auth_token', 'rotated-token')
      return new TestCredential()
    })
    await expect(passkeyAPI.login()).resolves.toEqual(issued)
    expect(adapter.mock.calls.map(([config]) => config.headers.get('Authorization'))).toEqual(['Bearer original-token', 'Bearer rotated-token'])
  })
})
