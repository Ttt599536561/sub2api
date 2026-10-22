import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import MonitorCard from '../MonitorCard.vue'
import MonitorCards from '../MonitorCards.vue'
import MonitorBars from '../MonitorBars.vue'
import { buildCards, groupByProvider } from '../monitorCards'
import { coverage, group, health, row, zeroMetrics } from './fixtures'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    locale: { value: 'en' },
    t: (key: string, params?: Record<string, unknown>) => {
      const labels: Record<string, string> = {
        cacheRate: 'Cache rate', availability: 'Availability', ttft: 'First token',
        recentBuckets: 'Recent {count} intervals', refreshIn: 'Refresh in {seconds}s',
        rateMultiplier: 'User rate {rate}×', rateHint: 'Base or user rate; excludes extra adjustments',
        providerCount: '{count} channels', groupFallback: 'Group {id}', platformFallback: 'Other',
        empty: 'No channel data', ttftUnavailable: 'No first-token samples',
        'status.healthy': 'Healthy', 'status.warning': 'Degraded', 'status.critical': 'Unhealthy',
        'status.unknown': 'Insufficient samples', 'status.noData': 'No data',
      }
      return (labels[key.replace('channelMonitorV2.cards.', '')] ?? key).replace(/\{(\w+)\}/g, (_, name) => String(params?.[name] ?? ''))
    },
  }),
}))

const cards = () => buildCards({ range: '90m', coverage, rows: [row()], dimensionGroups: [], groups: [group()], userRates: { 7: 0 }, ratesAvailable: true })
const mounted: Array<{ unmount(): void }> = []
afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('MonitorCard', () => {
  it('shows localized names, zero user pricing and backend metric states using ratio formatting', () => {
    const wrapper = mount(MonitorCard, { props: { card: cards()[0], countdownSeconds: 57 } })
    mounted.push(wrapper)
    expect(wrapper.text()).toContain('本地化名称')
    expect(wrapper.text()).toContain('User rate 0×')
    expect(wrapper.get('[data-metric="cache"]').text()).toContain('80.0%')
    expect(wrapper.get('[data-metric="availability"]').text()).toContain('97.5%')
    expect(wrapper.get('[data-metric="availability"]').attributes('data-health')).toBe('critical')
    expect(wrapper.get('[data-metric="ttft"]').attributes('data-health')).toBe('healthy')
    expect(wrapper.get('[data-testid="card-health"]').text()).toBe('Degraded')
    expect(wrapper.text()).toContain('Recent 18 intervals')
    expect(wrapper.text()).toContain('Refresh in 57s')
  })

  it('hides missing multipliers and displays idle metrics as dashes', () => {
    const card = buildCards({ range: '90m', coverage, rows: [row({ metrics: zeroMetrics, buckets: [] })], dimensionGroups: [], groups: [], userRates: {}, ratesAvailable: false })[0]
    const wrapper = mount(MonitorCard, { props: { card, countdownSeconds: -4 } })
    mounted.push(wrapper)
    expect(wrapper.find('[data-testid="card-rate"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="card-health"]').text()).toBe('No data')
    expect(wrapper.get('[data-metric="cache"]').text()).toContain('—')
    expect(wrapper.get('[data-metric="ttft"]').text()).toContain('—')
    expect(wrapper.text()).toContain('Refresh in 0s')
  })

  it('preserves unknown traffic as insufficient samples', () => {
    const card = cards()[0]
    const wrapper = mount(MonitorCard, { props: { card: { ...card, health: 'unknown' }, countdownSeconds: 4 } })
    mounted.push(wrapper)
    expect(wrapper.get('[data-testid="card-health"]').text()).toBe('Insufficient samples')
  })
})

