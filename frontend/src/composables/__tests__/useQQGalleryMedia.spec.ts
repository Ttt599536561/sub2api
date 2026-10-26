import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { QQOrder } from '@/api/qqImage'
import { createQQGalleryMedia, qqArtifactState } from '../useQQGalleryMedia'

const now = Date.parse('2026-10-26T00:00:00Z')
function order(id = '1', extra: Partial<QQOrder> = {}): QQOrder {
  return { id, prompt: 'private prompt', price: '0.04', model: 'image', size: '1024x1024', status: 'succeeded', funds_status: 'captured', delivery_status: 'sent', created_at: new Date(now).toISOString(), deadline: new Date(now + 60000).toISOString(), artifact_expires_at: new Date(now + 60000).toISOString(), website_status: 'available', ...extra }
}
const blob = () => new Blob(['private'], { type: 'image/png' })
let media: ReturnType<typeof createQQGalleryMedia>
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve() }
beforeEach(() => {
  vi.useFakeTimers(); vi.setSystemTime(now)
  let count = 0
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => `blob:private-${++count}`), revokeObjectURL: vi.fn() }))
})
afterEach(() => { media?.dispose(); vi.useRealTimers(); vi.unstubAllGlobals() })
describe('private QQ gallery media', () => {
  it('uses website availability for late free images without inventing success', () => {
    expect(qqArtifactState(order('1', { status: 'closed_unknown', funds_status: 'released' }), now)).toBe('available')
    expect(qqArtifactState(order('1', { website_status: 'pending' }), now)).toBe('pending')
    expect(qqArtifactState(order('1', { website_status: undefined, status: 'mystery' }), now)).toBe('pending')
    expect(qqArtifactState(order('1', { website_status: undefined }), now)).toBe('available')
    expect(qqArtifactState(order('1', { artifact_expires_at: new Date(now).toISOString() }), now)).toBe('expired')
  })
  it('loads only requested visible images with at most two requests in flight', async () => {
    const pending: Array<(value: Blob) => void> = []
    const fetch = vi.fn(() => new Promise<Blob>(resolve => pending.push(resolve)))
    media = createQQGalleryMedia({ fetch, current: () => true })
    expect(fetch).not.toHaveBeenCalled()
    for (let i = 0; i < 5; i++) media.retain(order(String(i)), `card-${i}`)
    expect(fetch).toHaveBeenCalledTimes(2)
    pending[0](blob()); await flush()
    expect(fetch).toHaveBeenCalledTimes(3)
    expect(media.entries.get('0')?.url).toBe('blob:private-1')
  })
  it('aborts offscreen requests, discards late blobs and revokes cached URLs', async () => {
    let finish!: (value: Blob) => void
    const fetch = vi.fn((_id: string, _signal: AbortSignal) => new Promise<Blob>(resolve => { finish = resolve }))
    media = createQQGalleryMedia({ fetch, current: () => true })
    media.retain(order(), 'card'); media.release('1', 'card')
    expect(fetch.mock.calls[0][1].aborted).toBe(true)
    finish(blob()); await flush()
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    media.retain(order(), 'detail'); finish(blob()); await flush()
    media.release('1', 'detail')
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:private-1')
  })
  it('expires loaded media on the clock and clears private data on session replacement', async () => {
    let current = true
    media = createQQGalleryMedia({ fetch: async () => blob(), current: () => current })
    media.retain(order('1', { artifact_expires_at: new Date(now + 1200).toISOString() }), 'card'); await flush()
    await vi.advanceTimersByTimeAsync(1500)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:private-1')
    expect(media.entries.get('1')?.url || '').toBe('')
    media.retain(order('2'), 'card'); await flush()
    current = false; await vi.advanceTimersByTimeAsync(1000)
    expect(media.entries.size).toBe(0)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:private-2')
  })
  it('limits memory to 12 URLs and 64 MiB and rejects originals above 20 MiB', async () => {
    const large = new Blob([new Uint8Array(17 * 1024 * 1024)], { type: 'image/png' })
    media = createQQGalleryMedia({ fetch: async () => large, current: () => true })
    for (let i = 0; i < 8; i++) media.retain(order(String(i)), 'card')
    await flush(); await flush()
    expect([...media.entries.values()].filter(entry => entry.url).length).toBe(3)
    media.dispose()
    media = createQQGalleryMedia({ fetch: async () => blob(), current: () => true })
    for (let i = 0; i < 18; i++) media.retain(order(String(i)), 'card')
    await flush(); await flush(); await flush()
    expect([...media.entries.values()].filter(entry => entry.url).length).toBe(12)
    media.dispose()
    media = createQQGalleryMedia({ fetch: async () => new Blob([new Uint8Array(21 * 1024 * 1024)], { type: 'image/png' }), current: () => true })
    media.retain(order(), 'detail'); await flush()
    expect(media.entries.get('1')?.state).toBe('error')
    expect(media.entries.get('1')?.url || '').toBe('')
  })
  it('never creates a URL from a late response after disposal or session change', async () => {
    let finish!: (value: Blob) => void
    media = createQQGalleryMedia({ fetch: () => new Promise(resolve => { finish = resolve }), current: () => true })
    media.retain(order(), 'detail'); media.dispose(); finish(blob()); await flush()
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })
  it('makes room for an explicitly opened detail within the same memory limit', async () => {
    const large = new Blob([new Uint8Array(17 * 1024 * 1024)], { type: 'image/png' })
    media = createQQGalleryMedia({ fetch: async () => large, current: () => true })
    for (let i = 0; i < 4; i++) media.retain(order(String(i)), `card-${i}`)
    await flush(); await flush()
    expect(media.entries.get('3')?.state).toBe('deferred')
    media.retain(order('3'), 'detail', true); await flush()
    expect(media.entries.get('3')?.state).toBe('ready')
    expect([...media.entries.values()].reduce((sum, entry) => sum + entry.bytes, 0)).toBeLessThanOrEqual(64 * 1024 * 1024)
    expect(URL.revokeObjectURL).toHaveBeenCalled()
  })
})
