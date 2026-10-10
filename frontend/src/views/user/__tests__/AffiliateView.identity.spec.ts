import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import AffiliateView from '../AffiliateView.vue'

const api = vi.hoisted(() => ({ detail: vi.fn(), transfer: vi.fn(), refreshUser: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
const auth = reactive({ user: { id: 7 }, sessionRevision: 'session-A', refreshUser: api.refreshUser })
vi.mock('@/api/user', () => ({ default: { getAffiliateDetail: api.detail, transferAffiliateQuota: api.transfer } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => api }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
const detail = (code = 'code-A') => ({ user_id: 7, aff_code: code, inviter_id: null, aff_count: 0, aff_quota: 30, aff_frozen_quota: 0, aff_history_quota: 30, effective_rebate_rate_percent: 10, invitees: [] })
let wrapper: VueWrapper | undefined
function start() { wrapper = mount(AffiliateView, { global: { stubs: { AppLayout: { template: '<main><slot/></main>' }, Icon: true } } }) }
function button() { return wrapper!.findAll('button').find(b => ['affiliate.transfer.button', 'affiliate.transfer.transferring'].includes(b.text()))! }
function replace(kind: string) {
  if (kind === 'unmount') { wrapper!.unmount(); wrapper = undefined; return }
  localStorage.setItem('auth_session_id', 'session-B'); localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
  if (kind === 'storage') window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
  else { auth.user = { id: 8 }; auth.sessionRevision = 'session-B' }
}
beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear(); auth.user = { id: 7 }; auth.sessionRevision = 'session-A'
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 })); localStorage.setItem('auth_session_id', 'session-A')
  api.detail.mockResolvedValue(detail()); api.refreshUser.mockResolvedValue({})
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; localStorage.clear() })
describe('affiliate transfer and detail callbacks keep their view owner', () => {
  it.each(['revision', 'storage', 'unmount'].flatMap(kind => ['success', 'failure'].map(outcome => ({ kind, outcome }))))('ignores old transfer $outcome after $kind', async ({ kind, outcome }) => {
    const pending = deferred<{ transferred_quota: number }>()
    api.transfer.mockReturnValue(pending.promise); start(); await flushPromises(); await button().trigger('click')
    api.detail.mockResolvedValue(detail('code-B')); replace(kind); await flushPromises()
    const detailCalls = api.detail.mock.calls.length
    if (outcome === 'success') pending.resolve({ transferred_quota: 30 })
    else pending.reject(new Error('old transfer failure'))
    await flushPromises()
    expect(api.showSuccess).not.toHaveBeenCalled(); expect(api.showError).not.toHaveBeenCalled()
    expect(api.refreshUser).not.toHaveBeenCalled(); expect(api.detail).toHaveBeenCalledTimes(detailCalls)
    if (wrapper) expect(wrapper.text()).toContain('code-B')
  })
  it('ignores an old initial detail after another login loads its own detail', async () => {
    const old = deferred<ReturnType<typeof detail>>()
    api.detail.mockReturnValueOnce(old.promise).mockResolvedValueOnce(detail('code-B'))
    start(); await flushPromises(); replace('revision'); await flushPromises()
    old.resolve(detail('code-A')); await flushPromises()
    expect(wrapper!.text()).toContain('code-B'); expect(wrapper!.text()).not.toContain('code-A')
  })
  it('preserves current-session transfer success and both refreshes', async () => {
    api.transfer.mockResolvedValue({ transferred_quota: 30 }); start(); await flushPromises(); await button().trigger('click'); await flushPromises()
    expect(api.showSuccess).toHaveBeenCalledTimes(1); expect(api.refreshUser).toHaveBeenCalledTimes(1); expect(api.detail).toHaveBeenCalledTimes(2)
  })
  it.each(['success', 'failure'])('does not unlock the new same-user transfer when the old transfer returns %s', async outcome => {
    const old = deferred<{ transferred_quota: number }>(), fresh = deferred<{ transferred_quota: number }>()
    api.transfer.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    start(); await flushPromises(); await button().trigger('click')
    api.detail.mockResolvedValue(detail('code-B')); localStorage.setItem('auth_session_id', 'session-B')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' })); await flushPromises()
    expect(button().element).toHaveProperty('disabled', false); await button().trigger('click')
    if (outcome === 'success') old.resolve({ transferred_quota: 30 })
    else old.reject(new Error('old transfer failure'))
    await flushPromises()
    expect(button().element).toHaveProperty('disabled', true)
    expect(api.showSuccess).not.toHaveBeenCalled(); expect(api.showError).not.toHaveBeenCalled()
    fresh.resolve({ transferred_quota: 30 }); await flushPromises()
    expect(api.showSuccess).toHaveBeenCalledTimes(1); expect(api.refreshUser).toHaveBeenCalledTimes(1)
  })
})
