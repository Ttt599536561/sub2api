import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAccountNameMap, resetAccountNameCacheForTests } from '../accountNameMap'

const list = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: { list } }))

function login(sessionID: string, userID = 1) {
  localStorage.setItem('auth_session_id', sessionID)
  localStorage.setItem('auth_user', JSON.stringify({ id: userID, role: 'admin' }))
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(ok => { resolve = ok })
  return { promise, resolve }
}

const result = (name: string, total = 1) => ({ items: [{ id: 10, name }], total })

beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  login('session-A')
  resetAccountNameCacheForTests()
})

describe('account name cache ownership', () => {
  it.each([1, 2])('reloads after a new login for user %s', async userID => {
    list.mockResolvedValueOnce(result('private-A')).mockResolvedValueOnce(result('current-B'))
    expect((await getAccountNameMap()).get(10)).toBe('private-A')
    login('session-B', userID)
    expect((await getAccountNameMap()).get(10)).toBe('current-B')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('stops an old paginated load before issuing another request or replacing the current cache', async () => {
    const old = deferred<ReturnType<typeof result>>()
    list.mockReturnValueOnce(old.promise).mockResolvedValue(result('current-B'))
    const pending = getAccountNameMap().catch(error => error)
    login('session-B', 2)
    expect((await getAccountNameMap()).get(10)).toBe('current-B')
    old.resolve(result('private-A', 2))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect((await getAccountNameMap()).get(10)).toBe('current-B')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('captures request ownership before the account API queues its interceptor', async () => {
    list.mockResolvedValue(result('private-A'))
    await getAccountNameMap()
    expect(list).toHaveBeenCalledWith(1, 1000, {
      lite: '1', include_scheduler_score: '0'
    }, expect.objectContaining({ authIdentity: { userID: 1, sessionID: 'session-A' } }))
  })
})
