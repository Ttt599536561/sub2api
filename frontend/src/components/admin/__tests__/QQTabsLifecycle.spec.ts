import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import QQImagePanel from '@/components/admin/QQImagePanel.vue'
import WelfareSettingsView from '@/views/admin/WelfareSettingsView.vue'
import { adminQQImageAPI } from '@/api/qqImage'
vi.mock('@/api/admin/welfare', () => ({ adminWelfareAPI: { getSettings: vi.fn().mockResolvedValue({ enabled: false, launch_at: null, rules_version: 3 }) } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({}) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/admin/welfare/WelfareStatisticsPanel.vue', () => ({ default: { template: '<div />' } }))
const locale = ref('zh')
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale, t: (key: string) => key, te: () => true }) }))
vi.mock('@/api/qqImage', () => ({ adminQQImageAPI: { connection: vi.fn(), config: vi.fn(), saveConfig: vi.fn(), orders: vi.fn(), grants: vi.fn(), audit: vi.fn(), discovery: vi.fn(), replyTemplates: vi.fn(), previewReplyTemplate: vi.fn(), saveReplyTemplates: vi.fn() } }))
const auth = reactive({ user: { id: 1, role: 'admin', email: 'fixture@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const config = { enabled: false, app_id: 'app1', api_url: 'https://images.example.test/v1', model: 'image-1', size: '1024x1024', price: '0.12000000', allowed_groups: ['group1'], public_base_url: 'https://site.example.test', user_daily_limit: 20, user_daily_amount: '10.00000000', group_concurrency: 2, global_concurrency: 5, queue_limit: 50, queue_timeout_seconds: 300, deadline_seconds: 7200, retention_days: 7, prompt_max_chars: 2000, bot_secret_configured: true, api_key_configured: true, encryption_available: true }
const templates = { schema_version: 1, catalog_version: 1, revision: 3, overrides: {}, catalog: [ { key: 'common.menu', label: '菜单', category: 'common', default_text: 'default', allowed_variables: [], required_variables: [], variable_limits: {} } ] }
let wrapper: ReturnType<typeof mount>
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear(); sessionStorage.clear()
  auth.user = { id: 1, role: 'admin', email: 'fixture@example.test' }; auth.sessionRevision = 'A'; auth.isAuthenticated = true
  localStorage.setItem('auth_user', JSON.stringify(auth.user)); localStorage.setItem('auth_session_id', 'A')
  vi.mocked(adminQQImageAPI.connection).mockResolvedValue({ state: 'webhook' })
  vi.mocked(adminQQImageAPI.discovery).mockResolvedValue(null)
  vi.mocked(adminQQImageAPI.config).mockResolvedValue(structuredClone(config))
  vi.mocked(adminQQImageAPI.orders).mockResolvedValue([])
  vi.mocked(adminQQImageAPI.grants).mockResolvedValue([]); vi.mocked(adminQQImageAPI.audit).mockResolvedValue([])
  vi.mocked(adminQQImageAPI.replyTemplates).mockResolvedValue(structuredClone(templates))
  vi.mocked(adminQQImageAPI.previewReplyTemplate).mockResolvedValue({ valid: true, text: 'example', length: 7, worst_length: 7, max_length: 2000, errors: [] })
})
afterEach(() => wrapper?.unmount())
async function start() { wrapper = mount(QQImagePanel, { global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } }); await flushPromises() }
async function openTemplates() {
  await wrapper.get('[data-test="qq-tab-templates"]').trigger('click'); await flushPromises()
}
async function startWelfare() {
  wrapper = mount(WelfareSettingsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, WelfareStatisticsPanel: true, RouterLink: { template: '<a><slot /></a>' } } } })
  await flushPromises()
  expect(adminQQImageAPI.config).not.toHaveBeenCalled()
  await wrapper.get('[data-test="tab-qq"]').trigger('click'); await flushPromises()
}
it('retains unsaved reply text when visiting a sibling configuration tab', async () => {
  await start(); await openTemplates()
  await wrapper.get('[data-test="template-text"]').setValue('UNSAVED IMPORTANT DRAFT')
  await wrapper.get('[data-test="qq-tab-config"]').trigger('click'); await flushPromises()
  await openTemplates()
  expect((wrapper.get('[data-test="template-text"]').element as HTMLTextAreaElement).value).toBe('UNSAVED IMPORTANT DRAFT')
  expect(adminQQImageAPI.replyTemplates).toHaveBeenCalledOnce()
  expect(adminQQImageAPI.saveReplyTemplates).not.toHaveBeenCalled()
})
it.each(['templates', 'orders', 'grants', 'audit'])('retains unsaved configuration and credentials after visiting %s', async tab => {
  await start(); await wrapper.get('[name="api_key"]').setValue('unsaved-fake-key')
  await wrapper.get('[name="price"]').setValue('0.27')
  await wrapper.get(`[data-test="qq-tab-${tab}"]`).trigger('click'); await flushPromises()
  await wrapper.get('[data-test="qq-tab-config"]').trigger('click'); await flushPromises()
  expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('unsaved-fake-key')
  expect((wrapper.get('[name="price"]').element as HTMLInputElement).value).toBe('0.27')
  expect(adminQQImageAPI.config).toHaveBeenCalledOnce()
  expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
})
it('requires discard confirmation for explicit refresh and supports keeping the draft', async () => {
  await start(); await wrapper.get('[name="api_key"]').setValue('unsaved-fake-key')
  await wrapper.get('button').trigger('click'); await flushPromises()
  expect(adminQQImageAPI.config).toHaveBeenCalledOnce()
  expect(wrapper.get('[data-test="confirm-config-reload"]').exists()).toBe(true)
  await wrapper.get('[data-test="cancel-config-reload"]').trigger('click')
  expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('unsaved-fake-key')
  await wrapper.get('button').trigger('click'); await flushPromises()
  await wrapper.get('[data-test="confirm-config-reload"]').trigger('click'); await flushPromises()
  expect(adminQQImageAPI.config).toHaveBeenCalledTimes(2)
  expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('')
})
it('keeps a failed explicit refresh from discarding the existing draft', async () => {
  await start(); await wrapper.get('[name="api_key"]').setValue('unsaved-fake-key')
  vi.mocked(adminQQImageAPI.config).mockRejectedValueOnce({ status: 503 })
  await wrapper.get('button').trigger('click'); await flushPromises()
  await wrapper.get('[data-test="confirm-config-reload"]').trigger('click'); await flushPromises()
  expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('unsaved-fake-key')
  expect(wrapper.text()).toContain('qqImage.errors.failed')
})
it('retains both drafts when leaving and returning to the outer welfare QQ tab', async () => {
  await startWelfare(); await wrapper.get('[name="api_key"]').setValue('unsaved-fake-key')
  await openTemplates(); await wrapper.get('[data-test="template-text"]').setValue('UNSAVED IMPORTANT DRAFT')
  await wrapper.get('[data-test="tab-statistics"]').trigger('click'); await flushPromises()
  await wrapper.get('[data-test="tab-qq"]').trigger('click'); await flushPromises()
  expect((wrapper.get('[data-test="template-text"]').element as HTMLTextAreaElement).value).toBe('UNSAVED IMPORTANT DRAFT')
  await wrapper.get('[data-test="qq-tab-config"]').trigger('click'); await flushPromises()
  expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('unsaved-fake-key')
  expect(adminQQImageAPI.config).toHaveBeenCalledOnce(); expect(adminQQImageAPI.replyTemplates).toHaveBeenCalledOnce()
})
it('blocks both tab layers during template save and accepts the eventual committed result', async () => {
  let signal!: AbortSignal, finish!: (value: typeof templates) => void
  vi.mocked(adminQQImageAPI.saveReplyTemplates).mockImplementation((_body, receivedSignal) => { signal = receivedSignal!; return new Promise(resolve => { finish = resolve }) })
  await startWelfare(); await openTemplates()
  await wrapper.get('[data-test="template-text"]').setValue('PENDING DRAFT')
  await wrapper.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
  expect(wrapper.get('[data-test="qq-tab-config"]').attributes('disabled')).toBeDefined()
  expect(wrapper.get('[data-test="tab-statistics"]').attributes('disabled')).toBeDefined()
  await wrapper.get('[data-test="qq-tab-config"]').trigger('click')
  await wrapper.get('[data-test="tab-statistics"]').trigger('click'); await flushPromises()
  expect(signal.aborted).toBe(false)
  finish({ ...templates, revision: 4, overrides: { 'common.menu': 'PENDING DRAFT' } }); await flushPromises()
  expect(wrapper.text()).toContain('qqImage.admin.templates.saved')
  expect(wrapper.get('[data-test="tab-statistics"]').attributes('disabled')).toBeUndefined()
  expect(signal.aborted).toBe(false)
})
it('blocks the outer welfare tabs during configuration save without canceling it', async () => {
  let signal!: AbortSignal, finish!: (value: typeof config) => void
  vi.mocked(adminQQImageAPI.saveConfig).mockImplementation((_body, receivedSignal) => { signal = receivedSignal!; return new Promise(resolve => { finish = resolve }) })
  await startWelfare(); await wrapper.get('[name="price"]').setValue('0.27')
  await wrapper.get('[data-test="qq-config"]').trigger('submit'); await flushPromises()
  expect(wrapper.get('[data-test="tab-settings"]').attributes('disabled')).toBeDefined()
  await wrapper.get('[data-test="tab-settings"]').trigger('click'); await flushPromises()
  expect(signal.aborted).toBe(false)
  finish({ ...config, price: '0.27' }); await flushPromises()
  expect(wrapper.get('[data-test="tab-settings"]').attributes('disabled')).toBeUndefined()
  expect(wrapper.text()).toContain('qqImage.admin.saved')
})
it.each(['role', 'session'])('still clears hidden drafts after %s changes', async change => {
  await startWelfare(); await wrapper.get('[name="api_key"]').setValue('unsaved-fake-key')
  await openTemplates(); await wrapper.get('[data-test="template-text"]').setValue('UNSAVED IMPORTANT DRAFT')
  await wrapper.get('[data-test="tab-statistics"]').trigger('click'); await flushPromises()
  if (change === 'role') auth.user.role = 'user'
  else { auth.sessionRevision = 'B'; localStorage.setItem('auth_session_id', 'B') }
  await flushPromises(); await wrapper.get('[data-test="tab-qq"]').trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('qqImage.sessionChanged')
  expect(wrapper.find('[data-test="template-text"]').exists()).toBe(false)
  expect(wrapper.find('[name="api_key"]').exists()).toBe(false)
  expect(adminQQImageAPI.config).toHaveBeenCalledOnce()
})
it('does not display a failed orders read as a configuration error when restoring its draft', async () => {
  await start(); await wrapper.get('[name="api_key"]').setValue('unsaved-fake-key')
  vi.mocked(adminQQImageAPI.orders).mockRejectedValueOnce({ status: 503 })
  await wrapper.get('[data-test="qq-tab-orders"]').trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('qqImage.errors.failed')
  await wrapper.get('[data-test="qq-tab-config"]').trigger('click'); await flushPromises()
  expect(wrapper.text()).not.toContain('qqImage.errors.failed')
  expect((wrapper.get('[name="api_key"]').element as HTMLInputElement).value).toBe('unsaved-fake-key')
})
