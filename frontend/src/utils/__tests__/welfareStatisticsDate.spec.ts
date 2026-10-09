import { describe, expect, it } from 'vitest'
import { validWelfareDateRange } from '../welfareStatisticsDate'

describe('welfare statistics business date validation', () => {
  it('rejects the year zero to match the backend business date contract', () => {
    expect(validWelfareDateRange('0000-01-01', '0000-01-02')).toBe(false)
    expect(validWelfareDateRange('0000-12-31', '0001-01-01')).toBe(false)
  })

  it('accepts a real early calendar year and a leap day', () => {
    expect(validWelfareDateRange('0001-01-01', '0001-01-02')).toBe(true)
    expect(validWelfareDateRange('2024-02-28', '2024-02-29')).toBe(true)
    expect(validWelfareDateRange('2025-02-29', '2025-03-01')).toBe(false)
  })
})
