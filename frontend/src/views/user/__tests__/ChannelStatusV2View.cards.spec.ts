import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, reactive, ref } from 'vue'
import { flushPromises, shallowMount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  snapshot: vi.fn(), matrix: vi.fn(), dimensions: vi.fn(), models: vi.fn(), errors: vi.fn(), users: vi.fn(),
  groups: vi.fn(), rates: vi.fn(), replace: vi.fn(), showError: vi.fn(),
}))
let auth: ReturnType<typeof reactive<any>>
let language: ReturnType<typeof ref<string>>
let routeQuery: Record<string, string>
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError }) }))
vi.mock('vue-router', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-router')>(), useRoute: () => ({ query: routeQuery }), useRouter: () => ({ replace: mocks.replace }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, te: () => false, locale: language }) }))
vi.mock('@/utils/featureFlags', () => ({ isChannelMonitorThroughputHidden: () => true, isChannelMonitorUserRankingHidden: () => true }))
vi.mock('@/api/channelMonitorV2', () => ({
  getSnapshot: mocks.snapshot, getMatrix: mocks.matrix, getDimensions: mocks.dimensions,
  getModels: mocks.models, getErrors: mocks.errors, getUsers: mocks.users,
}))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getAvailable: mocks.groups, getUserGroupRates: mocks.rates } }))
import ChannelStatusV2View from '../ChannelStatusV2View.vue'
import type { ProviderSection } from '@/features/channel-monitor-v2/cards/monitorCards'

const health = { overall: 'warning', error_rate: 'healthy', ttft: 'warning', cache: 'healthy', minimum_sample: 50 }
const metric = { request_count: 0, success_rate: 1, error_rate: 0, cache_rate: 0.8, ttft: { p50_ms: 5000 }, tpm: 0, rpm: 0 }
const coverage = { requested_start: '2026-10-23T00:00:00Z', requested_end: '2026-10-23T01:30:00Z', data_through: '2026-10-23T01:30:00Z', bucket_seconds: 300, coverage_complete: true }
const row = { platform: 'openai', group_id: 1, group_name: 'Group A', metrics: metric, health, buckets: [{ bucket_start: '2026-10-23T01:25:00Z', metrics: metric, health }] }
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((r, e) => { resolve = r; reject = e }); return { promise, resolve, reject } }
const wrappers: ReturnType<typeof shallowMount>[] = []
function mountView() {
  const wrapper = shallowMount(ChannelStatusV2View, { global: { stubs: {
    AppLayout: defineComponent({ template: '<main><slot /></main>' }),
    MonitorCards: defineComponent({ props: ['sections', 'countdownSeconds'], template: '<div data-testid="cards-props">{{ JSON.stringify(sections) }}</div>' }),
  } } })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  auth = reactive({ user: { id: 1, role: 'user' }, sessionRevision: 'a', isAdmin: false, isAuthenticated: true })
  language = ref('zh')
  routeQuery = {}
  localStorage.setItem('auth_session_id', 'a')
  localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'user' }))
  mocks.replace.mockResolvedValue(undefined)
  mocks.snapshot.mockResolvedValue({ metrics: metric, health, coverage, config: { refresh_interval_seconds: 60 }, trend: [] })
  mocks.matrix.mockResolvedValue({ coverage, items: [row], group_by: 'platform_group' })
  mocks.dimensions.mockResolvedValue({ platforms: [], models: [], groups: [{ id: 1, name: 'Group A', platform: 'openai' }] })
  mocks.groups.mockResolvedValue([{ id: 1, name: '本地化分组', platform: 'openai', rate_multiplier: 1 }])
  mocks.rates.mockResolvedValue({ 1: 0 })
  for (const fn of [mocks.models, mocks.errors, mocks.users]) fn.mockResolvedValue({ items: [] })
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); localStorage.clear(); vi.useRealTimers() })

