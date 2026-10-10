<template>
  <div :class="props.embedded ? 'space-y-4' : 'card'">
    <div
      v-if="!props.embedded"
      class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
    >
      <h2 class="text-lg font-medium text-gray-900 dark:text-white">
        {{ t('profile.editProfile') }}
      </h2>
    </div>
    <div :class="props.embedded ? '' : 'px-6 py-6'">
      <form @submit.prevent="handleUpdateProfile" class="space-y-4">
        <div v-if="props.embedded">
          <p class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('profile.editProfile') }}
          </p>
        </div>
        <div>
          <label for="username" class="input-label">
            {{ t('profile.username') }}
          </label>
          <input
            id="username"
            v-model="username"
            type="text"
            class="input"
            :placeholder="t('profile.enterUsername')"
          />
        </div>

        <div class="flex justify-end pt-4">
          <button type="submit" :disabled="loading" class="btn btn-primary">
            {{ loading ? t('profile.updating') : t('profile.updateProfile') }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { getAuthSessionID } from '@/api/authSession'
import { ownedAuthRequestConfig } from '@/api/client'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { userAPI } from '@/api'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = withDefaults(defineProps<{
  initialUsername: string
  embedded?: boolean
}>(), {
  embedded: false,
})

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const username = ref(props.initialUsername)
let savedUsername = props.initialUsername
const loading = ref(false)
let mutationGeneration = 0
let disposed = false
let sessionOwner = ownedAuthRequestConfig().authIdentity
function invalidateLogin() {
  sessionOwner = ownedAuthRequestConfig().authIdentity
  mutationGeneration++
  loading.value = false
}
function onSessionStorage(event: StorageEvent) {
  if (event.key && !['auth_session_id', 'auth_user'].includes(event.key)) return
  const next = ownedAuthRequestConfig().authIdentity
  if (next.sessionID !== sessionOwner.sessionID || next.userID !== sessionOwner.userID) invalidateLogin()
}
watch([() => authStore.user?.id, () => authStore.sessionRevision], invalidateLogin, { flush: 'sync' })
onMounted(() => window.addEventListener('storage', onSessionStorage))
onBeforeUnmount(() => {
  disposed = true; mutationGeneration++
  window.removeEventListener('storage', onSessionStorage)
})

watch(() => props.initialUsername, (val) => {
  if (username.value === savedUsername) username.value = val
  savedUsername = val
})

const handleUpdateProfile = async () => {
  if (!username.value.trim()) {
    appStore.showError(t('profile.usernameRequired'))
    return
  }

  loading.value = true
  const generation = mutationGeneration
  const session = getAuthSessionID()
  const isCurrent = () => !disposed && generation === mutationGeneration && session === getAuthSessionID()
  const submittedUsername = username.value
  try {
    const updatedUser = await userAPI.updateProfile({
      username: submittedUsername
    })
    if (!isCurrent()) return
    if (username.value === submittedUsername) username.value = updatedUser.username
    savedUsername = updatedUser.username
    authStore.user = updatedUser
    appStore.showSuccess(t('profile.updateSuccess'))
  } catch (error: unknown) {
    if (!isCurrent()) return
    appStore.showError(extractApiErrorMessage(error, t('profile.updateFailed')))
  } finally {
    if (isCurrent()) loading.value = false
  }
}
</script>
