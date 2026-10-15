import { accountsAPI } from '@/api/admin/accounts'
import { ownedAuthRequestConfig } from '@/api/client'
import { assertAuthSessionCurrent } from '@/api/authSession'
import { CanceledError } from 'axios'

// 运维监控请求明细只带 account_id，账号名由前端用 lite 账号列表建映射；
// 模块级缓存避免每次打开弹窗都拉全量账号。
const ACCOUNT_NAME_PAGE_SIZE = 1000

type AccountNameOwner = ReturnType<typeof ownedAuthRequestConfig>['authIdentity']
let accountNameCache: { owner: AccountNameOwner; names: Map<number, string> } | null = null
let cacheGeneration = 0

function sameOwner(a: AccountNameOwner, b: AccountNameOwner): boolean {
  return a.sessionID === b.sessionID && a.userID === b.userID
}

export async function getAccountNameMap(force = false, signal?: AbortSignal): Promise<Map<number, string>> {
  const request = ownedAuthRequestConfig()
  if (accountNameCache && !sameOwner(accountNameCache.owner, request.authIdentity)) clearAccountNameCache()
  const generation = cacheGeneration
  const assertCurrent = () => {
    assertAuthSessionCurrent(request.authIdentity.sessionID)
    if (signal?.aborted || generation !== cacheGeneration || !sameOwner(request.authIdentity, ownedAuthRequestConfig().authIdentity)) {
      throw new CanceledError('Account names request is no longer current')
    }
  }
  assertCurrent()
  if (!force && accountNameCache) return accountNameCache.names

  const map = new Map<number, string>()
  let page = 1
  let total = Number.POSITIVE_INFINITY
  while (map.size < total) {
    const res = await accountsAPI.list(page, ACCOUNT_NAME_PAGE_SIZE, {
      lite: '1',
      include_scheduler_score: '0'
    }, { ...request, signal })
    assertCurrent()
    total = res.total || 0
    const items = res.items || []
    if (items.length === 0) break
    for (const item of items) map.set(item.id, item.name)
    page++
  }

  accountNameCache = { owner: request.authIdentity, names: map }
  return map
}

export function clearAccountNameCache() {
  cacheGeneration++
  accountNameCache = null
}

export const resetAccountNameCacheForTests = clearAccountNameCache
