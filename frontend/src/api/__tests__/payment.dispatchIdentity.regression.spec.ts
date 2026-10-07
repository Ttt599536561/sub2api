import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '../client'
import { paymentAPI } from '../payment'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => ({
  status: 200, statusText: 'OK', headers: {}, config,
  data: { code: 0, data: { order_id: 71, amount: 10, pay_amount: 10 } },
}))
const payload = { amount: 10, payment_type: 'wxpay', order_type: 'balance' as const }

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

describe('payment order ownership at real Axios dispatch', () => {
  it.each([
    { transition: 'another account', userID: 8 },
    { transition: 'a new login of the same account', userID: 7 },
  ])('does not send the old order after switching to $transition before dispatch', async ({ userID }) => {
    const request = paymentAPI.createOrder(payload)
    // Axios schedules its interceptor on a microtask. A second tab can replace
    // shared auth storage after the old buyer clicks, before transport dispatch.
    localStorage.setItem('auth_session_id', 'replacement-session')
    localStorage.setItem('auth_user', JSON.stringify({ id: userID }))
    localStorage.setItem('auth_token', 'replacement-token')
    const outcome = await request.then(response => ({ response }), error => ({ error }))

    // If this fails, the diff exposes the actual credential sent to the adapter.
    expect(adapter.mock.calls.map(([config]) => config.headers.get('Authorization'))).toEqual([])
    expect(outcome).toMatchObject({ error: { code: 'ERR_CANCELED' } })
    expect(localStorage.getItem('auth_token')).toBe('replacement-token')
  })

  it('keeps public signed-token recovery independent of the browser login session', async () => {
    const request = paymentAPI.resolveOrderPublicByResumeToken('signed-original-order')
    localStorage.setItem('auth_session_id', 'replacement-session')
    localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
    await expect(request).resolves.toMatchObject({ status: 200 })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(adapter.mock.calls[0][0].url).toBe('/payment/public/orders/resolve')
    expect(JSON.parse(adapter.mock.calls[0][0].data)).toEqual({ resume_token: 'signed-original-order' })
  })

  it('allows access token rotation within the original buyer session', async () => {
    const request = paymentAPI.createOrder(payload)
    localStorage.setItem('auth_token', 'rotated-token')
    await expect(request).resolves.toMatchObject({ data: { order_id: 71 } })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(adapter.mock.calls[0][0].headers.get('Authorization')).toBe('Bearer rotated-token')
  })
})
