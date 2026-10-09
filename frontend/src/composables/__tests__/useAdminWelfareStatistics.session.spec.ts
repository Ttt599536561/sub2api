import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import axios, { type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { adminWelfareAPI } from '@/api/admin/welfare'
import { useAuthStore } from '@/stores/auth'
import { useAdminWelfareStatistics } from '../useAdminWelfareStatistics'
import WelfareStatisticsPanel from '@/components/admin/welfare/WelfareStatisticsPanel.vue'
import type { User } from '@/types'
import type { WelfareStatistics, WelfareStatisticsPage, WelfareStatisticsRecord, WelfareStatisticsUser } from '@/types/adminWelfare'

const { authAPI } = vi.hoisted(() => ({ authAPI: { login: vi.fn(), logout: vi.fn() } }))
vi.mock('@/api', () => ({ authAPI, passkeyAPI: {}, isTotp2FARequired: () => false }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } })
}))

const administrator: User = { id: 1, role: 'admin', username: 'administrator', email: 'admin@example.com', balance: 0, concurrency: 1, status: 'active', allowed_groups: null, balance_notify_enabled: false, balance_notify_threshold: null, balance_notify_extra_emails: [], created_at: '', updated_at: '' }
const totals = { daily_amount: '12.34', streak_amount: '2.00', checkin_amount: '14.34', draw_amount: '5.00', total_amount: '19.34', checkin_users: 2, checkin_count: 3, streak_users: 1, draw_users: 1, draw_count: 2, participating_users: 2 }
const overview: WelfareStatistics = { date_from: '2026-10-03', date_to: '2026-10-09', timezone: 'Asia/Shanghai', summary: totals, daily: [{ ...totals, date: '2026-10-09' }] }
const users: WelfareStatisticsPage<WelfareStatisticsUser> = { items: [{ user_id: 7, email: 'private@example.com', period: totals, lifetime: totals }], total: 1, page: 1, page_size: 20 }
const records: WelfareStatisticsPage<WelfareStatisticsRecord> = { items: [{ id: 'draw:8', user_id: 7, email: 'private@example.com', type: 'draw', created_at: '2026-10-08T16:30:00Z', business_date: '2026-10-09', amount: '5.00' }], total: 1, page: 1, page_size: 20 }
const originalAdapter = apiClient.defaults.adapter
let wrapper: VueWrapper | undefined
let stats: ReturnType<typeof useAdminWelfareStatistics>
let requests: InternalAxiosRequestConfig[]

