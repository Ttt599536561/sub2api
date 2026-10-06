import { describe, expect, it } from 'vitest'
import { calculatePaymentFee, currencySymbol, formatPaymentAmount } from '../currency'

describe('calculatePaymentFee', () => {
  it.each([
    { amount: 1, rate: 7, currency: 'CNY', fee: 0.07 },
    { amount: 1, rate: 14, currency: 'CNY', fee: 0.14 },
    { amount: 100, rate: 2.5, currency: 'JPY', fee: 3 },
    { amount: 12.345, rate: 1, currency: 'KWD', fee: 0.124 },
    { amount: 1, rate: 1.23, currency: 'IQD', fee: 0.013 },
    { amount: 1, rate: 1.23, currency: 'CLF', fee: 0.02 },
    { amount: 1, rate: 7.001, currency: 'CNY', fee: 0.08 },
    { amount: 1, rate: 7.000000000001, currency: 'CNY', fee: 0.08 },
    { amount: 1, rate: 7.000000000000001, currency: 'CNY', fee: 0.07 },
    { amount: 1e-21, rate: 7, currency: 'CNY', fee: 0 },
    { amount: 1e-7, rate: 7, currency: 'CNY', fee: 0.01 },
    { amount: 1, rate: 1e-7, currency: 'CNY', fee: 0.01 },
    { amount: 1e21, rate: 1e-7, currency: 'CNY', fee: 1e12 }
  ])('rounds $amount at $rate% upward to $currency minor units', ({ amount, rate, currency, fee }) => {
    expect(calculatePaymentFee(amount, rate, currency)).toBe(fee)
  })

  it.each([0, -1, NaN, Infinity])('ignores nonpositive or invalid amount/rate %s', value => {
    expect(calculatePaymentFee(value, 7, 'CNY')).toBe(0)
    expect(calculatePaymentFee(1, value, 'CNY')).toBe(0)
  })
})

describe('formatPaymentAmount', () => {
  it('uses the currency default fraction digits', () => {
    expect(formatPaymentAmount(100, 'JPY', 'en-US')).not.toContain('.00')
    expect(formatPaymentAmount(100, 'KRW', 'en-US')).not.toContain('.00')
    expect(formatPaymentAmount(100, 'HKD', 'en-US')).toContain('.00')
  })

  it('uses the backend payment precision when Intl currency defaults differ', () => {
    expect(formatPaymentAmount(1.234, 'IQD', 'en-US')).toContain('1.234')
    expect(formatPaymentAmount(1.2345, 'CLF', 'en-US')).toContain('1.23')
    expect(formatPaymentAmount(1.2345, 'CLF', 'en-US')).not.toContain('1.2345')
  })
})

describe('currencySymbol', () => {
  it('maps common payment currencies and falls back safely', () => {
    expect(currencySymbol('USD')).toBe('$')
    expect(currencySymbol('cny')).toBe('¥')
    expect(currencySymbol('EUR')).toBe('€')
    expect(currencySymbol('')).toBe('¥')
    expect(currencySymbol('XYZ')).toBe('XYZ')
  })
})
