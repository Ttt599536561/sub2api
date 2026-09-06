import { beforeEach, describe, expect, it, vi } from 'vitest'
import subscriptionsAPI from '../subscriptions'

const client = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn(), get: vi.fn() }))
vi.mock('../client', () => ({ apiClient: client }))

describe('subscription daily reset API', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    for (const method of Object.values(client)) method.mockResolvedValue({ data: { operation_id: 'op-1' } })
  })

  it('sends the observed round and idempotency key for manual reset', async () => {
    const request = { expected_version: 10, expected_date: '2026-09-06' }
    await subscriptionsAPI.resetDailyQuota(42, request, 'op-1')
    expect(client.post).toHaveBeenCalledWith('/subscriptions/42/reset-daily', request, { headers: { 'Idempotency-Key': 'op-1' } })
  })

  it('sets an explicit preference with the original version', async () => {
    const request = { expected_version: 10, expected_date: '2026-09-06', enabled: false }
    await subscriptionsAPI.setAutoDailyReset(42, request, 'op-2')
    expect(client.put).toHaveBeenCalledWith('/subscriptions/42/auto-daily-reset', request, { headers: { 'Idempotency-Key': 'op-2' } })
  })

  it('encodes the operation ID and reads authoritative state', async () => {
    await subscriptionsAPI.getDailyResetOperation(42, 'op/1')
    await subscriptionsAPI.getDailyResetState(42)
    expect(client.get).toHaveBeenCalledWith('/subscriptions/42/daily-reset-operations/op%2F1')
    expect(client.get).toHaveBeenCalledWith('/subscriptions/42/daily-reset-state')
  })
})