describe('V2 cards and detail integration', () => {
  it('defaults to cards, loads group rates, and uses platform_group without fetching detail tabs', async () => {
    routeQuery = { group_by: 'platform_model', platform: 'grok' }
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="monitor-cards-view"]').exists()).toBe(true)
    expect(mocks.matrix).toHaveBeenCalledWith({ range: '90m', platforms: [], groupIds: [], models: [] }, 'platform_group', false, expect.any(AbortSignal))
    expect(mocks.groups).toHaveBeenCalled()
    expect(mocks.models).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="cards-props"]').text()).toContain('本地化分组')
  })
  it('switches to detail and restores its filters/grouping; switching back restores card scope', async () => {
    routeQuery = { group_by: 'platform_model', platform: 'grok' }
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="monitor-view-detail"]').trigger('click')
    await flushPromises()
    expect(mocks.matrix).toHaveBeenLastCalledWith(expect.objectContaining({ platforms: ['grok'] }), 'platform_model', false, expect.any(AbortSignal))
    expect(mocks.models).toHaveBeenCalled()
    expect(mocks.replace).toHaveBeenLastCalledWith(expect.objectContaining({ query: expect.objectContaining({ view: 'detail' }) }))
    await wrapper.get('[data-testid="monitor-view-cards"]').trigger('click')
    await flushPromises()
    expect(mocks.matrix).toHaveBeenLastCalledWith(expect.objectContaining({ platforms: [] }), 'platform_group', false, expect.any(AbortSignal))
  })
  it('supports a detail deep link', async () => {
    routeQuery = { view: 'detail', range: '7d' }
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="monitor-cards-view"]').exists()).toBe(false)
    expect(mocks.models).toHaveBeenCalled()
  })
  it('rejects old monitor and rate results after a login session change', async () => {
    const oldSnapshot = deferred<any>(); const oldRates = deferred<any>()
    mocks.snapshot.mockReturnValueOnce(oldSnapshot.promise)
    mocks.rates.mockReturnValueOnce(oldRates.promise)
    const wrapper = mountView()
    await flushPromises()
    localStorage.setItem('auth_session_id', 'b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 2, role: 'user' }))
    auth.user = { id: 2, role: 'user' }; auth.sessionRevision = 'b'
    mocks.groups.mockResolvedValue([{ id: 1, name: '新用户分组', rate_multiplier: 2 }])
    mocks.rates.mockResolvedValue({ 1: 0.5 })
    await flushPromises()
    oldSnapshot.resolve({ coverage, metrics: metric, health, config: { refresh_interval_seconds: 300 }, trend: [] })
    oldRates.resolve({ 1: 99 })
    await flushPromises()
    expect(wrapper.get('[data-testid="cards-props"]').text()).not.toContain('99')
    expect(mocks.matrix.mock.calls.length).toBeGreaterThan(1)
    expect(mocks.showError).not.toHaveBeenCalled()
  })
  it('does not pretend a failed custom rate request means default pricing', async () => {
    mocks.rates.mockRejectedValue(new Error('offline'))
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.find('[data-testid="monitor-rates-error"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="cards-props"]').text()).toContain('"rateMultiplier":null')
  })
  it('ignores stale detail results when switching tabs', async () => {
    routeQuery = { view: 'detail' }
    const oldModels = deferred<any>()
    mocks.models.mockReturnValueOnce(oldModels.promise)
    const wrapper = mountView(); await flushPromises()
    const errorTab = wrapper.findAll('button').find(b => b.text() === 'channelMonitorV2.tabs.errors')!
    await errorTab.trigger('click'); await flushPromises()
    oldModels.resolve({ items: [{ model: 'stale-result' }] }); await flushPromises()
    expect(wrapper.text()).not.toContain('stale-result')
  })
  it('reloads localized group names when language changes', async () => {
    const wrapper = mountView(); await flushPromises()
    mocks.groups.mockResolvedValue([{ id: 1, name: 'English group', rate_multiplier: 1 }])
    language.value = 'en'; await flushPromises()
    expect(wrapper.get('[data-testid="cards-props"]').text()).toContain('English group')
  })
  it('clears data and cancels requests on logout', async () => {
    const wrapper = mountView(); await flushPromises()
    auth.isAuthenticated = false; auth.user = null; auth.sessionRevision = 'out'
    await flushPromises()
    expect(wrapper.text()).not.toContain('本地化分组')
    expect((mocks.matrix.mock.calls[0][3] as AbortSignal).aborted).toBe(true)
  })

  it('clears administrator data when another tab downgrades the same user in the same session', async () => {
    auth.isAdmin = true; auth.user.role = 'admin'
    localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'admin' }))
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('本地化分组')
    localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'user' }))
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_user' }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('本地化分组')
    expect((mocks.matrix.mock.calls[0][3] as AbortSignal).aborted).toBe(true)
  })
  it('clears an aborted detail spinner even if the replacement refresh fails', async () => {
    routeQuery = { view: 'detail' }
    const oldModels = deferred<any>()
    mocks.models.mockReturnValueOnce(oldModels.promise)
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('common.loading')
    mocks.snapshot.mockRejectedValueOnce(new Error('network unavailable'))
    await wrapper.get('button[title="common.refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('common.loading')
    oldModels.resolve({ items: [] }); await flushPromises()
  })
  it('does not show an error from a request rejected after unmount', async () => {
    let reject!: (error: Error) => void
    mocks.snapshot.mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    const wrapper = mountView(); await flushPromises()
    wrapper.unmount()
    reject(new Error('old network error')); await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('keeps the configured polling cadence after a failed automatic refresh and pauses when hidden', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    mountView(); await flushPromises()
    mocks.snapshot.mockRejectedValue(new Error('offline'))
    for (let i = 0; i < 60; i++) { await vi.advanceTimersByTimeAsync(1000); await flushPromises() }
    expect(mocks.snapshot).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect(mocks.snapshot).toHaveBeenCalledTimes(2)
    hidden.mockReturnValue(true)
    await vi.advanceTimersByTimeAsync(120000); await flushPromises()
    expect(mocks.snapshot).toHaveBeenCalledTimes(2)
    hidden.mockRestore()
  })

  it('blocks a refresh if persisted identity changed before its storage event is delivered', async () => {
    const wrapper = mountView(); await flushPromises()
    localStorage.setItem('auth_session_id', 'another-login')
    localStorage.setItem('auth_user', JSON.stringify({ id: 2, role: 'admin' }))
    await wrapper.get('button[title="common.refresh"]').trigger('click'); await flushPromises()
    expect(mocks.snapshot).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).not.toContain('本地化分组')
  })
  it('reloads after a same-tab role update persists the user after the synchronous store change', async () => {
    const wrapper = mountView(); await flushPromises()
    auth.user.role = 'admin'; auth.isAdmin = true
    localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'admin' }))
    await flushPromises()
    expect(mocks.matrix).toHaveBeenLastCalledWith(expect.any(Object), 'platform_group', true, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('本地化分组')
  })
})


