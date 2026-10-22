<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatMonitorMs, formatMonitorPercent, formatMonitorSuccessRateFromError } from '../monitorFormat'
import type { CardHealth, MonitorBarData } from './monitorCards'

const props = defineProps<{ bars: MonitorBarData[] }>()
const { t, locale } = useI18n()
const track = ref<HTMLElement | null>(null)
const tooltip = ref<HTMLElement | null>(null)
const tooltipId = useId()
const activeIndex = ref(0)
const visible = ref(false)
const position = ref({ left: '8px', top: '8px' })
const activeBar = computed(() => props.bars[activeIndex.value])
let anchor: HTMLElement | null = null

function statusLabel(health: CardHealth) {
  return t('channelMonitorV2.cards.status.' + (health === 'no-data' ? 'noData' : health))
}
function time(value: string) {
  return Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}
function interval(bar: MonitorBarData) {
  return time(bar.start) + ' – ' + time(bar.end)
}
const tooltipLines = computed(() => {
  const bar = activeBar.value
  if (!bar) return []
  const lines = [interval(bar), statusLabel(bar.health)]
  if (bar.metrics) {
    lines.push(
      t('channelMonitorV2.cards.availability') + ': ' + formatMonitorSuccessRateFromError(bar.metrics.error_rate),
      t('channelMonitorV2.cards.cacheRate') + ': ' + formatMonitorPercent(bar.metrics.cache_rate, locale.value),
      t('channelMonitorV2.cards.ttft') + ': ' + (bar.metrics.ttft.p50_ms == null ? t('channelMonitorV2.cards.ttftUnavailable') : formatMonitorMs(bar.metrics.ttft.p50_ms)),
    )
  }
  return lines
})

function positionTooltip() {
  if (!visible.value || !anchor || !tooltip.value) return
  const target = anchor.getBoundingClientRect()
  const box = tooltip.value.getBoundingClientRect()
  const gap = 8
  const left = Math.max(gap, Math.min(target.left + target.width / 2 - box.width / 2, window.innerWidth - box.width - gap))
  const above = target.top - box.height - gap
  const top = Math.max(gap, Math.min(above >= gap ? above : target.bottom + gap, window.innerHeight - box.height - gap))
  position.value = { left: left + 'px', top: top + 'px' }
}

async function show(index: number, event: MouseEvent | FocusEvent) {
  activeIndex.value = index
  anchor = event.currentTarget as HTMLElement
  visible.value = true
  await nextTick()
  positionTooltip()
}
function hide() { visible.value = false }
function onKeydown(event: KeyboardEvent, index: number) {
  if (event.key === 'Escape') {
    hide()
    event.preventDefault()
    return
  }
  const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End']
  if (!keys.includes(event.key)) return
  event.preventDefault()
  let next = index
  if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = props.bars.length - 1
  else next = Math.max(0, Math.min(props.bars.length - 1, index + (event.key === 'ArrowRight' ? 1 : -1)))
  activeIndex.value = next
  track.value?.querySelectorAll<HTMLButtonElement>('button')[next]?.focus()
}
watch(() => props.bars, () => { hide(); activeIndex.value = 0; anchor = null })
onMounted(() => {
  window.addEventListener('resize', positionTooltip)
  window.addEventListener('scroll', positionTooltip, true)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', positionTooltip)
  window.removeEventListener('scroll', positionTooltip, true)
})
</script>

<template>
  <div ref="track" class="monitor-bars" :aria-label="t('channelMonitorV2.cards.recentBuckets', { count: bars.length })" role="group">
    <button
      v-for="(bar, index) in bars"
      :key="bar.id"
      type="button"
      class="monitor-bar"
      :data-health="bar.health"
      :tabindex="index === activeIndex ? 0 : -1"
      :aria-label="interval(bar) + ': ' + statusLabel(bar.health)"
      :aria-describedby="visible && index === activeIndex ? tooltipId : undefined"
      @mouseenter="show(index, $event)"
      @mouseleave="hide"
      @focus="show(index, $event)"
      @blur="hide"
      @keydown="onKeydown($event, index)"
    ><span class="monitor-bar-fill" aria-hidden="true" /></button>
  </div>
  <Teleport to="body">
    <div v-if="visible && activeBar" :id="tooltipId" ref="tooltip" role="tooltip" class="monitor-bars-tooltip" :style="position">
      <p v-for="(line, index) in tooltipLines" :key="index" :class="{ 'monitor-bars-tooltip-title': index === 0 }" :title="index === 2 ? t('channelMonitorV2.cards.availabilityHint') : undefined">{{ line }}</p>
    </div>
  </Teleport>
</template>

<style scoped>
.monitor-bars { display: flex; gap: .25rem; min-width: 0; }
.monitor-bar { display: flex; align-items: flex-end; flex: 1; min-width: 0; height: 1.5rem; padding: 0; border: 0; border-radius: .2rem; background: transparent; cursor: help; }
.monitor-bar-fill { display: block; width: 100%; height: var(--bar-height); border-radius: .2rem; background: var(--bar-color); pointer-events: none; transform-origin: center bottom; transition: transform .12s ease, opacity .12s ease; }
.monitor-bar[data-health="healthy"] { --bar-color: #10b981; --bar-height: 100%; }
.monitor-bar[data-health="warning"] { --bar-color: #f59e0b; --bar-height: 65%; }
.monitor-bar[data-health="critical"] { --bar-color: #f43f5e; --bar-height: 35%; }
.monitor-bar[data-health="unknown"] { --bar-color: #94a3b8; --bar-height: 40%; }
.monitor-bar[data-health="no-data"] { --bar-color: #e2e8f0; --bar-height: 15%; }
.monitor-bar:hover .monitor-bar-fill { opacity: .85; transform: scale(1.06, 1.12); }
.monitor-bar:focus-visible { outline: 2px solid rgb(99 102 241); outline-offset: 2px; }
.monitor-bars-tooltip { position: fixed; z-index: 9999; box-sizing: border-box; width: max-content; max-width: calc(100vw - 16px); max-height: calc(100vh - 16px); overflow: auto; padding: .65rem .8rem; border: 1px solid rgb(226 232 240); border-radius: .75rem; background: rgb(255 255 255); color: rgb(71 85 105); box-shadow: 0 12px 28px rgb(15 23 42 / .16); pointer-events: none; font-size: .75rem; line-height: 1.6; overflow-wrap: anywhere; }
.monitor-bars-tooltip p { margin: 0; }
.monitor-bars-tooltip-title { font-weight: 600; color: rgb(15 23 42); padding-bottom: .2rem; }
.dark .monitor-bar[data-health="no-data"] { --bar-color: #334155; }
.dark .monitor-bar[data-health="unknown"] { --bar-color: #64748b; }
.dark .monitor-bars-tooltip { border-color: rgb(51 65 85); background: rgb(15 23 42); color: rgb(203 213 225); }
.dark .monitor-bars-tooltip-title { color: rgb(241 245 249); }
@media (prefers-reduced-motion: reduce) { .monitor-bar-fill { transition: none; animation: none; } .monitor-bar:hover .monitor-bar-fill { transform: none; } }
</style>
