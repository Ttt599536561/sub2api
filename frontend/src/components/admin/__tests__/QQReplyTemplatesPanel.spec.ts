import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import QQReplyTemplatesPanel from '../QQReplyTemplatesPanel.vue'
import { adminQQImageAPI, type QQReplyTemplate } from '@/api/qqImage'
import zh from '@/i18n/locales/zh/qqImage'
import en from '@/i18n/locales/en/qqImage'

const locale = ref('zh')
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale, t: (key: string, values?: Record<string, unknown>) => key + (values ? JSON.stringify(values) : '') }) }))
vi.mock('@/api/qqImage', () => ({ adminQQImageAPI: {
  replyTemplates: vi.fn(), saveReplyTemplates: vi.fn(), previewReplyTemplate: vi.fn(), replyTemplateRevisions: vi.fn(), replyTemplateRevision: vi.fn(), rollbackReplyTemplates: vi.fn(), saveConfig: vi.fn(), connection: vi.fn()
} }))
const auth = reactive({ user: { id: 1, role: 'admin', email: 'admin@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const catalog = [
  { key: 'common.menu', label: '统一菜单', label_en: 'Unified menu', category: 'common', default_text: '菜单 {{rules}}', allowed_variables: ['rules', 'site_url'], required_variables: [], variable_limits: { rules: 80, site_url: 200 } },
  { key: 'image.help', label: '生图帮助', category: 'image', default_text: '生图帮助默认文案', allowed_variables: [], required_variables: [], variable_limits: {} },
  { key: 'iq.ack', label: '智商确认', category: 'iq', default_text: '正在处理', allowed_variables: [], required_variables: [], variable_limits: {} }
]
// Snapshot of the real Go catalog exported for the free-editing contract (2026-10-24).
const serverCatalog: QQReplyTemplate[] = [
  { key: 'common.menu', label: '统一帮助菜单', category: 'common', default_text: '【智商查询】\n{{iq_commands}}\n\n可用分组：\n{{group_names}}\n\n【生图】\n{{image_commands}}\n\n{{usage_rules}}', allowed_variables: ['group_names', 'image_commands', 'iq_commands', 'usage_rules'], required_variables: [], variable_limits: { group_names: 680, image_commands: 256, iq_commands: 128, usage_rules: 256 } },
  { key: 'iq.ack', label: '智商查询受理', category: 'iq', default_text: '收到，正在查询并生成截图，请稍候。', allowed_variables: ['group_title'], required_variables: [], variable_limits: { group_title: 160 } },
  { key: 'iq.caption', label: '智商截图说明', category: 'iq', default_text: '{{group_title}}\n截图时间：{{captured_at}}（{{timezone}}）\n{{query_rules}}{{schedule_rules}}', allowed_variables: ['captured_at', 'group_title', 'query_rules', 'schedule_rules', 'timezone'], required_variables: [], variable_limits: { captured_at: 64, group_title: 160, query_rules: 256, schedule_rules: 256, timezone: 64 } },
  { key: 'iq.error.capture', label: '截图失败', category: 'iq', default_text: '本次截图生成失败，请稍后重新查询。{{query_rules}}', allowed_variables: ['query_rules'], required_variables: [], variable_limits: { query_rules: 256 } },
  { key: 'iq.error.station', label: '检测站异常', category: 'iq', default_text: '暂时无法读取分组状态，请稍后重新查询。{{query_rules}}', allowed_variables: ['query_rules'], required_variables: [], variable_limits: { query_rules: 256 } },
  { key: 'iq.error.delivery_unknown', label: '截图送达未知', category: 'iq', default_text: '{{delivery_rules}}如果未收到，请稍后重试查询指令。{{query_rules}}', allowed_variables: ['delivery_rules', 'query_rules'], required_variables: [], variable_limits: { delivery_rules: 256, query_rules: 256 } },
  { key: 'image.help', label: '生图帮助', category: 'image', default_text: '{{image_commands}}\n{{availability}}{{model_details}}{{price_details}}{{authorization_rules}}{{settlement_rules}}{{visibility_rules}}{{redelivery_rules}}', allowed_variables: ['authorization_rules', 'availability', 'image_commands', 'model_details', 'price_details', 'redelivery_rules', 'settlement_rules', 'visibility_rules'], required_variables: [], variable_limits: { authorization_rules: 128, availability: 64, image_commands: 160, model_details: 512, price_details: 64, redelivery_rules: 96, settlement_rules: 96, visibility_rules: 96 } },
  { key: 'image.generate_usage', label: '生图用法', category: 'image', default_text: '请使用 /生图 提示词；可用 /生图帮助 查看固定价格。', allowed_variables: [], required_variables: [], variable_limits: {  } },
  { key: 'binding.usage', label: '绑定用法', category: 'binding', default_text: '请使用 /绑定 发起本人申请。{{binding_rules}}', allowed_variables: ['binding_rules'], required_variables: [], variable_limits: { binding_rules: 256 } },
  { key: 'binding.request', label: '绑定申请', category: 'binding', default_text: '请仅绑定您本人刚发起的申请：{{binding_url}}\n绑定申请编号：{{request_id}}。{{binding_rules}}', allowed_variables: ['binding_rules', 'binding_url', 'request_id'], required_variables: [], variable_limits: { binding_rules: 256, binding_url: 768, request_id: 128 } },
  { key: 'binding.manual', label: '绑定手动备用', category: 'binding', default_text: '绑定申请编号：{{request_id}}。{{binding_rules}}', allowed_variables: ['binding_rules', 'request_id'], required_variables: [], variable_limits: { binding_rules: 256, request_id: 128 } },
  { key: 'binding.confirm_usage', label: '确认绑定用法', category: 'binding', default_text: '{{confirmation_rules}}', allowed_variables: ['confirmation_rules'], required_variables: [], variable_limits: { confirmation_rules: 256 } },
  { key: 'binding.success', label: '绑定成功', category: 'binding', default_text: '{{success_rules}}', allowed_variables: ['success_rules'], required_variables: [], variable_limits: { success_rules: 256 } },
  { key: 'order.accepted', label: '订单受理', category: 'order', default_text: '订单 {{order_id}} 已受理，已预留 US${{price}}。{{settlement_rules}}\n{{order_url}}', allowed_variables: ['order_id', 'order_url', 'price', 'settlement_rules'], required_variables: [], variable_limits: { order_id: 128, order_url: 768, price: 32, settlement_rules: 96 } },
  { key: 'order.status_usage', label: '任务用法', category: 'order', default_text: '请使用 /任务 订单号。', allowed_variables: [], required_variables: [], variable_limits: {  } },
  { key: 'order.status', label: '任务状态', category: 'order', default_text: '订单 {{order_id}}：任务 {{order_status}}；资金 {{funds_status}}；投递 {{delivery_status}}。\n{{private_rules}}{{order_url}}', allowed_variables: ['delivery_status', 'funds_status', 'order_id', 'order_status', 'order_url', 'private_rules'], required_variables: [], variable_limits: { delivery_status: 64, funds_status: 64, order_id: 128, order_status: 64, order_url: 768, private_rules: 96 } },
  { key: 'order.redelivery_usage', label: '重发用法', category: 'order', default_text: '请使用 /重发 订单号。', allowed_variables: [], required_variables: [], variable_limits: {  } },
  { key: 'order.redelivery', label: '重发受理', category: 'order', default_text: '正在重发订单 {{order_id}} 的已有图片，{{redelivery_rules}}若 QQ 无法投递，请到原网站领取：{{order_url}}', allowed_variables: ['order_id', 'order_url', 'redelivery_rules'], required_variables: [], variable_limits: { order_id: 128, order_url: 768, redelivery_rules: 96 } },
  { key: 'error.insufficient_balance', label: '余额不足', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } },
  { key: 'error.limit', label: '请求或额度限制', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } },
  { key: 'error.expired', label: '申请或图片过期', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } },
  { key: 'error.disabled', label: '入口未开放', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } },
  { key: 'error.conflict', label: '状态冲突', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } },
  { key: 'error.forbidden', label: '未获授权', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } },
  { key: 'error.invalid', label: '指令无效', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } },
  { key: 'error.unavailable', label: '服务暂不可用', category: 'error', default_text: '{{error_rules}}', allowed_variables: ['error_rules'], required_variables: [], variable_limits: { error_rules: 256 } }
]
const configuration = { schema_version: 1, catalog_version: 1, revision: 3, overrides: { 'iq.ack': '已接收' }, catalog }
const preview = { valid: true, text: '<svg onload="alert(1)">示例</svg>😀', length: 32, worst_length: 280, max_length: 2000, errors: [] }
const revision = { revision: 2, created_at: '2026-10-24T00:00:00Z', actor_id: 1, overrides: { 'common.menu': '旧菜单 {{rules}}' } }
let wrapper: ReturnType<typeof mount> | undefined
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
async function start() { wrapper = mount(QQReplyTemplatesPanel); await flushPromises(); return wrapper }
beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear(); sessionStorage.clear(); locale.value = 'zh'
  auth.user = { id: 1, role: 'admin', email: 'admin@example.test' }; auth.sessionRevision = 'A'; auth.isAuthenticated = true
  localStorage.setItem('auth_user', JSON.stringify(auth.user)); localStorage.setItem('auth_session_id', 'A')
  vi.mocked(adminQQImageAPI.replyTemplates).mockResolvedValue(structuredClone(configuration))
  vi.mocked(adminQQImageAPI.saveReplyTemplates).mockImplementation(async input => ({ ...structuredClone(configuration), revision: 4, overrides: input.overrides }))
  vi.mocked(adminQQImageAPI.previewReplyTemplate).mockResolvedValue(preview)
  vi.mocked(adminQQImageAPI.replyTemplateRevisions).mockResolvedValue([revision])
  vi.mocked(adminQQImageAPI.replyTemplateRevision).mockResolvedValue(revision)
  vi.mocked(adminQQImageAPI.rollbackReplyTemplates).mockResolvedValue({ ...configuration, revision: 4, overrides: revision.overrides })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })
