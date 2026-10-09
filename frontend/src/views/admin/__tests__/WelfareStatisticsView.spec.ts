import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import WelfareSettingsView from '../WelfareSettingsView.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import WelfareStatisticsUsers from '@/components/admin/welfare/WelfareStatisticsUsers.vue'
import WelfareStatisticsRecords from '@/components/admin/welfare/WelfareStatisticsRecords.vue'
import WelfareStatisticsDaily from '@/components/admin/welfare/WelfareStatisticsDaily.vue'
import * as welfareStatisticsDate from '@/utils/welfareStatisticsDate'

const { getStatistics, getStatisticsUsers, getStatisticsRecords, getSettings, updateSettings } = vi.hoisted(() => ({
  getStatistics: vi.fn(), getStatisticsUsers: vi.fn(), getStatisticsRecords: vi.fn(), getSettings: vi.fn(), updateSettings: vi.fn()
}))
vi.mock('@/api/admin/welfare', () => ({ adminWelfareAPI: { getStatistics, getStatisticsUsers, getStatisticsRecords, getSettings, updateSettings } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), fetchPublicSettings: vi.fn().mockResolvedValue({}) }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh-CN' } })
}))

const totals = { daily_amount: '12.34', streak_amount: '2.00', checkin_amount: '14.34', draw_amount: '5.00', total_amount: '19.34', checkin_users: 2, checkin_count: 3, streak_users: 1, draw_users: 1, draw_count: 2, participating_users: 2 }
const overview = { date_from: '2026-10-03', date_to: '2026-10-09', timezone: 'Asia/Shanghai', summary: totals, daily: [{ ...totals, date: '2026-10-09' }] }
const users = { items: [{ user_id: 7, email: 'alice@example.com', period: totals, lifetime: { ...totals, checkin_count: 88, daily_amount: '101.01', streak_amount: '202.02', checkin_amount: '303.03', draw_amount: '404.04', total_amount: '9999999999999999.01' } }], total: 51, page: 1, page_size: 20 }
const records = { items: [{ id: 'streak:8', user_id: 7, email: 'alice@example.com', type: 'streak', created_at: '2026-10-08T16:30:00Z', business_date: '2026-10-09', amount: '2.00', cycle_day: 7 }], total: 41, page: 1, page_size: 20 }
let wrapper: VueWrapper
function createView() { wrapper = mount(WelfareSettingsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } }); return wrapper }
async function applySearch(value: string) { await wrapper.get('[data-test="stats-search"]').setValue(value); await wrapper.get('[data-test="stats-filters"]').trigger('submit'); await flushPromises() }

