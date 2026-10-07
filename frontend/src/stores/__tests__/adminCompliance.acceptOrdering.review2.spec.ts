import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import type { AdminComplianceStatus } from '@/api/admin/compliance'

const api = vi.hoisted(() => ({ getStatus: vi.fn(), accept: vi.fn() }))
vi.mock('@/api/admin/compliance', () => ({ default: api }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))

function status(required: boolean, version = 'v2026.06.10'): AdminComplianceStatus {
  return {
    version, required, document_path_zh: 'zh.md', document_path_en: 'en.md',
    document_url_zh: '/zh', document_url_en: '/en', ack_phrase_zh: '同意', ack_phrase_en: 'agree',
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((success, failure) => { resolve = success; reject = failure })
  return { promise, resolve, reject }
}

beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks() })

describe('admin compliance acceptance and concurrent initial status reads', () => {
  it('does not reopen mandatory acknowledgement when the second initial GET returns after acceptance', async () => {
    const appRead = deferred<AdminComplianceStatus>()
    const routeRead = deferred<AdminComplianceStatus>()
    const acceptance = deferred<AdminComplianceStatus>()
    api.getStatus.mockReturnValueOnce(appRead.promise).mockReturnValueOnce(routeRead.promise)
    api.accept.mockReturnValueOnce(acceptance.promise)
    const store = useAdminComplianceStore()

    // App.vue and the admin route guard both fetch while initialized is false.
    const appRequest = store.fetchStatus()
    const routeRequest = store.fetchStatus()
    appRead.resolve(status(true))
    await appRequest
    expect(store.shouldShow).toBe(true)
    const accepted = store.accept('agree')
    acceptance.resolve(status(false))
    await accepted
    expect(store.shouldShow).toBe(false)

    // The GET already read the unacknowledged state before the POST committed.
    routeRead.resolve(status(true))
    await routeRequest
    expect(store.status?.required).toBe(false)
    expect(store.shouldShow).toBe(false)
    expect(store.initialized).toBe(true)
    expect(store.loading).toBe(false)
  })

  it('still accepts a later explicit status refresh requiring a newer document', async () => {
    const store = useAdminComplianceStore()
    api.accept.mockResolvedValueOnce(status(false))
    await store.accept('agree')
    api.getStatus.mockResolvedValueOnce(status(true, 'v-next'))
    await store.fetchStatus()
    expect(store.status?.version).toBe('v-next')
    expect(store.shouldShow).toBe(true)
  })

  it('does not suppress a concurrent status read when acceptance fails', async () => {
    const read = deferred<AdminComplianceStatus>()
    const store = useAdminComplianceStore()
    api.getStatus.mockReturnValueOnce(read.promise)
    api.accept.mockRejectedValueOnce(new Error('accept unavailable'))
    const request = store.fetchStatus()
    await expect(store.accept('agree')).rejects.toThrow('accept unavailable')
    read.resolve(status(true))
    await request
    expect(store.shouldShow).toBe(true)
    expect(store.initialized).toBe(true)
    expect(store.loading).toBe(false)
  })
})

it('rechecks authoritative state instead of reopening for a late same-version 423', async () => {
  const store = useAdminComplianceStore()
  api.accept.mockResolvedValueOnce(status(false))
  await store.accept('agree')
  const current = deferred<AdminComplianceStatus>()
  api.getStatus.mockReturnValueOnce(current.promise)
  store.requireAcknowledgement({ version: 'v2026.06.10' })
  expect(store.shouldShow).toBe(false)
  expect(api.getStatus).toHaveBeenCalledTimes(1)
  current.resolve(status(false))
  await Promise.resolve()
  expect(store.shouldShow).toBe(false)
})

it('can reopen the same document when the authoritative recheck confirms a real revocation', async () => {
  const store = useAdminComplianceStore()
  api.accept.mockResolvedValueOnce(status(false))
  await store.accept('agree')
  const current = deferred<AdminComplianceStatus>()
  api.getStatus.mockReturnValueOnce(current.promise)
  store.requireAcknowledgement({ version: 'v2026.06.10' })
  expect(api.getStatus).toHaveBeenCalledTimes(1)
  current.resolve(status(true))
  await Promise.resolve()
  expect(store.shouldShow).toBe(true)
})

it('keeps a new required version visible if an older notification recheck returns later', async () => {
  const store = useAdminComplianceStore()
  api.accept.mockResolvedValueOnce(status(false))
  await store.accept('agree')
  const current = deferred<AdminComplianceStatus>()
  api.getStatus.mockReturnValueOnce(current.promise)
  store.requireAcknowledgement({ version: 'v2026.06.10' })
  store.requireAcknowledgement({ version: 'v-next' })
  expect(store.shouldShow).toBe(true)
  expect(store.status?.version).toBe('v-next')
  current.resolve(status(false))
  await Promise.resolve()
  expect(store.shouldShow).toBe(true)
  expect(store.status?.version).toBe('v-next')
})

it('does not let an old identity acceptance invalidate the new identity status read', async () => {
  const oldAcceptance = deferred<AdminComplianceStatus>()
  const newRead = deferred<AdminComplianceStatus>()
  const store = useAdminComplianceStore()
  api.accept.mockReturnValueOnce(oldAcceptance.promise)
  const oldRequest = store.accept('agree')
  store.reset()
  api.getStatus.mockReturnValueOnce(newRead.promise)
  const newRequest = store.fetchStatus()
  oldAcceptance.resolve(status(false))
  await oldRequest
  newRead.resolve(status(true, 'new-admin'))
  await newRequest
  expect(store.shouldShow).toBe(true)
  expect(store.status?.version).toBe('new-admin')
})