function displayedCards(wrapper: ReturnType<typeof mountView>) {
  return (JSON.parse(wrapper.get('[data-testid="cards-props"]').text()) as ProviderSection[]).flatMap(section => section.cards)
}
function useSortedFixture() {
  const initialRows = [row, { ...row, group_id: 2, group_name: 'Alpha' }]
  const initialGroups = [{ id: 1, name: 'Zulu', platform: 'openai', rate_multiplier: 1 }, { id: 2, name: 'Alpha', platform: 'openai', rate_multiplier: 1 }]
  const initialDimensions = { platforms: [], models: [], groups: initialGroups }
  mocks.matrix.mockResolvedValue({ coverage, items: initialRows, group_by: 'platform_group' })
  mocks.dimensions.mockResolvedValue(initialDimensions)
  mocks.groups.mockResolvedValue(initialGroups)
  mocks.rates.mockResolvedValue({ 1: 0, 2: 1 })
  const updatedHealth = { ...health, overall: 'critical', error_rate: 'critical' }
  const updatedMetric = { ...metric, error_rate: 0.5, success_rate: 0.5 }
  const updatedRows = initialRows.map(item => ({ ...item, metrics: updatedMetric, health: updatedHealth,
    buckets: item.buckets.map(bucket => ({ ...bucket, metrics: updatedMetric, health: updatedHealth })) }))
  return { initialRows, initialGroups, initialDimensions, updatedRows, updatedMetric, updatedHealth }
}

