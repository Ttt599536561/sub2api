import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import ProfileIdentityBindingsSection from '../ProfileIdentityBindingsSection.vue'

const feedback = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn(), cachedPublicSettings: null }))
const auth = reactive({ user: { id: 7 }, sessionRevision: 'session-A' })
vi.mock('@/stores', () => ({ useAuthStore: () => auth, useAppStore: () => feedback }))
vi.mock('vue-router', () => ({ useRoute: () => ({ fullPath: '/profile' }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
const actualWindow = window
let location: { href: string; origin: string; pathname: string }
let wrapper: VueWrapper | undefined
function start() {
  wrapper = mount(ProfileIdentityBindingsSection, { props: { user: { id: 7, email: 'alice@example.com', email_bound: true } as never, linuxdoEnabled: true }, global: { stubs: { Icon: true } } })
}
beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear(); auth.user = { id: 7 }; auth.sessionRevision = 'session-A'
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 })); localStorage.setItem('auth_session_id', 'session-A'); localStorage.setItem('auth_token', 'token-A')
  location = { href: `${actualWindow.location.origin}/profile`, origin: actualWindow.location.origin, pathname: '/profile' }
  vi.stubGlobal('window', new Proxy(actualWindow, { get(object, key) { if (key === 'location') return location; const value = Reflect.get(object, key, object); return typeof value === 'function' ? value.bind(object) : value } }))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; apiClient.defaults.adapter = originalAdapter; vi.unstubAllGlobals(); localStorage.clear() })
describe('binding button lifecycle through real OAuth cookie preparation', () => {
  it.each(['unmount', 'storage'].flatMap(change => ['success', 'failure'].map(outcome => ({ change, outcome }))))('does not navigate or emit late feedback after $change and $outcome', async ({ change, outcome }) => {
    let finish!: () => void, reached!: () => void
    const dispatched = new Promise<void>(resolve => { reached = resolve })
    apiClient.defaults.adapter = config => {
      reached()
      return new Promise((resolve, reject) => {
        finish = () => outcome === 'success'
          ? resolve({ data: {}, status: 200, statusText: 'OK', headers: {}, config })
          : reject(new AxiosError('cookie failed', 'ERR_BAD_RESPONSE', config, undefined, { data: { message: 'cookie failed' }, status: 500, statusText: 'Error', headers: {}, config }))
      })
    }
    start(); const clicked = wrapper!.get('[data-testid="profile-binding-linuxdo-action"]').trigger('click')
    await dispatched; await clicked
    const before = location.href
    if (change === 'unmount') { wrapper!.unmount(); wrapper = undefined }
    else { localStorage.setItem('auth_session_id', 'session-B'); actualWindow.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' })) }
    finish(); await flushPromises()
    expect(location.href).toBe(before); expect(feedback.showSuccess).not.toHaveBeenCalled(); expect(feedback.showError).not.toHaveBeenCalled()
  })
  it('keeps normal provider navigation after a same-login token rotation', async () => {
    const requests: InternalAxiosRequestConfig[] = []
    apiClient.defaults.adapter = async config => { requests.push(config); return { data: {}, status: 200, statusText: 'OK', headers: {}, config } }
    start(); const clicked = wrapper!.get('[data-testid="profile-binding-linuxdo-action"]').trigger('click')
    localStorage.setItem('auth_token', 'rotated-token'); await clicked; await flushPromises()
    expect(requests[0].headers.get('Authorization')).toBe('Bearer rotated-token')
    expect(location.href).toBe('/api/v1/auth/oauth/linuxdo/bind/start?redirect=%2Fprofile&intent=bind_current_user')
  })
})
