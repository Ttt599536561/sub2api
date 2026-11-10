<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import { qqInviteAPI, type InviteConfig, type InviteConfigInput, type InviteConnection } from '@/api/qqInvite'
import { useQQSession } from '@/composables/useQQSession'
import { useInviteBusy } from '@/composables/useInviteAdmin'
import { connectionState, inviteError } from '@/utils/qqInvite'
const { t } = useI18n()
const emit = defineEmits<{ busy: [value: boolean] }>()
const configuration = ref<InviteConfig | null>(null), connection = ref<InviteConnection | null>(null)
const form = reactive<InviteConfigInput>({ version: 0, enabled: false, onebot_url: '', webui_url: '', bot_qq: '', public_base_url: '' })
const secretKeys = ['onebot_token', 'webui_token', 'event_secret'] as const
const fields = ['onebot_url', 'webui_url', 'bot_qq', 'public_base_url'] as const
const secrets = reactive({ onebot_token: '', webui_token: '', event_secret: '' })
const busy = ref(false), error = ref(''), notice = ref(''), qrImage = ref(''), restartConfirmation = ref(false)
useInviteBusy(busy, value => emit('busy', value))
let qrSequence = 0
const session = useQQSession(clear, true)
const dirty = computed(() => !!configuration.value && (fields.some(key => form[key] !== configuration.value![key]) || form.enabled !== configuration.value.enabled || secretKeys.some(key => !!secrets[key])))
const blocked = computed(() => busy.value || session.invalidated.value)
const loginPhase = computed(() => ['waiting_qrcode', 'generating_qrcode', 'qrcode_scanned', 'initializing', 'ready', 'offline', 'reconnecting'].includes(connection.value?.login_phase || '') ? connection.value!.login_phase : 'unknown')
function clearQR() { qrSequence++; qrImage.value = ''; restartConfirmation.value = false }
function clear() { configuration.value = null; connection.value = null; clearQR(); secretKeys.forEach(key => { secrets[key] = '' }); fields.forEach(key => { form[key] = '' }); error.value = ''; notice.value = ''; busy.value = false }
watch([() => ({ ...form }), () => ({ ...secrets })], () => { clearQR(); connection.value = null }, { flush: 'sync' })
function accept(value: InviteConfig) {
  configuration.value = value
  form.version = value.version; form.enabled = value.enabled
  fields.forEach(key => { form[key] = value[key] }); secretKeys.forEach(key => { secrets[key] = '' })
}
async function run(action: () => Promise<void>) {
  if (!session.current() || busy.value) return
  busy.value = true; error.value = ''; notice.value = ''
  try { await action() } catch (cause) { if (session.current()) error.value = inviteError(cause) }
  finally { if (session.current()) busy.value = false }
}
async function status() {
  connection.value = null
  const value = await qqInviteAPI.connection(session.signal)
  if (session.current()) { connection.value = value; if (value.online) clearQR() }
}
function load() { return run(async () => { clearQR(); connection.value = null; const value = await qqInviteAPI.config(session.signal); if (!session.current()) return; accept(value); await status() }) }
function refreshStatus() { if (!dirty.value) return run(status) }
function save() { return run(async () => {
  clearQR()
  const body: InviteConfigInput = { ...form }
  secretKeys.forEach(key => { if (secrets[key]) body[key] = secrets[key] })
  const value = await qqInviteAPI.saveConfig(body, session.signal)
  if (!session.current()) return
  accept(value); notice.value = 'saved'; await status()
}) }
function login(action: 'qrcode' | 'refresh' | 'restart') {
  if (!configuration.value || dirty.value) return
  return run(async () => {
    clearQR(); const sequence = qrSequence
    const value = await qqInviteAPI.login(action, session.signal)
    if (!session.current() || sequence !== qrSequence) return
    if (value.restarting) notice.value = 'restarting'
    if (value.url) {
      const image = await QRCode.toDataURL(value.url, { width: 280, margin: 2, errorCorrectionLevel: 'M' })
      if (session.current() && sequence === qrSequence) qrImage.value = image
    } else if (!value.restarting) notice.value = 'qrUnavailable'
  })
}
onMounted(load)
</script>

