import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import zh from '@/i18n/locales/zh/qqImage'
import en from '@/i18n/locales/en/qqImage'
import QQBindView from '../QQBindView.vue'
import QQOrdersView from '../QQOrdersView.vue'
import { qqImageAPI } from '@/api/qqImage'
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/layout/AuthLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
let testLocale = 'keys'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => {
  const messages = testLocale === 'zh' ? zh : testLocale === 'en' ? en : undefined
  const value = key.split('.').reduce<unknown>((current, part) => current && typeof current === 'object' ? (current as Record<string, unknown>)[part] : undefined, messages)
  return typeof value === 'string' ? value : key
}, locale: { value: 'en' } }) }))
vi.mock('@/api/qqImage', () => ({ qqImageAPI: { status: vi.fn(), request: vi.fn(), authorize: vi.fn(), sendEmailCode: vi.fn(), verifyEmail: vi.fn(), revoke: vi.fn(), orders: vi.fn(), order: vi.fn(), image: vi.fn(), cancel: vi.fn() } }))
const auth = reactive({ user: { id: 1, role: 'user', email: 'me@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
let wrapper: ReturnType<typeof mount>
const future = '2099-01-01T00:00:00Z'
const status = { enabled: true, email_verified: true, recent_authorization: true, email: 'me@example.test', grants: [{ id: 'g1', group_id: 'group-1', status: 'active', created_at: future }] }
async function start(path = '/qq/bind', orders = false, locale = 'keys') {
  const component = orders ? QQOrdersView : QQBindView
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/qq/bind', component }, { path: '/qq/orders/:id?', component }, { path: '/login', component: { template: '<div />' } }, { path: '/register', component: { template: '<div />' } }, { path: '/profile', component: { template: '<div />' } }] })
  await router.push(path); await router.isReady()
  testLocale = locale
  wrapper = mount(component, { global: { plugins: [router] } }); await flushPromises()
  return router
}
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear(); sessionStorage.clear()
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  auth.user = { id: 1, role: 'user', email: 'me@example.test' }; auth.sessionRevision = 'A'; auth.isAuthenticated = true
  localStorage.setItem('auth_user', JSON.stringify(auth.user)); localStorage.setItem('auth_session_id', 'A')
  vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, grants: [...status.grants] })
  vi.mocked(qqImageAPI.request).mockResolvedValue({ id: 'req1', group_id: 'group-1', subject_hint: 'member…123', status: 'pending', expires_at: future })
  vi.mocked(qqImageAPI.authorize).mockResolvedValue({ confirmation_code: 'ONLY-ONCE', expires_at: future })
  vi.mocked(qqImageAPI.orders).mockResolvedValue([])
  vi.mocked(qqImageAPI.sendEmailCode).mockResolvedValue(undefined)
  vi.mocked(qqImageAPI.verifyEmail).mockResolvedValue(undefined)
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:private-image'), revokeObjectURL: vi.fn() }))
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.unstubAllGlobals() })
describe('QQ binding', () => {
  it('keeps a fixed login/register return without consuming or authorizing a request', async () => {
    auth.isAuthenticated = false
    const router = await start('/qq/bind?request=req1')
    expect(router.currentRoute.value.query).toEqual({})
    expect(wrapper.get('[data-test="login"]').attributes('href')).toBe('/login?redirect=/qq/bind')
    expect(wrapper.get('[data-test="register"]').attributes('href')).toBe('/register?redirect=/qq/bind')
    expect(qqImageAPI.status).not.toHaveBeenCalled(); expect(qqImageAPI.authorize).not.toHaveBeenCalled()
    wrapper.unmount()
    expect(sessionStorage.getItem('qq-image.binding-request')).toContain('req1')
  })
  it('requires unchecked explicit consent, shows the one-time code only after POST and clears it on a new login', async () => {
    await start('/qq/bind?request=req1')
    expect(qqImageAPI.authorize).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="authorize"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="consent"]').setValue(true)
    await wrapper.get('[data-test="authorize"]').trigger('click'); await flushPromises()
    expect(qqImageAPI.authorize).toHaveBeenCalledOnce(); expect(wrapper.text()).toContain('ONLY-ONCE')
    expect(sessionStorage.getItem('qq-image.binding-request')).not.toContain('ONLY-ONCE')
    auth.sessionRevision = 'B'; localStorage.setItem('auth_session_id', 'B'); await flushPromises()
    expect(wrapper.text()).not.toContain('ONLY-ONCE'); expect(wrapper.text()).not.toContain('me@example.test')
    expect(wrapper.text()).toContain('qqImage.sessionChanged')
  })
  it('requires current-email proof for already verified users without recent authorization', async () => {
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, recent_authorization: false })
    await start('/qq/bind?request=req1')
    expect(wrapper.find('[data-test="send-code"]').exists()).toBe(true)
    await wrapper.get('[data-test="send-code"]').trigger('click'); await flushPromises()
    expect(qqImageAPI.sendEmailCode).toHaveBeenCalledOnce()
    await wrapper.get('[data-test="email-code"]').setValue('123456')
    await wrapper.get('[data-test="verify-email"]').element.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await flushPromises()
    expect(qqImageAPI.verifyEmail).toHaveBeenCalledWith('123456', expect.any(AbortSignal))
  })
  it('never shows sent success when the email transport fails', async () => {
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, email_verified: false })
    vi.mocked(qqImageAPI.sendEmailCode).mockRejectedValue({ status: 503 })
    await start(); await wrapper.get('[data-test="send-code"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.errors.failed'); expect(wrapper.text()).not.toContain('qqImage.emailSent')
  })
  describe.each(['zh', 'en'] as const)('%s email guidance', locale => {
    const messages = {
      zh: {
        EMAIL_NOT_CONFIGURED: '邮件服务尚未配置，请联系管理员配置 SMTP 后再发送验证码。',
        EMAIL_RESERVED: '当前邮箱无法接收验证码，请点击“管理我的邮箱”更换为真实邮箱。',
        INVALID_VERIFY_CODE: '验证码无效或已过期，请重新获取验证码。',
        VERIFY_CODE_MAX_ATTEMPTS: '验证码尝试次数过多，请重新获取验证码。',
        VERIFY_CODE_TOO_FREQUENT: '操作过于频繁，请稍后重试。',
        EMAIL_VERIFY_REQUIRED: '请先完成当前邮箱验证。'
      },
      en: {
        EMAIL_NOT_CONFIGURED: 'Email service is not configured. Contact an administrator to configure SMTP before requesting a code.',
        EMAIL_RESERVED: 'Your current email cannot receive verification codes. Use “Manage my email” to switch to a real email address.',
        INVALID_VERIFY_CODE: 'The verification code is invalid or expired. Request a new code.',
        VERIFY_CODE_MAX_ATTEMPTS: 'Too many failed verification attempts. Request a new code.',
        VERIFY_CODE_TOO_FREQUENT: 'Too many requests. Wait before trying again.',
        EMAIL_VERIFY_REQUIRED: 'Complete current-email verification first.'
      }
    }
    it.each([
      ['EMAIL_NOT_CONFIGURED', 503],
      ['EMAIL_RESERVED', 400],
      ['VERIFY_CODE_TOO_FREQUENT', 429]
    ] as const)('explains %s after requesting a code without reporting success', async (reason, statusCode) => {
      vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, email_verified: false })
      vi.mocked(qqImageAPI.sendEmailCode).mockRejectedValue({ status: statusCode, reason, message: 'private SMTP detail' })
      await start('/qq/bind', false, locale)
      await wrapper.get('[data-test="send-code"]').trigger('click'); await flushPromises()
      expect(wrapper.get('[role="alert"]').text()).toContain(messages[locale][reason])
      expect(wrapper.find('[role="status"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('private SMTP detail')
      expect(wrapper.get('a[href="/profile"]').text()).toBe(locale === 'zh' ? '管理我的邮箱' : 'Manage my email')
    })
    it.each([
      ['INVALID_VERIFY_CODE', 400],
      ['VERIFY_CODE_MAX_ATTEMPTS', 429],
      ['EMAIL_VERIFY_REQUIRED', 400]
    ] as const)('explains %s after verification without reporting success', async (reason, statusCode) => {
      vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, recent_authorization: false })
      vi.mocked(qqImageAPI.verifyEmail).mockRejectedValue({ status: statusCode, reason, message: 'private verification detail' })
      await start('/qq/bind', false, locale)
      await wrapper.get('[data-test="email-code"]').setValue('123456')
      await wrapper.get('form').trigger('submit'); await flushPromises()
      expect(wrapper.get('[role="alert"]').text()).toContain(messages[locale][reason])
      expect(wrapper.find('[role="status"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('private verification detail')
      expect(wrapper.get('a[href="/profile"]').exists()).toBe(true)
    })
  })
  it('discards a late email error after session replacement', async () => {
    let reject!: (reason: unknown) => void
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, email_verified: false })
    vi.mocked(qqImageAPI.sendEmailCode).mockReturnValue(new Promise((_, rejectPromise) => { reject = rejectPromise }))
    await start(); await wrapper.get('[data-test="send-code"]').trigger('click')
    auth.sessionRevision = 'B'; localStorage.setItem('auth_session_id', 'B'); await flushPromises()
    reject({ status: 503, reason: 'EMAIL_NOT_CONFIGURED', message: 'private SMTP detail' }); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.sessionChanged')
    expect(wrapper.text()).not.toContain('qqImage.errors.emailNotConfigured')
    expect(wrapper.text()).not.toContain('private SMTP detail')
    expect(wrapper.text()).not.toContain('qqImage.emailSent')
  })
  it.each(['expired', 'conflict', 'await_qq'])('does not reissue a code for %s requests', async state => {
    vi.mocked(qqImageAPI.request).mockResolvedValue({ id: 'req1', group_id: 'group-1', subject_hint: 'member', status: state, expires_at: future })
    await start('/qq/bind?request=req1')
    expect(wrapper.find('[data-test="authorize"]').exists()).toBe(false)
    expect(qqImageAPI.authorize).not.toHaveBeenCalled()
  })
  it('keeps historical grant management while new requests are disabled', async () => {
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, enabled: false })
    await start(); expect(wrapper.find('[data-test="revoke-g1"]').exists()).toBe(true)
    await wrapper.get('[data-test="revoke-g1"]').trigger('click')
    expect(qqImageAPI.revoke).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-revoke"]').trigger('click'); await flushPromises()
    expect(qqImageAPI.revoke).toHaveBeenCalledWith('g1', expect.any(AbortSignal))
  })
  it('discards a late authorization response after session replacement', async () => {
    let finish!: (value: { confirmation_code: string; expires_at: string }) => void
    vi.mocked(qqImageAPI.authorize).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await start('/qq/bind?request=req1'); await wrapper.get('[data-test="consent"]').setValue(true)
    await wrapper.get('[data-test="authorize"]').trigger('click')
    auth.sessionRevision = 'B'; localStorage.setItem('auth_session_id', 'B'); await flushPromises()
    finish({ confirmation_code: 'STALE-SECRET', expires_at: future }); await flushPromises()
    expect(wrapper.text()).not.toContain('STALE-SECRET')
  })
})
describe('QQ orders', () => {
  it('shows an empty history with the feature disabled', async () => {
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, enabled: false, image_enabled: false })
    await start('/qq/orders', true)
    expect(wrapper.text()).toContain('qqImage.emptyOrders')
    expect(wrapper.text()).toContain('qqGallery.paused')
  })
  it('separates task, funds and delivery state and revokes private blobs on session change', async () => {
    vi.mocked(qqImageAPI.order).mockResolvedValue({ id: 'o1', prompt: 'private prompt', price: '0.120000', model: 'image-1', size: '1024x1024', status: 'completed', funds_status: 'captured', delivery_status: 'failed', created_at: future, deadline: future, artifact_expires_at: future })
    vi.mocked(qqImageAPI.image).mockResolvedValue(new Blob(['fixture'], { type: 'image/png' }))
    await start('/qq/orders/o1', true)
    expect(wrapper.text()).toContain('0.120000'); expect(wrapper.text()).toContain('qqImage.deliveryStates.failed')
    await flushPromises()
    expect(wrapper.get('img').attributes('src')).toBe('blob:private-image')
    auth.sessionRevision = 'B'; localStorage.setItem('auth_session_id', 'B'); await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false); expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:private-image')
    expect(wrapper.text()).not.toContain('private prompt')
  })
  it('presents a gallery with search and availability filters limited to the loaded history', async () => {
    vi.mocked(qqImageAPI.orders).mockResolvedValue([
      { id: 'late', prompt: 'sunlit forest', price: '0.04', model: 'image-1', size: '1024x1024', status: 'closed_unknown', funds_status: 'released', delivery_status: 'site_only', website_status: 'available', created_at: future, deadline: future, artifact_expires_at: future },
      { id: 'pending', prompt: 'ocean study', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }
    ])
    await start('/qq/orders', true)
    expect(wrapper.text()).toContain('qqGallery.scope')
    expect(wrapper.findAll('[data-test="gallery-card"]')).toHaveLength(2)
    expect(qqImageAPI.image).not.toHaveBeenCalled()
    await wrapper.get('[data-test="gallery-filter"]').setValue('available')
    expect(wrapper.findAll('[data-test="gallery-card"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('sunlit forest')
    await wrapper.get('[data-test="gallery-search"]').setValue('ocean')
    expect(wrapper.findAll('[data-test="gallery-card"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('qqGallery.noMatches')
  })
  it('opens a linked side detail for a late released image and downloads the authenticated blob', async () => {
    const item = { id: 'late', prompt: 'late image', price: '0.04', model: 'image-1', size: '1024x1024', status: 'closed_unknown', funds_status: 'released', delivery_status: 'site_only', website_status: 'available' as const, created_at: future, deadline: future, artifact_expires_at: future }
    vi.mocked(qqImageAPI.orders).mockResolvedValue([item]); vi.mocked(qqImageAPI.order).mockResolvedValue(item)
    vi.mocked(qqImageAPI.image).mockResolvedValue(new Blob(['fixture'], { type: 'image/png' }))
    const router = await start('/qq/orders', true)
    await wrapper.get('[data-test="open-order"]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.path).toBe('/qq/orders/late')
    expect(wrapper.get('[role="dialog"]').text()).toContain('qqImage.fundsStates.released')
    expect(wrapper.get('[role="dialog"]').text()).toContain('qqGallery.freeResult')
    expect(wrapper.get('[data-test="download-image"]').attributes('href')).toBe('blob:private-image')
    expect(wrapper.get('[data-test="download-image"]').attributes('download')).toContain('late')
    await wrapper.get('[data-test="close-detail"]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.path).toBe('/qq/orders')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
  it('keeps queued cancellation behind explicit confirmation and refreshes the result', async () => {
    const item = { id: 'o1', prompt: 'queue', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }
    vi.mocked(qqImageAPI.order).mockResolvedValue(item)
    await start('/qq/orders/o1', true)
    await wrapper.get('[data-test="cancel-order"]').trigger('click')
    expect(qqImageAPI.cancel).not.toHaveBeenCalled()
    vi.mocked(qqImageAPI.order).mockResolvedValue({ ...item, status: 'cancelled', funds_status: 'released' })
    await wrapper.get('[data-test="confirm-cancel"]').trigger('click'); await flushPromises()
    expect(qqImageAPI.cancel).toHaveBeenCalledWith('o1', expect.any(AbortSignal))
    expect(wrapper.text()).toContain('qqImage.fundsStates.released')
  })
  it('polls only active work and pauses while the tab is hidden', async () => {
    vi.useFakeTimers()
    const item = { id: 'o1', prompt: 'queue', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }
    vi.mocked(qqImageAPI.orders).mockResolvedValue([item])
    await start('/qq/orders', true)
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect(qqImageAPI.orders).toHaveBeenCalledTimes(2)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(15000)
    expect(qqImageAPI.orders).toHaveBeenCalledTimes(2)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    vi.mocked(qqImageAPI.orders).mockResolvedValue([{ ...item, status: 'cancelled', funds_status: 'released' }])
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    const count = vi.mocked(qqImageAPI.orders).mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(qqImageAPI.orders).toHaveBeenCalledTimes(count)
  })
  it('removes an expired private image and download without a refresh', async () => {
    vi.useFakeTimers(); vi.setSystemTime('2026-10-26T00:00:00Z')
    vi.mocked(qqImageAPI.order).mockResolvedValue({ id: 'o1', prompt: 'expires', price: '0.04', model: 'image-1', size: '1024x1024', status: 'succeeded', funds_status: 'captured', delivery_status: 'sent', website_status: 'available', created_at: future, deadline: future, artifact_expires_at: '2026-10-26T00:00:01Z' })
    vi.mocked(qqImageAPI.image).mockResolvedValue(new Blob(['fixture'], { type: 'image/png' }))
    await start('/qq/orders/o1', true)
    expect(wrapper.find('img').exists()).toBe(true)
    expect(wrapper.find('[data-test="copy-resend"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(1500)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[data-test="download-image"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="copy-resend"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('qqImage.imageExpired')
  })
  it('keeps a cancel response usable after the tab is hidden', async () => {
    let finish!: () => void
    const item = { id: 'o1', prompt: 'queue', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }
    vi.mocked(qqImageAPI.order).mockResolvedValue(item)
    vi.mocked(qqImageAPI.cancel).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await start('/qq/orders/o1', true)
    await wrapper.get('[data-test="cancel-order"]').trigger('click')
    await wrapper.get('[data-test="confirm-cancel"]').trigger('click')
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    vi.mocked(qqImageAPI.order).mockResolvedValue({ ...item, status: 'cancelled', funds_status: 'released' })
    finish(); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.fundsStates.released')
    expect(wrapper.get('[data-test="close-detail"]').attributes('disabled')).toBeUndefined()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  })
  it('keeps cancellation errors visible inside the open detail with an explicit refresh', async () => {
    vi.mocked(qqImageAPI.order).mockResolvedValue({ id: 'o1', prompt: 'queue', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future })
    vi.mocked(qqImageAPI.cancel).mockRejectedValue({ status: 503 })
    await start('/qq/orders/o1', true)
    await wrapper.get('[data-test="cancel-order"]').trigger('click'); await wrapper.get('[data-test="confirm-cancel"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('qqImage.errors.failed')
    expect(wrapper.get('[data-test="refresh-detail"]').exists()).toBe(true)
  })
  it('clears private data and stops polling after an authentication failure', async () => {
    vi.useFakeTimers()
    vi.mocked(qqImageAPI.orders).mockResolvedValueOnce([{ id: 'o1', prompt: 'private queue', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }]).mockRejectedValueOnce({ status: 401 })
    await start('/qq/orders', true)
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.sessionChanged')
    expect(wrapper.text()).not.toContain('private queue')
    await vi.advanceTimersByTimeAsync(15000)
    expect(qqImageAPI.orders).toHaveBeenCalledTimes(2)
  })
  it('keeps private history viewable while new image generation is paused', async () => {
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, enabled: true, image_enabled: false })
    vi.mocked(qqImageAPI.order).mockResolvedValue({ id: 'old', prompt: 'past image', price: '0.04', model: 'image-1', size: '1024x1024', status: 'succeeded', funds_status: 'captured', delivery_status: 'sent', website_status: 'available', created_at: future, deadline: future, artifact_expires_at: future })
    vi.mocked(qqImageAPI.image).mockResolvedValue(new Blob(['fixture'], { type: 'image/png' }))
    await start('/qq/orders/old', true)
    expect(wrapper.text()).toContain('qqGallery.paused')
    expect(wrapper.get('[data-test="download-image"]').attributes('href')).toBe('blob:private-image')
  })
  it('discards an old detail GET after route navigation', async () => {
    let finish!: (value: Awaited<ReturnType<typeof qqImageAPI.order>>) => void
    const item = { id: 'o2', prompt: 'current prompt', price: '0.04', model: 'image-1', size: '1024x1024', status: 'cancelled', funds_status: 'released', delivery_status: 'none', created_at: future, deadline: future }
    vi.mocked(qqImageAPI.order).mockReturnValueOnce(new Promise(resolve => { finish = resolve })).mockResolvedValue(item)
    const router = await start('/qq/orders/o1', true)
    await router.push('/qq/orders/o2'); await flushPromises()
    expect(vi.mocked(qqImageAPI.order).mock.calls[0][1]?.aborted).toBe(true)
    finish({ ...item, id: 'o1', prompt: 'stale prompt' }); await flushPromises()
    expect(wrapper.text()).toContain('current prompt'); expect(wrapper.text()).not.toContain('stale prompt')
  })
  it('requests gallery previews only after intersection and releases them on exit', async () => {
    const intersections: Array<(entries: Array<{ isIntersecting: boolean }>) => void> = []
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: (entries: Array<{ isIntersecting: boolean }>) => void) { intersections.push(callback) }
      observe() {} disconnect() {}
    })
    vi.mocked(qqImageAPI.orders).mockResolvedValue([{ id: 'o1', prompt: 'private preview', price: '0.04', model: 'image-1', size: '1024x1024', status: 'succeeded', funds_status: 'captured', delivery_status: 'sent', website_status: 'available', created_at: future, deadline: future, artifact_expires_at: future }])
    vi.mocked(qqImageAPI.image).mockResolvedValue(new Blob(['fixture'], { type: 'image/png' }))
    await start('/qq/orders', true)
    expect(qqImageAPI.image).not.toHaveBeenCalled()
    intersections[0]([{ isIntersecting: true }]); await flushPromises()
    expect(qqImageAPI.image).toHaveBeenCalledOnce(); expect(wrapper.find('img').exists()).toBe(true)
    intersections[0]([{ isIntersecting: false }]); await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:private-image')
  })
  it('copies the prompt, order ID and original-group command for a captured available image without sending a request', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    vi.mocked(qqImageAPI.order).mockResolvedValue({ id: 'copy-me', prompt: 'a private forest', price: '0.04', model: 'image-1', size: '1024x1024', status: 'succeeded', funds_status: 'captured', delivery_status: 'sent', website_status: 'available', artifact_expires_at: future, created_at: future, deadline: future })
    vi.mocked(qqImageAPI.image).mockResolvedValue(new Blob(['fixture'], { type: 'image/png' }))
    await start('/qq/orders/copy-me', true)
    const imageCalls = vi.mocked(qqImageAPI.image).mock.calls.length
    for (const label of ['qqGallery.copyPrompt', 'qqGallery.copyOrder', 'qqGallery.copyResend']) await wrapper.findAll('button').find(button => button.text() === label)!.trigger('click')
    expect(writeText.mock.calls.map(call => call[0])).toEqual(['a private forest', 'copy-me', '/重发 copy-me'])
    expect(qqImageAPI.cancel).not.toHaveBeenCalled(); expect(qqImageAPI.image).toHaveBeenCalledTimes(imageCalls)
  })
  it.each(['released', 'refunded'])('keeps a %s late image downloadable without advertising QQ resend', async fundsStatus => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    vi.mocked(qqImageAPI.order).mockResolvedValue({ id: 'late', prompt: 'a late forest', price: '0.04', model: 'image-1', size: '1024x1024', status: 'closed_unknown', funds_status: fundsStatus, delivery_status: 'site_only', website_status: 'available', artifact_expires_at: future, created_at: future, deadline: future })
    vi.mocked(qqImageAPI.image).mockResolvedValue(new Blob(['fixture'], { type: 'image/png' }))
    await start('/qq/orders/late', true)
    expect(wrapper.get('[data-test="download-image"]').attributes('href')).toBe('blob:private-image')
    expect(wrapper.text()).toContain('qqGallery.resendSiteOnly')
    expect(wrapper.text()).not.toContain('/重发 late')
    expect(wrapper.findAll('button').some(button => button.text() === 'qqGallery.copyResend')).toBe(false)
    await wrapper.findAll('button').find(button => button.text() === 'qqGallery.copyOrder')!.trigger('click')
    expect(writeText).toHaveBeenCalledWith('late')
  })
  it.each(['expired', 'pending'] as const)('does not advertise QQ resend for a captured %s artifact', async websiteStatus => {
    vi.mocked(qqImageAPI.order).mockResolvedValue({ id: 'not-ready', prompt: 'a forest', price: '0.04', model: 'image-1', size: '1024x1024', status: 'succeeded', funds_status: 'captured', delivery_status: 'sent', website_status: websiteStatus, artifact_expires_at: future, created_at: future, deadline: future })
    await start('/qq/orders/not-ready', true)
    expect(wrapper.text()).toContain('qqGallery.resendUnavailable')
    expect(wrapper.text()).not.toContain('/重发 not-ready')
    expect(wrapper.findAll('button').some(button => button.text() === 'qqGallery.copyResend')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === 'qqGallery.copyOrder')).toBe(true)
    expect(qqImageAPI.image).not.toHaveBeenCalled()
  })
  it('bounds automatic polling even if an order remains active', async () => {
    vi.useFakeTimers()
    vi.mocked(qqImageAPI.orders).mockResolvedValue([{ id: 'o1', prompt: 'still queued', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }])
    await start('/qq/orders', true)
    await vi.advanceTimersByTimeAsync(310000); await flushPromises()
    expect(qqImageAPI.orders).toHaveBeenCalledTimes(61)
    await vi.advanceTimersByTimeAsync(10000)
    expect(qqImageAPI.orders).toHaveBeenCalledTimes(61)
  })
  it.each(['list', 'detail'] as const)('resumes an interrupted initial %s GET on return to the visible tab', async kind => {
    const item = { id: 'o1', prompt: 'resumed history', price: '0.04', model: 'image-1', size: '1024x1024', status: 'cancelled', funds_status: 'released', delivery_status: 'none', created_at: future, deadline: future }
    if (kind === 'list') vi.mocked(qqImageAPI.orders).mockReturnValueOnce(new Promise(() => {})).mockResolvedValue([item])
    else vi.mocked(qqImageAPI.order).mockReturnValueOnce(new Promise(() => {})).mockResolvedValue(item)
    await start(kind === 'list' ? '/qq/orders' : '/qq/orders/o1', true)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(wrapper.text()).not.toContain('qqImage.emptyOrders')
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(kind === 'list' ? qqImageAPI.orders : qqImageAPI.order).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('resumed history')
  })
  it('finishes cancellation while a polling GET is pending and the tab becomes hidden', async () => {
    vi.useFakeTimers()
    let finishCancel!: () => void, finishPoll!: (value: Awaited<ReturnType<typeof qqImageAPI.order>>) => void
    const item = { id: 'o1', prompt: 'queue', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }
    vi.mocked(qqImageAPI.order).mockResolvedValueOnce(item).mockReturnValueOnce(new Promise(resolve => { finishPoll = resolve })).mockResolvedValue({ ...item, status: 'cancelled', funds_status: 'released' })
    vi.mocked(qqImageAPI.cancel).mockReturnValue(new Promise(resolve => { finishCancel = resolve }))
    await start('/qq/orders/o1', true)
    await wrapper.get('[data-test="cancel-order"]').trigger('click')
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect(qqImageAPI.order).toHaveBeenCalledTimes(2)
    await wrapper.get('[data-test="confirm-cancel"]').trigger('click')
    expect(qqImageAPI.cancel).toHaveBeenCalledOnce()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    finishCancel(); await flushPromises()
    expect(wrapper.get('[data-test="close-detail"]').attributes('disabled')).toBeUndefined()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.fundsStates.released')
    finishPoll(item); await flushPromises()
    expect(wrapper.text()).not.toContain('qqImage.fundsStates.held')
  })
  it.each(['route', 'session'] as const)('discards a late cancellation completion after %s replacement', async replacement => {
    let finish!: () => void
    const item = { id: 'o1', prompt: 'private cancelled request', price: '0.04', model: 'image-1', size: '1024x1024', status: 'queued', funds_status: 'held', delivery_status: 'pending', created_at: future, deadline: future }
    vi.mocked(qqImageAPI.order).mockResolvedValueOnce(item).mockResolvedValue({ ...item, id: 'o2', prompt: 'new page', status: 'cancelled', funds_status: 'released' })
    vi.mocked(qqImageAPI.cancel).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const router = await start('/qq/orders/o1', true)
    await wrapper.get('[data-test="cancel-order"]').trigger('click'); await wrapper.get('[data-test="confirm-cancel"]').trigger('click')
    if (replacement === 'route') await router.push('/qq/orders/o2')
    else { auth.sessionRevision = 'B'; localStorage.setItem('auth_session_id', 'B') }
    await flushPromises()
    const calls = vi.mocked(qqImageAPI.order).mock.calls.length
    finish(); await flushPromises()
    expect(qqImageAPI.order).toHaveBeenCalledTimes(calls)
    expect(wrapper.text()).not.toContain('private cancelled request')
    expect(wrapper.text()).toContain(replacement === 'route' ? 'new page' : 'qqImage.sessionChanged')
  })
})
