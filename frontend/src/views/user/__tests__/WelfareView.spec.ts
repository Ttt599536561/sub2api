import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import WelfareView from '../WelfareView.vue'
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
import { welfareAPI } from '@/api/welfare'
const { welfareLocale } = vi.hoisted(() => ({ welfareLocale: { value: 'zh' as 'zh' | 'en' } }))
vi.mock('vue-i18n', async () => {
  const { default: zh } = await import('@/i18n/locales/zh/welfare')
  const { default: en } = await import('@/i18n/locales/en/welfare')
  return { useI18n: () => ({ locale: welfareLocale, t: (key: string, params: Record<string, unknown> = {}) => {
    const text = key.split('.').reduce((value: any, part) => value?.[part], welfareLocale.value === 'zh' ? zh : en) || key
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
  welfareLocale.value = 'zh'
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
  it.each([
    { locale: 'zh' as const, progress: 'API 余额消费进度', apiThreshold: 'API 余额每消费 $40.00 获得 1 次', subscriptionThreshold: '套餐单笔实付每满 ¥75.00 送 1 次', perOrder: '每笔独立计算，余数不累计', rulesLabel: '活动规则', refund: '退款确认后，按该订单剩余实付金额重新计算赠送次数', rewardsKept: '已获得的福利余额不收回' },
    { locale: 'en' as const, progress: 'API balance spending progress', apiThreshold: 'One draw per $40.00 in API balance spending', subscriptionThreshold: 'One draw per ¥75.00 paid on each subscription order', perOrder: 'Each order is calculated separately; remainders do not carry over', rulesLabel: 'Program rules', refund: 'After a refund is confirmed, draws are recalculated from the amount still paid on that order', rewardsKept: 'Welfare funds already earned are kept' },
  ])('separates API dollar progress from per-order yuan subscription rewards in $locale', async ({ locale, progress, apiThreshold, subscriptionThreshold, perOrder, rulesLabel, refund, rewardsKept }) => {
    welfareLocale.value = locale
    vi.mocked(welfareAPI.rules).mockResolvedValue({ prizes: [], draw_threshold: '40.00', timezone: 'Asia/Shanghai', subscription_draw_threshold: '75.00', subscription_draw_currency: 'CNY' })
    vi.mocked(welfareAPI.overview).mockResolvedValue({ ...initial, available_draws: 2, subscription_draws: 7, next_draw_remaining: '10.00' })
    await start()

    const drawsCard = wrapper.findAll('.wf-stat')[1]
    expect(drawsCard.get('strong').text()).toMatch(/^2\s/)
    expect(drawsCard.text()).toContain(apiThreshold)
    expect(drawsCard.text()).toContain(subscriptionThreshold)
    const lottery = wrapper.get('.wf-lottery')
    expect(lottery.text()).toContain(subscriptionThreshold)
    expect(lottery.text()).toContain(perOrder)
    expect(lottery.get('[role="progressbar"]').attributes('aria-label')).toBe(progress)
    expect(lottery.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('75')
    expect(lottery.text()).not.toContain('$75.00')

    await wrapper.get('.wf-heading').findAll('button').find(button => button.text() === rulesLabel)!.trigger('click')
    const rulesDialog = wrapper.get('[role="dialog"]')
    expect(rulesDialog.text()).toContain('$40.00')
    expect(rulesDialog.text()).toContain('¥75.00')
    expect(rulesDialog.text()).toContain(refund)
    expect(rulesDialog.text()).toContain(rewardsKept)
  })

  it.each([
    { name: 'omits the new rules', extra: {} },
    { name: 'returns a currency other than CNY', extra: { subscription_draw_currency: 'USD', subscription_draw_threshold: '50.00' } },
  ])('does not announce yuan subscription rewards when the server $name', async ({ extra }) => {
    vi.mocked(welfareAPI.rules).mockResolvedValue({ prizes: [], draw_threshold: '50.00', timezone: 'Asia/Shanghai', ...extra })
    await start()
    expect(wrapper.text()).toContain('API 余额每消费 $50.00 获得 1 次')
    expect(wrapper.text()).not.toContain('套餐单笔实付')
    await wrapper.get('.wf-heading').findAll('button').find(button => button.text() === '活动规则')!.trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).not.toContain('人民币订阅订单')
  })

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
  it('keeps redemption input and an in-flight quote when refreshing the same user', async () => {
    await start()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    await wrapper.get('#welfare-redemption-amount').setValue('1.00')
    let resolve!: (value: Awaited<ReturnType<typeof welfareAPI.quote>>) => void
    vi.mocked(welfareAPI.quote).mockReturnValueOnce(new Promise(r => { resolve = r }))
    await wrapper.get('[data-testid="quote-partial"]').trigger('click')
    const signal = vi.mocked(welfareAPI.quote).mock.calls[0][1]

    auth.user = { ...auth.user }
    await flushPromises()

    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(wrapper.get<HTMLInputElement>('#welfare-redemption-amount').element.value).toBe('1.00')
    expect(signal?.aborted).toBe(false)
    resolve({ amount: '1.00', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '19.35', welfare_balance_version: 1 })
    await flushPromises()
    expect(wrapper.find('[data-testid="confirm-redeem"]').exists()).toBe(true)
    expect(wrapper.get('[role="dialog"]').text()).toContain('$19.35')
  })
  it.each([
    { change: 'account ID', sessionID: 'first', userID: 2 },
    { change: 'session', sessionID: 'second', userID: 1 }
  ])('clears redemption input when the $change changes', async ({ sessionID, userID }) => {
    await start()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    await wrapper.get('#welfare-redemption-amount').setValue('1.00')

    localStorage.setItem('auth_session_id', sessionID)
    localStorage.setItem('auth_user', JSON.stringify({ id: userID }))
    auth.sessionRevision = sessionID; auth.user = { id: userID }
    await flushPromises()

    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    expect(wrapper.get<HTMLInputElement>('#welfare-redemption-amount').element.value).toBe('')
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
  it('keeps the transfer success notice after refreshing the same account data', async () => {
    await start()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    vi.mocked(welfareAPI.quote).mockResolvedValue({ amount: '2.80', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '21.15', welfare_balance_version: 1 })
    await wrapper.get('[data-testid="redeem-all"]').trigger('click'); await flushPromises()
    auth.refreshUser.mockImplementationOnce(async () => { auth.user = { ...auth.user }; return {} })
    const after = { ...initial, welfare_balance: '0.00', account_balance: '21.15', wallet_version: 2, welfare_balance_version: 2 }
    vi.mocked(welfareAPI.redeem).mockResolvedValue({ operation_id: 'transfer', status: 'completed', overview: after })
    vi.mocked(welfareAPI.overview).mockResolvedValue(after)

    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()

    expect(auth.refreshUser).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('$2.80 已转入账户余额')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
  it.each([
    { completion: 'background recovery with the dialog open', closeDialog: false, retry: false },
    { completion: 'background recovery with the dialog closed', closeDialog: true, retry: false },
    { completion: 'the pending-transfer retry button', closeDialog: true, retry: true }
  ])('allows a new redemption after $completion', async ({ closeDialog, retry }) => {
    await start()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    await wrapper.get('#welfare-redemption-amount').setValue('1.00')
    vi.mocked(welfareAPI.quote).mockResolvedValue({ amount: '1.00', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '19.35', welfare_balance_version: 1 })
    await wrapper.get('[data-testid="quote-partial"]').trigger('click'); await flushPromises()
    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce(new Error('response lost'))
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    const firstRequest = vi.mocked(welfareAPI.redeem).mock.calls[0]
    expect(wrapper.get('#welfare-redemption-amount').attributes('disabled')).toBeDefined()
    if (closeDialog) {
      await wrapper.get('[role="dialog"]').findAll('button').find(button => button.text() === '取消')!.trigger('click')
    }

    const after = { ...initial, welfare_balance: '1.80', account_balance: '19.35', wallet_version: 2, welfare_balance_version: 2 }
    const completed = { operation_id: 'transfer', status: 'completed' as const, amount: '1.00', overview: after }
    vi.mocked(welfareAPI.overview).mockResolvedValue(after)
    if (retry) {
      vi.mocked(welfareAPI.redeem).mockResolvedValueOnce(completed)
      await wrapper.get('[data-testid="retry-pending-redemption"]').trigger('click'); await flushPromises()
      expect(vi.mocked(welfareAPI.redeem).mock.calls[1].slice(0, 2)).toEqual(firstRequest.slice(0, 2))
    } else {
      vi.mocked(welfareAPI.operationByKey).mockResolvedValueOnce(completed)
      window.dispatchEvent(new Event('focus')); await flushPromises()
      expect(welfareAPI.operationByKey).toHaveBeenCalledWith('redeem', firstRequest[1], expect.any(AbortSignal))
      expect(welfareAPI.redeem).toHaveBeenCalledOnce()
    }
    expect(wrapper.text()).toContain('$1.00 已转入账户余额')
    expect(wrapper.find('[data-testid="retry-pending-redemption"]').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)

    await wrapper.get('[data-testid="open-redemption"]').trigger('click'); await flushPromises()
    expect(wrapper.get<HTMLInputElement>('#welfare-redemption-amount').element.value).toBe('')
    expect(wrapper.get('#welfare-redemption-amount').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="redeem-all"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="confirm-redeem"]').exists()).toBe(false)
    await wrapper.get('#welfare-redemption-amount').setValue('0.50')
    vi.mocked(welfareAPI.quote).mockResolvedValueOnce({ amount: '0.50', welfare_balance: '1.80', account_balance: '19.35', account_balance_after: '19.85', welfare_balance_version: 2 })
    await wrapper.get('[data-testid="quote-partial"]').trigger('click'); await flushPromises()
    const next = { ...after, welfare_balance: '1.30', account_balance: '19.85', wallet_version: 3, welfare_balance_version: 3 }
    vi.mocked(welfareAPI.overview).mockResolvedValue(next)
    vi.mocked(welfareAPI.redeem).mockResolvedValueOnce({ operation_id: 'next-transfer', status: 'completed', amount: '0.50', overview: next })
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    const lastRequest = vi.mocked(welfareAPI.redeem).mock.calls.at(-1)!
    expect(lastRequest[0]).toEqual({ amount: '0.50', welfare_balance_version: 2 })
    expect(lastRequest[1]).not.toBe(firstRequest[1])
    expect(wrapper.text()).toContain('$0.50 已转入账户余额')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
  it('sends record filters and rejects reversed date ranges', async () => {
    await start()
    await wrapper.get('select').setValue('draw'); await flushPromises()
    expect(welfareAPI.records).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'draw', page: 1 }), expect.any(AbortSignal))
    await wrapper.findAll('input[type="date"]')[0].setValue('2026-09-20')
    await wrapper.findAll('input[type="date"]')[1].setValue('2026-09-01'); await flushPromises()
    expect(wrapper.text()).toContain('开始日期不能晚于结束日期')
  })
  it('allows a fresh quote after a page retry definitively rejects an unresolved transfer', async () => {
    await start()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    await wrapper.get('#welfare-redemption-amount').setValue('1.00')
    vi.mocked(welfareAPI.quote).mockResolvedValueOnce({ amount: '1.00', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '19.35', welfare_balance_version: 1 })
    await wrapper.get('[data-testid="quote-partial"]').trigger('click'); await flushPromises()
    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce(new Error('request lost'))
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    const originalKey = vi.mocked(welfareAPI.redeem).mock.calls[0][1]
    await wrapper.get('[role="dialog"]').findAll('button').find(button => button.text() === '取消')!.trigger('click')

    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce({ status: 409, reason: 'WELFARE_QUOTE_STALE' })
    await wrapper.get('[data-testid="retry-pending-redemption"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="retry-pending-redemption"]').exists()).toBe(false)
    expect(auth.refreshUser).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click'); await flushPromises()
    expect(wrapper.get('#welfare-redemption-amount').attributes('disabled')).toBeUndefined()
    expect(wrapper.get<HTMLInputElement>('#welfare-redemption-amount').element.value).toBe('')
    expect(wrapper.find('[data-testid="confirm-redeem"]').exists()).toBe(false)

    await wrapper.get('#welfare-redemption-amount').setValue('0.50')
    vi.mocked(welfareAPI.quote).mockResolvedValueOnce({ amount: '0.50', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '18.85', welfare_balance_version: 2 })
    await wrapper.get('[data-testid="quote-partial"]').trigger('click'); await flushPromises()
    vi.mocked(welfareAPI.redeem).mockResolvedValueOnce({ operation_id: 'new-transfer', status: 'completed', overview: { ...initial, wallet_version: 3, welfare_balance_version: 3 } })
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    expect(vi.mocked(welfareAPI.redeem).mock.calls[2][0]).toEqual({ amount: '0.50', welfare_balance_version: 2 })
    expect(vi.mocked(welfareAPI.redeem).mock.calls[2][1]).not.toBe(originalKey)
    expect(wrapper.text()).toContain('$0.50 已转入账户余额')
  })
  it.each(['dialog', 'page'])('retains the original quote and key after an uncertain transfer has a rate-limited %s retry', async retryFrom => {
    await start()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    vi.mocked(welfareAPI.quote).mockResolvedValueOnce({ amount: '1.00', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '19.35', welfare_balance_version: 1 })
    await wrapper.get('[data-testid="redeem-all"]').trigger('click'); await flushPromises()
    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce(new Error('response lost'))
      .mockRejectedValueOnce({ status: 429, code: 'RATE_LIMITED' })
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    const original = vi.mocked(welfareAPI.redeem).mock.calls[0]
    await wrapper.get(`[data-testid="${retryFrom === 'dialog' ? 'confirm-redeem' : 'retry-pending-redemption'}"]`).trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="retry-pending-redemption"]').exists()).toBe(true)
    expect(wrapper.get<HTMLInputElement>('#welfare-redemption-amount').element.value).toBe('1.00')
    expect(wrapper.get('#welfare-redemption-amount').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="confirm-redeem"]').text()).toBe('重试本次兑换')
    expect(vi.mocked(welfareAPI.redeem).mock.calls[1].slice(0, 2)).toEqual(original.slice(0, 2))

    vi.mocked(welfareAPI.redeem).mockResolvedValueOnce({ operation_id: 'original', status: 'completed', overview: { ...initial, wallet_version: 2, welfare_balance_version: 2 } })
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    expect(vi.mocked(welfareAPI.redeem).mock.calls[2].slice(0, 2)).toEqual(original.slice(0, 2))
    expect(wrapper.find('[data-testid="retry-pending-redemption"]').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
  it('keeps the dialog editable when its first transfer attempt is rate limited', async () => {
    await start()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    vi.mocked(welfareAPI.quote).mockResolvedValueOnce({ amount: '1.00', welfare_balance: '2.80', account_balance: '18.35', account_balance_after: '19.35', welfare_balance_version: 1 })
    await wrapper.get('[data-testid="redeem-all"]').trigger('click'); await flushPromises()
    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce({ status: 429, code: 'RATE_LIMITED' })
    await wrapper.get('[data-testid="confirm-redeem"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="retry-pending-redemption"]').exists()).toBe(false)
    expect(wrapper.get('#welfare-redemption-amount').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="confirm-redeem"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quote-partial"]').exists()).toBe(true)
  })
  it('ignores an old page retry failure after switching accounts and opening a new dialog', async () => {
    sessionStorage.setItem('welfare.pending:1:first', JSON.stringify({ drawKey: null, redemption: { key: 'old-transfer', body: { amount: '1.00', welfare_balance_version: 1 } } }))
    vi.mocked(welfareAPI.operationByKey).mockRejectedValueOnce({ status: 404 })
    await start()
    let reject!: (error: unknown) => void
    vi.mocked(welfareAPI.redeem).mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    await wrapper.get('[data-testid="retry-pending-redemption"]').trigger('click')
    localStorage.setItem('auth_session_id', 'second'); localStorage.setItem('auth_user', JSON.stringify({ id: 2 }))
    auth.sessionRevision = 'second'; auth.user = { id: 2 }
    await flushPromises()
    await wrapper.get('[data-testid="open-redemption"]').trigger('click')
    await wrapper.get('#welfare-redemption-amount').setValue('0.50')
    reject({ status: 409, reason: 'WELFARE_QUOTE_STALE' }); await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(wrapper.get<HTMLInputElement>('#welfare-redemption-amount').element.value).toBe('0.50')
    expect(auth.refreshUser).not.toHaveBeenCalled()
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
