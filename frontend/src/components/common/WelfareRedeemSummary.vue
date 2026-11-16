<template>
  <div class="space-y-1 text-sm">
    <p class="font-medium">
      {{ t('welfareRedeem.' + details.subscription_effect) }}:
      <span v-if="details.subscription_effect === 'add_quota'">+${{ details.quota_amount_usd?.toFixed(2) }}</span>
      <span v-else>+{{ details.validity_days }} {{ t('redeem.days') }}</span>
    </p>
    <p v-if="details.welfare_campaign_name" class="text-xs">{{ details.welfare_campaign_name }}</p>
    <p v-if="groupName" class="text-xs">{{ groupName }}</p>
    <div v-if="details.welfare_result" class="space-y-1 text-xs font-normal">
      <p class="font-medium">{{ t('welfareRedeem.resultAtRedemption') }}</p>
      <p>{{ t('welfareRedeem.monthlyLimit') }}: ${{ details.welfare_result.monthly_limit_usd.toFixed(2) }}</p>
      <p>{{ t('welfareRedeem.monthlyUsage') }}: ${{ details.welfare_result.monthly_usage_usd.toFixed(2) }}</p>
      <p>{{ t('welfareRedeem.remaining') }}: ${{ details.welfare_result.remaining_usd.toFixed(2) }}</p>
      <p>{{ t('welfareRedeem.expiresAt') }}: {{ formatDateTime(details.welfare_result.expires_at) }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { WelfareRedeemDetails } from '@/types'
import { formatDateTime } from '@/utils/format'

defineProps<{
  details: WelfareRedeemDetails & { validity_days?: number }
  groupName?: string
}>()
const { t } = useI18n()
</script>
