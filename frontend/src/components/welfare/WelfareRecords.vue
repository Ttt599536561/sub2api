<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { welfareMoney } from '@/utils/welfareMoney'
import type { WelfareRecords, WelfareRecordQuery } from '@/types/welfare'
const props = defineProps<{ records: WelfareRecords | null; filters: WelfareRecordQuery; loading: boolean; error: string; drawsUsed: number }>()
const emit = defineEmits<{ filter: [values: Partial<WelfareRecordQuery>]; retry: [] }>()
const { t, locale } = useI18n()
const pages = computed(() => Math.max(1, Math.ceil((props.records?.total || 0) / props.filters.page_size)))
function recordTime(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short', timeZone: 'Asia/Shanghai' }).format(date) }
function typeChange(event: Event) { emit('filter', { type: (event.target as HTMLSelectElement).value as WelfareRecordQuery['type'] }) }
function dateChange(key: 'date_from' | 'date_to', event: Event) { emit('filter', { [key]: (event.target as HTMLInputElement).value }) }
</script>
<template>
  <section class="wf-panel wf-records" :aria-label="t('welfare.records')">
    <div class="wf-panel-title"><h2>{{ t('welfare.records') }}</h2><span class="wf-small">{{ t('welfare.totalDraws', { count: records?.total_draws ?? drawsUsed }) }}</span></div>
    <div class="wf-filters"><label class="wf-field">{{ t('welfare.recordType') }}<select class="input" :value="filters.type" @change="typeChange"><option v-for="type in ['all', 'daily', 'streak', 'draw', 'redeem']" :key="type" :value="type">{{ t(`welfare.types.${type}`) }}</option></select></label><label class="wf-field">{{ t('welfare.from') }}<input type="date" class="input" :value="filters.date_from" :max="filters.date_to || undefined" @change="dateChange('date_from', $event)" /></label><label class="wf-field">{{ t('welfare.to') }}<input type="date" class="input" :value="filters.date_to" :min="filters.date_from || undefined" @change="dateChange('date_to', $event)" /></label><button class="wf-button" @click="emit('filter', { type: 'all', date_from: '', date_to: '', page: 1 })">{{ t('welfare.reset') }}</button></div>
    <div v-if="error" role="alert" class="wf-error">{{ t(`welfare.errors.${error}`) }} <button v-if="error !== 'dateRange'" class="wf-link" @click="emit('retry')">{{ t('welfare.retry') }}</button></div>
    <div class="overflow-x-auto" :aria-busy="loading"><table class="wf-table"><thead><tr><th>{{ t('welfare.time') }}</th><th>{{ t('welfare.type') }}</th><th>{{ t('welfare.detail') }}</th><th>{{ t('welfare.change') }}</th></tr></thead><tbody><tr v-if="loading"><td colspan="4" class="text-center">{{ t('welfare.loading') }}</td></tr><template v-else-if="!error"><tr v-for="record in records?.items || []" :key="record.id"><td class="whitespace-nowrap">{{ recordTime(record.created_at) }}</td><td class="whitespace-nowrap">{{ t(`welfare.types.${record.type}`) }}</td><td>{{ record.description }}</td><td class="whitespace-nowrap font-medium" :class="{ 'text-primary-600 dark:text-primary-400': !record.amount.startsWith('-') }">{{ record.amount.startsWith('-') ? '' : '+' }}{{ welfareMoney(record.amount) }}</td></tr><tr v-if="!records?.items.length"><td colspan="4" class="text-center">{{ t('welfare.empty') }}</td></tr></template></tbody></table></div>
    <div class="wf-table-footer"><span>{{ t('welfare.recordCount', { count: records?.total || 0 }) }}</span><div class="flex items-center gap-3"><button class="wf-icon-button" :disabled="loading || filters.page <= 1" :aria-label="t('welfare.prevPage')" @click="emit('filter', { page: filters.page - 1 })"><Icon name="chevronLeft" /></button><span>{{ filters.page }} / {{ pages }}</span><button class="wf-icon-button" :disabled="loading || filters.page >= pages" :aria-label="t('welfare.nextPage')" @click="emit('filter', { page: filters.page + 1 })"><Icon name="chevronRight" /></button></div></div>
  </section>
</template>
