import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import { welfareAPI } from '../welfare'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))

const originalAdapter = apiClient.defaults.adapter
const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => ({
  status: 200,
  statusText: 'OK',
  headers: {},
  config,
  data: { code: 0, data: { status: 'completed' } }
}))
const redemption = { amount: '9999999.01', welfare_balance_version: 7 }
const operations = [
  {
    name: 'draw',
    url: '/user/welfare/draw',
    body: {},
    send: (signal: AbortSignal) => welfareAPI.draw('stable-operation-key', signal)
  },
  {
    name: 'redemption',
    url: '/user/welfare/redeem',
    body: redemption,
    send: (signal: AbortSignal) => welfareAPI.redeem(redemption, 'stable-operation-key', signal)
  }
]

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('auth_session_id', 'original-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
  localStorage.setItem('auth_token', 'original-token')
  adapter.mockClear()
  apiClient.defaults.adapter = adapter
})

afterEach(() => {
  apiClient.defaults.adapter = originalAdapter
  localStorage.clear()
})

describe.each(operations)('welfare $name through the Axios transport', operation => {
  it.each([
    { transition: 'another account', userID: 8 },
    { transition: 'a new login for the same account', userID: 7 }
  ])('cancels before dispatch after switching to $transition', async ({ userID }) => {
    const request = operation.send(new AbortController().signal)

    // Axios queues the request interceptor: change ownership after the API has
    // captured it, before allowing any request interceptor microtask to run.
    localStorage.setItem('auth_session_id', 'replacement-session')
    localStorage.setItem('auth_user', JSON.stringify({ id: userID }))
    localStorage.setItem('auth_token', 'replacement-token')

    await expect(request).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    expect(adapter).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('replacement-token')
  })

  it.each([false, true])('preserves the owned request and idempotency key with token rotation=%s', async rotateToken => {
    const signal = new AbortController().signal
    const request = operation.send(signal)
    if (rotateToken) localStorage.setItem('auth_token', 'rotated-token')

    await expect(request).resolves.toEqual({ status: 'completed' })
    expect(adapter).toHaveBeenCalledTimes(1)
    const config = adapter.mock.calls[0][0]
    expect(config).toMatchObject({
      method: 'post',
      url: operation.url,
      welfareIdentity: { sessionID: 'original-session', userID: 7 }
    })
    expect(config.signal).toBe(signal)
    expect(config.headers.get('Idempotency-Key')).toBe('stable-operation-key')
    expect(config.headers.get('Authorization')).toBe(`Bearer ${rotateToken ? 'rotated-token' : 'original-token'}`)
    expect(JSON.parse(config.data)).toEqual(operation.body)
  })
})
