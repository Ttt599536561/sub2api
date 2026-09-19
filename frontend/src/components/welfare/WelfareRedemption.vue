<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { welfareErrorCode } from '@/utils/welfareError'
import { moneyCents, welfareMoney } from '@/utils/welfareMoney'
import type { WelfareOperation, WelfareQuote, WelfareQuoteRequest, WelfareRedemption } from '@/types/welfare'
const props = defineProps<{ show: boolean; balance: string; busy: boolean; mutationError?: string; quoteRequest: (request: WelfareQuoteRequest) => Promise<WelfareQuote | null>; redeemRequest: (request: WelfareRedemption) => Promise<WelfareOperation | null> }>()
const emit = defineEmits<{ close: []; completed: [amount: string] }>()
const { t } = useI18n()
const amount = ref('')
const quote = ref<WelfareQuote | null>(null)
const quoting = ref(false)
const error = ref('')
const uncertain = ref(false)
let sequence = 0
const validation = computed(() => {
  if (!amount.value) return ''
  const cents = moneyCents(amount.value)
  if (cents === null || cents <= 0n) return t('welfare.invalidAmount')
  if (cents > (moneyCents(props.balance) ?? 0n)) return t('welfare.insufficient')
  return ''
})
watch(amount, () => { sequence++; quote.value = null; error.value = '' })
watch(() => props.show, show => {
  if (show && !uncertain.value) { amount.value = ''; quote.value = null; error.value = '' }
  if (!show) sequence++
})
async function request(mode: 'all' | 'partial') {
  if (props.busy || uncertain.value || (mode === 'partial' && (!amount.value || validation.value))) return
  const owner = ++sequence
  quoting.value = true; error.value = ''; quote.value = null
  try {
    const response = await props.quoteRequest(mode === 'all' ? { mode } : { mode, amount: amount.value.trim() })
    if (owner !== sequence || !props.show || !response) return
    amount.value = response.amount
    // Wait for the amount watcher before installing the matching server quote.
    await Promise.resolve()
    quote.value = response
  } catch (e) { if (owner === sequence) error.value = t(`welfare.errors.${welfareErrorCode(e)}`) }
  finally { quoting.value = false }
}
async function confirm() {
  if (!quote.value || props.busy) return
  const selected = quote.value
  const result = await props.redeemRequest({ amount: selected.amount, welfare_balance_version: selected.welfare_balance_version })
  if (result?.status === 'completed') { uncertain.value = false; emit('completed', selected.amount); emit('close') }
  else {
    uncertain.value = !props.mutationError || props.mutationError === 'network'
    error.value = uncertain.value ? t('welfare.uncertainTransfer') : t(`welfare.errors.${props.mutationError}`)
    if (!uncertain.value) quote.value = null
  }
}
</script>
<template>
  <BaseDialog :show="show" trap-focus :title="t('welfare.redemptionTitle')" width="normal" :show-close-button="!busy" :close-on-escape="!busy" @close="emit('close')">
    <div class="wf-redemption space-y-5">
      <div class="rounded-xl bg-primary-50 p-4 dark:bg-primary-900/20"><p class="text-sm text-gray-500 dark:text-dark-300">{{ t('welfare.redeemable') }}</p><strong class="mt-1 block text-3xl text-primary-700 dark:text-primary-300">{{ welfareMoney(balance) }}</strong></div>
      <div><label for="welfare-redemption-amount" class="mb-2 block text-sm font-medium">{{ t('welfare.amount') }}</label><div class="flex gap-2"><input id="welfare-redemption-amount" v-model="amount" class="input min-w-0 flex-1" inputmode="decimal" autocomplete="off" placeholder="0.00" :disabled="busy || uncertain" aria-describedby="welfare-redemption-error" /><button data-testid="redeem-all" class="btn btn-secondary" :disabled="busy || quoting || uncertain" @click="request('all')">{{ t('welfare.all') }}</button></div><p id="welfare-redemption-error" class="mt-2 text-sm text-red-600 dark:text-red-400" role="alert">{{ validation || error }}</p></div>
      <div class="flex justify-between gap-4 text-sm"><span class="text-gray-500 dark:text-dark-300">{{ t('welfare.rate') }}</span><span>{{ t('welfare.rateValue') }}</span></div>
      <div v-if="quote" class="flex justify-between gap-4 text-sm"><span class="text-gray-500 dark:text-dark-300">{{ t('welfare.accountAfter') }}</span><strong>{{ welfareMoney(quote.account_balance_after) }}</strong></div>
      <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('welfare.redemptionHint') }}</p>
    </div>
    <template #footer><button class="btn btn-secondary" :disabled="busy" @click="emit('close')">{{ t('welfare.cancel') }}</button><button v-if="quote" data-testid="confirm-redeem" class="btn btn-primary" :disabled="busy" @click="confirm">{{ busy ? t('welfare.processing') : uncertain ? t('welfare.retryTransfer') : t('welfare.confirm') }}</button><button v-else data-testid="quote-partial" class="btn btn-primary" :disabled="busy || quoting || !amount || !!validation" @click="request('partial')">{{ quoting ? t('welfare.loading') : t('welfare.quote') }}</button></template>
  </BaseDialog>
</template>
