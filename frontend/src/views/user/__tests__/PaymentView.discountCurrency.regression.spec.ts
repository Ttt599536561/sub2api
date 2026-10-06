import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import PaymentView from '../PaymentView.vue'
import AmountInput from '@/components/payment/AmountInput.vue'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import type { CheckoutInfoResponse, MethodLimit } from '@/types/payment'

const getCheckoutInfo = vi.hoisted(() => vi.fn())
vi.mock('vue-router', async () => ({
  ...await vi.importActual<typeof import('vue-router')>('vue-router'),
  useRoute: () => ({ path: '/purchase', query: {} }),
  useRouter: () => ({ replace: vi.fn(), push: vi.fn(), resolve: vi.fn() })
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } })
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7, username: 'test', balance: 0 }, refreshUser: vi.fn() }) }))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ createOrder: vi.fn() }) }))
vi.mock('@/stores/subscriptions', () => ({ useSubscriptionStore: () => ({ activeSubscriptions: [], fetchActiveSubscriptions: vi.fn().mockResolvedValue([]) }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn(), showInfo: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { getCheckoutInfo } }))

async function mountPayment(options: { alipay?: Partial<MethodLimit>; stripe?: Partial<MethodLimit>; fee?: number; discount?: boolean } = {}) {
  const method: MethodLimit = { daily_limit: 0, daily_used: 0, daily_remaining: 0, single_min: 0, single_max: 0, fee_rate: 0, available: true }
  const checkout: CheckoutInfoResponse = {
    methods: {
      alipay: { ...method, currency: 'CNY', ...options.alipay },
      stripe: { ...method, currency: 'JPY', ...options.stripe }
    },
    global_min: 0, global_max: 0, plans: [], balance_disabled: false,
    balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0,
    recharge_fee_rate: options.fee ?? 0, recharge_bonus_mode: 'discount',
    recharge_bonus_tiers: options.discount === false ? [] : [{ min_amount: 0, bonus_percent: 15 }],
    help_text: '', help_image_url: '', stripe_publishable_key: ''
  }
  getCheckoutInfo.mockResolvedValue({ data: checkout })
  const wrapper = shallowMount(PaymentView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Teleport: true, Transition: false } }
  })
  await flushPromises()
  return wrapper
}

