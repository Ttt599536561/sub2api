import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useWelfare } from '../useWelfare'
import { welfareAPI } from '@/api/welfare'

vi.mock('@/api/welfare', () => ({ welfareAPI: { overview: vi.fn(), calendar: vi.fn(), records: vi.fn(), rules: vi.fn(), checkIn: vi.fn(), draw: vi.fn(), quote: vi.fn(), redeem: vi.fn(), operationByKey: vi.fn() } }))
const auth = reactive({ sessionRevision: 'first', user: { id: 1 }, isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const snapshot = (version = 1, balance = '2.80') => ({ welfare_balance: balance, account_balance: '18.35', available_draws: 0, draws_used: 2, total_checkin_days: 23, cycle_day: 6, today_checked_in: false, business_date: '2026-09-19', next_reset_at: '2026-09-20T00:00:00+08:00', eligible_spend: '41.17', next_draw_remaining: '8.83', ticket_debt: 0, wallet_version: version, welfare_balance_version: version, rewards_enabled: true, rules_version: 1, milestones: [] })
let state: ReturnType<typeof useWelfare>
let wrapper: ReturnType<typeof mount>
function start() { wrapper = mount(defineComponent({ setup() { state = useWelfare(); return () => null } })); return flushPromises() }
beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear(); sessionStorage.clear()
  localStorage.setItem('auth_session_id', 'first'); localStorage.setItem('auth_user', JSON.stringify({ id: 1 })); localStorage.setItem('auth_token', 'token-1')
  vi.mocked(welfareAPI.operationByKey).mockRejectedValue({ status: 404 })
  auth.sessionRevision = 'first'; auth.user = { id: 1 }; auth.isAuthenticated = true
  vi.mocked(welfareAPI.overview).mockResolvedValue(snapshot())
  vi.mocked(welfareAPI.calendar).mockResolvedValue({ month: '2026-09', days: [] })
  vi.mocked(welfareAPI.records).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 10, total_draws: 2 })
  vi.mocked(welfareAPI.rules).mockResolvedValue({ prizes: [], draw_threshold: '50.00', timezone: 'Asia/Shanghai' })
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.unstubAllGlobals() })
describe('welfare state', () => {
  it('keeps an in-flight redemption when refreshing the same user', async () => {
    await start()
    let resolve!: (value: Awaited<ReturnType<typeof welfareAPI.redeem>>) => void
    vi.mocked(welfareAPI.redeem).mockReturnValueOnce(new Promise(r => { resolve = r }))
    const redemption = state.redeem({ amount: '1.00', welfare_balance_version: 1 })
    const signal = vi.mocked(welfareAPI.redeem).mock.calls[0][2]
    const pending = state.pendingRedemption.value
    const overview = state.overview.value

    auth.user = { ...auth.user }
    await flushPromises()

    expect(signal?.aborted).toBe(false)
    expect(state.busy.value).toBe(true)
    expect(state.pendingRedemption.value).toEqual(pending)
    expect(state.overview.value).toBe(overview)
    expect(welfareAPI.overview).toHaveBeenCalledOnce()
    resolve({ operation_id: 'transfer', status: 'completed', overview: snapshot(2, '1.80') })
    expect((await redemption)?.operation_id).toBe('transfer')
    expect(state.pendingRedemption.value).toBeNull()
    expect(state.busy.value).toBe(false)
  })
  it.each([
    { change: 'account ID', sessionID: 'first', userID: 2 },
    { change: 'session', sessionID: 'second', userID: 1 }
  ])('aborts redemption and clears private state when the $change changes', async ({ sessionID, userID }) => {
    await start()
    let resolve!: (value: Awaited<ReturnType<typeof welfareAPI.redeem>>) => void
    vi.mocked(welfareAPI.redeem).mockReturnValueOnce(new Promise(r => { resolve = r }))
    const redemption = state.redeem({ amount: '1.00', welfare_balance_version: 1 })
    const signal = vi.mocked(welfareAPI.redeem).mock.calls[0][2]
    vi.mocked(welfareAPI.overview).mockReturnValueOnce(new Promise(() => {}))
    vi.mocked(welfareAPI.rules).mockReturnValueOnce(new Promise(() => {}))

    localStorage.setItem('auth_session_id', sessionID)
    localStorage.setItem('auth_user', JSON.stringify({ id: userID }))
    auth.sessionRevision = sessionID; auth.user = { id: userID }
    await flushPromises()

    expect(signal?.aborted).toBe(true)
    expect(state.busy.value).toBe(false)
    expect(state.pendingRedemption.value).toBeNull()
    expect(state.overview.value).toBeNull()
    expect(state.calendar.value).toBeNull()
    expect(state.records.value).toBeNull()
    expect(state.rules.value).toBeNull()
    resolve({ operation_id: 'old-transfer', status: 'completed', overview: snapshot(2, '1.80') })
    expect(await redemption).toBeNull()
    expect(state.overview.value).toBeNull()
  })
  it.each([{}, undefined])('keeps draw and redemption retries stable when crypto lacks randomUUID (%j)', async crypto => {
    vi.stubGlobal('crypto', crypto)
    await start()
    vi.mocked(welfareAPI.draw).mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ operation_id: 'draw', status: 'completed', overview: snapshot(2) })
    await state.draw(); await state.draw()
    const drawKey = vi.mocked(welfareAPI.draw).mock.calls[0][0]
    expect(drawKey).toEqual(expect.any(String))
    expect(drawKey.length).toBeGreaterThan(0)
    expect(drawKey.length).toBeLessThanOrEqual(128)
    expect(vi.mocked(welfareAPI.draw).mock.calls[1][0]).toBe(drawKey)
    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ operation_id: 'redeem', status: 'completed', overview: snapshot(3) })
    const body = { amount: '1.00', welfare_balance_version: 2 }
    await state.redeem(body); await state.redeem(body)
    const redemptionKey = vi.mocked(welfareAPI.redeem).mock.calls[0][1]
    expect(redemptionKey).toEqual(expect.any(String))
    expect(redemptionKey.length).toBeGreaterThan(0)
    expect(redemptionKey.length).toBeLessThanOrEqual(128)
    expect(redemptionKey).not.toBe(drawKey)
    expect(vi.mocked(welfareAPI.redeem).mock.calls[1][1]).toBe(redemptionKey)
  })
  it('blocks an old screen POST immediately when another tab changes login', async () => {
    await start()
    localStorage.setItem('auth_session_id', 'second'); localStorage.setItem('auth_user', JSON.stringify({ id: 2 }))
    await state.draw()
    expect(welfareAPI.draw).not.toHaveBeenCalled()
    expect(state.overview.value).toBeNull()
    expect(state.sessionInvalidated.value).toBe(true)
  })
  it('rejects a response when storage changes before Pinia knows about the new login', async () => {
    let resolve!: (value: ReturnType<typeof snapshot>) => void
    await start()
    vi.mocked(welfareAPI.overview).mockReturnValueOnce(new Promise(r => { resolve = r }))
    const reading = state.refresh()
    localStorage.setItem('auth_session_id', 'second'); localStorage.setItem('auth_user', JSON.stringify({ id: 2 }))
    resolve(snapshot(99, '999.00')); await reading
    expect(state.overview.value).toBeNull()
    expect(auth.user.id).toBe(1)
  })
  it('restores an unresolved draw on remount and retries the original key even with zero tickets', async () => {
    await start()
    vi.mocked(welfareAPI.draw).mockRejectedValueOnce(new Error('offline'))
    await state.draw()
    const key = vi.mocked(welfareAPI.draw).mock.calls[0][0]
    wrapper.unmount(); await start()
    expect(welfareAPI.operationByKey).toHaveBeenCalledWith('draw', key, expect.any(AbortSignal))
    expect(state.pendingDraw.value).toBe(key)
    expect(welfareAPI.draw).toHaveBeenCalledTimes(1)
    vi.mocked(welfareAPI.draw).mockResolvedValueOnce({ operation_id: 'restored', status: 'completed', overview: snapshot(2) })
    await state.draw()
    expect(vi.mocked(welfareAPI.draw).mock.calls[1][0]).toBe(key)
    expect(state.pendingDraw.value).toBeNull()
  })
  it('resolves a committed transfer after reload using a GET without another POST', async () => {
    await start()
    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce(new Error('offline'))
    await state.redeem({ amount: '2.80', welfare_balance_version: 1 })
    const key = vi.mocked(welfareAPI.redeem).mock.calls[0][1]
    wrapper.unmount()
    vi.mocked(welfareAPI.operationByKey).mockResolvedValueOnce({ operation_id: 'committed', status: 'completed', amount: '2.80', overview: snapshot(2, '0.00') })
    await start()
    expect(welfareAPI.operationByKey).toHaveBeenCalledWith('redeem', key, expect.any(AbortSignal))
    expect(welfareAPI.redeem).toHaveBeenCalledTimes(1)
    expect(state.pendingRedemption.value).toBeNull()
    expect(state.recovered.value[0].kind).toBe('redeem')
  })
  it('releases the previous account recovery state when switching accounts', async () => {
    sessionStorage.setItem('welfare.pending:1:first', JSON.stringify({ drawKey: 'old-draw', redemption: null }))
    vi.mocked(welfareAPI.operationByKey).mockReturnValueOnce(new Promise(() => {}))
    await start()
    expect(state.recovering.value).toBe(true)
    const previousSignal = vi.mocked(welfareAPI.operationByKey).mock.calls[0][2]
    localStorage.setItem('auth_session_id', 'second'); localStorage.setItem('auth_user', JSON.stringify({ id: 2 }))
    auth.sessionRevision = 'second'; auth.user = { id: 2 }
    vi.mocked(welfareAPI.overview).mockResolvedValue(snapshot(2, '6.00'))
    await flushPromises()
    expect(previousSignal?.aborted).toBe(true)
    expect(state.overview.value?.welfare_balance).toBe('6.00')
    expect(state.pendingDraw.value).toBeNull()
    expect(state.recovering.value).toBe(false)
    vi.mocked(welfareAPI.draw).mockResolvedValueOnce({ operation_id: 'new-draw', status: 'completed', overview: snapshot(3) })
    await state.draw()
    expect(welfareAPI.draw).toHaveBeenCalledOnce()
    expect(vi.mocked(welfareAPI.draw).mock.calls[0][0]).not.toBe('old-draw')
  })
  it('keeps a failed rules request visible after a successful overview refresh', async () => {
    vi.mocked(welfareAPI.rules).mockRejectedValueOnce(new Error('offline'))
    await start()
    await state.refresh()
    expect(state.rulesError.value).toBe('network')
    await state.loadRules()
    expect(state.rulesError.value).toBe('')
    expect(state.rules.value?.draw_threshold).toBe('50.00')
  })
  it('blocks a changed transfer while an earlier response is uncertain', async () => {
    await start()
    vi.mocked(welfareAPI.redeem).mockRejectedValueOnce(new Error('offline'))
    await state.redeem({ amount: '1.00', welfare_balance_version: 1 })
    await state.redeem({ amount: '2.00', welfare_balance_version: 1 })
    expect(welfareAPI.redeem).toHaveBeenCalledTimes(1)
    expect(state.mutationError.value).toBe('WELFARE_OPERATION_UNRESOLVED')
  })
  it('respects server Retry-After when overview is rate limited', async () => {
    vi.useFakeTimers()
    vi.mocked(welfareAPI.overview).mockRejectedValueOnce({ status: 429, retryAfter: '35' })
    await start()
    await vi.advanceTimersByTimeAsync(30000)
    expect(welfareAPI.overview).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(10000)
    expect(welfareAPI.overview).toHaveBeenCalledTimes(2)
  })
  it('refreshes calendar after another device checks in, but not every poll', async () => {
    await start()
    const count = vi.mocked(welfareAPI.calendar).mock.calls.length
    await state.refresh()
    expect(welfareAPI.calendar).toHaveBeenCalledTimes(count)
    vi.mocked(welfareAPI.overview).mockResolvedValue({ ...snapshot(2), today_checked_in: true, total_checkin_days: 24 })
    await state.refresh(); await flushPromises()
    expect(welfareAPI.calendar).toHaveBeenCalledTimes(count + 1)
  })
  it('clears loading if dates become invalid while records are in flight', async () => {
    await start()
    vi.mocked(welfareAPI.records).mockReturnValueOnce(new Promise(() => {}))
    void state.loadRecords()
    expect(state.recordsLoading.value).toBe(true)
    state.filters.date_from = '2026-09-20'; state.filters.date_to = '2026-09-01'
    await flushPromises()
    expect(state.recordsLoading.value).toBe(false)
  })
  it('keeps committed check-in success when the follow-up read fails', async () => {
    await start()
    vi.mocked(welfareAPI.checkIn).mockResolvedValue({ operation_id: 'one', status: 'completed', reward_amount: '0.08', overview: { ...snapshot(2, '2.88'), today_checked_in: true } })
    vi.mocked(welfareAPI.overview).mockRejectedValue(new Error('offline'))
    const result = await state.checkIn()
    expect(result?.reward_amount).toBe('0.08')
    expect(state.overview.value?.welfare_balance).toBe('2.88')
    expect(state.mutationError.value).toBe('')
  })
  it('reuses the same draw key after an uncertain network response', async () => {
    await start()
    vi.mocked(welfareAPI.draw).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ operation_id: 'two', status: 'completed', overview: snapshot(2) })
    await state.draw(); await state.draw()
    expect(vi.mocked(welfareAPI.draw).mock.calls[0][0]).toBe(vi.mocked(welfareAPI.draw).mock.calls[1][0])
  })
  it('does not replace newer balances with an idempotent replay snapshot', async () => {
    await start()
    vi.mocked(welfareAPI.overview).mockResolvedValue(snapshot(9, '9.00'))
    await state.refresh()
    vi.mocked(welfareAPI.draw).mockResolvedValue({ operation_id: 'old', status: 'completed', overview: snapshot(2, '3.00') })
    await state.draw()
    expect(state.overview.value?.welfare_balance).toBe('9.00')
  })
  it('does not restore yesterday or a previous activity state from an equal-version replay', async () => {
    await start()
    vi.mocked(welfareAPI.overview).mockResolvedValue({ ...snapshot(2), business_date: '2026-09-20', rewards_enabled: false })
    await state.refresh()
    vi.mocked(welfareAPI.draw).mockResolvedValue({ operation_id: 'old', status: 'completed', overview: snapshot(2) })
    vi.mocked(welfareAPI.overview).mockRejectedValueOnce(new Error('offline'))
    await state.draw()
    expect(state.overview.value?.business_date).toBe('2026-09-20')
    expect(state.overview.value?.rewards_enabled).toBe(false)
  })
  it('discards an outstanding response after switching accounts', async () => {
    let resolve!: (value: ReturnType<typeof snapshot>) => void
    vi.mocked(welfareAPI.overview).mockReturnValueOnce(new Promise(r => { resolve = r }))
    await start()
    auth.sessionRevision = 'second'; auth.user = { id: 2 }
    await flushPromises()
    resolve(snapshot(9, '999.00')); await flushPromises()
    expect(state.overview.value?.welfare_balance).not.toBe('999.00')
  })
  it('polls only while visible and prevents overlapping overview requests', async () => {
    vi.useFakeTimers()
    await start()
    let resolve!: (value: ReturnType<typeof snapshot>) => void
    vi.mocked(welfareAPI.overview).mockReturnValue(new Promise(r => { resolve = r }))
    await vi.advanceTimersByTimeAsync(30000)
    expect(welfareAPI.overview).toHaveBeenCalledTimes(2)
    resolve(snapshot()); await flushPromises()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    await vi.advanceTimersByTimeAsync(20000)
    expect(welfareAPI.overview).toHaveBeenCalledTimes(2)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  })
})
