<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { qqInviteAPI, type InviteID, type InvitePage, type InviteRecord } from '@/api/qqInvite'
import { useInviteAdmin, useInviteBusy } from '@/composables/useInviteAdmin'
const props = defineProps<{ campaignId: InviteID }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const { t } = useI18n()
const records = ref<InvitePage<InviteRecord> | null>(null), page = ref(1), filter = ref(''), reviewTarget = ref<InviteRecord | null>(null)
const inviter = ref(''), reason = ref(''), validation = ref(false), resendTarget = ref<InviteRecord | null>(null)
const statuses = ['pending_attribution', 'pending_level', 'ineligible', 'baseline', 'queued_inventory', 'awarded']
const { session, busy, blocked, error, notice, run } = useInviteAdmin(() => { records.value = null; reviewTarget.value = null; resendTarget.value = null; inviter.value = ''; reason.value = ''; validation.value = false })
useInviteBusy(busy, value => emit('busy', value))
async function fetchRecords() { records.value = null; const value = await qqInviteAPI.records(props.campaignId, filter.value, page.value, session.signal); if (session.current()) records.value = value }
function reload() { return run(fetchRecords) }
function changeFilter() { page.value = 1; void reload() }
function openReview(record: InviteRecord) { if (blocked.value || !session.current()) return; reviewTarget.value = record; inviter.value = ''; reason.value = ''; validation.value = false; resendTarget.value = null }
function cancelReview() { reviewTarget.value = null; inviter.value = ''; reason.value = ''; validation.value = false }
function review() {
  if (!reviewTarget.value || !/^[1-9]\d{4,19}$/.test(inviter.value.trim()) || !reason.value.trim()) { validation.value = true; return }
  const target = reviewTarget.value.id
  return run(async () => { await qqInviteAPI.review(target, inviter.value.trim(), reason.value.trim(), session.signal); if (!session.current()) return; cancelReview(); notice.value = 'reviewed'; await fetchRecords() })
}
function recheck(record: InviteRecord) { return run(async () => { await qqInviteAPI.recheck(record.id, session.signal); if (!session.current()) return; notice.value = 'rechecked'; await fetchRecords() }) }
function redeliver() {
  const target = resendTarget.value
  if (!target?.reward_id || !canResend(target)) return
  return run(async () => { await qqInviteAPI.redeliver(target.reward_id!, session.signal); if (!session.current()) return; resendTarget.value = null; notice.value = 'redelivered'; await fetchRecords() })
}
function canResend(record: InviteRecord) { return !!record.reward_id && ['failed', 'sent', 'unknown', 'blocked'].includes(record.private_status || '') }
function delivery(value?: string) { return ['pending', 'sending', 'sent', 'failed', 'unknown', 'blocked'].includes(value || '') ? value : 'unknown' }
function move(delta: number) { if (blocked.value || page.value + delta < 1) return; page.value += delta; void reload() }
onMounted(reload)
</script>

