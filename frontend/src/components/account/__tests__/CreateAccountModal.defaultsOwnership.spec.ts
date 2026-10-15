import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({ defaults: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  accounts: { checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }) },
  settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }), getSettings: vi.fn().mockResolvedValue({}) },
  tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
} }))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: mocks.defaults }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))

async function open() {
  const { default: CreateAccountModal } = await import('../CreateAccountModal.vue')
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups: [] },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      OAuthAuthorizationFlow: true, ConfirmDialog: true, Select: true, Icon: true,
      PlatformIcon: true, ProxySelector: true, ProxyAdBanner: true, GroupSelector: true,
      ModelWhitelistSelector: true, QuotaLimitCard: true
    } }
  })
}
type Wrapper = Awaited<ReturnType<typeof open>>
async function selectAntigravity(wrapper: Wrapper) {
  await wrapper.findAll('button').find(button => button.text().includes('Antigravity'))!.trigger('click')
}
const targets = (wrapper: Wrapper) => wrapper.findAll<HTMLInputElement>('input[placeholder="admin.accounts.actualModel"]').map(input => input.element.value)
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(ok => { resolve = ok })
  return { promise, resolve }
}

enableAutoUnmount(afterEach)
beforeEach(() => { vi.resetModules(); mocks.defaults.mockReset() })

describe('Antigravity create defaults ownership', () => {
  it('does not carry edited default mapping targets into another create dialog', async () => {
    mocks.defaults.mockResolvedValue({ 'public-model': 'default-target' })
    const first = await open()
    await selectAntigravity(first)
    await flushPromises()
    expect(targets(first)).toEqual(['default-target'])
    await first.get('input[placeholder="admin.accounts.actualModel"]').setValue('private-first-account-target')
    first.unmount()

    const next = await open()
    await selectAntigravity(next)
    await flushPromises()
    expect(targets(next)).toEqual(['default-target'])
    expect(mocks.defaults).toHaveBeenCalledOnce()
  })

  it('does not replace a current mapping with late defaults from an earlier platform selection', async () => {
    const old = deferred<Record<string, string>>()
    mocks.defaults.mockReturnValueOnce(old.promise).mockResolvedValue({ 'public-model': 'default-target' })
    const wrapper = await open()
    await selectAntigravity(wrapper)
    await wrapper.findAll('button').find(button => button.text() === 'OpenAI')!.trigger('click')
    await selectAntigravity(wrapper)
    await flushPromises()
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('current-account-target')
    old.resolve({ 'public-model': 'outdated-target' })
    await flushPromises()
    expect(targets(wrapper)).toEqual(['current-account-target'])
  })

  it('preserves a manually added mapping while the first default request is pending', async () => {
    const pending = deferred<Record<string, string>>()
    mocks.defaults.mockReturnValue(pending.promise)
    const wrapper = await open()
    await selectAntigravity(wrapper)
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addMapping')!.trigger('click')
    await wrapper.get('input[placeholder="admin.accounts.requestModel"]').setValue('custom-model')
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('custom-target')
    pending.resolve({ 'public-model': 'default-target' })
    await flushPromises()
    expect(targets(wrapper)).toEqual(['custom-target'])
  })

  it('invalidates a closed dialog load without starting another request during reset', async () => {
    const old = deferred<Record<string, string>>()
    mocks.defaults.mockReturnValueOnce(old.promise).mockResolvedValue({ 'public-model': 'default-target' })
    const wrapper = await open()
    await selectAntigravity(wrapper)
    await wrapper.setProps({ show: false })
    await flushPromises()
    expect(mocks.defaults).toHaveBeenCalledOnce()
    await wrapper.setProps({ show: true })
    await selectAntigravity(wrapper)
    await flushPromises()
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('reopened-target')
    old.resolve({ 'public-model': 'closed-target' })
    await flushPromises()
    expect(targets(wrapper)).toEqual(['reopened-target'])
  })

  it('retries defaults after an unsuccessful load when the platform is selected again', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    try {
      mocks.defaults.mockRejectedValueOnce(new Error('temporary failure')).mockResolvedValue({ 'public-model': 'retry-target' })
      const wrapper = await open()
      await selectAntigravity(wrapper)
      await flushPromises()
      expect(targets(wrapper)).toEqual([])
      await wrapper.findAll('button').find(button => button.text() === 'OpenAI')!.trigger('click')
      await selectAntigravity(wrapper)
      await flushPromises()
      expect(targets(wrapper)).toEqual(['retry-target'])
      expect(mocks.defaults).toHaveBeenCalledTimes(2)
    } finally {
      warn.mockRestore()
    }
  })
})
