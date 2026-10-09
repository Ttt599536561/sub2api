<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import { welfareMoney } from '@/utils/welfareMoney'
import type { WelfareRewardType, WelfareStatisticsPage, WelfareStatisticsRecord } from '@/types/adminWelfare'
defineProps<{ data: WelfareStatisticsPage<WelfareStatisticsRecord> | null; loading: boolean; error: boolean; page: number; pageSize: number; type: WelfareRewardType; from: string; to: string; rangeFrom: string; rangeTo: string; user: { user_id: number; email: string } | null; appliedUserId?: number; search?: string; scoped: boolean; dateError: boolean }>()
defineEmits<{ page: [value: number]; pageSize: [value: number]; type: [value: WelfareRewardType]; dates: [from: string, to: string]; clear: []; retry: [] }>()
const { t, locale } = useI18n()
function recordTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short', timeZone: 'Asia/Shanghai' }).format(date)
}
const columns = computed(() => [
  { key: 'email', label: t('welfare.admin.stats.user') },
  { key: 'type', label: t('welfare.type'), formatter: (value: string) => t(`welfare.types.${value}`) },
  { key: 'amount', label: t('welfare.admin.stats.rewardAmount'), formatter: (value: string) => welfareMoney(value) },
  { key: 'created_at', label: t('welfare.time'), formatter: (value: string) => recordTime(value) },
  { key: 'business_date', label: t('welfare.admin.stats.date') },
  { key: 'cycle_day', label: t('welfare.admin.stats.cycleDay'), formatter: (value: number | undefined) => value === undefined ? '—' : String(value) }
])
function typeChange(event: Event) { return (event.target as HTMLSelectElement).value as WelfareRewardType }
function inputValue(event: Event) { return (event.target as HTMLInputElement).value }
</script>

<template>
  <section class="card overflow-hidden">
    <div class="space-y-4 p-5">
      <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('welfare.admin.stats.recordsTitle') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.recordsHint') }}</p></div>
      <div data-test="records-scope" class="flex flex-wrap items-center gap-2 rounded-lg bg-gray-50 px-3 py-2 text-sm text-gray-600 dark:bg-dark-800 dark:text-dark-300">
        <span>{{ t('welfare.admin.stats.detailScope') }} {{ from }} — {{ to }} · Asia/Shanghai</span>
        <span v-if="user">· {{ user.email || '—' }} (ID {{ user.user_id }})</span>
        <span v-else-if="appliedUserId">· ID {{ appliedUserId }}</span>
        <span v-if="search">· {{ t('welfare.admin.stats.search') }}: {{ search }}</span>
        <button v-if="scoped" data-test="records-clear-scope" type="button" class="ml-auto text-primary-600 hover:underline dark:text-primary-400" @click="$emit('clear')">{{ t('welfare.admin.stats.clearScope') }}</button>
      </div>
      <div class="grid gap-3 sm:grid-cols-3">
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.recordType') }}</span><select data-test="records-type" class="input w-full" :value="type" @change="$emit('type', typeChange($event))"><option v-for="value in ['all', 'daily', 'streak', 'draw']" :key="value" :value="value">{{ t(`welfare.types.${value}`) }}</option></select></label>
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.from') }}</span><input data-test="records-date-from" type="date" class="input w-full" :value="from" :min="rangeFrom" :max="to" @change="$emit('dates', inputValue($event), to)" /></label>
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.to') }}</span><input data-test="records-date-to" type="date" class="input w-full" :value="to" :min="from" :max="rangeTo" @change="$emit('dates', from, inputValue($event))" /></label>
      </div>
      <p v-if="dateError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('welfare.admin.stats.detailRangeError') }}</p>
    </div>
    <div v-if="error" role="alert" class="flex items-center justify-between gap-3 px-5 pb-5 text-sm text-red-600 dark:text-red-400"><span>{{ t('welfare.admin.stats.loadFailed') }}</span><button type="button" class="btn btn-secondary btn-sm" @click="$emit('retry')">{{ t('welfare.retry') }}</button></div>
    <template v-else>
      <DataTable :columns="columns" :data="data?.items ?? []" :loading="loading" row-key="id" :sticky-actions-column="false">
        <template #cell-email="{ row }"><span>{{ row.email || '—' }}</span><span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">ID {{ row.user_id }}</span></template>
        <template #empty>{{ t('welfare.admin.stats.emptyRecords') }}</template>
      </DataTable>
      <Pagination v-if="data && data.total > 0 && !loading" :total="data.total" :page="page" :page-size="pageSize" @update:page="$emit('page', $event)" @update:page-size="$emit('pageSize', $event)" />
    </template>
  </section>
</template>
