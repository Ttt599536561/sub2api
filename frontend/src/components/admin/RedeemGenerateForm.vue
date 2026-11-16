<template>
<form @submit.prevent="handleGenerateCodes" class="space-y-4">
  <div>
    <label for="generate-type" class="input-label">{{ t('admin.redeem.codeType') }}</label>
    <Select id="generate-type" v-model="generateForm.type" :options="typeOptions" />
  </div>
  <!-- 余额/并发类型：显示数值输入 -->
  <div v-if="generateForm.type !== 'subscription' && generateForm.type !== 'invitation'">
    <label class="input-label">
      {{
        generateForm.type === 'balance'
          ? t('admin.redeem.amount')
          : t('admin.redeem.columns.value')
      }}
    </label>
    <input
      v-model.number="generateForm.value"
      type="number"
      :step="generateForm.type === 'balance' ? '0.01' : '1'"
      :min="generateForm.type === 'balance' ? '0.01' : '1'"
      required
      class="input"
    />
  </div>
  <!-- 邀请码类型：显示提示信息 -->
  <div v-if="generateForm.type === 'invitation'" class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
    <p class="text-sm text-blue-700 dark:text-blue-300">
      {{ t('admin.redeem.invitationHint') }}
    </p>
  </div>
  <!-- 订阅类型：显示分组选择和有效天数 -->
  <template v-if="generateForm.type === 'subscription'">
    <div>
      <label for="subscription-purpose" class="input-label">{{ t('welfareRedeem.purpose') }}</label>
      <Select id="subscription-purpose" v-model="purpose" data-test="subscription-purpose" :options="purposeOptions" />
    </div>
    <template v-if="purpose === 'welfare'">
      <div>
        <label for="welfare-campaign" class="input-label">{{ t('welfareRedeem.campaign') }}</label>
        <Select
          id="welfare-campaign" v-model="campaignId" data-test="welfare-campaign"
          :options="campaignOptions" :disabled="loadingCampaigns" :placeholder="t('welfareRedeem.chooseCampaign')"
        />
        <p v-if="loadingCampaigns" role="status" class="input-hint">{{ t('common.loading') }}</p>
        <p v-else-if="campaignError" role="alert" class="mt-2 text-sm text-red-600 dark:text-red-400">{{ campaignError }}</p>
        <p v-else-if="campaignsLoaded && !campaigns.length" class="input-hint">{{ t('welfareRedeem.noCampaigns') }}</p>
        <button v-if="campaignError || (campaignsLoaded && !campaigns.length)" type="button" data-test="welfare-retry" class="btn btn-secondary mt-2" @click="loadCampaigns">
          {{ t('common.refresh') }}
        </button>
      </div>
      <div v-if="selectedCampaign" data-test="welfare-group" class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700">
        <p class="text-sm font-medium">{{ t('welfareRedeem.boundGroup') }}: {{ selectedCampaign.group_name }} <span class="font-mono">#{{ selectedCampaign.subscription_group_id }}</span></p>
        <p class="input-hint">{{ t('welfareRedeem.groupHint') }}</p>
      </div>
      <div>
        <label for="welfare-effect" class="input-label">{{ t('welfareRedeem.effect') }}</label>
        <Select id="welfare-effect" v-model="effect" data-test="welfare-effect" :options="effectOptions" />
      </div>
      <div v-if="effect === 'add_quota'">
        <label for="welfare-quota" class="input-label">{{ t('welfareRedeem.quotaAmount') }}</label>
        <input id="welfare-quota" v-model.number="quotaAmount" data-test="welfare-quota" type="number" min="0.01" max="9999999999.99" step="0.01" required class="input" />
        <p class="input-hint">{{ t('welfareRedeem.quotaHint') }}</p>
      </div>
      <div v-else>
        <label for="welfare-days" class="input-label">{{ t('welfareRedeem.extensionDays') }}</label>
        <input id="welfare-days" v-model.number="extensionDays" data-test="welfare-days" type="number" min="1" max="36500" step="1" required class="input" />
        <p class="input-hint">{{ t('welfareRedeem.durationHint') }}</p>
      </div>
      <p class="rounded-lg bg-blue-50 p-3 text-sm text-blue-700 dark:bg-blue-900/20 dark:text-blue-300">{{ t('welfareRedeem.activeCycleHint') }}</p>
    </template>
    <template v-else>
    <div>
      <label class="input-label">{{ t('admin.redeem.selectGroup') }}</label>
      <Select
        v-model="generateForm.group_id"
        data-test="subscription-group"
        :options="subscriptionGroupOptions"
        :placeholder="t('admin.redeem.selectGroupPlaceholder')"
      >
        <template #selected="{ option }">
          <GroupBadge
            v-if="option"
            :name="(option as unknown as GroupOption).label"
            :platform="(option as unknown as GroupOption).platform"
            :subscription-type="(option as unknown as GroupOption).subscriptionType"
            :rate-multiplier="(option as unknown as GroupOption).rate"
          />
          <span v-else class="text-gray-400">{{
            t('admin.redeem.selectGroupPlaceholder')
          }}</span>
        </template>
        <template #option="{ option, selected }">
          <GroupOptionItem
            :name="(option as unknown as GroupOption).label"
            :platform="(option as unknown as GroupOption).platform"
            :subscription-type="(option as unknown as GroupOption).subscriptionType"
            :rate-multiplier="(option as unknown as GroupOption).rate"
            :description="(option as unknown as GroupOption).description"
            :selected="selected"
          />
        </template>
      </Select>
    </div>
    <div>
      <label class="input-label">{{ t('admin.redeem.validityDays') }}</label>
      <input
        v-model.number="generateForm.validity_days"
        type="number"
        min="1"
        max="36500"
        required
        class="input"
      />
    </div>
    </template>
  </template>
  <div>
    <label class="input-label">{{ t('admin.redeem.codeExpiry') }}</label>
    <div class="grid grid-cols-2 gap-2 sm:grid-cols-5">
      <button
        v-for="option in redeemCodeExpiryOptions"
        :key="option.value"
        type="button"
        @click="generateForm.expiry_option = option.value"
        :class="[
          'rounded-lg border px-3 py-2 text-sm transition-colors',
          generateForm.expiry_option === option.value
            ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-300'
            : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700'
        ]"
      >
        {{ option.label }}
      </button>
    </div>
    <input
      v-if="generateForm.expiry_option === 'custom'"
      v-model.number="generateForm.custom_expiry_days"
      data-test="generate-expiry"
      type="number"
      min="1"
      max="3650"
      required
      class="input mt-2"
      :placeholder="t('admin.redeem.customExpiryDays')"
    />
  </div>
  <div>
    <label class="input-label">{{ t('admin.redeem.count') }}</label>
    <input
      v-model.number="generateForm.count"
      data-test="generate-count"
      type="number"
      min="1"
      max="100"
      required
      class="input"
    />
  </div>
  <p v-if="errorMessage" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
  <div class="flex justify-end gap-3 pt-2">
    <button type="button" @click="emit('cancel')" class="btn btn-secondary">
      {{ t('common.cancel') }}
    </button>
    <button type="submit" :disabled="generating || (isWelfare && (loadingCampaigns || !selectedCampaign))" class="btn btn-primary">
      {{ generating ? t('admin.redeem.generating') : t('admin.redeem.generate') }}
    </button>
  </div>
