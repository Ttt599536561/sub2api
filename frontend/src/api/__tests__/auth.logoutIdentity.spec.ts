import { beforeEach, describe, expect, it, vi } from 'vitest'
import { logout } from '@/api/auth'
import { advanceAuthSession } from '@/api/authSession'

const post = vi.hoisted(() => vi.fn())
vi.mock('@/api/client', () => ({ apiClient: { post } }))

describe('logout API storage isolation', () => {
  beforeEach(() => { localStorage.clear(); vi.resetAllMocks() })

  it.each(['success', 'failure'] as const)('preserves a new login after delayed logout %s', async outcome => {
    localStorage.setItem('auth_token', 'old-token')
    localStorage.setItem('refresh_token', 'old-refresh')
    advanceAuthSession()
    let finish!: (value: unknown) => void
    let fail!: (error: Error) => void
    post.mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject }))
    const old = logout()
    advanceAuthSession()
    localStorage.setItem('auth_token', 'new-token')
    localStorage.setItem('refresh_token', 'new-refresh')
    if (outcome === 'success') finish({})
    else fail(new Error('old logout failed'))
    await old

    expect(post).toHaveBeenCalledWith('/auth/logout', { refresh_token: 'old-refresh' })
    expect(localStorage.getItem('auth_token')).toBe('new-token')
    expect(localStorage.getItem('refresh_token')).toBe('new-refresh')
  })
})
