import { describe, expect, it } from 'vitest'
import { welfareErrorMessage } from '../welfareRedeemError'

describe('welfare group error details', () => {
  const t = (key: string, params?: Record<string, unknown>) => params
    ? `${key}: ${params.groupId}/${params.campaigns}/${params.codes}/${params.subscriptions}` : key

  it('shows server-supplied group reference counts with the localized protection message', () => {
    const message = welfareErrorMessage({ reason: 'GROUP_WELFARE_REFERENCED', metadata: {
      group_id: '42', campaign_count: '2', unused_supplement_count: '5', welfare_subscription_count: '12'
    } }, t, 'fallback')
    expect(message).toContain('welfareRedeem.errors.groupReferenced')
    expect(message).toContain('welfareRedeem.referenceCounts: 42/2/5/12')
  })

  it('keeps the localized protection message when an older server provides no counts', () => {
    expect(welfareErrorMessage({ reason: 'GROUP_WELFARE_REFERENCED' }, t, 'fallback'))
      .toBe('welfareRedeem.errors.groupReferenced')
  })
})
