<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, type LocationQuery } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import WelfareCalendar from '@/components/welfare/WelfareCalendar.vue'
import WelfareLottery from '@/components/welfare/WelfareLottery.vue'
import WelfareRecords from '@/components/welfare/WelfareRecords.vue'
import WelfareRedemption from '@/components/welfare/WelfareRedemption.vue'
import { useWelfare } from '@/composables/useWelfare'
import { useAuthStore } from '@/stores/auth'
import { welfareMoney } from '@/utils/welfareMoney'
import type { WelfareOperation, WelfareRecordQuery } from '@/types/welfare'
import '@/components/welfare/welfare.css'
const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const welfarePath = route.path
function recordFilters(query: LocationQuery): WelfareRecordQuery {
  const type = typeof query.type === 'string' && ['all', 'daily', 'streak', 'draw', 'redeem'].includes(query.type) ? query.type as WelfareRecordQuery['type'] : 'all'
  const date = (value: LocationQuery[string]) => {
    if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return ''
    const parsed = new Date(`${value}T00:00:00Z`)
    return !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value ? value : ''
  }
  const page = typeof query.page === 'string' && /^[1-9]\d*$/.test(query.page) && Number(query.page) <= 1000000 ? Number(query.page) : 1
  return { type, date_from: date(query.date_from), date_to: date(query.date_to), page, page_size: 10 }
}
const { overview, calendar, records, rules, month, filters, loading, calendarLoading, recordsLoading, error, rulesError, calendarError, recordsError, mutationError, busy, sessionInvalidated, recovering, recovered, pendingDraw, pendingRedemption, recoverPending, refresh, loadCalendar, loadRecords, loadRules, checkIn, draw, quote, redeem } = useWelfare(recordFilters(route.query))
function saveRecordQuery(next: WelfareRecordQuery) {
  const query = { ...route.query }
  for (const key of ['type', 'date_from', 'date_to', 'page']) delete query[key]
  if (next.type !== 'all') query.type = next.type
  if (next.date_from) query.date_from = next.date_from
  if (next.date_to) query.date_to = next.date_to
  if (next.page > 1) query.page = String(next.page)
  if (['type', 'date_from', 'date_to', 'page'].some(key => query[key] !== route.query[key])) void router.replace({ query, hash: route.hash })
}
function changeRecordFilters(values: Partial<WelfareRecordQuery>) {
  Object.assign(filters, values, { page: values.page ?? 1 })
  saveRecordQuery(filters)
}
watch(() => route.query, query => {
  if (route.path !== welfarePath) return
  const next = recordFilters(query)
  Object.assign(filters, next)
  saveRecordQuery(next)
}, { immediate: true })
const dialog = ref<'rules' | 'probabilities' | 'result' | ''>('')
const showRedemption = ref(false)
const notice = ref('')
const result = ref<WelfareOperation | null>(null)
const redemptionKey = ref(0)
watch(sessionInvalidated, invalid => { if (invalid) window.location.reload() }, { flush: 'post' })
watch(() => recovered.value.length, (count, previous) => {
  for (const recovery of recovered.value.slice(previous, count)) {
    if (recovery.kind === 'draw') { result.value = recovery.result; dialog.value = 'result' }
    else redemptionComplete(recovery.amount || recovery.result.amount || '0.00')
  }
})
watch([() => auth.sessionRevision, () => auth.user?.id], () => { dialog.value = ''; showRedemption.value = false; notice.value = ''; result.value = null; redemptionKey.value++ })
async function handleCheckIn() {
  const response = await checkIn()
  if (response?.status !== 'completed') return
  notice.value = response.streak_reward_amount && response.streak_reward_amount !== '0.00' ? t('welfare.checkSuccess', { daily: welfareMoney(response.base_reward_amount), streak: welfareMoney(response.streak_reward_amount), total: welfareMoney(response.reward_amount) }) : t('welfare.checkDailySuccess', { amount: welfareMoney(response.reward_amount) })
}
async function handleDraw() { const response = await draw(); if (response?.status === 'completed') { result.value = response; dialog.value = 'result' } }
function redemptionComplete(amount: string) {
  // Recovery and page-level retries can complete outside the dialog itself.
  showRedemption.value = false
  redemptionKey.value++
  notice.value = t('welfare.redemptionSuccess', { amount: welfareMoney(amount) })
  void auth.refreshUser().catch(() => { /* The transfer already committed; the next account refresh can recover. */ })
}
async function retryRedemption() {
  const pending = pendingRedemption.value
  if (!pending) return
  const owner = redemptionKey.value
  const response = await redeem(pending.body)
  if (owner !== redemptionKey.value) return
  if (response?.status === 'completed') redemptionComplete(pending.body.amount)
  else if (!pendingRedemption.value) { showRedemption.value = false; redemptionKey.value++ }
}
</script>
<template>
  <AppLayout>
    <div class="welfare-page">
      <div class="wf-heading"><div><h1>{{ t('welfare.title') }}</h1><p>{{ t('welfare.subtitle') }}</p></div><button class="wf-button" :disabled="!rules" @click="dialog = 'rules'"><Icon name="infoCircle" />{{ t('welfare.rules') }}</button></div>
      <div v-if="loading && !overview" class="wf-panel text-center" role="status">{{ t('welfare.loading') }}</div>
      <div v-if="error" class="wf-error" role="alert">{{ t(`welfare.errors.${error}`) }} <button class="wf-link" @click="refresh">{{ t('welfare.retry') }}</button></div>
      <div v-if="rulesError" class="wf-error" role="alert">{{ t(`welfare.errors.${rulesError}`) }} <button class="wf-link" @click="loadRules">{{ t('welfare.retry') }}</button></div>
      <div v-if="pendingDraw || pendingRedemption" class="wf-notice space-y-3" role="status">
        <p>{{ recovering ? t('welfare.recovering') : t('welfare.pendingHint') }}</p>
        <div v-if="pendingDraw" class="flex flex-wrap items-center justify-between gap-3"><span>{{ t('welfare.pendingDraw') }}</span><button data-testid="retry-pending-draw" class="wf-button" :disabled="busy || recovering" @click="handleDraw">{{ t('welfare.retryDraw') }}</button></div>
        <div v-if="pendingRedemption" class="flex flex-wrap items-center justify-between gap-3"><span>{{ t('welfare.pendingRedemption', { amount: welfareMoney(pendingRedemption.body.amount) }) }}</span><button data-testid="retry-pending-redemption" class="wf-button" :disabled="busy || recovering" @click="retryRedemption">{{ t('welfare.retryTransfer') }}</button></div>
        <button class="wf-link" :disabled="busy || recovering" @click="recoverPending">{{ t('welfare.checkResult') }}</button>
      </div>
      <template v-if="overview">
        <div class="wf-stats"><section class="wf-stat"><div class="wf-stat-label"><Icon name="creditCard" />{{ t('welfare.balance') }}</div><strong>{{ welfareMoney(overview.welfare_balance) }}</strong><p>{{ t('welfare.balanceHint') }}</p></section><section class="wf-stat"><div class="wf-stat-label"><Icon name="gift" />{{ t('welfare.draws') }}</div><strong>{{ overview.available_draws }} <small>{{ t('welfare.drawsUnit') }}</small></strong><p>{{ rules ? t('welfare.threshold', { amount: welfareMoney(rules.draw_threshold) }) : '—' }}</p></section><section class="wf-stat"><div class="wf-stat-label"><Icon name="calendar" />{{ t('welfare.totalDays') }}</div><strong>{{ overview.total_checkin_days }} <small>{{ t('welfare.daysUnit') }}</small></strong><p>{{ t('welfare.cycle', { days: overview.cycle_day }) }}</p></section></div>
        <p v-if="!overview.rewards_enabled" class="wf-notice" role="status">{{ t('welfare.paused') }}</p>
        <p v-if="notice" class="wf-notice" role="status" aria-live="polite">{{ notice }}</p>
        <p v-if="mutationError" class="wf-error" role="alert">{{ t(`welfare.errors.${mutationError}`) }}</p>
        <div class="wf-grid"><WelfareCalendar :overview="overview" :calendar="calendar" :month="month" :loading="calendarLoading" :error="calendarError" :busy="busy || recovering" @month="month = $event" @check-in="handleCheckIn" @retry="loadCalendar" /><WelfareLottery :overview="overview" :rules="rules" :busy="busy || recovering" :redemption-pending="!!pendingRedemption" @draw="handleDraw" @redeem="showRedemption = true" @probabilities="dialog = 'probabilities'" /></div>
        <WelfareRecords :records="records" :filters="filters" :loading="recordsLoading" :error="recordsError" :draws-used="overview.draws_used" @filter="changeRecordFilters" @retry="loadRecords" />
        <p class="wf-footer">{{ t('welfare.footer') }}</p>
        <WelfareRedemption :key="redemptionKey" :show="showRedemption" :balance="overview.welfare_balance" :busy="busy" :pending="!!pendingRedemption" :mutation-error="mutationError" :quote-request="quote" :redeem-request="redeem" @close="showRedemption = false" @completed="redemptionComplete" />
      </template>
      <BaseDialog :show="!!dialog" trap-focus :title="t(dialog === 'result' ? 'welfare.drawResult' : dialog === 'probabilities' ? 'welfare.probabilities' : 'welfare.rules')" @close="dialog = ''">
        <div v-if="dialog === 'rules'" class="space-y-5 text-sm leading-7 text-gray-600 dark:text-dark-300"><div><h3 class="font-semibold text-gray-900 dark:text-white">{{ t('welfare.checkIn') }}</h3><p>{{ t('welfare.ruleCheck') }}</p></div><p>{{ t('welfare.ruleStreak') }}</p><p>{{ t('welfare.ruleSpend', { amount: welfareMoney(rules?.draw_threshold) }) }}</p><p>{{ t('welfare.ruleRedeem') }}</p></div>
        <div v-else-if="dialog === 'probabilities'"><p class="mb-4 text-sm text-gray-500 dark:text-dark-300">{{ t('welfare.probabilityHint') }}</p><div v-for="prize in rules?.prizes || []" :key="prize.id" class="flex justify-between border-b border-gray-100 py-3 dark:border-dark-700"><strong>{{ welfareMoney(prize.amount) }}</strong><span>{{ prize.probability }}</span></div></div>
        <div v-else-if="result" class="py-5 text-center"><p class="text-sm text-gray-500 dark:text-dark-300">{{ t('welfare.received') }}</p><strong class="my-4 block text-4xl text-primary-600 dark:text-primary-400">+{{ welfareMoney(result.reward_amount || result.prize?.amount) }}</strong><p class="text-sm text-gray-500 dark:text-dark-300">{{ t('welfare.drawSuccess', { count: overview?.available_draws || 0 }) }}</p></div>
        <template #footer><button class="btn btn-secondary" @click="dialog = ''">{{ t(dialog === 'result' ? 'welfare.accept' : 'welfare.close') }}</button><button v-if="dialog === 'result'" class="btn btn-primary" @click="dialog = ''; showRedemption = true">{{ t('welfare.redeem') }}</button></template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>
