import { beforeEach, describe, expect, it, vi } from 'vitest'
const { get, put, owner } = vi.hoisted(() => ({
  get: vi.fn(), put: vi.fn(),
  owner: { authIdentity: { userID: 1, sessionID: 'admin-session' } }
}))
vi.mock('@/api/client', () => ({ apiClient: { get, put }, ownedAuthRequestConfig: () => owner }))
import { affiliatesAPI } from '@/api/admin/affiliates'
import { getAffiliateAttribution } from '@/api/auth'
describe('affiliate domain APIs', () => {
  beforeEach(() => vi.clearAllMocks())
  it('uses the dedicated atomic domain endpoint and captures the caller session', async () => {
    put.mockResolvedValue({ data: { affiliate_domain: '', referral_url: '' } })
    await affiliatesAPI.updateUserDomain(42, { domain: '' })
    expect(put).toHaveBeenCalledWith('/admin/affiliates/users/42/domain', { domain: '' }, owner)
  })
  it('reads attribution separately from public settings', async () => {
    get.mockResolvedValue({ data: { locked: true } })
    await expect(getAffiliateAttribution()).resolves.toEqual({ locked: true })
    expect(get.mock.calls[0][0]).toBe('/auth/affiliate-attribution')
  })
})
