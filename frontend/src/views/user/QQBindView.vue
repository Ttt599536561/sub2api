<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { qqImageAPI, type QQBindingRequest, type QQConfirmation, type QQStatus } from '@/api/qqImage'
import { useQQSession } from '@/composables/useQQSession'
import { qqAuthTarget, qqErrorKey, rememberQQRequest, restoreQQRequest, validQQRequest } from '@/utils/qqImage'
import '@/components/user/qqImage.css'

const { t } = useI18n()
const auth = useAuthStore(), route = useRoute(), router = useRouter()
const status = ref<QQStatus | null>(null), request = ref<QQBindingRequest | null>(null), confirmation = ref<QQConfirmation | null>(null)
const requestID = ref(validQQRequest(route.query.request ?? route.query.id) || restoreQQRequest())
const consent = ref(false), busy = ref(false), loading = ref(false), error = ref(''), notice = ref(''), emailCode = ref(''), revokeID = ref('')
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
const session = useQQSession(identityChanged => {
  status.value = null; request.value = null; confirmation.value = null; consent.value = false; requestID.value = ''; emailCode.value = ''; revokeID.value = ''; busy.value = false; loading.value = false; notice.value = ''; error.value = ''
  if (identityChanged) rememberQQRequest('')
})
const needsProof = computed(() => status.value && (!status.value.email_verified || !status.value.recent_authorization))
const expired = computed(() => !!request.value && Date.parse(request.value.expires_at) <= now.value)
const mayAuthorize = computed(() => request.value && ['pending', 'await_website'].includes(request.value.status) && !expired.value && !confirmation.value)
const requestState = computed(() => expired.value ? 'expired' : request.value?.status || 'pending')
const remaining = computed(() => confirmation.value ? Math.max(0, Math.ceil((Date.parse(confirmation.value.expires_at) - now.value) / 1000)) : 0)
const grants = computed(() => status.value?.grants.filter(grant => grant.status === 'active') || [])

