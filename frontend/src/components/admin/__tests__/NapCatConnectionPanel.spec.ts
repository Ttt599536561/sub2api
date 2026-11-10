import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import NapCatConnectionPanel from '../napcat/NapCatConnectionPanel.vue'
import { qqInviteAPI } from '@/api/qqInvite'
import QRCode from 'qrcode'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/qqInvite', () => ({ qqInviteAPI: { config: vi.fn(), saveConfig: vi.fn(), connection: vi.fn(), login: vi.fn() } }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn() } }))
const auth = reactive({ user: { id: 1, role: 'admin', email: 'admin@example.test' }, sessionRevision: 'A', isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
const config = { version: 3, enabled: false, onebot_url: 'http://napcat:3000', webui_url: 'http://napcat:6099', bot_qq: '123456', public_base_url: 'https://site.example.test', onebot_token_configured: true, webui_token_configured: true, event_secret_configured: true, encryption_available: true }
let wrapper: ReturnType<typeof mount>
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear()
  auth.user = { id: 1, role: 'admin', email: 'admin@example.test' }; auth.sessionRevision = 'A'; auth.isAuthenticated = true
  localStorage.setItem('auth_user', JSON.stringify(auth.user)); localStorage.setItem('auth_session_id', 'A')
  vi.mocked(qqInviteAPI.config).mockResolvedValue({ ...config }); vi.mocked(qqInviteAPI.saveConfig).mockResolvedValue({ ...config, version: 4 })
  vi.mocked(qqInviteAPI.connection).mockResolvedValue({ online: false, core_ready: true })
  vi.mocked(qqInviteAPI.login).mockResolvedValue({ url: 'https://qq.example.test/login?secret=QR', restarting: false })
  vi.mocked(QRCode.toDataURL).mockResolvedValue('data:image/png;base64,LOCAL')
})
afterEach(() => wrapper?.unmount())
async function start() { wrapper = mount(NapCatConnectionPanel); await flushPromises() }
describe('NapCat safe login controls', () => {
  it('does not call process readiness QQ online or request QR automatically', async () => {
    await start(); expect(wrapper.get('[data-test="connection-state"]').text()).toContain('qqInvite.states.offline')
    expect(qqInviteAPI.login).not.toHaveBeenCalled()
  })
  it('shows an accepted QR as login progress while QQ is still offline', async () => {
    vi.mocked(qqInviteAPI.connection).mockResolvedValue({ online: false, core_ready: false, qr_login_accepted: true, login_phase: 'qrcode_scanned' })
    await start()
    expect(wrapper.get('[data-test="connection-state"]').text()).toContain('qqInvite.states.waiting_confirmation')
    expect(wrapper.text()).toContain('qqInvite.loginPhases.qrcode_scanned')
    expect(wrapper.text()).toContain('qqInvite.qrAccepted')
  })
  it('keeps three secrets blank and preserves optimistic configuration version', async () => {
    await start()
    for (const name of ['onebot_token', 'webui_token', 'event_secret']) expect((wrapper.get(`[name="${name}"]`).element as HTMLInputElement).value).toBe('')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const body = vi.mocked(qqInviteAPI.saveConfig).mock.calls[0][0]
    expect(body.version).toBe(3); expect(body).not.toHaveProperty('webui_token'); expect(body).not.toHaveProperty('encryption_available')
  })
  it('shows a specific credential error and clears the previous connection status', async () => {
    await start()
    vi.mocked(qqInviteAPI.connection).mockRejectedValue({ status: 502, code: 'NAPCAT_UNAUTHORIZED', message: 'PRIVATE TOKEN' })
    await wrapper.get('[data-test="connection-refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('qqInvite.connectionErrors.unauthorized')
    expect(wrapper.text()).not.toContain('PRIVATE TOKEN')
    expect(wrapper.get('[data-test="connection-state"]').text()).toContain('qqInvite.states.unknown')
  })
  it('renders a QR locally without exposing the login URL in DOM or storage', async () => {
    await start(); await wrapper.get('[data-test="login-qrcode"]').trigger('click'); await flushPromises()
    expect(QRCode.toDataURL).toHaveBeenCalledWith('https://qq.example.test/login?secret=QR', expect.any(Object))
    expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,LOCAL')
    expect(wrapper.html()).not.toContain('secret=QR'); expect(wrapper.find('iframe').exists()).toBe(false)
  })
  it('discards late locally encoded QR data after role change and clears secret input', async () => {
    let finish!: (value: string) => void
    vi.mocked(QRCode.toDataURL).mockImplementation(() => new Promise(resolve => { finish = resolve }) as never)
    await start()
    await wrapper.get('[data-test="login-qrcode"]').trigger('click'); await flushPromises()
    auth.user.role = 'user'; finish('data:image/png;base64,LATE'); await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false); expect(wrapper.html()).not.toContain('PRIVATE'); expect(wrapper.find('form').exists()).toBe(false)
  })
  it('clears secrets synchronously when the current login changes', async () => {
    await start(); await wrapper.get('[name="webui_token"]').setValue('PRIVATE')
    auth.sessionRevision = 'B'; await flushPromises()
    expect(wrapper.find('form').exists()).toBe(false); expect(wrapper.html()).not.toContain('PRIVATE')
  })
  it('invalidates QR on configuration edits and requires save before login', async () => {
    await start(); await wrapper.get('[data-test="login-qrcode"]').trigger('click'); await flushPromises()
    await wrapper.get('[name="bot_qq"]').setValue('234567')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.get('[data-test="login-refresh"]').attributes('disabled')).toBeDefined()
  })
  it('requires an explicit restart confirmation and never automatically retries', async () => {
    await start(); await wrapper.get('[data-test="login-restart"]').trigger('click')
    expect(qqInviteAPI.login).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-restart"]').trigger('click'); await flushPromises()
    expect(qqInviteAPI.login).toHaveBeenCalledTimes(1); expect(vi.mocked(qqInviteAPI.login).mock.calls[0][0]).toBe('restart')
  })
  it('clears old status on a failed refresh and never displays raw upstream errors', async () => {
    await start(); vi.mocked(qqInviteAPI.connection).mockRejectedValue({ message: 'PRIVATE' })
    await wrapper.get('[data-test="connection-refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="connection-state"]').text()).toContain('qqInvite.states.unknown')
    expect(wrapper.text()).not.toContain('PRIVATE'); expect(wrapper.text()).toContain('qqInvite.failed')
  })
  it('keeps parent navigation busy until a configuration save finishes', async () => {
    await start()
    let finish!: (value: typeof config) => void
    vi.mocked(qqInviteAPI.saveConfig).mockReturnValue(new Promise(resolve => { finish = resolve }))
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([true])
    finish(config); await flushPromises()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
  })
})
