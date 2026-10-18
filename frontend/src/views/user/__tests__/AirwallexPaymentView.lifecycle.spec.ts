import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { useAuthStore } from '@/stores/auth'
import AirwallexPaymentView from '../AirwallexPaymentView.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'

const mocks = vi.hoisted(() => ({ init: vi.fn(), redirectToCheckout: vi.fn() }))
vi.mock('@airwallex/components-sdk', () => ({ init: mocks.init }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: { resume_token: 'resume-awx' } }), useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh-CN' } })
}))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  const state = reactive({ user: { id: 7 }, sessionRevision: 'initial' })
  return { useAuthStore: () => state }
})

const auth = useAuthStore() as unknown as { user: { id: number }; sessionRevision: string }
function login(session: string, userId = 7, notify = true) {
  localStorage.setItem('auth_user', JSON.stringify({ id: userId }))
  localStorage.setItem('auth_session_id', session)
  if (notify) {
    auth.user.id = userId
    auth.sessionRevision = session
  }
}
enableAutoUnmount(afterEach)
afterEach(() => window.localStorage.clear())

beforeEach(() => {
  vi.resetAllMocks()
  login('initial')
  window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
    orderId: 101, amount: 88, qrCode: '', expiresAt: '2099-01-01T00:10:00.000Z',
    paymentType: 'airwallex', payUrl: '/payment/airwallex', outTradeNo: 'sub2_awx_101',
    clientSecret: 'awx_client_secret', intentId: 'int_awx_101', currency: 'CNY',
    countryCode: 'CN', paymentEnv: 'demo', payAmount: 88, orderType: 'balance',
    paymentMode: '', resumeToken: 'resume-awx', createdAt: Date.UTC(2099, 0, 1)
  }))
})

function mountView() {
  return shallowMount(AirwallexPaymentView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } }
  })
}

describe('Airwallex checkout lifecycle', () => {
  it('does not initialize checkout after the same user logs in again during SDK import', async () => {
    mocks.init.mockResolvedValue({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
    mountView()
    login('replacement')
    await flushPromises()
    expect(mocks.init).not.toHaveBeenCalled()
    expect(mocks.redirectToCheckout).not.toHaveBeenCalled()
  })

  it.each(['same-tab', 'storage', 'before-storage-event'])('does not redirect an old checkout after %s login changes during initialization', async boundary => {
    let finish!: (result: unknown) => void
    mocks.init.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    expect(mocks.init).toHaveBeenCalledOnce()
    login('replacement', boundary === 'same-tab' ? 7 : 8, boundary === 'same-tab')
    if (boundary === 'storage') window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    finish({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
    await flushPromises()
    expect(mocks.redirectToCheckout).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.airwallexMissingParams')
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).not.toBeNull()
  })

  it('does not expose a late initialization error to the replacement session', async () => {
    let fail!: (reason: unknown) => void
    mocks.init.mockReturnValue(new Promise((_resolve, reject) => { fail = reject }))
    const wrapper = mountView()
    await flushPromises()
    login('replacement', 8)
    fail(new Error('private old checkout failure'))
    await flushPromises()
    expect(wrapper.text()).not.toContain('private old checkout failure')
    expect(mocks.redirectToCheckout).not.toHaveBeenCalled()
  })

  it('does not initialize checkout after leaving during the SDK import', async () => {
    mocks.init.mockResolvedValue({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
    const wrapper = mountView()
    wrapper.unmount()
    await flushPromises()
    expect(mocks.init).not.toHaveBeenCalled()
    expect(mocks.redirectToCheckout).not.toHaveBeenCalled()
  })

  it.each([true, false])('redirects after SDK initialization only while mounted (leave=%s)', async (leave) => {
    let finish!: (result: unknown) => void
    mocks.init.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    expect(mocks.init).toHaveBeenCalledOnce()
    if (leave) wrapper.unmount()
    finish({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
    await flushPromises()
    expect(mocks.redirectToCheckout).toHaveBeenCalledTimes(leave ? 0 : 1)
  })
})
