const reasonKeys: Record<string, string> = {
  WELFARE_SUPPLEMENT_INVALID: 'welfareRedeem.errors.invalid',
  WELFARE_SUPPLEMENT_CAMPAIGN_INVALID: 'welfareRedeem.errors.campaignUnavailable',
  WELFARE_SUPPLEMENT_NO_ACTIVE_CYCLE: 'welfareRedeem.errors.subscriptionRequired',
  WELFARE_SUPPLEMENT_LIMIT_EXCEEDED: 'welfareRedeem.errors.limitExceeded',
  GROUP_WELFARE_REFERENCED: 'welfareRedeem.errors.groupReferenced',
  GROUP_WRITE_RETRY_REQUIRED: 'welfareRedeem.errors.groupWriteRetry',
  REDEEM_CODE_CANCELLED: 'redeem.codeCancelled'
}

export function welfareErrorMessage(error: unknown, t: (key: string, params?: Record<string, unknown>) => string, fallback: string): string {
  const apiError = error as {
    reason?: string; code?: string; message?: string; metadata?: Record<string, unknown>
    response?: { data?: { reason?: string; detail?: string; message?: string; metadata?: Record<string, unknown> } }
  } | null
  const data = apiError?.response?.data
  const reason = apiError?.reason || data?.reason || apiError?.code || ''
  const key = reasonKeys[reason]
  if (reason === 'GROUP_WELFARE_REFERENCED') {
    const metadata = apiError?.metadata || data?.metadata
    const fields = ['group_id', 'campaign_count', 'unused_supplement_count', 'welfare_subscription_count']
    if (metadata && fields.every(field => metadata[field] !== undefined && /^\d+$/.test(String(metadata[field])))) {
      return `${t(key)} ${t('welfareRedeem.referenceCounts', {
        groupId: metadata.group_id, campaigns: metadata.campaign_count,
        codes: metadata.unused_supplement_count, subscriptions: metadata.welfare_subscription_count
      })}`
    }
  }
  return key ? t(key) : data?.detail || data?.message || apiError?.message || t(fallback)
}
