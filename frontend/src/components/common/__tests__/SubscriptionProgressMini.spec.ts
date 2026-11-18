import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SubscriptionProgressMini from '../SubscriptionProgressMini.vue'
const store = vi.hoisted(() => ({ activeSubscriptions: [] as unknown[], hasActiveSubscriptions: true, fetchActiveSubscriptions: vi.fn().mockResolvedValue(undefined) }))
vi.mock('@/stores', () => ({ useSubscriptionStore: () => store }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { subscription: 'subscription' }, isFeatureFlagEnabled: () => true }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date(2026, 8, 22, 12)) })
afterEach(() => vi.useRealTimers())
describe('subscription expiry calendar labels', () => {
  it.each(['welfare', 'standard'])('shows a zero monthly total correctly for %s', async kind => {
    store.activeSubscriptions = [{ id: 1, group_id: 1, entitlement_kind: kind, monthly_usage_usd: 7,
      group: { name: 'Plan', daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: 0 } }]
    const wrapper = mount(SubscriptionProgressMini, { global: { stubs: { Icon: true, RouterLink: true } } })
    await wrapper.get('button').trigger('click')
    if (kind === 'welfare') {
      expect(wrapper.text()).toContain('userSubscriptions.welfareQuotaDisabled')
      expect(wrapper.text()).toContain('$7.00/$0.00')
      expect(wrapper.text()).not.toContain('subscriptionProgress.unlimited')
    } else {
      expect(wrapper.text()).toContain('subscriptionProgress.unlimited')
      expect(wrapper.text()).not.toContain('userSubscriptions.welfareQuotaDisabled')
    }
  })

  it.each([
    { kind: 'standard', monthlyLimit: 1000, monthlyUsed: 80, supplement: 200, expected: '$1120.00' },
    { kind: 'welfare', monthlyLimit: 0, monthlyUsed: 7, supplement: 12.34, expected: '$12.34' },
    { kind: 'welfare', monthlyLimit: 0, monthlyUsed: 0, supplement: 0, expected: '$0.00' },
  ])('shows one monthly balance including supplement for $kind ($expected)', async ({ kind, monthlyLimit, monthlyUsed, supplement, expected }) => {
    store.activeSubscriptions = [{
      id: 1,
      group_id: 1,
      entitlement_kind: kind,
      status: 'active',
      starts_at: new Date(2026, 8, 20, 12).toISOString(),
      expires_at: new Date(2026, 8, 25, 12).toISOString(),
      daily_usage_usd: 3,
      monthly_usage_usd: monthlyUsed,
      supplement_granted_usd: 250,
      supplement_used_usd: 250 - supplement,
      supplement_remaining_usd: supplement,
      group: { name: 'Plan', daily_limit_usd: 10, weekly_limit_usd: null, monthly_limit_usd: monthlyLimit },
    }]
    const wrapper = mount(SubscriptionProgressMini, { global: { stubs: { Icon: true, RouterLink: true } } })
    await wrapper.get('button').trigger('click')

    const text = wrapper.text()
    expect(text).toContain('welfareRedeem.monthlyRemainingWithSupplement')
    expect(text).toContain(expected)
    expect(text).not.toContain('subscriptionProgress.monthly')
    expect(text).not.toContain(`$${monthlyUsed.toFixed(2)}/$${monthlyLimit.toFixed(2)}`)
    expect(text).not.toContain('userSubscriptions.welfareQuotaDisabled')
    expect(text).toContain('$3.00/$10.00')
  })

  it.each([
    [new Date(2026, 8, 22, 18), 'expiresToday'],
    [new Date(2026, 8, 23, 18), 'expiresTomorrow'],
    [new Date(2026, 8, 22, 12), 'expired'],
    [new Date(2026, 8, 25, 12), 'daysRemaining'],
  ])('labels %s as %s', async (expires, label) => {
    store.activeSubscriptions = [{ id: 1, group_id: 1, expires_at: expires.toISOString(), group: { name: 'Plan' } }]
    const w = mount(SubscriptionProgressMini, { global: { stubs: { Icon: true, RouterLink: true } } })
    await w.get('button').trigger('click')
    expect(w.text()).toContain('subscriptionProgress.' + label)
  })
})
