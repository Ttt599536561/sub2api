<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminQQImageAPI, type QQReplyTemplate, type QQReplyTemplates, type QQReplyPreview, type QQReplyRevision, type QQReplyRevisionDetail } from '@/api/qqImage'
import { useQQSession } from '@/composables/useQQSession'
import { replyVariableInfo, toDisplayReplyText, toStoredReplyText } from '@/utils/qqReplyTemplates'

const { t, locale } = useI18n()
const emit = defineEmits<{ busy: [value: boolean] }>()
const prefix = 'qqImage.admin.templates.'
const configuration = ref<QQReplyTemplates | null>(null)
const drafts = ref<Record<string, string>>({}), selectedKey = ref(''), category = ref('all')
const loading = ref(false), busy = ref(false), error = ref(''), notice = ref(''), reloadConfirmation = ref(false)
const editor = ref<HTMLTextAreaElement | null>(null)
const preview = ref<QQReplyPreview | null>(null), previewBusy = ref(false), previewFailed = ref(false)
const previewPending = ref(false), composing = ref(false)
const history = ref<QQReplyRevision[]>([]), historyOpen = ref(false), historyBusy = ref(false), historyFailed = ref(false)
const revisionDetail = ref<QQReplyRevisionDetail | null>(null), rollbackConfirmation = ref(false)
let sequence = 0, previewSequence = 0, historySequence = 0, previewController: AbortController | undefined
let previewTimer: ReturnType<typeof setTimeout> | undefined
const session = useQQSession(clear, true)
const catalog = computed(() => configuration.value?.catalog || [])
const categories = computed(() => [...new Set(catalog.value.map(template => template.category))])
const visibleTemplates = computed(() => catalog.value.filter(template => category.value === 'all' || template.category === category.value))
const selected = computed(() => catalog.value.find(template => template.key === selectedKey.value))
const currentText = computed({
  get: () => selected.value ? effectiveText(selected.value) : '',
  set: text => {
    if (!selected.value || busy.value || loading.value || !session.current()) return
    const values = { ...drafts.value }
    if (text === selected.value.default_text) delete values[selected.value.key]
    else values[selected.value.key] = text
    drafts.value = values
  }
})
const displayText = computed({
  get: () => toDisplayReplyText(currentText.value, selected.value?.allowed_variables || [], locale.value),
  set: text => { currentText.value = toStoredReplyText(text, selected.value?.allowed_variables || [], locale.value) }
})
const dirty = computed(() => !!configuration.value && catalog.value.some(template => effectiveText(template) !== (configuration.value!.overrides[template.key] ?? template.default_text)))
const tooLong = computed(() => catalog.value.some(template => worstLength(template, effectiveText(template)) > 2000))
const blocked = computed(() => loading.value || busy.value || session.invalidated.value)