</form>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Group, GroupPlatform, RedeemCode, RedeemCodeType, SubscriptionType, WelfareRedeemCampaign, WelfareRedeemGenerateOptions, WelfareSubscriptionEffect } from '@/types'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import { welfareErrorMessage } from '@/utils/welfareRedeemError'

const props = defineProps<{ groups: Group[] }>()
const emit = defineEmits<{ generated: [codes: RedeemCode[]]; cancel: [] }>()
const { t } = useI18n()
const appStore = useAppStore()

interface GroupOption {
  value: number
  label: string
  description: string | null
  platform: GroupPlatform
  subscriptionType: SubscriptionType
  rate: number
}
const subscriptionGroupOptions = computed(() => props.groups
  .filter(group => group.subscription_type === 'subscription')
  .map(group => ({ value: group.id, label: group.name, description: group.description,
    platform: group.platform, subscriptionType: group.subscription_type, rate: group.rate_multiplier })))
const typeOptions = computed(() => ['balance', 'concurrency', 'subscription', 'invitation'].map(value => ({
  value, label: t('admin.redeem.' + value)
})))
const purposeOptions = computed(() => ['standard', 'welfare'].map(value => ({ value, label: t('welfareRedeem.' + value) })))
const effectOptions = computed(() => ['add_quota', 'extend_duration'].map(value => ({ value, label: t('welfareRedeem.' + value) })))
type RedeemCodeExpiryOption = 'never' | '1' | '3' | '7' | 'custom'
const redeemCodeExpiryOptions = computed<{ value: RedeemCodeExpiryOption; label: string }[]>(() => [
  { value: 'never', label: t('admin.redeem.neverExpires') },
  ...([1, 3, 7] as const).map(days => ({ value: String(days) as RedeemCodeExpiryOption, label: t('admin.redeem.expiryPresetDays', { days }) })),
  { value: 'custom', label: t('admin.redeem.customExpiry') }
])
const generateForm = reactive({
  type: 'balance' as RedeemCodeType, value: 10, count: 1, group_id: null as number | null,
  validity_days: 30, expiry_option: 'never' as RedeemCodeExpiryOption, custom_expiry_days: 7
})
const purpose = ref<'standard' | 'welfare'>('standard')
const effect = ref<WelfareSubscriptionEffect>('add_quota')
const campaignId = ref<number | null>(null)
const quotaAmount = ref<number | ''>('')
const extensionDays = ref(30)
const generating = ref(false)
const errorMessage = ref('')
const campaigns = ref<WelfareRedeemCampaign[]>([])
const loadingCampaigns = ref(false)
const campaignsLoaded = ref(false)
const campaignError = ref('')
const isWelfare = computed(() => generateForm.type === 'subscription' && purpose.value === 'welfare')
const selectedCampaign = computed(() => campaigns.value.find(campaign => campaign.id === campaignId.value))
const campaignOptions = computed(() => campaigns.value.map(campaign => ({ value: campaign.id, label: campaign.name })))

