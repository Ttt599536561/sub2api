import { describe, expect, it } from 'vitest'
import { welfareErrorCode } from '../welfareError'
describe('welfare service errors', () => {
  it('uses the structured reason returned by the Go HTTP response', () => {
    expect(welfareErrorCode({ status: 409, code: 409, reason: 'WELFARE_QUOTE_STALE' })).toBe('WELFARE_QUOTE_STALE')
  })
  it('also accepts unwrapped string codes and ignores private server text', () => {
    expect(welfareErrorCode({ code: 'WELFARE_NO_TICKETS' })).toBe('WELFARE_NO_TICKETS')
    expect(welfareErrorCode({ reason: 'INTERNAL_ERROR', message: 'private details' })).toBe('network')
  })
})
