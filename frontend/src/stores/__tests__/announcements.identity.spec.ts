import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAnnouncementStore } from '@/stores/announcements'
import type { UserAnnouncement } from '@/types'

const api = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: api }))

function announcement(id: number): UserAnnouncement {
  return { id, title: `Announcement ${id}`, content: '', notify_mode: 'popup', created_at: '', updated_at: '' }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((success, failure) => { resolve = success; reject = failure })
  return { promise, resolve, reject }
}

describe('announcement identity isolation', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.resetAllMocks()
    api.markRead.mockResolvedValue({ message: 'ok' })
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
  })

  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

  it('reports a failed explicit mark-read without marking the announcement read', async () => {
    const error = new Error('mark read failed')
    api.markRead.mockRejectedValueOnce(error)
    const store = useAnnouncementStore()
    store.announcements = [announcement(1)]

    await expect(store.markAsRead(1)).rejects.toBe(error)
    expect(store.unreadCount).toBe(1)
  })

  it('can dismiss a popup even when its background mark-read fails', async () => {
    vi.useFakeTimers()
    api.list.mockResolvedValueOnce([announcement(1), announcement(2)])
    api.markRead.mockRejectedValueOnce(new Error('background mark read failed'))
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()

    await expect(store.dismissPopup()).resolves.toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup?.id).toBe(2)
    expect(store.announcements[0].read_at).toBeUndefined()
  })

  it('does not replace the new identity announcements or popup queue with an old response', async () => {
    const old = deferred<UserAnnouncement[]>()
    api.list.mockReturnValueOnce(old.promise).mockResolvedValueOnce([announcement(8)])
    const store = useAnnouncementStore()
    const oldRequest = store.fetchAnnouncements()
    store.reset()
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(2)
    old.resolve([announcement(7)])
    await oldRequest

    expect(store.announcements.map(item => item.id)).toEqual([8])
    expect(store.currentPopup?.id).toBe(8)
    await store.dismissPopup()
    expect(store.currentPopup).toBeNull()
  })

  it.each(['success', 'failure'] as const)('keeps the new fetch loading and throttle after an old %s', async (outcome) => {
    const old = deferred<UserAnnouncement[]>()
    const fresh = deferred<UserAnnouncement[]>()
    api.list.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const store = useAnnouncementStore()
    const oldRequest = store.fetchAnnouncements()
    store.reset()
    const newRequest = store.fetchAnnouncements()
    if (outcome === 'success') old.resolve([announcement(7)])
    else old.reject(new Error('old identity request failed'))
    await oldRequest

    expect(store.loading).toBe(true)
    expect(store.announcements).toEqual([])
    fresh.resolve([announcement(8)])
    await newRequest
    expect(store.loading).toBe(false)
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('does not mark the same announcement read for a new identity from an old mutation', async () => {
    const old = deferred<{ message: string }>()
    api.list.mockResolvedValueOnce([announcement(1)]).mockResolvedValueOnce([announcement(1)])
    api.markRead.mockReturnValueOnce(old.promise)
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    const oldMutation = store.markAsRead(1)
    store.reset()
    await store.fetchAnnouncements()
    old.resolve({ message: 'ok' })
    await oldMutation
    expect(store.announcements[0].read_at).toBeUndefined()

    await store.markAsRead(1)
    expect(store.announcements[0].read_at).toBeDefined()
  })

  it.each(['success', 'failure'] as const)('keeps a forced refresh loading after a superseded same-session %s', async (outcome) => {
    const old = deferred<UserAnnouncement[]>()
    const fresh = deferred<UserAnnouncement[]>()
    api.list.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const store = useAnnouncementStore()
    const oldRequest = store.fetchAnnouncements()
    const newRequest = store.fetchAnnouncements(true)
    if (outcome === 'success') old.resolve([announcement(7)])
    else old.reject(new Error('superseded request failed'))
    await oldRequest

    expect(store.loading).toBe(true)
    expect(store.announcements).toEqual([])
    expect(store.currentPopup).toBeNull()
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(2)
    fresh.resolve([announcement(8)])
    await newRequest
    expect(store.loading).toBe(false)
    expect(store.announcements.map(item => item.id)).toEqual([8])
    expect(store.currentPopup?.id).toBe(8)
  })

  it('keeps a pending mark-read valid after a forced refresh in the same session', async () => {
    const pending = deferred<{ message: string }>()
    api.list.mockResolvedValueOnce([announcement(1)]).mockResolvedValueOnce([announcement(1), announcement(2)])
    api.markRead.mockReturnValueOnce(pending.promise)
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    const mutation = store.markAsRead(1)
    await store.fetchAnnouncements(true)
    pending.resolve({ message: 'ok' })
    await mutation

    expect(store.announcements[0].read_at).toBeDefined()
    expect(store.announcements[1].read_at).toBeUndefined()
    expect(store.unreadCount).toBe(1)
  })

  it.each(['success', 'failure'] as const)('isolates an old mark-all %s from new reads and loading', async (outcome) => {
    const old = deferred<{ message: string }>()
    const fresh = deferred<UserAnnouncement[]>()
    api.list.mockResolvedValueOnce([announcement(1)]).mockResolvedValueOnce([announcement(8)]).mockReturnValueOnce(fresh.promise)
    api.markRead.mockReturnValueOnce(old.promise)
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    const oldMutation = store.markAllAsRead()
    store.reset()
    await store.fetchAnnouncements()
    const newRequest = store.fetchAnnouncements(true)
    if (outcome === 'success') {
      old.resolve({ message: 'ok' })
      await oldMutation
    } else {
      old.reject(new Error('old identity mark-all failed'))
      await expect(oldMutation).rejects.toThrow('old identity mark-all failed')
    }

    expect(store.announcements[0].read_at).toBeUndefined()
    expect(store.loading).toBe(true)
    fresh.resolve([announcement(8)])
    await newRequest
    await store.markAllAsRead()
    expect(store.unreadCount).toBe(0)
  })

  it('does not let an old dismiss timer advance the new identity popup queue', async () => {
    vi.useFakeTimers()
    api.list.mockResolvedValueOnce([announcement(1), announcement(2)]).mockResolvedValueOnce([announcement(3), announcement(4)])
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    await store.dismissPopup()
    store.reset()
    await store.fetchAnnouncements()
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup?.id).toBe(3)

    await store.dismissPopup()
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup?.id).toBe(4)
  })

  it.each(['success', 'mixed failure'] as const)('isolates an old batch %s from a new batch with the same IDs', async (outcome) => {
    const oldFirst = deferred<{ message: string }>()
    const oldSecond = deferred<{ message: string }>()
    const freshFirst = deferred<{ message: string }>()
    const freshSecond = deferred<{ message: string }>()
    api.markRead
      .mockReturnValueOnce(oldFirst.promise)
      .mockReturnValueOnce(oldSecond.promise)
      .mockReturnValueOnce(freshFirst.promise)
      .mockReturnValueOnce(freshSecond.promise)
    const store = useAnnouncementStore()
    store.announcements = [announcement(1), announcement(2)]
    const oldMutation = store.markAllAsRead().catch(err => err)
    store.reset()
    store.announcements = [announcement(1), announcement(2)]
    let freshFinished = false
    const freshMutation = store.markAllAsRead().then(() => { freshFinished = true })
    const oldError = new Error('old identity partial failure')

    oldFirst.resolve({ message: 'ok' })
    if (outcome === 'success') oldSecond.resolve({ message: 'ok' })
    else oldSecond.reject(oldError)
    expect(await oldMutation).toBe(outcome === 'success' ? undefined : oldError)

    expect(store.unreadCount).toBe(2)
    expect(store.loading).toBe(true)
    expect(freshFinished).toBe(false)
    expect(api.markRead.mock.calls).toEqual([[1], [2], [1], [2]])
    freshFirst.resolve({ message: 'ok' })
    freshSecond.resolve({ message: 'ok' })
    await freshMutation
    expect(store.unreadCount).toBe(0)
    expect(store.loading).toBe(false)
    expect(api.markRead.mock.calls).toEqual([[1], [2], [1], [2]])
  })

  it('only marks the submitted IDs read when a refresh adds announcements during mark-all', async () => {
    const pending = deferred<{ message: string }>()
    api.list.mockResolvedValueOnce([announcement(1)]).mockResolvedValueOnce([announcement(1), announcement(2)])
    api.markRead.mockReturnValueOnce(pending.promise)
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    const mutation = store.markAllAsRead()
    await store.fetchAnnouncements(true)
    expect(api.markRead.mock.calls.map(([id]) => id)).toEqual([1])
    pending.resolve({ message: 'ok' })
    await mutation

    expect(store.announcements[0].read_at).toBeDefined()
    expect(store.announcements[1].read_at).toBeUndefined()
    expect(store.unreadCount).toBe(1)
  })
})
