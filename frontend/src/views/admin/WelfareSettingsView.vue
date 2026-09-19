<template>
  <AppLayout>
    <div class="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('welfare.admin.title') }}</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('welfare.admin.description') }}</p>
      </div>
      <div v-if="loading" class="card p-6 text-sm text-gray-500" role="status">{{ t('common.loading') }}</div>
      <div v-else-if="loadError" class="card p-6 text-sm text-red-600" role="alert">{{ loadError }}</div>
      <form v-else class="card space-y-6 p-6" @submit.prevent="save">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('welfare.admin.program') }}</h2>
        <label class="flex cursor-pointer items-start gap-3">
          <input v-model="enabled" type="checkbox" :disabled="saving" class="mt-1 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
          <span>
            <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('welfare.admin.enabledLabel') }}</span>
            <span class="mt-1 block text-sm text-gray-500 dark:text-dark-400">{{ t('welfare.admin.enabledHint') }}</span>
          </span>
        </label>
        <dl class="grid gap-4 rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-800 sm:grid-cols-3">
          <div><dt class="text-gray-500 dark:text-dark-400">{{ t('welfare.admin.launchAt') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ launchLabel }}</dd></div>
          <div><dt class="text-gray-500 dark:text-dark-400">{{ t('welfare.admin.timezone') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">Asia/Shanghai</dd></div>
          <div><dt class="text-gray-500 dark:text-dark-400">{{ t('welfare.admin.rulesVersion') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">v{{ settings?.rules_version }}</dd></div>
        </dl>
        <p class="text-sm leading-relaxed text-gray-500 dark:text-dark-400">{{ t('welfare.admin.launchHint') }}</p>
        <div class="flex justify-end border-t border-gray-100 pt-5 dark:border-dark-700">
          <button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? t('common.loading') : t('common.save') }}</button>
        </div>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { adminWelfareAPI, type WelfareAdminSettings } from '@/api/admin/welfare'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const appStore = useAppStore()
const settings = ref<WelfareAdminSettings | null>(null)
const enabled = ref(false)
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')
let disposed = false
const launchLabel = computed(() => settings.value?.launch_at
  ? new Date(settings.value.launch_at).toLocaleString(undefined, { timeZone: 'Asia/Shanghai' })
  : t('welfare.admin.notLaunched'))

onMounted(async () => {
  try {
    const result = await adminWelfareAPI.getSettings()
    if (disposed) return
    settings.value = result
    enabled.value = result.enabled
  } catch {
    if (!disposed) loadError.value = t('welfare.admin.loadFailed')
  } finally {
    if (!disposed) loading.value = false
  }
})
onBeforeUnmount(() => { disposed = true })

async function save() {
  if (saving.value || loading.value || !settings.value) return
  saving.value = true
  try {
    const result = await adminWelfareAPI.updateSettings({ enabled: enabled.value })
    if (disposed) return
    settings.value = result
    enabled.value = result.enabled
    appStore.showSuccess(t('welfare.admin.saved'))
    // The save has committed. A later public-settings refresh cannot undo it.
    void appStore.fetchPublicSettings(true).catch(() => {})
  } catch {
    if (!disposed) appStore.showError(t('welfare.admin.saveFailed'))
  } finally {
    if (!disposed) saving.value = false
  }
}
</script>