async function loadCampaigns() {
  if (loadingCampaigns.value) return
  loadingCampaigns.value = true
  campaignError.value = ''
  try {
    campaigns.value = await adminAPI.redeem.welfareCampaigns()
    campaignsLoaded.value = true
    if (!selectedCampaign.value) campaignId.value = null
  } catch (error) {
    campaigns.value = []
    campaignId.value = null
    campaignError.value = welfareErrorMessage(error, t, 'welfareRedeem.campaignLoadFailed')
  } finally {
    loadingCampaigns.value = false
  }
}

watch(isWelfare, active => {
  errorMessage.value = ''
  if (active && !campaignsLoaded.value) void loadCampaigns()
})
watch(effect, () => { errorMessage.value = '' })
watch(() => generateForm.type, newType => {
  errorMessage.value = ''
  if (newType === 'invitation') generateForm.value = 0
  else if (generateForm.value === 0) generateForm.value = 10
})

function fail(key: string) {
  errorMessage.value = t(key)
  appStore.showError(errorMessage.value)
}

async function handleGenerateCodes() {
  if (generating.value) return
  errorMessage.value = ''
  if (!Number.isInteger(generateForm.count) || generateForm.count < 1 || generateForm.count > 100) {
    fail('welfareRedeem.countRequired')
    return
  }
  let welfare: WelfareRedeemGenerateOptions | undefined
  let days = generateForm.type === 'subscription' ? generateForm.validity_days : undefined
  if (isWelfare.value) {
    if (loadingCampaigns.value || !selectedCampaign.value) return fail('welfareRedeem.campaignRequired')
    if (effect.value === 'add_quota') {
      const amount = Number(quotaAmount.value)
      if (!Number.isFinite(amount) || amount <= 0 || amount >= 1e10 || Number(amount.toFixed(2)) !== amount) {
        return fail('welfareRedeem.quotaRequired')
      }
      welfare = { subscription_effect: 'add_quota', welfare_campaign_id: selectedCampaign.value.id, quota_amount_usd: amount }
      days = undefined
    } else {
      days = extensionDays.value
      welfare = { subscription_effect: 'extend_duration', welfare_campaign_id: selectedCampaign.value.id }
    }
  } else if (generateForm.type === 'subscription' && !generateForm.group_id) {
    return fail('admin.redeem.groupRequired')
  }
  if (days !== undefined && (!Number.isInteger(days) || days < 1 || days > 36500)) return fail('welfareRedeem.daysRequired')
  const expiresInDays = generateForm.expiry_option === 'never' ? undefined
    : generateForm.expiry_option === 'custom' ? generateForm.custom_expiry_days : Number(generateForm.expiry_option)
  if (expiresInDays !== undefined && (!Number.isInteger(expiresInDays) || expiresInDays < 1 || expiresInDays > 3650)) {
    return fail('admin.redeem.expiryDaysRequired')
  }
  generating.value = true
  try {
    const args = [generateForm.count, generateForm.type, isWelfare.value ? 0 : generateForm.value,
      generateForm.type === 'subscription' && !isWelfare.value ? generateForm.group_id : undefined,
      days, expiresInDays] as const
    const result = welfare
      ? await adminAPI.redeem.generate(...args, welfare)
      : await adminAPI.redeem.generate(...args)
    generateForm.group_id = null
    generateForm.validity_days = 30
    generateForm.expiry_option = 'never'
    generateForm.custom_expiry_days = 7
    quotaAmount.value = ''
    extensionDays.value = 30
    emit('generated', result)
  } catch (error) {
    errorMessage.value = welfareErrorMessage(error, t, 'admin.redeem.failedToGenerate')
    appStore.showError(errorMessage.value)
  } finally {
    generating.value = false
  }
}
</script>
