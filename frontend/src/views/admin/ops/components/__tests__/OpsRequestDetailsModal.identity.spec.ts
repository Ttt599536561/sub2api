import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive, ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import OpsRequestDetailsModal from '../OpsRequestDetailsModal.vue'

const api = vi.hoisted(() => ({ details: vi.fn(), names: vi.fn(), clearCache: vi.fn(), showError: vi.fn() }))
let auth: { user: { id: number; role: string }; sessionRevision: string; isAdmin: boolean }
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(true) }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { listRequestDetails: api.details } }))
vi.mock('../../utils/accountNameMap', () => ({ getAccountNameMap: api.names, clearAccountNameCache: api.clearCache }))
vi.mock('@/stores', () => ({ useAppStore: () => api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}

const details = { items: [{ kind: 'success', model: 'private-model', account_id: 10 }], total: 1 }
let wrapper: VueWrapper | undefined
async function open() {
  wrapper = mount(OpsRequestDetailsModal, {
    props: { modelValue: false, timeRange: '1h', preset: { title: 'Details' } },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Pagination: true } },
  })
  await wrapper.setProps({ modelValue: true })
  await flushPromises()
}

async function invalidate(change: string) {
  if (change === 'close') await wrapper!.setProps({ modelValue: false })
  else if (change === 'unmount') { wrapper!.unmount(); wrapper = undefined }
  else if (change === 'role') {
    auth.user.role = 'user'
    auth.isAdmin = false
    localStorage.setItem('auth_user', JSON.stringify(auth.user))
  } else {
    localStorage.setItem('auth_session_id', 'session-B')
    localStorage.setItem('auth_user', JSON.stringify({ id: 2, role: 'admin' }))
    if (change === 'storage') window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    else { auth.user.id = 2; auth.sessionRevision = 'session-B' }
  }
  await flushPromises()
}

beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  localStorage.setItem('auth_session_id', 'session-A')
  localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'admin' }))
  auth = reactive({ user: { id: 1, role: 'admin' }, sessionRevision: 'session-A', isAdmin: true })
  api.details.mockResolvedValue(details)
  api.names.mockResolvedValue(new Map([[10, 'private-name']]))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('ops details request ownership', () => {
  it.each(['session', 'storage', 'role', 'close', 'unmount'])('ignores late details after %s changes', async change => {
    const pending = deferred<typeof details>()
    api.details.mockReturnValue(pending.promise)
    await open()
    await invalidate(change)
    pending.resolve(details)
    await flushPromises()
    expect(api.names).not.toHaveBeenCalled()
    if (wrapper) expect(wrapper.text()).not.toContain('private-model')
  })

  it.each(['session', 'storage', 'role', 'close', 'unmount'])('does not retry missing names after %s changes', async change => {
    const pending = deferred<Map<number, string>>()
    api.names.mockReturnValue(pending.promise)
    await open()
    await invalidate(change)
    pending.resolve(new Map())
    await flushPromises()
    expect(api.names).toHaveBeenCalledTimes(1)
  })

  it.each(['session', 'storage', 'role'])('clears displayed private rows and names on %s changes', async change => {
    await open()
    expect(wrapper!.text()).toContain('private-name')
    await invalidate(change)
    expect(wrapper!.text()).not.toContain('private-name')
    expect(wrapper!.text()).not.toContain('private-model')
  })

  it('keeps errors from a closed request out of the reopened dialog', async () => {
    const old = deferred<typeof details>()
    api.details.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ items: [], total: 0 })
    await open()
    await wrapper!.setProps({ modelValue: false })
    await wrapper!.setProps({ modelValue: true })
    old.reject(new Error('old private failure'))
    await flushPromises()
    expect(api.showError).not.toHaveBeenCalled()
  })
})
