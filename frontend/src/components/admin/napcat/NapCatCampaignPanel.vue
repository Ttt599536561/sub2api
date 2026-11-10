<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { qqInviteAPI, type InviteCampaign, type InviteCampaignInput, type InviteID } from '@/api/qqInvite'
import { getAll } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import { useInviteAdmin, useInviteBusy } from '@/composables/useInviteAdmin'
import { localDateTime, validateInviteTemplates } from '@/utils/qqInvite'
import NapCatInventoryPanel from './NapCatInventoryPanel.vue'
import NapCatRecordsPanel from './NapCatRecordsPanel.vue'
const { t, locale } = useI18n()
const emit = defineEmits<{ busy: [value: boolean] }>()
const childBusy = ref(false)
const campaigns = ref<InviteCampaign[]>([]), groups = ref<AdminGroup[]>([]), selected = ref<InviteID | null>(null)
const tab = ref<'edit' | 'inventory' | 'records'>('edit'), groupText = ref(''), starts = ref(''), ends = ref(''), validation = ref('')
function defaults(): InviteCampaignInput {
  const en = locale.value.startsWith('en')
  return { version: 0, name: '', enabled: false, group_ids: [], subscription_group_id: 0, starts_at: '', ends_at: '',
    welcome_template: en ? 'Welcome {invitee}! {url}' : '欢迎 {invitee} 加入！{url}',
    congratulation_template: en ? 'Thank you {inviter}! Your monthly pass was sent privately.' : '恭喜 {inviter} 邀请成功，月卡兑换码已私信发送。',
    pending_template: en ? 'Welcome {invitee}. Invitation rewards are being verified.' : '欢迎 {invitee}，邀请奖励正在核验。',
    delivery_pending_template: en ? '{inviter}, your monthly pass reward is ready, but private delivery has not completed. Add the bot as a friend and send “领取奖励” in a private message.' : '{inviter}，已获得月卡奖励，但私信尚未完成。请添加机器人好友后私信“领取奖励”。',
    private_template: en ? 'Thank you! Monthly pass code: {code}\nRedeem: {url}' : '感谢邀请！月卡兑换码：{code}\n兑换地址：{url}' }
}
const form = reactive<InviteCampaignInput>(defaults())
const { session, busy, blocked, error, notice, run } = useInviteAdmin(() => { campaigns.value = []; groups.value = []; selected.value = null; Object.assign(form, defaults()); groupText.value = ''; starts.value = ''; ends.value = ''; validation.value = '' })
const controlsBlocked = computed(() => blocked.value || childBusy.value)
useInviteBusy(computed(() => busy.value || childBusy.value), value => emit('busy', value))
const current = computed(() => campaigns.value.find(item => item.id === selected.value))
const subscriptionGroups = computed(() => groups.value.filter(group => group.subscription_type === 'subscription'))
const templates = ['welcome_template', 'congratulation_template', 'pending_template', 'delivery_pending_template', 'private_template'] as const
function choose(id: InviteID | null) {
  if (controlsBlocked.value || !session.current()) return
  selected.value = id; tab.value = 'edit'; validation.value = ''; notice.value = ''
  const campaign = campaigns.value.find(item => item.id === id)
  Object.assign(form, campaign ? { version: campaign.version, name: campaign.name, enabled: campaign.enabled, group_ids: [...campaign.group_ids], subscription_group_id: campaign.subscription_group_id, starts_at: campaign.starts_at, ends_at: campaign.ends_at, welcome_template: campaign.welcome_template, congratulation_template: campaign.congratulation_template, pending_template: campaign.pending_template, delivery_pending_template: campaign.delivery_pending_template, private_template: campaign.private_template } : defaults())
  groupText.value = form.group_ids.join('\n'); starts.value = localDateTime(form.starts_at); ends.value = localDateTime(form.ends_at)
}
async function fetchCampaigns(preferred = selected.value) {
  const result = await qqInviteAPI.campaigns(session.signal)
  if (!session.current()) return
  campaigns.value = result.items || []
  const target = campaigns.value.find(item => item.id === preferred) || campaigns.value[0]
  // Selection is applied after the guarded operation releases its busy state.
  return target?.id ?? null
}
async function reload() {
  let target: InviteID | null | undefined
  await run(async () => { const [result, availableGroups] = await Promise.all([qqInviteAPI.campaigns(session.signal), getAll()]); if (!session.current()) return; campaigns.value = result.items || []; groups.value = availableGroups; target = campaigns.value.find(item => item.id === selected.value)?.id ?? campaigns.value[0]?.id ?? null })
  if (target !== undefined) choose(target)
}
function validate() {
  validation.value = validateInviteTemplates({ welcome_template: form.welcome_template, congratulation_template: form.congratulation_template, pending_template: form.pending_template, delivery_pending_template: form.delivery_pending_template, private_template: form.private_template })
  if (validation.value) return false
  const groupIDs = [...new Set(groupText.value.split(/[\s,，]+/).filter(Boolean))]
  if (!form.name.trim() || !groupIDs.length || groupIDs.some(group => !/^[1-9]\d{4,19}$/.test(group)) || !subscriptionGroups.value.some(group => group.id === form.subscription_group_id)) { validation.value = 'campaignFields'; return false }
  const start = new Date(starts.value), end = new Date(ends.value)
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || start >= end) { validation.value = 'dates'; return false }
  form.group_ids = groupIDs; form.starts_at = start.toISOString(); form.ends_at = end.toISOString()
  return true
}
async function save() {
  if (blocked.value || !session.current() || !validate()) return
  let result: InviteCampaign | undefined
  await run(async () => { result = await qqInviteAPI.saveCampaign(selected.value, { ...form, name: form.name.trim(), enabled: selected.value ? form.enabled : false }, session.signal); if (!session.current()) return; await fetchCampaigns(result.id) })
  if (result && session.current() && !error.value) { choose(result.id); notice.value = 'saved' }
}
async function baseline() {
  if (!current.value) return
  const id = current.value.id
  await run(async () => { await qqInviteAPI.baseline(id, session.signal); if (session.current()) await fetchCampaigns(id) })
  if (session.current() && !error.value) { choose(id); notice.value = 'baselineSaved' }
}
async function toggle() {
  if (!current.value || (!current.value.enabled && !current.value.baseline_ready)) return
  const campaign = current.value, id = campaign.id
  await run(async () => { await qqInviteAPI.saveCampaign(id, { version: campaign.version, name: campaign.name, enabled: !campaign.enabled, group_ids: campaign.group_ids, subscription_group_id: campaign.subscription_group_id, starts_at: campaign.starts_at, ends_at: campaign.ends_at, welcome_template: campaign.welcome_template, congratulation_template: campaign.congratulation_template, pending_template: campaign.pending_template, delivery_pending_template: campaign.delivery_pending_template, private_template: campaign.private_template }, session.signal); if (session.current()) await fetchCampaigns(id) })
  if (session.current() && !error.value) { choose(id); notice.value = 'saved' }
}
onMounted(reload)
</script>

