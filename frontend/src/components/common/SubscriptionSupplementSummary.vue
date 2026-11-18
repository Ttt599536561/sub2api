<template>
  <div v-if="(subscription.supplement_granted_usd ?? 0) > 0 || (subscription.supplement_used_usd ?? 0) > 0" data-test="subscription-supplement" class="space-y-2 rounded-lg bg-blue-50 p-3 text-xs text-blue-800 dark:bg-blue-900/20 dark:text-blue-200">
    <template v-if="monthlyBalance">
      <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <p class="text-sm font-medium">{{ t('welfareRedeem.monthlyRemainingWithSupplement') }}</p>
        <strong class="text-xl tabular-nums" data-test="monthly-remaining-with-supplement">
          {{ monthlyBalance.total }}
        </strong>
      </div>
      <p>{{ t('welfareRedeem.monthlySupplementBreakdown', { base: monthlyBalance.base, supplement: monthlyBalance.supplement }) }}</p>
      <p v-if="(subscription.group?.daily_limit_usd ?? 0) > 0 || (subscription.group?.weekly_limit_usd ?? 0) > 0">
        {{ t('welfareRedeem.monthlySupplementWindowHint') }}
      </p>
    </template>
    <p v-else class="font-medium">{{ t('welfareRedeem.supplementTitle') }}</p>
    <dl class="flex flex-wrap gap-x-4 gap-y-1 tabular-nums">
      <div><dt class="inline">{{ t('welfareRedeem.supplementGranted') }}: </dt><dd class="inline">${{ (subscription.supplement_granted_usd ?? 0).toFixed(2) }}</dd></div>
      <div><dt class="inline">{{ t('welfareRedeem.supplementUsed') }}: </dt><dd class="inline">${{ (subscription.supplement_used_usd ?? 0).toFixed(2) }}</dd></div>
      <div v-if="!monthlyBalance">
        <dt class="inline">{{ t('welfareRedeem.supplementRemaining') }}: </dt><dd class="inline">${{ (subscription.supplement_remaining_usd ?? 0).toFixed(2) }}</dd>
      </div>
    </dl>
    <p>{{ t('welfareRedeem.supplementQuotaHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserSubscription } from '@/types'

const props = defineProps<{
  subscription: Pick<UserSubscription, 'group' | 'supplement_granted_usd' | 'supplement_used_usd' | 'supplement_remaining_usd'>
  monthlyBaseRemaining?: number
}>()
const { t } = useI18n()

const monthlyBalance = computed(() => {
  const { monthlyBaseRemaining, subscription } = props
  const supplementRemaining = subscription.supplement_remaining_usd
  if (monthlyBaseRemaining === undefined || supplementRemaining === undefined) return

  const base = monthlyBaseRemaining.toFixed(2)
  const supplement = supplementRemaining.toFixed(2)
  // Keep the displayed total equal to the amounts shown in the breakdown.
  return {
    base: `$${base}`,
    supplement: `$${supplement}`,
    total: `$${(Number(base) + Number(supplement)).toFixed(2)}`
  }
})
</script>
