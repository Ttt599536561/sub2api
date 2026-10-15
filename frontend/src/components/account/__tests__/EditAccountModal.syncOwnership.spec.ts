import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { useAuthStore } from '@/stores/auth'
import type { Account } from '@/types'
import EditAccountModal from '../EditAccountModal.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  const state = reactive({ user: { id: 7 }, sessionRevision: 'initial', isAdmin: true, isSimpleMode: true })
  return { useAuthStore: () => state }
})
const mocks = vi.hoisted(() => ({
  sync: vi.fn(), update: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn(), showWarning: vi.fn()
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  accounts: { syncUpstreamModels: mocks.sync, update: mocks.update, checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }) },
  settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }), getSettings: vi.fn().mockResolvedValue({}) },
  tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
} }))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn() }))

const auth = useAuthStore() as unknown as { user: { id: number }; sessionRevision: string; isAdmin: boolean }
function account(id: number): Account {
  return { id, name: `Account ${id}`, platform: 'antigravity', type: 'oauth',
    credentials: { model_mapping: { [`existing-${id}`]: `existing-${id}` } }, extra: {},
    proxy_id: null, concurrency: 1, priority: 1, rate_multiplier: 1, status: 'active', group_ids: [] } as unknown as Account
}
function deferred() {
  let resolve!: (value: { models: string[] }) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<{ models: string[] }>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function login(session: string) {
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin' }))
}
function open() {
  return mount(EditAccountModal, {
    props: { show: true, account: account(1), groups: [], proxies: [] },
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      Icon: true, ProxySelector: true, GroupSelector: true, ModelWhitelistSelector: true, Select: true } }
  })
}
type Wrapper = ReturnType<typeof open>
const syncButton = (wrapper: Wrapper) => wrapper.findAll('button').find(button => button.text().startsWith('admin.accounts.syncUpstreamModels'))!
const models = (wrapper: Wrapper) => wrapper.findAll<HTMLInputElement>('input[placeholder="admin.accounts.requestModel"]').map(input => input.element.value)
function expectNoNotices() {
  for (const notify of [mocks.showError, mocks.showInfo, mocks.showSuccess, mocks.showWarning]) expect(notify).not.toHaveBeenCalled()
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  mocks.sync.mockReset()
  localStorage.clear()
  login('initial')
  auth.user.id = 7
  auth.sessionRevision = 'initial'
  auth.isAdmin = true
  mocks.update.mockResolvedValue(account(2))
})
afterEach(() => localStorage.clear())

describe('Antigravity model sync dialog ownership', () => {
  it('preserves the current account mapping when another account sync finishes late', async () => {
    const old = deferred()
    mocks.sync.mockReturnValueOnce(old.promise)
    const wrapper = open()
    await syncButton(wrapper).trigger('click')
    await wrapper.setProps({ account: account(2) })
    old.resolve({ models: ['old-account-model'] })
    await flushPromises()
    expect(models(wrapper)).toEqual(['existing-2'])
    expectNoNotices()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    expect(mocks.update).toHaveBeenCalledWith(2, expect.objectContaining({ credentials: expect.objectContaining({ model_mapping: { 'existing-2': 'existing-2' } }) }))
  })

  it.each(['success', 'failure'])('does not release a reopened dialog sync when the old request ends with %s', async outcome => {
    const old = deferred()
    const current = deferred()
    mocks.sync.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = open()
    await syncButton(wrapper).trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(syncButton(wrapper).attributes('disabled')).toBeUndefined()
    await syncButton(wrapper).trigger('click')
    expect(mocks.sync).toHaveBeenCalledTimes(2)
    if (outcome === 'success') old.resolve({ models: ['old-cycle-model'] })
    else old.reject(new Error('old cycle failed'))
    await flushPromises()
    expect(syncButton(wrapper).attributes('disabled')).toBeDefined()
    expect(models(wrapper)).toEqual(['existing-1'])
    expectNoNotices()
    current.resolve({ models: ['current-cycle-model'] })
    await flushPromises()
    expect(models(wrapper)).toEqual(['existing-1', 'current-cycle-model'])
    expect(syncButton(wrapper).attributes('disabled')).toBeUndefined()
    expect(mocks.showSuccess).toHaveBeenCalledOnce()
  })

  it.each(['storage', 'same-tab', 'permission', 'unmount'])('ignores sync completion after %s invalidation', async boundary => {
    const pending = deferred()
    mocks.sync.mockReturnValueOnce(pending.promise)
    const wrapper = open()
    await syncButton(wrapper).trigger('click')
    if (boundary === 'storage') {
      login('replacement')
      window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    } else if (boundary === 'same-tab') {
      login('replacement')
      auth.sessionRevision = 'replacement'
    } else if (boundary === 'permission') auth.isAdmin = false
    else wrapper.unmount()
    pending.resolve({ models: ['stale-model'] })
    await flushPromises()
    expectNoNotices()
    if (boundary !== 'unmount') expect(models(wrapper)).toEqual(['existing-1'])
  })
})
