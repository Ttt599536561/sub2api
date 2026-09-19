import { describe, expect, it, vi } from 'vitest'
import { AxiosError, AxiosHeaders } from 'axios'
import { apiClient } from '../client'
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
describe('rate-limit retry guidance', () => {
  it('preserves Retry-After on structured HTTP errors', async () => {
    await expect(apiClient.get('/user/welfare/overview', { adapter: async config => {
      const response = { status: 429, statusText: 'Too Many Requests', headers: new AxiosHeaders({ 'retry-after': '35' }), config, data: { code: 'RATE_LIMITED', message: 'Slow down' } }
      throw new AxiosError('Rate limited', 'ERR_BAD_REQUEST', config, undefined, response)
    } })).rejects.toMatchObject({ status: 429, retryAfter: '35' })
  })
})
