import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSubscriptionStore } from '@/stores/subscriptions'
import type { UserSubscription } from '@/types'

const api = vi.hoisted(() => ({
  getMySubscriptions: vi.fn(),
  getActiveSubscriptions: vi.fn(),
  resetDailyQuota: vi.fn(),
  setAutoDailyReset: vi.fn(),
  getDailyResetState: vi.fn(),
  getDailyResetOperation: vi.fn()
}))
vi.mock('@/api/subscriptions', () => ({ default: api }))

function subscription(id = 1, version = 10): UserSubscription {
  return {
    id, user_id: 7, group_id: id, status: 'active', starts_at: '2026-09-01T00:00:00Z',
    expires_at: '2026-10-01T00:00:00Z', created_at: '', updated_at: '',
    daily_usage_usd: 60, weekly_usage_usd: 160, monthly_usage_usd: 260,
    daily_window_start: null, weekly_window_start: null, monthly_window_start: null,
    daily_reset: {
      eligible: true, can_reset: true, auto_daily_reset_enabled: false,
      today_reset_count: 0, daily_reset_limit: 100, daily_reset_version: version,
      server_date: '2026-09-06', server_time: '2026-09-06T01:00:00Z'
    }
  }
}

function completed(id = 1) {
  const updated = subscription(id, 11)
  updated.daily_usage_usd = 0
  updated.expires_at = '2026-09-30T00:00:00Z'
  updated.daily_reset!.today_reset_count = 1
  updated.daily_reset!.can_reset = false
  return { operation_id: 'server-operation', replayed: false, subscription: updated, reset_performed: true }
}

