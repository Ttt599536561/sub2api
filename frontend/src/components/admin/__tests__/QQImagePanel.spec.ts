import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import QQImagePanel from '../QQImagePanel.vue'
import { adminQQImageAPI } from '@/api/qqImage'
import zhQQImage from '@/i18n/locales/zh/qqImage'
import enQQImage from '@/i18n/locales/en/qqImage'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => true }) }))
vi.mock('@/api/qqImage', () => ({ adminQQImageAPI: { connection: vi.fn(), config: vi.fn(), saveConfig: vi.fn(), orders: vi.fn(), grants: vi.fn(), audit: vi.fn(), refund: vi.fn(), reconcile: vi.fn(), release: vi.fn(), revoke: vi.fn(), image: vi.fn(), discovery: vi.fn(), startDiscovery: vi.fn(), closeDiscovery: vi.fn(), confirmDiscovery: vi.fn() } }))
const auth = reactive({ user: { id: 1, role: 'admin', email: 'admin@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const config = { enabled: false, app_id: 'app1', api_url: 'https://images.example.test/v1', model: 'image-1', size: '1024x1024', price: '0.12000000', allowed_groups: ['group1'], public_base_url: 'https://site.example.test', user_daily_limit: 20, user_daily_amount: '10.00000000', group_concurrency: 2, global_concurrency: 5, queue_limit: 50, queue_timeout_seconds: 300, deadline_seconds: 7200, retention_days: 7, prompt_max_chars: 2000, bot_secret_configured: true, api_key_configured: true, encryption_available: true }
let wrapper: ReturnType<typeof mount>
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear(); sessionStorage.clear()
  auth.user = { id: 1, role: 'admin', email: 'admin@example.test' }; auth.sessionRevision = 'A'; auth.isAuthenticated = true
  localStorage.setItem('auth_user', JSON.stringify(auth.user)); localStorage.setItem('auth_session_id', 'A')
  vi.mocked(adminQQImageAPI.connection).mockResolvedValue({ state: 'webhook' })
  vi.mocked(adminQQImageAPI.discovery).mockResolvedValue(null)
  vi.mocked(adminQQImageAPI.startDiscovery).mockResolvedValue({ id: 'discovery1', app_id: 'app1', status: 'waiting', expires_at: '2099-01-01T00:00:00Z', command: '@机器人 /识别群 SECRET-ONCE' })
  vi.mocked(adminQQImageAPI.closeDiscovery).mockResolvedValue({ id: 'discovery1', app_id: 'app1', status: 'closed', expires_at: '2099-01-01T00:00:00Z' })
  vi.mocked(adminQQImageAPI.confirmDiscovery).mockResolvedValue({ id: 'discovery1', app_id: 'app1', status: 'confirmed', expires_at: '2099-01-01T00:00:00Z' })
  vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config })
  vi.mocked(adminQQImageAPI.saveConfig).mockResolvedValue({ ...config })
  vi.mocked(adminQQImageAPI.orders).mockResolvedValue([{ id: 'o1', prompt: 'private', price: '0.12000000', model: 'image-1', size: '1024x1024', status: 'completed', funds_status: 'captured', delivery_status: 'failed', created_at: '2026-10-23T00:00:00Z', deadline: '2026-10-23T02:00:00Z' }])
  vi.mocked(adminQQImageAPI.grants).mockResolvedValue([]); vi.mocked(adminQQImageAPI.audit).mockResolvedValue([])
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })
async function start() { wrapper = mount(QQImagePanel, { global: { stubs: { QQReplyTemplatesPanel: true, RouterLink: { template: '<a><slot /></a>' } } } }); await flushPromises() }
describe('QQ transport configuration and connection status', () => {
  it('defaults absent receive mode to HTTP callbacks and saves it explicitly', async () => {
    await start()
    expect((wrapper.get('[name="receive_mode"]').element as HTMLSelectElement).value).toBe('webhook')
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0].receive_mode).toBe('webhook')
  })
  it('saves WebSocket with existing settings and credentials preserved while generation stays off', async () => {
    vi.mocked(adminQQImageAPI.saveConfig).mockResolvedValue({ ...config, receive_mode: 'websocket' })
    await start(); await wrapper.get('[name="receive_mode"]').setValue('websocket')
    expect(wrapper.text()).toContain('qqImage.admin.connection.saveFirst')
    expect(wrapper.get('[data-test="connection-refresh"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0]).toMatchObject({
      receive_mode: 'websocket', enabled: false, app_id: config.app_id, api_url: config.api_url,
      model: config.model, price: config.price, allowed_groups: config.allowed_groups, user_daily_amount: config.user_daily_amount
    })
    expect(vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0].bot_secret).toBeUndefined()
    expect(vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0].api_key).toBeUndefined()
    expect((wrapper.get('[name="receive_mode"]').element as HTMLSelectElement).value).toBe('websocket')
    expect((wrapper.get('[name="enabled"]').element as HTMLInputElement).checked).toBe(false)
  })
  it('shows saved WebSocket connection and safe timestamps while generation is disabled', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, receive_mode: 'websocket' })
    vi.mocked(adminQQImageAPI.connection).mockResolvedValue({ state: 'connected', last_connected_at: '2026-10-23T01:00:00Z', last_heartbeat_at: '2026-10-23T01:00:10Z', last_event_at: '2026-10-23T01:00:05Z', session_id: 'NEVER-EXPOSE-SESSION' } as Awaited<ReturnType<typeof adminQQImageAPI.connection>>)
    await start()
    expect(wrapper.get('[data-test="connection-state"]').text()).toBe('qqImage.admin.connection.states.connected')
    expect(wrapper.findAll('[data-test="qq-connection"] time').map(time => time.attributes('datetime'))).toEqual(['2026-10-23T01:00:00Z', '2026-10-23T01:00:10Z', '2026-10-23T01:00:05Z'])
    expect(wrapper.text()).not.toContain('NEVER-EXPOSE-SESSION')
    expect(wrapper.get('[data-test="discovery-start"]').attributes('disabled')).toBeUndefined()
    for (const storage of [localStorage, sessionStorage]) expect(Array.from({ length: storage.length }, (_, i) => storage.getItem(storage.key(i)!)).join()).not.toContain('connected')
  })
  it('replaces a previous success with loading and failure, without blocking configuration', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, receive_mode: 'websocket' })
    vi.mocked(adminQQImageAPI.connection).mockResolvedValue({ state: 'connected' })
    await start()
    let fail!: (value: unknown) => void
    vi.mocked(adminQQImageAPI.connection).mockReturnValue(new Promise((_, reject) => { fail = reject }))
    await wrapper.get('[data-test="connection-refresh"]').trigger('click')
    expect(wrapper.text()).not.toContain('qqImage.admin.connection.states.connected')
    expect(wrapper.get('[data-test="connection-state"]').text()).toBe('qqImage.admin.connection.loading')
    fail({ status: 503, message: 'PRIVATE-UPSTREAM-ERROR' }); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.admin.connection.failed')
    expect(wrapper.text()).not.toContain('PRIVATE-UPSTREAM-ERROR')
    expect(wrapper.find('[data-test="qq-config"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('qqImage.admin.connection.states.connected')
  })
  it('clears a displayed connection while the main configuration refresh is pending', async () => {
    vi.mocked(adminQQImageAPI.connection).mockResolvedValue({ state: 'connected' })
    await start()
    let finish!: (value: typeof config) => void
    vi.mocked(adminQQImageAPI.config).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await wrapper.get('button').trigger('click')
    expect(wrapper.text()).not.toContain('qqImage.admin.connection.states.connected')
    expect(wrapper.get('[data-test="connection-refresh"]').attributes('disabled')).toBeDefined()
    finish(config); await flushPromises()
    expect(adminQQImageAPI.connection).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="connection-state"]').text()).toBe('qqImage.admin.connection.states.connected')
  })
  it('discards a pending connection response after mode changes until configuration is saved', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, receive_mode: 'websocket' })
    let finish!: (value: { state: string }) => void
    vi.mocked(adminQQImageAPI.connection).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await start(); await wrapper.get('[name="receive_mode"]').setValue('webhook')
    const signal = vi.mocked(adminQQImageAPI.connection).mock.calls[0][0]
    expect(signal?.aborted).toBe(true)
    finish({ state: 'connected' }); await flushPromises()
    expect(wrapper.text()).not.toContain('qqImage.admin.connection.states.connected')
    expect(wrapper.text()).toContain('qqImage.admin.connection.saveFirst')
  })
  it.each(['logout', 'account', 'role', 'same-user-login'])('aborts and ignores a late connection response after %s', async change => {
    let finish!: (value: { state: string }) => void
    vi.mocked(adminQQImageAPI.connection).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await start()
    const signal = vi.mocked(adminQQImageAPI.connection).mock.calls[0][0]
    if (change === 'logout') auth.isAuthenticated = false
    if (change === 'account') auth.user.id = 2
    if (change === 'role') auth.user.role = 'user'
    if (change === 'same-user-login') auth.sessionRevision = 'B'
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    finish({ state: 'connected' }); await flushPromises()
    expect(wrapper.find('[data-test="qq-connection"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('qqImage.admin.connection.states.connected')
  })
  it('aborts a pending status read when unmounted', async () => {
    vi.mocked(adminQQImageAPI.connection).mockReturnValue(new Promise(() => {}))
    await start()
    const signal = vi.mocked(adminQQImageAPI.connection).mock.calls[0][0]
    wrapper.unmount(); expect(signal?.aborted).toBe(true)
  })
  it('uses generic translations for unknown states and errors and ignores invalid timestamps', async () => {
    vi.mocked(adminQQImageAPI.connection).mockResolvedValue({ state: 'PRIVATE-STATE', error_code: 'PRIVATE-ERROR', last_event_at: 'PRIVATE-TIME' })
    await start()
    expect(wrapper.get('[data-test="connection-state"]').text()).toBe('qqImage.admin.connection.states.unknown')
    expect(wrapper.text()).toContain('qqImage.admin.connection.errors.unknown')
    expect(wrapper.text()).not.toContain('PRIVATE-')
    expect(wrapper.find('[data-test="qq-connection"] time').exists()).toBe(false)
  })
  it('explains outbound WebSocket and HTTP callbacks in both languages', () => {
    expect(zhQQImage.qqImage.admin.receiveModeHint).toContain('主动')
    expect(zhQQImage.qqImage.admin.receiveModeHint).toContain('HTTP')
    expect(enQQImage.qqImage.admin.receiveModeHint).toContain('outbound')
    expect(enQQImage.qqImage.admin.receiveModeHint).toContain('HTTP')
  })
})
describe('QQ admin configuration and recovery', () => {
  it('distinguishes USD customer prices from unknown upstream costs in both languages', async () => {
    await start(); await wrapper.get('[data-test="qq-tab-orders"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="provider-cost-hint"]').text()).toBe('qqImage.admin.providerCostHint')
    expect(zhQQImage.qqImage.admin.providerCostHint).toContain('USD')
    expect(zhQQImage.qqImage.admin.providerCostHint).toContain('不能按 0')
    expect(enQQImage.qqImage.admin.providerCostHint).toContain('USD')
    expect(enQQImage.qqImage.admin.providerCostHint).toContain('not be counted as zero')
  })
  it('shows configured flags with blank password inputs and preserves exact decimal strings', async () => {
    await start()
    expect((wrapper.get('[name="bot_secret"]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('')
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    const body = vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0]
    expect(body.price).toBe('0.12000000'); expect(body.user_daily_amount).toBe('10.00000000')
    expect(body.bot_secret).toBeUndefined(); expect(body.api_key).toBeUndefined()
    expect(body).not.toHaveProperty('encryption_available')
  })
  it('does not enable or replace secrets without encryption availability', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, encryption_available: false })
    await start()
    expect(wrapper.get('[name="enabled"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[name="api_key"]').attributes('disabled')).toBeDefined()
  })
  it('rejects a full Images endpoint where an API base URL is required', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, enabled: true, api_url: 'https://images.example.test/v1/images/generations' })
    await start(); await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('qqImage.admin.configValidation.generation_endpoint')
  })
  it('requires an explicit clear flag and disables new orders when removing a credential', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, enabled: true, api_url: 'https://images.example.test/v1' })
    await start(); await wrapper.get('[name="clear_api_key"]').setValue(true)
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    const body = vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0]
    expect(body.enabled).toBe(false); expect(body).toHaveProperty('clear_api_key', true)
    expect(body.api_key).toBeUndefined()
  })
  it('saves only explicitly entered exact image CDN hostnames', async () => {
    await start(); await wrapper.get('[name="image_hosts"]').setValue('cdn.example.test\nimages.example.test')
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0]).toHaveProperty('image_hosts', ['cdn.example.test', 'images.example.test'])
    vi.mocked(adminQQImageAPI.saveConfig).mockClear()
    await wrapper.get('[name="image_hosts"]').setValue('*.example.test')
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
  })
  it('refreshes authoritative enabled state after abandoning two credential removals', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, enabled: true, api_url: 'https://images.example.test/v1' })
    await start(); await wrapper.get('[name="clear_api_key"]').setValue(true); await wrapper.get('[name="clear_bot_secret"]').setValue(true)
    await wrapper.get('button').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="confirm-config-reload"]').trigger('click'); await flushPromises()
    expect((wrapper.get('[name="enabled"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[name="clear_api_key"]').element as HTMLInputElement).checked).toBe(false)
  })
  it('requires a reason and confirmation before releasing an uncertain held order', async () => {
    vi.mocked(adminQQImageAPI.orders).mockResolvedValue([{ id: 'uncertain', prompt: 'fixture', price: '0.12', model: 'image-1', size: '1024x1024', status: 'reconciling', funds_status: 'held', delivery_status: 'pending', created_at: '2026-10-23T00:00:00Z', deadline: '2026-10-23T02:00:00Z' }])
    await start(); await wrapper.get('[data-test="qq-tab-orders"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="release-uncertain"]').trigger('click')
    expect(adminQQImageAPI.release).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="confirm-action"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="action-reason"]').setValue('Provider confirmed no deliverable')
    await wrapper.get('[data-test="confirm-action"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.release).toHaveBeenCalledWith('uncertain', 'Provider confirmed no deliverable', expect.any(AbortSignal))
    expect(adminQQImageAPI.orders).toHaveBeenCalledTimes(2)
  })
  it.each(['queued', 'generating', 'completed'])('does not offer manual release for %s orders', async status => {
    vi.mocked(adminQQImageAPI.orders).mockResolvedValue([{ id: 'other', prompt: 'fixture', price: '0.12', model: 'image-1', size: '1024x1024', status, funds_status: 'held', delivery_status: 'pending', created_at: '2026-10-23T00:00:00Z', deadline: '2026-10-23T02:00:00Z' }])
    await start(); await wrapper.get('[data-test="qq-tab-orders"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="release-other"]').exists()).toBe(false)
  })
  it('keeps old orders visible when disabled and requires a reason before a full refund', async () => {
    await start(); await wrapper.get('[data-test="qq-tab-orders"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('o1')
    await wrapper.get('[data-test="refund-o1"]').trigger('click')
    expect(adminQQImageAPI.refund).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="confirm-action"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="action-reason"]').setValue('Approved full refund')
    await wrapper.get('[data-test="confirm-action"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.refund).toHaveBeenCalledWith('o1', 'Approved full refund', expect.any(AbortSignal))
  })
  it('clears unsaved secrets and blocks delayed save after admin role is lost', async () => {
    await start(); await wrapper.get('[name="api_key"]').setValue('never-persist-me')
    auth.user.role = 'user'; await flushPromises()
    expect(wrapper.find('[name="api_key"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('qqImage.sessionChanged')
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
  })
  it('does not expose a blank save form after a configuration read failure', async () => {
    vi.mocked(adminQQImageAPI.config).mockRejectedValue({ status: 500 })
    await start(); expect(wrapper.find('[data-test="qq-config"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('qqImage.errors.failed')
  })
})

describe('QQ configuration field errors', () => {
  it('keeps a newly entered key and identifies retention above 30 days before dispatch', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, enabled: true, api_key_configured: false })
    await start(); await wrapper.get('[name="api_key"]').setValue('PRIVATE-NEW-KEY')
    await wrapper.get('[name="retention_days"]').setValue(31)
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="config-error"]').text()).toContain('qqImage.admin.fields.retention_days')
    expect(wrapper.get('[data-test="config-error"]').text()).toContain('qqImage.admin.configValidation.out_of_range')
    expect(wrapper.get('[name="retention_days"]').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('[name="retention_days"]').attributes('max')).toBe('30')
    expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('PRIVATE-NEW-KEY')
    expect(wrapper.text()).not.toContain('PRIVATE-NEW-KEY')
  })
  it('rejects an invalid nonempty endpoint even while generation is disabled', async () => {
    await start(); await wrapper.get('[name="api_url"]').setValue('https://images.example.test/v1/images/generations')
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="config-error"]').text()).toContain('qqImage.admin.fields.api_url')
    expect(wrapper.text()).toContain('qqImage.admin.configValidation.generation_endpoint')
  })
  it('shows controlled server field details and preserves the unsaved secret and all inputs', async () => {
    vi.mocked(adminQQImageAPI.saveConfig).mockRejectedValue({ status: 400, reason: 'QQ_IMAGE_CONFIG_INVALID', message: 'PRIVATE-SERVER-KEY', metadata: { field: 'api_key', issue: 'missing_credential', value: 'PRIVATE-SERVER-KEY' } })
    await start(); await wrapper.get('[name="api_key"]').setValue('PRIVATE-NEW-KEY')
    await wrapper.get('[name="model"]').setValue('my-draft-model')
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(wrapper.get('[data-test="config-error"]').text()).toContain('qqImage.admin.fields.api_key')
    expect(wrapper.text()).toContain('qqImage.admin.configValidation.missing_credential')
    expect(wrapper.text()).not.toContain('PRIVATE-')
    expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('PRIVATE-NEW-KEY')
    expect((wrapper.get('[name="model"]').element as HTMLInputElement).value).toBe('my-draft-model')
    expect(adminQQImageAPI.config).toHaveBeenCalledTimes(1)
    expect(adminQQImageAPI.saveConfig).toHaveBeenCalledTimes(1)
  })
  it.each([
    { field: 'PRIVATE-SECRET', issue: 'invalid_url' },
    { field: 'api_key', issue: 'PRIVATE-SECRET' },
    { field: 'api_key', issue: 'generation_endpoint' }
  ])('ignores untrusted or mismatched error metadata %#', async metadata => {
    vi.mocked(adminQQImageAPI.saveConfig).mockRejectedValue({ status: 400, reason: 'QQ_IMAGE_CONFIG_INVALID', metadata, message: 'PRIVATE-SECRET' })
    await start(); await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.admin.configValidation.invalid_request')
    expect(wrapper.text()).not.toContain('PRIVATE-SECRET')
  })
  it('submits a valid new image key with retained bot secret and only writable fields', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, enabled: true, api_key_configured: false })
    await start(); await wrapper.get('[name="api_key"]').setValue('new-image-key')
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    const body = vi.mocked(adminQQImageAPI.saveConfig).mock.calls[0][0]
    expect(body).toMatchObject({ enabled: true, api_key: 'new-image-key' })
    expect(body).not.toHaveProperty('bot_secret')
    expect(body).not.toHaveProperty('api_key_configured')
    expect(body).not.toHaveProperty('encryption_available')
    expect(wrapper.text()).toContain('qqImage.admin.saved')
  })
  it.each([
    [{ status: 400, reason: 'QQ_IMAGE_INVALID' }, 'invalid_request'],
    [{ status: 503, reason: 'QQ_IMAGE_ENCRYPTION_UNAVAILABLE' }, 'encryption_unavailable'],
    [{ status: 403 }, 'forbidden'], [{ status: 409 }, 'conflict'], [{ status: 0 }, 'uncertain']
  ])('uses configuration-specific recovery for %o', async (failure, key) => {
    vi.mocked(adminQQImageAPI.saveConfig).mockRejectedValue(failure)
    await start(); await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain(`qqImage.admin.configValidation.${key}`)
    expect(adminQQImageAPI.saveConfig).toHaveBeenCalledTimes(1)
  })
})

