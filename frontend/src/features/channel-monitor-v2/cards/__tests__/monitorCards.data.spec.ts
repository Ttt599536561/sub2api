import { describe, expect, it } from 'vitest'
import { buildCards, groupByProvider } from '../monitorCards'
import { coverage, group, health, metrics, row, zeroMetrics } from './fixtures'
import type { MonitorRange } from '@/api/channelMonitorV2'
const input = () => ({ range: '90m' as MonitorRange, coverage, rows: [row()], dimensionGroups: [], groups: [group()], userRates: {}, ratesAvailable: true })

describe('monitor card data', () => {
  it('uses backend states independently of metric values', () => {
    const [card] = buildCards(input())
    expect(card.health).toBe('warning')
    expect(card.metricHealth).toEqual({ availability: 'critical', ttft: 'healthy', cache: 'warning' })
    expect(card.bars.at(-1)?.health).toBe('warning')
  })
  it('distinguishes redacted unknown real buckets from no traffic', () => {
    const unknown = { ...health, overall: 'unknown' as const }
    const [card] = buildCards({ ...input(), rows: [row({ metrics: zeroMetrics, health: unknown, buckets: [{ bucket_start: '2026-10-23T01:25:00Z', metrics: zeroMetrics, health: unknown }] })] })
    expect(card.hasTraffic).toBe(true)
    expect(card.health).toBe('unknown')
    expect(card.bars.at(-1)?.health).toBe('unknown')
  })
  it('does not paint prefilled TTFT/cache healthy on an idle window', () => {
    const [card] = buildCards({ ...input(), rows: [row({ metrics: { ...zeroMetrics, cache_rate: 1 }, buckets: [] })] })
    expect(card.health).toBe('no-data')
    expect(card.metricHealth).toEqual({ availability: 'no-data', ttft: 'no-data', cache: 'no-data' })
  })
  it('keeps unavailable TTFT neutral', () => {
    const [card] = buildCards({ ...input(), rows: [row({ metrics: { ...metrics, ttft: { ...metrics.ttft, p50_ms: null } } })] })
    expect(card.metricHealth.ttft).toBe('unknown')
  })
  it('fills sparse requested coverage with empty intervals', () => {
    const [card] = buildCards({ ...input(), coverage: { ...coverage, coverage_start: '2026-10-23T01:25:00Z', coverage_complete: false } })
    expect(card.bars).toHaveLength(18)
    expect(card.bars[0]).toMatchObject({ start: '2026-10-23T00:00:00.000Z', end: '2026-10-23T00:05:00.000Z', health: 'no-data', metrics: null })
    expect(card.bars.filter(bar => bar.health === 'no-data')).toHaveLength(17)
    expect(card.bars.at(-1)?.metrics).toEqual(metrics)
  })
  it.each([['90m', 18], ['24h', 24], ['7d', 14], ['30d', 30]] as const)('caps %s at the last %s original buckets', (range, count) => {
    const [card] = buildCards({ ...input(), range, coverage: { ...coverage, bucket_seconds: 60 } })
    expect(card.bars).toHaveLength(count)
    expect(card.bars.at(-1)?.start).toBe('2026-10-23T01:29:00.000Z')
  })
  it('aligns native buckets while clipping partial range edges', () => {
    const [card] = buildCards({ ...input(), coverage: { ...coverage, requested_start: '2026-10-23T01:21:00Z', requested_end: '2026-10-23T01:29:00Z' }, rows: [row({ buckets: [{ bucket_start: '2026-10-23T01:20:00Z', metrics, health }] })] })
    expect(card.bars).toHaveLength(2)
    expect(card.bars[0]).toMatchObject({ health: 'warning', start: '2026-10-23T01:21:00.000Z' })
    expect(card.bars.at(-1)?.end).toBe('2026-10-23T01:29:00.000Z')
  })
  it.each([{ requested_start: 'bad' }, { requested_end: 'bad' }, { requested_end: coverage.requested_start }, { bucket_seconds: 0 }, { bucket_seconds: Infinity }, { bucket_seconds: 0.000001 }])('handles invalid coverage: %o', patch => {
    expect(buildCards({ ...input(), coverage: { ...coverage, ...patch } })[0].bars).toEqual([])
  })
  it('ignores malformed and out-of-range buckets as traffic evidence', () => {
    const [card] = buildCards({ ...input(), rows: [row({ metrics: zeroMetrics, buckets: [{ bucket_start: '2026-10-22T01:25:00Z', metrics, health }, { bucket_start: 'bad', metrics, health }] })] })
    expect(card.health).toBe('no-data')
    expect(card.bars.every(bar => bar.health === 'no-data')).toBe(true)
  })
  it('prefers localized names and preserves zero custom pricing', () => {
    const [card] = buildCards({ ...input(), dimensionGroups: [{ id: 7, name: 'Dimension', request_count: 40 }], userRates: { 7: 0 } })
    expect(card.name).toBe('本地化名称')
    expect(card.rateMultiplier).toBe(0)
  })
  it('uses explicit base pricing and hides missing or failed pricing', () => {
    expect(buildCards(input())[0].rateMultiplier).toBe(1.5)
    expect(buildCards({ ...input(), ratesAvailable: false })[0].rateMultiplier).toBeNull()
    expect(buildCards({ ...input(), groups: [] })[0].rateMultiplier).toBeNull()
    expect(buildCards({ ...input(), userRates: { 7: NaN } })[0].rateMultiplier).toBeNull()
  })
  it('normalizes aliases and keeps the approved provider order', () => {
    const cards = buildCards({ ...input(), rows: ['grok', 'Codex', 'Kiro', 'ChatGPT', 'Claude', 'gemini'].map((platform, index) => row({ platform, group_id: index + 1 })), groups: [] })
    const sections = groupByProvider(cards)
    expect(sections.map(section => section.provider)).toEqual(['openai', 'anthropic', 'gemini', 'grok'])
    expect(sections.find(section => section.provider === 'openai')?.cards).toHaveLength(2)
  })
  it('uses actual matrix platforms for composite groups and omits duplicate placeholders', () => {
    const cards = buildCards({ ...input(), groups: [group({ platform: 'composite' })], rows: [row(), row({ platform: 'anthropic' })], dimensionGroups: [{ id: 7, name: 'Composite', platform: 'composite', request_count: 100 }] })
    expect(cards).toHaveLength(2)
    expect(cards.map(card => card.provider).sort()).toEqual(['anthropic', 'openai'])
  })
  it('preserves idle composite dimensions as a no-data card', () => {
    const cards = buildCards({ ...input(), rows: [], groups: [group({ platform: 'composite' })], dimensionGroups: [{ id: 7, name: 'Composite', platform: 'composite', request_count: 0 }] })
    expect(cards).toHaveLength(1)
    expect(cards[0]).toMatchObject({ provider: 'composite', health: 'no-data', name: '本地化名称' })
    expect(cards[0].bars).toHaveLength(18)
  })
  it('sorts by rate then name then id, keeping unavailable pricing last and input intact', () => {
    const rows = [row({ group_id: 1, group_name: 'Z' }), row({ group_id: 2, group_name: 'A' }), row({ group_id: 3, group_name: 'M' }), row({ group_id: 4, group_name: 'Q' })]
    const cards = buildCards({ ...input(), rows, groups: [], userRates: { 1: 2, 2: 2, 3: 0 } })
    expect(cards.map(card => card.groupId)).toEqual([3, 2, 1, 4])
    expect(rows.map(item => item.group_id)).toEqual([1, 2, 3, 4])
  })
  it('uses ordinary dimension platform before group or matrix platforms', () => {
    const cards = buildCards({ ...input(), dimensionGroups: [{ id: 7, name: 'Kiro', platform: 'kiro', request_count: 40 }] })
    expect(cards[0].provider).toBe('anthropic')
    expect(cards[0].platform).toBe('kiro')
  })
  it('uses an ordinary group platform when dimension platform is absent', () => {
    expect(buildCards({ ...input(), groups: [group({ platform: 'anthropic' })] })[0].provider).toBe('anthropic')
  })
  it('retains nonzero summarized error ratios without a bucket', () => {
    expect(buildCards({ ...input(), rows: [row({ metrics: { ...zeroMetrics, error_rate: 0.2 }, buckets: [] })] })[0].hasTraffic).toBe(true)
  })
  it('retains an explicit nonzero success ratio without buckets or absolute counts', () => {
    expect(buildCards({ ...input(), rows: [row({ metrics: { ...zeroMetrics, success_rate: 0.8 }, buckets: [] })] })[0].hasTraffic).toBe(true)
  })
  it('keeps distinct source platforms uniquely keyed when metadata places them under one provider', () => {
    const cards = buildCards({ ...input(), rows: [row(), row({ platform: 'codex' })] })
    expect(new Set(cards.map(card => card.id)).size).toBe(2)
    expect(cards.every(card => card.provider === 'openai')).toBe(true)
  })
  it('omits platform-only and invalid group rows from this group card surface', () => {
    const rows = [row({ group_id: undefined }), row({ group_id: 0 }), row({ group_id: -1 }), row({ group_id: NaN })]
    expect(buildCards({ ...input(), rows })).toEqual([])
  })
})
