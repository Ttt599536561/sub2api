<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import { welfareMoney } from '@/utils/welfareMoney'
import type { WelfareStatisticsPage, WelfareStatisticsUser, WelfareUserSort } from '@/types/adminWelfare'
const props = defineProps<{ data: WelfareStatisticsPage<WelfareStatisticsUser> | null; loading: boolean; error: boolean; page: number; pageSize: number; sortBy: WelfareUserSort; sortOrder: 'asc' | 'desc' }>()
const emit = defineEmits<{ user: [value: WelfareStatisticsUser]; page: [value: number]; pageSize: [value: number]; sort: [key: string, order: 'asc' | 'desc']; retry: [] }>()
const { t } = useI18n()
const columns = computed(() => [
  { key: 'email', label: t('welfare.admin.stats.user') },
  { key: 'period_checkin_count', label: t('welfare.admin.stats.periodCheckinCount'), formatter: (_value: unknown, row: WelfareStatisticsUser) => String(row.period.checkin_count) },
  { key: 'checkin_count', label: t('welfare.admin.stats.lifetimeCheckinDays'), sortable: true, formatter: (_value: unknown, row: WelfareStatisticsUser) => String(row.lifetime.checkin_count) },
  ...(['daily_amount', 'streak_amount', 'checkin_amount', 'draw_amount'] as const).map(key => ({ key, label: `${t('welfare.admin.stats.period')} · ${t(`welfare.admin.stats.${key}`)}`, formatter: (_value: unknown, row: WelfareStatisticsUser) => welfareMoney(row.period[key]) })),
  { key: 'period_total_amount', label: t('welfare.admin.stats.periodTotal'), sortable: true, formatter: (_value: unknown, row: WelfareStatisticsUser) => welfareMoney(row.period.total_amount) },
  { key: 'lifetime_rewards', label: t('welfare.admin.stats.lifetimeRewards') },
  { key: 'total_amount', label: t('welfare.admin.stats.lifetimeTotal'), sortable: true, formatter: (_value: unknown, row: WelfareStatisticsUser) => welfareMoney(row.lifetime.total_amount) }
])
const sortOptions = computed(() => [
  { value: 'total_amount', label: t('welfare.admin.stats.lifetimeTotal') },
  { value: 'period_total_amount', label: t('welfare.admin.stats.periodTotal') },
  { value: 'checkin_count', label: t('welfare.admin.stats.lifetimeCheckinDays') }
])
function changeSortBy(event: Event) { emit('sort', (event.target as HTMLSelectElement).value, props.sortOrder) }
function changeSortOrder(event: Event) { emit('sort', props.sortBy, (event.target as HTMLSelectElement).value as 'asc' | 'desc') }
</script>

<template>
  <section class="card overflow-hidden">
    <div class="space-y-4 p-5">
      <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('welfare.admin.stats.usersTitle') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.usersHint') }}</p></div>
      <div class="grid gap-3 sm:max-w-xl sm:grid-cols-2">
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.admin.stats.sortBy') }}</span><select data-test="users-sort-by" class="input w-full" :value="sortBy" @change="changeSortBy"><option v-for="option in sortOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.admin.stats.sortOrder') }}</span><select data-test="users-sort-order" class="input w-full" :value="sortOrder" @change="changeSortOrder"><option value="desc">{{ t('welfare.admin.stats.descending') }}</option><option value="asc">{{ t('welfare.admin.stats.ascending') }}</option></select></label>
      </div>
    </div>
    <div v-if="error" role="alert" class="flex items-center justify-between gap-3 px-5 pb-5 text-sm text-red-600 dark:text-red-400"><span>{{ t('welfare.admin.stats.loadFailed') }}</span><button type="button" class="btn btn-secondary btn-sm" @click="$emit('retry')">{{ t('welfare.retry') }}</button></div>
    <template v-else>
      <DataTable :key="`${sortBy}:${sortOrder}`" :columns="columns" :data="data?.items ?? []" :loading="loading" row-key="user_id" :server-side-sort="true" :default-sort-key="sortBy" :default-sort-order="sortOrder" :sticky-actions-column="false" @sort="(key, order) => $emit('sort', key, order)">
        <template #cell-email="{ row }"><button :data-test="`user-${row.user_id}`" type="button" class="text-left font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('user', row)">{{ row.email || '—' }}<span class="mt-1 block text-xs font-normal text-gray-500 dark:text-dark-400">ID {{ row.user_id }}</span></button></template>
        <template #cell-lifetime_rewards="{ row }"><dl :data-test="`user-lifetime-${row.user_id}`" class="grid gap-1 text-xs"><div v-for="key in ['daily_amount', 'streak_amount', 'checkin_amount', 'draw_amount'] as const" :key="key" class="flex justify-between gap-4"><dt class="text-gray-500 dark:text-dark-400">{{ t(`welfare.admin.stats.${key}`) }}</dt><dd class="tabular-nums">{{ welfareMoney(row.lifetime[key]) }}</dd></div></dl></template>
        <template #empty>{{ t('welfare.admin.stats.emptyUsers') }}</template>
      </DataTable>
      <Pagination v-if="data && data.total > 0 && !loading" :total="data.total" :page="page" :page-size="pageSize" @update:page="$emit('page', $event)" @update:page-size="$emit('pageSize', $event)" />
    </template>
  </section>
</template>
