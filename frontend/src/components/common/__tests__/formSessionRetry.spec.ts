import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { useAuthStore } from '@/stores/auth'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import UserPlatformQuotaModal from '@/components/admin/user/UserPlatformQuotaModal.vue'
import ProfileEditForm from '@/components/user/profile/ProfileEditForm.vue'
import ProfileIdentityBindingsSection from '@/components/user/profile/ProfileIdentityBindingsSection.vue'

const api = vi.hoisted(() => ({ login: vi.fn(), logout: vi.fn(), getCurrentUser: vi.fn(), sync: vi.fn(), get: vi.fn(), save: vi.fn(), reset: vi.fn(), update: vi.fn(), unbind: vi.fn(), bind: vi.fn(), sendCode: vi.fn() }))
const feedback = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn(), showWarning: vi.fn() }))
vi.mock('@/api', () => ({ authAPI: { login: api.login, logout: api.logout, getCurrentUser: api.getCurrentUser }, isTotp2FARequired: () => false, passkeyAPI: {}, userAPI: { updateProfile: api.update } }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: { syncUpstreamModels: api.sync } }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { getPlatformQuotas: api.get, updatePlatformQuotas: api.save, resetPlatformQuotaWindow: api.reset } } }))
vi.mock('@/api/user', () => ({ unbindAuthIdentity: api.unbind, bindEmailIdentity: api.bind, sendEmailBindingCode: api.sendCode, startOAuthBinding: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => feedback }))
vi.mock('@/stores', () => ({ useAuthStore: () => useAuthStore(), useAppStore: () => feedback }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ fullPath: '/profile' }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' } }))

function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (error: Error) => void
  const promise = new Promise((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
const user = { id: 7, email: 'alice@example.com', username: 'alice', role: 'admin', email_bound: true, linuxdo_bound: true, auth_bindings: { linuxdo: { bound: true, can_unbind: true } } }
const quotas = { platform_quotas: [{ platform: 'anthropic', daily_limit_usd: 80 }] }
const credentials = { email: user.email, password: 'password' }
type Action = 'model' | 'quota-load' | 'quota-save' | 'quota-reset' | 'profile' | 'unbind' | 'bind' | 'send-code'
const actions: Action[] = ['model', 'quota-load', 'quota-save', 'quota-reset', 'profile', 'unbind', 'bind', 'send-code']
let wrapper: VueWrapper | undefined
function method(action: Action) { return ({ model: api.sync, 'quota-load': api.get, 'quota-save': api.save, 'quota-reset': api.reset, profile: api.update, unbind: api.unbind, bind: api.bind, 'send-code': api.sendCode })[action] }
function result(action: Action) { return action === 'model' ? { models: ['new-model'] } : action.startsWith('quota-') ? quotas : action === 'send-code' ? undefined : { ...user, username: 'new-result' } }
function control(action: Action) {
  if (action === 'model') return wrapper!.findAll('button').find(b => ['admin.accounts.syncUpstreamModels', 'admin.accounts.syncUpstreamModelsLoading'].includes(b.text()))!
  if (action === 'quota-save') return wrapper!.findAll('button').find(b => ['admin.users.platformQuota.save', 'admin.users.platformQuota.saving'].includes(b.text()))!
  if (action === 'quota-reset') return wrapper!.get('button[title="admin.users.platformQuota.reset.button"]')
  if (action === 'profile') return wrapper!.get('button[type="submit"]')
  const suffix = action === 'unbind' ? 'linuxdo-unbind' : action === 'bind' ? 'email-submit' : 'email-send-code'
  return wrapper!.get(`[data-testid="profile-binding-${suffix}"]`)
}
async function mountAction(action: Action) {
  if (action === 'model') wrapper = mount(ModelWhitelistSelector, { props: { platform: 'openai', accountId: 7, modelValue: [] }, global: { stubs: { ModelIcon: true } } })
  else if (action.startsWith('quota-')) {
    wrapper = mount(UserPlatformQuotaModal, { props: { show: false, user: user as never } })
    await wrapper.setProps({ show: true })
  } else if (action === 'profile') {
    wrapper = mount(ProfileEditForm, { props: { initialUsername: 'alice' } })
    await wrapper.get('input').setValue('new-name')
  } else {
    wrapper = mount(ProfileIdentityBindingsSection, { props: { user: user as never }, global: { stubs: { Icon: true } } })
    if (action === 'bind') {
      await wrapper.get('[data-testid="profile-binding-email-code-input"]').setValue('123456')
      await wrapper.get('[data-testid="profile-binding-email-password-input"]').setValue('password')
    }
  }
  await flushPromises()
}
async function changeLogin(kind: 'revision' | 'storage') {
  if (kind === 'revision') await useAuthStore().login(credentials)
  else {
    const oldValue = localStorage.getItem('auth_session_id')
    localStorage.setItem('auth_session_id', 'peer-login')
    localStorage.setItem('auth_token', 'peer-token')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id', oldValue, newValue: 'peer-login' }))
  }
  await flushPromises()
}
async function submit(action: Action) {
  if (action === 'profile') await wrapper!.get('form').trigger('submit')
  else await control(action).trigger('click')
}
beforeEach(async () => {
  vi.resetAllMocks(); localStorage.clear(); setActivePinia(createPinia())
  api.login.mockResolvedValue({ access_token: 'token', user })
  api.logout.mockResolvedValue(undefined); api.getCurrentUser.mockResolvedValue({ data: user })
  api.get.mockResolvedValue(quotas)
  await useAuthStore().login(credentials)
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})
afterEach(async () => { wrapper?.unmount(); wrapper = undefined; await useAuthStore().logout(); vi.restoreAllMocks(); localStorage.clear() })

describe('mounted forms can retry after a same-user replacement login', () => {
  it.each(actions.flatMap(action => (['revision', 'storage'] as const).flatMap(kind => (['success', 'failure'] as const).map(outcome => ({ action, kind, outcome })))))('$action can retry after $kind and keeps B busy after A $outcome', async ({ action, kind, outcome }) => {
    const old = deferred(), fresh = deferred(), request = method(action)
    request.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    await mountAction(action)
    if (action !== 'quota-load') await submit(action)
    expect(request).toHaveBeenCalledTimes(1)
    const oldSession = localStorage.getItem('auth_session_id')
    await changeLogin(kind)
    expect(localStorage.getItem('auth_session_id')).not.toBe(oldSession)
    if (action !== 'quota-load') {
      expect(control(action).element).toHaveProperty('disabled', false)
      await submit(action)
    }
    expect(request).toHaveBeenCalledTimes(2)
    if (outcome === 'success') old.resolve(result(action))
    else old.reject(new Error('old login failure'))
    await flushPromises()
    expect(feedback.showSuccess).not.toHaveBeenCalled(); expect(feedback.showError).not.toHaveBeenCalled()
    if (action === 'quota-load') expect(wrapper!.findAll('input[type=number]')).toHaveLength(0)
    else expect(control(action).element).toHaveProperty('disabled', true)
    fresh.resolve(result(action)); await flushPromises()
    if (action === 'quota-load') expect(wrapper!.findAll('input[type=number]')[0].element).toHaveProperty('value', '80')
    else expect(feedback.showSuccess).toHaveBeenCalledTimes(1)
  })

  it.each(actions.flatMap(action => (['token', 'profile'] as const).map(kind => ({ action, kind }))))('$action remains pending through same-login $kind storage changes', async ({ action, kind }) => {
    const pending = deferred(), request = method(action)
    request.mockReturnValueOnce(pending.promise); await mountAction(action)
    if (action !== 'quota-load') await submit(action)
    const key = kind === 'token' ? 'auth_token' : 'auth_user'
    localStorage.setItem(key, kind === 'token' ? 'rotated-token' : JSON.stringify({ ...user, username: 'refreshed-profile' }))
    window.dispatchEvent(new StorageEvent('storage', { key }))
    await flushPromises()
    expect(request).toHaveBeenCalledTimes(1)
    if (action === 'quota-load') expect(wrapper!.findAll('input[type=number]')).toHaveLength(0)
    else expect(control(action).element).toHaveProperty('disabled', true)
    pending.resolve(result(action)); await flushPromises()
    if (action === 'quota-load') expect(wrapper!.findAll('input[type=number]')[0].element).toHaveProperty('value', '80')
    else expect(feedback.showSuccess).toHaveBeenCalledTimes(1)
  })
})
