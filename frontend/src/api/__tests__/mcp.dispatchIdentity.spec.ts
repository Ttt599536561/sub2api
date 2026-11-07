import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import { mcpAPI as api, type MCPAuthorization } from '../mcp'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => ({
  status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data: {} }
}))
const authorization: MCPAuthorization = { response_type: 'code', client_id: 'client', redirect_uri: 'https://client.example/callback', code_challenge: 'a'.repeat(43), code_challenge_method: 'S256', resource: 'https://api.example/mcp' }
const operations = [
  { name: 'config', start: () => api.getConnectionConfig() },
  { name: 'grants', start: () => api.listGrants() },
  { name: 'revoke', start: () => api.revokeGrant('grant-1') },
  { name: 'preview', start: () => api.previewAuthorization(authorization) },
  { name: 'authorize', start: () => api.authorize({ authorization, approved: true, scope: 'admin', read_only: false }) },
]
beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('auth_session_id', 'original-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
  localStorage.setItem('auth_token', 'original-token')
  adapter.mockClear()
  apiClient.defaults.adapter = adapter
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter; localStorage.clear() })

describe.each(operations)('MCP $name request ownership', operation => {
  it('cancels before dispatch when the login changes in the Axios queue', async () => {
    const pending = operation.start().catch(error => error)
    localStorage.setItem('auth_session_id', 'replacement-session')
    localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
    localStorage.setItem('auth_token', 'replacement-token')
    expect(await pending).toMatchObject({ code: 'ERR_CANCELED' })
    expect(adapter).not.toHaveBeenCalled()
  })
  it.each([7, 8])('rejects a delayed success after a replacement login as user %s', async userID => {
    let release!: () => void
    let dispatched!: () => void
    const sent = new Promise<void>(resolve => { dispatched = resolve })
    adapter.mockImplementationOnce(async config => {
      dispatched()
      await new Promise<void>(resolve => { release = resolve })
      return { status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data: { key: 'old-session-secret' } } }
    })
    const pending = operation.start().catch(error => error)
    await sent
    localStorage.setItem('auth_session_id', 'replacement-session')
    localStorage.setItem('auth_user', JSON.stringify({ id: userID }))
    localStorage.setItem('auth_token', 'replacement-token')
    release()
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(adapter).toHaveBeenCalledTimes(1)
  })
  it('permits token rotation without changing the login owner', async () => {
    const pending = operation.start()
    localStorage.setItem('auth_token', 'rotated-token')
    await pending
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(adapter.mock.calls[0][0].headers.get('Authorization')).toBe('Bearer rotated-token')
  })
})
