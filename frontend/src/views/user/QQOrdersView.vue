<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import QQOrderCard from '@/components/user/QQOrderCard.vue'
import { qqImageAPI, type QQOrder } from '@/api/qqImage'
import { useQQSession } from '@/composables/useQQSession'
import { qqErrorKey } from '@/utils/qqImage'
import '@/components/user/qqImage.css'
const { t } = useI18n(), route = useRoute()
const orders = ref<QQOrder[]>([]), order = ref<QQOrder | null>(null), loading = ref(false), busy = ref(false), error = ref(''), imageURL = ref(''), cancelConfirm = ref(false)
let sequence = 0
function clearImage() { if (imageURL.value) URL.revokeObjectURL(imageURL.value); imageURL.value = '' }
function clear() { sequence++; orders.value = []; order.value = null; loading.value = false; busy.value = false; error.value = ''; cancelConfirm.value = false; clearImage() }
const session = useQQSession(clear)
const orderID = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const mayDownload = computed(() => order.value?.artifact_expires_at && Date.parse(order.value.artifact_expires_at) > Date.now())
async function refresh() {
  if (!session.current() || busy.value) return
  const ticket = ++sequence
  loading.value = true; error.value = ''
  try {
    if (orderID.value) {
      const result = await qqImageAPI.order(orderID.value, session.signal)
      if (session.current() && ticket === sequence) order.value = result
    } else {
      const result = await qqImageAPI.orders(session.signal)
      if (session.current() && ticket === sequence) orders.value = result || []
    }
  } catch (e) { if (session.current() && ticket === sequence) error.value = qqErrorKey(e) }
  finally { if (session.current() && ticket === sequence) loading.value = false }
}
async function loadImage() {
  if (!session.current() || busy.value || !order.value || !mayDownload.value) return
  const ticket = sequence, id = order.value.id
  busy.value = true; error.value = ''
  try {
    const blob = await qqImageAPI.image(id, session.signal)
    if (!session.current() || ticket !== sequence) return
    if (!['image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(blob.type)) throw new Error('Invalid image type')
    clearImage(); imageURL.value = URL.createObjectURL(blob)
  } catch (e) { if (session.current() && ticket === sequence) error.value = qqErrorKey(e) }
  finally { if (session.current() && ticket === sequence) busy.value = false }
}
async function cancel() {
  if (!session.current() || busy.value || !order.value || order.value.status !== 'queued' || !cancelConfirm.value) return
  const ticket = sequence
  busy.value = true; error.value = ''
  try {
    await qqImageAPI.cancel(order.value.id, session.signal)
    if (!session.current() || ticket !== sequence) return
    cancelConfirm.value = false; busy.value = false; await refresh()
  } catch (e) { if (session.current() && ticket === sequence) error.value = qqErrorKey(e) }
  finally { if (session.current() && ticket === sequence) busy.value = false }
}
watch(orderID, () => { clear(); void refresh() }, { immediate: true })
</script>
<template>
  <AppLayout><div class="qq-page">
    <header class="qq-heading"><div><h1>{{ t('qqImage.ordersTitle') }}</h1><p class="qq-muted mt-2">{{ t('qqImage.ordersHint') }}</p></div><div class="qq-actions"><RouterLink to="/qq/bind" class="qq-link">{{ t('qqImage.bindTitle') }}</RouterLink><RouterLink v-if="orderID" to="/qq/orders" class="qq-link">{{ t('qqImage.allOrders') }}</RouterLink><button class="btn btn-secondary" :disabled="loading || busy || session.invalidated.value" @click="refresh">{{ t('qqImage.refresh') }}</button></div></header>
    <div v-if="session.invalidated.value" class="qq-notice" role="alert">{{ t('qqImage.sessionChanged') }}</div>
    <template v-else>
      <div v-if="loading" class="qq-muted" role="status">{{ t('common.loading') }}</div><div v-if="error" class="qq-error" role="alert">{{ t(`qqImage.errors.${error}`) }}</div>
      <QQOrderCard v-if="order" :order="order" detail>
        <p class="qq-muted">{{ t('qqImage.settlementHint') }}</p>
        <div v-if="order.status === 'queued'" class="space-y-3"><button class="btn btn-secondary" :disabled="busy || loading" @click="cancelConfirm = true">{{ t('qqImage.cancelOrder') }}</button><div v-if="cancelConfirm" class="qq-notice space-y-3" role="alertdialog" :aria-label="t('qqImage.cancelOrder')"><p>{{ t('qqImage.cancelHint') }}</p><div class="qq-actions"><button class="btn btn-primary" :disabled="busy" @click="cancel">{{ t('qqImage.confirmCancel') }}</button><button class="btn btn-secondary" :disabled="busy" @click="cancelConfirm = false">{{ t('qqImage.back') }}</button></div></div></div>
        <p v-else-if="['dispatching', 'generating', 'reconciling', 'submitted'].includes(order.status)" class="qq-notice">{{ t('qqImage.dispatchedHint') }}</p>
        <button v-if="mayDownload && !imageURL" data-test="load-image" class="btn btn-primary" :disabled="busy || loading" @click="loadImage">{{ t('qqImage.loadImage') }}</button>
        <p v-else-if="order.artifact_expires_at && !mayDownload" class="qq-notice">{{ t('qqImage.imageExpired') }}</p>
        <div v-if="imageURL" class="space-y-3"><img :src="imageURL" :alt="t('qqImage.imageAlt')" class="max-h-[36rem] w-full rounded-xl object-contain" referrerpolicy="no-referrer" /><a :href="imageURL" :download="`qq-image-${order.id}`" class="btn btn-secondary">{{ t('qqImage.download') }}</a></div>
      </QQOrderCard>
      <template v-else-if="!orderID"><p v-if="!loading && !error && !orders.length" class="qq-panel qq-muted">{{ t('qqImage.emptyOrders') }}</p><QQOrderCard v-for="item in orders" :key="item.id" :order="item" /></template>
    </template>
  </div></AppLayout>
</template>
