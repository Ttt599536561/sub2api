import { defineComponent } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SubscriptionsView from '../SubscriptionsView.vue'
import type { UserSubscription } from '@/types'

const api = vi.hoisted(() => ({
  getMySubscriptions: vi.fn(), getActiveSubscriptions: vi.fn(), resetDailyQuota: vi.fn(),
  setAutoDailyReset: vi.fn(), getDailyResetState: vi.fn(), getDailyResetOperation: vi.fn()
}))
const feedback = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/subscriptions', () => ({ default: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => feedback }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => key === 'userSubscriptions.dailyResetCount'
      ? `${params?.count}/${params?.limit}` : key
  })
}))

function subscription(id = 1, canReset = true): UserSubscription {
  return {
    id, user_id: 7, group_id: id, status: 'active', starts_at: '2026-09-01T00:00:00Z',
    expires_at: '2026-10-01T00:00:00Z', created_at: '', updated_at: '',
    daily_usage_usd: 0.00000001, weekly_usage_usd: 700, monthly_usage_usd: 700,
    daily_window_start: null, weekly_window_start: null, monthly_window_start: null,
    group: { id, name: `Monthly ${id}`, platform: 'openai', daily_limit_usd: 100, weekly_limit_usd: 700 } as UserSubscription['group'],
    daily_reset: {
      eligible: true, can_reset: canReset, auto_daily_reset_enabled: true,
      today_reset_count: 100, daily_reset_limit: 100, daily_reset_version: 10,
      server_date: '2026-09-06', server_time: '2026-09-06T01:00:00Z', reason: 'RESET_WEEKLY_LIMIT'
    }
  }
}

function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(SubscriptionsView, { global: { plugins: [pinia], stubs: {
    AppLayout: defineComponent({ template: '<main><slot /></main>' }), Icon: true
  } } })
}

describe('subscription daily reset controls', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    sessionStorage.clear()
  })

  it('shows controls only for eligible subscriptions and trusts can_reset despite rounded usage', async () => {
    const ineligible = subscription(2)
    ineligible.daily_reset!.eligible = false
    ineligible.daily_reset!.auto_daily_reset_enabled = false
    api.getMySubscriptions.mockResolvedValue([subscription(), ineligible])
    const wrapper = mountView()
    await flushPromises()
    const buttons = wrapper.findAll('[data-testid="reset-daily"]')
    expect(buttons).toHaveLength(1)
    expect(buttons[0].attributes('disabled')).toBeUndefined()
    expect(wrapper.findAll('[role="switch"]')).toHaveLength(1)
    wrapper.unmount()
  })

  it('allows turning off an existing preference after the group reset permission is revoked', async () => {
    const revoked = subscription(1, false)
    revoked.daily_reset!.eligible = false
    api.getMySubscriptions.mockResolvedValue([revoked])
    api.setAutoDailyReset.mockResolvedValue({
      operation_id: 'auto-1', replayed: false,
      subscription: { ...revoked, daily_reset: { ...revoked.daily_reset, auto_daily_reset_enabled: false } },
      reset_performed: false, preference_saved: true
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="reset-daily"]').exists()).toBe(false)
    expect(wrapper.find('[role="switch"]').exists()).toBe(true)
    await wrapper.get('[role="switch"]').trigger('click')
    await flushPromises()

    expect(api.setAutoDailyReset).toHaveBeenCalledWith(1, expect.objectContaining({ enabled: false }), expect.any(String))
    expect(wrapper.find('[role="switch"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each(['RESET_WEEKLY_LIMIT', 'RESET_MONTHLY_LIMIT', 'RESET_DAILY_COUNT_LIMIT', 'RESET_INSUFFICIENT_VALIDITY'])(
    'keeps automatic reset on for %s without pause reasons and still allows disabling it', async (reason) => {
    const limited = subscription(1, false)
    limited.daily_reset!.reason = reason
    api.getMySubscriptions.mockResolvedValue([limited])
    api.setAutoDailyReset.mockResolvedValue({
      operation_id: 'auto-1', replayed: false, subscription: subscription(1, false),
      reset_performed: false, preference_saved: true
    })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="reset-daily"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('100/100')
    expect(wrapper.html()).not.toContain(reason)
    await wrapper.get('[data-testid="reset-daily"]').trigger('click')
    expect(api.resetDailyQuota).not.toHaveBeenCalled()
    await wrapper.get('[role="switch"]').trigger('click')
    await flushPromises()
    expect(api.setAutoDailyReset).toHaveBeenCalledWith(1, expect.objectContaining({ enabled: false }), expect.any(String))
    wrapper.unmount()
  })

  it('submits directly without confirmation and disables only the pending card', async () => {
    api.getMySubscriptions.mockResolvedValue([subscription(), subscription(2)])
    let resolve!: (value: unknown) => void
    api.resetDailyQuota.mockImplementation(() => new Promise(done => { resolve = done }))
    const confirm = vi.spyOn(window, 'confirm')
    const wrapper = mountView()
    await flushPromises()
    const buttons = wrapper.findAll('[data-testid="reset-daily"]')
    await buttons[0].trigger('click')
    await buttons[0].trigger('click')
    expect(api.resetDailyQuota).toHaveBeenCalledTimes(1)
    expect(confirm).not.toHaveBeenCalled()
    expect(buttons[0].attributes('disabled')).toBeDefined()
    expect(buttons[1].attributes('disabled')).toBeUndefined()
    resolve({ operation_id: 'op-1', replayed: false, subscription: subscription(), reset_performed: true })
    await flushPromises()
    wrapper.unmount()
    confirm.mockRestore()
  })

  it('shows verification after a network failure without enabling a second reset', async () => {
    api.getMySubscriptions.mockResolvedValue([subscription()])
    api.resetDailyQuota.mockRejectedValue({ code: 'NETWORK_ERROR' })
    api.getDailyResetOperation.mockRejectedValue({ code: 'NETWORK_ERROR' })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="reset-daily"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('userSubscriptions.verifyingReset')
    expect(wrapper.get('[data-testid="reset-daily"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="verify-daily-reset"]').exists()).toBe(true)
    expect(api.resetDailyQuota).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('keeps an unresolved operation visible when the group reset permission is revoked', async () => {
    const revoked = subscription(1, false)
    Object.assign(revoked.daily_reset!, { eligible: false, auto_daily_reset_enabled: false })
    sessionStorage.setItem('subscription-daily-reset:7:1', JSON.stringify({
      user_id: 7, subscription_id: 1, operation_id: 'pending-before-revocation',
      expected_version: 10, expected_date: '2026-09-06', kind: 'manual', status: 'checking'
    }))
    api.getMySubscriptions.mockResolvedValue([revoked])
    api.getDailyResetOperation.mockRejectedValue({ code: 'NETWORK_ERROR' })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="verify-daily-reset"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('userSubscriptions.verifyingReset')
    expect(api.getDailyResetOperation).toHaveBeenCalledWith(1, 'pending-before-revocation')
    expect(api.resetDailyQuota).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
