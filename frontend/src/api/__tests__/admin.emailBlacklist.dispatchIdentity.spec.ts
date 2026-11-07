import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import { getRegistrationEmailBlacklist, updateRegistrationEmailBlacklist } from '../admin/settings'
import { findBlacklistedEmailUsers, batchDisable } from '../admin/users'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => ({
  status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data: {} }
}))
const operations = [
  { name: 'load', start: () => getRegistrationEmailBlacklist() },
  { name: 'save', start: () => updateRegistrationEmailBlacklist(['user@example.com']) },
  { name: 'find users', start: () => findBlacklistedEmailUsers(['user@example.com']) },
  { name: 'batch disable', start: () => batchDisable([12]) },
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

describe.each(operations)('registration blacklist $name request ownership', operation => {
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
