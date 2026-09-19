import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import WelfareView from '../WelfareView.vue'
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
import { welfareAPI } from '@/api/welfare'
vi.mock('vue-i18n', async () => {
  const { default: messages } = await import('@/i18n/locales/zh/welfare')
  return { useI18n: () => ({ locale: { value: 'zh' }, t: (key: string, params: Record<string, unknown> = {}) => {
    const text = key.split('.').reduce((value: any, part) => value?.[part], messages) || key
    return text.replace(/\{(\w+)\}/g, (_match: string, name: string) => String(params[name] ?? ''))
  } }) }
})
vi.mock('@/api/welfare', () => ({ welfareAPI: { overview: vi.fn(), calendar: vi.fn(), records: vi.fn(), rules: vi.fn(), checkIn: vi.fn(), draw: vi.fn(), quote: vi.fn(), redeem: vi.fn(), operationByKey: vi.fn() } }))
const auth = reactive({ sessionRevision: 'first', user: { id: 1 }, isAuthenticated: true, refreshUser: vi.fn().mockResolvedValue({}) })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const initial = { welfare_balance: '2.80', account_balance: '18.35', available_draws: 0, draws_used: 2, total_checkin_days: 23, cycle_day: 6, today_checked_in: false, business_date: '2026-09-19', next_reset_at: '2026-09-20T00:00:00+08:00', eligible_spend: '41.17', next_draw_remaining: '8.83', ticket_debt: 0, wallet_version: 1, welfare_balance_version: 1, rewards_enabled: true, rules_version: 1, milestones: [{ day: 7, status: 'locked' as const }, { day: 15, status: 'locked' as const }, { day: 30, status: 'locked' as const }] }
let wrapper: ReturnType<typeof mount>
let router: Router
async function start(location = '/welfare') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/welfare', component: WelfareView }] })
  await router.push(location)
  await router.isReady()
  wrapper = mount(WelfareView, { global: { plugins: [router], stubs: { AppLayout: { template: '<main><slot /></main>' }, BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{title}}</h2><slot/><slot name="footer"/></section>' } } } })
  await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear(); sessionStorage.clear()
  localStorage.setItem('auth_session_id', 'first'); localStorage.setItem('auth_user', JSON.stringify({ id: 1 })); localStorage.setItem('auth_token', 'token-1')
  auth.sessionRevision = 'first'; auth.user = { id: 1 }
  vi.mocked(welfareAPI.overview).mockResolvedValue({ ...initial })
  vi.mocked(welfareAPI.calendar).mockResolvedValue({ month: '2026-09', days: [{ date: '2026-09-18', checked_in: true, reward_amount: '0.08' }] })
  vi.mocked(welfareAPI.records).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 10, total_draws: 2 })
  vi.mocked(welfareAPI.rules).mockResolvedValue({ prizes: ['0.50', '1', '2', '5', '10', '20', '50'].map((amount, index) => ({ id: String(index), name: amount, amount, probability: '1%' })), draw_threshold: '50.00', timezone: 'Asia/Shanghai' })
})
afterEach(() => wrapper?.unmount())
describe('welfare user interactions', () => {
  it('shows a real month and mystery milestones, with redemption usable at zero draws', async () => {
    await start()
    expect(wrapper.findAll('.wf-day')).toHaveLength(30)
    expect(wrapper.findAll('.wf-milestone').map(item => item.text()).every(text => text.includes('神秘奖励') && !text.includes('$'))).toBe(true)
    expect(wrapper.text()).toContain('今日惊喜，签到揭晓')
    expect(wrapper.get('[data-testid="draw"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="open-redemption"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.findAll('.wf-prize')).toHaveLength(7)
    expect(wrapper.html().indexOf('wf-exchange')).toBeGreaterThan(wrapper.html().indexOf('wf-prizes'))
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('兑换账户余额')
  })
  it('reveals credited check-in amounts and refreshes calendar and history', async () => {
    await start()
    const after = { ...initial, today_checked_in: true, welfare_balance: '3.48', cycle_day: 7, total_checkin_days: 24, wallet_version: 2, welfare_balance_version: 2 }
    vi.mocked(welfareAPI.checkIn).mockResolvedValue({ operation_id: 'check', status: 'completed', reward_amount: '0.68', base_reward_amount: '0.08', streak_reward_amount: '0.60', overview: after })
    vi.mocked(welfareAPI.overview).mockResolvedValue(after)
    const calendarCalls = vi.mocked(welfareAPI.calendar).mock.calls.length
    await wrapper.get('[data-testid="check-in"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('每日奖励 $0.08')
    expect(wrapper.text()).toContain('连续签到奖励 $0.60')
    expect(wrapper.get('[data-testid="check-in"]').attributes('disabled')).toBeDefined()
    expect(vi.mocked(welfareAPI.calendar).mock.calls.length).toBeGreaterThan(calendarCalls)
    expect(auth.refreshUser).not.toHaveBeenCalled()
  })
  it('shows the returned draw prize and current remaining tickets', async () => {
    vi.mocked(welfareAPI.overview).mockResolvedValue({ ...initial, available_draws: 1 })
    await start()
    const after = { ...initial, welfare_balance: '3.80', welfare_balance_version: 2, wallet_version: 2 }
    vi.mocked(welfareAPI.draw).mockResolvedValue({ operation_id: 'draw', status: 'completed', reward_amount: '1.00', overview: after })
    vi.mocked(welfareAPI.overview).mockResolvedValue(after)
    await wrapper.get('[data-testid="draw"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('+$1.00')
    expect(wrapper.get('[role="dialog"]').text()).toContain('剩余 0 次')
  })
  it('allows redemption while rewards are paused and refreshes account data after transfer', async () => {
    vi.mocked(welfareAPI.overview).mockResolvedValue({ ...initial, rewards_enabled: false })
    await start()
    expect(wrapper.get('[data-testid="check-in"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    vi.mocked(welfareAPI.quote).mockResolvedValue({ amount: '2.80', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '21.15', welfare_balance_version: 1 })
    await wrapper.get('[data-testid="redeem-all"]').trigger('click'); await flushPromises()
    auth.refreshUser.mockRejectedValueOnce(new Error('account refresh offline'))
    vi.mocked(welfareAPI.redeem).mockResolvedValue({ operation_id: 'transfer', status: 'completed', overview: { ...initial, welfare_balance: '0.00', wallet_version: 2, welfare_balance_version: 2 } })
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    expect(auth.refreshUser).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('$2.80 已转入账户余额')
  })
  it('sends record filters and rejects reversed date ranges', async () => {
    await start()
    await wrapper.get('select').setValue('draw'); await flushPromises()
    expect(welfareAPI.records).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'draw', page: 1 }), expect.any(AbortSignal))
    await wrapper.findAll('input[type="date"]')[0].setValue('2026-09-20')
    await wrapper.findAll('input[type="date"]')[1].setValue('2026-09-01'); await flushPromises()
    expect(wrapper.text()).toContain('开始日期不能晚于结束日期')
  })
  it('restores record filters and page from the URL, then saves edits with page reset', async () => {
    vi.mocked(welfareAPI.records).mockResolvedValue({ items: [], total: 50, page: 3, page_size: 10, total_draws: 2 })
    await start('/welfare?type=redeem&date_from=2026-09-01&date_to=2026-09-19&page=3&keep=yes')
    expect(welfareAPI.records).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'redeem', date_from: '2026-09-01', date_to: '2026-09-19', page: 3 }), expect.any(AbortSignal))
    expect(wrapper.get('select').element.value).toBe('redeem')
    expect(wrapper.findAll('input[type="date"]')[0].element.value).toBe('2026-09-01')
    await wrapper.get('select').setValue('draw'); await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ type: 'draw', date_from: '2026-09-01', date_to: '2026-09-19', keep: 'yes' })
    expect(welfareAPI.records).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'draw', page: 1 }), expect.any(AbortSignal))
    await wrapper.get('[aria-label="下一页"]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('2')
  })
  it('loads filters again when browser navigation changes the query', async () => {
    await start('/welfare?type=daily&page=2')
    await router.push('/welfare?type=streak&page=4'); await flushPromises()
    expect(welfareAPI.records).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'streak', page: 4 }), expect.any(AbortSignal))
    router.back(); await flushPromises()
    expect(welfareAPI.records).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'daily', page: 2 }), expect.any(AbortSignal))
  })
  it('rejects malformed filters from the URL before fetching records', async () => {
    await start('/welfare?type=unknown&date_from=2026-02-30&date_to=bad&page=1000001')
    expect(welfareAPI.records).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'all', date_from: '', date_to: '', page: 1 }), expect.any(AbortSignal))
    expect(router.currentRoute.value.query).toEqual({})
  })
})
