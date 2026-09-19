const known = new Set(['WELFARE_PAUSED', 'WELFARE_NOT_LAUNCHED', 'WELFARE_NO_TICKETS', 'WELFARE_QUOTE_STALE', 'WELFARE_INSUFFICIENT_BALANCE', 'WELFARE_IDEMPOTENCY_CONFLICT', 'WELFARE_INVALID_AMOUNT', 'WELFARE_INVALID_REQUEST'])
export function welfareErrorCode(error: unknown): string {
  const e = error as { code?: unknown; reason?: unknown; response?: { data?: { code?: unknown; reason?: unknown } } }
  const candidates = [e?.reason, e?.response?.data?.reason, e?.code, e?.response?.data?.code]
  return candidates.find((code): code is string => typeof code === 'string' && known.has(code)) || 'network'
}
