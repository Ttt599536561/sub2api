import { describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { welfareAPI } from '../welfare'
vi.mock('../client', () => ({ apiClient: { post: vi.fn().mockResolvedValue({ data: {} }), get: vi.fn().mockResolvedValue({ data: {} }) } }))
describe('welfare transport', () => {
  it('keeps exact decimal strings and a stable idempotency header for redemption', async () => {
    const signal = new AbortController().signal
    await welfareAPI.redeem({ amount: '9999999.01', welfare_balance_version: 7 }, 'retry-key', signal)
    expect(apiClient.post).toHaveBeenCalledWith('/user/welfare/redeem', { amount: '9999999.01', welfare_balance_version: 7 }, expect.objectContaining({ headers: { 'Idempotency-Key': 'retry-key' }, signal }))
  })
  it('uses a quote for all funds rather than computing a client transfer amount', async () => {
    await welfareAPI.quote({ mode: 'all' })
    expect(apiClient.post).toHaveBeenCalledWith('/user/welfare/redemption-quote', { mode: 'all' }, expect.objectContaining({ signal: undefined }))
  })
})
