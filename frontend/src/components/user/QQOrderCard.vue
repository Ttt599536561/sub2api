<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { QQOrder } from '@/api/qqImage'
defineProps<{ order: QQOrder; detail?: boolean }>()
const { t, te } = useI18n()
function state(kind: string, value: string) {
  const key = `qqImage.${kind}.${value}`
  return !te || te(key) ? t(key) : t(`qqImage.${kind}.unknown`)
}
</script>
<template>
  <article class="qq-panel space-y-4">
    <div class="qq-heading"><div class="min-w-0"><h2 class="break-all">{{ t('qqImage.order') }} {{ order.id }}</h2><p class="qq-muted">{{ new Date(order.created_at).toLocaleString() }}</p></div><RouterLink v-if="!detail" :to="`/qq/orders/${encodeURIComponent(order.id)}`" class="qq-link">{{ t('qqImage.details') }}</RouterLink></div>
    <dl class="qq-meta"><div><dt>{{ t('qqImage.task') }}</dt><dd>{{ state('taskStates', order.status) }}</dd></div><div><dt>{{ t('qqImage.funds') }}</dt><dd>{{ state('fundsStates', order.funds_status) }}</dd></div><div><dt>{{ t('qqImage.delivery') }}</dt><dd>{{ state('deliveryStates', order.delivery_status) }}</dd></div></dl>
    <dl class="qq-meta"><div><dt>{{ t('qqImage.price') }}</dt><dd>{{ order.price }} USD</dd></div><div><dt>{{ t('qqImage.model') }}</dt><dd>{{ order.model }}</dd></div><div><dt>{{ t('qqImage.size') }}</dt><dd>{{ order.size }}</dd></div></dl>
    <template v-if="detail"><p class="whitespace-pre-wrap break-words text-sm">{{ order.prompt }}</p><p class="qq-muted">{{ t('qqImage.deadline') }}: {{ new Date(order.deadline).toLocaleString() }}</p><p v-if="order.artifact_expires_at" class="qq-muted">{{ t('qqImage.retention') }}: {{ new Date(order.artifact_expires_at).toLocaleString() }}</p><p v-if="order.error" class="qq-notice">{{ t('qqImage.orderProblem') }}</p></template>
    <slot />
  </article>
</template>
