import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import type { AdminComplianceStatus } from '@/api/admin/compliance'

const api = vi.hoisted(() => ({ getStatus: vi.fn(), accept: vi.fn() }))
vi.mock('@/api/admin/compliance', () => ({ default: api }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))

function status(version: string, required: boolean): AdminComplianceStatus {
  return {
    version, required, document_path_zh: 'zh.md', document_path_en: 'en.md',
    document_url_zh: '/zh', document_url_en: '/en', ack_phrase_zh: '同意', ack_phrase_en: 'agree'
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((success, failure) => { resolve = success; reject = failure })
  return { promise, resolve, reject }
}

describe('admin compliance identity isolation', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks() })

  it.each(['fetch', 'accept'] as const)('does not replace the new identity status with an old %s result', async (operation) => {
    const old = deferred<AdminComplianceStatus>()
    const store = useAdminComplianceStore()
    if (operation === 'fetch') api.getStatus.mockReturnValueOnce(old.promise)
    else api.accept.mockReturnValueOnce(old.promise)
    const oldRequest = operation === 'fetch' ? store.fetchStatus() : store.accept('agree')
    store.reset()
    api.getStatus.mockResolvedValueOnce(status('B', true))
    await store.fetchStatus()
    old.resolve(status('A', false))
    await expect(oldRequest).resolves.toEqual(status('A', false))

    expect(store.status).toEqual(status('B', true))
    expect(store.initialized).toBe(true)
    expect(store.shouldShow).toBe(true)
  })

  it.each(['success', 'failure'] as const)('does not clear a new fetch loading flag after an old %s', async (outcome) => {
    const old = deferred<AdminComplianceStatus>()
    const fresh = deferred<AdminComplianceStatus>()
    api.getStatus.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const store = useAdminComplianceStore()
    const oldRequest = store.fetchStatus()
    store.reset()
    const newRequest = store.fetchStatus()
    expect(api.getStatus).toHaveBeenCalledTimes(2)
    if (outcome === 'success') {
      old.resolve(status('A', true))
      await oldRequest
    } else {
      old.reject(new Error('old fetch failed'))
      await expect(oldRequest).rejects.toThrow('old fetch failed')
    }

    expect(store.loading).toBe(true)
    expect(store.initialized).toBe(false)
    expect(store.status).toBeNull()
    expect(store.shouldShow).toBe(false)
    fresh.resolve(status('B', true))
    await expect(newRequest).resolves.toEqual(status('B', true))
    expect(store.loading).toBe(false)
    expect(store.shouldShow).toBe(true)
  })

  it.each(['success', 'failure'] as const)('does not clear a new accept submitting flag after an old %s', async (outcome) => {
    const old = deferred<AdminComplianceStatus>()
    const fresh = deferred<AdminComplianceStatus>()
    api.accept.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const store = useAdminComplianceStore()
    const oldRequest = store.accept('agree')
    store.reset()
    store.requireAcknowledgement(status('B', true))
    const newRequest = store.accept('agree')
    if (outcome === 'success') {
      old.resolve(status('A', false))
      await oldRequest
    } else {
      old.reject(new Error('old acceptance failed'))
      await expect(oldRequest).rejects.toThrow('old acceptance failed')
    }

    expect(store.submitting).toBe(true)
    expect(store.status?.version).toBe('B')
    expect(store.shouldShow).toBe(true)
    fresh.resolve(status('B', false))
    await expect(newRequest).resolves.toEqual(status('B', false))
    expect(store.submitting).toBe(false)
    expect(store.shouldShow).toBe(false)
    expect(api.accept).toHaveBeenLastCalledWith({ phrase: 'agree', language: 'en' })
  })
})
