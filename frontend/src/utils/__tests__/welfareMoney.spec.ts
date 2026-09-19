import { describe, expect, it } from 'vitest'
import { welfareMoney } from '../welfareMoney'

describe('welfare money presentation', () => {
  it('removes redundant zeros while preserving exact micro-spend and currency cents', () => {
    expect(welfareMoney('8.83000000')).toBe('$8.83')
    expect(welfareMoney('0.00000001')).toBe('$0.00000001')
    expect(welfareMoney('5000.00000000')).toBe('$5,000.00')
    expect(welfareMoney('-0.01000000')).toBe('−$0.01')
  })
})
