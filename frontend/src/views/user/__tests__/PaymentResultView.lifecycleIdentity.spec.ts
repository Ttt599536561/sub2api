import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PaymentResultView from '../PaymentResultView.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'

const api = vi.hoisted(() => ({ resolve: vi.fn(), poll: vi.fn(), verify: vi.fn(), verifyPublic: vi.fn(), refreshUser: vi.fn() }))
const route = { query: { resume_token: 'token-A' } as Record<string, string> }
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ pollOrderStatus: api.poll }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser: api.refreshUser }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { resolveOrderPublicByResumeToken: api.resolve, verifyOrder: api.verify, verifyOrderPublic: api.verifyPublic } }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
function snapshot(overrides: Record<string, unknown> = {}) {
  return { orderId: 42, amount: 88, qrCode: '', expiresAt: '2099-01-01T00:00:00Z', paymentType: 'alipay', payUrl: '', outTradeNo: 'order-A', clientSecret: '', intentId: '', currency: 'CNY', countryCode: '', paymentEnv: '', payAmount: 88, orderType: 'balance', paymentMode: '', resumeToken: 'token-A', createdAt: 0, owner: { userId: 7, sessionId: 'session-A' }, ...overrides }
}
function response(status: string) {
  return { data: { out_trade_no: 'order-A', status, paid: status === 'COMPLETED', created_at: '', expires_at: '' } }
}
let wrapper: VueWrapper | undefined
function start() { wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } }); return wrapper }
beforeEach(() => {
  vi.resetAllMocks(); vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }); localStorage.clear()
  route.query = { resume_token: 'token-A' }
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
  localStorage.setItem('auth_session_id', 'session-A')
  localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify(snapshot()))
  api.refreshUser.mockResolvedValue({})
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.useRealTimers(); localStorage.clear() })

describe('payment result lifecycle and recovery ownership', () => {
  it.each(['initial', 'poll'].flatMap(stage => ['PENDING', 'COMPLETED', 'failure'].map(outcome => ({ stage, outcome }))))('ignores $stage $outcome replies after unmount without clearing a new order', async ({ stage, outcome }) => {
    const pending = deferred<ReturnType<typeof response>>()
    if (stage === 'poll') api.resolve.mockResolvedValueOnce(response('PENDING')).mockReturnValueOnce(pending.promise)
    else api.resolve.mockReturnValue(pending.promise)
    start(); await flushPromises()
    if (stage === 'poll') { await vi.advanceTimersByTimeAsync(2000); await flushPromises() }
    wrapper!.unmount(); wrapper = undefined
    const scheduled = vi.spyOn(globalThis, 'setTimeout')
    const replacement = JSON.stringify(snapshot({ orderId: 99, resumeToken: 'token-B', owner: { userId: 8, sessionId: 'session-B' } }))
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, replacement)
    if (outcome === 'failure') pending.reject(new Error('old failure'))
    else pending.resolve(response(outcome))
    await flushPromises()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBe(replacement)
    expect(api.refreshUser).not.toHaveBeenCalled()
    // Vue test-utils schedules zero-delay cleanup callbacks during flushPromises.
    expect(scheduled.mock.calls.filter(([, delay]) => delay === 2000)).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(api.resolve).toHaveBeenCalledTimes(stage === 'poll' ? 2 : 1)
  })

  it.each([
    { name: 'order', change: { orderId: 99 } },
    { name: 'token', change: { resumeToken: 'token-B' } },
    { name: 'owner', change: { owner: { userId: 8, sessionId: 'session-B' } } }
  ])('preserves a replacement recovery $name while an active result completes', async ({ change }) => {
    const pending = deferred<ReturnType<typeof response>>()
    api.resolve.mockReturnValue(pending.promise)
    start(); await flushPromises()
    const replacement = JSON.stringify(snapshot(change))
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, replacement)
    pending.resolve(response('COMPLETED')); await flushPromises()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBe(replacement)
  })

  it('clears matching completed recovery and refreshes the current balance once', async () => {
    api.resolve.mockResolvedValue(response('COMPLETED'))
    start(); await flushPromises()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
    expect(api.refreshUser).toHaveBeenCalledTimes(1)
  })

  it.each(['COMPLETED', 'PENDING', 'failure'])('ignores a delayed %s after a stored login changes', async outcome => {
    const pending = deferred<ReturnType<typeof response>>()
    api.resolve.mockReturnValue(pending.promise); start(); await flushPromises()
    const before = wrapper!.html()
    localStorage.setItem('auth_session_id', 'session-B')
    const replacement = JSON.stringify(snapshot({ owner: { userId: 7, sessionId: 'session-B' } }))
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, replacement)
    if (outcome === 'failure') pending.reject(new Error('old login failure'))
    else pending.resolve(response(outcome))
    await flushPromises(); await vi.advanceTimersByTimeAsync(30_000)
    expect(wrapper!.html()).toBe(before)
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBe(replacement)
    expect(api.refreshUser).not.toHaveBeenCalled()
    expect(api.resolve).toHaveBeenCalledTimes(1)
  })

  it('keeps matching recovery valid after same-session access token rotation', async () => {
    const pending = deferred<ReturnType<typeof response>>()
    api.resolve.mockReturnValue(pending.promise); start(); await flushPromises()
    localStorage.setItem('auth_token', 'rotated-token')
    pending.resolve(response('COMPLETED')); await flushPromises()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
    expect(api.refreshUser).toHaveBeenCalledTimes(1)
  })
})
