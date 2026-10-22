import type {
  HealthState,
  MonitorCoverage,
  MonitorDimensions,
  MonitorMatrixBucket,
  MonitorMatrixRow,
  MonitorMetric,
  MonitorRange,
} from '@/api/channelMonitorV2'
import type { Group } from '@/types'
import { isTtftUnavailable } from '../monitorFormat'

export type CardHealth = HealthState | 'no-data'

export interface MonitorBarData {
  id: string
  start: string
  end: string
  health: CardHealth
  metrics: MonitorMetric | null
}

export interface MonitorCardData {
  id: string
  groupId?: number
  name: string
  platform: string
  provider: string
  providerLabel: string
  rateMultiplier: number | null
  hasTraffic: boolean
  health: CardHealth
  metricHealth: { availability: CardHealth; cache: CardHealth; ttft: CardHealth }
  metrics: MonitorMetric | null
  bars: MonitorBarData[]
}

export interface ProviderSection {
  provider: string
  label: string
  cards: MonitorCardData[]
}

export interface BuildCardsInput {
  range: MonitorRange
  coverage: MonitorCoverage | null | undefined
  rows: MonitorMatrixRow[]
  dimensionGroups: MonitorDimensions['groups']
  groups: Group[]
  userRates: Record<number, number>
  ratesAvailable: boolean
}

const BAR_COUNT: Record<MonitorRange, number> = { '90m': 18, '24h': 24, '7d': 14, '30d': 30 }
const PROVIDERS: Record<string, string> = {
  openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Gemini', grok: 'Grok',
  antigravity: 'Antigravity', kimi: 'Kimi', zhipu: 'Zhipu', deepseek: 'DeepSeek',
  composite: 'Composite',
}
const PROVIDER_ORDER = Object.keys(PROVIDERS)
const ALIASES: Record<string, string> = { claude: 'anthropic', kiro: 'anthropic', codex: 'openai', chatgpt: 'openai' }

interface CoverageWindow { start: number; end: number; step: number }

function coverageWindow(coverage: BuildCardsInput['coverage']): CoverageWindow | null {
  if (!coverage || !Number.isSafeInteger(coverage.bucket_seconds) || coverage.bucket_seconds <= 0) return null
  const start = Date.parse(coverage.requested_start)
  const end = Date.parse(coverage.requested_end ?? coverage.computed_at)
  const step = coverage.bucket_seconds * 1000
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || !Number.isSafeInteger(step)) return null
  return { start, end, step }
}

function validBuckets(row: MonitorMatrixRow | undefined, window: CoverageWindow | null): MonitorMatrixBucket[] {
  return (row?.buckets ?? []).filter(bucket => {
    const start = Date.parse(bucket.bucket_start)
    return Number.isFinite(start) && (!window || (start < window.end && start + window.step > window.start))
  })
}

function buildBars(buckets: MonitorMatrixBucket[], window: CoverageWindow | null, range: MonitorRange): MonitorBarData[] {
  if (!window) return []
  const { start, end, step } = window
  const first = Math.floor(start / step)
  const last = Math.ceil(end / step) - 1
  const from = Math.max(first, last - BAR_COUNT[range] + 1)
  const indexed = new Map(buckets.map(bucket => [Math.floor(Date.parse(bucket.bucket_start) / step), bucket]))
  const bars: MonitorBarData[] = []
  // Bound allocation before iteration, even if malformed coverage spans years.
  for (let index = from; index <= last; index++) {
    const bucket = indexed.get(index)
    const slotStart = new Date(Math.max(start, index * step)).toISOString()
    bars.push({
      id: String(index),
      start: slotStart,
      end: new Date(Math.min(end, (index + 1) * step)).toISOString(),
      health: bucket ? bucket.health.overall : 'no-data',
      metrics: bucket?.metrics ?? null,
    })
  }
  return bars
}

function hasMetricFlow(metrics: MonitorMetric | undefined): boolean {
  if (!metrics) return false
  // TTFT/cache samples and ratios can be prefilled for idle rows. Real buckets
  // remain the traffic signal when privacy redaction removes absolute counts.
  return [
    metrics.request_count, metrics.success_requests, metrics.error_requests,
    metrics.token_count, metrics.rpm, metrics.tpm,
    metrics.success_rate, metrics.error_rate,
    metrics.upstream_affected_requests, metrics.upstream_attempt_count,
  ].some(value => typeof value === 'number' && Number.isFinite(value) && value > 0)
}

