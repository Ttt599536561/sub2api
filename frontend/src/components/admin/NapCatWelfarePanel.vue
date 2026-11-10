<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import NapCatConnectionPanel from './napcat/NapCatConnectionPanel.vue'
import NapCatCampaignPanel from './napcat/NapCatCampaignPanel.vue'
import { useInviteBusy } from '@/composables/useInviteAdmin'
const { t } = useI18n()
const tab = ref<'connection' | 'campaigns'>('connection')
const busy = ref(false)
const emit = defineEmits<{ busy: [value: boolean] }>()
useInviteBusy(busy, value => emit('busy', value))
</script>
<template>
  <div class="space-y-5 text-gray-900 dark:text-white">
    <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('qqInvite.description') }}</p>
    <div class="flex flex-wrap gap-2"><button v-for="item in ['connection', 'campaigns'] as const" :key="item" class="btn" :class="tab === item ? 'btn-primary' : 'btn-secondary'" :disabled="busy" @click="tab = item">{{ t(`qqInvite.${item}`) }}</button></div>
    <NapCatConnectionPanel v-if="tab === 'connection'" @busy="busy = $event" />
    <NapCatCampaignPanel v-else @busy="busy = $event" />
  </div>
</template>
