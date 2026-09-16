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

  it.each(['manual', 'auto'] as const)('submits and retries %s operations when randomUUID is unavailable', async (kind) => {
    vi.stubGlobal('crypto', {})
    try {
      const store = useSubscriptionStore()
      const request = kind === 'manual' ? api.resetDailyQuota : api.setAutoDailyReset
      request.mockRejectedValueOnce({ code: 'NETWORK_ERROR' }).mockResolvedValueOnce(completed())
      api.getDailyResetOperation.mockRejectedValueOnce({ status: 404 })

      await (kind === 'manual'
        ? store.resetDailyQuota(subscription())
        : store.setAutoDailyReset(subscription(), true))

      expect(request).toHaveBeenCalledTimes(2)
      expect(request.mock.calls[0][2]).toEqual(expect.any(String))
      expect(request.mock.calls[0][2].length).toBeGreaterThanOrEqual(8)
      expect(request.mock.calls[1]).toEqual(request.mock.calls[0])
      expect(store.pendingDailyResets[1]).toBeUndefined()
    } finally {
      vi.unstubAllGlobals()
    }
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

  it('can disable automatic reset when session storage cannot be written', async () => {
    const store = useSubscriptionStore()
    const enabled = subscription()
    enabled.daily_reset!.auto_daily_reset_enabled = true
    const disabled = subscription(1, 11)
    api.setAutoDailyReset.mockResolvedValue({
      subscription: disabled, preference_saved: true, reset_performed: false
    })
    const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('Storage quota exceeded', 'QuotaExceededError')
    })
    try {
      await store.setAutoDailyReset(enabled, false)
      expect(api.setAutoDailyReset).toHaveBeenCalledWith(1, {
        expected_version: 10, expected_date: '2026-09-06', enabled: false
      }, expect.any(String))
      expect(store.subscriptions[0].daily_reset!.auto_daily_reset_enabled).toBe(false)
      expect(store.pendingDailyResets[1]).toBeUndefined()
    } finally {
      storage.mockRestore()
    }
  })

  it.each(['manual', 'enable'] as const)('does not submit a %s operation without durable recovery state', async (kind) => {
    const store = useSubscriptionStore()
    const storageError = new DOMException('Storage quota exceeded', 'QuotaExceededError')
    const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw storageError
    })
    try {
      const operation = kind === 'manual'
        ? store.resetDailyQuota(subscription())
        : store.setAutoDailyReset(subscription(), true)
      await expect(operation).rejects.toBe(storageError)
      expect(api.resetDailyQuota).not.toHaveBeenCalled()
      expect(api.setAutoDailyReset).not.toHaveBeenCalled()
    } finally {
      storage.mockRestore()
    }
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

  it.each(['list', 'active'] as const)('keeps the latest %s usage when server timestamps differ within a millisecond', async (source) => {
    const store = useSubscriptionStore()
    const newer = subscription()
    newer.daily_usage_usd = 70
    newer.daily_reset!.server_time = '2026-09-06T01:00:00.100002Z'
    const older = subscription()
    older.daily_reset!.server_time = '2026-09-06T09:00:00.100001+08:00'
    const read = source === 'list' ? api.getMySubscriptions : api.getActiveSubscriptions
    const fetch = () => source === 'list' ? store.fetchSubscriptions() : store.fetchActiveSubscriptions(true)
    read.mockResolvedValueOnce([newer]).mockResolvedValueOnce([older])

    await fetch()
    await fetch()

    const current = source === 'list' ? store.subscriptions : store.activeSubscriptions
    expect(current[0].daily_usage_usd).toBe(70)
  })

  it('does not switch automatic preference while a manual reset for the same card is unresolved', async () => {
    const store = useSubscriptionStore()
    let finish!: (value: ReturnType<typeof completed>) => void
    api.resetDailyQuota.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const manual = store.resetDailyQuota(subscription())

    await store.setAutoDailyReset(subscription(), true)
    expect(api.setAutoDailyReset).not.toHaveBeenCalled()
    finish(completed())
    await manual

    api.setAutoDailyReset.mockResolvedValue({ ...completed(), preference_saved: true, reset_performed: false })
    await store.setAutoDailyReset(store.subscriptions[0], true)
    expect(api.setAutoDailyReset).toHaveBeenCalledWith(1,
      expect.objectContaining({ expected_version: 11, enabled: true }), expect.any(String))
  })

  it.each(['RESET_WEEKLY_LIMIT', 'RESET_MONTHLY_LIMIT'])(
    'refreshes authoritative state after %s rejects a stale manual reset', async (reason) => {
      const store = useSubscriptionStore()
      const limited = subscription(1, 12)
      limited.daily_reset!.can_reset = false
      limited.daily_reset!.auto_daily_reset_enabled = true
      limited.daily_reset!.reason = reason
      api.resetDailyQuota.mockRejectedValue({ status: 400, reason })
      api.getDailyResetState.mockResolvedValue(limited)

      await expect(store.resetDailyQuota(subscription())).rejects.toMatchObject({ reason })

      expect(api.resetDailyQuota).toHaveBeenCalledTimes(1)
      expect(store.subscriptions[0].daily_reset).toEqual(limited.daily_reset)
      expect(store.pendingDailyResets).toEqual({})
      expect(sessionStorage.length).toBe(0)
    }
  )

  it('does not restore another user\'s uncertain operation from session storage', async () => {
    sessionStorage.setItem('subscription-daily-reset:7:1', JSON.stringify({
      user_id: 8, subscription_id: 1, operation_id: 'another-user-operation',
      expected_version: 10, expected_date: '2026-09-06', kind: 'manual', status: 'checking'
    }))
    const store = useSubscriptionStore()
    await store.fetchSubscriptions()
    await store.recoverDailyReset(1)

    expect(store.pendingDailyResets).toEqual({})
    expect(api.getDailyResetOperation).not.toHaveBeenCalled()
    expect(api.resetDailyQuota).not.toHaveBeenCalled()
  })
})
