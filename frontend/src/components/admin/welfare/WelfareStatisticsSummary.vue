<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { welfareMoney } from '@/utils/welfareMoney'
import type { WelfareRewardTotals } from '@/types/adminWelfare'
defineProps<{ totals: WelfareRewardTotals | null; loading: boolean; error: boolean }>()
defineEmits<{ retry: [] }>()
const { t } = useI18n()
const amounts = ['daily_amount', 'streak_amount', 'checkin_amount', 'draw_amount', 'total_amount'] as const
</script>

<template>
  <section :aria-label="t('welfare.admin.stats.overview')" :aria-busy="loading" class="space-y-4">
    <div v-if="error" role="alert" class="card flex items-center justify-between gap-4 p-5 text-sm text-red-600 dark:text-red-400">
      <span>{{ t('welfare.admin.stats.loadFailed') }}</span>
      <button data-test="summary-retry" type="button" class="btn btn-secondary btn-sm" @click="$emit('retry')">{{ t('welfare.retry') }}</button>
    </div>
    <template v-else>
      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <div v-for="key in amounts" :key="key" :data-test="`summary-${key}`" class="card p-4" :class="key === 'total_amount' ? 'border-primary-200 dark:border-primary-800' : ''">
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ t(`welfare.admin.stats.${key}`) }}</p>
          <p class="mt-2 break-all text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ loading ? '…' : welfareMoney(totals?.[key]) }}</p>
        </div>
      </div>
      <div class="card grid gap-4 p-4 text-sm sm:grid-cols-3">
        <div><span class="text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.checkinUsers') }}</span><p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ loading ? '…' : totals?.checkin_users ?? '—' }} <span class="font-normal text-gray-500">· {{ t('welfare.admin.stats.checkinCount') }} {{ loading ? '…' : totals?.checkin_count ?? '—' }}</span></p></div>
        <div><span class="text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.streakUsers') }}</span><p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ loading ? '…' : totals?.streak_users ?? '—' }}</p></div>
        <div><span class="text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.drawUsers') }}</span><p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ loading ? '…' : totals?.draw_users ?? '—' }} <span class="font-normal text-gray-500">· {{ t('welfare.admin.stats.drawCount') }} {{ loading ? '…' : totals?.draw_count ?? '—' }}</span></p></div>
      </div>
    </template>
  </section>
</template>
