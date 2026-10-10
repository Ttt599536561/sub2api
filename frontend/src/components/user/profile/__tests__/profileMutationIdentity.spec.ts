import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProfileIdentityBindingsSection from '../ProfileIdentityBindingsSection.vue'
import ProfileEditForm from '../ProfileEditForm.vue'

const api = vi.hoisted(() => ({ unbindAuthIdentity: vi.fn(), bindEmailIdentity: vi.fn(), updateProfile: vi.fn() }))
const feedback = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
const auth = reactive({ user: { id: 7, email: 'alice@example.com', username: 'alice', role: 'user' }, sessionRevision: 'session-A' })
vi.mock('@/stores', () => ({ useAuthStore: () => auth, useAppStore: () => feedback }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => feedback }))
vi.mock('@/api', () => ({ userAPI: { updateProfile: api.updateProfile } }))
vi.mock('@/api/user', () => ({ ...api, sendEmailBindingCode: vi.fn(), startOAuthBinding: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ fullPath: '/profile' }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  auth.user = { id: 7, email: 'alice@example.com', username: 'alice', role: 'user' }
  auth.sessionRevision = 'session-A'
  localStorage.setItem('auth_user', JSON.stringify(auth.user))
  localStorage.setItem('auth_session_id', auth.sessionRevision)
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; localStorage.clear() })

const transitions = ['replace-account', 'same-account-login', 'stored-session', 'unmount'] as const
function transition(kind: typeof transitions[number]) {
  if (kind === 'unmount') { wrapper!.unmount(); wrapper = undefined; return }
  if (kind === 'stored-session') { localStorage.setItem('auth_session_id', 'session-B'); return }
  if (kind === 'replace-account') auth.user = { id: 8, email: 'bob@example.com', username: 'bob', role: 'user' }
  auth.sessionRevision = 'session-B'
  localStorage.setItem('auth_user', JSON.stringify(auth.user))
  localStorage.setItem('auth_session_id', auth.sessionRevision)
}

describe('profile mutation callbacks stay with their login', () => {
  it.each(transitions.flatMap(kind => ['success', 'failure'].map(outcome => ({ kind, outcome }))))('ignores an old profile $outcome after $kind', async ({ kind, outcome }) => {
    const pending = deferred<typeof auth.user>()
    api.updateProfile.mockReturnValue(pending.promise)
    wrapper = mount(ProfileEditForm, { props: { initialUsername: 'alice' } })
    await wrapper.get('input').setValue('submitted-name')
    await wrapper.get('form').trigger('submit')
    transition(kind)
    const currentUser = auth.user
    if (outcome === 'success') pending.resolve({ ...currentUser, id: 7, username: 'old-reply' })
    else pending.reject(new Error('old profile failure'))
    await flushPromises()
    expect(auth.user).toBe(currentUser)
    expect(feedback.showSuccess).not.toHaveBeenCalled()
    expect(feedback.showError).not.toHaveBeenCalled()
  })

  it.each(['unbind', 'bind-email'].flatMap(action => transitions.flatMap(kind => ['success', 'failure'].map(outcome => ({ action, kind, outcome })))))('ignores old $action $outcome after $kind', async ({ action, kind, outcome }) => {
    const pending = deferred<typeof auth.user>()
    api.unbindAuthIdentity.mockReturnValue(pending.promise)
    api.bindEmailIdentity.mockReturnValue(pending.promise)
    wrapper = mount(ProfileIdentityBindingsSection, {
      props: { user: { ...auth.user, email_bound: true, linuxdo_bound: true, auth_bindings: { linuxdo: { bound: true, can_unbind: true } } } as never },
      global: { stubs: { Icon: true } }
    })
    if (action === 'unbind') await wrapper.get('[data-testid="profile-binding-linuxdo-unbind"]').trigger('click')
    else {
      await wrapper.get('[data-testid="profile-binding-email-code-input"]').setValue('123456')
      await wrapper.get('[data-testid="profile-binding-email-password-input"]').setValue('password')
      await wrapper.get('[data-testid="profile-binding-email-submit"]').trigger('click')
    }
    transition(kind)
    const currentUser = auth.user
    if (outcome === 'success') pending.resolve({ ...currentUser, id: 7, username: 'old-reply' })
    else pending.reject(new Error('old binding failure'))
    await flushPromises()
    expect(auth.user).toBe(currentUser)
    expect(feedback.showSuccess).not.toHaveBeenCalled()
    expect(feedback.showError).not.toHaveBeenCalled()
  })

  it('keeps current-session profile updates and feedback', async () => {
    api.updateProfile.mockResolvedValue({ ...auth.user, username: 'saved-name' })
    wrapper = mount(ProfileEditForm, { props: { initialUsername: 'alice' } })
    await wrapper.get('input').setValue('saved-name')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(auth.user.username).toBe('saved-name')
    expect(feedback.showSuccess).toHaveBeenCalledWith('profile.updateSuccess')
  })
})
