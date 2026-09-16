import { defineComponent, nextTick, reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminComplianceDialog from '@/components/admin/AdminComplianceDialog.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import { useAnnouncementStore } from '@/stores/announcements'
import type { AdminComplianceStatus } from '@/api/admin/compliance'
import type { UserAnnouncement } from '@/types'

const api = vi.hoisted(() => ({ accept: vi.fn(), markRead: vi.fn(), list: vi.fn() }))
const feedback = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
const auth = reactive({ isAuthenticated: true, isAdmin: true, user: { id: 7 }, sessionRevision: 0 })
vi.mock('@/api/admin/compliance', () => ({ default: { accept: api.accept } }))
vi.mock('@/api', () => ({ announcementsAPI: { list: api.list, markRead: api.markRead } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => feedback }))
vi.mock('@/stores', () => ({
  useAdminComplianceStore: () => useAdminComplianceStore(),
  useAppStore: () => feedback,
  useAuthStore: () => auth
}))
vi.mock('@/i18n', () => ({ getLocale: () => 'en', i18n: { global: { t: (key: string) => key } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((success, failure) => { resolve = success; reject = failure })
  return { promise, resolve, reject }
}

function compliance(required = true): AdminComplianceStatus {
  return { required, version: 'same-version', document_path_zh: '', document_path_en: '',
    document_url_zh: '', document_url_en: '', ack_phrase_zh: 'agree', ack_phrase_en: 'agree' }
}

function announcement(id: number, read = false): UserAnnouncement {
  return { id, title: `User announcement ${id}`, content: `Private details ${id}`,
    notify_mode: 'silent', created_at: '', updated_at: '', read_at: read ? '2026-09-17' : undefined }
}

type Transition = 'switch' | 'roundtrip' | 'logout' | 'unmount' | 'same-user-login'
function changeIdentity(kind: Transition) {
  if (kind === 'logout') auth.isAuthenticated = false
  else if (kind === 'same-user-login') auth.sessionRevision++
  else {
    auth.user = { id: 8 }
    if (kind === 'roundtrip') auth.user = { id: 7 }
  }
}

const cases = (['switch', 'roundtrip', 'logout', 'unmount', 'same-user-login'] as const)
  .flatMap(transition => (['success', 'failure'] as const).map(outcome => ({ transition, outcome })))

const dialogStub = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const bellStubs = { teleport: true, transition: defineComponent({ template: '<div><slot /></div>' }), Icon: true }

beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
  auth.isAuthenticated = true
  auth.user = { id: 7 }
  auth.sessionRevision = 0
  api.markRead.mockResolvedValue({ message: 'ok' })
})

describe('administrator acknowledgement callbacks', () => {
  it.each(cases)('ignores $outcome after $transition without clearing the current draft', async ({ transition, outcome }) => {
    const pending = deferred<AdminComplianceStatus>()
    api.accept.mockReturnValue(pending.promise)
    const store = useAdminComplianceStore()
    store.requireAcknowledgement(compliance())
    const wrapper = mount(AdminComplianceDialog, { global: { stubs: { BaseDialog: dialogStub, Icon: true } } })
    let mounted = true
    try {
      await wrapper.get('input').setValue('agree')
      await wrapper.findAll('button').find(button => button.text() === 'adminCompliance.accept')!.trigger('click')
      expect(api.accept).toHaveBeenCalledOnce()
      if (transition === 'unmount') { wrapper.unmount(); mounted = false }
      else changeIdentity(transition)
      store.reset()
      store.requireAcknowledgement(compliance())
      await nextTick()
      if (mounted && transition !== 'logout') await wrapper.get('input').setValue('new identity draft')

      if (outcome === 'success') pending.resolve(compliance(false))
      else pending.reject(new Error('old identity failure'))
      await flushPromises()

      expect(feedback.showSuccess).not.toHaveBeenCalled()
      expect(feedback.showError).not.toHaveBeenCalled()
      if (mounted && transition !== 'logout') expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('new identity draft')
    } finally { if (mounted) wrapper.unmount() }
  })

  it('clears the old input when identity changes even if visibility and phrase stay equal', async () => {
    useAdminComplianceStore().requireAcknowledgement(compliance())
    const wrapper = mount(AdminComplianceDialog, { global: { stubs: { BaseDialog: dialogStub, Icon: true } } })
    try {
      await wrapper.get('input').setValue('previous identity draft')
      auth.user = { id: 8 }
      await nextTick()
      expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('')
    } finally { wrapper.unmount() }
  })

  it.each(['success', 'failure'] as const)('keeps current-identity %s feedback', async outcome => {
    if (outcome === 'success') api.accept.mockResolvedValue(compliance(false))
    else api.accept.mockRejectedValue(new Error('current failure'))
    useAdminComplianceStore().requireAcknowledgement(compliance())
    const wrapper = mount(AdminComplianceDialog, { global: { stubs: { BaseDialog: dialogStub, Icon: true } } })
    try {
      await wrapper.get('input').setValue('agree')
      await wrapper.findAll('button').find(button => button.text() === 'adminCompliance.accept')!.trigger('click')
      await flushPromises()
      if (outcome === 'success') expect(feedback.showSuccess).toHaveBeenCalledWith('adminCompliance.accepted')
      else expect(feedback.showError).toHaveBeenCalledWith('current failure')
    } finally { wrapper.unmount() }
  })
})

describe('announcement callbacks and details', () => {
  it.each(['success', 'failure'] as const)('only closes details after a successful current-identity mark-read: %s', async outcome => {
    api.markRead.mockRejectedValueOnce(new Error('initial automatic mark failed'))
    useAnnouncementStore().announcements = [announcement(1)]
    const wrapper = mount(AnnouncementBell, { global: { stubs: bellStubs } })
    const errorLog = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    try {
      await wrapper.get('[aria-label="announcements.title"]').trigger('click')
      await wrapper.get('h3').trigger('click')
      await flushPromises()
      feedback.showError.mockClear()
      if (outcome === 'failure') api.markRead.mockRejectedValueOnce(new Error('mark read failed'))

      await wrapper.findAll('button').find(button => button.text() === 'announcements.markRead')!.trigger('click')
      await flushPromises()

      if (outcome === 'success') {
        expect(feedback.showSuccess).toHaveBeenCalledWith('announcements.markedAsRead')
        expect(wrapper.text()).not.toContain('Private details 1')
      } else {
        expect(feedback.showSuccess).not.toHaveBeenCalled()
        expect(feedback.showError).toHaveBeenCalledWith('mark read failed')
        expect(wrapper.text()).toContain('Private details 1')
        expect(useAnnouncementStore().unreadCount).toBe(1)
      }
    } finally { wrapper.unmount(); errorLog.mockRestore() }
  })

  it.each(cases)('ignores mark-all $outcome after $transition', async ({ transition, outcome }) => {
    const pending = deferred<{ message: string }>()
    api.markRead.mockReturnValue(pending.promise)
    const store = useAnnouncementStore()
    store.announcements = [announcement(1)]
    const wrapper = mount(AnnouncementBell, { global: { stubs: bellStubs } })
    let mounted = true
    const errorLog = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    try {
      await wrapper.get('[aria-label="announcements.title"]').trigger('click')
      await wrapper.findAll('button').find(button => button.text() === 'announcements.markAllRead')!.trigger('click')
      expect(api.markRead).toHaveBeenCalledWith(1)
      if (transition === 'unmount') { wrapper.unmount(); mounted = false }
      else changeIdentity(transition)
      store.reset()
      store.announcements = [announcement(2)]

      if (outcome === 'success') pending.resolve({ message: 'ok' })
      else pending.reject(new Error('old identity failure'))
      await flushPromises()

      expect(feedback.showSuccess).not.toHaveBeenCalled()
      expect(feedback.showError).not.toHaveBeenCalled()
    } finally { if (mounted) wrapper.unmount(); errorLog.mockRestore() }
  })

  it('clears open details and does not reopen old content after an identity roundtrip', async () => {
    const store = useAnnouncementStore()
    store.announcements = [announcement(1, true)]
    const wrapper = mount(AnnouncementBell, { global: { stubs: bellStubs } })
    try {
      await wrapper.get('[aria-label="announcements.title"]').trigger('click')
      await wrapper.get('h3').trigger('click')
      expect(wrapper.text()).toContain('Private details 1')
      changeIdentity('roundtrip')
      store.reset()
      await nextTick()

      expect(wrapper.text()).not.toContain('Private details 1')
      await wrapper.get('[aria-label="announcements.title"]').trigger('click')
      expect(wrapper.text()).not.toContain('Private details 1')
    } finally { wrapper.unmount() }
  })

  it('does not close new details when an old mark-read-and-close request completes', async () => {
    const pending = deferred<{ message: string }>()
    api.markRead.mockReturnValue(pending.promise)
    const store = useAnnouncementStore()
    store.announcements = [announcement(1)]
    const wrapper = mount(AnnouncementBell, { global: { stubs: bellStubs } })
    try {
      await wrapper.get('[aria-label="announcements.title"]').trigger('click')
      await wrapper.get('h3').trigger('click')
      await wrapper.findAll('button').find(button => button.text() === 'announcements.markRead')!.trigger('click')
      changeIdentity('switch')
      store.reset()
      store.announcements = [announcement(2, true)]
      await nextTick()
      await wrapper.get('[aria-label="announcements.title"]').trigger('click')
      await wrapper.get('h3').trigger('click')
      pending.resolve({ message: 'ok' })
      await flushPromises()

      expect(wrapper.text()).toContain('Private details 2')
      expect(feedback.showSuccess).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it.each(['success', 'failure'] as const)('keeps current-identity mark-all %s feedback', async outcome => {
    if (outcome === 'failure') api.markRead.mockRejectedValue(new Error('current failure'))
    useAnnouncementStore().announcements = [announcement(1)]
    const wrapper = mount(AnnouncementBell, { global: { stubs: bellStubs } })
    const errorLog = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    try {
      await wrapper.get('[aria-label="announcements.title"]').trigger('click')
      await wrapper.findAll('button').find(button => button.text() === 'announcements.markAllRead')!.trigger('click')
      await flushPromises()
      if (outcome === 'success') expect(feedback.showSuccess).toHaveBeenCalledWith('announcements.allMarkedAsRead')
      else expect(feedback.showError).toHaveBeenCalledWith('current failure')
    } finally { wrapper.unmount(); errorLog.mockRestore() }
  })
})
