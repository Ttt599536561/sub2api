<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { qqInviteAPI, type InviteCodePreview, type InviteID, type InviteInventory } from '@/api/qqInvite'
import { useInviteAdmin, useInviteBusy } from '@/composables/useInviteAdmin'
import { parseInviteCodes } from '@/utils/qqInvite'
const props = defineProps<{ campaignId: InviteID }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const { t } = useI18n()
const inventory = ref<InviteInventory | null>(null), preview = ref<InviteCodePreview | null>(null)
const pasted = ref(''), checkedInput = ref(''), page = ref(1), count = ref(10)
const { session, busy, blocked, error, notice, run } = useInviteAdmin(() => { pasted.value = ''; checkedInput.value = ''; preview.value = null; inventory.value = null })
useInviteBusy(busy, value => emit('busy', value))
const codes = computed(() => parseInviteCodes(pasted.value))
const canImport = computed(() => !!preview.value && preview.value.invalid_count === 0 && preview.value.valid_count > 0 && checkedInput.value === pasted.value)
watch(pasted, () => { preview.value = null; checkedInput.value = '' })
async function fetchStock() {
  inventory.value = null
  const value = await qqInviteAPI.codes(props.campaignId, page.value, session.signal)
  if (session.current()) inventory.value = value
}
function reload() { return run(fetchStock) }
function preflight() { return run(async () => {
  if (!codes.value.length || codes.value.length > 500) return
  preview.value = null; checkedInput.value = ''
  const text = pasted.value
  const value = await qqInviteAPI.previewCodes(props.campaignId, [...codes.value], session.signal)
  if (session.current() && pasted.value === text) { preview.value = value; checkedInput.value = text }
}) }
function importCodes() { if (!canImport.value) return; return run(async () => {
  preview.value = null; checkedInput.value = ''
  const value = await qqInviteAPI.importCodes(props.campaignId, [...codes.value], session.signal)
  if (!session.current()) return
  pasted.value = ''; checkedInput.value = ''; preview.value = null; page.value = 1
  notice.value = value?.invalid_count ? 'importChanged' : 'imported'
  await fetchStock()
}) }
function generate() { if (!Number.isInteger(count.value) || count.value < 1 || count.value > 500) return; return run(async () => {
  await qqInviteAPI.generateCodes(props.campaignId, count.value, session.signal)
  if (!session.current()) return
  notice.value = 'generated'; page.value = 1; await fetchStock()
}) }
function move(delta: number) { if (blocked.value || page.value + delta < 1) return; page.value += delta; void reload() }
function state(value: string) { return ['available', 'assigned', 'used', 'invalid', 'expired', 'revoked'].includes(value) ? value : 'unknown' }
function previewReason(value: string) { return ['duplicate', 'not_found', 'not_monthly_subscription', 'not_unused', 'expired', 'already_in_pool'].includes(value) ? value : 'unknown' }
onMounted(reload)
</script>

<template>
  <section class="space-y-4">
    <p v-if="session.invalidated.value" role="alert">{{ t('qqInvite.sessionChanged') }}</p>
    <template v-else>
      <div class="flex items-center justify-between gap-3"><h3 class="font-semibold">{{ t('qqInvite.inventory') }}</h3><button class="btn btn-secondary" :disabled="blocked" @click="reload">{{ t('qqInvite.reload') }}</button></div>
      <p v-if="busy" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert" class="text-red-600">{{ t(`qqInvite.${error}`) }}</p><p v-if="notice" role="status" class="text-green-700 dark:text-green-400">{{ t(`qqInvite.${notice}`) }}</p>
      <div class="card space-y-4 p-5">
        <p class="text-sm text-gray-500">{{ t('qqInvite.stockHint') }}</p>
        <label class="block text-sm">{{ t('qqInvite.pasteCodes') }}<textarea v-model="pasted" rows="5" autocomplete="off" spellcheck="false" class="input mt-1 w-full font-mono" :disabled="blocked" /></label>
        <div class="flex flex-wrap items-center gap-2"><button data-test="preview" class="btn btn-secondary" :disabled="blocked || !codes.length || codes.length > 500" @click="preflight">{{ t('qqInvite.preview') }}</button><button data-test="import" class="btn btn-primary" :disabled="blocked || !canImport" @click="importCodes">{{ t('qqInvite.import') }}</button><span class="text-xs text-gray-500">{{ t('qqInvite.batchLimit') }}</span></div>
        <div v-if="preview" class="space-y-2"><p class="text-sm">{{ t('qqInvite.valid') }} {{ preview.valid_count }} · {{ t('qqInvite.invalid') }} {{ preview.invalid_count }}</p><ul class="max-h-48 space-y-1 overflow-auto rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800"><li v-for="item in preview.items" :key="item.index" :class="item.valid ? 'text-green-700 dark:text-green-400' : 'text-red-600'">#{{ item.index + 1 }} {{ item.code_hint }} · {{ t(item.valid ? 'qqInvite.valid' : `qqInvite.codeReasons.${previewReason(item.reason)}`) }}</li></ul></div>
        <form class="flex flex-wrap items-end gap-3 border-t border-gray-100 pt-4 dark:border-dark-700" @submit.prevent="generate"><label class="text-sm">{{ t('qqInvite.generateCount') }}<input v-model.number="count" class="input mt-1 block w-28" type="number" min="1" max="500" step="1" required :disabled="blocked" /></label><button type="submit" class="btn btn-secondary" :disabled="blocked || !Number.isInteger(count) || count < 1 || count > 500">{{ t('qqInvite.generate') }}</button></form>
      </div>
      <div v-if="inventory" class="card space-y-4 p-5">
        <div class="flex flex-wrap gap-5 text-sm"><span>{{ t('qqInvite.available') }} <strong>{{ inventory.available }}</strong></span><span>{{ t('qqInvite.assigned') }} <strong>{{ inventory.assigned }}</strong></span><span>{{ t('qqInvite.total') }} <strong>{{ inventory.total }}</strong></span></div>
        <p v-if="!inventory.items?.length" class="text-sm text-gray-500">{{ t('qqInvite.noCodes') }}</p>
        <div v-else class="overflow-x-auto"><table class="w-full text-left text-sm"><thead><tr class="border-b border-gray-100 dark:border-dark-700"><th class="p-2">{{ t('qqInvite.maskedCode') }}</th><th class="p-2">{{ t('qqInvite.status') }}</th><th class="p-2">{{ t('qqInvite.createdAt') }}</th></tr></thead><tbody><tr v-for="item in inventory.items" :key="item.id" class="border-b border-gray-100 dark:border-dark-700"><td class="p-2 font-mono">{{ item.code_hint }}</td><td class="p-2">{{ t(`qqInvite.codeStates.${state(item.state)}`) }}</td><td class="p-2">{{ new Date(item.created_at).toLocaleString() }}</td></tr></tbody></table></div>
        <div class="flex items-center justify-end gap-3"><button class="btn btn-secondary" :disabled="blocked || page <= 1" @click="move(-1)">{{ t('qqInvite.previous') }}</button><span class="text-sm">{{ page }} / {{ Math.max(1, Math.ceil(inventory.total / 20)) }}</span><button class="btn btn-secondary" :disabled="blocked || page * 20 >= inventory.total" @click="move(1)">{{ t('qqInvite.next') }}</button></div>
      </div>
    </template>
  </section>
</template>
