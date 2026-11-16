import { beforeEach, describe, expect, it, vi } from 'vitest'
import { redeemAPI } from '../admin/redeem'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get, post } }))

describe('admin welfare redeem API contract', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    post.mockResolvedValue({ data: [] })
  })

  it('loads campaign options from the redeem management endpoint', async () => {
    const options = [{ id: 7, name: 'Autumn', version: 2, subscription_group_id: 9, group_name: 'Welfare' }]
    get.mockResolvedValue({ data: options })
    expect(await redeemAPI.welfareCampaigns()).toEqual(options)
    expect(get).toHaveBeenCalledWith('/admin/redeem-codes/welfare-campaigns')
  })

  it('sends fixed USD quota with the displayed group as a binding assertion and no duration', async () => {
    await redeemAPI.generate(3, 'subscription', 0, 9, undefined, 14, {
      subscription_effect: 'add_quota', welfare_campaign_id: 7, quota_amount_usd: 12.5
    })
    expect(post).toHaveBeenCalledWith('/admin/redeem-codes/generate', {
      count: 3, type: 'subscription', value: 0, group_id: 9, expires_in_days: 14,
      subscription_effect: 'add_quota', welfare_campaign_id: 7, quota_amount_usd: 12.5
    })
  })

  it('sends extension days without a quota amount', async () => {
    await redeemAPI.generate(1, 'subscription', 0, 9, 45, undefined, {
      subscription_effect: 'extend_duration', welfare_campaign_id: 7
    })
    expect(post).toHaveBeenCalledWith('/admin/redeem-codes/generate', {
      count: 1, type: 'subscription', value: 0, group_id: 9, validity_days: 45,
      subscription_effect: 'extend_duration', welfare_campaign_id: 7
    })
  })

  it('preserves ordinary subscription parameters without supplement metadata', async () => {
    await redeemAPI.generate(2, 'subscription', 10, 8, 30, 7)
    expect(post).toHaveBeenCalledWith('/admin/redeem-codes/generate', {
      count: 2, type: 'subscription', value: 10, group_id: 8, validity_days: 30, expires_in_days: 7
    })
  })
})
