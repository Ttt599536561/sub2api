import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent } from 'vue'
import AccountsView from '../AccountsView.vue'
import Pagination from '@/components/common/Pagination.vue'
import AccountUsageCell from '@/components/account/AccountUsageCell.vue'
import type { AccountUsageInfo } from '@/types'

const { getBatchUsage, getUsage, listAccounts } = vi.hoisted(() => ({
  getBatchUsage: vi.fn(), getUsage: vi.fn(), listAccounts: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  accounts: {
    getBatchUsage, getUsage, list: listAccounts,
    listWithEtag: async () => ({ notModified: true, data: null }),
    getBatchTodayStats: async () => ({ stats: {} }),
    getUpstreamBillingProbeSettings: async () => ({ enabled: true })
  },
  proxies: { getAll: async () => [] }, groups: { getAll: async () => [] }
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showWarning: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ token: 'test-token', isSimpleMode: false }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const row = { id: 42, name: 'quota account', platform: 'antigravity', type: 'oauth', status: 'active', schedulable: true, concurrency: 2, priority: 1, group_ids: [], extra: {}, credentials: {} }
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] }, loading: Boolean },
  template: '<div><template v-if="!loading"><div v-for="row in data" :key="row.id"><slot name="cell-usage" :row="row" /></div></template></div>'
})
const wrappers: VueWrapper[] = []
let desktop = true
const mediaListeners = new Set<(event: MediaQueryListEvent) => void>()
function viewport(matches: boolean) {
  desktop = matches
  mediaListeners.forEach(listener => listener({ matches } as MediaQueryListEvent))
}
function quota(utilization: number): AccountUsageInfo {
  return { antigravity_quota_groups: [{ key: '3p', name: 'Claude and GPT models', windows: [{ window: 'weekly', utilization }] }] }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function mountView() {
  const wrapper = mount(AccountsView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
      DataTable: DataTableStub,
      AccountTableActions: { template: '<button data-test="refresh" @click="$emit(\'refresh\')">refresh</button>' },
      AccountTableFilters: true, AccountBulkActionsBar: true, ConfirmDialog: true, AccountActionMenu: true,
      ImportDataModal: true, ReAuthAccountModal: true, AccountTestModal: true, AccountStatsModal: true,
      ScheduledTestsPanel: true, SyncFromCrsModal: true, TempUnschedStatusModal: true,
      ErrorPassthroughRulesModal: true, TLSFingerprintProfilesModal: true, CreateAccountModal: true,
      EditAccountModal: true, BulkEditAccountModal: true, UpstreamBillingRateCell: true, HelpTooltip: true,
      UsageProgressBar: { props: ['utilization'], template: '<div data-test="quota">{{ utilization }}</div>' },
      AccountQuotaInfo: true, Icon: true
    } }
  })
  wrappers.push(wrapper)
  return wrapper
}
async function settle() {
  await flushPromises()
  await vi.advanceTimersByTimeAsync(1)
  await flushPromises()
}
async function page(wrapper: VueWrapper, number: number) {
  const button = wrapper.getComponent(Pagination).findAll('button').find(button => button.text() === String(number))!
  await button.trigger('click')
  await settle()
}
async function query(wrapper: VueWrapper) {
  await wrapper.get('[data-testid="antigravity-active-query"]').trigger('click')
  await flushPromises()
}

