<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { GroupPlatform } from '@/types'
import { formatMonitorMs, formatMonitorPercent, formatMonitorSuccessRateFromError } from '../monitorFormat'
import MonitorBars from './MonitorBars.vue'
import type { CardHealth, MonitorCardData } from './monitorCards'

const props = defineProps<{ card: MonitorCardData; countdownSeconds: number }>()
const { t, locale } = useI18n()
const iconPlatform = computed(() => props.card.provider as GroupPlatform)
const name = computed(() => props.card.name || t('channelMonitorV2.cards.groupFallback', { id: props.card.groupId }))
const countdown = computed(() => Number.isFinite(props.countdownSeconds) ? Math.max(0, Math.ceil(props.countdownSeconds)) : 0)
const rate = computed(() => props.card.rateMultiplier == null ? '' : Intl.NumberFormat(locale.value, { maximumFractionDigits: 4 }).format(props.card.rateMultiplier))
const metricValues = computed(() => {
  const metrics = props.card.hasTraffic ? props.card.metrics : null
  return {
    cache: metrics ? formatMonitorPercent(metrics.cache_rate, locale.value) : '—',
    availability: metrics ? formatMonitorSuccessRateFromError(metrics.error_rate) : '—',
    ttft: metrics?.ttft.p50_ms != null ? formatMonitorMs(metrics.ttft.p50_ms) : '—',
  }
})
function statusLabel(health: CardHealth) {
  return t('channelMonitorV2.cards.status.' + (health === 'no-data' ? 'noData' : health))
}
</script>

<template>
  <article class="monitor-card" :data-provider="card.provider">
    <div class="monitor-card-header">
      <span class="monitor-provider-icon" aria-hidden="true"><PlatformIcon :platform="iconPlatform" size="lg" /></span>
      <div class="monitor-card-identity">
        <h3 :title="name">{{ name }}</h3>
        <div class="monitor-card-tags">
          <span class="monitor-platform-tag">{{ card.providerLabel || t('channelMonitorV2.cards.platformFallback') }}</span>
          <span v-if="card.rateMultiplier !== null" data-testid="card-rate" class="monitor-rate-tag" :title="t('channelMonitorV2.cards.rateHint')">
            {{ t('channelMonitorV2.cards.rateMultiplier', { rate }) }}
          </span>
        </div>
      </div>
      <span data-testid="card-health" class="monitor-health-badge" :data-health="card.health">{{ statusLabel(card.health) }}</span>
    </div>

    <dl class="monitor-card-metrics">
      <div data-metric="cache" :data-health="card.metricHealth.cache">
        <dt>{{ t('channelMonitorV2.cards.cacheRate') }}</dt>
        <dd :aria-label="metricValues.cache + ', ' + statusLabel(card.metricHealth.cache)">{{ metricValues.cache }}</dd>
      </div>
      <div data-metric="availability" :data-health="card.metricHealth.availability">
        <dt :title="t('channelMonitorV2.cards.availabilityHint')">{{ t('channelMonitorV2.cards.availability') }}</dt>
        <dd :title="t('channelMonitorV2.cards.availabilityHint')" :aria-label="metricValues.availability + ', ' + statusLabel(card.metricHealth.availability)">{{ metricValues.availability }}</dd>
      </div>
      <div data-metric="ttft" :data-health="card.metricHealth.ttft">
        <dt>{{ t('channelMonitorV2.cards.ttft') }}</dt>
        <dd :aria-label="metricValues.ttft + ', ' + statusLabel(card.metricHealth.ttft)" :title="card.hasTraffic && metricValues.ttft === '—' ? t('channelMonitorV2.cards.ttftUnavailable') : undefined">{{ metricValues.ttft }}</dd>
      </div>
    </dl>

    <div class="monitor-card-timeline">
      <div class="monitor-card-timeline-heading">
        <span>{{ t('channelMonitorV2.cards.recentBuckets', { count: card.bars.length }) }}</span>
        <span>{{ t('channelMonitorV2.cards.refreshIn', { seconds: countdown }) }}</span>
      </div>
      <MonitorBars :bars="card.bars" />
    </div>
  </article>
</template>

