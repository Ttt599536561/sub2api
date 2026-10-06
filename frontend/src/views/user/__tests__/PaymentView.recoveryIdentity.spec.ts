import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import PaymentView from '../PaymentView.vue'
import AmountInput from '@/components/payment/AmountInput.vue'
import PaymentStatusPanel from '@/components/payment/PaymentStatusPanel.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import { paymentAPI } from '@/api/payment'

const createOrder = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const routeState = vi.hoisted(() => ({ path: '/purchase', query: {} as Record<string, string> }))
vi.mock('vue-router', async () => ({
  ...await vi.importActual<typeof import('vue-router')>('vue-router'),
  useRoute: () => routeState,
  useRouter: () => ({ replace: vi.fn(), push: routerPush, resolve: vi.fn() }),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7, username: 'buyer', balance: 0 }, refreshUser: vi.fn() }) }))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ createOrder }) }))
vi.mock('@/stores/subscriptions', () => ({ useSubscriptionStore: () => ({ activeSubscriptions: [], fetchActiveSubscriptions: vi.fn().mockResolvedValue([]) }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError, showInfo: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/utils/device', () => ({ isMobileDevice: () => true }))
vi.mock('@/api/payment', () => ({ paymentAPI: { getCheckoutInfo: vi.fn().mockResolvedValue({ data: {
  methods: { wxpay: { daily_limit: 0, daily_used: 0, daily_remaining: 0, single_min: 0, single_max: 0, fee_rate: 0, available: true, currency: 'CNY' } },
  global_min: 0, global_max: 0, plans: [], balance_disabled: false,
  balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0,
  help_text: '', help_image_url: '', stripe_publishable_key: '',
} }) } }))

function signIn(userId: number, session: string) {
  localStorage.setItem('auth_user', JSON.stringify({ id: userId }))
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_token', `token-${session}`)
}

const orderResult = {
  order_id: 71, amount: 10, pay_amount: 10, fee_rate: 0,
  expires_at: '2099-01-01T00:00:00Z', payment_type: 'wxpay',
  qr_code: 'weixin://buyer-seven-order', out_trade_no: 'trade-71', resume_token: 'resume-71',
}
const wrappers: ReturnType<typeof shallowMount>[] = []
async function mountPayment() {
  const wrapper = shallowMount(PaymentView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Teleport: true, Transition: false } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
async function startOrder(wrapper: Awaited<ReturnType<typeof mountPayment>>) {
  wrapper.getComponent(AmountInput).vm.$emit('update:modelValue', 10)
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text().includes('payment.createOrder'))!.trigger('click')
  await flushPromises()
}

describe('PaymentView recovery ownership', () => {
  beforeEach(() => {
    localStorage.clear()
    signIn(7, 'session-seven')
    createOrder.mockReset().mockResolvedValue(orderResult)
    routerPush.mockReset()
    showError.mockReset()
    routeState.query = {}
    ;(window as Window & { WeixinJSBridge?: unknown }).WeixinJSBridge = undefined
  })
  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    localStorage.clear()
    ;(window as Window & { WeixinJSBridge?: unknown }).WeixinJSBridge = undefined
  })

  it.each([false, true])('restores the same buyer after a reload with token rotation=%s', async rotateToken => {
    const original = await mountPayment()
    await startOrder(original)
    expect(original.getComponent(PaymentStatusPanel).props('qrCode')).toBe(orderResult.qr_code)
    original.unmount()
    if (rotateToken) localStorage.setItem('auth_token', 'refreshed-access-token')

    const restored = await mountPayment()
    expect(restored.getComponent(PaymentStatusPanel).props('orderId')).toBe(71)
    expect(restored.getComponent(PaymentStatusPanel).props('qrCode')).toBe(orderResult.qr_code)
  })

  it('does not restore the previous buyer payment when another account opens purchase', async () => {
    const original = await mountPayment()
    await startOrder(original)
    original.unmount()
    signIn(8, 'session-eight')

    const nextBuyer = await mountPayment()
    expect(nextBuyer.findComponent(PaymentStatusPanel).exists()).toBe(false)
    expect(nextBuyer.findComponent(AmountInput).exists()).toBe(true)
  })

  it('does not launch or persist a late order response after the login session changes', async () => {
    let finish!: (result: typeof orderResult) => void
    createOrder.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = await mountPayment()
    await startOrder(wrapper)
    signIn(8, 'session-eight')
    finish(orderResult)
    await flushPromises()

    expect(wrapper.findComponent(PaymentStatusPanel).exists()).toBe(false)
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('removes a displayed payment when another tab replaces the login session', async () => {
    const wrapper = await mountPayment()
    await startOrder(wrapper)
    signIn(8, 'session-eight')
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, 'another-buyers-new-snapshot')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    await flushPromises()

    expect(wrapper.findComponent(PaymentStatusPanel).exists()).toBe(false)
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBe('another-buyers-new-snapshot')
  })

  it.each(['success', 'failure'] as const)('keeps the new buyer submit locked when the old order finishes with %s', async outcome => {
    let finish!: (result: typeof orderResult) => void
    let fail!: (reason: unknown) => void
    let finishNext!: (result: typeof orderResult) => void
    createOrder.mockReturnValueOnce(new Promise((resolve, reject) => { finish = resolve; fail = reject }))
      .mockReturnValueOnce(new Promise(resolve => { finishNext = resolve }))
    const wrapper = await mountPayment()
    await startOrder(wrapper)
    signIn(8, 'session-eight')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    await startOrder(wrapper)
    expect(createOrder).toHaveBeenCalledTimes(2)
    if (outcome === 'success') finish(orderResult)
    else fail({ reason: 'PAYMENT_GATEWAY_ERROR' })
    await flushPromises()

    const submit = wrapper.findAll('button').find(button => button.text().includes('common.processing'))!
    expect(submit?.attributes('disabled')).toBeDefined()
    expect(showError).not.toHaveBeenCalled()
    expect(createOrder).toHaveBeenCalledTimes(2)
    finishNext({ ...orderResult, order_id: 81 })
    await flushPromises()
    expect(wrapper.getComponent(PaymentStatusPanel).props('orderId')).toBe(81)
  })

  it('ignores a fallback error after switching accounts', async () => {
    let fail!: (reason: unknown) => void
    createOrder.mockRejectedValueOnce({ reason: 'PAYMENT_GATEWAY_ERROR' })
      .mockReturnValueOnce(new Promise((_resolve, reject) => { fail = reject }))
    const wrapper = await mountPayment()
    await startOrder(wrapper)
    expect(createOrder).toHaveBeenCalledTimes(2)
    signIn(8, 'session-eight')
    fail({ reason: 'PAYMENT_GATEWAY_ERROR' })
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.findComponent(PaymentStatusPanel).exists()).toBe(false)
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('does not invoke a delayed WeChat bridge after the login session changes', async () => {
    createOrder.mockResolvedValueOnce({ ...orderResult, result_type: 'jsapi_ready', jsapi: {
      appId: 'wx7', timeStamp: '1712345678', nonceStr: 'nonce', package: 'prepay_id=wx7', signType: 'RSA', paySign: 'signed',
    } })
    const wrapper = await mountPayment()
    await startOrder(wrapper)
    signIn(8, 'session-eight')
    const invoke = vi.fn((_action, _payload, callback) => callback({ err_msg: 'get_brand_wcpay_request:ok' }))
    ;(window as Window & { WeixinJSBridge?: unknown }).WeixinJSBridge = { invoke }
    document.dispatchEvent(new Event('WeixinJSBridgeReady'))
    await flushPromises()

    expect(invoke).not.toHaveBeenCalled()
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('does not resume an old WeChat route after checkout info returns to a different session', async () => {
    const checkout = await paymentAPI.getCheckoutInfo()
    let finish!: (value: typeof checkout) => void
    vi.mocked(paymentAPI.getCheckoutInfo).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    routeState.query = { wechat_resume: '1', wechat_resume_token: 'buyer-seven-resume' }
    await mountPayment()
    signIn(8, 'session-eight')
    finish(checkout)
    await flushPromises()

    expect(createOrder).not.toHaveBeenCalled()
  })
})
