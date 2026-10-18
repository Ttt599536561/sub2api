import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { useAuthStore } from '@/stores/auth'

enableAutoUnmount(afterEach)
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerPush = vi.hoisted(() => vi.fn())
const getOrder = vi.hoisted(() => vi.fn())
const paymentStore = vi.hoisted(() => ({
  config: { stripe_publishable_key: 'pk_test' } as { stripe_publishable_key?: string },
  fetchConfig: vi.fn(),
  pollOrderStatus: vi.fn(),
}))
const loadStripe = vi.hoisted(() => vi.fn())
const stripeElements = vi.hoisted(() => ({
  create: vi.fn(),
}))
const stripePaymentElement = vi.hoisted(() => ({
  mount: vi.fn(),
  on: vi.fn(),
  destroy: vi.fn(),
}))
const stripeInstance = vi.hoisted(() => ({
  elements: vi.fn(),
  confirmPayment: vi.fn(),
  confirmAlipayPayment: vi.fn(),
  confirmWechatPayPayment: vi.fn(),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({ push: routerPush }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'zh-CN' },
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => paymentStore,
}))

vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  const state = reactive({ user: { id: 7 }, sessionRevision: 'initial' })
  return { useAuthStore: () => state }
})

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getOrder,
  },
}))

vi.mock('@stripe/stripe-js/pure', () => ({
  loadStripe,
}))

import StripePaymentView from '../StripePaymentView.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import {
  writePaymentRecoverySnapshot,
  type PaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import type { PaymentOrder } from '@/types/payment'

function orderFactory(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 42,
    user_id: 7,
    amount: 100,
    pay_amount: 103,
    currency: 'CNY',
    fee_rate: 0.03,
    payment_type: 'stripe',
    out_trade_no: 'sub2_stripe_42',
    status: 'PENDING',
    order_type: 'balance',
    created_at: '2026-04-20T12:00:00Z',
    expires_at: '2026-04-20T12:30:00Z',
    refund_amount: 0,
    ...overrides,
  }
}

function stripeSnapshot(overrides: Partial<PaymentRecoverySnapshot> = {}): PaymentRecoverySnapshot {
  return {
    orderId: 42,
    amount: 100,
    qrCode: '',
    expiresAt: '2099-01-01T00:30:00.000Z',
    paymentType: 'stripe',
    payUrl: '/payment/stripe?order_id=42',
    outTradeNo: 'sub2_stripe_42',
    clientSecret: 'pi_secret_42',
    intentId: '',
    currency: 'CNY',
    countryCode: '',
    paymentEnv: '',
    payAmount: 103,
    orderType: 'balance',
    paymentMode: '',
    resumeToken: '',
    createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    ...overrides,
  }
}

function writeSnapshot(overrides: Partial<PaymentRecoverySnapshot> = {}) {
  writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot(overrides))
}

const auth = useAuthStore() as unknown as { user: { id: number }; sessionRevision: string }
function login(session: string, userId = 7, notify = true) {
  localStorage.setItem('auth_user', JSON.stringify({ id: userId }))
  localStorage.setItem('auth_session_id', session)
  if (notify) {
    auth.user.id = userId
    auth.sessionRevision = session
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}

const wechatQRResult = () => ({ paymentIntent: {
  status: 'requires_action', next_action: { wechat_pay_display_qr_code: { image_data_url: 'data:image/png;base64,test' } }
} })

function mountView() {
  return shallowMount(StripePaymentView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })
}

