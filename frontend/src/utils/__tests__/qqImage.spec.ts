import { beforeEach, describe, expect, it } from 'vitest'
import { qqAuthTarget, qqReturnPath, rememberQQRequest, restoreQQRequest, validQQRequest, safeAuthReturnPath, qqErrorKey } from '../qqImage'

beforeEach(() => sessionStorage.clear())
describe('QQ binding return and request locator', () => {
  it('only carries fixed local destinations across registration', () => {
    expect(qqReturnPath('/qq/bind')).toBe('/qq/bind')
    expect(qqReturnPath('https://evil.test/qq/bind')).toBe('')
    expect(qqReturnPath('//evil.test')).toBe('')
    expect(qqReturnPath('/qq/bind?redirect=https://evil.test')).toBe('')
    expect(qqAuthTarget('/register', '/qq/bind')).toEqual({ path: '/register', query: { redirect: '/qq/bind' } })
  })
  it('restores only an unexpired request identifier and never a confirmation code', () => {
    rememberQQRequest('request_123', 1000)
    expect(restoreQQRequest(2000)).toBe('request_123')
    expect(restoreQQRequest(1000 + 30 * 60 * 1000)).toBe('')
    expect(sessionStorage.length).toBe(0)
    expect(validQQRequest('a/b?token=bad')).toBe('')
  })
  it('rejects external and protocol-relative login destinations', () => {
    expect(safeAuthReturnPath('https://evil.test')).toBe('/dashboard')
    expect(safeAuthReturnPath('//evil.test')).toBe('/dashboard')
    expect(safeAuthReturnPath('/\\evil.test')).toBe('/dashboard')
    expect(safeAuthReturnPath('/qq/orders')).toBe('/qq/orders')
    expect(safeAuthReturnPath('/keys?page=2')).toBe('/keys?page=2')
  })
})


describe('QQ email error messages', () => {
  it.each([
    ['EMAIL_NOT_CONFIGURED', 503, 'emailNotConfigured'],
    ['EMAIL_RESERVED', 400, 'emailReserved'],
    ['INVALID_VERIFY_CODE', 400, 'invalidVerifyCode'],
    ['VERIFY_CODE_MAX_ATTEMPTS', 429, 'verifyCodeMaxAttempts'],
    ['VERIFY_CODE_TOO_FREQUENT', 429, 'rate'],
    ['EMAIL_VERIFY_REQUIRED', 400, 'email']
  ])('maps %s to actionable guidance', (reason, status, expected) => {
    expect(qqErrorKey({ status, reason })).toBe(expected)
    expect(qqErrorKey({ status, code: reason })).toBe(expected)
  })
  it('prefers the structured reason over a generic code', () => {
    expect(qqErrorKey({ status: 503, code: 'SERVICE_UNAVAILABLE', reason: 'EMAIL_NOT_CONFIGURED' })).toBe('emailNotConfigured')
  })
  it('maps the cooldown reason even when an HTTP status is unavailable', () => {
    expect(qqErrorKey({ reason: 'VERIFY_CODE_TOO_FREQUENT' })).toBe('rate')
  })
  it('does not mislabel unknown email failures as missing email proof', () => {
    expect(qqErrorKey({ status: 503, reason: 'EMAIL_DELIVERY_FAILED' })).toBe('failed')
  })
  it('never interprets backend message text as an error reason', () => {
    expect(qqErrorKey({ status: 503, message: 'EMAIL_NOT_CONFIGURED private SMTP detail' })).toBe('failed')
  })
})
