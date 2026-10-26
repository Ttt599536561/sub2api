<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { QQOrder } from '@/api/qqImage'
import { qqArtifactState, qqOrderInProgress, type QQGalleryMedia } from '@/composables/useQQGalleryMedia'

const props = defineProps<{ order: QQOrder; media: QQGalleryMedia; now: number; busy: boolean; loading: boolean; cancelConfirm: boolean; error: string }>()
const emit = defineEmits<{ close: []; cancel: []; refresh: []; confirm: [value: boolean] }>()
const { t, te } = useI18n()
const panel = ref<HTMLElement>(), copied = ref(''), copyFailed = ref(false)
const availability = computed(() => qqArtifactState(props.order, props.now))
const image = computed(() => props.media.entries.get(props.order.id))
const freeResult = computed(() => availability.value === 'available' && ['released', 'refunded'].includes(props.order.funds_status))
const canResend = computed(() => availability.value === 'available' && props.order.funds_status === 'captured')
const state = (kind: string, value: string) => { const key = `qqImage.${kind}.${value}`; return !te || te(key) ? t(key) : t(`qqImage.${kind}.unknown`) }
let originalFocus: HTMLElement | null = null, previousOverflow = ''
watch(() => props.order, (order, previous) => {
  if (previous && previous.id !== order.id) props.media.release(previous.id, 'detail')
  props.media.update(order); props.media.retain(order, 'detail', true)
}, { immediate: true })
function keyboard(event: KeyboardEvent) {
  if (event.key === 'Escape' && !props.busy) { event.preventDefault(); props.cancelConfirm ? emit('confirm', false) : emit('close'); return }
  if (event.key !== 'Tab') return
  const targets = [...(panel.value?.querySelectorAll<HTMLElement>('button:not([disabled]), a[href], input, select, textarea, [tabindex="0"]') || [])]
  const first = targets[0], last = targets[targets.length - 1]
  if (!first) { event.preventDefault(); panel.value?.focus(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.value)) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}
async function copy(text: string, label: string) {
  copied.value = ''; copyFailed.value = false
  try { await navigator.clipboard.writeText(text); copied.value = label }
  catch { copyFailed.value = true }
}
function copyResend() {
  if (props.order.funds_status === 'captured' && qqArtifactState(props.order) === 'available') void copy(`/重发 ${props.order.id}`, t('qqGallery.resendCommand'))
}
onMounted(async () => {
  originalFocus = document.activeElement as HTMLElement | null
  previousOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden'
  document.addEventListener('keydown', keyboard)
  await nextTick(); panel.value?.focus()
})
onBeforeUnmount(() => {
  props.media.release(props.order.id, 'detail'); document.removeEventListener('keydown', keyboard)
  document.body.style.overflow = previousOverflow
  if (originalFocus?.isConnected) originalFocus.focus()
})
</script>

