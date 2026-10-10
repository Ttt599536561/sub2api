import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UserPlatformQuotaModal from '../UserPlatformQuotaModal.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), reset: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { getPlatformQuotas: api.get, updatePlatformQuotas: api.save, resetPlatformQuotaWindow: api.reset } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' } }))
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
const quotas = (amount: number) => ({ platform_quotas: [{ platform: 'anthropic', daily_limit_usd: amount }] })
const user = (id: number) => ({ id, email: `user${id}@example.com` })
let wrapper: VueWrapper | undefined
function button(label: string) { return wrapper!.findAll('button').find(b => b.text() === label)! }
async function open() { wrapper = mount(UserPlatformQuotaModal, { props: { show: false, user: user(7) as never } }); await wrapper.setProps({ show: true }); await flushPromises() }
beforeEach(() => { vi.resetAllMocks(); setActivePinia(createPinia()); api.get.mockResolvedValue(quotas(70)); api.save.mockResolvedValue({}); api.reset.mockResolvedValue(quotas(0)); vi.spyOn(window, 'confirm').mockReturnValue(true) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks() })

describe('platform quota form request ownership', () => {
  it.each(['close-reopen', 'switch-user'].flatMap(change => ['success', 'failure'].map(outcome => ({ change, outcome }))))('ignores an old load $outcome after $change', async ({ change, outcome }) => {
    const pending = deferred<ReturnType<typeof quotas>>()
    api.get.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(quotas(800))
    await open()
    if (change === 'close-reopen') await wrapper!.setProps({ show: false })
    await wrapper!.setProps({ user: user(8) as never, show: true }); await flushPromises()
    if (outcome === 'success') pending.resolve(quotas(70))
    else pending.reject(new Error('old load failure'))
    await flushPromises()
    expect(wrapper!.findAll('input[type=number]')[0].element).toHaveProperty('value', '800')
    expect(api.showError).not.toHaveBeenCalled()
    await button('admin.users.platformQuota.save').trigger('click'); await flushPromises()
    expect(api.save).toHaveBeenCalledWith(8, expect.arrayContaining([expect.objectContaining({ platform: 'anthropic', daily_limit_usd: 800 })]))
  })

  it.each(['save', 'reset'].flatMap(operation => ['success', 'failure'].map(outcome => ({ operation, outcome }))))('ignores an old $operation $outcome after another user opens', async ({ operation, outcome }) => {
    await open()
    const pending = deferred<ReturnType<typeof quotas>>()
    if (operation === 'save') { api.save.mockReturnValue(pending.promise); await button('admin.users.platformQuota.save').trigger('click') }
    else { api.reset.mockReturnValue(pending.promise); await wrapper!.get('button[title="admin.users.platformQuota.reset.button"]').trigger('click') }
    await wrapper!.setProps({ show: false }); api.get.mockResolvedValueOnce(quotas(800))
    await wrapper!.setProps({ show: true, user: user(8) as never }); await flushPromises()
    if (outcome === 'success') pending.resolve(quotas(0))
    else pending.reject(new Error('old operation failure'))
    await flushPromises()
    expect(wrapper!.findAll('input[type=number]')[0].element).toHaveProperty('value', '800')
    expect(api.showSuccess).not.toHaveBeenCalled()
    expect(api.showError).not.toHaveBeenCalled()
    expect(wrapper!.emitted('close')).toBeUndefined()
  })

  it('does not apply a load that finishes after unmount', async () => {
    const pending = deferred<ReturnType<typeof quotas>>()
    api.get.mockReturnValue(pending.promise); await open(); wrapper!.unmount(); wrapper = undefined
    pending.reject(new Error('old load failure')); await flushPromises()
    expect(api.showError).not.toHaveBeenCalled()
  })

  it.each(['save', 'reset'])('does not unlock or replace a reopened same-user %s while its new request is pending', async operation => {
    await open()
    const old = deferred<ReturnType<typeof quotas>>(), fresh = deferred<ReturnType<typeof quotas>>()
    const method = operation === 'save' ? api.save : api.reset
    method.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const submit = async () => {
      if (operation === 'save') await button('admin.users.platformQuota.save').trigger('click')
      else await wrapper!.get('button[title="admin.users.platformQuota.reset.button"]').trigger('click')
    }
    await submit(); await wrapper!.setProps({ show: false }); await wrapper!.setProps({ show: true }); await flushPromises()
    await submit(); old.resolve(quotas(0)); await flushPromises()
    expect(api.showSuccess).not.toHaveBeenCalled()
    expect(wrapper!.emitted('close')).toBeUndefined()
    if (operation === 'save') expect(button('admin.users.platformQuota.saving').attributes('disabled')).toBeDefined()
    else expect(wrapper!.get('button[title="admin.users.platformQuota.reset.button"]').attributes('disabled')).toBeDefined()
    fresh.resolve(quotas(0)); await flushPromises()
    expect(api.showSuccess).toHaveBeenCalledTimes(1)
  })
})