function token(variable: string) { return `{{${variable}}}` }
function variableInfo(variable: string) { return replyVariableInfo(variable, locale.value) }
function label(template: QQReplyTemplate) { return locale.value.startsWith('en') && template.label_en ? template.label_en : template.label }
function categoryLabel(value: string) {
  return ['common', 'iq', 'image', 'binding', 'order', 'error'].includes(value) ? t(`${prefix}categories.${value}`) : value
}
function effectiveText(template: QQReplyTemplate) { return drafts.value[template.key] ?? template.default_text }
function worstLength(template: QQReplyTemplate, text: string) {
  return text.replace(/\{\{([a-zA-Z0-9_]+)\}\}/g, (placeholder, variable: string) => ' '.repeat(template.variable_limits[variable] ?? placeholder.length)).length
}
function clearPreview() {
  if (previewTimer !== undefined) clearTimeout(previewTimer)
  previewTimer = undefined; previewPending.value = false
  previewSequence++; previewController?.abort(); previewController = undefined
  preview.value = null; previewBusy.value = false; previewFailed.value = false
}
function schedulePreview() {
  clearPreview()
  if (!selected.value || blocked.value || composing.value || !session.current()) return
  previewPending.value = true
  previewTimer = setTimeout(() => { previewTimer = undefined; void showPreview() }, 350)
}
function startComposition() { composing.value = true; clearPreview() }
function finishComposition(event: CompositionEvent) {
  composing.value = false
  displayText.value = (event.target as HTMLTextAreaElement).value
  schedulePreview()
}
function clearHistory() {
  historySequence++; history.value = []; historyOpen.value = false; historyBusy.value = false; historyFailed.value = false
  revisionDetail.value = null; rollbackConfirmation.value = false
}
function clear() {
  sequence++; clearPreview(); clearHistory(); configuration.value = null; drafts.value = {}; selectedKey.value = ''; category.value = 'all'
  loading.value = false; busy.value = false; composing.value = false; error.value = ''; notice.value = ''; reloadConfirmation.value = false
}
function accept(value: QQReplyTemplates) {
  if (value?.schema_version !== 1 || value.catalog_version !== 1 || !Number.isSafeInteger(value.revision) || value.revision < 0 || !Array.isArray(value.catalog) || !value.catalog.length || !value.overrides) throw new Error('Invalid reply templates')
  configuration.value = value; drafts.value = { ...value.overrides }; clearPreview(); clearHistory()
  if (!value.catalog.some(template => template.key === selectedKey.value)) selectedKey.value = value.catalog[0].key
  if (category.value !== 'all' && !value.catalog.some(template => template.key === selectedKey.value && template.category === category.value)) category.value = 'all'
}
async function load() {
  if (!session.current() || loading.value || busy.value) return
  const ticket = ++sequence
  loading.value = true; error.value = ''; notice.value = ''; reloadConfirmation.value = false; clearPreview(); clearHistory()
  try {
    const value = await adminQQImageAPI.replyTemplates(session.signal)
    if (session.current() && ticket === sequence) accept(value)
  } catch {
    if (session.current() && ticket === sequence) error.value = 'loadFailed'
  } finally {
    if (session.current() && ticket === sequence) { loading.value = false; schedulePreview() }
  }
}
function reload() {
  if (!session.current() || blocked.value) return
  if (dirty.value) reloadConfirmation.value = true
  else void load()
}
function restoreSelected() {
  if (!selected.value || blocked.value || !session.current()) return
  const values = { ...drafts.value }; delete values[selected.value.key]; drafts.value = values
  schedulePreview()
}
function restoreAll() { if (!blocked.value && session.current()) { drafts.value = {}; schedulePreview() } }
async function insertVariable(variable: string) {
  if (!selected.value?.allowed_variables.includes(variable) || blocked.value || composing.value || !session.current()) return
  const text = displayText.value, placeholder = toDisplayReplyText(token(variable), selected.value.allowed_variables, locale.value)
  const start = editor.value?.selectionStart ?? text.length, end = editor.value?.selectionEnd ?? start
  displayText.value = text.slice(0, start) + placeholder + text.slice(end)
  await nextTick()
  if (session.current()) { editor.value?.focus(); editor.value?.setSelectionRange(start + placeholder.length, start + placeholder.length) }
}
async function showPreview() {
  if (!selected.value || blocked.value || composing.value || !session.current()) return
  clearPreview()
  const ticket = previewSequence, controller = new AbortController()
  const abort = () => controller.abort()
  previewController = controller; session.signal.addEventListener('abort', abort, { once: true }); previewBusy.value = true
  const current = () => session.current() && ticket === previewSequence
  try {
    const value = await adminQQImageAPI.previewReplyTemplate({ key: selected.value.key, text: currentText.value }, controller.signal)
    if (current()) preview.value = value
  } catch { if (current()) previewFailed.value = true }
  finally {
    session.signal.removeEventListener('abort', abort)
    if (current()) { previewBusy.value = false; previewController = undefined }
  }
}
function previewIssue(issue: string) {
  const [code] = issue.split(':')
  const codes: Record<string, string> = {
    unknown_template: 'unknownTemplate', empty_template: 'emptyTemplate', empty_rendered_text: 'emptyRenderedText', invalid_control_character: 'invalidCharacter',
    unknown_variable: 'unknownVariable', malformed_placeholder: 'malformedVariable', expansion_limit_exceeded: 'tooLong'
  }
  return t(`${prefix}validation.${codes[code] || 'invalidTemplate'}`)
}
function mutationError(value: unknown) {
  const failure = value as { status?: number; response?: { status?: number } }
  return failure?.status === 409 || failure?.response?.status === 409 ? 'conflict' : 'saveFailed'
}
async function save() {
  if (!configuration.value || !dirty.value || blocked.value || composing.value || tooLong.value || !session.current()) return
  const ticket = ++sequence, overrides = { ...drafts.value }
  for (const template of catalog.value) if (overrides[template.key] === template.default_text) delete overrides[template.key]
  busy.value = true; error.value = ''; notice.value = ''; clearPreview()
  try {
    const value = await adminQQImageAPI.saveReplyTemplates({ expected_revision: configuration.value.revision, overrides }, session.signal)
    if (session.current() && ticket === sequence) { accept(value); notice.value = 'saved' }
  } catch (failure) { if (session.current() && ticket === sequence) error.value = mutationError(failure) }
  finally { if (session.current() && ticket === sequence) { busy.value = false; schedulePreview() } }
}
async function showHistory() {
  if (blocked.value || !session.current() || historyBusy.value) return
  if (historyOpen.value && !historyFailed.value) { clearHistory(); return }
  const ticket = ++historySequence
  historyOpen.value = true; historyBusy.value = true; historyFailed.value = false; revisionDetail.value = null; rollbackConfirmation.value = false
  try {
    const value = await adminQQImageAPI.replyTemplateRevisions(session.signal)
    if (session.current() && ticket === historySequence) history.value = value.slice(0, 50)
  } catch { if (session.current() && ticket === historySequence) historyFailed.value = true }
  finally { if (session.current() && ticket === historySequence) historyBusy.value = false }
}
async function showRevision(revision: number) {
  if (blocked.value || !session.current()) return
  const ticket = ++historySequence
  revisionDetail.value = null; rollbackConfirmation.value = false; historyBusy.value = true; historyFailed.value = false
  try {
    const value = await adminQQImageAPI.replyTemplateRevision(revision, session.signal)
    if (session.current() && ticket === historySequence) revisionDetail.value = value
  } catch { if (session.current() && ticket === historySequence) historyFailed.value = true }
  finally { if (session.current() && ticket === historySequence) historyBusy.value = false }
}
async function rollback() {
  if (!configuration.value || !revisionDetail.value || !rollbackConfirmation.value || blocked.value || !session.current()) return
  const ticket = ++sequence
  busy.value = true; error.value = ''; notice.value = ''; clearPreview()
  try {
    const value = await adminQQImageAPI.rollbackReplyTemplates({ expected_revision: configuration.value.revision, revision: revisionDetail.value.revision }, session.signal)
    if (session.current() && ticket === sequence) { accept(value); notice.value = 'rolledBack' }
  } catch (failure) { if (session.current() && ticket === sequence) { error.value = mutationError(failure); rollbackConfirmation.value = false } }
  finally { if (session.current() && ticket === sequence) { busy.value = false; schedulePreview() } }
}
function displayTime(value: string) { return Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString(locale.value) : '—' }
function revisionText(key: string | number, text: string) { return toDisplayReplyText(text, catalog.value.find(item => item.key === key)?.allowed_variables || [], locale.value) }
watch(category, () => { if (!visibleTemplates.value.some(template => template.key === selectedKey.value)) selectedKey.value = visibleTemplates.value[0]?.key || '' })
watch([selectedKey, currentText], () => { schedulePreview(); notice.value = '' }, { flush: 'sync' })
watch(busy, value => emit('busy', value), { flush: 'sync', immediate: true })
onMounted(load)
</script>