describe('subscription daily reset state', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    sessionStorage.clear()
    vi.resetAllMocks()
    api.getMySubscriptions.mockResolvedValue([subscription(), subscription(2)])
    api.getActiveSubscriptions.mockResolvedValue([subscription(), subscription(2)])
    api.getDailyResetState.mockResolvedValue(subscription())
  })

  it('deduplicates one card while another card can submit independently', async () => {
    const store = useSubscriptionStore()
    let resolveFirst!: (value: ReturnType<typeof completed>) => void
    api.resetDailyQuota.mockImplementation((id: number) => id === 1
      ? new Promise(resolve => { resolveFirst = resolve })
      : Promise.resolve(completed(2)))

    const first = store.resetDailyQuota(subscription())
    await store.resetDailyQuota(subscription())
    await store.resetDailyQuota(subscription(2))

    expect(api.resetDailyQuota).toHaveBeenCalledTimes(2)
    expect(store.pendingDailyResets[1]).toBeDefined()
    expect(store.pendingDailyResets[2]).toBeUndefined()
    expect(api.resetDailyQuota.mock.calls[0][1]).toEqual({ expected_version: 10, expected_date: '2026-09-06' })
    resolveFirst(completed())
    await first
    expect(store.pendingDailyResets[1]).toBeUndefined()
  })

  it('uses the same operation ID and original round after a lost response and missing lookup', async () => {
    const store = useSubscriptionStore()
    api.resetDailyQuota.mockRejectedValueOnce({ code: 'NETWORK_ERROR' }).mockResolvedValueOnce(completed())
    api.getDailyResetOperation.mockRejectedValueOnce({ status: 404 })

    await store.resetDailyQuota(subscription())

    expect(api.resetDailyQuota).toHaveBeenCalledTimes(2)
    expect(api.resetDailyQuota.mock.calls[1]).toEqual(api.resetDailyQuota.mock.calls[0])
    expect(api.getDailyResetOperation).toHaveBeenCalledWith(1, api.resetDailyQuota.mock.calls[0][2])
    expect(store.subscriptions[0].daily_usage_usd).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })

  it('restores an uncertain operation after remount and queries it before submitting anything new', async () => {
    const store = useSubscriptionStore()
    api.resetDailyQuota.mockRejectedValue({ code: 'NETWORK_ERROR' })
    api.getDailyResetOperation.mockRejectedValue({ code: 'NETWORK_ERROR' })
    await store.resetDailyQuota(subscription())
    const operationId = store.pendingDailyResets[1].operation_id
    expect(store.pendingDailyResets[1].status).toBe('checking')

    setActivePinia(createPinia())
    const remounted = useSubscriptionStore()
    await remounted.fetchSubscriptions()
    expect(remounted.pendingDailyResets[1].operation_id).toBe(operationId)
    api.getDailyResetOperation.mockResolvedValue(completed())
    await remounted.recoverDailyReset(1)

    expect(api.resetDailyQuota).toHaveBeenCalledTimes(1)
    expect(api.getDailyResetOperation).toHaveBeenLastCalledWith(1, operationId)
    expect(remounted.pendingDailyResets[1]).toBeUndefined()
  })

  it('keeps the server preference after an uncertain enable request observes a newer version', async () => {
    const store = useSubscriptionStore()
    api.setAutoDailyReset.mockRejectedValue({ code: 'NETWORK_ERROR' })
    api.getDailyResetState.mockResolvedValue(subscription(1, 12))

    await store.setAutoDailyReset(subscription(), true)

    expect(api.setAutoDailyReset).toHaveBeenCalledTimes(1)
    expect(store.subscriptions[0].daily_reset!.auto_daily_reset_enabled).toBe(false)
    expect(store.pendingDailyResets[1]).toBeUndefined()
  })

  it('allows disabling automatic reset when the server disallows manual reset', async () => {
    const store = useSubscriptionStore()
    const limited = subscription()
    limited.daily_reset = { ...limited.daily_reset!, can_reset: false, today_reset_count: 100, auto_daily_reset_enabled: true }
    api.setAutoDailyReset.mockResolvedValue({ ...completed(), preference_saved: true, reset_performed: false })

    await store.setAutoDailyReset(limited, false)

    expect(api.setAutoDailyReset).toHaveBeenCalledWith(1, {
      expected_version: 10, expected_date: '2026-09-06', enabled: false
    }, expect.any(String))
  })

  it('does not let old list or active polling responses overwrite a mutation', async () => {
    const store = useSubscriptionStore()
    let resolveList!: (value: UserSubscription[]) => void
    let resolveActive!: (value: UserSubscription[]) => void
    api.getMySubscriptions.mockImplementation(() => new Promise(resolve => { resolveList = resolve }))
    api.getActiveSubscriptions.mockImplementation(() => new Promise(resolve => { resolveActive = resolve }))
    const list = store.fetchSubscriptions()
    const active = store.fetchActiveSubscriptions(true)
    api.resetDailyQuota.mockResolvedValue(completed())
    await store.resetDailyQuota(subscription())

    resolveList([subscription()])
    resolveActive([subscription()])
    await Promise.all([list, active])

    for (const updated of [store.subscriptions[0], store.activeSubscriptions[0]]) {
      expect(updated.daily_usage_usd).toBe(0)
      expect(updated.expires_at).toBe('2026-09-30T00:00:00Z')
      expect(updated.daily_reset!.today_reset_count).toBe(1)
    }
  })

  it('rejects a server-disabled manual operation without making a request', async () => {
    const store = useSubscriptionStore()
    const limited = subscription()
    limited.daily_reset!.can_reset = false
    await store.resetDailyQuota(limited)
    expect(api.resetDailyQuota).not.toHaveBeenCalled()
  })

  it.each(['list', 'active'] as const)('keeps newer %s state when RFC3339 fractional precision changes', async (source) => {
    const store = useSubscriptionStore()
    const newer = subscription()
    newer.daily_usage_usd = 70
    newer.daily_reset!.server_time = '2026-09-06T01:00:00.1Z'
    const older = subscription()
    const read = source === 'list' ? api.getMySubscriptions : api.getActiveSubscriptions
    const fetch = () => source === 'list' ? store.fetchSubscriptions() : store.fetchActiveSubscriptions(true)
    read.mockResolvedValueOnce([newer]).mockResolvedValueOnce([older])

    await fetch()
    await fetch()

    const current = source === 'list' ? store.subscriptions : store.activeSubscriptions
    expect(current[0].daily_usage_usd).toBe(70)
  })
})
