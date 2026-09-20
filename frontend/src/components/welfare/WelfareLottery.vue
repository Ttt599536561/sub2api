<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { WelfareOverview, WelfareRules } from '@/types/welfare'
import { moneyCents, welfareMoney } from '@/utils/welfareMoney'
const props = defineProps<{ overview: WelfareOverview; rules: WelfareRules | null; busy: boolean; redemptionPending?: boolean }>()
const emit = defineEmits<{ draw: []; redeem: []; probabilities: [] }>()
const { t } = useI18n()
const progress = computed(() => {
  const threshold = Number(props.rules?.draw_threshold)
  return threshold > 0 ? Math.max(0, Math.min(100, (1 - Number(props.overview.next_draw_remaining) / threshold) * 100)) : 0
})
const canRedeem = computed(() => (moneyCents(props.overview.welfare_balance) ?? 0n) > 0n)
</script>
<template>
  <section class="wf-panel wf-lottery">
    <div class="wf-panel-title"><h2>{{ t('welfare.lottery') }}</h2><button class="wf-link" :disabled="!rules" @click="emit('probabilities')">{{ t('welfare.probabilities') }} <Icon name="chevronRight" size="xs" /></button></div>
    <div class="wf-ticket"><span>{{ t('welfare.availableDraws') }}</span><strong>{{ overview.available_draws }}</strong><span>{{ t('welfare.guaranteed') }}</span></div>
    <div class="wf-caption"><span>{{ t('welfare.progress') }}</span><span>{{ rules ? t('welfare.threshold', { amount: welfareMoney(rules.draw_threshold) }) : '—' }}</span></div>
    <div class="wf-progress" role="progressbar" :aria-label="t('welfare.progress')" :aria-valuenow="Math.round(progress)" :aria-valuemin="0" :aria-valuemax="100"><span :style="{ width: `${progress}%` }"></span></div>
    <p class="wf-small mt-3">{{ t('welfare.spendRemaining', { amount: welfareMoney(overview.next_draw_remaining) }) }}</p>
    <div v-if="rules?.subscription_draw_currency === 'CNY' && rules.subscription_draw_threshold" class="mt-4 rounded-lg border border-gray-100 p-3 dark:border-dark-700">
      <p class="wf-small font-medium">{{ t('welfare.subscriptionThreshold', { amount: rules.subscription_draw_threshold }) }}</p>
      <p class="wf-small mt-1">{{ t('welfare.subscriptionHint') }}</p>
    </div>
    <p v-if="overview.ticket_debt > 0" class="wf-small mt-2">{{ t('welfare.debt', { count: overview.ticket_debt }) }}</p>
    <button data-testid="draw" class="wf-button wf-primary mt-5 w-full" :disabled="busy || overview.available_draws <= 0 || !overview.rewards_enabled" @click="emit('draw')"><Icon name="gift" />{{ overview.available_draws <= 0 ? t('welfare.noDraws') : busy ? t('welfare.processing') : t('welfare.drawNow') }}</button>
    <p class="wf-small mb-3 mt-5">{{ t('welfare.prizePool') }}</p>
    <div class="wf-prizes"><div v-for="prize in rules?.prizes || []" :key="prize.id" class="wf-prize">{{ welfareMoney(prize.amount) }}</div><span v-if="!rules" class="wf-small">{{ t('welfare.loading') }}</span></div>
    <div class="wf-exchange" :aria-label="t('welfare.redemption')">
      <div class="flex items-center justify-between gap-4"><div><h3>{{ t('welfare.redemption') }}</h3><p class="wf-small mt-1">{{ t('welfare.redeemable') }}</p></div><strong class="wf-exchange-amount">{{ welfareMoney(overview.welfare_balance) }}</strong></div>
      <button data-testid="open-redemption" class="wf-button wf-exchange-button mt-4 w-full" :disabled="busy || !canRedeem || redemptionPending" @click="emit('redeem')"><Icon name="sync" />{{ t('welfare.redeem') }}</button>
      <p class="wf-small mt-3 text-center">{{ t('welfare.exchangeNote') }}</p>
    </div>
  </section>
</template>
