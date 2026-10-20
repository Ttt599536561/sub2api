<template>
  <BaseDialog :show="show" :title="t(`${key}.title`)" width="normal" @close="emit('close')">
    <form class="space-y-4" @submit.prevent="submit">
      <div>
        <label for="affiliate-domain-user" class="input-label">{{ t(`${key}.user`) }}</label>
        <div v-if="selectedUser" class="flex items-center justify-between gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
          <span class="min-w-0 break-all text-sm">{{ selectedUser.email }} <span class="text-gray-500">({{ selectedUser.username }})</span></span>
          <button v-if="!entry" type="button" class="btn btn-secondary btn-sm" :disabled="saving" @click="clearUser">
            {{ t(`${key}.changeUser`) }}
          </button>
        </div>
        <template v-else>
          <input id="affiliate-domain-user" v-model="query" class="input" :placeholder="t(`${key}.userPlaceholder`)" data-test="domain-user-search" :disabled="!authorized" @input="search">
          <div v-if="users.length" class="mt-1 max-h-40 overflow-auto rounded-lg border border-gray-200 dark:border-dark-700">
            <button v-for="user in users" :key="user.id" type="button" class="block w-full break-all px-3 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-800" data-test="domain-user-option" @click="selectUser(user)">
              {{ user.email }} ({{ user.username }})
            </button>
          </div>
          <p v-else-if="searched" class="input-hint">{{ t(`${key}.noUsers`) }}</p>
        </template>
      </div>
      <div>
        <label for="affiliate-domain" class="input-label">{{ t(`${key}.domain`) }}</label>
        <input id="affiliate-domain" v-model="domain" class="input font-mono" type="text" autocomplete="off" maxlength="253" :placeholder="t(`${key}.placeholder`)" data-test="affiliate-domain" :disabled="!authorized || saving || !selectedUser">
        <p class="input-hint">{{ t(`${key}.hint`) }}</p>
        <p v-if="domainError" class="input-error-text">{{ domainError }}</p>
        <p v-if="entry?.affiliate_domain" class="input-hint">{{ t(`${key}.clearHint`) }}</p>
      </div>
      <p class="text-xs text-gray-500 dark:text-dark-400">{{ t(`${key}.setupHint`) }}</p>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="!canSubmit" data-test="domain-save">{{ t(saving ? 'common.saving' : 'common.save') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { affiliatesAPI, type AffiliateAdminEntry, type SimpleUser } from '@/api/admin/affiliates'
import { ownedAuthRequestConfig } from '@/api/client'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { extractI18nErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean; entry?: AffiliateAdminEntry | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const key = 'admin.settings.features.affiliate.domain'
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const query = ref('')
const users = ref<SimpleUser[]>([])
const selectedUser = ref<SimpleUser | null>(null)
const domain = ref('')
const searched = ref(false)
const saving = ref(false)
const lifecycle = reactive({ generation: 0 })
let searchSequence = 0
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
const authorized = computed(() => props.show && authStore.isAdmin === true)
const normalizedDomain = computed(() => domain.value.trim().toLowerCase())
const domainError = computed(() => {
  const value = normalizedDomain.value
  if (!value) return ''
  const labels = value.split('.')
  return value.length > 253 || labels.length < 2 || /^\d+(?:\.\d+){3}$/.test(value) ||
    labels.some(label => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))
    ? t(`${key}.invalid`) : ''
})
const canSubmit = computed(() => authorized.value && !!selectedUser.value && !saving.value &&
  !domainError.value && normalizedDomain.value !== (props.entry?.affiliate_domain || ''))

function errorMessage(error: unknown) {
  return extractI18nErrorMessage(error, t, `${key}.errors`, t('common.error'))
}

function context() {
  return {
    generation: lifecycle.generation,
    identity: ownedAuthRequestConfig().authIdentity,
    revision: authStore.sessionRevision,
    userID: authStore.user?.id
  }
}
function current(caller: ReturnType<typeof context>) {
  const owner = ownedAuthRequestConfig().authIdentity
  return !disposed && authorized.value && caller.generation === lifecycle.generation &&
    caller.revision === authStore.sessionRevision && caller.userID === authStore.user?.id &&
    caller.identity.userID === owner.userID && caller.identity.sessionID === owner.sessionID
}
function invalidateSearch() {
  clearTimeout(timer)
  searchSequence++
  users.value = []
  searched.value = false
}
function reset() {
  lifecycle.generation++
  invalidateSearch()
  query.value = ''
  domain.value = props.entry?.affiliate_domain || ''
  selectedUser.value = props.entry
    ? { id: props.entry.user_id, email: props.entry.email, username: props.entry.username }
    : null
  saving.value = false
}
function selectUser(user: SimpleUser) {
  if (!authorized.value || saving.value) return
  invalidateSearch()
  selectedUser.value = user
  query.value = ''
}
function clearUser() {
  invalidateSearch()
  selectedUser.value = null
  domain.value = ''
}
function search() {
  invalidateSearch()
  const value = query.value.trim()
  if (!value || !authorized.value) return
  const caller = context()
  const sequence = searchSequence
  timer = setTimeout(async () => {
    try {
      const result = await affiliatesAPI.lookupUsers(value)
      if (!current(caller) || sequence !== searchSequence) return
      users.value = result
      searched.value = true
    } catch (error) {
      if (current(caller) && sequence === searchSequence) appStore.showError(errorMessage(error))
    }
  }, 300)
}
async function submit() {
  if (!canSubmit.value || !selectedUser.value) return
  const caller = context()
  const userID = selectedUser.value.id
  saving.value = true
  try {
    await affiliatesAPI.updateUserDomain(userID, { domain: normalizedDomain.value })
    if (!current(caller)) return
    appStore.showSuccess(t('common.saved'))
    emit('saved')
  } catch (error) {
    if (current(caller)) appStore.showError(errorMessage(error))
  } finally {
    if (current(caller)) saving.value = false
  }
}
function invalidateSession() {
  reset()
  selectedUser.value = null
  domain.value = ''
  if (props.show) emit('close')
}
function onStorage(event: StorageEvent) {
  if (!event.key || ['auth_session_id', 'auth_user'].includes(event.key)) invalidateSession()
}
watch([() => props.show, () => props.entry], reset, { immediate: true, flush: 'sync' })
watch([() => authStore.user?.id, () => authStore.sessionRevision, () => authStore.isAdmin], invalidateSession, { flush: 'sync' })
window.addEventListener('storage', onStorage)
onBeforeUnmount(() => {
  disposed = true
  invalidateSearch()
  window.removeEventListener('storage', onStorage)
})
</script>
