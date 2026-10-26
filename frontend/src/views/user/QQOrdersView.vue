<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import QQGalleryCard from '@/components/user/QQGalleryCard.vue'
import QQGalleryDetail from '@/components/user/QQGalleryDetail.vue'
import { qqImageAPI, type QQOrder } from '@/api/qqImage'
import { useQQSession } from '@/composables/useQQSession'
import { createQQGalleryMedia, qqArtifactState, qqOrderInProgress, type QQGalleryMedia } from '@/composables/useQQGalleryMedia'
import { qqErrorKey } from '@/utils/qqImage'
import '@/components/user/qqImage.css'
import '@/components/user/qqGallery.css'

const { t } = useI18n(), route = useRoute(), router = useRouter()
const orders = ref<QQOrder[]>([]), order = ref<QQOrder | null>(null), loading = ref(false), busy = ref(false), error = ref(''), cancelConfirm = ref(false)
const query = ref(''), filter = ref('all'), now = ref(Date.now()), imageEnabled = ref(true), sessionExpired = ref(false)
let readSequence = 0, pageRevision = 0, pollCount = 0
const listLoaded = ref(false), resumeRead = ref(false)
let readController: AbortController | undefined, pollTimer: ReturnType<typeof setTimeout> | undefined, media: QQGalleryMedia
function stopPoll() { if (pollTimer) clearTimeout(pollTimer); pollTimer = undefined }
function abortRead() { readSequence++; readController?.abort(); readController = undefined; loading.value = false }
function clear() {
  pageRevision++; abortRead(); stopPoll(); media?.clear(); resumeRead.value = false
  orders.value = []; order.value = null; loading.value = false; busy.value = false; error.value = ''; cancelConfirm.value = false; query.value = ''; filter.value = 'all'
}
const session = useQQSession(clear)
function current() { return !sessionExpired.value && session.current() }
function handleError(e: unknown) {
  const value = e as { status?: number; code?: string }
  if (value?.status === 401 || value?.code === 'AUTH_SESSION_CHANGED') { sessionExpired.value = true; clear(); return }
  error.value = qqErrorKey(e)
}
media = createQQGalleryMedia({ fetch: qqImageAPI.image, current, onUnauthorized: () => { sessionExpired.value = true; clear() } })
const orderID = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const invalidated = computed(() => session.invalidated.value || sessionExpired.value)
const hasActive = computed(() => orderID.value ? !!order.value && qqOrderInProgress(order.value) : orders.value.some(qqOrderInProgress))
const filtered = computed(() => {
  const text = query.value.trim().toLocaleLowerCase()
  return orders.value.filter(item => {
    const artifact = qqArtifactState(item, now.value)
    const matchesFilter = filter.value === 'all' || (filter.value === 'available' && artifact === 'available') || (filter.value === 'active' && qqOrderInProgress(item)) || (filter.value === 'expired' && artifact === 'expired') || (filter.value === 'other' && !qqOrderInProgress(item) && artifact !== 'available' && artifact !== 'expired')
    return matchesFilter && (!text || `${item.prompt} ${item.id} ${item.model}`.toLocaleLowerCase().includes(text))
  })
})
function schedulePoll() {
  stopPoll()
  if (!current() || !hasActive.value || document.visibilityState === 'hidden' || pollCount >= 60) return
  pollTimer = setTimeout(() => { pollCount++; void refresh(false) }, 5000)
}
async function refresh(manual = true) {
  if (!current() || busy.value || loading.value) return
  if (manual) pollCount = 0
  stopPoll()
  const ticket = ++readSequence, id = orderID.value, controller = new AbortController()
  readController = controller
  const abort = () => controller.abort()
  session.signal.addEventListener('abort', abort, { once: true })
  loading.value = true; resumeRead.value = false; error.value = ''
  try {
    if (id) {
      const result = await qqImageAPI.order(id, controller.signal)
      if (!current() || ticket !== readSequence || controller.signal.aborted) return
      order.value = result; media.update(result)
      orders.value = orders.value.map(item => item.id === result.id ? result : item)
    } else {
      const result = await qqImageAPI.orders(controller.signal)
      if (!current() || ticket !== readSequence || controller.signal.aborted) return
      orders.value = (result || []).slice(0, 100); listLoaded.value = true
      for (const item of orders.value) media.update(item)
    }
    schedulePoll()
  } catch (e) { if (current() && ticket === readSequence && !controller.signal.aborted) handleError(e) }
  finally { session.signal.removeEventListener('abort', abort); if (ticket === readSequence) { loading.value = false; readController = undefined } }
}
async function cancel() {
  if (!current() || busy.value || !order.value || order.value.status !== 'queued' || !cancelConfirm.value) return
  // A confirmed mutation belongs to this route and login, independently of GET refreshes.
  // Abort any older polling read so its snapshot cannot overwrite the cancellation result.
  stopPoll(); abortRead(); resumeRead.value = false; const ticket = pageRevision
  busy.value = true; error.value = ''
  try {
    await qqImageAPI.cancel(order.value.id, session.signal)
    if (!current() || ticket !== pageRevision) return
    cancelConfirm.value = false; busy.value = false; await refresh()
  } catch (e) { if (current() && ticket === pageRevision) handleError(e) }
  finally { if (current() && ticket === pageRevision) busy.value = false }
}
function open(id: string) { void router.push(`/qq/orders/${encodeURIComponent(id)}`) }
function close() { if (!busy.value) void router.push('/qq/orders') }
watch(orderID, () => {
  pageRevision++; abortRead(); stopPoll(); resumeRead.value = false; order.value = null; cancelConfirm.value = false; busy.value = false; error.value = ''; pollCount = 0
  if (orderID.value) order.value = orders.value.find(item => item.id === orderID.value) || null
  void refresh()
}, { immediate: true })
if (current()) void qqImageAPI.status(session.signal).then(status => { if (current()) imageEnabled.value = status.image_enabled ?? status.enabled }).catch(e => { if (current() && (e as { status?: number })?.status === 401) handleError(e) })
function visibilityChanged() {
  stopPoll()
  if (document.visibilityState === 'hidden') {
    // Remember an interrupted initial read even before any active orders are known.
    if (loading.value) { resumeRead.value = true; abortRead() }
  }
  else if (current() && (resumeRead.value || (hasActive.value && pollCount < 60))) void refresh(false)
}
document.addEventListener('visibilitychange', visibilityChanged)
const clock = setInterval(() => { now.value = Date.now() }, 250)
onBeforeUnmount(() => { stopPoll(); clearInterval(clock); media.dispose(); document.removeEventListener('visibilitychange', visibilityChanged) })
</script>

