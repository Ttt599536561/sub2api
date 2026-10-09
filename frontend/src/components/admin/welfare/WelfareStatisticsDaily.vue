<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import { welfareMoney } from '@/utils/welfareMoney'
import { addBusinessDays, validWelfareDateRange } from '@/utils/welfareStatisticsDate'
import type { WelfareRewardTotals } from '@/types/adminWelfare'
const props = defineProps<{ daily: Array<WelfareRewardTotals & { date: string }>; from: string; to: string; loading: boolean; error: boolean }>()
defineEmits<{ date: [value: string] }>()
const { t } = useI18n()
const columns = computed(() => [
  { key: 'date', label: t('welfare.admin.stats.date') },
  { key: 'checkin_users', label: t('welfare.admin.stats.checkinUsers') },
  { key: 'streak_users', label: t('welfare.admin.stats.streakUsers') },
  ...(['daily_amount', 'streak_amount', 'checkin_amount'] as const).map(key => ({ key, label: t(`welfare.admin.stats.${key}`), formatter: (value: string) => welfareMoney(value) })),
  { key: 'draw_users', label: t('welfare.admin.stats.drawUsers') },
  { key: 'draw_count', label: t('welfare.admin.stats.drawCount') },
  ...(['draw_amount', 'total_amount'] as const).map(key => ({ key, label: t(`welfare.admin.stats.${key}`), formatter: (value: string) => welfareMoney(value) }))
])
const rows = computed(() => {
  if (!validWelfareDateRange(props.from, props.to)) return []
  const dates = new Map(props.daily.map(row => [row.date, row]))
  const values: Array<WelfareRewardTotals & { date: string }> = []
  for (let date = props.from; date <= props.to;) {
    values.push(dates.get(date) ?? { date, daily_amount: '0.00', streak_amount: '0.00', checkin_amount: '0.00', draw_amount: '0.00', total_amount: '0.00', checkin_users: 0, checkin_count: 0, streak_users: 0, draw_users: 0, draw_count: 0, participating_users: 0 })
    if (date === props.to) break
    date = addBusinessDays(date, 1)
  }
  return values
})
</script>

<template>
  <section class="card overflow-hidden">
    <div class="p-5"><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('welfare.admin.stats.dailyTitle') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.dailyHint') }}</p></div>
    <DataTable v-if="!error" class="md:max-h-[32rem]" :columns="columns" :data="rows" :loading="loading" row-key="date" :sticky-actions-column="false">
      <template #cell-date="{ row }"><button :data-test="`daily-${row.date}`" type="button" class="font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('date', row.date)">{{ row.date }}</button></template>
    </DataTable>
    <p v-else class="px-5 pb-5 text-sm text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.dailyUnavailable') }}</p>
  </section>
</template>
