<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminQQImageAPI, type QQDiscovery } from '@/api/qqImage'
import { useQQSession } from '@/composables/useQQSession'
import { qqErrorKey } from '@/utils/qqImage'

const props = defineProps<{ appId: string; ready: boolean; dirty: boolean; version: number; disabled: boolean }>()
const emit = defineEmits<{ busy: [value: boolean]; confirmed: [] }>()
const { t } = useI18n()
const discovery = ref<QQDiscovery | null>(null)
// The plaintext command exists only in this mounted component, never browser storage.
const command = ref(''), busy = ref(false), error = ref(''), now = ref(Date.now())
let sequence = 0
let timer: ReturnType<typeof setInterval> | undefined
const seconds = computed(() => Math.max(0, Math.ceil(((Date.parse(discovery.value?.expires_at || '') || 0) - now.value) / 1000)))
const active = computed(() => !!discovery.value && ['waiting', 'found'].includes(discovery.value.status) && seconds.value > 0)
const available = computed(() => props.ready && !props.dirty && !props.disabled && !session.invalidated.value)
const canConfirm = computed(() => available.value && active.value && discovery.value?.status === 'found' && !!discovery.value.candidate?.group_openid)
const session = useQQSession(clear, true)
function setBusy(value: boolean) { busy.value = value; emit('busy', value) }
function clear() { sequence++; discovery.value = null; command.value = ''; error.value = ''; setBusy(false) }
function expire() {
  now.value = Date.now()
  if (discovery.value && ['waiting', 'found'].includes(discovery.value.status) && seconds.value === 0) {
    command.value = ''; discovery.value = { ...discovery.value, status: 'expired', candidate: undefined }
  }
}
function accept(value: QQDiscovery | null) {
  if (!value || value.app_id !== props.appId) { discovery.value = null; command.value = ''; return }
  if (value.id !== discovery.value?.id || value.status !== 'waiting') command.value = ''
  // Pick only public discovery fields. GET can never restore a command or group-message data.
  discovery.value = {
    id: value.id, status: value.status, app_id: value.app_id, expires_at: value.expires_at,
    ...(value.candidate ? { candidate: { group_openid: value.candidate.group_openid, event_id: value.candidate.event_id, received_at: value.candidate.received_at } } : {})
  }
  expire()
}
async function run(operation: 'refresh' | 'start' | 'close' | 'confirm') {
  if (!session.current() || !available.value || busy.value) return
  expire()
  if (operation === 'confirm' && !canConfirm.value) return
  if (operation === 'close' && !active.value) return
  const target = discovery.value?.id || '', ticket = ++sequence
  const isCurrent = () => session.current() && ticket === sequence && available.value
  if (operation !== 'refresh') command.value = ''
  setBusy(true); error.value = ''
  let confirmed = false
  try {
    if (operation === 'start') {
      discovery.value = null
      const value = await adminQQImageAPI.startDiscovery(session.signal)
      if (!isCurrent()) return
      accept(value)
      if (active.value && value.app_id === props.appId && value.status === 'waiting') command.value = value.command
    } else {
      const value = operation === 'refresh' ? await adminQQImageAPI.discovery(session.signal)
        : operation === 'close' ? await adminQQImageAPI.closeDiscovery(target, session.signal)
          : await adminQQImageAPI.confirmDiscovery(target, session.signal)
      if (!isCurrent()) return
      accept(value)
      confirmed = operation === 'confirm' && value?.status === 'confirmed'
    }
  } catch (cause) {
    if (isCurrent()) {
      discovery.value = null; command.value = ''
      const key = qqErrorKey(cause)
      error.value = ['conflict', 'expired', 'rate', 'forbidden'].includes(key) ? key : 'failed'
    }
  } finally {
    if (session.current() && ticket === sequence) {
      setBusy(false)
      if (confirmed) emit('confirmed')
    }
  }
}
watch([() => props.dirty, () => props.appId, () => props.version, () => props.ready], () => {
  clear()
  if (available.value) void run('refresh')
}, { flush: 'sync' })
onMounted(() => { timer = setInterval(expire, 1000); void run('refresh') })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <section data-test="qq-discovery" class="qq-panel space-y-4" aria-labelledby="qq-discovery-title">
    <div><h3 id="qq-discovery-title" class="text-base font-semibold">{{ t('qqImage.admin.discovery.title') }}</h3><p class="qq-muted mt-1">{{ t('qqImage.admin.discovery.hint') }}</p></div>
    <p v-if="!ready || dirty" class="qq-notice">{{ t('qqImage.admin.discovery.saveFirst') }}</p>
    <p class="qq-muted">{{ t('qqImage.admin.discovery.noReply') }}</p>
    <p v-if="error" class="qq-error" role="alert">{{ t(`qqImage.admin.discovery.errors.${error}`) }}</p>
    <div v-if="discovery" class="space-y-3">
      <p role="status">{{ t(`qqImage.admin.discovery.states.${discovery.status}`) }}<span v-if="active" data-test="discovery-countdown"> · {{ t('qqImage.admin.discovery.countdown', { seconds }) }}</span></p>
      <div v-if="command" class="qq-notice space-y-2"><p>{{ t('qqImage.admin.discovery.commandHint') }}</p><code class="block select-all whitespace-pre-wrap break-all" data-test="discovery-command">{{ command }}</code></div>
      <p v-else-if="discovery.status === 'waiting'" class="qq-muted">{{ t('qqImage.admin.discovery.commandMissing') }}</p>
      <dl v-if="discovery.candidate && active" class="qq-meta">
        <div><dt>{{ t('qqImage.admin.discovery.group') }}</dt><dd class="break-all">{{ discovery.candidate.group_openid }}</dd></div>
        <div><dt>{{ t('qqImage.admin.discovery.event') }}</dt><dd class="break-all">{{ discovery.candidate.event_id }}</dd></div>
        <div><dt>{{ t('qqImage.admin.discovery.received') }}</dt><dd>{{ new Date(discovery.candidate.received_at).toLocaleString() }}</dd></div>
      </dl>
      <p v-if="canConfirm" class="qq-notice">{{ t('qqImage.admin.discovery.confirmHint') }}</p>
    </div>
    <div class="qq-actions">
      <button data-test="discovery-start" type="button" class="btn btn-secondary" :disabled="busy || !available" @click="run('start')">{{ t('qqImage.admin.discovery.start') }}</button>
      <button data-test="discovery-refresh" type="button" class="btn btn-secondary" :disabled="busy || !available" @click="run('refresh')">{{ t('qqImage.admin.discovery.refresh') }}</button>
      <button data-test="discovery-confirm" type="button" class="btn btn-primary" :disabled="busy || !canConfirm" @click="run('confirm')">{{ t('qqImage.admin.discovery.confirm') }}</button>
      <button v-if="active" data-test="discovery-close" type="button" class="btn btn-secondary" :disabled="busy || !available" @click="run('close')">{{ t('qqImage.admin.discovery.close') }}</button>
    </div>
  </section>
</template>