<template>
  <div class="qq-gallery-overlay" @click.self="!busy && emit('close')">
    <section ref="panel" class="qq-gallery-detail" role="dialog" aria-modal="true" aria-labelledby="qq-gallery-detail-title" tabindex="-1">
      <header class="qq-gallery-detail-header"><div><p class="qq-gallery-eyebrow">{{ t('qqGallery.workDetails') }}</p><h2 id="qq-gallery-detail-title">{{ t('qqImage.order') }} {{ order.id }}</h2></div><div class="qq-actions"><button data-test="refresh-detail" class="qq-link" :disabled="busy || loading" @click="emit('refresh')">{{ t('qqImage.refresh') }}</button><button type="button" data-test="close-detail" class="qq-gallery-close" :aria-label="t('qqGallery.close')" :disabled="busy" @click="emit('close')">×</button></div></header>
      <div class="qq-gallery-detail-scroll">
        <div class="qq-gallery-full-image">
          <img v-if="image?.url && availability === 'available'" :src="image.url" :alt="order.prompt || t('qqImage.imageAlt')" referrerpolicy="no-referrer" />
          <div v-else class="qq-gallery-placeholder">
            <svg class="qq-gallery-image-icon" viewBox="0 0 48 48" fill="none" aria-hidden="true"><rect x="7" y="7" width="34" height="34" rx="8" stroke="currentColor" stroke-width="1.5"/><circle cx="18" cy="18" r="3" stroke="currentColor" stroke-width="1.5"/><path d="m8 34 10-10 8 8 6-6 9 9" stroke="currentColor" stroke-width="1.5"/></svg>
            <p v-if="availability === 'expired'">{{ t('qqImage.imageExpired') }}</p>
            <p v-else-if="image?.state === 'loading' || image?.state === 'queued'" role="status">{{ t('qqGallery.loadingImage') }}</p>
            <template v-else-if="availability === 'available'"><p>{{ image?.state === 'error' ? t('qqGallery.previewFailed') : t('qqGallery.clickToLoad') }}</p><button data-test="load-image" class="btn btn-secondary" @click="media.retain(order, 'detail', true)">{{ t('qqImage.loadImage') }}</button></template>
            <p v-else>{{ qqOrderInProgress(order) ? state('taskStates', order.status) : t('qqGallery.noImage') }}</p>
          </div>
        </div>
        <div class="qq-gallery-detail-content">
          <p v-if="error" class="qq-error" role="alert">{{ t(`qqImage.errors.${error}`) }}</p>
          <div v-if="image?.url && availability === 'available'" class="qq-actions"><a data-test="download-image" :href="image.url" :download="`qq-image-${order.id}`" class="btn btn-primary">{{ t('qqImage.download') }} <span aria-hidden="true">↓</span></a><span class="qq-muted">{{ t('qqGallery.originalImage') }}</span></div>
          <p v-if="freeResult" class="qq-notice">{{ t('qqGallery.freeResult') }}</p>
          <section><div class="qq-gallery-section-heading"><h3>{{ t('qqGallery.prompt') }}</h3><button class="qq-link" @click="copy(order.prompt, t('qqGallery.prompt'))">{{ t('qqGallery.copyPrompt') }}</button></div><p class="qq-gallery-prompt">{{ order.prompt }}</p></section>
          <dl class="qq-gallery-detail-meta"><div><dt>{{ t('qqImage.task') }}</dt><dd>{{ state('taskStates', order.status) }}</dd></div><div><dt>{{ t('qqImage.funds') }}</dt><dd>{{ state('fundsStates', order.funds_status) }}</dd></div><div><dt>{{ t('qqImage.delivery') }}</dt><dd>{{ state('deliveryStates', order.delivery_status) }}</dd></div></dl>
          <dl class="qq-gallery-detail-meta"><div><dt>{{ t('qqGallery.quote') }}</dt><dd>{{ order.price }} USD</dd></div><div><dt>{{ t('qqImage.model') }}</dt><dd>{{ order.model }}</dd></div><div><dt>{{ t('qqImage.size') }}</dt><dd>{{ order.size }}</dd></div><div><dt>{{ t('qqGallery.created') }}</dt><dd>{{ new Date(order.created_at).toLocaleString() }}</dd></div><div><dt>{{ t('qqImage.deadline') }}</dt><dd>{{ new Date(order.deadline).toLocaleString() }}</dd></div><div v-if="order.artifact_expires_at"><dt>{{ t('qqImage.retention') }}</dt><dd>{{ new Date(order.artifact_expires_at).toLocaleString() }}</dd></div></dl>
          <p class="qq-muted">{{ t('qqGallery.quoteHint') }} {{ t('qqImage.settlementHint') }}</p>
          <p v-if="order.error" class="qq-notice">{{ t('qqImage.orderProblem') }}</p>
          <section class="qq-gallery-command">
            <h3>{{ t(canResend ? 'qqGallery.qqActions' : 'qqGallery.orderActions') }}</h3>
            <template v-if="canResend"><p class="qq-muted">{{ t('qqGallery.resendHint') }}</p><code>/重发 {{ order.id }}</code></template>
            <p v-else class="qq-muted">{{ t(freeResult ? 'qqGallery.resendSiteOnly' : 'qqGallery.resendUnavailable') }}</p>
            <div class="qq-actions"><button v-if="canResend" data-test="copy-resend" class="btn btn-secondary" @click="copyResend">{{ t('qqGallery.copyResend') }}</button><button class="qq-link" @click="copy(order.id, t('qqImage.order'))">{{ t('qqGallery.copyOrder') }}</button></div>
          </section>
          <p v-if="copied" class="qq-muted" role="status">{{ t('qqGallery.copied', { value: copied }) }}</p><p v-if="copyFailed" class="qq-error" role="alert">{{ t('qqGallery.copyFailed') }}</p>
          <div v-if="order.status === 'queued'" class="space-y-3"><button data-test="cancel-order" class="btn btn-secondary" :disabled="busy || loading" @click="emit('confirm', true)">{{ t('qqImage.cancelOrder') }}</button><div v-if="cancelConfirm" class="qq-notice space-y-3" role="alertdialog" :aria-label="t('qqImage.cancelOrder')"><p>{{ t('qqImage.cancelHint') }}</p><div class="qq-actions"><button data-test="confirm-cancel" class="btn btn-primary" :disabled="busy" @click="emit('cancel')">{{ t('qqImage.confirmCancel') }}</button><button class="btn btn-secondary" :disabled="busy" @click="emit('confirm', false)">{{ t('qqImage.back') }}</button></div></div></div>
          <p v-else-if="qqOrderInProgress(order)" class="qq-notice">{{ t('qqImage.dispatchedHint') }}</p>
        </div>
      </div>
    </section>
  </div>
</template>
