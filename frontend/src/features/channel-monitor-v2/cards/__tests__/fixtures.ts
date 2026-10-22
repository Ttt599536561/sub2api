import type { Group } from '@/types'
import type { MonitorCoverage, MonitorHealth, MonitorMatrixRow, MonitorMetric } from '@/api/channelMonitorV2'

export const health: MonitorHealth = { overall: 'warning', error_rate: 'critical', ttft: 'healthy', cache: 'warning', minimum_sample: 20 }
export const metrics: MonitorMetric = {
  request_count: 40, success_requests: 39, error_requests: 1, token_count: 1000,
  rpm: 1, tpm: 20, error_rate: 0.025, cache_rate: 0.8, cache_rate_numerator: 80, cache_rate_denominator: 100,
  ttft: { sample_count: 30, p50_ms: 120, p95_ms: 300, avg_ms: 170 },
  duration: { sample_count: 40, p50_ms: 600, p95_ms: 800, avg_ms: 700 },
}
export const zeroMetrics: MonitorMetric = { ...metrics, request_count: 0, success_requests: 0, error_requests: 0, token_count: 0, rpm: 0, tpm: 0, error_rate: 0, cache_rate: 0, cache_rate_numerator: 0, cache_rate_denominator: 0 }
export const coverage: MonitorCoverage = {
  requested_start: '2026-10-23T00:00:00Z', requested_end: '2026-10-23T01:30:00Z',
  coverage_start: '2026-10-23T00:00:00Z', data_through: '2026-10-23T01:30:00Z', computed_at: '2026-10-23T01:30:00Z',
  aggregation_lag_seconds: 0, coverage_complete: true, bucket_seconds: 300,
}
export function row(overrides: Partial<MonitorMatrixRow> = {}): MonitorMatrixRow {
  return { platform: 'openai', group_id: 7, group_name: 'Raw name', metrics, health,
    buckets: [{ bucket_start: '2026-10-23T01:25:00Z', metrics, health }], ...overrides }
}
export function group(overrides: Partial<Group> = {}): Group {
  return { id: 7, name: '本地化名称', platform: 'openai', rate_multiplier: 1.5, ...overrides } as Group
}