<template>
  <section data-test="qq-reply-templates" class="space-y-5" aria-labelledby="qq-reply-templates-title">
    <div class="qq-heading">
      <div><h3 id="qq-reply-templates-title" class="text-base font-semibold">{{ t(`${prefix}title`) }}</h3><p class="qq-muted mt-1">{{ t(`${prefix}hint`) }}</p></div>
      <button v-if="!session.invalidated.value" data-test="reload-templates" type="button" class="btn btn-secondary" :disabled="blocked" @click="reload">{{ t(`${prefix}reload`) }}</button>
    </div>
    <p v-if="session.invalidated.value" class="qq-notice" role="alert">{{ t('qqImage.sessionChanged') }}</p>
    <template v-else>
      <p class="qq-notice">{{ t(`${prefix}syncHint`) }}</p>
      <p v-if="loading" role="status" class="qq-muted">{{ t('common.loading') }}</p>
      <p v-if="error" class="qq-error" role="alert">{{ t(`${prefix}${error}`) }}</p>
      <p v-if="notice" class="qq-notice" role="status">{{ t(`${prefix}${notice}`) }}</p>
      <div v-if="reloadConfirmation" role="alertdialog" :aria-label="t(`${prefix}reload`)" class="qq-notice space-y-3">
        <p>{{ t(`${prefix}reloadConfirm`) }}</p><div class="qq-actions"><button data-test="confirm-reload" class="btn btn-primary" :disabled="blocked" @click="load">{{ t(`${prefix}confirmReload`) }}</button><button class="btn btn-secondary" :disabled="blocked" @click="reloadConfirmation = false">{{ t('common.cancel') }}</button></div>
      </div>
      <template v-if="configuration">
        <div class="qq-panel space-y-4">
          <div class="flex flex-wrap items-center justify-between gap-3"><p class="qq-muted">{{ t(`${prefix}currentRevision`, { revision: configuration.revision }) }}</p><span v-if="dirty" class="text-sm font-medium">{{ t(`${prefix}unsaved`) }}</span></div>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="qq-label">{{ t(`${prefix}category`) }}<select v-model="category" data-test="category-select" class="qq-input" :disabled="blocked"><option value="all">{{ t(`${prefix}allCategories`) }}</option><option v-for="item in categories" :key="item" :value="item">{{ categoryLabel(item) }}</option></select></label>
            <label class="qq-label">{{ t(`${prefix}template`) }}<select v-model="selectedKey" data-test="template-select" class="qq-input" :disabled="blocked"><option v-for="item in visibleTemplates" :key="item.key" :value="item.key">{{ label(item) }}</option></select></label>
          </div>
          <template v-if="selected">
            <div class="grid items-start gap-6 xl:grid-cols-2">
              <div class="min-w-0 space-y-4">
                <label class="qq-label">{{ t(`${prefix}text`) }}<textarea ref="editor" v-model="displayText" data-test="template-text" class="qq-input min-h-80 whitespace-pre-wrap" :disabled="blocked" rows="13" spellcheck="false" aria-describedby="qq-template-editor-hint qq-template-length qq-template-rules" @compositionstart="startComposition" @compositionend="finishComposition" /></label>
                <p id="qq-template-editor-hint" class="qq-muted">{{ t(`${prefix}editorHint`) }}</p>
                <p id="qq-template-length" data-test="template-length" class="qq-muted">{{ t(`${prefix}lengthSimple`, { length: currentText.length, max: 2000 }) }} {{ t(`${prefix}lengthHint`) }}</p>
                <div class="space-y-2">
                  <h4 class="text-sm font-semibold">{{ t(`${prefix}autoContentTitle`) }}</h4>
                  <p id="qq-template-rules" class="qq-muted">{{ t(`${prefix}autoContentHint`) }}</p>
                  <div v-if="selected.allowed_variables.length" class="grid gap-2 sm:grid-cols-2">
                    <button v-for="variable in selected.allowed_variables" :key="variable" :data-test="`variable-${variable}`" type="button" class="rounded-xl border border-gray-200 p-3 text-left text-sm transition hover:border-primary-400 disabled:cursor-default disabled:hover:border-gray-200 dark:border-dark-600 dark:disabled:hover:border-dark-600" :disabled="blocked || composing" @click="insertVariable(variable)">
                      <span class="flex flex-wrap items-center justify-between gap-2"><span class="font-medium">{{ variableInfo(variable).label }}</span><span class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}optionalContent`) }}</span></span>
                      <span class="qq-muted mt-1 block">{{ variableInfo(variable).description }}</span>
                    </button>
                  </div>
                  <p v-else class="qq-muted">{{ t(`${prefix}noVariables`) }}</p>
                </div>
                <p v-if="tooLong" class="qq-error" role="alert">{{ t(`${prefix}tooLong`) }}</p>
                <div class="qq-actions"><button data-test="restore-selected" type="button" class="btn btn-secondary" :disabled="blocked || composing" @click="restoreSelected">{{ t(`${prefix}restoreSelected`) }}</button><button data-test="restore-all" type="button" class="btn btn-secondary" :disabled="blocked || composing" @click="restoreAll">{{ t(`${prefix}restoreAll`) }}</button></div>
              </div>
              <aside data-test="live-preview-panel" class="min-w-0 rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-900 sm:p-5" :aria-label="t(`${prefix}livePreviewTitle`)">
                <div class="flex flex-wrap items-center justify-between gap-3"><h4 class="font-semibold">{{ t(`${prefix}livePreviewTitle`) }}</h4><button data-test="preview-template" type="button" class="text-sm font-medium text-primary-600 hover:underline disabled:opacity-50 dark:text-primary-400" :disabled="blocked || previewBusy || composing" @click="showPreview">{{ previewFailed ? t(`${prefix}previewRetry`) : t(`${prefix}previewRefresh`) }}</button></div>
                <p class="qq-muted mt-2">{{ t(`${prefix}livePreviewHint`) }}</p>
                <div class="mt-5 min-h-64" :aria-busy="previewBusy || previewPending">
                  <p v-if="previewBusy || previewPending || composing" class="qq-muted py-6 text-center" role="status">{{ t(`${prefix}previewUpdating`) }}</p>
                  <p v-else-if="previewFailed" class="qq-error" role="alert">{{ t(`${prefix}previewFailed`) }}</p>
                  <div v-else-if="preview && !preview.valid" class="space-y-2"><p class="qq-muted">{{ t(`${prefix}previewInvalid`) }}</p><ul class="qq-error list-inside list-disc" role="alert"><li v-for="(issue, index) in preview.errors" :key="index">{{ previewIssue(issue) }}</li></ul></div>
                  <div v-else-if="preview?.valid" class="flex items-start gap-3">
                    <span aria-hidden="true" class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary-100 text-lg dark:bg-primary-900/40">🤖</span>
                    <div class="min-w-0 flex-1"><p class="qq-muted mb-2">{{ t(`${prefix}previewBotName`) }}</p><pre data-test="template-preview" class="whitespace-pre-wrap break-words rounded-2xl rounded-tl-sm border border-gray-200 bg-white p-4 font-sans text-sm leading-relaxed text-gray-800 shadow-sm dark:border-dark-600 dark:bg-dark-700 dark:text-gray-100">{{ preview.text }}</pre><p class="qq-muted mt-2 text-right">{{ t(`${prefix}previewLengthSimple`, { length: preview.length, max: preview.max_length }) }}</p></div>
                  </div>
                  <p v-else class="qq-muted py-6 text-center">{{ t(`${prefix}previewEmpty`) }}</p>
                </div>
              </aside>
            </div>
          </template>
        </div>
        <div class="qq-heading"><button data-test="history-templates" type="button" class="btn btn-secondary" :disabled="blocked || historyBusy" @click="showHistory">{{ t(`${prefix}history`) }}</button><button data-test="save-templates" type="button" class="btn btn-primary" :disabled="blocked || composing || !dirty || tooLong" @click="save">{{ busy ? t('common.loading') : t(`${prefix}save`) }}</button></div>
        <section v-if="historyOpen" class="qq-panel space-y-4" :aria-label="t(`${prefix}history`)">
          <h4 class="font-semibold">{{ t(`${prefix}history`) }}</h4><p class="qq-muted">{{ t(`${prefix}historyHint`) }}</p><p v-if="historyBusy" role="status">{{ t('common.loading') }}</p><p v-if="historyFailed" class="qq-error" role="alert">{{ t(`${prefix}historyFailed`) }}</p><p v-if="!historyBusy && !historyFailed && !history.length" class="qq-muted">{{ t(`${prefix}emptyHistory`) }}</p>
          <ol class="space-y-2"><li v-for="item in history" :key="item.revision" class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600"><div class="space-y-1 text-sm"><strong>{{ t(`${prefix}currentRevision`, { revision: item.revision }) }}</strong><p class="qq-muted">{{ displayTime(item.created_at) }} · {{ t('qqImage.admin.actor') }} {{ item.actor_id }}</p><p v-if="item.restored_from_revision !== undefined" class="qq-muted">{{ t(`${prefix}restoredFrom`, { revision: item.restored_from_revision }) }}</p></div><button :data-test="`revision-${item.revision}`" class="btn btn-secondary" :disabled="blocked || historyBusy" @click="showRevision(item.revision)">{{ t(`${prefix}viewRevision`) }}</button></li></ol>
          <div v-if="revisionDetail" class="space-y-3"><h5 class="font-semibold">{{ t(`${prefix}revisionDetails`, { revision: revisionDetail.revision }) }}</h5><p class="qq-muted">{{ t(`${prefix}revisionDefaults`) }}</p><p v-if="!Object.keys(revisionDetail.overrides).length" class="qq-muted">{{ t(`${prefix}allDefault`) }}</p><article v-for="(text, key) in revisionDetail.overrides" :key="key" class="space-y-1"><h6 class="text-sm font-semibold">{{ catalog.find(item => item.key === key) ? label(catalog.find(item => item.key === key)!) : key }}</h6><pre class="whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800">{{ revisionText(key, text) }}</pre></article><button data-test="rollback-template" class="btn btn-secondary" :disabled="blocked || revisionDetail.revision === configuration.revision" @click="rollbackConfirmation = true">{{ t(`${prefix}rollback`) }}</button></div>
          <div v-if="rollbackConfirmation && revisionDetail" role="alertdialog" :aria-label="t(`${prefix}rollback`)" class="qq-notice space-y-3"><p>{{ t(`${prefix}rollbackConfirm`, { revision: revisionDetail.revision }) }}</p><div class="qq-actions"><button data-test="confirm-rollback" class="btn btn-primary" :disabled="blocked" @click="rollback">{{ t(`${prefix}confirmRollback`) }}</button><button class="btn btn-secondary" :disabled="blocked" @click="rollbackConfirmation = false">{{ t('common.cancel') }}</button></div></div>
        </section>
      </template>
    </template>
  </section>
</template>