function compareProvider(a: string, b: string): number {
  const aRank = PROVIDER_ORDER.indexOf(a)
  const bRank = PROVIDER_ORDER.indexOf(b)
  return (aRank < 0 ? PROVIDER_ORDER.length : aRank) - (bRank < 0 ? PROVIDER_ORDER.length : bRank) || a.localeCompare(b)
}

function compareCard(a: MonitorCardData, b: MonitorCardData): number {
  return compareProvider(a.provider, b.provider) ||
    (a.rateMultiplier ?? Infinity) - (b.rateMultiplier ?? Infinity) ||
    a.name.localeCompare(b.name) ||
    (a.groupId ?? Number.MAX_SAFE_INTEGER) - (b.groupId ?? Number.MAX_SAFE_INTEGER) ||
    a.id.localeCompare(b.id)
}

export function buildCards(input: BuildCardsInput): MonitorCardData[] {
  const groups = new Map(input.groups.map(group => [group.id, group]))
  const dimensions = new Map(input.dimensionGroups.map(group => [group.id, group]))
  const window = coverageWindow(input.coverage)
  const rows = input.rows.filter(row => Number.isSafeInteger(row.group_id) && (row.group_id ?? 0) > 0)
  const represented = new Set(rows.map(row => row.group_id))
  const candidates: Array<{ row?: MonitorMatrixRow; groupId?: number; platform: string }> = rows.map(row => ({
    row, groupId: row.group_id, platform: row.platform,
  }))
  for (const dimension of input.dimensionGroups) {
    if (Number.isSafeInteger(dimension.id) && dimension.id > 0 && !represented.has(dimension.id)) {
      candidates.push({ groupId: dimension.id, platform: dimension.platform || groups.get(dimension.id)?.platform || '' })
      represented.add(dimension.id)
    }
  }
  return candidates.map(({ row, groupId, platform: rawPlatform }) => {
    const group = groupId == null ? undefined : groups.get(groupId)
    const dimension = groupId == null ? undefined : dimensions.get(groupId)
    const groupPlatform = (dimension?.platform || group?.platform || '').trim().toLowerCase()
    const platform = (groupPlatform && groupPlatform !== 'composite' ? groupPlatform : rawPlatform.trim().toLowerCase())
    const provider = ALIASES[platform] ?? platform
    const buckets = validBuckets(row, window)
    const hasTraffic = buckets.length > 0 || hasMetricFlow(row?.metrics)
    const rate = groupId == null ? undefined : input.userRates[groupId] ?? group?.rate_multiplier
    const rateMultiplier = input.ratesAvailable && typeof rate === 'number' && Number.isFinite(rate) && rate >= 0 ? rate : null
    return {
      id: [rawPlatform.trim(), groupId ?? '', row?.model ?? ''].join(':'),
      groupId,
      name: group?.name?.trim() || dimension?.name?.trim() || row?.group_name?.trim() || '',
      platform,
      provider,
      providerLabel: PROVIDERS[provider] ?? rawPlatform.trim(),
      rateMultiplier,
      hasTraffic,
      health: hasTraffic && row ? row.health.overall : 'no-data',
      metricHealth: {
        availability: hasTraffic && row ? row.health.error_rate : 'no-data',
        cache: hasTraffic && row ? row.health.cache ?? 'unknown' : 'no-data',
        ttft: hasTraffic && row ? (isTtftUnavailable(row.metrics.ttft) ? 'unknown' : row.health.ttft) : 'no-data',
      },
      metrics: row?.metrics ?? null,
      bars: buildBars(buckets, window, input.range),
    } satisfies MonitorCardData
  }).sort(compareCard)
}

export function groupByProvider(cards: MonitorCardData[]): ProviderSection[] {
  const sections = new Map<string, ProviderSection>()
  for (const card of [...cards].sort(compareCard)) {
    const section = sections.get(card.provider) ?? { provider: card.provider, label: card.providerLabel, cards: [] }
    section.cards.push(card)
    sections.set(card.provider, section)
  }
  return [...sections.values()]
}
