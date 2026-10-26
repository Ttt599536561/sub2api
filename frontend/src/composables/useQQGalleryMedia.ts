import { reactive } from 'vue'
import type { QQOrder } from '@/api/qqImage'

export function qqArtifactState(order: QQOrder, now = Date.now()): 'available' | 'pending' | 'expired' {
  const expires = Date.parse(order.artifact_expires_at || '')
  if (order.website_status === 'expired' || (Number.isFinite(expires) && expires <= now)) return 'expired'
  if (!Number.isFinite(expires)) return 'pending'
  if (order.website_status === 'available') return 'available'
  // Old servers have no website_status. Only known successful states are safe fallbacks.
  if (order.website_status === undefined && ['succeeded', 'completed'].includes(order.status)) return 'available'
  return 'pending'
}
export const qqOrderInProgress = (order: QQOrder) => ['queued', 'dispatching', 'submitted', 'generating', 'reconciling'].includes(order.status)
export interface QQGalleryMediaEntry {
  url: string
  state: 'queued' | 'loading' | 'ready' | 'deferred' | 'error'
  bytes: number
}
interface RequestEntry {
  order: QQOrder
  owners: Set<string>
  controller: AbortController
  display: QQGalleryMediaEntry
}
const maxBytes = 64 * 1024 * 1024
const maxImageBytes = 20 * 1024 * 1024

/** Page-owned private originals: viewport demand only, no persistent browser storage. */
export function createQQGalleryMedia(options: { fetch: (id: string, signal: AbortSignal) => Promise<Blob>; current: () => boolean; onUnauthorized?: () => void }) {
  const entries = reactive(new Map<string, QQGalleryMediaEntry>())
  const requests = new Map<string, RequestEntry>()
  let active = 0, bytes = 0, cached = 0, disposed = false
  function remove(id: string) {
    const entry = requests.get(id)
    if (!entry) return
    entry.controller.abort()
    if (entry.display.url) {
      URL.revokeObjectURL(entry.display.url)
      bytes -= entry.display.bytes; cached--
    }
    requests.delete(id); entries.delete(id)
  }
  function clear() { for (const id of requests.keys()) remove(id) }
  function valid(entry: RequestEntry) {
    return !disposed && !entry.controller.signal.aborted && requests.get(entry.order.id) === entry && options.current() && qqArtifactState(entry.order) === 'available'
  }
  async function run(entry: RequestEntry) {
    active++; entry.display.state = 'loading'
    try {
      const blob = await options.fetch(entry.order.id, entry.controller.signal)
      if (!valid(entry)) return
      if (!['image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(blob.type) || blob.size > maxImageBytes) throw new Error('Invalid image')
      if (cached >= 12 || bytes + blob.size > maxBytes) { entry.display.state = 'deferred'; return }
      entry.display.url = URL.createObjectURL(blob)
      entry.display.bytes = blob.size; bytes += blob.size; cached++
      entry.display.state = 'ready'
    } catch (error) {
      if (valid(entry)) {
        entry.display.state = 'error'
        const value = error as { status?: number; code?: string }
        if (value?.status === 401 || value?.code === 'AUTH_SESSION_CHANGED') { clear(); options.onUnauthorized?.() }
      }
    } finally {
      active--
      if (requests.get(entry.order.id) === entry && !valid(entry)) remove(entry.order.id)
      pump()
    }
  }
  function pump() {
    if (disposed || !options.current()) return
    for (const entry of requests.values()) {
      if (active >= 2) break
      if (entry.display.state !== 'queued') continue
      if (!valid(entry)) { remove(entry.order.id); continue }
      // Do not download an original if even the image-count budget is already full.
      if (cached >= 12) { entry.display.state = 'deferred'; continue }
      void run(entry)
    }
  }
  function retain(order: QQOrder, owner: string, retry = false) {
    if (disposed || !options.current() || qqArtifactState(order) !== 'available') return
    if (retry && owner === 'detail' && !requests.get(order.id)?.display.url) {
      for (const [id, candidate] of requests) {
        if (cached < 12 && bytes + maxImageBytes <= maxBytes) break
        if (id !== order.id && candidate.display.url && !candidate.owners.has('detail')) remove(id)
      }
    }
    let entry = requests.get(order.id)
    if (!entry) {
      const display = reactive<QQGalleryMediaEntry>({ url: '', state: 'queued', bytes: 0 })
      entry = { order, owners: new Set(), controller: new AbortController(), display }
      requests.set(order.id, entry); entries.set(order.id, display)
    }
    entry.order = order; entry.owners.add(owner)
    if (retry && ['deferred', 'error'].includes(entry.display.state)) entry.display.state = 'queued'
    pump()
  }
  function release(id: string, owner: string) {
    const entry = requests.get(id)
    entry?.owners.delete(owner)
    if (entry && entry.owners.size === 0) remove(id)
  }
  function update(order: QQOrder) {
    const entry = requests.get(order.id)
    if (!entry) return
    entry.order = order
    if (qqArtifactState(order) !== 'available') remove(order.id)
  }
  const timer = setInterval(() => {
    if (!options.current()) { clear(); return }
    for (const [id, entry] of requests) if (qqArtifactState(entry.order) !== 'available') remove(id)
  }, 250)
  function dispose() { disposed = true; clearInterval(timer); clear() }
  return { entries, retain, release, update, clear, dispose }
}
export type QQGalleryMedia = ReturnType<typeof createQQGalleryMedia>
