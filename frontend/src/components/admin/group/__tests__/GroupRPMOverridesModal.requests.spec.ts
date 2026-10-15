vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 }, sessionRevision: 0, isAdmin: true }) }))
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/types'
import GroupRPMOverridesModal from '../GroupRPMOverridesModal.vue'

const mocks = vi.hoisted(() => ({ getGroupRPMOverrides: vi.fn(), batchSetGroupRPMOverrides: vi.fn(), showError: vi.fn(), list: vi.fn(), clearGroupRPMOverrides: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: mocks, users: { list: mocks.list } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.resetAllMocks(); localStorage.clear() })
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); localStorage.clear() })

function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const group = (id: number) => ({ id, name: `Group ${id}`, platform: 'openai', rpm_limit: 100 } as AdminGroup)
const entries = (id: number) => [{ user_id: id, user_name: '', user_email: `user${id}@example.com`, user_status: 'active', rpm_override: 100 }]

async function openGroup() {
  const wrapper = mount(GroupRPMOverridesModal, {
    props: { show: false, group: group(1) },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      Icon: true, PlatformIcon: true, Pagination: true
    } }
  })
  await wrapper.setProps({ show: true })
  return wrapper
}

describe('group RPM override request ownership', () => {
  it('keeps the reopened group data when an older response finishes last', async () => {
    const old = deferred()
    mocks.getGroupRPMOverrides.mockReturnValueOnce(old.promise).mockResolvedValueOnce(entries(2))
    const wrapper = await openGroup()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, group: group(2) })
    await flushPromises()
    old.resolve(entries(1))
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('user2@example.com')
    expect(wrapper.get('tbody').text()).not.toContain('user1@example.com')
    await wrapper.get('tbody input[type="number"]').setValue('200')
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    expect(mocks.batchSetGroupRPMOverrides).toHaveBeenCalledWith(2, [{ user_id: 2, rpm_override: 200 }])
  })

  it('does not end the new load or report an error when an old request fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const old = deferred()
    const current = deferred()
    mocks.getGroupRPMOverrides.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await openGroup()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, group: group(2) })
    old.reject(new Error('old request failed'))
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.find('svg.animate-spin').exists()).toBe(true)
    current.resolve(entries(2))
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('user2@example.com')
  })

  it('loads a different group while the dialog remains open', async () => {
    mocks.getGroupRPMOverrides.mockResolvedValueOnce(entries(1)).mockResolvedValueOnce(entries(2))
    const wrapper = await openGroup()
    await flushPromises()
    await wrapper.setProps({ group: group(2) })
    await flushPromises()
    expect(mocks.getGroupRPMOverrides).toHaveBeenLastCalledWith(2)
    expect(wrapper.get('tbody').text()).toContain('user2@example.com')
  })

  it('discards the previous group draft before loading the next group', async () => {
    mocks.getGroupRPMOverrides.mockResolvedValueOnce(entries(1)).mockReturnValueOnce(deferred().promise)
    const wrapper = await openGroup()
    await flushPromises()
    await wrapper.get('tbody input[type="number"]').setValue('200')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, group: group(2) })
    expect(wrapper.findAll('button').some(button => button.text() === 'common.save')).toBe(false)
  })

  it('ignores a request failure after unmounting', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const request = deferred()
    mocks.getGroupRPMOverrides.mockReturnValue(request.promise)
    const wrapper = await openGroup()
    wrapper.unmount()
    request.reject(new Error('offline'))
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
  })
})

