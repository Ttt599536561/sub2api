import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { useAuthStore } from '@/stores/auth'
import type { AdminGroup } from '@/types'
import GroupRPMOverridesModal from '../GroupRPMOverridesModal.vue'
import GroupRateMultipliersModal from '../GroupRateMultipliersModal.vue'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  const state = reactive({ user: { id: 7 }, sessionRevision: 'initial', isAdmin: true })
  return { useAuthStore: () => state }
})
const mocks = vi.hoisted(() => ({
  getGroupRPMOverrides: vi.fn(), batchSetGroupRPMOverrides: vi.fn(), clearGroupRPMOverrides: vi.fn(),
  getGroupRateMultipliers: vi.fn(), batchSetGroupRateMultipliers: vi.fn(),
  list: vi.fn(), showError: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: mocks, users: { list: mocks.list } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
const auth = useAuthStore() as unknown as { user: { id: number }; sessionRevision: string; isAdmin: boolean }
const group = { id: 1, name: 'Group 1', platform: 'openai', rpm_limit: 100, rate_multiplier: 1 } as AdminGroup
const row = { user_id: 1, user_name: '', user_email: 'user1@example.com', user_status: 'active', user_notes: '' }

function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function login(session: string) {
  localStorage.setItem('auth_session_id', session)
  localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin' }))
  localStorage.setItem('auth_token', session)
}
const modes = [
  { name: 'RPM', component: GroupRPMOverridesModal, get: mocks.getGroupRPMOverrides, save: mocks.batchSetGroupRPMOverrides,
    rows: [{ ...row, rpm_override: 100 }], saved: [{ user_id: 1, rpm_override: 200 }] },
  { name: 'rate', component: GroupRateMultipliersModal, get: mocks.getGroupRateMultipliers, save: mocks.batchSetGroupRateMultipliers,
    rows: [{ ...row, rate_multiplier: 2, rpm_override: null }], saved: [{ user_id: 1, rate_multiplier: 200 }] }
]
type Mode = typeof modes[number]
async function open(mode: Mode) {
  const wrapper = mount(mode.component, {
    props: { show: false, group },
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, Icon: true, PlatformIcon: true, Pagination: true } }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  login('initial')
  auth.user.id = 7
  auth.isAdmin = true
  auth.sessionRevision = 'initial'
  for (const mode of modes) { mode.get.mockResolvedValue(mode.rows); mode.save.mockResolvedValue({}) }
})
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); localStorage.clear() })

describe.each(modes)('$name dialog after login invalidation', mode => {
  it('closes on a replacement login and reloads the complete list when reopened', async () => {
    const wrapper = await open(mode)
    login('replacement')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('user1@example.com')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(mode.get).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('user1@example.com')
    await wrapper.get('tbody input[type="number"]').setValue('200')
    await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    expect(mode.save).toHaveBeenCalledWith(1, mode.saved)
  })

  it('preserves existing users when adding a user after the replacement login reopens', async () => {
    vi.useFakeTimers()
    mocks.list.mockResolvedValue({ items: [{ id: 2, email: 'user2@example.com', status: 'active' }] })
    const wrapper = await open(mode)
    login('replacement')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await wrapper.get('input[type="text"]').setValue('user2')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('user2@example.com'))!.trigger('click')
    await wrapper.get('input[type="number"]').setValue('200')
    await wrapper.findAll('button').find(b => b.text() === 'common.add')!.trigger('click')
    await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    expect(mode.save).toHaveBeenCalledWith(1, mode.name === 'RPM'
      ? [{ user_id: 1, rpm_override: 100 }, { user_id: 2, rpm_override: 200 }]
      : [{ user_id: 1, rate_multiplier: 2 }, { user_id: 2, rate_multiplier: 200 }])
  })
  it('rejects editing and saving until the parent processes the session close', async () => {
    vi.useFakeTimers()
    mocks.list.mockResolvedValue({ items: [{ id: 2, email: 'user2@example.com', status: 'active' }] })
    const wrapper = await open(mode)
    login('replacement')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    await flushPromises()
    // Parent intentionally has not changed show yet. Cleared rows are not an authoritative empty list.
    await wrapper.get('input[type="text"]').setValue('user2')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    const user = wrapper.findAll('button').find(b => b.text().includes('user2@example.com'))
    if (user) {
      await user.trigger('click')
      await wrapper.get('input[type="number"]').setValue('200')
      await wrapper.findAll('button').find(b => b.text() === 'common.add')!.trigger('click')
      await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    }
    expect(mocks.list).not.toHaveBeenCalled()
    expect(mode.save).not.toHaveBeenCalled()
  })

  it('closes on in-tab session replacement', async () => {
    const wrapper = await open(mode)
    login('replacement')
    auth.sessionRevision = 'replacement'
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('user1@example.com')
  })

  it('stays invalid after administrator privileges are lost and restored', async () => {
    vi.useFakeTimers()
    const wrapper = await open(mode)
    auth.isAdmin = false
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    auth.isAdmin = true
    await flushPromises()
    await wrapper.get('input[type="text"]').setValue('user2')
    await vi.advanceTimersByTimeAsync(300)
    expect(mocks.list).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(mode.save).not.toHaveBeenCalled()
    expect(mode.get).toHaveBeenCalledTimes(1)
  })

  it('keeps the reopened list when a previous login load completes late', async () => {
    const old = deferred()
    mode.get.mockReturnValueOnce(old.promise)
    const wrapper = await open(mode)
    login('replacement')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    old.resolve([])
    await flushPromises()
    expect(wrapper.text()).toContain('user1@example.com')
  })
})