async function refresh() {
  if (!session.current() || loading.value || busy.value) return
  loading.value = true; error.value = ''
  try {
    const next = await qqImageAPI.status(session.signal)
    if (!session.current()) return
    status.value = next
    if (requestID.value) {
      const nextRequest = await qqImageAPI.request(requestID.value, session.signal)
      if (!session.current()) return
      request.value = nextRequest
      if (!['pending', 'await_website', 'await_qq', 'authorized'].includes(nextRequest.status)) { confirmation.value = null; consent.value = false }
    }
  } catch (e) { if (session.current()) error.value = qqErrorKey(e) }
  finally { if (session.current()) loading.value = false }
}
async function lookup() {
  if (!session.current() || busy.value || loading.value) return
  const id = validQQRequest(requestID.value.trim())
  if (!id) { error.value = 'request'; return }
  requestID.value = id; rememberQQRequest(id); request.value = null; confirmation.value = null; consent.value = false
  await refresh()
}
async function mutate(operation: () => Promise<void>) {
  if (!session.current() || busy.value || loading.value) return
  busy.value = true; error.value = ''; notice.value = ''
  try { await operation() }
  catch (e) { if (session.current()) error.value = qqErrorKey(e) }
  finally { if (session.current()) busy.value = false }
}
async function authorize() {
  if (!consent.value || !mayAuthorize.value || !status.value?.enabled || needsProof.value) return
  await mutate(async () => {
    const result = await qqImageAPI.authorize(request.value!.id, session.signal)
    if (!session.current()) return
    now.value = Date.now(); confirmation.value = result; consent.value = false
    if (request.value) request.value.status = 'await_qq'
  })
}
async function sendCode() {
  await mutate(async () => { await qqImageAPI.sendEmailCode(session.signal); if (session.current()) notice.value = 'emailSent' })
}
async function verifyEmail() {
  if (!emailCode.value.trim()) return
  await mutate(async () => {
    await qqImageAPI.verifyEmail(emailCode.value.trim(), session.signal)
    if (!session.current()) return
    emailCode.value = ''; const next = await qqImageAPI.status(session.signal)
    if (!session.current()) return
    status.value = next
    notice.value = 'emailConfirmed'
  })
}
async function revoke() {
  if (!revokeID.value || needsProof.value) return
  const id = revokeID.value
  await mutate(async () => {
    await qqImageAPI.revoke(id, session.signal)
    if (!session.current()) return
    revokeID.value = ''; confirmation.value = null; request.value = null
    const next = await qqImageAPI.status(session.signal)
    if (session.current()) { status.value = next; notice.value = 'revoked' }
  })
}
onMounted(() => {
  if (requestID.value) rememberQQRequest(requestID.value)
  // No ticket or candidate code remains in browser history or subsequent referrers.
  if (Object.keys(route.query).length) void router.replace({ path: '/qq/bind' })
  if (auth.isAuthenticated) void refresh()
  timer = setInterval(() => {
    now.value = Date.now()
    if (confirmation.value && remaining.value === 0) { confirmation.value = null; consent.value = false }
    if (auth.isAuthenticated) session.current()
  }, 1000)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <component :is="auth.isAuthenticated ? AppLayout : AuthLayout">
    <div class="qq-page">
      <header class="qq-heading"><div><h1>{{ t('qqImage.bindTitle') }}</h1><p class="qq-muted mt-2">{{ t('qqImage.bindSubtitle') }}</p></div><RouterLink v-if="auth.isAuthenticated" to="/qq/orders" class="qq-link">{{ t('qqImage.ordersTitle') }}</RouterLink></header>
      <div v-if="session.invalidated.value" class="qq-notice" role="alert">{{ t('qqImage.sessionChanged') }}</div>
      <section v-else-if="!auth.isAuthenticated" class="qq-panel space-y-4">
        <p class="qq-muted">{{ t('qqImage.loginHint') }}</p>
        <div class="qq-actions"><RouterLink data-test="login" :to="qqAuthTarget('/login', '/qq/bind')" class="btn btn-primary">{{ t('qqImage.login') }}</RouterLink><RouterLink data-test="register" :to="qqAuthTarget('/register', '/qq/bind')" class="btn btn-secondary">{{ t('qqImage.register') }}</RouterLink></div>
        <p class="qq-muted">{{ t('qqImage.registrationRules') }}</p>
      </section>
      <template v-else>
        <div v-if="loading" role="status" class="qq-muted">{{ t('common.loading') }}</div>
        <div v-if="error" class="qq-error" role="alert">{{ t(`qqImage.errors.${error}`) }} <button class="qq-link" :disabled="loading || busy" @click="refresh">{{ t('qqImage.refresh') }}</button></div>
        <div v-if="notice" class="qq-notice" role="status">{{ t(`qqImage.${notice}`) }}</div>
        <div v-if="status && !status.enabled" class="qq-notice">{{ t('qqImage.disabled') }}</div>
        <section v-if="status" class="qq-panel space-y-4">
          <h2>{{ t('qqImage.account') }}</h2><p class="break-all text-sm">{{ status.email || t('qqImage.missingEmail') }}</p>
          <p class="qq-muted">{{ status.email_verified ? t('qqImage.emailVerified') : t('qqImage.emailRequired') }}</p>
          <div v-if="needsProof" class="space-y-3">
            <p class="qq-muted">{{ t('qqImage.recentHint') }}</p>
            <template v-if="status.email"><button data-test="send-code" class="btn btn-secondary" :disabled="busy || loading" @click="sendCode">{{ t('qqImage.sendCode') }}</button>
              <form class="flex flex-wrap gap-3" @submit.prevent="verifyEmail"><label class="qq-label flex-1">{{ t('qqImage.emailCode') }}<input v-model="emailCode" data-test="email-code" class="qq-input" inputmode="numeric" autocomplete="one-time-code" maxlength="12" :disabled="busy" /></label><button data-test="verify-email" type="submit" class="btn btn-primary self-end" :disabled="busy || loading || !emailCode.trim()">{{ t('qqImage.verifyEmail') }}</button></form>
            </template>
            <RouterLink to="/profile" class="qq-link">{{ t('qqImage.manageEmail') }}</RouterLink>
          </div>
        </section>
        <section class="qq-panel space-y-4">
          <h2>{{ t('qqImage.requestTitle') }}</h2><p class="qq-muted">{{ t('qqImage.requestHint') }}</p>
          <form class="flex flex-wrap gap-3" @submit.prevent="lookup"><label class="qq-label min-w-0 flex-1">{{ t('qqImage.requestID') }}<input v-model="requestID" data-test="request-id" class="qq-input" autocomplete="off" maxlength="160" :disabled="busy || loading" /></label><button class="btn btn-secondary self-end" :disabled="busy || loading || !requestID.trim()">{{ t('qqImage.lookup') }}</button></form>
          <template v-if="request">
            <dl class="qq-meta"><div><dt>{{ t('qqImage.group') }}</dt><dd>{{ request.group_id }}</dd></div><div><dt>{{ t('qqImage.subject') }}</dt><dd>{{ request.subject_hint }}</dd></div><div><dt>{{ t('qqImage.expires') }}</dt><dd>{{ new Date(request.expires_at).toLocaleString() }}</dd></div></dl>
            <p class="qq-notice">{{ t(`qqImage.bindingStates.${requestState}`) }}</p>
            <div v-if="confirmation && remaining > 0" class="space-y-3" aria-live="polite"><p class="qq-muted">{{ t('qqImage.confirmHint') }}</p><code class="qq-code">/确认绑定 {{ confirmation.confirmation_code }}</code><p class="qq-muted">{{ t('qqImage.codeLifetime', { seconds: remaining }) }}</p><button class="btn btn-secondary" :disabled="loading || busy" @click="refresh">{{ t('qqImage.refreshBinding') }}</button></div>
            <template v-else-if="mayAuthorize && status?.enabled">
              <div class="qq-notice">{{ t('qqImage.chargeNotice') }}<br />{{ t('qqImage.publicNotice') }}</div>
              <label class="flex items-start gap-3 text-sm leading-6"><input v-model="consent" data-test="consent" type="checkbox" class="mt-1" :disabled="busy || !!needsProof" /><span>{{ t('qqImage.consent') }}</span></label>
              <button data-test="authorize" class="btn btn-primary" :disabled="busy || loading || !consent || !!needsProof" @click="authorize">{{ t('qqImage.authorize') }}</button>
            </template>
            <p v-else-if="!confirmation" class="qq-muted">{{ t('qqImage.restartHint') }}</p>
          </template>
        </section>
        <section v-if="status" class="qq-panel space-y-4"><h2>{{ t('qqImage.grantsTitle') }}</h2><p class="qq-muted">{{ t('qqImage.scopeHint') }}</p><p v-if="!grants.length" class="qq-muted">{{ t('qqImage.emptyGrants') }}</p>
          <article v-for="grant in grants" :key="grant.id" class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-4 dark:border-dark-700"><div class="min-w-0 break-all text-sm">{{ t('qqImage.group') }}: {{ grant.group_id }}<p class="qq-muted">{{ new Date(grant.created_at).toLocaleString() }}</p></div><button :data-test="`revoke-${grant.id}`" class="btn btn-secondary" :disabled="busy || loading || !!needsProof" @click="revokeID = grant.id">{{ t('qqImage.revoke') }}</button></article>
          <div v-if="revokeID" class="qq-notice space-y-3" role="alertdialog" :aria-label="t('qqImage.revoke')"><p>{{ t('qqImage.revokeHint') }}</p><div class="qq-actions"><button data-test="confirm-revoke" class="btn btn-primary" :disabled="busy" @click="revoke">{{ t('qqImage.confirmRevoke') }}</button><button class="btn btn-secondary" :disabled="busy" @click="revokeID = ''">{{ t('qqImage.back') }}</button></div></div>
        </section>
      </template>
    </div>
  </component>
</template>
