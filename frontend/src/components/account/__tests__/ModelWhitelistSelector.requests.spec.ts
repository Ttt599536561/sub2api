import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

const api = vi.hoisted(() => ({ sync: vi.fn(), preview: vi.fn(), showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn(), showWarning: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: { syncUpstreamModels: api.sync, syncUpstreamModelsPreview: api.preview } }))
vi.mock('@/api', () => ({ authAPI: {}, passkeyAPI: {}, isTotp2FARequired: () => false }))
vi.mock('@/stores/app', () => ({ useAppStore: () => api }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
const credentials = { platform: 'openai', type: 'apikey', api_key: 'key-A', base_url: 'https://provider-A.example/v1' }
let wrapper: VueWrapper | undefined
function start(props = {}) { wrapper = mount(ModelWhitelistSelector, { props: { modelValue: [], platform: 'openai', syncCredentials: credentials, ...props }, global: { stubs: { ModelIcon: true } } }); return wrapper }
async function sync() { await wrapper!.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')?.trigger('click') }
beforeEach(() => { vi.resetAllMocks(); setActivePinia(createPinia()) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('model sync form request ownership', () => {
  it.each(['platform', 'account', 'key', 'endpoint', 'in-place-key', 'type', 'platforms', 'unmount'].flatMap(change => ['success', 'failure'].map(outcome => ({ change, outcome }))))('ignores an old sync $outcome after $change changes', async ({ change, outcome }) => {
    const pending = deferred<{ models: string[] }>()
    api.preview.mockReturnValue(pending.promise); api.sync.mockReturnValue(pending.promise)
    start(change === 'account' ? { accountId: 7 } : { syncCredentials: { ...credentials } }); await sync()
    if (change === 'platform') await wrapper!.setProps({ platform: 'cline', syncCredentials: { ...credentials, platform: 'cline' } })
    else if (change === 'account') await wrapper!.setProps({ accountId: 8 })
    else if (change === 'key') await wrapper!.setProps({ syncCredentials: { ...credentials, api_key: 'key-B' } })
    else if (change === 'endpoint') await wrapper!.setProps({ syncCredentials: { ...credentials, base_url: 'https://provider-B.example/v1' } })
    else if (change === 'in-place-key') wrapper!.props('syncCredentials')!.api_key = 'key-B'
    else if (change === 'type') await wrapper!.setProps({ syncCredentials: { ...credentials, type: 'upstream' } })
    else if (change === 'platforms') await wrapper!.setProps({ platforms: ['cline'] })
    else { wrapper!.unmount(); wrapper = undefined }
    if (outcome === 'success') pending.resolve({ models: ['old-provider-model'] })
    else pending.reject(new Error('old sync failure'))
    await flushPromises()
    if (wrapper) {
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
      expect(wrapper.emitted('upstream-synced')).toBeUndefined()
    }
    expect(api.showSuccess).not.toHaveBeenCalled(); expect(api.showError).not.toHaveBeenCalled()
  })

  it('allows a replacement sync and keeps it busy when the old request finishes', async () => {
    const old = deferred<{ models: string[] }>(), fresh = deferred<{ models: string[] }>()
    api.preview.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    start(); await sync()
    await wrapper!.setProps({ syncCredentials: { ...credentials, api_key: 'key-B' } })
    await sync(); old.resolve({ models: ['old-provider-model'] }); await flushPromises()
    expect(api.preview).toHaveBeenCalledTimes(2)
    expect(wrapper!.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper!.findAll('button').some(b => b.attributes('disabled') !== undefined && b.text() === 'admin.accounts.syncUpstreamModelsLoading')).toBe(true)
    fresh.resolve({ models: ['new-provider-model'] }); await flushPromises()
    expect(wrapper!.emitted('update:modelValue')).toEqual([[['new-provider-model']]])
  })

  it('keeps a reopened selector independent of a pending request from the closed one', async () => {
    const old = deferred<{ models: string[] }>(), fresh = deferred<{ models: string[] }>()
    api.preview.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    start(); await sync(); wrapper!.unmount(); wrapper = undefined
    start({ syncCredentials: { ...credentials, api_key: 'key-B' } }); await sync()
    old.reject(new Error('closed selector failure')); await flushPromises()
    expect(api.showError).not.toHaveBeenCalled()
    expect(wrapper!.emitted('update:modelValue')).toBeUndefined()
    fresh.resolve({ models: ['new-provider-model'] }); await flushPromises()
    expect(wrapper!.emitted('update:modelValue')).toEqual([[['new-provider-model']]])
  })
})