describe('discounted recharge method currency limits', () => {
  afterEach(() => localStorage.clear())

  it.each([
    { title: 'keeps the current CNY method and enables the alternative JPY method', max: 0, selected: 'alipay' },
    { title: 'switches to JPY when only its rounded amount fits', max: 85, selected: 'stripe' }
  ])('$title', async ({ max, selected }) => {
    const wrapper = await mountPayment({ alipay: { single_max: max }, stripe: { single_min: 86, single_max: 86 } })
    try {
      expect(wrapper.getComponent(PaymentMethodSelector).props('selected')).toBe('alipay')
      wrapper.getComponent(AmountInput).vm.$emit('update:modelValue', 101)
      await flushPromises()

      // CNY charges 85.85; the backend rounds the JPY quote to 86, so a
      // JPY minimum of 86 must still allow that alternative method.
      const methods = wrapper.getComponent(PaymentMethodSelector).props('methods')
      expect(methods.find(item => item.type === 'stripe')).toMatchObject({ available: true })
      expect(wrapper.getComponent(PaymentMethodSelector).props('selected')).toBe(selected)
      expect(wrapper.text()).not.toContain('payment.amountNoMethod')
    } finally {
      wrapper.unmount()
    }
  })

  it('allows switching back to a CNY method when the selected JPY quote exceeds its ceiling', async () => {
    const wrapper = await mountPayment({ alipay: { single_min: 85.85, single_max: 85.85 } })
    try {
      wrapper.getComponent(PaymentMethodSelector).vm.$emit('select', 'stripe')
      wrapper.getComponent(AmountInput).vm.$emit('update:modelValue', 101)
      await flushPromises()
      const selector = wrapper.getComponent(PaymentMethodSelector)
      expect(selector.props('selected')).toBe('stripe')
      expect(selector.props('methods').find(item => item.type === 'alipay')).toMatchObject({ available: true })
      selector.vm.$emit('select', 'alipay')
      await flushPromises()
      expect(selector.props('selected')).toBe('alipay')
    } finally {
      wrapper.unmount()
    }
  })

  it('rejects a JPY method below its rounded amount without changing the CNY selection', async () => {
    const wrapper = await mountPayment({ alipay: { single_max: 85.85 }, stripe: { single_max: 85 } })
    try {
      wrapper.getComponent(AmountInput).vm.$emit('update:modelValue', 101)
      await flushPromises()
      const selector = wrapper.getComponent(PaymentMethodSelector)
      expect(selector.props('methods').find(item => item.type === 'stripe')).toMatchObject({ available: false })
      expect(selector.props('selected')).toBe('alipay')
    } finally {
      wrapper.unmount()
    }
  })

  it.each([true, false])('uses each currency fee precision and gateway limit with discount=%s', async discount => {
    const wrapper = await mountPayment({
      alipay: { single_min: discount ? 88 : 102.5, single_max: discount ? 88 : 102.5 },
      stripe: { single_min: discount ? 89 : 103, single_max: discount ? 89 : 103 },
      fee: 2.5, discount
    })
    try {
      wrapper.getComponent(AmountInput).vm.$emit('update:modelValue', discount ? 101 : 100)
      await flushPromises()
      const selector = wrapper.getComponent(PaymentMethodSelector)
      expect(selector.props('methods').every(item => item.available)).toBe(true)
      expect(wrapper.text()).not.toContain('payment.amountNoMethod')
      selector.vm.$emit('select', 'stripe')
      await flushPromises()
      expect(selector.props('selected')).toBe('stripe')
      expect(wrapper.text()).toContain(discount ? '¥89' : '¥103')
    } finally {
      wrapper.unmount()
    }
  })

  it.each([
    { currency: 'CNY', rate: 7, input: 1, total: 1.07, discount: false },
    { currency: 'CNY', rate: 14, input: 1, total: 1.14, discount: false },
    { currency: 'CNY', rate: 7, input: 1.18, total: 1.07, discount: true },
    { currency: 'CNY', rate: 14, input: 1.18, total: 1.14, discount: true },
    { currency: 'JPY', rate: 7, input: 100, total: 107, discount: false },
    { currency: 'JPY', rate: 14, input: 118, total: 114, discount: true },
    { currency: 'CNY', rate: 7.001, input: 1, total: 1.08, discount: false },
    { currency: 'CNY', rate: 7.001, input: 1.18, total: 1.08, discount: true },
    { currency: 'JPY', rate: 7.001, input: 100, total: 108, discount: false },
    { currency: 'IQD', rate: 1.23, input: 1, total: 1.013, discount: false },
    { currency: 'CLF', rate: 1.23, input: 1, total: 1.02, discount: false },
    { currency: 'CNY', rate: 7.000000000001, input: 1, total: 1.08, discount: false },
    { currency: 'CNY', rate: 7.000000000000001, input: 1, total: 1.07, discount: false }
  ])('keeps the exact fee boundary for $currency, $rate%, discount=$discount', async ({ currency, rate, input, total, discount }) => {
    const wrapper = await mountPayment({
      alipay: { currency, single_min: total, single_max: total },
      stripe: { available: false }, fee: rate, discount
    })
    try {
      wrapper.getComponent(AmountInput).vm.$emit('update:modelValue', input)
      await flushPromises()
      const selector = wrapper.getComponent(PaymentMethodSelector)
      expect(selector.props('methods').find(item => item.type === 'alipay')).toMatchObject({ available: true })
      expect(selector.props('selected')).toBe('alipay')
      expect(wrapper.text()).not.toContain('payment.amountNoMethod')
      expect(wrapper.text()).toContain(formatPaymentAmount(total, currency, 'en'))
    } finally {
      wrapper.unmount()
    }
  })
})
