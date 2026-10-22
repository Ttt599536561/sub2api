import { describe, expect, it, vi, afterEach } from 'vitest'
import { apiClient } from '../client'
import { getAvailable, getUserGroupRates } from '../groups'
afterEach(() => vi.restoreAllMocks())
describe('user group cancellation', () => {
  it('forwards the cancellation signal to both read endpoints', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: [] })
    const signal = new AbortController().signal
    await getAvailable(signal)
    await getUserGroupRates(signal)
    expect(get).toHaveBeenCalledWith('/groups/available', { signal })
    expect(get).toHaveBeenCalledWith('/groups/rates', { signal })
  })
})