describe('atomic monitor card refresh', () => {
  it('keeps the last complete cards and rate ordering while manual refresh is pending', async () => {
    useSortedFixture()
    const wrapper = mountView(); await flushPromises()
    const before = displayedCards(wrapper)
    expect(before.map(card => card.name)).toEqual(['Zulu', 'Alpha'])
    const rates = deferred<Record<number, number>>()
    mocks.rates.mockReturnValueOnce(rates.promise)
    await wrapper.get('button[title="common.refresh"]').trigger('click'); await flushPromises()
    expect(displayedCards(wrapper)).toEqual(before)
    expect(wrapper.findAll('.animate-pulse')).toHaveLength(0)
    rates.resolve({ 1: 0, 2: 1 }); await flushPromises()
    expect(displayedCards(wrapper)).toEqual(before)
  })

  it.each(['rates', 'metrics'] as const)('publishes only the complete batch when %s finish first', async (first) => {
    const fixture = useSortedFixture()
    const wrapper = mountView(); await flushPromises()
    const before = displayedCards(wrapper)
    const rates = deferred<Record<number, number>>()
    const matrix = deferred<{ coverage: typeof coverage; items: typeof fixture.updatedRows; group_by: string }>()
    mocks.rates.mockReturnValueOnce(rates.promise)
    mocks.groups.mockResolvedValueOnce(fixture.initialGroups.map(group => ({ ...group, name: group.name + ' updated' })))
    mocks.matrix.mockReturnValueOnce(matrix.promise)
    mocks.snapshot.mockResolvedValueOnce({ coverage, health: fixture.updatedHealth, metrics: fixture.updatedMetric, config: { refresh_interval_seconds: 60 }, trend: [] })
    await wrapper.get('button[title="common.refresh"]').trigger('click')
    if (first === 'rates') rates.resolve({ 1: 2, 2: 0 })
    else matrix.resolve({ coverage, items: fixture.updatedRows, group_by: 'platform_group' })
    await flushPromises()
    expect(displayedCards(wrapper)).toEqual(before)
    if (first === 'rates') matrix.resolve({ coverage, items: fixture.updatedRows, group_by: 'platform_group' })
    else rates.resolve({ 1: 2, 2: 0 })
    await flushPromises()
    const after = displayedCards(wrapper)
    expect(after.map(card => card.name)).toEqual(['Alpha updated', 'Zulu updated'])
    expect(after.map(card => card.rateMultiplier)).toEqual([0, 2])
    expect(after.every(card => card.health === 'critical' && card.bars.at(-1)?.health === 'critical')).toBe(true)
  })

  it('retains the last full batch on a monitor failure and cancels pending rates', async () => {
    useSortedFixture()
    const wrapper = mountView(); await flushPromises()
    const before = displayedCards(wrapper)
    const rates = deferred<Record<number, number>>()
    const snapshot = deferred<unknown>()
    mocks.rates.mockReturnValueOnce(rates.promise)
    mocks.groups.mockResolvedValueOnce([{ id: 1, name: 'Discarded name', rate_multiplier: 7 }])
    mocks.snapshot.mockReturnValueOnce(snapshot.promise)
    await wrapper.get('button[title="common.refresh"]').trigger('click'); await flushPromises()
    snapshot.reject(new Error('monitor offline')); await flushPromises()
    expect(displayedCards(wrapper)).toEqual(before)
    expect(wrapper.get('button[title="common.refresh"]').attributes('disabled')).toBeUndefined()
    expect((mocks.rates.mock.lastCall![0] as AbortSignal).aborted).toBe(true)
    rates.resolve({ 1: 99 }); await flushPromises()
    expect(displayedCards(wrapper)).toEqual(before)
    expect(mocks.showError).toHaveBeenCalledTimes(1)
  })

  it('hides rates only after a failed rate refresh completes, then retries on the next refresh', async () => {
    const fixture = useSortedFixture()
    const wrapper = mountView(); await flushPromises()
    const before = displayedCards(wrapper)
    const rates = deferred<Record<number, number>>()
    mocks.rates.mockReturnValueOnce(rates.promise)
    mocks.matrix.mockResolvedValueOnce({ coverage, items: fixture.updatedRows, group_by: 'platform_group' })
    await wrapper.get('button[title="common.refresh"]').trigger('click'); await flushPromises()
    expect(displayedCards(wrapper)).toEqual(before)
    rates.reject(new Error('rates offline')); await flushPromises()
    expect(wrapper.find('[data-testid="monitor-rates-error"]').exists()).toBe(true)
    expect(displayedCards(wrapper).every(card => card.rateMultiplier === null && card.health === 'critical')).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'channelMonitorV2.ranges.24h')!.trigger('click')
    await flushPromises()
    expect(mocks.rates).toHaveBeenCalledTimes(3)
    expect(displayedCards(wrapper).map(card => card.rateMultiplier)).toEqual([0, 1])
    expect(wrapper.find('[data-testid="monitor-rates-error"]').exists()).toBe(false)
  })
})
