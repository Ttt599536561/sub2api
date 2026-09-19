<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { welfareMoney } from '@/utils/welfareMoney'
import type { WelfareCalendar, WelfareOverview } from '@/types/welfare'
const props = defineProps<{ overview: WelfareOverview; calendar: WelfareCalendar | null; month: string; loading: boolean; error: string; busy: boolean }>()
const emit = defineEmits<{ month: [value: string]; checkIn: []; retry: [] }>()
const { t, locale } = useI18n()
const cells = computed(() => {
  const [year, month] = props.month.split('-').map(Number)
  if (!year || !month) return []
  const offset = (new Date(Date.UTC(year, month - 1, 1)).getUTCDay() + 6) % 7
  const days = new Date(Date.UTC(year, month, 0)).getUTCDate()
  const checked = new Map((props.calendar?.month === props.month ? props.calendar.days : []).map(day => [day.date, day]))
  return Array.from({ length: offset + days }, (_, index) => {
    if (index < offset) return null
    const day = index - offset + 1
    const date = `${props.month}-${String(day).padStart(2, '0')}`
    return { day, date, today: date === props.overview.business_date, future: date > props.overview.business_date, record: checked.get(date) }
  })
})
const monthLabel = computed(() => props.month ? new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(`${props.month}-01T00:00:00Z`)) : '')
function changeMonth(delta: number) {
  const date = new Date(`${props.month}-01T00:00:00Z`)
  date.setUTCMonth(date.getUTCMonth() + delta)
  emit('month', date.toISOString().slice(0, 7))
}
function milestoneStatus(day: number) { return props.overview.milestones.find(m => m.day === day)?.status || 'locked' }
</script>
<template>
  <section class="wf-panel" :aria-label="t('welfare.checkIn')">
    <div class="wf-panel-title"><h2>{{ t('welfare.checkIn') }}</h2><span class="wf-chip">{{ t('welfare.cycle', { days: overview.cycle_day }) }}</span></div>
    <div class="wf-calendar-heading"><strong>{{ monthLabel }}</strong><div class="flex gap-2"><button class="wf-icon-button" :aria-label="t('welfare.prevMonth')" @click="changeMonth(-1)"><Icon name="chevronLeft" /></button><button class="wf-icon-button" :disabled="month >= overview.business_date.slice(0, 7)" :aria-label="t('welfare.nextMonth')" @click="changeMonth(1)"><Icon name="chevronRight" /></button></div></div>
    <div v-if="error" class="wf-error" role="alert">{{ t(`welfare.errors.${error}`) }} <button class="wf-link" @click="emit('retry')">{{ t('welfare.retry') }}</button></div>
    <div class="wf-calendar" :aria-label="t('welfare.calendar')" :aria-busy="loading">
      <div v-for="day in t('welfare.weekdays').split(' ')" :key="day" class="wf-weekday">{{ day }}</div>
      <div v-for="(cell, index) in cells" :key="index" :class="cell ? ['wf-day', { 'wf-signed': cell.record?.checked_in, 'wf-today': cell.today, 'wf-future': cell.future }] : ''" :aria-label="cell ? `${cell.date} ${t(cell.record?.checked_in ? 'welfare.signed' : 'welfare.notSigned')}` : undefined" :aria-hidden="cell ? undefined : true">
        <template v-if="cell"><span>{{ cell.day }}</span><small v-if="cell.record?.checked_in">✓ <span v-if="cell.record.reward_amount">{{ welfareMoney(cell.record.reward_amount) }}</span></small><small v-else-if="cell.today">{{ t('welfare.today') }}</small></template>
      </div>
    </div>
    <div class="wf-caption"><span>✓ {{ t('welfare.signed') }}</span><span>{{ loading ? t('welfare.loading') : t('welfare.calendarTimezone') }}</span></div>
    <div class="wf-milestones"><div v-for="day in [7, 15, 30]" :key="day" class="wf-milestone" :class="{ 'wf-earned': milestoneStatus(day) === 'claimed' }"><span>{{ t('welfare.milestone', { days: day }) }}</span><strong>{{ t('welfare.mystery') }}</strong><small>{{ milestoneStatus(day) === 'claimed' ? t('welfare.claimed') : day - overview.cycle_day === 1 || milestoneStatus(day) === 'available' ? t('welfare.unlocked') : t('welfare.remainingDays', { days: Math.max(0, day - overview.cycle_day) }) }}</small></div></div>
    <button data-testid="check-in" class="wf-button wf-primary w-full" :disabled="busy || overview.today_checked_in || !overview.rewards_enabled" @click="emit('checkIn')"><Icon name="check" />{{ overview.today_checked_in ? t('welfare.checked') : busy ? t('welfare.processing') : t('welfare.checkNow') }}</button>
    <p class="wf-small mt-3 text-center">{{ t('welfare.surprise') }}</p>
  </section>
</template>
