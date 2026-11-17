<template>
  <div v-if="(subscription.supplement_granted_usd ?? 0) > 0 || (subscription.supplement_used_usd ?? 0) > 0" data-test="subscription-supplement" class="space-y-1 rounded-lg bg-blue-50 p-3 text-xs text-blue-800 dark:bg-blue-900/20 dark:text-blue-200">
    <p class="font-medium">{{ t('welfareRedeem.supplementTitle') }}</p>
    <dl class="flex flex-wrap gap-x-4 gap-y-1 tabular-nums">
      <div><dt class="inline">{{ t('welfareRedeem.supplementGranted') }}: </dt><dd class="inline">${{ (subscription.supplement_granted_usd ?? 0).toFixed(2) }}</dd></div>
      <div><dt class="inline">{{ t('welfareRedeem.supplementUsed') }}: </dt><dd class="inline">${{ (subscription.supplement_used_usd ?? 0).toFixed(2) }}</dd></div>
      <div><dt class="inline">{{ t('welfareRedeem.supplementRemaining') }}: </dt><dd class="inline">${{ (subscription.supplement_remaining_usd ?? 0).toFixed(2) }}</dd></div>
    </dl>
    <p>{{ t('welfareRedeem.supplementQuotaHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UserSubscription } from '@/types'

defineProps<{ subscription: Pick<UserSubscription, 'supplement_granted_usd' | 'supplement_used_usd' | 'supplement_remaining_usd'> }>()
const { t } = useI18n()
</script>