describe('AccountsView accepted quota cache', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-11T00:00:00Z'))
    localStorage.clear()
    desktop = true
    mediaListeners.clear()
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
      matches: desktop, media: query, onchange: null,
      addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn(),
      addEventListener: (_type: string, listener: EventListenerOrEventListenerObject) => mediaListeners.add(listener as (event: MediaQueryListEvent) => void),
      removeEventListener: (_type: string, listener: EventListenerOrEventListenerObject) => mediaListeners.delete(listener as (event: MediaQueryListEvent) => void)
    }))
    listAccounts.mockReset().mockImplementation((page: number) => Promise.resolve({ items: page === 1 ? [{ ...row }] : [], total: 21, page, page_size: 20, pages: 2 }))
    getBatchUsage.mockReset().mockResolvedValue({ usage: { 42: quota(100) }, errors: {} })
    getUsage.mockReset().mockResolvedValue(quota(25))
  })
  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('retains an active result through real pagination and child remount without another request', async () => {
    const wrapper = mountView()
    await settle()
    expect(wrapper.get('[data-test="quota"]').text()).toBe('100')
    await query(wrapper)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    await page(wrapper, 2)
    expect(wrapper.findComponent(AccountUsageCell).exists()).toBe(false)
    await page(wrapper, 1)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    expect(getBatchUsage).toHaveBeenCalledTimes(1)
    expect(getUsage).toHaveBeenCalledTimes(1)
    expect(getUsage).toHaveBeenLastCalledWith(42, 'active', true)
  })

  it.each(['success', 'error'] as const)('ignores an older pending batch %s after an active result', async result => {
    const batch = deferred<{ usage: Record<string, AccountUsageInfo>; errors: Record<string, string> }>()
    const wrapper = mountView()
    await settle()
    getBatchUsage.mockReturnValueOnce(batch.promise)
    const active = deferred<AccountUsageInfo>()
    getUsage.mockReturnValueOnce(active.promise)
    await query(wrapper)
    await wrapper.get('[data-test="refresh"]').trigger('click')
    await settle()
    active.resolve(quota(25))
    await flushPromises()
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    batch.resolve(result === 'success' ? { usage: { 42: quota(100) }, errors: {} } : { usage: {}, errors: { 42: 'old batch error' } })
    await settle()
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    expect(wrapper.text()).not.toContain('old batch error')
    await page(wrapper, 2)
    await page(wrapper, 1)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    expect(getBatchUsage).toHaveBeenCalledTimes(2)
  })

  it('clears an initial batch error after active success and caches the recovery', async () => {
    getBatchUsage.mockResolvedValueOnce({ usage: {}, errors: { 42: 'initial network error' } })
    const wrapper = mountView()
    await settle()
    expect(wrapper.text()).toContain('initial network error')
    await query(wrapper)
    expect(wrapper.text()).not.toContain('initial network error')
    await page(wrapper, 2)
    await page(wrapper, 1)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    expect(getBatchUsage).toHaveBeenCalledTimes(1)
  })

  it('keeps the original five-minute expiry when cached parent props are replayed', async () => {
    const wrapper = mountView()
    await settle()
    await query(wrapper)
    const acceptedAt = Date.now()
    vi.setSystemTime(acceptedAt + 4 * 60 * 1000)
    await page(wrapper, 2)
    await page(wrapper, 1)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    expect(getBatchUsage).toHaveBeenCalledTimes(1)
    vi.setSystemTime(acceptedAt + 5 * 60 * 1000 + 1)
    await page(wrapper, 2)
    await page(wrapper, 1)
    expect(getBatchUsage).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('100')
  })

  it('preserves the last good cache on active failure and allows forced manual refresh', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = mountView()
    await settle()
    await query(wrapper)
    getUsage.mockRejectedValueOnce(new Error('active query failed'))
    await query(wrapper)
    await page(wrapper, 2)
    await page(wrapper, 1)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
    expect(getBatchUsage).toHaveBeenCalledTimes(1)
    getBatchUsage.mockResolvedValueOnce({ usage: { 42: quota(60) }, errors: {} })
    await wrapper.get('[data-test="refresh"]').trigger('click')
    await settle()
    expect(getBatchUsage).toHaveBeenLastCalledWith([42], true)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('60')
  })

  it('does not reuse an accepted cache after the browser login changes', async () => {
    const wrapper = mountView()
    await settle()
    await query(wrapper)
    localStorage.setItem('auth_session_id', 'replacement-login')
    getBatchUsage.mockResolvedValueOnce({ usage: { 42: quota(60) }, errors: {} })
    await page(wrapper, 2)
    await page(wrapper, 1)
    expect(getBatchUsage).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="quota"]').text()).toBe('60')
  })

  it('retains a mobile active result when switching to desktop and remounting', async () => {
    desktop = false
    // No observer: visible fallback invokes the normal mobile direct-request path.
    vi.stubGlobal('IntersectionObserver', undefined)
    try {
      getUsage.mockResolvedValueOnce(quota(100)).mockResolvedValueOnce(quota(25))
      const wrapper = mountView()
      await settle()
      expect(wrapper.get('[data-test="quota"]').text()).toBe('100')
      await query(wrapper)
      viewport(true)
      await settle()
      expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
      await page(wrapper, 2)
      await page(wrapper, 1)
      expect(wrapper.get('[data-test="quota"]').text()).toBe('25')
      expect(getBatchUsage).not.toHaveBeenCalled()
      expect(getUsage).toHaveBeenCalledTimes(2)
    } finally { vi.unstubAllGlobals() }
  })
})