function storedLogin(session = 'administrator-session', user: User = administrator, token = 'administrator-token') {
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_user', JSON.stringify(user))
  localStorage.setItem('auth_token', token)
}
function response(config: InternalAxiosRequestConfig, data: unknown): AxiosResponse {
  return { config, data: { code: 0, data }, status: 200, statusText: 'OK', headers: {} }
}
function installAdapter(deferredPath?: string) {
  const pending: Array<{ config: InternalAxiosRequestConfig; resolve: (value: AxiosResponse) => void; reject: (error: unknown) => void }> = []
  apiClient.defaults.adapter = config => {
    requests.push(config)
    if (config.url === deferredPath) return new Promise((resolve, reject) => { pending.push({ config, resolve, reject }) })
    return Promise.resolve(response(config, config.url?.endsWith('/users') ? users : config.url?.endsWith('/records') ? records : overview))
  }
  return pending
}
function start() {
  wrapper = mount(defineComponent({ setup() { stats = useAdminWelfareStatistics(); return () => null } }))
}
async function loginWithVolatileSession() {
  const originalSetItem = Storage.prototype.setItem
  const writes = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, key, value) {
    if (key === 'auth_session_id') throw new DOMException('Session marker quota', 'QuotaExceededError')
    originalSetItem.call(this, key, value)
  })
  try { await useAuthStore().login({ email: administrator.email, password: 'password' }) }
  finally { writes.mockRestore() }
  expect(localStorage.getItem('auth_session_id')).toBe('administrator-session')
  expect(useAuthStore().sessionRevision).not.toBe('administrator-session')
}
function expectCleared() {
  for (const resource of [stats.overview, stats.users, stats.records]) {
    expect(resource.data.value).toBeNull()
    expect(resource.loading.value).toBe(false)
    expect(resource.error.value).toBe(false)
  }
  expect(stats.draft.search).toBe('')
  expect(stats.draft.user_id).toBe('')
  expect(stats.applied.value).not.toHaveProperty('search')
  expect(stats.applied.value).not.toHaveProperty('user_id')
  expect(stats.detailUser.value).toBeNull()
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  storedLogin()
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.user = { ...administrator }
  auth.token = 'administrator-token'
  authAPI.logout.mockResolvedValue(undefined)
  authAPI.login.mockResolvedValue({ access_token: 'second-admin-token', user: administrator })
  requests = []
  installAdapter()
})
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  await useAuthStore().logout()
  apiClient.defaults.adapter = originalAdapter
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('administrator statistics session ownership with the real Axios client', () => {
  it.each(['auth_session_id', 'auth_user', 'auth_token', null])('clears loaded private statistics and scopes synchronously on storage event %s', async key => {
    start(); await flushPromises()
    stats.draft.search = 'private@example.com'; stats.draft.user_id = '7'; stats.applyFilters()
    stats.drillUser(users.items[0]); await flushPromises()
    expect(stats.users.data.value?.items[0].email).toBe('private@example.com')
    expect(stats.records.data.value?.items[0].email).toBe('private@example.com')
    storedLogin('ordinary-session', { ...administrator, id: 99, role: 'user' }, 'ordinary-token')
    if (key === null) localStorage.clear()
    window.dispatchEvent(new StorageEvent('storage', { key }))
    expectCleared()
  })

  it.each(['success', 'failure'] as const)('rejects a late real Axios %s without a storage event and clears other loaded resources', async outcome => {
    const pending = installAdapter('/admin/welfare/statistics/users')
    start(); await flushPromises()
    expect(pending[0].config.headers.get('Authorization')).toBe('Bearer administrator-token')
    expect(stats.overview.data.value?.summary.total_amount).toBe('19.34')
    storedLogin('ordinary-session', { ...administrator, id: 99, role: 'user' }, 'ordinary-token')
    if (outcome === 'success') pending[0].resolve(response(pending[0].config, users))
    else pending[0].reject(new axios.AxiosError('old failure', 'ERR_BAD_RESPONSE', pending[0].config, undefined, { ...response(pending[0].config, {}), status: 500 }))
    await flushPromises()
    expectCleared()
    expect(pending[0].config.signal?.aborted).toBe(true)
  })

  it('captures request ownership before Axios dispatch can use a replacement account', async () => {
    const queued = adminWelfareAPI.getStatisticsUsers({ date_from: '2026-10-03', date_to: '2026-10-09', page: 1, page_size: 20, sort_by: 'total_amount', sort_order: 'desc' })
    const result = expect(queued).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    storedLogin('ordinary-session', { ...administrator, id: 99, role: 'user' }, 'ordinary-token')
    await result
    expect(requests).toHaveLength(0)
  })

  it('detects another tab same-user new login after session marker storage used its volatile fallback', async () => {
    await loginWithVolatileSession()
    start(); await flushPromises()
    expect(stats.users.data.value?.items[0].email).toBe('private@example.com')
    storedLogin('other-tab-administrator-session')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    expectCleared()
    const count = requests.length
    await stats.users.load()
    expect(requests).toHaveLength(count)
  })

  it.each(['success', 'failure'] as const)('rejects late real Axios %s when a volatile session masks a same-user replacement login', async outcome => {
    await loginWithVolatileSession()
    const pending = installAdapter('/admin/welfare/statistics/users')
    start(); await flushPromises()
    storedLogin('other-tab-administrator-session')
    if (outcome === 'success') pending[0].resolve(response(pending[0].config, users))
    else pending[0].reject(new axios.AxiosError('old failure', 'ERR_BAD_RESPONSE', pending[0].config, undefined, { ...response(pending[0].config, {}), status: 500 }))
    await flushPromises()
    expectCleared()
  })

  it('blocks all queued statistics endpoints when a volatile session masks a same-user replacement login', async () => {
    await loginWithVolatileSession()
    const range = { date_from: '2026-10-03', date_to: '2026-10-09' }
    const queued = [adminWelfareAPI.getStatistics(range), adminWelfareAPI.getStatisticsUsers({ ...range, page: 1, page_size: 20, sort_by: 'total_amount', sort_order: 'desc' }), adminWelfareAPI.getStatisticsRecords({ ...range, page: 1, page_size: 20, type: 'all' })]
    storedLogin('other-tab-administrator-session')
    const outcomes = await Promise.allSettled(queued)
    expect(outcomes.every(outcome => outcome.status === 'rejected' && outcome.reason.code === 'ERR_CANCELED')).toBe(true)
    expect(requests).toHaveLength(0)
  })

  it('keeps volatile-session statistics and requests during a normal access token rotation', async () => {
    await loginWithVolatileSession()
    start(); await flushPromises()
    localStorage.setItem('auth_token', 'refreshed-administrator-token')
    useAuthStore().token = 'refreshed-administrator-token'
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' }))
    expect(stats.users.data.value).toEqual(users)
    await stats.users.load()
    expect(requests.at(-1)?.headers.get('Authorization')).toBe('Bearer refreshed-administrator-token')
    expect(stats.users.data.value).toEqual(users)
  })

  it('loads a legacy null session marker and invalidates when another tab creates a new marker', async () => {
    localStorage.removeItem('auth_session_id')
    setActivePinia(createPinia())
    useAuthStore().user = { ...administrator }
    useAuthStore().token = 'administrator-token'
    start(); await flushPromises()
    expect(stats.users.data.value).toEqual(users)
    storedLogin('other-tab-administrator-session')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    expectCleared()
  })

  it('preserves the configured default Axios request transformer chain', async () => {
    const defaults = apiClient.defaults.transformRequest
    apiClient.defaults.transformRequest = [function (data, headers) { headers.set('X-Statistics-Default-Transform', 'preserved'); return data }, ...(Array.isArray(defaults) ? defaults : defaults ? [defaults] : [])]
    try {
      const result = await adminWelfareAPI.getStatistics({ date_from: '2026-10-03', date_to: '2026-10-09' })
      expect(result).toEqual(overview)
      expect(requests[0].headers.get('X-Statistics-Default-Transform')).toBe('preserved')
    } finally { apiClient.defaults.transformRequest = defaults }
  })

  it.each(['account', 'role', 'token removed'] as const)('clears synchronously on a same-tab store %s change', async change => {
    start(); await flushPromises()
    const auth = useAuthStore()
    if (change === 'account') auth.user = { ...administrator, id: 2 }
    else if (change === 'role') auth.user!.role = 'user'
    else auth.token = null
    expectCleared()
  })

  it('invalidates on logout and never accepts the old in-flight response', async () => {
    const pending = installAdapter('/admin/welfare/statistics/records')
    start(); await flushPromises()
    await useAuthStore().logout()
    expectCleared()
    expect(pending[0].config.signal?.aborted).toBe(true)
    pending[0].resolve(response(pending[0].config, records)); await flushPromises()
    expectCleared()
  })

  it('invalidates when the same administrator starts a new login session', async () => {
    start(); await flushPromises()
    await useAuthStore().login({ email: administrator.email, password: 'password' })
    expect(useAuthStore().user?.id).toBe(administrator.id)
    expect(localStorage.getItem('auth_session_id')).not.toBe('administrator-session')
    expectCleared()
  })

  it.each(['focus', 'visibilitychange', 'auth_user'])('rechecks stored administrator permissions on %s', async event => {
    start(); await flushPromises()
    localStorage.setItem('auth_user', JSON.stringify({ ...administrator, role: 'user' }))
    if (event === 'visibilitychange') document.dispatchEvent(new Event(event))
    else if (event === 'auth_user') window.dispatchEvent(new StorageEvent('storage', { key: event }))
    else window.dispatchEvent(new Event(event))
    expectCleared()
  })

  it('keeps loaded data and in-flight requests during access token rotation', async () => {
    const pending = installAdapter('/admin/welfare/statistics/records')
    start(); await flushPromises()
    localStorage.setItem('auth_token', 'refreshed-administrator-token')
    useAuthStore().token = 'refreshed-administrator-token'
    useAuthStore().user = { ...administrator }
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' }))
    window.dispatchEvent(new Event('focus'))
    expect(stats.users.data.value).toEqual(users)
    expect(stats.overview.data.value).toEqual(overview)
    expect(pending[0].config.signal?.aborted).toBe(false)
    pending[0].resolve(response(pending[0].config, records)); await flushPromises()
    expect(stats.records.data.value).toEqual(records)
    await stats.users.load()
    expect(requests.at(-1)?.headers.get('Authorization')).toBe('Bearer refreshed-administrator-token')
  })

  it('blocks every interaction and retry after session invalidation', async () => {
    start(); await flushPromises()
    storedLogin('second-administrator-session')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    const count = requests.length
    stats.draft.search = 'new@example.com'; stats.draft.user_id = '9'
    stats.applyFilters(); stats.drillUser(users.items[0]); stats.drillDate('2026-10-09')
    stats.setRecordDates('2026-10-09', '2026-10-09'); stats.setRecordType('draw')
    stats.setUserPage(2); stats.setRecordPage(2); stats.setUserPageSize(100); stats.setRecordPageSize(100)
    stats.sortUsers('checkin_count', 'asc'); stats.clearDetailScope()
    await Promise.all([stats.overview.load(), stats.users.load(), stats.records.load()]); await flushPromises()
    expect(requests).toHaveLength(count)
    expect(stats.applied.value).not.toHaveProperty('search')
    expect(stats.detailUser.value).toBeNull()
  })

  it('checks ownership before an interaction when the storage event was missed', async () => {
    start(); await flushPromises()
    storedLogin('second-administrator-session')
    const count = requests.length
    stats.draft.search = 'new@example.com'; stats.applyFilters(); await flushPromises()
    expect(requests).toHaveLength(count)
    expectCleared()
  })

  it('aborts all resources and removes listeners and store watchers on unmount', async () => {
    apiClient.defaults.adapter = config => { requests.push(config); return new Promise(() => {}) }
    const addWindow = vi.spyOn(window, 'addEventListener')
    const removeWindow = vi.spyOn(window, 'removeEventListener')
    const addDocument = vi.spyOn(document, 'addEventListener')
    const removeDocument = vi.spyOn(document, 'removeEventListener')
    start(); await flushPromises()
    expect(requests).toHaveLength(3)
    wrapper!.unmount(); wrapper = undefined
    expect(requests.every(config => config.signal?.aborted)).toBe(true)
    for (const event of ['storage', 'focus']) expect(removeWindow).toHaveBeenCalledWith(event, addWindow.mock.calls.find(call => call[0] === event)?.[1])
    expect(removeDocument).toHaveBeenCalledWith('visibilitychange', addDocument.mock.calls.find(call => call[0] === 'visibilitychange')?.[1])
    const query = { ...stats.applied.value }
    storedLogin('second-administrator-session'); useAuthStore().user = { ...administrator, id: 2 }
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    expect(stats.applied.value).toEqual(query)
    await stats.overview.load(); expect(requests).toHaveLength(3)
  })

  it('replaces visible statistics and private scopes with a session change notice', async () => {
    wrapper = mount(WelfareStatisticsPanel); await flushPromises()
    await wrapper.get('[data-test="stats-search"]').setValue('private@example.com')
    await wrapper.get('[data-test="stats-user-id"]').setValue('7')
    await wrapper.get('[data-test="stats-filters"]').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain('private@example.com')
    storedLogin('ordinary-session', { ...administrator, id: 99, role: 'user' }, 'ordinary-token')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' })); await flushPromises()
    expect(wrapper.text()).not.toContain('private@example.com')
    expect(wrapper.text()).not.toContain('$19.34')
    expect(wrapper.get('[data-test="statistics-session-notice"]').text()).toContain('welfare.admin.stats.sessionChanged')
    expect(wrapper.find('[data-test="stats-filters"]').exists()).toBe(false)
  })
})
