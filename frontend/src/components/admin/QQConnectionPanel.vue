<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminQQImageAPI, type QQConnection } from '@/api/qqImage'
import { useQQSession } from '@/composables/useQQSession'

const props = defineProps<{ dirty: boolean; version: number; loading: boolean; disabled: boolean }>()
const { t } = useI18n()
const connection = ref<QQConnection | null>(null), busy = ref(false), failed = ref(false)
const states = ['webhook', 'waiting_config', 'standby', 'connecting', 'connected', 'reconnecting', 'error', 'stopped', 'disabled']
const state = computed(() => connection.value && states.includes(connection.value.state) ? connection.value.state : 'unknown')
const timeFields = ['last_connected_at', 'last_heartbeat_at', 'last_event_at'] as const
const timestamps = computed(() => timeFields.flatMap(field => {
  const value = connection.value?.[field]
  return value && Number.isFinite(Date.parse(value)) ? [{ field, value, display: new Date(value).toLocaleString() }] : []
}))
let sequence = 0, controller: AbortController | undefined
const session = useQQSession(clear, true)

function clear() {
  sequence++; controller?.abort(); controller = undefined
  connection.value = null; busy.value = false; failed.value = false
}
async function refresh(automatic = false) {
  if (!session.current() || props.dirty || props.loading || (!automatic && (props.disabled || busy.value))) return
  clear()
  const ticket = sequence, pending = new AbortController()
  controller = pending
  const abort = () => pending.abort()
  session.signal.addEventListener('abort', abort, { once: true })
  const current = () => session.current() && sequence === ticket && !props.dirty && !props.loading
  busy.value = true
  try {
    const value = await adminQQImageAPI.connection(pending.signal)
    if (!current()) return
    // Keep only the public connection summary; never retain session IDs or raw errors.
    connection.value = {
      state: value.state, last_connected_at: value.last_connected_at, last_heartbeat_at: value.last_heartbeat_at,
      last_event_at: value.last_event_at, error_code: value.error_code
    }
  } catch {
    if (current()) failed.value = true
  } finally {
    session.signal.removeEventListener('abort', abort)
    if (current()) { busy.value = false; controller = undefined }
  }
}
watch([() => props.dirty, () => props.version, () => props.loading], () => {
  clear()
  if (!props.dirty && !props.loading) void refresh(true)
})
onMounted(() => { void refresh(true) })
</script>

<template>
  <section data-test="qq-connection" class="qq-panel space-y-3" aria-labelledby="qq-connection-title">
    <div class="qq-heading"><h3 id="qq-connection-title" class="text-base font-semibold">{{ t('qqImage.admin.connection.title') }}</h3><button data-test="connection-refresh" type="button" class="btn btn-secondary" :disabled="busy || loading || disabled || dirty || session.invalidated.value" @click="refresh()">{{ t('qqImage.admin.connection.refresh') }}</button></div>
    <p class="qq-muted">{{ t('qqImage.admin.connection.hint') }}</p>
    <p v-if="dirty" class="qq-notice" role="status">{{ t('qqImage.admin.connection.saveFirst') }}</p>
    <template v-else-if="!session.invalidated.value">
      <p data-test="connection-state" role="status">{{ busy || loading ? t('qqImage.admin.connection.loading') : t(`qqImage.admin.connection.states.${state}`) }}</p>
      <p v-if="failed" class="qq-error" role="alert">{{ t('qqImage.admin.connection.failed') }}</p>
      <p v-if="connection?.error_code" class="qq-error" role="alert">{{ t('qqImage.admin.connection.errors.unknown') }}</p>
      <dl v-if="timestamps.length" class="qq-meta"><div v-for="timestamp in timestamps" :key="timestamp.field"><dt>{{ t(`qqImage.admin.connection.${timestamp.field}`) }}</dt><dd><time :datetime="timestamp.value">{{ timestamp.display }}</time></dd></div></dl>
    </template>
  </section>
</template>