it('does not close a new group when an old save finishes', async () => {
  const old = deferred()
  mocks.getGroupRPMOverrides.mockResolvedValueOnce(entries(1)).mockResolvedValueOnce(entries(2))
  mocks.batchSetGroupRPMOverrides.mockReturnValueOnce(old.promise)
  const wrapper = await openGroup()
  await flushPromises()
  await wrapper.get('tbody input[type="number"]').setValue('300')
  await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
  await wrapper.setProps({ show:false })
  await wrapper.setProps({ show:true,group:group(2) })
  await flushPromises()
  old.resolve({})
  await flushPromises()
  expect(wrapper.emitted('close')).toBeUndefined()
  expect(wrapper.emitted('success')).toBeUndefined()
  expect(wrapper.get('tbody').text()).toContain('user2@example.com')
})
it('keeps a newer save busy when an older save rejects', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  const old = deferred(), current = deferred()
  mocks.getGroupRPMOverrides.mockResolvedValueOnce(entries(1)).mockResolvedValueOnce(entries(2))
  mocks.batchSetGroupRPMOverrides.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
  const wrapper = await openGroup(); await flushPromises()
  await wrapper.get('tbody input[type="number"]').setValue('300')
  await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
  await wrapper.setProps({show:false}); await wrapper.setProps({show:true,group:group(2)}); await flushPromises()
  await wrapper.get('tbody input[type="number"]').setValue('400')
  await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
  old.reject(new Error('old')); await flushPromises()
  expect(mocks.showError).not.toHaveBeenCalled()
  expect(wrapper.findAll('button').find(b => b.text() === 'common.save')!.attributes('disabled')).toBeDefined()
  current.resolve({}); await flushPromises()
})

it('drops loaded private rows on a replacement login', async () => {
  mocks.getGroupRPMOverrides.mockResolvedValueOnce(entries(1))
  const wrapper=await openGroup(); await flushPromises()
  localStorage.setItem('auth_session_id','new-login')
  window.dispatchEvent(new StorageEvent('storage',{key:'auth_session_id'})); await flushPromises()
  expect(wrapper.text()).not.toContain('user1@example.com')
})

it('ignores stale search results and cancels a pending search on close', async () => {
  vi.useFakeTimers()
  const old=deferred()
  mocks.getGroupRPMOverrides.mockResolvedValue([])
  mocks.list.mockReturnValueOnce(old.promise).mockResolvedValueOnce({items:[{id:2,email:'new@example.com'}]})
  const wrapper=await openGroup(); await flushPromises()
  await wrapper.get('input[type="text"]').setValue('old'); await vi.advanceTimersByTimeAsync(300)
  await wrapper.get('input[type="text"]').setValue('new'); await vi.advanceTimersByTimeAsync(300); await flushPromises()
  old.resolve({items:[{id:1,email:'old@example.com'}]}); await flushPromises()
  expect(wrapper.text()).not.toContain('old@example.com'); expect(wrapper.text()).toContain('new@example.com')
  await wrapper.get('input[type="text"]').setValue('late'); await wrapper.setProps({show:false})
  await vi.advanceTimersByTimeAsync(300); expect(mocks.list).toHaveBeenCalledTimes(2)
})

it('does not clear a new group after an old clear completes', async () => {
  const old=deferred()
  mocks.getGroupRPMOverrides.mockResolvedValueOnce(entries(1)).mockResolvedValueOnce(entries(2))
  mocks.clearGroupRPMOverrides.mockReturnValueOnce(old.promise)
  const wrapper=await openGroup(); await flushPromises()
  await wrapper.findAll('button').find(b=>b.text()==='admin.groups.clearAll')!.trigger('click')
  await wrapper.setProps({show:false}); await wrapper.setProps({show:true,group:group(2)}); await flushPromises()
  old.resolve({}); await flushPromises()
  expect(wrapper.get('tbody').text()).toContain('user2@example.com')
})

it('does not submit an old draft before the storage event is delivered', async () => {
 mocks.getGroupRPMOverrides.mockResolvedValueOnce(entries(1))
 const wrapper=await openGroup(); await flushPromises()
 await wrapper.get('tbody input[type="number"]').setValue('300')
 localStorage.setItem('auth_session_id','replacement-before-event')
 await wrapper.findAll('button').find(b=>b.text()==='common.save')!.trigger('click')
 expect(mocks.batchSetGroupRPMOverrides).not.toHaveBeenCalled()
})