<style scoped>
.monitor-card { --provider-color: #059669; display: flex; flex-direction: column; container-type: inline-size; min-width: 0; padding: 1.25rem; border: 1px solid rgb(226 232 240); border-radius: 1.25rem; background: rgb(255 255 255); box-shadow: 0 2px 8px rgb(15 23 42 / .025); }
.monitor-card[data-provider="anthropic"] { --provider-color: #c98239; }
.monitor-card[data-provider="gemini"] { --provider-color: #3b82f6; }
.monitor-card[data-provider="grok"] { --provider-color: #64748b; }
.monitor-card-header { display: flex; align-items: flex-start; gap: .75rem; }
.monitor-provider-icon { display: flex; flex: none; align-items: center; justify-content: center; width: 2.625rem; height: 2.625rem; border-radius: .875rem; color: var(--provider-color); background: color-mix(in srgb, var(--provider-color) 12%, transparent); }
.monitor-card-identity { min-width: 0; flex: 1; }
.monitor-card-identity h3 { margin: 0; color: rgb(15 23 42); font-size: 1rem; font-weight: 700; line-height: 1.4; overflow-wrap: anywhere; }
.monitor-card-tags { display: flex; flex-wrap: wrap; align-items: center; gap: .3rem; margin-top: .3rem; font-size: .625rem; line-height: 1.5; }
.monitor-platform-tag, .monitor-rate-tag { padding: .1rem .35rem; border-radius: .35rem; }
.monitor-platform-tag { color: var(--provider-color); background: color-mix(in srgb, var(--provider-color) 10%, transparent); }
.monitor-rate-tag { color: rgb(100 116 139); background: rgb(241 245 249); font-variant-numeric: tabular-nums; }
.monitor-health-badge { flex: none; max-width: 6.75rem; padding: .2rem .55rem; border-radius: 999px; background: rgb(241 245 249); color: rgb(100 116 139); font-size: .625rem; font-weight: 600; text-align: center; line-height: 1.5; }
.monitor-health-badge[data-health="healthy"] { color: #047857; background: rgb(16 185 129 / .12); }
.monitor-health-badge[data-health="warning"] { color: #a16207; background: rgb(245 158 11 / .12); }
.monitor-health-badge[data-health="critical"] { color: #be123c; background: rgb(244 63 94 / .12); }
.monitor-card-metrics { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: .5rem; margin: 1rem 0; }
.monitor-card-metrics > div { min-width: 0; border: 1px solid rgb(226 232 240 / .8); border-radius: .875rem; padding: .65rem .7rem; background: rgb(248 250 252 / .7); }
.monitor-card-metrics dt { color: rgb(100 116 139); font-size: .625rem; line-height: 1.4; min-height: .875rem; }
.monitor-card-metrics dd { margin: .4rem 0 0; color: rgb(100 116 139); font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace; font-size: clamp(.75rem, 5.5cqw, 1.125rem); font-weight: 700; line-height: 1.35; letter-spacing: normal; font-variant-numeric: tabular-nums; white-space: nowrap; }
.monitor-card-metrics [data-health="healthy"] dd { color: #059669; }
.monitor-card-metrics [data-health="warning"] dd { color: #d97706; }
.monitor-card-metrics [data-health="critical"] dd { color: #e11d48; }
.monitor-card-timeline { margin-top: auto; padding-top: .875rem; border-top: 1px solid rgb(226 232 240); }
.monitor-card-timeline-heading { display: flex; flex-wrap: wrap; justify-content: space-between; gap: .25rem .5rem; margin-bottom: .5rem; color: rgb(100 116 139); font-size: .625rem; font-variant-numeric: tabular-nums; }
.dark .monitor-card { background: rgb(17 28 44 / .9); border-color: rgb(51 65 85 / .65); --provider-color: #34d399; }
.dark .monitor-card[data-provider="anthropic"] { --provider-color: #e6a958; }
.dark .monitor-card[data-provider="gemini"] { --provider-color: #60a5fa; }
.dark .monitor-card-identity h3 { color: rgb(241 245 249); }
.dark .monitor-rate-tag { background: rgb(51 65 85); color: rgb(203 213 225); }
.dark .monitor-card-metrics > div { border-color: rgb(51 65 85 / .5); background: rgb(9 19 35 / .4); }
.dark .monitor-card-metrics dt, .dark .monitor-card-timeline-heading { color: rgb(148 163 184); }
.dark .monitor-card-metrics dd { color: rgb(148 163 184); }
.dark .monitor-card-metrics [data-health="healthy"] dd { color: #34d399; }
.dark .monitor-card-metrics [data-health="warning"] dd { color: #fbbf24; }
.dark .monitor-card-metrics [data-health="critical"] dd { color: #fb7185; }
.dark .monitor-card-timeline { border-color: rgb(51 65 85 / .6); }
.dark .monitor-health-badge { background: rgb(51 65 85 / .6); color: rgb(148 163 184); }
.dark .monitor-health-badge[data-health="healthy"] { background: rgb(16 185 129 / .15); color: #6ee7b7; }
.dark .monitor-health-badge[data-health="warning"] { background: rgb(245 158 11 / .15); color: #fcd34d; }
.dark .monitor-health-badge[data-health="critical"] { background: rgb(244 63 94 / .15); color: #fda4af; }
@media (prefers-reduced-motion: reduce) { .monitor-card, .monitor-card * { transition: none !important; animation: none !important; } }
</style>