describe('StripePaymentView', () => {
  beforeEach(() => {
    routeState.query = {
      order_id: '42',
    }
    routerPush.mockReset()
    getOrder.mockReset().mockResolvedValue({ data: orderFactory() })
    paymentStore.config = { stripe_publishable_key: 'pk_test' }
    paymentStore.fetchConfig.mockReset().mockResolvedValue(undefined)
    paymentStore.pollOrderStatus.mockReset()
    loadStripe.mockReset().mockResolvedValue(stripeInstance)
    stripeElements.create.mockReset().mockReturnValue(stripePaymentElement)
    stripePaymentElement.mount.mockReset()
    stripePaymentElement.destroy.mockReset()
    stripePaymentElement.on.mockReset().mockImplementation((event: string, callback: () => void) => {
      if (event === 'ready') callback()
    })
    stripeInstance.elements.mockReset().mockReturnValue(stripeElements)
    stripeInstance.confirmPayment.mockReset()
    stripeInstance.confirmAlipayPayment.mockReset().mockResolvedValue({})
    stripeInstance.confirmWechatPayPayment.mockReset()
    window.localStorage.clear()
    login('initial')
    writeSnapshot()
  })

  it('uses the client secret from the local snapshot of the same order', async () => {
    routeState.query.method = 'alipay'
    routeState.query.client_secret = 'pi_untrusted_url_secret'
    mountView()
    await flushPromises()
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith(42)
    expect(stripeInstance.confirmAlipayPayment).toHaveBeenCalledWith('pi_secret_42', expect.any(Object))
  })

  it('ignores a client secret supplied in the URL query', async () => {
    window.localStorage.clear()
    routeState.query = { order_id: '42', client_secret: 'pi_other_secret', method: 'alipay' }

    const wrapper = mountView()
    await flushPromises()

    expect(getOrder).not.toHaveBeenCalled()
    expect(loadStripe).not.toHaveBeenCalled()
    expect(stripeInstance.confirmAlipayPayment).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.stripeMissingParams')
  })

  it('rejects a snapshot that belongs to a different order', async () => {
    writeSnapshot({ orderId: 7, clientSecret: 'pi_secret_7' })
    routeState.query = { order_id: '42', client_secret: 'pi_secret_42', method: 'alipay' }

    const wrapper = mountView()
    await flushPromises()

    expect(getOrder).not.toHaveBeenCalled()
    expect(stripeInstance.confirmAlipayPayment).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.stripeMissingParams')
  })

  it('rejects an implicit snapshot owned by an earlier login', async () => {
    login('replacement')
    const wrapper = mountView()
    await flushPromises()
    expect(getOrder).not.toHaveBeenCalled()
    expect(loadStripe).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.stripeMissingParams')
  })

  it.each(['order', 'config', 'sdk'])('stops initialization after a same-user login changes during %s loading', async stage => {
    routeState.query.method = 'alipay'
    const pending = deferred<unknown>()
    if (stage === 'order') getOrder.mockReturnValueOnce(pending.promise)
    else if (stage === 'config') paymentStore.fetchConfig.mockReturnValueOnce(pending.promise)
    else loadStripe.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    login('replacement')
    pending.resolve(stage === 'order' ? { data: orderFactory() } : stage === 'sdk' ? stripeInstance : undefined)
    await flushPromises()
    await flushPromises()
    expect(stripeInstance.confirmAlipayPayment).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('payment.actualPay')
    if (stage === 'order') expect(paymentStore.fetchConfig).not.toHaveBeenCalled()
    if (stage !== 'sdk') expect(loadStripe).not.toHaveBeenCalled()
  })

  it('clears mounted payment details and destroys the payment element on another-tab login', async () => {
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    expect(wrapper.find('#stripe-payment-element').exists()).toBe(true)
    login('replacement', 8, false)
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    await flushPromises()
    expect(wrapper.find('#stripe-payment-element').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('payment.actualPay')
    expect(stripePaymentElement.destroy).toHaveBeenCalledOnce()
  })

  it('discards a WeChat QR result after the login session changes', async () => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    const pending = deferred<unknown>()
    stripeInstance.confirmWechatPayPayment.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    login('replacement')
    pending.resolve(wechatQRResult())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.find('img[alt="WeChat Pay QR"]').exists()).toBe(false)
    expect(paymentStore.pollOrderStatus).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['success', 'error'])('discards a generic payment %s result after changing accounts', async outcome => {
    vi.useFakeTimers()
    const pending = deferred<unknown>()
    stripeInstance.confirmPayment.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    await wrapper.get('.btn-stripe').trigger('click')
    expect(stripeInstance.confirmPayment).toHaveBeenCalledOnce()
    login('replacement', 8)
    pending.resolve(outcome === 'success' ? {} : { error: { message: 'private old payment error' } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(wrapper.text()).not.toContain('private old payment error')
    expect(routerPush).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['before-poll', 'during-poll', 'before-redirect'])('stops polling and redirects after session change %s', async stage => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    stripeInstance.confirmWechatPayPayment.mockResolvedValueOnce(wechatQRResult())
    const pending = deferred<PaymentOrder>()
    paymentStore.pollOrderStatus.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    if (stage !== 'before-poll') await vi.advanceTimersByTimeAsync(3000)
    if (stage === 'before-redirect') {
      pending.resolve(orderFactory({ status: 'PAID' }))
      await flushPromises()
    }
    // The callback must check storage even before a storage event is delivered.
    login('replacement', 8, false)
    if (stage === 'during-poll') pending.resolve(orderFactory({ status: 'PAID' }))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(paymentStore.pollOrderStatus).toHaveBeenCalledTimes(stage === 'before-poll' ? 0 : 1)
    expect(routerPush).not.toHaveBeenCalled()
    expect(wrapper.find('img[alt="WeChat Pay QR"]').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['alipay', 'wechat_pay', ''])('does not start %s payment after leaving during SDK loading', async (method) => {
    routeState.query.method = method
    let resolveSDK!: (value: typeof stripeInstance) => void
    loadStripe.mockReturnValueOnce(new Promise((resolve) => { resolveSDK = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    expect(loadStripe).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    resolveSDK(stripeInstance)
    await flushPromises()
    await flushPromises()
    expect(stripeInstance.confirmAlipayPayment).not.toHaveBeenCalled()
    expect(stripeInstance.confirmWechatPayPayment).not.toHaveBeenCalled()
    expect(stripeInstance.elements).not.toHaveBeenCalled()
  })

  it('does not restart polling when a WeChat QR result arrives after leaving', async () => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    let resolvePayment!: (value: unknown) => void
    stripeInstance.confirmWechatPayPayment.mockReturnValueOnce(new Promise((resolve) => { resolvePayment = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    expect(stripeInstance.confirmWechatPayPayment).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    resolvePayment({ paymentIntent: { status: 'requires_action', next_action: {
      wechat_pay_display_qr_code: { image_data_url: 'data:image/png;base64,test' }
    } } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(paymentStore.pollOrderStatus).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([false, true])('handles a completed poll with page unmounted=%s', async (unmounted) => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    stripeInstance.confirmWechatPayPayment.mockResolvedValueOnce({ paymentIntent: {
      status: 'requires_action', next_action: { wechat_pay_display_qr_code: { image_data_url: 'data:image/png;base64,test' } }
    } })
    let resolvePoll!: (value: PaymentOrder) => void
    paymentStore.pollOrderStatus.mockReturnValueOnce(new Promise((resolve) => { resolvePoll = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(paymentStore.pollOrderStatus).toHaveBeenCalledTimes(1)
    if (unmounted) wrapper.unmount()
    resolvePoll(orderFactory({ status: 'PAID' }))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(routerPush).toHaveBeenCalledTimes(unmounted ? 0 : 1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('本地恢复快照未记录币种时使用订单接口返回的 Stripe 币种展示金额', async () => {
    writeSnapshot({ currency: '' })
    getOrder.mockResolvedValue({
      data: orderFactory({ currency: 'HKD', pay_amount: 103 }),
    })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith(42)
    expect(loadStripe).toHaveBeenCalledWith('pk_test')
    expect(wrapper.text()).toContain(formatPaymentAmount(103, 'HKD', 'zh-CN'))
  })
})
