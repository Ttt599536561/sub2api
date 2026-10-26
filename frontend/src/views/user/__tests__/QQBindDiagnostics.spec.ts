import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import QQBindView from '../QQBindView.vue'
import { qqImageAPI } from '@/api/qqImage'
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/layout/AuthLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => key + (params?.site ? ` ${params.site}` : ''), locale: { value: 'zh' } }) }))
vi.mock('@/api/qqImage', () => ({ qqImageAPI: { status: vi.fn(), request: vi.fn(), authorize: vi.fn(), sendEmailCode: vi.fn(), verifyEmail: vi.fn(), revoke: vi.fn() } }))
const auth = reactive({ user: { id: 1, role: 'user', email: 'owner@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const future = '2099-01-01T00:00:00Z'
const status = { enabled: true, email_verified: true, recent_authorization: true, email: 'owner@example.test', grants: [{ id: 'g1', group_id: 'group-1', status: 'active', created_at: future }] }
let wrapper: ReturnType<typeof mount>
async function start() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/qq/bind', component: QQBindView }, { path: '/qq/orders', component: { template: '<div />' } }] })
  await router.push('/qq/bind?request=req1'); await router.isReady()
  wrapper = mount(QQBindView, { global: { plugins: [router] } }); await flushPromises()
}
beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear(); sessionStorage.clear()
  localStorage.setItem('auth_user', JSON.stringify(auth.user)); localStorage.setItem('auth_session_id', 'A')
  vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, grants: [...status.grants] })
  vi.mocked(qqImageAPI.request).mockResolvedValue({ id: 'req1', group_id: 'group-1', subject_hint: '***123', status: 'await_website', expires_at: future })
  vi.mocked(qqImageAPI.revoke).mockResolvedValue(undefined)
})
afterEach(() => wrapper?.unmount())
describe('QQ binding diagnostics', () => {
  it('shows the current site and actionable missing-request guidance without authorizing', async () => {
    vi.mocked(qqImageAPI.request).mockRejectedValue({ status: 404, reason: 'QQ_IMAGE_NOT_FOUND' })
    await start()
    expect(wrapper.text()).toContain('qqImage.errors.requestNotFound')
    expect(wrapper.get('[data-test="request-site"]').text()).toContain(window.location.host)
    expect(qqImageAPI.authorize).not.toHaveBeenCalled()
  })
  it('honors the dedicated binding pause while preserving revoke', async () => {
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, binding_enabled: false, image_enabled: true })
    await start()
    expect(wrapper.text()).toContain('qqImage.bindingPaused')
    expect(wrapper.find('[data-test="authorize"]').exists()).toBe(false)
    await wrapper.get('[data-test="revoke-g1"]').trigger('click')
    await wrapper.get('[data-test="confirm-revoke"]').trigger('click'); await flushPromises()
    expect(qqImageAPI.revoke).toHaveBeenCalledOnce()
    expect(qqImageAPI.authorize).not.toHaveBeenCalled()
  })
  it('clears a stale request after a definitive missing-request refresh', async () => {
    await start()
    expect(wrapper.find('[data-test="authorize"]').exists()).toBe(true)
    vi.mocked(qqImageAPI.request).mockRejectedValue({ status: 404, reason: 'QQ_IMAGE_NOT_FOUND' })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.find('[data-test="authorize"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('qqImage.errors.requestNotFound')
  })
  it('removes an outstanding confirmation code after a refresh reports binding paused', async () => {
    await start()
    vi.mocked(qqImageAPI.authorize).mockResolvedValue({ confirmation_code: 'LOCAL-CODE', expires_at: future })
    await wrapper.get('[data-test="consent"]').setValue(true)
    await wrapper.get('[data-test="authorize"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('LOCAL-CODE')
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, binding_enabled: false })
    vi.mocked(qqImageAPI.request).mockResolvedValue({ id: 'req1', group_id: 'group-1', subject_hint: '***123', status: 'await_qq', expires_at: future })
    await wrapper.findAll('button').find(button => button.text() === 'qqImage.refreshBinding')!.trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('LOCAL-CODE')
    expect(wrapper.text()).toContain('qqImage.bindingPaused')
    expect(wrapper.text()).not.toContain('qqImage.restartHint')
    expect(wrapper.find('[data-test="authorize"]').exists()).toBe(false)
  })
  it('also removes the confirmation when email verification discovers binding was paused', async () => {
    await start()
    vi.mocked(qqImageAPI.authorize).mockResolvedValue({ confirmation_code: 'EMAIL-PAUSE-CODE', expires_at: future })
    await wrapper.get('[data-test="consent"]').setValue(true)
    await wrapper.get('[data-test="authorize"]').trigger('click'); await flushPromises()
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, recent_authorization: false })
    vi.mocked(qqImageAPI.request).mockResolvedValue({ id: 'req1', group_id: 'group-1', subject_hint: '***123', status: 'await_qq', expires_at: future })
    await wrapper.findAll('button').find(button => button.text() === 'qqImage.refreshBinding')!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('EMAIL-PAUSE-CODE')
    vi.mocked(qqImageAPI.verifyEmail).mockResolvedValue(undefined)
    vi.mocked(qqImageAPI.status).mockResolvedValue({ ...status, binding_enabled: false })
    await wrapper.get('[data-test="email-code"]').setValue('123456')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(qqImageAPI.verifyEmail).toHaveBeenCalledOnce()
    expect(wrapper.text()).not.toContain('EMAIL-PAUSE-CODE')
    expect(wrapper.text()).toContain('qqImage.bindingPaused')
    expect(wrapper.text()).not.toContain('qqImage.restartHint')
    expect(wrapper.get('[data-test="revoke-g1"]').attributes('disabled')).toBeUndefined()
  })
  it('retains the uncertain result warning for a failed authorization response', async () => {
    await start()
    vi.mocked(qqImageAPI.authorize).mockRejectedValue({ status: 500 })
    await wrapper.get('[data-test="consent"]').setValue(true)
    await wrapper.get('[data-test="authorize"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('qqImage.errors.failed')
    expect(qqImageAPI.authorize).toHaveBeenCalledOnce()
  })
})