const found = { id: 'discovery1', app_id: 'app1', status: 'found' as const, expires_at: '2099-01-01T00:00:00Z', candidate: { group_openid: 'official-openid', event_id: 'event1', received_at: '2026-10-23T01:00:00Z' } }
async function discover() { await wrapper.get('[data-test="discovery-start"]').trigger('click'); await flushPromises() }
describe('QQ test group discovery', () => {
  it('requires an explicit start and works with saved bot credentials while generation and image credentials are absent', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, enabled: false, api_key_configured: false, api_url: '', model: '', size: '', allowed_groups: [] })
    await start()
    expect(adminQQImageAPI.startDiscovery).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="discovery-start"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
    await discover()
    expect(adminQQImageAPI.startDiscovery).toHaveBeenCalledWith(expect.any(AbortSignal))
    expect(wrapper.text()).toContain('@机器人 /识别群 SECRET-ONCE')
    for (const storage of [localStorage, sessionStorage]) expect(Array.from({ length: storage.length }, (_, i) => storage.getItem(storage.key(i)!)).join()).not.toContain('SECRET-ONCE')
    expect((wrapper.get('[name="enabled"]').element as HTMLInputElement).checked).toBe(false)
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
  })
  it('requires saving bot credentials first and allows saving without Images configuration', async () => {
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, app_id: '', bot_secret_configured: false, api_key_configured: false, api_url: '', model: '', size: '', public_base_url: '', allowed_groups: [] })
    vi.mocked(adminQQImageAPI.saveConfig).mockResolvedValue({ ...config, api_key_configured: false })
    await start()
    expect(wrapper.get('[data-test="discovery-start"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[name="app_id"]').setValue('app1'); await wrapper.get('[name="bot_secret"]').setValue('saved-bot-secret')
    expect(wrapper.get('[data-test="discovery-start"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
    expect(adminQQImageAPI.saveConfig).toHaveBeenCalledWith(expect.objectContaining({ enabled: false, app_id: 'app1', bot_secret: 'saved-bot-secret', api_url: '' }), expect.any(AbortSignal))
    expect(wrapper.get('[data-test="discovery-start"]').attributes('disabled')).toBeUndefined()
  })
  it('only displays the necessary candidate fields and requires a separate confirm action to update groups', async () => {
    await start(); await discover()
    vi.mocked(adminQQImageAPI.discovery).mockResolvedValue({ ...found, command: 'DO-NOT-RESTORE', candidate: { ...found.candidate, content: 'private-content', member_id: 'private-member' } } as typeof found)
    await wrapper.get('[data-test="discovery-refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('official-openid'); expect(wrapper.text()).toContain('event1')
    expect(wrapper.text()).not.toContain('SECRET-ONCE'); expect(wrapper.text()).not.toContain('DO-NOT-RESTORE')
    expect(wrapper.text()).not.toContain('private-content'); expect(wrapper.text()).not.toContain('private-member')
    expect((wrapper.get('[name="allowed_groups"]').element as HTMLTextAreaElement).value).toBe('group1')
    expect(adminQQImageAPI.confirmDiscovery).not.toHaveBeenCalled()
    vi.mocked(adminQQImageAPI.config).mockResolvedValue({ ...config, allowed_groups: ['group1', 'official-openid'] })
    await wrapper.get('[data-test="discovery-confirm"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.confirmDiscovery).toHaveBeenCalledWith('discovery1', expect.any(AbortSignal))
    expect((wrapper.get('[name="allowed_groups"]').element as HTMLTextAreaElement).value).toContain('official-openid')
    expect((wrapper.get('[name="enabled"]').element as HTMLInputElement).checked).toBe(false)
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
  })
  it.each(['app_id', 'bot_secret', 'api_key', 'allowed_groups', 'price'])('clears the one-time command and blocks confirmation when %s changes', async field => {
    await start(); await discover()
    await wrapper.get(`[name="${field}"]`).setValue('changed')
    expect(wrapper.text()).not.toContain('SECRET-ONCE')
    expect(wrapper.get('[data-test="discovery-start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
  })
  it('clears the command before closing even if the close request fails', async () => {
    await start(); await discover()
    vi.mocked(adminQQImageAPI.closeDiscovery).mockRejectedValue({ status: 500 })
    await wrapper.get('[data-test="discovery-close"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.closeDiscovery).toHaveBeenCalledWith('discovery1', expect.any(AbortSignal))
    expect(wrapper.text()).not.toContain('SECRET-ONCE')
    expect(wrapper.text()).toContain('qqImage.admin.discovery.errors.failed')
  })
  it('clears the command at the local expiry and disables candidate confirmation', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] }); vi.setSystemTime(new Date('2026-10-23T01:00:00Z'))
    vi.mocked(adminQQImageAPI.startDiscovery).mockResolvedValue({ ...found, status: 'waiting', candidate: undefined, expires_at: '2026-10-23T01:00:02Z', command: '@机器人 /识别群 SECRET-ONCE' })
    await start(); await discover()
    expect(wrapper.get('[data-test="discovery-countdown"]').exists()).toBe(true)
    vi.advanceTimersByTime(2000); await flushPromises()
    expect(wrapper.text()).not.toContain('SECRET-ONCE')
    expect(wrapper.text()).toContain('qqImage.admin.discovery.states.expired')
    expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
  })
  it.each([409, 410])('clears the command on a status error %s', async status => {
    await start(); await discover()
    vi.mocked(adminQQImageAPI.discovery).mockRejectedValue({ status })
    await wrapper.get('[data-test="discovery-refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('SECRET-ONCE')
    expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
  })
  it.each(['closed', 'expired', 'confirmed'] as const)('clears the command on server state %s', async status => {
    await start(); await discover()
    vi.mocked(adminQQImageAPI.discovery).mockResolvedValue({ ...found, status })
    await wrapper.get('[data-test="discovery-refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('SECRET-ONCE')
    expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
  })
  it('never restores a command from GET and rejects candidates for another bot', async () => {
    vi.mocked(adminQQImageAPI.discovery).mockResolvedValue({ ...found, app_id: 'different-app', command: 'PRIVATE' } as typeof found)
    await start()
    expect(wrapper.text()).not.toContain('PRIVATE'); expect(wrapper.text()).not.toContain('official-openid')
    expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
  })
  it.each(['logout', 'account', 'role', 'same-user-login'])('clears an already displayed command on %s', async change => {
    await start(); await discover()
    if (change === 'logout') auth.isAuthenticated = false
    if (change === 'account') auth.user.id = 2
    if (change === 'role') auth.user.role = 'user'
    if (change === 'same-user-login') auth.sessionRevision = 'B'
    await flushPromises()
    expect(wrapper.text()).not.toContain('SECRET-ONCE')
    expect(wrapper.text()).toContain('qqImage.sessionChanged')
    expect(wrapper.find('[data-test="discovery-confirm"]').exists()).toBe(false)
  })
  it('does not offer confirm for a found response without a candidate', async () => {
    vi.mocked(adminQQImageAPI.discovery).mockResolvedValue({ ...found, candidate: undefined })
    await start(); expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
  })
  it('explains official group_openid and silent bot discovery in both languages', () => {
    expect(zhQQImage.qqImage.admin.groupsHint).toContain('官方group_openid，不是普通QQ群号')
    expect(zhQQImage.qqImage.admin.discovery.noReply).toContain('机器人不会在群里回复')
    expect(enQQImage.qqImage.admin.groupsHint).toContain('not ordinary QQ group numbers')
    expect(enQQImage.qqImage.admin.discovery.noReply).toContain('will not reply')
  })
  it('discards a late start response after account replacement' , async () => {
    let finish!: (value: typeof found & { command: string }) => void
    vi.mocked(adminQQImageAPI.startDiscovery).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await start(); await wrapper.get('[data-test="discovery-start"]').trigger('click')
    auth.sessionRevision = 'B'; localStorage.setItem('auth_session_id', 'B'); await flushPromises()
    finish({ ...found, command: 'LATE-PRIVATE' }); await flushPromises()
    expect(wrapper.text()).not.toContain('LATE-PRIVATE'); expect(wrapper.text()).not.toContain('official-openid')
    expect(wrapper.text()).toContain('qqImage.sessionChanged')
  })
  it('discards a late status response after local configuration changes', async () => {
    let finish!: (value: typeof found) => void
    await start(); await discover()
    vi.mocked(adminQQImageAPI.discovery).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await wrapper.get('[data-test="discovery-refresh"]').trigger('click')
    await wrapper.get('[name="app_id"]').setValue('new-app')
    finish(found); await flushPromises()
    expect(wrapper.text()).not.toContain('official-openid'); expect(wrapper.text()).not.toContain('SECRET-ONCE')
    expect(wrapper.get('[data-test="discovery-confirm"]').attributes('disabled')).toBeDefined()
  })
})

describe('QQ reply template tab isolation', () => {
  it('opens an independent panel while preserving unsaved configuration and credentials', async () => {
    await start(); await wrapper.get('[name="bot_secret"]').setValue('local-bot-draft')
    await wrapper.get('[data-test="qq-tab-templates"]').trigger('click'); await flushPromises()
    expect(wrapper.find('q-q-reply-templates-panel-stub').exists()).toBe(true)
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
    await wrapper.get('[data-test="qq-tab-config"]').trigger('click'); await flushPromises()
    expect((wrapper.get('[name="bot_secret"]').element as HTMLInputElement).value).toBe('local-bot-draft')
    expect(adminQQImageAPI.config).toHaveBeenCalledTimes(1)
  })
})