<template>
  <section class="space-y-4">
    <div class="flex flex-wrap justify-between gap-3"><h3 class="text-lg font-semibold">{{ t('qqInvite.campaigns') }}</h3><div class="flex gap-2"><button class="btn btn-secondary" :disabled="controlsBlocked" @click="reload">{{ t('qqInvite.reload') }}</button><button data-test="campaign-new" class="btn btn-primary" :disabled="controlsBlocked" @click="choose(null)">{{ t('qqInvite.createCampaign') }}</button></div></div>
    <p v-if="busy" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert" class="text-red-600">{{ t(`qqInvite.${error}`) }}</p><p v-if="notice" role="status" class="text-green-700 dark:text-green-400">{{ t(`qqInvite.${notice}`) }}</p>
    <p v-if="session.invalidated.value" role="alert">{{ t('qqInvite.sessionChanged') }}</p>
    <template v-else>
      <p class="rounded-lg bg-primary-50 p-4 text-sm leading-relaxed text-primary-800 dark:bg-primary-900/20 dark:text-primary-200">{{ t('qqInvite.rules') }}</p>
      <label class="block text-sm">{{ t('qqInvite.selectCampaign') }}<select :value="selected ?? ''" class="input mt-1 w-full" :disabled="controlsBlocked" @change="choose(($event.target as HTMLSelectElement).value ? campaigns.find(item => String(item.id) === ($event.target as HTMLSelectElement).value)?.id ?? null : null)"><option value="">{{ t('qqInvite.newCampaign') }}</option><option v-for="campaign in campaigns" :key="campaign.id" :value="campaign.id">{{ campaign.name }} · {{ t(campaign.enabled ? 'qqInvite.active' : 'qqInvite.paused') }}</option></select></label>
      <p v-if="!busy && !campaigns.length" class="text-sm text-gray-500">{{ t('qqInvite.noCampaigns') }}</p>
      <div v-if="current" class="flex flex-wrap items-center gap-2"><button v-for="item in ['edit', 'inventory', 'records'] as const" :key="item" class="btn" :class="tab === item ? 'btn-primary' : 'btn-secondary'" :disabled="controlsBlocked" @click="tab = item">{{ t(`qqInvite.${item}`) }}</button></div>
      <div v-if="tab === 'edit'" class="card space-y-4 p-5">
        <div v-if="current" class="space-y-3"><div class="flex flex-wrap items-center gap-2"><span class="text-xs text-gray-500">{{ t('qqInvite.version') }} {{ current.version }}</span><button data-test="baseline" class="btn btn-secondary" :disabled="controlsBlocked || current.enabled" @click="baseline">{{ t('qqInvite.baseline') }}</button><button data-test="campaign-toggle" class="btn btn-secondary" :disabled="controlsBlocked || (!current.enabled && !current.baseline_ready)" @click="toggle">{{ t(current.enabled ? 'qqInvite.pauseCampaign' : 'qqInvite.enableCampaign') }}</button></div><p class="text-sm text-gray-500">{{ t(current.baseline_ready ? 'qqInvite.baselineReady' : 'qqInvite.baselineRequired') }}</p></div>
        <form data-test="campaign-form" @submit.prevent="save"><fieldset :disabled="controlsBlocked" class="space-y-4">
          <div class="grid gap-4 md:grid-cols-2"><label class="text-sm">{{ t('qqInvite.name') }}<input v-model="form.name" name="campaign_name" maxlength="120" required class="input mt-1 w-full" /></label><label class="text-sm">{{ t('qqInvite.subscriptionGroup') }}<select v-model="form.subscription_group_id" class="input mt-1 w-full" required><option :value="0" disabled>{{ t('qqInvite.selectGroup') }}</option><option v-for="group in subscriptionGroups" :key="group.id" :value="group.id">{{ group.name }} (#{{ group.id }})</option></select></label></div>
          <p v-if="!subscriptionGroups.length" class="text-sm text-amber-700 dark:text-amber-400">{{ t('qqInvite.noGroups') }}</p>
          <label class="block text-sm">{{ t('qqInvite.groupIDs') }}<textarea v-model="groupText" name="group_ids" rows="2" required class="input mt-1 w-full" /></label>
          <div class="grid gap-4 md:grid-cols-2"><label class="text-sm">{{ t('qqInvite.startsAt') }}<input v-model="starts" name="starts_at" type="datetime-local" required class="input mt-1 w-full" /></label><label class="text-sm">{{ t('qqInvite.endsAt') }}<input v-model="ends" name="ends_at" type="datetime-local" required class="input mt-1 w-full" /></label></div>
          <p class="text-xs text-gray-500">{{ t('qqInvite.localTime') }}</p>
          <p class="text-sm text-gray-500">{{ t('qqInvite.templateHint') }} <code>{invitee} {inviter} {level} {url} {plan_name}</code> · {{ t('qqInvite.privateOnly') }} <code>{code}</code></p>
          <div class="grid gap-4 lg:grid-cols-2"><label v-for="key in templates" :key="key" class="text-sm">{{ t(`qqInvite.${key}`) }}<textarea v-model="form[key]" :name="key" rows="4" maxlength="2000" required class="input mt-1 w-full" /></label></div>
          <p v-if="validation" role="alert" class="text-sm text-red-600">{{ t(`qqInvite.validation.${validation}`) }}</p>
          <div class="flex justify-end"><button type="submit" class="btn btn-primary">{{ t('common.save') }}</button></div>
        </fieldset></form>
      </div>
      <NapCatInventoryPanel v-if="tab === 'inventory' && current" :key="`stock-${current.id}`" :campaign-id="current.id" @busy="childBusy = $event" />
      <NapCatRecordsPanel v-if="tab === 'records' && current" :key="`records-${current.id}`" :campaign-id="current.id" @busy="childBusy = $event" />
    </template>
  </section>
</template>