<template>
  <AppLayout><div class="qq-page qq-gallery-page">
    <div :inert="order ? true : undefined">
      <header class="qq-heading qq-gallery-heading"><div><p class="qq-gallery-eyebrow">{{ t('qqGallery.eyebrow') }}</p><h1>{{ t('qqGallery.title') }}</h1><p class="qq-muted mt-2">{{ t('qqGallery.subtitle') }}</p></div><div class="qq-actions"><RouterLink to="/qq/bind" class="qq-link">{{ t('qqImage.bindTitle') }}</RouterLink><button class="btn btn-secondary" :disabled="loading || busy || invalidated" @click="refresh()">{{ t('qqImage.refresh') }}</button></div></header>
      <div v-if="invalidated" class="qq-notice" role="alert">{{ t('qqImage.sessionChanged') }}</div>
      <template v-else>
        <p v-if="!imageEnabled" class="qq-notice qq-gallery-notice">{{ t('qqGallery.paused') }}</p>
        <div v-if="error" class="qq-error qq-gallery-notice" role="alert">{{ t(`qqImage.errors.${error}`) }}</div>
        <div class="qq-gallery-toolbar"><label class="qq-gallery-search"><svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5" stroke="currentColor" stroke-width="1.5"/><path d="m16 16 4 4" stroke="currentColor" stroke-width="1.5"/></svg><input v-model="query" data-test="gallery-search" type="search" :aria-label="t('qqGallery.search')" :placeholder="t('qqGallery.search')" /></label><select v-model="filter" data-test="gallery-filter" class="qq-input qq-gallery-filter" :aria-label="t('qqGallery.filter')"><option value="all">{{ t('qqGallery.filters.all') }}</option><option value="available">{{ t('qqGallery.filters.available') }}</option><option value="active">{{ t('qqGallery.filters.active') }}</option><option value="expired">{{ t('qqGallery.filters.expired') }}</option><option value="other">{{ t('qqGallery.filters.other') }}</option></select></div>
        <div class="qq-gallery-scope"><p>{{ t('qqGallery.scope') }}</p><span v-if="listLoaded">{{ t('qqGallery.resultCount', { count: filtered.length }) }}</span></div>
        <div v-if="(loading || resumeRead) && !orders.length && !order" class="qq-gallery-grid" role="status" :aria-label="t('common.loading')"><div v-for="i in 6" :key="i" class="qq-gallery-skeleton"><div /><span /><span /></div></div>
        <div v-else-if="filtered.length" class="qq-gallery-grid"><QQGalleryCard v-for="item in filtered" :key="item.id" :order="item" :media="media" :now="now" @open="open" /></div>
        <div v-else-if="!error && !orderID" class="qq-gallery-empty"><svg viewBox="0 0 48 48" fill="none" aria-hidden="true"><rect x="8" y="10" width="32" height="30" rx="7" stroke="currentColor" stroke-width="1.5"/><path d="M17 5h14M8 32l9-9 9 9 5-5 9 9" stroke="currentColor" stroke-width="1.5"/></svg><h2>{{ orders.length ? t('qqGallery.noMatches') : t('qqImage.emptyOrders') }}</h2><p>{{ orders.length ? t('qqGallery.adjustFilters') : t('qqGallery.emptyHint') }}</p><RouterLink v-if="!orders.length" to="/qq/bind" class="btn btn-secondary">{{ t('qqImage.bindTitle') }}</RouterLink></div>
        <RouterLink v-if="orderID && !order" to="/qq/orders" class="qq-link">{{ t('qqImage.allOrders') }}</RouterLink>
      </template>
    </div>
    <QQGalleryDetail v-if="order && !invalidated" :key="order.id" :order="order" :media="media" :now="now" :busy="busy" :loading="loading" :error="error" :cancel-confirm="cancelConfirm" @close="close" @cancel="cancel" @refresh="refresh()" @confirm="cancelConfirm = $event" />
  </div></AppLayout>
</template>
