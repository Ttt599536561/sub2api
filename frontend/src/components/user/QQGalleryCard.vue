<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { QQOrder } from '@/api/qqImage'
import { qqArtifactState, qqOrderInProgress, type QQGalleryMedia } from '@/composables/useQQGalleryMedia'

const props = defineProps<{ order: QQOrder; media: QQGalleryMedia; now: number }>()
const emit = defineEmits<{ open: [id: string] }>()
const { t, te } = useI18n()
const element = ref<HTMLElement>(), visible = ref(false)
const availability = computed(() => qqArtifactState(props.order, props.now))
const image = computed(() => props.media.entries.get(props.order.id))
const taskLabel = computed(() => {
  const key = `qqImage.taskStates.${props.order.status}`
  return !te || te(key) ? t(key) : t('qqImage.taskStates.unknown')
})
const placeholder = computed(() => availability.value === 'expired' ? t('qqGallery.expired') : availability.value === 'available' ? t('qqGallery.ready') : qqOrderInProgress(props.order) ? taskLabel.value : t('qqGallery.noImage'))
let observer: IntersectionObserver | undefined
watch([visible, () => props.order, availability], () => {
  props.media.update(props.order)
  if (visible.value && availability.value === 'available') props.media.retain(props.order, `card-${props.order.id}`)
  else props.media.release(props.order.id, `card-${props.order.id}`)
})
onMounted(() => {
  if (!('IntersectionObserver' in window) || !element.value) return
  observer = new IntersectionObserver(entries => { visible.value = entries.some(entry => entry.isIntersecting) }, { rootMargin: '0px', threshold: 0.01 })
  observer.observe(element.value)
})
onBeforeUnmount(() => { observer?.disconnect(); props.media.release(props.order.id, `card-${props.order.id}`) })
</script>

<template>
  <article ref="element" class="qq-gallery-card" data-test="gallery-card">
    <a :href="`/qq/orders/${encodeURIComponent(order.id)}`" class="qq-gallery-card-link" data-test="open-order" :aria-label="`${t('qqImage.details')}: ${order.prompt}`" @click.prevent="emit('open', order.id)">
      <div class="qq-gallery-cover" :class="{ 'qq-gallery-cover-pending': qqOrderInProgress(order) }">
        <img v-if="image?.url && availability === 'available'" :src="image.url" :alt="order.prompt || t('qqImage.imageAlt')" decoding="async" referrerpolicy="no-referrer" />
        <div v-else class="qq-gallery-placeholder">
          <svg class="qq-gallery-image-icon" viewBox="0 0 48 48" fill="none" aria-hidden="true"><rect x="7" y="7" width="34" height="34" rx="8" stroke="currentColor" stroke-width="1.5"/><circle cx="18" cy="18" r="3" stroke="currentColor" stroke-width="1.5"/><path d="m8 34 10-10 8 8 6-6 9 9" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>
          <span v-if="image?.state === 'loading' || image?.state === 'queued'" role="status">{{ t('qqGallery.loadingImage') }}</span>
          <span v-else-if="image?.state === 'error'">{{ t('qqGallery.previewFailed') }}</span>
          <span v-else>{{ placeholder }}</span>
          <small v-if="availability === 'available' && image?.state !== 'loading'">{{ t('qqGallery.openToView') }}</small>
        </div>
        <span class="qq-gallery-badge" :class="{ 'qq-gallery-badge-ready': availability === 'available' }">{{ availability === 'available' ? t('qqGallery.ready') : taskLabel }}</span>
        <span v-if="image?.url" class="qq-gallery-view-label">{{ t('qqGallery.viewWork') }} <span aria-hidden="true">↗</span></span>
      </div>
      <div class="qq-gallery-card-body">
        <h2>{{ order.prompt || t('qqGallery.untitled') }}</h2>
        <div class="qq-gallery-card-meta"><time :datetime="order.created_at">{{ new Date(order.created_at).toLocaleDateString() }}</time><span>{{ t('qqGallery.quote') }} {{ order.price }} USD</span></div>
      </div>
    </a>
  </article>
</template>
