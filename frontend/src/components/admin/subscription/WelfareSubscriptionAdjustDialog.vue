<template>
  <BaseDialog
    :show="true"
    :title="t('admin.subscriptions.welfareAdjust.title')"
    width="normal"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="handleClose"
  >
    <form id="welfare-subscription-adjust-form" class="space-y-4" novalidate @submit.prevent="submit">
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-if="subscription" class="space-y-2 rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-700">
        <p class="break-all font-medium text-gray-900 dark:text-gray-100">
          {{ subscription.user?.email || `#${subscription.user_id}` }}
        </p>
        <p class="text-gray-600 dark:text-gray-300">
          {{ subscription.group?.name || t('admin.subscriptions.bulk.groupFallback', { id: subscription.group_id }) }}
        </p>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-gray-700 dark:text-gray-300">
          <dt>{{ t('admin.subscriptions.welfareAdjust.currentTotal') }}</dt>
          <dd>{{ formatAmount(currentTotal) }}</dd>
          <dt>{{ t('admin.subscriptions.welfareAdjust.used') }}</dt>
          <dd>{{ formatAmount(currentUsed) }}</dd>
          <dt>{{ t('admin.subscriptions.welfareAdjust.available') }}</dt>
          <dd>{{ formatAmount(currentAvailable) }}</dd>
          <dt>{{ t('admin.subscriptions.currentExpiration') }}</dt>
          <dd>{{ subscription.expires_at ? formatDateTimeToMinute(subscription.expires_at) : t('admin.subscriptions.noExpiration') }}</dd>
        </dl>
      </div>

      <p class="rounded-lg bg-violet-50 p-3 text-sm text-violet-800 dark:bg-violet-900/20 dark:text-violet-200">
        {{ t('admin.subscriptions.welfareAdjust.scopeHint') }}
      </p>

      <fieldset :disabled="loading || submitting || !detailsReady" class="space-y-4 disabled:opacity-60">
        <div>
          <label class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-200">
            <input id="welfare-change-total" v-model="changeTotal" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />
            {{ t('admin.subscriptions.welfareAdjust.changeTotal') }}
          </label>
          <template v-if="changeTotal">
            <label for="welfare-monthly-limit" class="input-label mt-2">{{ t('admin.subscriptions.welfareAdjust.newTotal') }}</label>
            <input id="welfare-monthly-limit" v-model="monthlyLimit" type="text" inputmode="decimal" class="input" aria-describedby="welfare-total-hint" />
            <p id="welfare-total-hint" class="input-hint">{{ t('admin.subscriptions.welfareAdjust.totalHint') }}</p>
            <p v-if="validTotal" data-test="welfare-new-remaining" class="mt-2 text-sm text-gray-700 dark:text-gray-200">
              {{ t('admin.subscriptions.welfareAdjust.newAvailable') }}: {{ formatAmount(newAvailable) }}
            </p>
            <p v-if="validTotal && Number(monthlyLimit) === 0" class="mt-1 text-sm text-amber-700 dark:text-amber-300">
              {{ t('admin.subscriptions.welfareAdjust.quotaDisabled') }}
            </p>
          </template>
        </div>

        <div>
          <label class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-200">
            <input id="welfare-change-days" v-model="changeDays" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />
            {{ t('admin.subscriptions.welfareAdjust.changeDays') }}
          </label>
          <template v-if="changeDays">
            <label for="welfare-remaining-days" class="input-label mt-2">{{ t('admin.subscriptions.remainingDays') }}</label>
            <input id="welfare-remaining-days" v-model="remainingDays" type="text" inputmode="numeric" class="input" aria-describedby="welfare-days-hint" />
            <p id="welfare-days-hint" class="input-hint">{{ t('admin.subscriptions.welfareAdjust.daysHint') }}</p>
            <p v-if="validDays && Number(remainingDays) === 0" class="mt-1 text-sm text-amber-700 dark:text-amber-300">
              {{ t('admin.subscriptions.welfareAdjust.expiresImmediately') }}
            </p>
          </template>
        </div>

        <div>
          <label for="welfare-adjust-reason" class="input-label">{{ t('admin.subscriptions.welfareAdjust.reason') }}</label>
          <textarea id="welfare-adjust-reason" v-model="reason" rows="3" class="input" aria-describedby="welfare-reason-hint" />
          <p id="welfare-reason-hint" class="input-hint">{{ t('admin.subscriptions.welfareAdjust.reasonHint') }}</p>
        </div>
      </fieldset>

      <p v-if="validationError && !loading" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validationError }}</p>
      <div v-if="requestError || conflict" role="alert" class="space-y-2 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        <p>{{ conflict ? t('admin.subscriptions.welfareAdjust.conflict') : requestError }}</p>
        <p v-if="requestError && !conflict && operation">{{ t('admin.subscriptions.welfareAdjust.retryHint') }}</p>
      </div>
      <button v-if="conflict || loadFailed || (!loading && !detailsReady)" data-test="welfare-reload" type="button" class="btn btn-secondary" :disabled="loading || submitting" @click="loadDetails">
        {{ t('admin.subscriptions.welfareAdjust.reload') }}
      </button>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="handleClose">{{ t('common.cancel') }}</button>
        <button type="submit" form="welfare-subscription-adjust-form" class="btn btn-primary" :disabled="loading || submitting || conflict || !!validationError">
          {{ submitting ? t('admin.subscriptions.adjusting') : t('admin.subscriptions.welfareAdjust.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdjustWelfareSubscriptionRequest } from '@/api/admin/subscriptions'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { UserSubscription } from '@/types'
import { formatDateTimeToMinute } from '@/utils/format'

const props = defineProps<{ subscriptionId: number }>()
const emit = defineEmits<{ close: []; completed: [subscription: UserSubscription] }>()
const { t } = useI18n()
const subscription = shallowRef<UserSubscription | null>(null)
const loading = ref(true)
const submitting = ref(false)
const loadFailed = ref(false)
const conflict = ref(false)
const requestError = ref('')
const changeTotal = ref(false)
const changeDays = ref(false)
const monthlyLimit = ref('')
const remainingDays = ref('')
const reason = ref('')
const operation = shallowRef<{ fingerprint: string; key: string } | null>(null)
let disposed = false

const currentTotal = computed(() => {
  const value = subscription.value?.welfare_monthly_limit_usd ?? subscription.value?.group?.monthly_limit_usd
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
})
const currentUsed = computed(() => {
  const value = subscription.value?.monthly_usage_usd
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
})
const currentAvailable = computed(() => currentTotal.value !== null && currentUsed.value !== null
  ? Math.max(0, currentTotal.value - currentUsed.value) : null)
const eligible = computed(() => subscription.value?.entitlement_kind === 'welfare'
  && subscription.value.welfare_cycle_closed === false
  && ['active', 'expired'].includes(subscription.value.status))
const detailsReady = computed(() => {
  const current = subscription.value
  return !!current && eligible.value && currentTotal.value !== null && currentUsed.value !== null
    && typeof current.welfare_adjustment_version === 'number' && Number.isSafeInteger(current.welfare_adjustment_version) && current.welfare_adjustment_version >= 0
    && typeof current.welfare_grant_count === 'number' && Number.isSafeInteger(current.welfare_grant_count) && current.welfare_grant_count >= 0
    && !!current.expires_at && Number.isFinite(Date.parse(current.expires_at))
})
const validTotal = computed(() => /^\d+(?:\.\d{1,2})?$/.test(monthlyLimit.value.trim())
  && Number(monthlyLimit.value) < 1e10)
const validDays = computed(() => /^\d+$/.test(remainingDays.value.trim())
  && Number.isInteger(Number(remainingDays.value)) && Number(remainingDays.value) <= 36500)
const newAvailable = computed(() => validTotal.value && currentUsed.value !== null
  ? Math.max(0, Number(monthlyLimit.value) - currentUsed.value) : null)
const validationError = computed(() => {
  if (!detailsReady.value) return t('admin.subscriptions.welfareAdjust.unavailable')
  if (!changeTotal.value && !changeDays.value) return t('admin.subscriptions.welfareAdjust.selectChange')
  if (changeTotal.value && !validTotal.value) return t('admin.subscriptions.welfareAdjust.invalidTotal')
  if (changeDays.value && !validDays.value) return t('admin.subscriptions.welfareAdjust.invalidDays')
  const length = Array.from(reason.value.trim()).length
  if (length < 3 || length > 500) return t('admin.subscriptions.welfareAdjust.invalidReason')
  return ''
})

watch(changeTotal, enabled => {
  if (enabled && monthlyLimit.value === '' && currentTotal.value !== null) monthlyLimit.value = currentTotal.value.toFixed(2)
})
watch(changeDays, enabled => {
  if (enabled && remainingDays.value === '' && subscription.value?.expires_at) {
    remainingDays.value = String(Math.max(0, Math.ceil((Date.parse(subscription.value.expires_at) - Date.now()) / 86400000)))
  }
})

function formatAmount(value: number | null) {
  return value === null ? '—' : `$${value.toFixed(2)}`
}

function handleClose() {
  if (!submitting.value) emit('close')
}

async function loadDetails() {
  if (submitting.value) return
  loading.value = true
  loadFailed.value = false
  requestError.value = ''
  try {
    const current = await adminAPI.subscriptions.getById(props.subscriptionId)
    if (disposed) return
    subscription.value = current
    conflict.value = false
    // A reload updates the version and summary while retaining the admin's inputs.
  } catch {
    if (disposed) return
    loadFailed.value = true
    subscription.value = null
    requestError.value = t('admin.subscriptions.welfareAdjust.loadFailed')
  } finally {
    if (!disposed) loading.value = false
  }
}

async function submit() {
  if (loading.value || submitting.value || conflict.value || validationError.value || !subscription.value) return
  const request: AdjustWelfareSubscriptionRequest = {
    expected_version: subscription.value.welfare_adjustment_version!,
    expected_grant_count: subscription.value.welfare_grant_count!,
    reason: reason.value.trim()
  }
  if (changeTotal.value) request.monthly_limit_usd = Number(monthlyLimit.value)
  if (changeDays.value) request.remaining_days = Number(remainingDays.value)
  const fingerprint = JSON.stringify([props.subscriptionId, request])
  if (operation.value?.fingerprint !== fingerprint) {
    const requestId = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
    operation.value = { fingerprint, key: `welfare-adjust-${requestId}` }
  }
  submitting.value = true
  requestError.value = ''
  try {
    const result = await adminAPI.subscriptions.adjustWelfare(props.subscriptionId, request, operation.value.key)
    if (disposed) return
    operation.value = null
    emit('completed', result)
  } catch (error: unknown) {
    if (disposed) return
    const failure = error as { status?: number; message?: string; response?: { status?: number; data?: { detail?: string; message?: string } } }
    conflict.value = (failure?.status ?? failure?.response?.status) === 409
    requestError.value = failure?.response?.data?.detail || failure?.response?.data?.message || failure?.message || t('admin.subscriptions.failedToAdjust')
  } finally {
    if (!disposed) submitting.value = false
  }
}

onMounted(loadDetails)
onUnmounted(() => { disposed = true })
</script>
