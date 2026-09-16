// Separate login sessions from access-token rotations, including a return to
// the same user and login changes made by another tab.
const AUTH_SESSION_KEY = 'auth_session_id'
let volatileSessionID: string | null = null

export function getAuthSessionID(): string | null {
  if (volatileSessionID !== null) return volatileSessionID
  try { return localStorage.getItem(AUTH_SESSION_KEY) } catch { return null }
}

export function advanceAuthSession(): string {
  const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  try {
    localStorage.setItem(AUTH_SESSION_KEY, id)
    volatileSessionID = null
  } catch {
    // Storage limits must not prevent logout or same-tab invalidation.
    volatileSessionID = id
  }
  return id
}
