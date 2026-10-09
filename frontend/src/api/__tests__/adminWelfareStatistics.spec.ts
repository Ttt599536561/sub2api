import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { adminWelfareAPI } from '../admin/welfare'
vi.mock('../client', () => ({ apiClient: { get: vi.fn().mockResolvedValue({ data: { total_amount: '9999999999999999.01' } }) } }))

describe('admin welfare statistics transport', () => {
  beforeEach(() => vi.clearAllMocks())
  it('exposes summary, users and reward record endpoints', () => {
    expect(adminWelfareAPI).toHaveProperty('getStatistics', expect.any(Function))
    expect(adminWelfareAPI).toHaveProperty('getStatisticsUsers', expect.any(Function))
    expect(adminWelfareAPI).toHaveProperty('getStatisticsRecords', expect.any(Function))
  })
  it('transports filter, sorting and page queries with cancellation and preserves amount strings', async () => {
    const signal = new AbortController().signal
    const common = { date_from: '2026-10-03', date_to: '2026-10-09', search: 'alice', user_id: 7 }
    const result = await adminWelfareAPI.getStatistics(common, signal)
    await adminWelfareAPI.getStatisticsUsers({ ...common, page: 2, page_size: 20, sort_by: 'period_total_amount', sort_order: 'asc' }, signal)
    await adminWelfareAPI.getStatisticsRecords({ ...common, type: 'streak', page: 3, page_size: 20 }, signal)
    expect(apiClient.get).toHaveBeenNthCalledWith(1, '/admin/welfare/statistics', { params: common, signal })
    expect(apiClient.get).toHaveBeenNthCalledWith(2, '/admin/welfare/statistics/users', { params: expect.objectContaining({ ...common, page: 2, sort_by: 'period_total_amount', sort_order: 'asc' }), signal })
    expect(apiClient.get).toHaveBeenNthCalledWith(3, '/admin/welfare/statistics/records', { params: expect.objectContaining({ ...common, page: 3, type: 'streak' }), signal })
    expect(result).toEqual({ total_amount: '9999999999999999.01' })
  })
})
