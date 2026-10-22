<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { GroupPlatform } from '@/types'
import MonitorCard from './MonitorCard.vue'
import type { ProviderSection } from './monitorCards'

defineProps<{ sections: ProviderSection[]; countdownSeconds: number }>()
const { t } = useI18n()
</script>

<template>
  <div class="monitor-cards">
    <p v-if="!sections.length" class="monitor-cards-empty">{{ t('channelMonitorV2.cards.empty') }}</p>
    <section v-for="section in sections" :key="section.provider" class="monitor-provider-section" :data-provider="section.provider">
      <div class="monitor-provider-heading">
        <span class="monitor-section-icon" aria-hidden="true"><PlatformIcon :platform="section.provider as GroupPlatform" size="lg" /></span>
        <h2>{{ section.label || t('channelMonitorV2.cards.platformFallback') }}</h2>
        <span data-testid="provider-count" class="monitor-provider-count" :aria-label="t('channelMonitorV2.cards.providerCount', { count: section.cards.length })">{{ section.cards.length }}</span>
      </div>
      <div class="monitor-cards-grid">
        <MonitorCard v-for="card in section.cards" :key="card.id" :card="card" :countdown-seconds="countdownSeconds" />
      </div>
    </section>
  </div>
</template>

<style scoped>
.monitor-cards { min-width: 0; }
.monitor-provider-section + .monitor-provider-section { margin-top: 1.5rem; }
.monitor-provider-heading { display: flex; align-items: center; gap: .65rem; margin-bottom: .875rem; }
.monitor-provider-heading h2 { font-size: 1.125rem; line-height: 1.5; font-weight: 700; color: rgb(15 23 42); }
.monitor-section-icon { display: flex; align-items: center; justify-content: center; width: 2.25rem; height: 2.25rem; border-radius: .8rem; color: #059669; background: rgb(16 185 129 / .1); }
[data-provider="anthropic"] .monitor-section-icon { color: #c98239; background: rgb(201 130 57 / .1); }
[data-provider="gemini"] .monitor-section-icon { color: #3b82f6; background: rgb(59 130 246 / .1); }
.monitor-provider-count { padding: .1rem .45rem; border-radius: 999px; font-size: .75rem; font-weight: 600; color: rgb(100 116 139); background: rgb(226 232 240); }
.monitor-cards-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 1rem; }
.monitor-cards-empty { padding: 3rem 1rem; text-align: center; color: rgb(100 116 139); }
.dark .monitor-provider-heading h2 { color: rgb(241 245 249); }
.dark .monitor-provider-count { color: rgb(203 213 225); background: rgb(51 65 85); }
.dark .monitor-section-icon { color: #34d399; }
.dark [data-provider="anthropic"] .monitor-section-icon { color: #e6a958; }
.dark [data-provider="gemini"] .monitor-section-icon { color: #60a5fa; }
@media (min-width: 640px) { .monitor-cards-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (min-width: 1100px) { .monitor-cards-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (min-width: 1536px) { .monitor-cards-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
</style>
