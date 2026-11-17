const reasonKeys: Record<string, string> = {
  SUBSCRIPTION_SUPPLEMENT_INVALID: 'welfareRedeem.errors.supplementInvalid',
  SUBSCRIPTION_SUPPLEMENT_GROUP_INVALID: 'welfareRedeem.errors.supplementGroupUnavailable',
  SUBSCRIPTION_SUPPLEMENT_NO_ACTIVE_SUBSCRIPTION: 'welfareRedeem.errors.activeSubscriptionRequired',
  SUBSCRIPTION_SUPPLEMENT_LIMIT_EXCEEDED: 'welfareRedeem.errors.limitExceeded',
  WELFARE_SUPPLEMENT_INVALID: 'welfareRedeem.errors.invalid',
  WELFARE_SUPPLEMENT_CAMPAIGN_INVALID: 'welfareRedeem.errors.campaignUnavailable',
  WELFARE_SUPPLEMENT_NO_ACTIVE_CYCLE: 'welfareRedeem.errors.subscriptionRequired',
  WELFARE_SUPPLEMENT_LIMIT_EXCEEDED: 'welfareRedeem.errors.limitExceeded',
  GROUP_WELFARE_REFERENCED: 'welfareRedeem.errors.groupReferenced',
  GROUP_WRITE_RETRY_REQUIRED: 'welfareRedeem.errors.groupWriteRetry',
  REDEEM_CODE_CANCELLED: 'redeem.codeCancelled'
}

type WelfareAPIError = {
  reason?: string; code?: string; message?: string; metadata?: Record<string, unknown>
  response?: { data?: { reason?: string; detail?: string; message?: string; metadata?: Record<string, unknown> } }
}

export function welfareErrorReason(error: unknown): string {
  const apiError = error as WelfareAPIError | null
  return apiError?.reason || apiError?.response?.data?.reason || apiError?.code || ''
}

export function welfareErrorMessage(error: unknown, t: (key: string, params?: Record<string, unknown>) => string, fallback: string): string {
  const apiError = error as WelfareAPIError | null
  const data = apiError?.response?.data
  const reason = welfareErrorReason(error)
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