<template>
  <section class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3"><h3 class="text-lg font-semibold">{{ t('qqInvite.connection') }}</h3><button class="btn btn-secondary" :disabled="blocked" @click="load">{{ t('qqInvite.reload') }}</button></div>
    <p v-if="session.invalidated.value" role="alert" class="text-amber-700 dark:text-amber-400">{{ t('qqInvite.sessionChanged') }}</p>
    <p v-if="busy" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ t(`qqInvite.${error}`) }}</p>
    <p v-if="notice" role="status" class="text-sm text-green-700 dark:text-green-400">{{ t(`qqInvite.${notice}`) }}</p>
    <template v-if="configuration && !session.invalidated.value">
      <form class="card space-y-4 p-5" autocomplete="off" @submit.prevent="save">
        <fieldset :disabled="blocked" class="space-y-4">
          <div class="flex flex-wrap items-center justify-between gap-3"><label class="flex items-center gap-2"><input v-model="form.enabled" name="enabled" type="checkbox" :disabled="!configuration.encryption_available" />{{ t('qqInvite.enabled') }}</label><span class="text-xs text-gray-500">{{ t('qqInvite.version') }} {{ form.version }}</span></div>
          <div class="grid gap-4 md:grid-cols-2"><label v-for="field in fields" :key="field" class="space-y-1 text-sm"><span>{{ t(`qqInvite.${field}`) }}</span><input v-model="form[field]" :name="field" class="input w-full" :type="field === 'bot_qq' ? 'text' : 'url'" :inputmode="field === 'bot_qq' ? 'numeric' : 'url'" :required="form.enabled" /></label></div>
          <p class="text-xs text-gray-500">{{ t('qqInvite.serverURLs') }}</p>
          <div class="grid gap-4 md:grid-cols-3"><label v-for="key in secretKeys" :key="key" class="space-y-1 text-sm"><span>{{ t(`qqInvite.${key}`) }} · {{ t(configuration[`${key}_configured`] ? 'qqInvite.configured' : 'qqInvite.notConfigured') }}</span><input v-model="secrets[key]" :name="key" type="password" autocomplete="new-password" class="input w-full" :placeholder="t('qqInvite.keepSecret')" :disabled="!configuration.encryption_available" /></label></div>
          <p v-if="!configuration.encryption_available" class="text-sm text-amber-700 dark:text-amber-400">{{ t('qqInvite.encryptionRequired') }}</p>
          <div class="flex justify-end"><button class="btn btn-primary" type="submit">{{ t('common.save') }}</button></div>
        </fieldset>
      </form>
      <div class="card space-y-4 p-5">
        <div class="flex flex-wrap items-center gap-3"><span data-test="connection-state" class="rounded-full bg-gray-100 px-3 py-1 text-sm font-medium dark:bg-dark-700" :class="connection?.online ? 'text-green-700 dark:text-green-400' : 'text-amber-700 dark:text-amber-400'">{{ t(`qqInvite.states.${connectionState(connection)}`) }}</span><span v-if="connection?.qq" class="text-sm">QQ {{ connection.qq }} {{ connection.nickname }}</span><span v-if="connection?.core_ready" class="text-xs text-gray-500">{{ t('qqInvite.coreReady') }}</span></div>
        <p class="text-sm text-gray-500">{{ t('qqInvite.loginHint') }}</p>
        <p v-if="connection" class="text-sm text-gray-500">{{ t(`qqInvite.loginPhases.${loginPhase}`) }}<span v-if="connection.qr_login_accepted"> · {{ t('qqInvite.qrAccepted') }}</span></p>
        <p v-if="dirty" class="text-sm text-amber-700 dark:text-amber-400">{{ t('qqInvite.saveFirst') }}</p>
        <div class="flex flex-wrap gap-2">
          <button data-test="connection-refresh" class="btn btn-secondary" :disabled="blocked || dirty" @click="refreshStatus">{{ t('qqInvite.refreshStatus') }}</button>
          <button data-test="login-qrcode" class="btn btn-primary" :disabled="blocked || dirty" @click="login('qrcode')">{{ t('qqInvite.qrcode') }}</button>
          <button data-test="login-refresh" class="btn btn-secondary" :disabled="blocked || dirty" @click="login('refresh')">{{ t('qqInvite.refreshQR') }}</button>
          <button data-test="login-restart" class="btn btn-secondary" :disabled="blocked || dirty" @click="restartConfirmation = true">{{ t('qqInvite.restart') }}</button>
        </div>
        <div v-if="restartConfirmation" class="space-y-3 rounded-lg border border-amber-300 p-4" role="alert"><p>{{ t('qqInvite.restartWarning') }}</p><button data-test="confirm-restart" class="btn btn-primary mr-2" :disabled="blocked" @click="login('restart')">{{ t('qqInvite.confirmRestart') }}</button><button class="btn btn-secondary" :disabled="blocked" @click="restartConfirmation = false">{{ t('common.cancel') }}</button></div>
        <div v-if="qrImage" class="space-y-2"><img :src="qrImage" :alt="t('qqInvite.qrAlt')" width="280" height="280" class="rounded-lg bg-white" /><p class="text-sm text-gray-500">{{ t('qqInvite.scanHint') }}</p></div>
      </div>
    </template>
  </section>
</template>
