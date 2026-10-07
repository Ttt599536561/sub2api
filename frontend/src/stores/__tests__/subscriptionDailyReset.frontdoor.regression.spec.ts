import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSubscriptionStore } from '@/stores/subscriptions'
import type { UserSubscription } from '@/types'

const api = vi.hoisted(() => ({
  getMySubscriptions: vi.fn(), getActiveSubscriptions: vi.fn(),
  resetDailyQuota: vi.fn(), setAutoDailyReset: vi.fn(),
  getDailyResetState: vi.fn(), getDailyResetOperation: vi.fn(),
}))
vi.mock('@/api/subscriptions', () => ({ default: api }))

function subscription(): UserSubscription {
  return {
    id: 1, user_id: 7, group_id: 1, status: 'active', starts_at: '2026-09-01T00:00:00Z',
    expires_at: '2026-11-01T00:00:00Z', created_at: '', updated_at: '',
    daily_usage_usd: 60, weekly_usage_usd: 160, monthly_usage_usd: 260,
    daily_window_start: null, weekly_window_start: null, monthly_window_start: null,
    daily_reset: {
      eligible: true, can_reset: true, auto_daily_reset_enabled: false,
      today_reset_count: 0, daily_reset_limit: 100, daily_reset_version: 10,
      server_date: '2026-10-07', server_time: '2026-10-07T08:00:00Z',
    },
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  sessionStorage.clear()
  vi.resetAllMocks()
  api.getDailyResetState.mockResolvedValue(subscription())
})

describe('uncertain daily reset and a rejected transport retry', () => {
  it.each([429, 401, 403])('retains the original operation when retry receives HTTP %i before the first POST commits', async status => {
    // The first transaction is still uncommitted when its HTTP connection fails.
    // Its event is not visible to the lookup; rejection of the retry does not
    // establish whether the original transaction will eventually commit.
    api.resetDailyQuota.mockRejectedValueOnce({ status: 0, code: 'ERR_NETWORK' })
      .mockRejectedValueOnce({ status, code: 'GATEWAY_REJECTION' })
    api.getDailyResetOperation.mockRejectedValueOnce({ status: 404 })
    const store = useSubscriptionStore()
    const result = await store.resetDailyQuota(subscription()).catch(error => ({ unexpectedRejection: error }))
    const firstOperationID = api.resetDailyQuota.mock.calls[0][2]

    expect(api.resetDailyQuota).toHaveBeenCalledTimes(2)
    expect(api.resetDailyQuota.mock.calls[1]).toEqual(api.resetDailyQuota.mock.calls[0])
    expect(store.pendingDailyResets[1]?.operation_id).toBe(firstOperationID)
    expect(store.pendingDailyResets[1]?.status).toBe('checking')
    expect(JSON.parse(sessionStorage.getItem('subscription-daily-reset:7:1')!)).toMatchObject({ operation_id: firstOperationID })
    expect(result).toBeNull()

    // Once the original transaction is visible, recover its completed event
    // instead of generating or posting a second operation.
    const updated = subscription()
    updated.daily_reset!.daily_reset_version = 11
    updated.daily_usage_usd = 0
    api.getDailyResetOperation.mockResolvedValueOnce({
      operation_id: firstOperationID, replayed: true, reset_performed: true, subscription: updated,
    })
    await expect(store.recoverDailyReset(1)).resolves.toMatchObject({ operation_id: firstOperationID, replayed: true })
    expect(api.getDailyResetOperation).toHaveBeenLastCalledWith(1, firstOperationID)
    expect(api.resetDailyQuota).toHaveBeenCalledTimes(2)
    expect(store.pendingDailyResets).toEqual({})
    expect(sessionStorage.getItem('subscription-daily-reset:7:1')).toBeNull()
  })

  it('settles an uncertain retry when the subscription service definitively rejects its original round', async () => {
    api.resetDailyQuota.mockRejectedValueOnce({ status: 0 }).mockRejectedValueOnce({ status: 409, reason: 'RESET_STATE_CHANGED' })
    api.getDailyResetOperation.mockRejectedValueOnce({ status: 404 })
    const store = useSubscriptionStore()
    await expect(store.resetDailyQuota(subscription())).rejects.toMatchObject({ reason: 'RESET_STATE_CHANGED' })
    expect(store.pendingDailyResets).toEqual({})
    expect(sessionStorage.getItem('subscription-daily-reset:7:1')).toBeNull()
  })

  it('clears a first request that definitively fails before an operation becomes uncertain', async () => {
    api.resetDailyQuota.mockRejectedValueOnce({ status: 429, code: 'RATE_LIMITED' })
    const store = useSubscriptionStore()
    await expect(store.resetDailyQuota(subscription())).rejects.toMatchObject({ status: 429 })
    expect(store.pendingDailyResets).toEqual({})
    expect(sessionStorage.getItem('subscription-daily-reset:7:1')).toBeNull()
  })
})