describe('QQ reply templates', () => {
  it('loads dynamic catalog, defaults and overrides and selects templates by category', async () => {
    const panel = await start()
    expect(panel.findAll('[data-test="template-select"] option')).toHaveLength(3)
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('菜单 {{rules}}')
    await panel.get('[data-test="category-select"]').setValue('iq')
    expect(panel.findAll('[data-test="template-select"] option')).toHaveLength(1)
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('已接收')
    expect(panel.get('[data-test="template-select"]').text()).toContain('智商确认')
  })
  it('uses optional English labels with catalog-label fallback', async () => {
    locale.value = 'en'; const panel = await start()
    expect(panel.get('[data-test="template-select"]').text()).toContain('Unified menu')
    expect(panel.get('[data-test="template-select"]').text()).toContain('生图帮助')
  })
  it('inserts allowed variables at the caret and hides technical budgets', async () => {
    const panel = await start(); const editor = panel.get('textarea')
    await editor.setValue('😀 {{rules}}')
    ;(editor.element as HTMLTextAreaElement).setSelectionRange(2, 2)
    await panel.get('[data-test="variable-site_url"]').trigger('click')
    expect((editor.element as HTMLTextAreaElement).value).toBe('😀{{site_url}} {{rules}}')
    expect(panel.get('[data-test="template-length"]').text()).toContain('"length":24')
    expect(panel.get('[data-test="variable-rules"]').text()).toContain('qqImage.admin.templates.optionalContent')
    expect(panel.get('[data-test="variable-rules"]').attributes('disabled')).toBeUndefined()
    expect(panel.text()).not.toContain('"limit":200')
    expect(zh.qqImage.admin.templates.lengthHint).not.toContain('UTF-16')
    expect(en.qqImage.admin.templates.lengthHint).not.toContain('UTF-16')
  })
  it('previews fake data as plain text without HTML execution or configuration writes', async () => {
    const panel = await start()
    await panel.get('[data-test="preview-template"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenCalledWith({ key: 'common.menu', text: '菜单 {{rules}}' }, expect.any(AbortSignal))
    expect(panel.get('pre[data-test="template-preview"]').text()).toBe(preview.text)
    expect(panel.find('svg').exists()).toBe(false)
    expect(panel.text()).toContain('qqImage.admin.templates.livePreviewHint')
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled(); expect(adminQQImageAPI.connection).not.toHaveBeenCalled()
  })
  it('saves only overrides and revision, preserving other templates and separate QQ settings', async () => {
    const panel = await start(); await panel.get('textarea').setValue('新菜单 😀\n{{rules}}')
    await panel.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.saveReplyTemplates).toHaveBeenCalledWith({ expected_revision: 3, overrides: { 'common.menu': '新菜单 😀\n{{rules}}', 'iq.ack': '已接收' } }, expect.any(AbortSignal))
    expect(panel.text()).toContain('qqImage.admin.templates.saved')
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
    for (const storage of [localStorage, sessionStorage]) expect(Array.from({ length: storage.length }, (_, i) => storage.getItem(storage.key(i)!)).join()).not.toContain('新菜单')
  })
  it('restores defaults only in draft until save and omits default text from overrides', async () => {
    const panel = await start(); await panel.get('textarea').setValue('新菜单 {{rules}}')
    await panel.get('[data-test="restore-selected"]').trigger('click')
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('菜单 {{rules}}')
    await panel.get('[data-test="restore-all"]').trigger('click')
    expect(adminQQImageAPI.saveReplyTemplates).not.toHaveBeenCalled()
    await panel.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
    expect(vi.mocked(adminQQImageAPI.saveReplyTemplates).mock.calls[0][0]).toEqual({ expected_revision: 3, overrides: {} })
  })
  it.each(['欢迎来到糖果小站，有问题请联系管理员。', '{{rules}} {{rules}}'])('saves ordinary text or repeated optional content: %s', async text => {
    const panel = await start(); await panel.get('textarea').setValue(text)
    expect(panel.get('[data-test="save-templates"]').attributes('disabled')).toBeUndefined()
    expect(panel.text()).not.toContain('qqImage.admin.templates.requiredVariableList')
    await panel.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
    expect(vi.mocked(adminQQImageAPI.saveReplyTemplates).mock.calls[0][0].overrides['common.menu']).toBe(text)
  })
  it('fails closed on initial read failure and supports explicit retry', async () => {
    vi.mocked(adminQQImageAPI.replyTemplates).mockRejectedValueOnce({ message: 'PRIVATE-ERROR' })
    const panel = await start()
    expect(panel.find('textarea').exists()).toBe(false)
    expect(panel.find('[data-test="save-templates"]').exists()).toBe(false)
    expect(panel.text()).toContain('qqImage.admin.templates.loadFailed'); expect(panel.text()).not.toContain('PRIVATE-ERROR')
    await panel.get('[data-test="reload-templates"]').trigger('click'); await flushPromises()
    expect(panel.find('textarea').exists()).toBe(true)
  })
  it('preserves unsaved text and the expected revision on conflict without retry or overwrite', async () => {
    vi.mocked(adminQQImageAPI.saveReplyTemplates).mockRejectedValue({ status: 409 })
    const panel = await start(); await panel.get('textarea').setValue('本页草稿 {{rules}}')
    await panel.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('本页草稿 {{rules}}')
    expect(panel.text()).toContain('qqImage.admin.templates.conflict')
    expect(adminQQImageAPI.replyTemplates).toHaveBeenCalledTimes(1); expect(adminQQImageAPI.saveReplyTemplates).toHaveBeenCalledTimes(1)
    await panel.get('[data-test="reload-templates"]').trigger('click')
    expect(adminQQImageAPI.replyTemplates).toHaveBeenCalledTimes(1)
    expect(panel.find('[data-test="confirm-reload"]').exists()).toBe(true)
  })
  it('loads revision details and requires an explicit confirmation before rollback', async () => {
    const panel = await start(); await panel.get('[data-test="history-templates"]').trigger('click'); await flushPromises()
    await panel.get('[data-test="revision-2"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.replyTemplateRevision).toHaveBeenCalledWith(2, expect.any(AbortSignal))
    expect(panel.text()).toContain('旧菜单 {{rules}}')
    await panel.get('[data-test="rollback-template"]').trigger('click')
    expect(adminQQImageAPI.rollbackReplyTemplates).not.toHaveBeenCalled()
    expect(panel.find('[role="alertdialog"]').exists()).toBe(true)
    await panel.get('[data-test="confirm-rollback"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.rollbackReplyTemplates).toHaveBeenCalledWith({ expected_revision: 3, revision: 2 }, expect.any(AbortSignal))
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('旧菜单 {{rules}}')
  })
  it('discards stale preview after a draft edit and aborts its request', async () => {
    const pending = deferred<typeof preview>(); vi.mocked(adminQQImageAPI.previewReplyTemplate).mockReturnValue(pending.promise)
    const panel = await start(); await panel.get('[data-test="preview-template"]').trigger('click')
    const signal = vi.mocked(adminQQImageAPI.previewReplyTemplate).mock.calls[0][1]
    await panel.get('textarea').setValue('更新 {{rules}}'); expect(signal?.aborted).toBe(true)
    pending.resolve(preview); await flushPromises(); expect(panel.find('[data-test="template-preview"]').exists()).toBe(false)
  })
  it.each(['logout', 'account', 'role', 'same-user-login'])('clears draft, history and late preview on %s', async change => {
    const pending = deferred<typeof preview>(); vi.mocked(adminQQImageAPI.previewReplyTemplate).mockReturnValue(pending.promise)
    const panel = await start(); await panel.get('textarea').setValue('私人草稿 {{rules}}')
    await panel.get('[data-test="history-templates"]').trigger('click'); await flushPromises()
    await panel.get('[data-test="revision-2"]').trigger('click'); await flushPromises()
    await panel.get('[data-test="preview-template"]').trigger('click')
    const signal = vi.mocked(adminQQImageAPI.previewReplyTemplate).mock.calls[0][1]
    if (change === 'logout') auth.isAuthenticated = false
    if (change === 'account') auth.user.id = 2
    if (change === 'role') auth.user.role = 'user'
    if (change === 'same-user-login') auth.sessionRevision = 'B'
    await flushPromises(); pending.resolve(preview); await flushPromises()
    expect(signal?.aborted).toBe(true); expect(panel.find('textarea').exists()).toBe(false)
    expect(panel.text()).not.toContain('私人草稿'); expect(panel.text()).not.toContain('旧菜单'); expect(panel.text()).not.toContain(preview.text)
    expect(panel.text()).toContain('qqImage.sessionChanged')
  })
  it('ignores save completing after logout', async () => {
    const pending = deferred<typeof configuration>(); vi.mocked(adminQQImageAPI.saveReplyTemplates).mockReturnValue(pending.promise)
    const panel = await start(); await panel.get('textarea').setValue('草稿 {{rules}}')
    await panel.get('[data-test="save-templates"]').trigger('click'); auth.isAuthenticated = false
    pending.resolve({ ...configuration, revision: 4 }); await flushPromises()
    expect(panel.text()).not.toContain('qqImage.admin.templates.saved'); expect(panel.find('textarea').exists()).toBe(false)
    expect(vi.mocked(adminQQImageAPI.saveReplyTemplates).mock.calls[0][1]?.aborted).toBe(true)
  })
  it('aborts load on unmount and never installs the late data', async () => {
    const pending = deferred<typeof configuration>(); vi.mocked(adminQQImageAPI.replyTemplates).mockReturnValue(pending.promise)
    const panel = await start(); const signal = vi.mocked(adminQQImageAPI.replyTemplates).mock.calls[0][0]
    panel.unmount(); wrapper = undefined; pending.resolve(configuration); await flushPromises()
    expect(signal?.aborted).toBe(true); expect(adminQQImageAPI.saveReplyTemplates).not.toHaveBeenCalled()
  })
})


describe('QQ reply template edge cases', () => {
  it('permits 1000 surrogate-pair emoji and blocks the next one at the UTF-16 boundary', async () => {
    const panel = await start(); await panel.get('[data-test="template-select"]').setValue('image.help')
    await panel.get('textarea').setValue('😀'.repeat(1000))
    expect(panel.get('[data-test="save-templates"]').attributes('disabled')).toBeUndefined()
    await panel.get('textarea').setValue('😀'.repeat(1001))
    expect(panel.get('[data-test="save-templates"]').attributes('disabled')).toBeDefined()
    expect(panel.text()).toContain('qqImage.admin.templates.tooLong')
  })
  it('never shows a previous template preview after switching scenarios', async () => {
    const pending = deferred<typeof preview>(); vi.mocked(adminQQImageAPI.previewReplyTemplate).mockReturnValue(pending.promise)
    const panel = await start(); await panel.get('[data-test="preview-template"]').trigger('click')
    await panel.get('[data-test="template-select"]').setValue('image.help')
    pending.resolve(preview); await flushPromises()
    expect(panel.find('[data-test="template-preview"]').exists()).toBe(false)
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('生图帮助默认文案')
  })
  it('ignores history arriving after a cross-tab login replacement', async () => {
    const pending = deferred<typeof revision[]>(); vi.mocked(adminQQImageAPI.replyTemplateRevisions).mockReturnValue(pending.promise)
    const panel = await start(); await panel.get('[data-test="history-templates"]').trigger('click')
    localStorage.setItem('auth_session_id', 'replacement'); window.dispatchEvent(new Event('storage'))
    pending.resolve([revision]); await flushPromises()
    expect(panel.text()).toContain('qqImage.sessionChanged'); expect(panel.find('[data-test="revision-2"]').exists()).toBe(false)
    expect(vi.mocked(adminQQImageAPI.replyTemplateRevisions).mock.calls[0][0]?.aborted).toBe(true)
  })
  it('preserves current drafts if a rollback conflicts with a newer revision', async () => {
    vi.mocked(adminQQImageAPI.rollbackReplyTemplates).mockRejectedValue({ status: 409 })
    const panel = await start(); await panel.get('textarea').setValue('仍应保留 {{rules}}')
    await panel.get('[data-test="history-templates"]').trigger('click'); await flushPromises()
    await panel.get('[data-test="revision-2"]').trigger('click'); await flushPromises()
    await panel.get('[data-test="rollback-template"]').trigger('click'); await panel.get('[data-test="confirm-rollback"]').trigger('click'); await flushPromises()
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('仍应保留 {{rules}}')
    expect(panel.text()).toContain('qqImage.admin.templates.conflict')
    expect(adminQQImageAPI.replyTemplates).toHaveBeenCalledTimes(1); expect(adminQQImageAPI.rollbackReplyTemplates).toHaveBeenCalledTimes(1)
  })
})

describe('QQ reply preview diagnostics', () => {
  it('translates controlled validation codes and never exposes unexpected error text', async () => {
    vi.mocked(adminQQImageAPI.previewReplyTemplate).mockResolvedValue({ ...preview, valid: false, text: '', errors: ['unknown_variable', 'empty_rendered_text', 'PRIVATE-ERROR'] })
    const panel = await start(); await panel.get('[data-test="preview-template"]').trigger('click'); await flushPromises()
    expect(panel.text()).toContain('qqImage.admin.templates.validation.unknownVariable')
    expect(panel.text()).toContain('qqImage.admin.templates.validation.emptyRenderedText')
    expect(panel.text()).not.toContain('PRIVATE-ERROR')
    expect(panel.text()).not.toContain('required_variable_once')
  })
})

describe('QQ reply live editor', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })
  async function settlePreview() { await vi.advanceTimersByTimeAsync(350); await flushPromises() }
  function localizedCatalog() {
    vi.mocked(adminQQImageAPI.replyTemplates).mockResolvedValue({ ...structuredClone(configuration), catalog: [
      { ...catalog[0], default_text: '欢迎\n{{iq_commands}}', allowed_variables: ['iq_commands', 'group_names'], required_variables: [], variable_limits: { iq_commands: 128, group_names: 680 } },
      catalog[1]
    ] })
  }
  it('automatically previews initial text and coalesces edits without saving', async () => {
    const panel = await start(); await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'common.menu', text: '菜单 {{rules}}' }, expect.any(AbortSignal))
    expect(panel.get('[data-test="template-preview"]').text()).toBe(preview.text)
    await panel.get('textarea').setValue('第一 {{rules}}')
    expect(panel.find('[data-test="template-preview"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(200)
    await panel.get('textarea').setValue('第二 {{rules}}')
    await vi.advanceTimersByTimeAsync(349)
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1); await flushPromises()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenCalledTimes(2)
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'common.menu', text: '第二 {{rules}}' }, expect.any(AbortSignal))
    expect(adminQQImageAPI.saveReplyTemplates).not.toHaveBeenCalled()
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled(); expect(adminQQImageAPI.connection).not.toHaveBeenCalled()
  })
  it('shows localized placeholders, inserts readable names and serializes only canonical variables', async () => {
    localizedCatalog(); const panel = await start(); const editor = panel.get('textarea')
    expect((editor.element as HTMLTextAreaElement).value).toBe('欢迎\n{{智商查询指令}}')
    expect(panel.get('[data-test="variable-iq_commands"]').attributes('disabled')).toBeUndefined()
    expect(panel.get('[data-test="variable-iq_commands"]').text()).toContain('智商查询指令')
    expect(panel.get('[data-test="variable-iq_commands"]').text()).not.toContain('iq_commands')
    ;(editor.element as HTMLTextAreaElement).setSelectionRange(0, 0)
    await panel.get('[data-test="variable-group_names"]').trigger('click')
    expect((editor.element as HTMLTextAreaElement).value).toContain('{{可用分组名称}}')
    await editor.setValue('你好\n{{智商查询指令}}\n{{可用分组名称}}')
    await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'common.menu', text: '你好\n{{iq_commands}}\n{{group_names}}' }, expect.any(AbortSignal))
    await panel.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
    expect(vi.mocked(adminQQImageAPI.saveReplyTemplates).mock.calls[0][0].overrides['common.menu']).toBe('你好\n{{iq_commands}}\n{{group_names}}')
  })
  it('automatically refreshes scene and restored defaults, including after a reload', async () => {
    const panel = await start(); await settlePreview()
    await panel.get('[data-test="template-select"]').setValue('image.help'); await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'image.help', text: '生图帮助默认文案' }, expect.any(AbortSignal))
    await panel.get('textarea').setValue('更改帮助'); await settlePreview()
    await panel.get('[data-test="restore-selected"]').trigger('click'); await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'image.help', text: '生图帮助默认文案' }, expect.any(AbortSignal))
    await panel.get('[data-test="reload-templates"]').trigger('click'); await flushPromises(); await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenCalledTimes(5)
  })
  it('retries a failed preview when restoring an already-default scene', async () => {
    vi.mocked(adminQQImageAPI.previewReplyTemplate).mockRejectedValueOnce(new Error('offline'))
    const panel = await start(); await settlePreview()
    expect(panel.text()).toContain('qqImage.admin.templates.previewFailed')
    await panel.get('[data-test="restore-selected"]').trigger('click'); await settlePreview()
    expect(panel.get('[data-test="template-preview"]').text()).toBe(preview.text)
  })
  it('keeps the latest automatic result when aborted requests resolve out of order', async () => {
    const old = deferred<typeof preview>(), recent = deferred<typeof preview>()
    vi.mocked(adminQQImageAPI.previewReplyTemplate).mockReturnValueOnce(old.promise).mockReturnValueOnce(recent.promise)
    const panel = await start(); await settlePreview()
    const signal = vi.mocked(adminQQImageAPI.previewReplyTemplate).mock.calls[0][1]
    await panel.get('textarea').setValue('最新 {{rules}}'); expect(signal?.aborted).toBe(true)
    await settlePreview(); recent.resolve({ ...preview, text: '最新效果' }); await flushPromises()
    old.resolve({ ...preview, text: '过期效果' }); await flushPromises()
    expect(panel.get('[data-test="template-preview"]').text()).toBe('最新效果')
  })
  it('does not preview incomplete IME input and refreshes once composition ends', async () => {
    const panel = await start(); await settlePreview(); const editor = panel.get('textarea')
    await editor.trigger('compositionstart')
    ;(editor.element as HTMLTextAreaElement).value = '拼音 zhong {{rules}}'
    await editor.trigger('input'); await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenCalledTimes(1)
    expect(panel.find('[data-test="template-preview"]').exists()).toBe(false)
    ;(editor.element as HTMLTextAreaElement).value = '中文 {{rules}}'
    await editor.trigger('compositionend'); await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'common.menu', text: '中文 {{rules}}' }, expect.any(AbortSignal))
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenCalledTimes(2)
  })
  it.each(['unmount', 'logout'])('cleans the scheduled preview on %s', async change => {
    const panel = await start()
    if (change === 'unmount') { panel.unmount(); wrapper = undefined } else auth.isAuthenticated = false
    await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).not.toHaveBeenCalled()
  })
  it('shows failed or invalid current previews with explicit retry and never substitutes old text', async () => {
    vi.mocked(adminQQImageAPI.previewReplyTemplate).mockRejectedValueOnce(new Error('PRIVATE'))
    const panel = await start(); await settlePreview()
    expect(panel.text()).toContain('qqImage.admin.templates.previewFailed')
    expect(panel.text()).not.toContain('PRIVATE'); expect(panel.find('[data-test="template-preview"]').exists()).toBe(false)
    await panel.get('[data-test="preview-template"]').trigger('click'); await flushPromises()
    expect(panel.get('[data-test="template-preview"]').text()).toBe(preview.text)
    vi.mocked(adminQQImageAPI.previewReplyTemplate).mockResolvedValueOnce({ ...preview, valid: false, text: '不得显示的默认效果', errors: ['malformed_placeholder'] })
    await panel.get('textarea').setValue('自动内容 {{rules'); await settlePreview()
    expect(panel.find('[data-test="template-preview"]').exists()).toBe(false)
    expect(panel.text()).not.toContain('不得显示的默认效果')
  })
  it('keeps optional readable content and drafts unchanged across language switches', async () => {
    localizedCatalog(); const panel = await start(); await panel.get('textarea').setValue('没有自动内容')
    expect(panel.text()).toContain('智商查询指令'); expect(panel.text()).not.toContain('iq_commands')
    await panel.get('[data-test="variable-iq_commands"]').trigger('click')
    const canonical = '没有自动内容{{iq_commands}}'
    locale.value = 'en'; await flushPromises(); await settlePreview()
    expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'common.menu', text: canonical }, expect.any(AbortSignal))
    locale.value = 'zh'; await flushPromises()
    expect((panel.get('textarea').element as HTMLTextAreaElement).value).toBe('没有自动内容{{智商查询指令}}')
  })
})


