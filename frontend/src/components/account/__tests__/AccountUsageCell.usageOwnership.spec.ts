import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import AccountUsageCell from '../AccountUsageCell.vue'
import type { Account, AccountUsageInfo } from '@/types'

const { getUsage } = vi.hoisted(() => ({ getUsage: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getUsage } } }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
const account = { id: 42, platform: 'antigravity', type: 'oauth', extra: {} } as Account
function quota(utilization: number): AccountUsageInfo {
  return { antigravity_quota_groups: [{ key: '3p', name: 'Claude and GPT models', windows: [{ window: 'weekly', utilization }] }] }
}
function deferred() {
  let resolve!: (value: AccountUsageInfo) => void
  const promise = new Promise<AccountUsageInfo>(res => { resolve = res })
  return { promise, resolve }
}
const wrappers: VueWrapper[] = []
function mountCell(managed = false) {
  const wrapper = mount(AccountUsageCell, {
    props: { account, ...(managed ? { requestBatchedUsage: vi.fn(), batchedUsage: quota(100) } : {}) },
    global: { stubs: {
      UsageProgressBar: { props: ['utilization'], template: '<div data-test="quota">{{ utilization }}</div>' },
      AccountQuotaInfo: true
    } }
  })
  wrappers.push(wrapper)
  return wrapper
}
async function query(wrapper: VueWrapper) {
  await wrapper.get('[data-testid="antigravity-active-query"]').trigger('click')
  await flushPromises()
}
describe('AccountUsageCell accepted network results', () => {
  beforeEach(() => { localStorage.clear(); getUsage.mockReset().mockResolvedValue(quota(100)) })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })

  it('does not report parent props as a new network result', async () => {
    const wrapper = mountCell(true)
    await flushPromises()
    expect(wrapper.emitted('usage-loaded')).toBeUndefined()
    await wrapper.setProps({ batchedUsage: quota(25) })
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    expect(wrapper.emitted('usage-loaded')).toBeUndefined()
  })

  it.each(['account', 'session', 'mode'] as const)('ignores a pending active response after its %s changes', async boundary => {
    const pending = deferred()
    getUsage.mockReturnValueOnce(pending.promise)
    const wrapper = mountCell(true)
    await flushPromises()
    await query(wrapper)
    if (boundary === 'account') await wrapper.setProps({ account: { ...account, id: 43 }, batchedUsage: quota(60) })
    if (boundary === 'session') {
      localStorage.setItem('auth_session_id', 'replacement-login')
      await wrapper.setProps({ batchedUsage: quota(60) })
    }
    if (boundary === 'mode') await wrapper.setProps({ batchedUsage: quota(60), requestBatchedUsage: null })
    pending.resolve(quota(25))
    await flushPromises()
    expect(wrapper.find('[data-test="quota"]').text()).not.toBe('25')
    expect(wrapper.emitted('usage-loaded') ?? []).not.toContainEqual([quota(25)])
    expect(wrapper.get('[data-testid="antigravity-active-query"]').attributes('disabled')).toBeUndefined()
  })

  it('uses the active result for a deferred mobile visibility load without another request', async () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() } as unknown as MediaQueryList)
    let enterViewport!: () => void
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: IntersectionObserverCallback) {
        enterViewport = () => callback([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
      }
      observe() {}
      disconnect() {}
    })
    try {
      getUsage.mockResolvedValueOnce(quota(25)).mockResolvedValueOnce(quota(100))
      const wrapper = mountCell()
      await flushPromises()
      expect(getUsage).not.toHaveBeenCalled()
      await query(wrapper)
      expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
      enterViewport()
      await flushPromises()
      expect(getUsage).toHaveBeenCalledTimes(1)
      expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
      expect(wrapper.emitted('usage-loaded')).toEqual([[quota(25)]])
    } finally { vi.unstubAllGlobals() }
  })

  it('keeps a newer manual refresh when an earlier active query finishes late', async () => {
    const wrapper = mountCell()
    await flushPromises()
    const pending = deferred()
    getUsage.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(quota(60))
    await query(wrapper)
    await wrapper.setProps({ manualRefreshToken: 1 })
    await flushPromises()
    pending.resolve(quota(25))
    await flushPromises()
    expect(wrapper.get('[data-test="quota"]').text()).toBe('60')
    expect(wrapper.get('[data-testid="antigravity-active-query"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('usage-loaded')?.map(args => args[0])).toEqual([quota(100), quota(60)])
  })
})
