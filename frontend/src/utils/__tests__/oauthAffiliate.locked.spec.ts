import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as affiliate from '../oauthAffiliate'

describe('protected affiliate attribution', () => {
  beforeEach(() => { localStorage.clear(); sessionStorage.clear() })
  it('suppresses persisted, URL, form and OAuth affiliate codes after server attribution', () => {
    affiliate.storeAffiliateReferralCode('OLD')
    affiliate.storeOAuthAffiliateCode('OLD')
    affiliate.setAffiliateAttributionLocked(true)
    expect(affiliate.loadAffiliateReferralCode()).toBe('')
    expect(affiliate.loadOAuthAffiliateCode()).toBe('')
    expect(affiliate.resolveAffiliateReferralCode('URL')).toBe('')
    expect(affiliate.oauthAffiliatePayload('FORM')).toEqual({})
    affiliate.storeAffiliateReferralCode('NEW')
    affiliate.storeOAuthAffiliateCode('NEW')
    expect(affiliate.loadAffiliateReferralCode()).toBe('')
    expect(affiliate.loadOAuthAffiliateCode()).toBe('')
  })
  it('retains only a non-authoritative UI lock across the OAuth page reload', async () => {
    affiliate.setAffiliateAttributionLocked(true)
    vi.resetModules()
    const reloaded = await import('../oauthAffiliate')
    expect(reloaded.isAffiliateAttributionLocked()).toBe(true)
    expect(reloaded.oauthAffiliatePayload('TAMPERED')).toEqual({})
    reloaded.setAffiliateAttributionLocked(false)
    expect(reloaded.resolveAffiliateReferralCode('NORMAL')).toBe('NORMAL')
  })
})