describe('RPM mutation exclusion', () => {
  it('blocks save while clear is pending, then allows a save after clear fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const clearing = deferred()
    mocks.clearGroupRPMOverrides.mockReturnValueOnce(clearing.promise)
    const wrapper = await open(modes[0])
    await wrapper.get('tbody input[type="number"]').setValue('200')
    const save = wrapper.findAll('button').find(b => b.text() === 'common.save')!
    await wrapper.findAll('button').find(b => b.text() === 'admin.groups.clearAll')!.trigger('click')
    expect(save.attributes('disabled')).toBeDefined()
    await save.trigger('click')
    expect(mocks.batchSetGroupRPMOverrides).not.toHaveBeenCalled()
    clearing.reject(new Error('clear failed'))
    await flushPromises()
    expect(save.attributes('disabled')).toBeUndefined()
    await save.trigger('click')
    expect(mocks.batchSetGroupRPMOverrides).toHaveBeenCalledWith(1, [{ user_id: 1, rpm_override: 200 }])
  })

  it('blocks clear while save is pending, then allows clear after save fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const saving = deferred()
    mocks.batchSetGroupRPMOverrides.mockReturnValueOnce(saving.promise)
    mocks.clearGroupRPMOverrides.mockResolvedValue({})
    const wrapper = await open(modes[0])
    await wrapper.get('tbody input[type="number"]').setValue('200')
    const clear = wrapper.findAll('button').find(b => b.text() === 'admin.groups.clearAll')!
    await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    expect(clear.attributes('disabled')).toBeDefined()
    await clear.trigger('click')
    expect(mocks.clearGroupRPMOverrides).not.toHaveBeenCalled()
    saving.reject(new Error('save failed'))
    await flushPromises()
    expect(clear.attributes('disabled')).toBeUndefined()
    await clear.trigger('click')
    expect(mocks.clearGroupRPMOverrides).toHaveBeenCalledOnce()
  })
})

describe('RPM same-frame mutation clicks', () => {
  it.each(['clear-first', 'save-first'])('serializes %s before the DOM disables the buttons', async order => {
    const pending = deferred()
    const first = order === 'clear-first' ? mocks.clearGroupRPMOverrides : mocks.batchSetGroupRPMOverrides
    const second = order === 'clear-first' ? mocks.batchSetGroupRPMOverrides : mocks.clearGroupRPMOverrides
    first.mockReturnValueOnce(pending.promise)
    const wrapper = await open(modes[0])
    await wrapper.get('tbody input[type="number"]').setValue('200')
    const clear = wrapper.findAll('button').find(b => b.text() === 'admin.groups.clearAll')!
    const save = wrapper.findAll('button').find(b => b.text() === 'common.save')!
    const buttons = order === 'clear-first' ? [clear, save] : [save, clear]
    const firstClick = buttons[0].trigger('click')
    const secondClick = buttons[1].trigger('click')
    await Promise.all([firstClick, secondClick])
    expect(first).toHaveBeenCalledOnce()
    expect(second).not.toHaveBeenCalled()
    pending.resolve({})
    await flushPromises()
  })
})

describe.each(modes)('$name authoritative list requirement', mode => {
  it('does not replace server rows after reopening fails to load', async () => {
    vi.useFakeTimers()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.list.mockResolvedValue({ items: [{ id: 2, email: 'user2@example.com', status: 'active' }] })
    const wrapper = await open(mode)
    login('replacement')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_session_id' }))
    mode.get.mockRejectedValueOnce(new Error('cannot load current list'))
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await wrapper.get('input[type="text"]').setValue('user2')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    const user = wrapper.findAll('button').find(b => b.text().includes('user2@example.com'))
    if (user) {
      await user.trigger('click')
      await wrapper.get('input[type="number"]').setValue('200')
      await wrapper.findAll('button').find(b => b.text() === 'common.add')!.trigger('click')
      const save = wrapper.findAll('button').find(b => b.text() === 'common.save')
      if (save) await save.trigger('click')
    }
    expect(mode.save).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledOnce()
    // Closing and reopening successfully restores a usable, complete form.
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.text()).toContain('user1@example.com')
    await wrapper.get('tbody input[type="number"]').setValue('200')
    await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    expect(mode.save).toHaveBeenCalledWith(1, mode.saved)
  })
})