describe('QQ free reply editing', () => {
  it('lets every real catalog scenario replace all automatic content with the administrator’s own text', async () => {
    vi.mocked(adminQQImageAPI.replyTemplates).mockResolvedValue({ ...configuration, overrides: {}, catalog: structuredClone(serverCatalog) })
    const panel = await start(), overrides: Record<string, string> = {}
    expect(serverCatalog).toHaveLength(26)
    for (const item of serverCatalog) {
      expect(item.required_variables).toEqual([])
      await panel.get('[data-test="template-select"]').setValue(item.key)
      overrides[item.key] = `欢迎来到糖果小站（${item.key}），有问题请联系管理员。`
      await panel.get('textarea').setValue(overrides[item.key])
      expect(panel.get('[data-test="save-templates"]').attributes('disabled')).toBeUndefined()
      expect(panel.text()).not.toContain('qqImage.admin.templates.requiredVariableList')
    }
    await panel.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
    expect(adminQQImageAPI.saveReplyTemplates).toHaveBeenCalledWith({ expected_revision: 3, overrides }, expect.any(AbortSignal))
    expect(adminQQImageAPI.saveConfig).not.toHaveBeenCalled()
    expect(adminQQImageAPI.connection).not.toHaveBeenCalled()
  })
  it('explains that all automatic content is optional and ordinary text is freely editable', async () => {
    const panel = await start()
    expect(zh.qqImage.admin.templates.autoContentHint).toContain('自动内容均为可选，可全部删除，或直接写自己的文字')
    expect(en.qqImage.admin.templates.autoContentHint).toContain('All automatic content is optional')
    expect(zh.qqImage.admin.templates.editorHint).not.toContain('请保留完整')
    expect(panel.text()).not.toContain('qqImage.admin.templates.requiredContent')
    expect(panel.text()).not.toContain('qqImage.admin.templates.requiredVariableList')
  })
  it('previews and saves the actual IQ caption after deleting the time-zone label and parentheses', async () => {
    vi.useFakeTimers()
    try {
      const caption = serverCatalog.find(item => item.key === 'iq.caption')!
      vi.mocked(adminQQImageAPI.replyTemplates).mockResolvedValue({ ...configuration, overrides: {}, catalog: [caption] })
      const panel = await start(), editor = panel.get('textarea')
      const initial = (editor.element as HTMLTextAreaElement).value
      expect(initial).toContain('（{{时间所属时区}}）')
      await editor.setValue(initial.replace('（{{时间所属时区}}）', ''))
      await vi.advanceTimersByTimeAsync(350); await flushPromises()
      const text = caption.default_text.replace('（{{timezone}}）', '')
      expect(adminQQImageAPI.previewReplyTemplate).toHaveBeenLastCalledWith({ key: 'iq.caption', text }, expect.any(AbortSignal))
      expect(panel.get('[data-test="save-templates"]').attributes('disabled')).toBeUndefined()
      expect(panel.text()).not.toContain('qqImage.admin.templates.requiredVariableList')
      await panel.get('[data-test="save-templates"]').trigger('click'); await flushPromises()
      expect(vi.mocked(adminQQImageAPI.saveReplyTemplates).mock.calls[0][0].overrides['iq.caption']).toBe(text)
    } finally { vi.useRealTimers() }
  })
  it('allows inserting the same automatic content again at the caret and still enforces the expanded length limit', async () => {
    vi.mocked(adminQQImageAPI.replyTemplates).mockResolvedValue({ ...configuration, overrides: {}, catalog: [{
      ...catalog[0], default_text: '{{group_names}}', allowed_variables: ['group_names'], required_variables: [], variable_limits: { group_names: 680 }
    }] })
    const panel = await start(), editor = panel.get('textarea')
    ;(editor.element as HTMLTextAreaElement).setSelectionRange(0, 0)
    await panel.get('[data-test="variable-group_names"]').trigger('click')
    expect((editor.element as HTMLTextAreaElement).value).toBe('{{可用分组名称}}{{可用分组名称}}')
    expect(panel.get('[data-test="save-templates"]').attributes('disabled')).toBeUndefined()
    await panel.get('[data-test="variable-group_names"]').trigger('click')
    expect((editor.element as HTMLTextAreaElement).value).toBe('{{可用分组名称}}{{可用分组名称}}{{可用分组名称}}')
    expect(panel.get('[data-test="save-templates"]').attributes('disabled')).toBeDefined()
    expect(panel.text()).toContain('qqImage.admin.templates.tooLong')
    expect(adminQQImageAPI.saveReplyTemplates).not.toHaveBeenCalled()
  })
})
