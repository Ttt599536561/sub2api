import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AffiliateDomainDialog from '../AffiliateDomainDialog.vue'

const { lookupUsers, updateUserDomain, showError, showSuccess, auth } = vi.hoisted(() => ({
  lookupUsers: vi.fn(),
  updateUserDomain: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  auth: { current: null as unknown }
}))
vi.mock('@/api/admin/affiliates', () => ({ affiliatesAPI: { lookupUsers, updateUserDomain } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth.current }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/client', () => ({
  ownedAuthRequestConfig: () => ({ authIdentity: { userID: 1, sessionID: 'session-1' } })
}))
const entry = { user_id: 42, email: 'partner@example.com', username: 'partner', aff_code: 'CODE', aff_code_custom: false, aff_count: 0, affiliate_domain: 'partner.app.example.com', referral_url: 'https://partner.app.example.com/' }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function mountDialog(edit = false) {
  return mount(AffiliateDomainDialog, {
    props: { show: true, entry: edit ? entry : null },
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } }
  })
}
describe('AffiliateDomainDialog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    auth.current = reactive({ isAdmin: true, user: { id: 1 }, sessionRevision: 'session-1' })
    lookupUsers.mockResolvedValue([{ id: 42, email: entry.email, username: entry.username }])
    updateUserDomain.mockResolvedValue({ affiliate_domain: entry.affiliate_domain, referral_url: entry.referral_url })
  })
  afterEach(() => vi.useRealTimers())
  it('selects an existing user and saves only their normalized domain', async () => {
    const wrapper = mountDialog()
    await wrapper.get('[data-test="domain-user-search"]').setValue('partner')
    vi.advanceTimersByTime(300)
    await flushPromises()
    await wrapper.get('[data-test="domain-user-option"]').trigger('click')
    await wrapper.get('[data-test="affiliate-domain"]').setValue(' Partner.App.Example.Com ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateUserDomain).toHaveBeenCalledWith(42, { domain: 'partner.app.example.com' })
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })
  it('clears an existing domain without changing invite codes or rates', async () => {
    const wrapper = mountDialog(true)
    await wrapper.get('[data-test="affiliate-domain"]').setValue('')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateUserDomain).toHaveBeenCalledWith(42, { domain: '' })
  })
  it('keeps the dialog open on duplicate-domain errors', async () => {
    updateUserDomain.mockRejectedValueOnce({ message: 'Domain already in use' })
    const wrapper = mountDialog(true)
    await wrapper.get('[data-test="affiliate-domain"]').setValue('another.app.example.com')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(showError).toHaveBeenCalled()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.get('[data-test="affiliate-domain"]').element).toHaveProperty('value', 'another.app.example.com')
  })
  it('ignores a search response after the query changes, before the next debounce fires', async () => {
    const old = deferred<Array<{ id: number; email: string; username: string }>>()
    lookupUsers.mockReturnValueOnce(old.promise)
    const wrapper = mountDialog()
    await wrapper.get('[data-test="domain-user-search"]').setValue('old')
    vi.advanceTimersByTime(300)
    await wrapper.get('[data-test="domain-user-search"]').setValue('new')
    old.resolve([{ id: 9, email: 'old@example.com', username: 'old' }])
    await flushPromises()
    expect(wrapper.find('[data-test="domain-user-option"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('ignores a completed save after closing and reopening the dialog', async () => {
    const old = deferred<{ affiliate_domain: string; referral_url: string }>()
    updateUserDomain.mockReturnValueOnce(old.promise)
    const wrapper = mountDialog(true)
    await wrapper.get('[data-test="affiliate-domain"]').setValue('new.app.example.com')
    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, entry: null })
    old.resolve({ affiliate_domain: 'new.app.example.com', referral_url: 'https://new.app.example.com/' })
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="domain-user-search"]').exists()).toBe(true)
  })
  it.each(['session', 'permission'])('invalidates pending searches when %s changes', async (change) => {
    const old = deferred<Array<{ id: number; email: string; username: string }>>()
    lookupUsers.mockReturnValueOnce(old.promise)
    const wrapper = mountDialog()
    await wrapper.get('[data-test="domain-user-search"]').setValue('partner')
    vi.advanceTimersByTime(300)
    const current = auth.current as { sessionRevision: string; isAdmin: boolean }
    if (change === 'session') current.sessionRevision = 'session-2'
    else current.isAdmin = false
    await flushPromises()
    old.resolve([{ id: 42, email: entry.email, username: entry.username }])
    await flushPromises()
    expect(wrapper.find('[data-test="domain-user-option"]').exists()).toBe(false)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