describe('administrator welfare statistics', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-08T16:30:00Z'))
    getStatistics.mockResolvedValue(overview)
    getStatisticsUsers.mockResolvedValue(users)
    getStatisticsRecords.mockResolvedValue(records)
    getSettings.mockResolvedValue({ enabled: false, launch_at: null, rules_version: 2 })
  })
  afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })

  it('opens statistics by default with the last seven Shanghai business dates and exact money', async () => {
    createView(); await flushPromises()
    expect(wrapper.get('[data-test="tab-statistics"]').attributes('aria-selected')).toBe('true')
    expect(getStatistics).toHaveBeenCalledWith({ date_from: '2026-10-03', date_to: '2026-10-09' }, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('$9,999,999,999,999,999.01')
    expect(wrapper.text()).toContain('88')
    expect(wrapper.text()).toContain('welfare.admin.stats.streakUsers')
  })

  it('applies the same custom date, email and user id filters to all endpoints', async () => {
    createView(); await flushPromises()
    await wrapper.get('[data-test="stats-preset"]').setValue('custom')
    await wrapper.get('[data-test="stats-date-from"]').setValue('2026-09-01')
    await wrapper.get('[data-test="stats-date-to"]').setValue('2026-09-30')
    await wrapper.get('[data-test="stats-user-id"]').setValue('7')
    await applySearch(' alice@ ')
    const common = { date_from: '2026-09-01', date_to: '2026-09-30', search: 'alice@', user_id: 7 }
    expect(getStatistics).toHaveBeenLastCalledWith(common, expect.any(AbortSignal))
    expect(getStatisticsUsers).toHaveBeenLastCalledWith(expect.objectContaining(common), expect.any(AbortSignal))
    expect(getStatisticsRecords).toHaveBeenLastCalledWith(expect.objectContaining(common), expect.any(AbortSignal))
  })

  it('shows each lifetime reward category separately from the selected period', async () => {
    createView(); await flushPromises()
    const lifetime = wrapper.get('[data-test="user-lifetime-7"]')
    for (const amount of ['$101.01', '$202.02', '$303.03', '$404.04']) expect(lifetime.text()).toContain(amount)
    expect(wrapper.text()).toContain('$2.00')
    expect(wrapper.text()).toContain('$14.34')
  })

  it('fills every date without rewards with zero without summing money on the client', async () => {
    createView(); await flushPromises()
    const rows = wrapper.findAllComponents(DataTable)[0].findAll('tbody tr')
    expect(rows).toHaveLength(7)
    expect(rows[0].text()).toContain('2026-10-03')
    expect(rows[0].text()).toContain('$0.00')
    expect(rows[6].text()).toContain('$19.34')
  })

  it('renders a valid final calendar date without incrementing beyond its year range', () => {
    const addDays = welfareStatisticsDate.addBusinessDays
    // Make stepping outside the four-digit business calendar fail immediately,
    // so this regression cannot enter an unbounded expanded-year loop.
    const calendarStep = vi.spyOn(welfareStatisticsDate, 'addBusinessDays').mockImplementation((date, days) => {
      if (date === '9999-12-31' && days > 0) throw new RangeError('Outside the four-digit business calendar')
      return addDays(date, days)
    })
    try {
      expect(() => { wrapper = mount(WelfareStatisticsDaily, { props: { daily: [], from: '9999-12-31', to: '9999-12-31', loading: false, error: false } }) }).not.toThrow()
      expect(wrapper.get('[data-test="daily-9999-12-31"]').text()).toBe('9999-12-31')
    } finally {
      calendarStep.mockRestore()
    }
  })

  it.each([['today', '2026-10-09', '2026-10-09'], ['yesterday', '2026-10-08', '2026-10-08'], ['last30', '2026-09-10', '2026-10-09'], ['month', '2026-10-01', '2026-10-09']])('uses Shanghai dates for %s preset', async (preset, from, to) => {
    createView(); await flushPromises()
    await wrapper.get('[data-test="stats-preset"]').setValue(preset)
    await wrapper.get('[data-test="stats-filters"]').trigger('submit'); await flushPromises()
    expect(getStatistics).toHaveBeenLastCalledWith({ date_from: from, date_to: to }, expect.any(AbortSignal))
  })

  it('rejects reversed or more than 366 custom dates without requesting statistics', async () => {
    createView(); await flushPromises(); getStatistics.mockClear()
    await wrapper.get('[data-test="stats-preset"]').setValue('custom')
    await wrapper.get('[data-test="stats-date-from"]').setValue('2025-01-01')
    await wrapper.get('[data-test="stats-date-to"]').setValue('2026-10-09')
    await wrapper.get('[data-test="stats-filters"]').trigger('submit'); await flushPromises()
    expect(getStatistics).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('welfare.admin.stats.invalidRange')
    await wrapper.get('[data-test="stats-date-from"]').setValue('2026-10-09')
    await wrapper.get('[data-test="stats-date-to"]').setValue('2026-10-08')
    await wrapper.get('[data-test="stats-filters"]').trigger('submit'); await flushPromises()
    expect(getStatistics).not.toHaveBeenCalled()
  })

  it('accepts exactly 366 dates and rejects invalid user ids', async () => {
    createView(); await flushPromises()
    await wrapper.get('[data-test="stats-preset"]').setValue('custom')
    await wrapper.get('[data-test="stats-date-from"]').setValue('2025-10-09')
    await wrapper.get('[data-test="stats-date-to"]').setValue('2026-10-09')
    await wrapper.get('[data-test="stats-filters"]').trigger('submit'); await flushPromises()
    expect(getStatistics).toHaveBeenLastCalledWith({ date_from: '2025-10-09', date_to: '2026-10-09' }, expect.any(AbortSignal))
    getStatistics.mockClear()
    await wrapper.get('[data-test="stats-user-id"]').setValue('-7')
    await wrapper.get('[data-test="stats-filters"]').trigger('submit'); await flushPromises()
    expect(getStatistics).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('welfare.admin.stats.invalidUserId')
  })

  it('drills into a date and user and can clear the detail scope without changing overview totals', async () => {
    createView(); await flushPromises(); getStatistics.mockClear(); getStatisticsUsers.mockClear()
    await wrapper.get('[data-test="daily-2026-10-09"]').trigger('click'); await flushPromises()
    expect(getStatisticsRecords).toHaveBeenLastCalledWith(expect.objectContaining({ date_from: '2026-10-09', date_to: '2026-10-09', page: 1 }), expect.any(AbortSignal))
    await wrapper.get('[data-test="user-7"]').trigger('click'); await flushPromises()
    expect(getStatisticsRecords).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: 7, date_from: '2026-10-09', date_to: '2026-10-09' }), expect.any(AbortSignal))
    expect(wrapper.get('[data-test="records-scope"]').text()).toContain('2026-10-09')
    expect(wrapper.get('[data-test="records-scope"]').text()).toContain('alice@example.com')
    await wrapper.get('[data-test="records-clear-scope"]').trigger('click'); await flushPromises()
    expect(getStatisticsRecords).toHaveBeenLastCalledWith(expect.objectContaining({ date_from: '2026-10-03', date_to: '2026-10-09', type: 'all' }), expect.any(AbortSignal))
    expect(getStatistics).not.toHaveBeenCalled(); expect(getStatisticsUsers).not.toHaveBeenCalled()
  })

  it('loads server pages and sorting and only offers reward record types', async () => {
    createView(); await flushPromises()
    wrapper.findAllComponents(DataTable)[1].vm.$emit('sort', 'period_total_amount', 'asc'); await flushPromises()
    expect(getStatisticsUsers).toHaveBeenLastCalledWith(expect.objectContaining({ sort_by: 'period_total_amount', sort_order: 'asc', page: 1 }), expect.any(AbortSignal))
    wrapper.findAllComponents(Pagination)[0].vm.$emit('update:page', 2); await flushPromises()
    expect(getStatisticsUsers).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }), expect.any(AbortSignal))
    await wrapper.get('[data-test="records-type"]').setValue('streak'); await flushPromises()
    wrapper.findAllComponents(Pagination)[1].vm.$emit('update:page', 2); await flushPromises()
    expect(getStatisticsRecords).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'streak', page: 2 }), expect.any(AbortSignal))
    expect(wrapper.get('[data-test="records-type"]').find('option[value="redeem"]').exists()).toBe(false)
    wrapper.findAllComponents(Pagination)[0].vm.$emit('update:pageSize', 500); await flushPromises()
    expect(getStatisticsUsers).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, page_size: 100 }), expect.any(AbortSignal))
    wrapper.findAllComponents(Pagination)[1].vm.$emit('update:pageSize', 100); await flushPromises()
    expect(getStatisticsRecords).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, page_size: 100 }), expect.any(AbortSignal))
  })

  it('changes server sorting through native keyboard-accessible controls and keeps headers synchronized', async () => {
    createView(); await flushPromises()
    expect(wrapper.get('[data-test="users-sort-by"]').element.tagName).toBe('SELECT')
    expect(wrapper.get('[data-test="users-sort-order"]').element.tagName).toBe('SELECT')
    wrapper.findAllComponents(Pagination)[0].vm.$emit('update:page', 2); await flushPromises()
    await wrapper.get('[data-test="users-sort-by"]').setValue('period_total_amount'); await flushPromises()
    await wrapper.get('[data-test="users-sort-order"]').setValue('asc'); await flushPromises()
    expect(getStatisticsUsers).toHaveBeenLastCalledWith(expect.objectContaining({ sort_by: 'period_total_amount', sort_order: 'asc', page: 1 }), expect.any(AbortSignal))
    const userTable = wrapper.findAllComponents(DataTable)[1]
    expect(userTable.findAll('th').filter(th => th.attributes('aria-sort') === 'ascending').map(th => th.text())).toEqual(['welfare.admin.stats.periodTotal'])
    await userTable.findAll('th').find(th => th.text().includes('welfare.admin.stats.lifetimeTotal'))!.trigger('click'); await flushPromises()
    expect((wrapper.get('[data-test="users-sort-by"]').element as HTMLSelectElement).value).toBe('total_amount')
    expect((wrapper.get('[data-test="users-sort-order"]').element as HTMLSelectElement).value).toBe('asc')
    expect(getStatisticsUsers).toHaveBeenLastCalledWith(expect.objectContaining({ sort_by: 'total_amount', sort_order: 'asc', page: 1 }), expect.any(AbortSignal))
  })

  it('provides the same server sorting controls on mobile without table headers', async () => {
    const viewport = vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: false, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() }))
    try {
      createView(); await flushPromises()
      expect(wrapper.findAllComponents(DataTable)[1].find('th').exists()).toBe(false)
      await wrapper.get('[data-test="users-sort-by"]').setValue('checkin_count'); await flushPromises()
      await wrapper.get('[data-test="users-sort-order"]').setValue('asc'); await flushPromises()
      expect(getStatisticsUsers).toHaveBeenLastCalledWith(expect.objectContaining({ sort_by: 'checkin_count', sort_order: 'asc', page: 1 }), expect.any(AbortSignal))
    } finally {
      viewport.mockRestore()
    }
  })

  it('keeps detail dates inside the overview and resets detail scopes when applying overview filters', async () => {
    createView(); await flushPromises(); getStatisticsRecords.mockClear()
    await wrapper.get('[data-test="records-date-from"]').setValue('2026-10-01'); await flushPromises()
    expect(getStatisticsRecords).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('welfare.admin.stats.detailRangeError')
    await wrapper.get('[data-test="records-date-from"]').setValue('2026-10-04'); await flushPromises()
    await wrapper.get('[data-test="user-7"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="records-type"]').setValue('draw'); await flushPromises()
    expect(getStatisticsRecords).toHaveBeenLastCalledWith(expect.objectContaining({ date_from: '2026-10-04', user_id: 7, type: 'draw' }), expect.any(AbortSignal))
    await applySearch('alice@')
    const query = getStatisticsRecords.mock.calls.at(-1)![0]
    expect(query).toMatchObject({ date_from: '2026-10-03', date_to: '2026-10-09', search: 'alice@', type: 'all', page: 1 })
    expect(query).not.toHaveProperty('user_id')
    expect(wrapper.find('[data-test="records-clear-scope"]').exists()).toBe(false)
  })

  it('ignores old requests after applying a newer search', async () => {
    let resolveOld!: (value: typeof overview) => void
    getStatistics.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    createView(); await flushPromises()
    await applySearch('new@example.com')
    resolveOld({ ...overview, summary: { ...totals, total_amount: '777.00' } }); await flushPromises()
    expect(wrapper.get('[data-test="summary-total_amount"]').text()).toContain('$19.34')
    expect(wrapper.get('[data-test="summary-total_amount"]').text()).not.toContain('$777.00')
    expect(getStatistics.mock.calls[0][1].aborted).toBe(true)
  })

  it('also ignores old user and reward record responses and old errors', async () => {
    let rejectUsers!: (value: Error) => void
    let resolveRecords!: (value: typeof records) => void
    getStatisticsUsers.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectUsers = reject }))
    getStatisticsRecords.mockImplementationOnce(() => new Promise(resolve => { resolveRecords = resolve }))
    createView(); await flushPromises()
    await applySearch('alice@')
    rejectUsers(new Error('old failed request'))
    resolveRecords({ ...records, items: [{ ...records.items[0], email: 'old@example.com', amount: '777.00' }] }); await flushPromises()
    expect(wrapper.text()).not.toContain('old@example.com')
    expect(wrapper.text()).not.toContain('$777.00')
    expect(wrapper.text()).not.toContain('welfare.admin.stats.loadFailed')
    expect(getStatisticsUsers.mock.calls[0][1].aborted).toBe(true)
    expect(getStatisticsRecords.mock.calls[0][1].aborted).toBe(true)
  })

  it('shows independent user and record errors with retries', async () => {
    getStatisticsUsers.mockRejectedValueOnce(new Error('offline'))
    getStatisticsRecords.mockRejectedValueOnce(new Error('offline'))
    createView(); await flushPromises()
    const userPanel = wrapper.getComponent(WelfareStatisticsUsers)
    const recordPanel = wrapper.getComponent(WelfareStatisticsRecords)
    expect(userPanel.find('[role="alert"]').exists()).toBe(true)
    expect(recordPanel.find('[role="alert"]').exists()).toBe(true)
    await userPanel.get('[role="alert"] button').trigger('click')
    await recordPanel.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(userPanel.text()).toContain('alice@example.com')
    expect(recordPanel.text()).toContain('alice@example.com')
    expect(userPanel.find('[role="alert"]').exists()).toBe(false)
    expect(recordPanel.find('[role="alert"]').exists()).toBe(false)
  })

  it('shows empty user and reward records, and retries failed summary loading', async () => {
    getStatistics.mockRejectedValueOnce(new Error('offline'))
    getStatisticsUsers.mockResolvedValue({ ...users, items: [], total: 0 })
    getStatisticsRecords.mockResolvedValue({ ...records, items: [], total: 0 })
    createView(); await flushPromises()
    expect(wrapper.text()).toContain('welfare.admin.stats.loadFailed')
    expect(wrapper.text()).toContain('welfare.admin.stats.emptyUsers')
    expect(wrapper.text()).toContain('welfare.admin.stats.emptyRecords')
    await wrapper.get('[data-test="summary-retry"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="summary-total_amount"]').text()).toContain('$19.34')
  })
})
