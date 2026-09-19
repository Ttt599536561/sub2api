import { getAuthSessionID } from '@/api/authSession'
import type { WelfareRedemption } from '@/types/welfare'
export interface WelfareIdentity { sessionID: string | null; userID: number | null }
export interface WelfarePendingRedemption { key: string; body: WelfareRedemption }
export interface WelfarePending { drawKey: string | null; redemption: WelfarePendingRedemption | null }
export function welfareIdentity(): WelfareIdentity {
  let userID: number | null = null
  try { const id = JSON.parse(localStorage.getItem('auth_user') || 'null')?.id; if (Number.isSafeInteger(id) && id > 0) userID = id } catch { /* An unreadable identity is never authenticated. */ }
  return { sessionID: getAuthSessionID(), userID }
}
export function sameWelfareIdentity(a: WelfareIdentity, b: WelfareIdentity) { return a.sessionID === b.sessionID && a.userID === b.userID }
function storageKey(identity: WelfareIdentity) { return `welfare.pending:${identity.userID}:${identity.sessionID || 'legacy'}` }
export function loadWelfarePending(identity: WelfareIdentity): WelfarePending {
  const empty = { drawKey: null, redemption: null }
  if (!identity.userID) return empty
  try {
    const saved = JSON.parse(sessionStorage.getItem(storageKey(identity)) || 'null')
    if (!saved) return empty
    const keyValid = (key: unknown): key is string => typeof key === 'string' && key.length > 0 && key.length <= 128
    const redemption = saved.redemption
    const body = redemption?.body
    return {
      drawKey: keyValid(saved.drawKey) ? saved.drawKey : null,
      redemption: keyValid(redemption?.key) && typeof body?.amount === 'string' && /^\d+(\.\d{1,2})?$/.test(body.amount) && Number.isSafeInteger(body.welfare_balance_version) && body.welfare_balance_version >= 0 ? { key: redemption.key, body: { amount: body.amount, welfare_balance_version: body.welfare_balance_version } } : null
    }
  } catch { return empty }
}
export function saveWelfarePending(identity: WelfareIdentity, pending: WelfarePending): boolean {
  if (!identity.userID) return false
  try {
    if (pending.drawKey || pending.redemption) sessionStorage.setItem(storageKey(identity), JSON.stringify(pending))
    else sessionStorage.removeItem(storageKey(identity))
    return true
  } catch { return false }
}
