<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { WelfareStatisticsUser } from '@/types/adminWelfare'
import { useAdminWelfareStatistics } from '@/composables/useAdminWelfareStatistics'
import type { WelfareDatePreset } from '@/utils/welfareStatisticsDate'
import WelfareStatisticsSummary from './WelfareStatisticsSummary.vue'
import WelfareStatisticsDaily from './WelfareStatisticsDaily.vue'
import WelfareStatisticsUsers from './WelfareStatisticsUsers.vue'
import WelfareStatisticsRecords from './WelfareStatisticsRecords.vue'
const { t } = useI18n()
const stats = useAdminWelfareStatistics()
const { draft, applied, validationError, userPage, userPageSize, sortBy, sortOrder, recordPage, recordPageSize, recordType, detailDates, detailUser, hasDetailScope, recordDateError } = stats
const presets = ['today', 'yesterday', 'last7', 'last30', 'month', 'custom'] as const
function presetChange(event: Event) { stats.selectPreset((event.target as HTMLSelectElement).value as WelfareDatePreset) }
const recordsAnchor = ref<HTMLElement | null>(null)
async function scrollToRecords() {
  await nextTick()
  recordsAnchor.value?.scrollIntoView?.({
    behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
    block: 'start'
  })
}
function drillDate(date: string) { stats.drillDate(date); void scrollToRecords() }
function drillUser(user: WelfareStatisticsUser) { stats.drillUser(user); void scrollToRecords() }
</script>

<template>
  <div class="space-y-5">
    <form data-test="stats-filters" class="card space-y-4 p-5" @submit.prevent="stats.applyFilters">
      <div class="flex flex-wrap items-center justify-between gap-2"><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('welfare.admin.stats.filterTitle') }}</h2><span class="text-xs text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.timezoneHint') }}</span></div>
      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.admin.stats.range') }}</span><select data-test="stats-preset" class="input w-full" :value="draft.preset" @change="presetChange"><option v-for="preset in presets" :key="preset" :value="preset">{{ t(`welfare.admin.stats.presets.${preset}`) }}</option></select></label>
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.from') }}</span><input v-model="draft.date_from" data-test="stats-date-from" type="date" class="input w-full" :disabled="draft.preset !== 'custom'" :max="draft.date_to" /></label>
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.to') }}</span><input v-model="draft.date_to" data-test="stats-date-to" type="date" class="input w-full" :disabled="draft.preset !== 'custom'" :min="draft.date_from" /></label>
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.admin.stats.search') }}</span><input v-model="draft.search" data-test="stats-search" type="search" class="input w-full" :placeholder="t('welfare.admin.stats.searchHint')" /></label>
        <label class="space-y-1 text-sm text-gray-600 dark:text-dark-300"><span>{{ t('welfare.admin.stats.userId') }}</span><input v-model="draft.user_id" data-test="stats-user-id" inputmode="numeric" class="input w-full" :placeholder="t('welfare.admin.stats.userIdHint')" /></label>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3"><p class="text-sm text-gray-500 dark:text-dark-400">{{ t('welfare.admin.stats.appliedRange') }} {{ applied.date_from }} — {{ applied.date_to }}<span v-if="applied.search"> · {{ applied.search }}</span><span v-if="applied.user_id"> · ID {{ applied.user_id }}</span></p><button type="submit" class="btn btn-primary">{{ t('welfare.admin.stats.apply') }}</button></div>
      <p v-if="validationError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(`welfare.admin.stats.${validationError}`) }}</p>
    </form>
    <WelfareStatisticsSummary :totals="stats.overview.data.value?.summary ?? null" :loading="stats.overview.loading.value" :error="stats.overview.error.value" @retry="stats.overview.load" />
    <WelfareStatisticsDaily :daily="stats.overview.data.value?.daily ?? []" :from="applied.date_from" :to="applied.date_to" :loading="stats.overview.loading.value" :error="stats.overview.error.value" @date="drillDate" />
    <WelfareStatisticsUsers :data="stats.users.data.value" :loading="stats.users.loading.value" :error="stats.users.error.value" :page="userPage" :page-size="userPageSize" :sort-by="sortBy" :sort-order="sortOrder" @user="drillUser" @page="stats.setUserPage" @page-size="stats.setUserPageSize" @sort="stats.sortUsers" @retry="stats.users.load" />
    <div ref="recordsAnchor" class="scroll-mt-6">
    <WelfareStatisticsRecords :data="stats.records.data.value" :loading="stats.records.loading.value" :error="stats.records.error.value" :page="recordPage" :page-size="recordPageSize" :type="recordType" :from="detailDates.date_from" :to="detailDates.date_to" :range-from="applied.date_from" :range-to="applied.date_to" :user="detailUser" :applied-user-id="applied.user_id" :search="applied.search" :scoped="hasDetailScope" :date-error="recordDateError" @page="stats.setRecordPage" @page-size="stats.setRecordPageSize" @type="stats.setRecordType" @dates="stats.setRecordDates" @clear="stats.clearDetailScope" @retry="stats.records.load" />
    </div>
  </div>
</template>