describe('MonitorCards', () => {
  it('renders a provider heading and channel count for each section', () => {
    const wrapper = mount(MonitorCards, { props: { sections: groupByProvider(cards()), countdownSeconds: 8 } })
    mounted.push(wrapper)
    expect(wrapper.get('h2').text()).toContain('OpenAI')
    expect(wrapper.get('[data-testid="provider-count"]').attributes('aria-label')).toBe('1 channels')
    expect(wrapper.findAllComponents(MonitorCard)).toHaveLength(1)
  })

  it('renders a localized empty state', () => {
    const wrapper = mount(MonitorCards, { props: { sections: [], countdownSeconds: 8 } })
    mounted.push(wrapper)
    expect(wrapper.text()).toBe('No channel data')
  })
})

describe('MonitorBars', () => {
  it('teleports hover details to body with interval times, raw metrics and no absolute request counts', async () => {
    const wrapper = mount(MonitorBars, { attachTo: document.body, props: { bars: cards()[0].bars } })
    mounted.push(wrapper)
    const bars = wrapper.findAll('button')
    expect(bars).toHaveLength(18)
    await bars[17].trigger('mouseenter')
    await nextTick()
    const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement
    expect(tooltip).not.toBeNull()
    expect(wrapper.element.contains(tooltip)).toBe(false)
    expect(tooltip.textContent).toContain('Degraded')
    expect(tooltip.textContent).toContain('97.5%')
    expect(tooltip.textContent).toContain('120ms')
    expect(tooltip.textContent).toContain('10/23')
    expect(tooltip.textContent).not.toContain('requests')
    expect(bars[17].attributes('aria-describedby')).toBe(tooltip.id)
    await bars[17].trigger('mouseleave')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('supports keyboard focus, arrow navigation and Escape dismissal', async () => {
    const wrapper = mount(MonitorBars, { attachTo: document.body, props: { bars: cards()[0].bars } })
    mounted.push(wrapper)
    const bars = wrapper.findAll('button')
    expect(bars[0].attributes('tabindex')).toBe('0')
    expect(bars[1].attributes('tabindex')).toBe('-1')
    ;(bars[0].element as HTMLElement).focus()
    await nextTick()
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('No data')
    await bars[0].trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement).toBe(bars[1].element)
    await bars[1].trigger('keydown', { key: 'End' })
    expect(document.activeElement).toBe(bars[17].element)
    await bars[17].trigger('keydown', { key: 'Escape' })
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('clamps a tooltip to the viewport and positions it below a trigger near the top edge', async () => {
    const original = HTMLElement.prototype.getBoundingClientRect
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      if (this.getAttribute('role') === 'tooltip') return { x: 0, y: 0, width: 280, height: 160, top: 0, right: 280, bottom: 160, left: 0, toJSON: () => ({}) }
      if (this.tagName === 'BUTTON') return { x: 2, y: 2, width: 8, height: 20, top: 2, right: 10, bottom: 22, left: 2, toJSON: () => ({}) }
      return original.call(this)
    })
    const wrapper = mount(MonitorBars, { attachTo: document.body, props: { bars: cards()[0].bars } })
    mounted.push(wrapper)
    await wrapper.findAll('button')[17].trigger('mouseenter')
    await nextTick()
    const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement
    expect(parseFloat(tooltip.style.left)).toBeGreaterThanOrEqual(8)
    expect(parseFloat(tooltip.style.left) + 280).toBeLessThanOrEqual(window.innerWidth - 8)
    expect(parseFloat(tooltip.style.top)).toBe(30)
  })

  it('shows zero-ratio real unknown buckets as samples pending and dismisses stale tooltips when data changes', async () => {
    const card = buildCards({ range: '90m', coverage, rows: [row({ buckets: [{ bucket_start: '2026-10-23T01:25:00Z', metrics: zeroMetrics, health: { ...health, overall: 'unknown' } }] })], dimensionGroups: [], groups: [], userRates: {}, ratesAvailable: false })[0]
    const wrapper = mount(MonitorBars, { attachTo: document.body, props: { bars: card.bars } })
    mounted.push(wrapper)
    await wrapper.findAll('button')[17].trigger('mouseenter')
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Insufficient samples')
    await wrapper.setProps({ bars: [] })
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })
})