<template>
  <section class="space-y-4">
    <p v-if="session.invalidated.value" role="alert">{{ t('qqInvite.sessionChanged') }}</p>
    <template v-else>
      <div class="flex flex-wrap items-center justify-between gap-3"><h3 class="font-semibold">{{ t('qqInvite.records') }}</h3><div class="flex gap-2"><select v-model="filter" :aria-label="t('qqInvite.status')" class="input" :disabled="blocked" @change="changeFilter"><option value="">{{ t('qqInvite.allStatuses') }}</option><option v-for="status in statuses" :key="status" :value="status">{{ t(`qqInvite.recordStates.${status}`) }}</option></select><button class="btn btn-secondary" :disabled="blocked" @click="reload">{{ t('qqInvite.reload') }}</button></div></div>
      <p class="rounded-lg bg-amber-50 p-4 text-sm leading-relaxed text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('qqInvite.attributionHint') }}</p>
      <p v-if="busy" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert" class="text-red-600">{{ t(`qqInvite.${error}`) }}</p><p v-if="notice" role="status" class="text-green-700 dark:text-green-400">{{ t(`qqInvite.${notice}`) }}</p>
      <form v-if="reviewTarget" data-test="review-form" class="card space-y-3 p-5" @submit.prevent="review"><h4 class="font-semibold">{{ t('qqInvite.review') }} · {{ reviewTarget.invitee_qq }}</h4><fieldset :disabled="blocked" class="space-y-3"><label class="block text-sm">{{ t('qqInvite.inviter') }}<input v-model="inviter" name="inviter_qq" inputmode="numeric" required class="input mt-1 w-full" /></label><label class="block text-sm">{{ t('qqInvite.reviewReason') }}<textarea v-model="reason" name="review_reason" rows="3" maxlength="500" required class="input mt-1 w-full" /></label><p v-if="validation" role="alert" class="text-red-600">{{ t('qqInvite.reviewRequired') }}</p><div class="flex gap-2"><button type="submit" class="btn btn-primary">{{ t('qqInvite.confirmReview') }}</button><button type="button" class="btn btn-secondary" @click="cancelReview">{{ t('common.cancel') }}</button></div></fieldset></form>
      <div v-if="resendTarget" class="card space-y-3 p-5" role="alert"><p>{{ t('qqInvite.redeliverHint') }} · {{ resendTarget.inviter_qq }} · {{ resendTarget.code_hint }}</p><button data-test="confirm-redeliver" class="btn btn-primary mr-2" :disabled="blocked" @click="redeliver">{{ t('qqInvite.confirmRedeliver') }}</button><button class="btn btn-secondary" :disabled="blocked" @click="resendTarget = null">{{ t('common.cancel') }}</button></div>
      <div v-if="records" class="card space-y-4 p-5">
        <p v-if="!records.items?.length" class="text-sm text-gray-500">{{ t('qqInvite.noRecords') }}</p>
        <div v-else class="overflow-x-auto"><table class="w-full text-left text-sm"><thead><tr class="border-b border-gray-100 dark:border-dark-700"><th class="p-2">{{ t('qqInvite.member') }}</th><th class="p-2">{{ t('qqInvite.inviter') }}</th><th class="p-2">{{ t('qqInvite.status') }}</th><th class="p-2">{{ t('qqInvite.delivery') }}</th><th class="p-2">{{ t('qqInvite.actions') }}</th></tr></thead><tbody><tr v-for="record in records.items" :key="record.id" class="border-b border-gray-100 align-top dark:border-dark-700"><td class="space-y-1 p-2"><div>{{ record.invitee_qq }}</div><div class="text-xs text-gray-500">{{ t('qqInvite.group') }} {{ record.group_id }}</div><div class="text-xs text-gray-500">{{ t('qqInvite.level') }} {{ record.qq_level ?? '—' }}</div><div class="text-xs text-gray-500">{{ new Date(record.created_at).toLocaleString() }}</div></td><td class="p-2">{{ record.inviter_qq || t('qqInvite.unverified') }}</td><td class="space-y-1 p-2"><div>{{ t(`qqInvite.recordStates.${statuses.includes(record.status) ? record.status : 'unknown'}`) }}</div><div v-if="record.code_hint" class="font-mono text-xs">{{ record.code_hint }}</div></td><td class="space-y-1 p-2"><template v-if="record.reward_id"><div>{{ t('qqInvite.privateDelivery') }}: {{ t(`qqInvite.deliveryStates.${delivery(record.private_status)}`) }}</div><div>{{ t('qqInvite.groupDelivery') }}: {{ t(`qqInvite.deliveryStates.${delivery(record.group_status)}`) }}</div><p v-if="record.private_status === 'unknown'" class="max-w-56 text-xs text-amber-700 dark:text-amber-400">{{ t('qqInvite.unknownDeliveryHint') }}</p></template><span v-else>—</span></td><td class="p-2"><div class="flex flex-wrap gap-2"><button v-if="record.status === 'pending_attribution'" data-test="review-open" class="btn btn-secondary" :disabled="blocked" @click="openReview(record)">{{ t('qqInvite.review') }}</button><button class="btn btn-secondary" :disabled="blocked || ['awarded', 'baseline'].includes(record.status)" @click="recheck(record)">{{ t('qqInvite.recheck') }}</button><button v-if="record.reward_id" data-test="redeliver" class="btn btn-secondary" :disabled="blocked || !canResend(record)" @click="resendTarget = record; cancelReview()">{{ t('qqInvite.redeliver') }}</button></div></td></tr></tbody></table></div>
        <div class="flex items-center justify-end gap-3"><span class="text-sm">{{ t('qqInvite.total') }} {{ records.total }}</span><button class="btn btn-secondary" :disabled="blocked || page <= 1" @click="move(-1)">{{ t('qqInvite.previous') }}</button><span class="text-sm">{{ page }} / {{ Math.max(1, Math.ceil(records.total / 20)) }}</span><button class="btn btn-secondary" :disabled="blocked || page * 20 >= records.total" @click="move(1)">{{ t('qqInvite.next') }}</button></div>
      </div>
    </template>
  </section>
</template>
