<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminQQImageAPI, type QQConfig, type QQConfigInput, type QQOrder, type QQGrant, type QQAudit } from '@/api/qqImage'
import { useQQSession } from '@/composables/useQQSession'
import { qqErrorKey } from '@/utils/qqImage'
import { qqConfigNumberLimits, qqConfigSaveProblem, validateQQConfigInput, type QQConfigField, type QQConfigProblem } from '@/utils/qqImageConfig'
import QQDiscoveryPanel from './QQDiscoveryPanel.vue'
import QQConnectionPanel from './QQConnectionPanel.vue'
import QQReplyTemplatesPanel from './QQReplyTemplatesPanel.vue'
import QQOrderCard from '@/components/user/QQOrderCard.vue'
import '@/components/user/qqImage.css'
const { t } = useI18n()
const emit = defineEmits<{ busy: [value: boolean] }>()
const tabs = ['config', 'templates', 'orders', 'grants', 'audit'] as const
const tab = ref<typeof tabs[number]>('config'), config = ref<QQConfig | null>(null)
const botSecret = ref(''), apiKey = ref(''), groups = ref(''), loading = ref(false), busy = ref(false), error = ref(''), notice = ref('')
const clearBotSecret = ref(false), clearAPIKey = ref(false)
const imageHosts = ref('')
const configProblem = ref<QQConfigProblem | null>(null), configForm = ref<HTMLFormElement | null>(null)
const templatesVisited = ref(false), templatesBusy = ref(false), reloadConfirmation = ref(false)
const discoveryBusy = ref(false), configVersion = ref(0), savedConfig = ref('')
const configFingerprint = computed(() => JSON.stringify([config.value, groups.value, imageHosts.value]))
const configDirty = computed(() => configFingerprint.value !== savedConfig.value || !!botSecret.value || !!apiKey.value || clearBotSecret.value || clearAPIKey.value)
const orders = ref<QQOrder[]>([]), grants = ref<QQGrant[]>([]), audit = ref<QQAudit[]>([])
const action = ref<{ kind: 'refund' | 'revoke' | 'reconcile' | 'release'; id: string } | null>(null), reason = ref(''), imageURL = ref(''), imageID = ref('')
const actionPanel = ref<HTMLElement | null>(null)
let sequence = 0
const numberFields = Object.keys(qqConfigNumberLimits) as (keyof typeof qqConfigNumberLimits)[]
function clearImage() { if (imageURL.value) URL.revokeObjectURL(imageURL.value); imageURL.value = ''; imageID.value = '' }
const session = useQQSession(() => { sequence++; config.value = null; configProblem.value = null; botSecret.value = ''; apiKey.value = ''; clearBotSecret.value = false; clearAPIKey.value = false; groups.value = ''; imageHosts.value = ''; savedConfig.value = ''; discoveryBusy.value = false; templatesBusy.value = false; templatesVisited.value = false; reloadConfirmation.value = false; orders.value = []; grants.value = []; audit.value = []; action.value = null; reason.value = ''; loading.value = false; busy.value = false; error.value = ''; notice.value = ''; clearImage() }, true)
const configProblemLabel = computed(() => {
  const field = configProblem.value?.field
  if (!field || field === 'request') return ''
  if (field === 'receive_mode') return t('qqImage.admin.receiveMode')
  if (field === 'image_hosts') return t('qqImage.admin.imageHostsLabel')
  if (field === 'model' || field === 'size') return t(`qqImage.${field}`)
  return t(`qqImage.admin.fields.${field}`)
})
const configProblemText = computed(() => {
  const problem = configProblem.value
  if (!problem) return ''
  if (problem.issue === 'out_of_range') return t('qqImage.admin.configValidation.out_of_range', { max: qqConfigNumberLimits[problem.field as keyof typeof qqConfigNumberLimits] })
  if (problem.issue === 'invalid_format') return t(`qqImage.admin.configValidation.formats.${problem.field}`)
  if (problem.issue === 'invalid_url') return t(`qqImage.admin.configValidation.urls.${problem.field}`)
  return t(`qqImage.admin.configValidation.${problem.issue}`)
})
function fieldState(field: QQConfigField) {
  return configProblem.value?.field === field ? { 'aria-invalid': 'true' as const, 'aria-describedby': 'qq-config-error' } : {}
}
async function showConfigProblem(problem: QQConfigProblem) {
  configProblem.value = problem
  await nextTick()
  if (session.current() && configProblem.value?.field === problem.field && configProblem.value?.issue === problem.issue && problem.field !== 'request') configForm.value?.querySelector<HTMLElement>(`[name="${problem.field}"]`)?.focus()
}
function accept(value: QQConfig) {
  configProblem.value = null
  // Clear edit-only removal intent before installing the authoritative enabled value.
  clearBotSecret.value = false; clearAPIKey.value = false
  // Copy only public fields; never bind an unexpected credential response to a form.
  config.value = {
    enabled: value.enabled, receive_mode: value.receive_mode === 'websocket' ? 'websocket' : 'webhook', app_id: value.app_id, api_url: value.api_url, model: value.model, size: value.size,
    price: value.price, allowed_groups: value.allowed_groups || [], public_base_url: value.public_base_url,
    user_daily_limit: value.user_daily_limit, user_daily_amount: value.user_daily_amount,
    group_concurrency: value.group_concurrency, global_concurrency: value.global_concurrency, queue_limit: value.queue_limit,
    queue_timeout_seconds: value.queue_timeout_seconds, deadline_seconds: value.deadline_seconds, retention_days: value.retention_days,
    prompt_max_chars: value.prompt_max_chars, bot_secret_configured: value.bot_secret_configured, api_key_configured: value.api_key_configured, encryption_available: value.encryption_available,
    image_hosts: value.image_hosts || []
  }
  groups.value = config.value.allowed_groups.join('\n'); botSecret.value = ''; apiKey.value = ''
  imageHosts.value = (config.value.image_hosts || []).join('\n')
  savedConfig.value = configFingerprint.value; configVersion.value++
}
function requestRefresh() {
  if (!session.current() || busy.value || discoveryBusy.value || templatesBusy.value || loading.value) return
  if (tab.value === 'config' && config.value && configDirty.value) reloadConfirmation.value = true
  else void refresh()
}
async function refresh() {
  if (!session.current() || busy.value || discoveryBusy.value || tab.value === 'templates') return
  const ticket = ++sequence, selected = tab.value
  loading.value = true; error.value = ''; reloadConfirmation.value = false
  try {
    if (selected === 'config') { const value = await adminQQImageAPI.config(session.signal); if (session.current() && ticket === sequence) accept(value) }
    if (selected === 'orders') { const value = await adminQQImageAPI.orders(session.signal); if (session.current() && ticket === sequence) orders.value = value || [] }
    if (selected === 'grants') { const value = await adminQQImageAPI.grants(session.signal); if (session.current() && ticket === sequence) grants.value = value || [] }
    if (selected === 'audit') { const value = await adminQQImageAPI.audit(session.signal); if (session.current() && ticket === sequence) audit.value = value || [] }
  } catch (e) { if (session.current() && ticket === sequence) error.value = qqErrorKey(e) }
  finally { if (session.current() && ticket === sequence) loading.value = false }
}
async function save() {
  if (!session.current() || busy.value || discoveryBusy.value || loading.value || !config.value) return
  const value = config.value
  error.value = ''; notice.value = ''; configProblem.value = null
  const hosts = [...new Set(imageHosts.value.split(/[\n,]/).map(host => host.trim().toLowerCase()).filter(Boolean))]
  const body: QQConfigInput = {
    enabled: value.enabled && !clearBotSecret.value && !clearAPIKey.value, receive_mode: value.receive_mode || 'webhook', app_id: value.app_id.trim(), api_url: value.api_url.trim(), model: value.model.trim(), size: value.size.trim(), price: value.price.trim(),
    public_base_url: value.public_base_url.trim(), allowed_groups: [...new Set(groups.value.split(/[\n,]/).map(group => group.trim()).filter(Boolean))],
    image_hosts: hosts,
    user_daily_limit: value.user_daily_limit, user_daily_amount: value.user_daily_amount.trim(), group_concurrency: value.group_concurrency, global_concurrency: value.global_concurrency,
    queue_limit: value.queue_limit, queue_timeout_seconds: value.queue_timeout_seconds, deadline_seconds: value.deadline_seconds, retention_days: value.retention_days, prompt_max_chars: value.prompt_max_chars,
    ...(clearBotSecret.value ? { clear_bot_secret: true } : botSecret.value ? { bot_secret: botSecret.value } : {}),
    ...(clearAPIKey.value ? { clear_api_key: true } : apiKey.value ? { api_key: apiKey.value } : {})
  }
  const problem = validateQQConfigInput(body, value)
  if (problem) { await showConfigProblem(problem); return }
  busy.value = true
  try { const result = await adminQQImageAPI.saveConfig(body, session.signal); if (session.current()) { accept(result); notice.value = 'saved' } }
  catch (e) { if (session.current()) { busy.value = false; await showConfigProblem(qqConfigSaveProblem(e)) } }
  finally { if (session.current()) busy.value = false }
}
async function prepare(kind: 'refund' | 'revoke' | 'reconcile' | 'release', id: string) {
  action.value = { kind, id }; reason.value = ''; notice.value = ''
  await nextTick()
  if (session.current() && action.value?.id === id) actionPanel.value?.focus()
}
async function confirmAction() {
  if (!session.current() || busy.value || !action.value || (action.value.kind !== 'reconcile' && !reason.value.trim())) return
  const target = action.value
  busy.value = true; error.value = ''; notice.value = ''
  try {
    if (target.kind === 'refund') await adminQQImageAPI.refund(target.id, reason.value.trim(), session.signal)
    if (target.kind === 'revoke') await adminQQImageAPI.revoke(target.id, reason.value.trim(), session.signal)
    if (target.kind === 'reconcile') await adminQQImageAPI.reconcile(target.id, session.signal)
    if (target.kind === 'release') await adminQQImageAPI.release(target.id, reason.value.trim(), session.signal)
    if (!session.current()) return
    action.value = null; reason.value = ''; notice.value = 'actionRecorded'; busy.value = false; await refresh()
  } catch (e) { if (session.current()) error.value = qqErrorKey(e) }
  finally { if (session.current()) busy.value = false }
}
async function loadImage(order: QQOrder) {
  if (!session.current() || busy.value) return
  busy.value = true; error.value = ''
  try {
    const blob = await adminQQImageAPI.image(order.id, session.signal)
    if (!session.current()) return
    if (!['image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(blob.type)) throw new Error('Invalid image type')
    clearImage(); imageURL.value = URL.createObjectURL(blob); imageID.value = order.id
  } catch (e) { if (session.current()) error.value = qqErrorKey(e) }
  finally { if (session.current()) busy.value = false }
}
watch(tab, selected => {
  sequence++; loading.value = false; action.value = null; reason.value = ''; error.value = ''; notice.value = ''; reloadConfirmation.value = false; clearImage()
  if (selected === 'templates') templatesVisited.value = true
  else if (selected !== 'config' || !config.value) void refresh()
})
watch([busy, discoveryBusy, templatesBusy], values => emit('busy', values.some(Boolean)), { flush: 'sync', immediate: true })
watch([clearBotSecret, clearAPIKey], ([bot, image]) => {
  if (config.value && (bot || image)) config.value.enabled = false
  if (bot) botSecret.value = ''
  if (image) apiKey.value = ''
}, { flush: 'sync' })
onMounted(refresh)
</script>

<template>
  <div class="space-y-5">
    <div class="qq-heading"><div><h2 class="text-lg font-semibold">{{ t('qqImage.admin.title') }}</h2><p class="qq-muted mt-1">{{ t('qqImage.admin.hint') }}</p></div><button v-if="tab !== 'templates'" class="btn btn-secondary" :disabled="loading || busy || discoveryBusy || session.invalidated.value" @click="requestRefresh">{{ t('qqImage.refresh') }}</button></div>
    <div v-if="session.invalidated.value" class="qq-notice" role="alert">{{ t('qqImage.sessionChanged') }}</div>
    <template v-else>
      <div role="tablist" :aria-label="t('qqImage.admin.title')" class="flex flex-wrap gap-2"><button v-for="item in tabs" :id="`qq-tab-${item}`" :key="item" :data-test="`qq-tab-${item}`" role="tab" :aria-selected="tab === item" :aria-controls="`qq-panel-${item}`" class="btn" :class="tab === item ? 'btn-primary' : 'btn-secondary'" :disabled="busy || discoveryBusy || templatesBusy" @click="tab = item">{{ t(`qqImage.admin.tabs.${item}`) }}</button></div>
      <div v-if="loading" class="qq-muted" role="status">{{ t('common.loading') }}</div><div v-if="error" class="qq-error" role="alert">{{ t(`qqImage.errors.${error}`) }}</div><div v-if="notice" class="qq-notice" role="status">{{ t(`qqImage.admin.${notice}`) }}</div>
      <section :id="`qq-panel-${tab}`" role="tabpanel" :aria-labelledby="`qq-tab-${tab}`" class="space-y-4">
        <QQReplyTemplatesPanel v-if="templatesVisited" v-show="tab === 'templates'" @busy="templatesBusy = $event" />
        <div v-if="reloadConfirmation && tab === 'config'" role="alertdialog" :aria-label="t('qqImage.refresh')" class="qq-notice space-y-3">
          <p>{{ t('qqImage.admin.templates.reloadConfirm') }}</p><div class="qq-actions"><button data-test="confirm-config-reload" class="btn btn-primary" :disabled="loading || busy || discoveryBusy" @click="refresh">{{ t('qqImage.admin.templates.confirmReload') }}</button><button data-test="cancel-config-reload" class="btn btn-secondary" :disabled="loading || busy || discoveryBusy" @click="reloadConfirmation = false">{{ t('common.cancel') }}</button></div>
        </div>
        <form v-if="tab === 'config' && config" ref="configForm" novalidate data-test="qq-config" class="space-y-5" @submit.prevent="save">
          <div v-if="configProblem" id="qq-config-error" data-test="config-error" class="qq-error" role="alert"><strong v-if="configProblemLabel">{{ configProblemLabel }}： </strong>{{ configProblemText }}</div>
          <div class="qq-panel space-y-4"><label class="flex items-start gap-3 text-sm"><input v-model="config.enabled" name="enabled" type="checkbox" class="mt-1" :disabled="busy || discoveryBusy || loading || clearBotSecret || clearAPIKey || (!config.encryption_available && !config.enabled)" /><span><strong>{{ t('qqImage.admin.enabled') }}</strong><span class="qq-muted mt-1 block">{{ t('qqImage.admin.enabledHint') }}</span></span></label><p v-if="!config.encryption_available" class="qq-notice">{{ t('qqImage.admin.encryptionMissing') }}</p></div>
          <div class="grid gap-5 lg:grid-cols-2">
            <fieldset class="qq-panel space-y-4" :disabled="busy || discoveryBusy || loading"><legend class="px-2 text-base font-semibold">{{ t('qqImage.admin.qqCredentials') }}</legend>
              <label class="qq-label">{{ t('qqImage.admin.receiveMode') }}<select v-model="config.receive_mode" name="receive_mode" v-bind="fieldState('receive_mode')" class="qq-input"><option value="webhook">{{ t('qqImage.admin.receiveModes.webhook') }}</option><option value="websocket">{{ t('qqImage.admin.receiveModes.websocket') }}</option></select><span class="qq-muted">{{ t('qqImage.admin.receiveModeHint') }}</span></label>
              <label class="qq-label">{{ t('qqImage.admin.fields.app_id') }}<input v-model="config.app_id" class="qq-input" name="app_id" v-bind="fieldState('app_id')" :required="config.enabled" autocomplete="off" /></label>
              <label class="qq-label">{{ t('qqImage.admin.fields.bot_secret') }}<input v-model="botSecret" class="qq-input" name="bot_secret" v-bind="fieldState('bot_secret')" type="password" autocomplete="new-password" :disabled="!config.encryption_available || clearBotSecret" :placeholder="t('qqImage.admin.secretPlaceholder')" /><span class="qq-muted">{{ config.bot_secret_configured ? t('qqImage.admin.configured') : t('qqImage.admin.notConfigured') }}</span></label>
              <label class="flex items-start gap-2 text-sm"><input v-model="clearBotSecret" name="clear_bot_secret" type="checkbox" class="mt-1" /><span>{{ t('qqImage.admin.clearBotSecret') }}</span></label><p v-if="clearBotSecret" class="qq-notice">{{ t('qqImage.admin.clearSecretHint') }}</p><label class="qq-label">{{ t('qqImage.admin.fields.public_base_url') }}<input v-model="config.public_base_url" class="qq-input" type="url" name="public_base_url" v-bind="fieldState('public_base_url')" :required="config.enabled" placeholder="https://" /></label><p class="qq-muted">{{ t('qqImage.admin.officialHint') }}</p>
            </fieldset>
            <fieldset class="qq-panel space-y-4" :disabled="busy || discoveryBusy || loading"><legend class="px-2 text-base font-semibold">{{ t('qqImage.admin.imageCredentials') }}</legend>
              <label class="qq-label">{{ t('qqImage.admin.fields.api_url') }}<input v-model="config.api_url" class="qq-input" type="url" name="api_url" v-bind="fieldState('api_url')" :required="config.enabled" placeholder="https://api.example.com/v1" /></label>
              <label class="qq-label">{{ t('qqImage.admin.fields.api_key') }}<input v-model="apiKey" class="qq-input" name="api_key" v-bind="fieldState('api_key')" type="password" autocomplete="new-password" :disabled="!config.encryption_available || clearAPIKey" :placeholder="t('qqImage.admin.secretPlaceholder')" /><span class="qq-muted">{{ config.api_key_configured ? t('qqImage.admin.configured') : t('qqImage.admin.notConfigured') }}</span></label>
              <label class="flex items-start gap-2 text-sm"><input v-model="clearAPIKey" name="clear_api_key" type="checkbox" class="mt-1" /><span>{{ t('qqImage.admin.clearAPIKey') }}</span></label><p v-if="clearAPIKey" class="qq-notice">{{ t('qqImage.admin.clearSecretHint') }}</p><div class="grid gap-4 sm:grid-cols-2"><label class="qq-label">{{ t('qqImage.model') }}<input v-model="config.model" class="qq-input" name="model" v-bind="fieldState('model')" :required="config.enabled" /></label><label class="qq-label">{{ t('qqImage.size') }}<input v-model="config.size" class="qq-input" name="size" v-bind="fieldState('size')" :required="config.enabled" /></label></div><p class="qq-muted">{{ t('qqImage.admin.providerHint') }}</p>
            </fieldset>
          </div>
          <QQConnectionPanel :dirty="configDirty" :version="configVersion" :loading="loading" :disabled="busy || discoveryBusy" />
          <fieldset class="qq-panel space-y-3" :disabled="busy || discoveryBusy || loading"><legend class="px-2 text-base font-semibold">{{ t('qqImage.admin.imageHosts') }}</legend><label class="qq-label">{{ t('qqImage.admin.imageHostsLabel') }}<textarea v-model="imageHosts" name="image_hosts" v-bind="fieldState('image_hosts')" class="qq-input min-h-24" placeholder="cdn.example.com" /></label><p class="qq-muted">{{ t('qqImage.admin.imageHostsHint') }}</p></fieldset>
          <fieldset class="qq-panel space-y-4" :disabled="busy || discoveryBusy || loading"><legend class="px-2 text-base font-semibold">{{ t('qqImage.admin.limits') }}</legend><div class="grid gap-4 sm:grid-cols-2">
            <label class="qq-label">{{ t('qqImage.admin.fields.price') }}<input v-model="config.price" class="qq-input" name="price" v-bind="fieldState('price')" inputmode="decimal" pattern="[0-9]{1,12}(\.[0-9]{1,8})?" :required="config.enabled" /></label>
            <label class="qq-label">{{ t('qqImage.admin.fields.user_daily_amount') }}<input v-model="config.user_daily_amount" class="qq-input" name="user_daily_amount" v-bind="fieldState('user_daily_amount')" inputmode="decimal" pattern="[0-9]{1,12}(\.[0-9]{1,8})?" :required="config.enabled" /></label>
            <label v-for="field in numberFields" :key="field" class="qq-label">{{ t(`qqImage.admin.fields.${field}`) }}<input v-model.number="config[field]" class="qq-input" :name="field" v-bind="fieldState(field)" type="number" min="1" :max="qqConfigNumberLimits[field]" step="1" required /><span class="qq-muted">{{ t('qqImage.admin.configValidation.rangeHint', { max: qqConfigNumberLimits[field] }) }}</span></label>
          </div><label class="qq-label">{{ t('qqImage.admin.fields.allowed_groups') }}<textarea v-model="groups" class="qq-input min-h-28" name="allowed_groups" v-bind="fieldState('allowed_groups')" :required="config.enabled" /><span class="qq-muted">{{ t('qqImage.admin.groupsHint') }}</span></label><p class="qq-muted">{{ t('qqImage.admin.priceHint') }}</p></fieldset>
          <div class="flex justify-end"><button class="btn btn-primary" type="submit" :disabled="busy || discoveryBusy || loading">{{ busy ? t('common.loading') : t('common.save') }}</button></div>
        </form>
        <QQDiscoveryPanel v-if="tab === 'config' && config" :app-id="config.app_id" :ready="!!config.app_id.trim() && config.bot_secret_configured && config.encryption_available" :dirty="configDirty" :version="configVersion" :disabled="busy || loading" @busy="discoveryBusy = $event" @confirmed="refresh" />
        <template v-if="tab === 'orders'"><p data-test="provider-cost-hint" class="qq-notice">{{ t('qqImage.admin.providerCostHint') }}</p><p v-if="!loading && !error && !orders.length" class="qq-panel qq-muted">{{ t('qqImage.emptyOrders') }}</p><QQOrderCard v-for="order in orders" :key="order.id" :order="order" detail><div class="qq-actions"><button v-if="order.funds_status === 'captured'" :data-test="`refund-${order.id}`" class="btn btn-secondary" :disabled="busy" @click="prepare('refund', order.id)">{{ t('qqImage.admin.refund') }}</button><button v-if="['held', 'release_pending'].includes(order.funds_status)" class="btn btn-secondary" :disabled="busy" @click="prepare('reconcile', order.id)">{{ t('qqImage.admin.reconcile') }}</button><button v-if="order.status === 'reconciling' && order.funds_status === 'held'" :data-test="`release-${order.id}`" class="btn btn-secondary" :disabled="busy" @click="prepare('release', order.id)">{{ t('qqImage.admin.release') }}</button><button v-if="order.artifact_expires_at && Date.parse(order.artifact_expires_at) > Date.now()" class="btn btn-secondary" :disabled="busy" @click="loadImage(order)">{{ t('qqImage.admin.imageAudit') }}</button></div></QQOrderCard></template>
        <template v-if="tab === 'grants'"><p v-if="!loading && !error && !grants.length" class="qq-panel qq-muted">{{ t('qqImage.emptyGrants') }}</p><article v-for="grant in grants" :key="grant.id" class="qq-panel space-y-3"><dl class="qq-meta"><div><dt>{{ t('qqImage.group') }}</dt><dd>{{ grant.group_id }}</dd></div><div><dt>{{ t('qqImage.admin.user') }}</dt><dd>{{ grant.user_id }}</dd></div><div><dt>{{ t('qqImage.admin.grantStatus') }}</dt><dd>{{ t(`qqImage.bindingStates.${grant.status}`) }}</dd></div></dl><p class="qq-muted break-all">{{ grant.id }}</p><button v-if="grant.status === 'active'" class="btn btn-secondary" :disabled="busy" @click="prepare('revoke', grant.id)">{{ t('qqImage.revoke') }}</button></article></template>
        <template v-if="tab === 'audit'"><p v-if="!loading && !error && !audit.length" class="qq-panel qq-muted">{{ t('qqImage.admin.emptyAudit') }}</p><article v-for="entry in audit" :key="entry.id" class="qq-panel space-y-2 break-words text-sm"><strong>{{ entry.action }}</strong><p class="qq-muted">{{ new Date(entry.created_at).toLocaleString() }} · {{ t('qqImage.admin.actor') }} {{ entry.actor_id || '—' }}</p><p>{{ entry.object_id || entry.order_id }}</p><p>{{ entry.reason || entry.detail }}</p></article></template>
      </section>
      <div v-if="action" ref="actionPanel" tabindex="-1" class="qq-notice space-y-3" role="alertdialog" :aria-label="t(`qqImage.admin.${action.kind}`)"><h3 class="font-semibold">{{ t(`qqImage.admin.${action.kind}`) }} · {{ action.id }}</h3><p>{{ t(`qqImage.admin.${action.kind}Hint`) }}</p><label v-if="action.kind !== 'reconcile'" class="qq-label">{{ t('qqImage.admin.reason') }}<textarea v-model="reason" data-test="action-reason" class="qq-input" maxlength="500" :disabled="busy" /></label><div class="qq-actions"><button data-test="confirm-action" class="btn btn-primary" :disabled="busy || (action.kind !== 'reconcile' && !reason.trim())" @click="confirmAction">{{ t('qqImage.admin.confirm') }}</button><button class="btn btn-secondary" :disabled="busy" @click="action = null; reason = ''">{{ t('qqImage.back') }}</button></div></div>
      <div v-if="imageURL" class="qq-panel space-y-3"><p class="qq-muted">{{ t('qqImage.admin.imageAudit') }} · {{ imageID }}</p><img :src="imageURL" :alt="t('qqImage.imageAlt')" class="max-h-96 w-full rounded-xl object-contain" /><div class="qq-actions"><a :href="imageURL" :download="`qq-image-${imageID}`" class="btn btn-secondary">{{ t('qqImage.download') }}</a><button class="btn btn-secondary" @click="clearImage">{{ t('qqImage.back') }}</button></div></div>
    </template>
  </div>
</template>
